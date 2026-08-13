package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// ReplayRepository Redis 防重放与幂等存储
type ReplayRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewReplayRepository 创建防重放存储
func NewReplayRepository(client *redislib.Client, keyPrefix string) *ReplayRepository {
	return &ReplayRepository{client: client, keyPrefix: keyPrefix}
}

// Client 暴露底层客户端。
//
// redislock 需要直接拿到 go-redis 客户端来跑它的 Lua 脚本（释放锁时要
// 校验持有者令牌，那必须在服务端原子完成）。这里不再包一层接口：
// 包一层只能转发同样的方法，却让 redislock 的类型约束对不上。
func (r *ReplayRepository) Client() *redislib.Client { return r.client }

// TryAcquireNonce 尝试标记 Nonce 为已使用（原子操作）
// 返回 true 表示首次使用，false 表示 Nonce 已存在（重放）
func (r *ReplayRepository) TryAcquireNonce(ctx context.Context, nonce string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, r.key("replay:nonce", nonce), "1", ttl).Result()
}

// TryAcquireFingerprint 尝试标记请求指纹（原子操作）
// 返回 true 表示首次提交，false 表示短时间内重复提交
func (r *ReplayRepository) TryAcquireFingerprint(ctx context.Context, fingerprint string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, r.key("replay:fp", fingerprint), "1", ttl).Result()
}

// ReleaseFingerprint 主动释放指纹占位。
//
// 存在的理由：指纹是在 handler **执行前**占下的，如果这次请求最终失败了
// （4xx / 5xx），那它并没有产生副作用，占位就该立刻还回去 ——
// 否则用户改完参数重试会撞上自己刚才那次失败留下的坑，
// 而错误信息是「重复请求」，与他真正遇到的问题毫无关系。
func (r *ReplayRepository) ReleaseFingerprint(ctx context.Context, fingerprint string) error {
	return r.client.Del(ctx, r.key("replay:fp", fingerprint)).Err()
}

// IdempotencyRecord 一次已完成请求的响应快照
type IdempotencyRecord struct {
	Status      int    `json:"status"`
	Body        []byte `json:"body,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	// RequestHash 首次请求的内容指纹，用于识别"同一个键配了不同的请求体"
	RequestHash string `json:"requestHash,omitempty"`
	CompletedAt int64  `json:"completedAt"`
}

// LoadIdempotency 读取幂等记录。第二个返回值为 false 表示这个作用域还没有记录。
func (r *ReplayRepository) LoadIdempotency(ctx context.Context, scope string) (IdempotencyRecord, bool, error) {
	raw, err := r.client.Get(ctx, r.key("idem", scope)).Bytes()
	if errors.Is(err, redislib.Nil) {
		return IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return IdempotencyRecord{}, false, err
	}
	var record IdempotencyRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		// 解不开的记录当作不存在：让这次请求正常执行并覆盖掉它，
		// 比让这个幂等键永久卡住要好。
		return IdempotencyRecord{}, false, nil
	}
	return record, true, nil
}

// StoreIdempotency 写入幂等记录
func (r *ReplayRepository) StoreIdempotency(ctx context.Context, scope string, record IdempotencyRecord, ttl time.Duration) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key("idem", scope), payload, ttl).Err()
}

func (r *ReplayRepository) key(domain, id string) string {
	return fmt.Sprintf("%s:%s:%s", r.keyPrefix, domain, id)
}
