package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
)

// ════════════════════════════════════════════════════════════
//  风险规则 CRUD
// ════════════════════════════════════════════════════════════

// CreateRiskRule 创建风险规则
func (r *Repository) CreateRiskRule(ctx context.Context, input securitydomain.CreateRiskRuleInput, createdBy int64) (*securitydomain.RiskRule, error) {
	condJSON, err := json.Marshal(input.ConditionData)
	if err != nil {
		return nil, fmt.Errorf("marshal condition_data: %w", err)
	}
	query := `INSERT INTO risk_rules (name, description, scene, condition_type, condition_data, score, priority, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
RETURNING id, name, description, scene, condition_type, condition_data, score, is_active, priority, created_by, created_at, updated_at`
	return scanRiskRule(r.pool.QueryRow(ctx, query,
		input.Name, input.Description, input.Scene, input.ConditionType,
		condJSON, input.Score, input.Priority, createdBy,
	))
}

// ListRiskRules 列出风险规则（可按 scene 过滤）
func (r *Repository) ListRiskRules(ctx context.Context, scene string) ([]securitydomain.RiskRule, error) {
	var query string
	var args []any
	if scene != "" {
		query = `SELECT id, name, description, scene, condition_type, condition_data, score, is_active, priority, created_by, created_at, updated_at
FROM risk_rules WHERE scene = $1 ORDER BY priority ASC, id ASC`
		args = []any{scene}
	} else {
		query = `SELECT id, name, description, scene, condition_type, condition_data, score, is_active, priority, created_by, created_at, updated_at
FROM risk_rules ORDER BY priority ASC, id ASC`
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// UpdateRiskRule 更新风险规则（可选字段）
func (r *Repository) UpdateRiskRule(ctx context.Context, id int64, input securitydomain.UpdateRiskRuleInput) error {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)
	idx := 1
	if input.Name != nil {
		sets = append(sets, fmt.Sprintf("name = $%d", idx))
		args = append(args, *input.Name)
		idx++
	}
	if input.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", idx))
		args = append(args, *input.Description)
		idx++
	}
	if input.ConditionData != nil {
		condJSON, err := json.Marshal(*input.ConditionData)
		if err != nil {
			return fmt.Errorf("marshal condition_data: %w", err)
		}
		sets = append(sets, fmt.Sprintf("condition_data = $%d", idx))
		args = append(args, condJSON)
		idx++
	}
	if input.Score != nil {
		sets = append(sets, fmt.Sprintf("score = $%d", idx))
		args = append(args, *input.Score)
		idx++
	}
	if input.IsActive != nil {
		sets = append(sets, fmt.Sprintf("is_active = $%d", idx))
		args = append(args, *input.IsActive)
		idx++
	}
	if input.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", idx))
		args = append(args, *input.Priority)
		idx++
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
	query := `SELECT id, name, description, scene, condition_type, condition_data, score, is_active, priority, created_by, created_at, updated_at
FROM risk_rules WHERE scene = $1 AND is_active = TRUE ORDER BY priority ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, scene)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// ════════════════════════════════════════════════════════════
//  评估记录
// ════════════════════════════════════════════════════════════

// CreateRiskAssessment 创建风险评估记录
func (r *Repository) CreateRiskAssessment(ctx context.Context, a securitydomain.RiskAssessment) (*securitydomain.RiskAssessment, error) {
	rulesJSON, err := json.Marshal(a.MatchedRules)
	if err != nil {
		return nil, fmt.Errorf("marshal matched_rules: %w", err)
	}
	query := `INSERT INTO risk_assessments (scene, app_id, user_id, identity_id, ip, device_id, total_score, risk_level, matched_rules, action, action_detail, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
RETURNING id, scene, app_id, user_id, identity_id, ip, device_id, total_score, risk_level, matched_rules, action, action_detail, reviewed, reviewer_id, review_result, review_comment, reviewed_at, created_at`
	return scanRiskAssessment(r.pool.QueryRow(ctx, query,
		a.Scene, a.AppID, a.UserID, a.IdentityID,
		a.IP, a.DeviceID, a.TotalScore, a.RiskLevel,
		rulesJSON, a.Action, a.ActionDetail,
	))
}

// ListRiskAssessments 分页查询评估记录（可选过滤）
func (r *Repository) ListRiskAssessments(ctx context.Context, scene, riskLevel, action string, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}

	where := make([]string, 0, 3)
	args := make([]any, 0, 5)
	idx := 1
	if scene != "" {
		where = append(where, fmt.Sprintf("scene = $%d", idx))
		args = append(args, scene)
		idx++
	}
	if riskLevel != "" {
		where = append(where, fmt.Sprintf("risk_level = $%d", idx))
		args = append(args, riskLevel)
		idx++
	}
	if action != "" {
		where = append(where, fmt.Sprintf("action = $%d", idx))
		args = append(args, action)
		idx++
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// 计数
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM risk_assessments %s", whereClause)
	var total int64
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	args = append(args, limit, offset)
	dataQuery := fmt.Sprintf(`SELECT id, scene, app_id, user_id, identity_id, ip, device_id, total_score, risk_level, matched_rules, action, action_detail, reviewed, reviewer_id, review_result, review_comment, reviewed_at, created_at
FROM risk_assessments %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1)

	rows, err := r.pool.Query(ctx, dataQuery, args...)
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
	query := `SELECT id, scene, app_id, user_id, identity_id, ip, device_id, total_score, risk_level, matched_rules, action, action_detail, reviewed, reviewer_id, review_result, review_comment, reviewed_at, created_at
FROM risk_assessments WHERE id = $1`
	return scanRiskAssessment(r.pool.QueryRow(ctx, query, id))
}

