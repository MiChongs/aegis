package service

import (
	"strings"
	"testing"

	legaldomain "aegis/internal/domain/legal"
)

// 内置文本是编译进二进制的。文件名写错、embed 指令漏掉某个文件，
// 表现都是运行时「该法律文本暂未提供」—— 而登录页那两个链接一直都在。
func TestLegalDefaultsAreEmbeddedAndSubstantial(t *testing.T) {
	if len(legalDefaultDocs) == 0 {
		t.Fatal("内置法律文本清单为空")
	}
	for _, def := range legalDefaultDocs {
		body, err := legalDefaultBody(def)
		if err != nil {
			t.Fatalf("%s/%s 读取失败：%v", def.DocType, def.Locale, err)
		}
		// 一份法律文本短于 2000 字符基本可以断定是被截断或放错了文件
		if len(body) < 2000 {
			t.Errorf("%s/%s 正文只有 %d 字节，太短了", def.DocType, def.Locale, len(body))
		}
		if !strings.Contains(body, "<h2") {
			t.Errorf("%s/%s 没有分节标题", def.DocType, def.Locale)
		}
		if parseLegalEffective(def.Effective) == nil {
			t.Errorf("%s/%s 的生效日期 %q 解析不出来", def.DocType, def.Locale, def.Effective)
		}
	}
}

// 回落终点必须存在，否则「请求一个没有内置版本的语言」会一路走到 404。
func TestLegalDefaultsCoverEveryDocTypeInFallbackLocale(t *testing.T) {
	for _, docType := range legaldomain.DocTypes() {
		if _, ok := findLegalDefault(docType, legalDefaultLocale.String()); !ok {
			t.Errorf("%s 缺少回落语言 %s 的内置版本", docType, legalDefaultLocale)
		}
		if len(legalDefaultLocales(docType)) == 0 {
			t.Errorf("%s 一个内置语言都没有", docType)
		}
	}
}

// 占位符必须被换干净。漏一个的表现是用户在隐私政策里读到 `{{contactEmail}}`。
func TestRenderLegalTokensLeavesNothingBehind(t *testing.T) {
	for _, def := range legalDefaultDocs {
		body, err := legalDefaultBody(def)
		if err != nil {
			t.Fatalf("读取失败：%v", err)
		}
		rendered := renderLegalTokens(body, "Acme Identity", "legal@acme.example", def.Locale)
		for _, token := range []string{legalTokenPlatformName, legalTokenContactEmail, legalTokenContactLink} {
			if strings.Contains(rendered, token) {
				t.Errorf("%s/%s 渲染后仍残留占位符 %s", def.DocType, def.Locale, token)
			}
		}
		if !strings.Contains(rendered, "Acme Identity") {
			t.Errorf("%s/%s 没有替换平台名", def.DocType, def.Locale)
		}
		if !strings.Contains(rendered, `href="mailto:legal@acme.example"`) {
			t.Errorf("%s/%s 联系邮箱没有变成可点链接", def.DocType, def.Locale)
		}
	}
}

// 没配联系邮箱时不生造地址：假邮箱写在隐私政策里比明说没配更有害。
func TestRenderLegalTokensWithoutContactEmailDoesNotInventOne(t *testing.T) {
	body, err := legalDefaultBody(legalDefaultDocs[0])
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	rendered := renderLegalTokens(body, "Acme", "", "zh-Hans")
	if strings.Contains(rendered, "mailto:") {
		t.Error("未配置联系邮箱时不应产生 mailto 链接")
	}
	if !strings.Contains(rendered, "尚未配置联系邮箱") {
		t.Error("未配置时应当明说，而不是留空")
	}

	english := renderLegalTokens(body, "Acme", "", "en")
	if !strings.Contains(english, "no contact address configured") {
		t.Error("英文版占位文字缺失")
	}
}

// 内置正文会原样注入页面，因此不能带脚本 —— 它们不经过写入端的净化。
func TestLegalDefaultsCarryNoScriptableMarkup(t *testing.T) {
	for _, def := range legalDefaultDocs {
		body, err := legalDefaultBody(def)
		if err != nil {
			t.Fatalf("读取失败：%v", err)
		}
		lowered := strings.ToLower(body)
		for _, bad := range []string{"<script", "javascript:", " onclick=", " onerror=", "<iframe"} {
			if strings.Contains(lowered, bad) {
				t.Errorf("%s/%s 含有 %q", def.DocType, def.Locale, bad)
			}
		}
	}
}

func TestNormalizeLegalLocale(t *testing.T) {
	cases := map[string]string{
		"zh-hans": "zh-Hans",
		"ZH-Hans": "zh-Hans",
		" en ":    "en",
		"ja":      "ja",
	}
	for input, want := range cases {
		got, err := normalizeLegalLocale(input)
		if err != nil {
			t.Fatalf("normalizeLegalLocale(%q) 报错：%v", input, err)
		}
		if got != want {
			t.Errorf("normalizeLegalLocale(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeLegalLocale("not a locale"); err == nil {
		t.Error("非法语言标签应当被拒绝")
	}
}
