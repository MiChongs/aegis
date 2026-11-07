package traffic

import (
	"context"
	"hash/maphash"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go4.org/netipx"
)

// Sink 接收分钟聚合与事件（由插件落库）。实现必须非阻塞或自行缓冲。
type Sink interface {
	Minutes(samples []MinuteSample)
	Event(ev Event)
}

// Options 引擎构造参数。
type Options struct {
	Instance    string
	Coordinator Coordinator
	Sink        Sink
	Logger      *zap.Logger
}

// Engine 流量管理引擎。并发安全；配置经 Apply 原子替换。
type Engine struct {
	log      *zap.Logger
	instance string
	host     string
	started  time.Time
	co       Coordinator
	sink     Sink
	seed     maphash.Seed

	cur atomic.Pointer[compiled]

	// 规则运行态按 ID 保存，跨配置重载保留（计数、桶、预热、并发上限都不归零）。
	statesMu sync.Mutex
	states   map[string]*ruleState

	global     *stripedStats
	globalSer  *series
	inflight   atomic.Int64
	exempt     atomic.Int64
	ovl        *overload
	statsSince atomic.Int64 // unix 毫秒
	instances  atomic.Int64

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// compiled 一份编译后的不可变配置。
type compiled struct {
	cfg      Config
	exempt   pathSet
	rules    []*ruleRT
	response ResponseSpec
}

// ruleRT 编译后的规则（随配置替换），state 指向跨重载保留的运行态。
type ruleRT struct {
	spec      Rule
	methods   uint8
	paths     pathSet
	excludes  pathSet
	hosts     []hostPattern
	appKeys   map[string]struct{}
	cidrs     *netipx.IPSet
	exCidrs   *netipx.IPSet
	headers   []headerCond
	limits    []*limitRT
	queueSize int64
	queueWait int64 // 纳秒
	response  ResponseSpec
	state     *ruleState
	conc      *concLimiter // 与 state 里保存的是同一个对象；放在这里是为了热路径只读不可变结构
	warm      *warmup
}

// ruleState 规则的运行态。
type ruleState struct {
	id         string
	stats      *stripedStats
	ser        *series
	hot        *hotKeys
	inflight   atomic.Int64
	queueDepth atomic.Int64
	conc       *concLimiter
	warm       *warmup
	limits     map[string]*limitState // 按「键种类 + 名字 + 作用域」复用
}

// limitState 一项限速的桶（跨重载保留；字段只在持 statesMu 时读写）。
type limitState struct {
	global   *gcraLimiter
	keyed    *keyedLimiter
	lease    *leaseBucket
	rejected atomic.Int64
}

// limitRT 编译后的一项限速。三个桶指针恰有一个非空，编译后不再改变。
type limitRT struct {
	spec       LimitSpec
	headerName string
	state      *limitState
	global     *gcraLimiter
	keyed      *keyedLimiter
	lease      *leaseBucket
	cluster    bool // 作用域为集群且集群协同已开启
}

// NewEngine 构造引擎（初始为关闭状态，Apply 后生效）并启动采样循环。
func NewEngine(opts Options) *Engine {
	log := opts.Logger
	if log == nil {
		log = zap.NewNop()
	}
	host, _ := os.Hostname()
	e := &Engine{
		log:       log,
		instance:  opts.Instance,
		host:      host,
		started:   time.Now().UTC(),
		co:        opts.Coordinator,
		sink:      opts.Sink,
		seed:      maphash.MakeSeed(),
		states:    make(map[string]*ruleState),
		global:    &stripedStats{},
		globalSer: &series{},
		ovl:       newOverload(),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	if e.instance == "" {
		e.instance = host
	}
	e.instances.Store(1)
	e.statsSince.Store(time.Now().UnixMilli())
	e.cur.Store(&compiled{cfg: Config{Settings: Settings{Enabled: false}}, response: DefaultSettings().Response})
	go e.loop()
	return e
}

// Close 停止采样循环。
func (e *Engine) Close() {
	e.stopOnce.Do(func() { close(e.stopCh) })
	<-e.doneCh
}

// Instance 本实例标识。
func (e *Engine) Instance() string { return e.instance }

// Config 当前生效的配置（副本）。
func (e *Engine) Config() Config {
	c := e.cur.Load().cfg
	c.Rules = append([]Rule(nil), c.Rules...)
	return c
}

// Apply 编译并原子替换配置。
func (e *Engine) Apply(cfg Config) {
	cfg.Settings = NormalizeSettings(cfg.Settings)
	rules := make([]Rule, 0, len(cfg.Rules))
	for _, r := range dedupeRules(cfg.Rules) {
		rules = append(rules, NormalizeRule(r))
	}
	cfg.Rules = rules

	e.statesMu.Lock()
	c := e.compile(cfg, e.liveState)
	// 删除的规则：运行态丢弃。在途请求仍持有旧的 ruleRT/ruleState，释放不受影响。
	keep := make(map[string]struct{}, len(cfg.Rules))
	for _, r := range cfg.Rules {
		keep[r.ID] = struct{}{}
	}
	for id := range e.states {
		if _, ok := keep[id]; !ok {
			delete(e.states, id)
		}
	}
	e.statesMu.Unlock()

	e.ovl.configure(cfg.Settings.Overload)
	e.cur.Store(c)
	e.log.Info("traffic config applied",
		zap.Int64("revision", cfg.Revision),
		zap.Bool("enabled", cfg.Settings.Enabled),
		zap.Int("rules", len(cfg.Rules)))
}

// liveState 取或建规则运行态（调用方持 statesMu）。
func (e *Engine) liveState(id string) *ruleState {
	st := e.states[id]
	if st == nil {
		st = newRuleState(id)
		e.states[id] = st
	}
	return st
}

func newRuleState(id string) *ruleState {
	return &ruleState{
		id:     id,
		stats:  &stripedStats{},
		ser:    &series{},
		hot:    newHotKeys(),
		limits: make(map[string]*limitState),
	}
}

// compile 把配置编译成运行结构。stateFor 决定规则运行态从哪里来
// （正式应用 = 跨重载保留的运行态；模拟 = 临时运行态）。
func (e *Engine) compile(cfg Config, stateFor func(id string) *ruleState) *compiled {
	c := &compiled{
		cfg:      cfg,
		exempt:   compilePaths(cfg.Settings.ExemptPaths),
		response: cfg.Settings.Response,
	}
	clusterOn := cfg.Settings.Cluster.Enabled && e.co != nil
	n := int(e.instances.Load())
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		c.rules = append(c.rules, e.compileRule(r, stateFor(r.ID), cfg.Settings, clusterOn, n))
	}
	return c
}

// compileRule 编译一条规则，桶与控制器复用 st 里已有的对象。
func (e *Engine) compileRule(r Rule, st *ruleState, settings Settings, clusterOn bool, n int) *ruleRT {
	rt := &ruleRT{
		spec:     r,
		paths:    compilePaths(r.Match.Paths),
		excludes: compilePaths(r.Match.ExcludePaths),
		hosts:    compileHosts(r.Match.Hosts),
		cidrs:    compileCIDRs(r.Match.CIDRs),
		exCidrs:  compileCIDRs(r.Match.ExcludeCIDRs),
		headers:  compileHeaders(r.Match.Headers),
		response: settings.Response,
		state:    st,
	}
	if r.Response != nil {
		rt.response = *r.Response
	}
	for _, m := range r.Match.Methods {
		rt.methods |= methodBit(m)
	}
	if len(r.Match.AppKeys) > 0 {
		rt.appKeys = make(map[string]struct{}, len(r.Match.AppKeys))
		for _, k := range r.Match.AppKeys {
			rt.appKeys[k] = struct{}{}
		}
	}
	if r.Queue != nil {
		rt.queueSize = int64(r.Queue.Size)
		rt.queueWait = int64(time.Duration(r.Queue.TimeoutMs) * time.Millisecond)
	}
	used := make(map[string]struct{}, len(r.Limits))
	for i, l := range r.Limits {
		sig := limitSig(i, l)
		used[sig] = struct{}{}
		ls := st.limits[sig]
		if ls == nil {
			ls = &limitState{}
			st.limits[sig] = ls
		}
		lrt := &limitRT{spec: l, state: ls, cluster: clusterOn && l.Scope == ScopeCluster}
		if l.Key == KeyHeader {
			lrt.headerName = canonicalHeader(l.Name)
		}
		e.configureLimit(r.ID, sig, lrt, n, settings.MaxTrackedKeys)
		lrt.global, lrt.keyed, lrt.lease = ls.global, ls.keyed, ls.lease
		rt.limits = append(rt.limits, lrt)
	}
	for sig := range st.limits {
		if _, ok := used[sig]; !ok {
			delete(st.limits, sig)
		}
	}
	if r.Concurrency != nil {
		if st.conc == nil {
			st.conc = newConcLimiter(*r.Concurrency)
		} else {
			st.conc.configure(*r.Concurrency)
		}
	} else {
		st.conc = nil
	}
	if r.Warmup != nil {
		if st.warm == nil {
			st.warm = newWarmup(*r.Warmup)
		} else {
			st.warm.configure(*r.Warmup)
		}
	} else {
		st.warm = nil
	}
	rt.conc, rt.warm = st.conc, st.warm
	return rt
}

func limitSig(i int, l LimitSpec) string {
	var b strings.Builder
	b.WriteString(l.Key)
	b.WriteByte('|')
	b.WriteString(l.Name)
	b.WriteByte('|')
	b.WriteString(l.Scope)
	b.WriteByte('|')
	// 同一规则里键种类相同的多项限速（例如每秒 + 每天）要分开。
	b.WriteString(itoa(i))
	return b.String()
}

// configureLimit 按作用域与实例数设置桶参数。
func (e *Engine) configureLimit(ruleID, sig string, l *limitRT, instances int, capacity int) {
	spec := l.spec
	period := time.Duration(spec.PeriodMs) * time.Millisecond
	rate := float64(spec.Rate)
	burst := spec.Burst
	if l.cluster && instances > 1 && spec.Key != KeyGlobal {
		// 按键的集群限速：每个实例只承担自己那份。负载均衡把同一个键的请求
		// 打散到各实例时，这正好拼回集群总额；同一个键粘在一个实例上时会更严，
		// 这是不在热路径上访问 Redis 的代价。
		rate /= float64(instances)
		burst = maxInt(1, burst/instances)
	}
	ls := l.state
	switch {
	case spec.Key == KeyGlobal && l.cluster:
		if ls.lease == nil {
			ls.lease = newLeaseBucket("traffic:q:"+ruleID+":"+sig, e.co, spec)
		} else {
			ls.lease.configure(spec)
		}
		ls.global, ls.keyed = nil, nil
	case spec.Key == KeyGlobal:
		if ls.global == nil {
			ls.global = newGCRALimiter(rate, period, burst)
		} else {
			ls.global.set(rate, period, burst)
		}
		ls.keyed, ls.lease = nil, nil
	default:
		if ls.keyed == nil {
			ls.keyed = newKeyedLimiter(rate, period, burst, capacity)
		} else {
			ls.keyed.set(rate, period, burst)
			ls.keyed.setCapacity(capacity)
		}
		ls.global, ls.lease = nil, nil
	}
}

func canonicalHeader(name string) string {
	return http.CanonicalHeaderKey(strings.TrimSpace(name))
}

// ── 判定 ──

// 拒绝原因。
const (
	ReasonRate        = "rate"
	ReasonConcurrency = "concurrency"
	ReasonQueueFull   = "queue_full"
	ReasonTimeout     = "timeout"
	ReasonOverload    = "overload"
)

// Verdict 判定结果。
type Verdict struct {
	Allowed  bool
	Exempt   bool
	Reason   string
	RuleID   string
	Response ResponseSpec
}

const maxTracked = 8

// Ticket 放行的请求凭据，请求结束后必须调用 Done。值类型，不分配。
type Ticket struct {
	e       *Engine
	start   int64
	n       uint8
	rules   [maxTracked]*ruleRT
	conc    [maxTracked]bool
	more    []*ruleRT // 命中规则超过 maxTracked 时才分配
	moreCon []bool
	counted bool
}

// Done 释放并发名额、记录响应时间。重复调用无副作用。
func (t *Ticket) Done() {
	if t == nil || t.e == nil {
		return
	}
	e := t.e
	t.e = nil
	rtt := mono() - t.start
	for i := 0; i < int(t.n); i++ {
		releaseRule(t.rules[i], t.conc[i], rtt)
	}
	for i, r := range t.more {
		releaseRule(r, t.moreCon[i], rtt)
	}
	if t.counted {
		e.inflight.Add(-1)
		e.ovl.inflight.Add(-1)
		e.ovl.done(rtt)
		e.global.observe(rtt)
	}
}

func releaseRule(r *ruleRT, conc bool, rtt int64) {
	st := r.state
	st.inflight.Add(-1)
	st.stats.observe(rtt)
	if conc && r.conc != nil {
		r.conc.release(time.Duration(rtt))
	}
}

func (t *Ticket) track(r *ruleRT, conc bool) {
	if int(t.n) < maxTracked {
		t.rules[t.n] = r
		t.conc[t.n] = conc
		t.n++
		return
	}
	t.more = append(t.more, r)
	t.moreCon = append(t.moreCon, conc)
}

// abort 拒绝时归还已经拿到的并发名额（不计入延迟统计）。
func (t *Ticket) abort() {
	if t.e == nil {
		return
	}
	for i := 0; i < int(t.n); i++ {
		r := t.rules[i]
		r.state.inflight.Add(-1)
		if t.conc[i] && r.conc != nil {
			r.conc.release(0)
		}
	}
	for i, r := range t.more {
		r.state.inflight.Add(-1)
		if t.moreCon[i] && r.conc != nil {
			r.conc.release(0)
		}
	}
	t.e = nil
}

// Admit 判定一个请求。ip 是已解析的客户端地址。放行时返回的 Ticket
// 必须在请求结束后 Done；拒绝时 Ticket 为空值（Done 无副作用）。
func (e *Engine) Admit(ctx context.Context, r *http.Request, ip netip.Addr) (Ticket, Verdict) {
	c := e.cur.Load()
	if !c.cfg.Settings.Enabled {
		return Ticket{}, Verdict{Allowed: true, Exempt: true}
	}
	switch r.Method {
	case http.MethodOptions:
		// CORS 预检被拒会把真正的错误藏在一层跨域报错后面。
		return Ticket{}, Verdict{Allowed: true, Exempt: true}
	}
	path := lowerPath(r.URL.Path)
	if path == "/healthz" || path == "/readyz" {
		return Ticket{}, Verdict{Allowed: true, Exempt: true}
	}
	// WebSocket 是长连接：占着并发名额不还、把延迟统计拉到几小时，都不是想要的。
	// 连接建立的频率仍可以用规则限（按路径写一条只有速率限制的规则即可）。
	if isUpgrade(r) {
		e.exempt.Add(1)
		return Ticket{}, Verdict{Allowed: true, Exempt: true}
	}
	if (c.cfg.Settings.ExemptAdmin && strings.HasPrefix(path, "/api/admin/")) || c.exempt.match(path) {
		e.exempt.Add(1)
		return Ticket{}, Verdict{Allowed: true, Exempt: true}
	}

	// 第一遍：匹配（只读），得到命中的规则与请求档位。
	var buf [16]*ruleRT
	matched := buf[:0]
	class := ClassNormal
	classSet := false
	var appKey string
	appKeyRead := false
	mbit := methodBit(r.Method)
	for _, rt := range c.rules {
		if rt.methods != 0 && rt.methods&mbit == 0 {
			continue
		}
		if len(rt.paths) > 0 && !rt.paths.match(path) {
			continue
		}
		if len(rt.excludes) > 0 && rt.excludes.match(path) {
			continue
		}
		if len(rt.hosts) > 0 && !matchHost(rt.hosts, r.Host) {
			continue
		}
		if rt.appKeys != nil {
			if !appKeyRead {
				appKey, appKeyRead = requestAppKey(r), true
			}
			if _, ok := rt.appKeys[appKey]; !ok {
				continue
			}
		}
		if rt.cidrs != nil && !rt.cidrs.Contains(ip.Unmap()) {
			continue
		}
		if rt.exCidrs != nil && rt.exCidrs.Contains(ip.Unmap()) {
			continue
		}
		if !matchHeaders(rt.headers, r.Header) {
			continue
		}
		if !classSet {
			class, classSet = rt.spec.Class, true
		}
		matched = append(matched, rt)
		if rt.spec.Terminal || rt.spec.Mode == ModeBypass {
			break
		}
	}

	now := mono()
	e.global.add(cArrivals)

	// 过载保护先于规则：此时连令牌都不该消耗。
	if e.ovl.shouldDrop(class, now) {
		e.global.add(cRejOverload)
		for _, rt := range matched {
			rt.state.stats.add(cArrivals)
			rt.state.stats.add(cRejOverload)
		}
		resp := c.response
		ruleID := ""
		if len(matched) > 0 {
			resp, ruleID = matched[0].response, matched[0].spec.ID
		}
		return Ticket{}, Verdict{Reason: ReasonOverload, RuleID: ruleID, Response: resp}
	}

	t := Ticket{e: e}
	queued := false
	for _, rt := range matched {
		st := rt.state
		st.stats.add(cArrivals)
		if rt.spec.Mode == ModeBypass {
			st.stats.add(cBypassed)
			st.inflight.Add(1)
			t.track(rt, false)
			break
		}
		observe := rt.spec.Mode == ModeObserve
		reason, waited, gotConc := e.enforce(ctx, rt, r, ip, now, observe)
		if reason != "" {
			if observe {
				st.stats.add(cObserved)
				e.global.add(cObserved)
			} else {
				st.stats.add(reasonCounter(reason))
				e.global.add(reasonCounter(reason))
				t.abort()
				return Ticket{}, Verdict{Reason: reason, RuleID: rt.spec.ID, Response: rt.response}
			}
		}
		if waited {
			st.stats.add(cQueued)
			queued = true
		} else {
			st.stats.add(cAdmitted)
		}
		st.inflight.Add(1)
		t.track(rt, gotConc)
	}
	if queued {
		e.global.add(cQueued)
	} else {
		e.global.add(cAdmitted)
	}
	t.start = mono()
	t.counted = true
	e.inflight.Add(1)
	e.ovl.inflight.Add(1)
	return t, Verdict{Allowed: true}
}

func isUpgrade(r *http.Request) bool {
	v := r.Header["Upgrade"]
	return len(v) > 0 && v[0] != ""
}

func matchHeaders(conds []headerCond, h http.Header) bool {
	for i := range conds {
		if !conds[i].match(h) {
			return false
		}
	}
	return true
}

func reasonCounter(reason string) int {
	switch reason {
	case ReasonRate:
		return cRejRate
	case ReasonConcurrency:
		return cRejConc
	case ReasonQueueFull:
		return cRejQueue
	case ReasonTimeout:
		return cRejTimeout
	}
	return cRejOverload
}

// enforce 执行一条规则的控制手段：预热上限 → 各项限速 → 并发。
// 返回拒绝原因（空 = 放行）、是否排过队、是否占用了并发名额。
// observe 模式下只判定不排队、不占并发名额。
func (e *Engine) enforce(ctx context.Context, rt *ruleRT, r *http.Request, ip netip.Addr, now int64, observe bool) (string, bool, bool) {
	st := rt.state
	waited := false
	canQueue := rt.queueSize > 0 && !observe

	// 1. 预热上限
	if w := rt.warm; w != nil && !w.limiter.allow(now) {
		if !canQueue {
			return ReasonRate, false, false
		}
		reason, res := e.queueOn(ctx, rt, now, w.limiter.params.Load(), &w.limiter.tat)
		if res < 0 {
			return reason, false, false
		}
		waited = waited || res > 0
	}

	// 2. 速率限制
	for _, l := range rt.limits {
		var allowed bool
		var cell *atomic.Int64
		switch {
		case l.lease != nil:
			allowed = l.lease.allow(now)
		case l.global != nil:
			allowed = l.global.allow(now)
		default:
			h, ok := l.keyOf(e.seed, r, ip)
			if !ok {
				continue
			}
			cell = l.keyed.slot(h)
			allowed = l.keyed.params.Load().allow(cell, now)
		}
		if allowed {
			continue
		}
		reason := ReasonRate
		// 集群租约不支持预约（额度在 Redis 那边），直接拒绝。
		if canQueue && l.lease == nil {
			var params *gcraParams
			if l.global != nil {
				params, cell = l.global.params.Load(), &l.global.tat
			} else {
				params = l.keyed.params.Load()
			}
			var res int
			reason, res = e.queueOn(ctx, rt, mono(), params, cell)
			if res >= 0 {
				waited = waited || res > 0
				continue
			}
		}
		l.state.rejected.Add(1)
		e.noteHot(rt, l, r, ip)
		return reason, false, false
	}

	// 3. 并发
	cl := rt.conc
	if cl == nil {
		return "", waited, false
	}
	if observe {
		if cl.inflight.Load() >= cl.limit.Load() {
			return ReasonConcurrency, false, false
		}
		return "", waited, false
	}
	if cl.tryAcquire() {
		return "", waited, true
	}
	if !canQueue {
		return ReasonConcurrency, false, false
	}
	if st.queueDepth.Load() >= rt.queueSize {
		return ReasonQueueFull, false, false
	}
	st.queueDepth.Add(1)
	res := cl.acquireWait(ctx, int(rt.queueSize), time.Duration(rt.queueWait))
	st.queueDepth.Add(-1)
	switch res {
	case waitOK:
		return "", true, true
	case waitFull:
		return ReasonQueueFull, false, false
	default:
		return ReasonTimeout, false, false
	}
}

// queueOn 在一个速率桶上排队：预约名额并睡到可放行的时刻。
// 返回值：res = 0 无需等待，1 等待后放行，-1 拒绝（reason 为原因）。
func (e *Engine) queueOn(ctx context.Context, rt *ruleRT, now int64, p *gcraParams, cell *atomic.Int64) (string, int) {
	st := rt.state
	if st.queueDepth.Load() >= rt.queueSize {
		return ReasonQueueFull, -1
	}
	wait := p.reserve(cell, now, rt.queueWait)
	if wait < 0 {
		return ReasonTimeout, -1
	}
	if wait == 0 {
		return "", 0
	}
	st.queueDepth.Add(1)
	defer st.queueDepth.Add(-1)
	timer := time.NewTimer(time.Duration(wait))
	select {
	case <-timer.C:
		return "", 1
	case <-ctx.Done():
		timer.Stop()
		p.cancel(cell, mono())
		return ReasonTimeout, -1
	}
}

func (e *Engine) noteHot(rt *ruleRT, l *limitRT, r *http.Request, ip netip.Addr) {
	if !sampleHot() {
		return
	}
	key := l.keyDisplay(r, ip)
	if key == "" {
		return
	}
	rt.state.hot.hit(l.spec.Key, key, 8)
}

// ── 采样循环 ──

func (e *Engine) loop() {
	defer close(e.doneCh)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var tick int64
	lastSec := time.Now().Unix()
	for {
		select {
		case <-e.stopCh:
			return
		case now := <-ticker.C:
			tick++
			e.ovl.rotate()
			if tick%5 == 0 {
				e.ovl.sampleCPU()
			}
			c := e.cur.Load()
			if sec := now.Unix(); sec != lastSec {
				lastSec = sec
				e.perSecond(c, now, sec)
			}
			if tick%100 == 0 {
				e.sweep()
			}
			if tick%600 == 0 {
				e.decayHot(c)
			}
		}
	}
}

// perSecond 每秒一次：差分写秒级环、推进预热与自适应并发、分钟落库、实例数变化。
func (e *Engine) perSecond(c *compiled, now time.Time, sec int64) {
	enabled := c.cfg.Settings.Enabled
	monoNow := mono()
	var minutes []MinuteSample

	totals, h := e.global.read()
	dc, dh := e.globalSer.tick(sec, totals, h, e.inflight.Load(), 0)
	if enabled {
		if ms, ok := e.globalSer.accumulateMinute(sec, dc, dh, e.inflight.Load()); ok {
			minutes = append(minutes, ms)
		}
	}

	for _, rt := range c.rules {
		st := rt.state
		var limit float64
		if rt.warm != nil {
			limit = rt.warm.currentCeiling()
		}
		rc, rh := st.stats.read()
		d, dh := st.ser.tick(sec, rc, rh, st.inflight.Load(), limit)
		if ms, ok := st.ser.accumulateMinute(sec, d, dh, st.inflight.Load()); ok && enabled {
			ms.RuleID = rt.spec.ID
			minutes = append(minutes, ms)
		}
		if rt.warm != nil {
			if ev := rt.warm.evaluate(now, float64(d.Arrivals)); ev != "" {
				v := rt.warm.view()
				e.emit(Event{At: now.UTC(), Kind: ev, RuleID: rt.spec.ID, RuleName: rt.spec.Name,
					Detail: map[string]any{"ceiling": v.Ceiling, "demand": d.Arrivals}})
			}
		}
		if rt.conc != nil && rt.conc.adapt() {
			lim := rt.conc.limit.Load()
			if lim <= rt.conc.params.Load().minLimit {
				e.emit(Event{At: now.UTC(), Kind: EventConcurrencyDown, RuleID: rt.spec.ID, RuleName: rt.spec.Name,
					Detail: map[string]any{"limit": lim}})
			}
		}
		// 拒绝突增：一秒内拒绝 ≥ 100 且占到达的一半以上，且上一秒没报过。
		if rej := d.Rejected(); rej >= 100 && rej*2 >= d.Arrivals && !st.ser.burstFlag {
			st.ser.burstFlag = true
			e.emit(Event{At: now.UTC(), Kind: EventRejectBurst, RuleID: rt.spec.ID, RuleName: rt.spec.Name,
				Detail: map[string]any{"rejected": rej, "arrivals": d.Arrivals}})
		} else if d.Rejected() < 10 {
			st.ser.burstFlag = false
		}
	}

	if changed, active := e.ovl.updateActive(monoNow); changed {
		kind := EventOverloadEnd
		if active {
			kind = EventOverloadStart
		}
		v := e.ovl.view()
		e.emit(Event{At: now.UTC(), Kind: kind, Detail: map[string]any{
			"cpu": v.CPU, "inflight": v.Inflight, "capacity": v.Capacity, "dropped": v.Dropped,
		}})
	}

	if len(minutes) > 0 && e.sink != nil {
		e.sink.Minutes(minutes)
	}

	// 实例数变化：重算集群均分的桶参数。
	if e.co != nil {
		n := int64(maxInt(1, e.co.Instances()))
		if prev := e.instances.Swap(n); prev != n {
			e.reapplyCluster()
		}
	}
}

func (e *Engine) reapplyCluster() {
	c := e.cur.Load()
	n := int(e.instances.Load())
	e.statesMu.Lock()
	for _, rt := range c.rules {
		for i, l := range rt.limits {
			if l.cluster {
				e.configureLimit(rt.spec.ID, limitSig(i, l.spec), l, n, c.cfg.Settings.MaxTrackedKeys)
			}
		}
	}
	e.statesMu.Unlock()
}

func (e *Engine) emit(ev Event) {
	e.log.Info("traffic event", zap.String("kind", ev.Kind), zap.String("rule", ev.RuleID))
	if e.sink != nil {
		e.sink.Event(ev)
	}
}

func (e *Engine) sweep() {
	c := e.cur.Load()
	now := mono()
	for _, rt := range c.rules {
		for _, l := range rt.limits {
			if l.keyed != nil {
				l.keyed.sweep(now)
			}
		}
	}
}

func (e *Engine) decayHot(c *compiled) {
	for _, rt := range c.rules {
		rt.state.hot.decay()
	}
}

// ── 视图 ──

// Snapshot 本实例运行态。seconds 是序列跨度（60–900）。
func (e *Engine) Snapshot(seconds int) Snapshot {
	c := e.cur.Load()
	nowSec := time.Now().Unix()
	totals, _ := e.global.read()
	last, peak := e.globalSer.last()
	snap := Snapshot{
		Instance:   e.instance,
		Enabled:    c.cfg.Settings.Enabled,
		Revision:   c.cfg.Revision,
		StatsSince: time.UnixMilli(e.statsSince.Load()).UTC(),
		Now:        time.Now().UTC(),
		Totals:     totals,
		LastSec:    last,
		Exempt:     e.exempt.Load(),
		Inflight:   e.inflight.Load(),
		PeakRPS:    peak,
		Latency:    e.globalSer.recentLatency(),
		Overload:   e.ovl.view(),
		Series:     e.globalSer.points(seconds, nowSec-1),
	}
	snap.Rules = make([]RuleStats, 0, len(c.cfg.Rules))
	byID := make(map[string]*ruleRT, len(c.rules))
	for _, rt := range c.rules {
		byID[rt.spec.ID] = rt
	}
	for _, r := range c.cfg.Rules {
		rt := byID[r.ID]
		if rt == nil {
			snap.Rules = append(snap.Rules, RuleStats{ID: r.ID, Name: r.Name, Mode: r.Mode, Class: r.Class, Enabled: false, Limits: []LimitView{}, HotKeys: []HotKey{}})
			continue
		}
		snap.Rules = append(snap.Rules, e.ruleStats(rt, 0, nowSec))
	}
	return snap
}

// RuleStats 一条规则的详细运行态（含序列）。
func (e *Engine) RuleStats(id string, seconds int) (RuleStats, bool) {
	c := e.cur.Load()
	for _, rt := range c.rules {
		if rt.spec.ID == id {
			return e.ruleStats(rt, seconds, time.Now().Unix()), true
		}
	}
	for _, r := range c.cfg.Rules {
		if r.ID == id {
			return RuleStats{ID: r.ID, Name: r.Name, Mode: r.Mode, Class: r.Class, Limits: []LimitView{}, HotKeys: []HotKey{}, Series: []SeriesPoint{}}, true
		}
	}
	return RuleStats{}, false
}

func (e *Engine) ruleStats(rt *ruleRT, seconds int, nowSec int64) RuleStats {
	st := rt.state
	totals, _ := st.stats.read()
	last, _ := st.ser.last()
	rs := RuleStats{
		ID:       rt.spec.ID,
		Name:     rt.spec.Name,
		Mode:     rt.spec.Mode,
		Class:    rt.spec.Class,
		Enabled:  true,
		Totals:   totals,
		LastSec:  last,
		Inflight: st.inflight.Load(),
		Latency:  st.ser.recentLatency(),
		HotKeys:  st.hot.top(10),
	}
	if rt.conc != nil {
		rs.ConcurrencyLimit = rt.conc.limit.Load()
	}
	rs.QueueDepth = st.queueDepth.Load()
	rs.QueueCapacity = int(rt.queueSize)
	if rt.warm != nil {
		rs.Warmup = rt.warm.view()
	}
	n := float64(e.instances.Load())
	rs.Limits = make([]LimitView, 0, len(rt.limits))
	for _, l := range rt.limits {
		v := LimitView{
			Key: l.spec.Key, Name: l.spec.Name, Rate: l.spec.Rate, PeriodMs: l.spec.PeriodMs,
			Burst: l.spec.Burst, Scope: l.spec.Scope, Rejected: l.state.rejected.Load(),
			EffectiveRP: float64(l.spec.Rate),
		}
		if l.cluster && n > 1 {
			v.EffectiveRP = roundMs(float64(l.spec.Rate) / n)
		}
		if kl := l.keyed; kl != nil {
			v.TrackedKeys = kl.tracked.Load()
			v.Overflowed = kl.overflow.Load()
		}
		if lb := l.lease; lb != nil {
			v.LeaseMode = lb.mode()
		}
		rs.Limits = append(rs.Limits, v)
	}
	if seconds > 0 {
		rs.Series = st.ser.points(seconds, nowSec-1)
	}
	return rs
}

// Summary 集群心跳摘要。
func (e *Engine) Summary() NodeSummary {
	c := e.cur.Load()
	totals, _ := e.global.read()
	last, _ := e.globalSer.last()
	lat := e.globalSer.recentLatency()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	ov := e.ovl.view()
	return NodeSummary{
		Instance:   e.instance,
		Host:       e.host,
		StartedAt:  e.started,
		UpdatedAt:  time.Now().UTC(),
		Revision:   c.cfg.Revision,
		Enabled:    c.cfg.Settings.Enabled,
		LastSec:    last,
		Totals:     totals,
		Inflight:   e.inflight.Load(),
		CPU:        ov.CPU,
		Overloaded: ov.Active,
		P99Ms:      lat.P99,
		Goroutines: runtime.NumGoroutine(),
		HeapMB:     float64(ms.HeapAlloc/1024/1024*10) / 10,
		Cores:      runtime.GOMAXPROCS(0),
	}
}

// ResetStats 清零累计统计与曲线（配置、桶状态、预热与并发上限不动）。
func (e *Engine) ResetStats() {
	e.global.reset()
	e.globalSer.reset()
	e.exempt.Store(0)
	e.ovl.dropped.Store(0)
	e.statesMu.Lock()
	for _, st := range e.states {
		st.stats.reset()
		st.ser.reset()
		st.hot.reset()
		for _, l := range st.limits {
			l.rejected.Store(0)
		}
	}
	e.statesMu.Unlock()
	e.statsSince.Store(time.Now().UnixMilli())
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
