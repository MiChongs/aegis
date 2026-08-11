package notify

import "time"

// ─────────────── 渠道类型 ───────────────

const (
	KindFeishuBot    = "feishu_bot"    // 飞书群自定义机器人（Webhook + 加签）
	KindFeishuApp    = "feishu_app"    // 飞书企业自建应用（tenant_access_token + im/v1/messages）
	KindDingtalkBot  = "dingtalk_bot"  // 钉钉群自定义机器人
	KindWecomBot     = "wecom_bot"     // 企业微信群机器人
	KindSlackWebhook = "slack_webhook" // Slack Incoming Webhook
	KindWebhook      = "webhook"       // 通用 JSON Webhook（HMAC 签名）
	KindEmail        = "email"         // 邮件（复用 EmailService 出口）
	KindInApp        = "inapp"         // 站内信（写入 notifications 表）
	KindRealtime     = "realtime"      // WebSocket 实时推送
)

// ValidKinds 受支持的渠道类型。
var ValidKinds = map[string]struct{}{
	KindFeishuBot: {}, KindFeishuApp: {}, KindDingtalkBot: {}, KindWecomBot: {},
	KindSlackWebhook: {}, KindWebhook: {}, KindEmail: {}, KindInApp: {}, KindRealtime: {},
}

// KindNeedsSecret 该渠道是否使用 secret_cipher 存放敏感凭据。
var KindNeedsSecret = map[string]bool{
	KindFeishuBot:    true, // 加签密钥（可选）
	KindFeishuApp:    true, // app_secret（必填）
	KindDingtalkBot:  true, // 加签密钥（可选）
	KindWecomBot:     false,
	KindSlackWebhook: false,
	KindWebhook:      true, // HMAC 签名密钥（可选）
	KindEmail:        false,
	KindInApp:        false,
	KindRealtime:     false,
}

// 投递状态
const (
	DeliveryPending = "pending"
	DeliverySuccess = "success"
	DeliveryFailed  = "failed"
	DeliverySkipped = "skipped" // 命中静默窗口 / 过滤条件
	DeliveryDropped = "dropped" // 重试耗尽或渠道已禁用
)

// 事件级别
const (
	LevelInfo     = "info"
	LevelWarning  = "warning"
	LevelCritical = "critical"
	LevelSuccess  = "success"
)

// ─────────────── 事件 key ───────────────

// 工单相关事件。业务侧只发这些 key，路由由订阅表决定。
const (
	EventTicketCreated       = "ticket.created"
	EventTicketReplied       = "ticket.replied"        // 提单人追加回复
	EventTicketAgentReplied  = "ticket.agent_replied"  // 处理人对外回复
	EventTicketAssigned      = "ticket.assigned"
	EventTicketStatusChanged = "ticket.status_changed"
	EventTicketResolved      = "ticket.resolved"
	EventTicketClosed        = "ticket.closed"
	EventTicketReopened      = "ticket.reopened"
	EventTicketEscalated     = "ticket.escalated" // 优先级升到 urgent
	EventTicketSLAWarning    = "ticket.sla.warning"
	EventTicketSLABreached   = "ticket.sla.breached"
	EventTicketRated         = "ticket.rated"
	EventTicketOverdueDigest = "ticket.overdue.digest" // 定时汇总
)

// KnownEvents 事件目录，供控制台下拉选择与订阅校验。
var KnownEvents = []EventMeta{
	{Key: EventTicketCreated, Name: "工单创建", Group: "工单", Description: "新工单提交时触发"},
	{Key: EventTicketReplied, Name: "提单人回复", Group: "工单", Description: "提单人追加了新的回复"},
	{Key: EventTicketAgentReplied, Name: "处理人回复", Group: "工单", Description: "处理人发出对外回复"},
	{Key: EventTicketAssigned, Name: "工单指派", Group: "工单", Description: "工单被指派到人或处理组"},
	{Key: EventTicketStatusChanged, Name: "状态变更", Group: "工单", Description: "工单状态发生流转"},
	{Key: EventTicketResolved, Name: "工单已解决", Group: "工单", Description: "处理人标记为已解决"},
	{Key: EventTicketClosed, Name: "工单关闭", Group: "工单", Description: "工单被关闭"},
	{Key: EventTicketReopened, Name: "工单重开", Group: "工单", Description: "已解决/关闭的工单被重新打开"},
	{Key: EventTicketEscalated, Name: "工单升级", Group: "工单", Description: "优先级被提升为紧急"},
	{Key: EventTicketSLAWarning, Name: "SLA 预警", Group: "SLA", Description: "接近首响或解决时限"},
	{Key: EventTicketSLABreached, Name: "SLA 超时", Group: "SLA", Description: "已超出首响或解决时限"},
	{Key: EventTicketRated, Name: "满意度评价", Group: "工单", Description: "提单人提交了评价"},
	{Key: EventTicketOverdueDigest, Name: "超时汇总", Group: "SLA", Description: "定时汇总当前超时工单"},
}

