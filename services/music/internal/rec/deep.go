package rec

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/store"
)

// LoadedDeep 内存里一个应用已发布的深度模型（music-deep 训练、Go 原生推理）。
type LoadedDeep struct {
	M      *model.DeepModel
	RunID  int64
	Tracks []contract.Track // 与 M.Meta.Items 对齐

	mapMu  sync.Mutex
	mapRun int64
	toSnap []int32
}

// snapIndex 深度模型物品下标 → ALS 快照作品下标（快照里没有为 -1），按快照版本缓存。
func (d *LoadedDeep) snapIndex(snap *model.Snapshot, run int64) []int32 {
	d.mapMu.Lock()
	defer d.mapMu.Unlock()
	if d.toSnap != nil && d.mapRun == run {
		return d.toSnap
	}
	out := make([]int32, d.M.N())
	for i := range out {
		if j, ok := snap.Item(d.M.ItemKey(int32(i))); ok {
			out[i] = j
		} else {
			out[i] = -1
		}
	}
	d.mapRun, d.toSnap = run, out
	return out
}

// DeepRegistry 每个应用当前发布的深度模型。
type DeepRegistry struct {
	pg  *store.PG
	log *zap.Logger
	mu  sync.RWMutex
	m   map[int64]*LoadedDeep
	// failed 加载失败过的版本（不反复重试）
	failed sync.Map
}

// NewDeepRegistry 构造。
func NewDeepRegistry(pg *store.PG, log *zap.Logger) *DeepRegistry {
	return &DeepRegistry{pg: pg, log: log, m: map[int64]*LoadedDeep{}}
}

// Get 当前深度模型（没有为 nil）。
func (r *DeepRegistry) Get(appID int64) *LoadedDeep {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[appID]
}

// Sync 比对发布版本，变了的重新加载。
func (r *DeepRegistry) Sync(ctx context.Context) {
	if r == nil {
		return
	}
	published, err := r.pg.PublishedDeepRuns(ctx)
	if err != nil {
		r.log.Warn("list published deep models failed", zap.Error(err))
		return
	}
	// 撤销了发布（published_at 清空）的应用：内存里的模型一起卸掉
	r.mu.Lock()
	for app := range r.m {
		if _, ok := published[app]; !ok {
			delete(r.m, app)
			r.log.Info("deep model unloaded (no published run)", zap.Int64("app", app))
		}
	}
	r.mu.Unlock()
	for app, run := range published {
		if cur := r.Get(app); cur != nil && cur.RunID == run {
			continue
		}
		if _, bad := r.failed.Load(run); bad {
			continue
		}
		r.Load(ctx, app, run)
	}
}

// Load 加载某个版本（失败保留旧模型，并记住这个版本不再重试）。
func (r *DeepRegistry) Load(ctx context.Context, appID, runID int64) {
	begin := time.Now()
	blobs, err := r.pg.DeepBlobs(ctx, runID)
	if err != nil {
		r.log.Warn("load deep blobs failed", zap.Int64("run", runID), zap.Error(err))
		return
	}
	m, err := model.LoadDeep(blobs)
	if err != nil {
		r.failed.Store(runID, true)
		r.log.Error("deep model rejected", zap.Int64("app", appID), zap.Int64("run", runID), zap.Error(err))
		return
	}
	tracks, err := r.pg.Tracks(ctx, appID, m.Meta.Items)
	if err != nil {
		r.log.Warn("load deep tracks failed", zap.Error(err))
		return
	}
	l := &LoadedDeep{M: m, RunID: runID, Tracks: make([]contract.Track, m.N())}
	for i, k := range m.Meta.Items {
		l.Tracks[i] = tracks[k]
	}
	r.mu.Lock()
	r.m[appID] = l
	r.mu.Unlock()
	r.log.Info("deep model loaded", zap.Int64("app", appID), zap.Int64("run", runID), zap.Int("items", m.N()),
		zap.Bool("ranker", m.RankerEnabled()), zap.Float64("parityMaxError", m.ParityMaxError), zap.Duration("took", time.Since(begin)))
}

// Subscribe 订阅发布通知。
func (r *DeepRegistry) Subscribe(nc *nats.Conn) (*nats.Subscription, error) {
	return nc.Subscribe(contract.SubjectDeepPublished, func(msg *nats.Msg) {
		var p struct {
			AppID int64 `json:"app_id"`
			RunID int64 `json:"run_id"`
		}
		if json.Unmarshal(msg.Data, &p) == nil && p.AppID > 0 && p.RunID > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			r.Load(ctx, p.AppID, p.RunID)
		}
	})
}

// ── 内容（标签、语义向量、歌手知识）缓存：每个应用 10 分钟刷新一次 ──

type appContent struct {
	items   map[string]*store.ItemContent
	artists map[string]store.ArtistKnowledge
	at      time.Time
}

type contentCache struct {
	mu sync.Mutex
	m  map[int64]*appContent
}

func (s *Service) content(ctx context.Context, appID int64) *appContent {
	s.contentOnce.Do(func() { s.contentC = &contentCache{m: map[int64]*appContent{}} })
	c := s.contentC
	c.mu.Lock()
	cur := c.m[appID]
	c.mu.Unlock()
	if cur != nil && time.Since(cur.at) < 10*time.Minute {
		return cur
	}
	items, err := s.PG.AllContent(ctx, appID)
	if err != nil {
		s.Log.Warn("load content failed", zap.Error(err))
		if cur != nil {
			return cur
		}
		items = map[string]*store.ItemContent{}
	}
	artists, err := s.PG.AllArtists(ctx, appID)
	if err != nil {
		artists = map[string]store.ArtistKnowledge{}
	}
	next := &appContent{items: items, artists: artists, at: time.Now()}
	c.mu.Lock()
	c.m[appID] = next
	c.mu.Unlock()
	return next
}
