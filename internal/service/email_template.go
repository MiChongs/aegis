package service

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"

	"github.com/inbucket/html2text"
	"github.com/vanng822/go-premailer/premailer"
)

/*
邮件模板：平台发出的每一封信共用的骨架与文案。

三条结构性决定：

1. **Go 侧不出现任何 HTML 字面量。**
   业务代码只描述内容（一段话、一张字段表、一个验证码、一个按钮），
   HTML 由 `emailtpl/layout.gohtml` 渲染，样式由 `emailtpl/theme.css` 提供，
   premailer 在渲染时把样式表内联进标签（邮件客户端普遍不认 <style>）。
   旧实现是 400 行 `fmt.Sprintf` 拼字符串 + 手工 `html.EscapeString`：
   色值散落在几十处，转义漏一处就是注入。现在 html/template 按上下文自动转义。

2. **纯文本版从同一份内容模型渲染**（`layout.gotxt`），不是把 HTML 抓一遍。
   抓取版必然带上预览行的零宽字符与按钮重复文案，且 HTML 一改就悄悄劣化。

3. **文案每句只说一次事。**
   一封验证码邮件只需要回答三件事：这是什么码、多久失效、不是本人怎么办。
   旧文案在标题、引导句、字段行「用途」里把同一件事说三遍
   （「登录验证码」/「本次验证码用于登录校验，请在有效期内完成验证」/「用途：登录验证」），
   再加一句 12 个场景一字不差的「请勿将验证码泄露给任何人」。
   信息密度越低，真正要看的那行（验证码本身）越容易被略过。
*/

//go:embed emailtpl/layout.gohtml emailtpl/layout.gotxt emailtpl/theme.css
var emailTemplateFS embed.FS

// 字体沿用控制台的 --font-sans / --font-mono（globals.css），
// 后面追加系统字体兜底：邮件客户端不会去下 webfont。
//
// 字体名用单引号：这两个值会落进 style="" 属性，用双引号的话
// html/template 必须把它们转义成 &#34; 才不会提前闭合属性，
// 出来的样式表虽然合法但没法读，排查排版问题时全是实体编码。
const (
	mailFontSans = `'IBM Plex Sans','Segoe UI Variable Text','PingFang SC','Microsoft YaHei UI',-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif`
	mailFontMono = `'Maple Mono NF CN','Cascadia Code','SFMono-Regular',ui-monospace,Menlo,Consolas,monospace`
)

// 按钮取控制台的 --primary / --primary-foreground。
// VML 不认 class，只能把色值作为属性传进模板，所以这两个值在这里再出现一次。
const (
	mailButtonBG = "#18181b"
	mailButtonFG = "#fafafa"
)

// mso 条件注释由 Go 侧注入：html/template 会把模板源码里的 HTML 注释整段删掉，
// 写在 .gohtml 里的条件注释到不了收件人手上。
const (
	msoStart    = htmltemplate.HTML("<!--[if mso]>")
	msoEnd      = htmltemplate.HTML("<![endif]-->")
	nonMSOStart = htmltemplate.HTML("<!--[if !mso]><!-->")
	nonMSOEnd   = htmltemplate.HTML("<!--<![endif]-->")
)

var (
	emailHTMLTemplate = htmltemplate.Must(
		htmltemplate.ParseFS(emailTemplateFS, "emailtpl/layout.gohtml"),
	).Lookup("layout.gohtml")
	emailTextTemplate = texttemplate.Must(
		texttemplate.ParseFS(emailTemplateFS, "emailtpl/layout.gotxt"),
	).Lookup("layout.gotxt")
	emailThemeCSS = func() string {
		data, err := emailTemplateFS.ReadFile("emailtpl/theme.css")
		if err != nil {
			// embed 的文件缺失属于构建期问题，不是运行期状态
			panic("邮件样式表缺失: " + err.Error())
		}
		return string(data)
	}()
)

/* ── 内容模型 ── */

// emailDetail 字段行：左标签右值。
type emailDetail struct {
	Label string
	Value string
}

