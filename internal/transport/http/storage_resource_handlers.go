package httptransport

import (
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  文件管理
// ════════════════════════════════════════════════════════════

// ListStorageObjects 查询存储对象列表（同时返回子目录与筛选汇总）
// GET /api/admin/system/storage/objects
func (h *Handler) ListStorageObjects(c *gin.Context) {
	query, ok := bindObjectListQuery(c)
	if !ok {
		return
	}
	result, err := h.storageResource.BrowseStorageObjects(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// bindObjectListQuery 把查询串解析成领域查询对象。
// 时间字段单独在这里解析：交给 gin 的 time_format 绑定只支持一种固定格式，
// 而控制台的日期选择器发 RFC3339、手工调接口的人常发 `2026-08-12`。
func bindObjectListQuery(c *gin.Context) (storagedomain.ObjectListQuery, bool) {
	var q ListObjectsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return storagedomain.ObjectListQuery{}, false
	}
	createdFrom, err := parseFlexibleTime(q.CreatedFrom, false)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdFrom 时间格式无效")
		return storagedomain.ObjectListQuery{}, false
	}
	createdTo, err := parseFlexibleTime(q.CreatedTo, true)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdTo 时间格式无效")
		return storagedomain.ObjectListQuery{}, false
	}
	return storagedomain.ObjectListQuery{
		ConfigID:     q.ConfigID,
		AppID:        q.AppID,
		Prefix:       q.Prefix,
		Folder:       q.Folder,
		FolderView:   q.FolderView,
		Keyword:      q.Keyword,
		ContentType:  q.ContentType,
		Status:       q.Status,
		Statuses:     q.Statuses,
		UploaderType: q.UploaderType,
		UploadedBy:   q.UploadedBy,
		MinSize:      q.MinSize,
		MaxSize:      q.MaxSize,
		CreatedFrom:  createdFrom,
		CreatedTo:    createdTo,
		Sort:         q.Sort,
		Order:        q.Order,
		Page:         q.Page,
		Limit:        q.Limit,
	}, true
}

// parseFlexibleTime 解析 RFC3339 或纯日期。
// endOfDay 为真时把纯日期补成当天 23:59:59 —— 否则「截止到 8 月 12 日」
// 会解析成 8 月 12 日 00:00:00，把那一整天的文件全部排除在外。
func parseFlexibleTime(value string, endOfDay bool) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return nil, err
	}
	if endOfDay {
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	return &parsed, nil
}

// BatchMutateStorageObjects 批量移入回收站 / 恢复 / 永久删除
// POST /api/admin/system/storage/objects/batch
func (h *Handler) BatchMutateStorageObjects(c *gin.Context) {
	var req BatchObjectsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 永久删除与单条接口保持同一道闸：批量入口如果宽松一格，
	// 那道闸就等于不存在。
	if req.Action == storagedomain.BatchActionPurge {
		if _, ok := requireSuperAdminSession(c); !ok {
			return
		}
	}
	result, err := h.storageResource.BatchMutateObjects(c.Request.Context(), req.Action, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	messages := map[string]string{
		storagedomain.BatchActionDelete:  "已移入回收站",
		storagedomain.BatchActionRestore: "已恢复",
		storagedomain.BatchActionPurge:   "已永久删除",
	}
	response.Success(c, 200, messages[req.Action], result)
	h.recordAudit(c, "storage.object.batch_"+req.Action, "storage_object", "",
		fmt.Sprintf("批量%s存储对象：请求 %d 个，实际影响 %d 个", messages[req.Action], result.Requested, result.Affected))
}

// CreateStorageObjectLink 为已索引对象签发访问链接（预览 / 下载）
// POST /api/admin/system/storage/objects/:objectId/link
func (h *Handler) CreateStorageObjectLink(c *gin.Context) {
	obj, ok := h.requireStorageObject(c)
	if !ok {
		return
	}
	var req ObjectAccessLinkRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	appID := int64(0)
	if obj.AppID != nil {
		appID = *obj.AppID
	}
	fileName := obj.FileName
	if fileName == "" {
		fileName = path.Base(obj.ObjectKey)
	}
	result, ticketID, err := h.storage.CreateIndexedObjectLink(c.Request.Context(), appID, obj.ConfigID,
		obj.ObjectKey, req.Download, fileName, time.Duration(req.ExpiresIn)*time.Second)
	if err != nil {
		h.writeError(c, err)
		return
	}
	url := result.URL
	if ticketID != "" {
		url = proxyURLFromRequest(c.Request, ticketID)
	}
	response.Success(c, 200, "ok", storagedomain.ObjectAccessLink{
		ObjectID:    obj.ID,
		ConfigID:    obj.ConfigID,
		Provider:    result.Provider,
		ObjectKey:   obj.ObjectKey,
		URL:         url,
		Download:    req.Download,
		ContentType: obj.ContentType,
		ExpiresAt:   result.ExpiresAt,
	})
	if req.Download {
		// 只有下载留痕。预览会随列表滚动大量触发，全记会把审计日志淹掉，
		// 那时真正需要追责的下载记录反而找不出来。
		h.recordAudit(c, "storage.object.download", "storage_object", strconv.FormatInt(obj.ID, 10),
			"下载存储对象 "+obj.ObjectKey)
	}
}

// GetStorageObjectThumbnail 输出存储对象的缩略图
// GET /api/admin/system/storage/objects/:objectId/thumbnail
func (h *Handler) GetStorageObjectThumbnail(c *gin.Context) {
	obj, ok := h.requireStorageObject(c)
	if !ok {
		return
	}
	if !service.CanRenderThumbnail(obj) {
		response.Error(c, http.StatusUnsupportedMediaType, 41580, "该对象不支持生成缩略图")
		return
	}
	width, _ := strconv.Atoi(c.DefaultQuery("w", "192"))
	thumb, err := h.storage.RenderThumbnail(c.Request.Context(), obj.ConfigID, obj.ObjectKey, obj.ETag, width)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 缩略图由 (对象内容, 宽度) 唯一决定，ETag 命中就不必再传一遍字节
	if match := strings.TrimSpace(c.GetHeader("If-None-Match")); match != "" && match == thumb.ETag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Header("ETag", thumb.ETag)
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, thumb.ContentType, thumb.Data)
}

