package httptransport

import (
	"fmt"
	"net/http"
	"strconv"

	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  统一身份
// ════════════════════════════════════════════════════════════

// AdminListIdentities 查询全局身份列表
// GET /api/admin/system/user-master/identities
func (h *Handler) AdminListIdentities(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var q IdentityListQueryParams
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	query := userdomain.IdentityListQuery{
		Keyword:        q.Keyword,
		Status:         q.Status,
		LifecycleState: q.LifecycleState,
		RiskLevel:      q.RiskLevel,
		TagID:          q.TagID,
		Page:           q.Page,
		Limit:          q.Limit,
	}
	items, total, err := h.userMaster.ListGlobalIdentities(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", map[string]any{
		"items": items,
		"total": total,
	})
}

// AdminCreateIdentity 创建全局身份
// POST /api/admin/system/user-master/identities
func (h *Handler) AdminCreateIdentity(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateIdentityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.userMaster.CreateGlobalIdentity(c.Request.Context(), req.Email, req.Phone, req.DisplayName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "identity", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 创建全局身份", session.DisplayName))
	response.Success(c, 200, "创建成功", result)
}

// AdminGetIdentity 获取身份详情（含标签+映射）
// GET /api/admin/system/user-master/identities/:id
func (h *Handler) AdminGetIdentity(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	result, err := h.userMaster.GetGlobalIdentity(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminUpdateIdentityStatus 更新身份状态
// PUT /api/admin/system/user-master/identities/:id/status
func (h *Handler) AdminUpdateIdentityStatus(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	var req UpdateIdentityStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.UpdateIdentityStatus(c.Request.Context(), id, req.Status); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "identity", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 更新身份状态为 %s", session.DisplayName, req.Status))
	response.Success(c, 200, "更新成功", nil)
}

// AdminUpdateIdentityLifecycle 更新身份生命周期
// PUT /api/admin/system/user-master/identities/:id/lifecycle
func (h *Handler) AdminUpdateIdentityLifecycle(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	var req UpdateIdentityLifecycleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.UpdateIdentityLifecycle(c.Request.Context(), id, req.State); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "identity", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 更新身份生命周期为 %s", session.DisplayName, req.State))
	response.Success(c, 200, "更新成功", nil)
}

// AdminUpdateIdentityRisk 更新身份风险评分
// PUT /api/admin/system/user-master/identities/:id/risk
func (h *Handler) AdminUpdateIdentityRisk(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	var req UpdateIdentityRiskRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.UpdateIdentityRisk(c.Request.Context(), id, req.Score, req.Level); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "identity", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 更新身份风险等级为 %s (score=%d)", session.DisplayName, req.Level, req.Score))
	response.Success(c, 200, "更新成功", nil)
}

// ════════════════════════════════════════════════════════════
//  映射
// ════════════════════════════════════════════════════════════

// AdminCreateMapping 创建身份-用户映射
// POST /api/admin/system/user-master/mappings
func (h *Handler) AdminCreateMapping(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateMappingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.CreateIdentityMapping(c.Request.Context(), req.IdentityID, req.AppID, req.UserID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "identity-mapping", fmt.Sprintf("%d", req.IdentityID), fmt.Sprintf("管理员 %s 创建身份映射 identity=%d app=%d user=%d", session.DisplayName, req.IdentityID, req.AppID, req.UserID))
	response.Success(c, 200, "创建成功", nil)
}

// AdminListMappingsByIdentity 查询身份的所有映射
// GET /api/admin/system/user-master/identities/:id/mappings
func (h *Handler) AdminListMappingsByIdentity(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	items, err := h.userMaster.ListMappingsByIdentity(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminDeleteMapping 删除映射
// DELETE /api/admin/system/user-master/mappings/:id
func (h *Handler) AdminDeleteMapping(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的映射 ID")
		return
	}
	if err := h.userMaster.DeleteIdentityMapping(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "identity-mapping", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 删除身份映射", session.DisplayName))
	response.Success(c, 200, "删除成功", nil)
}

// ════════════════════════════════════════════════════════════
//  标签
// ════════════════════════════════════════════════════════════

// AdminListUserTags 列出所有标签
// GET /api/admin/system/user-master/tags
func (h *Handler) AdminListUserTags(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	items, err := h.userMaster.ListUserTags(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateUserTag 创建标签
// POST /api/admin/system/user-master/tags
func (h *Handler) AdminCreateUserTag(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateTagRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := userdomain.CreateTagInput{Name: req.Name, Color: req.Color, Description: req.Description}
	result, err := h.userMaster.CreateUserTag(c.Request.Context(), input, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "user-tag", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 创建标签 %s", session.DisplayName, req.Name))
	response.Success(c, 200, "创建成功", result)
}

// AdminDeleteUserTag 删除标签
// DELETE /api/admin/system/user-master/tags/:id
func (h *Handler) AdminDeleteUserTag(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的标签 ID")
		return
	}
	if err := h.userMaster.DeleteUserTag(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "user-tag", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 删除标签", session.DisplayName))
	response.Success(c, 200, "删除成功", nil)
}

// AdminAssignTag 给身份分配标签
// POST /api/admin/system/user-master/tags/assign
func (h *Handler) AdminAssignTag(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req TagAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.AssignTagToIdentity(c.Request.Context(), req.IdentityID, req.TagID, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "tag-assignment", fmt.Sprintf("%d-%d", req.IdentityID, req.TagID), fmt.Sprintf("管理员 %s 给身份 %d 分配标签 %d", session.DisplayName, req.IdentityID, req.TagID))
	response.Success(c, 200, "分配成功", nil)
}

// AdminRemoveTag 移除身份标签
// POST /api/admin/system/user-master/tags/remove
func (h *Handler) AdminRemoveTag(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req TagAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.RemoveTagFromIdentity(c.Request.Context(), req.IdentityID, req.TagID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "tag-assignment", fmt.Sprintf("%d-%d", req.IdentityID, req.TagID), fmt.Sprintf("管理员 %s 移除身份 %d 的标签 %d", session.DisplayName, req.IdentityID, req.TagID))
	response.Success(c, 200, "移除成功", nil)
}

// AdminListIdentityTags 获取身份的所有标签
// GET /api/admin/system/user-master/identities/:id/tags
func (h *Handler) AdminListIdentityTags(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	items, err := h.userMaster.ListIdentityTags(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// ════════════════════════════════════════════════════════════
//  分群
// ════════════════════════════════════════════════════════════

// AdminListSegments 列出所有分群
// GET /api/admin/system/user-master/segments
func (h *Handler) AdminListSegments(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	items, err := h.userMaster.ListUserSegments(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateSegment 创建分群
// POST /api/admin/system/user-master/segments
func (h *Handler) AdminCreateSegment(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateSegmentRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := userdomain.CreateSegmentInput{Name: req.Name, Description: req.Description, SegmentType: req.SegmentType, Rules: req.Rules}
	result, err := h.userMaster.CreateUserSegment(c.Request.Context(), input, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "user-segment", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 创建分群 %s", session.DisplayName, req.Name))
	response.Success(c, 200, "创建成功", result)
}

// AdminUpdateSegment 更新分群
// PUT /api/admin/system/user-master/segments/:id
func (h *Handler) AdminUpdateSegment(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分群 ID")
		return
	}
	var req UpdateSegmentRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.UpdateUserSegment(c.Request.Context(), id, req.Name, req.Description, req.Rules); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "user-segment", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 更新分群", session.DisplayName))
	response.Success(c, 200, "更新成功", nil)
}

// AdminDeleteSegment 删除分群
// DELETE /api/admin/system/user-master/segments/:id
func (h *Handler) AdminDeleteSegment(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分群 ID")
		return
	}
	if err := h.userMaster.DeleteUserSegment(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "user-segment", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 删除分群", session.DisplayName))
	response.Success(c, 200, "删除成功", nil)
}

// AdminAddSegmentMember 添加分群成员
// POST /api/admin/system/user-master/segments/:id/members
func (h *Handler) AdminAddSegmentMember(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	segmentID, err := pathInt64(c, "id")
	if err != nil || segmentID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分群 ID")
		return
	}
	var req SegmentMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.AddSegmentMember(c.Request.Context(), segmentID, req.IdentityID, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "segment-member", fmt.Sprintf("%d-%d", segmentID, req.IdentityID), fmt.Sprintf("管理员 %s 向分群 %d 添加身份 %d", session.DisplayName, segmentID, req.IdentityID))
	response.Success(c, 200, "添加成功", nil)
}

// AdminRemoveSegmentMember 移除分群成员
// DELETE /api/admin/system/user-master/segments/:id/members/:identityId
func (h *Handler) AdminRemoveSegmentMember(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	segmentID, err := pathInt64(c, "id")
	if err != nil || segmentID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分群 ID")
		return
	}
	identityID, err := strconv.ParseInt(c.Param("identityId"), 10, 64)
	if err != nil || identityID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的身份 ID")
		return
	}
	if err := h.userMaster.RemoveSegmentMember(c.Request.Context(), segmentID, identityID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "segment-member", fmt.Sprintf("%d-%d", segmentID, identityID), fmt.Sprintf("管理员 %s 从分群 %d 移除身份 %d", session.DisplayName, segmentID, identityID))
	response.Success(c, 200, "移除成功", nil)
}

// AdminListSegmentMembers 分页查询分群成员
// GET /api/admin/system/user-master/segments/:id/members
func (h *Handler) AdminListSegmentMembers(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	segmentID, err := pathInt64(c, "id")
	if err != nil || segmentID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分群 ID")
		return
	}
	var q SegmentMemberListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.userMaster.ListSegmentMembers(c.Request.Context(), segmentID, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", map[string]any{
		"items": items,
		"total": total,
	})
}

// ════════════════════════════════════════════════════════════
//  黑白名单
// ════════════════════════════════════════════════════════════

// AdminListUserListEntries 查询名单列表
// GET /api/admin/system/user-master/lists
func (h *Handler) AdminListUserListEntries(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var q ListEntryListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.userMaster.ListUserListEntries(c.Request.Context(), q.ListType, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", map[string]any{
		"items": items,
		"total": total,
	})
}

// AdminCreateUserListEntry 创建名单条目
// POST /api/admin/system/user-master/lists
func (h *Handler) AdminCreateUserListEntry(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateListEntryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := userdomain.CreateListEntryInput{
		ListType:   req.ListType,
		IdentityID: req.IdentityID,
		Email:      req.Email,
		Phone:      req.Phone,
		IP:         req.IP,
		Reason:     req.Reason,
		ExpiresAt:  req.ExpiresAt,
	}
	result, err := h.userMaster.CreateUserListEntry(c.Request.Context(), input, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "user-list", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 创建 %s 条目", session.DisplayName, req.ListType))
	response.Success(c, 200, "创建成功", result)
}

// AdminDeleteUserListEntry 删除名单条目
// DELETE /api/admin/system/user-master/lists/:id
func (h *Handler) AdminDeleteUserListEntry(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的条目 ID")
		return
	}
	if err := h.userMaster.DeleteUserListEntry(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "user-list", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 删除名单条目", session.DisplayName))
	response.Success(c, 200, "删除成功", nil)
}

// AdminCheckBlacklist 检查是否在黑名单中
// POST /api/admin/system/user-master/lists/check
func (h *Handler) AdminCheckBlacklist(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CheckBlacklistRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	blocked, err := h.userMaster.CheckBlacklisted(c.Request.Context(), req.Email, req.Phone, req.IP)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "查询成功", map[string]any{
		"blacklisted": blocked,
	})
}

// ════════════════════════════════════════════════════════════
//  合并
// ════════════════════════════════════════════════════════════

// AdminMergeIdentity 执行身份合并
// POST /api/admin/system/user-master/merges
func (h *Handler) AdminMergeIdentity(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req MergeIdentityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.userMaster.ExecuteIdentityMerge(c.Request.Context(), req.PrimaryID, req.MergedID, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "identity-merge", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 合并身份 %d → %d", session.DisplayName, req.MergedID, req.PrimaryID))
	response.Success(c, 200, "合并成功", result)
}

// AdminListMerges 列出合并记录
// GET /api/admin/system/user-master/merges
func (h *Handler) AdminListMerges(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	items, err := h.userMaster.ListIdentityMerges(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// ════════════════════════════════════════════════════════════
//  申诉
// ════════════════════════════════════════════════════════════

// AdminListAppeals 查询申诉列表
// GET /api/admin/system/user-master/appeals
func (h *Handler) AdminListAppeals(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var q AppealListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.userMaster.ListUserAppeals(c.Request.Context(), q.Status, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", map[string]any{
		"items": items,
		"total": total,
	})
}

// AdminCreateAppeal 创建申诉
// POST /api/admin/system/user-master/appeals
func (h *Handler) AdminCreateAppeal(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateAppealRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := userdomain.CreateAppealInput{AppealType: req.AppealType, Reason: req.Reason, Evidence: req.Evidence}
	result, err := h.userMaster.CreateUserAppeal(c.Request.Context(), req.IdentityID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "user-appeal", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 为身份 %d 创建申诉", session.DisplayName, req.IdentityID))
	response.Success(c, 200, "创建成功", result)
}

// AdminReviewAppeal 审核申诉
// PUT /api/admin/system/user-master/appeals/:id
func (h *Handler) AdminReviewAppeal(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的申诉 ID")
		return
	}
	var req ReviewAppealRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := userdomain.ReviewAppealInput{Action: req.Action, Comment: req.Comment}
	if err := h.userMaster.ReviewUserAppeal(c.Request.Context(), id, session.AdminID, input); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "user-appeal", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 审核申诉: %s", session.DisplayName, req.Action))
	response.Success(c, 200, "审核成功", nil)
}

// ════════════════════════════════════════════════════════════
//  注销
// ════════════════════════════════════════════════════════════

// AdminListDeactivations 列出待处理的注销请求
// GET /api/admin/system/user-master/deactivations
func (h *Handler) AdminListDeactivations(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	items, err := h.userMaster.ListPendingDeactivations(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateDeactivation 创建注销请求
// POST /api/admin/system/user-master/deactivations
func (h *Handler) AdminCreateDeactivation(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req CreateDeactivationRequestDTO
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.userMaster.CreateDeactivationRequest(c.Request.Context(), req.IdentityID, req.Reason, req.CoolingDays)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "deactivation", fmt.Sprintf("%d", result.ID), fmt.Sprintf("管理员 %s 为身份 %d 创建注销请求", session.DisplayName, req.IdentityID))
	response.Success(c, 200, "创建成功", result)
}

// AdminCancelDeactivation 取消注销请求
// POST /api/admin/system/user-master/deactivations/:id/cancel
func (h *Handler) AdminCancelDeactivation(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的注销请求 ID")
		return
	}
	if err := h.userMaster.CancelDeactivation(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "deactivation", fmt.Sprintf("%d", id), fmt.Sprintf("管理员 %s 取消注销请求", session.DisplayName))
	response.Success(c, 200, "取消成功", nil)
}

// ════════════════════════════════════════════════════════════
//  同步
// ════════════════════════════════════════════════════════════

// AdminSyncIdentity 同步单个用户到全局身份
// POST /api/admin/system/user-master/sync
func (h *Handler) AdminSyncIdentity(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req SyncIdentityRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.userMaster.SyncIdentityFromUser(c.Request.Context(), req.AppID, req.UserID); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "identity-sync", fmt.Sprintf("app=%d,user=%d", req.AppID, req.UserID), fmt.Sprintf("管理员 %s 同步用户身份", session.DisplayName))
	response.Success(c, 200, "同步成功", nil)
}

// AdminBatchSyncIdentities 批量同步应用用户到全局身份
// POST /api/admin/system/user-master/sync/batch
func (h *Handler) AdminBatchSyncIdentities(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可访问用户主数据")
		return
	}
	var req BatchSyncRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	synced, err := h.userMaster.BatchSyncIdentities(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "identity-sync-batch", fmt.Sprintf("app=%d", req.AppID), fmt.Sprintf("管理员 %s 批量同步身份，成功 %d 条", session.DisplayName, synced))
	response.Success(c, 200, "批量同步完成", map[string]any{
		"synced": synced,
	})
}
