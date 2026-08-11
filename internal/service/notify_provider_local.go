package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	notificationdomain "aegis/internal/domain/notification"
	notifydomain "aegis/internal/domain/notify"
	apperrors "aegis/pkg/errors"
)

// 通用 Webhook + 三个"平台内"渠道（邮件 / 站内信 / 实时推送）。
// 后三者不走 HTTP，而是复用平台既有服务，因此统一出口对业务侧才是真正"一处发、处处到"。

// ─────────────── 通用 Webhook ───────────────

type genericWebhookProvider struct{}

func (p *genericWebhookProvider) Kind() string { return notifydomain.KindWebhook }

func (p *genericWebhookProvider) Validate(cfg map[string]any, secret string) error {
	target := cfgString(cfg, "url")
	if target == "" {
		return apperrors.New(40000, http.StatusBadRequest, "Webhook 目标地址不能为空")
	}
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return apperrors.New(40000, http.StatusBadRequest, "Webhook 地址必须以 http(s):// 开头")
	}
	return nil
}

func (p *genericWebhookProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	target := cfgString(channel.Config, "url")
	if target == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "Webhook 渠道未配置目标地址")
	}
	method := strings.ToUpper(cfgString(channel.Config, "method"))
	if method != http.MethodPut {
		method = http.MethodPost
	}

	event := msg.Event
	payload := map[string]any{
		"event":      event.Key,
		"level":      event.Level,
		"title":      msg.Title,
		"summary":    msg.Body,
		"link":       event.Link,
		"appid":      event.AppID,
		"appName":    event.AppName,
		"priority":   event.Priority,
		"resource":   event.Resource,
		"resourceId": event.ResourceID,
		"fields":     event.Fields,
		"vars":       event.Vars,
		"timestamp":  time.Now().Unix(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "Aegis-Notify/1.0")
	req.Header.Set("X-Aegis-Event", event.Key)
	for key, value := range parseHeaderLines(cfgString(channel.Config, "headers")) {
		req.Header.Set(key, value)
	}
	// 配置了密钥就带 HMAC 签名，接收方可据此验真
	if secret := strings.TrimSpace(channel.Secret); secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp))
		mac.Write([]byte("."))
		mac.Write(body)
		req.Header.Set("X-Aegis-Timestamp", timestamp)
		req.Header.Set("X-Aegis-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := hub.httpClient.Do(req)
	result := &notifydomain.Result{RequestSnippet: string(body)}
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	result.ResponseSnippet = string(respBody)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("Webhook 返回 HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody)))
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// parseHeaderLines 解析「每行一条 Key: Value」的附加请求头配置。
func parseHeaderLines(raw string) map[string]string {
	headers := make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" && value != "" {
			headers[key] = value
		}
	}
	return headers
}

// ─────────────── 邮件 ───────────────

type emailNotifyProvider struct{}

func (p *emailNotifyProvider) Kind() string { return notifydomain.KindEmail }

func (p *emailNotifyProvider) Validate(cfg map[string]any, secret string) error {
	// 收件人可以为空：留空时按事件自带的收件目标发送
	return nil
}

func (p *emailNotifyProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	if hub.email == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "邮件服务未启用")
	}
	recipients := cfgStrings(channel.Config, "to")
	if len(recipients) == 0 {
		recipients = append(recipients, msg.Event.Recipients.Emails...)
	}
	// 管理员收件人：按 ID 反查账号邮箱
	if len(msg.Event.Recipients.AdminIDs) > 0 {
		contacts, err := hub.pg.ListAdminContactsByIDs(ctx, msg.Event.Recipients.AdminIDs)
		if err == nil {
			for _, contact := range contacts {
				if strings.TrimSpace(contact.Email) != "" {
					recipients = append(recipients, contact.Email)
				}
			}
		}
	}
	recipients = dedupeStrings(recipients)
	if len(recipients) == 0 {
		return &notifydomain.Result{Status: notifydomain.DeliverySkipped}, nil
	}

	appID := channel.AppID
	if appID == 0 {
		appID = msg.Event.AppID
	}
	html := notifyEmailHTML(msg)
	var lastErr error
	sent := 0
	for _, to := range recipients {
		if err := hub.email.SendNotificationEmail(ctx, appID, to, msg.Title, html, cfgString(channel.Config, "configName")); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	result := &notifydomain.Result{
		RequestSnippet: fmt.Sprintf("to=%s subject=%s", strings.Join(recipients, ","), msg.Title),
	}
	if sent == 0 && lastErr != nil {
		return result, lastErr
	}
	result.Status = notifydomain.DeliverySuccess
	result.ResponseSnippet = fmt.Sprintf("已发送 %d/%d 封", sent, len(recipients))
	return result, nil
}

