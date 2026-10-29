package mihomo

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ErrBinaryMissing 核心程序不存在且没有开启自动安装。
var ErrBinaryMissing = errors.New("mihomo 核心未安装")

// 核心状态。
const (
	StatusDisabled   = "disabled"   // 没有任何 mihomo 节点，不需要核心
	StatusMissing    = "missing"    // 需要核心但程序不存在
	StatusInstalling = "installing" // 正在下载核心
	StatusStarting   = "starting"
	StatusRunning    = "running"
	StatusCrashed    = "crashed" // 意外退出，等待自动拉起
	StatusStopped    = "stopped" // 手动停止
	StatusFailed     = "failed"  // 配置或启动失败，没有在跑
)

// Options 核心托管参数。
type Options struct {
	// Home 核心的家目录：配置、缓存、订阅与规则集文件、Geo 数据库都在这里。
	Home string
	// Binary 显式指定的核心程序路径；留空时用 Home/bin/mihomo。
	Binary string
	// AutoInstall 需要核心但程序不存在时自动从 GitHub 发布页下载。
	AutoInstall bool
	// Mirror GitHub 下载前缀（如 https://axisnow.gh-proxy.org/），失败时回落直连。
	Mirror string
	// Version 自动安装的版本，留空为最新版。
	Version string
	Log     *zap.Logger
}

