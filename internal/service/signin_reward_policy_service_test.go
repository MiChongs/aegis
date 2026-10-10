package service

import (
	"context"
	"strings"
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
)

func TestCalculateSignInRewardWithPolicy_DefaultPolicy(t *testing.T) {
	policy := defaultSignInRewardPolicy()
	policy.ApplyLevelExperienceMultiplier = false

	now := time.Date(2026, 3, 15, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	reward, appliedRules, env, err := calculateSignInRewardWithPolicy(context.Background(), nil, policy, now, 0, 7, 0)
	if err != nil {
		t.Fatalf("calculateSignInRewardWithPolicy returned error: %v", err)
	}

	if got := reward.IntegralReward; got != 30 {
		t.Fatalf("expected integral reward 30, got %d", got)
	}
	if got := reward.ExperienceReward; got != 247 {
		t.Fatalf("expected experience reward 247, got %d", got)
	}
	if got := reward.RewardMultiplier; got != 3 {
		t.Fatalf("expected reward multiplier 3, got %v", got)
	}
	if reward.BonusType != "compound" {
		t.Fatalf("expected bonus type compound, got %q", reward.BonusType)
	}
	if len(appliedRules) != 4 {
		t.Fatalf("expected 4 applied rules, got %d", len(appliedRules))
	}
	if value, ok := env["is_weekend"].(bool); !ok || !value {
		t.Fatalf("expected weekend env to be true, got %#v", env["is_weekend"])
	}
}

func TestNormalizeAndValidateSignInRewardPolicy_InvalidExpression(t *testing.T) {
	policy := defaultSignInRewardPolicy()
	policy.Rules[0].Expression = "consecutive_days >="

	_, errs := normalizeAndValidateSignInRewardPolicy(policy)
	if len(errs) == 0 {
		t.Fatal("expected validation errors, got none")
	}

	found := false
	for _, err := range errs {
		if strings.Contains(err, "表达式无效") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected expression validation error, got %v", errs)
	}
}

func TestSimulateSignInRewards_StreakTiersAndMilestones(t *testing.T) {
	policy := defaultSignInRewardPolicy()
	policy.ApplyLevelExperienceMultiplier = false
	policy.Timezone = "Asia/Shanghai"

	// 2026-03-02 是周一，推演 14 天覆盖两个周末、7 天与 14 天里程碑
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.FixedZone("CST", 8*3600))
	sim, err := simulateSignInRewards(context.Background(), nil, policy, start, appdomain.SignInRewardPreviewInput{
		ConsecutiveDays: 1,
		TotalSignIns:    0,
		SimulateDays:    14,
	})
	if err != nil {
		t.Fatalf("simulateSignInRewards returned error: %v", err)
	}
	if len(sim.Days) != 14 {
		t.Fatalf("expected 14 days, got %d", len(sim.Days))
	}
	first, seventh := sim.Days[0], sim.Days[6]
	if first.ConsecutiveDays != 1 || first.WeekdayISO != 1 || first.Date != "2026-03-02" {
		t.Fatalf("unexpected first day: %+v", first)
	}
	if !seventh.Milestone || seventh.ConsecutiveDays != 7 {
		t.Fatalf("expected day 7 to hit milestone, got %+v", seventh)
	}
	if sim.RuleHits["milestone_7"] != 1 || sim.RuleHits["milestone_14"] != 1 {
		t.Fatalf("expected one hit for each milestone, got %v", sim.RuleHits)
	}
	// 周末规则在两个周六两个周日命中
	if sim.RuleHits["weekend"] != 4 {
		t.Fatalf("expected weekend rule to hit 4 times, got %d", sim.RuleHits["weekend"])
	}
	var total int64
	for _, day := range sim.Days {
		total += day.IntegralReward
	}
	if total != sim.TotalIntegral || sim.Days[13].CumulativeIntegral != total {
		t.Fatalf("cumulative integral mismatch: total=%d sum=%d last=%d", sim.TotalIntegral, total, sim.Days[13].CumulativeIntegral)
	}
	// 首签经验加成只在第一天
	if first.ExperienceReward <= sim.Days[1].ExperienceReward-10 {
		t.Fatalf("expected first-sign bonus on day 1, got day1=%d day2=%d", first.ExperienceReward, sim.Days[1].ExperienceReward)
	}
}

func TestSimulateSignInRewards_CapsDays(t *testing.T) {
	policy := defaultSignInRewardPolicy()
	policy.ApplyLevelExperienceMultiplier = false
	sim, err := simulateSignInRewards(context.Background(), nil, policy, time.Now(), appdomain.SignInRewardPreviewInput{
		ConsecutiveDays: 1,
		SimulateDays:    1000,
	})
	if err != nil {
		t.Fatalf("simulateSignInRewards returned error: %v", err)
	}
	if len(sim.Days) != appdomain.SignInRewardMaxSimulateDays {
		t.Fatalf("expected simulation capped at %d days, got %d", appdomain.SignInRewardMaxSimulateDays, len(sim.Days))
	}
}

func TestNormalizeAndValidateSignInRewardPolicy_CamelCaseVariableRejected(t *testing.T) {
	// 表达式变量是下划线命名；控制台曾经用 consecutiveDays 作为新规则的默认表达式
	policy := defaultSignInRewardPolicy()
	policy.Rules = append(policy.Rules, appdomain.SignInRewardRule{Key: "camel", Name: "camel", Enabled: true, Expression: "consecutiveDays >= 1"})
	_, errs := normalizeAndValidateSignInRewardPolicy(policy)
	if len(errs) == 0 {
		t.Fatal("expected camelCase variable to be rejected")
	}
}
