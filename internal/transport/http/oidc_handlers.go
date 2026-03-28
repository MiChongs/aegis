package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AdminOIDCPublicConfig 返回 OIDC 是否启用（公开端点，登录页用）
func (h *Handler) AdminOIDCPublicConfig(c *gin.Context) {
	enabled := h.oidcSvc != nil && h.oidcSvc.IsEnabled()
	response.Success(c, 200, "ok", gin.H{"enabled": enabled})
}

// AdminOIDCAuthorize 生成 OIDC 授权 URL
func (h *Handler) AdminOIDCAuthorize(c *gin.Context) {
	url, state, err := h.admin.GetOIDCAuthURL(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"url": url, "state": state})
}

// AdminOIDCCallback 处理 IdP 回调 → 生成 ticket → 302 到前端
func (h *Handler) AdminOIDCCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		errMsg := c.Query("error_description")
		if errMsg == "" {
			errMsg = c.Query("error")
		}
		if errMsg == "" {
			errMsg = "缺少 code 或 state 参数"
		}
		h.oidcRedirectError(c, errMsg)
		return
	}

	result, err := h.admin.HandleOIDCCallback(c.Request.Context(), code, state, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.recordAuditFailed(c, "admin.oidc_login_failed", "admin", "", "OIDC 登录失败: "+err.Error())
		h.oidcRedirectError(c, err.Error())
		return
	}

	// 生成一次性 ticket 存 Redis
	ticket := uuid.NewString()
	payload, _ := json.Marshal(result)
	if err := h.sessions.SetOIDCTicket(c.Request.Context(), ticket, payload, 30*time.Second); err != nil {
		h.oidcRedirectError(c, "内部错误")
		return
	}

	frontendURL := h.oidcFrontendCallbackURL()
	redirectURL := fmt.Sprintf("%s?ticket=%s", frontendURL, ticket)
	if result.RequiresSecondFactor {
		redirectURL += "&mfa=true"
	}
	c.Redirect(http.StatusFound, redirectURL)
}

// AdminOIDCExchange 用一次性 ticket 换取 LoginResult
func (h *Handler) AdminOIDCExchange(c *gin.Context) {
	var req AdminOIDCExchangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	payload, err := h.sessions.GetAndDeleteOIDCTicket(c.Request.Context(), req.Ticket)
	if err != nil || payload == nil {
		response.Error(c, http.StatusUnauthorized, 40197, "ticket 无效或已过期")
		return
	}

	// 直接返回存储的 LoginResult
	var raw json.RawMessage = payload
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": raw})
}

// AdminOIDCTest 测试 OIDC Discovery URL（仅超管）
func (h *Handler) AdminOIDCTest(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可测试 OIDC 连接")
		return
	}
	var req AdminOIDCTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result := h.oidcSvc.TestDiscovery(c.Request.Context(), req.IssuerURL)
	response.Success(c, 200, "OIDC Discovery 测试完成", result)
	h.recordAudit(c, "security.oidc_test", "security", "", "测试 OIDC Discovery: "+req.IssuerURL)
}

func mapOIDCPatch(req AdminOIDCSettingsUpdateRequest) systemdomain.OIDCSettingsPatch {
	patch := systemdomain.OIDCSettingsPatch{
		Enabled: req.Enabled, IssuerURL: req.IssuerURL, ClientID: req.ClientID,
		ClientSecret: req.ClientSecret, RedirectURL: req.RedirectURL,
		Scopes: req.Scopes, AllowedDomains: req.AllowedDomains,
		AdminGroupClaim: req.AdminGroupClaim, AdminGroupValue: req.AdminGroupValue,
		FallbackToLocal: req.FallbackToLocal, FrontendCallbackURL: req.FrontendCallbackURL,
	}
	if req.AttrMapping != nil {
		patch.AttrMapping = &systemdomain.OIDCAttributeMappingPatch{
			Account: req.AttrMapping.Account, DisplayName: req.AttrMapping.DisplayName,
			Email: req.AttrMapping.Email, Phone: req.AttrMapping.Phone,
		}
	}
	return patch
}

func (h *Handler) oidcFrontendCallbackURL() string {
	if h.oidcSvc != nil {
		cfg := h.oidcSvc.CurrentConfig()
		if cfg.FrontendCallbackURL != "" {
			return cfg.FrontendCallbackURL
		}
	}
	return "http://localhost:3000/login/oidc-callback"
}

func (h *Handler) oidcRedirectError(c *gin.Context, msg string) {
	frontendURL := h.oidcFrontendCallbackURL()
	c.Redirect(http.StatusFound, fmt.Sprintf("%s?error=%s", frontendURL, msg))
}
