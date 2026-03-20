package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	realtimedomain "aegis/internal/domain/realtime"
	redislib "github.com/redis/go-redis/v9"
)

type RealtimeRepository struct {
	client        *redislib.Client
	keyPrefix     string
	metaTTLJitter time.Duration
}

func NewRealtimeRepository(client *redislib.Client, keyPrefix string) *RealtimeRepository {
	return &RealtimeRepository{
		client:        client,
		keyPrefix:     keyPrefix,
		metaTTLJitter: time.Minute,
	}
}

func (r *RealtimeRepository) UpsertConnection(ctx context.Context, conn realtimedomain.PresenceConnection, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	if conn.LastSeenAt.IsZero() {
		conn.LastSeenAt = time.Now().UTC()
	}
	if conn.ConnectedAt.IsZero() {
		conn.ConnectedAt = conn.LastSeenAt
	}
	data, err := json.Marshal(conn)
	if err != nil {
		return err
	}
	expireAt := float64(conn.LastSeenAt.Add(ttl).Unix())
	metaTTL := ttl + ttl + r.metaTTLJitter
	userMember := strconv.FormatInt(conn.UserID, 10)
	appUserMember := r.globalUserMember(conn.AppID, conn.UserID)
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, r.connectionMetaKey(conn.ConnectionID), data, metaTTL)
	pipe.ZAdd(ctx, r.globalConnectionsKey(), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	pipe.ZAdd(ctx, r.appConnectionsKey(conn.AppID), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	pipe.ZAdd(ctx, r.userConnectionsKey(conn.AppID, conn.UserID), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	pipe.ZAdd(ctx, r.globalUsersKey(), redislib.Z{Score: expireAt, Member: appUserMember})
	pipe.ZAdd(ctx, r.appUsersKey(conn.AppID), redislib.Z{Score: expireAt, Member: userMember})
	_, err = pipe.Exec(ctx)
	return err
}

func (r *RealtimeRepository) RefreshConnection(ctx context.Context, connectionID string, ttl time.Duration) (*realtimedomain.PresenceConnection, error) {
	conn, err := r.GetConnection(ctx, connectionID)
	if err != nil || conn == nil {
		return conn, err
	}
	conn.LastSeenAt = time.Now().UTC()
	return conn, r.UpsertConnection(ctx, *conn, ttl)
}

func (r *RealtimeRepository) GetConnection(ctx context.Context, connectionID string) (*realtimedomain.PresenceConnection, error) {
	raw, err := r.client.Get(ctx, r.connectionMetaKey(connectionID)).Bytes()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var conn realtimedomain.PresenceConnection
	if err := json.Unmarshal(raw, &conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (r *RealtimeRepository) RemoveConnection(ctx context.Context, appID int64, userID int64, connectionID string) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.connectionMetaKey(connectionID))
	pipe.ZRem(ctx, r.globalConnectionsKey(), connectionID)
	pipe.ZRem(ctx, r.appConnectionsKey(appID), connectionID)
	pipe.ZRem(ctx, r.userConnectionsKey(appID, userID), connectionID)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	return r.refreshUserIndexes(ctx, appID, userID)
}

func (r *RealtimeRepository) CleanupExpired(ctx context.Context) error {
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	connectionIDs, err := r.client.ZRangeByScore(ctx, r.globalConnectionsKey(), &redislib.ZRangeBy{
		Min: "-inf",
		Max: now,
	}).Result()
	if err != nil {
		return err
	}
	for _, connectionID := range connectionIDs {
		conn, err := r.GetConnection(ctx, connectionID)
		if err != nil {
			return err
		}
		if conn != nil {
			if err := r.RemoveConnection(ctx, conn.AppID, conn.UserID, connectionID); err != nil {
				return err
			}
			continue
		}
		if err := r.client.ZRem(ctx, r.globalConnectionsKey(), connectionID).Err(); err != nil {
			return err
		}
	}
	return r.cleanupExpiredUsers(ctx, now)
}

func (r *RealtimeRepository) OnlineStats(ctx context.Context) (*realtimedomain.OnlineStats, error) {
	if err := r.CleanupExpired(ctx); err != nil {
		return nil, err
	}
	userMembers, err := r.client.ZRange(ctx, r.globalUsersKey(), 0, -1).Result()
	if err != nil {
		return nil, err
	}
	apps := make(map[int64]struct{}, len(userMembers))
	for _, member := range userMembers {
		appID, _, ok := parseGlobalUserMember(member)
		if !ok {
			continue
		}
		apps[appID] = struct{}{}
	}
	connections, err := r.client.ZCard(ctx, r.globalConnectionsKey()).Result()
	if err != nil {
		return nil, err
	}
	users, err := r.client.ZCard(ctx, r.globalUsersKey()).Result()
	if err != nil {
		return nil, err
	}
	return &realtimedomain.OnlineStats{
		OnlineUsers:       users,
		OnlineConnections: connections,
		OnlineApps:        int64(len(apps)),
		RefreshedAt:       time.Now().UTC(),
	}, nil
}

func (r *RealtimeRepository) AppOnlineStats(ctx context.Context, appID int64) (*realtimedomain.AppOnlineStats, error) {
	if err := r.CleanupExpired(ctx); err != nil {
		return nil, err
	}
	users, err := r.client.ZCard(ctx, r.appUsersKey(appID)).Result()
	if err != nil {
		return nil, err
	}
	connections, err := r.client.ZCard(ctx, r.appConnectionsKey(appID)).Result()
	if err != nil {
		return nil, err
	}
	return &realtimedomain.AppOnlineStats{
		AppID:             appID,
		OnlineUsers:       users,
		OnlineConnections: connections,
		RefreshedAt:       time.Now().UTC(),
	}, nil
}

func (r *RealtimeRepository) ListAppOnlineUsers(ctx context.Context, appID int64, page int, limit int) (*realtimedomain.AppOnlineUserList, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if err := r.CleanupExpired(ctx); err != nil {
		return nil, err
	}
	total, err := r.client.ZCard(ctx, r.appUsersKey(appID)).Result()
	if err != nil {
		return nil, err
	}
	offset := int64((page - 1) * limit)
	stop := offset + int64(limit) - 1
	userIDs, err := r.client.ZRevRange(ctx, r.appUsersKey(appID), offset, stop).Result()
	if err != nil {
		return nil, err
	}
	items := make([]realtimedomain.AppOnlineUser, 0, len(userIDs))
	for _, rawUserID := range userIDs {
		userID, err := strconv.ParseInt(strings.TrimSpace(rawUserID), 10, 64)
		if err != nil {
			continue
		}
		item, err := r.buildAppOnlineUser(ctx, appID, userID)
		if err != nil {
			return nil, err
		}
		if item.Connections == 0 {
			continue
		}
		items = append(items, item)
	}
	return &realtimedomain.AppOnlineUserList{
		AppID:       appID,
		Page:        page,
		Limit:       limit,
		Total:       total,
		TotalPages:  totalPages(total, limit),
		Items:       items,
		RefreshedAt: time.Now().UTC(),
	}, nil
}

func (r *RealtimeRepository) buildAppOnlineUser(ctx context.Context, appID int64, userID int64) (realtimedomain.AppOnlineUser, error) {
	connectionIDs, err := r.client.ZRevRange(ctx, r.userConnectionsKey(appID, userID), 0, 4).Result()
	if err != nil {
		return realtimedomain.AppOnlineUser{}, err
	}
	allConnections, err := r.client.ZCard(ctx, r.userConnectionsKey(appID, userID)).Result()
	if err != nil {
		return realtimedomain.AppOnlineUser{}, err
	}
	item := realtimedomain.AppOnlineUser{
		AppID:       appID,
		UserID:      userID,
		Connections: allConnections,
	}
	for _, connectionID := range connectionIDs {
		conn, err := r.GetConnection(ctx, connectionID)
		if err != nil {
			return realtimedomain.AppOnlineUser{}, err
		}
		if conn == nil {
			continue
		}
		if item.LastSeenAt.Before(conn.LastSeenAt) {
			item.LastSeenAt = conn.LastSeenAt
		}
		item.ConnectionSamples = append(item.ConnectionSamples, *conn)
	}
	if len(item.ConnectionSamples) > 0 {
		item.SampleConnection = &item.ConnectionSamples[0]
	}
	return item, nil
}

func (r *RealtimeRepository) cleanupExpiredUsers(ctx context.Context, now string) error {
	appUsers, err := r.client.ZRangeByScore(ctx, r.globalUsersKey(), &redislib.ZRangeBy{
		Min: "-inf",
		Max: now,
	}).Result()
	if err != nil {
		return err
	}
	for _, member := range appUsers {
		appID, userID, ok := parseGlobalUserMember(member)
		if !ok {
			continue
		}
		if err := r.refreshUserIndexes(ctx, appID, userID); err != nil {
			return err
		}
	}
	return nil
}

func (r *RealtimeRepository) refreshUserIndexes(ctx context.Context, appID int64, userID int64) error {
	key := r.userConnectionsKey(appID, userID)
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	if err := r.client.ZRemRangeByScore(ctx, key, "-inf", now).Err(); err != nil {
		return err
	}
	count, err := r.client.ZCard(ctx, key).Result()
	if err != nil {
		return err
	}
	userMember := strconv.FormatInt(userID, 10)
	globalUser := r.globalUserMember(appID, userID)
	if count == 0 {
		pipe := r.client.TxPipeline()
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, r.appUsersKey(appID), userMember)
		pipe.ZRem(ctx, r.globalUsersKey(), globalUser)
		_, err = pipe.Exec(ctx)
		return err
	}
	top, err := r.client.ZRevRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return err
	}
	if len(top) == 0 {
		return nil
	}
	score := top[0].Score
	pipe := r.client.TxPipeline()
	pipe.ZAdd(ctx, r.appUsersKey(appID), redislib.Z{Score: score, Member: userMember})
	pipe.ZAdd(ctx, r.globalUsersKey(), redislib.Z{Score: score, Member: globalUser})
	_, err = pipe.Exec(ctx)
	return err
}

