package service

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/internal/domain/accountdeletion"
	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 账号封禁、全站封禁与申诉的端到端集成测试：真实 Postgres（全部迁移）。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestSuspensionIntegration
func TestSuspensionIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newDeletionTestEnv(t, ctx, pool)
	app := env.appID
	const app2 = int64(31201)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'second-app') ON CONFLICT DO NOTHING`, app2); err != nil {
		t.Fatal(err)
	}
	log := zap.NewNop()
	bans, err := NewAccountBanService(config.AccountBanConfig{}, log, env.pg, env.sessions)
	if err != nil {
		t.Fatal(err)
	}
	sus := NewSuspensionService(log, env.pg, "suspension-test-key")
	sus.SetDependencies(bans, nil, nil, nil, nil)
	InstallSuspensionHooks(sus)
	t.Cleanup(func() { InstallSuspensionHooks(nil) })

	alice := env.recovery.user(t, "alice", "Pw-123456", "Alice@Example.com", "")
	bob := env.recovery.user(t, "bob", "Pw-123456", "bob@example.com", "")
	var alice2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, 'alice-other', 'x', TRUE) RETURNING id`, app2).Scan(&alice2); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, email) VALUES ($1, 'alice@example.com')`, alice2); err != nil {
		t.Fatal(err)
	}
	loadUser := func(id int64) *userdomain.User {
		u, err := env.pg.GetUserByID(ctx, id)
		if err != nil || u == nil {
			t.Fatalf("读不到用户 %d：%v", id, err)
		}
		return u
	}
	en := WithRequestLocale(ctx, "en-US,en;q=0.9")
	op := userdomain.BanOperator{AdminID: 1, AdminName: "root"}

	// ── 应用级封禁：被拒时带封禁详情与申诉凭证，提示按语言 ──
	ban, err := bans.BanUser(ctx, app, alice, userdomain.AccountBanCreateInput{Reason: "刷单", ReasonCode: "fraud", PublicReason: "Abnormal reward activity", Operator: op})
	if err != nil {
		t.Fatal(err)
	}
	if ban.Level != userdomain.AccountBanLevelApp || ban.ReasonCode != "fraud" || !ban.Appealable || ban.Source != userdomain.AccountBanSourceManual {
		t.Fatalf("封禁字段不对：%+v", ban)
	}
	err = enforceAccountBan(en, env.pg, log, loadUser(alice), userdomain.AccountBanCheckpointSignIn)
	appErr, ok := err.(*apperrors.AppError)
	if !ok || appErr.Code != 40301 || appErr.Message != "Your account has been suspended." {
		t.Fatalf("被拒错误不对：%v", err)
	}
	view := appErr.Data.(map[string]any)["suspension"].(suspension.View)
	if view.ID != ban.ID || view.Level != "app" || view.ReasonTitle != "Fraud or cheating" || view.Reason != "Abnormal reward activity" || view.Appeal.Token == "" || !view.Appeal.Allowed || view.Appeal.Remaining != 2 {
		t.Fatalf("封禁详情不对：%+v", view)
	}
	token := view.Appeal.Token

	// ── 申诉：凭证查看、字数、重复、撤回、再交 ──
	cred := SuspensionCredential{AppID: app, Token: token}
	st, err := sus.Status(ctx, cred)
	if err != nil || st.Suspension == nil || st.Suspension.ID != ban.ID {
		t.Fatalf("凭证查看封禁失败：%+v %v", st, err)
	}
	if _, err := sus.Status(ctx, SuspensionCredential{AppID: app2, Token: token}); errCode(err) != 40150 {
		t.Fatalf("别的应用拿这个凭证应被拒，得到 %v", err)
	}
	if _, err := sus.Status(ctx, SuspensionCredential{AppID: app, Token: token[:len(token)-2] + "xx"}); errCode(err) != 40150 {
		t.Fatalf("篡改的凭证应被拒，得到 %v", err)
	}
	if _, err := sus.SubmitAppeal(ctx, AppealSubmitInput{Credential: cred, Content: "太短"}); errCode(err) != 42220 {
		t.Fatalf("太短应被拒，得到 %v", err)
	}
	if _, err := sus.SubmitAppeal(ctx, AppealSubmitInput{Credential: cred, Content: "我一直正常使用，没有刷单，请复核。", ContactEmail: "bad"}); errCode(err) != 42221 {
		t.Fatalf("邮箱不对应被拒，得到 %v", err)
	}
	first, err := sus.SubmitAppeal(en, AppealSubmitInput{Credential: cred, Content: "我一直正常使用，没有刷单，请复核。", Source: "web"})
	if err != nil || first.Appeal.Status != suspension.AppealPending || first.Suspension.Appeal.BlockedReason != suspension.BlockedOpen {
		t.Fatalf("提交申诉不对：%+v %v", first, err)
	}
	if _, err := sus.SubmitAppeal(ctx, AppealSubmitInput{Credential: cred, Content: "再交一次试试，还是同样的理由。"}); errCode(err) != 40933 {
		t.Fatalf("已有进行中的申诉应被拒，得到 %v", err)
	}
	if _, err := sus.WithdrawAppeal(ctx, cred, first.Appeal.ID); err != nil {
		t.Fatalf("撤回失败：%v", err)
	}
	second, err := sus.SubmitAppeal(ctx, AppealSubmitInput{Credential: cred, Content: "补充说明：那几天是家里人用我的手机。"})
	if err != nil || second.Suspension.Appeal.Remaining != 1 {
		t.Fatalf("撤回后应能再交，且撤回的不算次数：%+v %v", second, err)
	}
	var locale string
	_ = pool.QueryRow(ctx, `SELECT locale FROM ban_appeals WHERE id = $1`, first.Appeal.ID).Scan(&locale)
	if locale != suspension.LocaleEN {
		t.Fatalf("申诉应记下用户语言 en，得到 %q", locale)
	}

	// ── 模型复审：没有 AI 通道与风控时记下原因，转人工，状态进入复审中 ──
	if _, err := sus.SaveSettings(ctx, app, suspension.Settings{Enabled: true, AutoReview: suspension.AutoReviewAuto, MaxAppealsPerBan: 2}, "root"); err != nil {
		t.Fatal(err)
	}
	reviewed, err := sus.RerunReview(ctx, app, second.Appeal.ID)
	if err != nil || reviewed.Status != suspension.AppealReviewing || reviewed.ModelError == "" || reviewed.RiskLevel != suspension.RiskMedium || reviewed.ModelReviewedAt == nil {
		t.Fatalf("没有 AI 通道时应转人工：%+v %v", reviewed, err)
	}
	list, total, err := sus.ListAppeals(ctx, suspension.AppealQuery{AppID: app, Status: "open"})
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != second.Appeal.ID {
		t.Fatalf("进行中且模型未给结论的队列不对：%d %+v %v", total, list, err)
	}

	// ── 人工通过：解封，申诉结案 ──
	if _, err := sus.DecideAppealByAdmin(ctx, app, second.Appeal.ID, true, op, "核实为家人使用", "We've reinstated your account."); err != nil {
		t.Fatal(err)
	}
	if active, _ := bans.GetActiveBan(ctx, app, alice); active != nil {
		t.Fatalf("申诉通过后应已解封：%+v", active)
	}
	if _, err := sus.DecideAppealByAdmin(ctx, app, second.Appeal.ID, false, op, "", ""); errCode(err) != 40957 {
		t.Fatalf("已结案的不能再结，得到 %v", err)
	}
	stats, _ := sus.AppealStats(ctx, app)
	if stats.Approved != 1 || stats.Withdrawn != 1 || stats.Total != 2 {
		t.Fatalf("统计不对：%+v", stats)
	}

	// ── 全站封禁：同一邮箱在另一个应用的账号一起封；同邮箱注册被拒；之后登录的同邮箱新账号被连带 ──
	pbBan, err := bans.BanUser(ctx, app, alice, userdomain.AccountBanCreateInput{Reason: "批量注册", ReasonCode: "spam", Level: "platform", Operator: op})
	if err != nil {
		t.Fatal(err)
	}
	if pbBan.Level != userdomain.AccountBanLevelPlatform || pbBan.PlatformBanID == nil {
		t.Fatalf("全站封禁应落在当前账号上：%+v", pbBan)
	}
	linked, err := bans.GetActiveBan(ctx, app2, alice2)
	if err != nil || linked == nil || linked.PlatformBanID == nil || *linked.PlatformBanID != *pbBan.PlatformBanID || linked.Source != userdomain.AccountBanSourcePlatform {
		t.Fatalf("另一个应用里同邮箱的账号应被连带：%+v %v", linked, err)
	}
	if active, _ := bans.GetActiveBan(ctx, app, bob); active != nil {
		t.Fatalf("不相干的账号不应被封")
	}
	if err := sus.CheckRegistration(ctx, RegisterIdentifier{Kind: accountdeletion.TombstoneEmail, Value: " alice@EXAMPLE.com "}); errCode(err) != 45100 {
		t.Fatalf("用被全站封禁的邮箱注册应被拒，得到 %v", err)
	}
	if err := sus.CheckRegistration(ctx, RegisterIdentifier{Kind: accountdeletion.TombstoneEmail, Value: "new@example.com"}); err != nil {
		t.Fatalf("干净的邮箱应能注册：%v", err)
	}
	carol := env.recovery.user(t, "carol", "Pw-123456", "alice@example.com", "")
	err = enforceAccountBan(ctx, env.pg, log, loadUser(carol), userdomain.AccountBanCheckpointSignIn)
	if appErr, ok := err.(*apperrors.AppError); !ok || appErr.Code != 40301 {
		t.Fatalf("同邮箱的新账号登录时应被连带封禁，得到 %v", err)
	}
	detail, err := sus.PlatformBanDetail(ctx, *pbBan.PlatformBanID)
	if err != nil || detail.AccountCount != 3 || detail.SubjectCount < 1 || !strings.Contains(detail.Subjects[0].Masked, "***@example.com") {
		t.Fatalf("全站封禁详情不对：%+v %v", detail, err)
	}

	// ── 解除连带的任意一条 = 解除整条全站封禁 ──
	if _, err := bans.RevokeBan(ctx, app2, alice2, linked.ID, userdomain.AccountBanRevokeInput{Reason: "误封", Operator: op}); err != nil {
		t.Fatal(err)
	}
	for _, x := range [][2]int64{{app, alice}, {app2, alice2}, {app, carol}} {
		if active, _ := bans.GetActiveBan(ctx, x[0], x[1]); active != nil {
			t.Fatalf("全站封禁解除后账号 %d 仍在封禁：%+v", x[1], active)
		}
	}
	if err := sus.CheckRegistration(ctx, RegisterIdentifier{Kind: accountdeletion.TombstoneEmail, Value: "alice@example.com"}); err != nil {
		t.Fatalf("全站封禁解除后应能注册：%v", err)
	}

	// ── 次数用完与冷却 ──
	if _, err := sus.SaveSettings(ctx, app, suspension.Settings{Enabled: true, MaxAppealsPerBan: 1, CooldownHours: 0}, "root"); err != nil {
		t.Fatal(err)
	}
	b2, _ := bans.BanUser(ctx, app, bob, userdomain.AccountBanCreateInput{Reason: "x", Operator: op, Appealable: boolPtr(false)})
	bobView := sus.viewOf(ctx, b2, suspension.LocaleZhCN)
	if bobView.Appeal.Allowed || bobView.Appeal.BlockedReason != suspension.BlockedNotAppealable {
		t.Fatalf("不允许申诉的封禁：%+v", bobView.Appeal)
	}
	if _, err := sus.SubmitAppeal(ctx, AppealSubmitInput{Credential: SuspensionCredential{AppID: app, Token: bobView.Appeal.Token}, Content: "请求复核我的账号封禁。"}); errCode(err) != 40935 {
		t.Fatalf("不允许申诉应被拒，得到 %v", err)
	}

	// ── 语言 ──
	for raw, want := range map[string]string{"zh-Hant-TW": "zh-TW", "zh-HK": "zh-TW", "zh": "zh-CN", "ja-JP": "ja", "ko": "ko", "fr-FR": "en", "": ""} {
		if got := suspension.NormalizeLocale(raw); got != want {
			t.Errorf("NormalizeLocale(%q) = %q，应为 %q", raw, got, want)
		}
	}
	rememberLocale(WithRequestLocale(ctx, "ja"), env.pg, bob)
	if c, _ := env.pg.SuspensionContactOf(ctx, app, bob); c == nil || c.Locale != "ja" {
		t.Fatalf("登录时应记下用户语言：%+v", c)
	}
	_ = time.Now
}

