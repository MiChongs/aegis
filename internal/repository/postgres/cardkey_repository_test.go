package postgres

import (
	"os"
	"strings"
	"testing"
	"time"

	cardkeydomain "aegis/internal/domain/cardkey"
)

// TestRewardCatalogHasGrantBranch 双向钉死「权益目录 ↔ 发放分支」。
//
// 目录多一档 → 控制台配得上、核销时走进 default 分支报一句语焉不详的错；
// 发放多一档 → 那段代码永远不会被执行，因为控制台配不出这个类型。
// 两种漂移都不会在编译期暴露，也不会在任何一条业务用例里失败。
//
// 扫源码而不是把类型列成一个变量：列变量的话，「加了 case 却忘了加进变量」
// 与「加进变量却忘了加 case」仍然是两件可以各自发生的事，等于没有钉住。
func TestRewardCatalogHasGrantBranch(t *testing.T) {
	source, err := os.ReadFile("cardkey_redeem_repository.go")
	if err != nil {
		t.Fatalf("读取发放实现失败：%v", err)
	}
	text := string(source)

	// 目录 → 发放分支
	constNames := map[string]string{
		cardkeydomain.RewardVipPlan:      "RewardVipPlan",
		cardkeydomain.RewardVipDays:      "RewardVipDays",
		cardkeydomain.RewardIntegral:     "RewardIntegral",
		cardkeydomain.RewardExperience:   "RewardExperience",
		cardkeydomain.RewardBalance:      "RewardBalance",
		cardkeydomain.RewardLotteryDraws: "RewardLotteryDraws",
		cardkeydomain.RewardDeviceSlots:  "RewardDeviceSlots",
	}
	for _, spec := range cardkeydomain.RewardCatalog() {
		name, ok := constNames[spec.Type]
		if !ok {
			t.Errorf("权益 %s 在测试的常量表里没有登记 —— 新增权益时这张表也要跟着加一行", spec.Type)
			continue
		}
		if !strings.Contains(text, "case cardkeydomain."+name+":") {
			t.Errorf("权益「%s」（%s）在目录里，但 grantCardRewardsTx 没有对应的 case —— "+
				"控制台配得上却发不出来", spec.Label, spec.Type)
		}
	}

	// 发放分支 → 目录
	for _, name := range constNames {
		if !strings.Contains(text, "case cardkeydomain."+name+":") {
			continue
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "case cardkeydomain.Reward") {
			continue
		}
		constName := strings.TrimSuffix(strings.TrimPrefix(trimmed, "case cardkeydomain."), ":")
		found := false
		for _, registered := range constNames {
			if registered == constName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("grantCardRewardsTx 里有 %s 分支，但它不在权益目录里 —— 控制台配不出这个类型", constName)
		}
	}
}

func TestCardExpiredTreatsMissingExpiryAsPermanent(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name    string
		expires *time.Time
		want    bool
	}{
		{name: "没有到期时间即永久有效", expires: nil, want: false},
		{name: "到期时间在过去", expires: &past, want: true},
		{name: "到期时间在未来", expires: &future, want: false},
		// 边界：恰好到期算过期。反过来的话，一张「有效期到 12:00」的卡在 12:00:00
		// 这一秒仍然可用，而账单上写的是到 12:00 为止。
		{name: "恰好到期", expires: &now, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := &cardkeydomain.Card{ExpiresAt: tc.expires}
			if got := card.Expired(now); got != tc.want {
				t.Fatalf("Expired = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestResolveCardExpiryOnlyComputesForFirstUseMode(t *testing.T) {
	now := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)
	fixed := now.AddDate(0, 0, 90)

	// 「统一到期」与「永久」都返回 nil：前者在生成时就写进行里了，
	// 这里返回新值会被 COALESCE 采纳，把固定到期悄悄改成「从今天起算」。
	for _, mode := range []string{cardkeydomain.ValidityPermanent, cardkeydomain.ValidityFixedUntil} {
		batch := &cardkeydomain.Batch{ValidityMode: mode, ValidUntil: &fixed, ValidityDays: 30}
		if got := resolveCardExpiry(batch, now); got != nil {
			t.Errorf("%s 模式不应算出新的到期时间，实际 %v", mode, got)
		}
	}

	batch := &cardkeydomain.Batch{ValidityMode: cardkeydomain.ValidityFromFirstUse, ValidityDays: 30}
	got := resolveCardExpiry(batch, now)
	if got == nil {
		t.Fatal("激活即计时模式应当算出到期时间")
	}
	if want := now.AddDate(0, 0, 30); !got.Equal(want) {
		t.Fatalf("到期时间 = %v，期望 %v", got, want)
	}
}

// checkCardUsable 的顺序即错误优先级：一张被作废的过期卡应当报「已作废」，
// 报「已过期」会让客服给出错误的解释（引导续费，而实际上是被主动作废的）。
func TestCheckCardUsableErrorPriority(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	activeBatch := &cardkeydomain.Batch{Status: cardkeydomain.BatchActive}

	cases := []struct {
		name  string
		card  *cardkeydomain.Card
		batch *cardkeydomain.Batch
		want  error
	}{
		{
			name:  "作废优先于过期",
			card:  &cardkeydomain.Card{Status: cardkeydomain.StatusDisabled, ExpiresAt: &past},
			batch: activeBatch,
			want:  ErrCardKeyDisabled,
		},
		{
			name:  "批次停用等同作废",
			card:  &cardkeydomain.Card{Status: cardkeydomain.StatusUnused},
			batch: &cardkeydomain.Batch{Status: cardkeydomain.BatchDisabled},
			want:  ErrCardKeyDisabled,
		},
		{
			name:  "已用优先于过期",
			card:  &cardkeydomain.Card{Status: cardkeydomain.StatusUsed, ExpiresAt: &past},
			batch: activeBatch,
			want:  ErrCardKeyUsed,
		},
		{
			name:  "过期",
			card:  &cardkeydomain.Card{Status: cardkeydomain.StatusUnused, ExpiresAt: &past},
			batch: activeBatch,
			want:  ErrCardKeyExpired,
		},
		{
			name:  "可用",
			card:  &cardkeydomain.Card{Status: cardkeydomain.StatusUnused},
			batch: activeBatch,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkCardUsable(tc.card, tc.batch, now); got != tc.want {
				t.Fatalf("checkCardUsable = %v，期望 %v", got, tc.want)
			}
		})
	}
}
