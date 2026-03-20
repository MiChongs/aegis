package service

import (
	"context"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	appdomain "aegis/internal/domain/app"
	apperrors "aegis/pkg/errors"
)

func (s *AppService) GetPasswordPolicy(ctx context.Context, appID int64) (*appdomain.PasswordPolicyView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy := s.ResolvePasswordPolicy(app)
	stats, err := s.pg.GetPasswordPolicyStats(ctx, appID, policy.MinScore)
	if err != nil {
		return nil, err
	}
	return &appdomain.PasswordPolicyView{
		AppID:   appID,
		AppName: app.Name,
		Policy:  policy,
		Stats:   stats,
	}, nil
}

func (s *AppService) SetPasswordPolicy(ctx context.Context, appID int64, policy appdomain.PasswordPolicy) (*appdomain.PasswordPolicyView, error) {
	normalized, validationErrors := normalizeAndValidatePasswordPolicy(policy)
	if len(validationErrors) > 0 {
		return nil, apperrors.New(40027, http.StatusBadRequest, strings.Join(validationErrors, "; "))
	}
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings["passwordPolicy"] = passwordPolicyToMap(normalized)
	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	return s.GetPasswordPolicy(ctx, appID)
}

func (s *AppService) ResetPasswordPolicy(ctx context.Context, appID int64) (*appdomain.PasswordPolicyView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	delete(settings, "passwordPolicy")
	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	return s.GetPasswordPolicy(ctx, appID)
}

func (s *AppService) ResolvePasswordPolicy(app *appdomain.App) appdomain.PasswordPolicy {
	policy := defaultPasswordPolicy()
	if app == nil || app.Settings == nil {
		policy.IsDefault = true
		return policy
	}
	raw, ok := app.Settings["passwordPolicy"]
	if !ok || raw == nil {
		policy.IsDefault = true
		return policy
	}
	typed, ok := raw.(map[string]any)
	if !ok {
		policy.IsDefault = true
		return policy
	}
	if value := strings.TrimSpace(stringSetting(typed, "name")); value != "" {
		policy.Name = value
	}
	if value := strings.TrimSpace(stringSetting(typed, "description")); value != "" {
		policy.Description = value
	}
	if value := intSetting(typed, "minLength"); value > 0 {
		policy.MinLength = value
	}
	if value := intSetting(typed, "maxLength"); value > 0 {
		policy.MaxLength = value
	}
	if value, ok := lookupBool(typed, "requireUppercase"); ok {
		policy.RequireUppercase = value
	}
	if value, ok := lookupBool(typed, "requireLowercase"); ok {
		policy.RequireLowercase = value
	}
	if value, ok := lookupBool(typed, "requireNumbers"); ok {
		policy.RequireNumbers = value
	}
	if value, ok := lookupBool(typed, "requireSpecialChars"); ok {
		policy.RequireSpecialChars = value
	}
	if value := intSetting(typed, "minScore"); value >= 0 {
		policy.MinScore = value
	}
	if value := intSetting(typed, "maxAge"); value > 0 {
		policy.MaxAge = value
	}
	if value := intSetting(typed, "preventReuse"); value >= 0 {
		policy.PreventReuse = value
	}
	policy.IsDefault = false
	return policy
}

func (s *AppService) GetPasswordPolicyTemplates() map[string]appdomain.PasswordPolicy {
	return map[string]appdomain.PasswordPolicy{
		"basic": {
			Name:                "基础策略",
			Description:         "适用于一般应用的基础密码要求",
			MinLength:           6,
			MaxLength:           128,
			RequireUppercase:    false,
			RequireLowercase:    true,
			RequireNumbers:      true,
			RequireSpecialChars: false,
			MinScore:            30,
			MaxAge:              365,
			PreventReuse:        3,
		},
		"standard": {
			Name:                "标准策略",
			Description:         "平衡安全性和用户体验的标准配置",
			MinLength:           8,
			MaxLength:           128,
			RequireUppercase:    true,
			RequireLowercase:    true,
			RequireNumbers:      true,
			RequireSpecialChars: false,
			MinScore:            50,
			MaxAge:              180,
			PreventReuse:        5,
		},
		"strict": {
			Name:                "严格策略",
			Description:         "高安全要求的严格密码策略",
			MinLength:           12,
			MaxLength:           128,
			RequireUppercase:    true,
			RequireLowercase:    true,
			RequireNumbers:      true,
			RequireSpecialChars: true,
			MinScore:            70,
			MaxAge:              90,
			PreventReuse:        10,
		},
		"enterprise": {
			Name:                "企业策略",
			Description:         "企业级安全要求的密码策略",
			MinLength:           14,
			MaxLength:           128,
			RequireUppercase:    true,
			RequireLowercase:    true,
			RequireNumbers:      true,
			RequireSpecialChars: true,
			MinScore:            80,
			MaxAge:              60,
			PreventReuse:        15,
		},
	}
}