// mailBlock 正文积木。调用方只声明「要一段话 / 一个验证码 / 一张表」，
// 长什么样是模板的事。
type mailBlock struct {
	Kind    string
	Text    string
	Title   string
	Label   string
	Code    string
	Note    string
	URL     string
	Details []emailDetail
	// VMLWidth Outlook 那个圆角矩形的宽度。VML 不会随内容自适应，
	// 按字数估一个，太窄会把文字挤出矩形。
	VMLWidth int
}

// LastIndex 字段表最后一行的下标，模板据此决定哪几行画分隔线。
func (b mailBlock) LastIndex() int { return len(b.Details) - 1 }

func (b mailBlock) MSOStart() htmltemplate.HTML    { return msoStart }
func (b mailBlock) MSOEnd() htmltemplate.HTML      { return msoEnd }
func (b mailBlock) NonMSOStart() htmltemplate.HTML { return nonMSOStart }
func (b mailBlock) NonMSOEnd() htmltemplate.HTML   { return nonMSOEnd }

func mailParagraph(text string) mailBlock {
	return mailBlock{Kind: "paragraph", Text: strings.TrimSpace(text)}
}

func mailCode(code string, expireMinutes int) mailBlock {
	note := ""
	if expireMinutes > 0 {
		note = fmt.Sprintf("%d 分钟内有效", expireMinutes)
	}
	return mailBlock{Kind: "code", Code: strings.TrimSpace(code), Note: note}
}

func mailDetails(details ...emailDetail) mailBlock {
	filtered := make([]emailDetail, 0, len(details))
	for _, item := range details {
		if strings.TrimSpace(item.Label) == "" && strings.TrimSpace(item.Value) == "" {
			continue
		}
		filtered = append(filtered, emailDetail{
			Label: strings.TrimSpace(item.Label),
			Value: strings.TrimSpace(item.Value),
		})
	}
	return mailBlock{Kind: "details", Details: filtered}
}

func mailNotice(title string, text string) mailBlock {
	return mailBlock{Kind: "notice", Title: strings.TrimSpace(title), Text: strings.TrimSpace(text)}
}

func mailButton(label string, url string) mailBlock {
	trimmed := strings.TrimSpace(label)
	width := 44 + len([]rune(trimmed))*17
	if width < 160 {
		width = 160
	}
	if width > 420 {
		width = 420
	}
	return mailBlock{Kind: "button", Label: trimmed, URL: strings.TrimSpace(url), VMLWidth: width}
}

func mailLink(label string, url string) mailBlock {
	return mailBlock{Kind: "link", Label: strings.TrimSpace(label), URL: strings.TrimSpace(url)}
}

// emailLayout 一封信的全部内容。
//
// 语言是参数而不是两份模板：凭证邮件要按收件人的语言出具（含 <html lang>
// 与「请勿回复」那句话），而平台自身的验证码 / 密码重置邮件仍是中文。
type emailLayout struct {
	// Lang HTML 语言标记，留空为 zh-CN
	Lang string
	// AppName 品牌名，出现在标题上方与页脚
	AppName string
	// Eyebrow 品牌行后缀，如「收据」，留空则只显示品牌
	Eyebrow string
	Title   string
	Lead    string
	// Preheader 收件箱预览行，留空则退回 Lead
	Preheader string
	Blocks    []mailBlock
	// FooterNote 卡内末尾那句「不是本人怎么办」
	FooterNote string
	// NoReplyNote 页脚提示，留空用中文默认句
	NoReplyNote string
}

// mailTemplateData 交给模板的视图对象。
type mailTemplateData struct {
	Lang         string
	AppName      string
	Brand        string
	Title        string
	Lead         string
	Preheader    string
	PreheaderPad htmltemplate.HTML
	Blocks       []mailBlock
	FooterNote   string
	NoReply      string
	CSS          htmltemplate.CSS
	FontSans     htmltemplate.CSS
	FontMono     htmltemplate.CSS
	ButtonBG     string
	ButtonFG     string
}

// preheaderPad 撑开收件箱摘要的零宽字符，防止客户端把正文开头接在摘要后面。
const preheaderPad = htmltemplate.HTML(
	"&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;" +
		"&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;" +
		"&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;" +
		"&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;",
)

