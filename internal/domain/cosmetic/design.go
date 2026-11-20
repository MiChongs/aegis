package cosmetic

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ── 设计稿（动态资源规格 v1） ──
//
// 控制台设计器产出、服务端校验并下发、客户端逐帧绘制的同一份 JSON。规格写在 docs/cosmetics.md「设计稿」，
// 三端（Go 校验、控制台 TypeScript 渲染、Android Compose 渲染）对同一份语义负责：
//
//	坐标     画布左上角为原点、单位是画布像素；客户端按「实际显示尺寸 / 画布宽」等比缩放
//	图层     数组顺序即绘制顺序（先画的在下面）；placement=below 的图层画在头像之下，其余在头像之上
//	变换     先以锚点（anchorX/anchorY，相对图层自身 0–1）为中心缩放、旋转，再平移到 (x, y)
//	动画     每个图层若干条轨道，每条轨道改一个属性；关键帧时刻 t 单位毫秒、落在 [0, duration] 内，
//	         整份设计稿按 duration 循环；轨道值与静态值是「覆盖」关系（轨道存在时静态值只作为第 0 帧之前的值）
//	素材     图层的 asset 是 assets 表里的键；存库时只记素材编号，下发时补上地址与尺寸

// 规格版本。
const DesignVersion = 1

// 图层类型。
const (
	LayerImage     = "image"     // 静态图（PNG / WEBP / SVG / GIF 首帧）
	LayerSprite    = "sprite"    // 序列帧：一张网格图，按 fps 逐格播放
	LayerText      = "text"      // 文字
	LayerShape     = "shape"     // 矢量形状
	LayerParticles = "particles" // 粒子发射器
)

// 形状。
const (
	ShapeRect    = "rect"
	ShapeCircle  = "circle"
	ShapeRing    = "ring"
	ShapeStar    = "star"
	ShapePolygon = "polygon"
)

// 头像形状。
const (
	AvatarCircle  = "circle"
	AvatarRounded = "rounded"
	AvatarSquare  = "square"
)

// 图层相对头像的位置。
const (
	PlacementAbove = "above"
	PlacementBelow = "below"
)

// 混合模式（三端都有对应实现的那几种）。
const (
	BlendNormal   = "normal"
	BlendScreen   = "screen"
	BlendMultiply = "multiply"
	BlendAdd      = "add"
	BlendOverlay  = "overlay"
)

// 图片适配。
const (
	FitFill    = "fill"
	FitContain = "contain"
	FitCover   = "cover"
)

// 动画属性。
const (
	PropX        = "x"
	PropY        = "y"
	PropScale    = "scale"
	PropScaleX   = "scaleX"
	PropScaleY   = "scaleY"
	PropRotation = "rotation"
	PropOpacity  = "opacity"
	// PropHue 色相偏移（度），作用在整个图层上
	PropHue = "hue"
	// PropSweep 圆环 / 圆弧的扫过角度（度）
	PropSweep = "sweep"
	// PropDashOffset 虚线描边的偏移（流光沿边框跑动）
	PropDashOffset = "dashOffset"
)

// 缓动。
const (
	EaseLinear    = "linear"
	EaseIn        = "easeIn"
	EaseOut       = "easeOut"
	EaseInOut     = "easeInOut"
	EaseStep      = "step"
	EaseBackOut   = "backOut"
	EaseElasticIn = "elastic"
)

// 粒子发射器形状。
const (
	EmitterPoint = "point"
	EmitterRing  = "ring"
	EmitterRect  = "rect"
)

// 粒子形状。
const (
	ParticleCircle = "circle"
	ParticleStar   = "star"
	ParticleSpark  = "spark"
	ParticleImage  = "image"
)

// 昵称效果。
const (
	NameEffectNone    = "none"
	NameEffectShimmer = "shimmer" // 一道高光从左到右扫过
	NameEffectPulse   = "pulse"   // 亮度呼吸
	NameEffectCycle   = "cycle"   // 在几种颜色间循环过渡
	NameEffectGlow    = "glow"    // 外发光
	NameEffectWave    = "wave"    // 逐字上下起伏
)

// 素材类型。
const (
	AssetImage  = "image"
	AssetSprite = "sprite"
)

// 上界。
const (
	MaxLayers        = 40
	MaxTracks        = 8
	MaxKeyframes     = 64
	MaxDurationMs    = 20_000
	MaxParticles     = 240
	MaxTextRunes     = 32
	MaxLayerIDLen    = 32
	MaxLayerNameLen  = 32
	MaxAssetsPerItem = 32
	MaxSpriteFrames  = 256
	MaxNameColors    = 6
	MaxDashItems     = 8
)

// Size 画布尺寸。
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// AvatarSlot 头像在画布里的位置（左上角 + 边长）。
type AvatarSlot struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Size float64 `json:"size"`
	// Shape circle / rounded / square；Radius 是 rounded 时圆角占边长的比例（0–0.5）
	Shape  string  `json:"shape"`
	Radius float64 `json:"radius,omitempty"`
}

