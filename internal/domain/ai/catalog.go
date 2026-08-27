package ai

// AI 供应商的**自描述**元数据。
//
// 与邮件九档服务商、支付渠道 Describe()、远程函数能力目录同一套做法：
// 一份目录同时驱动服务端校验、控制台配置表单、以及「这家走哪种线上协议」这类
// 客户端要先问清楚的事实。新增一家供应商只需在这里补一份描述 ——
// 只要它说的是 OpenAI 或 Anthropic 两种协议之一，控制台与客户端零改动即自动出现。
//
// 放在 domain 而不是 service，理由与邮件目录相同：transport 层要把它序列化下发。

// ── 线上协议 ──
//
// 平台只实现这两种协议的完整客户端；所有供应商都归一到其中之一。
// 这不是偷懒：国内外主流服务（DeepSeek / Kimi / GLM / 通义 / 豆包 / Groq /
// OpenRouter / Ollama …）对外暴露的正是 OpenAI 兼容面，Anthropic 系走 Messages。
const (
	// ProtocolOpenAI OpenAI Chat Completions（/chat/completions）
	ProtocolOpenAI = "openai"
	// ProtocolAnthropic Anthropic Messages（/messages）
	ProtocolAnthropic = "anthropic"
)

// ── 供应商标识 ──
const (
	ProviderOpenAI      = "openai"
	ProviderAnthropic   = "anthropic"
	ProviderAzureOpenAI = "azure-openai"
	ProviderGemini      = "gemini"
	ProviderDeepSeek    = "deepseek"
	ProviderMoonshot    = "moonshot"
	ProviderZhipu       = "zhipu"
	ProviderQwen        = "qwen"
	ProviderDoubao      = "doubao"
	ProviderOpenRouter  = "openrouter"
	ProviderGroq        = "groq"
	ProviderXAI         = "xai"
	ProviderSiliconFlow = "siliconflow"
	ProviderOllama      = "ollama"
	// ProviderCustomOpenAI / ProviderCustomAnthropic 自建或未收录的兼容端点。
	// 目录永远追不上市场上的每一家，这两档保证「追不上」不等于「接不了」。
	ProviderCustomOpenAI    = "custom-openai"
	ProviderCustomAnthropic = "custom-anthropic"
)

// ── 配置字段类型（驱动控制台动态表单，与邮件目录同一组取值）──
const (
	FieldText     = "text"
	FieldSecret   = "secret"
	FieldNumber   = "number"
	FieldSwitch   = "switch"
	FieldSelect   = "select"
	FieldTextarea = "textarea"
	FieldURL      = "url"
	FieldKV       = "kv"
)

// ── 字段分区 ──
const (
	GroupCredential = "credential" // 服务商凭据
	GroupEndpoint   = "endpoint"   // 端点与模型
	GroupAdvanced   = "advanced"   // 高级选项
)

var GroupNames = map[string]string{
	GroupCredential: "服务商凭据",
	GroupEndpoint:   "端点与模型",
	GroupAdvanced:   "高级选项",
}

// ── 供应商分组（控制台「供应商市场」按此归类）──
const (
	CategoryFrontier   = "frontier"   // 国际前沿模型厂
	CategoryChina      = "china"      // 国内模型厂
	CategoryAggregator = "aggregator" // 聚合网关
	CategoryLocal      = "local"      // 本地推理
	CategoryCustom     = "custom"     // 自定义兼容端点
)

var CategoryNames = map[string]string{
	CategoryFrontier:   "国际服务商",
	CategoryChina:      "国内服务商",
	CategoryAggregator: "聚合网关",
	CategoryLocal:      "本地推理",
	CategoryCustom:     "自定义",
}

