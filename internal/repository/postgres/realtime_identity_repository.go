package postgres

import (
	"context"

	realtimedomain "aegis/internal/domain/realtime"
)

// FillOnlineUserIdentities 给在线用户列表补上账号与昵称。
//
// presence 存在 Redis 里，那里只有 userId —— 管理端表格要显示的是人，
// 不是一串数字。一次查询取回整页，不按行去查：在线列表一页最多 100 人，
// 逐个查就是 100 次往返，而这个接口是管理端轮询调用的。
//
// 查不到的行保持空账号（用户可能刚被删除，连接还没断），不报错：
// 少一个名字远好过整张表打不开。
func (r *Repository) FillOnlineUserIdentities(ctx context.Context, appID int64, items []realtimedomain.AppOnlineUser) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.UserID > 0 {
			ids = append(ids, item.UserID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := r.pool.Query(ctx, `
SELECT u.id, u.account, COALESCE(p.nickname, '')
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND u.id = ANY($2)`, appID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	type identity struct {
		account  string
		nickname string
	}
	found := make(map[int64]identity, len(ids))
	for rows.Next() {
		var id int64
		var value identity
		if err := rows.Scan(&id, &value.account, &value.nickname); err != nil {
			return err
		}
		found[id] = value
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range items {
		if value, ok := found[items[i].UserID]; ok {
			items[i].Account = value.account
			items[i].Nickname = value.nickname
		}
	}
	return nil
}
