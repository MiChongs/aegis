package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	notifydomain "aegis/internal/domain/notify"
	apperrors "aegis/pkg/errors"
)

// 通知出口的管理面：渠道 / 订阅 / 模板 / 投递记录 / 测试发送。
// 与投递面（notify_hub.go）同属 NotifyHub，密钥加解密不出这一层。

// ─────────────── 渠道 ───────────────

// ListChannels 渠道列表。appID<0 表示全部。返回值不含密钥明文。
func (h *NotifyHub) ListChannels(ctx context.Context, appID int64, kind string) ([]notifydomain.Channel, error) {
	channels, err := h.pg.ListNotifyChannels(ctx, appID, kind, false)
	if err != nil {
		return nil, err
	}
	subs, err := h.pg.ListNotifySubscriptions(ctx, 0)
	if err != nil {
		return nil, err
	}
	byChannel := make(map[int64][]notifydomain.Subscription, len(subs))
	for _, sub := range subs {
		byChannel[sub.ChannelID] = append(byChannel[sub.ChannelID], sub)
	}
	for i := range channels {
		channels[i].Subscriptions = byChannel[channels[i].ID]
		// 出网响应只保留"是否已配置"，绝不回传密文或明文
		channels[i].SecretSet = strings.TrimSpace(channels[i].Secret) != ""
		channels[i].Secret = ""
	}
	return channels, nil
}

// GetChannel 单条渠道（不含密钥）。
func (h *NotifyHub) GetChannel(ctx context.Context, id int64) (*notifydomain.Channel, error) {
	channel, err := h.pg.GetNotifyChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "通知渠道不存在")
	}
	subs, err := h.pg.ListNotifySubscriptions(ctx, id)
	if err != nil {
		return nil, err
	}
	channel.Subscriptions = subs
	channel.SecretSet = strings.TrimSpace(channel.Secret) != ""
	channel.Secret = ""
	return channel, nil
}

// SaveChannel 新建或更新渠道。
//
// 密钥语义：
//   - mutation.Secret == nil       → 保持原值
//   - *mutation.Secret == ""       → 保持原值（前端占位符原样回传时不会误清空）
//   - *mutation.Secret == "-"      → 显式清空
//   - 其它                          → 覆盖为新值
func (h *NotifyHub) SaveChannel(ctx context.Context, mutation notifydomain.ChannelMutation) (*notifydomain.Channel, error) {
	item := notifydomain.Channel{
		ID:      mutation.ID,
		AppID:   mutation.AppID,
		Enabled: true,
	}
	var existing *notifydomain.Channel
	if mutation.ID > 0 {
		found, err := h.pg.GetNotifyChannel(ctx, mutation.ID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, apperrors.New(40470, http.StatusNotFound, "通知渠道不存在")
		}
		existing = found
		item = *found
		item.Secret = "" // 下面单独处理
	}
	if mutation.Key != nil {
		item.Key = strings.TrimSpace(*mutation.Key)
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Kind != nil {
		item.Kind = strings.TrimSpace(*mutation.Kind)
	}
	if mutation.Config != nil {
		item.Config = mutation.Config
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.RateLimit != nil {
		item.RateLimit = *mutation.RateLimit
		if item.RateLimit < 0 {
			item.RateLimit = 0
		}
	}
	if mutation.CreatedBy != nil && item.ID == 0 {
		item.CreatedBy = mutation.CreatedBy
	}

	if item.Key == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "渠道标识不能为空")
	}
	if item.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "渠道名称不能为空")
	}
	if _, ok := notifydomain.ValidKinds[item.Kind]; !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的通知渠道类型")
	}

	// 解析本次要落库的密钥
	cipher := ""
	keepSecret := true
	plainSecret := ""
	if existing != nil {
		h.decryptChannelSecret(existing)
		plainSecret = existing.Secret
	}
	if mutation.Secret != nil {
		raw := strings.TrimSpace(*mutation.Secret)
		switch raw {
		case "":
			// 保持原值
		case "-":
			keepSecret = false
			cipher = ""
			item.SecretHint = ""
			plainSecret = ""
		default:
			encrypted, hint, err := h.EncryptChannelSecret(raw)
			if err != nil {
				return nil, err
			}
			keepSecret = false
			cipher = encrypted
			item.SecretHint = hint
			plainSecret = raw
		}
	}
	if item.ID == 0 {
		// 新建时没有"原值"可保持
		keepSecret = false
	}

	if err := h.ValidateChannel(item.Kind, item.Config, plainSecret); err != nil {
		return nil, err
	}

	saved, err := h.pg.UpsertNotifyChannel(ctx, item, cipher, keepSecret)
	if err != nil {
		return nil, err
	}
	saved.SecretSet = strings.TrimSpace(saved.Secret) != ""
	saved.Secret = ""
	return saved, nil
}

// DeleteChannel 删除渠道及其订阅。
func (h *NotifyHub) DeleteChannel(ctx context.Context, id int64) error {
	ok, err := h.pg.DeleteNotifyChannel(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40470, http.StatusNotFound, "通知渠道不存在")
	}
	return nil
}

