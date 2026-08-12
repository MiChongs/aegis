package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/pkg/clientip"
	"aegis/pkg/egress"

	"github.com/gin-gonic/gin"
)

// bannerTestSecrets 是横幅绝对不能打出来的东西。
// 值都取成足够独特的字符串，出现在输出里就一定是泄漏而不是巧合。
var bannerTestSecrets = []string{
	"pgpass-must-not-leak",
	"redispass-must-not-leak",
	"mysqlpass-must-not-leak",
	"natspass-must-not-leak",
	"jwtsecret-must-not-leak-and-is-long-enough-to-pass",
	"masterkey-must-not-leak",
	"oauthsecret-must-not-leak",
}

func bannerTestConfig() config.Config {
	return config.Config{
		AppName:         "Aegis",
		AppEnv:          "production",
		HTTPPort:        8088,
		DocsPortalURL:   "/developers",
		ConsoleBaseURL:  "https://console.example.com",
		DefaultTimezone: "Asia/Shanghai",
		ClientIP:        clientip.Config{TrustedProxies: []string{"127.0.0.1/32"}},
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    30 * time.Second,
		ShutdownTimeout: 15 * time.Second,
		Banner:          config.DefaultBannerConfig(),
		JWT: config.JWTConfig{
			Secret:     "jwtsecret-must-not-leak-and-is-long-enough-to-pass",
			Issuer:     "aegis",
			TTL:        720 * time.Hour,
			RefreshTTL: 168 * time.Hour,
		},
		CORS: config.CORSConfig{Enabled: true, AllowOrigins: []string{"https://console.example.com"}},
		Firewall: config.FirewallConfig{
			Enabled: true, GlobalRate: "600-M", AuthRate: "20-M",
			CorazaEnabled: true, CorazaParanoia: 1, CorazaAnomalyThreshold: 40,
		},
		Postgres: config.PostgresConfig{
			DSN:             "postgres://aegis:pgpass-must-not-leak@db.internal:5432/aegis?sslmode=disable",
			MinConns:        2,
			MaxConns:        10,
			ApplicationName: "aegis-api",
		},
		Database:    config.DatabaseConfig{MonitorEnabled: true, SlowQueryThreshold: time.Second, LeakDetection: true},
		LegacyMySQL: config.LegacyMySQLConfig{DSN: "legacy:mysqlpass-must-not-leak@tcp(mysql.internal:3306)/legacy", BatchSize: 200, Concurrency: 4},
		Redis:       config.RedisConfig{Addr: "redis.internal:6379", Password: "redispass-must-not-leak", DB: 3, KeyPrefix: "aegis"},
		NATS:        config.NATSConfig{URL: "nats://aegis:natspass-must-not-leak@nats.internal:4222", StreamName: "AEGIS_EVENTS"},
		Temporal:    config.TemporalConfig{HostPort: "temporal.internal:7233", Namespace: "default", TaskQueue: "aegis-workflow"},
		GeoIP:       config.GeoIPConfig{Enabled: true, DatabaseDir: ".runtime/geoip", UpdateInterval: 168 * time.Hour},
		GeoRisk:     config.GeoRiskConfig{Enabled: true, MaxSpeedKMH: 900},
		AutoSign:    config.AutoSignConfig{Enabled: true, TickInterval: time.Minute, RebuildInterval: 15 * time.Minute},
		Memory:      config.MemoryConfig{GCAutoTune: true, MonitorInterval: 15 * time.Second, LeakDetection: true},
		CrashLog:    config.CrashLogConfig{Dir: "data/crashlogs", MaxFiles: 20, MaxSize: 50 << 20},
		Tracing:     config.TracingConfig{Enabled: true, Exporter: "otlp-http", Sampler: "parentbased_always_on", SampleRatio: 1, Endpoint: "http://otel:4318"},
		ReplayProtection: config.ReplayProtectionConfig{
			Enabled: true, NonceWindow: 5 * time.Minute, NonceSkew: 30 * time.Second, SignatureEnabled: true,
		},
		Security: config.SecurityConfig{
			MasterKey: "masterkey-must-not-leak",
			Modules:   config.SecurityModulesConfig{TOTPEnabled: true, RecoveryCodesEnabled: true},
		},
		Egress: egress.Config{
			Enabled:       true,
			DefaultAction: egress.ActionDirect,
			Endpoints:     []egress.EndpointConfig{{Name: "hk", Protocol: egress.ProtocolSOCKS5, Address: "hk.example.com:1080"}},
			Rules:         []egress.RuleConfig{{Name: "stripe", Match: egress.MatchConfig{DomainSuffixes: []string{"stripe.com"}}}},
		},
		OAuth: map[string]config.OAuthProviderConfig{
			"github": {Name: "github", ClientID: "gh-client", ClientSecret: "oauthsecret-must-not-leak"},
		},
	}
}