// requireStorageObject 取出路径上的对象，不存在即 404
func (h *Handler) requireStorageObject(c *gin.Context) (*storagedomain.StorageObject, bool) {
	id, err := pathInt64(c, "objectId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的对象 ID")
		return nil, false
	}
	obj, err := h.storageResource.GetStorageObject(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	if obj == nil {
		response.Error(c, http.StatusNotFound, 40400, "对象不存在")
		return nil, false
	}
	return obj, true
}

// GetStorageObjectDetail 获取存储对象详情
// GET /api/admin/system/storage/objects/:objectId
func (h *Handler) GetStorageObjectDetail(c *gin.Context) {
	id, err := pathInt64(c, "objectId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的对象 ID")
		return
	}
	obj, err := h.storageResource.GetStorageObject(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if obj == nil {
		response.Error(c, http.StatusNotFound, 40400, "对象不存在")
		return
	}
	response.Success(c, 200, "ok", obj)
}

// SoftDeleteStorageObject 软删除存储对象（移入回收站）
// DELETE /api/admin/system/storage/objects/:objectId
func (h *Handler) SoftDeleteStorageObject(c *gin.Context) {
	id, err := pathInt64(c, "objectId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的对象 ID")
		return
	}
	if err := h.storageResource.SoftDeleteStorageObject(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "对象已移入回收站", nil)
	h.recordAudit(c, "storage.object.soft_delete", "storage_object", strconv.FormatInt(id, 10), fmt.Sprintf("软删除存储对象 #%d", id))
}

// RestoreStorageObject 恢复已软删除的对象
// POST /api/admin/system/storage/objects/:objectId/restore
func (h *Handler) RestoreStorageObject(c *gin.Context) {
	id, err := pathInt64(c, "objectId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的对象 ID")
		return
	}
	if err := h.storageResource.RestoreStorageObject(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "对象已恢复", nil)
	h.recordAudit(c, "storage.object.restore", "storage_object", strconv.FormatInt(id, 10), fmt.Sprintf("恢复存储对象 #%d", id))
}

// PermanentDeleteStorageObject 永久删除存储对象
// DELETE /api/admin/system/storage/objects/:objectId/permanent
func (h *Handler) PermanentDeleteStorageObject(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "objectId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的对象 ID")
		return
	}
	if err := h.storageResource.PermanentDeleteStorageObject(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "对象已永久删除", nil)
	h.recordAudit(c, "storage.object.permanent_delete", "storage_object", strconv.FormatInt(id, 10), fmt.Sprintf("永久删除存储对象 #%d", id))
}

