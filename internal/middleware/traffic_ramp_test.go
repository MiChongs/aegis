package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	systemdomain "aegis/internal/domain/system"

	"github.com/gin-gonic/gin"
)

func newRampEngine(t *testing.T, cfg config.TrafficRampConfig) (*TrafficRamp, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ramp := NewTrafficRamp(cfg, nil)
	t.Cleanup(ramp.Close)
	router := gin.New()
	router.Use(ramp.Handler())
	router.GET("/api/things", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/api/admin/system/traffic-ramp/stats", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.GET("/api/public/docs", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return ramp, router
}

func doRampRequest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(rec, req)
	return rec
}

func TestNormalizeTrafficRampConfigDefaults(t *testing.T) {
	cfg := config.NormalizeTrafficRampConfig(config.TrafficRampConfig{})
	if cfg.Enabled {
		t.Fatal("默认必须关闭：升级不应引入静默的行为变更")
	}
	if cfg.BaselineRPS != 300 {
		t.Fatalf("unexpected baseline: %d", cfg.BaselineRPS)
	}
	if cfg.MaxRPS != 3000 {
		t.Fatalf("unexpected max rps: %d", cfg.MaxRPS)
	}
	if cfg.RampStepPct != 20 || cfg.RampIntervalMs != 1000 || cfg.CooldownSeconds != 30 {
		t.Fatalf("unexpected ramp params: %+v", cfg)
	}
	if cfg.QueueTimeoutMs != 2000 || cfg.RetryAfterSeconds != 3 {
		t.Fatalf("unexpected queue params: %+v", cfg)
	}

	clamped := config.NormalizeTrafficRampConfig(config.TrafficRampConfig{BaselineRPS: 500, MaxRPS: 100})
	if clamped.MaxRPS != 500 {
		t.Fatalf("max 必须被夹到不低于 baseline，得到 %d", clamped.MaxRPS)
	}
	prefixed := config.NormalizeTrafficRampConfig(config.TrafficRampConfig{ExemptPathPrefixes: []string{" api/Public ", ""}})
	if len(prefixed.ExemptPathPrefixes) != 1 || prefixed.ExemptPathPrefixes[0] != "/api/public" {
		t.Fatalf("前缀应补斜杠并小写化，得到 %v", prefixed.ExemptPathPrefixes)
	}
}

func TestTrafficRampDisabledPassesThrough(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{Enabled: false, BaselineRPS: 1})
	for i := 0; i < 10; i++ {
		if rec := doRampRequest(router, "/api/things"); rec.Code != http.StatusOK {
			t.Fatalf("关闭时第 %d 个请求被拦截：%d", i, rec.Code)
		}
	}
	if got := ramp.StatsView(60).TotalArrivals; got != 0 {
		t.Fatalf("关闭时不应计数，得到 %d", got)
	}
}

func TestTrafficRampShedsWithoutQueue(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 1, MaxRPS: 1, QueueSize: 0, RetryAfterSeconds: 7, ExemptAdmin: true,
	})
	first := doRampRequest(router, "/api/things")
	if first.Code != http.StatusOK {
		t.Fatalf("第一个请求应放行，得到 %d", first.Code)
	}
	second := doRampRequest(router, "/api/things")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("超额请求应回 429，得到 %d", second.Code)
	}
	if second.Header().Get("Retry-After") != "7" {
		t.Fatalf("缺少 Retry-After 头，得到 %q", second.Header().Get("Retry-After"))
	}
	if !strings.Contains(second.Body.String(), "42910") {
		t.Fatalf("响应体缺少业务码 42910：%s", second.Body.String())
	}
	stats := ramp.StatsView(60)
	if stats.TotalAdmitted != 1 || stats.TotalRejectedRate != 1 {
		t.Fatalf("计数不符：admitted=%d rejectedRate=%d", stats.TotalAdmitted, stats.TotalRejectedRate)
	}
}

func TestTrafficRampExemptions(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 1, MaxRPS: 1, QueueSize: 0,
		ExemptAdmin: true, ExemptPathPrefixes: []string{"/api/public"},
	})
	// 耗尽唯一的令牌。
	if rec := doRampRequest(router, "/api/things"); rec.Code != http.StatusOK {
		t.Fatalf("第一个请求应放行，得到 %d", rec.Code)
	}
	// 探活、管理端、自定义前缀全部不受整形影响。
	for _, path := range []string{"/healthz", "/api/admin/system/traffic-ramp/stats", "/api/public/docs"} {
		if rec := doRampRequest(router, path); rec.Code != http.StatusOK {
			t.Fatalf("豁免路径 %s 被拦截：%d", path, rec.Code)
		}
	}
	stats := ramp.StatsView(60)
	// /healthz 恒免不计豁免数；管理端与自定义前缀各计一次。
	if stats.TotalExempt != 2 {
		t.Fatalf("豁免计数应为 2，得到 %d", stats.TotalExempt)
	}
}

