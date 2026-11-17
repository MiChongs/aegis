package backfill

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DeletedCandidates 主库里已经不存在的账号 ID（用户 ID 是全局自增，删掉的账号在序列里留下空缺）与全部应用 ID。
// 按 ID 顺序流式读，只在内存里留空缺；序列取 last_value，最新的几个账号被删了也能找到。
// 序列里因插入失败而跳过的号也会出现在结果里，调用方再到关系图里核对是否真的留有这个账号。
func DeletedCandidates(ctx context.Context, conn *pgx.Conn) (ids []int64, apps []int64, err error) {
	var prev int64
	_, err = query(ctx, conn, `SELECT id FROM users ORDER BY id`, func(row pgx.Rows) error {
		var id int64
		if err := row.Scan(&id); err != nil {
			return err
		}
		for v := prev + 1; v < id; v++ {
			ids = append(ids, v)
		}
		prev = id
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var seq string
	if err := conn.QueryRow(ctx, `SELECT coalesce(pg_get_serial_sequence('users', 'id'), '')`).Scan(&seq); err == nil && seq != "" {
		var last int64
		if err := conn.QueryRow(ctx, fmt.Sprintf(`SELECT last_value FROM %s`, seq)).Scan(&last); err == nil {
			for v := prev + 1; v <= last; v++ {
				ids = append(ids, v)
			}
		}
	}
	_, err = query(ctx, conn, `SELECT id FROM apps UNION SELECT DISTINCT appid FROM users`, func(row pgx.Rows) error {
		var id int64
		if err := row.Scan(&id); err != nil {
			return err
		}
		if id > 0 {
			apps = append(apps, id)
		}
		return nil
	})
	return ids, apps, err
}

// Existing 这些账号里此刻还在主库的（清扫删点前最后核对一次，防止和新注册赛跑）
func Existing(ctx context.Context, conn *pgx.Conn, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := conn.Query(ctx, `SELECT id FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
