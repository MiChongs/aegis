package decision

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/models"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/release"
	"aegis/services/risk/internal/sources"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/server"
	etcdreg "github.com/kitex-contrib/registry-etcd"
	"github.com/nats-io/nats.go/jetstream"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// 端到端：etcd 下发发布 → Kitex（经 etcd 发现）→ 引擎 + Redis Cluster → 处置写名单 → 事件进 JetStream
func TestDecisionE2E(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	ctx := context.Background()
	cfg := config.Load()
	cfg.RedisPassword = "aegis-risk"
	cfg.RedisAddrMap = map[string]string{}
	for i := 1; i <= 6; i++ {
		cfg.RedisAddrMap[fmt.Sprintf("risk-redis-%d:%d", i, 7000+i)] = fmt.Sprintf("127.0.0.1:%d", 7000+i)
	}
	rdb := redisx.New(cfg)
	defer rdb.Close()
	idx := lists.NewIndex(rdb)
	if err := idx.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	bus, err := events.Connect(cfg.NATSURL, t.TempDir(), "e2e", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	if err := events.EnsureStreams(ctx, bus.JetStream()); err != nil {
		t.Fatal(err)
	}
	etcd, err := clientv3.New(clientv3.Config{Endpoints: cfg.EtcdEndpoints, DialTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer etcd.Close()

	tag := time.Now().UnixNano() % 1_000_000
	scene := fmt.Sprintf("e2e_login_%d", tag)
	b := bundle.Bundle{
		Version: "v1",
		Scene: bundle.Scene{Key: scene, Name: "端到端", TimeoutMS: 300, FailMode: "open", CaptchaSupported: true,
			Enforcement: bundle.Enforcement{Targets: []string{"device"}, DurationSeconds: 600, AutoBanAllowed: true}},
		Features: []bundle.Feature{{Key: "dev_hits", Kind: bundle.FeatureVelocity, Entity: bundle.EntityDevice, Velocity: &bundle.VelocityDef{WindowSeconds: 600, Agg: bundle.AggCount}}},
		Rules: []bundle.Rule{
			{Key: "dev_burst", Name: "设备突增", Dimension: bundle.DimDevice, Expr: "F.dev_hits >= 3", Score: 60, Mode: bundle.ModeEnforce},
			{Key: "dev_flood", Name: "设备刷量", Dimension: bundle.DimDevice, Expr: "F.dev_hits >= 5", Mode: bundle.ModeEnforce, Decision: bundle.ActionBan},
		},
	}
	b.Checksum = b.ComputeChecksum()
	if _, err := release.Publish(ctx, etcd, bundle.Release{Scene: scene, Primary: b}); err != nil {
		t.Fatal(err)
	}
	defer release.Withdraw(ctx, etcd, scene)

	reg := release.NewRegistry(etcd, models.New(cfg), t.Logf)
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _ = reg.Run(rctx) }()
	<-reg.Ready()
	h := &Handler{Registry: reg, Sources: &sources.Live{Lists: idx, Redis: sources.NewRedis(rdb)}, Lists: idx, Bus: bus, Redis: rdb, Instance: "e2e", Logf: t.Logf}

	svcName := fmt.Sprintf("aegis.risk.decision.e2e%d", tag)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	ln.Close()
	r, err := etcdreg.NewEtcdRegistry(cfg.EtcdEndpoints)
	if err != nil {
		t.Fatal(err)
	}
	svr := decisionservice.NewServer(h, server.WithServiceAddr(addr), server.WithRegistry(r),
		server.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: svcName}))
	go func() { _ = svr.Run() }()
	defer svr.Stop()

	resolver, err := etcdreg.NewEtcdResolver(cfg.EtcdEndpoints)
	if err != nil {
		t.Fatal(err)
	}
	cli, err := decisionservice.NewClient(svcName, client.WithResolver(resolver), client.WithRPCTimeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	// 订阅决策事件
	cons, err := bus.JetStream().CreateOrUpdateConsumer(ctx, events.StreamEvents, jetstream.ConsumerConfig{
		FilterSubject: events.SubjectDecisionPrefix + scene, DeliverPolicy: jetstream.DeliverNewPolicy, AckPolicy: jetstream.AckExplicitPolicy})
	if err != nil {
		t.Fatal(err)
	}

	device := fmt.Sprintf("e2e-dev-%d", tag)
	var last *riskv1.EvaluateResponse
	deadline := time.Now().Add(10 * time.Second)
	for i := 1; i <= 6; i++ {
		req := &riskv1.EvaluateRequest{Scene: scene, AppId: 5, Ip: "198.18.0.9", Account: "u", Device: &riskv1.Device{Id: device, Brand: "x"}, RequestId: fmt.Sprint("r", tag, i)}
		for {
			last, err = cli.Evaluate(ctx, req)
			if err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(300 * time.Millisecond) // 等 etcd 里的注册被解析到
		}
		if err != nil {
			t.Fatal(err)
		}
		want := map[int]string{1: "pass", 2: "pass", 3: "captcha", 4: "captcha", 5: "ban", 6: "block"}[i]
		if last.Action != want {
			t.Fatalf("#%d action=%s want %s (score %d, by %s, hits %v)", i, last.Action, want, last.Score, last.DecidedBy, last.Hits)
		}
	}
	if last.DecidedBy != "list" {
		t.Fatalf("6th should be blocked by the device list, got %s", last.DecidedBy)
	}
	// 幂等：同一个 requestId 返回第一次的结论
	again, err := cli.Evaluate(ctx, &riskv1.EvaluateRequest{Scene: scene, AppId: 5, Ip: "198.18.0.9", Account: "u", Device: &riskv1.Device{Id: device}, RequestId: fmt.Sprint("r", tag, 1)})
	if err != nil || again.Action != "pass" {
		t.Fatalf("idempotent replay = %v %v", again, err)
	}
	batch, err := cons.Fetch(6, jetstream.FetchMaxWait(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for m := range batch.Messages() {
		var e events.Event
		if json.Unmarshal(m.Data(), &e) == nil && e.Scene == scene && e.Trace != "" {
			n++
		}
		_ = m.Ack()
	}
	if n != 6 {
		t.Fatalf("decision events = %d", n)
	}
	_ = bus.JetStream().DeleteConsumer(ctx, events.StreamEvents, cons.CachedInfo().Name)
	t.Logf("last: %+v", last)
}
