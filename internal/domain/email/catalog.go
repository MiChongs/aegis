package email

// 邮件服务商的**自描述**元数据。
//
// 与支付渠道的 `Provider.Describe()`、风控条件目录、远程函数能力目录同一套做法：
// 一份目录同时驱动服务端校验、控制台配置表单、以及「这条通道能不能带附件」这类
// 调用方要先问清楚的能力。因此新增一家服务商只需在 Go 侧补一个发送器 +
// 一份 Describe()，控制台零改动即自动出现。
//
// 放在 domain 而不是 service，是因为 transport 层要把它序列化下发，
// 而 transport 不允许 import service 之外的实现细节。

// ── 配置字段类型（驱动控制台动态表单）──
const (
	FieldText     = "text"     // 单行文本
	FieldSecret   = "secret"   // 密文：加密落库、出网抹除、留空即不修改
	FieldNumber   = "number"   // 数值
	FieldSwitch   = "switch"   // 布尔开关
	FieldSelect   = "select"   // 单选下拉
	FieldTextarea = "textarea" // 多行文本（证书 / 私钥）
	FieldEmail    = "email"    // 邮箱地址（服务端会 mail.ParseAddress 校验）
	FieldURL      = "url"      // URL 文本
	FieldKV       = "kv"       // 键值对集合，值以 JSON 对象字符串存放
)

// ── 字段分区（控制台按声明顺序分区渲染）──
const (
	GroupCredential = "credential" // 服务商凭据
	GroupSender     = "sender"     // 发件人身份
	GroupWebhook    = "webhook"    // 投递回执
	GroupAdvanced   = "advanced"   // 高级选项
)

// GroupNames 分区标识 → 中文名。控制台直接用，不再各自维护一份。
var GroupNames = map[string]string{
	GroupCredential: "服务商凭据",
	GroupSender:     "发件人身份",
	GroupWebhook:    "投递回执",
	GroupAdvanced:   "高级选项",
}

// ── 服务商分组（控制台的「服务商市场」按此归类）──
const (
	CategoryDirect        = "direct"        // 自有服务器直连
	CategoryInternational = "international" // 国际邮件服务
	CategoryChina         = "china"         // 国内云厂商
	CategoryPlatform      = "platform"      // 部署平台自带
)

var CategoryNames = map[string]string{
	CategoryDirect:        "直连",
	CategoryInternational: "国际服务商",
	CategoryChina:         "国内云厂商",
	CategoryPlatform:      "部署平台",
}

// FieldOption 下拉选项。
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
}

// ConfigField 单个配置项的声明式描述。
//
// Secret 为 true 的字段有三条固定语义，服务端强制执行，控制台不必各自实现：
// 值以 AES-GCM 加密落库、任何出网响应一律抹除、提交时留空表示「不修改」。
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

// ProviderCapabilities 一条通道的能力自述。
//
// 必须**如实**反映，不能乐观地一律 true：调用方是先问能力再决定正文措辞的
// （「收据见附件」还是「点这里下载」），问到假答案的表现是
// 「邮件正常送达、附件不翼而飞」，用户和运维都不会收到任何错误。
type ProviderCapabilities struct {
	Attachments bool `json:"attachments"` // 能否投递附件
	Webhook     bool `json:"webhook"`     // 有没有投递回执回调
	Tags        bool `json:"tags"`        // 支持给邮件打标签（服务商控制台按标签筛选）
	Tracking    bool `json:"tracking"`    // 支持打开 / 点击追踪
}

// ProviderMeta 一家邮件服务商的完整描述。
// 经 /providers 目录接口下发给控制台，驱动「服务商卡片 + 动态配置表单 + 回调地址提示」。
type ProviderMeta struct {
	Provider     string               `json:"provider"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Category     string               `json:"category,omitempty"`
	CategoryName string               `json:"categoryName,omitempty"`
	Icon         string               `json:"icon,omitempty"`       // Simple Icons slug
	BrandColor   string               `json:"brandColor,omitempty"` // #RRGGBB
	DocURL       string               `json:"docUrl,omitempty"`
	Capabilities ProviderCapabilities `json:"capabilities"`
	Fields       []ConfigField        `json:"fields,omitempty"`

	// WebhookPath 投递回执的回调路径模板，空串表示这家没有回执回调。
	//
	// 三个占位符由控制台替换：
	//   {scope}  应用 id，或字面量 `platform`
	//   {config} 配置名；为空时连同前面那个 `/` 一起去掉（落到该作用域的默认配置）
	//   {token}  回调令牌，即该配置的 Webhook 密钥
	//
	// `{token}` 只出现在**没有官方签名机制**的服务商上（Postmark / 阿里云 / 腾讯云）。
	// 那几家的回执报文里没有任何可验证的凭据，唯一能证明「这条回执确实来自服务商」
	// 的东西就是地址本身 —— 因此该地址等同于密钥，控制台会就此明确提示。
	WebhookPath string `json:"webhookPath,omitempty"`
	WebhookNote string `json:"webhookNote,omitempty"`

	// Notes 接入注意事项，逐条显示在配置表单顶部。
	Notes []string `json:"notes,omitempty"`
}

// SecretKeys 返回该服务商声明为密钥的字段键。
// 服务层据此决定「哪些值要加密、哪些值出网要抹掉」——
// 判据来自目录而不是散在各处的 if，新增密钥字段不会漏掉其中一处。
func (m ProviderMeta) SecretKeys() []string {
	keys := make([]string, 0, 4)
	for _, field := range m.Fields {
		if field.Secret {
			keys = append(keys, field.Key)
		}
	}
	return keys
}

// Field 按键取字段声明。
func (m ProviderMeta) Field(key string) (ConfigField, bool) {
	for _, field := range m.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return ConfigField{}, false
}
