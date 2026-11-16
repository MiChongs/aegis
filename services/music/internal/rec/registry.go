// Package rec music-rec 的在线部分：模型注册表、推荐、画像、遗忘、配置缓存。
package rec

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/store"
)

// Loaded 内存里一个应用已发布的模型。
type Loaded struct {
	Snap     *model.Snapshot
	RunID    int64
	Tracks   []contract.Track // 与 Snap.Items 对齐，曲库里没有的是零值
	LoadedAt time.Time

	forgotten sync.Map // userID → true：模型发布后才清除数据的人
}

// Forgotten 这个人是否在模型发布后清除过数据。
func (l *Loaded) Forgotten(userID int64) bool { _, ok := l.forgotten.Load(userID); return ok }

// Registry 每个应用当前发布的模型。加载：启动时全部、收到发布通知、兜底轮询。
type Registry struct {
	pg  *store.PG
	log *zap.Logger

	mu    sync.RWMutex
	byApp map[int64]*Loaded
	// loading 同一应用同一时刻只加载一次
	loading sync.Map
	// pinned 实验固定的模型版本（runID → 模型）
	pinned map[int64]*pinnedRun
}

// NewRegistry 构造。
func NewRegistry(pg *store.PG, log *zap.Logger) *Registry {
	return &Registry{pg: pg, log: log, byApp: map[int64]*Loaded{}}
}

// Get 应用当前模型（没有返回 nil：走热度兜底）。
func (r *Registry) Get(appID int64) *Loaded {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byApp[appID]
}

// Sync 比对数据库里的发布版本，有变化的重新加载。
func (r *Registry) Sync(ctx context.Context) {
	published, err := r.pg.PublishedRuns(ctx)
	if err != nil {
		r.log.Warn("list published models failed", zap.Error(err))
		return
	}
	for appID, runID := range published {
		if cur := r.Get(appID); cur != nil && cur.RunID == runID {
			continue
		}
		r.Load(ctx, appID, runID)
	}
}

// Load 加载某个版本作为当前发布（失败保留旧模型）。
func (r *Registry) Load(ctx context.Context, appID, runID int64) {
	if _, busy := r.loading.LoadOrStore(appID, true); busy {
		return
	}
	defer r.loading.Delete(appID)
	begin := time.Now()
	loaded, err := r.loadRun(ctx, appID, runID)
	if err != nil {
		r.log.Warn("load model failed", zap.Int64("app", appID), zap.Int64("run", runID), zap.Error(err))
		return
	}
	r.mu.Lock()
	r.byApp[appID] = loaded
	r.mu.Unlock()
	r.log.Info("model loaded", zap.Int64("app", appID), zap.Int64("run", runID), zap.Int("items", len(loaded.Snap.Items)),
		zap.Int("users", len(loaded.Snap.Users)), zap.Duration("took", time.Since(begin)))
}

// GetRun 实验固定的某个版本：与当前发布相同直接复用，否则按需加载并缓存（最多 4 个，按最近使用淘汰）。
func (r *Registry) GetRun(ctx context.Context, appID, runID int64) *Loaded {
	if cur := r.Get(appID); cur != nil && cur.RunID == runID {
		return cur
	}
	r.mu.Lock()
	if r.pinned == nil {
		r.pinned = map[int64]*pinnedRun{}
	}
	if p := r.pinned[runID]; p != nil && p.l.Snap.AppID == appID {
		p.used = time.Now()
		r.mu.Unlock()
		return p.l
	}
	r.mu.Unlock()
	loaded, err := r.loadRun(ctx, appID, runID)
	if err != nil {
		r.log.Warn("load pinned model failed", zap.Int64("app", appID), zap.Int64("run", runID), zap.Error(err))
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pinned) >= 4 {
		var oldest int64
		var at time.Time
		for id, p := range r.pinned {
			if oldest == 0 || p.used.Before(at) {
				oldest, at = id, p.used
			}
		}
		delete(r.pinned, oldest)
	}
	r.pinned[runID] = &pinnedRun{l: loaded, used: time.Now()}
	return loaded
}

type pinnedRun struct {
	l    *Loaded
	used time.Time
}

