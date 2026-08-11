package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	geodomain "aegis/internal/domain/geo"

	redislib "github.com/redis/go-redis/v9"
)

// GeoProfileRepository 用户地理画像的 Redis 读主缓存。
//
// 近线风控判定（不可能旅行 / 异地登录）只读这一份缓存，
// miss 时回源 PostgreSQL（user_geo_profiles）并回填，保证判定路径零 PG 查询。
type GeoProfileRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewGeoProfileRepository 创建地理画像缓存仓库。
func NewGeoProfileRepository(client *redislib.Client, keyPrefix string) *GeoProfileRepository {
	return &GeoProfileRepository{client: client, keyPrefix: keyPrefix}
}

func (r *GeoProfileRepository) key(appID, userID int64) string {
	return fmt.Sprintf("%s:geo:profile:%d:%d", r.keyPrefix, appID, userID)
}

// Get 读取画像缓存；miss 返回 (nil, nil)。
func (r *GeoProfileRepository) Get(ctx context.Context, appID, userID int64) (*geodomain.Profile, error) {
	data, err := r.client.Get(ctx, r.key(appID, userID)).Bytes()
	if err == redislib.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p geodomain.Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Set 写入画像缓存。
func (r *GeoProfileRepository) Set(ctx context.Context, p *geodomain.Profile, ttl time.Duration) error {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.key(p.AppID, p.UserID), data, ttl).Err()
}

// Delete 失效画像缓存（画像重算后调用）。
func (r *GeoProfileRepository) Delete(ctx context.Context, appID, userID int64) error {
	return r.client.Del(ctx, r.key(appID, userID)).Err()
}
