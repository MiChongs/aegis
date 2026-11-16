package rec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"aegis/services/music/contract"
)

// Forget 清除一个人的全部听歌数据：ClickHouse 事件与曝光、Redis 实时状态与缓存、模型里的向量（全部实例）。
// 先记遗忘日志：之后加载的模型版本据此跳过这个人，即使别的步骤失败重试也不会漏。
func (s *Service) Forget(ctx context.Context, nc *nats.Conn, appID, userID int64, reason, operator string) error {
	if appID <= 0 || userID <= 0 {
		return ErrBadRequest{"缺少应用或用户"}
	}
	if err := s.PG.LogForget(ctx, appID, userID, reason, operator); err != nil {
		return fmt.Errorf("记录清除失败：%w", err)
	}
	s.Reg.Forget(appID, userID)
	if nc != nil {
		payload, _ := json.Marshal(forgetMsg{AppID: appID, UserID: userID})
		_ = nc.Publish(SubjectUserForgotten, payload)
	}
	var errs []error
	if err := s.CH.ForgetUser(ctx, appID, userID); err != nil {
		errs = append(errs, err)
	}
	if err := s.RT.Forget(ctx, appID, userID); err != nil {
		errs = append(errs, fmt.Errorf("清除实时状态失败：%w", err))
	}
	return errors.Join(errs...)
}

// RealtimeView 控制台看到的实时状态。
type RealtimeView struct {
	Recent          []TrackAt     `json:"recent"`
	Positive        []TrackWeight `json:"positive"`
	Negative        []TrackAt     `json:"negative"`
	DislikedArtists []ArtistAt    `json:"dislikedArtists"`
}

// TrackAt 作品与时刻。
type TrackAt struct {
	WorkKey string          `json:"workKey"`
	Track   *contract.Track `json:"track"`
	At      int64           `json:"at"`
}

// TrackWeight 作品与分。
type TrackWeight struct {
	WorkKey string          `json:"workKey"`
	Track   *contract.Track `json:"track"`
	Weight  float64         `json:"weight"`
}

// ArtistAt 歌手与时刻。
type ArtistAt struct {
	Name string `json:"name"`
	At   int64  `json:"at"`
}

// ModelView 模型里的这个人。
type ModelView struct {
	InModel      bool           `json:"inModel"`
	Version      int64          `json:"version"`
	Interactions int            `json:"interactions"`
	TopArtists   []ArtistWeight `json:"topArtists"`
}

// ArtistWeight 歌手偏好。
type ArtistWeight struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

// UserDetail 控制台用户画像页。
type UserDetail struct {
	UserID   int64        `json:"userId"`
	Profile  *Profile     `json:"profile"`
	Realtime RealtimeView `json:"realtime"`
	Model    ModelView    `json:"model"`
}

// UserDetail 画像 + 实时状态 + 模型里的口味。
func (s *Service) UserDetail(ctx context.Context, appID, userID int64, days int) (*UserDetail, error) {
	profile, err := s.Profile(ctx, appID, userID, days, false)
	if err != nil {
		return nil, err
	}
	st, err := s.RT.Load(ctx, appID, userID)
	if err != nil {
		return nil, fmt.Errorf("读取实时状态失败：%w", err)
	}
	var keys []string
	for _, e := range st.Recent {
		keys = append(keys, e.WorkKey)
	}
	for _, e := range st.Positive {
		keys = append(keys, e.WorkKey)
	}
	for k := range st.Negative {
		keys = append(keys, k)
	}
	tracks, err := s.PG.Tracks(ctx, appID, keys)
	if err != nil {
		return nil, err
	}
	trackOf := func(key string) *contract.Track {
		if t, ok := tracks[key]; ok {
			return &t
		}
		return nil
	}
	out := &UserDetail{UserID: userID, Profile: profile, Realtime: RealtimeView{
		Recent: []TrackAt{}, Positive: []TrackWeight{}, Negative: []TrackAt{}, DislikedArtists: []ArtistAt{}},
		Model: ModelView{TopArtists: []ArtistWeight{}}}
	for k, e := range st.Recent {
		if k >= 50 {
			break
		}
		out.Realtime.Recent = append(out.Realtime.Recent, TrackAt{WorkKey: e.WorkKey, Track: trackOf(e.WorkKey), At: e.At})
	}
	for _, e := range st.Positive {
		out.Realtime.Positive = append(out.Realtime.Positive, TrackWeight{WorkKey: e.WorkKey, Track: trackOf(e.WorkKey), Weight: round4(e.Weight)})
	}
	for key, at := range st.Negative {
		out.Realtime.Negative = append(out.Realtime.Negative, TrackAt{WorkKey: key, Track: trackOf(key), At: at})
	}
	sortTrackAt(out.Realtime.Negative)
	for name, at := range st.DislikedArtists {
		out.Realtime.DislikedArtists = append(out.Realtime.DislikedArtists, ArtistAt{Name: name, At: at})
	}
	sortArtistAt(out.Realtime.DislikedArtists)
	if l := s.Reg.Get(appID); l != nil {
		out.Model.Version = l.RunID
		if u, ok := l.Snap.User(userID); ok && !l.Forgotten(userID) {
			out.Model.InModel = true
			out.Model.Interactions = int(l.Snap.UserCounts[u])
			for _, a := range l.Snap.UserArtists[u] {
				out.Model.TopArtists = append(out.Model.TopArtists, ArtistWeight{Name: a.Name, Weight: round4(float64(a.Weight))})
			}
		}
	}
	return out, nil
}

