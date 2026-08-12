package service

import (
	"strings"
	"testing"
)

// 这一组用例钉住邮件模板里**改坏了也不会有人立刻发现**的几件事：
// 排版在 Outlook 里散架、预览行漏进纯文本、文案退回套话、品牌名没转义。
// 邮件发出去就收不回来，只能靠测试在发之前挡住。

// 每个用途都要过一遍：只测一种会漏掉复制粘贴带来的退化。
var allEmailPurposes = []string{
	"register", "login", "admin_login",
	"bind_email", "change_email", "profile_email_change",
	"bind_phone", "change_phone", "profile_phone_change",
	"password_reset", "verify_identity", "two_factor",
	"custom", "",
}

func TestCodeMailSubjectLeadsWithCode(t *testing.T) {
	// 手机通知栏和邮件列表只显示开头十几个字，验证码放在那里往往不用点开邮件
	for _, purpose := range allEmailPurposes {
		subject, _, _ := renderCodeMailContent("远航", "默认配置", purpose, "482913", 5)
		if !strings.HasPrefix(subject, "482913 ") {
			t.Errorf("用途 %q 的主题应以验证码开头，实际：%q", purpose, subject)
		}
	}
}

func TestCodeMailCarriesCodeAndExpiry(t *testing.T) {
	_, html, text := renderCodeMailContent("远航", "默认配置", "login", "482913", 7)
	for _, want := range []string{"482913", "7 分钟内有效"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML 正文缺少 %q", want)
		}
		if !strings.Contains(text, want) {
			t.Errorf("纯文本正文缺少 %q", want)
		}
	}
}

// 预览行只该出现在收件箱摘要里。它带着一串撑开摘要用的零宽字符，
// 混进纯文本版就是开头一行乱码。
func TestPlainTextHasNoPreheaderPadding(t *testing.T) {
	_, _, text := renderCodeMailContent("远航", "默认配置", "login", "482913", 5)
	for _, junk := range []string{"&#847;", "&zwnj;", "&nbsp;", "<"} {
		if strings.Contains(text, junk) {
			t.Errorf("纯文本不应包含 %q，实际：\n%s", junk, text)
		}
	}
}

// Outlook 2007–2021 用 Word 排版引擎：flex 会让字段表的标签与值竖排。
// 这条规则最容易在「顺手改个样式」时被破坏，且只有 Outlook 用户看得见。
func TestMailHTMLAvoidsUnsupportedLayout(t *testing.T) {
	_, html, _ := renderCodeMailContent("远航", "默认配置", "login", "482913", 5)
	resetHTML, _ := renderEmail(emailLayout{
		AppName: "远航",
		Title:   "重置密码",
		Blocks:  passwordResetBlocks("https://example.com/reset?token=abc", 30),
	})
	for name, doc := range map[string]string{"验证码邮件": html, "重置邮件": resetHTML} {
		for _, banned := range []string{"display:flex", "display: flex", "grid-template"} {
			if strings.Contains(doc, banned) {
				t.Errorf("%s 用到了 Outlook 不支持的 %q", name, banned)
			}
		}
	}
}

// premailer 必须真的把样式表内联进标签，否则 Gmail 会把 <style> 整块丢掉。
// 同时媒体查询要原样留下 —— 它内联不了，只能留在 <style> 里。
func TestMailStylesAreInlinedButMediaQueriesSurvive(t *testing.T) {
	_, html, _ := renderCodeMailContent("远航", "默认配置", "login", "482913", 5)
	if !strings.Contains(html, `class="m-code"`) || !strings.Contains(html, "letter-spacing:10px") {
		t.Error("验证码样式没有被内联到标签上")
	}
	if !strings.Contains(html, "prefers-color-scheme") {
		t.Error("深色模式媒体查询丢失")
	}
	if !strings.Contains(html, "max-width: 620px") && !strings.Contains(html, "max-width:620px") {
		t.Error("窄屏媒体查询丢失")
	}
}

// Outlook 的圆角按钮靠 VML 条件注释画。html/template 会删掉模板源码里的注释，
// 所以它必须由 Go 侧注入 —— 这条断言就是防止有人把注入改回模板里。
func TestButtonKeepsOutlookVML(t *testing.T) {
	html, _ := renderEmail(emailLayout{
		AppName: "远航",
		Title:   "重置密码",
		Blocks:  []mailBlock{mailButton("设置新密码", "https://example.com/reset")},
	})
	for _, want := range []string{"[if mso]", "v:roundrect", "[if !mso]", "<![endif]-->"} {
		if !strings.Contains(html, want) {
			t.Errorf("按钮缺少 Outlook 兼容片段 %q", want)
		}
	}
}

// 模板全部走 html/template 的自动转义，业务代码里不该再有手工转义。
// 应用名是用户可改的字段，直接进标题。
func TestMailEscapesUntrustedText(t *testing.T) {
	_, html, _ := renderCodeMailContent(`<script>alert(1)</script>`, "默认配置", "login", "482913", 5)
	if strings.Contains(html, "<script>") {
		t.Error("应用名未转义，HTML 里出现了裸 <script>")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("应用名应被转义成实体后照常显示")
	}
}