func (layout emailLayout) data() mailTemplateData {
	brand := strings.TrimSpace(layout.AppName)
	if eyebrow := strings.TrimSpace(layout.Eyebrow); eyebrow != "" {
		if brand != "" {
			brand += " · " + eyebrow
		} else {
			brand = eyebrow
		}
	}
	return mailTemplateData{
		Lang:         firstNonEmpty(layout.Lang, "zh-CN"),
		AppName:      strings.TrimSpace(layout.AppName),
		Brand:        brand,
		Title:        strings.TrimSpace(layout.Title),
		Lead:         strings.TrimSpace(layout.Lead),
		Preheader:    firstNonEmpty(layout.Preheader, layout.Lead, layout.Title),
		PreheaderPad: preheaderPad,
		Blocks:       layout.Blocks,
		FooterNote:   strings.TrimSpace(layout.FooterNote),
		NoReply:      firstNonEmpty(layout.NoReplyNote, "这是一封系统邮件，直接回复不会有人看到。"),
		CSS:          htmltemplate.CSS(emailThemeCSS),
		FontSans:     htmltemplate.CSS(mailFontSans),
		FontMono:     htmltemplate.CSS(mailFontMono),
		ButtonBG:     mailButtonBG,
		ButtonFG:     mailButtonFG,
	}
}

/* ── 渲染 ── */

// renderEmail 渲染一封信，返回 HTML 与配套纯文本。
//
// 内联失败时退回未内联的 HTML：样式表还在 <style> 里，Gmail 之类会丢掉样式，
// 但内容完整可读。为了排版问题让一封验证码邮件发不出去是本末倒置。
func renderEmail(layout emailLayout) (string, string) {
	data := layout.data()

	var rawHTML bytes.Buffer
	if err := emailHTMLTemplate.Execute(&rawHTML, data); err != nil {
		return "", ""
	}

	var text bytes.Buffer
	if err := emailTextTemplate.Execute(&text, data); err != nil {
		text.Reset()
	}

	inlined := rawHTML.String()
	// RemoveClasses 保持 false：深色模式与窄屏的媒体查询靠 class 生效，
	// premailer 内联不了媒体查询，会把它们原样留在 <style> 里。
	if pm, err := premailer.NewPremailerFromString(inlined, premailer.NewOptions(
		premailer.WithRemoveClasses(false),
		premailer.WithCssToAttributes(true),
		premailer.WithKeepBangImportant(true),
	)); err == nil {
		if result, err := pm.Transform(); err == nil {
			inlined = result
		}
	}

	return inlined, strings.TrimSpace(text.String())
}

// renderEmailLayoutWith 只要 HTML 的调用方用这个。
func renderEmailLayoutWith(layout emailLayout) string {
	html, _ := renderEmail(layout)
	return html
}

/* ── 验证码邮件 ── */

// emailPurposePresentation 一种验证码用途的全部文案。
//
// 每个字段只说一件事：
//   - Noun   进主题，例如「482913 是您的登录验证码」
//   - Title  信里的大标题
//   - Lead   输入这个码之后会发生什么（标题说不出来的那部分）
//   - Footer 不是本人操作时该做什么（各场景的处置不同，不是同一句套话）
type emailPurposePresentation struct {
	Noun   string
	Title  string
	Lead   string
	Footer string
}

// renderCodeMailContent 返回验证码邮件的主题、HTML 与纯文本。
//
// 主题把验证码放在最前面（「482913 是您的登录验证码」）：手机通知栏和
// 邮件列表都只显示开头十几个字，验证码放在那里往往不用点开邮件。
func renderCodeMailContent(appName string, configName string, purpose string, code string, expireMinutes int) (string, string, string) {
	if normalizeEmailPurpose(purpose) == "test" {
		return renderChannelTestMail(appName, configName, code)
	}

	p := getEmailPurposePresentation(purpose)
	trimmed := strings.TrimSpace(code)
	html, text := renderEmail(emailLayout{
		AppName:   appName,
		Title:     p.Title,
		Lead:      p.Lead,
		Preheader: fmt.Sprintf("验证码 %s，%d 分钟内有效", trimmed, expireMinutes),
		Blocks: []mailBlock{
			mailCode(trimmed, expireMinutes),
			mailParagraph("请不要把这串数字转发给任何人。"),
		},
		FooterNote: p.Footer,
	})
	return fmt.Sprintf("%s 是您的%s", trimmed, p.Noun), html, text
}

