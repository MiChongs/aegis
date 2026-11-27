package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	vipdomain "aegis/internal/domain/vip"
	"github.com/jackc/pgx/v5"
)

// 会员档位（规则见 internal/domain/vip/tier.go，文档 docs/vip-tiers.md）。
//
// 这里管四件事：档位的增删改查、套餐 features 镜像的同步、删档位时把套餐与会员迁走、
// 发放时解析这一笔落在哪个档位（resolveGrantTierTx，所有发放入口都经 extendUserVipTx 走到它）。

// ErrVipTierNotFound 档位不存在（或不属于这个应用）
var ErrVipTierNotFound = errors.New("postgres: vip tier not found")

// VipTierInUseError 删档位时档位下还有套餐或会员，又没有指定迁往的档位。
type VipTierInUseError struct {
	Plans   int64
	Members int64
}

func (e *VipTierInUseError) Error() string {
	return fmt.Sprintf("postgres: vip tier in use (plans=%d, members=%d)", e.Plans, e.Members)
}

const vipTierColumns = `id, appid, name, COALESCE(description, ''), features, perks, sort_order, created_at, updated_at`

// vipDefaultLifetimeTierFor 应用的永久档位：不挂套餐的永久开通（自定义发放、老系统迁入、抽奖 / 卡密的永久会员）归入它。
//
// 取在售的永久套餐所属档位（排序靠前的、同序取最新的）；永久套餐都删了时，取最近一笔仍生效、
// 有档位的永久开通所在的档位 —— 套餐删光了，永久会员的档位还在，新发的永久会员应当与他们一样。
// 都没有时为 NULL：这笔开通只带发放时给的功能。appid 是 SQL 表达式（参数或外层的列）。
func vipDefaultLifetimeTierFor(appid string) string {
	return `COALESCE(
    (SELECT dp.tier_id FROM vip_plans dp
     WHERE dp.appid = ` + appid + ` AND dp.lifetime AND dp.tier_id IS NOT NULL
     ORDER BY dp.is_active DESC, dp.sort_order ASC, dp.id DESC LIMIT 1),
    (SELECT vt.tier_id FROM vip_transactions vt
     WHERE vt.appid = ` + appid + ` AND vt.lifetime AND vt.tier_id IS NOT NULL AND ` + vipSegmentScopeSQL + `
     ORDER BY vt.id DESC LIMIT 1))`
}

var vipDefaultLifetimeTierSQL = vipDefaultLifetimeTierFor("$1")

