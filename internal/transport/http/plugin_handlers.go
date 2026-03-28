package httptransport

import (
	"net/http"

	plugindomain "aegis/internal/domain/plugin"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminListPlugins(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var q PluginListQueryParams
	_ = c.ShouldBindQuery(&q)
	result, err := h.plugin.ListPlugins(c.Request.Context(), plugindomain.PluginListQuery{
		Status: q.Status, Type: q.Type, Keyword: q.Keyword, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

func (h *Handler) AdminCreatePlugin(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	var req PluginCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	p, err := h.plugin.CreatePlugin(c.Request.Context(), plugindomain.CreatePluginInput{
		Name: req.Name, DisplayName: req.DisplayName, Description: req.Description,
		Type: req.Type, Hooks: req.Hooks, Config: req.Config,
		ExprScript: req.ExprScript, Priority: req.Priority,
	}, &session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "插件已创建", p)
	h.recordAudit(c, "plugin.create", "plugin", p.Name, "创建插件 "+p.DisplayName)
}

func (h *Handler) AdminGetPlugin(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的插件 ID")
		return
	}
	p, err := h.plugin.GetPlugin(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if p == nil {
		response.Error(c, http.StatusNotFound, 40480, "插件不存在")
		return
	}
	response.Success(c, 200, "ok", p)
}

func (h *Handler) AdminUpdatePlugin(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的插件 ID")
		return
	}
	var req PluginUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	p, err := h.plugin.UpdatePlugin(c.Request.Context(), id, plugindomain.UpdatePluginInput{
		DisplayName: req.DisplayName, Description: req.Description,
		Hooks: req.Hooks, Config: req.Config,
		ExprScript: req.ExprScript, Priority: req.Priority,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "插件已更新", p)
	h.recordAudit(c, "plugin.update", "plugin", p.Name, "修改插件 "+p.DisplayName)
}

func (h *Handler) AdminDeletePlugin(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的插件 ID")
		return
	}
	if err := h.plugin.DeletePlugin(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "插件已删除", nil)
	h.recordAudit(c, "plugin.delete", "plugin", "", "删除插件")
}

func (h *Handler) AdminEnablePlugin(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的插件 ID")
		return
	}
	if err := h.plugin.EnablePlugin(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "插件已启用", nil)
	h.recordAudit(c, "plugin.enable", "plugin", "", "启用插件")
}

func (h *Handler) AdminDisablePlugin(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的插件 ID")
		return
	}
	if err := h.plugin.DisablePlugin(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "插件已禁用", nil)
	h.recordAudit(c, "plugin.disable", "plugin", "", "禁用插件")
}

func (h *Handler) AdminGetHookRegistry(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	view, err := h.plugin.GetHookRegistry(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", view)
}

func (h *Handler) AdminListHookExecutions(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	var q HookExecutionQueryParams
	_ = c.ShouldBindQuery(&q)
	result, err := h.plugin.ListHookExecutions(c.Request.Context(), plugindomain.HookExecutionListQuery{
		PluginID: q.PluginID, HookName: q.HookName, Status: q.Status, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}
