package service

import (
	"aegis/pkg/egress"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"aegis/internal/config"
	notificationdomain "aegis/internal/domain/notification"
	notifydomain "aegis/internal/domain/notify"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// NotifyHub 是平台唯一的通知出口。
//
// 业务侧（工单、风控、告警……）只做一件事：构造 notifydomain.Event 并调用 Dispatch/DispatchAsync。
// 「发给谁、用什么渠道、长什么样、失败怎么重试」全部在这一层收敛：
//
//	业务事件 ──► 订阅匹配（事件 key / 应用 / 优先级 / 分类 / 静默窗口）
//	         ──► 模板渲染（按渠道类型选模板，text/template 变量替换）
//	         ──► Provider 投递（飞书/钉钉/企微/Slack/Webhook/邮件/站内信/实时）
//	         ──► 留痕 + 重试（notify_deliveries，指数退避 3 次）
//
// 新增一种 IM 只需实现 notifyProvider 并注册，业务代码零改动。
type NotifyHub struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	key        []byte
	httpClient *http.Client
	providers  map[string]notifyProvider

	// 面向"人"的渠道复用既有服务。
	// notifications 收应用用户，adminInbox 收管理员 —— 两套收件箱主键空间不同，不可混用。
	// 这两个字段声明为接口而非具体服务：站内信的双收件箱分发是最容易悄悄失效的一环
	// （早期实现就把管理员那一路整个丢了），必须能在单测里注入假实现验证。
	email         *EmailService
	notifications userInboxWriter
	adminInbox    adminInboxWriter
	realtime      UserEventPublisher

	// 控制台基址，用于把「查看工单」按钮指向真实页面
	consoleBaseURL string

	// 渠道级限流：内存令牌桶，按渠道 ID 计数（单实例粒度，够用且零外部依赖）
	rateMu    sync.Mutex
	rateState map[int64]*notifyRateWindow

	// 飞书 tenant_access_token 缓存（按 appId 维度，提前 5 分钟过期）
	tokenMu    sync.Mutex
	tokenCache map[string]notifyCachedToken
}

type notifyRateWindow struct {
	windowStart time.Time
	count       int
}

type notifyCachedToken struct {
	token     string
	expiresAt time.Time
}

// userInboxWriter 应用用户站内信写入口（由 *NotificationService 实现）。
type userInboxWriter interface {
	NotifyUser(ctx context.Context, appID int64, userID int64,
		notificationType string, title string, content string, level string, metadata map[string]any) error
}

// adminInboxWriter 管理员收件箱写入口（由 *AdminInboxService 实现）。
type adminInboxWriter interface {
	Push(ctx context.Context, items []notificationdomain.AdminInboxPush) (int64, error)
}

// 编译期断言：两个具体服务必须满足上面的接口，改签名时立刻报错而不是运行时才发现。
var (
	_ userInboxWriter  = (*NotificationService)(nil)
	_ adminInboxWriter = (*AdminInboxService)(nil)
)

// notifyProvider 单一渠道的投递实现。
type notifyProvider interface {
	// Kind 渠道类型标识，与 notify_channels.kind 对应
	Kind() string
	// Validate 保存渠道配置时的校验
	Validate(cfg map[string]any, secret string) error
	// Send 执行一次投递；返回 error 表示可重试的失败
	Send(ctx context.Context, hub *NotifyHub, channel *notifydomain.Channel, msg *notifydomain.Message) (*notifydomain.Result, error)
}

const (
	notifyHTTPTimeout   = 10 * time.Second
	notifyMaxAttempts   = 3
	notifyDispatchLimit = 8 // 单事件并发投递上限
)

