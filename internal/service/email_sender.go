package service

import (
	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
	"bytes"
	"context"
	"net/http"
	"sort"
	"strings"

	mailpkg "github.com/wneessen/go-mail"
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
// MessageID 是 RFC 5322 的 Message-ID（SMTP 由本地生成，REST 类服务商入队时尚不存在）；
// ProviderMessageID 是服务商侧的邮件 ID，webhook 靠它回填投递状态。
type emailSendResult struct {
	MessageID         string
	ProviderMessageID string
	Status            string
}

// emailSender 是邮件出口的可插拔实现。
//
// 新增服务商只需实现该接口并注册进 EmailService.senders：
// 业务代码零改动，控制台的服务商卡片与配置表单由 Describe() 自动产出。
type emailSender interface {
	Provider() string
	// Describe 自述：展示信息、能力矩阵、配置字段 schema。
	// 它同时是服务端校验的依据与控制台表单的数据源，因此两边不可能漂移。
	Describe() emaildomain.ProviderMeta
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
		return nil, apperrors.New(40067, http.StatusBadRequest,
			"不支持的邮件服务商："+provider+"，可选："+strings.Join(s.providerKeys(), " / "))
	}
	return sender, nil
}

// providerKeys 已注册服务商的标识，按目录顺序。
func (s *EmailService) providerKeys() []string {
	keys := make([]string, 0, len(s.senders))
	for key := range s.senders {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return emailProviderOrder(keys[i]) < emailProviderOrder(keys[j])
	})
	return keys
}

// emailProviderOrder 固定服务商的展示顺序。
//
// 不能直接遍历 map —— 迭代顺序随机会让控制台的服务商列表每次刷新都跳动，
// 与支付渠道的 methodOrder 同一条约束。顺序按「最可能被选中」排：
// 直连与部署平台在前，国际服务商次之，国内云厂商最后。
func emailProviderOrder(provider string) int {
	for index, key := range emailProviderDisplayOrder {
		if key == provider {
			return index
		}
	}
	return len(emailProviderDisplayOrder)
}

var emailProviderDisplayOrder = []string{
	emaildomain.ProviderSMTP,
	emaildomain.ProviderZeabur,
	emaildomain.ProviderSES,
	emaildomain.ProviderResend,
	emaildomain.ProviderSendGrid,
	emaildomain.ProviderMailgun,
	emaildomain.ProviderPostmark,
	emaildomain.ProviderAliyun,
	emaildomain.ProviderTencent,
}

// ProviderCatalog 返回全部服务商的自述，供控制台渲染服务商市场与配置表单。
func (s *EmailService) ProviderCatalog() []emaildomain.ProviderMeta {
	metas := make([]emaildomain.ProviderMeta, 0, len(s.senders))
	for _, key := range s.providerKeys() {
		meta := s.senders[key].Describe()
		if meta.CategoryName == "" {
			meta.CategoryName = emaildomain.CategoryNames[meta.Category]
		}
		metas = append(metas, meta)
	}
	return metas
}

// providerMeta 取某个服务商的自述；未注册时返回零值。
func (s *EmailService) providerMeta(provider string) emaildomain.ProviderMeta {
	sender, ok := s.senders[normalizeEmailProvider(provider)]
	if !ok {
		return emaildomain.ProviderMeta{}
	}
	return sender.Describe()
}

func normalizeEmailProvider(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return emaildomain.ProviderSMTP
	}
	return normalized
}

// buildRawMIME 用 go-mail 组装一封完整的 RFC 5322 报文。
//
// 给「只收原始 MIME」的服务商用（目前是 AWS SES 的 Raw 内容）。
// 自己拼 multipart 边界与 quoted-printable 编码是一件很容易在中文主题、
// 长行折叠、附件文件名这三处各错一次的事，而错了的表现是**部分**邮件客户端
// 显示乱码 —— 交给已经把这些坑踩完的库更划算。
func buildRawMIME(fromAddress string, fromName string, replyTo string, out emailOutbound) ([]byte, string, error) {
	msg := mailpkg.NewMsg()
	var err error
	if strings.TrimSpace(fromName) != "" {
		err = msg.FromFormat(fromName, fromAddress)
	} else {
		err = msg.From(fromAddress)
	}
	if err == nil {
		err = msg.To(out.To)
	}
	if err == nil && strings.TrimSpace(replyTo) != "" {
		err = msg.ReplyTo(replyTo)
	}
	if err != nil {
		return nil, "", apperrors.New(40062, http.StatusBadRequest, "邮件地址配置错误："+err.Error())
	}
	msg.Subject(truncateSubject(out.Subject))
	msg.SetBodyString(mailpkg.TypeTextHTML, out.HTML)
	if text := strings.TrimSpace(out.Text); text != "" {
		msg.AddAlternativeString(mailpkg.TypeTextPlain, text)
	}
	if err := attachToMessage(msg, out.Attachments); err != nil {
		return nil, "", err
	}
	msg.SetMessageID()

	var buffer bytes.Buffer
	if _, err := msg.WriteTo(&buffer); err != nil {
		return nil, "", apperrors.New(50061, http.StatusInternalServerError, "邮件报文构造失败："+err.Error())
	}
	return buffer.Bytes(), msg.GetMessageID(), nil
}

// attachToMessage 把附件挂到 go-mail 报文上。
//
// 挂不上就整封信都不发：一封「您的收据见附件」却没有附件的邮件
// 会让用户以为收据丢了，比发信失败更难排查。
func attachToMessage(msg *mailpkg.Msg, attachments []emailAttachment) error {
	for _, attachment := range attachments {
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if err := msg.AttachReader(attachment.Filename, bytes.NewReader(attachment.Content),
			mailpkg.WithFileContentType(mailpkg.ContentType(contentType))); err != nil {
			return apperrors.New(50061, http.StatusInternalServerError, "邮件附件构造失败："+attachment.Filename)
		}
	}
	return nil
}
