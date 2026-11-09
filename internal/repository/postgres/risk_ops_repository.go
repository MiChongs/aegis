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

// RiskDeviceClusters 登录过至少 minUsers 个账号的设备（占位设备码、UA 摘要键不算，与 events.NormalizeDeviceID 一致）
func (r *Repository) RiskDeviceClusters(ctx context.Context, appID int64, minUsers, limit int) ([]userdomain.RiskDeviceCluster, error) {
	if minUsers < 2 {
		minUsers = 2
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args := []any{minUsers, limit}
	where := `d.device_key !~ '^[0\-:_ .]*$' AND d.device_key NOT ILIKE 'ua:%'
		AND lower(d.device_key) NOT IN ('9774d56d682e549c', 'unknown', 'null', 'nil', 'none', 'undefined')`
	if appID > 0 {
		args = append(args, appID)
		where += " AND d.appid = $3"
	}
	rows, err := r.pool.Query(ctx, `
		SELECT d.appid, COALESCE(max(a.name), ''), d.device_key,
			COALESCE((array_agg(NULLIF(d.platform, '') ORDER BY d.last_seen_at DESC) FILTER (WHERE d.platform <> ''))[1], ''),
			COALESCE((array_agg(d.model ORDER BY d.last_seen_at DESC) FILTER (WHERE d.model <> ''))[1], ''),
			COALESCE((array_agg(d.name ORDER BY d.last_seen_at DESC) FILTER (WHERE d.name <> ''))[1], ''),
			count(*)::int, max(d.last_seen_at),
			(array_agg(d.user_id ORDER BY d.last_seen_at DESC))[1:200]
		FROM user_login_devices d
		LEFT JOIN apps a ON a.id = d.appid
		WHERE `+where+`
		GROUP BY d.appid, d.device_key
		HAVING count(*) >= $1
		ORDER BY count(*) DESC, max(d.last_seen_at) DESC
		LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userdomain.RiskDeviceCluster
	for rows.Next() {
		var c userdomain.RiskDeviceCluster
		if err := rows.Scan(&c.AppID, &c.AppName, &c.DeviceKey, &c.Platform, &c.Model, &c.Name, &c.Users, &c.LastSeen, &c.UserIDs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RegisterIPLedgerRef 一条注册台账（账号删除、解除风控时用它找到要重算的地址、设备与属地）
type RegisterIPLedgerRef struct {
	ID       int64
	UserID   int64
	IP       string
	Device   string
	Country  string
	Province string
	City     string
	Released bool
}

// RegisterIPLedgerOfUsers 这些账号的注册台账
func (r *Repository) RegisterIPLedgerOfUsers(ctx context.Context, appID int64, userIDs []int64) ([]RegisterIPLedgerRef, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, host(ip), device_id, country, province, city, released_at IS NOT NULL
		FROM register_ip_ledger WHERE appid = $1 AND user_id = ANY($2)`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RegisterIPLedgerRef
	for rows.Next() {
		var x RegisterIPLedgerRef
		if err := rows.Scan(&x.ID, &x.UserID, &x.IP, &x.Device, &x.Country, &x.Province, &x.City, &x.Released); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ReleaseRegisterIPLedgerIDs 把这几条台账标为已解除：不再计入地址、设备、属地的累计注册数
func (r *Repository) ReleaseRegisterIPLedgerIDs(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `UPDATE register_ip_ledger SET released_at = NOW() WHERE id = ANY($1) AND released_at IS NULL`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
