package model

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/dmitryikh/leaves"
)

// 深度序列模型（SASRec）的 Go 原生推理，导出格式与前向公式见 docs/music-personalization-3.md「导出」。
// 训练在 music-deep（PyTorch）里，这里只做前向：u = Transformer(最近听的歌)，分 = u · item_repr。

// DeepMeta 导出的元数据。
type DeepMeta struct {
	Format         int             `json:"format"`
	AppID          int64           `json:"appId"`
	RunID          int64           `json:"runId"`
	D              int             `json:"d"`
	Heads          int             `json:"heads"`
	Layers         int             `json:"layers"`
	FFN            int             `json:"ffn"`
	MaxLen         int             `json:"maxLen"`
	Eps            float64         `json:"eps"`
	Items          []string        `json:"items"`
	ItemCount      []int           `json:"itemCount"`
	TrainedAt      int64           `json:"trainedAt"`
	Metrics        json.RawMessage `json:"metrics"`
	RankerFeatures []string        `json:"rankerFeatures"`
	Ranker         json.RawMessage `json:"ranker"`
}

type deepBlock struct {
	ln1w, ln1b, ln2w, ln2b []float32
	wq, wk, wv, wo         []float32 // [d, d]
	bq, bk, bv, bo         []float32
	w1                     []float32 // [d, ffn]
	b1                     []float32
	w2                     []float32 // [ffn, d]
	b2                     []float32
}

// DeepModel 加载好的深度模型。
type DeepModel struct {
	Meta     DeepMeta
	itemRepr []float32 // [N, d]
	posEmb   []float32 // [maxLen, d]
	blocks   []deepBlock
	lnw, lnb []float32
	index    map[string]int32
	Ranker   *leaves.Ensemble
	// ParityMaxError 加载时对齐用例的最大误差
	ParityMaxError float64
}

// N 物品数。
func (m *DeepModel) N() int { return len(m.Meta.Items) }

// Item 作品键 → 下标。
func (m *DeepModel) Item(key string) (int32, bool) { i, ok := m.index[key]; return i, ok }

// ItemKey 下标 → 作品键。
func (m *DeepModel) ItemKey(i int32) string { return m.Meta.Items[i] }

// ItemVec 物品表示。
func (m *DeepModel) ItemVec(i int32) []float32 {
	d := m.Meta.D
	return m.itemRepr[int(i)*d : (int(i)+1)*d]
}

type stTensor struct {
	Dtype   string `json:"dtype"`
	Shape   []int  `json:"shape"`
	Offsets [2]int `json:"data_offsets"`
}

// parseSafetensors 解出全部 F32 张量。
func parseSafetensors(data []byte) (map[string][]float32, map[string][]int, error) {
	if len(data) < 8 {
		return nil, nil, errors.New("safetensors 文件太短")
	}
	n := binary.LittleEndian.Uint64(data[:8])
	if n > uint64(len(data)-8) || n > 100<<20 {
		return nil, nil, errors.New("safetensors 头长度不对")
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(data[8:8+n], &header); err != nil {
		return nil, nil, fmt.Errorf("safetensors 头解析失败：%w", err)
	}
	body := data[8+n:]
	out := map[string][]float32{}
	shapes := map[string][]int{}
	for name, raw := range header {
		if name == "__metadata__" {
			continue
		}
		var t stTensor
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, nil, err
		}
		if t.Dtype != "F32" {
			return nil, nil, fmt.Errorf("张量 %s 不是 F32（%s）", name, t.Dtype)
		}
		size := 1
		for _, s := range t.Shape {
			size *= s
		}
		if t.Offsets[0] < 0 || t.Offsets[1] > len(body) || t.Offsets[1]-t.Offsets[0] != size*4 {
			return nil, nil, fmt.Errorf("张量 %s 的数据范围不对", name)
		}
		buf := body[t.Offsets[0]:t.Offsets[1]]
		v := make([]float32, size)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
		}
		out[name] = v
		shapes[name] = t.Shape
	}
	return out, shapes, nil
}

