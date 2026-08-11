package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/expr-lang/expr"
	gojson "github.com/goccy/go-json"
)

const signInRewardSettingsKey = "signInReward"

func (s *AppService) GetSignInRewardPolicy(ctx context.Context, appID int64) (*appdomain.SignInRewardPolicyView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &appdomain.SignInRewardPolicyView{
		AppID:   appID,
		AppName: app.Name,
		Policy:  resolveSignInRewardPolicy(app),
	}, nil
}

func (s *AppService) SetSignInRewardPolicy(ctx context.Context, appID int64, policy appdomain.SignInRewardPolicy) (*appdomain.SignInRewardPolicyView, error) {
	normalized, validationErrors := normalizeAndValidateSignInRewardPolicy(policy)
	if len(validationErrors) > 0 {
		return nil, apperrors.New(40067, http.StatusBadRequest, strings.Join(validationErrors, "; "))
	}
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[signInRewardSettingsKey] = signInRewardPolicyToMap(normalized)
	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	return s.GetSignInRewardPolicy(ctx, appID)
}

func (s *AppService) ResetSignInRewardPolicy(ctx context.Context, appID int64) (*appdomain.SignInRewardPolicyView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	delete(settings, signInRewardSettingsKey)
	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	return s.GetSignInRewardPolicy(ctx, appID)
}

func (s *AppService) GetSignInRewardTemplates() map[string]appdomain.SignInRewardPolicy {
	return map[string]appdomain.SignInRewardPolicy{
		"balanced":  defaultSignInRewardPolicy(),
		"growth":    growthSignInRewardPolicy(),
		"retention": retentionSignInRewardPolicy(),
	}
}

func (s *AppService) PreviewSignInReward(ctx context.Context, appID int64, input appdomain.SignInRewardPreviewInput) (*appdomain.SignInRewardPreview, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy := resolveSignInRewardPolicy(app)
	occurredAt := time.Now()
	if input.OccurredAt != nil && !input.OccurredAt.IsZero() {
		occurredAt = *input.OccurredAt
	}
	resolved, appliedRules, env, err := calculateSignInRewardWithPolicy(ctx, s.pg, policy, occurredAt, input.UserExperience, input.ConsecutiveDays, input.TotalSignIns)
	if err != nil {
		return nil, err
	}
	return &appdomain.SignInRewardPreview{
		AppID:        appID,
		AppName:      app.Name,
		OccurredAt:   occurredAt,
		Timezone:     policy.Timezone,
		Policy:       policy,
		Reward:       resolved,
		AppliedRules: appliedRules,
		Environment:  env,
	}, nil
}

func resolveSignInRewardPolicy(app *appdomain.App) appdomain.SignInRewardPolicy {
	policy := defaultSignInRewardPolicy()
	if app == nil || app.Settings == nil {
		policy.IsDefault = true
		return policy
	}
	raw := lookupMap(app.Settings, signInRewardSettingsKey)
	if raw == nil {
		policy.IsDefault = true
		return policy
	}

	payload, err := gojson.Marshal(raw)
	if err != nil {
		policy.IsDefault = true
		return policy
	}

	var stored appdomain.SignInRewardPolicy
	if err := gojson.Unmarshal(payload, &stored); err != nil {
		policy.IsDefault = true
		return policy
	}

	normalized, validationErrors := normalizeAndValidateSignInRewardPolicy(stored)
	if len(validationErrors) > 0 {
		policy.IsDefault = true
		return policy
	}
	return normalized
}

