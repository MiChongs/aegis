package service

import (
	"strings"
	"testing"
	"time"

	notifydomain "aegis/internal/domain/notify"
	ticketdomain "aegis/internal/domain/ticket"
)

func retractTestMessage(authorType string, authorAdminID int64, internal bool) *ticketdomain.Message {
	message := &ticketdomain.Message{ID: 9, TicketID: 1, AuthorType: authorType, Internal: internal, Content: "原文"}
	if authorAdminID > 0 {
		id := authorAdminID
		message.AuthorAdminID = &id
	}
	return message
}

func TestCanRetractMessage(t *testing.T) {
	t.Parallel()
	const me = int64(7)
	viewer := &ticketdomain.ActionSet{View: true, RetractOwn: true}
	internalViewer := &ticketdomain.ActionSet{View: true, ViewInternal: true, RetractOwn: true}
	manager := &ticketdomain.ActionSet{View: true, ViewInternal: true, RetractOwn: true, RetractAny: true}

	retracted := retractTestMessage(ticketdomain.AuthorAgent, me, false)
	now := time.Now()
	retracted.DeletedAt = &now
	retracted.Retracted = true

	cases := []struct {
		name    string
		message *ticketdomain.Message
		actions *ticketdomain.ActionSet
		code    int
	}{
		{"自己的对外回复", retractTestMessage(ticketdomain.AuthorAgent, me, false), viewer, 0},
		{"别人的回复没有 manage", retractTestMessage(ticketdomain.AuthorAgent, 8, false), viewer, 40311},
		{"别人的回复有 manage", retractTestMessage(ticketdomain.AuthorAgent, 8, false), manager, 0},
		{"提单人的消息 manage 也不行", retractTestMessage(ticketdomain.AuthorRequester, 0, false), manager, 40311},
		{"系统消息只有 manage 能撤", retractTestMessage(ticketdomain.AuthorSystem, 0, false), internalViewer, 40311},
		{"系统消息 manage", retractTestMessage(ticketdomain.AuthorSystem, 0, false), manager, 0},
		{"看不到的内部备注按不存在回答", retractTestMessage(ticketdomain.AuthorAgent, me, true), viewer, errCodeTicketMessageNotFound},
		{"自己的内部备注", retractTestMessage(ticketdomain.AuthorAgent, me, true), internalViewer, 0},
		{"已撤回", retracted, manager, errCodeTicketMessageRetracted},
		{"看不到工单", retractTestMessage(ticketdomain.AuthorAgent, me, false), &ticketdomain.ActionSet{}, 40313},
		{"消息不存在", nil, manager, errCodeTicketMessageNotFound},
		{"作者已被删除（无 admin id）", retractTestMessage(ticketdomain.AuthorAgent, 0, false), viewer, 40311},
	}
	for _, tc := range cases {
		err := canRetractMessage(tc.message, tc.actions, me)
		if got := errCode(err); got != tc.code {
			t.Errorf("%s：期望错误码 %d，得到 %d（%v）", tc.name, tc.code, got, err)
		}
	}
}

func TestPresentMessagesForAdmin(t *testing.T) {
	t.Parallel()
	const me = int64(7)
	now := time.Now()
	build := func() []ticketdomain.Message {
		return []ticketdomain.Message{
			{ID: 1, AuthorType: ticketdomain.AuthorRequester, Content: "用户原话"},
			{ID: 2, AuthorType: ticketdomain.AuthorAgent, AuthorAdminID: ptrInt64(me), Content: "我的回复"},
			{ID: 3, AuthorType: ticketdomain.AuthorAgent, AuthorAdminID: ptrInt64(me), Content: "撤回的原文",
				Retracted: true, DeletedAt: &now, DeleteReason: "功能已撤回",
				Attachments: []ticketdomain.Attachment{{ID: 30}}},
		}
	}

	// 能看内部备注：已撤回的原文与原因保留
	full := build()
	presentMessagesForAdmin(full, &ticketdomain.ActionSet{View: true, ViewInternal: true, RetractOwn: true}, me)
	if full[2].Content != "撤回的原文" || full[2].DeleteReason != "功能已撤回" || len(full[2].Attachments) != 1 {
		t.Fatalf("有内部备注权限应看到原文：%+v", full[2])
	}
	if full[0].CanRetract || !full[1].CanRetract || full[2].CanRetract {
		t.Fatalf("canRetract 应为 false/true/false，得到 %v/%v/%v", full[0].CanRetract, full[1].CanRetract, full[2].CanRetract)
	}

	// 只能看工单：已撤回的只剩占位
	limited := build()
	presentMessagesForAdmin(limited, &ticketdomain.ActionSet{View: true, RetractOwn: true}, me)
	if limited[2].Content != "" || limited[2].DeleteReason != "" || limited[2].Attachments != nil || !limited[2].Retracted {
		t.Fatalf("无内部备注权限应隐去原文：%+v", limited[2])
	}
}

func TestDropRetractedAttachments(t *testing.T) {
	t.Parallel()
	messages := []ticketdomain.Message{{ID: 1}, {ID: 2, Retracted: true}}
	attachments := []ticketdomain.Attachment{
		{ID: 10, MessageID: ptrInt64(1)}, {ID: 20, MessageID: ptrInt64(2)}, {ID: 30},
	}
	got := dropRetractedAttachments(messages, attachments)
	if len(got) != 2 || got[0].ID != 10 || got[1].ID != 30 {
		t.Fatalf("应只去掉挂在已撤回消息上的附件，得到 %+v", got)
	}
}

func TestRetractEventSummaryAndProbe(t *testing.T) {
	t.Parallel()
	own := retractEventSummary("川意", &ticketdomain.Message{AuthorName: "川意"}, "功能已撤回")
	if own != "川意 撤回了一条回复：功能已撤回" {
		t.Fatalf("自己撤回的文案不对：%q", own)
	}
	other := retractEventSummary("川意", &ticketdomain.Message{AuthorName: "小语义", Internal: true}, "")
	if other != "川意 撤回了 小语义 的一条内部备注" {
		t.Fatalf("撤回他人备注的文案不对：%q", other)
	}
	long := strings.Repeat("字", 100)
	if probe := retractProbe("  " + long); len([]rune(probe)) != ticketRetractProbeLen {
		t.Fatalf("探针应截到 %d 字，得到 %d", ticketRetractProbeLen, len([]rune(probe)))
	}
	if retractProbe("短") != "短" {
		t.Fatal("短正文应原样返回")
	}
}

func TestEventMessageIDIntoInboxMetadata(t *testing.T) {
	t.Parallel()
	for _, raw := range []any{int64(811), 811, float64(811), "811"} {
		event := &notifydomain.Event{Key: notifydomain.EventTicketAgentReplied, Resource: "feedback", ResourceID: "395",
			Vars: map[string]any{"messageId": raw}}
		if got := inAppMetadata(event, "")["messageId"]; got != "811" {
			t.Errorf("站内信元数据 messageId（%T）应为 \"811\"，得到 %v", raw, got)
		}
		if got := adminInboxMetadata(event)["messageId"]; got != "811" {
			t.Errorf("收件箱元数据 messageId（%T）应为 \"811\"，得到 %v", raw, got)
		}
	}
	plain := &notifydomain.Event{Key: notifydomain.EventTicketCreated}
	if _, ok := inAppMetadata(plain, "")["messageId"]; ok {
		t.Fatal("没有 messageId 的事件不应写入该字段")
	}
}
