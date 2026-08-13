package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	emaildomain "aegis/internal/domain/email"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 邮件配置管理面。
//
// 两套作用域共用同一批服务层方法，只在「appid 从哪来」上不同：
//   - 应用级：`/api/admin/app/email-config/*`，appid 由请求体携带
//   - 平台级：`/api/admin/system/email/*`，appid 恒为 emaildomain.PlatformAppID
//
// 平台级刻意不接受请求体里的 appid：接受它就得防「传了别的 appid 会怎样」，
// 而那正是应用级与平台级混用同一组路由时最容易漏掉的一处越权。

// ── 服务商目录 ──

// EmailProviderCatalog 下发全部邮件服务商的自述。
//
// 静态目录，不含任何租户数据与凭据，因此两个作用域共用同一个 handler。
// 控制台据此渲染服务商卡片与配置表单 —— 新增一家服务商时前端零改动。
func (h *Handler) EmailProviderCatalog(c *gin.Context) {
	response.Success(c, 200, "获取成功", gin.H{
		"providers":  h.email.ProviderCatalog(),
		"groups":     emaildomain.GroupNames,
		"categories": emaildomain.CategoryNames,
	})
}

// ── 应用级 ──

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
	mutation := emaildomain.ConfigMutation{
		ID:              id,
		AppID:           req.AppID,
		Name:            maybeString(req.Name),
		Provider:        maybeString(req.Provider),
		Enabled:         req.Enabled,
		IsDefault:       req.IsDefault,
		Description:     maybeString(req.Description),
		Settings:        copyStringMap(req.Settings),
		Secrets:         copyStringMap(req.Secrets),
		ClearSecrets:    req.ClearSecrets,
		ReplaceSettings: req.ReplaceSettings,
	}
	applyLegacyEmailFields(&mutation, req)

	item, err := h.email.Save(c.Request.Context(), mutation)
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

