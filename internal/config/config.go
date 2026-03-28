package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	AppName           string
	AppEnv            string
	HTTPPort          int
	AdminSessionTTL   time.Duration
	AdminBootstrap    AdminBootstrapConfig
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	ShutdownTimeout   time.Duration
	CORS              CORSConfig
	JWT               JWTConfig
	Firewall          FirewallConfig
	Postgres          PostgresConfig
	LegacyMySQL       LegacyMySQLConfig
	Redis             RedisConfig
	PaymentBillExport PaymentBillExportConfig
	AdminUserSearch   AdminUserSearchConfig
	AccountBan        AccountBanConfig
	Risk              RiskConfig
	GeoIP             GeoIPConfig
	NATS              NATSConfig
	Temporal          TemporalConfig
	AutoSign          AutoSignConfig
	Security          SecurityConfig
	Captcha           CaptchaConfig
	RDKitCaptchaURL   string // RDKit 手性碳验证码微服务地址（默认 http://localhost:5050）
	CrashLog          CrashLogConfig
	ReplayProtection  ReplayProtectionConfig
	Lottery           LotteryConfig
	Memory            MemoryConfig
	OAuth             map[string]OAuthProviderConfig
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
	Enabled           bool
	GlobalRate        string
	AuthRate          string
	AdminRate         string
	CorazaEnabled     bool
	CorazaParanoia    int
	RequestBodyLimit  int
	RequestBodyMemory int
	AllowedCIDRs      []string
	BlockedCIDRs      []string
	BlockedUserAgents []string
	BlockedPathPrefix []string
	MaxPathLength     int
	MaxQueryLength    int
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
	if len(cfg.BlockedUserAgents) == 0 {
		cfg.BlockedUserAgents = []string{"sqlmap", "nikto", "acunetix", "nessus", "wpscan", "gobuster", "dirbuster", "masscan", "nmap", "zgrab", "nuclei"}
	}
	if len(cfg.BlockedPathPrefix) == 0 {
		cfg.BlockedPathPrefix = []string{"/.env", "/.git", "/.svn", "/wp-admin", "/wp-login", "/phpmyadmin", "/vendor/phpunit", "/_ignition", "/hnap1"}
	}
	return cfg
}

type PostgresConfig struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
}

type LegacyMySQLConfig struct {
	DSN       string
	BatchSize int
}

type RedisConfig struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

