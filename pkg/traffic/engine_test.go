package traffic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestEngine(t testing.TB, rules ...Rule) *Engine {
	t.Helper()
	e := NewEngine(Options{Instance: "test"})
	t.Cleanup(e.Close)
	s := DefaultSettings()
	s.Overload.Enabled = false
	for i := range rules {
		if rules[i].ID == "" {
			rules[i].ID = fmt.Sprintf("r%d", i)
		}
		rules[i].Enabled = true
	}
	e.Apply(Config{Revision: 1, Settings: s, Rules: rules})
	return e
}

func req(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

var ipA = netip.MustParseAddr("198.51.100.1")
var ipB = netip.MustParseAddr("198.51.100.2")

func admit(e *Engine, r *http.Request, ip netip.Addr) Verdict {
	t, v := e.Admit(context.Background(), r, ip)
	t.Done()
	return v
}

func TestPathPatterns(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/api/auth/login", "/api/auth/login", true},
		{"/api/auth/login", "/api/auth/login/x", false},
		{"/api/auth/*", "/api/auth", true},
		{"/api/auth/*", "/api/auth/login", true},
		{"/api/auth/*", "/api/authx", false},
		{"/api/v1/apps/*/auth/login", "/api/v1/apps/k1/auth/login", true},
		{"/api/v1/apps/*/auth/login", "/api/v1/apps//auth/login", false},
		{"/api/v1/apps/*/auth/login", "/api/v1/apps/k1/auth/login/x", false},
		{"/api/v1/apps/*/*", "/api/v1/apps/k1/me", true},
		{"/api/v1/apps/*/*", "/api/v1/apps", false},
		{"/api/v1/apps/*/*", "/api/v1/apps/k1", true}, // 末尾 /* 含前缀本身
		{"/api/v1/apps/*/cloud/*", "/api/v1/apps/k1/cloud/a/b", true},
		{"/*", "/anything/at/all", true},
	}
	for _, c := range cases {
		pp := compilePath(c.pattern)
		if got := pp.match(c.path); got != c.want {
			t.Errorf("%s ~ %s = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestGCRABurstThenReject(t *testing.T) {
	p := newGCRAParams(10, time.Second, 5)
	var tat atomic.Int64
	now := mono()
	for i := 0; i < 5; i++ {
		if !p.allow(&tat, now) {
			t.Fatalf("burst request %d rejected", i)
		}
	}
	if p.allow(&tat, now) {
		t.Fatal("6th request in the same instant should be rejected")
	}
	// 100ms 后补回一个
	if !p.allow(&tat, now+int64(100*time.Millisecond)) {
		t.Fatal("token should refill after one emission interval")
	}
}

func TestPerIPLimitIsolatesKeys(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "ip",
		Match:  MatchSpec{Paths: []string{"/api/*"}},
		Limits: []LimitSpec{{Key: KeyIP, Rate: 2, PeriodMs: 60_000, Burst: 2}},
	})
	for i := 0; i < 2; i++ {
		if v := admit(e, req("GET", "/api/x"), ipA); !v.Allowed {
			t.Fatalf("A request %d rejected", i)
		}
	}
	v := admit(e, req("GET", "/api/x"), ipA)
	if v.Allowed || v.Reason != ReasonRate || v.Response.Status != 429 {
		t.Fatalf("A third request: %+v", v)
	}
	if v := admit(e, req("GET", "/api/x"), ipB); !v.Allowed {
		t.Fatal("B must have its own bucket")
	}
	if v := admit(e, req("GET", "/other"), ipA); !v.Allowed {
		t.Fatal("unmatched path must pass")
	}
}

func TestExemptions(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "all",
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1, PeriodMs: 60_000, Burst: 1}},
	})
	_ = admit(e, req("GET", "/api/x"), ipA)
	for _, p := range []string{"/healthz", "/readyz", "/api/admin/system/traffic/overview"} {
		if v := admit(e, req("GET", p), ipA); !v.Allowed || !v.Exempt {
			t.Errorf("%s should be exempt: %+v", p, v)
		}
	}
	if v := admit(e, req("OPTIONS", "/api/x"), ipA); !v.Allowed {
		t.Error("preflight must pass")
	}
}

func TestObserveModeNeverRejects(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "obs",
		Mode:   ModeObserve,
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1, PeriodMs: 60_000, Burst: 1}},
	})
	for i := 0; i < 5; i++ {
		if v := admit(e, req("GET", "/a"), ipA); !v.Allowed {
			t.Fatal("observe mode rejected")
		}
	}
	totals, _ := e.global.read()
	if totals.Observed != 4 {
		t.Fatalf("observed = %d, want 4", totals.Observed)
	}
}

