// Package platform 定义平台治理（超级管理员 / 平台管理员对全站应用的强制管控）的领域类型。
//
// 与 domain/app 的分工：
//   - `app.App` 的 Status / LoginStatus / RegisterStatus 是**应用自治**的营业开关，
//     应用管理员自己就能开关，用来表达「我这两天不开放注册」。
//   - 本包的 `Governance` 是**平台强制**的治理状态，只有平台级管理员能改，
//     优先级高于前者：被冻结的应用即使自己把 Status 打开也一律拒绝服务。
//
// 两者刻意不共用一张表、不共用一个开关，正是为了让应用管理员改不动平台的治理结论。
package platform

import "time"

// ── 治理状态 ──
const (
	// StateActive 正常运营，无任何平台限制
	StateActive = "active"
	// StateRestricted 部分受限：限制项由操作者逐条指定
	StateRestricted = "restricted"
	// StateFrozen 冻结：用户侧全部能力停用，应用管理员仍可登录控制台处理问题
	StateFrozen = "frozen"
	// StateSuspended 停运：在冻结之上再冻结应用管理员的写操作，配置一并只读
	StateSuspended = "suspended"
	// StateBanned 封禁：永久停运，仅超级管理员 / 持危险操作权限者可解除
	StateBanned = "banned"
	// StateArchived 归档：永久只读留档，用于已下线但需保留数据的应用
	StateArchived = "archived"
)

// ── 治理动作 ──
const (
	ActionRestrict = "restrict"
	ActionFreeze   = "freeze"
	ActionSuspend  = "suspend"
	ActionBan      = "ban"
	ActionArchive  = "archive"
	ActionRestore  = "restore"
	// ActionUpdate 维持当前状态，仅调整理由 / 期限 / 限制项
	ActionUpdate = "update"
	// ActionExpire 到期自动恢复（系统发起，operator 为空）
	ActionExpire = "expire"
	// ActionAppealApproved 申诉通过后的自动恢复
	ActionAppealApproved = "appeal_approved"
	// ActionRevokeSessions 强制下线该应用全部用户会话（不改变治理状态）
	ActionRevokeSessions = "revoke_sessions"
)

// ── 能力（限制项的判定单位）──
//
// 每一项都必须有真实执行点，见 service.PlatformGovernanceService 顶部的执行点索引。
const (
	CapabilityLogin        = "login"
	CapabilityRegister     = "register"
	CapabilityAPI          = "api"
	CapabilityPayment      = "payment"
	CapabilityStorage      = "storage"
	CapabilityNotification = "notification"
	CapabilityAdminWrite   = "adminWrite"
)

// ── 申诉状态 ──
const (
	AppealStatusNone      = "none"
	AppealStatusPending   = "pending"
	AppealStatusApproved  = "approved"
	AppealStatusRejected  = "rejected"
	AppealStatusWithdrawn = "withdrawn"
)

// Restrictions 细粒度限制项。零值表示不限制。
type Restrictions struct {
	// BlockLogin 拒绝一切登录（含密码 / 短信 / OAuth / Passkey）
	BlockLogin bool `json:"blockLogin"`
	// BlockRegister 拒绝注册与自动建号
	BlockRegister bool `json:"blockRegister"`
	// BlockAPI 拒绝一切携带用户令牌的业务请求（等于现存会话立即失效）
	BlockAPI bool `json:"blockApi"`
	// BlockPayment 拒绝下单、支付与退款发起
	BlockPayment bool `json:"blockPayment"`
	// BlockStorage 拒绝上传与写入类存储操作（读取与下载不受影响）
	BlockStorage bool `json:"blockStorage"`
	// BlockNotification 拒绝该应用的对外发信（邮件 / 站内信 / 实时推送）
	BlockNotification bool `json:"blockNotification"`
	// BlockAdminWrite 应用管理员对该应用的一切写操作只读化（平台级管理员不受限）
	BlockAdminWrite bool `json:"blockAdminWrite"`
}

// Any 是否存在任一限制项。
func (r Restrictions) Any() bool {
	return r.BlockLogin || r.BlockRegister || r.BlockAPI || r.BlockPayment ||
		r.BlockStorage || r.BlockNotification || r.BlockAdminWrite
}

