package wallet

import (
	"time"

	"github.com/shopspring/decimal"
)

// 钱包流水的凭证入口与查询视图。
//
// 钱包流水与支付订单是**两条并行的资金记录**，此前只有后者能出凭证：
// 余额直购会员、业务消费、管理员调账都只落 wallet_transactions，
// 用户手里就只有一行流水，拿不到任何可归档、可报销、可对账的文件。
//
// 补齐的方式不是再造一套凭证引擎，而是让钱包流水成为凭证的第二类主体，
// 与订单共用 pkg/receipt 的同一份排版、同一份译文与同一套导出/寄送链路。

// 凭证出具方：同一笔钱只能有一份凭证，因此流水挂着订单时由订单出具。
const (
	// ReceiptSourceWallet 由钱包流水自身出具（无关联订单）
	ReceiptSourceWallet = "wallet"
	// ReceiptSourceOrder 由关联的支付订单出具（充值到账、余额支付订单等）
	ReceiptSourceOrder = "order"
)

// TransactionReceipt 挂在钱包流水上的凭证入口。
//
// 结构与 paymentdomain.OrderReceipt 保持一致，客户端可以用同一段代码渲染
// 「下载 / 导出 / 寄邮件」三个按钮；多出来的 Source 与 OrderNo 用于说明
// 这份凭证到底由谁出具 —— 否则用户会疑惑为什么下载下来的编号是 RCP- 而不是 WAL-。
type TransactionReceipt struct {
	// Available 该流水能否出具凭证
	Available bool `json:"available"`
	// Source 凭证出具方：wallet（流水自身）/ order（关联订单）
	Source string `json:"source,omitempty"`
	// OrderNo Source 为 order 时的订单号
	OrderNo string `json:"orderNo,omitempty"`
	// DocumentType receipt / invoice / credit_note
	DocumentType string `json:"documentType,omitempty"`
	// Locale 按当前请求协商出的推荐语言
	Locale string `json:"locale,omitempty"`
	// Currency 凭证上的币种
	Currency string `json:"currency,omitempty"`
	// DownloadURL 直接取 PDF
	DownloadURL string `json:"downloadUrl,omitempty"`
	// ExportURL 生成可分享的一次性下载凭据
	ExportURL string `json:"exportUrl,omitempty"`
	// EmailURL 寄送到账号邮箱
	EmailURL string `json:"emailUrl,omitempty"`
	// Emailable 能否寄送（账号已绑邮箱且邮件出口可用）
	Emailable bool `json:"emailable"`
	// EmailHint 不能寄送时的原因，可直接展示给用户
	EmailHint string `json:"emailHint,omitempty"`
}

// TransactionView 钱包流水 + 凭证入口。
// Transaction 内嵌，JSON 上是扁平的，对既有客户端是纯增量。
type TransactionView struct {
	Transaction
	Receipt TransactionReceipt `json:"receipt"`
}

// TransactionViewListResult 带凭证入口的流水分页
type TransactionViewListResult struct {
	Items      []TransactionView `json:"items"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"totalPages"`
}

// AdminListQuery 管理端全应用流水查询。
//
// 与用户侧的 ListQuery 分开：用户只能看自己的，条件天然收敛在会话上；
// 管理端要按用户、单号、时间窗、方向筛，把两者合成一个结构会让用户侧
// 多出一批「传了也不该生效」的字段。
type AdminListQuery struct {
	// UserID 限定用户；0 表示全应用
	UserID int64 `json:"userId"`
	// Type 流水类型（recharge / consume / …）
	Type string `json:"type"`
	// Direction 资金方向：in（入账）/ out（出账）/ 空（不限）
	Direction string `json:"direction"`
	// Keyword 模糊匹配流水号、关联订单号、标题、备注
	Keyword string `json:"keyword"`
	// Start / End 创建时间窗（闭区间，零值表示不限）
	Start *time.Time `json:"start"`
	End   *time.Time `json:"end"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}

// 资金方向取值
const (
	DirectionIn  = "in"
	DirectionOut = "out"
)

// AdminTransactionItem 管理端流水行：流水本体 + 账号信息。
//
// 带上账号是因为管理端列表按应用而不是按用户拉取，只给 userId
// 会让每看一行都要再查一次用户表 —— 那既是 N+1，也让列表无法搜索账号。
type AdminTransactionItem struct {
	Transaction
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// AdminTransactionListResult 管理端流水分页
type AdminTransactionListResult struct {
	Items      []AdminTransactionItem `json:"items"`
	Page       int                    `json:"page"`
	Limit      int                    `json:"limit"`
	Total      int64                  `json:"total"`
	TotalPages int                    `json:"totalPages"`
}

// TypeStat 按流水类型的聚合
type TypeStat struct {
	Type   string          `json:"type"`
	Count  int64           `json:"count"`
	Amount decimal.Decimal `json:"amount"`
}

// Stats 钱包资金面板。
//
// 入账与出账分开统计而不是只给净额：净额为零既可能是「没有交易」，
// 也可能是「充了一万又花了一万」，这两种情况的运营含义完全相反。
type Stats struct {
	// TotalIn 入账合计（正数）
	TotalIn decimal.Decimal `json:"totalIn"`
	// TotalOut 出账合计（正数，已取绝对值）
	TotalOut decimal.Decimal `json:"totalOut"`
	// Net 净额 = 入账 - 出账
	Net decimal.Decimal `json:"net"`
	// Count 流水笔数
	Count int64 `json:"count"`
	// UserCount 发生过流水的用户数
	UserCount int64 `json:"userCount"`
	// Balance 当前全应用钱包余额合计（即平台的待兑付负债）
	Balance decimal.Decimal `json:"balance"`
	// ByType 按类型拆分
	ByType []TypeStat `json:"byType"`
}