// EventMeta 事件目录条目。
type EventMeta struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
}

// ─────────────── 实体 ───────────────

// Channel 渠道实例。Secret 字段永不出网，仅回传 SecretSet / SecretHint。
type Channel struct {
	ID          int64          `json:"id"`
	AppID       int64          `json:"appid"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Kind        string         `json:"kind"`
	Config      map[string]any `json:"config"`
	Secret      string         `json:"-"`
	SecretSet   bool           `json:"secretSet"`
	SecretHint  string         `json:"secretHint,omitempty"`
	Enabled     bool           `json:"enabled"`
	RateLimit   int            `json:"rateLimitPerMinute"`
	LastStatus  string         `json:"lastStatus,omitempty"`
	LastError   string         `json:"lastError,omitempty"`
	LastSentAt  *time.Time     `json:"lastSentAt,omitempty"`
	CreatedBy   *int64         `json:"createdBy,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	Subscriptions []Subscription `json:"subscriptions,omitempty"`
}

// ChannelMutation 渠道新建/更新入参。指针语义：nil = 不改。
type ChannelMutation struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	Key       *string        `json:"key,omitempty"`
	Name      *string        `json:"name,omitempty"`
	Kind      *string        `json:"kind,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	// Secret 为空字符串表示不改；显式传 "-" 表示清空
	Secret    *string `json:"secret,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
	RateLimit *int    `json:"rateLimitPerMinute,omitempty"`
	CreatedBy *int64  `json:"-"`
}

// Subscription 事件订阅路由。
type Subscription struct {
	ID          int64          `json:"id"`
	ChannelID   int64          `json:"channelId"`
	ChannelName string         `json:"channelName,omitempty"`
	ChannelKind string         `json:"channelKind,omitempty"`
	EventKey    string         `json:"eventKey"`
	AppID       *int64         `json:"appid,omitempty"`
	MinPriority string         `json:"minPriority,omitempty"`
	CategoryIDs []int64        `json:"categoryIds,omitempty"`
	TemplateID  *int64         `json:"templateId,omitempty"`
	QuietHours  *QuietHours    `json:"quietHours,omitempty"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// SubscriptionMutation 订阅新建/更新入参。
type SubscriptionMutation struct {
	ID          int64       `json:"id"`
	ChannelID   int64       `json:"channelId"`
	EventKey    string      `json:"eventKey"`
	AppID       *int64      `json:"appid,omitempty"`
	MinPriority string      `json:"minPriority,omitempty"`
	CategoryIDs []int64     `json:"categoryIds,omitempty"`
	TemplateID  *int64      `json:"templateId,omitempty"`
	QuietHours  *QuietHours `json:"quietHours,omitempty"`
	Enabled     *bool       `json:"enabled,omitempty"`
}

// QuietHours 静默窗口。跨零点（如 23:00→08:00）由服务层处理。
type QuietHours struct {
	Timezone string `json:"timezone"`
	Start    string `json:"start"`
	End      string `json:"end"`
}

// Template 通知模板。
type Template struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	EventKey      string         `json:"eventKey"`
	ChannelKind   string         `json:"channelKind,omitempty"`
	TitleTemplate string         `json:"titleTemplate"`
	BodyTemplate  string         `json:"bodyTemplate"`
	CardTemplate  map[string]any `json:"cardTemplate,omitempty"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// TemplateMutation 模板新建/更新入参。
type TemplateMutation struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Key           *string        `json:"key,omitempty"`
	Name          *string        `json:"name,omitempty"`
	EventKey      *string        `json:"eventKey,omitempty"`
	ChannelKind   *string        `json:"channelKind,omitempty"`
	TitleTemplate *string        `json:"titleTemplate,omitempty"`
	BodyTemplate  *string        `json:"bodyTemplate,omitempty"`
	CardTemplate  map[string]any `json:"cardTemplate,omitempty"`
	Enabled       *bool          `json:"enabled,omitempty"`
}

