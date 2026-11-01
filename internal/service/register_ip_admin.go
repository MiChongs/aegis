package service

import (
	"context"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/regip"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 注册 IP 控制的事实来源（名单快照、属地、突增）、批量写入与管理端能力。规则见 docs/register-ip.md。

/* ───────────────────── 事实 ───────────────────── */

// listsFor 应用的名单快照：15 秒内存缓存，过期时同一应用只有一个请求去库里拉（singleflight），
// 其余请求在拉取期间继续用旧快照。库出错时用旧快照，没有旧快照就当没有名单。
func (s *RegisterIPService) listsFor(ctx context.Context, appID int64) *regip.CompiledLists {
	now := s.now()
	cached, _ := s.lists.Load(appID)
	snapshot, _ := cached.(*registerIPListSnapshot)
	if snapshot != nil && now.Sub(snapshot.loadedAt) < registerIPListTTL {
		return snapshot.lists
	}
	load := func() *regip.CompiledLists {
		value, err, _ := s.listGroup.Do(itoa64(appID), func() (any, error) {
			entries, err := s.pg.ListActiveRegisterIPLists(context.WithoutCancel(ctx), appID)
			if err != nil {
				return nil, err
			}
			compiled := regip.CompileLists(entries, s.now())
			s.lists.Store(appID, &registerIPListSnapshot{lists: compiled, loadedAt: s.now()})
			return compiled, nil
		})
		if err != nil {
			s.log.Warn("注册 IP 名单加载失败", zap.Int64("appid", appID), zap.Error(err))
			if snapshot != nil {
				return snapshot.lists
			}
			return nil
		}
		compiled, _ := value.(*regip.CompiledLists)
		return compiled
	}
	if snapshot != nil {
		// 旧快照先顶上，后台刷新：热路径上永远不等库
		go load()
		return snapshot.lists
	}
	return load()
}

func (s *RegisterIPService) invalidateLists(appID int64) { s.lists.Delete(appID) }

// resolveLocation 查属地，最多等 1.5 秒；查不到返回 Resolved=false。
func (s *RegisterIPService) resolveLocation(ctx context.Context, ip string) regip.Location {
	if s.location == nil {
		return regip.Location{}
	}
	ctx, cancel := context.WithTimeout(ctx, registerIPLocationMax)
	defer cancel()
	return toRegIPLocation(s.location.Resolve(ctx, ip))
}

func toRegIPLocation(loc IPLocation) regip.Location {
	out := regip.Location{Country: loc.Country, Province: loc.Region, City: loc.City, ISP: loc.ISP}
	if out.ISP == "" {
		out.ISP = loc.Network.Organization
	}
	out.Resolved = !loc.IsPrivate && strings.TrimSpace(loc.Country) != "" && loc.Country != "保留" && loc.Country != "未知"
	return out
}

// surgeFor 应用的突增状态：5 秒一算（一次 MGET 取全部分钟桶），配置变了立即重算。
func (s *RegisterIPService) surgeFor(ctx context.Context, appID int64, cfg regip.Surge) regip.SurgeState {
	now := s.now()
	if cached, ok := s.surge.Load(appID); ok {
		snapshot := cached.(*registerIPSurgeSnapshot)
		if snapshot.config == cfg && now.Sub(snapshot.at) < registerIPSurgeTTL {
			return snapshot.state
		}
	}
	if s.counters == nil {
		return regip.SurgeState{}
	}
	series, err := s.counters.MinuteSeries(ctx, appID, now, cfg.BaselineMinutes+cfg.WindowMinutes)
	if err != nil {
		s.log.Warn("注册突增分钟桶读取失败", zap.Int64("appid", appID), zap.Error(err))
		return regip.SurgeState{}
	}
	state := regip.DetectSurge(series, cfg)
	s.surge.Store(appID, &registerIPSurgeSnapshot{state: state, config: cfg, at: now})
	return state
}

/* ───────────────────── 记录与批量写入 ───────────────────── */

func (s *RegisterIPService) recordDecision(req RegisterIPRequest, eval *registerIPEvaluation) {
	kind := regip.EventOf(eval.decision)
	if kind == "" {
		return
	}
	d := eval.decision
	event := regip.Event{
		AppID: req.AppID, Kind: kind, Reason: d.Reason, RuleID: d.RuleID, RuleName: d.RuleName, Method: req.Method,
		IP: req.IP, Account: req.Account, DeviceID: req.DeviceID, Detail: d.Detail, At: s.now(),
		Country: eval.facts.Location.Country, Province: eval.facts.Location.Province, City: eval.facts.Location.City,
		ISP: eval.facts.Location.ISP,
	}
	if eval.facts.Address.Valid() {
		event.IP = eval.facts.Address.Addr.String()
		event.Subnet = eval.facts.Address.Subnet.String()
	}
	for _, hit := range d.Rules {
		if hit.RuleID == d.RuleID && d.RuleID != "" {
			event.Count, event.Limit = hit.Count, hit.Limit
		}
	}
	extra := map[string]any{}
	if d.Observed != "" {
		extra["observed"] = d.Observed
	}
	if d.Factor > 0 {
		extra["factor"] = d.Factor
	}
	if d.Tighten > 0 {
		extra["tighten"] = d.Tighten
	}
	if eval.facts.Surge.Active {
		extra["surge"] = map[string]any{"current": eval.facts.Surge.Current, "expected": eval.facts.Surge.Expected, "score": eval.facts.Surge.Score}
	}
	if eval.facts.List != nil {
		extra["list"] = eval.facts.List
	}
	if req.UserAgent != "" {
		extra["userAgent"] = truncateRunes(req.UserAgent, 200)
	}
	if len(extra) > 0 {
		event.Extra = extra
	}
	s.events.Enqueue(event)
}

// flushEvents 批量写拦截记录；属地没查过的这里一次批量补上（ResolveMany 合并成一次远程请求）。
func (s *RegisterIPService) flushEvents(ctx context.Context, events []regip.Event) error {
	if s.location != nil {
		missing := []string{}
		for _, event := range events {
			if event.Country == "" && event.IP != "" {
				missing = append(missing, event.IP)
			}
		}
		if len(missing) > 0 {
			resolved := s.location.ResolveMany(ctx, missing)
			for i := range events {
				if loc, ok := resolved[events[i].IP]; ok && events[i].Country == "" {
					l := toRegIPLocation(loc)
					events[i].Country, events[i].Province, events[i].City, events[i].ISP = l.Country, l.Province, l.City, l.ISP
				}
			}
		}
	}
	return s.pg.InsertRegisterIPEvents(ctx, events)
}

// flushLedger 批量写台账，同样批量补属地。
func (s *RegisterIPService) flushLedger(ctx context.Context, rows []pgrepo.RegisterIPLedgerRow) error {
	if s.location != nil {
		missing := []string{}
		for _, row := range rows {
			if row.Country == "" {
				missing = append(missing, row.IP.String())
			}
		}
		if len(missing) > 0 {
			resolved := s.location.ResolveMany(ctx, missing)
			for i := range rows {
				if loc, ok := resolved[rows[i].IP.String()]; ok && rows[i].Country == "" {
					l := toRegIPLocation(loc)
					if l.Resolved {
						rows[i].Country, rows[i].Province, rows[i].City, rows[i].ISP = l.Country, l.Province, l.City, l.ISP
					}
				}
			}
		}
	}
	return s.pg.InsertRegisterIPLedger(ctx, rows)
}

func (s *RegisterIPService) countHit(id int64) {
	s.hitsMu.Lock()
	s.hits[id]++
	s.hitsMu.Unlock()
}

func (s *RegisterIPService) flushHits() {
	s.hitsMu.Lock()
	hits := s.hits
	s.hits = map[int64]int64{}
	s.hitsMu.Unlock()
	if len(hits) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.pg.AddRegisterIPListHits(ctx, hits, s.now()); err != nil {
		s.log.Warn("注册 IP 名单命中次数写入失败", zap.Error(err))
	}
}

func (s *RegisterIPService) hitsLoop() {
	defer s.loops.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.flushHits()
		}
	}
}

