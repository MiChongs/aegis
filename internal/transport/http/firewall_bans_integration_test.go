package httptransport

import (
	"context"
	"os"
	"testing"

	firewalldomain "aegis/internal/domain/firewall"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// IP 封禁批量管理的端到端测试：真实 Postgres（迁移 000175）+ miniredis。
//
//	AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/aegis_fw_test go test ./internal/transport/http -run TestIPBanBatchIntegration
func TestIPBanBatchIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetRelayTestDatabase(t, ctx, pool)
	pg := pgrepo.New(pool)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rc := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	redis := redisrepo.NewIPBanRepository(rc, "it")
	svc := service.NewIPBanService(zap.NewNop(), pg, redis, nil)

	// ── 批量封禁：去重、非法、网段、已在封禁 ──
	if _, err := svc.BanIPWithOptions(ctx, "198.51.100.9", "先封一个", 0, 1, service.BanIPOptions{}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.BanIPs(ctx, service.BanIPsInput{
		IPs:    []string{"198.51.100.1", "198.51.100.2", " 198.51.100.1 ", "::ffff:198.51.100.3", "198.51.100.9", "not-an-ip", "10.0.0.0/8", "2001:db8::1"},
		Reason: "批量测试", Duration: 3600, Mode: firewalldomain.BanModeTarpit, DelayMs: 1500,
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Succeeded != 4 || res.Skipped != 1 || res.Failed != 2 {
		t.Fatalf("批量封禁结果不对：%+v", res)
	}
	if banned, _ := redis.IsBanned(ctx, "198.51.100.3"); !banned {
		t.Fatal("IPv4 映射地址应按 IPv4 封禁并写入 Redis")
	}
	if meta, _ := redis.GetBanMeta(ctx, "198.51.100.1"); meta == nil || meta.Mode != firewalldomain.BanModeTarpit || meta.DelayMs != 1500 {
		t.Fatalf("Redis 元信息应带模式与延迟：%+v", meta)
	}

	// ── Redis 同步不丢模式与延迟 ──
	mr.FlushAll()
	if err := svc.SyncBansToRedis(ctx); err != nil {
		t.Fatal(err)
	}
	if meta, _ := redis.GetBanMeta(ctx, "198.51.100.2"); meta == nil || meta.Mode != firewalldomain.BanModeTarpit || meta.DelayMs != 1500 {
		t.Fatalf("重启同步后应保留模式与延迟：%+v", meta)
	}

	// ── 筛选、计数、统计 ──
	tarpit := firewalldomain.IPBanFilter{Status: "active", Mode: firewalldomain.BanModeTarpit}
	if n, _ := svc.CountBans(ctx, tarpit); n != 4 {
		t.Fatalf("tarpit 生效中应为 4 条，得到 %d", n)
	}
	permanent := true
	if n, _ := svc.CountBans(ctx, firewalldomain.IPBanFilter{Status: "active", Permanent: &permanent}); n != 1 {
		t.Fatalf("永久封禁应为 1 条，得到 %d", n)
	}
	if n, _ := svc.CountBans(ctx, firewalldomain.IPBanFilter{Status: "active", Keyword: "198.51.100", ExpiringWithin: 7200}); n != 3 {
		t.Fatalf("前缀 198.51.100 且 2 小时内到期的应为 3 条，得到 %d", n)
	}
	stats, err := svc.Stats(ctx)
	if err != nil || stats.Active != 5 || stats.Permanent != 1 || stats.ExpiringSoon != 4 || len(stats.Daily) != 14 {
		t.Fatalf("概况不对：%+v %v", stats, err)
	}

	// ── 修改：改成永久、换模式 ──
	page, _ := svc.ListBans(ctx, firewalldomain.IPBanFilter{IP: "198.51.100.1", Status: "active"})
	if len(page.Items) != 1 {
		t.Fatalf("应能按 IP 找到：%+v", page)
	}
	id := page.Items[0].ID
	zero := int64(0)
	forbidden := firewalldomain.BanModeStealth404
	updated, err := svc.UpdateBan(ctx, id, firewalldomain.IPBanUpdate{Duration: &zero, Mode: &forbidden}, 2)
	if err != nil || updated.ExpiresAt != nil || updated.Mode != forbidden || updated.UpdatedBy == nil || *updated.UpdatedBy != 2 {
		t.Fatalf("修改结果不对：%+v %v", updated, err)
	}
	if ttl := mr.TTL("it:firewall:ban:198.51.100.1"); ttl < 24*3600*1e9 {
		t.Fatalf("改为永久后 Redis TTL 应变长，得到 %v", ttl)
	}

	// ── 按选中解封 ──
	out, err := svc.UnbanByIDs(ctx, []int64{id, id, 999999}, 1, "误封")
	if err != nil || out.Succeeded != 1 || out.Skipped != 1 {
		t.Fatalf("按选中解封：%+v %v", out, err)
	}
	if banned, _ := redis.IsBanned(ctx, "198.51.100.1"); banned {
		t.Fatal("解封后 Redis 应删除")
	}
	revoked, _ := pg.GetIPBan(ctx, id)
	if revoked.Status != "revoked" || revoked.RevokeNote != "误封" {
		t.Fatalf("解封记录：%+v", revoked)
	}

	// ── 按筛选解封：条数不符拒绝，相符执行 ──
	if _, err := svc.UnbanByFilter(ctx, tarpit, 99, 1, "清理"); err == nil {
		t.Fatal("确认条数不符必须拒绝")
	}
	out, err = svc.UnbanByFilter(ctx, tarpit, 3, 1, "清理")
	if err != nil || out.Succeeded != 3 {
		t.Fatalf("按筛选解封：%+v %v", out, err)
	}
	if _, err := svc.UnbanByFilter(ctx, firewalldomain.IPBanFilter{}, 0, 1, ""); err == nil {
		t.Fatal("不带任何条件、不带确认条数的「全部解封」必须拒绝")
	}

	// ── IP 查询 ──
	inspect, err := svc.Inspect(ctx, "198.51.100.9")
	if err != nil || !inspect.Blocked || inspect.Layer != "ip_ban" || inspect.ActiveBan == nil || len(inspect.History) != 1 {
		t.Fatalf("查询生效中的 IP：%+v %v", inspect, err)
	}
	inspect, err = svc.Inspect(ctx, "198.51.100.2")
	if err != nil || inspect.Blocked || len(inspect.History) != 1 || inspect.History[0].Status != "revoked" {
		t.Fatalf("查询已解封的 IP：%+v %v", inspect, err)
	}
	if _, err := svc.Inspect(ctx, "nope"); err == nil {
		t.Fatal("非法 IP 必须报错")
	}
}
