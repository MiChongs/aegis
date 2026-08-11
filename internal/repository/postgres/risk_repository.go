package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"

	"github.com/jackc/pgx/v5"
)

// ════════════════════════════════════════════════════════════
//  风险规则 CRUD
// ════════════════════════════════════════════════════════════

// riskRuleColumns 规则查询列，顺序与 scanRiskRule 一一对应。
const riskRuleColumns = `id, name, description, scene, condition_type, condition_data, score, is_active, priority,
       hit_count, last_hit_at, created_by, updated_by, created_at, updated_at`

// CreateRiskRule 创建风险规则
func (r *Repository) CreateRiskRule(ctx context.Context, input securitydomain.CreateRiskRuleInput, createdBy int64) (*securitydomain.RiskRule, error) {
	condJSON, err := json.Marshal(input.ConditionData)
	if err != nil {
		return nil, fmt.Errorf("marshal condition_data: %w", err)
	}
	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	query := `INSERT INTO risk_rules (name, description, scene, condition_type, condition_data, score, is_active, priority, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
RETURNING ` + riskRuleColumns
	return scanRiskRule(r.pool.QueryRow(ctx, query,
		input.Name, input.Description, input.Scene, input.ConditionType,
		condJSON, input.Score, isActive, input.Priority, createdBy,
	))
}

// ListRiskRules 列出风险规则（可按 scene 过滤）
func (r *Repository) ListRiskRules(ctx context.Context, scene string) ([]securitydomain.RiskRule, error) {
	query := `SELECT ` + riskRuleColumns + ` FROM risk_rules`
	var args []any
	if scene != "" {
		query += ` WHERE scene = $1`
		args = []any{scene}
	}
	query += ` ORDER BY scene ASC, priority ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// GetRiskRule 按 ID 查询单条规则
func (r *Repository) GetRiskRule(ctx context.Context, id int64) (*securitydomain.RiskRule, error) {
	return scanRiskRule(r.pool.QueryRow(ctx, `SELECT `+riskRuleColumns+` FROM risk_rules WHERE id = $1`, id))
}

// UpdateRiskRule 更新风险规则（可选字段）
func (r *Repository) UpdateRiskRule(ctx context.Context, id int64, input securitydomain.UpdateRiskRuleInput) error {
	sets := make([]string, 0, 9)
	args := make([]any, 0, 10)
	idx := 1
	add := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}

	if input.Name != nil {
		add("name", *input.Name)
	}
	if input.Description != nil {
		add("description", *input.Description)
	}
	if input.Scene != nil {
		add("scene", *input.Scene)
	}
	if input.ConditionType != nil {
		add("condition_type", *input.ConditionType)
	}
	if input.ConditionData != nil {
		condJSON, err := json.Marshal(*input.ConditionData)
		if err != nil {
			return fmt.Errorf("marshal condition_data: %w", err)
		}
		add("condition_data", condJSON)
	}
	if input.Score != nil {
		add("score", *input.Score)
	}
	if input.IsActive != nil {
		add("is_active", *input.IsActive)
	}
	if input.Priority != nil {
		add("priority", *input.Priority)
	}
	if input.UpdatedBy != nil {
		add("updated_by", *input.UpdatedBy)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE risk_rules SET %s WHERE id = $%d", strings.Join(sets, ", "), idx)
	_, err := r.pool.Exec(ctx, query, args...)
	return err
}

// DeleteRiskRule 删除风险规则
func (r *Repository) DeleteRiskRule(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM risk_rules WHERE id = $1`, id)
	return err
}

// GetActiveRulesByScene 获取指定场景的活跃规则（按优先级排序）
func (r *Repository) GetActiveRulesByScene(ctx context.Context, scene string) ([]securitydomain.RiskRule, error) {
	query := `SELECT ` + riskRuleColumns + `
FROM risk_rules WHERE scene = $1 AND is_active = TRUE ORDER BY priority ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, scene)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// BumpRuleHits 批量累加规则命中计数。
// 一次评估可能命中多条规则，逐条 UPDATE 会产生 N 次往返；
// 用 ANY($1) 一次改完，且天然按主键顺序加锁，不会与并发评估互相死锁。
func (r *Repository) BumpRuleHits(ctx context.Context, ruleIDs []int64) error {
	if len(ruleIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE risk_rules SET hit_count = hit_count + 1, last_hit_at = NOW() WHERE id = ANY($1)`, ruleIDs)
	return err
}

// GetRuleHitStat 统计一条规则在区间内的命中效果。
// matched_rules 是 JSONB 数组，用 @> 做包含判断即可走 GIN 索引。
func (r *Repository) GetRuleHitStat(ctx context.Context, ruleID int64, start, end time.Time) (securitydomain.RuleHitStat, error) {
	stat := securitydomain.RuleHitStat{RuleID: ruleID}
	// ScoreSum 统计的是**这条规则**贡献的分数，不是命中请求的总分 ——
	// 后者会把其他规则的贡献一并算进来，用它衡量单条规则的影响是错的。
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(COUNT(*), 0),
       COALESCE(COUNT(*) FILTER (WHERE a.action IN ('block','ban')), 0),
       COALESCE(SUM((m.rule ->> 'score')::int), 0),
       MAX(a.created_at)
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(`+matchedRulesArray+`) AS m(rule)
WHERE a.created_at BETWEEN $2 AND $3
  AND (m.rule ->> 'ruleId')::bigint = $1`,
		ruleID, start, end).Scan(&stat.Hits, &stat.Blocked, &stat.ScoreSum, &stat.LastHitAt)
	if err != nil {
		return stat, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT name, scene, condition_type, score, is_active FROM risk_rules WHERE id = $1`, ruleID,
	).Scan(&stat.RuleName, &stat.Scene, &stat.ConditionType, &stat.Score, &stat.IsActive); err != nil {
		return stat, normalizeNotFound(err)
	}
	return stat, nil
}

