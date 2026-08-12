package httptransport

// 角色申请审核的旧命名空间。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminRoleApplicationsCompat(c *gin.Context) {
	var req RoleApplicationsQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.AdminList(c.Request.Context(), req.AppID, userdomain.RoleApplicationListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, RequestedRole: req.RequestedRole, Priority: req.Priority, Keyword: req.Keyword, SortBy: req.SortBy, SortOrder: req.SortOrder})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminRoleApplicationDetailCompat(c *gin.Context) {
	var req AdminRoleApplicationDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.roleApp.AdminDetail(c.Request.Context(), req.AppID, req.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminRoleApplicationReviewCompat(c *gin.Context) {
	var req AdminRoleApplicationReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.roleApp.Review(c.Request.Context(), req.AppID, req.ID, adminID, adminName, req.Action, req.ReviewReason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审核成功", item)
}

func (h *Handler) AdminRoleApplicationBatchReviewCompat(c *gin.Context) {
	var req AdminRoleApplicationBatchReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	success := 0
	failed := 0
	adminID, adminName := adminActor(c)
	for _, id := range req.IDs {
		if _, err := h.roleApp.Review(c.Request.Context(), req.AppID, id, adminID, adminName, req.Action, req.ReviewReason); err != nil {
			failed++
		} else {
			success++
		}
	}
	response.Success(c, 200, "批量审核完成", gin.H{"success": success, "failed": failed})
}

func (h *Handler) AdminRoleApplicationStatisticsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.roleApp.Statistics(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}
