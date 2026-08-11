package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Zeabur webhook 的请求头。
const (
	ZeaburWebhookEventHeader     = "X-ZSend-Event"
	ZeaburWebhookTimestampHeader = "X-ZSend-Timestamp"
	ZeaburWebhookSignatureHeader = "X-ZSend-Signature"
)

// zeaburWebhookTolerance 是签名时间戳的容忍窗口。
// 签名消息里带了时间戳，卡住窗口即可让被截获的回调无法在事后重放。
const zeaburWebhookTolerance = 5 * time.Minute

// ZeaburWebhookRequest 是传输层递交上来的原始回调。
// Body 必须是**未经解析的原始字节** —— 签名覆盖的是原文，
// 任何重新序列化（键序、空白、转义差异）都会让验签失败。
type ZeaburWebhookRequest struct {
	AppID      int64
	ConfigName string
	Event      string
	Timestamp  string
	Signature  string
	Body       []byte
}

// ZeaburWebhookResult 是回调处理结果，用于给控制台/日志一个可观测的回执。
type ZeaburWebhookResult struct {
	Event    string `json:"event"`
	Matched  bool   `json:"matched"`
	Status   string `json:"status,omitempty"`
	EmailID  string `json:"emailId,omitempty"`
	Ignored  bool   `json:"ignored,omitempty"`
	Received bool   `json:"received"`
}

// HandleZeaburWebhook 校验并落地一次 Zeabur 投递回执。
//
// 只有走完「找到配置 → 取出密钥 → 验签 → 时间窗口」四步才会碰数据库；
// 任一步失败都以 4xx 返回，让 Zeabur 侧的重试与告警能如实反映问题。
func (s *EmailService) HandleZeaburWebhook(ctx context.Context, request ZeaburWebhookRequest) (*ZeaburWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName)
	if err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(config.Zeabur.WebhookSecret)
	if secret == "" {
		return nil, apperrors.New(40073, http.StatusPreconditionFailed,
			"该邮件配置尚未设置 Zeabur Webhook 密钥，无法校验回调来源")
	}
	if err := verifyZeaburWebhookSignature(secret, request.Timestamp, request.Signature, request.Body); err != nil {
		s.log.Warn("zeabur webhook signature rejected",
			zap.Int64("appid", request.AppID), zap.String("config", config.Name), zap.Error(err))
		return nil, err
	}

	var event emaildomain.WebhookEvent
	if err := json.Unmarshal(request.Body, &event); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "Zeabur 回调报文解析失败")
	}
	// 事件名以请求头为准：头部参与了签名，body 里的 event 字段没有单独的完整性保证。
	eventName := strings.ToLower(strings.TrimSpace(request.Event))
	if eventName == "" {
		eventName = strings.ToLower(strings.TrimSpace(event.Event))
	}

	update, ok := buildZeaburDeliveryUpdate(eventName, event)
	if !ok {
		// 未知事件不算错误：Zeabur 之后新增事件类型时，这里静默放行比 4xx 触发无谓重试更合适。
		s.log.Info("zeabur webhook event ignored",
			zap.Int64("appid", request.AppID), zap.String("event", eventName))
		return &ZeaburWebhookResult{Event: eventName, Received: true, Ignored: true}, nil
	}
	update.AppID = request.AppID

	matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
	if err != nil {
		return nil, err
	}
	if !matched {
		// 常见于：webhook 早于本地留痕落库，或该邮件由别的实例/环境发出。
		s.log.Info("zeabur webhook has no matching delivery",
			zap.Int64("appid", request.AppID), zap.String("event", eventName),
			zap.String("emailId", event.Email.ID))
	}
	return &ZeaburWebhookResult{
		Event:    eventName,
		Matched:  matched,
		Status:   update.Status,
		EmailID:  event.Email.ID,
		Received: true,
	}, nil
}

// resolveWebhookConfig 定位回调对应的邮件配置。
// 指定了配置名就精确匹配，否则回落到该应用的默认配置。
func (s *EmailService) resolveWebhookConfig(ctx context.Context, appID int64, configName string) (*emaildomain.Config, error) {
	var (
		item *emaildomain.Config
		err  error
	)
	if name := strings.TrimSpace(configName); name != "" {
		item, err = s.pg.GetEmailConfigByName(ctx, appID, name)
	} else {
		item, err = s.pg.GetDefaultEmailConfig(ctx, appID)
	}
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40460, http.StatusNotFound, "邮件配置不存在")
	}
	if normalizeEmailProvider(item.Provider) != emaildomain.ProviderZeabur {
		return nil, apperrors.New(40075, http.StatusBadRequest, "该邮件配置的服务商不是 zeabur，不接受 Zeabur 回调")
	}
	s.decryptEmailSecrets(item)
	return item, nil
}

