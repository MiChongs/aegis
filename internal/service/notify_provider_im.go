package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	notifydomain "aegis/internal/domain/notify"
	apperrors "aegis/pkg/errors"
)

// 钉钉 / 企业微信 / Slack 三个群机器人渠道。
// 与飞书同构：都是「POST 一段 JSON 到 Webhook」，差异只在签名方式与消息体结构。

// ─────────────── 钉钉 ───────────────

type dingtalkBotProvider struct{}

func (p *dingtalkBotProvider) Kind() string { return notifydomain.KindDingtalkBot }

func (p *dingtalkBotProvider) Validate(cfg map[string]any, secret string) error {
	webhook := cfgString(cfg, "webhook")
	if webhook == "" {
		return apperrors.New(40000, http.StatusBadRequest, "钉钉机器人 Webhook 地址不能为空")
	}
	if !strings.HasPrefix(webhook, "https://") {
		return apperrors.New(40000, http.StatusBadRequest, "钉钉 Webhook 必须是 https 地址")
	}
	return nil
}

func (p *dingtalkBotProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	webhook := cfgString(channel.Config, "webhook")
	if webhook == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "钉钉机器人未配置 Webhook")
	}
	// 加签模式：URL 追加 timestamp + sign
	if secret := strings.TrimSpace(channel.Secret); secret != "" {
		timestamp := time.Now().UnixMilli()
		stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		separator := "?"
		if strings.Contains(webhook, "?") {
			separator = "&"
		}
		webhook = fmt.Sprintf("%s%stimestamp=%d&sign=%s", webhook, separator, timestamp, url.QueryEscape(sign))
	}

	at := map[string]any{}
	if mobiles := cfgStrings(channel.Config, "atMobiles"); len(mobiles) > 0 {
		at["atMobiles"] = mobiles
	}
	if cfgBool(channel.Config, "atAll") && msg.Event.Level == notifydomain.LevelCritical {
		at["isAtAll"] = true
	}

	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"title": msg.Title, "text": markdownBody(msg, "#### ")},
	}
	if len(at) > 0 {
		payload["at"] = at
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	respBody, status, err := hub.postJSON(ctx, webhook, body, nil)
	result := &notifydomain.Result{RequestSnippet: string(body), ResponseSnippet: respBody}
	if err != nil {
		return result, err
	}
	if status < 200 || status >= 300 {
		return result, fmt.Errorf("钉钉返回 HTTP %d: %s", status, truncateForError(respBody))
	}
	var parsed struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err == nil && parsed.ErrCode != 0 {
		return result, fmt.Errorf("钉钉返回错误 errcode=%d errmsg=%s", parsed.ErrCode, parsed.ErrMsg)
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// ─────────────── 企业微信 ───────────────

type wecomBotProvider struct{}

func (p *wecomBotProvider) Kind() string { return notifydomain.KindWecomBot }

func (p *wecomBotProvider) Validate(cfg map[string]any, secret string) error {
	webhook := cfgString(cfg, "webhook")
	if webhook == "" {
		return apperrors.New(40000, http.StatusBadRequest, "企业微信机器人 Webhook 地址不能为空")
	}
	if !strings.HasPrefix(webhook, "https://") {
		return apperrors.New(40000, http.StatusBadRequest, "企业微信 Webhook 必须是 https 地址")
	}
	return nil
}

func (p *wecomBotProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	webhook := cfgString(channel.Config, "webhook")
	if webhook == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "企业微信机器人未配置 Webhook")
	}
	text := markdownBody(msg, "## ")
	if users := cfgStrings(channel.Config, "atUserIds"); len(users) > 0 {
		mentions := make([]string, 0, len(users))
		for _, user := range users {
			mentions = append(mentions, "<@"+user+">")
		}
		text = strings.Join(mentions, " ") + "\n" + text
	}
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": text},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	respBody, status, err := hub.postJSON(ctx, webhook, body, nil)
	result := &notifydomain.Result{RequestSnippet: string(body), ResponseSnippet: respBody}
	if err != nil {
		return result, err
	}
	if status < 200 || status >= 300 {
		return result, fmt.Errorf("企业微信返回 HTTP %d: %s", status, truncateForError(respBody))
	}
	var parsed struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal([]byte(respBody), &parsed); err == nil && parsed.ErrCode != 0 {
		return result, fmt.Errorf("企业微信返回错误 errcode=%d errmsg=%s", parsed.ErrCode, parsed.ErrMsg)
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// ─────────────── Slack ───────────────

type slackWebhookProvider struct{}

func (p *slackWebhookProvider) Kind() string { return notifydomain.KindSlackWebhook }

func (p *slackWebhookProvider) Validate(cfg map[string]any, secret string) error {
	webhook := cfgString(cfg, "webhook")
	if webhook == "" {
		return apperrors.New(40000, http.StatusBadRequest, "Slack Webhook URL 不能为空")
	}
	if !strings.HasPrefix(webhook, "https://") {
		return apperrors.New(40000, http.StatusBadRequest, "Slack Webhook 必须是 https 地址")
	}
	return nil
}

func (p *slackWebhookProvider) Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error) {
	webhook := cfgString(channel.Config, "webhook")
	if webhook == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "Slack 渠道未配置 Webhook")
	}
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": levelEmoji(msg.Event.Level) + " " + msg.Title, "emoji": true}},
	}
	if strings.TrimSpace(msg.Body) != "" {
		blocks = append(blocks, map[string]any{
			"type": "section", "text": map[string]any{"type": "mrkdwn", "text": msg.Body},
		})
	}
	if len(msg.Event.Fields) > 0 {
		fields := make([]map[string]any, 0, len(msg.Event.Fields))
		for _, field := range msg.Event.Fields {
			fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*%s*\n%s", field.Label, field.Value)})
		}
		blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	}
	if link := strings.TrimSpace(msg.Event.Link); link != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type": "button",
				"text": map[string]any{"type": "plain_text", "text": "查看详情"},
				"url":  link,
			}},
		})
	}
	payload := map[string]any{"text": msg.Title, "blocks": blocks}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	respBody, status, err := hub.postJSON(ctx, webhook, body, nil)
	result := &notifydomain.Result{RequestSnippet: string(body), ResponseSnippet: respBody}
	if err != nil {
		return result, err
	}
	if status < 200 || status >= 300 {
		return result, fmt.Errorf("Slack 返回 HTTP %d: %s", status, truncateForError(respBody))
	}
	result.Status = notifydomain.DeliverySuccess
	return result, nil
}

// markdownBody 把事件拼成 Markdown，钉钉/企微通用。
func markdownBody(msg *notifydomain.Message, titlePrefix string) string {
	var sb strings.Builder
	sb.WriteString(titlePrefix)
	sb.WriteString(levelEmoji(msg.Event.Level))
	sb.WriteString(" ")
	sb.WriteString(msg.Title)
	sb.WriteString("\n")
	if strings.TrimSpace(msg.Body) != "" {
		sb.WriteString("\n")
		sb.WriteString(msg.Body)
		sb.WriteString("\n")
	}
	for _, field := range msg.Event.Fields {
		sb.WriteString("\n> **")
		sb.WriteString(field.Label)
		sb.WriteString("**：")
		sb.WriteString(field.Value)
	}
	if link := strings.TrimSpace(msg.Event.Link); link != "" {
		sb.WriteString("\n\n[查看详情](")
		sb.WriteString(link)
		sb.WriteString(")")
	}
	return sb.String()
}