func TestTrafficRampQueueAdmitsAfterWait(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 1, MaxRPS: 1, QueueSize: 5, QueueTimeoutMs: 1500, ExemptAdmin: true,
	})
	if rec := doRampRequest(router, "/api/things"); rec.Code != http.StatusOK {
		t.Fatalf("第一个请求应放行，得到 %d", rec.Code)
	}
	start := time.Now()
	second := doRampRequest(router, "/api/things")
	if second.Code != http.StatusOK {
		t.Fatalf("排队请求应在令牌补充后放行，得到 %d", second.Code)
	}
	if waited := time.Since(start); waited < 500*time.Millisecond {
		t.Fatalf("排队请求应等待令牌补充（约 1s），实际只等了 %v", waited)
	}
	if got := ramp.StatsView(60).TotalQueuedAdmitted; got != 1 {
		t.Fatalf("排队放行计数应为 1，得到 %d", got)
	}
}

func TestTrafficRampQueueTimeoutRejectsFast(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 1, MaxRPS: 1, QueueSize: 5, QueueTimeoutMs: 200, ExemptAdmin: true,
	})
	if rec := doRampRequest(router, "/api/things"); rec.Code != http.StatusOK {
		t.Fatalf("第一个请求应放行，得到 %d", rec.Code)
	}
	start := time.Now()
	second := doRampRequest(router, "/api/things")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("等不到令牌应回 429，得到 %d", second.Code)
	}
	// rate.Wait 判断出 200ms 内等不到 1s 后的令牌，应当立即失败而不是傻等。
	if waited := time.Since(start); waited > 700*time.Millisecond {
		t.Fatalf("等不到令牌应快速失败，实际等了 %v", waited)
	}
	if got := ramp.StatsView(60).TotalRejectedTimeout; got != 1 {
		t.Fatalf("排队超时计数应为 1，得到 %d", got)
	}
}

func TestTrafficRampConcurrencyGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ramp := NewTrafficRamp(config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 1000, MaxRPS: 1000, MaxConcurrent: 1, ExemptAdmin: true,
	}, nil)
	t.Cleanup(ramp.Close)

	release := make(chan struct{})
	entered := make(chan struct{})
	router := gin.New()
	router.Use(ramp.Handler())
	router.GET("/api/slow", func(c *gin.Context) {
		close(entered)
		<-release
		c.String(http.StatusOK, "ok")
	})

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/slow", nil))
		done <- rec.Code
	}()
	<-entered

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/slow", nil))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("超过并发上限应回 429，得到 %d", rec.Code)
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("首个慢请求应正常完成，得到 %d", code)
	}
	if got := ramp.StatsView(60).TotalRejectedLoad; got != 1 {
		t.Fatalf("并发拒绝计数应为 1，得到 %d", got)
	}
}

// 爬坡状态机白盒：直接喂 demand 调 evaluate，不依赖真实时钟节拍。
func TestTrafficRampStateMachine(t *testing.T) {
	ramp := NewTrafficRamp(config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 100, MaxRPS: 200, RampStepPct: 50, CooldownSeconds: 1,
	}, nil)
	ramp.Close() // 停掉后台循环，避免它用真实 demand=0 干扰白盒评估

	seedDemand := func(at time.Time, n int) {
		for i := 0; i < n; i++ {
			ramp.note(at, rampNoteArrival)
		}
	}

	// 突发：上一秒 95 次到达（≥ 100×0.9），应进入 ramping 并抬一步（+50%）。
	t0 := time.Now()
	seedDemand(t0.Add(-time.Second), 95)
	ramp.evaluate(t0)
	stats := ramp.StatsView(60)
	if stats.State != systemdomain.TrafficRampStateRamping {
		t.Fatalf("应进入 ramping，得到 %s", stats.State)
	}
	if stats.CurrentLimit != 150 {
		t.Fatalf("上限应抬到 150，得到 %v", stats.CurrentLimit)
	}
	if stats.RampEvents != 1 || stats.LastBurstAt == nil {
		t.Fatalf("爬坡事件应记 1 次并带时间，得到 %d / %v", stats.RampEvents, stats.LastBurstAt)
	}

	// 需求继续顶着新上限：再抬一步到顶（200），进入 saturated。
	t1 := t0.Add(2 * time.Second)
	seedDemand(t1.Add(-time.Second), 140) // ≥ 150×0.9=135
	ramp.evaluate(t1)
	stats = ramp.StatsView(60)
	if stats.State != systemdomain.TrafficRampStateSaturated {
		t.Fatalf("到顶仍供不应求应为 saturated，得到 %s", stats.State)
	}
	if stats.CurrentLimit != 200 {
		t.Fatalf("上限应到顶 200，得到 %v", stats.CurrentLimit)
	}
	if stats.RampEvents != 1 {
		t.Fatalf("同一场突发不应重复计数，得到 %d", stats.RampEvents)
	}

	// 峰值退去：demand=0 低于基线，先熬冷静期（1s），再逐步回落到基线。
	t2 := t1.Add(2 * time.Second)
	ramp.evaluate(t2) // 记录 belowSince，还不回落
	if got := ramp.StatsView(60).CurrentLimit; got != 200 {
		t.Fatalf("冷静期内不应回落，得到 %v", got)
	}
	t3 := t2.Add(1100 * time.Millisecond)
	ramp.evaluate(t3) // 冷静期已过，回落一步：200 → 150
	stats = ramp.StatsView(60)
	if stats.State != systemdomain.TrafficRampStateCooldown {
		t.Fatalf("回落中应为 cooldown，得到 %s", stats.State)
	}
	if stats.CurrentLimit != 150 {
		t.Fatalf("应回落到 150，得到 %v", stats.CurrentLimit)
	}
	t4 := t3.Add(time.Second)
	ramp.evaluate(t4) // 再回落一步：150 → 100，落回基线转 stable
	stats = ramp.StatsView(60)
	if stats.State != systemdomain.TrafficRampStateStable {
		t.Fatalf("落回基线应为 stable，得到 %s", stats.State)
	}
	if stats.CurrentLimit != 100 {
		t.Fatalf("上限应回到基线 100，得到 %v", stats.CurrentLimit)
	}
	if stats.PeakLimit != 200 {
		t.Fatalf("峰值上限应记住 200，得到 %v", stats.PeakLimit)
	}
}

