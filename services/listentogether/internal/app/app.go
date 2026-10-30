// Package app 用 fx 装配一起听服务：依赖注入、生命周期、优雅关停。
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/grpchealth"
	"connectrpc.com/grpcreflect"
	"connectrpc.com/otelconnect"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"aegis/api/gen/listentogether/v1/listentogetherv1connect"
	"aegis/services/listentogether/internal/adminrpc"
	"aegis/services/listentogether/internal/bus"
	"aegis/services/listentogether/internal/config"
	"aegis/services/listentogether/internal/gateway"
	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/store"
	"aegis/services/listentogether/internal/telemetry"
	"aegis/services/listentogether/internal/ticket"
)

const (
	heartbeatEvery = 5 * time.Second
	heartbeatTTL   = 20 * time.Second
	sweepEvery     = 15 * time.Second
)

// New 组装整个服务。
func New(cfg config.Config) *fx.App {
	return fx.New(
		fx.Supply(cfg),
		fx.Provide(
			newLogger,
			telemetry.NewMetrics,
			newRedis,
			newNATS,
			newRepository,
			newBus,
			newVerifier,
			newRoomService,
			newHub,
			newGateway,
			newAdmin,
		),
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log.Named("fx")}
		}),
		fx.Invoke(registerTracing, registerServers, registerBackground),
		fx.StopTimeout(30*time.Second),
	)
}

func newLogger(cfg config.Config) (*zap.Logger, error) {
	var log *zap.Logger
	var err error
	if cfg.Development() {
		log, err = zap.NewDevelopment()
	} else {
		log, err = zap.NewProduction()
	}
	if err != nil {
		return nil, err
	}
	return log.With(zap.String("service", "listen-together"), zap.String("instance", cfg.InstanceID)), nil
}

// newRedis 没配置 Redis 时返回 nil，存储退回进程内实现。
func newRedis(lc fx.Lifecycle, cfg config.Config, log *zap.Logger) (*redis.Client, error) {
	if cfg.RedisURL == "" {
		log.Warn("LT_REDIS_URL 未配置：使用进程内存储，只能单实例运行")
		return nil, nil
	}
	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("LT_REDIS_URL 无效: %w", err)
	}
	client := redis.NewClient(options)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return client.Ping(ctx).Err() },
		OnStop:  func(context.Context) error { return client.Close() },
	})
	return client, nil
}

// newNATS 没配置 NATS 时返回 nil，总线退回进程内实现。
func newNATS(lc fx.Lifecycle, cfg config.Config, log *zap.Logger) (*nats.Conn, error) {
	if cfg.NATSURL == "" {
		log.Warn("LT_NATS_URL 未配置：使用进程内事件总线，只能单实例运行")
		return nil, nil
	}
	conn, err := nats.Connect(cfg.NATSURL,
		nats.Name("aegis-listen-together/"+cfg.InstanceID),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Warn("nats disconnected", zap.Error(err))
			}
		}),
		nats.ReconnectHandler(func(c *nats.Conn) { log.Info("nats reconnected", zap.String("url", c.ConnectedUrl())) }),
	)
	if err != nil {
		return nil, fmt.Errorf("连接 NATS 失败: %w", err)
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return conn.Drain() }})
	return conn, nil
}

func newRepository(cfg config.Config, client *redis.Client) room.Repository {
	if client == nil {
		return store.NewMemory()
	}
	return store.NewRedis(client, cfg.RedisPrefix)
}

func newBus(conn *nats.Conn, log *zap.Logger) room.Bus {
	if conn == nil {
		return bus.NewMemory()
	}
	return bus.NewNATS(conn, log.Named("bus"))
}

func newVerifier(lc fx.Lifecycle, cfg config.Config) (*ticket.Verifier, error) {
	if cfg.TicketJWKS != "" {
		return ticket.NewFromJWKS(cfg.TicketJWKS, cfg.TicketLeeway)
	}
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{OnStop: func(context.Context) error { cancel(); return nil }})
	return ticket.NewFromURL(ctx, cfg.TicketJWKSURL, cfg.TicketLeeway)
}

func newRoomService(cfg config.Config, repo room.Repository, b room.Bus, log *zap.Logger) *room.Service {
	return room.NewService(repo, b, time.Now, room.Options{
		Capacity: cfg.RoomCapacity, CapacityMax: cfg.RoomCapacityMax, QueueLimit: cfg.QueueLimit,
		ChatHistory: cfg.ChatHistory, MemberGrace: cfg.MemberGrace, IdleTTL: cfg.RoomIdleTTL,
		Instance: cfg.InstanceID,
	}, log.Named("room"))
}

func newHub(b room.Bus, metrics *telemetry.Metrics, log *zap.Logger) (*gateway.Hub, error) {
	return gateway.NewHub(b, metrics, log.Named("hub"))
}

func newGateway(cfg config.Config, rooms *room.Service, hub *gateway.Hub, verifier *ticket.Verifier, metrics *telemetry.Metrics, log *zap.Logger, rc *redis.Client, nc *nats.Conn) *gateway.Server {
	ready := func(ctx context.Context) error {
		if rc != nil {
			if err := rc.Ping(ctx).Err(); err != nil {
				return fmt.Errorf("redis: %w", err)
			}
		}
		if nc != nil && !nc.IsConnected() {
			return errors.New("nats: disconnected")
		}
		return nil
	}
	return gateway.NewServer(cfg, rooms, hub, verifier, metrics, log.Named("gateway"), ready)
}

