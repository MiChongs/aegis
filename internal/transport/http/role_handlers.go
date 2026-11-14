package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 角色管理接口。权限点由 AdminAccess 按 authz/tree.go 的「角色权限」菜单判定
// （查看 / 新增 / 编辑 / 删除），授权上限与档位守卫在 AdminService 里。

// AdminGetRoleDetail 角色详情（定义、override、生效权限、旧权限点、成员）。
func (h *Handler) AdminGetRoleDetail(c *gin.Context) {
	session, _ := adminAccessSession(c)
	detail, err := h.admin.GetRoleDetail(c.Request.Context(), session, c.Param("roleKey"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	for i := range detail.Members {
		detail.Members[i].Avatar = h.adminAvatarURL(c, detail.Members[i].AdminID, detail.Members[i].Avatar, detail.Members[i].Account)
	}
	response.Success(c, 200, "ok", detail)
}

// AdminCreateCustomRole 创建自定义角色
func (h *Handler) AdminCreateCustomRole(c *gin.Context) {
	session, _ := adminAccessSession(c)
	var req CreateCustomRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cr, err := h.admin.CreateCustomRole(c.Request.Context(), session, admindomain.CreateCustomRoleInput{
		RoleKey: req.RoleKey, Name: req.Name, Description: req.Description,
		Level: req.Level, Scope: req.Scope, BaseRole: req.BaseRole,
		Permissions: req.Permissions, Deny: req.Deny, Status: req.Status, SortOrder: req.SortOrder,
		DataScope: req.DataScope, DataAppIDs: req.DataAppIDs,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "自定义角色已创建", cr)
	h.recordAudit(c, "role.create", "role", req.RoleKey, "创建角色 "+req.RoleKey)
}

// AdminCopyRole 以某个角色为模板复制出一个自定义角色。
func (h *Handler) AdminCopyRole(c *gin.Context) {
	session, _ := adminAccessSession(c)
	var req RoleCopyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	source := c.Param("roleKey")
	cr, err := h.admin.CopyRole(c.Request.Context(), session, source, req.RoleKey, req.Name)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "角色已复制", cr)
	h.recordAudit(c, "role.copy", "role", req.RoleKey, fmt.Sprintf("由角色 %s 复制出 %s", source, req.RoleKey))
}

// AdminUpdateCustomRole 更新自定义角色
func (h *Handler) AdminUpdateCustomRole(c *gin.Context) {
	session, _ := adminAccessSession(c)
	roleKey := c.Param("roleKey")
	var req UpdateCustomRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cr, err := h.admin.UpdateCustomRole(c.Request.Context(), session, roleKey, admindomain.UpdateCustomRoleInput{
		Name: req.Name, Description: req.Description, Level: req.Level,
		Permissions: req.Permissions, Deny: req.Deny, BaseRole: req.BaseRole,
		Status: req.Status, SortOrder: req.SortOrder, DataScope: req.DataScope, DataAppIDs: req.DataAppIDs,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自定义角色已更新", cr)
	h.recordAudit(c, "role.update", "role", roleKey, "修改角色 "+roleKey)
}

// AdminSetRolePermissions 整组保存角色的功能权限（自定义角色改定义，内置角色改 override）。
func (h *Handler) AdminSetRolePermissions(c *gin.Context) {
	session, _ := adminAccessSession(c)
	roleKey := c.Param("roleKey")
	var req RolePermissionsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.admin.SetRolePermissions(c.Request.Context(), session, roleKey, req.Allow, req.Deny); err != nil {
		h.writeError(c, err)
		return
	}
	detail, err := h.admin.GetRoleDetail(c.Request.Context(), session, roleKey)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "功能权限已保存", detail)
	h.recordAudit(c, "role.permissions", "role", roleKey,
		fmt.Sprintf("保存角色 %s 的功能权限：允许 %d 项，禁止 %d 项", roleKey, len(req.Allow), len(req.Deny)))
}

// AdminSetRoleStatus 启用 / 停用自定义角色。
func (h *Handler) AdminSetRoleStatus(c *gin.Context) {
	session, _ := adminAccessSession(c)
	roleKey := c.Param("roleKey")
	var req RoleStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.admin.SetRoleStatus(c.Request.Context(), session, roleKey, req.Status); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "角色状态已更新", gin.H{"roleKey": roleKey, "status": req.Status})
	h.recordAudit(c, "role.status", "role", roleKey, fmt.Sprintf("角色 %s 状态变更为 %s", roleKey, req.Status))
}

// AdminDeleteCustomRole 删除自定义角色
func (h *Handler) AdminDeleteCustomRole(c *gin.Context) {
	roleKey := c.Param("roleKey")
	force := c.Query("force") == "true"
	if err := h.admin.DeleteCustomRole(c.Request.Context(), roleKey, force); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自定义角色已删除", nil)
	h.recordAudit(c, "role.delete", "role", roleKey, "删除角色 "+roleKey)
}

// AdminListRoleMembers 持有某角色的全部授权。
func (h *Handler) AdminListRoleMembers(c *gin.Context) {
	items, err := h.admin.ListRoleMembers(c.Request.Context(), c.Param("roleKey"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	for i := range items {
		items[i].Avatar = h.adminAvatarURL(c, items[i].AdminID, items[i].Avatar, items[i].Account)
	}
	response.Success(c, 200, "ok", items)
}

// AdminAddRoleMembers 把一批管理员加进角色。
func (h *Handler) AdminAddRoleMembers(c *gin.Context) {
	session, _ := adminAccessSession(c)
	roleKey := c.Param("roleKey")
	var req RoleMembersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	added, err := h.admin.AddRoleMembers(c.Request.Context(), session, roleKey, req.AdminIDs, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	ids := make([]string, 0, len(req.AdminIDs))
	for _, id := range req.AdminIDs {
		ids = append(ids, "#"+strconv.FormatInt(id, 10))
	}
	response.Success(c, 200, "已添加成员", gin.H{"roleKey": roleKey, "added": added})
	h.recordAudit(c, "role.members_add", "role", roleKey, fmt.Sprintf("为角色 %s 添加成员 %s", roleKey, strings.Join(ids, "、")))
}

// AdminRemoveRoleMember 把某个管理员移出角色（?appid= 指定移除哪个应用上的那条）。
func (h *Handler) AdminRemoveRoleMember(c *gin.Context) {
	session, _ := adminAccessSession(c)
	roleKey := c.Param("roleKey")
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员标识")
		return
	}
	var appID *int64
	if raw := strings.TrimSpace(c.Query("appid")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.Error(c, http.StatusBadRequest, 40058, "无效的应用标识")
			return
		}
		appID = &id
	}
	if err := h.admin.RemoveRoleMember(c.Request.Context(), session, roleKey, adminID, appID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已移除成员", gin.H{"roleKey": roleKey, "adminId": adminID})
	h.recordAudit(c, "role.members_remove", "role", roleKey, fmt.Sprintf("将管理员 #%d 移出角色 %s", adminID, roleKey))
}

// AdminGetRoleMatrix 权限矩阵
func (h *Handler) AdminGetRoleMatrix(c *gin.Context) {
	response.Success(c, 200, "ok", h.admin.GetRoleMatrix())
}

// AdminGetRoleGraph 角色关系图
func (h *Handler) AdminGetRoleGraph(c *gin.Context) {
	response.Success(c, 200, "ok", h.admin.GetRoleGraph())
}

// AdminGetRoleImpactPreview 角色修改影响预览
func (h *Handler) AdminGetRoleImpactPreview(c *gin.Context) {
	roleKey := c.Param("roleKey")
	result, err := h.admin.GetRoleImpactPreview(c.Request.Context(), roleKey)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// adminAvatarURL 把库里的头像引用换成可展示的地址（与管理员列表同一套规则）。
func (h *Handler) adminAvatarURL(c *gin.Context, adminID int64, avatar, account string) string {
	if h.avatar == nil {
		return avatar
	}
	return h.avatar.ResolveAdminAvatar(c.Request.Context(), requestBaseURL(c.Request), adminID, avatar, "", account)
}
