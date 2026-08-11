package service

import (
	"slices"
	"strings"
	"testing"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"
)

// TestRiskEnvMatchesVariableCatalog 双向钉死「表达式环境」与「变量目录」。
//
// 目录里多一条 → 控制台的提示里有它，但表达式里一用就是编译错误；
// 环境里多一条 → 变量能用，可没有任何人知道它存在。
// 两种漂移都不会在运行时报错，只会让规则悄悄写歪，所以必须由测试盯着。
func TestRiskEnvMatchesVariableCatalog(t *testing.T) {
	envNames := RiskEnvVariableNames()
	catalog := securitydomain.VariableCatalogEntries()

	catalogNames := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		catalogNames = append(catalogNames, entry.Name)
	}

	for _, name := range envNames {
		if !slices.Contains(catalogNames, name) {
			t.Errorf("环境变量 %q 未登记进 VariableCatalogEntries()，控制台不会提示它", name)
		}
	}
	for _, name := range catalogNames {
		if !slices.Contains(envNames, name) {
			t.Errorf("目录变量 %q 在 RiskEvalEnv 上不存在，用它写的表达式会编译失败", name)
		}
	}
}

// TestRiskConditionCatalogHasEvaluator 保证目录里的每一种条件类型都有判定分支。
// 目录里多一条而 evaluateCondition 里没有，等于给管理员一个假的防线：
// 规则存得进去、列表上显示"已启用"，但对每个请求都判假。
func TestRiskConditionCatalogHasEvaluator(t *testing.T) {
	svc := &RiskService{}
	env := newRiskEvalEnv(timeutil.Now())

	for _, entry := range securitydomain.ConditionCatalogEntries() {
		rule := securitydomain.RiskRule{
			Name:          entry.Label,
			ConditionType: entry.Value,
			ConditionData: sampleConditionData(entry),
		}
		if _, _, err := svc.evaluateCondition(rule, env); err != nil && strings.Contains(err.Error(), "未知规则条件类型") {
			t.Errorf("条件类型 %q 在目录里，但 evaluateCondition 没有对应分支", entry.Value)
		}
	}
}

// sampleConditionData 用目录声明的默认值 / 占位符造一份可用参数。
func sampleConditionData(entry securitydomain.ConditionCatalog) map[string]any {
	data := map[string]any{}
	for _, field := range entry.Fields {
		switch {
		case field.Default != nil:
			data[field.Key] = field.Default
		case field.Type == "list":
			data[field.Key] = []string{"desktop"}
		case field.Type == "time":
			data[field.Key] = "00:00"
		default:
			data[field.Key] = "CN"
		}
	}
	if entry.Value == securitydomain.CondCustomExpr {
		data["expression"] = "ip_request_count > 0"
	}
	if entry.Value == securitydomain.CondIPCIDR {
		data["cidrs"] = []string{"10.0.0.0/8"}
	}
	if entry.Value == securitydomain.CondASNIn {
		data["keywords"] = []string{"amazon"}
	}
	if entry.Value == securitydomain.CondGeoCountryIn || entry.Value == securitydomain.CondGeoCountryNotIn {
		data["countries"] = []string{"CN"}
	}
	return data
}

// TestValidateRuleInputRejectsBadExpression 是这轮改造最核心的一条保证：
// 写错变量名的表达式必须在**保存时**就被拒绝。
// 旧实现会把它顺利存进库，之后它对每个请求都判假、永不命中，
// 而规则列表上一直显示"已启用" —— 没有比这更危险的安全配置。
func TestValidateRuleInputRejectsBadExpression(t *testing.T) {
	err := ValidateRuleInput(securitydomain.SceneLogin, securitydomain.CondCustomExpr, map[string]any{
		"expression": "ip_reqeust_count > 100", // 故意拼错 request
	})
	if err == nil {
		t.Fatal("拼错变量名的表达式应当被拒绝")
	}
	if !strings.Contains(err.Error(), "表达式无法编译") {
		t.Fatalf("错误信息应指明是编译失败，实际为：%v", err)
	}

	if err := ValidateRuleInput(securitydomain.SceneLogin, securitydomain.CondCustomExpr, map[string]any{
		"expression": "ip_request_count > 100 and device_age_hours < 24",
	}); err != nil {
		t.Fatalf("合法表达式不应被拒绝：%v", err)
	}
}

