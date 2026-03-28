package system

import "time"

// MessageTemplate 消息模板
type MessageTemplate struct {
	ID          int64              `json:"id"`
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Channel     string             `json:"channel"` // email / sms / notification
	Subject     string             `json:"subject"`
	BodyHTML    string             `json:"bodyHtml"`
	BodyText    string             `json:"bodyText"`
	Variables   []TemplateVariable `json:"variables"`
	IsBuiltin   bool               `json:"isBuiltin"`
	Enabled     bool               `json:"enabled"`
	CreatedBy   *int64             `json:"createdBy,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

// TemplateVariable 模板变量定义
type TemplateVariable struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Example     string `json:"example,omitempty"`
}

// RenderResult 模板渲染结果
type RenderResult struct {
	Subject  string `json:"subject"`
	BodyHTML string `json:"bodyHtml"`
	BodyText string `json:"bodyText"`
}

// CreateTemplateInput 创建模板请求
type CreateTemplateInput struct {
	Code        string             `json:"code"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Channel     string             `json:"channel"`
	Subject     string             `json:"subject"`
	BodyHTML    string             `json:"bodyHtml"`
	BodyText    string             `json:"bodyText"`
	Variables   []TemplateVariable `json:"variables"`
}

// UpdateTemplateInput 更新模板请求
type UpdateTemplateInput struct {
	Name        *string             `json:"name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Subject     *string             `json:"subject,omitempty"`
	BodyHTML    *string             `json:"bodyHtml,omitempty"`
	BodyText    *string             `json:"bodyText,omitempty"`
	Variables   *[]TemplateVariable `json:"variables,omitempty"`
	Enabled     *bool               `json:"enabled,omitempty"`
}