// Blocks 判定某项能力是否被限制。
func (r Restrictions) Blocks(capability string) bool {
	switch capability {
	case CapabilityLogin:
		return r.BlockLogin
	case CapabilityRegister:
		return r.BlockRegister
	case CapabilityAPI:
		return r.BlockAPI
	case CapabilityPayment:
		return r.BlockPayment
	case CapabilityStorage:
		return r.BlockStorage
	case CapabilityNotification:
		return r.BlockNotification
	case CapabilityAdminWrite:
		return r.BlockAdminWrite
	default:
		return false
	}
}

// Codes 返回被限制能力的代码列表（稳定顺序，用于展示与审计）。
func (r Restrictions) Codes() []string {
	codes := make([]string, 0, 7)
	for _, item := range []struct {
		code    string
		blocked bool
	}{
		{CapabilityLogin, r.BlockLogin},
		{CapabilityRegister, r.BlockRegister},
		{CapabilityAPI, r.BlockAPI},
		{CapabilityPayment, r.BlockPayment},
		{CapabilityStorage, r.BlockStorage},
		{CapabilityNotification, r.BlockNotification},
		{CapabilityAdminWrite, r.BlockAdminWrite},
	} {
		if item.blocked {
			codes = append(codes, item.code)
		}
	}
	return codes
}

// Governance 单个应用的当前治理状态。
type Governance struct {
	AppID           int64          `json:"appid"`
	AppName         string         `json:"appName,omitempty"`
	AppKey          string         `json:"appKey,omitempty"`
	State           string         `json:"state"`
	Reason          string         `json:"reason,omitempty"`
	Restrictions    Restrictions   `json:"restrictions"`
	Evidence        map[string]any `json:"evidence,omitempty"`
	StartAt         *time.Time     `json:"startAt,omitempty"`
	EndAt           *time.Time     `json:"endAt,omitempty"`
	OperatorAdminID *int64         `json:"operatorAdminId,omitempty"`
	OperatorName    string         `json:"operatorName,omitempty"`
	LastAction      string         `json:"lastAction,omitempty"`
	AppealStatus    string         `json:"appealStatus"`
	CreatedAt       time.Time      `json:"createdAt,omitzero"`
	UpdatedAt       time.Time      `json:"updatedAt,omitzero"`
}

// IsActive 是否处于无治理限制的正常状态。
func (g *Governance) IsActive() bool {
	return g == nil || g.State == "" || g.State == StateActive
}

// Permanent 当前状态是否为永久性（不允许设置到期时间）。
func (g *Governance) Permanent() bool {
	if g == nil {
		return false
	}
	return StatePermanent(g.State)
}

// StatePermanent 永久状态：不接受到期时间，必须人工解除。
func StatePermanent(state string) bool {
	return state == StateBanned || state == StateArchived
}

// StateRequiresDanger 解除该状态是否需要危险操作权限（platform:app:danger / 超管）。
func StateRequiresDanger(state string) bool {
	return state == StateBanned || state == StateArchived
}

// ValidState 状态是否合法。
func ValidState(state string) bool {
	switch state {
	case StateActive, StateRestricted, StateFrozen, StateSuspended, StateBanned, StateArchived:
		return true
	default:
		return false
	}
}

// PresetRestrictions 各状态的预设限制项。
//
// restricted 由操作者自行指定，因此返回零值；其余状态的语义即由这里定义，
// 前端展示的"这一档会禁掉什么"与后端判定读的是同一份预设。
func PresetRestrictions(state string) Restrictions {
	switch state {
	case StateFrozen:
		return Restrictions{
			BlockLogin: true, BlockRegister: true, BlockAPI: true,
			BlockPayment: true, BlockStorage: true,
		}
	case StateSuspended, StateBanned:
		return Restrictions{
			BlockLogin: true, BlockRegister: true, BlockAPI: true,
			BlockPayment: true, BlockStorage: true,
			BlockNotification: true, BlockAdminWrite: true,
		}
	case StateArchived:
		return Restrictions{
			BlockLogin: true, BlockRegister: true, BlockAPI: true,
			BlockPayment: true, BlockStorage: true,
			BlockNotification: true, BlockAdminWrite: true,
		}
	default:
		return Restrictions{}
	}
}

// ActionTargetState 动作对应的目标状态；未知动作返回空串。
func ActionTargetState(action string) string {
	switch action {
	case ActionRestrict:
		return StateRestricted
	case ActionFreeze:
		return StateFrozen
	case ActionSuspend:
		return StateSuspended
	case ActionBan:
		return StateBanned
	case ActionArchive:
		return StateArchived
	case ActionRestore, ActionExpire, ActionAppealApproved:
		return StateActive
	default:
		return ""
	}
}