func renderTestBanner(t *testing.T, role Role) string {
	t.Helper()
	cfg := bannerTestConfig()
	cfg.Banner.Width = 120
	cfg.Banner.Color = "never"
	// 平台探测显式喂空：不这么做的话，横幅输出会随「跑测试的这台机器上恰好有哪些
	// 环境变量」变化（CI 在 Kubernetes 里就会多出一行探测结果）。
	resolver, err := clientip.NewWithEnv(cfg.ClientIP, func(string) string { return "" })
	if err != nil {
		t.Fatalf("clientip.NewWithEnv() error = %v", err)
	}
	return RenderReadyBanner(context.Background(), BannerRuntime{
		Role:     role,
		Config:   cfg,
		ClientIP: resolver,
		Elapsed:  1234 * time.Millisecond,
	})
}

// TestReadyBannerNeverLeaksSecrets 横幅打在终端上、也会进日志采集与截图。
// 任何一个密码 / 密钥出现在里面都是事故，因此这条测试比排版更重要。
func TestReadyBannerNeverLeaksSecrets(t *testing.T) {
	for _, role := range []Role{RoleUnified, RoleAPI, RoleWorker} {
		out := renderTestBanner(t, role)
		for _, secret := range bannerTestSecrets {
			if strings.Contains(out, secret) {
				t.Errorf("role=%s 横幅泄漏了敏感值 %q", role, secret)
			}
		}
	}
}

// TestReadyBannerShowsConnectionTargets 脱敏之后必须还认得出连的是哪台机器，
// 否则这块横幅就只是装饰。
func TestReadyBannerShowsConnectionTargets(t *testing.T) {
	out := renderTestBanner(t, RoleAPI)
	for _, want := range []string{
		"aegis@db.internal:5432/aegis", // 用户名 + 库名保留，密码剔除
		"redis.internal:6379",
		"nats.internal:4222",
		"temporal.internal:7233",
		"legacy@mysql.internal:3306/legacy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("横幅缺少连接目标 %q，实际输出：\n%s", want, out)
		}
	}
}

// TestBannerSectionsByRole Worker 不对外提供 HTTP，展示「入口」只会误导人去访问一个不存在的端口。
func TestBannerSectionsByRole(t *testing.T) {
	api := renderTestBanner(t, RoleAPI)
	if !strings.Contains(api, "健康检查") || !strings.Contains(api, "http://127.0.0.1:8088") {
		t.Errorf("API 角色应当展示入口分区，实际输出：\n%s", api)
	}

	// 客户端 IP 判定方式是排「线上 IP 不对」时第一个要确认的事实，
	// 它必须在横幅上，而且报的是生效结果（受信网段数量）而不是配置原文。
	if !strings.Contains(api, "客户端 IP") || !strings.Contains(api, "受信 1 个网段") {
		t.Errorf("API 角色应当展示客户端 IP 判定方式，实际输出：\n%s", api)
	}

	worker := renderTestBanner(t, RoleWorker)
	if strings.Contains(worker, "健康检查") {
		t.Error("Worker 角色不应当展示入口分区")
	}
	if strings.Contains(worker, "CORS") {
		t.Error("Worker 角色不应当展示 CORS / 客户端 IP 这类 HTTP 侧配置")
	}
	if strings.Contains(worker, "客户端 IP") {
		t.Error("Worker 角色不对外提供 HTTP，不应当展示客户端 IP 判定方式")
	}
	if !strings.Contains(worker, "PostgreSQL") {
		t.Error("Worker 角色仍然应当展示数据面")
	}
}

func TestBannerHostSectionToggle(t *testing.T) {
	cfg := bannerTestConfig()
	cfg.Banner.Width = 120
	cfg.Banner.Color = "never"
	cfg.Banner.ShowHost = false

	out := RenderReadyBanner(context.Background(), BannerRuntime{Role: RoleAPI, Config: cfg})
	if strings.Contains(out, "开机") {
		t.Error("BANNER_SHOW_HOST=false 时不应当展示主机分区")
	}
	if !strings.Contains(out, "PostgreSQL") {
		t.Error("关闭主机分区不应当影响其余分区")
	}
}

