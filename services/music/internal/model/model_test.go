package model

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/signal"
	"aegis/services/music/internal/store"
)

// synthetic 两类口味：前一半人听 0–29 号歌（同一组歌手），后一半人听 30–59 号歌；每人随机听其中 15 首。
func synthetic(t *testing.T, users int) *Matrix {
	t.Helper()
	cfg := appcfg.Default()
	now := time.Now()
	b := NewBuilder(cfg, now)
	rng := rand.New(rand.NewPCG(1, 2))
	for u := 1; u <= users; u++ {
		group := 0
		if u > users/2 {
			group = 30
		}
		picked := rng.Perm(30)[:15]
		ts := now.Add(-48 * time.Hour).UnixMilli()
		for _, k := range picked {
			ts += 4 * 60 * 1000
			item := group + k
			b.Add(&store.InteractionRow{UserID: int64(u), WorkKey: fmt.Sprintf("w%02d", item), Type: "play_end", TS: ts,
				PlayedMs: 200000, DurationMs: 220000, Reason: "completed", SessionID: "s",
				Title: fmt.Sprintf("歌%02d", item), Artist: fmt.Sprintf("歌手%d", item/5)})
		}
		// 每人对另一组的一首歌早跳
		b.Add(&store.InteractionRow{UserID: int64(u), WorkKey: fmt.Sprintf("w%02d", (group+30)%60), Type: "play_end", TS: ts + 1000,
			PlayedMs: 5000, DurationMs: 220000, Reason: "skipped", Title: "x", Artist: "y"})
	}
	return b.Finish()
}

func TestBuilderMatrix(t *testing.T) {
	m := synthetic(t, 40)
	if len(m.Users) != 40 || len(m.Items) != 60 {
		t.Fatalf("users=%d items=%d", len(m.Users), len(m.Items))
	}
	neg := 0
	for _, row := range m.Rows {
		for _, e := range row {
			if e.Score < 0 {
				neg++
			}
		}
	}
	if neg == 0 {
		t.Fatal("skips should give negative entries")
	}
	if len(m.Trans) == 0 {
		t.Fatal("session transitions missing")
	}
}

func TestALSAndItemCFLearnClusters(t *testing.T) {
	m := synthetic(t, 60)
	cfg := appcfg.Default()
	cfg.ALS.Factors, cfg.ALS.Iterations = 16, 10
	snap, err := Build(context.Background(), m, cfg, 4, 42, func(string, time.Time) {})
	if err != nil {
		t.Fatal(err)
	}
	// 第 1 个人属于前一组：ALS 打分最高的作品应当大多在 0–29
	u, ok := snap.User(m.Users[0])
	if !ok {
		t.Fatal("user missing")
	}
	scores := make([]float32, len(snap.Items))
	for i := range snap.Items {
		scores[i] = Dot(snap.UserVec(u), snap.ItemVec(int32(i)))
	}
	top := topK(scores, nil, 10)
	inGroup := 0
	for _, i := range top {
		var n int
		fmt.Sscanf(snap.Items[i], "w%d", &n)
		if n < 30 {
			inGroup++
		}
	}
	if inGroup < 9 {
		t.Fatalf("ALS top10 in own group = %d", inGroup)
	}
	// ItemCF 邻居同组
	i, _ := snap.Item("w05")
	same := 0
	for _, n := range snap.Neighbors[i][:10] {
		var k int
		fmt.Sscanf(snap.Items[n.Item], "w%d", &k)
		if k < 30 {
			same++
		}
	}
	if same < 9 {
		t.Fatalf("itemcf neighbors in group = %d", same)
	}
	if len(snap.UserArtists[u]) == 0 || len(snap.Trending) == 0 || len(snap.ArtistTop) == 0 {
		t.Fatal("profile / trending / artist top missing")
	}
	// 编码往返
	blob, err := snap.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Items) != len(snap.Items) || back.Factors != 16 {
		t.Fatal("decode mismatch")
	}
	if _, ok := back.Item("w05"); !ok {
		t.Fatal("index not rebuilt")
	}
	back.ForgetOnLoad(m.Users[0])
	if _, ok := back.User(m.Users[0]); ok {
		t.Fatal("forgotten user still indexed")
	}
}

func TestEvaluateBeatsPopularity(t *testing.T) {
	m := synthetic(t, 80)
	cfg := appcfg.Default()
	cfg.ALS.Factors, cfg.ALS.Iterations = 16, 10
	metrics, err := Evaluate(context.Background(), m, cfg, 4, 7)
	if err != nil {
		t.Fatal(err)
	}
	if metrics == nil || metrics.TestUsers < 10 {
		t.Fatalf("metrics = %+v", metrics)
	}
	if metrics.NDCG20 <= metrics.Baseline.NDCG20 || metrics.Recall20 <= metrics.Baseline.Recall20 {
		t.Fatalf("model should beat popularity on clustered data: %+v", metrics)
	}
}

func TestDecayAndConfidence(t *testing.T) {
	if d := signal.Decay(45, 45); d < 0.49 || d > 0.51 {
		t.Fatalf("decay = %f", d)
	}
	if signal.Confidence(0, 20) != 1 || signal.Confidence(3, 20) <= signal.Confidence(1, 20) {
		t.Fatal("confidence must grow with |score|")
	}
}