// applyLegacyEmailFields 把重构前的扁平字段折进通用字段袋。
//
// 只在对应字段**确实出现**时才写入：旧客户端保存一个 zeabur 配置时不会带
// smtp_* 字段，无条件写入会把 host / port 覆盖成空值与 0。
// 新客户端一律走 settings / secrets，走不到这里。
func applyLegacyEmailFields(mutation *emaildomain.ConfigMutation, req AdminEmailConfigSaveRequest) {
	set := func(key string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if mutation.Settings == nil {
			mutation.Settings = map[string]string{}
		}
		if _, exists := mutation.Settings[key]; !exists {
			mutation.Settings[key] = value
		}
	}
	setSecret := func(key string, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if mutation.Secrets == nil {
			mutation.Secrets = map[string]string{}
		}
		if _, exists := mutation.Secrets[key]; !exists {
			mutation.Secrets[key] = value
		}
	}

	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == emaildomain.ProviderZeabur {
		set(emaildomain.KeyFromAddress, req.ZeaburFrom)
		set(emaildomain.KeyFromName, req.ZeaburFromName)
		set(emaildomain.KeyReplyTo, req.ZeaburReplyTo)
		set("baseUrl", req.ZeaburBaseURL)
		if len(req.ZeaburTags) > 0 {
			if encoded, err := json.Marshal(req.ZeaburTags); err == nil {
				set(emaildomain.KeyTags, string(encoded))
			}
		}
		setSecret("apiKey", req.ZeaburAPIKey)
		setSecret(emaildomain.KeyWebhookSecret, req.ZeaburWebhookSecret)
		return
	}
	if provider != "" && provider != emaildomain.ProviderSMTP {
		return
	}
	set("host", req.SMTPHost)
	if req.SMTPPort > 0 {
		set("port", strconv.Itoa(req.SMTPPort))
	}
	set("username", req.SMTPUser)
	set(emaildomain.KeyFromAddress, req.SMTPFrom)
	set(emaildomain.KeyFromName, req.SMTPFromName)
	set(emaildomain.KeyReplyTo, req.SMTPReplyTo)
	setSecret("password", req.SMTPPassword)
	if req.SMTPTLS != nil {
		encryption := emaildomain.SMTPEncryptionSTARTTLS
		if *req.SMTPTLS {
			encryption = emaildomain.SMTPEncryptionSSL
		}
		set("encryption", encryption)
	}
	if req.SMTPInsecure != nil && *req.SMTPInsecure {
		set("insecureSkipVerify", "true")
	}
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
		Purpose:  req.Purpose,
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

// AdminEmailChannel 回答「这个应用现在实际用哪条通道发信」。
//
// 应用没有自己的配置、正在借用平台共享通道时，这里的 inherited 为 true ——
// 控制台据此显示「当前继承自平台通道 X」。不说的话，管理员会对着一个空的
// 邮件配置页纳闷验证码是怎么发出去的。
func (h *Handler) AdminEmailChannel(c *gin.Context) {
	var req AdminEmailChannelRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	resolution, err := h.email.ResolveChannel(c.Request.Context(), req.AppID, req.ConfigName)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", resolution)
}

// AdminEmailDeliveryStats 应用级投递概况。
func (h *Handler) AdminEmailDeliveryStats(c *gin.Context) {
	var req AdminEmailConfigListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.email.DeliveryStats(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// ── 平台级 ──

func (h *Handler) AdminPlatformEmailConfigList(c *gin.Context) {
	items, err := h.email.ListConfigs(c.Request.Context(), emaildomain.PlatformAppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminPlatformEmailConfigDetail(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	item, err := h.email.Detail(c.Request.Context(), emaildomain.PlatformAppID, configID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminPlatformEmailConfigCreate(c *gin.Context) {
	h.savePlatformEmailConfig(c, 0)
}

func (h *Handler) AdminPlatformEmailConfigUpdate(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	h.savePlatformEmailConfig(c, configID)
}

func (h *Handler) savePlatformEmailConfig(c *gin.Context, id int64) {
	var req AdminPlatformEmailConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.email.Save(c.Request.Context(), emaildomain.ConfigMutation{
		ID:              id,
		AppID:           emaildomain.PlatformAppID,
		Name:            maybeString(req.Name),
		Provider:        maybeString(req.Provider),
		Enabled:         req.Enabled,
		IsDefault:       req.IsDefault,
		Shared:          req.Shared,
		Description:     maybeString(req.Description),
		Settings:        copyStringMap(req.Settings),
		Secrets:         copyStringMap(req.Secrets),
		ClearSecrets:    req.ClearSecrets,
		ReplaceSettings: req.ReplaceSettings,
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

func (h *Handler) AdminPlatformEmailConfigDelete(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	if err := h.email.Delete(c.Request.Context(), emaildomain.PlatformAppID, configID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminPlatformEmailConfigTest(c *gin.Context) {
	configID, ok := parseEmailConfigID(c)
	if !ok {
		return
	}
	var req AdminPlatformEmailTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.email.TestConfig(c.Request.Context(), emaildomain.PlatformAppID, configID, req.TestEmail)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试成功", result)
}

func (h *Handler) AdminPlatformEmailDeliveryList(c *gin.Context) {
	page, err := h.email.ListDeliveries(c.Request.Context(), emaildomain.DeliveryQuery{
		AppID:    emaildomain.PlatformAppID,
		ConfigID: parseInt64Query(c, "configId"),
		Status:   strings.TrimSpace(c.Query("status")),
		Provider: strings.TrimSpace(c.Query("provider")),
		Purpose:  strings.TrimSpace(c.Query("purpose")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Page:     int(parseInt64Query(c, "page")),
		PageSize: int(parseInt64Query(c, "pageSize")),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", page)
}

func (h *Handler) AdminPlatformEmailDeliveryStats(c *gin.Context) {
	stats, err := h.email.DeliveryStats(c.Request.Context(), emaildomain.PlatformAppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// AdminPlatformEmailChannel 平台级当前生效的通道。
func (h *Handler) AdminPlatformEmailChannel(c *gin.Context) {
	resolution, err := h.email.ResolveChannel(c.Request.Context(), emaildomain.PlatformAppID, strings.TrimSpace(c.Query("configName")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", resolution)
}

func parseEmailConfigID(c *gin.Context) (int64, bool) {
	configID, err := strconv.ParseInt(strings.TrimSpace(c.Param("configId")), 10, 64)
	if err != nil || configID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "邮件配置标识非法")
		return 0, false
	}
	return configID, true
}

func parseInt64Query(c *gin.Context, key string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(c.Query(key)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func copyStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// ── 投递回执 ──

// ZeaburEmailWebhook 接收 Zeabur Email 的投递回执。
//
// 该路由是公开的（服务商侧不可能携带管理员令牌），真正的准入是 HMAC 签名校验，
// 由 EmailService 用该配置里的 Webhook 密钥完成。
func (h *Handler) ZeaburEmailWebhook(c *gin.Context) {
	appID, ok := parseEmailWebhookScope(c)
	if !ok {
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

// SESEmailWebhook 接收 AWS SES 经 SNS 推送的投递回执（含订阅确认）。
func (h *Handler) SESEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleSESWebhook)
}

// ResendEmailWebhook 接收 Resend 的投递回执。
func (h *Handler) ResendEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleResendWebhook)
}

// SendGridEmailWebhook 接收 SendGrid Event Webhook 的一批投递回执。
func (h *Handler) SendGridEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleSendGridWebhook)
}

// MailgunEmailWebhook 接收 Mailgun 的投递回执（HMAC 验签 + nonce 防重放）。
func (h *Handler) MailgunEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleMailgunWebhook)
}

// PostmarkEmailWebhook 接收 Postmark 的投递回执。
// Postmark 不签名，准入靠地址里的回调令牌（也接受 Basic Auth 的密码位）。
func (h *Handler) PostmarkEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandlePostmarkWebhook)
}

// AliyunEmailWebhook 接收阿里云邮件推送的回执。同样靠回调令牌准入。
func (h *Handler) AliyunEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleAliyunWebhook)
}

// TencentEmailWebhook 接收腾讯云 SES 的回执。同样靠回调令牌准入。
func (h *Handler) TencentEmailWebhook(c *gin.Context) {
	h.handleProviderEmailWebhook(c, h.email.HandleTencentWebhook)
}

// providerWebhookHandler 三家服务商的回执处理器共用的形状。
type providerWebhookHandler func(ctx context.Context, request service.ProviderWebhookRequest) (*service.ProviderWebhookResult, error)

func (h *Handler) handleProviderEmailWebhook(c *gin.Context, handle providerWebhookHandler) {
	appID, ok := parseEmailWebhookScope(c)
	if !ok {
		return
	}
	body, err := c.GetRawData()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取回调报文失败")
		return
	}
	result, err := handle(c.Request.Context(), service.ProviderWebhookRequest{
		AppID:      appID,
		ConfigName: strings.TrimSpace(c.Param("config")),
		Headers:    c.Request.Header,
		// 不签名的那三家把回调令牌放在 query 里，因此必须原样带下去。
		Query: c.Request.URL.Query(),
		Body:  body,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "接收成功", result)
}

// parseEmailWebhookScope 解析回调地址里的作用域段。
//
// 字面量 `platform` 表示平台级通道。用关键字而不是数字 0：
// 这个地址要人工填进服务商控制台，`/webhook/ses/0` 看起来像个占位符没替换掉，
// 而填错的后果是回执永远匹配不到留痕，且不报错。
func parseEmailWebhookScope(c *gin.Context) (int64, bool) {
	raw := strings.TrimSpace(c.Param("scope"))
	if raw == "" {
		raw = strings.TrimSpace(c.Param("appid"))
	}
	if strings.EqualFold(raw, "platform") {
		return emaildomain.PlatformAppID, true
	}
	appID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || appID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "应用标识非法；平台级通道请使用 platform")
		return 0, false
	}
	return appID, true
}
