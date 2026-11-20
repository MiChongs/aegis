package usernumber

import (
	"slices"
	"strconv"
)

// ── 规律目录 ──
//
// 每条规律有一个默认等级，应用可以改它落在哪一级，或者停用（PatternTiers 里写 -1）。
// 命中多条时取最高的一级；一条都不命中是普通号。
//
// 数据库里发默认号的函数（迁移 000165 的 aegis_user_number_is_plain）按这些规律的**超集**过滤：
// 凡是它放行的默认位数号码，这里一定判普通（TestPlainFilterIsSuperset 钉住）。
// 所以新增规律时，要么它在默认位数（8–12 位）下本来就被那个函数挡住，要么同步改那个函数。

// Pattern 一条规律。
type Pattern struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Example     string `json:"example"`
	DefaultTier int    `json:"defaultTier"`
	// Tier 应用里实际落在哪一级；-1 表示停用（只在带设置的目录里填）
	Tier  int `json:"tier"`
	match func(digits string) bool
	build func(length int, rnd Rand) string
	// lengths 这条规律只在这些位数上成立（空表示任意位数）
	lengths []int
}

// 规律键。
const (
	PatternAllSame    = "all_same"
	PatternLen5       = "len5"
	PatternLen6       = "len6"
	PatternLen7       = "len7"
	PatternLeopard5   = "leopard5"
	PatternRun4       = "run4"
	PatternStraight4  = "straight4"
	PatternYearTail   = "year_tail"
	PatternRun3Tail   = "run3_tail"
	PatternAABB       = "aabb"
	PatternABAB       = "abab"
	PatternLuckyTail  = "lucky_tail"
	PatternMirror     = "mirror"
	PatternDisabled   = -1
	maxPatternRetries = 400
)

var luckyTails = []string{"520", "1314", "168", "518", "666", "888", "999"}

var patterns = []Pattern{
	{Key: PatternAllSame, Name: "全同号", Example: "88888888", DefaultTier: TierSupreme,
		match: func(d string) bool { return runLength(d) == len(d) },
		build: func(n int, r Rand) string { return repeat(byte('1'+r.IntN(9)), n) }},
	{Key: PatternLen5, Name: "五位号", Example: "36521", DefaultTier: TierSupreme, lengths: []int{5},
		match: func(d string) bool { return len(d) == 5 },
		build: func(_ int, r Rand) string { return randomDigits(5, r) }},
	{Key: PatternLen6, Name: "六位号", Example: "473920", DefaultTier: TierLegend, lengths: []int{6},
		match: func(d string) bool { return len(d) == 6 },
		build: func(_ int, r Rand) string { return randomDigits(6, r) }},
	{Key: PatternLen7, Name: "七位号", Example: "5839201", DefaultTier: TierLegend, lengths: []int{7},
		match: func(d string) bool { return len(d) == 7 },
		build: func(_ int, r Rand) string { return randomDigits(7, r) }},
	{Key: PatternLeopard5, Name: "豹子号（五连及以上）", Example: "318888802", DefaultTier: TierLegend,
		match: func(d string) bool { return runLength(d) >= 5 },
		build: func(n int, r Rand) string { return embedRun(n, 5, r) }},
	{Key: PatternMirror, Name: "对称号", Example: "123454321", DefaultTier: TierRare,
		match: isMirror,
		build: buildMirror},
	{Key: PatternRun4, Name: "四连号", Example: "527777103", DefaultTier: TierRare,
		match: func(d string) bool { return runLength(d) >= 4 },
		build: func(n int, r Rand) string { return embedRun(n, 4, r) }},
	{Key: PatternStraight4, Name: "顺子号（四位）", Example: "601234957", DefaultTier: TierRare,
		match: func(d string) bool { return straightLength(d) >= 4 },
		build: buildStraight},
	{Key: PatternYearTail, Name: "年份尾号", Example: "538202026", DefaultTier: TierRare,
		match: hasYearTail,
		build: buildYearTail},
	{Key: PatternRun3Tail, Name: "尾号三连", Example: "530271888", DefaultTier: TierFine,
		match: func(d string) bool { return len(d) >= 3 && d[len(d)-1] == d[len(d)-2] && d[len(d)-2] == d[len(d)-3] },
		build: func(n int, r Rand) string { return withTail(n, repeat(byte('0'+r.IntN(10)), 3), r) }},
	{Key: PatternAABB, Name: "AABB", Example: "590335571", DefaultTier: TierFine,
		match: hasAABB,
		build: func(n int, r Rand) string { a, b := twoDigits(r); return embed(n, string([]byte{a, a, b, b}), r) }},
	{Key: PatternABAB, Name: "ABAB", Example: "507373219", DefaultTier: TierFine,
		match: hasABAB,
		build: func(n int, r Rand) string { a, b := twoDigits(r); return embed(n, string([]byte{a, b, a, b}), r) }},
	{Key: PatternLuckyTail, Name: "吉利尾号", Example: "730261314", DefaultTier: TierFine,
		match: func(d string) bool {
			return slices.ContainsFunc(luckyTails, func(t string) bool { return len(d) > len(t) && d[len(d)-len(t):] == t })
		},
		build: func(n int, r Rand) string { return withTail(n, luckyTails[r.IntN(len(luckyTails))], r) }},
}

