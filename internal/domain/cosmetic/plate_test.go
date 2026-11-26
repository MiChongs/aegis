package cosmetic

import (
	"encoding/json"
	"strings"
	"testing"
)

func plateDesign(t *testing.T, slot string) Design {
	t.Helper()
	for _, tpl := range Templates() {
		if tpl.Slot == slot {
			return NormalizeDesign(slot, tpl.Design)
		}
	}
	t.Fatalf("槽位 %s 没有内置模板", slot)
	return Design{}
}

func TestPlateSlots(t *testing.T) {
	for _, slot := range []string{SlotIDPlate, SlotVIPPlate} {
		spec, ok := FindSlot(slot)
		if !ok || !spec.Plate || !spec.Layered || spec.Square || spec.Multiple || spec.Workshop {
			t.Fatalf("%s 的槽位自述不对：%+v", slot, spec)
		}
		if spec.Canvas != (Size{Width: 360, Height: 96}) || spec.MinCanvas != 32 || spec.MaxCanvas != 1024 {
			t.Fatalf("%s 的画布预设不对：%+v", slot, spec)
		}
		if !IsPlateSlot(slot) {
			t.Fatalf("IsPlateSlot(%s) 应为真", slot)
		}
	}
	if IsPlateSlot(SlotNameStyle) || IsPlateSlot("nope") {
		t.Fatal("非铭牌槽位被当成铭牌")
	}
	for _, spec := range WorkshopSlots() {
		if spec.Plate {
			t.Fatalf("工坊槽位里出现了铭牌：%s", spec.Key)
		}
	}
	if len(WorkshopSlots()) != 4 {
		t.Fatalf("工坊应有 4 个槽位，得到 %d", len(WorkshopSlots()))
	}
	v := DesignVocabulary()
	if strings.Join(v.PlateAligns, ",") != "left,center,right" || strings.Join(v.PlateFonts, ",") != "sans,serif,mono" ||
		v.Limits.MaxPlateTemplateRunes != 32 {
		t.Fatalf("规格枚举里缺铭牌：%+v %+v", v.PlateAligns, v.PlateFonts)
	}
}

func TestPlateNormalize(t *testing.T) {
	d := NewDesign(SlotVIPPlate)
	d.Plate.Align, d.Plate.Font, d.Plate.FontWeight, d.Plate.Template = "", "", 0, "  {value}  "
	d.NameStyle = &NameStyle{Color: "#FFFFFFFF"}
	down := baseLayer("bg", "底", LayerShape, 0, 0, 360, 96)
	down.Placement = ""
	down.Shape, down.Fill = ShapeRect, "#FF000000"
	up := baseLayer("up", "盖", LayerShape, 0, 0, 360, 96)
	up.Placement = PlacementAbove
	up.Shape, up.Fill = ShapeRect, "#33FFFFFF"
	d.Layers = []Layer{down, up}
	n := NormalizeDesign(SlotVIPPlate, d)
	if n.Layers[0].Placement != PlacementBelow {
		t.Fatalf("铭牌里缺省 placement 应为 below，得到 %q", n.Layers[0].Placement)
	}
	if n.Layers[1].Placement != PlacementAbove {
		t.Fatal("显式的 above 不应被改")
	}
	if n.Plate.Align != "center" || n.Plate.Font != PlateFontSans || n.Plate.FontWeight != 700 || n.Plate.Template != PlateValueToken {
		t.Fatalf("plate 缺省值不对：%+v", n.Plate)
	}
	if n.NameStyle == nil || n.NameStyle.Effect != NameEffectNone || n.NameStyle.FontWeight != 700 || n.NameStyle.Speed != 2400 {
		t.Fatalf("铭牌的 nameStyle 应被规范化且保留：%+v", n.NameStyle)
	}
	if n.Avatar != nil {
		t.Fatal("铭牌不应有头像区域")
	}
	if err := ValidateDesign(SlotVIPPlate, n); err != nil {
		t.Fatal(err)
	}
	// 其他槽位的 placement 缺省仍为 above
	b := NewDesign(SlotBadge)
	bl := baseLayer("x", "", LayerShape, 0, 0, 96, 96)
	bl.Placement, bl.Shape, bl.Fill = "", ShapeCircle, "#FF000000"
	b.Layers = []Layer{bl}
	if got := NormalizeDesign(SlotBadge, b).Layers[0].Placement; got != PlacementAbove {
		t.Fatalf("勋章的缺省 placement 应为 above，得到 %q", got)
	}
}

