package i18n

import (
	"sort"
	"strings"

	"golang.org/x/text/language"
)

// Negotiate 在一组可用语言里按偏好挑一个。
//
// 与 Bundle.Match 是同一套语义，区别只在语言表的来源：Bundle 的表来自编译期装载的
// 消息目录，而这里的表是运行时才知道的 —— 比如法律文本，管理员今天新写一份
// 日文版，明天这个语言就该出现在协商结果里。两处共用同一份实现，
// 免得出现「接口按一套规则协商、PDF 按另一套」这种只在特定语言下暴露的分歧。
//
// prefs 依次可以是显式指定的 locale、用户设置里的语言、Accept-Language 头 ——
// 前者为空或不认识时自动看后者，全不认识则回落到 fallback。
// 每个 pref 都按 Accept-Language 语法解析，因此可以直接把请求头整段传进来。
//
// fallback 不在 available 里时会被自动补进去：它是整条回落链的终点，
// 悬空的终点意味着「什么都没匹配上」时返回一个谁都没装载的语言。
func Negotiate(available []language.Tag, fallback language.Tag, prefs ...string) language.Tag {
	tags := orderedTags(available, fallback)
	if len(tags) == 1 {
		return tags[0]
	}
	matcher := language.NewMatcher(tags)

	for _, pref := range prefs {
		pref = strings.TrimSpace(pref)
		if pref == "" {
			continue
		}
		desired, _, err := language.ParseAcceptLanguage(pref)
		if err != nil || len(desired) == 0 {
			continue
		}
		// x/text 的匹配器在完全不认识时会返回列表首项，因此必须用 Confidence
		// 区分「真的匹配上了」与「兜底」—— 少了这一步，任何一个不认识的语言
		// 都会被当成命中 fallback，后面的 prefs 再也没有机会。
		_, index, confidence := matcher.Match(desired...)
		if confidence == language.No {
			continue
		}
		if index >= 0 && index < len(tags) {
			return tags[index]
		}
	}
	return fallback
}

// orderedTags 把 fallback 排到首位、其余去重后按字典序排列。
//
// 首位是 x/text 匹配器的兜底项，顺序错了会让「完全不认识的语言」落到一个
// 随机的语言上；字典序是为了让同一组语言每次得到同一个匹配器，
// 免得协商结果随 map 遍历顺序抖动。
func orderedTags(available []language.Tag, fallback language.Tag) []language.Tag {
	seen := map[language.Tag]bool{fallback: true}
	rest := make([]language.Tag, 0, len(available))
	for _, tag := range available {
		if tag == language.Und || seen[tag] {
			continue
		}
		seen[tag] = true
		rest = append(rest, tag)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].String() < rest[j].String() })
	return append([]language.Tag{fallback}, rest...)
}

// ParseTag 宽松解析一个语言标签，解析不出来时返回 language.Und。
//
// 用在「这个值是用户填的」的地方：管理员在控制台里给法律文本填语言时
// 打错一个字母不该让整个请求 500，而应当被识别为「不是合法语言」。
func ParseTag(raw string) language.Tag {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return language.Und
	}
	tag, err := language.Parse(raw)
	if err != nil {
		return language.Und
	}
	return tag
}