// Design 一份设计稿。
type Design struct {
	Version int  `json:"version"`
	Canvas  Size `json:"canvas"`
	// Avatar 头像框专有
	Avatar *AvatarSlot `json:"avatar,omitempty"`
	// Duration 一个循环多少毫秒；0 为静态（轨道不生效，序列帧与粒子仍按自己的节奏动）
	Duration int `json:"duration"`
	// Background 画布底色（资料卡用；头像框、勋章应为空即透明）
	Background string  `json:"background,omitempty"`
	Layers     []Layer `json:"layers"`
	// NameStyle 昵称样式专有
	NameStyle *NameStyle `json:"nameStyle,omitempty"`
	// Assets 图层引用的素材：键是图层里写的 asset
	Assets map[string]AssetRef `json:"assets,omitempty"`
}

// AssetRef 设计稿里的一个素材引用。ID 存库；URL / 尺寸在下发时补上。
type AssetRef struct {
	ID          int64  `json:"id"`
	URL         string `json:"url,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

// Layer 一个图层。不同类型只用到其中一部分字段，其余为零值（存库前会被抹掉）。
type Layer struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Type      string  `json:"type"`
	Hidden    bool    `json:"hidden,omitempty"`
	Placement string  `json:"placement,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	AnchorX   float64 `json:"anchorX"`
	AnchorY   float64 `json:"anchorY"`
	Rotation  float64 `json:"rotation,omitempty"`
	Scale     float64 `json:"scale"`
	Opacity   float64 `json:"opacity"`
	Blend     string  `json:"blend,omitempty"`

	// image / sprite
	Asset string `json:"asset,omitempty"`
	Fit   string `json:"fit,omitempty"`
	// Tint 把图片染成这个颜色（保留透明度）；为空不染
	Tint string `json:"tint,omitempty"`
	// sprite：网格列数、行数、有效帧数、每秒帧数
	Columns    int     `json:"columns,omitempty"`
	Rows       int     `json:"rows,omitempty"`
	FrameCount int     `json:"frameCount,omitempty"`
	FPS        float64 `json:"fps,omitempty"`

	// text
	Text          string  `json:"text,omitempty"`
	FontSize      float64 `json:"fontSize,omitempty"`
	FontWeight    int     `json:"fontWeight,omitempty"`
	Color         string  `json:"color,omitempty"`
	Align         string  `json:"align,omitempty"`
	LetterSpacing float64 `json:"letterSpacing,omitempty"`

	// shape
	Shape        string  `json:"shape,omitempty"`
	Fill         string  `json:"fill,omitempty"`
	Stroke       string  `json:"stroke,omitempty"`
	StrokeWidth  float64 `json:"strokeWidth,omitempty"`
	CornerRadius float64 `json:"cornerRadius,omitempty"`
	// Sides 多边形边数 / 星形角数
	Sides int `json:"sides,omitempty"`
	// InnerRatio 星形内径占外径的比例
	InnerRatio float64 `json:"innerRatio,omitempty"`
	// Sweep 圆环扫过的角度（度，0–360），从 12 点方向顺时针；StartAngle 起点偏移
	Sweep      float64   `json:"sweep,omitempty"`
	StartAngle float64   `json:"startAngle,omitempty"`
	Dash       []float64 `json:"dash,omitempty"`
	DashOffset float64   `json:"dashOffset,omitempty"`
	// Glow 外发光半径（画布像素）与颜色；文字与形状可用
	Glow      float64 `json:"glow,omitempty"`
	GlowColor string  `json:"glowColor,omitempty"`

	// particles
	Particles *Particles `json:"particles,omitempty"`

	Tracks []Track `json:"tracks,omitempty"`
}

// Particles 粒子发射器。粒子由客户端按 seed 确定性地生成：同一份设计稿在三端看起来一致。
type Particles struct {
	Emitter string `json:"emitter"`
	// 发射区域：ring 用 Radius（以图层中心为圆心），rect 用图层的宽高，point 是图层中心
	Radius float64 `json:"radius,omitempty"`
	Shape  string  `json:"shape"`
	Asset  string  `json:"asset,omitempty"`
	// Colors 每个粒子从中取一种
	Colors []string `json:"colors,omitempty"`
	Count  int      `json:"count"`
	// Lifetime 每个粒子活多少毫秒；粒子在一个生命周期内均匀错开出生，形成连续的流
	Lifetime int `json:"lifetime"`
	// Speed 初速度（画布像素 / 秒）；Direction 发射方向（度，0 为向上）；Spread 散开的角度
	Speed     float64 `json:"speed"`
	Direction float64 `json:"direction"`
	Spread    float64 `json:"spread"`
	// Outward 环形发射时沿半径向外（忽略 Direction）
	Outward bool    `json:"outward,omitempty"`
	Gravity float64 `json:"gravity,omitempty"`
	SizeMin float64 `json:"sizeMin"`
	SizeMax float64 `json:"sizeMax"`
	// Spin 自转速度（度 / 秒）
	Spin float64 `json:"spin,omitempty"`
	// Fade 随生命周期淡出；Shrink 随生命周期缩小
	Fade   bool  `json:"fade,omitempty"`
	Shrink bool  `json:"shrink,omitempty"`
	Seed   int64 `json:"seed"`
}

