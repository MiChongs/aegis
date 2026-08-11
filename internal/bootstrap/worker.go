package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	"aegis/pkg/crashlog"
	"aegis/pkg/egress"
	pkglogger "aegis/pkg/logger"
	"aegis/pkg/tracing"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	redislib "github.com/redis/go-redis/v9"
	"go.temporal.io/sdk/client"
	temporalworker "go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

type WorkerApp struct {
	Config         config.Config
	ConfigManager  *config.Manager
	Logger         *zap.Logger
	CrashLog       *crashlog.Logger
	Postgres       *pgxpool.Pool
	PostgresHandle *db.Postgres
	Database       *service.DatabaseManager
	Redis          *redislib.Client
	NATSConn       *nats.Conn
	JetStream      nats.JetStreamContext
	Temporal       client.Client
	TemporalWorker temporalworker.Worker
	AutoSign       *service.AutoSignService
	Events         *service.WorkerEventService
	FirewallLogs   *service.FirewallLogService
	Location       *service.LocationService
	IPBan          *service.IPBanService
	GeoRisk        *service.GeoRiskService
	GeoAnalytics   *service.GeoAnalyticsService
	// Tickets 只用于 SLA 巡检（预警 / 超时告警），工单读写走 API 侧
	Tickets *service.TicketService
	// Governance 平台治理判定（只读快照）：Worker 侧的发信同样要受治理约束。
	// 到期结算由 API 侧的循环负责，Worker 只跟着刷新快照。
	Governance *service.PlatformGovernanceService
	// Egress 出海代理网关：Worker 侧同样出网（通知投递 / 邮件 / GeoIP 更新），
	// 必须和 API 用同一份路由表，否则同一个域名两边走法不同
	Egress          *service.EgressService
	EgressGateway   *egress.Gateway
	OwnsEgress      bool
	ShutdownTracing func(context.Context) error
}

const (
	workerQueueAuthLoginAudit   = "aegis-worker-auth-login-audit"
	workerQueueSessionAudit     = "aegis-worker-session-audit"
	workerQueueUserMyAccessed   = "aegis-worker-user-my-accessed"
	workerQueueUserProfileCache = "aegis-worker-user-profile-cache"
	workerQueueUserSignedIn     = "aegis-worker-user-signed-in"
	workerQueueAutoSignSync     = "aegis-worker-auto-sign-sync"
	workerQueueFirewallBlocked  = "aegis-worker-firewall-blocked"
	workerQueueGeoRisk          = "aegis-worker-geo-risk"
)

func NewWorkerApp(ctx context.Context, cl *crashlog.Logger) (*WorkerApp, error) {
	manager, err := config.NewManager()
	if err != nil {
		return nil, err
	}
	return NewWorkerAppWithConfigManager(ctx, cl, manager)
}

