package postgres

import (
	"os"
	"strings"
	"testing"

	"aegis/internal/domain/itemgrant"
)

// 物品目录与发放分支双向钉死：目录多一档 → 控制台配得上却发不出来；发放多一档 → 控制台配不出来。
func TestItemCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("item_grant_grant.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)
	constNames := map[string]string{
		itemgrant.ItemPoints:       "itemgrant.ItemPoints",
		itemgrant.ItemExperience:   "itemgrant.ItemExperience",
		itemgrant.ItemVipDays:      "itemgrant.ItemVipDays",
		itemgrant.ItemVipPlan:      "itemgrant.ItemVipPlan",
		itemgrant.ItemVipLifetime:  "itemgrant.ItemVipLifetime",
		itemgrant.ItemFeature:      "itemgrant.ItemFeature",
		itemgrant.ItemFeatureTime:  "itemgrant.ItemFeatureTime",
		itemgrant.ItemAdFree:       "itemgrant.ItemAdFree",
		itemgrant.ItemLotteryDraws: "itemgrant.ItemLotteryDraws",
		itemgrant.ItemAICredit:     "itemgrant.ItemAICredit",
		itemgrant.ItemBalance:      "itemgrant.ItemBalance",
		itemgrant.ItemLicenseDays:  "itemgrant.ItemLicenseDays",
		itemgrant.ItemDeviceSlots:  "itemgrant.ItemDeviceSlots",
		itemgrant.ItemManual:       "itemgrant.ItemManual",
		itemgrant.ItemNumberChange: "itemgrant.ItemNumberChange",
		itemgrant.ItemNumberTicket: "itemgrant.ItemNumberTicket",
		itemgrant.ItemNumberDraw:   "itemgrant.ItemNumberDraw",
		itemgrant.ItemNumberAssign: "itemgrant.ItemNumberAssign",
		itemgrant.ItemCosmetic:     "itemgrant.ItemCosmetic",
		itemgrant.ItemCoupon:       "itemgrant.ItemCoupon",
	}
	offered := map[string]bool{}
	for _, spec := range itemgrant.ItemCatalog() {
		offered[spec.Type] = true
		name, ok := constNames[spec.Type]
		if !ok {
			t.Fatalf("物品类型 %s 在测试的常量表里没有登记", spec.Type)
		}
		if !strings.Contains(text, "case "+name+":") {
			t.Errorf("物品目录开放了 %s，但发放实现里没有对应分支", spec.Type)
		}
	}
	for itemType, name := range constNames {
		if !offered[itemType] && strings.Contains(text, "case "+name+":") {
			t.Errorf("发放实现处理了 %s，但目录没有开放它 —— 控制台配不出来", itemType)
		}
	}
}