func TestBannerDisabled(t *testing.T) {
	cfg := bannerTestConfig()
	cfg.Banner.Enabled = false

	if out := RenderBootBanner(cfg, RoleUnified); out != "" {
		t.Errorf("BANNER_ENABLED=false 时启动横幅应当为空，实际 %q", out)
	}
	if out := RenderReadyBanner(context.Background(), BannerRuntime{Role: RoleAPI, Config: cfg}); out != "" {
		t.Errorf("BANNER_ENABLED=false 时运行时横幅应当为空，实际 %q", out)
	}
}

// TestBannerStyleCompactKeepsBootBannerCompact BANNER_STYLE=full 不该让启动横幅
// 多长出一张还没有数据的表格。
func TestBannerStyleCompactKeepsBootBannerCompact(t *testing.T) {
	cfg := bannerTestConfig()
	cfg.Banner.Style = "full"
	cfg.Banner.Color = "never"
	cfg.Banner.Width = 120

	out := RenderBootBanner(cfg, RoleUnified)
	if strings.Contains(out, "PostgreSQL") {
		t.Errorf("启动横幅不应当包含分区表格，实际输出：\n%s", out)
	}
	if !strings.Contains(out, "___") {
		t.Errorf("启动横幅应当包含艺术字，实际输出：\n%s", out)
	}
}

// TestBannerUnknownStyleFallsBack 配了不认识的枚举值时不能静默当成关闭——
// 那会让人以为横幅坏了。
func TestBannerUnknownStyleFallsBack(t *testing.T) {
	cfg := bannerTestConfig()
	cfg.Banner.Style = "fancy"
	cfg.Banner.Color = "never"
	cfg.Banner.Width = 120

	if out := RenderBootBanner(cfg, RoleAPI); out == "" {
		t.Error("未知 BANNER_STYLE 应当回退到默认档，而不是什么都不打")
	}
}

func TestPlaceholderSecretIsFlagged(t *testing.T) {
	cases := map[string]bool{
		"change-me":                              true, // .env.example 的原值
		"short":                                  true,
		strings.Repeat("a", 31):                  true, // 不足 32 字节
		"please-change-this-before-going-live-1": true,
		"3f8b1c7d9e2a4b6c8d0e2f4a6b8c0d2e4f6a8b": false,
		// 真实密钥凑巧带上 secret / example 不该误报，否则这一行的告警会被无视
		"jwtsecret-must-not-leak-and-is-long-enough-to-pass": false,
		"example-corp-prod-signing-key-4f8b1c7d9e2a":         false,
	}
	for secret, want := range cases {
		if got := isPlaceholderSecret(secret); got != want {
			t.Errorf("isPlaceholderSecret(%q) = %v，期望 %v", secret, got, want)
		}
	}
}

func TestPostgresSummaryHandlesBothDSNForms(t *testing.T) {
	cases := map[string]string{
		"postgres://aegis:pw@db.internal:5432/aegis?sslmode=disable":     "aegis@db.internal:5432/aegis",
		"host=db.internal port=5432 user=aegis password=pw dbname=aegis": "aegis@db.internal:5432/aegis",
		"":              "",
		"not a dsn ://": "",
	}
	for dsn, want := range cases {
		if got := postgresSummary(dsn); got != want {
			t.Errorf("postgresSummary(%q) = %q，期望 %q", dsn, got, want)
		}
	}
}

func TestRedactURLDropsCredentials(t *testing.T) {
	if got, want := redactURL("nats://user:pw@nats.internal:4222"), "nats://user@nats.internal:4222"; got != want {
		t.Errorf("redactURL = %q，期望 %q", got, want)
	}
	if got, want := redactURL("nats://nats.internal:4222"), "nats://nats.internal:4222"; got != want {
		t.Errorf("redactURL = %q，期望 %q", got, want)
	}
}

// ── 路由分区 ───────────────────────────────────────────────────────────

// testRouteEngine 用结构扫描路由（全 nil 服务）当被测对象。
// 与 openapi / postman / routes 三个子命令共用同一个装配入口，
// 因此这里数出来的条数就是那三个命令看到的条数。
func testRouteEngine(t *testing.T) *gin.Engine {
	t.Helper()
	engine, err := newInspectionRouter()
	if err != nil {
		t.Fatalf("装配路由失败：%v", err)
	}
	return engine
}

