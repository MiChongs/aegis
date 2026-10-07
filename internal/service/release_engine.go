package service

import (
	"fmt"
	"hash/fnv"
	"slices"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
)

// 发布中心的检测引擎：纯函数，不碰数据库。
//
// 检测接口、官网「最新版本」、版本历史与控制台的「模拟检测」都走这一份判定 ——
// 让它们各写一遍，迟早会出现「控制台模拟说能收到、真机却收不到」的事。

const (
	releaseChangelogLimit   = 20
	releaseNextCheckDefault = 6 * 60 * 60
	releaseNextCheckForce   = 60 * 60
)

// releaseViewer 一次判定里「谁在看」：客户端环境加上他所在的渠道。
type releaseViewer struct {
	Client appdomain.ReleaseClient
	// ChannelIDs 显式加入的渠道（管理员分配或自助加入）
	ChannelIDs map[int64]bool
	// DefaultChannelID 默认渠道 = 所有人
	DefaultChannelID int64
}

// releaseBucket 灰度分桶：同一个版本、同一个人永远落在同一个桶里，
// 放量从 10% 调到 30% 时，原来那 10% 的人仍然在里面。
// 以版本 ID 加盐：否则每次灰度都是同一批人当小白鼠。
func releaseBucket(releaseID int64, subject string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strconv.FormatInt(releaseID, 10) + ":" + subject))
	return int(h.Sum32() % 100)
}

func releaseSubject(client appdomain.ReleaseClient) string {
	if client.UserID > 0 {
		return "u:" + strconv.FormatInt(client.UserID, 10)
	}
	if device := strings.TrimSpace(client.DeviceID); device != "" {
		return "d:" + device
	}
	return ""
}

// evaluateRelease 判一个版本对这个人是否可下发。ignoreVersion 为真时不比较版本码（官网最新版本、历史）。
func evaluateRelease(rel *appdomain.Release, viewer releaseViewer, now time.Time, ignoreVersion bool) (bool, string, int) {
	client := viewer.Client
	if !rel.IsLive(now) {
		if rel.Status == appdomain.ReleaseStatusScheduled {
			return false, "定时发布尚未到点", 0
		}
		return false, "未在下发状态", 0
	}
	if rel.Platform != "" && rel.Platform != "all" && !strings.EqualFold(rel.Platform, client.Platform) {
		return false, fmt.Sprintf("平台不符（版本为 %s）", rel.Platform), 0
	}
	if !ignoreVersion && rel.VersionCode <= client.VersionCode {
		return false, "不高于当前版本", 0
	}
	if rel.Channel != nil && rel.Channel.ID != viewer.DefaultChannelID && !viewer.ChannelIDs[rel.Channel.ID] {
		return false, fmt.Sprintf("不在渠道「%s」", rel.Channel.Name), 0
	}

	t := rel.Targeting
	device := strings.TrimSpace(client.DeviceID)
	if client.UserID > 0 && slices.Contains(t.ExcludeUserIDs, client.UserID) {
		return false, "用户在排除名单", 0
	}
	if device != "" && slices.Contains(t.ExcludeDeviceIDs, device) {
		return false, "设备在排除名单", 0
	}
	if (client.UserID > 0 && slices.Contains(t.TesterUserIDs, client.UserID)) ||
		(device != "" && slices.Contains(t.TesterDeviceIDs, device)) {
		return true, "内测名单", 0
	}
	switch rel.Visibility {
	case appdomain.ReleaseVisibilityTesters:
		return false, "仅内测名单可见", 0
	case appdomain.ReleaseVisibilitySignedIn:
		if client.UserID <= 0 {
			return false, "仅登录用户可见", 0
		}
	}

	if len(t.Regions) > 0 && !releaseContainsFold(t.Regions, client.Region) {
		return false, "地区不在定向范围", 0
	}
	if len(t.Locales) > 0 && !localeMatches(t.Locales, client.Locale) {
		return false, "语言不在定向范围", 0
	}
	if len(t.DeviceModels) > 0 && !modelMatches(t.DeviceModels, client.DeviceModel) {
		return false, "机型不在定向范围", 0
	}
	if len(t.Abis) > 0 && !slices.ContainsFunc(client.Abis, func(abi string) bool { return releaseContainsFold(t.Abis, abi) }) {
		return false, "ABI 不在定向范围", 0
	}
	if t.MinOSVersion > 0 && client.OSVersion > 0 && client.OSVersion < t.MinOSVersion {
		return false, fmt.Sprintf("系统版本低于 %d", t.MinOSVersion), 0
	}
	if t.MaxOSVersion > 0 && client.OSVersion > t.MaxOSVersion {
		return false, fmt.Sprintf("系统版本高于 %d", t.MaxOSVersion), 0
	}
	if !ignoreVersion {
		if t.MinSourceVersionCode > 0 && client.VersionCode < t.MinSourceVersionCode {
			return false, "当前版本低于来源版本范围", 0
		}
		if t.MaxSourceVersionCode > 0 && client.VersionCode > t.MaxSourceVersionCode {
			return false, "当前版本高于来源版本范围", 0
		}
	}

	if rel.RolloutPct >= 100 {
		return true, "全量下发", 0
	}
	if rel.RolloutPct <= 0 {
		return false, "灰度比例为 0", 0
	}
	subject := releaseSubject(client)
	if subject == "" {
		return false, "未登录且无设备标识，不参与灰度", 0
	}
	bucket := releaseBucket(rel.ID, subject)
	if bucket >= rel.RolloutPct {
		return false, fmt.Sprintf("未进入灰度（第 %d 桶，放量 %d%%）", bucket, rel.RolloutPct), bucket
	}
	return true, fmt.Sprintf("进入灰度（第 %d 桶，放量 %d%%）", bucket, rel.RolloutPct), bucket
}

