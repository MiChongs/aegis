package sources

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/vector"
)

// Live 决策服务的真实数据源。图与向量可以为 nil（没部署时这两类特征按默认值）。
type Live struct {
	Lists  *lists.Index
	Redis  *Redis
	Nebula *graph.Client
	Milvus *vector.Client
}

var _ engine.Sources = (*Live)(nil)

// CheckLists 名单
func (l *Live) CheckLists(ctx context.Context, q engine.ListQuery) ([]engine.ListHit, error) {
	return l.Lists.Check(ctx, q)
}

// Velocity 滑窗计数
func (l *Live) Velocity(ctx context.Context, q []engine.VelocityQuery) (map[string]float64, error) {
	return l.Redis.Velocity(ctx, q)
}

// Stream 流式特征
func (l *Live) Stream(ctx context.Context, q []engine.LookupQuery) (map[string]float64, error) {
	return l.Redis.Stream(ctx, q)
}

// Graph 图特征：先读缓存，没命中的并发查 Nebula（各自受 ctx 截止时间约束），结果回写缓存
func (l *Live) Graph(ctx context.Context, qs []engine.GraphQuery) (map[string]float64, error) {
	if l.Nebula == nil {
		return nil, graph.ErrNotReady
	}
	now := time.Now()
	keys := make([]string, len(qs))
	for i, q := range qs {
		keys[i] = "rg:" + q.Template + ":" + strconv.Itoa(q.WithinDays) + ":" + strconv.FormatInt(q.AppID, 10) + ":" + q.Entity + ":" + q.Value
	}
	out := map[string]float64{}
	var miss []int
	for i, c := range l.Redis.cacheGet(ctx, keys) {
		if v, err := c.Float64(); err == nil {
			out[qs[i].Feature] = v
		} else {
			miss = append(miss, i)
		}
	}
	if len(miss) == 0 {
		return out, nil
	}
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
		fresh    = map[string]float64{}
		ttl      = time.Minute
	)
	for _, i := range miss {
		q := qs[i]
		if q.CacheTTL > 0 {
			ttl = time.Duration(q.CacheTTL) * time.Second
		}
		vid := graph.SubjectVID(q.Entity, q.AppID, q.Value)
		stmt, err := graph.FeatureQuery(q.Template, vid, q.AppID, q.WithinDays, now)
		if err != nil {
			firstErr = err
			continue
		}
		wg.Add(1)
		go func(i int, stmt string) {
			defer wg.Done()
			done := make(chan struct{})
			var n float64
			var err error
			go func() { n, err = l.Nebula.Count(stmt); close(done) }()
			select {
			case <-done:
			case <-ctx.Done():
				err = ctx.Err()
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				return
			}
			out[qs[i].Feature] = n
			fresh[keys[i]] = n
		}(i, stmt)
	}
	wg.Wait()
	mu.Lock()
	l.Redis.cacheSet(context.WithoutCancel(ctx), fresh, ttl)
	mu.Unlock()
	return out, firstErr
}

// Vector 向量特征：一次检索取最大的 TopK，各特征按自己的阈值数
func (l *Live) Vector(ctx context.Context, qs []engine.VectorQuery, dev engine.Device) (map[string]float64, error) {
	if l.Milvus == nil {
		return nil, errors.New("向量检索未就绪")
	}
	if len(qs) == 0 {
		return nil, nil
	}
	topK := 0
	for _, q := range qs {
		if q.TopK > topK {
			topK = q.TopK
		}
	}
	vec := vector.Embed(dev.Fingerprint())
	if vector.Empty(vec) {
		return nil, nil
	}
	type result struct {
		ns  []vector.Neighbor
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ns, err := l.Milvus.Similar(ctx, qs[0].AppID, qs[0].Self, vec, topK)
		ch <- result{ns, err}
	}()
	var r result
	select {
	case r = <-ch:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if r.err != nil {
		return nil, r.err
	}
	out := map[string]float64{}
	for _, q := range qs {
		var count, banned, best float64
		for i, n := range r.ns {
			if i >= q.TopK {
				break
			}
			if float64(n.Score) > best {
				best = float64(n.Score)
			}
			if float64(n.Score) >= q.MinScore {
				count++
				if n.Banned {
					banned++
				}
			}
		}
		switch q.Output {
		case "similar_banned":
			out[q.Feature] = banned
		case "max_score":
			out[q.Feature] = best
		default:
			out[q.Feature] = count
		}
	}
	return out, nil
}
