package rec

import (
	"context"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/store"
)

// opsCache 运营规则、运行中的实验、用户偏好的短时缓存（规则与实验 30 秒、偏好 1 分钟；改了立即失效）。
type opsCache struct {
	mu    sync.Mutex
	rules map[int64]cached[[]store.Rule]
	exps  map[int64]cached[[]store.Experiment]
	prefs map[[2]int64]cached[store.Prefs]
}

type cached[T any] struct {
	v  T
	at time.Time
}

func (s *Service) ops() *opsCache {
	s.opsOnce.Do(func() {
		s.opsC = &opsCache{rules: map[int64]cached[[]store.Rule]{}, exps: map[int64]cached[[]store.Experiment]{},
			prefs: map[[2]int64]cached[store.Prefs]{}}
	})
	return s.opsC
}

// activeRules 此刻生效的规则。
func (s *Service) activeRules(ctx context.Context, appID int64, now time.Time) []store.Rule {
	c := s.ops()
	c.mu.Lock()
	e, ok := c.rules[appID]
	c.mu.Unlock()
	if !ok || time.Since(e.at) > 30*time.Second {
		list, err := s.PG.Rules(ctx, appID)
		if err != nil {
			s.Log.Warn("load rules failed")
			list = e.v
		}
		e = cached[[]store.Rule]{v: list, at: time.Now()}
		c.mu.Lock()
		c.rules[appID] = e
		c.mu.Unlock()
	}
	out := make([]store.Rule, 0, len(e.v))
	for _, r := range e.v {
		if r.Active(now) {
			out = append(out, r)
		}
	}
	return out
}

// InvalidateOps 规则 / 实验改了（管理端与 NATS 广播调用）。
func (s *Service) InvalidateOps(appID int64) {
	c := s.ops()
	c.mu.Lock()
	delete(c.rules, appID)
	delete(c.exps, appID)
	c.mu.Unlock()
}

func (s *Service) runningExperiments(ctx context.Context, appID int64) []store.Experiment {
	c := s.ops()
	c.mu.Lock()
	e, ok := c.exps[appID]
	c.mu.Unlock()
	if !ok || time.Since(e.at) > 30*time.Second {
		list, err := s.PG.Experiments(ctx, appID, store.ExpRunning)
		if err != nil {
			s.Log.Warn("load experiments failed")
			list = e.v
		}
		e = cached[[]store.Experiment]{v: list, at: time.Now()}
		c.mu.Lock()
		c.exps[appID] = e
		c.mu.Unlock()
	}
	return e.v
}

// userPrefs 用户偏好（缓存 1 分钟；PUT 后立即失效）。
func (s *Service) userPrefs(ctx context.Context, appID, userID int64) store.Prefs {
	if userID <= 0 {
		return store.DefaultPrefs()
	}
	c := s.ops()
	key := [2]int64{appID, userID}
	c.mu.Lock()
	e, ok := c.prefs[key]
	c.mu.Unlock()
	if ok && time.Since(e.at) < time.Minute {
		return e.v
	}
	p, _, err := s.PG.UserPrefs(ctx, appID, userID)
	if err != nil {
		s.Log.Warn("load prefs failed")
	}
	c.mu.Lock()
	if len(c.prefs) > 50000 {
		c.prefs = map[[2]int64]cached[store.Prefs]{}
	}
	c.prefs[key] = cached[store.Prefs]{v: p, at: time.Now()}
	c.mu.Unlock()
	return p
}

func (s *Service) invalidatePrefs(appID, userID int64) {
	c := s.ops()
	c.mu.Lock()
	delete(c.prefs, [2]int64{appID, userID})
	c.mu.Unlock()
}

// Assignment 这次请求落在哪个实验的哪个变体。
type Assignment struct {
	ExperimentID int64
	Name         string
	Variant      string
	Overrides    store.Overrides
}

// Bucket 用户在某实验里的桶（0–99），同一实验同一人恒定。
func Bucket(expID, userID int64) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strconv.FormatInt(expID, 10) + ":" + strconv.FormatInt(userID, 10)))
	return int(h.Sum32() % 100)
}

