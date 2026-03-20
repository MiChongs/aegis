package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/auth"
	"aegis/internal/domain/user"
	redislib "github.com/redis/go-redis/v9"
)

type SessionRepository struct {
	client    *redislib.Client
	keyPrefix string
}

func NewSessionRepository(client *redislib.Client, keyPrefix string) *SessionRepository {
	return &SessionRepository{client: client, keyPrefix: keyPrefix}
}

func (r *SessionRepository) SetSession(ctx context.Context, token string, session auth.Session, ttl time.Duration) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	tokenHash := hashToken(token)
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, r.sessionKeyByHash(tokenHash), data, ttl)
	pipe.ZAdd(ctx, r.userSessionsKey(session.AppID, session.UserID), redislib.Z{
		Score:  float64(session.IssuedAt.Unix()),
		Member: tokenHash,
	})
	pipe.Expire(ctx, r.userSessionsKey(session.AppID, session.UserID), ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) GetSession(ctx context.Context, token string) (*auth.Session, error) {
	value, err := r.client.Get(ctx, r.sessionKey(token)).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var session auth.Session
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) DeleteSession(ctx context.Context, token string) error {
	tokenHash := hashToken(token)
	session, err := r.GetSession(ctx, token)
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.sessionKeyByHash(tokenHash))
	if session != nil {
		pipe.ZRem(ctx, r.userSessionsKey(session.AppID, session.UserID), tokenHash)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) DeleteSessionByHash(ctx context.Context, appID int64, userID int64, tokenHash string) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.sessionKeyByHash(tokenHash))
	pipe.ZRem(ctx, r.userSessionsKey(appID, userID), tokenHash)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *SessionRepository) BlacklistToken(ctx context.Context, tokenID string, ttl time.Duration) error {
	return r.client.Set(ctx, r.blacklistKey(tokenID), "1", ttl).Err()
}

func (r *SessionRepository) IsBlacklisted(ctx context.Context, tokenID string) (bool, error) {
	count, err := r.client.Exists(ctx, r.blacklistKey(tokenID)).Result()
	return count > 0, err
}

func (r *SessionRepository) SetOAuthState(ctx context.Context, state string, payload map[string]string, ttl time.Duration) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.oauthStateKey(state), data, ttl).Err()
}