// Delivery 投递记录。
type Delivery struct {
	ID              int64      `json:"id"`
	ChannelID       *int64     `json:"channelId,omitempty"`
	ChannelName     string     `json:"channelName,omitempty"`
	ChannelKind     string     `json:"channelKind"`
	EventKey        string     `json:"eventKey"`
	AppID           int64      `json:"appid"`
	Resource        string     `json:"resource,omitempty"`
	ResourceID      string     `json:"resourceId,omitempty"`
	DedupeKey       string     `json:"dedupeKey,omitempty"`
	Status          string     `json:"status"`
	Attempt         int        `json:"attempt"`
	RequestSnippet  string     `json:"requestSnippet,omitempty"`
	ResponseSnippet string     `json:"responseSnippet,omitempty"`
	Error           string     `json:"error,omitempty"`
	LatencyMs       int        `json:"latencyMs"`
	CreatedAt       time.Time  `json:"createdAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

// DeliveryQuery 投递记录查询条件。
type DeliveryQuery struct {
	ChannelID  *int64 `json:"channelId,omitempty"`
	EventKey   string `json:"eventKey,omitempty"`
	Status     string `json:"status,omitempty"`
	Resource   string `json:"resource,omitempty"`
	ResourceID string `json:"resourceId,omitempty"`
	AppID      *int64 `json:"appid,omitempty"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
}

// DeliveryPage 投递记录分页。
type DeliveryPage struct {
	Items      []Delivery `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

// DeliveryStats 渠道健康度概览。
type DeliveryStats struct {
	Total      int64            `json:"total"`
	Success    int64            `json:"success"`
	Failed     int64            `json:"failed"`
	Skipped    int64            `json:"skipped"`
	SuccessPct float64          `json:"successPct"`
	AvgLatency int64            `json:"avgLatencyMs"`
	ByKind     map[string]int64 `json:"byKind"`
	ByEvent    map[string]int64 `json:"byEvent"`
}

// ─────────────── 投递消息 ───────────────

// Field 结构化字段，渲染成飞书/钉钉卡片的字段区或 Webhook 的 fields 数组。
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
	// Short=true 时卡片内两列并排展示
	Short bool `json:"short"`
}

// Action 卡片按钮。
type Action struct {
	Text  string `json:"text"`
	URL   string `json:"url"`
	Style string `json:"style,omitempty"` // primary / default / danger
}

// Event 统一通知事件。业务层构造它并交给 Hub，不关心下游渠道差异。
type Event struct {
	Key     string `json:"key"`
	AppID   int64  `json:"appid"`
	AppName string `json:"appName,omitempty"`
	Level   string `json:"level"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// Link 面向「处理侧」的控制台深链（如 /tickets?id=42）。
	// IM / 邮件 / 管理员收件箱用它；绝不能下发给应用用户。
	Link string `json:"link,omitempty"`
	// UserLink 面向「提单人」的应用侧路径（如 /tickets/42）。
	// 站内信与实时推送给应用用户时用它；为空则不带链接。
	//
	// 两条链接必须分开：控制台路径对 App 用户既点不开，也泄露内部路由结构。
	UserLink string   `json:"userLink,omitempty"`
	Fields   []Field  `json:"fields,omitempty"`
	Actions  []Action `json:"actions,omitempty"`
	// UserTitle / UserSummary 面向提单人的文案覆盖。
	// 为空时回落到 Title / Summary —— 有些事件（如指派、SLA 超时）压根不该发给用户，
	// 由 Recipients 控制；而「已回复」这类事件两侧措辞不同，用这两个字段区分。
	UserTitle   string `json:"userTitle,omitempty"`
	UserSummary string `json:"userSummary,omitempty"`
	// Type 业务域标识（ticket / security / system…），写入收件箱的 type 字段
	Type string `json:"type,omitempty"`
	// Priority 参与订阅的 min_priority 过滤
	Priority string `json:"priority,omitempty"`
	// CategoryID 参与订阅的 category_ids 过滤
	CategoryID *int64 `json:"categoryId,omitempty"`
	// Resource/ResourceID 写入投递记录，便于按工单回查
	Resource   string `json:"resource,omitempty"`
	ResourceID string `json:"resourceId,omitempty"`
	// DedupeKey 幂等键；同一键对同一渠道只投一次
	DedupeKey string `json:"dedupeKey,omitempty"`
	// Vars 模板变量
	Vars map[string]any `json:"vars,omitempty"`
	// Recipients 面向"人"的渠道（email/inapp/realtime）的收件目标
	Recipients Recipients `json:"recipients"`
	// ChannelIDs 非空时绕过订阅表，定向投递到指定渠道（用于「测试发送」）
	ChannelIDs []int64 `json:"-"`
}

