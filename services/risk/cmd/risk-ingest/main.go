// risk-ingest 风控事件落地与风控侧处置（docs/risk-platform.md）。
//
// 依赖全部是必需的：任何一个存储写不进去，消息就不确认、留在 JetStream 里等重投，
// 不会出现「ClickHouse 有、关系图没有」之后再也补不上的情况。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/analytics"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/ingest"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/vector"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logf := func(format string, args ...any) { log.Info(fmt.Sprintf(format, args...)) }
	if err := run(logf); err != nil {
		log.Error("risk-ingest 退出", "err", err)
		os.Exit(1)
	}
}

func run(logf func(string, ...any)) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.PGDSN)
	if err != nil {
		return fmt.Errorf("postgres：%w", err)
	}
	defer st.Close()
	ch, err := analytics.Open(cfg)
	if err != nil {
		return fmt.Errorf("clickhouse：%w", err)
	}
	defer ch.Close()
	g, err := graph.Open(cfg)
	if err != nil {
		return fmt.Errorf("nebula：%w", err)
	}
	defer g.Close()
	v, err := vector.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("milvus：%w", err)
	}
	defer v.Close(ctx)
	rdb := redisx.New(cfg)
	defer rdb.Close()
	etcd, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("etcd：%w", err)
	}
	defer etcd.Close()
	bus, err := events.Connect(cfg.NATSURL, "", "risk-ingest-"+cfg.Instance, logf)
	if err != nil {
		return fmt.Errorf("nats：%w", err)
	}
	defer bus.Close()

	w := &ingest.Worker{Bus: bus, CH: ch, Graph: g, Vector: v, Store: st, Lists: lists.NewIndex(rdb), Etcd: etcd, Logf: logf,
		BatchSize: config.Int("RISK_INGEST_BATCH", 500), MaxWait: config.Duration("RISK_INGEST_WAIT", time.Second)}
	logf("risk-ingest 启动")
	return w.Run(ctx)
}
