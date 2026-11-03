package service

import (
	"context"
	stderrors "errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 管理员调整积分 / 经验的端到端集成测试：真实 Postgres（全部迁移跑两遍）。
// 「加减与余额不足 → 数量边界 → 上限与溢出 → 请求编号幂等（含并发）→ 编号挪用 →
// 经验只增不减 → 跨应用 → 批量（部分成功、重放）→ 数据库约束兜底 → 流水与余额对平」。
//
// AEGIS_TEST_PG_DSN=postgres://aegis:***@127.0.0.1:15432/aegis_points_test?sslmode=disable go test ./internal/service -run TestPointsAdjustIntegration
func TestPointsAdjustIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	points := NewPointsService(zap.NewNop(), pg, nil)
	opts := pointdomain.AdminAdjustOptions{AdminID: 1, AdminAccount: "root", ClientIP: "127.0.0.1"}

	const appID, otherApp = int64(41000), int64(41001)
	for _, id := range []int64{appID, otherApp} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'points-adjust-test') ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}
	alice := insertTestUser(t, ctx, pool, appID, "pa-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "pa-bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "pa-carol", "0")
	outsider := insertTestUser(t, ctx, pool, otherApp, "pa-outsider", "0")

	adjust := func(userID, amount int64, requestID string) (*pointdomain.IntegralAdjustResult, error) {
		return points.AdminAdjustIntegral(ctx, pointdomain.AdjustInput{AppID: appID, UserID: userID, Amount: amount, RequestID: requestID}, opts)
	}

	// 加减与余额不足
	if r, err := adjust(alice, 100, ""); err != nil || r.AfterAmount != 100 || r.OperationType != "add" {
		t.Fatalf("增加 100：%+v %v", r, err)
	}
	if r, err := adjust(alice, -30, ""); err != nil || r.AfterAmount != 70 || r.OperationType != "consume" {
		t.Fatalf("扣减 30：%+v %v", r, err)
	}
	_, err := adjust(alice, -71, "")
	expectCode(t, err, 40009, "当前 70")
	expectBalance(t, ctx, pool, alice, "integral", 70)

	// 数量边界
	_, err = adjust(alice, 0, "")
	expectCode(t, err, 40000, "不能为 0")
	_, err = adjust(alice, pointdomain.MaxAdminAdjustAmount+1, "")
	expectCode(t, err, 40000, "不能超过")

	// 上限与溢出：余额离上限只差 5，再加 10 必须拒绝且余额不动
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = $1 WHERE id = $2`, pointdomain.MaxBalance-5, bob); err != nil {
		t.Fatal(err)
	}
	_, err = adjust(bob, 10, "")
	expectCode(t, err, 40961, "超过上限")
	expectBalance(t, ctx, pool, bob, "integral", pointdomain.MaxBalance-5)
	if _, err := adjust(bob, 5, ""); err != nil {
		t.Fatalf("加到恰好上限应成功：%v", err)
	}

	// 请求编号幂等：同一编号只生效一次，重放返回首次结果
	first, err := adjust(carol, 50, "req-carol-0001")
	if err != nil || first.Replayed {
		t.Fatalf("首次请求：%+v %v", first, err)
	}
	again, err := adjust(carol, 50, "req-carol-0001")
	if err != nil || !again.Replayed || again.TransactionNo != first.TransactionNo || again.AfterAmount != 50 {
		t.Fatalf("重放应返回首次结果：%+v %v", again, err)
	}
	expectBalance(t, ctx, pool, carol, "integral", 50)

	// 并发重复提交：十个相同编号的请求同时到达，只能加一次
	var wg sync.WaitGroup
	replays := make(chan bool, 10)
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := adjust(carol, 7, "req-carol-race")
			if err != nil {
				t.Errorf("并发请求失败：%v", err)
				return
			}
			replays <- r.Replayed
		}()
	}
	wg.Wait()
	close(replays)
	applied := 0
	for replayed := range replays {
		if !replayed {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("并发相同编号应只生效一次，实际 %d 次", applied)
	}
	expectBalance(t, ctx, pool, carol, "integral", 57)

	// 编号被挪用到另一笔不同的调整
	_, err = adjust(carol, 99, "req-carol-0001")
	expectCode(t, err, 40921, "请求编号")
	_, err = adjust(alice, 50, "req-carol-0001")
	expectCode(t, err, 40921, "请求编号")

	// 经验：增加、重放；管理员可扣减（不足整笔拒绝），内部自动路径不能扣
	exp, err := points.AdminAdjustExperience(ctx, pointdomain.AdjustInput{AppID: appID, UserID: alice, Amount: 20, RequestID: "req-exp-0001"}, opts)
	if err != nil || exp.AfterAmount != 20 {
		t.Fatalf("增加经验：%+v %v", exp, err)
	}
	expAgain, err := points.AdminAdjustExperience(ctx, pointdomain.AdjustInput{AppID: appID, UserID: alice, Amount: 20, RequestID: "req-exp-0001"}, opts)
	if err != nil || !expAgain.Replayed {
		t.Fatalf("经验重放：%+v %v", expAgain, err)
	}
	expectBalance(t, ctx, pool, alice, "experience", 20)
	deducted, err := points.AdminAdjustExperience(ctx, pointdomain.AdjustInput{AppID: appID, UserID: alice, Amount: -5}, opts)
	if err != nil || deducted.AfterAmount != 15 || deducted.OperationType != "consume" {
		t.Fatalf("扣减经验：%+v %v", deducted, err)
	}
	_, err = points.AdminAdjustExperience(ctx, pointdomain.AdjustInput{AppID: appID, UserID: alice, Amount: -100}, opts)
	expectCode(t, err, 40009, "经验不足")
	expectBalance(t, ctx, pool, alice, "experience", 15)
	_, err = points.AdjustUserExperience(ctx, alice, appID, -5, "内部扣减", opts)
	expectCode(t, err, 40000, "自动发放的经验不能为负数")

	// 跨应用：用户不属于该应用
	_, err = adjust(outsider, 10, "")
	expectCode(t, err, 40401, "不属于该应用")

	// 批量：部分成功（正常 / 超上限 / 不存在 / 重复 ID 去重）
	batch, err := points.BatchAdjustUserIntegral(ctx, []int64{alice, bob, 99999999, alice}, appID, 10, "add", "活动", "req-batch-0001", opts)
	if err != nil {
		t.Fatal(err)
	}
	if batch.RequestedCount != 3 || batch.SuccessCount != 1 || batch.FailedCount != 2 {
		t.Fatalf("批量结果不符：%+v", batch)
	}
	for _, f := range batch.Failures {
		if f.UserID == bob && !strings.Contains(f.Error, "上限") {
			t.Errorf("bob 应因超上限失败：%+v", f)
		}
		if f.UserID == 99999999 && f.Error != "用户不存在" {
			t.Errorf("不存在的用户：%+v", f)
		}
	}
	replayed, err := points.BatchAdjustUserIntegral(ctx, []int64{alice, bob, 99999999}, appID, 10, "add", "活动", "req-batch-0001", opts)
	if err != nil || !replayed.Replayed || replayed.SuccessCount != 1 {
		t.Fatalf("批量重放应返回首次结果：%+v %v", replayed, err)
	}
	expectBalance(t, ctx, pool, alice, "integral", 80)
	_, err = points.BatchAdjustUserIntegral(ctx, []int64{alice}, appID, 11, "add", "", "req-batch-0001", opts)
	expectCode(t, err, 40921, "请求编号")
	consume, err := points.BatchAdjustUserIntegral(ctx, []int64{alice, carol}, appID, 60, "consume", "", "", opts)
	if err != nil || consume.SuccessCount != 1 || consume.Failures[0].UserID != carol || !strings.Contains(consume.Failures[0].Error, "当前 57") {
		t.Fatalf("批量扣减：%+v %v", consume, err)
	}
	_, err = points.BatchAdjustUserIntegral(ctx, []int64{alice}, appID, -10, "add", "", "", opts)
	expectCode(t, err, 40000, "必须大于 0")

	// 数据库约束兜底：绕过应用层直接写负数或超上限，必须被拒
	for _, stmt := range []string{
		`UPDATE users SET integral = -1 WHERE id = $1`,
		`UPDATE users SET experience = -1 WHERE id = $1`,
		`UPDATE users SET integral = 9007199254740992 WHERE id = $1`,
	} {
		_, err := pool.Exec(ctx, stmt, alice)
		var pgErr *pgconn.PgError
		if !stderrors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s 应触发 CHECK 约束，实际 %v", stmt, err)
		}
	}

	// 流水与余额对平：每个用户的积分 = 其全部积分流水之和（bob 起始值是直接写库的，单独算）
	for _, id := range []int64{alice, carol} {
		var sum, balance int64
		if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM integral_transactions WHERE user_id = $1`, id).Scan(&sum); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, id).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		if sum != balance {
			t.Errorf("用户 %d 流水合计 %d 与余额 %d 不一致", id, sum, balance)
		}
	}
}

func expectCode(t *testing.T, err error, code int, contains string) {
	t.Helper()
	var appErr *apperrors.AppError
	if !stderrors.As(err, &appErr) {
		t.Fatalf("期望业务错误 %d（%s），实际 %v", code, contains, err)
	}
	if appErr.Code != code || !strings.Contains(appErr.Message, contains) {
		t.Fatalf("期望 %d 且含 %q，实际 %d %q", code, contains, appErr.Code, appErr.Message)
	}
}

func expectBalance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, column string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(ctx, `SELECT `+column+` FROM users WHERE id = $1`, userID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("用户 %d 的 %s 应为 %d，实际 %d", userID, column, want, got)
	}
}
