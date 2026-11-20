package cosmetic

import "slices"

// 内置模板：只用形状、文字与粒子拼出来（不依赖任何素材文件），新建装扮时一键套用，
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
