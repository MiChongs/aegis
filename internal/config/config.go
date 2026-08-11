package config

import (
	"aegis/pkg/egress"
	"aegis/pkg/timeutil"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	AppName string
	AppEnv  string
	// DocsPortalURL 是开发者门户（aegis-console 的 /developers）地址。
	// 后端的 /docs 只做 302 跳转，文档本体由门户承载。
	// 同源部署保持默认相对路径即可；前后端分域时填绝对地址。
	DocsPortalURL string
	// ConsoleBaseURL 是管理控制台（aegis-console）的对外地址。
	// 通知出口用它拼「查看工单」等深链；留空时通知里只带相对路径，
	// 飞书/邮件里点不开，因此分域部署务必配置绝对地址。
	ConsoleBaseURL string
	// APIBaseURL 是本服务自身的对外地址（如 https://api.example.com）。
	// 邮件里的下载链接这类**必须绝对**的地址由它拼出 —— 邮件客户端里没有「当前站点」，
	// 相对路径点不开。留空时相关邮件会退化为不带链接（附件仍照常发）。
	APIBaseURL      string
	HTTPPort        int
	AdminSessionTTL   time.Duration
	DefaultTimezone   string
	AdminBootstrap    AdminBootstrapConfig
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	ShutdownTimeout   time.Duration
	TrustedProxies    []string
	CORS              CORSConfig
	JWT               JWTConfig
	Firewall          FirewallConfig
	LoginGuard        LoginGuardConfig
	Postgres          PostgresConfig
	Database          DatabaseConfig
	LegacyMySQL       LegacyMySQLConfig
	Redis             RedisConfig
	PaymentReceipt PaymentReceiptConfig
	AdminUserSearch   AdminUserSearchConfig
	AccountBan        AccountBanConfig
	Risk              RiskConfig
	GeoIP             GeoIPConfig
	GeoRisk           GeoRiskConfig
	NATS              NATSConfig
	Temporal          TemporalConfig
	AutoSign          AutoSignConfig
	Security          SecurityConfig
	Captcha           CaptchaConfig
	RDKitCaptchaURL   string // RDKit 手性碳验证码微服务地址（默认 http://localhost:5050）
	Banner            BannerConfig
	CrashLog          CrashLogConfig
	ReplayProtection  ReplayProtectionConfig
	Lottery           LotteryConfig
	Memory            MemoryConfig
	Tracing           TracingConfig
	// Egress 出海代理网关：按目标域名后缀把出站流量路由到境外线路。
	// 详见 pkg/egress 与 docs/egress-gateway.md。
	Egress egress.Config
	OAuth  map[string]OAuthProviderConfig
}

// TracingConfig OpenTelemetry 追踪配置（环境变量驱动）。
//
// 相关环境变量：
//
//	TRACING_ENABLED           bool, default true
//	TRACING_EXPORTER          otlp-http | otlp-grpc | stdout | none
//	TRACING_ENDPOINT          OTLP collector endpoint
//	TRACING_INSECURE          bool
//	TRACING_HEADERS           "key1=v1,key2=v2"
//	TRACING_SAMPLER           always_on | traceid_ratio | parentbased_always_on | parentbased_ratio | always_off
//	TRACING_SAMPLE_RATIO      0~1 (default 1.0)
//	TRACING_SERVICE_NAME      override service.name (fallback APP_NAME → "aegis")
//	TRACING_SERVICE_VERSION   service.version
//	TRACING_ENVIRONMENT       deployment.environment (fallback APP_ENV)
//	TRACING_BATCH_TIMEOUT     duration (default 5s)
//	TRACING_EXPORT_TIMEOUT    duration (default 10s)
//
// 也兼容 OTel 标准变量：OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_SERVICE_NAME /
// OTEL_EXPORTER_OTLP_HEADERS / OTEL_TRACES_SAMPLER / OTEL_TRACES_SAMPLER_ARG /
// OTEL_RESOURCE_ATTRIBUTES。
type TracingConfig struct {
	Enabled        bool
	ServiceName    string
	ServiceVersion string
	Environment    string
	Exporter       string // otlp-http / otlp-grpc / stdout / none
	Endpoint       string
	Insecure       bool
	Headers        map[string]string
	Sampler        string  // always_on / traceid_ratio / parentbased_always_on / always_off
	SampleRatio    float64 // 0~1
	BatchTimeout   time.Duration
	ExportTimeout  time.Duration
}

// BannerConfig 启动横幅。
//
// 横幅只在进程启动时打印一次，不参与任何业务逻辑；做成配置是为了
// 容器 / CI / 日志采集这些「不需要艺术字、只需要那几行事实」的场景能一键调档。
//
// 相关环境变量：
//
//	BANNER_ENABLED    bool，默认 true；false 等价于 BANNER_STYLE=off
//	BANNER_STYLE      auto | full | compact | minimal | off，默认 auto
//	                  auto = 交互式终端给全量彩色，重定向到文件/日志时给全量纯文本
//	BANNER_FONT       FIGlet 字体名，默认 slant（go-figure 内嵌 148 种）
//	BANNER_COLOR      auto | always | never，默认 auto
//	                  auto 之外还叠加 NO_COLOR / FORCE_COLOR / TERM 的标准判定
//	BANNER_WIDTH      强制列宽；0 = 自动探测终端宽度，探测不到用 100
//	BANNER_SHOW_HOST  是否采集宿主机 CPU / 内存 / 磁盘事实，默认 true。
//	                  采集要问 WMI（Windows）或挂载点（Linux），
//	                  极少数环境下会慢，可以单独关掉而保留其余分区。
type BannerConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Style    string `mapstructure:"style"`
	Font     string `mapstructure:"font"`
	Color    string `mapstructure:"color"`
	Width    int    `mapstructure:"width"`
	ShowHost bool   `mapstructure:"show_host"`
}

// DefaultBannerConfig 返回横幅的默认配置。
//
// 除了 setDefaults 之外，它还有一个专门的用途：config.Load 失败时（缺 JWT_SECRET、
// .env 写坏了……）调用方拿到的是零值 Config，其中 Enabled=false 会让横幅整个消失。
// 而恰恰是那种时候最需要横幅先告诉人「进程起来了、版本是多少」，
// 再由随后的装配错误解释为什么起不来。所以入口拿到错误时用它兜一下。
func DefaultBannerConfig() BannerConfig {
	return BannerConfig{
		Enabled:  true,
		Style:    "auto",
		Font:     "slant",
		Color:    "auto",
		ShowHost: true,
	}
}

type CrashLogConfig struct {
	Dir      string `mapstructure:"dir"`       // 崩溃日志目录（默认 data/crashlogs）
	MaxFiles int    `mapstructure:"max_files"` // 最多保留文件数（默认 20）
	MaxSize  int64  `mapstructure:"max_size"`  // 单文件最大字节（默认 50MB）
}

type ReplayProtectionConfig struct {
	Enabled          bool
	NonceWindow      time.Duration
	NonceSkew        time.Duration
	FingerprintTTL   time.Duration
	SignatureEnabled bool
}

type CORSConfig struct {
	Enabled          bool
	AllowAllOrigins  bool
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           time.Duration
}

type JWTConfig struct {
	Secret     string
	Issuer     string
	TTL        time.Duration
	RefreshTTL time.Duration
}

type FirewallConfig struct {
	Enabled        bool
	GlobalRate     string
	AuthRate       string
	AdminRate      string
	CorazaEnabled  bool
	CorazaParanoia int
	// CorazaDetectionOnly true 时 WAF 仅记录不拦截（SecRuleEngine DetectionOnly），
	// 用于灰度验证 paranoia 升级或规则调整的误报面。
	CorazaDetectionOnly bool
	// CorazaAnomalyThreshold CRS 入站异常分阈值（越低越严格；CRS 官方默认 5
	// 在真实业务下误报极高，本项目默认 40）。
	CorazaAnomalyThreshold int
	// CorazaRemovedRules 误报排除规则 ID 列表；空 = 内置默认清单，
	// ["none"] = 不排除任何规则（完整 CRS）。
	CorazaRemovedRules []string
	RequestBodyLimit   int
	RequestBodyMemory  int
	AllowedCIDRs       []string
	BlockedCIDRs       []string
	BlockedUserAgents  []string
	BlockedPathPrefix  []string
	MaxPathLength      int
	MaxQueryLength     int

	// ── IP 封禁响应模式 ───────────────────────────────────────
	// DefaultBanMode 未显式指定 mode 时的全局默认（见 firewall.BanMode* 常量）。
	// 合法值：forbidden / silent_drop / connection_reset / tarpit / stealth_404 / teapot / rate_choke
	// 默认 "forbidden"（返回 HTTP 403）。
	DefaultBanMode string
	// TarpitDelayMs tarpit 模式的延迟毫秒（默认 5000 = 5s，最大 30000）。
	TarpitDelayMs int
	// BanRedirectURL redirect 模式跳转目标 URL（默认 "/"）。
	BanRedirectURL string

	// AutoBanRules 自动封禁规则覆盖（JSON 数组，空 = 使用内置默认规则）。
	// 格式见 service.ParseAutoBanRules；解析失败时回退默认规则并告警。
	AutoBanRules string
}

