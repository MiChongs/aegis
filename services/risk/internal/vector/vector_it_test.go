package vector

import (
	"context"
	"os"
	"testing"
	"time"

	"aegis/services/risk/internal/config"
)

func TestEmbedSimilarity(t *testing.T) {
	a := map[string]string{"brand": "Xiaomi", "model": "2312DRA50C", "screen": "1220x2712@446", "os_version": "14.0", "platform": "android", "locale": "zh-CN", "timezone": "Asia/Shanghai"}
	b := map[string]string{}
	for k, v := range a {
		b[k] = v
	}
	c := map[string]string{"brand": "Apple", "model": "iPhone16,1", "screen": "1179x2556@460", "os_version": "18.1", "platform": "ios", "locale": "en-US", "timezone": "America/New_York"}
	cos := func(x, y []float32) float64 {
		var s float64
		for i := range x {
			s += float64(x[i] * y[i])
		}
		return s
	}
	if v := cos(Embed(a), Embed(b)); v < 0.999 {
		t.Fatalf("identical = %v", v)
	}
	if v := cos(Embed(a), Embed(c)); v > 0.3 {
		t.Fatalf("different = %v", v)
	}
	b["os_version"] = "14.1"
	if v := cos(Embed(a), Embed(b)); v < 0.8 || v > 0.99 {
		t.Fatalf("near = %v", v)
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
	farm := map[string]string{"brand": "Google", "model": "sdk_gphone64", "screen": "1080x2400@420", "os_version": "13", "emulator": "true"}
	rows := []Device{
		{AppID: app, DeviceID: "f1", UserID: 1, Vec: Embed(farm), Banned: true},
		{AppID: app, DeviceID: "f2", UserID: 2, Vec: Embed(farm)},
		{AppID: app, DeviceID: "real", UserID: 3, Vec: Embed(map[string]string{"brand": "Apple", "model": "iPhone15,2", "os_version": "17.4"})},
	}
	if err := c.Upsert(ctx, rows); err != nil {
		t.Fatal(err)
	}
	var ns []Neighbor
	for i := 0; i < 20; i++ {
		ns, err = c.Similar(ctx, app, "f3", Embed(farm), 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(ns) >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if len(ns) < 2 || ns[0].Score < 0.99 {
		t.Fatalf("neighbors = %+v", ns)
	}
	banned := 0
	for _, n := range ns {
		if n.Score > 0.95 && n.Banned {
			banned++
		}
	}
	if banned != 1 {
		t.Fatalf("banned similar = %d (%+v)", banned, ns)
	}
	if err := c.SetBanned(ctx, app, "f2", true); err != nil {
		t.Fatal(err)
	}
	t.Logf("neighbors: %+v", ns)
}