// checkReleases 从候选集（按版本码倒序）里挑出该下发的版本，合并跨越版本的说明与更新类型。
func checkReleases(candidates []appdomain.Release, viewer releaseViewer, now time.Time) (appdomain.ReleaseCheckResult, []appdomain.ReleaseDecision) {
	result := appdomain.ReleaseCheckResult{
		CurrentVersionCode: viewer.Client.VersionCode,
		CheckedAt:          now,
		NextCheckAfter:     releaseNextCheckDefault,
	}
	decisions := make([]appdomain.ReleaseDecision, 0, len(candidates))
	eligible := make([]*appdomain.Release, 0, 4)
	for i := range candidates {
		rel := &candidates[i]
		ok, reason, bucket := evaluateRelease(rel, viewer, now, false)
		decisions = append(decisions, appdomain.ReleaseDecision{
			ReleaseID: rel.ID, Version: rel.Version, VersionCode: rel.VersionCode,
			Eligible: ok, Reason: reason, Bucket: bucket,
		})
		if ok {
			eligible = append(eligible, rel)
		}
	}
	slices.SortStableFunc(eligible, func(a, b *appdomain.Release) int {
		switch {
		case a.VersionCode > b.VersionCode:
			return -1
		case a.VersionCode < b.VersionCode:
			return 1
		default:
			return 0
		}
	})
	if len(eligible) == 0 {
		return result, decisions
	}

	target := eligible[0]
	result.HasUpdate = true
	result.UpdateType = normalizeUpdateType(target.UpdateType)
	if result.UpdateType == appdomain.ReleaseUpdateForce {
		result.ForceReason = "release"
	}
	for i, rel := range eligible {
		if i < releaseChangelogLimit {
			result.Changelog = append(result.Changelog, appdomain.ReleaseChangelogEntry{
				Version: rel.Version, VersionCode: rel.VersionCode, Title: rel.Title,
				Summary: rel.Summary, Notes: rel.Notes, UpdateType: normalizeUpdateType(rel.UpdateType),
				PublishedAt: rel.PublishedAt,
			})
		}
		if appdomain.ReleaseUpdateRank(rel.UpdateType) > appdomain.ReleaseUpdateRank(result.UpdateType) {
			result.UpdateType = normalizeUpdateType(rel.UpdateType)
			if result.UpdateType == appdomain.ReleaseUpdateForce {
				result.ForceReason = "skipped"
			}
		}
		if rel.MinSupportedCode > 0 && viewer.Client.VersionCode < rel.MinSupportedCode &&
			result.UpdateType != appdomain.ReleaseUpdateForce {
			result.UpdateType = appdomain.ReleaseUpdateForce
			result.ForceReason = "unsupported"
		}
	}
	if result.UpdateType == appdomain.ReleaseUpdateForce {
		result.NextCheckAfter = releaseNextCheckForce
	}
	public := publicRelease(target)
	result.Release = &public
	result.Asset = pickReleaseAsset(public.Assets, viewer.Client.Abis)
	return result, decisions
}

