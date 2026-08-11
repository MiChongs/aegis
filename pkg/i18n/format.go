package i18n

import (
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Number 整数按当前语言分组。
func (l *Localizer) Number(value int) string {
	return l.groupDigits(decimal.NewFromInt(int64(value)).Abs().String(), value < 0)
}

// Int64 同 Number，接收 int64。
func (l *Localizer) Int64(value int64) string {
	return l.groupDigits(decimal.NewFromInt(value).Abs().String(), value < 0)
}

// Decimal 定点小数按当前语言格式化，保留 scale 位小数。
func (l *Localizer) Decimal(value decimal.Decimal, scale int32) string {
	rounded := value.Round(scale)
	negative := rounded.IsNegative()
	digits := rounded.Abs().StringFixed(scale)
	return l.groupDigits(digits, negative)
}

// Money 金额按当前语言与币种格式化，例如 en 下的 "$1,234.56"、zh-Hans 下的 "¥1,234.56"、
// ja 下的 "￥1,235"（日元无小数位）。
//
// 小数位数取自 ISO 4217 的标准位数，而不是硬编码 2 位 —— 日元 / 韩元没有小数，
// 给它们补两位小数在收据上是明显的外行错误。
func (l *Localizer) Money(amount decimal.Decimal, code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	scale := int32(2)
	symbol := code
	if unit, err := currency.ParseISO(code); err == nil {
		if digits, _ := currency.Standard.Rounding(unit); digits >= 0 {
			scale = int32(digits)
		}
		symbol = l.currencySymbol(code, unit)
	} else if code == "" {
		symbol = ""
	}

	negative := amount.IsNegative()
	value := l.groupDigits(amount.Abs().Round(scale).StringFixed(scale), false)

	pattern := l.locale.Format.Currency
	if negative {
		pattern = l.locale.Format.CurrencyNegative
	}
	if pattern == "" {
		pattern = "{symbol}{value}"
		if negative {
			pattern = "-{symbol}{value}"
		}
	}
	replaced := strings.NewReplacer("{symbol}", symbol, "{value}", value, "{code}", code).Replace(pattern)
	return strings.TrimSpace(replaced)
}

// MoneyWithCode 在金额后附上 ISO 代码，用于同一页面出现多种货币、或符号本身有歧义时
// （¥ 既是人民币也是日元；$ 有十几个国家在用）。收据的合计行走这个。
func (l *Localizer) MoneyWithCode(amount decimal.Decimal, code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	formatted := l.Money(amount, code)
	if code == "" || strings.Contains(formatted, code) {
		return formatted
	}
	return formatted + " " + code
}

// CurrencyScale 该币种的标准小数位数。
func CurrencyScale(code string) int32 {
	unit, err := currency.ParseISO(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return 2
	}
	digits, _ := currency.Standard.Rounding(unit)
	if digits < 0 {
		return 2
	}
	return int32(digits)
}

// IsCurrencyCode 是否为合法的 ISO 4217 币种代码。
func IsCurrencyCode(code string) bool {
	_, err := currency.ParseISO(strings.ToUpper(strings.TrimSpace(code)))
	return err == nil
}

// Date / DateTime / Time 按当前语言的布局格式化。loc 为 nil 时用时间自带的时区。
func (l *Localizer) Date(t time.Time, loc *time.Location) string {
	return l.formatTime(t, loc, l.locale.Format.Date, "2006-01-02")
}

func (l *Localizer) DateTime(t time.Time, loc *time.Location) string {
	return l.formatTime(t, loc, l.locale.Format.DateTime, "2006-01-02 15:04:05 MST")
}

func (l *Localizer) Time(t time.Time, loc *time.Location) string {
	return l.formatTime(t, loc, l.locale.Format.Time, "15:04:05")
}

func (l *Localizer) formatTime(t time.Time, loc *time.Location, layout, fallback string) string {
	if t.IsZero() {
		return ""
	}
	if loc != nil {
		t = t.In(loc)
	}
	if strings.TrimSpace(layout) == "" {
		layout = fallback
	}
	return t.Format(layout)
}

// groupDigits 给「已经是纯数字串」的值加上分组分隔符与小数点。
// 输入形如 "1234567.89"，全程字符串运算，不经浮点。
func (l *Localizer) groupDigits(digits string, negative bool) string {
	spec := l.locale.Format
	intPart, fracPart, _ := strings.Cut(digits, ".")

	group := spec.Group
	sizes := spec.GroupSizes
	if len(sizes) == 0 {
		sizes = []int{3}
	}
	var b strings.Builder
	if negative {
		b.WriteByte('-')
	}
	if group == "" {
		b.WriteString(intPart)
	} else {
		b.WriteString(groupWithSizes(intPart, group, sizes))
	}
	if fracPart != "" {
		sep := spec.Decimal
		if sep == "" {
			sep = "."
		}
		b.WriteString(sep)
		b.WriteString(fracPart)
	}
	return b.String()
}

// groupWithSizes 从低位起按 sizes 分组，最后一个尺寸对剩余高位重复使用。
// 这样 [3] 得到西式的 1,234,567，[3,2] 得到印度记数法的 12,34,567。
func groupWithSizes(intPart, group string, sizes []int) string {
	if len(intPart) == 0 {
		return intPart
	}
	var chunks []string
	remaining := intPart
	for i := 0; len(remaining) > 0; i++ {
		size := sizes[min(i, len(sizes)-1)]
		if size <= 0 || len(remaining) <= size {
			chunks = append(chunks, remaining)
			break
		}
		cut := len(remaining) - size
		chunks = append(chunks, remaining[cut:])
		remaining = remaining[:cut]
	}
	// chunks 是从低位往高位收集的，输出前反转
	for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
		chunks[i], chunks[j] = chunks[j], chunks[i]
	}
	return strings.Join(chunks, group)
}

// currencySymbol 取当前语言下的货币符号：目录覆盖 → x/text 的 CLDR 数据 → ISO 代码。
//
// 之所以允许目录覆盖：CLDR 在部分语言下给出的是消歧写法（en 下人民币是 "CN¥"），
// 而收据的抬头已经写明了商户与地区，此时用本地符号更自然。要不要覆盖是排版决定，
// 该由译者在目录里表达，不该写死在代码里。
func (l *Localizer) currencySymbol(code string, unit currency.Unit) string {
	if symbol, ok := l.locale.Format.Symbols[code]; ok && symbol != "" {
		return symbol
	}
	return cldrSymbol(l.locale.Tag, unit)
}

var symbolCache sync.Map // struct{tag,code} → string

type symbolKey struct {
	tag  language.Tag
	code string
}

// cldrSymbol 从 x/text 的 CLDR 数据里取符号。
//
// x/text 只提供「带金额的完整格式」而没有裸符号的导出接口，因此这里格式化一个 0
// 再把数字部分剥掉。货币符号里不含数字与千分位/小数点符号，剥离是安全的。
func cldrSymbol(tag language.Tag, unit currency.Unit) string {
	key := symbolKey{tag: tag, code: unit.String()}
	if cached, ok := symbolCache.Load(key); ok {
		return cached.(string)
	}
	formatted := message.NewPrinter(tag).Sprint(currency.NarrowSymbol(unit.Amount(0)))
	symbol := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) || r == '.' || r == ',' || r == ' ' || r == ' ' || r == '\'' {
			return -1
		}
		return r
	}, formatted))
	if symbol == "" {
		symbol = unit.String()
	}
	symbolCache.Store(key, symbol)
	return symbol
}
