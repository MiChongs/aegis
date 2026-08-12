package service

import (
	"strings"
	"testing"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	userdomain "aegis/internal/domain/user"
	walletdomain "aegis/internal/domain/wallet"
	"aegis/pkg/receipt"

	"github.com/shopspring/decimal"
)

func walletTxn(txnType string, amount string) *walletdomain.Transaction {
	return &walletdomain.Transaction{
		ID:            9,
		TransactionNo: "WAL20260321100000ABCDEF",
		UserID:        88,
		AppID:         12,
		Type:          txnType,
		Amount:        decimal.RequireFromString(amount),
		BalanceBefore: decimal.RequireFromString("200.00"),
		BalanceAfter:  decimal.RequireFromString("200.00").Add(decimal.RequireFromString(amount)),
		Title:         "余额支付订单",
		CreatedAt:     time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC),
	}
}

func walletEnv() walletReceiptEnv {
	return walletReceiptEnv{AppName: "示例应用", Brand: "Aegis", Currency: "CNY"}
}

// 只有退款入账出「退款凭证」。其余都是已完成的账本条目，一律出收据 ——
// 钱包流水没有「待支付」这种中间态，写进表里的那一刻钱已经动了。
func TestWalletReceiptDocTypeFollowsTransactionType(t *testing.T) {
	cases := []struct {
		name     string
		txnType  string
		amount   string
		override string
		want     receipt.DocType
	}{
		{name: "余额支付订单", txnType: walletdomain.TxnTypeOrderPay, amount: "-50.00", want: receipt.TypeReceipt},
		{name: "业务消费", txnType: walletdomain.TxnTypeConsume, amount: "-12.00", want: receipt.TypeReceipt},
		{name: "会员直购", txnType: walletdomain.TxnTypeVipPurchase, amount: "-88.00", want: receipt.TypeReceipt},
		{name: "充值到账", txnType: walletdomain.TxnTypeRecharge, amount: "100.00", want: receipt.TypeReceipt},
		{name: "管理员调增", txnType: walletdomain.TxnTypeAdminAdjust, amount: "20.00", want: receipt.TypeReceipt},
		{name: "管理员调减", txnType: walletdomain.TxnTypeAdminAdjust, amount: "-20.00", want: receipt.TypeReceipt},
		{name: "退款入账出退款凭证", txnType: walletdomain.TxnTypeRefund, amount: "50.00", want: receipt.TypeCreditNote},
		{
			name: "显式指定优先", txnType: walletdomain.TxnTypeRefund, amount: "50.00",
			override: paymentdomain.ReceiptTypeReceipt, want: receipt.TypeReceipt,
		},
		{
			name: "无法识别的类型退回推导", txnType: walletdomain.TxnTypeRefund, amount: "50.00",
			override: "nonsense", want: receipt.TypeCreditNote,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveWalletReceiptDocType(walletTxn(tc.txnType, tc.amount), tc.override); got != tc.want {
				t.Fatalf("得到 %s，期望 %s", got, tc.want)
			}
		})
	}
}

// 凭证上的合计恒为正数：方向由类型与附注表达。
// 印一个负数总额会让任何一款报销系统都拒收。
func TestWalletReceiptTotalIsAlwaysPositive(t *testing.T) {
	for _, amount := range []string{"-88.00", "88.00"} {
		doc := assembleWalletReceiptDocument(walletTxn(walletdomain.TxnTypeVipPurchase, amount), nil, nil,
			paymentdomain.ReceiptOptions{}, walletEnv())
		if !doc.Total.Equal(decimal.RequireFromString("88.00")) {
			t.Fatalf("金额 %s 的凭证合计 = %s，期望 88.00", amount, doc.Total)
		}
		if len(doc.Items) != 1 || !doc.Items[0].Amount.Equal(doc.Total) {
			t.Fatalf("行项目金额应与合计一致，实得 %+v", doc.Items)
		}
	}
}

