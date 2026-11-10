package rewardedad

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 广告位与它的 Security Key。
//
// Apex 的 Security Key 是按广告位签发的：同一个平台应用下，「激励视频」与「激励视频2」两个广告位
// 各有一把，回调签名用的是观看所在广告位的那一把。只存一把应用级的 Key，第二个广告位的回调就永远验不过签。
//
// 所以 Key 挂在广告位上；应用级的那一把保留为「默认 Key」，给没单独配 Key 的广告位兜底
// （单广告位的应用、以及这项配置出现之前的老配置，都只有它）。

// 广告位规模与字段上界。
const (
	MaxPlacements        = 20
	MaxPlacementName     = 32
	MaxSecurityKeyLength = 256
)

// 广告位验签用的是哪一把 Key。
const (
	KeySourceOwn     = "own"     // 广告位自己的 Key
	KeySourceDefault = "default" // 没单独配，用应用的默认 Key
	KeySourceMissing = "missing" // 两者都没有：这个广告位的回调验不过签
)

// Placement 平台上的一个激励广告位。
type Placement struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// SecurityKeyCipher AES-GCM 密文，只在服务端内部流转，任何出网响应都不带。
	SecurityKeyCipher string     `json:"-"`
	SecurityKeyHint   string     `json:"securityKeyHint,omitempty"`
	KeyUpdatedAt      *time.Time `json:"keyUpdatedAt,omitempty"`
}

// HasKey 广告位是否单独配了 Key。
func (p Placement) HasKey() bool {
	return p.SecurityKeyCipher != ""
}

// PlacementInput 保存配置时的一个广告位。
//
// SecurityKey 留空表示沿用；PreviousID 是改 ID 前的旧值，用来把已存的 Key 带到新 ID 上
// （新加的广告位留空）。
type PlacementInput struct {
	ID               string
	PreviousID       string
	Name             string
	SecurityKey      string
	ClearSecurityKey bool
}

// AdminPlacement 管理端看到的一个广告位：Key 只回答「配没配」，另附用途与最近的回调。
type AdminPlacement struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	HasSecurityKey  bool       `json:"hasSecurityKey"`
	SecurityKeyHint string     `json:"securityKeyHint,omitempty"`
	KeyUpdatedAt    *time.Time `json:"keyUpdatedAt,omitempty"`
	// KeySource 回调验签实际会用哪一把：own / default / missing
	KeySource string `json:"keySource"`
	// Scenes 用这个广告位的场景标识
	Scenes []string `json:"scenes"`
	// LastCallbackAt 最近一次验签通过的回调（统计窗口内），Callbacks 窗口内验签通过的回调次数
	LastCallbackAt *time.Time `json:"lastCallbackAt,omitempty"`
	Callbacks      int64      `json:"callbacks"`
}

// PlacementActivity 一个广告位在统计窗口内的回调情况，仓储层按广告位聚合。
type PlacementActivity struct {
	LastCallbackAt *time.Time
	Callbacks      int64
}

// NormalizePlacement 去空白，不做合法性判断。
func NormalizePlacement(input PlacementInput) PlacementInput {
	input.ID = strings.TrimSpace(input.ID)
	input.PreviousID = strings.TrimSpace(input.PreviousID)
	input.Name = strings.TrimSpace(input.Name)
	input.SecurityKey = strings.TrimSpace(input.SecurityKey)
	return input
}

// ValidatePlacements 校验广告位列表的形态（ID、名称、Key 长度、重复）。
func ValidatePlacements(placements []PlacementInput) error {
	if len(placements) > MaxPlacements {
		return fmt.Errorf("最多登记 %d 个广告位", MaxPlacements)
	}
	seen := make(map[string]bool, len(placements))
	for index, placement := range placements {
		label := fmt.Sprintf("第 %d 个广告位", index+1)
		if !placementIDPattern.MatchString(placement.ID) {
			return fmt.Errorf("%s的 ID 需为 1–64 位字母、数字、下划线或短横线", label)
		}
		if seen[placement.ID] {
			return fmt.Errorf("广告位「%s」重复登记", placement.ID)
		}
		seen[placement.ID] = true
		if utf8.RuneCountInString(placement.Name) > MaxPlacementName {
			return fmt.Errorf("广告位「%s」的名称最多 %d 个字", placement.ID, MaxPlacementName)
		}
		if utf8.RuneCountInString(placement.SecurityKey) > MaxSecurityKeyLength {
			return fmt.Errorf("广告位「%s」的 Security Key 过长", placement.ID)
		}
	}
	return nil
}

// FindPlacement 按 ID 取广告位。
func (c *Config) FindPlacement(id string) (Placement, bool) {
	if c == nil || id == "" {
		return Placement{}, false
	}
	for _, placement := range c.Placements {
		if placement.ID == id {
			return placement, true
		}
	}
	return Placement{}, false
}

// KeySource 这个广告位的回调会用哪一把 Key 验签。
func (c *Config) KeySource(placementID string) string {
	if placement, ok := c.FindPlacement(placementID); ok && placement.HasKey() {
		return KeySourceOwn
	}
	if c != nil && c.SecurityKeyCipher != "" {
		return KeySourceDefault
	}
	return KeySourceMissing
}

// CallbackKeyCiphers 验一次回调要试的 Key（密文），按顺序试，任一通过即可。
//
//   - 回调里的广告位登记过且有自己的 Key：只认它。别的广告位的 Key 签出来的回调不算数 ——
//     那说明回调里的广告位是被改过的。
//   - 否则用默认 Key。
//   - 连默认 Key 都没有，且广告位没登记（或回调没带广告位）：逐一试各广告位的 Key。
//     签名能对上任一把，就说明回调确实出自本应用在平台上的某个广告位；
//     登记过却没有 Key 的广告位不走这一步，它的回调本就该报「缺少 Key」。
func (c *Config) CallbackKeyCiphers(placementID string) []string {
	if c == nil {
		return nil
	}
	placement, registered := c.FindPlacement(placementID)
	if registered && placement.HasKey() {
		return []string{placement.SecurityKeyCipher}
	}
	if c.SecurityKeyCipher != "" {
		return []string{c.SecurityKeyCipher}
	}
	if registered {
		return nil
	}
	keys := make([]string, 0, len(c.Placements))
	for _, item := range c.Placements {
		if item.HasKey() {
			keys = append(keys, item.SecurityKeyCipher)
		}
	}
	return keys
}

// EnsureScenePlacements 场景里用到、却没登记的广告位补登进列表（不带 Key，验签走默认 Key）。
//
// 这项配置出现之前，广告位只是场景上的一个字段；老配置读出来时据此补齐，
// 只改场景不传广告位列表的调用方（老版本控制台、MCP）也不会因此丢广告位。
func (c *Config) EnsureScenePlacements() {
	if c == nil {
		return
	}
	known := make(map[string]bool, len(c.Placements))
	for _, placement := range c.Placements {
		known[placement.ID] = true
	}
	for _, scene := range c.Scenes {
		if scene.PlacementID == "" || known[scene.PlacementID] {
			continue
		}
		known[scene.PlacementID] = true
		c.Placements = append(c.Placements, Placement{ID: scene.PlacementID, Name: scene.Name})
	}
}

// ScenesByPlacement 广告位 → 用它的场景标识。
func (c *Config) ScenesByPlacement() map[string][]string {
	result := map[string][]string{}
	if c == nil {
		return result
	}
	for _, scene := range c.Scenes {
		result[scene.PlacementID] = append(result[scene.PlacementID], scene.Key)
	}
	return result
}
