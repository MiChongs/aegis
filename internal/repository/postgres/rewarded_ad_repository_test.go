package postgres

import (
	"os"
	"strings"
	"testing"
	"time"

	cardkeydomain "aegis/internal/domain/cardkey"
	rewardedad "aegis/internal/domain/rewardedad"
)

// TestRewardedAdCatalogHasGrantBranch 双向钉死「激励广告权益目录 ↔ 发放分支」，理由同卡密。
func TestRewardedAdCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("rewarded_ad_settle_repository.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)

	constNames := map[string]string{
		cardkeydomain.RewardVipPlan:      "RewardVipPlan",
		cardkeydomain.RewardVipDays:      "RewardVipDays",
		cardkeydomain.RewardIntegral:     "RewardIntegral",
		cardkeydomain.RewardExperience:   "RewardExperience",
		cardkeydomain.RewardLotteryDraws: "RewardLotteryDraws",
		cardkeydomain.RewardBalance:      "RewardBalance",
		cardkeydomain.RewardDeviceSlots:  "RewardDeviceSlots",
	}
	offered := map[string]bool{}
	for _, spec := range rewardedad.RewardCatalog() {
		offered[spec.Type] = true
		name, ok := constNames[spec.Type]
		if !ok {
			t.Fatalf("权益 %s 在测试的常量表里没有登记", spec.Type)
		}
		if !strings.Contains(text, "case cardkeydomain."+name+":") {
			t.Errorf("激励广告目录开放了 %s，但发放实现里没有对应分支", spec.Type)
		}
	}
	for kind, name := range constNames {
		if !offered[kind] && strings.Contains(text, "case cardkeydomain."+name+":") {
			t.Errorf("发放实现处理了 %s，但激励广告目录没有开放它 —— 控制台配不出来", kind)
		}
	}
}

// 合并两方事实时，后到的一方不能覆盖先到一方的那一半。
func TestMergeRewardedAdFactsKeepsEachSide(t *testing.T) {
	now := time.Now().UTC()
	verified := true
	view := &rewardedad.View{Status: rewardedad.StatusPending}

	mergeRewardedAdFacts(view, rewardedad.RecordInput{Side: rewardedad.SideClient, Scene: "vip",
		PlacementID: "p1", ClientVerified: &verified, DeviceID: "dev", ClientIP: "1.2.3.4"}, now)
	mergeRewardedAdFacts(view, rewardedad.RecordInput{Side: rewardedad.SideServer, Scene: "points",
		PlacementID: "p2", RewardName: "会员", RewardAmount: 1, Extra: `{"aegisScene":"points"}`}, now)

	if view.Scene != "vip" {
		t.Fatalf("场景应以客户端上报为准，得到 %q", view.Scene)
	}
	if view.PlacementID != "p2" {
		t.Fatalf("广告位应以平台回调为准，得到 %q", view.PlacementID)
	}
	if view.ServerVerifiedAt == nil || view.ClientReportedAt == nil {
		t.Fatal("两方的到达时间都应记录")
	}
	if view.DeviceID != "dev" || view.RewardName != "会员" {
		t.Fatal("两方各自的字段都应保留")
	}

	first := *view.ClientReportedAt
	later := now.Add(time.Minute)
	mergeRewardedAdFacts(view, rewardedad.RecordInput{Side: rewardedad.SideClient, Scene: "other"}, later)
	if !view.ClientReportedAt.Equal(first) {
		t.Fatal("重复上报不应刷新首次上报时间")
	}
}

// 服务端先到、客户端还没说是哪个场景时，回调里推出的场景只补位。
func TestMergeRewardedAdFactsServerSceneOnlyFillsBlank(t *testing.T) {
	view := &rewardedad.View{Status: rewardedad.StatusPending}
	mergeRewardedAdFacts(view, rewardedad.RecordInput{Side: rewardedad.SideServer, Scene: "points"}, time.Now())
	if view.Scene != "points" {
		t.Fatalf("空场景应由回调补位，得到 %q", view.Scene)
	}
	mergeRewardedAdFacts(view, rewardedad.RecordInput{Side: rewardedad.SideClient, Scene: "vip"}, time.Now())
	if view.Scene != "vip" {
		t.Fatalf("pending 时客户端上报的场景应覆盖回调推测，得到 %q", view.Scene)
	}
}