// 出账与入账的附注必须不同：一份写着「已计入余额」的凭证被当成扣款凭据，
// 或者反过来，都会直接引发客诉。
func TestWalletReceiptNoteFollowsDirection(t *testing.T) {
	debit := assembleWalletReceiptDocument(walletTxn(walletdomain.TxnTypeConsume, "-10.00"), nil, nil,
		paymentdomain.ReceiptOptions{}, walletEnv())
	credit := assembleWalletReceiptDocument(walletTxn(walletdomain.TxnTypeRecharge, "10.00"), nil, nil,
		paymentdomain.ReceiptOptions{}, walletEnv())
	if len(debit.Notes) != 1 || debit.Notes[0].Key != "notes.walletDebit" {
		t.Fatalf("出账附注 = %+v", debit.Notes)
	}
	if len(credit.Notes) != 1 || credit.Notes[0].Key != "notes.walletCredit" {
		t.Fatalf("入账附注 = %+v", credit.Notes)
	}
}

// 平台生成的标题走译文键，接入方填的消费说明原样保留。
//
// 翻译 consume 的标题等于把接入方的业务数据改掉；
// 不翻译其余类型的标题，则会让一份英文凭证的商品栏出现中文。
func TestWalletLineItemTranslatesOnlyPlatformTitles(t *testing.T) {
	consume := walletTxn(walletdomain.TxnTypeConsume, "-12.00")
	consume.Title = "购买 3 张地图券"
	item := buildWalletLineItem(consume, consume.Amount.Abs())
	if item.NameKey != "" {
		t.Errorf("接入方填写的消费说明不该走译文键，实得 %q", item.NameKey)
	}
	if item.Name != "购买 3 张地图券" {
		t.Errorf("消费说明被改写成了 %q", item.Name)
	}

	orderPay := walletTxn(walletdomain.TxnTypeOrderPay, "-50.00")
	orderPay.Remark = "年度会员"
	platform := buildWalletLineItem(orderPay, orderPay.Amount.Abs())
	if platform.NameKey != "wallet.type."+walletdomain.TxnTypeOrderPay {
		t.Errorf("平台生成的标题应走译文键，实得 %q", platform.NameKey)
	}
	if platform.Description != "年度会员" {
		t.Errorf("备注里的业务内容丢了：%q", platform.Description)
	}
}

// 每种流水类型都要有译名。少一个，凭证的商品栏与「流水类型」就会显示成键名。
func TestEveryWalletTypeHasLabel(t *testing.T) {
	types := []string{
		walletdomain.TxnTypeRecharge, walletdomain.TxnTypeConsume, walletdomain.TxnTypeRefund,
		walletdomain.TxnTypeAdminAdjust, walletdomain.TxnTypeVipPurchase, walletdomain.TxnTypeOrderPay,
	}
	bundle, err := receipt.Bundle()
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range bundle.Locales() {
		loc := bundle.For(info.Tag)
		for _, txnType := range types {
			if !loc.Has("wallet.type." + txnType) {
				t.Errorf("%s 缺少流水类型 %q 的译名", info.Tag, txnType)
			}
		}
		for _, key := range []string{
			"wallet.direction.in", "wallet.direction.out",
			"meta.walletTxnNo", "meta.walletType", "meta.direction",
			"meta.balanceBefore", "meta.balanceAfter", "meta.relatedOrder", "meta.operator",
			"notes.walletDebit", "notes.walletCredit",
		} {
			if !loc.Has(key) {
				t.Errorf("%s 缺少译文 %q", info.Tag, key)
			}
		}
	}
}

