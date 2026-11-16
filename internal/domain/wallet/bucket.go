package wallet

import (
	"errors"

	"github.com/shopspring/decimal"
)

// 余额分两个桶：
//
//   - 充值余额（本金，cash）：用户真金白银付进来的钱，以及余额支付订单退回来的那部分本金。
//   - 赠送余额（bonus）：充值档位的赠送、积分商城换来的余额、卡密、邀请、排行榜、物品发放、
//     管理员发放等一切「没花钱拿到的」余额。
//
// 分桶只为一件事：**只有本金能兑换积分**。积分商城可以用积分换余额，
// 如果换来的余额又能原样换回积分，两条路一接就是一台套利机（例如商城里 1000 积分换 12 元、
// 余额 1 元换 100 积分）。账本上把两种钱分开记，兑换时只认本金，这条环路就断了。
//
// 不变量（数据库 CHECK 兜底）：0 ≤ bonus_balance ≤ balance，0 ≤ frozen_bonus ≤ frozen。
// 充值余额不单独存列，恒等于 balance - bonus_balance，两列之间不可能对不上。
const (
	BucketCash  = "cash"
	BucketBonus = "bonus"
)

// 出账时从哪个桶扣。
const (
	// SpendBonusFirst 先扣赠送余额、不够再扣本金。默认策略：
	// 用户总是希望「送的钱先花掉」，本金留着还能兑积分、退款。
	SpendBonusFirst = ""
	// SpendCashFirst 先扣本金。用于充值退款冲正：退回渠道的是本金，扣回的也应当是本金。
	SpendCashFirst = "cash_first"
	// SpendCashOnly 只扣本金，不够就拒绝。余额兑换积分固定走这条。
	SpendCashOnly = "cash_only"
	// SpendBonusOnly 只扣赠送余额。管理员指定从赠送余额里扣减时用。
	SpendBonusOnly = "bonus_only"
)

var (
	// ErrInsufficientFunds 总余额不够
	ErrInsufficientFunds = errors.New("wallet: insufficient balance")
	// ErrInsufficientCash 本金不够（只扣本金的出账）
	ErrInsufficientCash = errors.New("wallet: insufficient cash balance")
	// ErrInsufficientBonus 赠送余额不够（只扣赠送余额的出账）
	ErrInsufficientBonus = errors.New("wallet: insufficient bonus balance")
)

// DefaultInflowBucket 入账默认落进哪个桶。
//
// 只有「真付了钱」的两类进本金：在线充值，以及余额支付订单的退款（按原单拆分退回，
// 原单查不到时才按这里的默认值落本金）。其余一律进赠送余额 —— 新增入账类型时
// 漏改这里，结果是把钱放进不能兑积分的那一桶，宁可保守。
func DefaultInflowBucket(txnType string) string {
	switch txnType {
	case TxnTypeRecharge, TxnTypeRefund:
		return BucketCash
	default:
		return BucketBonus
	}
}

// DefaultSpend 出账类型的默认扣款策略。
func DefaultSpend(txnType string) string {
	switch txnType {
	case TxnTypeIntegralExchange:
		return SpendCashOnly
	case TxnTypeRefund:
		// 出账方向的 refund 只有一种：充值订单退款后扣回充值入账
		return SpendCashFirst
	default:
		return SpendBonusFirst
	}
}

// Allocation 一次变更在两个桶上的拆分（与 Amount 同号）。
type Allocation struct {
	Cash  decimal.Decimal
	Bonus decimal.Decimal
}

// Allocate 计算一次余额变更落在两个桶上的数额。
//
//   - balance / bonus 是变更前（已锁行）的总余额与赠送余额；
//   - restorable 非 nil 表示这是一笔「退回」：原出账里扣掉的赠送余额还有多少没退回，
//     退回时先把这部分补回赠送余额，余下才进本金 —— 否则「用赠送余额下单再退款」
//     就能把赠送余额洗成本金，再拿去兑积分。
func Allocate(balance, bonus decimal.Decimal, c Change, restorable *decimal.Decimal) (Allocation, error) {
	amount := c.Amount
	if amount.IsPositive() {
		if restorable != nil {
			back := decimal.Min(decimal.Max(*restorable, decimal.Zero), amount)
			return Allocation{Bonus: back, Cash: amount.Sub(back)}, nil
		}
		bucket := c.Bucket
		if bucket != BucketCash && bucket != BucketBonus {
			bucket = DefaultInflowBucket(c.Type)
		}
		if bucket == BucketCash {
			return Allocation{Cash: amount, Bonus: decimal.Zero}, nil
		}
		return Allocation{Cash: decimal.Zero, Bonus: amount}, nil
	}

	need := amount.Neg()
	if bonus.GreaterThan(balance) {
		// 防御：CHECK 约束保证不会出现，万一出现也不让本金算成负数
		bonus = balance
	}
	cash := balance.Sub(bonus)
	spend := c.Spend
	if c.Type == TxnTypeIntegralExchange {
		// 兑换积分只认本金，调用方传什么都不放宽
		spend = SpendCashOnly
	} else if spend == "" {
		spend = DefaultSpend(c.Type)
	}
	var fromBonus decimal.Decimal
	switch spend {
	case SpendCashOnly:
		if need.GreaterThan(cash) {
			return Allocation{}, ErrInsufficientCash
		}
		fromBonus = decimal.Zero
	case SpendBonusOnly:
		if need.GreaterThan(bonus) {
			return Allocation{}, ErrInsufficientBonus
		}
		fromBonus = need
	case SpendCashFirst:
		if need.GreaterThan(balance) {
			return Allocation{}, ErrInsufficientFunds
		}
		fromCash := decimal.Min(cash, need)
		fromBonus = need.Sub(fromCash)
	default:
		if need.GreaterThan(balance) {
			return Allocation{}, ErrInsufficientFunds
		}
		fromBonus = decimal.Min(bonus, need)
	}
	return Allocation{Bonus: fromBonus.Neg(), Cash: need.Sub(fromBonus).Neg()}, nil
}
