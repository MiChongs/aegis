package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/i18n"
	"aegis/pkg/receipt"

	"go.uber.org/zap"
)

// 凭证邮件投递。
//
// 一封凭证邮件由三部分构成，缺一不可：
//   - **PDF 本体**。渠道支持附件就直接附上（SMTP）。
//   - **签名下载链接**。恒附带，即使已经带了附件 —— 邮件会被转发、
//     附件会被网关剥离、手机客户端未必肯下载附件。链接是最后一条退路。
//   - **与 PDF 同语言的正文**。一封中文邮件配一份英文 PDF 是最难向用户解释的错位，
//     因此邮件文案与 PDF 共用 pkg/receipt 的同一份译文与同一次语言协商结果。

// receiptEmailPurpose 投递留痕里的用途标识，同时也是补发频次限制的统计维度。
const receiptEmailPurpose = "payment_receipt"

// receiptLinkSignaturePurpose 签名密钥的用途盐，与平台其它派生密钥同构。
const receiptLinkSignaturePurpose = "aegis.payment-receipt.link\x00"

// ReceiptEmailResult 一次凭证邮件投递的结果。
type ReceiptEmailResult struct {
	// To 实际收件地址
	To string `json:"to"`
	// Locale 凭证与邮件正文的语言
	Locale string `json:"locale"`
	// DocumentType receipt / invoice / credit_note
	DocumentType string `json:"documentType"`
	// Attached PDF 是否作为附件发出；false 表示渠道不支持附件，只发了下载链接
	Attached bool `json:"attached"`
	// DownloadURL 邮件里的签名下载地址（无需登录即可下载，凭签名与随机标识授权）
	DownloadURL string `json:"downloadUrl,omitempty"`
	// LinkExpiresAt 下载链接的失效时间
	LinkExpiresAt time.Time `json:"linkExpiresAt"`
	// MessageID 邮件出口回执
	MessageID string `json:"messageId,omitempty"`
	// LocaleFallback 是否因缺字体把语言降级成了默认语言
	LocaleFallback bool `json:"localeFallback,omitempty"`
}

// ── 对外入口 ──

