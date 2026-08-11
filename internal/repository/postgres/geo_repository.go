package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	geodomain "aegis/internal/domain/geo"

	"github.com/jackc/pgx/v5"
)

// ──────────────────────────────────────
// 地理围栏（geo_fences）
// ──────────────────────────────────────

const geoFenceColumns = `id, app_id, name, mode,
	ST_AsGeoJSON(fence),
	CASE WHEN center IS NOT NULL THEN ST_Y(center::geometry) END,
	CASE WHEN center IS NOT NULL THEN ST_X(center::geometry) END,
	radius_m, ban_mode, reason, enabled, expires_at,
	match_count, last_match_at, created_by, created_at, updated_at`

func scanGeoFence(row pgx.Row) (geodomain.Fence, error) {
	var (
		f         geodomain.Fence
		fenceJSON *string
	)
	err := row.Scan(
		&f.ID, &f.AppID, &f.Name, &f.Mode,
		&fenceJSON, &f.CenterLat, &f.CenterLng,
		&f.RadiusM, &f.BanMode, &f.Reason, &f.Enabled, &f.ExpiresAt,
		&f.MatchCount, &f.LastMatchAt, &f.CreatedBy, &f.CreatedAt, &f.UpdatedAt,
	)
	if err != nil {
		return geodomain.Fence{}, err
	}
	if fenceJSON != nil && *fenceJSON != "" {
		f.Fence = json.RawMessage(*fenceJSON)
	}
	return f, nil
}

