package bootstrap

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"aegis/internal/config"
	httptransport "aegis/internal/transport/http"
	"aegis/pkg/banner"
	"aegis/pkg/clientip"
	"aegis/pkg/egress"

	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
)

// 启动横幅的业务侧组装。
//
// 分工：pkg/banner 只管「怎么画」（艺术字、表格、着色、终端降级），
// 这里管「画什么」——把 config.Config 与装配完成后的真实连接状态
// 翻译成一组分区。两者分开，pkg/ 就不用认识任何业务配置结构。
//
// 打印分两次，因为「让人立刻看到进程活了」和「把事实交代清楚」是两件事：
//
//	PrintBootBanner  依赖装配之前，只有艺术字与身份行，零 I/O，瞬间出现
//	PrintReadyBanner 装配完成、开始服务之前，把连上的东西和生效的开关摊开
type Role string

const (
	RoleUnified Role = "unified"
	RoleAPI     Role = "api"
	RoleWorker  Role = "worker"
)

func (r Role) label() string {
	switch r {
	case RoleUnified:
		return "unified (API + Worker)"
	case RoleAPI:
		return "api (仅 HTTP 服务)"
	case RoleWorker:
		return "worker (仅事件消费)"
	default:
		return string(r)
	}
}

// servesHTTP 报告该角色是否对外提供 HTTP 服务，决定是否展示「入口」分区。
func (r Role) servesHTTP() bool { return r == RoleUnified || r == RoleAPI }

// BannerRuntime 汇总「装配完成后」才知道的事实。
// 各字段都允许为空——某个组件没装上就少显示一行，横幅不该因此报错。
type BannerRuntime struct {
	Role       Role
	Config     config.Config
	Elapsed    time.Duration // 从进程入口到装配完成的耗时
	Postgres   *pgxpool.Pool
	Redis      *redislib.Client
	NATS       *nats.Conn
	Temporal   client.Client
	Egress     *egress.Gateway
	OwnsEgress bool
	// Router 用来数「这个进程到底暴露了哪些接口」。
	// Worker 角色下为 nil，那时候「路由」分区整个不出现。
	Router *gin.Engine
	// ClientIP 客户端 IP 判定器。横幅要报的是**生效后的**判定方式
	// （含平台探测与预设展开的结果），照抄配置项达不到这个目的。
	ClientIP *clientip.Resolver
}

// BannerRuntimeOf 从装配完成的 APIApp 取横幅需要的运行期事实。
func (a *APIApp) BannerRuntimeOf(role Role, elapsed time.Duration) BannerRuntime {
	if a == nil {
		return BannerRuntime{Role: role, Elapsed: elapsed}
	}
	return BannerRuntime{
		Role:       role,
		Config:     a.Config,
		Elapsed:    elapsed,
		Postgres:   a.Postgres,
		Redis:      a.Redis,
		NATS:       a.NATSConn,
		Temporal:   a.Temporal,
		Egress:     a.EgressGateway,
		OwnsEgress: a.OwnsEgress,
		Router:     a.Router,
		ClientIP:   a.ClientIP,
	}
}

// BannerRuntimeOf 从装配完成的 WorkerApp 取横幅需要的运行期事实。
func (w *WorkerApp) BannerRuntimeOf(role Role, elapsed time.Duration) BannerRuntime {
	if w == nil {
		return BannerRuntime{Role: role, Elapsed: elapsed}
	}
	return BannerRuntime{
		Role:       role,
		Config:     w.Config,
		Elapsed:    elapsed,
		Postgres:   w.Postgres,
		Redis:      w.Redis,
		NATS:       w.NATSConn,
		Temporal:   w.Temporal,
		Egress:     w.EgressGateway,
		OwnsEgress: w.OwnsEgress,
	}
}

// PrintBootBanner 在依赖装配之前打印艺术字与身份行。
func PrintBootBanner(cfg config.Config, role Role) {
	banner.Print(bootBanner(cfg, role), bannerOptions(cfg, banner.StyleCompact))
}

// RenderBootBanner 渲染启动横幅为字符串（供测试与复用）。
func RenderBootBanner(cfg config.Config, role Role) string {
	return banner.Render(bootBanner(cfg, role), bannerOptions(cfg, banner.StyleCompact))
}