// cleanupLoop 每 6 小时清一次：过期超过 7 天的名单项、180 天前的拦截记录。
func (s *RegisterIPService) cleanupLoop(ctx context.Context) {
	defer s.loops.Done()
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			cctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			if n, err := s.pg.PurgeExpiredRegisterIPLists(cctx, 7*24*time.Hour); err != nil {
				s.log.Warn("清理过期注册 IP 名单失败", zap.Error(err))
			} else if n > 0 {
				s.log.Info("已清理过期注册 IP 名单", zap.Int64("count", n))
			}
			if _, err := s.pg.PurgeRegisterIPEvents(cctx, s.now().Add(-registerIPEventKeep)); err != nil {
				s.log.Warn("清理注册 IP 拦截记录失败", zap.Error(err))
			}
			cancel()
		}
	}
}

/* ───────────────────── 策略 ───────────────────── */

// RegisterIPPolicyView 管理端读到的策略。
type RegisterIPPolicyView struct {
	Policy regip.Policy `json:"policy"`
	// Configured false 表示还没用新策略保存过，当前值是按旧开关 registerCheckIp 推导的
	Configured bool `json:"configured"`
	// Defaults 一套推荐起点，控制台「恢复推荐值」用
	Defaults regip.Policy `json:"defaults"`
}

