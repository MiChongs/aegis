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
)

// Plan VIP 套餐（按应用配置）
type Plan struct {
	ID            int64            `json:"id"`
	AppID         int64            `json:"appid"`
	Name          string           `json:"name"`
	DurationDays  int              `json:"durationDays"`
	Price         decimal.Decimal  `json:"price"`
	OriginalPrice *decimal.Decimal `json:"originalPrice,omitempty"`
	BonusIntegral int64            `json:"bonusIntegral"`
	Description   string           `json:"description,omitempty"`
	IsActive      bool             `json:"isActive"`
	SortOrder     int              `json:"sortOrder"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

// PlanMutation 套餐创建/更新（指针字段为空表示不变更）
type PlanMutation struct {
	ID            int64
	AppID         int64
	Name          *string
	DurationDays  *int
	Price         *decimal.Decimal
	OriginalPrice *decimal.Decimal
	BonusIntegral *int64
	Description   *string
	IsActive      *bool
	SortOrder     *int
}

// Grant 一次 VIP 开通/续期指令（仓储层单事务执行：锁用户 → 顺延到期时间 → 记账）
type Grant struct {
	UserID         int64
	AppID          int64
	PlanID         *int64
	PlanName       string
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
}
