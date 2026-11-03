// Package growth 成长调控（docs/growth-control.md）：管理员对积分与经验「获取速度」的奖惩控制。
//
// 一条规则 = 作用对象（某个用户 / 全应用）× 数值类型（积分 / 经验）× 倍率（分数，×N 与 ÷N 同一种写法）
// × 取整方式 × 每日上限 × 来源范围 × 生效时间。用户入账时，同一类型下所有生效的规则叠乘：
// 全应用「双倍积分周末」×2 再乘上这个人的「违规减速」÷4，最终是 ÷2。
//
// 只作用于**自动发放**的收入（签到、激励广告、抽奖、邀请、排行榜、物品发放与等级奖励，可选卡密与积分商城经验包）。
// 下面这些永远不受影响，因为它们不是「获取」：管理员手动调整、付费充值与会员赠送、会员折算、各种退回与冲正、消费。
package growth

import (
	"time"
)

// Kind 规则作用的数值类型。
type Kind string

const (
	KindIntegral   Kind = "integral"
	KindExperience Kind = "experience"
)

// Valid 是否为已知类型。
func (k Kind) Valid() bool { return k == KindIntegral || k == KindExperience }

// Label 中文名。
func (k Kind) Label() string {
	if k == KindExperience {
		return "经验"
	}
	return "积分"
}

// Rounding 倍率作用后的取整方式。
type Rounding string

const (
	// RoundFloor 向下取整（默认）：÷3 时 2 积分得 0
	RoundFloor Rounding = "floor"
	// RoundNearest 四舍五入
	RoundNearest Rounding = "round"
	// RoundCeil 向上取整：只要原本有收入，就至少得 1
	RoundCeil Rounding = "ceil"
	// RoundRandom 按小数部分的概率进位：÷3 时 1 积分有 1/3 的概率得 1，长期期望恰好是三分之一
	RoundRandom Rounding = "random"
)

// Valid 是否为已知取整方式。
func (r Rounding) Valid() bool {
	switch r {
	case RoundFloor, RoundNearest, RoundCeil, RoundRandom:
		return true
	}
	return false
}

// 数值边界。
const (
	// MaxFactorTerm 倍率分子 / 分母的上限：×1000 与 ÷1000 足够任何活动与处罚
	MaxFactorTerm = 1000
	// MaxDailyCap 每日上限的上限（与余额上限同一量级，防手滑）
	MaxDailyCap int64 = 100_000_000
	// MaxActiveRulesPerTarget 同一个人（或全应用）同一类型下最多同时存在的未撤销规则数
	MaxActiveRulesPerTarget = 20
	// MaxReasonRunes 原因长度上限
	MaxReasonRunes = 200
	// MaxLabelRunes 名称长度上限
	MaxLabelRunes = 32
)