// GetRuleHitSeries 一条规则的命中时间序列
func (r *Repository) GetRuleHitSeries(ctx context.Context, ruleID int64, start, end time.Time, bucket string) ([]securitydomain.RuleHitPoint, error) {
	unit := normalizeBucket(bucket)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
SELECT buckets.slot, COALESCE(hits.total, 0)
FROM generate_series(date_trunc('%[1]s', $2::timestamptz), date_trunc('%[1]s', $3::timestamptz), interval '1 %[1]s') AS buckets(slot)
LEFT JOIN (
    SELECT date_trunc('%[1]s', created_at) AS slot, COUNT(*) AS total
    FROM risk_assessments
    WHERE created_at BETWEEN $2 AND $3 AND matched_rules @> $1::jsonb
    GROUP BY 1
) AS hits ON hits.slot = buckets.slot
ORDER BY buckets.slot ASC`, unit), ruleMatchFilter(ruleID), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]securitydomain.RuleHitPoint, 0, 64)
	for rows.Next() {
		var point securitydomain.RuleHitPoint
		if err := rows.Scan(&point.Time, &point.Count); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

// matchedRulesArray 把 matched_rules 收敛成 JSONB 数组之后再展开。
//
// 这一层防御不是多余的：旧写入路径对一个 nil 切片调 json.Marshal 得到的是
// **标量 `null`**（不是 `[]`），而 jsonb_array_elements 展开标量会直接抛
// 22023 cannot extract elements from a scalar —— 一条历史脏数据就能让整个
// 大盘 500。列上的 DEFAULT '[]' 挡不住显式写入的 null。
const matchedRulesArray = `CASE WHEN jsonb_typeof(a.matched_rules) = 'array' THEN a.matched_rules ELSE '[]'::jsonb END`

// ruleMatchFilter 构造 `matched_rules @> '[{"ruleId":N}]'` 的右操作数
func ruleMatchFilter(ruleID int64) string {
	return fmt.Sprintf(`[{"ruleId":%d}]`, ruleID)
}

// normalizeBucket 只允许 hour / day 两种粒度。
// 这个值会被拼进 SQL（date_trunc 的单位不能参数化），所以必须在这里收敛成白名单。
func normalizeBucket(bucket string) string {
	if strings.EqualFold(strings.TrimSpace(bucket), "day") {
		return "day"
	}
	return "hour"
}

// ════════════════════════════════════════════════════════════
//  评估记录
// ════════════════════════════════════════════════════════════

// riskAssessmentColumns 评估记录的查询列，顺序与 scanRiskAssessment 一一对应。
// review_result 建表时可空，而域类型里是非指针 string —— 未复核的记录直接 Scan 会报
// "cannot scan NULL into *string"。所有查询共用这份清单，避免有人新加查询时漏掉 COALESCE。
const riskAssessmentColumns = `a.id, a.scene, a.app_id, a.user_id, a.identity_id, a.account, a.ip, a.device_id,
       a.user_agent, a.country, a.total_score, a.risk_level, a.matched_rules, a.eval_context, a.latency_ms,
       a.action, a.action_detail, a.reviewed, a.reviewer_id, COALESCE(adm.account, ''),
       COALESCE(a.review_result, ''), a.review_comment, a.reviewed_at, a.created_at`

const riskAssessmentFrom = `FROM risk_assessments a LEFT JOIN admin_accounts adm ON adm.id = a.reviewer_id`

// normalizeAssessmentShape 把两个 JSONB 字段的形状定死在写入入口。
//
// nil 切片 / nil map 的 json.Marshal 结果是**标量 `null`**，不是 `[]` / `{}`；
// 列上的 DEFAULT 挡不住显式写入。落库之后，任何
// jsonb_array_elements(matched_rules) 都会在这一行上抛
// 22023 cannot extract elements from a scalar —— 一条脏数据足以让整个大盘 500。
func normalizeAssessmentShape(a *securitydomain.RiskAssessment) {
	if a.MatchedRules == nil {
		a.MatchedRules = []securitydomain.MatchedRule{}
	}
	if a.EvalContext == nil {
		a.EvalContext = map[string]any{}
	}
}

// CreateRiskAssessment 创建风险评估记录
func (r *Repository) CreateRiskAssessment(ctx context.Context, a securitydomain.RiskAssessment) (*securitydomain.RiskAssessment, error) {
	normalizeAssessmentShape(&a)
	rulesJSON, err := json.Marshal(a.MatchedRules)
	if err != nil {
		return nil, fmt.Errorf("marshal matched_rules: %w", err)
	}
	contextJSON, err := json.Marshal(a.EvalContext)
	if err != nil {
		return nil, fmt.Errorf("marshal eval_context: %w", err)
	}
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO risk_assessments
    (scene, app_id, user_id, identity_id, account, ip, device_id, user_agent, country,
     total_score, risk_level, matched_rules, eval_context, latency_ms, action, action_detail, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
RETURNING id`,
		a.Scene, a.AppID, a.UserID, a.IdentityID, truncateColumn(a.Account, 190),
		a.IP, a.DeviceID, a.UserAgent, truncateColumn(a.Country, 64),
		a.TotalScore, a.RiskLevel, rulesJSON, contextJSON, a.LatencyMS, a.Action, a.ActionDetail,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	a.ID = id
	return &a, nil
}

