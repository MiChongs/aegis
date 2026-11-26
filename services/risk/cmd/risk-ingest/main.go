// risk-ingest 风控事件落地与风控侧处置（docs/risk-platform.md）。
//
// 依赖全部是必需的：任何一个存储写不进去，消息就不确认、留在 JetStream 里等重投，
// 不会出现「ClickHouse 有、关系图没有」之后再也补不上的情况。
// 所以启动时缺哪个依赖就不开始消费，但也不退出：进程留着，在后台每 15 秒重连一次，
// 全部连上再开消费者（宿主机重启后 Nebula / Milvus 往往比这里起得慢）。
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

	st, err := connect(ctx, "postgres", logf, func() (*store.Store, error) { return store.Open(ctx, cfg.PGDSN) })
	if err != nil {
		return err
	}
	defer st.Close()
	ch, err := connect(ctx, "clickhouse", logf, func() (*analytics.Client, error) { return analytics.Open(cfg) })
	if err != nil {
		return err
	}
	defer ch.Close()
	g, err := connect(ctx, "nebula", logf, func() (*graph.Client, error) { return graph.Open(cfg) })
	if err != nil {
		return err
	}
	defer g.Close()
	v, err := connect(ctx, "milvus", logf, func() (*vector.Client, error) { return vector.Open(ctx, cfg) })
	if err != nil {
		return err
	}
	defer v.Close(context.Background())
	rdb := redisx.New(cfg)
	defer rdb.Close()
	etcd, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("etcd：%w", err)
	}
	defer etcd.Close()
	bus, err := connect(ctx, "nats", logf, func() (*events.Bus, error) {
		return events.Connect(cfg.NATSURL, "", "risk-ingest-"+cfg.Instance, logf)
	})
	if err != nil {
		return err
	}
	defer bus.Close()

	w := &ingest.Worker{Bus: bus, CH: ch, Graph: g, Vector: v, Store: st, Lists: lists.NewIndex(rdb), Etcd: etcd, Logf: logf,
		BatchSize: config.Int("RISK_INGEST_BATCH", 500), MaxWait: config.Duration("RISK_INGEST_WAIT", time.Second)}
	logf("risk-ingest 启动")
	return w.Run(ctx)
}

// connect 打开一个必需依赖：失败就每 15 秒重试，直到成功或收到退出信号（只在收到退出信号时返回错误）
func connect[T any](ctx context.Context, name string, logf func(string, ...any), open func() (T, error)) (T, error) {
	for i := 0; ; i++ {
		c, err := open()
		if err == nil {
			if i > 0 {
				logf("%s 已连接（第 %d 次尝试）", name, i+1)
			}
			return c, nil
		}
		if i == 0 || i%20 == 0 {
			logf("%s 未连接，暂不消费，稍后重试：%v", name, err)
		}
		select {
		case <-ctx.Done():
			var zero T
			return zero, fmt.Errorf("%s：%w", name, err)
		case <-time.After(15 * time.Second):
		}
	}
}