type PaymentBillExportConfig struct {
	RootDir         string
	TTL             time.Duration
	CleanupInterval time.Duration
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

type OAuthProviderConfig struct {
	Name         string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
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
	v := viper.New()
	v.SetConfigType("env")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	if err := loadEnvFile(v); err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppName:         v.GetString("APP_NAME"),
		AppEnv:          v.GetString("APP_ENV"),
		HTTPPort:        v.GetInt("HTTP_PORT"),
		AdminSessionTTL: v.GetDuration("ADMIN_SESSION_TTL"),
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
			AllowAllOrigins:  getBool(v, "CORS_ALLOW_ALL_ORIGINS", true),
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
			Enabled:           getBool(v, "FIREWALL_ENABLED", true),
			GlobalRate:        v.GetString("FIREWALL_GLOBAL_RATE"),
			AuthRate:          v.GetString("FIREWALL_AUTH_RATE"),
			AdminRate:         v.GetString("FIREWALL_ADMIN_RATE"),
			CorazaEnabled:     getBool(v, "FIREWALL_CORAZA_ENABLED", true),
			CorazaParanoia:    v.GetInt("FIREWALL_CORAZA_PARANOIA_LEVEL"),
			RequestBodyLimit:  v.GetInt("FIREWALL_REQUEST_BODY_LIMIT"),
			RequestBodyMemory: v.GetInt("FIREWALL_REQUEST_BODY_IN_MEMORY_LIMIT"),
			AllowedCIDRs:      csvList(v.GetString("FIREWALL_ALLOWED_CIDRS")),
			BlockedCIDRs:      csvList(v.GetString("FIREWALL_BLOCKED_CIDRS")),
			BlockedUserAgents: csvList(v.GetString("FIREWALL_BLOCKED_USER_AGENTS")),
			BlockedPathPrefix: csvList(v.GetString("FIREWALL_BLOCKED_PATH_PREFIXES")),
			MaxPathLength:     v.GetInt("FIREWALL_MAX_PATH_LENGTH"),
			MaxQueryLength:    v.GetInt("FIREWALL_MAX_QUERY_LENGTH"),
		},
		Postgres: PostgresConfig{
			DSN:             v.GetString("POSTGRES_DSN"),
			MaxConns:        int32(v.GetInt("POSTGRES_MAX_CONNS")),
			MinConns:        int32(v.GetInt("POSTGRES_MIN_CONNS")),
			MaxConnLifetime: v.GetDuration("POSTGRES_MAX_CONN_LIFETIME"),
		},
		LegacyMySQL: LegacyMySQLConfig{
			DSN:       v.GetString("LEGACY_MYSQL_DSN"),
			BatchSize: v.GetInt("LEGACY_MYSQL_BATCH_SIZE"),
		},
		Redis: RedisConfig{
			Addr:      v.GetString("REDIS_ADDR"),
			Password:  v.GetString("REDIS_PASSWORD"),
			DB:        v.GetInt("REDIS_DB"),
			KeyPrefix: v.GetString("REDIS_KEY_PREFIX"),
		},
		PaymentBillExport: PaymentBillExportConfig{
			RootDir:         v.GetString("PAYMENT_BILL_EXPORT_ROOT_DIR"),
			TTL:             v.GetDuration("PAYMENT_BILL_EXPORT_TTL"),
			CleanupInterval: v.GetDuration("PAYMENT_BILL_EXPORT_CLEANUP_INTERVAL"),
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
		OAuth: map[string]OAuthProviderConfig{},
	}

	setDefaults(&cfg)
	for name, defaults := range oauthDefaults {
		prefix := strings.ToUpper(name)
		cfg.OAuth[name] = OAuthProviderConfig{
			Name:         name,
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
	if cfg.NATS.URL == "" {
		return Config{}, fmt.Errorf("NATS_URL is required")
	}

	return cfg, nil
}

func loadEnvFile(v *viper.Viper) error {
	configFile, err := resolveEnvFilePath()
	if err != nil {
		return err
	}
	if configFile == "" {
		return nil
	}
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return fmt.Errorf("read env config %q: %w", configFile, err)
	}
	return nil
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
	cfg.Firewall = NormalizeFirewallConfig(cfg.Firewall)
	if cfg.Postgres.MaxConns == 0 {
		cfg.Postgres.MaxConns = 10
	}
	if cfg.Postgres.MinConns == 0 {
		cfg.Postgres.MinConns = 2
	}
	if cfg.Postgres.MaxConnLifetime == 0 {
		cfg.Postgres.MaxConnLifetime = 30 * time.Minute
	}
	if cfg.LegacyMySQL.BatchSize == 0 {
		cfg.LegacyMySQL.BatchSize = 500
	}
	if cfg.Redis.KeyPrefix == "" {
		cfg.Redis.KeyPrefix = "aegis"
	}
	if strings.TrimSpace(cfg.PaymentBillExport.RootDir) == "" {
		cfg.PaymentBillExport.RootDir = filepath.Join("data", "payment-bills")
	}
	if cfg.PaymentBillExport.TTL <= 0 {
		cfg.PaymentBillExport.TTL = 30 * time.Minute
	}
	if cfg.PaymentBillExport.CleanupInterval <= 0 {
		cfg.PaymentBillExport.CleanupInterval = 5 * time.Minute
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
		cfg.AutoSign.Timezone = "Asia/Shanghai"
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
	if cfg.AutoSign.RetryDelay == 0 {
		cfg.AutoSign.RetryDelay = 5 * time.Minute
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