func (s *AppService) TestPasswordPolicy(ctx context.Context, appID int64, password string) (*appdomain.PasswordPolicyTestResult, error) {
	view, err := s.GetPasswordPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	analysis := AnalyzePasswordStrength(password)
	check := CheckPasswordPolicy(password, view.Policy)
	return &appdomain.PasswordPolicyTestResult{
		Password:         strings.Repeat("*", len(password)),
		Policy:           view.Policy,
		StrengthAnalysis: analysis,
		PolicyCheck:      check,
		Result: appdomain.PasswordPolicyTestSummary{
			IsValid:         check.IsValid,
			Score:           analysis.Score,
			Level:           analysis.Level,
			Violations:      check.Violations,
			Recommendations: analysis.Recommendations,
		},
	}, nil
}

func (s *AppService) ValidatePasswordWithAppPolicy(ctx context.Context, appID int64, password string) error {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	check := CheckPasswordPolicy(password, s.ResolvePasswordPolicy(app))
	if check.IsValid {
		return nil
	}
	return apperrors.New(40007, http.StatusBadRequest, strings.Join(check.Violations, "; "))
}

func defaultPasswordPolicy() appdomain.PasswordPolicy {
	return appdomain.PasswordPolicy{
		Name:                "默认密码策略",
		MinLength:           8,
		MaxLength:           128,
		RequireUppercase:    false,
		RequireLowercase:    true,
		RequireNumbers:      true,
		RequireSpecialChars: false,
		MinScore:            40,
		MaxAge:              365,
		PreventReuse:        5,
	}
}

func normalizeAndValidatePasswordPolicy(policy appdomain.PasswordPolicy) (appdomain.PasswordPolicy, []string) {
	current := defaultPasswordPolicy()
	if strings.TrimSpace(policy.Name) != "" {
		current.Name = strings.TrimSpace(policy.Name)
	}
	current.Description = strings.TrimSpace(policy.Description)
	if policy.MinLength > 0 {
		current.MinLength = policy.MinLength
	}
	if policy.MaxLength > 0 {
		current.MaxLength = policy.MaxLength
	}
	current.RequireUppercase = policy.RequireUppercase
	current.RequireLowercase = policy.RequireLowercase
	current.RequireNumbers = policy.RequireNumbers
	current.RequireSpecialChars = policy.RequireSpecialChars
	if policy.MinScore >= 0 {
		current.MinScore = policy.MinScore
	}
	if policy.MaxAge > 0 {
		current.MaxAge = policy.MaxAge
	}
	if policy.PreventReuse >= 0 {
		current.PreventReuse = policy.PreventReuse
	}
	validationErrors := make([]string, 0)
	if current.MinLength < 1 || current.MinLength > 50 {
		validationErrors = append(validationErrors, "最小长度必须在1-50之间")
	}
	if current.MaxLength < 1 || current.MaxLength > 256 {
		validationErrors = append(validationErrors, "最大长度必须在1-256之间")
	}
	if current.MinLength > current.MaxLength {
		validationErrors = append(validationErrors, "最小长度不能大于最大长度")
	}
	if current.MinScore < 0 || current.MinScore > 100 {
		validationErrors = append(validationErrors, "最低强度分数必须在0-100之间")
	}
	if current.MaxAge < 1 || current.MaxAge > 3650 {
		validationErrors = append(validationErrors, "密码最大年龄必须在1-3650天之间")
	}
	if current.PreventReuse < 0 || current.PreventReuse > 20 {
		validationErrors = append(validationErrors, "防重用密码数量必须在0-20之间")
	}
	current.IsDefault = false
	return current, validationErrors
}

