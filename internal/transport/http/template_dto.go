package httptransport

import systemdomain "aegis/internal/domain/system"

type CreateTemplateRequest struct {
	Code        string                        `json:"code" binding:"required"`
	Name        string                        `json:"name" binding:"required"`
	Description string                        `json:"description"`
	Channel     string                        `json:"channel" binding:"required,oneof=email sms notification"`
	Subject     string                        `json:"subject"`
	BodyHTML    string                        `json:"bodyHtml"`
	BodyText    string                        `json:"bodyText"`
	Variables   []systemdomain.TemplateVariable `json:"variables"`
}

type UpdateTemplateRequest struct {
	Name        *string                         `json:"name,omitempty"`
	Description *string                         `json:"description,omitempty"`
	Subject     *string                         `json:"subject,omitempty"`
	BodyHTML    *string                         `json:"bodyHtml,omitempty"`
	BodyText    *string                         `json:"bodyText,omitempty"`
	Variables   *[]systemdomain.TemplateVariable `json:"variables,omitempty"`
	Enabled     *bool                           `json:"enabled,omitempty"`
}

type PreviewTemplateRequest struct {
	Data map[string]string `json:"data"`
}

type TestSendTemplateRequest struct {
	Data    map[string]string `json:"data"`
	ToEmail string            `json:"toEmail" binding:"required,email"`
}