// Policy 读取策略。
func (s *RegisterIPService) Policy(ctx context.Context, appID int64) (*RegisterIPPolicyView, error) {
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy, configured := regip.FromSettings(app.Settings)
	return &RegisterIPPolicyView{Policy: policy, Configured: configured, Defaults: regip.DefaultPolicy()}, nil
}

// SavePolicy 保存策略，并写回旧开关（只给还在读它的旧客户端看）。
func (s *RegisterIPService) SavePolicy(ctx context.Context, appID int64, policy regip.Policy) (*RegisterIPPolicyView, error) {
	normalized, err := policy.Normalize()
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings[regip.SettingsKey] = normalized.ToSettings()
	settings[regip.LegacySettingsKey] = normalized.LegacyValue()
	if _, err := s.app.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	s.surge.Delete(appID)
	return &RegisterIPPolicyView{Policy: normalized, Configured: true, Defaults: regip.DefaultPolicy()}, nil
}

func (s *RegisterIPService) policyOf(ctx context.Context, appID int64) (regip.Policy, error) {
	view, err := s.Policy(ctx, appID)
	if err != nil {
		return regip.Policy{}, err
	}
	return view.Policy, nil
}

/* ───────────────────── 概览 ───────────────────── */

// RegisterIPOverview 概览。
type RegisterIPOverview struct {
	Policy       regip.Policy                `json:"policy"`
	Configured   bool                        `json:"configured"`
	Days         int                         `json:"days"`
	TZ           string                      `json:"tz"`
	Totals       map[string]int64            `json:"totals"`
	Trend        []map[string]any            `json:"trend"`
	Reasons      map[string]int64            `json:"reasons"`
	Methods      map[string]int64            `json:"methods"`
	Rules        []pgrepo.RegisterIPRuleStat `json:"rules"`
	Distribution []map[string]any            `json:"distribution"`
	TopIPs       []pgrepo.RegisterIPTopItem  `json:"topIps"`
	TopSubnets   []pgrepo.RegisterIPTopItem  `json:"topSubnets"`
	TopBlocked   []pgrepo.RegisterIPTopItem  `json:"topBlocked"`
	TopCities    []pgrepo.RegisterIPTopItem  `json:"topCities"`
	Lists        map[string]int64            `json:"lists"`
	Penalties    []regip.PenaltyEntry        `json:"penalties"`
	PenaltyCount int64                       `json:"penaltyCount"`
	Surge        *regip.SurgeState           `json:"surge"`
	Writers      map[string]asyncBatchStats  `json:"writers"`
}