// TestChannel 向指定渠道发一条测试消息，同步返回投递结果。
func (h *NotifyHub) TestChannel(ctx context.Context, id int64, operator string) (*notifydomain.Result, error) {
	channel, err := h.pg.GetNotifyChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "通知渠道不存在")
	}
	h.decryptChannelSecret(channel)
	provider, ok := h.providers[channel.Kind]
	if !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的通知渠道类型")
	}

	event := notifydomain.Event{
		Key:      "notify.channel.test",
		AppID:    channel.AppID,
		Level:    notifydomain.LevelInfo,
		Title:    "Aegis 通知渠道连通性测试",
		Summary:  fmt.Sprintf("这是一条来自 Aegis 的测试消息，用于验证渠道「%s」配置是否正确。", channel.Name),
		Resource: "notify_channel",
		Fields: []notifydomain.Field{
			{Label: "渠道", Value: channel.Name, Short: true},
			{Label: "类型", Value: channel.Kind, Short: true},
			{Label: "操作人", Value: fallbackText(operator, "系统"), Short: true},
			{Label: "时间", Value: time.Now().Format("2006-01-02 15:04:05"), Short: true},
		},
	}
	msg := &notifydomain.Message{Event: &event, Title: event.Title, Body: event.Summary}

	started := time.Now()
	result, sendErr := provider.Send(ctx, h, channel, msg)
	if result == nil {
		result = &notifydomain.Result{}
	}
	if result.LatencyMs == 0 {
		result.LatencyMs = int(time.Since(started).Milliseconds())
	}
	status := notifydomain.DeliverySuccess
	errMsg := ""
	if sendErr != nil {
		status = notifydomain.DeliveryFailed
		errMsg = sendErr.Error()
		result.Status = status
		result.Error = errMsg
	}
	// 测试也留痕，便于事后核对"到底发出去没有"
	deliveryID, insertErr := h.pg.InsertNotifyDelivery(ctx, notifydomain.Delivery{
		ChannelID: &channel.ID, ChannelKind: channel.Kind, EventKey: event.Key,
		AppID: channel.AppID, Resource: "notify_channel", ResourceID: fmt.Sprint(channel.ID),
	})
	if insertErr == nil && deliveryID > 0 {
		_ = h.pg.CompleteNotifyDelivery(ctx, deliveryID, status, 1,
			result.RequestSnippet, result.ResponseSnippet, errMsg, result.LatencyMs)
	}
	_ = h.pg.TouchNotifyChannelResult(ctx, channel.ID, status, errMsg)

	if sendErr != nil {
		return result, apperrors.New(50281, http.StatusBadGateway, "测试消息发送失败："+errMsg)
	}
	result.Status = status
	return result, nil
}

// ─────────────── 订阅 ───────────────

// ListSubscriptions 订阅列表。channelID=0 表示全部。
func (h *NotifyHub) ListSubscriptions(ctx context.Context, channelID int64) ([]notifydomain.Subscription, error) {
	return h.pg.ListNotifySubscriptions(ctx, channelID)
}

// SaveSubscription 新建或更新订阅。
func (h *NotifyHub) SaveSubscription(ctx context.Context, mutation notifydomain.SubscriptionMutation) (*notifydomain.Subscription, error) {
	item := notifydomain.Subscription{
		ID:          mutation.ID,
		ChannelID:   mutation.ChannelID,
		EventKey:    strings.TrimSpace(mutation.EventKey),
		AppID:       mutation.AppID,
		MinPriority: strings.TrimSpace(mutation.MinPriority),
		CategoryIDs: mutation.CategoryIDs,
		TemplateID:  mutation.TemplateID,
		QuietHours:  mutation.QuietHours,
		Enabled:     true,
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if item.ChannelID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择通知渠道")
	}
	if item.EventKey == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "事件 key 不能为空")
	}
	if !isKnownEventKey(item.EventKey) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的事件 key，可用值见事件目录（支持 xxx.* 通配与 *）")
	}
	if item.MinPriority != "" && notifyPriorityWeight(item.MinPriority) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的最低优先级")
	}
	if item.QuietHours != nil {
		if _, ok := parseClock(item.QuietHours.Start); !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "静默开始时间格式应为 HH:mm")
		}
		if _, ok := parseClock(item.QuietHours.End); !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "静默结束时间格式应为 HH:mm")
		}
	}
	channel, err := h.pg.GetNotifyChannel(ctx, item.ChannelID)
	if err != nil {
		return nil, err
	}
	if channel == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "通知渠道不存在")
	}
	return h.pg.UpsertNotifySubscription(ctx, item)
}

// isKnownEventKey 校验事件 key：目录内的精确值，或 `前缀.*` / `*` 通配。
func isKnownEventKey(key string) bool {
	if key == "*" {
		return true
	}
	if strings.HasSuffix(key, ".*") {
		prefix := strings.TrimSuffix(key, ".*")
		for _, meta := range notifydomain.KnownEvents {
			if strings.HasPrefix(meta.Key, prefix+".") {
				return true
			}
		}
		return false
	}
	for _, meta := range notifydomain.KnownEvents {
		if meta.Key == key {
			return true
		}
	}
	return false
}

