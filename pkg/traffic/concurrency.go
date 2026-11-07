package traffic

import (
	"container/list"
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// concLimiter 并发上限 + 等待队列 + 可选的自适应上限。
//
// 快路径（有空位）只有一次 CAS；满了才进锁排队。释放时先归还名额，
// 再看有没有等待者 —— 等待者入队与「检查是否有空位」都在锁内完成，
// 两边各自的原子操作保证不会出现「有空位却没人被唤醒」的丢失唤醒。
type concLimiter struct {
	limit    atomic.Int64
	inflight atomic.Int64
	waiting  atomic.Int64

	mu      sync.Mutex
	waiters list.List // *concWaiter

	// 自适应（AIMD）。样本只在 release 路径上累加，窗口由引擎采样循环结算。
	params     atomic.Pointer[concParams]
	winSumNs   atomic.Int64
	winCount   atomic.Int64
	winPeak    atomic.Int64 // 窗口内的最高在途
	baselineNs atomic.Int64 // 长期最小 RTT（缓慢上漂，适应负载变化）
	shrinks    atomic.Int64
}

type concParams struct {
	adaptive  bool
	minLimit  int64
	maxLimit  int64
	tolerance float64
}

type concWaiter struct {
	ch chan struct{}
}

func newConcLimiter(spec ConcurrencySpec) *concLimiter {
	c := &concLimiter{}
	c.configure(spec)
	return c
}

func (c *concLimiter) configure(spec ConcurrencySpec) {
	p := &concParams{
		adaptive:  spec.Adaptive,
		minLimit:  int64(spec.MinLimit),
		maxLimit:  int64(spec.MaxLimit),
		tolerance: float64(spec.LatencyToleranceX10) / 10,
	}
	prev := c.params.Swap(p)
	if !spec.Adaptive {
		c.limit.Store(int64(spec.Max))
	} else {
		cur := c.limit.Load()
		// 刚从固定上限切到自适应时从配置的初值起步，否则保留已学到的上限。
		if cur == 0 || prev == nil || !prev.adaptive {
			cur = int64(spec.Max)
		}
		if cur < p.minLimit {
			cur = p.minLimit
		}
		if cur > p.maxLimit {
			cur = p.maxLimit
		}
		c.limit.Store(cur)
	}
	// 上限变大时可能有等待者可以放行了。
	c.dispatch()
}

// tryAcquire 快路径。
func (c *concLimiter) tryAcquire() bool {
	for {
		cur := c.inflight.Load()
		if cur >= c.limit.Load() {
			return false
		}
		if c.inflight.CompareAndSwap(cur, cur+1) {
			c.notePeak(cur + 1)
			return true
		}
	}
}

func (c *concLimiter) notePeak(v int64) {
	for {
		p := c.winPeak.Load()
		if v <= p || c.winPeak.CompareAndSwap(p, v) {
			return
		}
	}
}

// 排队结果。
const (
	waitOK = iota
	waitFull
	waitTimeout
)

// acquireWait 慢路径：排队等空位。queueSize 是本限制器的等待上限。
func (c *concLimiter) acquireWait(ctx context.Context, queueSize int, timeout time.Duration) int {
	c.mu.Lock()
	// 锁内再试一次：释放者可能刚好在我们进锁前归还了名额。
	if c.tryAcquire() {
		c.mu.Unlock()
		return waitOK
	}
	if queueSize <= 0 || c.waiters.Len() >= queueSize {
		c.mu.Unlock()
		return waitFull
	}
	w := &concWaiter{ch: make(chan struct{}, 1)}
	elem := c.waiters.PushBack(w)
	c.waiting.Add(1)
	// 入队后再查一次空位，堵住「释放者在我们入队前读到 waiting=0」的窗口。
	c.dispatchLocked()
	c.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.ch:
		return waitOK
	case <-timer.C:
	case <-ctx.Done():
	}
	c.mu.Lock()
	select {
	case <-w.ch:
		// 超时与被唤醒同时发生：名额已经交给我们了，照常放行。
		c.mu.Unlock()
		return waitOK
	default:
	}
	c.waiters.Remove(elem)
	c.waiting.Add(-1)
	c.mu.Unlock()
	return waitTimeout
}

// release 归还名额，并记录响应时间样本。
func (c *concLimiter) release(rtt time.Duration) {
	c.inflight.Add(-1)
	if c.params.Load().adaptive {
		c.winSumNs.Add(int64(rtt))
		c.winCount.Add(1)
	}
	if c.waiting.Load() > 0 {
		c.dispatch()
	}
}

func (c *concLimiter) dispatch() {
	c.mu.Lock()
	c.dispatchLocked()
	c.mu.Unlock()
}

// dispatchLocked 把空位交给队首的等待者（名额直接转交，在途数 +1）。
func (c *concLimiter) dispatchLocked() {
	for c.waiters.Len() > 0 && c.tryAcquire() {
		front := c.waiters.Front()
		c.waiters.Remove(front)
		c.waiting.Add(-1)
		front.Value.(*concWaiter).ch <- struct{}{}
	}
}

// adapt 结算一个自适应窗口（由引擎每秒调用）。返回是否发生了收缩。
//
// 规则（类 TCP Vegas / Netflix concurrency-limits 的 AIMD）：
//   - 基线 = 观测到的最小平均 RTT，每个窗口上漂 1%，让负载结构变化后基线能跟上；
//   - 平均 RTT > 基线 × 容忍倍数：上限 × 0.9（乘性收缩，快速卸压）；
//   - 否则若窗口内在途曾触顶（说明需求在推上限）：上限 + √上限（加性试探）。
func (c *concLimiter) adapt() bool {
	p := c.params.Load()
	if !p.adaptive {
		return false
	}
	count := c.winCount.Swap(0)
	sum := c.winSumNs.Swap(0)
	peak := c.winPeak.Swap(c.inflight.Load())
	if count < 10 {
		return false
	}
	avg := sum / count
	base := c.baselineNs.Load()
	if base == 0 || avg < base {
		base = avg
	} else {
		base += base / 100
	}
	c.baselineNs.Store(base)

	limit := c.limit.Load()
	next := limit
	shrunk := false
	if float64(avg) > float64(base)*p.tolerance {
		next = int64(math.Floor(float64(limit) * 0.9))
		shrunk = next < limit
	} else if peak >= limit {
		next = limit + int64(math.Max(1, math.Sqrt(float64(limit))))
	}
	if next < p.minLimit {
		next = p.minLimit
	}
	if next > p.maxLimit {
		next = p.maxLimit
	}
	if next != limit {
		c.limit.Store(next)
		if next > limit {
			c.dispatch()
		}
	}
	if shrunk {
		c.shrinks.Add(1)
	}
	return shrunk
}
