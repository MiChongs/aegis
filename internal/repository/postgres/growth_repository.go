package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"aegis/internal/domain/growth"
	"aegis/internal/domain/levels"
)

// 成长调控（docs/growth-control.md）：倍率规则的存取、入账时套用规则、每日统计、调控记录与等级封顶。
//
// 入账时**每次都查库**而不缓存：规则表有部分索引（只含未撤销的行），一次查询是索引内的几行，
// 远比多实例下「刚加的处罚要等缓存过期才生效」划算。

var (
	ErrGrowthRuleMissing   = errors.New("growth: rule not found")
	ErrGrowthRuleLimit     = errors.New("growth: too many live rules for this target")
	ErrGrowthRuleRevoked   = errors.New("growth: rule already revoked")
	ErrGrowthLevelAboveCap = errors.New("growth: current level is above the cap")
	ErrGrowthLevelCapRange = errors.New("growth: level cap out of range")
)

const growthRuleColumns = `r.id, r.appid, COALESCE(r.user_id, 0), COALESCE(u.account, ''), COALESCE(up.nickname, ''), r.kind, r.numerator, r.denominator,
r.rounding, r.daily_cap, r.sources, r.starts_at, r.ends_at, r.enabled, r.label, r.reason, COALESCE(r.created_by, ''), COALESCE(r.updated_by, ''),
r.revoked_at, COALESCE(r.revoked_by, ''), r.created_at, r.updated_at`

const growthRuleJoins = `FROM growth_rate_rules r
LEFT JOIN users u ON u.id = r.user_id
LEFT JOIN user_profiles up ON up.user_id = r.user_id`

func scanGrowthRule(row interface{ Scan(dest ...any) error }) (*growth.Rule, error) {
	var item growth.Rule
	var kind, rounding string
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Account, &item.Nickname, &kind, &item.Numerator, &item.Denominator,
		&rounding, &item.DailyCap, &item.Sources, &item.StartsAt, &item.EndsAt, &item.Enabled, &item.Label, &item.Reason,
		&item.CreatedBy, &item.UpdatedBy, &item.RevokedAt, &item.RevokedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Kind, item.Rounding = growth.Kind(kind), growth.Rounding(rounding)
	item.Decorate(time.Now())
	return &item, nil
}

// ── 入账时套用规则 ──

