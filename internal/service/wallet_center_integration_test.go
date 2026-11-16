package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	walletdomain "aegis/internal/domain/wallet"
	pgrepo "aegis/internal/repository/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// TestWalletCenterIntegration 完整钱包：
// 「档位下单校验 → 档位充值履约（本金 + 赠送余额 + 赠送积分）→ 兑换只认本金（下限、上限、每日、幂等、全部）→
// 消费先扣赠送 → 余额支付订单退款按原拆分退回 → 充值退款扣回本金并回收赠送 → 限购 → 管理员按桶调账」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres:pw@127.0.0.1:55491/aegis_wallet_test go test ./internal/service -run TestWalletCenter
func TestWalletCenterIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	wallets := NewWalletService(zap.NewNop(), pg)
	payments := NewPaymentService(zap.NewNop(), pg, config.PaymentReceiptConfig{})
	payments.SetWalletService(wallets)
	const appID = int64(31000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'wallet-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	var configID int64
	if err := pool.QueryRow(ctx, `INSERT INTO payment_configs (appid, payment_method, config_name) VALUES ($1, 'epay', '易支付') RETURNING id`, appID).Scan(&configID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "wallet-alice", "0")
	session := &authdomain.Session{UserID: alice, AppID: appID}
	money := decimal.RequireFromString

	wallet := func() walletdomain.Wallet {
		t.Helper()
		w, err := wallets.GetMyWallet(ctx, session)
		if err != nil {
			t.Fatal(err)
		}
		return *w
	}
	expectWallet := func(step, cash, bonus string) {
		t.Helper()
		w := wallet()
		if !w.CashBalance.Equal(money(cash)) || !w.BonusBalance.Equal(money(bonus)) {
			t.Fatalf("%s：期望本金 %s 赠送 %s，实际本金 %s 赠送 %s", step, cash, bonus, w.CashBalance, w.BonusBalance)
		}
	}
	integral := func() int64 {
		t.Helper()
		n, err := pg.GetUserIntegralBalance(ctx, alice, appID)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	expectCode := func(step string, err error, code int) {
		t.Helper()
		if errCode(err) != code {
			t.Fatalf("%s：期望错误码 %d，得到 %v", step, code, err)
		}
	}

	// ── 档位 ──
	pkg, err := wallets.AdminSavePackage(ctx, appID, walletdomain.RechargePackage{
		Title: "充 100 送 20", Amount: money("100"), BonusAmount: money("20"), BonusIntegral: 500,
		Badge: "首充", PerUserLimit: 1, IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wallets.AdminSavePackage(ctx, appID, walletdomain.RechargePackage{Title: "坏档位", Amount: money("0")}); errCode(err) != 40000 {
		t.Fatalf("0 元档位应被拒，得到 %v", err)
	}

	// ── 下单校验 ──
	_, err = payments.prepareFulfillmentMetadata(ctx, appID, alice, money("99"), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeWalletRecharge, paymentdomain.MetaKeyWalletPackageID: float64(pkg.ID)})
	expectCode("价格不一致", err, errCodeWalletPackagePrice)
	_, err = payments.prepareFulfillmentMetadata(ctx, appID, alice, money("6000"), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeWalletRecharge})
	expectCode("自定义金额超上限", err, errCodeWalletRechargeRange)
	custom, err := payments.prepareFulfillmentMetadata(ctx, appID, alice, money("10"), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeWalletRecharge, paymentdomain.MetaKeyWalletBonus: "999"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := custom[paymentdomain.MetaKeyWalletBonus]; ok {
		t.Fatal("客户端塞进来的 walletBonus 必须被删掉")
	}
	meta, err := payments.prepareFulfillmentMetadata(ctx, appID, alice, money("100"), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeWalletRecharge, paymentdomain.MetaKeyWalletPackageID: float64(pkg.ID),
		paymentdomain.MetaKeyWalletBonusIntegral: float64(1_000_000)})
	if err != nil {
		t.Fatal(err)
	}
	if meta[paymentdomain.MetaKeyWalletBonus] != "20.00" || meta[paymentdomain.MetaKeyWalletBonusIntegral] != int64(500) {
		t.Fatalf("档位快照不对：%v", meta)
	}

	// ── 档位充值履约 ──
	rechargeOrder := insertRechargeOrder(t, ctx, pool, pg, appID, configID, alice, "ORD-W-1", money("100"), meta)
	instr, has, err := payments.buildFulfillmentInstruction(rechargeOrder)
	if err != nil || !has {
		t.Fatalf("履约指令：%v", err)
	}
	if ok, err := pg.FulfillPaymentOrder(ctx, rechargeOrder, instr); err != nil || !ok {
		t.Fatalf("履约失败：%v", err)
	}
	expectWallet("档位充值后", "100", "20")
	if got := integral(); got != 500 {
		t.Fatalf("赠送积分应为 500，得到 %d", got)
	}
	if ok, _ := pg.FulfillPaymentOrder(ctx, rechargeOrder, instr); ok {
		t.Fatal("重复履约必须被拦下")
	}
	expectWallet("重复履约后", "100", "20")

	// 限购 1：付过一次之后再下单被拒
	_, err = payments.prepareFulfillmentMetadata(ctx, appID, alice, money("100"), map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeWalletRecharge, paymentdomain.MetaKeyWalletPackageID: float64(pkg.ID)})
	expectCode("限购", err, errCodeWalletPackageLimit)
	overview, err := wallets.Overview(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Recharge.Packages) != 1 || overview.Recharge.Packages[0].Unavailable != walletdomain.PackageLimitReached {
		t.Fatalf("总览里档位应标为已达限购：%+v", overview.Recharge.Packages)
	}
	if overview.Exchange.Rate != 100 || !overview.Exchange.Exchangeable.Equal(money("100")) {
		t.Fatalf("兑换额度：%+v", overview.Exchange)
	}

	// ── 兑换 ──
	exchange := func(amount string, all bool, key string) (*walletdomain.ExchangeResult, error) {
		return wallets.Exchange(ctx, session, WalletExchangeRequest{Amount: amount, All: all, IdempotencyKey: key})
	}
	res, err := exchange("30", false, "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Integral != 3000 || res.IntegralBalance != 3500 || !res.Transaction.BonusAmount.IsZero() {
		t.Fatalf("兑换结果不对：%+v", res)
	}
	expectWallet("兑换 30 后", "70", "20")
	again, err := exchange("30", false, "ex-1")
	if err != nil || !again.Replayed || again.Transaction.TransactionNo != res.Transaction.TransactionNo {
		t.Fatalf("同一幂等键应重放首次结果：%+v %v", again, err)
	}
	expectWallet("重放后", "70", "20")
	_, err = exchange("80", false, "ex-2")
	expectCode("总余额够但本金不够", err, errCodeWalletCashInsufficient)
	_, err = exchange("0.5", false, "ex-3")
	expectCode("低于单笔下限", err, errCodeWalletExchangeRange)

	settings := walletdomain.DefaultSettings()
	settings.ExchangeDailyMax = money("50")
	if _, err := wallets.AdminUpdateSettings(ctx, appID, settings); err == nil {
		t.Fatal("未注入应用服务时保存设置应报错")
	}
	wallets.SetAppService(NewAppService(zap.NewNop(), pg, nil))
	if _, err := wallets.AdminUpdateSettings(ctx, appID, settings); err != nil {
		t.Fatal(err)
	}
	_, err = exchange("25", false, "ex-4")
	expectCode("超出每日上限（已兑 30）", err, errCodeWalletExchangeDaily)
	res, err = exchange("", true, "ex-5")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Transaction.Amount.Equal(money("-20")) {
		t.Fatalf("全部兑换应受每日剩余 20 限制，实扣 %s", res.Transaction.Amount)
	}
	expectWallet("全部兑换后", "50", "20")
	_, err = exchange("", true, "ex-6")
	expectCode("今日额度用完后全部兑换", err, errCodeWalletExchangeDaily)
	settings.ExchangeEnabled = false
	settings.ExchangeDailyMax = decimal.Zero
	if _, err := wallets.AdminUpdateSettings(ctx, appID, settings); err != nil {
		t.Fatal(err)
	}
	_, err = exchange("1", false, "ex-7")
	expectCode("兑换关闭", err, errCodeWalletExchangeClosed)
	settings.ExchangeEnabled = true
	if _, err := wallets.AdminUpdateSettings(ctx, appID, settings); err != nil {
		t.Fatal(err)
	}

	// ── 消费先扣赠送 ──
	if _, err := wallets.Consume(ctx, session, money("25"), "测试消费", "", "c-1", ""); err != nil {
		t.Fatal(err)
	}
	expectWallet("消费 25 后", "45", "0")

	// ── 管理员按桶调账 ──
	if _, err := wallets.AdminAdjust(ctx, alice, appID, money("10"), "", "补偿", "tester", ""); err != nil {
		t.Fatal(err)
	}
	expectWallet("默认调账入账", "45", "10")
	if _, err := wallets.AdminAdjust(ctx, alice, appID, money("-11"), walletdomain.BucketBonus, "扣赠送", "tester", ""); errCode(err) != errCodeWalletBonusInsuffic {
		t.Fatalf("只扣赠送余额不足应报 40149，得到 %v", err)
	}
	if _, err := wallets.AdminAdjust(ctx, alice, appID, money("5"), walletdomain.BucketCash, "线下收款", "tester", ""); err != nil {
		t.Fatal(err)
	}
	expectWallet("按本金调账入账", "50", "10")

	// ── 余额支付订单退款：按原拆分退回 ──
	orderPay := insertRechargeOrder(t, ctx, pool, pg, appID, configID, alice, "ORD-W-PAY", money("15"), map[string]any{})
	if _, err := pool.Exec(ctx, `UPDATE payment_orders SET status = 'pending', paid_at = NULL, payment_method = 'balance' WHERE id = $1`, orderPay.ID); err != nil {
		t.Fatal(err)
	}
	orderPay, _ = pg.GetPaymentOrderByOrderNo(ctx, "ORD-W-PAY")
	payTxn, err := pg.PayPaymentOrderWithWallet(ctx, orderPay, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !payTxn.BonusAmount.Equal(money("-10")) {
		t.Fatalf("余额支付 15 应先扣赠送 10，实际赠送部分 %s", payTxn.BonusAmount)
	}
	expectWallet("余额支付后", "45", "0")
	orderPay, _ = pg.GetPaymentOrderByOrderNo(ctx, "ORD-W-PAY")
	refund, err := pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{AppID: appID, Order: orderPay, RefundNo: "RF-W-PAY-1",
		Amount: money("12"), Reason: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pg.RefundPaymentOrderToWallet(ctx, refund.ID, orderPay, money("12"), refund.RefundNo, nil, false); err != nil {
		t.Fatal(err)
	}
	expectWallet("部分退款 12（先补赠送 10）", "47", "10")
	refund, err = pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{AppID: appID, Order: orderPay, RefundNo: "RF-W-PAY-2",
		Amount: money("3"), Reason: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := pg.RefundPaymentOrderToWallet(ctx, refund.ID, orderPay, money("3"), refund.RefundNo, nil, false); err != nil {
		t.Fatal(err)
	}
	expectWallet("再退 3（赠送已补满，全进本金）", "50", "10")

	// ── 充值订单渠道退款：扣回本金、回收赠送余额与积分 ──
	rechargeOrder, _ = pg.GetPaymentOrderByOrderNo(ctx, "ORD-W-1")
	if _, err := wallets.AdminAdjust(ctx, alice, appID, money("60"), walletdomain.BucketCash, "补足", "tester", ""); err != nil {
		t.Fatal(err)
	}
	expectWallet("补足后", "110", "10")
	refund, err = pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{AppID: appID, Order: rechargeOrder, RefundNo: "RF-W-2",
		Amount: money("100"), Reason: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	before := integral()
	settled, err := pg.SettlePaymentRefund(ctx, paymentdomain.RefundSettlement{RefundID: refund.ID, Status: paymentdomain.RefundSuccess},
		rechargeOrder, &instr, true)
	if err != nil {
		t.Fatal(err)
	}
	if settled.ReversalStatus != paymentdomain.ReversalDone {
		t.Fatalf("冲正应成功，得到 %s：%s", settled.ReversalStatus, settled.ReversalMessage)
	}
	// 扣回 100 本金（先扣本金：110→10），再回收赠送 20：赠送只有 10，先扣赠送 10、再扣本金 10
	expectWallet("充值全额退款后", "0", "0")
	if got := integral(); got != before-500 {
		t.Fatalf("赠送积分应回收 500：%d → %d", before, got)
	}

	// ── 流水筛选与拆分 ──
	list, err := wallets.ListMyTransactions(ctx, session, walletdomain.ListQuery{Type: "recharge_bonus,integral_exchange", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list.Items {
		if item.Type != walletdomain.TxnTypeRechargeBonus && item.Type != walletdomain.TxnTypeIntegralExchange {
			t.Fatalf("多类型筛选混进了 %s", item.Type)
		}
		if !item.CashAmount.Add(item.BonusAmount).Equal(item.Amount) {
			t.Fatalf("流水拆分之和不等于金额：%+v", item)
		}
	}
	w := wallet()
	if !w.TotalExchanged.Equal(money("50")) || !w.TotalBonus.Equal(money("30")) {
		t.Fatalf("累计兑换应为 50、累计获赠 30（档位 20 + 调账 10），得到 %s / %s", w.TotalExchanged, w.TotalBonus)
	}
}

// TestWalletBucketBackfill 迁移第一次加列时按历史流水重放回填赠送余额。
func TestWalletBucketBackfill(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(31001)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'wallet-backfill') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/postgres/000154_wallet_buckets_and_packages.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatal(err)
	}
	user := insertTestUser(t, ctx, pool, appID, "backfill", "0")
	// 充值 50（本金）→ 积分换余额 30（赠送）→ 消费 40（先扣赠送 30、再扣本金 10）→ 卡密 5（赠送）
	steps := []struct {
		typ    string
		amount string
	}{{"recharge", "50"}, {"points_exchange", "30"}, {"consume", "-40"}, {"card_key", "5"}}
	balance := decimal.Zero
	for i, s := range steps {
		before := balance
		balance = balance.Add(decimal.RequireFromString(s.amount))
		if _, err := pool.Exec(ctx, `INSERT INTO wallet_transactions (transaction_no, user_id, appid, type, amount, balance_before, balance_after, title)
VALUES ($1, $2, $3, $4, $5, $6, $7, 't')`, "BF"+string(rune('A'+i)), user, appID, s.typ, s.amount, before.String(), balance.String()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE user_wallets SET balance = $2 WHERE user_id = $1`, user, balance.String()); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/postgres/000154_wallet_buckets_and_packages.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if _, err := pool.Exec(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
	}
	var bonus, consumeBonus string
	if err := pool.QueryRow(ctx, `SELECT bonus_balance::text FROM user_wallets WHERE user_id = $1`, user).Scan(&bonus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT bonus_amount::text FROM wallet_transactions WHERE transaction_no = 'BFC'`).Scan(&consumeBonus); err != nil {
		t.Fatal(err)
	}
	if bonus != "5.00" || consumeBonus != "-30.00" {
		t.Fatalf("回填：赠送余额应为 5.00、消费的赠送部分 -30.00，得到 %s / %s", bonus, consumeBonus)
	}
}

func insertRechargeOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pg *pgrepo.Repository, appID, configID, userID int64,
	orderNo string, amount decimal.Decimal, metadata map[string]any) *paymentdomain.Order {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, amount, payment_method,
provider_type, status, metadata, paid_at, created_at) VALUES ($1, $2, $3, $4, '余额充值', $5, 'epay', 'alipay', 'paid', $6, NOW(), $7)`,
		appID, userID, configID, orderNo, amount.StringFixed(2), metadata, time.Now()); err != nil {
		t.Fatal(err)
	}
	order, err := pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil || order == nil {
		t.Fatalf("读回订单失败：%v", err)
	}
	if !strings.HasPrefix(order.OrderNo, "ORD-") {
		t.Fatal("unexpected order")
	}
	return order
}
