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
	Router          *gin.Engine
	Server          *http.Server
	Postgres        *pgxpool.Pool
	Redis           *redislib.Client
	NATSConn        *nats.Conn
	JetStream       nats.JetStreamContext
	Temporal        client.Client
	Realtime        *service.RealtimeService
	Location        *service.LocationService
	ShutdownTracing func(context.Context) error
}

func NewAPIApp(ctx context.Context) (*APIApp, error) {
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
	sessions := redisrepo.NewSessionRepository(redisClient, cfg.Redis.KeyPrefix)
	realtimeRepo := redisrepo.NewRealtimeRepository(redisClient, cfg.Redis.KeyPrefix)
	publisher := event.NewPublisher(js)
	appService := service.NewAppService(log, pg, sessions)
	authService := service.NewAuthService(cfg, log, pg, sessions, publisher, appService)
	adminService, err := service.NewAdminService(cfg, log, pg, sessions)
	if err != nil {
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	userService := service.NewUserService(log, pg, sessions, publisher)
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
	notificationService := service.NewNotificationService(log, pg, sessions, realtimeService)
	siteService := service.NewSiteService(pg)
	versionService := service.NewVersionService(pg)
	roleApplicationService := service.NewRoleApplicationService(pg)
	emailService := service.NewEmailService(log, pg, redisClient, cfg.Redis.KeyPrefix)
	paymentService := service.NewPaymentService(log, pg)
	workflowService := service.NewWorkflowService(log, pg, temporalClient, cfg.Temporal)
	storageService := service.NewStorageService(log, pg, redisClient, cfg.Redis.KeyPrefix)
	firewall, err := middleware.NewFirewall(cfg.Firewall, log, redisClient, cfg.Redis.KeyPrefix)
	if err != nil {
		realtimeService.Close(context.Background())
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	locationService := service.NewLocationService(log, redisClient, cfg.Redis.KeyPrefix, cfg.GeoIP)
	if err := adminService.EnsureBootstrapSuperAdmin(ctx); err != nil {
		locationService.Close()
		realtimeService.Close(context.Background())
		temporalClient.Close()
		natsConn.Close()
		_ = redisClient.Close()
		postgres.Close()
		return nil, err
	}
	router, err := httptransport.NewRouter(authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, firewall, locationService, realtimeService)
	if err != nil {
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

	return &APIApp{
		Config:          cfg,
		Logger:          log,
		Router:          router,
		Server:          server,
		Postgres:        postgres,
		Redis:           redisClient,
		NATSConn:        natsConn,
		JetStream:       js,
		Temporal:        temporalClient,
		Realtime:        realtimeService,
		Location:        locationService,
		ShutdownTracing: shutdownTracing,
	}, nil
}

func (a *APIApp) Close(ctx context.Context) {
	if a.Server != nil {
		_ = a.Server.Shutdown(ctx)
	}
	if a.Realtime != nil {
		a.Realtime.Close(ctx)
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