// buildAssessmentFilter 把查询条件翻译成 WHERE 子句。
// 列表页、详情页的关联查询、规则详情的最近命中共用它 ——
// 各写一套过滤条件是「同一个筛选在两个页面结果不一致」的来源。
func buildAssessmentFilter(query securitydomain.AssessmentQuery, startIdx int) (string, []any) {
	where := make([]string, 0, 12)
	args := make([]any, 0, 12)
	idx := startIdx
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, idx))
		args = append(args, value)
		idx++
	}

	if query.Scene != "" {
		add("a.scene = $%d", query.Scene)
	}
	if query.RiskLevel != "" {
		add("a.risk_level = $%d", query.RiskLevel)
	}
	if query.Action != "" {
		add("a.action = $%d", query.Action)
	}
	if query.IP != "" {
		add("a.ip = $%d", query.IP)
	}
	if query.DeviceID != "" {
		add("a.device_id = $%d", query.DeviceID)
	}
	if query.Account != "" {
		add("a.account = $%d", query.Account)
	}
	if query.RuleID > 0 {
		add("a.matched_rules @> $%d::jsonb", ruleMatchFilter(query.RuleID))
	}
	if query.Reviewed != nil {
		add("a.reviewed = $%d", *query.Reviewed)
	}
	if query.MinScore != nil {
		add("a.total_score >= $%d", *query.MinScore)
	}
	if query.MaxScore != nil {
		add("a.total_score <= $%d", *query.MaxScore)
	}
	if query.Start != nil {
		add("a.created_at >= $%d", *query.Start)
	}
	if query.End != nil {
		add("a.created_at <= $%d", *query.End)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		// 一个搜索框同时找 IP / 设备 / 账号：三者在排查时是同一件事的三个入口，
		// 让运维先想清楚"这串字符属于哪一类"是多余的负担。
		where = append(where, fmt.Sprintf("(a.ip ILIKE $%d OR a.device_id ILIKE $%d OR a.account ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}

	if len(where) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// ListRiskAssessments 分页查询评估记录
func (r *Repository) ListRiskAssessments(ctx context.Context, query securitydomain.AssessmentQuery) ([]securitydomain.RiskAssessment, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	whereClause, args := buildAssessmentFilter(query, 1)

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM risk_assessments a %s", whereClause)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.RiskAssessment{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	dataQuery := fmt.Sprintf(`SELECT %s %s %s ORDER BY a.created_at DESC, a.id DESC LIMIT $%d OFFSET $%d`,
		riskAssessmentColumns, riskAssessmentFrom, whereClause, len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectRiskAssessments(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetRiskAssessment 获取单条评估记录
func (r *Repository) GetRiskAssessment(ctx context.Context, id int64) (*securitydomain.RiskAssessment, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE a.id = $1`, riskAssessmentColumns, riskAssessmentFrom)
	return scanRiskAssessment(r.pool.QueryRow(ctx, query, id))
}

// ReviewRiskAssessment 复核评估记录
func (r *Repository) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, result, comment string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE risk_assessments
SET reviewed = TRUE, reviewer_id = $2, review_result = $3, review_comment = $4, reviewed_at = NOW()
WHERE id = $1 AND NOT reviewed`, id, reviewerID, result, comment)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("评估记录不存在或已复核")
	}
	return nil
}

// PurgeRiskAssessments 清理指定时刻之前的评估记录，返回删除行数
func (r *Repository) PurgeRiskAssessments(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM risk_assessments WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetEntitySummary 聚合某个实体（ip / device / account）的画像。
// 详情页靠它回答「这个 IP 到底闹了多久、碰过多少账号」。
func (r *Repository) GetEntitySummary(ctx context.Context, kind, value string) (securitydomain.RiskEntitySummary, error) {
	var column string
	switch kind {
	case "ip":
		column = "ip"
	case "device":
		column = "device_id"
	case "account":
		column = "account"
	default:
		return securitydomain.RiskEntitySummary{}, fmt.Errorf("未知实体类型：%s", kind)
	}

	var summary securitydomain.RiskEntitySummary
	err := r.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE action IN ('block','ban')),
       COALESCE(AVG(total_score), 0),
       COALESCE(MAX(total_score), 0),
       COUNT(DISTINCT account) FILTER (WHERE account <> ''),
       COUNT(DISTINCT device_id) FILTER (WHERE device_id <> ''),
       COUNT(DISTINCT ip) FILTER (WHERE ip <> ''),
       MIN(created_at), MAX(created_at)
FROM risk_assessments WHERE %s = $1`, column), value).Scan(
		&summary.TotalAssessments, &summary.Blocked, &summary.AvgScore, &summary.MaxScore,
		&summary.DistinctAccounts, &summary.DistinctDevices, &summary.DistinctIPs,
		&summary.FirstSeenAt, &summary.LastSeenAt,
	)
	return summary, err
}

// ListIPPeers 列出与某 IP 共现的设备与账号
func (r *Repository) ListIPPeers(ctx context.Context, ip string, limit int) ([]securitydomain.DeviceFingerprint, []string, error) {
	deviceRows, err := r.pool.Query(ctx, `SELECT `+deviceFingerprintColumns+`
FROM device_fingerprints
WHERE device_id IN (SELECT DISTINCT device_id FROM risk_assessments WHERE ip = $1 AND device_id <> '' LIMIT $2)
ORDER BY last_seen_at DESC`, ip, limit)
	if err != nil {
		return nil, nil, err
	}
	defer deviceRows.Close()
	devices, err := collectDeviceFingerprints(deviceRows)
	if err != nil {
		return nil, nil, err
	}

	accounts, err := r.distinctColumn(ctx, `SELECT DISTINCT account FROM risk_assessments WHERE ip = $1 AND account <> '' ORDER BY account LIMIT $2`, ip, limit)
	if err != nil {
		return devices, nil, err
	}
	return devices, accounts, nil
}

// ListDevicePeers 列出某设备用过的 IP 与账号
func (r *Repository) ListDevicePeers(ctx context.Context, deviceID string, limit int) ([]string, []string, error) {
	ips, err := r.distinctColumn(ctx, `SELECT DISTINCT ip FROM risk_assessments WHERE device_id = $1 AND ip <> '' ORDER BY ip LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := r.distinctColumn(ctx, `SELECT DISTINCT account FROM risk_assessments WHERE device_id = $1 AND account <> '' ORDER BY account LIMIT $2`, deviceID, limit)
	if err != nil {
		return ips, nil, err
	}
	return ips, accounts, nil
}

func (r *Repository) distinctColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0, 16)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

const deviceFingerprintColumns = `id, device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, note,
       first_seen_at, last_seen_at, seen_count`

// TouchDeviceFingerprint 记录一次设备出现。
//
// 与旧的 UpsertDeviceFingerprint 有两处关键差别：
//   - **不覆盖 risk_tag**。管理员刚把一台设备标成 blocked，下一个请求就把它写回
//     默认值，等于人工处置永远生效不了几秒。
//   - **user_id 只在原本为空时补写**。设备是物理实体，第一次绑定的账号才有归属意义，
//     后来者不断覆盖只会让这一列变成"最后一个用它登录的人"，失去查证价值。
func (r *Repository) TouchDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	fpJSON, err := json.Marshal(fp.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("marshal fingerprint: %w", err)
	}
	riskTag := fp.RiskTag
	if riskTag == "" {
		riskTag = securitydomain.TagNormal
	}
	query := `INSERT INTO device_fingerprints
    (device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, first_seen_at, last_seen_at, seen_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), 1)
ON CONFLICT (device_id) DO UPDATE SET
    fingerprint  = COALESCE(NULLIF(EXCLUDED.fingerprint, '{}'::jsonb), device_fingerprints.fingerprint),
    user_id      = COALESCE(device_fingerprints.user_id, EXCLUDED.user_id),
    app_id       = COALESCE(device_fingerprints.app_id, EXCLUDED.app_id),
    last_ip      = COALESCE(NULLIF(EXCLUDED.last_ip, ''), device_fingerprints.last_ip),
    user_agent   = COALESCE(NULLIF(EXCLUDED.user_agent, ''), device_fingerprints.user_agent),
    last_seen_at = NOW(),
    seen_count   = device_fingerprints.seen_count + 1
RETURNING ` + deviceFingerprintColumns
	return scanDeviceFingerprint(r.pool.QueryRow(ctx, query,
		fp.DeviceID, fp.UserID, fp.AppID, fpJSON, riskTag, fp.LastIP, fp.UserAgent,
	))
}

// UpsertDeviceFingerprint 保留旧名，语义等同 TouchDeviceFingerprint
func (r *Repository) UpsertDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	return r.TouchDeviceFingerprint(ctx, fp)
}