// ListPendingReviews 分页查询待复核记录
func (r *Repository) ListPendingReviews(ctx context.Context, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments WHERE action = 'review' AND NOT reviewed`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	query := `SELECT id, scene, app_id, user_id, identity_id, ip, device_id, total_score, risk_level, matched_rules, action, action_detail, reviewed, reviewer_id, review_result, review_comment, reviewed_at, created_at
FROM risk_assessments WHERE action = 'review' AND NOT reviewed ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, query, limit, offset)
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

// ════════════════════════════════════════════════════════════
//  复核
// ════════════════════════════════════════════════════════════

// ReviewRiskAssessment 复核评估记录
func (r *Repository) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, result, comment string) error {
	query := `UPDATE risk_assessments
SET reviewed = TRUE, reviewer_id = $2, review_result = $3, review_comment = $4, reviewed_at = NOW()
WHERE id = $1 AND NOT reviewed`
	tag, err := r.pool.Exec(ctx, query, id, reviewerID, result, comment)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("评估记录不存在或已复核")
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

// UpsertDeviceFingerprint 插入或更新设备指纹
func (r *Repository) UpsertDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	fpJSON, err := json.Marshal(fp.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("marshal fingerprint: %w", err)
	}
	query := `INSERT INTO device_fingerprints (device_id, user_id, app_id, fingerprint, risk_tag, first_seen_at, last_seen_at, seen_count)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW(), 1)
ON CONFLICT (device_id) DO UPDATE SET
    fingerprint = EXCLUDED.fingerprint,
    last_seen_at = NOW(),
    seen_count = device_fingerprints.seen_count + 1
RETURNING id, device_id, user_id, app_id, fingerprint, risk_tag, first_seen_at, last_seen_at, seen_count`
	return scanDeviceFingerprint(r.pool.QueryRow(ctx, query,
		fp.DeviceID, fp.UserID, fp.AppID, fpJSON, fp.RiskTag,
	))
}

// GetDeviceFingerprint 按 device_id 查询设备指纹
func (r *Repository) GetDeviceFingerprint(ctx context.Context, deviceID string) (*securitydomain.DeviceFingerprint, error) {
	query := `SELECT id, device_id, user_id, app_id, fingerprint, risk_tag, first_seen_at, last_seen_at, seen_count
FROM device_fingerprints WHERE device_id = $1`
	return scanDeviceFingerprint(r.pool.QueryRow(ctx, query, deviceID))
}

