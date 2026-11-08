// Package admin risk-admin 的 HTTP 管理接口（Hertz）。
//
// 控制台 → 主服务 /api/admin/risk/* → 这里的 /v1/*。主服务负责登录与权限（risk:read / risk:write），
// 转发时带 X-Risk-Token（内部令牌）与 X-Risk-Operator（操作者），这里只认令牌。
// 响应信封与主服务一致：{code: 200, message, data}，分页 data = {list, total, page, pageSize}。
package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/analytics"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/models"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/stream"
	"aegis/services/risk/internal/vector"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/registry"
	hconfig "github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// ServiceName 注册到 etcd 的服务名
const ServiceName = "aegis.risk.admin"

// Server 管理接口依赖
type Server struct {
	Cfg      config.Config
	Store    *store.Store
	Lists    *lists.Index
	Redis    redis.UniversalClient
	CH       *analytics.Client
	Graph    *graph.Client  // 可为 nil
	Vector   *vector.Client // 可为 nil
	Models   *models.Store
	Etcd     *clientv3.Client
	Decision decisionservice.Client
	Bus      *events.Bus
	Stream   stream.Target
	Logf     func(string, ...any)
}

// apiError 带业务码的错误
type apiError struct {
	status int
	code   int
	msg    string
	data   any
}

func (e *apiError) Error() string { return e.msg }

func bad(msg string) error { return &apiError{status: http.StatusBadRequest, code: 40000, msg: msg} }
func badData(msg string, d any) error {
	return &apiError{status: http.StatusUnprocessableEntity, code: 42200, msg: msg, data: d}
}
func missing(msg string) error { return &apiError{status: http.StatusNotFound, code: 40400, msg: msg} }

type handler func(ctx context.Context, c *app.RequestContext) (any, error)

func (s *Server) wrap(h handler) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		data, err := h(ctx, c)
		if err != nil {
			var ae *apiError
			switch {
			case errors.As(err, &ae):
				body := map[string]any{"code": ae.code, "message": ae.msg}
				if ae.data != nil {
					body["data"] = ae.data
				}
				c.JSON(ae.status, body)
			case errors.Is(err, store.ErrNotFound):
				c.JSON(http.StatusNotFound, map[string]any{"code": 40400, "message": "不存在"})
			default:
				if s.Logf != nil {
					s.Logf("%s %s 失败：%v", c.Method(), c.Path(), err)
				}
				c.JSON(http.StatusInternalServerError, map[string]any{"code": 50000, "message": err.Error()})
			}
			return
		}
		c.JSON(http.StatusOK, map[string]any{"code": 200, "message": "ok", "data": emptyLists(data)})
	}
}

func (s *Server) auth(ctx context.Context, c *app.RequestContext) {
	if s.Cfg.InternalToken != "" {
		got := string(c.GetHeader("X-Risk-Token"))
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.Cfg.InternalToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{"code": 40100, "message": "内部令牌不正确"})
			return
		}
	}
	c.Next(ctx)
}

func operator(c *app.RequestContext) string {
	if op := strings.TrimSpace(string(c.GetHeader("X-Risk-Operator"))); op != "" {
		// 主服务按 URL 转义带过来（请求头里放不了任意 UTF-8）
		if un, err := url.PathUnescape(op); err == nil {
			return un
		}
		return op
	}
	return "unknown"
}

func bind(c *app.RequestContext, v any) error {
	if err := json.Unmarshal(c.Request.Body(), v); err != nil {
		return bad("请求体不是合法 JSON：" + err.Error())
	}
	return nil
}

func qint(c *app.RequestContext, k string, def int) int {
	if v, err := strconv.Atoi(string(c.Query(k))); err == nil {
		return v
	}
	return def
}

func qint64(c *app.RequestContext, k string) int64 {
	v, _ := strconv.ParseInt(string(c.Query(k)), 10, 64)
	return v
}

func qapp(c *app.RequestContext) *int64 {
	raw := string(c.Query("app"))
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func qtime(c *app.RequestContext, k string, def time.Time) time.Time {
	raw := string(c.Query(k))
	if raw == "" {
		return def
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.UnixMilli(ms)
	}
	return def
}

func paged(list any, total int64, page, size int) map[string]any {
	return map[string]any{"list": list, "total": total, "page": page, "pageSize": size}
}

// Advertise 注册到 etcd 的地址：监听 0.0.0.0 时换成本机可达的 IP
func Advertise(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return listen
	}
	if h := os.Getenv("RISK_ADVERTISE_HOST"); h != "" {
		return net.JoinHostPort(h, port)
	}
	if name, err := os.Hostname(); err == nil {
		if ips, err := net.LookupHost(name); err == nil {
			for _, ip := range ips {
				if a := net.ParseIP(ip); a != nil && a.To4() != nil && !a.IsLoopback() {
					return net.JoinHostPort(ip, port)
				}
			}
		}
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			return net.JoinHostPort(n.IP.String(), port)
		}
	}
	return listen
}

// Run 起 HTTP 服务并注册到 etcd，直到 ctx 结束
func (s *Server) Run(ctx context.Context, reg registry.Registry) error {
	opts := []hconfig.Option{server.WithHostPorts(s.Cfg.AdminAddr), server.WithMaxRequestBodySize(80 << 20), server.WithExitWaitTime(5 * time.Second)}
	if reg != nil {
		opts = append(opts, server.WithRegistry(reg, &registry.Info{ServiceName: ServiceName, Addr: utils.NewNetAddr("tcp", Advertise(s.Cfg.AdminAddr)), Weight: 10}))
	}
	h := server.New(opts...)
	h.GET("/healthz", func(_ context.Context, c *app.RequestContext) { c.String(http.StatusOK, "ok") })
	h.GET("/readyz", func(ctx context.Context, c *app.RequestContext) {
		if err := s.Store.Pool().Ping(ctx); err != nil {
			c.String(http.StatusServiceUnavailable, err.Error())
			return
		}
		c.String(http.StatusOK, "ok")
	})
	s.routes(h.Group("/v1", s.auth))
	h.GET("/internal/cep/patterns", s.auth, s.wrap(s.internalPatterns))
	// Spin 而不是 Run：注册到 etcd 是挂在 Spin 的启动钩子上的，收到 SIGTERM 时它会先注销再优雅关闭
	_ = ctx
	h.Spin()
	return nil
}
