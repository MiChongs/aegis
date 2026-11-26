package cosmetic

import "slices"

// 内置模板：只用形状、文字与粒子拼出来（铭牌模板不用文字图层，文字来自 plate）（不依赖任何素材文件），新建装扮时一键套用，
// 三端渲染器的回归样例也用它们——每个模板都必须能过 ValidateDesign（TestTemplatesValidate 钉住）。

// Template 一个设计模板。
type Template struct {
	Key         string `json:"key"`
	Slot        string `json:"slot"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Rarity      string `json:"rarity"`
	Design      Design `json:"design"`
}

func kf(t int, v float64, easing string) Keyframe { return Keyframe{T: t, V: v, Easing: easing} }

func baseLayer(id, name, kind string, x, y, w, h float64) Layer {
	return Layer{ID: id, Name: name, Type: kind, Placement: PlacementAbove, X: x, Y: y, Width: w, Height: h,
		AnchorX: 0.5, AnchorY: 0.5, Scale: 1, Opacity: 1, Blend: BlendNormal}
}

func frameAvatar() *AvatarSlot { return &AvatarSlot{X: 50, Y: 50, Size: 200, Shape: AvatarCircle} }

var templates = []Template{
	{
		Key: "frame_golden_halo", Slot: SlotAvatarFrame, Name: "鎏金光环", Rarity: RarityLegendary,
		Description: "双层金环，外环虚线缓慢旋转，内环呼吸发光",
		Design: Design{Version: DesignVersion, Canvas: Size{300, 300}, Avatar: frameAvatar(), Duration: 6000,
			Layers: []Layer{
				func() Layer {
					l := baseLayer("outer", "外环", LayerShape, 10, 10, 280, 280)
					l.Shape, l.Stroke, l.StrokeWidth, l.Sweep = ShapeRing, "#FFD86B", 6, 360
					l.Dash = []float64{18, 10}
					l.Tracks = []Track{{Property: PropRotation, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(6000, 360, EaseLinear)}}}
					return l
				}(),
				func() Layer {
					l := baseLayer("inner", "内环", LayerShape, 40, 40, 220, 220)
					l.Shape, l.Stroke, l.StrokeWidth, l.Sweep = ShapeRing, "#F5B841", 8, 360
					l.Glow, l.GlowColor = 14, "#CCFFC94D"
					l.Tracks = []Track{{Property: PropOpacity, Keyframes: []Keyframe{kf(0, 0.65, EaseInOut), kf(3000, 1, EaseInOut), kf(6000, 0.65, EaseInOut)}}}
					return l
				}(),
				func() Layer {
					l := baseLayer("crown", "顶饰", LayerShape, 126, 4, 48, 48)
					l.Shape, l.Fill, l.Sides, l.InnerRatio = ShapeStar, "#FFE08A", 5, 0.45
					l.Tracks = []Track{{Property: PropScale, Keyframes: []Keyframe{kf(0, 1, EaseInOut), kf(3000, 1.15, EaseInOut), kf(6000, 1, EaseInOut)}}}
					return l
				}(),
			}},
	},
	{
		Key: "frame_neon_orbit", Slot: SlotAvatarFrame, Name: "霓虹轨道", Rarity: RarityEpic,
		Description: "两道相向旋转的霓虹圆弧，色相持续流转",
		Design: Design{Version: DesignVersion, Canvas: Size{300, 300}, Avatar: frameAvatar(), Duration: 4000,
			Layers: []Layer{
				func() Layer {
					l := baseLayer("arc_a", "圆弧一", LayerShape, 22, 22, 256, 256)
					l.Shape, l.Stroke, l.StrokeWidth, l.Sweep = ShapeRing, "#22D3EE", 7, 220
					l.Glow, l.GlowColor = 12, "#9922D3EE"
					l.Tracks = []Track{
						{Property: PropRotation, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(4000, 360, EaseLinear)}},
						{Property: PropHue, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(4000, 360, EaseLinear)}},
					}
					return l
				}(),
				func() Layer {
					l := baseLayer("arc_b", "圆弧二", LayerShape, 34, 34, 232, 232)
					l.Shape, l.Stroke, l.StrokeWidth, l.Sweep, l.StartAngle = ShapeRing, "#E879F9", 5, 140, 180
					l.Glow, l.GlowColor = 10, "#99E879F9"
					l.Tracks = []Track{{Property: PropRotation, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(4000, -360, EaseLinear)}}}
					return l
				}(),
			}},
	},
	{
		Key: "frame_starlight", Slot: SlotAvatarFrame, Name: "星河流光", Rarity: RarityMythic,
		Description: "细环外沿持续向外飘散星点",
		Design: Design{Version: DesignVersion, Canvas: Size{300, 300}, Avatar: frameAvatar(), Duration: 3000,
			Layers: []Layer{
				func() Layer {
					l := baseLayer("ring", "细环", LayerShape, 44, 44, 212, 212)
					l.Shape, l.Stroke, l.StrokeWidth, l.Sweep = ShapeRing, "#C4B5FD", 4, 360
					return l
				}(),
				func() Layer {
					l := baseLayer("stars", "星点", LayerParticles, 0, 0, 300, 300)
					l.Particles = &Particles{Emitter: EmitterRing, Radius: 108, Shape: ParticleStar,
						Colors: []string{"#FFFFFF", "#C4B5FD", "#93C5FD"}, Count: 36, Lifetime: 2200, Speed: 26,
						Outward: true, Spread: 30, SizeMin: 3, SizeMax: 8, Spin: 90, Fade: true, Shrink: true, Seed: 20261121}
					return l
				}(),
			}},
	},
	{
		Key: "card_aurora_dust", Slot: SlotProfileCard, Name: "极光尘埃", Rarity: RarityEpic,
		Description: "深色底上缓缓上升的光点",
		Design: Design{Version: DesignVersion, Canvas: Size{1080, 608}, Duration: 8000, Background: "#FF0F172A",
			Layers: []Layer{
				func() Layer {
					l := baseLayer("glow", "光晕", LayerShape, 640, 40, 520, 520)
					l.Shape, l.Fill, l.Opacity = ShapeCircle, "#3322D3EE", 0.8
					l.Tracks = []Track{{Property: PropX, Keyframes: []Keyframe{kf(0, 600, EaseInOut), kf(4000, 700, EaseInOut), kf(8000, 600, EaseInOut)}}}
					return l
				}(),
				func() Layer {
					l := baseLayer("dust", "光点", LayerParticles, 0, 0, 1080, 608)
					l.Particles = &Particles{Emitter: EmitterRect, Shape: ParticleCircle,
						Colors: []string{"#A5F3FC", "#FFFFFF", "#C4B5FD"}, Count: 80, Lifetime: 6000, Speed: 30,
						Direction: 0, Spread: 40, SizeMin: 2, SizeMax: 6, Fade: true, Seed: 7}
					return l
				}(),
			}},
	},
	{
		Key: "badge_star", Slot: SlotBadge, Name: "星章", Rarity: RarityRare,
		Description: "缓慢摆动的五角星",
		Design: Design{Version: DesignVersion, Canvas: Size{96, 96}, Duration: 2400,
			Layers: []Layer{
				func() Layer {
					l := baseLayer("disc", "底", LayerShape, 4, 4, 88, 88)
					l.Shape, l.Fill = ShapeCircle, "#FF2563EB"
					return l
				}(),
				func() Layer {
					l := baseLayer("star", "星", LayerShape, 20, 18, 56, 56)
					l.Shape, l.Fill, l.Sides, l.InnerRatio = ShapeStar, "#FFFDE68A", 5, 0.45
					l.Tracks = []Track{{Property: PropRotation, Keyframes: []Keyframe{kf(0, -10, EaseInOut), kf(1200, 10, EaseInOut), kf(2400, -10, EaseInOut)}}}
					return l
				}(),
			}},
	},
	{
		Key: "badge_text", Slot: SlotBadge, Name: "文字章", Rarity: RarityCommon,
		Description: "圆底上的两个字",
		Design: Design{Version: DesignVersion, Canvas: Size{96, 96},
			Layers: []Layer{
				func() Layer {
					l := baseLayer("disc", "底", LayerShape, 4, 4, 88, 88)
					l.Shape, l.Fill, l.Stroke, l.StrokeWidth = ShapeCircle, "#FFF59E0B", "#FFFFFFFF", 4
					return l
				}(),
				func() Layer {
					l := baseLayer("label", "字", LayerText, 8, 28, 80, 40)
					l.Text, l.FontSize, l.FontWeight, l.Color, l.Align = "元老", 30, 800, "#FFFFFFFF", "center"
					return l
				}(),
			}},
	},
	{
		Key: "name_gold_shimmer", Slot: SlotNameStyle, Name: "流金", Rarity: RarityLegendary,
		Description: "金色昵称，高光从左到右扫过",
		Design: Design{Version: DesignVersion, Layers: []Layer{},
			NameStyle: &NameStyle{Color: "#D99A1E", Effect: NameEffectShimmer, Speed: 2400, FontWeight: 700, HighlightColor: "#FFF6D5"}},
	},
	{
		Key: "name_spectrum", Slot: SlotNameStyle, Name: "幻彩", Rarity: RarityEpic,
		Description: "在几种颜色之间循环过渡",
		Design: Design{Version: DesignVersion, Layers: []Layer{},
			NameStyle: &NameStyle{Color: "#8B5CF6", Colors: []string{"#8B5CF6", "#3B82F6", "#10B981", "#F59E0B"},
				Effect: NameEffectCycle, Speed: 4000, FontWeight: 700}},
	},
	{
		Key: "name_neon_glow", Slot: SlotNameStyle, Name: "霓光", Rarity: RarityRare,
		Description: "青色昵称带外发光",
		Design: Design{Version: DesignVersion, Layers: []Layer{},
			NameStyle: &NameStyle{Color: "#06B6D4", Effect: NameEffectGlow, Speed: 2400, FontWeight: 700, GlowColor: "#8006B6D4", GlowRadius: 8}},
	},
}

// plateCapsule 黑金铭牌：黑色胶囊 + 金色描边（垫在文字下）与沿边跑的流光（盖在文字上），中间夹上各自的徽记。
// 胶囊三层横跨伸缩段（x 84–336），随文字长短拉长、圆头不变形；徽记在左段固定不动。
func plateCapsule(duration int, emblems ...Layer) []Layer {
	body := baseLayer("body", "底", LayerShape, 4, 4, 352, 88)
	body.Placement = PlacementBelow
	body.Shape, body.Fill, body.Stroke, body.StrokeWidth, body.CornerRadius = ShapeRect, "#FF111111", "#FFC9A14A", 4, 44
	inner := baseLayer("inner", "内线", LayerShape, 12, 12, 336, 72)
	inner.Placement = PlacementBelow
	inner.Shape, inner.Stroke, inner.StrokeWidth, inner.CornerRadius = ShapeRect, "#66F3D58A", 1.5, 36
	flow := baseLayer("flow", "流光", LayerShape, 4, 4, 352, 88)
	flow.Placement = PlacementAbove
	flow.Shape, flow.Stroke, flow.StrokeWidth, flow.CornerRadius = ShapeRect, "#FFFFE9A8", 4, 44
	flow.Dash = []float64{60, 820}
	flow.Glow, flow.GlowColor = 6, "#CCFFD86B"
	flow.Tracks = []Track{{Property: PropDashOffset, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(duration, -880, EaseLinear)}}}
	layers := append([]Layer{body, inner}, emblems...)
	return append(layers, flow)
}

func init() {
	idEmblem := baseLayer("emblem", "星徽", LayerShape, 26, 26, 44, 44)
	idEmblem.Placement = PlacementBelow
	idEmblem.Shape, idEmblem.Fill, idEmblem.Sides, idEmblem.InnerRatio = ShapeStar, "#FFE7C46A", 5, 0.45
	idEmblem.Glow, idEmblem.GlowColor = 6, "#99FFD86B"
	idEmblem.Tracks = []Track{{Property: PropRotation, Keyframes: []Keyframe{kf(0, 0, EaseLinear), kf(4000, 72, EaseInOut)}}}
	idSparks := baseLayer("sparks", "金屑", LayerParticles, 20, 20, 56, 56)
	idSparks.Placement = PlacementBelow
	idSparks.Particles = &Particles{Emitter: EmitterRing, Radius: 26, Shape: ParticleSpark,
		Colors: []string{"#FFFFE9A8", "#FFE7C46A"}, Count: 10, Lifetime: 1600, Speed: 14, Outward: true,
		SizeMin: 1.5, SizeMax: 3, Fade: true, Shrink: true, Seed: 20261126}
	idLayers := plateCapsule(4000, idEmblem, idSparks)

	vipGem := baseLayer("gem", "钻", LayerShape, 28, 26, 40, 44)
	vipGem.Placement = PlacementBelow
	vipGem.Shape, vipGem.Fill, vipGem.Stroke, vipGem.StrokeWidth, vipGem.Sides = ShapePolygon, "#FFC9A14A", "#FFFFE9A8", 2, 4
	vipGem.Tracks = []Track{{Property: PropScale, Keyframes: []Keyframe{kf(0, 1, EaseInOut), kf(2000, 1.12, EaseInOut), kf(4000, 1, EaseInOut)}}}
	vipDust := baseLayer("dust", "金尘", LayerParticles, 84, 8, 268, 80)
	vipDust.Placement = PlacementBelow
	vipDust.Particles = &Particles{Emitter: EmitterRect, Shape: ParticleStar,
		Colors: []string{"#99FFE9A8", "#66E7C46A"}, Count: 14, Lifetime: 2400, Speed: 8, Direction: 0, Spread: 60,
		SizeMin: 2, SizeMax: 4, Spin: 60, Fade: true, Seed: 1126}
	vipLayers := plateCapsule(4000, vipDust, vipGem)

	goldText := func() *NameStyle {
		return &NameStyle{Color: "#FFE7C46A", Effect: NameEffectShimmer, Speed: 2400, FontWeight: 700, HighlightColor: "#FFFFF6D5"}
	}
	templates = append(templates,
		Template{
			Key: "plate_id_black_gold", Slot: SlotIDPlate, Name: "黑金号牌", Rarity: RarityLegendary,
			Description: "黑色胶囊配金色描边，星徽旋转、流光沿边跑动，号码金色扫光",
			Design: Design{Version: DesignVersion, Canvas: Size{360, 96}, Duration: 4000, Layers: idLayers,
				Plate: &Plate{X: 84, Y: 18, Width: 252, Height: 60, Template: "ID " + PlateValueToken, Align: "center",
					FontSize: 40, FontWeight: 700, LetterSpacing: 0.06, Font: PlateFontMono,
					Stretch: &PlateStretch{X0: 84, X1: 336}},
				NameStyle: goldText()},
		},
		Template{
			Key: "plate_vip_black_gold", Slot: SlotVIPPlate, Name: "黑金尊享", Rarity: RarityLegendary,
			Description: "黑色胶囊配金色描边，钻石呼吸、金尘飘落，「档位 · 有效期」金色扫光",
			Design: Design{Version: DesignVersion, Canvas: Size{360, 96}, Duration: 4000, Layers: vipLayers,
				Plate: &Plate{X: 84, Y: 18, Width: 252, Height: 60, Template: PlateTierToken + " · " + PlateValueToken, Align: "center",
					FontSize: 36, FontWeight: 700, LetterSpacing: 0.04, Font: PlateFontSans,
					Stretch: &PlateStretch{X0: 84, X1: 336}},
				NameStyle: goldText()},
		},
	)
}

// Templates 全部模板（深拷贝，调用方可以随意改）。
func Templates() []Template {
	out := make([]Template, 0, len(templates))
	for _, t := range templates {
		copyT := t
		copyT.Design = cloneDesign(t.Design)
		out = append(out, copyT)
	}
	return out
}

// FindTemplate 按键取模板（深拷贝）。
func FindTemplate(key string) (Template, bool) {
	for _, t := range Templates() {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

func cloneDesign(d Design) Design {
	out := d
	if d.Avatar != nil {
		a := *d.Avatar
		out.Avatar = &a
	}
	if d.NameStyle != nil {
		s := *d.NameStyle
		s.Colors = slices.Clone(d.NameStyle.Colors)
		out.NameStyle = &s
	}
	if d.Plate != nil {
		p := *d.Plate
		if d.Plate.Stretch != nil {
			s := *d.Plate.Stretch
			p.Stretch = &s
		}
		out.Plate = &p
	}
	out.Layers = make([]Layer, 0, len(d.Layers))
	for _, l := range d.Layers {
		c := l
		c.Dash = slices.Clone(l.Dash)
		if l.Particles != nil {
			p := *l.Particles
			p.Colors = slices.Clone(l.Particles.Colors)
			c.Particles = &p
		}
		c.Tracks = make([]Track, 0, len(l.Tracks))
		for _, t := range l.Tracks {
			c.Tracks = append(c.Tracks, Track{Property: t.Property, Keyframes: slices.Clone(t.Keyframes)})
		}
		if len(c.Tracks) == 0 {
			c.Tracks = nil
		}
		out.Layers = append(out.Layers, c)
	}
	if d.Assets != nil {
		out.Assets = make(map[string]AssetRef, len(d.Assets))
		for k, v := range d.Assets {
			out.Assets[k] = v
		}
	}
	return out
}