func TestPlateValidateAccepts(t *testing.T) {
	cases := map[string]func(d *Design){
		"左对齐衬线":   func(d *Design) { d.Plate.Align, d.Plate.Font = "left", PlateFontSerif },
		"右对齐等宽":   func(d *Design) { d.Plate.Align, d.Plate.Font = "right", PlateFontMono },
		"细字重负字距":  func(d *Design) { d.Plate.FontWeight, d.Plate.LetterSpacing = 100, -0.2 },
		"粗字重宽字距":  func(d *Design) { d.Plate.FontWeight, d.Plate.LetterSpacing = 900, 1 },
		"模板 32 字": func(d *Design) { d.Plate.Template = strings.Repeat("号", 25) + PlateValueToken },
		"区域贴满画布":  func(d *Design) { d.Plate.X, d.Plate.Y, d.Plate.Width, d.Plate.Height = 0, 0, 360, 96 },
		"竖长画布": func(d *Design) {
			d.Canvas = Size{Width: 20, Height: 1024}
			d.Plate.X, d.Plate.Y, d.Plate.Width, d.Plate.Height = 0, 0, 20, 1024
			d.Plate.Stretch = &PlateStretch{X0: 0, X1: 20}
			d.Layers = nil
		},
		"无伸缩段":    func(d *Design) { d.Plate.Stretch = nil },
		"伸缩段贴满画布": func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: 0, X1: 360} },
		"伸缩段一像素":  func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: 100, X1: 101} },
		"循环变色": func(d *Design) {
			d.NameStyle.Effect, d.NameStyle.Colors = NameEffectCycle, []string{"#FFFFE9A8", "#FFC9A14A"}
		},
	}
	for name, mutate := range cases {
		for _, slot := range []string{SlotIDPlate, SlotVIPPlate} {
			d := plateDesign(t, slot)
			mutate(&d)
			if err := ValidateDesign(slot, d); err != nil {
				t.Fatalf("%s / %s：不应被拒：%v", slot, name, err)
			}
		}
	}
}

func TestPlateValidateRejects(t *testing.T) {
	cases := map[string]func(d *Design){
		"缺 plate":      func(d *Design) { d.Plate = nil },
		"缺 nameStyle":  func(d *Design) { d.NameStyle = nil },
		"nameStyle 非法": func(d *Design) { d.NameStyle.Color = "gold" },
		"nameStyle 周期": func(d *Design) { d.NameStyle.Speed = 10 },
		"模板为空":         func(d *Design) { d.Plate.Template = "" },
		"模板无占位":        func(d *Design) { d.Plate.Template = "ID" },
		"模板两个占位":       func(d *Design) { d.Plate.Template = "{value}{value}" },
		"模板超长":         func(d *Design) { d.Plate.Template = strings.Repeat("号", 26) + PlateValueToken },
		"区域越右":         func(d *Design) { d.Plate.X = 200 },
		"区域越下":         func(d *Design) { d.Plate.Height = 90 },
		"区域负坐标":        func(d *Design) { d.Plate.X = -1 },
		"区域零宽":         func(d *Design) { d.Plate.Width = 0 },
		"未知对齐":         func(d *Design) { d.Plate.Align = "justify" },
		"未知字体":         func(d *Design) { d.Plate.Font = "cursive" },
		"字重过小":         func(d *Design) { d.Plate.FontWeight = 50 },
		"字重过大":         func(d *Design) { d.Plate.FontWeight = 1000 },
		"字距过小":         func(d *Design) { d.Plate.LetterSpacing = -0.3 },
		"字距过大":         func(d *Design) { d.Plate.LetterSpacing = 1.5 },
		"字号为零":         func(d *Design) { d.Plate.FontSize = 0 },
		"字号为负":         func(d *Design) { d.Plate.FontSize = -4 },
		"最长边过短": func(d *Design) {
			d.Canvas = Size{Width: 31, Height: 20}
			d.Layers = nil
			d.Plate.X, d.Plate.Y, d.Plate.Width, d.Plate.Height = 0, 0, 10, 10
			d.Plate.Stretch = nil
		},
		"最长边过长":  func(d *Design) { d.Canvas = Size{Width: 1025, Height: 96} },
		"伸缩段倒置":  func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: 300, X1: 120} },
		"伸缩段为零":  func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: 120, X1: 120} },
		"伸缩段负起点": func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: -1, X1: 120} },
		"伸缩段越右":  func(d *Design) { d.Plate.Stretch = &PlateStretch{X0: 120, X1: 361} },
	}
	for name, mutate := range cases {
		for _, slot := range []string{SlotIDPlate, SlotVIPPlate} {
			d := plateDesign(t, slot)
			mutate(&d)
			if err := ValidateDesign(slot, d); err == nil {
				t.Fatalf("%s / %s：应当校验失败", slot, name)
			}
		}
	}
}

