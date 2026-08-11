package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/pkg/egress"
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
		TrustedProxies:  []string{"127.0.0.1/32"},
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
	return RenderReadyBanner(context.Background(), BannerRuntime{
		Role:    role,
		Config:  cfg,
		Elapsed: 1234 * time.Millisecond,
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

	worker := renderTestBanner(t, RoleWorker)
	if strings.Contains(worker, "健康检查") {
		t.Error("Worker 角色不应当展示入口分区")
	}
	if strings.Contains(worker, "CORS") {
		t.Error("Worker 角色不应当展示 CORS / 受信代理这类 HTTP 侧配置")
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