func routeBannerConfig() config.Config {
	cfg := bannerTestConfig()
	cfg.Banner.Width = 150
	cfg.Banner.Color = "never"
	cfg.Banner.ShowHost = false
	return cfg
}

func TestBannerRouteSectionCountsByRealm(t *testing.T) {
	engine := testRouteEngine(t)
	out := RenderReadyBanner(context.Background(), BannerRuntime{
		Role: RoleAPI, Config: routeBannerConfig(), Router: engine,
	})

	if !strings.Contains(out, "路由") {
		t.Fatalf("缺少路由分区，实际输出：\n%s", out)
	}
	// 顶层域各占一行；分组一级（几十个）刻意不进横幅
	for _, realm := range []string{"公开", "接入网关", "管理端", "用户端"} {
		if !strings.Contains(out, realm) {
			t.Errorf("路由分区缺少顶层域 %q", realm)
		}
	}
	// 合计要与真实路由数一致——横幅里报个不准的数比不报更糟
	if !strings.Contains(out, fmt.Sprintf("%d 条", len(engine.Routes()))) {
		t.Errorf("路由分区没有报出真实总数 %d，实际输出：\n%s", len(engine.Routes()), out)
	}
	// 完整清单不在横幅里打（那正是这次要消掉的滚屏），要给出去哪儿看
	if !strings.Contains(out, "cmd/server routes") {
		t.Error("路由分区应当指明完整清单怎么看")
	}
}

// TestBannerRouteSectionStaysASummary 横幅里绝不能出现逐条路由。
// gin 在 debug 档原本就会把近千条路由逐行打出来，那正是要消掉的东西；
// 在横幅里换个更漂亮的方式再打一遍，等于什么都没解决。
func TestBannerRouteSectionStaysASummary(t *testing.T) {
	out := RenderReadyBanner(context.Background(), BannerRuntime{
		Role: RoleAPI, Config: routeBannerConfig(), Router: testRouteEngine(t),
	})
	// 刻意不拿 /healthz 当判据：「入口」分区本来就该列出健康探针地址，
	// 那是给人点开的链接，不是路由清单。
	for _, path := range []string{
		"/api/v1/apps/:appkey/auth/login",
		"/api/admin/system/organizations",
		"/api/admin/platform/overview",
	} {
		if strings.Contains(out, path) {
			t.Errorf("横幅里出现了具体路由 %q，它应该只报计数", path)
		}
	}
	// 横幅整体不该因为路由分区而变成滚屏
	if lines := strings.Count(out, "\n"); lines > 80 {
		t.Errorf("横幅长达 %d 行，路由分区应当只占几行", lines)
	}
}

func TestBannerRouteSectionToggle(t *testing.T) {
	cfg := routeBannerConfig()
	cfg.Banner.ShowRoutes = false

	out := RenderReadyBanner(context.Background(), BannerRuntime{
		Role: RoleAPI, Config: cfg, Router: testRouteEngine(t),
	})
	if strings.Contains(out, "cmd/server routes") {
		t.Error("BANNER_SHOW_ROUTES=false 时不应当展示路由分区")
	}
	if !strings.Contains(out, "PostgreSQL") {
		t.Error("关闭路由分区不应当影响其余分区")
	}
}

// TestBannerRouteSectionAbsentWithoutRouter Worker 不监听端口，
// 列出「暴露了哪些接口」等于指引人去访问一个不存在的服务。
func TestBannerRouteSectionAbsentWithoutRouter(t *testing.T) {
	worker := RenderReadyBanner(context.Background(), BannerRuntime{
		Role: RoleWorker, Config: routeBannerConfig(), Router: testRouteEngine(t),
	})
	if strings.Contains(worker, "cmd/server routes") {
		t.Error("Worker 角色不应当展示路由分区")
	}

	// API 角色但装配失败拿不到 router 时，少一个分区而不是 panic
	api := RenderReadyBanner(context.Background(), BannerRuntime{
		Role: RoleAPI, Config: routeBannerConfig(),
	})
	if strings.Contains(api, "cmd/server routes") {
		t.Error("router 为 nil 时不应当展示路由分区")
	}
	if !strings.Contains(api, "PostgreSQL") {
		t.Error("router 为 nil 不应当影响其余分区")
	}
}
