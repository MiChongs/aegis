package i18n

import (
	"testing"

	"golang.org/x/text/language"
)

func TestNegotiatePicksBestAvailable(t *testing.T) {
	available := []language.Tag{language.SimplifiedChinese, language.English, language.Japanese}
	fallback := language.English

	cases := []struct {
		name  string
		prefs []string
		want  language.Tag
	}{
		{"精确命中", []string{"ja"}, language.Japanese},
		{"Accept-Language 整段带权重", []string{"ja;q=0.4,zh-CN;q=0.9"}, language.SimplifiedChinese},
		{"zh-CN 归到 zh-Hans", []string{"zh-CN"}, language.SimplifiedChinese},
		{"前一个不认识就看后一个", []string{"xx-YY", "ja"}, language.Japanese},
		{"空串跳过", []string{"", "ja"}, language.Japanese},
		{"全不认识回落", []string{"ko", "ar"}, language.English},
		{"没有偏好回落", nil, language.English},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Negotiate(available, fallback, tc.prefs...); got != tc.want {
				t.Fatalf("Negotiate(%v) = %s, want %s", tc.prefs, got, tc.want)
			}
		})
	}
}

// 兜底项排在首位是 x/text 匹配器的要求。顺序错了的表现是
// 「请求一个完全不认识的语言，拿回来的是列表里某个随机语言」。
func TestNegotiateKeepsFallbackFirstAndDeduped(t *testing.T) {
	tags := orderedTags(
		[]language.Tag{language.Japanese, language.English, language.SimplifiedChinese, language.English, language.Und},
		language.English,
	)
	if tags[0] != language.English {
		t.Fatalf("首位 = %s，必须是 fallback", tags[0])
	}
	if len(tags) != 3 {
		t.Fatalf("去重后应有 3 个语言，实际 %d：%v", len(tags), tags)
	}
}

// fallback 不在可用列表里时必须被补进去，否则回落终点是悬空的。
func TestNegotiateAddsMissingFallback(t *testing.T) {
	got := Negotiate([]language.Tag{language.Japanese}, language.English, "ko")
	if got != language.English {
		t.Fatalf("got %s, want en", got)
	}
}

func TestParseTagRejectsGarbageInsteadOfPanicking(t *testing.T) {
	if tag := ParseTag("zh-Hans"); tag != language.SimplifiedChinese {
		t.Fatalf("zh-Hans → %s", tag)
	}
	for _, raw := range []string{"", "   ", "not a tag", "zh_Hans!!"} {
		if tag := ParseTag(raw); tag != language.Und {
			t.Fatalf("ParseTag(%q) = %s，应当是 Und", raw, tag)
		}
	}
}