func TestValidateRuleInputChecksRequiredFields(t *testing.T) {
	cases := []struct {
		name      string
		scene     string
		condition string
		data      map[string]any
		wantErr   bool
	}{
		{"未知场景", "nowhere", securitydomain.CondUABot, nil, true},
		{"未知条件类型", securitydomain.SceneLogin, "telepathy", nil, true},
		{"缺少必填阈值", securitydomain.SceneLogin, securitydomain.CondIPFrequency, map[string]any{}, true},
		{"阈值低于下界", securitydomain.SceneLogin, securitydomain.CondIPFrequency, map[string]any{"threshold": 0}, true},
		{"合法阈值", securitydomain.SceneLogin, securitydomain.CondIPFrequency, map[string]any{"threshold": 60}, false},
		{"非法网段", securitydomain.SceneLogin, securitydomain.CondIPCIDR, map[string]any{"cidrs": []any{"10.0.0.0/99"}}, true},
		{"合法网段", securitydomain.SceneLogin, securitydomain.CondIPCIDR, map[string]any{"cidrs": []any{"10.0.0.0/8"}}, false},
		{"非法时段", securitydomain.SceneLogin, securitydomain.CondTimeWindow, map[string]any{"start": "25:00", "end": "06:00"}, true},
		{"跨零点时段", securitydomain.SceneLogin, securitydomain.CondTimeWindow, map[string]any{"start": "23:00", "end": "06:00"}, false},
		{"无参数条件", securitydomain.SceneLogin, securitydomain.CondUABot, map[string]any{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRuleInput(tc.scene, tc.condition, tc.data)
			if tc.wantErr && err == nil {
				t.Fatal("期望被拒绝，实际通过")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望通过，实际被拒绝：%v", err)
			}
		})
	}
}

// TestEvaluateConditionReasons 判定必须给出人类可读的判据。
// 只回 true/false 的引擎在复核台上提供不了任何信息 ——
// 「命中 IP 高频」和「命中 IP 高频：312 次 > 阈值 100」是两种可运维性。
func TestEvaluateConditionReasons(t *testing.T) {
	svc := &RiskService{}
	env := newRiskEvalEnv(timeutil.Now())
	env.IP = "203.0.113.7"
	env.DeviceID = "dev-1"
	env.IPRequestCount = 312
	env.GeoCountry = "US"
	env.GeoKnown = true
	env.applyUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")

	hit, reason, err := svc.evaluateCondition(securitydomain.RiskRule{
		ConditionType: securitydomain.CondIPFrequency,
		ConditionData: map[string]any{"threshold": float64(100)},
	}, env)
	if err != nil || !hit {
		t.Fatalf("应当命中，got hit=%v err=%v", hit, err)
	}
	if !strings.Contains(reason, "312") || !strings.Contains(reason, "100") {
		t.Fatalf("判据里应同时出现实际值与阈值，实际为：%s", reason)
	}

	hit, reason, err = svc.evaluateCondition(securitydomain.RiskRule{
		ConditionType: securitydomain.CondGeoAnomaly,
		ConditionData: map[string]any{"expected_country": "CN"},
	}, env)
	if err != nil || !hit {
		t.Fatalf("归属地异常应当命中，got hit=%v err=%v", hit, err)
	}
	if !strings.Contains(reason, "US") {
		t.Fatalf("判据里应给出实际归属地，实际为：%s", reason)
	}
}

// TestGeoConditionsSkipOnUnknownLocation 情报缺失时必须判「不命中」。
// 反过来会把每一个查不到归属地的请求都算成异常 —— 那不是风控，是拒绝服务。
func TestGeoConditionsSkipOnUnknownLocation(t *testing.T) {
	svc := &RiskService{}
	env := newRiskEvalEnv(timeutil.Now())
	env.IP = "203.0.113.7"

	for _, condition := range []string{
		securitydomain.CondGeoAnomaly,
		securitydomain.CondGeoCountryIn,
		securitydomain.CondGeoCountryNotIn,
	} {
		hit, reason, err := svc.evaluateCondition(securitydomain.RiskRule{
			ConditionType: condition,
			ConditionData: map[string]any{"expected_country": "CN", "countries": []any{"CN"}},
		}, env)
		if err != nil {
			t.Fatalf("%s 不应报错：%v", condition, err)
		}
		if hit {
			t.Fatalf("%s 在归属地未知时不应命中（判据：%s）", condition, reason)
		}
	}
}

