package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	securitydomain "aegis/internal/domain/security"
)

// 风控留痕的批量写入（service/risk_trail.go 的消费者与直接写库的退路共用）。
//
// 一批留痕在**一个事务**里写：先把事件 ID 插进 risk_trail_events（插得进去的才是第一次见到的），只对这些写评估记录
// （COPY）、加规则命中数与名单命中数（按 ID 合并成一条 UPDATE）、加 IP 计数与设备指纹（按键合并成一条 UPSERT）。
// 消费者在提交之后才确认消息：提交前崩溃，整批回滚、消息重投；提交后确认前崩溃，重投的那批在去重表里全部撞上，
// 什么都不做。所以每条留痕恰好写一次、计数恰好加一次。
// 多个消费者同时写时，各张表都按键排序更新（规则 → 名单 → IP → 设备），不会互相死锁。

// RiskTrailRecord 一条风控评估的留痕。
type RiskTrailRecord struct {
	// EventID 留痕的唯一 ID（UUID），重投去重用
	EventID    string                        `json:"id"`
	At         time.Time                     `json:"at"`
	Assessment securitydomain.RiskAssessment `json:"assessment"`
	// RuleIDs / ListIDs 命中的规则与名单项（各加一次命中数）
	RuleIDs []int64 `json:"rules,omitempty"`
	ListIDs []int64 `json:"lists,omitempty"`
	// 归属地与运营商（IP 计数第一次见到这个 IP 时记下）
	Country string `json:"country,omitempty"`
	Region  string `json:"region,omitempty"`
	ISP     string `json:"isp,omitempty"`
	ASN     string `json:"asn,omitempty"`
	// Fingerprint 设备指纹（有设备号时）
	Fingerprint map[string]any `json:"fingerprint,omitempty"`
}

var riskAssessmentCopyColumns = []string{
	"scene", "app_id", "user_id", "identity_id", "account", "ip", "device_id", "user_agent", "country",
	"total_score", "risk_level", "matched_rules", "eval_context", "latency_ms", "action", "action_detail",
	"dimension_scores", "list_hits", "decision_by", "enforcement", "created_at",
}

