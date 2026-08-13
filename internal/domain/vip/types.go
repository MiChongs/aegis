package vip

import (
	"time"

	"github.com/shopspring/decimal"
)

// 开通渠道
const (
	ChannelWallet       = "wallet"        // 余额购买
	ChannelPaymentOrder = "payment_order" // 在线支付直购
	ChannelAdminGrant   = "admin_grant"   // 管理员授予
	ChannelTrial        = "trial"         // 领取试用
	ChannelCardKey      = "card_key"      // 卡密核销
)

// 套餐种类。
//
// 试用是**套餐的一种**，不是另一套时长体系：它一样落 vip_transactions、
// 一样只把 vip_expire_at 往后推。差别只在入口（领取而非购买）与资格（一人一次）。
const (
	KindPaid  = "paid"
	KindTrial = "trial"
)

// Plan VIP 套餐（按应用配置）
type Plan struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"appid"`
	Name   string `json:"name"`
	Kind   string `json:"kind"` // paid / trial
	// TrialDeviceLimited 仅试用套餐有意义：同一设备只能领一次。
	// 防的是"注册小号反复领试用"，代价是请求必须带设备标识（否则拒领而不是放行 ——
	// 开着的开关放行等于没有这个开关）。
	TrialDeviceLimited bool `json:"trialDeviceLimited"`
	// Features 这个套餐包含的功能标识（引用 vip_features.tag）。
	// 空数组即"只是会员"，不带任何细分权益。
	Features           []string         `json:"features"`
	DurationDays       int              `json:"durationDays"`
	Price              decimal.Decimal  `json:"price"`
	OriginalPrice      *decimal.Decimal `json:"originalPrice,omitempty"`
	BonusIntegral      int64            `json:"bonusIntegral"`
	Description        string           `json:"description,omitempty"`
	IsActive           bool             `json:"isActive"`
	SortOrder          int              `json:"sortOrder"`
	CreatedAt          time.Time        `json:"createdAt"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

// IsTrial 是否试用套餐。
func (p Plan) IsTrial() bool { return p.Kind == KindTrial }

// PlanMutation 套餐创建/更新（指针字段为空表示不变更）
type PlanMutation struct {
	ID                 int64
	AppID              int64
	Name               *string
	Kind               *string
	TrialDeviceLimited *bool
	// Features nil 表示不变更；空切片表示清空
	Features           *[]string
	DurationDays       *int
	Price              *decimal.Decimal
	OriginalPrice      *decimal.Decimal
	BonusIntegral      *int64
	Description        *string
	IsActive           *bool
	SortOrder          *int
}

// Grant 一次 VIP 开通/续期指令（仓储层单事务执行：锁用户 → 顺延到期时间 → 记账）
type Grant struct {
	UserID   int64
	AppID    int64
	PlanID   *int64
	PlanName string
	// Features 开通那一刻套餐包含的功能标识，**按值快照**落进账本。
	// 不存快照的话，运营明天把某个功能从套餐里拿掉，已经卖出去的会员当场少一项权益。
	Features       []string
	DurationDays   int
	PayChannel     string
	PayAmount      decimal.Decimal
	RelatedOrderNo string
	BonusIntegral  int64
	Operator       string
	Metadata       map[string]any
}

// Transaction VIP 开通/续费记录
type Transaction struct {
	ID             int64           `json:"id"`
	TransactionNo  string          `json:"transactionNo"`
	UserID         int64           `json:"userId"`
	AppID          int64           `json:"appid"`
	PlanID         *int64          `json:"planId,omitempty"`
	PlanName       string          `json:"planName"`
	Features       []string        `json:"features"`
	DurationDays   int             `json:"durationDays"`
	PayChannel     string          `json:"payChannel"`
	PayAmount      decimal.Decimal `json:"payAmount"`
	RelatedOrderNo string          `json:"relatedOrderNo,omitempty"`
	BonusIntegral  int64           `json:"bonusIntegral"`
	ExpireBefore   *time.Time      `json:"expireBefore,omitempty"`
	ExpireAfter    time.Time       `json:"expireAfter"`
	Operator       string          `json:"operator,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// Status 用户 VIP 状态
type Status struct {
	IsVIP         bool       `json:"isVip"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	RemainingDays int        `json:"remainingDays"`
}

// PurchaseResult 购买结果
type PurchaseResult struct {
	Transaction   Transaction     `json:"transaction"`
	Status        Status          `json:"status"`
	WalletBalance decimal.Decimal `json:"walletBalance"`
	BonusIntegral int64           `json:"bonusIntegral"`
	// WalletTransactionNo 扣款对应的钱包流水号。凭证由它出具 ——
	// 余额直购不产生支付订单，这是这笔购买唯一的资金凭据。
	// 0 元套餐不动钱包，此处为空。
	WalletTransactionNo string `json:"walletTransactionNo,omitempty"`
	// Replayed 命中幂等键，返回的是首次购买结果，本次未实际扣款
	Replayed bool `json:"replayed,omitempty"`
	// Receipt 这笔购买的凭证入口；未接入凭证引擎或 0 元套餐时为空
	Receipt *ReceiptEntry `json:"receipt,omitempty"`
}

// ReceiptEntry 会员购买的凭证入口。
//
// 字段与钱包流水的凭证入口一致，客户端可以用同一段代码渲染按钮。
// 之所以不直接复用 walletdomain 的类型：domain 之间互相 import 会
// 把「会员」和「钱包」两个本可独立演进的包焊死在一起。
type ReceiptEntry struct {
	Available     bool   `json:"available"`
	TransactionNo string `json:"transactionNo,omitempty"`
	DocumentType  string `json:"documentType,omitempty"`
	Locale        string `json:"locale,omitempty"`
	Currency      string `json:"currency,omitempty"`
	DownloadURL   string `json:"downloadUrl,omitempty"`
	ExportURL     string `json:"exportUrl,omitempty"`
	EmailURL      string `json:"emailUrl,omitempty"`
	Emailable     bool   `json:"emailable"`
	EmailHint     string `json:"emailHint,omitempty"`
}
