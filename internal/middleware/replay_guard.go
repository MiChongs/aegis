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
	headerNonce    = "X-Nonce"
	headerSignature = "X-Signature"

	// 指纹计算时 body 最大读取量
	fingerprintBodyLimit = 2048
)

// ReplayGuard 防重放中间件（三层：指纹 + Nonce + HMAC 签名）
type ReplayGuard struct {
	enabled          bool
	signatureEnabled bool
	repo             *redisrepo.ReplayRepository
	nonceWindow      time.Duration
	nonceSkew        time.Duration
	nonceExpiry      time.Duration
	fingerprintTTL   time.Duration
	jwtSecret        string
	log              *zap.Logger
}

// NewReplayGuard 创建防重放中间件
func NewReplayGuard(cfg config.ReplayProtectionConfig, jwtSecret string, repo *redisrepo.ReplayRepository, log *zap.Logger) *ReplayGuard {
	if log == nil {
		log = zap.NewNop()
	}
	nonceExpiry := cfg.NonceWindow + cfg.NonceSkew + 30*time.Second
	return &ReplayGuard{
		enabled:          cfg.Enabled,
		signatureEnabled: cfg.SignatureEnabled,
		repo:             repo,
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
		// 跳过幂等方法
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		// 跳过健康检查
		path := c.Request.URL.Path
		if path == "/healthz" || path == "/readyz" {
			c.Next()
			return
		}

		// 读取并恢复请求体
		body, err := g.snapshotBody(c.Request)
		if err != nil {
			c.Next()
			return
		}

		ts := strings.TrimSpace(c.GetHeader(headerTimestamp))
		nonce := strings.TrimSpace(c.GetHeader(headerNonce))
		sig := strings.TrimSpace(c.GetHeader(headerSignature))

		ctx, cancel := context.WithTimeout(c.Request.Context(), 200*time.Millisecond)
		defer cancel()

		// Layer 1: Nonce + 时间戳
		if ts != "" && nonce != "" {
			// 验证时间窗口
			tsUnix, parseErr := strconv.ParseInt(ts, 10, 64)
			if parseErr != nil {
				response.Error(c, http.StatusForbidden, 40381, "无效的请求时间戳")
				c.Abort()
				return
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
				return
			}

			// Nonce 去重
			if len(nonce) < 8 || len(nonce) > 128 {
				response.Error(c, http.StatusForbidden, 40382, "无效的 Nonce")
				c.Abort()
				return
			}
			acquired, redisErr := g.repo.TryAcquireNonce(ctx, nonce, g.nonceExpiry)
			if redisErr != nil {
				g.log.Warn("replay guard: redis nonce check failed", zap.Error(redisErr))
				c.Next() // Redis 故障时放行
				return
			}
			if !acquired {
				g.log.Warn("replay guard: nonce reused",
					zap.String("ip", c.ClientIP()),
					zap.String("path", path),
					zap.String("nonce", nonce[:8]+"..."),
				)
				response.Error(c, http.StatusForbidden, 40382, "重复请求")
				c.Abort()
				return
			}

			// Layer 3: HMAC 签名验证
			if sig != "" && g.signatureEnabled {
				bodyHash := sha256Hex(body)
				payload := ts + "\n" + nonce + "\n" + method + "\n" + path + "\n" + bodyHash
				secret := g.deriveSecret(c)
				expected := computeHMAC(secret, payload)

				if !hmac.Equal([]byte(sig), []byte(expected)) {
					g.log.Warn("replay guard: signature mismatch",
						zap.String("ip", c.ClientIP()),
						zap.String("path", path),
					)
					response.Error(c, http.StatusForbidden, 40383, "签名验证失败")
					c.Abort()
					return
				}
			}

			c.Next()
			return
		}

		// Layer 2: 指纹去重（兜底，无需客户端配合）
		fp := g.computeFingerprint(c, body)
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
			response.Error(c, http.StatusForbidden, 40382, "重复请求")
			c.Abort()
			return
		}

		c.Next()
	}
}

// snapshotBody 读取请求体并恢复，供后续 handler 使用
func (g *ReplayGuard) snapshotBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<20)) // 最大 1MB
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