// Track 一条动画轨道。
type Track struct {
	Property  string     `json:"property"`
	Keyframes []Keyframe `json:"keyframes"`
}

// Keyframe 关键帧：t 毫秒时取值 v，从上一帧到这一帧用 easing 过渡。
type Keyframe struct {
	T      int     `json:"t"`
	V      float64 `json:"v"`
	Easing string  `json:"easing,omitempty"`
}

// NameStyle 昵称样式。
type NameStyle struct {
	// Color 主色；Colors cycle 效果循环的几种颜色
	Color  string   `json:"color"`
	Colors []string `json:"colors,omitempty"`
	Effect string   `json:"effect"`
	// Speed 效果一个周期多少毫秒
	Speed      int     `json:"speed,omitempty"`
	FontWeight int     `json:"fontWeight,omitempty"`
	GlowColor  string  `json:"glowColor,omitempty"`
	GlowRadius float64 `json:"glowRadius,omitempty"`
	// HighlightColor shimmer 的高光颜色
	HighlightColor string `json:"highlightColor,omitempty"`
}

var (
	layerIDPattern = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)
	colorPattern   = regexp.MustCompile(`^#([0-9A-Fa-f]{6}|[0-9A-Fa-f]{8})$`)
)

var (
	layerTypes   = []string{LayerImage, LayerSprite, LayerText, LayerShape, LayerParticles}
	shapeTypes   = []string{ShapeRect, ShapeCircle, ShapeRing, ShapeStar, ShapePolygon}
	avatarShapes = []string{AvatarCircle, AvatarRounded, AvatarSquare}
	placements   = []string{PlacementAbove, PlacementBelow}
	blends       = []string{BlendNormal, BlendScreen, BlendMultiply, BlendAdd, BlendOverlay}
	fits         = []string{FitFill, FitContain, FitCover}
	easings      = []string{EaseLinear, EaseIn, EaseOut, EaseInOut, EaseStep, EaseBackOut, EaseElasticIn}
	emitters     = []string{EmitterPoint, EmitterRing, EmitterRect}
	particleKind = []string{ParticleCircle, ParticleStar, ParticleSpark, ParticleImage}
	nameEffects  = []string{NameEffectNone, NameEffectShimmer, NameEffectPulse, NameEffectCycle, NameEffectGlow, NameEffectWave}
	aligns       = []string{"left", "center", "right"}
)

// propertyRanges 每个可动画属性的取值范围。
var propertyRanges = map[string][2]float64{
	PropX:          {-4096, 8192},
	PropY:          {-4096, 8192},
	PropScale:      {0, 20},
	PropScaleX:     {0, 20},
	PropScaleY:     {0, 20},
	PropRotation:   {-36000, 36000},
	PropOpacity:    {0, 1},
	PropHue:        {-3600, 3600},
	PropSweep:      {0, 360},
	PropDashOffset: {-100000, 100000},
}

// Vocabulary 规格里的全部枚举，控制台设计器的下拉框读它（与校验同一份，不会出现「选得到却存不进去」）。
type Vocabulary struct {
	Version      int      `json:"version"`
	LayerTypes   []string `json:"layerTypes"`
	Shapes       []string `json:"shapes"`
	AvatarShapes []string `json:"avatarShapes"`
	Placements   []string `json:"placements"`
	Blends       []string `json:"blends"`
	Fits         []string `json:"fits"`
	Easings      []string `json:"easings"`
	Emitters     []string `json:"emitters"`
	Particles    []string `json:"particles"`
	NameEffects  []string `json:"nameEffects"`
	Aligns       []string `json:"aligns"`
	Properties   []string `json:"properties"`
	Limits       Limits   `json:"limits"`
}

// Limits 规格上界。
type Limits struct {
	MaxLayers        int `json:"maxLayers"`
	MaxTracks        int `json:"maxTracks"`
	MaxKeyframes     int `json:"maxKeyframes"`
	MaxDurationMs    int `json:"maxDurationMs"`
	MaxParticles     int `json:"maxParticles"`
	MaxTextRunes     int `json:"maxTextRunes"`
	MaxAssetsPerItem int `json:"maxAssetsPerItem"`
	MaxSpriteFrames  int `json:"maxSpriteFrames"`
	MaxAssetBytes    int `json:"maxAssetBytes"`
	MaxPreviewBytes  int `json:"maxPreviewBytes"`
	MaxBadgeSlots    int `json:"maxBadgeSlots"`
	MaxGrantDays     int `json:"maxGrantDays"`
}