// TestTrustedIPBypassesProxyRules 人工加白优先于情报源判断。
// 否则管理员刚把公司出口 IP 加白，下一个请求还是被代理规则拦下。
func TestTrustedIPBypassesProxyRules(t *testing.T) {
	svc := &RiskService{}
	env := newRiskEvalEnv(timeutil.Now())
	env.IP = "203.0.113.7"
	env.IPKnown = true
	env.IPIsProxy = true
	env.IPRiskScore = 95
	env.IPTrusted = true

	for _, condition := range []string{securitydomain.CondIPProxy, securitydomain.CondIPReputation} {
		hit, reason, err := svc.evaluateCondition(securitydomain.RiskRule{
			ConditionType: condition,
			ConditionData: map[string]any{"min_score": float64(75)},
		}, env)
		if err != nil {
			t.Fatalf("%s 不应报错：%v", condition, err)
		}
		if hit {
			t.Fatalf("%s 对加白 IP 不应命中（判据：%s）", condition, reason)
		}
	}
}

// TestDeviceNewSkipsWhenNoDeviceID 没带设备标识是「缺数据」，不是风险信号。
// 旧实现里 device_age_hours 恒为 0，于是「新设备」规则对每个请求都命中。
func TestDeviceNewSkipsWhenNoDeviceID(t *testing.T) {
	svc := &RiskService{}
	env := newRiskEvalEnv(timeutil.Now())

	hit, reason, err := svc.evaluateCondition(securitydomain.RiskRule{
		ConditionType: securitydomain.CondDeviceNew,
		ConditionData: map[string]any{"max_hours": float64(1)},
	}, env)
	if err != nil {
		t.Fatalf("不应报错：%v", err)
	}
	if hit {
		t.Fatalf("未携带设备标识时不应判成新设备（判据：%s）", reason)
	}

	env.DeviceID = "dev-1"
	if hit, _, _ = svc.evaluateCondition(securitydomain.RiskRule{
		ConditionType: securitydomain.CondDeviceNew,
		ConditionData: map[string]any{"max_hours": float64(1)},
	}, env); !hit {
		t.Fatal("带了设备标识且不在档时应判成新设备")
	}
}

func TestScoreToLevelMatchesBands(t *testing.T) {
	cases := map[int]string{
		0: securitydomain.LevelNormal, 20: securitydomain.LevelNormal,
		21: securitydomain.LevelLow, 40: securitydomain.LevelLow,
		41: securitydomain.LevelMedium, 60: securitydomain.LevelMedium,
		61: securitydomain.LevelHigh, 80: securitydomain.LevelHigh,
		81: securitydomain.LevelCritical, 999: securitydomain.LevelCritical,
	}
	for score, want := range cases {
		if got := securitydomain.ScoreToLevel(score); got != want {
			t.Errorf("分数 %d 应映射为 %s，实际 %s", score, want, got)
		}
	}
}

