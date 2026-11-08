// Package sources 决策服务的外部数据源：名单、滑窗计数、流式特征（Redis Cluster）、图（NebulaGraph）、向量（Milvus）。
package sources

import (
	"context"
	"errors"
	"strconv"
	"time"

	"aegis/services/risk/internal/engine"

	"github.com/redis/go-redis/v9"
)

// 滑窗计数：窗口切成最多 60 个桶，每桶一个键，键名用 {主体} 哈希标签落在同一个槽，
// 一条脚本里「本桶加一 + 读全部桶」。本次请求算在内。
//
// KEYS[1] 是当前桶，KEYS[2..] 是窗口内的其余桶。ARGV[1] 是桶的存活秒数。
var velocityCount = redis.NewScript(`
redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[1])
local vals = redis.call('MGET', unpack(KEYS))
local sum = 0
for _, v in ipairs(vals) do
  if v then sum = sum + tonumber(v) end
end
return sum
`)

// 去重计数：每桶一个 HyperLogLog，PFCOUNT 跨桶合并。ARGV[2] 是要记的成员。
var velocityDistinct = redis.NewScript(`
redis.call('PFADD', KEYS[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[1])
return redis.call('PFCOUNT', unpack(KEYS))
`)

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
}

// NewRedis 新建
func NewRedis(rdb redis.UniversalClient) *Redis { return &Redis{rdb: rdb, now: time.Now} }

// Velocity 一个管道里跑完全部滑窗特征
func (r *Redis) Velocity(ctx context.Context, qs []engine.VelocityQuery) (map[string]float64, error) {
	now := r.now()
	cmds := make([]*redis.Cmd, len(qs))
	_, err := r.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, q := range qs {
			prefix := "rv:{" + q.Key + "}:"
			if q.Distinct != "" {
				prefix = "rvd:{" + q.Key + "}:"
			}
			keys := bucketKeys(prefix, q.WindowSeconds, now)
			ttl := int64(q.WindowSeconds) + bucketWidth(q.WindowSeconds) + 5
			if q.Distinct != "" {
				cmds[i] = velocityDistinct.Eval(ctx, p, keys, ttl, q.Distinct)
			} else {
				cmds[i] = velocityCount.Eval(ctx, p, keys, ttl)
			}
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
