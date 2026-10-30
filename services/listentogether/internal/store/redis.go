package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"

	"aegis/services/listentogether/internal/room"
)

// Redis 多实例共享的房间存储。
//
// 键（前缀默认 lt:）：
//
//	room:{app}:{id}    房间文档（JSON），乐观并发：WATCH → 读 → 改 → MULTI/EXEC
//	code:{app}:{code}  房间号 → 房间 ID（SETNX 占号）
//	user:{app}:{uid}   用户所在房间
//	rooms:{app}        有序集合，房间 ID 按最近活动时间排序（列表与巡检）
//	apps               有房间的应用
//	chat:{app}:{id}    最近聊天（列表，LTRIM 截断）
//	inst:{id}          实例心跳（带 TTL）；insts 为实例 ID 集合
//
// 房间文档不设 TTL：解散由巡检负责，巡检看得到的房间才能被清理。
type Redis struct {
	client *redis.Client
	prefix string
	locker *redislock.Client
}

// NewRedis 用已连接的客户端构造存储。
func NewRedis(client *redis.Client, prefix string) *Redis {
	return &Redis{client: client, prefix: prefix, locker: redislock.New(client)}
}

const maxUpdateAttempts = 16

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func (s *Redis) roomKey(app, id string) string   { return s.prefix + "room:" + app + ":" + id }
func (s *Redis) codeKey(app, code string) string { return s.prefix + "code:" + app + ":" + code }
func (s *Redis) userKey(app string, uid int64) string {
	return s.prefix + "user:" + app + ":" + itoa(uid)
}
func (s *Redis) roomsKey(app string) string     { return s.prefix + "rooms:" + app }
func (s *Redis) appsKey() string                { return s.prefix + "apps" }
func (s *Redis) chatKey(app, id string) string  { return s.prefix + "chat:" + app + ":" + id }
func (s *Redis) instKey(id string) string       { return s.prefix + "inst:" + id }
func (s *Redis) instsKey() string               { return s.prefix + "insts" }
func (s *Redis) lockKey(key string) string      { return s.prefix + "lock:" + key }

func (s *Redis) CreateRoom(ctx context.Context, r *room.Room) error {
	ok, err := s.client.SetNX(ctx, s.codeKey(r.AppKey, r.Code), r.ID, 0).Result()
	if err != nil {
		return err
	}
	if !ok {
		return room.ErrCodeTaken
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, s.roomKey(r.AppKey, r.ID), raw, 0)
		pipe.ZAdd(ctx, s.roomsKey(r.AppKey), redis.Z{Score: float64(r.UpdatedAt), Member: r.ID})
		pipe.SAdd(ctx, s.appsKey(), r.AppKey)
		return nil
	})
	if err != nil {
		_ = s.client.Del(ctx, s.codeKey(r.AppKey, r.Code)).Err()
	}
	return err
}

func (s *Redis) LoadRoom(ctx context.Context, app, id string) (*room.Room, error) {
	raw, err := s.client.Get(ctx, s.roomKey(app, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, room.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var r room.Room
	return &r, json.Unmarshal(raw, &r)
}

func (s *Redis) UpdateRoom(ctx context.Context, app, id string, fn func(*room.Room) error) (*room.Room, error) {
	key := s.roomKey(app, id)
	var result *room.Room
	for attempt := 0; attempt < maxUpdateAttempts; attempt++ {
		err := s.client.Watch(ctx, func(tx *redis.Tx) error {
			raw, err := tx.Get(ctx, key).Bytes()
			if errors.Is(err, redis.Nil) {
				return room.ErrNotFound
			}
			if err != nil {
				return err
			}
			var r room.Room
			if err := json.Unmarshal(raw, &r); err != nil {
				return err
			}
			if err := fn(&r); err != nil {
				return err
			}
			next, err := json.Marshal(&r)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, key, next, 0)
				pipe.ZAdd(ctx, s.roomsKey(app), redis.Z{Score: float64(r.UpdatedAt), Member: id})
				return nil
			})
			if err == nil {
				result = &r
			}
			return err
		}, key)
		if errors.Is(err, redis.TxFailedErr) {
			// 期间有别的实例改过，重新读再来；稍微错开避免一直撞车
			time.Sleep(time.Duration(attempt+1) * 3 * time.Millisecond)
			continue
		}
		return result, err
	}
	return nil, room.ErrConflict
}

// deleteUserRoomScript 只删仍指向这个房间的「用户所在房间」。
var deleteUserRoomScript = redis.NewScript(`
for i, key in ipairs(KEYS) do
  if redis.call('GET', key) == ARGV[1] then redis.call('DEL', key) end
end
return 1`)

