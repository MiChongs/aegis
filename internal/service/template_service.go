package service

import (
	"bytes"
	"context"
	"net/http"
	"text/template"

	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// TemplateService 消息模板服务
type TemplateService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

func NewTemplateService(log *zap.Logger, pg *pgrepo.Repository) *TemplateService {
	return &TemplateService{log: log, pg: pg}
}

// Render 渲染模板
func (s *TemplateService) Render(ctx context.Context, code string, data map[string]string) (*systemdomain.RenderResult, error) {
	tmpl, err := s.pg.GetMessageTemplate(ctx, code)
	if err != nil {
		return nil, err
	}
	if tmpl == nil || !tmpl.Enabled {
		return nil, apperrors.New(40482, http.StatusNotFound, "模板不存在或已禁用")
	}
	return renderTemplate(tmpl, data)
}

// RenderPreview 预览渲染（不检查 enabled）
func (s *TemplateService) RenderPreview(ctx context.Context, code string, data map[string]string) (*systemdomain.RenderResult, error) {
	tmpl, err := s.pg.GetMessageTemplate(ctx, code)
	if err != nil {
		return nil, err
	}
	if tmpl == nil {
		return nil, apperrors.New(40482, http.StatusNotFound, "模板不存在")
	}
	return renderTemplate(tmpl, data)
}

func (s *TemplateService) ListTemplates(ctx context.Context) ([]systemdomain.MessageTemplate, error) {
	return s.pg.ListMessageTemplates(ctx)
}

func (s *TemplateService) GetTemplate(ctx context.Context, code string) (*systemdomain.MessageTemplate, error) {
	return s.pg.GetMessageTemplate(ctx, code)
}

func (s *TemplateService) CreateTemplate(ctx context.Context, input systemdomain.CreateTemplateInput, createdBy int64) (*systemdomain.MessageTemplate, error) {
	return s.pg.CreateMessageTemplate(ctx, input, createdBy)
}

func (s *TemplateService) UpdateTemplate(ctx context.Context, code string, input systemdomain.UpdateTemplateInput) (*systemdomain.MessageTemplate, error) {
	return s.pg.UpdateMessageTemplate(ctx, code, input)
}

func (s *TemplateService) DeleteTemplate(ctx context.Context, code string) error {
	return s.pg.DeleteMessageTemplate(ctx, code)
}

func renderTemplate(tmpl *systemdomain.MessageTemplate, data map[string]string) (*systemdomain.RenderResult, error) {
	result := &systemdomain.RenderResult{}
	var err error

	if tmpl.Subject != "" {
		result.Subject, err = execTemplate("subject", tmpl.Subject, data)
		if err != nil {
			return nil, apperrors.New(50095, http.StatusInternalServerError, "主题渲染失败: "+err.Error())
		}
	}
	if tmpl.BodyHTML != "" {
		result.BodyHTML, err = execTemplate("html", tmpl.BodyHTML, data)
		if err != nil {
			return nil, apperrors.New(50096, http.StatusInternalServerError, "HTML 渲染失败: "+err.Error())
		}
	}
	if tmpl.BodyText != "" {
		result.BodyText, err = execTemplate("text", tmpl.BodyText, data)
		if err != nil {
			return nil, apperrors.New(50097, http.StatusInternalServerError, "纯文本渲染失败: "+err.Error())
		}
	}
	return result, nil
}

func execTemplate(name, tmplStr string, data map[string]string) (string, error) {
	t, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
