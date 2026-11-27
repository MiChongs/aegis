package postgres

import (
	"os"
	"strings"
	"testing"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
)

// 邀请的权益目录与发放分支双向钉死：目录多一档 → 控制台配得上，填写邀请码时却整笔回滚；
// 发放多一档 → 控制台配不出来。
func TestInviteCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("invite_repository.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)
	constNames := map[string]string{
		invite.RewardPoints:           "invite.RewardPoints",
		pointstore.RewardVipPlan:      "pointstore.RewardVipPlan",
		pointstore.RewardVipDays:      "pointstore.RewardVipDays",
		pointstore.RewardFeature:      "pointstore.RewardFeature",
		pointstore.RewardExperience:   "pointstore.RewardExperience",
		pointstore.RewardBalance:      "pointstore.RewardBalance",
		pointstore.RewardLotteryDraws: "pointstore.RewardLotteryDraws",
		pointstore.RewardManual:       "pointstore.RewardManual",
		pointstore.RewardNumberChange: "pointstore.RewardNumberChange",
		pointstore.RewardNumberTicket: "pointstore.RewardNumberTicket",
		pointstore.RewardNumberDraw:   "pointstore.RewardNumberDraw",
		pointstore.RewardCosmetic:     "pointstore.RewardCosmetic",
		pointstore.RewardCoupon:       "pointstore.RewardCoupon",
	}
	offered := map[string]bool{}
	for _, spec := range invite.RewardCatalog() {
		offered[spec.Type] = true
		name, ok := constNames[spec.Type]
		if !ok {
			t.Fatalf("权益 %s 在测试的常量表里没有登记", spec.Type)
		}
		if !strings.Contains(text, "case "+name+":") {
			t.Errorf("邀请目录开放了 %s，但发放实现里没有对应分支", spec.Type)
		}
	}
	for kind, name := range constNames {
		if !offered[kind] && strings.Contains(text, "case "+name+":") {
			t.Errorf("发放实现处理了 %s，但目录没有开放它 —— 控制台配不出来", kind)
		}
	}
}
