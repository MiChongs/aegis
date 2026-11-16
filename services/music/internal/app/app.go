// Package app 用 fx 装配音乐个性化的三个服务：music-rec、music-ingest、music-trainer。
// 依赖相同（PostgreSQL、ClickHouse、Redis、NATS JetStream），各自只启动自己的那部分。
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"connectrpc.com/grpchealth"
	"connectrpc.com/otelconnect"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"aegis/api/gen/music/v1/musicv1connect"
	"aegis/services/music/contract"
	"aegis/services/music/internal/config"
	"aegis/services/music/internal/ingest"
	"aegis/services/music/internal/llm"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/rec"
	"aegis/services/music/internal/store"
	"aegis/services/music/internal/trainer"
)

// Role 进程角色。
type Role string

const (
	RoleRec     Role = "music-rec"
	RoleIngest  Role = "music-ingest"
	RoleTrainer Role = "music-trainer"
)

// New 组装一个角色的服务。
func New(cfg config.Config, role Role) *fx.App {
	opts := []fx.Option{
		fx.Supply(cfg, role),
		fx.Provide(newLogger, newRegistry, newPG, newCH, newRedis, newNATS, newJetStream, newRealtime),
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger { return &fxevent.ZapLogger{Logger: log.Named("fx")} }),
		fx.StopTimeout(30 * time.Second),
	}
	switch role {
	case RoleRec:
		opts = append(opts, fx.Provide(newService), fx.Invoke(startRec))
	case RoleIngest:
		opts = append(opts, fx.Invoke(startIngest))
	case RoleTrainer:
		opts = append(opts, fx.Invoke(startTrainer))
	}
	return fx.New(opts...)
}

func newLogger(cfg config.Config, role Role) (*zap.Logger, error) {
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
	return log.With(zap.String("service", string(role)), zap.String("instance", cfg.InstanceID)), nil
}

func newRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return reg
}

func newPG(lc fx.Lifecycle, cfg config.Config) (*store.PG, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg, err := store.OpenPG(ctx, cfg.PGDSN)
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { pg.Close(); return nil }})
	return pg, nil
}

func newCH(lc fx.Lifecycle, cfg config.Config) (*store.CH, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := store.OpenCH(ctx, store.CHOptions{Addr: cfg.ClickHouseAddr, DB: cfg.ClickHouseDB, User: cfg.ClickHouseUser,
		Password: cfg.ClickHousePassword, TZ: cfg.TZ})
	if err != nil {
		return nil, err
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return ch.Close() }})
	return ch, nil
}

func newRedis(lc fx.Lifecycle, cfg config.Config) (redis.UniversalClient, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("MUSIC_REDIS_URL 无效：%w", err)
	}
	client := redis.NewClient(opts)
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error { return client.Ping(ctx).Err() },
		OnStop:  func(context.Context) error { return client.Close() },
	})
	return client, nil
}

func newNATS(lc fx.Lifecycle, cfg config.Config, role Role, log *zap.Logger) (*nats.Conn, error) {
	conn, err := nats.Connect(cfg.NATSURL,
		nats.Name("aegis-"+string(role)+"/"+cfg.InstanceID),
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
		return nil, fmt.Errorf("连接 NATS 失败：%w", err)
	}
	lc.Append(fx.Hook{OnStop: func(context.Context) error { return conn.Drain() }})
	return conn, nil
}

func newJetStream(nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := contract.EnsureStreams(ctx, js); err != nil {
		return nil, err
	}
	return js, nil
}

func newRealtime(cfg config.Config, rdb redis.UniversalClient) *realtime.Store {
	return realtime.New(rdb, cfg.RedisPrefix)
}

func newService(cfg config.Config, pg *store.PG, ch *store.CH, rt *realtime.Store, js jetstream.JetStream, log *zap.Logger) *rec.Service {
	loc, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		loc = time.Local
	}
	return &rec.Service{
		Reg: rec.NewRegistry(pg, log.Named("models")), Cfgs: rec.NewConfigs(pg),
		PG: pg, CH: ch, RT: rt, JS: js, Log: log.Named("rec"), Loc: loc,
		Deep: rec.NewDeepRegistry(pg, log.Named("deep")),
		LLM:  llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.EmbedModel, cfg.EmbedDim, cfg.LLMConcurrency),
	}
}

// ready 就绪探针：各依赖都通。
func ready(pg *store.PG, ch *store.CH, rdb redis.UniversalClient, nc *nats.Conn) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := pg.Pool.Ping(ctx); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		if err := ch.Ping(ctx); err != nil {
			return fmt.Errorf("clickhouse: %w", err)
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		if !nc.IsConnected() {
			return errors.New("nats: disconnected")
		}
		return nil
	}
}

func probes(mux *http.ServeMux, check func(context.Context) error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready"))
	})
}