// Rule 一条倍率规则。UserID 为 0 表示全应用。
type Rule struct {
	ID          int64      `json:"id"`
	AppID       int64      `json:"appid"`
	UserID      int64      `json:"userId"`
	Account     string     `json:"account,omitempty"`
	Nickname    string     `json:"nickname,omitempty"`
	Kind        Kind       `json:"kind"`
	Numerator   int        `json:"numerator"`
	Denominator int        `json:"denominator"`
	Rounding    Rounding   `json:"rounding"`
	DailyCap    int64      `json:"dailyCap"`
	Sources     []string   `json:"sources"`
	StartsAt    *time.Time `json:"startsAt,omitempty"`
	EndsAt      *time.Time `json:"endsAt,omitempty"`
	Enabled     bool       `json:"enabled"`
	Label       string     `json:"label"`
	Reason      string     `json:"reason"`
	CreatedBy   string     `json:"createdBy,omitempty"`
	UpdatedBy   string     `json:"updatedBy,omitempty"`
	RevokedAt   *time.Time `json:"revokedAt,omitempty"`
	RevokedBy   string     `json:"revokedBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	// Status 由 StatusAt 算出：active / scheduled / expired / paused / revoked
	Status string `json:"status"`
	// Effect 奖励 / 处罚 / 冻结 / 中性，按倍率与上限判断，供列表着色
	Effect string `json:"effect"`
}

// 规则状态。
const (
	StatusActive    = "active"
	StatusScheduled = "scheduled"
	StatusExpired   = "expired"
	StatusPaused    = "paused"
	StatusRevoked   = "revoked"
)

// 规则效果。
const (
	EffectBoost   = "boost"
	EffectPenalty = "penalty"
	EffectFreeze  = "freeze"
	EffectNeutral = "neutral"
)

// StatusAt 规则在某一时刻的状态。
func (r *Rule) StatusAt(now time.Time) string {
	switch {
	case r.RevokedAt != nil:
		return StatusRevoked
	case !r.Enabled:
		return StatusPaused
	case r.EndsAt != nil && !now.Before(*r.EndsAt):
		return StatusExpired
	case r.StartsAt != nil && now.Before(*r.StartsAt):
		return StatusScheduled
	}
	return StatusActive
}

// ActiveAt 规则在某一时刻是否生效。
func (r *Rule) ActiveAt(now time.Time) bool { return r.StatusAt(now) == StatusActive }

// Factor 规则的倍率。
func (r *Rule) Factor() Factor { return Factor{Num: int64(r.Numerator), Den: int64(r.Denominator)} }

// EffectOf 规则效果：倍率为 0 是冻结，小于 1 或带每日上限是处罚，大于 1 是奖励。
func (r *Rule) EffectOf() string {
	f := r.Factor()
	switch {
	case f.Num == 0:
		return EffectFreeze
	case f.Num < f.Den:
		return EffectPenalty
	case f.Num > f.Den:
		return EffectBoost
	case r.DailyCap > 0:
		return EffectPenalty
	}
	return EffectNeutral
}

// Decorate 填上派生字段（状态、效果）。
func (r *Rule) Decorate(now time.Time) {
	r.Status = r.StatusAt(now)
	r.Effect = r.EffectOf()
	if r.Sources == nil {
		r.Sources = []string{}
	}
}

// Covers 规则是否作用于这个来源：没有列出来源时作用于全部默认来源。
func (r *Rule) Covers(source string) bool {
	if len(r.Sources) == 0 {
		spec, ok := FindSource(source)
		return ok && spec.Default
	}
	for _, item := range r.Sources {
		if item == source {
			return true
		}
	}
	return false
}

// RuleInput 新建 / 修改规则的输入（已解析、未校验）。
type RuleInput struct {
	AppID       int64
	UserID      int64
	Kind        Kind
	Numerator   int
	Denominator int
	Rounding    Rounding
	DailyCap    int64
	Sources     []string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Enabled     bool
	Label       string
	Reason      string
	Operator    string
}

// RuleQuery 规则列表筛选。Scope：all / app（只看全应用）/ user（只看个人规则）。
type RuleQuery struct {
	AppID   int64
	UserID  int64
	Scope   string
	Kind    Kind
	Status  string
	Effect  string
	Keyword string
	Page    int
	Limit   int
}

// Page 分页结果。
type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

// Event 一条调控记录（建规则、改规则、撤销、等级封顶 / 解除……），用于审计与用户详情页的时间线。
type Event struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	UserID    int64          `json:"userId"`
	RuleID    int64          `json:"ruleId,omitempty"`
	Action    string         `json:"action"`
	Summary   string         `json:"summary"`
	Detail    map[string]any `json:"detail"`
	Operator  string         `json:"operator,omitempty"`
	Note      string         `json:"note,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// 调控记录动作。
const (
	ActionRuleCreate = "rule_create"
	ActionRuleUpdate = "rule_update"
	ActionRuleRevoke = "rule_revoke"
	ActionRuleDelete = "rule_delete"
	ActionLevelCap   = "level_cap"
	ActionLevelUncap = "level_uncap"
)

// Usage 一个人某一天某一类型的入账统计（只在有规则作用时记录）。
type Usage struct {
	Day        string `json:"day"`
	Kind       Kind   `json:"kind"`
	Base       int64  `json:"base"`
	Granted    int64  `json:"granted"`
	Suppressed int64  `json:"suppressed"`
	Events     int64  `json:"events"`
}

// Outcome 一次入账经过规则之后的结果，写进流水的 extra_data.growth。
type Outcome struct {
	Base      int64   `json:"base"`
	Scaled    int64   `json:"scaled"`
	Granted   int64   `json:"granted"`
	Num       int64   `json:"num"`
	Den       int64   `json:"den"`
	Rounding  string  `json:"rounding"`
	DailyCap  int64   `json:"dailyCap,omitempty"`
	UsedToday int64   `json:"usedToday,omitempty"`
	Capped    bool    `json:"capped,omitempty"`
	RuleIDs   []int64 `json:"ruleIds"`
	Source    string  `json:"source"`
}

// Multiplier 写进流水 multiplier 列（NUMERIC(4,2)）的值：截到 [0, 99.99]。
func (o Outcome) Multiplier() float64 {
	if o.Den <= 0 {
		return 1
	}
	value := float64(o.Num) / float64(o.Den)
	if value > 99.99 {
		value = 99.99
	}
	return float64(int64(value*100+0.5)) / 100
}

// Touched 规则是否改变了这笔入账。
func (o Outcome) Touched() bool { return o.Granted != o.Base }
