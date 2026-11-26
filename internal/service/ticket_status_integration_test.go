package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 状态精简（000168）：七种状态、全部三个字；等待中与不采纳的流转；旧的两种等待状态不再被接受。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestTicketStatusIntegration
func TestTicketStatusIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(32100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ticket-status-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	svc := NewTicketService(zap.NewNop(), pg, nil, nil, nil)
	userID := insertTestUser(t, ctx, pool, appID, "ts-user", "0")
	session := &authdomain.Session{UserID: userID, AppID: appID, Account: "ts-user"}
	var adminID int64
	if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ('ts-admin', 'x', '客服', TRUE) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	admin := &admindomain.AccessContext{Session: admindomain.Session{AdminID: adminID, Account: "ts-admin", DisplayName: "客服", IsSuperAdmin: true}}

	// ── 状态名一律三个字 ──
	if len(ticketdomain.StatusLabels) != 7 || len(ticketdomain.ValidStatuses) != 7 {
		t.Fatalf("应为七种状态，得到 %d / %d", len(ticketdomain.StatusLabels), len(ticketdomain.ValidStatuses))
	}
	for _, item := range ticketdomain.StatusLabels {
		if utf8.RuneCountInString(item.Label) != 3 {
			t.Errorf("状态 %s 的中文名 %q 不是三个字", item.Value, item.Label)
		}
		if _, ok := ticketdomain.ValidStatuses[item.Value]; !ok {
			t.Errorf("状态 %s 有中文名却不在 ValidStatuses 里", item.Value)
		}
	}

	// ── 数据库约束：旧状态写不进去，新状态可以 ──
	if _, err := pool.Exec(ctx, `INSERT INTO tickets (kind, ticket_no, appid, requester_type, requester_user_id, title, source, status)
VALUES ('feedback', 'TS-OLD', $1, 'user', $2, '旧状态', 'app', 'pending_user')`, appID, userID); err == nil || !strings.Contains(err.Error(), "tickets_status_check") {
		t.Fatalf("pending_user 应被 tickets_status_check 拒绝，得到 %v", err)
	}

	newFeedback := func(title string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO tickets (kind, ticket_no, appid, requester_type, requester_user_id, requester_name, title, source)
VALUES ('feedback', $1, $2, 'user', $3, '用户', $4, 'app') RETURNING id`, "TS-"+title, appID, userID, title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// ── 等待中：反馈人回复后自动回到处理中 ──
	waiting := newFeedback("等待")
	if _, err := svc.ChangeStatus(ctx, admin, waiting, ticketdomain.StatusCommand{Status: "pending_user"}); errCode(err) != 40000 {
		t.Fatalf("旧状态 pending_user 应被拒，得到 %v", err)
	}
	if item, err := svc.ChangeStatus(ctx, admin, waiting, ticketdomain.StatusCommand{Status: ticketdomain.StatusPending}); err != nil || item.Status != ticketdomain.StatusPending {
		t.Fatalf("改为等待中失败：%+v / %v", item, err)
	}
	detail, err := svc.ReplyFeedback(ctx, session, waiting, FeedbackReplyInput{Content: "补充了截图说明"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Status != ticketdomain.StatusProcessing || detail.StatusText != "处理中" {
		t.Fatalf("等待中被反馈人回复后应回到处理中，得到 %s / %s", detail.Status, detail.StatusText)
	}

	// ── 不采纳：终态，不能回复、不能撤回，可以评价；不计入未结 ──
	rejected := newFeedback("不采纳")
	item, err := svc.ChangeStatus(ctx, admin, rejected, ticketdomain.StatusCommand{Status: ticketdomain.StatusRejected, Reason: "与产品方向不符"})
	if err != nil || item.Status != ticketdomain.StatusRejected || item.ClosedAt == nil || item.SLAState != ticketdomain.SLAMet {
		t.Fatalf("不采纳应记关闭时间且 SLA 达标：%+v / %v", item, err)
	}
	event := svc.buildTicketEvent(ctx, item, ticketEventClosed, ticketActor{AdminID: &adminID}, map[string]any{"reason": "与产品方向不符"})
	if event.UserTitle != "【反馈未被采纳】不采纳" || !strings.Contains(event.UserSummary, "与产品方向不符") {
		t.Fatalf("不采纳的通知文案不对：%q / %q", event.UserTitle, event.UserSummary)
	}
	if len(event.Recipients.UserIDs) != 1 || event.Recipients.UserIDs[0] != userID {
		t.Fatalf("不采纳应通知反馈人，得到 %v", event.Recipients.UserIDs)
	}
	if _, err := svc.ReplyFeedback(ctx, session, rejected, FeedbackReplyInput{Content: "为什么"}); errCode(err) != 40316 {
		t.Fatalf("不采纳后不能再回复，得到 %v", err)
	}
	if _, err := svc.CancelFeedback(ctx, session, rejected, "算了"); errCode(err) != 40316 {
		t.Fatalf("不采纳后不能撤回，得到 %v", err)
	}
	if rated, err := svc.RateFeedback(ctx, session, rejected, ticketdomain.RatingCommand{Rating: 3}); err != nil || rated.Rating == nil || *rated.Rating != 3 {
		t.Fatalf("不采纳后应能评价：%+v / %v", rated, err)
	}
	open, err := svc.List(ctx, admin, ticketdomain.ListQuery{Kind: ticketdomain.KindFeedback, AppID: ptrInt64(appID), Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range open.Items {
		if row.ID == rejected {
			t.Fatal("默认列表（不含已结束）不应包含不采纳的反馈")
		}
	}
	stats, err := svc.Stats(ctx, admin, ptrInt64(appID), ticketdomain.KindFeedback)
	if err != nil || stats.Rejected != 1 || stats.Processing != 1 || stats.Pending != 0 {
		t.Fatalf("统计应有 1 条不采纳、1 条处理中，得到 %+v / %v", stats, err)
	}

	// ── 重开：不采纳改回处理中算重开 ──
	reopened, err := svc.ChangeStatus(ctx, admin, rejected, ticketdomain.StatusCommand{Status: ticketdomain.StatusProcessing})
	if err != nil || reopened.ReopenedCount != 1 || reopened.ClosedAt != nil {
		t.Fatalf("不采纳改回处理中应算重开：%+v / %v", reopened, err)
	}

	// ── 回复并标为已解决：走正式流转，记解决时间 ──
	if _, err := svc.ReplyByAdmin(ctx, admin, ticketdomain.ReplyCommand{TicketID: waiting, Content: "已修复", NextStatus: ticketdomain.StatusResolved}); err != nil {
		t.Fatal(err)
	}
	solved, err := pg.GetTicketByID(ctx, waiting)
	if err != nil || solved.Status != ticketdomain.StatusResolved || solved.ResolvedAt == nil {
		t.Fatalf("回复并标为已解决应记解决时间：%+v / %v", solved, err)
	}
	// 回复并改为等待中：随消息落库
	if _, err := svc.ReplyByAdmin(ctx, admin, ticketdomain.ReplyCommand{TicketID: rejected, Content: "请补充机型", NextStatus: ticketdomain.StatusPending}); err != nil {
		t.Fatal(err)
	}
	if asked, err := pg.GetTicketByID(ctx, rejected); err != nil || asked.Status != ticketdomain.StatusPending {
		t.Fatalf("回复并改为等待中失败：%+v / %v", asked, err)
	}
}

