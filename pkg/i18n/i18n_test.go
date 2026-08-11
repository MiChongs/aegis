package i18n

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/text/language"
)

func testBundle(t *testing.T) *Bundle {
	t.Helper()
	fsys := fstest.MapFS{
		"locales/en.json": &fstest.MapFile{Data: []byte(`{
			"$meta": {"name":"English","nativeName":"English","direction":"ltr","script":"latin"},
			"$format": {
				"group": ",", "decimal": ".", "groupSizes": [3],
				"currency": "{symbol}{value}", "currencyNegative": "-{symbol}{value}",
				"date": "Jan 2, 2006", "dateTime": "Jan 2, 2006 15:04 MST", "time": "15:04"
			},
			"receipt.title": "RECEIPT",
			"receipt.hello": "Hello {name}",
			"receipt.items": {"one": "{count} item", "other": "{count} items"},
			"receipt.onlyEnglish": "English only"
		}`)},
		"locales/zh-Hans.json": &fstest.MapFile{Data: []byte(`{
			"$meta": {"name":"Simplified Chinese","nativeName":"简体中文","direction":"ltr","script":"han"},
			"$format": {"currency": "{symbol}{value}", "date": "2006年1月2日", "dateTime": "2006年1月2日 15:04 MST"},
			"receipt.title": "收据",
			"receipt.hello": "你好 {name}",
			"receipt.items": {"other": "{count} 件商品"}
		}`)},
		"locales/ru.json": &fstest.MapFile{Data: []byte(`{
			"$meta": {"name":"Russian","nativeName":"Русский","script":"cyrillic"},
			"$format": {"group": " ", "decimal": ",", "currency": "{value} {symbol}"},
			"receipt.title": "Квитанция",
			"receipt.items": {"one": "{count} товар", "few": "{count} товара", "many": "{count} товаров", "other": "{count} товара"}
		}`)},
	}
	bundle, err := LoadFS(fsys, "locales", language.English)
	if err != nil {
		t.Fatalf("装配失败：%v", err)
	}
	return bundle
}

func TestMatchNegotiatesAcceptLanguage(t *testing.T) {
	bundle := testBundle(t)
	cases := []struct {
		prefs []string
		want  string
	}{
		{[]string{"zh-CN"}, "zh-Hans"},
		{[]string{"zh-Hans-CN"}, "zh-Hans"},
		{[]string{"ru-RU,ru;q=0.9,en;q=0.8"}, "ru"},
		{[]string{"", "  ", "zh-CN"}, "zh-Hans"},    // 空偏好被跳过而不是当成命中
		{[]string{"xx-YY"}, "en"},                   // 完全不认识 → 默认语言
		{[]string{"!!!bad!!!", "zh-CN"}, "zh-Hans"}, // 语法错误的一项不该吃掉后面的偏好
		{nil, "en"},
	}
	for _, tc := range cases {
		if got := bundle.Match(tc.prefs...).String(); got != tc.want {
			t.Errorf("Match(%q) = %s，期望 %s", tc.prefs, got, tc.want)
		}
	}
}

// 缺译文必须回落到默认语言而不是留空 —— 一份凭证上的空白字段无从排查。
func TestMissingKeyFallsBackToDefaultLocale(t *testing.T) {
	zh := testBundle(t).Localizer(language.MustParse("zh-Hans"))
	if got := zh.T("receipt.onlyEnglish"); got != "English only" {
		t.Errorf("缺译文未回落到默认语言：%q", got)
	}
	if got := zh.T("receipt.nonexistent"); got != "receipt.nonexistent" {
		t.Errorf("两级都缺失时应露出 key，实得 %q", got)
	}
}

func TestInterpolation(t *testing.T) {
	l := testBundle(t).Localizer(language.English)
	if got := l.T("receipt.hello", Args{"name": "Ada"}); got != "Hello Ada" {
		t.Errorf("map 形式插值失败：%q", got)
	}
	if got := l.T("receipt.hello", "name", "Ada"); got != "Hello Ada" {
		t.Errorf("键值对形式插值失败：%q", got)
	}
	// 未提供的占位符原样保留：静默抹掉会让参数名写错表现为凭证上的空白
	if got := l.T("receipt.hello"); got != "Hello {name}" {
		t.Errorf("缺参数时应保留占位符：%q", got)
	}
}

// 复数规则必须来自 CLDR：英语 one/other、中文只有 other、俄语 one/few/many。
func TestPluralFollowsCLDRRules(t *testing.T) {
	bundle := testBundle(t)
	en := bundle.Localizer(language.English)
	if got := en.Plural("receipt.items", 1); got != "1 item" {
		t.Errorf("en/1 = %q", got)
	}
	if got := en.Plural("receipt.items", 3); got != "3 items" {
		t.Errorf("en/3 = %q", got)
	}
	zh := bundle.Localizer(language.MustParse("zh-Hans"))
	if got := zh.Plural("receipt.items", 1); got != "1 件商品" {
		t.Errorf("zh/1 = %q", got)
	}
	ru := bundle.Localizer(language.Russian)
	for count, want := range map[int]string{1: "1 товар", 3: "3 товара", 7: "7 товаров"} {
		if got := ru.Plural("receipt.items", count); got != want {
			t.Errorf("ru/%d = %q，期望 %q", count, got, want)
		}
	}
}