var registerIPDistributionBuckets = []string{"1", "2", "3-5", "6-10", "11+"}

// Overview 概览：days 天内的注册与拦截、排行、分布，以及此刻的惩罚期与突增状态。
func (s *RegisterIPService) Overview(ctx context.Context, appID int64, days int, tz string) (*RegisterIPOverview, error) {
	if days <= 0 {
		days = 7
	}
	days = min(days, 90)
	location := time.Local
	if tz = strings.TrimSpace(tz); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			location = loaded
		} else {
			tz = ""
		}
	}
	if tz == "" {
		tz = location.String()
		if tz == "Local" {
			tz = "UTC"
		}
	}
	view, err := s.Policy(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := s.now().In(location)
	startDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).AddDate(0, 0, -(days - 1))
	stats, err := s.pg.RegisterIPOverviewStats(ctx, appID, startDay, tz, view.Policy.Network.IPv4Subnet, view.Policy.Network.IPv6Subnet, 10)
	if err != nil {
		return nil, err
	}
	out := &RegisterIPOverview{
		Policy: view.Policy, Configured: view.Configured, Days: days, TZ: tz,
		Totals: map[string]int64{
			"registrations": stats.Registrations, "distinctIps": stats.DistinctIPs, "distinctSubnets": stats.DistinctSubnet,
		},
		Reasons: stats.Reasons, Methods: stats.Methods, Rules: stats.Rules, TopIPs: stats.TopIPs, TopSubnets: stats.TopSubnets,
		TopBlocked: stats.TopBlocked, TopCities: stats.TopCities, Lists: stats.Lists, Penalties: []regip.PenaltyEntry{},
		Writers: map[string]asyncBatchStats{"events": s.events.Stats(), "ledger": s.ledger.Stats()},
	}
	for _, kind := range regip.EventKinds {
		out.Totals[kind] = stats.Kinds[kind]
	}
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		point := map[string]any{"date": day, "registered": stats.Trend[day]["registered"]}
		for _, kind := range regip.EventKinds {
			point[kind] = stats.Trend[day][kind]
		}
		out.Trend = append(out.Trend, point)
	}
	for _, bucket := range registerIPDistributionBuckets {
		out.Distribution = append(out.Distribution, map[string]any{"bucket": bucket, "count": stats.Distribution[bucket]})
	}
	sort.SliceStable(out.Rules, func(i, j int) bool { return out.Rules[i].Count > out.Rules[j].Count })
	if s.counters != nil {
		if penalties, total, err := s.counters.Penalties(ctx, appID, 10); err == nil {
			out.PenaltyCount = total
			out.Penalties = toPenaltyEntries(penalties, s.now())
		} else {
			s.log.Warn("读取注册 IP 惩罚期失败", zap.Int64("appid", appID), zap.Error(err))
		}
		// 没开突增检测时也按推荐参数算一份给概览看：开不开之前先看看平时的量
		cfg := view.Policy.Surge
		if !cfg.Enabled {
			cfg = regip.DefaultPolicy().Surge
		}
		state := s.surgeFor(ctx, appID, cfg)
		out.Surge = &state
	}
	return out, nil
}

func toPenaltyEntries(items []redisrepo.RegisterIPPenalty, now time.Time) []regip.PenaltyEntry {
	out := make([]regip.PenaltyEntry, 0, len(items))
	for _, item := range items {
		remaining := int(item.ExpiresAt.Sub(now).Seconds())
		if remaining <= 0 {
			continue
		}
		out = append(out, regip.PenaltyEntry{Subject: item.Subject, Level: item.Level, ExpiresAt: item.ExpiresAt, Remaining: remaining})
	}
	return out
}

