package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	AppName         string
	AppEnv          string
	HTTPPort        int
	AdminAPIToken   string
	AdminSessionTTL time.Duration
	AdminBootstrap  AdminBootstrapConfig
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	JWT             JWTConfig
	Firewall        FirewallConfig
	Postgres        PostgresConfig
	LegacyMySQL     LegacyMySQLConfig
	Redis           RedisConfig
	GeoIP           GeoIPConfig
	NATS            NATSConfig
	Temporal        TemporalConfig
	AutoSign        AutoSignConfig
	OAuth           map[string]OAuthProviderConfig
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
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	_ = v.ReadInConfig()

	cfg := Config{
		AppName:         v.GetString("APP_NAME"),
		AppEnv:          v.GetString("APP_ENV"),
		HTTPPort:        v.GetInt("HTTP_PORT"),
		AdminAPIToken:   v.GetString("ADMIN_API_TOKEN"),
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
	if cfg.JWT.Issuer == "" {
		cfg.JWT.Issuer = cfg.AppName
	}
	if cfg.JWT.TTL == 0 {
		cfg.JWT.TTL = 30 * 24 * time.Hour
	}
	if cfg.JWT.RefreshTTL == 0 {
		cfg.JWT.RefreshTTL = 7 * 24 * time.Hour
	}
	if strings.TrimSpace(cfg.Firewall.GlobalRate) == "" {
		cfg.Firewall.GlobalRate = "300-M"
	}
	if strings.TrimSpace(cfg.Firewall.AuthRate) == "" {
		cfg.Firewall.AuthRate = "30-M"
	}
	if strings.TrimSpace(cfg.Firewall.AdminRate) == "" {
		cfg.Firewall.AdminRate = "120-M"
	}
	if cfg.Firewall.CorazaParanoia <= 0 {
		cfg.Firewall.CorazaParanoia = 1
	}
	if cfg.Firewall.CorazaParanoia > 4 {
		cfg.Firewall.CorazaParanoia = 4
	}
	if cfg.Firewall.RequestBodyLimit <= 0 {
		cfg.Firewall.RequestBodyLimit = 13 * 1024 * 1024
	}
	if cfg.Firewall.RequestBodyMemory <= 0 {
		cfg.Firewall.RequestBodyMemory = 256 * 1024
	}
	if cfg.Firewall.RequestBodyMemory > cfg.Firewall.RequestBodyLimit {
		cfg.Firewall.RequestBodyMemory = cfg.Firewall.RequestBodyLimit
	}
	if cfg.Firewall.MaxPathLength <= 0 {
		cfg.Firewall.MaxPathLength = 1024
	}
	if cfg.Firewall.MaxQueryLength <= 0 {
		cfg.Firewall.MaxQueryLength = 2048
	}
	if len(cfg.Firewall.BlockedUserAgents) == 0 {
		cfg.Firewall.BlockedUserAgents = []string{"sqlmap", "nikto", "acunetix", "nessus", "wpscan", "gobuster", "dirbuster", "masscan", "nmap", "zgrab", "nuclei"}
	}
	if len(cfg.Firewall.BlockedPathPrefix) == 0 {
		cfg.Firewall.BlockedPathPrefix = []string{"/.env", "/.git", "/.svn", "/wp-admin", "/wp-login", "/phpmyadmin", "/vendor/phpunit", "/actuator", "/jmx-console", "/cgi-bin", "/autodiscover", "/server-status", "/solr", "/_ignition", "/hnap1"}
	}
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

func getBool(v *viper.Viper, key string, fallback bool) bool {
	if !v.IsSet(key) {
		return fallback
	}
	return v.GetBool(key)
}
