package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// 永久封禁使用 10 年 TTL（Redis 不支持真正的永久 key 带 TTL 语义）
const permanentBanTTL = 10 * 365 * 24 * time.Hour

// BanMeta 存储在 Redis 中的封禁元信息
type BanMeta struct {
	BanID   int64  `json:"banId"`
	Reason  string `json:"reason"`
	Source  string `json:"source"`
	BannedAt string `json:"bannedAt"`
}

// IPBanRepository Redis 动态 IP 黑名单存储
type IPBanRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewIPBanRepository 创建 IP 封禁 Redis 仓库
func NewIPBanRepository(client *redislib.Client, keyPrefix string) *IPBanRepository {
	return &IPBanRepository{client: client, keyPrefix: keyPrefix}
}

func (r *IPBanRepository) banKey(ip string) string {
	return fmt.Sprintf("%s:firewall:ban:%s", r.keyPrefix, ip)
}

// IsBanned 检查 IP 是否被封禁（O(1) EXISTS 查询）
func (r *IPBanRepository) IsBanned(ctx context.Context, ip string) (bool, error) {
	result, err := r.client.Exists(ctx, r.banKey(ip)).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// SetBan 设置 IP 封禁（带 TTL，duration=0 表示永久）
func (r *IPBanRepository) SetBan(ctx context.Context, ip string, meta BanMeta, duration time.Duration) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	ttl := duration
	if ttl <= 0 {
		ttl = permanentBanTTL
	}
	return r.client.Set(ctx, r.banKey(ip), data, ttl).Err()
}

// RemoveBan 移除 IP 封禁
func (r *IPBanRepository) RemoveBan(ctx context.Context, ip string) error {
	return r.client.Del(ctx, r.banKey(ip)).Err()
}

// GetBanMeta 获取封禁元信息（用于调试/展示）
func (r *IPBanRepository) GetBanMeta(ctx context.Context, ip string) (*BanMeta, error) {
	data, err := r.client.Get(ctx, r.banKey(ip)).Bytes()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var meta BanMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