func (r *SessionRepository) ConsumeOAuthState(ctx context.Context, state string) (map[string]string, error) {
	key := r.oauthStateKey(state)
	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return nil, err
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (r *SessionRepository) GetMyView(ctx context.Context, appID int64, userID int64) (*user.MyView, error) {
	var view user.MyView
	found, err := r.getJSON(ctx, r.myKey(appID, userID), &view)
	if err != nil || !found {
		return nil, err
	}
	return &view, nil
}

func (r *SessionRepository) SetMyView(ctx context.Context, appID int64, userID int64, view user.MyView, ttl time.Duration) error {
	return r.setJSON(ctx, r.myKey(appID, userID), view, ttl)
}

func (r *SessionRepository) DeleteMyView(ctx context.Context, appID int64, userID int64) error {
	return r.client.Del(ctx, r.myKey(appID, userID)).Err()
}

func (r *SessionRepository) GetUserProfile(ctx context.Context, appID int64, userID int64) (*user.Profile, error) {
	var profile user.Profile
	found, err := r.getJSON(ctx, r.profileKey(appID, userID), &profile)
	if err != nil || !found {
		return nil, err
	}
	return &profile, nil
}

func (r *SessionRepository) SetUserProfile(ctx context.Context, appID int64, userID int64, profile user.Profile, ttl time.Duration) error {
	return r.setJSON(ctx, r.profileKey(appID, userID), profile, ttl)
}

func (r *SessionRepository) DeleteUserProfile(ctx context.Context, appID int64, userID int64) error {
	return r.client.Del(ctx, r.profileKey(appID, userID)).Err()
}

func (r *SessionRepository) GetUserSettings(ctx context.Context, appID int64, userID int64, category string) (*user.Settings, error) {
	var settings user.Settings
	found, err := r.getJSON(ctx, r.settingsKey(appID, userID, category), &settings)
	if err != nil || !found {
		return nil, err
	}
	return &settings, nil
}

func (r *SessionRepository) SetUserSettings(ctx context.Context, appID int64, userID int64, category string, settings user.Settings, ttl time.Duration) error {
	return r.setJSON(ctx, r.settingsKey(appID, userID, category), settings, ttl)
}

func (r *SessionRepository) DeleteUserSettings(ctx context.Context, appID int64, userID int64, category string) error {
	return r.client.Del(ctx, r.settingsKey(appID, userID, category)).Err()
}

func (r *SessionRepository) GetSecurityStatus(ctx context.Context, appID int64, userID int64) (*user.SecurityStatus, error) {
	var status user.SecurityStatus
	found, err := r.getJSON(ctx, r.securityKey(appID, userID), &status)
	if err != nil || !found {
		return nil, err
	}
	return &status, nil
}

func (r *SessionRepository) SetSecurityStatus(ctx context.Context, appID int64, userID int64, status user.SecurityStatus, ttl time.Duration) error {
	return r.setJSON(ctx, r.securityKey(appID, userID), status, ttl)
}

func (r *SessionRepository) DeleteSecurityStatus(ctx context.Context, appID int64, userID int64) error {
	return r.client.Del(ctx, r.securityKey(appID, userID)).Err()
}

func (r *SessionRepository) GetRankingCache(ctx context.Context, namespace string, appID int64, rankingType string, scope string, page int, limit int, target any) (bool, error) {
	return r.getJSON(ctx, r.rankingKey(namespace, appID, rankingType, scope, page, limit), target)
}

func (r *SessionRepository) SetRankingCache(ctx context.Context, namespace string, appID int64, rankingType string, scope string, page int, limit int, value any, ttl time.Duration) error {
	return r.setJSON(ctx, r.rankingKey(namespace, appID, rankingType, scope, page, limit), value, ttl)
}

func (r *SessionRepository) GetAppByID(ctx context.Context, appID int64) (*appdomain.App, error) {
	var item appdomain.App
	found, err := r.getJSON(ctx, r.appKey(appID), &item)
	if err != nil || !found {
		return nil, err
	}
	return &item, nil
}

func (r *SessionRepository) SetAppByID(ctx context.Context, appID int64, item appdomain.App, ttl time.Duration) error {
	return r.setJSON(ctx, r.appKey(appID), item, ttl)
}

func (r *SessionRepository) DeleteAppByID(ctx context.Context, appID int64) error {
	return r.client.Del(ctx, r.appKey(appID)).Err()
}

func (r *SessionRepository) GetBanners(ctx context.Context, appID int64) ([]appdomain.Banner, error) {
	var items []appdomain.Banner
	found, err := r.getJSON(ctx, r.bannerKey(appID), &items)
	if err != nil || !found {
		return nil, err
	}
	return items, nil
}

func (r *SessionRepository) SetBanners(ctx context.Context, appID int64, items []appdomain.Banner, ttl time.Duration) error {
	return r.setJSON(ctx, r.bannerKey(appID), items, ttl)
}

func (r *SessionRepository) DeleteBanners(ctx context.Context, appID int64) error {
	return r.client.Del(ctx, r.bannerKey(appID)).Err()
}

func (r *SessionRepository) GetNotices(ctx context.Context, appID int64) ([]appdomain.Notice, error) {
	var items []appdomain.Notice
	found, err := r.getJSON(ctx, r.noticeKey(appID), &items)
	if err != nil || !found {
		return nil, err
	}
	return items, nil
}

func (r *SessionRepository) SetNotices(ctx context.Context, appID int64, items []appdomain.Notice, ttl time.Duration) error {
	return r.setJSON(ctx, r.noticeKey(appID), items, ttl)
}

func (r *SessionRepository) DeleteNotices(ctx context.Context, appID int64) error {
	return r.client.Del(ctx, r.noticeKey(appID)).Err()
}

func (r *SessionRepository) GetNotificationListCache(ctx context.Context, appID int64, userID int64, status string, page int, limit int, target any) (bool, error) {
	return r.getJSON(ctx, r.notificationListKey(appID, userID, status, page, limit), target)
}

func (r *SessionRepository) SetNotificationListCache(ctx context.Context, appID int64, userID int64, status string, page int, limit int, value any, ttl time.Duration) error {
	return r.setJSON(ctx, r.notificationListKey(appID, userID, status, page, limit), value, ttl)
}

func (r *SessionRepository) DeleteNotificationListCache(ctx context.Context, appID int64, userID int64) error {
	return r.deleteByPattern(ctx, fmt.Sprintf("%s:notification:list:%d:%d:*", r.keyPrefix, appID, userID))
}

func (r *SessionRepository) GetNotificationUnreadCount(ctx context.Context, appID int64, userID int64) (int64, bool, error) {
	value, err := r.client.Get(ctx, r.notificationUnreadKey(appID, userID)).Result()
	if err != nil {
		if err == redislib.Nil {
			return 0, false, nil
		}
		return 0, false, err
	}
	var count int64
	if _, err := fmt.Sscanf(value, "%d", &count); err != nil {
		return 0, false, err
	}
	return count, true, nil
}

func (r *SessionRepository) SetNotificationUnreadCount(ctx context.Context, appID int64, userID int64, count int64, ttl time.Duration) error {
	return r.client.Set(ctx, r.notificationUnreadKey(appID, userID), fmt.Sprintf("%d", count), ttl).Err()
}

func (r *SessionRepository) DeleteNotificationUnreadCount(ctx context.Context, appID int64, userID int64) error {
	return r.client.Del(ctx, r.notificationUnreadKey(appID, userID)).Err()
}

func (r *SessionRepository) ListUserSessions(ctx context.Context, appID int64, userID int64) ([]auth.IndexedSession, error) {
	hashes, err := r.client.ZRange(ctx, r.userSessionsKey(appID, userID), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	items := make([]auth.IndexedSession, 0, len(hashes))
	stale := make([]any, 0)
	for _, tokenHash := range hashes {
		session, err := r.getSessionByHash(ctx, tokenHash)
		if err != nil {
			return nil, err
		}
		if session == nil {
			stale = append(stale, tokenHash)
			continue
		}
		items = append(items, auth.IndexedSession{
			TokenHash: tokenHash,
			Session:   *session,
		})
	}
	if len(stale) > 0 {
		_ = r.client.ZRem(ctx, r.userSessionsKey(appID, userID), stale...).Err()
	}
	return items, nil
}

func (r *SessionRepository) setJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, data, ttl).Err()
}