// notifyEmailHTML 通知邮件正文（复用邮件模板的排版骨架）。
func notifyEmailHTML(msg *notifydomain.Message) string {
	var body strings.Builder
	if strings.TrimSpace(msg.Body) != "" {
		body.WriteString("<p>")
		body.WriteString(htmlEscapeText(msg.Body))
		body.WriteString("</p>")
	}
	if len(msg.Event.Fields) > 0 {
		body.WriteString(`<table style="width:100%;border-collapse:collapse;margin:16px 0">`)
		for _, field := range msg.Event.Fields {
			body.WriteString(`<tr><td style="padding:6px 12px;color:#71717a;white-space:nowrap">`)
			body.WriteString(htmlEscapeText(field.Label))
			body.WriteString(`</td><td style="padding:6px 12px;color:#18181b">`)
			body.WriteString(htmlEscapeText(field.Value))
			body.WriteString(`</td></tr>`)
		}
		body.WriteString(`</table>`)
	}
	if link := strings.TrimSpace(msg.Event.Link); link != "" {
		body.WriteString(`<p><a href="` + htmlEscapeText(link) + `" style="color:#2563eb">查看详情</a></p>`)
	}
	return renderEmailLayout("Aegis", "通知", msg.Title, msg.Body, body.String(), "本邮件由 Aegis 统一通知出口自动发送。")
}

