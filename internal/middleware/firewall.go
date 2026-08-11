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
			if ext, ok := f.banChecker.(ExtendedBanChecker); ok {
				if dec, _ := ext.CheckBan(c.Request.Context(), ip); dec.Banned {
					f.applyBanMode(c, ip, dec, state)
					return
				}
			} else if banned, _ := f.banChecker.IsBanned(c.Request.Context(), ip); banned {
				// 回退：未实现 ExtendedBanChecker，按默认模式处理
				f.applyBanMode(c, ip, firewalldomain.BanDecision{
					Banned: true,
					Mode:   state.cfg.DefaultBanMode,
					Reason: "banned_ip",
				}, state)
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
			// 单行摘要 + 精简结构字段，避免长串 stack-like 输出刷屏
			//   示例：WAF BLOCK #932235 [RCE] Unix Command Injection (command without evasion)
			//       uri: /api/system/monitor/history?keys=postgres%2Credis%2C...
			//       matched: time,location,http_api,monitor,docs,cors,firewall
			// 这个回调按「每条命中的规则」触发，此时请求的最终裁决还没产生，
			// 因此这里不能宣称拦截。CRS v4 是异常计分模式：911100 之类带 block
			// 动作的规则经 SecDefaultAction 解析成 pass，只累加异常分，真正的
			// 阻断由 phase 2 的 949110 在越过 inbound 阈值时做出。
			// Disruptive() 只说明规则**声明**了 block，写成 "BLOCK" 会让每条放行
			// 请求都在日志里显示成被拦，排查时直接指错方向。
			// 真正的拦截由 blockCoraza 另打一条 "firewall coraza blocked request"。
			verdict := "HIT"
			if cfg.CorazaDetectionOnly {
				verdict = "observe"
			}
			category := corazaRuleCategory(meta.File())
			message := truncateWAFText(rule.Message(), 96)
			uri := truncateWAFText(rule.URI(), 160)
			matched := truncateWAFText(rule.Data(), 140)
			log.Warn(
				fmt.Sprintf("WAF %s #%d [%s] %s", verdict, meta.ID(), category, message),
				zap.String("uri", uri),
				zap.String("matched", matched),
				zap.String("ip", rule.ClientIPAddress()),
			)
		})

	return coraza.NewWAF(wafConfig)
}

// corazaRuleCategory 从规则文件名提取简短分类：
//
//	REQUEST-932-APPLICATION-ATTACK-RCE.conf  → RCE
//	REQUEST-942-APPLICATION-ATTACK-SQLI.conf → SQLI
//	REQUEST-941-APPLICATION-ATTACK-XSS.conf  → XSS
//	REQUEST-920-PROTOCOL-ENFORCEMENT.conf    → PROTOCOL
func corazaRuleCategory(file string) string {
	base := path.Base(strings.ReplaceAll(file, "\\", "/"))
	base = strings.TrimSuffix(base, path.Ext(base))
	segs := strings.Split(base, "-")
	if len(segs) == 0 {
		return "WAF"
	}
	return strings.ToUpper(segs[len(segs)-1])
}

func truncateWAFText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// CorazaAllowedMethods CRS 911100（METHOD-ENFORCEMENT）的放行方法集。
//
// CRS 的默认值是 `GET HEAD POST OPTIONS`（REQUEST-901-INITIALIZATION.conf 里
// 用 `&TX:allowed_methods "@eq 0"` 守着，只在变量未设置时才写入），
// 而本平台是 REST 风格：路由表里 PUT 70 条、DELETE 75 条、PATCH 3 条。
//
// 不覆盖这个变量并不会当场拦掉写请求 —— CRS v4 是异常计分模式，911100 只加
// 5 分（critical_anomaly_score），而本项目的 inbound 阈值放宽到了 40。
// 真正的代价是每个写请求都白吃 12.5% 的异常分预算：它本身不越线，却让同一请求
// 里任何一个轻微命中更容易把总分顶过 949110 的阻断线，于是表现为
// "大部分时候好用，偶尔 403"。此外每个写请求都会刷一条 911100 告警日志。
// 阈值若被调到 CRS 原生的 5，则每个 PUT/PATCH/DELETE 当场 403。
//
// 这里刻意选择"设置放行集"而不是 SecRuleRemoveById 911100：后者会连
// TRACE / TRACK / CONNECT 一起放行（TRACE 是 XST 向量），而方法白名单
// 恰恰是这条规则唯一的价值所在。
//
// 这份清单必须跟随路由表：新增一种 HTTP 方法而忘了加进来，该方法的所有接口
// 会在开了 WAF 的环境里持续掉分。因此导出给 transport 层的
// TestRegisteredRouteMethodsAreAllowedByWAF 反向核对真实路由表。
const CorazaAllowedMethods = "GET HEAD POST PUT PATCH DELETE OPTIONS"

