package service

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	appdomain "aegis/internal/domain/app"
	apperrors "aegis/pkg/errors"

	"github.com/trustelem/zxcvbn"
	"github.com/trustelem/zxcvbn/match"
	"golang.org/x/text/secure/precis"
)

// 密码强度评估引擎。
//
// # 为什么不自己写
//
// 早期实现是一套手写启发式：正则查四类字符 + 每字符香农熵 + 十几个词的弱口令表。
// 它有两个结构性缺陷，不是补几个词能修的：
//
//   - **香农熵度量的是字符分布，不是可猜测性。** "abcabcabc" 的每字符熵是 1.58，
//     "Xy9$Kw" 是 2.58 —— 但前者在字典攻击下一秒即破。用它当强度依据，
//     等于奖励"字符种类多"而不是"难猜"。
//   - **弱口令靠 substring 匹配。** 名单里有 "password" 就只能挡 "password"，
//     挡不住 "Pa55word"、"drowssap"、"p@ssw0rd123"。而攻击者的字典早就覆盖了这些。
//
// 现在用 zxcvbn（Dropbox 提出、业界通用的口令强度估算算法）：它把口令拆成
// 字典词 / 键盘串 / 重复 / 递增序列 / 日期 / 年份 / l33t 替换 / 倒序 等模式的最优组合，
// 输出**攻击者需要的猜测次数**。评分从这个数推导，而不是从字符长相推导。
//
// # 分数刻度
//
// 对外仍是 0~100 —— 这个刻度已经落在每个应用的 `passwordPolicy.minScore` 里、
// 落在 `user_password_security_states.password_strength_score` 列里、
// 也画在控制台的滑块上。换刻度要迁移这三处，收益却只是换个数字表示法。
// 因此这里做的是「zxcvbn 猜测次数 → 0~100」的映射（见 passwordScoreFromGuesses）。

// PasswordContext 参与评估的用户上下文。
//
// zxcvbn 支持把这些串当成一份**临时字典**喂进去，于是 "账号叫 zhangsan、
// 密码设成 Zhangsan2024" 会被识别成字典命中而不是随机串 —— 这类口令在
// 拖库撞库里是最先被试的一批，手写正则永远抓不到。
type PasswordContext struct {
	Account  string
	Nickname string
	Email    string
	Phone    string
	AppName  string
}

// inputs 展开成 zxcvbn 的 userInputs。邮箱额外拆出 local part：
// 用 "zhang@corp.com" 注册的人，密码常写成 "zhang123" 而不是整个邮箱。
func (c PasswordContext) inputs() []string {
	raw := []string{c.Account, c.Nickname, c.Phone, c.AppName, c.Email}
	if local, _, ok := strings.Cut(c.Email, "@"); ok {
		raw = append(raw, local)
	}
	seen := make(map[string]struct{}, len(raw))
	inputs := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		inputs = append(inputs, item)
	}
	return inputs
}

// passwordScoreAnchors 猜测次数（log10）→ 0~100 分的锚点，其间线性插值。
//
// 锚点刻意压在 zxcvbn 自己的档位边界上，让 0~100 分保留可解释的含义，
// 而不是一条随手画的曲线：
//
//	log10  分数  含义（zxcvbn 原档）
//	  3     20   限速在线攻击可破（score 0→1）
//	  6     40   不限速在线攻击可破（score 1→2）
//	  8     55   离线慢哈希可破（score 2→3）
//	 10     70   离线快哈希可破（score 3→4）
//	 13     85   —
//	 16    100   —
//
// 于是内置模板的门槛依然说得通：默认 40 = 扛得住在线撞库；
// 严格 70（配 12 位下限）= 扛得住离线慢哈希；企业 80（配 14 位下限）更高一档。
var passwordScoreAnchors = []struct {
	log10 float64
	score float64
}{
	{0, 0}, {3, 20}, {6, 40}, {8, 55}, {10, 70}, {13, 85}, {16, 100},
}

// passwordScoreFromGuesses 把 zxcvbn 的猜测次数映射到 0~100。
func passwordScoreFromGuesses(guesses float64) int {
	if guesses <= 1 || math.IsNaN(guesses) {
		return 0
	}
	log10 := math.Log10(guesses)
	if math.IsInf(log10, 1) {
		return 100
	}
	last := passwordScoreAnchors[len(passwordScoreAnchors)-1]
	if log10 >= last.log10 {
		return 100
	}
	for i := 1; i < len(passwordScoreAnchors); i++ {
		high := passwordScoreAnchors[i]
		if log10 > high.log10 {
			continue
		}
		low := passwordScoreAnchors[i-1]
		span := high.log10 - low.log10
		if span <= 0 {
			return clampScore(int(math.Round(high.score)))
		}
		ratio := (log10 - low.log10) / span
		return clampScore(int(math.Round(low.score + ratio*(high.score-low.score))))
	}
	return 100
}

