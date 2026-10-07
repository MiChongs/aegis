package service

import (
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
)

func liveRelease(id int64, code int64, mutate func(*appdomain.Release)) appdomain.Release {
	published := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rel := appdomain.Release{
		ID: id, AppID: 10000, Version: "v", VersionCode: code, Platform: "android",
		UpdateType: appdomain.ReleaseUpdateOptional, Status: appdomain.ReleaseStatusPublished,
		Visibility: appdomain.ReleaseVisibilityPublic, RolloutPct: 100, PublishedAt: &published,
		Assets: []appdomain.ReleaseAsset{
			{ID: id*10 + 1, Abi: "universal"},
			{ID: id*10 + 2, Abi: "arm64-v8a"},
		},
	}
	if mutate != nil {
		mutate(&rel)
	}
	return rel
}

func viewerFor(client appdomain.ReleaseClient) releaseViewer {
	if client.Platform == "" {
		client.Platform = "android"
	}
	return releaseViewer{Client: client, ChannelIDs: map[int64]bool{}, DefaultChannelID: 1}
}

var engineNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func TestCheckPicksHighestEligibleAndMatchingAbi(t *testing.T) {
	candidates := []appdomain.Release{liveRelease(3, 300, nil), liveRelease(2, 200, nil)}
	result, _ := checkReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 100, Abis: []string{"arm64-v8a", "armeabi-v7a"}}), engineNow)
	if !result.HasUpdate || result.Release.VersionCode != 300 {
		t.Fatalf("expected 300, got %+v", result.Release)
	}
	if result.Asset == nil || result.Asset.Abi != "arm64-v8a" {
		t.Fatalf("expected arm64 asset, got %+v", result.Asset)
	}
	if len(result.Changelog) != 2 {
		t.Fatalf("changelog should cover both skipped releases, got %d", len(result.Changelog))
	}
}

func TestCheckFallsBackToUniversalAsset(t *testing.T) {
	result, _ := checkReleases([]appdomain.Release{liveRelease(3, 300, nil)}, viewerFor(appdomain.ReleaseClient{VersionCode: 1, Abis: []string{"x86"}}), engineNow)
	if result.Asset == nil || result.Asset.Abi != "universal" {
		t.Fatalf("expected universal fallback, got %+v", result.Asset)
	}
}

func TestCheckNoUpdateWhenCurrent(t *testing.T) {
	result, _ := checkReleases([]appdomain.Release{liveRelease(3, 300, nil)}, viewerFor(appdomain.ReleaseClient{VersionCode: 300}), engineNow)
	if result.HasUpdate {
		t.Fatal("same version must not be offered")
	}
}

// 跨越的版本里有一个强制更新，结果就是强制 —— 否则跳过安全修复的人永远不会被强制升级。
func TestCheckForceFromSkippedRelease(t *testing.T) {
	candidates := []appdomain.Release{
		liveRelease(3, 300, nil),
		liveRelease(2, 200, func(r *appdomain.Release) { r.UpdateType = appdomain.ReleaseUpdateForce }),
	}
	result, _ := checkReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 100}), engineNow)
	if result.UpdateType != appdomain.ReleaseUpdateForce || result.ForceReason != "skipped" {
		t.Fatalf("expected force/skipped, got %s/%s", result.UpdateType, result.ForceReason)
	}
	// 已经越过那个强制版本的人不受影响
	result, _ = checkReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 200}), engineNow)
	if result.UpdateType != appdomain.ReleaseUpdateOptional {
		t.Fatalf("client past the forced release should be optional, got %s", result.UpdateType)
	}
}

func TestCheckForceWhenBelowMinSupported(t *testing.T) {
	candidates := []appdomain.Release{liveRelease(3, 300, func(r *appdomain.Release) { r.MinSupportedCode = 150 })}
	result, _ := checkReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 100}), engineNow)
	if result.UpdateType != appdomain.ReleaseUpdateForce || result.ForceReason != "unsupported" {
		t.Fatalf("expected force/unsupported, got %s/%s", result.UpdateType, result.ForceReason)
	}
	result, _ = checkReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 160}), engineNow)
	if result.UpdateType != appdomain.ReleaseUpdateOptional {
		t.Fatalf("supported client should stay optional, got %s", result.UpdateType)
	}
}