// ListTrashObjects 查询回收站对象
// GET /api/admin/system/storage/trash
func (h *Handler) ListTrashObjects(c *gin.Context) {
	var configID *int64
	if v := c.Query("configId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
			return
		}
		configID = &id
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	items, total, err := h.storageResource.ListDeletedObjects(c.Request.Context(), configID, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": items, "total": total})
}

// CleanupTrash 清理回收站（超管专用）
// POST /api/admin/system/storage/trash/cleanup
func (h *Handler) CleanupTrash(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var req CleanupTrashRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	days := req.OlderThanDays
	if days <= 0 {
		days = 30
	}
	count, err := h.storageResource.CleanupDeletedObjects(c.Request.Context(), time.Duration(days)*24*time.Hour)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "回收站已清理", gin.H{"deleted": count})
	h.recordAudit(c, "storage.trash.cleanup", "storage", "", fmt.Sprintf("清理回收站（>%d天），删除 %d 个对象", days, count))
}

// ════════════════════════════════════════════════════════════
//  规则管理
// ════════════════════════════════════════════════════════════

// ListStorageRules 查询存储规则
// GET /api/admin/system/storage/rules
func (h *Handler) ListStorageRules(c *gin.Context) {
	var configID, appID *int64
	if v := c.Query("configId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
			return
		}
		configID = &id
	}
	if v := c.Query("appId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的应用 ID")
			return
		}
		appID = &id
	}
	items, err := h.storageResource.ListStorageRules(c.Request.Context(), configID, appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// CreateStorageRule 创建存储规则
// POST /api/admin/system/storage/rules
func (h *Handler) CreateStorageRule(c *gin.Context) {
	var req CreateRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rule, err := h.storageResource.CreateStorageRule(c.Request.Context(), storagedomain.CreateRuleInput{
		ConfigID: req.ConfigID,
		AppID:    req.AppID,
		Name:     req.Name,
		RuleType: req.RuleType,
		RuleData: req.RuleData,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "规则已创建", rule)
	h.recordAudit(c, "storage.rule.create", "storage_rule", strconv.FormatInt(rule.ID, 10), "创建存储规则 "+req.Name)
}

// UpdateStorageRule 更新存储规则
// PUT /api/admin/system/storage/rules/:ruleId
func (h *Handler) UpdateStorageRule(c *gin.Context) {
	ruleID, err := pathInt64(c, "ruleId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	var req UpdateRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 合并字段：未提供时使用空/默认值
	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	var ruleData map[string]any
	if req.RuleData != nil {
		ruleData = *req.RuleData
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if err := h.storageResource.UpdateStorageRule(c.Request.Context(), ruleID, name, ruleData, isActive); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "规则已更新", nil)
	h.recordAudit(c, "storage.rule.update", "storage_rule", strconv.FormatInt(ruleID, 10), fmt.Sprintf("更新存储规则 #%d", ruleID))
}

// DeleteStorageRule 删除存储规则
// DELETE /api/admin/system/storage/rules/:ruleId
func (h *Handler) DeleteStorageRule(c *gin.Context) {
	ruleID, err := pathInt64(c, "ruleId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	if err := h.storageResource.DeleteStorageRule(c.Request.Context(), ruleID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "规则已删除", nil)
	h.recordAudit(c, "storage.rule.delete", "storage_rule", strconv.FormatInt(ruleID, 10), fmt.Sprintf("删除存储规则 #%d", ruleID))
}

// ════════════════════════════════════════════════════════════
//  CDN 配置
// ════════════════════════════════════════════════════════════

// GetCDNConfig 获取 CDN 配置
// GET /api/admin/system/storage/cdn/:configId
func (h *Handler) GetCDNConfig(c *gin.Context) {
	configID, err := pathInt64(c, "configId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
		return
	}
	cdn, err := h.storageResource.GetCDNConfig(c.Request.Context(), configID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if cdn == nil {
		response.Error(c, http.StatusNotFound, 40400, "CDN 配置不存在")
		return
	}
	response.Success(c, 200, "ok", cdn)
}

// UpsertCDNConfig 创建或更新 CDN 配置
// PUT /api/admin/system/storage/cdn/:configId
func (h *Handler) UpsertCDNConfig(c *gin.Context) {
	configID, err := pathInt64(c, "configId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
		return
	}
	var req UpsertCDNConfigRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cdn, err := h.storageResource.UpsertCDNConfig(c.Request.Context(), configID, storagedomain.UpsertCDNConfigInput{
		CDNDomain:        req.CDNDomain,
		CDNProtocol:      req.CDNProtocol,
		CacheMaxAge:      req.CacheMaxAge,
		RefererWhitelist: req.RefererWhitelist,
		RefererBlacklist: req.RefererBlacklist,
		IPWhitelist:      req.IPWhitelist,
		SignURLEnabled:   req.SignURLEnabled,
		SignURLSecret:    req.SignURLSecret,
		SignURLTTL:       req.SignURLTTL,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "CDN 配置已更新", cdn)
	h.recordAudit(c, "storage.cdn.upsert", "storage_cdn", strconv.FormatInt(configID, 10), fmt.Sprintf("更新存储配置 #%d 的 CDN 配置", configID))
}

// DeleteCDNConfig 删除 CDN 配置
// DELETE /api/admin/system/storage/cdn/:configId
func (h *Handler) DeleteCDNConfig(c *gin.Context) {
	configID, err := pathInt64(c, "configId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
		return
	}
	if err := h.storageResource.DeleteCDNConfig(c.Request.Context(), configID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "CDN 配置已删除", nil)
	h.recordAudit(c, "storage.cdn.delete", "storage_cdn", strconv.FormatInt(configID, 10), fmt.Sprintf("删除存储配置 #%d 的 CDN 配置", configID))
}

// ════════════════════════════════════════════════════════════
//  图片规则
// ════════════════════════════════════════════════════════════

// ListImageRules 查询图片处理规则
// GET /api/admin/system/storage/image-rules
func (h *Handler) ListImageRules(c *gin.Context) {
	var configID *int64
	if v := c.Query("configId"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
			return
		}
		configID = &id
	}
	items, err := h.storageResource.ListImageRules(c.Request.Context(), configID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// CreateImageRule 创建图片处理规则
// POST /api/admin/system/storage/image-rules
func (h *Handler) CreateImageRule(c *gin.Context) {
	var req CreateImageRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rule, err := h.storageResource.CreateImageRule(c.Request.Context(), storagedomain.CreateImageRuleInput{
		ConfigID: req.ConfigID,
		Name:     req.Name,
		RuleType: req.RuleType,
		RuleData: req.RuleData,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "图片规则已创建", rule)
	h.recordAudit(c, "storage.image_rule.create", "storage_image_rule", strconv.FormatInt(rule.ID, 10), "创建图片规则 "+req.Name)
}

// DeleteImageRule 删除图片处理规则
// DELETE /api/admin/system/storage/image-rules/:ruleId
func (h *Handler) DeleteImageRule(c *gin.Context) {
	ruleID, err := pathInt64(c, "ruleId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	if err := h.storageResource.DeleteImageRule(c.Request.Context(), ruleID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "图片规则已删除", nil)
	h.recordAudit(c, "storage.image_rule.delete", "storage_image_rule", strconv.FormatInt(ruleID, 10), fmt.Sprintf("删除图片规则 #%d", ruleID))
}

// ════════════════════════════════════════════════════════════
//  用量统计
// ════════════════════════════════════════════════════════════

// GetStorageUsage 获取存储用量统计
// GET /api/admin/system/storage/usage
func (h *Handler) GetStorageUsage(c *gin.Context) {
	var configID int64
	if s := c.Query("configId"); s != "" {
		var err error
		configID, err = strconv.ParseInt(s, 10, 64)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
			return
		}
	}
	// configID=0 时返回全局汇总
	stats, err := h.storageResource.GetUsageStats(c.Request.Context(), configID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", stats)
}

// GetStorageUsageHistory 获取存储用量历史
// GET /api/admin/system/storage/usage/history
func (h *Handler) GetStorageUsageHistory(c *gin.Context) {
	configIDStr := c.Query("configId")
	if configIDStr == "" {
		response.Error(c, http.StatusBadRequest, 40000, "configId 参数必填")
		return
	}
	configID, err := strconv.ParseInt(configIDStr, 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的配置 ID")
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	if days < 1 {
		days = 30
	}
	if days > 365 {
		days = 365
	}
	items, err := h.storageResource.GetUsageHistory(c.Request.Context(), configID, days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}
