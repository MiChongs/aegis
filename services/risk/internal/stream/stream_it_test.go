package stream

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/redisx"
)

func TestSQLRejectsInjection(t *testing.T) {
	f := bundle.Feature{Key: "x", Kind: bundle.FeatureStream, Entity: bundle.EntityUser,
		Stream: &bundle.StreamDef{Events: []string{"login.fail'; DROP TABLE t; --"}, Agg: "count", WindowSeconds: 60,
			Conditions: []bundle.Condition{{Field: "scene", Op: "eq", Value: "a' OR '1'='1"}}}}
	stmts, err := SQL(f, Target{NATSServer: "n:4222", RedisURLs: []string{"redis://r:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := "'login.fail''; DROP TABLE t; --'"; !contains(stmts[1], want) {
		t.Fatalf("not escaped:\n%s", stmts[1])
	}
	f.Stream.Conditions = []bundle.Condition{{Field: "1=1; --", Op: "eq", Value: 1}}
	if _, err := SQL(f, Target{}); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestStreamIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	ctx := context.Background()
	cfg := config.Load()
	bus, err := events.Connect(cfg.NATSURL, t.TempDir(), "it", t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	if err := events.EnsureStreams(ctx, bus.JetStream()); err != nil {
		t.Fatal(err)
	}
	tag := time.Now().UnixNano() % 1_000_000
	key := fmt.Sprintf("it_fail_%d", tag)
	f := bundle.Feature{Key: key, Kind: bundle.FeatureStream, Entity: bundle.EntityAccount,
		Stream: &bundle.StreamDef{Events: []string{events.TypeLoginFail}, Agg: bundle.AggCount, WindowSeconds: 3600}}
	sum := bundle.Feature{Key: key + "_amt", Kind: bundle.FeatureStream, Entity: bundle.EntityUser,
		Stream: &bundle.StreamDef{Events: []string{events.TypePaymentPaid}, Agg: bundle.AggSum, Field: "amount", WindowSeconds: 86400}}
	pw := "aegis-risk"
	var urls []string
	for i := 1; i <= 6; i++ {
		urls = append(urls, fmt.Sprintf("redis://:%s@risk-redis-%d:%d", pw, i, 7000+i))
	}
	m, err := Connect(ctx, cfg.RisingWaveDSN, Target{NATSServer: "nats:4222", RedisURLs: urls, StartupMode: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(ctx)
	m.OnDrop = func(ctx context.Context, durable string) {
		_ = bus.JetStream().DeleteConsumer(ctx, events.StreamEvents, durable)
	}
	res, err := m.Sync(ctx, []bundle.Feature{f, sum})
	if err != nil || len(res.Failed) > 0 {
		t.Fatalf("sync: %+v %v", res, err)
	}
	t.Logf("created %v", res.Created)
	time.Sleep(3 * time.Second) // 源的消费者从 latest 起读
	acc := fmt.Sprintf("victim%d", tag)
	for i := 0; i < 4; i++ {
		if err := bus.PublishEvent(ctx, events.Event{Type: events.TypeLoginFail, AppID: 9, Account: acc}); err != nil {
			t.Fatal(err)
		}
	}
	for _, a := range []float64{10, 32.5} {
		_ = bus.PublishEvent(ctx, events.Event{Type: events.TypePaymentPaid, AppID: 9, UserID: 77, Amount: a})
	}
	rcfg := cfg
	rcfg.RedisPassword = pw
	rcfg.RedisAddrMap = map[string]string{}
	for i := 1; i <= 6; i++ {
		rcfg.RedisAddrMap[fmt.Sprintf("risk-redis-%d:%d", i, 7000+i)] = fmt.Sprintf("127.0.0.1:%d", 7000+i)
	}
	rdb := redisx.New(rcfg)
	defer rdb.Close()
	var got, amt string
	for i := 0; i < 60; i++ {
		got, _ = rdb.Get(ctx, "rf:"+key+":9:"+acc).Result()
		amt, _ = rdb.Get(ctx, "rf:"+key+"_amt:9:77").Result()
		if got == "4" && amt == "42.5" {
			break
		}
		time.Sleep(time.Second)
	}
	if got != "4" || amt != "42.5" {
		t.Fatalf("redis values: count=%q sum=%q", got, amt)
	}
	// 删掉特征：对象一并删除
	res, err = m.Sync(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dropped %v", res.Dropped)
}
