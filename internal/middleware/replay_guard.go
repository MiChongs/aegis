package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/config"
	redisrepo "aegis/internal/repository/redis"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	headerTimestamp = "X-Timestamp"
	headerNonce     = "X-Nonce"
	headerSignature = "X-Signature"

	// 指纹计算时 body 最大读取量
	fingerprintBodyLimit = 2048

	// snapshotBody 的通用 JSON/form body 上限：8 MB
	// 对非文件型请求做去重/签名足够；多出的部分对 replay 无意义
	replayGuardBodyLimit = 8 << 20
)

// ReplayGuard 防重复请求中间件。
//
// 四层，从"调用方明确表达意图"到"服务端兜底猜测"依次降级：
//
//	1. 路由策略   —— 这条路由该不该做去重（replay_policy.go 的那张表）
//	2. Nonce+签名 —— 客户端带了 X-Timestamp / X-Nonce / X-Signature 时的强校验
//	3. 幂等键     —— 带了 Idempotency-Key 时回放首次响应（idempotency.go）
//	4. 指纹兜底   —— 只对 PolicyGuarded 的路由生效
//
// 这个顺序不是随手排的：越靠前的层越知道调用方**想要什么**，
// 越靠后的层越是在替调用方猜。旧实现只有第 4 层，且对所有非 GET 请求生效，
// 于是「猜」变成了默认行为，验证码这类天然可重复的接口被稳定误杀。
type ReplayGuard struct {
	enabled          bool
	signatureEnabled bool
	repo             *redisrepo.ReplayRepository
	idem             *IdempotencyManager
	nonceWindow      time.Duration
	nonceSkew        time.Duration
	nonceExpiry      time.Duration
	fingerprintTTL   time.Duration
	jwtSecret        string
	log              *zap.Logger
}

// NewReplayGuard 创建防重复请求中间件
func NewReplayGuard(cfg config.ReplayProtectionConfig, jwtSecret string, repo *redisrepo.ReplayRepository, log *zap.Logger) *ReplayGuard {
	if log == nil {
		log = zap.NewNop()
	}
	nonceExpiry := cfg.NonceWindow + cfg.NonceSkew + 30*time.Second

	var idem *IdempotencyManager
	if cfg.IdempotencyEnabled {
		idem = NewIdempotencyManager(repo, cfg.IdempotencyTTL, cfg.IdempotencyLockTTL, log)
	}

	return &ReplayGuard{
		enabled:          cfg.Enabled,
		signatureEnabled: cfg.SignatureEnabled,
		repo:             repo,
		idem:             idem,
		nonceWindow:      cfg.NonceWindow,
		nonceSkew:        cfg.NonceSkew,
		nonceExpiry:      nonceExpiry,
		fingerprintTTL:   cfg.FingerprintTTL,
		jwtSecret:        jwtSecret,
		log:              log,
	}
}

// Handler 返回 Gin 中间件
func (g *ReplayGuard) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g == nil || !g.enabled || g.repo == nil {
			c.Next()
			return
		}

		method := strings.ToUpper(c.Request.Method)
		// 幂等方法不需要任何去重
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}

		// Layer 1：路由策略。明确声明可重复的路由到此为止。
		policy, reason := ResolveReplayPolicy(method, path)
		if policy == PolicyOff {
			c.Header("X-Replay-Policy", "off")
			_ = reason // 保留在表里供排障阅读，不进响应头
			c.Next()
			return
		}

		body, err := g.snapshotBody(c.Request)
		if err != nil {
			c.Next()
			return
		}

		// Layer 2：Nonce + 时间戳 + 签名
		ts := strings.TrimSpace(c.GetHeader(headerTimestamp))
		nonce := strings.TrimSpace(c.GetHeader(headerNonce))
		if ts != "" && nonce != "" {
			if !g.verifyNonce(c, method, path, ts, nonce, body) {
				return
			}
			c.Next()
			return
		}

		// Layer 3：幂等键
		key, present, keyErr := NormalizeIdempotencyKey(c)
		if keyErr != nil {
			response.Error(c, http.StatusBadRequest, 40387,
				"Idempotency-Key 格式无效：需为 8-255 个可打印 ASCII 字符且不含冒号")
			c.Abort()
			return
		}
		if present && g.idem != nil {
			if g.idem.Handle(c, key, canonicalRequestHash(method, path, body)) {
				return
			}
			c.Next()
			return
		}

		// Layer 4：指纹兜底。**只对声明为 guarded 的路由生效。**
		if policy == PolicyGuarded {
			g.guardByFingerprint(c, method, path, body)
			return
		}

		c.Next()
	}
}

