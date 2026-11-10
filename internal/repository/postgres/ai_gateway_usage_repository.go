package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
)

// AIGatewayUserUsage 一个用户在 [From, To) 里的用量：合计、按时间分桶、按模型 / 密钥 / 接口 / 扣费来源的分布。
// 全部条件都带 appid + user_id + created_at 区间，走 idx_ai_gateway_requests_user。
func (r *Repository) AIGatewayUserUsage(ctx context.Context, query aigateway.UsageQuery) (*aigateway.Usage, error) {
	loc := query.Location
	if loc == nil {
		loc = time.UTC
	}
	bucket := aigateway.UsageBucketDay
	if query.Bucket == aigateway.UsageBucketHour {
		bucket = aigateway.UsageBucketHour
	}

	args := []any{query.AppID, query.UserID, query.From, query.To}
	where := []string{"r.appid = $1", "r.user_id = $2", "r.created_at >= $3", "r.created_at < $4"}
	if query.KeyID != nil {
		args = append(args, *query.KeyID)
		where = append(where, fmt.Sprintf("r.key_id = $%d", len(args)))
	}
	if model := strings.TrimSpace(query.Model); model != "" {
		args = append(args, model)
		where = append(where, fmt.Sprintf("r.model = $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")

	usage := &aigateway.Usage{
		Range: query.Range, From: query.From.In(loc), To: query.To.In(loc), Bucket: bucket, Timezone: loc.String(),
		Series: []aigateway.UsagePoint{}, ByModel: []aigateway.UsageGroup{}, ByKey: []aigateway.UsageGroup{},
		ByEndpoint: []aigateway.UsageGroup{}, BySource: []aigateway.SourceTotal{},
	}

	// ── 合计 ──
	var charged, refund, shortfall int64
	totals := &usage.Totals
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
  COUNT(*) FILTER (WHERE r.status IN ('succeeded', 'refunded')),
  COUNT(*) FILTER (WHERE r.status = 'aborted'),
  COUNT(*) FILTER (WHERE r.status IN ('failed', 'expired')),
  COUNT(*) FILTER (WHERE r.status = 'refunded'),
  COUNT(*) FILTER (WHERE r.status = 'reserved'),
  COUNT(*) FILTER (WHERE r.stream),
  COALESCE(SUM(r.input_tokens), 0), COALESCE(SUM(r.output_tokens), 0), COALESCE(SUM(r.images), 0),
  COALESCE(SUM(r.characters), 0), COALESCE(SUM(r.audio_ms), 0),
  COALESCE(SUM(r.charged) FILTER (WHERE r.status IN ('succeeded', 'aborted')), 0),
  COALESCE(SUM(r.charged) FILTER (WHERE r.status = 'refunded'), 0),
  COALESCE(SUM(r.shortfall) FILTER (WHERE r.status IN ('succeeded', 'aborted')), 0),
  COALESCE(ROUND(AVG(r.latency_ms) FILTER (WHERE r.status <> 'reserved' AND r.latency_ms > 0)), 0)::int,
  COALESCE(ROUND(percentile_cont(0.95) WITHIN GROUP (ORDER BY r.latency_ms)
    FILTER (WHERE r.status <> 'reserved' AND r.latency_ms > 0)), 0)::int,
  COALESCE(ROUND(AVG(r.first_token_ms) FILTER (WHERE r.first_token_ms > 0)), 0)::int
FROM ai_gateway_requests r WHERE `+clause, args...).Scan(&totals.Requests, &totals.Succeeded, &totals.Aborted,
		&totals.Failed, &totals.Refunded, &totals.Pending, &totals.Streamed, &totals.InputTokens, &totals.OutputTokens,
		&totals.Images, &totals.Characters, &totals.AudioMs, &charged, &refund, &shortfall, &totals.AvgLatencyMs,
		&totals.P95LatencyMs, &totals.AvgFirstTokenMs); err != nil {
		return nil, err
	}
	totals.Charged = aigateway.MicroToPoints(charged)
	totals.Refund = aigateway.MicroToPoints(refund)
	totals.Shortfall = aigateway.MicroToPoints(shortfall)

	// ── 时间分桶（按设置时区的整点 / 零点）──
	seriesArgs := append(append([]any{}, args...), bucket, loc.String())
	b, tz := len(seriesArgs)-1, len(seriesArgs)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT date_trunc($%d::text, r.created_at AT TIME ZONE $%d::text) AT TIME ZONE $%d::text,
  COUNT(*), COUNT(*) FILTER (WHERE r.status IN ('failed', 'expired')),
  COALESCE(SUM(r.input_tokens), 0), COALESCE(SUM(r.output_tokens), 0),
  COALESCE(SUM(r.charged) FILTER (WHERE r.status IN ('succeeded', 'aborted')), 0)
FROM ai_gateway_requests r WHERE %s GROUP BY 1 ORDER BY 1`, b, tz, tz, clause), seriesArgs...)
	if err != nil {
		return nil, err
	}
	points := []aigateway.UsagePoint{}
	for rows.Next() {
		var point aigateway.UsagePoint
		var micro int64
		if err := rows.Scan(&point.Time, &point.Requests, &point.Failed, &point.InputTokens, &point.OutputTokens, &micro); err != nil {
			rows.Close()
			return nil, err
		}
		point.Charged = aigateway.MicroToPoints(micro)
		points = append(points, point)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	usage.Series = aigateway.FillUsageSeries(points, usage.From, usage.To, bucket)

	// ── 分组 ──
	group := func(column, join, label, hint string) ([]aigateway.UsageGroup, error) {
		rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s::text, %s, %s,
  COUNT(*), COUNT(*) FILTER (WHERE r.status IN ('failed', 'expired')),
  COALESCE(SUM(r.input_tokens), 0), COALESCE(SUM(r.output_tokens), 0), COALESCE(SUM(r.images), 0),
  COALESCE(SUM(r.charged) FILTER (WHERE r.status IN ('succeeded', 'aborted')), 0),
  COALESCE(ROUND(AVG(r.latency_ms) FILTER (WHERE r.status <> 'reserved' AND r.latency_ms > 0)), 0)::int
FROM ai_gateway_requests r %s WHERE %s GROUP BY 1 ORDER BY 9 DESC, 4 DESC, 1 LIMIT 200`, column, label, hint, join, clause), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []aigateway.UsageGroup{}
		for rows.Next() {
			var item aigateway.UsageGroup
			var micro int64
			if err := rows.Scan(&item.Key, &item.Label, &item.Hint, &item.Requests, &item.Failed, &item.InputTokens,
				&item.OutputTokens, &item.Images, &micro, &item.AvgLatencyMs); err != nil {
				return nil, err
			}
			item.Charged = aigateway.MicroToPoints(micro)
			out = append(out, item)
		}
		return out, rows.Err()
	}
	if usage.ByModel, err = group("r.model", "", "''", "''"); err != nil {
		return nil, err
	}
	// 密钥按 key_id 分组；名称取自密钥表（已删除的密钥为空），尾号取调用时记下的。key_id = 0 为登录令牌。
	if usage.ByKey, err = group("r.key_id",
		"LEFT JOIN ai_gateway_keys k ON k.id = r.key_id AND k.appid = r.appid AND k.user_id = r.user_id",
		"COALESCE(MAX(k.name), '')", "COALESCE(MAX(r.key_hint), '')"); err != nil {
		return nil, err
	}
	if usage.ByEndpoint, err = group("r.endpoint", "", "''", "''"); err != nil {
		return nil, err
	}

	// ── 扣费来源 ──
	rows, err = r.pool.Query(ctx, `SELECT c->>'source', COALESCE(SUM((c->>'micro')::bigint), 0), COALESCE(SUM((c->>'units')::bigint), 0)
FROM ai_gateway_requests r, jsonb_array_elements(r.charges) c
WHERE `+clause+` AND r.status IN ('succeeded', 'aborted')
GROUP BY 1 ORDER BY 2 DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item aigateway.SourceTotal
		var micro int64
		if err := rows.Scan(&item.Source, &micro, &item.Units); err != nil {
			return nil, err
		}
		item.Amount = aigateway.MicroToPoints(micro)
		usage.BySource = append(usage.BySource, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return usage, nil
}
