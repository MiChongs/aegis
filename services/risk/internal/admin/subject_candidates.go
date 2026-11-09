package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/store"

	"github.com/cloudwego/hertz/pkg/app"
)

// 风险账号发现：不从某个具体用户出发，按风控信号直接列出可疑账号（GET /subjects/candidates）。
//
//   - blocked：近 N 天被拦截 / 封禁的次数达到门槛；
//   - score：近 N 天最高风险分达到门槛；
//   - listed：在用户黑名单里（生效中）；
//   - confirmed：近 N 天复核结论为「确认有风险」。
//
// 「风控封禁中」与「设备聚集」在主服务里查（封号与登录设备是主库的数据），见 internal/service/risk_ops.go。

// Candidate 一个候选账号与它被列出来的原因
type Candidate struct {
	AppID    int64      `json:"appId"`
	UserID   int64      `json:"userId"`
	Count    uint64     `json:"count"`
	MaxScore int32      `json:"maxScore"`
	LastAt   time.Time  `json:"lastAt"`
	Reason   string     `json:"reason"`
	Scene    string     `json:"scene"`
	Expires  *time.Time `json:"expiresAt,omitempty"`
}

type candidateRow struct {
	AppID    int64     `ch:"app_id"`
	UserID   int64     `ch:"user_id"`
	Count    uint64    `ch:"n"`
	MaxScore int32     `ch:"max_score"`
	LastAt   time.Time `ch:"last"`
	Reason   string    `ch:"why"`
	Scene    string    `ch:"scene"`
}

func (s *Server) subjectCandidates(ctx context.Context, c *app.RequestContext) (any, error) {
	appID := qint64(c, "app")
	source := string(c.Query("source"))
	days := min(max(qint(c, "days", 30), 1), 365)
	limit := min(max(qint(c, "limit", 100), 1), 300)
	minimum := qint(c, "min", 0)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	var (
		out []Candidate
		err error
	)
	switch source {
	case "blocked", "score":
		if s.CH == nil {
			return nil, bad("分析库未就绪")
		}
		having, order := "n >= ?", "n DESC, last DESC"
		if source == "score" {
			having, order = "max_score >= ?", "max_score DESC, last DESC"
			if minimum <= 0 || minimum > 100 {
				minimum = 80
			}
		} else if minimum <= 0 {
			minimum = 3
		}
		where, args := "ts >= ? AND user_id > 0 AND shadow = 0", []any{since}
		if appID > 0 {
			where += " AND app_id = ?"
			args = append(args, appID)
		}
		args = append(args, minimum, limit)
		risky := "action IN ('block','ban')"
		if source == "score" {
			risky = "action != 'pass'"
		}
		var rows []candidateRow
		err = s.CH.Conn().Select(ctx, &rows, `
			SELECT app_id, user_id, countIf(`+risky+`) AS n, max(score) AS max_score,
				max(ts) AS last,
				argMaxIf(arrayStringConcat(reasons, '；'), ts, `+risky+`) AS why,
				argMaxIf(scene, ts, `+risky+`) AS scene
			FROM decisions WHERE `+where+`
			GROUP BY app_id, user_id HAVING `+having+` ORDER BY `+order+` LIMIT ?`, args...)
		for _, r := range rows {
			out = append(out, Candidate{AppID: r.AppID, UserID: r.UserID, Count: r.Count, MaxScore: r.MaxScore, LastAt: r.LastAt, Reason: r.Reason, Scene: r.Scene})
		}
	case "listed":
		q := store.ListQuery{ListType: bundle.ListUser, Kind: bundle.KindBlock, Active: true, Limit: limit, Page: 1}
		if appID > 0 {
			q.AppID = &appID
		}
		entries, _, lerr := s.Store.ListEntries(ctx, q)
		err = lerr
		for _, e := range entries {
			uid, perr := strconv.ParseInt(e.Value, 10, 64)
			if perr != nil || uid <= 0 || e.AppID <= 0 {
				continue // 全平台的用户 ID 名单分不清是哪个应用的账号
			}
			out = append(out, Candidate{AppID: e.AppID, UserID: uid, Count: 1, LastAt: e.CreatedAt, Reason: e.Reason, Scene: e.Source, Expires: e.ExpiresAt})
		}
	case "confirmed":
		where, args := "verdict = 'reject' AND user_id > 0 AND reviewed_at >= $1", []any{since}
		if appID > 0 {
			args = append(args, appID)
			where += " AND app_id = $2"
		}
		args = append(args, limit)
		rows, qerr := s.Store.Pool().Query(ctx, `
			SELECT app_id, user_id, count(*), max(score), max(reviewed_at),
				(array_agg(CASE WHEN note <> '' THEN note ELSE array_to_string(reasons, '；') END ORDER BY reviewed_at DESC))[1],
				(array_agg(scene ORDER BY reviewed_at DESC))[1]
			FROM cases WHERE `+where+` GROUP BY app_id, user_id ORDER BY max(reviewed_at) DESC LIMIT $`+strconv.Itoa(len(args)), args...)
		if qerr != nil {
			return nil, qerr
		}
		defer rows.Close()
		for rows.Next() {
			var c Candidate
			var n int64
			var score int
			if err := rows.Scan(&c.AppID, &c.UserID, &n, &score, &c.LastAt, &c.Reason, &c.Scene); err != nil {
				return nil, err
			}
			c.Count, c.MaxScore = uint64(n), int32(score)
			out = append(out, c)
		}
		err = rows.Err()
	default:
		return nil, bad("来源只能是 blocked / score / listed / confirmed")
	}
	if err != nil {
		return nil, err
	}
	for i := range out {
		// 原因只留第一句，列表里一行放得下
		if r := strings.TrimSpace(out[i].Reason); r != "" {
			out[i].Reason = strings.SplitN(r, "；", 2)[0]
		}
	}
	return map[string]any{"source": source, "days": days, "min": minimum, "items": nonNilSlice(out)}, nil
}