// ListGeoFences 返回围栏规则（onlyEnabled 时仅启用中）。
func (r *Repository) ListGeoFences(ctx context.Context, onlyEnabled bool) ([]geodomain.Fence, error) {
	sql := `SELECT ` + geoFenceColumns + ` FROM geo_fences`
	if onlyEnabled {
		sql += ` WHERE enabled = TRUE`
	}
	sql += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []geodomain.Fence
	for rows.Next() {
		f, err := scanGeoFence(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetGeoFence 按 ID 读取围栏。
func (r *Repository) GetGeoFence(ctx context.Context, id int64) (*geodomain.Fence, error) {
	f, err := scanGeoFence(r.pool.QueryRow(ctx,
		`SELECT `+geoFenceColumns+` FROM geo_fences WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ValidateFenceGeoJSON 用 PostGIS 校验 GeoJSON 多边形合法性。
func (r *Repository) ValidateFenceGeoJSON(ctx context.Context, geoJSON string) error {
	var valid bool
	err := r.pool.QueryRow(ctx,
		`SELECT ST_IsValid(ST_Multi(ST_GeomFromGeoJSON($1)))`, geoJSON,
	).Scan(&valid)
	if err != nil {
		return fmt.Errorf("围栏 GeoJSON 解析失败: %w", err)
	}
	if !valid {
		return fmt.Errorf("围栏多边形无效（自相交或环未闭合）")
	}
	return nil
}

// fenceShapeSQL 生成围栏几何表达式：$N=GeoJSON、$N+1=lat、$N+2=lng。
const fenceInsertSQL = `
	INSERT INTO geo_fences (app_id, name, mode, fence, center, radius_m, ban_mode, reason, enabled, expires_at, created_by)
	VALUES ($1, $2, $3,
		CASE WHEN $4 <> '' THEN ST_Multi(ST_GeomFromGeoJSON($4))::geography END,
		CASE WHEN $5::float8 IS NOT NULL AND $6::float8 IS NOT NULL
		     THEN ST_SetSRID(ST_MakePoint($6::float8, $5::float8), 4326)::geography END,
		$7, $8, $9, $10, $11, $12)
	RETURNING ` + geoFenceColumns

// CreateGeoFence 创建围栏。
func (r *Repository) CreateGeoFence(ctx context.Context, m geodomain.FenceMutation, createdBy *int64) (geodomain.Fence, error) {
	return scanGeoFence(r.pool.QueryRow(ctx, fenceInsertSQL,
		m.AppID, strings.TrimSpace(m.Name), m.Mode, strings.TrimSpace(m.FenceGeoJSON),
		m.CenterLat, m.CenterLng, m.RadiusM,
		m.BanMode, m.Reason, m.Enabled, m.ExpiresAt, createdBy,
	))
}

// UpdateGeoFence 全量更新围栏。
func (r *Repository) UpdateGeoFence(ctx context.Context, id int64, m geodomain.FenceMutation) (geodomain.Fence, error) {
	return scanGeoFence(r.pool.QueryRow(ctx, `
		UPDATE geo_fences SET
			app_id   = $2,
			name     = $3,
			mode     = $4,
			fence    = CASE WHEN $5 <> '' THEN ST_Multi(ST_GeomFromGeoJSON($5))::geography END,
			center   = CASE WHEN $6::float8 IS NOT NULL AND $7::float8 IS NOT NULL
			                THEN ST_SetSRID(ST_MakePoint($7::float8, $6::float8), 4326)::geography END,
			radius_m = $8,
			ban_mode = $9,
			reason   = $10,
			enabled  = $11,
			expires_at = $12,
			updated_at = NOW()
		WHERE id = $1
		RETURNING `+geoFenceColumns,
		id, m.AppID, strings.TrimSpace(m.Name), m.Mode, strings.TrimSpace(m.FenceGeoJSON),
		m.CenterLat, m.CenterLng, m.RadiusM,
		m.BanMode, m.Reason, m.Enabled, m.ExpiresAt,
	))
}

// UpdateGeoFenceStatus 切换启用状态。
func (r *Repository) UpdateGeoFenceStatus(ctx context.Context, id int64, enabled bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE geo_fences SET enabled = $1, updated_at = NOW() WHERE id = $2`, enabled, id)
	return err
}

// DeleteGeoFence 删除围栏。
func (r *Repository) DeleteGeoFence(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM geo_fences WHERE id = $1`, id)
	return err
}

// IncrementGeoFenceMatch 累计命中次数（异步调用，忽略错误）。
func (r *Repository) IncrementGeoFenceMatch(ctx context.Context, id int64, ts time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE geo_fences SET match_count = match_count + 1, last_match_at = $2 WHERE id = $1`, id, ts)
	return err
}

// PreviewGeoFence 回测围栏影响面：统计窗口内命中该几何的登录事件与防火墙拦截。
func (r *Repository) PreviewGeoFence(ctx context.Context, m geodomain.FenceMutation, windowDays int) (*geodomain.FencePreview, error) {
	if windowDays <= 0 {
		windowDays = 7
	}
	p := &geodomain.FencePreview{WindowDays: windowDays}
	err := r.pool.QueryRow(ctx, `
		WITH f AS (
			SELECT CASE WHEN $1 <> '' THEN ST_Multi(ST_GeomFromGeoJSON($1))::geography END AS poly,
			       CASE WHEN $2::float8 IS NOT NULL AND $3::float8 IS NOT NULL
			            THEN ST_SetSRID(ST_MakePoint($3::float8, $2::float8), 4326)::geography END AS c,
			       $4::float8 AS r
		)
		SELECT
			(SELECT COUNT(*) FROM login_geo_events e, f
			  WHERE e.created_at > NOW() - make_interval(days => $5) AND e.geom IS NOT NULL
			    AND CASE WHEN f.poly IS NOT NULL THEN ST_Intersects(e.geom, f.poly)
			             ELSE ST_DWithin(e.geom, f.c, f.r) END),
			(SELECT COUNT(DISTINCT e.user_id) FROM login_geo_events e, f
			  WHERE e.created_at > NOW() - make_interval(days => $5) AND e.geom IS NOT NULL
			    AND CASE WHEN f.poly IS NOT NULL THEN ST_Intersects(e.geom, f.poly)
			             ELSE ST_DWithin(e.geom, f.c, f.r) END),
			(SELECT COUNT(*) FROM firewall_logs l, f
			  WHERE l.blocked_at > NOW() - make_interval(days => $5) AND l.geom IS NOT NULL
			    AND CASE WHEN f.poly IS NOT NULL THEN ST_Intersects(l.geom, f.poly)
			             ELSE ST_DWithin(l.geom, f.c, f.r) END)`,
		strings.TrimSpace(m.FenceGeoJSON), m.CenterLat, m.CenterLng, m.RadiusM, windowDays,
	).Scan(&p.LoginMatches, &p.UniqueUsers, &p.BlockMatches)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ──────────────────────────────────────
// 登录地理事件（login_geo_events）
// ──────────────────────────────────────

// InsertLoginGeoEvent 写入单条登录地理事件。
func (r *Repository) InsertLoginGeoEvent(ctx context.Context, e geodomain.LoginEvent) error {
	createdAt := e.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO login_geo_events
			(user_id, app_id, ip, country_code, country, region, city, asn, isp, geom, login_type, device_id, created_at)
		VALUES ($1, $2, $3::inet, $4, $5, $6, $7, $8, $9,
			CASE WHEN $10::float8 IS NOT NULL AND $11::float8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($11::float8, $10::float8), 4326)::geography END,
			$12, $13, $14)`,
		e.UserID, e.AppID, e.IP, e.CountryCode, e.Country, e.Region, e.City, e.ASN, e.ISP,
		e.Lat, e.Lng, e.LoginType, e.DeviceID, createdAt,
	)
	return err
}

// ListUserGeoTrail 返回用户最近的登录轨迹（按时间倒序）。
func (r *Repository) ListUserGeoTrail(ctx context.Context, userID, appID int64, limit int) ([]geodomain.TrailPoint, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT ip::text, country_code, country, region, city,
		       CASE WHEN geom IS NOT NULL THEN ST_Y(geom::geometry) END,
		       CASE WHEN geom IS NOT NULL THEN ST_X(geom::geometry) END,
		       login_type, device_id, created_at
		  FROM login_geo_events
		 WHERE user_id = $1 AND app_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3`, userID, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []geodomain.TrailPoint
	for rows.Next() {
		var t geodomain.TrailPoint
		if err := rows.Scan(&t.IP, &t.CountryCode, &t.Country, &t.Region, &t.City,
			&t.Lat, &t.Lng, &t.LoginType, &t.DeviceID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ──────────────────────────────────────
// 用户地理画像（user_geo_profiles）
// ──────────────────────────────────────

// GetUserGeoProfile 读取用户地理画像；不存在返回 nil。
func (r *Repository) GetUserGeoProfile(ctx context.Context, userID, appID int64) (*geodomain.Profile, error) {
	var p geodomain.Profile
	var lastIP *string
	err := r.pool.QueryRow(ctx, `
		SELECT user_id, app_id,
		       CASE WHEN home_geom IS NOT NULL THEN ST_Y(home_geom::geometry) END,
		       CASE WHEN home_geom IS NOT NULL THEN ST_X(home_geom::geometry) END,
		       home_radius_m, known_countries,
		       CASE WHEN last_geom IS NOT NULL THEN ST_Y(last_geom::geometry) END,
		       CASE WHEN last_geom IS NOT NULL THEN ST_X(last_geom::geometry) END,
		       last_country, last_ip::text, last_login_at, login_count, updated_at
		  FROM user_geo_profiles
		 WHERE user_id = $1 AND app_id = $2`, userID, appID,
	).Scan(
		&p.UserID, &p.AppID, &p.HomeLat, &p.HomeLng, &p.HomeRadiusM, &p.KnownCountries,
		&p.LastLat, &p.LastLng, &p.LastCountry, &lastIP, &p.LastLoginAt, &p.LoginCount, &p.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastIP != nil {
		p.LastIP = *lastIP
	}
	return &p, nil
}

// TouchUserGeoProfileOnLogin 登录后增量更新画像（last_* / login_count / known_countries）。
// home_geom 等基线字段由每日重算任务维护（RecomputeUserGeoProfiles）。
func (r *Repository) TouchUserGeoProfileOnLogin(ctx context.Context, e geodomain.LoginEvent) error {
	loginAt := e.CreatedAt
	if loginAt.IsZero() {
		loginAt = time.Now().UTC()
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_geo_profiles AS p
			(user_id, app_id, known_countries, last_geom, last_country, last_ip, last_login_at, login_count)
		VALUES ($1, $2,
			CASE WHEN $3 <> '' THEN ARRAY[$3]::text[] ELSE '{}'::text[] END,
			CASE WHEN $4::float8 IS NOT NULL AND $5::float8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($5::float8, $4::float8), 4326)::geography END,
			$3, NULLIF($6, '')::inet, $7, 1)
		ON CONFLICT (user_id, app_id) DO UPDATE SET
			known_countries = CASE WHEN $3 <> '' AND NOT ($3 = ANY(p.known_countries))
			                       THEN array_append(p.known_countries, $3)
			                       ELSE p.known_countries END,
			last_geom     = COALESCE(EXCLUDED.last_geom, p.last_geom),
			last_country  = CASE WHEN $3 <> '' THEN $3 ELSE p.last_country END,
			last_ip       = COALESCE(EXCLUDED.last_ip, p.last_ip),
			last_login_at = EXCLUDED.last_login_at,
			login_count   = p.login_count + 1,
			updated_at    = NOW()`,
		e.UserID, e.AppID, e.CountryCode, e.Lat, e.Lng, e.IP, loginAt,
	)
	return err
}

// RecomputeUserGeoProfiles 基于近 windowDays 天登录事件重算画像基线
// （home 质心 + P90 半径 + 已知国家），返回更新行数。
func (r *Repository) RecomputeUserGeoProfiles(ctx context.Context, windowDays int) (int64, error) {
	if windowDays <= 0 {
		windowDays = 90
	}
	tag, err := r.pool.Exec(ctx, `
		WITH agg AS (
			SELECT user_id, app_id,
			       ST_Centroid(ST_Collect(geom::geometry)) AS centroid,
			       array_agg(DISTINCT country_code) FILTER (WHERE country_code <> '') AS countries
			  FROM login_geo_events
			 WHERE created_at > NOW() - make_interval(days => $1) AND geom IS NOT NULL
			 GROUP BY user_id, app_id
		), radii AS (
			SELECT e.user_id, e.app_id,
			       percentile_cont(0.9) WITHIN GROUP (
			           ORDER BY ST_Distance(a.centroid::geography, e.geom)
			       ) AS r90
			  FROM login_geo_events e
			  JOIN agg a USING (user_id, app_id)
			 WHERE e.created_at > NOW() - make_interval(days => $1) AND e.geom IS NOT NULL
			 GROUP BY e.user_id, e.app_id
		)
		INSERT INTO user_geo_profiles AS p (user_id, app_id, home_geom, home_radius_m, known_countries)
		SELECT a.user_id, a.app_id, a.centroid::geography, COALESCE(r.r90, 0), COALESCE(a.countries, '{}'::text[])
		  FROM agg a
		  LEFT JOIN radii r USING (user_id, app_id)
		ON CONFLICT (user_id, app_id) DO UPDATE SET
			home_geom       = EXCLUDED.home_geom,
			home_radius_m   = EXCLUDED.home_radius_m,
			known_countries = ARRAY(SELECT DISTINCT unnest(p.known_countries || EXCLUDED.known_countries)),
			updated_at      = NOW()`,
		windowDays,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ──────────────────────────────────────
// 小时聚合（geo_stats_hourly）
// ──────────────────────────────────────

// RollupGeoStatsRange 重算 [start, end) 区间的小时聚合（幂等，可安全重跑）。
// start/end 必须小时对齐；kind 见 geodomain.StatsKind*。
func (r *Repository) RollupGeoStatsRange(ctx context.Context, kind string, start, end time.Time) error {
	var sql string
	switch kind {
	case geodomain.StatsKindBlock:
		sql = `
			INSERT INTO geo_stats_hourly (bucket, kind, country_code, city, geohash5, lat, lng, cnt)
			SELECT date_trunc('hour', blocked_at), 'block', country_code, city,
			       COALESCE(ST_GeoHash(geom::geometry, 5), ''),
			       COALESCE(AVG(ST_Y(geom::geometry)), 0),
			       COALESCE(AVG(ST_X(geom::geometry)), 0),
			       COUNT(*)
			  FROM firewall_logs
			 WHERE blocked_at >= $1 AND blocked_at < $2
			 GROUP BY 1, 3, 4, 5
			ON CONFLICT (bucket, kind, country_code, city, geohash5)
			DO UPDATE SET cnt = EXCLUDED.cnt, lat = EXCLUDED.lat, lng = EXCLUDED.lng`
	case geodomain.StatsKindLogin:
		sql = `
			INSERT INTO geo_stats_hourly (bucket, kind, country_code, city, geohash5, lat, lng, cnt)
			SELECT date_trunc('hour', created_at), 'login', country_code, city,
			       COALESCE(ST_GeoHash(geom::geometry, 5), ''),
			       COALESCE(AVG(ST_Y(geom::geometry)), 0),
			       COALESCE(AVG(ST_X(geom::geometry)), 0),
			       COUNT(*)
			  FROM login_geo_events
			 WHERE created_at >= $1 AND created_at < $2
			 GROUP BY 1, 3, 4, 5
			ON CONFLICT (bucket, kind, country_code, city, geohash5)
			DO UPDATE SET cnt = EXCLUDED.cnt, lat = EXCLUDED.lat, lng = EXCLUDED.lng`
	default:
		return fmt.Errorf("未知聚合类别: %s", kind)
	}
	_, err := r.pool.Exec(ctx, sql, start, end)
	return err
}

// QueryGeoHeatmap 查询热力图（仅读预聚合表）。
func (r *Repository) QueryGeoHeatmap(ctx context.Context, q geodomain.HeatmapQuery) (*geodomain.HeatmapResult, error) {
	if q.Limit <= 0 || q.Limit > 5000 {
		q.Limit = 1000
	}
	result := &geodomain.HeatmapResult{Cells: []geodomain.HeatmapCell{}, Countries: []geodomain.CountryStat{}}

	rows, err := r.pool.Query(ctx, `
		SELECT geohash5, AVG(lat), AVG(lng), country_code, city, SUM(cnt)
		  FROM geo_stats_hourly
		 WHERE kind = $1 AND bucket >= $2 AND bucket < $3
		   AND ($4 = '' OR country_code = $4)
		   AND geohash5 <> ''
		 GROUP BY geohash5, country_code, city
		 ORDER BY SUM(cnt) DESC
		 LIMIT $5`,
		q.Kind, q.Start, q.End, strings.ToUpper(strings.TrimSpace(q.Country)), q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c geodomain.HeatmapCell
		if err := rows.Scan(&c.Geohash, &c.Lat, &c.Lng, &c.CountryCode, &c.City, &c.Count); err != nil {
			return nil, err
		}
		result.Cells = append(result.Cells, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `
		SELECT country_code, SUM(cnt)
		  FROM geo_stats_hourly
		 WHERE kind = $1 AND bucket >= $2 AND bucket < $3 AND country_code <> ''
		 GROUP BY country_code
		 ORDER BY SUM(cnt) DESC
		 LIMIT 50`, q.Kind, q.Start, q.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s geodomain.CountryStat
		if err := rows.Scan(&s.CountryCode, &s.Count); err != nil {
			return nil, err
		}
		result.Countries = append(result.Countries, s)
		result.Total += s.Count
	}
	return result, rows.Err()
}

// QueryGeoClusters 对近期防火墙拦截做 DBSCAN 空间聚类（管理端按需调用，带时间窗口）。
// epsDegrees 为聚类半径（度，约 1° ≈ 111km）；minPoints 为成簇最小样本数。
func (r *Repository) QueryGeoClusters(ctx context.Context, since time.Time, epsDegrees float64, minPoints, limit int) ([]geodomain.Cluster, error) {
	if epsDegrees <= 0 {
		epsDegrees = 0.5
	}
	if minPoints <= 0 {
		minPoints = 10
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		WITH pts AS (
			SELECT ip, reason, geom::geometry AS g,
			       ST_ClusterDBSCAN(geom::geometry, eps := $2, minpoints := $3) OVER () AS cid
			  FROM firewall_logs
			 WHERE blocked_at > $1 AND geom IS NOT NULL
		)
		SELECT cid, COUNT(*), COUNT(DISTINCT ip),
		       ST_Y(ST_Centroid(ST_Collect(g))), ST_X(ST_Centroid(ST_Collect(g))),
		       mode() WITHIN GROUP (ORDER BY reason)
		  FROM pts
		 WHERE cid IS NOT NULL
		 GROUP BY cid
		 ORDER BY COUNT(*) DESC
		 LIMIT $4`, since, epsDegrees, minPoints, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []geodomain.Cluster
	for rows.Next() {
		var c geodomain.Cluster
		if err := rows.Scan(&c.ClusterID, &c.Hits, &c.UniqueIPs, &c.CenterLat, &c.CenterLng, &c.TopReason); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ──────────────────────────────────────
// 分区维护（login_geo_events）
// ──────────────────────────────────────

// EnsureLoginGeoPartitions 确保当月与下月分区存在（Worker 定时调用）。
func (r *Repository) EnsureLoginGeoPartitions(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `SELECT ensure_login_geo_partition(NOW()::date)`); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `SELECT ensure_login_geo_partition((NOW() + INTERVAL '1 month')::date)`)
	return err
}

var loginGeoPartitionPattern = regexp.MustCompile(`^login_geo_events_(\d{6})$`)

// DropLoginGeoPartitionsBefore 删除保留期之外的月分区（DROP 替代 DELETE，瞬时完成）。
// 返回删除的分区名。
func (r *Repository) DropLoginGeoPartitionsBefore(ctx context.Context, retainMonths int) ([]string, error) {
	if retainMonths < 1 {
		retainMonths = 6
	}
	rows, err := r.pool.Query(ctx, `
		SELECT c.relname
		  FROM pg_inherits i
		  JOIN pg_class c ON c.oid = i.inhrelid
		  JOIN pg_class p ON p.oid = i.inhparent
		 WHERE p.relname = 'login_geo_events'`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().AddDate(0, -retainMonths, 0).Format("200601")
	var dropped []string
	for _, name := range names {
		m := loginGeoPartitionPattern.FindStringSubmatch(name)
		if m == nil || m[1] >= cutoff {
			continue
		}
		// 分区名来自系统目录且匹配固定模式，可安全拼接标识符
		if _, err := r.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, name)); err != nil {
			return dropped, err
		}
		dropped = append(dropped, name)
	}
	return dropped, nil
}