func bootBanner(cfg config.Config, role Role) banner.Banner {
	build := banner.ReadBuildFacts()
	return banner.Banner{
		Logo: "AEGIS",
		Tagline: banner.JoinText(
			"多租户用户系统平台",
			role.label(),
			cfg.AppEnv,
			build.Release(),
		),
	}
}

// PrintReadyBanner 在开始对外服务之前打印完整的运行时明细。
func PrintReadyBanner(ctx context.Context, rt BannerRuntime) {
	banner.Print(readyBanner(ctx, rt), bannerOptions(rt.Config, banner.StyleFull))
}

// RenderReadyBanner 渲染运行时横幅为字符串（供测试与复用）。
func RenderReadyBanner(ctx context.Context, rt BannerRuntime) string {
	return banner.Render(readyBanner(ctx, rt), bannerOptions(rt.Config, banner.StyleFull))
}

// readyBanner 组装运行时横幅。艺术字已经由 PrintBootBanner 打过，这里只补摘要与表格。
func readyBanner(ctx context.Context, rt BannerRuntime) banner.Banner {
	cfg := rt.Config
	build := banner.ReadBuildFacts()
	proc := banner.CollectRuntimeFacts(ctx)

	var hostFacts banner.HostFacts
	if cfg.Banner.ShowHost {
		hostFacts = banner.CollectHostFacts(ctx)
	}

	return banner.Banner{
		Title:      fmt.Sprintf("%s 运行时", displayAppName(cfg)),
		Highlights: bannerHighlights(rt, build, proc),
		Sections:   bannerSections(rt, build, proc, hostFacts),
		Footer:     bannerFooter(rt),
	}
}

// bannerOptions 把配置翻译成渲染参数。
// fallback 是 BANNER_STYLE=auto 时该档横幅想要的形态。
func bannerOptions(cfg config.Config, fallback banner.Style) banner.Options {
	style := banner.Style(cfg.Banner.Style)
	if !cfg.Banner.Enabled {
		style = banner.StyleOff
	}
	switch style {
	case banner.StyleAuto, "":
		style = fallback
	case banner.StyleFull, banner.StyleCompact, banner.StyleMinimal, banner.StyleOff:
		// 显式指定，按指定的来；但 compact 档的横幅不该因为配了 full 而多长出表格
		if style == banner.StyleFull && fallback == banner.StyleCompact {
			style = banner.StyleCompact
		}
	default:
		// 配了个不认识的值，别静默当成关闭——那会让人以为横幅坏了
		style = fallback
	}
	return banner.Options{
		Style: style,
		Font:  cfg.Banner.Font,
		Color: banner.ColorMode(cfg.Banner.Color),
		Width: cfg.Banner.Width,
	}
}

func displayAppName(cfg config.Config) string {
	if name := strings.TrimSpace(cfg.AppName); name != "" {
		return name
	}
	return "Aegis"
}

// ── 摘要行 ─────────────────────────────────────────────────────────────

func bannerHighlights(rt BannerRuntime, build banner.BuildFacts, proc banner.RuntimeFacts) []string {
	lines := []string{
		banner.JoinText(
			"版本 "+build.Release(),
			build.GoVersion+" "+build.Platform(),
			fmt.Sprintf("PID %d", proc.PID),
		),
	}
	second := []string{}
	if rt.Role.servesHTTP() {
		second = append(second, "监听 "+listenURL(rt.Config))
	}
	if rt.Elapsed > 0 {
		second = append(second, "装配耗时 "+banner.Elapsed(rt.Elapsed))
	}
	second = append(second, "环境 "+rt.Config.AppEnv)
	lines = append(lines, banner.JoinText(second...))
	return lines
}

// listenURL 拼出可以直接点开的监听地址。
// 服务端实际 bind 的是 ":port"（所有网卡），这里显示 127.0.0.1 是因为
// 横幅是给运行它的人看的，本机地址才是那个能点开的链接。
func listenURL(cfg config.Config) string {
	return fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)
}

// ── 分区 ───────────────────────────────────────────────────────────────