// EmailUserOrderReceipt 用户自助把凭证寄到自己账号绑定的邮箱。
//
// **收件地址不接受调用方指定**：允许任意填写等于把平台变成一个能带 PDF 附件的
// 转发器，攻击者注册一个账号就能以你的域名向任意邮箱发信。要寄给别人，
// 用户自己转发即可。
func (s *PaymentService) EmailUserOrderReceipt(ctx context.Context, session *authdomain.Session, orderNo string, opts paymentdomain.ReceiptOptions) (*ReceiptEmailResult, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	order, err := s.GetUserOrder(ctx, session, orderNo)
	if err != nil {
		return nil, err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	to := ""
	if profile != nil {
		to = strings.TrimSpace(profile.Email)
	}
	if to == "" {
		return nil, apperrors.New(40097, http.StatusBadRequest, "账号尚未绑定邮箱，请先在个人资料中绑定后再补发凭证")
	}
	if err := s.ensureReceiptEmailQuota(ctx, session.AppID, to); err != nil {
		return nil, err
	}
	return s.deliverReceiptEmail(ctx, order, user, profile, to, opts, s.loadUserUISettings(ctx, session.UserID))
}

// EmailAppOrderReceipt 管理端把凭证寄到指定邮箱（客服代发 / 补发到财务邮箱）。
// 收件地址由操作者指定，因此这条路径**必须走管理端鉴权与审计中间件**。
func (s *PaymentService) EmailAppOrderReceipt(ctx context.Context, appID int64, orderNo string, to string, opts paymentdomain.ReceiptOptions) (*ReceiptEmailResult, error) {
	to = strings.TrimSpace(to)
	if !isEmailAddress(to) {
		return nil, apperrors.New(40098, http.StatusBadRequest, "收件邮箱格式错误")
	}
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, err
	}
	if order == nil || order.AppID != appID {
		return nil, apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	user, profile, err := s.loadOrderCustomer(ctx, order)
	if err != nil {
		return nil, err
	}
	return s.deliverReceiptEmail(ctx, order, user, profile, to, opts, nil)
}

// autoEmailReceipt 支付成功后自动把凭证寄给下单用户。
//
// 只在**首次**确认支付时调用（回调可能重复到达），且全程不反噬支付链路：
// 钱已经收到、履约也已完成，此时因为发信失败让回调返回错误，
// 会让上游按重试策略反复重放一笔已完成的支付。
func (s *PaymentService) autoEmailReceipt(appID int64, orderNo string) {
	if s.email == nil || s.apps == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	app, err := s.apps.GetApp(ctx, appID)
	if err != nil || app == nil {
		return
	}
	// 先看开关再取单：绝大多数应用没开这个开关，不该为此多打一次库
	settings := s.apps.ResolveCommerceSettings(app)
	if !settings.ReceiptEmailOnPaid {
		return
	}
	// 重新取一次订单：调用方手里那份是状态翻转**之前**的快照，
	// 拿它去开凭证会印出「待支付」。
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, orderNo)
	if err != nil || order == nil || order.AppID != appID || order.UserID == nil {
		return
	}
	user, profile, err := s.loadOrderCustomer(ctx, order)
	if err != nil {
		s.log.Warn("auto receipt email: load customer failed", zap.String("orderNo", order.OrderNo), zap.Error(err))
		return
	}
	to := ""
	if profile != nil {
		to = strings.TrimSpace(profile.Email)
	}
	if to == "" {
		// 没绑邮箱不是错误，只是这条链路无处可送
		s.log.Info("auto receipt email skipped: customer has no email", zap.String("orderNo", order.OrderNo))
		return
	}
	var uiSettings map[string]any
	if order.UserID != nil {
		uiSettings = s.loadUserUISettings(ctx, *order.UserID)
	}
	result, err := s.deliverReceiptEmail(ctx, order, user, profile, to,
		paymentdomain.ReceiptOptions{Locale: settings.ReceiptLocale}, uiSettings)
	if err != nil {
		s.log.Warn("auto receipt email failed",
			zap.String("orderNo", order.OrderNo), zap.String("to", maskEmail(to)), zap.Error(err))
		return
	}
	s.log.Info("auto receipt email sent",
		zap.String("orderNo", order.OrderNo), zap.String("to", maskEmail(to)),
		zap.String("locale", result.Locale), zap.Bool("attached", result.Attached))
}

// ── 投递主链路 ──

func (s *PaymentService) deliverReceiptEmail(
	ctx context.Context,
	order *paymentdomain.Order,
	user *userdomain.User,
	profile *userdomain.Profile,
	to string,
	opts paymentdomain.ReceiptOptions,
	uiSettings map[string]any,
) (*ReceiptEmailResult, error) {
	if s.email == nil {
		return nil, apperrors.New(50374, http.StatusServiceUnavailable, "邮件服务不可用，无法投递凭证")
	}
	doc, rendered, err := s.renderReceipt(ctx, order, user, profile, opts, uiSettings)
	if err != nil {
		return nil, err
	}
	return s.sendReceiptDocumentEmail(ctx, newOrderEmailSubject(order), doc, rendered, profile, to, opts, uiSettings)
}

// newOrderEmailSubject 订单 → 邮件主体事实。
func newOrderEmailSubject(order *paymentdomain.Order) receiptEmailSubject {
	subject := receiptEmailSubject{
		AppID:         order.AppID,
		OrderNo:       order.OrderNo,
		PaidAt:        order.PaidAt,
		PaymentMethod: order.PaymentMethod,
	}
	if order.UserID != nil {
		subject.UserID = *order.UserID
	}
	return subject
}

// receiptEmailSubject 一封凭证邮件里与**凭证主体**有关的最小事实。
//
// 抽出来是因为投递链路（能力探测 → 落盘 → 签名链接 → 同语言正文 → 附件）
// 与主体是订单还是钱包流水完全无关。不抽的话，钱包凭证要么复制一整条
// 六十行的链路，要么被迫伪造一个 Order —— 两条路都会在下一次改动时分叉。
type receiptEmailSubject struct {
	AppID         int64
	UserID        int64
	OrderNo       string
	TransactionNo string
	PaidAt        *time.Time
	PaymentMethod string
}

