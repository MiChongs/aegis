package ai

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// ── 作用域 ──

// PlatformAppID 平台级配置在 appid 这一维上的取值。
// 与邮件通道同一条约定：Go 侧以 0 表示平台级，库里以 NULL 表示（保外键）。
const PlatformAppID int64 = 0

func ScopeLabel(appID int64) string {
	if appID == PlatformAppID {
		return "平台级"
	}
	return "应用级"
}

const (
	ScopeApp      = "app"
	ScopePlatform = "platform"
)

// Config 是一条 AI 供应商通道的配置。
//
// 字段值放在通用的 Settings / Secrets 两个袋子里，键的含义由供应商目录声明 ——
// 与邮件配置同一套结构，理由也相同：十几家供应商一家一个具名 struct 的话，
// 每加一家要动四处，漏改任何一处都不报错。
type Config struct {
	ID int64 `json:"id"`
	// AppID 为 0 时是平台级配置（PlatformAppID）。
	AppID     int64  `json:"appid"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"isDefault"`
	// Shared 只对平台级配置有意义：允许应用在自己没有任何可用配置时回落到这条通道。
	// 默认关闭 —— 打开意味着应用的调用花的是平台的钱，必须是平台管理员的显式授权。
	Shared bool `json:"shared"`
	// Priority 供应商链路里的次序，小的先试。同级再按 isDefault、id 排。
	Priority    int    `json:"priority"`
	Description string `json:"description,omitempty"`

	// Settings 非密钥字段，出网原样返回。
	Settings map[string]string `json:"settings"`
	// Secrets 密钥明文，只在进程内存在。
	Secrets map[string]string `json:"-"`
	// SecretsCipher 密钥密文，仓储层与服务层之间传递用。
	SecretsCipher map[string]string `json:"-"`
	// SecretSet 出网用的「这个密钥配没配」布尔位。
	SecretSet map[string]bool `json:"secretSet"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c Config) IsPlatform() bool { return c.AppID == PlatformAppID }

func (c Config) Setting(key string) string {
	if c.Settings == nil {
		return ""
	}
	return strings.TrimSpace(c.Settings[key])
}

func (c Config) SettingInt(key string, fallback int) int {
	value := c.Setting(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// SettingMap 把 kv 类型字段（以 JSON 对象字符串存放）解析成 map。
func (c Config) SettingMap(key string) map[string]string {
	raw := c.Setting(key)
	if raw == "" {
		return nil
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func (c Config) Secret(key string) string {
	if c.Secrets == nil {
		return ""
	}
	return strings.TrimSpace(c.Secrets[key])
}

func (c Config) HasSecret(key string) bool {
	if strings.TrimSpace(c.Secret(key)) != "" {
		return true
	}
	if c.SecretsCipher == nil {
		return false
	}
	return strings.TrimSpace(c.SecretsCipher[key]) != ""
}

// Models 解析「可用型号」字段：每行一个，忽略空行与首尾空白。
func (c Config) Models() []string {
	raw := c.Setting(KeyModels)
	if raw == "" {
		return nil
	}
	lines := strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' })
	models := make([]string, 0, len(lines))
	seen := map[string]bool{}
	for _, line := range lines {
		model := strings.TrimSpace(line)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	return models
}

// DefaultModel 该配置的默认型号：显式指定优先，否则取型号清单第一行。
func (c Config) DefaultModel() string {
	if model := c.Setting(KeyDefaultModel); model != "" {
		return model
	}
	if models := c.Models(); len(models) > 0 {
		return models[0]
	}
	return ""
}

// HasModel 该配置是否声明了某个型号。清单为空时不设限 ——
// 清单是给控制台下拉用的备忘，不是白名单，没写全不该把请求挡死。
func (c Config) HasModel(model string) bool {
	models := c.Models()
	if len(models) == 0 {
		return true
	}
	for _, item := range models {
		if item == model {
			return true
		}
	}
	return false
}

func (c Config) Clone() Config {
	cloned := c
	cloned.Settings = cloneStringMap(c.Settings)
	cloned.Secrets = cloneStringMap(c.Secrets)
	cloned.SecretsCipher = cloneStringMap(c.SecretsCipher)
	if c.SecretSet != nil {
		set := make(map[string]bool, len(c.SecretSet))
		for key, value := range c.SecretSet {
			set[key] = value
		}
		cloned.SecretSet = set
	}
	return cloned
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// ConfigMutation 一次配置写入。指针为 nil 表示不修改；Settings / Secrets 只覆盖出现的键。
type ConfigMutation struct {
	ID          int64
	AppID       int64
	Name        *string
	Provider    *string
	Enabled     *bool
	IsDefault   *bool
	Shared      *bool
	Priority    *int
	Description *string
	Settings    map[string]string
	// Secrets 明文密钥。空串键被忽略（留空即不修改）；要清空请显式传 ClearSecrets。
	Secrets         map[string]string
	ClearSecrets    []string
	ReplaceSettings bool
}

// Resolution 「这次调用最终用的是哪条通道、为什么」。
// 应用没配而回落到平台共享通道时，控制台必须说得出这件事。
type Resolution struct {
	ConfigID   int64  `json:"configId"`
	ConfigName string `json:"configName"`
	Provider   string `json:"provider"`
	Protocol   string `json:"protocol"`
	Scope      string `json:"scope"`
	Inherited  bool   `json:"inherited"`
	Model      string `json:"model"`
	// Models 该通道声明的可用型号，控制台的型号选择器直接用。
	Models []string `json:"models,omitempty"`
}

// ── 统一聊天协议 ──
//
// 两种线上格式（OpenAI Chat Completions / Anthropic Messages）都归一化到这组
// 类型上：Agent 循环、脚本 SDK、网关翻译层全部只面对它，谁也不直接拼线上 JSON。

// 消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// 内容分片类型。
const (
	PartText  = "text"
	PartImage = "image"
)

// ContentPart 一条消息里的一个分片。
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// ImageURL http(s) 地址或 data: URL；两种线上协议都收。
	ImageURL string `json:"imageUrl,omitempty"`
}

// ToolCall 模型发起的一次工具调用。
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ChatMessage 统一格式的一条消息。
//
//   - assistant 消息可以同时带正文与 ToolCalls；
//   - tool 消息以 ToolCallID 关联那次调用，正文放结果。
type ChatMessage struct {
	Role      string        `json:"role"`
	Content   []ContentPart `json:"content,omitempty"`
	ToolCalls []ToolCall    `json:"toolCalls,omitempty"`
	// ToolCallID 仅 role=tool 时使用。
	ToolCallID string `json:"toolCallId,omitempty"`
}

// TextMessage 便捷构造：单段纯文本消息。
func TextMessage(role string, text string) ChatMessage {
	return ChatMessage{Role: role, Content: []ContentPart{{Type: PartText, Text: text}}}
}

// PlainText 把消息正文压成纯文本（跳过图像分片）。
func (m ChatMessage) PlainText() string {
	var builder strings.Builder
	for _, part := range m.Content {
		if part.Type == PartText {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

// Tool 暴露给模型的一个工具。InputSchema 是 JSON Schema。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// 工具选择策略。
const (
	ToolChoiceAuto = "auto"
	ToolChoiceNone = "none"
	// ToolChoiceRequired 必须调用至少一个工具（OpenAI required / Anthropic any）。
	ToolChoiceRequired = "required"
)

// ChatRequest 一次统一格式的对话请求。
type ChatRequest struct {
	Model    string        `json:"model"`
	System   string        `json:"system,omitempty"`
	Messages []ChatMessage `json:"messages"`
	Tools    []Tool        `json:"tools,omitempty"`
	// ToolChoice 空串等价 auto。
	ToolChoice  string   `json:"toolChoice,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	// JSONMode 强制 JSON 输出（OpenAI response_format json_object；
	// Anthropic 无原生开关，由客户端在 system 里追加硬约束）。
	JSONMode bool `json:"jsonMode,omitempty"`
}

