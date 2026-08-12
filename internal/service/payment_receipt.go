package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/receipt"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 支付凭证（收据 / 账单 / 退款凭证）的装配与导出。
//
// 分工：本文件负责「订单 + 用户 + 应用 + 退款单 → 一份与业务无关的凭证文档」，
// pkg/receipt 负责「文档 → 任何语言下都读得懂的 PDF」。
// 这条线划在这里，是因为排版与字体的问题（换行、缺字、分页）与支付业务毫无关系，
// 混在一起会让两边都难改。

// initReceiptRenderer 构造凭证渲染器。
//
// 失败不阻断服务启动：凭证只是支付的一个附属能力，不该因为字体资源有问题
// 让整个支付网关起不来。真正导出时会返回一个说明清楚的错误。
func (s *PaymentService) initReceiptRenderer() {
	renderer, err := receipt.NewRenderer(receipt.Config{
		Fonts: receipt.FontConfig{
			RegularPath:       s.receiptCfg.FontPath,
			BoldPath:          s.receiptCfg.FontBoldPath,
			Dirs:              s.receiptCfg.FontDirs,
			DisableSystemScan: s.receiptCfg.DisableSystemFontScan,
		},
		Producer: "Aegis",
	})
	if err != nil {
		s.log.Error("payment receipt renderer unavailable", zap.Error(err))
		return
	}
	s.receipts = renderer

	// 字体能力在启动时就说清楚。等到用户点「下载收据」才发现出不来中文，
	// 排查会从支付一路查到前端，而根因只是镜像里没装字体。
	s.log.Info("payment receipt renderer ready",
		zap.String("fonts", renderer.FontStatus()),
		zap.Bool("cjk", renderer.SupportsCJK()),
		zap.String("defaultLocale", s.receiptCfg.DefaultLocale))
	for _, note := range renderer.FontNotes() {
		s.log.Warn("payment receipt font", zap.String("note", note))
	}
}

// ReceiptCapability 凭证能力自述：支持哪些语言、能不能出中日韩、字体从哪来。
// 控制台据此提示管理员「这台机器出不了中文凭证」，而不是等用户下载到一份英文的。
func (s *PaymentService) ReceiptCapability() paymentdomain.ReceiptCapability {
	if s.receipts == nil {
		return paymentdomain.ReceiptCapability{
			DefaultLocale: s.receiptCfg.DefaultLocale,
			FontStatus:    "凭证渲染器不可用",
		}
	}
	supportsCJK := s.receipts.SupportsCJK()
	infos := s.receipts.Locales()
	locales := make([]paymentdomain.ReceiptLocale, 0, len(infos))
	for _, info := range infos {
		needsFont := info.Script == "han" || info.Script == "kana" || info.Script == "hangul"
		locales = append(locales, paymentdomain.ReceiptLocale{
			Tag:        info.Tag,
			Name:       info.Name,
			NativeName: info.NativeName,
			Direction:  info.Direction,
			Script:     info.Script,
			Default:    info.Default,
			NeedsFont:  needsFont,
			Available:  !needsFont || supportsCJK,
		})
	}
	return paymentdomain.ReceiptCapability{
		Locales:       locales,
		DefaultLocale: s.receiptCfg.DefaultLocale,
		SupportsCJK:   supportsCJK,
		FontStatus:    s.receipts.FontStatus(),
		FontNotes:     s.receipts.FontNotes(),
	}
}

// ── 用户侧导出 ──

