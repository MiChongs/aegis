package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// LeaderboardRateLimit 排行榜专用二级限流（内存实现，叠加在 Firewall 全局限流之上）
//
// 设计要点：
//   - Firewall 已提供 IP 维度全局限流（通常 1200/min），这里再针对排行榜热点做精细节流
//   - 使用 sync.Map + 时间窗口计数器（无外部依赖，O(1) 判定）
//   - 限流键：已登录走 userID；匿名走 IP
//   - 超限返回 429，并附带 Retry-After 与 X-Leaderboard-RateLimit-* 头
//   - 节流命中返回 JSON 形式 {code:42900, message:...}，与项目统一响应格式一致
//
// 默认参数：每用户/IP 每分钟 60 次（单人正常使用远小于此值），可通过参数调整
func LeaderboardRateLimit(limitPerMinute int) gin.HandlerFunc {
	if limitPerMinute <= 0 {
		limitPerMinute = 60
	}
	const windowSize = time.Minute
	buckets := &sync.Map{} // key -> *rateBucket

	// 周期性清理过期桶，防止 map 持续增长
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			cutoff := time.Now().Add(-windowSize).UnixNano()
			buckets.Range(func(key, value any) bool {
				if b, ok := value.(*rateBucket); ok {
					b.mu.Lock()
					expired := b.resetAt < cutoff
					b.mu.Unlock()
					if expired {
						buckets.Delete(key)
					}
				}
				return true
			})
		}
	}()

	return func(c *gin.Context) {
		key := leaderboardRateKey(c)
		if key == "" {
			c.Next()
			return
		}
		bRaw, _ := buckets.LoadOrStore(key, &rateBucket{})
		bucket := bRaw.(*rateBucket)

		now := time.Now()
		bucket.mu.Lock()
		if now.UnixNano() >= bucket.resetAt {
			bucket.count = 0
			bucket.resetAt = now.Add(windowSize).UnixNano()
		}
		bucket.count++
		count := bucket.count
		resetAt := bucket.resetAt
		bucket.mu.Unlock()

		remaining := limitPerMinute - count
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-Leaderboard-RateLimit-Limit", strconv.Itoa(limitPerMinute))
		c.Header("X-Leaderboard-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-Leaderboard-RateLimit-Reset", strconv.FormatInt(resetAt/int64(time.Second), 10))

		if count > limitPerMinute {
			retry := (resetAt - now.UnixNano()) / int64(time.Second)
			if retry < 1 {
				retry = 1
			}
			c.Header("Retry-After", strconv.FormatInt(retry, 10))
			c.AbortWithStatusJSON(429, gin.H{
				"code":    42900,
				"message": "排行榜请求过于频繁，请稍后再试",
			})
			return
		}
		c.Next()
	}
}

type rateBucket struct {
	mu      sync.Mutex
	count   int
	resetAt int64 // UnixNano
}

// leaderboardRateKey 已登录优先使用 userID，匿名使用 IP
func leaderboardRateKey(c *gin.Context) string {
	if v, ok := c.Get("session"); ok {
		if s, ok := v.(interface{ GetUserID() int64 }); ok {
			id := s.GetUserID()
			if id > 0 {
				return "u:" + strconv.FormatInt(id, 10)
			}
		}
	}
	ip := c.ClientIP()
	if ip == "" {
		return ""
	}
	return "ip:" + ip
}