// DesignVocabulary 规格枚举（副本）。
func DesignVocabulary() Vocabulary {
	props := make([]string, 0, len(propertyRanges))
	for key := range propertyRanges {
		props = append(props, key)
	}
	slices.Sort(props)
	return Vocabulary{
		Version: DesignVersion, LayerTypes: slices.Clone(layerTypes), Shapes: slices.Clone(shapeTypes),
		AvatarShapes: slices.Clone(avatarShapes), Placements: slices.Clone(placements), Blends: slices.Clone(blends),
		Fits: slices.Clone(fits), Easings: slices.Clone(easings), Emitters: slices.Clone(emitters),
		Particles: slices.Clone(particleKind), NameEffects: slices.Clone(nameEffects), Aligns: slices.Clone(aligns),
		Properties: props,
		Limits: Limits{
			MaxLayers: MaxLayers, MaxTracks: MaxTracks, MaxKeyframes: MaxKeyframes, MaxDurationMs: MaxDurationMs,
			MaxParticles: MaxParticles, MaxTextRunes: MaxTextRunes, MaxAssetsPerItem: MaxAssetsPerItem,
			MaxSpriteFrames: MaxSpriteFrames, MaxAssetBytes: MaxAssetBytes, MaxPreviewBytes: MaxPreviewBytes,
			MaxBadgeSlots: MaxBadgeSlots, MaxGrantDays: MaxGrantDays,
		},
	}
}

// NewDesign 一个槽位的空白设计稿。
func NewDesign(slot string) Design {
	spec, _ := FindSlot(slot)
	design := Design{Version: DesignVersion, Canvas: spec.Canvas, Layers: []Layer{}}
	if spec.Avatar != nil {
		avatar := *spec.Avatar
		design.Avatar = &avatar
	}
	if slot == SlotNameStyle {
		design.Canvas = Size{}
		design.NameStyle = &NameStyle{Color: "#3B82F6", Effect: NameEffectNone, FontWeight: 600, Speed: 2400}
	}
	return design
}

// NormalizeDesign 补默认值、抹掉与图层类型无关的字段、关键帧按时刻排序。返回新值，不改入参。
func NormalizeDesign(slot string, in Design) Design {
	out := in
	out.Version = DesignVersion
	out.Background = strings.TrimSpace(out.Background)
	if slot == SlotNameStyle {
		out.Canvas = Size{}
		out.Avatar = nil
		out.Layers = []Layer{}
		out.Duration = 0
		out.Background = ""
		out.Assets = nil
		if out.NameStyle != nil {
			style := *out.NameStyle
			style.Effect = strings.TrimSpace(style.Effect)
			if style.Effect == "" {
				style.Effect = NameEffectNone
			}
			if style.FontWeight == 0 {
				style.FontWeight = 600
			}
			if style.Speed == 0 {
				style.Speed = 2400
			}
			if style.Effect != NameEffectCycle {
				style.Colors = nil
			}
			out.NameStyle = &style
		}
		return out
	}
	out.NameStyle = nil
	if slot != SlotAvatarFrame {
		out.Avatar = nil
	} else if out.Avatar != nil {
		avatar := *out.Avatar
		if avatar.Shape == "" {
			avatar.Shape = AvatarCircle
		}
		if avatar.Shape != AvatarRounded {
			avatar.Radius = 0
		}
		out.Avatar = &avatar
	}
	layers := make([]Layer, 0, len(in.Layers))
	for _, layer := range in.Layers {
		layers = append(layers, normalizeLayer(layer))
	}
	out.Layers = layers
	// 只留被引用到的素材：删掉图层后素材表里的残留不该继续占着「被引用」
	used := out.UsedAssetKeys()
	if len(in.Assets) > 0 {
		assets := make(map[string]AssetRef, len(used))
		for key, ref := range in.Assets {
			if slices.Contains(used, key) {
				assets[key] = AssetRef{ID: ref.ID}
			}
		}
		out.Assets = assets
	}
	if len(out.Assets) == 0 {
		out.Assets = nil
	}
	return out
}