func TestVisibilityAndTesters(t *testing.T) {
	signedIn := liveRelease(5, 500, func(r *appdomain.Release) { r.Visibility = appdomain.ReleaseVisibilitySignedIn })
	if ok, _, _ := evaluateRelease(&signedIn, viewerFor(appdomain.ReleaseClient{VersionCode: 1}), engineNow, false); ok {
		t.Fatal("signed_in release must not reach anonymous clients")
	}
	if ok, _, _ := evaluateRelease(&signedIn, viewerFor(appdomain.ReleaseClient{VersionCode: 1, UserID: 7}), engineNow, false); !ok {
		t.Fatal("signed_in release should reach a signed-in user")
	}

	testers := liveRelease(6, 600, func(r *appdomain.Release) {
		r.Visibility = appdomain.ReleaseVisibilityTesters
		r.RolloutPct = 0
		r.Targeting.Regions = []string{"JP"}
		r.Targeting.TesterDeviceIDs = []string{"dev-qa"}
	})
	if ok, _, _ := evaluateRelease(&testers, viewerFor(appdomain.ReleaseClient{VersionCode: 1, UserID: 7}), engineNow, false); ok {
		t.Fatal("testers-only release must not reach ordinary users")
	}
	// 内测设备跳过定向与灰度
	if ok, _, _ := evaluateRelease(&testers, viewerFor(appdomain.ReleaseClient{VersionCode: 1, DeviceID: "dev-qa", Region: "CN"}), engineNow, false); !ok {
		t.Fatal("tester device should bypass targeting and rollout")
	}
	testers.Targeting.ExcludeDeviceIDs = []string{"dev-qa"}
	if ok, _, _ := evaluateRelease(&testers, viewerFor(appdomain.ReleaseClient{VersionCode: 1, DeviceID: "dev-qa"}), engineNow, false); ok {
		t.Fatal("exclusion must win over tester list")
	}
}

func TestTargetingFilters(t *testing.T) {
	rel := liveRelease(7, 700, func(r *appdomain.Release) {
		r.Targeting = appdomain.ReleaseTargeting{
			Regions: []string{"CN"}, Locales: []string{"zh"}, DeviceModels: []string{"pixel"},
			Abis: []string{"arm64-v8a"}, MinOSVersion: 26, MaxOSVersion: 36,
			MinSourceVersionCode: 100, MaxSourceVersionCode: 500,
		}
	})
	match := appdomain.ReleaseClient{VersionCode: 200, Region: "cn", Locale: "zh_CN", DeviceModel: "Google Pixel 9", Abis: []string{"arm64-v8a"}, OSVersion: 35}
	if ok, reason, _ := evaluateRelease(&rel, viewerFor(match), engineNow, false); !ok {
		t.Fatalf("matching client rejected: %s", reason)
	}
	cases := map[string]func(c *appdomain.ReleaseClient){
		"region":  func(c *appdomain.ReleaseClient) { c.Region = "US" },
		"locale":  func(c *appdomain.ReleaseClient) { c.Locale = "en-US" },
		"model":   func(c *appdomain.ReleaseClient) { c.DeviceModel = "SM-S928" },
		"abi":     func(c *appdomain.ReleaseClient) { c.Abis = []string{"x86_64"} },
		"osLow":   func(c *appdomain.ReleaseClient) { c.OSVersion = 24 },
		"osHigh":  func(c *appdomain.ReleaseClient) { c.OSVersion = 37 },
		"srcLow":  func(c *appdomain.ReleaseClient) { c.VersionCode = 50 },
		"srcHigh": func(c *appdomain.ReleaseClient) { c.VersionCode = 600 },
	}
	for name, mutate := range cases {
		client := match
		mutate(&client)
		if ok, _, _ := evaluateRelease(&rel, viewerFor(client), engineNow, false); ok {
			t.Errorf("%s: client should be filtered out", name)
		}
	}
}