func TestScoreRangesOverlap(t *testing.T) {
	cases := []struct {
		name       string
		aMin, bMin int
		aMax, bMax *int
		want       bool
	}{
		{"相邻不重叠", 0, 41, new(40), new(60), false},
		{"边界重叠", 0, 40, new(40), new(60), true},
		{"开口区间吞掉后者", 61, 80, nil, new(90), true},
		{"完全分离", 0, 61, new(20), nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreRangesOverlap(tc.aMin, tc.aMax, tc.bMin, tc.bMax); got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}

func TestValidateActionInputRejectsZeroBanDuration(t *testing.T) {
	// 封禁 0 秒等于没封。让它存进去只会制造一条看起来生效、实际不生效的策略。
	if err := validateActionInput(securitydomain.SceneLogin, securitydomain.ActionBan, 80, nil, 0); err == nil {
		t.Fatal("封禁动作未指定时长应被拒绝")
	}
	if err := validateActionInput(securitydomain.SceneLogin, securitydomain.ActionBan, 80, nil, 3600); err != nil {
		t.Fatalf("合法封禁策略不应被拒绝：%v", err)
	}
	if err := validateActionInput(securitydomain.SceneLogin, securitydomain.ActionBlock, 80, new(40), 0); err == nil {
		t.Fatal("上界小于下界应被拒绝")
	}
}

func TestInTimeWindowAcrossMidnight(t *testing.T) {
	loc := timeutil.DefaultLocation()
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 3, 21, hour, minute, 0, 0, loc)
	}
	cases := []struct {
		name       string
		at         time.Time
		start, end string
		want       bool
	}{
		{"跨零点-凌晨内", at(2, 30), "23:00", "06:00", true},
		{"跨零点-深夜内", at(23, 30), "23:00", "06:00", true},
		{"跨零点-白天外", at(12, 0), "23:00", "06:00", false},
		{"常规区间内", at(10, 0), "09:00", "18:00", true},
		{"常规区间外", at(20, 0), "09:00", "18:00", false},
		{"起止相同视为全天", at(3, 0), "00:00", "00:00", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := inTimeWindow(tc.at, tc.start, tc.end)
			if err != nil {
				t.Fatalf("不应报错：%v", err)
			}
			if got != tc.want {
				t.Fatalf("期望 %v，实际 %v", tc.want, got)
			}
		})
	}
}

func TestIPInCIDR(t *testing.T) {
	cases := []struct {
		ip, cidr string
		want     bool
	}{
		{"10.1.2.3", "10.0.0.0/8", true},
		{"11.1.2.3", "10.0.0.0/8", false},
		{"2001:db8::1", "2001:db8::/32", true},
		{"2001:dba::1", "2001:db8::/32", false},
		{"::ffff:10.1.2.3", "10.0.0.0/8", true}, // v4-mapped 必须能匹配 v4 网段
		{"not-an-ip", "10.0.0.0/8", false},
		{"10.1.2.3", "garbage", false},
	}
	for _, tc := range cases {
		if got := ipInCIDR(tc.ip, tc.cidr); got != tc.want {
			t.Errorf("ipInCIDR(%q, %q) = %v，期望 %v", tc.ip, tc.cidr, got, tc.want)
		}
	}
}

func TestUserAgentClassification(t *testing.T) {
	cases := []struct {
		name      string
		ua        string
		wantClass string
		wantBot   bool
	}{
		{"桌面 Chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", deviceClassDesktop, false},
		{"iPhone Safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", deviceClassMobile, false},
		{"Googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", deviceClassBot, true},
		{"空 UA", "", deviceClassUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newRiskEvalEnv(timeutil.Now())
			env.applyUserAgent(tc.ua)
			if env.UADeviceClass != tc.wantClass {
				t.Errorf("客户端类型期望 %s，实际 %s", tc.wantClass, env.UADeviceClass)
			}
			if env.UAIsBot != tc.wantBot {
				t.Errorf("Bot 判定期望 %v，实际 %v", tc.wantBot, env.UAIsBot)
			}
			if tc.ua != "" && env.UALength == 0 {
				t.Error("非空 UA 的长度不应为 0")
			}
		})
	}
}

// TestRiskEnvSnapshotRoundTrip 快照必须能还原成环境 —— 评估记录的「重放」依赖它。
func TestRiskEnvSnapshotRoundTrip(t *testing.T) {
	env := newRiskEvalEnv(timeutil.Now())
	env.IP = "203.0.113.7"
	env.Account = "zhangsan"
	env.IPRequestCount = 42
	env.DeviceAgeHours = 3.5
	env.GeoCountry = "CN"
	env.GeoKnown = true
	env.IPIsProxy = true
	env.Extra = map[string]any{"channel": "h5"}

	restored := riskEnvFromSnapshot(env.Snapshot(), timeutil.Now())
	if restored.IP != env.IP || restored.Account != env.Account {
		t.Fatalf("请求维度未还原：%+v", restored)
	}
	if restored.IPRequestCount != 42 || restored.DeviceAgeHours != 3.5 {
		t.Fatalf("数值维度未还原：count=%d age=%v", restored.IPRequestCount, restored.DeviceAgeHours)
	}
	if !restored.IPIsProxy || restored.GeoCountry != "CN" {
		t.Fatalf("网络与地理维度未还原：%+v", restored)
	}
	if restored.Extra["channel"] != "h5" {
		t.Fatalf("扩展字段未还原：%+v", restored.Extra)
	}
}