// ── 通用配置字段键 ──
//
// 与邮件的发件人三件套同理：这些键同时出现在目录声明、客户端读取处与控制台表单里，
// 拼错任何一处的表现是「配置看着都在、请求却发到了默认端点」而不报错。
const (
	KeyAPIKey       = "apiKey"
	KeyBaseURL      = "baseUrl"
	KeyModels       = "models"
	KeyDefaultModel = "defaultModel"
	KeyExtraHeaders = "extraHeaders"
	KeyAPIVersion   = "apiVersion"
	// KeyMaxContextChars 自动压缩阈值（估算字符数）。留空用平台默认。
	KeyMaxContextChars = "maxContextChars"
)

type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
}

// ConfigField 单个配置项的声明式描述。
// Secret 为 true 的字段沿用平台三条固定语义：AES-GCM 加密落库、出网抹除、留空即不修改。
type ConfigField struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        string        `json:"type"`
	Group       string        `json:"group,omitempty"`
	Required    bool          `json:"required,omitempty"`
	Secret      bool          `json:"secret,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Default     any           `json:"default,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
	Advanced    bool          `json:"advanced,omitempty"`
}

// ProviderCapabilities 一家供应商的能力自述。
//
// 必须**如实**反映：Agent 会先问「这条链路能不能带工具」再决定要不要把
// 工具清单塞进请求 —— 问到假答案的表现是模型把工具 JSON 当正文复述出来。
type ProviderCapabilities struct {
	Streaming bool `json:"streaming"` // SSE 流式输出
	ToolCalls bool `json:"toolCalls"` // 原生工具调用
	Vision    bool `json:"vision"`    // 图像输入
	JSONMode  bool `json:"jsonMode"`  // 强制 JSON 输出
	Reasoning bool `json:"reasoning"` // 显式思考块（reasoning / thinking）
}

