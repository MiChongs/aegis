package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"

	"github.com/jackc/pgx/v5"
)

// ListGeoBans 返回全部地域封禁规则（enabled 过滤可选）。
func (r *Repository) ListGeoBans(ctx context.Context, onlyEnabled bool) ([]firewalldomain.GeoBan, error) {
	sql := `SELECT id, scope_type, scope_value, mode, reason, enabled,
	               created_by, created_at, updated_at, expires_at,
	               match_count, last_match_at
	          FROM geo_bans`
	if onlyEnabled {
		sql += ` WHERE enabled = TRUE`
	}
	sql += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []firewalldomain.GeoBan
	for rows.Next() {
		var b firewalldomain.GeoBan
		if err := rows.Scan(
			&b.ID, &b.ScopeType, &b.ScopeValue, &b.Mode, &b.Reason, &b.Enabled,
			&b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ExpiresAt,
			&b.MatchCount, &b.LastMatchAt,
		); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpsertGeoBan 按 (scope_type, scope_value) 唯一键插入或更新规则。
func (r *Repository) UpsertGeoBan(ctx context.Context, m firewalldomain.GeoBanMutation, createdBy *int64) (firewalldomain.GeoBan, error) {
	scope := strings.ToLower(strings.TrimSpace(m.ScopeType))
	value := strings.TrimSpace(m.ScopeValue)
	if scope == "" || value == "" {
		return firewalldomain.GeoBan{}, fmt.Errorf("scopeType 与 scopeValue 必填")
	}

	// 国家统一大写
	if scope == firewalldomain.GeoScopeCountry {
		value = strings.ToUpper(value)
	}

	var b firewalldomain.GeoBan
	err := r.pool.QueryRow(ctx, `
		INSERT INTO geo_bans (scope_type, scope_value, mode, reason, enabled, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (scope_type, scope_value) DO UPDATE
		SET mode       = EXCLUDED.mode,
		    reason     = EXCLUDED.reason,
		    enabled    = EXCLUDED.enabled,
		    expires_at = EXCLUDED.expires_at,
		    updated_at = NOW()
		RETURNING id, scope_type, scope_value, mode, reason, enabled,
		          created_by, created_at, updated_at, expires_at,
		          match_count, last_match_at`,
		scope, value, m.Mode, m.Reason, m.Enabled, createdBy, m.ExpiresAt,
	).Scan(
		&b.ID, &b.ScopeType, &b.ScopeValue, &b.Mode, &b.Reason, &b.Enabled,
		&b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ExpiresAt,
		&b.MatchCount, &b.LastMatchAt,
	)
	if err != nil {
		return firewalldomain.GeoBan{}, err
	}
	return b, nil
}

// UpdateGeoBanStatus 快速切换启用状态。
func (r *Repository) UpdateGeoBanStatus(ctx context.Context, id int64, enabled bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE geo_bans SET enabled = $1, updated_at = NOW() WHERE id = $2`, enabled, id)
	return err
}

// DeleteGeoBan 删除规则。
func (r *Repository) DeleteGeoBan(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM geo_bans WHERE id = $1`, id)
	return err
}

// IncrementGeoBanMatch 累计命中次数（异步调用，忽略错误）。
func (r *Repository) IncrementGeoBanMatch(ctx context.Context, id int64, ts time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE geo_bans
		SET match_count = match_count + 1,
		    last_match_at = $2
		WHERE id = $1`, id, ts)
	return err
}

// GetGeoBan 按 ID 读取。
func (r *Repository) GetGeoBan(ctx context.Context, id int64) (*firewalldomain.GeoBan, error) {
	var b firewalldomain.GeoBan
	err := r.pool.QueryRow(ctx, `
		SELECT id, scope_type, scope_value, mode, reason, enabled,
		       created_by, created_at, updated_at, expires_at,
		       match_count, last_match_at
		  FROM geo_bans WHERE id = $1`, id,
	).Scan(
		&b.ID, &b.ScopeType, &b.ScopeValue, &b.Mode, &b.Reason, &b.Enabled,
		&b.CreatedBy, &b.CreatedAt, &b.UpdatedAt, &b.ExpiresAt,
		&b.MatchCount, &b.LastMatchAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return &b, err
}
