package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/adpolicy"
)

// 广告服务策略：应用策略、用户的选择与变更历史、开屏广告记录。

// ── 策略 ──

// GetAdPolicy 读取应用的广告服务策略；没配过时返回 (nil, nil)。
func (r *Repository) GetAdPolicy(ctx context.Context, appID int64) (*adpolicy.Policy, error) {
	var policy adpolicy.Policy
	var tools []byte
	var updatedBy *string
	err := r.pool.QueryRow(ctx, `SELECT appid, enabled, consent_version, policy_url, vip_exempt, basic_tools,
splash_enabled, splash_placement_id, splash_min_interval_seconds, splash_daily_limit, updated_by, updated_at
FROM app_ad_policies WHERE appid = $1`, appID).Scan(
		&policy.AppID, &policy.Enabled, &policy.ConsentVersion, &policy.PolicyURL, &policy.VipExempt, &tools,
		&policy.Splash.Enabled, &policy.Splash.PlacementID, &policy.Splash.MinIntervalSeconds,
		&policy.Splash.DailyLimit, &updatedBy, &policy.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(tools, &policy.BasicTools); err != nil {
		return nil, fmt.Errorf("decode ad policy basic tools: %w", err)
	}
	if policy.BasicTools == nil {
		policy.BasicTools = []string{}
	}
	policy.UpdatedBy = derefString(updatedBy)
	return &policy, nil
}

// SaveAdPolicy 整体覆盖保存。
func (r *Repository) SaveAdPolicy(ctx context.Context, policy adpolicy.Policy) (*adpolicy.Policy, error) {
	tools := policy.BasicTools
	if tools == nil {
		tools = []string{}
	}
	payload, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO app_ad_policies
(appid, enabled, consent_version, policy_url, vip_exempt, basic_tools, splash_enabled, splash_placement_id,
 splash_min_interval_seconds, splash_daily_limit, updated_by, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
ON CONFLICT (appid) DO UPDATE SET
  enabled = EXCLUDED.enabled,
  consent_version = EXCLUDED.consent_version,
  policy_url = EXCLUDED.policy_url,
  vip_exempt = EXCLUDED.vip_exempt,
  basic_tools = EXCLUDED.basic_tools,
  splash_enabled = EXCLUDED.splash_enabled,
  splash_placement_id = EXCLUDED.splash_placement_id,
  splash_min_interval_seconds = EXCLUDED.splash_min_interval_seconds,
  splash_daily_limit = EXCLUDED.splash_daily_limit,
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()`,
		policy.AppID, policy.Enabled, policy.ConsentVersion, policy.PolicyURL, policy.VipExempt, payload,
		policy.Splash.Enabled, policy.Splash.PlacementID, policy.Splash.MinIntervalSeconds,
		policy.Splash.DailyLimit, nullableString(policy.UpdatedBy))
	if err != nil {
		return nil, err
	}
	return r.GetAdPolicy(ctx, policy.AppID)
}

// ── 用户的选择 ──

const adConsentColumns = `c.user_id, c.accepted, c.version, c.source, c.device_id, c.client_ip, c.decided_at`

func scanAdConsent(row interface{ Scan(dest ...any) error }, extra ...any) (*adpolicy.Consent, error) {
	var consent adpolicy.Consent
	dest := []any{&consent.UserID, &consent.Accepted, &consent.Version, &consent.Source, &consent.DeviceID,
		&consent.ClientIP, &consent.DecidedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	return &consent, nil
}

// GetUserAdConsent 用户当前的选择；没选过时返回 (nil, nil)。
func (r *Repository) GetUserAdConsent(ctx context.Context, appID, userID int64) (*adpolicy.Consent, error) {
	consent, err := scanAdConsent(r.pool.QueryRow(ctx, `SELECT `+adConsentColumns+`
FROM user_ad_consents c WHERE c.appid = $1 AND c.user_id = $2`, appID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return consent, err
}

// SaveUserAdConsent 记录一次选择，返回记录后的当前选择与这次是否真的改变了什么。
//
// 与当前选择完全相同（同意与否、条款版本都一样）时什么都不写：App 每次启动都可能补报一次，
// 不该在历史里留下一串重复的「同意」。IfAbsent 时账号上已有选择就原样返回，
// 两台设备同时把各自未登录时的选择同步上来，只有先到的那一个算数。
func (r *Repository) SaveUserAdConsent(ctx context.Context, input adpolicy.SaveConsentInput) (*adpolicy.Consent, bool, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	inserted, err := scanAdConsent(tx.QueryRow(ctx, `INSERT INTO user_ad_consents AS c
(appid, user_id, accepted, version, source, device_id, client_ip, decided_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8,$8)
ON CONFLICT (appid, user_id) DO NOTHING
RETURNING `+adConsentColumns,
		input.AppID, input.UserID, input.Accepted, input.Version, input.Source, input.DeviceID, input.ClientIP, now))
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		inserted = nil
	default:
		return nil, false, err
	}

	result := inserted
	if inserted == nil {
		existing, err := scanAdConsent(tx.QueryRow(ctx, `SELECT `+adConsentColumns+`
FROM user_ad_consents c WHERE c.appid = $1 AND c.user_id = $2 FOR UPDATE`, input.AppID, input.UserID))
		if err != nil {
			return nil, false, err
		}
		if input.IfAbsent || (existing.Accepted == input.Accepted && existing.Version == input.Version) {
			if err := tx.Commit(ctx); err != nil {
				return nil, false, err
			}
			tx = nil
			return existing, false, nil
		}
		result, err = scanAdConsent(tx.QueryRow(ctx, `UPDATE user_ad_consents AS c SET
  accepted = $3, version = $4, source = $5, device_id = $6, client_ip = $7, decided_at = $8, updated_at = $8
WHERE c.appid = $1 AND c.user_id = $2
RETURNING `+adConsentColumns,
			input.AppID, input.UserID, input.Accepted, input.Version, input.Source, input.DeviceID, input.ClientIP, now))
		if err != nil {
			return nil, false, err
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO user_ad_consent_logs
(appid, user_id, accepted, version, source, device_id, client_ip, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		input.AppID, input.UserID, input.Accepted, input.Version, input.Source, input.DeviceID, input.ClientIP, now); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	tx = nil
	return result, true, nil
}

// ListAdConsents 管理端按当前选择列用户。
func (r *Repository) ListAdConsents(ctx context.Context, query adpolicy.ConsentQuery) (*adpolicy.Page[adpolicy.Consent], error) {
	where := []string{"c.appid = $1"}
	args := []any{query.AppID}
	if query.Accepted != nil {
		args = append(args, *query.Accepted)
		where = append(where, fmt.Sprintf("c.accepted = $%d", len(args)))
	}
	if query.Outdated {
		args = append(args, query.CurrentVersion)
		where = append(where, fmt.Sprintf("c.version < $%d", len(args)))
	}
	if source := strings.TrimSpace(query.Source); source != "" {
		args = append(args, source)
		where = append(where, fmt.Sprintf("c.source = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR c.device_id ILIKE $%d)", len(args), len(args)))
	}
	clause := "WHERE " + strings.Join(where, " AND ")
	from := ` FROM user_ad_consents c LEFT JOIN users u ON u.id = c.user_id `

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+adConsentColumns+`, COALESCE(u.account, '')`+from+clause+
		fmt.Sprintf(" ORDER BY c.decided_at DESC, c.user_id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]adpolicy.Consent, 0, limit)
	for rows.Next() {
		var account string
		consent, err := scanAdConsent(rows, &account)
		if err != nil {
			return nil, err
		}
		consent.Account = account
		items = append(items, *consent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return adPolicyPage(items, total, page, limit), nil
}

// ListAdConsentLogs 选择的变更历史，新的在前。
func (r *Repository) ListAdConsentLogs(ctx context.Context, query adpolicy.ConsentLogQuery) (*adpolicy.Page[adpolicy.ConsentLog], error) {
	where := []string{"l.appid = $1"}
	args := []any{query.AppID}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("l.user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR l.device_id ILIKE $%d)", len(args), len(args)))
	}
	clause := "WHERE " + strings.Join(where, " AND ")
	from := ` FROM user_ad_consent_logs l LEFT JOIN users u ON u.id = l.user_id `

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT l.id, l.user_id, COALESCE(u.account, ''), l.accepted, l.version,
l.source, l.device_id, l.client_ip, l.created_at`+from+clause+
		fmt.Sprintf(" ORDER BY l.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]adpolicy.ConsentLog, 0, limit)
	for rows.Next() {
		var item adpolicy.ConsentLog
		if err := rows.Scan(&item.ID, &item.UserID, &item.Account, &item.Accepted, &item.Version,
			&item.Source, &item.DeviceID, &item.ClientIP, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return adPolicyPage(items, total, page, limit), nil
}

// ── 开屏广告 ──

// RecordSplashEvents 落一批开屏记录，返回实际新增了几条（重复的 eventId 不算）。
// 每条挂到哪个账号由服务层按上报时的令牌与记录上的 anonymous 定好（SplashEvent.UserID）。
func (r *Repository) RecordSplashEvents(ctx context.Context, appID int64, deviceID, clientIP string,
	events []adpolicy.SplashEvent) (int, error) {
	if len(events) == 0 {
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
	recorded := 0
	for _, event := range events {
		tag, err := tx.Exec(ctx, `INSERT INTO app_splash_ad_events
(appid, user_id, event_id, device_id, placement_id, status, error_code, error_message, load_ms, shown_ms,
 client_ip, occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT (appid, event_id) DO NOTHING`,
			appID, nullableInt64(event.UserID), event.EventID, deviceID, event.PlacementID, event.Status,
			event.ErrorCode, event.ErrorMessage, event.LoadMs, event.ShownMs, clientIP, event.OccurredAt)
		if err != nil {
			return 0, err
		}
		recorded += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	tx = nil
	return recorded, nil
}

// SplashUsage 今天展示了几次、上次是什么时候。登录用户按账号数，未登录按设备数，两样都没有时为零。
//
// 间隔最长一天，所以「上次」只需要往回看一天；与今天零点取更早的那个，一条查询同时回答两个问题。
func (r *Repository) SplashUsage(ctx context.Context, appID, userID int64, deviceID string, dayStart time.Time) (*adpolicy.SplashUsage, error) {
	usage := &adpolicy.SplashUsage{}
	var who string
	var arg any
	switch {
	case userID > 0:
		who, arg = "user_id = $2", userID
	case strings.TrimSpace(deviceID) != "":
		who, arg = "device_id = $2", deviceID
	default:
		return usage, nil
	}
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE occurred_at >= $3)::int, MAX(occurred_at)
FROM app_splash_ad_events
WHERE appid = $1 AND `+who+` AND status IN ('shown', 'clicked')
  AND occurred_at >= LEAST($3::timestamptz, NOW() - INTERVAL '1 day')`,
		appID, arg, dayStart).Scan(&usage.TodayCount, &usage.LastShownAt)
	if err != nil {
		return nil, err
	}
	return usage, nil
}

const splashEventColumns = `e.id, e.appid, COALESCE(e.user_id, 0), COALESCE(u.account, ''), e.event_id, e.device_id,
e.placement_id, e.status, e.error_code, e.error_message, e.load_ms, e.shown_ms, e.client_ip, e.occurred_at, e.created_at`

func scanSplashEvent(row interface{ Scan(dest ...any) error }) (*adpolicy.SplashEvent, error) {
	var event adpolicy.SplashEvent
	err := row.Scan(&event.ID, &event.AppID, &event.UserID, &event.Account, &event.EventID, &event.DeviceID,
		&event.PlacementID, &event.Status, &event.ErrorCode, &event.ErrorMessage, &event.LoadMs, &event.ShownMs,
		&event.ClientIP, &event.OccurredAt, &event.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

// ListSplashEvents 开屏记录分页。
func (r *Repository) ListSplashEvents(ctx context.Context, query adpolicy.SplashQuery) (*adpolicy.Page[adpolicy.SplashEvent], error) {
	where := []string{"e.appid = $1"}
	args := []any{query.AppID}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("e.user_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("e.status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR e.device_id ILIKE $%d OR e.error_code ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if query.Start != nil {
		args = append(args, *query.Start)
		where = append(where, fmt.Sprintf("e.occurred_at >= $%d", len(args)))
	}
	if query.End != nil {
		args = append(args, *query.End)
		where = append(where, fmt.Sprintf("e.occurred_at < $%d", len(args)))
	}
	clause := "WHERE " + strings.Join(where, " AND ")
	from := ` FROM app_splash_ad_events e LEFT JOIN users u ON u.id = e.user_id `

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+splashEventColumns+from+clause+
		fmt.Sprintf(" ORDER BY e.occurred_at DESC, e.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]adpolicy.SplashEvent, 0, limit)
	for rows.Next() {
		event, err := scanSplashEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return adPolicyPage(items, total, page, limit), nil
}

// UserSplashSummary 一个用户的开屏累计情况。
func (r *Repository) UserSplashSummary(ctx context.Context, appID, userID int64, dayStart time.Time) (*adpolicy.SplashSummary, error) {
	var summary adpolicy.SplashSummary
	err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE status IN ('shown', 'clicked')),
  COUNT(*) FILTER (WHERE status = 'clicked'),
  COUNT(*) FILTER (WHERE status IN ('failed', 'timeout')),
  COUNT(*) FILTER (WHERE status IN ('shown', 'clicked') AND occurred_at >= $3),
  MAX(occurred_at) FILTER (WHERE status IN ('shown', 'clicked'))
FROM app_splash_ad_events WHERE appid = $1 AND user_id = $2`, appID, userID, dayStart).Scan(
		&summary.Shown, &summary.Clicked, &summary.Failed, &summary.TodayShown, &summary.LastShownAt)
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

// ── 统计 ──

// AdPolicyStats 当前选择的分布，以及统计窗口内按天的开屏与选择变更。缺失的日期由服务层补零。
func (r *Repository) AdPolicyStats(ctx context.Context, appID int64, currentVersion int, since time.Time, timezone string) (*adpolicy.Stats, error) {
	stats := &adpolicy.Stats{Trend: []adpolicy.StatsDay{}}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE accepted AND version >= $2),
  COUNT(*) FILTER (WHERE NOT accepted AND version >= $2),
  COUNT(*) FILTER (WHERE version < $2)
FROM user_ad_consents WHERE appid = $1`, appID, currentVersion).Scan(
		&stats.Summary.Accepted, &stats.Summary.Declined, &stats.Summary.Outdated); err != nil {
		return nil, err
	}

	byDate := map[string]*adpolicy.StatsDay{}
	day := func(date string) *adpolicy.StatsDay {
		if item, ok := byDate[date]; ok {
			return item
		}
		item := &adpolicy.StatsDay{Date: date}
		byDate[date] = item
		return item
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(occurred_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*) FILTER (WHERE status IN ('shown', 'clicked')),
       COUNT(*) FILTER (WHERE status = 'clicked'),
       COUNT(*) FILTER (WHERE status IN ('failed', 'timeout')),
       COUNT(DISTINCT COALESCE(user_id::text, 'd:' || device_id)) FILTER (WHERE status IN ('shown', 'clicked'))
FROM app_splash_ad_events
WHERE appid = $1 AND occurred_at >= $2
GROUP BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date string
		var shown, clicked, failed, users int64
		if err := rows.Scan(&date, &shown, &clicked, &failed, &users); err != nil {
			rows.Close()
			return nil, err
		}
		item := day(date)
		item.Shown, item.Clicked, item.Failed, item.SplashUsers = shown, clicked, failed, users
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	logRows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*) FILTER (WHERE accepted),
       COUNT(*) FILTER (WHERE NOT accepted)
FROM user_ad_consent_logs
WHERE appid = $1 AND created_at >= $2
GROUP BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for logRows.Next() {
		var date string
		var accepted, declined int64
		if err := logRows.Scan(&date, &accepted, &declined); err != nil {
			logRows.Close()
			return nil, err
		}
		item := day(date)
		item.Accepted, item.Declined = accepted, declined
	}
	logRows.Close()
	if err := logRows.Err(); err != nil {
		return nil, err
	}

	for _, item := range byDate {
		stats.Trend = append(stats.Trend, *item)
	}
	return stats, nil
}

func adPolicyPaging(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return page, limit
}

func adPolicyPage[T any](items []T, total int64, page, limit int) *adpolicy.Page[T] {
	return &adpolicy.Page[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: int((total + int64(limit) - 1) / int64(limit)),
	}
}
