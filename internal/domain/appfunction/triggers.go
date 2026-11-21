package appfunction

import (
	"encoding/json"
	"slices"
	"time"
)

// 触发方式：远程函数除了被接入方调用（api），还能由平台事件与定时计划拉起。
//
// 两种自动触发共用一条持久队列（app_function_trigger_jobs）：钩子回调与定时扫描只负责
// 入队，执行由后台工作循环抢占（SKIP LOCKED）、失败按退避重试。直接在钩子回调里
// 跑脚本是不行的 —— 钩子是在注册、支付这些关键链路上同步触发的，脚本一慢就把它们拖慢，
// 进程一重启，内存里还没跑完的那批就悄悄没了。
const (
	TriggerKindEvent    = "event"
	TriggerKindSchedule = "schedule"
)

// 调用来源。写在调用记录上，控制台按它区分「接入方调的」「事件拉起的」「定时跑的」。
const (
	SourceAPI      = "api"
	SourceEvent    = "event"
	SourceSchedule = "schedule"
	SourceConsole  = "console"
	SourceFunction = "function"
)

// 调用者类型。
const (
	CallerUser     = "user"
	CallerKey      = "app"
	CallerAdmin    = "admin"
	CallerEvent    = "event"
	CallerSchedule = "schedule"
	CallerFunction = "function"
)

// 触发任务状态。
const (
	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// 触发器的上界：都是防手滑，不是业务规则。
const (
	MaxTriggersPerFunction = 16
	MaxTriggerAttempts     = 5
	// MinScheduleGapMinutes 定时触发相邻两次的最小间隔
	MinScheduleGapMinutes = 1
)

// 平台事件键。与插件钩子一一对应，但键名是给作者看的（钩子名是给插件看的）。
const (
	EventUserRegistered     = "user.registered"
	EventUserLogin          = "user.login"
	EventUserProfileUpdated = "user.profile_updated"
	EventUserBanned         = "user.banned"
	EventUserSignIn         = "user.signin"
	EventPaymentCompleted   = "payment.completed"
	EventPaymentRefunded    = "payment.refunded"
)

// TriggerEventField 事件自带的一项数据，脚本里读作 ctx.trigger.data.<key>。
type TriggerEventField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// TriggerEventSpec 一种平台事件的自述：控制台的事件下拉、编辑器里 ctx.trigger 的类型都读它。
type TriggerEventSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Group string `json:"group"`
	// Hook 对应的插件钩子名
	Hook string `json:"hook,omitempty"`
	// HasUser 事件带着一个应用用户：脚本里 RequiresUser 的能力照常可用（锁定到这个人）
	HasUser bool                `json:"hasUser"`
	Fields  []TriggerEventField `json:"fields"`
}

var triggerEventSpecs = []TriggerEventSpec{
	{Key: EventUserRegistered, Label: "用户注册", Group: "账号", Hook: "user.onRegistered", HasUser: true,
		Hint:   "新用户完成注册（账号密码、短信、第三方、卡密注册都算），同一个人只触发一次",
		Fields: []TriggerEventField{{Key: "account", Label: "账号", Type: "string"}}},
	{Key: EventUserLogin, Label: "用户登录", Group: "账号", Hook: "auth.onSessionIssued", HasUser: true,
		Hint:   "每次签发登录会话；频繁触发，脚本里自己按天去重（kv.user.incr + dayKey）",
		Fields: []TriggerEventField{{Key: "account", Label: "账号", Type: "string"}}},
	{Key: EventUserProfileUpdated, Label: "资料变更", Group: "账号", Hook: "user.onProfileUpdated", HasUser: true,
		Hint: "用户修改昵称、头像等资料之后", Fields: []TriggerEventField{}},
	{Key: EventUserBanned, Label: "用户被封禁", Group: "账号", Hook: "user.onBanned", HasUser: true,
		Hint:   "任何来源的封禁落成之后（管理员、风控、脚本）",
		Fields: []TriggerEventField{{Key: "reason", Label: "封禁原因", Type: "string"}}},
	{Key: EventUserSignIn, Label: "每日签到", Group: "成长", HasUser: true,
		Hint:   "用户完成当天签到（含自动签到）",
		Fields: []TriggerEventField{{Key: "consecutiveDays", Label: "连续签到天数", Type: "number"}}},
	{Key: EventPaymentCompleted, Label: "支付成功", Group: "交易", Hook: "payment.onCompleted", HasUser: true,
		Hint: "一笔在线支付订单确认到账（发货之后）",
		Fields: []TriggerEventField{
			{Key: "orderId", Label: "订单号", Type: "string"},
			{Key: "amount", Label: "金额", Type: "string"},
		}},
	{Key: EventPaymentRefunded, Label: "订单退款", Group: "交易", Hook: "payment.onRefunded", HasUser: true,
		Hint: "一笔退款确认成功",
		Fields: []TriggerEventField{
			{Key: "orderId", Label: "订单号", Type: "string"},
			{Key: "amount", Label: "退款金额", Type: "string"},
		}},
}