// ProviderMeta 一家 AI 供应商的完整描述。
// 经 /providers 目录接口下发给控制台，驱动「供应商卡片 + 动态配置表单」。
type ProviderMeta struct {
	Provider     string               `json:"provider"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Category     string               `json:"category,omitempty"`
	CategoryName string               `json:"categoryName,omitempty"`
	// Protocol 该供应商走哪种线上协议（openai / anthropic）。
	Protocol   string `json:"protocol"`
	Icon       string `json:"icon,omitempty"`       // Simple Icons slug
	BrandColor string `json:"brandColor,omitempty"` // #RRGGBB
	DocURL     string `json:"docUrl,omitempty"`
	// DefaultBaseURL baseUrl 留空时使用的端点根地址。
	DefaultBaseURL string               `json:"defaultBaseUrl,omitempty"`
	Capabilities   ProviderCapabilities `json:"capabilities"`
	Fields         []ConfigField        `json:"fields,omitempty"`
	// SuggestedModels 建议的型号清单，仅用于控制台预填 —— 不是白名单。
	SuggestedModels []string `json:"suggestedModels,omitempty"`
	// Notes 接入注意事项，逐条显示在配置表单顶部。
	Notes []string `json:"notes,omitempty"`
}

// SecretKeys 返回该供应商声明为密钥的字段键，服务层据此决定加密与抹除范围。
func (m ProviderMeta) SecretKeys() []string {
	keys := make([]string, 0, 2)
	for _, field := range m.Fields {
		if field.Secret {
			keys = append(keys, field.Key)
		}
	}
	return keys
}

func (m ProviderMeta) Field(key string) (ConfigField, bool) {
	for _, field := range m.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return ConfigField{}, false
}

// commonFields 所有供应商共有的字段。apiKeyRequired 控制密钥是否必填
// （Ollama 与自定义端点可以没有）。
func commonFields(apiKeyRequired bool, baseURLHelp string) []ConfigField {
	return []ConfigField{
		{
			Key: KeyAPIKey, Label: "API Key", Type: FieldSecret,
			Group: GroupCredential, Required: apiKeyRequired, Secret: true,
			Help: "在服务商控制台创建的访问密钥，加密存储、永不回显",
		},
		{
			Key: KeyBaseURL, Label: "端点地址", Type: FieldURL,
			Group: GroupEndpoint, Help: baseURLHelp,
		},
		{
			Key: KeyModels, Label: "可用型号", Type: FieldTextarea,
			Group: GroupEndpoint,
			Help:  "每行一个型号标识；第一行即该配置的默认型号（也可在下面单独指定）",
		},
		{
			Key: KeyDefaultModel, Label: "默认型号", Type: FieldText,
			Group: GroupEndpoint, Help: "留空时取「可用型号」的第一行",
		},
		{
			Key: KeyExtraHeaders, Label: "附加请求头", Type: FieldKV,
			Group: GroupAdvanced, Advanced: true,
			Help: "随每个请求附带的自定义 HTTP 头（如网关鉴权、路由标记）",
		},
		{
			Key: KeyMaxContextChars, Label: "上下文压缩阈值", Type: FieldNumber,
			Group: GroupAdvanced, Advanced: true,
			Help: "会话正文估算超过该字符数时自动摘要压缩；留空用平台默认（约 12 万字符）",
		},
	}
}

// providers 顺序即控制台展示顺序：先国际、后国内、再聚合/本地/自定义。
var providers = []ProviderMeta{
	{
		Provider: ProviderOpenAI, Name: "OpenAI",
		Description: "GPT 系列官方 API",
		Category:    CategoryFrontier, Protocol: ProtocolOpenAI,
		Icon: "openai", BrandColor: "#10A37F",
		DocURL:         "https://platform.openai.com/docs/api-reference",
		DefaultBaseURL: "https://api.openai.com/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 https://api.openai.com/v1；走代理网关时填代理地址"),
		SuggestedModels: []string{
			"gpt-5.2", "gpt-5.2-mini", "gpt-5.1", "gpt-4.1", "gpt-4o",
		},
	},
	{
		Provider: ProviderAnthropic, Name: "Anthropic",
		Description: "Claude 系列官方 API",
		Category:    CategoryFrontier, Protocol: ProtocolAnthropic,
		Icon: "anthropic", BrandColor: "#D97706",
		DocURL:         "https://docs.anthropic.com/en/api/messages",
		DefaultBaseURL: "https://api.anthropic.com/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, Reasoning: true},
		Fields:         commonFields(true, "留空使用 https://api.anthropic.com/v1"),
		SuggestedModels: []string{
			"claude-opus-4-5", "claude-sonnet-4-5", "claude-haiku-4-5",
		},
	},
	{
		Provider: ProviderAzureOpenAI, Name: "Azure OpenAI",
		Description: "微软 Azure 托管的 OpenAI 服务",
		Category:    CategoryFrontier, Protocol: ProtocolOpenAI,
		Icon: "microsoftazure", BrandColor: "#0078D4",
		DocURL:       "https://learn.microsoft.com/azure/ai-services/openai/reference",
		Capabilities: ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields: append(commonFields(true,
			"资源端点，如 https://{resource}.openai.azure.com；型号名即部署名（deployment）"),
			ConfigField{
				Key: KeyAPIVersion, Label: "API 版本", Type: FieldText,
				Group: GroupEndpoint, Placeholder: "2024-10-21",
				Help: "Azure 的 api-version 查询参数，留空用 2024-10-21",
			}),
		Notes: []string{"「可用型号」里填的是**部署名**而不是模型名 —— Azure 按部署路由请求。"},
	},
	{
		Provider: ProviderGemini, Name: "Google Gemini",
		Description: "Gemini 系列（OpenAI 兼容端点）",
		Category:    CategoryFrontier, Protocol: ProtocolOpenAI,
		Icon: "googlegemini", BrandColor: "#4285F4",
		DocURL:         "https://ai.google.dev/gemini-api/docs/openai",
		DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用官方 OpenAI 兼容端点"),
		SuggestedModels: []string{
			"gemini-3-pro", "gemini-3-flash", "gemini-2.5-pro", "gemini-2.5-flash",
		},
	},
	{
		Provider: ProviderDeepSeek, Name: "DeepSeek",
		Description: "深度求索官方 API",
		Category:    CategoryChina, Protocol: ProtocolOpenAI,
		Icon: "deepseek", BrandColor: "#4D6BFE",
		DocURL:         "https://api-docs.deepseek.com",
		DefaultBaseURL: "https://api.deepseek.com/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, JSONMode: true, Reasoning: true},
		Fields:         commonFields(true, "留空使用 https://api.deepseek.com/v1"),
		SuggestedModels: []string{
			"deepseek-chat", "deepseek-reasoner",
		},
	},
	{
		Provider: ProviderMoonshot, Name: "Moonshot Kimi",
		Description: "月之暗面 Kimi 开放平台",
		Category:    CategoryChina, Protocol: ProtocolOpenAI,
		Icon: "moonshotai", BrandColor: "#16191E",
		DocURL:         "https://platform.moonshot.cn/docs",
		DefaultBaseURL: "https://api.moonshot.cn/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 https://api.moonshot.cn/v1"),
		SuggestedModels: []string{
			"kimi-k2.5", "kimi-k2", "moonshot-v1-32k",
		},
	},
	{
		Provider: ProviderZhipu, Name: "智谱 GLM",
		Description: "智谱开放平台 BigModel",
		Category:    CategoryChina, Protocol: ProtocolOpenAI,
		Icon: "zhipu", BrandColor: "#3859FF",
		DocURL:         "https://open.bigmodel.cn/dev/api",
		DefaultBaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 https://open.bigmodel.cn/api/paas/v4"),
		SuggestedModels: []string{
			"glm-5", "glm-4.7", "glm-4.6v",
		},
	},
	{
		Provider: ProviderQwen, Name: "通义千问",
		Description: "阿里云百炼 DashScope（兼容模式）",
		Category:    CategoryChina, Protocol: ProtocolOpenAI,
		Icon: "alibabacloud", BrandColor: "#FF6A00",
		DocURL:         "https://help.aliyun.com/zh/model-studio/",
		DefaultBaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 DashScope OpenAI 兼容端点"),
		SuggestedModels: []string{
			"qwen3.5-max", "qwen3.5-plus", "qwen3.5-flash",
		},
	},
	{
		Provider: ProviderDoubao, Name: "豆包（火山方舟）",
		Description: "字节跳动火山引擎方舟",
		Category:    CategoryChina, Protocol: ProtocolOpenAI,
		Icon: "bytedance", BrandColor: "#325AB4",
		DocURL:         "https://www.volcengine.com/docs/82379",
		DefaultBaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用华北区端点；型号填接入点 ID（ep-…）或开放模型名"),
		SuggestedModels: []string{
			"doubao-seed-2.0", "doubao-seed-1.8",
		},
	},
	{
		Provider: ProviderOpenRouter, Name: "OpenRouter",
		Description: "多厂商聚合网关，一把钥匙调数百个模型",
		Category:    CategoryAggregator, Protocol: ProtocolOpenAI,
		Icon: "openrouter", BrandColor: "#8B5CF6",
		DocURL:         "https://openrouter.ai/docs",
		DefaultBaseURL: "https://openrouter.ai/api/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true, Reasoning: true},
		Fields:         commonFields(true, "留空使用 https://openrouter.ai/api/v1"),
		SuggestedModels: []string{
			"anthropic/claude-sonnet-4.5", "openai/gpt-5.2", "google/gemini-3-pro",
		},
	},
	{
		Provider: ProviderGroq, Name: "Groq",
		Description: "LPU 高速推理云",
		Category:    CategoryAggregator, Protocol: ProtocolOpenAI,
		Icon: "groq", BrandColor: "#F55036",
		DocURL:         "https://console.groq.com/docs",
		DefaultBaseURL: "https://api.groq.com/openai/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 https://api.groq.com/openai/v1"),
		SuggestedModels: []string{
			"llama-4-maverick", "qwen-3-coder-480b",
		},
	},
	{
		Provider: ProviderXAI, Name: "xAI Grok",
		Description: "xAI 官方 API",
		Category:    CategoryFrontier, Protocol: ProtocolOpenAI,
		Icon: "x", BrandColor: "#000000",
		DocURL:         "https://docs.x.ai",
		DefaultBaseURL: "https://api.x.ai/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true, Reasoning: true},
		Fields:         commonFields(true, "留空使用 https://api.x.ai/v1"),
		SuggestedModels: []string{
			"grok-4.2", "grok-4.2-mini",
		},
	},
	{
		Provider: ProviderSiliconFlow, Name: "硅基流动",
		Description: "SiliconFlow 开源模型云",
		Category:    CategoryAggregator, Protocol: ProtocolOpenAI,
		Icon: "siliconflow", BrandColor: "#7C3AED",
		DocURL:         "https://docs.siliconflow.cn",
		DefaultBaseURL: "https://api.siliconflow.cn/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:         commonFields(true, "留空使用 https://api.siliconflow.cn/v1"),
		SuggestedModels: []string{
			"deepseek-ai/DeepSeek-V3.2", "Qwen/Qwen3.5-72B-Instruct",
		},
	},
	{
		Provider: ProviderOllama, Name: "Ollama",
		Description: "本地/内网自托管推理",
		Category:    CategoryLocal, Protocol: ProtocolOpenAI,
		Icon: "ollama", BrandColor: "#111111",
		DocURL:         "https://github.com/ollama/ollama/blob/main/docs/openai.md",
		DefaultBaseURL: "http://127.0.0.1:11434/v1",
		Capabilities:   ProviderCapabilities{Streaming: true, ToolCalls: true, JSONMode: true},
		Fields:         commonFields(false, "Ollama 服务地址（含 /v1），如 http://10.0.0.8:11434/v1"),
		SuggestedModels: []string{
			"qwen3.5", "llama4", "deepseek-v3.2",
		},
		Notes: []string{"本地端点没有鉴权时 API Key 可留空；生产环境务必放在内网并配好访问控制。"},
	},
	{
		Provider: ProviderCustomOpenAI, Name: "自定义（OpenAI 兼容）",
		Description: "任何暴露 /chat/completions 的兼容端点",
		Category:    CategoryCustom, Protocol: ProtocolOpenAI,
		Icon: "openai", BrandColor: "#64748B",
		Capabilities: ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, JSONMode: true},
		Fields:       withRequiredBaseURL(commonFields(false, "端点根地址（不含 /chat/completions）")),
	},
	{
		Provider: ProviderCustomAnthropic, Name: "自定义（Anthropic 兼容）",
		Description: "任何暴露 /messages 的兼容端点",
		Category:    CategoryCustom, Protocol: ProtocolAnthropic,
		Icon: "anthropic", BrandColor: "#64748B",
		Capabilities: ProviderCapabilities{Streaming: true, ToolCalls: true, Vision: true, Reasoning: true},
		Fields:       withRequiredBaseURL(commonFields(false, "端点根地址（不含 /messages）")),
	},
}

// withRequiredBaseURL 把 baseUrl 字段改成必填 —— 自定义端点没有默认地址可回落。
func withRequiredBaseURL(fields []ConfigField) []ConfigField {
	for i := range fields {
		if fields[i].Key == KeyBaseURL {
			fields[i].Required = true
		}
	}
	return fields
}

// Providers 返回目录（拷贝，调用方改不到目录本身），并补齐分组显示名。
func Providers() []ProviderMeta {
	out := make([]ProviderMeta, len(providers))
	copy(out, providers)
	for i := range out {
		out[i].CategoryName = CategoryNames[out[i].Category]
	}
	return out
}

// ProviderByKey 按标识取供应商描述。
func ProviderByKey(key string) (ProviderMeta, bool) {
	for _, meta := range providers {
		if meta.Provider == key {
			meta.CategoryName = CategoryNames[meta.Category]
			return meta, true
		}
	}
	return ProviderMeta{}, false
}

// KnownProvider 该标识是否在目录里。
func KnownProvider(key string) bool {
	_, ok := ProviderByKey(key)
	return ok
}
