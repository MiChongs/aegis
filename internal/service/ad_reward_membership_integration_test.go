package service

import (
	"slices"
	"testing"

	"aegis/internal/config"
	"aegis/internal/domain/adpolicy"
	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 看广告赠送的会员权益的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「场景配会员权益 → 看完领会员 → 功能与免广告 → 广告服务免除 → 改场景（存量随之变）→
// 删场景（回落快照）」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAdRewardMembershipIntegration
func TestAdRewardMembershipIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	ads := NewAdPolicyService(zap.NewNop(), pg, rewarded)
	const appID = int64(20000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ad-reward-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"export", "ai.chat"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	userA := insertTestUser(t, ctx, pool, appID, "ad-reward-a", "0")

	saveScenes := func(scenes ...rewardedad.Scene) error {
		_, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
			AppID: appID, Enabled: true, VerifyMode: rewardedad.VerifyClient, Scenes: scenes, Operator: "admin",
		}, "ad-reward-test")
		return err
	}
	daysScene := func(features []string, adFree *bool) rewardedad.Scene {
		return rewardedad.Scene{
			Key: "vip", Name: "看广告领会员", PlacementID: "p1", Enabled: true,
			Rewards:    []rewardedad.Reward{{Type: cardkeydomain.RewardVipDays, Amount: 1}},
			Membership: rewardedad.SceneMembership{Features: features, AdFree: adFree},
		}
	}
	pointsScene := rewardedad.Scene{
		Key: "points", Name: "看广告领积分", PlacementID: "p1", Enabled: true,
		Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}},
	}

	// 功能目录里没有的标识、不送会员天数却配了功能，都挡在保存之前
	if err := saveScenes(daysScene([]string{"missing"}, nil)); errCode(err) != 40000 {
		t.Fatalf("unknown feature should be rejected, got %v", err)
	}
	withFeatures := pointsScene
	withFeatures.Membership.Features = []string{"export"}
	if err := saveScenes(withFeatures); errCode(err) != 40000 {
		t.Fatalf("features without vip_days should be rejected, got %v", err)
	}

	keepAds := false
	if err := saveScenes(daysScene([]string{"export"}, &keepAds), pointsScene); err != nil {
		t.Fatal(err)
	}
	status, err := rewarded.ClientStatus(ctx, appID, userA)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Scenes) != 2 || !slices.Equal(status.Scenes[0].Membership.Features, []string{"export"}) ||
		status.Scenes[0].RewardSummary != "会员天数 1 天（会员可用export）" {
		t.Fatalf("client scene should describe the membership terms, got %+v", status.Scenes[0])
	}

	verified := true
	claim, err := rewarded.Claim(ctx, RewardedAdClaimInput{AppID: appID, UserID: userA, Scene: "vip",
		TransID: "trans-0001", Verified: &verified})
	if err != nil {
		t.Fatal(err)
	}
	if claim.Status != rewardedad.StatusGranted {
		t.Fatalf("claim should be granted in client mode, got %+v", claim)
	}

	ent := mustEntitlement(t, vip, appID, userA)
	if !ent.IsVIP || ent.Source != vipdomain.SourceAdReward || !slices.Equal(ent.Features, []string{"export"}) || ent.AdFree {
		t.Fatalf("ad-granted membership should carry the scene's features and keep ads, got %+v", ent)
	}

	// 广告服务：看广告领的会员不免广告时，与非会员一样要同意
	if _, err := ads.SavePolicy(ctx, adpolicy.SavePolicyInput{AppID: appID, Enabled: true, ConsentVersion: 1,
		VipExempt: true, Splash: adpolicy.SplashConfig{Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	policy, err := ads.ClientPolicy(ctx, appID, userA, "")
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Vip || policy.AdFree || policy.Exempt || policy.Mode != adpolicy.ModeBasic {
		t.Fatalf("ad-granted members that keep ads are not exempt, got %+v", policy)
	}

	// 改场景：已领到的会员随即按新的权益算
	free := true
	if err := saveScenes(daysScene([]string{"ai.chat", "export"}, &free), pointsScene); err != nil {
		t.Fatal(err)
	}
	ent = mustEntitlement(t, vip, appID, userA)
	if !slices.Equal(ent.Features, []string{"ai.chat", "export"}) || !ent.AdFree {
		t.Fatalf("existing ad-granted membership should follow the scene, got %+v", ent)
	}
	policy, err = ads.ClientPolicy(ctx, appID, userA, "")
	if err != nil {
		t.Fatal(err)
	}
	if !policy.AdFree || !policy.Exempt || policy.Mode != adpolicy.ModeFull || policy.Splash.Available {
		t.Fatalf("ad-free members are exempt and see no splash, got %+v", policy)
	}
	profile, err := ads.UserProfile(ctx, appID, userA)
	if err != nil || !profile.AdFree || !profile.Exempt {
		t.Fatalf("user profile should show the same verdict, got %+v / %v", profile, err)
	}

	// 删场景：回落到开通时的快照（功能 export、不免广告）
	if err := saveScenes(pointsScene); err != nil {
		t.Fatal(err)
	}
	ent = mustEntitlement(t, vip, appID, userA)
	if !slices.Equal(ent.Features, []string{"export"}) || ent.AdFree {
		t.Fatalf("deleted scene should fall back to the grant-time snapshot, got %+v", ent)
	}

	// 场景没设置会员权益（这项配置出现之前保存的）：不带功能、免广告，与此前一致
	if err := saveScenes(daysScene(nil, nil), pointsScene); err != nil {
		t.Fatal(err)
	}
	ent = mustEntitlement(t, vip, appID, userA)
	if len(ent.Features) != 0 || !ent.AdFree {
		t.Fatalf("unset membership terms keep the previous behaviour, got %+v", ent)
	}
}