// DefaultCorazaRemovedRules 内置 CRS 误报排除清单（自托管 / 内网 / API 网关 /
// 后台 SaaS 场景的高频误报规则；语义说明见 middleware.buildCorazaDirectives）。
var DefaultCorazaRemovedRules = []string{
	"920350", "920300", "920420", "920230", "920440",
	"913100", "921110",
	"932200", "932230", "932235", "932236", "932240", "932250",
	"942100", "942430", "942440",
	"941100",
}

func NormalizeFirewallConfig(cfg FirewallConfig) FirewallConfig {
	if strings.TrimSpace(cfg.GlobalRate) == "" {
		cfg.GlobalRate = "1200-M"
	}
	if strings.TrimSpace(cfg.AuthRate) == "" {
		cfg.AuthRate = "180-M"
	}
	if strings.TrimSpace(cfg.AdminRate) == "" {
		cfg.AdminRate = "360-M"
	}
	if cfg.CorazaParanoia <= 0 {
		cfg.CorazaParanoia = 1
	}
	if cfg.CorazaParanoia > 4 {
		cfg.CorazaParanoia = 4
	}
	if cfg.CorazaAnomalyThreshold <= 0 {
		cfg.CorazaAnomalyThreshold = 40
	}
	if cfg.CorazaAnomalyThreshold < 5 {
		cfg.CorazaAnomalyThreshold = 5
	}
	if cfg.CorazaAnomalyThreshold > 10000 {
		cfg.CorazaAnomalyThreshold = 10000
	}
	switch {
	case len(cfg.CorazaRemovedRules) == 0:
		cfg.CorazaRemovedRules = append([]string(nil), DefaultCorazaRemovedRules...)
	case len(cfg.CorazaRemovedRules) == 1 && strings.EqualFold(strings.TrimSpace(cfg.CorazaRemovedRules[0]), "none"):
		cfg.CorazaRemovedRules = nil
	default:
		valid := cfg.CorazaRemovedRules[:0]
		for _, id := range cfg.CorazaRemovedRules {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, err := strconv.Atoi(id); err == nil {
				valid = append(valid, id)
			}
		}
		cfg.CorazaRemovedRules = valid
	}
	if cfg.RequestBodyLimit <= 0 {
		cfg.RequestBodyLimit = 13 * 1024 * 1024
	}
	if cfg.RequestBodyMemory <= 0 {
		cfg.RequestBodyMemory = 256 * 1024
	}
	if cfg.RequestBodyMemory > cfg.RequestBodyLimit {
		cfg.RequestBodyMemory = cfg.RequestBodyLimit
	}
	if cfg.MaxPathLength <= 0 {
		cfg.MaxPathLength = 2048
	}
	if cfg.MaxQueryLength <= 0 {
		cfg.MaxQueryLength = 4096
	}
	cfg.DefaultBanMode = strings.ToLower(strings.TrimSpace(cfg.DefaultBanMode))
	switch cfg.DefaultBanMode {
	case "", "forbidden", "silent_drop", "connection_reset", "tarpit",
		"stealth_404", "stealth_503", "teapot", "rate_choke",
		"redirect", "honeypot", "fake_empty", "random_error",
		"slow_response", "random_delay", "gone", "bandwidth_choke",
		"zip_bomb", "chunked_infinite", "infinite_redirect", "mirror_request",
		"fake_login", "random_garbage", "cursed_headers", "json_bomb",
		"cookie_bomb", "reverse_slowloris":
		if cfg.DefaultBanMode == "" {
			cfg.DefaultBanMode = "forbidden"
		}
	default:
		cfg.DefaultBanMode = "forbidden"
	}
	if cfg.TarpitDelayMs <= 0 {
		cfg.TarpitDelayMs = 5000
	}
	if cfg.TarpitDelayMs > 30000 {
		cfg.TarpitDelayMs = 30000
	}
	if len(cfg.BlockedUserAgents) == 0 {
		cfg.BlockedUserAgents = []string{"sqlmap", "nikto", "acunetix", "nessus", "wpscan", "gobuster", "dirbuster", "masscan", "nmap", "zgrab", "nuclei"}
	}
	if len(cfg.BlockedPathPrefix) == 0 {
		cfg.BlockedPathPrefix = []string{"/.env", "/.git", "/.svn", "/wp-admin", "/wp-login", "/phpmyadmin", "/vendor/phpunit", "/_ignition", "/hnap1"}
	}
	return cfg
}

// LoginGuardConfig 登录防爆破配置（账号 + IP 双维度失败计数与指数退避锁定）。
type LoginGuardConfig struct {
	// Enabled 是否启用（默认 true）。
	Enabled bool
	// Window 失败计数滑动窗口（默认 15m）。
	Window time.Duration
	// AccountThreshold 窗口内同一账号失败次数阈值（默认 5）。
	AccountThreshold int
	// IPThreshold 窗口内同一 IP 失败次数阈值（默认 20，覆盖撞库场景）。
	IPThreshold int
	// BaseLockDuration 首次锁定时长（默认 5m）；之后每次锁定按 2 的幂递增。
	BaseLockDuration time.Duration
	// MaxLockDuration 锁定时长上限（默认 24h），同时也是退避级别的记忆窗口。
	MaxLockDuration time.Duration
}

// NormalizeLoginGuardConfig 规范化登录防爆破配置默认值。
func NormalizeLoginGuardConfig(cfg LoginGuardConfig) LoginGuardConfig {
	if cfg.Window <= 0 {
		cfg.Window = 15 * time.Minute
	}
	if cfg.AccountThreshold <= 0 {
		cfg.AccountThreshold = 5
	}
	if cfg.IPThreshold <= 0 {
		cfg.IPThreshold = 20
	}
	if cfg.BaseLockDuration <= 0 {
		cfg.BaseLockDuration = 5 * time.Minute
	}
	if cfg.MaxLockDuration < cfg.BaseLockDuration {
		cfg.MaxLockDuration = 24 * time.Hour
	}
	if cfg.MaxLockDuration < cfg.BaseLockDuration {
		cfg.MaxLockDuration = cfg.BaseLockDuration
	}
	return cfg
}

// PostgresConfig 连接池与会话级生命周期参数。
//
// 这里的每一项都直接决定「一条连接从建立到销毁」的行为：
// 建多少、留多久、空闲多久回收、会话上会强制哪些超时。
type PostgresConfig struct {
	DSN             string
	SessionTimezone string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	// MaxConnLifetimeJitter 生命期抖动，避免同一批连接同时到期造成「集体重连风暴」
	MaxConnLifetimeJitter time.Duration
	// MaxConnIdleTime 空闲连接回收时间（超过即关闭，缩回 MinConns）
	MaxConnIdleTime time.Duration
	// HealthCheckPeriod 池自检周期：回收超龄/空闲连接、补足 MinConns
	HealthCheckPeriod time.Duration
	// ConnectTimeout 单次建连超时
	ConnectTimeout time.Duration
	// StatementTimeout 会话级 statement_timeout：单条语句最长执行时间，0=不限制。
	// 这是防止「慢查询把连接池占满」的第一道闸。
	StatementTimeout time.Duration
	// IdleInTxTimeout 会话级 idle_in_transaction_session_timeout：
	// 事务开着却不干活超过该时长，服务端直接结束会话。这是事务泄漏的兜底。
	IdleInTxTimeout time.Duration
	// LockTimeout 会话级 lock_timeout：等锁最长时间，0=无限等
	LockTimeout time.Duration
	// ApplicationName 写入 pg_stat_activity.application_name，便于在库侧区分来源
	ApplicationName string
}

// DatabaseConfig 数据库可观测性与泄漏治理配置。
//
// 与 PostgresConfig 的分工：
//   - PostgresConfig 决定连接「怎么活」；
//   - DatabaseConfig 决定我们「怎么看住它」——采集、判定泄漏、以及是否自动处置。
type DatabaseConfig struct {
	// MonitorEnabled 是否启动后台采集循环（关闭后仍可按需拉取快照）
	MonitorEnabled bool
	// MonitorInterval 采集间隔（池指标 + 服务端指标）
	MonitorInterval time.Duration
	// HistoryRetain Redis 历史指标保留时长
	HistoryRetain time.Duration
	// SlowQueryThreshold 慢查询阈值，超过即结构化告警
	SlowQueryThreshold time.Duration
	// VerySlowQueryThreshold 极慢查询阈值，单独计数并升级告警级别
	VerySlowQueryThreshold time.Duration
	// SlowQuerySampleSize 慢查询样本环形缓冲容量
	SlowQuerySampleSize int
	// LeakDetection 是否启用泄漏检测（连接持有 + 指标趋势）
	LeakDetection bool
	// LeakWindow 趋势检测滑动窗口采样点数
	LeakWindow int
	// ConnHoldThreshold 连接被持有超过该时长即判定为「疑似连接泄漏」。
	// 正常请求持有连接是毫秒级，秒级持有基本都是忘记 Release 或长事务。
	ConnHoldThreshold time.Duration
	// TrackAcquireStack 获取连接时抓取调用栈。
	// 这是唯一能把泄漏定位到代码行的手段，代价是每次 acquire 一次栈回溯。
	TrackAcquireStack bool
	// IdleInTxThreshold 服务端 idle in transaction 超过该时长即上报
	IdleInTxThreshold time.Duration
	// LongQueryThreshold 服务端语句执行超过该时长即上报
	LongQueryThreshold time.Duration
	// AutoTerminateIdleInTx 是否自动终止超时的 idle in transaction 会话（默认关闭）
	AutoTerminateIdleInTx bool
	// AutoTerminateThreshold 自动终止的时长阈值（必须 >= IdleInTxThreshold）
	AutoTerminateThreshold time.Duration
	// WarmupOnStart 启动时预热连接池到 MinConns，避免首个请求承担建连成本
	WarmupOnStart bool
	// DrainTimeout 关闭时等待在途查询结束的最长时间
	DrainTimeout time.Duration
}