// sendReceiptDocumentEmail 投递一份已经渲染好的凭证。订单与钱包流水共用。
func (s *PaymentService) sendReceiptDocumentEmail(
	ctx context.Context,
	subject receiptEmailSubject,
	doc receipt.Document,
	rendered *receipt.Result,
	profile *userdomain.Profile,
	to string,
	opts paymentdomain.ReceiptOptions,
	uiSettings map[string]any,
) (*ReceiptEmailResult, error) {
	// 先问渠道能不能带附件，再决定正文怎么写。
	// 顺序反过来的话，正文已经写着「收据见附件」了才发现带不了。
	capability, err := s.email.ResolveChannelCapability(ctx, subject.AppID, "")
	if err != nil {
		return nil, err
	}

	// 无论能否附件都落一份带签名链接的导出：附件会被邮件网关剥离，
	// 邮件也会被转发到打不开附件的客户端上。
	export, err := s.persistEmailReceipt(subject, doc, rendered)
	if err != nil {
		return nil, err
	}
	downloadURL := s.signedReceiptURL(subject.AppID, export.BillID, export.ExpiresAt)

	if !capability.Attachments && downloadURL == "" {
		// 既带不了附件又拼不出链接（未配置 API_BASE_URL），这封信发出去也是空的
		return nil, apperrors.New(50375, http.StatusServiceUnavailable,
			"当前邮件通道不支持附件，且未配置 API_BASE_URL，无法生成凭证下载链接")
	}

	loc := s.receipts.Localizer(rendered.Locale)
	mailSubject, body := s.buildReceiptEmail(loc, receiptEmailView{
		Brand:       s.platformBrand(ctx),
		Doc:         doc,
		Subject:     subject,
		Customer:    profile,
		Attached:    capability.Attachments,
		DownloadURL: downloadURL,
		LinkExpiry:  export.ExpiresAt,
		Timezone:    resolveReceiptTimezone(opts.Timezone, uiSettings),
	})

	message := newReceiptEmailMessage(to, mailSubject, body)
	if capability.Attachments {
		message.Files = []DocumentAttachment{{
			Filename:    export.FileName,
			ContentType: "application/pdf",
			Content:     rendered.PDF,
		}}
	}
	messageID, err := s.email.SendDocumentEmail(ctx, subject.AppID, message)
	if err != nil {
		return nil, err
	}
	return &ReceiptEmailResult{
		To:             to,
		Locale:         rendered.Locale,
		DocumentType:   string(doc.Type),
		Attached:       capability.Attachments,
		DownloadURL:    downloadURL,
		LinkExpiresAt:  export.ExpiresAt,
		MessageID:      messageID,
		LocaleFallback: rendered.LocaleFallback,
	}, nil
}

// isEmailAddress 收件地址是否可解析。管理端代发的两条路径共用同一判定 ——
// 两处各写一遍，迟早出现「订单凭证能寄、钱包凭证说格式错」这种解释不清的差异。
func isEmailAddress(value string) bool {
	_, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil
}

// newReceiptEmailMessage 构造一封凭证邮件的骨架。
func newReceiptEmailMessage(to, subject, body string) DocumentEmail {
	return DocumentEmail{To: to, Subject: subject, HTML: body, Purpose: receiptEmailPurpose}
}

// persistEmailReceipt 为邮件落一份导出，有效期用邮件专用的 TTL。
//
// 单独一条路径而不是复用 CreateUserOrderReceipt：那条是「用户刚点了下载」，
// 半小时有效期足够；邮件里的链接可能几天后才被点开，用同一个 TTL 等于没发。
func (s *PaymentService) persistEmailReceipt(subject receiptEmailSubject, doc receipt.Document, rendered *receipt.Result) (paymentdomain.ReceiptExport, error) {
	ttl := s.receiptCfg.EmailLinkTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	now := time.Now().UTC()
	export := paymentdomain.ReceiptExport{
		BillID:          randomReceiptID(16),
		OrderNo:         doc.OrderNo,
		TransactionNo:   subject.TransactionNo,
		FileName:        receiptFileName(doc.Type, receiptSubjectNo(doc), rendered.Locale),
		DocumentType:    string(doc.Type),
		Locale:          rendered.Locale,
		RequestedLocale: rendered.RequestedLocale,
		LocaleFallback:  rendered.LocaleFallback,
		DegradedGlyphs:  string(rendered.MissingGlyphs),
		Currency:        doc.Currency,
		Pages:           rendered.Pages,
		Size:            len(rendered.PDF),
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
	}
	if err := s.persistReceipt(subject.AppID, subject.UserID, export, rendered.PDF); err != nil {
		return paymentdomain.ReceiptExport{}, err
	}
	return export, nil
}