// LoadDeep 从 music_deep_blobs 的产物加载（model、meta 必须有；ranker、parity 可选）。对齐用例误差 > 1e-3 时拒绝。
func LoadDeep(blobs map[string][]byte) (*DeepModel, error) {
	var meta DeepMeta
	if err := json.Unmarshal(blobs["meta"], &meta); err != nil {
		return nil, fmt.Errorf("深度模型 meta 解析失败：%w", err)
	}
	if meta.Format != 1 {
		return nil, fmt.Errorf("深度模型格式 %d 不认识", meta.Format)
	}
	d, L := meta.D, meta.MaxLen
	if d <= 0 || meta.Heads <= 0 || d%meta.Heads != 0 || L <= 0 || meta.Layers <= 0 || meta.FFN <= 0 {
		return nil, errors.New("深度模型维度参数不对")
	}
	if meta.Eps <= 0 {
		meta.Eps = 1e-5
	}
	ts, shapes, err := parseSafetensors(blobs["model"])
	if err != nil {
		return nil, err
	}
	get := func(name string, shape ...int) ([]float32, error) {
		v, ok := ts[name]
		if !ok {
			return nil, fmt.Errorf("缺少张量 %s", name)
		}
		if fmt.Sprint(shapes[name]) != fmt.Sprint(shape) {
			return nil, fmt.Errorf("张量 %s 形状 %v，应为 %v", name, shapes[name], shape)
		}
		return v, nil
	}
	m := &DeepModel{Meta: meta, index: make(map[string]int32, len(meta.Items))}
	N := len(meta.Items)
	if m.itemRepr, err = get("item_repr", N, d); err != nil {
		return nil, err
	}
	if m.posEmb, err = get("pos_emb", L, d); err != nil {
		return nil, err
	}
	for b := 0; b < meta.Layers; b++ {
		p := fmt.Sprintf("blocks.%d.", b)
		var blk deepBlock
		specs := []struct {
			dst   *[]float32
			name  string
			shape []int
		}{
			{&blk.ln1w, "ln1.weight", []int{d}}, {&blk.ln1b, "ln1.bias", []int{d}},
			{&blk.wq, "attn.wq", []int{d, d}}, {&blk.wk, "attn.wk", []int{d, d}}, {&blk.wv, "attn.wv", []int{d, d}}, {&blk.wo, "attn.wo", []int{d, d}},
			{&blk.bq, "attn.bq", []int{d}}, {&blk.bk, "attn.bk", []int{d}}, {&blk.bv, "attn.bv", []int{d}}, {&blk.bo, "attn.bo", []int{d}},
			{&blk.ln2w, "ln2.weight", []int{d}}, {&blk.ln2b, "ln2.bias", []int{d}},
			{&blk.w1, "ffn.w1", []int{d, meta.FFN}}, {&blk.b1, "ffn.b1", []int{meta.FFN}},
			{&blk.w2, "ffn.w2", []int{meta.FFN, d}}, {&blk.b2, "ffn.b2", []int{d}},
		}
		for _, sp := range specs {
			if *sp.dst, err = get(p+sp.name, sp.shape...); err != nil {
				return nil, err
			}
		}
		m.blocks = append(m.blocks, blk)
	}
	if m.lnw, err = get("final_ln.weight", d); err != nil {
		return nil, err
	}
	if m.lnb, err = get("final_ln.bias", d); err != nil {
		return nil, err
	}
	for i, k := range meta.Items {
		m.index[k] = int32(i)
	}
	if raw := blobs["ranker"]; len(raw) > 0 {
		ens, err := leaves.LGEnsembleFromReader(bufio.NewReader(bytes.NewReader(raw)), false)
		if err != nil {
			return nil, fmt.Errorf("LightGBM 精排加载失败：%w", err)
		}
		if ens.NFeatures() != NumRankFeatures {
			return nil, fmt.Errorf("LightGBM 精排特征数 %d，应为 %d", ens.NFeatures(), NumRankFeatures)
		}
		m.Ranker = ens
	}
	if raw := blobs["parity"]; len(raw) > 0 {
		var p struct {
			Cases []struct {
				Seq  []int32   `json:"seq"`
				User []float64 `json:"user"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("对齐用例解析失败：%w", err)
		}
		for k, c := range p.Cases {
			u := m.Encode(c.Seq)
			if len(u) != len(c.User) {
				return nil, fmt.Errorf("对齐用例 %d 维度不一致", k)
			}
			for i := range u {
				m.ParityMaxError = math.Max(m.ParityMaxError, math.Abs(float64(u[i])-c.User[i]))
			}
		}
		if m.ParityMaxError > 1e-3 {
			return nil, fmt.Errorf("Go 推理与训练输出不一致（最大误差 %.2e），拒绝加载", m.ParityMaxError)
		}
	}
	return m, nil
}

// Encode 输入序列（物品下标，旧 → 新）→ 用户向量 u。空序列返回 nil。
func (m *DeepModel) Encode(seq []int32) []float32 {
	d, L, H := m.Meta.D, m.Meta.MaxLen, m.Meta.Heads
	var s []int32
	for _, i := range seq {
		if i >= 0 && int(i) < m.N() {
			s = append(s, i)
		}
	}
	if len(s) > L {
		s = s[len(s)-L:]
	}
	n := len(s)
	if n == 0 {
		return nil
	}
	x := make([]float32, n*d)
	for t, it := range s {
		row := x[t*d : (t+1)*d]
		iv := m.itemRepr[int(it)*d : (int(it)+1)*d]
		pv := m.posEmb[(L-n+t)*d : (L-n+t+1)*d]
		for k := range row {
			row[k] = iv[k] + pv[k]
		}
	}
	dh := d / H
	h := make([]float32, n*d)
	q := make([]float32, n*d)
	kk := make([]float32, n*d)
	v := make([]float32, n*d)
	att := make([]float32, n*d)
	ffn := m.Meta.FFN
	hid := make([]float32, ffn)
	scores := make([]float64, n)
	scale := 1 / math.Sqrt(float64(dh))
	eps := m.Meta.Eps
	for _, b := range m.blocks {
		for t := 0; t < n; t++ {
			layerNorm(x[t*d:(t+1)*d], h[t*d:(t+1)*d], b.ln1w, b.ln1b, eps)
		}
		linear(h, n, d, b.wq, b.bq, d, q)
		linear(h, n, d, b.wk, b.bk, d, kk)
		linear(h, n, d, b.wv, b.bv, d, v)
		for i := range att {
			att[i] = 0
		}
		for hd := 0; hd < H; hd++ {
			off := hd * dh
			for t := 0; t < n; t++ {
				mx := math.Inf(-1)
				for j := 0; j <= t; j++ {
					var sdot float64
					for c := 0; c < dh; c++ {
						sdot += float64(q[t*d+off+c]) * float64(kk[j*d+off+c])
					}
					scores[j] = sdot * scale
					mx = math.Max(mx, scores[j])
				}
				var sum float64
				for j := 0; j <= t; j++ {
					scores[j] = math.Exp(scores[j] - mx)
					sum += scores[j]
				}
				for j := 0; j <= t; j++ {
					w := scores[j] / sum
					for c := 0; c < dh; c++ {
						att[t*d+off+c] += float32(w * float64(v[j*d+off+c]))
					}
				}
			}
		}
		linear(att, n, d, b.wo, b.bo, d, h)
		for i := range x {
			x[i] += h[i]
		}
		for t := 0; t < n; t++ {
			row := x[t*d : (t+1)*d]
			hn := h[t*d : (t+1)*d]
			layerNorm(row, hn, b.ln2w, b.ln2b, eps)
			for o := 0; o < ffn; o++ {
				s := float64(b.b1[o])
				for c := 0; c < d; c++ {
					s += float64(hn[c]) * float64(b.w1[c*ffn+o])
				}
				hid[o] = float32(0.5 * s * (1 + math.Erf(s/math.Sqrt2)))
			}
			for o := 0; o < d; o++ {
				s := float64(b.b2[o])
				for c := 0; c < ffn; c++ {
					s += float64(hid[c]) * float64(b.w2[c*d+o])
				}
				row[o] += float32(s)
			}
		}
	}
	u := make([]float32, d)
	layerNorm(x[(n-1)*d:n*d], u, m.lnw, m.lnb, eps)
	return u
}

func layerNorm(x, out, w, b []float32, eps float64) {
	var mean float64
	for _, v := range x {
		mean += float64(v)
	}
	mean /= float64(len(x))
	var vr float64
	for _, v := range x {
		dd := float64(v) - mean
		vr += dd * dd
	}
	vr /= float64(len(x))
	inv := 1 / math.Sqrt(vr+eps)
	for i, v := range x {
		out[i] = float32((float64(v)-mean)*inv)*w[i] + b[i]
	}
}

// linear y[n, out] = x[n, in] · W[in, out] + b。
func linear(x []float32, n, in int, w, b []float32, out int, y []float32) {
	for t := 0; t < n; t++ {
		xr := x[t*in : (t+1)*in]
		yr := y[t*out : (t+1)*out]
		copy(yr, b)
		for c := 0; c < in; c++ {
			xc := xr[c]
			if xc == 0 {
				continue
			}
			wr := w[c*out : (c+1)*out]
			for o := range yr {
				yr[o] += xc * wr[o]
			}
		}
	}
}

// TopK u 对全部物品打分，取前 k（exclude 里的跳过）。
func (m *DeepModel) TopK(u []float32, k int, exclude func(int32) bool) []ScoredItem {
	out := make([]ScoredItem, 0, k+1)
	for i := 0; i < m.N(); i++ {
		ii := int32(i)
		if exclude != nil && exclude(ii) {
			continue
		}
		s := Dot(u, m.ItemVec(ii))
		if len(out) < k {
			out = append(out, ScoredItem{Item: ii, Score: s})
			if len(out) == k {
				sort.Slice(out, func(a, b int) bool { return out[a].Score > out[b].Score })
			}
			continue
		}
		if s > out[k-1].Score {
			pos := sort.Search(k, func(j int) bool { return out[j].Score < s })
			copy(out[pos+1:], out[pos:k-1])
			out[pos] = ScoredItem{Item: ii, Score: s}
		}
	}
	if len(out) < k {
		sort.Slice(out, func(a, b int) bool { return out[a].Score > out[b].Score })
	}
	return out
}

// RankerEnabled 导出的精排是否可用。
func (m *DeepModel) RankerEnabled() bool {
	return m.Ranker != nil && !strings.Contains(string(m.Meta.Ranker), `"enabled": false`) && !strings.Contains(string(m.Meta.Ranker), `"enabled":false`)
}

// RankScores LightGBM 原始分（每行一个候选的 20 个特征）。
func (m *DeepModel) RankScores(rows [][]float32) []float64 {
	out := make([]float64, len(rows))
	f := make([]float64, NumRankFeatures)
	for i, r := range rows {
		for k := range f {
			f[k] = float64(r[k])
		}
		f[FeatPosition] = 0
		out[i] = m.Ranker.PredictSingle(f, 0)
	}
	return out
}
