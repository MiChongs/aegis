package montecarlo

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 同一个种子，1 个与 16 个工作协程得到逐位相同的结果；合并严格按序号。
func TestRunIsDeterministicAcrossWorkers(t *testing.T) {
	run := func(workers int) (float64, []int) {
		var sum float64
		order := []int{}
		err := Run(context.Background(), Config{Replications: 200, Workers: workers, Seed: 42},
			func(_ context.Context, index int, rng *rand.Rand) (float64, error) {
				// 让先领到的后跑完，逼出乱序
				if index%7 == 0 {
					time.Sleep(time.Millisecond)
				}
				var s float64
				for range 1000 {
					s += rng.Float64()
				}
				return s, nil
			},
			func(index int, result float64) error {
				order = append(order, index)
				sum += result * 1.0000001 // 浮点累加对顺序敏感
				return nil
			})
		if err != nil {
			t.Fatal(err)
		}
		return sum, order
	}
	serial, order := run(1)
	parallel, parallelOrder := run(16)
	if serial != parallel {
		t.Fatalf("1 个与 16 个工作协程的结果不同：%v / %v", serial, parallel)
	}
	for i := range order {
		if order[i] != i || parallelOrder[i] != i {
			t.Fatalf("合并没有按序号：%v", parallelOrder[:i+1])
		}
	}
}

func TestRunBoundsPendingResults(t *testing.T) {
	const workers = 4
	var running, peak atomic.Int64
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Config{Replications: 100, Workers: workers, Seed: 1},
			func(_ context.Context, index int, _ *rand.Rand) (int, error) {
				n := running.Add(1)
				for {
					old := peak.Load()
					if n <= old || peak.CompareAndSwap(old, n) {
						break
					}
				}
				if index == 0 {
					<-release // 第 0 次迟迟不结束
				}
				return index, nil
			},
			func(int, int) error { return nil })
	}()
	// 第 0 次卡着时，别的协程最多做到窗口尽头就得停下等
	time.Sleep(100 * time.Millisecond)
	if started := running.Load(); started > 2*workers {
		t.Fatalf("第 0 次没合并时开始了 %d 次，窗口应为 %d", started, 2*workers)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if running.Load() != 100 {
		t.Fatalf("应跑完 100 次，得到 %d", running.Load())
	}
}

func TestRunStopsOnErrorAndCancel(t *testing.T) {
	boom := errors.New("boom")
	collected := 0
	err := Run(context.Background(), Config{Replications: 50, Workers: 4},
		func(_ context.Context, index int, _ *rand.Rand) (int, error) {
			if index == 10 {
				return 0, boom
			}
			return index, nil
		},
		func(int, int) error { collected++; return nil })
	if !errors.Is(err, boom) {
		t.Fatalf("应返回重复实验的错误，得到 %v", err)
	}
	if collected > 10 {
		t.Fatalf("出错的序号之后不应再合并，合并了 %d 次", collected)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var ran atomic.Int64
	err = Run(ctx, Config{Replications: 10_000, Workers: 4},
		func(ctx context.Context, index int, _ *rand.Rand) (int, error) {
			if ran.Add(1) == 20 {
				cancel()
			}
			return index, ctx.Err()
		},
		func(int, int) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，得到 %v", err)
	}
	if ran.Load() > 200 {
		t.Fatalf("取消后还跑了 %d 次", ran.Load())
	}
	if err := Run(context.Background(), Config{}, func(context.Context, int, *rand.Rand) (int, error) { return 0, nil },
		func(int, int) error { return nil }); !errors.Is(err, ErrNoReplications) {
		t.Fatalf("没有重复实验应报错，得到 %v", err)
	}
}

// 工作协程里的 panic 变成这一次计算的错误，不带垮进程。
func TestRunTurnsPanicIntoError(t *testing.T) {
	err := Run(context.Background(), Config{Replications: 20, Workers: 4},
		func(_ context.Context, index int, _ *rand.Rand) (int, error) {
			if index == 5 {
				var values []int
				return values[index], nil // 越界
			}
			return index, nil
		},
		func(int, int) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "replication 5 panicked") {
		t.Fatalf("panic 应变成错误，得到 %v", err)
	}
}

func TestStreamsAreIndependent(t *testing.T) {
	// 相邻序号的随机流不相关：两条流各取 5 万个数，相关系数应在 0 附近
	a, b := Stream(7, 0), Stream(7, 1)
	const n = 50_000
	var sa, sb, sab, saa, sbb float64
	for range n {
		x, y := a.Float64(), b.Float64()
		sa, sb, sab, saa, sbb = sa+x, sb+y, sab+x*y, saa+x*x, sbb+y*y
	}
	cov := sab/n - sa/n*sb/n
	corr := cov / math.Sqrt((saa/n-sa/n*sa/n)*(sbb/n-sb/n*sb/n))
	if math.Abs(corr) > 0.02 {
		t.Fatalf("相邻随机流相关系数 %.4f", corr)
	}
	if Stream(7, 3).Uint64() != Stream(7, 3).Uint64() {
		t.Fatal("同一个 (seed, index) 应得到同一条流")
	}
	if Derive(7, 3) == Derive(7, 4) || Derive(7, 3) != Derive(7, 3) {
		t.Fatal("Derive 应确定且随序号变化")
	}
}

func TestMomentsMergeMatchesSinglePass(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var whole, left, right Moments
	for i := range 10_000 {
		x := rng.NormFloat64()*3 + 10
		whole.Add(x)
		if i%3 == 0 {
			left.Add(x)
		} else {
			right.Add(x)
		}
	}
	left.Merge(right)
	if math.Abs(left.Mean-whole.Mean) > 1e-9 || math.Abs(left.Variance()-whole.Variance()) > 1e-9 {
		t.Fatalf("合并后的均值方差应与一次算完相同：%v/%v，%v/%v", left.Mean, whole.Mean, left.Variance(), whole.Variance())
	}
	if left.Min != whole.Min || left.Max != whole.Max || left.N != whole.N {
		t.Fatal("合并后的极值与个数不对")
	}
	estimate := whole.Estimate(0.95)
	if !(estimate.Low < 10 && 10 < estimate.High) {
		t.Fatalf("95%% 区间应盖住真实均值 10：%+v", estimate)
	}
	if single := (Moments{N: 1, Mean: 5}).Estimate(0.95); single.Low != 5 || single.High != 5 {
		t.Fatalf("只有一次时给不出区间：%+v", single)
	}
}

// 置信区间的覆盖率：重复 400 组、每组 12 个观测，95% 区间应有约 95% 盖住真实均值。
func TestEstimateCoverage(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 9))
	covered := 0
	const groups = 400
	for range groups {
		var m Moments
		for range 12 {
			m.Add(rng.ExpFloat64()) // 均值 1 的指数分布
		}
		if e := m.Estimate(0.95); e.Low <= 1 && 1 <= e.High {
			covered++
		}
	}
	if rate := float64(covered) / groups; rate < 0.88 || rate > 0.99 {
		t.Fatalf("95%% 区间的覆盖率 %.3f", rate)
	}
}