// ensureReceiptEmailQuota 限制同一收件地址的补发频次。
//
// 统计口径直接取投递留痕表，不另建计数：多存一份计数只会多一处可能和事实对不上的数字。
// 失败的投递同样计入 —— 反复触发失败发信一样在消耗上游配额与信誉分。
func (s *PaymentService) ensureReceiptEmailQuota(ctx context.Context, appID int64, to string) error {
	limit := s.receiptCfg.EmailPerDay
	if limit <= 0 {
		return nil
	}
	used, err := s.pg.CountEmailDeliveriesSince(ctx, appID, receiptEmailPurpose, to, time.Now().Add(-24*time.Hour))
	if err != nil {
		// 限流读失败时放行：为了一个防滥用计数把正常的凭证补发挡掉不划算，
		// 与平台其它 fail-open 的判定取向一致。
		s.log.Warn("receipt email quota check failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	if used >= int64(limit) {
		return apperrors.New(42901, http.StatusTooManyRequests,
			fmt.Sprintf("凭证邮件发送过于频繁，同一邮箱每天最多 %d 封，请稍后再试", limit))
	}
	return nil
}

func (s *PaymentService) loadOrderCustomer(ctx context.Context, order *paymentdomain.Order) (*userdomain.User, *userdomain.Profile, error) {
	if order.UserID == nil {
		return nil, nil, nil
	}
	user, err := s.pg.GetUserByID(ctx, *order.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, nil
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, *order.UserID)
	if err != nil {
		return nil, nil, err
	}
	return user, profile, nil
}

// ── 签名下载链接 ──

// signedReceiptURL 拼出无需登录即可下载的绝对地址。
// 未配置 API_BASE_URL 时返回空串：邮件里放相对路径点不开，不如不放。
func (s *PaymentService) signedReceiptURL(appID int64, billID string, expiresAt time.Time) string {
	base := strings.TrimRight(strings.TrimSpace(s.receiptCfg.PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	expires := expiresAt.UTC().Unix()
	return fmt.Sprintf("%s/api/pay/receipts/%d/%s/download?expires=%d&token=%s",
		base, appID, billID, expires, s.signReceiptLink(appID, billID, expires))
}

// signReceiptLink 对 (应用, 凭证标识, 失效时刻) 三元组签名。
//
// 三者都进签名，缺一不可：不签应用就能拿别的应用的凭证标识去猜路径，
// 不签失效时刻就能把过期时间改成十年后。
func (s *PaymentService) signReceiptLink(appID int64, billID string, expires int64) string {
	key := sha256.Sum256([]byte(receiptLinkSignaturePurpose + s.receiptCfg.SigningKey))
	mac := hmac.New(sha256.New, key[:])
	fmt.Fprintf(mac, "%d\n%s\n%d", appID, billID, expires)
	return hex.EncodeToString(mac.Sum(nil))
}

// DownloadSignedReceipt 凭签名链接取回凭证，**不需要登录会话**。
//
// 授权来自两处叠加：128 位随机的凭证标识（猜不到）+ 服务端签名（改不动）。
// 这与密码重置链接是同一套模型：拿到链接的人即被视为获授权，
// 因此链接的有效期必须有限，且不能出现在任何日志里。
func (s *PaymentService) DownloadSignedReceipt(ctx context.Context, appID int64, billID string, expires int64, token string) ([]byte, string, error) {
	_ = ctx
	if strings.TrimSpace(s.receiptCfg.SigningKey) == "" {
		return nil, "", apperrors.New(50376, http.StatusServiceUnavailable, "凭证签名密钥未配置")
	}
	expected := s.signReceiptLink(appID, billID, expires)
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(token))) {
		// 与「不存在」同一个响应：区分「签名错」和「单号不存在」等于告诉爆破方哪一半猜对了
		return nil, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	if time.Now().UTC().Unix() > expires {
		return nil, "", apperrors.New(41075, http.StatusGone, "账单下载链接已过期")
	}
	meta, metaPath, err := s.loadReceiptMeta(appID, billID)
	if err != nil {
		return nil, "", err
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		s.deleteReceipt(meta, metaPath)
		return nil, "", apperrors.New(41075, http.StatusGone, "账单文件已过期")
	}
	data, _, err := readReceiptFile(meta.FilePath)
	if err != nil {
		s.log.Warn("read signed receipt failed", zap.String("bill_id", billID), zap.Error(err))
		return nil, "", err
	}
	return data, meta.FileName, nil
}

// ── 邮件正文 ──

// receiptEmailView 渲染邮件正文所需的全部素材。
type receiptEmailView struct {
	Brand       string
	Doc         receipt.Document
	Subject     receiptEmailSubject
	Customer    *userdomain.Profile
	Attached    bool
	DownloadURL string
	LinkExpiry  time.Time
	Timezone    *time.Location
}

// buildReceiptEmail 生成与 PDF 同语言的邮件标题与正文。
func (s *PaymentService) buildReceiptEmail(loc *i18n.Localizer, view receiptEmailView) (string, string) {
	title := loc.T(receipt.TitleKey(view.Doc.Type))
	args := i18n.Args{
		"title":   title,
		"number":  view.Doc.Number,
		"brand":   view.Brand,
		"orderNo": view.Doc.OrderNo,
	}
	subject := loc.T("email.subject", args)

	name := ""
	if view.Customer != nil {
		name = strings.TrimSpace(view.Customer.Nickname)
	}
	if name == "" {
		name = loc.T("party.guest")
	}

	blocks := []mailBlock{mailParagraph(loc.T("email.greeting", i18n.Args{"name": name}))}
	if view.Attached {
		blocks = append(blocks, mailParagraph(loc.T("email.attached")))
	}
	// 单号行按主体取：订单凭证给订单号，钱包凭证给流水号。
	// 空值的明细行由 mailDetails 自行略过，因此两者各填各的即可。
	blocks = append(blocks, mailDetails(
		emailDetail{Label: loc.T("payment.orderNo"), Value: view.Doc.OrderNo},
		emailDetail{Label: loc.T("meta.walletTxnNo"), Value: view.Subject.TransactionNo},
		emailDetail{Label: loc.T("totals.total"), Value: loc.MoneyWithCode(view.Doc.Total, view.Doc.Currency)},
		emailDetail{Label: loc.T("payment.method"), Value: s.receiptMethodLabel(loc, view.Subject.PaymentMethod)},
		emailDetail{Label: loc.T("payment.paidAt"), Value: formatReceiptTime(loc, view.Subject.PaidAt, view.Timezone)},
		emailDetail{Label: loc.T("refunds.status"), Value: loc.T(receipt.StatusKey(view.Doc.Status))},
	))
	if view.DownloadURL != "" {
		blocks = append(blocks,
			mailParagraph(loc.T("email.linkLead", i18n.Args{
				"expiry": loc.DateTime(view.LinkExpiry, view.Timezone),
			})),
			mailButton(loc.T("email.button", args), view.DownloadURL),
			mailLink(title, view.DownloadURL),
		)
	}

	html := renderEmailLayoutWith(emailLayout{
		Lang:        loc.Tag().String(),
		AppName:     view.Brand,
		Eyebrow:     title,
		Title:       loc.T("email.title", args),
		Lead:        loc.T("email.lead", args),
		Blocks:      blocks,
		FooterNote:  loc.T("footer.disclaimer"),
		NoReplyNote: loc.T("email.footer"),
	})
	return subject, html
}

// receiptMethodLabel 渠道展示名，优先取译文；与 PDF 上的写法保持一致。
func (s *PaymentService) receiptMethodLabel(loc *i18n.Localizer, method string) string {
	key := "method." + strings.TrimSpace(method)
	if method != "" && loc.Has(key) {
		return loc.T(key)
	}
	return method
}

func formatReceiptTime(loc *i18n.Localizer, at *time.Time, tz *time.Location) string {
	if at == nil {
		return ""
	}
	return loc.DateTime(*at, tz)
}
