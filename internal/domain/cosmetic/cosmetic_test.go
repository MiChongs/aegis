package cosmetic

import (
	"strings"
	"testing"
	"time"
)

func TestTemplatesValidate(t *testing.T) {
	for _, tpl := range Templates() {
		normalized := NormalizeDesign(tpl.Slot, tpl.Design)
		if err := ValidateDesign(tpl.Slot, normalized); err != nil {
			t.Fatalf("模板 %s 校验失败：%v", tpl.Key, err)
		}
		if _, ok := FindSlot(tpl.Slot); !ok {
			t.Fatalf("模板 %s 的槽位未登记", tpl.Key)
		}
		if _, ok := FindRarity(tpl.Rarity); !ok {
			t.Fatalf("模板 %s 的稀有度未登记", tpl.Key)
		}
	}
}

func TestNewDesignValidatesForEverySlot(t *testing.T) {
	for _, slot := range Slots() {
		design := NormalizeDesign(slot.Key, NewDesign(slot.Key))
		if err := ValidateDesign(slot.Key, design); err != nil {
			t.Fatalf("槽位 %s 的空白设计稿校验失败：%v", slot.Key, err)
		}
	}
}

func TestTemplatesAreDeepCopies(t *testing.T) {
	a := Templates()
	a[0].Design.Layers[0].Tracks[0].Keyframes[0].V = 999
	b := Templates()
	if b[0].Design.Layers[0].Tracks[0].Keyframes[0].V == 999 {
		t.Fatal("Templates 返回的设计稿与内部共享了底层数组")
	}
}

func TestValidateDesignRejects(t *testing.T) {
	base := func() Design {
		tpl, _ := FindTemplate("frame_golden_halo")
		return NormalizeDesign(SlotAvatarFrame, tpl.Design)
	}
	cases := map[string]func(d *Design){
		"缺头像区域": func(d *Design) { d.Avatar = nil },
		"非正方形":  func(d *Design) { d.Canvas.Height = 200 },
		"关键帧越界": func(d *Design) { d.Layers[0].Tracks[0].Keyframes[1].T = d.Duration + 1 },
		"重复编号":  func(d *Design) { d.Layers[1].ID = d.Layers[0].ID },
		"非法颜色":  func(d *Design) { d.Layers[0].Stroke = "gold" },
		"未知属性":  func(d *Design) { d.Layers[0].Tracks[0].Property = "blur" },
		"引用缺失素材": func(d *Design) {
			d.Layers[0].Type = LayerImage
			d.Layers[0].Asset = "nope"
			d.Layers[0].Fit = FitContain
		},
		"时长为零有轨道": func(d *Design) {
			d.Duration = 0
			d.Layers[0].Tracks[0].Keyframes = []Keyframe{kf(0, 0, EaseLinear), kf(0, 1, EaseLinear)}
		},
	}
	for name, mutate := range cases {
		d := base()
		mutate(&d)
		if err := ValidateDesign(SlotAvatarFrame, d); err == nil {
			t.Fatalf("%s：应当校验失败", name)
		}
	}
}

func TestNormalizeDropsUnusedAssets(t *testing.T) {
	d := NewDesign(SlotBadge)
	img := baseLayer("img", "", LayerImage, 0, 0, 96, 96)
	img.Asset = "a"
	d.Layers = []Layer{img}
	d.Assets = map[string]AssetRef{"a": {ID: 1, URL: "https://x"}, "b": {ID: 2}}
	n := NormalizeDesign(SlotBadge, d)
	if len(n.Assets) != 1 || n.Assets["a"].ID != 1 || n.Assets["a"].URL != "" {
		t.Fatalf("应只保留被引用的素材且去掉下发字段：%+v", n.Assets)
	}
	if err := ValidateDesign(SlotBadge, n); err != nil {
		t.Fatal(err)
	}
}

func TestEligibility(t *testing.T) {
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	design := NewDesign(SlotAvatarFrame)
	item := Item{ID: 7, Slot: SlotAvatarFrame, Status: StatusActive, Revision: 1, Design: &design, Unlock: Unlock{Mode: UnlockGrant}}
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	check := func(name string, it Item, facts Facts, wantOK bool, wantReason string) {
		t.Helper()
		ok, reason := Eligibility(it, facts, now)
		if ok != wantOK || reason != wantReason {
			t.Fatalf("%s：得到 (%v, %q)，期望 (%v, %q)", name, ok, reason, wantOK, wantReason)
		}
	}
	check("未持有", item, Facts{}, false, ReasonNotOwned)
	check("永久持有", item, Facts{Owned: map[int64]*time.Time{7: nil}}, true, "")
	check("限时有效", item, Facts{Owned: map[int64]*time.Time{7: &future}}, true, "")
	check("限时过期", item, Facts{Owned: map[int64]*time.Time{7: &past}}, false, ReasonNotOwned)

	archived := item
	archived.Status = StatusArchived
	check("已下架即使持有", archived, Facts{Owned: map[int64]*time.Time{7: nil}}, false, ReasonUnavailable)

	vip := item
	vip.Unlock = Unlock{Mode: UnlockVIP, TierIDs: []int64{3}}
	check("非会员", vip, Facts{}, false, ReasonNeedTier)
	check("档位不符", vip, Facts{IsVIP: true, TierIDs: []int64{2}}, false, ReasonNeedTier)
	check("档位相符", vip, Facts{IsVIP: true, TierIDs: []int64{3}}, true, "")

	level := item
	level.Unlock = Unlock{Mode: UnlockLevel, MinLevel: 10}
	check("等级不足", level, Facts{Level: 9}, false, ReasonNeedLevel)
	check("等级达到", level, Facts{Level: 10}, true, "")

	feature := item
	feature.Unlock = Unlock{Mode: UnlockFeature, Feature: "music"}
	check("缺功能", feature, Facts{Features: []string{"ai"}}, false, ReasonNeedFeature)
	check("有功能", feature, Facts{Features: []string{"music"}}, true, "")
}

func TestValidateItemInput(t *testing.T) {
	ok := NormalizeItemInput(ItemInput{Slot: SlotBadge, Code: " Founder_01 ", Name: "元老"})
	if err := ValidateItemInput(ok); err != nil {
		t.Fatal(err)
	}
	if ok.Code != "founder_01" || ok.Rarity != RarityCommon || ok.Unlock.Mode != UnlockGrant {
		t.Fatalf("归一化不符：%+v", ok)
	}
	bad := ok
	bad.Code = "中文"
	if err := ValidateItemInput(bad); err == nil || !strings.Contains(err.Error(), "编码") {
		t.Fatalf("中文编码应被拒绝：%v", err)
	}
	lvl := ok
	lvl.Unlock = Unlock{Mode: UnlockLevel}
	if err := ValidateItemInput(lvl); err == nil {
		t.Fatal("等级解锁未填等级应被拒绝")
	}
}
