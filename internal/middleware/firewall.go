package middleware

import (
	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"
	"aegis/internal/event"
	"aegis/pkg/response"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	coreruleset "github.com/corazawaf/coraza-coreruleset/v4"
	"github.com/corazawaf/coraza/v3"
	corazatypes "github.com/corazawaf/coraza/v3/types"
	"github.com/gin-gonic/gin"
	redislib "github.com/redis/go-redis/v9"
	"github.com/ulule/limiter/v3"
	redisstore "github.com/ulule/limiter/v3/drivers/store/redis"
	"go.uber.org/zap"
)

type firewallState struct {
	cfg               config.FirewallConfig
	waf               coraza.WAF
	globalLimiter     *limiter.Limiter
	authLimiter       *limiter.Limiter
	adminLimiter      *limiter.Limiter
	allowedCIDRs      []netip.Prefix
	blockedCIDRs      []netip.Prefix
	blockedUserAgents []string
	blockedPathPrefix []string
	blockedFragments  []string
}

type FirewallSnapshot struct {
	Config         config.FirewallConfig
	ReloadVersion  uint64
	ReloadedAt     time.Time
	RuntimeEnabled bool
}

// BanChecker 动态 IP 封禁检查接口
type BanChecker interface {
	IsBanned(ctx context.Context, ip string) (bool, error)
}

type Firewall struct {
	log         *zap.Logger
	redisClient *redislib.Client
	keyPrefix   string
	publisher   *event.Publisher
	banChecker  BanChecker
	mu          sync.RWMutex
	state       firewallState
	reloadedAt  time.Time
	version     uint64
}

func NewFirewall(cfg config.FirewallConfig, log *zap.Logger, redisClient *redislib.Client, keyPrefix string, publisher *event.Publisher, banChecker BanChecker) (*Firewall, error) {
	if log == nil {
		log = zap.NewNop()
	}

	firewall := &Firewall{
		log:         log,
		redisClient: redisClient,
		keyPrefix:   keyPrefix,
		publisher:   publisher,
		banChecker:  banChecker,
	}
	if err := firewall.Reload(cfg); err != nil {
		return nil, err
	}
	return firewall, nil
}

func (f *Firewall) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if f == nil {
			c.Next()
			return
		}
		state := f.snapshotState()
		if !state.cfg.Enabled {
			c.Next()
			return
		}

		path := strings.TrimSpace(c.Request.URL.Path)
		if path == "/healthz" || path == "/readyz" || path == "/api/ws" {
			c.Next()
			return
		}

		ip := sanitizeIP(c.ClientIP())
		if ip == "" {
			f.block(c, http.StatusForbidden, 40390, "invalid_ip", "当前请求已被安全策略拦截")
			return
		}

		if blocked, reason := blockByCIDR(state, ip); blocked {
			f.block(c, http.StatusForbidden, 40391, reason, "当前请求已被安全策略拦截")
			return
		}
		if f.banChecker != nil {
			if banned, _ := f.banChecker.IsBanned(c.Request.Context(), ip); banned {
				f.block(c, http.StatusForbidden, 40397, "banned_ip", "当前请求已被安全策略拦截")
				return
			}
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
		if state.cfg.MaxQueryLength > 0 && len(rawQuery) > state.cfg.MaxQueryLength {
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
		if limited, resetAt := f.rateLimit(state, c, ip); limited {
			retryAfter := maxInt64(1, resetAt-time.Now().Unix())
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			f.block(c, http.StatusTooManyRequests, 42900, "rate_limited", "请求过于频繁，请稍后再试")
			return
		}
		if state.waf != nil {
			if interrupted, err := f.inspectRequest(state, c, ip); err != nil {
				f.log.Error("firewall coraza inspect failed",
					zap.Error(err),
					zap.String("method", c.Request.Method),
					zap.String("path", c.Request.URL.Path),
					zap.String("ip", ip),
					zap.String("request_id", requestID(c)),
				)
				f.block(c, http.StatusServiceUnavailable, 50310, "waf_processing_error", "服务维护中，请稍后再试")
				return
			} else if interrupted != nil {
				f.blockCoraza(c, interrupted)
				return
			}
		}

		c.Next()
	}
}

func (f *Firewall) ValidateConfig(cfg config.FirewallConfig) error {
	_, err := f.buildState(cfg)
	return err
}