// NewNotifyHub 构造统一通知出口。
//
// adminInbox 可为 nil（例如导出 OpenAPI 的空装配），此时 inapp 渠道对管理员侧记为 skipped
// 并在投递记录里写明原因，不会静默丢消息。
func NewNotifyHub(log *zap.Logger, pg *pgrepo.Repository, cfg config.Config,
	email *EmailService, notifications *NotificationService, adminInbox *AdminInboxService,
	realtime UserEventPublisher) *NotifyHub {
	if log == nil {
		log = zap.NewNop()
	}
	// 与 AppOAuthService / AuthProtocolService 一致：同一主密钥按用途各自派生，互不复用
	digest := sha256.Sum256([]byte("aegis.notify.master\x00" + cfg.Security.MasterKey))
	hub := &NotifyHub{
		log: log,
		pg:  pg,
		key: digest[:],
		// Slack / Discord / 通用 Webhook 常在境外，投递统一经出海网关
		httpClient: egress.NewClient(egress.Profile{Name: "notify.webhook", Timeout: notifyHTTPTimeout}),
		email:      email,
		realtime:   realtime,
		// 只认 CONSOLE_BASE_URL：留空即用相对路径（/tickets?id=N），同源部署天然可用。
		// 这里曾误用 DocsPortalURL（默认 /developers），导致通知链接变成
		// /developers/tickets?id=N —— 点进去必然 404。
		consoleBaseURL: strings.TrimRight(strings.TrimSpace(cfg.ConsoleBaseURL), "/"),
		rateState:      make(map[int64]*notifyRateWindow),
		tokenCache:     make(map[string]notifyCachedToken),
	}
	// 显式判空后再装进接口字段。
	// 直接赋 (*NotificationService)(nil) 会得到一个「非 nil 接口包着 nil 指针」，
	// provider 里的 `== nil` 判空就会失效，进而在 nil 接收者上解引用 s.pg 而 panic。
	if notifications != nil {
		hub.notifications = notifications
	}
	if adminInbox != nil {
		hub.adminInbox = adminInbox
	}
	hub.providers = map[string]notifyProvider{
		notifydomain.KindFeishuBot:    &feishuBotProvider{},
		notifydomain.KindFeishuApp:    &feishuAppProvider{},
		notifydomain.KindDingtalkBot:  &dingtalkBotProvider{},
		notifydomain.KindWecomBot:     &wecomBotProvider{},
		notifydomain.KindSlackWebhook: &slackWebhookProvider{},
		notifydomain.KindWebhook:      &genericWebhookProvider{},
		notifydomain.KindEmail:        &emailNotifyProvider{},
		notifydomain.KindInApp:        &inAppNotifyProvider{},
		notifydomain.KindRealtime:     &realtimeNotifyProvider{},
	}
	return hub
}

// SetConsoleBaseURL 覆盖控制台基址（部署形态不同时由 bootstrap 注入）。
func (h *NotifyHub) SetConsoleBaseURL(base string) {
	h.consoleBaseURL = strings.TrimRight(strings.TrimSpace(base), "/")
}

// ─────────────── 投递入口 ───────────────

// DispatchAsync 后台投递，不阻塞业务链路。业务侧几乎总是用这个。
func (h *NotifyHub) DispatchAsync(event notifydomain.Event) {
	if h == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := h.Dispatch(ctx, event); err != nil {
			h.log.Warn("通知投递失败", zap.String("event", event.Key), zap.Error(err))
		}
	}()
}

// Dispatch 同步投递。返回 error 仅代表"整体流程异常"，
// 单个渠道失败会写入 notify_deliveries 而不中断其它渠道。
func (h *NotifyHub) Dispatch(ctx context.Context, event notifydomain.Event) error {
	if h == nil || h.pg == nil {
		return nil
	}
	event.Key = strings.TrimSpace(event.Key)
	if event.Key == "" {
		return apperrors.New(40000, http.StatusBadRequest, "通知事件 key 不能为空")
	}
	if strings.TrimSpace(event.Level) == "" {
		event.Level = notifydomain.LevelInfo
	}

	targets, err := h.resolveTargets(ctx, &event)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}

	sem := make(chan struct{}, notifyDispatchLimit)
	var wg sync.WaitGroup
	for i := range targets {
		target := targets[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				// provider 里的意外 panic 不应带崩整个投递流程
				if rec := recover(); rec != nil {
					h.log.Error("通知 provider panic",
						zap.String("event", event.Key), zap.String("kind", target.channel.Kind), zap.Any("panic", rec))
				}
			}()
			h.deliver(ctx, &event, target)
		}()
	}
	wg.Wait()
	return nil
}

