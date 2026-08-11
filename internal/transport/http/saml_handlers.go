package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *Handler) AdminSAMLPublicConfig(c *gin.Context) {
	enabled := h.samlSvc != nil && h.samlSvc.IsEnabled()
	payload := gin.H{"enabled": enabled}
	if h.samlSvc != nil {
		cfg := h.samlSvc.CurrentConfig()
		payload["metadataURL"] = cfg.MetadataURL
	}
	response.Success(c, 200, "ok", payload)
}

func (h *Handler) AdminSAMLAuthorize(c *gin.Context) {
	urlValue, relayState, err := h.admin.GetSAMLAuthURL(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"url": urlValue, "state": relayState})
}

func (h *Handler) AdminSAMLMetadata(c *gin.Context) {
	if h.samlSvc == nil {
		response.Error(c, http.StatusBadRequest, 40098, "SAML 服务未初始化")
		return
	}
	data, err := h.samlSvc.MetadataXML(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Content-Type", "application/samlmetadata+xml; charset=utf-8")
	c.String(http.StatusOK, string(data))
}

func (h *Handler) AdminSAMLCallback(c *gin.Context) {
	if errMsg := c.Request.FormValue("error"); errMsg != "" {
		h.recordAuditAuth(c, AuthAuditParams{
			Provider: "saml",
			Event:    "login",
			Status:   systemdomain.AuditStatusFailed,
			Reason:   errMsg,
		})
		h.samlRedirectError(c, errMsg)
		return
	}
	result, err := h.admin.HandleSAMLCallback(c.Request.Context(), c.Request, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.recordAuditAuth(c, AuthAuditParams{
			Provider: "saml",
			Event:    "login",
			Status:   systemdomain.AuditStatusFailed,
			Reason:   err.Error(),
		})
		h.samlRedirectError(c, err.Error())
		return
	}

	ticket := uuid.NewString()
	payload, _ := json.Marshal(result)
	if err := h.sessions.SetSAMLTicket(c.Request.Context(), ticket, payload, 30*time.Second); err != nil {
		h.recordAuditAuth(c, AuthAuditParams{
			AdminID:   result.Admin.ID,
			AdminName: result.Admin.Account,
			Provider:  "saml",
			Event:     "login",
			Status:    systemdomain.AuditStatusFailed,
			Reason:    "ticket 存储失败",
		})
		h.samlRedirectError(c, "内部错误")
		return
	}

	redirectURL := fmt.Sprintf("%s?ticket=%s", h.samlFrontendCallbackURL(), url.QueryEscape(ticket))
	if result.RequiresSecondFactor {
		redirectURL += "&mfa=true"
	}
	c.Redirect(http.StatusFound, redirectURL)

	// 在 SAML 实际认证完成的位置记录成功审计 —— exchange 只是前端取 ticket，不是认证点
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    "saml",
		Event:       "login",
		Status:      systemdomain.AuditStatusSuccess,
		MFARequired: result.RequiresSecondFactor,
	})
}

func (h *Handler) AdminSAMLExchange(c *gin.Context) {
	var req AdminSAMLExchangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	payload, err := h.sessions.GetAndDeleteSAMLTicket(c.Request.Context(), req.Ticket)
	if err != nil || payload == nil {
		response.Error(c, http.StatusUnauthorized, 40199, "ticket 无效或已过期")
		return
	}
	var raw json.RawMessage = payload
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": raw})
}

func (h *Handler) samlFrontendCallbackURL() string {
	if h.samlSvc != nil {
		cfg := h.samlSvc.CurrentConfig()
		if cfg.FrontendCallbackURL != "" {
			return cfg.FrontendCallbackURL
		}
	}
	return "http://localhost:3000/login/saml-callback"
}

func (h *Handler) samlRedirectError(c *gin.Context, msg string) {
	c.Redirect(http.StatusFound, fmt.Sprintf("%s?error=%s", h.samlFrontendCallbackURL(), url.QueryEscape(msg)))
}
