package service

import (
	"context"
	"math"
	"strconv"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// sharedRateWindow 跨实例共享的固定窗口限速：所有副本对同一个上游的调用合计不超过 limit 次 / period。
//
// 用在「上游按调用方整体限额」的地方（UAPIs 的 QPS 是按密钥算的，不管我们开了几个副本）。
// 进程内的令牌桶只能管住本进程，N 个副本合起来就是 N 倍 —— 超出的那部分被上游拒掉，还可能触发封禁。
//
// 实现取最朴素的固定窗口：键带窗口序号，INCR + PEXPIRE 一次往返。
// 不用滑动窗口或 Lua 令牌桶：这里只要求「合计不超额」，窗口边界上偶尔挤进两倍突发，上游的限额本身也是按秒统计的。
// Redis 不可用时放行（只剩进程内令牌桶在管）：限速是保护措施，不能让它的故障变成归属地整体不可用。
type sharedRateWindow struct {
	redis  *redislib.Client
	key    string
	limit  int64
	period time.Duration
	now    func() time.Time
}

// newSharedRateWindow 按每秒 qps 次建一个共享窗口。qps ≥ 1 时窗口 1 秒、上限取整；
// qps < 1（例如 0.5）时窗口拉长到 1/qps 秒、上限 1 次。redis 为 nil 或 qps ≤ 0 时返回 nil（不限）。
func newSharedRateWindow(redis *redislib.Client, key string, qps float64) *sharedRateWindow {
	if redis == nil || qps <= 0 {
		return nil
	}
	w := &sharedRateWindow{redis: redis, key: key, limit: 1, period: time.Second, now: time.Now}
	if qps >= 1 {
		w.limit = int64(math.Floor(qps))
	} else {
		w.period = time.Duration(float64(time.Second) / qps)
	}
	return w
}

// Wait 等到本次调用在全集群的额度内。ctx 结束时返回 ctx 的错误。
func (w *sharedRateWindow) Wait(ctx context.Context) error {
	if w == nil {
		return nil
	}
	for {
		allowed, retryAfter, err := sharedWindowTake(ctx, w.redis, w.key, w.limit, w.period, w.now())
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		}
		if allowed {
			return nil
		}
		timer := time.NewTimer(retryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// sharedWindowTake 在 key 的当前窗口里占一个名额。返回是否占到；没占到时给出到下一个窗口还要等多久。
// err 非空表示 Redis 不可用，调用方自行决定退回本地判定还是放行。
func sharedWindowTake(ctx context.Context, redis *redislib.Client, key string, limit int64, period time.Duration, now time.Time) (bool, time.Duration, error) {
	periodMs := period.Milliseconds()
	if periodMs <= 0 {
		periodMs = 1
	}
	nowMs := now.UnixMilli()
	window := nowMs / periodMs
	windowKey := key + ":" + strconv.FormatInt(window, 10)
	pipe := redis.Pipeline()
	incr := pipe.Incr(ctx, windowKey)
	// 过期留两个窗口：本窗口结束前键不能先没了，否则计数被清零、额度翻倍
	pipe.PExpire(ctx, windowKey, 2*period)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}
	if incr.Val() <= limit {
		return true, 0, nil
	}
	wait := time.Duration((window+1)*periodMs-nowMs) * time.Millisecond
	if wait <= 0 {
		wait = 10 * time.Millisecond
	}
	return false, wait, nil
}