type LegacyMySQLConfig struct {
	DSN         string
	BatchSize   int
	Concurrency int
}

type RedisConfig struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

// PaymentReceiptConfig 支付凭证（收据 / 账单 / 退款凭证）导出配置。
//
// 字体这几项是「中文凭证能不能出得来」的唯一开关。拉丁字形由内嵌字体承担，
// 任何环境下都可用；中日韩字形必须来自外部字体文件，因此需要这组配置。
type PaymentReceiptConfig struct {
	RootDir         string
	TTL             time.Duration
	CleanupInterval time.Duration

	// FontPath 中日韩字体文件路径（.ttf 或 .ttc）。
	// **不支持 .otf** —— 那是 CFF 轮廓，PDF 引擎只能嵌入 TrueType 轮廓。
	FontPath string
	// FontBoldPath 粗体伴侣字体，可留空（层级会改由字号与颜色表达）
	FontBoldPath string
	// FontDirs 额外的字体搜索目录，逗号分隔
	FontDirs []string
	// DisableSystemFontScan 关闭系统字体目录扫描。容器里字体是显式装进来的，
	// 关掉可省去一轮无谓的目录遍历。
	DisableSystemFontScan bool

	// DefaultLocale 未指定语言且无从协商时使用的语言。
	// 默认 en：凭证可能寄往任何地方，且英文只需内嵌字体，任何环境下都出得来。
	DefaultLocale string

	// EmailLinkTTL 邮件里凭证下载链接的有效期。
	// 明显长于 TTL：邮件可能几天后才被打开，一条半小时就失效的链接等于没发。
	EmailLinkTTL time.Duration
	// EmailPerDay 同一收件地址每天最多收到几封凭证邮件（补发限流）。
	EmailPerDay int

	// SigningKey / PublicBaseURL 由全局配置拷贝而来（SECURITY_MASTER_KEY、API_BASE_URL），
	// 不是独立的配置项。放在这里只是为了让凭证相关的运行期依赖聚在一处。
	SigningKey    string
	PublicBaseURL string
}

type AdminUserSearchConfig struct {
	RootDir           string
	BatchSize         int
	MaxPinyinPaths    int
	WarmupEnabled     bool
	WarmupConcurrency int
}

type AccountBanConfig struct {
	CleanupEnabled   bool
	CleanupSpec      string
	CleanupBatchSize int
}

type RiskConfig struct {
	IPReputation RiskIPReputationConfig
	RateLimit    RiskRateLimitConfig
}

type RiskIPReputationConfig struct {
	Provider   string
	CacheTTL   time.Duration
	Timeout    time.Duration
	AllowStale bool
	IPQS       RiskIPQualityScoreConfig
}

type RiskIPQualityScoreConfig struct {
	APIKey           string
	BaseURL          string
	Strictness       int
	Fast             bool
	Mobile           bool
	LighterPenalties bool
}

type RiskRateLimitConfig struct {
	Enabled                bool
	IPPerMinute            int
	AccountPerMinute       int
	DevicePerMinute        int
	AccountDevicePerMinute int
}

type GeoIPConfig struct {
	Enabled             bool
	DatabaseDir         string
	CityDBURL           string
	ASNDBURL            string
	UpdateInterval      time.Duration
	DownloadTimeout     time.Duration
	GitHubMirror        string
	ChinaOptimized      bool
	AllowRemoteFallback bool
}

// GeoRiskConfig 地理风控 / 地理分析配置。
// 阈值为 0 时由服务层回退到内置默认值（见 internal/service/geo_risk_service.go）。
type GeoRiskConfig struct {
	Enabled              bool          // 登录地理风控总开关
	MaxSpeedKMH          float64       // 不可能旅行速度阈值（km/h，默认 900）
	MinTravelKM          float64       // 不可能旅行最小位移（km，默认 100，过滤定位抖动）
	NewCountryMinLogins  int64         // 触发"异地新国家"所需的最小历史登录次数（默认 3）
	ProfileCacheTTL      time.Duration // 画像 Redis 缓存 TTL（默认 72h）
	RetentionMonths      int           // login_geo_events 分区保留月数（默认 6）
	RollupInterval       time.Duration // 小时聚合任务执行间隔（默认 10m）
	ProfileRecomputeDays int           // 画像基线重算的回看窗口天数（默认 90）
}

type NATSConfig struct {
	URL        string
	StreamName string
}

type TemporalConfig struct {
	HostPort                 string
	Namespace                string
	TaskQueue                string
	WorkflowExecutionTimeout time.Duration
	WorkflowRunTimeout       time.Duration
	WorkflowTaskTimeout      time.Duration
	ActivityTimeout          time.Duration
}

type AutoSignConfig struct {
	Enabled         bool
	Timezone        string
	TickInterval    time.Duration
	RebuildInterval time.Duration
	BatchSize       int
	Concurrency     int
	RetryDelay      time.Duration
}

type AdminBootstrapConfig struct {
	Account     string
	Password    string
	DisplayName string
	Email       string
}

type LotteryConfig struct {
	ChainRPCURL     string // 以太坊 RPC 地址（为空则跳过链上提交）
	ChainPrivateKey string // 链上提交私钥（hex）
	ChainID         int64  // 链 ID（0 = 自动检测）
}

type MemoryConfig struct {
	GCAutoTune      bool          `mapstructure:"gc_auto_tune"`      // 是否启用自适应 GC 调优（默认 true）
	MemoryLimitMB   int64         `mapstructure:"memory_limit_mb"`   // 软内存上限 MB（0 = 自动检测系统可用内存的 80%）
	MonitorInterval time.Duration `mapstructure:"monitor_interval"`  // 内存指标采集间隔（默认 15s）
	GCTuneInterval  time.Duration `mapstructure:"gc_tune_interval"`  // GC 调优间隔（默认 30s）
	LeakDetection   bool          `mapstructure:"leak_detection"`    // 是否启用泄漏检测（默认 true）
	LeakWindow      int           `mapstructure:"leak_window"`       // 泄漏检测滑动窗口大小（默认 20 个采样点）
	CacheMaxEntries int           `mapstructure:"cache_max_entries"` // 本地缓存最大条目数（默认 10000）
	CacheTTL        time.Duration `mapstructure:"cache_ttl"`         // 本地缓存默认 TTL（默认 5m）
	HistoryRetain   time.Duration `mapstructure:"history_retain"`    // Redis 历史指标保留时长（默认 1h）
}

// OAuthProviderConfig 单个第三方登录渠道的运行期配置。
//
// 两个来源共用该结构：
//  1. 平台级 .env（OAUTH_*），进程启动时装载，作为未做应用级配置时的兜底；
//  2. 应用级 app_oauth_providers 表，由 AppOAuthService 在请求期组装。
type OAuthProviderConfig struct {
	Name string
	// Kind 协议适配器类型（generic/qq/wechat/weibo/github/microsoft）。
	// 留空时按 Name 推断，保持旧配置的行为不变。
	Kind         string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	// TokenAuthStyle token 端点凭据传递方式：auto / params / basic（空=auto）
	TokenAuthStyle string
	// UserInfoAuthStyle 用户信息端点凭据传递方式：header / query（空=header）
	UserInfoAuthStyle string
	// ProfileMapping 自定义用户信息字段映射，支持点号路径（如 data.user_id）
	ProfileMapping map[string]string
	// ExtraAuthParams 附加到 authorize 链接的额外查询参数
	ExtraAuthParams map[string]string
}

var oauthDefaults = map[string]struct {
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	Scopes      []string
}{
	"qq": {
		AuthURL:     "https://graph.qq.com/oauth2.0/authorize",
		TokenURL:    "https://graph.qq.com/oauth2.0/token",
		UserInfoURL: "https://graph.qq.com/user/get_user_info",
		Scopes:      []string{"get_user_info"},
	},
	"wechat": {
		AuthURL:     "https://open.weixin.qq.com/connect/qrconnect",
		TokenURL:    "https://api.weixin.qq.com/sns/oauth2/access_token",
		UserInfoURL: "https://api.weixin.qq.com/sns/userinfo",
		Scopes:      []string{"snsapi_login"},
	},
	"github": {
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		Scopes:      []string{"read:user", "user:email"},
	},
	"google": {
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
	},
	"microsoft": {
		AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL: "https://graph.microsoft.com/oidc/userinfo",
		Scopes:      []string{"openid", "email", "profile", "User.Read"},
	},
	"weibo": {
		AuthURL:     "https://api.weibo.com/oauth2/authorize",
		TokenURL:    "https://api.weibo.com/oauth2/access_token",
		UserInfoURL: "https://api.weibo.com/2/users/show.json",
		Scopes:      []string{"email"},
	},
}

