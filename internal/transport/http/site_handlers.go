package httptransport

// 站点（用户提交与自助管理）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) CreateSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Create(c.Request.Context(), session, appdomain.SiteMutation{
		AppID:       req.AppID,
		Name:        &req.Name,
		URL:         &req.URL,
		Description: &req.Description,
		Type:        &req.Type,
		Header:      &req.Header,
		Category:    &req.Category,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功，请等待审核。", item)
}

func (h *Handler) UpdateSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Update(c.Request.Context(), session, appdomain.SiteMutation{
		ID:          req.ID,
		AppID:       req.AppID,
		Name:        maybeString(req.Name),
		URL:         maybeString(req.URL),
		Description: maybeString(req.Description),
		Type:        maybeString(req.Type),
		Header:      maybeString(req.Header),
		Category:    maybeString(req.Category),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功，站点需重新审核", item)
}

func (h *Handler) DeleteSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.site.Delete(c.Request.Context(), session, req.ID, req.AppID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) SiteDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query SiteDetailQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Detail(c.Request.Context(), session, query.ID, query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) SiteList(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query SiteListQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.PublicList(c.Request.Context(), session, query.AppID, appdomain.SiteListQuery{
		Page:      normalizePage(pickPositive(query.Page, query.PageSize)),
		Limit:     normalizeLimit(pickPositive(query.PageSize, query.Limit)),
		Keyword:   query.Keyword,
		SortBy:    query.SortBy,
		SortOrder: query.SortOrder,
		Category:  query.Category,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"data": result.List,
		"pagination": gin.H{
			"currentPage": result.Page,
			"pageSize":    result.Limit,
			"totalCount":  result.Total,
			"totalPages":  result.TotalPages,
			"hasNextPage": result.HasNextPage,
			"hasPrevPage": result.HasPrevPage,
		},
		"cached": result.Cached,
	})
}

func (h *Handler) SearchSites(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.Search(c.Request.Context(), session, req.AppID, req.Keyword, normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"data": result.List, "pagination": gin.H{"currentPage": result.Page, "pageSize": result.Limit, "totalCount": result.Total, "totalPages": result.TotalPages}})
}

func (h *Handler) MySites(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.MySites(c.Request.Context(), session, appdomain.SiteListQuery{
		Page:   normalizePage(req.Page),
		Limit:  normalizeLimit(pickPositive(req.Limit, req.PageSize)),
		Status: req.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ResubmitSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Resubmit(c.Request.Context(), session, req.ID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重新提交成功，请等待审核", item)
}
