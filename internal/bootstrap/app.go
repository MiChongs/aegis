package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis/internal/authz"
	"aegis/internal/config"
	"aegis/internal/db"
	"aegis/internal/event"
	"aegis/internal/middleware"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/mailtpl"
	"aegis/internal/service"
	httptransport "aegis/internal/transport/http"
	"aegis/pkg/clientip"
	"aegis/pkg/crashlog"
	"aegis/pkg/egress"
	"aegis/pkg/httpserve"
	pkglogger "aegis/pkg/logger"
	"aegis/pkg/tracing"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

type APIApp struct {
	Config          config.Config
	ConfigManager   *config.Manager
	Logger          *zap.Logger
	CrashLog        *crashlog.Logger
	Router          *gin.Engine
	Server          *http.Server
	Secure          *httpserve.Server // TLS_ENABLED 时的 HTTPS + HTTP/3 监听，由 Serve 建立；未开启时为 nil
	Postgres        *pgxpool.Pool
	Redis           *redislib.Client
	NATSConn        *nats.Conn
	JetStream       nats.JetStreamContext
	Temporal        client.Client
	Realtime        *service.RealtimeService
	OnlineLive      *service.OnlineLiveHub
	Payment         *service.PaymentService
	AutoSign        *service.AutoSignService
	SessionMgmt     *service.SessionMgmtService
	AdminUserSearch *service.AdminUserSearchService
	AccountBan      *service.AccountBanService
	Location        *service.LocationService
	Monitor         *service.MonitorService
	Memory          *service.MemoryManager
	Database        *service.DatabaseManager
	PostgresHandle  *db.Postgres
	// ClientIP 真实客户端 IP 判定器。装配期就要建好并留在这里：
	// 横幅要把生效的判定方式说出来，否则「线上 IP 不对」只能靠猜配置。
	ClientIP *clientip.Resolver
	Firewall *middleware.Firewall
	Security *service.SecurityService
	Risk     *service.RiskService
	// Governance 平台治理：后台循环负责到期解冻与跨实例快照收敛，退出时必须 Stop
	Governance *service.PlatformGovernanceService
	// CloudStorage 用户云存储：后台循环清理过期回收站，退出时必须 Stop
	CloudStorage *service.CloudStorageService
	// Storage 存储中心：存储池探活、用量刷新、迁移任务的后台循环。
	Storage *service.StorageService
	// AccountDeletion 账号注销：后台循环执行到期的冷静期申请并发到期提醒，退出时必须 Stop
	AccountDeletion *service.AccountDeletionService
	// Leaderboard 积分排行榜：后台循环结算到期的周期榜并发放奖励，退出时必须 Stop
	Leaderboard *service.LeaderboardService
	// ItemGrant 物品发放：后台循环执行到点的定时规则、接管中断的执行，退出时必须 Stop
	ItemGrant *service.ItemGrantService
	// Level 等级体系：后台循环发放达成奖励、发送升级通知、接管中断的重算任务，退出时必须 Stop
	Level *service.LevelService
	// AIGateway AI 中转网关：后台循环放回超时未结算的暂扣，退出时必须 Stop
	AIGateway *service.AIGatewayService
	// AuthProviderHealth 后台 30s 轮询 LDAP/OIDC/SAML 可用性；进程退出时必须 Close() 避免 goroutine 泄漏
	AuthProviderHealth *service.AuthProviderHealthService
	// Egress 出海代理网关管理面；EgressGateway 是进程级单例，
	// OwnsEgress 标记本实例是否负责它的启停（Unified 模式下只有 API 侧负责）
	Egress          *service.EgressService
	EgressGateway   *egress.Gateway
	OwnsEgress      bool
	ShutdownTracing func(context.Context) error

	// closeOnce 保证 Close 只执行一次（信号路径与 fx OnStop 都可能调用）。
	closeOnce sync.Once
}

func NewAPIApp(ctx context.Context, cl *crashlog.Logger) (*APIApp, error) {
	manager, err := config.NewManager()
	if err != nil {
		return nil, err
	}
	return NewAPIAppWithConfigManager(ctx, cl, manager)
}