func TestBypassStopsEvaluation(t *testing.T) {
	e := newTestEngine(t,
		Rule{Name: "pay", Mode: ModeBypass, Class: ClassCritical, Match: MatchSpec{Paths: []string{"/api/public/pay/*"}}},
		Rule{Name: "all", Match: MatchSpec{Paths: []string{"/*"}}, Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1, PeriodMs: 60_000, Burst: 1}}},
	)
	for i := 0; i < 3; i++ {
		if v := admit(e, req("POST", "/api/public/pay/callback/alipay"), ipA); !v.Allowed {
			t.Fatal("bypass rule rejected")
		}
	}
	_ = admit(e, req("GET", "/x"), ipA)
	if v := admit(e, req("GET", "/x"), ipA); v.Allowed {
		t.Fatal("second rule should limit other paths")
	}
}

func TestQueueAdmitsAfterWait(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "q",
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 20, PeriodMs: 1000, Burst: 1}},
		Queue:  &QueueSpec{Size: 10, TimeoutMs: 500},
	})
	_ = admit(e, req("GET", "/a"), ipA)
	start := time.Now()
	v := admit(e, req("GET", "/a"), ipA)
	if !v.Allowed {
		t.Fatalf("queued request rejected: %+v", v)
	}
	if waited := time.Since(start); waited < 20*time.Millisecond {
		t.Fatalf("should have waited ~50ms, waited %v", waited)
	}
	totals, _ := e.global.read()
	if totals.Queued != 1 {
		t.Fatalf("queued = %d", totals.Queued)
	}
}

func TestQueueTimeoutRejects(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "q",
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1, PeriodMs: 10_000, Burst: 1}},
		Queue:  &QueueSpec{Size: 10, TimeoutMs: 50},
	})
	_ = admit(e, req("GET", "/a"), ipA)
	start := time.Now()
	v := admit(e, req("GET", "/a"), ipA)
	if v.Allowed || v.Reason != ReasonTimeout {
		t.Fatalf("want timeout, got %+v", v)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Fatal("a wait longer than the timeout must be rejected immediately, not slept on")
	}
}

func TestConcurrencyGateAndQueue(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:        "c",
		Match:       MatchSpec{Paths: []string{"/*"}},
		Concurrency: &ConcurrencySpec{Max: 2},
	})
	t1, v1 := e.Admit(context.Background(), req("GET", "/a"), ipA)
	t2, v2 := e.Admit(context.Background(), req("GET", "/a"), ipA)
	if !v1.Allowed || !v2.Allowed {
		t.Fatal("first two must pass")
	}
	if v := admit(e, req("GET", "/a"), ipA); v.Allowed || v.Reason != ReasonConcurrency {
		t.Fatalf("third must be rejected by concurrency: %+v", v)
	}
	t1.Done()
	if v := admit(e, req("GET", "/a"), ipA); !v.Allowed {
		t.Fatal("slot freed, must pass")
	}
	t2.Done()

	// 排队：占满后，第三个请求等到释放
	e2 := newTestEngine(t, Rule{
		Name:        "cq",
		Match:       MatchSpec{Paths: []string{"/*"}},
		Concurrency: &ConcurrencySpec{Max: 1},
		Queue:       &QueueSpec{Size: 5, TimeoutMs: 1000},
	})
	hold, _ := e2.Admit(context.Background(), req("GET", "/a"), ipA)
	var wg sync.WaitGroup
	var got Verdict
	wg.Add(1)
	go func() {
		defer wg.Done()
		tk, v := e2.Admit(context.Background(), req("GET", "/a"), ipA)
		got = v
		tk.Done()
	}()
	time.Sleep(30 * time.Millisecond)
	hold.Done()
	wg.Wait()
	if !got.Allowed {
		t.Fatalf("queued request should get the released slot: %+v", got)
	}
}

