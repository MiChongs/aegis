package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	lotterydomain "aegis/internal/domain/lottery"

	"golang.org/x/sync/errgroup"
)

// lotteryRiskClusterLimit 每类聚集最多列几个（按账号数倒序，只列 2 个账号以上的）。
const lotteryRiskClusterLimit = 12

// lotteryRiskEventLimit 最近被拒绝的请求列几条。
const lotteryRiskEventLimit = 40

// lotteryRiskSubnetExpr client_ip（文本）→ /24 或 /64 网段；解析不了为 NULL。与 lottery.SubnetOf 同一口径。
const lotteryRiskSubnetExpr = `CASE WHEN pg_input_is_valid(client_ip, 'inet')
  THEN network(set_masklen(client_ip::inet, CASE WHEN family(client_ip::inet) = 4 THEN 24 ELSE 64 END))::text END`

// LotteryRiskOverview 一个活动 since 之后的风控总览。
//
// 汇总、处置、信号、趋势、三类聚集、拒绝事件互不依赖，在各自的连接上并行查（errgroup），
// 墙钟时间约等于最慢的一条，而不是全部相加。
func (r *Repository) LotteryRiskOverview(ctx context.Context, appID, activityID int64, since time.Time, timezone string) (*lotterydomain.RiskOverview, error) {
	started := time.Now()
	out := &lotterydomain.RiskOverview{
		ActivityID: activityID, Since: since,
		Actions: []lotterydomain.RiskCount{}, Signals: []lotterydomain.RiskCount{}, Trend: []lotterydomain.RiskDay{},
		Devices: []lotterydomain.RiskCluster{}, IPs: []lotterydomain.RiskCluster{}, Subnets: []lotterydomain.RiskCluster{},
		Events: []lotterydomain.RiskEvent{},
	}
	var drawActions, eventActions, drawSignals, eventSignals map[string]int64
	var drawDays, eventDays map[string]int64
	var flaggedDays map[string]int64
	var dayOrder []string

	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT user_id),
  COUNT(*) FILTER (WHERE risk_action IS NOT NULL), COUNT(DISTINCT user_id) FILTER (WHERE risk_action IS NOT NULL)
FROM lottery_draws WHERE appid = $1 AND activity_id = $2 AND created_at >= $3`, appID, activityID, since).
			Scan(&out.Draws, &out.Users, &out.Flagged, &out.FlaggedUsers)
	})
	group.Go(func() error {
		var err error
		drawActions, err = r.lotteryRiskKeyCounts(ctx, `SELECT risk_action, COUNT(*) FROM lottery_draws
WHERE activity_id = $1 AND created_at >= $2 AND risk_action IS NOT NULL GROUP BY risk_action`, activityID, since)
		if err != nil {
			return err
		}
		drawSignals, err = r.lotteryRiskKeyCounts(ctx, `SELECT rule, COUNT(*) FROM lottery_draws, unnest(risk_rules) AS rule