func bannerSections(rt BannerRuntime, build banner.BuildFacts, proc banner.RuntimeFacts, hostFacts banner.HostFacts) []banner.Section {
	sections := []banner.Section{
		buildSection(build),
		processSection(rt, proc),
	}
	if rt.Config.Banner.ShowHost {
		sections = append(sections, hostSection(hostFacts, proc))
	}
	sections = append(sections,
		dataPlaneSection(rt),
		securitySection(rt),
		capabilitySection(rt),
	)
	if rt.Role.servesHTTP() {
		sections = append(sections, endpointSection(rt))
		if section, ok := routeSection(rt); ok {
			sections = append(sections, section)
		}
	}
	return sections
}

// routeSection 按顶层命名空间交代这个进程暴露了什么。
//
// 这里刻意只给**每个顶层域一行**，而不是把清单摊开：
// gin 在 debug 档原本会把近千条路由逐行打出来，那正是这次要消掉的东西，
// 在横幅里换个更漂亮的方式再打一遍毫无意义。完整清单归 `routes` 子命令。
//
// 分区随 BANNER_SHOW_ROUTES 开关，以及只在对外提供 HTTP 的角色下出现——
// Worker 不监听端口，列出「暴露了哪些接口」等于指引人去访问一个不存在的服务。
func routeSection(rt BannerRuntime) (banner.Section, bool) {
	if !rt.Config.Banner.ShowRoutes || rt.Router == nil {
		return banner.Section{}, false
	}
	inventory := httptransport.RouteInventory(rt.Router)
	realms := inventory.Realms()
	if len(realms) == 0 {
		return banner.Section{}, false
	}

	fields := make([]banner.Field, 0, len(realms)+1)
	for _, realm := range realms {
		fields = append(fields, banner.Field{
			Key:   realm.Realm,
			Value: banner.Countf("%d 条 / %d 组", realm.Routes, realm.Groups),
			Note:  banner.Join(realm.Auth...),
		})
	}
	fields = append(fields, banner.Field{
		Key:   "合计",
		Value: banner.Countf("%d 条 / %d 组", inventory.Total(), len(inventory.Groups)),
		State: banner.StateOK,
		Note:  "完整清单：go run ./cmd/server routes",
	})
	return banner.Section{Title: "路由", Fields: fields}, true
}

func buildSection(build banner.BuildFacts) banner.Section {
	cgo := "CGO 关闭"
	if build.CGO {
		cgo = "CGO 开启"
	}
	return banner.Section{
		Title: "构建",
		Fields: []banner.Field{
			{Key: "版本", Value: build.Version, Note: build.Module},
			{
				Key:   "提交",
				Value: banner.Fallback(build.ShortRevision(), "未知（无 VCS 戳）"),
				Note:  banner.Fallback(banner.TimeWithAgo(build.Time), "构建时间未知"),
			},
			{
				Key:   "工具链",
				Value: banner.Join(build.GoVersion, build.Platform()),
				Note:  banner.Join(build.Compiler, cgo, banner.Countf("依赖模块 %d", build.Deps)),
			},
		},
	}
}

func processSection(rt BannerRuntime, proc banner.RuntimeFacts) banner.Section {
	cfg := rt.Config
	// gin 的运行模式由 APP_ENV 推导（见 initGinMode），一并显示省得再去翻代码
	ginMode := "gin debug"
	if strings.EqualFold(cfg.AppEnv, "production") {
		ginMode = "gin release"
	}
	return banner.Section{
		Title: "进程",
		Fields: []banner.Field{
			{Key: "角色", Value: rt.Role.label(), Note: banner.Countf("PID %d", proc.PID)},
			{Key: "环境", Value: banner.Fallback(cfg.AppEnv, "未设置"), Note: banner.Join("APP_ENV", ginMode)},
			{
				Key:   "调度",
				Value: banner.Countf("GOMAXPROCS %d / %d 逻辑核", proc.GoMaxProcs, proc.NumCPU),
				Note:  banner.Countf("goroutine %d", proc.Goroutines),
			},
			{
				Key:   "内存",
				Value: banner.Join("堆 "+banner.Bytes(proc.HeapAlloc), "RSS "+banner.Bytes(proc.RSS)),
				Note:  "GOMEMLIMIT " + banner.MemoryLimit(proc.MemoryLimit),
			},
			{Key: "时区", Value: banner.Fallback(cfg.DefaultTimezone, time.Local.String()), Note: "APP_DEFAULT_IANA_TIMEZONE"},
			{Key: "工作目录", Value: proc.WorkingDir, Note: proc.Executable},
		},
	}
}