// Penalties 正在惩罚期的主体。
func (s *RegisterIPService) Penalties(ctx context.Context, appID int64, limit int) ([]regip.PenaltyEntry, int64, error) {
	if s.counters == nil {
		return []regip.PenaltyEntry{}, 0, nil
	}
	limit = min(max(limit, 1), 500)
	items, total, err := s.counters.Penalties(ctx, appID, limit)
	if err != nil {
		return nil, 0, err
	}
	return toPenaltyEntries(items, s.now()), total, nil
}

/* ───────────────────── 模拟与查询 ───────────────────── */

// RegisterIPSimulation 一次模拟的结果。
type RegisterIPSimulation struct {
	Decision regip.Decision   `json:"decision"`
	Address  map[string]any   `json:"address"`
	Location regip.Location   `json:"location"`
	List     *regip.ListMatch `json:"list"`
	Penalty  int              `json:"penaltySeconds"`
	Surge    regip.SurgeState `json:"surge"`
	// Draft 是否按草稿策略判的
	Draft bool `json:"draft"`
	// StoreFailed 计数存储不可用，结果按故障处置推演
	StoreFailed bool `json:"storeFailed"`
}

// Simulate 模拟一次注册：只数不占，可改写计数、属地、突增与惩罚期。
func (s *RegisterIPService) Simulate(ctx context.Context, appID int64, in regip.SimulateInput) (*RegisterIPSimulation, error) {
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return nil, err
	}
	draft := in.Policy != nil
	if draft {
		normalized, err := in.Policy.Normalize()
		if err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		policy = normalized
	}
	method := strings.TrimSpace(in.Method)
	if method == "" {
		method = regip.MethodPassword
	}
	req := RegisterIPRequest{AppID: appID, IP: in.IP, Method: method}
	// 模拟要看到全部规则的计数，哪怕草稿没开启
	probe := policy
	probe.Enabled = true
	eval := s.evaluate(ctx, req, probe, in.CaptchaPassed, evalOptions{override: &in})
	decision := eval.decision
	if !policy.Enabled {
		decision = regip.Decide(policy, eval.facts)
		decision.Rules = eval.decision.Rules
	}
	out := &RegisterIPSimulation{
		Decision: decision, Location: eval.facts.Location, List: eval.facts.List, Penalty: eval.facts.PenaltySeconds,
		Surge: eval.facts.Surge, Draft: draft, StoreFailed: eval.facts.StoreFailed,
	}
	out.Surge.Series = nil
	if eval.facts.Address.Valid() {
		out.Address = addressView(eval.facts.Address)
	}
	return out, nil
}

func addressView(addr regip.Address) map[string]any {
	family := 4
	if addr.Addr.Is6() {
		family = 6
	}
	return map[string]any{"ip": addr.Addr.String(), "host": addr.Host.String(), "subnet": addr.Subnet.String(), "family": family,
		"private": addr.Addr.IsPrivate() || addr.Addr.IsLoopback() || addr.Addr.IsLinkLocalUnicast()}
}

// RegisterIPInspection IP 查询的结果。
type RegisterIPInspection struct {
	Address  map[string]any    `json:"address"`
	Location regip.Location    `json:"location"`
	List     *regip.ListMatch  `json:"list"`
	Lists    []regip.ListEntry `json:"lists"`
	Penalty  struct {
		Subject   string     `json:"subject"`
		Remaining int        `json:"remaining"`
		Level     int        `json:"level"`
		ExpiresAt *time.Time `json:"expiresAt"`
	} `json:"penalty"`
	// Decision 此刻用这个地址按密码注册会怎样
	Decision regip.Decision    `json:"decision"`
	Usage    []regip.RuleUsage `json:"usage"`
	// HostRegistrations / SubnetRegistrations 该地址 / 网段未解除的累计注册数
	HostRegistrations   int64                `json:"hostRegistrations"`
	SubnetRegistrations int64                `json:"subnetRegistrations"`
	Registrations       []regip.Registration `json:"registrations"`
	SubnetRecent        []regip.Registration `json:"subnetRecent"`
	Events              []regip.Event        `json:"events"`
}