func TestChannelMembership(t *testing.T) {
	beta := liveRelease(8, 800, func(r *appdomain.Release) { r.Channel = &appdomain.ReleaseChannelRef{ID: 9, Name: "Beta"} })
	viewer := viewerFor(appdomain.ReleaseClient{VersionCode: 1, UserID: 7})
	if ok, _, _ := evaluateRelease(&beta, viewer, engineNow, false); ok {
		t.Fatal("non-member must not get the beta channel release")
	}
	viewer.ChannelIDs[9] = true
	if ok, _, _ := evaluateRelease(&beta, viewer, engineNow, false); !ok {
		t.Fatal("member should get the beta channel release")
	}
	beta.Channel.ID = 1 // 默认渠道 = 所有人
	if ok, _, _ := evaluateRelease(&beta, viewerFor(appdomain.ReleaseClient{VersionCode: 1}), engineNow, false); !ok {
		t.Fatal("default channel release should reach everyone")
	}
}

// 灰度从 10% 调到 50%，原来那 10% 的人必须仍在里面；比例大致接近设定值。
func TestRolloutIsStableAndMonotonic(t *testing.T) {
	rel := liveRelease(11, 1100, func(r *appdomain.Release) { r.RolloutPct = 10 })
	inAt10 := map[int64]bool{}
	for user := int64(1); user <= 2000; user++ {
		if ok, _, _ := evaluateRelease(&rel, viewerFor(appdomain.ReleaseClient{VersionCode: 1, UserID: user}), engineNow, false); ok {
			inAt10[user] = true
		}
	}
	if n := len(inAt10); n < 120 || n > 280 {
		t.Fatalf("10%% rollout admitted %d of 2000", n)
	}
	rel.RolloutPct = 50
	for user := range inAt10 {
		if ok, _, _ := evaluateRelease(&rel, viewerFor(appdomain.ReleaseClient{VersionCode: 1, UserID: user}), engineNow, false); !ok {
			t.Fatalf("user %d dropped out when rollout widened", user)
		}
	}
	// 没有主体（未登录且无设备标识）不参与灰度
	if ok, _, _ := evaluateRelease(&rel, viewerFor(appdomain.ReleaseClient{VersionCode: 1}), engineNow, false); ok {
		t.Fatal("anonymous client without device id must not join a partial rollout")
	}
}

func TestScheduledReleaseGoesLiveAtPublishAt(t *testing.T) {
	future := engineNow.Add(time.Hour)
	rel := liveRelease(12, 1200, func(r *appdomain.Release) {
		r.Status = appdomain.ReleaseStatusScheduled
		r.PublishAt = &future
	})
	viewer := viewerFor(appdomain.ReleaseClient{VersionCode: 1})
	if ok, _, _ := evaluateRelease(&rel, viewer, engineNow, false); ok {
		t.Fatal("scheduled release must wait for publishAt")
	}
	if ok, _, _ := evaluateRelease(&rel, viewer, future.Add(time.Second), false); !ok {
		t.Fatal("scheduled release should be live after publishAt")
	}
	rel.Status = appdomain.ReleaseStatusPaused
	if ok, _, _ := evaluateRelease(&rel, viewer, future.Add(time.Second), false); ok {
		t.Fatal("paused release must not be offered")
	}
}

func TestVisibleReleasesIgnoreVersionButKeepAudience(t *testing.T) {
	candidates := []appdomain.Release{
		liveRelease(13, 1300, func(r *appdomain.Release) { r.RolloutPct = 20 }),
		liveRelease(14, 1250, nil),
		liveRelease(15, 1200, func(r *appdomain.Release) { r.Platform = "ios" }),
	}
	visible := visibleReleases(candidates, viewerFor(appdomain.ReleaseClient{VersionCode: 99999}), engineNow)
	if len(visible) != 1 || visible[0].ID != 14 {
		t.Fatalf("anonymous visitor should only see the fully rolled-out android release, got %+v", visible)
	}
}
