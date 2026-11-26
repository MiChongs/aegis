package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"
	"aegis/internal/event"
	fw "aegis/internal/firewall"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/hashicorp/golang-lru/v2/expirable"
	redislib "github.com/redis/go-redis/v9"
	"github.com/ulule/limiter/v3"
	redisstore "github.com/ulule/limiter/v3/drivers/store/redis"
	"go.uber.org/zap"
)

// 防火墙分两段（docs/firewall.md）：
//
//	Handler()    入口段：IP 白 / 黑名单、封禁、方法与长度、扫描器 UA、探测路径与快速签名、
//	             自定义规则、限流。排在 ReplayGuard 与解密中间件之前，只看连接、请求行与请求头。
//	WAFHandler() 载荷段：Coraza + CRS。排在 AppGateway / AppEncryption **之后**，
//	             所以 Transport v2（sealed）与旧 X-Aegis-Encrypted 请求在这里已经是明文；
//	             拦截响应写进解密中间件包好的 Writer，客户端收到的是加密过的错误信封，照常能解开。
//
// v1 把 Coraza 放在入口段：加密请求的请求体与 `_payload` 查询串在那里只是一段密文，
// CRS 等于没检查；为了压误报又给所有 JSON 请求体关掉了 SQLi / XSS / RCE 规则。

// CorazaAllowedMethods 与 firewall.AllowedMethods 相同，transport 层的路由核对测试引用它。
const CorazaAllowedMethods = fw.AllowedMethods

// 请求上下文里的防火墙标记（Handler 写、WAFHandler 读）。
const (
	ctxFirewallMark = "aegis.firewall.mark"
	// ctxTransportKey 解密中间件写入的载荷来源（sealed / legacy），WAF 据此标注事件。
	ctxTransportKey = "aegis.transport"
)

type firewallMark struct {
	skipWAF bool
	ruleRef int64
}

func markOf(c *gin.Context) *firewallMark {
	if v, ok := c.Get(ctxFirewallMark); ok {
		if m, ok := v.(*firewallMark); ok {
			return m
		}
	}
	m := &firewallMark{}
	c.Set(ctxFirewallMark, m)
	return m
}

// SetRequestTransport 由解密中间件调用：这次请求的载荷是从哪种加密里解出来的。
func SetRequestTransport(c *gin.Context, transport string) {
	c.Set(ctxTransportKey, transport)
}

type firewallState struct {
	cfg               config.FirewallConfig
	waf               *fw.WAF
	rules             *fw.RuleSet
	ruleErrors        []string
	exclusions        int
	globalLimiter     *limiter.Limiter
	authLimiter       *limiter.Limiter
	adminLimiter      *limiter.Limiter
	allowed           *fw.IPSet
	blocked           *fw.IPSet
	trusted           *fw.IPSet
	blockedUserAgents []string
	blockedPathPrefix []string
	blockedFragments  []string
	bodyExempt        []string
}

// FirewallSnapshot 当前运行态（类型定义在引擎包，避免 service ↔ middleware 成环）。
type FirewallSnapshot = fw.Snapshot

// FirewallCounters 本副本计数。
type FirewallCounters = fw.Counters

// BanChecker 动态 IP 封禁检查接口
type BanChecker interface {
	IsBanned(ctx context.Context, ip string) (bool, error)
}

// GeoInfo 自定义规则用到的归属地。
type GeoInfo struct {
	Country, Region, City, ISP, ASN string
}

// GeoResolver 查 IP 归属地（可能走远程接口，防火墙自己会缓存）。
type GeoResolver func(ctx context.Context, ip string) GeoInfo

type Firewall struct {
	log         *zap.Logger
	redisClient *redislib.Client
	keyPrefix   string
	publisher   *event.Publisher
	banChecker  BanChecker

	state      atomic.Pointer[firewallState]
	reloadMu   sync.Mutex
	version    atomic.Uint64
	reloadedAt atomic.Pointer[time.Time]

	// 规则与例外来自数据库（FirewallPolicyService 加载后 SetPolicy），配置来自平台设置（Reload）。
	policyMu   sync.Mutex
	rules      []fw.CustomRule
	exclusions []fw.Exclusion

	geo      GeoResolver
	geoCache *expirable.LRU[string, GeoInfo]
	// observed 观察类事件去重：同一来源同一条规则一分钟只记一次，观察模式不至于把表写爆。
	observed *expirable.LRU[string, struct{}]

	inspected, edgeBlocked, wafBlocked, observedCount, decrypted, wafErrors atomic.Int64
	wafNanos                                                                atomic.Int64
	startedAt                                                               time.Time
}

func NewFirewall(cfg config.FirewallConfig, log *zap.Logger, redisClient *redislib.Client, keyPrefix string, publisher *event.Publisher, banChecker BanChecker) (*Firewall, error) {
	if log == nil {
		log = zap.NewNop()
	}
	f := &Firewall{
		log:         log,
		redisClient: redisClient,
		keyPrefix:   keyPrefix,
		publisher:   publisher,
		banChecker:  banChecker,
		geoCache:    expirable.NewLRU[string, GeoInfo](50000, nil, 30*time.Minute),
		observed:    expirable.NewLRU[string, struct{}](20000, nil, time.Minute),
		startedAt:   time.Now().UTC(),
	}
	if err := f.Reload(cfg); err != nil {
		return nil, err
	}
	return f, nil
}

