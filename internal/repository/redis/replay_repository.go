package redis

import (
	"context"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// ReplayRepository Redis 防重放存储
type ReplayRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewReplayRepository 创建防重放存储
func NewReplayRepository(client *redislib.Client, keyPrefix string) *ReplayRepository {
	return &ReplayRepository{client: client, keyPrefix: keyPrefix}
}

// TryAcquireNonce 尝试标记 Nonce 为已使用（原子操作）
// 返回 true 表示首次使用，false 表示 Nonce 已存在（重放）
func (r *ReplayRepository) TryAcquireNonce(ctx context.Context, nonce string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("%s:replay:nonce:%s", r.keyPrefix, nonce)
	return r.client.SetNX(ctx, key, "1", ttl).Result()
}

// TryAcquireFingerprint 尝试标记请求指纹（原子操作）
// 返回 true 表示首次提交，false 表示短时间内重复提交
func (r *ReplayRepository) TryAcquireFingerprint(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("%s:replay:fp:%s", r.keyPrefix, fingerprint)
	return r.client.SetNX(ctx, key, "1", ttl).Result()
}