func passwordPolicyToMap(policy appdomain.PasswordPolicy) map[string]any {
	return map[string]any{
		"name":                policy.Name,
		"description":         policy.Description,
		"minLength":           policy.MinLength,
		"maxLength":           policy.MaxLength,
		"requireUppercase":    policy.RequireUppercase,
		"requireLowercase":    policy.RequireLowercase,
		"requireNumbers":      policy.RequireNumbers,
		"requireSpecialChars": policy.RequireSpecialChars,
		"minScore":            policy.MinScore,
		"maxAge":              policy.MaxAge,
		"preventReuse":        policy.PreventReuse,
	}
}

func AnalyzePasswordStrength(password string) appdomain.PasswordStrengthAnalysis {
	if password == "" {
		return appdomain.PasswordStrengthAnalysis{
			Score:    0,
			Level:    "invalid",
			Feedback: []string{"密码不能为空"},
			Details:  appdomain.PasswordStrengthDetails{},
		}
	}
	details := appdomain.PasswordStrengthDetails{
		Length:            len(password),
		HasLowercase:      regexp.MustCompile(`[a-z]`).MatchString(password),
		HasUppercase:      regexp.MustCompile(`[A-Z]`).MatchString(password),
		HasNumbers:        regexp.MustCompile(`\d`).MatchString(password),
		HasSpecialChars:   regexp.MustCompile(`[!@#$%^&*()_+\-=\[\]{};':"\\|,.<>/?]`).MatchString(password),
		HasCommonPatterns: detectCommonPasswordPatterns(password),
		Entropy:           calculatePasswordEntropy(password),
	}
	score := 0
	feedback := make([]string, 0)
	if details.Length >= 12 {
		score += 30
	} else if details.Length >= 8 {
		score += 20
	} else if details.Length >= 6 {
		score += 10
		feedback = append(feedback, "密码长度至少应为8位")
	} else {
		score += 5
		feedback = append(feedback, "密码长度过短，建议至少8位")
	}
	if details.HasLowercase {
		score += 10
	} else {
		feedback = append(feedback, "建议包含小写字母")
	}
	if details.HasUppercase {
		score += 10
	} else {
		feedback = append(feedback, "建议包含大写字母")
	}
	if details.HasNumbers {
		score += 10
	} else {
		feedback = append(feedback, "建议包含数字")
	}
	if details.HasSpecialChars {
		score += 10
	} else {
		feedback = append(feedback, "建议包含特殊字符")
	}
	switch {
	case details.Entropy >= 4.0:
		score += 20
	case details.Entropy >= 3.5:
		score += 15
	case details.Entropy >= 3.0:
		score += 10
	case details.Entropy >= 2.5:
		score += 5
	default:
		feedback = append(feedback, "密码复杂度不足，请增加字符多样性")
	}
	if len(details.HasCommonPatterns) > 0 {
		score -= len(details.HasCommonPatterns) * 5
		for _, pattern := range details.HasCommonPatterns {
			feedback = append(feedback, "避免使用"+pattern)
		}
	}
	score = clampScore(score)
	level := "very_weak"
	switch {
	case score >= 80:
		level = "very_strong"
	case score >= 60:
		level = "strong"
	case score >= 40:
		level = "medium"
	case score >= 20:
		level = "weak"
	}
	if len(feedback) == 0 {
		feedback = []string{"密码强度良好"}
	}
	return appdomain.PasswordStrengthAnalysis{
		Score:           score,
		Level:           level,
		Feedback:        feedback,
		Details:         details,
		Recommendations: generatePasswordRecommendations(score, details),
	}
}