func TestTrafficRampReload(t *testing.T) {
	ramp := NewTrafficRamp(config.TrafficRampConfig{Enabled: true, BaselineRPS: 100, MaxRPS: 200}, nil)
	ramp.Close()

	v1, _ := ramp.ReloadMeta()
	if err := ramp.Reload(config.TrafficRampConfig{Enabled: true, BaselineRPS: 50, MaxRPS: 80}); err != nil {
		t.Fatalf("reload 失败：%v", err)
	}
	v2, _ := ramp.ReloadMeta()
	if v2 != v1+1 {
		t.Fatalf("版本应递增：%d → %d", v1, v2)
	}
	cfg := ramp.CurrentConfig()
	if cfg.BaselineRPS != 50 || cfg.MaxRPS != 80 {
		t.Fatalf("配置未生效：%+v", cfg)
	}
	// 原上限 100 超出新区间 [50, 80]，应被夹回 80。
	if got := ramp.StatsView(60).CurrentLimit; got != 80 {
		t.Fatalf("上限应被夹回 80，得到 %v", got)
	}

	// 关闭时状态机复位，再开启从基线干净起步。
	if err := ramp.Reload(config.TrafficRampConfig{Enabled: false, BaselineRPS: 50, MaxRPS: 80}); err != nil {
		t.Fatalf("reload(禁用)失败：%v", err)
	}
	stats := ramp.StatsView(60)
	if stats.State != systemdomain.TrafficRampStateStable || stats.CurrentLimit != 50 {
		t.Fatalf("禁用后应复位到基线，得到 %s / %v", stats.State, stats.CurrentLimit)
	}
}

func TestTrafficRampResetStats(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 100, MaxRPS: 200, ExemptAdmin: true,
	})
	for i := 0; i < 5; i++ {
		doRampRequest(router, "/api/things")
	}
	if got := ramp.StatsView(60).TotalArrivals; got != 5 {
		t.Fatalf("到达计数应为 5，得到 %d", got)
	}
	ramp.ResetStats()
	stats := ramp.StatsView(60)
	if stats.TotalArrivals != 0 || stats.TotalAdmitted != 0 || stats.RampEvents != 0 {
		t.Fatalf("清零后计数应为 0：%+v", stats)
	}
	var seriesSum int64
	for _, p := range stats.Series {
		seriesSum += p.Arrivals
	}
	if seriesSum != 0 {
		t.Fatalf("清零后序列应为空，累计 %d", seriesSum)
	}
}

func TestTrafficRampStatsSeriesShape(t *testing.T) {
	ramp, router := newRampEngine(t, config.TrafficRampConfig{
		Enabled: true, BaselineRPS: 100, MaxRPS: 200, ExemptAdmin: true,
	})
	doRampRequest(router, "/api/things")

	stats := ramp.StatsView(10) // 低于下限，应被夹到 60
	if len(stats.Series) != 60 {
		t.Fatalf("序列长度应被夹到 60，得到 %d", len(stats.Series))
	}
	last := stats.Series[len(stats.Series)-1]
	if last.Limit <= 0 {
		t.Fatalf("最新一格的上限应为当前上限，得到 %v", last.Limit)
	}
	var arrivals int64
	for _, p := range stats.Series {
		arrivals += p.Arrivals
	}
	if arrivals != 1 {
		t.Fatalf("序列到达合计应为 1，得到 %d", arrivals)
	}
	for i := 1; i < len(stats.Series); i++ {
		if stats.Series[i].Ts != stats.Series[i-1].Ts+1 {
			t.Fatalf("序列时间戳应逐秒连续：%d 后跟 %d", stats.Series[i-1].Ts, stats.Series[i].Ts)
		}
	}
}
