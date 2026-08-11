package banner

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

// 以下三个变量供 -ldflags 覆盖：
//
//	go build -ldflags "\
//	  -X aegis/pkg/banner.Version=v1.4.0 \
//	  -X aegis/pkg/banner.Revision=$(git rev-parse HEAD) \
//	  -X aegis/pkg/banner.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" ./cmd/server
//
// 不覆盖也不影响：go build 会把 VCS 信息戳进 runtime/debug.BuildInfo，
// ReadBuildFacts 会自动读出来。ldflags 只是给「从 tarball 构建、没有 .git」的场景兜底。
var (
	Version   string
	Revision  string
	BuildTime string
)

// buildFactsTimeout 限制 gopsutil 采集的总时长。
// Windows 上 cpu.Info 走注册表 / WMI，容器里 disk.Usage 可能卡在坏挂载点上，
// 启动横幅没有任何理由为此阻塞进程启动——超时就少显示两行。
const factsTimeout = 2 * time.Second

// BuildFacts 编译期事实：这个二进制是什么、谁编的、什么时候编的。
type BuildFacts struct {
	Module    string    // 主模块路径
	Version   string    // 版本号（ldflags > BuildInfo > "dev"）
	Revision  string    // 完整 commit hash
	Dirty     bool      // 构建时工作区有未提交改动
	Time      time.Time // 构建时间（ldflags > vcs.time）
	GoVersion string
	OS        string
	Arch      string
	Compiler  string
	CGO       bool
	Deps      int // 直接 + 间接依赖模块数
}

// ReadBuildFacts 读取编译期事实。
// 优先级：ldflags 注入 > runtime/debug.BuildInfo > 运行期兜底。
func ReadBuildFacts() BuildFacts {
	f := BuildFacts{
		Version:   strings.TrimSpace(Version),
		Revision:  strings.TrimSpace(Revision),
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Compiler:  runtime.Compiler,
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(BuildTime)); err == nil {
		f.Time = t
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return f.normalize()
	}
	f.Module = info.Main.Path
	f.Deps = len(info.Deps)
	if f.Version == "" {
		f.Version = info.Main.Version
	}
	if info.GoVersion != "" {
		f.GoVersion = info.GoVersion
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if f.Revision == "" {
				f.Revision = s.Value
			}
		case "vcs.modified":
			f.Dirty = s.Value == "true"
		case "vcs.time":
			if f.Time.IsZero() {
				if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
					f.Time = t
				}
			}
		case "GOOS":
			f.OS = s.Value
		case "GOARCH":
			f.Arch = s.Value
		case "CGO_ENABLED":
			f.CGO = s.Value == "1"
		}
	}
	return f.normalize()
}

