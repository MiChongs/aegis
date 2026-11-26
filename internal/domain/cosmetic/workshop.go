package cosmetic

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ── 自定义工坊（docs/cosmetics.md「自定义工坊」）──
//
// 用户在 App 里用官方零件拼自己的头像框、资料卡、昵称样式、勋章（铭牌不进工坊）。作品就是一件 origin = workshop 的装扮：
// 与官方装扮同一张表、同一份设计稿规格、同一套佩戴与绘制，只是
//
//   - 只有作者本人能戴（获取方式 creator），且作者此刻要有工坊额度（会员到期 → 额度归零 → 作品戴不上，读取时过滤）；
//   - 不能上传图片：图片与序列帧只能引用运营登记的「零件」（cosmetic_workshop_parts），不能有文字图层；
//   - 不进装扮中心、不能被各系统发放、不进管理端的装扮列表；
//   - 名称只给作者自己看，别人看到的是「工坊作品」。

// 来源。
const (
	OriginOfficial = "official"
	OriginWorkshop = "workshop"
)

// UnlockCreator 工坊作品的获取方式：只有作者本人、且有工坊额度时能戴。不在 unlockSpecs 里，运营建装扮选不到它。
const UnlockCreator = "creator"

// ReasonNeedWorkshop 工坊额度已失效（会员到期等），作品暂时不能戴。
const ReasonNeedWorkshop = "need_workshop"

// WorkshopDisplayName 别人看到的工坊作品名称。
const WorkshopDisplayName = "工坊作品"

// 工坊上界。
const (
	MaxWorkshopQuota     = 50
	MaxWorkshopLayers    = 16
	MaxWorkshopParticles = 160
	MaxWorkshopNameRunes = 16
	MaxWorkshopParts     = 500
	MaxPartNameRunes     = 32
	MaxPartCategoryRunes = 16
	MaxTakedownRunes     = 128
)

// WorkshopSettings 应用的工坊设置（存在 cosmetic_settings.workshop）。
//
// 额度：每人最多保存几件作品。取「人人都有的 FreeQuota」「有效会员的 VIPQuota」「所在会员档位的 TierQuotas」三者里最大的；
// 为 0 即没有工坊权限。
type WorkshopSettings struct {
	Enabled   bool `json:"enabled"`
	FreeQuota int  `json:"freeQuota"`
	VIPQuota  int  `json:"vipQuota"`
	// TierQuotas 会员档位编号（字符串）→ 额度
	TierQuotas map[string]int `json:"tierQuotas,omitempty"`
}

// QuotaFor 这个人此刻的工坊额度。工坊没开启时为 0。
func (w WorkshopSettings) QuotaFor(f Facts) int {
	if !w.Enabled {
		return 0
	}
	quota := w.FreeQuota
	if f.IsVIP {
		quota = max(quota, w.VIPQuota)
		for _, id := range f.TierIDs {
			if q, ok := w.TierQuotas[strconv.FormatInt(id, 10)]; ok {
				quota = max(quota, q)
			}
		}
	}
	return min(max(quota, 0), MaxWorkshopQuota)
}

// ValidateWorkshopSettings 校验工坊设置。
func ValidateWorkshopSettings(w WorkshopSettings) error {
	check := func(label string, q int) error {
		if q < 0 || q > MaxWorkshopQuota {
			return fmt.Errorf("%s额度需要在 0–%d 之间", label, MaxWorkshopQuota)
		}
		return nil
	}
	if err := check("免费用户", w.FreeQuota); err != nil {
		return err
	}
	if err := check("会员", w.VIPQuota); err != nil {
		return err
	}
	for key, q := range w.TierQuotas {
		if id, err := strconv.ParseInt(key, 10, 64); err != nil || id <= 0 {
			return fmt.Errorf("会员档位编号不正确：%s", key)
		}
		if err := check("会员档位", q); err != nil {
			return err
		}
	}
	return nil
}

