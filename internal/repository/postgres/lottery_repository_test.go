package postgres

import (
	"os"
	"strings"
	"testing"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖的奖品目录与发放分支双向钉死：目录多一档 → 控制台配得上，抽中时却整次回滚；
// 发放多一档 → 控制台配不出来。
func TestLotteryCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("lottery_repository.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)
	constNames := map[string]string{
		lotterydomain.KindNone:         "lotterydomain.KindNone",
		lotterydomain.KindPoints:       "lotterydomain.KindPoints",
		lotterydomain.KindExperience:   "lotterydomain.KindExperience",
		lotterydomain.KindVipDays:      "lotterydomain.KindVipDays",
		lotterydomain.KindVipPlan:      "lotterydomain.KindVipPlan",
		lotterydomain.KindFeature:      "lotterydomain.KindFeature",
		lotterydomain.KindLotteryDraws: "lotterydomain.KindLotteryDraws",
		lotterydomain.KindManual:       "lotterydomain.KindManual",
		lotterydomain.KindVipLifetime:  "lotterydomain.KindVipLifetime",
		lotterydomain.KindFeatureTime:  "lotterydomain.KindFeatureTime",
		lotterydomain.KindAdFree:       "lotterydomain.KindAdFree",
		lotterydomain.KindNumberChange: "lotterydomain.KindNumberChange",
		lotterydomain.KindNumberTicket: "lotterydomain.KindNumberTicket",
		lotterydomain.KindNumberDraw:   "lotterydomain.KindNumberDraw",
		lotterydomain.KindCosmetic:     "lotterydomain.KindCosmetic",
		lotterydomain.KindCoupon:       "lotterydomain.KindCoupon",
	}
	offered := map[string]bool{}
	for _, spec := range lotterydomain.Catalog() {
		offered[spec.Type] = true
		name, ok := constNames[spec.Type]
		if !ok {
			t.Fatalf("奖品类型 %s 在测试的常量表里没有登记", spec.Type)
		}
		if !strings.Contains(text, "case "+name+":") {
			t.Errorf("奖品目录开放了 %s，但抽奖的发放实现里没有对应分支", spec.Type)
		}
	}
	for kind, name := range constNames {
		if !offered[kind] && strings.Contains(text, "case "+name+":") {
			t.Errorf("抽奖的发放实现处理了 %s，但目录没有开放它 —— 控制台配不出来", kind)
		}
	}
}