func TestConcurrencyNoLostWakeups(t *testing.T) {
	cl := newConcLimiter(ConcurrencySpec{Max: 4, LatencyToleranceX10: 20})
	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cl.tryAcquire() || cl.acquireWait(context.Background(), 1000, 2*time.Second) == waitOK {
				ok.Add(1)
				time.Sleep(time.Millisecond)
				cl.release(time.Millisecond)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 200 {
		t.Fatalf("only %d of 200 acquired", ok.Load())
	}
	if cl.inflight.Load() != 0 || cl.waiting.Load() != 0 {
		t.Fatalf("leak: inflight=%d waiting=%d", cl.inflight.Load(), cl.waiting.Load())
	}
}

func TestWarmupStateMachine(t *testing.T) {
	w := newWarmup(WarmupSpec{BaselineRPS: 100, MaxRPS: 200, StepPct: 50, IntervalMs: 100, CooldownSeconds: 1})
	now := time.Now()
	step := func(demand float64) string {
		now = now.Add(200 * time.Millisecond)
		return w.evaluate(now, demand)
	}
	if ev := step(95); ev != EventWarmupStart || w.view().State != WarmupRamping || w.currentCeiling() != 150 {
		t.Fatalf("ramp start: ev=%q %+v", ev, w.view())
	}
	if ev := step(150); ev != EventWarmupSaturated || w.view().State != WarmupSaturated {
		t.Fatalf("saturate: ev=%q %+v", ev, w.view())
	}
	_ = step(10) // 开始计冷静期
	now = now.Add(2 * time.Second)
	_ = step(10)
	if w.view().State != WarmupCooldown || w.currentCeiling() != 150 {
		t.Fatalf("cooldown: %+v", w.view())
	}
	if ev := step(10); ev != EventWarmupSettled || w.view().State != WarmupStable || w.currentCeiling() != 100 {
		t.Fatalf("settle: ev=%q %+v", ev, w.view())
	}
}

func TestReloadKeepsBucketState(t *testing.T) {
	rule := Rule{
		ID:     "keep",
		Name:   "keep",
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyIP, Rate: 1, PeriodMs: 60_000, Burst: 1}},
	}
	e := newTestEngine(t, rule)
	_ = admit(e, req("GET", "/a"), ipA)
	rule.Name = "renamed"
	rule.Enabled = true
	e.Apply(Config{Revision: 2, Settings: e.Config().Settings, Rules: []Rule{rule}})
	if v := admit(e, req("GET", "/a"), ipA); v.Allowed {
		t.Fatal("bucket state must survive a reload that does not change the limit")
	}
	stats, ok := e.RuleStats("keep", 60)
	if !ok || stats.Totals.Arrivals != 2 {
		t.Fatalf("rule counters must survive reload: %+v", stats.Totals)
	}
}

func TestOverloadHardInflightCap(t *testing.T) {
	e := newTestEngine(t)
	s := e.Config().Settings
	s.Overload = OverloadSpec{Enabled: true, CPUThreshold: 99, MaxInflight: 1, CooldownMs: 100}
	e.Apply(Config{Revision: 2, Settings: s})
	hold, v := e.Admit(context.Background(), req("GET", "/a"), ipA)
	if !v.Allowed {
		t.Fatal("first must pass")
	}
	if v := admit(e, req("GET", "/a"), ipA); v.Allowed || v.Reason != ReasonOverload {
		t.Fatalf("second must be dropped by the inflight cap: %+v", v)
	}
	hold.Done()
}

func TestHeaderAndAppMatch(t *testing.T) {
	e := newTestEngine(t, Rule{
		Name:   "app",
		Match:  MatchSpec{AppKeys: []string{"k1"}, Headers: []HeaderMatch{{Name: "x-client", Op: "equals", Value: "android"}}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1, PeriodMs: 60_000, Burst: 1}},
	})
	r := req("GET", "/api/x?appKey=k1")
	r.Header.Set("X-Client", "android")
	_ = admit(e, r, ipA)
	if v := admit(e, r, ipA); v.Allowed {
		t.Fatal("matching request must be limited")
	}
	r2 := req("GET", "/api/x?appKey=k2")
	r2.Header.Set("X-Client", "android")
	if v := admit(e, r2, ipA); !v.Allowed {
		t.Fatal("other app must not match")
	}
}

func TestSimulateDoesNotConsume(t *testing.T) {
	e := newTestEngine(t, Rule{
		ID:     "lim",
		Name:   "lim",
		Match:  MatchSpec{Paths: []string{"/api/*"}},
		Limits: []LimitSpec{{Key: KeyIP, Rate: 1, PeriodMs: 60_000, Burst: 1}},
	})
	in := SimulateInput{Method: "GET", Path: "/api/x", IP: ipA.String()}
	for i := 0; i < 3; i++ {
		if res := e.Simulate(in, nil); res.Verdict != "allow" {
			t.Fatalf("simulate %d: %+v", i, res)
		}
	}
	_ = admit(e, req("GET", "/api/x"), ipA)
	res := e.Simulate(in, nil)
	if res.Verdict != "reject" || len(res.Steps) != 1 || res.Steps[0].Outcome != "reject" {
		t.Fatalf("after real request the bucket is empty: %+v", res)
	}
	draft := Rule{ID: "lim", Name: "draft", Match: MatchSpec{Paths: []string{"/api/*"}}, Limits: []LimitSpec{{Key: KeyIP, Rate: 100, Burst: 100}}}
	if res := e.Simulate(in, &draft); res.Verdict != "allow" {
		t.Fatalf("draft has a fresh bucket: %+v", res)
	}
	miss := e.Simulate(SimulateInput{Method: "GET", Path: "/other", IP: ipA.String()}, nil)
	if miss.Steps[0].Matched || miss.Steps[0].Miss == "" {
		t.Fatalf("expected a miss reason: %+v", miss.Steps[0])
	}
}

