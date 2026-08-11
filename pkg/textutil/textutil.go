// Package textutil 处理字符串在「字节预算」与「合法 UTF-8」之间的冲突。
//
// 这两件事在本项目里反复以同一种方式出错：`s[:n]` 按字节切片会把一个多字节字符
// 劈成两半，剩下的半个字符写进 Postgres 的 text 列会被直接拒收 ——
//
//	ERROR: invalid byte sequence for encoding "UTF8": 0xe7 (SQLSTATE 22021)
//
// 0xe4–0xe9 恰好是汉字在 UTF-8 里的首字节，所以中文界面几乎必然踩到。
// 麻烦的是报错发生在几层之外的写库处，与真正动手截断的那行代码毫无线索关联，
// 而写入方往往是 fire-and-forget 的（审计、留痕），只留一条 warn 就没了。
package textutil

import (
	"strings"
	"unicode/utf8"
)

// TruncateBytes 返回 s 的一个前缀，长度不超过 max 字节，且不切断任何字符。
//
// 预算按字节而非字符计，是因为约束本身就以字节计（数据库列宽、日志行上限）；
// 按字符数截断在全中文输入下仍会超出列宽三倍。
//
// 若 s 本身末尾就带着不完整的字符（例如上游已经按字节截过一刀），也会一并去掉。
func TruncateBytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) > max {
		cut := max
		// 回退到最近的字符起始字节；合法 UTF-8 下最多退 3 次
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return TrimPartialRune(s)
}

// TrimPartialRune 去掉结尾处不完整的 UTF-8 字符。
//
// 用在按字节累积的缓冲区上：分片边界与字符边界毫无关系，缓冲区随时可能停在
// 一个字符中间。这种场合没法只看单个分片 —— 分片的头部本身可能是上一片某个
// 字符的延续，边界只能在拼好的缓冲区上判定。
func TrimPartialRune(s string) string {
	for len(s) > 0 {
		// DecodeLastRuneInString 对非法编码返回 (RuneError, 1)，而一个货真价实的
		// U+FFFD 占 3 字节 —— 用 size 区分两者，否则会把用户原本写着的 "�" 啃掉
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// SanitizeUTF8 把非法 UTF-8 字节序列替换成 U+FFFD。
//
// 用在「不可信字节 → Postgres text 列」这道边界上。客户端能往 User-Agent、
// URL、非 JSON 请求体里塞任意字节，落库前不归一，一次请求就能让整条记录写不进去。
// 注意 encoding/json 编码时已自带这个转换，因此 jsonb 列不需要再过一遍。
func SanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, "�")
}
