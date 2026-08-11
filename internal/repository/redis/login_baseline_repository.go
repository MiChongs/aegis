package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appdomain "aegis/internal/domain/app"

	redislib "github.com/redis/go-redis/v9"
)

// LoginBaselineRepository 登录一致性基线的 Redis 存储。
//
// 键空间：
//   - {prefix}:lb:{appID}:{userID}  该用户上一次被放行的登录指纹（JSON，长 TTL）
//
// 选 Redis 而不是新建表：这是登录路径上的读 + 写，每次登录都打库不值得；
// 基线丢失的后果是「本次登录按首次处理并重建基线」，与安全语义不冲突。
type LoginBaselineRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewLoginBaselineRepository 创建登录基线存储。
func NewLoginBaselineRepository(client *redislib.Client, keyPrefix string) *LoginBaselineRepository {
	return &LoginBaselineRepository{client: client, keyPrefix: keyPrefix}
}

func (r *LoginBaselineRepository) key(appID, userID int64) string {
	return fmt.Sprintf("%s:lb:%d:%d", r.keyPrefix, appID, userID)
}

// Get 读取基线。不存在时返回 (nil, nil) —— 调用方据此按「首次登录」处理。
func (r *LoginBaselineRepository) Get(ctx context.Context, appID, userID int64) (*appdomain.LoginBaseline, error) {
	raw, err := r.client.Get(ctx, r.key(appID, userID)).Bytes()
	if err != nil {
		if errors.Is(err, redislib.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var baseline appdomain.LoginBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil {
		// 结构漂移过的旧值当作无基线，而不是让登录一直失败
		return nil, nil
	}
	return &baseline, nil
}

// Set 覆盖写入基线并刷新 TTL。
func (r *LoginBaselineRepository) Set(ctx context.Context, appID, userID int64, baseline appdomain.LoginBaseline, ttl time.Duration) error {
	payload, err := json.Marshal(baseline)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(appID, userID), payload, ttl).Err()
}

// Delete 清除基线（管理端「重置登录绑定」）。
func (r *LoginBaselineRepository) Delete(ctx context.Context, appID, userID int64) error {
	return r.client.Del(ctx, r.key(appID, userID)).Err()
}