// SetGeoResolver 注入归属地查询（只有引用了 country / asn 等变量的自定义规则会用到）。
func (f *Firewall) SetGeoResolver(resolver GeoResolver) {
	if f != nil {
		f.geo = resolver
	}
}

func (f *Firewall) current() *firewallState {
	if f == nil {
		return nil
	}
	return f.state.Load()
}

// ──────────────────────────────────────
// 入口段
// ──────────────────────────────────────

func (f *Firewall) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		state := f.current()
		if state == nil || !state.cfg.Enabled {
			c.Next()
			return
		}

		path := strings.TrimSpace(c.Request.URL.Path)
		if path == "/healthz" || path == "/readyz" || path == "/api/ws" {
			c.Next()
			return
		}

		ip := sanitizeIP(c.ClientIP())
		addr, err := netip.ParseAddr(ip)
		if err != nil {
			f.block(c, http.StatusForbidden, 40390, "invalid_ip", "当前请求已被安全策略拦截")
			return
		}
		addr = addr.Unmap()

		if !state.allowed.Empty() && !state.allowed.Contains(addr) {
			f.block(c, http.StatusForbidden, 40391, "not_in_allowlist", "当前请求已被安全策略拦截")
			return
		}
		if state.blocked.Contains(addr) {
			f.block(c, http.StatusForbidden, 40391, "blocked_cidr", "当前请求已被安全策略拦截")
			return
		}
		if f.banChecker != nil {
			if ext, ok := f.banChecker.(ExtendedBanChecker); ok {
				if dec, _ := ext.CheckBan(c.Request.Context(), ip); dec.Banned {
					f.applyBanMode(c, ip, dec, state)
					return
				}
			} else if banned, _ := f.banChecker.IsBanned(c.Request.Context(), ip); banned {
				f.applyBanMode(c, ip, firewalldomain.BanDecision{Banned: true, Mode: state.cfg.DefaultBanMode, Reason: "banned_ip"}, state)
				return
			}
		}
		mark := markOf(c)
		// 可信网段：封禁之后、其余检查之前。监控探针与办公出口不该被签名、限流和 WAF 误伤。
		if state.trusted.Contains(addr) {
			mark.skipWAF = true
			c.Next()
			return
		}
		if blockedMethod(c.Request.Method) {
			f.block(c, http.StatusNotImplemented, 50190, "blocked_method", "服务能力暂未开放")
			return
		}
		if state.cfg.MaxPathLength > 0 && len(path) > state.cfg.MaxPathLength {
			f.block(c, http.StatusForbidden, 40392, "path_too_long", "当前请求已被安全策略拦截")
			return
		}
		rawQuery := c.Request.URL.RawQuery
		if state.cfg.MaxQueryLength > 0 && len(rawQuery) > state.cfg.MaxQueryLength && !hasEncryptedQuery(c.Request) {
			f.block(c, http.StatusForbidden, 40393, "query_too_long", "当前请求已被安全策略拦截")
			return
		}
		if blocked, reason := blockByUserAgent(state, c.GetHeader("User-Agent")); blocked {
			f.block(c, http.StatusForbidden, 40394, reason, "当前请求已被安全策略拦截")
			return
		}
		if blocked, reason := blockByPathOrQuery(state, path, rawQuery); blocked {
			f.block(c, http.StatusForbidden, 40395, reason, "当前请求已被安全策略拦截")
			return
		}
		if !state.rules.Empty() {
			if f.applyCustomRules(c, state, ip, mark) {
				return
			}
			if mark.ruleRef < 0 { // allow：跳过限流与 WAF
				c.Next()
				return
			}
		}
		if limited, resetAt := f.rateLimit(state, c, ip); limited {
			retryAfter := maxInt64(1, resetAt-time.Now().Unix())
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			f.block(c, http.StatusTooManyRequests, 42900, "rate_limited", "请求过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// hasEncryptedQuery 加密 GET 的查询串是 `_payload=<密文>`，长度随明文膨胀约 1.4 倍，不按明文的上限卡。
func hasEncryptedQuery(req *http.Request) bool {
	return claimsEncryption(req) && strings.HasPrefix(req.URL.RawQuery, "_payload=")
}

// claimsEncryption 客户端声明了加密载荷（入口段还没解开，只看声明）。
func claimsEncryption(req *http.Request) bool {
	if strings.TrimSpace(req.Header.Get(gwHeaderProtocol)) != "" {
		return true
	}
	return isEncryptedRequest(req)
}

// applyCustomRules 求值自定义规则；返回 true 表示请求已被拦截。allow 动作用 mark.ruleRef = -1 告知调用方。
func (f *Firewall) applyCustomRules(c *gin.Context, state *firewallState, ip string, mark *firewallMark) bool {
	env := f.ruleEnv(c, state, ip)
	terminal, logged := state.rules.Evaluate(env)
	for _, rule := range logged {
		f.emitObserved(c, rule.ID, "custom_log", firewalldomain.EventDetail{
			Action: firewalldomain.EventActionLog, Stage: firewalldomain.StageEdge, RuleRef: rule.ID, RuleName: rule.Name,
		})
	}
	if terminal == nil {
		return false
	}
	switch terminal.Action {
	case fw.ActionAllow:
		mark.skipWAF = true
		mark.ruleRef = -1
	case fw.ActionSkipWAF:
		mark.skipWAF = true
		mark.ruleRef = terminal.ID
	case fw.ActionBlock:
		f.edgeBlocked.Add(1)
		detail := firewalldomain.EventDetail{
			Action: firewalldomain.EventActionBlock, Stage: firewalldomain.StageEdge,
			RuleRef: terminal.ID, RuleName: terminal.Name, BanSeconds: terminal.BanSeconds,
		}
		mode := strings.TrimSpace(terminal.BlockMode)
		if mode == "" || mode == firewalldomain.BanModeForbidden {
			f.log.Warn("firewall custom rule blocked request",
				zap.Int64("rule_id", terminal.ID), zap.String("rule", terminal.Name),
				zap.String("ip", ip), zap.String("path", c.Request.URL.Path), zap.String("request_id", requestID(c)))
			response.Error(c, http.StatusForbidden, 40395, "当前请求已被安全策略拦截")
			c.Abort()
			f.emitEvent(c, "custom_rule", http.StatusForbidden, 40395, detail, nil, "", "")
			return true
		}
		// 自定义响应方式复用封禁模式（伪装 404、拖延、蜜罐……）；事件仍按自定义规则记。
		f.applyBanModeWith(c, ip, firewalldomain.BanDecision{Banned: true, Mode: mode, Reason: "custom_rule"}, state, &detail)
		return true
	}
	return false
}

func (f *Firewall) ruleEnv(c *gin.Context, state *firewallState, ip string) *fw.RuleEnv {
	req := c.Request
	headers := make(map[string]string, len(req.Header))
	for key, values := range req.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}
	args := map[string]string{}
	if !hasEncryptedQuery(req) {
		for key, values := range req.URL.Query() {
			if len(values) > 0 {
				args[key] = values[0]
			}
		}
	}
	env := &fw.RuleEnv{
		IP:          ip,
		Method:      req.Method,
		Path:        req.URL.Path,
		Query:       req.URL.RawQuery,
		Host:        req.Host,
		UA:          req.Header.Get("User-Agent"),
		Referer:     req.Header.Get("Referer"),
		ContentType: req.Header.Get("Content-Type"),
		Headers:     headers,
		Args:        args,
		App:         appKeyFromGatewayPath(req.URL.Path),
		Encrypted:   claimsEncryption(req),
	}
	env.FillTime(time.Now())
	if state.rules.NeedGeo() {
		geo := f.lookupGeo(req.Context(), ip)
		env.Country, env.Region, env.City, env.ISP, env.ASN = geo.Country, geo.Region, geo.City, geo.ISP, geo.ASN
	}
	return env
}

func (f *Firewall) lookupGeo(ctx context.Context, ip string) GeoInfo {
	if f.geo == nil {
		return GeoInfo{}
	}
	if cached, ok := f.geoCache.Get(ip); ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	info := f.geo(ctx, ip)
	f.geoCache.Add(ip, info)
	return info
}

// ──────────────────────────────────────
// 载荷段（解密之后）
// ──────────────────────────────────────

// WAFHandler 解密之后的 Coraza 检测。必须注册在 AppGateway 与 AppEncryption 之后。
func (f *Firewall) WAFHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		state := f.current()
		if state == nil || !state.cfg.Enabled || state.waf == nil {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" || path == "/api/ws" {
			c.Next()
			return
		}
		if mark := markOf(c); mark.skipWAF {
			c.Next()
			return
		}
		transport := firewalldomain.TransportPlain
		if v := c.GetString(ctxTransportKey); v != "" {
			transport = v
			f.decrypted.Add(1)
		}

		started := time.Now()
		body, inspectBody, err := f.readBodyForInspection(c.Request, state)
		if errors.Is(err, fw.ErrBodyTooLarge) {
			f.wafBlocked.Add(1)
			response.Error(c, http.StatusRequestEntityTooLarge, 41390, "请求体超过安全检查上限")
			c.Abort()
			f.emitEvent(c, "body_too_large", http.StatusRequestEntityTooLarge, 41390, firewalldomain.EventDetail{
				Action: firewalldomain.EventActionBlock, Stage: firewalldomain.StagePayload, Transport: transport,
			}, nil, "", "")
			return
		}
		if err != nil {
			f.wafError(c, err, transport)
			return
		}

		headers := c.Request.Header
		contentLength := int64(-1)
		if inspectBody {
			contentLength = int64(len(body))
			if effective := fw.EffectiveContentType(headers.Get("Content-Type"), body); effective != headers.Get("Content-Type") {
				headers = headers.Clone()
				headers.Set("Content-Type", effective)
			}
		}
		uri := c.Request.URL.EscapedPath()
		if c.Request.URL.RawQuery != "" {
			uri += "?" + c.Request.URL.RawQuery
		}
		_, port := splitRemote(c.Request.RemoteAddr)
		txn, res := state.waf.Begin(fw.Request{
			RemoteIP: sanitizeIP(c.ClientIP()), RemotePort: port, Method: c.Request.Method, URI: uri,
			Proto: c.Request.Proto, Host: c.Request.Host, Headers: headers, Transport: transport, ContentLength: contentLength,
		})
		closeTxn := func() {
			if err := txn.Close(); err != nil {
				f.log.Debug("firewall coraza close failed", zap.Error(err))
			}
		}
		if res == nil {
			var reader io.Reader
			if inspectBody && len(body) > 0 && txn.BodyAccessible() {
				reader = bytes.NewReader(body)
			}
			res, err = txn.RequestBody(reader)
			if err != nil {
				closeTxn()
				f.wafError(c, err, transport)
				return
			}
		}
		f.inspected.Add(1)
		f.wafNanos.Add(time.Since(started).Nanoseconds())

		if res.Interrupted {
			closeTxn()
			f.blockWAF(c, res, transport)
			return
		}
		if res.WouldBlock {
			f.recordObserved(c, res, transport, firewalldomain.StagePayload, "waf_observed")
		}

		if !txn.ResponseAccessible() {
			closeTxn()
			c.Next()
			return
		}
		tee := newWAFResponseTee(c.Writer, state.waf.ResponseBodyLimit())
		c.Writer = tee
		c.Next()
		c.Writer = tee.ResponseWriter
		out, err := txn.Response(tee.Status(), tee.Header(), tee.captured.Bytes())
		closeTxn()
		if err != nil {
			f.log.Debug("firewall outbound inspection failed", zap.Error(err))
			return
		}
		if out != nil && out.WouldBlock && out.OutboundScore > 0 {
			f.recordObserved(c, out, transport, firewalldomain.StageResponse, "waf_outbound")
		}
	}
}

