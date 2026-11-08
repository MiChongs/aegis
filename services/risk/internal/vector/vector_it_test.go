package vector

import (
	"context"
	"os"
	"testing"
	"time"

	"aegis/services/risk/internal/config"
)

func TestSimilarity(t *testing.T) {
	cos := func(x, y []float32) float64 {
		var s float64
		for i := range x {
			s += float64(x[i] * y[i])
		}
		return s
	}
	real1, real2 := "108db9c2f1a64e0b9d3c7a5e2f8b6d41", "cfd6ed07a9b34c1e8f2d6b0a7c5e9f13"
	if v := Similarity(real1, real2); v > 0.3 {
		t.Fatalf("两个随机码 = %v", v)
	}
	if v := Similarity(real1, real1[:31]+"2"); v < 0.96 || v > 0.97 {
		t.Fatalf("32 位差 1 位 = %v", v)
	}
	long := real1 + real2
	if v := Similarity(long, long[:61]+"zzz"); v < 0.95 {
		t.Fatalf("64 位差 3 位 = %v", v)
	}
	if v := Similarity("ABC", "abc"); v != 1 {
		t.Fatalf("大小写 = %v", v)
	}
	if !Empty(Embed("0000-0000")) || !Empty(Embed("ua:19619c1804348206")) || !Empty(Embed(" ")) {
		t.Fatal("占位值与 UA 摘要不该有向量")
	}
	// 召回向量：只差末位的码应远比随机码近
	if a, b := cos(Embed(real1), Embed(real1[:31]+"2")), cos(Embed(real1), Embed(real2)); a < 0.85 || b > 0.4 {
		t.Fatalf("召回 near=%v far=%v", a, b)
	}
}

func TestVectorIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	ctx := context.Background()
	c, err := Open(ctx, config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close(ctx)
	if err := c.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	app := time.Now().UnixNano() % 1_000_000
	base := "5f0e9c1a7b3d4e2f8a6c0b9d1e7f3a2"
	f1, f2, f3, real := base+"1", base+"2", base+"3", "cfd6ed07a9b34c1e8f2d6b0a7c5e9f13"
	rows := []Device{
		{AppID: app, DeviceID: f1, UserID: 1, Vec: Embed(f1), Banned: true},
		{AppID: app, DeviceID: f2, UserID: 2, Vec: Embed(f2)},
		{AppID: app, DeviceID: real, UserID: 3, Vec: Embed(real)},
	}
	if err := c.Upsert(ctx, rows); err != nil {
		t.Fatal(err)
	}
	var ns []Neighbor
	for i := 0; i < 20; i++ {
		ns, err = c.Similar(ctx, app, f3, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(ns) >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(ns) < 2 || ns[0].Score < 0.96 || ns[len(ns)-1].Score > 0.5 {
		t.Fatalf("neighbors = %+v", ns)
	}
	banned := 0
	for _, n := range ns {
		if n.Score >= 0.95 && n.Banned {
			banned++
		}
	}
	if banned != 1 {
		t.Fatalf("banned similar = %d (%+v)", banned, ns)
	}
	if err := c.SetBanned(ctx, app, f2, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("neighbors: %+v", ns)
}
