package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	notifydomain "aegis/internal/domain/notify"
	apperrors "aegis/pkg/errors"
)

// 飞书投递实现。两种形态：
//
//  1. feishu_bot —— 群自定义机器人。POST 到 Webhook 即可，安全设置选「签名校验」时
//     需要带 timestamp + sign（HMAC-SHA256，密钥作为 key，"{timestamp}\n{secret}" 作为消息体，结果 base64）。
//  2. feishu_app —— 企业自建应用。先用 app_id/app_secret 换 tenant_access_token（缓存 2 小时），
//     再 POST /open-apis/im/v1/messages?receive_id_type=xxx 发消息，可发到群聊或单聊。
//
// 两者都优先发交互式卡片（msg_type=interactive），信息密度最高且可带跳转按钮。

const (
	feishuDefaultBaseURL = "https://open.feishu.cn"
	feishuTokenTTLBuffer = 5 * time.Minute
)

// ─────────────── 群自定义机器人 ───────────────

type feishuBotProvider struct{}

func (p *feishuBotProvider) Kind() string { return notifydomain.KindFeishuBot }

func (p *feishuBotProvider) Validate(cfg map[string]any, secret string) error {
	webhook := cfgString(cfg, "webhook")
	if webhook == "" {
		return apperrors.New(40000, http.StatusBadRequest, "飞书机器人 Webhook 地址不能为空")
	}
	if !strings.HasPrefix(webhook, "https://") {
		return apperrors.New(40000, http.StatusBadRequest, "飞书 Webhook 必须是 https 地址")
	}
	if !strings.Contains(webhook, "/open-apis/bot/") {
		return apperrors.New(40000, http.StatusBadRequest, "飞书 Webhook 地址格式不正确，应形如 https://open.feishu.cn/open-apis/bot/v2/hook/xxx")
	}
	return nil
}

