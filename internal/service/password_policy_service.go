package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	appdomain "aegis/internal/domain/app"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
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
	if value, ok := lookupInt(typed, "minScore"); ok && value >= 0 {
		policy.MinScore = value
	}
	// maxAge / preventReuse 用 lookupInt 而非 intSetting：这两个字段的 0 是有效取值
	// （永不过期 / 不限制重用），"键不存在" 才该回落默认值
	if value, ok := lookupInt(typed, "maxAge"); ok && value >= 0 {
		policy.MaxAge = value
	}
	if value, ok := lookupInt(typed, "preventReuse"); ok && value >= 0 {
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
		// 掩码长度按字符数：按字节算会让中文口令的星号数比实际长度多两倍，
		// 管理员会以为自己输错了
		Password:         strings.Repeat("*", utf8.RuneCountInString(password)),
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

// PasswordLifecycle 一次密码写入需要落地的策略派生值。
type PasswordLifecycle struct {
	// ExpiresAt 密码过期时间；nil = 永不过期（策略 MaxAge <= 0）
	ExpiresAt *time.Time
	// HistoryKeep 需要保留的历史密码条数（策略 PreventReuse）
	HistoryKeep int
}

// ResolvePasswordLifecycle 按应用密码策略推导本次密码写入的过期时间与历史保留条数。
// 应用读取失败时返回零值（不过期、不留历史）而非报错：改密本身不该因为
// 读不到策略而失败，宁可少一层约束。
func (s *AppService) ResolvePasswordLifecycle(ctx context.Context, appID int64, changedAt time.Time) PasswordLifecycle {
	app, err := s.GetApp(ctx, appID)
	if err != nil || app == nil {
		return PasswordLifecycle{}
	}
	policy := s.ResolvePasswordPolicy(app)
	lifecycle := PasswordLifecycle{HistoryKeep: policy.PreventReuse}
	if policy.MaxAge > 0 {
		expiresAt := changedAt.UTC().AddDate(0, 0, policy.MaxAge)
		lifecycle.ExpiresAt = &expiresAt
	}
	return lifecycle
}

// EnsurePasswordNotReused 校验新密码是否命中该用户最近 PreventReuse 个历史密码。
//
// bcrypt 每条哈希自带 salt，无法用等值查询判重，只能逐条比较；
// 因此策略把 PreventReuse 限制在 20 以内，避免一次改密跑几十次 bcrypt。
func (s *AppService) EnsurePasswordNotReused(ctx context.Context, appID, userID int64, newPassword string) error {
	if s.pg == nil || userID <= 0 {
		return nil
	}
	app, err := s.GetApp(ctx, appID)
	if err != nil || app == nil {
		return nil
	}
	keep := s.ResolvePasswordPolicy(app).PreventReuse
	if keep <= 0 {
		return nil
	}
	hashes, err := s.pg.ListRecentPasswordHashes(ctx, userID, keep)
	if err != nil {
		// 历史读不出来时放行：拿不到证据不该反过来指控用户重用了密码
		s.log.Warn("密码历史读取失败，跳过防重用校验",
			zap.Int64("appid", appID), zap.Int64("user_id", userID), zap.Error(err))
		return nil
	}
	for _, hash := range hashes {
		if verifyPassword(hash, newPassword) {
			return apperrors.New(40028, http.StatusBadRequest,
				fmt.Sprintf("新密码不能与最近 %d 次使用过的密码相同", keep))
		}
	}
	return nil
}

func (s *AppService) ValidatePasswordWithAppPolicy(ctx context.Context, appID int64, password string) error {
	return s.ValidatePasswordWithAppPolicyContext(ctx, appID, password, PasswordContext{})
}

// ValidatePasswordWithAppPolicyContext 带用户上下文的策略校验。
// 能拿到账号 / 昵称的链路都应当走这条，否则「密码 = 账号」判不出来。
func (s *AppService) ValidatePasswordWithAppPolicyContext(ctx context.Context, appID int64, password string, pctx PasswordContext) error {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app != nil && pctx.AppName == "" {
		pctx.AppName = app.Name
	}
	check := CheckPasswordPolicyWithContext(password, s.ResolvePasswordPolicy(app), pctx)
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
	// MaxAge / PreventReuse 的 0 是有意义的取值（永不过期 / 不限制重用），
	// 不能像长度那样「0 就回落默认值」—— 那会让管理员把过期关掉后
	// 实际仍按默认 365 天过期，且界面显示的还是 0。
	if policy.MaxAge >= 0 {
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
	if current.MaxAge < 0 || current.MaxAge > 3650 {
		validationErrors = append(validationErrors, "密码有效期必须在0-3650天之间（0 表示永不过期）")
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

// CheckPasswordPolicy 按应用策略校验口令（无用户上下文）。
func CheckPasswordPolicy(password string, policy appdomain.PasswordPolicy) appdomain.PasswordPolicyCheck {
	return CheckPasswordPolicyWithContext(password, policy, PasswordContext{})
}

// CheckPasswordPolicyWithContext 按应用策略校验口令，并把账号 / 昵称等上下文
// 一起交给强度引擎 —— 「密码就是自己的账号」只有在这条路径上才拦得住。
//
// # 与旧实现的一处关键行为差异
//
// 旧版只要 `HasCommonPatterns` 非空就判一条「密码不能包含常见模式或弱密码」。
// 换成 zxcvbn 后这条**必须去掉**：zxcvbn 会把任意口令拆成模式序列，
// 强口令里同样会出现字典片段（`Xy9$Kwe2` 里的 `w` `e` 都可能被识别成短词），
// 照搬旧规则会导致几乎没有口令能通过。
//
// 正确做法是：模式的代价**已经计入猜测次数**，也就是已经反映在分数里，
// 再单独扣一次是重复计价。这里只额外拦一种分数说明不了的情况 ——
// 整条口令就是一个可猜测模式（见 fatalPasswordPattern）。
func CheckPasswordPolicyWithContext(password string, policy appdomain.PasswordPolicy, pctx PasswordContext) appdomain.PasswordPolicyCheck {
	active := policy
	if active.MinLength == 0 {
		active = defaultPasswordPolicy()
	}
	analysis := AnalyzePasswordStrengthWithContext(password, pctx)
	violations := make([]string, 0)

	// 长度按字符数（rune）判定而不是字节数：管理员配「至少 8 位」时想的是
	// 8 个字符，按字节算会让 3 个汉字（9 字节）冒充 9 位密码通过
	length := analysis.Details.Length
	if length < active.MinLength {
		violations = append(violations, "密码长度不能少于"+strconv.Itoa(active.MinLength)+"位")
	}
	if length > active.MaxLength {
		violations = append(violations, "密码长度不能超过"+strconv.Itoa(active.MaxLength)+"位")
	}
	// bcrypt 只取前 72 字节，超出部分静默丢弃。策略的 MaxLength 上限是 256，
	// 因此必须在这里单独把关 —— 否则前 72 字节相同的两个口令可以互相登录，
	// 而哈希层只会在写入时报一个与策略无关的错。
	if analysis.Details.ByteLength > bcryptPasswordByteLimit {
		violations = append(violations, fmt.Sprintf(
			"密码不能超过 %d 字节（当前 %d 字节；一个汉字算 3 字节）",
			bcryptPasswordByteLimit, analysis.Details.ByteLength))
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
	if analysis.Level == "invalid" {
		// 归一化阶段就被否掉（空口令 / 控制字符 / 非法编码），
		// 此时 Feedback 里是具体原因，比"强度不足 0 分"有用得多
		violations = append(violations, analysis.Feedback...)
	} else if analysis.Score < active.MinScore {
		violations = append(violations, "密码强度不足，当前"+strconv.Itoa(analysis.Score)+
			"分，要求至少"+strconv.Itoa(active.MinScore)+"分")
	}
	if fatal := fatalPasswordPattern(analysis); fatal != "" {
		violations = append(violations, fatal)
	}

	return appdomain.PasswordPolicyCheck{
		IsValid:    len(violations) == 0,
		Violations: violations,
		Analysis:   analysis,
		Policy:     active,
	}
}

// fatalPasswordPattern 判定口令是否**整体**就是一个可猜测模式，返回拦截理由。
//
// 为什么分数之外还要这一条：`minScore` 可以被应用调到 0（"不校验强度"），
// 那是给内部工具类应用留的口子。但即便如此，「密码 = 账号」「密码 = 123456」
// 这种也不该放过 —— 它不是"强度低"，是"等于没有密码"。
//
// 判据是单个模式覆盖整条口令：部分覆盖（如 `qwerty` + 随机 6 位）交给分数处理。
func fatalPasswordPattern(analysis appdomain.PasswordStrengthAnalysis) string {
	if analysis.Details.Length == 0 {
		return ""
	}
	for _, item := range analysis.Details.Patterns {
		if item.Start != 1 || item.End != analysis.Details.Length {
			continue
		}
		switch {
		case item.Kind == "dictionary" && item.Source == sourceUserInput:
			return "密码不能与账号、昵称或手机号相同"
		case item.Kind == "dictionary":
			return "密码整体是一个已知的弱口令（" + item.Label + "），请更换"
		case item.Kind == "spatial":
			return "密码整体是一串键盘相邻字符，请更换"
		case item.Kind == "sequence":
			return "密码整体是一段连续序列，请更换"
		case item.Kind == "repeat":
			return "密码整体是重复片段，请更换"
		case item.Kind == "date":
			return "密码整体是一个日期，请更换"
		}
	}
	return ""
}

// generatePasswordRecommendations 生成改进建议。
//
// 建议刻意**以加长为主、以增加字符种类为辅**：在 zxcvbn 的模型里，
// 把 8 位随机串加到 12 位带来约 10^4 倍猜测量，而在 8 位上补一个大写字母
// 只带来个位数倍率 —— "必须含大小写数字符号"是上一代密码学建议，
// NIST SP 800-63B 已经明确不再推荐强制字符组合。
func generatePasswordRecommendations(score int, details appdomain.PasswordStrengthDetails) []appdomain.PasswordRecommendation {
	items := make([]appdomain.PasswordRecommendation, 0, 4)
	if details.Length > 0 && details.Length < 12 {
		items = append(items, appdomain.PasswordRecommendation{
			Type: "length", Priority: "high",
			Message: "加长到 12 位以上，这是提升强度最有效的一步",
		})
	}
	for _, item := range details.Patterns {
		switch item.Kind {
		case "dictionary":
			items = append(items, appdomain.PasswordRecommendation{
				Type: "patterns", Priority: "high",
				Message: "避免使用字典词、姓名拼音或已泄露的口令；把字母换成数字（如 a→@）并不能提高强度",
			})
		case "spatial":
			items = append(items, appdomain.PasswordRecommendation{
				Type: "patterns", Priority: "high",
				Message: "避免键盘上相邻的字符串（如 qwerty、1qaz2wsx）",
			})
		case "sequence", "repeat":
			items = append(items, appdomain.PasswordRecommendation{
				Type: "complexity", Priority: "medium",
				Message: "避免连续或重复的片段（如 abcdef、111111）",
			})
		case "date", "regex":
			items = append(items, appdomain.PasswordRecommendation{
				Type: "patterns", Priority: "medium",
				Message: "避免生日、纪念日与年份 —— 这类组合的取值空间很小",
			})
		default:
			continue
		}
		break // 一次只给一条最相关的模式建议，列一堆没人看
	}
	if score < 60 && !details.HasLowercase && !details.HasUppercase {
		items = append(items, appdomain.PasswordRecommendation{
			Type: "complexity", Priority: "medium",
			Message: "纯数字口令的取值空间极小，建议混入字母",
		})
	}
	if score >= 60 && len(items) == 0 {
		items = append(items, appdomain.PasswordRecommendation{
			Type: "manager", Priority: "low",
			Message: "强度已足够；建议用密码管理器保存，不要在多个站点复用",
		})
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
