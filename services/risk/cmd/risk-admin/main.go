// risk-admin 风控管理接口（docs/risk-platform.md）。
//
//	risk-admin            起服务（Hertz :8891，注册到 etcd 服务名 aegis.risk.admin）
//	risk-admin migrate    初始化全部存储：PG 库与表、内置场景与特征、JetStream 流、ClickHouse、Nebula、Milvus、MinIO，
//	                      导入旧风控中心仍有效的名单（主库 risk_lists），并从库里重建名单索引
//	risk-admin probe URL  健康检查（镜像里没有 wget / curl）
//	risk-admin eval JSON  经 etcd 发现 risk-decision 评估一次（排障用，JSON 是 EvaluateRequest）
//	risk-admin emit JSON  往 RISK 流发一条业务事件（排障用，JSON 是 events.Event）
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/admin"
	"aegis/services/risk/internal/analytics"
	"aegis/services/risk/internal/backfill"
	"aegis/services/risk/internal/catalog"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/models"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/stream"
	"aegis/services/risk/internal/vector"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/cloudwego/kitex/client"
	hertzetcd "github.com/hertz-contrib/registry/etcd"
	etcdreg "github.com/kitex-contrib/registry-etcd"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logf := func(format string, args ...any) { log.Info(fmt.Sprintf(format, args...)) }
	var err error
	switch {
	case len(os.Args) > 2 && os.Args[1] == "probe":
		err = probe(os.Args[2])
	case len(os.Args) > 2 && os.Args[1] == "eval":
		err = evalOnce(os.Args[2])
	case len(os.Args) > 2 && os.Args[1] == "emit":
		err = emit(os.Args[2], logf)
	case len(os.Args) > 1 && os.Args[1] == "migrate":
		err = migrate(logf)
	case len(os.Args) > 1 && os.Args[1] == "graph-backfill":
		err = graphBackfill(logf)
	default:
		err = serve(logf)
	}
	if err != nil {
		log.Error("risk-admin 退出", "err", err)
		os.Exit(1)
	}
}

func evalOnce(raw string) error {
	cfg := config.Load()
	var req riskv1.EvaluateRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		return err
	}
	resolver, err := etcdreg.NewEtcdResolver(cfg.EtcdEndpoints)
	if err != nil {
		return err
	}
	dc, err := decisionservice.NewClient("aegis.risk.decision", client.WithResolver(resolver), client.WithRPCTimeout(5*time.Second))
	if err != nil {
		return err
	}
	resp, err := dc.Evaluate(context.Background(), &req)
	if err != nil {
		return err
	}
	out, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(out))
	return nil
}

func emit(raw string, logf func(string, ...any)) error {
	cfg := config.Load()
	var e events.Event
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		return err
	}
	bus, err := events.Connect(cfg.NATSURL, "", "risk-emit", logf)
	if err != nil {
		return err
	}
	defer bus.Close()
	return bus.PublishEvent(context.Background(), e)
}