// CreateUserOrderReceipt 生成凭证并落盘，返回一次性下载凭据。
func (s *PaymentService) CreateUserOrderReceipt(ctx context.Context, session *authdomain.Session, orderNo string, opts paymentdomain.ReceiptOptions) (*paymentdomain.ReceiptExport, error) {
	if session == nil {
		return nil, apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	doc, result, err := s.renderUserReceipt(ctx, session, orderNo, opts)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	export := paymentdomain.ReceiptExport{
		BillID:          randomReceiptID(16),
		OrderNo:         doc.OrderNo,
		FileName:        receiptFileName(doc.Type, doc.OrderNo, result.Locale),
		DocumentType:    string(doc.Type),
		Locale:          result.Locale,
		RequestedLocale: result.RequestedLocale,
		LocaleFallback:  result.LocaleFallback,
		DegradedGlyphs:  string(result.MissingGlyphs),
		Currency:        doc.Currency,
		Pages:           result.Pages,
		Size:            len(result.PDF),
		CreatedAt:       now,
		ExpiresAt:       now.Add(s.resolveReceiptTTL(opts.TTL)),
	}
	export.DownloadURL = fmt.Sprintf("/api/pay/bills/%s/download", export.BillID)
	if err := s.persistReceipt(session.AppID, session.UserID, export, result.PDF); err != nil {
		return nil, err
	}
	return &export, nil
}

// RenderUserOrderReceipt 直接返回 PDF 字节，不落盘。
// 两步式导出（先建后下）是为了让下载链接可以分享；只想拿一份文件的调用方走这条。
func (s *PaymentService) RenderUserOrderReceipt(ctx context.Context, session *authdomain.Session, orderNo string, opts paymentdomain.ReceiptOptions) ([]byte, string, error) {
	if session == nil {
		return nil, "", apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	doc, result, err := s.renderUserReceipt(ctx, session, orderNo, opts)
	if err != nil {
		return nil, "", err
	}
	return result.PDF, receiptFileName(doc.Type, doc.OrderNo, result.Locale), nil
}

// DownloadUserOrderReceipt 取回之前导出的凭证文件。
func (s *PaymentService) DownloadUserOrderReceipt(ctx context.Context, session *authdomain.Session, billID string) ([]byte, string, error) {
	_ = ctx
	if session == nil {
		return nil, "", apperrors.New(40170, http.StatusUnauthorized, "未认证")
	}
	meta, metaPath, err := s.loadReceiptMeta(session.AppID, billID)
	if err != nil {
		return nil, "", err
	}
	// 元数据里的 appid 与 userId 都要核对：只对 billID 做随机化不足以防越权，
	// 猜中一个 ID 就能拿到别人的交易明细。
	if meta.AppID != session.AppID || meta.UserID != session.UserID {
		return nil, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	if time.Now().UTC().After(meta.ExpiresAt) {
		s.deleteReceipt(meta, metaPath)
		return nil, "", apperrors.New(41075, http.StatusGone, "账单文件已过期")
	}
	pdfBytes, missing, err := readReceiptFile(meta.FilePath)
	if err != nil {
		s.log.Warn("read receipt file failed", zap.String("bill_id", meta.BillID), zap.String("file_path", meta.FilePath), zap.Error(err))
		if missing {
			// 文件没了，留着元数据只会让下一次请求再走一遍同样的失败
			_ = os.Remove(metaPath)
		}
		return nil, "", err
	}
	return pdfBytes, meta.FileName, nil
}

// readReceiptFile 读取落盘的凭证文件。
// 第二个返回值区分「文件确实不在了」与「读不动」——前者可以顺手清掉元数据。
func readReceiptFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, false, nil
	}
	if os.IsNotExist(err) {
		return nil, true, apperrors.New(40476, http.StatusNotFound, "账单文件不存在")
	}
	return nil, false, apperrors.New(50075, http.StatusInternalServerError, "读取账单文件失败")
}

// RenderAppOrderReceipt 管理端按应用维度导出任意订单的凭证（不落盘）。
func (s *PaymentService) RenderAppOrderReceipt(ctx context.Context, appID int64, orderNo string, opts paymentdomain.ReceiptOptions) ([]byte, string, error) {
	order, err := s.pg.GetPaymentOrderByOrderNo(ctx, strings.TrimSpace(orderNo))
	if err != nil {
		return nil, "", err
	}
	if order == nil || order.AppID != appID {
		return nil, "", apperrors.New(40472, http.StatusNotFound, "订单不存在")
	}
	var user *userdomain.User
	var profile *userdomain.Profile
	if order.UserID != nil {
		if user, err = s.pg.GetUserByID(ctx, *order.UserID); err != nil {
			return nil, "", err
		}
		if user != nil {
			if profile, err = s.pg.GetUserProfileByUserID(ctx, *order.UserID); err != nil {
				return nil, "", err
			}
		}
	}
	doc, result, err := s.renderReceipt(ctx, order, user, profile, opts, nil)
	if err != nil {
		return nil, "", err
	}
	return result.PDF, receiptFileName(doc.Type, doc.OrderNo, result.Locale), nil
}

// renderUserReceipt 用户侧的取单 + 渲染，权限收敛在 GetUserOrder 上。
func (s *PaymentService) renderUserReceipt(ctx context.Context, session *authdomain.Session, orderNo string, opts paymentdomain.ReceiptOptions) (receipt.Document, *receipt.Result, error) {
	order, err := s.GetUserOrder(ctx, session, orderNo)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return receipt.Document{}, nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	settings := s.loadUserUISettings(ctx, session.UserID)
	return s.renderReceipt(ctx, order, user, profile, opts, settings)
}

// renderReceipt 装配文档并交给渲染器。
func (s *PaymentService) renderReceipt(
	ctx context.Context,
	order *paymentdomain.Order,
	user *userdomain.User,
	profile *userdomain.Profile,
	opts paymentdomain.ReceiptOptions,
	settings map[string]any,
) (receipt.Document, *receipt.Result, error) {
	if s.receipts == nil {
		return receipt.Document{}, nil, errReceiptRendererUnavailable()
	}
	doc, err := s.buildReceiptDocument(ctx, order, user, profile, opts)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	result, err := s.renderReceiptDocument(doc, order.OrderNo, opts, settings)
	if err != nil {
		return receipt.Document{}, nil, err
	}
	return doc, result, nil
}

// renderReceiptDocument 把一份装配好的文档交给渲染器，并统一处理降级与缺字上报。
//
// 订单凭证与钱包流水凭证共用这一步：语言协商、时区、字体降级判定与告警口径
// 都与凭证主体无关，各写一份只会让两种凭证在同一个环境下表现不一致。
// subject 只进日志（订单号或流水号），用于把告警定位回具体那一笔。
func (s *PaymentService) renderReceiptDocument(
	doc receipt.Document,
	subject string,
	opts paymentdomain.ReceiptOptions,
	settings map[string]any,
) (*receipt.Result, error) {
	if s.receipts == nil {
		return nil, errReceiptRendererUnavailable()
	}
	result, err := s.receipts.Render(doc, receipt.Options{
		LocalePrefs: s.resolveLocalePrefs(opts, settings),
		Timezone:    resolveReceiptTimezone(opts.Timezone, settings),
	})
	if err != nil {
		s.log.Error("render payment receipt failed", zap.String("subject", subject), zap.Error(err))
		return nil, apperrors.New(50373, http.StatusInternalServerError, "生成凭证失败")
	}
	if result.LocaleFallback {
		s.log.Warn("payment receipt locale downgraded to default",
			zap.String("subject", subject),
			zap.String("requested", result.RequestedLocale),
			zap.String("fonts", s.receipts.FontStatus()))
	}
	if len(result.MissingGlyphs) > 0 {
		s.log.Warn("payment receipt has unrenderable characters",
			zap.String("subject", subject),
			zap.String("glyphs", string(result.MissingGlyphs)),
			zap.String("fonts", s.receipts.FontStatus()))
	}
	return result, nil
}

func errReceiptRendererUnavailable() error {
	return apperrors.New(50372, http.StatusServiceUnavailable,
		"凭证渲染器不可用，请检查 PAYMENT_RECEIPT_FONT_PATH 配置与服务日志")
}

// buildReceiptDocument 把订单装配成一份凭证文档。
func (s *PaymentService) buildReceiptDocument(
	ctx context.Context,
	order *paymentdomain.Order,
	user *userdomain.User,
	profile *userdomain.Profile,
	opts paymentdomain.ReceiptOptions,
) (receipt.Document, error) {
	app, err := s.requireApp(ctx, order.AppID)
	if err != nil {
		return receipt.Document{}, err
	}
	refunds, err := s.pg.ListPaymentRefundsByOrder(ctx, order.ID)
	if err != nil {
		// 退款记录取不到不该让凭证出不来：主体信息（金额、订单号、支付时间）都在订单上，
		// 缺一段退款明细远好于一份也拿不到的凭证。
		s.log.Warn("load refunds for receipt failed", zap.String("orderNo", order.OrderNo), zap.Error(err))
		refunds = nil
	}

	currency := s.resolveOrderCurrency(ctx, order)
	docType := resolveReceiptDocType(order, opts.DocumentType)

	doc := receipt.Document{
		Type:     docType,
		Status:   resolveReceiptStatus(order),
		Number:   receiptNumber(docType, order),
		OrderNo:  order.OrderNo,
		IssuedAt: time.Now().UTC(),
		Brand:    s.platformBrand(ctx),
		Issuer: receipt.Party{
			Name:      pickString(app.Name, fmt.Sprintf("App #%d", order.AppID)),
			Reference: fmt.Sprintf("APP-%d", order.AppID),
		},
		Customer: buildReceiptCustomer(order, user, profile),
		Currency: currency,
		Items: []receipt.LineItem{{
			Name:        pickString(order.Subject, order.OrderNo),
			Description: order.Body,
			Quantity:    decimal.NewFromInt(1),
			UnitPrice:   order.Amount,
			Amount:      order.Amount,
		}},
		Total:         order.Amount,
		RefundedTotal: refundedTotal(order, refunds),
		Payment: receipt.PaymentInfo{
			MethodKey:       order.PaymentMethod,
			MethodLabel:     order.PaymentMethod,
			ProviderType:    order.ProviderType,
			ProviderOrderNo: order.ProviderOrderNo,
			PaidAt:          order.PaidAt,
			ClientIP:        order.ClientIP,
		},
		Refunds:  buildReceiptRefunds(refunds),
		Metadata: buildReceiptMetadata(order),
		Notes:    receiptNotes(order),
	}
	return doc, nil
}

func buildReceiptCustomer(order *paymentdomain.Order, user *userdomain.User, profile *userdomain.Profile) receipt.Party {
	var party receipt.Party
	if profile != nil && strings.TrimSpace(profile.Nickname) != "" {
		party.Name = strings.TrimSpace(profile.Nickname)
	} else if user != nil && strings.TrimSpace(user.Account) != "" {
		party.Name = strings.TrimSpace(user.Account)
	}
	if user != nil {
		party.Subtitle = strings.TrimSpace(user.Account)
	}
	if profile != nil {
		party.Email = strings.TrimSpace(profile.Email)
	}
	if order.UserID != nil {
		party.Reference = fmt.Sprintf("UID-%d", *order.UserID)
	}
	return party
}

func buildReceiptRefunds(refunds []paymentdomain.Refund) []receipt.RefundInfo {
	items := make([]receipt.RefundInfo, 0, len(refunds))
	for _, refund := range refunds {
		// 未提交上游的退款单不该出现在凭证上：它还不是一笔已发生的资金往来
		if refund.Status == paymentdomain.RefundPending {
			continue
		}
		items = append(items, receipt.RefundInfo{
			Number: refund.RefundNo,
			Amount: refund.Amount,
			Status: refund.Status,
			Reason: refund.Reason,
			At:     refund.RefundedAt,
		})
	}
	return items
}

// refundedTotal 已退金额。
//
// 以退款单里**已成功**的部分为准，而不是订单上的 refunded_amount ——
// 后者记的是「已占用额度」，包含还在途中的退款。凭证上写「已退款」
// 却对应一笔尚未到账的钱，会直接引发客诉。
func refundedTotal(order *paymentdomain.Order, refunds []paymentdomain.Refund) decimal.Decimal {
	if len(refunds) == 0 {
		if order.RefundStatus == paymentdomain.OrderRefundFull {
			return order.Amount
		}
		return decimal.Zero
	}
	total := decimal.Zero
	for _, refund := range refunds {
		if refund.Status == paymentdomain.RefundSuccess {
			total = total.Add(refund.Amount)
		}
	}
	return total
}

// buildReceiptMetadata 附加信息区。标签恒为译文键；枚举值走 ValueKey，字面值走 Value。
func buildReceiptMetadata(order *paymentdomain.Order) []receipt.KeyValue {
	pairs := []receipt.KeyValue{
		{Key: "meta.appId", Value: fmt.Sprintf("%d", order.AppID)},
	}
	if order.UserID != nil {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.userId", Value: fmt.Sprintf("%d", *order.UserID)})
	}
	purpose := metaString(order.Metadata, paymentdomain.MetaKeyPurpose)
	switch purpose {
	case paymentdomain.PurposeWalletRecharge, paymentdomain.PurposeVipPurchase, paymentdomain.PurposeIntegralPurchase:
		pairs = append(pairs, receipt.KeyValue{Key: "meta.purpose", ValueKey: "purpose." + purpose})
	}
	if name := metaString(order.Metadata, paymentdomain.MetaKeyVipPlanName); name != "" {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.plan", Value: name})
	}
	if days := metaInt64(order.Metadata, paymentdomain.MetaKeyVipDays); days > 0 {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.duration", Value: fmt.Sprintf("%d", days)})
	}
	if bonus := metaInt64(order.Metadata, paymentdomain.MetaKeyVipBonus); bonus > 0 {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.bonusPoints", Value: fmt.Sprintf("%d", bonus)})
	}
	if points := metaInt64(order.Metadata, paymentdomain.MetaKeyIntegralAmount); points > 0 {
		pairs = append(pairs, receipt.KeyValue{Key: "meta.points", Value: fmt.Sprintf("%d", points)})
	}
	return pairs
}

// receiptNotes 状态相关的提示语。全部走译文键，由渲染器按目标语言展开。
func receiptNotes(order *paymentdomain.Order) []receipt.Note {
	switch {
	case order.Status != "paid":
		return []receipt.Note{{Key: "notes.unpaid"}}
	case order.RefundStatus == paymentdomain.OrderRefundFull:
		return []receipt.Note{{Key: "notes.refunded"}}
	case order.RefundStatus == paymentdomain.OrderRefundPartial:
		return []receipt.Note{{Key: "notes.partiallyRefunded"}}
	default:
		return nil
	}
}

// resolveReceiptDocType 凭证类型。
//
// 未支付的订单出「账单」而不是「收据」—— 给一笔还没收到的钱开收据是在伪造凭证；
// 全额退款后出「退款凭证」，因为这份文件此时证明的是退款而非收款。
func resolveReceiptDocType(order *paymentdomain.Order, override string) receipt.DocType {
	switch strings.TrimSpace(override) {
	case paymentdomain.ReceiptTypeReceipt:
		return receipt.TypeReceipt
	case paymentdomain.ReceiptTypeInvoice:
		return receipt.TypeInvoice
	case paymentdomain.ReceiptTypeCreditNote:
		return receipt.TypeCreditNote
	}
	switch {
	case order.Status != "paid":
		return receipt.TypeInvoice
	case order.RefundStatus == paymentdomain.OrderRefundFull:
		return receipt.TypeCreditNote
	default:
		return receipt.TypeReceipt
	}
}

func resolveReceiptStatus(order *paymentdomain.Order) receipt.Status {
	switch order.Status {
	case "paid":
		switch order.RefundStatus {
		case paymentdomain.OrderRefundFull:
			return receipt.StatusRefunded
		case paymentdomain.OrderRefundPartial:
			return receipt.StatusPartiallyRefunded
		default:
			return receipt.StatusPaid
		}
	case "expired":
		return receipt.StatusExpired
	case "failed":
		return receipt.StatusFailed
	case "cancelled", "closed":
		return receipt.StatusCancelled
	default:
		return receipt.StatusPending
	}
}

// receiptNumber 凭证编号：类型前缀 + 订单号。同一订单反复导出得到同一个编号 ——
// 凭证编号是给人对账用的，每次下载都变一个号会让对账无从下手。
func receiptNumber(docType receipt.DocType, order *paymentdomain.Order) string {
	return receiptNumberFor(docType, order.OrderNo)
}

// receiptNumberFor 编号规则的唯一实现，订单与钱包流水共用。
//
// 两类主体不另立前缀：单号本身就带出处（订单是 P…、钱包流水是 WAL…），
// 再分一套 RCP/WAL 前缀只会多出一条对账时要记住的规则。
func receiptNumberFor(docType receipt.DocType, subjectNo string) string {
	prefix := "RCP"
	switch docType {
	case receipt.TypeInvoice:
		prefix = "INV"
	case receipt.TypeCreditNote:
		prefix = "CRN"
	}
	return prefix + "-" + subjectNo
}

// ── 语言、时区与币种 ──

// resolveLocalePrefs 语言偏好链：显式指定 → 用户设置 → 请求头 → 平台默认。
//
// 用户设置排在请求头前面：用户在应用里选过语言，那是一次明确的表达，
// 不该被浏览器的 Accept-Language 覆盖掉。
func (s *PaymentService) resolveLocalePrefs(opts paymentdomain.ReceiptOptions, settings map[string]any) []string {
	prefs := []string{opts.Locale}
	if settings != nil {
		if lang, ok := settings["language"].(string); ok {
			prefs = append(prefs, lang)
		}
	}
	return append(prefs, opts.AcceptLanguage, s.receiptCfg.DefaultLocale)
}

// loadUserUISettings 取用户的界面设置（语言、时区）。
// 取不到就当没配置：为了一份凭证的语言去阻断导出不合算。
func (s *PaymentService) loadUserUISettings(ctx context.Context, userID int64) map[string]any {
	settings, err := s.pg.GetUserSettings(ctx, userID, "ui")
	if err != nil || settings == nil {
		return nil
	}
	return settings.Settings
}

func resolveReceiptTimezone(explicit string, settings map[string]any) *time.Location {
	candidates := []string{explicit}
	if settings != nil {
		if tz, ok := settings["timezone"].(string); ok {
			candidates = append(candidates, tz)
		}
	}
	for _, name := range candidates {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.UTC
}

// resolveOrderCurrency 订单币种。
//
// 优先用订单上固化的值。历史订单没有这一列，退回按渠道配置推断 ——
// 推断只用于展示既有数据，新订单一律在下单时固化（见 CreateOrder）。
func (s *PaymentService) resolveOrderCurrency(ctx context.Context, order *paymentdomain.Order) string {
	if code := strings.ToUpper(strings.TrimSpace(order.Currency)); code != "" {
		return code
	}
	config, err := s.pg.GetPaymentConfigByID(ctx, order.AppID, order.ConfigID)
	if err != nil || config == nil {
		return s.defaultCurrencyForMethod(order.PaymentMethod)
	}
	return s.resolveConfigCurrency(order.PaymentMethod, config.ConfigData)
}

// resolveConfigCurrency 从渠道配置解析计价货币，缺失时用该渠道自述的首选货币。
func (s *PaymentService) resolveConfigCurrency(method string, configData map[string]any) string {
	if raw, ok := configData["currency"].(string); ok {
		if code := strings.ToUpper(strings.TrimSpace(raw)); code != "" {
			return code
		}
	}
	return s.defaultCurrencyForMethod(method)
}

// defaultCurrencyForMethod 渠道未配置 currency 时的计价货币，取自渠道**自述**的
// Currencies 首项（国内渠道只结算人民币，因此它们根本没有 currency 配置项）。
//
// 走自述而不是在这里维护一张 method → 货币的表：新增渠道时只要 Describe() 写对了，
// 凭证上的币种就是对的；两处各维护一份，早晚会出现凭证上印着渠道根本不支持的货币。
func (s *PaymentService) defaultCurrencyForMethod(method string) string {
	if provider, err := s.resolveProvider(method); err == nil {
		if currencies := provider.Describe().Currencies; len(currencies) > 0 {
			if code := strings.ToUpper(strings.TrimSpace(currencies[0])); code != "" {
				return code
			}
		}
	}
	return "USD"
}

// platformBrand 凭证抬头处的平台名。取不到就用产品名兜底 ——
// 抬头空着比写错更难看，而这不值得让导出失败。
func (s *PaymentService) platformBrand(ctx context.Context) string {
	if s.platform == nil {
		return "Aegis"
	}
	settings, err := s.platform.GetSettings(ctx)
	if err != nil || settings == nil {
		return "Aegis"
	}
	return pickString(strings.TrimSpace(settings.Branding.PlatformName), "Aegis")
}

// receiptFileName 下载文件名。
//
// 恒为 ASCII：Content-Disposition 里的非 ASCII 文件名需要 RFC 5987 编码，
// 各家客户端处理得参差不齐，出一份下载下来叫「????.pdf」的凭证不值得。
func receiptFileName(docType receipt.DocType, orderNo string, locale string) string {
	prefix := "receipt"
	switch docType {
	case receipt.TypeInvoice:
		prefix = "invoice"
	case receipt.TypeCreditNote:
		prefix = "credit_note"
	}
	safe := func(value string) string {
		var b strings.Builder
		for _, r := range value {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
				b.WriteRune(r)
			default:
				b.WriteRune('_')
			}
		}
		return b.String()
	}
	return fmt.Sprintf("%s_%s_%s.pdf", prefix, safe(orderNo), safe(locale))
}

// ── 导出文件的落盘与清理 ──

// receiptMeta 落盘凭证的元数据。与 PDF 同目录、同名不同后缀。
type receiptMeta struct {
	BillID string `json:"billId"`
	AppID  int64  `json:"appid"`
	UserID int64  `json:"userId"`
	// OrderNo / TransactionNo 二选一：凭证要么由订单出具，要么由钱包流水出具
	OrderNo       string    `json:"orderNo,omitempty"`
	TransactionNo string    `json:"transactionNo,omitempty"`
	FileName      string    `json:"fileName"`
	FilePath      string    `json:"filePath"`
	Locale        string    `json:"locale,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (s *PaymentService) resolveReceiptTTL(override time.Duration) time.Duration {
	ttl := s.receiptCfg.TTL
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	if override <= 0 || override > ttl {
		return ttl
	}
	return override
}

func (s *PaymentService) receiptCleanupInterval() time.Duration {
	if s.receiptCfg.CleanupInterval <= 0 {
		return 5 * time.Minute
	}
	return s.receiptCfg.CleanupInterval
}

func (s *PaymentService) receiptAppDir(appID int64) string {
	return filepath.Join(s.receiptCfg.RootDir, fmt.Sprintf("%d", appID))
}

func (s *PaymentService) receiptPaths(appID int64, billID string) (string, string) {
	dirPath := s.receiptAppDir(appID)
	return filepath.Join(dirPath, billID+".pdf"), filepath.Join(dirPath, billID+".meta.json")
}

func (s *PaymentService) persistReceipt(appID int64, userID int64, export paymentdomain.ReceiptExport, pdfBytes []byte) error {
	dirPath := s.receiptAppDir(appID)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		s.log.Warn("create receipt dir failed", zap.String("dir", dirPath), zap.Error(err))
		return apperrors.New(50076, http.StatusInternalServerError, "创建账单导出目录失败")
	}
	pdfPath, metaPath := s.receiptPaths(appID, export.BillID)
	meta := receiptMeta{
		BillID:        export.BillID,
		AppID:         appID,
		UserID:        userID,
		OrderNo:       export.OrderNo,
		TransactionNo: export.TransactionNo,
		FileName:      export.FileName,
		FilePath:      pdfPath,
		Locale:        export.Locale,
		CreatedAt:     export.CreatedAt,
		ExpiresAt:     export.ExpiresAt,
	}
	if err := os.WriteFile(pdfPath, pdfBytes, 0o600); err != nil {
		s.log.Warn("write receipt file failed", zap.String("bill_id", export.BillID), zap.String("file_path", pdfPath), zap.Error(err))
		return apperrors.New(50077, http.StatusInternalServerError, "写入账单文件失败")
	}
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		_ = os.Remove(pdfPath)
		return apperrors.New(50078, http.StatusInternalServerError, "写入账单元数据失败")
	}
	if err := os.WriteFile(metaPath, rawMeta, 0o600); err != nil {
		_ = os.Remove(pdfPath)
		s.log.Warn("write receipt metadata failed", zap.String("bill_id", export.BillID), zap.String("meta_path", metaPath), zap.Error(err))
		return apperrors.New(50078, http.StatusInternalServerError, "写入账单元数据失败")
	}
	return nil
}

func (s *PaymentService) loadReceiptMeta(appID int64, billID string) (receiptMeta, string, error) {
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return receiptMeta{}, "", apperrors.New(40075, http.StatusBadRequest, "账单标识不能为空")
	}
	// 凭证标识来自 URL，必须挡住路径穿越：../ 拼进文件名就能读到其它应用的目录
	if !isHexToken(billID) {
		return receiptMeta{}, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	_, metaPath := s.receiptPaths(appID, billID)
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return receiptMeta{}, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
		}
		s.log.Warn("read receipt metadata failed", zap.String("bill_id", billID), zap.String("meta_path", metaPath), zap.Error(err))
		return receiptMeta{}, "", apperrors.New(50079, http.StatusInternalServerError, "读取账单元数据失败")
	}
	var meta receiptMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		s.log.Warn("parse receipt metadata failed", zap.String("bill_id", billID), zap.String("meta_path", metaPath), zap.Error(err))
		pdfPath, _ := s.receiptPaths(appID, billID)
		s.deleteReceipt(receiptMeta{BillID: billID, AppID: appID, FilePath: pdfPath}, metaPath)
		return receiptMeta{}, "", apperrors.New(40475, http.StatusNotFound, "账单不存在")
	}
	return meta, metaPath, nil
}

func (s *PaymentService) cleanupExpiredReceipts() {
	rootDir := strings.TrimSpace(s.receiptCfg.RootDir)
	if rootDir == "" {
		return
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		s.log.Warn("create receipt root failed", zap.String("root_dir", rootDir), zap.Error(err))
		return
	}
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("list receipt root failed", zap.String("root_dir", rootDir), zap.Error(err))
		}
		return
	}
	now := time.Now().UTC()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaPaths, err := filepath.Glob(filepath.Join(rootDir, entry.Name(), "*.meta.json"))
		if err != nil {
			s.log.Warn("glob receipt metadata failed", zap.String("app_dir", filepath.Join(rootDir, entry.Name())), zap.Error(err))
			continue
		}
		for _, metaPath := range metaPaths {
			meta, ok := s.readReceiptMetaFile(metaPath)
			if !ok {
				continue
			}
			if now.After(meta.ExpiresAt) {
				s.deleteReceipt(meta, metaPath)
			}
		}
	}
}

func (s *PaymentService) readReceiptMetaFile(metaPath string) (receiptMeta, bool) {
	rawMeta, err := os.ReadFile(metaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Warn("read receipt metadata failed", zap.String("meta_path", metaPath), zap.Error(err))
		}
		return receiptMeta{}, false
	}
	var meta receiptMeta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		s.log.Warn("parse receipt metadata failed", zap.String("meta_path", metaPath), zap.Error(err))
		_ = os.Remove(metaPath)
		return receiptMeta{}, false
	}
	return meta, true
}

func (s *PaymentService) deleteReceipt(meta receiptMeta, metaPath string) {
	if strings.TrimSpace(meta.FilePath) != "" {
		if err := os.Remove(meta.FilePath); err != nil && !os.IsNotExist(err) {
			s.log.Warn("delete receipt file failed", zap.String("bill_id", meta.BillID), zap.String("file_path", meta.FilePath), zap.Error(err))
		}
	}
	if strings.TrimSpace(metaPath) != "" {
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			s.log.Warn("delete receipt metadata failed", zap.String("bill_id", meta.BillID), zap.String("meta_path", metaPath), zap.Error(err))
		}
	}
}

func randomReceiptID(byteLen int) string {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x%s", time.Now().UTC().UnixNano(), randomDigits(6))
	}
	return hex.EncodeToString(buf)
}

// isHexToken 仅由十六进制字符组成（凭证标识的形态）。
func isHexToken(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// ── 订单上的凭证入口 ──

// ListUserOrderViews 用户订单分页，每条都带上凭证入口。
func (s *PaymentService) ListUserOrderViews(ctx context.Context, session *authdomain.Session, query paymentdomain.OrderListQuery, opts paymentdomain.ReceiptOptions) (*paymentdomain.UserOrderListResult, error) {
	result, err := s.ListUserOrders(ctx, session, query)
	if err != nil {
		return nil, err
	}
	ctxInfo := s.resolveOrderReceiptContext(ctx, session, opts)
	items := make([]paymentdomain.UserOrderView, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, paymentdomain.UserOrderView{
			Order:   result.Items[i],
			Receipt: s.buildOrderReceipt(ctx, &result.Items[i], ctxInfo),
		})
	}
	return &paymentdomain.UserOrderListResult{
		Items:      items,
		Page:       result.Page,
		Limit:      result.Limit,
		Total:      result.Total,
		TotalPages: result.TotalPages,
	}, nil
}

// GetUserOrderView 单个订单 + 凭证入口。
func (s *PaymentService) GetUserOrderView(ctx context.Context, session *authdomain.Session, orderNo string, opts paymentdomain.ReceiptOptions) (*paymentdomain.UserOrderView, error) {
	order, err := s.GetUserOrder(ctx, session, orderNo)
	if err != nil {
		return nil, err
	}
	view := paymentdomain.UserOrderView{
		Order:   *order,
		Receipt: s.buildOrderReceipt(ctx, order, s.resolveOrderReceiptContext(ctx, session, opts)),
	}
	return &view, nil
}

// orderReceiptContext 一次请求内对所有订单都相同的部分：推荐语言、能否寄送。
// 单独算一次，否则列表里每条订单都要重新读一遍用户设置与资料。
type orderReceiptContext struct {
	locale string
	// walletCurrency 钱包记账币种，钱包流水的凭证入口用它标注金额单位。
	// 只在钱包那条链路上填（见 WalletService.receiptContext）——
	// 订单列表用不到它，为它多打一次库不划算。
	walletCurrency string
	emailable      bool
	emailHint      string
}

func (s *PaymentService) resolveOrderReceiptContext(ctx context.Context, session *authdomain.Session, opts paymentdomain.ReceiptOptions) orderReceiptContext {
	info := orderReceiptContext{locale: s.receiptCfg.DefaultLocale}
	if s.receipts == nil {
		return info
	}
	settings := s.loadUserUISettings(ctx, session.UserID)
	// 用与出具凭证时**同一条**优先级链与同一个降级判定，
	// 否则列表上标的语言会和点下载后真正拿到的对不上。
	info.locale = s.receipts.EffectiveLocale(s.resolveLocalePrefs(opts, settings)...)

	switch {
	case s.email == nil:
		info.emailHint = "邮件服务不可用"
	default:
		profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID)
		if err != nil || profile == nil || strings.TrimSpace(profile.Email) == "" {
			info.emailHint = "账号尚未绑定邮箱"
		} else {
			info.emailable = true
		}
	}
	return info
}

func (s *PaymentService) buildOrderReceipt(ctx context.Context, order *paymentdomain.Order, info orderReceiptContext) paymentdomain.OrderReceipt {
	_ = ctx
	if s.receipts == nil {
		return paymentdomain.OrderReceipt{Available: false, EmailHint: "凭证渲染器不可用"}
	}
	docType := resolveReceiptDocType(order, "")
	return paymentdomain.OrderReceipt{
		Available:    true,
		DocumentType: string(docType),
		Locale:       info.locale,
		Currency:     strings.ToUpper(strings.TrimSpace(order.Currency)),
		DownloadURL:  fmt.Sprintf("/api/pay/orders/%s/receipt", order.OrderNo),
		ExportURL:    fmt.Sprintf("/api/pay/orders/%s/bill", order.OrderNo),
		EmailURL:     fmt.Sprintf("/api/pay/orders/%s/receipt/email", order.OrderNo),
		Emailable:    info.emailable,
		EmailHint:    info.emailHint,
	}
}