// bcryptPasswordByteLimit bcrypt 只取前 72 字节，超出部分**静默丢弃**。
// 策略允许的 MaxLength 最大到 256，因此这条限制必须由策略层单独把关，
// 否则两个前 72 字节相同的密码可以互相登录，而任何一层都不会报错。
const bcryptPasswordByteLimit = 72

// normalizePasswordInput 按 RFC 8265 的 OpaqueString profile 归一化并校验口令。
//
// 这是 IETF 为「密码」这一类字符串专门定的 PRECIS profile，x/text 里有现成实现：
// 统一各种 Unicode 空格为 U+0020、做 NFC 规范化、拒绝控制字符与非法码点。
// 手写等价物意味着自己维护一张 Unicode 类别表，每个 Unicode 版本都要跟。
//
// **归一化结果只用于强度评估，不用于哈希。** 存量用户的 bcrypt 哈希是按原始
// 字节算的，改哈希输入会让他们全部登不上去 —— 这种迁移必须配合「下次登录时
// 用旧串验过再按新串重算」的双写，不在本次范围内。因此这里只做两件事：
// 挡掉畸形口令，以及给 zxcvbn 一个稳定的输入。
func normalizePasswordInput(password string) (string, error) {
	if password == "" {
		return "", apperrors.New(40007, http.StatusBadRequest, "密码不能为空")
	}
	if !utf8.ValidString(password) {
		return "", apperrors.New(40007, http.StatusBadRequest, "密码包含非法字符编码")
	}
	normalized, err := precis.OpaqueString.String(password)
	if err != nil {
		// precis 的错误文案是英文且面向协议实现者，直接透传对用户没有意义
		return "", apperrors.New(40007, http.StatusBadRequest, "密码包含不被允许的字符（如控制字符或未分配码点）")
	}
	if normalized == "" {
		return "", apperrors.New(40007, http.StatusBadRequest, "密码不能全部由空白字符组成")
	}
	return normalized, nil
}

// AnalyzePasswordStrength 评估口令强度（无用户上下文）。
//
// 保留这个签名是为了兼容既有调用点；能拿到账号 / 昵称的链路应当改用
// AnalyzePasswordStrengthWithContext，那条路径才能识别「拿账号当密码」。
func AnalyzePasswordStrength(password string) appdomain.PasswordStrengthAnalysis {
	return AnalyzePasswordStrengthWithContext(password, PasswordContext{})
}

// AnalyzePasswordStrengthWithContext 带用户上下文的口令强度评估。
func AnalyzePasswordStrengthWithContext(password string, pctx PasswordContext) appdomain.PasswordStrengthAnalysis {
	normalized, err := normalizePasswordInput(password)
	if err != nil {
		return appdomain.PasswordStrengthAnalysis{
			Score:    0,
			Level:    "invalid",
			Feedback: []string{err.Error()},
			Details: appdomain.PasswordStrengthDetails{
				Length:            utf8.RuneCountInString(password),
				ByteLength:        len(password),
				HasCommonPatterns: []string{},
			},
			Recommendations: []appdomain.PasswordRecommendation{},
		}
	}

	// 只把前 72 字节交给 zxcvbn。这不是近似，而是**精确**：bcrypt 只哈希前
	// 72 字节，后面的内容对攻击者完全不存在，口令的真实强度就是这段前缀的强度。
	//
	// 同时这是一道必要的 DoS 闸门 —— zxcvbn 的匹配是超线性的，实测 256 字符
	// 要跑 0.5 秒以上，而注册接口的口令长度由请求方指定。策略 MaxLength
	// 最大可配到 256，没有这道闸门就等于开放了一个 CPU 消耗型攻击面。
	scored := truncateToBytes(normalized, bcryptPasswordByteLimit)
	inputs := append(pctx.inputs(), chineseWeakPasswordDictionary()...)
	result := zxcvbn.PasswordStrength(scored, inputs)

	// zxcvbn 对无效 UTF-8 会返回零值 Result（Guesses = 0）。上面已经挡过一次，
	// 这里兜底避免 log10(0) 变成 -Inf 传染整条链路。
	guesses := result.Guesses
	if guesses < 1 {
		guesses = 1
	}

	details := appdomain.PasswordStrengthDetails{
		Length:     utf8.RuneCountInString(normalized),
		ByteLength: len(normalized),
		Entropy:    math.Log2(guesses),
	}
	for _, r := range normalized {
		switch {
		case unicode.IsLower(r):
			details.HasLowercase = true
		case unicode.IsUpper(r):
			details.HasUppercase = true
		}
		// 数字与标点用 unicode 表判定而不是 ASCII 字符集正则：
		// 全角数字、中文标点同样是"数字 / 特殊字符"，用 [0-9] 会把它们判成
		// "既不是数字也不是字母"，凭空给一个弱口令加分。
		if unicode.IsDigit(r) || unicode.IsNumber(r) {
			details.HasNumbers = true
		}
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			details.HasSpecialChars = true
		}
	}
	// 位置换算按送进 zxcvbn 的那份串来，否则超长口令的区间会对不上
	details.Patterns = describePasswordPatterns(scored, result.Sequence)
	details.HasCommonPatterns = patternLabels(details.Patterns)

	score := passwordScoreFromGuesses(guesses)
	analysis := appdomain.PasswordStrengthAnalysis{
		Score:        score,
		Level:        passwordStrengthLevel(score),
		Details:      details,
		ZxcvbnScore:  result.Score,
		GuessesLog10: math.Log10(guesses),
		// 1e4 次/秒 = 离线慢哈希场景，对应平台实际使用的 bcrypt
		CrackTime: describeCrackTime(guesses / 1e4),
	}
	analysis.Feedback = buildPasswordFeedback(analysis)
	analysis.Recommendations = generatePasswordRecommendations(score, details)
	return analysis
}