// Inspect 查一个地址此刻的全部状况。
func (s *RegisterIPService) Inspect(ctx context.Context, appID int64, ip string) (*RegisterIPInspection, error) {
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return nil, err
	}
	addr, ok := regip.ParseAddress(ip, policy.Network)
	if !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入有效的 IP 地址")
	}
	probe := policy
	probe.Enabled = true
	needLocation := probe.NeedsLocation()
	eval := s.evaluate(ctx, RegisterIPRequest{AppID: appID, IP: addr.Addr.String(), Method: regip.MethodPassword}, probe, false, evalOptions{})
	out := &RegisterIPInspection{Address: addressView(addr), Location: eval.facts.Location, List: eval.facts.List, Decision: eval.decision}
	if !policy.Enabled {
		out.Decision = regip.Decide(policy, eval.facts)
		out.Decision.Rules = eval.decision.Rules
	}
	if !needLocation {
		out.Location = s.resolveLocation(ctx, addr.Addr.String())
	}
	out.Penalty.Subject = eval.penalty
	if s.counters != nil && eval.penalty != "" {
		if remaining, level, err := s.counters.PenaltyOf(ctx, appID, eval.penalty); err == nil {
			out.Penalty.Remaining = int(remaining.Seconds())
			out.Penalty.Level = level
			if remaining > 0 {
				at := s.now().Add(remaining)
				out.Penalty.ExpiresAt = &at
			}
		}
	}
	out.Usage = make([]regip.RuleUsage, 0, len(eval.decision.Rules))
	for _, hit := range eval.decision.Rules {
		usage := regip.RuleUsage{RuleHit: hit, Remaining: max(0, hit.Limit-hit.Count)}
		for i, counter := range eval.counters {
			if policy.Rules[eval.ruleOf[i]].ID != hit.RuleID || s.counters == nil {
				continue
			}
			if oldest, err := s.counters.WindowOldest(ctx, appID, counter); err == nil && !oldest.IsZero() {
				reset := oldest.Add(time.Duration(hit.WindowSeconds) * time.Second)
				usage.ResetAt = &reset
			}
		}
		out.Usage = append(out.Usage, usage)
	}
	lists, _, err := s.pg.ListRegisterIPLists(ctx, regip.ListQuery{AppID: appID, Keyword: addr.Addr.String(), IncludeExpired: true, Page: 1, PageSize: 20})
	if err != nil {
		return nil, err
	}
	out.Lists = lists
	if out.Registrations, out.HostRegistrations, err = s.pg.RegisterIPLedgerByPrefix(ctx, appID, addr.Host, 20); err != nil {
		return nil, err
	}
	if out.SubnetRecent, out.SubnetRegistrations, err = s.pg.RegisterIPLedgerByPrefix(ctx, appID, addr.Subnet, 20); err != nil {
		return nil, err
	}
	events, _, err := s.pg.ListRegisterIPEvents(ctx, regip.EventQuery{AppID: appID, IP: addr.Subnet.String(), Page: 1, PageSize: 20})
	if err != nil {
		return nil, err
	}
	out.Events = events
	return out, nil
}

/* ───────────────────── 解除 ───────────────────── */

// RegisterIPReleaseInput 解除限制。
type RegisterIPReleaseInput struct {
	IP string `json:"ip"`
	// Scope ip：只解除这个地址；subnet：连同它所在的网段
	Scope string `json:"scope"`
	// Counters 清空窗口计数（默认开）
	Counters *bool `json:"counters"`
	// Penalty 解除惩罚期；ResetLevel 连惩罚等级一起清
	Penalty    *bool `json:"penalty"`
	ResetLevel bool  `json:"resetLevel"`
	// Ledger 把台账标为已解除：不再计入累计上限（账号不受影响）
	Ledger bool   `json:"ledger"`
	Note   string `json:"note"`
}