// Patterns 规律目录；settings 不为空时填上应用里实际的等级（停用为 -1）。
func Patterns(settings *Settings) []Pattern {
	out := make([]Pattern, len(patterns))
	for i, p := range patterns {
		p.Tier = p.DefaultTier
		if settings != nil {
			p.Tier = settings.patternTier(p.Key, p.DefaultTier)
		}
		out[i] = p
	}
	return out
}

// FindPattern 按键找规律。
func FindPattern(key string) (Pattern, bool) {
	for _, p := range patterns {
		if p.Key == key {
			return p, true
		}
	}
	return Pattern{}, false
}

// PatternName 规律的中文名；不认识的原样返回。
func PatternName(key string) string {
	if p, ok := FindPattern(key); ok {
		return p.Name
	}
	return key
}

// Classification 一个号码的等级与命中的规律（按等级由高到低）。
type Classification struct {
	Number   int64    `json:"number"`
	Tier     int      `json:"tier"`
	TierName string   `json:"tierName"`
	Patterns []string `json:"patterns"`
}

// Classify 判一个号码的等级。号码本身合法与否不在这里判（见 Settings.ValidNumber）。
func Classify(number int64, settings *Settings) Classification {
	digits := strconv.FormatInt(number, 10)
	type hit struct {
		key  string
		tier int
	}
	var hits []hit
	for _, p := range patterns {
		tier := p.DefaultTier
		if settings != nil {
			tier = settings.patternTier(p.Key, p.DefaultTier)
		}
		if tier == PatternDisabled || !p.match(digits) {
			continue
		}
		hits = append(hits, hit{p.Key, tier})
	}
	slices.SortStableFunc(hits, func(a, b hit) int { return b.tier - a.tier })
	out := Classification{Number: number, Tier: TierCommon, Patterns: []string{}}
	for _, h := range hits {
		out.Patterns = append(out.Patterns, h.key)
	}
	if len(hits) > 0 {
		out.Tier = hits[0].tier
	}
	out.TierName = TierOf(out.Tier).Name
	return out
}

// ── 判定 ──

// runLength 最长的连续相同数字。
func runLength(d string) int {
	best, cur := 0, 0
	for i := 0; i < len(d); i++ {
		if i > 0 && d[i] == d[i-1] {
			cur++
		} else {
			cur = 1
		}
		best = max(best, cur)
	}
	return best
}

// straightLength 最长的步长 ±1 连续数字（0123、9876）。
func straightLength(d string) int {
	best := min(len(d), 1)
	up, down := 1, 1
	for i := 1; i < len(d); i++ {
		if d[i] == d[i-1]+1 {
			up++
		} else {
			up = 1
		}
		if d[i]+1 == d[i-1] {
			down++
		} else {
			down = 1
		}
		best = max(best, up, down)
	}
	return best
}

func hasAABB(d string) bool {
	for i := 0; i+3 < len(d); i++ {
		if d[i] == d[i+1] && d[i+2] == d[i+3] && d[i] != d[i+2] {
			return true
		}
	}
	return false
}

func hasABAB(d string) bool {
	for i := 0; i+3 < len(d); i++ {
		if d[i] == d[i+2] && d[i+1] == d[i+3] && d[i] != d[i+1] {
			return true
		}
	}
	return false
}

// hasYearTail 尾四位是 1950–2039 年。
func hasYearTail(d string) bool {
	if len(d) < 6 {
		return false
	}
	year, err := strconv.Atoi(d[len(d)-4:])
	return err == nil && year >= 1950 && year <= 2039
}

// isMirror 回文且至少 7 位、不是全同号（全同另有规律）。
func isMirror(d string) bool {
	if len(d) < 7 || runLength(d) == len(d) {
		return false
	}
	for i, j := 0, len(d)-1; i < j; i, j = i+1, j-1 {
		if d[i] != d[j] {
			return false
		}
	}
	return true
}

// ── 造号 ──

// Rand 造号用的随机源（math/rand/v2 的 *rand.Rand 满足它；测试里可以换成固定序列）。
type Rand interface {
	IntN(n int) int
}