func (p *feishuBotProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	webhook := cfgString(channel.Config, "webhook")
	if webhook == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "飞书机器人未配置 Webhook")
	}

	payload := map[string]any{}
	msgType := cfgString(channel.Config, "msgType")
	if msgType == "" {
		msgType = "interactive"
	}
	switch msgType {
	case "text":
		payload["msg_type"] = "text"
		payload["content"] = map[string]any{"text": feishuTextWithMentions(channel, msg)}
	case "post":
		payload["msg_type"] = "post"
		payload["content"] = map[string]any{"post": map[string]any{"zh_cn": feishuPostContent(msg)}}
	default:
		payload["msg_type"] = "interactive"
		payload["card"] = feishuCard(hub, channel, msg)
	}

	// 加签：密钥存在时必须带 timestamp + sign，否则飞书返回 19021
	if secret := strings.TrimSpace(channel.Secret); secret != "" {
		timestamp := fmt.Sprint(time.Now().Unix())
		payload["timestamp"] = timestamp
		payload["sign"] = feishuSign(timestamp, secret)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	respBody, status, err := hub.postJSON(ctx, webhook, body, nil)
	result := &notifydomain.Result{
		RequestSnippet:  string(body),
		ResponseSnippet: respBody,
	}
	if err != nil {
		return result, err
	}
	if err := feishuCheckResponse(status, respBody); err != nil {
		return result, err
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// feishuSign 计算群机器人加签。
// 官方算法：以 "{timestamp}\n{secret}" 为 HMAC-SHA256 的 key，对空消息体签名，结果 base64。
func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	mac.Write([]byte(""))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ─────────────── 企业自建应用 ───────────────

type feishuAppProvider struct{}

func (p *feishuAppProvider) Kind() string { return notifydomain.KindFeishuApp }

func (p *feishuAppProvider) Validate(cfg map[string]any, secret string) error {
	if cfgString(cfg, "appId") == "" {
		return apperrors.New(40000, http.StatusBadRequest, "飞书应用 App ID 不能为空")
	}
	if strings.TrimSpace(secret) == "" {
		return apperrors.New(40000, http.StatusBadRequest, "飞书应用 App Secret 不能为空")
	}
	if cfgString(cfg, "receiveId") == "" {
		return apperrors.New(40000, http.StatusBadRequest, "飞书接收方 ID 不能为空")
	}
	receiveType := cfgString(cfg, "receiveIdType")
	switch receiveType {
	case "", "chat_id", "open_id", "user_id", "union_id", "email":
	default:
		return apperrors.New(40000, http.StatusBadRequest, "不支持的飞书接收方类型")
	}
	return nil
}

func (p *feishuAppProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	appID := cfgString(channel.Config, "appId")
	appSecret := strings.TrimSpace(channel.Secret)
	if appID == "" || appSecret == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "飞书应用凭据不完整")
	}
	baseURL := cfgString(channel.Config, "baseUrl")
	if baseURL == "" {
		baseURL = feishuDefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	token, err := hub.feishuTenantToken(ctx, baseURL, appID, appSecret)
	if err != nil {
		return nil, err
	}

	receiveType := cfgString(channel.Config, "receiveIdType")
	if receiveType == "" {
		receiveType = "chat_id"
	}
	// 事件自带飞书 open_id 时优先单聊直达当事人（如「你的工单被指派」）
	receiveIDs := []string{cfgString(channel.Config, "receiveId")}
	if len(msg.Event.Recipients.FeishuOpenIDs) > 0 && receiveType == "open_id" {
		receiveIDs = msg.Event.Recipients.FeishuOpenIDs
	}

	msgType := cfgString(channel.Config, "msgType")
	if msgType == "" {
		msgType = "interactive"
	}
	var contentJSON []byte
	if msgType == "text" {
		contentJSON, err = json.Marshal(map[string]any{"text": feishuTextWithMentions(channel, msg)})
	} else {
		contentJSON, err = json.Marshal(feishuCard(hub, channel, msg))
	}
	if err != nil {
		return nil, err
	}

	result := &notifydomain.Result{}
	for _, receiveID := range receiveIDs {
		if strings.TrimSpace(receiveID) == "" {
			continue
		}
		body, err := json.Marshal(map[string]any{
			"receive_id": receiveID,
			"msg_type":   msgType,
			"content":    string(contentJSON),
		})
		if err != nil {
			return result, err
		}
		url := fmt.Sprintf("%s/open-apis/im/v1/messages?receive_id_type=%s", baseURL, receiveType)
		respBody, status, err := hub.postJSON(ctx, url, body, map[string]string{
			"Authorization": "Bearer " + token,
		})
		result.RequestSnippet = string(body)
		result.ResponseSnippet = respBody
		if err != nil {
			return result, err
		}
		if err := feishuCheckResponse(status, respBody); err != nil {
			// token 失效时清缓存，交给外层重试
			if strings.Contains(respBody, "99991663") || strings.Contains(respBody, "99991661") {
				hub.invalidateFeishuToken(appID)
			}
			return result, err
		}
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// feishuTenantToken 取 tenant_access_token，带进程内缓存（提前 5 分钟过期）。
func (h *NotifyHub) feishuTenantToken(ctx context.Context, baseURL, appID, appSecret string) (string, error) {
	h.tokenMu.Lock()
	if cached, ok := h.tokenCache[appID]; ok && time.Now().Before(cached.expiresAt) {
		h.tokenMu.Unlock()
		return cached.token, nil
	}
	h.tokenMu.Unlock()

	body, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	respBody, status, err := h.postJSON(ctx, baseURL+"/open-apis/auth/v3/tenant_access_token/internal", body, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("飞书鉴权失败 HTTP %d: %s", status, respBody)
	}
	var parsed struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err != nil {
		return "", fmt.Errorf("飞书鉴权响应解析失败: %w", err)
	}
	if parsed.Code != 0 || parsed.TenantAccessToken == "" {
		return "", fmt.Errorf("飞书鉴权失败 code=%d msg=%s", parsed.Code, parsed.Msg)
	}
	ttl := time.Duration(parsed.Expire) * time.Second
	if ttl <= feishuTokenTTLBuffer {
		ttl = 30 * time.Minute
	}
	h.tokenMu.Lock()
	h.tokenCache[appID] = notifyCachedToken{token: parsed.TenantAccessToken, expiresAt: time.Now().Add(ttl - feishuTokenTTLBuffer)}
	h.tokenMu.Unlock()
	return parsed.TenantAccessToken, nil
}

func (h *NotifyHub) invalidateFeishuToken(appID string) {
	h.tokenMu.Lock()
	delete(h.tokenCache, appID)
	h.tokenMu.Unlock()
}

// ─────────────── 卡片构建 ───────────────

// feishuCard 构建交互式卡片。渠道模板提供 card_template 时直接用模板结果。
func feishuCard(hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) map[string]any {
	if len(msg.Card) > 0 {
		return msg.Card
	}
	event := msg.Event

	elements := make([]map[string]any, 0, len(event.Fields)+3)
	if strings.TrimSpace(msg.Body) != "" {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": msg.Body},
		})
	}
	if len(event.Fields) > 0 {
		fields := make([]map[string]any, 0, len(event.Fields))
		for _, field := range event.Fields {
			fields = append(fields, map[string]any{
				"is_short": field.Short,
				"text": map[string]any{
					"tag":     "lark_md",
					"content": fmt.Sprintf("**%s**\n%s", field.Label, field.Value),
				},
			})
		}
		elements = append(elements, map[string]any{"tag": "div", "fields": fields})
	}

	// @人：critical 事件允许 @所有人，其余只 @指定成员
	mentions := feishuMentionMarkdown(channel, event.Level)
	if mentions != "" {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": mentions},
		})
	}

	actions := make([]map[string]any, 0, len(event.Actions)+1)
	for _, action := range event.Actions {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": action.Text},
			"url":  action.URL,
			"type": feishuButtonType(action.Style),
		})
	}
	if len(actions) == 0 && strings.TrimSpace(event.Link) != "" {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": "查看详情"},
			"url":  event.Link,
			"type": "primary",
		})
	}
	if len(actions) > 0 {
		elements = append(elements, map[string]any{"tag": "action", "actions": actions})
	}

	note := fmt.Sprintf("Aegis · %s", time.Now().Format("2006-01-02 15:04:05"))
	if strings.TrimSpace(event.AppName) != "" {
		note = fmt.Sprintf("Aegis · %s · %s", event.AppName, time.Now().Format("2006-01-02 15:04:05"))
	}
	elements = append(elements, map[string]any{
		"tag":      "note",
		"elements": []map[string]any{{"tag": "plain_text", "content": note}},
	})

	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true, "enable_forward": true},
		"header": map[string]any{
			"template": levelColor(event.Level),
			"title":    map[string]any{"tag": "plain_text", "content": msg.Title},
		},
		"elements": elements,
	}
}