func Load() (Config, error) {
	v, _, err := newConfiguredViper()
	if err != nil {
		return Config{}, err
	}
	return loadWithViper(v)
}

func newConfiguredViper() (*viper.Viper, string, error) {
	v := viper.New()
	v.SetConfigType("env")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	configFile, err := loadEnvFile(v)
	if err != nil {
		return nil, "", err
	}
	return v, configFile, nil
}

func loadWithViper(v *viper.Viper) (Config, error) {
	cfg := Config{
		AppName:         v.GetString("APP_NAME"),
		AppEnv:          v.GetString("APP_ENV"),
		DocsPortalURL:   v.GetString("DOCS_PORTAL_URL"),
		ConsoleBaseURL:  v.GetString("CONSOLE_BASE_URL"),
		APIBaseURL:      v.GetString("API_BASE_URL"),
		HTTPPort:        v.GetInt("HTTP_PORT"),
		TrustedProxies:  csvList(v.GetString("TRUSTED_PROXIES")),
		AdminSessionTTL: v.GetDuration("ADMIN_SESSION_TTL"),
		DefaultTimezone: v.GetString("APP_DEFAULT_IANA_TIMEZONE"),
		AdminBootstrap: AdminBootstrapConfig{
			Account:     v.GetString("ADMIN_BOOTSTRAP_ACCOUNT"),
			Password:    v.GetString("ADMIN_BOOTSTRAP_PASSWORD"),
			DisplayName: v.GetString("ADMIN_BOOTSTRAP_DISPLAY_NAME"),
			Email:       v.GetString("ADMIN_BOOTSTRAP_EMAIL"),
		},
		ReadTimeout:     v.GetDuration("READ_TIMEOUT"),
		WriteTimeout:    v.GetDuration("WRITE_TIMEOUT"),
		ShutdownTimeout: v.GetDuration("SHUTDOWN_TIMEOUT"),
		CORS: CORSConfig{
			Enabled:          getBool(v, "CORS_ENABLED", true),
			AllowAllOrigins:  getBool(v, "CORS_ALLOW_ALL_ORIGINS", false),
			AllowOrigins:     csvList(v.GetString("CORS_ALLOW_ORIGINS")),
			AllowMethods:     csvList(v.GetString("CORS_ALLOW_METHODS")),
			AllowHeaders:     csvList(v.GetString("CORS_ALLOW_HEADERS")),
			ExposeHeaders:    csvList(v.GetString("CORS_EXPOSE_HEADERS")),
			AllowCredentials: getBool(v, "CORS_ALLOW_CREDENTIALS", false),
			MaxAge:           v.GetDuration("CORS_MAX_AGE"),
		},
		JWT: JWTConfig{
			Secret:     v.GetString("JWT_SECRET"),
			Issuer:     v.GetString("JWT_ISSUER"),
			TTL:        v.GetDuration("JWT_TTL"),
			RefreshTTL: v.GetDuration("JWT_REFRESH_TTL"),
		},
		Firewall: FirewallConfig{
			Enabled:                getBool(v, "FIREWALL_ENABLED", true),
			GlobalRate:             v.GetString("FIREWALL_GLOBAL_RATE"),
			AuthRate:               v.GetString("FIREWALL_AUTH_RATE"),
			AdminRate:              v.GetString("FIREWALL_ADMIN_RATE"),
			CorazaEnabled:          getBool(v, "FIREWALL_CORAZA_ENABLED", true),
			CorazaParanoia:         v.GetInt("FIREWALL_CORAZA_PARANOIA_LEVEL"),
			CorazaDetectionOnly:    getBool(v, "FIREWALL_CORAZA_DETECTION_ONLY", false),
			CorazaAnomalyThreshold: v.GetInt("FIREWALL_CORAZA_ANOMALY_THRESHOLD"),
			CorazaRemovedRules:     csvList(v.GetString("FIREWALL_CORAZA_REMOVED_RULES")),
			RequestBodyLimit:       v.GetInt("FIREWALL_REQUEST_BODY_LIMIT"),
			RequestBodyMemory:      v.GetInt("FIREWALL_REQUEST_BODY_IN_MEMORY_LIMIT"),
			AllowedCIDRs:           csvList(v.GetString("FIREWALL_ALLOWED_CIDRS")),
			BlockedCIDRs:           csvList(v.GetString("FIREWALL_BLOCKED_CIDRS")),
			BlockedUserAgents:      csvList(v.GetString("FIREWALL_BLOCKED_USER_AGENTS")),
			BlockedPathPrefix:      csvList(v.GetString("FIREWALL_BLOCKED_PATH_PREFIXES")),
			MaxPathLength:          v.GetInt("FIREWALL_MAX_PATH_LENGTH"),
			MaxQueryLength:         v.GetInt("FIREWALL_MAX_QUERY_LENGTH"),
			DefaultBanMode:         v.GetString("FIREWALL_DEFAULT_BAN_MODE"),
			TarpitDelayMs:          v.GetInt("FIREWALL_TARPIT_DELAY_MS"),
			BanRedirectURL:         v.GetString("FIREWALL_BAN_REDIRECT_URL"),
			AutoBanRules:           v.GetString("FIREWALL_AUTO_BAN_RULES"),
		},
		LoginGuard: LoginGuardConfig{
			Enabled:          getBool(v, "LOGIN_GUARD_ENABLED", true),
			Window:           v.GetDuration("LOGIN_GUARD_WINDOW"),
			AccountThreshold: v.GetInt("LOGIN_GUARD_ACCOUNT_THRESHOLD"),
			IPThreshold:      v.GetInt("LOGIN_GUARD_IP_THRESHOLD"),
			BaseLockDuration: v.GetDuration("LOGIN_GUARD_BASE_LOCK_DURATION"),
			MaxLockDuration:  v.GetDuration("LOGIN_GUARD_MAX_LOCK_DURATION"),
		},
		Postgres: PostgresConfig{
			DSN:                   v.GetString("POSTGRES_DSN"),
			SessionTimezone:       v.GetString("POSTGRES_SESSION_TIMEZONE"),
			MaxConns:              int32(v.GetInt("POSTGRES_MAX_CONNS")),
			MinConns:              int32(v.GetInt("POSTGRES_MIN_CONNS")),
			MaxConnLifetime:       v.GetDuration("POSTGRES_MAX_CONN_LIFETIME"),
			MaxConnLifetimeJitter: getDuration(v, "POSTGRES_MAX_CONN_LIFETIME_JITTER", 5*time.Minute),
			MaxConnIdleTime:       getDuration(v, "POSTGRES_MAX_CONN_IDLE_TIME", 5*time.Minute),
			HealthCheckPeriod:     getDuration(v, "POSTGRES_HEALTH_CHECK_PERIOD", time.Minute),
			ConnectTimeout:        getDuration(v, "POSTGRES_CONNECT_TIMEOUT", 10*time.Second),
			StatementTimeout:      getDuration(v, "POSTGRES_STATEMENT_TIMEOUT", 30*time.Second),
			IdleInTxTimeout:       getDuration(v, "POSTGRES_IDLE_IN_TX_TIMEOUT", 60*time.Second),
			LockTimeout:           getDuration(v, "POSTGRES_LOCK_TIMEOUT", 10*time.Second),
			ApplicationName:       firstNonEmptyStr(v.GetString("POSTGRES_APPLICATION_NAME"), "aegis"),
		},
		Database: DatabaseConfig{
			MonitorEnabled:         getBool(v, "DB_MONITOR_ENABLED", true),
			MonitorInterval:        getDuration(v, "DB_MONITOR_INTERVAL", 15*time.Second),
			HistoryRetain:          getDuration(v, "DB_HISTORY_RETAIN", time.Hour),
			SlowQueryThreshold:     getDuration(v, "DB_SLOW_QUERY_THRESHOLD", 200*time.Millisecond),
			VerySlowQueryThreshold: getDuration(v, "DB_VERY_SLOW_QUERY_THRESHOLD", 2*time.Second),
			SlowQuerySampleSize:    v.GetInt("DB_SLOW_QUERY_SAMPLES"),
			LeakDetection:          getBool(v, "DB_LEAK_DETECTION", true),
			LeakWindow:             v.GetInt("DB_LEAK_WINDOW"),
			ConnHoldThreshold:      getDuration(v, "DB_CONN_HOLD_THRESHOLD", 30*time.Second),
			TrackAcquireStack:      getBool(v, "DB_TRACK_ACQUIRE_STACK", true),
			IdleInTxThreshold:      getDuration(v, "DB_IDLE_IN_TX_THRESHOLD", 60*time.Second),
			LongQueryThreshold:     getDuration(v, "DB_LONG_QUERY_THRESHOLD", 30*time.Second),
			AutoTerminateIdleInTx:  getBool(v, "DB_AUTO_TERMINATE_IDLE_IN_TX", false),
			AutoTerminateThreshold: getDuration(v, "DB_AUTO_TERMINATE_THRESHOLD", 5*time.Minute),
			WarmupOnStart:          getBool(v, "DB_WARMUP_ON_START", true),
			DrainTimeout:           getDuration(v, "DB_DRAIN_TIMEOUT", 15*time.Second),
		},
		LegacyMySQL: LegacyMySQLConfig{
			DSN:         v.GetString("LEGACY_MYSQL_DSN"),
			BatchSize:   v.GetInt("LEGACY_MYSQL_BATCH_SIZE"),
			Concurrency: v.GetInt("LEGACY_MYSQL_CONCURRENCY"),
		},
		Redis: RedisConfig{
			Addr:      v.GetString("REDIS_ADDR"),
			Password:  v.GetString("REDIS_PASSWORD"),
			DB:        v.GetInt("REDIS_DB"),
			KeyPrefix: v.GetString("REDIS_KEY_PREFIX"),
		},
		PaymentReceipt: PaymentReceiptConfig{
			RootDir:               v.GetString("PAYMENT_RECEIPT_ROOT_DIR"),
			TTL:                   v.GetDuration("PAYMENT_RECEIPT_TTL"),
			CleanupInterval:       v.GetDuration("PAYMENT_RECEIPT_CLEANUP_INTERVAL"),
			FontPath:              v.GetString("PAYMENT_RECEIPT_FONT_PATH"),
			FontBoldPath:          v.GetString("PAYMENT_RECEIPT_FONT_BOLD_PATH"),
			FontDirs:              csvList(v.GetString("PAYMENT_RECEIPT_FONT_DIRS")),
			DisableSystemFontScan: getBool(v, "PAYMENT_RECEIPT_DISABLE_SYSTEM_FONTS", false),
			DefaultLocale:         v.GetString("PAYMENT_RECEIPT_DEFAULT_LOCALE"),
			EmailLinkTTL:          v.GetDuration("PAYMENT_RECEIPT_EMAIL_LINK_TTL"),
			EmailPerDay:           v.GetInt("PAYMENT_RECEIPT_EMAIL_PER_DAY"),
		},
		AdminUserSearch: AdminUserSearchConfig{
			RootDir:           v.GetString("ADMIN_USER_SEARCH_ROOT_DIR"),
			BatchSize:         v.GetInt("ADMIN_USER_SEARCH_BATCH_SIZE"),
			MaxPinyinPaths:    v.GetInt("ADMIN_USER_SEARCH_MAX_PINYIN_PATHS"),
			WarmupEnabled:     getBool(v, "ADMIN_USER_SEARCH_WARMUP_ENABLED", true),
			WarmupConcurrency: v.GetInt("ADMIN_USER_SEARCH_WARMUP_CONCURRENCY"),
		},
		AccountBan: AccountBanConfig{
			CleanupEnabled:   getBool(v, "ACCOUNT_BAN_CLEANUP_ENABLED", true),
			CleanupSpec:      v.GetString("ACCOUNT_BAN_CLEANUP_SPEC"),
			CleanupBatchSize: v.GetInt("ACCOUNT_BAN_CLEANUP_BATCH_SIZE"),
		},
		Risk: RiskConfig{
			IPReputation: RiskIPReputationConfig{
				Provider:   v.GetString("RISK_IP_REPUTATION_PROVIDER"),
				CacheTTL:   v.GetDuration("RISK_IP_REPUTATION_CACHE_TTL"),
				Timeout:    v.GetDuration("RISK_IP_REPUTATION_TIMEOUT"),
				AllowStale: getBool(v, "RISK_IP_REPUTATION_ALLOW_STALE", true),
				IPQS: RiskIPQualityScoreConfig{
					APIKey:           v.GetString("RISK_IPQUALITYSCORE_API_KEY"),
					BaseURL:          v.GetString("RISK_IPQUALITYSCORE_BASE_URL"),
					Strictness:       v.GetInt("RISK_IPQUALITYSCORE_STRICTNESS"),
					Fast:             getBool(v, "RISK_IPQUALITYSCORE_FAST", true),
					Mobile:           getBool(v, "RISK_IPQUALITYSCORE_MOBILE", false),
					LighterPenalties: getBool(v, "RISK_IPQUALITYSCORE_LIGHTER_PENALTIES", false),
				},
			},
			RateLimit: RiskRateLimitConfig{
				Enabled:                getBool(v, "RISK_RATE_LIMIT_ENABLED", false),
				IPPerMinute:            v.GetInt("RISK_RATE_LIMIT_IP_PER_MINUTE"),
				AccountPerMinute:       v.GetInt("RISK_RATE_LIMIT_ACCOUNT_PER_MINUTE"),
				DevicePerMinute:        v.GetInt("RISK_RATE_LIMIT_DEVICE_PER_MINUTE"),
				AccountDevicePerMinute: v.GetInt("RISK_RATE_LIMIT_ACCOUNT_DEVICE_PER_MINUTE"),
			},
		},
		GeoIP: GeoIPConfig{
			Enabled:             getBool(v, "GEOIP_ENABLED", true),
			DatabaseDir:         v.GetString("GEOIP_DATABASE_DIR"),
			CityDBURL:           v.GetString("GEOIP_CITY_DB_URL"),
			ASNDBURL:            v.GetString("GEOIP_ASN_DB_URL"),
			UpdateInterval:      v.GetDuration("GEOIP_UPDATE_INTERVAL"),
			DownloadTimeout:     v.GetDuration("GEOIP_DOWNLOAD_TIMEOUT"),
			GitHubMirror:        v.GetString("GEOIP_GITHUB_MIRROR"),
			ChinaOptimized:      getBool(v, "GEOIP_CHINA_OPTIMIZED", true),
			AllowRemoteFallback: getBool(v, "GEOIP_ALLOW_REMOTE_FALLBACK", true),
		},
		GeoRisk: GeoRiskConfig{
			Enabled:              getBool(v, "GEORISK_ENABLED", true),
			MaxSpeedKMH:          v.GetFloat64("GEORISK_MAX_SPEED_KMH"),
			MinTravelKM:          v.GetFloat64("GEORISK_MIN_TRAVEL_KM"),
			NewCountryMinLogins:  v.GetInt64("GEORISK_NEW_COUNTRY_MIN_LOGINS"),
			ProfileCacheTTL:      v.GetDuration("GEORISK_PROFILE_CACHE_TTL"),
			RetentionMonths:      v.GetInt("GEORISK_RETENTION_MONTHS"),
			RollupInterval:       v.GetDuration("GEORISK_ROLLUP_INTERVAL"),
			ProfileRecomputeDays: v.GetInt("GEORISK_PROFILE_RECOMPUTE_DAYS"),
		},
		NATS: NATSConfig{
			URL:        v.GetString("NATS_URL"),
			StreamName: v.GetString("NATS_STREAM_NAME"),
		},
		Temporal: TemporalConfig{
			HostPort:                 v.GetString("TEMPORAL_HOST_PORT"),
			Namespace:                v.GetString("TEMPORAL_NAMESPACE"),
			TaskQueue:                v.GetString("TEMPORAL_TASK_QUEUE"),
			WorkflowExecutionTimeout: v.GetDuration("TEMPORAL_WORKFLOW_EXECUTION_TIMEOUT"),
			WorkflowRunTimeout:       v.GetDuration("TEMPORAL_WORKFLOW_RUN_TIMEOUT"),
			WorkflowTaskTimeout:      v.GetDuration("TEMPORAL_WORKFLOW_TASK_TIMEOUT"),
			ActivityTimeout:          v.GetDuration("TEMPORAL_ACTIVITY_TIMEOUT"),
		},
		AutoSign: AutoSignConfig{
			Enabled:         v.GetBool("AUTO_SIGN_ENABLED"),
			Timezone:        v.GetString("AUTO_SIGN_TIMEZONE"),
			TickInterval:    v.GetDuration("AUTO_SIGN_TICK_INTERVAL"),
			RebuildInterval: v.GetDuration("AUTO_SIGN_REBUILD_INTERVAL"),
			BatchSize:       v.GetInt("AUTO_SIGN_BATCH_SIZE"),
			Concurrency:     v.GetInt("AUTO_SIGN_CONCURRENCY"),
			RetryDelay:      v.GetDuration("AUTO_SIGN_RETRY_DELAY"),
		},
		Security: SecurityConfig{
			MasterKey:    v.GetString("SECURITY_MASTER_KEY"),
			ChallengeTTL: v.GetDuration("SECURITY_CHALLENGE_TTL"),
			Modules: SecurityModulesConfig{
				TOTPEnabled:          getBool(v, "SECURITY_MODULE_TOTP_ENABLED", true),
				RecoveryCodesEnabled: getBool(v, "SECURITY_MODULE_RECOVERY_CODES_ENABLED", true),
				PasskeyEnabled:       getBool(v, "SECURITY_MODULE_PASSKEY_ENABLED", true),
			},
			TOTP: TOTPConfig{
				Enabled:       getBool(v, "SECURITY_TOTP_ENABLED", true),
				Issuer:        v.GetString("SECURITY_TOTP_ISSUER"),
				EnrollmentTTL: v.GetDuration("SECURITY_TOTP_ENROLLMENT_TTL"),
				Skew:          uint(v.GetUint("SECURITY_TOTP_SKEW")),
				Digits:        v.GetInt("SECURITY_TOTP_DIGITS"),
			},
			RecoveryCode: RecoveryCodeConfig{
				Enabled: getBool(v, "SECURITY_RECOVERY_CODES_ENABLED", true),
				Count:   v.GetInt("SECURITY_RECOVERY_CODES_COUNT"),
				Length:  v.GetInt("SECURITY_RECOVERY_CODES_LENGTH"),
			},
			Passkey: PasskeyConfig{
				Enabled:          getBool(v, "SECURITY_PASSKEY_ENABLED", true),
				RPDisplayName:    v.GetString("SECURITY_PASSKEY_RP_DISPLAY_NAME"),
				RPID:             v.GetString("SECURITY_PASSKEY_RP_ID"),
				RPOrigins:        csvList(v.GetString("SECURITY_PASSKEY_RP_ORIGINS")),
				RPTopOrigins:     csvList(v.GetString("SECURITY_PASSKEY_RP_TOP_ORIGINS")),
				ChallengeTTL:     v.GetDuration("SECURITY_PASSKEY_CHALLENGE_TTL"),
				UserVerification: v.GetString("SECURITY_PASSKEY_USER_VERIFICATION"),
			},
		},
		Captcha: CaptchaConfig{
			Enabled: getBool(v, "CAPTCHA_ENABLED", true),
			TTL:     v.GetDuration("CAPTCHA_TTL"),
			Image: ImageCaptchaConfig{
				Enabled:    getBool(v, "CAPTCHA_IMAGE_ENABLED", true),
				Length:     v.GetInt("CAPTCHA_IMAGE_LENGTH"),
				Width:      v.GetInt("CAPTCHA_IMAGE_WIDTH"),
				Height:     v.GetInt("CAPTCHA_IMAGE_HEIGHT"),
				NoiseCount: v.GetInt("CAPTCHA_IMAGE_NOISE_COUNT"),
				ShowLine:   getBool(v, "CAPTCHA_IMAGE_SHOW_LINE", true),
			},
			Math: MathCaptchaConfig{
				Enabled:   getBool(v, "CAPTCHA_MATH_ENABLED", true),
				MaxNumber: v.GetInt("CAPTCHA_MATH_MAX_NUMBER"),
				Width:     v.GetInt("CAPTCHA_MATH_WIDTH"),
				Height:    v.GetInt("CAPTCHA_MATH_HEIGHT"),
			},
			Digit: DigitCaptchaConfig{
				Enabled: getBool(v, "CAPTCHA_DIGIT_ENABLED", true),
				Length:  v.GetInt("CAPTCHA_DIGIT_LENGTH"),
				Width:   v.GetInt("CAPTCHA_DIGIT_WIDTH"),
				Height:  v.GetInt("CAPTCHA_DIGIT_HEIGHT"),
			},
			SMS: SMSCaptchaConfig{
				Enabled:               getBool(v, "CAPTCHA_SMS_ENABLED", false),
				CodeLength:            v.GetInt("CAPTCHA_SMS_CODE_LENGTH"),
				TTL:                   v.GetDuration("CAPTCHA_SMS_TTL"),
				SendInterval:          v.GetDuration("CAPTCHA_SMS_SEND_INTERVAL"),
				MaxAttempts:           v.GetInt("CAPTCHA_SMS_MAX_ATTEMPTS"),
				DailyLimit:            v.GetInt("CAPTCHA_SMS_DAILY_LIMIT"),
				RequireCaptcha:        getBool(v, "CAPTCHA_SMS_REQUIRE_CAPTCHA", true),
				IPHourlyLimit:         v.GetInt("CAPTCHA_SMS_IP_HOURLY_LIMIT"),
				IPDailyLimit:          v.GetInt("CAPTCHA_SMS_IP_DAILY_LIMIT"),
				GlobalPhoneDailyLimit: v.GetInt("CAPTCHA_SMS_GLOBAL_PHONE_DAILY_LIMIT"),
			},
		},
		RDKitCaptchaURL: func() string {
			s := v.GetString("RDKIT_CAPTCHA_URL")
			if s == "" {
				return "http://localhost:5050"
			}
			return s
		}(),
		Lottery: LotteryConfig{
			ChainRPCURL:     v.GetString("LOTTERY_CHAIN_RPC_URL"),
			ChainPrivateKey: v.GetString("LOTTERY_CHAIN_PRIVATE_KEY"),
			ChainID:         v.GetInt64("LOTTERY_CHAIN_ID"),
		},
		Memory: MemoryConfig{
			GCAutoTune:      getBool(v, "MEMORY_GC_AUTO_TUNE", true),
			MemoryLimitMB:   v.GetInt64("MEMORY_LIMIT_MB"),
			MonitorInterval: getDuration(v, "MEMORY_MONITOR_INTERVAL", 15*time.Second),
			GCTuneInterval:  getDuration(v, "MEMORY_GC_TUNE_INTERVAL", 30*time.Second),
			LeakDetection:   getBool(v, "MEMORY_LEAK_DETECTION", true),
			LeakWindow:      v.GetInt("MEMORY_LEAK_WINDOW"),
			CacheMaxEntries: v.GetInt("MEMORY_CACHE_MAX_ENTRIES"),
			CacheTTL:        getDuration(v, "MEMORY_CACHE_TTL", 5*time.Minute),
			HistoryRetain:   getDuration(v, "MEMORY_HISTORY_RETAIN", time.Hour),
		},
		Banner: BannerConfig{
			Enabled:  getBool(v, "BANNER_ENABLED", true),
			Style:    strings.ToLower(strings.TrimSpace(v.GetString("BANNER_STYLE"))),
			Font:     strings.TrimSpace(v.GetString("BANNER_FONT")),
			Color:    strings.ToLower(strings.TrimSpace(v.GetString("BANNER_COLOR"))),
			Width:    v.GetInt("BANNER_WIDTH"),
			ShowHost: getBool(v, "BANNER_SHOW_HOST", true),
		},
		CrashLog: CrashLogConfig{
			Dir:      v.GetString("CRASHLOG_DIR"),
			MaxFiles: v.GetInt("CRASHLOG_MAX_FILES"),
			MaxSize:  v.GetInt64("CRASHLOG_MAX_SIZE"),
		},
		ReplayProtection: ReplayProtectionConfig{
			Enabled:          getBool(v, "REPLAY_PROTECTION_ENABLED", true),
			NonceWindow:      getDuration(v, "REPLAY_PROTECTION_NONCE_WINDOW", 5*time.Minute),
			NonceSkew:        getDuration(v, "REPLAY_PROTECTION_NONCE_SKEW", 30*time.Second),
			FingerprintTTL:   getDuration(v, "REPLAY_PROTECTION_FINGERPRINT_TTL", 5*time.Second),
			SignatureEnabled: getBool(v, "REPLAY_PROTECTION_SIGNATURE_ENABLED", true),
		},
		Tracing: TracingConfig{
			Enabled:        getBool(v, "TRACING_ENABLED", true),
			ServiceName:    firstNonEmptyStr(v.GetString("TRACING_SERVICE_NAME"), v.GetString("OTEL_SERVICE_NAME")),
			ServiceVersion: firstNonEmptyStr(v.GetString("TRACING_SERVICE_VERSION"), v.GetString("OTEL_SERVICE_VERSION")),
			Environment:    v.GetString("TRACING_ENVIRONMENT"),
			Exporter:       strings.ToLower(strings.TrimSpace(v.GetString("TRACING_EXPORTER"))),
			Endpoint:       firstNonEmptyStr(v.GetString("TRACING_ENDPOINT"), v.GetString("OTEL_EXPORTER_OTLP_ENDPOINT"), v.GetString("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")),
			Insecure:       getBool(v, "TRACING_INSECURE", true),
			Headers:        parseHeadersEnv(firstNonEmptyStr(v.GetString("TRACING_HEADERS"), v.GetString("OTEL_EXPORTER_OTLP_HEADERS"))),
			Sampler:        strings.ToLower(strings.TrimSpace(firstNonEmptyStr(v.GetString("TRACING_SAMPLER"), v.GetString("OTEL_TRACES_SAMPLER")))),
			SampleRatio:    parseFloatEnv(firstNonEmptyStr(v.GetString("TRACING_SAMPLE_RATIO"), v.GetString("OTEL_TRACES_SAMPLER_ARG")), 1.0),
			BatchTimeout:   getDuration(v, "TRACING_BATCH_TIMEOUT", 5*time.Second),
			ExportTimeout:  getDuration(v, "TRACING_EXPORT_TIMEOUT", 10*time.Second),
		},
		OAuth: map[string]OAuthProviderConfig{},
	}

	setDefaults(&cfg)
	for name, defaults := range oauthDefaults {
		prefix := strings.ToUpper(name)
		cfg.OAuth[name] = OAuthProviderConfig{
			Name:         name,
			Kind:         name,
			ClientID:     v.GetString(fmt.Sprintf("OAUTH_%s_CLIENT_ID", prefix)),
			ClientSecret: v.GetString(fmt.Sprintf("OAUTH_%s_CLIENT_SECRET", prefix)),
			RedirectURL:  v.GetString(fmt.Sprintf("OAUTH_%s_REDIRECT_URL", prefix)),
			AuthURL:      defaults.AuthURL,
			TokenURL:     defaults.TokenURL,
			UserInfoURL:  defaults.UserInfoURL,
			Scopes:       defaults.Scopes,
		}
	}

	if cfg.JWT.Secret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}
	if cfg.Postgres.DSN == "" {
		return Config{}, fmt.Errorf("POSTGRES_DSN is required")
	}
	if cfg.Redis.Addr == "" {
		return Config{}, fmt.Errorf("REDIS_ADDR is required")
	}
	if _, err := timeutil.LoadLocation(cfg.DefaultTimezone); err != nil {
		return Config{}, err
	}
	if _, err := timeutil.LoadLocation(cfg.Postgres.SessionTimezone); err != nil {
		return Config{}, err
	}
	if _, err := timeutil.LoadLocation(cfg.AutoSign.Timezone); err != nil {
		return Config{}, err
	}
	if err := timeutil.Init(cfg.DefaultTimezone); err != nil {
		return Config{}, err
	}
	if cfg.NATS.URL == "" {
		return Config{}, fmt.Errorf("NATS_URL is required")
	}

	egressCfg, err := loadEgressConfig(v)
	if err != nil {
		return Config{}, err
	}
	cfg.Egress = egressCfg.Normalize()
	if err := cfg.Egress.Validate(); err != nil {
		return Config{}, fmt.Errorf("出海网关配置无效: %w", err)
	}

	return cfg, nil
}

