package traffic

import (
	"hash/maphash"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// GCRA（Generic Cell Rate Algorithm）：令牌桶的等价形式，每个键只存一个
// 「理论到达时间」TAT。判定是一次 CAS，不需要后台补充令牌，也不需要锁。
//
//	emission T = period / rate       （每个请求「占用」的时间）
//	tolerance  = T × burst           （允许超前的时间，即突发容量）
//	tat' = max(tat, now) + T
//	放行条件：tat' − tolerance ≤ now
//
// 时间统一用单调时钟的纳秒偏移（mono()），不受系统时间回拨影响。

var monoBase = time.Now()

// mono 单调时钟纳秒（相对进程启动）。加 1 小时偏移，保证 0 值的 TAT 永远「已过期」。
func mono() int64 { return int64(time.Since(monoBase)) + int64(time.Hour) }

// gcraParams 一个限速器的参数，热更新时整体原子替换。
type gcraParams struct {
	emission  int64 // T，纳秒
	tolerance int64 // T × burst
	burst     int64
}

func newGCRAParams(rate float64, period time.Duration, burst int) *gcraParams {
	if rate <= 0 {
		rate = 1
	}
	emission := int64(float64(period) / rate)
	if emission < 1 {
		emission = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &gcraParams{emission: emission, tolerance: emission * int64(burst), burst: int64(burst)}
}

// allow 尝试在 tat 上记一次请求。成功返回 true。
func (p *gcraParams) allow(tat *atomic.Int64, now int64) bool {
	for {
		cur := tat.Load()
		base := cur
		if base < now {
			base = now
		}
		next := base + p.emission
		if next-p.tolerance > now {
			return false
		}
		if tat.CompareAndSwap(cur, next) {
			return true
		}
	}
}

// reserve 预约一个名额：可以立刻放行时返回 0；需要等待且等待 ≤ maxWait 时
// 占位并返回需要等待的时长；等待超过 maxWait 时不占位，返回 -1。
func (p *gcraParams) reserve(tat *atomic.Int64, now, maxWait int64) int64 {
	for {
		cur := tat.Load()
		base := cur
		if base < now {
			base = now
		}
		next := base + p.emission
		wait := next - p.tolerance - now
		if wait > maxWait {
			return -1
		}
		if tat.CompareAndSwap(cur, next) {
			if wait < 0 {
				return 0
			}
			return wait
		}
	}
}

// cancel 归还一次预约（排队超时、客户端断开时）。尽力而为：只回退一个 T，
// 回退后不早于 now，避免把桶「透支」成负债以外的额外额度。
func (p *gcraParams) cancel(tat *atomic.Int64, now int64) {
	for {
		cur := tat.Load()
		next := cur - p.emission
		if next < now {
			next = now
		}
		if next >= cur || tat.CompareAndSwap(cur, next) {
			return
		}
	}
}

// remaining 估算剩余令牌（模拟与展示用，不修改状态）。
func (p *gcraParams) remaining(tat int64, now int64) float64 {
	if tat < now {
		tat = now
	}
	debt := float64(tat-now) / float64(p.emission)
	r := float64(p.burst) - debt
	if r < 0 {
		return 0
	}
	return math.Floor(r*100) / 100
}

// ── 单桶限速器（全局键 / 预热上限） ──

type gcraLimiter struct {
	params atomic.Pointer[gcraParams]
	tat    atomic.Int64
}

func newGCRALimiter(rate float64, period time.Duration, burst int) *gcraLimiter {
	l := &gcraLimiter{}
	l.params.Store(newGCRAParams(rate, period, burst))
	return l
}

func (l *gcraLimiter) set(rate float64, period time.Duration, burst int) {
	l.params.Store(newGCRAParams(rate, period, burst))
}

func (l *gcraLimiter) allow(now int64) bool { return l.params.Load().allow(&l.tat, now) }

func (l *gcraLimiter) reserve(now, maxWait int64) int64 {
	return l.params.Load().reserve(&l.tat, now, maxWait)
}

func (l *gcraLimiter) cancel(now int64) { l.params.Load().cancel(&l.tat, now) }

// ── 按键限速器：分片哈希表 ──
//
// 键先哈希成 uint64（maphash，进程随机种子，防哈希洪水），表里不保存键字符串。
// 2^64 空间里 200 万个键的碰撞概率约 1e-7，碰撞的后果只是两个键共用一个桶。
//
// 分片数 512：每片一把 RWMutex，命中已有键只拿读锁 + 一次 CAS；
// 新键才拿写锁。容量满时新键落进本片的溢出桶（共享限速），内存有硬上界。
// 后台每 10 秒清扫一遍「早已回满」的键（TAT 落后当前时间超过一个容忍窗口）。

const keyShards = 512

type keyedLimiter struct {
	params   atomic.Pointer[gcraParams]
	seed     maphash.Seed
	shards   [keyShards]keyShard
	perShard atomic.Int64
	tracked  atomic.Int64
	overflow atomic.Int64 // 落进溢出桶的次数
}

type keyShard struct {
	mu       sync.RWMutex
	m        map[uint64]*atomic.Int64
	overflow atomic.Int64 // 溢出桶的 TAT
	_        [40]byte     // 填充，避免相邻分片的锁落在同一缓存行
}

func newKeyedLimiter(rate float64, period time.Duration, burst int, capacity int) *keyedLimiter {
	k := &keyedLimiter{seed: maphash.MakeSeed()}
	k.params.Store(newGCRAParams(rate, period, burst))
	k.setCapacity(capacity)
	for i := range k.shards {
		k.shards[i].m = make(map[uint64]*atomic.Int64)
	}
	return k
}

func (k *keyedLimiter) set(rate float64, period time.Duration, burst int) {
	k.params.Store(newGCRAParams(rate, period, burst))
}

func (k *keyedLimiter) setCapacity(capacity int) {
	// 留 1/8 余量：哈希分布不可能绝对均匀，按平均值卡死会让少数分片提前溢出。
	per := capacity/keyShards + capacity/keyShards/8
	if per < 16 {
		per = 16
	}
	k.perShard.Store(int64(per))
}

// slot 取（或建）一个键的 TAT 单元。
func (k *keyedLimiter) slot(h uint64) *atomic.Int64 {
	sh := &k.shards[h%keyShards]
	sh.mu.RLock()
	cell := sh.m[h]
	sh.mu.RUnlock()
	if cell != nil {
		return cell
	}
	sh.mu.Lock()
	if cell = sh.m[h]; cell == nil {
		if int64(len(sh.m)) >= k.perShard.Load() {
			sh.mu.Unlock()
			k.overflow.Add(1)
			return &sh.overflow
		}
		cell = new(atomic.Int64)
		sh.m[h] = cell
		k.tracked.Add(1)
	}
	sh.mu.Unlock()
	return cell
}

// peek 读一个键的 TAT，不建新键（模拟用）。
func (k *keyedLimiter) peek(h uint64) int64 {
	sh := &k.shards[h%keyShards]
	sh.mu.RLock()
	cell := sh.m[h]
	sh.mu.RUnlock()
	if cell == nil {
		return 0
	}
	return cell.Load()
}

func (k *keyedLimiter) allow(h uint64, now int64) bool {
	return k.params.Load().allow(k.slot(h), now)
}

func (k *keyedLimiter) reserve(h uint64, now, maxWait int64) (int64, *atomic.Int64) {
	cell := k.slot(h)
	return k.params.Load().reserve(cell, now, maxWait), cell
}

// sweep 清掉已经回满的键。持锁时间与分片大小成正比（每片 ≤ perShard）。
func (k *keyedLimiter) sweep(now int64) {
	p := k.params.Load()
	// TAT 早于 now 就意味着桶已满；再留一个容忍窗口的余量，
	// 避免刚回满的活跃键被清掉又立刻重建。
	horizon := now - p.tolerance
	var removed int64
	for i := range k.shards {
		sh := &k.shards[i]
		sh.mu.Lock()
		for h, cell := range sh.m {
			if cell.Load() < horizon {
				delete(sh.m, h)
				removed++
			}
		}
		// 表收缩后重建，释放 map 的桶内存（Go 的 map 不会自动缩容）。
		if len(sh.m) == 0 {
			sh.m = make(map[uint64]*atomic.Int64)
		}
		sh.mu.Unlock()
	}
	if removed > 0 {
		k.tracked.Add(-removed)
	}
}
