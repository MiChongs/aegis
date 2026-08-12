package httptransport

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 应用级内容中心的管理端接口：Banner（投放位）与公告。
//
// 与 `/api/admin/system/banners/*`（平台横幅）的分工是作用域，不是功能：
// 那一组没有 appid、只有超管能改、画在控制台总览页；
// 这一组属于某个应用，由该应用的管理员维护，画在客户端里。

/* ───────────────────────── Banner ───────────────────────── */

// AdminBanners GET /api/admin/apps/:appkey/banners
func (h *Handler) AdminBanners(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminBannerListQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.ListBannersForAdmin(c.Request.Context(), appID, bannerFilterFrom(query))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminContentOverview GET /api/admin/apps/:appkey/content/overview
//
// Banner 与公告的计数一次取齐。分两条接口拉会出现「Banner 那栏已刷新、
// 公告那栏还是上一次」的画面，而管理员会照着它做投放决定。
func (h *Handler) AdminContentOverview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	overview, err := h.app.ContentOverview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", overview)
}

// ExportAdminBanners GET /api/admin/apps/:appkey/banners/export
func (h *Handler) ExportAdminBanners(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.app.ListBannersForAdmin(c.Request.Context(), appID, appdomain.BannerFilter{})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_banners_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "header", "title", "content", "url", "type", "position", "status", "start_time", "end_time", "view_count", "click_count", "created_at", "updated_at"})
	for _, item := range items {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			item.Header,
			item.Title,
			item.Content,
			item.URL,
			item.Type,
			strconv.Itoa(item.Position),
			strconv.FormatBool(item.Status),
			formatCSVTime(item.StartTime),
			formatCSVTime(item.EndTime),
			strconv.FormatInt(item.ViewCount, 10),
			strconv.FormatInt(item.ClickCount, 10),
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
}

// CreateAdminBanner POST /api/admin/apps/:appkey/banners
func (h *Handler) CreateAdminBanner(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminBanner(c, appID, 0, req)
}