// ListVipTiers 应用的全部档位，带档位下的套餐、在期会员数与是否默认永久档位。
func (r *Repository) ListVipTiers(ctx context.Context, appID int64) ([]vipdomain.Tier, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+vipTierColumns+`,
    COALESCE((SELECT jsonb_agg(jsonb_build_object('id', p.id, 'name', p.name, 'kind', p.kind,
                  'lifetime', p.lifetime, 'isActive', p.is_active) ORDER BY p.sort_order, p.id)
              FROM vip_plans p WHERE p.tier_id = t.id), '[]'::jsonb),
    (SELECT COUNT(DISTINCT vt.user_id) FROM vip_transactions vt
     WHERE vt.tier_id = t.id AND `+vipSegmentScopeSQL+` AND `+vipSegmentUntilSQL+` > NOW()),
    (SELECT COUNT(DISTINCT vt.user_id) FROM vip_transactions vt
     WHERE vt.tier_id = t.id AND vt.lifetime AND `+vipSegmentScopeSQL+`),
    t.id = `+vipDefaultLifetimeTierFor("t.appid")+`
FROM vip_tiers t
WHERE t.appid = $1
ORDER BY t.sort_order ASC, t.id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]vipdomain.Tier, 0, 4)
	for rows.Next() {
		var (
			item     vipdomain.Tier
			plansRaw []byte
			isDef    *bool
		)
		var perksRaw []byte
		if err := rows.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Features, &perksRaw, &item.SortOrder,
			&item.CreatedAt, &item.UpdatedAt, &plansRaw, &item.Members, &item.LifetimeMembers, &isDef); err != nil {
			return nil, err
		}
		item.Perks = decodeVipPerks(perksRaw)
		item.Plans = []vipdomain.TierPlanRef{}
		if err := json.Unmarshal(plansRaw, &item.Plans); err != nil {
			return nil, fmt.Errorf("decode vip tier plans: %w", err)
		}
		item.DefaultLifetime = isDef != nil && *isDef
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetVipTier 取一个档位（不带列表统计）。
func (r *Repository) GetVipTier(ctx context.Context, appID int64, tierID int64) (*vipdomain.Tier, error) {
	return getVipTierQ(ctx, r.pool, appID, tierID)
}

func getVipTierQ(ctx context.Context, q queryExecutor, appID int64, tierID int64) (*vipdomain.Tier, error) {
	return scanVipTier(q.QueryRow(ctx, `SELECT `+vipTierColumns+` FROM vip_tiers WHERE appid = $1 AND id = $2`, appID, tierID))
}

// UpsertVipTier 新建或更新档位。改功能即对档位下所有在期会员生效，套餐的 features 镜像一并同步。
func (r *Repository) UpsertVipTier(ctx context.Context, mutation vipdomain.TierMutation) (*vipdomain.Tier, error) {
	var out *vipdomain.Tier
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		tier, err := upsertVipTierTx(ctx, tx, mutation)
		out = tier
		return err
	})
	return out, err
}

func upsertVipTierTx(ctx context.Context, tx pgx.Tx, mutation vipdomain.TierMutation) (*vipdomain.Tier, error) {
	current := &vipdomain.Tier{AppID: mutation.AppID, Features: []string{}, Perks: []vipdomain.Perk{}}
	if mutation.ID > 0 {
		existing, err := getVipTierQ(ctx, tx, mutation.AppID, mutation.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, ErrVipTierNotFound
		}
		current = existing
	}
	if mutation.Name != nil {
		current.Name = truncateColumn(strings.TrimSpace(*mutation.Name), vipPlanNameMaxRunes)
	}
	if mutation.Description != nil {
		current.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.Features != nil {
		current.Features = vipdomain.NormalizeFeatureTags(*mutation.Features)
	}
	if mutation.SortOrder != nil {
		current.SortOrder = *mutation.SortOrder
	}
	if mutation.Perks != nil {
		current.Perks = *mutation.Perks
	}
	perksRaw, err := json.Marshal(current.Perks)
	if err != nil {
		return nil, err
	}
	var tier *vipdomain.Tier
	if current.ID == 0 {
		tier, err = scanVipTier(tx.QueryRow(ctx, `INSERT INTO vip_tiers (appid, name, description, features, perks, sort_order)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+vipTierColumns,
			current.AppID, current.Name, nullableString(current.Description), current.Features, perksRaw, current.SortOrder))
	} else {
		tier, err = scanVipTier(tx.QueryRow(ctx, `UPDATE vip_tiers
SET name = $3, description = $4, features = $5, perks = $6, sort_order = $7, updated_at = NOW()
WHERE appid = $1 AND id = $2 RETURNING `+vipTierColumns,
			current.AppID, current.ID, current.Name, nullableString(current.Description), current.Features, perksRaw, current.SortOrder))
	}
	if err != nil {
		return nil, err
	}
	if err := syncTierPlanFeaturesTx(ctx, tx, tier.ID); err != nil {
		return nil, err
	}
	return tier, nil
}

// syncTierPlanFeaturesTx 把档位的功能写回档位下各套餐的 features 镜像。
//
// 读套餐时功能已经取自档位（vipPlanColumns），镜像留给两类读者：滚动发布期间还没换新的实例，
// 与「有几个套餐挂着这个功能」这类按列筛的统计（CountVipPlansUsingFeature）。
func syncTierPlanFeaturesTx(ctx context.Context, q queryExecutor, tierID int64) error {
	_, err := q.Exec(ctx, `UPDATE vip_plans p SET features = t.features
FROM vip_tiers t WHERE t.id = $1 AND p.tier_id = t.id AND p.features IS DISTINCT FROM t.features`, tierID)
	return err
}

