package postgres

import (
	"context"
	"strings"

	"aegis/internal/authz"

	"github.com/jackc/pgx/v5"
)

// authz_policies 的读写。实现 authz.Store —— 授权引擎的持久化出口。

var _ authz.Store = (*Repository)(nil)

// ListAuthzPolicies 装载全部策略行。
//
// 排序是刻意的：Casbin 的策略在内存里是有序切片，EnforceEx 返回的"命中哪一条"
// 依赖这个顺序。装载顺序不稳定时，同一次拒绝在两台实例上会给出不同的判据，
// 而排查授权问题时唯一能抓的就是那条判据。
func (r *Repository) ListAuthzPolicies(ctx context.Context) ([]authz.PolicyRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ptype, v0, v1, v2, v3, v4, v5, source, owner, note
		   FROM authz_policies
		  ORDER BY ptype, source, v0, v1, v2, v3, v4, v5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]authz.PolicyRule, 0, 256)
	for rows.Next() {
		var (
			ptype                  string
			v0, v1, v2, v3, v4, v5 string
			source, owner, note    string
		)
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5, &source, &owner, &note); err != nil {
			return nil, err
		}
		items = append(items, authz.PolicyRule{
			PType:  ptype,
			Values: trimTrailingEmpty([]string{v0, v1, v2, v3, v4, v5}),
			Source: source,
			Owner:  owner,
			Note:   note,
		})
	}
	return items, rows.Err()
}

// ReplaceAuthzPolicyGroup 整组替换 (source, owner) 下的全部策略。
//
// 删后插放在同一事务里：分两次提交会有一个"这个角色什么权限都没有"的窗口，
// 而那个窗口里的请求会被拒 —— 编辑一次角色导致线上短暂 403 是不能接受的。
func (r *Repository) ReplaceAuthzPolicyGroup(ctx context.Context, source, owner string, rules []authz.PolicyRule, updatedBy *int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM authz_policies WHERE source = $1 AND owner = $2`, source, owner); err != nil {
		return err
	}
	for _, rule := range rules {
		values := padTo6(rule.Values)
		if _, err := tx.Exec(ctx,
			`INSERT INTO authz_policies (ptype, v0, v1, v2, v3, v4, v5, source, owner, note, updated_by, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, NOW(), NOW())
			 ON CONFLICT (ptype, source, v0, v1, v2, v3, v4, v5)
			 DO UPDATE SET owner = EXCLUDED.owner, note = EXCLUDED.note,
			               updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			rule.PType, values[0], values[1], values[2], values[3], values[4], values[5],
			source, owner, rule.Note, nullableInt64Value(updatedBy)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteAuthzPolicyGroup 删除 (source, owner) 下的全部策略。
func (r *Repository) DeleteAuthzPolicyGroup(ctx context.Context, source, owner string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM authz_policies WHERE source = $1 AND owner = $2`, source, owner)
	return err
}

// ListAuthzPoliciesBySubject 按主体查策略，供管理端展示「这个角色/这个人有哪些策略」。
func (r *Repository) ListAuthzPoliciesBySubject(ctx context.Context, subject string) ([]authz.PolicyRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ptype, v0, v1, v2, v3, v4, v5, source, owner, note
		   FROM authz_policies WHERE v0 = $1 ORDER BY ptype, source, v1, v2`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]authz.PolicyRule, 0, 32)
	for rows.Next() {
		var (
			ptype                  string
			v0, v1, v2, v3, v4, v5 string
			source, owner, note    string
		)
		if err := rows.Scan(&ptype, &v0, &v1, &v2, &v3, &v4, &v5, &source, &owner, &note); err != nil {
			return nil, err
		}
		items = append(items, authz.PolicyRule{
			PType: ptype, Values: trimTrailingEmpty([]string{v0, v1, v2, v3, v4, v5}),
			Source: source, Owner: owner, Note: note,
		})
	}
	return items, rows.Err()
}

// trimTrailingEmpty 去掉尾部空列。
//
// 必须去：Casbin 按**列数**匹配策略段，`p` 是四列，硬塞六列进去的行
// 不会报错，只会永远匹配不上 —— 表现为"策略明明在表里却不生效"。
func trimTrailingEmpty(values []string) []string {
	end := len(values)
	for end > 0 && strings.TrimSpace(values[end-1]) == "" {
		end--
	}
	return values[:end]
}

func padTo6(values []string) [6]string {
	var padded [6]string
	for i := 0; i < len(values) && i < 6; i++ {
		padded[i] = values[i]
	}
	return padded
}