// UpdateAdminBanner PUT /api/admin/apps/:appkey/banners/:bannerId
func (h *Handler) UpdateAdminBanner(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	bannerID, err := pathInt64(c, "bannerId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	var req AdminBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminBanner(c, appID, bannerID, req)
}

func (h *Handler) saveAdminBanner(c *gin.Context, appID int64, bannerID int64, req AdminBannerUpsertRequest) {
	item, err := h.app.SaveBanner(c.Request.Context(), appID, appdomain.BannerMutation{
		ID:        bannerID,
		Header:    req.Header,
		Title:     req.Title,
		Content:   req.Content,
		URL:       req.URL,
		Type:      req.Type,
		Position:  req.Position,
		Status:    req.Status,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

// ReorderAdminBanners PUT /api/admin/apps/:appkey/banners/order
func (h *Handler) ReorderAdminBanners(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminBannerReorderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.ReorderBanners(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "排序已保存", items)
}

// UploadAdminBannerImage POST /api/admin/apps/:appkey/banners/image（multipart，file 字段）
//
// 升级前这里只有一个 URL 输入框：要放一张图得先自己找地方托管再把链接抄进来，
// 图床挂掉时 Banner 在客户端上就是一块白。
func (h *Handler) UploadAdminBannerImage(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
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

	result, err := h.app.UploadBannerImage(c.Request.Context(), appID, requestBaseURL(c.Request), service.ContentImageUploadInput{
		ConfigName:    strings.TrimSpace(c.PostForm("config_name")),
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploadedBy:    currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", result)
}

// DeleteAdminBanner DELETE /api/admin/apps/:appkey/banners/:bannerId
func (h *Handler) DeleteAdminBanner(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	bannerID, err := pathInt64(c, "bannerId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	if err := h.app.DeleteBanner(c.Request.Context(), appID, bannerID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", gin.H{"id": bannerID})
}

// DeleteAdminBanners DELETE /api/admin/apps/:appkey/banners
func (h *Handler) DeleteAdminBanners(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminBatchIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, ids, err := h.app.DeleteBanners(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量删除成功", gin.H{"deleted": deleted, "ids": ids})
}

/* ───────────────────────── 公告 ───────────────────────── */

// AdminNotices GET /api/admin/apps/:appkey/notices
//
// 返回分页信封而不是裸数组：公告是会持续累积的，一次性全取回来在
// 运营了两年的应用上就是几千条。Banner 那侧刻意保持裸数组（见 BannerFilter）。
func (h *Handler) AdminNotices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminNoticeListQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, limit := normalizePage(query.Page), normalizeLimit(query.Limit)
	items, total, err := h.app.ListNoticesForAdmin(c.Request.Context(), appID, appdomain.NoticeFilter{
		Status:  strings.TrimSpace(query.Status),
		Type:    strings.TrimSpace(query.Type),
		Level:   strings.TrimSpace(query.Level),
		Keyword: strings.TrimSpace(query.Keyword),
		Limit:   limit,
		Offset:  (page - 1) * limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// ExportAdminNotices GET /api/admin/apps/:appkey/notices/export
//
// 导出的是**摘要**而不是正文：正文是 HTML，塞进 CSV 单元格之后表格软件
// 既不会渲染它，也会被里面的逗号与换行撑破格式。
func (h *Handler) ExportAdminNotices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, _, err := h.app.ListNoticesForAdmin(c.Request.Context(), appID, appdomain.NoticeFilter{Limit: 5000})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_notices_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "title", "summary", "type", "level", "status", "pinned", "start_time", "end_time", "published_at", "view_count", "created_at", "updated_at"})
	for _, item := range items {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			item.Title,
			item.Summary,
			item.Type,
			item.Level,
			item.Status,
			strconv.FormatBool(item.Pinned),
			formatCSVTime(item.StartTime),
			formatCSVTime(item.EndTime),
			formatCSVTime(item.PublishedAt),
			strconv.FormatInt(item.ViewCount, 10),
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
}

// CreateAdminNotice POST /api/admin/apps/:appkey/notices
func (h *Handler) CreateAdminNotice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminNoticeUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminNotice(c, appID, 0, req)
}

// UpdateAdminNotice PUT /api/admin/apps/:appkey/notices/:noticeId
func (h *Handler) UpdateAdminNotice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	noticeID, err := pathInt64(c, "noticeId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的公告标识")
		return
	}
	var req AdminNoticeUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminNotice(c, appID, noticeID, req)
}

func (h *Handler) saveAdminNotice(c *gin.Context, appID int64, noticeID int64, req AdminNoticeUpsertRequest) {
	item, err := h.app.SaveNotice(c.Request.Context(), appID, appdomain.NoticeMutation{
		ID:        noticeID,
		Title:     req.Title,
		Content:   req.Content,
		Type:      req.Type,
		Level:     req.Level,
		Status:    req.Status,
		Pinned:    req.Pinned,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
		CreatedBy: currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

// DeleteAdminNotice DELETE /api/admin/apps/:appkey/notices/:noticeId
func (h *Handler) DeleteAdminNotice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	noticeID, err := pathInt64(c, "noticeId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的公告标识")
		return
	}
	if err := h.app.DeleteNotice(c.Request.Context(), appID, noticeID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", gin.H{"id": noticeID})
}

// DeleteAdminNotices DELETE /api/admin/apps/:appkey/notices
func (h *Handler) DeleteAdminNotices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminBatchIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, ids, err := h.app.DeleteNotices(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量删除成功", gin.H{"deleted": deleted, "ids": ids})
}

/* ───────────────────────── 小工具 ───────────────────────── */

func bannerFilterFrom(query AdminBannerListQuery) appdomain.BannerFilter {
	filter := appdomain.BannerFilter{
		Type:    strings.TrimSpace(query.Type),
		Keyword: strings.TrimSpace(query.Keyword),
	}
	switch strings.ToLower(strings.TrimSpace(query.Status)) {
	case "enabled", "true", "1":
		enabled := true
		filter.Status = &enabled
	case "disabled", "false", "0":
		disabled := false
		filter.Status = &disabled
	}
	return filter
}

func formatCSVTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
