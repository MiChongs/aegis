package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	userdomain "aegis/internal/domain/user"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool              *pgxpool.Pool
	levelCacheMu      sync.RWMutex
	levelCache        []pointdomain.LevelConfig
	levelCacheExpires time.Time
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const levelCacheTTL = 5 * time.Minute

var ErrInsufficientIntegral = errors.New("postgres: insufficient integral")

type queryExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type levelRecordRow struct {
	UserID              int64
	AppID               int64
	CurrentLevel        int
	CurrentExperience   int64
	TotalExperience     int64
	NextLevelExperience *int64
	LevelProgress       float64
	HighestLevel        int
	LevelUpCount        int
	LastLevelUpAt       *time.Time
}

type levelState struct {
	Config             *pointdomain.LevelConfig
	NextConfig         *pointdomain.LevelConfig
	CurrentLevel       int
	CurrentLevelName   string
	NextLevelName      string
	ExpMultiplier      float64
	TotalExperience    int64
	ExpInCurrentLevel  int64
	ExpRangeForLevel   int64
	ExpToNextLevel     int64
	CurrentLevelMinExp int64
	NextLevelMinExp    *int64
	LevelProgress      float64
	IsMaxLevel         bool
}

func (r *Repository) GetUserByAppAndAccount(ctx context.Context, appID int64, account string) (*userdomain.User, error) {
	query := `SELECT id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at FROM users WHERE appid = $1 AND account = $2 LIMIT 1`
	return scanUser(r.pool.QueryRow(ctx, query, appID, account))
}

func (r *Repository) GetAppByID(ctx context.Context, appID int64) (*appdomain.App, error) {
	query := `SELECT id, name, COALESCE(app_key, ''), status, COALESCE(disabled_reason, ''), register_status, COALESCE(disabled_register_reason, ''), login_status, COALESCE(disabled_login_reason, ''), COALESCE(settings, '{}'::jsonb), created_at, updated_at FROM apps WHERE id = $1 LIMIT 1`
	return scanApp(r.pool.QueryRow(ctx, query, appID))
}

func (r *Repository) GetAppByKey(ctx context.Context, appKey string) (*appdomain.App, error) {
	query := `SELECT id, name, COALESCE(app_key, ''), status, COALESCE(disabled_reason, ''), register_status, COALESCE(disabled_register_reason, ''), login_status, COALESCE(disabled_login_reason, ''), COALESCE(settings, '{}'::jsonb), created_at, updated_at FROM apps WHERE app_key = $1 LIMIT 1`
	return scanApp(r.pool.QueryRow(ctx, query, appKey))
}

func (r *Repository) ListApps(ctx context.Context) ([]appdomain.App, error) {
	query := `SELECT id, name, COALESCE(app_key, ''), status, COALESCE(disabled_reason, ''), register_status, COALESCE(disabled_register_reason, ''), login_status, COALESCE(disabled_login_reason, ''), COALESCE(settings, '{}'::jsonb), created_at, updated_at FROM apps ORDER BY id ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.App, 0, 8)
	for rows.Next() {
		item, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertApp(ctx context.Context, item appdomain.App) (*appdomain.App, error) {
	const returnCols = `id, name, COALESCE(app_key, ''), status, COALESCE(disabled_reason, ''), register_status, COALESCE(disabled_register_reason, ''), login_status, COALESCE(disabled_login_reason, ''), COALESCE(settings, '{}'::jsonb), created_at, updated_at`
	settingsJSON, _ := json.Marshal(item.Settings)

	if item.ID == 0 {
		// 新建：不传 id（BIGSERIAL 自动生成），不传 app_key（DEFAULT gen_random_uuid()）
		return scanApp(r.pool.QueryRow(ctx,
			`INSERT INTO apps (name, status, disabled_reason, register_status, disabled_register_reason, login_status, disabled_login_reason, settings, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			 RETURNING `+returnCols,
			item.Name, item.Status, nullableString(item.DisabledReason), item.RegisterStatus, nullableString(item.DisabledRegisterReason), item.LoginStatus, nullableString(item.DisabledLoginReason), settingsJSON))
	}

	// 更新：按 id 更新，不修改 app_key（不可更改）
	return scanApp(r.pool.QueryRow(ctx,
		`UPDATE apps SET
			name = $2, status = $3, disabled_reason = $4,
			register_status = $5, disabled_register_reason = $6,
			login_status = $7, disabled_login_reason = $8,
			settings = $9, updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+returnCols,
		item.ID, item.Name, item.Status, nullableString(item.DisabledReason), item.RegisterStatus, nullableString(item.DisabledRegisterReason), item.LoginStatus, nullableString(item.DisabledLoginReason), settingsJSON))
}

// UpdateAppSettings 仅更新应用的 settings JSONB 字段
func (r *Repository) UpdateAppSettings(ctx context.Context, appID int64, settings map[string]any) (*appdomain.App, error) {
	settingsJSON, _ := json.Marshal(settings)
	return scanApp(r.pool.QueryRow(ctx,
		`UPDATE apps SET settings = $2, updated_at = NOW()
		 WHERE id = $1
		 RETURNING id, name, COALESCE(app_key, ''), status, COALESCE(disabled_reason, ''), register_status, COALESCE(disabled_register_reason, ''), login_status, COALESCE(disabled_login_reason, ''), COALESCE(settings, '{}'::jsonb), created_at, updated_at`,
		appID, settingsJSON))
}

