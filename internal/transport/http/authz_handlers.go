package httptransport

import (
	"net/http"

	"aegis/internal/authz"
	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// 授权策略管理端。
//
// 这一组接口是"权限系统能不能灵活配置"的落点：角色增减、按人授予/禁止、
// 以及最重要的 —— 一次 403 到底是被什么挡住的。

// AdminAuthzModel 返回授权模型的自述：权限目录、内置角色、路由规则表。
//
// 三者以前分散在三个包里且只存在于代码中，运维要回答"这个接口需要什么权限"
// 只能翻源码。现在一次请求就能拿到全部，控制台的权限页与「接入自检」都读它。
func (h *Handler) AdminAuthzModel(c *gin.Context) {
	rules := authz.AdminRouteRules()
	routes := make([]gin.H, 0, len(rules))
	for _, rule := range rules {
		scope := "platform"
		if rule.Scope == authz.ScopeApp {
			scope = "app"
		}
		routes = append(routes, gin.H{
			"methods":    rule.Methods,
			"pattern":    rule.Pattern,
			"permission": rule.Permission,
			"scope":      scope,
			"note":       rule.Note,
		})
	}
	response.Success(c, 200, "ok", gin.H{
		"permissionGroups": authz.PermissionCatalog(),
		"builtinRoles":     authz.BuiltinRoles(),
		"routeRules":       routes,
	})
}

// AdminAuthzPolicies 返回引擎内存里当前生效的策略。
func (h *Handler) AdminAuthzPolicies(c *gin.Context) {
	response.Success(c, 200, "ok", h.admin.PolicySnapshot())
}

// AdminAuthzSubjectPolicies 列出某个主体（角色或管理员）名下的策略行。
func (h *Handler) AdminAuthzSubjectPolicies(c *gin.Context) {
	subject := c.Query("subject")
	if subject == "" {
		response.Error(c, http.StatusBadRequest, 40066, "请指定主体（role:<key> 或 admin:<id>）")
		return
	}
	items, err := h.admin.ListAdminPolicies(c.Request.Context(), subject)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// AdminAuthzSetRoleOverride 写入某个角色的人工增减（allow / deny / 继承）。
//
// 这是"内置角色不可编辑"的出口：内置定义每次启动按代码重刷，
// 而 override 这一组启动时不动，两者叠加即最终权限。
func (h *Handler) AdminAuthzSetRoleOverride(c *gin.Context) {
	var req service.PolicyOverrideInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var actor *int64
	if session, ok := adminAccessSession(c); ok && session != nil {
		actor = &session.AdminID
	}
	if err := h.admin.SetRoleOverride(c.Request.Context(), req, actor); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已保存", gin.H{"roleKey": req.RoleKey})
}

// AdminAuthzGrantsRequest 是整组替换某个管理员直接授予/禁止的请求体。
//
// 与下面的 AdminAuthzExplainRequest 一样，提成具名类型是为了让 docsgen
// 能反查到它：匿名 struct 没有名字可引用，接口在 OpenAPI 里就会缺 requestBody。
type AdminAuthzGrantsRequest struct {
	Grants []service.AdminGrantInput `json:"grants"`
}

// AdminAuthzExplainRequest 是排障接口的请求体：某人在某作用域下能否做某事。
type AdminAuthzExplainRequest struct {
	AdminID    int64  `json:"adminId"`
	Permission string `json:"permission"`
	AppID      *int64 `json:"appid"`
}

// AdminAuthzSetAdminGrants 整组替换某个管理员的直接授予/禁止。
func (h *Handler) AdminAuthzSetAdminGrants(c *gin.Context) {
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40058, "无效的管理员标识")
		return
	}
	var req AdminAuthzGrantsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var actor *int64
	if session, ok := adminAccessSession(c); ok && session != nil {
		actor = &session.AdminID
	}
	if err := h.admin.SetAdminGrants(c.Request.Context(), adminID, req.Grants, actor); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已保存", gin.H{"adminId": adminID, "count": len(req.Grants)})
}

// AdminAuthzExplain 回答「某人在某作用域下能不能做某事，为什么」。
//
// 一次 403 的排查以前要翻四个地方（角色定义、权限点常量、路由映射、作用域），
// 且四处都在代码里。现在一次请求给出：判定用到的全部主体、命中的策略行、结论。
func (h *Handler) AdminAuthzExplain(c *gin.Context) {
	var req AdminAuthzExplainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.admin.ExplainAuthorization(c.Request.Context(), req.AdminID, req.Permission, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

// AdminAuthzReload 手动触发一次策略重载（排障用）。
func (h *Handler) AdminAuthzReload(c *gin.Context) {
	if err := h.admin.ReloadPolicies(c.Request.Context()); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已重载", h.admin.PolicySnapshot())
}
