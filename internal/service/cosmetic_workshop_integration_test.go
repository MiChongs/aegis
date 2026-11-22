package service

import (
	"errors"
	"testing"

	"go.uber.org/zap"

	"aegis/internal/domain/cosmetic"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 自定义工坊端到端：零件 → 作品（额度）→ 佩戴 → 别人看到的名称 → 运营下架 → 删除；官方列表与发放不认作品。
func TestCosmeticWorkshopIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewCosmeticService(zap.NewNop(), pg, nil)
	const appID = int64(40611)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'workshop') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "ws-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "ws-bob", "0")
	codeOf := func(err error) int {
		t.Helper()
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("应当是业务错误，得到 %v", err)
		}
		return appErr.Code
	}

	var ringAsset, otherAsset int64
	for i, dest := range []*int64{&ringAsset, &otherAsset} {
		if err := pool.QueryRow(ctx, `INSERT INTO cosmetic_assets (appid, name, kind, content_type, width, height, size_bytes, sha256, reference)
VALUES ($1, $2, 'image', 'image/png', 256, 256, 100, $3, 'storage://1/x.png') RETURNING id`,
			appID, "asset", []string{"aa", "bb"}[i]).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}

	// 工坊没开：40362
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Workshop(ctx, appID, alice, ""); codeOf(err) != errCodeWorkshopUnavailable {
		t.Fatalf("工坊未开启应报 40362，得到 %v", err)
	}
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 3,
		Workshop: cosmetic.WorkshopSettings{Enabled: true, FreeQuota: 1, VIPQuota: 5}}); err != nil {
		t.Fatal(err)
	}

	// 零件：只允许头像框
	if _, err := svc.CreateWorkshopPart(ctx, appID, cosmetic.PartInput{AssetID: ringAsset, Name: "银环", Slots: []string{"name_style"}}); err == nil {
		t.Fatal("零件不能用在昵称样式上")
	}
	part, err := svc.CreateWorkshopPart(ctx, appID, cosmetic.PartInput{AssetID: ringAsset, Name: "银环", Category: "环",
		Slots: []string{cosmetic.SlotAvatarFrame}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWorkshopPart(ctx, appID, cosmetic.PartInput{AssetID: ringAsset, Name: "重复", Slots: []string{cosmetic.SlotBadge}, Enabled: true}); codeOf(err) != errCodeWorkshopPartDup {
		t.Fatalf("同一素材重复登记应报 40953：%v", err)
	}

	design := func(asset int64) cosmetic.Design {
		d := cosmetic.NewDesign(cosmetic.SlotAvatarFrame)
		d.Layers = []cosmetic.Layer{{ID: "ring", Type: cosmetic.LayerImage, Asset: "ring", Fit: cosmetic.FitContain,
			Width: 300, Height: 300, AnchorX: 0.5, AnchorY: 0.5, Scale: 1, Opacity: 1}}
		d.Assets = map[string]cosmetic.AssetRef{"ring": {ID: asset}}
		return d
	}
	// 引用不是零件的素材：拒
	if _, err := svc.CreateWorkshopItem(ctx, appID, alice, cosmetic.WorkshopInput{Slot: cosmetic.SlotAvatarFrame, Name: "x", Design: design(otherAsset)}, ""); codeOf(err) != 40000 {
		t.Fatalf("非零件素材应被拒：%v", err)
	}
	mine, err := svc.CreateWorkshopItem(ctx, appID, alice, cosmetic.WorkshopInput{Slot: cosmetic.SlotAvatarFrame, Name: "我的银环", Design: design(ringAsset)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !mine.Workshop || mine.Name != "我的银环" || mine.Revision != 1 || mine.Status != cosmetic.StatusActive {
		t.Fatalf("作品不对：%+v", mine)
	}
	// 免费额度 1：第二件报满
	if _, err := svc.CreateWorkshopItem(ctx, appID, alice, cosmetic.WorkshopInput{Slot: cosmetic.SlotNameStyle, Name: "名字",
		Design: cosmetic.NewDesign(cosmetic.SlotNameStyle)}, ""); codeOf(err) != errCodeWorkshopQuotaFull {
		t.Fatalf("额度已满应报 40954：%v", err)
	}

	// 官方列表、发放都不认作品
	if items, err := svc.ListItems(ctx, pgrepo.CosmeticItemFilter{AppID: appID}); err != nil || len(items) != 0 {
		t.Fatalf("官方列表混进了作品：%d %v", len(items), err)
	}
	if _, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: bob, CosmeticID: mine.ID, Source: cosmetic.SourceAdminGrant}); err == nil {
		t.Fatal("作品不能被发放")
	}

	// 作者能戴，别人不能戴；别人看到的名字是「工坊作品」
	if _, err := svc.Equip(ctx, EquipInput{AppID: appID, UserID: bob, Slot: cosmetic.SlotAvatarFrame, CosmeticID: mine.ID}); codeOf(err) != errCodeCosmeticNotWearable {
		t.Fatalf("别人戴作者的作品应报 40360：%v", err)
	}
	loadout, err := svc.Equip(ctx, EquipInput{AppID: appID, UserID: alice, Slot: cosmetic.SlotAvatarFrame, CosmeticID: mine.ID})
	if err != nil {
		t.Fatal(err)
	}
	if loadout.AvatarFrame == nil || loadout.AvatarFrame.Name != "我的银环" || !loadout.AvatarFrame.Workshop {
		t.Fatalf("作者身上应戴着作品：%+v", loadout.AvatarFrame)
	}
	others := svc.LookupByUserIDs(ctx, appID, []int64{alice}, "")
	if v := others[alice].AvatarFrame; v == nil || v.Name != cosmetic.WorkshopDisplayName || v.Code != "" {
		t.Fatalf("别人看到的应是「工坊作品」：%+v", v)
	}

	// 修改：版本 +1
	updated, err := svc.UpdateWorkshopItem(ctx, appID, alice, mine.ID, "银环二代", design(ringAsset), "")
	if err != nil || updated.Revision != 2 || updated.Name != "银环二代" {
		t.Fatalf("修改失败：%+v %v", updated, err)
	}
	if _, err := svc.UpdateWorkshopItem(ctx, appID, bob, mine.ID, "偷改", design(ringAsset), ""); codeOf(err) != errCodeCosmeticNotFound {
		t.Fatalf("改别人的作品应报 40425：%v", err)
	}

	// 零件停用后：已有作品照戴，新保存被拒
	if _, err := svc.UpdateWorkshopPart(ctx, appID, part.ID, cosmetic.PartInput{AssetID: ringAsset, Name: "银环",
		Slots: []string{cosmetic.SlotAvatarFrame}, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if mine, _ := svc.MyLoadout(ctx, appID, alice, ""); mine.AvatarFrame == nil {
		t.Fatal("零件停用后已有作品应当照戴")
	}
	if _, err := svc.UpdateWorkshopItem(ctx, appID, alice, updated.ID, "三代", design(ringAsset), ""); codeOf(err) != 40000 {
		t.Fatalf("零件停用后保存应被拒：%v", err)
	}

	// 额度归零（工坊关闭免费额度）：作品戴不上，读取时消失
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 3,
		Workshop: cosmetic.WorkshopSettings{Enabled: true, FreeQuota: 0, VIPQuota: 5}}); err != nil {
		t.Fatal(err)
	}
	if mine, _ := svc.MyLoadout(ctx, appID, alice, ""); mine.AvatarFrame != nil {
		t.Fatal("没有额度时作品不该还挂着")
	}
	ws, err := svc.Workshop(ctx, appID, alice, "")
	if err != nil {
		t.Fatal(err)
	}
	if st := ws["settings"].(map[string]any); st["quota"] != 0 || st["canCreate"] != false || st["reason"] != cosmetic.ReasonNeedWorkshop {
		t.Fatalf("额度为零的工坊首页不对：%+v", st)
	}
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 3,
		Workshop: cosmetic.WorkshopSettings{Enabled: true, FreeQuota: 2}}); err != nil {
		t.Fatal(err)
	}

	// 运营下架：作者身上消失、不能再改；恢复后回来
	if _, err := svc.SetWorkshopItemStatus(ctx, appID, mine.ID, true, ""); err == nil {
		t.Fatal("下架要填原因")
	}
	if _, err := svc.SetWorkshopItemStatus(ctx, appID, mine.ID, true, "含不当内容"); err != nil {
		t.Fatal(err)
	}
	if mine, _ := svc.MyLoadout(ctx, appID, alice, ""); mine.AvatarFrame != nil {
		t.Fatal("下架的作品不该还挂着")
	}
	if _, err := svc.UpdateWorkshopItem(ctx, appID, alice, updated.ID, "四代", design(ringAsset), ""); codeOf(err) != errCodeWorkshopTakenDown {
		t.Fatalf("下架的作品不能改，应报 40363：%v", err)
	}
	list, err := svc.AdminWorkshopItems(ctx, pgrepo.WorkshopItemFilter{AppID: appID, Status: cosmetic.StatusArchived})
	if err != nil || list["total"].(int64) != 1 {
		t.Fatalf("管理端作品列表不对：%+v %v", list, err)
	}
	if _, err := svc.SetWorkshopItemStatus(ctx, appID, mine.ID, false, ""); err != nil {
		t.Fatal(err)
	}
	if mine, _ := svc.MyLoadout(ctx, appID, alice, ""); mine.AvatarFrame == nil {
		t.Fatal("恢复后作品应当回来")
	}

	// 删除：佩戴随之消失
	if err := svc.DeleteWorkshopItem(ctx, appID, bob, mine.ID); codeOf(err) != errCodeCosmeticNotFound {
		t.Fatalf("删别人的作品应报 40425：%v", err)
	}
	if err := svc.DeleteWorkshopItem(ctx, appID, alice, mine.ID); err != nil {
		t.Fatal(err)
	}
	if mine, _ := svc.MyLoadout(ctx, appID, alice, ""); mine.AvatarFrame != nil {
		t.Fatal("删掉的作品不该还挂着")
	}
	// 素材被零件引用时不能删
	if err := svc.DeleteAsset(ctx, appID, ringAsset); codeOf(err) != errCodeCosmeticAssetInUse {
		t.Fatalf("被零件引用的素材不能删：%v", err)
	}
}