// renderChannelTestMail 通道自检邮件。
//
// 它不是验证码 —— 这串数字不写 Redis，验不了任何东西，只是让操作者把邮件里的
// 内容和控制台返回的值对一眼，确认中途没有被改写。写成「验证码」会让收到的人
// 以为账号上正在发生什么。
func renderChannelTestMail(appName string, configName string, code string) (string, string, string) {
	html, text := renderEmail(emailLayout{
		AppName:   appName,
		Eyebrow:   "运维",
		Title:     "邮件通道自检",
		Lead:      "这封信由控制台手动触发，能收到就说明这条通道是通的。",
		Preheader: "通道自检邮件，收到即代表投递正常",
		Blocks: []mailBlock{
			mailDetails(
				emailDetail{Label: "发送通道", Value: fallbackEmailValue(configName, "默认配置")},
				emailDetail{Label: "校验串", Value: strings.TrimSpace(code)},
			),
			mailParagraph("控制台上显示的校验串与这里一致，说明正文在投递途中没有被改写。"),
		},
		FooterNote: "这封信不涉及任何账号操作，看完可以直接删除。",
	})
	return fmt.Sprintf("%s 邮件通道自检", appName), html, text
}

func normalizeEmailPurpose(purpose string) string {
	return strings.ToLower(strings.TrimSpace(purpose))
}

func getEmailPurposePresentation(purpose string) emailPurposePresentation {
	switch normalizeEmailPurpose(purpose) {
	case "register", "signup", "sign-up":
		return emailPurposePresentation{
			Noun:   "注册验证码",
			Title:  "注册验证码",
			Lead:   "输入验证码完成注册，之后就用这个邮箱登录。",
			Footer: "不是您本人在注册的话，忽略这封信即可，账号不会被创建。",
		}
	case "login", "signin", "sign-in":
		return emailPurposePresentation{
			Noun:   "登录验证码",
			Title:  "登录验证码",
			Lead:   "输入验证码即可登录。",
			Footer: "如果不是您本人在登录，建议立刻改密码，并看一下最近的登录记录。",
		}
	case "admin_login":
		return emailPurposePresentation{
			Noun:   "管理员登录验证码",
			Title:  "管理后台登录验证码",
			Lead:   "输入验证码登录管理后台。",
			Footer: "后台账号权限高。不是您本人在登录的话，请立刻联系平台管理员。",
		}
	case "bind_email", "bind-email":
		return emailPurposePresentation{
			Noun:   "邮箱绑定验证码",
			Title:  "绑定这个邮箱",
			Lead:   "验证通过后，这个邮箱会绑定到您的账号。",
			Footer: "不是您本人在操作的话，忽略即可，绑定不会生效。",
		}
	case "change_email", "change-email", "profile_email_change":
		return emailPurposePresentation{
			Noun:   "邮箱变更验证码",
			Title:  "换成这个邮箱",
			Lead:   "验证通过后，账号邮箱会改成您当前正在查看的这个地址。",
			Footer: "变更完成后原邮箱会收到通知。不是您本人在操作的话，请立刻改密码。",
		}
	case "bind_phone", "bind-phone":
		return emailPurposePresentation{
			Noun:   "手机绑定验证码",
			Title:  "绑定手机号",
			Lead:   "验证通过后，这个手机号会绑定到您的账号。",
			Footer: "不是您本人在操作的话，忽略即可，绑定不会生效。",
		}
	case "change_phone", "change-phone", "profile_phone_change":
		return emailPurposePresentation{
			Noun:   "手机变更验证码",
			Title:  "更换手机号",
			Lead:   "验证通过后，账号手机号会换成新的那个。",
			Footer: "不是您本人在操作的话，请立刻改密码，并检查账号的绑定信息。",
		}
	case "password_reset", "reset_password", "reset-password":
		return emailPurposePresentation{
			Noun:   "密码重置验证码",
			Title:  "重置密码",
			Lead:   "输入验证码后就可以设置新密码。",
			Footer: "新密码设置好之前，原密码一直有效。不是您本人申请的话，忽略即可。",
		}
	case "verify_identity", "identity_verify", "identity-verification":
		return emailPurposePresentation{
			Noun:   "身份验证码",
			Title:  "身份验证",
			Lead:   "输入验证码完成这次身份核验。",
			Footer: "任何人向您要这串数字都是诈骗，包括自称客服的。",
		}
	case "two_factor", "two-factor", "2fa", "mfa":
		return emailPurposePresentation{
			Noun:   "两步验证码",
			Title:  "两步验证",
			Lead:   "密码已经通过，再输入验证码就能完成这次登录。",
			Footer: "如果您并没有在登录，说明密码可能已经泄露，请尽快更换。",
		}
	default:
		return emailPurposePresentation{
			Noun:   "验证码",
			Title:  "验证码",
			Lead:   "输入验证码完成这次操作。",
			Footer: "任何人向您要这串数字都是诈骗，包括自称客服的。",
		}
	}
}

