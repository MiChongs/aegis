package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/egress"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

// ── 邮件服务商自描述的构造工具 ──
//
// 与支付渠道的 payment_provider_schema.go 同构。各发送器的 Describe() 用这里的
// 构造器声明配置字段，控制台据此动态渲染表单 —— 因此**新增一家邮件服务商时
// 前端不需要改任何一行 TSX**。

func emText(key, label, placeholder, help string, required bool) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldText,
		Placeholder: placeholder, Help: help, Required: required,
		Group: emaildomain.GroupCredential,
	}
}

// emSecret 密钥字段。Secret 一旦为 true，三条语义由服务端统一兑现：
// AES-GCM 加密落库、出网一律抹除、提交留空表示不修改。
func emSecret(key, label, placeholder, help string, required bool) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldSecret, Secret: true,
		Placeholder: placeholder, Help: help, Required: required,
		Group: emaildomain.GroupCredential,
	}
}

func emEmail(key, label, placeholder, help string, required bool) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldEmail,
		Placeholder: placeholder, Help: help, Required: required,
		Group: emaildomain.GroupSender,
	}
}

func emNumber(key, label, placeholder, help string, def any) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldNumber,
		Placeholder: placeholder, Help: help, Default: def,
		Group: emaildomain.GroupCredential,
	}
}

func emSwitch(key, label, help string, def bool) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldSwitch,
		Help: help, Default: def, Group: emaildomain.GroupAdvanced, Advanced: true,
	}
}

func emSelect(key, label, help string, def string, options ...emaildomain.FieldOption) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldSelect,
		Help: help, Default: def, Options: options, Group: emaildomain.GroupCredential,
	}
}

func emURL(key, label, placeholder, help string) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: key, Label: label, Type: emaildomain.FieldURL,
		Placeholder: placeholder, Help: help,
		Group: emaildomain.GroupAdvanced, Advanced: true,
	}
}

func emOption(value, label string) emaildomain.FieldOption {
	return emaildomain.FieldOption{Value: value, Label: label}
}

// emIn 把一批字段归入同一分区。
func emIn(group string, fields ...emaildomain.ConfigField) []emaildomain.ConfigField {
	out := make([]emaildomain.ConfigField, 0, len(fields))
	for _, field := range fields {
		field.Group = group
		if group == emaildomain.GroupAdvanced {
			field.Advanced = true
		}
		out = append(out, field)
	}
	return out
}

// emFields 串接多个分区，保持声明顺序。
func emFields(groups ...[]emaildomain.ConfigField) []emaildomain.ConfigField {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]emaildomain.ConfigField, 0, total)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// senderIdentityFields 发件人身份三件套。
//
// 所有服务商共用同一批键（fromAddress / fromName / replyTo），
// 这样 SenderIdentity()、投递留痕、凭证邮件都不必按服务商分支去取。
// 各家对「发件地址」的叫法不同（阿里云叫 AccountName、腾讯云要求
// "别名 <地址>" 一整串），归一化在各自的发送器里做，不外溢到这里。
func senderIdentityFields(fromHelp string) []emaildomain.ConfigField {
	return []emaildomain.ConfigField{
		emEmail(emaildomain.KeyFromAddress, "发件地址", "noreply@example.com", fromHelp, true),
		{
			Key: emaildomain.KeyFromName, Label: "发件人名称", Type: emaildomain.FieldText,
			Placeholder: "Aegis", Group: emaildomain.GroupSender,
			Help: "显示在收件人邮件列表里的名字，留空则只显示地址",
		},
		{
			Key: emaildomain.KeyReplyTo, Label: "回信地址", Type: emaildomain.FieldEmail,
			Placeholder: "可选", Group: emaildomain.GroupSender,
			Help: "收件人点「回复」时的目标地址，留空即回到发件地址",
		},
	}
}

// webhookSecretField 投递回执的验签密钥。
//
// 提示语里那句「未配置时回调一律拒收」是真的有代码兑现的：
// 无法验签的回执可被任意伪造，而伪造一条 delivered 就能把一封退信的邮件
// 显示成送达 —— 这类留痕的全部价值就是可信。
func webhookSecretField(label, placeholder, help string) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: emaildomain.KeyWebhookSecret, Label: label, Type: emaildomain.FieldSecret, Secret: true,
		Placeholder: placeholder, Group: emaildomain.GroupWebhook,
		Help: help + "；未配置时回调一律拒收 —— 无法验签的回执可被任意伪造",
	}
}

// tagsField 邮件标签。值以 JSON 对象字符串存放（通用 Settings 是 map[string]string，
// 装不下嵌套结构），控制台用键值对行编辑。
func tagsField(help string) emaildomain.ConfigField {
	return emaildomain.ConfigField{
		Key: emaildomain.KeyTags, Label: "邮件标签", Type: emaildomain.FieldKV,
		Group: emaildomain.GroupAdvanced, Advanced: true, Help: help,
	}
}

// ── 目录驱动的通用校验 ──