func htmlEscapeText(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// ─────────────── 站内信 ───────────────

// inAppNotifyProvider 站内信。
//
// 会同时向两个**互相独立**的收件箱投递：
//   - Recipients.UserIDs  → notifications        （应用用户，外键 users）
//   - Recipients.AdminIDs → admin_notifications  （管理员，外键 admin_accounts）
//
// 两张表分开是必须的：admin_id 与 user_id 是两套主键空间，
// 把管理员 ID 写进 notifications 要么违反外键，要么静默命中同号的应用用户。
type inAppNotifyProvider struct{}

func (p *inAppNotifyProvider) Kind() string { return notifydomain.KindInApp }

func (p *inAppNotifyProvider) Validate(cfg map[string]any, secret string) error { return nil }

func (p *inAppNotifyProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	event := msg.Event
	userIDs := dedupeInt64(event.Recipients.UserIDs)
	adminIDs := dedupeInt64(event.Recipients.AdminIDs)
	if len(userIDs) == 0 && len(adminIDs) == 0 {
		return &notifydomain.Result{Status: notifydomain.DeliverySkipped,
			ResponseSnippet: "事件没有站内信收件人"}, nil
	}

	notificationType := cfgString(channel.Config, "type")
	if notificationType == "" {
		notificationType = strings.TrimSpace(event.Type)
	}
	if notificationType == "" {
		notificationType = "system"
	}

	var (
		lastErr    error
		userSent   int
		adminSent  int64
		skipReason []string
	)

	// ── 应用用户侧 ──
	if len(userIDs) > 0 {
		if hub.notifications == nil {
			skipReason = append(skipReason, "站内信服务未启用")
		} else {
			appID := event.AppID
			if appID == 0 {
				appID = channel.AppID
			}
			// 用户侧只带 UserLink，绝不下发控制台路径
			metadata := inAppMetadata(event, strings.TrimSpace(event.UserLink))
			title, body := userFacingText(msg)
			for _, userID := range userIDs {
				if err := hub.notifications.NotifyUser(ctx, appID, userID, notificationType,
					title, body, notifyLevelToNotificationLevel(event.Level), metadata); err != nil {
					lastErr = err
					continue
				}
				userSent++
			}
		}
	}

	// ── 管理员侧 ──
	if len(adminIDs) > 0 {
		if hub.adminInbox == nil {
			skipReason = append(skipReason, "管理员收件箱未启用")
		} else {
			pushes := make([]notificationdomain.AdminInboxPush, 0, len(adminIDs))
			for _, adminID := range adminIDs {
				pushes = append(pushes, notificationdomain.AdminInboxPush{
					AdminID:    adminID,
					Type:       notificationType,
					Title:      msg.Title,
					Content:    msg.Body,
					Level:      notifyLevelToAdminLevel(event.Level),
					Resource:   event.Resource,
					ResourceID: event.ResourceID,
					Link:       strings.TrimSpace(event.Link),
					// 幂等到"人"这一级：同一事件对同一管理员只进一次收件箱
					DedupeKey: adminInboxDedupeKey(event, adminID),
					Metadata:  map[string]any{"event": event.Key},
				})
			}
			delivered, err := hub.adminInbox.Push(ctx, pushes)
			if err != nil {
				lastErr = err
			}
			adminSent = delivered
		}
	}

	result := &notifydomain.Result{
		RequestSnippet: fmt.Sprintf("users=%v admins=%v", userIDs, adminIDs),
	}
	if userSent == 0 && adminSent == 0 {
		if lastErr != nil {
			return result, lastErr
		}
		// 没报错也没投出去：多半是收件人全部停用或命中幂等，记为 skipped 而非 failed
		result.Status = notifydomain.DeliverySkipped
		result.ResponseSnippet = strings.Join(append(skipReason, "无有效收件人或已投递过"), "；")
		return result, nil
	}
	result.Status = notifydomain.DeliverySuccess
	result.ResponseSnippet = fmt.Sprintf("用户 %d/%d，管理员 %d/%d", userSent, len(userIDs), adminSent, len(adminIDs))
	if len(skipReason) > 0 {
		result.ResponseSnippet += "（" + strings.Join(skipReason, "；") + "）"
	}
	return result, nil
}

// inAppMetadata 站内信附带的结构化上下文。link 由调用方按受众决定。
func inAppMetadata(event *notifydomain.Event, link string) map[string]any {
	metadata := map[string]any{"event": event.Key}
	if event.ResourceID != "" {
		metadata["resource"] = event.Resource
		metadata["resourceId"] = event.ResourceID
	}
	if link != "" {
		metadata["link"] = link
	}
	return metadata
}

// userFacingText 取面向提单人的文案；未单独提供时回落到通用文案。
func userFacingText(msg *notifydomain.Message) (string, string) {
	title := strings.TrimSpace(msg.Event.UserTitle)
	if title == "" {
		title = msg.Title
	}
	body := strings.TrimSpace(msg.Event.UserSummary)
	if body == "" {
		body = msg.Body
	}
	return title, body
}

// adminInboxDedupeKey 管理员收件箱幂等键。
// 事件自带 DedupeKey 时以它为基准；否则退化为 事件+资源+人。
func adminInboxDedupeKey(event *notifydomain.Event, adminID int64) string {
	base := strings.TrimSpace(event.DedupeKey)
	if base == "" {
		if event.ResourceID == "" {
			return ""
		}
		base = event.Key + ":" + event.Resource + ":" + event.ResourceID
	}
	return fmt.Sprintf("inbox:%s:%d", base, adminID)
}

// notifyLevelToAdminLevel 事件级别 → 管理员收件箱级别。
func notifyLevelToAdminLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case notifydomain.LevelCritical:
		return notificationdomain.AdminLevelCritical
	case notifydomain.LevelWarning:
		return notificationdomain.AdminLevelWarning
	case notifydomain.LevelSuccess:
		return notificationdomain.AdminLevelSuccess
	default:
		return notificationdomain.AdminLevelInfo
	}
}