func (r *Registry) loadRun(ctx context.Context, appID, runID int64) (*Loaded, error) {
	run, err := r.pg.Run(ctx, appID, runID)
	if err != nil {
		return nil, err
	}
	blob, err := r.pg.Blob(ctx, runID)
	if err != nil {
		return nil, err
	}
	snap, err := model.Decode(blob)
	if err != nil {
		return nil, err
	}
	// 训练开始之后才清除数据的人：事件已经删了，但这版模型里还有他，加载时抹掉
	forgotten, err := r.pg.ForgottenSince(ctx, appID, time.UnixMilli(run.CreatedAt).Add(-time.Hour))
	if err != nil {
		r.log.Warn("load forget log failed", zap.Error(err))
	}
	for uid := range forgotten {
		snap.ForgetOnLoad(uid)
	}
	tracks, err := r.pg.Tracks(ctx, appID, snap.Items)
	if err != nil {
		return nil, err
	}
	loaded := &Loaded{Snap: snap, RunID: runID, LoadedAt: time.Now(), Tracks: make([]contract.Track, len(snap.Items))}
	for i, key := range snap.Items {
		loaded.Tracks[i] = tracks[key]
	}
	snap.AppID = appID
	return loaded, nil
}

// Forget 内存里的模型立即不再用这个人。
func (r *Registry) Forget(appID, userID int64) {
	if l := r.Get(appID); l != nil {
		l.forgotten.Store(userID, true)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.pinned {
		if p.l.Snap.AppID == appID {
			p.l.forgotten.Store(userID, true)
		}
	}
}

// Subscribe 订阅发布通知与遗忘广播。
func (r *Registry) Subscribe(nc *nats.Conn) ([]*nats.Subscription, error) {
	pub, err := nc.Subscribe(contract.SubjectModelPublished, func(m *nats.Msg) {
		var p contract.ModelPublished
		if json.Unmarshal(m.Data, &p) == nil && p.AppID > 0 && p.RunID > 0 {
			go r.Load(context.Background(), p.AppID, p.RunID)
		}
	})
	if err != nil {
		return nil, err
	}
	forget, err := nc.Subscribe(SubjectUserForgotten, func(m *nats.Msg) {
		var p forgetMsg
		if json.Unmarshal(m.Data, &p) == nil {
			r.Forget(p.AppID, p.UserID)
		}
	})
	if err != nil {
		_ = pub.Unsubscribe()
		return nil, err
	}
	return []*nats.Subscription{pub, forget}, nil
}

// SubjectUserForgotten 清除数据后通知所有 music-rec 实例。
const SubjectUserForgotten = "music.user.forgotten"

type forgetMsg struct {
	AppID  int64 `json:"app_id"`
	UserID int64 `json:"user_id"`
}

// Configs 应用配置缓存（30 秒，收到变更通知立即失效）。
type Configs struct {
	pg *store.PG
	mu sync.Mutex
	m  map[int64]cachedCfg
}

type cachedCfg struct {
	cfg appcfg.Config
	at  time.Time
}

// NewConfigs 构造。
func NewConfigs(pg *store.PG) *Configs { return &Configs{pg: pg, m: map[int64]cachedCfg{}} }

// Get 取配置。
func (c *Configs) Get(ctx context.Context, appID int64) (appcfg.Config, error) {
	c.mu.Lock()
	item, ok := c.m[appID]
	c.mu.Unlock()
	if ok && time.Since(item.at) < 30*time.Second {
		return item.cfg, nil
	}
	cfg, err := c.pg.Config(ctx, appID)
	if err != nil {
		if ok {
			return item.cfg, nil
		}
		return cfg, err
	}
	c.mu.Lock()
	c.m[appID] = cachedCfg{cfg: cfg, at: time.Now()}
	c.mu.Unlock()
	return cfg, nil
}

// Invalidate 失效。
func (c *Configs) Invalidate(appID int64) {
	c.mu.Lock()
	delete(c.m, appID)
	c.mu.Unlock()
}

// Subscribe 订阅配置变更。
func (c *Configs) Subscribe(nc *nats.Conn) (*nats.Subscription, error) {
	return nc.Subscribe(contract.SubjectConfigChanged, func(m *nats.Msg) {
		var p struct {
			AppID int64 `json:"app_id"`
		}
		if json.Unmarshal(m.Data, &p) == nil {
			c.Invalidate(p.AppID)
		}
	})
}