// RegisterIPReleaseResult 解除了什么。
type RegisterIPReleaseResult struct {
	Subjects        []string `json:"subjects"`
	CountersCleared int      `json:"countersCleared"`
	PenaltyCleared  bool     `json:"penaltyCleared"`
	LedgerReleased  int64    `json:"ledgerReleased"`
}

// Release 解除一个地址（或它所在网段）的限制。
func (s *RegisterIPService) Release(ctx context.Context, appID int64, in RegisterIPReleaseInput, operator string) (*RegisterIPReleaseResult, error) {
	policy, err := s.policyOf(ctx, appID)
	if err != nil {
		return nil, err
	}
	addr, ok := regip.ParseAddress(in.IP, policy.Network)
	if !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入有效的 IP 地址")
	}
	scopes := map[string]netip.Prefix{regip.ScopeIP: addr.Host}
	if in.Scope == regip.ScopeSubnet {
		scopes[regip.ScopeSubnet] = addr.Subnet
	}
	out := &RegisterIPReleaseResult{}
	for _, prefix := range scopes {
		out.Subjects = append(out.Subjects, prefix.String())
	}
	sort.Strings(out.Subjects)
	if s.counters != nil && (in.Counters == nil || *in.Counters) {
		counters := []redisrepo.RegisterIPCounter{}
		for _, rule := range policy.Rules {
			if prefix, ok := scopes[rule.Scope]; ok {
				counters = append(counters, redisrepo.RegisterIPCounter{Rule: rule.ID, Subject: prefix.String()})
			}
		}
		if err := s.counters.ClearCounters(ctx, appID, counters); err != nil {
			return nil, err
		}
		out.CountersCleared = len(counters)
	}
	if s.counters != nil && (in.Penalty == nil || *in.Penalty) {
		for _, prefix := range scopes {
			if err := s.counters.ClearPenalty(ctx, appID, prefix.String(), in.ResetLevel); err != nil {
				return nil, err
			}
		}
		out.PenaltyCleared = true
	}
	if in.Ledger {
		prefix := scopes[regip.ScopeIP]
		if subnet, ok := scopes[regip.ScopeSubnet]; ok {
			prefix = subnet
		}
		released, err := s.pg.ReleaseRegisterIPLedger(ctx, appID, prefix)
		if err != nil {
			return nil, err
		}
		out.LedgerReleased = released
		// 累计计数按台账重算：删掉缓存，下次注册时回填
		if s.counters != nil {
			counters := []redisrepo.RegisterIPCounter{}
			for _, rule := range policy.Rules {
				if !rule.Lifetime() {
					continue
				}
				for _, p := range []netip.Prefix{addr.Host, addr.Subnet} {
					counters = append(counters, redisrepo.RegisterIPCounter{Rule: rule.ID, Subject: p.String()})
				}
			}
			_ = s.counters.ClearCounters(ctx, appID, counters)
		}
	}
	detail := "解除限制：" + strings.Join(out.Subjects, "、")
	if note := strings.TrimSpace(in.Note); note != "" {
		detail += "（" + note + "）"
	}
	s.events.Enqueue(regip.Event{
		AppID: appID, Kind: regip.EventReleased, IP: addr.Addr.String(), Subnet: addr.Subnet.String(), Detail: detail,
		Operator: operator, At: s.now(),
		Extra: map[string]any{"countersCleared": out.CountersCleared, "penaltyCleared": out.PenaltyCleared, "ledgerReleased": out.LedgerReleased},
	})
	return out, nil
}

/* ───────────────────── 名单 ───────────────────── */

func normalizeListInput(kind string, factor float64) (string, float64, error) {
	switch kind {
	case regip.ListAllow, regip.ListDeny:
		return kind, 1, nil
	case regip.ListElevated:
		if factor <= 1 || factor > regip.MaxListFactor {
			return "", 0, apperrors.New(40000, http.StatusBadRequest, "放宽倍数需要大于 1、不超过 1000")
		}
		return kind, factor, nil
	}
	return "", 0, apperrors.New(40000, http.StatusBadRequest, "名单类型只能是放行、拒绝或放宽")
}