// verifyNonce 校验时间窗、签名与一次性 Nonce。返回 false 表示已写入错误响应。
//
// 顺序是刻意的：**先验签名，后消耗 Nonce。**
// 反过来的话（旧实现就是反的），一个签名错误的请求会把 Nonce 先烧掉，
// 于是客户端拿正确签名重试时收到的是「重复请求」——
// 而它其实一次都没成功过。更糟的是，任何人只要猜到别人正在用的 Nonce，
// 就能用一个乱签名把它作废掉。
func (g *ReplayGuard) verifyNonce(c *gin.Context, method, path, ts, nonce string, body []byte) bool {
	tsUnix, parseErr := strconv.ParseInt(ts, 10, 64)
	if parseErr != nil {
		response.Error(c, http.StatusForbidden, 40381, "无效的请求时间戳")
		c.Abort()
		return false
	}
	diff := time.Duration(math.Abs(float64(time.Now().Unix()-tsUnix))) * time.Second
	if diff > g.nonceWindow+g.nonceSkew {
		g.log.Warn("replay guard: timestamp expired",
			zap.String("ip", c.ClientIP()),
			zap.String("path", path),
			zap.Int64("timestamp", tsUnix),
			zap.Duration("diff", diff),
		)
		response.Error(c, http.StatusForbidden, 40381, "请求时间戳已过期")
		c.Abort()
		return false
	}

	if len(nonce) < 8 || len(nonce) > 128 {
		response.Error(c, http.StatusForbidden, 40382, "无效的 Nonce")
		c.Abort()
		return false
	}

	if sig := strings.TrimSpace(c.GetHeader(headerSignature)); sig != "" && g.signatureEnabled {
		payload := ts + "\n" + nonce + "\n" + method + "\n" + path + "\n" + sha256Hex(body)
		expected := computeHMAC(g.deriveSecret(c), payload)
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			g.log.Warn("replay guard: signature mismatch",
				zap.String("ip", c.ClientIP()),
				zap.String("path", path),
			)
			response.Error(c, http.StatusForbidden, 40383, "签名验证失败")
			c.Abort()
			return false
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
	defer cancel()
	acquired, redisErr := g.repo.TryAcquireNonce(ctx, nonce, g.nonceExpiry)
	if redisErr != nil {
		// Redis 故障时放行。防重放是纵深防御的一层，不是唯一一层；
		// 把它变成硬依赖会让一次缓存抖动升级成全站写操作不可用。
		g.log.Warn("replay guard: redis nonce check failed", zap.Error(redisErr))
		return true
	}
	if !acquired {
		g.log.Warn("replay guard: nonce reused",
			zap.String("ip", c.ClientIP()),
			zap.String("path", path),
			zap.String("nonce", nonce[:8]+"..."),
		)
		response.Error(c, http.StatusConflict, 40382, "重复请求：该 Nonce 已被使用")
		c.Abort()
		return false
	}
	return true
}

// guardByFingerprint 指纹兜底。只在 PolicyGuarded 且调用方没给幂等键时走到。
func (g *ReplayGuard) guardByFingerprint(c *gin.Context, method, path string, body []byte) {
	fp := g.computeFingerprint(c, body)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
	defer cancel()
	acquired, redisErr := g.repo.TryAcquireFingerprint(ctx, fp, g.fingerprintTTL)
	if redisErr != nil {
		g.log.Warn("replay guard: redis fingerprint check failed", zap.Error(redisErr))
		c.Next()
		return
	}
	if !acquired {
		g.log.Warn("replay guard: duplicate request fingerprint",
			zap.String("ip", c.ClientIP()),
			zap.String("path", path),
			zap.String("method", method),
		)
		// 409 而不是 403：这不是"你没有权限"，是"这一单可能已经在处理中"。
		// 403 会把调用方引向排查凭据，而正确的动作是稍后重试或去查询结果。
		c.Header("Retry-After", "1")
		response.Error(c, http.StatusConflict, 40382,
			"重复请求：相同内容的提交正在处理中，请稍后重试或改用 Idempotency-Key")
		c.Abort()
		return
	}

	c.Next()

	// 这次请求失败了就把占位还回去。指纹是在执行前占下的，
	// 而失败的请求没有产生副作用 —— 留着它会让用户改完参数立刻重试时
	// 撞上自己刚才那次失败，且错误信息是「重复请求」，与真正的问题无关。
	if c.Writer.Status() >= http.StatusBadRequest {
		releaseCtx, releaseCancel := context.WithTimeout(
			context.WithoutCancel(c.Request.Context()), 200*time.Millisecond)
		defer releaseCancel()
		if err := g.repo.ReleaseFingerprint(releaseCtx, fp); err != nil {
			g.log.Warn("replay guard: release fingerprint failed", zap.Error(err))
		}
	}
}

// snapshotBody 读取请求体并恢复，供后续 handler 使用
//
// 关键边界处理：
//   - multipart/form-data 与 application/octet-stream 不读取 body，直接返回 nil
//     原因：文件上传 body 往往数 MB，一旦用 LimitReader 截断，multipart 边界会在
//     下游 handler 解析时丢失，表现为"上传 >1MB 文件请求被截断"的 bug。
//     这两类请求有 AdminAuth/AdminAccess 的会话验证兜底，replay 层无需再读 body。
//   - 其它请求最多读 replayGuardBodyLimit（8MB），超过部分对 replay 指纹无意义。
func (g *ReplayGuard) snapshotBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	contentType := strings.ToLower(req.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") ||
		strings.HasPrefix(contentType, "application/octet-stream") {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, replayGuardBodyLimit))
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// computeFingerprint 计算请求指纹
func (g *ReplayGuard) computeFingerprint(c *gin.Context, body []byte) string {
	h := sha256.New()
	h.Write([]byte(c.Request.Method))
	h.Write([]byte(c.Request.URL.Path))
	h.Write([]byte(c.ClientIP()))

	// auth token 前缀（区分不同用户，不暴露完整 token）
	auth := c.GetHeader("Authorization")
	if len(auth) > 20 {
		auth = auth[:20]
	}
	h.Write([]byte(auth))

	// body 前 2KB
	limit := len(body)
	if limit > fingerprintBodyLimit {
		limit = fingerprintBodyLimit
	}
	if limit > 0 {
		h.Write(body[:limit])
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// deriveSecret 从请求上下文派生签名密钥
func (g *ReplayGuard) deriveSecret(c *gin.Context) string {
	// 优先使用 Authorization 头中的 token 作为密钥种子
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && len(auth) > 20 {
		return sha256Hex([]byte(auth))[:32]
	}
	// 管理员 token
	adminToken := c.GetHeader("X-Admin-Token")
	if len(adminToken) > 10 {
		return sha256Hex([]byte(adminToken))[:32]
	}
	// 兜底：使用全局 JWT Secret 的派生
	return sha256Hex([]byte("replay:" + g.jwtSecret))[:32]
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:])
}

func computeHMAC(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