// ListSuspiciousDevices 分页查询可疑设备
func (r *Repository) ListSuspiciousDevices(ctx context.Context, page, limit int) ([]securitydomain.DeviceFingerprint, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_fingerprints WHERE risk_tag != 'normal'`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	rows, err := r.pool.Query(ctx, `SELECT id, device_id, user_id, app_id, fingerprint, risk_tag, first_seen_at, last_seen_at, seen_count
FROM device_fingerprints WHERE risk_tag != 'normal' ORDER BY last_seen_at DESC LIMIT $1 OFFSET $2`, limit, offset)
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
func (r *Repository) UpdateDeviceRiskTag(ctx context.Context, id int64, tag string) error {
	_, err := r.pool.Exec(ctx, `UPDATE device_fingerprints SET risk_tag = $2 WHERE id = $1`, id, tag)
	return err
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

// UpsertIPRisk 插入或更新 IP 风险记录
func (r *Repository) UpsertIPRisk(ctx context.Context, rec securitydomain.IPRiskRecord) (*securitydomain.IPRiskRecord, error) {
	query := `INSERT INTO ip_risk_records (ip, risk_tag, risk_score, country, region, isp, is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    risk_tag = EXCLUDED.risk_tag,
    risk_score = EXCLUDED.risk_score,
    country = EXCLUDED.country,
    region = EXCLUDED.region,
    isp = EXCLUDED.isp,
    is_proxy = EXCLUDED.is_proxy,
    is_vpn = EXCLUDED.is_vpn,
    is_tor = EXCLUDED.is_tor,
    is_datacenter = EXCLUDED.is_datacenter,
    total_requests = EXCLUDED.total_requests,
    total_blocks = EXCLUDED.total_blocks,
    last_seen_at = NOW()
RETURNING id, ip, risk_tag, risk_score, country, region, isp, is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at`
	return scanIPRiskRecord(r.pool.QueryRow(ctx, query,
		rec.IP, rec.RiskTag, rec.RiskScore, rec.Country, rec.Region, rec.ISP,
		rec.IsProxy, rec.IsVPN, rec.IsTor, rec.IsDatacenter,
		rec.TotalRequests, rec.TotalBlocks,
	))
}

// GetIPRisk 按 IP 查询风险记录
func (r *Repository) GetIPRisk(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	query := `SELECT id, ip, risk_tag, risk_score, country, region, isp, is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at
FROM ip_risk_records WHERE ip = $1`
	return scanIPRiskRecord(r.pool.QueryRow(ctx, query, ip))
}

// ListHighRiskIPs 分页查询高风险 IP
func (r *Repository) ListHighRiskIPs(ctx context.Context, page, limit int) ([]securitydomain.IPRiskRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 20
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM ip_risk_records WHERE risk_tag != 'normal'`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	rows, err := r.pool.Query(ctx, `SELECT id, ip, risk_tag, risk_score, country, region, isp, is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at
FROM ip_risk_records WHERE risk_tag != 'normal' ORDER BY last_seen_at DESC LIMIT $1 OFFSET $2`, limit, offset)
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

// UpdateIPRiskTag 更新 IP 风险标签
func (r *Repository) UpdateIPRiskTag(ctx context.Context, id int64, tag string) error {
	_, err := r.pool.Exec(ctx, `UPDATE ip_risk_records SET risk_tag = $2 WHERE id = $1`, id, tag)
	return err
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

// CreateRiskAction 创建自动处置策略
func (r *Repository) CreateRiskAction(ctx context.Context, input securitydomain.CreateRiskActionInput) (*securitydomain.RiskAction, error) {
	query := `INSERT INTO risk_actions (scene, min_score, max_score, action, ban_duration, description, is_active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, TRUE, NOW())
RETURNING id, scene, min_score, max_score, action, ban_duration, description, is_active, created_at`
	return scanRiskAction(r.pool.QueryRow(ctx, query,
		input.Scene, input.MinScore, input.MaxScore,
		input.Action, input.BanDuration, input.Description,
	))
}

// ListRiskActions 查询处置策略（可按 scene 过滤）
func (r *Repository) ListRiskActions(ctx context.Context, scene string) ([]securitydomain.RiskAction, error) {
	var query string
	var args []any
	if scene != "" {
		query = `SELECT id, scene, min_score, max_score, action, ban_duration, description, is_active, created_at
FROM risk_actions WHERE scene = $1 AND is_active = TRUE ORDER BY min_score ASC`
		args = []any{scene}
	} else {
		query = `SELECT id, scene, min_score, max_score, action, ban_duration, description, is_active, created_at
FROM risk_actions ORDER BY scene ASC, min_score ASC`
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskActions(rows)
}

// UpdateRiskAction 更新处置策略启用状态
func (r *Repository) UpdateRiskAction(ctx context.Context, id int64, isActive bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE risk_actions SET is_active = $2 WHERE id = $1`, id, isActive)
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

// GetRiskDashboard 获取风控统计大盘
func (r *Repository) GetRiskDashboard(ctx context.Context, start, end time.Time) (*securitydomain.RiskDashboard, error) {
	dash := &securitydomain.RiskDashboard{}

	// 总评估数
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2`, start, end).Scan(&dash.TotalAssessments); err != nil {
		return nil, err
	}
	// 总拦截数
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 AND action IN ('block','ban')`, start, end).Scan(&dash.TotalBlocked); err != nil {
		return nil, err
	}
	// 总复核数
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 AND action = 'review'`, start, end).Scan(&dash.TotalReviews); err != nil {
		return nil, err
	}
	// 待复核数
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 AND action = 'review' AND NOT reviewed`, start, end).Scan(&dash.PendingReviews); err != nil {
		return nil, err
	}

	// 场景分布
	sceneRows, err := r.pool.Query(ctx, `SELECT scene, COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 GROUP BY scene ORDER BY COUNT(*) DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer sceneRows.Close()
	for sceneRows.Next() {
		var s securitydomain.SceneStat
		if err := sceneRows.Scan(&s.Scene, &s.Count); err != nil {
			return nil, err
		}
		dash.SceneDistribution = append(dash.SceneDistribution, s)
	}
	if err := sceneRows.Err(); err != nil {
		return nil, err
	}

	// 风险等级分布
	levelRows, err := r.pool.Query(ctx, `SELECT risk_level, COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 GROUP BY risk_level ORDER BY COUNT(*) DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer levelRows.Close()
	for levelRows.Next() {
		var l securitydomain.LevelStat
		if err := levelRows.Scan(&l.Level, &l.Count); err != nil {
			return nil, err
		}
		dash.LevelDistribution = append(dash.LevelDistribution, l)
	}
	if err := levelRows.Err(); err != nil {
		return nil, err
	}

	// 处置动作分布
	actionRows, err := r.pool.Query(ctx, `SELECT action, COUNT(*) FROM risk_assessments WHERE created_at BETWEEN $1 AND $2 GROUP BY action ORDER BY COUNT(*) DESC`, start, end)
	if err != nil {
		return nil, err
	}
	defer actionRows.Close()
	for actionRows.Next() {
		var a securitydomain.ActionStat
		if err := actionRows.Scan(&a.Action, &a.Count); err != nil {
			return nil, err
		}
		dash.ActionDistribution = append(dash.ActionDistribution, a)
	}
	if err := actionRows.Err(); err != nil {
		return nil, err
	}

	return dash, nil
}

// ════════════════════════════════════════════════════════════
//  scan 辅助函数
// ════════════════════════════════════════════════════════════

func scanRiskRule(row interface{ Scan(dest ...any) error }) (*securitydomain.RiskRule, error) {
	var item securitydomain.RiskRule
	var condRaw []byte
	if err := row.Scan(
		&item.ID, &item.Name, &item.Description, &item.Scene,
		&item.ConditionType, &condRaw, &item.Score, &item.IsActive,
		&item.Priority, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
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

func collectRiskRules(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]securitydomain.RiskRule, error) {
	var items []securitydomain.RiskRule
	for rows.Next() {
		item, err := scanRiskRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAssessment(row interface{ Scan(dest ...any) error }) (*securitydomain.RiskAssessment, error) {
	var item securitydomain.RiskAssessment
	var rulesRaw []byte
	if err := row.Scan(
		&item.ID, &item.Scene, &item.AppID, &item.UserID, &item.IdentityID,
		&item.IP, &item.DeviceID, &item.TotalScore, &item.RiskLevel,
		&rulesRaw, &item.Action, &item.ActionDetail,
		&item.Reviewed, &item.ReviewerID, &item.ReviewResult,
		&item.ReviewComment, &item.ReviewedAt, &item.CreatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(rulesRaw) > 0 {
		_ = json.Unmarshal(rulesRaw, &item.MatchedRules)
	}
	return &item, nil
}

func collectRiskAssessments(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]securitydomain.RiskAssessment, error) {
	var items []securitydomain.RiskAssessment
	for rows.Next() {
		item, err := scanRiskAssessment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanDeviceFingerprint(row interface{ Scan(dest ...any) error }) (*securitydomain.DeviceFingerprint, error) {
	var item securitydomain.DeviceFingerprint
	var fpRaw []byte
	if err := row.Scan(
		&item.ID, &item.DeviceID, &item.UserID, &item.AppID,
		&fpRaw, &item.RiskTag, &item.FirstSeenAt, &item.LastSeenAt, &item.SeenCount,
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

func collectDeviceFingerprints(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]securitydomain.DeviceFingerprint, error) {
	var items []securitydomain.DeviceFingerprint
	for rows.Next() {
		item, err := scanDeviceFingerprint(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanIPRiskRecord(row interface{ Scan(dest ...any) error }) (*securitydomain.IPRiskRecord, error) {
	var item securitydomain.IPRiskRecord
	if err := row.Scan(
		&item.ID, &item.IP, &item.RiskTag, &item.RiskScore,
		&item.Country, &item.Region, &item.ISP,
		&item.IsProxy, &item.IsVPN, &item.IsTor, &item.IsDatacenter,
		&item.TotalRequests, &item.TotalBlocks,
		&item.FirstSeenAt, &item.LastSeenAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func collectIPRiskRecords(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]securitydomain.IPRiskRecord, error) {
	var items []securitydomain.IPRiskRecord
	for rows.Next() {
		item, err := scanIPRiskRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAction(row interface{ Scan(dest ...any) error }) (*securitydomain.RiskAction, error) {
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

func collectRiskActions(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]securitydomain.RiskAction, error) {
	var items []securitydomain.RiskAction
	for rows.Next() {
		item, err := scanRiskAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
