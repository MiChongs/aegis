package bootstrap

import (
	"context"
	"encoding/json"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
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
	Config          config.Config
	Logger          *zap.Logger
	Postgres        *pgxpool.Pool
	Redis           *redislib.Client
	NATSConn        *nats.Conn
	JetStream       nats.JetStreamContext
	Temporal        client.Client
	TemporalWorker  temporalworker.Worker
	AutoSign        *service.AutoSignService
	Events          *service.WorkerEventService
	ShutdownTracing func(context.Context) error
}

const (
	workerQueueAuthLoginAudit   = "aegis-worker-auth-login-audit"
	workerQueueSessionAudit     = "aegis-worker-session-audit"
	workerQueueUserMyAccessed   = "aegis-worker-user-my-accessed"
	workerQueueUserProfileCache = "aegis-worker-user-profile-cache"
	workerQueueUserSignedIn     = "aegis-worker-user-signed-in"
	workerQueueAutoSignSync     = "aegis-worker-auto-sign-sync"
)

func NewWorkerApp(ctx context.Context) (*WorkerApp, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	log, err := pkglogger.New(cfg.AppEnv)
	if err != nil {
		return nil, err
	}
	shutdownTracing := tracing.Init()
	postgres, err := db.NewPostgres(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}
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
	temporalClient, err := db.NewTemporal(cfg.Temporal, log)
	if err != nil {
		natsConn.Close()
		postgres.Close()
		_ = redisClient.Close()
		return nil, err
	}
	pg := pgrepo.New(postgres)
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	schedules := redisrepo.NewAutoSignRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	signInService := service.NewSignInService(log, pg, sessions, publisher)
	autoSignService := service.NewAutoSignService(cfg.AutoSign, log, pg, schedules, signInService)
	eventService := service.NewWorkerEventService(log, pg, sessions)
	tw := temporalworker.New(temporalClient, cfg.Temporal.TaskQueue, temporalworker.Options{})
	service.RegisterTemporalWorkflowEngine(tw, log, pg)
	return &WorkerApp{
		Config:          cfg,
		Logger:          log,
		Postgres:        postgres,
		Redis:           redisClient,
		NATSConn:        natsConn,
		JetStream:       js,
		Temporal:        temporalClient,
		TemporalWorker:  tw,
		AutoSign:        autoSignService,
		Events:          eventService,
		ShutdownTracing: shutdownTracing,
	}, nil
}

func (w *WorkerApp) Run(ctx context.Context) error {
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

	if w.Config.AutoSign.Enabled && w.AutoSign != nil {
		if scheduled, rebuildErr := w.AutoSign.RebuildSchedule(ctx); rebuildErr != nil {
			w.Logger.Warn("auto sign schedule rebuild failed", zap.Error(rebuildErr))
		} else {
			w.Logger.Info("auto sign schedule rebuilt", zap.Int("scheduled", scheduled))
		}

		go w.runAutoSignLoop(ctx)
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
	if w.Postgres != nil {
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
	tick := time.NewTicker(w.Config.AutoSign.TickInterval)
	defer tick.Stop()
	rebuild := time.NewTicker(w.Config.AutoSign.RebuildInterval)
	defer rebuild.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			processed, err := w.AutoSign.RunDue(ctx)
			if err != nil {
				w.Logger.Warn("auto sign due run failed", zap.Error(err))
				continue
			}
			if processed > 0 {
				w.Logger.Info("auto sign due processed", zap.Int("processed", processed), zap.Int64("scheduled_count", w.AutoSign.ScheduledCount(ctx)))
			}
		case <-rebuild.C:
			scheduled, err := w.AutoSign.RebuildSchedule(ctx)
			if err != nil {
				w.Logger.Warn("auto sign periodic rebuild failed", zap.Error(err))
				continue
			}
			w.Logger.Info("auto sign periodic rebuild completed", zap.Int("scheduled", scheduled))
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
