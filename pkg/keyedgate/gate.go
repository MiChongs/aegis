// Package keyedgate 按键串行的闸门：同一个键同一时刻只有一个持有者，后来的按到达顺序排队，
// 队伍超过上限或等太久就立刻拒绝。
//
// 用在「同一个人的请求必须串行」的地方（抽奖、兑换）：串行本来由数据库行锁保证，但在数据库里排队
// 意味着每个排队的请求都占着一条连接。一个人并发打几十个请求，就能把整个连接池占满、拖垮所有人。
// 先在进程里排队：排队的请求不占连接，超出上限的直接告诉客户端「稍后再试」。数据库行锁仍然在，
// 负责多实例之间的串行。
//
// 实现：按键哈希分 64 片，每片一把锁和一张表；每个键一个容量为 1 的通道当信号量（Go 的通道发送方
// 按到达顺序排队）。没有人持有、也没有人排队的键立即从表里删掉，表只装正在用的键。
package keyedgate

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrBusy 这个键前面排的人太多或等太久（调用方应让客户端稍后重试）。
var ErrBusy = errors.New("keyedgate: busy")

const shardCount = 64

// Gate 按键串行的闸门。零值不可用，用 New 建。
type Gate struct {
	shards [shardCount]shard
	// depth 除了持有者之外最多排几个
	depth int
	// maxWait 排队最多等多久（0 不限，只受 ctx 约束）
	maxWait time.Duration
}

type shard struct {
	mu sync.Mutex
	m  map[uint64]*entry
}

type entry struct {
	sem chan struct{}
	// refs 持有者 + 排队者
	refs int
}

// New 建一个闸门：每个键除了持有者之外最多排 depth 个，排队最多等 maxWait。
func New(depth int, maxWait time.Duration) *Gate {
	if depth < 0 {
		depth = 0
	}
	g := &Gate{depth: depth, maxWait: maxWait}
	for i := range g.shards {
		g.shards[i].m = make(map[uint64]*entry)
	}
	return g
}

// Acquire 拿到这个键的通行权，返回释放函数（必须调用且只调用一次）。
// 排队超出上限、等到 maxWait 都返回 ErrBusy；ctx 结束返回 ctx.Err()。
func (g *Gate) Acquire(ctx context.Context, key uint64) (func(), error) {
	sh := &g.shards[key%shardCount]
	sh.mu.Lock()
	e := sh.m[key]
	if e == nil {
		e = &entry{sem: make(chan struct{}, 1)}
		sh.m[key] = e
	}
	if e.refs > g.depth {
		sh.mu.Unlock()
		return nil, ErrBusy
	}
	e.refs++
	sh.mu.Unlock()

	// 没人持有时直接拿到，不建计时器
	select {
	case e.sem <- struct{}{}:
		return g.releaser(sh, key, e), nil
	default:
	}
	var timeout <-chan time.Time
	if g.maxWait > 0 {
		timer := time.NewTimer(g.maxWait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case e.sem <- struct{}{}:
		return g.releaser(sh, key, e), nil
	case <-timeout:
		g.unref(sh, key, e)
		return nil, ErrBusy
	case <-ctx.Done():
		g.unref(sh, key, e)
		return nil, ctx.Err()
	}
}

func (g *Gate) releaser(sh *shard, key uint64, e *entry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			<-e.sem
			g.unref(sh, key, e)
		})
	}
}

func (g *Gate) unref(sh *shard, key uint64, e *entry) {
	sh.mu.Lock()
	e.refs--
	if e.refs == 0 && sh.m[key] == e {
		delete(sh.m, key)
	}
	sh.mu.Unlock()
}

// Len 正在用（有人持有或排队）的键数。
func (g *Gate) Len() int {
	total := 0
	for i := range g.shards {
		sh := &g.shards[i]
		sh.mu.Lock()
		total += len(sh.m)
		sh.mu.Unlock()
	}
	return total
}
