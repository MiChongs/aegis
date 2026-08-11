package service

import (
	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
	"context"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// emailAttachment 一个邮件附件。
type emailAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// emailOutbound 是一封待发信件的传输无关表示。
// 模板渲染在 EmailService 里完成，发送器只负责「怎么把它交出去」。
type emailOutbound struct {
	To      string
	Subject string
	HTML    string
	Text    string
	Purpose string
	// Attachments 附件。发送器若不支持附件，必须**报错而不是丢掉** ——
	// 一封本该带着收据的邮件安静地不带附件发出去，用户和运维都不会发现。
	Attachments []emailAttachment
}

// emailSendResult 是发送器交出信件后的回执。
//
// MessageID 是 RFC 5322 的 Message-ID（SMTP 由本地生成，Zeabur 入队时尚不存在）；
// ProviderMessageID 是服务商侧的邮件 ID，webhook 靠它回填投递状态。
type emailSendResult struct {
	MessageID         string
	ProviderMessageID string
	Status            string
}

// emailSender 是邮件出口的可插拔实现。
// 新增服务商只需实现该接口并注册进 EmailService.senders，业务代码零改动。
type emailSender interface {
	Provider() string
	// Validate 在保存配置时做静态校验，不产生网络请求。
	Validate(config emaildomain.Config) error
	// SupportsAttachments 该通道能否投递附件。
	// 调用方据此决定正文措辞（「附件是收据」还是「点这里下载」），
	// 因此它必须如实反映能力，不能乐观地一律返回 true。
	SupportsAttachments() bool
	Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error)
}

// resolveSender 按配置的 provider 取发送器；未知 provider 直接报错而不是静默回落到 SMTP
// —— 静默回落会让「配了 Zeabur 却在用 SMTP」这种故障以超时的形式出现在几层之外。
func (s *EmailService) resolveSender(provider string) (emailSender, error) {
	normalized := normalizeEmailProvider(provider)
	sender, ok := s.senders[normalized]
	if !ok {
		return nil, apperrors.New(40067, http.StatusBadRequest, "不支持的邮件服务商："+provider)
	}
	return sender, nil
}

func normalizeEmailProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return emaildomain.ProviderSMTP
	}
	return normalized
}

// htmlToPlainText 从 HTML 正文提取纯文本备份。
//
// Zeabur 要求 html / text 至少给一个，但两个都给才是正经做法：
// 纯文本分片能改善反垃圾评分，也照顾纯文本客户端。
func htmlToPlainText(source string) string {
	node, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return strings.TrimSpace(source)
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "head", "title":
				return
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				builder.WriteString(text)
				builder.WriteString(" ")
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "p", "div", "br", "tr", "h1", "h2", "h3", "h4", "li":
				builder.WriteString("\n")
			}
		}
	}
	walk(node)

	lines := make([]string, 0, 16)
	for _, line := range strings.Split(builder.String(), "\n") {
		if trimmed := strings.Join(strings.Fields(line), " "); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}
