package cosmetic

import (
	"testing"
	"time"
)

func workshopFrame() Design {
	d := NewDesign(SlotAvatarFrame)
	ring := baseLayer("ring", "环", LayerImage, 0, 0, 300, 300)
	ring.Asset, ring.Fit = "ring", FitContain
	glow := baseLayer("sp", "流光", LayerSprite, 0, 0, 300, 300)
	glow.Asset, glow.Fit, glow.Columns, glow.Rows, glow.FrameCount, glow.FPS = "glow", FitFill, 4, 4, 16, 12
	d.Layers = []Layer{ring, glow}
	d.Assets = map[string]AssetRef{"ring": {ID: 11}, "glow": {ID: 12}}
	return NormalizeDesign(SlotAvatarFrame, d)
}

func workshopParts() map[int64]Part {
	return map[int64]Part{
		11: {ID: 1, AssetID: 11, Name: "银环", Slots: []string{SlotAvatarFrame, SlotBadge}, Enabled: true},
		12: {ID: 2, AssetID: 12, Name: "流光", Slots: []string{SlotAvatarFrame}, Columns: 4, Rows: 4, FrameCount: 16, FPS: 12, Enabled: true},
	}
}

func TestValidateWorkshopDesign(t *testing.T) {
	if err := ValidateWorkshopDesign(SlotAvatarFrame, workshopFrame(), workshopParts()); err != nil {
		t.Fatalf("合法作品被拒：%v", err)
	}
	cases := map[string]func(d *Design, parts map[int64]Part){
		"非零件素材":   func(d *Design, _ map[int64]Part) { d.Assets["ring"] = AssetRef{ID: 99} },
		"零件已停用":   func(_ *Design, p map[int64]Part) { x := p[11]; x.Enabled = false; p[11] = x },
		"槽位不允许":   func(_ *Design, p map[int64]Part) { x := p[11]; x.Slots = []string{SlotBadge}; p[11] = x },
		"序列帧切分不符": func(d *Design, _ map[int64]Part) { d.Layers[1].FrameCount = 8 },
		"改画布":     func(d *Design, _ map[int64]Part) { d.Canvas = Size{Width: 400, Height: 400} },
		"改头像位置":   func(d *Design, _ map[int64]Part) { d.Avatar.Size = 180 },
		"文字图层": func(d *Design, _ map[int64]Part) {
			l := baseLayer("t", "字", LayerText, 0, 0, 100, 40)
			l.Text, l.FontSize, l.Color = "hi", 20, "#FFFFFF"
			d.Layers = append(d.Layers, l)
		},
		"粒子过多": func(d *Design, _ map[int64]Part) {
			l := baseLayer("p", "粒", LayerParticles, 0, 0, 300, 300)
			l.Particles = &Particles{Emitter: EmitterPoint, Shape: ParticleCircle, Colors: []string{"#FFFFFF"}, Count: 200,
				Lifetime: 1000, SizeMin: 2, SizeMax: 4, Seed: 1}
			d.Layers = append(d.Layers, l)
		},
	}
	for name, mutate := range cases {
		d := workshopFrame()
		parts := workshopParts()
		mutate(&d, parts)
		if err := ValidateWorkshopDesign(SlotAvatarFrame, NormalizeDesign(SlotAvatarFrame, d), parts); err == nil {
			t.Fatalf("%s：应当被拒", name)
		}
	}
	name := NewDesign(SlotNameStyle)
	if err := ValidateWorkshopDesign(SlotNameStyle, NormalizeDesign(SlotNameStyle, name), nil); err != nil {
		t.Fatalf("昵称样式作品被拒：%v", err)
	}
}

func TestWorkshopQuota(t *testing.T) {
	w := WorkshopSettings{Enabled: true, FreeQuota: 1, VIPQuota: 3, TierQuotas: map[string]int{"5": 10}}
	if q := w.QuotaFor(Facts{}); q != 1 {
		t.Fatalf("免费额度 %d", q)
	}
	if q := w.QuotaFor(Facts{IsVIP: true, TierIDs: []int64{1}}); q != 3 {
		t.Fatalf("会员额度 %d", q)
	}
	if q := w.QuotaFor(Facts{IsVIP: true, TierIDs: []int64{1, 5}}); q != 10 {
		t.Fatalf("档位额度 %d", q)
	}
	if q := w.QuotaFor(Facts{TierIDs: []int64{5}}); q != 1 {
		t.Fatalf("会员已过期仍按档位算：%d", q)
	}
	w.Enabled = false
	if q := w.QuotaFor(Facts{IsVIP: true}); q != 0 {
		t.Fatalf("关闭后额度 %d", q)
	}
	if err := ValidateWorkshopSettings(WorkshopSettings{TierQuotas: map[string]int{"x": 1}}); err == nil {
		t.Fatal("档位编号不是数字应当被拒")
	}
}

func TestCreatorEligibility(t *testing.T) {
	d := workshopFrame()
	item := Item{ID: 7, Slot: SlotAvatarFrame, Status: StatusActive, Revision: 1, Design: &d, Origin: OriginWorkshop,
		CreatorUserID: 42, Unlock: Unlock{Mode: UnlockCreator}}
	now := time.Now()
	if ok, _ := Eligibility(item, Facts{UserID: 42, WorkshopQuota: 3}, now); !ok {
		t.Fatal("作者有额度应当能戴")
	}
	if ok, reason := Eligibility(item, Facts{UserID: 42}, now); ok || reason != ReasonNeedWorkshop {
		t.Fatalf("额度为零应当戴不上：%v %s", ok, reason)
	}
	if ok, reason := Eligibility(item, Facts{UserID: 43, WorkshopQuota: 3}, now); ok || reason != ReasonNotOwned {
		t.Fatalf("别人不能戴：%v %s", ok, reason)
	}
	item.Status = StatusArchived
	if ok, _ := Eligibility(item, Facts{UserID: 42, WorkshopQuota: 3}, now); ok {
		t.Fatal("被下架的作品不能戴")
	}
}
