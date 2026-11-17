package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Worker 侧的后台任务租约名（规则见 cluster_wiring.go 顶部）。
const (
	leaseGeoAnalytics = "geo.analytics" // 小时聚合与每日分区维护：幂等，但只需全集群跑一份
	leaseTicketSLA    = "ticket.sla"    // SLA 巡检：条件更新防重，但告警投递只该有一个实例在扫
	leaseIPBanCleanup = "ipban.cleanup" // 过期封禁清理
)

// workerGate 返回 name 的租约判定。没有集群句柄时恒为 true。
func (w *WorkerApp) workerGate(name string) func() bool {
	if w == nil || w.Cluster == nil {
		return func() bool { return true }
	}
	if w.gates == nil {
		w.gates = map[string]func() bool{}
	}
	if gate, ok := w.gates[name]; ok {
		return gate
	}
	gate := w.Cluster.Gate(name)
	w.gates[name] = gate
	return gate
}

// attachCluster 在 Elector.Start 之前登记全部租约名，第一轮就能抢到。
func (w *WorkerApp) attachCluster() {
	for _, name := range []string{leaseAutoSign, leaseGeoAnalytics, leaseTicketSLA, leaseIPBanCleanup} {
		w.workerGate(name)
	}
}

// startHealthServer Worker 不对外提供 HTTP，但编排系统需要探针：
// /healthz 只说明进程活着；/readyz 检查 Postgres、Redis 与 NATS 连接。
// WORKER_HEALTH_ADDR=off 时不监听（Unified 模式下由 API 的 8088 承担探针）。
func (w *WorkerApp) startHealthServer() {
	addr := strings.TrimSpace(w.Config.Cluster.WorkerHealthAddr)
	if addr == "" || strings.EqualFold(addr, "off") {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, r *http.Request) {
		writeProbe(rw, r, http.StatusOK, map[string]any{"status": "healthy", "node": w.Cluster.ID()})
	})
	mux.HandleFunc("/readyz", func(rw http.ResponseWriter, r *http.Request) {
		if w.draining.Load() {
			writeProbe(rw, r, http.StatusServiceUnavailable, map[string]any{"status": "draining"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{"postgres": "ok", "redis": "ok", "nats": "ok"}
		ready := true
		if w.Postgres == nil || w.Postgres.Ping(ctx) != nil {
			checks["postgres"], ready = "down", false
		}
		if w.Redis == nil || w.Redis.Ping(ctx).Err() != nil {
			checks["redis"], ready = "down", false
		}
		if w.NATSConn == nil || !w.NATSConn.IsConnected() {
			checks["nats"], ready = "down", false
		}
		status := http.StatusOK
		state := "ready"
		if !ready {
			status, state = http.StatusServiceUnavailable, "not_ready"
		}
		writeProbe(rw, r, status, map[string]any{
			"status": state, "components": checks, "node": w.Cluster.ID(), "leases": w.Cluster.Elector.Held(),
		})
	})
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// 端口被占（例如同机两个 Unified 进程）只降级：探针没了，任务照常跑
		w.Logger.Warn("worker 健康检查端口监听失败", zap.String("addr", addr), zap.Error(err))
		return
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	w.health = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			w.Logger.Warn("worker 健康检查服务退出", zap.Error(err))
		}
	}()
	w.Logger.Info("worker 健康检查已监听", zap.String("addr", ln.Addr().String()))
}

func writeProbe(rw http.ResponseWriter, r *http.Request, status int, body map[string]any) {
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("Content-Type", "application/json; charset=utf-8")
	rw.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(rw).Encode(body)
}