func hostSection(h banner.HostFacts, proc banner.RuntimeFacts) banner.Section {
	// 内存与磁盘水位到 90% 就该在启动时被看见，而不是等半夜写不进崩溃日志才发现
	memState := banner.StateNeutral
	if h.MemPercent >= 90 {
		memState = banner.StateWarn
	}
	diskState := banner.StateNeutral
	if h.DiskPercent >= 90 {
		diskState = banner.StateWarn
	}
	swap := ""
	if h.SwapTotal > 0 {
		swap = banner.Countf("Swap %s / %s", banner.Bytes(h.SwapUsed), banner.Bytes(h.SwapTotal))
	}
	return banner.Section{
		Title: "主机",
		Fields: []banner.Field{
			{Key: "主机名", Value: banner.Fallback(h.Hostname, "未知"), Note: h.OSSummary()},
			{Key: "CPU", Value: banner.Fallback(h.CPUSummary(), "未知"), Note: banner.Countf("GOMAXPROCS %d", proc.GoMaxProcs)},
			{Key: "物理内存", Value: banner.Fallback(h.MemorySummary(), "未知"), State: memState, Note: swap},
			{Key: "磁盘", Value: banner.Fallback(h.DiskSummary(), "未知"), State: diskState, Note: h.DiskPath},
			{Key: "开机", Value: banner.Duration(h.Uptime), Note: banner.TimeWithAgo(h.BootTime)},
		},
	}
}

func dataPlaneSection(rt BannerRuntime) banner.Section {
	cfg := rt.Config
	fields := []banner.Field{
		postgresField(rt),
		redisField(rt),
		natsField(rt),
		{
			Key:   "Temporal",
			Value: banner.Fallback(cfg.Temporal.HostPort, "未配置"),
			State: stateOf(rt.Temporal != nil),
			Note:  banner.Join("命名空间 "+cfg.Temporal.Namespace, "队列 "+cfg.Temporal.TaskQueue),
		},
	}

	// 遗留 MySQL 只在迁移命令里用，没配是常态，不该显示成故障
	legacy := banner.Field{Key: "遗留 MySQL", Value: "未配置", State: banner.StateOff, Note: "仅 sync-legacy-* 命令需要"}
	if dsn := strings.TrimSpace(cfg.LegacyMySQL.DSN); dsn != "" {
		legacy = banner.Field{
			Key:   "遗留 MySQL",
			Value: banner.Fallback(mysqlSummary(dsn), "已配置"),
			State: banner.StateNeutral,
			Note:  banner.Countf("批量 %d / 并发 %d", cfg.LegacyMySQL.BatchSize, cfg.LegacyMySQL.Concurrency),
		}
	}
	return banner.Section{Title: "数据面", Fields: append(fields, legacy)}
}

func postgresField(rt BannerRuntime) banner.Field {
	cfg := rt.Config
	f := banner.Field{
		Key:   "PostgreSQL",
		Value: banner.Fallback(postgresSummary(cfg.Postgres.DSN), "未配置"),
		State: stateOf(rt.Postgres != nil),
	}
	if rt.Postgres == nil {
		f.Note = banner.Countf("连接池 %d-%d", cfg.Postgres.MinConns, cfg.Postgres.MaxConns)
		return f
	}
	stat := rt.Postgres.Stat()
	f.Note = banner.Join(
		banner.Countf("连接 %d/%d（空闲 %d）", stat.TotalConns(), stat.MaxConns(), stat.IdleConns()),
		banner.Fallback(cfg.Postgres.ApplicationName, ""),
	)
	return f
}