func calculateSignInRewardWithPolicy(ctx context.Context, pg experienceMultiplierProvider, policy appdomain.SignInRewardPolicy, now time.Time, userExperience int64, consecutiveDays int, totalSignIns int64) (appdomain.SignInRewardResolved, []appdomain.SignInRewardAppliedRule, map[string]any, error) {
	if reflect.DeepEqual(policy, appdomain.SignInRewardPolicy{}) {
		policy = defaultSignInRewardPolicy()
	}
	location, err := timeutil.LoadLocation(policy.Timezone)
	if err != nil {
		location = timeutil.DefaultLocation()
	}
	now = now.In(location)

	env := buildSignInRewardEnv(now, userExperience, consecutiveDays, totalSignIns)
	appliedRules := make([]appdomain.SignInRewardAppliedRule, 0, len(policy.Rules)+len(policy.Milestones))

	baseIntegral := policy.BaseIntegral
	if !policy.Enabled {
		return appdomain.SignInRewardResolved{
			BaseIntegral:     0,
			IntegralReward:   0,
			ExperienceReward: 0,
			RewardMultiplier: 0,
			BonusType:        "disabled",
			BonusDescription: "签到奖励已关闭",
		}, appliedRules, env, nil
	}

	integralMultiplier := 1.0
	experienceMultiplier := 1.0
	experienceReward := policy.BaseExperience
	if totalSignIns == 0 {
		experienceReward += policy.FirstSignInExperienceBonus
	}
	if consecutiveDays > 1 {
		bonus := int64(consecutiveDays-1) * policy.ConsecutiveExperienceStep
		if policy.ConsecutiveExperienceStepCap > 0 && bonus > policy.ConsecutiveExperienceStepCap {
			bonus = policy.ConsecutiveExperienceStepCap
		}
		experienceReward += bonus
	}

	matchedGroups := map[string]struct{}{}
	for _, rule := range policy.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.Group != "" {
			if _, ok := matchedGroups[rule.Group]; ok {
				continue
			}
		}
		matched, err := evaluateSignInRewardExpression(rule.Expression, env)
		if err != nil {
			return appdomain.SignInRewardResolved{}, nil, env, err
		}
		if !matched {
			continue
		}
		integralMultiplier += rule.IntegralMultiplierDelta
		experienceMultiplier += rule.ExperienceMultiplierDelta
		experienceReward += rule.ExperienceBonus
		appliedRules = append(appliedRules, appdomain.SignInRewardAppliedRule{
			Key:                       rule.Key,
			Name:                      rule.Name,
			Group:                     rule.Group,
			BonusType:                 rule.BonusType,
			Description:               strings.TrimSpace(rule.BonusDescription),
			Expression:                rule.Expression,
			IntegralMultiplierDelta:   rule.IntegralMultiplierDelta,
			IntegralBonus:             rule.IntegralBonus,
			ExperienceMultiplierDelta: rule.ExperienceMultiplierDelta,
			ExperienceBonus:           rule.ExperienceBonus,
		})
		baseIntegral += rule.IntegralBonus
		if rule.Group != "" {
			matchedGroups[rule.Group] = struct{}{}
		}
	}

	for _, milestone := range policy.Milestones {
		if milestone.ConsecutiveDays <= 0 || int64(consecutiveDays) != milestone.ConsecutiveDays {
			continue
		}
		baseIntegral += milestone.IntegralBonus
		experienceReward += milestone.ExperienceBonus
		appliedRules = append(appliedRules, appdomain.SignInRewardAppliedRule{
			Key:             fmt.Sprintf("milestone_%d", milestone.ConsecutiveDays),
			Name:            fmt.Sprintf("%d 天里程碑", milestone.ConsecutiveDays),
			BonusType:       milestone.BonusType,
			Description:     strings.TrimSpace(milestone.Description),
			IntegralBonus:   milestone.IntegralBonus,
			ExperienceBonus: milestone.ExperienceBonus,
			ConsecutiveDays: milestone.ConsecutiveDays,
		})
	}

	if integralMultiplier < 0 {
		integralMultiplier = 0
	}
	if experienceMultiplier < 0 {
		experienceMultiplier = 0
	}

	integralReward := int64(math.Floor(float64(baseIntegral) * integralMultiplier))
	if integralReward < 0 {
		integralReward = 0
	}
	experienceReward = int64(math.Floor(float64(experienceReward) * experienceMultiplier))
	if experienceReward < 0 {
		experienceReward = 0
	}

	if policy.ApplyLevelExperienceMultiplier && pg != nil {
		levelMultiplier, err := pg.GetExperienceMultiplier(ctx, userExperience)
		if err != nil {
			return appdomain.SignInRewardResolved{}, nil, env, err
		}
		experienceReward = int64(math.Floor(float64(experienceReward) * levelMultiplier))
	}

	if integralReward < 1 {
		integralReward = 1
	}
	if experienceReward < 1 {
		experienceReward = 1
	}
	if policy.MaxIntegralReward > 0 && integralReward > policy.MaxIntegralReward {
		integralReward = policy.MaxIntegralReward
	}
	if policy.MaxExperienceReward > 0 && experienceReward > policy.MaxExperienceReward {
		experienceReward = policy.MaxExperienceReward
	}

	bonusType, bonusDescription := summarizeSignInReward(appliedRules)
	return appdomain.SignInRewardResolved{
		BaseIntegral:     baseIntegral,
		IntegralReward:   integralReward,
		ExperienceReward: experienceReward,
		RewardMultiplier: integralMultiplier,
		BonusType:        bonusType,
		BonusDescription: bonusDescription,
	}, appliedRules, env, nil
}