// Recipients 面向人的投递目标。
type Recipients struct {
	// Emails 直接邮件地址
	Emails []string `json:"emails,omitempty"`
	// UserIDs 应用用户（站内信 / 实时推送）
	UserIDs []int64 `json:"userIds,omitempty"`
	// AdminIDs 管理员（站内信落库为管理员通知，邮件取账号邮箱）
	AdminIDs []int64 `json:"adminIds,omitempty"`
	// FeishuUserIDs / FeishuOpenIDs 飞书应用渠道的单聊目标
	FeishuOpenIDs []string `json:"feishuOpenIds,omitempty"`
}

// Message 渲染后的待投递消息，交给 Provider。
// 内部类型：只在 Hub → Provider 之间传递，不经任何 handler 出网，故不加 json tag。
type Message struct {
	Event   *Event
	Title   string
	Body    string
	// Card 由模板 card_template 提供的结构化覆盖；为空时 provider 自行构建
	Card map[string]any
}

// Result 单次投递结果。
// Result 会经 POST /api/admin/notify/channels/{id}/test 直接出网，
// 因此必须带 camelCase tag —— 缺了 tag 时 Go 按字段名序列化成 LatencyMs，
// 前端读 latencyMs 得到 undefined（曾导致「测试消息已发送（undefinedms）」）。
type Result struct {
	Status          string `json:"status"`
	RequestSnippet  string `json:"requestSnippet,omitempty"`
	ResponseSnippet string `json:"responseSnippet,omitempty"`
	Error           string `json:"error,omitempty"`
	LatencyMs       int    `json:"latencyMs"`
}

// ChannelKindMeta 渠道类型说明，供控制台渲染配置表单。
type ChannelKindMeta struct {
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	NeedsSecret bool         `json:"needsSecret"`
	SecretLabel string       `json:"secretLabel,omitempty"`
	Fields      []ConfigField `json:"fields"`
	DocURL      string       `json:"docUrl,omitempty"`
}

// ConfigField 渠道配置项定义。
type ConfigField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text / password / select / switch / textarea / tags
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Help        string   `json:"help,omitempty"`
	Options     []string `json:"options,omitempty"`
	Default     string   `json:"default,omitempty"`
}

