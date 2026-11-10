// Package suspension 账号封禁对用户的那一面：多语言文案（原因类别、错误提示、通知邮件、站内消息）、
// 申诉的视图与设置。契约见 docs/account-suspension.md。
package suspension

import (
	"strings"
)

// 支持的语言
const (
	LocaleEN   = "en"
	LocaleZhCN = "zh-CN"
	LocaleZhTW = "zh-TW"
	LocaleJA   = "ja"
	LocaleKO   = "ko"
)

// Locales 全部支持的语言
var Locales = []string{LocaleEN, LocaleZhCN, LocaleZhTW, LocaleJA, LocaleKO}

// DefaultLocale 取不到用户语言时用
const DefaultLocale = LocaleZhCN

// NormalizeLocale 把 Accept-Language 或语言标记归到支持的语言；空串返回 ""（调用方决定回落）。
//
// zh-Hans*、zh-CN、zh-SG、zh → zh-CN；zh-Hant*、zh-TW、zh-HK、zh-MO → zh-TW；其余取主语言，不支持的回落 en。
// Accept-Language 带多个时按出现顺序取第一个认得的（权重通常已按序排好）。
func NormalizeLocale(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, part := range strings.Split(raw, ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if tag == "" || tag == "*" {
			continue
		}
		if l := normalizeTag(tag); l != "" {
			return l
		}
	}
	return LocaleEN
}

func normalizeTag(tag string) string {
	t := strings.ToLower(strings.ReplaceAll(tag, "_", "-"))
	parts := strings.Split(t, "-")
	switch parts[0] {
	case "zh":
		for _, p := range parts[1:] {
			switch p {
			case "hant", "tw", "hk", "mo":
				return LocaleZhTW
			case "hans", "cn", "sg":
				return LocaleZhCN
			}
		}
		return LocaleZhCN
	case "en":
		return LocaleEN
	case "ja":
		return LocaleJA
	case "ko":
		return LocaleKO
	}
	return ""
}

// Pick 在一组语言文案里按语言挑，缺的回落 zh-CN，再缺回落 en
func Pick[T any](m map[string]T, locale string) T {
	if v, ok := m[locale]; ok {
		return v
	}
	if v, ok := m[LocaleZhCN]; ok {
		return v
	}
	return m[LocaleEN]
}
