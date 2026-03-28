package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
	"aegis/internal/event"
	"aegis/internal/middleware"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	httptransport "aegis/internal/transport/http"
	"aegis/pkg/crashlog"
	pkglogger "aegis/pkg/logger"
	"aegis/pkg/tracing"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

type APIApp struct {
	Config          config.Config
	Logger          *zap.Logger
	CrashLog        *crashlog.Logger
	Router          *gin.Engine
	Server          *http.Server
	Postgres        *pgxpool.Pool
	Redis           *redislib.Client
	NATSConn        *nats.Conn
	JetStream       nats.JetStreamContext
	Temporal        client.Client
	Realtime        *service.RealtimeService
	Payment         *service.PaymentService
	AdminUserSearch *service.AdminUserSearchService
	AccountBan      *service.AccountBanService
	Location        *service.LocationService
	Monitor         *service.MonitorService
	Memory          *service.MemoryManager
	ShutdownTracing func(context.Context) error
}

func NewAPIApp(ctx context.Context, cl *crashlog.Logger) (*APIApp, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	initGinMode(cfg.AppEnv)
	log, err := pkglogger.New(cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	shutdownTracing := tracing.Init()
	postgres, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}
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
	temporalClient, err := db.NewTemporal(cfg.Temporal, log)
	if err != nil {
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}

	pg := pgrepo.New(postgres)
	adminUserSearch, err := service.NewAdminUserSearchService(log, pg, cfg.AdminUserSearch)
	if err != nil {
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	realtimeRepo := redisrepo.NewRealtimeRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	publisher.SetConn(natsConn)
	accountBanService, err := service.NewAccountBanService(cfg.AccountBan, log, pg, sessions)
	if err != nil {
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	appService := service.NewAppService(log, pg, sessions)
	securityService := service.NewSecurityService(cfg, log, pg, sessions, appService)
	authService := service.NewAuthService(cfg, log, pg, sessions, publisher, appService, securityService)
	adminService, err := service.NewAdminService(cfg, log, pg, sessions)
	if err != nil {
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	adminService.SetSecurityService(securityService)
	userService := service.NewUserService(log, pg, sessions, publisher, securityService)
	userService.SetAdminUserSearchService(adminUserSearch)
	userService.SetAccountBanService(accountBanService)
	authService.SetAdminUserSearchService(adminUserSearch)
	adminUserSearch.StartWarmup(ctx)
	signInService := service.NewSignInService(log, pg, sessions, publisher)
	pointsService := service.NewPointsService(log, pg, sessions)
	realtimeService, err := service.NewRealtimeService(log, authService, realtimeRepo, natsConn)
	if err != nil {
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	realtimeService.SetAdminService(adminService)
	notificationService := service.NewNotificationService(log, pg, sessions, realtimeService)
	siteService := service.NewSiteService(pg)
	versionService := service.NewVersionService(pg)
	roleApplicationService := service.NewRoleApplicationService(pg)
	emailService := service.NewEmailService(log, pg, redisClient, cfg.Redis.KeyPrefix)
	paymentService := service.NewPaymentService(log, pg, cfg.PaymentBillExport)
	workflowService := service.NewWorkflowService(log, pg, temporalClient, cfg.Temporal)
	storageService := service.NewStorageService(log, pg, redisClient, cfg.Redis.KeyPrefix)
	avatarService := service.NewAvatarService(log, storageService, userService, adminService)
	captchaRepo := redisrepo.NewCaptchaRepository(redisClient, cfg.Redis.KeyPrefix)
	captchaService := service.NewCaptchaService(cfg, log, captchaRepo)
	// 注册短信服务商（启动时注册，运行时可动态扩展）
	captchaService.RegisterSMSProvider("aliyun", service.NewAliyunSMSProvider())
	captchaService.RegisterSMSProvider("tencent", service.NewTencentSMSProvider())
	monitorService := service.NewMonitorService(cfg, log, postgres, redisClient, natsConn, temporalClient, authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, avatarService, realtimeService, nil, nil, nil)
	ipBanRepo := redisrepo.NewIPBanRepository(redisClient, cfg.Redis.KeyPrefix)
	locationService := service.NewLocationService(log, redisClient, cfg.Redis.KeyPrefix, cfg.GeoIP)
	ipBanService := service.NewIPBanService(log, pg, ipBanRepo, locationService)
	firewall, err := middleware.NewFirewall(cfg.Firewall, log, redisClient, cfg.Redis.KeyPrefix, publisher, ipBanService)
	if err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	ldapService := service.NewLDAPService(log, cfg.JWT.Secret)
	adminService.SetLDAPService(ldapService)
	oidcService := service.NewOIDCService(log, cfg.JWT.Secret)
	adminService.SetOIDCService(oidcService)
	systemService := service.NewPlatformSettingsService(cfg, log, pg, firewall, securityService, ldapService, oidcService)
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
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	if err := systemService.Initialize(ctx); err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	if err := ipBanService.SyncBansToRedis(ctx); err != nil {
		log.Warn("启动时同步 IP 封禁到 Redis 失败", zap.Error(err))
	}
	if err := storageService.EnsureDefaultLocalConfig(ctx, fmt.Sprintf("http://localhost:%d", cfg.HTTPPort)); err != nil {
		log.Error("默认本地存储配置初始化失败", zap.Error(err))
	}
	replayRepo := redisrepo.NewReplayRepository(redisClient, cfg.Redis.KeyPrefix)
	replayGuard := middleware.NewReplayGuard(cfg.ReplayProtection, cfg.JWT.Secret, replayRepo, log)
	chainCommitter := service.NewChainCommitter(cfg.Lottery.ChainRPCURL, cfg.Lottery.ChainPrivateKey, cfg.Lottery.ChainID, log)
	lotteryService := service.NewLotteryService(log, pg, pointsService, chainCommitter)
	memoryManager := service.NewMemoryManager(cfg.Memory, log, redisClient, cfg.Redis.KeyPrefix)
	announcementService := service.NewAnnouncementService(log, pg, publisher)
	orgService := service.NewOrganizationService(log, pg)
	orgService.SetRealtimeService(realtimeService)
	approvalService := service.NewApprovalService(log, pg, realtimeService, orgService)
	templateService := service.NewTemplateService(log, pg)
	auditService := service.NewAuditService(log, pg)
	dashboardService := service.NewDashboardService(log, pg)
	pluginService := service.NewPluginService(log, pg)
	if err := pluginService.Initialize(ctx); err != nil {
		log.Warn("插件系统初始化失败（非致命）", zap.Error(err))
	}
	adminService.SetPluginService(pluginService)
	authService.SetPluginService(pluginService)
	userService.SetPluginService(pluginService)
	accountBanService.SetPluginService(pluginService)
	appService.SetPluginService(pluginService)
	paymentService.SetPluginService(pluginService)
	notificationService.SetPluginService(pluginService)
	storageService.SetPluginService(pluginService)
	systemService.SetPluginService(pluginService)
	sessionMgmtService := service.NewSessionMgmtService(log, pg, sessions, realtimeService)
	storageResourceService := service.NewStorageResourceService(log, pg)
	userMasterService := service.NewUserMasterService(log, pg)
	reportService := service.NewReportService(log, pg)
	riskService := service.NewRiskService(cfg.Risk, log, pg, redisClient, cfg.Redis.KeyPrefix)
	authService.SetRiskService(riskService)
	adminService.SetRiskService(riskService)
	accountBanService.Start()
	router, err := httptransport.NewRouter(authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, avatarService, monitorService, firewall, replayGuard, locationService, realtimeService, systemService, securityService, captchaService, firewallLogService, ipBanService, lotteryService, announcementService, ldapService, oidcService, sessions, orgService, templateService, auditService, pluginService, dashboardService, approvalService, sessionMgmtService, storageResourceService, userMasterService, reportService, riskService, memoryManager, cl, log, cfg.CORS)
	if err != nil {
		_ = accountBanService.Close(context.Background())
		locationService.Close()
		realtimeService.Close(context.Background())
		temporalClient.Close()
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
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       60 * time.Second,
	}

	// 启动监控后台采集器
	monitorService.StartCollector(ctx, 15*time.Second)

	// 启动内存管理系统
	memoryManager.Start(ctx)

	return &APIApp{
		Config:          cfg,
		Logger:          log,
		CrashLog:        cl,
		Router:          router,
		Server:          server,
		Postgres:        postgres,
		Redis:           redisClient,
		NATSConn:        natsConn,
		JetStream:       js,
		Temporal:        temporalClient,
		Realtime:        realtimeService,
		Payment:         paymentService,
		AdminUserSearch: adminUserSearch,
		AccountBan:      accountBanService,
		Location:        locationService,
		Monitor:         monitorService,
		Memory:          memoryManager,
		ShutdownTracing: shutdownTracing,
	}, nil
}

func (a *APIApp) Close(ctx context.Context) {
	if a.Memory != nil {
		a.Memory.Stop()
	}
	if a.Monitor != nil {
		a.Monitor.StopCollector()
	}
	if a.Server != nil {
		_ = a.Server.Shutdown(ctx)
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
	if a.Redis != nil {
		_ = a.Redis.Close()
	}
	if a.NATSConn != nil {
		a.NATSConn.Drain()
		a.NATSConn.Close()
	}
	if a.Temporal != nil {
		a.Temporal.Close()
	}
	if a.Postgres != nil {
		a.Postgres.Close()
	}
	if a.ShutdownTracing != nil {
		_ = a.ShutdownTracing(ctx)
	}
	if a.Logger != nil {
		_ = a.Logger.Sync()
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