func TestHistogramQuantilesAndMerge(t *testing.T) {
	var a, b Histogram
	for value := 1; value <= 100; value++ {
		if value%2 == 0 {
			a.Add(value)
		} else {
			b.Add(value)
		}
	}
	b.AddN(10_000, 1) // 稀疏区
	a.Merge(&b)
	if a.Count() != 101 || a.Max() != 10_000 {
		t.Fatalf("个数 %d，最大 %d", a.Count(), a.Max())
	}
	for q, want := range map[float64]int{0.5: 51, 0.9: 91, 0.99: 100, 1: 10_000} {
		if got := a.Quantile(q); got != want {
			t.Fatalf("P%.0f 应为 %d，得到 %d", q*100, want, got)
		}
	}
	buckets := a.Buckets(20)
	var total int64
	for _, bucket := range buckets {
		total += bucket.Count
	}
	if total != 101 || len(buckets) > 20 {
		t.Fatalf("分段后个数 %d、段数 %d", total, len(buckets))
	}
	var empty Histogram
	if empty.Quantile(0.5) != 0 || len(empty.Buckets(10)) != 0 {
		t.Fatal("空直方图")
	}
}

func TestSketchQuantilesWithinAccuracy(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	left, right := NewSketch(), NewSketch()
	values := make([]float64, 0, 100_000)
	for i := range 100_000 {
		x := math.Exp(rng.NormFloat64() * 2) // 跨好几个数量级
		values = append(values, x)
		if i%2 == 0 {
			left.Add(x)
		} else {
			right.Add(x)
		}
	}
	left.Merge(right)
	sorted := append([]float64(nil), values...)
	sortFloats(sorted)
	for _, q := range []float64{0.5, 0.9, 0.99, 0.999} {
		exact := sorted[int(q*float64(len(sorted)-1))]
		got := left.Quantile(q)
		if math.Abs(got-exact)/exact > 0.02 {
			t.Fatalf("P%.1f 应约为 %.4f，得到 %.4f", q*100, exact, got)
		}
	}
	if left.Count() != 100_000 || left.Max() != sorted[len(sorted)-1] {
		t.Fatal("个数与最大值应精确")
	}
	if NewSketch().Quantile(0.5) != 0 {
		t.Fatal("空草图")
	}
}

func TestWilsonInterval(t *testing.T) {
	low, high := Wilson(8, 1_000_000, 0.95)
	if !(low > 0 && low < 8e-6 && high > 8e-6 && high < 2e-5) {
		t.Fatalf("稀有事件的区间 [%g, %g]", low, high)
	}
	if low, high := Wilson(0, 100, 0.95); low != 0 || high <= 0 {
		t.Fatalf("0 次成功的区间下界应为 0、上界大于 0：[%g, %g]", low, high)
	}
	if low, high := Wilson(0, 0, 0.95); low != 0 || high != 0 {
		t.Fatal("没有试验")
	}
}

func sortFloats(values []float64) {
	// 测试里用的简单排序封装（避免再引一个包）
	for gap := len(values) / 2; gap > 0; gap /= 2 {
		for i := gap; i < len(values); i++ {
			for j := i; j >= gap && values[j-gap] > values[j]; j -= gap {
				values[j-gap], values[j] = values[j], values[j-gap]
			}
		}
	}
}
