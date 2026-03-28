package plugin

import "time"

// Plugin 插件定义
type Plugin struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	DisplayName   string         `json:"displayName"`
	Description   string         `json:"description"`
	Type          string         `json:"type"`   // expr | wasm
	Status        string         `json:"status"` // enabled | disabled | error
	Version       string         `json:"version"`
	Author        string         `json:"author"`
	Hooks         []HookBinding  `json:"hooks"`
	Config        map[string]any `json:"config"`
	ExprScript    string         `json:"exprScript,omitempty"`
	WASMModuleURL string         `json:"wasmModuleUrl,omitempty"`
	WASMHash      string         `json:"wasmHash,omitempty"`
	Priority      int            `json:"priority"`
	ErrorMessage  string         `json:"errorMessage,omitempty"`
	CreatedBy     *int64         `json:"createdBy,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// HookBinding 插件对钩子的绑定
type HookBinding struct {
	HookName string `json:"hookName"`
	Phase    string `json:"phase"` // before | after
	Priority int    `json:"priority"`
}

// HookDefinition 钩子点元数据
type HookDefinition struct {
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Phase       string `json:"phase"` // before | after | both
	Description string `json:"description"`
}

// HookExecution 钩子执行日志
type HookExecution struct {
	ID         int64          `json:"id"`
	PluginID   int64          `json:"pluginId"`
	PluginName string         `json:"pluginName"`
	HookName   string         `json:"hookName"`
	Phase      string         `json:"phase"`
	DurationMs float64        `json:"durationMs"`
	Status     string         `json:"status"` // success | error | skipped | timeout
	Error      string         `json:"error,omitempty"`
	Input      map[string]any `json:"input,omitempty"`
	Output     map[string]any `json:"output,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// HookPayload 传入钩子的统一载荷
type HookPayload struct {
	HookName string         `json:"hookName"`
	Phase    string         `json:"phase"`
	Data     map[string]any `json:"data"`
	Metadata HookMetadata   `json:"metadata"`
}

// HookMetadata 钩子执行上下文元数据
type HookMetadata struct {
	RequestID string `json:"requestId,omitempty"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
	AdminID   *int64 `json:"adminId,omitempty"`
	AppID     *int64 `json:"appId,omitempty"`
	UserID    *int64 `json:"userId,omitempty"`
}

// HookResult 钩子执行结果
type HookResult struct {
	Allow   bool              `json:"allow"`
	Data    map[string]any    `json:"data,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Message string            `json:"message,omitempty"`
}

// PluginListQuery 分页查询
type PluginListQuery struct {
	Status  string
	Type    string
	Keyword string
	Page    int
	Limit   int
}

// PluginListResult 分页结果
type PluginListResult struct {
	Items      []Plugin `json:"items"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	Total      int64    `json:"total"`
	TotalPages int      `json:"totalPages"`
}

// HookExecutionListQuery 执行日志分页查询
type HookExecutionListQuery struct {
	PluginID int64
	HookName string
	Status   string
	Page     int
	Limit    int
}

// HookExecutionListResult 执行日志分页结果
type HookExecutionListResult struct {
	Items      []HookExecution `json:"items"`
	Page       int             `json:"page"`
	Limit      int             `json:"limit"`
	Total      int64           `json:"total"`
	TotalPages int             `json:"totalPages"`
}

// PluginRegistryView 钩子注册表视图
type PluginRegistryView struct {
	Hooks    []HookDefinition               `json:"hooks"`
	Bindings map[string][]PluginHookSummary  `json:"bindings"`
}

// PluginHookSummary 钩子上的插件摘要
type PluginHookSummary struct {
	PluginID    int64  `json:"pluginId"`
	PluginName  string `json:"pluginName"`
	DisplayName string `json:"displayName"`
	Phase       string `json:"phase"`
	Priority    int    `json:"priority"`
	Type        string `json:"type"`
	Status      string `json:"status"`
}

// CreatePluginInput 创建插件输入
type CreatePluginInput struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"displayName"`
	Description string         `json:"description"`
	Type        string         `json:"type"`
	Hooks       []HookBinding  `json:"hooks"`
	Config      map[string]any `json:"config"`
	ExprScript  string         `json:"exprScript"`
	Priority    int            `json:"priority"`
}

// UpdatePluginInput 更新插件输入
type UpdatePluginInput struct {
	DisplayName *string         `json:"displayName,omitempty"`
	Description *string         `json:"description,omitempty"`
	Hooks       []HookBinding   `json:"hooks,omitempty"`
	Config      map[string]any  `json:"config,omitempty"`
	ExprScript  *string         `json:"exprScript,omitempty"`
	Priority    *int            `json:"priority,omitempty"`
}
