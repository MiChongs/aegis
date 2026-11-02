package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/regip"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// RegisterIPService 注册 IP 控制。规则见 docs/register-ip.md。
//
// 四条建号路径（密码、短信、第三方、卡密）在写库之前调 Admit，拿到一张票：
//
//	Admit   取事实（名单快照、属地、突增状态并行取）→ 前置判定 → 一个 Lua 脚本原子地数完全部规则、
//	        都没超才占位 → 判定 → 拒绝 / 加验 / 放行
//	Commit  账号建好之后：累计规则的占位转正、分钟桶加一（异步），台账入队批量写
//	Release 没建成（账号已存在、风控拦了……）：撤销占位，名额还回去
//
// 判定是 regip.Decide 这个纯函数，线上、模拟与 IP 查询共用。热路径上不查库：名单是 15 秒的内存快照，
// 突增状态 5 秒一算，累计计数只在 Redis 里缺失时按台账回填一次；台账与记录由多协程批量写。
//
// 基础设施出错（Redis、数据库）按策略 failMode 处置，默认放行并记 warn。
type RegisterIPService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	counters *redisrepo.RegisterIPRepository
	app      *AppService
	location *LocationService
	now      func() time.Time

	lists       sync.Map // appID → *registerIPListSnapshot
	deviceLists sync.Map // appID → *registerDeviceListSnapshot
	listGroup   singleflight.Group
	surge       sync.Map // appID → *registerIPSurgeSnapshot

	events *asyncBatchWriter[regip.Event]
	ledger *asyncBatchWriter[pgrepo.RegisterIPLedgerRow]

	hitsMu     sync.Mutex
	hits       map[int64]int64
	deviceHits map[int64]int64

	// proxy 判断地址是否是自己的入口代理（见 SetProxyFilter）；proxyReported 每个应用只告警一次
	proxy         func(netip.Addr) bool
	proxyReported sync.Map

	stopOnce sync.Once
	stop     chan struct{}
	loops    sync.WaitGroup
}

// 错误码，见 auth_protocol_catalog.go。
const (
	errCodeRegisterIPMissing     = 40025 // 拿不到注册 IP 且策略要求拒绝
	errCodeRegisterIPLimited     = 40902 // 配额规则：当前网络注册的账号数已达上限（沿用旧开关的码）
	errCodeRegisterIPForbidden   = 40919 // 拒绝名单 / 属地 / 运营商：当前网络环境不允许注册
	errCodeRegisterIPPenalty     = 42917 // 惩罚期内
	errCodeRegisterIPSurge       = 42918 // 注册量突增，暂停注册
	errCodeRegisterIPBusy        = 42919 // 计数存储不可用且策略要求拒绝
	errCodeRegisterDeviceMissing = 40931 // 没上报设备标识且策略要求拒绝
	errCodeRegisterDeviceInvalid = 40932 // 设备标识无效且策略要求拒绝
)

const (
	registerIPListTTL     = 15 * time.Second
	registerIPSurgeTTL    = 5 * time.Second
	registerIPLocationMax = 1500 * time.Millisecond
	registerIPEventKeep   = 180 * 24 * time.Hour
)

type registerIPListSnapshot struct {
	lists    *regip.CompiledLists
	loadedAt time.Time
}

type registerDeviceListSnapshot struct {
	lists    *regip.CompiledDeviceLists
	loadedAt time.Time
}

type registerIPSurgeSnapshot struct {
	state  regip.SurgeState
	config regip.Surge
	at     time.Time
}

// NewRegisterIPService 构造服务。location 可以为 nil（不做属地 / 运营商判定）。
func NewRegisterIPService(log *zap.Logger, pg *pgrepo.Repository, counters *redisrepo.RegisterIPRepository, app *AppService, location *LocationService) *RegisterIPService {
	if log == nil {
		log = zap.NewNop()
	}
	s := &RegisterIPService{
		log: log, pg: pg, counters: counters, app: app, location: location, now: time.Now,
		hits: map[int64]int64{}, deviceHits: map[int64]int64{}, stop: make(chan struct{}),
	}
	s.events = newAsyncBatchWriter("register_ip.events", log, 8192, 2, 500, 500*time.Millisecond, s.flushEvents)
	s.ledger = newAsyncBatchWriter("register_ip.ledger", log, 8192, 2, 500, 200*time.Millisecond, s.flushLedger)
	return s
}