// readBodyForInspection 读出要检查的请求体并原样接回请求。
//
//   - 管理端（未开启管理端请求体检查）与豁免前缀：不读，phase 1 的 ctl:requestBodyAccess=Off 同样会跳过；
//   - 超出上限的文件上传：已读的前缀接回原流，只检查头与 URI（截断的 multipart 喂给 WAF 只会解析失败）；
//   - 其余类型超限：ErrBodyTooLarge。
//
// 旧加密（AppEncryption）解出来的明文在临时文件里：读完要 Close 原 Body，临时文件才会被删。
func (f *Firewall) readBodyForInspection(req *http.Request, state *firewallState) ([]byte, bool, error) {
	if req.Body == nil || req.Body == http.NoBody || !bodyInspectable(req.URL.Path, state) {
		return nil, false, nil
	}
	limit := int64(state.waf.RequestBodyLimit())
	original := req.Body
	body, err := io.ReadAll(io.LimitReader(original, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("读取请求体: %w", err)
	}
	if int64(len(body)) > limit {
		if isFileUploadContentType(req.Header.Get("Content-Type")) {
			req.Body = struct {
				io.Reader
				io.Closer
			}{io.MultiReader(bytes.NewReader(body), original), original}
			return nil, false, nil
		}
		_ = original.Close()
		req.Body = http.NoBody
		return nil, false, fw.ErrBodyTooLarge
	}
	_ = original.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body, true, nil
}

func bodyInspectable(path string, state *firewallState) bool {
	// 防火墙自己的管理接口（测试台请求体就是攻击样本），与 directives 里的 1100099 对应
	if strings.HasPrefix(path, "/api/admin/system/firewall/") {
		return false
	}
	if !state.cfg.CorazaInspectAdminBody && strings.HasPrefix(path, "/api/admin/") {
		return false
	}
	for _, prefix := range state.bodyExempt {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

func isFileUploadContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "multipart/form-data") || strings.HasPrefix(contentType, "application/octet-stream")
}

func splitRemote(remote string) (string, int) {
	host, port, err := net.SplitHostPort(remote)
	if err != nil {
		return remote, 0
	}
	n, _ := strconv.Atoi(port)
	return host, n
}

func (f *Firewall) wafError(c *gin.Context, err error, transport string) {
	f.wafErrors.Add(1)
	f.log.Error("firewall coraza inspect failed",
		zap.Error(err),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("ip", sanitizeIP(c.ClientIP())),
		zap.String("request_id", requestID(c)),
	)
	response.Error(c, http.StatusServiceUnavailable, 50310, "服务维护中，请稍后再试")
	c.Abort()
	f.emitEvent(c, "waf_processing_error", http.StatusServiceUnavailable, 50310, firewalldomain.EventDetail{
		Action: firewalldomain.EventActionBlock, Stage: firewalldomain.StagePayload, Transport: transport,
	}, nil, "", "")
}

func (f *Firewall) blockWAF(c *gin.Context, res *fw.Result, transport string) {
	f.wafBlocked.Add(1)
	status := res.Status
	if status < 400 || status > 599 {
		status = http.StatusForbidden
	}
	f.log.Warn("firewall coraza blocked request",
		zap.Int("rule_id", res.RuleID),
		zap.Int("score", res.InboundScore),
		zap.String("transport", transport),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("ip", sanitizeIP(c.ClientIP())),
		zap.String("request_id", requestID(c)),
		zap.String("rules", summarizeMatches(res.Matches)),
	)
	response.Error(c, status, 40396, "当前请求已被安全策略拦截")
	c.Abort()
	ruleID := primaryRule(res)
	f.emitEvent(c, "waf_blocked", status, 40396, firewalldomain.EventDetail{
		Action: firewalldomain.EventActionBlock, Stage: firewalldomain.StagePayload, Transport: transport,
		AnomalyScore: res.InboundScore, Matches: toDomainMatches(res.Matches),
	}, &ruleID, res.Action, primaryData(res))
}

// recordObserved 观察模式的「本该拦截」与出站命中：只记事件、不改响应，按来源 + 规则一分钟去重。
func (f *Firewall) recordObserved(c *gin.Context, res *fw.Result, transport, stage, reason string) {
	f.observedCount.Add(1)
	ruleID := primaryRule(res)
	key := sanitizeIP(c.ClientIP()) + "|" + reason + "|" + strconv.Itoa(ruleID) + "|" + c.Request.URL.Path
	if _, seen := f.observed.Get(key); seen {
		return
	}
	f.observed.Add(key, struct{}{})
	score := res.InboundScore
	if stage == firewalldomain.StageResponse {
		score = res.OutboundScore
	}
	f.emitEvent(c, reason, 0, 0, firewalldomain.EventDetail{
		Action: firewalldomain.EventActionObserve, Stage: stage, Transport: transport,
		AnomalyScore: score, Matches: toDomainMatches(res.Matches),
	}, &ruleID, res.Action, primaryData(res))
}

// emitObserved 自定义规则的 log 动作（同样去重）。
func (f *Firewall) emitObserved(c *gin.Context, ruleID int64, reason string, detail firewalldomain.EventDetail) {
	key := sanitizeIP(c.ClientIP()) + "|" + reason + "|" + strconv.FormatInt(ruleID, 10)
	if _, seen := f.observed.Get(key); seen {
		return
	}
	f.observed.Add(key, struct{}{})
	f.emitEvent(c, reason, 0, 0, detail, nil, "", "")
}

// primaryRule 事件的「主规则」：真正命中攻击特征的那条，而不是 949110 这种汇总评估。
func primaryRule(res *fw.Result) int {
	best, bestRank := 0, -1
	for _, m := range res.Matches {
		if m.RuleID == 949110 || m.RuleID == 959100 || m.RuleID == 980170 {
			continue
		}
		rank := map[string]int{"critical": 4, "error": 3, "warning": 2, "notice": 1}[m.Severity]
		if rank > bestRank {
			best, bestRank = m.RuleID, rank
		}
	}
	if best == 0 {
		return res.RuleID
	}
	return best
}

func primaryData(res *fw.Result) string {
	id := primaryRule(res)
	for _, m := range res.Matches {
		if m.RuleID == id {
			return m.Data
		}
	}
	return ""
}

func summarizeMatches(matches []fw.Match) string {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, strconv.Itoa(m.RuleID))
	}
	return strings.Join(parts, ",")
}