type experienceMultiplierProvider interface {
	GetExperienceMultiplier(ctx context.Context, experience int64) (float64, error)
}

func evaluateSignInRewardExpression(expression string, env map[string]any) (bool, error) {
	program, err := expr.Compile(expression, expr.Env(defaultSignInRewardExprEnv()), expr.AsBool())
	if err != nil {
		return false, err
	}
	result, err := expr.Run(program, env)
	if err != nil {
		return false, err
	}
	value, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression %q does not return bool", expression)
	}
	return value, nil
}

func normalizeAndValidateSignInRewardPolicy(policy appdomain.SignInRewardPolicy) (appdomain.SignInRewardPolicy, []string) {
	if reflect.DeepEqual(policy, appdomain.SignInRewardPolicy{}) {
		return defaultSignInRewardPolicy(), nil
	}

	policy.Name = strings.TrimSpace(policy.Name)
	policy.Description = strings.TrimSpace(policy.Description)
	policy.Timezone = strings.TrimSpace(policy.Timezone)
	if policy.Name == "" {
		policy.Name = "自定义签到奖励策略"
	}
	if policy.Timezone == "" {
		policy.Timezone = timeutil.DefaultTimezone()
	}
	policy.Rules = cloneSignInRewardRules(policy.Rules)
	policy.Milestones = cloneSignInRewardMilestones(policy.Milestones)
	policy.IsDefault = false

	sort.Slice(policy.Rules, func(i, j int) bool {
		if policy.Rules[i].Priority == policy.Rules[j].Priority {
			return policy.Rules[i].Key < policy.Rules[j].Key
		}
		return policy.Rules[i].Priority < policy.Rules[j].Priority
	})
	sort.Slice(policy.Milestones, func(i, j int) bool {
		return policy.Milestones[i].ConsecutiveDays < policy.Milestones[j].ConsecutiveDays
	})

	errorsList := make([]string, 0)
	if _, err := timeutil.LoadLocation(policy.Timezone); err != nil {
		errorsList = append(errorsList, "timezone 无效")
	}
	if policy.BaseIntegral < 0 || policy.BaseIntegral > 1_000_000 {
		errorsList = append(errorsList, "baseIntegral 必须在 0-1000000 之间")
	}
	if policy.BaseExperience < 0 || policy.BaseExperience > 1_000_000 {
		errorsList = append(errorsList, "baseExperience 必须在 0-1000000 之间")
	}
	if policy.FirstSignInExperienceBonus < 0 || policy.FirstSignInExperienceBonus > 1_000_000 {
		errorsList = append(errorsList, "firstSignInExperienceBonus 必须在 0-1000000 之间")
	}
	if policy.ConsecutiveExperienceStep < 0 || policy.ConsecutiveExperienceStep > 100_000 {
		errorsList = append(errorsList, "consecutiveExperienceStep 必须在 0-100000 之间")
	}
	if policy.ConsecutiveExperienceStepCap < 0 || policy.ConsecutiveExperienceStepCap > 1_000_000 {
		errorsList = append(errorsList, "consecutiveExperienceStepCap 必须在 0-1000000 之间")
	}
	if policy.MaxIntegralReward < 0 || policy.MaxIntegralReward > 1_000_000 {
		errorsList = append(errorsList, "maxIntegralReward 必须在 0-1000000 之间")
	}
	if policy.MaxExperienceReward < 0 || policy.MaxExperienceReward > 1_000_000 {
		errorsList = append(errorsList, "maxExperienceReward 必须在 0-1000000 之间")
	}

	ruleKeys := map[string]struct{}{}
	for i := range policy.Rules {
		rule := &policy.Rules[i]
		rule.Key = strings.TrimSpace(rule.Key)
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Description = strings.TrimSpace(rule.Description)
		rule.Group = strings.TrimSpace(rule.Group)
		rule.Expression = strings.TrimSpace(rule.Expression)
		rule.BonusType = strings.TrimSpace(rule.BonusType)
		rule.BonusDescription = strings.TrimSpace(rule.BonusDescription)
		if rule.Key == "" {
			rule.Key = fmt.Sprintf("rule_%d", i+1)
		}
		if rule.Name == "" {
			rule.Name = rule.Key
		}
		if _, ok := ruleKeys[rule.Key]; ok {
			errorsList = append(errorsList, fmt.Sprintf("规则 key 重复: %s", rule.Key))
		}
		ruleKeys[rule.Key] = struct{}{}
		if rule.Priority == 0 {
			rule.Priority = i + 1
		}
		if rule.Enabled && rule.Expression == "" {
			errorsList = append(errorsList, fmt.Sprintf("规则 %s 缺少 expression", rule.Key))
		}
		if rule.Expression != "" {
			if _, err := expr.Compile(rule.Expression, expr.Env(defaultSignInRewardExprEnv()), expr.AsBool()); err != nil {
				errorsList = append(errorsList, fmt.Sprintf("规则 %s 表达式无效: %v", rule.Key, err))
			}
		}
		if rule.IntegralMultiplierDelta < -1 || rule.IntegralMultiplierDelta > 20 {
			errorsList = append(errorsList, fmt.Sprintf("规则 %s integralMultiplierDelta 超出范围", rule.Key))
		}
		if rule.ExperienceMultiplierDelta < -1 || rule.ExperienceMultiplierDelta > 20 {
			errorsList = append(errorsList, fmt.Sprintf("规则 %s experienceMultiplierDelta 超出范围", rule.Key))
		}
		if rule.IntegralBonus < -1_000_000 || rule.IntegralBonus > 1_000_000 {
			errorsList = append(errorsList, fmt.Sprintf("规则 %s integralBonus 超出范围", rule.Key))
		}
		if rule.ExperienceBonus < -1_000_000 || rule.ExperienceBonus > 1_000_000 {
			errorsList = append(errorsList, fmt.Sprintf("规则 %s experienceBonus 超出范围", rule.Key))
		}
	}

	milestoneDays := map[int64]struct{}{}
	for _, milestone := range policy.Milestones {
		if milestone.ConsecutiveDays <= 0 {
			errorsList = append(errorsList, "里程碑 consecutiveDays 必须大于 0")
			continue
		}
		if _, ok := milestoneDays[milestone.ConsecutiveDays]; ok {
			errorsList = append(errorsList, fmt.Sprintf("里程碑 %d 天重复", milestone.ConsecutiveDays))
		}
		milestoneDays[milestone.ConsecutiveDays] = struct{}{}
		if milestone.IntegralBonus < -1_000_000 || milestone.IntegralBonus > 1_000_000 {
			errorsList = append(errorsList, fmt.Sprintf("里程碑 %d integralBonus 超出范围", milestone.ConsecutiveDays))
		}
		if milestone.ExperienceBonus < -1_000_000 || milestone.ExperienceBonus > 1_000_000 {
			errorsList = append(errorsList, fmt.Sprintf("里程碑 %d experienceBonus 超出范围", milestone.ConsecutiveDays))
		}
	}

	return policy, errorsList
}