func loadEnvFile(v *viper.Viper) (string, error) {
	configFile, err := resolveEnvFilePath()
	if err != nil {
		return "", err
	}
	if configFile == "" {
		return "", nil
	}
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return "", nil
		}
		return "", fmt.Errorf("read env config %q: %w", configFile, err)
	}
	return configFile, nil
}

func resolveEnvFilePath() (string, error) {
	if customPath := strings.TrimSpace(os.Getenv("AEGIS_ENV_FILE")); customPath != "" {
		if !filepath.IsAbs(customPath) {
			absPath, err := filepath.Abs(customPath)
			if err != nil {
				return "", fmt.Errorf("resolve AEGIS_ENV_FILE: %w", err)
			}
			customPath = absPath
		}
		if _, err := os.Stat(customPath); err != nil {
			return "", fmt.Errorf("AEGIS_ENV_FILE %q: %w", customPath, err)
		}
		return customPath, nil
	}

	searchRoots := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil {
		searchRoots = append(searchRoots, wd)
	}
	if exePath, err := os.Executable(); err == nil {
		searchRoots = append(searchRoots, filepath.Dir(exePath))
	}

	seen := make(map[string]struct{}, len(searchRoots)*4)
	for _, root := range searchRoots {
		for _, dir := range parentDirs(root) {
			candidate := filepath.Join(dir, ".env")
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	return "", nil
}

func parentDirs(start string) []string {
	if strings.TrimSpace(start) == "" {
		return nil
	}
	absStart, err := filepath.Abs(start)
	if err != nil {
		absStart = start
	}

	dirs := make([]string, 0, 8)
	current := filepath.Clean(absStart)
	for {
		dirs = append(dirs, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return dirs
}

func setDefaults(cfg *Config) {
	if cfg.AppName == "" {
		cfg.AppName = "aegis"
	}
	if cfg.AppEnv == "" {
		cfg.AppEnv = "development"
	}

	// Tracing 默认：启用 + 空 exporter（进程内 TraceID 仍会生成）
	if cfg.Tracing.ServiceName == "" {
		cfg.Tracing.ServiceName = cfg.AppName
	}
	if cfg.Tracing.ServiceVersion == "" {
		cfg.Tracing.ServiceVersion = "dev"
	}
	if cfg.Tracing.Environment == "" {
		cfg.Tracing.Environment = cfg.AppEnv
	}
	if cfg.Tracing.Exporter == "" {
		if cfg.Tracing.Endpoint != "" {
			if strings.HasPrefix(cfg.Tracing.Endpoint, "http://") || strings.HasPrefix(cfg.Tracing.Endpoint, "https://") {
				cfg.Tracing.Exporter = "otlp-http"
			} else {
				cfg.Tracing.Exporter = "otlp-grpc"
			}
		} else {
			cfg.Tracing.Exporter = "none"
		}
	}
	if cfg.Tracing.Sampler == "" {
		cfg.Tracing.Sampler = "always_on"
	}
	if cfg.Tracing.SampleRatio == 0 {
		cfg.Tracing.SampleRatio = 1.0
	}
	if cfg.Tracing.BatchTimeout == 0 {
		cfg.Tracing.BatchTimeout = 5 * time.Second
	}
	if cfg.Tracing.ExportTimeout == 0 {
		cfg.Tracing.ExportTimeout = 10 * time.Second
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8088
	}
	if cfg.AdminSessionTTL == 0 {
		cfg.AdminSessionTTL = 12 * time.Hour
	}
	if strings.TrimSpace(cfg.AdminBootstrap.Account) == "" {
		cfg.AdminBootstrap.Account = "superadmin"
	}
	if strings.TrimSpace(cfg.AdminBootstrap.DisplayName) == "" {
		cfg.AdminBootstrap.DisplayName = "Super Administrator"
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 5 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	if len(cfg.CORS.AllowMethods) == 0 {
		cfg.CORS.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"}
	}
	if len(cfg.CORS.AllowHeaders) == 0 {
		cfg.CORS.AllowHeaders = []string{
			"Origin",
			"Content-Type",
			"Content-Length",
			"Accept",
			"Accept-Encoding",
			"Authorization",
			"Cache-Control",
			"Pragma",
			"X-Requested-With",
			"X-Admin-Token",
			"X-App-Id",
			"X-Appid",
			"X-Aegis-Appid",
			"X-Aegis-App-Key",
			"X-Aegis-Protocol",
			"X-Aegis-Key-Id",
			"X-Aegis-Client-Key",
			"X-Aegis-Timestamp",
			"X-Aegis-Encrypted",
			"X-Aegis-Nonce",
			"X-Aegis-Algorithm",
			"X-Aegis-Plain-Content-Type",
			"X-Encryption",
			"X-Signature",
			"X-Timestamp",
			"X-Nonce",
		}
	}
	if len(cfg.CORS.ExposeHeaders) == 0 {
		cfg.CORS.ExposeHeaders = []string{
			"Content-Length",
			"Content-Type",
			"X-Request-Id",
			"X-Aegis-Protocol",
			"X-Aegis-Key-Id",
			"X-Aegis-Response-Nonce",
			"X-Aegis-Encrypted",
			"X-Aegis-Nonce",
			"X-Aegis-Algorithm",
			"X-Aegis-Plain-Content-Type",
		}
	}
	if cfg.CORS.MaxAge == 0 {
		cfg.CORS.MaxAge = 12 * time.Hour
	}
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = cfg.AppName
	}
	if cfg.JWT.TTL == 0 {
		cfg.JWT.TTL = 30 * 24 * time.Hour
	}
	if cfg.JWT.RefreshTTL == 0 {
		cfg.JWT.RefreshTTL = 7 * 24 * time.Hour
	}
	if strings.TrimSpace(cfg.DefaultTimezone) == "" {
		cfg.DefaultTimezone = "Asia/Shanghai"
	}
	cfg.Firewall = NormalizeFirewallConfig(cfg.Firewall)
	cfg.LoginGuard = NormalizeLoginGuardConfig(cfg.LoginGuard)
	if cfg.Postgres.MaxConns == 0 {
		cfg.Postgres.MaxConns = 10
	}
	if cfg.Postgres.MinConns == 0 {
		cfg.Postgres.MinConns = 2
	}
	if cfg.Postgres.MaxConnLifetime == 0 {
		cfg.Postgres.MaxConnLifetime = 30 * time.Minute
	}
	if strings.TrimSpace(cfg.Postgres.SessionTimezone) == "" {
		cfg.Postgres.SessionTimezone = cfg.DefaultTimezone
	}
	if strings.TrimSpace(cfg.Postgres.ApplicationName) == "" {
		cfg.Postgres.ApplicationName = cfg.AppName
	}
	// MinConns 不得超过 MaxConns，否则 pgxpool 直接构造失败
	if cfg.Postgres.MinConns > cfg.Postgres.MaxConns {
		cfg.Postgres.MinConns = cfg.Postgres.MaxConns
	}
	if cfg.Database.SlowQuerySampleSize <= 0 {
		cfg.Database.SlowQuerySampleSize = 50
	}
	if cfg.Database.LeakWindow <= 0 {
		cfg.Database.LeakWindow = 20
	}
	// 自动终止的阈值必须不小于上报阈值，否则会出现「还没上报就被杀」的反直觉行为
	if cfg.Database.AutoTerminateThreshold < cfg.Database.IdleInTxThreshold {
		cfg.Database.AutoTerminateThreshold = cfg.Database.IdleInTxThreshold
	}
	if cfg.LegacyMySQL.BatchSize == 0 {
		cfg.LegacyMySQL.BatchSize = 500
	}
	if cfg.LegacyMySQL.Concurrency <= 0 {
		cfg.LegacyMySQL.Concurrency = 8
	}
	if cfg.Redis.KeyPrefix == "" {
		cfg.Redis.KeyPrefix = "aegis"
	}
	if strings.TrimSpace(cfg.PaymentReceipt.RootDir) == "" {
		cfg.PaymentReceipt.RootDir = filepath.Join("data", "payment-bills")
	}
	if strings.TrimSpace(cfg.PaymentReceipt.DefaultLocale) == "" {
		cfg.PaymentReceipt.DefaultLocale = "en"
	}
	if cfg.PaymentReceipt.TTL <= 0 {
		cfg.PaymentReceipt.TTL = 30 * time.Minute
	}
	if cfg.PaymentReceipt.EmailLinkTTL <= 0 {
		cfg.PaymentReceipt.EmailLinkTTL = 7 * 24 * time.Hour
	}
	if cfg.PaymentReceipt.EmailPerDay <= 0 {
		cfg.PaymentReceipt.EmailPerDay = 10
	}
	// 签名密钥与对外地址来自全局配置，在这里拷进来，避免服务层再去认识整个 Config
	cfg.PaymentReceipt.SigningKey = cfg.Security.MasterKey
	cfg.PaymentReceipt.PublicBaseURL = strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if cfg.PaymentReceipt.CleanupInterval <= 0 {
		cfg.PaymentReceipt.CleanupInterval = 5 * time.Minute
	}
	if strings.TrimSpace(cfg.AdminUserSearch.RootDir) == "" {
		cfg.AdminUserSearch.RootDir = filepath.Join("data", "search")
	}
	if cfg.AdminUserSearch.BatchSize <= 0 {
		cfg.AdminUserSearch.BatchSize = 2000
	}
	if cfg.AdminUserSearch.MaxPinyinPaths <= 0 {
		cfg.AdminUserSearch.MaxPinyinPaths = 16
	}
	if cfg.AdminUserSearch.WarmupConcurrency <= 0 {
		cfg.AdminUserSearch.WarmupConcurrency = 2
	}
	if strings.TrimSpace(cfg.AccountBan.CleanupSpec) == "" {
		cfg.AccountBan.CleanupSpec = "@every 1m"
	}
	if cfg.AccountBan.CleanupBatchSize <= 0 {
		cfg.AccountBan.CleanupBatchSize = 200
	}
	if strings.TrimSpace(cfg.DocsPortalURL) == "" {
		cfg.DocsPortalURL = "/developers"
	}
	if strings.TrimSpace(cfg.Risk.IPReputation.Provider) == "" {
		cfg.Risk.IPReputation.Provider = "none"
	}
	if cfg.Risk.IPReputation.CacheTTL == 0 {
		cfg.Risk.IPReputation.CacheTTL = 6 * time.Hour
	}
	if cfg.Risk.IPReputation.Timeout == 0 {
		cfg.Risk.IPReputation.Timeout = 1500 * time.Millisecond
	}
	if strings.TrimSpace(cfg.Risk.IPReputation.IPQS.BaseURL) == "" {
		cfg.Risk.IPReputation.IPQS.BaseURL = "https://www.ipqualityscore.com/api/json/ip"
	}
	if cfg.Risk.IPReputation.IPQS.Strictness <= 0 {
		cfg.Risk.IPReputation.IPQS.Strictness = 1
	}
	if cfg.Risk.RateLimit.IPPerMinute <= 0 {
		cfg.Risk.RateLimit.IPPerMinute = 120
	}
	if strings.TrimSpace(cfg.GeoIP.DatabaseDir) == "" {
		cfg.GeoIP.DatabaseDir = ".runtime/geoip"
	}
	if strings.TrimSpace(cfg.GeoIP.CityDBURL) == "" {
		cfg.GeoIP.CityDBURL = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb"
	}
	if strings.TrimSpace(cfg.GeoIP.ASNDBURL) == "" {
		cfg.GeoIP.ASNDBURL = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-ASN.mmdb"
	}
	if cfg.GeoIP.UpdateInterval == 0 {
		cfg.GeoIP.UpdateInterval = 24 * time.Hour
	}
	if cfg.GeoIP.DownloadTimeout == 0 {
		cfg.GeoIP.DownloadTimeout = 2 * time.Minute
	}
	if strings.TrimSpace(cfg.GeoIP.GitHubMirror) == "" {
		cfg.GeoIP.GitHubMirror = "https://ghfast.top/"
	}
	if cfg.NATS.StreamName == "" {
		cfg.NATS.StreamName = "AEGIS_EVENTS"
	}
	if cfg.Temporal.HostPort == "" {
		cfg.Temporal.HostPort = "127.0.0.1:7233"
	}
	if cfg.Temporal.Namespace == "" {
		cfg.Temporal.Namespace = "default"
	}
	if cfg.Temporal.TaskQueue == "" {
		cfg.Temporal.TaskQueue = "aegis-workflow"
	}
	if cfg.Temporal.WorkflowExecutionTimeout == 0 {
		cfg.Temporal.WorkflowExecutionTimeout = 30 * 24 * time.Hour
	}
	if cfg.Temporal.WorkflowRunTimeout == 0 {
		cfg.Temporal.WorkflowRunTimeout = 24 * time.Hour
	}
	if cfg.Temporal.WorkflowTaskTimeout == 0 {
		cfg.Temporal.WorkflowTaskTimeout = 30 * time.Second
	}
	if cfg.Temporal.ActivityTimeout == 0 {
		cfg.Temporal.ActivityTimeout = 30 * time.Second
	}
	if !cfg.AutoSign.Enabled {
		cfg.AutoSign.Enabled = true
	}
	if cfg.AutoSign.Timezone == "" {
		cfg.AutoSign.Timezone = cfg.DefaultTimezone
	}
	if cfg.AutoSign.TickInterval == 0 {
		cfg.AutoSign.TickInterval = time.Minute
	}
	if cfg.AutoSign.RebuildInterval == 0 {
		cfg.AutoSign.RebuildInterval = 15 * time.Minute
	}
	if cfg.AutoSign.BatchSize == 0 {
		cfg.AutoSign.BatchSize = 200
	}
	if cfg.AutoSign.Concurrency <= 0 {
		cfg.AutoSign.Concurrency = 8
	}
	if cfg.AutoSign.RetryDelay == 0 {
		cfg.AutoSign.RetryDelay = 5 * time.Minute
	}
	// 横幅：只归一化枚举值，宽度留 0 交给 pkg/banner 去探测终端
	bannerDefaults := DefaultBannerConfig()
	if cfg.Banner.Style == "" {
		cfg.Banner.Style = bannerDefaults.Style
	}
	if cfg.Banner.Font == "" {
		cfg.Banner.Font = bannerDefaults.Font
	}
	if cfg.Banner.Color == "" {
		cfg.Banner.Color = bannerDefaults.Color
	}
	if cfg.Banner.Width < 0 {
		cfg.Banner.Width = 0
	}
	if cfg.CrashLog.Dir == "" {
		cfg.CrashLog.Dir = "data/crashlogs"
	}
	if cfg.CrashLog.MaxFiles <= 0 {
		cfg.CrashLog.MaxFiles = 20
	}
	if cfg.CrashLog.MaxSize <= 0 {
		cfg.CrashLog.MaxSize = 50 * 1024 * 1024
	}
	if cfg.Memory.LeakWindow <= 0 {
		cfg.Memory.LeakWindow = 20
	}
	if cfg.Memory.CacheMaxEntries <= 0 {
		cfg.Memory.CacheMaxEntries = 10000
	}
	applySecurityDefaults(cfg)
	cfg.Captcha = NormalizeCaptchaConfig(cfg.Captcha)
}

func csvList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func getDuration(v *viper.Viper, key string, fallback time.Duration) time.Duration {
	if !v.IsSet(key) {
		return fallback
	}
	d := v.GetDuration(key)
	if d <= 0 {
		return fallback
	}
	return d
}

func getBool(v *viper.Viper, key string, fallback bool) bool {
	if !v.IsSet(key) {
		return fallback
	}
	return v.GetBool(key)
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseHeadersEnv(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, kv := range strings.Split(raw, ",") {
		p := strings.SplitN(strings.TrimSpace(kv), "=", 2)
		if len(p) == 2 && p[0] != "" {
			out[p[0]] = p[1]
		}
	}
	return out
}

func parseFloatEnv(raw string, fallback float64) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	var f float64
	if _, err := fmt.Sscanf(raw, "%f", &f); err == nil {
		return f
	}
	return fallback
}
