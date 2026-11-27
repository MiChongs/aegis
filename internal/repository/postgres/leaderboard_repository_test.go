package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/leaderboard"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
)

// 排行榜奖励复用邀请的权益目录，发放分支与目录双向钉死：目录多一档 → 控制台配得上，结算时却每份都失败；
// 发放多一档 → 控制台配不出来。
func TestLeaderboardCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("leaderboard_grant.go")
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
			t.Errorf("权益目录开放了 %s，但排行榜的发放实现里没有对应分支", spec.Type)
		}
	}
	for kind, name := range constNames {
		if !offered[kind] && strings.Contains(text, "case "+name+":") {
			t.Errorf("排行榜的发放实现处理了 %s，但目录没有开放它 —— 控制台配不出来", kind)
		}
	}
}

// 周期榜只统计「获得的」积分：每个写积分流水的分类都必须在 leaderboard 的分类目录里登记
// （计入 / 不计入 / 管理员调整），漏登记的新分类会被当成「获得」—— 若它其实是退回或冲正，
// 周期榜就会被刷。这里扫描本包全部写 integral_transactions 的地方，把用到的分类逐一核对。
func TestIntegralCategoriesCoverEveryWriter(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	identifiers := map[string]string{
		"pointstore.IntegralCategoryRedeem": pointstore.IntegralCategoryRedeem,
		"pointstore.IntegralCategoryRefund": pointstore.IntegralCategoryRefund,
		"invite.IntegralCategory":           invite.IntegralCategory,
		"leaderboard.IntegralCategory":      leaderboard.IntegralCategory,
		"lotterydomain.IntegralCategory":    lotterydomain.IntegralCategory,
		"vipConversionIntegralCategory":     vipConversionIntegralCategory,
		"itemgrant.IntegralCategory":        itemgrant.IntegralCategory,
	}
	callPattern := regexp.MustCompile(`applyIntegralChangeTx\(ctx, tx,(?s:.{0,300}?)"(?:earn|consume)",\s*([^,\s]+),`)
	structPattern := regexp.MustCompile(`integralChange\{(?s:.{0,300}?)Category:\s*([^,\s]+),`)
	sqlPattern := regexp.MustCompile(`INSERT INTO integral_transactions(?s:.{0,700}?)(?:NOW\(\)\)|RETURNING)`)
	literalPattern := regexp.MustCompile(`'([a-z_]+)'`)
	// SQL 里出现、但不是分类的字面量（类型、状态、来源类型）
	notCategory := map[string]bool{"earn": true, "consume": true, "completed": true, "admin_manual": true}

	found := map[string]string{}
	record := func(file, expr string) {
		expr = strings.TrimSpace(expr)
		switch {
		case strings.HasPrefix(expr, `"`):
			found[strings.Trim(expr, `"`)] = file
		case expr == "in.Category" || expr == "category":
			// applyIntegralChangeTx 自己把参数转交给 applyIntegralChangeTxNo
		default:
			value, ok := identifiers[expr]
			if !ok {
				t.Errorf("%s：积分分类 %s 不是字面量，也不在测试的常量表里 —— 先登记它", file, expr)
				return
			}
			found[value] = file
		}
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, match := range callPattern.FindAllStringSubmatch(text, -1) {
			record(file, match[1])
		}
		for _, match := range structPattern.FindAllStringSubmatch(text, -1) {
			record(file, match[1])
		}
		for _, statement := range sqlPattern.FindAllString(text, -1) {
			for _, literal := range literalPattern.FindAllStringSubmatch(statement, -1) {
				if !notCategory[literal[1]] {
					found[literal[1]] = file
				}
			}
		}
	}
	if len(found) < 8 {
		t.Fatalf("只找到 %d 个分类，扫描规则大概失效了：%v", len(found), found)
	}
	for category, file := range found {
		if _, ok := leaderboard.FindCategory(category); !ok {
			t.Errorf("%s 写入的积分分类 %q 没有在 leaderboard 的分类目录里登记（计入 / 不计入 / 管理员调整）", file, category)
		}
	}
	// 反方向：目录里登记的分类都应当真有人在写（否则是死条目，让人误以为某个口径还在生效）
	for _, rule := range leaderboard.IntegralCategories() {
		if _, ok := found[rule.Category]; !ok {
			t.Errorf("分类目录里的 %s 没有任何写入方", rule.Category)
		}
	}
}