// DeleteSubscription 删除订阅。
func (h *NotifyHub) DeleteSubscription(ctx context.Context, id int64) error {
	ok, err := h.pg.DeleteNotifySubscription(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40471, http.StatusNotFound, "订阅不存在")
	}
	return nil
}

// ─────────────── 模板 ───────────────

// ListTemplates 模板列表。
func (h *NotifyHub) ListTemplates(ctx context.Context, appID int64) ([]notifydomain.Template, error) {
	return h.pg.ListNotifyTemplates(ctx, appID)
}

// SaveTemplate 新建或更新模板。
func (h *NotifyHub) SaveTemplate(ctx context.Context, mutation notifydomain.TemplateMutation) (*notifydomain.Template, error) {
	item := notifydomain.Template{ID: mutation.ID, AppID: mutation.AppID, Enabled: true}
	if mutation.ID > 0 {
		existing, err := h.pg.GetNotifyTemplate(ctx, mutation.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(40472, http.StatusNotFound, "通知模板不存在")
		}
		item = *existing
	}
	if mutation.Key != nil {
		item.Key = strings.TrimSpace(*mutation.Key)
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.EventKey != nil {
		item.EventKey = strings.TrimSpace(*mutation.EventKey)
	}
	if mutation.ChannelKind != nil {
		item.ChannelKind = strings.TrimSpace(*mutation.ChannelKind)
	}
	if mutation.TitleTemplate != nil {
		item.TitleTemplate = *mutation.TitleTemplate
	}
	if mutation.BodyTemplate != nil {
		item.BodyTemplate = *mutation.BodyTemplate
	}
	if mutation.CardTemplate != nil {
		item.CardTemplate = mutation.CardTemplate
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if item.Key == "" || item.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "模板标识与名称不能为空")
	}
	if item.EventKey == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "模板必须绑定事件 key")
	}
	if item.ChannelKind != "" {
		if _, ok := notifydomain.ValidKinds[item.ChannelKind]; !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的渠道类型")
		}
	}
	// 保存前先试渲染，把语法错误挡在生产投递之前
	sample := map[string]any{"TicketNo": "TK20260101000001", "TicketTitle": "示例工单", "Title": "示例", "Summary": "示例摘要"}
	if strings.TrimSpace(item.TitleTemplate) != "" {
		if _, err := renderNotifyTemplate(item.TitleTemplate, sample); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, "标题模板语法错误："+err.Error())
		}
	}
	if strings.TrimSpace(item.BodyTemplate) != "" {
		if _, err := renderNotifyTemplate(item.BodyTemplate, sample); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, "正文模板语法错误："+err.Error())
		}
	}
	return h.pg.UpsertNotifyTemplate(ctx, item)
}

// DeleteTemplate 删除模板。
func (h *NotifyHub) DeleteTemplate(ctx context.Context, id int64) error {
	ok, err := h.pg.DeleteNotifyTemplate(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40472, http.StatusNotFound, "通知模板不存在")
	}
	return nil
}

// PreviewTemplate 用样例变量预览模板渲染结果。
func (h *NotifyHub) PreviewTemplate(ctx context.Context, titleTemplate, bodyTemplate string, vars map[string]any) (string, string, error) {
	if vars == nil {
		vars = map[string]any{}
	}
	title, err := renderNotifyTemplate(titleTemplate, vars)
	if err != nil {
		return "", "", apperrors.New(40000, http.StatusBadRequest, "标题模板语法错误："+err.Error())
	}
	body, err := renderNotifyTemplate(bodyTemplate, vars)
	if err != nil {
		return "", "", apperrors.New(40000, http.StatusBadRequest, "正文模板语法错误："+err.Error())
	}
	return title, body, nil
}

// ─────────────── 投递记录 ───────────────

// ListDeliveries 投递记录分页。
func (h *NotifyHub) ListDeliveries(ctx context.Context, query notifydomain.DeliveryQuery) (*notifydomain.DeliveryPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	items, total, err := h.pg.ListNotifyDeliveries(ctx, query)
	if err != nil {
		return nil, err
	}
	return &notifydomain.DeliveryPage{
		Items: items, Page: query.Page, Limit: query.Limit,
		Total: total, TotalPages: totalPages(total, query.Limit),
	}, nil
}

// DeliveryStats 投递健康度。
func (h *NotifyHub) DeliveryStats(ctx context.Context, days int) (*notifydomain.DeliveryStats, error) {
	return h.pg.NotifyDeliveryStats(ctx, days)
}

// PurgeDeliveries 清理历史投递记录。
func (h *NotifyHub) PurgeDeliveries(ctx context.Context, days int) (int64, error) {
	return h.pg.PurgeNotifyDeliveries(ctx, days)
}

// Events 返回事件目录，供控制台下拉选择。
func (h *NotifyHub) Events() []notifydomain.EventMeta {
	events := make([]notifydomain.EventMeta, len(notifydomain.KnownEvents))
	copy(events, notifydomain.KnownEvents)
	return events
}
