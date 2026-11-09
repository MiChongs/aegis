package service

import (
	"strings"
	"testing"
)

// 提过工单的用户必须能被删除（管理员删除与账号注销共用 DeleteUserByApp）：
// 工单留档、提单人置空、联系方式清空；新建工单仍然必须带提单人（迁移 000151）。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestTicketRequesterDeletedIntegration
func TestTicketRequesterDeletedIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newDeletionTestEnv(t, ctx, pool)

	bob := env.recovery.user(t, "bob", "Delete-Me#2026", "bob@example.com", "13900139000")
	other := env.recovery.user(t, "carol", "Delete-Me#2026", "carol@example.com", "13700137000")

	insert := func(no string, requester int64, contact string) int64 {
		t.Helper()
		var id int64
		if err := pool.QueryRow(ctx, `
			INSERT INTO tickets (ticket_no, appid, requester_type, requester_user_id, requester_name, requester_contact, title)
			VALUES ($1, $2, 'user', $3, 'bob', $4, '登录不上') RETURNING id`,
			no, env.appID, requester, contact).Scan(&id); err != nil {
			t.Fatalf("新建工单 %s：%v", no, err)
		}
		return id
	}
	bobTicket := insert("T-DEL-1", bob, "bob@example.com")
	carolTicket := insert("T-DEL-2", other, "carol@example.com")
	if _, err := pool.Exec(ctx, `
		INSERT INTO ticket_messages (ticket_id, author_type, author_user_id, author_name, content)
		VALUES ($1, 'requester', $2, 'bob', '还是登录不上')`, bobTicket, bob); err != nil {
		t.Fatal(err)
	}

	// 删除前曾因 ck_tickets_requester 整体回滚
	if err := env.pg.DeleteUserByApp(ctx, env.appID, bob); err != nil {
		t.Fatalf("提过工单的用户应能删除：%v", err)
	}

	var (
		userID  *int64
		name    string
		contact string
		msgs    int
	)
	if err := pool.QueryRow(ctx, `SELECT requester_user_id, requester_name, requester_contact FROM tickets WHERE id = $1`, bobTicket).
		Scan(&userID, &name, &contact); err != nil {
		t.Fatalf("工单应留档：%v", err)
	}
	if userID != nil || name != "bob" || contact != "" {
		t.Fatalf("提单人应置空、姓名快照保留、联系方式清空：user=%v name=%q contact=%q", userID, name, contact)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ticket_messages WHERE ticket_id = $1`, bobTicket).Scan(&msgs); err != nil || msgs != 1 {
		t.Fatalf("工单会话应留档：%d / %v", msgs, err)
	}
	if err := pool.QueryRow(ctx, `SELECT requester_contact FROM tickets WHERE id = $1`, carolTicket).Scan(&contact); err != nil || contact != "carol@example.com" {
		t.Fatalf("别人的工单不受影响：%q / %v", contact, err)
	}

	// 提单人被删后，工单的其它更新照常
	if _, err := pool.Exec(ctx, `UPDATE tickets SET status = 'closed' WHERE id = $1`, bobTicket); err != nil {
		t.Fatalf("提单人已删除的工单应能继续处理：%v", err)
	}

	// 新建工单仍必须带提单人；类型与另一侧 ID 仍互斥
	for _, tc := range []struct{ name, sql string }{
		{"user 型缺 user_id", `INSERT INTO tickets (ticket_no, appid, requester_type, title) VALUES ('T-BAD-1', $1, 'user', 'x')`},
		{"admin 型缺 admin_id", `INSERT INTO tickets (ticket_no, appid, requester_type, title) VALUES ('T-BAD-2', $1, 'admin', 'x')`},
		{"user 型改成 admin 型", `UPDATE tickets SET requester_type = 'admin' WHERE appid = $1 AND ticket_no = 'T-DEL-2'`},
	} {
		_, err := pool.Exec(ctx, tc.sql, env.appID)
		if err == nil || !strings.Contains(err.Error(), "ck_tickets_requester") && !strings.Contains(err.Error(), "requester_") {
			t.Fatalf("%s 应被拒，得到 %v", tc.name, err)
		}
	}
}
