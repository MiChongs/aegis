package service

import (
	"strings"
	"testing"

	appdomain "aegis/internal/domain/app"
)

// TestAnalyzePasswordStrengthDoesNotPanic 钉住一次线上崩溃：
// 旧实现用 `(.)\1{2,}` 检测重复字符，而 Go 的 regexp 是 RE2、不支持反向引用，
// MustCompile 在**每次调用**时 panic —— 表现是所有带密码的注册请求 500。
// 这里跑一遍各种形状的口令，任何 panic 都会让测试失败。
func TestAnalyzePasswordStrengthDoesNotPanic(t *testing.T) {
	passwords := []string{
		"", "a", "aa", "aaa", "password123", "Aa1!Aa1!Aa1!", "qwerty",
		"2024-01-01", "密码密码密码", "日日日", "🙂🙂🙂", "  ",
		strings.Repeat("x", 256), strings.Repeat("汉", 100),
	}
	for _, password := range passwords {
		t.Run(password, func(t *testing.T) {
			analysis := AnalyzePasswordStrength(password)
			if analysis.Score < 0 || analysis.Score > 100 {
				t.Fatalf("分数越界: %d", analysis.Score)
			}
			CheckPasswordPolicy(password, defaultPasswordPolicy())
		})
	}
}

// TestWeakPasswordsScoreLow 是这次换引擎的核心收益。
// 左列全部是旧实现放行的口令 —— 它们凑齐了字符类、香农熵也不低，
// 但在字典攻击下秒破。新引擎必须给出足够低的分数把它们挡在默认策略之外。
func TestWeakPasswordsScoreLow(t *testing.T) {
	const defaultMinScore = 40 // defaultPasswordPolicy().MinScore
	weak := []string{
		"password123", // 旧实现 45 分，压着默认门槛通过
		"Password1",   // 四类字符齐三类，旧实现给高分
		"P@ssw0rd",    // l33t 替换，旧实现完全识别不出
		"qwerty123",   // 键盘串
		"abc123456",
		"woaini1314", // 中文语境高频，zxcvbn 原生词表覆盖不到
		"5201314",
		"zhangwei",
		"woaini520",
		"a123456",
		"iloveyou",
		"11223344",
	}
	for _, password := range weak {
		t.Run(password, func(t *testing.T) {
			analysis := AnalyzePasswordStrength(password)
			if analysis.Score >= defaultMinScore {
				t.Errorf("弱口令 %q 拿到 %d 分（≥ 默认门槛 %d），模式=%v",
					password, analysis.Score, defaultMinScore, analysis.Details.HasCommonPatterns)
			}
		})
	}
}

// TestStrongPasswordsPass 反向守住：换引擎后不能把正常的强口令也一并拒了。
// 旧实现「命中任一模式即判违规」的规则若照搬到 zxcvbn 上就会出现这种情况 ——
// zxcvbn 会把任意口令都拆成模式序列，强口令里同样有字典片段。
func TestStrongPasswordsPass(t *testing.T) {
	strong := []string{
		"7xKq2mVzP4wR",
		"correct-horse-battery-staple-92",
		"Tr0ub4dor&3xKq2mVz",
		"j8#Fq2$Lm9@Wz5",
	}
	policy := defaultPasswordPolicy()
	for _, password := range strong {
		t.Run(password, func(t *testing.T) {
			check := CheckPasswordPolicy(password, policy)
			if !check.IsValid {
				t.Errorf("强口令 %q 被拒: %v（%d 分）", password, check.Violations, check.Analysis.Score)
			}
			if check.Analysis.Score < 60 {
				t.Errorf("强口令 %q 只拿到 %d 分", password, check.Analysis.Score)
			}
		})
	}
}