func feishuButtonType(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "danger":
		return "danger"
	case "primary":
		return "primary"
	default:
		return "default"
	}
}

// feishuMentionMarkdown 生成卡片内的 @ 文本。
func feishuMentionMarkdown(channel *notifydomain.Channel, level string) string {
	parts := make([]string, 0, 4)
	if cfgBool(channel.Config, "atAll") && level == notifydomain.LevelCritical {
		parts = append(parts, "<at id=all></at>")
	}
	for _, openID := range cfgStrings(channel.Config, "atUserIds") {
		parts = append(parts, fmt.Sprintf("<at id=%s></at>", openID))
	}
	return strings.Join(parts, " ")
}

// feishuTextWithMentions 纯文本形态下的 @ 处理。
func feishuTextWithMentions(channel *notifydomain.Channel, msg *notifydomain.Message) string {
	text := plainTextBody(msg)
	if mentions := feishuMentionMarkdown(channel, msg.Event.Level); mentions != "" {
		text = mentions + "\n" + text
	}
	return text
}

// feishuPostContent 富文本形态（post）。
func feishuPostContent(msg *notifydomain.Message) map[string]any {
	content := make([][]map[string]any, 0, len(msg.Event.Fields)+2)
	if strings.TrimSpace(msg.Body) != "" {
		content = append(content, []map[string]any{{"tag": "text", "text": msg.Body}})
	}
	for _, field := range msg.Event.Fields {
		content = append(content, []map[string]any{{"tag": "text", "text": field.Label + "：" + field.Value}})
	}
	if link := strings.TrimSpace(msg.Event.Link); link != "" {
		content = append(content, []map[string]any{{"tag": "a", "text": "查看详情", "href": link}})
	}
	return map[string]any{"title": msg.Title, "content": content}
}

// feishuCheckResponse 飞书统一返回 {code:0,msg:"success"}；code≠0 即失败。
func feishuCheckResponse(status int, body string) error {
	if status < 200 || status >= 300 {
		return fmt.Errorf("飞书返回 HTTP %d: %s", status, truncateForError(body))
	}
	var parsed struct {
		Code          int    `json:"code"`
		Msg           string `json:"msg"`
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		// 非 JSON 响应按成功处理（部分代理会包一层）
		return nil
	}
	if parsed.Code != 0 {
		return fmt.Errorf("飞书返回错误 code=%d msg=%s", parsed.Code, parsed.Msg)
	}
	// 老版群机器人用 StatusCode 字段
	if parsed.StatusCode != 0 {
		return fmt.Errorf("飞书返回错误 StatusCode=%d %s", parsed.StatusCode, parsed.StatusMessage)
	}
	return nil
}

// ─────────────── HTTP 辅助 ───────────────

// postJSON 统一的 JSON POST，返回响应体文本与状态码。
func (h *NotifyHub) postJSON(ctx context.Context, url string, body []byte, headers map[string]string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", resp.StatusCode, err
	}
	return string(respBody), resp.StatusCode, nil
}

func truncateForError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 300 {
		return value
	}
	return value[:300] + "…"
}
