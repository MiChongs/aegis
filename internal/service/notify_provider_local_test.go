package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	notificationdomain "aegis/internal/domain/notification"
	notifydomain "aegis/internal/domain/notify"
)

// 这一组用例钉住站内信/实时推送最容易悄悄失效的两件事：
//   1. 管理员那一路有没有真的投出去（早期实现整条丢了，且不报错）
//   2. 给应用用户的载荷里有没有混进控制台深链（越权信息泄露）

// ── 假实现 ──

type fakeUserInbox struct {
	mu    sync.Mutex
	calls []fakeUserInboxCall
	err   error
}

type fakeUserInboxCall struct {
	AppID    int64
	UserID   int64
	Type     string
	Title    string
	Content  string
	Level    string
	Metadata map[string]any
}

func (f *fakeUserInbox) NotifyUser(ctx context.Context, appID, userID int64,
	notificationType, title, content, level string, metadata map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeUserInboxCall{
		AppID: appID, UserID: userID, Type: notificationType,
		Title: title, Content: content, Level: level, Metadata: metadata,
	})
	return f.err
}

type fakeAdminInbox struct {
	mu    sync.Mutex
	items []notificationdomain.AdminInboxPush
	err   error
}

func (f *fakeAdminInbox) Push(ctx context.Context, items []notificationdomain.AdminInboxPush) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = append(f.items, items...)
	if f.err != nil {
		return 0, f.err
	}
	return int64(len(items)), nil
}

type fakeRealtime struct {
	mu     sync.Mutex
	events []fakeRealtimeEvent
}

type fakeRealtimeEvent struct {
	AppID  int64
	UserID int64
	Type   string
	Data   map[string]any
}

func (f *fakeRealtime) PublishUserEvent(ctx context.Context, appID, userID int64, eventType string, data map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fakeRealtimeEvent{AppID: appID, UserID: userID, Type: eventType, Data: data})
	return nil
}

// ticketLikeEvent 构造一个典型的工单事件：同时有提单用户和两名处理人。
func ticketLikeEvent() *notifydomain.Event {
	return &notifydomain.Event{
		Key:         "ticket.agent_replied",
		AppID:       7,
		Type:        "ticket",
		Level:       notifydomain.LevelInfo,
		Title:       "【工单已回复】TK001 支付未到账",
		Summary:     "李四 回复了工单：已为你补单",
		Link:        "https://console.example.com/tickets?id=42",
		UserLink:    "/tickets/42",
		UserTitle:   "【客服已回复】支付未到账",
		UserSummary: "客服回复：已为你补单",
		Resource:    "ticket",
		ResourceID:  "42",
		DedupeKey:   "ticket:42:ticket.agent_replied:1700000000",
		Recipients: notifydomain.Recipients{
			UserIDs:  []int64{1001},
			AdminIDs: []int64{5, 9},
		},
	}
}

func newTestMessage(event *notifydomain.Event) *notifydomain.Message {
	return &notifydomain.Message{Event: event, Title: event.Title, Body: event.Summary}
}

// ── 站内信 ──

func TestInAppProviderFansOutToBothInboxes(t *testing.T) {
	users := &fakeUserInbox{}
	admins := &fakeAdminInbox{}
	hub := &NotifyHub{notifications: users, adminInbox: admins}
	channel := &notifydomain.Channel{ID: 1, Kind: notifydomain.KindInApp, Config: map[string]any{}}

	event := ticketLikeEvent()
	result, err := (&inAppNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event))
	if err != nil {
		t.Fatalf("投递失败：%v", err)
	}
	if result.Status != notifydomain.DeliverySuccess {
		t.Fatalf("期望 success，实际 %s（%s）", result.Status, result.ResponseSnippet)
	}
	if len(users.calls) != 1 {
		t.Fatalf("应用用户应收到 1 条，实际 %d", len(users.calls))
	}
	// 这是回归重点：管理员那一路曾经整个是空操作
	if len(admins.items) != 2 {
		t.Fatalf("管理员应收到 2 条，实际 %d", len(admins.items))
	}
	for _, item := range admins.items {
		if item.Link != event.Link {
			t.Fatalf("管理员通知应带控制台深链，实际 %q", item.Link)
		}
		if item.ResourceID != "42" {
			t.Fatalf("管理员通知应关联工单 42，实际 %q", item.ResourceID)
		}
		if !strings.HasPrefix(item.DedupeKey, "inbox:") || !strings.HasSuffix(item.DedupeKey, ":"+itoa(item.AdminID)) {
			t.Fatalf("幂等键应细化到人，实际 %q", item.DedupeKey)
		}
	}
}

