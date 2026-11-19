// Package montecarlo 可复现的并行蒙特卡洛计算框架。
//
// 一次计算由若干次互相独立的「重复实验」（replication）组成：每一次用自己的随机流把整件事从头做一遍，
// 得到一份结果；重复实验之间的差异就是统计误差，于是每个指标都能给出置信区间，而不只是一个数。
//
// 三条保证：
//
//   - 可复现：第 i 次重复实验的随机流只由 (种子, i) 决定（SplitMix64 派生两个 64 位状态喂给 PCG），
//     与用了几个工作协程、谁先跑完无关。
//   - 合并有序：结果按序号 0, 1, 2 … 依次交给 collect，同一时刻只有一个 collect 在跑。浮点累加的顺序固定，
//     所以同一个种子在 1 个核与 24 个核上得到逐位相同的结果。
//   - 内存有界：跑完但还没轮到合并的结果最多攒 2 × 工作协程数份，最慢的那一次不会让结果堆满内存。
//
// 配套的统计件都是可合并的（每次重复实验各算各的，最后按序号并起来）：Moments（在线均值与方差、
// Student t 置信区间）、Histogram（整数直方图，精确分位数）、Sketch（DDSketch，相对误差有界的分位数）。
package montecarlo

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"
)

// Config 一次计算的规模与并行度。
type Config struct {
	// Replications 重复实验的次数（至少 1）
	Replications int
	// Workers 同时跑几个；0 为 GOMAXPROCS
	Workers int
	// Seed 随机种子：同一个种子得到同一份结果
	Seed uint64
	// Progress 每合并完一次调用一次（done 从 1 到 Replications）；在合并的那个协程里调用，不必自己加锁
	Progress func(done, total int)
}

// ErrNoReplications 没有要跑的重复实验。
var ErrNoReplications = errors.New("montecarlo: replications must be positive")

// Stream 第 index 条随机流：只由 (seed, index) 决定，不同序号之间互不相关。
//
// 把 seed 与 index 混进 SplitMix64 的状态，取两个输出作 PCG 的两个 64 位状态。SplitMix64 是
// 双射的终混函数，相邻序号的输出没有可见的相关性（Steele、Lea、Flood 2014）。
func Stream(seed, index uint64) *rand.Rand {
	state := seed ^ (index+1)*0x9e3779b97f4a7c15
	return rand.New(rand.NewPCG(splitmix(&state), splitmix(&state)))
}

// Derive 从 (seed, index) 派生一个 64 位数（给需要自己播种的地方用，与 Stream 的输出互不相关）。
func Derive(seed, index uint64) uint64 {
	state := seed ^ 0xd1b54a32d192ed03 ^ (index+1)*0x9e3779b97f4a7c15
	return splitmix(&state)
}

func splitmix(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// guarded 跑一次重复实验，把里面的 panic 变成错误：工作协程里没人接的 panic 会带垮整个进程，
// 而一次算错只该让这一次计算失败。
func guarded[R any](ctx context.Context, index int, seed uint64,
	replicate func(ctx context.Context, index int, rng *rand.Rand) (R, error)) (result R, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("montecarlo: replication %d panicked: %v", index, r)
		}
	}()
	return replicate(ctx, index, Stream(seed, uint64(index)))
}

// Run 并行跑 cfg.Replications 次重复实验，并按序号依次把结果交给 collect。
//
// replicate 在工作协程里跑，只能用传给它的 rng，不能碰共享的可变状态；collect 串行、按序号升序调用，
// 在里面把这一次的结果并进总的统计量。任何一次返回错误或 ctx 取消时停下并返回那个错误
// （已经合并的结果保持原样，调用方可以据此给出部分进度）。
func Run[R any](ctx context.Context, cfg Config,
	replicate func(ctx context.Context, index int, rng *rand.Rand) (R, error),
	collect func(index int, result R) error) error {
	total := cfg.Replications
	if total <= 0 {
		return ErrNoReplications
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	workers = min(workers, total)
	window := 2 * workers

	var (
		mu      sync.Mutex
		ready   = sync.NewCond(&mu)
		pending = make(map[int]R, window)
		next    int // 下一个要合并的序号
		claimed int // 下一个要开始的序号
		failed  error
	)
	group, groupCtx := errgroup.WithContext(ctx)
	// ctx 取消时把等窗口的协程叫醒
	stop := context.AfterFunc(groupCtx, func() {
		mu.Lock()
		ready.Broadcast()
		mu.Unlock()
	})
	defer stop()

	// claim 领一个序号；序号超出合并进度一个窗口时等合并追上来
	claim := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		for {
			if failed != nil || groupCtx.Err() != nil || claimed >= total {
				return 0, false
			}
			if claimed < next+window {
				index := claimed
				claimed++
				return index, true
			}
			ready.Wait()
		}
	}
	// deliver 交回一份结果，并把从 next 起连续就绪的结果依次合并
	deliver := func(index int, result R) error {
		mu.Lock()
		defer mu.Unlock()
		pending[index] = result
		for failed == nil {
			item, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if err := collect(next, item); err != nil {
				failed = err
				break
			}
			next++
			if cfg.Progress != nil {
				cfg.Progress(next, total)
			}
		}
		ready.Broadcast()
		return failed
	}

	for range workers {
		group.Go(func() error {
			for {
				index, ok := claim()
				if !ok {
					return groupCtx.Err()
				}
				result, err := guarded(groupCtx, index, cfg.Seed, replicate)
				if err != nil {
					mu.Lock()
					if failed == nil {
						failed = err
					}
					ready.Broadcast()
					mu.Unlock()
					return err
				}
				if err := deliver(index, result); err != nil {
					return err
				}
			}
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	if failed != nil {
		return failed
	}
	return ctx.Err()
}