// WriteRiskTrail 在一个事务里写一批留痕，返回第一次见到（真正写进去）的事件 ID。
func (r *Repository) WriteRiskTrail(ctx context.Context, records []RiskTrailRecord) (map[string]bool, error) {
	fresh := map[string]bool{}
	if len(records) == 0 {
		return fresh, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ids := make([]string, len(records))
	for index, record := range records {
		ids[index] = record.EventID
	}
	rows, err := tx.Query(ctx, `INSERT INTO risk_trail_events (event_id)
SELECT DISTINCT unnest($1::uuid[]) ON CONFLICT DO NOTHING RETURNING event_id::text`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		fresh[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(fresh) == 0 {
		return fresh, tx.Commit(ctx)
	}

	copyRows := make([][]any, 0, len(fresh))
	ruleHits, listHits := map[int64]int{}, map[int64]int{}
	ips := map[string]*ipRiskTouch{}
	devices := map[string]*deviceTouch{}
	seen := map[string]bool{}
	for _, record := range records {
		if !fresh[record.EventID] || seen[record.EventID] {
			continue
		}
		seen[record.EventID] = true
		row, err := riskAssessmentCopyRow(record)
		if err != nil {
			return nil, err
		}
		copyRows = append(copyRows, row)
		for _, id := range record.RuleIDs {
			ruleHits[id]++
		}
		for _, id := range record.ListIDs {
			listHits[id]++
		}
		a := record.Assessment
		if a.IP != "" {
			touch := ips[a.IP]
			if touch == nil {
				touch = &ipRiskTouch{ip: a.IP}
				ips[a.IP] = touch
			}
			touch.requests++
			if securitydomain.IsRejection(a.Action) {
				touch.blocks++
			}
			touch.country = firstNonEmptyString(touch.country, record.Country)
			touch.region = firstNonEmptyString(touch.region, record.Region)
			touch.isp = firstNonEmptyString(touch.isp, record.ISP)
			touch.asn = firstNonEmptyString(touch.asn, record.ASN)
		}
		if a.DeviceID != "" {
			touch := devices[a.DeviceID]
			if touch == nil {
				touch = &deviceTouch{}
				devices[a.DeviceID] = touch
			}
			touch.seen++
			// 指纹取这一批里最后一次的（与逐条 UPSERT 的结果相同）
			touch.fp = securitydomain.DeviceFingerprint{DeviceID: a.DeviceID, UserID: a.UserID, AppID: a.AppID,
				LastIP: a.IP, UserAgent: a.UserAgent, Fingerprint: record.Fingerprint}
		}
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"risk_assessments"}, riskAssessmentCopyColumns, pgx.CopyFromRows(copyRows)); err != nil {
		return nil, err
	}
	if err := addCountsTx(ctx, tx, `UPDATE risk_rules r SET hit_count = r.hit_count + v.n, last_hit_at = NOW()
FROM unnest($1::bigint[], $2::int[]) AS v(id, n) WHERE r.id = v.id`, ruleHits); err != nil {
		return nil, err
	}
	if err := addCountsTx(ctx, tx, `UPDATE risk_lists l SET hit_count = l.hit_count + v.n, last_hit_at = NOW()
FROM unnest($1::bigint[], $2::int[]) AS v(id, n) WHERE l.id = v.id`, listHits); err != nil {
		return nil, err
	}
	if err := touchIPRiskCountersTx(ctx, tx, ips); err != nil {
		return nil, err
	}
	if err := touchDeviceFingerprintsTx(ctx, tx, devices); err != nil {
		return nil, err
	}
	return fresh, tx.Commit(ctx)
}

// PurgeRiskTrailEvents 删掉过期的去重记录（比任何重投间隔都久的）。
func (r *Repository) PurgeRiskTrailEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM risk_trail_events WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func riskAssessmentCopyRow(record RiskTrailRecord) ([]any, error) {
	a := record.Assessment
	normalizeAssessmentShape(&a)
	rulesJSON, err := json.Marshal(a.MatchedRules)
	if err != nil {
		return nil, err
	}
	contextJSON, err := json.Marshal(a.EvalContext)
	if err != nil {
		return nil, err
	}
	dimJSON, _ := json.Marshal(a.DimensionScores)
	listJSON, _ := json.Marshal(a.ListHits)
	enforceJSON, _ := json.Marshal(a.Enforcement)
	created := record.At
	if created.IsZero() {
		created = time.Now()
	}
	return []any{
		a.Scene, a.AppID, a.UserID, a.IdentityID, truncateColumn(a.Account, 190), truncateColumn(a.IP, 64),
		truncateColumn(a.DeviceID, 256), a.UserAgent, truncateColumn(a.Country, 64), a.TotalScore, a.RiskLevel,
		rulesJSON, contextJSON, a.LatencyMS, a.Action, a.ActionDetail, dimJSON, listJSON, a.DecisionBy, enforceJSON, created,
	}, nil
}

// addCountsTx 按 ID 升序合并成一条 UPDATE（多个写入者按同一顺序加锁）。
func addCountsTx(ctx context.Context, tx pgx.Tx, sql string, counts map[int64]int) error {
	ids := make([]int64, 0, len(counts))
	for id, n := range counts {
		if id > 0 && n > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ns := make([]int32, len(ids))
	for index, id := range ids {
		ns[index] = int32(counts[id])
	}
	_, err := tx.Exec(ctx, sql, ids, ns)
	return err
}

type ipRiskTouch struct {
	ip                        string
	requests, blocks          int64
	country, region, isp, asn string
}

// touchIPRiskCountersTx IP 计数按 IP 合并后一次 UPSERT（与 TouchIPRiskCounters 同一口径）。
func touchIPRiskCountersTx(ctx context.Context, tx pgx.Tx, touches map[string]*ipRiskTouch) error {
	if len(touches) == 0 {
		return nil
	}
	keys := make([]string, 0, len(touches))
	for key := range touches {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	n := len(keys)
	ips, countries, regions, isps, asns := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	requests, blocks := make([]int64, n), make([]int64, n)
	for index, key := range keys {
		touch := touches[key]
		ips[index] = truncateColumn(touch.ip, 64)
		requests[index], blocks[index] = touch.requests, touch.blocks
		countries[index], regions[index] = truncateColumn(touch.country, 64), truncateColumn(touch.region, 128)
		isps[index], asns[index] = truncateColumn(touch.isp, 128), truncateColumn(touch.asn, 64)
	}
	_, err := tx.Exec(ctx, `INSERT INTO ip_risk_records
    (ip, risk_tag, country, region, isp, asn, total_requests, total_blocks, first_seen_at, last_seen_at)
SELECT v.ip, 'normal', v.country, v.region, v.isp, v.asn, v.requests, v.blocks, NOW(), NOW()
FROM unnest($1::text[], $2::bigint[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[])
  AS v(ip, requests, blocks, country, region, isp, asn)
ON CONFLICT (ip) DO UPDATE SET
    country        = COALESCE(NULLIF(ip_risk_records.country, ''), EXCLUDED.country),
    region         = COALESCE(NULLIF(ip_risk_records.region, ''), EXCLUDED.region),
    isp            = COALESCE(NULLIF(ip_risk_records.isp, ''), EXCLUDED.isp),
    asn            = COALESCE(NULLIF(ip_risk_records.asn, ''), EXCLUDED.asn),
    total_requests = ip_risk_records.total_requests + EXCLUDED.total_requests,
    total_blocks   = ip_risk_records.total_blocks + EXCLUDED.total_blocks,
    last_seen_at   = NOW()`, ips, requests, blocks, countries, regions, isps, asns)
	return err
}

type deviceTouch struct {
	fp   securitydomain.DeviceFingerprint
	seen int
}

// touchDeviceFingerprintsTx 设备指纹按设备合并后一次 UPSERT（与 TouchDeviceFingerprint 同一口径，seen_count 加合计）。
func touchDeviceFingerprintsTx(ctx context.Context, tx pgx.Tx, touches map[string]*deviceTouch) error {
	if len(touches) == 0 {
		return nil
	}
	keys := make([]string, 0, len(touches))
	for key := range touches {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	n := len(keys)
	ids, fps, tags, lastIPs, agents := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	userIDs, appIDs := make([]*int64, n), make([]*int64, n)
	seen := make([]int32, n)
	for index, key := range keys {
		touch := touches[key]
		fp := touch.fp
		raw := []byte("{}")
		if fp.Fingerprint != nil {
			if encoded, err := json.Marshal(fp.Fingerprint); err == nil {
				raw = encoded
			}
		}
		tag := fp.RiskTag
		if tag == "" {
			tag = securitydomain.TagNormal
		}
		ids[index], fps[index], tags[index] = truncateColumn(fp.DeviceID, 256), string(raw), tag
		lastIPs[index], agents[index] = truncateColumn(fp.LastIP, 64), fp.UserAgent
		userIDs[index], appIDs[index] = fp.UserID, fp.AppID
		seen[index] = int32(max(touch.seen, 1))
	}
	_, err := tx.Exec(ctx, `INSERT INTO device_fingerprints
    (device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, first_seen_at, last_seen_at, seen_count)
SELECT v.device_id, v.user_id, v.app_id, v.fingerprint::jsonb, v.risk_tag, v.last_ip, v.user_agent, NOW(), NOW(), v.seen
FROM unnest($1::text[], $2::bigint[], $3::bigint[], $4::text[], $5::text[], $6::text[], $7::text[], $8::int[])
  AS v(device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, seen)
ON CONFLICT (device_id) DO UPDATE SET
    fingerprint  = COALESCE(NULLIF(EXCLUDED.fingerprint, '{}'::jsonb), device_fingerprints.fingerprint),
    user_id      = COALESCE(device_fingerprints.user_id, EXCLUDED.user_id),
    app_id       = COALESCE(device_fingerprints.app_id, EXCLUDED.app_id),
    last_ip      = COALESCE(NULLIF(EXCLUDED.last_ip, ''), device_fingerprints.last_ip),
    user_agent   = COALESCE(NULLIF(EXCLUDED.user_agent, ''), device_fingerprints.user_agent),
    last_seen_at = NOW(),
    seen_count   = device_fingerprints.seen_count + EXCLUDED.seen_count`,
		ids, userIDs, appIDs, fps, tags, lastIPs, agents, seen)
	return err
}

func firstNonEmptyString(current, next string) string {
	if current != "" {
		return current
	}
	return next
}