func dedupeInt64(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// notifyLevelToNotificationLevel 事件级别 → 站内信级别。
func notifyLevelToNotificationLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case notifydomain.LevelCritical:
		return "critical"
	case notifydomain.LevelWarning:
		return "warning"
	case notifydomain.LevelSuccess:
		return "success"
	default:
		return "info"
	}
}

// ─────────────── 实时推送 ───────────────

// realtimeNotifyProvider WebSocket 实时推送。
//
// 命名空间约定（与 RealtimeService.AuthenticateRequest 一致）：
//   - 应用用户：subject = realtime.user.{appid}.{userId}
//   - 管理员：  subject = realtime.user.0.{adminId}   ← appid 固定 0
//
// 管理员那条是控制台 WebSocket 实际监听的通道；早期实现只推用户侧，
// 导致受理人/关注人永远收不到实时刷新。
type realtimeNotifyProvider struct{}

func (p *realtimeNotifyProvider) Kind() string { return notifydomain.KindRealtime }

func (p *realtimeNotifyProvider) Validate(cfg map[string]any, secret string) error { return nil }

func (p *realtimeNotifyProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	if hub.realtime == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "实时推送服务未启用")
	}
	event := msg.Event
	userIDs := dedupeInt64(event.Recipients.UserIDs)
	adminIDs := dedupeInt64(event.Recipients.AdminIDs)
	if len(userIDs) == 0 && len(adminIDs) == 0 {
		return &notifydomain.Result{Status: notifydomain.DeliverySkipped,
			ResponseSnippet: "事件没有实时推送收件人"}, nil
	}

	eventType := cfgString(channel.Config, "eventType")
	if eventType == "" {
		// 默认按事件 key 推（ticket.assigned / ticket.sla.breached …），
		// 前端可按具体类型订阅，也可用 onAny 兜底
		eventType = event.Key
	}

	var (
		lastErr   error
		userSent  int
		adminSent int
	)

	if len(userIDs) > 0 {
		appID := event.AppID
		if appID == 0 {
			appID = channel.AppID
		}
		title, body := userFacingText(msg)
		payload := realtimePayload(event, title, body, strings.TrimSpace(event.UserLink), "user")
		for _, userID := range userIDs {
			if err := hub.realtime.PublishUserEvent(ctx, appID, userID, eventType, payload); err != nil {
				lastErr = err
				continue
			}
			userSent++
		}
	}

	if len(adminIDs) > 0 {
		payload := realtimePayload(event, msg.Title, msg.Body, strings.TrimSpace(event.Link), "admin")
		for _, adminID := range adminIDs {
			if err := hub.realtime.PublishUserEvent(ctx, realtimeAdminAppID, adminID, eventType, payload); err != nil {
				lastErr = err
				continue
			}
			adminSent++
		}
	}

	result := &notifydomain.Result{
		RequestSnippet: fmt.Sprintf("event=%s users=%v admins=%v", eventType, userIDs, adminIDs),
	}
	if userSent == 0 && adminSent == 0 {
		if lastErr != nil {
			return result, lastErr
		}
		result.Status = notifydomain.DeliverySkipped
		return result, nil
	}
	result.Status = notifydomain.DeliverySuccess
	result.ResponseSnippet = fmt.Sprintf("用户 %d/%d，管理员 %d/%d", userSent, len(userIDs), adminSent, len(adminIDs))
	return result, nil
}

// realtimePayload 构造实时事件载荷。audience 让前端能区分同一事件的两种视角。
func realtimePayload(event *notifydomain.Event, title, body, link, audience string) map[string]any {
	return map[string]any{
		"audience":   audience,
		"event":      event.Key,
		"title":      title,
		"summary":    body,
		"level":      event.Level,
		"link":       link,
		"resource":   event.Resource,
		"resourceId": event.ResourceID,
		"appid":      event.AppID,
		// refreshRequired 是给前端的统一信号：收到即失效相关查询缓存，
		// 不必为每种事件在前端各写一套增量合并逻辑
		"refreshRequired": true,
	}
}