// ActionInput 治理动作入参。
type ActionInput struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
	// Restrictions 仅 restrict / update 需要；其余状态用预设，传入即忽略
	Restrictions *Restrictions  `json:"restrictions,omitempty"`
	Evidence     map[string]any `json:"evidence,omitempty"`
	// EndAt 与 DurationSeconds 二选一；均为空表示无限期（需人工解除）
	EndAt           *time.Time `json:"endAt,omitempty"`
	DurationSeconds int64      `json:"durationSeconds,omitempty"`
	// RevokeSessions 立即踢掉该应用全部在线用户
	RevokeSessions bool `json:"revokeSessions"`
	// NotifyAdmins 向该应用的管理员发送站内通知
	NotifyAdmins bool `json:"notifyAdmins"`
}

// BatchActionInput 批量治理入参。
type BatchActionInput struct {
	AppIDs []int64 `json:"appids"`
	ActionInput
}

// BatchActionResult 批量治理结果。
type BatchActionResult struct {
	Requested int                  `json:"requested"`
	Succeeded int                  `json:"succeeded"`
	Failed    int                  `json:"failed"`
	Items     []BatchActionOutcome `json:"items"`
}

// BatchActionOutcome 单个应用的批量治理结果。
type BatchActionOutcome struct {
	AppID   int64  `json:"appid"`
	AppName string `json:"appName,omitempty"`
	OK      bool   `json:"ok"`
	State   string `json:"state,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Actor 治理动作的操作者。
type Actor struct {
	AdminID      int64  `json:"adminId"`
	AdminName    string `json:"adminName"`
	IP           string `json:"ip,omitempty"`
	IsSuperAdmin bool   `json:"isSuperAdmin"`
	// CanDanger 是否持有危险操作权限（封禁 / 归档 / 强制下线）
	CanDanger bool `json:"canDanger"`
}

// ActionRecord 一条治理动作流水。
type ActionRecord struct {
	ID              int64          `json:"id"`
	AppID           int64          `json:"appid"`
	AppName         string         `json:"appName,omitempty"`
	AppKey          string         `json:"appKey,omitempty"`
	Action          string         `json:"action"`
	FromState       string         `json:"fromState"`
	ToState         string         `json:"toState"`
	Reason          string         `json:"reason,omitempty"`
	Restrictions    Restrictions   `json:"restrictions"`
	Evidence        map[string]any `json:"evidence,omitempty"`
	EndAt           *time.Time     `json:"endAt,omitempty"`
	OperatorAdminID *int64         `json:"operatorAdminId,omitempty"`
	OperatorName    string         `json:"operatorName,omitempty"`
	OperatorIP      string         `json:"operatorIp,omitempty"`
	RevokedSessions int            `json:"revokedSessions"`
	CreatedAt       time.Time      `json:"createdAt"`
}

// ActionQuery 流水查询条件。
type ActionQuery struct {
	AppID   int64  `json:"appid"`
	Action  string `json:"action"`
	State   string `json:"state"`
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
}

// ActionListResult 流水分页结果。
type ActionListResult struct {
	Items      []ActionRecord `json:"items"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	Total      int64          `json:"total"`
	TotalPages int            `json:"totalPages"`
}

// Appeal 治理申诉。
type Appeal struct {
	ID                 int64      `json:"id"`
	AppID              int64      `json:"appid"`
	AppName            string     `json:"appName,omitempty"`
	AppKey             string     `json:"appKey,omitempty"`
	ActionID           *int64     `json:"actionId,omitempty"`
	StateSnapshot      string     `json:"stateSnapshot,omitempty"`
	Content            string     `json:"content"`
	Attachments        []string   `json:"attachments,omitempty"`
	SubmittedByAdminID *int64     `json:"submittedByAdminId,omitempty"`
	SubmittedByName    string     `json:"submittedByName,omitempty"`
	Status             string     `json:"status"`
	ReviewAdminID      *int64     `json:"reviewAdminId,omitempty"`
	ReviewAdminName    string     `json:"reviewAdminName,omitempty"`
	ReviewNote         string     `json:"reviewNote,omitempty"`
	ReviewedAt         *time.Time `json:"reviewedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

// AppealCreateInput 提交申诉。
type AppealCreateInput struct {
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"`
}

