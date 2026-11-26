package service

import (
	"errors"
	"strconv"
	"testing"

	"go.uber.org/zap"

	"aegis/internal/domain/cosmetic"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 铭牌的端到端集成测试（真实 Postgres，迁移 000176 放宽槽位约束）：
// 两个铭牌槽位套模板建装扮 → 发布上架 → 佩戴进 Loadout 的 idPlate / vipPlate（设计稿带 plate、stretch、nameStyle）→
// 别人按对外 ID 查得到 → 槽位不符戴不上 → 摘下 → 其他槽位的草稿带 plate 被拒 → 工坊不接受铭牌。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55461/postgres go test ./internal/service -run TestCosmeticPlateIntegration
func TestCosmeticPlateIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewCosmeticService(zap.NewNop(), pg, nil)
	const appID = int64(40630)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cosmetic-plates') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "plate-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "plate-bob", "0")
	codeOf := func(err error) int {
		t.Helper()
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("应当是业务错误，得到 %v", err)
		}
		return appErr.Code
	}
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 3, UpdatedBy: "admin",
		Workshop: cosmetic.WorkshopSettings{Enabled: true, FreeQuota: 2}}); err != nil {
		t.Fatal(err)
	}

	live := func(slot, code, template string) *cosmetic.Item {
		t.Helper()
		item, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: slot, Code: code, Name: code,
			Unlock: cosmetic.Unlock{Mode: cosmetic.UnlockFree}}, template)
		if err != nil {
			t.Fatalf("建 %s 失败：%v", slot, err)
		}
		if item.Draft == nil || item.Draft.Plate == nil || item.Draft.NameStyle == nil {
			t.Fatalf("%s 的草稿应带 plate 与 nameStyle：%+v", slot, item.Draft)
		}
		if _, err := svc.Publish(ctx, appID, item.ID, "首发", "admin"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetStatus(ctx, appID, item.ID, cosmetic.StatusActive); err != nil {
			t.Fatal(err)
		}
		return item
	}
	idPlate := live(cosmetic.SlotIDPlate, "black_gold_id", "plate_id_black_gold")
	vipPlate := live(cosmetic.SlotVIPPlate, "black_gold_vip", "plate_vip_black_gold")
	// 空白铭牌（不套模板）也能直接发布
	blank := live(cosmetic.SlotVIPPlate, "blank_vip", "")
	if blank.Draft.Plate.Template != cosmetic.PlateValueToken || blank.Draft.Plate.Stretch == nil {
		t.Fatalf("空白会员铭牌的默认 plate 不对：%+v", blank.Draft.Plate)
	}
	// 铭牌模板不能套到别的槽位
	if _, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotBadge, Code: "x", Name: "x"}, "plate_id_black_gold"); err == nil {
		t.Fatal("勋章套铭牌模板应当被拒")
	}

	equip := func(slot string, id int64) (cosmetic.Loadout, error) {
		return svc.Equip(ctx, EquipInput{AppID: appID, UserID: alice, Slot: slot, CosmeticID: id})
	}
	if _, err := equip(cosmetic.SlotVIPPlate, idPlate.ID); codeOf(err) != errCodeCosmeticNotWearable {
		t.Fatal("号码铭牌戴到会员铭牌槽位应被拒")
	}
	if _, err := equip(cosmetic.SlotIDPlate, idPlate.ID); err != nil {
		t.Fatal(err)
	}
	loadout, err := equip(cosmetic.SlotVIPPlate, vipPlate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadout.IDPlate == nil || loadout.IDPlate.ID != idPlate.ID || loadout.VIPPlate == nil || loadout.VIPPlate.ID != vipPlate.ID {
		t.Fatalf("Loadout 应带两枚铭牌：%+v", loadout)
	}
	d := loadout.IDPlate.Design
	if d == nil || d.Plate == nil || d.Plate.Template != "ID {value}" || d.Plate.Stretch == nil || d.NameStyle == nil {
		t.Fatalf("下发的号码铭牌设计稿不完整：%+v", d)
	}
	for _, l := range d.Layers {
		if l.Placement == "" {
			t.Fatalf("图层 %s 没有 placement", l.ID)
		}
	}
	if loadout.Empty() {
		t.Fatal("只戴铭牌时 Loadout 不应为空")
	}

	// 别人按对外 ID 查得到
	var alicePublic int64
	if err := pool.QueryRow(ctx, `SELECT public_id FROM users WHERE id = $1`, alice).Scan(&alicePublic); err != nil {
		t.Fatal(err)
	}
	others, err := svc.Lookup(ctx, appID, []int64{alicePublic}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := others[strconv.FormatInt(alicePublic, 10)]; got.IDPlate == nil || got.VIPPlate == nil {
		t.Fatalf("按对外 ID 查不到铭牌：%+v", others)
	}
	if byID := svc.LookupByUserIDs(ctx, appID, []int64{alice, bob}, ""); byID[alice].VIPPlate == nil || !byID[bob].Empty() {
		t.Fatalf("按用户编号查的铭牌不对：%+v", byID)
	}

	// 摘下号码铭牌
	loadout, err = equip(cosmetic.SlotIDPlate, 0)
	if err != nil || loadout.IDPlate != nil || loadout.VIPPlate == nil {
		t.Fatalf("摘下号码铭牌后只应剩会员铭牌：%v %+v", err, loadout)
	}

	// 别的槽位草稿带 plate 被拒
	badge, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: cosmetic.SlotBadge, Code: "badge", Name: "勋章"}, "badge_star")
	if err != nil {
		t.Fatal(err)
	}
	withPlate := *badge.Draft
	withPlate.Plate = &cosmetic.Plate{Width: 10, Height: 10, Template: "{value}", Align: "center", FontSize: 8, FontWeight: 700, Font: "sans"}
	if _, err := svc.SaveDraft(ctx, appID, badge.ID, withPlate); err == nil {
		t.Fatal("勋章草稿带 plate 应当被拒")
	}

	// 工坊不接受铭牌，也不在工坊首页列出
	ws, err := svc.Workshop(ctx, appID, alice, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range ws["slots"].([]cosmetic.SlotSpec) {
		if spec.Plate {
			t.Fatalf("工坊首页不应列出铭牌槽位：%s", spec.Key)
		}
	}
	for _, starter := range ws["starters"].([]cosmetic.Template) {
		if cosmetic.IsPlateSlot(starter.Slot) {
			t.Fatalf("工坊起点模板不应有铭牌：%s", starter.Key)
		}
	}
	if _, err := svc.CreateWorkshopItem(ctx, appID, alice, cosmetic.WorkshopInput{Slot: cosmetic.SlotIDPlate, Name: "我的号牌",
		Design: cosmetic.NewDesign(cosmetic.SlotIDPlate)}, ""); err == nil {
		t.Fatal("工坊应拒绝铭牌作品")
	}
}
