package model

import (
	"context"
	"math"
	"testing"

	"aegis/services/music/internal/appcfg"
)

// 折入：用训练好的作品向量，按某人的正反馈解出的向量应与训练向量方向接近，且推荐的作品落在他那一类里。
func TestFoldInMatchesTrainedUser(t *testing.T) {
	m := synthetic(t, 80)
	rows, nItems := m.Rows, len(m.Items)
	cfg := appcfg.Default()
	cfg.ALS.Factors = 16
	cfg.ALS.Iterations = 10
	als, err := TrainALS(context.Background(), rows, nItems, cfg.ALS, 2, 7)
	if err != nil {
		t.Fatal(err)
	}
	s := &Snapshot{Factors: 16, ItemFactors: als.Y, UserFactors: als.X, Gram: gramian(als.Y, 16),
		Lambda: float32(cfg.ALS.Regularization), Alpha: float32(cfg.ALS.Alpha)}
	for _, st := range m.Items {
		s.Items = append(s.Items, st.key)
	}
	s.Index()
	u := 3
	var items []int32
	var conf []float32
	for _, e := range rows[u] {
		if e.Score > 0 {
			items = append(items, e.Item)
			conf = append(conf, float32(1+cfg.ALS.Alpha*math.Log1p(float64(e.Score)/0.5)))
		}
	}
	x := s.FoldIn(items, conf)
	if x == nil {
		t.Fatal("fold-in returned nil")
	}
	trained := als.X[u*16 : (u+1)*16]
	cos := float64(Dot(x, trained)) / math.Sqrt(float64(Dot(x, x))*float64(Dot(trained, trained)))
	if cos < 0.9 {
		t.Fatalf("fold-in vector cosine to trained = %.3f", cos)
	}
}

func TestRankerLearnsSignal(t *testing.T) {
	var samples []Sample
	seed := uint64(1)
	rnd := func() float32 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return float32(seed>>40) / float32(1<<24)
	}
	for i := 0; i < 6000; i++ {
		x := make([]float32, NumRankFeatures)
		for k := range x {
			x[k] = rnd()
		}
		// 真实规律：完播率高、早跳率低的更会被点；als 通道只是噪声
		y := x[7]*2-x[8]*1.5+rnd()*0.8 > 0.9
		samples = append(samples, Sample{X: x, Y: y, TS: int64(i), Base: x[0]})
	}
	r := TrainRanker(samples, RankerParams{MinSamples: 1000, L2: 0.001, Epochs: 6})
	if !r.Enabled || r.AUC < 0.8 || r.AUC <= r.BaselineAUC {
		t.Fatalf("ranker = enabled %v auc %.3f base %.3f reason %s", r.Enabled, r.AUC, r.BaselineAUC, r.Reason)
	}
	if r.Weights[7] <= 0 || r.Weights[8] >= 0 {
		t.Fatalf("weights complete=%.3f skip=%.3f", r.Weights[7], r.Weights[8])
	}
	if few := TrainRanker(samples[:100], RankerParams{MinSamples: 1000, Epochs: 2}); few.Enabled || few.Reason == "" {
		t.Fatal("ranker with few samples must be disabled")
	}
}

func TestKMeansSeparatesClusters(t *testing.T) {
	var vecs [][]float32
	for i := 0; i < 20; i++ {
		v := []float32{1, 0.1 * float32(i%3), 0}
		if i%2 == 1 {
			v = []float32{0, 0.1 * float32(i%3), 1}
		}
		vecs = append(vecs, v)
	}
	a := KMeans(vecs, 2, 10)
	for i := 2; i < 20; i++ {
		if (a[i] == a[i%2]) != true {
			t.Fatalf("point %d assigned %d, expected same as %d", i, a[i], i%2)
		}
	}
	if a[0] == a[1] {
		t.Fatal("two clusters merged")
	}
}

func TestArtistSimilarity(t *testing.T) {
	users := [][]ArtistWeight{
		{{Artist: "a", Weight: 1}, {Artist: "b", Weight: 0.8}},
		{{Artist: "a", Weight: 1}, {Artist: "b", Weight: 0.9}},
		{{Artist: "a", Weight: 1}, {Artist: "c", Weight: 0.2}},
		{{Artist: "d", Weight: 1}, {Artist: "e", Weight: 1}},
	}
	nb := artistSimilarity(users, 5)
	if len(nb["a"]) == 0 || nb["a"][0].Artist != "b" {
		t.Fatalf("similar to a = %+v", nb["a"])
	}
	for _, n := range nb["a"] {
		if n.Artist == "d" || n.Artist == "e" {
			t.Fatal("unrelated artist marked similar")
		}
	}
}
