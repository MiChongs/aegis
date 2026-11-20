package postgres

import (
	"os"
	"strings"
	"testing"

	"aegis/internal/domain/pointstore"
)

// 积分商城的权益目录与发放分支双向钉死：
// 目录多一档 → 控制台配得上却发不出来（兑换时整笔回滚，用户只看到「兑换失败」）；
// 发放多一档 → 控制台配不出来。
func TestPointsStoreCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("points_store_repository.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)

	constNames := map[string]string{
		pointstore.RewardVipPlan:      "RewardVipPlan",
		pointstore.RewardVipDays:      "RewardVipDays",
		pointstore.RewardFeature:      "RewardFeature",
		pointstore.RewardExperience:   "RewardExperience",
		pointstore.RewardBalance:      "RewardBalance",
		pointstore.RewardLotteryDraws: "RewardLotteryDraws",
		pointstore.RewardManual:       "RewardManual",
		pointstore.RewardNumberChange: "RewardNumberChange",
		pointstore.RewardNumberTicket: "RewardNumberTicket",
		pointstore.RewardNumberDraw:   "RewardNumberDraw",
		pointstore.RewardCosmetic:     "RewardCosmetic",
	}
	offered := map[string]bool{}
	for _, spec := range pointstore.RewardCatalog() {
		offered[spec.Type] = true
		name, ok := constNames[spec.Type]
		if !ok {
			t.Fatalf("权益 %s 在测试的常量表里没有登记", spec.Type)
		}
		if !strings.Contains(text, "case pointstore."+name+":") {
			t.Errorf("积分商城目录开放了 %s，但发放实现里没有对应分支", spec.Type)
		}
	}
	for kind, name := range constNames {
		if !offered[kind] && strings.Contains(text, "case pointstore."+name+":") {
			t.Errorf("发放实现处理了 %s，但目录没有开放它 —— 控制台配不出来", kind)
		}
	}
}