// 文案退化是这次重写要解决的核心问题：旧版 12 个场景共用同一句
// 「本次验证码用于 XX，请在有效期内完成验证」+「请勿将验证码泄露给任何人」，
// 于是每封信都在说同一件事，真正要看的那行反而被淹没。
func TestPurposeCopyStaysSpecific(t *testing.T) {
	bannedPhrases := []string{
		"本次验证码用于",
		"请在有效期内",
		"系统工作人员",
		"感谢您的使用",
		"祝您使用愉快",
	}

	// 别名（change_email / profile_email_change、custom / 未知值）走同一个分支，
	// 共用文案是对的。因此先按整份文案归并，再看「不同的场景有没有给出不同的话」。
	byPresentation := map[emailPurposePresentation][]string{}
	for _, purpose := range allEmailPurposes {
		p := getEmailPurposePresentation(purpose)

		if strings.TrimSpace(p.Noun) == "" || strings.TrimSpace(p.Title) == "" ||
			strings.TrimSpace(p.Lead) == "" || strings.TrimSpace(p.Footer) == "" {
			t.Errorf("用途 %q 的文案有空字段：%+v", purpose, p)
			continue
		}

		for _, banned := range bannedPhrases {
			for label, text := range map[string]string{"Lead": p.Lead, "Footer": p.Footer} {
				if strings.Contains(text, banned) {
					t.Errorf("用途 %q 的 %s 出现套话 %q：%s", purpose, label, banned, text)
				}
			}
		}
		byPresentation[p] = append(byPresentation[p], purpose)
	}

	// Lead 要回答「输入这个码之后会发生什么」，各场景各不相同；
	// 两个不同场景给出同一句话，说明有人复制粘贴时没想清楚差别在哪。
	// （Footer 允许重复：绑定邮箱与绑定手机的处置建议本来就一样。）
	generic := getEmailPurposePresentation("")
	leadOwner := map[string][]string{}
	for p, purposes := range byPresentation {
		if owner, dup := leadOwner[p.Lead]; dup {
			t.Errorf("场景 %v 与 %v 的引导句完全相同：%q", purposes, owner, p.Lead)
		}
		leadOwner[p.Lead] = purposes

		// 引导句不该只是把标题再念一遍。
		// 兜底那档除外：它的标题就是「验证码」这三个字本身，正文里避不开。
		if p != generic && strings.Contains(p.Lead, p.Title) {
			t.Errorf("场景 %v 的引导句复述了标题：%q / %q", purposes, p.Title, p.Lead)
		}
	}
}

// 通道自检信里那串数字不写 Redis、验不了任何东西。
// 叫它「验证码」会让收到的人以为账号上正在发生什么。
func TestChannelTestMailIsNotPresentedAsVerificationCode(t *testing.T) {
	subject, html, text := renderCodeMailContent("远航", "阿里云 SMTP", "test", "482913", 5)
	if strings.Contains(subject, "验证码") {
		t.Errorf("自检邮件主题不应出现「验证码」：%q", subject)
	}
	if !strings.Contains(html, "校验串") || !strings.Contains(text, "校验串") {
		t.Error("自检邮件应把这串数字称作校验串")
	}
	if !strings.Contains(html, "阿里云 SMTP") {
		t.Error("自检邮件应写明用的是哪条通道")
	}
}

// 有效期这类数字一旦在文案里写死，改配置时必然漏掉一处。
func TestPasswordResetCopyFollowsConfiguredTTL(t *testing.T) {
	_, text := renderEmail(emailLayout{
		AppName: "远航",
		Title:   "重置密码",
		Blocks:  passwordResetBlocks("https://example.com/reset?token=abc", passwordResetTTLMinutes),
	})
	if !strings.Contains(text, "30 分钟后失效") {
		t.Errorf("重置邮件应写明配置的有效期，实际：\n%s", text)
	}
}

// 没配 resetBaseURL 时不能发一封「点按钮重置」但没有按钮的信。
func TestPasswordResetWithoutURLExplainsWhy(t *testing.T) {
	_, text := renderEmail(emailLayout{
		AppName: "远航",
		Title:   "重置密码",
		Blocks:  passwordResetBlocks("", passwordResetTTLMinutes),
	})
	if !strings.Contains(text, "resetBaseURL") {
		t.Errorf("缺少重置地址时应指明缺的是哪一项配置，实际：\n%s", text)
	}
}

func TestMailDetailsSkipsEmptyRows(t *testing.T) {
	block := mailDetails(
		emailDetail{Label: "订单号", Value: "AG001"},
		emailDetail{Label: "", Value: ""},
		emailDetail{Label: "流水号", Value: ""},
	)
	// 空标签空值整行丢弃；有标签无值的行保留（表示「该项为空」本身是信息）
	if len(block.Details) != 2 {
		t.Fatalf("期望保留 2 行，实际 %d 行：%+v", len(block.Details), block.Details)
	}
	if block.LastIndex() != 1 {
		t.Errorf("LastIndex 应为 1，实际 %d", block.LastIndex())
	}
}
