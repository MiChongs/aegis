package httptransport

import (
	"net/http"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AdminCreateCustomRole 创建自定义角色
func (h *Handler) AdminCreateCustomRole(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	var req CreateCustomRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cr, err := h.admin.CreateCustomRole(c.Request.Context(), admindomain.CreateCustomRoleInput{
		RoleKey: req.RoleKey, Name: req.Name, Description: req.Description,
		Level: req.Level, Scope: req.Scope, BaseRole: req.BaseRole, Permissions: req.Permissions,
	}, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "自定义角色已创建", cr)
	h.recordAudit(c, "role.create", "role", req.RoleKey, "创建角色 "+req.RoleKey)
}

// AdminUpdateCustomRole 更新自定义角色
func (h *Handler) AdminUpdateCustomRole(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	roleKey := c.Param("roleKey")
	var req UpdateCustomRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	cr, err := h.admin.UpdateCustomRole(c.Request.Context(), roleKey, admindomain.UpdateCustomRoleInput{
		Name: req.Name, Description: req.Description, Level: req.Level, Permissions: req.Permissions,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自定义角色已更新", cr)
	h.recordAudit(c, "role.update", "role", roleKey, "修改角色 "+roleKey)
}

// AdminDeleteCustomRole 删除自定义角色
func (h *Handler) AdminDeleteCustomRole(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	roleKey := c.Param("roleKey")
	force := c.Query("force") == "true"
	if err := h.admin.DeleteCustomRole(c.Request.Context(), roleKey, force); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "自定义角色已删除", nil)
	h.recordAudit(c, "role.delete", "role", roleKey, "删除角色 "+roleKey)
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
