package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
)

// 同一小时聚合两次（多副本下负责实例换手、或重启后重算）只留一条：周 / 月曲线上不能出现重影。
func TestAppendHourlyHistoryReplacesSameHour(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	repo := NewMonitorRepository(client, "t")
	ctx := context.Background()

	const hour = int64(1_800_000_000_000)
	if err := repo.AppendHourlyHistory(ctx, "system:db", MonitorHistoryPoint{T: hour, S: "available", L: 3}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendHourlyHistory(ctx, "system:db", MonitorHistoryPoint{T: hour, S: "degraded", L: 9}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AppendHourlyHistory(ctx, "system:db", MonitorHistoryPoint{T: hour + 3_600_000, S: "available", L: 1}); err != nil {
		t.Fatal(err)
	}
	raw, err := client.ZRange(ctx, repo.hourlyHistoryKey("system:db"), 0, -1).Result()
	if err != nil {
		t.Fatal(err)
	}
	points := parseHistoryPoints(raw)
	if len(points) != 2 {
		t.Fatalf("应只有两个小时点，实得 %d：%+v", len(points), points)
	}
	if points[0].T != hour || points[0].S != "degraded" || points[0].L != 9 {
		t.Fatalf("同一小时应以后一次聚合为准，实得 %+v", points[0])
	}
}