func (r *SessionRepository) getJSON(ctx context.Context, key string, target any) (bool, error) {
	value, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redislib.Nil {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal([]byte(value), target); err != nil {
		return false, err
	}
	return true, nil
}

func (r *SessionRepository) getSessionByHash(ctx context.Context, tokenHash string) (*auth.Session, error) {
	value, err := r.client.Get(ctx, r.sessionKeyByHash(tokenHash)).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var session auth.Session
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) deleteByPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			return nil
		}
	}
}

func (r *SessionRepository) sessionKey(token string) string {
	return r.sessionKeyByHash(hashToken(token))
}

func (r *SessionRepository) blacklistKey(tokenID string) string {
	return fmt.Sprintf("%s:auth:blacklist:%s", r.keyPrefix, tokenID)
}

func (r *SessionRepository) oauthStateKey(state string) string {
	return fmt.Sprintf("%s:oauth:state:%s", r.keyPrefix, state)
}

func (r *SessionRepository) appKey(appID int64) string {
	return fmt.Sprintf("%s:app:%d", r.keyPrefix, appID)
}

func (r *SessionRepository) bannerKey(appID int64) string {
	return fmt.Sprintf("%s:app:banner:%d", r.keyPrefix, appID)
}

func (r *SessionRepository) noticeKey(appID int64) string {
	return fmt.Sprintf("%s:app:notice:%d", r.keyPrefix, appID)
}

func (r *SessionRepository) myKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:user:my:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) profileKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:user:profile:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) settingsKey(appID int64, userID int64, category string) string {
	return fmt.Sprintf("%s:user:settings:%d:%d:%s", r.keyPrefix, appID, userID, category)
}

func (r *SessionRepository) securityKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:user:security:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) rankingKey(namespace string, appID int64, rankingType string, scope string, page int, limit int) string {
	return fmt.Sprintf("%s:rank:%s:%d:%s:%s:%d:%d", r.keyPrefix, namespace, appID, rankingType, scope, page, limit)
}

func (r *SessionRepository) notificationListKey(appID int64, userID int64, status string, page int, limit int) string {
	return fmt.Sprintf("%s:notification:list:%d:%d:%s:%d:%d", r.keyPrefix, appID, userID, status, page, limit)
}

func (r *SessionRepository) notificationUnreadKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:notification:unread:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) sessionKeyByHash(tokenHash string) string {
	return fmt.Sprintf("%s:auth:session:%s", r.keyPrefix, tokenHash)
}

func (r *SessionRepository) userSessionsKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:auth:user-sessions:%d:%d", r.keyPrefix, appID, userID)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