func TestSeriesAndSnapshot(t *testing.T) {
	e := newTestEngine(t, Rule{
		ID:     "s",
		Name:   "s",
		Match:  MatchSpec{Paths: []string{"/*"}},
		Limits: []LimitSpec{{Key: KeyGlobal, Rate: 1000, Burst: 1000}},
	})
	for i := 0; i < 10; i++ {
		_ = admit(e, req("GET", "/a"), ipA)
	}
	snap := e.Snapshot(60)
	if len(snap.Series) != 60 || snap.Totals.Arrivals != 10 || len(snap.Rules) != 1 {
		t.Fatalf("snapshot shape: series=%d arrivals=%d rules=%d", len(snap.Series), snap.Totals.Arrivals, len(snap.Rules))
	}
	e.ResetStats()
	if s := e.Snapshot(60); s.Totals.Arrivals != 0 {
		t.Fatal("reset must zero totals")
	}
}

func TestHistogramQuantiles(t *testing.T) {
	var st stripedStats
	for i := 0; i < 1000; i++ {
		st.observe(int64(time.Millisecond))
	}
	st.observe(int64(500 * time.Millisecond))
	_, h := st.read()
	v := h.view()
	if v.P50 < 0.5 || v.P50 > 2 {
		t.Fatalf("p50 = %v", v.P50)
	}
	if v.Max < 499 {
		t.Fatalf("max = %v", v.Max)
	}
}

func TestKeyedSweepAndCapacity(t *testing.T) {
	k := newKeyedLimiter(1000, time.Second, 10, keyShards*16)
	now := mono()
	for i := uint64(0); i < 20000; i++ {
		k.allow(i, now)
	}
	if k.tracked.Load() > keyShards*18 { // 容量 + 1/8 余量
		t.Fatalf("tracked %d exceeds capacity", k.tracked.Load())
	}
	if k.overflow.Load() == 0 {
		t.Fatal("expected overflow once capacity is reached")
	}
	k.sweep(now + int64(time.Minute))
	if k.tracked.Load() != 0 {
		t.Fatalf("sweep left %d keys", k.tracked.Load())
	}
}

// ── 基准 ──

func benchEngine(b testing.TB) *Engine {
	return newTestEngine(b,
		Rule{Name: "pay", Mode: ModeBypass, Match: MatchSpec{Paths: []string{"/api/public/pay/*"}}},
		Rule{Name: "login", Match: MatchSpec{Methods: []string{"POST"}, Paths: []string{"/api/auth/login/password"}},
			Limits: []LimitSpec{{Key: KeyIP, Rate: 10, PeriodMs: 60_000}}},
		Rule{Name: "ip", Match: MatchSpec{Paths: []string{"/api/*"}},
			Limits: []LimitSpec{{Key: KeyIP, Rate: 1_000_000, Burst: 1_000_000}}},
		Rule{Name: "app", Match: MatchSpec{Paths: []string{"/api/v1/apps/*/*"}},
			Limits: []LimitSpec{{Key: KeyApp, Rate: 10_000_000, Burst: 10_000_000}}},
	)
}

// BenchmarkAdmitParallel 热路径：四条规则、按 IP 限速，2 百万个不同来源 IP。
func BenchmarkAdmitParallel(b *testing.B) {
	e := benchEngine(b)
	const ips = 2_000_000
	addrs := make([]netip.Addr, 4096)
	for i := range addrs {
		addrs[i] = netip.AddrFrom4([4]byte{10, byte(i >> 8), byte(i), 1})
	}
	reqs := make([]*http.Request, 8)
	for i := range reqs {
		reqs[i] = req("GET", "/api/v1/apps/app"+fmt.Sprint(i)+"/me")
	}
	var seq atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			n := seq.Add(1)
			ip := netip.AddrFrom4([4]byte{byte(n % ips >> 16), byte(n % ips >> 8), byte(n % ips), 7})
			_ = addrs
			t, v := e.Admit(ctx, reqs[n&7], ip)
			if v.Allowed {
				t.Done()
			}
		}
	})
}

func BenchmarkGCRAAllow(b *testing.B) {
	l := newGCRALimiter(1e9, time.Second, 1e9)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.allow(mono())
		}
	})
}

func TestAdmitZeroAlloc(t *testing.T) {
	e := benchEngine(t)
	r := req("GET", "/api/v1/apps/k/me")
	ctx := context.Background()
	// 预热：建好键
	for i := 0; i < 10; i++ {
		tk, _ := e.Admit(ctx, r, ipA)
		tk.Done()
	}
	allocs := testing.AllocsPerRun(1000, func() {
		tk, _ := e.Admit(ctx, r, ipA)
		tk.Done()
	})
	if allocs > 0 {
		t.Fatalf("Admit allocates %.1f times per request", allocs)
	}
}