func NewWorkerAppWithConfigManager(ctx context.Context, cl *crashlog.Logger, manager *config.Manager) (*WorkerApp, error) {
	if manager == nil {
		var err error
		manager, err = config.NewManager()
		if err != nil {
			return nil, err
		}
	}
	cfg := manager.Current()
	log, err := pkglogger.New(cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	tracingShutdown, err := tracing.Init(ctx, tracing.Config{
		Enabled:        cfg.Tracing.Enabled,
		ServiceName:    cfg.Tracing.ServiceName + "-worker",
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
		log.Warn("tracing 初始化失败", zap.Error(err))
	}
	shutdownTracing := func(ctx context.Context) error { return tracingShutdown(ctx) }
	// Unified 模式下 API 已经建过网关，这里会拿到同一个实例（owned=false）
	egressGateway, ownsEgress, err := ensureEgressGateway(cfg.Egress, log)
	if err != nil {
		return nil, fmt.Errorf("出海网关初始化失败: %w", err)
	}
	// Worker 同样走带生命周期治理的连接池：会话超时、慢查询追踪、排空关闭一并生效
	pgHandle, err := db.NewPostgresWithLifecycle(ctx, cfg.Postgres, cfg.Database, log)
	if err != nil {
		return nil, err
	}
	postgres := pgHandle.Pool
	redisClient := db.NewRedis(ctx, cfg.Redis)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		postgres.Close()
		return nil, err
	}
	natsConn, js, err := db.NewNATS(ctx, cfg.NATS)
	if err != nil {
		postgres.Close()
		_ = redisClient.Close()
		return nil, err
	}
	// Temporal 为可选依赖：不可达时仅告警并降级——Worker 会跳过 TemporalWorker 启动，
	// AutoSign 等基于 NATS 的订阅循环不受影响。
	temporalClient, err := db.NewTemporal(cfg.Temporal, log)
	if err != nil {
		log.Warn("temporal 不可达，Worker 将跳过 TemporalWorker 启动", zap.String("hostPort", cfg.Temporal.HostPort), zap.Error(err))
		temporalClient = nil
		err = nil
	}
	pg := pgrepo.New(postgres)
	egressService := service.NewEgressService(log, pg, egressGateway, cfg.Security.MasterKey, cfg.Egress)
	if err := egressService.Initialize(ctx); err != nil {
		log.Warn("加载持久化的出海网关配置失败，沿用 .env 配置", zap.Error(err))
	}
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	schedules := redisrepo.NewAutoSignRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	signInService := service.NewSignInService(log, pg, sessions, publisher)
	autoSignService := service.NewAutoSignService(cfg.AutoSign, log, pg, schedules, signInService)
	eventService := service.NewWorkerEventService(log, pg, sessions)
	locationService := service.NewLocationService(log, redisClient, cfg.Redis.KeyPrefix, cfg.GeoIP)
	ipBanRepo := redisrepo.NewIPBanRepository(redisClient, cfg.Redis.KeyPrefix)
	ipBanService := service.NewIPBanService(log, pg, ipBanRepo, locationService)
	firewallLogService := service.NewFirewallLogService(log, pg, locationService, ipBanService)
	geoProfiles := redisrepo.NewGeoProfileRepository(redisClient, cfg.Redis.KeyPrefix)
	geoRiskService := service.NewGeoRiskService(log, cfg.GeoRisk, pg, geoProfiles, locationService)
	geoAnalyticsService := service.NewGeoAnalyticsService(log, cfg.GeoRisk, pg)
	// 工单 SLA 巡检需要的最小依赖：统一通知出口 + 工单服务。
	// AdminService / StorageService 在 Worker 侧用不到（巡检不做权限判定、不传附件），故传 nil。
	//
	// 但**管理员收件箱必须接**：SLA 预警/超时是纯处理侧事件，收件人全是管理员，
	// 少了它这条链路会静默变成 skipped，正是早期实现的坑。
	workerNotificationService := service.NewNotificationService(log, pg, sessions, nil)
	workerEmailService := service.NewEmailService(log, pg, redisClient, cfg.Redis.KeyPrefix, cfg.Security.MasterKey)
	// Worker 不持有 WebSocket 连接，实时推送经 NATS 广播由 API 实例分发给在线客户端
	workerRealtimePublisher := service.NewNATSUserEventPublisher(log, natsConn)
	workerAdminInboxService := service.NewAdminInboxService(log, pg, workerRealtimePublisher)
	workerNotifyHub := service.NewNotifyHub(log, pg, cfg, workerEmailService, workerNotificationService,
		workerAdminInboxService, workerRealtimePublisher)
	if base := strings.TrimSpace(cfg.ConsoleBaseURL); base != "" {
		workerNotifyHub.SetConsoleBaseURL(base)
	}
	ticketService := service.NewTicketService(log, pg, nil, workerNotifyHub, nil)

	// 平台治理在 Worker 侧也要接：被冻结应用的对外发信不能因为"这封信是 Worker 发的"
	// 就绕过限制。Worker 不提供管理面，因此只装配判定所需的最小部分。
	workerGovernance := service.NewPlatformGovernanceService(log, pg, sessions)
	if err := workerGovernance.Initialize(ctx); err != nil {
		log.Warn("worker 平台治理状态加载失败，本轮判定按无治理放行", zap.Error(err))
	}
	workerNotificationService.SetGovernanceService(workerGovernance)
	workerEmailService.SetGovernanceService(workerGovernance)

	var tw temporalworker.Worker
	if temporalClient != nil {
		tw = temporalworker.New(temporalClient, cfg.Temporal.TaskQueue, temporalworker.Options{})
		service.RegisterTemporalWorkflowEngine(tw, log, pg)
	}
	return &WorkerApp{
		Config:          cfg,
		ConfigManager:   manager,
		Logger:          log,
		CrashLog:        cl,
		Postgres:        postgres,
		PostgresHandle:  pgHandle,
		Database:        service.NewDatabaseManager(log, cfg.Database, cfg.Postgres, pgHandle, redisClient, cfg.Redis.KeyPrefix, "worker"),
		Redis:           redisClient,
		NATSConn:        natsConn,
		JetStream:       js,
		Temporal:        temporalClient,
		TemporalWorker:  tw,
		AutoSign:        autoSignService,
		Events:          eventService,
		FirewallLogs:    firewallLogService,
		Location:        locationService,
		IPBan:           ipBanService,
		GeoRisk:         geoRiskService,
		GeoAnalytics:    geoAnalyticsService,
		Tickets:         ticketService,
		Governance:      workerGovernance,
		Egress:          egressService,
		EgressGateway:   egressGateway,
		OwnsEgress:      ownsEgress,
		ShutdownTracing: shutdownTracing,
	}, nil
}

func (w *WorkerApp) Run(ctx context.Context) error {
	if w.OwnsEgress && w.EgressGateway != nil {
		w.EgressGateway.Start(ctx)
	}
	registerWorkerConfigHotReload(w.ConfigManager, w.Logger, w.AutoSign, w.Egress)
	// Worker 侧同样采集：长事务与连接泄漏往往出在后台任务而非请求链路
	if w.Database != nil {
		w.Database.Start(ctx)
	}
	// 只收敛快照、不结算到期：到期恢复由 API 侧唯一负责，否则流水里会出现重复记录
	if w.Governance != nil {
		w.Governance.StartReadOnly(ctx)
	}
	if w.TemporalWorker != nil {
		if err := w.TemporalWorker.Start(); err != nil {
			return err
		}
	}
	_, err := w.JetStream.QueueSubscribe(event.SubjectAuthLoginAuditRequested, workerQueueAuthLoginAudit, func(msg *nats.Msg) {
		w.handleJSONMessage(msg, w.Events.HandleAuthLoginAudit)
	}, nats.ManualAck())
	if err != nil {
		return err
	}
	_, err = w.JetStream.QueueSubscribe(event.SubjectSessionAuditRequested, workerQueueSessionAudit, func(msg *nats.Msg) {
		w.handleJSONMessage(msg, w.Events.HandleSessionAudit)
	}, nats.ManualAck())
	if err != nil {
		return err
	}
	_, err = w.JetStream.QueueSubscribe(event.SubjectUserMyAccessed, workerQueueUserMyAccessed, func(msg *nats.Msg) {
		w.handleJSONMessage(msg, w.Events.HandleUserMyAccessed)
	}, nats.ManualAck())
	if err != nil {
		return err
	}
	_, err = w.JetStream.QueueSubscribe(event.SubjectUserProfileRefresh, workerQueueUserProfileCache, func(msg *nats.Msg) {
		w.logMessage("user.profile.cache.refresh.requested", msg.Data)
		_ = msg.Ack()
	}, nats.ManualAck())
	if err != nil {
		return err
	}
	_, err = w.JetStream.QueueSubscribe(event.SubjectUserSignedIn, workerQueueUserSignedIn, func(msg *nats.Msg) {
		w.handleJSONMessage(msg, w.Events.HandleUserSignedIn)
	}, nats.ManualAck())
	if err != nil {
		return err
	}

	_, err = w.JetStream.QueueSubscribe(event.SubjectUserAutoSignSync, workerQueueAutoSignSync, func(msg *nats.Msg) {
		payload := map[string]any{}
		_ = json.Unmarshal(msg.Data, &payload)
		userID := int64FromPayload(payload["user_id"])
		appID := int64FromPayload(payload["appid"])
		if userID > 0 && appID > 0 {
			if syncErr := w.AutoSign.SyncUserSchedule(context.Background(), userID, appID); syncErr != nil {
				w.Logger.Warn("auto sign sync failed", zap.Int64("user_id", userID), zap.Int64("appid", appID), zap.Error(syncErr))
			}
		}
		_ = msg.Ack()
	}, nats.ManualAck())
	if err != nil {
		return err
	}

	_, err = w.JetStream.QueueSubscribe(event.SubjectFirewallBlocked, workerQueueFirewallBlocked, func(msg *nats.Msg) {
		w.handleJSONMessage(msg, w.FirewallLogs.HandleFirewallBlocked)
	}, nats.ManualAck())
	if err != nil {
		return err
	}

	// 地理风控：与登录审计共用同一事件，使用独立 Queue Group 互不影响
	if w.GeoRisk != nil {
		_, err = w.JetStream.QueueSubscribe(event.SubjectAuthLoginAuditRequested, workerQueueGeoRisk, func(msg *nats.Msg) {
			w.handleJSONMessage(msg, w.GeoRisk.HandleLoginEvent)
		}, nats.ManualAck())
		if err != nil {
			return err
		}
	}

	// 地理分析：小时聚合 + 每日维护（分区滚动 / 画像基线重算）
	if w.GeoAnalytics != nil {
		SafeGo(w.Logger, w.CrashLog, "worker.geo_analytics", true, func() {
			w.runGeoAnalyticsLoop(ctx)
		})
	}

	// 工单 SLA 巡检：预警 / 超时通过统一通知出口告警
	if w.Tickets != nil {
		SafeGo(w.Logger, w.CrashLog, "worker.ticket_sla", true, func() {
			w.runTicketSLALoop(ctx)
		})
	}

	// 同步 IP 封禁到 Redis 并启动定时清理
	if w.IPBan != nil {
		if err := w.IPBan.SyncBansToRedis(ctx); err != nil {
			w.Logger.Warn("worker sync ip bans to redis failed", zap.Error(err))
		}
		SafeGo(w.Logger, w.CrashLog, "worker.ip_ban_cleanup", true, func() {
			w.runIPBanCleanupLoop(ctx)
		})
	}

	if w.AutoSign != nil {
		if scheduled, processed, catchUpErr := w.AutoSign.CatchUpOnStartup(ctx); catchUpErr != nil {
			w.Logger.Warn("auto sign startup catch-up failed", zap.Error(catchUpErr))
		} else {
			w.Logger.Info("auto sign startup catch-up completed", zap.Int("scheduled", scheduled), zap.Int("processed", processed))
		}

		SafeGo(w.Logger, w.CrashLog, "worker.auto_sign", true, func() {
			w.runAutoSignLoop(ctx)
		})
	}
	<-ctx.Done()
	return nil
}

func (w *WorkerApp) logMessage(subject string, data []byte) {
	payload := map[string]any{}
	_ = json.Unmarshal(data, &payload)
	w.Logger.Info("worker event received", zap.String("subject", subject), zap.Any("payload", payload))
}

func (w *WorkerApp) handleJSONMessage(msg *nats.Msg, handler func(context.Context, map[string]any) error) {
	payload := map[string]any{}
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		w.Logger.Warn("worker event decode failed", zap.String("subject", msg.Subject), zap.Error(err))
		_ = msg.Ack()
		return
	}
	if err := handler(context.Background(), payload); err != nil {
		w.Logger.Warn("worker event handle failed", zap.String("subject", msg.Subject), zap.Any("payload", payload), zap.Error(err))
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

func (w *WorkerApp) Close(ctx context.Context) {
	if w.Governance != nil {
		w.Governance.Stop()
	}
	if w.Location != nil {
		w.Location.Close()
	}
	if w.OwnsEgress {
		releaseEgressGateway(w.EgressGateway)
	}
	if w.Database != nil {
		w.Database.Stop()
	}
	if w.PostgresHandle != nil {
		w.PostgresHandle.Close(ctx)
	} else if w.Postgres != nil {
		w.Postgres.Close()
	}
	if w.Redis != nil {
		_ = w.Redis.Close()
	}
	if w.NATSConn != nil {
		w.NATSConn.Drain()
		w.NATSConn.Close()
	}
	if w.TemporalWorker != nil {
		w.TemporalWorker.Stop()
	}
	if w.Temporal != nil {
		w.Temporal.Close()
	}
	if w.ShutdownTracing != nil {
		_ = w.ShutdownTracing(ctx)
	}
	if w.Logger != nil {
		_ = w.Logger.Sync()
	}
}

func (w *WorkerApp) runAutoSignLoop(ctx context.Context) {
	tick := time.NewTimer(w.autoSignTickInterval())
	defer tick.Stop()
	rebuild := time.NewTimer(w.autoSignRebuildInterval())
	defer rebuild.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			processed, err := w.AutoSign.RunDue(ctx)
			if err != nil {
				w.Logger.Warn("auto sign due run failed", zap.Error(err))
			} else if processed > 0 {
				w.Logger.Info("auto sign due processed", zap.Int("processed", processed), zap.Int64("scheduled_count", w.AutoSign.ScheduledCount(ctx)))
			}
			tick.Reset(w.autoSignTickInterval())
		case <-rebuild.C:
			scheduled, err := w.AutoSign.RebuildSchedule(ctx)
			if err != nil {
				w.Logger.Warn("auto sign periodic rebuild failed", zap.Error(err))
			} else {
				w.Logger.Info("auto sign periodic rebuild completed", zap.Int("scheduled", scheduled))
			}
			rebuild.Reset(w.autoSignRebuildInterval())
		}
	}
}

