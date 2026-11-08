package keyedgate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGateSerializesSameKey(t *testing.T) {
	g := New(1000, 0)
	var inside, maxInside, total atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.Acquire(context.Background(), 7)
			if err != nil {
				t.Error(err)
				return
			}
			n := inside.Add(1)
			for {
				old := maxInside.Load()
				if n <= old || maxInside.CompareAndSwap(old, n) {
					break
				}
			}
			total.Add(1)
			time.Sleep(10 * time.Microsecond)
			inside.Add(-1)
			release()
		}()
	}
	wg.Wait()
	if maxInside.Load() != 1 || total.Load() != 200 {
		t.Fatalf("同一个键同时最多 1 个，得到 %d；共 %d 次", maxInside.Load(), total.Load())
	}
	if g.Len() != 0 {
		t.Fatalf("用完的键要从表里删掉，剩 %d", g.Len())
	}
}

func TestGateDifferentKeysParallel(t *testing.T) {
	g := New(0, 0)
	a, err := g.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := g.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("不同的键互不影响：%v", err)
	}
	a()
	b()
}

func TestGateRejectsBeyondDepth(t *testing.T) {
	g := New(2, 0)
	hold, _ := g.Acquire(context.Background(), 9)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			started <- struct{}{}
			if release, err := g.Acquire(ctx, 9); err == nil {
				release()
			}
		}()
	}
	<-started
	<-started
	deadline := time.Now().Add(time.Second)
	for {
		g.shards[9%shardCount].mu.Lock()
		refs := g.shards[9%shardCount].m[9].refs
		g.shards[9%shardCount].mu.Unlock()
		if refs == 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := g.Acquire(context.Background(), 9); !errors.Is(err, ErrBusy) {
		t.Fatalf("持有 1 个、排队 2 个之后第 4 个要被拒绝，得到 %v", err)
	}
	hold()
}

func TestGateMaxWait(t *testing.T) {
	g := New(10, 20*time.Millisecond)
	hold, _ := g.Acquire(context.Background(), 3)
	defer hold()
	start := time.Now()
	if _, err := g.Acquire(context.Background(), 3); !errors.Is(err, ErrBusy) {
		t.Fatalf("等过 maxWait 返回 ErrBusy，得到 %v", err)
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("要等满 maxWait 才放弃")
	}
}

func TestGateContextCancel(t *testing.T) {
	g := New(10, 0)
	hold, _ := g.Acquire(context.Background(), 4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := g.Acquire(ctx, 4); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ctx 结束时返回 ctx.Err()，得到 %v", err)
	}
	hold()
	if g.Len() != 0 {
		t.Fatalf("放弃排队的要减掉引用，剩 %d 个键", g.Len())
	}
}

func TestGateReleaseIdempotent(t *testing.T) {
	g := New(0, 0)
	release, _ := g.Acquire(context.Background(), 5)
	release()
	release()
	again, err := g.Acquire(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func BenchmarkGateUncontended(b *testing.B) {
	g := New(4, 0)
	ctx := context.Background()
	b.ReportAllocs()
	var next atomic.Uint64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			key := next.Add(1)
			release, err := g.Acquire(ctx, key)
			if err != nil {
				b.Fatal(err)
			}
			release()
		}
	})
}
