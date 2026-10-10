package service

import (
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/internal/domain/adpolicy"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 广告服务策略的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「未配置 → 保存策略 → 未登录与登录的处境 → 拒绝 / 同意 / 重复同意不留痕 → ifAbsent 同步 →
// 调高条款版本 → 会员免除 → 开屏上报去重与频控 → 统计与用户详情」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAdPolicyIntegration
func TestAdPolicyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	svc := NewAdPolicyService(zap.NewNop(), pg, rewarded)
	const appID = int64(20000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ad-policy-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	userA := insertTestUser(t, ctx, pool, appID, "ad-a", "0")
	userB := insertTestUser(t, ctx, pool, appID, "ad-b", "0")

	// ── 没配过：如实回答 configured=false，管理端看到默认值 ──
	guest, err := svc.ClientPolicy(ctx, appID, 0, "device-guest")
	if err != nil {
		t.Fatal(err)
	}
	if guest.Configured || guest.Enabled || guest.Mode != adpolicy.ModeFull || !guest.DecisionRequired {
		t.Fatalf("未配置时应为 configured=false、full、需要选择，得到 %+v", guest)
	}
	admin, err := svc.AdminPolicy(ctx, appID)
	if err != nil || admin.Configured || !admin.VipExempt || !admin.Splash.Enabled || admin.ConsentVersion != 1 {
		t.Fatalf("默认策略不对：%+v / %v", admin, err)
	}

	saved, err := svc.SavePolicy(ctx, adpolicy.SavePolicyInput{
		AppID: appID, Enabled: true, ConsentVersion: 1, PolicyURL: "https://example.com/ads", VipExempt: true,
		BasicTools: []string{"Todo", "stopwatch", "todo"},
		Splash:     adpolicy.SplashConfig{Enabled: true, PlacementID: "splash1", MinIntervalSeconds: 600, DailyLimit: 2},
		Operator:   "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Configured || len(saved.BasicTools) != 2 || saved.BasicTools[0] != "todo" {
		t.Fatalf("保存结果不对：%+v", saved)
	}

	// ── 未登录：按「还没选」回答，开屏的同意条件交给客户端 ──
	guest, err = svc.ClientPolicy(ctx, appID, 0, "device-guest")
	if err != nil {
		t.Fatal(err)
	}
	if !guest.Configured || guest.SignedIn || guest.Mode != adpolicy.ModeBasic || guest.AdUserID != "" || !guest.Splash.Available {
		t.Fatalf("未登录时的处境不对：%+v", guest)
	}

	// ── 登录、拒绝 ──
	declined, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: false, DeviceID: "device-a"})
	if err != nil {
		t.Fatal(err)
	}
	if declined.Mode != adpolicy.ModeBasic || declined.DecisionRequired || declined.Consent == nil || declined.Consent.Accepted ||
		declined.Splash.Available || declined.AdUserID == "" {
		t.Fatalf("拒绝后应为基础模式、不放开屏，得到 %+v", declined)
	}
	// 同意
	accepted, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: true, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Mode != adpolicy.ModeFull || !accepted.Splash.Available {
		t.Fatalf("同意后应为完整模式、可放开屏，得到 %+v", accepted)
	}
	// 同样的选择再报一次不留痕
	if _, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: true}); err != nil {
		t.Fatal(err)
	}
	// ifAbsent：账号上已经有选择，本机的「拒绝」不覆盖
	synced, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: false,
		IfAbsent: true, Source: adpolicy.SourceGuestSync})
	if err != nil {
		t.Fatal(err)
	}
	if !synced.Consent.Accepted || synced.Mode != adpolicy.ModeFull {
		t.Fatalf("ifAbsent 不应覆盖已有的选择，得到 %+v", synced.Consent)
	}
	logs, err := svc.ListConsentLogs(ctx, adpolicy.ConsentLogQuery{AppID: appID, UserID: userA})
	if err != nil {
		t.Fatal(err)
	}
	if logs.Total != 2 || !logs.Items[0].Accepted || logs.Items[1].Accepted {
		t.Fatalf("历史应只有「拒绝、同意」两条，得到 %+v", logs.Items)
	}
	// ifAbsent：账号上还没有选择，本机的选择同步上来
	syncedB, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userB, Accepted: true,
		IfAbsent: true, Source: adpolicy.SourceGuestSync})
	if err != nil {
		t.Fatal(err)
	}
	if syncedB.Consent == nil || !syncedB.Consent.Accepted || syncedB.Consent.Source != adpolicy.SourceGuestSync {
		t.Fatalf("账号上没有选择时应同步本机的选择，得到 %+v", syncedB.Consent)
	}
	// 来源与版本校验
	if _, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: true, Version: 2}); errCode(err) != 40000 {
		t.Fatalf("高于当前版本的选择应被拒，得到 %v", err)
	}
	if _, err := svc.SaveConsent(ctx, adpolicy.SaveConsentInput{AppID: appID, UserID: userA, Accepted: true, Source: "robot"}); errCode(err) != 40000 {
		t.Fatalf("未知来源应被拒，得到 %v", err)
	}

	// ── 开屏上报：去重、坏数据丢弃、频控 ──
	now := time.Now().UTC()
	earlier := now.Add(-time.Minute)
	report, err := svc.ReportSplash(ctx, adpolicy.SplashReportInput{
		AppID: appID, UserID: userA, DeviceID: "device-a",
		Events: []adpolicy.SplashEventInput{
			{EventID: "evt-00000001", Status: "shown", PlacementID: "splash1", LoadMs: 800, ShownMs: 5000, OccurredAt: &earlier},
			{EventID: "evt-00000001", Status: "shown"},
			{EventID: "bad", Status: "shown"},
			{EventID: "evt-00000002", Status: "exploded"},
			{EventID: "evt-00000003", Status: "failed", ErrorCode: "500422"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Received != 5 || report.Recorded != 2 {
		t.Fatalf("应记录 2 条，得到 %+v", report)
	}
	again, err := svc.ReportSplash(ctx, adpolicy.SplashReportInput{AppID: appID, UserID: userA,
		Events: []adpolicy.SplashEventInput{{EventID: "evt-00000001", Status: "shown"}}})
	if err != nil || again.Recorded != 0 {
		t.Fatalf("重传不应重复记录，得到 %+v / %v", again, err)
	}
	afterShown, err := svc.ClientPolicy(ctx, appID, userA, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if afterShown.Splash.TodayCount != 1 || afterShown.Splash.Available || afterShown.Splash.NextAvailableAt == nil {
		t.Fatalf("10 分钟间隔内不应再放开屏，得到 %+v", afterShown.Splash)
	}
	// 未登录的记录只挂设备，不算到登录账号的频控里
	if _, err := svc.ReportSplash(ctx, adpolicy.SplashReportInput{AppID: appID, DeviceID: "device-guest",
		Events: []adpolicy.SplashEventInput{{EventID: "evt-guest-01", Status: "clicked"}}}); err != nil {
		t.Fatal(err)
	}
	guestAfter, err := svc.ClientPolicy(ctx, appID, 0, "device-guest")
	if err != nil || guestAfter.Splash.TodayCount != 1 || guestAfter.Splash.Available {
		t.Fatalf("未登录按设备频控，得到 %+v / %v", guestAfter.Splash, err)
	}
	bAfter, err := svc.ClientPolicy(ctx, appID, userB, "device-guest")
	if err != nil || bAfter.Splash.TodayCount != 0 || !bAfter.Splash.Available {
		t.Fatalf("登录用户按账号频控，不受设备上的未登录记录影响，得到 %+v / %v", bAfter.Splash, err)
	}
	events, err := svc.ListSplashEvents(ctx, adpolicy.SplashQuery{AppID: appID, UserID: userA})
	if err != nil || events.Total != 2 || events.Items[0].Account != "ad-a" {
		t.Fatalf("开屏记录应挂在账号上，得到 %+v / %v", events, err)
	}

	// ── 调高条款版本：所有人重新选择，按旧版本同意的人回到基础模式 ──
	if _, err := svc.SavePolicy(ctx, adpolicy.SavePolicyInput{AppID: appID, Enabled: true, ConsentVersion: 2, VipExempt: true,
		BasicTools: []string{"todo"}, Splash: adpolicy.SplashConfig{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SavePolicy(ctx, adpolicy.SavePolicyInput{AppID: appID, Enabled: true, ConsentVersion: 1}); errCode(err) != 40000 {
		t.Fatalf("条款版本不能调低，得到 %v", err)
	}
	outdated, err := svc.ClientPolicy(ctx, appID, userA, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if outdated.Mode != adpolicy.ModeBasic || !outdated.DecisionRequired || outdated.Splash.Available {
		t.Fatalf("按旧版本同意的人应回到基础模式并重新选择，得到 %+v", outdated)
	}
	outdatedList, err := svc.ListConsents(ctx, adpolicy.ConsentQuery{AppID: appID, Outdated: true})
	if err != nil || outdatedList.Total != 2 || !outdatedList.Items[0].Outdated {
		t.Fatalf("两个人都应列为待重新选择，得到 %+v / %v", outdatedList, err)
	}

	// ── 会员免除 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_expire_at = NOW() + INTERVAL '30 days' WHERE id = $1`, userA); err != nil {
		t.Fatal(err)
	}
	member, err := svc.ClientPolicy(ctx, appID, userA, "device-a")
	if err != nil {
		t.Fatal(err)
	}
	if !member.Vip || !member.Exempt || member.Mode != adpolicy.ModeFull || member.Splash.Available {
		t.Fatalf("有效会员应免除同意要求且不放开屏，得到 %+v", member)
	}

	// ── 统计与用户详情 ──
	stats, err := svc.Stats(ctx, appID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.Trend) != 7 || stats.Summary.Outdated != 2 || stats.Summary.TodayShown != 2 || stats.Summary.TodayFailed != 1 ||
		stats.Summary.TodayClicked != 1 || stats.Summary.TodayUsers != 2 {
		t.Fatalf("统计不对：%+v", stats.Summary)
	}
	if last := stats.Trend[6]; last.Accepted != 2 || last.Declined != 1 {
		t.Fatalf("今天的选择变更应为同意 2、拒绝 1，得到 %+v", last)
	}
	profile, err := svc.UserProfile(ctx, appID, userA)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.Exempt || len(profile.Logs) != 2 || profile.Splash.Shown != 1 || profile.Splash.Failed != 1 || len(profile.RecentSplash) != 2 {
		t.Fatalf("用户详情不对：%+v", profile)
	}
}
