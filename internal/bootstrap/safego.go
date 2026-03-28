package bootstrap

import (
	"context"
	"time"

	"aegis/pkg/crashlog"

	"go.uber.org/zap"
)

// SafeGo 启动带 panic 恢复的 goroutine。
// 如果 fn panic，记录崩溃日志；restartOnPanic 为 true 时自动重启（1 秒退避）。
func SafeGo(log *zap.Logger, cl *crashlog.Logger, component string, restartOnPanic bool, fn func()) {
	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						if cl != nil {
							cl.Write(component, r, true)
						}
						log.Error("goroutine panic recovered",
							zap.String("component", component),
							zap.Any("panic", r),
							zap.Stack("stack"),
						)
					}
				}()
				fn()
			}()

			if !restartOnPanic {
				return
			}
			log.Warn("goroutine will restart after backoff",
				zap.String("component", component),
				zap.Duration("backoff", time.Second),
			)
			time.Sleep(time.Second)
		}
	}()
}

// SafeGoLoop 启动带 panic 恢复的循环 goroutine。
// 每次 panic 后等待 interval 时间再重启，永不退出（除非 ctx 取消）。
func SafeGoLoop(ctx context.Context, log *zap.Logger, cl *crashlog.Logger, component string, interval time.Duration, fn func(ctx context.Context)) {
	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						if cl != nil {
							cl.Write(component, r, true)
						}
						log.Error("loop goroutine panic recovered",
							zap.String("component", component),
							zap.Any("panic", r),
							zap.Stack("stack"),
						)
					}
				}()
				fn(ctx)
			}()

			// 检查 ctx 是否已取消
			select {
			case <-ctx.Done():
				return
			default:
			}

			log.Warn("loop goroutine will restart after interval",
				zap.String("component", component),
				zap.Duration("interval", interval),
			)
			select {
			case <-time.After(interval):
			case <-ctx.Done():
				return
			}
		}
	}()
}