func signInRewardPolicyToMap(policy appdomain.SignInRewardPolicy) map[string]any {
	payload, _ := gojson.Marshal(policy)
	result := map[string]any{}
	_ = gojson.Unmarshal(payload, &result)
	return result
}

func defaultSignInRewardPolicy() appdomain.SignInRewardPolicy {
	return appdomain.SignInRewardPolicy{
		Name:                           "默认签到奖励策略",
		Description:                    "兼容当前系统默认签到奖励逻辑，支持连签阶梯、周末和月度特殊奖励。",
		Enabled:                        true,
		Timezone:                       timeutil.DefaultTimezone(),
		BaseIntegral:                   10,
		BaseExperience:                 20,
		FirstSignInExperienceBonus:     100,
		ConsecutiveExperienceStep:      2,
		ConsecutiveExperienceStepCap:   80,
		ApplyLevelExperienceMultiplier: true,
		Rules: []appdomain.SignInRewardRule{
			{Key: "monthly_master", Name: "30 天连签", Enabled: true, Priority: 10, Group: "streak-tier", Expression: "consecutive_days >= 30", BonusType: "monthly_master", BonusDescription: "月度签到达人，奖励翻 3 倍", IntegralMultiplierDelta: 2},
			{Key: "half_month", Name: "14 天连签", Enabled: true, Priority: 20, Group: "streak-tier", Expression: "consecutive_days >= 14", BonusType: "half_month", BonusDescription: "半月坚持奖励，奖励 2.5 倍", IntegralMultiplierDelta: 1.5},
			{Key: "weekly", Name: "7 天连签", Enabled: true, Priority: 30, Group: "streak-tier", Expression: "consecutive_days >= 7", BonusType: "weekly", BonusDescription: "一周连签奖励，奖励翻倍", IntegralMultiplierDelta: 1},
			{Key: "streak", Name: "3 天连签", Enabled: true, Priority: 40, Group: "streak-tier", Expression: "consecutive_days >= 3", BonusType: "streak", BonusDescription: "连续签到奖励，奖励 1.5 倍", IntegralMultiplierDelta: 0.5},
			{Key: "weekend", Name: "周末奖励", Enabled: true, Priority: 50, Expression: "is_weekend", BonusType: "weekend", BonusDescription: "周末奖励", IntegralMultiplierDelta: 0.5, ExperienceBonus: 15},
			{Key: "month_start", Name: "月初奖励", Enabled: true, Priority: 60, Expression: "day <= 3", BonusType: "month_start", BonusDescription: "月初奖励", IntegralMultiplierDelta: 0.3},
			{Key: "mid_month", Name: "月中奖励", Enabled: true, Priority: 70, Expression: "day == 15", BonusType: "mid_month", BonusDescription: "月中特殊奖励", IntegralMultiplierDelta: 0.5},
		},
		Milestones: []appdomain.SignInRewardMilestone{
			{ConsecutiveDays: 7, ExperienceBonus: 100, BonusType: "milestone", Description: "7 天里程碑奖励"},
			{ConsecutiveDays: 14, ExperienceBonus: 250, BonusType: "milestone", Description: "14 天里程碑奖励"},
			{ConsecutiveDays: 30, ExperienceBonus: 600, BonusType: "milestone", Description: "30 天里程碑奖励"},
			{ConsecutiveDays: 90, ExperienceBonus: 2000, BonusType: "milestone", Description: "90 天里程碑奖励"},
			{ConsecutiveDays: 365, ExperienceBonus: 10000, BonusType: "milestone", Description: "365 天里程碑奖励"},
		},
		IsDefault: true,
	}
}

