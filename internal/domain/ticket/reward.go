package ticket

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// 有效反馈奖励积分（docs/feedback-rewards.md）。
//
// 管理员在服务台给被采纳的意见反馈发积分：
//   - 按应用配置策略：开关、默认建议积分、按分类的建议积分、单条上限、每用户每日 / 每月累计上限；
//     没有策略行或未开启 = 不能发，老行为不变。
//   - 一张反馈同一时刻至多一条「已发放」的奖励（库里部分唯一索引兜底）；重复点击原样返回首次结果。
//   - 撤回扣回积分：默认余额不够整笔拒绝（与积分冲正同一规则）；也可选择「扣到 0」，差额记 shortfall
//     （与取消永久会员收回折算积分同一规则）。撤回后可以按正确的数额重新发。
//
// 这里只放纯函数：建议值、上限判定、能不能发。存取与事务在仓储层，权限在服务层。

// 奖励记录状态。
const (
	RewardGranted = "granted"
	RewardRevoked = "revoked"
)

// 时间线事件。
const (
	EventRewardGranted = "reward_granted"
	EventRewardRevoked = "reward_revoked"
)

// 积分流水分类与来源（integral_transactions.category / source_type）。
// 两个分类都在 leaderboard.integralCategories 里登记为不计入周期榜：运营发的不是用户挣来的，
// 撤回又是扣减，计入会让「发了再撤」在榜上留下凭空的分数。
const (
	RewardIntegralCategory       = "feedback_reward"
	RewardRevokeIntegralCategory = "feedback_reward_revoke"
	RewardSourceType             = "feedback_reward"
)

// 数值边界。
const (
	// RewardMaxPoints 任何一个积分字段的上限，与管理员单次调整上限一致（防手滑多打几个 0）
	RewardMaxPoints int64 = 100_000_000
	// RewardReasonMaxRunes 发放与撤回理由的长度上限
	RewardReasonMaxRunes = 200
	// RewardMaxCategoryEntries 按分类建议积分最多配多少项
	RewardMaxCategoryEntries = 64
	rewardCategoryKeyMaxLen  = 64
)