// AppealReviewInput 申诉裁决。
type AppealReviewInput struct {
	// Decision approved / rejected
	Decision string `json:"decision"`
	Note     string `json:"note"`
	// Restore 通过申诉的同时解除治理（默认 true）
	Restore *bool `json:"restore,omitempty"`
}

// AppealQuery 申诉查询条件。
type AppealQuery struct {
	AppID   int64  `json:"appid"`
	Status  string `json:"status"`
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
}

// AppealListResult 申诉分页结果。
type AppealListResult struct {
	Items      []Appeal `json:"items"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	Total      int64    `json:"total"`
	TotalPages int      `json:"totalPages"`
}

// AppOverviewItem 全站应用总览的单行。
type AppOverviewItem struct {
	AppID          int64  `json:"appid"`
	Name           string `json:"name"`
	AppKey         string `json:"appKey,omitempty"`
	Status         bool   `json:"status"`
	LoginStatus    bool   `json:"loginStatus"`
	RegisterStatus bool   `json:"registerStatus"`

	State        string       `json:"state"`
	Reason       string       `json:"reason,omitempty"`
	Restrictions Restrictions `json:"restrictions"`
	StartAt      *time.Time   `json:"startAt,omitempty"`
	EndAt        *time.Time   `json:"endAt,omitempty"`
	OperatorName string       `json:"operatorName,omitempty"`
	LastAction   string       `json:"lastAction,omitempty"`
	AppealStatus string       `json:"appealStatus"`

	TotalUsers        int64 `json:"totalUsers"`
	DisabledUsers     int64 `json:"disabledUsers"`
	BannedUsers       int64 `json:"bannedUsers"`
	NewUsersToday     int64 `json:"newUsersToday"`
	LoginSuccessToday int64 `json:"loginSuccessToday"`
	LoginFailureToday int64 `json:"loginFailureToday"`
	AdminCount        int64 `json:"adminCount"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// OverviewQuery 全站应用总览查询条件。
type OverviewQuery struct {
	Keyword string `json:"keyword"`
	State   string `json:"state"`
	// Governed 只看被治理的应用
	Governed  bool   `json:"governed"`
	SortBy    string `json:"sortBy"`
	SortOrder string `json:"sortOrder"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
}

// OverviewSummary 全站聚合指标。
type OverviewSummary struct {
	TotalApps      int64            `json:"totalApps"`
	ActiveApps     int64            `json:"activeApps"`
	GovernedApps   int64            `json:"governedApps"`
	StateCounts    map[string]int64 `json:"stateCounts"`
	TotalUsers     int64            `json:"totalUsers"`
	NewUsersToday  int64            `json:"newUsersToday"`
	LoginsToday    int64            `json:"loginsToday"`
	PendingAppeals int64            `json:"pendingAppeals"`
	ExpiringSoon   int64            `json:"expiringSoon"`
}

// OverviewResult 全站应用总览分页结果。
type OverviewResult struct {
	Items      []AppOverviewItem `json:"items"`
	Summary    OverviewSummary   `json:"summary"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"totalPages"`
}

// StateMeta 状态目录项（驱动控制台的动作面板）。
type StateMeta struct {
	Key            string       `json:"key"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Action         string       `json:"action"`
	Severity       int          `json:"severity"`
	Permanent      bool         `json:"permanent"`
	RequiresDanger bool         `json:"requiresDanger"`
	Restrictions   Restrictions `json:"restrictions"`
	Customizable   bool         `json:"customizable"`
}

// CapabilityMeta 能力目录项。
type CapabilityMeta struct {
	Key         string `json:"key"`
	Field       string `json:"field"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Enforcement 执行点，写在目录里是为了让"这个开关到底管不管用"可被直接核对
	Enforcement string `json:"enforcement"`
}

// Catalog 治理目录：状态 / 能力 / 常用时长，供控制台渲染。
type Catalog struct {
	States       []StateMeta      `json:"states"`
	Capabilities []CapabilityMeta `json:"capabilities"`
	Durations    []DurationOption `json:"durations"`
}

// DurationOption 常用治理时长。
type DurationOption struct {
	Label   string `json:"label"`
	Seconds int64  `json:"seconds"`
}

// Decision 一次能力判定的结果。
type Decision struct {
	Allowed    bool       `json:"allowed"`
	State      string     `json:"state"`
	Capability string     `json:"capability,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Message    string     `json:"message,omitempty"`
	EndAt      *time.Time `json:"endAt,omitempty"`
}
