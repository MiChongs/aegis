package service

import (
	"context"
	"strings"
	"testing"
	"time"
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