// SetProxyFilter 接入「这个地址是不是受信代理」的判断（客户端 IP 判定器的受信网段）。
//
// 上游 CDN 节点没带客户端地址、或入口反代不信任它而丢了转发头时，判定只能退回直连对端，
// 拿到的是 Docker 内网负载均衡、入口反代这类地址。拿它计数，经过同一入口的所有人会挤在
// 一个地址上互相占名额、一起进惩罚期。这类地址按「拿不到 IP」处理，设备那条线照常数。
func (s *RegisterIPService) SetProxyFilter(fn func(netip.Addr) bool) {
	if s != nil {
		s.proxy = fn
	}
}

// proxyAddress 地址是否是受信代理；是的话每个应用告警一次，提示去修入口的转发头。
func (s *RegisterIPService) proxyAddress(appID int64, addr netip.Addr) bool {
	if s.proxy == nil || !s.proxy(addr) {
		return false
	}
	if _, seen := s.proxyReported.LoadOrStore(appID, struct{}{}); !seen {
		s.log.Warn("注册准入：客户端 IP 是受信代理的地址，按拿不到 IP 处理",
			zap.Int64("appid", appID), zap.String("ip", addr.String()),
			zap.String("cause", "上游没带客户端地址，或入口反代（Traefik forwardedHeaders.trustedIPs）不信任上游而丢了转发头"))
	}
	return true
}

// Start 启动批量写入协程与后台循环（名单命中次数落库、过期名单与旧记录清理）。
func (s *RegisterIPService) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.events.Start()
	s.ledger.Start()
	s.loops.Add(2)
	go s.hitsLoop()
	go s.cleanupLoop(ctx)
}

// Stop 停止后台循环，把队列里剩下的台账与记录写完。
func (s *RegisterIPService) Stop(ctx context.Context) {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stop)
		s.loops.Wait()
		s.flushHits()
		s.ledger.Stop(ctx)
		s.events.Stop(ctx)
	})
}

/* ───────────────────── 准入 ───────────────────── */

// RegisterIPRequest 一次注册的现场。
type RegisterIPRequest struct {
	AppID int64
	// App 调用方已经拉过的应用；为空时按 AppID 取
	App       *appdomain.App
	IP        string
	Method    string
	Account   string
	DeviceID  string
	UserAgent string
}

// RegisterIPTicket Admit 放行后的票：账号建好调 Commit，没建成调 Release。两者都幂等、都可在 nil 上调用。
type RegisterIPTicket struct {
	svc      *RegisterIPService
	req      RegisterIPRequest
	addr     regip.Address
	loc      regip.Location
	token    string
	at       int64
	reserved bool
	counters []redisrepo.RegisterIPCounter
	surgeTTL time.Duration
	done     atomic.Bool
}

// Commit 账号建好了：占位转正、写台账。全部异步，不拖慢注册。
func (t *RegisterIPTicket) Commit(userID int64, account string) {
	if t == nil || t.svc == nil || !t.done.CompareAndSwap(false, true) {
		return
	}
	s := t.svc
	if t.addr.Valid() {
		if account == "" {
			account = t.req.Account
		}
		s.ledger.Enqueue(pgrepo.RegisterIPLedgerRow{
			AppID: t.req.AppID, UserID: userID, Account: account, IP: t.addr.Addr, Method: t.req.Method,
			Country: t.loc.Country, Province: t.loc.Province, City: t.loc.City, ISP: t.loc.ISP,
			DeviceID: t.req.DeviceID, CreatedAt: s.now(),
		})
	}
	if s.counters == nil {
		return
	}
	counters := t.counters
	if !t.reserved {
		counters = nil // 没占位（存储出错放行的）：只给分钟桶加一，累计计数下次按台账回填
	}
	at := s.now()
	ttl := t.surgeTTL
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.counters.Commit(ctx, t.req.AppID, t.token, at, ttl, counters); err != nil {
			s.log.Warn("注册 IP 计数确认失败", zap.Int64("appid", t.req.AppID), zap.Error(err))
		}
	}()
}

