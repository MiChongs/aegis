package service

import (
	"testing"
	"time"

	appdomain "aegis/internal/domain/app"
	pgrepo "aegis/internal/repository/postgres"
)

// 签到统计与明细的 SQL 集成测试：窗口口径、时段分布、连签分段、排行与奖励类型过滤。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestSignInStatsIntegration
func TestSignInStatsIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)

	const appID = 9701
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'signin-stats-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	loc := time.FixedZone("CST", 8*3600)
	today := time.Date(2026, 10, 11, 0, 0, 0, 0, loc)
	day := func(offset int) time.Time { return today.AddDate(0, 0, offset) }

	newUser := func(account string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled) VALUES ($1, $2, TRUE) RETURNING id`, appID, account).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	alice, bob, carol := newUser("alice"), newUser("bob"), newUser("carol")

	// signed_at 用本地 08:30 与 21:10，验证时段按 +08:00 折算
	sign := func(user int64, offset int, hour, minute int, integral, experience int64, consecutive int, bonus string) {
		at := time.Date(day(offset).Year(), day(offset).Month(), day(offset).Day(), hour, minute, 0, 0, loc)
		if _, err := pool.Exec(ctx, `INSERT INTO daily_signins (user_id, appid, signed_at, sign_date, integral_reward, experience_reward, consecutive_days, bonus_type)
VALUES ($1, $2, $3, $4::date, $5, $6, $7, NULLIF($8, ''))`, user, appID, at, day(offset).Format("2006-01-02"), integral, experience, consecutive, bonus); err != nil {
			t.Fatal(err)
		}
	}
	sign(alice, 0, 8, 30, 30, 100, 7, "weekly")
	sign(alice, -1, 8, 30, 20, 90, 6, "")
	sign(bob, 0, 21, 10, 10, 20, 1, "")
	sign(bob, -1, 21, 10, 10, 20, 1, "weekend")
	sign(carol, -30, 8, 30, 10, 20, 1, "") // 窗口外

	stat := func(user int64, consecutive int, total int64, last time.Time) {
		if _, err := pool.Exec(ctx, `INSERT INTO sign_stats (user_id, appid, last_sign_date, consecutive_days, total_sign_days) VALUES ($1, $2, $3::date, $4, $5)`,
			user, appID, last.Format("2006-01-02"), consecutive, total); err != nil {
			t.Fatal(err)
		}
	}
	stat(alice, 7, 40, day(0))
	stat(bob, 1, 2, day(-1)) // 昨天签过，连签仍可延续
	stat(carol, 1, 1, day(-30))

	stats, err := pg.GetAppSignInStats(ctx, appID, today, day(-13), today)
	if err != nil {
		t.Fatal(err)
	}
	check := func(name string, got, want int64) {
		t.Helper()
		if got != want {
			t.Errorf("%s：得到 %d，期望 %d", name, got, want)
		}
	}
	check("全量记录", stats.TotalSignRecords, 5)
	check("今日", stats.TodaySignCount, 2)
	check("昨日", stats.YesterdaySignCount, 2)
	check("窗口签到", stats.WindowSignCount, 4)
	check("窗口人数", stats.WindowUniqueUsers, 2)
	check("窗口积分", stats.WindowIntegralReward, 70)
	check("窗口经验", stats.WindowExperienceReward, 230)
	check("活跃连签", stats.ActiveStreakUsers, 2)
	check("08 点", stats.HourDistribution[8], 2)
	check("21 点", stats.HourDistribution[21], 2)
	if len(stats.Trend) != 14 {
		t.Fatalf("趋势应有 14 天，得到 %d", len(stats.Trend))
	}
	last := stats.Trend[13]
	check("今日趋势人数", last.Count, 2)
	check("今日趋势积分", last.IntegralReward, 40)
	check("1 天分段", stats.StreakBuckets[0].Count, 1)
	check("7-13 天分段", stats.StreakBuckets[2].Count, 1)
	if len(stats.TopStreaks) != 2 || stats.TopStreaks[0].Account != "alice" || stats.TopStreaks[0].ConsecutiveDays != 7 {
		t.Fatalf("连签排行不符：%+v", stats.TopStreaks)
	}
	bonus := map[string]int64{}
	for _, item := range stats.BonusTypes {
		bonus[item.BonusType] = item.Count
	}
	check("normal 类型", bonus["normal"], 2)
	check("weekly 类型", bonus["weekly"], 1)

	normal, total, err := pg.ListAppSignInRecords(ctx, appID, appdomain.AppSignInRecordQuery{BonusType: "normal", Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	check("normal 过滤（含空值）", total, 3)
	if len(normal) != 3 {
		t.Fatalf("normal 过滤应返回 3 条，得到 %d", len(normal))
	}
	_, total, err = pg.ListAppSignInRecords(ctx, appID, appdomain.AppSignInRecordQuery{BonusType: "weekend", Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	check("weekend 过滤", total, 1)
}
