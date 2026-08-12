package service

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDumpEmailPreviews 把各类邮件渲染到 AEGIS_MAIL_PREVIEW_DIR 指定的目录，
// 供人工在浏览器里核对排版。不设该环境变量时整条跳过，因此不影响 CI。
//
//	AEGIS_MAIL_PREVIEW_DIR=./preview go test ./internal/service -run TestDumpEmailPreviews
func TestDumpEmailPreviews(t *testing.T) {
	dir := os.Getenv("AEGIS_MAIL_PREVIEW_DIR")
	if dir == "" {
		t.Skip("未设置 AEGIS_MAIL_PREVIEW_DIR，跳过预览导出")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建预览目录失败: %v", err)
	}

	write := func(name string, html string, text string) {
		if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(html), 0o644); err != nil {
			t.Fatalf("写入 %s 失败: %v", name, err)
		}
		if text != "" {
			if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte(text), 0o644); err != nil {
				t.Fatalf("写入 %s 纯文本失败: %v", name, err)
			}
		}
	}

	for _, purpose := range []string{"login", "register", "password_reset", "two_factor", "test"} {
		subject, html, text := renderCodeMailContent("远航", "默认 SMTP", purpose, "482913", 5)
		t.Logf("[%s] 主题：%s", purpose, subject)
		write("code_"+purpose, html, text)
	}

	resetHTML, resetText := renderEmail(emailLayout{
		AppName:    "远航",
		Title:      "重置密码",
		Lead:       "我们收到了一次重置这个账号密码的请求。",
		Blocks:     passwordResetBlocks("https://example.com/reset?token=abc123&email=a@b.com", passwordResetTTLMinutes),
		FooterNote: "不是您本人申请的话，忽略这封信即可，密码不会有任何变化。",
	})
	write("password_reset_link", resetHTML, resetText)

	welcomeHTML, welcomeText := renderEmail(emailLayout{
		AppName:    "远航",
		Title:      "账号已开通",
		Lead:       welcomeLead("远航", "张三"),
		Blocks:     []mailBlock{mailDetails(emailDetail{Label: "登录邮箱", Value: "zhangsan@example.com"})},
		FooterNote: "如果您没有注册过 远航，忽略这封信即可。",
	})
	write("welcome", welcomeHTML, welcomeText)

	changeHTML, changeText := renderEmail(emailLayout{
		AppName: "远航",
		Title:   "邮箱地址已变更",
		Lead:    "变更已经生效，下面是这次的改动。",
		Blocks: []mailBlock{
			mailDetails(
				emailDetail{Label: "变更前", Value: maskEmailForNotification("zhangsan@example.com")},
				emailDetail{Label: "变更后", Value: maskEmailForNotification("zhang.san@corp.example.com")},
			),
		},
		FooterNote: "不是您本人改的话，请立刻修改密码，并检查账号的绑定信息和最近登录记录。",
	})
	write("profile_change", changeHTML, changeText)

	noticeHTML, noticeText := renderEmail(emailLayout{
		AppName: "远航",
		Eyebrow: "收据",
		Title:   "您的收据 RCP-20260812001",
		Lead:    "感谢付款，下面是这笔交易的明细。",
		Blocks: []mailBlock{
			mailParagraph("张三，您好。"),
			mailDetails(
				emailDetail{Label: "订单号", Value: "AG20260812000137"},
				emailDetail{Label: "合计", Value: "CNY 128.00"},
				emailDetail{Label: "支付方式", Value: "微信支付"},
			),
			mailButton("下载收据 PDF", "https://example.com/receipt/abc"),
			mailLink("收据", "https://example.com/receipt/abc?sig=deadbeef"),
			mailNotice("链接有效期", "该下载链接 24 小时后失效。"),
		},
		FooterNote: "本收据由系统自动出具。",
	})
	write("receipt", noticeHTML, noticeText)

	t.Logf("预览已写入 %s", dir)
}
