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
		response.Error(c, http.StatusUnauthorized, 40100, "访问请求未获授权")
		return
	}
	if err := h.realtime.Upgrade(c.Writer, c.Request, session, c.ClientIP(), c.Request.UserAgent()); err != nil {
		h.writeError(c, err)
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