func TestInAppProviderNeverLeaksConsoleLinkToUsers(t *testing.T) {
	users := &fakeUserInbox{}
	hub := &NotifyHub{notifications: users, adminInbox: &fakeAdminInbox{}}
	channel := &notifydomain.Channel{ID: 1, Kind: notifydomain.KindInApp, Config: map[string]any{}}

	event := ticketLikeEvent()
	if _, err := (&inAppNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event)); err != nil {
		t.Fatalf("投递失败：%v", err)
	}
	call := users.calls[0]
	link, _ := call.Metadata["link"].(string)
	if link != "/tickets/42" {
		t.Fatalf("用户站内信应带应用侧路径，实际 %q", link)
	}
	if strings.Contains(link, "console.example.com") {
		t.Fatal("用户站内信里出现了控制台深链")
	}
	// 文案也要用面向用户的那一份，不能出现"李四回复了工单"这种内部视角
	if call.Title != event.UserTitle || call.Content != event.UserSummary {
		t.Fatalf("用户站内信应使用 UserTitle/UserSummary，实际 title=%q content=%q", call.Title, call.Content)
	}
}

func TestInAppProviderSkipsWhenNoRecipients(t *testing.T) {
	hub := &NotifyHub{notifications: &fakeUserInbox{}, adminInbox: &fakeAdminInbox{}}
	channel := &notifydomain.Channel{ID: 1, Kind: notifydomain.KindInApp, Config: map[string]any{}}
	event := &notifydomain.Event{Key: "ticket.created", Title: "x"}

	result, err := (&inAppNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event))
	if err != nil {
		t.Fatalf("无收件人时不应报错：%v", err)
	}
	if result.Status != notifydomain.DeliverySkipped {
		t.Fatalf("无收件人应记为 skipped，实际 %s", result.Status)
	}
}

func TestInAppProviderReportsMissingAdminInbox(t *testing.T) {
	// adminInbox 未接线时必须在投递记录里写明原因，而不是静默丢掉管理员那一路
	hub := &NotifyHub{notifications: &fakeUserInbox{}}
	channel := &notifydomain.Channel{ID: 1, Kind: notifydomain.KindInApp, Config: map[string]any{}}
	event := &notifydomain.Event{Key: "ticket.sla.breached", Title: "SLA 超时",
		Recipients: notifydomain.Recipients{AdminIDs: []int64{5}}}

	result, err := (&inAppNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event))
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if !strings.Contains(result.ResponseSnippet, "管理员收件箱未启用") {
		t.Fatalf("应记录未接线原因，实际 %q", result.ResponseSnippet)
	}
}

// ── 实时推送 ──

func TestRealtimeProviderPublishesAdminsOnAdminNamespace(t *testing.T) {
	realtime := &fakeRealtime{}
	hub := &NotifyHub{realtime: realtime}
	channel := &notifydomain.Channel{ID: 2, Kind: notifydomain.KindRealtime, Config: map[string]any{}}

	event := ticketLikeEvent()
	result, err := (&realtimeNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event))
	if err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	if result.Status != notifydomain.DeliverySuccess {
		t.Fatalf("期望 success，实际 %s", result.Status)
	}
	if len(realtime.events) != 3 { // 1 个用户 + 2 个管理员
		t.Fatalf("应推送 3 条，实际 %d", len(realtime.events))
	}

	var userEvents, adminEvents int
	for _, evt := range realtime.events {
		if evt.Type != event.Key {
			t.Fatalf("默认应按事件 key 推送，实际 %q", evt.Type)
		}
		switch evt.Data["audience"] {
		case "user":
			userEvents++
			if evt.AppID != 7 {
				t.Fatalf("用户推送应走应用命名空间 appid=7，实际 %d", evt.AppID)
			}
			if evt.Data["link"] != "/tickets/42" {
				t.Fatalf("用户推送应带应用侧路径，实际 %v", evt.Data["link"])
			}
		case "admin":
			adminEvents++
			// 回归重点：管理员必须走 appid=0，否则控制台长连接根本收不到
			if evt.AppID != realtimeAdminAppID {
				t.Fatalf("管理员推送必须走 appid=0，实际 %d", evt.AppID)
			}
			if evt.Data["link"] != event.Link {
				t.Fatalf("管理员推送应带控制台深链，实际 %v", evt.Data["link"])
			}
		default:
			t.Fatalf("载荷缺少 audience 标识：%v", evt.Data)
		}
	}
	if userEvents != 1 || adminEvents != 2 {
		t.Fatalf("受众分布错误：user=%d admin=%d", userEvents, adminEvents)
	}
}

