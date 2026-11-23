package service

import (
	"testing"

	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
)

// 反馈台左侧「按应用」一栏：计数按应用分组、自己创建的应用排前面且零条也列出；
// 列表的 owned / ownedFirst 只看我建的应用、或把我建的应用排在前面。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestTicketAppSummaryIntegration
func TestTicketAppSummaryIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	repo := pgrepo.New(pool)

	var me, other int64
	for _, row := range []struct {
		account string
		id      *int64
	}{{"zz_tas_me", &me}, {"zz_tas_other", &other}} {
		if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
			VALUES ($1, 'x', $1, FALSE) RETURNING id`, row.account).Scan(row.id); err != nil {
			t.Fatal(err)
		}
	}
	const mineApp, otherApp, emptyApp = int64(970001), int64(970002), int64(970003)
	for _, app := range []struct {
		id    int64
		name  string
		owner int64
	}{{mineApp, "我的应用", me}, {otherApp, "别人的应用", other}, {emptyApp, "还没有反馈", me}} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name, created_by) VALUES ($1, $2, $3)`, app.id, app.name, app.owner); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM apps WHERE id = ANY($1)`, []int64{mineApp, otherApp, emptyApp})
		_, _ = pool.Exec(ctx, `DELETE FROM admin_accounts WHERE id = ANY($1)`, []int64{me, other})
	})

	insert := func(no string, app int64, status string, assignee *int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO tickets (ticket_no, kind, appid, requester_type, requester_admin_id, requester_name, title, status, assignee_admin_id)
			VALUES ($1, 'feedback', $2, 'admin', $3, 'x', '反馈', $4, $5)`, no, app, other, status, assignee); err != nil {
			t.Fatalf("新建 %s：%v", no, err)
		}
	}
	insert("ZZTAS-1", otherApp, "open", nil)
	insert("ZZTAS-2", otherApp, "processing", nil)
	insert("ZZTAS-3", otherApp, "closed", nil)
	insert("ZZTAS-4", mineApp, "open", &me)
	insert("ZZTAS-5", mineApp, "resolved", nil)

	scope := ticketdomain.Scope{All: true, AdminID: me}
	items, err := repo.TicketAppSummaries(ctx, ticketdomain.KindFeedback, scope)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]ticketdomain.AppSummary{}
	for _, item := range items {
		got[item.AppID] = item
	}
	if s := got[otherApp]; s.Owned || s.Total != 3 || s.Active != 2 || s.Unaccepted != 1 || s.Unassigned != 2 {
		t.Fatalf("别人的应用计数不对：%+v", s)
	}
	if s := got[mineApp]; !s.Owned || s.Total != 2 || s.Active != 1 || s.Mine != 1 || s.LastAt == nil {
		t.Fatalf("我的应用计数不对：%+v", s)
	}
	if s, ok := got[emptyApp]; !ok || !s.Owned || s.Total != 0 || s.LastAt != nil {
		t.Fatalf("我建的空应用应列出且计数为 0：%+v ok=%v", s, ok)
	}
	if _, err := repo.TicketAppSummaries(ctx, ticketdomain.KindFeedback, ticketdomain.Scope{AdminID: me, AppIDs: []int64{mineApp}}); err != nil {
		t.Fatalf("应用范围下汇总：%v", err)
	}

	owned, total, err := repo.ListTickets(ctx, ticketdomain.ListQuery{Kind: "feedback", Owned: true, IncludeClosed: true, Limit: 20}, scope)
	if err != nil || total != 2 {
		t.Fatalf("只看我的应用应有 2 条：%d / %v", total, err)
	}
	for _, item := range owned {
		if item.AppID != mineApp {
			t.Fatalf("混进了别的应用：%+v", item)
		}
	}
	first, _, err := repo.ListTickets(ctx, ticketdomain.ListQuery{Kind: "feedback", OwnedFirst: true, IncludeClosed: true, Limit: 200}, scope)
	if err != nil {
		t.Fatal(err)
	}
	seenOther := false
	for _, item := range first {
		if item.AppID != mineApp && item.AppID != otherApp {
			continue
		}
		if item.AppID == otherApp {
			seenOther = true
		} else if seenOther {
			t.Fatalf("我的应用应排在别人的前面")
		}
	}
	if _, err := repo.ListTicketsForExport(ctx, ticketdomain.ListQuery{Kind: "feedback", OwnedFirst: true}, scope, 10); err != nil {
		t.Fatalf("导出排序：%v", err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM tickets WHERE ticket_no LIKE 'ZZTAS-%'`)
}
