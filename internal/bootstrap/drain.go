package bootstrap

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// drainState 滚动发布时的摘流开关。
//
// 收到停止信号后先让 /readyz 回 503，等 SHUTDOWN_DRAIN_DELAY 让负载均衡（Swarm 健康检查、
// Traefik、CDN 回源探测）把本实例摘掉，再关监听。直接 Shutdown 的话，负载均衡在下一次探测前
// 还会把请求往这里送，这些请求会撞上已关闭的监听，表现为发布期间零星的 502。
type drainState struct {
	draining atomic.Bool
	// schemaBehind 镜像里带的迁移在库里还没执行过（MIGRATE_ON_BOOT=false 且发布时跳过了 migrate）。
	// 此时 /readyz 回 503：start-first 滚动会停在新副本上，不会让新代码跑在旧表结构上。
	schemaBehind atomic.Bool
}

// wrap 只拦截就绪探针；其余请求照常处理（摘流期间在途与新到的请求都要服务完）。
func (d *drainState) wrap(next http.Handler) http.Handler {
	if d == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" && d.schemaBehind.Load() && !d.draining.Load() {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(`{"code":50314,"message":"数据库迁移尚未执行","data":{"status":"schema_behind"}}`))
			}
			return
		}
		if r.URL.Path == "/readyz" && d.draining.Load() {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Connection", "close")
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method != http.MethodHead {
				_, _ = w.Write([]byte(`{"code":50313,"message":"实例正在下线","data":{"status":"draining"}}`))
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// begin 置为摘流并等待 delay（ctx 先到期则提前返回）。
func (d *drainState) begin(ctx context.Context, delay time.Duration) {
	if d == nil {
		return
	}
	d.draining.Store(true)
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