func (r *Repository) GetAppStats(ctx context.Context, appID int64) (*appdomain.Stats, error) {
	today := "date_trunc('day', NOW())"
	query := fmt.Sprintf(`
WITH
  user_stats AS (
    SELECT
      COUNT(*) AS total,
      COUNT(*) FILTER (WHERE enabled = true) AS enabled,
      COUNT(*) FILTER (WHERE enabled = false) AS disabled,
      COUNT(*) FILTER (WHERE created_at >= %s) AS new_today,
      COUNT(*) FILTER (WHERE created_at >= %s - INTERVAL '6 day') AS new_7d,
      COUNT(*) FILTER (WHERE created_at >= %s - INTERVAL '29 day') AS new_30d
    FROM users WHERE appid = $1
  ),
  login_stats AS (
    SELECT
      COUNT(*) FILTER (WHERE status = 'success') AS ok,
      COUNT(*) FILTER (WHERE status <> 'success') AS fail
    FROM login_audit_logs WHERE appid = $1 AND created_at >= %s
  )
SELECT
  u.total, u.enabled, u.disabled,
  (SELECT COUNT(*) FROM banners WHERE appid = $1),
  (SELECT COUNT(*) FROM notices WHERE appid = $1),
  (SELECT COUNT(*) FROM oauth_bindings WHERE appid = $1),
  u.new_today, u.new_7d, u.new_30d,
  l.ok, l.fail
FROM user_stats u, login_stats l`, today, today, today, today)

	item := appdomain.Stats{AppID: appID}
	if err := r.pool.QueryRow(ctx, query, appID).Scan(
		&item.TotalUsers,
		&item.EnabledUsers,
		&item.DisabledUsers,
		&item.BannerCount,
		&item.NoticeCount,
		&item.OAuthBindCount,
		&item.NewUsersToday,
		&item.NewUsersLast7Days,
		&item.NewUsersLast30Days,
		&item.LoginSuccessToday,
		&item.LoginFailureToday,
	); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetAppUserTrend(ctx context.Context, appID int64, days int) (*appdomain.UserTrend, error) {
	if days <= 0 {
		days = 7
	}
	query := `WITH days AS (
    SELECT generate_series(
        date_trunc('day', NOW()) - (($2::int - 1) * INTERVAL '1 day'),
        date_trunc('day', NOW()),
        INTERVAL '1 day'
    )::date AS day
),
counts AS (
    SELECT DATE(created_at) AS day, COUNT(*) AS count
    FROM users
    WHERE appid = $1
      AND created_at >= date_trunc('day', NOW()) - (($2::int - 1) * INTERVAL '1 day')
    GROUP BY DATE(created_at)
)
SELECT days.day, COALESCE(counts.count, 0) AS count
FROM days
LEFT JOIN counts ON counts.day = days.day
ORDER BY days.day ASC`
	rows, err := r.pool.Query(ctx, query, appID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := &appdomain.UserTrend{
		AppID:  appID,
		Days:   days,
		Series: make([]appdomain.UserTrendPoint, 0, days),
	}
	for rows.Next() {
		var day time.Time
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}
		result.TotalNew += count
		result.Series = append(result.Series, appdomain.UserTrendPoint{
			Date:  day.Format("2006-01-02"),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) GetAppRegionStats(ctx context.Context, appID int64, query appdomain.RegionStatsQuery) (*appdomain.RegionStatsResult, error) {
	regionType := strings.TrimSpace(strings.ToLower(query.Type))
	if regionType == "" {
		regionType = "province"
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	result := &appdomain.RegionStatsResult{
		AppID: appID,
		Type:  regionType,
		Items: make([]appdomain.RegionStatItem, 0, limit),
	}

	if regionType == "country" {
		sql := `SELECT
	COALESCE(
		NULLIF(BTRIM(p.extra->>'register_country'), ''),
		NULLIF(BTRIM(p.extra->>'country'), ''),
		CASE
			WHEN NULLIF(BTRIM(p.extra->>'register_province'), '') IS NOT NULL THEN '中国'
			ELSE '未知'
		END
	) AS country,
	COALESCE(
		NULLIF(UPPER(BTRIM(p.extra->>'register_country_code')), ''),
		NULLIF(UPPER(BTRIM(p.extra->>'countryCode')), ''),
		CASE
			WHEN NULLIF(BTRIM(p.extra->>'register_province'), '') IS NOT NULL THEN 'CN'
			ELSE ''
		END
	) AS country_code,
	COUNT(*) AS count
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
GROUP BY country, country_code
ORDER BY count DESC, country ASC
LIMIT $2`
		rows, err := r.pool.Query(ctx, sql, appID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var item appdomain.RegionStatItem
			if err := rows.Scan(&item.Region, &item.Code, &item.Count); err != nil {
				return nil, err
			}
			result.Total += item.Count
			result.Items = append(result.Items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	if regionType == "city" {
		sql := `SELECT
	COALESCE(NULLIF(BTRIM(p.extra->>'register_province'), ''), '未知') AS province,
	COALESCE(NULLIF(BTRIM(p.extra->>'register_city'), ''), '未知') AS city,
	COUNT(*) AS count
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
GROUP BY province, city
ORDER BY count DESC, province ASC, city ASC
LIMIT $2`
		rows, err := r.pool.Query(ctx, sql, appID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var item appdomain.RegionStatItem
			if err := rows.Scan(&item.Parent, &item.Region, &item.Count); err != nil {
				return nil, err
			}
			item.ParentPath = item.Parent
			result.Total += item.Count
			result.Items = append(result.Items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	if regionType == "district" || regionType == "county" {
		sql := `SELECT
	COALESCE(NULLIF(BTRIM(p.extra->>'register_province'), ''), NULLIF(BTRIM(p.extra->>'province'), ''), '未知') AS province,
	COALESCE(NULLIF(BTRIM(p.extra->>'register_city'), ''), NULLIF(BTRIM(p.extra->>'city'), ''), '未知') AS city,
	COALESCE(
		NULLIF(BTRIM(p.extra->>'register_district'), ''),
		NULLIF(BTRIM(p.extra->>'district'), ''),
		NULLIF(BTRIM(p.extra->>'register_county'), ''),
		NULLIF(BTRIM(p.extra->>'county'), ''),
		'未知'
	) AS district,
	COUNT(*) AS count
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
GROUP BY province, city, district
ORDER BY count DESC, province ASC, city ASC, district ASC
LIMIT $2`
		rows, err := r.pool.Query(ctx, sql, appID, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var province string
			var item appdomain.RegionStatItem
			if err := rows.Scan(&province, &item.Parent, &item.Region, &item.Count); err != nil {
				return nil, err
			}
			item.ParentPath = province + "/" + item.Parent
			result.Total += item.Count
			result.Items = append(result.Items, item)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	sql := `SELECT
	COALESCE(NULLIF(BTRIM(p.extra->>'register_province'), ''), '未知') AS province,
	COUNT(*) AS count
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
GROUP BY province
ORDER BY count DESC, province ASC
LIMIT $2`
	rows, err := r.pool.Query(ctx, sql, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item appdomain.RegionStatItem
		if err := rows.Scan(&item.Region, &item.Count); err != nil {
			return nil, err
		}
		result.Total += item.Count
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repository) GetAppAuthSourceStats(ctx context.Context, appID int64) (*appdomain.AuthSourceStats, error) {
	query := `SELECT
	(SELECT COUNT(*) FROM users WHERE appid = $1) AS total_users,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND COALESCE(password_hash, '') <> '') AS password_users,
	(SELECT COUNT(DISTINCT user_id) FROM oauth_bindings WHERE appid = $1) AS oauth_bound_users`
	var result appdomain.AuthSourceStats
	result.AppID = appID
	if err := r.pool.QueryRow(ctx, query, appID).Scan(&result.TotalUsers, &result.PasswordUsers, &result.OAuthBoundUsers); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT provider, COUNT(*) AS count FROM oauth_bindings WHERE appid = $1 GROUP BY provider ORDER BY count DESC, provider ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result.ProviderBindings = make([]appdomain.AuthSourceStatItem, 0, 4)
	for rows.Next() {
		var item appdomain.AuthSourceStatItem
		if err := rows.Scan(&item.Source, &item.Count); err != nil {
			return nil, err
		}
		result.ProviderBindings = append(result.ProviderBindings, item)
	}
	return &result, rows.Err()
}

func (r *Repository) GetPasswordPolicyStats(ctx context.Context, appID int64, minScore int) (*appdomain.PasswordPolicyStats, error) {
	query := `SELECT
	(SELECT COUNT(*) FROM users WHERE appid = $1) AS total_users,
	(SELECT COUNT(*) FROM users WHERE appid = $1 AND COALESCE(password_hash, '') <> '') AS password_users,
	(SELECT COUNT(*) FROM users u
	 LEFT JOIN user_profiles p ON p.user_id = u.id
	 WHERE u.appid = $1
	   AND COALESCE(u.password_hash, '') <> ''
	   AND COALESCE((p.extra->>'password_strength_score')::int, 0) >= $2) AS compliant_users,
	(SELECT COUNT(*) FROM users u
	 LEFT JOIN user_profiles p ON p.user_id = u.id
	 WHERE u.appid = $1
	   AND COALESCE((p.extra->>'password_change_required')::boolean, false) = true) AS need_change_users`
	var stats appdomain.PasswordPolicyStats
	if err := r.pool.QueryRow(ctx, query, appID, minScore).Scan(
		&stats.TotalUsers,
		&stats.PasswordUsers,
		&stats.CompliantUsers,
		&stats.NeedChangeUsers,
	); err != nil {
		return nil, err
	}
	if stats.TotalUsers > 0 {
		stats.ComplianceRate = int64(math.Round(float64(stats.CompliantUsers) / float64(stats.TotalUsers) * 100))
		stats.NeedChangeRate = int64(math.Round(float64(stats.NeedChangeUsers) / float64(stats.TotalUsers) * 100))
	}
	return &stats, nil
}

func (r *Repository) ListLoginAuditsByApp(ctx context.Context, appID int64, query appdomain.LoginAuditQuery) ([]appdomain.LoginAuditItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	baseQuery, args := buildLoginAuditFilter(appID, query.Keyword, query.Status)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := `SELECT
	l.id,
	l.user_id,
	l.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	l.login_type,
	COALESCE(l.provider, ''),
	COALESCE(l.token_jti, ''),
	COALESCE(HOST(l.login_ip), ''),
	COALESCE(l.device_id, ''),
	COALESCE(l.user_agent, ''),
	l.status,
	COALESCE(l.metadata, '{}'::jsonb),
	l.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]appdomain.LoginAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanLoginAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListLoginAuditsByAppForExport(ctx context.Context, appID int64, query appdomain.LoginAuditExportQuery) ([]appdomain.LoginAuditItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildLoginAuditFilter(appID, query.Keyword, query.Status)
	sql := `SELECT
	l.id,
	l.user_id,
	l.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	l.login_type,
	COALESCE(l.provider, ''),
	COALESCE(l.token_jti, ''),
	COALESCE(HOST(l.login_ip), ''),
	COALESCE(l.device_id, ''),
	COALESCE(l.user_agent, ''),
	l.status,
	COALESCE(l.metadata, '{}'::jsonb),
	l.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.LoginAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanLoginAudit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListSessionAuditsByApp(ctx context.Context, appID int64, query appdomain.SessionAuditQuery) ([]appdomain.SessionAuditItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	baseQuery, args := buildSessionAuditFilter(appID, query.Keyword, query.EventType)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := `SELECT
	s.id,
	s.user_id,
	s.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	COALESCE(s.token_jti, ''),
	s.event_type,
	COALESCE(s.metadata, '{}'::jsonb),
	s.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY s.created_at DESC, s.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]appdomain.SessionAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanSessionAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListSessionAuditsByAppForExport(ctx context.Context, appID int64, query appdomain.SessionAuditExportQuery) ([]appdomain.SessionAuditItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildSessionAuditFilter(appID, query.Keyword, query.EventType)
	sql := `SELECT
	s.id,
	s.user_id,
	s.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	COALESCE(s.token_jti, ''),
	s.event_type,
	COALESCE(s.metadata, '{}'::jsonb),
	s.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY s.created_at DESC, s.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.SessionAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanSessionAudit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListLoginAuditsByUser(ctx context.Context, appID int64, userID int64, query userdomain.LoginAuditQuery) ([]appdomain.LoginAuditItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	baseQuery, args := buildUserLoginAuditFilter(appID, userID, query.Status)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := `SELECT
	l.id,
	l.user_id,
	l.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	l.login_type,
	COALESCE(l.provider, ''),
	COALESCE(l.token_jti, ''),
	COALESCE(HOST(l.login_ip), ''),
	COALESCE(l.device_id, ''),
	COALESCE(l.user_agent, ''),
	l.status,
	COALESCE(l.metadata, '{}'::jsonb),
	l.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]appdomain.LoginAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanLoginAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListLoginAuditsByUserForExport(ctx context.Context, appID int64, userID int64, query userdomain.LoginAuditExportQuery) ([]appdomain.LoginAuditItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildUserLoginAuditFilter(appID, userID, query.Status)

	sql := `SELECT
	l.id,
	l.user_id,
	l.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	l.login_type,
	COALESCE(l.provider, ''),
	COALESCE(l.token_jti, ''),
	COALESCE(HOST(l.login_ip), ''),
	COALESCE(l.device_id, ''),
	COALESCE(l.user_agent, ''),
	l.status,
	COALESCE(l.metadata, '{}'::jsonb),
	l.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY l.created_at DESC, l.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.LoginAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanLoginAudit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListSessionAuditsByUser(ctx context.Context, appID int64, userID int64, query userdomain.SessionAuditQuery) ([]appdomain.SessionAuditItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	baseQuery, args := buildUserSessionAuditFilter(appID, userID, query.EventType)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := `SELECT
	s.id,
	s.user_id,
	s.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	COALESCE(s.token_jti, ''),
	s.event_type,
	COALESCE(s.metadata, '{}'::jsonb),
	s.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY s.created_at DESC, s.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]appdomain.SessionAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanSessionAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListSessionAuditsByUserForExport(ctx context.Context, appID int64, userID int64, query userdomain.SessionAuditExportQuery) ([]appdomain.SessionAuditItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildUserSessionAuditFilter(appID, userID, query.EventType)

	sql := `SELECT
	s.id,
	s.user_id,
	s.appid,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	COALESCE(s.token_jti, ''),
	s.event_type,
	COALESCE(s.metadata, '{}'::jsonb),
	s.created_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY s.created_at DESC, s.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.SessionAuditItem, 0, limit)
	for rows.Next() {
		item, err := scanSessionAudit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListActiveBanners(ctx context.Context, appID int64, now time.Time) ([]appdomain.Banner, error) {
	query := `SELECT id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at
FROM banners
WHERE appid = $1
  AND status = true
  AND (start_time IS NULL OR start_time <= $2)
  AND (end_time IS NULL OR end_time >= $2)
ORDER BY position ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, appID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Banner, 0, 8)
	ids := make([]int64, 0, 8)
	for rows.Next() {
		item, err := scanBanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, _ = r.pool.Exec(ctx, `UPDATE banners SET view_count = view_count + 1, updated_at = NOW() WHERE id = ANY($1)`, ids)
	}
	return items, nil
}

func (r *Repository) ListBanners(ctx context.Context, appID int64) ([]appdomain.Banner, error) {
	query := `SELECT id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at
FROM banners
WHERE appid = $1
ORDER BY position ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Banner, 0, 8)
	for rows.Next() {
		item, err := scanBanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetBannerByID(ctx context.Context, appID int64, bannerID int64) (*appdomain.Banner, error) {
	query := `SELECT id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at FROM banners WHERE appid = $1 AND id = $2 LIMIT 1`
	return scanBanner(r.pool.QueryRow(ctx, query, appID, bannerID))
}

func (r *Repository) UpsertBanner(ctx context.Context, appID int64, item appdomain.Banner) (*appdomain.Banner, error) {
	if item.ID > 0 {
		query := `UPDATE banners
SET header = $3,
	title = $4,
	content = $5,
	url = $6,
	type = $7,
	position = $8,
	status = $9,
	start_time = $10,
	end_time = $11,
	updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at`
		return scanBanner(r.pool.QueryRow(ctx, query, appID, item.ID, nullableString(item.Header), item.Title, nullableString(item.Content), nullableString(item.URL), item.Type, item.Position, item.Status, item.StartTime, item.EndTime))
	}

	query := `INSERT INTO banners (appid, header, title, content, url, type, position, status, start_time, end_time, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
RETURNING id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at`
	return scanBanner(r.pool.QueryRow(ctx, query, appID, nullableString(item.Header), item.Title, nullableString(item.Content), nullableString(item.URL), item.Type, item.Position, item.Status, item.StartTime, item.EndTime))
}

func (r *Repository) DeleteBanner(ctx context.Context, appID int64, bannerID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM banners WHERE appid = $1 AND id = $2`, appID, bannerID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteBanners(ctx context.Context, appID int64, bannerIDs []int64) (int64, error) {
	if len(bannerIDs) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM banners WHERE appid = $1 AND id = ANY($2)`, appID, bannerIDs)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) ListNotices(ctx context.Context, appID int64) ([]appdomain.Notice, error) {
	query := `SELECT id, COALESCE(title, ''), content, created_at, updated_at FROM notices WHERE appid = $1 ORDER BY created_at DESC, id DESC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Notice, 0, 8)
	for rows.Next() {
		item, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetNoticeByID(ctx context.Context, appID int64, noticeID int64) (*appdomain.Notice, error) {
	query := `SELECT id, COALESCE(title, ''), content, created_at, updated_at FROM notices WHERE appid = $1 AND id = $2 LIMIT 1`
	return scanNotice(r.pool.QueryRow(ctx, query, appID, noticeID))
}

func (r *Repository) UpsertNotice(ctx context.Context, appID int64, item appdomain.Notice) (*appdomain.Notice, error) {
	if item.ID > 0 {
		query := `UPDATE notices
SET title = $3,
	content = $4,
	updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING id, COALESCE(title, ''), content, created_at, updated_at`
		return scanNotice(r.pool.QueryRow(ctx, query, appID, item.ID, nullableString(item.Title), item.Content))
	}

	query := `INSERT INTO notices (appid, title, content, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING id, COALESCE(title, ''), content, created_at, updated_at`
	return scanNotice(r.pool.QueryRow(ctx, query, appID, nullableString(item.Title), item.Content))
}

func (r *Repository) DeleteNotice(ctx context.Context, appID int64, noticeID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM notices WHERE appid = $1 AND id = $2`, appID, noticeID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteNotices(ctx context.Context, appID int64, noticeIDs []int64) (int64, error) {
	if len(noticeIDs) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM notices WHERE appid = $1 AND id = ANY($2)`, appID, noticeIDs)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) GetUserByID(ctx context.Context, userID int64) (*userdomain.User, error) {
	query := `SELECT id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at FROM users WHERE id = $1 LIMIT 1`
	return scanUser(r.pool.QueryRow(ctx, query, userID))
}

func (r *Repository) GetUserProfileByUserID(ctx context.Context, userID int64) (*userdomain.Profile, error) {
	query := `SELECT user_id, COALESCE(nickname, ''), COALESCE(avatar, ''), COALESCE(email, ''), COALESCE(phone, ''), birthday, COALESCE(bio, ''), COALESCE(contacts, '[]'::jsonb), COALESCE(extra, '{}'::jsonb), updated_at FROM user_profiles WHERE user_id = $1 LIMIT 1`
	var profile userdomain.Profile
	var contactsBytes, extraBytes []byte
	if err := r.pool.QueryRow(ctx, query, userID).Scan(&profile.UserID, &profile.Nickname, &profile.Avatar, &profile.Email, &profile.Phone, &profile.Birthday, &profile.Bio, &contactsBytes, &extraBytes, &profile.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(contactsBytes, &profile.Contacts)
	_ = json.Unmarshal(extraBytes, &profile.Extra)
	return &profile, nil
}

func (r *Repository) FindUserIDByProfileEmail(ctx context.Context, appID int64, email string) (int64, error) {
	query := `SELECT u.id
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND LOWER(COALESCE(p.email, '')) = LOWER($2)
LIMIT 1`
	var userID int64
	if err := r.pool.QueryRow(ctx, query, appID, strings.TrimSpace(email)).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return userID, nil
}

func (r *Repository) FindUserIDByProfilePhone(ctx context.Context, appID int64, phone string) (int64, error) {
	query := `SELECT u.id
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND COALESCE(p.phone, '') = $2
LIMIT 1`
	var userID int64
	if err := r.pool.QueryRow(ctx, query, appID, strings.TrimSpace(phone)).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return userID, nil
}

func (r *Repository) ListAdminUsersByApp(ctx context.Context, appID int64, keyword string, enabled *bool, page int, limit int) ([]userdomain.AdminUserView, int64, error) {
	return r.ListAdminUsersByAppQuery(ctx, appID, userdomain.AdminUserQuery{
		Keyword: keyword,
		Enabled: enabled,
		Page:    page,
		Limit:   limit,
	}, page, limit)
}

func (r *Repository) ListAdminUsersByAppQuery(ctx context.Context, appID int64, adminQuery userdomain.AdminUserQuery, page int, limit int) ([]userdomain.AdminUserView, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	adminQuery.Keyword = strings.TrimSpace(adminQuery.Keyword)

	if isAdminUserFastPath(adminQuery) {
		return r.listAdminUsersByAppFast(ctx, appID, adminQuery.Enabled, page, limit, offset)
	}

	baseQuery, args := buildAdminUserListBaseQuery(appID, adminQuery)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT
	u.id,
	u.appid,
	u.account,
	u.integral,
	u.experience,
	u.enabled,
	u.disabled_end_time,
	u.vip_expire_at,
	u.created_at,
	u.updated_at,
	COALESCE(p.nickname, ''),
	COALESCE(p.avatar, ''),
	COALESCE(p.email, ''),
	COALESCE(p.phone, ''),
	COALESCE(p.extra, '{}'::jsonb)` + baseQuery +
		fmt.Sprintf(`
ORDER BY u.created_at DESC, u.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.AdminUserView, 0, limit)
	for rows.Next() {
		item, err := scanAdminUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) listAdminUsersByAppFast(ctx context.Context, appID int64, enabled *bool, page int, limit int, offset int) ([]userdomain.AdminUserView, int64, error) {
	countQuery := `SELECT COUNT(*) FROM users WHERE appid = $1`
	countArgs := []any{appID}
	if enabled != nil {
		countQuery += " AND enabled = $2"
		countArgs = append(countArgs, *enabled)
	}

	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `WITH page_users AS (
    SELECT id, appid, account, integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at
    FROM users
    WHERE appid = $1`
	args := []any{appID}
	if enabled != nil {
		query += fmt.Sprintf(" AND enabled = $%d", len(args)+1)
		args = append(args, *enabled)
	}
	query += fmt.Sprintf(`
    ORDER BY created_at DESC, id DESC
    LIMIT $%d OFFSET $%d
)
SELECT
    u.id,
    u.appid,
    u.account,
    u.integral,
    u.experience,
    u.enabled,
    u.disabled_end_time,
    u.vip_expire_at,
    u.created_at,
    u.updated_at,
    COALESCE(p.nickname, ''),
    COALESCE(p.avatar, ''),
    COALESCE(p.email, ''),
    COALESCE(p.phone, ''),
    COALESCE(p.extra, '{}'::jsonb)
FROM page_users u
LEFT JOIN user_profiles p ON p.user_id = u.id
ORDER BY u.created_at DESC, u.id DESC`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.AdminUserView, 0, limit)
	for rows.Next() {
		item, err := scanAdminUser(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) GetAdminUserByApp(ctx context.Context, appID int64, userID int64) (*userdomain.AdminUserView, error) {
	query := `SELECT
	u.id,
	u.appid,
	u.account,
	u.integral,
	u.experience,
	u.enabled,
	u.disabled_end_time,
	u.vip_expire_at,
	u.created_at,
	u.updated_at,
	COALESCE(p.nickname, ''),
	COALESCE(p.avatar, ''),
	COALESCE(p.email, ''),
	COALESCE(p.phone, ''),
	COALESCE(p.extra, '{}'::jsonb)
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND u.id = $2
LIMIT 1`
	return scanAdminUser(r.pool.QueryRow(ctx, query, appID, userID))
}

func (r *Repository) ListAdminUsersForExport(ctx context.Context, appID int64, keyword string, enabled *bool, limit int) ([]userdomain.AdminUserView, error) {
	return r.ListAdminUsersForExportQuery(ctx, appID, userdomain.AdminUserQuery{
		Keyword: keyword,
		Enabled: enabled,
		Limit:   limit,
	}, limit)
}

func (r *Repository) ListAdminUsersForExportQuery(ctx context.Context, appID int64, adminQuery userdomain.AdminUserQuery, limit int) ([]userdomain.AdminUserView, error) {
	if limit <= 0 {
		limit = 5000
	}
	baseQuery, args := buildAdminUserListBaseQuery(appID, adminQuery)
	query := `SELECT
	u.id,
	u.appid,
	u.account,
	u.integral,
	u.experience,
	u.enabled,
	u.disabled_end_time,
	u.vip_expire_at,
	u.created_at,
	u.updated_at,
	COALESCE(p.nickname, ''),
	COALESCE(p.avatar, ''),
	COALESCE(p.email, ''),
	COALESCE(p.phone, ''),
	COALESCE(p.extra, '{}'::jsonb)` + baseQuery +
		fmt.Sprintf(`
ORDER BY u.created_at DESC, u.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.AdminUserView, 0, limit)
	for rows.Next() {
		item, err := scanAdminUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAdminUsersByIDs(ctx context.Context, appID int64, userIDs []int64) ([]userdomain.AdminUserView, error) {
	if len(userIDs) == 0 {
		return []userdomain.AdminUserView{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT
	u.id,
	u.appid,
	u.account,
	u.integral,
	u.experience,
	u.enabled,
	u.disabled_end_time,
	u.vip_expire_at,
	u.created_at,
	u.updated_at,
	COALESCE(p.nickname, ''),
	COALESCE(p.avatar, ''),
	COALESCE(p.email, ''),
	COALESCE(p.phone, ''),
	COALESCE(p.extra, '{}'::jsonb)
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND u.id = ANY($2)
ORDER BY array_position($2::bigint[], u.id)`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.AdminUserView, 0, len(userIDs))
	for rows.Next() {
		item, err := scanAdminUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAdminUserSearchSourcesByApp(ctx context.Context, appID int64, afterUpdatedAt time.Time, afterUserID int64, limit int) ([]userdomain.AdminUserSearchSource, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `SELECT
    u.id,
    u.appid,
    u.account,
    COALESCE(p.nickname, ''),
    COALESCE(p.email, ''),
    COALESCE(p.phone, ''),
    COALESCE(p.extra->>'register_ip', ''),
    u.enabled,
    u.created_at,
    GREATEST(
      u.updated_at,
      COALESCE(p.updated_at, u.updated_at),
      u.created_at
    ) AS source_updated_at
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
  AND (
    GREATEST(
      u.updated_at,
      COALESCE(p.updated_at, u.updated_at),
      u.created_at
    ) > $2
    OR (
      GREATEST(
        u.updated_at,
        COALESCE(p.updated_at, u.updated_at),
        u.created_at
      ) = $2
      AND u.id > $3
    )
  )
ORDER BY source_updated_at ASC, u.id ASC
LIMIT $4`, appID, afterUpdatedAt.UTC(), afterUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.AdminUserSearchSource, 0, limit)
	for rows.Next() {
		var item userdomain.AdminUserSearchSource
		if err := rows.Scan(&item.UserID, &item.AppID, &item.Account, &item.Nickname, &item.Email, &item.Phone, &item.RegisterIP, &item.Enabled, &item.CreatedAt, &item.SourceUpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func isAdminUserFastPath(adminQuery userdomain.AdminUserQuery) bool {
	return strings.TrimSpace(adminQuery.Keyword) == "" &&
		strings.TrimSpace(adminQuery.Account) == "" &&
		strings.TrimSpace(adminQuery.Nickname) == "" &&
		strings.TrimSpace(adminQuery.Email) == "" &&
		strings.TrimSpace(adminQuery.Phone) == "" &&
		strings.TrimSpace(adminQuery.RegisterIP) == "" &&
		adminQuery.UserID == nil &&
		adminQuery.CreatedFrom == nil &&
		adminQuery.CreatedTo == nil
}

func buildAdminUserListBaseQuery(appID int64, adminQuery userdomain.AdminUserQuery) (string, []any) {
	baseQuery := ` FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1`
	args := []any{appID}

	appendLike := func(expression string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		baseQuery += fmt.Sprintf(" AND %s ILIKE $%d", expression, len(args)+1)
		args = append(args, "%"+value+"%")
	}

	keyword := strings.TrimSpace(adminQuery.Keyword)
	if keyword != "" {
		baseQuery += fmt.Sprintf(`
  AND (
    u.account ILIKE $%d
    OR COALESCE(p.nickname, '') ILIKE $%d
    OR COALESCE(p.email, '') ILIKE $%d
    OR COALESCE(p.phone, '') ILIKE $%d
    OR COALESCE(p.extra->>'register_ip', '') ILIKE $%d
    OR CAST(u.id AS TEXT) ILIKE $%d
  )`, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, "%"+keyword+"%")
	}

	appendLike("u.account", adminQuery.Account)
	appendLike("COALESCE(p.nickname, '')", adminQuery.Nickname)
	appendLike("COALESCE(p.email, '')", adminQuery.Email)
	appendLike("COALESCE(p.phone, '')", adminQuery.Phone)
	appendLike("COALESCE(p.extra->>'register_ip', '')", adminQuery.RegisterIP)

	if adminQuery.UserID != nil && *adminQuery.UserID > 0 {
		baseQuery += fmt.Sprintf(" AND u.id = $%d", len(args)+1)
		args = append(args, *adminQuery.UserID)
	}
	if adminQuery.Enabled != nil {
		baseQuery += fmt.Sprintf(" AND u.enabled = $%d", len(args)+1)
		args = append(args, *adminQuery.Enabled)
	}
	if adminQuery.CreatedFrom != nil {
		baseQuery += fmt.Sprintf(" AND u.created_at >= $%d", len(args)+1)
		args = append(args, *adminQuery.CreatedFrom)
	}
	if adminQuery.CreatedTo != nil {
		baseQuery += fmt.Sprintf(" AND u.created_at <= $%d", len(args)+1)
		args = append(args, *adminQuery.CreatedTo)
	}
	return baseQuery, args
}

func (r *Repository) FilterExistingUserIDsByApp(ctx context.Context, appID int64, userIDs []int64) ([]int64, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id FROM users WHERE appid = $1 AND id = ANY($2) ORDER BY id ASC`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]int64, 0, len(userIDs))
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		items = append(items, userID)
	}
	return items, rows.Err()
}

func (r *Repository) UpdateAdminUserStatus(ctx context.Context, appID int64, userID int64, mutation userdomain.AdminUserStatusMutation) (*userdomain.AdminUserView, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	user, err := scanUser(tx.QueryRow(ctx, `SELECT id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	nextEnabled := user.Enabled
	if mutation.Enabled != nil {
		nextEnabled = *mutation.Enabled
	}
	nextDisabledEndTime := user.DisabledEndTime
	if mutation.ClearDisabledEndTime {
		nextDisabledEndTime = nil
	} else if mutation.DisabledEndTime != nil {
		value := mutation.DisabledEndTime.UTC()
		nextDisabledEndTime = &value
	}

	if _, err := tx.Exec(ctx, `UPDATE users SET enabled = $3, disabled_end_time = $4, updated_at = NOW() WHERE id = $1 AND appid = $2`, userID, appID, nextEnabled, nextDisabledEndTime); err != nil {
		return nil, err
	}

	if mutation.DisabledReason != nil {
		reason := strings.TrimSpace(*mutation.DisabledReason)
		extra := map[string]any{
			"disabled_reason": nil,
		}
		if reason != "" {
			extra["disabled_reason"] = reason
		}
		extraJSON, _ := json.Marshal(extra)
		if _, err := tx.Exec(ctx, `INSERT INTO user_profiles (user_id, extra, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    extra = COALESCE(user_profiles.extra, '{}'::jsonb) || EXCLUDED.extra,
    updated_at = NOW()`, userID, extraJSON); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return r.GetAdminUserByApp(ctx, appID, userID)
}

func (r *Repository) BatchUpdateAdminUserStatus(ctx context.Context, appID int64, userIDs []int64, mutation userdomain.AdminUserStatusMutation) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var nextEnabled any = nil
	if mutation.Enabled != nil {
		nextEnabled = *mutation.Enabled
	}
	var nextDisabledEndTime any = nil
	if mutation.ClearDisabledEndTime {
		nextDisabledEndTime = nil
	} else if mutation.DisabledEndTime != nil {
		nextDisabledEndTime = mutation.DisabledEndTime.UTC()
	}

	query := `UPDATE users
SET enabled = COALESCE($3, enabled),
    disabled_end_time = CASE
        WHEN $4::boolean = true THEN NULL
        WHEN $5::timestamptz IS NOT NULL THEN $5::timestamptz
        ELSE disabled_end_time
    END,
    updated_at = NOW()
WHERE appid = $1 AND id = ANY($2)`
	result, err := tx.Exec(ctx, query, appID, userIDs, nextEnabled, mutation.ClearDisabledEndTime, nextDisabledEndTime)
	if err != nil {
		return 0, err
	}

	if mutation.DisabledReason != nil {
		reason := strings.TrimSpace(*mutation.DisabledReason)
		extra := map[string]any{"disabled_reason": nil}
		if reason != "" {
			extra["disabled_reason"] = reason
		}
		extraJSON, _ := json.Marshal(extra)
		for _, userID := range userIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO user_profiles (user_id, extra, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    extra = COALESCE(user_profiles.extra, '{}'::jsonb) || EXCLUDED.extra,
    updated_at = NOW()`, userID, extraJSON); err != nil {
				return 0, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	tx = nil
	return result.RowsAffected(), nil
}

// DeleteUsersByApp 删除指定应用下的所有用户
func (r *Repository) DeleteUsersByApp(ctx context.Context, appID int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE appid = $1`, appID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteApp 删除应用（关联的 banners/notices/sites 等通过 CASCADE 自动清理）
func (r *Repository) DeleteApp(ctx context.Context, appID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM apps WHERE id = $1`, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("应用不存在")
	}
	return nil
}

// DeleteUserByApp 硬删除指定应用下的用户（CASCADE 会清理关联表）
func (r *Repository) DeleteUserByApp(ctx context.Context, appID int64, userID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1 AND appid = $2`, userID, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("用户不存在或已删除")
	}
	return nil
}

func (r *Repository) HasAnyUserRegisteredFromIP(ctx context.Context, ip string) (bool, error) {
	query := `SELECT EXISTS(
SELECT 1
FROM user_profiles p
JOIN users u ON u.id = p.user_id
WHERE COALESCE(p.extra->>'register_ip', '') = $1
LIMIT 1
)`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, ip).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) GetUserSettings(ctx context.Context, userID int64, category string) (*userdomain.Settings, error) {
	query := `SELECT user_id, category, COALESCE(settings, '{}'::jsonb), version, is_active, created_at, updated_at FROM user_settings WHERE user_id = $1 AND category = $2 LIMIT 1`
	var item userdomain.Settings
	var raw []byte
	if err := r.pool.QueryRow(ctx, query, userID, category).Scan(&item.UserID, &item.Category, &raw, &item.Version, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.Settings)
	return &item, nil
}

func (r *Repository) CountUsersByApp(ctx context.Context, appID int64) (int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1`, appID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) CountUsersWithActiveSettingByCategory(ctx context.Context, appID int64, category string) (int64, error) {
	var total int64
	query := `SELECT COUNT(DISTINCT us.user_id)
FROM user_settings us
JOIN users u ON u.id = us.user_id
WHERE u.appid = $1
  AND us.category = $2
  AND us.is_active = true`
	if err := r.pool.QueryRow(ctx, query, appID, category).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) ListRecentUserSettingsByApp(ctx context.Context, appID int64, limit int) ([]userdomain.RecentSettingRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `SELECT us.user_id, us.category, us.created_at, us.version
FROM user_settings us
JOIN users u ON u.id = us.user_id
WHERE u.appid = $1
  AND us.is_active = true
ORDER BY us.created_at DESC, us.id DESC
LIMIT $2`
	rows, err := r.pool.Query(ctx, query, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.RecentSettingRecord, 0, limit)
	for rows.Next() {
		var item userdomain.RecentSettingRecord
		if err := rows.Scan(&item.UserID, &item.Category, &item.CreatedAt, &item.Version); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListUserSettingRecordsByApp(ctx context.Context, appID int64) ([]userdomain.SettingRecord, error) {
	query := `SELECT us.id, us.user_id, u.appid, us.category, COALESCE(us.settings, '{}'::jsonb), us.version, us.is_active, us.created_at, us.updated_at
FROM user_settings us
JOIN users u ON u.id = us.user_id
WHERE u.appid = $1
ORDER BY us.user_id ASC, us.category ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.SettingRecord, 0, 32)
	for rows.Next() {
		item, err := scanSettingRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListUserSettingRecordsByUser(ctx context.Context, appID int64, userID int64) ([]userdomain.SettingRecord, error) {
	query := `SELECT us.id, us.user_id, u.appid, us.category, COALESCE(us.settings, '{}'::jsonb), us.version, us.is_active, us.created_at, us.updated_at
FROM user_settings us
JOIN users u ON u.id = us.user_id
WHERE u.appid = $1 AND us.user_id = $2
ORDER BY us.category ASC`
	rows, err := r.pool.Query(ctx, query, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.SettingRecord, 0, 8)
	for rows.Next() {
		item, err := scanSettingRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListInvalidUserSettingsByApp(ctx context.Context, appID int64, limit int) ([]userdomain.InvalidSettingRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	query := `SELECT us.id, us.user_id, us.category, us.is_active
FROM user_settings us
LEFT JOIN users u ON u.id = us.user_id
WHERE (u.appid = $1 AND (us.is_active = false OR us.settings = '{}'::jsonb))
   OR u.id IS NULL
ORDER BY us.id ASC
LIMIT $2`
	rows, err := r.pool.Query(ctx, query, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userdomain.InvalidSettingRecord, 0, limit)
	for rows.Next() {
		var item userdomain.InvalidSettingRecord
		if err := rows.Scan(&item.ID, &item.UserID, &item.Category, &item.IsActive); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) DeleteUserSettingsByIDs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM user_settings WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) ListAutoSignCandidatesAfterUserID(ctx context.Context, lastUserID int64, limit int) ([]userdomain.AutoSignCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	query := `
SELECT
    u.id,
    u.appid,
    u.account,
    u.vip_expire_at,
    u.enabled,
    true AS settings_enabled,
    COALESCE(us.settings->>'time', '00:00') AS sign_time,
    COALESCE((us.settings->>'retryOnFail')::boolean, true) AS retry_on_fail,
    COALESCE((us.settings->>'maxRetries')::integer, 3) AS max_retries,
    COALESCE((us.settings->>'notifyOnSuccess')::boolean, true) AS notify_on_success,
    COALESCE((us.settings->>'notifyOnFail')::boolean, true) AS notify_on_fail,
    COALESCE((us.settings->>'disableLocationTracking')::boolean, true) AS disable_location_tracking
FROM users u
JOIN user_settings us ON us.user_id = u.id
WHERE u.id > $1
  AND u.enabled = true
  AND us.category = 'autoSign'
  AND us.is_active = true
  AND COALESCE(us.settings->>'enabled', 'false') = 'true'
ORDER BY u.id ASC
LIMIT $2`
	rows, err := r.pool.Query(ctx, query, lastUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.AutoSignCandidate, 0, limit)
	for rows.Next() {
		var item userdomain.AutoSignCandidate
		if err := rows.Scan(&item.UserID, &item.AppID, &item.Account, &item.VIPExpireAt, &item.Enabled, &item.SettingsEnabled, &item.Time, &item.RetryOnFail, &item.MaxRetries, &item.NotifyOnSuccess, &item.NotifyOnFail, &item.DisableLocationTracking); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAutoSignCandidate(ctx context.Context, userID int64, appID int64) (*userdomain.AutoSignCandidate, error) {
	query := `
SELECT
    u.id,
    u.appid,
    u.account,
    u.vip_expire_at,
    u.enabled,
    COALESCE((us.settings->>'enabled')::boolean, false) AS settings_enabled,
    COALESCE(us.settings->>'time', '00:00') AS sign_time,
    COALESCE((us.settings->>'retryOnFail')::boolean, true) AS retry_on_fail,
    COALESCE((us.settings->>'maxRetries')::integer, 3) AS max_retries,
    COALESCE((us.settings->>'notifyOnSuccess')::boolean, true) AS notify_on_success,
    COALESCE((us.settings->>'notifyOnFail')::boolean, true) AS notify_on_fail,
    COALESCE((us.settings->>'disableLocationTracking')::boolean, true) AS disable_location_tracking
FROM users u
LEFT JOIN user_settings us ON us.user_id = u.id AND us.category = 'autoSign' AND us.is_active = true
WHERE u.id = $1 AND u.appid = $2
LIMIT $3`
	var item userdomain.AutoSignCandidate
	err := r.pool.QueryRow(ctx, query, userID, appID, 1).Scan(&item.UserID, &item.AppID, &item.Account, &item.VIPExpireAt, &item.Enabled, &item.SettingsEnabled, &item.Time, &item.RetryOnFail, &item.MaxRetries, &item.NotifyOnSuccess, &item.NotifyOnFail, &item.DisableLocationTracking)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func (r *Repository) CreateUser(ctx context.Context, appID int64, account string, passwordHash string) (*userdomain.User, error) {
	query := `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, $2, $3, TRUE) RETURNING id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at`
	user, err := scanUser(r.pool.QueryRow(ctx, query, appID, account, nullableString(passwordHash)))
	if isUniqueViolation(err) {
		return nil, ErrAccountAlreadyExists
	}
	return user, err
}

func (r *Repository) UpdateUserPassword(ctx context.Context, userID int64, passwordHash string, changedAt time.Time) error {
	query := `UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, userID, nullableString(passwordHash))
	if err != nil {
		return err
	}

	extra := map[string]any{
		"password_changed_at": changedAt.UTC().Format(time.RFC3339),
	}
	query = `INSERT INTO user_profiles (user_id, extra, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    extra = COALESCE(user_profiles.extra, '{}'::jsonb) || EXCLUDED.extra,
    updated_at = NOW()`
	extraJSON, _ := json.Marshal(extra)
	_, err = r.pool.Exec(ctx, query, userID, extraJSON)
	return err
}

func (r *Repository) UpsertUserProfile(ctx context.Context, profile userdomain.Profile) error {
	extraJSON, _ := json.Marshal(profile.Extra)
	contactsJSON, _ := json.Marshal(profile.Contacts)
	query := `INSERT INTO user_profiles (user_id, nickname, avatar, email, phone, birthday, bio, contacts, extra, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW()) ON CONFLICT (user_id) DO UPDATE SET nickname = EXCLUDED.nickname, avatar = EXCLUDED.avatar, email = EXCLUDED.email, phone = EXCLUDED.phone, birthday = EXCLUDED.birthday, bio = EXCLUDED.bio, contacts = EXCLUDED.contacts, extra = EXCLUDED.extra, updated_at = NOW()`
	_, err := r.pool.Exec(ctx, query, profile.UserID, nullableString(profile.Nickname), nullableString(profile.Avatar), nullableString(profile.Email), profile.Phone, profile.Birthday, profile.Bio, contactsJSON, extraJSON)
	return err
}

func (r *Repository) PatchUserProfileExtra(ctx context.Context, userID int64, extra map[string]any) error {
	extraJSON, _ := json.Marshal(extra)
	query := `INSERT INTO user_profiles (user_id, extra, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    extra = COALESCE(user_profiles.extra, '{}'::jsonb) || EXCLUDED.extra,
    updated_at = NOW()`
	_, err := r.pool.Exec(ctx, query, userID, extraJSON)
	return err
}

func (r *Repository) UpsertUserSettings(ctx context.Context, setting userdomain.Settings) error {
	settingsJSON, _ := json.Marshal(setting.Settings)
	query := `INSERT INTO user_settings (user_id, category, settings, version, is_active, updated_at) VALUES ($1, $2, $3, $4, $5, COALESCE($6, NOW())) ON CONFLICT (user_id, category) DO UPDATE SET settings = EXCLUDED.settings, version = EXCLUDED.version, is_active = EXCLUDED.is_active, updated_at = EXCLUDED.updated_at`
	_, err := r.pool.Exec(ctx, query, setting.UserID, setting.Category, settingsJSON, setting.Version, setting.IsActive, nullableTime(setting.UpdatedAt))
	return err
}

func (r *Repository) FindOAuthBinding(ctx context.Context, appID int64, provider string, providerUserID string) (int64, error) {
	query := `SELECT user_id FROM oauth_bindings WHERE appid = $1 AND provider = $2 AND provider_user_id = $3 LIMIT 1`
	var userID int64
	if err := r.pool.QueryRow(ctx, query, appID, provider, providerUserID).Scan(&userID); err != nil {
		if err == pgx.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return userID, nil
}

func (r *Repository) ListOAuthProvidersByUserID(ctx context.Context, appID int64, userID int64) ([]string, error) {
	query := `SELECT DISTINCT provider FROM oauth_bindings WHERE appid = $1 AND user_id = $2 ORDER BY provider ASC`
	rows, err := r.pool.Query(ctx, query, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]string, 0, 4)
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (r *Repository) UpsertOAuthBinding(ctx context.Context, appID int64, userID int64, profile authdomain.ProviderProfile) error {
	rawJSON, _ := json.Marshal(profile.RawProfile)
	query := `INSERT INTO oauth_bindings (appid, user_id, provider, provider_user_id, union_id, access_token, refresh_token, raw_profile, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW()) ON CONFLICT (appid, provider, provider_user_id) DO UPDATE SET user_id = EXCLUDED.user_id, union_id = EXCLUDED.union_id, access_token = EXCLUDED.access_token, refresh_token = EXCLUDED.refresh_token, raw_profile = EXCLUDED.raw_profile, updated_at = NOW()`
	_, err := r.pool.Exec(ctx, query, appID, userID, profile.Provider, profile.ProviderUserID, nullableString(profile.UnionID), nullableString(profile.Tokens["access_token"]), nullableString(profile.Tokens["refresh_token"]), rawJSON)
	return err
}

func (r *Repository) UpsertImportedUser(ctx context.Context, user userdomain.User) error {
	query := `
INSERT INTO users (id, appid, account, password_hash, integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
    appid = EXCLUDED.appid,
    account = EXCLUDED.account,
    password_hash = EXCLUDED.password_hash,
    integral = EXCLUDED.integral,
    experience = EXCLUDED.experience,
    enabled = EXCLUDED.enabled,
    disabled_end_time = EXCLUDED.disabled_end_time,
    vip_expire_at = EXCLUDED.vip_expire_at,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at`
	_, err := r.pool.Exec(ctx, query, user.ID, user.AppID, user.Account, nullableString(user.PasswordHash), user.Integral, user.Experience, user.Enabled, user.DisabledEndTime, user.VIPExpireAt, user.CreatedAt, user.UpdatedAt)
	if isUniqueViolation(err) {
		return ErrAccountAlreadyExists
	}
	return err
}

func (r *Repository) ListLevelConfigs(ctx context.Context) ([]pointdomain.LevelConfig, error) {
	return r.getActiveLevels(ctx)
}

func (r *Repository) GetExperienceMultiplier(ctx context.Context, experience int64) (float64, error) {
	levels, err := r.getActiveLevels(ctx)
	if err != nil {
		return 1, err
	}
	return r.resolveLevelState(levels, experience).ExpMultiplier, nil
}

func (r *Repository) GetUserLevelProfile(ctx context.Context, userID int64, appID int64) (*pointdomain.LevelProfile, error) {
	user, err := r.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, nil
	}

	profile, err := r.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	levelInfo, err := r.syncUserLevelRecord(ctx, r.pool, userID, appID, user.Experience, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	result := &pointdomain.LevelProfile{
		UserInfo: pointdomain.LevelUserInfo{
			ID:         user.ID,
			Account:    user.Account,
			Integral:   user.Integral,
			Experience: user.Experience,
		},
		LevelInfo: *levelInfo,
	}
	if profile != nil {
		result.UserInfo.Nickname = profile.Nickname
		result.UserInfo.Avatar = profile.Avatar
	}
	return result, nil
}

func (r *Repository) getActiveLevels(ctx context.Context) ([]pointdomain.LevelConfig, error) {
	now := time.Now()
	r.levelCacheMu.RLock()
	if now.Before(r.levelCacheExpires) && len(r.levelCache) > 0 {
		levels := append([]pointdomain.LevelConfig(nil), r.levelCache...)
		r.levelCacheMu.RUnlock()
		return levels, nil
	}
	r.levelCacheMu.RUnlock()

	levels, err := r.loadActiveLevels(ctx, r.pool)
	if err != nil {
		return nil, err
	}

	r.levelCacheMu.Lock()
	r.levelCache = append([]pointdomain.LevelConfig(nil), levels...)
	r.levelCacheExpires = now.Add(levelCacheTTL)
	r.levelCacheMu.Unlock()

	return append([]pointdomain.LevelConfig(nil), levels...), nil
}

func (r *Repository) loadActiveLevels(ctx context.Context, q queryExecutor) ([]pointdomain.LevelConfig, error) {
	rows, err := q.Query(ctx, `SELECT id, level, level_name, experience_required, experience_next, exp_multiplier, COALESCE(icon, ''), COALESCE(color, ''), COALESCE(privileges, '[]'::jsonb), COALESCE(rewards, '{}'::jsonb), COALESCE(description, ''), is_active, sort_order, created_at, updated_at FROM user_levels WHERE is_active = true ORDER BY level ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	levels := make([]pointdomain.LevelConfig, 0, 16)
	for rows.Next() {
		var item pointdomain.LevelConfig
		var experienceNext *int64
		var privilegesJSON []byte
		var rewardsJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.Level,
			&item.LevelName,
			&item.ExperienceRequired,
			&experienceNext,
			&item.ExpMultiplier,
			&item.Icon,
			&item.Color,
			&privilegesJSON,
			&rewardsJSON,
			&item.Description,
			&item.IsActive,
			&item.SortOrder,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.ExperienceNext = experienceNext
		_ = json.Unmarshal(privilegesJSON, &item.Privileges)
		_ = json.Unmarshal(rewardsJSON, &item.Rewards)
		levels = append(levels, item)
	}
	return levels, rows.Err()
}

func (r *Repository) syncUserLevelRecord(ctx context.Context, q queryExecutor, userID int64, appID int64, totalExperience int64, updatedAt time.Time) (*pointdomain.LevelInfo, error) {
	levels, err := r.getActiveLevels(ctx)
	if err != nil {
		return nil, err
	}
	state := r.resolveLevelState(levels, totalExperience)

	query := `SELECT user_id, appid, current_level, current_experience, total_experience, next_level_experience, level_progress, highest_level, level_up_count, last_level_up_at FROM user_level_records WHERE user_id = $1 AND appid = $2`
	if _, isTx := q.(pgx.Tx); isTx {
		query += ` FOR UPDATE`
	}
	record, err := scanLevelRecord(q.QueryRow(ctx, query, userID, appID))
	if err != nil {
		return nil, err
	}

	levelDelta := 0
	highestLevel := state.CurrentLevel
	levelUpCount := 0
	lastLevelUpAt := (*time.Time)(nil)

	if record != nil {
		levelDelta = state.CurrentLevel - record.CurrentLevel
		if levelDelta < 0 {
			levelDelta = 0
		}
		highestLevel = maxInt(record.HighestLevel, state.CurrentLevel)
		levelUpCount = record.LevelUpCount + levelDelta
		lastLevelUpAt = record.LastLevelUpAt
		if levelDelta > 0 {
			ts := updatedAt.UTC()
			lastLevelUpAt = &ts
		}
	} else {
		highestLevel = state.CurrentLevel
	}

	var nextLevelExperience any
	if state.IsMaxLevel {
		nextLevelExperience = nil
	} else {
		nextLevelExperience = state.ExpToNextLevel
	}
	nextLevel := 0
	if state.NextConfig != nil {
		nextLevel = state.NextConfig.Level
	}

	upsertQuery := `INSERT INTO user_level_records (user_id, appid, current_level, current_experience, total_experience, next_level_experience, level_progress, highest_level, level_up_count, last_level_up_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11, NOW()), $11)
ON CONFLICT (user_id, appid) DO UPDATE SET
    current_level = EXCLUDED.current_level,
    current_experience = EXCLUDED.current_experience,
    total_experience = EXCLUDED.total_experience,
    next_level_experience = EXCLUDED.next_level_experience,
    level_progress = EXCLUDED.level_progress,
    highest_level = EXCLUDED.highest_level,
    level_up_count = EXCLUDED.level_up_count,
    last_level_up_at = EXCLUDED.last_level_up_at,
    updated_at = EXCLUDED.updated_at`
	if _, err := q.Exec(ctx, upsertQuery, userID, appID, state.CurrentLevel, state.ExpInCurrentLevel, totalExperience, nextLevelExperience, state.LevelProgress, highestLevel, levelUpCount, lastLevelUpAt, updatedAt.UTC()); err != nil {
		return nil, err
	}

	return &pointdomain.LevelInfo{
		CurrentLevel:       state.CurrentLevel,
		CurrentLevelName:   state.CurrentLevelName,
		ExpMultiplier:      state.ExpMultiplier,
		TotalExperience:    totalExperience,
		ExpInCurrentLevel:  state.ExpInCurrentLevel,
		ExpRangeForLevel:   state.ExpRangeForLevel,
		ExpToNextLevel:     state.ExpToNextLevel,
		LevelProgress:      state.LevelProgress,
		CurrentLevelMinExp: state.CurrentLevelMinExp,
		HighestLevel:       highestLevel,
		LevelUpCount:       levelUpCount,
		LastLevelUpAt:      lastLevelUpAt,
		CurrentLevelConfig: state.Config,
		NextLevelConfig:    state.NextConfig,
		IsMaxLevel:         state.IsMaxLevel,
		NextLevel:          nextLevel,
		NextLevelMinExp:    state.NextLevelMinExp,
		NextLevelName:      state.NextLevelName,
	}, nil
}

func (r *Repository) resolveLevelState(levels []pointdomain.LevelConfig, experience int64) levelState {
	if experience < 0 {
		experience = 0
	}
	if len(levels) == 0 {
		return levelState{
			CurrentLevel:       1,
			CurrentLevelName:   "新手",
			ExpMultiplier:      1,
			TotalExperience:    experience,
			ExpInCurrentLevel:  experience,
			CurrentLevelMinExp: 0,
			LevelProgress:      0,
			IsMaxLevel:         true,
		}
	}

	current := levels[0]
	currentIndex := 0
	for index, level := range levels {
		if experience >= level.ExperienceRequired {
			current = level
			currentIndex = index
			continue
		}
		break
	}

	state := levelState{
		Config:             &current,
		CurrentLevel:       current.Level,
		CurrentLevelName:   current.LevelName,
		ExpMultiplier:      current.ExpMultiplier,
		TotalExperience:    experience,
		ExpInCurrentLevel:  maxInt64(experience-current.ExperienceRequired, 0),
		CurrentLevelMinExp: current.ExperienceRequired,
		IsMaxLevel:         currentIndex == len(levels)-1,
	}

	if !state.IsMaxLevel {
		next := levels[currentIndex+1]
		state.NextConfig = &next
		state.NextLevelMinExp = &next.ExperienceRequired
		state.NextLevelName = next.LevelName
		state.ExpRangeForLevel = next.ExperienceRequired - current.ExperienceRequired
		state.ExpToNextLevel = maxInt64(next.ExperienceRequired-experience, 0)
		if state.ExpRangeForLevel > 0 {
			state.LevelProgress = math.Round((float64(state.ExpInCurrentLevel)/float64(state.ExpRangeForLevel))*10000) / 100
		}
	} else {
		state.LevelProgress = 100
	}
	if state.LevelProgress < 0 {
		state.LevelProgress = 0
	}
	if state.LevelProgress > 100 {
		state.LevelProgress = 100
	}

	return state
}

func (r *Repository) GetLatestDailySign(ctx context.Context, userID int64, appID int64) (*userdomain.DailySignIn, error) {
	query := `SELECT id, user_id, appid, signed_at, sign_date, integral_reward, experience_reward, integral_before, integral_after, experience_before, experience_after, consecutive_days, reward_multiplier, COALESCE(bonus_type, ''), COALESCE(bonus_description, ''), sign_in_source, COALESCE(device_info, ''), COALESCE(ip_address, ''), COALESCE(location, ''), created_at FROM daily_signins WHERE user_id = $1 AND appid = $2 ORDER BY sign_date DESC, id DESC LIMIT 1`
	return scanDailySign(r.pool.QueryRow(ctx, query, userID, appID))
}

func (r *Repository) GetDailySignByDate(ctx context.Context, userID int64, appID int64, signDate string) (*userdomain.DailySignIn, error) {
	query := `SELECT id, user_id, appid, signed_at, sign_date, integral_reward, experience_reward, integral_before, integral_after, experience_before, experience_after, consecutive_days, reward_multiplier, COALESCE(bonus_type, ''), COALESCE(bonus_description, ''), sign_in_source, COALESCE(device_info, ''), COALESCE(ip_address, ''), COALESCE(location, ''), created_at FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date = $3 LIMIT 1`
	return scanDailySign(r.pool.QueryRow(ctx, query, userID, appID, signDate))
}

func (r *Repository) ListDailySigns(ctx context.Context, userID int64, appID int64, page int, limit int) ([]userdomain.DailySignIn, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM daily_signins WHERE user_id = $1 AND appid = $2`, userID, appID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `SELECT id, user_id, appid, signed_at, sign_date, integral_reward, experience_reward, integral_before, integral_after, experience_before, experience_after, consecutive_days, reward_multiplier, COALESCE(bonus_type, ''), COALESCE(bonus_description, ''), sign_in_source, COALESCE(device_info, ''), COALESCE(ip_address, ''), COALESCE(location, ''), created_at
FROM daily_signins
WHERE user_id = $1 AND appid = $2
ORDER BY sign_date DESC, id DESC
LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, query, userID, appID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.DailySignIn, 0, limit)
	for rows.Next() {
		item, err := scanDailySign(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListDailySignsForExport(ctx context.Context, userID int64, appID int64, limit int) ([]userdomain.DailySignIn, error) {
	if limit < 1 {
		limit = 5000
	}

	query := `SELECT id, user_id, appid, signed_at, sign_date, integral_reward, experience_reward, integral_before, integral_after, experience_before, experience_after, consecutive_days, reward_multiplier, COALESCE(bonus_type, ''), COALESCE(bonus_description, ''), sign_in_source, COALESCE(device_info, ''), COALESCE(ip_address, ''), COALESCE(location, ''), created_at
FROM daily_signins
WHERE user_id = $1 AND appid = $2
ORDER BY sign_date DESC, id DESC
LIMIT $3`
	rows, err := r.pool.Query(ctx, query, userID, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]userdomain.DailySignIn, 0, limit)
	for rows.Next() {
		item, err := scanDailySign(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetSignStats(ctx context.Context, userID int64, appID int64) (*userdomain.SignStats, error) {
	query := `SELECT user_id, appid, last_sign_date, last_sign_at, consecutive_days, total_sign_days, total_integral_reward, total_experience_reward, updated_at FROM sign_stats WHERE user_id = $1 AND appid = $2 LIMIT 1`
	return scanSignStats(r.pool.QueryRow(ctx, query, userID, appID))
}

func (r *Repository) CountDailySigns(ctx context.Context, userID int64, appID int64) (int64, error) {
	stats, err := r.GetSignStats(ctx, userID, appID)
	if err != nil {
		return 0, err
	}
	if stats == nil {
		return 0, nil
	}
	return stats.TotalSignDays, nil
}

func (r *Repository) SumDailyRewards(ctx context.Context, userID int64, appID int64) (int64, int64, error) {
	stats, err := r.GetSignStats(ctx, userID, appID)
	if err != nil {
		return 0, 0, err
	}
	if stats == nil {
		return 0, 0, nil
	}
	return stats.TotalIntegralReward, stats.TotalExperienceReward, nil
}

func (r *Repository) CreateDailySign(ctx context.Context, userID int64, appID int64, reward userdomain.SignInReward, signDate time.Time, source, deviceInfo, ipAddress, location string) (*userdomain.SignInResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	user, err := scanUser(tx.QueryRow(ctx, `SELECT id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}

	var existingID int64
	err = tx.QueryRow(ctx, `SELECT id FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date = $3 LIMIT 1`, userID, appID, signDate.Format("2006-01-02")).Scan(&existingID)
	if err == nil {
		return nil, ErrAlreadySigned
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	consecutiveDays := 1
	stats, err := scanSignStats(tx.QueryRow(ctx, `SELECT user_id, appid, last_sign_date, last_sign_at, consecutive_days, total_sign_days, total_integral_reward, total_experience_reward, updated_at FROM sign_stats WHERE user_id = $1 AND appid = $2 FOR UPDATE`, userID, appID))
	if err != nil {
		return nil, err
	}
	if stats != nil && stats.LastSignDate != "" {
		latestDate, parseErr := time.Parse("2006-01-02", stats.LastSignDate)
		if parseErr == nil {
			diff := int(signDate.Sub(latestDate).Hours() / 24)
			switch diff {
			case 0:
				return nil, ErrAlreadySigned
			case 1:
				consecutiveDays = stats.ConsecutiveDays + 1
			default:
				consecutiveDays = 1
			}
		}
	}

	integralBefore := user.Integral
	experienceBefore := user.Experience
	integralAfter := integralBefore + reward.IntegralReward
	experienceAfter := experienceBefore + reward.ExperienceReward

	if _, err = tx.Exec(ctx, `UPDATE users SET integral = $2, experience = $3, updated_at = NOW() WHERE id = $1`, userID, integralAfter, experienceAfter); err != nil {
		return nil, err
	}

	record := userdomain.DailySignIn{
		UserID:           userID,
		AppID:            appID,
		SignedAt:         signDate.UTC(),
		SignDate:         signDate.Format("2006-01-02"),
		IntegralReward:   reward.IntegralReward,
		ExperienceReward: reward.ExperienceReward,
		IntegralBefore:   integralBefore,
		IntegralAfter:    integralAfter,
		ExperienceBefore: experienceBefore,
		ExperienceAfter:  experienceAfter,
		ConsecutiveDays:  consecutiveDays,
		RewardMultiplier: reward.RewardMultiplier,
		BonusType:        reward.BonusType,
		BonusDescription: reward.BonusDescription,
		SignInSource:     source,
		DeviceInfo:       deviceInfo,
		IPAddress:        ipAddress,
		Location:         location,
	}

	insertQuery := `INSERT INTO daily_signins (user_id, appid, signed_at, sign_date, integral_reward, experience_reward, integral_before, integral_after, experience_before, experience_after, consecutive_days, reward_multiplier, bonus_type, bonus_description, sign_in_source, device_info, ip_address, location, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, NOW(), NOW()) RETURNING id, created_at`
	if err = tx.QueryRow(ctx, insertQuery, record.UserID, record.AppID, record.SignedAt, record.SignDate, record.IntegralReward, record.ExperienceReward, record.IntegralBefore, record.IntegralAfter, record.ExperienceBefore, record.ExperienceAfter, record.ConsecutiveDays, record.RewardMultiplier, nullableString(record.BonusType), nullableString(record.BonusDescription), record.SignInSource, nullableString(record.DeviceInfo), nullableString(record.IPAddress), nullableString(record.Location)).Scan(&record.ID, &record.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadySigned
		}
		return nil, err
	}

	var totalSignIns int64 = 1
	var totalIntegralReward int64 = record.IntegralReward
	var totalExperienceReward int64 = record.ExperienceReward
	if stats != nil {
		totalSignIns = stats.TotalSignDays + 1
		totalIntegralReward = stats.TotalIntegralReward + record.IntegralReward
		totalExperienceReward = stats.TotalExperienceReward + record.ExperienceReward
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sign_stats (user_id, appid, last_sign_date, last_sign_at, consecutive_days, total_sign_days, total_integral_reward, total_experience_reward, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (user_id, appid) DO UPDATE SET
    last_sign_date = EXCLUDED.last_sign_date,
    last_sign_at = EXCLUDED.last_sign_at,
    consecutive_days = EXCLUDED.consecutive_days,
    total_sign_days = EXCLUDED.total_sign_days,
    total_integral_reward = EXCLUDED.total_integral_reward,
    total_experience_reward = EXCLUDED.total_experience_reward,
    updated_at = NOW()`,
		record.UserID,
		record.AppID,
		record.SignDate,
		record.SignedAt,
		record.ConsecutiveDays,
		totalSignIns,
		totalIntegralReward,
		totalExperienceReward,
	); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sign_daily_rollups (appid, rollup_date, sign_user_count, total_integral_reward, total_experience_reward, updated_at)
VALUES ($1, $2, 1, $3, $4, NOW())
ON CONFLICT (appid, rollup_date) DO UPDATE SET
    sign_user_count = sign_daily_rollups.sign_user_count + 1,
    total_integral_reward = sign_daily_rollups.total_integral_reward + EXCLUDED.total_integral_reward,
    total_experience_reward = sign_daily_rollups.total_experience_reward + EXCLUDED.total_experience_reward,
    updated_at = NOW()`,
		record.AppID,
		record.SignDate,
		record.IntegralReward,
		record.ExperienceReward,
	); err != nil {
		return nil, err
	}
	monthKey := time.Date(signDate.Year(), signDate.Month(), 1, 0, 0, 0, 0, signDate.Location()).Format("2006-01-02")
	if _, err = tx.Exec(ctx, `INSERT INTO sign_monthly_stats (month_key, appid, user_id, sign_days, total_integral_reward, total_experience_reward, last_sign_at, updated_at)
VALUES ($1, $2, $3, 1, $4, $5, $6, NOW())
ON CONFLICT (month_key, appid, user_id) DO UPDATE SET
    sign_days = sign_monthly_stats.sign_days + 1,
    total_integral_reward = sign_monthly_stats.total_integral_reward + EXCLUDED.total_integral_reward,
    total_experience_reward = sign_monthly_stats.total_experience_reward + EXCLUDED.total_experience_reward,
    last_sign_at = EXCLUDED.last_sign_at,
    updated_at = NOW()`,
		monthKey,
		record.AppID,
		record.UserID,
		record.IntegralReward,
		record.ExperienceReward,
		record.SignedAt,
	); err != nil {
		return nil, err
	}

	integralExtra, _ := json.Marshal(map[string]any{
		"sign_date":         record.SignDate,
		"consecutive_days":  record.ConsecutiveDays,
		"bonus_type":        record.BonusType,
		"bonus_description": record.BonusDescription,
		"source":            record.SignInSource,
	})
	if _, err = tx.Exec(ctx, `INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, status, title, description, source_id, source_type, multiplier, client_ip, user_agent, extra_data, created_at, updated_at) VALUES ($1, $2, $3, 'earn', 'daily_signin', $4, $5, $6, 'completed', $7, $8, $9, 'daily_signin', $10, $11, $12, $13, NOW(), NOW())`,
		generateTransactionNo("INT"),
		record.UserID,
		record.AppID,
		record.IntegralReward,
		record.IntegralBefore,
		record.IntegralAfter,
		"每日签到奖励",
		record.BonusDescription,
		record.ID,
		record.RewardMultiplier,
		nullableString(record.IPAddress),
		nullableString(record.DeviceInfo),
		integralExtra,
	); err != nil {
		return nil, err
	}

	levels, err := r.getActiveLevels(ctx)
	if err != nil {
		return nil, err
	}
	levelBefore := r.resolveLevelState(levels, record.ExperienceBefore)
	levelAfterInfo, err := r.syncUserLevelRecord(ctx, tx, userID, appID, record.ExperienceAfter, signDate.UTC())
	if err != nil {
		return nil, err
	}
	isLevelUp := levelAfterInfo.CurrentLevel > levelBefore.CurrentLevel
	levelRewards := collectLevelUpRewards(levels, levelBefore.CurrentLevel, levelAfterInfo.CurrentLevel)
	experienceExtra, _ := json.Marshal(map[string]any{
		"sign_date":         record.SignDate,
		"consecutive_days":  record.ConsecutiveDays,
		"bonus_type":        record.BonusType,
		"bonus_description": record.BonusDescription,
		"source":            record.SignInSource,
		"level_before":      levelBefore.CurrentLevel,
		"level_after":       levelAfterInfo.CurrentLevel,
		"level_rewards":     levelRewards,
	})
	experienceTitle := "每日签到经验奖励"
	if isLevelUp {
		experienceTitle = "每日签到经验奖励（等级提升）"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO experience_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, level_before, level_after, status, title, description, source_id, source_type, multiplier, is_level_up, client_ip, user_agent, extra_data, created_at, updated_at) VALUES ($1, $2, $3, 'earn', 'daily_signin', $4, $5, $6, $7, $8, 'completed', $9, $10, $11, 'daily_signin', $12, $13, $14, $15, $16, NOW(), NOW())`,
		generateTransactionNo("EXP"),
		record.UserID,
		record.AppID,
		record.ExperienceReward,
		record.ExperienceBefore,
		record.ExperienceAfter,
		levelBefore.CurrentLevel,
		levelAfterInfo.CurrentLevel,
		experienceTitle,
		record.BonusDescription,
		record.ID,
		levelBefore.ExpMultiplier,
		isLevelUp,
		nullableString(record.IPAddress),
		nullableString(record.DeviceInfo),
		experienceExtra,
	); err != nil {
		return nil, err
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	return &userdomain.SignInResult{
		Record:       record,
		Reward:       reward,
		TotalSignIns: totalSignIns,
	}, nil
}

func (r *Repository) ListIntegralTransactions(ctx context.Context, userID int64, appID int64, page int, limit int) ([]pointdomain.Transaction, int64, error) {
	return r.listTransactions(ctx, `SELECT id, transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, 0 AS level_before, 0 AS level_after, status, title, COALESCE(description, ''), source_id, COALESCE(source_type, ''), multiplier, false AS is_level_up, COALESCE(client_ip, ''), COALESCE(user_agent, ''), COALESCE(extra_data, '{}'::jsonb), created_at FROM integral_transactions WHERE user_id = $1 AND appid = $2 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, "integral_transactions", userID, appID, page, limit)
}

func (r *Repository) ListExperienceTransactions(ctx context.Context, userID int64, appID int64, page int, limit int) ([]pointdomain.Transaction, int64, error) {
	return r.listTransactions(ctx, `SELECT id, transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, level_before, level_after, status, title, COALESCE(description, ''), source_id, COALESCE(source_type, ''), multiplier, is_level_up, COALESCE(client_ip, ''), COALESCE(user_agent, ''), COALESCE(extra_data, '{}'::jsonb), created_at FROM experience_transactions WHERE user_id = $1 AND appid = $2 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, "experience_transactions", userID, appID, page, limit)
}

func (r *Repository) listTransactions(ctx context.Context, query string, table string, userID int64, appID int64, page int, limit int) ([]pointdomain.Transaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	countQuery := `SELECT COUNT(*) FROM ` + table + ` WHERE user_id = $1 AND appid = $2`
	if err := r.pool.QueryRow(ctx, countQuery, userID, appID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx, query, userID, appID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]pointdomain.Transaction, 0, limit)
	for rows.Next() {
		item, err := scanTransaction(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) GetPointsOverview(ctx context.Context, userID int64, appID int64) (*pointdomain.Overview, error) {
	user, err := r.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, nil
	}

	stats, err := r.GetSignStats(ctx, userID, appID)
	if err != nil {
		return nil, err
	}
	recentIntegral, _, err := r.ListIntegralTransactions(ctx, userID, appID, 1, 5)
	if err != nil {
		return nil, err
	}
	recentExperience, _, err := r.ListExperienceTransactions(ctx, userID, appID, 1, 5)
	if err != nil {
		return nil, err
	}

	levelProfile, err := r.GetUserLevelProfile(ctx, userID, appID)
	if err != nil {
		return nil, err
	}

	var levelInfo *pointdomain.LevelInfo
	if levelProfile != nil {
		levelInfo = &levelProfile.LevelInfo
	}

	totalSignIns := int64(0)
	totalIntegral := int64(0)
	totalExperience := int64(0)
	if stats != nil {
		totalSignIns = stats.TotalSignDays
		totalIntegral = stats.TotalIntegralReward
		totalExperience = stats.TotalExperienceReward
	}

	return &pointdomain.Overview{
		Integral:                user.Integral,
		Experience:              user.Experience,
		TotalSignIns:            totalSignIns,
		TotalIntegralEarned:     totalIntegral,
		TotalExperienceEarned:   totalExperience,
		LevelInfo:               levelInfo,
		RecentIntegralRecords:   recentIntegral,
		RecentExperienceRecords: recentExperience,
	}, nil
}

func (r *Repository) GetAppPointsStatistics(ctx context.Context, appID int64, timeRange int, startDate time.Time, timezone string) (*pointdomain.AppStatistics, error) {
	if timeRange <= 0 {
		timeRange = 30
	}
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}

	result := &pointdomain.AppStatistics{
		AppID: appID,
		Overview: pointdomain.AppStatsOverview{
			TimeRange: timeRange,
		},
		Integral: pointdomain.AppIntegralStatistics{
			Stats:    make([]pointdomain.AppTransactionCategoryStat, 0, 8),
			TopUsers: make([]pointdomain.AppTopUser, 0, 10),
		},
		Experience: pointdomain.AppExperienceStatistics{
			Stats:    make([]pointdomain.AppTransactionCategoryStat, 0, 8),
			TopUsers: make([]pointdomain.AppTopUser, 0, 10),
		},
		DailyStats: make([]pointdomain.AppDailyTransactionStat, 0, timeRange),
	}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1`, appID).Scan(&result.Overview.TotalUsers); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*) FROM (
	SELECT DISTINCT user_id
	FROM integral_transactions
	WHERE appid = $1 AND created_at >= $2
	UNION
	SELECT DISTINCT user_id
	FROM experience_transactions
	WHERE appid = $1 AND created_at >= $2
) active_users`, appID, startDate.UTC()).Scan(&result.Overview.ActiveUsers); err != nil {
		return nil, err
	}
	if result.Overview.TotalUsers > 0 {
		result.Overview.ActiveRate = fmt.Sprintf("%.2f%%", float64(result.Overview.ActiveUsers)/float64(result.Overview.TotalUsers)*100)
	} else {
		result.Overview.ActiveRate = "0.00%"
	}

	integralRows, err := r.pool.Query(ctx, `
SELECT type, category, COUNT(*) AS count, COALESCE(SUM(amount), 0) AS total_amount
FROM integral_transactions
WHERE appid = $1 AND status = 'completed' AND created_at >= $2
GROUP BY type, category
ORDER BY type ASC, category ASC`, appID, startDate.UTC())
	if err != nil {
		return nil, err
	}
	defer integralRows.Close()

	for integralRows.Next() {
		var item pointdomain.AppTransactionCategoryStat
		if err := integralRows.Scan(&item.Type, &item.Category, &item.Count, &item.TotalAmount); err != nil {
			return nil, err
		}
		result.Integral.Stats = append(result.Integral.Stats, item)
	}
	if err := integralRows.Err(); err != nil {
		return nil, err
	}

	experienceRows, err := r.pool.Query(ctx, `
SELECT type, category, COUNT(*) AS count, COALESCE(SUM(amount), 0) AS total_amount, COALESCE(SUM(CASE WHEN is_level_up THEN 1 ELSE 0 END), 0) AS level_ups
FROM experience_transactions
WHERE appid = $1 AND status = 'completed' AND created_at >= $2
GROUP BY type, category
ORDER BY type ASC, category ASC`, appID, startDate.UTC())
	if err != nil {
		return nil, err
	}
	defer experienceRows.Close()

	for experienceRows.Next() {
		var item pointdomain.AppTransactionCategoryStat
		if err := experienceRows.Scan(&item.Type, &item.Category, &item.Count, &item.TotalAmount, &item.LevelUps); err != nil {
			return nil, err
		}
		result.Experience.Stats = append(result.Experience.Stats, item)
	}
	if err := experienceRows.Err(); err != nil {
		return nil, err
	}

	topIntegralRows, err := r.pool.Query(ctx, `
SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), u.integral
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
ORDER BY u.integral DESC, u.id ASC
LIMIT 10`, appID)
	if err != nil {
		return nil, err
	}
	defer topIntegralRows.Close()

	for topIntegralRows.Next() {
		var item pointdomain.AppTopUser
		if err := topIntegralRows.Scan(&item.ID, &item.Account, &item.Nickname, &item.Avatar, &item.Integral); err != nil {
			return nil, err
		}
		result.Integral.TopUsers = append(result.Integral.TopUsers, item)
	}
	if err := topIntegralRows.Err(); err != nil {
		return nil, err
	}

	topExperienceRows, err := r.pool.Query(ctx, `
SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), u.experience
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1
ORDER BY u.experience DESC, u.id ASC
LIMIT 10`, appID)
	if err != nil {
		return nil, err
	}
	defer topExperienceRows.Close()

	for topExperienceRows.Next() {
		var item pointdomain.AppTopUser
		if err := topExperienceRows.Scan(&item.ID, &item.Account, &item.Nickname, &item.Avatar, &item.Experience); err != nil {
			return nil, err
		}
		result.Experience.TopUsers = append(result.Experience.TopUsers, item)
	}
	if err := topExperienceRows.Err(); err != nil {
		return nil, err
	}

	dailyByDate := make(map[string]*pointdomain.AppDailyTransactionStat, timeRange)
	integralDailyRows, err := r.pool.Query(ctx, `
SELECT TIMEZONE($3, created_at)::date AS day,
	COUNT(*) AS transaction_count,
	COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END), 0) AS earned,
	COALESCE(SUM(CASE WHEN amount < 0 THEN ABS(amount) ELSE 0 END), 0) AS consumed,
	COUNT(DISTINCT user_id) AS active_users
FROM integral_transactions
WHERE appid = $1 AND status = 'completed' AND created_at >= $2
GROUP BY day
ORDER BY day ASC`, appID, startDate.UTC(), timezone)
	if err != nil {
		return nil, err
	}
	defer integralDailyRows.Close()

	for integralDailyRows.Next() {
		var day time.Time
		var item pointdomain.AppDailyTransactionStat
		if err := integralDailyRows.Scan(&day, &item.IntegralTransactionCount, &item.IntegralEarned, &item.IntegralConsumed, &item.IntegralActiveUsers); err != nil {
			return nil, err
		}
		item.Date = day.Format("2006-01-02")
		dailyByDate[item.Date] = &item
	}
	if err := integralDailyRows.Err(); err != nil {
		return nil, err
	}

	experienceDailyRows, err := r.pool.Query(ctx, `
SELECT TIMEZONE($3, created_at)::date AS day,
	COUNT(*) AS transaction_count,
	COALESCE(SUM(amount), 0) AS experience_gained,
	COALESCE(SUM(CASE WHEN is_level_up THEN 1 ELSE 0 END), 0) AS level_ups,
	COUNT(DISTINCT user_id) AS active_users
FROM experience_transactions
WHERE appid = $1 AND status = 'completed' AND created_at >= $2
GROUP BY day
ORDER BY day ASC`, appID, startDate.UTC(), timezone)
	if err != nil {
		return nil, err
	}
	defer experienceDailyRows.Close()

	for experienceDailyRows.Next() {
		var day time.Time
		var count int64
		var gained int64
		var levelUps int64
		var activeUsers int64
		if err := experienceDailyRows.Scan(&day, &count, &gained, &levelUps, &activeUsers); err != nil {
			return nil, err
		}
		dateKey := day.Format("2006-01-02")
		item, ok := dailyByDate[dateKey]
		if !ok {
			item = &pointdomain.AppDailyTransactionStat{Date: dateKey}
			dailyByDate[dateKey] = item
		}
		item.ExperienceTransactionCount = count
		item.ExperienceGained = gained
		item.ExperienceLevelUps = levelUps
		item.ExperienceActiveUsers = activeUsers
	}
	if err := experienceDailyRows.Err(); err != nil {
		return nil, err
	}

	dates := make([]string, 0, len(dailyByDate))
	for date := range dailyByDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		result.DailyStats = append(result.DailyStats, *dailyByDate[date])
	}

	return result, nil
}

func (r *Repository) AdjustUserIntegralByAdmin(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var userIDDB int64
	var account string
	var balanceBefore int64
	if err := tx.QueryRow(ctx, `SELECT id, account, integral FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&userIDDB, &account, &balanceBefore); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	balanceAfter := balanceBefore + amount
	if balanceAfter < 0 {
		return nil, ErrInsufficientIntegral
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE users SET integral = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`, balanceAfter, userID, appID); err != nil {
		return nil, err
	}

	transactionNo := generateTransactionNo("INT")
	transactionType := "earn"
	title := "管理员手动调整"
	if amount < 0 {
		transactionType = "consume"
		title = "管理员手动扣除"
	}
	if strings.TrimSpace(reason) == "" {
		reason = title
	}

	extraData, _ := json.Marshal(map[string]any{
		"adminId":      options.AdminID,
		"adminAccount": options.AdminAccount,
		"reason":       reason,
	})

	if _, err := tx.Exec(ctx, `INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, status, title, description, source_id, source_type, multiplier, client_ip, user_agent, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'admin_adjust', $5, $6, $7, 'completed', $8, $9, $10, 'admin_manual', 1, $11, $12, $13, $14, $14)`,
		transactionNo,
		userID,
		appID,
		transactionType,
		amount,
		balanceBefore,
		balanceAfter,
		title,
		reason,
		nullableInt64(options.AdminID),
		nullableString(options.ClientIP),
		nullableString(options.UserAgent),
		extraData,
		now,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	operationType := "add"
	if amount < 0 {
		operationType = "consume"
	}

	return &pointdomain.IntegralAdjustResult{
		UserID:        userIDDB,
		AppID:         appID,
		Account:       account,
		Amount:        amount,
		BeforeAmount:  balanceBefore,
		AfterAmount:   balanceAfter,
		Reason:        reason,
		OperationType: operationType,
		TransactionNo: transactionNo,
		CreatedAt:     now,
	}, nil
}

func (r *Repository) AdjustUserExperienceByAdmin(ctx context.Context, userID int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var userIDDB int64
	var account string
	var experienceBefore int64
	if err := tx.QueryRow(ctx, `SELECT id, account, experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&userIDDB, &account, &experienceBefore); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	levels, err := r.getActiveLevels(ctx)
	if err != nil {
		return nil, err
	}

	beforeState := r.resolveLevelState(levels, experienceBefore)
	experienceAfter := experienceBefore + amount
	afterState := r.resolveLevelState(levels, experienceAfter)
	now := time.Now().UTC()

	if _, err := tx.Exec(ctx, `UPDATE users SET experience = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`, experienceAfter, userID, appID); err != nil {
		return nil, err
	}
	if _, err := r.syncUserLevelRecord(ctx, tx, userID, appID, experienceAfter, now); err != nil {
		return nil, err
	}

	transactionNo := generateTransactionNo("EXP")
	title := "管理员手动调整"
	if strings.TrimSpace(reason) == "" {
		reason = "管理员手动调整经验值"
	}
	isLevelUp := afterState.CurrentLevel > beforeState.CurrentLevel

	extraData, _ := json.Marshal(map[string]any{
		"adminId":      options.AdminID,
		"adminAccount": options.AdminAccount,
		"reason":       reason,
		"oldLevel":     beforeState.CurrentLevel,
		"newLevel":     afterState.CurrentLevel,
	})

	if _, err := tx.Exec(ctx, `INSERT INTO experience_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, level_before, level_after, status, title, description, source_id, source_type, multiplier, is_level_up, client_ip, user_agent, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, 'earn', 'admin_adjust', $4, $5, $6, $7, $8, 'completed', $9, $10, $11, 'admin_manual', 1, $12, $13, $14, $15, $16, $16)`,
		transactionNo,
		userID,
		appID,
		amount,
		experienceBefore,
		experienceAfter,
		beforeState.CurrentLevel,
		afterState.CurrentLevel,
		title,
		reason,
		nullableInt64(options.AdminID),
		isLevelUp,
		nullableString(options.ClientIP),
		nullableString(options.UserAgent),
		extraData,
		now,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	return &pointdomain.ExperienceAdjustResult{
		UserID:        userIDDB,
		AppID:         appID,
		Account:       account,
		Amount:        amount,
		BeforeAmount:  experienceBefore,
		AfterAmount:   experienceAfter,
		Reason:        reason,
		OperationType: "add",
		TransactionNo: transactionNo,
		LevelChanged:  isLevelUp,
		OldLevel:      beforeState.CurrentLevel,
		NewLevel:      afterState.CurrentLevel,
		CreatedAt:     now,
	}, nil
}

func (r *Repository) GetIntegralRankings(ctx context.Context, appID int64, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	return r.getUserMetricRankings(ctx, appID, page, limit, currentUserID, "integral")
}

func (r *Repository) GetExperienceRankings(ctx context.Context, appID int64, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	return r.getUserMetricRankings(ctx, appID, page, limit, currentUserID, "experience")
}

func (r *Repository) GetLevelRankings(ctx context.Context, appID int64, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1 AND enabled = true`, appID).Scan(&total); err != nil {
		return nil, err
	}

	query := `
WITH ranked AS (
    SELECT
        u.id,
        u.account,
        u.experience,
        COALESCE(p.nickname, '') AS nickname,
        COALESCE(p.avatar, '') AS avatar,
        COALESCE(curr.level, 1) AS current_level,
        COALESCE(curr.level_name, '新手') AS level_name,
        CASE
            WHEN next_level.experience_required IS NULL THEN 100.00
            WHEN next_level.experience_required = COALESCE(curr.experience_required, 0) THEN 0.00
            ELSE ROUND(
                ((u.experience - COALESCE(curr.experience_required, 0))::numeric / NULLIF((next_level.experience_required - COALESCE(curr.experience_required, 0)), 0)::numeric) * 100.00,
                2
            )
        END AS level_progress
    FROM users u
    LEFT JOIN user_profiles p ON p.user_id = u.id
    LEFT JOIN LATERAL (
        SELECT level, level_name, experience_required
        FROM user_levels
        WHERE is_active = true AND experience_required <= u.experience
        ORDER BY level DESC
        LIMIT 1
    ) curr ON true
    LEFT JOIN LATERAL (
        SELECT experience_required
        FROM user_levels
        WHERE is_active = true AND experience_required > u.experience
        ORDER BY level ASC
        LIMIT 1
    ) next_level ON true
    WHERE u.appid = $1 AND u.enabled = true
)
SELECT id, account, current_level, level_name, experience, nickname, avatar, level_progress
FROM ranked
ORDER BY current_level DESC, experience DESC, id ASC
LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, query, appID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pointdomain.RankingItem, 0, limit)
	for rows.Next() {
		var item pointdomain.RankingItem
		var currentLevel int64
		if err := rows.Scan(&item.UserID, &item.Account, &currentLevel, &item.LevelName, &item.Experience, &item.Nickname, &item.Avatar, &item.Progress); err != nil {
			return nil, err
		}
		item.Type = "level"
		item.Value = currentLevel
		item.Rank = offset + len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	response := &pointdomain.RankingResponse{
		Type:       "level",
		Page:       page,
		Limit:      limit,
		Total:      total,
		Items:      items,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}
	if currentUserID > 0 {
		myRank, err := r.getMyLevelRank(ctx, appID, currentUserID)
		if err != nil {
			return nil, err
		}
		response.MyRank = myRank
	}
	return response, nil
}

func (r *Repository) GetTodaySignRankings(ctx context.Context, appID int64, now time.Time, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	signDate := now.Format("2006-01-02")

	var total int64
	countQuery := `SELECT COUNT(*) FROM daily_signins ds JOIN users u ON u.id = ds.user_id WHERE ds.appid = $1 AND ds.sign_date = $2 AND u.enabled = true`
	if err := r.pool.QueryRow(ctx, countQuery, appID, signDate).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), ds.signed_at, ds.consecutive_days
FROM daily_signins ds
JOIN users u ON u.id = ds.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE ds.appid = $1 AND ds.sign_date = $2
ORDER BY ds.signed_at ASC, ds.id ASC
LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, query, appID, signDate, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pointdomain.RankingItem, 0, limit)
	for rows.Next() {
		var item pointdomain.RankingItem
		var signedAt time.Time
		if err := rows.Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &signedAt, &item.ConsecutiveDays); err != nil {
			return nil, err
		}
		item.Type = "sign_today"
		item.Value = int64(item.ConsecutiveDays)
		item.SignedAt = &signedAt
		item.Period = signDate
		item.Rank = offset + len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	response := &pointdomain.RankingResponse{
		Type:       "sign_today",
		Page:       page,
		Limit:      limit,
		Total:      total,
		Items:      items,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}
	if currentUserID > 0 {
		myRank, err := r.GetMyTodaySignRank(ctx, appID, currentUserID, now)
		if err != nil {
			return nil, err
		}
		response.MyRank = myRank
	}
	return response, nil
}

func (r *Repository) GetConsecutiveSignRankings(ctx context.Context, appID int64, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	countQuery := `SELECT COUNT(*) FROM sign_stats ss JOIN users u ON u.id = ss.user_id WHERE ss.appid = $1 AND u.enabled = true`
	if err := r.pool.QueryRow(ctx, countQuery, appID).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), ss.consecutive_days, ss.total_sign_days, ss.last_sign_date, ss.last_sign_at
FROM sign_stats ss
JOIN users u ON u.id = ss.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE ss.appid = $1
ORDER BY ss.consecutive_days DESC, ss.total_sign_days DESC, ss.user_id ASC
LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, query, appID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pointdomain.RankingItem, 0, limit)
	for rows.Next() {
		var item pointdomain.RankingItem
		var lastSignDate *time.Time
		if err := rows.Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &item.ConsecutiveDays, &item.TotalSignDays, &lastSignDate, &item.LastSignAt); err != nil {
			return nil, err
		}
		item.Type = "sign_consecutive"
		item.Value = int64(item.ConsecutiveDays)
		if lastSignDate != nil {
			item.LastSignDate = lastSignDate.Format("2006-01-02")
		}
		item.Rank = offset + len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	response := &pointdomain.RankingResponse{
		Type:       "sign_consecutive",
		Page:       page,
		Limit:      limit,
		Total:      total,
		Items:      items,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}
	if currentUserID > 0 {
		myRank, err := r.GetMyConsecutiveSignRank(ctx, appID, currentUserID)
		if err != nil {
			return nil, err
		}
		response.MyRank = myRank
	}
	return response, nil
}

func (r *Repository) GetMonthlySignRankings(ctx context.Context, appID int64, now time.Time, page int, limit int, currentUserID int64) (*pointdomain.RankingResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	monthKey := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	period := now.Format("2006-01")

	var total int64
	countQuery := `SELECT COUNT(*) FROM sign_monthly_stats sms JOIN users u ON u.id = sms.user_id WHERE sms.appid = $1 AND sms.month_key = $2 AND u.enabled = true`
	if err := r.pool.QueryRow(ctx, countQuery, appID, monthKey).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), sms.sign_days, sms.total_integral_reward, sms.total_experience_reward, sms.last_sign_at
FROM sign_monthly_stats sms
JOIN users u ON u.id = sms.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE sms.appid = $1 AND sms.month_key = $2
ORDER BY sms.sign_days DESC, sms.last_sign_at ASC, sms.user_id ASC
LIMIT $3 OFFSET $4`
	rows, err := r.pool.Query(ctx, query, appID, monthKey, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pointdomain.RankingItem, 0, limit)
	for rows.Next() {
		var item pointdomain.RankingItem
		var totalIntegral int64
		var totalExperience int64
		if err := rows.Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &item.TotalSignDays, &totalIntegral, &totalExperience, &item.LastSignAt); err != nil {
			return nil, err
		}
		item.Type = "sign_monthly"
		item.Value = item.TotalSignDays
		item.Period = period
		item.Rank = offset + len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	response := &pointdomain.RankingResponse{
		Type:       "sign_monthly",
		Page:       page,
		Limit:      limit,
		Total:      total,
		Items:      items,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}
	if currentUserID > 0 {
		myRank, err := r.GetMyMonthlySignRank(ctx, appID, currentUserID, now)
		if err != nil {
			return nil, err
		}
		response.MyRank = myRank
	}
	return response, nil
}

func (r *Repository) getUserMetricRankings(ctx context.Context, appID int64, page int, limit int, currentUserID int64, metric string) (*pointdomain.RankingResponse, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1 AND enabled = true`, appID).Scan(&total); err != nil {
		return nil, err
	}

	query := `SELECT u.id, u.account, ` + metric + `, COALESCE(p.nickname, ''), COALESCE(p.avatar, '') FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id WHERE u.appid = $1 AND u.enabled = true ORDER BY ` + metric + ` DESC, u.id ASC LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, query, appID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]pointdomain.RankingItem, 0, limit)
	for rows.Next() {
		var item pointdomain.RankingItem
		var value int64
		if err := rows.Scan(&item.UserID, &item.Account, &value, &item.Nickname, &item.Avatar); err != nil {
			return nil, err
		}
		item.Type = metric
		item.Value = value
		item.Rank = offset + len(items) + 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	response := &pointdomain.RankingResponse{
		Type:       metric,
		Page:       page,
		Limit:      limit,
		Total:      total,
		Items:      items,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}
	if currentUserID > 0 {
		myRank, err := r.getMyMetricRank(ctx, appID, currentUserID, metric)
		if err != nil {
			return nil, err
		}
		response.MyRank = myRank
	}
	return response, nil
}

func (r *Repository) getMyMetricRank(ctx context.Context, appID int64, userID int64, metric string) (*pointdomain.RankingItem, error) {
	query := `SELECT u.id, u.account, ` + metric + `, COALESCE(p.nickname, ''), COALESCE(p.avatar, '') FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1 AND u.appid = $2 LIMIT 1`
	var item pointdomain.RankingItem
	if err := r.pool.QueryRow(ctx, query, userID, appID).Scan(&item.UserID, &item.Account, &item.Value, &item.Nickname, &item.Avatar); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rankQuery := `SELECT COUNT(*) + 1 FROM users WHERE appid = $1 AND enabled = true AND ` + metric + ` > (SELECT ` + metric + ` FROM users WHERE id = $2 AND appid = $1)`
	var rank int64
	if err := r.pool.QueryRow(ctx, rankQuery, appID, userID).Scan(&rank); err != nil {
		return nil, err
	}
	item.Rank = int(rank)
	item.Type = metric
	return &item, nil
}

func (r *Repository) getMyLevelRank(ctx context.Context, appID int64, userID int64) (*pointdomain.RankingItem, error) {
	query := `
WITH target AS (
    SELECT
        u.id,
        u.account,
        u.experience,
        COALESCE(p.nickname, '') AS nickname,
        COALESCE(p.avatar, '') AS avatar,
        COALESCE(curr.level, 1) AS current_level,
        COALESCE(curr.level_name, '新手') AS level_name,
        CASE
            WHEN next_level.experience_required IS NULL THEN 100.00
            WHEN next_level.experience_required = COALESCE(curr.experience_required, 0) THEN 0.00
            ELSE ROUND(
                ((u.experience - COALESCE(curr.experience_required, 0))::numeric / NULLIF((next_level.experience_required - COALESCE(curr.experience_required, 0)), 0)::numeric) * 100.00,
                2
            )
        END AS level_progress
    FROM users u
    LEFT JOIN user_profiles p ON p.user_id = u.id
    LEFT JOIN LATERAL (
        SELECT level, level_name, experience_required
        FROM user_levels
        WHERE is_active = true AND experience_required <= u.experience
        ORDER BY level DESC
        LIMIT 1
    ) curr ON true
    LEFT JOIN LATERAL (
        SELECT experience_required
        FROM user_levels
        WHERE is_active = true AND experience_required > u.experience
        ORDER BY level ASC
        LIMIT 1
    ) next_level ON true
    WHERE u.id = $2 AND u.appid = $1
    LIMIT 1
)
SELECT id, account, current_level, level_name, experience, nickname, avatar, level_progress
FROM target`

	var item pointdomain.RankingItem
	var currentLevel int64
	if err := r.pool.QueryRow(ctx, query, appID, userID).Scan(&item.UserID, &item.Account, &currentLevel, &item.LevelName, &item.Experience, &item.Nickname, &item.Avatar, &item.Progress); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	rankQuery := `
WITH current_user AS (
    SELECT
        COALESCE(curr.level, 1) AS current_level,
        u.experience
    FROM users u
    LEFT JOIN LATERAL (
        SELECT level
        FROM user_levels
        WHERE is_active = true AND experience_required <= u.experience
        ORDER BY level DESC
        LIMIT 1
    ) curr ON true
    WHERE u.id = $2 AND u.appid = $1
    LIMIT 1
),
all_users AS (
    SELECT
        COALESCE(curr.level, 1) AS current_level,
        u.experience
    FROM users u
    LEFT JOIN LATERAL (
        SELECT level
        FROM user_levels
        WHERE is_active = true AND experience_required <= u.experience
        ORDER BY level DESC
        LIMIT 1
    ) curr ON true
    WHERE u.appid = $1 AND u.enabled = true
)
SELECT COUNT(*) + 1
FROM all_users au, current_user cu
WHERE au.current_level > cu.current_level
   OR (au.current_level = cu.current_level AND au.experience > cu.experience)`

	var rank int64
	if err := r.pool.QueryRow(ctx, rankQuery, appID, userID).Scan(&rank); err != nil {
		return nil, err
	}

	item.Rank = int(rank)
	item.Type = "level"
	item.Value = currentLevel
	return &item, nil
}

func (r *Repository) GetMyTodaySignRank(ctx context.Context, appID int64, userID int64, now time.Time) (*pointdomain.RankingItem, error) {
	signDate := now.Format("2006-01-02")
	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), ds.signed_at, ds.consecutive_days, ds.id
FROM daily_signins ds
JOIN users u ON u.id = ds.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE ds.appid = $1 AND ds.user_id = $2 AND ds.sign_date = $3
LIMIT 1`
	var item pointdomain.RankingItem
	var signedAt time.Time
	var consecutiveDays int
	var signID int64
	if err := r.pool.QueryRow(ctx, query, appID, userID, signDate).Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &signedAt, &consecutiveDays, &signID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rankQuery := `SELECT COUNT(*) + 1
FROM daily_signins ds
JOIN users u ON u.id = ds.user_id AND u.enabled = true
WHERE ds.appid = $1 AND ds.sign_date = $2
  AND (ds.signed_at < $3 OR (ds.signed_at = $3 AND ds.id < $4))`
	var rank int64
	if err := r.pool.QueryRow(ctx, rankQuery, appID, signDate, signedAt, signID).Scan(&rank); err != nil {
		return nil, err
	}
	item.Rank = int(rank)
	item.Type = "sign_today"
	item.Value = int64(consecutiveDays)
	item.ConsecutiveDays = consecutiveDays
	item.SignedAt = &signedAt
	item.Period = signDate
	return &item, nil
}

func (r *Repository) GetMyConsecutiveSignRank(ctx context.Context, appID int64, userID int64) (*pointdomain.RankingItem, error) {
	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), ss.consecutive_days, ss.total_sign_days, ss.last_sign_date, ss.last_sign_at
FROM sign_stats ss
JOIN users u ON u.id = ss.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE ss.appid = $1 AND ss.user_id = $2
LIMIT 1`
	var item pointdomain.RankingItem
	var lastSignDate *time.Time
	if err := r.pool.QueryRow(ctx, query, appID, userID).Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &item.ConsecutiveDays, &item.TotalSignDays, &lastSignDate, &item.LastSignAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rankQuery := `SELECT COUNT(*) + 1
FROM sign_stats ss
JOIN users u ON u.id = ss.user_id AND u.enabled = true
WHERE ss.appid = $1
  AND (ss.consecutive_days > $2 OR (ss.consecutive_days = $2 AND ss.total_sign_days > $3) OR (ss.consecutive_days = $2 AND ss.total_sign_days = $3 AND ss.user_id < $4))`
	var rank int64
	if err := r.pool.QueryRow(ctx, rankQuery, appID, item.ConsecutiveDays, item.TotalSignDays, userID).Scan(&rank); err != nil {
		return nil, err
	}
	item.Rank = int(rank)
	item.Type = "sign_consecutive"
	item.Value = int64(item.ConsecutiveDays)
	if lastSignDate != nil {
		item.LastSignDate = lastSignDate.Format("2006-01-02")
	}
	return &item, nil
}

func (r *Repository) GetMyMonthlySignRank(ctx context.Context, appID int64, userID int64, now time.Time) (*pointdomain.RankingItem, error) {
	monthKey := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	period := now.Format("2006-01")
	query := `SELECT u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), sms.sign_days, sms.last_sign_at
FROM sign_monthly_stats sms
JOIN users u ON u.id = sms.user_id AND u.enabled = true
LEFT JOIN user_profiles p ON p.user_id = u.id
WHERE sms.appid = $1 AND sms.month_key = $2 AND sms.user_id = $3
LIMIT 1`
	var item pointdomain.RankingItem
	if err := r.pool.QueryRow(ctx, query, appID, monthKey, userID).Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &item.TotalSignDays, &item.LastSignAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rankQuery := `SELECT COUNT(*) + 1
FROM sign_monthly_stats sms
JOIN users u ON u.id = sms.user_id AND u.enabled = true
WHERE sms.appid = $1 AND sms.month_key = $2
  AND (sms.sign_days > $3 OR (sms.sign_days = $3 AND sms.last_sign_at < $4) OR (sms.sign_days = $3 AND sms.last_sign_at = $4 AND sms.user_id < $5))`
	var rank int64
	if err := r.pool.QueryRow(ctx, rankQuery, appID, monthKey, item.TotalSignDays, item.LastSignAt, userID).Scan(&rank); err != nil {
		return nil, err
	}
	item.Rank = int(rank)
	item.Type = "sign_monthly"
	item.Value = item.TotalSignDays
	item.Period = period
	return &item, nil
}

func (r *Repository) ResetUserIDSequence(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `SELECT setval(pg_get_serial_sequence('users', 'id'), COALESCE((SELECT MAX(id) FROM users), 1), true)`)
	return err
}

func (r *Repository) InsertLoginAudit(ctx context.Context, appID int64, userID int64, loginType string, provider string, tokenJTI string, ip string, deviceID string, userAgent string, status string, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	query := `INSERT INTO login_audit_logs (user_id, appid, login_type, provider, token_jti, login_ip, device_id, user_agent, status, metadata) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet, $7, $8, $9, $10)`
	_, err := r.pool.Exec(ctx, query, nullableInt64(userID), appID, loginType, nullableString(provider), nullableString(tokenJTI), nullableString(ip), nullableString(deviceID), nullableString(userAgent), status, metadataJSON)
	return err
}

func (r *Repository) InsertSessionAudit(ctx context.Context, appID int64, userID int64, tokenJTI string, eventType string, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	query := `INSERT INTO session_audit_logs (user_id, appid, token_jti, event_type, metadata) VALUES ($1, $2, $3, $4, $5)`
	_, err := r.pool.Exec(ctx, query, nullableInt64(userID), appID, tokenJTI, eventType, metadataJSON)
	return err
}

func (r *Repository) CreateUserNotification(ctx context.Context, appID int64, userID int64, notificationType string, title string, content string, level string, metadata map[string]any) error {
	metadataJSON, _ := json.Marshal(metadata)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var notificationID int64
	query := `INSERT INTO notifications (appid, user_id, type, title, content, level, status, metadata, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'unread', $7, NOW(), NOW())
RETURNING id`
	if err := tx.QueryRow(ctx, query, appID, nullableInt64(userID), notificationType, title, content, level, metadataJSON).Scan(&notificationID); err != nil {
		return err
	}

	receiptQuery := `INSERT INTO notification_receipts (notification_id, user_id, status, created_at, updated_at) VALUES ($1, $2, 'delivered', NOW(), NOW())
ON CONFLICT (notification_id, user_id) DO UPDATE SET status = EXCLUDED.status, updated_at = NOW()`
	if _, err := tx.Exec(ctx, receiptQuery, notificationID, userID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *Repository) ListAppNotifications(ctx context.Context, appID int64, query notificationdomain.AdminListQuery) ([]notificationdomain.AdminItem, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit
	baseQuery, args := buildAdminNotificationFilter(appID, query.Keyword, query.Type, query.Level)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+baseQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sql := `SELECT
	n.id,
	n.appid,
	n.user_id,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	n.type,
	n.title,
	n.content,
	n.level,
	COALESCE(nr.status, n.status),
	COALESCE(n.metadata, '{}'::jsonb),
	nr.read_at,
	n.created_at,
	n.updated_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]notificationdomain.AdminItem, 0, limit)
	for rows.Next() {
		item, err := scanAdminNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) ListAppNotificationsForExport(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]notificationdomain.AdminItem, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildAdminNotificationFilter(appID, query.Keyword, query.Type, query.Level)
	sql := `SELECT
	n.id,
	n.appid,
	n.user_id,
	COALESCE(u.account, ''),
	COALESCE(p.nickname, ''),
	n.type,
	n.title,
	n.content,
	n.level,
	COALESCE(nr.status, n.status),
	COALESCE(n.metadata, '{}'::jsonb),
	nr.read_at,
	n.created_at,
	n.updated_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]notificationdomain.AdminItem, 0, limit)
	for rows.Next() {
		item, err := scanAdminNotification(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ResolveAppNotificationIDs(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]int64, error) {
	limit := query.Limit
	if limit < 1 {
		limit = 5000
	}
	baseQuery, args := buildAdminNotificationFilter(appID, query.Keyword, query.Type, query.Level)
	sql := `SELECT n.id` + baseQuery +
		fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *Repository) DeleteAppNotifications(ctx context.Context, appID int64, ids []int64) (int64, []int64, error) {
	if len(ids) == 0 {
		return 0, nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT user_id FROM notifications WHERE appid = $1 AND id = ANY($2) AND user_id IS NOT NULL ORDER BY user_id ASC`, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	affectedUsers := make([]int64, 0, len(ids))
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return 0, nil, err
		}
		affectedUsers = append(affectedUsers, userID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, nil, err
	}
	rows.Close()

	result, err := r.pool.Exec(ctx, `DELETE FROM notifications WHERE appid = $1 AND id = ANY($2)`, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	return result.RowsAffected(), affectedUsers, nil
}

func (r *Repository) ListUserNotifications(ctx context.Context, appID int64, userID int64, query notificationdomain.UserListQuery) ([]notificationdomain.Item, int64, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	offset := (page - 1) * limit

	baseQuery, args := buildUserNotificationFilter(appID, userID, query.Status, query.Type, query.Level)
	countQuery := `SELECT COUNT(*)` + baseQuery
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}

	unreadQuery := `SELECT COUNT(*)
FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = $2
WHERE n.appid = $1 AND nr.status <> 'read'`
	var unread int64
	if err := r.pool.QueryRow(ctx, unreadQuery, appID, userID).Scan(&unread); err != nil {
		return nil, 0, 0, err
	}

	sql := `SELECT n.id, n.appid, n.user_id, n.type, n.title, n.content, n.level, nr.status, n.metadata, nr.read_at, n.created_at, n.updated_at` + baseQuery +
		fmt.Sprintf(`
ORDER BY n.created_at DESC, n.id DESC
LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	items := make([]notificationdomain.Item, 0, limit)
	for rows.Next() {
		item, err := scanNotification(rows)
		if err != nil {
			return nil, 0, 0, err
		}
		items = append(items, *item)
	}
	return items, total, unread, rows.Err()
}

func (r *Repository) CountUnreadNotifications(ctx context.Context, appID int64, userID int64) (int64, error) {
	query := `SELECT COUNT(*)
FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = $2
WHERE n.appid = $1 AND nr.status <> 'read'`
	var unread int64
	if err := r.pool.QueryRow(ctx, query, appID, userID).Scan(&unread); err != nil {
		return 0, err
	}
	return unread, nil
}

func (r *Repository) MarkNotificationRead(ctx context.Context, appID int64, userID int64, notificationID int64) error {
	query := `UPDATE notification_receipts nr
SET status = 'read', read_at = NOW(), updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND n.appid = $1
  AND nr.user_id = $2
  AND nr.notification_id = $3`
	_, err := r.pool.Exec(ctx, query, appID, userID, notificationID)
	return err
}

func (r *Repository) MarkNotificationsRead(ctx context.Context, appID int64, userID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query := `UPDATE notification_receipts nr
SET status = 'read', read_at = NOW(), updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND n.appid = $1
  AND nr.user_id = $2
  AND nr.notification_id = ANY($3)
  AND nr.status <> 'read'`
	result, err := r.pool.Exec(ctx, query, appID, userID, ids)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) MarkAllNotificationsRead(ctx context.Context, appID int64, userID int64) error {
	query := `UPDATE notification_receipts nr
SET status = 'read', read_at = NOW(), updated_at = NOW()
FROM notifications n
WHERE nr.notification_id = n.id
  AND n.appid = $1
  AND nr.user_id = $2
  AND nr.status <> 'read'`
	_, err := r.pool.Exec(ctx, query, appID, userID)
	return err
}

func (r *Repository) DeleteUserNotification(ctx context.Context, appID int64, userID int64, notificationID int64) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM notifications WHERE appid = $1 AND user_id = $2 AND id = $3`, appID, userID, notificationID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (r *Repository) DeleteUserNotifications(ctx context.Context, appID int64, userID int64, status string, notificationType string, level string) (int64, error) {
	status = strings.TrimSpace(status)
	notificationType = strings.TrimSpace(notificationType)
	level = strings.TrimSpace(level)
	args := []any{appID, userID}
	query := `DELETE FROM notifications n
USING notification_receipts nr
WHERE n.id = nr.notification_id
  AND n.appid = $1
  AND n.user_id = $2
  AND nr.user_id = $2`
	if status != "" && status != "all" {
		query += fmt.Sprintf(" AND nr.status = $%d", len(args)+1)
		args = append(args, status)
	}
	if notificationType != "" && notificationType != "all" {
		query += fmt.Sprintf(" AND n.type = $%d", len(args)+1)
		args = append(args, notificationType)
	}
	if level != "" && level != "all" {
		query += fmt.Sprintf(" AND n.level = $%d", len(args)+1)
		args = append(args, level)
	}
	result, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func scanUser(row interface{ Scan(dest ...any) error }) (*userdomain.User, error) {
	var user userdomain.User
	if err := row.Scan(&user.ID, &user.AppID, &user.Account, &user.PasswordHash, &user.Integral, &user.Experience, &user.Enabled, &user.DisabledEndTime, &user.VIPExpireAt, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &user, nil
}

func scanAdminUser(row interface{ Scan(dest ...any) error }) (*userdomain.AdminUserView, error) {
	var item userdomain.AdminUserView
	var extraBytes []byte
	if err := row.Scan(
		&item.ID,
		&item.AppID,
		&item.Account,
		&item.Integral,
		&item.Experience,
		&item.Enabled,
		&item.DisabledEndTime,
		&item.VIPExpireAt,
		&item.CreatedAt,
		&item.UpdatedAt,
		&item.Nickname,
		&item.Avatar,
		&item.Email,
		&item.Phone,
		&extraBytes,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(extraBytes, &item.Extra)
	item.RegisterIP = stringFromMap(item.Extra, "register_ip")
	item.RegisterProvince = stringFromMap(item.Extra, "register_province")
	item.RegisterCity = stringFromMap(item.Extra, "register_city")
	item.RegisterISP = stringFromMap(item.Extra, "register_isp")
	item.DisabledReason = stringFromMap(item.Extra, "disabled_reason")
	item.MarkCode = stringFromMap(item.Extra, "markcode")
	item.RegisterTime = timeFromMap(item.Extra, "register_time")
	if item.RegisterTime == nil {
		createdAt := item.CreatedAt
		item.RegisterTime = &createdAt
	}
	return &item, nil
}

func scanApp(row interface{ Scan(dest ...any) error }) (*appdomain.App, error) {
	var item appdomain.App
	var settings []byte
	if err := row.Scan(&item.ID, &item.Name, &item.AppKey, &item.Status, &item.DisabledReason, &item.RegisterStatus, &item.DisabledRegisterReason, &item.LoginStatus, &item.DisabledLoginReason, &settings, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(settings, &item.Settings)
	return &item, nil
}

func scanBanner(row interface{ Scan(dest ...any) error }) (*appdomain.Banner, error) {
	var item appdomain.Banner
	if err := row.Scan(&item.ID, &item.Header, &item.Title, &item.Content, &item.URL, &item.Type, &item.Position, &item.Status, &item.StartTime, &item.EndTime, &item.ViewCount, &item.ClickCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanNotice(row interface{ Scan(dest ...any) error }) (*appdomain.Notice, error) {
	var item appdomain.Notice
	if err := row.Scan(&item.ID, &item.Title, &item.Content, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanDailySign(row interface{ Scan(dest ...any) error }) (*userdomain.DailySignIn, error) {
	var sign userdomain.DailySignIn
	var signDate time.Time
	if err := row.Scan(&sign.ID, &sign.UserID, &sign.AppID, &sign.SignedAt, &signDate, &sign.IntegralReward, &sign.ExperienceReward, &sign.IntegralBefore, &sign.IntegralAfter, &sign.ExperienceBefore, &sign.ExperienceAfter, &sign.ConsecutiveDays, &sign.RewardMultiplier, &sign.BonusType, &sign.BonusDescription, &sign.SignInSource, &sign.DeviceInfo, &sign.IPAddress, &sign.Location, &sign.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	sign.SignDate = signDate.Format("2006-01-02")
	return &sign, nil
}

func scanSignStats(row interface{ Scan(dest ...any) error }) (*userdomain.SignStats, error) {
	var item userdomain.SignStats
	var lastSignDate *time.Time
	if err := row.Scan(&item.UserID, &item.AppID, &lastSignDate, &item.LastSignAt, &item.ConsecutiveDays, &item.TotalSignDays, &item.TotalIntegralReward, &item.TotalExperienceReward, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if lastSignDate != nil {
		item.LastSignDate = lastSignDate.Format("2006-01-02")
	}
	return &item, nil
}

func scanSettingRecord(row interface{ Scan(dest ...any) error }) (*userdomain.SettingRecord, error) {
	var item userdomain.SettingRecord
	var raw []byte
	if err := row.Scan(&item.ID, &item.UserID, &item.AppID, &item.Category, &raw, &item.Version, &item.IsActive, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(raw, &item.Settings)
	return &item, nil
}

func scanTransaction(row interface{ Scan(dest ...any) error }) (*pointdomain.Transaction, error) {
	var item pointdomain.Transaction
	var sourceID *int64
	var extra []byte
	if err := row.Scan(&item.ID, &item.TransactionNo, &item.UserID, &item.AppID, &item.Type, &item.Category, &item.Amount, &item.BalanceBefore, &item.BalanceAfter, &item.LevelBefore, &item.LevelAfter, &item.Status, &item.Title, &item.Description, &sourceID, &item.SourceType, &item.Multiplier, &item.IsLevelUp, &item.ClientIP, &item.UserAgent, &extra, &item.CreatedAt); err != nil {
		return nil, err
	}
	item.SourceID = sourceID
	_ = json.Unmarshal(extra, &item.ExtraData)
	return &item, nil
}

func scanNotification(row interface{ Scan(dest ...any) error }) (*notificationdomain.Item, error) {
	var item notificationdomain.Item
	var metadata []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Type, &item.Title, &item.Content, &item.Level, &item.Status, &metadata, &item.ReadAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func scanAdminNotification(row interface{ Scan(dest ...any) error }) (*notificationdomain.AdminItem, error) {
	var item notificationdomain.AdminItem
	var metadata []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Account, &item.Nickname, &item.Type, &item.Title, &item.Content, &item.Level, &item.Status, &metadata, &item.ReadAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func scanLoginAudit(row interface{ Scan(dest ...any) error }) (*appdomain.LoginAuditItem, error) {
	var item appdomain.LoginAuditItem
	var userID *int64
	var metadata []byte
	if err := row.Scan(
		&item.ID,
		&userID,
		&item.AppID,
		&item.Account,
		&item.Nickname,
		&item.LoginType,
		&item.Provider,
		&item.TokenJTI,
		&item.LoginIP,
		&item.DeviceID,
		&item.UserAgent,
		&item.Status,
		&metadata,
		&item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.UserID = userID
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func buildAdminNotificationFilter(appID int64, keyword string, notificationType string, level string) (string, []any) {
	keyword = strings.TrimSpace(keyword)
	notificationType = strings.TrimSpace(notificationType)
	level = strings.TrimSpace(level)

	baseQuery := ` FROM notifications n
LEFT JOIN users u ON u.id = n.user_id
LEFT JOIN user_profiles p ON p.user_id = n.user_id
LEFT JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = n.user_id
WHERE n.appid = $1`
	args := []any{appID}
	if notificationType != "" && notificationType != "all" {
		baseQuery += fmt.Sprintf(" AND n.type = $%d", len(args)+1)
		args = append(args, notificationType)
	}
	if level != "" && level != "all" {
		baseQuery += fmt.Sprintf(" AND n.level = $%d", len(args)+1)
		args = append(args, level)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		baseQuery += fmt.Sprintf(`
  AND (
    COALESCE(n.title, '') ILIKE $%d
    OR COALESCE(n.content, '') ILIKE $%d
    OR COALESCE(u.account, '') ILIKE $%d
    OR COALESCE(p.nickname, '') ILIKE $%d
  )`, len(args)+1, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, like)
	}
	return baseQuery, args
}

func buildUserNotificationFilter(appID int64, userID int64, status string, notificationType string, level string) (string, []any) {
	status = strings.TrimSpace(status)
	notificationType = strings.TrimSpace(notificationType)
	level = strings.TrimSpace(level)

	baseQuery := ` FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = $2
WHERE n.appid = $1 AND n.user_id = $2`
	args := []any{appID, userID}
	if status != "" && status != "all" {
		baseQuery += fmt.Sprintf(" AND nr.status = $%d", len(args)+1)
		args = append(args, status)
	}
	if notificationType != "" && notificationType != "all" {
		baseQuery += fmt.Sprintf(" AND n.type = $%d", len(args)+1)
		args = append(args, notificationType)
	}
	if level != "" && level != "all" {
		baseQuery += fmt.Sprintf(" AND n.level = $%d", len(args)+1)
		args = append(args, level)
	}
	return baseQuery, args
}

func scanSessionAudit(row interface{ Scan(dest ...any) error }) (*appdomain.SessionAuditItem, error) {
	var item appdomain.SessionAuditItem
	var userID *int64
	var metadata []byte
	if err := row.Scan(
		&item.ID,
		&userID,
		&item.AppID,
		&item.Account,
		&item.Nickname,
		&item.TokenJTI,
		&item.EventType,
		&metadata,
		&item.CreatedAt,
	); err != nil {
		return nil, err
	}
	item.UserID = userID
	_ = json.Unmarshal(metadata, &item.Metadata)
	return &item, nil
}

func buildLoginAuditFilter(appID int64, keyword string, status string) (string, []any) {
	status = strings.TrimSpace(status)
	keyword = strings.TrimSpace(keyword)

	baseQuery := ` FROM login_audit_logs l
LEFT JOIN users u ON u.id = l.user_id
LEFT JOIN user_profiles p ON p.user_id = l.user_id
WHERE l.appid = $1`
	args := []any{appID}

	if status != "" && status != "all" {
		baseQuery += fmt.Sprintf(" AND l.status = $%d", len(args)+1)
		args = append(args, status)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		baseQuery += fmt.Sprintf(`
  AND (
    COALESCE(u.account, '') ILIKE $%d
    OR COALESCE(p.nickname, '') ILIKE $%d
    OR COALESCE(HOST(l.login_ip), '') ILIKE $%d
    OR COALESCE(l.device_id, '') ILIKE $%d
    OR COALESCE(l.user_agent, '') ILIKE $%d
    OR COALESCE(l.provider, '') ILIKE $%d
  )`, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, like)
	}
	return baseQuery, args
}

func buildSessionAuditFilter(appID int64, keyword string, eventType string) (string, []any) {
	eventType = strings.TrimSpace(eventType)
	keyword = strings.TrimSpace(keyword)

	baseQuery := ` FROM session_audit_logs s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN user_profiles p ON p.user_id = s.user_id
WHERE s.appid = $1`
	args := []any{appID}

	if eventType != "" && eventType != "all" {
		baseQuery += fmt.Sprintf(" AND s.event_type = $%d", len(args)+1)
		args = append(args, eventType)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		baseQuery += fmt.Sprintf(`
  AND (
    COALESCE(u.account, '') ILIKE $%d
    OR COALESCE(p.nickname, '') ILIKE $%d
    OR COALESCE(s.token_jti, '') ILIKE $%d
    OR COALESCE(s.event_type, '') ILIKE $%d
    OR COALESCE(s.metadata->>'ip', '') ILIKE $%d
    OR COALESCE(s.metadata->>'device_id', '') ILIKE $%d
    OR COALESCE(s.metadata->>'user_agent', '') ILIKE $%d
  )`, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1, len(args)+1)
		args = append(args, like)
	}
	return baseQuery, args
}

func buildUserLoginAuditFilter(appID int64, userID int64, status string) (string, []any) {
	status = strings.TrimSpace(status)

	baseQuery := ` FROM login_audit_logs l
LEFT JOIN users u ON u.id = l.user_id
LEFT JOIN user_profiles p ON p.user_id = l.user_id
WHERE l.appid = $1 AND l.user_id = $2`
	args := []any{appID, userID}
	if status != "" && status != "all" {
		baseQuery += fmt.Sprintf(" AND l.status = $%d", len(args)+1)
		args = append(args, status)
	}
	return baseQuery, args
}

func buildUserSessionAuditFilter(appID int64, userID int64, eventType string) (string, []any) {
	eventType = strings.TrimSpace(eventType)

	baseQuery := ` FROM session_audit_logs s
LEFT JOIN users u ON u.id = s.user_id
LEFT JOIN user_profiles p ON p.user_id = s.user_id
WHERE s.appid = $1 AND s.user_id = $2`
	args := []any{appID, userID}
	if eventType != "" && eventType != "all" {
		baseQuery += fmt.Sprintf(" AND s.event_type = $%d", len(args)+1)
		args = append(args, eventType)
	}
	return baseQuery, args
}

func scanLevelRecord(row interface{ Scan(dest ...any) error }) (*levelRecordRow, error) {
	var item levelRecordRow
	if err := row.Scan(
		&item.UserID,
		&item.AppID,
		&item.CurrentLevel,
		&item.CurrentExperience,
		&item.TotalExperience,
		&item.NextLevelExperience,
		&item.LevelProgress,
		&item.HighestLevel,
		&item.LevelUpCount,
		&item.LastLevelUpAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func normalizeNotFound(err error) error {
	if err == nil {
		return nil
	}
	if err == pgx.ErrNoRows {
		return nil
	}
	return err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func stringFromMap(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func timeFromMap(data map[string]any, key string) *time.Time {
	if data == nil {
		return nil
	}
	value, ok := data[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case time.Time:
		v := typed.UTC()
		return &v
	case string:
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05"} {
			parsed, err := time.Parse(layout, strings.TrimSpace(typed))
			if err == nil {
				v := parsed.UTC()
				return &v
			}
		}
	}
	return nil
}

func generateTransactionNo(prefix string) string {
	return prefix + time.Now().UTC().Format("20060102150405") + randomDigits(6)
}

func randomDigits(length int) string {
	const digits = "0123456789"
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = digits[rng.Intn(len(digits))]
	}
	return string(buf)
}

func collectLevelUpRewards(levels []pointdomain.LevelConfig, fromLevel int, toLevel int) []map[string]any {
	if toLevel <= fromLevel {
		return nil
	}
	items := make([]map[string]any, 0, toLevel-fromLevel)
	for _, level := range levels {
		if level.Level <= fromLevel || level.Level > toLevel {
			continue
		}
		items = append(items, map[string]any{
			"level":      level.Level,
			"level_name": level.LevelName,
			"rewards":    level.Rewards,
			"privileges": level.Privileges,
		})
	}
	return items
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func LegacyOAuthAccount(provider string, providerUserID string) string {
	base := provider + "_" + providerUserID
	if len(base) <= 128 {
		return base
	}
	return base[:128]
}

var ErrAccountAlreadyExists = errors.New("account already exists")
var ErrAlreadySigned = errors.New("already signed today")
var ErrUserNotFound = errors.New("user not found")

// ── 工作台聚合查询 ──

func (r *Repository) CountPendingRoleApplications(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM role_applications WHERE status = 'pending'`).Scan(&count)
	return count, err
}

type DashboardAuditLog struct {
	Action    string
	Detail    string
	IP        string
	CreatedAt time.Time
}

func (r *Repository) ListRecentAuditLogs(ctx context.Context, adminID int64, limit int) ([]DashboardAuditLog, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT action, COALESCE(detail,''), COALESCE(ip,''), created_at FROM admin_audit_logs WHERE admin_id = $1 ORDER BY created_at DESC LIMIT $2`,
		adminID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DashboardAuditLog
	for rows.Next() {
		var l DashboardAuditLog
		if err := rows.Scan(&l.Action, &l.Detail, &l.IP, &l.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	return items, rows.Err()
}

func (r *Repository) CountRecentFirewallBlocks(ctx context.Context, dur time.Duration) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM firewall_logs WHERE created_at > $1`, time.Now().Add(-dur)).Scan(&count)
	return count, err
}

func (r *Repository) CountRecentLoginFailures(ctx context.Context, dur time.Duration) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'admin.login_failed' AND created_at > $1`, time.Now().Add(-dur)).Scan(&count)
	return count, err
}

func (r *Repository) CountApps(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM apps`).Scan(&count)
	return count, err
}

func (r *Repository) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (r *Repository) CountTodayLogins(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_audit_logs WHERE action = 'admin.login' AND created_at > $1`, time.Now().Truncate(24*time.Hour)).Scan(&count)
	return count, err
}