func toDomainMatches(matches []fw.Match) []firewalldomain.MatchedRule {
	out := make([]firewalldomain.MatchedRule, 0, len(matches))
	for _, m := range matches {
		out = append(out, firewalldomain.MatchedRule{
			ID: m.RuleID, Message: m.Message, Data: m.Data, Severity: m.Severity,
			Phase: m.Phase, Group: m.Group, Tags: m.Tags, Targets: m.Targets,
		})
	}
	return out
}

// wafResponseTee 出站检测：响应照常流式写出，同时截一段（上限内）副本给 WAF。不缓冲、不改写。
type wafResponseTee struct {
	gin.ResponseWriter
	captured bytes.Buffer
	limit    int
}

func newWAFResponseTee(w gin.ResponseWriter, limit int) *wafResponseTee {
	return &wafResponseTee{ResponseWriter: w, limit: limit}
}

func (w *wafResponseTee) capture(p []byte) {
	if room := w.limit - w.captured.Len(); room > 0 {
		ct := strings.ToLower(w.Header().Get("Content-Type"))
		if strings.Contains(ct, "event-stream") {
			return
		}
		if len(p) > room {
			p = p[:room]
		}
		w.captured.Write(p)
	}
}

func (w *wafResponseTee) Write(p []byte) (int, error) {
	w.capture(p)
	return w.ResponseWriter.Write(p)
}

