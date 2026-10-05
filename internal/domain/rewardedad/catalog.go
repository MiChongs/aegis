package rewardedad

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	cardkeydomain "aegis/internal/domain/cardkey"
)

var (
	sceneKeyPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	placementIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	providerAppPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{0,64}$`)
	transIDPattern     = regexp.MustCompile(`^[A-Za-z0-9_.:-]{6,128}$`)
)

// RewardCatalog 激励广告可配的权益档位（卡密目录的子集，顺序沿用卡密目录）。
func RewardCatalog() []cardkeydomain.RewardSpec {
	out := make([]cardkeydomain.RewardSpec, 0, len(allowedRewardTypes))
	for _, spec := range cardkeydomain.RewardCatalog() {
		if slices.Contains(allowedRewardTypes, spec.Type) {
			out = append(out, spec)
		}
	}
	return out
}

// RewardTypes 激励广告可发的权益类型。
func RewardTypes() []string {
	out := make([]string, len(allowedRewardTypes))
	copy(out, allowedRewardTypes)
	return out
}

// VerifyModes 全部校验模式，顺序即控制台的展示顺序。
func VerifyModes() []string {
	return []string{VerifyDual, VerifyServer, VerifyClient}
}

// Providers 已支持的广告平台。
func Providers() []string {
	return []string{ProviderHuijing}
}

// ValidVerifyMode 校验模式是否合法。
func ValidVerifyMode(mode string) bool {
	return slices.Contains(VerifyModes(), mode)
}

// ValidTransID 平台 trans_id 的形态校验。只挡明显的垃圾输入，不对平台的格式做更多假设。
func ValidTransID(transID string) bool {
	return transIDPattern.MatchString(transID)
}

// ValidateRewards 校验一个场景的权益：卡密目录的规则之上，再挡掉不开放给广告的档位。
func ValidateRewards(rewards []Reward) error {
	for _, reward := range rewards {
		if !slices.Contains(allowedRewardTypes, reward.Type) {
			if spec, ok := cardkeydomain.FindRewardSpec(reward.Type); ok {
				return fmt.Errorf("激励广告不能发放「%s」", spec.Label)
			}
			return fmt.Errorf("未登记的权益类型：%s", reward.Type)
		}
	}
	return cardkeydomain.ValidateRewards(rewards)
}

// NormalizeScene 清洗一个场景（去空白、权益按目录排序），不做合法性判断。
func NormalizeScene(scene Scene) Scene {
	scene.Key = strings.ToLower(strings.TrimSpace(scene.Key))
	scene.Name = strings.TrimSpace(scene.Name)
	scene.PlacementID = strings.TrimSpace(scene.PlacementID)
	scene.Rewards = cardkeydomain.NormalizeRewards(scene.Rewards)
	if scene.Rewards == nil {
		scene.Rewards = []Reward{}
	}
	return scene
}

// ValidateScenes 校验全部场景。每一类问题都给出能直接照着改的文案。
func ValidateScenes(scenes []Scene) error {
	if len(scenes) > MaxScenes {
		return fmt.Errorf("最多配置 %d 个场景", MaxScenes)
	}
	seen := make(map[string]bool, len(scenes))
	for index, scene := range scenes {
		label := fmt.Sprintf("第 %d 个场景", index+1)
		if !sceneKeyPattern.MatchString(scene.Key) {
			return fmt.Errorf("%s的标识需为 1–32 位小写字母、数字、下划线或短横线", label)
		}
		if seen[scene.Key] {
			return fmt.Errorf("场景标识「%s」重复", scene.Key)
		}
		seen[scene.Key] = true
		label = "场景「" + scene.Key + "」"
		if scene.Name == "" || utf8.RuneCountInString(scene.Name) > 32 {
			return fmt.Errorf("%s的名称需为 1–32 个字", label)
		}
		if !placementIDPattern.MatchString(scene.PlacementID) {
			return fmt.Errorf("%s的广告位 ID 需为 1–64 位字母或数字", label)
		}
		if scene.DailyLimit < 0 || scene.DailyLimit > MaxDailyLimit {
			return fmt.Errorf("%s的每日次数需在 0–%d 之间", label, MaxDailyLimit)
		}
		if scene.CooldownSeconds < 0 || scene.CooldownSeconds > MaxCooldownSeconds {
			return fmt.Errorf("%s的冷却时间需在 0–%d 秒之间", label, MaxCooldownSeconds)
		}
		if err := ValidateRewards(scene.Rewards); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
	}
	return nil
}

// ValidProviderAppID 平台应用 ID 的形态校验（允许留空）。
func ValidProviderAppID(value string) bool {
	return providerAppPattern.MatchString(value)
}

// DescribeRewards 一句话说清一组权益，客户端与核销结果直接展示。
func DescribeRewards(rewards []Reward, planNames map[int64]string) string {
	parts := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		if reward.Type == cardkeydomain.RewardVipPlan {
			if name := planNames[reward.RefID]; name != "" {
				parts = append(parts, "会员套餐「"+name+"」")
				continue
			}
		}
		parts = append(parts, cardkeydomain.DescribeReward(reward))
	}
	return strings.Join(parts, "、")
}

// ReasonMessage 拒发原因的用户可读文案。
func ReasonMessage(status, reason string) string {
	switch status {
	case StatusGranted:
		return "奖励已发放"
	case StatusPending:
		return "正在确认观看结果，奖励稍后到账"
	}
	switch reason {
	case ReasonDisabled:
		return "激励广告暂未开放"
	case ReasonSceneUnavailable:
		return "该奖励活动已下线"
	case ReasonDailyLimit:
		return "今天的观看奖励次数已用完，明天再来"
	case ReasonSceneDailyLimit:
		return "今天这个奖励已经领满了，明天再来"
	case ReasonCooldown:
		return "领取太频繁了，请稍后再看"
	case ReasonUserMismatch:
		return "这次观看不属于当前账号"
	case ReasonClientUnverified:
		return "广告未完整播放，未获得奖励"
	case ReasonUserNotFound:
		return "账号不存在"
	}
	return "未获得奖励"
}

// HuijingSign 灰鲸服务端回调的签名：sha256(securityKey + ":" + transId) 的十六进制小写。
func HuijingSign(securityKey, transID string) string {
	sum := sha256.Sum256([]byte(securityKey + ":" + transID))
	return hex.EncodeToString(sum[:])
}

// VerifyHuijingSign 常数时间比较签名，大小写不敏感。
func VerifyHuijingSign(securityKey, transID, sign string) bool {
	if securityKey == "" || transID == "" || sign == "" {
		return false
	}
	expected := HuijingSign(securityKey, transID)
	got := strings.ToLower(strings.TrimSpace(sign))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// sceneExtraKeys 客户端放进 SDK options 里表示场景的键。
//
// 灰鲸把 HJRewardAdRequest 的 options 原样作为 EXTRAINFO 回传，但文档没有约定序列化形态，
// 实测既见过 JSON 对象，也可能是 a=b&c=d。两种都认；认不出来就当没有，交给客户端上报。
var sceneExtraKeys = []string{"aegisScene", "aegis_scene", "scene"}

// SceneFromExtra 从回调的 EXTRAINFO 里取出客户端声明的场景。
func SceneFromExtra(extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return ""
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(extra), &object); err == nil {
		for _, key := range sceneExtraKeys {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.ToLower(strings.TrimSpace(value))
			}
		}
		return ""
	}
	if values, err := url.ParseQuery(extra); err == nil {
		for _, key := range sceneExtraKeys {
			if value := strings.TrimSpace(values.Get(key)); value != "" {
				return strings.ToLower(value)
			}
		}
	}
	return ""
}