// Release 没建成：撤销占位。
func (t *RegisterIPTicket) Release() {
	if t == nil || t.svc == nil || !t.done.CompareAndSwap(false, true) || !t.reserved || t.svc.counters == nil {
		return
	}
	s := t.svc
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.counters.Rollback(ctx, t.req.AppID, t.token, t.at, t.counters); err != nil {
			s.log.Warn("注册 IP 占位撤销失败", zap.Int64("appid", t.req.AppID), zap.Error(err))
		}
	}()
}

// registerIPEvaluation 一次判定的全部中间结果：准入、模拟与 IP 查询共用。
type registerIPEvaluation struct {
	policy   regip.Policy
	facts    regip.Facts
	decision regip.Decision
	reserve  *redisrepo.RegisterIPReserveResult
	counters []redisrepo.RegisterIPCounter
	// ruleOf 计数器下标 → 规则下标
	ruleOf []int
	token  string
	// penalty / devicePenalty 地址与设备两侧的惩罚主体，缺哪边为空
	penalty       string
	devicePenalty string
}

// evalOptions 判定的开关：线上占位；模拟与查询只数不占，并可改写事实。
type evalOptions struct {
	reserve  bool
	override *regip.SimulateInput
}

// Admit 注册准入。放行返回票（可能为 nil：没有可记的东西），拦截返回业务错误。
func (s *RegisterIPService) Admit(ctx context.Context, req RegisterIPRequest) (*RegisterIPTicket, error) {
	if s == nil {
		return nil, nil
	}
	app := req.App
	if app == nil && s.app != nil {
		loaded, err := s.app.GetApp(ctx, req.AppID)
		if err != nil {
			return nil, err
		}
		app = loaded
	}
	var settings map[string]any
	if app != nil {
		settings = app.Settings
	}
	policy, _ := regip.FromSettings(settings)
	captchaPassed := riskCaptchaPassed(ctx)
	eval := s.evaluate(ctx, req, policy, captchaPassed, evalOptions{reserve: true})
	ticket := &RegisterIPTicket{
		svc: s, req: req, addr: eval.facts.Address, loc: eval.facts.Location, token: eval.token,
		counters: eval.counters, surgeTTL: surgeBucketTTL(policy),
	}
	if eval.reserve != nil {
		ticket.reserved = eval.reserve.Reserved
		ticket.at = eval.reserve.At
	}
	d := eval.decision
	if d.Notable() {
		s.recordDecision(req, eval)
	}
	if !d.Blocking() {
		return ticket, nil
	}
	// 脚本只在「没有挡住的超限」时占位，判定挡住了却占了位只可能是两边口径不一致：撤销，宁可多放一次名额
	ticket.Release()
	if d.Reason == regip.ReasonRule && d.Outcome == regip.OutcomeReject {
		if subject := strikeSubject(policy, d.RuleID, eval); subject != "" {
			s.strike(req.AppID, policy, subject)
		}
	}
	return nil, registerIPError(d)
}

// strikeSubject 配额拒绝记在谁头上：设备一侧的规则（同一设备、同一设备换地址）记设备，
// 地址一侧的规则记地址（或网段）；城市与整个应用的规则不怪任何一个地址或设备。
func strikeSubject(policy regip.Policy, ruleID string, eval *registerIPEvaluation) string {
	for _, rule := range policy.Rules {
		if rule.ID != ruleID {
			continue
		}
		switch {
		case regip.DeviceScope(rule.Scope):
			return eval.devicePenalty
		case regip.IPScope(rule.Scope):
			return eval.penalty
		}
	}
	return ""
}