// verifyZeaburWebhookSignature 按 Zeabur 规格验签：HMAC-SHA256 over "{timestamp}.{body}"，
// 头部形如 sha256=<hex>。比较必须是恒定时间的，否则签名可被逐字节试探出来。
func verifyZeaburWebhookSignature(secret string, timestamp string, signature string, body []byte) error {
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	if timestamp == "" || signature == "" {
		return apperrors.New(40176, http.StatusUnauthorized, "Zeabur 回调缺少签名或时间戳请求头")
	}
	if err := checkZeaburTimestamp(timestamp); err != nil {
		return err
	}

	provided := signature
	if index := strings.Index(provided, "="); index >= 0 {
		if !strings.EqualFold(strings.TrimSpace(provided[:index]), "sha256") {
			return apperrors.New(40176, http.StatusUnauthorized, "Zeabur 回调签名算法不受支持")
		}
		provided = strings.TrimSpace(provided[index+1:])
	}
	providedBytes, err := hex.DecodeString(provided)
	if err != nil {
		return apperrors.New(40176, http.StatusUnauthorized, "Zeabur 回调签名格式非法")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	if !hmac.Equal(providedBytes, mac.Sum(nil)) {
		return apperrors.New(40176, http.StatusUnauthorized, "Zeabur 回调签名校验失败，请核对 Webhook 密钥")
	}
	return nil
}

// checkZeaburTimestamp 同时接受 Unix 秒和 RFC3339，前者是文档规格，后者是容错。
func checkZeaburTimestamp(timestamp string) error {
	var issuedAt time.Time
	if seconds, err := strconv.ParseInt(timestamp, 10, 64); err == nil {
		issuedAt = time.Unix(seconds, 0)
	} else if parsed, parseErr := time.Parse(time.RFC3339, timestamp); parseErr == nil {
		issuedAt = parsed
	} else {
		return apperrors.New(40176, http.StatusUnauthorized, "Zeabur 回调时间戳格式非法")
	}

	drift := timeutil.Now().Sub(issuedAt)
	if drift < 0 {
		drift = -drift
	}
	if drift > zeaburWebhookTolerance {
		return apperrors.New(40176, http.StatusUnauthorized,
			fmt.Sprintf("Zeabur 回调时间戳超出 %.0f 分钟容忍窗口，已按重放攻击拒绝", zeaburWebhookTolerance.Minutes()))
	}
	return nil
}

// buildZeaburDeliveryUpdate 把事件翻译成一次状态推进。
// open / click 只累计计数、不动主状态 —— 已投递的邮件被打开多少次都不该改变「是否送达」。
func buildZeaburDeliveryUpdate(eventName string, event emaildomain.WebhookEvent) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: strings.TrimSpace(event.Email.ID),
		Event:             eventName,
		Payload:           event.Data,
		OccurredAt:        parseZeaburEventTime(event.Timestamp),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}

	switch eventName {
	case emaildomain.EventSend:
		update.Status = emaildomain.DeliveryStatusSent
	case emaildomain.EventDelivery:
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
	case emaildomain.EventBounce:
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = describeZeaburFailure(event.Data, "邮件被退回")
	case emaildomain.EventComplaint:
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = describeZeaburFailure(event.Data, "收件人投诉为垃圾邮件")
	case emaildomain.EventReject:
		update.Status = emaildomain.DeliveryStatusRejected
		update.ErrorMessage = describeZeaburFailure(event.Data, "邮件被服务商拒绝发送")
	case emaildomain.EventOpen:
		update.IncrementOpen = true
	case emaildomain.EventClick:
		update.IncrementClick = true
	default:
		return update, false
	}
	return update, true
}

func parseZeaburEventTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return timeutil.Now()
}

// describeZeaburFailure 从事件 data 里挑出最能说明原因的字段拼成一句话。
func describeZeaburFailure(data map[string]any, fallback string) string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"bounce_type", "bounce_sub_type", "complaint_feedback_type", "reason", "smtp_response", "diagnostic_code"} {
		value, ok := data[key]
		if !ok {
			continue
		}
		if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" {
			parts = append(parts, key+"="+text)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return fallback
	}
	return fallback + "（" + strings.Join(parts, ", ") + "）"
}
