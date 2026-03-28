package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	storagedomain "aegis/internal/domain/storage"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  文件管理
// ════════════════════════════════════════════════════════════

// ListStorageObjects 查询存储对象列表
// GET /api/admin/system/storage/objects
func (h *Handler) ListStorageObjects(c *gin.Context) {
	var q ListObjectsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.storageResource.ListStorageObjects(c.Request.Context(), storagedomain.ObjectListQuery{
		ConfigID:    q.ConfigID,
		AppID:       q.AppID,
		Prefix:      q.Prefix,
		ContentType: q.ContentType,
		Status:      q.Status,
		Page:        q.Page,
		Limit:       q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": items, "total": total})
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
