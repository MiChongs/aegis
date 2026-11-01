package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// asyncBatchWriter 把注册路径上的写库挪出关键路径：入队立刻返回，若干个工作协程各自攒批，
// 攒够 maxBatch 条或等满 interval 就写一次（台账一条 INSERT … unnest、记录一次 COPY）。
//
//   - 队列有界：库慢到写不过来时丢最新的一条并计数，而不是让注册请求排队等库
//   - 多个工作协程并行写：一个批次卡在慢查询上时，其余协程照常消费
//   - Stop 关闭入口后把队列里剩下的全部写完再返回（受 ctx 期限约束）
type asyncBatchWriter[T any] struct {
	name     string
	log      *zap.Logger
	queue    chan T
	flush    func(ctx context.Context, items []T) error
	maxBatch int
	interval time.Duration
	timeout  time.Duration
	workers  int

	startOnce sync.Once
	stopOnce  sync.Once
	started   atomic.Bool
	closed    atomic.Bool
	wg        sync.WaitGroup
	dropped   atomic.Int64
	written   atomic.Int64
	failed    atomic.Int64
	// mu 保护 closed 与 close(queue) 之间的竞态：Stop 关通道时不能有 Enqueue 正在往里写
	mu sync.RWMutex
}

func newAsyncBatchWriter[T any](name string, log *zap.Logger, capacity, workers, maxBatch int, interval time.Duration,
	flush func(ctx context.Context, items []T) error) *asyncBatchWriter[T] {
	if log == nil {
		log = zap.NewNop()
	}
	return &asyncBatchWriter[T]{
		name: name, log: log, queue: make(chan T, capacity), flush: flush,
		maxBatch: maxBatch, interval: interval, timeout: 10 * time.Second, workers: max(1, workers),
	}
}

// Start 启动工作协程。重复调用无效。
func (w *asyncBatchWriter[T]) Start() {
	w.startOnce.Do(func() {
		for i := 0; i < w.workers; i++ {
			w.wg.Add(1)
			go w.run()
		}
		w.started.Store(true)
	})
}

// Enqueue 入队；队列满或已停止时返回 false。未 Start 时直接同步写（测试与离线工具）。
func (w *asyncBatchWriter[T]) Enqueue(item T) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed.Load() {
		w.dropped.Add(1)
		return false
	}
	if !w.started.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
		defer cancel()
		if err := w.flush(ctx, []T{item}); err != nil {
			w.failed.Add(1)
			w.log.Warn("同步写入失败", zap.String("writer", w.name), zap.Error(err))
			return false
		}
		w.written.Add(1)
		return true
	}
	select {
	case w.queue <- item:
		return true
	default:
		w.dropped.Add(1)
		w.log.Warn("异步写入队列已满，丢弃一条", zap.String("writer", w.name), zap.Int64("dropped", w.dropped.Load()))
		return false
	}
}

func (w *asyncBatchWriter[T]) run() {
	defer w.wg.Done()
	batch := make([]T, 0, w.maxBatch)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	write := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), w.timeout)
		err := w.flush(ctx, batch)
		cancel()
		if err != nil {
			w.failed.Add(int64(len(batch)))
			w.log.Warn("异步批量写入失败", zap.String("writer", w.name), zap.Int("size", len(batch)), zap.Error(err))
		} else {
			w.written.Add(int64(len(batch)))
		}
		batch = make([]T, 0, w.maxBatch)
	}
	for {
		select {
		case item, ok := <-w.queue:
			if !ok {
				write()
				return
			}
			batch = append(batch, item)
			if len(batch) >= w.maxBatch {
				write()
			}
		case <-ticker.C:
			write()
		}
	}
}

// Stop 停止接收并把剩余的写完。ctx 到期时不再等，剩下的随进程退出丢弃。
func (w *asyncBatchWriter[T]) Stop(ctx context.Context) {
	w.stopOnce.Do(func() {
		w.mu.Lock()
		w.closed.Store(true)
		close(w.queue)
		w.mu.Unlock()
		done := make(chan struct{})
		go func() {
			w.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			w.log.Warn("异步写入未在期限内写完", zap.String("writer", w.name), zap.Int("pending", len(w.queue)))
		}
	})
}

// asyncBatchStats 写入统计，概览里展示「队列健康」。
type asyncBatchStats struct {
	Pending int   `json:"pending"`
	Written int64 `json:"written"`
	Dropped int64 `json:"dropped"`
	Failed  int64 `json:"failed"`
}

func (w *asyncBatchWriter[T]) Stats() asyncBatchStats {
	return asyncBatchStats{Pending: len(w.queue), Written: w.written.Load(), Dropped: w.dropped.Load(), Failed: w.failed.Load()}
}
