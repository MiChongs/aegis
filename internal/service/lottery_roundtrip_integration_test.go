package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// roundTripTracer 数往返：单发的语句一次一个，批量管道一批一个。
type roundTripTracer struct {
	queries atomic.Int64
	batches atomic.Int64
	inBatch atomic.Int64
}

func (t *roundTripTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if t.inBatch.Load() == 0 {
		t.queries.Add(1)
	}
	return ctx
}
func (t *roundTripTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func (t *roundTripTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	t.batches.Add(1)
	t.inBatch.Add(1)
	return ctx
}
func (t *roundTripTracer) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}
func (t *roundTripTracer) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {
	t.inBatch.Add(-1)
}

func (t *roundTripTracer) reset() { t.queries.Store(0); t.batches.Store(0) }
func (t *roundTripTracer) total() int64 {
	return t.queries.Load() + t.batches.Load()
}

// 抽奖事务的往返次数（真实 Postgres）：单抽不超过 14 次往返（含开始与提交），
// 十连抽在一个事务里、往返远少于十次单抽之和。数字写在 docs/lottery.md「容量与并发」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryRoundTrips
func TestLotteryRoundTrips(t *testing.T) {
	ctx, base := openTestDatabase(t)
	tracer := &roundTripTracer{}
	config := base.Config()
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40700)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-rt') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	user := insertTestUser(t, ctx, base, appID, "rt-user", "0")
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, 100, 100)`,
		appID, user); err != nil {
		t.Fatal(err)
	}
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
			Weight: 1, Quantity: -1, Fallback: position == 0})
	}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "往返", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{
			WinRate: &lotterydomain.WinRateControl{Target: 0.3, Gain: 0.5},
			Risk: &lotterydomain.RiskGuard{Rules: []lotterydomain.RiskRule{
				{Signal: lotterydomain.RiskSignalDeviceAccounts, Op: lotterydomain.RiskGTE, Threshold: 5, Action: lotterydomain.RiskFloor},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := lotterydomain.DrawInput{AppID: appID, UserID: user, ActivityID: activity.ID, Source: lotterydomain.SourceBonus,
		DeviceID: "rt-dev", ClientIP: "192.0.2.9", DayStart: lotteryDayStart(time.Now()), Now: time.Now()}

	// 第一次建状态行（每人一次的慢路径），不计
	if _, err := pg.DrawLottery(ctx, input); err != nil {
		t.Fatal(err)
	}
	tracer.reset()
	input.Now = time.Now()
	if _, err := pg.DrawLottery(ctx, input); err != nil {
		t.Fatal(err)
	}
	single := tracer.total()

	tracer.reset()
	ids := make([]string, lotterydomain.MaxBatchDraws)
	for index := range ids {
		ids[index] = "rt-batch-" + string(rune('a'+index))
	}
	input.Now = time.Now()
	outcomes, stop, err := pg.DrawLotteryBatch(ctx, input, ids)
	if err != nil || stop != nil || len(outcomes) != len(ids) {
		t.Fatalf("十连抽应全部抽成，得到 %d 次 %v %v", len(outcomes), stop, err)
	}
	batch := tracer.total()
	t.Logf("单抽 %d 次往返；十连抽 %d 次往返（十次单抽 %d 次）", single, batch, single*10)
	// 奖盘全是谢谢参与（发奖不碰库），往返数是确定的
	if single > 9 {
		t.Fatalf("单抽应不超过 9 次往返，得到 %d", single)
	}
	if batch > single+9*4 {
		t.Fatalf("十连抽后 9 次每次应不超过 4 次往返（共 %d），得到 %d", single+9*4, batch)
	}

	// ── 奖品本身是抽奖次数：用次数连抽，发奖时汇总与批次已经一致，不会误补「早期次数」批次 ──
	ticketPrizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		ticketPrizes = append(ticketPrizes, lotterydomain.PrizeInput{Position: position,
			Reward: lotterydomain.Reward{Type: lotterydomain.KindLotteryDraws, Amount: 2}, Weight: 1, Quantity: -1,
			Fallback: position == 0})
	}
	ticketActivity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "抽次数", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		PityPosition: -1, Prizes: ticketPrizes, Operator: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	legacyGrants := func() int64 {
		t.Helper()
		var n int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draw_ledger WHERE appid = $1 AND user_id = $2
AND origin = 'legacy' AND kind = 'grant'`, appID, user).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var before int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`, appID, user).
		Scan(&before); err != nil {
		t.Fatal(err)
	}
	legacyBefore := legacyGrants()
	result, err := lottery.DrawBatch(ctx, LotteryBatchRequest{LotteryDrawRequest: LotteryDrawRequest{AppID: appID, UserID: user,
		ActivityID: ticketActivity.ID, Source: lotterydomain.SourceBonus, RequestID: "rt-tickets"}, Count: 10})
	if err != nil || len(result.Draws) != 10 {
		t.Fatalf("用次数连抽 10 次应全部抽成，得到 %+v %v", result, err)
	}
	var balance, open int64
	if err := pool.QueryRow(ctx, `SELECT
  (SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2),
  (SELECT COALESCE(SUM(remaining), 0) FROM lottery_draw_lots WHERE appid = $1 AND user_id = $2)`,
		appID, user).Scan(&balance, &open); err != nil {
		t.Fatal(err)
	}
	if balance != before-10+20 || balance != open {
		t.Fatalf("连抽 10 次、每次得 2 次：余额应为 %d 且等于批次之和，得到余额 %d、批次 %d", before+10, balance, open)
	}
	if added := legacyGrants() - legacyBefore; added != 0 {
		t.Fatalf("连抽期间不应补「早期次数」，补了 %d 条", added)
	}
}
