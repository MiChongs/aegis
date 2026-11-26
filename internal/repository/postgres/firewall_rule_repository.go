package postgres

import (
	"context"
	"errors"
	"time"

	firewalldomain "aegis/internal/domain/firewall"

	"github.com/jackc/pgx/v5"
)

// ──────────────────────────────────────
// 自定义规则（firewall_rules）
// ──────────────────────────────────────

const firewallRuleColumns = `id, name, description, enabled, priority, expression, action, block_mode, ban_seconds,
	hits, last_hit_at, created_by, updated_by, created_at, updated_at`

func scanFirewallRule(row pgx.Row) (*firewalldomain.Rule, error) {
	var r firewalldomain.Rule
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Enabled, &r.Priority, &r.Expression, &r.Action, &r.BlockMode, &r.BanSeconds,
		&r.Hits, &r.LastHitAt, &r.CreatedBy, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListFirewallRules 全部自定义规则（按求值顺序）。
func (r *Repository) ListFirewallRules(ctx context.Context) ([]firewalldomain.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+firewallRuleColumns+` FROM firewall_rules ORDER BY priority, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]firewalldomain.Rule, 0)
	for rows.Next() {
		item, err := scanFirewallRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetFirewallRule 按 ID 取一条；不存在返回 nil, nil。
func (r *Repository) GetFirewallRule(ctx context.Context, id int64) (*firewalldomain.Rule, error) {
	item, err := scanFirewallRule(r.pool.QueryRow(ctx, `SELECT `+firewallRuleColumns+` FROM firewall_rules WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// InsertFirewallRule 新建。
func (r *Repository) InsertFirewallRule(ctx context.Context, rule firewalldomain.Rule) (*firewalldomain.Rule, error) {
	return scanFirewallRule(r.pool.QueryRow(ctx, `
		INSERT INTO firewall_rules (name, description, enabled, priority, expression, action, block_mode, ban_seconds, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
		RETURNING `+firewallRuleColumns,
		rule.Name, rule.Description, rule.Enabled, rule.Priority, rule.Expression, rule.Action, rule.BlockMode, rule.BanSeconds, rule.CreatedBy))
}

// UpdateFirewallRule 整条更新（命中计数不动）。不存在返回 nil, nil。
func (r *Repository) UpdateFirewallRule(ctx context.Context, rule firewalldomain.Rule) (*firewalldomain.Rule, error) {
	item, err := scanFirewallRule(r.pool.QueryRow(ctx, `
		UPDATE firewall_rules SET name=$2, description=$3, enabled=$4, priority=$5, expression=$6, action=$7,
			block_mode=$8, ban_seconds=$9, updated_by=$10, updated_at=NOW()
		WHERE id=$1
		RETURNING `+firewallRuleColumns,
		rule.ID, rule.Name, rule.Description, rule.Enabled, rule.Priority, rule.Expression, rule.Action, rule.BlockMode, rule.BanSeconds, rule.UpdatedBy))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// SetFirewallRuleEnabled 启停。返回是否存在。
func (r *Repository) SetFirewallRuleEnabled(ctx context.Context, id int64, enabled bool, adminID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE firewall_rules SET enabled=$2, updated_by=$3, updated_at=NOW() WHERE id=$1`, id, enabled, adminID)
	return tag.RowsAffected() > 0, err
}

// DeleteFirewallRule 删除。返回是否存在。
func (r *Repository) DeleteFirewallRule(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM firewall_rules WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}

// ReorderFirewallRules 按给定顺序重排优先级（10、20、30…，留出插队空间）。
func (r *Repository) ReorderFirewallRules(ctx context.Context, ids []int64, adminID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE firewall_rules SET priority=$2, updated_by=$3, updated_at=NOW() WHERE id=$1`, id, (i+1)*10, adminID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// AddFirewallRuleHits 累加各实例攒下的命中数（每个副本只报自己的增量，不会互相覆盖）。
func (r *Repository) AddFirewallRuleHits(ctx context.Context, hits map[int64]int64, at time.Time) error {
	if len(hits) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for id, n := range hits {
		if n <= 0 {
			continue
		}
		batch.Queue(`UPDATE firewall_rules SET hits = hits + $2, last_hit_at = GREATEST(COALESCE(last_hit_at, $3), $3) WHERE id=$1`, id, n, at)
	}
	return r.pool.SendBatch(ctx, batch).Close()
}

// ──────────────────────────────────────
// WAF 例外（firewall_waf_exclusions）
// ──────────────────────────────────────

const wafExclusionColumns = `id, name, note, enabled, position, path_match, path, methods, rule_ids, tags, targets,
	created_by, updated_by, created_at, updated_at`

func scanWAFExclusion(row pgx.Row) (*firewalldomain.WAFExclusion, error) {
	var e firewalldomain.WAFExclusion
	var ruleIDs []int32
	if err := row.Scan(&e.ID, &e.Name, &e.Note, &e.Enabled, &e.Position, &e.PathMatch, &e.Path, &e.Methods, &ruleIDs, &e.Tags, &e.Targets,
		&e.CreatedBy, &e.UpdatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.RuleIDs = make([]int, len(ruleIDs))
	for i, id := range ruleIDs {
		e.RuleIDs[i] = int(id)
	}
	if e.Methods == nil {
		e.Methods = []string{}
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}
	if e.Targets == nil {
		e.Targets = []string{}
	}
	return &e, nil
}

func int32s(values []int) []int32 {
	out := make([]int32, len(values))
	for i, v := range values {
		out[i] = int32(v)
	}
	return out
}

// ListWAFExclusions 全部例外（按生成顺序）。
func (r *Repository) ListWAFExclusions(ctx context.Context) ([]firewalldomain.WAFExclusion, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+wafExclusionColumns+` FROM firewall_waf_exclusions ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]firewalldomain.WAFExclusion, 0)
	for rows.Next() {
		item, err := scanWAFExclusion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetWAFExclusion 不存在返回 nil, nil。
func (r *Repository) GetWAFExclusion(ctx context.Context, id int64) (*firewalldomain.WAFExclusion, error) {
	item, err := scanWAFExclusion(r.pool.QueryRow(ctx, `SELECT `+wafExclusionColumns+` FROM firewall_waf_exclusions WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// InsertWAFExclusion 新建，排在最后。
func (r *Repository) InsertWAFExclusion(ctx context.Context, e firewalldomain.WAFExclusion) (*firewalldomain.WAFExclusion, error) {
	return scanWAFExclusion(r.pool.QueryRow(ctx, `
		INSERT INTO firewall_waf_exclusions (name, note, enabled, position, path_match, path, methods, rule_ids, tags, targets, created_by, updated_by)
		VALUES ($1,$2,$3,(SELECT COALESCE(MAX(position), 0) + 10 FROM firewall_waf_exclusions),$4,$5,$6,$7,$8,$9,$10,$10)
		RETURNING `+wafExclusionColumns,
		e.Name, e.Note, e.Enabled, e.PathMatch, e.Path, nonNilStrings(e.Methods), int32s(e.RuleIDs), nonNilStrings(e.Tags), nonNilStrings(e.Targets), e.CreatedBy))
}

// UpdateWAFExclusion 不存在返回 nil, nil。
func (r *Repository) UpdateWAFExclusion(ctx context.Context, e firewalldomain.WAFExclusion) (*firewalldomain.WAFExclusion, error) {
	item, err := scanWAFExclusion(r.pool.QueryRow(ctx, `
		UPDATE firewall_waf_exclusions SET name=$2, note=$3, enabled=$4, path_match=$5, path=$6, methods=$7, rule_ids=$8,
			tags=$9, targets=$10, updated_by=$11, updated_at=NOW()
		WHERE id=$1
		RETURNING `+wafExclusionColumns,
		e.ID, e.Name, e.Note, e.Enabled, e.PathMatch, e.Path, nonNilStrings(e.Methods), int32s(e.RuleIDs), nonNilStrings(e.Tags), nonNilStrings(e.Targets), e.UpdatedBy))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// SetWAFExclusionEnabled 启停。
func (r *Repository) SetWAFExclusionEnabled(ctx context.Context, id int64, enabled bool, adminID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE firewall_waf_exclusions SET enabled=$2, updated_by=$3, updated_at=NOW() WHERE id=$1`, id, enabled, adminID)
	return tag.RowsAffected() > 0, err
}

// DeleteWAFExclusion 删除。
func (r *Repository) DeleteWAFExclusion(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM firewall_waf_exclusions WHERE id=$1`, id)
	return tag.RowsAffected() > 0, err
}
