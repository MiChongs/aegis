package model

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"sort"
	"testing"
)

// writeSafetensors 测试用：按 safetensors 格式写一组 F32 张量。
func writeSafetensors(t *testing.T, tensors map[string][]float32, shapes map[string][]int) []byte {
	t.Helper()
	names := make([]string, 0, len(tensors))
	for n := range tensors {
		names = append(names, n)
	}
	sort.Strings(names)
	header := map[string]any{}
	var body []byte
	for _, n := range names {
		start := len(body)
		for _, v := range tensors[n] {
			body = binary.LittleEndian.AppendUint32(body, math.Float32bits(v))
		}
		header[n] = map[string]any{"dtype": "F32", "shape": shapes[n], "data_offsets": []int{start, len(body)}}
	}
	hb, _ := json.Marshal(header)
	out := binary.LittleEndian.AppendUint64(nil, uint64(len(hb)))
	return append(append(out, hb...), body...)
}

func tinyDeep(t *testing.T) (map[string][]byte, DeepMeta) {
	d, L, ffn, N := 4, 3, 8, 5
	seed := uint64(3)
	rnd := func(n int) []float32 {
		v := make([]float32, n)
		for i := range v {
			seed = seed*6364136223846793005 + 1442695040888963407
			v[i] = float32(int64(seed>>40)%2000-1000) / 2000
		}
		return v
	}
	ones := func(n int) []float32 {
		v := make([]float32, n)
		for i := range v {
			v[i] = 1
		}
		return v
	}
	ts := map[string][]float32{"item_repr": rnd(N * d), "pos_emb": rnd(L * d), "final_ln.weight": ones(d), "final_ln.bias": rnd(d)}
	sh := map[string][]int{"item_repr": {N, d}, "pos_emb": {L, d}, "final_ln.weight": {d}, "final_ln.bias": {d}}
	for _, n := range []string{"ln1.weight", "ln2.weight"} {
		ts["blocks.0."+n], sh["blocks.0."+n] = ones(d), []int{d}
	}
	for _, n := range []string{"ln1.bias", "ln2.bias", "attn.bq", "attn.bk", "attn.bv", "attn.bo", "ffn.b2"} {
		ts["blocks.0."+n], sh["blocks.0."+n] = rnd(d), []int{d}
	}
	for _, n := range []string{"attn.wq", "attn.wk", "attn.wv", "attn.wo"} {
		ts["blocks.0."+n], sh["blocks.0."+n] = rnd(d*d), []int{d, d}
	}
	ts["blocks.0.ffn.w1"], sh["blocks.0.ffn.w1"] = rnd(d*ffn), []int{d, ffn}
	ts["blocks.0.ffn.b1"], sh["blocks.0.ffn.b1"] = rnd(ffn), []int{ffn}
	ts["blocks.0.ffn.w2"], sh["blocks.0.ffn.w2"] = rnd(ffn*d), []int{ffn, d}
	meta := DeepMeta{Format: 1, D: d, Heads: 2, Layers: 1, FFN: ffn, MaxLen: L, Eps: 1e-5, Items: []string{"a", "b", "c", "d", "e"}}
	raw, _ := json.Marshal(meta)
	return map[string][]byte{"model": writeSafetensors(t, ts, sh), "meta": raw}, meta
}

func TestDeepLoadEncodeAndParity(t *testing.T) {
	blobs, _ := tinyDeep(t)
	m, err := LoadDeep(blobs)
	if err != nil {
		t.Fatal(err)
	}
	u := m.Encode([]int32{0, 3, 1, 2}) // 超过 maxLen=3，只取最后 3 个
	if len(u) != 4 {
		t.Fatalf("u = %v", u)
	}
	if v := m.Encode([]int32{3, 1, 2}); !equalF(v, u) {
		t.Fatal("truncation to the last maxLen items")
	}
	if m.Encode(nil) != nil {
		t.Fatal("empty sequence")
	}
	// LayerNorm 权重为 1 时，final_ln 输出减去 bias 后均值为 0
	var mean float64
	for i, v := range u {
		mean += float64(v) - float64(m.lnb[i])
	}
	if math.Abs(mean) > 1e-4 {
		t.Fatalf("final layer norm mean = %v", mean)
	}
	// 对齐用例：自己算的结果写回去应通过，改一位应被拒绝
	parity, _ := json.Marshal(map[string]any{"cases": []map[string]any{{"seq": []int{3, 1, 2}, "user": u}}})
	blobs["parity"] = parity
	if _, err := LoadDeep(blobs); err != nil {
		t.Fatalf("parity with itself: %v", err)
	}
	bad := append([]float32(nil), u...)
	bad[0] += 0.01
	parity, _ = json.Marshal(map[string]any{"cases": []map[string]any{{"seq": []int{3, 1, 2}, "user": bad}}})
	blobs["parity"] = parity
	if _, err := LoadDeep(blobs); err == nil {
		t.Fatal("parity mismatch must reject the model")
	}
	top := m.TopK(u, 3, func(i int32) bool { return i == 2 })
	if len(top) != 3 || top[0].Score < top[1].Score || top[1].Score < top[2].Score {
		t.Fatalf("top = %+v", top)
	}
	for _, s := range top {
		if s.Item == 2 {
			t.Fatal("excluded item returned")
		}
	}
}

func TestDeepRejectsBadShapes(t *testing.T) {
	blobs, meta := tinyDeep(t)
	meta.D = 8
	raw, _ := json.Marshal(meta)
	blobs["meta"] = raw
	if _, err := LoadDeep(blobs); err == nil {
		t.Fatal("shape mismatch accepted")
	}
}

func equalF(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(float64(a[i]-b[i])) > 1e-6 {
			return false
		}
	}
	return true
}

func TestRankFeatureCount(t *testing.T) {
	// 第三期特征版本：20 个，与 music-deep 的 LightGBM、rank_samples 里记录的长度一致
	if NumRankFeatures != 20 || RankFeatures[16] != "seq" || RankFeatures[19] != "llm" || RankFeatures[FeatPosition] != "position" {
		t.Fatalf("rank features = %d %v", NumRankFeatures, RankFeatures)
	}
}

// 用 music-deep（LightGBM 4.x 导出后改写为 v3 文本头）产出的精排文件：leaves 加载后的原始分与 LightGBM 一致。
func TestDeepLightGBMFixture(t *testing.T) {
	txt, err := os.ReadFile("testdata/ranker_v3.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile("testdata/ranker_cases.json")
	var cases struct {
		X   [][]float64 `json:"x"`
		Raw []float64   `json:"raw"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	blobs, _ := tinyDeep(t)
	blobs["ranker"] = txt
	m, err := LoadDeep(blobs)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([][]float32, len(cases.X))
	for i, x := range cases.X {
		rows[i] = make([]float32, len(x))
		for k, v := range x {
			rows[i][k] = float32(v)
		}
	}
	got := m.RankScores(rows)
	for i := range got {
		if math.Abs(got[i]-cases.Raw[i]) > 1e-4 {
			t.Fatalf("case %d: leaves %.6f, lightgbm %.6f", i, got[i], cases.Raw[i])
		}
	}
	if !m.RankerEnabled() {
		t.Fatal("ranker should be enabled when meta has no disabled flag")
	}
}