// TestRiskExprCompileCache 表达式必须只编译一次。
// 旧实现在每次评估里重新编译 —— 登录热路径上每个请求都做一遍词法/语法/类型检查。
func TestRiskExprCompileCache(t *testing.T) {
	const expression = "ip_risk_score >= 60 and not ip_trusted"
	riskExprCache.Delete(expression)

	first, err := compileRiskExpr(expression)
	if err != nil {
		t.Fatalf("编译失败：%v", err)
	}
	second, err := compileRiskExpr(expression)
	if err != nil {
		t.Fatalf("二次编译失败：%v", err)
	}
	if first != second {
		t.Fatal("同一段表达式应命中缓存，返回同一个程序对象")
	}
}

func TestRiskExprDomainFunctions(t *testing.T) {
	env := newRiskEvalEnv(timeutil.Now())
	env.IP = "10.1.2.3"
	env.IPISP = "Amazon Technologies Inc."

	hit, err := runRiskExpr(`any_cidr(ip, ["10.0.0.0/8", "192.168.0.0/16"])`, env)
	if err != nil || !hit {
		t.Fatalf("any_cidr 应命中：hit=%v err=%v", hit, err)
	}
	hit, err = runRiskExpr(`contains_any(ip_isp, ["digitalocean", "amazon"])`, env)
	if err != nil || !hit {
		t.Fatalf("contains_any 应忽略大小写命中：hit=%v err=%v", hit, err)
	}
	if _, err = runRiskExpr(`in_time_window("00:00", "00:00")`, env); err != nil {
		t.Fatalf("in_time_window 不应报错：%v", err)
	}
}

// TestRiskExprSamplesCompile 目录里给管理员抄的示例必须真的能编译。
// 抄一段官方示例结果报语法错误，是最难解释的一种 bug。
func TestRiskExprSamplesCompile(t *testing.T) {
	for _, sample := range securitydomain.ExprSampleEntries() {
		if err := ValidateRiskExpression(sample.Expression); err != nil {
			t.Errorf("示例 %q 无法编译：%v", sample.Title, err)
		}
	}
}

// TestSummarizeEvaluations 汇总只统计命中项，且把判据带进留痕。
func TestSummarizeEvaluations(t *testing.T) {
	total, matched := summarizeEvaluations([]securitydomain.RuleEvaluation{
		{RuleID: 1, RuleName: "A", Score: 30, Hit: true, Reason: "因为 A"},
		{RuleID: 2, RuleName: "B", Score: 50, Hit: false, Reason: "因为不 B"},
		{RuleID: 3, RuleName: "C", Score: 15, Hit: true, Reason: "因为 C"},
	})
	if total != 45 {
		t.Fatalf("总分应为 45，实际 %d", total)
	}
	if len(matched) != 2 {
		t.Fatalf("命中项应为 2 条，实际 %d", len(matched))
	}
	if matched[0].Reason != "因为 A" {
		t.Fatalf("判据未带入留痕：%+v", matched[0])
	}
}

// TestExplainRuleCoversCatalog 每种条件类型都要能翻译成一句中文。
// 控制台上并排列着十几条规则时，conditionData 的 JSON 是看不动的。
func TestExplainRuleCoversCatalog(t *testing.T) {
	for _, entry := range securitydomain.ConditionCatalogEntries() {
		rule := securitydomain.RiskRule{
			ConditionType: entry.Value,
			ConditionData: sampleConditionData(entry),
			Score:         20,
		}
		if explainRule(rule) == "" {
			t.Errorf("条件类型 %q 缺少中文说明", entry.Value)
		}
	}
}
