package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	redislib "github.com/redis/go-redis/v9"
)

func (r *SessionRepository) SetRefreshSession(ctx context.Context, token string, session auth.RefreshSession, ttl time.Duration) error {
	data, err := marshalJSON(session)
	if err != nil {
		return err
	}
	tokenHash := hashToken(token)
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, r.refreshSessionKeyByHash(tokenHash), data, ttl)
	pipe.ZAdd(ctx, r.userRefreshSessionsKey(session.AppID, session.UserID), redislib.Z{
		Score:  float64(session.IssuedAt.Unix()),
		Member: tokenHash,
	})
	pipe.Expire(ctx, r.userRefreshSessionsKey(session.AppID, session.UserID), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) GetRefreshSession(ctx context.Context, token string) (*auth.RefreshSession, error) {
	var session auth.RefreshSession
	found, err := r.getJSON(ctx, r.refreshSessionKeyByHash(hashToken(token)), &session)
	if err != nil || !found {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) UpdateRefreshSession(ctx context.Context, token string, session auth.RefreshSession, ttl time.Duration) error {
	return r.setJSON(ctx, r.refreshSessionKeyByHash(hashToken(token)), session, ttl)
}

func (r *SessionRepository) DeleteRefreshSession(ctx context.Context, token string) error {
	tokenHash := hashToken(token)
	session, err := r.GetRefreshSession(ctx, token)
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.refreshSessionKeyByHash(tokenHash))
	if session != nil {
		pipe.ZRem(ctx, r.userRefreshSessionsKey(session.AppID, session.UserID), tokenHash)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) DeleteRefreshSessionByHash(ctx context.Context, appID int64, userID int64, tokenHash string) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.refreshSessionKeyByHash(tokenHash))
	pipe.ZRem(ctx, r.userRefreshSessionsKey(appID, userID), tokenHash)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) ListIndexedRefreshSessions(ctx context.Context, appID int64, userID int64) ([]auth.IndexedRefreshSession, error) {
	hashes, err := r.client.ZRange(ctx, r.userRefreshSessionsKey(appID, userID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	items := make([]auth.IndexedRefreshSession, 0, len(hashes))
	stale := make([]any, 0)
	for _, tokenHash := range hashes {
		var session auth.RefreshSession
		found, err := r.getJSON(ctx, r.refreshSessionKeyByHash(tokenHash), &session)
		if err != nil {
			return nil, err
		}
		if !found {
			stale = append(stale, tokenHash)
			continue
		}
		items = append(items, auth.IndexedRefreshSession{
			TokenHash: tokenHash,
			Session:   session,
		})
	}
	if len(stale) > 0 {
		_ = r.client.ZRem(ctx, r.userRefreshSessionsKey(appID, userID), stale...).Err()
	}
	return items, nil
}

func (r *SessionRepository) ListRefreshSessions(ctx context.Context, appID int64, userID int64) ([]auth.RefreshSession, error) {
	items, err := r.ListIndexedRefreshSessions(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	result := make([]auth.RefreshSession, 0, len(items))
	for _, item := range items {
		result = append(result, item.Session)
	}
	return result, nil
}

func (r *SessionRepository) RevokeRefreshFamily(ctx context.Context, appID int64, userID int64, familyID string, ttl time.Duration) error {
	if familyID == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	return r.client.Set(ctx, r.refreshFamilyRevokedKey(appID, userID, familyID), "1", ttl).Err()
}

func (r *SessionRepository) IsRefreshFamilyRevoked(ctx context.Context, appID int64, userID int64, familyID string) (bool, error) {
	if familyID == "" {
		return false, nil
	}
	count, err := r.client.Exists(ctx, r.refreshFamilyRevokedKey(appID, userID, familyID)).Result()
	return count > 0, err
}

func (r *SessionRepository) SetPendingProfileChange(ctx context.Context, appID int64, userID int64, change userdomain.PendingProfileChange, ttl time.Duration) error {
	return r.setJSON(ctx, r.pendingProfileChangeKey(appID, userID, change.Field), change, ttl)
}

func (r *SessionRepository) GetPendingProfileChange(ctx context.Context, appID int64, userID int64, field string) (*userdomain.PendingProfileChange, error) {
	var change userdomain.PendingProfileChange
	found, err := r.getJSON(ctx, r.pendingProfileChangeKey(appID, userID, field), &change)
	if err != nil || !found {
		return nil, err
	}
	return &change, nil
}

func (r *SessionRepository) DeletePendingProfileChange(ctx context.Context, appID int64, userID int64, field string) error {
	return r.client.Del(ctx, r.pendingProfileChangeKey(appID, userID, field)).Err()
}

func (r *SessionRepository) ListPendingProfileChanges(ctx context.Context, appID int64, userID int64) ([]userdomain.PendingProfileChange, error) {
	fields := []string{"email", "phone"}
	items := make([]userdomain.PendingProfileChange, 0, len(fields))
	for _, field := range fields {
		item, err := r.GetPendingProfileChange(ctx, appID, userID, field)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, nil
}

func (r *SessionRepository) AcquireSignInLock(ctx context.Context, appID int64, userID int64, signDate string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, r.signInLockKey(appID, userID, signDate), "1", ttl).Result()
}

func (r *SessionRepository) ReleaseSignInLock(ctx context.Context, appID int64, userID int64, signDate string) error {
	return r.client.Del(ctx, r.signInLockKey(appID, userID, signDate)).Err()
}

func (r *SessionRepository) refreshSessionKeyByHash(tokenHash string) string {
	return fmt.Sprintf("%s:auth:refresh:%s", r.keyPrefix, tokenHash)
}

func (r *SessionRepository) userRefreshSessionsKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:auth:user-refresh:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) refreshFamilyRevokedKey(appID int64, userID int64, familyID string) string {
	return fmt.Sprintf("%s:auth:refresh-family-revoked:%d:%d:%s", r.keyPrefix, appID, userID, familyID)
}

func (r *SessionRepository) pendingProfileChangeKey(appID int64, userID int64, field string) string {
	return fmt.Sprintf("%s:user:profile-change:%d:%d:%s", r.keyPrefix, appID, userID, field)
}

func (r *SessionRepository) signInLockKey(appID int64, userID int64, signDate string) string {
	return fmt.Sprintf("%s:signin:lock:%d:%d:%s", r.keyPrefix, appID, userID, signDate)
}

func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}
