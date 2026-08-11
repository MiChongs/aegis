package httptransport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	emaildomain "aegis/internal/domain/email"
	paymentdomain "aegis/internal/domain/payment"
	workflowdomain "aegis/internal/domain/workflow"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminEmailConfigList(c *gin.Context) {
	var req AdminEmailConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.email.ListConfigs(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminEmailConfigDetail(c *gin.Context) {
	var req AdminEmailConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.email.Detail(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminEmailConfigCreate(c *gin.Context) { h.adminEmailConfigSave(c, 0) }

func (h *Handler) AdminEmailConfigUpdate(c *gin.Context) {
	var req AdminEmailConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminEmailConfigSaveWithReq(c, req, req.ConfigID)
}

func (h *Handler) adminEmailConfigSave(c *gin.Context, id int64) {
	var req AdminEmailConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminEmailConfigSaveWithReq(c, req, id)
}

func (h *Handler) adminEmailConfigSaveWithReq(c *gin.Context, req AdminEmailConfigSaveRequest, id int64) {
	smtp := req.SMTP
	if smtp == nil {
		smtp = &emaildomain.SMTPConfig{
			Host:               req.SMTPHost,
			Port:               req.SMTPPort,
			Username:           req.SMTPUser,
			Password:           req.SMTPPassword,
			FromAddress:        req.SMTPFrom,
			FromName:           req.SMTPFromName,
			ReplyTo:            req.SMTPReplyTo,
			MaxConnections:     5,
			MaxMessagesPerConn: 100,
		}
		if req.SMTPTLS != nil {
			smtp.UseTLS = *req.SMTPTLS
		}
		if req.SMTPInsecure != nil {
			smtp.InsecureSkipVerify = *req.SMTPInsecure
		}
	}
	item, err := h.email.Save(c.Request.Context(), emaildomain.ConfigMutation{
		ID:          id,
		AppID:       req.AppID,
		Name:        maybeString(req.Name),
		Provider:    maybeString(req.Provider),
		Enabled:     req.Enabled,
		IsDefault:   req.IsDefault,
		Description: maybeString(req.Description),
		SMTP:        smtp,
		Zeabur:      buildZeaburMutation(req),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

// buildZeaburMutation 只在请求确实涉及 Zeabur 时才返回非 nil。
// 保存一个纯 SMTP 配置时不该顺手把 Zeabur 段清成空值，那会在切换服务商后丢配置。
func buildZeaburMutation(req AdminEmailConfigSaveRequest) *emaildomain.ZeaburConfig {
	if req.Zeabur != nil {
		return req.Zeabur
	}
	touched := strings.TrimSpace(req.ZeaburAPIKey) != "" ||
		strings.TrimSpace(req.ZeaburBaseURL) != "" ||
		strings.TrimSpace(req.ZeaburFrom) != "" ||
		strings.TrimSpace(req.ZeaburFromName) != "" ||
		strings.TrimSpace(req.ZeaburReplyTo) != "" ||
		strings.TrimSpace(req.ZeaburWebhookSecret) != "" ||
		len(req.ZeaburTags) > 0
	if !touched && !strings.EqualFold(strings.TrimSpace(req.Provider), emaildomain.ProviderZeabur) {
		return nil
	}
	return &emaildomain.ZeaburConfig{
		APIKey:        req.ZeaburAPIKey,
		BaseURL:       req.ZeaburBaseURL,
		FromAddress:   req.ZeaburFrom,
		FromName:      req.ZeaburFromName,
		ReplyTo:       req.ZeaburReplyTo,
		WebhookSecret: req.ZeaburWebhookSecret,
		Tags:          req.ZeaburTags,
	}
}

func (h *Handler) AdminEmailDeliveryList(c *gin.Context) {
	var req AdminEmailDeliveryListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.email.ListDeliveries(c.Request.Context(), emaildomain.DeliveryQuery{
		AppID:    req.AppID,
		ConfigID: req.ConfigID,
		Status:   req.Status,
		Provider: req.Provider,
		Keyword:  req.Keyword,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", page)
}

// ZeaburEmailWebhook 接收 Zeabur Email 的投递回执。
//
// 该路由是公开的（Zeabur 侧不可能携带管理员令牌），真正的准入是 HMAC 签名校验，
// 由 EmailService 用该应用配置里的 Webhook 密钥完成。
func (h *Handler) ZeaburEmailWebhook(c *gin.Context) {
	appID, err := strconv.ParseInt(strings.TrimSpace(c.Param("appid")), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "应用标识非法")
		return
	}
	// 必须拿原始字节：签名覆盖的是原文，任何重新序列化都会让验签失败。
	body, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取回调报文失败")
		return
	}
	result, err := h.email.HandleZeaburWebhook(c.Request.Context(), service.ZeaburWebhookRequest{
		AppID:      appID,
		ConfigName: strings.TrimSpace(c.Param("config")),
		Event:      c.GetHeader(service.ZeaburWebhookEventHeader),
		Timestamp:  c.GetHeader(service.ZeaburWebhookTimestampHeader),
		Signature:  c.GetHeader(service.ZeaburWebhookSignatureHeader),
		Body:       body,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "接收成功", result)
}

func (h *Handler) AdminEmailConfigDelete(c *gin.Context) {
	var req AdminEmailConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.email.Delete(c.Request.Context(), req.AppID, req.ConfigID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminEmailConfigTest(c *gin.Context) {
	var req AdminEmailConfigTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.TestConfig(c.Request.Context(), req.AppID, req.ConfigID, req.TestEmail)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试成功", result)
}

func (h *Handler) SendEmailCode(c *gin.Context) {
	var req EmailCodeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.SendVerificationCode(c.Request.Context(), req.AppID, req.Email, req.Purpose, normalizeEmailExpire(req.ExpireMinutes), req.ConfigName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

func (h *Handler) VerifyEmailCode(c *gin.Context) {
	var req EmailVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	valid, err := h.email.VerifyCode(c.Request.Context(), req.AppID, req.Email, req.Code, req.Purpose)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证完成", gin.H{"valid": valid})
}

func (h *Handler) SendPasswordResetEmail(c *gin.Context) {
	var req EmailResetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.SendPasswordResetEmail(c.Request.Context(), req.AppID, req.Email, req.ResetURL, req.ConfigName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

func (h *Handler) VerifyResetToken(c *gin.Context) {
	var req EmailVerifyResetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	valid, err := h.email.VerifyResetToken(c.Request.Context(), req.AppID, req.Email, req.Token)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证完成", gin.H{"valid": valid})
}

// AdminPaymentOrderListRequest 管理端订单列表请求
type AdminPaymentOrderListRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	Status  string `json:"status" form:"status"`
	Method  string `json:"payment_method" form:"payment_method"`
	Keyword string `json:"keyword" form:"keyword"`
	UserID  int64  `json:"user_id" form:"user_id"`
	Page    int    `json:"page" form:"page"`
	Limit   int    `json:"limit" form:"limit"`
}

// AdminPaymentOrderDetailRequest 管理端订单详情请求
type AdminPaymentOrderDetailRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	OrderNo string `json:"order_no" form:"order_no" binding:"required"`
}

func (h *Handler) AdminPaymentOrderList(c *gin.Context) {
	var req AdminPaymentOrderListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.AdminListOrders(c.Request.Context(), req.AppID, service.AdminOrderQuery{
		Status:  req.Status,
		Method:  req.Method,
		Keyword: req.Keyword,
		UserID:  req.UserID,
		Page:    req.Page,
		Limit:   req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminPaymentOrderDetail(c *gin.Context) {
	var req AdminPaymentOrderDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	detail, err := h.payment.AdminOrderDetail(c.Request.Context(), req.AppID, req.OrderNo)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminPaymentReceiptRequest 管理端凭证导出请求
type AdminPaymentReceiptRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	OrderNo string `json:"order_no" form:"order_no" binding:"required"`
	// Locale 期望语言（BCP 47）；留空用平台默认（en）
	Locale string `json:"locale" form:"locale"`
	// DocumentType receipt / invoice / credit_note；留空按订单状态推导
	DocumentType string `json:"documentType" form:"documentType"`
	// Timezone IANA 时区名；留空为 UTC
	Timezone string `json:"timezone" form:"timezone"`
}

// AdminPaymentOrderReceipt 管理端导出任意订单的凭证（客服代开、对账留档）。
//
// 与用户侧不同，这里不落盘：管理端是一次性下载，没有「把链接发给别人」的场景，
// 落盘只会在磁盘上多留一份含交易明细的文件。
func (h *Handler) AdminPaymentOrderReceipt(c *gin.Context) {
	var req AdminPaymentReceiptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	data, filename, err := h.payment.RenderAppOrderReceipt(c.Request.Context(), req.AppID, req.OrderNo, paymentdomain.ReceiptOptions{
		Locale:         strings.TrimSpace(req.Locale),
		AcceptLanguage: c.GetHeader("Accept-Language"),
		DocumentType:   strings.TrimSpace(req.DocumentType),
		Timezone:       strings.TrimSpace(req.Timezone),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

func (h *Handler) AdminPaymentConfigList(c *gin.Context) {
	var req AdminPaymentConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.payment.ListConfigs(c.Request.Context(), req.AppID, req.PaymentMethod, req.EnabledOnly)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminPaymentConfigDetail(c *gin.Context) {
	var req AdminPaymentConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.payment.Detail(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminPaymentConfigCreate(c *gin.Context) { h.adminPaymentSave(c, 0) }

func (h *Handler) AdminPaymentConfigUpdate(c *gin.Context) {
	var req AdminPaymentConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminPaymentSaveWithReq(c, req, req.ConfigID)
}

func (h *Handler) adminPaymentSave(c *gin.Context, id int64) {
	var req AdminPaymentConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.adminPaymentSaveWithReq(c, req, id)
}

func (h *Handler) adminPaymentSaveWithReq(c *gin.Context, req AdminPaymentConfigSaveRequest, id int64) {
	item, err := h.payment.Save(c.Request.Context(), paymentdomain.ConfigMutation{
		ID:            id,
		AppID:         req.AppID,
		PaymentMethod: maybeString(req.PaymentMethod),
		ConfigName:    maybeString(req.ConfigName),
		ConfigData:    req.ConfigData,
		Enabled:       req.Enabled,
		IsDefault:     req.IsDefault,
		Description:   maybeString(req.Description),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminPaymentConfigDelete(c *gin.Context) {
	var req AdminPaymentConfigDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.payment.Delete(c.Request.Context(), req.AppID, req.ConfigID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminPaymentConfigTest(c *gin.Context) {
	var req AdminPaymentConfigTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.TestConfig(c.Request.Context(), req.AppID, req.ConfigID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", result)
}

func (h *Handler) AdminPaymentEpayInit(c *gin.Context) {
	var req AdminPaymentInitEpayRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var cfg paymentdomain.EpayConfig
	if err := decodeJSON(req.EpayConfig, &cfg); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "易支付配置格式错误")
		return
	}
	item, err := h.payment.InitDefaultEpayConfig(c.Request.Context(), req.AppID, cfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "初始化成功", item)
}

func (h *Handler) CreatePaymentOrder(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req CreatePaymentOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	payload, order, err := h.payment.CreateOrder(c.Request.Context(), session, req.Subject, req.Body, req.Amount, req.Type, req.ConfigName, req.NotifyURL, req.ReturnURL, req.Metadata, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", gin.H{"payment": payload, "order": order})
}

// PaymentOrders 用户订单分页。每条订单都带上 receipt 区块 ——
// 「这单能不能开票、开出来是收据还是账单、能不能寄到邮箱」由服务端算好，
// 各端自己判断会很快各写一套且互不一致。
func (h *Handler) PaymentOrders(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserPaymentOrdersQuery
	_ = bind(c, &query)
	result, err := h.payment.ListUserOrderViews(c.Request.Context(), session, paymentdomain.OrderListQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	}, receiptOptions(c, PaymentBillExportRequest{}))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) PaymentOrderDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	order, err := h.payment.GetUserOrderView(c.Request.Context(), session, c.Param("orderNo"), receiptOptions(c, PaymentBillExportRequest{}))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", order)
}

// ExportPaymentBill 生成支付凭证并返回一次性下载凭据。
func (h *Handler) ExportPaymentBill(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	export, err := h.payment.CreateUserOrderReceipt(c.Request.Context(), session, c.Param("orderNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", export)
}

// DownloadPaymentBill 取回之前导出的凭证文件。
func (h *Handler) DownloadPaymentBill(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	data, filename, err := h.payment.DownloadUserOrderReceipt(c.Request.Context(), session, c.Param("billId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

// DownloadPaymentReceipt 直接返回凭证 PDF，省掉「先创建再下载」两步。
// 只想拿一份文件的客户端走这条；需要可分享链接的走 ExportPaymentBill。
func (h *Handler) DownloadPaymentReceipt(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	data, filename, err := h.payment.RenderUserOrderReceipt(c.Request.Context(), session, c.Param("orderNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

// EmailPaymentReceipt 把凭证寄到账号绑定的邮箱。
//
// 收件地址**不接受请求指定**：允许任意填写等于把平台变成一个能带 PDF 附件的
// 转发器。要寄给别人，用户自己转发即可。
func (h *Handler) EmailPaymentReceipt(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req PaymentBillExportRequest
	_ = bind(c, &req)
	result, err := h.payment.EmailUserOrderReceipt(c.Request.Context(), session, c.Param("orderNo"), receiptOptions(c, req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

// DownloadSignedPaymentReceipt 凭邮件里的签名链接下载凭证，**无需登录**。
//
// 这条路由刻意放在鉴权组之外：邮件客户端里没有会话，需要登录的链接等于打不开。
// 授权来自「128 位随机凭证标识 + 服务端签名 + 有限有效期」，与密码重置链接同一套模型。
func (h *Handler) DownloadSignedPaymentReceipt(c *gin.Context) {
	appID, err := strconv.ParseInt(strings.TrimSpace(c.Param("appid")), 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusNotFound, 40475, "账单不存在")
		return
	}
	expires, _ := strconv.ParseInt(strings.TrimSpace(c.Query("expires")), 10, 64)
	data, filename, err := h.payment.DownloadSignedReceipt(c.Request.Context(), appID,
		c.Param("billId"), expires, c.Query("token"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writePDF(c, filename, data)
}

// AdminPaymentOrderReceiptEmail 管理端把凭证寄到指定邮箱（客服代发 / 补发到财务）。
func (h *Handler) AdminPaymentOrderReceiptEmail(c *gin.Context) {
	var req AdminPaymentReceiptEmailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.payment.EmailAppOrderReceipt(c.Request.Context(), req.AppID, req.OrderNo, req.Email, paymentdomain.ReceiptOptions{
		Locale:         strings.TrimSpace(req.Locale),
		AcceptLanguage: c.GetHeader("Accept-Language"),
		DocumentType:   strings.TrimSpace(req.DocumentType),
		Timezone:       strings.TrimSpace(req.Timezone),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发送成功", result)
}

// AdminPaymentReceiptEmailRequest 管理端凭证寄送请求
type AdminPaymentReceiptEmailRequest struct {
	AppID   int64  `json:"appid" form:"appid" binding:"required"`
	OrderNo string `json:"order_no" form:"order_no" binding:"required"`
	// Email 收件地址。管理端可指定任意地址，因此这条路径必须经过审计中间件。
	Email        string `json:"email" form:"email" binding:"required"`
	Locale       string `json:"locale" form:"locale"`
	DocumentType string `json:"documentType" form:"documentType"`
	Timezone     string `json:"timezone" form:"timezone"`
}

// PaymentReceiptOptions 下发凭证可选语言与当前环境的字体能力。
// 客户端据此渲染语言选择器，并在缺中日韩字体时提前提示，而不是等下载到一份英文的。
func (h *Handler) PaymentReceiptOptions(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.payment.ReceiptCapability())
}

// receiptOptions 组装凭证选项。语言的次选来源是 Accept-Language ——
// 请求头在这里读，服务层只负责按优先级挑，不必认识 HTTP。
func receiptOptions(c *gin.Context, req PaymentBillExportRequest) paymentdomain.ReceiptOptions {
	return paymentdomain.ReceiptOptions{
		Locale:         strings.TrimSpace(req.Locale),
		AcceptLanguage: c.GetHeader("Accept-Language"),
		DocumentType:   strings.TrimSpace(req.DocumentType),
		Timezone:       strings.TrimSpace(req.Timezone),
		TTL:            time.Duration(req.ExpireMinutes) * time.Minute,
	}
}

// writePDF 输出 PDF。文件名恒为 ASCII（见 service.receiptFileName），
// 因此不需要 RFC 5987 的 filename* 扩展。
func writePDF(c *gin.Context, filename string, data []byte) {
	c.Header("Content-Type", "application/pdf")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/pdf", data)
}

func (h *Handler) QueryEpayOrder(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if _, err := h.payment.GetUserOrder(c.Request.Context(), session, c.Param("orderNo")); err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.payment.QueryEpayRemoteOrder(c.Request.Context(), c.Param("orderNo"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) EpayCallback(c *gin.Context) {
	_ = c.Request.ParseForm()
	data := map[string]string{}
	for key, values := range c.Request.Form {
		if len(values) > 0 {
			data[key] = values[0]
		}
	}
	result, err := h.payment.HandleEpayCallback(c.Request.Context(), data, c.Request.Method, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result.Paid {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "fail")
}

func (h *Handler) PaymentCallback(c *gin.Context) {
	method := c.Param("method")
	// 先完整读取原始报文（JSON Webhook 验签必须基于逐字节原文），再恢复 Body 供表单解析
	rawBody, _ := io.ReadAll(io.LimitReader(c.Request.Body, 4<<20))
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))
	_ = c.Request.ParseForm()
	data := map[string]string{}
	for key, values := range c.Request.Form {
		if len(values) > 0 {
			data[key] = values[0]
		}
	}
	// 仅对非表单回调（Stripe / 微信 / PayPal 的 JSON Webhook）注入保留键，
	// 避免污染易支付系表单验签的参数集合
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if len(rawBody) > 0 && !strings.Contains(contentType, "form") {
		data[service.CallbackRawBodyKey] = string(rawBody)
		for _, headerKey := range service.CallbackSignatureHeaders {
			if v := c.GetHeader(headerKey); v != "" {
				data[service.CallbackHeaderPrefix+strings.ToLower(headerKey)] = v
			}
		}
	}
	// 路径段应用标识（微信等通知地址禁带查询参数的渠道）：/callback/:method/:appid
	if appID := strings.TrimSpace(c.Param("appid")); appID != "" {
		data["__app_id"] = appID
	}
	result, err := h.payment.HandleCallback(c.Request.Context(), method, data, c.Request.Method, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result.Paid {
		c.String(http.StatusOK, "success")
		return
	}
	c.String(http.StatusOK, "fail")
}

func (h *Handler) PaymentMethods(c *gin.Context) {
	methods := h.payment.AvailableMethods()
	response.Success(c, 200, "获取成功", methods)
}

func (h *Handler) WorkflowList(c *gin.Context) {
	var req WorkflowListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.List(c.Request.Context(), req.AppID, workflowdomain.ListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, Category: req.Category, Keyword: req.Keyword})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowCreate(c *gin.Context) { h.workflowSave(c, 0) }

func (h *Handler) WorkflowUpdate(c *gin.Context) {
	var req WorkflowSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.workflowSaveWithReq(c, req, req.WorkflowID)
}

func (h *Handler) workflowSave(c *gin.Context, id int64) {
	var req WorkflowSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.workflowSaveWithReq(c, req, id)
}

func (h *Handler) workflowSaveWithReq(c *gin.Context, req WorkflowSaveRequest, id int64) {
	definition, err := toWorkflowDefinition(req.Definition)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "工作流定义格式错误")
		return
	}
	item, err := h.workflow.Save(c.Request.Context(), workflowdomain.WorkflowMutation{
		ID:            id,
		AppID:         req.AppID,
		Name:          maybeString(req.Name),
		Description:   maybeString(req.Description),
		Category:      maybeString(req.Category),
		Status:        maybeString(req.Status),
		Definition:    definition,
		TriggerConfig: req.TriggerConfig,
		UIConfig:      req.UIConfig,
		Permissions:   req.Permissions,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) WorkflowDetail(c *gin.Context) {
	var req WorkflowDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.Detail(c.Request.Context(), req.AppID, req.WorkflowID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowDelete(c *gin.Context) {
	var req WorkflowDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.workflow.Delete(c.Request.Context(), req.AppID, req.WorkflowID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) WorkflowStart(c *gin.Context) {
	var req WorkflowStartRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	workflowID := req.WorkflowID
	if workflowID == 0 {
		workflowID = req.WorkflowID2
	}
	item, err := h.workflow.Start(c.Request.Context(), req.AppID, workflowID, nil, req.InputData, req.InstanceName, req.Priority)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "启动成功", item)
}

func (h *Handler) WorkflowInstances(c *gin.Context) {
	var req WorkflowInstancesRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	workflowID := req.WorkflowID
	if workflowID == 0 {
		workflowID = req.WorkflowID2
	}
	result, err := h.workflow.Instances(c.Request.Context(), req.AppID, workflowdomain.InstanceQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), WorkflowID: workflowID, Status: req.Status})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowInstanceDetail(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.InstanceDetail(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowInstancePause(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.PauseInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "暂停成功", item)
}

func (h *Handler) WorkflowInstanceResume(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.ResumeInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "恢复成功", item)
}

func (h *Handler) WorkflowInstanceCancel(c *gin.Context) {
	var req WorkflowInstanceDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instanceID := req.InstanceID
	if instanceID == 0 {
		instanceID = req.InstanceID2
	}
	item, err := h.workflow.CancelInstance(c.Request.Context(), req.AppID, instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "取消成功", item)
}

func (h *Handler) WorkflowTasksTodo(c *gin.Context) {
	var req WorkflowTaskQueryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.UserTasks(c.Request.Context(), req.AppID, workflowdomain.TaskQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), UserID: req.UserID, Status: req.Status, Priority: req.Priority})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowTaskDetail(c *gin.Context) {
	var req WorkflowTaskDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.TaskDetail(c.Request.Context(), req.AppID, req.TaskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) WorkflowTaskComplete(c *gin.Context) {
	var req WorkflowTaskCompleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	taskID := req.TaskID
	if taskID == 0 {
		taskID = req.TaskID2
	}
	item, err := h.workflow.CompleteTask(c.Request.Context(), req.AppID, taskID, req.Output, req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "处理成功", item)
}

func (h *Handler) WorkflowTaskAssign(c *gin.Context) {
	var req WorkflowTaskAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.AssignTask(c.Request.Context(), req.AppID, req.TaskID, req.AssignedTo, req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "分配成功", item)
}

func (h *Handler) WorkflowTaskHistory(c *gin.Context) {
	var req WorkflowTaskDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.workflow.TaskHistory(c.Request.Context(), req.AppID, req.TaskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) WorkflowTemplates(c *gin.Context) {
	var req WorkflowTemplatesRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.Templates(c.Request.Context(), req.AppID, req.Category, normalizePage(req.Page), normalizeLimit(req.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowCreateFromTemplate(c *gin.Context) {
	var req WorkflowCreateFromTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.CreateFromTemplate(c.Request.Context(), req.AppID, req.TemplateID, req.Name, req.Description, 0)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) WorkflowSaveAsTemplate(c *gin.Context) {
	var req WorkflowSaveAsTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.workflow.SaveAsTemplate(c.Request.Context(), req.AppID, req.WorkflowID, req.TemplateName, req.TemplateDescription, req.Category, req.IsPublic)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) WorkflowValidate(c *gin.Context) {
	var req WorkflowValidateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	definition, err := toWorkflowDefinition(req.Definition)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "工作流定义格式错误")
		return
	}
	if err := h.workflow.ValidateDefinition(*definition); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "校验通过", gin.H{"valid": true})
}

func (h *Handler) WorkflowNodeTypes(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.workflow.NodeTypes())
}

func (h *Handler) WorkflowStatistics(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.workflow.Statistics(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) WorkflowLogs(c *gin.Context) {
	var req WorkflowLogsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.workflow.Logs(c.Request.Context(), req.AppID, req.WorkflowID, req.InstanceID, req.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) WorkflowEngineStatus(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.workflow.EngineStatus())
}

func normalizeEmailExpire(value int) int {
	if value <= 0 {
		return 5
	}
	if value > 60 {
		return 60
	}
	return value
}

func toWorkflowDefinition(input map[string]any) (*workflowdomain.Definition, error) {
	if input == nil {
		return &workflowdomain.Definition{}, nil
	}
	var item workflowdomain.Definition
	if err := decodeJSON(input, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func decodeJSON(input any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