func TestPlateTierToken(t *testing.T) {
	cases := []struct {
		slot, template string
		ok             bool
	}{
		{SlotVIPPlate, "{tier} · {value}", true},
		{SlotVIPPlate, "{tier}", true},
		{SlotVIPPlate, "{value} 到期", true},
		{SlotVIPPlate, "{value}{tier}", true},
		{SlotVIPPlate, "会员", false},
		{SlotVIPPlate, "{tier}{tier}", false},
		{SlotVIPPlate, "{value}{value}", false},
		{SlotVIPPlate, "{tier} {value} {tier}", false},
		{SlotIDPlate, "ID {value}", true},
		{SlotIDPlate, "{tier} {value}", false},
		{SlotIDPlate, "{tier}", false},
	}
	for _, c := range cases {
		d := plateDesign(t, c.slot)
		d.Plate.Template = c.template
		if err := ValidateDesign(c.slot, d); (err == nil) != c.ok {
			t.Fatalf("%s 模板 %q：期望通过=%v，得到 %v", c.slot, c.template, c.ok, err)
		}
	}
	if tpl, _ := FindTemplate("plate_vip_black_gold"); tpl.Design.Plate.Template != "{tier} · {value}" {
		t.Fatalf("内置会员铭牌模板应为「{tier} · {value}」，得到 %q", tpl.Design.Plate.Template)
	}
}

func TestPlateOnlyOnPlateSlots(t *testing.T) {
	for _, slot := range []string{SlotAvatarFrame, SlotProfileCard, SlotBadge, SlotNameStyle} {
		d := NewDesign(slot)
		d.Plate = &Plate{Width: 10, Height: 10, Template: PlateValueToken, Align: "center", FontSize: 8, FontWeight: 700, Font: PlateFontSans}
		n := NormalizeDesign(slot, d)
		if err := ValidateDesign(slot, n); err == nil || !strings.Contains(err.Error(), "plate") {
			t.Fatalf("%s 带了 plate 应被拒：%v", slot, err)
		}
	}
}

func TestPlateRoundTripAndClone(t *testing.T) {
	d := plateDesign(t, SlotIDPlate)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	want := `"plate":{"x":84,"y":18,"width":252,"height":60,"template":"ID {value}","align":"center","fontSize":40,"fontWeight":700,"letterSpacing":0.06,"font":"mono","stretch":{"x0":84,"x1":336}}`
	if !strings.Contains(string(raw), want) {
		t.Fatalf("plate 的 JSON 形状不符合契约：%s", raw)
	}
	var back Design
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !EqualDesign(&d, &back) {
		t.Fatal("往返之后设计稿不相等")
	}
	changed := back
	p := *back.Plate
	p.Template = "No." + PlateValueToken
	changed.Plate = &p
	if EqualDesign(&d, &changed) {
		t.Fatal("改了 plate 却判为相等")
	}
	a := Templates()
	for i := range a {
		if a[i].Design.Plate != nil {
			a[i].Design.Plate.Template = "x"
			a[i].Design.Plate.Stretch.X1 = 1
		}
	}
	for _, tpl := range Templates() {
		if p := tpl.Design.Plate; p != nil && (p.Template == "x" || p.Stretch == nil || p.Stretch.X1 == 1) {
			t.Fatal("Templates 返回的 plate 与内部共享了指针，或铭牌模板缺伸缩段")
		}
	}
}

func TestWorkshopRejectsPlates(t *testing.T) {
	for _, slot := range []string{SlotIDPlate, SlotVIPPlate} {
		d := NormalizeDesign(slot, NewDesign(slot))
		if err := ValidateWorkshopDesign(slot, d, nil); err == nil || !strings.Contains(err.Error(), "工坊") {
			t.Fatalf("工坊应拒绝 %s：%v", slot, err)
		}
		in := NormalizePartInput(PartInput{AssetID: 1, Name: "零件", Slots: []string{slot}})
		if err := ValidatePartInput(in, Asset{Kind: AssetImage}); err == nil {
			t.Fatalf("零件不应允许用在 %s 上", slot)
		}
	}
}
