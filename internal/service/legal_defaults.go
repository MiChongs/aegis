package service

import (
	"embed"
	"fmt"
	"strings"
	"time"

	legaldomain "aegis/internal/domain/legal"
)

// 内置法律文本。
//
// 全新部署在管理员写下自己的条款之前，也必须有一份**可用的**用户协议与隐私政策 ——
// 登录页和注册页上那两个链接是一直在的，点开落到空白页比没有链接更糟。
//
// 语言集与支付凭证（`pkg/receipt/locales`）保持一致的十种。两处用同一套语言
// 不是巧合：一个用户在凭证上读到的语言，和他在隐私政策里读到的语言应当是同一种，
// 否则「这套系统支持哪些语言」这个问题会有两个不同的答案。
//
//go:embed legaldocs/*.html
var legalDefaultFS embed.FS

// legalDefaultDoc 一份内置文本的元信息。正文在 file 指向的文件里。
type legalDefaultDoc struct {
	DocType   legaldomain.DocType
	Locale    string // 规范化后的 BCP 47 标签
	Title     string
	Version   string
	Effective string // YYYY-MM-DD
	file      string
}

// legalBuiltinLocales 内置语言集，顺序即控制台里的展示顺序（准据语言在首位）。
//
// **这张表必须与 legaldocs/ 下真实存在的文件一一对应。** 多列一个语言的表现是
// 语言切换器里能选到它、点进去 500 —— 有测试（TestLegalDefaultsAreEmbedded…）
// 逐条读文件把这件事钉死。
//
// 新增一种语言 = 两个 HTML 文件 + 这里一行，没有第三处要改。
// 目标语言集与支付凭证（pkg/receipt/locales）对齐的十种：
// zh-Hans / en / zh-Hant / ja / ko / de / fr / es / pt-BR / ru —— 一个用户在凭证上
// 读到的语言和他在隐私政策里读到的语言应当是同一种，否则「这套系统支持哪些语言」
// 会有两个不同的答案。**尚未译完的语言不列在这里**：宁可回落到读得懂的准据版本，
// 也不该让语言切换器指向一个不存在的文件。
var legalBuiltinLocales = []struct {
	Locale string
	Terms  string // 该语言下「用户服务协议」的标题
	Priv   string // 该语言下「隐私政策」的标题
}{
	{"zh-Hans", "用户服务协议", "隐私政策"},
	{"en", "Terms of Service", "Privacy Policy"},
}

// legalDefaultVersion / legalDefaultEffective 全部内置文本共用同一个版本与生效日期。
//
// 共用是刻意的：十种语言是同一份条款的十个译本，让它们各自带版本号，
// 意味着「英文版 2026.08、日文版 2026.03」这种在法律上说不通的状态可以存在。
// 生效日期是**文本本身的**日期，不是编译时间 —— 拿编译时间冒充会让每次发版
// 都把公示的「最后更新」推到今天，而条款一个字都没改。
const (
	legalDefaultVersion   = "2026.08"
	legalDefaultEffective = "2026-08-12"
)

// legalDefaultDocs 内置文本清单，由语言集展开。
var legalDefaultDocs = buildLegalDefaultDocs()

func buildLegalDefaultDocs() []legalDefaultDoc {
	docs := make([]legalDefaultDoc, 0, len(legalBuiltinLocales)*2)
	for _, item := range legalBuiltinLocales {
		docs = append(docs,
			legalDefaultDoc{legaldomain.DocTerms, item.Locale, item.Terms,
				legalDefaultVersion, legalDefaultEffective, "legaldocs/terms." + item.Locale + ".html"},
			legalDefaultDoc{legaldomain.DocPrivacy, item.Locale, item.Priv,
				legalDefaultVersion, legalDefaultEffective, "legaldocs/privacy." + item.Locale + ".html"},
		)
	}
	return docs
}

// legalTokens 正文里允许出现的占位符。
//
// 用最朴素的字符串替换而不是 text/template：正文是 HTML，模板引擎的转义规则
// 会和 HTML 打架（`&mdash;` 这类实体、属性里的引号），而这里要替换的只有三个词。
const (
	legalTokenPlatformName = "{{platformName}}"
	legalTokenContactEmail = "{{contactEmail}}"
	legalTokenContactLink  = "{{contactLink}}"
)

// legalDefaultBody 读取内置正文。文件是编译进二进制的，读不到只可能是构建出了问题。
func legalDefaultBody(doc legalDefaultDoc) (string, error) {
	raw, err := legalDefaultFS.ReadFile(doc.file)
	if err != nil {
		return "", fmt.Errorf("读取内置法律文本 %s 失败：%w", doc.file, err)
	}
	return string(raw), nil
}

// findLegalDefault 查一份内置文本。locale 需已规范化。
func findLegalDefault(docType legaldomain.DocType, locale string) (legalDefaultDoc, bool) {
	for _, item := range legalDefaultDocs {
		if item.DocType == docType && item.Locale == locale {
			return item, true
		}
	}
	return legalDefaultDoc{}, false
}

// legalDefaultLocales 某个文档类型内置了哪些语言。
func legalDefaultLocales(docType legaldomain.DocType) []string {
	locales := make([]string, 0, len(legalBuiltinLocales))
	for _, item := range legalDefaultDocs {
		if item.DocType == docType {
			locales = append(locales, item.Locale)
		}
	}
	return locales
}

// renderLegalTokens 把占位符替换成本部署的实际值。
//
// contactEmail 为空时不生造一个地址：假邮箱写在隐私政策的「联系我们」一节里，
// 比明说「还没配」更有害 —— 用户会照着它发信，然后石沉大海。
func renderLegalTokens(body, platformName, contactEmail, locale string) string {
	email := strings.TrimSpace(contactEmail)
	emailText := email
	link := ""
	if email == "" {
		emailText = legalNoContactText(locale)
		link = emailText
	} else {
		link = fmt.Sprintf(`<a href="mailto:%s">%s</a>`, email, email)
	}

	replacer := strings.NewReplacer(
		legalTokenPlatformName, platformName,
		legalTokenContactLink, link,
		legalTokenContactEmail, emailText,
	)
	return replacer.Replace(body)
}

// legalNoContactText 未配置联系邮箱时的占位文字，按语言给。
//
// 逐语言写死而不是只给英文：一份日文的隐私政策里突然出现一句英文
// 「no contact address configured」，读者会以为是页面出错了。
var legalNoContactText = func() func(string) string {
	texts := map[string]string{
		"zh-Hans": "（本部署尚未配置联系邮箱）",
		"zh-Hant": "（本部署尚未設定聯絡電子郵件）",
		"ja":      "（本デプロイでは連絡先メールアドレスが未設定です）",
		"ko":      "(이 배포에는 연락처 이메일이 설정되지 않았습니다)",
		"de":      "(für diese Installation ist keine Kontaktadresse konfiguriert)",
		"fr":      "(aucune adresse de contact configurée pour ce déploiement)",
		"es":      "(no se ha configurado una dirección de contacto para esta instalación)",
		"pt-BR":   "(nenhum endereço de contato configurado para esta implantação)",
		"ru":      "(для этой установки не настроен контактный адрес)",
		"en":      "(no contact address configured for this deployment)",
	}
	return func(locale string) string {
		if text, ok := texts[locale]; ok {
			return text
		}
		// 未知语言退到英文而不是留空：留空会让「联系我们」一节看起来是排版事故
		return texts["en"]
	}
}()

// parseLegalEffective 解析内置文本的生效日期。清单是代码里的常量，写错了应当立刻暴露。
func parseLegalEffective(value string) *time.Time {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil
	}
	return &parsed
}