// visibleReleases 对这个人可见的版本（不比较版本码）：官网最新版本与版本历史用。
func visibleReleases(candidates []appdomain.Release, viewer releaseViewer, now time.Time) []appdomain.Release {
	out := make([]appdomain.Release, 0, len(candidates))
	for i := range candidates {
		if ok, _, _ := evaluateRelease(&candidates[i], viewer, now, true); ok {
			out = append(out, candidates[i])
		}
	}
	return out
}

// pickReleaseAsset 按客户端 ABI 的偏好顺序挑包，没有匹配的退回通用包，再退回第一个。
func pickReleaseAsset(assets []appdomain.ReleaseAsset, abis []string) *appdomain.ReleaseAsset {
	if len(assets) == 0 {
		return nil
	}
	for _, abi := range abis {
		for i := range assets {
			if strings.EqualFold(assets[i].Abi, strings.TrimSpace(abi)) {
				return &assets[i]
			}
		}
	}
	for i := range assets {
		if assets[i].Abi == appdomain.ReleaseAssetUniversal {
			return &assets[i]
		}
	}
	return &assets[0]
}

func publicRelease(rel *appdomain.Release) appdomain.PublicRelease {
	// 副本里暂留落库地址：解析下载链接要用，出网前由 stripAssetSources 清掉
	assets := slices.Clone(rel.Assets)
	if assets == nil {
		assets = []appdomain.ReleaseAsset{}
	}
	return appdomain.PublicRelease{
		ID: rel.ID, Version: rel.Version, VersionCode: rel.VersionCode, Title: rel.Title,
		Notes: rel.Notes, Summary: rel.Summary, Platform: rel.Platform, MinOSVersion: rel.MinOSVersion,
		UpdateType: normalizeUpdateType(rel.UpdateType), Channel: rel.Channel, Assets: assets, PublishedAt: rel.PublishedAt,
	}
}

// stripAssetSources 清掉落库值（外链原文或 storage:// 引用），客户端只拿解析后的 downloadUrl。
func stripAssetSources(assets []appdomain.ReleaseAsset) {
	for i := range assets {
		assets[i].URL = ""
	}
}

func normalizeUpdateType(value string) string {
	if _, ok := appdomain.ValidReleaseUpdateTypes[value]; ok {
		return value
	}
	return appdomain.ReleaseUpdateOptional
}

func releaseContainsFold(list []string, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return slices.ContainsFunc(list, func(item string) bool { return strings.EqualFold(strings.TrimSpace(item), value) })
}

func localeMatches(prefixes []string, locale string) bool {
	locale = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(locale), "_", "-"))
	if locale == "" {
		return false
	}
	for _, prefix := range prefixes {
		p := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(prefix), "_", "-"))
		if p != "" && (locale == p || strings.HasPrefix(locale, p+"-")) {
			return true
		}
	}
	return false
}

func modelMatches(keywords []string, model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return false
	}
	for _, keyword := range keywords {
		if k := strings.ToLower(strings.TrimSpace(keyword)); k != "" && strings.Contains(model, k) {
			return true
		}
	}
	return false
}