func redisField(rt BannerRuntime) banner.Field {
	cfg := rt.Config
	f := banner.Field{
		Key:   "Redis",
		Value: banner.Fallback(cfg.Redis.Addr, "未配置"),
		State: stateOf(rt.Redis != nil),
		Note:  banner.Join(banner.Countf("db %d", cfg.Redis.DB), "键前缀 "+cfg.Redis.KeyPrefix),
	}
	if rt.Redis != nil {
		stat := rt.Redis.PoolStats()
		f.Note = banner.Join(
			banner.Countf("db %d", cfg.Redis.DB),
			"键前缀 "+cfg.Redis.KeyPrefix,
			banner.Countf("连接 %d（空闲 %d）", stat.TotalConns, stat.IdleConns),
		)
	}
	return f
}

func natsField(rt BannerRuntime) banner.Field {
	cfg := rt.Config
	f := banner.Field{
		Key:   "NATS",
		Value: banner.Fallback(redactURL(cfg.NATS.URL), "未配置"),
		State: stateOf(rt.NATS != nil),
		Note:  "流 " + cfg.NATS.StreamName,
	}
	// 连上之后以服务端自报的地址与版本为准：配的是集群地址时，实际连的是哪台很重要
	if rt.NATS != nil && rt.NATS.IsConnected() {
		f.Value = banner.Fallback(redactURL(rt.NATS.ConnectedUrl()), f.Value)
		f.Note = banner.Join(
			"流 "+cfg.NATS.StreamName,
			banner.Fallback(rt.NATS.ConnectedServerVersion(), ""),
		)
	} else if rt.NATS != nil {
		f.State = banner.StateWarn
		f.Note = banner.Join("流 "+cfg.NATS.StreamName, "当前未连接，等待重连")
	}
	return f
}

func securitySection(rt BannerRuntime) banner.Section {
	cfg := rt.Config
	fields := []banner.Field{firewallField(cfg), jwtField(cfg)}

	if rt.Role.servesHTTP() {
		fields = append(fields, corsField(cfg), clientIPField(rt))
	}

	replay := "已关闭"
	replayState := banner.StateOff
	if cfg.ReplayProtection.Enabled {
		replayState = banner.StateOK
		replay = banner.Join(
			"已启用",
			"窗口 "+cfg.ReplayProtection.NonceWindow.String(),
			"容差 "+cfg.ReplayProtection.NonceSkew.String(),
		)
	}
	signature := "签名校验关闭"
	if cfg.ReplayProtection.SignatureEnabled {
		signature = "签名校验开启"
	}
	fields = append(fields, banner.Field{Key: "重放防护", Value: replay, State: replayState, Note: signature})

	modules := make([]string, 0, 3)
	if cfg.Security.Modules.TOTPEnabled {
		modules = append(modules, "TOTP")
	}
	if cfg.Security.Modules.RecoveryCodesEnabled {
		modules = append(modules, "恢复码")
	}
	if cfg.Security.Modules.PasskeyEnabled {
		modules = append(modules, "Passkey")
	}
	mfaValue, mfaState := banner.Enabled(len(modules) > 0, banner.Join(modules...), "全部关闭")
	fields = append(fields, banner.Field{Key: "多因子", Value: mfaValue, State: mfaState, Note: "SECURITY_* 模块开关"})

	return banner.Section{Title: "安全", Fields: fields}
}

func firewallField(cfg config.Config) banner.Field {
	if !cfg.Firewall.Enabled {
		// 防火墙是这套系统的第一道闸，关掉不是「配置项为 false」这么中性的事
		return banner.Field{Key: "防火墙", Value: "已关闭", State: banner.StateWarn, Note: "FIREWALL_ENABLED=false"}
	}
	waf := "WAF 关闭"
	if cfg.Firewall.CorazaEnabled {
		mode := "拦截"
		if cfg.Firewall.CorazaDetectionOnly {
			mode = "仅检测"
		}
		waf = banner.Join(
			"Coraza + CRS v4",
			banner.Countf("paranoia %d", cfg.Firewall.CorazaParanoia),
			banner.Countf("异常分 %d", cfg.Firewall.CorazaAnomalyThreshold),
			mode,
		)
	}
	return banner.Field{
		Key:   "防火墙",
		Value: banner.Join("已启用", "全局 "+cfg.Firewall.GlobalRate, "认证 "+cfg.Firewall.AuthRate),
		State: banner.StateOK,
		Note:  waf,
	}
}