// pseudoVersionSuffix 匹配 Go 从 VCS 信息合成的伪版本尾巴。
// 三种基线写法的尾巴都是「时间戳-12 位短 hash」，只是前面的分隔符可能是 - 或 .：
//
//	v0.0.0-20260329193906-7ab26a5c607a          （无基线 tag）
//	v1.4.1-0.20260329193906-7ab26a5c607a        （基线是正式版）
//	v2.0.0-rc.1.0.20260329193906-7ab26a5c607a   （基线是预发布版）
//
// 后面还可能跟 +dirty / +incompatible。仓库没打过 tag 时 Main.Version 就是它，
// 和旁边那一列短提交完全重复。
var pseudoVersionSuffix = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}(\+|$)`)

func (f BuildFacts) normalize() BuildFacts {
	// go run / 未打 tag 的构建，Main.Version 是 "(devel)" 或一串伪版本，
	// 原样显示只会让人以为版本号读错了。这个二进制确实没有版本，就说 dev。
	if f.Version == "" || f.Version == "(devel)" || pseudoVersionSuffix.MatchString(f.Version) {
		f.Version = "dev"
	}
	return f
}

// ShortRevision 返回 7 位短 commit，带脏工作区标记。
func (f BuildFacts) ShortRevision() string {
	rev := f.Revision
	if rev == "" {
		return ""
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if f.Dirty {
		rev += "+dirty"
	}
	return rev
}

// Platform 返回 GOOS/GOARCH。
func (f BuildFacts) Platform() string { return f.OS + "/" + f.Arch }

// Release 返回「版本 (短提交)」形式的一行版本描述。
func (f BuildFacts) Release() string {
	if rev := f.ShortRevision(); rev != "" {
		return fmt.Sprintf("%s (%s)", f.Version, rev)
	}
	return f.Version
}

// RuntimeFacts 进程运行期事实。
type RuntimeFacts struct {
	PID         int
	Executable  string
	WorkingDir  string
	StartedAt   time.Time
	NumCPU      int
	GoMaxProcs  int
	Goroutines  int
	MemoryLimit int64  // GOMEMLIMIT；math.MaxInt64 表示未设置
	HeapAlloc   uint64 // 当前堆上活跃对象
	RSS         uint64 // 进程常驻内存（gopsutil，失败为 0）
}

// CollectRuntimeFacts 采集当前进程的运行期事实。
//
// GOMEMLIMIT 用 debug.SetMemoryLimit(-1) 读取——传负值是官方文档明确的「只读不改」用法。
// 这里刻意不报告 GOGC：runtime/debug 没有只读接口，唯一的读法是
// SetGCPercent 读回旧值再写回去，会和 MemoryManager 的自适应调优互相踩。
func CollectRuntimeFacts(ctx context.Context) RuntimeFacts {
	f := RuntimeFacts{
		PID:         os.Getpid(),
		NumCPU:      runtime.NumCPU(),
		GoMaxProcs:  runtime.GOMAXPROCS(0),
		Goroutines:  runtime.NumGoroutine(),
		MemoryLimit: debug.SetMemoryLimit(-1),
	}
	if exe, err := os.Executable(); err == nil {
		f.Executable = exe
	}
	if wd, err := os.Getwd(); err == nil {
		f.WorkingDir = wd
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	f.HeapAlloc = ms.HeapAlloc

	ctx, cancel := context.WithTimeout(ctx, factsTimeout)
	defer cancel()
	if p, err := process.NewProcessWithContext(ctx, int32(f.PID)); err == nil {
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			f.RSS = mi.RSS
		}
		if ct, err := p.CreateTimeWithContext(ctx); err == nil && ct > 0 {
			f.StartedAt = time.UnixMilli(ct)
		}
	}
	if f.StartedAt.IsZero() {
		f.StartedAt = time.Now()
	}
	return f
}

// HostFacts 宿主机事实（全部经 gopsutil 采集，单项失败时留零值不报错）。
type HostFacts struct {
	Hostname       string
	OS             string // 发行版 / 产品名，如 "Windows 11 Pro"、"ubuntu 24.04"
	Kernel         string
	KernelArch     string
	Virtualization string // gopsutil 探测到的虚拟化/容器运行时（docker、lxc、kvm…）
	BootTime       time.Time
	Uptime         time.Duration

	CPUModel    string
	CPUPhysical int
	CPULogical  int
	CPUMHz      float64

	MemTotal     uint64
	MemUsed      uint64
	MemAvailable uint64
	MemPercent   float64
	SwapTotal    uint64
	SwapUsed     uint64

	DiskPath    string
	DiskTotal   uint64
	DiskFree    uint64
	DiskPercent float64
}

// CollectHostFacts 采集宿主机事实。
// 整体受 factsTimeout 约束，任何一项失败都只是少一行，不会阻断启动。
func CollectHostFacts(ctx context.Context) HostFacts {
	ctx, cancel := context.WithTimeout(ctx, factsTimeout)
	defer cancel()

	f := HostFacts{}
	if info, err := host.InfoWithContext(ctx); err == nil && info != nil {
		f.Hostname = info.Hostname
		f.OS = strings.TrimSpace(strings.Join(nonEmpty(info.Platform, info.PlatformVersion), " "))
		if f.OS == "" {
			f.OS = info.OS
		}
		f.Kernel = info.KernelVersion
		f.KernelArch = info.KernelArch
		f.Virtualization = info.VirtualizationSystem
		if info.BootTime > 0 {
			f.BootTime = time.Unix(int64(info.BootTime), 0)
		}
		f.Uptime = time.Duration(info.Uptime) * time.Second
	}
	if f.Hostname == "" {
		f.Hostname, _ = os.Hostname()
	}

	if n, err := cpu.CountsWithContext(ctx, false); err == nil {
		f.CPUPhysical = n
	}
	if n, err := cpu.CountsWithContext(ctx, true); err == nil {
		f.CPULogical = n
	}
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		f.CPUModel = strings.TrimSpace(infos[0].ModelName)
		f.CPUMHz = infos[0].Mhz
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil && vm != nil {
		f.MemTotal, f.MemUsed, f.MemAvailable, f.MemPercent = vm.Total, vm.Used, vm.Available, vm.UsedPercent
	}
	if sm, err := mem.SwapMemoryWithContext(ctx); err == nil && sm != nil {
		f.SwapTotal, f.SwapUsed = sm.Total, sm.Used
	}

	// 磁盘看的是工作目录所在卷——进程真正会写崩溃日志、导出文件、GeoIP 库的地方
	if wd, err := os.Getwd(); err == nil {
		f.DiskPath = wd
		if usage, err := disk.UsageWithContext(ctx, wd); err == nil && usage != nil && usage.Total > 0 {
			f.DiskTotal, f.DiskFree, f.DiskPercent = usage.Total, usage.Free, usage.UsedPercent
		}
	}
	return f
}

// CPUSummary 返回「型号 | 物理/逻辑核 | 主频」的一行描述。
func (f HostFacts) CPUSummary() string {
	cores := ""
	switch {
	case f.CPUPhysical > 0 && f.CPULogical > 0 && f.CPUPhysical != f.CPULogical:
		cores = fmt.Sprintf("%d 核 %d 线程", f.CPUPhysical, f.CPULogical)
	case f.CPULogical > 0:
		cores = fmt.Sprintf("%d 线程", f.CPULogical)
	}
	freq := ""
	if f.CPUMHz > 0 {
		freq = fmt.Sprintf("%.2f GHz", f.CPUMHz/1000)
	}
	return Join(f.CPUModel, cores, freq)
}

// MemorySummary 返回「已用/总量 (百分比)」。
func (f HostFacts) MemorySummary() string {
	if f.MemTotal == 0 {
		return ""
	}
	return fmt.Sprintf("%s / %s (%.0f%%)", Bytes(f.MemUsed), Bytes(f.MemTotal), f.MemPercent)
}

// DiskSummary 返回工作目录所在卷的「可用/总量 (使用率)」。
func (f HostFacts) DiskSummary() string {
	if f.DiskTotal == 0 {
		return ""
	}
	return fmt.Sprintf("可用 %s / %s (%.0f%%)", Bytes(f.DiskFree), Bytes(f.DiskTotal), f.DiskPercent)
}

// OSSummary 返回「发行版 | 内核 | 架构 | 虚拟化」。
func (f HostFacts) OSSummary() string {
	virt := ""
	if f.Virtualization != "" {
		virt = "虚拟化 " + f.Virtualization
	}
	return Join(f.OS, f.Kernel, f.KernelArch, virt)
}

// ── 格式化助手（统一走 go-humanize，避免各处自己拼单位）──────────────────

// Bytes 人类可读的字节数（IEC，1024 进制）。
func Bytes(v uint64) string {
	if v == 0 {
		return "0 B"
	}
	return humanize.IBytes(v)
}

// Duration 把时长压成「3天4小时」这类紧凑写法。
// 不用 time.Duration.String()：那会输出 76h12m30.5s，对「已运行多久」并不好读。
func Duration(d time.Duration) string {
	if d <= 0 {
		return "-"
	}
	d = d.Round(time.Second)
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	seconds := int((d - time.Duration(minutes)*time.Minute) / time.Second)

	switch {
	case days > 0:
		return fmt.Sprintf("%d天%d小时", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d小时%d分", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%d分%d秒", minutes, seconds)
	default:
		return fmt.Sprintf("%d秒", seconds)
	}
}

// Elapsed 把启动耗时这类短时长格式化到合适精度。
func Elapsed(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Millisecond:
		return d.Round(time.Microsecond).String()
	case d < time.Second:
		return d.Round(time.Millisecond).String()
	default:
		return d.Round(10 * time.Millisecond).String()
	}
}

// TimeWithAgo 返回「2026-08-11 10:03:24 (3 天前)」。
func TimeWithAgo(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%s (%s)", t.Local().Format("2006-01-02 15:04:05"), humanize.Time(t))
}

// MemoryLimit 把 GOMEMLIMIT 渲染成可读值；未设置时返回「未设置」。
func MemoryLimit(limit int64) string {
	if limit <= 0 || limit == math.MaxInt64 {
		return "未设置"
	}
	return Bytes(uint64(limit))
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
