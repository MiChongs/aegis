// Package model 音乐个性化模型：训练产物（快照）与训练算法（隐式 ALS、ItemCF、热度、离线评估）。
// 训练在 music-trainer 里跑，快照 gob + zstd 存 PostgreSQL，music-rec 加载到内存里在线打分。
package model

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"math"

	"github.com/klauspost/compress/zstd"
)

// FormatVersion 快照格式版本：结构变了就加一，旧快照加载失败时 music-rec 继续用热度兜底并提示重新训练。
const FormatVersion = 2

// Neighbor ItemCF 邻居。
type Neighbor struct {
	Item  int32
	Score float32
}

// ScoredItem 作品与分。
type ScoredItem struct {
	Item  int32
	Score float32
}

// ArtistWeight 用户的歌手偏好。
type ArtistWeight struct {
	Artist string // 规范化名
	Name   string // 展示名
	Weight float32
}

// Snapshot 一次训练的全部产物。下标即作品编号 / 用户编号。
type Snapshot struct {
	Format    int
	AppID     int64
	RunID     int64
	CreatedAt int64
	Factors   int

	Items       []string  // 作品键
	ItemArtist  []string  // 规范化第一歌手
	ItemArtistN []string  // 第一歌手展示名
	ItemTitle   []string  // 歌名（原因文案用）
	ItemFactors []float32 // len = items × factors
	ItemPop     []float32 // 全窗口热度（听众数对数）归一到 [0,1]

	Users       []int64
	UserFactors []float32 // len = users × factors
	UserArtists [][]ArtistWeight
	UserCounts  []int32 // 交互数

	Neighbors [][]Neighbor       // ItemCF 前 K
	Trending  []ScoredItem       // 近 7 天热度，降序
	Rising    []ScoredItem       // 近 1 天飙升，降序
	ArtistTop map[string][]int32 // 规范化歌手 → 热门作品

	// 第二期（格式 2）
	ItemLang     []uint8                     // 语种下标（contract.Langs）
	ItemComplete []float32                   // 近 30 天完播率（平滑）
	ItemSkip     []float32                   // 近 30 天早跳率（平滑）
	Gram         []float32                   // YᵀY（f × f），实时折入用
	Lambda       float32                     // ALS 正则
	Alpha        float32                     // ALS 置信度系数
	ArtistSim    map[string][]ArtistNeighbor // 规范化歌手 → 相似歌手
	ArtistName   map[string]string           // 规范化歌手 → 展示名
	Ranker       *Ranker                     // 精排（没启用为 nil）

	itemIndex map[string]int32
	userIndex map[int64]int32
	itemNorm  []float32
}

// ArtistNeighbor 相似歌手。
type ArtistNeighbor struct {
	Artist string // 规范化名
	Score  float32
}

// Index 建索引（加载后、训练产出后各调一次）。
func (s *Snapshot) Index() {
	s.itemIndex = make(map[string]int32, len(s.Items))
	for i, k := range s.Items {
		s.itemIndex[k] = int32(i)
	}
	s.userIndex = make(map[int64]int32, len(s.Users))
	for i, u := range s.Users {
		s.userIndex[u] = int32(i)
	}
	f := s.Factors
	s.itemNorm = make([]float32, len(s.Items))
	for i := range s.Items {
		var n float64
		for _, v := range s.ItemFactors[i*f : (i+1)*f] {
			n += float64(v) * float64(v)
		}
		s.itemNorm[i] = float32(math.Sqrt(n))
	}
}

// Item 作品键 → 编号。
func (s *Snapshot) Item(key string) (int32, bool) { i, ok := s.itemIndex[key]; return i, ok }

// User 用户 → 编号。
func (s *Snapshot) User(id int64) (int32, bool) { i, ok := s.userIndex[id]; return i, ok }

// ItemVec 作品向量。
func (s *Snapshot) ItemVec(i int32) []float32 {
	f := s.Factors
	return s.ItemFactors[int(i)*f : (int(i)+1)*f]
}

// UserVec 用户向量。
func (s *Snapshot) UserVec(u int32) []float32 {
	f := s.Factors
	return s.UserFactors[int(u)*f : (int(u)+1)*f]
}

// ItemNorm 作品向量长度（余弦用）。
func (s *Snapshot) ItemNorm(i int32) float32 { return s.itemNorm[i] }

// ForgetOnLoad 从快照里抹掉一个人的向量（只在加载时、发布给读者之前调用）。
func (s *Snapshot) ForgetOnLoad(userID int64) {
	u, ok := s.userIndex[userID]
	if !ok {
		return
	}
	delete(s.userIndex, userID)
	vec := s.UserVec(u)
	for i := range vec {
		vec[i] = 0
	}
	s.UserArtists[u] = nil
}

// Encode gob + zstd。
func (s *Snapshot) Encode() ([]byte, error) {
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return nil, err
	}
	if err := gob.NewEncoder(zw).Encode(s); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Decode 解出快照并建索引。
func Decode(data []byte) (*Snapshot, error) {
	zr, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var s Snapshot
	if err := gob.NewDecoder(io.Reader(zr)).Decode(&s); err != nil {
		return nil, err
	}
	if s.Format != FormatVersion {
		return nil, fmt.Errorf("模型格式 %d 与当前服务（%d）不一致，请重新训练", s.Format, FormatVersion)
	}
	if len(s.ItemFactors) != len(s.Items)*s.Factors || len(s.UserFactors) != len(s.Users)*s.Factors {
		return nil, fmt.Errorf("模型数据不完整")
	}
	s.Index()
	return &s, nil
}

// Dot 点积。
func Dot(a, b []float32) float32 {
	var sum float32
	n := len(a)
	i := 0
	for ; i+4 <= n; i += 4 {
		sum += a[i]*b[i] + a[i+1]*b[i+1] + a[i+2]*b[i+2] + a[i+3]*b[i+3]
	}
	for ; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}
