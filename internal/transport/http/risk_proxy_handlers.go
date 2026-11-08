package httptransport

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 风控平台的管理接口在独立服务 risk-admin 里（services/risk，docs/risk-platform.md）。
// 主服务只做登录与权限：/api/admin/system/risk/* 原样转发到 risk-admin 的 /v1/*，
// 带上内部令牌与操作者身份；请求体、查询参数、响应（含 {code, data} 信封）都不改。
//
// 权限与旧风控中心一致：能进 /api/admin/system 的管理员都能看（风控留痕是排障的第一手材料），
// 改配置、发布、处置、复核限超级管理员。

const riskForbiddenMsg = "仅超级管理员可管理风控配置"

var riskProxyTransport = &http.Transport{
	Proxy:                 nil,
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   32,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
}

// AdminRiskProxy 转发到 risk-admin
func (h *Handler) AdminRiskProxy(c *gin.Context) {
	if h.risk == nil || !h.risk.Enabled() {
		response.Error(c, http.StatusServiceUnavailable, 50301, "风控平台未接入（未配置 RISK_ETCD_ENDPOINTS）")
		return
	}
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		if _, ok := requireSuperAdminSession(c); !ok {
			response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
			return
		}
	}
	base, err := h.risk.AdminBaseURL(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50302, "风控管理服务不可用："+err.Error())
		return
	}
	target, err := url.Parse(base)
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50302, "风控管理服务地址无效")
		return
	}
	operator := "admin"
	if session, ok := adminAccessSession(c); ok && session != nil {
		name := strings.TrimSpace(session.DisplayName)
		if name == "" {
			name = session.Account
		}
		operator = name
	}
	path := "/v1/" + strings.TrimPrefix(c.Param("path"), "/")
	proxy := &httputil.ReverseProxy{
		Transport: riskProxyTransport,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = path
			r.Out.URL.RawPath = ""
			r.Out.URL.RawQuery = c.Request.URL.RawQuery
			r.Out.Host = target.Host
			// 主服务的登录凭据不往下游带
			r.Out.Header.Del("Authorization")
			r.Out.Header.Del("Cookie")
			r.Out.Header.Set("X-Risk-Token", h.risk.InternalToken())
			r.Out.Header.Set("X-Risk-Operator", url.PathEscape(operator))
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			response.Error(c, http.StatusBadGateway, 50303, "风控管理服务请求失败："+err.Error())
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}
