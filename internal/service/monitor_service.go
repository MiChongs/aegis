package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	storagedomain "aegis/internal/domain/storage"
	redisrepo "aegis/internal/repository/redis"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

const (
	monitorStatusAvailable   = "available"
	monitorStatusDegraded    = "degraded"
	monitorStatusUnavailable = "unavailable"
)

type MonitorComponent struct {
	Key       string         `json:"key"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Severity  string         `json:"severity"`
	Available bool           `json:"available"`
	Summary   string         `json:"summary"`
	Detail    string         `json:"detail"`
	CheckedAt time.Time      `json:"checkedAt"`
	DependsOn []string       `json:"dependsOn,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

type MonitorCounts struct {
	Total       int `json:"total"`
	Available   int `json:"available"`
	Degraded    int `json:"degraded"`
	Unavailable int `json:"unavailable"`
}

type MonitorRuntime struct {
	AppName       string    `json:"appName"`
	Environment   string    `json:"environment"`
	Port          int       `json:"port"`
	CheckedAt     time.Time `json:"checkedAt"`
	StartedAt     time.Time `json:"startedAt"`
	UptimeSeconds int64     `json:"uptimeSeconds"`
	Timezone      string    `json:"timezone"`

	// Go 运行时
	GoVersion     string `json:"goVersion"`
	GoOS          string `json:"goOS"`
	GoArch        string `json:"goArch"`
	Goroutines    int    `json:"goroutines"`
	CGOCalls      int64  `json:"cgoCalls"`

	// Go 堆内存
	MemAlloc      uint64 `json:"memAlloc"`
	MemTotalAlloc uint64 `json:"memTotalAlloc"`
	MemSys        uint64 `json:"memSys"`
	NumGC         uint32 `json:"numGC"`
	LastGCTime    int64  `json:"lastGCTime"`

	// 系统信息
	Hostname      string  `json:"hostname"`
	OS            string  `json:"os"`
	Platform      string  `json:"platform"`
	PlatformVer   string  `json:"platformVersion"`
	KernelArch    string  `json:"kernelArch"`
	KernelVer     string  `json:"kernelVersion"`

	// CPU
	CPUModel      string  `json:"cpuModel"`
	CPUCores      int     `json:"cpuCores"`
	CPUThreads    int     `json:"cpuThreads"`
	CPUUsage      float64 `json:"cpuUsage"`

	// 系统内存
	MemTotal      uint64  `json:"memTotal"`
	MemUsed       uint64  `json:"memUsed"`
	MemFree       uint64  `json:"memFree"`
	MemUsedPct    float64 `json:"memUsedPercent"`

	// 磁盘
	DiskTotal     uint64  `json:"diskTotal"`
	DiskUsed      uint64  `json:"diskUsed"`
	DiskFree      uint64  `json:"diskFree"`
	DiskUsedPct   float64 `json:"diskUsedPercent"`

	// 进程
	PID           int    `json:"pid"`
	ProcessMem    uint64 `json:"processMemory"`
}

type MonitorEndpoint struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Scope       string   `json:"scope"`
	Protected   bool     `json:"protected"`
	Status      string   `json:"status"`
	Summary     string   `json:"summary"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	Description string   `json:"description,omitempty"`
}

type MonitorAppBrief struct {
	ID               int64         `json:"id"`
	Name             string        `json:"name"`
	Status           string        `json:"status"`
	Score            int           `json:"score"`
	AvailabilityRate float64       `json:"availabilityRate"`
	Summary          string        `json:"summary"`
	CheckedAt        time.Time     `json:"checkedAt"`
	Counts           MonitorCounts `json:"counts"`
}

type MonitorOverview struct {
	Status           string             `json:"status"`
	Score            int                `json:"score"`
	AvailabilityRate float64            `json:"availabilityRate"`
	Summary          string             `json:"summary"`
	CheckedAt        time.Time          `json:"checkedAt"`
	Runtime          MonitorRuntime     `json:"runtime"`
	Counts           MonitorCounts      `json:"counts"`
	Highlights       []string           `json:"highlights,omitempty"`
	Endpoints        []MonitorEndpoint  `json:"endpoints,omitempty"`
	Applications     []MonitorAppBrief  `json:"applications,omitempty"`
	Infrastructure   []MonitorComponent `json:"infrastructure"`
	Modules          []MonitorComponent `json:"modules"`
	Components       []MonitorComponent `json:"components"`
}

type AppMonitorOverview struct {
	Status           string             `json:"status"`
	Score            int                `json:"score"`
	AvailabilityRate float64            `json:"availabilityRate"`
	Summary          string             `json:"summary"`
	CheckedAt        time.Time          `json:"checkedAt"`
	Runtime          MonitorRuntime     `json:"runtime"`
	Counts           MonitorCounts      `json:"counts"`
	App              map[string]any     `json:"app"`
	Highlights       []string           `json:"highlights,omitempty"`
	Metrics          map[string]any     `json:"metrics,omitempty"`
	Infrastructure   []MonitorComponent `json:"infrastructure"`
	Entrypoints      []MonitorComponent `json:"entrypoints,omitempty"`
	Modules          []MonitorComponent `json:"modules"`
	Components       []MonitorComponent `json:"components"`
}

type monitorFirewallRuntime interface {
	CurrentConfig() config.FirewallConfig
	ReloadMeta() (uint64, time.Time)
}

type MonitorService struct {
	cfg           config.Config
	log           *zap.Logger
	pg            *pgxpool.Pool
	redis         *redislib.Client
	nats          *nats.Conn
	temporal      client.Client
	auth          *AuthService
	admin         *AdminService
	user          *UserService
	signin        *SignInService
	points        *PointsService
	notifications *NotificationService
	app           *AppService
	site          *SiteService
	version       *VersionService
	roleApp       *RoleApplicationService
	email         *EmailService
	payment       *PaymentService
	workflow      *WorkflowService
	storage       *StorageService
	avatar        *AvatarService
	realtime      *RealtimeService
	firewall      monitorFirewallRuntime
	system        *PlatformSettingsService
	location      *LocationService
	monitorRepo   *redisrepo.MonitorRepository
	crashLog      interface{ Write(component string, r interface{}, recovered bool) }
	stopCollect   chan struct{}
	startedAt     time.Time
}

func NewMonitorService(
	cfg config.Config,
	log *zap.Logger,
	pg *pgxpool.Pool,
	redis *redislib.Client,
	natsConn *nats.Conn,
	temporalClient client.Client,
	auth *AuthService,
	admin *AdminService,
	user *UserService,
	signin *SignInService,
	points *PointsService,
	notifications *NotificationService,
	app *AppService,
	site *SiteService,
	version *VersionService,
	roleApp *RoleApplicationService,
	email *EmailService,
	payment *PaymentService,
	workflow *WorkflowService,
	storage *StorageService,
	avatar *AvatarService,
	realtime *RealtimeService,
	firewall monitorFirewallRuntime,
	system *PlatformSettingsService,
	location *LocationService,
) *MonitorService {
	if log == nil {
		log = zap.NewNop()
	}
	return &MonitorService{
		cfg:           cfg,
		log:           log,
		pg:            pg,
		redis:         redis,
		nats:          natsConn,
		temporal:      temporalClient,
		auth:          auth,
		admin:         admin,
		user:          user,
		signin:        signin,
		points:        points,
		notifications: notifications,
		app:           app,
		site:          site,
		version:       version,
		roleApp:       roleApp,
		email:         email,
		payment:       payment,
		workflow:      workflow,
		storage:       storage,
		avatar:        avatar,
		realtime:      realtime,
		firewall:      firewall,
		system:        system,
		location:      location,
		startedAt:     time.Now().UTC(),
	}
}

func (s *MonitorService) SystemOverview(ctx context.Context) (*MonitorOverview, error) {
	checkedAt := time.Now().UTC()
	infrastructure := s.checkInfrastructure(ctx, checkedAt)
	infraStatus := componentsToStatusMap(infrastructure)
	modules := append(s.buildSystemModules(infraStatus, checkedAt), s.buildControlPlaneModules(ctx, infraStatus, checkedAt)...)
	components := append(append([]MonitorComponent{}, infrastructure...), modules...)
	status, score, availabilityRate, counts := summarizeComponents(components)
	applications, err := s.listAppBriefs(ctx, checkedAt)
	if err != nil {
		s.log.Debug("load monitor app briefs failed", zap.Error(err))
	}
	return &MonitorOverview{
		Status:           status,
		Score:            score,
		AvailabilityRate: availabilityRate,
		Summary:          buildSystemSummary(counts),
		CheckedAt:        checkedAt,
		Runtime:          s.runtimeSnapshot(checkedAt),
		Counts:           counts,
		Highlights:       buildHighlights(components),
		Endpoints:        s.buildSystemEndpoints(),
		Applications:     applications,
		Infrastructure:   infrastructure,
		Modules:          modules,
		Components:       components,
	}, nil
}

func (s *MonitorService) AppOverview(ctx context.Context, appID int64) (*AppMonitorOverview, error) {
	checkedAt := time.Now().UTC()
	if s.app == nil {
		return nil, fmt.Errorf("app service unavailable")
	}
	appItem, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}

	infrastructure := s.checkInfrastructure(ctx, checkedAt)
	infraStatus := componentsToStatusMap(infrastructure)

	emailConfigs, emailErr := s.safeEmailConfigs(ctx, appID)
	paymentConfigs, paymentErr := s.safePaymentConfigs(ctx, appID)
	storageGlobalConfigs, storageGlobalErr := s.safeStorageConfigs(ctx, storagedomain.ScopeGlobal, nil)
	storageAppConfigs, storageAppErr := s.safeStorageConfigs(ctx, storagedomain.ScopeApp, &appID)
	policyView, policyErr := s.safeAppPolicy(ctx, appID)
	passwordPolicyView, passwordPolicyErr := s.safePasswordPolicy(ctx, appID)
	authSources, authSourcesErr := s.safeAppAuthSources(ctx, appID)

	var appStats *appdomain.Stats
	if s.app != nil {
		stats, statsErr := s.app.GetStats(ctx, appID)
		if statsErr == nil {
			appStats = stats
		} else {
			s.log.Debug("load app monitor stats failed", zap.Int64("appid", appID), zap.Error(statsErr))
		}
	}

	var onlineStats any
	if s.realtime != nil && infraStatus["redis"] == monitorStatusAvailable {
		stats, realtimeErr := s.realtime.AppOnlineStats(ctx, appID)
		if realtimeErr == nil {
			onlineStats = stats
		} else {
			s.log.Debug("load app online stats failed", zap.Int64("appid", appID), zap.Error(realtimeErr))
		}
	}

	transportPolicy := appdomain.TransportEncryptionPolicy{}
	if s.app != nil {
		transportPolicy = s.app.ResolveTransportEncryption(appItem)
	}

	appConfig := appModuleConfigCounts{
		emailTotal:         len(emailConfigs),
		emailEnabled:       countEnabledEmailConfigs(emailConfigs),
		emailLoadError:     emailErr != nil,
		paymentTotal:       len(paymentConfigs),
		paymentEnabled:     countEnabledPaymentConfigs(paymentConfigs),
		paymentLoadError:   paymentErr != nil,
		storageTotal:       len(storageGlobalConfigs) + len(storageAppConfigs),
		storageEnabled:     countEnabledStorageConfigs(storageGlobalConfigs) + countEnabledStorageConfigs(storageAppConfigs),
		storageAppTotal:    len(storageAppConfigs),
		storageGlobalTotal: len(storageGlobalConfigs),
		storageLoadError:   storageGlobalErr != nil || storageAppErr != nil,
		onlineStats:        onlineStats,
		appStats:           appStats,
		policy:             policyView,
		policyLoadError:    policyErr != nil,
		passwordPolicy:     passwordPolicyView,
		passwordLoadError:  passwordPolicyErr != nil,
		transportPolicy:    transportPolicy,
		authSources:        authSources,
		authSourcesError:   authSourcesErr != nil,
	}

	entrypoints := s.buildAppEntrypoints(appItem, infraStatus, checkedAt, appConfig)
	modules := append(s.buildAppModules(appItem, infraStatus, checkedAt, appConfig), s.buildAppExtensionModules(appItem, infraStatus, checkedAt, appConfig)...)
	components := append(append(append([]MonitorComponent{}, infrastructure...), entrypoints...), modules...)
	status, score, availabilityRate, counts := summarizeComponents(components)

	appPayload := map[string]any{
		"id":                     appItem.ID,
		"name":                   appItem.Name,
		"status":                 appItem.Status,
		"registerStatus":         appItem.RegisterStatus,
		"loginStatus":            appItem.LoginStatus,
		"disabledReason":         appItem.DisabledReason,
		"disabledRegisterReason": appItem.DisabledRegisterReason,
		"disabledLoginReason":    appItem.DisabledLoginReason,
	}

	metrics := map[string]any{
		"configs": map[string]any{
			"email":   map[string]any{"total": len(emailConfigs), "enabled": countEnabledEmailConfigs(emailConfigs)},
			"payment": map[string]any{"total": len(paymentConfigs), "enabled": countEnabledPaymentConfigs(paymentConfigs)},
			"storage": map[string]any{
				"total":          len(storageGlobalConfigs) + len(storageAppConfigs),
				"enabled":        countEnabledStorageConfigs(storageGlobalConfigs) + countEnabledStorageConfigs(storageAppConfigs),
				"appScoped":      len(storageAppConfigs),
				"platformScoped": len(storageGlobalConfigs),
			},
		},
	}
	if appStats != nil {
		metrics["users"] = map[string]any{
			"total":          appStats.TotalUsers,
			"enabled":        appStats.EnabledUsers,
			"disabled":       appStats.DisabledUsers,
			"newUsersToday":  appStats.NewUsersToday,
			"loginSuccesses": appStats.LoginSuccessToday,
			"loginFailures":  appStats.LoginFailureToday,
			"bannerCount":    appStats.BannerCount,
			"noticeCount":    appStats.NoticeCount,
			"oauthBindings":  appStats.OAuthBindCount,
		}
	}
	if onlineStats != nil {
		metrics["realtime"] = onlineStats
	}
	if policyView != nil {
		metrics["securityPolicy"] = policyView
	}
	if passwordPolicyView != nil {
		metrics["passwordPolicy"] = passwordPolicyView
	}
	metrics["transportEncryption"] = map[string]any{
		"enabled":            transportPolicy.Enabled,
		"strict":             transportPolicy.Strict,
		"responseEncryption": transportPolicy.ResponseEncryption,
	}
	if authSources != nil {
		metrics["authSources"] = authSources
	}

	return &AppMonitorOverview{
		Status:           status,
		Score:            score,
		AvailabilityRate: availabilityRate,
		Summary:          buildAppSummary(appItem, counts),
		CheckedAt:        checkedAt,
		Runtime:          s.runtimeSnapshot(checkedAt),
		Counts:           counts,
		App:              appPayload,
		Highlights:       buildHighlights(components),
		Metrics:          metrics,
		Infrastructure:   infrastructure,
		Entrypoints:      entrypoints,
		Modules:          modules,
		Components:       components,
	}, nil
}

type infraCheckResult struct {
	key       string
	component MonitorComponent
}

type appModuleConfigCounts struct {
	emailTotal         int
	emailEnabled       int
	emailLoadError     bool
	paymentTotal       int
	paymentEnabled     int
	paymentLoadError   bool
	storageTotal       int
	storageEnabled     int
	storageAppTotal    int
	storageGlobalTotal int
	storageLoadError   bool
	onlineStats        any
	appStats           *appdomain.Stats
	policy             *appdomain.Policy
	policyLoadError    bool
	passwordPolicy     *appdomain.PasswordPolicyView
	passwordLoadError  bool
	transportPolicy    appdomain.TransportEncryptionPolicy
	authSources        *appdomain.AuthSourceStats
	authSourcesError   bool
}

func (s *MonitorService) checkInfrastructure(ctx context.Context, checkedAt time.Time) []MonitorComponent {
	checks := []func(context.Context, time.Time) infraCheckResult{
		s.checkPostgres,
		s.checkRedis,
		s.checkNATS,
		s.checkTemporal,
	}
	results := make([]MonitorComponent, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(index int, fn func(context.Context, time.Time) infraCheckResult) {
			defer wg.Done()
			results[index] = fn(ctx, checkedAt).component
		}(i, check)
	}
	wg.Wait()
	return results
}

func (s *MonitorService) buildSystemModules(infra map[string]string, checkedAt time.Time) []MonitorComponent {
	return []MonitorComponent{
		s.composeModule("admin", "管理员服务", checkedAt, s.admin != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, "管理员认证、后台会话与控制台能力"),
		s.composeModule("auth", "认证服务", checkedAt, s.auth != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, "登录、注册、令牌刷新与访问校验"),
		s.composeModule("app", "应用管理", checkedAt, s.app != nil, []string{infra["postgres"]}, []string{"postgres", "redis"}, "多应用配置、公共内容与策略解析"),
		s.composeModule("user", "用户服务", checkedAt, s.user != nil, []string{infra["postgres"]}, []string{"postgres", "redis", "nats"}, "用户资料、会话与安全信息"),
		s.composeModule("signin", "签到服务", checkedAt, s.signin != nil, []string{infra["postgres"]}, []string{"postgres", "nats"}, "签到状态、历史记录与积分发放"),
		s.composeModule("points", "积分等级", checkedAt, s.points != nil, []string{infra["postgres"]}, []string{"postgres", "redis"}, "积分、经验值与排行"),
		s.composeModule("notifications", "通知中心", checkedAt, s.notifications != nil, []string{infra["postgres"]}, []string{"postgres", "redis", "nats"}, "站内通知、已读状态与推送"),
		s.composeModule("site", "站点服务", checkedAt, s.site != nil, []string{infra["postgres"]}, []string{"postgres"}, "站点收录、审核与管理"),
		s.composeModule("version", "版本管理", checkedAt, s.version != nil, []string{infra["postgres"]}, []string{"postgres"}, "版本发布、渠道与分发"),
		s.composeModule("role_application", "角色申请", checkedAt, s.roleApp != nil, []string{infra["postgres"]}, []string{"postgres"}, "角色申请、审核与审批流转"),
		s.composeModule("email", "邮件系统", checkedAt, s.email != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, "验证码、找回密码与邮件通道"),
		s.composeModule("payment", "支付系统", checkedAt, s.payment != nil, []string{infra["postgres"]}, []string{"postgres"}, "支付配置、订单与回调处理"),
		s.composeModule("workflow", "工作流", checkedAt, s.workflow != nil, []string{infra["postgres"], infra["temporal"]}, []string{"postgres", "temporal"}, "自动化流程、编排任务与实例执行"),
		s.composeModule("storage", "存储管理", checkedAt, s.storage != nil, []string{infra["postgres"]}, []string{"postgres", "redis"}, "对象存储、代理下载与多存储配置"),
		s.composeModule("avatar", "头像服务", checkedAt, s.avatar != nil, []string{moduleStatusFromService(s.storage != nil)}, []string{"storage"}, "头像上传、外链生成与统一头像入口"),
		s.composeModule("realtime", "实时通信", checkedAt, s.realtime != nil, []string{infra["redis"]}, []string{"redis", "nats"}, "全局 WebSocket、在线状态与事件广播"),
		s.composeModule("location", "IP 定位", checkedAt, s.location != nil, nil, []string{"redis"}, "Geo 数据库定位与异步地址解析"),
	}
}

func (s *MonitorService) buildAppModules(appItem *appdomain.App, infra map[string]string, checkedAt time.Time, cfg appModuleConfigCounts) []MonitorComponent {
	modules := make([]MonitorComponent, 0, 12)
	appCoreStatus := appAvailabilityStatus(appItem)
	modules = append(modules, MonitorComponent{
		Key:       "app_core",
		Name:      "应用主体",
		Status:    appCoreStatus,
		Severity:  severityFromStatus(appCoreStatus),
		Available: appCoreStatus == monitorStatusAvailable,
		Summary:   appCoreSummary(appItem),
		Detail:    appCoreDetail(appItem),
		CheckedAt: checkedAt,
		Meta: map[string]any{
			"appName":                appItem.Name,
			"loginStatus":            appItem.LoginStatus,
			"registerStatus":         appItem.RegisterStatus,
			"disabledReason":         appItem.DisabledReason,
			"disabledLoginReason":    appItem.DisabledLoginReason,
			"disabledRegisterReason": appItem.DisabledRegisterReason,
		},
	})
	modules = append(modules, s.composeAppGateModule("auth_login", "登录入口", checkedAt, s.auth != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, appItem.Status, appItem.LoginStatus, appItem.DisabledReason, appItem.DisabledLoginReason))
	modules = append(modules, s.composeAppGateModule("registration", "注册入口", checkedAt, s.auth != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, appItem.Status, appItem.RegisterStatus, appItem.DisabledReason, appItem.DisabledRegisterReason))
	modules = append(modules, s.composeAppModule("signin", "签到服务", checkedAt, s.signin != nil, []string{infra["postgres"]}, []string{"postgres", "nats"}, appItem.Status, appItem.DisabledReason, "应用签到与每日奖励"))
	modules = append(modules, s.composeAppModule("points", "积分等级", checkedAt, s.points != nil, []string{infra["postgres"]}, []string{"postgres", "redis"}, appItem.Status, appItem.DisabledReason, "积分、等级与排行榜"))
	modules = append(modules, s.composeAppModule("notifications", "通知中心", checkedAt, s.notifications != nil, []string{infra["postgres"]}, []string{"postgres", "redis", "nats"}, appItem.Status, appItem.DisabledReason, "用户消息与站内通知"))
	modules = append(modules, s.composeConfigModule("email", "邮件通道", checkedAt, s.email != nil, []string{infra["postgres"], infra["redis"]}, []string{"postgres", "redis"}, cfg.emailTotal, cfg.emailEnabled, cfg.emailLoadError, "邮件验证码、找回密码与系统通知"))
	modules = append(modules, s.composeConfigModule("payment", "支付通道", checkedAt, s.payment != nil, []string{infra["postgres"]}, []string{"postgres"}, cfg.paymentTotal, cfg.paymentEnabled, cfg.paymentLoadError, "支付订单、回调与收款配置"))
	storageModule := s.composeConfigModule("storage", "存储管理", checkedAt, s.storage != nil, []string{infra["postgres"]}, []string{"postgres", "redis"}, cfg.storageTotal, cfg.storageEnabled, cfg.storageLoadError, "对象存储、私有代理与资源访问")
	storageModule.Meta = map[string]any{
		"totalConfigs":   cfg.storageTotal,
		"enabledConfigs": cfg.storageEnabled,
		"appScoped":      cfg.storageAppTotal,
		"platformScoped": cfg.storageGlobalTotal,
	}
	modules = append(modules, storageModule)
	workflowModule := s.composeAppModule("workflow", "工作流", checkedAt, s.workflow != nil, []string{infra["postgres"], infra["temporal"]}, []string{"postgres", "temporal"}, appItem.Status, appItem.DisabledReason, "自动化流程、审批与定时编排")
	if s.workflow != nil {
		workflowModule.Meta = s.workflow.EngineStatus()
	}
	modules = append(modules, workflowModule)
	realtimeModule := s.composeAppModule("realtime", "实时通信", checkedAt, s.realtime != nil, []string{infra["redis"]}, []string{"redis", "nats"}, appItem.Status, appItem.DisabledReason, "WebSocket、在线用户与事件推送")
	if cfg.onlineStats != nil {
		realtimeModule.Meta = map[string]any{"online": cfg.onlineStats}
	}
	modules = append(modules, realtimeModule)
	modules = append(modules, s.composeAppModule("avatar", "头像服务", checkedAt, s.avatar != nil, []string{moduleStatusFromConfig(cfg.storageEnabled > 0)}, []string{"storage"}, appItem.Status, appItem.DisabledReason, "头像上传、头像地址与默认头像策略"))
	modules = append(modules, s.composeAppModule("location", "IP 定位", checkedAt, s.location != nil, nil, []string{"redis"}, true, "", "Geo 定位与地区信息补充"))
	return modules
}

func (s *MonitorService) checkPostgres(ctx context.Context, checkedAt time.Time) infraCheckResult {
	startedAt := time.Now()
	component := MonitorComponent{Key: "postgres", Name: "PostgreSQL", Status: monitorStatusUnavailable, Severity: "critical", Summary: "连接异常", Detail: "主数据库不可用。", CheckedAt: checkedAt}
	if s.pg == nil {
		component.Meta = map[string]any{"durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "postgres", component: component}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	var ping int
	if err := s.pg.QueryRow(timeoutCtx, "SELECT 1").Scan(&ping); err != nil {
		component.Detail = "查询失败。"
		component.Meta = map[string]any{"error": err.Error(), "durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "postgres", component: component}
	}
	component.Status = monitorStatusAvailable
	component.Severity = "info"
	component.Available = true
	component.Summary = "连接正常"
	component.Detail = "连接池可用，查询通过。"
	component.Meta = map[string]any{"pool": s.pg.Stat().TotalConns(), "durationMs": time.Since(startedAt).Milliseconds()}
	return infraCheckResult{key: "postgres", component: component}
}

func (s *MonitorService) checkRedis(ctx context.Context, checkedAt time.Time) infraCheckResult {
	startedAt := time.Now()
	component := MonitorComponent{Key: "redis", Name: "Redis", Status: monitorStatusUnavailable, Severity: "critical", Summary: "连接异常", Detail: "缓存服务不可用。", CheckedAt: checkedAt}
	if s.redis == nil {
		component.Meta = map[string]any{"durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "redis", component: component}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := s.redis.Ping(timeoutCtx).Err(); err != nil {
		component.Meta = map[string]any{"error": err.Error(), "durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "redis", component: component}
	}
	component.Status = monitorStatusAvailable
	component.Severity = "info"
	component.Available = true
	component.Summary = "响应正常"
	component.Detail = "PING 通过。"
	component.Meta = map[string]any{"durationMs": time.Since(startedAt).Milliseconds()}
	return infraCheckResult{key: "redis", component: component}
}
func (s *MonitorService) checkNATS(ctx context.Context, checkedAt time.Time) infraCheckResult {
	startedAt := time.Now()
	component := MonitorComponent{Key: "nats", Name: "NATS", Status: monitorStatusUnavailable, Severity: "warning", Summary: "连接异常", Detail: "事件总线不可用。", CheckedAt: checkedAt}
	if s.nats == nil {
		component.Meta = map[string]any{"durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "nats", component: component}
	}
	if !s.nats.IsConnected() {
		component.Meta = map[string]any{"status": s.nats.Status().String(), "durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "nats", component: component}
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if err := s.nats.FlushWithContext(timeoutCtx); err != nil {
		component.Status = monitorStatusDegraded
		component.Severity = "warning"
		component.Summary = "状态波动"
		component.Detail = "连接存在波动。"
		component.Meta = map[string]any{"error": err.Error(), "status": s.nats.Status().String(), "durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "nats", component: component}
	}
	component.Status = monitorStatusAvailable
	component.Severity = "info"
	component.Available = true
	component.Summary = "连接正常"
	component.Detail = "Flush 通过。"
	component.Meta = map[string]any{"status": s.nats.Status().String(), "durationMs": time.Since(startedAt).Milliseconds()}
	return infraCheckResult{key: "nats", component: component}
}

func (s *MonitorService) checkTemporal(_ context.Context, checkedAt time.Time) infraCheckResult {
	startedAt := time.Now()
	component := MonitorComponent{Key: "temporal", Name: "Temporal", Status: monitorStatusUnavailable, Severity: "warning", Summary: "连接异常", Detail: "工作流引擎不可用。", CheckedAt: checkedAt}
	if s.temporal == nil || s.workflow == nil {
		component.Meta = map[string]any{"durationMs": time.Since(startedAt).Milliseconds()}
		return infraCheckResult{key: "temporal", component: component}
	}
	engineStatus := s.workflow.EngineStatus()
	connected, _ := engineStatus["connected"].(bool)
	if !connected {
		engineStatus["durationMs"] = time.Since(startedAt).Milliseconds()
		component.Meta = engineStatus
		return infraCheckResult{key: "temporal", component: component}
	}
	component.Status = monitorStatusAvailable
	component.Severity = "info"
	component.Available = true
	component.Summary = "已连接"
	component.Detail = "客户端可用。"
	engineStatus["durationMs"] = time.Since(startedAt).Milliseconds()
	component.Meta = engineStatus
	return infraCheckResult{key: "temporal", component: component}
}

func (s *MonitorService) composeModule(key string, name string, checkedAt time.Time, serviceReady bool, required []string, dependencies []string, detail string) MonitorComponent {
	status := evaluateStatus(serviceReady, required, nil)
	return MonitorComponent{Key: key, Name: name, Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: moduleSummary(name, status), Detail: detail, CheckedAt: checkedAt, DependsOn: dependencies}
}

func (s *MonitorService) composeAppModule(key string, name string, checkedAt time.Time, serviceReady bool, required []string, dependencies []string, appEnabled bool, disabledReason string, detail string) MonitorComponent {
	status := evaluateStatus(serviceReady, required, nil)
	component := MonitorComponent{Key: key, Name: name, Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: moduleSummary(name, status), Detail: detail, CheckedAt: checkedAt, DependsOn: dependencies}
	if !appEnabled {
		component.Status = monitorStatusUnavailable
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "应用已停用"
		component.Detail = pickReason(disabledReason, "应用主体处于停用状态，相关能力不会对外提供。")
	}
	return component
}

func (s *MonitorService) composeAppGateModule(key string, name string, checkedAt time.Time, serviceReady bool, required []string, dependencies []string, appEnabled bool, gateEnabled bool, appReason string, gateReason string) MonitorComponent {
	status := evaluateStatus(serviceReady, required, nil)
	component := MonitorComponent{Key: key, Name: name, Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: moduleSummary(name, status), Detail: "该入口状态由应用总开关、业务开关与认证服务共同决定。", CheckedAt: checkedAt, DependsOn: dependencies}
	if !appEnabled {
		component.Status = monitorStatusUnavailable
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "应用已停用"
		component.Detail = pickReason(appReason, "应用主体已停用，该入口不会开放。")
		return component
	}
	if !gateEnabled {
		component.Status = monitorStatusUnavailable
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "入口已关闭"
		component.Detail = pickReason(gateReason, "当前入口已关闭，不对外提供服务。")
	}
	return component
}

func (s *MonitorService) composeConfigModule(key string, name string, checkedAt time.Time, serviceReady bool, required []string, dependencies []string, total int, enabled int, loadError bool, detail string) MonitorComponent {
	status := evaluateStatus(serviceReady, required, nil)
	component := MonitorComponent{Key: key, Name: name, Status: status, Severity: severityFromStatus(status), Available: status == monitorStatusAvailable, Summary: moduleSummary(name, status), Detail: detail, CheckedAt: checkedAt, DependsOn: dependencies, Meta: map[string]any{"totalConfigs": total, "enabledConfigs": enabled}}
	if loadError {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "配置读取异常"
		component.Detail = "模块已装配，但当前配置状态无法完整读取。"
		return component
	}
	if total == 0 {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "尚未配置"
		component.Detail = "模块服务已装配，但当前应用尚未配置可用通道。"
		return component
	}
	if enabled == 0 {
		component.Status = monitorStatusDegraded
		component.Severity = severityFromStatus(component.Status)
		component.Available = false
		component.Summary = "配置存在但未启用"
		component.Detail = "检测到配置记录，但尚未启用任何可用通道。"
	}
	return component
}

func (s *MonitorService) safeEmailConfigs(ctx context.Context, appID int64) ([]struct{ Enabled bool }, error) {
	if s.email == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	items, err := s.email.ListConfigs(timeoutCtx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]struct{ Enabled bool }, 0, len(items))
	for _, item := range items {
		out = append(out, struct{ Enabled bool }{Enabled: item.Enabled})
	}
	return out, nil
}

func (s *MonitorService) safePaymentConfigs(ctx context.Context, appID int64) ([]struct{ Enabled bool }, error) {
	if s.payment == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	items, err := s.payment.ListConfigs(timeoutCtx, appID, "", false)
	if err != nil {
		return nil, err
	}
	out := make([]struct{ Enabled bool }, 0, len(items))
	for _, item := range items {
		out = append(out, struct{ Enabled bool }{Enabled: item.Enabled})
	}
	return out, nil
}

func (s *MonitorService) safeStorageConfigs(ctx context.Context, scope string, appID *int64) ([]storagedomain.Config, error) {
	if s.storage == nil {
		return nil, nil
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	return s.storage.ListConfigs(timeoutCtx, scope, appID, "")
}
func evaluateStatus(serviceReady bool, required []string, optional []string) string {
	if !serviceReady {
		return monitorStatusUnavailable
	}
	for _, status := range required {
		if status == monitorStatusUnavailable {
			return monitorStatusUnavailable
		}
	}
	for _, status := range required {
		if status == monitorStatusDegraded {
			return monitorStatusDegraded
		}
	}
	for _, status := range optional {
		if status != monitorStatusAvailable && status != "" {
			return monitorStatusDegraded
		}
	}
	return monitorStatusAvailable
}

func summarizeComponents(components []MonitorComponent) (string, int, float64, MonitorCounts) {
	counts := MonitorCounts{Total: len(components)}
	scoreSum := 0
	for _, component := range components {
		switch component.Status {
		case monitorStatusAvailable:
			counts.Available++
			scoreSum += 100
		case monitorStatusDegraded:
			counts.Degraded++
			scoreSum += 60
		default:
			counts.Unavailable++
		}
	}
	status := monitorStatusAvailable
	if counts.Unavailable > 0 {
		status = monitorStatusUnavailable
	} else if counts.Degraded > 0 {
		status = monitorStatusDegraded
	}
	return status, normalizedScore(scoreSum, len(components)), availabilityRate(counts), counts
}

func normalizedScore(scoreSum int, total int) int {
	if total == 0 {
		return 100
	}
	return int(math.Round(float64(scoreSum) / float64(total)))
}

func availabilityRate(counts MonitorCounts) float64 {
	if counts.Total == 0 {
		return 100
	}
	return math.Round((float64(counts.Available)/float64(counts.Total))*10000) / 100
}

func buildSystemSummary(counts MonitorCounts) string {
	if counts.Total == 0 {
		return "当前没有可供监测的系统组件。"
	}
	return fmt.Sprintf("共 %d 项，正常 %d，降级 %d，不可用 %d。", counts.Total, counts.Available, counts.Degraded, counts.Unavailable)
}

func buildAppSummary(appItem *appdomain.App, counts MonitorCounts) string {
	if !appItem.Status {
		return fmt.Sprintf("应用“%s”已停用。", appItem.Name)
	}
	return fmt.Sprintf("应用“%s”：正常 %d，降级 %d，不可用 %d。", appItem.Name, counts.Available, counts.Degraded, counts.Unavailable)
}

func buildHighlights(components []MonitorComponent) []string {
	highlights := make([]string, 0, 4)
	for _, component := range components {
		if component.Status == monitorStatusUnavailable {
			highlights = append(highlights, fmt.Sprintf("%s：%s", component.Name, component.Summary))
		}
		if len(highlights) >= 4 {
			return highlights
		}
	}
	for _, component := range components {
		if component.Status == monitorStatusDegraded {
			highlights = append(highlights, fmt.Sprintf("%s：%s", component.Name, component.Summary))
		}
		if len(highlights) >= 4 {
			break
		}
	}
	return highlights
}

func componentsToStatusMap(components []MonitorComponent) map[string]string {
	result := make(map[string]string, len(components))
	for _, component := range components {
		result[component.Key] = component.Status
	}
	return result
}

func severityFromStatus(status string) string {
	switch status {
	case monitorStatusAvailable:
		return "info"
	case monitorStatusDegraded:
		return "warning"
	default:
		return "critical"
	}
}

func moduleSummary(name string, status string) string {
	switch status {
	case monitorStatusAvailable:
		return "正常"
	case monitorStatusDegraded:
		return "降级"
	default:
		return "不可用"
	}
}

func appAvailabilityStatus(appItem *appdomain.App) string {
	if appItem == nil || !appItem.Status {
		return monitorStatusUnavailable
	}
	if !appItem.LoginStatus || !appItem.RegisterStatus {
		return monitorStatusDegraded
	}
	return monitorStatusAvailable
}

func appCoreSummary(appItem *appdomain.App) string {
	if appItem == nil {
		return "应用不存在"
	}
	if !appItem.Status {
		return "应用已停用"
	}
	if !appItem.LoginStatus || !appItem.RegisterStatus {
		return "部分受限"
	}
	return "正常"
}

func appCoreDetail(appItem *appdomain.App) string {
	if appItem == nil {
		return "未查询到对应应用。"
	}
	if !appItem.Status {
		return pickReason(appItem.DisabledReason, "应用已停用。")
	}
	if !appItem.LoginStatus {
		return pickReason(appItem.DisabledLoginReason, "登录已关闭。")
	}
	if !appItem.RegisterStatus {
		return pickReason(appItem.DisabledRegisterReason, "注册已关闭。")
	}
	return "状态正常。"
}

func pickReason(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func moduleStatusFromService(ok bool) string {
	if ok {
		return monitorStatusAvailable
	}
	return monitorStatusUnavailable
}

func moduleStatusFromConfig(ok bool) string {
	if ok {
		return monitorStatusAvailable
	}
	return monitorStatusDegraded
}

func countEnabledEmailConfigs(items []struct{ Enabled bool }) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func countEnabledPaymentConfigs(items []struct{ Enabled bool }) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}

func countEnabledStorageConfigs(items []storagedomain.Config) int {
	count := 0
	for _, item := range items {
		if item.Enabled {
			count++
		}
	}
	return count
}