// Status 核心运行态快照。
type Status struct {
	Status         string     `json:"status"`
	Version        string     `json:"version,omitempty"`
	Binary         string     `json:"binary"`
	BinaryPresent  bool       `json:"binaryPresent"`
	Home           string     `json:"home"`
	PID            int        `json:"pid,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	AppliedAt      *time.Time `json:"appliedAt,omitempty"`
	ConfigHash     string     `json:"configHash,omitempty"`
	Restarts       int        `json:"restarts"`
	LastError      string     `json:"lastError,omitempty"`
	ControllerAddr string     `json:"controllerAddr,omitempty"`
	InboundAddr    string     `json:"inboundAddr,omitempty"`
	Targets        int        `json:"targets"`
	Proxies        int        `json:"proxies"`
	Groups         int        `json:"groups"`
	Providers      int        `json:"providers"`
	Rules          int        `json:"rules"`
	AutoInstall    bool       `json:"autoInstall"`
	Installing     bool       `json:"installing"`
	Platform       string     `json:"platform"`
	MemoryInUse    int64      `json:"memoryInUse"`
	UploadSpeed    int64      `json:"uploadSpeed"`
	DownloadSpeed  int64      `json:"downloadSpeed"`
}

// Core 托管一个 mihomo 进程。所有导出方法并发安全。
type Core struct {
	opts Options
	log  *zap.Logger

	secret   string
	password string

	mu             sync.Mutex
	status         string
	version        string
	lastError      string
	restarts       int
	startedAt      time.Time
	appliedAt      time.Time
	configHash     string
	controllerPort int
	inboundPort    int
	profile        *Profile
	targets        []string
	cmd            *exec.Cmd
	exited         chan struct{}
	wanted         bool // 期望核心在跑（Apply 过非空配置且没有手动停止）
	sampleCancel   context.CancelFunc
	installing     bool

	applyMu sync.Mutex
	closed  atomic.Bool

	logs    *logRing
	traffic *trafficRing
}

// NewCore 构造托管器，不会立刻启动进程（第一次 Apply 时才启动）。
func NewCore(opts Options) *Core {
	if opts.Log == nil {
		opts.Log = zap.NewNop()
	}
	if opts.Home == "" {
		opts.Home = filepath.Join(os.TempDir(), "aegis-mihomo")
	}
	if abs, err := filepath.Abs(opts.Home); err == nil {
		opts.Home = abs
	}
	return &Core{
		opts:     opts,
		log:      opts.Log.Named("mihomo"),
		secret:   randomToken(24),
		password: randomToken(24),
		status:   StatusDisabled,
		logs:     newLogRing(2000),
		traffic:  newTrafficRing(300),
	}
}

var defaultCore atomic.Pointer[Core]

// SetDefault 装配进程级核心，"mihomo" 端点协议拨号时从这里取入站地址。
func SetDefault(c *Core) { defaultCore.Store(c) }

// Default 进程级核心，未装配时为 nil。
func Default() *Core { return defaultCore.Load() }

// BinaryPath 实际使用的核心程序路径。
func (c *Core) BinaryPath() string {
	if strings.TrimSpace(c.opts.Binary) != "" {
		return c.opts.Binary
	}
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(c.opts.Home, "bin", name)
}

func (c *Core) configPath() string { return filepath.Join(c.opts.Home, "config.yaml") }

func binaryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// Status 当前运行态。
func (c *Core) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := Status{
		Status:        c.status,
		Version:       c.version,
		Binary:        c.BinaryPath(),
		BinaryPresent: binaryExists(c.BinaryPath()),
		Home:          c.opts.Home,
		ConfigHash:    c.configHash,
		Restarts:      c.restarts,
		LastError:     c.lastError,
		Targets:       len(c.targets),
		AutoInstall:   c.opts.AutoInstall,
		Installing:    c.installing,
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
	if c.cmd != nil && c.cmd.Process != nil && c.status != StatusCrashed {
		out.PID = c.cmd.Process.Pid
	}
	if !c.startedAt.IsZero() && c.status == StatusRunning {
		started := c.startedAt
		out.StartedAt = &started
	}
	if !c.appliedAt.IsZero() {
		applied := c.appliedAt
		out.AppliedAt = &applied
	}
	if c.controllerPort > 0 {
		out.ControllerAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(c.controllerPort))
	}
	if c.inboundPort > 0 {
		out.InboundAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(c.inboundPort))
	}
	if c.profile != nil {
		out.Proxies = len(c.profile.Proxies)
		out.Groups = len(c.profile.ProxyGroups)
		out.Providers = len(c.profile.ProxyProviders)
		out.Rules = len(c.profile.Rules)
	}
	if last, ok := c.traffic.last(); ok {
		out.MemoryInUse, out.UploadSpeed, out.DownloadSpeed = last.Memory, last.Up, last.Down
	}
	return out
}

// Running 核心是否就绪可用。
func (c *Core) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status == StatusRunning
}

// Inbound 网关连核心用的 SOCKS5 入站地址与口令；核心没在跑时 ok 为 false。
func (c *Core) Inbound() (addr, password string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status != StatusRunning || c.inboundPort <= 0 {
		return "", "", false
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.inboundPort)), c.password, true
}

// Client 控制器客户端；核心没在跑时返回错误。
func (c *Core) Client() (*Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.status != StatusRunning || c.controllerPort <= 0 {
		return nil, fmt.Errorf("mihomo 核心未运行（%s）", statusLabel(c.status))
	}
	return NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(c.controllerPort)), c.secret), nil
}

// HasTarget 网关引用的目标是否在当前生效的配置里。
func (c *Core) HasTarget(target string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, item := range c.targets {
		if item == target {
			return true
		}
	}
	return false
}

// Profile 当前生效的配置副本。
func (c *Core) Profile() (Profile, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.profile == nil {
		return Profile{}, false
	}
	return c.profile.Clone(), true
}

// Test 用核心自己的 -t 校验一份配置，不影响正在运行的核心。
// 核心程序不存在时返回 ErrBinaryMissing（调用方可以选择跳过这一步）。
func (c *Core) Test(ctx context.Context, p Profile, targets []string) error {
	if p.Empty() {
		return nil
	}
	binary := c.BinaryPath()
	if !binaryExists(binary) {
		return ErrBinaryMissing
	}
	data, err := Render(p, c.placeholderSystem(targets))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.opts.Home, 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(c.opts.Home, "config-test-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	_ = file.Close()
	return c.runTest(ctx, binary, file.Name())
}

func (c *Core) placeholderSystem(targets []string) System {
	c.mu.Lock()
	defer c.mu.Unlock()
	controller, inbound := c.controllerPort, c.inboundPort
	if controller <= 0 {
		controller = 9090
	}
	if inbound <= 0 {
		inbound = 7891
	}
	return System{
		ControllerAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(controller)),
		Secret:         c.secret,
		InboundPort:    inbound,
		Password:       c.password,
		Targets:        targets,
	}
}

func (c *Core) runTest(ctx context.Context, binary, path string) error {
	testCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(testCtx, binary, "-t", "-d", c.opts.Home, "-f", path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if testCtx.Err() != nil {
		return fmt.Errorf("核心校验配置超时（可能在下载 Geo 数据库或规则集）")
	}
	return fmt.Errorf("核心拒绝了这份配置: %s", summarizeOutput(output))
}

// Apply 让核心按新配置运行：校验 → 落盘 → 热重载（或首次启动）。
// targets 是网关引用的策略组 / 节点名。p 为空配置时停掉核心。
func (c *Core) Apply(ctx context.Context, p Profile, targets []string) error {
	if c.closed.Load() {
		return errors.New("mihomo 核心托管器已关闭")
	}
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	if p.Empty() {
		c.mu.Lock()
		c.wanted = false
		c.profile = nil
		c.targets = nil
		c.mu.Unlock()
		c.stopProcess()
		c.setStatus(StatusDisabled, "")
		return nil
	}
	if err := p.Validate(); err != nil {
		return err
	}
	binary := c.BinaryPath()
	if !binaryExists(binary) {
		if !c.opts.AutoInstall {
			c.setStatus(StatusMissing, ErrBinaryMissing.Error())
			return ErrBinaryMissing
		}
		if _, err := c.Install(ctx, c.opts.Version); err != nil {
			c.setStatus(StatusMissing, err.Error())
			return fmt.Errorf("自动安装 mihomo 核心失败: %w", err)
		}
	}
	if err := os.MkdirAll(c.opts.Home, 0o750); err != nil {
		return fmt.Errorf("创建核心目录失败: %w", err)
	}
	if err := c.ensurePorts(); err != nil {
		return err
	}

	data, err := Render(p, c.system(targets))
	if err != nil {
		return err
	}
	next := c.configPath() + ".next"
	if err := os.WriteFile(next, data, 0o600); err != nil {
		return fmt.Errorf("写入核心配置失败: %w", err)
	}
	if err := c.runTest(ctx, binary, next); err != nil {
		_ = os.Remove(next)
		return err
	}
	previous, _ := os.ReadFile(c.configPath())
	if err := os.Rename(next, c.configPath()); err != nil {
		return fmt.Errorf("替换核心配置失败: %w", err)
	}

	running := c.Running()
	if running {
		client, clientErr := c.Client()
		if clientErr == nil {
			clientErr = client.ReloadConfig(ctx, c.configPath())
		}
		if clientErr != nil {
			if len(previous) > 0 {
				_ = os.WriteFile(c.configPath(), previous, 0o600)
			}
			return fmt.Errorf("核心热重载失败: %w", clientErr)
		}
		c.logs.add("aegis", "info", "已热重载配置")
	} else {
		c.mu.Lock()
		c.wanted = true
		c.mu.Unlock()
		if err := c.startAndWait(ctx); err != nil {
			return err
		}
	}

	sum := sha256.Sum256(data)
	c.mu.Lock()
	cloned := p.Clone()
	c.profile = &cloned
	c.targets = uniqueStrings(targets)
	c.configHash = hex.EncodeToString(sum[:6])
	c.appliedAt = time.Now()
	c.wanted = true
	c.lastError = ""
	c.mu.Unlock()
	return nil
}

func (c *Core) system(targets []string) System {
	c.mu.Lock()
	defer c.mu.Unlock()
	return System{
		ControllerAddr: net.JoinHostPort("127.0.0.1", strconv.Itoa(c.controllerPort)),
		Secret:         c.secret,
		InboundPort:    c.inboundPort,
		Password:       c.password,
		Targets:        targets,
	}
}

func (c *Core) ensurePorts() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.controllerPort > 0 && c.inboundPort > 0 {
		return nil
	}
	return c.allocatePortsLocked()
}

func (c *Core) allocatePortsLocked() error {
	controller, err := freePort()
	if err != nil {
		return fmt.Errorf("分配核心控制器端口失败: %w", err)
	}
	inbound, err := freePort()
	if err != nil {
		return fmt.Errorf("分配核心入站端口失败: %w", err)
	}
	c.controllerPort, c.inboundPort = controller, inbound
	return nil
}

// Restart 重启核心进程（配置不变）。
func (c *Core) Restart(ctx context.Context) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	hasProfile := c.profile != nil
	c.mu.Unlock()
	if !hasProfile {
		return errors.New("当前没有需要核心的配置")
	}
	c.stopProcess()
	c.mu.Lock()
	c.wanted = true
	c.mu.Unlock()
	return c.startAndWait(ctx)
}

// Stop 手动停止核心，直到下一次 Apply 或 Restart。
func (c *Core) Stop() {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	c.wanted = false
	c.mu.Unlock()
	c.stopProcess()
	c.setStatus(StatusStopped, "")
}

// Close 停止核心并拒绝后续 Apply。
func (c *Core) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.mu.Lock()
	c.wanted = false
	c.mu.Unlock()
	c.stopProcess()
}

func (c *Core) setStatus(status, lastError string) {
	c.mu.Lock()
	c.status = status
	c.lastError = lastError
	c.mu.Unlock()
}

// startAndWait 启动进程并等待控制器可用；端口被占用时换一对端口重试一次。
func (c *Core) startAndWait(ctx context.Context) error {
	for attempt := 0; attempt < 2; attempt++ {
		err := c.startProcess()
		if err == nil {
			err = c.waitReady(ctx, 20*time.Second)
		}
		if err == nil {
			return nil
		}
		c.stopProcess()
		if attempt == 0 && c.logs.containsRecent("address already in use", 40) {
			c.mu.Lock()
			allocErr := c.allocatePortsLocked()
			profile, targets := c.profile, c.targets
			c.mu.Unlock()
			if allocErr == nil && profile != nil {
				if data, renderErr := Render(*profile, c.system(targets)); renderErr == nil {
					_ = os.WriteFile(c.configPath(), data, 0o600)
					c.logs.add("aegis", "warning", "端口被占用，已换一对端口重试")
					continue
				}
			}
		}
		c.setStatus(StatusFailed, err.Error())
		return err
	}
	return nil
}

func (c *Core) startProcess() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.status == StatusRunning {
		return nil
	}
	cmd := exec.Command(c.BinaryPath(), "-d", c.opts.Home, "-f", c.configPath())
	cmd.Dir = c.opts.Home
	cmd.Env = append(os.Environ(), "SAFE_PATHS="+c.opts.Home)
	configureProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 mihomo 核心失败: %w", err)
	}
	exited := make(chan struct{})
	c.cmd = cmd
	c.exited = exited
	c.status = StatusStarting
	c.startedAt = time.Now()
	c.logs.add("aegis", "info", fmt.Sprintf("核心进程已启动（PID %d）", cmd.Process.Pid))
	go c.pump(stdout)
	go c.watch(cmd, exited)
	return nil
}

func (c *Core) pump(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		level, message := parseLogLine(scanner.Text())
		c.logs.add("core", level, message)
	}
}

// watch 等进程退出；不是我们主动停的就按退避重新拉起。
func (c *Core) watch(cmd *exec.Cmd, exited chan struct{}) {
	err := cmd.Wait()
	close(exited)

	c.mu.Lock()
	if c.cmd != cmd {
		c.mu.Unlock()
		return
	}
	wanted := c.wanted && !c.closed.Load()
	uptime := time.Since(c.startedAt)
	c.cmd = nil
	if c.sampleCancel != nil {
		c.sampleCancel()
		c.sampleCancel = nil
	}
	if !wanted {
		c.mu.Unlock()
		return
	}
	if uptime > 2*time.Minute {
		c.restarts = 0
	}
	c.restarts++
	attempt := c.restarts
	c.status = StatusCrashed
	reason := "核心进程意外退出"
	if err != nil {
		reason += ": " + err.Error()
	}
	c.lastError = reason
	c.mu.Unlock()

	backoff := time.Duration(1<<min(attempt-1, 6)) * time.Second
	c.logs.add("aegis", "error", fmt.Sprintf("%s，%s 后重新拉起", reason, backoff))
	c.log.Warn("mihomo 核心意外退出", zap.Error(err), zap.Duration("backoff", backoff))
	time.AfterFunc(backoff, func() {
		c.applyMu.Lock()
		defer c.applyMu.Unlock()
		c.mu.Lock()
		still := c.wanted && c.cmd == nil && !c.closed.Load()
		c.mu.Unlock()
		if !still {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := c.startAndWait(ctx); err != nil {
			c.log.Warn("重新拉起 mihomo 核心失败", zap.Error(err))
		}
	})
}

func (c *Core) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	c.mu.Lock()
	exited := c.exited
	client := NewClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(c.controllerPort)), c.secret)
	c.mu.Unlock()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		version, err := client.Version(probeCtx)
		cancel()
		if err == nil {
			c.mu.Lock()
			c.status = StatusRunning
			c.version = version.Version
			c.lastError = ""
			sampleCtx, sampleCancel := context.WithCancel(context.Background())
			c.sampleCancel = sampleCancel
			c.mu.Unlock()
			c.logs.add("aegis", "info", "核心已就绪，版本 "+version.Version)
			go c.sample(sampleCtx, client)
			return nil
		}
		select {
		case <-exited:
			return fmt.Errorf("核心启动后立即退出: %s", c.logs.lastError())
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待核心就绪超时: %s", c.logs.lastError())
		}
	}
}

func (c *Core) stopProcess() {
	c.mu.Lock()
	cmd, exited := c.cmd, c.exited
	c.cmd = nil
	if c.sampleCancel != nil {
		c.sampleCancel()
		c.sampleCancel = nil
	}
	c.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = interruptProcess(cmd.Process)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-exited
	}
	c.logs.add("aegis", "info", "核心进程已停止")
}

// sample 订阅核心的流量与内存推送，保留最近五分钟。
func (c *Core) sample(ctx context.Context, client *Client) {
	var memory atomic.Int64
	go func() {
		for ctx.Err() == nil {
			_ = client.Stream(ctx, "/memory", func(line []byte) {
				var m struct {
					InUse int64 `json:"inuse"`
				}
				if json.Unmarshal(line, &m) == nil {
					memory.Store(m.InUse)
				}
			})
			sleepCtx(ctx, 3*time.Second)
		}
	}()
	for ctx.Err() == nil {
		_ = client.Stream(ctx, "/traffic", func(line []byte) {
			var t struct {
				Up   int64 `json:"up"`
				Down int64 `json:"down"`
			}
			if json.Unmarshal(line, &t) == nil {
				c.traffic.add(TrafficSample{Time: time.Now(), Up: t.Up, Down: t.Down, Memory: memory.Load()})
			}
		})
		sleepCtx(ctx, 3*time.Second)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// Logs 返回序号大于 after 的日志，最多 limit 条。
func (c *Core) Logs(after uint64, limit int) []LogLine { return c.logs.since(after, limit) }

// Traffic 最近的流量与内存采样（每秒一条）。
func (c *Core) Traffic() []TrafficSample { return c.traffic.all() }

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func statusLabel(status string) string {
	switch status {
	case StatusDisabled:
		return "未启用"
	case StatusMissing:
		return "未安装"
	case StatusInstalling:
		return "安装中"
	case StatusStarting:
		return "启动中"
	case StatusCrashed:
		return "异常退出"
	case StatusStopped:
		return "已停止"
	case StatusFailed:
		return "启动失败"
	default:
		return status
	}
}

var logLinePattern = regexp.MustCompile(`level=(\w+)\s+msg="((?:[^"\\]|\\.)*)"`)

