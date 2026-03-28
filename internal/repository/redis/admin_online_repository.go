package redis

import (
	"context"
	"fmt"
	"strconv"
)

// ── 管理员在线状态（基于 SessionRepository，复用 client 和 keyPrefix） ──

// SetAdminOnline 标记管理员的某个会话为在线
func (r *SessionRepository) SetAdminOnline(ctx context.Context, adminID int64, sessionID string) error {
	pipe := r.client.TxPipeline()
	pipe.SAdd(ctx, r.adminOnlineSessionsKey(adminID), sessionID)
	pipe.SAdd(ctx, r.adminOnlineSetKey(), strconv.FormatInt(adminID, 10))
	_, err := pipe.Exec(ctx)
	return err
}

// RemoveAdminOnline 移除管理员的某个会话在线状态；若该管理员无剩余会话则移除整体在线标记
func (r *SessionRepository) RemoveAdminOnline(ctx context.Context, adminID int64, sessionID string) error {
	sessKey := r.adminOnlineSessionsKey(adminID)
	if err := r.client.SRem(ctx, sessKey, sessionID).Err(); err != nil {
		return err
	}
	count, err := r.client.SCard(ctx, sessKey).Result()
	if err != nil {
		return err
	}
	if count == 0 {
		pipe := r.client.TxPipeline()
		pipe.Del(ctx, sessKey)
		pipe.SRem(ctx, r.adminOnlineSetKey(), strconv.FormatInt(adminID, 10))
		_, err = pipe.Exec(ctx)
		return err
	}
	return nil
}

// ListOnlineAdminIDs 返回当前所有在线管理员 ID
func (r *SessionRepository) ListOnlineAdminIDs(ctx context.Context) ([]int64, error) {
	members, err := r.client.SMembers(ctx, r.adminOnlineSetKey()).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		id, err := strconv.ParseInt(m, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetOnlineSessionCount 获取管理员的在线会话数量
func (r *SessionRepository) GetOnlineSessionCount(ctx context.Context, adminID int64) (int64, error) {
	return r.client.SCard(ctx, r.adminOnlineSessionsKey(adminID)).Result()
}

// ClearAdminOnline 清除管理员的全部在线状态
func (r *SessionRepository) ClearAdminOnline(ctx context.Context, adminID int64) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.adminOnlineSessionsKey(adminID))
	pipe.SRem(ctx, r.adminOnlineSetKey(), strconv.FormatInt(adminID, 10))
	_, err := pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) adminOnlineSessionsKey(adminID int64) string {
	return fmt.Sprintf("%s:online:sessions:%d", r.keyPrefix, adminID)
}

func (r *SessionRepository) adminOnlineSetKey() string {
	return fmt.Sprintf("%s:online:admins", r.keyPrefix)
}