func NewAPIAppWithConfigManager(ctx context.Context, cl *crashlog.Logger, manager *config.Manager) (*APIApp, error) {
	if manager == nil {
		var err error
		manager, err = config.NewManager()
		if err != nil {
			return nil, err
		}
	}
	cfg := manager.Current()
	initGinMode(cfg.AppEnv)
	log, err := pkglogger.New(cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	tracingShutdown, err := tracing.Init(ctx, tracing.Config{
		Enabled:        cfg.Tracing.Enabled,
		ServiceName:    cfg.Tracing.ServiceName,
		ServiceVersion: cfg.Tracing.ServiceVersion,
		Environment:    cfg.Tracing.Environment,
		Exporter:       cfg.Tracing.Exporter,
		Endpoint:       cfg.Tracing.Endpoint,
		Insecure:       cfg.Tracing.Insecure,
		Headers:        cfg.Tracing.Headers,
		Sampler:        cfg.Tracing.Sampler,
		SampleRatio:    cfg.Tracing.SampleRatio,
		BatchTimeout:   cfg.Tracing.BatchTimeout,
		ExportTimeout:  cfg.Tracing.ExportTimeout,
	})
	if err != nil {
		log.Warn("tracing 初始化失败，降级为空 exporter", zap.Error(err))
	}
	log.Info("tracing 已启用",
		zap.Bool("enabled", cfg.Tracing.Enabled),
		zap.String("exporter", cfg.Tracing.Exporter),
		zap.String("sampler", cfg.Tracing.Sampler),
		zap.String("endpoint", cfg.Tracing.Endpoint),
		zap.String("service", cfg.Tracing.ServiceName),
	)
	shutdownTracing := func(ctx context.Context) error { return tracingShutdown(ctx) }
	// 出海代理网关：进程内唯一的出站路由表，必须在任何服务构造之前装配成全局默认，
	// 这样服务里用 egress.NewClient 建的客户端第一次请求就已经按规则走了。
	egressGateway, ownsEgress, err := ensureEgressGateway(cfg.Egress, log)
	if err != nil {
		return nil, fmt.Errorf("出海网关初始化失败: %w", err)
	}
	log.Info("出海网关已装配",
		zap.Bool("enabled", cfg.Egress.Enabled),
		zap.Int("endpoints", len(cfg.Egress.Endpoints)),
		zap.Int("rules", len(cfg.Egress.Rules)),
		zap.Bool("owner", ownsEgress),
	)
	pgHandle, err := db.NewPostgresWithLifecycle(ctx, cfg.Postgres, cfg.Database, log)
	if err != nil {
		return nil, err
	}
	// 其余代码继续按 *pgxpool.Pool 使用；生命周期治理由 pgHandle 承担
	postgres := pgHandle.Pool
	// 自动执行数据库迁移
	if err := autoMigrate(ctx, postgres, log); err != nil {
		log.Warn("自动迁移失败", zap.Error(err))
	}
	redisClient := db.NewRedis(ctx, cfg.Redis)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	natsConn, js, err := db.NewNATS(ctx, cfg.NATS)
	if err != nil {
		return nil, err
	}
	// Temporal 为可选依赖：不可达时仅告警并降级（WorkflowService 已做 nil-guard，
	// 管理后台访问 Workflow 功能会返回 503，但 API / 用户认证 / 应用管理等核心链路继续运行）。
	temporalClient, err := db.NewTemporal(cfg.Temporal, log)
	if err != nil {
		log.Warn("temporal 不可达，Workflow 功能降级；启动继续", zap.String("hostPort", cfg.Temporal.HostPort), zap.Error(err))
		temporalClient = nil
		err = nil
	}
	// 统一 close：nil 安全（temporalClient.Close 直调以避免递归）
	closeTemporal := func() {
		if temporalClient != nil {
			temporalClient.Close()
		}
	}

	pg := pgrepo.New(postgres)
	adminUserSearch, err := service.NewAdminUserSearchService(log, pg, cfg.AdminUserSearch)
	if err != nil {
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	schedules := redisrepo.NewAutoSignRepository(redisClient, cfg.Redis.KeyPrefix)
	realtimeRepo := redisrepo.NewRealtimeRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	publisher.SetConn(natsConn)
	accountBanService, err := service.NewAccountBanService(cfg.AccountBan, log, pg, sessions)
	if err != nil {
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	appService := service.NewAppService(log, pg, sessions)
	securityService := service.NewSecurityService(cfg, log, pg, sessions, appService)
	authService := service.NewAuthService(cfg, log, pg, sessions, publisher, appService, securityService)
	// 应用级第三方登录渠道：注入后 OAuth 链路按「应用级配置 → 平台级 .env」解析
	appOAuthService := service.NewAppOAuthService(log, pg, cfg)
	authService.SetAppOAuthService(appOAuthService)
	loginGuardRepo := redisrepo.NewLoginGuardRepository(redisClient, cfg.Redis.KeyPrefix)
	authService.SetLoginGuard(service.NewLoginGuardService(cfg.LoginGuard, log, loginGuardRepo, pg))
	// 授权引擎：策略落库 + 内置角色重刷 + 跨实例广播。
	// 它必须先于 AdminService 起来 —— 后者的每一次判定都要问它。
	// 内置策略 = 平台角色 + 组织内置角色，两者共用一张策略表、一个模型。
	builtinPolicies := append(authz.BuiltinPolicies(), service.OrgBuiltinPolicies()...)
	authzEngine, err := authz.New(ctx, log, pg, builtinPolicies)
	if err != nil {
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	// 广播接上之后，任一实例改了角色/策略，其余实例立刻重载。
	// 失败只降级（各实例仍按自己那份，重启后收敛），不该让进程起不来。
	if watcher := authz.NewNATSWatcher(natsConn, uuid.NewString(), log); watcher != nil {
		if err := authzEngine.SetWatcher(watcher); err != nil {
			log.Warn("授权策略广播接入失败，多实例下策略变更将不会即时同步", zap.Error(err))
		}
	}
	adminService, err := service.NewAdminService(cfg, log, pg, sessions, authzEngine)
	if err != nil {
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	adminService.SetSecurityService(securityService)
	userService := service.NewUserService(log, pg, sessions, publisher, securityService)
	userService.SetAdminUserSearchService(adminUserSearch)
	userService.SetAccountBanService(accountBanService)
	// 管理员重置密码需按应用密码策略写过期时间与历史留存
	userService.SetAppService(appService)
	authService.SetAdminUserSearchService(adminUserSearch)
	authService.SetPublicIDs(userService.PublicIDs())
	adminUserSearch.StartWarmup(ctx)
	signInService := service.NewSignInService(log, pg, sessions, publisher)
	autoSignService := service.NewAutoSignService(cfg.AutoSign, log, pg, schedules, signInService)
	// 开启自动签到前按应用的开放策略校验资格
	userService.SetAutoSignGuard(autoSignService)
	pointsService := service.NewPointsService(log, pg, sessions)
	pointsService.SetPublicIDs(userService.PublicIDs())
	realtimeService, err := service.NewRealtimeService(log, authService, realtimeRepo, natsConn, cfg.CORS.AllowOrigins)
	if err != nil {
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	realtimeService.SetAdminService(adminService)
	// 管理端在线用户表要把 userId 翻成账号名，presence 本身不碰数据库。
	realtimeService.SetIdentityRepository(pg)
	appService.SetAppEventPublisher(realtimeService)
	notificationService := service.NewNotificationService(log, pg, sessions, realtimeService)
	// 安全类站内信（两步验证、Passkey、密码已重置）也走统一投递，才会实时推送
	securityService.SetNotificationService(notificationService)
	authService.SetNotificationService(notificationService)
	// 自动签到的成功、失败与暂停走站内信
	autoSignService.SetNotifier(notificationService)
	siteService := service.NewSiteService(pg)
	versionService := service.NewVersionService(pg)
	releaseService := service.NewReleaseService(log, pg, redisClient, nil)
	releaseService.SetAppEventPublisher(realtimeService)
	// 版本管控：加密上报 + 签名裁决 + 网关闸门（docs/client-control.md）
	clientControlService := service.NewClientControlService(log, pg, redisClient, cfg)
	clientControlService.SetAppEventPublisher(realtimeService)
	roleApplicationService := service.NewRoleApplicationService(pg)
	emailService := service.NewEmailService(log, pg, redisClient, cfg.Redis.KeyPrefix, cfg.Security.MasterKey)
	// 邮件模板服务（MJML 运行在 wasm 里）首次编译要实例化模块，启动后在后台预热，第一封信不必多等
	go mailtpl.Default().Warmup(context.Background())
	paymentService := service.NewPaymentService(log, pg, cfg.PaymentReceipt)
	walletService := service.NewWalletService(log, pg)
	vipService := service.NewVipService(log, pg)
	workflowService := service.NewWorkflowService(log, pg, temporalClient, cfg.Temporal)
	storageService := service.NewStorageService(log, pg, redisClient, cfg.Redis.KeyPrefix)
	storageService.SetLinkSigningKey(cfg.Security.MasterKey)
	avatarService := service.NewAvatarService(log, storageService, userService, adminService,
		pg, redisClient, cfg.Redis.KeyPrefix, service.AvatarSettings{
			DefaultStyle:      cfg.Avatar.DefaultStyle,
			GravatarBaseURL:   cfg.Avatar.GravatarBaseURL,
			GravatarHashAlgo:  cfg.Avatar.GravatarHashAlgo,
			Sizes:             cfg.Avatar.Sizes,
			JPEGQuality:       cfg.Avatar.JPEGQuality,
			KeepAnimated:      cfg.Avatar.KeepAnimated,
			MaxUploadBytes:    cfg.Avatar.MaxUploadBytes,
			UploadsPerHour:    cfg.Avatar.UploadsPerHour,
			StorageConfigName: cfg.Avatar.StorageConfigName,
			CacheTTL:          cfg.Avatar.CacheTTL,
			PublicBaseURL:     cfg.Avatar.PublicBaseURL,
			SigningKey:        cfg.Avatar.SigningKey,
		})
	captchaRepo := redisrepo.NewCaptchaRepository(redisClient, cfg.Redis.KeyPrefix)
	captchaService := service.NewCaptchaService(cfg, log, captchaRepo)
	userService.SetVerificationServices(emailService, captchaService)
	authService.SetPasswordRecovery(emailService, captchaService, userService)
	// 注册短信服务商（启动时注册，运行时可动态扩展）
	captchaService.RegisterSMSProvider("aliyun", service.NewAliyunSMSProvider())
	captchaService.RegisterSMSProvider("tencent", service.NewTencentSMSProvider())
	monitorService := service.NewMonitorService(cfg, log, postgres, redisClient, natsConn, temporalClient, authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, avatarService, realtimeService, nil, nil, nil)
	ipBanRepo := redisrepo.NewIPBanRepository(redisClient, cfg.Redis.KeyPrefix)
	locationService := service.NewLocationService(log, redisClient, cfg.Redis.KeyPrefix, cfg.GeoIP)
	userService.SetLocationService(locationService)
	// 登录一致性校验（设备绑定 / 登录 IP / 登录属地）——
	// 依赖 LocationService 解析属地，因此在其构造之后注入
	loginConsistencyService := service.NewLoginConsistencyService(
		log, redisrepo.NewLoginBaselineRepository(redisClient, cfg.Redis.KeyPrefix), locationService)
	authService.SetLoginConsistency(loginConsistencyService)
	ipBanService := service.NewIPBanService(log, pg, ipBanRepo, locationService)
	ipBanService.SetDefaultModeReader(func() string {
		return manager.Current().Firewall.DefaultBanMode
	})
	if rules, err := service.ParseAutoBanRules(cfg.Firewall.AutoBanRules); err != nil {
		log.Warn("FIREWALL_AUTO_BAN_RULES 解析失败，沿用内置默认规则", zap.Error(err))
	} else if len(rules) > 0 {
		ipBanService.SetAutoBanRules(rules)
	}
	geoBanService := service.NewGeoBanService(log, pg, locationService)
	if err := geoBanService.Initialize(ctx); err != nil {
		log.Warn("geo_bans 初始化失败", zap.Error(err))
	}
	ipBanService.SetGeoBanService(geoBanService)
	geoFenceService := service.NewGeoFenceService(log, pg, locationService)
	if err := geoFenceService.Initialize(ctx); err != nil {
		log.Warn("geo_fences 初始化失败（围栏判定将为空集）", zap.Error(err))
	}
	ipBanService.SetGeoFenceService(geoFenceService)
	geoAnalyticsService := service.NewGeoAnalyticsService(log, cfg.GeoRisk, pg)
	firewall, err := middleware.NewFirewall(cfg.Firewall, log, redisClient, cfg.Redis.KeyPrefix, publisher, ipBanService)
	if err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	// 突发流量爬坡：单实例自适应准入，挂在 Firewall 之后整形合法洪峰。
	// 持久化配置由 systemService.Initialize 热加载覆盖。
	trafficRamp := middleware.NewTrafficRamp(cfg.TrafficRamp, log)
	ldapService := service.NewLDAPService(log, cfg.JWT.Secret)
	adminService.SetLDAPService(ldapService)
	oidcService := service.NewOIDCService(log, cfg.JWT.Secret)
	adminService.SetOIDCService(oidcService)
	samlService := service.NewSAMLService(log, cfg.JWT.Secret)
	adminService.SetSAMLService(samlService)
	// 第三方认证源健康探测服务（单例，30s TTL 缓存 + 单飞）
	// 供 /api/admin/system/admins 超管视图标注"账号可能无法登录"，N 账号共享一次探测
	authProviderHealthService := service.NewAuthProviderHealthService(log, ldapService, oidcService, samlService)
	systemService := service.NewPlatformSettingsService(cfg, log, pg, firewall, securityService, ldapService, oidcService, samlService)
	systemService.SetTrafficRamp(trafficRamp)
	// 自助注册开关与自助建应用配额存在平台设置里，权限层要读它们
	adminService.SetPlatformSettings(systemService)
	// 出海网关管理面：数据库里的配置覆盖 .env 基线
	egressService := service.NewEgressService(log, pg, egressGateway, cfg.Security.MasterKey, cfg.Egress)
	egressService.AttachMihomo(ensureMihomoCore(cfg.EgressMihomo, "api", log), ownsEgress, cfg.EgressMihomo.Mirror)
	if err := adminService.LoadCustomRoles(ctx); err != nil {
		log.Warn("加载自定义角色失败", zap.Error(err))
	}
	firewallLogService := service.NewFirewallLogService(log, pg, locationService, ipBanService)
	monitorService = service.NewMonitorService(cfg, log, postgres, redisClient, natsConn, temporalClient, authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, avatarService, realtimeService, firewall, systemService, locationService)
	monitorRepo := redisrepo.NewMonitorRepository(redisClient, cfg.Redis.KeyPrefix)
	monitorService.SetMonitorRepo(monitorRepo)
	monitorService.SetCrashLog(cl)
	if err := adminService.EnsureBootstrapSuperAdmin(ctx); err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	if err := egressService.Initialize(ctx); err != nil {
		log.Warn("加载持久化的出海网关配置失败，沿用 .env 配置", zap.Error(err))
	}
	if err := systemService.Initialize(ctx); err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	// 平台设置 Initialize 完成后，LDAP/OIDC/SAML 的真实配置才通过 Reload 写入对应服务；
	// 构造时 NewAuthProviderHealthService 的初始探测只看到空配置，此处立即重新探测一次，
	// 确保首次 Snapshot() 命中即是真实健康数据（而不是等 30s 的下一轮 tick）。
	authProviderHealthService.Refresh(ctx)
	if err := ipBanService.SyncBansToRedis(ctx); err != nil {
		log.Warn("启动时同步 IP 封禁到 Redis 失败", zap.Error(err))
	}
	if err := storageService.EnsureDefaultLocalConfig(ctx, fmt.Sprintf("http://localhost:%d", cfg.HTTPPort)); err != nil {
		log.Error("默认本地存储配置初始化失败", zap.Error(err))
	}
	replayRepo := redisrepo.NewReplayRepository(redisClient, cfg.Redis.KeyPrefix)
	replayGuard := middleware.NewReplayGuard(cfg.ReplayProtection, cfg.JWT.Secret, replayRepo, log)
	authProtocolService := service.NewAuthProtocolService(log, pg, replayRepo, cfg.Security.MasterKey)
	memoryManager := service.NewMemoryManager(cfg.Memory, log, redisClient, cfg.Redis.KeyPrefix)
	// 数据库生命周期与泄漏监控：与 MemoryManager 同构（构造即可用，Start 拉起后台采集）
	databaseManager := service.NewDatabaseManager(log, cfg.Database, cfg.Postgres, pgHandle, redisClient, cfg.Redis.KeyPrefix, service.DatabaseRolePrimary)
	announcementService := service.NewAnnouncementService(log, pg, publisher)
	legalService := service.NewLegalService(log, pg, systemService, cfg.AppName, cfg.LegalContactEmail, cfg.LegalAuthoritativeLocale)
	// 组织域权限：Casbin 带 domain 的 enforcer，组织自定义角色策略在启动时装载
	orgAccessControl, err := service.NewOrgAccessControl(log, pg, adminService, authzEngine)
	if err != nil {
		return nil, fmt.Errorf("初始化组织权限判定失败: %w", err)
	}
	if err := orgAccessControl.Reload(ctx); err != nil {
		log.Warn("装载组织角色策略失败，自定义角色暂不生效", zap.Error(err))
	}
	orgService := service.NewOrganizationService(log, pg, orgAccessControl)
	orgService.SetRealtimeService(realtimeService)
	approvalService := service.NewOrgApprovalService(log, pg, orgAccessControl, orgService)
	approvalService.SetRealtimeService(realtimeService)
	orgService.SetApprovalService(approvalService)
	templateService := service.NewTemplateService(log, pg)
	auditService := service.NewAuditService(log, pg)
	// 目录改版后把历史审计的模块与类型对齐到新目录（只处理版本落后的行）
	go auditService.BackfillCatalog(context.Background())
	dashboardService := service.NewDashboardService(log, pg)
	pluginService := service.NewPluginService(log, pg)
	cardKeyService := service.NewCardKeyService(log, pg)
	// 卡密即登录凭证：注入后 loginMethods 里的 cardkey 一档才真正可用，
	// 未注入时该方式直接报「未启用」而不是空指针。
	authService.SetCardKeyService(cardKeyService)
	// 输错卡密的计数与锁定和登录防爆破共用同一套 Redis 键空间（kind 前缀 ck-）
	cardKeyService.SetGuard(loginGuardRepo)
	cardKeyService.SetNotifier(notificationService)
	rewardedAdService := service.NewRewardedAdService(log, pg, cfg)
	rewardedAdService.SetPublicIDs(userService.PublicIDs())
	// 抽奖：视频抽奖读激励广告场景的剩余次数，看完发的抽奖次数进应用级余额
	lotteryService := service.NewLotteryService(log, pg, rewardedAdService)
	lotteryService.SetNotifier(notificationService)
	lotteryService.SetEventPublisher(realtimeService)
	// 广告服务策略：开屏广告与激励视频传给 SDK 的是同一个带签名的用户标识。
	adPolicyService := service.NewAdPolicyService(log, pg, rewardedAdService)
	adPolicyService.SetEventPublishers(realtimeService, realtimeService)
	// 崩溃报告：用户在崩溃屏上逐份同意后免登录上报（docs/crash-reports.md）
	crashReportService := service.NewCrashReportService(log, pg, locationService)
	// 功能时长：看激励广告领到的使用时长，先租后用、停用时结算（发放在激励广告的结算事务里）。
	featureTimeService := service.NewFeatureTimeService(log, pg)
	// 积分商城：人工发放的处理结果走站内信，兑换后通知用户的其它在线设备刷新积分与权益。
	pointsStoreService := service.NewPointsStoreService(log, pg)
	pointsStoreService.SetNotifier(notificationService)
	pointsStoreService.SetEventPublisher(realtimeService)
	// 邀请：邀请人奖励到账走站内信，双方的在线设备收到 invite.changed 刷新积分与权益；
	// 发放条件是「被邀请人签到满 N 天」时由签到回调结算。
	inviteService := service.NewInviteService(log, pg)
	inviteService.SetNotifier(notificationService)
	inviteService.SetEventPublisher(realtimeService)
	inviteService.SetAppNames(appService)
	signInService.SetAfterSignIn(inviteService.OnSignIn)
	// 积分排行榜：实时榜走 Redis 缓存；周期榜到期由 API 进程的后台循环结算、发奖，奖励到账走站内信。
	leaderboardService := service.NewLeaderboardService(log, pg, sessions)
	leaderboardService.SetPublicIDs(userService.PublicIDs())
	leaderboardService.SetNotifier(notificationService)
	// 物品发放：画布规则手动 / 定时 / 事件触发（docs/item-grant.md）；定时与接管中断的执行由 API 进程的后台循环负责。
	// 事件来自插件钩子（注册、支付成功）、登录收口（finalizeLogin）与签到回调。
	itemGrantService := service.NewItemGrantService(log, pg)
	itemGrantService.SetNotifier(notificationService)
	itemGrantService.SetEventPublisher(realtimeService)
	levelService := service.NewLevelService(log, pg)
	levelService.SetNotifier(notificationService)
	levelService.SetEventPublisher(realtimeService)
	signInService.SetSignInObserver(itemGrantService.OnSignIn)
	authService.SetLoginObserver(itemGrantService.OnLogin)
	// 用户云存储：内容走 storageService 解析出的存储配置，治理闸门（blockStorage）
	// 也随之生效 —— 写入经 UploadForApp，那里是所有上传的收口。
	cloudStorageService := service.NewCloudStorageService(log, pg, storageService)
	// 账号注销：验证走邮件与短信，执行时吊销登录、清空云存储、删用户；登录与注册经 AuthService 回调
	accountDeletionService := service.NewAccountDeletionService(log, pg, sessions, cfg.Security.MasterKey)
	accountDeletionService.SetAccountServices(authService, userService, securityService)
	accountDeletionService.SetVerificationChannels(emailService, captchaService, appService)
	accountDeletionService.SetCloudStorage(cloudStorageService)
	accountDeletionService.SetNotifier(notificationService)
	accountDeletionService.SetEventPublisher(realtimeService)
	authService.SetAccountDeletion(accountDeletionService)
	appFunctionService := service.NewAppFunctionService(log, pg, cfg.JWT.Secret)
	// AI 供应商通道（系统级 + 应用级，密钥加密落库）与 Agent 编排。
	aiProviderService := service.NewAIProviderService(log, pg, cfg.Security.MasterKey)
	aiAgentService := service.NewAIAgentService(log, pg, aiProviderService, appFunctionService)
	// 面向应用终端用户的计费中转：通道复用上面的链路，计费落积分 / 余额 / 额度（docs/ai-gateway.md）。
	aiGatewayService := service.NewAIGatewayService(log, pg, authService, aiProviderService)
	// 上游空闲看门狗（AI_UPSTREAM_IDLE_TIMEOUT）：上游卡死时主动断开，而不是挂到代理切断客户端。
	aiProviderService.SetUpstreamIdleTimeout(cfg.AIRelay.UpstreamIdleTimeout)
	// 调用记录的实时事件经 NATS 跨实例分发（控制台调用记录页的实时刷新）。
	aiGatewayService.AttachLiveBus(natsConn)
	// script 运行时的 SDK 依赖：脚本通过它们读写平台数据。
	// 未装配的那一项对应的能力会在绑定时被拒绝并点名，
	// 而不是绑上去等脚本调用时空指针（wasm/http 不受影响）。
	appFunctionService.SetScriptDeps(service.ScriptSDKDeps{
		Points:        pointsService,
		Vip:           vipService,
		Notifications: notificationService,
		Audit:         auditService,
		Wallet:        walletService,
		Email:         emailService,
		Bans:          accountBanService,
		Realtime:      realtimeService,
		Location:      locationService,
		Redis:         redisClient,
		KeyPrefix:     cfg.Redis.KeyPrefix,
		AI:            aiProviderService,
	})
	if err := pluginService.Initialize(ctx); err != nil {
		log.Warn("插件系统初始化失败（非致命）", zap.Error(err))
	}
	adminService.SetPluginService(pluginService)
	pluginService.ObserveHooks(itemGrantService.ObserveHook)
	authService.SetPluginService(pluginService)
	userService.SetPluginService(pluginService)
	accountBanService.SetPluginService(pluginService)
	appService.SetPluginService(pluginService)
	paymentService.SetPluginService(pluginService)
	notificationService.SetPluginService(pluginService)
	storageService.SetPluginService(pluginService)
	systemService.SetPluginService(pluginService)
	// Banner 图片落到该应用自己的对象存储，落库的是 storage:// 引用
	appService.SetStorageService(storageService)
	releaseService.SetStorageService(storageService)
	// 凭证：抬头的品牌名来自平台设置，寄送走邮件出口，是否自动寄送读应用级交易设置
	paymentService.SetPlatformSettingsService(systemService)
	paymentService.SetEmailService(emailService)
	paymentService.SetAppService(appService)
	paymentService.SetConsoleBaseURL(cfg.ConsoleBaseURL) // 同步跳转默认指向控制台的支付结果页
	// 钱包流水与会员直购也要能出凭证：凭证引擎只有 PaymentService 持有，
	// 这两个服务通过它把自己的资金记录接进同一套排版与寄送链路
	walletService.SetPaymentService(paymentService)
	vipService.SetPaymentService(paymentService)
	sessionMgmtService := service.NewSessionMgmtService(log, pg, sessions, realtimeService)
	storageResourceService := service.NewStorageResourceService(log, pg)
	storageResourceService.SetObjectRemover(storageService)
	userMasterService := service.NewUserMasterService(log, pg)
	reportService := service.NewReportService(log, pg)
	riskService := service.NewRiskService(cfg.Risk, log, pg, redisClient, cfg.Redis.KeyPrefix)
	// 归属地走 LocationService：未采购外部 IP 情报源时，geo_* 系列变量若恒为空，
	// 「归属地异常」这类规则配了也永不命中 —— 那是最典型的"看起来防住了"。
	riskService.SetLocationService(locationService)
	authService.SetRiskService(riskService)
	inviteService.SetRiskService(riskService)
	lotteryService.SetRiskService(riskService)
	adminService.SetRiskService(riskService)
	// 新接入的业务场景：下单、签到、卡密兑换、积分兑换、广告领奖
	paymentService.SetRiskService(riskService)
	signInService.SetRiskService(riskService)
	cardKeyService.SetRiskService(riskService)
	pointsStoreService.SetRiskService(riskService)
	rewardedAdService.SetRiskService(riskService)
	// 处置执行器：ban 与复核驳回真的落到账号封禁与防火墙上
	riskService.SetAccountBanService(accountBanService)
	riskService.SetFirewall(ipBanService)
	riskService.SetCaptchaProbe(func(ctx context.Context, appID int64) bool {
		cfg, err := appService.GetCaptchaConfig(ctx, appID)
		return err == nil && service.IsCaptchaRequired(cfg)
	})
	// 对外 OAuth2 / OIDC 授权服务器（Ory Hydra 承载协议，Aegis 负责登录与同意页）
	oauth2ServerService := service.NewOAuth2ServerService(cfg, log, pg, authService, appService, avatarService)
	accountBanService.SetOAuth2Server(oauth2ServerService)
	userService.SetOAuth2Server(oauth2ServerService)

	// 一起听（独立微服务 services/listentogether）：主服务只签票据、发布公钥、转发管理操作。
	// 这里的配置错误只让一起听不可用，不拖垮主服务启动（docs/listen-together.md）。
	listenTogetherService, err := service.NewListenTogetherService(log, cfg.ListenTogether, vipService, userService, appService, natsConn)
	if err != nil {
		log.Error("一起听配置无效，一起听不可用", zap.Error(err))
		listenTogetherService = nil
	}
	accountBanService.SetListenTogether(listenTogetherService)

	// 设备营销名称字典：启动时幂等种子（表空则解析嵌入的 kt 源并批量写入）
	deviceMarketingService := service.NewDeviceMarketingService(log, pg)
	// 设备字典映射到用户接口：会话、登录记录、扫码登录发起端的 deviceInfo 读取时按字典现查
	userService.SetDeviceDirectory(deviceMarketingService)
	appService.SetDeviceDirectory(deviceMarketingService)
	authService.SetDeviceDirectory(deviceMarketingService)
	reportService.SetDeviceDirectory(deviceMarketingService)

	// 登录设备数控制：新登录的签发收口过关卡，超限自动下线 / 拒绝 / 让用户选择（docs/device-limit.md）
	deviceLimitService := service.NewDeviceLimitService(log, pg, sessions, appService, cfg.JWT.RefreshTTL)
	deviceLimitService.SetVipService(vipService)
	deviceLimitService.SetDeviceDirectory(deviceMarketingService)
	deviceLimitService.SetNotificationService(notificationService)
	deviceLimitService.SetRealtime(realtimeService)
	authService.SetDeviceLimitService(deviceLimitService)

	// 平台级 Banner（超级管理员专属 CRUD；Overview 对所有管理员展示；上传经 StorageService）
	platformBannerService := service.NewPlatformBannerService(log, pg, sessions, storageService)

	// 管理员站内收件箱：与用户站内信分表（admin_accounts vs users 主键空间不同），
	// 写入成功后经 RealtimeService 推 admin.notification.created 驱动控制台角标
	adminInboxService := service.NewAdminInboxService(log, pg, realtimeService)
	orgService.SetAdminInbox(adminInboxService)
	approvalService.SetAdminInbox(adminInboxService)

	// ── 平台治理 ──
	// 超级管理员 / 平台管理员对全站应用的强制管控（冻结 / 限制 / 封禁 / 归档）。
	// 判定走内存快照，因此必须在任何执行点被调用之前 Initialize；
	// 加载失败时快照为空 = 全部放行（fail-open），与防火墙的取向一致 ——
	// 治理表读不出来时把整个平台锁死，代价远大于漏放一会儿。
	governanceService := service.NewPlatformGovernanceService(log, pg, sessions)
	governanceService.SetAdminInbox(adminInboxService)
	governanceService.SetPluginService(pluginService)
	if err := governanceService.Initialize(ctx); err != nil {
		log.Warn("平台治理状态加载失败，本轮判定按无治理放行", zap.Error(err))
	}
	// 执行点注入：每一处都对应 platformdomain 里的一项限制，
	// 少接一处就等于那一项开关只落库不生效。
	appService.SetGovernanceService(governanceService)             // blockLogin / blockRegister
	authService.SetGovernanceService(governanceService)            // blockApi
	paymentService.SetGovernanceService(governanceService)         // blockPayment
	storageService.SetGovernanceService(governanceService)         // blockStorage
	notificationService.SetGovernanceService(governanceService)    // blockNotification（站内信）
	emailService.SetGovernanceService(governanceService)           // blockNotification（邮件）
	aiProviderService.SetGovernanceService(governanceService)      // blockApi（被限的应用不再消耗 AI 通道）
	// 平台级邮件的落款主体取品牌名（应用级取应用名）。没有它的话，
	// 一封平台通知的标题会渲染成「 账号已开通」——前面缺一个词。
	emailService.SetPlatformSettings(systemService)

	// 统一通知出口：所有业务事件（工单、SLA 告警…）只发到这里，
	// 由订阅表决定投给飞书 / 钉钉 / 企微 / Slack / Webhook / 邮件 / 站内信 / 实时推送
	notifyHub := service.NewNotifyHub(log, pg, cfg, emailService, notificationService, adminInboxService, realtimeService)
	if base := strings.TrimSpace(cfg.ConsoleBaseURL); base != "" {
		notifyHub.SetConsoleBaseURL(base)
	}
	// 工单系统：权限判定依赖 AdminService（Casbin），通知统一走 NotifyHub
	ticketService := service.NewTicketService(log, pg, adminService, notifyHub, storageService)
	go func() {
		seedCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := deviceMarketingService.SeedIfEmpty(seedCtx); err != nil {
			log.Warn("seed device marketing names failed", zap.Error(err))
		}
	}()
	_, err = js.QueueSubscribe(event.SubjectUserAutoSignSync, workerQueueAutoSignSync, func(msg *nats.Msg) {
		payload := map[string]any{}
		_ = json.Unmarshal(msg.Data, &payload)
		userID := int64FromPayload(payload["user_id"])
		appID := int64FromPayload(payload["appid"])
		if userID > 0 && appID > 0 {
			if syncErr := autoSignService.SyncUserSchedule(context.Background(), userID, appID); syncErr != nil {
				log.Warn("api auto sign sync failed", zap.Int64("user_id", userID), zap.Int64("appid", appID), zap.Error(syncErr))
			}
		}
		_ = msg.Ack()
	}, nats.ManualAck())
	if err != nil {
		_ = accountBanService.Close(context.Background())
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	// 客户端 IP 判定器要在路由之前建好：配置写错时应当**启动即失败**，
	// 而不是先跑起来、再让每一条限流与封禁按错误的地址执行。
	clientIPResolver, err := clientip.New(cfg.ClientIP)
	if err != nil {
		_ = accountBanService.Close(context.Background())
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, fmt.Errorf("客户端 IP 判定配置无效: %w", err)
	}
	logClientIPResolution(log, clientIPResolver)

	// 在线记录带上 IP 归属地（建连时只读本地库与缓存）
	realtimeService.SetLocationService(locationService)
	// 管理端实时在线统计：加密长连接推送快照、曲线与上下线动态
	onlineLiveHub := service.NewOnlineLiveHub(log, realtimeService, adminService, appService, pg, cfg.CORS.AllowOrigins)
	router, err := httptransport.NewRouter(httptransport.RouterDeps{
		Auth:               authService,
		Admin:              adminService,
		User:               userService,
		UserMaster:         userMasterService,
		Security:           securityService,
		Captcha:            captchaService,
		Sessions:           sessions,
		SessionMgmt:        sessionMgmtService,
		LDAP:               ldapService,
		OIDC:               oidcService,
		SAML:               samlService,
		AuthProtocol:       authProtocolService,
		AuthProviderHealth: authProviderHealthService,
		AppOAuth:           appOAuthService,
		OAuth2Server:       oauth2ServerService,

		App:              appService,
		AppFunction:      appFunctionService,
		CardKey:          cardKeyService,
		RewardedAd:       rewardedAdService,
		FeatureTime:      featureTimeService,
		AdPolicy:         adPolicyService,
		CrashReport:      crashReportService,
		PointsStore:      pointsStoreService,
		Invite:           inviteService,
		Leaderboard:      leaderboardService,
		AccountDeletion:  accountDeletionService,
		CloudStorage:     cloudStorageService,
		Site:             siteService,
		Version:          versionService,
		Release:          releaseService,
		ClientControl:    clientControlService,
		ListenTogether:   listenTogetherService,
		DeviceLimit:      deviceLimitService,
		PlatformSettings: systemService,
		PlatformBanner:   platformBannerService,
		Governance:       governanceService,
		Legal:            legalService,
		Plugin:           pluginService,
		Template:         templateService,
		Egress:           egressService,
		AIProvider:       aiProviderService,
		AIGateway:        aiGatewayService,
		AIRelay:          cfg.AIRelay,
		AIAgent:          aiAgentService,

		Organization:    orgService,
		OrgApproval:     approvalService,
		RoleApplication: roleApplicationService,

		SignIn:       signInService,
		AutoSign:     autoSignService,
		Points:       pointsService,
		Lottery:      lotteryService,
		ItemGrant:    itemGrantService,
		Level:        levelService,
		Wallet:       walletService,
		Vip:          vipService,
		Payment:      paymentService,
		Workflow:     workflowService,
		Ticket:       ticketService,
		Announcement: announcementService,

		Notification: notificationService,
		Notify:       notifyHub,
		AdminInbox:   adminInboxService,
		Email:        emailService,
		Realtime:     realtimeService,
		OnlineLive:   onlineLiveHub,

		Storage:         storageService,
		StorageResource: storageResourceService,
		Avatar:          avatarService,

		Risk:            riskService,
		FirewallLog:     firewallLogService,
		IPBan:           ipBanService,
		GeoBan:          geoBanService,
		GeoFence:        geoFenceService,
		GeoAnalytics:    geoAnalyticsService,
		Location:        locationService,
		Audit:           auditService,
		Monitor:         monitorService,
		Dashboard:       dashboardService,
		Report:          reportService,
		DeviceMarketing: deviceMarketingService,
		MemoryManager:   memoryManager,
		DatabaseManager: databaseManager,

		Firewall:       firewall,
		TrafficRamp:    trafficRamp,
		ReplayGuard:    replayGuard,
		CrashLog:       cl,
		Logger:         log,
		CORS:           cfg.CORS,
		ClientIP:       clientIPResolver,
		DocsPortalURL:  cfg.DocsPortalURL,
		ConsoleBaseURL: cfg.ConsoleBaseURL,

		// 闸门与上报共用同一个服务：同一份缓存、同一套判定
		ClientControlGate: clientControlService,
	})
	if err != nil {
		_ = accountBanService.Close(context.Background())
		locationService.Close()
		realtimeService.Close(context.Background())
		closeTemporal()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		// 缺省 0（不限）：SSE 对话流与流式转发的生存前提，见 config.NormalizeConfig 的说明。
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	app := &APIApp{
		Config:             cfg,
		ConfigManager:      manager,
		Logger:             log,
		CrashLog:           cl,
		Router:             router,
		Server:             server,
		Postgres:           postgres,
		Redis:              redisClient,
		NATSConn:           natsConn,
		JetStream:          js,
		Temporal:           temporalClient,
		Realtime:           realtimeService,
		OnlineLive:         onlineLiveHub,
		Payment:            paymentService,
		AutoSign:           autoSignService,
		SessionMgmt:        sessionMgmtService,
		AdminUserSearch:    adminUserSearch,
		AccountBan:         accountBanService,
		Location:           locationService,
		Monitor:            monitorService,
		Memory:             memoryManager,
		Firewall:           firewall,
		Security:           securityService,
		Risk:               riskService,
		Governance:         governanceService,
		CloudStorage:       cloudStorageService,
		Storage:            storageService,
		AccountDeletion:    accountDeletionService,
		Leaderboard:        leaderboardService,
		ItemGrant:          itemGrantService,
		Level:              levelService,
		AIGateway:          aiGatewayService,
		AuthProviderHealth: authProviderHealthService,
		Egress:             egressService,
		EgressGateway:      egressGateway,
		OwnsEgress:         ownsEgress,
		Database:           databaseManager,
		PostgresHandle:     pgHandle,
		ClientIP:           clientIPResolver,
		ShutdownTracing:    shutdownTracing,
	}
	return app, nil
}

func (a *APIApp) Start(ctx context.Context) error {
	if a.AccountBan != nil {
		a.AccountBan.Start()
	}
	if a.Monitor != nil {
		a.Monitor.StartCollector(ctx, 15*time.Second)
	}
	if a.Memory != nil {
		a.Memory.Start(ctx)
	}
	if a.Database != nil {
		a.Database.Start(ctx)
	}
	if a.Governance != nil {
		a.Governance.Start(ctx)
	}
	if a.CloudStorage != nil {
		a.CloudStorage.Start(ctx)
	}
	if a.Storage != nil {
		a.Storage.Start(ctx)
	}
	if a.AccountDeletion != nil {
		a.AccountDeletion.Start(ctx)
	}
	if a.Leaderboard != nil {
		a.Leaderboard.Start(ctx)
	}
	if a.ItemGrant != nil {
		a.ItemGrant.Start(ctx)
	}
	if a.Level != nil {
		a.Level.Start(ctx)
	}
	if a.AIGateway != nil {
		a.AIGateway.Start(ctx)
	}
	if a.OwnsEgress && a.EgressGateway != nil {
		a.EgressGateway.Start(ctx)
		a.Egress.StartSync(ctx)
	}
	registerAPIConfigHotReload(a.ConfigManager, a.Logger, a.Firewall, a.Security, a.AutoSign, a.AccountBan, a.Risk, a.Egress)

	SafeGo(a.Logger, a.CrashLog, "api.admin_session_cleanup", true, func() {
		a.runAdminSessionCleanupLoop(ctx)
	})

	if a.AutoSign != nil {
		if scheduled, processed, err := a.AutoSign.CatchUpOnStartup(ctx); err != nil {
			a.Logger.Warn("api auto sign startup catch-up failed", zap.Error(err))
		} else {
			a.Logger.Info("api auto sign startup catch-up completed", zap.Int("scheduled", scheduled), zap.Int("processed", processed))
		}
		SafeGo(a.Logger, a.CrashLog, "api.auto_sign", true, func() {
			a.runAutoSignLoop(ctx)
		})
	}
	return nil
}

func (a *APIApp) Close(ctx context.Context) {
	a.closeOnce.Do(func() { a.close(ctx) })
}

// close 先关监听：不再接新请求，在途请求在 ctx（ShutdownTimeout）内跑完；
// 之后才停后台协程，最后断开 Redis / NATS / Temporal / Postgres。
// 反过来的顺序会让还在处理的请求撞上已经关掉的连接池。
func (a *APIApp) close(ctx context.Context) {
	// 流式 AI 对话不会在 Shutdown 的期限内自己结束：先约好宽限后主动取消，让它们带着
	// 可重试的错误事件收尾、放回暂扣，而不是被进程退出硬切、暂扣挂到过期（ai_gateway_inflight.go）。
	if a.AIGateway != nil {
		a.AIGateway.CancelInFlightAfter(aiRelayShutdownGrace(ctx))
	}
	if a.Secure != nil {
		_ = a.Secure.Shutdown(ctx)
	}
	if a.Server != nil {
		_ = a.Server.Shutdown(ctx)
	}
	if a.Governance != nil {
		a.Governance.Stop()
	}
	if a.CloudStorage != nil {
		a.CloudStorage.Stop()
	}
	if a.Storage != nil {
		a.Storage.Stop()
	}
	if a.AccountDeletion != nil {
		a.AccountDeletion.Stop()
	}
	if a.Leaderboard != nil {
		a.Leaderboard.Stop()
	}
	if a.ItemGrant != nil {
		a.ItemGrant.Stop()
	}
	if a.Level != nil {
		a.Level.Stop()
	}
	if a.AIGateway != nil {
		a.AIGateway.Stop()
	}
	if a.Database != nil {
		a.Database.Stop()
	}
	if a.Memory != nil {
		a.Memory.Stop()
	}
	if a.Monitor != nil {
		a.Monitor.StopCollector()
	}
	if a.OnlineLive != nil {
		a.OnlineLive.Close()
	}
	if a.Realtime != nil {
		a.Realtime.Close(ctx)
	}
	if a.Payment != nil {
		a.Payment.Close(ctx)
	}
	if a.AdminUserSearch != nil {
		_ = a.AdminUserSearch.Close()
	}
	if a.AccountBan != nil {
		_ = a.AccountBan.Close(ctx)
	}
	if a.Location != nil {
		a.Location.Close()
	}
	// 停止第三方认证源后台探测循环
	if a.AuthProviderHealth != nil {
		a.AuthProviderHealth.Close()
	}
	// 出海网关持有 SSH 长连接与健康探测协程，必须显式释放
	if a.OwnsEgress {
		releaseEgressGateway(a.EgressGateway)
	}
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	drainNATS(ctx, a.NATSConn)
	if a.Temporal != nil {
		a.Temporal.Close()
	}
	// 连接池最后关闭，且先排空在途查询再断开
	if a.PostgresHandle != nil {
		a.PostgresHandle.Close(ctx)
	} else if a.Postgres != nil {
		a.Postgres.Close()
	}
	if a.ShutdownTracing != nil {
		_ = a.ShutdownTracing(ctx)
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
	}
}

func (a *APIApp) runAutoSignLoop(ctx context.Context) {
	if a.AutoSign == nil {
		return
	}

	tick := time.NewTimer(a.autoSignTickInterval())
	defer tick.Stop()
	rebuild := time.NewTimer(a.autoSignRebuildInterval())
	defer rebuild.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			processed, err := a.AutoSign.RunDue(ctx)
			if err != nil {
				a.Logger.Warn("api auto sign due run failed", zap.Error(err))
			} else if processed > 0 {
				a.Logger.Info("api auto sign due processed", zap.Int("processed", processed), zap.Int64("scheduled_count", a.AutoSign.ScheduledCount(ctx)))
			}
			tick.Reset(a.autoSignTickInterval())
		case <-rebuild.C:
			scheduled, err := a.AutoSign.RebuildSchedule(ctx)
			if err != nil {
				a.Logger.Warn("api auto sign periodic rebuild failed", zap.Error(err))
			} else {
				a.Logger.Info("api auto sign periodic rebuild completed", zap.Int("scheduled", scheduled))
			}
			rebuild.Reset(a.autoSignRebuildInterval())
		}
	}
}

func (a *APIApp) autoSignTickInterval() time.Duration {
	if a.AutoSign != nil {
		if interval := a.AutoSign.CurrentConfig().TickInterval; interval > 0 {
			return interval
		}
	}
	return time.Minute
}

func (a *APIApp) autoSignRebuildInterval() time.Duration {
	if a.AutoSign != nil {
		if interval := a.AutoSign.CurrentConfig().RebuildInterval; interval > 0 {
			return interval
		}
	}
	return 15 * time.Minute
}

func (a *APIApp) runAdminSessionCleanupLoop(ctx context.Context) {
	if a.SessionMgmt == nil {
		return
	}

	runCleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		a.SessionMgmt.Cleanup(cleanupCtx)
	}

	runCleanup()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCleanup()
		}
	}
}

// initGinMode 根据环境变量 GIN_MODE 或 APP_ENV 设置 Gin 运行模式
func initGinMode(appEnv string) {
	// GIN_MODE 环境变量优先（Gin 原生支持，但需要在创建 Engine 前设置）
	if mode := gin.Mode(); mode != gin.DebugMode {
		return // 已被 GIN_MODE 环境变量设置为非 debug
	}
	switch appEnv {
	case "production", "prod":
		gin.SetMode(gin.ReleaseMode)
	case "test", "testing":
		gin.SetMode(gin.TestMode)
	}
}

// aiRelayShutdownGrace 退出时给在途 AI 对话的宽限：Shutdown 期限的一半（1–10 秒），
// 另一半留给它们写错误事件、放回暂扣。
func aiRelayShutdownGrace(ctx context.Context) time.Duration {
	grace := 5 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		grace = time.Until(deadline) / 2
	}
	return min(max(grace, time.Second), 10*time.Second)
}