func (w *wafResponseTee) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

// ──────────────────────────────────────
// 配置与热更新
// ──────────────────────────────────────

func (f *Firewall) ValidateConfig(cfg config.FirewallConfig) error {
	f.policyMu.Lock()
	rules, exclusions := f.rules, f.exclusions
	f.policyMu.Unlock()
	state, err := f.buildState(cfg, rules, exclusions)
	if err == nil && state.waf != nil {
		_ = state.waf.Close()
	}
	return err
}

// ValidatePolicy 用当前配置试编译一组规则与例外（保存前调用；例外拼错会让整个 WAF 编译失败）。
func (f *Firewall) ValidatePolicy(rules []fw.CustomRule, exclusions []fw.Exclusion) error {
	for _, e := range exclusions {
		if err := fw.ValidateExclusion(e); err != nil {
			return fmt.Errorf("WAF 例外 %s：%w", e.Name, err)
		}
	}
	state, err := f.buildState(f.CurrentConfig(), rules, exclusions)
	if err != nil {
		return err
	}
	if len(state.ruleErrors) > 0 {
		return errors.New(strings.Join(state.ruleErrors, "；"))
	}
	if state.waf != nil {
		_ = state.waf.Close()
	}
	return nil
}

// TestWAF 用当前配置 + 给定例外编译一份临时 WAF（测试台用，不影响线上）。调用方负责 Close。
func (f *Firewall) TestWAF(exclusions []fw.Exclusion) (*fw.WAF, error) {
	return fw.CompileWAF(firewallOptions(f.CurrentConfig(), exclusions))
}