// truncateToBytes 截到不超过 limit 字节的最长前缀，且不切断多字节字符。
func truncateToBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func passwordStrengthLevel(score int) string {
	switch {
	case score >= 80:
		return "very_strong"
	case score >= 60:
		return "strong"
	case score >= 40:
		return "medium"
	case score >= 20:
		return "weak"
	default:
		return "very_weak"
	}
}

// 模式标签常量。判定逻辑（fatalPasswordPattern）要按来源分流，
// 直接比字面量会在改文案时静默失效，因此统一走常量。
const (
	// userInputsDictionary zxcvbn 给外部词表固定的字典名。库只开放一份外部词表，
	// 用户上下文与中文补充词表共用它，只能按命中的词反查真实来源。
	userInputsDictionary = "user_inputs"

	labelUserInput          = "与账号信息相关"
	sourceUserInput         = "账号相关信息"
	labelCommonWeak         = "常见弱密码"
	chineseDictionaryLabel  = "中文常见弱口令"
	chineseDictionarySource = "补充弱口令表"
)

// patternKindLabels zxcvbn 的匹配器名 → 中文标签。
var patternKindLabels = map[string]string{
	"dictionary": "字典词汇",
	"spatial":    "键盘模式",
	"repeat":     "重复模式",
	"sequence":   "连续序列",
	"regex":      "年份",
	"date":       "日期模式",
	"bruteforce": "无明显规律",
}

// patternDictionaryLabels zxcvbn 内置词表名 → 中文来源说明。
var patternDictionaryLabels = map[string]string{
	"passwords":          "泄露口令榜",
	"english_wikipedia":  "英文常用词",
	"female_names":       "常见名字",
	"male_names":         "常见名字",
	"surnames":           "常见姓氏",
	"us_tv_and_film":     "影视台词",
	userInputsDictionary: sourceUserInput,
}

