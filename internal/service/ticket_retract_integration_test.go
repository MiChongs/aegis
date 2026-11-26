package service

import (
	"strconv"
	"testing"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	notifydomain "aegis/internal/domain/notify"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 单条回复撤回（000170）：提单人侧看不到、汇总字段重算、时间线留痕、它生成的站内信与收件箱条目一并删除。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestTicketRetractIntegration
func TestTicketRetractIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(32200)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ticket-retract-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	notifications := NewNotificationService(zap.NewNop(), pg, nil, nil)
	adminInbox := NewAdminInboxService(zap.NewNop(), pg, nil)
	svc := NewTicketService(zap.NewNop(), pg, nil, nil, nil)
	svc.SetInboxes(notifications, adminInbox)

	userID := insertTestUser(t, ctx, pool, appID, "tr-user", "0")
	session := &authdomain.Session{UserID: userID, AppID: appID, Account: "tr-user"}
	newAdmin := func(account string, super bool) *admindomain.AccessContext {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ($1, 'x', $1, $2) RETURNING id`, account, super).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return &admindomain.AccessContext{Session: admindomain.Session{AdminID: id, Account: account, DisplayName: account, IsSuperAdmin: super}}
	}
	boss := newAdmin("tr-boss", true)
	colleague := newAdmin("tr-colleague", true)

	var feedbackID int64
	if err := pool.QueryRow(ctx, `INSERT INTO tickets (kind, ticket_no, appid, requester_type, requester_user_id, requester_name, title, source)
VALUES ('feedback', 'TR-1', $1, 'user', $2, '用户', '工具台样式范围更广', 'app') RETURNING id`, appID, userID).Scan(&feedbackID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplyFeedback(ctx, session, feedbackID, FeedbackReplyInput{Content: "希望首页也能跟随"}); err != nil {
		t.Fatal(err)
	}
	keep, err := svc.ReplyByAdmin(ctx, colleague, ticketdomain.ReplyCommand{TicketID: feedbackID, Content: "收到，评估中"})
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := svc.ReplyByAdmin(ctx, boss, ticketdomain.ReplyCommand{TicketID: feedbackID, Content: "已加上，下个版本生效：工具台样式编辑器里新增「范围」页"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := svc.ReplyByAdmin(ctx, boss, ticketdomain.ReplyCommand{TicketID: feedbackID, Content: "内部：先别发版", Internal: true})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := pg.GetTicketByID(ctx, feedbackID)
	if before.MessageCount != 3 || before.LastMessageRole != ticketdomain.AuthorAgent {
		t.Fatalf("撤回前应有 3 条对外消息、最后发言是客服，得到 %d / %s", before.MessageCount, before.LastMessageRole)
	}

	// 这条回复生成的通知：新格式带 messageId；旧格式没有，只能按时间窗 + 正文匹配
	resourceID := strconv.FormatInt(feedbackID, 10)
	replyEvent := func(messageID int64) *notifydomain.Event {
		vars := map[string]any{}
		if messageID > 0 {
			vars["messageId"] = messageID
		}
		return &notifydomain.Event{Key: notifydomain.EventTicketAgentReplied, Resource: "feedback", ResourceID: resourceID, Vars: vars}
	}
	deliver := func(event *notifydomain.Event, content string) int64 {
		item, err := notifications.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: "ticket", Title: "【反馈已回复】工具台样式范围更广", Content: content,
			Level: "info", Metadata: inAppMetadata(event, "/feedback/"+resourceID),
		})
		if err != nil {
			t.Fatal(err)
		}
		return item.ID
	}
	newStyle := deliver(replyEvent(wrong.ID), "客服回复：已加上，下个版本生效：工具台样式编辑器里新增「范围」页")
	legacy := deliver(replyEvent(0), "客服回复：已加上，下个版本生效：工具台样式编辑器里新增「范围」页")
	other := deliver(replyEvent(keep.ID), "客服回复：收到，评估中")
	if _, err := adminInbox.Push(ctx, []notificationdomain.AdminInboxPush{
		{AdminID: colleague.AdminID, Type: "ticket", Title: "【工单已回复】TR-1", Content: "tr-boss 回复了你的工单：已加上",
			Resource: "feedback", ResourceID: resourceID, Metadata: adminInboxMetadata(replyEvent(wrong.ID)), DedupeKey: "tr-inbox-1"},
		{AdminID: colleague.AdminID, Type: "ticket", Title: "【新反馈】TR-1", Content: "用户 提交了新反馈",
			Resource: "feedback", ResourceID: resourceID, Metadata: map[string]any{"event": notifydomain.EventTicketCreated}, DedupeKey: "tr-inbox-2"},
	}); err != nil {
		t.Fatal(err)
	}

	// ── 权限 ──
	requesterMessages, _ := pg.ListTicketMessages(ctx, feedbackID, false, false)
	if _, err := svc.RetractMessage(ctx, boss, feedbackID, requesterMessages[0].ID, ""); errCode(err) != 40311 {
		t.Fatalf("提单人的消息不能撤回，得到 %v", err)
	}
	if _, err := svc.RetractMessage(ctx, boss, feedbackID+1000, wrong.ID, ""); errCode(err) != 40460 {
		t.Fatalf("工单不存在应回 40460，得到 %v", err)
	}
	if _, err := svc.RetractMessage(ctx, boss, feedbackID, 9_999_999, ""); errCode(err) != errCodeTicketMessageNotFound {
		t.Fatalf("消息不存在应回 %d，得到 %v", errCodeTicketMessageNotFound, err)
	}

	// ── 撤回 ──
	result, err := svc.RetractMessage(ctx, boss, feedbackID, wrong.ID, "功能已撤回")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Message.Retracted || result.Message.DeleteReason != "功能已撤回" || result.Message.DeletedByName != "tr-boss" {
		t.Fatalf("撤回记录不完整：%+v", result.Message)
	}
	if result.NotificationsRemoved != 2 || result.AdminInboxRemoved != 1 {
		t.Fatalf("应删掉 2 条站内信（新格式 + 旧格式）与 1 条收件箱，得到 %d / %d", result.NotificationsRemoved, result.AdminInboxRemoved)
	}
	if _, err := svc.RetractMessage(ctx, boss, feedbackID, wrong.ID, ""); errCode(err) != errCodeTicketMessageRetracted {
		t.Fatalf("重复撤回应回 %d，得到 %v", errCodeTicketMessageRetracted, err)
	}

	// 通知：只删这条回复的
	var left []int64
	rows, err := pool.Query(ctx, `SELECT id FROM notifications WHERE appid = $1 AND user_id = $2 ORDER BY id`, appID, userID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		left = append(left, id)
	}
	rows.Close()
	if len(left) != 1 || left[0] != other {
		t.Fatalf("应只剩另一条回复的通知 %d（删了 %d、%d），得到 %v", other, newStyle, legacy, left)
	}
	var inboxLeft int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_notifications WHERE resource_id = $1`, resourceID).Scan(&inboxLeft)
	if inboxLeft != 1 {
		t.Fatalf("收件箱应只剩新反馈那条，得到 %d", inboxLeft)
	}

	// 提单人侧：看不到撤回的回复，摘要按剩下的消息算
	detail, err := svc.FeedbackDetail(ctx, session, feedbackID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range detail.Messages {
		if message.ID == wrong.ID {
			t.Fatal("反馈详情不应再返回已撤回的回复")
		}
	}
	if len(detail.Messages) != 2 || detail.MessageCount != 2 || !detail.LastReplyFromAgent {
		t.Fatalf("应剩 2 条对外消息、最后一条是客服回复，得到 %d 条 / count=%d / agent=%v",
			len(detail.Messages), detail.MessageCount, detail.LastReplyFromAgent)
	}
	after, _ := pg.GetTicketByID(ctx, feedbackID)
	if after.LastMessageAt == nil || !after.LastMessageAt.Equal(keep.CreatedAt) {
		t.Fatalf("最后发言时间应回到上一条对外回复 %v，得到 %v", keep.CreatedAt, after.LastMessageAt)
	}
	if after.FirstRespondedAt == nil {
		t.Fatal("首次响应时间不应因撤回被清掉")
	}

	// 管理端：已撤回的仍在、标为已撤回；时间线有撤回事件
	adminView, err := svc.Detail(ctx, boss, feedbackID, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, message := range adminView.Messages {
		if message.ID == wrong.ID {
			found = true
			if !message.Retracted || message.Content == "" || message.CanRetract {
				t.Fatalf("管理端应看到已撤回的原文且不能再撤：%+v", message)
			}
		}
		if message.ID == keep.ID && !message.CanRetract {
			t.Fatal("超管应能撤回同事的回复")
		}
	}
	if !found {
		t.Fatal("管理端详情应保留已撤回的消息")
	}
	var retractEvent *ticketdomain.Event
	for i := range adminView.Events {
		if adminView.Events[i].Event == ticketdomain.EventMessageRetracted {
			retractEvent = &adminView.Events[i]
		}
	}
	if retractEvent == nil || retractEvent.Summary != "tr-boss 撤回了一条回复：功能已撤回" || retractEvent.FromValue != strconv.FormatInt(wrong.ID, 10) {
		t.Fatalf("时间线应有撤回事件，得到 %+v", retractEvent)
	}

	// 内部备注：撤回不动对外汇总
	if _, err := svc.RetractMessage(ctx, boss, feedbackID, note.ID, ""); err != nil {
		t.Fatal(err)
	}
	final, _ := pg.GetTicketByID(ctx, feedbackID)
	if final.MessageCount != 2 {
		t.Fatalf("撤回内部备注不应改变对外消息数，得到 %d", final.MessageCount)
	}

	// 撤回最后一条对外消息后，最后发言回到提单人
	if _, err := svc.RetractMessage(ctx, colleague, feedbackID, keep.ID, ""); err != nil {
		t.Fatal(err)
	}
	last, _ := pg.GetTicketByID(ctx, feedbackID)
	if last.MessageCount != 1 || last.LastMessageRole != ticketdomain.AuthorRequester {
		t.Fatalf("只剩提单人的消息时应为 1 条 / requester，得到 %d / %s", last.MessageCount, last.LastMessageRole)
	}
}