WHERE activity_id = $1 AND created_at >= $2 AND risk_action IS NOT NULL GROUP BY rule`, activityID, since)
		return err
	})
	group.Go(func() error {
		var err error
		if err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_risk_events WHERE activity_id = $1 AND created_at >= $2`,
			activityID, since).Scan(&out.Denied); err != nil {
			return err
		}
		eventActions, err = r.lotteryRiskKeyCounts(ctx, `SELECT action, COUNT(*) FROM lottery_risk_events
WHERE activity_id = $1 AND created_at >= $2 GROUP BY action`, activityID, since)
		if err != nil {
			return err
		}
		eventSignals, err = r.lotteryRiskKeyCounts(ctx, `SELECT rule, COUNT(*) FROM lottery_risk_events, unnest(rules) AS rule
WHERE activity_id = $1 AND created_at >= $2 GROUP BY rule`, activityID, since)
		if err != nil {
			return err
		}
		eventDays, err = r.lotteryRiskKeyCounts(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day, COUNT(*)
FROM lottery_risk_events WHERE activity_id = $1 AND created_at >= $2 GROUP BY day`, activityID, since, timezone)
		return err
	})
	group.Go(func() error {
		rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
  COUNT(*), COUNT(*) FILTER (WHERE risk_action IS NOT NULL)
FROM lottery_draws WHERE activity_id = $1 AND created_at >= $2 GROUP BY day ORDER BY day`, activityID, since, timezone)
		if err != nil {
			return err
		}
		defer rows.Close()
		drawDays, flaggedDays = map[string]int64{}, map[string]int64{}
		for rows.Next() {
			var day string
			var draws, flagged int64
			if err := rows.Scan(&day, &draws, &flagged); err != nil {
				return err
			}
			drawDays[day], flaggedDays[day] = draws, flagged
			dayOrder = append(dayOrder, day)
		}
		return rows.Err()
	})
	cluster := func(keyExpr string, into *[]lotterydomain.RiskCluster) func() error {
		return func() error {
			rows, err := r.pool.Query(ctx, `SELECT key, COUNT(DISTINCT user_id), COUNT(*),
  COUNT(*) FILTER (WHERE risk_action IS NOT NULL), MAX(created_at)
FROM (SELECT `+keyExpr+` AS key, user_id, risk_action, created_at FROM lottery_draws
      WHERE activity_id = $1 AND created_at >= $2) t
WHERE key IS NOT NULL AND key <> ''
GROUP BY key HAVING COUNT(DISTINCT user_id) >= 2
ORDER BY COUNT(DISTINCT user_id) DESC, COUNT(*) DESC LIMIT $3`, activityID, since, lotteryRiskClusterLimit)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var item lotterydomain.RiskCluster
				if err := rows.Scan(&item.Key, &item.Accounts, &item.Draws, &item.Flagged, &item.LastAt); err != nil {
					return err
				}
				*into = append(*into, item)
			}
			return rows.Err()
		}
	}
	group.Go(cluster("device_id", &out.Devices))
	group.Go(cluster("client_ip", &out.IPs))
	group.Go(cluster(lotteryRiskSubnetExpr, &out.Subnets))
	group.Go(func() error {
		rows, err := r.pool.Query(ctx, `SELECT e.id, e.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''), e.action, e.rules,
  e.report, e.source, COALESCE(e.device_id, ''), COALESCE(e.client_ip, ''), e.created_at
FROM lottery_risk_events e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN user_profiles up ON up.user_id = e.user_id
WHERE e.activity_id = $1 AND e.created_at >= $2
ORDER BY e.created_at DESC, e.id DESC LIMIT $3`, activityID, since, lotteryRiskEventLimit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item lotterydomain.RiskEvent
			var report []byte
			if err := rows.Scan(&item.ID, &item.UserID, &item.Account, &item.Nickname, &item.Action, &item.Rules, &report,
				&item.Source, &item.DeviceID, &item.ClientIP, &item.CreatedAt); err != nil {
				return err
			}
			if len(report) > 0 {
				var parsed lotterydomain.RiskReport
				if json.Unmarshal(report, &parsed) == nil {
					item.Report = &parsed
				}
			}
			out.Events = append(out.Events, item)
		}
		return rows.Err()
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}

	out.Actions = mergeRiskCounts(drawActions, eventActions)
	out.Signals = mergeRiskCounts(drawSignals, eventSignals)
	seen := map[string]bool{}
	for _, day := range dayOrder {
		seen[day] = true
	}
	for day := range eventDays {
		if !seen[day] {
			dayOrder = append(dayOrder, day)
		}
	}
	for _, day := range dayOrder {
		out.Trend = append(out.Trend, lotterydomain.RiskDay{Date: day, Draws: drawDays[day], Flagged: flaggedDays[day], Denied: eventDays[day]})
	}
	out.TookMs = time.Since(started).Milliseconds()
	return out, nil
}

func (r *Repository) lotteryRiskKeyCounts(ctx context.Context, sql string, args ...any) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, rows.Err()
}

// mergeRiskCounts 记录里的命中与拒绝事件按键合并，按总数倒序。
func mergeRiskCounts(draws, denied map[string]int64) []lotterydomain.RiskCount {
	index := map[string]int{}
	out := []lotterydomain.RiskCount{}
	for key, count := range draws {
		index[key] = len(out)
		out = append(out, lotterydomain.RiskCount{Key: key, Draws: count})
	}
	for key, count := range denied {
		if i, ok := index[key]; ok {
			out[i].Denied = count
			continue
		}
		out = append(out, lotterydomain.RiskCount{Key: key, Denied: count})
	}
	slices.SortFunc(out, func(a, b lotterydomain.RiskCount) int {
		if diff := cmp.Compare(b.Draws+b.Denied, a.Draws+a.Denied); diff != 0 {
			return diff
		}
		return strings.Compare(a.Key, b.Key)
	})
	return out
}
