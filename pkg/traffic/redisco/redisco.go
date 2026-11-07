// Package redisco 用 Redis 实现 traffic.Coordinator：
//   - 集群配额租约：Lua 原子地在「键 × 周期」计数上申请一批名额；
//   - 实例心跳：每个实例定期写自己的摘要（带 TTL）并登记到有序集合；
//   - 配置广播：发布新修订号，各实例收到后从数据库重载。
//
// Redis 只在租约续期、心跳与广播时访问，不在请求热路径上。
package redisco

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"

	"aegis/pkg/traffic"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	heartbeatEvery = 2 * time.Second
	nodeTTL        = 10 * time.Second
)

// acquireScript KEYS[1] 计数键；ARGV: limit, want, ttlMs。返回实际发放数。
var acquireScript = redis.NewScript(`
local want = tonumber(ARGV[2])
if want <= 0 then return 0 end
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
local limit = tonumber(ARGV[1])
local grant = limit - used
if grant <= 0 then return 0 end
if grant > want then grant = want end
redis.call('INCRBY', KEYS[1], grant)
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return grant
`)

// Coordinator Redis 实现。
type Coordinator struct {
	rdb      *redis.Client
	prefix   string
	instance string
	log      *zap.Logger
	live     atomic.Int64
}

// New 构造协调器。prefix 是全局 Redis 键前缀（与平台其余部分一致）。
func New(rdb *redis.Client, prefix, instance string, log *zap.Logger) *Coordinator {
	if log == nil {
		log = zap.NewNop()
	}
	c := &Coordinator{rdb: rdb, prefix: prefix + "traffic:", instance: instance, log: log}
	c.live.Store(1)
	return c
}

// Acquire 实现 traffic.Coordinator。
func (c *Coordinator) Acquire(ctx context.Context, key string, window int64, period time.Duration, limit, want int64) (int64, error) {
	if want <= 0 {
		return 0, c.rdb.Ping(ctx).Err()
	}
	k := c.prefix + key + ":" + strconv.FormatInt(window, 10)
	ttl := period.Milliseconds() * 2
	if ttl < 1000 {
		ttl = 1000
	}
	return acquireScript.Run(ctx, c.rdb, []string{k}, limit, want, ttl).Int64()
}

// Instances 实现 traffic.Coordinator：最近一次心跳看到的存活实例数。
func (c *Coordinator) Instances() int {
	n := int(c.live.Load())
	if n < 1 {
		return 1
	}
	return n
}

func (c *Coordinator) nodesKey() string         { return c.prefix + "nodes" }
func (c *Coordinator) nodeKey(id string) string { return c.prefix + "node:" + id }
func (c *Coordinator) reloadChannel() string    { return c.prefix + "reload" }

// Heartbeat 写一次本实例摘要，并刷新存活实例数。
func (c *Coordinator) Heartbeat(ctx context.Context, summary traffic.NodeSummary) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	now := time.Now()
	pipe := c.rdb.Pipeline()
	pipe.Set(ctx, c.nodeKey(c.instance), payload, nodeTTL)
	pipe.ZAdd(ctx, c.nodesKey(), redis.Z{Score: float64(now.Unix()), Member: c.instance})
	pipe.ZRemRangeByScore(ctx, c.nodesKey(), "-inf", strconv.FormatInt(now.Add(-3*nodeTTL).Unix(), 10))
	count := pipe.ZCount(ctx, c.nodesKey(), strconv.FormatInt(now.Add(-nodeTTL).Unix(), 10), "+inf")
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	if n := count.Val(); n > 0 {
		c.live.Store(n)
	}
	return nil
}

// Nodes 读全部存活实例的摘要。
func (c *Coordinator) Nodes(ctx context.Context) ([]traffic.NodeSummary, error) {
	min := strconv.FormatInt(time.Now().Add(-nodeTTL).Unix(), 10)
	ids, err := c.rdb.ZRangeByScore(ctx, c.nodesKey(), &redis.ZRangeBy{Min: min, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []traffic.NodeSummary{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.nodeKey(id)
	}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]traffic.NodeSummary, 0, len(vals))
	for _, v := range vals {
		s, ok := v.(string)
		if !ok {
			continue
		}
		var n traffic.NodeSummary
		if json.Unmarshal([]byte(s), &n) == nil {
			out = append(out, n)
		}
	}
	return out, nil
}

// Leave 实例退出时注销自己，其余实例下一次心跳即可重新均分。
func (c *Coordinator) Leave(ctx context.Context) {
	pipe := c.rdb.Pipeline()
	pipe.Del(ctx, c.nodeKey(c.instance))
	pipe.ZRem(ctx, c.nodesKey(), c.instance)
	_, _ = pipe.Exec(ctx)
}

// PublishReload 广播新修订号。
func (c *Coordinator) PublishReload(ctx context.Context, revision int64) error {
	return c.rdb.Publish(ctx, c.reloadChannel(), strconv.FormatInt(revision, 10)).Err()
}

// Run 心跳与订阅循环，直到 ctx 结束。onReload 在收到广播时调用（参数为修订号）。
func (c *Coordinator) Run(ctx context.Context, summary func() traffic.NodeSummary, onReload func(revision int64)) {
	go c.subscribe(ctx, onReload)
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	beat := func() {
		hctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := c.Heartbeat(hctx, summary()); err != nil && ctx.Err() == nil {
			c.log.Debug("traffic heartbeat failed", zap.Error(err))
		}
	}
	beat()
	for {
		select {
		case <-ctx.Done():
			lctx, cancel := context.WithTimeout(context.Background(), time.Second)
			c.Leave(lctx)
			cancel()
			return
		case <-ticker.C:
			beat()
		}
	}
}

func (c *Coordinator) subscribe(ctx context.Context, onReload func(int64)) {
	for ctx.Err() == nil {
		sub := c.rdb.Subscribe(ctx, c.reloadChannel())
		ch := sub.Channel()
	recv:
		for {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					break recv
				}
				if rev, err := strconv.ParseInt(msg.Payload, 10, 64); err == nil && onReload != nil {
					onReload(rev)
				}
			}
		}
		_ = sub.Close()
		// 连接断开：稍后重订阅（期间由定时对账兜底）。
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