func buildCorazaDirectives(cfg config.FirewallConfig) string {
	// 防御式规范化：保证默认阈值 / 排除清单即使被裸 struct 调用也成立
	cfg = config.NormalizeFirewallConfig(cfg)
	tempDir := strings.ReplaceAll(os.TempDir(), "\\", "/")
	paranoia := cfg.CorazaParanoia

	// CRS 常见误报（自托管 / 内网 / API 网关 / 后台 SaaS 场景）在此统一放行：
	//   920350 — Host 头为数字 IP：内网、docker compose、局域网直连 IP 都会命中
	//   920300 — Accept 头缺失：healthz / readyz / 自动化脚本 / SDK 默认不带
	//   920420 — Content-Type 不在白名单：前端 FormData、SSE、OAuth callback 等会被误伤
	//   920230 — 多重 URL 编码：部分浏览器 / 代理会二次编码
	//   920440 — URL 后缀命中静态扩展：Next.js 打包产物 (.map/.wasm) 会被拦
	//   913100 — UA 命中扫描器关键字：误伤正常健康检查 (httpclient)，由我方黑名单兜底
	//   921110 — HTTP Request Smuggling：在反代层（Nginx/Caddy）已拦截，gin 侧重复拦截易误伤
	//   932200 — RCE via Unix 命令：中文姓名、路径等极易触发
	//   932230 — Unix shell metacharacters：反斜杠/管道符在 JSON / Markdown 中常见
	//   932235 — Unix command (no evasion)：query 参数如 keys=time,location,docs 命中命令名黑名单（本次 #9 报错）
	//   932236 — Unix command with evasion：类似 932235，变体模式
	//   932240 — Unix command execution via @：username/邮箱等含 @ 的参数会命中
	//   932250 — Unix command execution via $：模板字符串、正则中 $ 常见
	//   942100 — SQLi libinjection：Next.js / 表单中的正则、emoji 会误伤
	//   942430/942440 — SQL 关键字 + 注释符号：Markdown 内容会误伤
	//   941100 — XSS 过滤器：富文本 / Markdown 场景会误伤（应用层已做 sanitize）
	//
	// 排除清单默认取 config.DefaultCorazaRemovedRules，可经
	// FIREWALL_CORAZA_REMOVED_RULES 覆盖（"none" = 不排除任何规则）。
	// 剩余的 900-series 协议校验 / 949/959/980 score 合计依然生效，不会裸奔。
	engine := "On"
	if cfg.CorazaDetectionOnly {
		// 观察模式：规则全量评估、命中走 ErrorCallback 记录 zap 日志（verdict=observe），
		// 但不产生拦截/入库 —— 用于灰度验证 paranoia / 阈值调整的误报面。
		engine = "DetectionOnly"
	}
	var removed strings.Builder
	for _, id := range cfg.CorazaRemovedRules {
		removed.WriteString("SecRuleRemoveById ")
		removed.WriteString(id)
		removed.WriteString("\n")
	}
	return fmt.Sprintf(`
Include @coraza.conf-recommended
Include @crs-setup.conf.example
SecRuleEngine %s
SecRequestBodyAccess On
SecResponseBodyAccess Off
SecDataDir "%s"
SecAction "id:1000001,phase:1,pass,nolog,setvar:tx.blocking_paranoia_level=%d,setvar:tx.detection_paranoia_level=%d,setvar:tx.inbound_anomaly_score_threshold=%d,setvar:tx.outbound_anomaly_score_threshold=20"
SecAction "id:1000002,phase:1,pass,nolog,setvar:tx.allowed_methods=%s"
SecRule REQUEST_HEADERS:Content-Type "@rx ^application/(?:json|[a-z0-9.+-]+\\+json)" "id:1000003,phase:1,pass,nolog,ctl:ruleRemoveByTag=attack-sqli,ctl:ruleRemoveByTag=attack-xss,ctl:ruleRemoveByTag=attack-rce,ctl:ruleRemoveByTag=attack-protocol"
Include @owasp_crs/*.conf
%s`, engine, tempDir, paranoia, paranoia, cfg.CorazaAnomalyThreshold, CorazaAllowedMethods, removed.String())
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