// ChannelKinds 渠道类型目录（编译进二进制的静态元数据，控制台据此动态渲染表单）。
var ChannelKinds = []ChannelKindMeta{
	{
		Kind: KindFeishuBot, Name: "飞书群机器人", NeedsSecret: true, SecretLabel: "加签密钥（Secret）",
		Description: "飞书群 → 设置 → 群机器人 → 添加自定义机器人，复制 Webhook 地址；安全设置选「签名校验」时填入密钥。",
		DocURL:      "https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot",
		Fields: []ConfigField{
			{Key: "webhook", Label: "Webhook 地址", Type: "text", Required: true, Placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/xxxx"},
			{Key: "msgType", Label: "消息形态", Type: "select", Options: []string{"interactive", "text", "post"}, Default: "interactive", Help: "interactive = 富文本卡片（推荐）"},
			{Key: "atAll", Label: "@所有人", Type: "switch", Help: "仅在 critical 级别事件生效"},
			{Key: "atUserIds", Label: "@指定成员 open_id", Type: "tags", Help: "填写飞书 open_id，多个用回车分隔"},
		},
	},
	{
		Kind: KindFeishuApp, Name: "飞书企业应用", NeedsSecret: true, SecretLabel: "App Secret",
		Description: "企业自建应用，可发送到群聊或单聊，支持 @成员。需开通 im:message 权限。",
		DocURL:      "https://open.feishu.cn/document/server-docs/im-v1/message/create",
		Fields: []ConfigField{
			{Key: "appId", Label: "App ID", Type: "text", Required: true, Placeholder: "cli_xxxxxxxx"},
			{Key: "receiveIdType", Label: "接收方类型", Type: "select", Options: []string{"chat_id", "open_id", "user_id", "email"}, Default: "chat_id", Required: true},
			{Key: "receiveId", Label: "接收方 ID", Type: "text", Required: true, Placeholder: "oc_xxxxxxxx"},
			{Key: "msgType", Label: "消息形态", Type: "select", Options: []string{"interactive", "text"}, Default: "interactive"},
			{Key: "baseUrl", Label: "开放平台地址", Type: "text", Default: "https://open.feishu.cn", Help: "Lark 国际版填 https://open.larksuite.com"},
		},
	},
	{
		Kind: KindDingtalkBot, Name: "钉钉群机器人", NeedsSecret: true, SecretLabel: "加签密钥（SEC 开头）",
		Description: "钉钉群 → 智能群助手 → 添加机器人 → 自定义；安全设置选「加签」时填入密钥。",
		Fields: []ConfigField{
			{Key: "webhook", Label: "Webhook 地址", Type: "text", Required: true, Placeholder: "https://oapi.dingtalk.com/robot/send?access_token=xxx"},
			{Key: "atMobiles", Label: "@手机号", Type: "tags"},
			{Key: "atAll", Label: "@所有人", Type: "switch"},
		},
	},
	{
		Kind: KindWecomBot, Name: "企业微信群机器人", NeedsSecret: false,
		Description: "企业微信群 → 群机器人 → 添加，复制 Webhook 地址。",
		Fields: []ConfigField{
			{Key: "webhook", Label: "Webhook 地址", Type: "text", Required: true, Placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"},
			{Key: "atUserIds", Label: "@成员 userid", Type: "tags"},
		},
	},
	{
		Kind: KindSlackWebhook, Name: "Slack Webhook", NeedsSecret: false,
		Description: "Slack App → Incoming Webhooks，复制 Webhook URL。",
		Fields: []ConfigField{
			{Key: "webhook", Label: "Webhook URL", Type: "text", Required: true, Placeholder: "https://hooks.slack.com/services/..."},
		},
	},
	{
		Kind: KindWebhook, Name: "通用 Webhook", NeedsSecret: true, SecretLabel: "HMAC 签名密钥",
		Description: "POST 结构化 JSON 到自建服务；配置密钥后会带 X-Aegis-Signature（HMAC-SHA256）与 X-Aegis-Timestamp。",
		Fields: []ConfigField{
			{Key: "url", Label: "目标地址", Type: "text", Required: true, Placeholder: "https://your-service/hooks/aegis"},
			{Key: "method", Label: "方法", Type: "select", Options: []string{"POST", "PUT"}, Default: "POST"},
			{Key: "headers", Label: "附加请求头", Type: "textarea", Help: "每行一条，形如 X-Token: abc"},
		},
	},
	{
		Kind: KindEmail, Name: "邮件", NeedsSecret: false,
		Description: "复用应用已配置的邮件出口；收件人为空时取事件自带的收件目标。",
		Fields: []ConfigField{
			{Key: "configName", Label: "邮件配置名", Type: "text", Help: "留空使用该应用的默认邮件配置"},
			{Key: "to", Label: "固定收件人", Type: "tags", Help: "留空则按事件的收件目标发送"},
		},
	},
	{
		Kind: KindInApp, Name: "站内信", NeedsSecret: false,
		Description: "同时写入两个收件箱：应用用户 → 通知中心，管理员 → 控制台铃铛。" +
			"收件人由事件自身决定（提单人 / 受理人 / 关注人 / 处理组成员），无需在此配置。",
		Fields: []ConfigField{
			{Key: "type", Label: "通知分类", Type: "text", Default: "ticket",
				Help: "写入收件箱的 type 字段，用于前端分组；留空取事件自带分类"},
		},
	},
	{
		Kind: KindRealtime, Name: "实时推送", NeedsSecret: false,
		Description: "通过 WebSocket 推给在线的应用用户与管理员，页面无需刷新即可更新。" +
			"管理员走 appid=0 命名空间，与控制台长连接一致。",
		Fields: []ConfigField{
			{Key: "eventType", Label: "事件类型覆盖", Type: "text",
				Help: "留空则按事件 key 推送（如 ticket.assigned），前端可按类型订阅"},
		},
	},
}
