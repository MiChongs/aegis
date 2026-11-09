package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	userdomain "aegis/internal/domain/user"

	"github.com/jackc/pgx/v5"
)

// 账号处置（一键解除风控、关联删除）用到的批量查询。

// riskBanCond 风控平台下的封禁：命令执行时 evidence.source = risk，操作者名以「风控平台」开头（risk_commands.go）
const riskBanCond = `(b.evidence->>'source' = 'risk' OR b.banned_by_admin_name LIKE '风控平台%')`

const riskOpsUserSelect = `
SELECT u.id, u.public_id, u.appid, COALESCE(a.name, ''), u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), u.enabled, u.created_at,
	(SELECT max(l.created_at) FROM login_audit_logs l WHERE l.user_id = u.id AND l.status = 'success'),
	b.id, b.appid, b.user_id, b.ban_type, b.ban_scope, b.status, COALESCE(b.reason, ''), COALESCE(b.evidence, '{}'::jsonb), b.banned_by_admin_id,
	COALESCE(b.banned_by_admin_name, ''), b.start_at, b.end_at, b.created_at
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
LEFT JOIN apps a ON a.id = u.appid
LEFT JOIN LATERAL (
	SELECT * FROM user_account_bans b
	WHERE b.appid = u.appid AND b.user_id = u.id AND b.status = 'active' AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW())
	ORDER BY b.created_at DESC, b.id DESC LIMIT 1
) b ON TRUE`

func scanRiskOpsUser(rows pgx.Rows) (userdomain.RiskOpsUser, error) {
	var (
		u        userdomain.RiskOpsUser
		banID    *int64
		banApp   *int64
		banUser  *int64
		banType  *string
		banScope *string
		status   *string
		reason   *string
		evidence map[string]any
		byID     *int64
		byName   *string
		startAt  *time.Time
		created  *time.Time
		ban      userdomain.AccountBan
	)
	if err := rows.Scan(&u.ID, &u.PublicID, &u.AppID, &u.AppName, &u.Account, &u.Nickname, &u.Avatar, &u.Enabled, &u.CreatedAt, &u.LastLoginAt,
		&banID, &banApp, &banUser, &banType, &banScope, &status, &reason, &evidence, &byID, &byName, &startAt, &ban.EndAt, &created); err != nil {
		return u, err
	}
	if banID != nil {
		ban.ID, ban.AppID, ban.UserID = *banID, deref(banApp), deref(banUser)
		ban.BanType, ban.BanScope, ban.Status, ban.Reason = derefStr(banType), derefStr(banScope), derefStr(status), derefStr(reason)
		ban.Evidence, ban.BannedByAdminID, ban.BannedByAdminName = evidence, byID, derefStr(byName)
		if startAt != nil {
			ban.StartAt = *startAt
		}
		if created != nil {
			ban.CreatedAt = *created
		}
		u.Ban = &ban
		src, _ := evidence["source"].(string)
		u.BanFromRisk = src == "risk" || strings.HasPrefix(ban.BannedByAdminName, "风控平台")
	}
	return u, nil
}

func deref(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func derefStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// RiskOpsUsers 一组账号的基本情况与当前封禁（同一应用；不存在的跳过）
func (r *Repository) RiskOpsUsers(ctx context.Context, appID int64, ids []int64) ([]userdomain.RiskOpsUser, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, riskOpsUserSelect+` WHERE u.appid = $1 AND u.id = ANY($2)`, appID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userdomain.RiskOpsUser
	for rows.Next() {
		u, err := scanRiskOpsUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// RiskBannedUsers 正被风控平台封禁的账号（最近封的在前）
func (r *Repository) RiskBannedUsers(ctx context.Context, q userdomain.RiskBannedQuery) ([]userdomain.RiskOpsUser, int64, error) {
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	page := max(q.Page, 1)
	where := []string{"b.id IS NOT NULL", riskBanCond}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if q.AppID > 0 {
		add("u.appid = ?", q.AppID)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%", kw)
		like, exact := len(args)-1, len(args)
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR p.nickname ILIKE $%d OR u.id::text = $%d OR u.public_id::text = $%d)", like, like, exact, exact))
	}
	cond := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM (`+riskOpsUserSelect+cond+`) x`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, riskOpsUserSelect+cond+fmt.Sprintf(` ORDER BY b.start_at DESC, u.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []userdomain.RiskOpsUser
	for rows.Next() {
		u, err := scanRiskOpsUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, u)
	}
	return out, total, rows.Err()
}

// ActiveIPBansFor 一组 IP 上生效中的防火墙封禁
func (r *Repository) ActiveIPBansFor(ctx context.Context, ips []string) ([]firewalldomain.IPBan, error) {
	if len(ips) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, ip, reason, source, mode, trigger_rule, severity, duration, expires_at,
			status, revoked_by, revoked_at, country, country_code, region, city, isp, trigger_count, created_at, updated_at
		FROM ip_bans WHERE ip = ANY($1) AND status = 'active' AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY created_at DESC`, ips)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []firewalldomain.IPBan
	for rows.Next() {
		b, err := scanIPBanFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}
