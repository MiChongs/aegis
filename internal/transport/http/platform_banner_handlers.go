package httptransport

import (
	"net/http"
	"strings"

	systemdomain "aegis/internal/domain/system"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ─────────────── 展示端（所有已登录管理员可读） ───────────────

// GetActivePlatformBanners 返回当前生效的平台 Banner，供总览页轮播使用。
// GET /api/admin/system/banners/active
func (h *Handler) GetActivePlatformBanners(c *gin.Context) {
	items, err := h.platformBanner.GetActiveBanners(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.platformBanner.ResolveDisplayURLs(c.Request.Context(), requestBaseURL(c.Request), items)
	response.Success(c, http.StatusOK, "获取成功", items)
}

// ─────────────── 管理端（RequireSuperAdmin） ───────────────

// ListPlatformBanners 管理端列表
// GET /api/admin/system/banners
func (h *Handler) ListPlatformBanners(c *gin.Context) {
	var q PlatformBannerListQuery
	_ = c.ShouldBindQuery(&q)
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	filter := systemdomain.PlatformBannerFilter{
		Status:  q.Status,
		Type:    q.Type,
		Keyword: q.Keyword,
		Limit:   q.Limit,
		Offset:  (q.Page - 1) * q.Limit,
	}
	items, total, err := h.platformBanner.ListForAdmin(c.Request.Context(), filter)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.platformBanner.ResolveDisplayURLs(c.Request.Context(), requestBaseURL(c.Request), items)
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"items": items,
		"total": total,
		"page":  q.Page,
		"limit": q.Limit,
	})
}

// GetPlatformBanner 单条详情
// GET /api/admin/system/banners/:id
func (h *Handler) GetPlatformBanner(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	item, err := h.platformBanner.Get(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.platformBanner.ResolveOne(c.Request.Context(), requestBaseURL(c.Request), item)
	response.Success(c, http.StatusOK, "获取成功", item)
}

// CreatePlatformBanner 新建
// POST /api/admin/system/banners
func (h *Handler) CreatePlatformBanner(c *gin.Context) {
	var req PlatformBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.savePlatformBanner(c, 0, req)
}

// UpdatePlatformBanner 更新
// PUT /api/admin/system/banners/:id
func (h *Handler) UpdatePlatformBanner(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	var req PlatformBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.savePlatformBanner(c, id, req)
}

func (h *Handler) savePlatformBanner(c *gin.Context, id int64, req PlatformBannerUpsertRequest) {
	mutation := systemdomain.PlatformBannerMutation{
		ID:          id,
		Title:       req.Title,
		Description: req.Description,
		ImageURL:    req.ImageURL,
		ClickURL:    req.ClickURL,
		Type:        req.Type,
		Position:    req.Position,
		Status:      req.Status,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
	}
	if id == 0 {
		if session, ok := adminAccessSession(c); ok && session != nil {
			creator := session.AdminID
			mutation.CreatedBy = &creator
		}
	}
	item, err := h.platformBanner.Save(c.Request.Context(), mutation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.platformBanner.ResolveOne(c.Request.Context(), requestBaseURL(c.Request), item)
	response.Success(c, http.StatusOK, "保存成功", item)
}

// DeletePlatformBanner 单条删除
// DELETE /api/admin/system/banners/:id
func (h *Handler) DeletePlatformBanner(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	if err := h.platformBanner.Delete(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// UploadPlatformBannerImage 图片上传
// POST /api/admin/system/banners/upload  （multipart file 字段）
// 仅超管可用；走 StorageService 推到对象存储，返回可用 URL。
func (h *Handler) UploadPlatformBannerImage(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()

	var uploaderID *int64
	if session, ok := adminAccessSession(c); ok && session != nil {
		id := session.AdminID
		uploaderID = &id
	}

	result, err := h.platformBanner.UploadImage(c.Request.Context(), requestBaseURL(c.Request), service.PlatformBannerUploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploadedBy:    uploaderID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", result)
}

// BulkDeletePlatformBanners 批量删除
// POST /api/admin/system/banners/bulk-delete
func (h *Handler) BulkDeletePlatformBanners(c *gin.Context) {
	var req PlatformBannerBatchIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.platformBanner.DeleteMany(c.Request.Context(), req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"affected": affected})
}