// GetDeviceFingerprint 按 device_id 查询设备指纹
func (r *Repository) GetDeviceFingerprint(ctx context.Context, deviceID string) (*securitydomain.DeviceFingerprint, error) {
	return scanDeviceFingerprint(r.pool.QueryRow(ctx,
		`SELECT `+deviceFingerprintColumns+` FROM device_fingerprints WHERE device_id = $1`, deviceID))
}

// ListDeviceFingerprints 分页查询设备。
// OnlyRisk 为 false 时返回全部设备 —— 只能看"可疑设备"意味着无法回答
// 「这个设备号到底有没有在档」这种最常见的查证需求。
func (r *Repository) ListDeviceFingerprints(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.DeviceFingerprint, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	idx := 1
	if query.OnlyRisk {
		where = append(where, "risk_tag NOT IN ('normal','trusted')")
	}
	if query.Tag != "" {
		where = append(where, fmt.Sprintf("risk_tag = $%d", idx))
		args = append(args, query.Tag)
		idx++
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(device_id ILIKE $%d OR last_ip ILIKE $%d OR user_agent ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM device_fingerprints "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.DeviceFingerprint{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM device_fingerprints %s
ORDER BY last_seen_at DESC LIMIT $%d OFFSET $%d`, deviceFingerprintColumns, whereClause, idx, idx+1), dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectDeviceFingerprints(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateDeviceRiskTag 更新设备风险标签
func (r *Repository) UpdateDeviceRiskTag(ctx context.Context, id int64, tag, note string) error {
	tag2, err := r.pool.Exec(ctx,
		`UPDATE device_fingerprints SET risk_tag = $2, note = COALESCE(NULLIF($3, ''), note) WHERE id = $1`, id, tag, note)
	if err != nil {
		return err
	}
	if tag2.RowsAffected() == 0 {
		return fmt.Errorf("设备记录不存在")
	}
	return nil
}

// SetDeviceRiskTagByDeviceID 按 device_id 更新标签（复核联动用）
func (r *Repository) SetDeviceRiskTagByDeviceID(ctx context.Context, deviceID, tag string) error {
	_, err := r.pool.Exec(ctx, `UPDATE device_fingerprints SET risk_tag = $2 WHERE device_id = $1`, deviceID, tag)
	return err
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

const ipRiskColumns = `id, ip, risk_tag, risk_score, country, region, isp, asn, source, note,
       is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at`

// UpsertIPRisk 写入情报源返回的 IP 画像。
//
// 关键修正：**空值不覆盖已有值，计数列完全不动**。旧实现无条件用传入结构覆盖，
// 于是一次只填了 IP 与分数的调用会把国家 / 运营商抹成空串、把累计请求数清零。
// 复核拒绝那条路径正是这么用的 —— 复核这个动作本身在销毁证据。
func (r *Repository) UpsertIPRisk(ctx context.Context, rec securitydomain.IPRiskRecord) (*securitydomain.IPRiskRecord, error) {
	riskTag := rec.RiskTag
	if riskTag == "" {
		riskTag = securitydomain.TagNormal
	}
	query := `INSERT INTO ip_risk_records
    (ip, risk_tag, risk_score, country, region, isp, asn, source, note,
     is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 0, 0, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    risk_tag      = EXCLUDED.risk_tag,
    risk_score    = EXCLUDED.risk_score,
    country       = COALESCE(NULLIF(EXCLUDED.country, ''), ip_risk_records.country),
    region        = COALESCE(NULLIF(EXCLUDED.region, ''), ip_risk_records.region),
    isp           = COALESCE(NULLIF(EXCLUDED.isp, ''), ip_risk_records.isp),
    asn           = COALESCE(NULLIF(EXCLUDED.asn, ''), ip_risk_records.asn),
    source        = COALESCE(NULLIF(EXCLUDED.source, ''), ip_risk_records.source),
    note          = COALESCE(NULLIF(EXCLUDED.note, ''), ip_risk_records.note),
    is_proxy      = EXCLUDED.is_proxy,
    is_vpn        = EXCLUDED.is_vpn,
    is_tor        = EXCLUDED.is_tor,
    is_datacenter = EXCLUDED.is_datacenter,
    last_seen_at  = NOW()
RETURNING ` + ipRiskColumns
	return scanIPRiskRecord(r.pool.QueryRow(ctx, query,
		rec.IP, riskTag, rec.RiskScore, rec.Country, rec.Region, rec.ISP, rec.ASN, rec.Source, rec.Note,
		rec.IsProxy, rec.IsVPN, rec.IsTor, rec.IsDatacenter,
	))
}

// TouchIPRiskCounters 累加 IP 的请求 / 拦截计数并补齐归属地。
// 这两个计数列此前从未被写过，「高风险 IP」列表上恒显示 0 —— 管理员因此
// 无从判断一个 IP 是偶发还是持续在打。
func (r *Repository) TouchIPRiskCounters(ctx context.Context, ip string, blocked bool, country, region, isp, asn string) error {
	blockDelta := 0
	if blocked {
		blockDelta = 1
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO ip_risk_records
    (ip, risk_tag, country, region, isp, asn, total_requests, total_blocks, first_seen_at, last_seen_at)
VALUES ($1, 'normal', $2, $3, $4, $5, 1, $6, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    country        = COALESCE(NULLIF(ip_risk_records.country, ''), EXCLUDED.country),
    region         = COALESCE(NULLIF(ip_risk_records.region, ''), EXCLUDED.region),
    isp            = COALESCE(NULLIF(ip_risk_records.isp, ''), EXCLUDED.isp),
    asn            = COALESCE(NULLIF(ip_risk_records.asn, ''), EXCLUDED.asn),
    total_requests = ip_risk_records.total_requests + 1,
    total_blocks   = ip_risk_records.total_blocks + $6,
    last_seen_at   = NOW()`,
		ip, truncateColumn(country, 64), truncateColumn(region, 128), truncateColumn(isp, 128), truncateColumn(asn, 64), blockDelta)
	return err
}

// GetIPRisk 按 IP 查询风险记录
func (r *Repository) GetIPRisk(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	return scanIPRiskRecord(r.pool.QueryRow(ctx, `SELECT `+ipRiskColumns+` FROM ip_risk_records WHERE ip = $1`, ip))
}

// ListIPRiskRecords 分页查询 IP 风险库
func (r *Repository) ListIPRiskRecords(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.IPRiskRecord, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	idx := 1
	if query.OnlyRisk {
		where = append(where, "risk_tag NOT IN ('normal','trusted')")
	}
	if query.Tag != "" {
		where = append(where, fmt.Sprintf("risk_tag = $%d", idx))
		args = append(args, query.Tag)
		idx++
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(ip ILIKE $%d OR country ILIKE $%d OR isp ILIKE $%d OR asn ILIKE $%d)", idx, idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ip_risk_records "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.IPRiskRecord{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM ip_risk_records %s
ORDER BY risk_score DESC, last_seen_at DESC LIMIT $%d OFFSET $%d`, ipRiskColumns, whereClause, idx, idx+1), dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectIPRiskRecords(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateIPRiskTag 人工更新 IP 标签，返回被更新的 IP（供调用方失效缓存）
func (r *Repository) UpdateIPRiskTag(ctx context.Context, id int64, tag, source, note string) (string, error) {
	var ip string
	// 这里刻意不走 normalizeNotFound：ID 不存在时静默返回成功，
	// 控制台会提示"标记已更新"而实际什么都没发生。
	if err := r.pool.QueryRow(ctx, `UPDATE ip_risk_records
SET risk_tag = $2, source = $3, note = COALESCE(NULLIF($4, ''), note), last_seen_at = NOW()
WHERE id = $1 RETURNING ip`, id, tag, source, note).Scan(&ip); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("IP 风险记录不存在")
		}
		return "", err
	}
	return ip, nil
}

// SetIPRiskTag 按 IP 设置标签（复核联动用）。
// 记录不存在时插入一条最小档案，这样「复核拒绝了但库里查不到这个 IP」不会发生。
func (r *Repository) SetIPRiskTag(ctx context.Context, ip, tag, source, note string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO ip_risk_records (ip, risk_tag, source, note, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    risk_tag     = EXCLUDED.risk_tag,
    source       = EXCLUDED.source,
    note         = COALESCE(NULLIF(EXCLUDED.note, ''), ip_risk_records.note),
    last_seen_at = NOW()`, ip, tag, source, note)
	return err
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

const riskActionColumns = `id, scene, min_score, max_score, action, ban_duration, description, is_active, created_at`

// CreateRiskAction 创建自动处置策略
func (r *Repository) CreateRiskAction(ctx context.Context, input securitydomain.CreateRiskActionInput) (*securitydomain.RiskAction, error) {
	query := `INSERT INTO risk_actions (scene, min_score, max_score, action, ban_duration, description, is_active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, TRUE, NOW())
RETURNING ` + riskActionColumns
	return scanRiskAction(r.pool.QueryRow(ctx, query,
		input.Scene, input.MinScore, input.MaxScore, input.Action, input.BanDuration, input.Description))
}

// ListRiskActions 查询处置策略（可按 scene 过滤）。
//
// 注意这里**不再过滤 is_active**：判定侧自己会跳过停用项，
// 而管理端需要看到停用的策略才能把它重新打开。旧实现在带 scene 时
// 隐式只返回启用中的，于是控制台上停用一条策略之后它就从列表里消失了。
func (r *Repository) ListRiskActions(ctx context.Context, scene string) ([]securitydomain.RiskAction, error) {
	query := `SELECT ` + riskActionColumns + ` FROM risk_actions`
	var args []any
	if scene != "" {
		query += ` WHERE scene = $1`
		args = []any{scene}
	}
	query += ` ORDER BY scene ASC, min_score ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskActions(rows)
}

// GetRiskAction 按 ID 查询处置策略
func (r *Repository) GetRiskAction(ctx context.Context, id int64) (*securitydomain.RiskAction, error) {
	return scanRiskAction(r.pool.QueryRow(ctx, `SELECT `+riskActionColumns+` FROM risk_actions WHERE id = $1`, id))
}

// UpdateRiskAction 更新处置策略
func (r *Repository) UpdateRiskAction(ctx context.Context, id int64, input securitydomain.UpdateRiskActionInput) error {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)
	idx := 1
	add := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}
	if input.MinScore != nil {
		add("min_score", *input.MinScore)
	}
	if input.MaxScore != nil {
		add("max_score", *input.MaxScore)
	}
	if input.Action != nil {
		add("action", *input.Action)
	}
	if input.BanDuration != nil {
		add("ban_duration", *input.BanDuration)
	}
	if input.Description != nil {
		add("description", *input.Description)
	}
	if input.IsActive != nil {
		add("is_active", *input.IsActive)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := r.pool.Exec(ctx, fmt.Sprintf("UPDATE risk_actions SET %s WHERE id = $%d", strings.Join(sets, ", "), idx), args...)
	return err
}

// DeleteRiskAction 删除处置策略
func (r *Repository) DeleteRiskAction(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM risk_actions WHERE id = $1`, id)
	return err
}

// ════════════════════════════════════════════════════════════
//  统计大盘
// ════════════════════════════════════════════════════════════

// GetRiskDashboard 组装风控统计大盘。
//
// 全部聚合走同一个时间窗口，且额外查一次**上一等长周期**做对照 ——
// 只给绝对数看不出「今天是不是不对劲」，而那正是大盘唯一的用途。
func (r *Repository) GetRiskDashboard(ctx context.Context, start, end time.Time, bucket string) (*securitydomain.RiskDashboard, error) {
	dash := &securitydomain.RiskDashboard{
		Range: securitydomain.DashboardRange{Start: start, End: end, Bucket: normalizeBucket(bucket)},
	}

	summary, err := r.riskSummary(ctx, start, end)
	if err != nil {
		return nil, err
	}
	// 上一等长周期
	span := end.Sub(start)
	prev, err := r.riskSummary(ctx, start.Add(-span), start)
	if err != nil {
		return nil, err
	}
	summary.PrevTotalAssessments = prev.TotalAssessments
	summary.PrevTotalBlocked = prev.TotalBlocked
	summary.PrevBlockRate = prev.BlockRate
	summary.PrevAvgScore = prev.AvgScore
	dash.Summary = summary

	if dash.Series, err = r.riskSeries(ctx, start, end, dash.Range.Bucket); err != nil {
		return nil, err
	}
	if dash.SceneDistribution, err = r.riskSceneDistribution(ctx, start, end); err != nil {
		return nil, err
	}
	if dash.LevelDistribution, err = r.riskLevelDistribution(ctx, start, end); err != nil {
		return nil, err
	}
	if dash.ActionDistribution, err = r.riskActionDistribution(ctx, start, end); err != nil {
		return nil, err
	}
	if dash.ScoreHistogram, err = r.riskScoreHistogram(ctx, start, end); err != nil {
		return nil, err
	}
	if dash.TopRules, err = r.riskTopRules(ctx, start, end, 10); err != nil {
		return nil, err
	}
	if dash.TopIPs, err = r.riskTopIPs(ctx, start, end, 10); err != nil {
		return nil, err
	}
	if dash.TopDevices, err = r.riskTopDevices(ctx, start, end, 10); err != nil {
		return nil, err
	}
	if dash.TopCountries, err = r.riskTopCountries(ctx, start, end, 10); err != nil {
		return nil, err
	}
	return dash, nil
}

func (r *Repository) riskSummary(ctx context.Context, start, end time.Time) (securitydomain.RiskSummary, error) {
	var s securitydomain.RiskSummary
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE action IN ('block','ban')),
       COUNT(*) FILTER (WHERE action = 'captcha'),
       COUNT(*) FILTER (WHERE action = 'review'),
       COUNT(*) FILTER (WHERE action = 'review' AND NOT reviewed),
       COUNT(*) FILTER (WHERE action = 'pass'),
       COUNT(*) FILTER (WHERE risk_level IN ('high','critical')),
       COALESCE(AVG(total_score), 0),
       COALESCE(MAX(total_score), 0),
       COALESCE(AVG(latency_ms), 0),
       COUNT(DISTINCT ip) FILTER (WHERE ip <> ''),
       COUNT(DISTINCT device_id) FILTER (WHERE device_id <> ''),
       COUNT(DISTINCT account) FILTER (WHERE account <> '')
FROM risk_assessments WHERE created_at BETWEEN $1 AND $2`, start, end).Scan(
		&s.TotalAssessments, &s.TotalBlocked, &s.TotalChallenged, &s.TotalReviews, &s.PendingReviews,
		&s.TotalPassed, &s.HighRiskCount, &s.AvgScore, &s.MaxScore, &s.AvgLatencyMS,
		&s.DistinctIPs, &s.DistinctDevices, &s.DistinctAccounts,
	)
	if err != nil {
		return s, err
	}
	if s.TotalAssessments > 0 {
		s.BlockRate = float64(s.TotalBlocked) / float64(s.TotalAssessments)
	}
	return s, nil
}

// riskSeries 时间序列。用 generate_series 左连接补齐空桶 ——
// 缺桶的折线图会把「那几个小时没有任何请求」画成一条直连的斜线，
// 看上去像是流量平滑，实际是数据缺失。
func (r *Repository) riskSeries(ctx context.Context, start, end time.Time, bucket string) ([]securitydomain.RiskSeriesPoint, error) {
	unit := normalizeBucket(bucket)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
SELECT b.slot,
       COALESCE(d.total, 0), COALESCE(d.normal, 0), COALESCE(d.low, 0), COALESCE(d.medium, 0),
       COALESCE(d.high, 0), COALESCE(d.critical, 0),
       COALESCE(d.pass, 0), COALESCE(d.captcha, 0), COALESCE(d.review, 0), COALESCE(d.block, 0), COALESCE(d.ban, 0),
       COALESCE(d.avg_score, 0)
FROM generate_series(date_trunc('%[1]s', $1::timestamptz), date_trunc('%[1]s', $2::timestamptz), interval '1 %[1]s') AS b(slot)
LEFT JOIN (
    SELECT date_trunc('%[1]s', created_at) AS slot,
           COUNT(*) AS total,
           COUNT(*) FILTER (WHERE risk_level = 'normal')   AS normal,
           COUNT(*) FILTER (WHERE risk_level = 'low')      AS low,
           COUNT(*) FILTER (WHERE risk_level = 'medium')   AS medium,
           COUNT(*) FILTER (WHERE risk_level = 'high')     AS high,
           COUNT(*) FILTER (WHERE risk_level = 'critical') AS critical,
           COUNT(*) FILTER (WHERE action = 'pass')    AS pass,
           COUNT(*) FILTER (WHERE action = 'captcha') AS captcha,
           COUNT(*) FILTER (WHERE action = 'review')  AS review,
           COUNT(*) FILTER (WHERE action = 'block')   AS block,
           COUNT(*) FILTER (WHERE action = 'ban')     AS ban,
           AVG(total_score) AS avg_score
    FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 GROUP BY 1
) AS d ON d.slot = b.slot
ORDER BY b.slot ASC`, unit), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]securitydomain.RiskSeriesPoint, 0, 128)
	for rows.Next() {
		var p securitydomain.RiskSeriesPoint
		if err := rows.Scan(&p.Time, &p.Total, &p.Normal, &p.Low, &p.Medium, &p.High, &p.Critical,
			&p.Pass, &p.Captcha, &p.Review, &p.Block, &p.Ban, &p.AvgScore); err != nil {
			return nil, err
		}
		if p.Total > 0 {
			p.BlockRate = float64(p.Block+p.Ban) / float64(p.Total)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (r *Repository) riskSceneDistribution(ctx context.Context, start, end time.Time) ([]securitydomain.SceneStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT scene, COUNT(*), COUNT(*) FILTER (WHERE action IN ('block','ban')), COALESCE(AVG(total_score), 0)
FROM risk_assessments WHERE created_at BETWEEN $1 AND $2
GROUP BY scene ORDER BY COUNT(*) DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]securitydomain.SceneStat, 0, 8)
	for rows.Next() {
		var s securitydomain.SceneStat
		if err := rows.Scan(&s.Scene, &s.Count, &s.Blocked, &s.AvgScore); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// riskLevelDistribution 等级分布。五个等级恒定输出（没有的补 0）——
// 饼图缺一块和那一块为零是两回事，前者会让人以为该等级不存在。
func (r *Repository) riskLevelDistribution(ctx context.Context, start, end time.Time) ([]securitydomain.LevelStat, error) {
	counts := map[string]int64{}
	rows, err := r.pool.Query(ctx, `SELECT risk_level, COUNT(*) FROM risk_assessments
WHERE created_at BETWEEN $1 AND $2 GROUP BY risk_level`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var level string
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			return nil, err
		}
		counts[level] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]securitydomain.LevelStat, 0, len(securitydomain.RiskLevelBands))
	for _, band := range securitydomain.RiskLevelBands {
		items = append(items, securitydomain.LevelStat{Level: band.Level, Count: counts[band.Level]})
	}
	return items, nil
}