// AddLists 批量添加名单，一次最多 500 项。已存在的网段按新值覆盖。
func (s *RegisterIPService) AddLists(ctx context.Context, appID int64, in regip.ListInput, operator string) ([]regip.ListEntry, error) {
	kind, factor, err := normalizeListInput(in.Kind, in.Factor)
	if err != nil {
		return nil, err
	}
	in.Kind, in.Factor = kind, factor
	values := []string{}
	for _, value := range in.Values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' || r == '，' || r == ';' }) {
			if part = strings.TrimSpace(part); part != "" {
				values = append(values, part)
			}
		}
	}
	if len(values) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请至少填写一个地址或网段")
	}
	if len(values) > 500 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多添加 500 项")
	}
	seen := map[netip.Prefix]bool{}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := regip.ParsePrefix(value)
		if err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		if !seen[prefix] {
			seen[prefix] = true
			prefixes = append(prefixes, prefix)
		}
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "到期时间需要晚于现在")
	}
	items, err := s.pg.UpsertRegisterIPLists(ctx, appID, prefixes, in, operator)
	if err != nil {
		return nil, err
	}
	s.invalidateLists(appID)
	return items, nil
}

// UpdateList 修改一项名单。
func (s *RegisterIPService) UpdateList(ctx context.Context, appID, id int64, in regip.ListUpdate) (*regip.ListEntry, error) {
	kind, factor, err := normalizeListInput(in.Kind, in.Factor)
	if err != nil {
		return nil, err
	}
	in.Kind, in.Factor = kind, factor
	item, err := s.pg.UpdateRegisterIPList(ctx, appID, id, in)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40400, http.StatusNotFound, "名单项不存在")
	}
	s.invalidateLists(appID)
	return item, nil
}

// DeleteLists 删除名单项。
func (s *RegisterIPService) DeleteLists(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, apperrors.New(40000, http.StatusBadRequest, "请选择要删除的名单项")
	}
	n, err := s.pg.DeleteRegisterIPLists(ctx, appID, ids)
	if err != nil {
		return 0, err
	}
	s.invalidateLists(appID)
	return n, nil
}

// ListLists 名单列表。
func (s *RegisterIPService) ListLists(ctx context.Context, q regip.ListQuery) ([]regip.ListEntry, int64, error) {
	q.Page, q.PageSize = normalizePageLimit(q.Page, q.PageSize, 200)
	return s.pg.ListRegisterIPLists(ctx, q)
}

// Events 拦截记录。
func (s *RegisterIPService) Events(ctx context.Context, q regip.EventQuery) ([]regip.Event, int64, error) {
	q.Page, q.PageSize = normalizePageLimit(q.Page, q.PageSize, 200)
	if err := validateIPFilter(q.IP); err != nil {
		return nil, 0, err
	}
	return s.pg.ListRegisterIPEvents(ctx, q)
}

// Registrations 注册台账。
func (s *RegisterIPService) Registrations(ctx context.Context, q regip.RegistrationQuery) ([]regip.Registration, int64, error) {
	q.Page, q.PageSize = normalizePageLimit(q.Page, q.PageSize, 200)
	if err := validateIPFilter(q.IP); err != nil {
		return nil, 0, err
	}
	return s.pg.ListRegisterIPLedger(ctx, q)
}

// validateIPFilter 列表的地址筛选：地址或网段，写错了如实报 400 而不是 500。
func validateIPFilter(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if _, err := regip.ParsePrefix(raw); err != nil {
		return apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return nil
}

func singleIPOf(subject string) string {
	if prefix, err := netip.ParsePrefix(subject); err == nil && prefix.IsSingleIP() {
		return prefix.Addr().String()
	}
	return ""
}