// Usage 一次调用的用量。
type Usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}

func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.TotalTokens += other.TotalTokens
}

// 归一化后的完成原因。
const (
	FinishStop      = "stop"
	FinishLength    = "length"
	FinishToolCalls = "tool-calls"
	FinishFiltered  = "content-filter"
	FinishError     = "error"
	FinishOther     = "other"
)

// ChatResponse 一次调用的完整结论（流式时由客户端聚合出同样的东西）。
type ChatResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	// Text 正文；Reasoning 思考块（供应商支持时才有）。
	Text         string     `json:"text"`
	Reasoning    string     `json:"reasoning,omitempty"`
	ToolCalls    []ToolCall `json:"toolCalls,omitempty"`
	FinishReason string     `json:"finishReason"`
	Usage        Usage      `json:"usage"`
}

// ── 流式事件 ──

// 流事件类型。
const (
	StreamText      = "text"       // 正文增量
	StreamReasoning = "reasoning"  // 思考增量
	StreamToolStart = "tool-start" // 一次工具调用开始（已知 id 与名字）
	StreamToolDelta = "tool-delta" // 工具入参 JSON 增量
)

// StreamEvent 统一格式的流式增量。终态不走事件 —— ChatStream 返回聚合好的 ChatResponse。
type StreamEvent struct {
	Type string `json:"type"`
	// Delta 正文/思考/入参的文本增量。
	Delta string `json:"delta,omitempty"`
	// ToolIndex / ToolID / ToolName 标识这段增量属于哪次工具调用。
	ToolIndex int    `json:"toolIndex,omitempty"`
	ToolID    string `json:"toolId,omitempty"`
	ToolName  string `json:"toolName,omitempty"`
}