// CurrentWAF 线上正在用的 WAF（可能为 nil）。只读使用，不要 Close。
func (f *Firewall) CurrentWAF() *fw.WAF {
	if state := f.current(); state != nil {
		return state.waf
	}
	return nil
}

func (f *Firewall) Reload(cfg config.FirewallConfig) error {
	f.policyMu.Lock()
	rules, exclusions := f.rules, f.exclusions
	f.policyMu.Unlock()
	return f.install(cfg, rules, exclusions)
}

// SetPolicy 换上数据库里的自定义规则与 WAF 例外（启动加载、控制台保存、集群广播都走这里）。
func (f *Firewall) SetPolicy(rules []fw.CustomRule, exclusions []fw.Exclusion) error {
	f.policyMu.Lock()
	f.rules = rules
	f.exclusions = exclusions
	f.policyMu.Unlock()
	return f.install(f.CurrentConfig(), rules, exclusions)
}

func (f *Firewall) install(cfg config.FirewallConfig, rules []fw.CustomRule, exclusions []fw.Exclusion) error {
	f.reloadMu.Lock()
	defer f.reloadMu.Unlock()
	state, err := f.buildState(cfg, rules, exclusions)
	if err != nil {
		return err
	}
	old := f.state.Swap(state)
	version := f.version.Add(1)
	now := time.Now().UTC()
	f.reloadedAt.Store(&now)
	if old != nil && old.waf != nil && old.waf != state.waf {
		// 旧实例上可能还有正在跑的事务：晚一点再释放。
		retired := old.waf
		time.AfterFunc(30*time.Second, func() { _ = retired.Close() })
	}
	for _, msg := range state.ruleErrors {
		f.log.Error("firewall custom rule skipped", zap.String("error", msg))
	}
	f.log.Info("firewall settings reloaded",
		zap.Uint64("version", version),
		zap.Bool("enabled", state.cfg.Enabled),
		zap.Bool("coraza_enabled", state.waf != nil),
		zap.Bool("detection_only", state.cfg.CorazaDetectionOnly),
		zap.Int("anomaly_threshold", state.cfg.CorazaAnomalyThreshold),
		zap.Int("custom_rules", len(state.rules.Rules())),
		zap.Int("waf_exclusions", state.exclusions),
	)
	return nil
}

func (f *Firewall) Snapshot() FirewallSnapshot {
	state := f.current()
	if state == nil {
		return FirewallSnapshot{}
	}
	snap := FirewallSnapshot{
		Config:         state.cfg,
		ReloadVersion:  f.version.Load(),
		RuntimeEnabled: state.cfg.Enabled,
		WAFActive:      state.cfg.Enabled && state.waf != nil,
		CustomRules:    len(state.rules.Rules()),
		RuleErrors:     state.ruleErrors,
		Exclusions:     state.exclusions,
		Counters:       f.Counters(),
	}
	if t := f.reloadedAt.Load(); t != nil {
		snap.ReloadedAt = *t
	}
	if state.waf != nil {
		snap.Catalog = state.waf.Catalog()
		snap.Directives = state.waf.Directives()
	}
	return snap
}

// Counters 本副本计数。
func (f *Firewall) Counters() FirewallCounters {
	inspected := f.inspected.Load()
	avg := int64(0)
	if inspected > 0 {
		avg = f.wafNanos.Load() / inspected / 1000
	}
	return FirewallCounters{
		Inspected: inspected, EdgeBlocked: f.edgeBlocked.Load(), WAFBlocked: f.wafBlocked.Load(),
		Observed: f.observedCount.Load(), Decrypted: f.decrypted.Load(), WAFErrors: f.wafErrors.Load(),
		AvgWAFMicros: avg, StartedAt: f.startedAt,
	}
}

// TakeRuleHits 取走自上次以来各自定义规则的命中数（FirewallPolicyService 定时累加进库）。
func (f *Firewall) TakeRuleHits() map[int64]int64 {
	state := f.current()
	if state == nil {
		return nil
	}
	out := map[int64]int64{}
	for _, rule := range state.rules.Rules() {
		if n := rule.TakeHits(); n > 0 {
			out[rule.ID] = n
		}
	}
	return out
}

func (f *Firewall) CurrentConfig() config.FirewallConfig {
	if state := f.current(); state != nil {
		return state.cfg
	}
	return config.FirewallConfig{}
}

func (f *Firewall) ReloadMeta() (uint64, time.Time) {
	snapshot := f.Snapshot()
	return snapshot.ReloadVersion, snapshot.ReloadedAt
}