func jwtField(cfg config.Config) banner.Field {
	// 只看「是不是还在用示例值」，不打印任何密钥内容
	state := banner.StateOK
	note := banner.Join("签发者 "+cfg.JWT.Issuer, "刷新 "+cfg.JWT.RefreshTTL.String())
	value := banner.Join("访问令牌 "+cfg.JWT.TTL.String(), "HS256")
	if isPlaceholderSecret(cfg.JWT.Secret) {
		state = banner.StateWarn
		note = "仍在使用示例密钥，上线前必须更换 JWT_SECRET"
	}
	return banner.Field{Key: "JWT", Value: value, State: state, Note: note}
}

func corsField(cfg config.Config) banner.Field {
	if !cfg.CORS.Enabled {
		return banner.Field{Key: "CORS", Value: "已关闭", State: banner.StateOff, Note: "同源部署无需开启"}
	}
	if cfg.CORS.AllowAllOrigins {
		return banner.Field{Key: "CORS", Value: "放行全部来源", State: banner.StateWarn, Note: "CORS_ALLOW_ALL_ORIGINS=true"}
	}
	return banner.Field{
		Key:   "CORS",
		Value: banner.Countf("放行 %d 个来源", len(cfg.CORS.AllowOrigins)),
		State: banner.StateOK,
		Note:  banner.Join(cfg.CORS.AllowOrigins...),
	}
}

func capabilitySection(rt BannerRuntime) banner.Section {
	cfg := rt.Config
	fields := []banner.Field{tracingField(cfg), egressField(rt)}

	geo, geoState := banner.Toggle(cfg.GeoIP.Enabled)
	fields = append(fields, banner.Field{
		Key:   "GeoIP",
		Value: geo,
		State: geoState,
		Note:  banner.Join(cfg.GeoIP.DatabaseDir, "自动更新 "+cfg.GeoIP.UpdateInterval.String()),
	})

	risk, riskState := banner.Toggle(cfg.GeoRisk.Enabled)
	// GEORISK_MAX_SPEED_KMH 留空时由 GeoRiskService 回退到内置默认，
	// 照抄配置里的 0 会显示成「不可能旅行 > 0 km/h」——那是句假话
	speedNote := banner.Countf("不可能旅行 > %.0f km/h", cfg.GeoRisk.MaxSpeedKMH)
	if cfg.GeoRisk.MaxSpeedKMH <= 0 {
		speedNote = "不可能旅行阈值取服务内置默认"
	}
	fields = append(fields, banner.Field{
		Key:   "地理风控",
		Value: risk,
		State: riskState,
		Note:  speedNote,
	})

	autoSign, autoSignState := banner.Toggle(cfg.AutoSign.Enabled)
	fields = append(fields, banner.Field{
		Key:   "自动签到",
		Value: autoSign,
		State: autoSignState,
		Note:  banner.Join("周期 "+cfg.AutoSign.TickInterval.String(), "重建 "+cfg.AutoSign.RebuildInterval.String()),
	})

	gc, gcState := banner.Enabled(cfg.Memory.GCAutoTune, "自适应 GC", "固定 GOGC")
	fields = append(fields, banner.Field{
		Key:   "内存治理",
		Value: gc,
		State: gcState,
		Note:  banner.Join("采集 "+cfg.Memory.MonitorInterval.String(), leakLabel(cfg.Memory.LeakDetection)),
	})

	dbMon, dbMonState := banner.Toggle(cfg.Database.MonitorEnabled)
	fields = append(fields, banner.Field{
		Key:   "连接池巡检",
		Value: dbMon,
		State: dbMonState,
		Note:  banner.Join("慢查询 > "+cfg.Database.SlowQueryThreshold.String(), leakLabel(cfg.Database.LeakDetection)),
	})

	if n := configuredOAuthProviders(cfg); n > 0 {
		fields = append(fields, banner.Field{
			Key:   "第三方登录",
			Value: banner.Countf("%d 个平台级渠道", n),
			State: banner.StateOK,
			Note:  "应用级渠道以后台配置为准",
		})
	}

	fields = append(fields, banner.Field{
		Key:   "崩溃日志",
		Value: cfg.CrashLog.Dir,
		State: banner.StateOK,
		Note:  banner.Countf("保留 %d 份 / 单份上限 %s", cfg.CrashLog.MaxFiles, banner.Bytes(uint64(cfg.CrashLog.MaxSize))),
	})

	return banner.Section{Title: "能力", Fields: fields}
}