// Part 工坊零件：运营从素材库里挑出来、允许用户在作品里使用的图片或序列帧。
type Part struct {
	ID       int64  `json:"id"`
	AppID    int64  `json:"appid,omitempty"`
	AssetID  int64  `json:"assetId"`
	Name     string `json:"name"`
	Category string `json:"category"`
	// Slots 可以用在哪些槽位（不含昵称样式）
	Slots []string `json:"slots"`
	// 序列帧的切分（素材是 sprite 时必填，作品里的图层必须照用）
	Columns    int       `json:"columns,omitempty"`
	Rows       int       `json:"rows,omitempty"`
	FrameCount int       `json:"frameCount,omitempty"`
	FPS        float64   `json:"fps,omitempty"`
	SortOrder  int       `json:"sortOrder"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	// Asset 素材（地址已解析）
	Asset *Asset `json:"asset,omitempty"`
}

// Sprite 零件是序列帧。
func (p Part) Sprite() bool { return p.FrameCount > 0 }

// PartInput 新建 / 修改零件。
type PartInput struct {
	AssetID    int64    `json:"assetId"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Slots      []string `json:"slots"`
	Columns    int      `json:"columns"`
	Rows       int      `json:"rows"`
	FrameCount int      `json:"frameCount"`
	FPS        float64  `json:"fps"`
	SortOrder  int      `json:"sortOrder"`
	Enabled    bool     `json:"enabled"`
}

// NormalizePartInput 去空白、槽位去重。
func NormalizePartInput(in PartInput) PartInput {
	in.Name = strings.TrimSpace(in.Name)
	in.Category = strings.TrimSpace(in.Category)
	slots := make([]string, 0, len(in.Slots))
	for _, s := range in.Slots {
		s = strings.TrimSpace(s)
		if s != "" && !slices.Contains(slots, s) {
			slots = append(slots, s)
		}
	}
	in.Slots = slots
	if in.FrameCount == 0 {
		in.Columns, in.Rows, in.FPS = 0, 0, 0
	}
	return in
}

// ValidatePartInput 校验零件。asset 是它引用的素材。
func ValidatePartInput(in PartInput, asset Asset) error {
	if in.Name == "" {
		return fmt.Errorf("请填写零件名称")
	}
	if utf8.RuneCountInString(in.Name) > MaxPartNameRunes {
		return fmt.Errorf("零件名称不能超过 %d 字", MaxPartNameRunes)
	}
	if utf8.RuneCountInString(in.Category) > MaxPartCategoryRunes {
		return fmt.Errorf("分类不能超过 %d 字", MaxPartCategoryRunes)
	}
	if len(in.Slots) == 0 {
		return fmt.Errorf("请至少选择一个可用的槽位")
	}
	for _, s := range in.Slots {
		spec, ok := FindSlot(s)
		if !ok || !spec.Layered || !spec.Workshop {
			return fmt.Errorf("零件不能用在%s上", SlotLabel(s))
		}
	}
	if in.FrameCount > 0 {
		if in.Columns < 1 || in.Rows < 1 || in.FrameCount > in.Columns*in.Rows || in.FrameCount > MaxSpriteFrames {
			return fmt.Errorf("序列帧的行列与帧数不匹配（最多 %d 帧）", MaxSpriteFrames)
		}
		if in.FPS <= 0 || in.FPS > 60 {
			return fmt.Errorf("序列帧的帧率需要在 0–60 之间")
		}
	} else if asset.Kind == "sprite" {
		return fmt.Errorf("这份素材是序列帧，请填写行列、帧数与帧率")
	}
	return nil
}