// TestPasswordContextDetectsAccountReuse 「拿账号当密码」必须被拦。
// 这条只有把账号喂给强度引擎才做得到，字符类规则永远拦不住 ——
// 账号 "ZhangSan2024" 恰好满足大小写 + 数字 + 12 位。
func TestPasswordContextDetectsAccountReuse(t *testing.T) {
	pctx := PasswordContext{Account: "ZhangSan2024", Nickname: "小张", Email: "zhangsan@corp.com"}
	policy := defaultPasswordPolicy()

	// 无上下文时它看起来像个正常口令
	if check := CheckPasswordPolicy("ZhangSan2024", policy); !check.IsValid {
		t.Logf("无上下文判定: %v（%d 分）", check.Violations, check.Analysis.Score)
	}

	check := CheckPasswordPolicyWithContext("ZhangSan2024", policy, pctx)
	if check.IsValid {
		t.Fatalf("密码与账号相同却通过了校验（%d 分）", check.Analysis.Score)
	}
	if !containsSubstring(check.Violations, "账号") {
		t.Errorf("违规说明没点出账号复用: %v", check.Violations)
	}
}

// TestFatalPatternRejectedEvenWithoutScoreGate minScore=0（"不校验强度"）
// 是给内部工具类应用留的口子，但「密码 = 123456」不是"强度低"，是"等于没有密码"，
// 必须照拦不误。
func TestFatalPatternRejectedEvenWithoutScoreGate(t *testing.T) {
	policy := appdomain.PasswordPolicy{MinLength: 1, MaxLength: 128, MinScore: 0}
	for _, password := range []string{"123456", "qwertyui", "aaaaaaaa", "password"} {
		check := CheckPasswordPolicy(password, policy)
		if check.IsValid {
			t.Errorf("整体弱模式 %q 在 minScore=0 下仍应被拒", password)
		}
	}
	// 而正常口令在 minScore=0 下必须放行，否则这个口子就白留了
	if check := CheckPasswordPolicy("7xKq2mVzP4wR", policy); !check.IsValid {
		t.Errorf("强口令在 minScore=0 下被拒: %v", check.Violations)
	}
}

// TestPasswordLengthCountedInRunes 长度按字符算而不是字节算。
// 按字节算会让 3 个汉字（9 字节）冒充 9 位密码通过「至少 8 位」。
func TestPasswordLengthCountedInRunes(t *testing.T) {
	policy := appdomain.PasswordPolicy{MinLength: 8, MaxLength: 128, MinScore: 0}
	check := CheckPasswordPolicy("汉字密码", policy) // 4 字符 / 12 字节
	if check.Analysis.Details.Length != 4 {
		t.Errorf("字符数 = %d, 期望 4", check.Analysis.Details.Length)
	}
	if check.Analysis.Details.ByteLength != 12 {
		t.Errorf("字节数 = %d, 期望 12", check.Analysis.Details.ByteLength)
	}
	if check.IsValid {
		t.Error("4 个汉字不该满足「至少 8 位」")
	}
}

// TestBcryptByteCeilingEnforced bcrypt 只取前 72 字节且静默丢弃剩余部分。
// 策略的 MaxLength 上限是 256，因此这条必须由策略层单独把关 ——
// 否则前 72 字节相同的两个口令可以互相登录。
func TestBcryptByteCeilingEnforced(t *testing.T) {
	policy := appdomain.PasswordPolicy{MinLength: 8, MaxLength: 256, MinScore: 0}
	// 30 个汉字 = 90 字节，字符数远未触顶 MaxLength
	check := CheckPasswordPolicy(strings.Repeat("霜", 15)+strings.Repeat("雪", 15), policy)
	if check.IsValid {
		t.Fatal("90 字节的口令应当因超出 bcrypt 72 字节上限被拒")
	}
	if !containsSubstring(check.Violations, "字节") {
		t.Errorf("违规说明没点出字节上限: %v", check.Violations)
	}
}

// TestMalformedPasswordRejected PRECIS（RFC 8265 OpaqueString）挡掉畸形口令。
func TestMalformedPasswordRejected(t *testing.T) {
	for _, password := range []string{"", "abc\x00def", "abc\x07def"} {
		analysis := AnalyzePasswordStrength(password)
		if analysis.Level != "invalid" {
			t.Errorf("畸形口令 %q 应判 invalid, 实际 %q(%d 分)", password, analysis.Level, analysis.Score)
		}
		if check := CheckPasswordPolicy(password, defaultPasswordPolicy()); check.IsValid {
			t.Errorf("畸形口令 %q 通过了策略校验", password)
		}
	}
}