// registerIPError 判定结论翻译成业务错误。
func registerIPError(d regip.Decision) error {
	if d.Outcome == regip.OutcomeChallenge {
		return apperrors.New(errCodeRiskCaptchaRequired, http.StatusBadRequest, d.Message)
	}
	switch d.Reason {
	case regip.ReasonMissingIP:
		return apperrors.New(errCodeRegisterIPMissing, http.StatusBadRequest, d.Message)
	case regip.ReasonMissingDevice:
		return apperrors.New(errCodeRegisterDeviceMissing, http.StatusConflict, d.Message)
	case regip.ReasonInvalidDevice:
		return apperrors.New(errCodeRegisterDeviceInvalid, http.StatusConflict, d.Message)
	case regip.ReasonListDeny, regip.ReasonGeo, regip.ReasonISP:
		return apperrors.New(errCodeRegisterIPForbidden, http.StatusConflict, d.Message)
	case regip.ReasonPenalty:
		return apperrors.New(errCodeRegisterIPPenalty, http.StatusTooManyRequests, d.Message)
	case regip.ReasonSurge:
		return apperrors.New(errCodeRegisterIPSurge, http.StatusTooManyRequests, d.Message)
	case regip.ReasonFailure:
		return apperrors.New(errCodeRegisterIPBusy, http.StatusTooManyRequests, d.Message)
	}
	return apperrors.New(errCodeRegisterIPLimited, http.StatusConflict, d.Message)
}

// IsRegisterIPRejection 错误是否为注册 IP 控制的拦截。
func IsRegisterIPRejection(err error) bool {
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	switch appErr.Code {
	case errCodeRegisterIPMissing, errCodeRegisterIPLimited, errCodeRegisterIPForbidden,
		errCodeRegisterIPPenalty, errCodeRegisterIPSurge, errCodeRegisterIPBusy,
		errCodeRegisterDeviceMissing, errCodeRegisterDeviceInvalid:
		return true
	}
	return false
}

func surgeBucketTTL(policy regip.Policy) time.Duration {
	minutes := regip.MaxSurgeBaselineMinutes + 60
	if policy.Surge.Enabled {
		minutes = policy.Surge.BaselineMinutes + policy.Surge.WindowMinutes + 10
	}
	return time.Duration(minutes) * time.Minute
}