func (s *Redis) DeleteRoom(ctx context.Context, r *room.Room) error {
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, s.roomKey(r.AppKey, r.ID), s.chatKey(r.AppKey, r.ID))
		pipe.ZRem(ctx, s.roomsKey(r.AppKey), r.ID)
		return nil
	})
	if err != nil {
		return err
	}
	_ = deleteIfEquals(ctx, s.client, s.codeKey(r.AppKey, r.Code), r.ID)
	if len(r.Members) > 0 {
		keys := make([]string, 0, len(r.Members))
		for _, member := range r.Members {
			keys = append(keys, s.userKey(r.AppKey, member.UserID))
		}
		if err := deleteUserRoomScript.Run(ctx, s.client, keys, r.ID).Err(); err != nil {
			return err
		}
	}
	return nil
}

func deleteIfEquals(ctx context.Context, client *redis.Client, key, value string) error {
	return deleteUserRoomScript.Run(ctx, client, []string{key}, value).Err()
}

func (s *Redis) FindByCode(ctx context.Context, app, code string) (string, error) {
	id, err := s.client.Get(ctx, s.codeKey(app, code)).Result()
	if errors.Is(err, redis.Nil) {
		return "", room.ErrNotFound
	}
	return id, err
}

func (s *Redis) UserRoom(ctx context.Context, app string, userID int64) (string, error) {
	id, err := s.client.Get(ctx, s.userKey(app, userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return id, err
}

func (s *Redis) SetUserRoom(ctx context.Context, app string, userID int64, roomID string) error {
	return s.client.Set(ctx, s.userKey(app, userID), roomID, 0).Err()
}

func (s *Redis) ClearUserRoom(ctx context.Context, app string, userID int64, roomID string) error {
	return deleteIfEquals(ctx, s.client, s.userKey(app, userID), roomID)
}

func (s *Redis) ListRooms(ctx context.Context, app string, offset, limit int) ([]string, int, error) {
	total, err := s.client.ZCard(ctx, s.roomsKey(app)).Result()
	if err != nil {
		return nil, 0, err
	}
	ids, err := s.client.ZRevRange(ctx, s.roomsKey(app), int64(offset), int64(offset+limit-1)).Result()
	return ids, int(total), err
}

func (s *Redis) Apps(ctx context.Context) ([]string, error) {
	apps, err := s.client.SMembers(ctx, s.appsKey()).Result()
	if err != nil {
		return nil, err
	}
	// 顺手清掉已经没有房间的应用
	kept := apps[:0]
	for _, app := range apps {
		count, err := s.client.ZCard(ctx, s.roomsKey(app)).Result()
		if err == nil && count == 0 {
			_ = s.client.SRem(ctx, s.appsKey(), app).Err()
			continue
		}
		kept = append(kept, app)
	}
	return kept, nil
}

func (s *Redis) AppendChat(ctx context.Context, app, roomID string, msg room.ChatMessage, keep int) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	key := s.chatKey(app, roomID)
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.RPush(ctx, key, raw)
		pipe.LTrim(ctx, key, int64(-keep), -1)
		pipe.Expire(ctx, key, 48*time.Hour)
		return nil
	})
	return err
}

func (s *Redis) RecentChat(ctx context.Context, app, roomID string, limit int) ([]room.ChatMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	items, err := s.client.LRange(ctx, s.chatKey(app, roomID), int64(-limit), -1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]room.ChatMessage, 0, len(items))
	for _, item := range items {
		var msg room.ChatMessage
		if json.Unmarshal([]byte(item), &msg) == nil {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (s *Redis) Heartbeat(ctx context.Context, info room.InstanceInfo, ttl time.Duration) error {
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	_, err = s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, s.instKey(info.ID), raw, ttl)
		pipe.SAdd(ctx, s.instsKey(), info.ID)
		return nil
	})
	return err
}

func (s *Redis) Instances(ctx context.Context) ([]room.InstanceInfo, error) {
	ids, err := s.client.SMembers(ctx, s.instsKey()).Result()
	if err != nil {
		return nil, err
	}
	out := make([]room.InstanceInfo, 0, len(ids))
	for _, id := range ids {
		raw, err := s.client.Get(ctx, s.instKey(id)).Bytes()
		if errors.Is(err, redis.Nil) {
			_ = s.client.SRem(ctx, s.instsKey(), id).Err()
			continue
		}
		if err != nil {
			return nil, err
		}
		var info room.InstanceInfo
		if json.Unmarshal(raw, &info) == nil {
			out = append(out, info)
		}
	}
	return out, nil
}

func (s *Redis) RemoveInstance(ctx context.Context, id string) error {
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, s.instKey(id))
		pipe.SRem(ctx, s.instsKey(), id)
		return nil
	})
	return err
}

func (s *Redis) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	lock, err := s.locker.Obtain(ctx, s.lockKey(key), ttl, nil)
	if errors.Is(err, redislock.ErrNotObtained) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return func() { _ = lock.Release(context.Background()) }, true, nil
}

var _ room.Repository = (*Redis)(nil)
