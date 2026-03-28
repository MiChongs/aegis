package httptransport

import (
	stderrors "errors"
	"net/http"
	"strconv"
	"strings"

	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) WebSocket(c *gin.Context) {
	if h.realtime == nil {
		response.Error(c, http.StatusServiceUnavailable, 50300, "服务维护中，请稍后再试")
		return
	}
	session, _, err := h.realtime.AuthenticateRequest(c.Request.Context(), c.Request)
	if err != nil {
		var appErr *apperrors.AppError
		if stderrors.As(err, &appErr) {
			response.Error(c, appErr.HTTPStatus, appErr.Code, appErr.Message)
			return
		}
		// 透传真实错误信息（非 AppError 需要暴露原始错误用于排查）
		response.Error(c, http.StatusUnauthorized, 40100, err.Error())
		return
	}
	if err := h.realtime.Upgrade(c.Writer, c.Request, session, c.ClientIP(), c.Request.UserAgent()); err != nil {
		c.Error(err) //nolint:errcheck // gorilla 可能已写入 HTTP 响应
		return
	}
}

func (h *Handler) AdminOnlineStats(c *gin.Context) {
	stats, err := h.realtime.OnlineStats(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminAppOnlineStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.realtime.AppOnlineStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminAppOnlineUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	page := parsePositiveInt(c.Query("page"), 1)
	limit := parsePositiveInt(c.Query("limit"), 20)
	items, err := h.realtime.ListAppOnlineUsers(c.Request.Context(), appID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func parsePositiveInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
