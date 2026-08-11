package taskpool

import (
	"context"
	"fmt"
	"sync"

	"github.com/panjf2000/ants/v2"
)

func Dispatch[T any](ctx context.Context, concurrency int, items []T, handler func(context.Context, T)) error {
	if len(items) == 0 {
		return nil
	}
	if handler == nil {
		return fmt.Errorf("task handler is required")
	}
	_, err := dispatch(ctx, concurrency, items, func(taskCtx context.Context, item T) error {
		handler(taskCtx, item)
		return nil
	})
	return err
}

func DispatchWithError[T any](ctx context.Context, concurrency int, items []T, handler func(context.Context, T) error) error {
	if len(items) == 0 {
		return nil
	}
	if handler == nil {
		return fmt.Errorf("task handler is required")
	}
	_, err := dispatch(ctx, concurrency, items, handler)
	return err
}

func dispatch[T any](ctx context.Context, concurrency int, items []T, handler func(context.Context, T) error) (int, error) {
	if concurrency <= 0 {
		concurrency = 1
	}
	if len(items) < concurrency {
		concurrency = len(items)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg        sync.WaitGroup
		firstErr  error
		firstErrM sync.Mutex
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		firstErrM.Lock()
		defer firstErrM.Unlock()
		if firstErr != nil {
			return
		}
		firstErr = err
		cancel()
	}

	pool, err := ants.NewPool(concurrency, ants.WithPreAlloc(true), ants.WithPanicHandler(func(panicValue any) {
		recordErr(fmt.Errorf("ants worker panic: %v", panicValue))
	}))
	if err != nil {
		return 0, err
	}
	defer pool.Release()

	submitted := 0
	for _, item := range items {
		if err := runCtx.Err(); err != nil {
			break
		}
		task := item
		wg.Add(1)
		if err := pool.Submit(func() {
			defer wg.Done()
			if runCtx.Err() != nil {
				return
			}
			recordErr(handler(runCtx, task))
		}); err != nil {
			wg.Done()
			recordErr(err)
			break
		}
		submitted++
	}

	wg.Wait()
	if firstErr != nil {
		return submitted, firstErr
	}
	if err := ctx.Err(); err != nil {
		return submitted, err
	}
	return submitted, nil
}
