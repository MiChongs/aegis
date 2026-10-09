package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 永久会员的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「建永久套餐 → 余额购买 → 已包含拦截 → 叠加限时高级版 → 扣天数 → 取消永久 →
// 管理员发放 → 在线直购履约 → 退款冲正 → 老系统永久会员转换」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestVipLifetimeIntegration
func TestVipLifetimeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewVipService(zap.NewNop(), pg)
	pay := &PaymentService{log: zap.NewNop(), pg: pg}
	const appID = int64(20000)

	for _, tag := range []string{"export", "ai.chat"} {
		if _, err := svc.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}

	// ── 套餐 ──
	lifetimePlan := mustSavePlan(t, svc, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久基础版"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(99)), Features: &[]string{"export"}, BonusIntegral: ptrTo(int64(50)),
	})
	if !lifetimePlan.Lifetime || lifetimePlan.DurationDays != 0 {
		t.Fatalf("永久套餐应存为 lifetime、0 天，得到 %+v", lifetimePlan)
	}
	monthly := mustSavePlan(t, svc, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("基础版月卡"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(10)), Features: &[]string{"export"},
	})
	premium := mustSavePlan(t, svc, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("高级版月卡"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(20)), Features: &[]string{"ai.chat", "export"},
	})

	mustSavePlan(t, svc, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("7 天试用"), Kind: ptrTo(vipdomain.KindTrial), DurationDays: ptrTo(7),
	})

	// 永久与限时不能互相切换；试用不能是永久的
	if _, err := svc.AdminSavePlan(ctx, vipdomain.PlanMutation{ID: monthly.ID, AppID: appID, Lifetime: ptrTo(true)}); errCode(err) != errCodeTrialPlanKindInvalid {
		t.Fatalf("限时套餐改成永久应被拒，得到 %v", err)
	}
	if _, err := svc.AdminSavePlan(ctx, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久试用"), Kind: ptrTo(vipdomain.KindTrial), Lifetime: ptrTo(true),
	}); errCode(err) != errCodeTrialPlanKindInvalid {
		t.Fatalf("永久试用应被拒，得到 %v", err)
	}
	// 改名不动永久标记
	renamed := mustSavePlan(t, svc, vipdomain.PlanMutation{ID: lifetimePlan.ID, AppID: appID, Name: ptrTo("永久会员")})
	if !renamed.Lifetime || renamed.DurationDays != 0 {
		t.Fatalf("只改名不该丢掉永久标记，得到 %+v", renamed)
	}

	// ── 余额购买永久套餐 ──
	userA := insertTestUser(t, ctx, pool, appID, "lifetime-a", "500")
	sessionA := &authdomain.Session{AppID: appID, UserID: userA}
	bought, err := svc.PurchaseWithWallet(ctx, sessionA, lifetimePlan.ID, "buy-lifetime-1", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !bought.Status.IsVIP || !bought.Status.IsLifetime || bought.Status.ExpireAt != nil {
		t.Fatalf("购买结果应是永久会员且没有到期时间，得到 %+v", bought.Status)
	}
	if !bought.Transaction.Lifetime || bought.Transaction.ExpireAfter != nil || bought.Transaction.DurationDays != 0 {
		t.Fatalf("永久开通的账本记录不对：%+v", bought.Transaction)
	}
	// 幂等重放仍返回首次结果，不被「已包含」拦住
	replayed, err := svc.PurchaseWithWallet(ctx, sessionA, lifetimePlan.ID, "buy-lifetime-1", "127.0.0.1")
	if err != nil || !replayed.Replayed || !replayed.Status.IsLifetime {
		t.Fatalf("同一幂等键应重放首次结果，得到 %+v / %v", replayed, err)
	}

	ent := mustEntitlement(t, svc, appID, userA)
	if !ent.IsLifetime || ent.ExpireAt != nil || ent.PlanName != "永久会员" || ent.Source != vipdomain.SourceWallet {
		t.Fatalf("永久会员判定不对：%+v", ent)
	}
	if !slices.Equal(ent.Features, []string{"export"}) {
		t.Fatalf("永久基础版的功能应是 export，得到 %v", ent.Features)
	}
	if ent.TrialOffer.Reason != vipdomain.TrialReasonMemberActive {
		t.Fatalf("永久会员不能领试用，得到 %+v", ent.TrialOffer)
	}

	// 已包含：同一个永久套餐、同档月卡都拦；高级版月卡放行
	if _, err := svc.PurchaseWithWallet(ctx, sessionA, lifetimePlan.ID, "buy-lifetime-2", "127.0.0.1"); errCode(err) != errCodeVipLifetimeIncluded {
		t.Fatalf("重复购买永久套餐应以 40378 拒绝，得到 %v", err)
	}
	if _, err := svc.PurchaseWithWallet(ctx, sessionA, monthly.ID, "buy-monthly", "127.0.0.1"); errCode(err) != errCodeVipLifetimeIncluded {
		t.Fatalf("永久会员续同档月卡应以 40378 拒绝，得到 %v", err)
	}
	plans, err := svc.ListActivePlans(ctx, sessionA)
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range plans {
		wantIncluded := plan.ID != premium.ID
		if plan.Included != wantIncluded {
			t.Fatalf("套餐 %s 的 included 应为 %v", plan.Name, wantIncluded)
		}
	}
	if _, err := svc.PurchaseWithWallet(ctx, sessionA, premium.ID, "buy-premium", "127.0.0.1"); err != nil {
		t.Fatalf("永久基础版买限时高级版应放行：%v", err)
	}
	ent = mustEntitlement(t, svc, appID, userA)
	if !ent.IsLifetime || ent.PlanName != "永久会员" || ent.TimedExpireAt == nil ||
		!slices.Equal(ent.Features, []string{"ai.chat", "export"}) {
		t.Fatalf("叠加高级版后应仍是永久会员、给出限时到期时间、功能取并集，得到 %+v", ent)
	}

	// 扣天数只扣限时那条线
	if _, err := svc.AdminRevokeVip(ctx, AdminVipRevokeInput{UserID: userA, AppID: appID, Days: 3650, Operator: "test"}); err != nil {
		t.Fatal(err)
	}
	ent = mustEntitlement(t, svc, appID, userA)
	if !ent.IsLifetime || ent.TimedExpireAt != nil || !slices.Equal(ent.Features, []string{"export"}) {
		t.Fatalf("扣光限时天数后应只剩永久基础版，得到 %+v", ent)
	}
	if _, err := svc.AdminRevokeVip(ctx, AdminVipRevokeInput{UserID: userA, AppID: appID, Days: 1, Operator: "test"}); errCode(err) != errCodeVipNotActive {
		t.Fatalf("没有限时时长时扣天数应以 40377 拒绝，得到 %v", err)
	}

	// 取消永久会员
	revokeTxn, err := svc.AdminRevokeVipLifetime(ctx, AdminVipRevokeLifetimeInput{UserID: userA, AppID: appID, Reason: "测试取消", Operator: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !revokeTxn.Lifetime || revokeTxn.PayChannel != vipdomain.ChannelAdminRevoke {
		t.Fatalf("取消永久应留一条 admin_revoke 记录，得到 %+v", revokeTxn)
	}
	ent = mustEntitlement(t, svc, appID, userA)
	if ent.IsVIP || ent.IsLifetime || len(ent.Features) != 0 {
		t.Fatalf("取消永久后不该再是会员，得到 %+v", ent)
	}
	if _, err := svc.AdminRevokeVipLifetime(ctx, AdminVipRevokeLifetimeInput{UserID: userA, AppID: appID}); errCode(err) != errCodeVipLifetimeNotActive {
		t.Fatalf("不是永久会员时取消应以 40379 拒绝，得到 %v", err)
	}
	// 取消之后可以再买（套餐不再被视为已包含）
	if _, err := svc.PurchaseWithWallet(ctx, sessionA, lifetimePlan.ID, "buy-lifetime-3", "127.0.0.1"); err != nil {
		t.Fatalf("取消永久后应能重新购买：%v", err)
	}

	// ── 管理员发放 ──
	userB := insertTestUser(t, ctx, pool, appID, "lifetime-b", "0")
	if _, err := svc.AdminGrantVip(ctx, AdminVipGrantInput{UserID: userB, AppID: appID, PlanID: lifetimePlan.ID, Quantity: 2}); err == nil {
		t.Fatal("永久套餐发放 2 份应被拒")
	}
	granted, err := svc.AdminGrantVip(ctx, AdminVipGrantInput{UserID: userB, AppID: appID, Lifetime: true, Features: []string{"ai.chat"}, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !granted.Lifetime || granted.PlanName != "管理员授予永久会员" {
		t.Fatalf("自定义永久发放的记录不对：%+v", granted)
	}
	ent = mustEntitlement(t, svc, appID, userB)
	if !ent.IsLifetime || ent.Source != vipdomain.SourceAdminGrant || !slices.Equal(ent.Features, []string{"ai.chat"}) {
		t.Fatalf("管理员发放的永久会员判定不对：%+v", ent)
	}
	if _, err := svc.ClaimTrial(ctx, &authdomain.Session{AppID: appID, UserID: userB}, ""); err == nil {
		t.Fatal("永久会员不该领到试用")
	}

	// ── 在线直购：下单快照、履约、退款冲正 ──
	userC := insertTestUser(t, ctx, pool, appID, "lifetime-c", "0")
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lifetime-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	var configID int64
	if err := pool.QueryRow(ctx, `INSERT INTO payment_configs (appid, payment_method, config_name) VALUES ($1, 'balance', '余额') RETURNING id`, appID).Scan(&configID); err != nil {
		t.Fatal(err)
	}

	// 月卡订单里塞 vipLifetime: true 不能开出永久会员
	forged, err := pay.prepareFulfillmentMetadata(ctx, appID, userC, monthly.Price, map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeVipPurchase, paymentdomain.MetaKeyVipPlanID: float64(monthly.ID),
		paymentdomain.MetaKeyVipLifetime: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := forged[paymentdomain.MetaKeyVipLifetime]; ok {
		t.Fatal("限时套餐订单的快照里不该留下客户端传来的 vipLifetime")
	}

	meta, err := pay.prepareFulfillmentMetadata(ctx, appID, userC, lifetimePlan.Price, map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeVipPurchase, paymentdomain.MetaKeyVipPlanID: float64(lifetimePlan.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	order := insertPaidOrder(t, ctx, pool, pg, appID, configID, userC, "ORD-LIFETIME-C", lifetimePlan.Price, meta)
	instr, has, err := pay.buildFulfillmentInstruction(order)
	if err != nil || !has || !instr.VipLifetime || instr.VipDays != 0 {
		t.Fatalf("永久套餐订单的履约指令不对：%+v / %v / %v", instr, has, err)
	}
	if done, err := pg.FulfillPaymentOrder(ctx, order, instr); err != nil || !done {
		t.Fatalf("履约失败：%v / %v", done, err)
	}
	ent = mustEntitlement(t, svc, appID, userC)
	if !ent.IsLifetime || ent.Source != vipdomain.SourcePaymentOrder {
		t.Fatalf("直购履约后应是永久会员，得到 %+v", ent)
	}
	// 已是永久会员时再下同一个套餐的单，拦在付钱之前
	if _, err := pay.prepareFulfillmentMetadata(ctx, appID, userC, lifetimePlan.Price, map[string]any{
		paymentdomain.MetaKeyPurpose: paymentdomain.PurposeVipPurchase, paymentdomain.MetaKeyVipPlanID: float64(lifetimePlan.ID),
	}); errCode(err) != errCodeVipLifetimeIncluded {
		t.Fatalf("已包含的套餐下单应以 40378 拒绝，得到 %v", err)
	}

	order, _ = pg.GetPaymentOrderByOrderNo(ctx, order.OrderNo)
	refund, err := pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{
		AppID: appID, Order: order, RefundNo: "RF-LIFETIME-C", Amount: order.Amount, Reason: "测试退款",
	})
	if err != nil {
		t.Fatal(err)
	}
	settled, _, err := pg.RefundPaymentOrderToWallet(ctx, refund.ID, order, order.Amount, refund.RefundNo, &instr, true)
	if err != nil {
		t.Fatal(err)
	}
	if settled.ReversalStatus != paymentdomain.ReversalDone {
		t.Fatalf("永久套餐全额退款应冲正成功，得到 %s：%s", settled.ReversalStatus, settled.ReversalMessage)
	}
	ent = mustEntitlement(t, svc, appID, userC)
	if ent.IsVIP || ent.IsLifetime {
		t.Fatalf("退款冲正后不该再是永久会员，得到 %+v", ent)
	}

	// ── 老系统的永久会员 ──
	userD := insertTestUser(t, ctx, pool, appID, "legacy-d", "0")
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_expire_at = $1 WHERE id = $2`, vipdomain.LegacyLifetimeExpireAt, userD); err != nil {
		t.Fatal(err)
	}
	// 迁移文件本身那条转换语句
	if _, err := pool.Exec(ctx, mustRead(t, "../../migrations/postgres/000091_vip_lifetime.up.sql")); err != nil {
		t.Fatal(err)
	}
	ent = mustEntitlement(t, svc, appID, userD)
	if !ent.IsLifetime || ent.ExpireAt != nil || ent.Source != vipdomain.SourceUnknown {
		t.Fatalf("老系统永久会员应转成永久会员（来源不明），得到 %+v", ent)
	}
	// 重新导入又写回 2099：仓储那份转换再收敛一次，且不重复补记
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_expire_at = $1 WHERE id = $2`, vipdomain.LegacyLifetimeExpireAt, userD); err != nil {
		t.Fatal(err)
	}
	if converted, err := pg.ConvertLegacyLifetimeVip(ctx); err != nil || converted != 1 {
		t.Fatalf("应收敛 1 个用户，得到 %d / %v", converted, err)
	}
	var ledgers int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM vip_transactions WHERE user_id = $1 AND lifetime`, userD).Scan(&ledgers)
	if ledgers != 1 {
		t.Fatalf("老系统永久会员只应补记一笔，得到 %d", ledgers)
	}
	ent = mustEntitlement(t, svc, appID, userD)
	if !ent.IsLifetime || ent.ExpireAt != nil {
		t.Fatalf("收敛后仍应是永久会员且没有到期时间，得到 %+v", ent)
	}
}

func mustSavePlan(t *testing.T, svc *VipService, mutation vipdomain.PlanMutation) *vipdomain.Plan {
	t.Helper()
	plan, err := svc.AdminSavePlan(context.Background(), mutation)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustEntitlement(t *testing.T, svc *VipService, appID int64, userID int64) *vipdomain.Entitlement {
	t.Helper()
	ent, err := svc.ResolveEntitlement(context.Background(), appID, userID, "")
	if err != nil {
		t.Fatal(err)
	}
	return ent
}

func insertTestUser(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appID int64, account string, balance string) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled) VALUES ($1, $2, TRUE) RETURNING id`,
		appID, account).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_wallets (user_id, appid, balance) VALUES ($1, $2, $3)`,
		id, appID, balance); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertPaidOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pg *pgrepo.Repository, appID int64, configID int64,
	userID int64, orderNo string, amount decimal.Decimal, metadata map[string]any) *paymentdomain.Order {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, amount, payment_method,
provider_type, status, metadata, paid_at) VALUES ($1, $2, $3, $4, '永久会员', $5, 'balance', 'balance', 'paid', $6, NOW())`,
		appID, userID, configID, orderNo, amount.StringFixed(2), metadata); err != nil {
		t.Fatal(err)
	}
	order, err := pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil || order == nil {
		t.Fatalf("读回订单失败：%v", err)
	}
	return order
}

func errCode(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return 0
}

func ptrTo[T any](v T) *T { return &v }
