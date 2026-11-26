package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/cosmetic"
	"aegis/internal/domain/itemgrant"
	pgrepo "aegis/internal/repository/postgres"
)

// 「发放时佩戴」（奖励 equip = true）的端到端集成测试（真实 Postgres）：
// 管理员发放后 Loadout 里有这件 → 同槽位再发一件替换 → 已永久持有再次发放仍会戴上 → equip = false 只发不戴 →
// 勋章放第一个空位、满了只发不戴 → 物品发放规则里的装扮带 equip 时同样戴上、结果注明「已戴上」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55461/postgres go test ./internal/service -run TestCosmeticGrantEquipIntegration
func TestCosmeticGrantEquipIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewCosmeticService(zap.NewNop(), pg, nil)
	const appID = int64(40640)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cosmetic-equip') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "equip-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "equip-bob", "0")
	if _, err := svc.SaveSettings(ctx, cosmetic.Settings{AppID: appID, Enabled: true, BadgeSlots: 2, UpdatedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	live := func(slot, code, template string) *cosmetic.Item {
		t.Helper()
		item, err := svc.CreateItem(ctx, cosmetic.ItemInput{AppID: appID, Slot: slot, Code: code, Name: code}, template)
		if err != nil {
			t.Fatalf("建 %s 失败：%v", code, err)
		}
		if _, err := svc.Publish(ctx, appID, item.ID, "", "admin"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetStatus(ctx, appID, item.ID, cosmetic.StatusActive); err != nil {
			t.Fatal(err)
		}
		return item
	}
	loadoutOf := func(user int64) cosmetic.Loadout {
		t.Helper()
		l, err := svc.MyLoadout(ctx, appID, user, "")
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	grant := func(user, id int64, days int, equip bool) *cosmetic.GrantResult {
		t.Helper()
		r, err := svc.Grant(ctx, cosmetic.GrantInput{AppID: appID, UserID: user, CosmeticID: id, Days: days, Operator: "admin", Equip: equip})
		if err != nil {
			t.Fatalf("发放 #%d 失败：%v", id, err)
		}
		return r
	}

	gold := live(cosmetic.SlotVIPPlate, "vip_gold", "plate_vip_black_gold")
	plain := live(cosmetic.SlotVIPPlate, "vip_plain", "")

	// 只能发放获得的会员铭牌：发 30 天并戴上
	if r := grant(alice, gold.ID, 30, true); !r.Equipped || r.Ownership == nil {
		t.Fatalf("equip=true 发放后应戴上：%+v", r)
	}
	if l := loadoutOf(alice); l.VIPPlate == nil || l.VIPPlate.ID != gold.ID {
		t.Fatalf("Loadout 里应有刚发的会员铭牌：%+v", l)
	}
	// 同一槽位再发一件，替换原有佩戴
	if r := grant(alice, plain.ID, 0, true); !r.Equipped {
		t.Fatal("第二件也应戴上")
	}
	if l := loadoutOf(alice); l.VIPPlate == nil || l.VIPPlate.ID != plain.ID {
		t.Fatalf("应替换为第二件：%+v", l.VIPPlate)
	}
	// 摘下后再发一次已永久持有的那件：不记新段，但仍会戴上
	if _, err := svc.Equip(ctx, EquipInput{AppID: appID, UserID: alice, Slot: cosmetic.SlotVIPPlate}); err != nil {
		t.Fatal(err)
	}
	if r := grant(alice, plain.ID, 0, true); !r.AlreadyPermanent || !r.Equipped || r.Ownership != nil {
		t.Fatalf("已永久持有再次发放应只戴不记：%+v", r)
	}
	if l := loadoutOf(alice); l.VIPPlate == nil || l.VIPPlate.ID != plain.ID {
		t.Fatalf("已永久持有再次发放后应戴着：%+v", l.VIPPlate)
	}
	// equip = false 只发不戴
	if r := grant(bob, gold.ID, 7, false); r.Equipped {
		t.Fatal("equip=false 不应戴上")
	}
	if l := loadoutOf(bob); l.VIPPlate != nil {
		t.Fatal("equip=false 时 Loadout 不应有铭牌")
	}

	// 勋章：放到第一个空位；已戴着的不挪；满了只发不戴、不报错
	b1 := live(cosmetic.SlotBadge, "b1", "badge_star")
	b2 := live(cosmetic.SlotBadge, "b2", "badge_text")
	b3 := live(cosmetic.SlotBadge, "b3", "badge_star")
	if r := grant(bob, b1.ID, 0, true); !r.Equipped || r.Position != 0 {
		t.Fatalf("第一枚勋章应戴在 0 号位：%+v", r)
	}
	if r := grant(bob, b2.ID, 0, true); !r.Equipped || r.Position != 1 {
		t.Fatalf("第二枚勋章应戴在 1 号位：%+v", r)
	}
	if r := grant(bob, b1.ID, 0, true); !r.AlreadyPermanent || !r.Equipped || r.Position != 0 {
		t.Fatalf("已戴着的勋章再发应原位不动：%+v", r)
	}
	if r := grant(bob, b3.ID, 0, true); r.Equipped || r.Ownership == nil {
		t.Fatalf("勋章位满了应只发不戴：%+v", r)
	}
	if l := loadoutOf(bob); len(l.Badges) != 2 || l.Badges[0].ID != b1.ID || l.Badges[1].ID != b2.ID {
		t.Fatalf("勋章佩戴不对：%+v", l.Badges)
	}

	// 物品发放：规则里的装扮带 equip
	grants := NewItemGrantService(zap.NewNop(), pg)
	nodeOf := func(id, nodeType string, x float64, data any) itemgrant.Node {
		raw, _ := json.Marshal(data)
		return itemgrant.Node{ID: id, Type: nodeType, Position: itemgrant.Position{X: x}, Data: raw}
	}
	link := func(source, handle, target string) itemgrant.Edge {
		return itemgrant.Edge{ID: source + ">" + target, Source: source, SourceHandle: handle, Target: target}
	}
	idPlate := live(cosmetic.SlotIDPlate, "id_gold", "plate_id_black_gold")
	graph := itemgrant.Graph{
		Nodes: []itemgrant.Node{
			nodeOf("t", itemgrant.NodeTrigger, 0, itemgrant.TriggerConfig{Kind: itemgrant.TriggerManual}),
			nodeOf("aud", itemgrant.NodeAudience, 300, itemgrant.AudienceConfig{Scope: itemgrant.ScopeAll}),
			nodeOf("lim", itemgrant.NodeLimit, 600, itemgrant.LimitConfig{Period: itemgrant.PeriodTotal, PerUser: 1}),
			nodeOf("g", itemgrant.NodeGrant, 900, itemgrant.GrantConfig{Items: []itemgrant.Item{
				{Type: itemgrant.ItemCosmetic, RefID: idPlate.ID, Amount: 0, Equip: true},
			}}),
		},
		Edges: []itemgrant.Edge{link("t", "next", "aud"), link("aud", "next", "lim"), link("lim", "pass", "g")},
	}
	rule, err := grants.CreateRule(ctx, itemgrant.RuleInput{AppID: appID, Name: "领航者铭牌", Graph: graph, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	var saved itemgrant.GrantConfig
	if err := json.Unmarshal(rule.Graph.Nodes[3].Data, &saved); err != nil || len(saved.Items) != 1 || !saved.Items[0].Equip {
		t.Fatalf("规则保存后应保留 equip：%s %v", rule.Graph.Nodes[3].Data, err)
	}
	if _, err := grants.SetStatus(ctx, appID, rule.ID, itemgrant.StatusActive, "admin"); err != nil {
		t.Fatal(err)
	}
	run, err := grants.RunRule(ctx, appID, rule.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if run, err = grants.GetRun(ctx, appID, run.ID); err != nil {
			t.Fatal(err)
		}
		if run.FinishedAt != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if run.Status != itemgrant.RunSucceeded || run.Granted != 2 {
		t.Fatalf("物品发放应给 2 人到账：%+v", run)
	}
	for _, user := range []int64{alice, bob} {
		if l := loadoutOf(user); l.IDPlate == nil || l.IDPlate.ID != idPlate.ID {
			t.Fatalf("用户 %d 应戴上号码铭牌：%+v", user, l.IDPlate)
		}
	}
	page, err := grants.ListRecords(ctx, itemgrant.RecordQuery{AppID: appID, RuleID: rule.ID, Page: 1, Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("应有 2 条发放记录：%v", err)
	}
	for _, rec := range page.Items {
		if len(rec.Results) != 1 || !rec.Results[0].Equipped || !strings.Contains(rec.Results[0].Detail, "已戴上") {
			t.Fatalf("发放结果应注明已戴上：%+v", rec.Results)
		}
	}
}
