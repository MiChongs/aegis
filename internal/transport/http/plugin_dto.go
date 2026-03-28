package httptransport

import plugindomain "aegis/internal/domain/plugin"

type PluginCreateRequest struct {
	Name        string                      `json:"name" binding:"required"`
	DisplayName string                      `json:"displayName" binding:"required"`
	Description string                      `json:"description"`
	Type        string                      `json:"type" binding:"required"`
	Hooks       []plugindomain.HookBinding  `json:"hooks"`
	Config      map[string]any              `json:"config"`
	ExprScript  string                      `json:"exprScript"`
	Priority    int                         `json:"priority"`
}

type PluginUpdateRequest struct {
	DisplayName *string                     `json:"displayName,omitempty"`
	Description *string                     `json:"description,omitempty"`
	Hooks       []plugindomain.HookBinding  `json:"hooks,omitempty"`
	Config      map[string]any              `json:"config,omitempty"`
	ExprScript  *string                     `json:"exprScript,omitempty"`
	Priority    *int                        `json:"priority,omitempty"`
}

type PluginListQueryParams struct {
	Status  string `form:"status"`
	Type    string `form:"type"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

type HookExecutionQueryParams struct {
	PluginID int64  `form:"pluginId"`
	HookName string `form:"hookName"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	Limit    int    `form:"limit"`
}