// 变动前后余额必须印在凭证上，且带币种代码。
// 只写「扣了 50」的凭证无法自证；不写币种的金额没法拿去报销。
func TestWalletReceiptMetadataCarriesBalanceTrail(t *testing.T) {
	txn := walletTxn(walletdomain.TxnTypeConsume, "-50.00")
	txn.RelatedOrderNo = ""
	txn.Operator = "admin@example.com"
	pairs := buildWalletReceiptMetadata(txn, "CNY")

	got := map[string]receipt.KeyValue{}
	for _, kv := range pairs {
		got[kv.Key] = kv
	}
	if v := got["meta.balanceBefore"].Value; v != "200.00 CNY" {
		t.Errorf("变动前余额 = %q，期望 \"200.00 CNY\"", v)
	}
	if v := got["meta.balanceAfter"].Value; v != "150.00 CNY" {
		t.Errorf("变动后余额 = %q，期望 \"150.00 CNY\"", v)
	}
	if v := got["meta.walletTxnNo"].Value; v != txn.TransactionNo {
		t.Errorf("流水号 = %q", v)
	}
	// 枚举值走译文键，不是字面值 —— 否则中文凭证上会出现 "consume"
	if key := got["meta.walletType"].ValueKey; key != "wallet.type.consume" {
		t.Errorf("流水类型应走译文键，实得 %q", key)
	}
	if key := got["meta.direction"].ValueKey; key != "wallet.direction.out" {
		t.Errorf("资金方向应走译文键，实得 %q", key)
	}
	if v := got["meta.operator"].Value; v != "admin@example.com" {
		t.Errorf("经办人 = %q", v)
	}
	if _, exists := got["meta.relatedOrder"]; exists {
		t.Error("没有关联订单时不该出现关联订单行")
	}
}

// 钱包凭证不占用「订单号」与「交易流水号」两个字段。
//
// 订单号留空是因为它真的没有订单；「交易流水号」在支付明细区表示的是
// **上游渠道**的单号，内部账本没有这个东西，填进去会让人拿着它去找渠道对账。
func TestWalletReceiptLeavesOrderFieldsEmpty(t *testing.T) {
	doc := assembleWalletReceiptDocument(walletTxn(walletdomain.TxnTypeConsume, "-10.00"), nil, nil,
		paymentdomain.ReceiptOptions{}, walletEnv())
	if doc.OrderNo != "" {
		t.Errorf("钱包凭证不该有订单号，实得 %q", doc.OrderNo)
	}
	if doc.Payment.ProviderOrderNo != "" {
		t.Errorf("内部账本没有上游单号，实得 %q", doc.Payment.ProviderOrderNo)
	}
	if doc.Payment.MethodKey != paymentdomain.MethodBalance {
		t.Errorf("支付方式 = %q，期望 balance", doc.Payment.MethodKey)
	}
}

// 凭证编号规则订单与钱包共用：同一主体反复导出得到同一个编号。
// 每次下载都换一个号会让对账无从下手。
func TestWalletReceiptNumberIsStableAndTyped(t *testing.T) {
	txn := walletTxn(walletdomain.TxnTypeRefund, "50.00")
	first := assembleWalletReceiptDocument(txn, nil, nil, paymentdomain.ReceiptOptions{}, walletEnv())
	second := assembleWalletReceiptDocument(txn, nil, nil, paymentdomain.ReceiptOptions{}, walletEnv())
	if first.Number != second.Number {
		t.Fatalf("同一流水两次导出编号不同：%q / %q", first.Number, second.Number)
	}
	if !strings.HasPrefix(first.Number, "CRN-") {
		t.Fatalf("退款凭证编号应以 CRN- 开头，实得 %q", first.Number)
	}
	if got := receiptSubjectNo(first); got != txn.TransactionNo {
		t.Fatalf("文件名主体单号 = %q，期望 %q", got, txn.TransactionNo)
	}
}

// 委派到订单出具时，文件名里的主体单号要跟着变成订单号 ——
// 两处下载到的本来就是同一份文件，名字必须一样。
func TestReceiptSubjectNoPrefersOrderNo(t *testing.T) {
	doc := receipt.Document{Number: "RCP-P1220260321100000123456", OrderNo: "P1220260321100000123456"}
	if got := receiptSubjectNo(doc); got != "P1220260321100000123456" {
		t.Fatalf("得到 %q", got)
	}
}

// 客户信息优先昵称，退回账号；两者都没有时不该在凭证上留下一个空的抬头。
func TestWalletReceiptCustomerFallsBackToAccount(t *testing.T) {
	txn := walletTxn(walletdomain.TxnTypeConsume, "-10.00")
	user := &userdomain.User{Account: "zhangsan", AppID: 12}

	withNickname := buildWalletReceiptCustomer(txn, user, &userdomain.Profile{Nickname: "张三", Email: "a@b.c"})
	if withNickname.Name != "张三" || withNickname.Email != "a@b.c" {
		t.Fatalf("带昵称时 = %+v", withNickname)
	}
	withoutNickname := buildWalletReceiptCustomer(txn, user, nil)
	if withoutNickname.Name != "zhangsan" {
		t.Fatalf("无昵称时应退回账号，实得 %q", withoutNickname.Name)
	}
	if withoutNickname.Reference != "UID-88" {
		t.Fatalf("客户号 = %q", withoutNickname.Reference)
	}
}