func growthSignInRewardPolicy() appdomain.SignInRewardPolicy {
	policy := defaultSignInRewardPolicy()
	policy.Name = "增长型签到策略"
	policy.Description = "更强调新用户启动和周内活跃拉升。"
	policy.BaseIntegral = 12
	policy.BaseExperience = 24
	policy.FirstSignInExperienceBonus = 160
	policy.ConsecutiveExperienceStep = 3
	policy.ConsecutiveExperienceStepCap = 120
	policy.Rules = append(policy.Rules, appdomain.SignInRewardRule{
		Key: "workday_bonus", Name: "工作日冲刺", Enabled: true, Priority: 55,
		Expression: "weekday_iso >= 1 && weekday_iso <= 5 && consecutive_days >= 2",
		BonusType:  "workday_bonus", BonusDescription: "工作日冲刺奖励", IntegralMultiplierDelta: 0.2, ExperienceBonus: 20,
	})
	return policy
}

func retentionSignInRewardPolicy() appdomain.SignInRewardPolicy {
	policy := defaultSignInRewardPolicy()
	policy.Name = "留存型签到策略"
	policy.Description = "更强调长周期连续签到和里程碑奖励。"
	policy.BaseIntegral = 8
	policy.BaseExperience = 18
	policy.ConsecutiveExperienceStep = 4
	policy.ConsecutiveExperienceStepCap = 160
	policy.Milestones = []appdomain.SignInRewardMilestone{
		{ConsecutiveDays: 7, IntegralBonus: 20, ExperienceBonus: 120, BonusType: "milestone", Description: "7 天连签礼包"},
		{ConsecutiveDays: 14, IntegralBonus: 40, ExperienceBonus: 300, BonusType: "milestone", Description: "14 天连签礼包"},
		{ConsecutiveDays: 30, IntegralBonus: 100, ExperienceBonus: 800, BonusType: "milestone", Description: "30 天连签礼包"},
		{ConsecutiveDays: 60, IntegralBonus: 180, ExperienceBonus: 1500, BonusType: "milestone", Description: "60 天连签礼包"},
		{ConsecutiveDays: 180, IntegralBonus: 500, ExperienceBonus: 5000, BonusType: "milestone", Description: "180 天连签礼包"},
	}
	return policy
}

