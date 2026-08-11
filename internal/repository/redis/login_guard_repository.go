package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// LoginGuardRepository 登录防爆破的 Redis 状态存储。
//
// 键空间（kind ∈ {acct, ip}）：
//   - {prefix}:lg:fail:{kind}:{appID}:{subject}  失败计数器（窗口 TTL）
//   - {prefix}:lg:lock:{kind}:{appID}:{subject}  锁定标记（TTL = 本次锁定时长）
//   - {prefix}:lg:lvl:{kind}:{appID}:{subject}   退避级别（TTL = 级别记忆窗口）
type LoginGuardRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewLoginGuardRepository 创建登录防爆破存储。
func NewLoginGuardRepository(client *redislib.Client, keyPrefix string) *LoginGuardRepository {
	return &LoginGuardRepository{client: client, keyPrefix: keyPrefix}
}

func (r *LoginGuardRepository) key(class, kind string, appID int64, subject string) string {
	return fmt.Sprintf("%s:lg:%s:%s:%d:%s", r.keyPrefix, class, kind, appID, strings.ToLower(strings.TrimSpace(subject)))
}

// IncrFailure 失败计数 +1；首次写入时设置窗口 TTL。返回当前计数。
func (r *LoginGuardRepository) IncrFailure(ctx context.Context, kind string, appID int64, subject string, window time.Duration) (int64, error) {
	key := r.key("fail", kind, appID, subject)
	cnt, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if cnt == 1 {
		_ = r.client.Expire(ctx, key, window).Err()
	}
	return cnt, nil
}

// ClearFailure 清空失败计数（登录成功或触发锁定后重新计窗）。
func (r *LoginGuardRepository) ClearFailure(ctx context.Context, kind string, appID int64, subject string) error {
	return r.client.Del(ctx, r.key("fail", kind, appID, subject)).Err()
}

// EscalateLevel 退避级别 +1（用于指数退避）；首次写入时设置级别记忆窗口。
func (r *LoginGuardRepository) EscalateLevel(ctx context.Context, kind string, appID int64, subject string, memory time.Duration) (int64, error) {
	key := r.key("lvl", kind, appID, subject)
	lvl, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if lvl == 1 {
		_ = r.client.Expire(ctx, key, memory).Err()
	}
	return lvl, nil
}

// SetLock 写入锁定标记，TTL 即锁定剩余时长。
func (r *LoginGuardRepository) SetLock(ctx context.Context, kind string, appID int64, subject string, ttl time.Duration) error {
	return r.client.Set(ctx, r.key("lock", kind, appID, subject), time.Now().UTC().Format(time.RFC3339), ttl).Err()
}

// LockRemaining 返回锁定剩余时长；未锁定返回 0。
func (r *LoginGuardRepository) LockRemaining(ctx context.Context, kind string, appID int64, subject string) (time.Duration, error) {
	d, err := r.client.PTTL(ctx, r.key("lock", kind, appID, subject)).Result()
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		// -2 = 键不存在；-1 = 无过期（不应出现，视作未锁定以 fail-open）
		return 0, nil
	}
	return d, nil
}
