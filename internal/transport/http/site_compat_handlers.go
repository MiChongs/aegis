package httptransport

// 站点审核的旧命名空间（/api/admin/app/site/*）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminSiteAuditListCompat(c *gin.Context) {
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.AdminList(c.Request.Context(), req.AppID, appdomain.SiteListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(pickPositive(req.Limit, req.PageSize)), Status: req.Status, Keyword: req.Keyword, SortBy: req.SortBy, SortOrder: req.SortOrder})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminSiteAuditCompat(c *gin.Context) {
	var req AdminSiteAuditRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, _ := adminActor(c)
	item, err := h.site.AdminAudit(c.Request.Context(), req.SiteID, req.AppID, adminID, req.Status, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审核成功", item)
}

func (h *Handler) AdminSiteBatchAuditCompat(c *gin.Context) {
	var req AdminSiteBatchAuditRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	success := 0
	failed := 0
	adminID, _ := adminActor(c)
	for _, id := range req.SiteIDs {
		if _, err := h.site.AdminAudit(c.Request.Context(), id, req.AppID, adminID, req.Status, req.Reason); err != nil {
			failed++
		} else {
			success++
		}
	}
	response.Success(c, 200, "批量审核完成", gin.H{"success": success, "failed": failed})
}

func (h *Handler) AdminSiteListCompat(c *gin.Context) { h.AdminSiteAuditListCompat(c) }

func (h *Handler) AdminSiteDetailCompat(c *gin.Context) {
	var req AdminSiteDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminDetail(c.Request.Context(), req.AppID, req.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if item == nil {
		response.Error(c, http.StatusNotFound, 40420, "站点不存在")
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminSiteUpdateCompat(c *gin.Context) {
	var req SiteUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminUpdate(c.Request.Context(), req.AppID, appdomain.SiteMutation{ID: req.ID, AppID: req.AppID, Name: maybeString(req.Name), URL: maybeString(req.URL), Description: maybeString(req.Description), Type: maybeString(req.Type), Header: maybeString(req.Header), Category: maybeString(req.Category)})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminSiteDeleteCompat(c *gin.Context) {
	var req AdminSiteDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.site.AdminDelete(c.Request.Context(), req.AppID, req.ID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminSiteTogglePinCompat(c *gin.Context) {
	var req AdminSiteTogglePinRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminTogglePinned(c.Request.Context(), req.AppID, req.ID, req.IsPinned)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作成功", item)
}

func (h *Handler) AdminSiteUserSitesCompat(c *gin.Context) {
	var req AdminSiteUserRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.AdminUserSites(c.Request.Context(), req.AppID, req.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminSiteAuditStatsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.site.AdminAuditStats(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}