// firewallOptions 配置 → WAF 编译参数。
func firewallOptions(cfg config.FirewallConfig, exclusions []fw.Exclusion) fw.Options {
	cfg = config.NormalizeFirewallConfig(cfg)
	return fw.Options{
		DetectionOnly:      cfg.CorazaDetectionOnly,
		Paranoia:           cfg.CorazaParanoia,
		DetectionParanoia:  cfg.CorazaDetectionParanoia,
		InboundThreshold:   cfg.CorazaAnomalyThreshold,
		OutboundThreshold:  cfg.CorazaOutboundThreshold,
		ResponseInspection: cfg.CorazaResponseInspection,
		ResponseBodyLimit:  cfg.CorazaResponseBodyLimit,
		RequestBodyLimit:   cfg.RequestBodyLimit,
		RequestBodyMemory:  cfg.RequestBodyMemory,
		ArgumentsLimit:     cfg.CorazaArgumentsLimit,
		DisabledGroups:     cfg.CorazaDisabledGroups,
		RemovedRules:       cfg.CorazaRemovedRules,
		InspectAdminBody:   cfg.CorazaInspectAdminBody,
		BodyExemptPrefixes: cfg.CorazaBodyExemptPrefixes,
		SensitiveArgs:      cfg.CorazaSensitiveArgs,
		RichTextArgs:       cfg.CorazaRichTextArgs,
		ExtraContentTypes:  cfg.CorazaExtraContentTypes,
		Exclusions:         exclusions,
	}
}

func (f *Firewall) buildState(cfg config.FirewallConfig, rules []fw.CustomRule, exclusions []fw.Exclusion) (*firewallState, error) {
	cfg = config.NormalizeFirewallConfig(cfg)
	state := &firewallState{
		cfg:               cfg,
		blockedUserAgents: normalizeEntries(cfg.BlockedUserAgents),
		blockedPathPrefix: normalizeEntries(cfg.BlockedPathPrefix),
		blockedFragments:  normalizeEntries(cfg.BlockedSignatures),
		bodyExempt:        cfg.CorazaBodyExemptPrefixes,
		exclusions:        len(exclusions),
	}
	var err error
	if state.allowed, err = fw.ParseIPSet(cfg.AllowedCIDRs); err != nil {
		return nil, err
	}
	if state.blocked, err = fw.ParseIPSet(cfg.BlockedCIDRs); err != nil {
		return nil, err
	}
	if state.trusted, err = fw.ParseIPSet(cfg.TrustedCIDRs); err != nil {
		return nil, err
	}
	ruleSet, ruleErrs := fw.NewRuleSet(rules)
	state.rules = ruleSet
	for _, e := range ruleErrs {
		state.ruleErrors = append(state.ruleErrors, e.Error())
	}
	if !cfg.Enabled {
		return state, nil
	}
	if f.redisClient == nil {
		return nil, net.InvalidAddrError("firewall requires redis client")
	}
	if cfg.CorazaEnabled {
		state.waf, err = fw.CompileWAF(firewallOptions(cfg, exclusions))
		if err != nil {
			return nil, err
		}
	}
	globalRate, err := limiter.NewRateFromFormatted(cfg.GlobalRate)
	if err != nil {
		return nil, fmt.Errorf("全局限流格式无效: %w", err)
	}
	authRate, err := limiter.NewRateFromFormatted(cfg.AuthRate)
	if err != nil {
		return nil, fmt.Errorf("认证限流格式无效: %w", err)
	}
	adminRate, err := limiter.NewRateFromFormatted(cfg.AdminRate)
	if err != nil {
		return nil, fmt.Errorf("管理端限流格式无效: %w", err)
	}
	prefix := strings.TrimSpace(f.keyPrefix)
	if state.globalLimiter, err = newLimiter(f.redisClient, prefix+":fw:global", globalRate); err != nil {
		return nil, err
	}
	if state.authLimiter, err = newLimiter(f.redisClient, prefix+":fw:auth", authRate); err != nil {
		return nil, err
	}
	if state.adminLimiter, err = newLimiter(f.redisClient, prefix+":fw:admin", adminRate); err != nil {
		return nil, err
	}
	return state, nil
}

func newLimiter(redisClient *redislib.Client, prefix string, rate limiter.Rate) (*limiter.Limiter, error) {
	store, err := redisstore.NewStoreWithOptions(redisClient, limiter.StoreOptions{Prefix: prefix})
	if err != nil {
		return nil, err
	}
	return limiter.New(store, rate), nil
}