func (r *RealtimeRepository) globalUsersKey() string {
	return fmt.Sprintf("%s:realtime:users", r.keyPrefix)
}

func (r *RealtimeRepository) globalConnectionsKey() string {
	return fmt.Sprintf("%s:realtime:connections", r.keyPrefix)
}

func (r *RealtimeRepository) appUsersKey(appID int64) string {
	return fmt.Sprintf("%s:realtime:app:%d:users", r.keyPrefix, appID)
}

func (r *RealtimeRepository) appConnectionsKey(appID int64) string {
	return fmt.Sprintf("%s:realtime:app:%d:connections", r.keyPrefix, appID)
}

func (r *RealtimeRepository) userConnectionsKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:realtime:app:%d:user:%d:connections", r.keyPrefix, appID, userID)
}

func (r *RealtimeRepository) connectionMetaKey(connectionID string) string {
	return fmt.Sprintf("%s:realtime:conn:%s", r.keyPrefix, connectionID)
}

func (r *RealtimeRepository) globalUserMember(appID int64, userID int64) string {
	return fmt.Sprintf("%d:%d", appID, userID)
}

func parseGlobalUserMember(value string) (int64, int64, bool) {
	left, right, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return 0, 0, false
	}
	appID, err := strconv.ParseInt(left, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(right, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return appID, userID, true
}

func totalPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	return pages
}
