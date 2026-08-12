package postgres

import (
	"context"
	"strings"

	vipdomain "aegis/internal/domain/vip"
)

// 会员功能标识目录（应用级）。
//
// 与套餐是引用关系而不是外键：套餐存的是 tag 数组，功能被删掉时套餐里那一项
// 变成一个悬空引用。这是刻意的 —— 删功能是运营动作，不该被"有套餐在用"卡住，
// 而悬空引用在校验入口会明确报「未登记的功能标识」，比 ON DELETE 静默改写
// 一批套餐配置要好排查得多。

const vipFeatureColumns = `id, appid, tag, name, COALESCE(description, ''), is_active, sort_order, created_at, updated_at`

func (r *Repository) ListVipFeatures(ctx context.Context, appID int64, activeOnly bool) ([]vipdomain.Feature, error) {
	query := `SELECT ` + vipFeatureColumns + ` FROM vip_features WHERE appid = $1`
	if activeOnly {
		query += ` AND is_active = TRUE`
	}
	query += ` ORDER BY sort_order ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]vipdomain.Feature, 0, 8)
	for rows.Next() {
		item, err := scanVipFeature(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetVipFeature(ctx context.Context, appID int64, tag string) (*vipdomain.Feature, error) {
	return scanVipFeature(r.pool.QueryRow(ctx, `SELECT `+vipFeatureColumns+
		` FROM vip_features WHERE appid = $1 AND tag = $2 LIMIT 1`, appID, strings.ToLower(strings.TrimSpace(tag))))
}

// UpsertVipFeature 按 (appid, tag) 落库。tag 是定位键，不可改名 ——
// 它已经写进接入方的代码与每一条历史开通记录的功能快照里。
func (r *Repository) UpsertVipFeature(ctx context.Context, mutation vipdomain.FeatureMutation) (*vipdomain.Feature, error) {
	tag := strings.ToLower(strings.TrimSpace(mutation.Tag))
	current := &vipdomain.Feature{AppID: mutation.AppID, Tag: tag, IsActive: true}
	existing, err := r.GetVipFeature(ctx, mutation.AppID, tag)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		current = existing
	}
	if mutation.Name != nil {
		current.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Description != nil {
		current.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.IsActive != nil {
		current.IsActive = *mutation.IsActive
	}
	if mutation.SortOrder != nil {
		current.SortOrder = *mutation.SortOrder
	}
	return scanVipFeature(r.pool.QueryRow(ctx,
		`INSERT INTO vip_features (appid, tag, name, description, is_active, sort_order, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
ON CONFLICT (appid, tag) DO UPDATE SET
	name = EXCLUDED.name,
	description = EXCLUDED.description,
	is_active = EXCLUDED.is_active,
	sort_order = EXCLUDED.sort_order,
	updated_at = NOW()
RETURNING `+vipFeatureColumns,
		current.AppID, current.Tag, current.Name, nullableString(current.Description),
		current.IsActive, current.SortOrder))
}

func (r *Repository) DeleteVipFeature(ctx context.Context, appID int64, tag string) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM vip_features WHERE appid = $1 AND tag = $2`,
		appID, strings.ToLower(strings.TrimSpace(tag)))
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// CountVipPlansUsingFeature 有多少个套餐还挂着这个功能标识。
//
// 删除前用它给出提示：删掉之后那些套餐的这一项会变成悬空引用，
// 新开通的用户就不再拿到这个权益了 —— 这件事必须在删之前说出来。
func (r *Repository) CountVipPlansUsingFeature(ctx context.Context, appID int64, tag string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM vip_plans WHERE appid = $1 AND $2 = ANY(features)`,
		appID, strings.ToLower(strings.TrimSpace(tag))).Scan(&count)
	return count, err
}

func scanVipFeature(row interface{ Scan(dest ...any) error }) (*vipdomain.Feature, error) {
	var item vipdomain.Feature
	if err := row.Scan(&item.ID, &item.AppID, &item.Tag, &item.Name, &item.Description,
		&item.IsActive, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}
