package redis

import (
	"context"
	"fmt"

	redislib "github.com/redis/go-redis/v9"
)

// AppSessionSweepResult 一次应用级会话清扫的结果。
type AppSessionSweepResult struct {
	// Users 被清扫的用户数（有在线会话的用户）
	Users int
	// Sessions 被删除的访问会话数
	Sessions int
}

// PurgeAppSessions 删除某应用下全部用户的访问会话。
//
// 平台冻结 / 封禁一个应用时用它把在线用户立刻踢下线。判定层（BlockAPI）本身已经能挡住
// 后续请求，这里再删一遍是为了让"已经登录的人"当场掉线而不是等下一次请求被拒 ——
// 长连接与本地缓存会让"下一次请求"来得比想象中晚。
//
// 用 SCAN 而不是 KEYS：应用规模大时 KEYS 会阻塞整个 Redis。
func (r *SessionRepository) PurgeAppSessions(ctx context.Context, appID int64) (AppSessionSweepResult, error) {
	result := AppSessionSweepResult{}
	pattern := fmt.Sprintf("%s:auth:user-sessions:%d:*", r.keyPrefix, appID)

	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return result, err
		}
		for _, indexKey := range keys {
			hashes, err := r.client.ZRange(ctx, indexKey, 0, -1).Result()
			if err != nil {
				if err == redislib.Nil {
					continue
				}
				return result, err
			}
			pipe := r.client.TxPipeline()
			for _, tokenHash := range hashes {
				pipe.Del(ctx, r.sessionKeyByHash(tokenHash))
			}
			pipe.Del(ctx, indexKey)
			if _, err := pipe.Exec(ctx); err != nil {
				return result, err
			}
			result.Users++
			result.Sessions += len(hashes)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return result, nil
}