func sortTrackAt(list []TrackAt) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].At > list[j-1].At; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func sortArtistAt(list []ArtistAt) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].At > list[j-1].At; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// Similar 作品的 ItemCF 邻居与 ALS 近邻（控制台作品详情）。
type Similar struct {
	WorkKey string          `json:"workKey"`
	Track   *contract.Track `json:"track"`
	Score   float64         `json:"score"`
	Via     string          `json:"via"`
}

// SimilarOf 相似作品。
func (s *Service) SimilarOf(appID int64, workKey string) []Similar {
	out := []Similar{}
	l := s.Reg.Get(appID)
	if l == nil {
		return out
	}
	snap := l.Snap
	i, ok := snap.Item(workKey)
	if !ok {
		return out
	}
	trackOf := func(j int32) *contract.Track {
		if t := l.Tracks[j]; t.Source != "" {
			return &t
		}
		return nil
	}
	for k, n := range snap.Neighbors[i] {
		if k >= 20 {
			break
		}
		out = append(out, Similar{WorkKey: snap.Items[n.Item], Track: trackOf(n.Item), Score: round4(float64(n.Score)), Via: "itemcf"})
	}
	if norm := snap.ItemNorm(i); norm > 0 {
		vec := snap.ItemVec(i)
		scores := make([]float32, len(snap.Items))
		for j := range snap.Items {
			if n := snap.ItemNorm(int32(j)); n > 0 && int32(j) != i {
				scores[j] = dot32(vec, snap.ItemVec(int32(j))) / (norm * n)
			}
		}
		top := topScores(scores, 20)
		for a := 1; a < len(top); a++ {
			for b := a; b > 0 && top[b].Score > top[b-1].Score; b-- {
				top[b], top[b-1] = top[b-1], top[b]
			}
		}
		for _, p := range top {
			out = append(out, Similar{WorkKey: snap.Items[p.Item], Track: trackOf(p.Item), Score: round4(float64(p.Score)), Via: "als"})
		}
	}
	return out
}

func dot32(a, b []float32) float32 {
	var s float32
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

// PipelineLag 消费积压：落地消费者待处理条数与最早一条的延迟。
type PipelineLag struct {
	LagSeconds  int64 `json:"lagSeconds"`
	Pending     int64 `json:"pending"`
	LastEventAt int64 `json:"lastEventAt"`
}

// Lag 读 JetStream 消费者状态。
func (s *Service) Lag(ctx context.Context, lastEvent int64) PipelineLag {
	out := PipelineLag{LastEventAt: lastEvent}
	if s.JS == nil {
		return out
	}
	cons, err := s.JS.Consumer(ctx, contract.StreamEvents, contract.ConsumerIngest)
	if err != nil {
		return out
	}
	info, err := cons.Info(ctx)
	if err != nil {
		return out
	}
	out.Pending = int64(info.NumPending) + int64(info.NumAckPending)
	if out.Pending > 0 && info.Delivered.Last != nil {
		out.LagSeconds = int64(time.Since(*info.Delivered.Last).Seconds())
	}
	return out
}