// 金额走定点数：18 位精度的订单金额过一遍 float64 就会失真。
func TestMoneyIsExactAndLocaleAware(t *testing.T) {
	bundle := testBundle(t)
	en := bundle.Localizer(language.English)
	ru := bundle.Localizer(language.Russian)

	if got := en.Money(decimal.RequireFromString("1234567.89"), "USD"); got != "$1,234,567.89" {
		t.Errorf("en/USD = %q", got)
	}
	// 俄语：空格分组、逗号小数点、符号后置
	if got := ru.Money(decimal.RequireFromString("1234.5"), "RUB"); got != "1 234,50 ₽" {
		t.Errorf("ru/RUB = %q", got)
	}
	// 日元没有小数位，补两位是外行错误
	if got := en.Money(decimal.RequireFromString("1234.4"), "JPY"); got != "¥1,234" {
		t.Errorf("en/JPY = %q", got)
	}
	if got := en.Money(decimal.RequireFromString("-42.5"), "USD"); got != "-$42.50" {
		t.Errorf("负数金额 = %q", got)
	}
	// 精度：超出 float64 安全整数范围仍需逐位准确
	huge := decimal.RequireFromString("99999999999999.99")
	if got := en.Money(huge, "USD"); got != "$99,999,999,999,999.99" {
		t.Errorf("大额失真：%q", got)
	}
	// 未知币种不该报错，退化成「代码 + 数值」
	if got := en.Money(decimal.RequireFromString("10"), "XYZ"); got != "XYZ10.00" {
		t.Errorf("未知币种 = %q", got)
	}
}

func TestMoneyWithCodeDisambiguates(t *testing.T) {
	en := testBundle(t).Localizer(language.English)
	if got := en.MoneyWithCode(decimal.RequireFromString("100"), "CNY"); got != "¥100.00 CNY" {
		t.Errorf("合计行应带 ISO 代码消歧：%q", got)
	}
}

func TestIndianGroupingSizes(t *testing.T) {
	locale := &Locale{
		Tag:      language.MustParse("hi"),
		Format:   FormatSpec{Group: ",", Decimal: ".", GroupSizes: []int{3, 2}, Currency: "{symbol}{value}"},
		messages: map[string]entry{},
	}
	bundle, err := New(map[language.Tag]*Locale{language.MustParse("hi"): locale}, language.MustParse("hi"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bundle.Localizer(language.MustParse("hi")).Money(decimal.RequireFromString("1234567"), "INR"); got != "₹12,34,567.00" {
		t.Errorf("印度记数法分组失败：%q", got)
	}
}

func TestDateFormatsPerLocale(t *testing.T) {
	bundle := testBundle(t)
	at := time.Date(2026, 3, 21, 15, 4, 5, 0, time.UTC)
	if got := bundle.Localizer(language.English).Date(at, time.UTC); got != "Mar 21, 2026" {
		t.Errorf("en 日期 = %q", got)
	}
	if got := bundle.Localizer(language.MustParse("zh-Hans")).Date(at, time.UTC); got != "2026年3月21日" {
		t.Errorf("zh 日期 = %q", got)
	}
	// 时区参数生效：收据上的时间必须是收件人所在时区
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("缺少时区数据库")
	}
	if got := bundle.Localizer(language.English).DateTime(at, shanghai); got != "Mar 21, 2026 23:04 CST" {
		t.Errorf("时区换算失败：%q", got)
	}
}

func TestFormatSpecInheritsDefaults(t *testing.T) {
	// zh-Hans 目录里没写 group/decimal，应当继承默认语言的，而不是变成空串
	zh := testBundle(t).Localizer(language.MustParse("zh-Hans"))
	if got := zh.Money(decimal.RequireFromString("1234.5"), "CNY"); got != "¥1,234.50" {
		t.Errorf("格式规范未继承默认语言：%q", got)
	}
}

func TestBundleRejectsCatalogWithoutDefaultLocale(t *testing.T) {
	fsys := fstest.MapFS{"locales/zh-Hans.json": &fstest.MapFile{Data: []byte(`{"a":"b"}`)}}
	if _, err := LoadFS(fsys, "locales", language.English); err == nil {
		t.Fatal("默认语言缺失时应当构造失败 —— 否则整条回落链是悬空的")
	}
}

func TestPluralCatalogMustDeclareOther(t *testing.T) {
	fsys := fstest.MapFS{"locales/en.json": &fstest.MapFile{Data: []byte(`{"a":{"one":"x"}}`)}}
	if _, err := LoadFS(fsys, "locales", language.English); err == nil {
		t.Fatal("缺少 other 形式时应当构造失败")
	}
}