func CheckPasswordPolicy(password string, policy appdomain.PasswordPolicy) appdomain.PasswordPolicyCheck {
	active := policy
	if active.MinLength == 0 {
		active = defaultPasswordPolicy()
	}
	analysis := AnalyzePasswordStrength(password)
	violations := make([]string, 0)
	if len(password) < active.MinLength {
		violations = append(violations, "密码长度不能少于"+strconv.Itoa(active.MinLength)+"位")
	}
	if len(password) > active.MaxLength {
		violations = append(violations, "密码长度不能超过"+strconv.Itoa(active.MaxLength)+"位")
	}
	if active.RequireUppercase && !analysis.Details.HasUppercase {
		violations = append(violations, "密码必须包含大写字母")
	}
	if active.RequireLowercase && !analysis.Details.HasLowercase {
		violations = append(violations, "密码必须包含小写字母")
	}
	if active.RequireNumbers && !analysis.Details.HasNumbers {
		violations = append(violations, "密码必须包含数字")
	}
	if active.RequireSpecialChars && !analysis.Details.HasSpecialChars {
		violations = append(violations, "密码必须包含特殊字符")
	}
	if analysis.Score < active.MinScore {
		violations = append(violations, "密码强度不足，当前"+strconv.Itoa(analysis.Score)+"分，要求至少"+strconv.Itoa(active.MinScore)+"分")
	}
	if len(analysis.Details.HasCommonPatterns) > 0 {
		violations = append(violations, "密码不能包含常见模式或弱密码")
	}
	return appdomain.PasswordPolicyCheck{
		IsValid:    len(violations) == 0,
		Violations: violations,
		Analysis:   analysis,
		Policy:     active,
	}
}

func detectCommonPasswordPatterns(password string) []string {
	lower := strings.ToLower(password)
	patterns := make([]string, 0)
	for _, common := range []string{"password", "123456", "qwerty", "abc123", "password123", "admin", "root", "user", "test", "guest", "111111", "000000"} {
		if strings.Contains(lower, common) {
			patterns = append(patterns, "常见弱密码")
			break
		}
	}
	if regexp.MustCompile(`(.)\1{2,}`).MatchString(password) {
		patterns = append(patterns, "重复字符")
	}
	for _, pattern := range []string{"qwer", "asdf", "zxcv", "1234", "4321"} {
		if strings.Contains(lower, pattern) {
			patterns = append(patterns, "键盘模式")
			break
		}
	}
	if regexp.MustCompile(`\d{4}|\d{2}/\d{2}|\d{2}-\d{2}`).MatchString(password) {
		patterns = append(patterns, "日期模式")
	}
	return patterns
}

func calculatePasswordEntropy(password string) float64 {
	if password == "" {
		return 0
	}
	counts := make(map[rune]int, len(password))
	for _, char := range password {
		counts[char]++
	}
	length := float64(len(password))
	entropy := 0.0
	for _, freq := range counts {
		probability := float64(freq) / length
		entropy -= probability * math.Log2(probability)
	}
	return entropy
}

func generatePasswordRecommendations(score int, details appdomain.PasswordStrengthDetails) []appdomain.PasswordRecommendation {
	items := make([]appdomain.PasswordRecommendation, 0)
	if score < 60 {
		if details.Length < 8 {
			items = append(items, appdomain.PasswordRecommendation{Type: "length", Priority: "high", Message: "将密码长度增加到至少8位字符"})
		}
		if !details.HasUppercase || !details.HasLowercase {
			items = append(items, appdomain.PasswordRecommendation{Type: "case", Priority: "medium", Message: "同时使用大写和小写字母"})
		}
		if !details.HasNumbers {
			items = append(items, appdomain.PasswordRecommendation{Type: "numbers", Priority: "medium", Message: "添加数字字符"})
		}
		if !details.HasSpecialChars {
			items = append(items, appdomain.PasswordRecommendation{Type: "special", Priority: "medium", Message: "添加特殊字符（如!@#$%^&*）"})
		}
	}
	if details.Entropy < 3.0 {
		items = append(items, appdomain.PasswordRecommendation{Type: "complexity", Priority: "high", Message: "增加字符的多样性，避免重复模式"})
	}
	if len(details.HasCommonPatterns) > 0 {
		items = append(items, appdomain.PasswordRecommendation{Type: "patterns", Priority: "high", Message: "避免使用常见的密码模式和字典词汇"})
	}
	return items
}

func stringSetting(settings map[string]any, key string) string {
	if settings == nil {
		return ""
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func clampScore(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