func (f *Firewall) Reload(cfg config.FirewallConfig) error {
	state, err := f.buildState(cfg)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.state = state
	f.version++
	f.reloadedAt = time.Now().UTC()
	version := f.version
	reloadedAt := f.reloadedAt
	enabled := state.cfg.Enabled
	corazaEnabled := state.cfg.CorazaEnabled && state.waf != nil
	f.mu.Unlock()

	f.log.Info("firewall settings reloaded",
		zap.Uint64("version", version),
		zap.Time("reloaded_at", reloadedAt),
		zap.Bool("enabled", enabled),
		zap.Bool("coraza_enabled", corazaEnabled),
	)
	return nil
}

func (f *Firewall) Snapshot() FirewallSnapshot {
	if f == nil {
		return FirewallSnapshot{}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return FirewallSnapshot{
		Config:         f.state.cfg,
		ReloadVersion:  f.version,
		ReloadedAt:     f.reloadedAt,
		RuntimeEnabled: f.state.cfg.Enabled,
	}
}

func (f *Firewall) CurrentConfig() config.FirewallConfig {
	return f.Snapshot().Config
}

func (f *Firewall) ReloadMeta() (uint64, time.Time) {
	snapshot := f.Snapshot()
	return snapshot.ReloadVersion, snapshot.ReloadedAt
}

func (f *Firewall) snapshotState() firewallState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.state
}