// TestPasswordScoreFromGuesses 评分映射必须单调，且锚点落在 zxcvbn 自己的档位上 ——
// 内置模板的门槛（30/50/70/80）就是照这套刻度定的，改动这里会静默改变
// 所有应用的实际密码要求。
func TestPasswordScoreFromGuesses(t *testing.T) {
	cases := []struct {
		guesses float64
		want    int
	}{
		{0, 0}, {1, 0}, {1e3, 20}, {1e6, 40}, {1e8, 55}, {1e10, 70}, {1e13, 85}, {1e16, 100}, {1e30, 100},
	}
	for _, item := range cases {
		if got := passwordScoreFromGuesses(item.guesses); got != item.want {
			t.Errorf("passwordScoreFromGuesses(%g) = %d, 期望 %d", item.guesses, got, item.want)
		}
	}
	previous := -1
	for exp := 0.0; exp <= 18; exp += 0.5 {
		score := passwordScoreFromGuesses(pow10(exp))
		if score < previous {
			t.Fatalf("评分非单调: 10^%g 得 %d, 前一档 %d", exp, score, previous)
		}
		previous = score
	}
}

// TestChineseDictionaryLabelled 中文补充词表与用户上下文共用 zxcvbn 的同一个
// 外部词表槽位，来源靠命中词反查。这条守住两者没有互相串标签。
func TestChineseDictionaryLabelled(t *testing.T) {
	analysis := AnalyzePasswordStrength("woaini1314")
	if !containsSubstring(analysis.Details.HasCommonPatterns, chineseDictionaryLabel) {
		t.Errorf("中文弱口令未被标记: %v", analysis.Details.HasCommonPatterns)
	}
	withAccount := AnalyzePasswordStrengthWithContext("zhaoyunfei", PasswordContext{Account: "zhaoyunfei"})
	if !containsSubstring(withAccount.Details.HasCommonPatterns, labelUserInput) {
		t.Errorf("账号复用未被标记: %v", withAccount.Details.HasCommonPatterns)
	}
}

// TestPatternsCarryNoPlaintext 模式明细会经 API 出网并进审计日志，
// 绝不能回显口令原文 —— 否则「测试密码策略」这个接口就成了密码泄露通道。
func TestPatternsCarryNoPlaintext(t *testing.T) {
	const password = "qwerty5201314"
	analysis := AnalyzePasswordStrength(password)
	if len(analysis.Details.Patterns) == 0 {
		t.Fatal("这个口令应当命中若干模式")
	}
	for _, item := range analysis.Details.Patterns {
		for _, field := range []string{item.Kind, item.Label, item.Source} {
			if field != "" && strings.Contains(password, field) {
				t.Errorf("模式明细回显了口令片段: %q", field)
			}
		}
		if item.Start < 1 || item.End < item.Start || item.End > len([]rune(password)) {
			t.Errorf("位置区间越界: %d-%d", item.Start, item.End)
		}
	}
}

// TestCheckPasswordPolicyCharacterClasses 字符类要求仍按原语义生效。
func TestCheckPasswordPolicyCharacterClasses(t *testing.T) {
	policy := appdomain.PasswordPolicy{
		MinLength: 8, MaxLength: 128, MinScore: 0,
		RequireUppercase: true, RequireLowercase: true,
		RequireNumbers: true, RequireSpecialChars: true,
	}
	check := CheckPasswordPolicy("7xkq2mvzp4wr", policy)
	if check.IsValid {
		t.Fatal("缺大写与特殊字符却通过了")
	}
	if !containsSubstring(check.Violations, "大写") || !containsSubstring(check.Violations, "特殊字符") {
		t.Errorf("违规项不完整: %v", check.Violations)
	}
	if check := CheckPasswordPolicy("7xKq2mVzP4wR!", policy); !check.IsValid {
		t.Errorf("满足全部字符类要求却被拒: %v", check.Violations)
	}
}

func containsSubstring(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(item, needle) {
			return true
		}
	}
	return false
}

func pow10(exp float64) float64 {
	result := 1.0
	for i := 0.0; i+1 <= exp; i++ {
		result *= 10
	}
	if rest := exp - float64(int(exp)); rest > 0 {
		result *= 3.1622776601683795 // 10^0.5
	}
	return result
}
