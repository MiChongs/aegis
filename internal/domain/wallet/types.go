package wallet

import (
	"time"

	"github.com/shopspring/decimal"
)

// 流水类型
const (
	TxnTypeRecharge    = "recharge"     // 在线支付充值
	TxnTypeConsume     = "consume"      // 业务消费
	TxnTypeRefund      = "refund"       // 退款入账
	TxnTypeAdminAdjust = "admin_adjust" // 管理员调整（正负皆可）
	TxnTypeVipPurchase = "vip_purchase" // 余额购买 VIP
	TxnTypeOrderPay    = "order_pay"    // 余额支付订单
	// TxnTypeCardKey 卡密核销入账。刻意不复用 recharge：卡密不是充值，
	// 算进 total_recharged 会让充值报表凭空虚高（applyWalletChangeTx 只把
	// recharge / refund 计入累计充值，这里正是靠类型区分开的）。
	TxnTypeCardKey = "card_key"
)

// Wallet 用户钱包
type Wallet struct {
	UserID         int64           `json:"userId"`
	AppID          int64           `json:"appid"`
	Balance        decimal.Decimal `json:"balance"`
	Frozen         decimal.Decimal `json:"frozen"`
	TotalRecharged decimal.Decimal `json:"totalRecharged"`
	TotalConsumed  decimal.Decimal `json:"totalConsumed"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// Transaction 钱包流水
type Transaction struct {
	ID             int64           `json:"id"`
	TransactionNo  string          `json:"transactionNo"`
	UserID         int64           `json:"userId"`
	AppID          int64           `json:"appid"`
	Type           string          `json:"type"`
	Amount         decimal.Decimal `json:"amount"`
	BalanceBefore  decimal.Decimal `json:"balanceBefore"`
	BalanceAfter   decimal.Decimal `json:"balanceAfter"`
	RelatedOrderNo string          `json:"relatedOrderNo,omitempty"`
	IdempotencyKey string          `json:"-"`
	Title          string          `json:"title"`
	Remark         string          `json:"remark,omitempty"`
	Operator       string          `json:"operator,omitempty"`
	ClientIP       string          `json:"clientIp,omitempty"`
	Metadata       map[string]any  `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// Change 一次余额变更指令（仓储层在单事务中执行：锁钱包 → 校验 → 更新 → 记账）
type Change struct {
	UserID         int64
	AppID          int64
	Type           string
	Amount         decimal.Decimal // 正数入账、负数出账
	RelatedOrderNo string
	IdempotencyKey string // 非空时幂等：重复提交返回首次流水，不重复入账
	Title          string
	Remark         string
	Operator       string
	ClientIP       string
	Metadata       map[string]any
}

// ChangeResult 变更结果
type ChangeResult struct {
	Transaction Transaction `json:"transaction"`
	Wallet      Wallet      `json:"wallet"`
	// Replayed 为 true 表示命中幂等键，本次未实际变更余额（返回的是首次流水）
	Replayed bool `json:"replayed,omitempty"`
	// Receipt 这笔流水的凭证入口。由服务层填充（仓储层不认识凭证）。
	// 扣完款当场把凭证入口一并给出，客户端不必为了拿下载地址再拉一次列表。
	Receipt *TransactionReceipt `json:"receipt,omitempty"`
}

// ListQuery 流水查询
type ListQuery struct {
	Type  string `json:"type"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// ListResult 流水分页结果
type ListResult struct {
	Items      []Transaction `json:"items"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	Total      int64         `json:"total"`
	TotalPages int           `json:"totalPages"`
}