// WorkshopItem 工坊作品（作者在工坊里看到的样子）。
type WorkshopItem struct {
	View
	// Status active 正常 / archived 被运营下架（TakedownReason 写着原因）
	Status         string    `json:"status"`
	TakedownReason string    `json:"takedownReason,omitempty"`
	Equipped       bool      `json:"equipped"`
	Position       int       `json:"position,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// WorkshopAdminItem 管理端看到的工坊作品：带作者。
type WorkshopAdminItem struct {
	Item
	CreatorUserID   int64  `json:"creatorUserId"`
	CreatorAccount  string `json:"creatorAccount,omitempty"`
	CreatorNickname string `json:"creatorNickname,omitempty"`
}

// WorkshopInput 新建 / 修改作品。
type WorkshopInput struct {
	Slot   string `json:"slot"`
	Name   string `json:"name"`
	Design Design `json:"design"`
}

// ValidateWorkshopName 作品名称。
func ValidateWorkshopName(name string) error {
	if name == "" {
		return fmt.Errorf("请给作品起个名字")
	}
	if utf8.RuneCountInString(name) > MaxWorkshopNameRunes {
		return fmt.Errorf("作品名称不能超过 %d 字", MaxWorkshopNameRunes)
	}
	return nil
}

// ValidateWorkshopDesign 在 ValidateDesign 之外再收紧：画布与头像位置用槽位默认值、不能有文字图层、
// 图层与粒子数更少、图片只能引用允许这个槽位的零件、序列帧照零件的切分。parts 以素材编号为键。
// d 必须已 NormalizeDesign。
func ValidateWorkshopDesign(slot string, d Design, parts map[int64]Part) error {
	spec, ok := FindSlot(slot)
	if !ok {
		return fmt.Errorf("未登记的槽位：%s", slot)
	}
	if !spec.Workshop {
		return fmt.Errorf("%s不能在工坊里制作", spec.Label)
	}
	if err := ValidateDesign(slot, d); err != nil {
		return err
	}
	if !spec.Layered {
		return nil
	}
	if d.Canvas != spec.Canvas {
		return fmt.Errorf("工坊作品的画布固定为 %d×%d", spec.Canvas.Width, spec.Canvas.Height)
	}
	if spec.Avatar != nil && (d.Avatar == nil || *d.Avatar != *spec.Avatar) {
		return fmt.Errorf("工坊作品的头像位置不能修改")
	}
	if len(d.Layers) > MaxWorkshopLayers {
		return fmt.Errorf("工坊作品最多 %d 个图层", MaxWorkshopLayers)
	}
	particles := 0
	partFor := func(i int, key string) (Part, error) {
		ref, ok := d.Assets[key]
		if !ok {
			return Part{}, fmt.Errorf("第 %d 个图层引用的零件不存在", i+1)
		}
		part, ok := parts[ref.ID]
		if !ok || !part.Enabled || !slices.Contains(part.Slots, slot) {
			return Part{}, fmt.Errorf("第 %d 个图层用的零件已下线或不能用在%s上", i+1, spec.Label)
		}
		return part, nil
	}
	for i, layer := range d.Layers {
		switch layer.Type {
		case LayerText:
			return fmt.Errorf("工坊作品不能使用文字图层")
		case LayerImage:
			part, err := partFor(i, layer.Asset)
			if err != nil {
				return err
			}
			if part.Sprite() {
				return fmt.Errorf("第 %d 个图层：「%s」是序列帧零件，请用序列帧图层", i+1, part.Name)
			}
		case LayerSprite:
			part, err := partFor(i, layer.Asset)
			if err != nil {
				return err
			}
			if !part.Sprite() || layer.Columns != part.Columns || layer.Rows != part.Rows || layer.FrameCount != part.FrameCount {
				return fmt.Errorf("第 %d 个图层：序列帧的切分要与零件「%s」一致", i+1, part.Name)
			}
		case LayerParticles:
			if layer.Particles != nil {
				particles += layer.Particles.Count
				if layer.Particles.Shape == ParticleImage {
					if _, err := partFor(i, layer.Particles.Asset); err != nil {
						return err
					}
				}
			}
		}
	}
	if particles > MaxWorkshopParticles {
		return fmt.Errorf("工坊作品的粒子总数不能超过 %d", MaxWorkshopParticles)
	}
	return nil
}

// WorkshopLimits 给客户端编辑器的上界。
func WorkshopLimits() map[string]int {
	return map[string]int{
		"maxQuota": MaxWorkshopQuota, "maxLayers": MaxWorkshopLayers, "maxParticles": MaxWorkshopParticles,
		"maxNameRunes": MaxWorkshopNameRunes, "maxTracks": MaxTracks, "maxKeyframes": MaxKeyframes,
		"maxDurationMs": MaxDurationMs,
	}
}