func leakLabel(on bool) string {
	if on {
		return "泄漏检测开"
	}
	return "泄漏检测关"
}

// tracingField 报告追踪的**实际去向**而不是 TRACING_ENABLED 的字面值。
// TRACING_ENDPOINT 没配时 setDefaults 会把 exporter 落成 none，
// 此时 enabled=true 也只是空 exporter——把它显示成「已启用」等于谎报已接入。
func tracingField(cfg config.Config) banner.Field {
	exporter := cfg.Tracing.Exporter
	if !cfg.Tracing.Enabled {
		return banner.Field{Key: "分布式追踪", Value: "已关闭", State: banner.StateOff, Note: "TRACING_ENABLED=false"}
	}
	if exporter == "" || exporter == "none" {
		return banner.Field{
			Key:   "分布式追踪",
			Value: "空 exporter（span 不落地）",
			State: banner.StateOff,
			Note:  "配置 TRACING_ENDPOINT 或 TRACING_EXPORTER 后生效",
		}
	}
	state := banner.StateOK
	note := banner.Fallback(cfg.Tracing.Endpoint, "")
	// 配了 OTLP 出口却没有地址，span 会攒在内存里再丢掉，比明确关闭更容易被误判成已接入
	if exporter != "stdout" && cfg.Tracing.Endpoint == "" {
		state = banner.StateWarn
		note = "exporter 已配但缺少 endpoint，span 不会送达"
	}
	return banner.Field{
		Key:   "分布式追踪",
		Value: banner.Join(exporter, cfg.Tracing.Sampler, banner.Countf("采样 %.0f%%", cfg.Tracing.SampleRatio*100)),
		State: state,
		Note:  note,
	}
}

func egressField(rt BannerRuntime) banner.Field {
	cfg := rt.Config
	if !cfg.Egress.Enabled {
		return banner.Field{Key: "出海网关", Value: "已关闭", State: banner.StateOff, Note: "全部出站直连"}
	}
	owner := "本进程持有"
	if !rt.OwnsEgress {
		owner = "复用进程内单例"
	}
	f := banner.Field{
		Key:   "出海网关",
		Value: banner.Countf("%d 端点 / %d 规则", len(cfg.Egress.Endpoints), len(cfg.Egress.Rules)),
		State: banner.StateOK,
		Note:  banner.Join("默认动作 "+string(cfg.Egress.DefaultAction), owner),
	}
	if rt.Egress == nil {
		return f
	}
	// 健康探测刚起步时全部端点都还没探过，这里报的是「当前已知健康数」，不是最终态
	stats := rt.Egress.Stats()
	healthy := 0
	for _, e := range stats.Endpoints {
		if e.Enabled && e.Healthy {
			healthy++
		}
	}
	f.Value = banner.Countf("%d/%d 端点健康 / %d 规则", healthy, len(stats.Endpoints), len(stats.Rules))
	if healthy == 0 && len(stats.Endpoints) > 0 {
		f.State = banner.StateWarn
	}
	f.Note = banner.Join("默认动作 "+string(stats.DefaultAction), "来源 "+banner.Fallback(stats.Source, "env"), owner)
	return f
}

