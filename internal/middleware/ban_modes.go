package middleware

import (
	"context"
	"math/rand"
	"net/http"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ExtendedBanChecker 允许返回完整的 BanDecision（含 Mode / Reason / Delay），
// 供防火墙中间件按模式分发响应。
//
// 历史 BanChecker（仅 IsBanned）依然兼容——本包的封禁分派逻辑会在 checker
// 仅实现 BanChecker 时回退到 DefaultBanMode。
type ExtendedBanChecker interface {
	BanChecker
	CheckBan(ctx context.Context, ip string) (firewalldomain.BanDecision, error)
}

// applyBanMode 根据决策执行对应的拦截行为。
func (f *Firewall) applyBanMode(c *gin.Context, ip string, dec firewalldomain.BanDecision, state firewallState) {
	mode := firewalldomain.NormalizeBanMode(dec.Mode, state.cfg.DefaultBanMode)
	reason := dec.Reason
	if reason == "" {
		reason = "banned_ip"
	}

	f.log.Warn("firewall blocked banned IP",
		zap.String("ip", ip),
		zap.String("mode", mode),
		zap.String("reason", reason),
		zap.Int64("ban_id", dec.BanID),
		zap.String("method", c.Request.Method),
		zap.String("path", c.Request.URL.Path),
		zap.String("request_id", requestID(c)),
	)

	switch mode {

	case firewalldomain.BanModeSilentDrop, firewalldomain.BanModeConnReset:
		// 劫持底层连接直接关闭；客户端感知为"连接重置"/"无响应"
		if dropTCPConnection(c) {
			c.Abort()
			f.emitBlockEvent(c, "banned_"+mode, 0, 0, nil, "", "")
			return
		}
		// 劫持失败（http.ResponseController 不支持 Hijack）则回退到静默 403，
		// 但不写 body，尽量降低指纹
		c.Status(http.StatusForbidden)
		c.Abort()
		f.emitBlockEvent(c, "banned_"+mode+"_fallback", http.StatusForbidden, 40397, nil, "", "")

	case firewalldomain.BanModeTarpit:
		// 拖延 DelayMs 后再返回（消耗攻击者资源）
		delay := dec.DelayMs
		if delay <= 0 {
			delay = state.cfg.TarpitDelayMs
		}
		if delay <= 0 {
			delay = 5000
		}
		if delay > 30000 {
			delay = 30000
		}
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
		case <-c.Request.Context().Done():
		}
		response.Error(c, http.StatusForbidden, 40397, "当前请求已被安全策略拦截")
		c.Abort()
		f.emitBlockEvent(c, "banned_tarpit", http.StatusForbidden, 40397, nil, "", "")

	case firewalldomain.BanModeStealth404:
		// 伪装 404 — 让扫描器以为资源不存在
		response.Error(c, http.StatusNotFound, 40400, "请求的资源不存在")
		c.Abort()
		f.emitBlockEvent(c, "banned_stealth_404", http.StatusNotFound, 40400, nil, "", "")

	case firewalldomain.BanModeStealth503:
		// 伪装 503 — 让攻击者以为服务暂时性不可用
		c.Header("Retry-After", "600")
		response.Error(c, http.StatusServiceUnavailable, 50300, "服务维护中，请稍后再试")
		c.Abort()
		f.emitBlockEvent(c, "banned_stealth_503", http.StatusServiceUnavailable, 50300, nil, "", "")

	case firewalldomain.BanModeTeapot:
		response.Error(c, http.StatusTeapot, 41800, "I'm a teapot")
		c.Abort()
		f.emitBlockEvent(c, "banned_teapot", http.StatusTeapot, 41800, nil, "", "")

	case firewalldomain.BanModeGone:
		response.Error(c, http.StatusGone, 41000, "资源已永久移除")
		c.Abort()
		f.emitBlockEvent(c, "banned_gone", http.StatusGone, 41000, nil, "", "")

	case firewalldomain.BanModeRedirect:
		target := state.cfg.BanRedirectURL
		if target == "" {
			target = "/"
		}
		c.Header("Cache-Control", "no-store")
		c.Redirect(http.StatusFound, target)
		c.Abort()
		f.emitBlockEvent(c, "banned_redirect", http.StatusFound, 0, nil, "", "")

	case firewalldomain.BanModeHoneypot:
		// 伪装成功响应，诱导攻击者误判
		c.Header("Content-Type", "application/json; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
		_, _ = c.Writer.WriteString(`{"code":200,"message":"ok","data":null}`)
		c.Abort()
		f.emitBlockEvent(c, "banned_honeypot", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeFakeEmpty:
		c.Header("Cache-Control", "no-store")
		c.Status(http.StatusOK)
		c.Abort()
		f.emitBlockEvent(c, "banned_fake_empty", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeRandomError:
		pool := []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout}
		status := pool[rand.Intn(len(pool))]
		response.Error(c, status, status*100, "服务暂时不可用")
		c.Abort()
		f.emitBlockEvent(c, "banned_random_error", status, status*100, nil, "", "")

	case firewalldomain.BanModeSlowResponse:
		// 逐字节慢输出：拖住连接，消耗攻击者并发资源
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Status(http.StatusForbidden)
		msg := []byte("forbidden")
		for _, b := range msg {
			select {
			case <-c.Request.Context().Done():
				c.Abort()
				return
			case <-time.After(500 * time.Millisecond):
				_, _ = c.Writer.Write([]byte{b})
				c.Writer.Flush()
			}
		}
		c.Abort()
		f.emitBlockEvent(c, "banned_slow_response", http.StatusForbidden, 40397, nil, "", "")

	case firewalldomain.BanModeBandwidthChoke:
		// 响应 403 时以 1 字节/100ms 的极低带宽输出
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Status(http.StatusForbidden)
		body := []byte("request blocked by firewall policy")
		for _, b := range body {
			select {
			case <-c.Request.Context().Done():
				c.Abort()
				return
			case <-time.After(100 * time.Millisecond):
				_, _ = c.Writer.Write([]byte{b})
				c.Writer.Flush()
			}
		}
		c.Abort()
		f.emitBlockEvent(c, "banned_bandwidth_choke", http.StatusForbidden, 40397, nil, "", "")

	case firewalldomain.BanModeRandomDelay:
		// 随机 1-15 秒延迟后返 403
		delay := time.Duration(1000+rand.Intn(14000)) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-c.Request.Context().Done():
		}
		response.Error(c, http.StatusForbidden, 40397, "当前请求已被安全策略拦截")
		c.Abort()
		f.emitBlockEvent(c, "banned_random_delay", http.StatusForbidden, 40397, nil, "", "")

	// ── 增强型"恶心"模式 ──

	case firewalldomain.BanModeZipBomb:
		zipBombResponse(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_zip_bomb", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeChunkedInfinite:
		chunkedInfiniteRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_chunked_infinite", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeInfiniteRedirect:
		infiniteRedirectRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_infinite_redirect", http.StatusFound, 0, nil, "", "")

	case firewalldomain.BanModeMirrorRequest:
		mirrorRequestRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_mirror_request", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeFakeLogin:
		fakeLoginRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_fake_login", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeRandomGarbage:
		randomGarbageRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_random_garbage", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeCursedHeaders:
		cursedHeadersRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_cursed_headers", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeJSONBomb:
		jsonBombRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_json_bomb", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeCookieBomb:
		cookieBombRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_cookie_bomb", http.StatusOK, 200, nil, "", "")

	case firewalldomain.BanModeReverseSlowloris:
		reverseSlowlorisRespond(c)
		c.Abort()
		f.emitBlockEvent(c, "banned_reverse_slowloris", 0, 0, nil, "", "")

	case firewalldomain.BanModeRateChoke:
		// 极严格限速 — 先拖 2s 再返回 429，下次仍会被卡
		select {
		case <-time.After(2 * time.Second):
		case <-c.Request.Context().Done():
		}
		c.Header("Retry-After", "300")
		response.Error(c, http.StatusTooManyRequests, 42900, "请求过于频繁，请稍后再试")
		c.Abort()
		f.emitBlockEvent(c, "banned_rate_choke", http.StatusTooManyRequests, 42900, nil, "", "")

	case firewalldomain.BanModeForbidden:
		fallthrough
	default:
		f.block(c, http.StatusForbidden, 40397, "banned_"+mode, "当前请求已被安全策略拦截")
	}
}

// dropTCPConnection 尝试劫持当前 HTTP 连接并关闭底层 TCP socket。
// 成功时调用者无需再写入响应；失败返回 false。
func dropTCPConnection(c *gin.Context) bool {
	hj, ok := c.Writer.(http.Hijacker)
	if !ok {
		return false
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return false
	}
	// 不写任何字节，直接 close，客户端将收到 EOF / RST
	_ = conn.Close()
	return true
}
