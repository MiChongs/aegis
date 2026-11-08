package service

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 抽奖吞吐压测（默认跳过）：N 个人同时抽，每人 M 次，连接池 40 条（与生产默认的上限一致），
// 报每秒抽奖数与延迟分位。
//
// AEGIS_LOTTERY_LOAD=1 AEGIS_LOTTERY_LOAD_USERS=2000 AEGIS_LOTTERY_LOAD_DRAWS=5 [AEGIS_LOTTERY_LOAD_BATCH=1] \
// AEGIS_TEST_PG_DSN=... go test ./internal/service -run TestLotteryLoad -v
func TestLotteryLoad(t *testing.T) {
	if os.Getenv("AEGIS_LOTTERY_LOAD") == "" {
		t.Skip("压测：设置 AEGIS_LOTTERY_LOAD=1 才跑")
	}
	ctx, base := openTestDatabase(t)
	config := base.Config()
	config.MaxConns = 40
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	users := envInt("AEGIS_LOTTERY_LOAD_USERS", 1000)
	draws := envInt("AEGIS_LOTTERY_LOAD_DRAWS", 5)
	// AEGIS_LOTTERY_LOAD_BATCH=1：每人一次十连抽（draws 次），而不是 draws 次单抽
	batch := os.Getenv("AEGIS_LOTTERY_LOAD_BATCH") != ""
	if batch && draws > lotterydomain.MaxBatchDraws {
		draws = lotterydomain.MaxBatchDraws
	}

	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40500)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-load') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
			Weight: 10, Quantity: -1})
	}
	prizes[0] = lotterydomain.PrizeInput{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1},
		Weight: 30, Quantity: -1, Fallback: true}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 10},
		Weight: 5, Quantity: 100_000, DailyLimit: 50_000}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "压测", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{
			WinRate: &lotterydomain.WinRateControl{Target: 0.4, Gain: 0.5},
			Budget:  &lotterydomain.Budget{DailyPoints: 100_000_000},
			Risk: &lotterydomain.RiskGuard{Rules: []lotterydomain.RiskRule{
				{Signal: lotterydomain.RiskSignalDeviceAccounts, Op: lotterydomain.RiskGTE, Threshold: 5, Action: lotterydomain.RiskFloor},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (appid, account)
SELECT $1, 'load-' || g FROM generate_series(1, $2::int) g`, appID, users); err != nil {
		t.Fatalf("建用户失败：%v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted)
SELECT $1, id, $2::bigint, $2::bigint FROM users WHERE appid = $1`, appID, draws); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT id FROM users WHERE appid = $1 ORDER BY id`, appID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, users)
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()

	latencies := make([]time.Duration, 0, users*draws)
	var mu sync.Mutex
	var failed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for index, userID := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]time.Duration, 0, draws)
			req := LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activity.ID,
				Source: lotterydomain.SourceBonus, DeviceID: fmt.Sprintf("load-dev-%d", index/3),
				ClientIP: fmt.Sprintf("10.%d.%d.%d", index/65536%256, index/256%256, index%256)}
			if batch {
				// 十连抽：每人一次抽完 draws 次（draws 不超过 10）
				begin := time.Now()
				result, err := lottery.DrawBatch(ctx, LotteryBatchRequest{LotteryDrawRequest: req, Count: draws})
				if err != nil || len(result.Draws) != draws {
					failed.Add(1)
				} else {
					for range draws {
						local = append(local, time.Since(begin))
					}
				}
			} else {
				for i := 0; i < draws; i++ {
					begin := time.Now()
					if _, err := lottery.Draw(ctx, req); err != nil {
						failed.Add(1)
						continue
					}
					local = append(local, time.Since(begin))
				}
			}
			mu.Lock()
			latencies = append(latencies, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		return latencies[min(len(latencies)-1, int(float64(len(latencies))*p))]
	}
	mode := "单抽"
	if batch {
		mode = "十连抽"
	}
	t.Logf("%s：%d 人 × %d 次 = %d 次抽奖，失败 %d，用时 %v，%.0f 次/秒；延迟 p50 %v p95 %v p99 %v", mode,
		users, draws, len(latencies), failed.Load(), elapsed, float64(len(latencies))/elapsed.Seconds(),
		pct(0.50), pct(0.95), pct(0.99))
	if failed.Load() > 0 {
		t.Fatalf("不同的人并发抽奖不应失败，失败 %d 次", failed.Load())
	}
}

func envInt(name string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(name)); err == nil && value > 0 {
		return value
	}
	return fallback
}
