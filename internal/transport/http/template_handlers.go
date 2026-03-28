package httptransport

import (
	"net/http"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) ListTemplates(c *gin.Context) {
	items, err := h.tmpl.ListTemplates(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) GetTemplate(c *gin.Context) {
	code := c.Param("code")
	t, err := h.tmpl.GetTemplate(c.Request.Context(), code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if t == nil {
		response.Error(c, http.StatusNotFound, 40482, "模板不存在")
		return
	}
	response.Success(c, 200, "ok", t)
}

func (h *Handler) CreateTemplate(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	var req CreateTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	t, err := h.tmpl.CreateTemplate(c.Request.Context(), systemdomain.CreateTemplateInput{
		Code: req.Code, Name: req.Name, Description: req.Description,
		Channel: req.Channel, Subject: req.Subject,
		BodyHTML: req.BodyHTML, BodyText: req.BodyText, Variables: req.Variables,
	}, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "模板已创建", t)
	h.recordAudit(c, "template.create", "template", req.Code, "创建模板 "+req.Code)
}

func (h *Handler) UpdateTemplate(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	code := c.Param("code")
	var req UpdateTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	t, err := h.tmpl.UpdateTemplate(c.Request.Context(), code, systemdomain.UpdateTemplateInput{
		Name: req.Name, Description: req.Description,
		Subject: req.Subject, BodyHTML: req.BodyHTML, BodyText: req.BodyText,
		Variables: req.Variables, Enabled: req.Enabled,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "模板已更新", t)
	h.recordAudit(c, "template.update", "template", code, "修改模板 "+code)
}

func (h *Handler) DeleteTemplate(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	code := c.Param("code")
	if err := h.tmpl.DeleteTemplate(c.Request.Context(), code); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "模板已删除", nil)
	h.recordAudit(c, "template.delete", "template", code, "删除模板 "+code)
}

func (h *Handler) PreviewTemplate(c *gin.Context) {
	code := c.Param("code")
	var req PreviewTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.tmpl.RenderPreview(c.Request.Context(), code, req.Data)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}
