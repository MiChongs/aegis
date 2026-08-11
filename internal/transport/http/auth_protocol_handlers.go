package httptransport

import (
	"net/http"
	"strings"

	authprotocol "aegis/internal/domain/authprotocol"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// requestOrigin 还原本次请求对外可见的 scheme://host，供自检回访自身。
func requestOrigin(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// 应用接入协议的**管理端**接口。
// 面向接入方的运行时入口在 app_gateway_handlers.go（/api/v1/apps/{appKey}/*）。

func (h *Handler) AdminGetAppAuthProtocol(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	policy, err := h.authProtocol.GetPolicy(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", policy)
}

func (h *Handler) AdminUpdateAppAuthProtocol(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	var patch authprotocol.PolicyPatch
	if err := bind(c, &patch); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	policy, err := h.authProtocol.UpdatePolicy(c.Request.Context(), app.ID, patch)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "接入配置已更新", policy)
}

// AdminRotateAppSigningSecret 轮换应用密钥。明文只在本次响应里出现一次，
// 之后库里只剩密文与提示串——控制台必须提示管理员当场保存。
func (h *Handler) AdminRotateAppSigningSecret(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	secret, policy, err := h.authProtocol.RotateSigningSecret(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusCreated, "应用密钥已轮换", gin.H{
		"appSecret": secret,
		"hint":      policy.SigningSecretHint,
		"rotatedAt": policy.SigningSecretRotatedAt,
		"warning":   "该明文仅此一次可见，请立即保存到接入方的密钥管理中",
	})
}

// AdminAppIntegrationSelfTest 按当前安全等级实跑一遍接入链路，逐步返回结果。
func (h *Handler) AdminAppIntegrationSelfTest(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	if h.authProtocol == nil {
		response.Error(c, http.StatusServiceUnavailable, 50370, "认证协议服务不可用")
		return
	}
	var req AppIntegrationSelfTestRequest
	_ = bind(c, &req)
	// 控制台通常会带上它配置的 API 地址；缺省时回落到本次请求自己的 scheme+host，
	// 这样单机部署不填也能跑。
	if strings.TrimSpace(req.BaseURL) == "" {
		req.BaseURL = requestOrigin(c)
	}
	result, err := h.authProtocol.SelfTest(c.Request.Context(), app.Key, req.BaseURL)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "自检完成", result)
}

func (h *Handler) AdminRotateAppTransportKey(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	key, err := h.authProtocol.RotateTransportKey(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusCreated, "传输密钥已轮换", key)
}

func (h *Handler) AdminListAppTransportKeys(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	keys, err := h.authProtocol.ListTransportKeys(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": keys})
}

func (h *Handler) AdminRevokeAppTransportKey(c *gin.Context) {
	app, ok := h.resolveAdminProtocolApp(c)
	if !ok {
		return
	}
	if err := h.authProtocol.RevokeTransportKey(c.Request.Context(), app.ID, c.Param("keyId")); err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "传输密钥已撤销", gin.H{"revoked": true})
}

type adminProtocolApp struct {
	ID  int64
	Key string
}

func (h *Handler) resolveAdminProtocolApp(c *gin.Context) (*adminProtocolApp, bool) {
	if h.authProtocol == nil || h.app == nil {
		response.Error(c, http.StatusServiceUnavailable, 50370, "认证协议服务不可用")
		return nil, false
	}
	app, err := h.app.GetAppByKey(c.Request.Context(), c.Param("appkey"))
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	if app == nil {
		response.Error(c, http.StatusNotFound, 40470, "应用不存在")
		return nil, false
	}
	return &adminProtocolApp{ID: app.ID, Key: app.AppKey}, true
}