func (f *Firewall) rateLimit(state *firewallState, c *gin.Context, ip string) (bool, int64) {
	requestPath := strings.ToLower(strings.TrimSpace(c.Request.URL.Path))
	type check struct {
		limiter *limiter.Limiter
		key     string
	}
	checks := []check{{limiter: state.globalLimiter, key: "global:" + ip}}
	if strings.HasPrefix(requestPath, "/api/auth/") || strings.HasPrefix(requestPath, "/api/email/send-") ||
		(strings.HasPrefix(requestPath, gatewayPathPrefix) && strings.Contains(requestPath, "/auth/")) {
		checks = append(checks, check{limiter: state.authLimiter, key: "auth:" + ip})
	}
	if strings.HasPrefix(requestPath, "/api/admin/") ||
		strings.HasPrefix(requestPath, "/api/app/password-policy") ||
		strings.HasPrefix(requestPath, "/api/app/points") ||
		strings.HasPrefix(requestPath, "/api/app/workflow") {
		checks = append(checks, check{limiter: state.adminLimiter, key: "admin:" + ip})
	}
	for _, item := range checks {
		if item.limiter == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		result, err := item.limiter.Increment(ctx, item.key, 1)
		cancel()
		if err != nil {
			f.log.Warn("firewall limiter failed", zap.Error(err), zap.String("key", item.key))
			continue
		}
		c.Header("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
		c.Header("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(result.Reset, 10))
		if result.Reached {
			return true, result.Reset
		}
	}
	return false, 0
}

func blockByUserAgent(state *firewallState, userAgent string) (bool, string) {
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	if userAgent == "" {
		return false, ""
	}
	for _, fragment := range state.blockedUserAgents {
		if strings.Contains(userAgent, fragment) {
			return true, "blocked_user_agent"
		}
	}
	return false, ""
}

func blockByPathOrQuery(state *firewallState, path, rawQuery string) (bool, string) {
	target := strings.ToLower(strings.TrimSpace(path))
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	for _, prefix := range state.blockedPathPrefix {
		if strings.HasPrefix(target, prefix) {
			return true, "blocked_path"
		}
	}
	combined := target
	if query != "" {
		combined += "?" + query
	}
	for _, fragment := range state.blockedFragments {
		if strings.Contains(combined, fragment) {
			return true, "blocked_signature"
		}
	}
	return false, ""
}

func (f *Firewall) block(c *gin.Context, httpStatus int, code int, reason string, message string) {
	f.edgeBlocked.Add(1)
	f.log.Warn("firewall blocked request",
		zap.String("reason", reason),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("ip", sanitizeIP(c.ClientIP())),
		zap.String("request_id", requestID(c)),
	)
	response.Error(c, httpStatus, code, message)
	c.Abort()
	f.emitBlockEvent(c, reason, httpStatus, code, nil, "", "")
}

// emitBlockEvent 入口段拦截事件（封禁模式等旧调用点沿用这个签名）。
func (f *Firewall) emitBlockEvent(c *gin.Context, reason string, httpStatus int, responseCode int, wafRuleID *int, wafAction string, wafData string) {
	if v, ok := c.Get(ctxEventOverride); ok {
		if detail, ok := v.(*firewalldomain.EventDetail); ok && detail != nil {
			f.emitEvent(c, "custom_rule", httpStatus, responseCode, *detail, nil, reason, "")
			return
		}
	}
	f.emitEvent(c, reason, httpStatus, responseCode, firewalldomain.EventDetail{
		Action: firewalldomain.EventActionBlock, Stage: firewalldomain.StageEdge,
	}, wafRuleID, wafAction, wafData)
}

// emitEvent 异步发射防火墙事件到 NATS（Worker 落库、补归属地、评估自动封禁）。
func (f *Firewall) emitEvent(c *gin.Context, reason string, httpStatus int, responseCode int, detail firewalldomain.EventDetail, wafRuleID *int, wafAction string, wafData string) {
	if f.publisher == nil {
		return
	}
	headers := make(map[string]string)
	for _, key := range []string{"Referer", "Origin", "Accept-Language", "Content-Type", "X-Forwarded-For", gwHeaderProtocol, appEncryptionHeaderEnabled, "X-App-Version", "X-Device-Platform"} {
		if v := c.GetHeader(key); v != "" {
			headers[key] = truncateText(v, 256)
		}
	}
	if detail.Transport == "" {
		detail.Transport = firewalldomain.TransportPlain
	}
	if detail.AppKey == "" {
		detail.AppKey = c.GetString("aegis.app_key")
		if detail.AppKey == "" {
			detail.AppKey = appKeyFromGatewayPath(c.Request.URL.Path)
		}
	}
	query := c.Request.URL.RawQuery
	if detail.Stage == firewalldomain.StageEdge && hasEncryptedQuery(c.Request) {
		query = "_payload=<密文>"
	}
	evt := firewalldomain.BlockEvent{
		RequestID:    requestID(c),
		IP:           sanitizeIP(c.ClientIP()),
		Method:       c.Request.Method,
		Path:         c.Request.URL.Path,
		QueryString:  truncateText(query, 2048),
		UserAgent:    truncateText(c.GetHeader("User-Agent"), 512),
		Headers:      headers,
		Reason:       reason,
		HTTPStatus:   httpStatus,
		ResponseCode: responseCode,
		WAFRuleID:    wafRuleID,
		WAFAction:    wafAction,
		WAFData:      truncateText(wafData, 512),
		Severity:     firewalldomain.ReasonSeverity(reason),
		BlockedAt:    time.Now().UTC(),
		EventDetail:  detail,
	}
	if err := f.publisher.PublishJSON(context.Background(), event.SubjectFirewallBlocked, evt); err != nil {
		f.log.Warn("firewall emit block event failed", zap.Error(err))
	}
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

func normalizeEntries(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func blockedMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodConnect, http.MethodTrace, "TRACK", "DEBUG":
		return true
	default:
		return false
	}
}

func sanitizeIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		return strings.TrimSpace(host)
	}
	return raw
}

func requestID(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	id, _ := value.(string)
	return id
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