// validateByCatalog 按服务商自述的字段声明做通用校验。
//
// 校验的依据是目录而不是各发送器里的一串 if：目录里加一个 Required 字段，
// 校验自动跟上。各发送器的 Validate 只需在此之上补自己独有的规则
// （地域取值、端点协议这类目录表达不了的约束）。
func validateByCatalog(meta emaildomain.ProviderMeta, config emaildomain.Config) error {
	for _, field := range meta.Fields {
		value := config.Setting(field.Key)
		if field.Secret {
			// 密钥的「已配置」包含密文：编辑配置时前端不会回传原值（留空即不修改），
			// 只看明文会把一条配好的通道判成没配。
			if field.Required && !config.HasSecret(field.Key) {
				return emailFieldError(field, "不能为空")
			}
			continue
		}
		if field.Required && value == "" {
			return emailFieldError(field, "不能为空")
		}
		if value == "" {
			continue
		}
		switch field.Type {
		case emaildomain.FieldEmail:
			if _, err := mail.ParseAddress(value); err != nil {
				return emailFieldError(field, "不是合法的邮箱地址")
			}
		case emaildomain.FieldURL:
			parsed, err := url.Parse(value)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return emailFieldError(field, "不是合法的地址，需要带 http(s):// 前缀")
			}
			if parsed.Scheme != "http" && parsed.Scheme != "https" {
				return emailFieldError(field, "只接受 http/https 地址")
			}
		case emaildomain.FieldNumber:
			if _, err := strconv.Atoi(value); err != nil {
				return emailFieldError(field, "必须是数字")
			}
		case emaildomain.FieldSelect:
			if len(field.Options) == 0 {
				continue
			}
			matched := false
			for _, option := range field.Options {
				if option.Value == value {
					matched = true
					break
				}
			}
			if !matched {
				return emailFieldError(field, "取值不在可选范围内")
			}
		}
	}
	return nil
}

// emailFieldError 统一的字段校验错误。
//
// 带上服务商名与字段中文名：同一个 40066「发件人邮箱格式错误」在九家服务商
// 之间共用时，管理员看不出到底是哪一家的哪一个框填错了。
func emailFieldError(field emaildomain.ConfigField, reason string) error {
	return apperrors.New(40066, http.StatusBadRequest, "「"+field.Label+"」"+reason)
}

// formatMailAddress 组装 RFC 5322 的 "Name <addr>" 形式。
func formatMailAddress(address string, name string) string {
	address = strings.TrimSpace(address)
	name = strings.TrimSpace(name)
	if address == "" {
		return ""
	}
	if name == "" {
		return address
	}
	return (&mail.Address{Name: name, Address: address}).String()
}

// buildProviderTags 把业务用途并进服务商标签，便于在对方控制台按用途筛选投递情况。
func buildProviderTags(configured map[string]string, purpose string) map[string]string {
	tags := make(map[string]string, len(configured)+1)
	for key, value := range configured {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
	}
	if purpose = strings.TrimSpace(purpose); purpose != "" {
		if _, exists := tags["purpose"]; !exists {
			tags["purpose"] = purpose
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// truncateSubject 主题超长时截断。
// RFC 5322 的单行上限是 998 字节，各服务商也普遍按这个数拒收。
func truncateSubject(subject string) string {
	const limit = 998
	if len([]rune(subject)) <= limit {
		return subject
	}
	return string([]rune(subject)[:limit])
}

// clampDetail 把上游返回的报错裁到可展示的长度。
// 不裁的话，一段几十 KB 的 HTML 错误页会原样进审计日志与控制台的提示条。
func clampDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len([]rune(detail)) > 300 {
		return string([]rune(detail)[:300])
	}
	return detail
}

// withDetail 把上游细节接在人话后面。
func withDetail(message string, detail string) string {
	if detail = clampDetail(detail); detail == "" {
		return message
	}
	return message + "：" + detail
}

// emailMaxBodyRead 读上游响应体的上限。
// 不设上限的话，一个异常网关返回的几百 MB 报文会把内存打满。
const emailMaxBodyRead = 1 << 20

// newEmailHTTPClient 邮件服务商统一的出网客户端。
//
// 一律经过出海代理网关（pkg/egress）：这些服务商的端点都在境外，
// 与 OAuth / 对象存储 / GeoIP 共用同一张域名路由表。绕开它的后果是
// 「开了代理，别的都通、唯独这家超时」，而那种差异极难联想到出网路由上去。
func newEmailHTTPClient(name string, timeoutSeconds int) *http.Client {
	return egress.NewClient(egress.Profile{Name: name, Timeout: timeutil.Seconds(timeoutSeconds)})
}

// readLimitedBody 读响应体，出错时返回已读到的部分。
// 报错路径上「读了一半」的报文仍然有诊断价值，因此不区分 err。
func readLimitedBody(body io.Reader) []byte {
	raw, _ := io.ReadAll(io.LimitReader(body, emailMaxBodyRead))
	return raw
}
