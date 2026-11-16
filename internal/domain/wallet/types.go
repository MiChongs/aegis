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
	// TxnTypePointsExchange 积分兑换余额入账。与卡密同理，不计入累计充值。
	TxnTypePointsExchange = "points_exchange"
	// TxnTypeInvite 邀请奖励入账。同理不计入累计充值。
	TxnTypeInvite = "invite"
	// TxnTypeLeaderboard 排行榜奖励入账。同理不计入累计充值。
	TxnTypeLeaderboard = "leaderboard"
	// TxnTypeItemGrant 物品发放入账。同理不计入累计充值。
	TxnTypeItemGrant = "item_grant"
	// TxnTypeAIRefund AI 中转网关的调用退款（管理员整笔退回）。把消费退回来，不是充值，
	// 因此同样不计入累计充值。
	TxnTypeAIRefund = "ai_refund"
	// TxnTypeRechargeBonus 充值档位附赠的余额，进赠送余额，不计入累计充值。
	TxnTypeRechargeBonus = "recharge_bonus"
	// TxnTypeIntegralExchange 余额兑换积分（出账），只扣充值余额（本金）。
	// 计入 total_exchanged 而不是 total_consumed：钱没花在业务上，只是换成了积分。
	TxnTypeIntegralExchange = "integral_exchange"
)

// Wallet 用户钱包
//
// Balance 是可用总余额 = CashBalance + BonusBalance。两个分项都随响应下发，
// 客户端不必自己相减（也就不会出现小数尾差）。
type Wallet struct {
	UserID  int64           `json:"userId"`
	AppID   int64           `json:"appid"`
	Balance decimal.Decimal `json:"balance"`
	// CashBalance 充值余额（本金）：可兑换积分
	CashBalance decimal.Decimal `json:"cashBalance"`
	// BonusBalance 赠送余额：可消费，不可兑换积分
	BonusBalance   decimal.Decimal `json:"bonusBalance"`
	Frozen         decimal.Decimal `json:"frozen"`
	FrozenBonus    decimal.Decimal `json:"frozenBonus"`
	TotalRecharged decimal.Decimal `json:"totalRecharged"`
	TotalConsumed  decimal.Decimal `json:"totalConsumed"`
	// TotalBonus 累计获赠（不含退回）
	TotalBonus decimal.Decimal `json:"totalBonus"`
	// TotalExchanged 累计兑换成积分的本金
	TotalExchanged decimal.Decimal `json:"totalExchanged"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// Normalize 由 Balance 与 BonusBalance 推出 CashBalance。
func (w *Wallet) Normalize() {
	if w.BonusBalance.GreaterThan(w.Balance) {
		w.BonusBalance = w.Balance
	}
	w.CashBalance = w.Balance.Sub(w.BonusBalance)
}

// Transaction 钱包流水
type Transaction struct {
	ID            int64           `json:"id"`
	TransactionNo string          `json:"transactionNo"`
	UserID        int64           `json:"userId"`
	AppID         int64           `json:"appid"`
	Type          string          `json:"type"`
	Amount        decimal.Decimal `json:"amount"`
	// CashAmount / BonusAmount 这笔变动在本金与赠送余额上的拆分（与 Amount 同号，两者之和恒等于 Amount）
	CashAmount     decimal.Decimal `json:"cashAmount"`
	BonusAmount    decimal.Decimal `json:"bonusAmount"`
	BalanceBefore  decimal.Decimal `json:"balanceBefore"`
	BalanceAfter   decimal.Decimal `json:"balanceAfter"`
	RelatedOrderNo string          `json:"relatedOrderNo,omitempty"`
	// RestoresTxnNo 退回类流水指向被退回的那笔出账
	RestoresTxnNo  string         `json:"restoresTransactionNo,omitempty"`
	IdempotencyKey string         `json:"-"`
	Title          string         `json:"title"`
	Remark         string         `json:"remark,omitempty"`
	Operator       string         `json:"operator,omitempty"`
	ClientIP       string         `json:"clientIp,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
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
	// Bucket 入账落哪个桶（cash / bonus）；空表示按类型默认（DefaultInflowBucket）
	Bucket string
	// Spend 出账扣款策略；空表示按类型默认（DefaultSpend）
	Spend string
	// Restores 非 nil 表示这笔入账是在退回某笔出账：按原出账的拆分先补回赠送余额
	Restores *RestoreRef
}

// RestoreRef 定位被退回的那笔出账。三个字段按顺序取第一个非空的：
// 流水号 → 幂等键 → 关联订单号（配合 Type）。
type RestoreRef struct {
	TransactionNo  string
	IdempotencyKey string
	OrderNo        string
	Type           string
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
