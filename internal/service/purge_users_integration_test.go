package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 批量删号：一条查询取登录名、一个事务删整批，钩子按批收到整批；不存在的 id 略过；
// 外加守门：从 users 级联下去的每个外键列都要有以它开头的索引，否则删号会把子表整表扫一遍。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPurgeUsersIntegration
func TestPurgeUsersIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newDeletionTestEnv(t, ctx, pool)

	// ── 守门：级联外键列都有索引 ──
	rows, err := pool.Query(ctx, `
WITH RECURSIVE fks AS (
  SELECT c.conrelid, c.conkey, c.confdeltype, 1 AS depth
  FROM pg_constraint c WHERE c.contype = 'f' AND c.confrelid = 'users'::regclass
  UNION
  SELECT c.conrelid, c.conkey, c.confdeltype, f.depth + 1
  FROM pg_constraint c JOIN fks f ON c.confrelid = f.conrelid
  WHERE c.contype = 'f' AND f.confdeltype = 'c' AND f.depth < 4
)
SELECT DISTINCT f.conrelid::regclass::text || '.' || a.attname
FROM fks f JOIN pg_attribute a ON a.attrelid = f.conrelid AND a.attnum = f.conkey[1]
WHERE NOT EXISTS (
  SELECT 1 FROM pg_index i WHERE i.indrelid = f.conrelid AND i.indkey[0] = f.conkey[1]
   AND (i.indpred IS NULL OR pg_get_expr(i.indpred, i.indrelid) = '(' || quote_ident(a.attname) || ' IS NOT NULL)'))
ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatal(err)
		}
		missing = append(missing, col)
	}
	rows.Close()
	if len(missing) > 0 {
		t.Fatalf("这些外键列引用 users（或其级联子表）却没有以它开头的索引，删号时会整表扫描，请在迁移里补上：\n%s", strings.Join(missing, "\n"))
	}

	// ── 批量删除 ──
	var ids []int64
	for i := range 5 {
		ids = append(ids, env.recovery.user(t, fmt.Sprintf("purge%d", i), "Delete-Me#2026", fmt.Sprintf("purge%d@example.com", i), fmt.Sprintf("1390013900%d", i)))
	}
	keep := env.recovery.user(t, "keeper", "Delete-Me#2026", "keeper@example.com", "13700137009")
	if _, err := pool.Exec(ctx, `
		INSERT INTO tickets (ticket_no, appid, requester_type, requester_user_id, requester_name, requester_contact, title)
		VALUES ('T-PURGE-1', $1, 'user', $2, 'p0', 'purge0@example.com', 'x'), ('T-PURGE-2', $1, 'user', $3, 'k', 'keeper@example.com', 'y')`,
		env.appID, ids[0], keep); err != nil {
		t.Fatal(err)
	}

	users := NewUserService(zap.NewNop(), env.pg, env.sessions, nil, nil)
	var mu sync.Mutex
	var prepared, finished [][]int64
	users.AddPurgeHook(func(_ context.Context, appID int64, targets []pgrepo.PurgeTarget) func(context.Context, []pgrepo.PurgeTarget) {
		mu.Lock()
		prepared = append(prepared, purgeIDs(targets))
		mu.Unlock()
		for _, tg := range targets {
			if tg.Account == "" {
				t.Errorf("钩子应拿到登录名：%+v", tg)
			}
		}
		return func(_ context.Context, deleted []pgrepo.PurgeTarget) {
			mu.Lock()
			finished = append(finished, purgeIDs(deleted))
			mu.Unlock()
		}
	})

	request := append(slices.Clone(ids), ids[1], 0, -3, 987654321) // 重复、非法、不存在的都要略过
	started := time.Now()
	deleted, err := users.PurgeUsers(ctx, env.appID, request)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("批量删除 %d 个账号用时 %s", len(deleted), time.Since(started))
	slices.Sort(deleted)
	if !slices.Equal(deleted, ids) {
		t.Fatalf("应删掉 %v，得到 %v", ids, deleted)
	}
	if len(prepared) != 1 || !slices.Equal(prepared[0], ids) || len(finished) != 1 || !slices.Equal(finished[0], ids) {
		t.Fatalf("钩子应按批调用一次：prepared=%v finished=%v", prepared, finished)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY($1)`, ids).Scan(&left); err != nil || left != 0 {
		t.Fatalf("账号应已删除：%d / %v", left, err)
	}
	var contact string
	if err := pool.QueryRow(ctx, `SELECT requester_contact FROM tickets WHERE ticket_no = 'T-PURGE-1'`).Scan(&contact); err != nil || contact != "" {
		t.Fatalf("被删账号的工单联系方式应清空：%q / %v", contact, err)
	}
	if err := pool.QueryRow(ctx, `SELECT requester_contact FROM tickets WHERE ticket_no = 'T-PURGE-2'`).Scan(&contact); err != nil || contact != "keeper@example.com" {
		t.Fatalf("别人的工单不受影响：%q / %v", contact, err)
	}

	// 再删一次：都不存在，不报错也不调钩子
	again, err := users.PurgeUsers(ctx, env.appID, ids)
	if err != nil || len(again) != 0 || len(prepared) != 1 {
		t.Fatalf("重复删除应静默略过：%v / %v / hooks=%d", again, err, len(prepared))
	}
	// 单个删除走同一路径，不存在时报错
	if err := users.PurgeUser(ctx, env.appID, ids[0]); err == nil {
		t.Fatal("删除不存在的账号应报错")
	}
	if err := users.PurgeUser(ctx, env.appID, keep); err != nil {
		t.Fatal(err)
	}
}

func purgeIDs(ts []pgrepo.PurgeTarget) []int64 {
	out := make([]int64, len(ts))
	for i, t := range ts {
		out[i] = t.UserID
	}
	slices.Sort(out)
	return out
}
