package service

import (
	"testing"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// Apex 的 Security Key 按广告位签发：两个广告位的回调各用各的 Key 验签，默认 Key 给没单独配的兜底。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55433/postgres go test ./internal/service -run TestRewardedAdPlacementKeysIntegration
func TestRewardedAdPlacementKeysIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	const appID = int64(20200)
	const appKey = "placement-test"
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, appID, appKey); err != nil {
		t.Fatal(err)
	}
	const first, second = "5736335593863633", "8725524153286753"
	scenes := []rewardedad.Scene{
		{Key: "points", Name: "看广告领积分", PlacementID: first, Enabled: true,
			Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 10}}},
		{Key: "exp", Name: "看广告领经验", PlacementID: second, Enabled: true,
			Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardExperience, Amount: 5}}},
	}
	save := func(input rewardedad.SaveConfigInput) (*rewardedad.AdminConfig, error) {
		input.AppID, input.Provider, input.VerifyMode, input.Operator = appID, "apex", rewardedad.VerifyServer, "admin"
		if input.Scenes == nil {
			input.Scenes = scenes
		}
		return rewarded.SaveConfig(ctx, input, appKey)
	}

	// 只给第一个广告位配 Key、没有默认 Key：第二个广告位的场景启用时会验不过签，保存要挡下
	onlyFirst := []rewardedad.PlacementInput{{ID: first, Name: "激励视频", SecurityKey: "key-one"}, {ID: second, Name: "激励视频2"}}
	if _, err := save(rewardedad.SaveConfigInput{Enabled: true, Placements: &onlyFirst}); errCode(err) != 40000 {
		t.Fatalf("enabling with a keyless placement should be refused, got %v", err)
	}
	both := []rewardedad.PlacementInput{
		{ID: first, Name: "激励视频", SecurityKey: "key-one"},
		{ID: second, Name: "激励视频2", SecurityKey: "key-two"},
	}
	view, err := save(rewardedad.SaveConfigInput{Enabled: true, Placements: &both})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Placements) != 2 || view.Placements[0].KeySource != rewardedad.KeySourceOwn ||
		view.Placements[1].SecurityKeyHint != "…-two" && view.Placements[1].SecurityKeyHint != "已配置" {
		t.Fatalf("both placements should report their own key, got %+v", view.Placements)
	}
	if view.HasSecurityKey {
		t.Fatal("no default key was set")
	}

	user := insertTestUser(t, ctx, pool, appID, "placement-a", "0")
	status, err := rewarded.ClientStatus(ctx, appID, user)
	if err != nil {
		t.Fatal(err)
	}
	callback := func(trans, placement, key, scene string) (bool, string) {
		return rewarded.AdCallback(ctx, AdCallbackInput{
			AppID: appID, UserID: status.UserID, TransID: trans, PlacementID: placement,
			Sign: rewardedad.CallbackSign(key, trans), Extra: `{"aegisScene":"` + scene + `"}`,
		})
	}
	if valid, reason := callback("trans-1", first, "key-one", "points"); !valid || reason != rewardedad.StatusGranted {
		t.Fatalf("first placement with its own key should grant, got %v %q", valid, reason)
	}
	if valid, reason := callback("trans-2", second, "key-two", "exp"); !valid || reason != rewardedad.StatusGranted {
		t.Fatalf("second placement with its own key should grant, got %v %q", valid, reason)
	}
	// 用别的广告位的 Key 签：说明回调里的广告位被改过，不认
	if valid, reason := callback("trans-3", second, "key-one", "exp"); valid || reason != "bad_sign" {
		t.Fatalf("a key from another placement must be refused, got %v %q", valid, reason)
	}

	// 只改场景、不传广告位列表（老控制台、MCP）：广告位与它们的 Key 原样保留
	view, err = save(rewardedad.SaveConfigInput{Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Placements) != 2 || !view.Placements[1].HasSecurityKey {
		t.Fatalf("omitting placements must keep them, got %+v", view.Placements)
	}
	if view.Placements[0].Callbacks != 1 || view.Placements[0].LastCallbackAt == nil {
		t.Fatalf("activity should count the verified callback, got %+v", view.Placements[0])
	}

	// 改广告位 ID 时用 previousId 把 Key 带过去；场景跟着改到新 ID
	const renamed = "9999999999999999"
	moved := []rewardedad.PlacementInput{{ID: first, Name: "激励视频"}, {ID: renamed, PreviousID: second, Name: "激励视频2"}}
	movedScenes := append([]rewardedad.Scene{}, scenes...)
	movedScenes[1].PlacementID = renamed
	view, err = save(rewardedad.SaveConfigInput{Enabled: true, Placements: &moved, Scenes: movedScenes})
	if err != nil {
		t.Fatal(err)
	}
	if view.Placements[1].ID != renamed || view.Placements[1].KeySource != rewardedad.KeySourceOwn {
		t.Fatalf("renamed placement should keep its key, got %+v", view.Placements[1])
	}
	if valid, _ := callback("trans-4", renamed, "key-two", "exp"); !valid {
		t.Fatal("renamed placement should verify with the carried key")
	}

	// 清掉第二个广告位的 Key、填默认 Key：它改用默认 Key 验签，第一个仍只认自己的
	cleared := []rewardedad.PlacementInput{{ID: first, Name: "激励视频"}, {ID: renamed, Name: "激励视频2", ClearSecurityKey: true}}
	view, err = save(rewardedad.SaveConfigInput{Enabled: true, Placements: &cleared, Scenes: movedScenes, SecurityKey: "key-default"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Placements[1].KeySource != rewardedad.KeySourceDefault {
		t.Fatalf("cleared placement should fall back to the default key, got %+v", view.Placements[1])
	}
	if valid, _ := callback("trans-5", renamed, "key-default", "exp"); !valid {
		t.Fatal("cleared placement should verify with the default key")
	}
	if valid, reason := callback("trans-6", first, "key-default", "points"); valid || reason != "bad_sign" {
		t.Fatalf("a placement with its own key must not accept the default key, got %v %q", valid, reason)
	}
}