func (f *Firewall) buildState(cfg config.FirewallConfig) (firewallState, error) {
	cfg = config.NormalizeFirewallConfig(cfg)
	state := firewallState{
		cfg:               cfg,
		blockedUserAgents: normalizeEntries(cfg.BlockedUserAgents),
		blockedPathPrefix: normalizeEntries(cfg.BlockedPathPrefix),
		blockedFragments: []string{
			// 路径遍历
			"../",
			"..\\",
			"%2e%2e",
			// 敏感路径探测
			"/.git/",
			"/.env",
			"/vendor/phpunit",
			"/etc/passwd",
			"/proc/self/environ",
			// XSS
			"<script",
			"%3cscript",
			"javascript:",
			"onerror=",
			"onload=",
			// SQL 注入
			"union+select",
			"union%20select",
			"sleep(",
			"benchmark(",
			"load_file(",
			"information_schema",
			// 命令注入
			";cat+",
			"|cat+",
			"$(curl",
			"$(wget",
			"`curl",
			"`wget",
		},
	}

	var err error
	state.allowedCIDRs, err = parseCIDRs(cfg.AllowedCIDRs)
	if err != nil {
		return firewallState{}, err
	}
	state.blockedCIDRs, err = parseCIDRs(cfg.BlockedCIDRs)
	if err != nil {
		return firewallState{}, err
	}
	if !cfg.Enabled {
		return state, nil
	}
	if f.redisClient == nil {
		return firewallState{}, net.InvalidAddrError("firewall requires redis client")
	}
	if cfg.CorazaEnabled {
		state.waf, err = newCorazaWAF(cfg, f.log)
		if err != nil {
			return firewallState{}, fmt.Errorf("init coraza waf: %w", err)
		}
	}
	globalRate, err := limiter.NewRateFromFormatted(cfg.GlobalRate)
	if err != nil {
		return firewallState{}, err
	}
	authRate, err := limiter.NewRateFromFormatted(cfg.AuthRate)
	if err != nil {
		return firewallState{}, err
	}
	adminRate, err := limiter.NewRateFromFormatted(cfg.AdminRate)
	if err != nil {
		return firewallState{}, err
	}

	prefix := strings.TrimSpace(f.keyPrefix)
	state.globalLimiter, err = newLimiter(f.redisClient, prefix+":fw:global", globalRate)
	if err != nil {
		return firewallState{}, err
	}
	state.authLimiter, err = newLimiter(f.redisClient, prefix+":fw:auth", authRate)
	if err != nil {
		return firewallState{}, err
	}
	state.adminLimiter, err = newLimiter(f.redisClient, prefix+":fw:admin", adminRate)
	if err != nil {
		return firewallState{}, err
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

func newCorazaWAF(cfg config.FirewallConfig, log *zap.Logger) (coraza.WAF, error) {
	wafConfig := coraza.NewWAFConfig().
		WithRootFS(newSlashFS(coreruleset.FS)).
		WithDirectives(buildCorazaDirectives(cfg)).
		WithRequestBodyAccess().
		WithRequestBodyLimit(cfg.RequestBodyLimit).
		WithRequestBodyInMemoryLimit(cfg.RequestBodyMemory).
		WithErrorCallback(func(rule corazatypes.MatchedRule) {
			if log == nil {
				return
			}
			meta := rule.Rule()
			log.Warn("firewall coraza rule matched",
				zap.Int("rule_id", meta.ID()),
				zap.String("file", meta.File()),
				zap.Int("line", meta.Line()),
				zap.String("message", rule.Message()),
				zap.String("data", rule.Data()),
				zap.String("uri", rule.URI()),
				zap.Bool("disruptive", rule.Disruptive()),
				zap.String("client_ip", rule.ClientIPAddress()),
				zap.String("tx_id", rule.TransactionID()),
			)
		})

	return coraza.NewWAF(wafConfig)
}

func buildCorazaDirectives(cfg config.FirewallConfig) string {
	tempDir := strings.ReplaceAll(os.TempDir(), "\\", "/")
	paranoia := cfg.CorazaParanoia

	return fmt.Sprintf(`
Include @coraza.conf-recommended
Include @crs-setup.conf.example
SecRuleEngine On
SecRequestBodyAccess On
SecResponseBodyAccess Off
SecDataDir "%s"
SecAction "id:1000001,phase:1,pass,nolog,setvar:tx.blocking_paranoia_level=%d,setvar:tx.detection_paranoia_level=%d,setvar:tx.inbound_anomaly_score_threshold=25,setvar:tx.outbound_anomaly_score_threshold=10"
SecRule REQUEST_HEADERS:Content-Type "@rx ^application/(?:json|[a-z0-9.+-]+\\+json)" "id:1000003,phase:1,pass,nolog,ctl:ruleRemoveByTag=attack-sqli,ctl:ruleRemoveByTag=attack-xss,ctl:ruleRemoveByTag=attack-rce,ctl:ruleRemoveByTag=attack-protocol"
Include @owasp_crs/*.conf
`, tempDir, paranoia, paranoia)
}

func (f *Firewall) inspectRequest(state firewallState, c *gin.Context, clientIP string) (*corazatypes.Interruption, error) {
	tx := state.waf.NewTransaction()
	defer func() {
		tx.ProcessLogging()
		if err := tx.Close(); err != nil {
			f.log.Warn("firewall coraza close failed", zap.Error(err), zap.String("request_id", requestID(c)))
		}
	}()

	if tx.IsRuleEngineOff() {
		return nil, nil
	}

	tx.ProcessConnection(clientIP, 0, "", 0)
	tx.ProcessURI(c.Request.URL.String(), c.Request.Method, c.Request.Proto)

	for key, values := range c.Request.Header {
		for _, value := range values {
			tx.AddRequestHeader(key, value)
		}
	}
	if c.Request.Host != "" {
		tx.AddRequestHeader("Host", c.Request.Host)
		tx.SetServerName(c.Request.Host)
	}
	for _, value := range c.Request.TransferEncoding {
		tx.AddRequestHeader("Transfer-Encoding", value)
	}

	if interrupted := tx.ProcessRequestHeaders(); interrupted != nil {
		return interrupted, nil
	}

	if tx.IsRequestBodyAccessible() && c.Request.Body != nil && c.Request.Body != http.NoBody {
		body, err := f.snapshotRequestBody(c.Request)
		if err != nil {
			return nil, fmt.Errorf("snapshot request body: %w", err)
		}
		interrupted, _, err := tx.ReadRequestBodyFrom(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		if interrupted != nil {
			return interrupted, nil
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
	}

	interrupted, err := tx.ProcessRequestBody()
	if err != nil {
		return nil, fmt.Errorf("process request body: %w", err)
	}
	return interrupted, nil
}

func (f *Firewall) snapshotRequestBody(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}

	limit := int64(0)
	if f != nil {
		snapshot := f.Snapshot()
		if snapshot.Config.RequestBodyLimit > 0 {
			limit = int64(snapshot.Config.RequestBodyLimit)
		}
	}

	var (
		body []byte
		err  error
	)
	if limit > 0 {
		body, err = io.ReadAll(io.LimitReader(req.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > limit {
			return nil, fmt.Errorf("request body exceeds configured limit")
		}
	} else {
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func (f *Firewall) rateLimit(state firewallState, c *gin.Context, ip string) (bool, int64) {
	requestPath := strings.ToLower(strings.TrimSpace(c.Request.URL.Path))
	checks := []struct {
		limiter *limiter.Limiter
		key     string
	}{
		{limiter: state.globalLimiter, key: "global:" + ip},
	}

	if strings.HasPrefix(requestPath, "/api/auth/") || strings.HasPrefix(requestPath, "/api/email/send-") {
		checks = append(checks, struct {
			limiter *limiter.Limiter
			key     string
		}{limiter: state.authLimiter, key: "auth:" + ip})
	}
	if strings.HasPrefix(requestPath, "/api/admin/") ||
		strings.HasPrefix(requestPath, "/api/app/password-policy") ||
		strings.HasPrefix(requestPath, "/api/app/points") ||
		strings.HasPrefix(requestPath, "/api/app/workflow") {
		checks = append(checks, struct {
			limiter *limiter.Limiter
			key     string
		}{limiter: state.adminLimiter, key: "admin:" + ip})
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

func blockByCIDR(state firewallState, ip string) (bool, string) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return true, "invalid_ip"
	}
	if len(state.allowedCIDRs) > 0 {
		allowed := false
		for _, prefix := range state.allowedCIDRs {
			if prefix.Contains(addr) {
				allowed = true
				break
			}
		}
		if !allowed {
			return true, "not_in_allowlist"
		}
	}
	for _, prefix := range state.blockedCIDRs {
		if prefix.Contains(addr) {
			return true, "blocked_cidr"
		}
	}
	return false, ""
}

func blockByUserAgent(state firewallState, userAgent string) (bool, string) {
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

func blockByPathOrQuery(state firewallState, path, rawQuery string) (bool, string) {
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

func (f *Firewall) blockCoraza(c *gin.Context, interrupted *corazatypes.Interruption) {
	f.log.Warn("firewall coraza blocked request",
		zap.Int("rule_id", interrupted.RuleID),
		zap.String("action", interrupted.Action),
		zap.String("data", interrupted.Data),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("ip", sanitizeIP(c.ClientIP())),
		zap.String("request_id", requestID(c)),
	)
	response.Error(c, http.StatusForbidden, 40396, "当前请求已被安全策略拦截")
	c.Abort()
	ruleID := interrupted.RuleID
	f.emitBlockEvent(c, "waf_blocked", http.StatusForbidden, 40396, &ruleID, interrupted.Action, interrupted.Data)
}

func (f *Firewall) block(c *gin.Context, httpStatus int, code int, reason string, message string) {
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

// emitBlockEvent 异步发射防火墙拦截事件到 NATS
func (f *Firewall) emitBlockEvent(c *gin.Context, reason string, httpStatus int, responseCode int, wafRuleID *int, wafAction string, wafData string) {
	if f.publisher == nil {
		return
	}
	headers := make(map[string]string)
	for _, key := range []string{"Referer", "Origin", "Accept-Language", "Content-Type", "X-Forwarded-For"} {
		if v := c.GetHeader(key); v != "" {
			headers[key] = v
		}
	}
	evt := firewalldomain.BlockEvent{
		RequestID:    requestID(c),
		IP:           sanitizeIP(c.ClientIP()),
		Method:       c.Request.Method,
		Path:         c.Request.URL.Path,
		QueryString:  c.Request.URL.RawQuery,
		UserAgent:    c.GetHeader("User-Agent"),
		Headers:      headers,
		Reason:       reason,
		HTTPStatus:   httpStatus,
		ResponseCode: responseCode,
		WAFRuleID:    wafRuleID,
		WAFAction:    wafAction,
		WAFData:      wafData,
		Severity:     firewalldomain.ReasonSeverity(reason),
		BlockedAt:    time.Now().UTC(),
	}
	if err := f.publisher.PublishJSON(context.Background(), event.SubjectFirewallBlocked, evt); err != nil {
		f.log.Warn("firewall emit block event failed", zap.Error(err))
	}
}

func parseCIDRs(values []string) ([]netip.Prefix, error) {
	items := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "/") {
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return nil, err
			}
			items = append(items, prefix)
			continue
		}
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return nil, err
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		items = append(items, netip.PrefixFrom(addr, bits))
	}
	return items, nil
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

type slashFS struct {
	root fs.FS
}

func newSlashFS(root fs.FS) fs.FS {
	return slashFS{root: root}
}

func (s slashFS) Open(name string) (fs.File, error) {
	return s.root.Open(normalizeFSPath(name))
}

func (s slashFS) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(s.root, normalizeFSPath(name))
}

func (s slashFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return fs.ReadDir(s.root, normalizeFSPath(name))
}

func normalizeFSPath(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" {
		return "."
	}
	cleaned := path.Clean(name)
	cleaned = strings.TrimPrefix(cleaned, "./")
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" {
		return "."
	}
	return cleaned
}