// parseLogLine 解析核心的 logrus 文本格式：time="…" level=info msg="…"。
func parseLogLine(line string) (string, string) {
	if match := logLinePattern.FindStringSubmatch(line); match != nil {
		message, err := strconv.Unquote(`"` + match[2] + `"`)
		if err != nil {
			message = match[2]
		}
		return strings.ToLower(match[1]), message
	}
	return "info", strings.TrimSpace(line)
}

// summarizeOutput 从 -t 的输出里挑出错误行，避免把整段启动日志塞给管理员。
func summarizeOutput(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	picked := make([]string, 0, 4)
	for _, line := range lines {
		level, message := parseLogLine(line)
		if level == "error" || level == "fatal" || strings.Contains(strings.ToLower(message), "error") {
			picked = append(picked, message)
		}
	}
	if len(picked) == 0 && len(lines) > 0 {
		picked = lines[max(0, len(lines)-3):]
	}
	text := strings.Join(picked, "；")
	if len(text) > 600 {
		text = text[:600] + "…"
	}
	return text
}

// LogLine 一行日志。Source 为 core（核心输出）或 aegis（托管器自己的事件）。
type LogLine struct {
	Seq     uint64    `json:"seq"`
	Time    time.Time `json:"time"`
	Source  string    `json:"source"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type logRing struct {
	mu    sync.Mutex
	items []LogLine
	cap   int
	seq   uint64
}

func newLogRing(capacity int) *logRing { return &logRing{cap: capacity} }

func (r *logRing) add(source, level, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	r.items = append(r.items, LogLine{Seq: r.seq, Time: time.Now(), Source: source, Level: level, Message: message})
	if len(r.items) > r.cap {
		r.items = append([]LogLine(nil), r.items[len(r.items)-r.cap:]...)
	}
}

func (r *logRing) since(after uint64, limit int) []LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 || limit > r.cap {
		limit = r.cap
	}
	out := make([]LogLine, 0, limit)
	for _, item := range r.items {
		if item.Seq > after {
			out = append(out, item)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (r *logRing) lastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.items) - 1; i >= 0 && i >= len(r.items)-60; i-- {
		if r.items[i].Level == "error" || r.items[i].Level == "fatal" {
			return r.items[i].Message
		}
	}
	return "没有错误输出"
}

func (r *logRing) containsRecent(needle string, window int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.items) - 1; i >= 0 && i >= len(r.items)-window; i-- {
		if strings.Contains(r.items[i].Message, needle) {
			return true
		}
	}
	return false
}

// TrafficSample 每秒一条的速率采样（字节 / 秒）与当时的内存占用。
type TrafficSample struct {
	Time   time.Time `json:"time"`
	Up     int64     `json:"up"`
	Down   int64     `json:"down"`
	Memory int64     `json:"memory"`
}

type trafficRing struct {
	mu    sync.Mutex
	items []TrafficSample
	cap   int
}

func newTrafficRing(capacity int) *trafficRing { return &trafficRing{cap: capacity} }

func (r *trafficRing) add(sample TrafficSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, sample)
	if len(r.items) > r.cap {
		r.items = append([]TrafficSample(nil), r.items[len(r.items)-r.cap:]...)
	}
}

func (r *trafficRing) all() []TrafficSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TrafficSample(nil), r.items...)
}

func (r *trafficRing) last() (TrafficSample, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) == 0 {
		return TrafficSample{}, false
	}
	return r.items[len(r.items)-1], true
}