func (r *Repository) riskActionDistribution(ctx context.Context, start, end time.Time) ([]securitydomain.ActionStat, error) {
	counts := map[string]int64{}
	rows, err := r.pool.Query(ctx, `SELECT action, COUNT(*) FROM risk_assessments
WHERE created_at BETWEEN $1 AND $2 GROUP BY action`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		counts[action] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]securitydomain.ActionStat, 0, 5)
	for _, action := range securitydomain.ActionValues() {
		items = append(items, securitydomain.ActionStat{Action: action, Count: counts[action]})
	}
	return items, nil
}

// riskScoreHistogram 分数直方图，桶边界与等级分档一致。
// 用独立的分桶宽度会让「直方图上的峰在中危区，可饼图说高危更多」这类矛盾出现。
func (r *Repository) riskScoreHistogram(ctx context.Context, start, end time.Time) ([]securitydomain.ScoreBucket, error) {
	buckets := make([]securitydomain.ScoreBucket, 0, len(securitydomain.RiskLevelBands))
	for _, band := range securitydomain.RiskLevelBands {
		bucket := securitydomain.ScoreBucket{Min: band.MinScore, Level: band.Level, Max: 1 << 20}
		if band.MaxScore != nil {
			bucket.Max = *band.MaxScore
		}
		var count int64
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments
WHERE created_at BETWEEN $1 AND $2 AND total_score >= $3 AND total_score <= $4`,
			start, end, bucket.Min, bucket.Max).Scan(&count); err != nil {
			return nil, err
		}
		bucket.Count = count
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// riskTopRules 命中最多的规则。
// 展开 matched_rules 数组做聚合：规则可能已被删除，因此规则名取留痕里的快照，
// 场景等元信息用 LEFT JOIN 补 —— 删掉一条规则不该让它过去的战绩一起消失。
func (r *Repository) riskTopRules(ctx context.Context, start, end time.Time, limit int) ([]securitydomain.RuleHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT (m.rule ->> 'ruleId')::bigint AS rule_id,
       COALESCE(MAX(m.rule ->> 'ruleName'), '') AS rule_name,
       COALESCE(MAX(r.scene), '') AS scene,
       COALESCE(MAX(m.rule ->> 'conditionType'), COALESCE(MAX(r.condition_type), '')) AS condition_type,
       COALESCE(MAX((m.rule ->> 'score')::int), 0) AS score,
       COALESCE(bool_or(r.is_active), FALSE) AS is_active,
       COUNT(*) AS hits,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(SUM((m.rule ->> 'score')::int), 0) AS score_sum,
       MAX(a.created_at) AS last_hit_at
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(`+matchedRulesArray+`) AS m(rule)
LEFT JOIN risk_rules r ON r.id = (m.rule ->> 'ruleId')::bigint
WHERE a.created_at BETWEEN $1 AND $2
  -- 元素里没有 ruleId 时 GROUP BY 会得到一个 NULL 分组，Scan 进 int64 直接报错。
  -- 留痕是历史数据，形状不能假定，只能显式排除。
  AND (m.rule ->> 'ruleId') IS NOT NULL
GROUP BY 1 ORDER BY hits DESC LIMIT $3`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.RuleHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.RuleHitStat
		if err := rows.Scan(&s.RuleID, &s.RuleName, &s.Scene, &s.ConditionType, &s.Score,
			&s.IsActive, &s.Hits, &s.Blocked, &s.ScoreSum, &s.LastHitAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopIPs(ctx context.Context, start, end time.Time, limit int) ([]securitydomain.IPHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.ip,
       COALESCE(MAX(a.country), '') AS country,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(MAX(a.total_score), 0) AS max_score,
       COALESCE(AVG(a.total_score), 0) AS avg_score,
       COALESCE(MAX(ir.risk_tag), 'normal') AS risk_tag
FROM risk_assessments a
LEFT JOIN ip_risk_records ir ON ir.ip = a.ip
WHERE a.created_at BETWEEN $1 AND $2 AND a.ip <> ''
GROUP BY a.ip ORDER BY total DESC LIMIT $3`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.IPHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.IPHitStat
		if err := rows.Scan(&s.IP, &s.Country, &s.Count, &s.Blocked, &s.MaxScore, &s.AvgScore, &s.RiskTag); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopDevices(ctx context.Context, start, end time.Time, limit int) ([]securitydomain.DeviceHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.device_id,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(MAX(a.total_score), 0) AS max_score,
       COALESCE(AVG(a.total_score), 0) AS avg_score,
       COALESCE(MAX(df.risk_tag), 'normal') AS risk_tag,
       COUNT(DISTINCT a.account) FILTER (WHERE a.account <> '') AS accounts
FROM risk_assessments a
LEFT JOIN device_fingerprints df ON df.device_id = a.device_id
WHERE a.created_at BETWEEN $1 AND $2 AND a.device_id <> ''
GROUP BY a.device_id ORDER BY total DESC LIMIT $3`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.DeviceHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.DeviceHitStat
		if err := rows.Scan(&s.DeviceID, &s.Count, &s.Blocked, &s.MaxScore, &s.AvgScore, &s.RiskTag, &s.Accounts); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopCountries(ctx context.Context, start, end time.Time, limit int) ([]securitydomain.CountryStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT country, COUNT(*), COUNT(*) FILTER (WHERE action IN ('block','ban'))
FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 AND country <> ''
GROUP BY country ORDER BY COUNT(*) DESC LIMIT $3`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.CountryStat, 0, limit)
	for rows.Next() {
		var s securitydomain.CountryStat
		if err := rows.Scan(&s.Country, &s.Count, &s.Blocked); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  scan 辅助
// ════════════════════════════════════════════════════════════

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}

// truncateColumn 按 rune 截断，避免超长输入撞上 VARCHAR 上限报错。
// UA 与账号都可能被恶意构造成超长串，让它把一次评估的留痕整条写失败并不划算。
func truncateColumn(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type rowScanner interface{ Scan(dest ...any) error }

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanRiskRule(row rowScanner) (*securitydomain.RiskRule, error) {
	var item securitydomain.RiskRule
	var condRaw []byte
	if err := row.Scan(
		&item.ID, &item.Name, &item.Description, &item.Scene,
		&item.ConditionType, &condRaw, &item.Score, &item.IsActive, &item.Priority,
		&item.HitCount, &item.LastHitAt, &item.CreatedBy, &item.UpdatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(condRaw) > 0 {
		_ = json.Unmarshal(condRaw, &item.ConditionData)
	}
	if item.ConditionData == nil {
		item.ConditionData = make(map[string]any)
	}
	return &item, nil
}

func collectRiskRules(rows rowsScanner) ([]securitydomain.RiskRule, error) {
	items := make([]securitydomain.RiskRule, 0, 32)
	for rows.Next() {
		item, err := scanRiskRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAssessment(row rowScanner) (*securitydomain.RiskAssessment, error) {
	var item securitydomain.RiskAssessment
	var rulesRaw, contextRaw []byte
	if err := row.Scan(
		&item.ID, &item.Scene, &item.AppID, &item.UserID, &item.IdentityID, &item.Account,
		&item.IP, &item.DeviceID, &item.UserAgent, &item.Country,
		&item.TotalScore, &item.RiskLevel, &rulesRaw, &contextRaw, &item.LatencyMS,
		&item.Action, &item.ActionDetail,
		&item.Reviewed, &item.ReviewerID, &item.ReviewerName, &item.ReviewResult,
		&item.ReviewComment, &item.ReviewedAt, &item.CreatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(rulesRaw) > 0 {
		_ = json.Unmarshal(rulesRaw, &item.MatchedRules)
	}
	if item.MatchedRules == nil {
		item.MatchedRules = []securitydomain.MatchedRule{}
	}
	if len(contextRaw) > 0 {
		_ = json.Unmarshal(contextRaw, &item.EvalContext)
	}
	return &item, nil
}

func collectRiskAssessments(rows rowsScanner) ([]securitydomain.RiskAssessment, error) {
	items := make([]securitydomain.RiskAssessment, 0, 32)
	for rows.Next() {
		item, err := scanRiskAssessment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanDeviceFingerprint(row rowScanner) (*securitydomain.DeviceFingerprint, error) {
	var item securitydomain.DeviceFingerprint
	var fpRaw []byte
	if err := row.Scan(
		&item.ID, &item.DeviceID, &item.UserID, &item.AppID, &fpRaw, &item.RiskTag,
		&item.LastIP, &item.UserAgent, &item.Note,
		&item.FirstSeenAt, &item.LastSeenAt, &item.SeenCount,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(fpRaw) > 0 {
		_ = json.Unmarshal(fpRaw, &item.Fingerprint)
	}
	if item.Fingerprint == nil {
		item.Fingerprint = make(map[string]any)
	}
	return &item, nil
}

func collectDeviceFingerprints(rows rowsScanner) ([]securitydomain.DeviceFingerprint, error) {
	items := make([]securitydomain.DeviceFingerprint, 0, 32)
	for rows.Next() {
		item, err := scanDeviceFingerprint(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanIPRiskRecord(row rowScanner) (*securitydomain.IPRiskRecord, error) {
	var item securitydomain.IPRiskRecord
	if err := row.Scan(
		&item.ID, &item.IP, &item.RiskTag, &item.RiskScore,
		&item.Country, &item.Region, &item.ISP, &item.ASN, &item.Source, &item.Note,
		&item.IsProxy, &item.IsVPN, &item.IsTor, &item.IsDatacenter,
		&item.TotalRequests, &item.TotalBlocks, &item.FirstSeenAt, &item.LastSeenAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func collectIPRiskRecords(rows rowsScanner) ([]securitydomain.IPRiskRecord, error) {
	items := make([]securitydomain.IPRiskRecord, 0, 32)
	for rows.Next() {
		item, err := scanIPRiskRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAction(row rowScanner) (*securitydomain.RiskAction, error) {
	var item securitydomain.RiskAction
	if err := row.Scan(
		&item.ID, &item.Scene, &item.MinScore, &item.MaxScore,
		&item.Action, &item.BanDuration, &item.Description,
		&item.IsActive, &item.CreatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func collectRiskActions(rows rowsScanner) ([]securitydomain.RiskAction, error) {
	items := make([]securitydomain.RiskAction, 0, 16)
	for rows.Next() {
		item, err := scanRiskAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
