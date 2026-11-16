package store

import (
	"context"
	"time"
)

// RankSample 一条精排训练样本（出推荐时记录的特征 + 之后的行为标签）。
type RankSample struct {
	TS       int64
	Features []float32
	Label    bool
}

// 正向行为：收藏、加歌单、下载、分享、播放 ≥ 30 秒；或点了 / 开始放了且没有早跳。
const sqlRankLabel = `(countIf(type IN ('like','playlist_add','download','share') OR (type = 'play_end' AND played_ms >= 30000)) > 0
  OR (countIf(type IN ('click','play_start','search_click')) > 0 AND countIf(` + sqlIsSkip + `) = 0))`

// RankSamples 近 days 天真正被展示过（有 impression 事件）的推荐里逐首的样本，最多 limit 条（取最新的）。
func (c *CH) RankSamples(ctx context.Context, appID int64, days, limit int) ([]RankSample, error) {
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	app := uint64(appID)
	rows, err := c.conn.Query(ctx, `
SELECT toUnixTimestamp64Milli(s.ts), s.features, toUInt8(ifNull(l.label, 0))
FROM (SELECT rec_id, work_key, features, ts FROM rank_samples FINAL WHERE app_id = ? AND ts >= ?) AS s
INNER JOIN (SELECT DISTINCT rec_id FROM events WHERE app_id = ? AND ts >= ? AND type = 'impression') AS imp ON imp.rec_id = s.rec_id
LEFT JOIN (
  SELECT rec_id, work_key, toUInt8(`+sqlRankLabel+`) AS label
  FROM events FINAL WHERE app_id = ? AND ts >= ? AND rec_id != '' AND work_key != ''
  GROUP BY rec_id, work_key) AS l ON l.rec_id = s.rec_id AND l.work_key = s.work_key
ORDER BY s.ts DESC LIMIT ?`, app, from, app, from, app, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankSample
	for rows.Next() {
		var r RankSample
		var label uint8
		if err := rows.Scan(&r.TS, &r.Features, &label); err != nil {
			return nil, err
		}
		r.Label = label == 1
		out = append(out, r)
	}
	return out, rows.Err()
}