// 整份钱包凭证要能真的渲染出来（十种语言逐一），而不是只在结构上正确。
func TestWalletReceiptRendersInEveryLocale(t *testing.T) {
	s := newReceiptEmailService(t)
	txn := walletTxn(walletdomain.TxnTypeVipPurchase, "-88.00")
	txn.Title = "购买 VIP：Gold"
	txn.Metadata = map[string]any{"planName": "Gold", "durationDays": 365}
	doc := assembleWalletReceiptDocument(txn, &userdomain.User{Account: "zhangsan"}, nil,
		paymentdomain.ReceiptOptions{}, walletEnv())

	for _, info := range s.receipts.Locales() {
		result, err := s.receipts.Render(doc, receipt.Options{LocalePrefs: []string{info.Tag}})
		if err != nil {
			t.Fatalf("%s 渲染失败：%v", info.Tag, err)
		}
		if len(result.PDF) == 0 || result.Pages < 1 {
			t.Fatalf("%s 产出空凭证", info.Tag)
		}
	}
}

// 凭证入口挂着关联订单时要如实说明「由订单出具」，否则用户会疑惑
// 为什么下载下来的编号是订单号而不是流水号。
func TestWalletReceiptEntryDeclaresIssuer(t *testing.T) {
	s := newReceiptEmailService(t)
	info := orderReceiptContext{locale: "en", walletCurrency: "CNY", emailable: true}

	standalone := s.BuildWalletTransactionReceipt(walletTxn(walletdomain.TxnTypeConsume, "-10.00"), info)
	if !standalone.Available || standalone.Source != walletdomain.ReceiptSourceWallet {
		t.Fatalf("无关联订单时应由流水自行出具，实得 %+v", standalone)
	}
	if standalone.DocumentType != string(receipt.TypeReceipt) {
		t.Errorf("凭证类型 = %q", standalone.DocumentType)
	}
	if standalone.Currency != "CNY" {
		t.Errorf("币种 = %q", standalone.Currency)
	}

	linked := walletTxn(walletdomain.TxnTypeOrderPay, "-50.00")
	linked.RelatedOrderNo = "P1220260321100000123456"
	entry := s.BuildWalletTransactionReceipt(linked, info)
	if entry.Source != walletdomain.ReceiptSourceOrder || entry.OrderNo != linked.RelatedOrderNo {
		t.Fatalf("挂着订单时应声明由订单出具，实得 %+v", entry)
	}
	// 下载入口恒指向钱包这条路由：关联订单被清理时按钮也不能失效
	if !strings.Contains(entry.DownloadURL, linked.TransactionNo) {
		t.Errorf("下载入口应指向流水路由，实得 %q", entry.DownloadURL)
	}
}

// 未接入凭证渲染器时如实说不可用，而不是给出一个点了会 500 的按钮。
func TestWalletReceiptEntryUnavailableWithoutRenderer(t *testing.T) {
	s := &PaymentService{}
	entry := s.BuildWalletTransactionReceipt(walletTxn(walletdomain.TxnTypeConsume, "-10.00"), orderReceiptContext{})
	if entry.Available || entry.DownloadURL != "" {
		t.Fatalf("渲染器不可用时不该给出入口，实得 %+v", entry)
	}
}

// 0 元套餐不动钱包，没有流水也就没有凭证 —— 没发生的资金往来不该有凭据。
func TestVipReceiptEntryRequiresWalletTransaction(t *testing.T) {
	s := newReceiptEmailService(t)
	if entry := s.BuildVipPurchaseReceipt(t.Context(), nil, ""); entry != nil {
		t.Fatalf("无会话时不该给出凭证入口，实得 %+v", entry)
	}
}
