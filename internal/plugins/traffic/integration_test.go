package trafficplugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	trafficdomain "aegis/internal/domain/traffic"
	"aegis/internal/nativeplugin"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/traffic"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 全链路集成测试：真实 Postgres（必需）与 Redis（可选）。
//
//	AEGIS_TEST_PG_DSN=postgres://aegis:***@127.0.0.1:15432/aegis_traffic_test \
//	AEGIS_TEST_REDIS_ADDR=127.0.0.1:6379 AEGIS_TEST_REDIS_PASSWORD=*** \
//	go test ./internal/plugins/traffic -run TestTrafficIntegration -v
//
// 会清空库里的 traffic_* 与 native_plugins 表，务必指向专用的测试库。
func TestTrafficIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migration, err := os.ReadFile("../../../migrations/postgres/000148_traffic_management.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `CREATE TABLE IF NOT EXISTS platform_settings (
	setting_key VARCHAR(128) PRIMARY KEY, setting_value JSONB NOT NULL, updated_by BIGINT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	mustExec(t, pool, string(migration))
	mustExec(t, pool, string(migration)) // 迁移必须可重复执行
	mustExec(t, pool, `TRUNCATE traffic_settings, traffic_rules, traffic_revisions, traffic_metrics_minute, traffic_events, native_plugins`)
	mustExec(t, pool, `DELETE FROM platform_settings WHERE setting_key = 'security.traffic_ramp'`)
	// 旧版爬坡配置（无 tag 结构体直接序列化的形状）
	mustExec(t, pool, `INSERT INTO platform_settings (setting_key, setting_value) VALUES ('security.traffic_ramp',
	'{"Enabled":true,"BaselineRPS":200,"MaxRPS":800,"RampStepPct":25,"RampIntervalMs":500,"CooldownSeconds":10,"QueueSize":50,"QueueTimeoutMs":1000,"MaxConcurrent":0,"ExemptPathPrefixes":["/api/public"],"ExemptAdmin":true,"RetryAfterSeconds":5}')`)

	store := pgrepo.New(pool)
	var rdb *redis.Client
	if addr := os.Getenv("AEGIS_TEST_REDIS_ADDR"); addr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: addr, Password: os.Getenv("AEGIS_TEST_REDIS_PASSWORD")})
		defer rdb.Close()
		if err := rdb.Ping(ctx).Err(); err != nil {
			t.Fatalf("redis: %v", err)
		}
	}
	p := New(store, rdb, "aegistest:", zap.NewNop())
	host := nativeplugin.NewHost(store, zap.NewNop())
	host.Register(p)
	host.Boot(ctx)
	defer host.Shutdown(ctx)
	if !p.Running() {
		v, _ := host.Get(ID)
		t.Fatalf("plugin not running after boot: %+v", v)
	}

	// 1. 首次启动：旧配置导入成一条预热规则
	cfg, err := p.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Revision != 1 || len(cfg.Rules) != 1 || cfg.Rules[0].Warmup == nil || cfg.Rules[0].Warmup.BaselineRPS != 200 {
		t.Fatalf("legacy import: %+v", cfg)
	}
	if cfg.Settings.Response.RetryAfterSeconds != 5 || len(cfg.Settings.ExemptPaths) != 1 || cfg.Settings.ExemptPaths[0] != "/api/public/*" {
		t.Fatalf("legacy settings: %+v", cfg.Settings)
	}
	revs, err := p.Revisions(ctx, 10)
	if err != nil || len(revs) != 1 || revs[0].Action != trafficdomain.ActionImport {
		t.Fatalf("revisions after import: %+v %v", revs, err)
	}

	// 2. 规则增改删、乐观锁
	created, err := p.CreateRule(ctx, nil, traffic.Rule{
		Name:    "单 IP 限速",
		Enabled: true,
		Match:   traffic.MatchSpec{Paths: []string{"/api/x"}},
		Limits:  []traffic.LimitSpec{{Key: traffic.KeyIP, Rate: 2, PeriodMs: 60_000, Burst: 2}},
	}, "top", cfg.Revision, "")
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 2 || len(created.Rules) != 2 || created.Rules[0].Name != "单 IP 限速" {
		t.Fatalf("create: rev=%d rules=%d", created.Revision, len(created.Rules))
	}
	ruleID := created.Rules[0].ID
	if _, err := p.ToggleRule(ctx, nil, ruleID, true, 1); !isCode(err, 40990) {
		t.Fatalf("stale expectedRevision must conflict, got %v", err)
	}
	if _, err := p.CreateRule(ctx, nil, traffic.Rule{Name: ""}, "", 0, ""); !isCode(err, 40091) {
		t.Fatalf("invalid rule must fail validation, got %v", err)
	}
	legacyID := created.Rules[1].ID
	toggled, err := p.ToggleRule(ctx, nil, legacyID, false, 0)
	if err != nil || toggled.Rules[1].Enabled {
		t.Fatalf("toggle: %v", err)
	}
	reordered, err := p.ReorderRules(ctx, nil, []string{legacyID, ruleID}, toggled.Revision)
	if err != nil || reordered.Rules[0].ID != legacyID {
		t.Fatalf("reorder: %v", err)
	}
	if _, err := p.ReorderRules(ctx, nil, []string{ruleID}, 0); !isCode(err, 40091) {
		t.Fatalf("partial reorder must fail, got %v", err)
	}

	// 3. 请求链路：中间件按规则限速，插件停用后直通
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(p.Handler())
	r.GET("/api/x", func(c *gin.Context) { c.String(200, "ok") })
	hit := func() int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.RemoteAddr = "198.51.100.7:5555"
		r.ServeHTTP(w, req)
		return w.Code
	}
	if a, b, c := hit(), hit(), hit(); a != 200 || b != 200 || c != 429 {
		t.Fatalf("limit through middleware: %d %d %d", a, b, c)
	}
	if _, err := host.Disable(ctx, ID, nil); err != nil {
		t.Fatal(err)
	}
	if hit() != 200 {
		t.Fatal("disabled plugin must pass through")
	}
	view, _ := host.Get(ID)
	if view.State != plugindomain.NativeStateStopped || view.Enabled {
		t.Fatalf("host view after disable: %+v", view)
	}
	if _, err := host.Enable(ctx, ID, nil); err != nil || !p.Running() {
		t.Fatalf("enable: %v", err)
	}
	if hit() != 429 {
		t.Fatal("bucket state must survive a plugin restart within the process")
	}

	// 4. 模拟与统计
	sim := p.Simulate(traffic.SimulateInput{Method: "GET", Path: "/api/x", IP: "198.51.100.7"}, nil)
	if sim.Verdict != "reject" {
		t.Fatalf("simulate: %+v", sim)
	}
	if _, err := p.RuleStats(ruleID, 60); err != nil {
		t.Fatal(err)
	}

	// 5. 分钟样本与历史曲线（两个实例同一分钟累加）
	minute := time.Now().UTC().Truncate(time.Minute).Add(-2 * time.Minute)
	sample := traffic.MinuteSample{Minute: minute, Counts: traffic.Counts{Arrivals: 100, Admitted: 90, RejectedRate: 10}, P50Ms: 3, P99Ms: 40, PeakRPS: 5, MaxInflight: 7}
	if err := store.TrafficInsertMinutes(ctx, []trafficdomain.MinuteBatch{
		{Instance: "a", Samples: []traffic.MinuteSample{sample}},
		{Instance: "b", Samples: []traffic.MinuteSample{sample}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.TrafficInsertMinutes(ctx, []trafficdomain.MinuteBatch{{Instance: "a", Samples: []traffic.MinuteSample{sample}}}); err != nil {
		t.Fatal(err)
	}
	hist, err := p.History(ctx, "1h", "")
	if err != nil {
		t.Fatal(err)
	}
	if hist.Totals.Arrivals != 300 || hist.Totals.RejectedRate != 30 || len(hist.Points) != 1 || hist.Points[0].PeakRPS != 10 {
		t.Fatalf("history: %+v", hist)
	}
	for _, rng := range []string{"6h", "24h", "7d"} {
		if _, err := p.History(ctx, rng, ruleID); err != nil {
			t.Fatalf("history %s: %v", rng, err)
		}
	}

	// 6. 事件
	if err := store.TrafficInsertEvents(ctx, "a", []traffic.Event{
		{At: time.Now().UTC(), Kind: traffic.EventWarmupStart, RuleID: ruleID, RuleName: "x", Detail: map[string]any{"ceiling": 250}},
		{At: time.Now().UTC(), Kind: traffic.EventOverloadStart},
	}); err != nil {
		t.Fatal(err)
	}
	evs, err := p.Events(ctx, 10, "warmup.")
	if err != nil || len(evs) != 1 || evs[0].Detail["ceiling"] == nil {
		t.Fatalf("events: %+v %v", evs, err)
	}

	// 7. 回滚到导入时的修订
	rolled, err := p.Rollback(ctx, nil, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rolled.Rules) != 1 || rolled.Rules[0].ID != legacyID || !rolled.Rules[0].Enabled {
		t.Fatalf("rollback: %+v", rolled.Rules)
	}
	if _, err := p.Rollback(ctx, nil, 9999, ""); !isCode(err, 40491) {
		t.Fatalf("missing revision: %v", err)
	}
	detail, err := p.Revision(ctx, 2)
	if err != nil || len(detail.Config.Rules) != 2 {
		t.Fatalf("revision detail: %v", err)
	}

	// 8. 清理与集群协同
	if err := store.TrafficPurge(ctx, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if p.co != nil {
		got, err := p.co.Acquire(ctx, "itest", time.Now().Unix(), time.Second, 10, 6)
		if err != nil || got != 6 {
			t.Fatalf("acquire 1: %d %v", got, err)
		}
		got, err = p.co.Acquire(ctx, "itest", time.Now().Unix(), time.Second, 10, 6)
		if err != nil || got > 4 {
			t.Fatalf("acquire 2 must be capped by the window total: %d %v", got, err)
		}
		if err := p.co.Heartbeat(ctx, p.engine.Summary()); err != nil {
			t.Fatal(err)
		}
		nodes, err := p.co.Nodes(ctx)
		if err != nil || len(nodes) == 0 {
			t.Fatalf("nodes: %+v %v", nodes, err)
		}
		ov := p.Overview(ctx, 60, &view)
		if ov.Cluster.Coordinator != "redis" || ov.Cluster.Instances < 1 {
			t.Fatalf("overview cluster: %+v", ov.Cluster)
		}
	}
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}

func isCode(err error, code int) bool {
	var ae *apperrors.AppError
	return errors.As(err, &ae) && ae.Code == code
}
