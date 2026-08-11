package i18n

import (
	"maps"
	"strconv"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Args 消息插值参数。模板中用 {name} 引用。
type Args map[string]any

// Localizer 绑定到某个语言的本地化器。构造后只读，可并发使用。
type Localizer struct {
	bundle   *Bundle
	locale   *Locale
	fallback *Locale
}

// Tag 当前语言。
func (l *Localizer) Tag() language.Tag { return l.locale.Tag }

// Meta 当前语言的自描述信息。
func (l *Localizer) Meta() Meta { return l.locale.Meta }

// Format 当前语言的格式规范。
func (l *Localizer) Format() FormatSpec { return l.locale.Format }

// IsFallback 当前语言是否就是默认语言。
func (l *Localizer) IsFallback() bool { return l.locale.Tag == l.bundle.defaultTag }

// Has 该 key 在当前语言或默认语言中是否有译文。
func (l *Localizer) Has(key string) bool {
	if _, ok := l.locale.messages[key]; ok {
		return true
	}
	_, ok := l.fallback.messages[key]
	return ok
}

// T 取一条消息并插值。
//
// args 支持两种写法，按调用点的可读性自选：
//
//	l.T("receipt.orderNo")
//	l.T("receipt.greeting", i18n.Args{"name": "张三"})
//	l.T("receipt.greeting", "name", "张三")
//
// key 在当前语言缺失时回落到默认语言；再缺失则**返回 key 本身**而不是空串 ——
// 空白字段在一份凭证上无从排查，露出 key 至少能一眼看出漏了哪条译文。
func (l *Localizer) T(key string, args ...any) string {
	msg, ok := l.lookup(key)
	if !ok {
		return key
	}
	text := msg.text
	if text == "" && msg.plural != nil {
		text = msg.plural[plural.Other]
	}
	return interpolate(text, toArgs(args))
}

// Plural 取一条带复数形式的消息。count 会自动注入为 {count}，无需重复传。
//
// 复数形式按 CLDR 规则匹配：英语只区分 one/other，中日韩只有 other，
// 俄语有 one/few/many —— 这些差异由 x/text 的规则表决定，不该在业务代码里 if 出来。
func (l *Localizer) Plural(key string, count int, args ...any) string {
	msg, ok := l.lookup(key)
	if !ok {
		return key
	}
	values := toArgs(args)
	if values == nil {
		values = Args{}
	}
	if _, exists := values["count"]; !exists {
		values["count"] = l.Number(count)
	}
	if msg.plural == nil {
		return interpolate(msg.text, values)
	}
	form := plural.Cardinal.MatchPlural(l.locale.Tag, count, 0, 0, 0, 0)
	text, ok := msg.plural[form]
	if !ok {
		text = msg.plural[plural.Other]
	}
	return interpolate(text, values)
}

// lookup 当前语言 → 默认语言的两级查找。
func (l *Localizer) lookup(key string) (entry, bool) {
	if msg, ok := l.locale.messages[key]; ok {
		return msg, true
	}
	msg, ok := l.fallback.messages[key]
	return msg, ok
}

// interpolate 替换 {name} 占位符。未提供的占位符原样保留 ——
// 静默抹掉会让「参数名拼错了」这种错误在凭证上表现为莫名的空白。
func interpolate(text string, args Args) string {
	if len(args) == 0 || !strings.ContainsRune(text, '{') {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for i := 0; i < len(text); {
		open := strings.IndexByte(text[i:], '{')
		if open < 0 {
			b.WriteString(text[i:])
			break
		}
		b.WriteString(text[i : i+open])
		rest := text[i+open:]
		close := strings.IndexByte(rest, '}')
		if close < 0 {
			b.WriteString(rest)
			break
		}
		name := rest[1:close]
		if value, ok := args[name]; ok {
			b.WriteString(stringify(value))
		} else {
			b.WriteString(rest[:close+1])
		}
		i += open + close + 1
	}
	return b.String()
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	case interface{ String() string }:
		return v.String()
	default:
		return ""
	}
}

// toArgs 把 T/Plural 的可变参数统一成 Args。
func toArgs(args []any) Args {
	if len(args) == 0 {
		return nil
	}
	if len(args) == 1 {
		switch v := args[0].(type) {
		case Args:
			return cloneArgs(v)
		case map[string]any:
			return cloneArgs(v)
		}
	}
	out := make(Args, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		out[key] = args[i+1]
	}
	return out
}

// cloneArgs 复制一份，避免 Plural 注入 count 时改到调用方传进来的 map。
func cloneArgs(src map[string]any) Args {
	out := make(Args, len(src)+1)
	maps.Copy(out, src)
	return out
}
