// risk-decision 在线决策服务（docs/risk-platform.md）。
//
// Kitex :8890 注册到 etcd（服务名 aegis.risk.decision），HTTP :8892 只有 /healthz /readyz。
// NebulaGraph 与 Milvus 连不上时照常启动：对应特征降级为默认值，后台每 30 秒重连。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/decision"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/models"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/release"
	"aegis/services/risk/internal/sources"
	"aegis/services/risk/internal/vector"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/cloudwego/kitex/pkg/limit"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/server"
	etcdreg "github.com/kitex-contrib/registry-etcd"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// ServiceName Kitex 服务名
const ServiceName = "aegis.risk.decision"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logf := func(format string, args ...any) { log.Info(fmt.Sprintf(format, args...)) }
	if err := run(log, logf); err != nil {
		log.Error("risk-decision 退出", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, logf func(string, ...any)) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rdb := redisx.New(cfg)
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis cluster：%w", err)
	}
	idx := lists.NewIndex(rdb)
	if err := idx.Refresh(ctx); err != nil {
		return fmt.Errorf("名单快照：%w", err)
	}
	go idx.Watch(ctx, 2*time.Second, func(err error) { log.Warn("名单快照刷新失败", "err", err) })

	live := &sources.Live{Lists: idx, Redis: sources.NewRedis(rdb)}
	var depMu sync.RWMutex
	deps := map[string]string{"redis": "ok", "nebula": "connecting", "milvus": "connecting"}
	setDep := func(k, v string) { depMu.Lock(); deps[k] = v; depMu.Unlock() }
	go connectOptional(ctx, func() error {
		g, err := graph.Open(cfg)
		if err != nil {
			setDep("nebula", err.Error())
			return err
		}
		live.Nebula = g
		setDep("nebula", "ok")
		return nil
	})
	go connectOptional(ctx, func() error {
		v, err := vector.Open(ctx, cfg)
		if err != nil {
			setDep("milvus", err.Error())
			return err
		}
		live.Milvus = v
		setDep("milvus", "ok")
		return nil
	})

	bus, err := events.Connect(cfg.NATSURL, cfg.SpoolDir, "risk-decision-"+cfg.Instance, logf)
	if err != nil {
		return fmt.Errorf("nats：%w", err)
	}
	defer bus.Close()

	etcd, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("etcd：%w", err)
	}
	defer etcd.Close()

	store := models.New(cfg)
	reg := release.NewRegistry(etcd, store, logf)
	go func() { _ = reg.Run(ctx) }()

	h := &decision.Handler{
		Registry: reg, Sources: live, Lists: idx, Bus: bus, Redis: rdb, Models: store, Instance: cfg.Instance,
		Logf: logf,
		Deps: func(context.Context) map[string]string {
			depMu.RLock()
			defer depMu.RUnlock()
			out := map[string]string{}
			for k, v := range deps {
				out[k] = v
			}
			if bus.Conn().IsConnected() {
				out["nats"] = "ok"
			} else {
				out["nats"] = "disconnected"
			}
			return out
		},
	}

	// 健康检查
	ready := false
	go func() {
		select {
		case <-reg.Ready():
			ready = true
		case <-ctx.Done():
		}
	}()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready {
			http.Error(w, "发布未加载", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	hs := &http.Server{Addr: cfg.DecisionHTTP, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hs.ListenAndServe() }()
	defer hs.Close()

	addr, err := net.ResolveTCPAddr("tcp", cfg.DecisionAddr)
	if err != nil {
		return err
	}
	registry, err := etcdreg.NewEtcdRegistry(cfg.EtcdEndpoints)
	if err != nil {
		return fmt.Errorf("etcd 注册：%w", err)
	}
	opts := []server.Option{
		server.WithServiceAddr(addr),
		server.WithRegistry(registry),
		server.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: ServiceName}),
		server.WithLimit(&limit.Option{MaxConnections: 20000, MaxQPS: 200000}),
		server.WithExitWaitTime(5 * time.Second),
	}
	svr := decisionservice.NewServer(h, opts...)
	go func() {
		<-ctx.Done()
		_ = svr.Stop()
	}()
	log.Info("risk-decision 启动", "addr", cfg.DecisionAddr, "instance", cfg.Instance)
	return svr.Run()
}

// connectOptional 可选依赖：失败就 30 秒后再试，直到成功或退出
func connectOptional(ctx context.Context, connect func() error) {
	for {
		if connect() == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
