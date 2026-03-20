package middleware

import (
	"aegis/internal/config"
	"aegis/pkg/response"
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

type Firewall struct {
	cfg               config.FirewallConfig
	log               *zap.Logger
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

func NewFirewall(cfg config.FirewallConfig, log *zap.Logger, redisClient *redislib.Client, keyPrefix string) (*Firewall, error) {
	if log == nil {
		log = zap.NewNop()
	}

	firewall := &Firewall{
		cfg:               cfg,
		log:               log,
		blockedUserAgents: normalizeEntries(cfg.BlockedUserAgents),
		blockedPathPrefix: normalizeEntries(cfg.BlockedPathPrefix),
		blockedFragments: []string{
			"../",
			"%2e%2e",
			"<script",
			"%3cscript",
			"union select",
			"union%20select",
			"${jndi:",
			"sleep(",
			"benchmark(",
			"load_file(",
			"into outfile",
			"/etc/passwd",
		},
	}

	var err error
	firewall.allowedCIDRs, err = parseCIDRs(cfg.AllowedCIDRs)
	if err != nil {
		return nil, err
	}
	firewall.blockedCIDRs, err = parseCIDRs(cfg.BlockedCIDRs)
	if err != nil {
		return nil, err
	}

	if !cfg.Enabled {
		return firewall, nil
	}
	if redisClient == nil {
		return nil, net.InvalidAddrError("firewall requires redis client")
	}

	if cfg.CorazaEnabled {
		firewall.waf, err = newCorazaWAF(cfg, log)
		if err != nil {
			return nil, fmt.Errorf("init coraza waf: %w", err)
		}
	}

	globalRate, err := limiter.NewRateFromFormatted(cfg.GlobalRate)
	if err != nil {
		return nil, err
	}
	authRate, err := limiter.NewRateFromFormatted(cfg.AuthRate)
	if err != nil {
		return nil, err
	}
	adminRate, err := limiter.NewRateFromFormatted(cfg.AdminRate)
	if err != nil {
		return nil, err
	}

	prefix := strings.TrimSpace(keyPrefix)
	firewall.globalLimiter, err = newLimiter(redisClient, prefix+":fw:global", globalRate)
	if err != nil {
		return nil, err
	}
	firewall.authLimiter, err = newLimiter(redisClient, prefix+":fw:auth", authRate)
	if err != nil {
		return nil, err
	}
	firewall.adminLimiter, err = newLimiter(redisClient, prefix+":fw:admin", adminRate)
	if err != nil {
		return nil, err
	}
	return firewall, nil
}

func (f *Firewall) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if f == nil || !f.cfg.Enabled {
			c.Next()
			return
		}

		path := strings.TrimSpace(c.Request.URL.Path)
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}

		ip := sanitizeIP(c.ClientIP())
		if ip == "" {
			f.block(c, http.StatusForbidden, 40390, "invalid_ip", "当前请求已被安全策略拦截")
			return
		}

		if blocked, reason := f.blockByCIDR(ip); blocked {
			f.block(c, http.StatusForbidden, 40391, reason, "当前请求已被安全策略拦截")
			return
		}
		if blockedMethod(c.Request.Method) {
			f.block(c, http.StatusNotImplemented, 50190, "blocked_method", "服务能力暂未开放")
			return
		}
		if f.cfg.MaxPathLength > 0 && len(path) > f.cfg.MaxPathLength {
			f.block(c, http.StatusForbidden, 40392, "path_too_long", "当前请求已被安全策略拦截")
			return
		}
		rawQuery := c.Request.URL.RawQuery
		if f.cfg.MaxQueryLength > 0 && len(rawQuery) > f.cfg.MaxQueryLength {
			f.block(c, http.StatusForbidden, 40393, "query_too_long", "当前请求已被安全策略拦截")
			return
		}
		if blocked, reason := f.blockByUserAgent(c.GetHeader("User-Agent")); blocked {
			f.block(c, http.StatusForbidden, 40394, reason, "当前请求已被安全策略拦截")
			return
		}
		if blocked, reason := f.blockByPathOrQuery(path, rawQuery); blocked {
			f.block(c, http.StatusForbidden, 40395, reason, "当前请求已被安全策略拦截")
			return
		}
		if limited, resetAt := f.rateLimit(c, ip); limited {
			retryAfter := maxInt64(1, resetAt-time.Now().Unix())
			c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
			f.block(c, http.StatusTooManyRequests, 42900, "rate_limited", "请求过于频繁，请稍后再试")
			return
		}
		if f.waf != nil {
			if interrupted, err := f.inspectRequest(c, ip); err != nil {
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

func newLimiter(redisClient *redislib.Client, prefix string, rate limiter.Rate) (*limiter.Limiter, error) {
	store, err := redisstore.NewStoreWithOptions(redisClient, limiter.StoreOptions{Prefix: prefix})
	if err != nil {
		return nil, err
	}
	return limiter.New(store, rate), nil
}

func newCorazaWAF(cfg config.FirewallConfig, log *zap.Logger) (coraza.WAF, error) {
	tempDir := strings.ReplaceAll(os.TempDir(), "\\", "/")
	paranoia := cfg.CorazaParanoia

	directives := fmt.Sprintf(`
Include @coraza.conf-recommended
Include @crs-setup.conf.example
SecRuleEngine On
SecRequestBodyAccess On
SecResponseBodyAccess Off
SecDataDir "%s"
SecAction "id:1000001,phase:1,pass,nolog,setvar:tx.blocking_paranoia_level=%d,setvar:tx.detection_paranoia_level=%d,setvar:tx.inbound_anomaly_score_threshold=5,setvar:tx.outbound_anomaly_score_threshold=4"
Include @owasp_crs/*.conf
`, tempDir, paranoia, paranoia)

	wafConfig := coraza.NewWAFConfig().
		WithRootFS(newSlashFS(coreruleset.FS)).
		WithDirectives(directives).
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

func (f *Firewall) inspectRequest(c *gin.Context, clientIP string) (*corazatypes.Interruption, error) {
	tx := f.waf.NewTransaction()
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
		interrupted, _, err := tx.ReadRequestBodyFrom(c.Request.Body)
		if err != nil {
			return nil, fmt.Errorf("read request body: %w", err)
		}
		if interrupted != nil {
			return interrupted, nil
		}
		reader, err := tx.RequestBodyReader()
		if err != nil {
			return nil, fmt.Errorf("restore request body: %w", err)
		}
		c.Request.Body = io.NopCloser(io.MultiReader(reader, c.Request.Body))
	}

	interrupted, err := tx.ProcessRequestBody()
	if err != nil {
		return nil, fmt.Errorf("process request body: %w", err)
	}
	return interrupted, nil
}

func (f *Firewall) rateLimit(c *gin.Context, ip string) (bool, int64) {
	requestPath := strings.ToLower(strings.TrimSpace(c.Request.URL.Path))
	checks := []struct {
		limiter *limiter.Limiter
		key     string
	}{
		{limiter: f.globalLimiter, key: "global:" + ip},
	}

	if strings.HasPrefix(requestPath, "/api/auth/") || strings.HasPrefix(requestPath, "/api/email/send-") {
		checks = append(checks, struct {
			limiter *limiter.Limiter
			key     string
		}{limiter: f.authLimiter, key: "auth:" + ip})
	}
	if strings.HasPrefix(requestPath, "/api/admin/") ||
		strings.HasPrefix(requestPath, "/api/app/password-policy") ||
		strings.HasPrefix(requestPath, "/api/app/points") ||
		strings.HasPrefix(requestPath, "/api/app/workflow") {
		checks = append(checks, struct {
			limiter *limiter.Limiter
			key     string
		}{limiter: f.adminLimiter, key: "admin:" + ip})
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

func (f *Firewall) blockByCIDR(ip string) (bool, string) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return true, "invalid_ip"
	}
	if len(f.allowedCIDRs) > 0 {
		allowed := false
		for _, prefix := range f.allowedCIDRs {
			if prefix.Contains(addr) {
				allowed = true
				break
			}
		}
		if !allowed {
			return true, "not_in_allowlist"
		}
	}
	for _, prefix := range f.blockedCIDRs {
		if prefix.Contains(addr) {
			return true, "blocked_cidr"
		}
	}
	return false, ""
}

func (f *Firewall) blockByUserAgent(userAgent string) (bool, string) {
	userAgent = strings.ToLower(strings.TrimSpace(userAgent))
	if userAgent == "" {
		return false, ""
	}
	for _, fragment := range f.blockedUserAgents {
		if strings.Contains(userAgent, fragment) {
			return true, "blocked_user_agent"
		}
	}
	return false, ""
}

func (f *Firewall) blockByPathOrQuery(path, rawQuery string) (bool, string) {
	target := strings.ToLower(strings.TrimSpace(path))
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	for _, prefix := range f.blockedPathPrefix {
		if strings.HasPrefix(target, prefix) {
			return true, "blocked_path"
		}
	}
	combined := target
	if query != "" {
		combined += "?" + query
	}
	for _, fragment := range f.blockedFragments {
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