// evaluate 取齐事实并判定。
//
// 地址与设备是两条互补的线：缺了哪条，只依赖那条的规则就不参与，另一条照常数；
// 名单两边合并（任一边拒绝即拒绝），惩罚期两边各查一个键、同一个脚本里读。
func (s *RegisterIPService) evaluate(ctx context.Context, req RegisterIPRequest, policy regip.Policy, captchaPassed bool, opts evalOptions) *registerIPEvaluation {
	eval := &registerIPEvaluation{policy: policy, token: newRegisterIPToken()}
	facts := regip.Facts{Method: req.Method, CaptchaPassed: captchaPassed, Device: regip.ParseDevice(req.DeviceID, policy.Device.MinLength)}
	addr, ok := regip.ParseAddress(req.IP, policy.Network)
	if ok && s.proxyAddress(req.AppID, addr.Addr) {
		ok = false
	}
	if ok {
		facts.Address = addr
	}
	dev := facts.Device
	if !policy.Enabled || (!ok && !dev.Valid) {
		eval.facts = facts
		eval.decision = regip.Decide(policy, facts)
		return eval
	}

	// 地址名单、设备名单、属地、突增四样互不依赖，并行取。各自失败都降级：名单空、属地未知、不算突增
	var ipMatch, deviceMatch *regip.ListMatch
	var g errgroup.Group
	if ok {
		g.Go(func() error {
			ipMatch = s.listsFor(ctx, req.AppID).Match(addr.Addr)
			return nil
		})
		if policy.NeedsLocation() {
			g.Go(func() error {
				facts.Location = s.resolveLocation(ctx, addr.Addr.String())
				return nil
			})
		}
	}
	if dev.Valid {
		g.Go(func() error {
			deviceMatch = s.deviceListsFor(ctx, req.AppID).Match(dev)
			return nil
		})
	}
	if policy.Surge.Enabled {
		g.Go(func() error {
			facts.Surge = s.surgeFor(ctx, req.AppID, policy.Surge)
			return nil
		})
	}
	_ = g.Wait()
	facts.List = regip.CombineLists(ipMatch, deviceMatch)
	if override := opts.override; override != nil {
		if override.Location != nil {
			facts.Location = *override.Location
			facts.Location.Resolved = override.Location.Resolved || override.Location.Country != ""
		}
		if override.SurgeActive != nil {
			facts.Surge.Active = *override.SurgeActive
		}
	}
	if m := facts.List; m != nil && m.EntryID > 0 && opts.reserve {
		if m.Target == regip.ListTargetDevice {
			s.countDeviceHit(m.EntryID)
		} else {
			s.countHit(m.EntryID)
		}
	}

	pre, final := regip.PreDecide(policy, facts)
	if final && pre.Blocking() {
		eval.facts = facts
		eval.decision = pre
		return eval
	}
	// 放行名单与观察模式：照样数、照样占位（台账与窗口都该记上这一笔），只是谁也挡不住
	bypass := final
	if ok {
		eval.penalty = addr.Host.String()
		if policy.Penalty.Scope == regip.ScopeSubnet {
			eval.penalty = addr.Subnet.String()
		}
	}
	if dev.Valid && policy.Penalty.Device {
		eval.devicePenalty = dev.Subject()
	}
	for i, rule := range policy.Rules {
		subject := regip.SubjectKey(rule.Scope, addr, dev, facts.Location)
		member := regip.MemberKey(rule.Scope, addr, dev)
		if !rule.Applies(req.Method) || subject == "" || regip.DistinctScope(rule.Scope) && member == "" {
			continue
		}
		limit := regip.EffectiveLimit(rule.Limit, pre.Factor, pre.Tighten)
		counter := redisrepo.RegisterIPCounter{
			Rule: rule.ID, Subject: subject, Limit: limit, WindowMs: int64(rule.WindowSeconds) * 1000, Member: member,
			Blocking: !bypass && regip.RuleBlocks(policy, rule, captchaPassed),
		}
		switch {
		case regip.DistinctScope(rule.Scope):
			counter.Kind = redisrepo.RegisterIPCounterDistinct
		case rule.Lifetime():
			counter.Kind = redisrepo.RegisterIPCounterLifetime
		case limit > redisrepo.RegisterIPBucketedThreshold:
			counter.Kind = redisrepo.RegisterIPCounterBucketed
		default:
			counter.Kind = redisrepo.RegisterIPCounterLog
		}
		eval.counters = append(eval.counters, counter)
		eval.ruleOf = append(eval.ruleOf, i)
	}
	penaltyBlocks := !bypass && policy.Penalty.Enabled && policy.Mode == regip.ModeEnforce

	// 回填累计计数要用到地址与设备，先把已取齐的事实挂上
	eval.facts = facts
	result, err := s.reserve(ctx, req.AppID, policy, eval, opts.reserve, penaltyBlocks)
	facts.Counts = map[string]int{}
	if err != nil {
		s.log.Warn("注册准入计数失败", zap.Int64("appid", req.AppID), zap.Error(err))
		facts.StoreFailed = true
	} else {
		eval.reserve = result
		for i, count := range result.Counts {
			facts.Counts[policy.Rules[eval.ruleOf[i]].ID] = count
			eval.counters[i].Added = result.Added[i]
		}
		if policy.Penalty.Enabled {
			facts.PenaltySeconds = int((result.PenaltyMs + 999) / 1000)
			if policy.Penalty.Device {
				facts.DevicePenaltySeconds = int((result.DevicePenaltyMs + 999) / 1000)
			}
		}
	}
	if override := opts.override; override != nil {
		for id, count := range override.Counts {
			facts.Counts[id] = max(0, count)
		}
		if override.PenaltySeconds != nil {
			facts.PenaltySeconds = max(0, *override.PenaltySeconds)
		}
		if override.DevicePenaltySeconds != nil {
			facts.DevicePenaltySeconds = max(0, *override.DevicePenaltySeconds)
		}
		if facts.StoreFailed && len(override.Counts) > 0 {
			facts.StoreFailed = false
		}
	}
	eval.facts = facts
	eval.decision = regip.Decide(policy, facts)
	return eval
}