// TriggerEventCatalog 平台事件目录（副本）。
func TriggerEventCatalog() []TriggerEventSpec { return slices.Clone(triggerEventSpecs) }

// FindTriggerEvent 按事件键查。
func FindTriggerEvent(key string) (TriggerEventSpec, bool) {
	for _, spec := range triggerEventSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return TriggerEventSpec{}, false
}

// TriggerEventForHook 按插件钩子名反查事件键。
func TriggerEventForHook(hook string) (string, bool) {
	for _, spec := range triggerEventSpecs {
		if spec.Hook != "" && spec.Hook == hook {
			return spec.Key, true
		}
	}
	return "", false
}

// Trigger 一条触发器：某个平台事件发生时、或按 cron 计划，拉起一次函数调用。
type Trigger struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appId"`
	FunctionID  int64  `json:"functionId"`
	Function    string `json:"function,omitempty"`
	Kind        string `json:"kind"`
	Event       string `json:"event,omitempty"`
	Cron        string `json:"cron,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	Description string `json:"description"`
	// Input 每次触发时作为 ctx.input 传入的固定入参（同样要过入参契约）
	Input   json.RawMessage `json:"input"`
	Enabled bool            `json:"enabled"`

	NextRunAt  *time.Time `json:"nextRunAt,omitempty"`
	LastFireAt *time.Time `json:"lastFireAt,omitempty"`
	LastStatus string     `json:"lastStatus,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
	FireCount  int64      `json:"fireCount"`
	FailCount  int64      `json:"failCount"`
	CreatedBy  *int64     `json:"createdBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// TriggerInput 新建 / 修改触发器。修改时 Kind 不可变（换种类请删了重建）。
type TriggerInput struct {
	Kind        string          `json:"kind"`
	Event       string          `json:"event"`
	Cron        string          `json:"cron"`
	Timezone    string          `json:"timezone"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input"`
	Enabled     *bool           `json:"enabled"`
}

// TriggerContext 注入脚本的 ctx.trigger；接入方直接调用时为 null。
type TriggerContext struct {
	Kind      string         `json:"kind"`
	TriggerID int64          `json:"triggerId"`
	Event     string         `json:"event,omitempty"`
	Cron      string         `json:"cron,omitempty"`
	FiredAt   time.Time      `json:"firedAt"`
	Attempt   int            `json:"attempt"`
	Data      map[string]any `json:"data"`
}

// TriggerJob 持久队列里的一次待执行触发。
type TriggerJob struct {
	ID         int64           `json:"id"`
	AppID      int64           `json:"appId"`
	FunctionID int64           `json:"functionId"`
	TriggerID  int64           `json:"triggerId"`
	EventID    string          `json:"eventId"`
	Kind       string          `json:"kind"`
	Event      string          `json:"event,omitempty"`
	UserID     *int64          `json:"userId,omitempty"`
	Data       json.RawMessage `json:"data"`
	Status     string          `json:"status"`
	Attempts   int             `json:"attempts"`
	LastError  string          `json:"lastError,omitempty"`
	RunAfter   time.Time       `json:"runAfter"`
	FiredAt    time.Time       `json:"firedAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

// TriggerJobQuery 触发任务列表筛选。
type TriggerJobQuery struct {
	TriggerID int64
	Status    string
	Page      int
	Limit     int
}

// TriggerJobPage 触发任务分页。
type TriggerJobPage struct {
	Items []TriggerJob `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}

// TriggerFireInput 控制台「立即触发」：事件触发器可指定一个用户与事件数据，模拟一次事件。
type TriggerFireInput struct {
	UserID *int64          `json:"userId"`
	Data   json.RawMessage `json:"data"`
}