/* ── 密码重置 / 欢迎 / 资料变更 ── */

func passwordResetBlocks(resetURL string, expireMinutes int) []mailBlock {
	if strings.TrimSpace(resetURL) == "" {
		// 这是配置问题，不是收件人能处理的事，所以直说缺哪一项
		return []mailBlock{
			mailNotice("重置入口尚未配置", "这个应用还没有填写 resetBaseURL，暂时无法自助重置，请联系管理员。"),
		}
	}
	return []mailBlock{
		mailParagraph(fmt.Sprintf("点下面的按钮设置新密码，链接 %d 分钟后失效。", expireMinutes)),
		mailButton("设置新密码", resetURL),
		mailLink("按钮打不开就复制这个链接", resetURL),
	}
}

func welcomeLead(appName string, userName string) string {
	if name := strings.TrimSpace(userName); name != "" {
		return fmt.Sprintf("%s，您在 %s 的账号已经可以用了。", name, strings.TrimSpace(appName))
	}
	return fmt.Sprintf("您在 %s 的账号已经可以用了。", strings.TrimSpace(appName))
}

/* ── HTML → 纯文本（仅用于正文由外部渲染的通道） ── */

// htmlToPlainText 从 HTML 正文提取纯文本备份。
//
// 模板渲染出来的信自带纯文本版（见 layout.gotxt），走不到这里；
// 这条路径留给「正文由调用方给一段现成 HTML」的通道。
// 用 html2text 而不是自己遍历节点：列表、表格、链接的降级规则它都处理过。
func htmlToPlainText(source string) string {
	text, err := html2text.FromString(source, html2text.Options{OmitLinks: false})
	if err != nil {
		return strings.TrimSpace(source)
	}
	return strings.TrimSpace(text)
}

/* ── 小工具 ── */

func fallbackEmailValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func describeProfileChangeField(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return "邮箱地址"
	case "phone":
		return "手机号码"
	default:
		return "资料信息"
	}
}

func maskProfileChangeNotificationValue(field string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未设置"
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return maskEmailForNotification(value)
	case "phone":
		return maskPhoneForNotification(value)
	default:
		return value
	}
}

func maskEmailForNotification(value string) string {
	parts := strings.Split(value, "@")
	if len(parts) != 2 {
		return value
	}
	local := parts[0]
	if len(local) <= 1 {
		return "*@" + parts[1]
	}
	if len(local) == 2 {
		return local[:1] + "*@" + parts[1]
	}
	return local[:1] + strings.Repeat("*", len(local)-2) + local[len(local)-1:] + "@" + parts[1]
}

func maskPhoneForNotification(value string) string {
	if len(value) <= 7 {
		return "***"
	}
	return value[:3] + "****" + value[len(value)-4:]
}
