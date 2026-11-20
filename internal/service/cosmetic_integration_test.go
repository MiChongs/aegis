package service

import (
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/cosmetic"
	"aegis/internal/domain/pointstore"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 个性装扮的端到端集成测试（真实 Postgres）：
// 建装扮（套模板）→ 草稿校验 → 发布与版本 → 上架 → 未开启时报错 → 只能发放获得的装扮戴不上 → 管理员发放 →
// 佩戴 → 限时发放顺延 → 已永久持有不重复记 → 下架后读取时消失（佩戴行不动）→ 收回后消失 → 免费勋章与勋章位数 →
// 积分商城兑换装扮（已永久持有时灰掉）→ 别人身上的装扮按对外 ID 查 → 有人持有过的不能删。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55461/postgres go test ./internal/service -run TestCosmeticIntegration
func TestCosmeticIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewCosmeticService(zap.NewNop(), pg, nil)
	store := NewPointsStoreService(zap.NewNop(), pg)
	const appID = int64(40610)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cosmetics') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "cos-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "cos-bob", "0")

	codeOf := func(err error) int {
		t.Helper()
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("应当是业务错误，得到 %v", err)
		}
		return appErr.Code
	}

	// 建装扮：模板与槽位不符被拒，套对模板成功
	if _, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotBadge, Code: "halo", Name: "光环"}, "frame_golden_halo"); err == nil {
		t.Fatal("勋章套头像框模板应当被拒")
	}
	frame, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotAvatarFrame, Code: "halo", Name: "鎏金光环",
		Rarity: cosmetic.RarityLegendary}, "frame_golden_halo")
	if err != nil {
		t.Fatal(err)
	}
	if frame.Status != cosmetic.StatusDraft || frame.Revision != 0 || frame.Draft == nil || len(frame.Draft.Layers) != 3 {
		t.Fatalf("新建装扮应是带模板草稿的未发布草稿：%+v", frame)
	}
	if _, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotAvatarFrame, Code: "halo", Name: "重复"}, ""); codeOf(err) != errCodeCosmeticCodeTaken {
		t.Fatalf("重复编码应报 %d", errCodeCosmeticCodeTaken)
	}
	// 未发布不能上架
	if _, err := svc.SetStatus(ctx, appID, frame.ID, cosmetic.StatusActive); codeOf(err) != errCodeCosmeticNotPublished {
		t.Fatal("未发布的装扮不应能上架")
	}
	// 草稿整份校验：坏的草稿存不进去
	bad := *frame.Draft
	bad.Layers = append([]cosmetic.Layer{}, bad.Layers...)
	bad.Layers[0].Stroke = "gold"
	if _, err := svc.SaveDraft(ctx, appID, frame.ID, bad); err == nil {
		t.Fatal("非法颜色的草稿应当被拒")
	}
	// 引用不存在的素材被拒
	withAsset := *frame.Draft
	withAsset.Layers = append([]cosmetic.Layer{}, withAsset.Layers...)
	withAsset.Layers = append(withAsset.Layers, cosmetic.Layer{ID: "img", Type: cosmetic.LayerImage, Placement: cosmetic.PlacementAbove,
		X: 0, Y: 0, Width: 300, Height: 300, AnchorX: 0.5, AnchorY: 0.5, Scale: 1, Opacity: 1, Asset: "a"})
	withAsset.Assets = map[string]cosmetic.AssetRef{"a": {ID: 999999}}
	if _, err := svc.SaveDraft(ctx, appID, frame.ID, withAsset); err == nil {
		t.Fatal("引用不存在素材的草稿应当被拒")
	}
	published, err := svc.Publish(ctx, appID, frame.ID, "首发", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if published.Revision != 1 || published.Design == nil || published.DraftDirty {
		t.Fatalf("发布后应为第 1 版且草稿与发布一致：rev=%d dirty=%v", published.Revision, published.DraftDirty)
	}
	if _, err := svc.Publish(ctx, appID, frame.ID, "", "admin"); err == nil {
		t.Fatal("没有修改时不应能重复发布")
	}
	// 改草稿再发第 2 版，回退第 1 版到草稿
	edited := *published.Draft
	edited.Duration = 4000
	edited.Layers = append([]cosmetic.Layer{}, edited.Layers...)
	edited.Layers[0].Tracks = []cosmetic.Track{{Property: cosmetic.PropRotation, Keyframes: []cosmetic.Keyframe{{T: 0, V: 0}, {T: 4000, V: 360}}}}
	edited.Layers[1].Tracks = nil
	edited.Layers[2].Tracks = nil
	if _, err := svc.SaveDraft(ctx, appID, frame.ID, edited); err != nil {
		t.Fatal(err)
	}
	if v2, err := svc.Publish(ctx, appID, frame.ID, "加快", "admin"); err != nil || v2.Revision != 2 {
		t.Fatalf("第 2 版发布失败：%v", err)
	}
	revisions, err := svc.Revisions(ctx, appID, frame.ID)
	if err != nil || len(revisions) != 2 || revisions[0].Revision != 2 {
		t.Fatalf("版本历史不对：%v %+v", err, revisions)
	}
	restored, err := svc.RestoreRevision(ctx, appID, frame.ID, 1)
	if err != nil || !restored.DraftDirty || restored.Draft.Duration != 6000 {
		t.Fatalf("回退第 1 版到草稿失败：%v", err)
	}
	if _, err := svc.SetStatus(ctx, appID, frame.ID, cosmetic.StatusActive); err != nil {
		t.Fatal(err)
	}

	// 应用没开启：装扮中心报错，我的佩戴为空
	if _, err := svc.Center(ctx, appID, alice, ""); codeOf(err) != errCodeCosmeticDisabled {
		t.Fatal("未开启时装扮中心应报 40361")
	}
	if loadout, err := svc.MyLoadout(ctx, appID, alice, ""); err != nil || !loadout.Empty() {
		t.Fatalf("未开启时我的佩戴应为空：%v", err)
	}
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 2, UpdatedBy: "admin"}); err != nil {
		t.Fatal(err)
	}

	// 只能发放获得：没有就戴不上
	equip := func(user int64, slot string, position int, id int64) (cosmetic.Loadout, error) {
		return svc.Equip(ctx, EquipInput{AppID: appID, UserID: user, Slot: slot, Position: position, CosmeticID: id})
	}
	if _, err := equip(alice, cosmetic.SlotAvatarFrame, 0, frame.ID); codeOf(err) != errCodeCosmeticNotWearable {
		t.Fatal("没有持有时应不能佩戴")
	}
	if _, err := equip(alice, cosmetic.SlotBadge, 0, frame.ID); codeOf(err) != errCodeCosmeticNotWearable {
		t.Fatal("槽位不符应不能佩戴")
	}

	// 管理员发 7 天，佩戴；再发 7 天顺延
	first, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: alice, CosmeticID: frame.ID, Days: 7, Operator: "admin"})
	if err != nil || first.Ownership == nil || first.Ownership.EndsAt == nil {
		t.Fatalf("发放失败：%v", err)
	}
	loadout, err := equip(alice, cosmetic.SlotAvatarFrame, 0, frame.ID)
	if err != nil || loadout.AvatarFrame == nil || loadout.AvatarFrame.Revision != 2 {
		t.Fatalf("佩戴后应戴着第 2 版头像框：%v %+v", err, loadout)
	}
	second, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: alice, CosmeticID: frame.ID, Days: 7, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Ownership.StartsAt.Equal(*first.Ownership.EndsAt) {
		t.Fatalf("第二段应从第一段的终点开始：%v vs %v", second.Ownership.StartsAt, *first.Ownership.EndsAt)
	}
	if gap := second.Ownership.EndsAt.Sub(time.Now()); gap < 13*24*time.Hour || gap > 14*24*time.Hour+time.Minute {
		t.Fatalf("两段顺延后应约 14 天，得到 %v", gap)
	}

	// 下架：读取时消失，佩戴行还在；重新上架即恢复
	if _, err := svc.SetStatus(ctx, appID, frame.ID, cosmetic.StatusArchived); err != nil {
		t.Fatal(err)
	}
	if loadout, _ := svc.MyLoadout(ctx, appID, alice, ""); loadout.AvatarFrame != nil {
		t.Fatal("下架后不应再显示")
	}
	if _, err := svc.SetStatus(ctx, appID, frame.ID, cosmetic.StatusActive); err != nil {
		t.Fatal(err)
	}
	if loadout, _ := svc.MyLoadout(ctx, appID, alice, ""); loadout.AvatarFrame == nil {
		t.Fatal("重新上架后应当恢复")
	}

	// 收回两段后消失
	for _, own := range []*cosmetic.Ownership{first.Ownership, second.Ownership} {
		if _, err := svc.Revoke(ctx, appID, alice, own.ID, "admin", "测试"); err != nil {
			t.Fatal(err)
		}
	}
	if loadout, _ := svc.MyLoadout(ctx, appID, alice, ""); loadout.AvatarFrame != nil {
		t.Fatal("收回后不应再显示")
	}
	center, err := svc.Center(ctx, appID, alice, "")
	if err != nil {
		t.Fatal(err)
	}
	items := center["items"].([]cosmetic.CenterItem)
	if len(items) != 1 || items[0].State.Usable || items[0].State.Reason != cosmetic.ReasonExpired {
		t.Fatalf("持有过又被收回的装扮应显示已过期：%+v", items)
	}

	// 永久发放后再发不重复记
	if _, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: alice, CosmeticID: frame.ID, Days: 0, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if again, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: alice, CosmeticID: frame.ID, Days: 30, Operator: "admin"}); err != nil || !again.AlreadyPermanent {
		t.Fatalf("已永久持有时应不再记新的一段：%v", err)
	}

	// 免费勋章：人人可戴；勋章位数为 2，第 3 位被拒；同一枚换位不重复
	badge, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotBadge, Code: "star", Name: "星章",
		Unlock: cosmetic.Unlock{Mode: cosmetic.UnlockFree}}, "badge_star")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, appID, badge.ID, "", "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, appID, badge.ID, cosmetic.StatusActive); err != nil {
		t.Fatal(err)
	}
	if _, err := equip(bob, cosmetic.SlotBadge, 2, badge.ID); err == nil {
		t.Fatal("超出勋章位数应当被拒")
	}
	if _, err := equip(bob, cosmetic.SlotBadge, 0, badge.ID); err != nil {
		t.Fatal(err)
	}
	loadout, err = equip(bob, cosmetic.SlotBadge, 1, badge.ID)
	if err != nil || len(loadout.Badges) != 1 {
		t.Fatalf("同一枚勋章换位后应只戴一枚：%v %+v", err, loadout.Badges)
	}

	// 别人身上的装扮按对外 ID 查
	var bobPublic, alicePublic int64
	if err := pool.QueryRow(ctx, `SELECT public_id FROM users WHERE id = $1`, bob).Scan(&bobPublic); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT public_id FROM users WHERE id = $1`, alice).Scan(&alicePublic); err != nil {
		t.Fatal(err)
	}
	if _, err := equip(alice, cosmetic.SlotAvatarFrame, 0, frame.ID); err != nil {
		t.Fatal(err)
	}
	others, err := svc.Lookup(ctx, appID, []int64{bobPublic, alicePublic, 123}, "https://api.example.com")
	if err != nil || len(others) != 2 {
		t.Fatalf("应查到两个人：%v %+v", err, others)
	}

	// 积分商城：兑换装扮；已永久持有的人看到「已拥有」
	if _, err := store.SaveSettings(ctx, appID, true, "admin"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "光环 30 天", Cost: 10,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardCosmetic, RefID: frame.ID, Amount: 30}}, Status: pointstore.StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100 WHERE id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Redeem(ctx, pointstore.RedeemInput{AppID: appID, UserID: bob, ItemID: item.ID, RequestID: "cos-1"}); err != nil {
		t.Fatalf("兑换装扮失败：%v", err)
	}
	if loadout, err := equip(bob, cosmetic.SlotAvatarFrame, 0, frame.ID); err != nil || loadout.AvatarFrame == nil {
		t.Fatalf("兑换后应能佩戴：%v", err)
	}
	storeView, err := store.Store(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(storeView.Items) != 1 || storeView.Items[0].Availability.Reason != pointstore.ReasonOwned {
		t.Fatalf("已永久持有的人应看到已拥有：%+v", storeView.Items)
	}
	// 引用未上架的装扮不能配成商品
	if _, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "坏商品", Cost: 10,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardCosmetic, RefID: 999999}}, Status: pointstore.StatusActive}); err == nil {
		t.Fatal("引用不存在的装扮应当被拒")
	}

	// 有人持有过不能删；没人持有的可以
	if err := svc.DeleteItem(ctx, appID, frame.ID); codeOf(err) != errCodeCosmeticHasOwners {
		t.Fatal("有人持有过的装扮不应能删除")
	}
	spare, err := svc.Duplicate(ctx, appID, frame.ID, "admin")
	if err != nil || spare.Code != "halo-copy" {
		t.Fatalf("复制失败：%v", err)
	}
	if err := svc.DeleteItem(ctx, appID, spare.ID); err != nil {
		t.Fatal(err)
	}
	stats, err := svc.Stats(ctx, appID)
	if err != nil || stats.Items != 2 || stats.Wearers != 2 {
		t.Fatalf("统计不对：%v %+v", err, stats)
	}
}