// notifyTarget 一次投递的目标：渠道 + 命中的订阅（定向发送时订阅为 nil）。
type notifyTarget struct {
	channel      *notifydomain.Channel
	subscription *notifydomain.Subscription
}

// resolveTargets 解析事件应该投给哪些渠道。
func (h *NotifyHub) resolveTargets(ctx context.Context, event *notifydomain.Event) ([]notifyTarget, error) {
	// 定向发送（测试按钮）：绕过订阅表
	if len(event.ChannelIDs) > 0 {
		channels, err := h.pg.GetNotifyChannelsByIDs(ctx, event.ChannelIDs)
		if err != nil {
			return nil, err
		}
		targets := make([]notifyTarget, 0, len(channels))
		for i := range channels {
			ch := channels[i]
			h.decryptChannelSecret(&ch)
			targets = append(targets, notifyTarget{channel: &ch})
		}
		return targets, nil
	}

	subs, err := h.pg.MatchNotifySubscriptions(ctx, event.Key, event.AppID)
	if err != nil {
		return nil, err
	}
	if len(subs) == 0 {
		return nil, nil
	}

	// 同一渠道被多条订阅命中时只投一次，取最先匹配的那条（订阅按 id 升序）
	seen := make(map[int64]struct{}, len(subs))
	channelIDs := make([]int64, 0, len(subs))
	matched := make(map[int64]*notifydomain.Subscription, len(subs))
	for i := range subs {
		sub := subs[i]
		if _, ok := seen[sub.ChannelID]; ok {
			continue
		}
		if !h.subscriptionMatches(&sub, event) {
			continue
		}
		seen[sub.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, sub.ChannelID)
		matched[sub.ChannelID] = &sub
	}
	if len(channelIDs) == 0 {
		return nil, nil
	}

	channels, err := h.pg.GetNotifyChannelsByIDs(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	targets := make([]notifyTarget, 0, len(channels))
	for i := range channels {
		ch := channels[i]
		if !ch.Enabled {
			continue
		}
		h.decryptChannelSecret(&ch)
		targets = append(targets, notifyTarget{channel: &ch, subscription: matched[ch.ID]})
	}
	return targets, nil
}

// subscriptionMatches 精细过滤：优先级下限、分类白名单、静默窗口。
func (h *NotifyHub) subscriptionMatches(sub *notifydomain.Subscription, event *notifydomain.Event) bool {
	if min := strings.TrimSpace(sub.MinPriority); min != "" && strings.TrimSpace(event.Priority) != "" {
		if notifyPriorityWeight(event.Priority) < notifyPriorityWeight(min) {
			return false
		}
	}
	if len(sub.CategoryIDs) > 0 {
		if event.CategoryID == nil {
			return false
		}
		hit := false
		for _, id := range sub.CategoryIDs {
			if id == *event.CategoryID {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if sub.QuietHours != nil && inQuietHours(*sub.QuietHours, time.Now()) {
		// critical 事件穿透静默窗口：真出事了不能被"免打扰"挡掉
		if event.Level != notifydomain.LevelCritical {
			return false
		}
	}
	return true
}

func notifyPriorityWeight(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "urgent":
		return 4
	case "high":
		return 3
	case "normal":
		return 2
	case "low":
		return 1
	}
	return 0
}

// inQuietHours 判定当前是否落在静默窗口内，支持跨零点（23:00 → 08:00）。
func inQuietHours(quiet notifydomain.QuietHours, now time.Time) bool {
	loc := timeutil.DefaultLocation()
	if tz := strings.TrimSpace(quiet.Timezone); tz != "" {
		if parsed, err := time.LoadLocation(tz); err == nil {
			loc = parsed
		}
	}
	local := now.In(loc)
	start, okStart := parseClock(quiet.Start)
	end, okEnd := parseClock(quiet.End)
	if !okStart || !okEnd {
		return false
	}
	current := local.Hour()*60 + local.Minute()
	if start == end {
		return false
	}
	if start < end {
		return current >= start && current < end
	}
	// 跨零点
	return current >= start || current < end
}

func parseClock(value string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, false
	}
	var hour, minute int
	if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil {
		return 0, false
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &minute); err != nil {
		return 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// deliver 单渠道投递：限流 → 渲染 → 留痕 → 重试 → 回填。
func (h *NotifyHub) deliver(ctx context.Context, event *notifydomain.Event, target notifyTarget) {
	channel := target.channel
	provider, ok := h.providers[channel.Kind]
	if !ok {
		h.log.Warn("未知通知渠道类型", zap.String("kind", channel.Kind), zap.Int64("channelId", channel.ID))
		return
	}

	dedupe := ""
	if key := strings.TrimSpace(event.DedupeKey); key != "" {
		dedupe = fmt.Sprintf("%s:%d", key, channel.ID)
	}
	deliveryID, err := h.pg.InsertNotifyDelivery(ctx, notifydomain.Delivery{
		ChannelID:   &channel.ID,
		ChannelKind: channel.Kind,
		EventKey:    event.Key,
		AppID:       event.AppID,
		Resource:    event.Resource,
		ResourceID:  event.ResourceID,
		DedupeKey:   dedupe,
	})
	if err != nil {
		h.log.Warn("写入通知投递记录失败", zap.Error(err))
		return
	}
	if deliveryID == 0 {
		// 命中幂等键，说明同一事件已经投过这个渠道
		return
	}

	if !h.allowRate(channel) {
		_ = h.pg.CompleteNotifyDelivery(ctx, deliveryID, notifydomain.DeliverySkipped, 0, "", "", "渠道限流，本分钟配额已用尽", 0)
		return
	}

	msg, err := h.render(ctx, event, target)
	if err != nil {
		_ = h.pg.CompleteNotifyDelivery(ctx, deliveryID, notifydomain.DeliveryFailed, 0, "", "", "模板渲染失败："+err.Error(), 0)
		return
	}

	var (
		lastResult *notifydomain.Result
		lastErr    error
	)
	for attempt := 1; attempt <= notifyMaxAttempts; attempt++ {
		started := time.Now()
		result, sendErr := provider.Send(ctx, h, channel, msg)
		if result == nil {
			result = &notifydomain.Result{}
		}
		if result.LatencyMs == 0 {
			result.LatencyMs = int(time.Since(started).Milliseconds())
		}
		lastResult, lastErr = result, sendErr
		if sendErr == nil {
			// 尊重 provider 自报的状态：它返回 skipped 表示「压根没投出去」
			// （无收件人 / 依赖未接线）。早期实现一律记 success，
			// 结果投递记录里满屏"成功"，而实际一条都没送达，问题被完全掩盖。
			status := notifydomain.DeliverySuccess
			if result.Status == notifydomain.DeliverySkipped {
				status = notifydomain.DeliverySkipped
			}
			_ = h.pg.CompleteNotifyDelivery(ctx, deliveryID, status, attempt,
				result.RequestSnippet, result.ResponseSnippet, "", result.LatencyMs)
			// 渠道状态灯只反映"真的发出去过"，skipped 不该把红灯刷绿
			if status == notifydomain.DeliverySuccess {
				_ = h.pg.TouchNotifyChannelResult(ctx, channel.ID, notifydomain.DeliverySuccess, "")
			}
			return
		}
		if attempt < notifyMaxAttempts {
			// 指数退避：1s → 2s；同时尊重上层 ctx 取消
			select {
			case <-ctx.Done():
				attempt = notifyMaxAttempts
			case <-time.After(time.Duration(1<<(attempt-1)) * time.Second):
			}
		}
	}

	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	_ = h.pg.CompleteNotifyDelivery(ctx, deliveryID, notifydomain.DeliveryFailed, notifyMaxAttempts,
		lastResult.RequestSnippet, lastResult.ResponseSnippet, errMsg, lastResult.LatencyMs)
	_ = h.pg.TouchNotifyChannelResult(ctx, channel.ID, notifydomain.DeliveryFailed, errMsg)
	h.log.Warn("通知渠道投递失败",
		zap.String("event", event.Key), zap.String("kind", channel.Kind),
		zap.String("channel", channel.Name), zap.Error(lastErr))
}

// allowRate 渠道级每分钟限流。0 表示不限。
func (h *NotifyHub) allowRate(channel *notifydomain.Channel) bool {
	if channel.RateLimit <= 0 {
		return true
	}
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	now := time.Now()
	state, ok := h.rateState[channel.ID]
	if !ok || now.Sub(state.windowStart) >= time.Minute {
		h.rateState[channel.ID] = &notifyRateWindow{windowStart: now, count: 1}
		return true
	}
	if state.count >= channel.RateLimit {
		return false
	}
	state.count++
	return true
}

// ─────────────── 模板渲染 ───────────────

// render 生成标题 / 正文 / 卡片覆盖。
// 模板查找顺序：订阅指定 → 事件+渠道类型精确匹配 → 事件通用 → 内置默认。
func (h *NotifyHub) render(ctx context.Context, event *notifydomain.Event, target notifyTarget) (*notifydomain.Message, error) {
	msg := &notifydomain.Message{Event: event, Title: event.Title, Body: event.Summary}

	var tpl *notifydomain.Template
	if target.subscription != nil && target.subscription.TemplateID != nil {
		found, err := h.pg.GetNotifyTemplate(ctx, *target.subscription.TemplateID)
		if err != nil {
			return nil, err
		}
		tpl = found
	}
	if tpl == nil {
		found, err := h.pg.ResolveNotifyTemplate(ctx, event.Key, target.channel.Kind, event.AppID)
		if err != nil {
			return nil, err
		}
		tpl = found
	}
	if tpl == nil {
		return msg, nil
	}

	vars := h.templateVars(event)
	if strings.TrimSpace(tpl.TitleTemplate) != "" {
		rendered, err := renderNotifyTemplate(tpl.TitleTemplate, vars)
		if err != nil {
			return nil, err
		}
		msg.Title = rendered
	}
	if strings.TrimSpace(tpl.BodyTemplate) != "" {
		rendered, err := renderNotifyTemplate(tpl.BodyTemplate, vars)
		if err != nil {
			return nil, err
		}
		msg.Body = rendered
	}
	msg.Card = tpl.CardTemplate
	return msg, nil
}

// templateVars 把事件摊平成模板变量。
func (h *NotifyHub) templateVars(event *notifydomain.Event) map[string]any {
	vars := map[string]any{
		"Event":    event.Key,
		"Title":    event.Title,
		"Summary":  event.Summary,
		"Level":    event.Level,
		"Link":     event.Link,
		"AppID":    event.AppID,
		"AppName":  event.AppName,
		"Priority": event.Priority,
		"Time":     time.Now().In(timeutil.DefaultLocation()).Format("2006-01-02 15:04:05"),
	}
	for _, field := range event.Fields {
		vars[field.Label] = field.Value
	}
	for k, v := range event.Vars {
		vars[k] = v
	}
	return vars
}

func renderNotifyTemplate(source string, vars map[string]any) (string, error) {
	tpl, err := template.New("notify").Option("missingkey=zero").Parse(source)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ─────────────── 密钥 ───────────────

func (h *NotifyHub) decryptChannelSecret(channel *notifydomain.Channel) {
	cipher := strings.TrimSpace(channel.Secret)
	if cipher == "" {
		channel.Secret = ""
		channel.SecretSet = false
		return
	}
	plain, err := decryptSecret(h.key, cipher)
	if err != nil {
		h.log.Warn("通知渠道密钥解密失败", zap.Int64("channelId", channel.ID), zap.Error(err))
		channel.Secret = ""
		return
	}
	channel.Secret = plain
	channel.SecretSet = true
}

// EncryptChannelSecret 供管理服务保存渠道时调用。
func (h *NotifyHub) EncryptChannelSecret(plain string) (string, string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", "", nil
	}
	cipher, err := encryptSecret(h.key, plain)
	if err != nil {
		return "", "", err
	}
	return cipher, secretHint(plain), nil
}

// secretHint 生成脱敏提示（保留末 4 位），用于控制台展示"已配置"。
func secretHint(plain string) string {
	runes := []rune(plain)
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return "****" + string(runes[len(runes)-4:])
}

// ─────────────── 渠道校验 ───────────────

// ValidateChannel 保存前校验渠道配置是否完整。
func (h *NotifyHub) ValidateChannel(kind string, cfg map[string]any, secret string) error {
	provider, ok := h.providers[kind]
	if !ok {
		return apperrors.New(40000, http.StatusBadRequest, "不支持的通知渠道类型")
	}
	return provider.Validate(cfg, secret)
}

// SupportedKinds 已注册的渠道类型（按目录顺序）。
func (h *NotifyHub) SupportedKinds() []notifydomain.ChannelKindMeta {
	metas := make([]notifydomain.ChannelKindMeta, 0, len(notifydomain.ChannelKinds))
	for _, meta := range notifydomain.ChannelKinds {
		if _, ok := h.providers[meta.Kind]; ok {
			metas = append(metas, meta)
		}
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].Kind < metas[j].Kind })
	return metas
}

// ─────────────── 配置读取辅助（provider 共用） ───────────────

func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	value, ok := cfg[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func cfgBool(cfg map[string]any, key string) bool {
	if cfg == nil {
		return false
	}
	switch typed := cfg[key].(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	}
	return false
}

func cfgStrings(cfg map[string]any, key string) []string {
	if cfg == nil {
		return nil
	}
	switch typed := cfg[key].(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		parts := strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if s := strings.TrimSpace(part); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// levelColor 事件级别 → 卡片主题色（各 IM 通用语义）。
func levelColor(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case notifydomain.LevelCritical:
		return "red"
	case notifydomain.LevelWarning:
		return "orange"
	case notifydomain.LevelSuccess:
		return "green"
	default:
		return "blue"
	}
}

// levelEmoji 级别图标，纯文本渠道用它保留可读的严重度提示。
func levelEmoji(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case notifydomain.LevelCritical:
		return "🔴"
	case notifydomain.LevelWarning:
		return "🟠"
	case notifydomain.LevelSuccess:
		return "🟢"
	default:
		return "🔵"
	}
}

// plainTextBody 把事件拼成纯文本，供不支持卡片的渠道兜底。
func plainTextBody(msg *notifydomain.Message) string {
	event := msg.Event
	var sb strings.Builder
	sb.WriteString(levelEmoji(event.Level))
	sb.WriteString(" ")
	sb.WriteString(msg.Title)
	if strings.TrimSpace(msg.Body) != "" {
		sb.WriteString("\n")
		sb.WriteString(msg.Body)
	}
	for _, field := range event.Fields {
		sb.WriteString("\n")
		sb.WriteString(field.Label)
		sb.WriteString("：")
		sb.WriteString(field.Value)
	}
	if strings.TrimSpace(event.Link) != "" {
		sb.WriteString("\n")
		sb.WriteString(event.Link)
	}
	return sb.String()
}
