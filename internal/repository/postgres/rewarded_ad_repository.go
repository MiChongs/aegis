package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	rewardedad "aegis/internal/domain/rewardedad"
)

// 激励广告：配置、观看记录查询与统计。结算（含发奖）见 rewarded_ad_settle_repository.go。

var (
	// ErrRewardedAdViewNotFound 观看记录不存在。
	ErrRewardedAdViewNotFound = errors.New("rewarded ad view not found")
	// ErrRewardedAdUserMismatch 这次观看已被另一个账号认领。
	ErrRewardedAdUserMismatch = errors.New("rewarded ad view belongs to another user")
	// ErrRewardedAdAlreadyGranted 已发放的记录不能再补发。
	ErrRewardedAdAlreadyGranted = errors.New("rewarded ad view already granted")
)

// rewardedAdQuerier 连接池与事务的公共子集：用量查询在事务内外各用一次。
type rewardedAdQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ── 配置 ──

// GetRewardedAdConfig 读取应用的激励广告配置；没配过时返回 (nil, nil)。
func (r *Repository) GetRewardedAdConfig(ctx context.Context, appID int64) (*rewardedad.Config, error) {
	var cfg rewardedad.Config
	var scenes []byte
	var updatedBy *string
	err := r.pool.QueryRow(ctx, `SELECT appid, enabled, provider, provider_app_id, security_key_cipher,
security_key_hint, verify_mode, daily_limit, scenes, updated_by, updated_at
FROM app_rewarded_ad_configs WHERE appid = $1`, appID).Scan(
		&cfg.AppID, &cfg.Enabled, &cfg.Provider, &cfg.ProviderAppID, &cfg.SecurityKeyCipher,
		&cfg.SecurityKeyHint, &cfg.VerifyMode, &cfg.DailyLimit, &scenes, &updatedBy, &cfg.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(scenes, &cfg.Scenes); err != nil {
		return nil, fmt.Errorf("decode rewarded ad scenes: %w", err)
	}
	if cfg.Scenes == nil {
		cfg.Scenes = []rewardedad.Scene{}
	}
	cfg.UpdatedBy = derefString(updatedBy)
	return &cfg, nil
}

// SaveRewardedAdConfig 整体覆盖保存（密文由服务层决定沿用还是替换）。
func (r *Repository) SaveRewardedAdConfig(ctx context.Context, cfg rewardedad.Config) (*rewardedad.Config, error) {
	scenes := cfg.Scenes
	if scenes == nil {
		scenes = []rewardedad.Scene{}
	}
	payload, err := json.Marshal(scenes)
	if err != nil {
		return nil, err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO app_rewarded_ad_configs
(appid, enabled, provider, provider_app_id, security_key_cipher, security_key_hint, verify_mode,
 daily_limit, scenes, updated_by, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NOW())
ON CONFLICT (appid) DO UPDATE SET
  enabled = EXCLUDED.enabled,
  provider = EXCLUDED.provider,
  provider_app_id = EXCLUDED.provider_app_id,
  security_key_cipher = EXCLUDED.security_key_cipher,
  security_key_hint = EXCLUDED.security_key_hint,
  verify_mode = EXCLUDED.verify_mode,
  daily_limit = EXCLUDED.daily_limit,
  scenes = EXCLUDED.scenes,
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()`,
		cfg.AppID, cfg.Enabled, cfg.Provider, cfg.ProviderAppID, cfg.SecurityKeyCipher,
		cfg.SecurityKeyHint, cfg.VerifyMode, cfg.DailyLimit, payload, nullableString(cfg.UpdatedBy))
	if err != nil {
		return nil, err
	}
	return r.GetRewardedAdConfig(ctx, cfg.AppID)
}

// ── 观看记录 ──

const rewardedAdViewColumns = `v.id, v.appid, COALESCE(v.user_id, 0), v.trans_id, v.scene, v.placement_id,
v.status, v.reason, v.server_verified_at, v.reward_name, v.reward_amount, v.network_id, v.extra,
v.client_reported_at, v.client_verified, v.client_error, v.device_id, v.client_ip, v.rewards,
v.granted_at, v.operator, v.created_at, v.updated_at`

func scanRewardedAdView(row interface{ Scan(dest ...any) error }, extra ...any) (*rewardedad.View, error) {
	var view rewardedad.View
	var rewards []byte
	dest := []any{&view.ID, &view.AppID, &view.UserID, &view.TransID, &view.Scene, &view.PlacementID,
		&view.Status, &view.Reason, &view.ServerVerifiedAt, &view.RewardName, &view.RewardAmount,
		&view.NetworkID, &view.Extra, &view.ClientReportedAt, &view.ClientVerified, &view.ClientError,
		&view.DeviceID, &view.ClientIP, &rewards, &view.GrantedAt, &view.Operator, &view.CreatedAt,
		&view.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRewardedAdViewNotFound
		}
		return nil, err
	}
	if len(rewards) > 0 {
		_ = json.Unmarshal(rewards, &view.Results)
	}
	if view.Results == nil {
		view.Results = []rewardedad.RewardResult{}
	}
	return &view, nil
}

// GetRewardedAdView 按 ID 取一条观看记录。
func (r *Repository) GetRewardedAdView(ctx context.Context, appID, viewID int64) (*rewardedad.View, error) {
	var account string
	view, err := scanRewardedAdView(r.pool.QueryRow(ctx, `SELECT `+rewardedAdViewColumns+`, COALESCE(u.account, '')
FROM app_rewarded_ad_views v LEFT JOIN users u ON u.id = v.user_id
WHERE v.appid = $1 AND v.id = $2`, appID, viewID), &account)
	if err != nil {
		return nil, err
	}
	view.Account = account
	return view, nil
}

// ListRewardedAdViews 观看记录分页（管理端与「我的记录」共用）。
func (r *Repository) ListRewardedAdViews(ctx context.Context, query rewardedad.ViewQuery) (*rewardedad.ViewPage, error) {
	where := []string{"v.appid = $1"}
	args := []any{query.AppID}

	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("v.user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("v.status = $%d", len(args)))
	}
	if scene := strings.TrimSpace(query.Scene); scene != "" {
		args = append(args, scene)
		where = append(where, fmt.Sprintf("v.scene = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(v.trans_id ILIKE $%d OR u.account ILIKE $%d)", len(args), len(args)))
	}
	if query.Start != nil {
		args = append(args, *query.Start)
		where = append(where, fmt.Sprintf("v.created_at >= $%d", len(args)))
	}
	if query.End != nil {
		args = append(args, *query.End)
		where = append(where, fmt.Sprintf("v.created_at < $%d", len(args)))
	}
	clause := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM app_rewarded_ad_views v LEFT JOIN users u ON u.id = v.user_id `+clause,
		args...).Scan(&total); err != nil {
		return nil, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	args = append(args, limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, `SELECT `+rewardedAdViewColumns+`, COALESCE(u.account, '')
FROM app_rewarded_ad_views v LEFT JOIN users u ON u.id = v.user_id
`+clause+fmt.Sprintf(" ORDER BY v.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]rewardedad.View, 0, limit)
	for rows.Next() {
		var account string
		view, err := scanRewardedAdView(rows, &account)
		if err != nil {
			return nil, err
		}
		view.Account = account
		items = append(items, *view)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &rewardedad.ViewPage{Items: items, Total: total, Page: page, Limit: limit, TotalPages: totalPages}, nil
}

// RewardedAdUsage 一个用户今天在各场景领了几次、每个场景最近一次是什么时候。
func (r *Repository) RewardedAdUsage(ctx context.Context, appID, userID int64, dayStart time.Time) (*rewardedad.Usage, error) {
	return rewardedAdUsage(ctx, r.pool, appID, userID, dayStart)
}

// rewardedAdUsage 只数已发放的行（部分索引 idx_app_rewarded_ad_views_user_granted）。
//
// 冷却最长一天，所以「最近一次」只需要往回看一天；与今天零点取更早的那个，
// 一条查询同时回答日限额与冷却两个问题。
func rewardedAdUsage(ctx context.Context, q rewardedAdQuerier, appID, userID int64, dayStart time.Time) (*rewardedad.Usage, error) {
	usage := &rewardedad.Usage{TodayByScene: map[string]int{}, LastByScene: map[string]time.Time{}}
	rows, err := q.Query(ctx, `SELECT scene,
       COUNT(*) FILTER (WHERE granted_at >= $3)::int,
       MAX(granted_at)
FROM app_rewarded_ad_views
WHERE appid = $1 AND user_id = $2 AND status = 'granted'
  AND granted_at >= LEAST($3::timestamptz, NOW() - INTERVAL '1 day')
GROUP BY scene`, appID, userID, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var scene string
		var today int
		var last time.Time
		if err := rows.Scan(&scene, &today, &last); err != nil {
			return nil, err
		}
		usage.TodayByScene[scene] = today
		usage.TodayTotal += today
		usage.LastByScene[scene] = last
	}
	return usage, rows.Err()
}

// ── 统计 ──

// RewardedAdStats 统计窗口内按天、按场景的观看与发放情况。缺失的日期由服务层补零。
func (r *Repository) RewardedAdStats(ctx context.Context, appID int64, since time.Time, timezone string) (*rewardedad.Stats, error) {
	stats := &rewardedad.Stats{Trend: []rewardedad.StatsDay{}, Scenes: []rewardedad.StatsScene{}}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*),
       COUNT(*) FILTER (WHERE status = 'granted'),
       COUNT(*) FILTER (WHERE status = 'rejected'),
       COUNT(DISTINCT user_id) FILTER (WHERE status = 'granted')
FROM app_rewarded_ad_views
WHERE appid = $1 AND created_at >= $2
GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day rewardedad.StatsDay
		if err := rows.Scan(&day.Date, &day.Views, &day.Granted, &day.Rejected, &day.Users); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sceneRows, err := r.pool.Query(ctx, `SELECT scene, COUNT(*), COUNT(*) FILTER (WHERE status = 'granted')
FROM app_rewarded_ad_views
WHERE appid = $1 AND created_at >= $2
GROUP BY scene ORDER BY COUNT(*) DESC`, appID, since)
	if err != nil {
		return nil, err
	}
	for sceneRows.Next() {
		var item rewardedad.StatsScene
		if err := sceneRows.Scan(&item.Scene, &item.Views, &item.Granted); err != nil {
			sceneRows.Close()
			return nil, err
		}
		stats.Scenes = append(stats.Scenes, item)
	}
	sceneRows.Close()
	if err := sceneRows.Err(); err != nil {
		return nil, err
	}

	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE status = 'pending'),
  COUNT(*) FILTER (WHERE status = 'granted')
FROM app_rewarded_ad_views WHERE appid = $1`, appID).Scan(
		&stats.Summary.Pending, &stats.Summary.TotalGranted); err != nil {
		return nil, err
	}
	return stats, nil
}
