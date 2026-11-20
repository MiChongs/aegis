package redis

// 站内信缓存（docs/notification-center.md「缓存」）。
//
// 每个用户一个哈希键：字段 counts 是分类未读计数，字段 list:<筛选>:<页>:<条数> 是一页列表。
// 失效只要 DEL 这一个键——旧实现按前缀 SCAN 整个库，群发十万人就是十万次全库扫描。
//
// 字段各有寿命（不超过最早一条消息的过期时间），哈希只能整键设过期，所以每个值包一层 {e: 到期毫秒, v: 值}，
// 读到过期的字段当作没有并顺手删掉；整键的过期时间固定为 NotificationCacheMaxTTL，每次写入顺延。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// NotificationCacheMaxTTL 站内信缓存的最长寿命（整键过期时间）。
const NotificationCacheMaxTTL = 60 * time.Second

type notificationCacheEnvelope struct {
	ExpiresAt int64           `json:"e"`
	Value     json.RawMessage `json:"v"`
}

func (r *SessionRepository) notificationCacheKey(appID, userID int64) string {
	return fmt.Sprintf("%s:notification:cache:%d:%d", r.keyPrefix, appID, userID)
}

func notificationListField(query string, page, limit int) string {
	return "list:" + query + ":" + strconv.Itoa(page) + ":" + strconv.Itoa(limit)
}

const notificationCountsField = "counts"

func (r *SessionRepository) getNotificationField(ctx context.Context, appID, userID int64, field string, target any) (bool, error) {
	key := r.notificationCacheKey(appID, userID)
	raw, err := r.client.HGet(ctx, key, field).Bytes()
	if err != nil {
		if err == redislib.Nil {
			return false, nil
		}
		return false, err
	}
	var envelope notificationCacheEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || time.Now().UnixMilli() >= envelope.ExpiresAt {
		_ = r.client.HDel(ctx, key, field).Err()
		return false, nil
	}
	if err := json.Unmarshal(envelope.Value, target); err != nil {
		return false, err
	}
	return true, nil
}

func (r *SessionRepository) setNotificationField(ctx context.Context, appID, userID int64, field string, value any, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	ttl = min(ttl, NotificationCacheMaxTTL)
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(notificationCacheEnvelope{ExpiresAt: time.Now().Add(ttl).UnixMilli(), Value: data})
	if err != nil {
		return err
	}
	key := r.notificationCacheKey(appID, userID)
	pipe := r.client.Pipeline()
	pipe.HSet(ctx, key, field, payload)
	pipe.PExpire(ctx, key, NotificationCacheMaxTTL)
	_, err = pipe.Exec(ctx)
	return err
}

// GetNotificationListCache 一页列表缓存；query 是筛选条件拼成的键。
func (r *SessionRepository) GetNotificationListCache(ctx context.Context, appID, userID int64, query string, page, limit int, target any) (bool, error) {
	return r.getNotificationField(ctx, appID, userID, notificationListField(query, page, limit), target)
}

func (r *SessionRepository) SetNotificationListCache(ctx context.Context, appID, userID int64, query string, page, limit int, value any, ttl time.Duration) error {
	return r.setNotificationField(ctx, appID, userID, notificationListField(query, page, limit), value, ttl)
}

// GetNotificationCountsCache 分类未读计数缓存。
func (r *SessionRepository) GetNotificationCountsCache(ctx context.Context, appID, userID int64, target any) (bool, error) {
	return r.getNotificationField(ctx, appID, userID, notificationCountsField, target)
}

func (r *SessionRepository) SetNotificationCountsCache(ctx context.Context, appID, userID int64, value any, ttl time.Duration) error {
	return r.setNotificationField(ctx, appID, userID, notificationCountsField, value, ttl)
}

// InvalidateNotificationCaches 一批用户的站内信缓存全部失效：列表与分类计数、总未读、「我的」页聚合。
// 一次管道往返，每人三条 DEL（分开删，Redis Cluster 下各键不在同一个槽也没问题）。
func (r *SessionRepository) InvalidateNotificationCaches(ctx context.Context, appID int64, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}
	pipe := r.client.Pipeline()
	for _, userID := range userIDs {
		pipe.Del(ctx, r.notificationCacheKey(appID, userID))
		pipe.Del(ctx, r.notificationUnreadKey(appID, userID))
		pipe.Del(ctx, r.myKey(appID, userID))
	}
	_, err := pipe.Exec(ctx)
	return err
}