func endpointSection(rt BannerRuntime) banner.Section {
	cfg := rt.Config
	base := listenURL(cfg)
	return banner.Section{
		Title: "入口",
		Fields: []banner.Field{
			{
				Key:   "HTTP",
				Value: base,
				State: banner.StateOK,
				Note:  banner.Join("读 "+cfg.ReadTimeout.String(), "写 "+cfg.WriteTimeout.String(), "关停 "+cfg.ShutdownTimeout.String()),
			},
			{Key: "健康检查", Value: base + "/healthz", Note: "存活探针"},
			{Key: "就绪探针", Value: base + "/readyz", Note: "依赖连通性"},
			{Key: "OpenAPI", Value: base + "/openapi.json", Note: "go run ./cmd/server openapi 可导出"},
			{Key: "开发者门户", Value: absoluteOrRelative(base, cfg.DocsPortalURL), Note: "DOCS_PORTAL_URL"},
			{
				Key:   "管理控制台",
				Value: banner.Fallback(cfg.ConsoleBaseURL, "未配置"),
				State: stateOf(strings.TrimSpace(cfg.ConsoleBaseURL) != ""),
				Note:  "通知深链依赖它，分域部署必须填绝对地址",
			},
		},
	}
}

func bannerFooter(rt BannerRuntime) []string {
	lines := []string{}
	if rt.Role.servesHTTP() {
		lines = append(lines, banner.JoinText(
			"就绪  "+listenURL(rt.Config),
			"健康 /healthz",
			"就绪 /readyz",
			"文档 "+rt.Config.DocsPortalURL,
		))
	}
	shutdown := rt.Config.ShutdownTimeout
	if shutdown <= 0 {
		shutdown = 15 * time.Second
	}
	lines = append(lines, banner.JoinText(
		"停止  Ctrl+C 优雅关闭（最长等待 "+shutdown.String()+"）",
		"横幅  BANNER_STYLE=off 关闭",
	))
	return lines
}

// ── 解析助手（一律交给对应驱动自带的解析器，不手写字符串切割）───────────

// postgresSummary 从 DSN 提取 host:port/database。
// 用 pgx 自己的 pgconn.ParseConfig：它认识 URL 与 keyword/value 两种写法，
// 也认识 PGHOST 之类的环境兜底，手写切割迟早会在某种写法上翻车；
// 更关键的是，只取这三个字段就天然不会把密码打到终端上。
func postgresSummary(dsn string) string {
	if strings.TrimSpace(dsn) == "" {
		return ""
	}
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil || cfg == nil {
		return ""
	}
	summary := fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
	if cfg.User != "" {
		summary = cfg.User + "@" + summary
	}
	return summary
}

// mysqlSummary 同理，交给 go-sql-driver 自己的 DSN 解析器。
func mysqlSummary(dsn string) string {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil || cfg == nil {
		return ""
	}
	summary := cfg.Addr
	if cfg.DBName != "" {
		summary += "/" + cfg.DBName
	}
	if cfg.User != "" {
		summary = cfg.User + "@" + summary
	}
	return summary
}

// redactURL 去掉 URL 里的凭据。NATS 允许 nats://user:pass@host 这种写法。
func redactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User(u.User.Username())
	return u.String()
}

// absoluteOrRelative 把相对路径的门户地址补成可点击的绝对地址。
func absoluteOrRelative(base, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return "未配置"
	}
	if strings.HasPrefix(target, "/") {
		return base + target
	}
	return target
}

// placeholderSecrets 是 .env.example 与各类教程里的占位值。
// 刻意不含裸的 "secret" / "example"：真实密钥里凑巧带上这两个词并不罕见，
// 每次启动都误报一次「密钥没换」，最后的结果是所有人都不看这一行了。
var placeholderSecrets = []string{
	"change-me", "changeme", "change_me", "changethis",
	"your-secret", "yoursecret", "your_secret",
	"please-change", "replace-me", "placeholder", "todo",
}

// isPlaceholderSecret 判断密钥是否还是占位值或明显过短。
// HS256 的密钥短于 32 字节就谈不上安全强度，这一条比关键词匹配更有价值。
func isPlaceholderSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 {
		return true
	}
	lowered := strings.ToLower(secret)
	for _, placeholder := range placeholderSecrets {
		if strings.Contains(lowered, placeholder) {
			return true
		}
	}
	return false
}

func configuredOAuthProviders(cfg config.Config) int {
	n := 0
	for _, provider := range cfg.OAuth {
		if strings.TrimSpace(provider.ClientID) != "" {
			n++
		}
	}
	return n
}

func stateOf(ok bool) banner.State {
	if ok {
		return banner.StateOK
	}
	return banner.StateOff
}