func normalizeLayer(in Layer) Layer {
	l := Layer{
		ID: strings.TrimSpace(in.ID), Name: strings.TrimSpace(in.Name), Type: strings.TrimSpace(in.Type),
		Hidden: in.Hidden, Placement: in.Placement, X: in.X, Y: in.Y, Width: in.Width, Height: in.Height,
		AnchorX: in.AnchorX, AnchorY: in.AnchorY, Rotation: in.Rotation, Scale: in.Scale, Opacity: in.Opacity,
		Blend: in.Blend, Glow: in.Glow, GlowColor: in.GlowColor,
	}
	if l.Placement == "" {
		l.Placement = PlacementAbove
	}
	if l.Blend == "" {
		l.Blend = BlendNormal
	}
	if l.Glow <= 0 {
		l.Glow, l.GlowColor = 0, ""
	}
	switch l.Type {
	case LayerImage:
		l.Asset, l.Fit, l.Tint = strings.TrimSpace(in.Asset), in.Fit, in.Tint
		if l.Fit == "" {
			l.Fit = FitContain
		}
		l.Glow, l.GlowColor = 0, ""
	case LayerSprite:
		l.Asset, l.Fit, l.Tint = strings.TrimSpace(in.Asset), in.Fit, in.Tint
		if l.Fit == "" {
			l.Fit = FitContain
		}
		l.Columns, l.Rows, l.FrameCount, l.FPS = in.Columns, in.Rows, in.FrameCount, in.FPS
		if l.FrameCount == 0 {
			l.FrameCount = l.Columns * l.Rows
		}
		l.Glow, l.GlowColor = 0, ""
	case LayerText:
		l.Text, l.FontSize, l.FontWeight, l.Color = in.Text, in.FontSize, in.FontWeight, in.Color
		l.Align, l.LetterSpacing, l.Stroke, l.StrokeWidth = in.Align, in.LetterSpacing, in.Stroke, in.StrokeWidth
		if l.Align == "" {
			l.Align = "center"
		}
		if l.FontWeight == 0 {
			l.FontWeight = 600
		}
		if l.StrokeWidth <= 0 {
			l.Stroke, l.StrokeWidth = "", 0
		}
	case LayerShape:
		l.Shape, l.Fill, l.Stroke, l.StrokeWidth = in.Shape, in.Fill, in.Stroke, in.StrokeWidth
		l.CornerRadius, l.Sides, l.InnerRatio = in.CornerRadius, in.Sides, in.InnerRatio
		l.Sweep, l.StartAngle, l.Dash, l.DashOffset = in.Sweep, in.StartAngle, in.Dash, in.DashOffset
		if l.Shape == ShapeRing && l.Sweep == 0 {
			l.Sweep = 360
		}
		if l.Shape != ShapeRing {
			l.Sweep, l.StartAngle = 0, 0
		}
		if l.Shape != ShapeRect {
			l.CornerRadius = 0
		}
		if l.Shape != ShapeStar && l.Shape != ShapePolygon {
			l.Sides = 0
		}
		if l.Shape != ShapeStar {
			l.InnerRatio = 0
		} else if l.InnerRatio == 0 {
			l.InnerRatio = 0.5
		}
		if len(l.Dash) == 0 {
			l.Dash, l.DashOffset = nil, 0
		}
	case LayerParticles:
		if in.Particles != nil {
			p := *in.Particles
			if p.Emitter == "" {
				p.Emitter = EmitterPoint
			}
			if p.Shape == "" {
				p.Shape = ParticleCircle
			}
			if p.Shape != ParticleImage {
				p.Asset = ""
			} else {
				p.Asset = strings.TrimSpace(p.Asset)
			}
			if p.Emitter != EmitterRing {
				p.Radius, p.Outward = 0, false
			}
			l.Particles = &p
		}
		l.Glow, l.GlowColor = 0, ""
	}
	tracks := make([]Track, 0, len(in.Tracks))
	for _, track := range in.Tracks {
		frames := slices.Clone(track.Keyframes)
		slices.SortStableFunc(frames, func(a, b Keyframe) int { return a.T - b.T })
		for i := range frames {
			if frames[i].Easing == "" {
				frames[i].Easing = EaseLinear
			}
		}
		tracks = append(tracks, Track{Property: strings.TrimSpace(track.Property), Keyframes: frames})
	}
	if len(tracks) > 0 {
		l.Tracks = tracks
	}
	return l
}

