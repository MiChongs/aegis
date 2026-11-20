package cosmetic

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"
)

// Eligibility 「这个人此刻能不能戴这件装扮」的唯一判据。佩戴时判、读取时再判（见包注释）。
//
// 顺序：装扮不可用 → 不能；持有仍生效 → 能；否则看获取方式。
// 返回的 reason 与装扮中心「为什么灰着」、佩戴失败的错误码同一套。
func Eligibility(item Item, facts Facts, now time.Time) (bool, string) {
	if !item.Live() {
		return false, ReasonUnavailable
	}
	if expires, ok := facts.Owned[item.ID]; ok && (expires == nil || expires.After(now)) {
		return true, ""
	}
	switch item.Unlock.Mode {
	case UnlockFree:
		return true, ""
	case UnlockVIP:
		if !facts.IsVIP {
			if len(item.Unlock.TierIDs) > 0 {
				return false, ReasonNeedTier
			}
			return false, ReasonNeedVIP
		}
		if len(item.Unlock.TierIDs) == 0 {
			return true, ""
		}
		for _, id := range facts.TierIDs {
			if slices.Contains(item.Unlock.TierIDs, id) {
				return true, ""
			}
		}
		return false, ReasonNeedTier
	case UnlockLevel:
		if facts.Level >= item.Unlock.MinLevel {
			return true, ""
		}
		return false, ReasonNeedLevel
	case UnlockFeature:
		if slices.Contains(facts.Features, item.Unlock.Feature) {
			return true, ""
		}
		return false, ReasonNeedFeature
	}
	return false, ReasonNotOwned
}

// ReasonLabel 不能戴的原因的中文说明。
func ReasonLabel(reason string) string {
	switch reason {
	case ReasonNotOwned:
		return "尚未获得"
	case ReasonExpired:
		return "已过期"
	case ReasonNeedVIP:
		return "会员专属"
	case ReasonNeedTier:
		return "指定会员专属"
	case ReasonNeedLevel:
		return "等级不足"
	case ReasonNeedFeature:
		return "需要对应权益"
	case ReasonUnavailable:
		return "已下架"
	case ReasonSlotMismatch:
		return "槽位不符"
	}
	return ""
}

// ToView 客户端视图。
func (i Item) ToView() View {
	return View{ID: i.ID, Slot: i.Slot, Code: i.Code, Name: i.Name, Description: i.Description, Rarity: i.Rarity,
		Series: i.Series, Revision: i.Revision, Preview: i.Preview, Design: i.Design}
}

// UnlockLabel 获取方式的一句话：「会员专属」「Lv.10 解锁」。
func UnlockLabel(u Unlock) string {
	spec, ok := FindUnlock(u.Mode)
	if !ok {
		return u.Mode
	}
	if u.Mode == UnlockLevel {
		return "Lv." + strconv.Itoa(u.MinLevel) + " 解锁"
	}
	return spec.Label
}

// designFingerprint 设计稿的规范 JSON（map 键有序，比较用）。
func designFingerprint(d Design) string {
	raw, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(raw)
}
