package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	realtimedomain "aegis/internal/domain/realtime"
	redislib "github.com/redis/go-redis/v9"
)

// RealtimeRepository 实时连接的在线记录。
//
// 每条连接一份元数据（JSON），外加几组有序集合做索引，分数都是「过期时刻」
// （Unix 秒）：心跳把分数往后推，进程崩溃留下的连接到点自然失效。
//
// 读路径一律按分数计数（ZCOUNT now +inf），不在读的时候顺手清理 —— 旧实现
// 每次统计都先全量扫一遍过期项，管理端十秒一轮询，人一多就是 Redis 的热点。
// 过期项交给 CleanupExpired 由后台节拍统一收拾。
//
// 两个互不相交的空间：
//
//	用户空间  realtime:connections / users / apps / app:N:*      —— 应用用户的连接
//	管理员空间 realtime:admin:connections / admins / admin:N:*    —— 开着控制台的管理员
//
// 管理员的连接 AppID 为 0，只进管理员空间，不计入任何用户统计。
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

// AppCount 一个应用的在线人数与连接数。
type AppCount struct {
	AppID       int64
	Users       int64
	Connections int64
}

// AdminConnections 一位管理员当前的控制台连接。
type AdminConnections struct {
	AdminID     int64
	Connections []realtimedomain.PresenceConnection
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
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, r.connectionMetaKey(conn.ConnectionID), data, metaTTL)
	if conn.AppID == 0 {
		adminMember := strconv.FormatInt(conn.UserID, 10)
		pipe.ZAdd(ctx, r.adminConnectionsKey(), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
		pipe.ZAdd(ctx, r.adminOwnConnectionsKey(conn.UserID), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
		pipe.ZAddGT(ctx, r.adminsKey(), redislib.Z{Score: expireAt, Member: adminMember})
		_, err = pipe.Exec(ctx)
		return err
	}
	userMember := strconv.FormatInt(conn.UserID, 10)
	pipe.ZAdd(ctx, r.globalConnectionsKey(), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	pipe.ZAdd(ctx, r.appConnectionsKey(conn.AppID), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	pipe.ZAdd(ctx, r.userConnectionsKey(conn.AppID, conn.UserID), redislib.Z{Score: expireAt, Member: conn.ConnectionID})
	// 用户与应用的分数取其名下连接里最晚的过期时刻：GT 保证一条连接的心跳
	// 不会把另一条更晚到期的连接撑起来的分数压低。
	pipe.ZAddGT(ctx, r.globalUsersKey(), redislib.Z{Score: expireAt, Member: r.globalUserMember(conn.AppID, conn.UserID)})
	pipe.ZAddGT(ctx, r.appUsersKey(conn.AppID), redislib.Z{Score: expireAt, Member: userMember})
	pipe.ZAddGT(ctx, r.appsKey(), redislib.Z{Score: expireAt, Member: strconv.FormatInt(conn.AppID, 10)})
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

// GetConnections 批量取元数据，顺序与入参一致；已过期或解析失败的位置为 nil。
func (r *RealtimeRepository) GetConnections(ctx context.Context, connectionIDs []string) ([]*realtimedomain.PresenceConnection, error) {
	out := make([]*realtimedomain.PresenceConnection, len(connectionIDs))
	const batch = 500
	for start := 0; start < len(connectionIDs); start += batch {
		end := min(start+batch, len(connectionIDs))
		keys := make([]string, 0, end-start)
		for _, id := range connectionIDs[start:end] {
			keys = append(keys, r.connectionMetaKey(id))
		}
		values, err := r.client.MGet(ctx, keys...).Result()
		if err != nil {
			return nil, err
		}
		for i, value := range values {
			raw, ok := value.(string)
			if !ok || raw == "" {
				continue
			}
			var conn realtimedomain.PresenceConnection
			if err := json.Unmarshal([]byte(raw), &conn); err != nil {
				continue
			}
			out[start+i] = &conn
		}
	}
	return out, nil
}

func (r *RealtimeRepository) RemoveConnection(ctx context.Context, appID int64, userID int64, connectionID string) error {
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, r.connectionMetaKey(connectionID))
	if appID == 0 {
		pipe.ZRem(ctx, r.adminConnectionsKey(), connectionID)
		pipe.ZRem(ctx, r.adminOwnConnectionsKey(userID), connectionID)
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
		return r.refreshAdminIndex(ctx, userID)
	}
	pipe.ZRem(ctx, r.globalConnectionsKey(), connectionID)
	pipe.ZRem(ctx, r.appConnectionsKey(appID), connectionID)
	pipe.ZRem(ctx, r.userConnectionsKey(appID, userID), connectionID)
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	return r.refreshUserIndexes(ctx, appID, userID)
}

// RemoveUserConnections 一个用户的全部在线连接（账号删除后不该再算在线人数里）；返回删掉的连接数
func (r *RealtimeRepository) RemoveUserConnections(ctx context.Context, appID int64, userID int64) (int, error) {
	ids, err := r.client.ZRange(ctx, r.userConnectionsKey(appID, userID), 0, -1).Result()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := r.RemoveConnection(ctx, appID, userID, id); err != nil {
			return 0, err
		}
	}
	return len(ids), r.refreshUserIndexes(ctx, appID, userID)
}

// CleanupExpired 收拾到点未续期的连接（进程崩溃、网络中断没走正常关闭），
// 返回被清掉的连接，调用方据此补发下线事件。
func (r *RealtimeRepository) CleanupExpired(ctx context.Context) ([]realtimedomain.PresenceConnection, error) {
	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	var removed []realtimedomain.PresenceConnection
	for _, key := range []string{r.globalConnectionsKey(), r.adminConnectionsKey()} {
		connectionIDs, err := r.client.ZRangeByScore(ctx, key, &redislib.ZRangeBy{Min: "-inf", Max: now, Count: 2000}).Result()
		if err != nil {
			return removed, err
		}
		if len(connectionIDs) == 0 {
			continue
		}
		metas, err := r.GetConnections(ctx, connectionIDs)
		if err != nil {
			return removed, err
		}
		for i, connectionID := range connectionIDs {
			conn := metas[i]
			if conn == nil {
				// 元数据已先于索引过期，只能摘掉全局索引；用户与应用索引
				// 由下面按分数的整理兜住。
				if err := r.client.ZRem(ctx, key, connectionID).Err(); err != nil {
					return removed, err
				}
				continue
			}
			if err := r.RemoveConnection(ctx, conn.AppID, conn.UserID, connectionID); err != nil {
				return removed, err
			}
			removed = append(removed, *conn)
		}
	}
	if err := r.cleanupExpiredUsers(ctx, now); err != nil {
		return removed, err
	}
	if err := r.cleanupExpiredAdmins(ctx, now); err != nil {
		return removed, err
	}
	if err := r.client.ZRemRangeByScore(ctx, r.appsKey(), "-inf", now).Err(); err != nil {
		return removed, err
	}
	return removed, nil
}

func (r *RealtimeRepository) OnlineStats(ctx context.Context) (*realtimedomain.OnlineStats, error) {
	now := nowScore()
	users, connections, err := r.countPair(ctx, r.globalUsersKey(), r.globalConnectionsKey(), now)
	if err != nil {
		return nil, err
	}
	apps, err := r.ActiveApps(ctx)
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
	users, connections, err := r.countPair(ctx, r.appUsersKey(appID), r.appConnectionsKey(appID), nowScore())
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

// ActiveApps 当前有在线用户的应用，按在线人数降序。
func (r *RealtimeRepository) ActiveApps(ctx context.Context) ([]AppCount, error) {
	now := nowScore()
	members, err := r.client.ZRangeByScore(ctx, r.appsKey(), &redislib.ZRangeBy{Min: now, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}
	pipe := r.client.Pipeline()
	type pending struct {
		appID int64
		users *redislib.IntCmd
		conns *redislib.IntCmd
	}
	queued := make([]pending, 0, len(members))
	for _, member := range members {
		appID, err := strconv.ParseInt(member, 10, 64)
		if err != nil || appID <= 0 {
			continue
		}
		queued = append(queued, pending{
			appID: appID,
			users: pipe.ZCount(ctx, r.appUsersKey(appID), now, "+inf"),
			conns: pipe.ZCount(ctx, r.appConnectionsKey(appID), now, "+inf"),
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redislib.Nil {
		return nil, err
	}
	out := make([]AppCount, 0, len(queued))
	for _, item := range queued {
		users := item.users.Val()
		if users <= 0 {
			continue
		}
		out = append(out, AppCount{AppID: item.appID, Users: users, Connections: item.conns.Val()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Users != out[j].Users {
			return out[i].Users > out[j].Users
		}
		return out[i].AppID < out[j].AppID
	})
	return out, nil
}

// GlobalCounts 全站在线用户与连接数。
func (r *RealtimeRepository) GlobalCounts(ctx context.Context) (int64, int64, error) {
	return r.countPair(ctx, r.globalUsersKey(), r.globalConnectionsKey(), nowScore())
}

// AdminCounts 控制台在线的管理员人数与连接数。
func (r *RealtimeRepository) AdminCounts(ctx context.Context) (int64, int64, error) {
	return r.countPair(ctx, r.adminsKey(), r.adminConnectionsKey(), nowScore())
}

// ScanConnections 遍历在线的用户连接，最多 limit 条；返回在线连接总数。
// 按过期时刻倒序取，截断时保留的是最近仍在心跳的那一批。
func (r *RealtimeRepository) ScanConnections(ctx context.Context, limit int, fn func(realtimedomain.PresenceConnection)) (int64, error) {
	return r.scanSpace(ctx, r.globalConnectionsKey(), limit, fn)
}

// ScanAppConnections 遍历某应用下在线的连接。
func (r *RealtimeRepository) ScanAppConnections(ctx context.Context, appID int64, limit int, fn func(realtimedomain.PresenceConnection)) (int64, error) {
	return r.scanSpace(ctx, r.appConnectionsKey(appID), limit, fn)
}

// ScanAdminConnections 遍历在线的管理员连接。
func (r *RealtimeRepository) ScanAdminConnections(ctx context.Context, limit int, fn func(realtimedomain.PresenceConnection)) (int64, error) {
	return r.scanSpace(ctx, r.adminConnectionsKey(), limit, fn)
}

func (r *RealtimeRepository) scanSpace(ctx context.Context, key string, limit int, fn func(realtimedomain.PresenceConnection)) (int64, error) {
	now := nowScore()
	total, err := r.client.ZCount(ctx, key, now, "+inf").Result()
	if err != nil {
		return 0, err
	}
	if total == 0 || limit <= 0 {
		return total, nil
	}
	const page = 2000
	var offset int64
	for offset < int64(limit) {
		count := min(int64(page), int64(limit)-offset)
		ids, err := r.client.ZRevRangeByScore(ctx, key, &redislib.ZRangeBy{Min: now, Max: "+inf", Offset: offset, Count: count}).Result()
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			break
		}
		metas, err := r.GetConnections(ctx, ids)
		if err != nil {
			return total, err
		}
		for _, conn := range metas {
			if conn != nil {
				fn(*conn)
			}
		}
		offset += int64(len(ids))
		if int64(len(ids)) < count {
			break
		}
	}
	return total, nil
}

// ListAdminPresence 控制台在线的管理员及各自的连接，按最近心跳降序。
func (r *RealtimeRepository) ListAdminPresence(ctx context.Context) ([]AdminConnections, error) {
	now := nowScore()
	members, err := r.client.ZRevRangeByScore(ctx, r.adminsKey(), &redislib.ZRangeBy{Min: now, Max: "+inf"}).Result()
	if err != nil {
		return nil, err
	}
	out := make([]AdminConnections, 0, len(members))
	for _, member := range members {
		adminID, err := strconv.ParseInt(member, 10, 64)
		if err != nil || adminID <= 0 {
			continue
		}
		ids, err := r.client.ZRevRangeByScore(ctx, r.adminOwnConnectionsKey(adminID), &redislib.ZRangeBy{Min: now, Max: "+inf", Count: 50}).Result()
		if err != nil {
			return nil, err
		}
		metas, err := r.GetConnections(ctx, ids)
		if err != nil {
			return nil, err
		}
		item := AdminConnections{AdminID: adminID}
		for _, conn := range metas {
			if conn != nil {
				item.Connections = append(item.Connections, *conn)
			}
		}
		if len(item.Connections) > 0 {
			out = append(out, item)
		}
	}
	return out, nil
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
	now := nowScore()
	total, err := r.client.ZCount(ctx, r.appUsersKey(appID), now, "+inf").Result()
	if err != nil {
		return nil, err
	}
	offset := int64((page - 1) * limit)
	userIDs, err := r.client.ZRevRangeByScore(ctx, r.appUsersKey(appID), &redislib.ZRangeBy{Min: now, Max: "+inf", Offset: offset, Count: int64(limit)}).Result()
	if err != nil {
		return nil, err
	}
	items, err := r.BuildAppOnlineUsers(ctx, appID, parseIDs(userIDs))
	if err != nil {
		return nil, err
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

// ListAppOnlineUserIDs 应用当前在线的用户 ID，按最近心跳降序，最多 limit 个。
// 关键字搜索要在全部在线用户里找人，分页接口给不了这个范围。
func (r *RealtimeRepository) ListAppOnlineUserIDs(ctx context.Context, appID int64, limit int) ([]int64, error) {
	ids, err := r.client.ZRevRangeByScore(ctx, r.appUsersKey(appID), &redislib.ZRangeBy{Min: nowScore(), Max: "+inf", Count: int64(limit)}).Result()
	if err != nil {
		return nil, err
	}
	return parseIDs(ids), nil
}

// BuildAppOnlineUsers 按给定顺序组装在线用户行；已经下线的用户跳过。
func (r *RealtimeRepository) BuildAppOnlineUsers(ctx context.Context, appID int64, userIDs []int64) ([]realtimedomain.AppOnlineUser, error) {
	items := make([]realtimedomain.AppOnlineUser, 0, len(userIDs))
	for _, userID := range userIDs {
		item, err := r.buildAppOnlineUser(ctx, appID, userID)
		if err != nil {
			return nil, err
		}
		if item.Connections == 0 {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *RealtimeRepository) buildAppOnlineUser(ctx context.Context, appID int64, userID int64) (realtimedomain.AppOnlineUser, error) {
	now := nowScore()
	key := r.userConnectionsKey(appID, userID)
	connectionIDs, err := r.client.ZRevRangeByScore(ctx, key, &redislib.ZRangeBy{Min: now, Max: "+inf", Count: 5}).Result()
	if err != nil {
		return realtimedomain.AppOnlineUser{}, err
	}
	allConnections, err := r.client.ZCount(ctx, key, now, "+inf").Result()
	if err != nil {
		return realtimedomain.AppOnlineUser{}, err
	}
	item := realtimedomain.AppOnlineUser{
		AppID:       appID,
		UserID:      userID,
		Connections: allConnections,
	}
	metas, err := r.GetConnections(ctx, connectionIDs)
	if err != nil {
		return realtimedomain.AppOnlineUser{}, err
	}
	for _, conn := range metas {
		if conn == nil {
			continue
		}
		if item.LastSeenAt.Before(conn.LastSeenAt) {
			item.LastSeenAt = conn.LastSeenAt
		}
		// 最早的一条连接才是"在线起始时间"；取最新的会让一次断线重连
		// 把在线时长清零。
		if item.ConnectedAt.IsZero() || conn.ConnectedAt.Before(item.ConnectedAt) {
			item.ConnectedAt = conn.ConnectedAt
		}
		// IP 取第一条有值的：同一用户的多条连接可能来自不同网络，
		// 表格只显示一个，取哪条都行，但不能因为某条没记到就留空。
		if item.IP == "" {
			item.IP = conn.IP
			item.Location = conn.Location()
		}
		if item.Account == "" {
			item.Account = conn.Account
		}
		item.ConnectionSamples = append(item.ConnectionSamples, *conn)
	}
	if len(item.ConnectionSamples) > 0 {
		item.SampleConnection = &item.ConnectionSamples[0]
	}
	return item, nil
}

// UserConnectionIDs 某用户在某应用下的全部在线连接，用于强制下线时逐条断开。
func (r *RealtimeRepository) UserConnectionIDs(ctx context.Context, appID int64, userID int64) ([]string, error) {
	return r.client.ZRange(ctx, r.userConnectionsKey(appID, userID), 0, -1).Result()
}

// ── 在线曲线 ──

// WriteSample 写入一分钟的采样点。按 UTC 日分桶，每桶一个哈希，到期整桶删除。
func (r *RealtimeRepository) WriteSample(ctx context.Context, point realtimedomain.SeriesPoint, apps []AppCount, retention time.Duration) error {
	at := time.Unix(point.At, 0).UTC()
	day := at.Format("20060102")
	field := strconv.FormatInt(point.At, 10)
	expire := retention + 24*time.Hour
	pipe := r.client.Pipeline()
	globalKey := r.seriesGlobalKey(day)
	pipe.HSet(ctx, globalKey, field, fmt.Sprintf("%d,%d,%d", point.Users, point.Connections, point.Admins))
	pipe.Expire(ctx, globalKey, expire)
	for _, app := range apps {
		key := r.seriesAppKey(app.AppID, day)
		pipe.HSet(ctx, key, field, fmt.Sprintf("%d,%d", app.Users, app.Connections))
		pipe.Expire(ctx, key, expire)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// ReadSeries 读 [from, to] 内的采样点，按时间升序。appID 为 0 时读全站。
// 某分钟没有该应用的记录表示那一分钟它没有在线用户，补成 0 交给调用方决定是否填点。
func (r *RealtimeRepository) ReadSeries(ctx context.Context, appID int64, from, to time.Time) ([]realtimedomain.SeriesPoint, error) {
	from, to = from.UTC(), to.UTC()
	if to.Before(from) {
		return nil, nil
	}
	pipe := r.client.Pipeline()
	var cmds []*redislib.MapStringStringCmd
	for day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC); !day.After(to); day = day.AddDate(0, 0, 1) {
		stamp := day.Format("20060102")
		key := r.seriesGlobalKey(stamp)
		if appID > 0 {
			key = r.seriesAppKey(appID, stamp)
		}
		cmds = append(cmds, pipe.HGetAll(ctx, key))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redislib.Nil {
		return nil, err
	}
	fromUnix, toUnix := from.Unix(), to.Unix()
	var out []realtimedomain.SeriesPoint
	for _, cmd := range cmds {
		for field, value := range cmd.Val() {
			at, err := strconv.ParseInt(field, 10, 64)
			if err != nil || at < fromUnix || at > toUnix {
				continue
			}
			parts := strings.Split(value, ",")
			point := realtimedomain.SeriesPoint{At: at}
			if len(parts) > 0 {
				point.Users, _ = strconv.ParseInt(parts[0], 10, 64)
			}
			if len(parts) > 1 {
				point.Connections, _ = strconv.ParseInt(parts[1], 10, 64)
			}
			if len(parts) > 2 {
				point.Admins, _ = strconv.ParseInt(parts[2], 10, 64)
			}
			out = append(out, point)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At })
	return out, nil
}

// ── 集群协作：快照锁与快照缓存 ──

// TryLock 抢一把带过期的锁，抢到返回 true。用于让集群里只有一个实例算快照、写采样。
func (r *RealtimeRepository) TryLock(ctx context.Context, name string, ttl time.Duration) (bool, error) {
	return r.client.SetNX(ctx, r.lockKey(name), "1", ttl).Result()
}

// StoreSnapshot 写入最新快照，TTL 到期即视为没有快照。
func (r *RealtimeRepository) StoreSnapshot(ctx context.Context, raw []byte, ttl time.Duration) error {
	return r.client.Set(ctx, r.snapshotKey(), raw, ttl).Err()
}

// LoadSnapshot 读取最新快照，没有时返回 nil。
func (r *RealtimeRepository) LoadSnapshot(ctx context.Context) ([]byte, error) {
	raw, err := r.client.Get(ctx, r.snapshotKey()).Bytes()
	if err == redislib.Nil {
		return nil, nil
	}
	return raw, err
}

// NextSnapshotVersion 快照的单调版本号，订阅者据此判断有没有新快照。
func (r *RealtimeRepository) NextSnapshotVersion(ctx context.Context) (int64, error) {
	return r.client.Incr(ctx, r.snapshotKey()+":version").Result()
}

// ── 内部 ──

func (r *RealtimeRepository) countPair(ctx context.Context, usersKey, connectionsKey, now string) (int64, int64, error) {
	pipe := r.client.Pipeline()
	users := pipe.ZCount(ctx, usersKey, now, "+inf")
	conns := pipe.ZCount(ctx, connectionsKey, now, "+inf")
	if _, err := pipe.Exec(ctx); err != nil && err != redislib.Nil {
		return 0, 0, err
	}
	return users.Val(), conns.Val(), nil
}

func (r *RealtimeRepository) cleanupExpiredUsers(ctx context.Context, now string) error {
	appUsers, err := r.client.ZRangeByScore(ctx, r.globalUsersKey(), &redislib.ZRangeBy{Min: "-inf", Max: now, Count: 2000}).Result()
	if err != nil {
		return err
	}
	for _, member := range appUsers {
		appID, userID, ok := parseGlobalUserMember(member)
		if !ok {
			if err := r.client.ZRem(ctx, r.globalUsersKey(), member).Err(); err != nil {
				return err
			}
			continue
		}
		if err := r.refreshUserIndexes(ctx, appID, userID); err != nil {
			return err
		}
	}
	return nil
}

func (r *RealtimeRepository) cleanupExpiredAdmins(ctx context.Context, now string) error {
	members, err := r.client.ZRangeByScore(ctx, r.adminsKey(), &redislib.ZRangeBy{Min: "-inf", Max: now, Count: 500}).Result()
	if err != nil {
		return err
	}
	for _, member := range members {
		adminID, err := strconv.ParseInt(member, 10, 64)
		if err != nil {
			if err := r.client.ZRem(ctx, r.adminsKey(), member).Err(); err != nil {
				return err
			}
			continue
		}
		if err := r.refreshAdminIndex(ctx, adminID); err != nil {
			return err
		}
	}
	return nil
}

func (r *RealtimeRepository) refreshUserIndexes(ctx context.Context, appID int64, userID int64) error {
	key := r.userConnectionsKey(appID, userID)
	score, alive, err := r.trimAndTop(ctx, key)
	if err != nil {
		return err
	}
	userMember := strconv.FormatInt(userID, 10)
	globalUser := r.globalUserMember(appID, userID)
	pipe := r.client.TxPipeline()
	if !alive {
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, r.appUsersKey(appID), userMember)
		pipe.ZRem(ctx, r.globalUsersKey(), globalUser)
	} else {
		// 这里刻意不用 GT：刚摘掉的那条连接可能正是把分数撑高的那条
		pipe.ZAdd(ctx, r.appUsersKey(appID), redislib.Z{Score: score, Member: userMember})
		pipe.ZAdd(ctx, r.globalUsersKey(), redislib.Z{Score: score, Member: globalUser})
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *RealtimeRepository) refreshAdminIndex(ctx context.Context, adminID int64) error {
	key := r.adminOwnConnectionsKey(adminID)
	score, alive, err := r.trimAndTop(ctx, key)
	if err != nil {
		return err
	}
	member := strconv.FormatInt(adminID, 10)
	if !alive {
		pipe := r.client.TxPipeline()
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, r.adminsKey(), member)
		_, err = pipe.Exec(ctx)
		return err
	}
	return r.client.ZAdd(ctx, r.adminsKey(), redislib.Z{Score: score, Member: member}).Err()
}

// trimAndTop 删掉集合里已过期的成员，返回剩余成员里最晚的过期时刻。
func (r *RealtimeRepository) trimAndTop(ctx context.Context, key string) (float64, bool, error) {
	if err := r.client.ZRemRangeByScore(ctx, key, "-inf", nowScore()).Err(); err != nil {
		return 0, false, err
	}
	top, err := r.client.ZRevRangeWithScores(ctx, key, 0, 0).Result()
	if err != nil {
		return 0, false, err
	}
	if len(top) == 0 {
		return 0, false, nil
	}
	return top[0].Score, true, nil
}

func (r *RealtimeRepository) globalUsersKey() string {
	return fmt.Sprintf("%s:realtime:users", r.keyPrefix)
}

func (r *RealtimeRepository) globalConnectionsKey() string {
	return fmt.Sprintf("%s:realtime:connections", r.keyPrefix)
}

func (r *RealtimeRepository) appsKey() string {
	return fmt.Sprintf("%s:realtime:apps", r.keyPrefix)
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

func (r *RealtimeRepository) adminConnectionsKey() string {
	return fmt.Sprintf("%s:realtime:admin:connections", r.keyPrefix)
}

func (r *RealtimeRepository) adminsKey() string {
	return fmt.Sprintf("%s:realtime:admin:admins", r.keyPrefix)
}

func (r *RealtimeRepository) adminOwnConnectionsKey(adminID int64) string {
	return fmt.Sprintf("%s:realtime:admin:%d:connections", r.keyPrefix, adminID)
}

func (r *RealtimeRepository) connectionMetaKey(connectionID string) string {
	return fmt.Sprintf("%s:realtime:conn:%s", r.keyPrefix, connectionID)
}

func (r *RealtimeRepository) seriesGlobalKey(day string) string {
	return fmt.Sprintf("%s:realtime:series:g:%s", r.keyPrefix, day)
}

func (r *RealtimeRepository) seriesAppKey(appID int64, day string) string {
	return fmt.Sprintf("%s:realtime:series:a:%d:%s", r.keyPrefix, appID, day)
}

func (r *RealtimeRepository) snapshotKey() string {
	return fmt.Sprintf("%s:realtime:snapshot", r.keyPrefix)
}

func (r *RealtimeRepository) lockKey(name string) string {
	return fmt.Sprintf("%s:realtime:lock:%s", r.keyPrefix, name)
}

func (r *RealtimeRepository) globalUserMember(appID int64, userID int64) string {
	return fmt.Sprintf("%d:%d", appID, userID)
}

func nowScore() string {
	return strconv.FormatInt(time.Now().UTC().Unix(), 10)
}

func parseIDs(values []string) []int64 {
	out := make([]int64, 0, len(values))
	for _, raw := range values {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
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
