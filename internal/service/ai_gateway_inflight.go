package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// 进程退出时的在途对话。
//
// http.Server.Shutdown 不取消在途请求的上下文，只等它们自己结束；流式对话一跑几分钟，
// 等到 SHUTDOWN_TIMEOUT 到点进程就直接退出 —— 连接被硬切，客户端拿不到错误事件，
// 调用记录停在 reserved，暂扣要等 StreamHoldTTL（2 小时）后才被后台放回，期间还占着
// 用户的并发名额（MaxConcurrent），后续请求连吃 40212。
//
// 所以退出时先给在途调用一小段宽限，之后主动取消：调用照「上游失败」收尾 —— 暂扣整笔放回、
// 不扣费，客户端收到一个可重试的错误（40231），换到新实例上重来。

// errAIGatewayShuttingDown 退出取消在途调用时的原因。
var errAIGatewayShuttingDown = errors.New("ai gateway: shutting down")

// aiGatewayShutdownError 退出取消时交给调用方的错误。
func aiGatewayShutdownError() error {
	return aiGatewayError(40231, http.StatusServiceUnavailable, "服务正在更新，本次调用已中止且不扣费，请重试")
}

type aiGatewayInflight struct {
	mu     sync.Mutex
	next   uint64
	calls  map[uint64]context.CancelCauseFunc
	wg     sync.WaitGroup
	closed bool
}

func newAIGatewayInflight() *aiGatewayInflight {
	return &aiGatewayInflight{calls: map[uint64]context.CancelCauseFunc{}}
}

// track 登记一次在途调用；返回的 done 必须在调用收尾（结算或放回）之后调用。
func (r *aiGatewayInflight) track(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel(errAIGatewayShuttingDown)
		return ctx, func() {}
	}
	r.next++
	id := r.next
	r.calls[id] = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.calls, id)
			r.mu.Unlock()
			cancel(nil)
			r.wg.Done()
		})
	}
}

func (r *aiGatewayInflight) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// cancelAll 取消全部在途调用，此后新登记的调用直接以退出为由取消。
func (r *aiGatewayInflight) cancelAll() int {
	r.mu.Lock()
	r.closed = true
	calls := make([]context.CancelCauseFunc, 0, len(r.calls))
	for _, cancel := range r.calls {
		calls = append(calls, cancel)
	}
	r.mu.Unlock()
	for _, cancel := range calls {
		cancel(errAIGatewayShuttingDown)
	}
	return len(calls)
}

// wait 等在途调用全部收尾，最多 timeout。
func (r *aiGatewayInflight) wait(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// shuttingDown 这个上下文是不是因为进程退出被取消的。
func aiGatewayShuttingDown(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errAIGatewayShuttingDown)
}

// CancelInFlightAfter 进程退出的第一步（在 http.Server.Shutdown 之前调用）：在途对话再跑
// grace 就主动取消，让它们在 Shutdown 的期限内带着错误事件收尾、放回暂扣。
// 宽限内全部自然结束就什么也不做。
func (s *AIGatewayService) CancelInFlightAfter(grace time.Duration) {
	go func() {
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) {
			if s.inflight.count() == 0 {
				return
			}
			time.Sleep(min(200*time.Millisecond, time.Until(deadline)))
		}
		if canceled := s.inflight.cancelAll(); canceled > 0 {
			s.log.Warn("进程退出：在途 AI 中转调用已取消（暂扣放回，不扣费）", zap.Int("count", canceled))
		}
	}()
}

// drainInFlight Stop 的兜底：Shutdown 到点后仍在跑的调用立即取消，并等它们放回暂扣。
func (s *AIGatewayService) drainInFlight(timeout time.Duration) {
	if canceled := s.inflight.cancelAll(); canceled > 0 {
		s.log.Warn("进程退出：在途 AI 中转调用已取消（暂扣放回，不扣费）", zap.Int("count", canceled))
	}
	if !s.inflight.wait(timeout) {
		s.log.Warn("进程退出：仍有 AI 中转调用未收尾，其暂扣将由后台按过期放回")
	}
}
