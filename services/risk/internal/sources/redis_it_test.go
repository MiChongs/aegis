package sources

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/redisx"
)

// RISK_IT=1 RISK_REDIS_ADDR_MAP=risk-redis-1:7001=127.0.0.1:7001,… RISK_REDIS_PASSWORD=aegis-risk
func TestRedisIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	cfg := config.Load()
	if len(cfg.RedisAddrMap) == 0 {
		cfg.RedisAddrMap = map[string]string{}
		for i := 1; i <= 6; i++ {
			cfg.RedisAddrMap[fmt.Sprintf("risk-redis-%d:%d", i, 7000+i)] = fmt.Sprintf("127.0.0.1:%d", 7000+i)
		}
	}
	if cfg.RedisPassword == "" {
		cfg.RedisPassword = "aegis-risk"
	}
	rdb := redisx.New(cfg)
	defer rdb.Close()
	ctx := context.Background()
	r := NewRedis(rdb)
	tag := fmt.Sprint(time.Now().UnixNano())

	// 滑窗计数：本次算在内
	for i := 1; i <= 5; i++ {
		v, err := r.Velocity(ctx, []engine.VelocityQuery{
			{Feature: "c", Key: "it:" + tag, WindowSeconds: 600},
			{Feature: "d", Key: "itd:" + tag, WindowSeconds: 600, Distinct: fmt.Sprint("acc", i%3)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if v["c"] != float64(i) {
			t.Fatalf("count #%d = %v", i, v["c"])
		}
		if i == 5 && v["d"] != 3 {
			t.Fatalf("distinct = %v", v["d"])
		}
	}

	// 流式特征：RisingWave 写回的值
	if err := rdb.Set(ctx, StreamPrefix+"pay_24h:1:"+tag, "128.5", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	sv, err := r.Stream(ctx, []engine.LookupQuery{{Feature: "pay", Key: "pay_24h:1:" + tag}, {Feature: "none", Key: "nope:" + tag}})
	if err != nil || sv["pay"] != 128.5 {
		t.Fatalf("stream = %v %v", sv, err)
	}
	if _, ok := sv["none"]; ok {
		t.Fatal("missing key should be absent")
	}

	// 名单
	idx := lists.NewIndex(rdb)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	ip := "203.0.113." + fmt.Sprint(time.Now().UnixNano()%250)
	entries := []lists.Entry{
		{ID: "b1-" + tag, ListType: bundle.ListIP, Value: ip, Kind: bundle.KindBlock, ExpiresAt: &future},
		{ID: "b2-" + tag, ListType: bundle.ListIP, Value: ip, Kind: bundle.KindWatch, ExpiresAt: &past},
		{ID: "b3-" + tag, ListType: bundle.ListDevice, Value: "dev-" + tag, Kind: bundle.KindAllow, AppID: 7, Scenes: []string{"login"}},
		{ID: "s1-" + tag, ListType: bundle.ListCIDR, Value: "198.51.100.0/24", Kind: bundle.KindBlock},
		{ID: "s2-" + tag, ListType: bundle.ListEmailDomain, Value: "spam-" + tag + ".com", Kind: bundle.KindWatch},
	}
	if err := idx.Put(ctx, entries); err != nil {
		t.Fatal(err)
	}
	if err := idx.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	hits, err := idx.Check(ctx, engine.ListQuery{AppID: 7, Scene: "login", Values: map[string]string{
		bundle.ListIP: ip, bundle.ListDevice: "dev-" + tag, bundle.ListCIDR: "198.51.100.9", bundle.ListEmailDomain: "mail.spam-" + tag + ".com",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.ListType+"/"+h.Kind] = true
	}
	for _, want := range []string{"ip/block", "device/allow", "cidr/block", "email_domain/watch"} {
		if !got[want] {
			t.Errorf("missing hit %s in %+v", want, hits)
		}
	}
	if got["ip/watch"] {
		t.Error("expired entry should not hit")
	}
	// 其它场景不命中限定场景的条目
	hits, _ = idx.Check(ctx, engine.ListQuery{AppID: 7, Scene: "payment", Values: map[string]string{bundle.ListDevice: "dev-" + tag}})
	if len(hits) != 0 {
		t.Errorf("scene filter: %+v", hits)
	}
	ttl := rdb.PTTL(ctx, "rl:{ip:"+ip+"}").Val()
	if ttl <= 0 || ttl > 2*time.Hour {
		t.Errorf("exact key ttl = %v", ttl)
	}
	_ = idx.Remove(ctx, entries)
}