// UsedAssetKeys 图层与粒子引用到的素材键（去重、按出现顺序）。
func (d Design) UsedAssetKeys() []string {
	keys := make([]string, 0, 4)
	add := func(key string) {
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, layer := range d.Layers {
		switch layer.Type {
		case LayerImage, LayerSprite:
			add(layer.Asset)
		case LayerParticles:
			if layer.Particles != nil && layer.Particles.Shape == ParticleImage {
				add(layer.Particles.Asset)
			}
		}
	}
	return keys
}

// AssetIDs 设计稿引用的素材编号（去重）。
func (d Design) AssetIDs() []int64 {
	ids := make([]int64, 0, len(d.Assets))
	for _, ref := range d.Assets {
		if ref.ID > 0 && !slices.Contains(ids, ref.ID) {
			ids = append(ids, ref.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// ValidateDesign 校验一份（已 NormalizeDesign 过的）设计稿。错误信息点名第几个图层、哪个字段。
func ValidateDesign(slot string, d Design) error {
	spec, ok := FindSlot(slot)
	if !ok {
		return fmt.Errorf("未登记的槽位：%s", slot)
	}
	if slot == SlotNameStyle {
		return validateNameStyle(d.NameStyle)
	}
	if d.Canvas.Width < spec.MinCanvas || d.Canvas.Width > spec.MaxCanvas ||
		d.Canvas.Height < spec.MinCanvas || d.Canvas.Height > spec.MaxCanvas {
		return fmt.Errorf("%s的画布边长需要在 %d–%d 之间", spec.Label, spec.MinCanvas, spec.MaxCanvas)
	}
	if spec.Square && d.Canvas.Width != d.Canvas.Height {
		return fmt.Errorf("%s的画布必须是正方形", spec.Label)
	}
	if d.Duration < 0 || d.Duration > MaxDurationMs {
		return fmt.Errorf("动画时长需要在 0–%d 毫秒之间", MaxDurationMs)
	}
	if d.Background != "" && !colorPattern.MatchString(d.Background) {
		return fmt.Errorf("画布底色不是合法的颜色（#RRGGBB 或 #AARRGGBB）")
	}
	if slot == SlotAvatarFrame {
		if d.Avatar == nil {
			return fmt.Errorf("头像框需要指定头像所在的位置")
		}
		a := d.Avatar
		if !slices.Contains(avatarShapes, a.Shape) {
			return fmt.Errorf("未知的头像形状：%s", a.Shape)
		}
		if a.Size < 8 || a.X < 0 || a.Y < 0 || a.X+a.Size > float64(d.Canvas.Width)+0.01 || a.Y+a.Size > float64(d.Canvas.Height)+0.01 {
			return fmt.Errorf("头像区域需要完整落在画布内，且边长不小于 8")
		}
		if a.Radius < 0 || a.Radius > 0.5 {
			return fmt.Errorf("头像圆角比例需要在 0–0.5 之间")
		}
	}
	if len(d.Layers) > MaxLayers {
		return fmt.Errorf("最多 %d 个图层", MaxLayers)
	}
	if len(d.Assets) > MaxAssetsPerItem {
		return fmt.Errorf("一件装扮最多引用 %d 个素材", MaxAssetsPerItem)
	}
	for key, ref := range d.Assets {
		if ref.ID <= 0 {
			return fmt.Errorf("素材「%s」没有对应的素材文件", key)
		}
	}
	seen := make(map[string]bool, len(d.Layers))
	for i, layer := range d.Layers {
		where := "第 " + strconv.Itoa(i+1) + " 个图层"
		if layer.Name != "" {
			where += "「" + layer.Name + "」"
		}
		if err := validateLayer(d, layer); err != nil {
			return fmt.Errorf("%s：%w", where, err)
		}
		if seen[layer.ID] {
			return fmt.Errorf("%s：图层编号 %s 重复", where, layer.ID)
		}
		seen[layer.ID] = true
	}
	return nil
}

func validateLayer(d Design, l Layer) error {
	if l.ID == "" || len(l.ID) > MaxLayerIDLen || !layerIDPattern.MatchString(l.ID) {
		return fmt.Errorf("图层编号只能用字母、数字、下划线与短横线，最长 %d 位", MaxLayerIDLen)
	}
	if utf8.RuneCountInString(l.Name) > MaxLayerNameLen {
		return fmt.Errorf("图层名称不能超过 %d 字", MaxLayerNameLen)
	}
	if !slices.Contains(layerTypes, l.Type) {
		return fmt.Errorf("未知的图层类型：%s", l.Type)
	}
	if !slices.Contains(placements, l.Placement) {
		return fmt.Errorf("未知的图层位置：%s", l.Placement)
	}
	if !slices.Contains(blends, l.Blend) {
		return fmt.Errorf("未知的混合模式：%s", l.Blend)
	}
	for name, value := range map[string]float64{"x": l.X, "y": l.Y, "宽": l.Width, "高": l.Height, "旋转": l.Rotation,
		"缩放": l.Scale, "不透明度": l.Opacity, "锚点 x": l.AnchorX, "锚点 y": l.AnchorY} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("%s不是有效数字", name)
		}
	}
	maxSide := float64(max(d.Canvas.Width, d.Canvas.Height)) * 4
	if l.Width <= 0 || l.Height <= 0 || l.Width > maxSide || l.Height > maxSide {
		return fmt.Errorf("宽高需要大于 0，且不超过画布的 4 倍")
	}
	if l.Opacity < 0 || l.Opacity > 1 {
		return fmt.Errorf("不透明度需要在 0–1 之间")
	}
	if l.Scale <= 0 || l.Scale > 20 {
		return fmt.Errorf("缩放需要在 0–20 之间")
	}
	if l.AnchorX < 0 || l.AnchorX > 1 || l.AnchorY < 0 || l.AnchorY > 1 {
		return fmt.Errorf("锚点需要在 0–1 之间")
	}
	if err := validateColor("发光颜色", l.GlowColor, false); err != nil {
		return err
	}
	if l.Glow < 0 || l.Glow > 200 {
		return fmt.Errorf("发光半径需要在 0–200 之间")
	}
	if l.Glow > 0 && l.GlowColor == "" {
		return fmt.Errorf("开了发光需要选择发光颜色")
	}

	switch l.Type {
	case LayerImage, LayerSprite:
		if l.Asset == "" {
			return fmt.Errorf("没有选择素材")
		}
		if _, ok := d.Assets[l.Asset]; !ok {
			return fmt.Errorf("引用的素材「%s」不在素材表里", l.Asset)
		}
		if !slices.Contains(fits, l.Fit) {
			return fmt.Errorf("未知的适配方式：%s", l.Fit)
		}
		if err := validateColor("染色", l.Tint, false); err != nil {
			return err
		}
		if l.Type == LayerSprite {
			if l.Columns < 1 || l.Rows < 1 || l.Columns*l.Rows > MaxSpriteFrames {
				return fmt.Errorf("序列帧的行列数需要至少 1，总格数不超过 %d", MaxSpriteFrames)
			}
			if l.FrameCount < 1 || l.FrameCount > l.Columns*l.Rows {
				return fmt.Errorf("序列帧的有效帧数需要在 1–%d 之间", l.Columns*l.Rows)
			}
			if l.FPS <= 0 || l.FPS > 60 {
				return fmt.Errorf("序列帧的帧率需要在 0–60 之间")
			}
		}
	case LayerText:
		if strings.TrimSpace(l.Text) == "" {
			return fmt.Errorf("文字不能为空")
		}
		if utf8.RuneCountInString(l.Text) > MaxTextRunes {
			return fmt.Errorf("文字不能超过 %d 字", MaxTextRunes)
		}
		if l.FontSize < 4 || l.FontSize > 512 {
			return fmt.Errorf("字号需要在 4–512 之间")
		}
		if l.FontWeight < 100 || l.FontWeight > 900 || l.FontWeight%100 != 0 {
			return fmt.Errorf("字重需要是 100–900 之间的整百")
		}
		if err := validateColor("文字颜色", l.Color, true); err != nil {
			return err
		}
		if err := validateColor("描边颜色", l.Stroke, false); err != nil {
			return err
		}
		if !slices.Contains(aligns, l.Align) {
			return fmt.Errorf("未知的对齐方式：%s", l.Align)
		}
		if l.LetterSpacing < -50 || l.LetterSpacing > 200 {
			return fmt.Errorf("字间距需要在 -50–200 之间")
		}
	case LayerShape:
		if !slices.Contains(shapeTypes, l.Shape) {
			return fmt.Errorf("未知的形状：%s", l.Shape)
		}
		if err := validateColor("填充色", l.Fill, false); err != nil {
			return err
		}
		if err := validateColor("描边色", l.Stroke, false); err != nil {
			return err
		}
		if l.Fill == "" && (l.Stroke == "" || l.StrokeWidth <= 0) {
			return fmt.Errorf("形状需要填充色或描边，至少一样")
		}
		if l.StrokeWidth < 0 || l.StrokeWidth > 256 {
			return fmt.Errorf("描边宽度需要在 0–256 之间")
		}
		if l.Shape == ShapeRing && (l.Stroke == "" || l.StrokeWidth <= 0) {
			return fmt.Errorf("圆环需要描边颜色与宽度")
		}
		if l.Sweep < 0 || l.Sweep > 360 {
			return fmt.Errorf("扫过角度需要在 0–360 之间")
		}
		if (l.Shape == ShapeStar || l.Shape == ShapePolygon) && (l.Sides < 3 || l.Sides > 24) {
			return fmt.Errorf("边数 / 角数需要在 3–24 之间")
		}
		if l.Shape == ShapeStar && (l.InnerRatio <= 0 || l.InnerRatio >= 1) {
			return fmt.Errorf("星形内径比例需要在 0–1 之间")
		}
		if l.CornerRadius < 0 {
			return fmt.Errorf("圆角不能为负")
		}
		if len(l.Dash) > MaxDashItems {
			return fmt.Errorf("虚线最多 %d 段", MaxDashItems)
		}
		for _, v := range l.Dash {
			if v <= 0 || v > 4096 {
				return fmt.Errorf("虚线每段长度需要大于 0")
			}
		}
	case LayerParticles:
		if err := validateParticles(d, l.Particles); err != nil {
			return err
		}
	}

	if len(l.Tracks) > MaxTracks {
		return fmt.Errorf("最多 %d 条动画轨道", MaxTracks)
	}
	props := make([]string, 0, len(l.Tracks))
	for _, track := range l.Tracks {
		bounds, ok := propertyRanges[track.Property]
		if !ok {
			return fmt.Errorf("未知的动画属性：%s", track.Property)
		}
		if slices.Contains(props, track.Property) {
			return fmt.Errorf("属性 %s 有两条轨道", track.Property)
		}
		props = append(props, track.Property)
		if (track.Property == PropSweep) && !(l.Type == LayerShape && l.Shape == ShapeRing) {
			return fmt.Errorf("扫过角度动画只能用在圆环上")
		}
		if track.Property == PropDashOffset && len(l.Dash) == 0 {
			return fmt.Errorf("虚线偏移动画需要先设置虚线")
		}
		if len(track.Keyframes) < 2 || len(track.Keyframes) > MaxKeyframes {
			return fmt.Errorf("属性 %s 的关键帧需要 2–%d 个", track.Property, MaxKeyframes)
		}
		for i, kf := range track.Keyframes {
			if kf.T < 0 || kf.T > d.Duration {
				return fmt.Errorf("属性 %s 第 %d 帧的时刻需要在 0–%d 毫秒之间", track.Property, i+1, d.Duration)
			}
			if math.IsNaN(kf.V) || kf.V < bounds[0] || kf.V > bounds[1] {
				return fmt.Errorf("属性 %s 第 %d 帧的值需要在 %g–%g 之间", track.Property, i+1, bounds[0], bounds[1])
			}
			if !slices.Contains(easings, kf.Easing) {
				return fmt.Errorf("未知的缓动：%s", kf.Easing)
			}
		}
	}
	if len(l.Tracks) > 0 && d.Duration == 0 {
		return fmt.Errorf("有动画轨道时整份设计稿的时长不能为 0")
	}
	return nil
}

func validateParticles(d Design, p *Particles) error {
	if p == nil {
		return fmt.Errorf("粒子图层缺少发射器设置")
	}
	if !slices.Contains(emitters, p.Emitter) {
		return fmt.Errorf("未知的发射器：%s", p.Emitter)
	}
	if !slices.Contains(particleKind, p.Shape) {
		return fmt.Errorf("未知的粒子形状：%s", p.Shape)
	}
	if p.Shape == ParticleImage {
		if p.Asset == "" {
			return fmt.Errorf("图片粒子没有选择素材")
		}
		if _, ok := d.Assets[p.Asset]; !ok {
			return fmt.Errorf("粒子引用的素材「%s」不在素材表里", p.Asset)
		}
	}
	if p.Count < 1 || p.Count > MaxParticles {
		return fmt.Errorf("粒子数量需要在 1–%d 之间", MaxParticles)
	}
	if p.Lifetime < 100 || p.Lifetime > 20_000 {
		return fmt.Errorf("粒子寿命需要在 100–20000 毫秒之间")
	}
	if p.Speed < 0 || p.Speed > 5000 {
		return fmt.Errorf("粒子速度需要在 0–5000 之间")
	}
	if p.Spread < 0 || p.Spread > 360 {
		return fmt.Errorf("散射角度需要在 0–360 之间")
	}
	if p.SizeMin <= 0 || p.SizeMax < p.SizeMin || p.SizeMax > 512 {
		return fmt.Errorf("粒子大小需要大于 0，且最大值不小于最小值、不超过 512")
	}
	if p.Radius < 0 || p.Radius > 4096 {
		return fmt.Errorf("发射半径需要在 0–4096 之间")
	}
	if math.Abs(p.Gravity) > 5000 || math.Abs(p.Spin) > 3600 {
		return fmt.Errorf("重力或自转超出范围")
	}
	if len(p.Colors) > 8 {
		return fmt.Errorf("粒子颜色最多 8 种")
	}
	for _, c := range p.Colors {
		if err := validateColor("粒子颜色", c, true); err != nil {
			return err
		}
	}
	if p.Shape != ParticleImage && len(p.Colors) == 0 {
		return fmt.Errorf("粒子至少要选一种颜色")
	}
	return nil
}

func validateNameStyle(s *NameStyle) error {
	if s == nil {
		return fmt.Errorf("昵称样式缺少样式设置")
	}
	if err := validateColor("昵称颜色", s.Color, true); err != nil {
		return err
	}
	if !slices.Contains(nameEffects, s.Effect) {
		return fmt.Errorf("未知的昵称效果：%s", s.Effect)
	}
	if s.Effect == NameEffectCycle && (len(s.Colors) < 2 || len(s.Colors) > MaxNameColors) {
		return fmt.Errorf("循环变色需要 2–%d 种颜色", MaxNameColors)
	}
	for _, c := range s.Colors {
		if err := validateColor("循环颜色", c, true); err != nil {
			return err
		}
	}
	if s.Speed < 300 || s.Speed > 20_000 {
		return fmt.Errorf("效果周期需要在 300–20000 毫秒之间")
	}
	if s.FontWeight < 100 || s.FontWeight > 900 || s.FontWeight%100 != 0 {
		return fmt.Errorf("字重需要是 100–900 之间的整百")
	}
	if err := validateColor("发光颜色", s.GlowColor, s.Effect == NameEffectGlow); err != nil {
		return err
	}
	if err := validateColor("高光颜色", s.HighlightColor, false); err != nil {
		return err
	}
	if s.GlowRadius < 0 || s.GlowRadius > 32 {
		return fmt.Errorf("发光半径需要在 0–32 之间")
	}
	return nil
}

func validateColor(name, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("请选择%s", name)
		}
		return nil
	}
	if !colorPattern.MatchString(value) {
		return fmt.Errorf("%s不是合法的颜色（#RRGGBB 或 #AARRGGBB）", name)
	}
	return nil
}

// EqualDesign 两份设计稿语义相同（草稿是否与已发布的一致）。调用方传入的都应是规范化后的值。
func EqualDesign(a, b *Design) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return designFingerprint(*a) == designFingerprint(*b)
}