func TestRealtimeProviderHonoursEventTypeOverride(t *testing.T) {
	realtime := &fakeRealtime{}
	hub := &NotifyHub{realtime: realtime}
	channel := &notifydomain.Channel{
		ID: 2, Kind: notifydomain.KindRealtime,
		Config: map[string]any{"eventType": "ticket.updated"},
	}
	event := ticketLikeEvent()
	if _, err := (&realtimeNotifyProvider{}).Send(context.Background(), hub, channel, newTestMessage(event)); err != nil {
		t.Fatalf("推送失败：%v", err)
	}
	for _, evt := range realtime.events {
		if evt.Type != "ticket.updated" {
			t.Fatalf("应使用渠道配置的事件类型，实际 %q", evt.Type)
		}
	}
}

// ── 操作者剔除 ──

func TestExcludeActorAdminRemovesSelfAndDedupes(t *testing.T) {
	actor := int64(5)
	got := excludeActorAdmin([]int64{5, 9, 9, 0, 12}, &actor)
	want := []int64{9, 12}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	}
	// 系统触发（无操作者）时不剔除任何人
	if all := excludeActorAdmin([]int64{5, 9}, nil); len(all) != 2 {
		t.Fatalf("无操作者时不应剔除，实际 %v", all)
	}
}

func TestNotifyLevelToAdminLevel(t *testing.T) {
	cases := map[string]string{
		notifydomain.LevelCritical: notificationdomain.AdminLevelCritical,
		notifydomain.LevelWarning:  notificationdomain.AdminLevelWarning,
		notifydomain.LevelSuccess:  notificationdomain.AdminLevelSuccess,
		notifydomain.LevelInfo:     notificationdomain.AdminLevelInfo,
		"":                         notificationdomain.AdminLevelInfo,
	}
	for input, want := range cases {
		if got := notifyLevelToAdminLevel(input); got != want {
			t.Fatalf("%q → 期望 %q，实际 %q", input, want, got)
		}
	}
}

// ── 受众路由 ──

// 所有工单事件都要告知处理侧：回复人本身已由 ticketActor 排除，
// 其余关注人 / 同组成员理应看到进展。早期实现让 agent_replied 返回 false，
// 结果「同事已回复」这类进展对关注人完全不可见。
func TestTicketEventFacesAgentsCoversEveryEvent(t *testing.T) {
	events := []string{
		ticketEventCreated, ticketEventUserReplied, ticketEventAgentReplied,
		ticketEventAssigned, ticketEventStatusChanged, ticketEventResolved,
		ticketEventClosed, ticketEventReopened, ticketEventEscalated,
		ticketEventSLAWarning, ticketEventSLABreached, ticketEventRated,
	}
	for _, event := range events {
		if !ticketEventFacesAgents(event) {
			t.Fatalf("%s 应告知处理侧", event)
		}
	}
}

// 兜底只覆盖「必须有人看见」的事件，避免每次状态微调都打扰超管。
func TestTicketEventNeedsFallbackScope(t *testing.T) {
	shouldFallback := []string{
		ticketEventCreated, ticketEventUserReplied, ticketEventEscalated,
		ticketEventSLAWarning, ticketEventSLABreached,
	}
	for _, event := range shouldFallback {
		if !ticketEventNeedsFallback(event) {
			t.Fatalf("%s 无人认领时应兜底给超管", event)
		}
	}
	shouldNot := []string{
		ticketEventAgentReplied, ticketEventAssigned, ticketEventStatusChanged,
		ticketEventResolved, ticketEventClosed, ticketEventRated, ticketEventReopened,
	}
	for _, event := range shouldNot {
		if ticketEventNeedsFallback(event) {
			t.Fatalf("%s 不应兜底给超管", event)
		}
	}
}

// 面向提单人的事件必须同时覆盖「用户提单」与「管理员提单」两种身份。
// 早期实现只看 RequesterUserID，平台内部工单（提单人是管理员）被回复后无人收到。
func TestTicketEventFacesRequesterSet(t *testing.T) {
	requesterFacing := []string{
		ticketEventAgentReplied, ticketEventResolved, ticketEventClosed,
		ticketEventStatusChanged, ticketEventReopened,
	}
	for _, event := range requesterFacing {
		if !ticketEventFacesRequester(event) {
			t.Fatalf("%s 应告知提单人", event)
		}
	}
	if ticketEventFacesRequester(ticketEventAssigned) {
		t.Fatal("指派属于内部动作，不应告知提单人")
	}
	if ticketEventFacesRequester(ticketEventSLABreached) {
		t.Fatal("SLA 超时属于内部指标，不应告知提单人")
	}
}

func itoa(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf []byte
	for value > 0 {
		buf = append([]byte{byte('0' + value%10)}, buf...)
		value /= 10
	}
	if negative {
		return "-" + string(buf)
	}
	return string(buf)
}