// reserve 跑计数脚本；缺累计计数时按台账并行回填后再跑一次。
func (s *RegisterIPService) reserve(ctx context.Context, appID int64, policy regip.Policy, eval *registerIPEvaluation, reserve, penaltyBlocks bool) (*redisrepo.RegisterIPReserveResult, error) {
	if s.counters == nil {
		return nil, errors.New("计数存储未接入")
	}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := s.counters.Reserve(ctx, appID, eval.penalty, eval.devicePenalty, eval.token, reserve, penaltyBlocks, eval.counters)
		if err != nil {
			return nil, err
		}
		if len(result.NeedSeed) == 0 {
			return result, nil
		}
		if err := s.seedLifetime(ctx, appID, policy, eval, result.NeedSeed); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("累计计数回填后仍缺失")
}

// seedLifetime 按台账回填缺失的累计计数。每个计数器一条带索引的 COUNT，最多 4 条并行。
func (s *RegisterIPService) seedLifetime(ctx context.Context, appID int64, policy regip.Policy, eval *registerIPEvaluation, indexes []int) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, index := range indexes {
		if index < 0 || index >= len(eval.counters) {
			continue
		}
		counter := eval.counters[index]
		rule := policy.Rules[eval.ruleOf[index]]
		g.Go(func() error {
			query := pgrepo.RegisterIPCountQuery{AppID: appID, Methods: rule.Methods}
			switch rule.Scope {
			case regip.ScopeIP:
				query.Prefix = eval.facts.Address.Host
			case regip.ScopeSubnet:
				query.Prefix = eval.facts.Address.Subnet
			case regip.ScopeDevice:
				query.Device = eval.facts.Device.ID
			case regip.ScopeCity:
				query.City = counter.Subject
			}
			// 没有网段、设备也没有属地的查询数的是整个应用：主体缺失时宁可报错（按故障处置），也不能拿全应用的数当成这一个的
			if !query.Prefix.IsValid() && query.City == "" && query.Device == "" {
				return errors.New("累计计数回填缺少统计主体：" + rule.ID)
			}
			count, err := s.pg.CountRegisterIPLedger(gctx, query)
			if err != nil {
				return err
			}
			return s.counters.SeedLifetime(gctx, appID, counter, count)
		})
	}
	return g.Wait()
}

// strike 记一次配额拒绝；达到阈值时进惩罚期并留一条记录。
func (s *RegisterIPService) strike(appID int64, policy regip.Policy, subject string) {
	if !policy.Penalty.Enabled || policy.Mode != regip.ModeEnforce || subject == "" || s.counters == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cfg := policy.Penalty
	levelTTL := time.Duration(cfg.MaxSeconds)*2*time.Second + 24*time.Hour
	level, err := s.counters.Strike(ctx, appID, subject, newRegisterIPToken(), time.Duration(cfg.StrikeWindowSeconds)*time.Second, cfg.Strikes, levelTTL)
	if err != nil {
		s.log.Warn("注册 IP 惩罚计次失败", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	if level == 0 {
		return
	}
	seconds := regip.PenaltyDuration(cfg, level)
	if err := s.counters.Penalize(ctx, appID, subject, level, time.Duration(seconds)*time.Second); err != nil {
		s.log.Warn("注册 IP 进入惩罚期失败", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	s.events.Enqueue(regip.Event{
		AppID: appID, Kind: regip.EventPenalized, Reason: regip.ReasonPenalty, Subnet: subject,
		IP: singleIPOf(subject), Detail: "第 " + strconv.Itoa(level) + " 次进入惩罚期，时长 " + regip.DurationLabel(seconds),
		Extra: map[string]any{"level": level, "seconds": seconds}, At: s.now(),
	})
}

func newRegisterIPToken() string {
	var buf [12]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
