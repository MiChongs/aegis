package contract

import "unicode"

// 作品语种（docs/music-personalization-2.md「模型增量」）。
const (
	LangZh    = "zh"
	LangEn    = "en"
	LangJa    = "ja"
	LangKo    = "ko"
	LangOther = "other"
)

// Langs 全部语种，顺序即控制台与客户端展示顺序。
var Langs = []string{LangZh, LangEn, LangJa, LangKo, LangOther}

// LangTitles 语种展示名。
var LangTitles = map[string]string{LangZh: "华语", LangEn: "欧美", LangJa: "日语", LangKo: "韩语", LangOther: "其他"}

// LangIndex 语种在 Langs 里的下标（未知为 other）。
func LangIndex(lang string) int {
	for i, l := range Langs {
		if l == lang {
			return i
		}
	}
	return len(Langs) - 1
}

// ValidLang 语种键是否合法。
func ValidLang(lang string) bool {
	for _, l := range Langs {
		if l == lang {
			return true
		}
	}
	return false
}

// DetectLang 按歌名与歌手的文字判定语种：假名 → ja，谚文 → ko，汉字 → zh，拉丁字母占多数 → en，其余 other。
// 只看文字，不看音源元数据（各音源没有统一的语种字段）；日文歌名全是汉字时会判成 zh，可以接受。
func DetectLang(title, artist string) string {
	var han, kana, hangul, latin, other int
	for _, s := range []string{title, artist} {
		for _, r := range s {
			switch {
			case unicode.In(r, unicode.Hiragana, unicode.Katakana):
				kana++
			case unicode.Is(unicode.Hangul, r):
				hangul++
			case unicode.Is(unicode.Han, r):
				han++
			case r < 0x250 && unicode.IsLetter(r):
				latin++
			case unicode.IsLetter(r):
				other++
			}
		}
	}
	switch {
	case kana > 0:
		return LangJa
	case hangul > 0:
		return LangKo
	case han > 0:
		return LangZh
	case latin > 0 && latin >= other:
		return LangEn
	case other > 0:
		return LangOther
	}
	return LangOther
}