// describePasswordPatterns 把 zxcvbn 的最优匹配序列翻译成可展示的模式明细。
//
// 只翻译 zxcvbn **最终选中的**那条序列（MostGuessableMatchSequence 的结果），
// 不是所有候选匹配 —— 候选里什么都有，全展示会让每个口令都挂满红标签。
func describePasswordPatterns(password string, sequence []*match.Match) []appdomain.PasswordPatternMatch {
	if len(sequence) == 0 {
		return nil
	}
	// zxcvbn 的 I/J 是字节下标，而对外报的是字符位置：中文口令下两者差 3 倍
	byteToRune := make([]int, len(password)+1)
	runeIndex := 0
	for i := range password {
		byteToRune[i] = runeIndex
		runeIndex++
	}
	byteToRune[len(password)] = runeIndex
	position := func(byteIndex int) int {
		if byteIndex < 0 {
			return 0
		}
		if byteIndex >= len(byteToRune) {
			return runeIndex
		}
		return byteToRune[byteIndex]
	}

	patterns := make([]appdomain.PasswordPatternMatch, 0, len(sequence))
	for _, m := range sequence {
		if m == nil {
			continue
		}
		// bruteforce 是"这段没有可识别规律"的兜底段，它恰恰是口令里最强的部分，
		// 报成"命中弱模式"会把强口令标红
		if m.Pattern == "bruteforce" {
			continue
		}
		label, ok := patternKindLabels[m.Pattern]
		if !ok {
			label = m.Pattern
		}
		item := appdomain.PasswordPatternMatch{
			Kind:    m.Pattern,
			Label:   label,
			Start:   position(m.I) + 1,
			End:     position(m.J) + 1,
			Guesses: m.Guesses,
		}
		if m.Pattern == "dictionary" {
			item.Source = patternDictionaryLabels[m.DictionaryName]
			if item.Source == "" {
				item.Source = m.DictionaryName
			}
			switch {
			case m.DictionaryName == userInputsDictionary:
				// 补充词表与用户上下文共用 user_inputs 这一个外部词表槽位
				// （zxcvbn 只开放一份，且字典名写死），只能按命中的词反查来源
				if label := classifySupplementalWord(m.MatchedWord); label != "" {
					item.Label = label
					item.Source = chineseDictionarySource
				} else {
					item.Label = labelUserInput
				}
			case m.DictionaryName == "passwords":
				item.Label = labelCommonWeak
			}
			// l33t 与倒序是"自以为安全"的两种典型改法，值得单独点名
			switch {
			case m.L33t:
				item.Label += "（数字符号替换）"
			case m.Reversed:
				item.Label += "（倒序）"
			}
		}
		patterns = append(patterns, item)
	}
	sort.SliceStable(patterns, func(i, j int) bool { return patterns[i].Start < patterns[j].Start })
	return patterns
}

// patternLabels 去重后的标签列表，供既有前端直接渲染成标签墙。
func patternLabels(patterns []appdomain.PasswordPatternMatch) []string {
	labels := make([]string, 0, len(patterns))
	seen := make(map[string]struct{}, len(patterns))
	for _, item := range patterns {
		if _, dup := seen[item.Label]; dup {
			continue
		}
		seen[item.Label] = struct{}{}
		labels = append(labels, item.Label)
	}
	return labels
}

// buildPasswordFeedback 生成面向用户的提示。
//
// 与旧实现的关键差别：**不再逐条罗列"建议包含大写字母"这类字符类要求**。
// 那种提示会把人推向 "Password1!" —— 四类字符齐全、zxcvbn 只需 10^3 次猜测。
// 这里先说清楚"哪一段弱"，再给可执行的建议。
func buildPasswordFeedback(analysis appdomain.PasswordStrengthAnalysis) []string {
	feedback := make([]string, 0, 4)
	for _, item := range analysis.Details.Patterns {
		suffix := ""
		if item.End >= item.Start {
			suffix = fmt.Sprintf("（第 %d-%d 位）", item.Start, item.End)
		}
		feedback = append(feedback, "包含"+item.Label+suffix)
		if len(feedback) >= 3 {
			break
		}
	}
	switch {
	case analysis.Score < 40:
		feedback = append(feedback, "预计"+analysis.CrackTime+"即可被离线破解，建议改用一串无规律的长口令")
	case analysis.Score < 70:
		feedback = append(feedback, "强度尚可；再加长几位比增加字符种类更有效")
	case len(feedback) == 0:
		feedback = append(feedback, "密码强度良好")
	}
	return feedback
}

// describeCrackTime 把秒数转成中文可读时长。
//
// zxcvbn 库自带的 displayTime 是英文且未导出，这里按同一套档位给中文。
func describeCrackTime(seconds float64) string {
	if math.IsNaN(seconds) || seconds < 1 {
		return "不到 1 秒"
	}
	const (
		minute  = 60.0
		hour    = minute * 60
		day     = hour * 24
		month   = day * 30
		year    = day * 365
		century = year * 100
	)
	switch {
	case seconds < minute:
		return fmt.Sprintf("%.0f 秒", seconds)
	case seconds < hour:
		return fmt.Sprintf("%.0f 分钟", seconds/minute)
	case seconds < day:
		return fmt.Sprintf("%.0f 小时", seconds/hour)
	case seconds < month:
		return fmt.Sprintf("%.0f 天", seconds/day)
	case seconds < year:
		return fmt.Sprintf("%.0f 个月", seconds/month)
	case seconds < century:
		return fmt.Sprintf("%.0f 年", seconds/year)
	default:
		return "数百年以上"
	}
}
