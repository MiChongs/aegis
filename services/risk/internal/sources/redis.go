// Package sources 决策服务的外部数据源：名单、滑窗计数、流式特征（Redis Cluster）、图（NebulaGraph）、向量（Milvus）。
package sources

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"aegis/services/risk/internal/engine"

	"github.com/redis/go-redis/v9"
)

// bucketWidth 桶宽：窗口 / 60，至少 1 秒
func bucketWidth(window int) int64 {
	w := int64(window / 60)
	if w < 1 {
		w = 1
	}
	return w
}

// bucketKeys 窗口内全部桶的键（第一个是当前桶）
func bucketKeys(prefix string, window int, now time.Time) []string {
	width := bucketWidth(window)
	cur := now.Unix() / width
	n := (int64(window) + width - 1) / width
	keys := make([]string, 0, n)
	for i := int64(0); i < n; i++ {
		keys = append(keys, prefix+strconv.FormatInt(cur-i, 10))
	}
	return keys
}

// Redis 滑窗计数与流式特征
type Redis struct {
	rdb redis.UniversalClient
	now func() time.Time

	cutoverOnce sync.Once
	cutover     int64
}

// NewRedis 新建
func NewRedis(rdb redis.UniversalClient) *Redis { return &Redis{rdb: rdb, now: time.Now} }

// Velocity 一个管道里跑完全部滑窗特征，并登记反向索引（存法见 counters.go）。本次请求算在内。
//
// 次数：窗口切成最多 60 个桶，每桶一个键。去重：一个精确集合，窗口按秒滑动。
// 计数与索引不在一个槽里，做不到一起成功或失败：索引写丢的那一次贡献回退不掉，会随窗口自然过期。
func (r *Redis) Velocity(ctx context.Context, qs []engine.VelocityQuery) (map[string]float64, error) {
	now := r.now()
	since := r.legacySince(ctx)
	cmds := make([]*redis.Cmd, len(qs))
	index := map[string][]any{}
	note := func(tag string, role, kind byte, q engine.VelocityQuery, expire int64) {
		if tag = clean(tag); tag != "" {
			index[tag] = append(index[tag], expire, indexEntry(role, kind, q.WindowSeconds, q.Key))
		}
	}
	_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, q := range qs {
			width := bucketWidth(q.WindowSeconds)
			ttl := int64(q.WindowSeconds) + width + 5
			expire := now.Unix() + ttl
			tuple := actorTuple(q.Actors)
			kind := byte(kindCount)
			if q.Distinct != "" {
				kind = kindSet
				set := distinctPrefix + q.Key + "}"
				keys := []string{set, set + ":w"}
				if now.Unix() < since+ttl {
					keys = append(keys, bucketKeys(legacyHLLPrefix+q.Key+"}:", q.WindowSeconds, now)...)
				}
				cmds[i] = distinctAdd.Eval(ctx, p, keys, now.Unix(), now.Unix()-int64(q.WindowSeconds), ttl, clean(q.Distinct), tuple)
				note(q.Member, roleMember, kind, q, expire)
			} else {
				cmds[i] = countAdd.Eval(ctx, p, bucketKeys(countPrefix+q.Key+"}:", q.WindowSeconds, now), ttl, tuple)
			}
			note(q.Subject, roleSubject, kind, q, expire)
			for _, a := range q.Actors {
				note(a, roleActor, kind, q, expire)
			}
		}
		for tag, pairs := range index {
			indexAdd.Eval(ctx, p, []string{indexPrefix + tag + "}"}, append([]any{now.Unix()}, pairs...)...)
		}
		return nil
	})
	out := make(map[string]float64, len(qs))
	for i, c := range cmds {
		if c == nil {
			continue
		}
		if v, e := c.Int64(); e == nil {
			out[qs[i].Feature] = float64(v)
		}
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		return out, err
	}
	return out, nil
}

// StreamPrefix RisingWave 写回流式特征用的键前缀
const StreamPrefix = "rf:"

// Stream 读 RisingWave 写回的流式特征（键不存在 = 窗口里没有事件 = 0，由特征默认值兜底）
func (r *Redis) Stream(ctx context.Context, qs []engine.LookupQuery) (map[string]float64, error) {
	cmds := make([]*redis.StringCmd, len(qs))
	_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, q := range qs {
			cmds[i] = p.Get(ctx, StreamPrefix+q.Key)
		}
		return nil
	})
	out := make(map[string]float64, len(qs))
	for i, c := range cmds {
		raw, e := c.Result()
		if e != nil {
			continue
		}
		if v, e := strconv.ParseFloat(raw, 64); e == nil {
			out[qs[i].Feature] = v
		}
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		return out, err
	}
	return out, nil
}

// cacheGet / cacheSet 图与向量特征的短期缓存
func (r *Redis) cacheGet(ctx context.Context, keys []string) []*redis.StringCmd {
	cmds := make([]*redis.StringCmd, len(keys))
	_, _ = r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, k := range keys {
			cmds[i] = p.Get(ctx, k)
		}
		return nil
	})
	return cmds
}

func (r *Redis) cacheSet(ctx context.Context, values map[string]float64, ttl time.Duration) {
	if len(values) == 0 {
		return
	}
	_, _ = r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for k, v := range values {
			p.Set(ctx, k, strconv.FormatFloat(v, 'f', -1, 64), ttl)
		}
		return nil
	})
}
