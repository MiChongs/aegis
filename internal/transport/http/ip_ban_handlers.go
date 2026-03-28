package httptransport

import (
	"net/http"
	"strconv"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AdminListIPBans 查询 IP 封禁列表
// GET /api/admin/system/firewall/bans
func (h *Handler) AdminListIPBans(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	var req IPBanListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	filter := firewalldomain.IPBanFilter{
		IP:       req.IP,
		Status:   req.Status,
		Source:   req.Source,
		Page:     req.Page,
		PageSize: req.PageSize,
	}
	page, err := h.ipBan.ListBans(c.Request.Context(), filter)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", page)
}

// AdminBanIP 手动封禁 IP
// POST /api/admin/system/firewall/bans
func (h *Handler) AdminBanIP(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	var req IPBanCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	ban, err := h.ipBan.BanIP(c.Request.Context(), req.IP, req.Reason, req.Duration, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "封禁成功", ban)
}

// AdminUnbanIP 手动解封 IP
// DELETE /api/admin/system/firewall/bans/:banId
func (h *Handler) AdminUnbanIP(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 封禁")
		return
	}
	banID, err := strconv.ParseInt(c.Param("banId"), 10, 64)
	if err != nil || banID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的封禁记录 ID")
		return
	}
	if err := h.ipBan.UnbanIP(c.Request.Context(), banID, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "解封成功", map[string]any{
		"banId":     banID,
		"revokedAt": time.Now().UTC(),
	})
}
