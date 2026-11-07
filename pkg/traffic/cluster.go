package traffic

import (
	"context"
	"sync/atomic"
	"time"
)

// Coordinator 集群协同：跨实例的配额租约与存活实例数。
// 实现见 pkg/traffic/redisco（Redis Lua 原子扣减 + 心跳）。
type Coordinator interface {
	// Acquire 从「key 在第 window 个周期」的集群额度里申请 want 个名额，
	// 返回实际得到的数量（0 ≤ granted ≤ want）。
	Acquire(ctx context.Context, key string, window int64, period time.Duration, limit, want int64) (int64, error)
	// Instances 当前存活实例数（≥ 1）。
	Instances() int
}

// leaseBucket 集群共享的全局限速：本实例手里持有一小批从 Redis 租来的名额，
// 热路径只做本地原子扣减；余量低于水位时异步续租。一次 Redis 往返换来
// 「租约大小」个请求的判定，热路径上没有网络 I/O。
//
// 语义是固定窗口（每个 period 一个集群总额），这是跨实例共享计数的通常做法。
// Redis 不可用时自动降级为「总额 ÷ 实例数」的本地 GCRA，恢复后切回。
type leaseBucket struct {
	key    string
	co     Coordinator
	params atomic.Pointer[leaseParams]

	tokens    atomic.Int64
	window    atomic.Int64
	refilling atomic.Bool
	degraded  atomic.Bool
	failures  atomic.Int64
	fallback  *gcraLimiter
	optimist  atomic.Int64 // 本窗口先行发放、尚未向 Redis 记账的名额
}

type leaseParams struct {
	rate   int64
	period time.Duration
}

func newLeaseBucket(key string, co Coordinator, spec LimitSpec) *leaseBucket {
	lb := &leaseBucket{key: key, co: co}
	lb.configure(spec)
	return lb
}

func (lb *leaseBucket) configure(spec LimitSpec) {
	period := time.Duration(spec.PeriodMs) * time.Millisecond
	lb.params.Store(&leaseParams{rate: int64(spec.Rate), period: period})
	n := 1
	if lb.co != nil {
		n = maxInt(1, lb.co.Instances())
	}
	share := float64(spec.Rate) / float64(n)
	burst := maxInt(1, spec.Burst/n)
	if lb.fallback == nil {
		lb.fallback = newGCRALimiter(share, period, burst)
	} else {
		lb.fallback.set(share, period, burst)
	}
}

func (lb *leaseBucket) leaseSize() int64 {
	n := int64(1)
	if lb.co != nil {
		n = int64(maxInt(1, lb.co.Instances()))
	}
	size := lb.params.Load().rate / n / 8
	if size < 1 {
		size = 1
	}
	return size
}

func (lb *leaseBucket) currentWindow() int64 {
	return time.Now().UnixNano() / int64(lb.params.Load().period)
}

// allow 热路径。
func (lb *leaseBucket) allow(now int64) bool {
	if lb.degraded.Load() || lb.co == nil {
		return lb.fallback.allow(now)
	}
	w := lb.currentWindow()
	if cur := lb.window.Load(); cur != w && lb.window.CompareAndSwap(cur, w) {
		// 新窗口：先行发放一份本地份额（约总额 ÷ 实例数 ÷ 4），
		// 续租时一并向 Redis 记账；Redis 判定超额时从余量里扣回。
		opt := lb.leaseSize() * 2
		lb.optimist.Store(opt)
		lb.tokens.Store(opt)
		lb.refill(w)
	}
	if v := lb.tokens.Add(-1); v >= 0 {
		if v < lb.leaseSize()/2 {
			lb.refill(w)
		}
		return true
	}
	lb.tokens.Add(1)
	lb.refill(w)
	return false
}

func (lb *leaseBucket) refill(window int64) {
	if !lb.refilling.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer lb.refilling.Store(false)
		want := lb.leaseSize()
		opt := lb.optimist.Swap(0)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		p := lb.params.Load()
		granted, err := lb.co.Acquire(ctx, lb.key, window, p.period, p.rate, want+opt)
		cancel()
		if err != nil {
			if lb.failures.Add(1) >= 3 {
				lb.degraded.Store(true)
				go lb.probe()
			}
			lb.optimist.Add(opt)
			return
		}
		lb.failures.Store(0)
		if lb.window.Load() != window {
			return // 窗口已经翻过去了，这批租约作废
		}
		lb.tokens.Add(granted - opt)
	}()
}

// probe 降级后每秒探测一次 Redis，恢复即切回租约模式。
func (lb *leaseBucket) probe() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, err := lb.co.Acquire(ctx, lb.key+":probe", 0, time.Second, 1<<62, 0)
		cancel()
		if err == nil {
			lb.failures.Store(0)
			lb.window.Store(0)
			lb.degraded.Store(false)
			return
		}
	}
}

func (lb *leaseBucket) mode() string {
	if lb.co == nil || lb.degraded.Load() {
		return "fallback"
	}
	return "lease"
}