// DeleteVipTier 删除档位。
//
// 档位下还有套餐或在期会员时必须给 moveTo：套餐改卖目标档位，**全部**开通记录（含已结束的）
// 改挂目标档位，之后这些会员的权益按目标档位算。没有 moveTo 时返回 *VipTierInUseError，什么都不动。
// 不存在返回 ErrVipTierNotFound；moveTo 不存在或等于自己同样是 ErrVipTierNotFound。
func (r *Repository) DeleteVipTier(ctx context.Context, appID int64, tierID int64, moveTo int64) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var plans, members int64
		err := tx.QueryRow(ctx, `SELECT
    (SELECT COUNT(*) FROM vip_plans p WHERE p.tier_id = t.id),
    (SELECT COUNT(DISTINCT vt.user_id) FROM vip_transactions vt
     WHERE vt.tier_id = t.id AND `+vipSegmentScopeSQL+` AND `+vipSegmentUntilSQL+` > NOW())
FROM vip_tiers t WHERE t.appid = $1 AND t.id = $2 FOR UPDATE OF t`, appID, tierID).Scan(&plans, &members)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVipTierNotFound
		}
		if err != nil {
			return err
		}
		if moveTo > 0 {
			if moveTo == tierID {
				return ErrVipTierNotFound
			}
			target, err := getVipTierQ(ctx, tx, appID, moveTo)
			if err != nil {
				return err
			}
			if target == nil {
				return ErrVipTierNotFound
			}
			if _, err := tx.Exec(ctx, `UPDATE vip_plans SET tier_id = $2, updated_at = NOW() WHERE tier_id = $1`,
				tierID, moveTo); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE vip_transactions SET tier_id = $2 WHERE tier_id = $1`,
				tierID, moveTo); err != nil {
				return err
			}
			if err := syncTierPlanFeaturesTx(ctx, tx, moveTo); err != nil {
				return err
			}
		} else if plans > 0 || members > 0 {
			return &VipTierInUseError{Plans: plans, Members: members}
		}
		// 已结束的开通记录随外键置空（ON DELETE SET NULL），它们的 features 快照仍是当时的留档
		_, err = tx.Exec(ctx, `DELETE FROM vip_tiers WHERE appid = $1 AND id = $2`, appID, tierID)
		return err
	})
}

// resolvePlanTierTx 套餐保存时定下它卖的档位，返回档位 ID。
//
//   - 指定了档位：必须属于这个应用；
//   - 没指定、套餐原本就有档位：沿用；
//   - 新套餐没指定：功能完全相同的档位就并进去（删掉再上架一个同样的套餐，自然回到原档位），
//     否则按套餐名新建一个。
//
// features 非空时改的是档位的功能（同档位的套餐一起变）。
func resolvePlanTierTx(ctx context.Context, tx pgx.Tx, appID int64, plan *vipdomain.Plan, tierID *int64, features *[]string) (int64, error) {
	target := plan.TierID
	if tierID != nil && *tierID > 0 {
		tier, err := getVipTierQ(ctx, tx, appID, *tierID)
		if err != nil {
			return 0, err
		}
		if tier == nil {
			return 0, ErrVipTierNotFound
		}
		target = tier.ID
	}
	if target == 0 {
		wanted := plan.Features
		if features != nil {
			wanted = *features
		}
		wanted = vipdomain.NormalizeFeatureTags(wanted)
		err := tx.QueryRow(ctx, `SELECT t.id FROM vip_tiers t
WHERE t.appid = $1 AND ARRAY(SELECT DISTINCT f FROM unnest(t.features) f ORDER BY f) = $2::text[]
ORDER BY t.sort_order, t.id LIMIT 1`, appID, wanted).Scan(&target)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
		if target == 0 {
			name := plan.Name
			tier, err := upsertVipTierTx(ctx, tx, vipdomain.TierMutation{AppID: appID, Name: &name, Features: &wanted,
				SortOrder: &plan.SortOrder})
			if err != nil {
				return 0, err
			}
			return tier.ID, nil
		}
	}
	if features != nil {
		var tier vipdomain.Tier
		if err := tx.QueryRow(ctx, `UPDATE vip_tiers SET features = $3, updated_at = NOW()
WHERE appid = $1 AND id = $2 AND features IS DISTINCT FROM $3::text[] RETURNING id`,
			appID, target, vipdomain.NormalizeFeatureTags(*features)).Scan(&tier.ID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return 0, err
		}
	}
	return target, nil
}

// ResolveVipGrantTier 发放前的预览用：按发放事务同一条规则定档位、并上档位功能（不落账）。
func (r *Repository) ResolveVipGrantTier(ctx context.Context, grant *vipdomain.Grant) error {
	return resolveGrantTierTx(ctx, r.pool, grant)
}

// resolveGrantTierTx 定下这一笔发放落在哪个档位，并把档位的功能并进 grant.Features。
//
//   - 挂套餐：套餐的档位，另给的功能为空（套餐已删时没有档位，按快照）；
//   - 不挂套餐、指定了档位：该档位，grant.Features 是档位之外另给的；
//   - 不挂套餐的永久开通、没指定档位：应用的永久档位（看广告领的除外，它按场景算）；
//   - 其余（会员天数、卡密赠送天数）：不挂档位，与以前一样按发放时的功能快照。
//
// 必须在 extendUserVipTx 的最前面调用：「已拥有」判据（GrantBenefit）看的是解析后的功能。
func resolveGrantTierTx(ctx context.Context, q queryExecutor, grant *vipdomain.Grant) error {
	grant.ExtraFeatures = nil
	if grant.PlanID != nil && *grant.PlanID > 0 {
		var (
			tierID   *int64
			features []string
		)
		err := q.QueryRow(ctx, `SELECT p.tier_id, t.features FROM vip_plans p
LEFT JOIN vip_tiers t ON t.id = p.tier_id
WHERE p.appid = $1 AND p.id = $2`, grant.AppID, *grant.PlanID).Scan(&tierID, &features)
		if errors.Is(err, pgx.ErrNoRows) {
			grant.TierID = nil
			return nil
		}
		if err != nil {
			return err
		}
		grant.TierID = tierID
		if tierID != nil {
			grant.Features = vipdomain.NormalizeFeatureTags(features)
		}
		return nil
	}
	extras := vipdomain.NormalizeFeatureTags(grant.Features)
	if (grant.TierID == nil || *grant.TierID <= 0) && grant.Lifetime && grant.PayChannel != vipdomain.ChannelAdReward {
		var fallback *int64
		if err := q.QueryRow(ctx, `SELECT `+vipDefaultLifetimeTierSQL, grant.AppID).Scan(&fallback); err != nil {
			return err
		}
		grant.TierID = fallback
	}
	if grant.TierID == nil || *grant.TierID <= 0 {
		grant.TierID = nil
		return nil
	}
	tier, err := getVipTierQ(ctx, q, grant.AppID, *grant.TierID)
	if err != nil {
		return err
	}
	if tier == nil {
		return ErrVipTierNotFound
	}
	grant.ExtraFeatures = extras
	grant.Features = vipdomain.TierFeatures(tier.Features, extras)
	return nil
}

// decodeVipPerks 档位的手填权益标签（坏数据当作没有：展示用字段不该让套餐列表整个失败）。
func decodeVipPerks(raw []byte) []vipdomain.Perk {
	perks := []vipdomain.Perk{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &perks)
	}
	if perks == nil {
		perks = []vipdomain.Perk{}
	}
	return perks
}

func scanVipTier(row pgx.Row) (*vipdomain.Tier, error) {
	var item vipdomain.Tier
	var perksRaw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Features, &perksRaw, &item.SortOrder,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Perks = decodeVipPerks(perksRaw)
	if item.Features == nil {
		item.Features = []string{}
	}
	item.Plans = []vipdomain.TierPlanRef{}
	return &item, nil
}

// VipTierBrief 档位的名字与手填权益标签（套餐列表合成权益标签用，不带统计，开销小）。
type VipTierBrief struct {
	Name  string
	Perks []vipdomain.Perk
}

// ListVipTierBriefs 应用全部档位的名字与手填标签。
func (r *Repository) ListVipTierBriefs(ctx context.Context, appID int64) (map[int64]VipTierBrief, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name, perks FROM vip_tiers WHERE appid = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]VipTierBrief{}
	for rows.Next() {
		var id int64
		var name string
		var raw []byte
		if err := rows.Scan(&id, &name, &raw); err != nil {
			return nil, err
		}
		out[id] = VipTierBrief{Name: name, Perks: decodeVipPerks(raw)}
	}
	return out, rows.Err()
}