func buildSignInRewardEnv(now time.Time, userExperience int64, consecutiveDays int, totalSignIns int64) map[string]any {
	weekdayISO := int(now.Weekday())
	if weekdayISO == 0 {
		weekdayISO = 7
	}
	return map[string]any{
		"year":             now.Year(),
		"month":            int(now.Month()),
		"day":              now.Day(),
		"hour":             now.Hour(),
		"minute":           now.Minute(),
		"weekday":          int(now.Weekday()),
		"weekday_iso":      weekdayISO,
		"is_weekend":       now.Weekday() == time.Saturday || now.Weekday() == time.Sunday,
		"is_first_sign":    totalSignIns == 0,
		"consecutive_days": consecutiveDays,
		"total_sign_ins":   totalSignIns,
		"user_experience":  userExperience,
	}
}

func defaultSignInRewardExprEnv() map[string]any {
	return buildSignInRewardEnv(time.Date(2026, 3, 28, 12, 0, 0, 0, timeutil.DefaultLocation()), 0, 0, 0)
}

func summarizeSignInReward(appliedRules []appdomain.SignInRewardAppliedRule) (string, string) {
	if len(appliedRules) == 0 {
		return "normal", "普通签到奖励"
	}
	descriptions := make([]string, 0, len(appliedRules))
	bonusTypes := make([]string, 0, len(appliedRules))
	for _, item := range appliedRules {
		if item.BonusType != "" {
			bonusTypes = append(bonusTypes, item.BonusType)
		}
		if item.Description != "" {
			descriptions = append(descriptions, item.Description)
		}
	}
	bonusType := "compound"
	if len(bonusTypes) == 1 {
		bonusType = bonusTypes[0]
	}
	description := strings.Join(descriptions, " + ")
	if description == "" {
		description = "组合签到奖励"
	}
	return bonusType, description
}

func cloneSignInRewardRules(items []appdomain.SignInRewardRule) []appdomain.SignInRewardRule {
	if len(items) == 0 {
		return nil
	}
	out := make([]appdomain.SignInRewardRule, len(items))
	copy(out, items)
	return out
}

func cloneSignInRewardMilestones(items []appdomain.SignInRewardMilestone) []appdomain.SignInRewardMilestone {
	if len(items) == 0 {
		return nil
	}
	out := make([]appdomain.SignInRewardMilestone, len(items))
	copy(out, items)
	return out
}

func toUserSignInReward(resolved appdomain.SignInRewardResolved) user.SignInReward {
	return user.SignInReward{
		BaseIntegral:     resolved.BaseIntegral,
		IntegralReward:   resolved.IntegralReward,
		ExperienceReward: resolved.ExperienceReward,
		RewardMultiplier: resolved.RewardMultiplier,
		BonusType:        resolved.BonusType,
		BonusDescription: resolved.BonusDescription,
	}
}