// RewardPolicy 一个应用的反馈奖励策略。上限字段 0 表示不限。
type RewardPolicy struct {
	AppID   int64 `json:"appid"`
	Enabled bool  `json:"enabled"`
	// DefaultPoints 默认建议积分：分类没单独配时预填它
	DefaultPoints int64 `json:"defaultPoints"`
	// CategoryPoints 反馈分类 key → 建议积分（问题反馈 feedback_bug、功能建议 feedback_feature、体验吐槽 feedback_experience……）
	CategoryPoints map[string]int64 `json:"categoryPoints"`
	// MaxPoints 单条上限
	MaxPoints int64 `json:"maxPoints"`
	// UserDailyLimit / UserMonthlyLimit 每用户自然日 / 自然月累计上限（已撤回的不计）
	UserDailyLimit   int64 `json:"userDailyLimit"`
	UserMonthlyLimit int64 `json:"userMonthlyLimit"`
	// Configured 库里是否已有这一行（没有时返回推荐默认值，且未开启）
	Configured    bool       `json:"configured"`
	UpdatedByName string     `json:"updatedByName,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// DefaultRewardPolicy 没有配置时的推荐默认值：未开启；开启后按分类预填 50 / 30 / 20，单条至多 500，
// 每人每天至多 500、每月至多 2000。
func DefaultRewardPolicy(appID int64) RewardPolicy {
	return RewardPolicy{
		AppID:         appID,
		Enabled:       false,
		DefaultPoints: 20,
		CategoryPoints: map[string]int64{
			"feedback_bug":        50,
			"feedback_feature":    30,
			"feedback_experience": 20,
		},
		MaxPoints:        500,
		UserDailyLimit:   500,
		UserMonthlyLimit: 2000,
	}
}

// RewardPolicyError 策略或理由校验失败；Message 可直接给调用方。
type RewardPolicyError struct{ Message string }

func (e *RewardPolicyError) Error() string { return e.Message }

func policyInvalid(format string, args ...any) error {
	return &RewardPolicyError{Message: fmt.Sprintf(format, args...)}
}

// NormalizeRewardPolicy 校验并整理策略：数值非负且不超过 RewardMaxPoints；建议值不超过单条上限；
// 日上限不超过月上限（都设了的话）；分类 key 去空白、建议值 0 的项去掉。
func NormalizeRewardPolicy(in RewardPolicy) (RewardPolicy, error) {
	out := in
	check := func(label string, value int64) error {
		if value < 0 {
			return policyInvalid("%s不能为负数", label)
		}
		if value > RewardMaxPoints {
			return policyInvalid("%s不能超过 %d", label, RewardMaxPoints)
		}
		return nil
	}
	for _, field := range []struct {
		label string
		value int64
	}{
		{"默认建议积分", in.DefaultPoints}, {"单条上限", in.MaxPoints},
		{"每用户每日上限", in.UserDailyLimit}, {"每用户每月上限", in.UserMonthlyLimit},
	} {
		if err := check(field.label, field.value); err != nil {
			return out, err
		}
	}
	if in.MaxPoints > 0 && in.DefaultPoints > in.MaxPoints {
		return out, policyInvalid("默认建议积分不能超过单条上限 %d", in.MaxPoints)
	}
	if in.UserDailyLimit > 0 && in.UserMonthlyLimit > 0 && in.UserDailyLimit > in.UserMonthlyLimit {
		return out, policyInvalid("每用户每日上限不能超过每月上限")
	}
	if len(in.CategoryPoints) > RewardMaxCategoryEntries {
		return out, policyInvalid("按分类的建议积分最多配置 %d 项", RewardMaxCategoryEntries)
	}
	out.CategoryPoints = make(map[string]int64, len(in.CategoryPoints))
	for rawKey, value := range in.CategoryPoints {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			return out, policyInvalid("分类标识不能为空")
		}
		if utf8.RuneCountInString(key) > rewardCategoryKeyMaxLen {
			return out, policyInvalid("分类标识 %s 过长", key)
		}
		if err := check("分类「"+key+"」的建议积分", value); err != nil {
			return out, err
		}
		if in.MaxPoints > 0 && value > in.MaxPoints {
			return out, policyInvalid("分类「%s」的建议积分不能超过单条上限 %d", key, in.MaxPoints)
		}
		if value == 0 {
			continue
		}
		out.CategoryPoints[key] = value
	}
	return out, nil
}

// SuggestPoints 某分类反馈的建议积分：分类配了用分类的，否则用默认值；不超过单条上限。
func (p RewardPolicy) SuggestPoints(categoryKey string) int64 {
	points := p.DefaultPoints
	if value, ok := p.CategoryPoints[strings.TrimSpace(categoryKey)]; ok && value > 0 {
		points = value
	}
	if p.MaxPoints > 0 && points > p.MaxPoints {
		points = p.MaxPoints
	}
	return max(points, 0)
}

// CategoryKeys 按 key 排序的分类列表（展示与测试用）。
func (p RewardPolicy) CategoryKeys() []string {
	keys := make([]string, 0, len(p.CategoryPoints))
	for key := range p.CategoryPoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// RewardUsage 某用户在本应用当天 / 当月已领的反馈奖励积分（已撤回的不计）。
type RewardUsage struct {
	Daily   int64 `json:"daily"`
	Monthly int64 `json:"monthly"`
}

// MaxGrantable 这一次最多还能发多少：单条上限、当日余量、当月余量取最小；nil 表示不限。
func (p RewardPolicy) MaxGrantable(usage RewardUsage) *int64 {
	var limit *int64
	take := func(value int64) {
		value = max(value, 0)
		if limit == nil || value < *limit {
			limit = &value
		}
	}
	if p.MaxPoints > 0 {
		take(p.MaxPoints)
	}
	if p.UserDailyLimit > 0 {
		take(p.UserDailyLimit - usage.Daily)
	}
	if p.UserMonthlyLimit > 0 {
		take(p.UserMonthlyLimit - usage.Monthly)
	}
	return limit
}

// 发放判定的失败原因。
var (
	ErrRewardDisabled = errors.New("ticket: feedback reward disabled")
	ErrRewardPoints   = errors.New("ticket: reward points out of range")
)

// RewardLimitError 超过单条 / 每日 / 每月上限。Remaining 为这一档还剩多少（单条上限时即上限本身）。
type RewardLimitError struct {
	Scope     string // single / daily / monthly
	Limit     int64
	Remaining int64
}

func (e *RewardLimitError) Error() string {
	switch e.Scope {
	case "daily":
		return fmt.Sprintf("超过该用户每日反馈奖励上限 %d（今天还可发 %d）", e.Limit, e.Remaining)
	case "monthly":
		return fmt.Sprintf("超过该用户每月反馈奖励上限 %d（本月还可发 %d）", e.Limit, e.Remaining)
	default:
		return fmt.Sprintf("超过单条反馈奖励上限 %d", e.Limit)
	}
}

// CheckGrant 这一笔能不能发（策略层面）：开启、数额在 1..RewardMaxPoints、不超过单条与累计上限。
func (p RewardPolicy) CheckGrant(points int64, usage RewardUsage) error {
	if !p.Enabled {
		return ErrRewardDisabled
	}
	if points <= 0 || points > RewardMaxPoints {
		return ErrRewardPoints
	}
	if p.MaxPoints > 0 && points > p.MaxPoints {
		return &RewardLimitError{Scope: "single", Limit: p.MaxPoints, Remaining: p.MaxPoints}
	}
	if p.UserDailyLimit > 0 && usage.Daily+points > p.UserDailyLimit {
		return &RewardLimitError{Scope: "daily", Limit: p.UserDailyLimit, Remaining: max(p.UserDailyLimit-usage.Daily, 0)}
	}
	if p.UserMonthlyLimit > 0 && usage.Monthly+points > p.UserMonthlyLimit {
		return &RewardLimitError{Scope: "monthly", Limit: p.UserMonthlyLimit, Remaining: max(p.UserMonthlyLimit-usage.Monthly, 0)}
	}
	return nil
}

// RewardIneligibleReason 这张单子本身能不能发奖励（与策略无关）；能发返回空串。
//
// 只有意见反馈、提单人是该应用用户的才能发；用户撤销的与「不采纳」的不发（奖励的是有效反馈）。
func RewardIneligibleReason(item *Ticket) string {
	switch {
	case item == nil:
		return "反馈不存在"
	case item.Kind != KindFeedback:
		return "只有意见反馈可以发放奖励"
	case item.RequesterType != RequesterUser || item.RequesterUserID == nil || item.AppID <= 0:
		return "提单人不是该应用的用户"
	case item.Status == StatusCancelled:
		return "反馈已被用户撤销"
	case item.Status == StatusRejected:
		return "不采纳的反馈不能发放奖励"
	}
	return ""
}

// RewardableStatusTarget 改状态时能否「同时发放奖励」：目标状态本身不能是不采纳 / 已撤销。
func RewardableStatusTarget(status string) bool {
	return status != StatusRejected && status != StatusCancelled
}

// NormalizeRewardReason 去空白并限长。
func NormalizeRewardReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > RewardReasonMaxRunes {
		return "", policyInvalid("理由不能超过 %d 字", RewardReasonMaxRunes)
	}
	return reason, nil
}

// RewardPeriodStarts 用户累计上限的统计起点：loc 下的当天 0 点与当月 1 日 0 点。
func RewardPeriodStarts(now time.Time, loc *time.Location) (day time.Time, month time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	day = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	month = time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	return day, month
}

// Reward 一条反馈奖励记录。
type Reward struct {
	ID          int64  `json:"id"`
	TicketID    int64  `json:"ticketId"`
	AppID       int64  `json:"appid"`
	UserID      *int64 `json:"userId,omitempty"`
	TicketNo    string `json:"ticketNo"`
	CategoryKey string `json:"categoryKey,omitempty"`
	Points      int64  `json:"points"`
	Reason      string `json:"reason,omitempty"`
	Status      string `json:"status"`

	GrantTxnNo       string    `json:"grantTxnNo"`
	GrantedByAdminID *int64    `json:"grantedByAdminId,omitempty"`
	GrantedByName    string    `json:"grantedByName,omitempty"`
	GrantedAt        time.Time `json:"grantedAt"`
	NotificationID   *int64    `json:"-"`

	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	RevokedByAdminID *int64     `json:"revokedByAdminId,omitempty"`
	RevokedByName    string     `json:"revokedByName,omitempty"`
	RevokeReason     string     `json:"revokeReason,omitempty"`
	RevokeTxnNo      string     `json:"revokeTxnNo,omitempty"`
	ClawedBack       int64      `json:"clawedBack"`
	Shortfall        int64      `json:"shortfall"`

	RequestID       string `json:"-"`
	RevokeRequestID string `json:"-"`
}

// RewardGrantCommand 发放一笔反馈奖励。RequestID 可空：带上时同一编号重复提交原样返回首次结果。
type RewardGrantCommand struct {
	Points    int64  `json:"points"`
	Reason    string `json:"reason,omitempty"`
	RequestID string `json:"requestId,omitempty"`
	// 操作者的地址与 UA，记进积分流水（由处理函数填，不出网）
	ClientIP  string `json:"-"`
	UserAgent string `json:"-"`
}

// RewardRevokeCommand 撤回当前有效的奖励。
//
// AllowPartial 为假（默认）时用户余额不足整笔拒绝（与积分冲正一致）；为真时扣到 0 为止，差额记 shortfall。
type RewardRevokeCommand struct {
	Reason       string `json:"reason,omitempty"`
	AllowPartial bool   `json:"allowPartial,omitempty"`
	RequestID    string `json:"requestId,omitempty"`
	ClientIP     string `json:"-"`
	UserAgent    string `json:"-"`
}

// RewardInfo 管理端反馈详情里的奖励区块：策略要点、这张单子能不能发、已发的与历史记录。
type RewardInfo struct {
	// Enabled 应用是否开启了反馈奖励
	Enabled bool `json:"enabled"`
	// SuggestedPoints 按分类预填的积分
	SuggestedPoints int64 `json:"suggestedPoints"`
	MaxPoints       int64 `json:"maxPoints"`
	// MaxGrantable 这次最多还能发多少（单条上限与该用户当日、当月余量取最小）；null 表示不限
	MaxGrantable *int64      `json:"maxGrantable"`
	Usage        RewardUsage `json:"usage"`
	// Ineligible 这张单子本身不能发的原因（不是反馈、提单人不是用户、已撤销、不采纳）；空表示可以
	Ineligible string `json:"ineligible,omitempty"`
	// Active 当前有效的奖励；History 全部记录（新的在前，含已撤回的）
	Active  *Reward  `json:"active,omitempty"`
	History []Reward `json:"history"`
}
