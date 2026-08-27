package ai

import (
	"encoding/json"
	"time"
)

// ── Agent 会话 ──

// 会话场景。目前只有一个：帮管理员写远程函数。
// 单独留一维是因为后续的「工作流编排助手」「风控规则助手」都会复用同一张表。
const (
	SceneFunction = "function"
)

// Conversation 一个 Agent 会话。
//
// CompactSummary / CompactedBefore 支撑自动压缩：超过阈值时把旧消息摘要成一段
// 滚动总结，水位线之前的消息不再送给模型（但仍留在库里供界面回放）。
type Conversation struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"appId"`
	AdminID int64  `json:"adminId"`
	Scene   string `json:"scene"`
	// Ref 场景内的锚点：function 场景下是函数名。
	Ref   string `json:"ref"`
	Title string `json:"title"`
	// ProviderConfigID / Model 记住上次用的通道与型号，续聊时不必重选。
	ProviderConfigID int64  `json:"providerConfigId,omitempty"`
	Model            string `json:"model,omitempty"`
	InputTokens      int64  `json:"inputTokens"`
	OutputTokens     int64  `json:"outputTokens"`
	CompactSummary   string `json:"-"`
	CompactedBefore  int64  `json:"-"`
	// Compactions 压缩发生过几次 —— 界面上要能看出「这个会话被摘要过」。
	Compactions int       `json:"compactions"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AgentMessage 会话里的一条消息。
//
// Parts 存的是**界面分片**（text / reasoning / tool-xxx / data-xxx），与控制台
// 渲染格式一致，回放时原样下发；喂给模型前由服务层翻译成 ChatMessage。
// 存界面格式而不是模型格式是刻意的：工具调用的输入输出、思考块这些东西
// 界面要逐条展示，而模型格式在不同供应商之间来回转换会丢字段。
type AgentMessage struct {
	ID             int64           `json:"id"`
	ConversationID int64           `json:"conversationId"`
	Role           string          `json:"role"`
	Parts          json.RawMessage `json:"parts"`
	Usage          *Usage          `json:"usage,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// ConversationQuery 会话列表的筛选。
type ConversationQuery struct {
	AppID   int64
	AdminID int64
	Scene   string
	Ref     string
	Limit   int
}

// ── Skill ──

// Skill 一段可复用的提示词包：领域约定、代码风格、排错清单。
//
// 内置技能由 Go 侧目录提供（函数写作指南、effects 说明），自定义技能落库。
// 两者对 Agent 完全同构：启用即注入系统提示词。
type Skill struct {
	ID int64 `json:"id"`
	// AppID 为 0 时是平台级技能，对所有应用可用。
	AppID int64 `json:"appid"`
	// Key 稳定标识，内置技能以 builtin: 前缀区分。
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Enabled     bool      `json:"enabled"`
	Builtin     bool      `json:"builtin"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SkillMutation 一次技能写入。
type SkillMutation struct {
	ID          int64
	AppID       int64
	Key         *string
	Name        *string
	Description *string
	Content     *string
	Enabled     *bool
}

// ── MCP 服务器 ──

// MCPServer 一个外接的 MCP 工具服务器（Streamable HTTP）。
//
// Headers 整体按密钥处理：MCP 的鉴权通常放在自定义头里（Authorization / X-API-Key），
// 逐键区分「哪个头算密钥」只会让人漏标 —— 全加密最稳。
type MCPServer struct {
	ID int64 `json:"id"`
	// AppID 为 0 时是平台级服务器。
	AppID       int64  `json:"appid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
	// Headers 明文只在进程内存在；出网只回 HeadersSet。
	Headers       map[string]string `json:"-"`
	HeadersCipher string            `json:"-"`
	HeadersSet    bool              `json:"headersSet"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// MCPServerMutation 一次 MCP 服务器写入。Headers 为 nil 表示不修改。
type MCPServerMutation struct {
	ID          int64
	AppID       int64
	Name        *string
	URL         *string
	Enabled     *bool
	Description *string
	Headers     map[string]string
	// ClearHeaders 显式清空已配置的请求头。
	ClearHeaders bool
}

// MCPTool MCP 服务器声明的一个工具（tools/list 的结论）。
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
