package httptransport

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"
)

// 一起听（docs/listen-together.md）。房间与长连接在独立的一起听微服务里，
// 主服务这里只有签票、发布票据公钥和管理端转发。

// AppListenTogetherTicket POST /api/v1/apps/{appkey}/listen-together/ticket
//
// 已登录且拥有 listen_together 权益时签一张短期票据，并给出一起听服务的连接地址。
// 没有权益返回 40230（HTTP 402），客户端据此引导开通会员；服务未开通返回 50360。
func (h *Handler) AppListenTogetherTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.listenTogether == nil {
		response.Error(c, http.StatusServiceUnavailable, 50360, "一起听服务尚未开通")
		return
	}
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	deviceID := strings.TrimSpace(session.DeviceID)
	if deviceID == "" {
		deviceID = strings.TrimSpace(c.GetHeader("X-Device-Id"))
	}
	ticket, err := h.listenTogether.IssueTicket(c.Request.Context(), app, session, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "签发一起听票据成功", ticket)
}

// ListenTogetherJWKS GET /api/listen-together/jwks.json
//
// 票据公钥（RFC 7517 原样格式，不套响应信封：验签库按标准格式读）。公开、可缓存。
func (h *Handler) ListenTogetherJWKS(c *gin.Context) {
	if h.listenTogether == nil {
		c.JSON(http.StatusNotFound, gin.H{"keys": []any{}})
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.listenTogether.JWKS())
}

func (h *Handler) listenTogetherAdminApp(c *gin.Context) (*appdomain.App, bool) {
	if h.listenTogether == nil {
		response.Error(c, http.StatusServiceUnavailable, 50360, "一起听服务尚未开通")
		return nil, false
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return nil, false
	}
	app, err := h.app.GetApp(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	return app, true
}

// AdminListenTogetherStats GET /api/admin/apps/:appkey/listen-together/stats
func (h *Handler) AdminListenTogetherStats(c *gin.Context) {
	app, ok := h.listenTogetherAdminApp(c)
	if !ok {
		return
	}
	stats, err := h.listenTogether.Stats(c.Request.Context(), app.AppKey)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取一起听统计成功", stats)
}

// AdminListenTogetherRooms GET /api/admin/apps/:appkey/listen-together/rooms?page=&pageSize=
func (h *Handler) AdminListenTogetherRooms(c *gin.Context) {
	app, ok := h.listenTogetherAdminApp(c)
	if !ok {
		return
	}
	page, err := h.listenTogether.ListRooms(c.Request.Context(), app, queryInt(c, "page", 1), queryInt(c, "pageSize", 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取一起听房间成功", page)
}

// AdminListenTogetherRoom GET /api/admin/apps/:appkey/listen-together/rooms/:roomId
func (h *Handler) AdminListenTogetherRoom(c *gin.Context) {
	app, ok := h.listenTogetherAdminApp(c)
	if !ok {
		return
	}
	room, err := h.listenTogether.GetRoom(c.Request.Context(), app, c.Param("roomId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取一起听房间详情成功", room)
}

// AdminListenTogetherCloseRequest 解散房间的请求体。
type AdminListenTogetherCloseRequest struct {
	Reason string `json:"reason"`
}

// AdminListenTogetherCloseRoom POST /api/admin/apps/:appkey/listen-together/rooms/:roomId/close
func (h *Handler) AdminListenTogetherCloseRoom(c *gin.Context) {
	app, ok := h.listenTogetherAdminApp(c)
	if !ok {
		return
	}
	var req AdminListenTogetherCloseRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.listenTogether.CloseRoom(c.Request.Context(), app, c.Param("roomId"), strings.TrimSpace(req.Reason)); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "房间已解散", nil)
}

// AdminListenTogetherKickRequest 移出成员的请求体（userId 为应用内用户 ID）。
type AdminListenTogetherKickRequest struct {
	UserID int64 `json:"userId" binding:"required"`
}

// AdminListenTogetherKick POST /api/admin/apps/:appkey/listen-together/rooms/:roomId/kick
func (h *Handler) AdminListenTogetherKick(c *gin.Context) {
	app, ok := h.listenTogetherAdminApp(c)
	if !ok {
		return
	}
	var req AdminListenTogetherKickRequest
	if err := bind(c, &req); err != nil || req.UserID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "请指定要移出的用户")
		return
	}
	if err := h.listenTogether.KickMember(c.Request.Context(), app, c.Param("roomId"), req.UserID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已移出房间", nil)
}