func repeat(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

// randomDigits n 位随机数字，首位非 0。
func randomDigits(n int, r Rand) string {
	b := make([]byte, n)
	b[0] = byte('1' + r.IntN(9))
	for i := 1; i < n; i++ {
		b[i] = byte('0' + r.IntN(10))
	}
	return string(b)
}

func twoDigits(r Rand) (byte, byte) {
	a := byte('0' + r.IntN(10))
	b := byte('0' + r.IntN(9))
	if b >= a {
		b++
	}
	return a, b
}

// embed 在 n 位随机数里随机位置嵌入一段；落在首位时这段不能以 0 开头（否则整体右移一位）。
func embed(n int, part string, r Rand) string {
	if len(part) >= n {
		return part[:n]
	}
	base := []byte(randomDigits(n, r))
	pos := r.IntN(n - len(part) + 1)
	if pos == 0 && part[0] == '0' {
		pos = 1
	}
	copy(base[pos:], part)
	return string(base)
}

func withTail(n int, tail string, r Rand) string {
	if len(tail) >= n {
		return tail[len(tail)-n:]
	}
	return randomDigits(n-len(tail), r) + tail
}

func embedRun(n, run int, r Rand) string {
	return embed(n, repeat(byte('0'+r.IntN(10)), run), r)
}

func buildStraight(n int, r Rand) string {
	start := r.IntN(7) // 0–6，4 位顺子 start..start+3
	part := []byte{byte('0' + start), byte('1' + start), byte('2' + start), byte('3' + start)}
	if r.IntN(2) == 1 {
		slices.Reverse(part)
	}
	return embed(n, string(part), r)
}

func buildYearTail(n int, r Rand) string {
	year := 1950 + r.IntN(90)
	return withTail(n, strconv.Itoa(year), r)
}

func buildMirror(n int, r Rand) string {
	if n < 7 {
		n = 7
	}
	half := randomDigits((n+1)/2, r)
	b := []byte(half)
	for i := n/2 - 1; i >= 0; i-- {
		b = append(b, half[i])
	}
	return string(b)
}

// BuildNumber 按一条规律造一个 length 位的号码（位数被规律固定时用规律的位数）。
func BuildNumber(p Pattern, length int, r Rand) int64 {
	if len(p.lengths) > 0 {
		length = p.lengths[0]
	}
	n, _ := strconv.ParseInt(p.build(length, r), 10, 64)
	return n
}

// PlainNumber 一个 length 位、在 settings 下判普通的号码（最多试若干次，失败返回 0）。
func PlainNumber(length int, settings *Settings, r Rand) int64 {
	for range maxPatternRetries {
		n, _ := strconv.ParseInt(randomDigits(length, r), 10, 64)
		if Classify(n, settings).Tier == TierCommon {
			return n
		}
	}
	return 0
}

// NumberOfTier 造一个恰好落在 tier 级的号码：从这一级的规律里随机挑一条来造，造出来再判一次
// （顺手命中了更高的规律就重来）。这一级没有可用规律时返回 0。
func NumberOfTier(tier int, settings *Settings, r Rand) int64 {
	if tier == TierCommon {
		return PlainNumber(settings.DefaultLength, settings, r)
	}
	var pool []Pattern
	for _, p := range Patterns(settings) {
		if p.Tier != tier {
			continue
		}
		if len(p.lengths) > 0 && !settings.lengthAllowed(p.lengths[0]) {
			continue
		}
		pool = append(pool, p)
	}
	if len(pool) == 0 {
		return 0
	}
	for range maxPatternRetries {
		p := pool[r.IntN(len(pool))]
		length := settings.DefaultLength
		if !settings.lengthAllowed(length) {
			length = settings.MaxLength
		}
		n := BuildNumber(p, length, r)
		if settings.ValidNumber(n) == nil && Classify(n, settings).Tier == tier {
			return n
		}
	}
	return 0
}

// ContainingNumbers 自选时的联想：把 part 嵌进默认位数的随机号里，返回最多 count 个合法号码（未判占用）。
// part 本身就是合法位数时它排第一个。
func ContainingNumbers(part string, count int, settings *Settings, r Rand) []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(n int64) {
		if n > 0 && !seen[n] && settings.ValidNumber(n) == nil {
			seen[n] = true
			out = append(out, n)
		}
	}
	if n, err := strconv.ParseInt(part, 10, 64); err == nil && part != "" && part[0] != '0' {
		add(n)
	}
	length := max(settings.DefaultLength, len(part))
	for i := 0; len(out) < count && i < count*20; i++ {
		var s string
		switch {
		case len(part) >= length:
			break
		case i%3 == 0:
			s = withTail(length, part, r)
		default:
			s = embed(length, part, r)
		}
		if s == "" {
			break
		}
		n, _ := strconv.ParseInt(s, 10, 64)
		add(n)
	}
	return out
}