func probe(u string) error {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// StreamTarget RisingWave 视角的 NATS 与 Redis 地址（默认取风控服务自己的配置）
func StreamTarget(cfg config.Config) stream.Target {
	natsAddr := os.Getenv("RISK_STREAM_NATS")
	var natsUser, natsPass string
	if u, err := url.Parse(cfg.NATSURL); err == nil {
		if natsAddr == "" {
			natsAddr = u.Host
		}
		if u.User != nil {
			natsUser = u.User.Username()
			natsPass, _ = u.User.Password()
		}
	}
	addrs := cfg.RedisAddrs
	if v := os.Getenv("RISK_STREAM_REDIS_ADDRS"); v != "" {
		addrs = strings.Split(v, ",")
	}
	var urls []string
	for _, a := range addrs {
		if cfg.RedisPassword != "" {
			urls = append(urls, "redis://:"+url.QueryEscape(cfg.RedisPassword)+"@"+strings.TrimSpace(a))
		} else {
			urls = append(urls, "redis://"+strings.TrimSpace(a))
		}
	}
	return stream.Target{NATSServer: natsAddr, NATSUser: natsUser, NATSPassword: natsPass, RedisURLs: urls, DurablePrefix: "aegis_"}
}

func migrate(logf func(string, ...any)) error {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	retry := func(name string, fn func() error) error {
		var err error
		for i := 0; i < 30; i++ {
			if err = fn(); err == nil {
				logf("migrate: %s 就绪", name)
				return nil
			}
			logf("migrate: %s 未就绪（%v），重试", name, err)
			time.Sleep(3 * time.Second)
		}
		return fmt.Errorf("%s：%w", name, err)
	}

	var st *store.Store
	if err := retry("postgres", func() error {
		if err := store.EnsureDatabase(ctx, cfg.PGAdminDSN, cfg.PGDSN); err != nil {
			return err
		}
		s, err := store.Open(ctx, cfg.PGDSN)
		if err != nil {
			return err
		}
		if err := store.Migrate(ctx, s.Pool()); err != nil {
			s.Close()
			return err
		}
		st = s
		return catalog.Seed(ctx, s)
	}); err != nil {
		return err
	}
	defer st.Close()

	if err := retry("nats", func() error {
		bus, err := events.Connect(cfg.NATSURL, "", "risk-migrate", logf)
		if err != nil {
			return err
		}
		defer bus.Close()
		return events.EnsureStreams(ctx, bus.JetStream())
	}); err != nil {
		return err
	}
	if err := retry("clickhouse", func() error {
		c, err := analytics.Open(cfg)
		if err != nil {
			return err
		}
		defer c.Close()
		return c.Migrate(ctx)
	}); err != nil {
		return err
	}
	if err := retry("minio", func() error { return models.New(cfg).EnsureBucket(ctx) }); err != nil {
		return err
	}
	if err := retry("nebula", func() error { return graph.Migrate(cfg, logf) }); err != nil {
		return err
	}
	if err := retry("milvus", func() error {
		v, err := vector.Open(ctx, cfg)
		if err != nil {
			return err
		}
		defer v.Close(ctx)
		return v.Migrate(ctx)
	}); err != nil {
		return err
	}
	if err := retry("risingwave", func() error {
		m, err := stream.Connect(ctx, cfg.RisingWaveDSN, StreamTarget(cfg))
		if err != nil {
			return err
		}
		defer m.Close(ctx)
		_, err = m.Existing(ctx)
		return err
	}); err != nil {
		return err
	}
	if err := retry("旧名单导入", func() error {
		legacy, err := store.LegacyLists(ctx, cfg.PGAdminDSN)
		if err != nil {
			return err
		}
		for i := 0; i < len(legacy); i += 500 {
			if err := st.PutEntries(ctx, legacy[i:min(i+500, len(legacy))]); err != nil {
				return err
			}
		}
		logf("migrate: 旧风控中心名单导入 %d 条", len(legacy))
		return nil
	}); err != nil {
		return err
	}
	return retry("名单索引", func() error {
		rdb := redisx.New(cfg)
		defer rdb.Close()
		idx := lists.NewIndex(rdb)
		n := 0
		err := st.EachActiveEntry(ctx, func(es []lists.Entry) error { n += len(es); return idx.Put(ctx, es) })
		if err == nil {
			logf("migrate: 名单索引重建 %d 条", n)
		}
		return err
	})
}

func serve(logf func(string, ...any)) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.PGDSN)
	if err != nil {
		return fmt.Errorf("postgres：%w", err)
	}
	defer st.Close()
	rdb := redisx.New(cfg)
	defer rdb.Close()
	ch, err := analytics.Open(cfg)
	if err != nil {
		return fmt.Errorf("clickhouse：%w", err)
	}
	defer ch.Close()
	etcd, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		return fmt.Errorf("etcd：%w", err)
	}
	defer etcd.Close()
	bus, err := events.Connect(cfg.NATSURL, cfg.SpoolDir, "risk-admin-"+cfg.Instance, logf)
	if err != nil {
		return fmt.Errorf("nats：%w", err)
	}
	defer bus.Close()
	resolver, err := etcdreg.NewEtcdResolver(cfg.EtcdEndpoints)
	if err != nil {
		return err
	}
	dc, err := decisionservice.NewClient("aegis.risk.decision", client.WithResolver(resolver), client.WithRPCTimeout(10*time.Second))
	if err != nil {
		return err
	}
	idx := lists.NewIndex(rdb)
	_ = idx.Refresh(ctx)

	srv := &admin.Server{Cfg: cfg, Store: st, Lists: idx, Redis: rdb, CH: ch, Models: models.New(cfg), Etcd: etcd, Decision: dc, Bus: bus,
		Stream: StreamTarget(cfg), Logf: logf}
	// 图与向量可选：连不上照常提供其余接口
	if g, err := graph.Open(cfg); err == nil {
		srv.Graph = g
		defer g.Close()
	} else {
		logf("关系图未连接：%v", err)
	}
	if v, err := vector.Open(ctx, cfg); err == nil {
		srv.Vector = v
		defer v.Close(ctx)
	} else {
		logf("向量库未连接：%v", err)
	}
	reg, err := hertzetcd.NewEtcdRegistry(cfg.EtcdEndpoints)
	if err != nil {
		return err
	}
	logf("risk-admin 启动 %s", cfg.AdminAddr)
	// 从没回灌过关系图（新部署）就在后台自动跑一次；等服务起来、依赖都连上再开始
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(15 * time.Second):
			srv.AutoBackfill(ctx)
		}
	}()
	go srv.DailyBackfill(ctx)
	return srv.Run(ctx, reg)
}

// graphBackfill 命令行跑一次关系图回灌（前台、打印每个来源的结果）
func graphBackfill(logf func(string, ...any)) error {
	cfg := config.Load()
	ctx := context.Background()
	// 先补齐图模式（新加的点类型与属性），可重复执行
	if err := graph.Migrate(cfg, logf); err != nil {
		return err
	}
	g, err := graph.Open(cfg)
	if err != nil {
		return err
	}
	defer g.Close()
	var vc *vector.Client
	if v, err := vector.Open(ctx, cfg); err == nil {
		vc = v
		defer v.Close(ctx)
	}
	done := map[string]bool{}
	r := &backfill.Runner{MainDSN: cfg.PGAdminDSN, Graph: g, Vector: vc, Logf: logf, OnProgress: func(p backfill.Progress) {
		for _, st := range p.Steps {
			if (st.Status == "done" || st.Status == "failed" || st.Status == "skipped") && !done[st.Key] {
				done[st.Key] = true
				logf("关系图回灌：%s %s，%d 行，%d 秒 %s", st.Label, st.Status, st.Rows, st.Seconds, st.Error)
			}
		}
	}}
	p, err := r.Run(ctx)
	logf("关系图回灌结束：%d 位用户、%d 个 IP、%d 台设备、%d 条设备向量，写入 %v", p.Users, p.IPs, p.Devices, p.Vectors, p.Written)
	return err
}