func newAdmin(rooms *room.Service, repo room.Repository, b room.Bus, hub *gateway.Hub, log *zap.Logger) *adminrpc.Server {
	return adminrpc.New(rooms, repo, b, hub.UserConnections, log.Named("admin"))
}

func registerTracing(lc fx.Lifecycle, cfg config.Config) error {
	shutdown, err := telemetry.SetupTracing(context.Background(), cfg.OTLPEndpoint, cfg.InstanceID, cfg.Version)
	if err != nil {
		return err
	}
	lc.Append(fx.Hook{OnStop: shutdown})
	return nil
}

// registerServers 两个监听：对外（WebSocket + 探针）与对内（Connect/gRPC 管理接口 + 健康 + 反射 + 指标）。
// 对内端口用 h2c，gRPC 客户端不走 TLS 也能直连（只在内网暴露）。
func registerServers(lc fx.Lifecycle, cfg config.Config, gw *gateway.Server, hub *gateway.Hub, admin *adminrpc.Server, metrics *telemetry.Metrics, repo room.Repository, log *zap.Logger) error {
	otelInterceptor, err := otelconnect.NewInterceptor()
	if err != nil {
		return err
	}
	internalMux := http.NewServeMux()
	adminrpc.Mount(internalMux, admin, otelInterceptor, adminrpc.AuthInterceptor(cfg.InternalToken))
	checker := grpchealth.NewStaticChecker(listentogetherv1connect.ListenTogetherAdminServiceName)
	internalMux.Handle(grpchealth.NewHandler(checker))
	reflector := grpcreflect.NewStaticReflector(listentogetherv1connect.ListenTogetherAdminServiceName)
	internalMux.Handle(grpcreflect.NewHandlerV1(reflector, connect.WithInterceptors(adminrpc.AuthInterceptor(cfg.InternalToken))))
	internalMux.Handle(grpcreflect.NewHandlerV1Alpha(reflector, connect.WithInterceptors(adminrpc.AuthInterceptor(cfg.InternalToken))))
	internalMux.Handle("GET /metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{Registry: metrics.Registry}))

	public := &http.Server{Addr: cfg.PublicAddr, Handler: gw.Handler(), ReadHeaderTimeout: 10 * time.Second}
	internal := &http.Server{
		Addr: cfg.InternalAddr, Handler: h2c.NewHandler(internalMux, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			for _, srv := range []*http.Server{public, internal} {
				listener, err := net.Listen("tcp", srv.Addr)
				if err != nil {
					return fmt.Errorf("listen %s: %w", srv.Addr, err)
				}
				go func(srv *http.Server, l net.Listener) {
					if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
						log.Error("http server stopped", zap.String("addr", srv.Addr), zap.Error(err))
					}
				}(srv, listener)
			}
			log.Info("listen-together started", zap.String("public", cfg.PublicAddr), zap.String("internal", cfg.InternalAddr), zap.String("version", cfg.Version))
			return nil
		},
		OnStop: func(ctx context.Context) error {
			// 先摘流量，再让现有连接带着「稍后重连」的原因断开，最后关监听
			gw.Drain()
			hub.CloseAll(ctx)
			_ = repo.RemoveInstance(ctx, cfg.InstanceID)
			return errors.Join(public.Shutdown(ctx), internal.Shutdown(ctx))
		},
	})
	return nil
}

// registerBackground 实例心跳（在线判定靠它）与房间巡检（多实例下只有抢到锁的一个在做）。
func registerBackground(lc fx.Lifecycle, cfg config.Config, rooms *room.Service, repo room.Repository, hub *gateway.Hub, metrics *telemetry.Metrics, log *zap.Logger) {
	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now().UnixMilli()
	heartbeat := func() {
		conns, roomCount := hub.Counts()
		info := room.InstanceInfo{
			ID: cfg.InstanceID, Version: cfg.Version, StartedAt: started,
			Connections: conns, Rooms: roomCount, HeartbeatAt: time.Now().UnixMilli(),
		}
		if err := repo.Heartbeat(ctx, info, heartbeatTTL); err != nil && ctx.Err() == nil {
			log.Warn("heartbeat failed", zap.Error(err))
		}
	}
	sweep := func() {
		release, ok, err := repo.TryLock(ctx, "sweep", sweepEvery*2)
		if err != nil || !ok {
			return
		}
		defer release()
		begin := time.Now()
		instances, err := repo.Instances(ctx)
		if err != nil {
			log.Warn("load instances failed", zap.Error(err))
			return
		}
		alive := map[string]bool{cfg.InstanceID: true}
		for _, item := range instances {
			alive[item.ID] = true
		}
		if err := rooms.Sweep(ctx, alive); err != nil && ctx.Err() == nil {
			log.Warn("sweep failed", zap.Error(err))
		}
		metrics.SweepDuration.Observe(time.Since(begin).Seconds())
	}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			heartbeat()
			go loop(ctx, heartbeatEvery, heartbeat)
			go loop(ctx, sweepEvery, sweep)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
}

func loop(ctx context.Context, every time.Duration, fn func()) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}
