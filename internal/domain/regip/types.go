package regip

import "time"

// 拦截记录里的结论（Decision.Outcome 加上观察与加验通过两种留痕）。
const (
	EventRejected        = "rejected"         // 拒绝
	EventChallenged      = "challenged"       // 要求加验
	EventChallengePassed = "challenge_passed" // 加验后放行
	EventObserved        = "observed"         // 观察模式或观察型规则：本该拦截，放行了
	EventReleased        = "released"         // 管理员解除限制
	EventPenalized       = "penalized"        // 进入惩罚期
)

// EventKinds 全部记录类型。
var EventKinds = []string{EventRejected, EventChallenged, EventChallengePassed, EventObserved, EventReleased, EventPenalized}

// EventOf 判定结论对应的记录类型；不值得记的返回空串。
func EventOf(d Decision) string {
	switch {
	case d.Outcome == OutcomeReject:
		return EventRejected
	case d.Outcome == OutcomeChallenge:
		return EventChallenged
	case d.Observed != "":
		return EventObserved
	case d.ChallengePassed:
		return EventChallengePassed
	}
	return ""
}

// Event 一条拦截记录。
type Event struct {
	ID       int64          `json:"id"`
	AppID    int64          `json:"appId"`
	Kind     string         `json:"kind"`
	Reason   string         `json:"reason"`
	RuleID   string         `json:"ruleId"`
	RuleName string         `json:"ruleName"`
	Method   string         `json:"method"`
	IP       string         `json:"ip"`
	Subnet   string         `json:"subnet"`
	Account  string         `json:"account"`
	DeviceID string         `json:"deviceId"`
	Country  string         `json:"country"`
	Province string         `json:"province"`
	City     string         `json:"city"`
	ISP      string         `json:"isp"`
	Count    int            `json:"count"`
	Limit    int            `json:"limit"`
	Detail   string         `json:"detail"`
	Extra    map[string]any `json:"extra,omitempty"`
	Operator string         `json:"operator,omitempty"`
	At       time.Time      `json:"createdAt"`
}

// EventQuery 拦截记录筛选。
type EventQuery struct {
	AppID    int64
	Kind     string
	Reason   string
	RuleID   string
	Method   string
	IP       string // 地址或网段（CIDR）
	DeviceID string // 设备标识（精确）
	Keyword  string // 账号、设备、属地、详情
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// Registration 注册台账的一行：哪个账号、什么时候、从哪个地址、用什么方式注册的。
// 累计规则按它数，管理员「解除」只是给它打上 released_at，账号本身不受影响。
type Registration struct {
	ID         int64      `json:"id"`
	AppID      int64      `json:"appId"`
	UserID     *int64     `json:"userId"`
	Account    string     `json:"account"`
	IP         string     `json:"ip"`
	Method     string     `json:"method"`
	Country    string     `json:"country"`
	Province   string     `json:"province"`
	City       string     `json:"city"`
	ISP        string     `json:"isp"`
	DeviceID   string     `json:"deviceId"`
	ReleasedAt *time.Time `json:"releasedAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// RegistrationQuery 注册台账筛选。
type RegistrationQuery struct {
	AppID    int64
	IP       string // 地址或网段
	DeviceID string // 设备标识（精确）
	Method   string
	Keyword  string
	Released *bool
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// ListInput 批量添加名单。
type ListInput struct {
	Values    []string   `json:"values"`
	Kind      string     `json:"kind"`
	Factor    float64    `json:"factor"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// ListUpdate 修改一项名单。
type ListUpdate struct {
	Kind      string     `json:"kind"`
	Factor    float64    `json:"factor"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// ListQuery 名单筛选。
type ListQuery struct {
	AppID          int64
	Kind           string
	Keyword        string // 地址（落在哪个网段里）或备注
	IncludeExpired bool
	Page           int
	PageSize       int
}

// PenaltyEntry 一个正在惩罚期的主体。
type PenaltyEntry struct {
	Subject   string    `json:"subject"`
	Level     int       `json:"level"`
	ExpiresAt time.Time `json:"expiresAt"`
	Remaining int       `json:"remaining"`
}

// RuleUsage IP 查询里一条规则此刻的用量。
type RuleUsage struct {
	RuleHit
	// Remaining 还能注册几个
	Remaining int `json:"remaining"`
	// ResetAt 窗口内最早一笔滑出窗口的时刻（滑动窗口才有）
	ResetAt *time.Time `json:"resetAt,omitempty"`
}

// SimulateInput 模拟一次注册：草稿策略（为空用已保存的）+ 地址与场景，计数取真实值，可整体改写。
type SimulateInput struct {
	Policy        *Policy `json:"policy"`
	IP            string  `json:"ip"`
	DeviceID      string  `json:"deviceId"`
	Method        string  `json:"method"`
	CaptchaPassed bool    `json:"captchaPassed"`
	// Counts 改写某些规则的计数（规则 ID → 已有注册数），模拟「如果已经注册了 N 个」
	Counts map[string]int `json:"counts"`
	// Location 改写属地（resolved 为真时生效）
	Location *Location `json:"location"`
	// Surge 强制突增状态：nil 用真实值
	SurgeActive *bool `json:"surgeActive"`
	// PenaltySeconds 强制地址惩罚期剩余秒数：nil 用真实值
	PenaltySeconds *int `json:"penaltySeconds"`
	// DevicePenaltySeconds 强制设备惩罚期剩余秒数：nil 用真实值
	DevicePenaltySeconds *int `json:"devicePenaltySeconds"`
}

// EventList 拦截记录分页。
type EventList struct {
	Items []Event `json:"items"`
	Total int64   `json:"total"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
}

// RegistrationList 注册台账分页。
type RegistrationList struct {
	Items []Registration `json:"items"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Limit int            `json:"limit"`
}

// ListEntryList 名单分页。
type ListEntryList struct {
	Items []ListEntry `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

// PenaltyList 惩罚期列表。
type PenaltyList struct {
	Items []PenaltyEntry `json:"items"`
	Total int64          `json:"total"`
}

// ListDeleteInput 批量删除名单。
type ListDeleteInput struct {
	IDs []int64 `json:"ids"`
}

// DeviceListList 设备名单分页。
type DeviceListList struct {
	Items []DeviceListEntry `json:"items"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

// LinkedItem 关联视图的一行：某地址下出现过的一台设备，或某设备用过的一个地址。
type LinkedItem struct {
	Key      string    `json:"key"`
	Accounts int64     `json:"accounts"`
	FirstAt  time.Time `json:"firstAt"`
	LastAt   time.Time `json:"lastAt"`
	Location string    `json:"location,omitempty"`
}