// assign 找这个场景运行中的实验并分桶；不在任何变体流量里返回 nil。
func (s *Service) assign(ctx context.Context, appID, userID int64, scene string) *Assignment {
	if userID <= 0 {
		return nil
	}
	for _, e := range s.runningExperiments(ctx, appID) {
		if !contains(e.Scenes, scene) {
			continue
		}
		b := Bucket(e.ID, userID)
		acc := 0
		for _, v := range e.Variants {
			acc += v.Traffic
			if b < acc {
				return &Assignment{ExperimentID: e.ID, Name: e.Name, Variant: v.Key, Overrides: v.Overrides}
			}
		}
		return nil
	}
	return nil
}

// Assignments 这个人当前所在的全部实验（控制台用户详情）。
func (s *Service) Assignments(ctx context.Context, appID, userID int64) []Assignment {
	out := []Assignment{}
	for _, e := range s.runningExperiments(ctx, appID) {
		b := Bucket(e.ID, userID)
		acc := 0
		for _, v := range e.Variants {
			acc += v.Traffic
			if b < acc {
				out = append(out, Assignment{ExperimentID: e.ID, Name: e.Name, Variant: v.Key})
				break
			}
		}
	}
	return out
}

// applyOverrides 变体覆盖应用配置（返回副本）。
func applyOverrides(cfg appcfg.Config, o store.Overrides) appcfg.Config {
	if o.Blend != nil {
		set := func(dst *float64, v *float64) {
			if v != nil {
				*dst = *v
			}
		}
		set(&cfg.Blend.ALS, o.Blend.ALS)
		set(&cfg.Blend.ItemCF, o.Blend.ItemCF)
		set(&cfg.Blend.Artist, o.Blend.Artist)
		set(&cfg.Blend.Trending, o.Blend.Trending)
		set(&cfg.Blend.Explore, o.Blend.Explore)
	}
	if o.ExploreRatio != nil {
		cfg.ExploreRatio = *o.ExploreRatio
	}
	if o.ArtistCap != nil {
		cfg.ArtistCap = *o.ArtistCap
	}
	if o.Ranker != nil {
		cfg.Ranker.Enabled = *o.Ranker
	}
	if o.FoldIn != nil {
		cfg.FoldIn = *o.FoldIn
	}
	return cfg
}

// ValidateExperiment 实验的变体与覆盖参数校验（错误信息给控制台看）。
func ValidateExperiment(e *store.Experiment) error {
	if e.Name == "" || len([]rune(e.Name)) > 64 {
		return ErrBadRequest{"实验名称不能为空，且不超过 64 字"}
	}
	if len(e.Scenes) == 0 {
		return ErrBadRequest{"至少选择一个场景"}
	}
	for _, sc := range e.Scenes {
		if !contract.ValidScene(sc) {
			return ErrBadRequest{"未知的场景：" + sc}
		}
	}
	if len(e.Variants) < 2 || len(e.Variants) > 6 {
		return ErrBadRequest{"变体需要 2 到 6 个"}
	}
	total := 0
	keys := map[string]bool{}
	for i := range e.Variants {
		v := &e.Variants[i]
		if v.Key == "" || len(v.Key) > 8 || keys[v.Key] {
			return ErrBadRequest{"变体键不能为空、不能重复，且不超过 8 个字符"}
		}
		keys[v.Key] = true
		if v.Name == "" {
			v.Name = "变体 " + v.Key
		}
		if v.Traffic < 0 || v.Traffic > 100 {
			return ErrBadRequest{"变体流量应在 0 到 100 之间"}
		}
		total += v.Traffic
		o := v.Overrides
		chk := func(name string, p *float64, lo, hi float64) error {
			if p != nil && (*p < lo || *p > hi || *p != *p) {
				return ErrBadRequest{"变体 " + v.Key + " 的" + name + "超出范围"}
			}
			return nil
		}
		if o.Blend != nil {
			for name, p := range map[string]*float64{"融合权重 als": o.Blend.ALS, "融合权重 itemcf": o.Blend.ItemCF,
				"融合权重 artist": o.Blend.Artist, "融合权重 trending": o.Blend.Trending, "融合权重 explore": o.Blend.Explore} {
				if err := chk(name, p, 0, 5); err != nil {
					return err
				}
			}
		}
		if err := chk("探索比例", o.ExploreRatio, 0, 0.5); err != nil {
			return err
		}
		if o.ArtistCap != nil && (*o.ArtistCap < 1 || *o.ArtistCap > 10) {
			return ErrBadRequest{"变体 " + v.Key + " 的同一歌手上限应在 1 到 10 之间"}
		}
	}
	if total > 100 {
		return ErrBadRequest{"全部变体的流量之和不能超过 100"}
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
