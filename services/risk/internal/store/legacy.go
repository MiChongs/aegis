package store

import (
	"context"
	"fmt"
	"time"

	"aegis/services/risk/internal/lists"

	"github.com/jackc/pgx/v5"
)

// LegacyLists 读主库里旧风控中心的名单（risk_lists，仍有效的那些）。表不存在时返回空。
// 条目 ID 用 legacy:<旧 ID>，重复导入覆盖同一条，不会重复。
func LegacyLists(ctx context.Context, mainDSN string) ([]lists.Entry, error) {
	if mainDSN == "" {
		return nil, nil
	}
	conn, err := pgx.Connect(ctx, mainDSN)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('public.risk_lists') IS NOT NULL").Scan(&exists); err != nil || !exists {
		return nil, err
	}
	rows, err := conn.Query(ctx, `SELECT id, coalesce(app_id, 0), list_type, value, kind, coalesce(scenes, '{}'), coalesce(reason, ''),
		coalesce(source, ''), coalesce(created_by, 0), expires_at, created_at
		FROM risk_lists WHERE expires_at IS NULL OR expires_at > now() ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lists.Entry
	for rows.Next() {
		var (
			id, appID, createdBy int64
			e                    lists.Entry
			scenes               []string
			source               string
			exp                  *time.Time
		)
		if err := rows.Scan(&id, &appID, &e.ListType, &e.Value, &e.Kind, &scenes, &e.Reason, &source, &createdBy, &exp, &e.CreatedAt); err != nil {
			return nil, err
		}
		norm, err := lists.Normalize(e.ListType, e.Value)
		if err != nil {
			continue
		}
		e.ID = fmt.Sprintf("legacy:%d", id)
		e.Value, e.AppID, e.ExpiresAt = norm, appID, exp
		for _, s := range scenes {
			if s != "*" && s != "" {
				e.Scenes = append(e.Scenes, s)
			}
		}
		e.Source = "import"
		if source != "" {
			e.Reason = fmt.Sprintf("%s（旧风控中心 %s）", e.Reason, source)
		}
		if createdBy > 0 {
			e.CreatedBy = fmt.Sprintf("admin:%d", createdBy)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