// liveGrowthRules 一个人此刻对某一类型生效的全部规则（个人 + 全应用）。
func liveGrowthRules(ctx context.Context, q queryExecutor, appID, userID int64, kind growth.Kind) ([]growth.Rule, error) {
	rows, err := q.Query(ctx, `SELECT `+growthRuleColumns+` `+growthRuleJoins+`
WHERE r.appid = $1 AND (r.user_id = $2 OR r.user_id IS NULL) AND r.kind = $3 AND r.revoked_at IS NULL AND r.enabled
  AND (r.starts_at IS NULL OR r.starts_at <= NOW()) AND (r.ends_at IS NULL OR r.ends_at > NOW())`, appID, userID, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []growth.Rule
	for rows.Next() {
		rule, err := scanGrowthRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

// growthDay 统计用的「今天」：与物品发放的每日判定同一个日界。
func growthDay(now time.Time) string { return itemGrantDayStart(now).Format("2006-01-02") }

// applyGrowthTx 一笔受管收入经过规则（调用方已锁住用户行，同一个人的入账在这里是串行的）。
// 没有任何规则作用时返回 (原值结果, false)，不写统计。
func applyGrowthTx(ctx context.Context, q queryExecutor, appID, userID int64, kind growth.Kind, source string, base int64) (growth.Outcome, bool, error) {
	untouched := growth.Outcome{Base: base, Scaled: base, Granted: base, Num: 1, Den: 1, Source: source}
	if source == "" || base <= 0 || userID <= 0 {
		return untouched, false, nil
	}
	rules, err := liveGrowthRules(ctx, q, appID, userID, kind)
	if err != nil || len(rules) == 0 {
		return untouched, false, err
	}
	now := time.Now()
	combined := growth.Combine(rules, source, now)
	if len(combined.RuleIDs) == 0 {
		return untouched, false, nil
	}
	day := growthDay(now)
	var used int64
	if combined.DailyCap > 0 {
		err := q.QueryRow(ctx, `SELECT granted_amount FROM growth_daily_usage WHERE appid = $1 AND user_id = $2 AND kind = $3 AND day = $4`,
			appID, userID, string(kind), day).Scan(&used)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return untouched, false, err
		}
	}
	outcome := growth.Apply(base, combined, used, source, nil)
	if _, err := q.Exec(ctx, `INSERT INTO growth_daily_usage (appid, user_id, kind, day, base_amount, granted_amount, suppressed_amount, events, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, NOW())
ON CONFLICT (appid, user_id, kind, day) DO UPDATE SET
    base_amount = growth_daily_usage.base_amount + EXCLUDED.base_amount,
    granted_amount = growth_daily_usage.granted_amount + EXCLUDED.granted_amount,
    suppressed_amount = growth_daily_usage.suppressed_amount + EXCLUDED.suppressed_amount,
    events = growth_daily_usage.events + 1,
    updated_at = NOW()`,
		appID, userID, string(kind), day, outcome.Base, outcome.Granted, max(outcome.Base-outcome.Granted, 0)); err != nil {
		return untouched, false, err
	}
	return outcome, true, nil
}

// growthExtra 把规则结果并进流水的 extra_data（不改调用方传进来的 map）。
func growthExtra(extra map[string]any, outcome growth.Outcome) map[string]any {
	out := make(map[string]any, len(extra)+1)
	for key, value := range extra {
		out[key] = value
	}
	out["growth"] = outcome
	return out
}

// ── 规则 ──

// ListGrowthRules 规则列表；count 与列表两条查询并行执行。
func (r *Repository) ListGrowthRules(ctx context.Context, query growth.RuleQuery) (*growth.Page[growth.Rule], error) {
	page, limit := max(query.Page, 1), query.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	conds := []string{"r.appid = $1"}
	args := []any{query.AppID}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	switch query.Scope {
	case "app":
		conds = append(conds, "r.user_id IS NULL")
	case "user":
		if query.UserID > 0 {
			conds = append(conds, "r.user_id = "+arg(query.UserID))
		} else {
			conds = append(conds, "r.user_id IS NOT NULL")
		}
	default:
		if query.UserID > 0 {
			conds = append(conds, "(r.user_id = "+arg(query.UserID)+" OR r.user_id IS NULL)")
		}
	}
	if query.Kind.Valid() {
		conds = append(conds, "r.kind = "+arg(string(query.Kind)))
	}
	switch query.Status {
	case growth.StatusRevoked:
		conds = append(conds, "r.revoked_at IS NOT NULL")
	case growth.StatusPaused:
		conds = append(conds, "r.revoked_at IS NULL AND NOT r.enabled")
	case growth.StatusExpired:
		conds = append(conds, "r.revoked_at IS NULL AND r.enabled AND r.ends_at IS NOT NULL AND r.ends_at <= NOW()")
	case growth.StatusScheduled:
		conds = append(conds, "r.revoked_at IS NULL AND r.enabled AND r.starts_at > NOW() AND (r.ends_at IS NULL OR r.ends_at > NOW())")
	case growth.StatusActive:
		conds = append(conds, "r.revoked_at IS NULL AND r.enabled AND (r.starts_at IS NULL OR r.starts_at <= NOW()) AND (r.ends_at IS NULL OR r.ends_at > NOW())")
	case "live":
		// 未撤销、未过期（生效中、暂停、待生效）
		conds = append(conds, "r.revoked_at IS NULL AND (r.ends_at IS NULL OR r.ends_at > NOW())")
	}
	switch query.Effect {
	case growth.EffectFreeze:
		conds = append(conds, "r.numerator = 0")
	case growth.EffectPenalty:
		conds = append(conds, "r.numerator > 0 AND (r.numerator < r.denominator OR (r.numerator = r.denominator AND r.daily_cap > 0))")
	case growth.EffectBoost:
		conds = append(conds, "r.numerator > r.denominator")
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		like := arg("%" + escapeLike(keyword) + "%")
		cond := "(r.label ILIKE " + like + " OR r.reason ILIKE " + like + " OR r.created_by ILIKE " + like + " OR u.account ILIKE " + like + " OR up.nickname ILIKE " + like
		if id, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			cond += " OR r.user_id = " + arg(id) + " OR r.id = " + arg(id)
		}
		conds = append(conds, cond+")")
	}
	where := strings.Join(conds, " AND ")
	countArgs := append([]any(nil), args...)
	listArgs := append(args, limit, (page-1)*limit)

	out := &growth.Page[growth.Rule]{Items: []growth.Rule{}, Page: page, Limit: limit}
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return r.pool.QueryRow(gctx, `SELECT COUNT(*) `+growthRuleJoins+` WHERE `+where, countArgs...).Scan(&out.Total)
	})
	group.Go(func() error {
		rows, err := r.pool.Query(gctx, `SELECT `+growthRuleColumns+` `+growthRuleJoins+` WHERE `+where+`
ORDER BY (r.revoked_at IS NULL) DESC, r.id DESC LIMIT $`+strconv.Itoa(len(listArgs)-1)+` OFFSET $`+strconv.Itoa(len(listArgs)), listArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			rule, err := scanGrowthRule(rows)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, *rule)
		}
		return rows.Err()
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// escapeLike 转义 LIKE 的通配符。
func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

// GetGrowthRule 一条规则。
func (r *Repository) GetGrowthRule(ctx context.Context, appID, ruleID int64) (*growth.Rule, error) {
	rule, err := scanGrowthRule(r.pool.QueryRow(ctx, `SELECT `+growthRuleColumns+` `+growthRuleJoins+` WHERE r.appid = $1 AND r.id = $2`, appID, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrowthRuleMissing
	}
	return rule, err
}

func nullableUserID(userID int64) any {
	if userID <= 0 {
		return nil
	}
	return userID
}

func insertGrowthEvent(ctx context.Context, q queryExecutor, appID, userID, ruleID int64, action, summary string, detail map[string]any, operator, note string) error {
	raw, _ := json.Marshal(detail)
	if detail == nil {
		raw = []byte("{}")
	}
	var rule any
	if ruleID > 0 {
		rule = ruleID
	}
	_, err := q.Exec(ctx, `INSERT INTO growth_events (appid, user_id, rule_id, action, summary, detail, operator, note) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		appID, nullableUserID(userID), rule, action, truncateRunes(summary, 400), raw, nullableString(operator), nullableString(truncateRunes(note, 400)))
	return err
}

// lockGrowthTarget 锁住规则的作用对象（个人规则锁用户行，全应用规则用事务级咨询锁），让「最多 N 条」的检查不被并发绕过。
func lockGrowthTarget(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	if userID > 0 {
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUserNotFound
			}
			return err
		}
		return nil
	}
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('growth_rules'), $1::int)`, int32(appID))
	return err
}

// CreateGrowthRule 新建规则并记一条调控记录。
func (r *Repository) CreateGrowthRule(ctx context.Context, in growth.RuleInput) (*growth.Rule, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockGrowthTarget(ctx, tx, in.AppID, in.UserID); err != nil {
		return nil, err
	}
	var live int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM growth_rate_rules WHERE appid = $1 AND user_id IS NOT DISTINCT FROM $2 AND kind = $3
AND revoked_at IS NULL AND (ends_at IS NULL OR ends_at > NOW())`, in.AppID, nullableUserID(in.UserID), string(in.Kind)).Scan(&live); err != nil {
		return nil, err
	}
	if live >= growth.MaxActiveRulesPerTarget {
		return nil, ErrGrowthRuleLimit
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO growth_rate_rules (appid, user_id, kind, numerator, denominator, rounding, daily_cap, sources, starts_at, ends_at, enabled, label, reason, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14) RETURNING id`,
		in.AppID, nullableUserID(in.UserID), string(in.Kind), in.Numerator, in.Denominator, string(in.Rounding), in.DailyCap, in.Sources,
		in.StartsAt, in.EndsAt, in.Enabled, in.Label, in.Reason, nullableString(in.Operator)).Scan(&id); err != nil {
		return nil, err
	}
	if err := insertGrowthEvent(ctx, tx, in.AppID, in.UserID, id, growth.ActionRuleCreate, in.Describe(),
		map[string]any{"rule": ruleInputDetail(in)}, in.Operator, in.Reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetGrowthRule(ctx, in.AppID, id)
}

func ruleInputDetail(in growth.RuleInput) map[string]any {
	return map[string]any{
		"kind": in.Kind, "numerator": in.Numerator, "denominator": in.Denominator, "rounding": in.Rounding,
		"dailyCap": in.DailyCap, "sources": in.Sources, "startsAt": in.StartsAt, "endsAt": in.EndsAt, "enabled": in.Enabled, "label": in.Label,
	}
}

// UpdateGrowthRule 修改规则（作用对象与类型不可改；已撤销的不可改）。
func (r *Repository) UpdateGrowthRule(ctx context.Context, ruleID int64, in growth.RuleInput) (*growth.Rule, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	before, err := scanGrowthRule(tx.QueryRow(ctx, `SELECT `+growthRuleColumns+` `+growthRuleJoins+` WHERE r.appid = $1 AND r.id = $2 FOR UPDATE OF r`, in.AppID, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrGrowthRuleMissing
	}
	if err != nil {
		return nil, err
	}
	if before.RevokedAt != nil {
		return nil, ErrGrowthRuleRevoked
	}
	in.UserID, in.Kind = before.UserID, before.Kind
	if _, err := tx.Exec(ctx, `UPDATE growth_rate_rules SET numerator = $3, denominator = $4, rounding = $5, daily_cap = $6, sources = $7,
starts_at = $8, ends_at = $9, enabled = $10, label = $11, reason = $12, updated_by = $13, updated_at = NOW() WHERE appid = $1 AND id = $2`,
		in.AppID, ruleID, in.Numerator, in.Denominator, string(in.Rounding), in.DailyCap, in.Sources, in.StartsAt, in.EndsAt, in.Enabled,
		in.Label, in.Reason, nullableString(in.Operator)); err != nil {
		return nil, err
	}
	summary := "修改规则：" + in.Describe()
	if before.Enabled != in.Enabled {
		if in.Enabled {
			summary = "恢复规则：" + in.Describe()
		} else {
			summary = "暂停规则：" + in.Describe()
		}
	}
	if err := insertGrowthEvent(ctx, tx, in.AppID, in.UserID, ruleID, growth.ActionRuleUpdate, summary,
		map[string]any{"before": before, "after": ruleInputDetail(in)}, in.Operator, in.Reason); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetGrowthRule(ctx, in.AppID, ruleID)
}

// RevokeGrowthRule 撤销规则（立即失效，保留记录）。
func (r *Repository) RevokeGrowthRule(ctx context.Context, appID, ruleID int64, operator, note string) (*growth.Rule, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID int64
	var kind string
	var num, den int
	err = tx.QueryRow(ctx, `UPDATE growth_rate_rules SET revoked_at = NOW(), revoked_by = $3, updated_at = NOW()
WHERE appid = $1 AND id = $2 AND revoked_at IS NULL RETURNING COALESCE(user_id, 0), kind, numerator, denominator`,
		appID, ruleID, nullableString(operator)).Scan(&userID, &kind, &num, &den)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetGrowthRule(ctx, appID, ruleID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrGrowthRuleRevoked
	}
	if err != nil {
		return nil, err
	}
	summary := "撤销规则：" + growth.Kind(kind).Label() + " " + growth.Factor{Num: int64(num), Den: int64(den)}.String()
	if err := insertGrowthEvent(ctx, tx, appID, userID, ruleID, growth.ActionRuleRevoke, summary, nil, operator, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetGrowthRule(ctx, appID, ruleID)
}

// DeleteGrowthRule 删除规则（调控记录保留）。
func (r *Repository) DeleteGrowthRule(ctx context.Context, appID, ruleID int64, operator string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID int64
	var kind string
	var num, den int
	err = tx.QueryRow(ctx, `DELETE FROM growth_rate_rules WHERE appid = $1 AND id = $2 RETURNING COALESCE(user_id, 0), kind, numerator, denominator`,
		appID, ruleID).Scan(&userID, &kind, &num, &den)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrGrowthRuleMissing
	}
	if err != nil {
		return err
	}
	summary := "删除规则：" + growth.Kind(kind).Label() + " " + growth.Factor{Num: int64(num), Den: int64(den)}.String()
	if err := insertGrowthEvent(ctx, tx, appID, userID, ruleID, growth.ActionRuleDelete, summary, nil, operator, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LiveGrowthRules 一个人此刻对某一类型生效的规则（推演与详情页用，不在事务里）。
func (r *Repository) LiveGrowthRules(ctx context.Context, appID, userID int64, kind growth.Kind) ([]growth.Rule, error) {
	return liveGrowthRules(ctx, r.pool, appID, userID, kind)
}

// ── 调控记录与统计 ──

// ListGrowthEvents 调控记录。userID > 0 时包括这个人的记录与全应用的记录。
func (r *Repository) ListGrowthEvents(ctx context.Context, appID, userID int64, page, limit int) (*growth.Page[growth.Event], error) {
	page = max(page, 1)
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	where := "appid = $1"
	args := []any{appID}
	if userID > 0 {
		where += " AND (user_id = $2 OR user_id IS NULL)"
		args = append(args, userID)
	}
	out := &growth.Page[growth.Event]{Items: []growth.Event{}, Page: page, Limit: limit}
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return r.pool.QueryRow(gctx, `SELECT COUNT(*) FROM growth_events WHERE `+where, args...).Scan(&out.Total)
	})
	group.Go(func() error {
		listArgs := append(append([]any(nil), args...), limit, (page-1)*limit)
		rows, err := r.pool.Query(gctx, `SELECT id, appid, COALESCE(user_id, 0), COALESCE(rule_id, 0), action, summary, detail, COALESCE(operator, ''), COALESCE(note, ''), created_at
FROM growth_events WHERE `+where+` ORDER BY id DESC LIMIT $`+strconv.Itoa(len(listArgs)-1)+` OFFSET $`+strconv.Itoa(len(listArgs)), listArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item growth.Event
			var raw []byte
			if err := rows.Scan(&item.ID, &item.AppID, &item.UserID, &item.RuleID, &item.Action, &item.Summary, &raw, &item.Operator, &item.Note, &item.CreatedAt); err != nil {
				return err
			}
			_ = json.Unmarshal(raw, &item.Detail)
			if item.Detail == nil {
				item.Detail = map[string]any{}
			}
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// GrowthUsage 一个人最近 days 天（含今天）的入账统计，按天倒序。
func (r *Repository) GrowthUsage(ctx context.Context, appID, userID int64, days int) ([]growth.Usage, error) {
	since := itemGrantDayStart(time.Now()).AddDate(0, 0, -(max(days, 1) - 1)).Format("2006-01-02")
	rows, err := r.pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), kind, base_amount, granted_amount, suppressed_amount, events
FROM growth_daily_usage WHERE appid = $1 AND user_id = $2 AND day >= $3 ORDER BY day DESC, kind`, appID, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []growth.Usage{}
	for rows.Next() {
		var item growth.Usage
		var kind string
		if err := rows.Scan(&item.Day, &kind, &item.Base, &item.Granted, &item.Suppressed, &item.Events); err != nil {
			return nil, err
		}
		item.Kind = growth.Kind(kind)
		out = append(out, item)
	}
	return out, rows.Err()
}

// GrowthTodayGranted 今天某一类型已经实际入账的受管收入（每日上限的进度条用）。
func (r *Repository) GrowthTodayGranted(ctx context.Context, appID, userID int64, kind growth.Kind) (int64, error) {
	var used int64
	err := r.pool.QueryRow(ctx, `SELECT granted_amount FROM growth_daily_usage WHERE appid = $1 AND user_id = $2 AND kind = $3 AND day = $4`,
		appID, userID, string(kind), growthDay(time.Now())).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

// GrowthSourceStat 一个来源在一段时间里的收入：base 是规则作用前，granted 是实际入账。
type GrowthSourceStat struct {
	Source  string `json:"source"`
	Base    int64  `json:"base"`
	Granted int64  `json:"granted"`
	Count   int64  `json:"count"`
	Touched int64  `json:"touched"`
}

// GrowthSourceStats 最近 days 天按来源汇总的收入。签到事务自己会写 source_type = daily_signin 的流水，归到 signin。
func (r *Repository) GrowthSourceStats(ctx context.Context, appID, userID int64, kind growth.Kind, days int) ([]GrowthSourceStat, error) {
	since := itemGrantDayStart(time.Now()).AddDate(0, 0, -(max(days, 1) - 1))
	table := "integral_transactions"
	if kind == growth.KindExperience {
		table = "experience_transactions"
	}
	rows, err := r.pool.Query(ctx, `SELECT CASE WHEN COALESCE(source_type, category) = 'daily_signin' THEN 'signin' ELSE COALESCE(source_type, category) END AS source,
COALESCE(SUM(COALESCE((extra_data->'growth'->>'base')::bigint, amount)), 0),
COALESCE(SUM(amount), 0), COUNT(*), COUNT(*) FILTER (WHERE extra_data ? 'growth')
FROM `+table+` WHERE user_id = $1 AND appid = $2 AND type = 'earn' AND amount >= 0 AND created_at >= $3
GROUP BY 1
ORDER BY 3 DESC`, userID, appID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GrowthSourceStat{}
	for rows.Next() {
		var item GrowthSourceStat
		if err := rows.Scan(&item.Source, &item.Base, &item.Granted, &item.Count, &item.Touched); err != nil {
			return nil, err
		}
		if item.Count == 0 {
			continue
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ── 等级封顶 ──

// GrowthLevelState 一个人的等级与封顶状态。
type GrowthLevelState struct {
	Level      int   `json:"level"`
	Locked     int   `json:"locked"`
	Capped     int   `json:"capped"`
	Highest    int   `json:"highest"`
	MaxLevel   int   `json:"maxLevel"`
	Experience int64 `json:"experience"`
	// Eligible 只按经验与晋级条件（不看封顶）应到的等级
	Eligible int `json:"eligible"`
}

// GrowthLevel 读等级、锁定、封顶与「不封顶时应到的等级」。
func (r *Repository) GrowthLevel(ctx context.Context, appID, userID int64) (*GrowthLevelState, error) {
	scheme, _, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	state := &GrowthLevelState{MaxLevel: scheme.MaxLevel()}
	err = r.pool.QueryRow(ctx, `SELECT u.experience, COALESCE(l.current_level, 0), COALESCE(l.locked_level, 0), COALESCE(l.capped_level, 0), COALESCE(l.highest_level, 0)
FROM users u LEFT JOIN user_level_records l ON l.user_id = u.id AND l.appid = u.appid WHERE u.id = $1 AND u.appid = $2`, userID, appID).
		Scan(&state.Experience, &state.Level, &state.Locked, &state.Capped, &state.Highest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	// 只按经验估算（晋级条件按需画像，这里不取）：用来提示「解除封顶后会到第几级」
	state.Eligible, _, _ = scheme.Resolve(state.Experience, max(state.Level, 1), nil)
	if state.Level == 0 {
		state.Level = max(state.Eligible, 1)
	}
	return state, nil
}

// SetGrowthLevelCap 设置等级封顶。现有等级高于封顶时：demote 为 true 就降到封顶（锁定等级一并截到封顶，已发的达成奖励不收回、
// 再次升上去也不重复发），否则拒绝。
func (r *Repository) SetGrowthLevelCap(ctx context.Context, appID, userID int64, capLevel int, demote bool, operator, note string) (*GrowthLevelState, error) {
	scheme, version, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	if capLevel < 1 || capLevel > scheme.MaxLevel() {
		return nil, ErrGrowthLevelCapRange
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var experience int64
	if err := tx.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	// 先确保有等级记录（第一次时按体系补判）
	synced, err := r.syncUserLevel(ctx, tx, userID, appID, experience, time.Now().UTC(), levelSyncOptions{Reason: levels.ReasonSync, Silent: true})
	if err != nil {
		return nil, err
	}
	current := synced.To
	if current > capLevel && !demote {
		return nil, ErrGrowthLevelAboveCap
	}
	if _, err := tx.Exec(ctx, `UPDATE user_level_records SET capped_level = $3, updated_at = NOW() WHERE user_id = $1 AND appid = $2`,
		userID, appID, capLevel); err != nil {
		return nil, err
	}
	detail := map[string]any{"cap": capLevel, "from": current, "demote": demote && current > capLevel}
	summary := "等级封顶 Lv." + strconv.Itoa(capLevel)
	if demote && current > capLevel {
		levelState := scheme.StateOf(capLevel, experience)
		var nextLevelExperience any
		if !levelState.IsMaxLevel {
			nextLevelExperience = levelState.ExpToNextLevel
		}
		if _, err := tx.Exec(ctx, `UPDATE user_level_records SET current_level = $3, current_experience = $4, next_level_experience = $5,
level_progress = $6, locked_level = LEAST(locked_level, $3), updated_at = NOW() WHERE user_id = $1 AND appid = $2`,
			userID, appID, capLevel, levelState.ExpInCurrentLevel, nextLevelExperience, levelState.Progress); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_level_events (appid, user_id, from_level, to_level, floor_level, experience, reason, scheme_version, operator, note, notify_status)
VALUES ($1, $2, $3, $4, $3, $5, $6, $7, $8, $9, $10)`,
			appID, userID, current, capLevel, experience, levels.ReasonPenalty, version, operator, truncateRunes(note, 200), levels.NotifyNone); err != nil {
			return nil, err
		}
		summary += "，由 Lv." + strconv.Itoa(current) + " 降至 Lv." + strconv.Itoa(capLevel)
	}
	if err := insertGrowthEvent(ctx, tx, appID, userID, 0, growth.ActionLevelCap, summary, detail, operator, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GrowthLevel(ctx, appID, userID)
}

// ClearGrowthLevelCap 解除等级封顶，并按当前体系补判一次（经验已够的级数立即升上去，达成奖励不会重复发）。
func (r *Repository) ClearGrowthLevelCap(ctx context.Context, appID, userID int64, operator, note string) (*GrowthLevelState, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var experience int64
	if err := tx.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	var previous int
	err = tx.QueryRow(ctx, `SELECT capped_level FROM user_level_records WHERE user_id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE user_level_records SET capped_level = 0, updated_at = NOW() WHERE user_id = $1 AND appid = $2`, userID, appID); err != nil {
		return nil, err
	}
	synced, err := r.syncUserLevel(ctx, tx, userID, appID, experience, time.Now().UTC(), levelSyncOptions{Reason: levels.ReasonAdmin, Operator: operator, Note: note})
	if err != nil {
		return nil, err
	}
	summary := "解除等级封顶"
	if synced.Changed {
		summary += "，升至 Lv." + strconv.Itoa(synced.To)
	}
	if err := insertGrowthEvent(ctx, tx, appID, userID, 0, growth.ActionLevelUncap, summary,
		map[string]any{"previousCap": previous, "from": synced.From, "to": synced.To}, operator, note); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GrowthLevel(ctx, appID, userID)
}
