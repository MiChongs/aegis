package wallet

import (
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Settings 应用级钱包设置（存在应用 settings 的 "wallet" 键下）。
//
// 兑换比例不在这里：余额兑积分与在线直购积分用同一个比例
// （CommerceSettings.IntegralPerCurrency），两条路价格不一致时，
// 用户自然会走便宜的那条，另一条的定价就形同虚设。
type Settings struct {
	// RechargeEnabled 是否开放充值
	RechargeEnabled bool `json:"rechargeEnabled"`
	// CustomAmountEnabled 是否允许自定义金额；关闭时只能按档位充值
	CustomAmountEnabled bool `json:"customAmountEnabled"`
	// RechargeMin / RechargeMax 自定义金额的单笔范围（档位不受此限）
	RechargeMin decimal.Decimal `json:"rechargeMin"`
	RechargeMax decimal.Decimal `json:"rechargeMax"`

	// ExchangeEnabled 是否开放充值余额兑换积分
	ExchangeEnabled bool `json:"exchangeEnabled"`
	// ExchangeMin 单笔最少兑换的本金
	ExchangeMin decimal.Decimal `json:"exchangeMin"`
	// ExchangeMax 单笔最多兑换的本金，0 不限
	ExchangeMax decimal.Decimal `json:"exchangeMax"`
	// ExchangeDailyMax 每人每天最多兑换的本金（应用默认时区自然日），0 不限
	ExchangeDailyMax decimal.Decimal `json:"exchangeDailyMax"`

	// Notice 钱包页底部的说明文字（充值须知等），可空
	Notice string `json:"notice"`
}

// MaxNoticeLength 说明文字上限（字符）
const MaxNoticeLength = 500

// 金额上限：与余额列 NUMERIC(18,2) 的整数位数留足余量，也挡住多打几个 0 的误操作
var maxSettingAmount = decimal.NewFromInt(1_000_000)

// DefaultSettings 未配置时的默认值：充值与兑换都开放，自定义金额 1–5000。
func DefaultSettings() Settings {
	return Settings{
		RechargeEnabled:     true,
		CustomAmountEnabled: true,
		RechargeMin:         decimal.NewFromInt(1),
		RechargeMax:         decimal.NewFromInt(5000),
		ExchangeEnabled:     true,
		ExchangeMin:         decimal.NewFromInt(1),
		ExchangeMax:         decimal.Zero,
		ExchangeDailyMax:    decimal.Zero,
	}
}

// ErrInvalidSettings 设置不合法（消息即原因）
type ErrInvalidSettings struct{ Reason string }

func (e *ErrInvalidSettings) Error() string { return e.Reason }

// Validate 校验并规整设置。
func (s *Settings) Validate() error {
	s.Notice = strings.TrimSpace(s.Notice)
	if len([]rune(s.Notice)) > MaxNoticeLength {
		return &ErrInvalidSettings{"说明文字最多 500 字"}
	}
	amounts := []struct {
		name  string
		value *decimal.Decimal
		zero  bool
	}{
		{"自定义充值下限", &s.RechargeMin, false},
		{"自定义充值上限", &s.RechargeMax, false},
		{"单笔兑换下限", &s.ExchangeMin, false},
		{"单笔兑换上限", &s.ExchangeMax, true},
		{"每日兑换上限", &s.ExchangeDailyMax, true},
	}
	for _, a := range amounts {
		if !a.value.Equal(a.value.Round(2)) {
			return &ErrInvalidSettings{a.name + "最多保留两位小数"}
		}
		if a.value.IsNegative() || (!a.zero && !a.value.IsPositive()) {
			return &ErrInvalidSettings{a.name + "必须大于 0"}
		}
		if a.value.GreaterThan(maxSettingAmount) {
			return &ErrInvalidSettings{a.name + "不能超过 1,000,000"}
		}
	}
	if s.RechargeMin.GreaterThan(s.RechargeMax) {
		return &ErrInvalidSettings{"自定义充值下限不能大于上限"}
	}
	if s.ExchangeMax.IsPositive() && s.ExchangeMin.GreaterThan(s.ExchangeMax) {
		return &ErrInvalidSettings{"单笔兑换下限不能大于上限"}
	}
	if s.ExchangeDailyMax.IsPositive() && s.ExchangeMin.GreaterThan(s.ExchangeDailyMax) {
		return &ErrInvalidSettings{"单笔兑换下限不能大于每日上限"}
	}
	return nil
}

// ── 充值档位 ──

// RechargePackage 充值档位：付 Amount，本金入账 Amount，另赠 BonusAmount 余额与 BonusIntegral 积分。
type RechargePackage struct {
	ID            int64           `json:"id"`
	AppID         int64           `json:"appid"`
	Title         string          `json:"title"`
	Amount        decimal.Decimal `json:"amount"`
	BonusAmount   decimal.Decimal `json:"bonusAmount"`
	BonusIntegral int64           `json:"bonusIntegral"`
	// Badge 角标（如「首充」「超值」），可空
	Badge       string `json:"badge,omitempty"`
	Description string `json:"description,omitempty"`
	// PerUserLimit 每人限购次数，0 不限（「首充专享」设 1）
	PerUserLimit int        `json:"perUserLimit"`
	IsActive     bool       `json:"isActive"`
	SortOrder    int        `json:"sortOrder"`
	StartsAt     *time.Time `json:"startsAt,omitempty"`
	EndsAt       *time.Time `json:"endsAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// 档位字段长度上限，与表结构一致
const (
	MaxPackageTitle       = 60
	MaxPackageBadge       = 20
	MaxPackageDescription = 200
)

// Validate 校验并规整档位。
func (p *RechargePackage) Validate() error {
	p.Title = strings.TrimSpace(p.Title)
	p.Badge = strings.TrimSpace(p.Badge)
	p.Description = strings.TrimSpace(p.Description)
	switch {
	case p.Title == "":
		return &ErrInvalidSettings{"档位名称不能为空"}
	case len([]rune(p.Title)) > MaxPackageTitle:
		return &ErrInvalidSettings{"档位名称最多 60 字"}
	case len([]rune(p.Badge)) > MaxPackageBadge:
		return &ErrInvalidSettings{"角标最多 20 字"}
	case len([]rune(p.Description)) > MaxPackageDescription:
		return &ErrInvalidSettings{"档位说明最多 200 字"}
	case !p.Amount.IsPositive():
		return &ErrInvalidSettings{"充值金额必须大于 0"}
	case !p.Amount.Equal(p.Amount.Round(2)) || !p.BonusAmount.Equal(p.BonusAmount.Round(2)):
		return &ErrInvalidSettings{"金额最多保留两位小数"}
	case p.Amount.GreaterThan(maxSettingAmount) || p.BonusAmount.GreaterThan(maxSettingAmount):
		return &ErrInvalidSettings{"金额不能超过 1,000,000"}
	case p.BonusAmount.IsNegative():
		return &ErrInvalidSettings{"赠送余额不能为负数"}
	case p.BonusIntegral < 0 || p.BonusIntegral > 100_000_000:
		return &ErrInvalidSettings{"赠送积分必须在 0–100,000,000 之间"}
	case p.PerUserLimit < 0 || p.PerUserLimit > 10_000:
		return &ErrInvalidSettings{"每人限购次数必须在 0–10000 之间"}
	case p.StartsAt != nil && p.EndsAt != nil && !p.EndsAt.After(*p.StartsAt):
		return &ErrInvalidSettings{"结束时间必须晚于开始时间"}
	}
	return nil
}

// 档位当前不可购买的原因
const (
	PackageAvailable    = ""
	PackageInactive     = "inactive"
	PackageNotStarted   = "not_started"
	PackageEnded        = "ended"
	PackageLimitReached = "limit_reached"
)

// Availability 档位此刻对某个用户是否可买（purchased 为该用户已付款的次数）。
func (p RechargePackage) Availability(now time.Time, purchased int) string {
	switch {
	case !p.IsActive:
		return PackageInactive
	case p.StartsAt != nil && now.Before(*p.StartsAt):
		return PackageNotStarted
	case p.EndsAt != nil && !now.Before(*p.EndsAt):
		return PackageEnded
	case p.PerUserLimit > 0 && purchased >= p.PerUserLimit:
		return PackageLimitReached
	}
	return PackageAvailable
}

// PackageView 用户侧档位：档位本体 + 该用户已买次数与可买状态。
type PackageView struct {
	RechargePackage
	Purchased int `json:"purchased"`
	// Unavailable 不可买的原因（见 Package* 常量），可买时为空
	Unavailable string `json:"unavailable,omitempty"`
}

// ── 兑换 ──

var errRate = errors.New("wallet: exchange rate must be positive")

// QuoteExchange 按比例计算一笔兑换：amount 本金最多换多少积分、实际只扣多少本金。
//
// 积分是整数：points = floor(amount × rate)。扣款按「换到的积分值多少钱」向上取到分：
// cost = ceil(points / rate, 0.01)，cost ≤ amount。比例能整除时 cost 就等于 amount；
// 不能整除时多出的零头不扣（例如比例 7、输入 0.50 只换 3 积分、扣 0.43）。
func QuoteExchange(amount decimal.Decimal, rate int64) (points int64, cost decimal.Decimal, err error) {
	if rate <= 0 {
		return 0, decimal.Zero, errRate
	}
	r := decimal.NewFromInt(rate)
	points = amount.Mul(r).Floor().IntPart()
	if points <= 0 {
		return 0, decimal.Zero, nil
	}
	cost = decimal.NewFromInt(points).Div(r).Mul(decimal.NewFromInt(100)).Ceil().Div(decimal.NewFromInt(100))
	if cost.GreaterThan(amount) {
		// 浮点除法的尾差兜底：不可能多扣
		cost = amount
	}
	return points, cost.Round(2), nil
}

// ExchangeInput 一次兑换请求（仓储层在单事务里执行）。
type ExchangeInput struct {
	UserID int64
	AppID  int64
	// Amount 要兑换的本金；All 为 true 时忽略，按锁行后的全部本金兑换
	Amount decimal.Decimal
	All    bool
	Rate   int64
	// Settings 下限、单笔上限、每日上限
	Settings Settings
	// DayStart 「今天」的起点（应用默认时区），用于每日上限
	DayStart       time.Time
	IdempotencyKey string
	ClientIP       string
	UserAgent      string
}

// ExchangeResult 兑换结果。
type ExchangeResult struct {
	Transaction           Transaction `json:"transaction"`
	Wallet                Wallet      `json:"wallet"`
	Integral              int64       `json:"integral"`
	IntegralBalance       int64       `json:"integralBalance"`
	IntegralTransactionNo string      `json:"integralTransactionNo"`
	Rate                  int64       `json:"rate"`
	Replayed              bool        `json:"replayed,omitempty"`
}

// 兑换被拒的原因（仓储层判定，服务层翻译成错误码）
var (
	ErrExchangeTooSmall   = errors.New("wallet: exchange amount below minimum")
	ErrExchangeTooLarge   = errors.New("wallet: exchange amount above per-transaction maximum")
	ErrExchangeDailyLimit = errors.New("wallet: exchange daily limit reached")
	ErrExchangeNoPoints   = errors.New("wallet: exchange amount too small to yield a point")
)

// ExchangeQuota 用户侧兑换额度视图。
type ExchangeQuota struct {
	Enabled bool  `json:"enabled"`
	Rate    int64 `json:"rate"`
	// Exchangeable 当前可兑换的本金
	Exchangeable decimal.Decimal `json:"exchangeable"`
	Min          decimal.Decimal `json:"min"`
	Max          decimal.Decimal `json:"max"`
	DailyMax     decimal.Decimal `json:"dailyMax"`
	// UsedToday 今天已兑换的本金
	UsedToday decimal.Decimal `json:"usedToday"`
	// RemainingToday 今天还能兑换的本金；每日不限时等于 Exchangeable
	RemainingToday decimal.Decimal `json:"remainingToday"`
	// MaxNow 此刻单笔最多能兑换的本金 = min(本金, 单笔上限, 今日剩余)
	MaxNow decimal.Decimal `json:"maxNow"`
}

// BuildExchangeQuota 由设置、本金与今日已兑算出额度。
func BuildExchangeQuota(s Settings, rate int64, cash, usedToday decimal.Decimal) ExchangeQuota {
	q := ExchangeQuota{
		Enabled: s.ExchangeEnabled, Rate: rate, Exchangeable: cash,
		Min: s.ExchangeMin, Max: s.ExchangeMax, DailyMax: s.ExchangeDailyMax, UsedToday: usedToday,
	}
	q.RemainingToday = cash
	if s.ExchangeDailyMax.IsPositive() {
		q.RemainingToday = decimal.Max(decimal.Zero, s.ExchangeDailyMax.Sub(usedToday))
	}
	q.MaxNow = decimal.Min(cash, q.RemainingToday)
	if s.ExchangeMax.IsPositive() {
		q.MaxNow = decimal.Min(q.MaxNow, s.ExchangeMax)
	}
	return q
}

// CheckExchange 判定一笔兑换的本金是否在额度内（requested 为用户想兑的本金）。
// 本金不足不在这里判：那要在锁行之后由账本判，这里只管设置给的边界。
func CheckExchange(s Settings, requested, usedToday decimal.Decimal) error {
	if requested.LessThan(s.ExchangeMin) {
		return ErrExchangeTooSmall
	}
	if s.ExchangeMax.IsPositive() && requested.GreaterThan(s.ExchangeMax) {
		return ErrExchangeTooLarge
	}
	if s.ExchangeDailyMax.IsPositive() && usedToday.Add(requested).GreaterThan(s.ExchangeDailyMax) {
		return ErrExchangeDailyLimit
	}
	return nil
}

// Overview 用户侧钱包总览：一次请求拿齐钱包页需要的全部东西。
type Overview struct {
	Wallet   Wallet          `json:"wallet"`
	Currency string          `json:"currency"`
	Recharge RechargeOptions `json:"recharge"`
	Exchange ExchangeQuota   `json:"exchange"`
	// IntegralBalance 当前积分（兑换后客户端直接用响应里的新值）
	IntegralBalance int64  `json:"integralBalance"`
	Notice          string `json:"notice,omitempty"`
}

// RechargeOptions 用户侧充值选项。
type RechargeOptions struct {
	Enabled             bool            `json:"enabled"`
	CustomAmountEnabled bool            `json:"customAmountEnabled"`
	Min                 decimal.Decimal `json:"min"`
	Max                 decimal.Decimal `json:"max"`
	Packages            []PackageView   `json:"packages"`
}

// PackageStat 档位的销售统计（已付款订单）。
type PackageStat struct {
	Orders int64           `json:"orders"`
	Users  int64           `json:"users"`
	Amount decimal.Decimal `json:"amount"`
}

// AdminPackage 控制台档位行：档位本体 + 销售统计。
type AdminPackage struct {
	RechargePackage
	Stat PackageStat `json:"stat"`
}
