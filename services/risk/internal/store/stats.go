package store

import (
	"context"
	"time"
)

// CaseStats 复核单全局统计（控制台复核页页头用；不受分页影响）
type CaseStats struct {
	Pending int64 `json:"pending"`
	// PendingByLevel 待复核按风险分段：critical ≥ 80、high 60–79、medium 30–59、low < 30
	PendingByLevel map[string]int64 `json:"pendingByLevel"`
	// OldestPendingAt 等得最久的一张待复核单的创建时间
	OldestPendingAt *time.Time `json:"oldestPendingAt,omitempty"`
	// Closed30d 近 30 天结案数，按结论（approve 判定为正常 / reject 确认有风险）
	Closed30d map[string]int64 `json:"closed30d"`
}

func (s *Store) CaseStats(ctx context.Context, appID *int64) (*CaseStats, error) {
	appCond, args := "", []any{}
	if appID != nil {
		appCond, args = " AND app_id = $1", []any{*appID}
	}
	out := &CaseStats{PendingByLevel: map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}, Closed30d: map[string]int64{}}
	rows, err := s.pool.Query(ctx, `SELECT CASE WHEN score >= 80 THEN 'critical' WHEN score >= 60 THEN 'high' WHEN score >= 30 THEN 'medium' ELSE 'low' END AS lv,
		count(*), min(created_at) FROM cases WHERE status = 'pending'`+appCond+` GROUP BY lv`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var lv string
		var n int64
		var oldest time.Time
		if err := rows.Scan(&lv, &n, &oldest); err != nil {
			rows.Close()
			return nil, err
		}
		out.PendingByLevel[lv] = n
		out.Pending += n
		if out.OldestPendingAt == nil || oldest.Before(*out.OldestPendingAt) {
			o := oldest
			out.OldestPendingAt = &o
		}
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT verdict, count(*) FROM cases WHERE status = 'closed' AND reviewed_at > now() - interval '30 days'`+appCond+` GROUP BY verdict`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		var n int64
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		out.Closed30d[v] = n
	}
	return out, rows.Err()
}

// ListStats 名单统计（有效条目）：按种类、类型的条数，未来 7 天每天到期多少，长期有效多少
type ListStats struct {
	Total     int64            `json:"total"`
	ByKind    map[string]int64 `json:"byKind"`
	ByType    map[string]int64 `json:"byType"`
	Permanent int64            `json:"permanent"`
	// Expiring 未来 7 天按天（本地日期，Asia/Shanghai）到期的条数，按种类拆开
	Expiring []ListExpiryDay `json:"expiring"`
}

type ListExpiryDay struct {
	Day    string           `json:"day"`
	ByKind map[string]int64 `json:"byKind"`
}

func (s *Store) ListStats(ctx context.Context, appID *int64) (*ListStats, error) {
	cond := "removed_at IS NULL AND (expires_at IS NULL OR expires_at > now())"
	args := []any{}
	if appID != nil {
		// 与名单列表一致：按应用看时，全平台条目也算
		cond += " AND (app_id = $1 OR app_id = 0)"
		args = append(args, *appID)
	}
	out := &ListStats{ByKind: map[string]int64{}, ByType: map[string]int64{}}
	rows, err := s.pool.Query(ctx, `SELECT kind, list_type, (expires_at IS NULL) AS forever, count(*) FROM list_entries WHERE `+cond+` GROUP BY kind, list_type, forever`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, typ string
		var forever bool
		var n int64
		if err := rows.Scan(&kind, &typ, &forever, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.Total += n
		out.ByKind[kind] += n
		out.ByType[typ] += n
		if forever {
			out.Permanent += n
		}
	}
	rows.Close()
	rows, err = s.pool.Query(ctx, `SELECT to_char(expires_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS d, kind, count(*) FROM list_entries
		WHERE `+cond+` AND expires_at IS NOT NULL AND expires_at < now() + interval '7 days' GROUP BY d, kind ORDER BY d`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := map[string]int{}
	for rows.Next() {
		var d, kind string
		var n int64
		if err := rows.Scan(&d, &kind, &n); err != nil {
			return nil, err
		}
		i, ok := idx[d]
		if !ok {
			i = len(out.Expiring)
			idx[d] = i
			out.Expiring = append(out.Expiring, ListExpiryDay{Day: d, ByKind: map[string]int64{}})
		}
		out.Expiring[i].ByKind[kind] = n
	}
	if out.Expiring == nil {
		out.Expiring = []ListExpiryDay{}
	}
	return out, rows.Err()
}

// Published 一个场景最近一次发布
type Published struct {
	At time.Time
	By string
}

// LastPublished 各场景最近一次发布的时间与发布人
func (s *Store) LastPublished(ctx context.Context) (map[string]Published, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (scene) scene, published_at, published_by FROM releases ORDER BY scene, published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Published{}
	for rows.Next() {
		var scene string
		var p Published
		if err := rows.Scan(&scene, &p.At, &p.By); err != nil {
			return nil, err
		}
		out[scene] = p
	}
	return out, rows.Err()
}