func (w *WorkerApp) autoSignTickInterval() time.Duration {
	if w.AutoSign != nil {
		if interval := w.AutoSign.CurrentConfig().TickInterval; interval > 0 {
			return interval
		}
	}
	return time.Minute
}

func (w *WorkerApp) autoSignRebuildInterval() time.Duration {
	if w.AutoSign != nil {
		if interval := w.AutoSign.CurrentConfig().RebuildInterval; interval > 0 {
			return interval
		}
	}
	return 15 * time.Minute
}

// runGeoAnalyticsLoop 周期执行地理聚合与维护任务。
// 启动即跑一轮（确保分区存在 + 补齐停机期间的聚合缺口），随后按间隔滚动。
func (w *WorkerApp) runGeoAnalyticsLoop(ctx context.Context) {
	runRollup := func() {
		if err := w.GeoAnalytics.RunHourlyRollup(ctx); err != nil {
			w.Logger.Warn("geo stats rollup failed", zap.Error(err))
		}
	}
	w.GeoAnalytics.RunDailyMaintenance(ctx)
	runRollup()

	rollupTick := time.NewTicker(w.GeoAnalytics.RollupInterval())
	defer rollupTick.Stop()
	dailyTick := time.NewTicker(24 * time.Hour)
	defer dailyTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rollupTick.C:
			runRollup()
		case <-dailyTick.C:
			w.GeoAnalytics.RunDailyMaintenance(ctx)
		}
	}
}

// runTicketSLALoop 每分钟巡检一次工单 SLA。
// 状态跃迁（ontime → warning → breached）只在跨越阈值那一刻发一次通知，
// 因为 SetTicketSLAState 带 `sla_state <> $2` 条件，重复巡检不会重复打扰。
func (w *WorkerApp) runTicketSLALoop(ctx context.Context) {
	runScan := func() {
		warned, breached, err := w.Tickets.RunSLAScan(ctx, 500)
		if err != nil {
			w.Logger.Warn("ticket sla scan failed", zap.Error(err))
			return
		}
		if warned > 0 || breached > 0 {
			w.Logger.Info("ticket sla scan completed",
				zap.Int("warning", warned), zap.Int("breached", breached))
		}
	}
	runScan()

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			runScan()
		}
	}
}

func (w *WorkerApp) runIPBanCleanupLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			expired, err := w.IPBan.CleanupExpired(ctx)
			if err != nil {
				w.Logger.Warn("ip ban cleanup failed", zap.Error(err))
			} else if expired > 0 {
				w.Logger.Info("ip ban cleanup completed", zap.Int64("expired", expired))
			}
		}
	}
}

func int64FromPayload(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
