package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ListAllSessions GET /api/admin/system/sessions — 分页列出所有活跃管理员会话
func (h *Handler) ListAllSessions(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	items, total, err := h.sessionMgmt.ListAllSessions(c.Request.Context(), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"items": items, "total": total, "page": page, "limit": limit})
}

// ListAdminSessions GET /api/admin/system/admins/:adminId/sessions — 列出指定管理员会话
func (h *Handler) ListAdminSessions(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	adminID, err := strconv.ParseInt(c.Param("adminId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	items, err := h.sessionMgmt.ListAdminSessions(c.Request.Context(), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// RevokeSession POST /api/admin/system/sessions/:sessionId/revoke — 撤销指定会话
func (h *Handler) RevokeSession(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少会话 ID")
		return
	}
	if err := h.sessionMgmt.ForceLogout(c.Request.Context(), sessionID, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", gin.H{"sessionId": sessionID})
	h.recordAudit(c, "session.revoke", "admin_session", sessionID, fmt.Sprintf("撤销管理员会话 %s", sessionID))
}

// ForceLogoutAdmin POST /api/admin/system/admins/:adminId/force-logout — 强制踢出管理员所有会话
func (h *Handler) ForceLogoutAdmin(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	adminID, err := strconv.ParseInt(c.Param("adminId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	count, err := h.sessionMgmt.ForceLogoutAll(c.Request.Context(), adminID, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "强制登出成功", gin.H{"adminId": adminID, "revokedCount": count})
	h.recordAudit(c, "session.force_logout", "admin", strconv.FormatInt(adminID, 10), fmt.Sprintf("强制登出管理员 #%d 的 %d 个会话", adminID, count))
}

// ListOnlineAdmins GET /api/admin/system/admins/online — 列出当前在线管理员
func (h *Handler) ListOnlineAdmins(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	items, err := h.sessionMgmt.ListOnlineAdmins(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// ListTempPermissions GET /api/admin/system/temp-permissions — 列出临时权限
func (h *Handler) ListTempPermissions(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	var adminID *int64
	if v := c.Query("adminId"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			adminID = &id
		}
	}
	items, err := h.sessionMgmt.ListTempPermissions(c.Request.Context(), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// GrantTempPermission POST /api/admin/system/temp-permissions — 授予临时权限
func (h *Handler) GrantTempPermission(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	var req GrantTempPermRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "expiresAt 格式无效，需要 RFC3339")
		return
	}
	if expiresAt.Before(time.Now()) {
		response.Error(c, http.StatusBadRequest, 40000, "过期时间不能早于当前时间")
		return
	}
	tp, err := h.sessionMgmt.GrantTempPermission(c.Request.Context(), req.AdminID, req.Permission, req.AppID, session.AdminID, req.Reason, expiresAt)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "临时权限已授予", tp)
	h.recordAudit(c, "temp_perm.grant", "admin_temp_permission", strconv.FormatInt(tp.ID, 10),
		fmt.Sprintf("为管理员 #%d 授予临时权限 %s", req.AdminID, req.Permission))
}

// RevokeTempPermission POST /api/admin/system/temp-permissions/:permId/revoke — 撤销临时权限
func (h *Handler) RevokeTempPermission(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	permID, err := strconv.ParseInt(c.Param("permId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的权限 ID")
		return
	}
	if err := h.sessionMgmt.RevokeTempPermission(c.Request.Context(), permID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "临时权限已撤销", gin.H{"id": permID})
	h.recordAudit(c, "temp_perm.revoke", "admin_temp_permission", strconv.FormatInt(permID, 10),
		fmt.Sprintf("撤销临时权限 #%d", permID))
}

// ListDelegations GET /api/admin/system/delegations — 列出代理授权
func (h *Handler) ListDelegations(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	adminIDStr := c.Query("adminId")
	role := c.DefaultQuery("role", "delegator")
	if role != "delegator" && role != "delegate" {
		role = "delegator"
	}
	adminID, err := strconv.ParseInt(adminIDStr, 10, 64)
	if err != nil || adminID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "缺少有效的 adminId 参数")
		return
	}
	items, err := h.sessionMgmt.ListDelegations(c.Request.Context(), adminID, role)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// CreateDelegation POST /api/admin/system/delegations — 创建代理授权
func (h *Handler) CreateDelegation(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	var req CreateDelegationRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	expiresAt, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "expiresAt 格式无效，需要 RFC3339")
		return
	}
	if expiresAt.Before(time.Now()) {
		response.Error(c, http.StatusBadRequest, 40000, "过期时间不能早于当前时间")
		return
	}
	delegation := admindomain.AdminDelegation{
		DelegatorID: req.DelegatorID,
		DelegateID:  req.DelegateID,
		Scope:       req.Scope,
		ScopeID:     req.ScopeID,
		GrantedBy:   session.AdminID,
		Reason:      req.Reason,
		ExpiresAt:   expiresAt,
	}
	result, err := h.sessionMgmt.CreateDelegation(c.Request.Context(), delegation)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "代理授权已创建", result)
	h.recordAudit(c, "delegation.create", "admin_delegation", strconv.FormatInt(result.ID, 10),
		fmt.Sprintf("创建代理授权：管理员 #%d → #%d，范围 %s", req.DelegatorID, req.DelegateID, req.Scope))
}

// RevokeDelegation POST /api/admin/system/delegations/:delegationId/revoke — 撤销代理授权
func (h *Handler) RevokeDelegation(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40311, "需要超级管理员权限")
		return
	}
	if h.sessionMgmt == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "会话管理服务未初始化")
		return
	}
	delegationID, err := strconv.ParseInt(c.Param("delegationId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的授权 ID")
		return
	}
	if err := h.sessionMgmt.RevokeDelegation(c.Request.Context(), delegationID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "代理授权已撤销", gin.H{"id": delegationID})
	h.recordAudit(c, "delegation.revoke", "admin_delegation", strconv.FormatInt(delegationID, 10),
		fmt.Sprintf("撤销代理授权 #%d", delegationID))
}