func serve(lc fx.Lifecycle, addr string, handler http.Handler, log *zap.Logger) {
	srv := &http.Server{Addr: addr, Handler: h2c.NewHandler(handler, &http2.Server{}), ReadHeaderTimeout: 10 * time.Second}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			l, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("listen %s: %w", addr, err)
			}
			go func() {
				if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error("http server stopped", zap.Error(err))
				}
			}()
			log.Info("listening", zap.String("addr", addr))
			return nil
		},
		OnStop: srv.Shutdown,
	})
}

func background(lc fx.Lifecycle, fns ...func(ctx context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, len(fns))
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			for _, fn := range fns {
				go func(fn func(context.Context)) { fn(ctx); done <- struct{}{} }(fn)
			}
			return nil
		},
		OnStop: func(stop context.Context) error {
			cancel()
			for range fns {
				select {
				case <-done:
				case <-stop.Done():
					return stop.Err()
				}
			}
			return nil
		},
	})
}

func startRec(lc fx.Lifecycle, cfg config.Config, svc *rec.Service, pg *store.PG, ch *store.CH, rdb redis.UniversalClient,
	nc *nats.Conn, js jetstream.JetStream, reg *prometheus.Registry, log *zap.Logger) error {
	otel, err := otelconnect.NewInterceptor()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	(&rec.RPC{Svc: svc, NC: nc}).Mount(mux, cfg.InternalToken, otel)
	(&rec.Admin{Svc: svc, NC: nc, JS: js, Token: cfg.InternalToken, Log: log.Named("admin")}).Mount(mux)
	mux.Handle(grpchealth.NewHandler(grpchealth.NewStaticChecker(musicv1connect.MusicServiceName)))
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	probes(mux, ready(pg, ch, rdb, nc))
	serve(lc, cfg.RPCAddr, mux, log)

	var subs []*nats.Subscription
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			s, err := svc.Reg.Subscribe(nc)
			if err != nil {
				return err
			}
			c, err := svc.Cfgs.Subscribe(nc)
			if err != nil {
				return err
			}
			o, err := svc.SubscribeOps(nc)
			if err != nil {
				return err
			}
			d, err := svc.Deep.Subscribe(nc)
			if err != nil {
				return err
			}
			subs = append(s, c, o, d)
			return nil
		},
		OnStop: func(context.Context) error {
			for _, s := range subs {
				_ = s.Unsubscribe()
			}
			return nil
		},
	})
	background(lc, func(ctx context.Context) {
		svc.Reg.Sync(ctx)
		svc.Deep.Sync(ctx)
		ticker := time.NewTicker(cfg.ModelPoll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				svc.Reg.Sync(ctx)
				svc.Deep.Sync(ctx)
			}
		}
	})
	return nil
}

func startIngest(lc fx.Lifecycle, cfg config.Config, pg *store.PG, ch *store.CH, rt *realtime.Store, rdb redis.UniversalClient,
	nc *nats.Conn, js jetstream.JetStream, reg *prometheus.Registry, log *zap.Logger) {
	in := &ingest.Ingest{JS: js, CH: ch, PG: pg, RT: rt, Batch: cfg.IngestBatch, Wait: cfg.IngestWait,
		Log: log.Named("ingest"), Metrics: ingest.NewMetrics(reg)}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	probes(mux, ready(pg, ch, rdb, nc))
	serve(lc, cfg.MetricsAddr, mux, log)
	background(lc, func(ctx context.Context) {
		for ctx.Err() == nil {
			if err := in.Run(ctx); err != nil {
				log.Error("ingest stopped, restarting", zap.Error(err))
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
			}
		}
	})
}

func startTrainer(lc fx.Lifecycle, cfg config.Config, pg *store.PG, ch *store.CH, rdb redis.UniversalClient,
	nc *nats.Conn, js jetstream.JetStream, reg *prometheus.Registry, log *zap.Logger) {
	t := &trainer.Trainer{PG: pg, CH: ch, NC: nc, JS: js, Redis: rdb, Prefix: cfg.RedisPrefix, Workers: cfg.TrainWorkers,
		Timeout: cfg.TrainTimeout, Log: log.Named("trainer")}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	probes(mux, ready(pg, ch, rdb, nc))
	serve(lc, cfg.MetricsAddr, mux, log)
	enricher := &trainer.Enricher{T: t, LLM: llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.EmbedModel, cfg.EmbedDim, cfg.LLMConcurrency)}
	background(lc, enricher.Run)
	background(lc,
		func(ctx context.Context) {
			for ctx.Err() == nil {
				if err := t.Consume(ctx); err != nil {
					log.Error("train consumer stopped, restarting", zap.Error(err))
					select {
					case <-ctx.Done():
					case <-time.After(5 * time.Second):
					}
				}
			}
		},
		t.Schedule,
	)
}
