package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	lotterydomain "aegis/internal/domain/lottery"

	"github.com/jackc/pgx/v5"
)

// ── 活动 CRUD ──

func (r *Repository) CreateLotteryActivity(ctx context.Context, appID int64, a lotterydomain.Activity) (*lotterydomain.Activity, error) {
	rulesJSON, _ := json.Marshal(a.AutoJoinRules)
	if a.AutoJoinRules == nil {
		rulesJSON = []byte("{}")
	}
	query := `INSERT INTO lottery_activities
		(appid, name, description, ui_mode, status, join_mode, auto_join_rules, cost_type, cost_amount, daily_limit, total_limit, start_time, end_time)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, appid, name, COALESCE(description,''), ui_mode, status, join_mode, auto_join_rules,
		          cost_type, cost_amount, daily_limit, total_limit, start_time, end_time,
		          COALESCE(seed_hash,''), COALESCE(seed_value,''), COALESCE(chain_tx_hash,''), COALESCE(chain_network,''),
		          created_at, updated_at`
	return scanLotteryActivity(r.pool.QueryRow(ctx, query,
		appID, a.Name, nullableString(a.Description), a.UIMode, a.Status, a.JoinMode, rulesJSON,
		a.CostType, a.CostAmount, a.DailyLimit, a.TotalLimit, a.StartTime, a.EndTime))
}

func (r *Repository) UpdateLotteryActivity(ctx context.Context, activityID int64, m lotterydomain.ActivityMutation) (*lotterydomain.Activity, error) {
	sets := make([]string, 0, 14)
	args := make([]any, 0, 14)
	idx := 1

	addSet := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
		args = append(args, val)
		idx++
	}

	if m.Name != nil {
		addSet("name", *m.Name)
	}
	if m.Description != nil {
		addSet("description", *m.Description)
	}
	if m.UIMode != nil {
		addSet("ui_mode", *m.UIMode)
	}
	if m.Status != nil {
		addSet("status", *m.Status)
	}
	if m.JoinMode != nil {
		addSet("join_mode", *m.JoinMode)
	}
	if m.AutoJoinRules != nil {
		rulesJSON, _ := json.Marshal(m.AutoJoinRules)
		addSet("auto_join_rules", rulesJSON)
	}
	if m.CostType != nil {
		addSet("cost_type", *m.CostType)
	}
	if m.CostAmount != nil {
		addSet("cost_amount", *m.CostAmount)
	}
	if m.DailyLimit != nil {
		addSet("daily_limit", *m.DailyLimit)
	}
	if m.TotalLimit != nil {
		addSet("total_limit", *m.TotalLimit)
	}
	if m.StartTime != nil {
		addSet("start_time", *m.StartTime)
	}
	if m.EndTime != nil {
		addSet("end_time", *m.EndTime)
	}

	if len(sets) == 0 {
		return r.GetLotteryActivity(ctx, activityID)
	}

	addSet("updated_at", time.Now().UTC())
	args = append(args, activityID)

	query := fmt.Sprintf(`UPDATE lottery_activities SET %s WHERE id = $%d
		RETURNING id, appid, name, COALESCE(description,''), ui_mode, status, join_mode, auto_join_rules,
		          cost_type, cost_amount, daily_limit, total_limit, start_time, end_time,
		          COALESCE(seed_hash,''), COALESCE(seed_value,''), COALESCE(chain_tx_hash,''), COALESCE(chain_network,''),
		          created_at, updated_at`,
		strings.Join(sets, ", "), idx)

	return scanLotteryActivity(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) GetLotteryActivity(ctx context.Context, activityID int64) (*lotterydomain.Activity, error) {
	query := `SELECT id, appid, name, COALESCE(description,''), ui_mode, status, join_mode, auto_join_rules,
		          cost_type, cost_amount, daily_limit, total_limit, start_time, end_time,
		          COALESCE(seed_hash,''), COALESCE(seed_value,''), COALESCE(chain_tx_hash,''), COALESCE(chain_network,''),
		          created_at, updated_at
		FROM lottery_activities WHERE id = $1`
	return scanLotteryActivity(r.pool.QueryRow(ctx, query, activityID))
}

func (r *Repository) ListLotteryActivities(ctx context.Context, q lotterydomain.ActivityListQuery) ([]lotterydomain.Activity, int64, error) {
	where := []string{"appid = $1"}
	args := []any{q.AppID}
	idx := 2

	if q.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}
	if q.Keyword != "" {
		where = append(where, fmt.Sprintf("name ILIKE $%d", idx))
		args = append(args, "%"+q.Keyword+"%")
		idx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	countSQL := "SELECT COUNT(*) FROM lottery_activities WHERE " + whereClause
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	listSQL := fmt.Sprintf(`SELECT id, appid, name, COALESCE(description,''), ui_mode, status, join_mode, auto_join_rules,
		cost_type, cost_amount, daily_limit, total_limit, start_time, end_time,
		COALESCE(seed_hash,''), COALESCE(seed_value,''), COALESCE(chain_tx_hash,''), COALESCE(chain_network,''),
		created_at, updated_at
		FROM lottery_activities WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]lotterydomain.Activity, 0, limit)
	for rows.Next() {
		item, err := scanLotteryActivityRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, nil
}

func (r *Repository) DeleteLotteryActivity(ctx context.Context, activityID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM lottery_activities WHERE id = $1`, activityID)
	return err
}

// ── 奖品 CRUD ──

func (r *Repository) CreateLotteryPrize(ctx context.Context, activityID int64, p lotterydomain.Prize) (*lotterydomain.Prize, error) {
	extraJSON, _ := json.Marshal(p.Extra)
	if p.Extra == nil {
		extraJSON = []byte("{}")
	}
	query := `INSERT INTO lottery_prizes
		(activity_id, name, type, value, image_url, quantity, weight, position, is_default, extra)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, activity_id, name, type, value, COALESCE(image_url,''), quantity, used, weight, position, is_default, extra, created_at, updated_at`
	return scanLotteryPrize(r.pool.QueryRow(ctx, query,
		activityID, p.Name, p.Type, p.Value, nullableString(p.ImageURL), p.Quantity, p.Weight, p.Position, p.IsDefault, extraJSON))
}

func (r *Repository) UpdateLotteryPrize(ctx context.Context, prizeID int64, m lotterydomain.PrizeMutation) (*lotterydomain.Prize, error) {
	sets := make([]string, 0, 10)
	args := make([]any, 0, 10)
	idx := 1

	addSet := func(col string, val any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", col, idx))
		args = append(args, val)
		idx++
	}

	if m.Name != nil {
		addSet("name", *m.Name)
	}
	if m.Type != nil {
		addSet("type", *m.Type)
	}
	if m.Value != nil {
		addSet("value", *m.Value)
	}
	if m.ImageURL != nil {
		addSet("image_url", *m.ImageURL)
	}
	if m.Quantity != nil {
		addSet("quantity", *m.Quantity)
	}
	if m.Weight != nil {
		addSet("weight", *m.Weight)
	}
	if m.Position != nil {
		addSet("position", *m.Position)
	}
	if m.IsDefault != nil {
		addSet("is_default", *m.IsDefault)
	}
	if m.Extra != nil {
		extraJSON, _ := json.Marshal(m.Extra)
		addSet("extra", extraJSON)
	}

	if len(sets) == 0 {
		return r.getLotteryPrize(ctx, prizeID)
	}

	addSet("updated_at", time.Now().UTC())
	args = append(args, prizeID)

	query := fmt.Sprintf(`UPDATE lottery_prizes SET %s WHERE id = $%d
		RETURNING id, activity_id, name, type, value, COALESCE(image_url,''), quantity, used, weight, position, is_default, extra, created_at, updated_at`,
		strings.Join(sets, ", "), idx)

	return scanLotteryPrize(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) DeleteLotteryPrize(ctx context.Context, prizeID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM lottery_prizes WHERE id = $1`, prizeID)
	return err
}

func (r *Repository) ListLotteryPrizes(ctx context.Context, activityID int64) ([]lotterydomain.Prize, error) {
	query := `SELECT id, activity_id, name, type, value, COALESCE(image_url,''), quantity, used, weight, position, is_default, extra, created_at, updated_at
		FROM lottery_prizes WHERE activity_id = $1 ORDER BY position ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]lotterydomain.Prize, 0, 8)
	for rows.Next() {
		item, err := scanLotteryPrizeRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *Repository) DecrementLotteryPrizeStock(ctx context.Context, prizeID int64) error {
	query := `UPDATE lottery_prizes SET used = used + 1, updated_at = NOW()
		WHERE id = $1 AND (quantity = -1 OR used < quantity)`
	tag, err := r.pool.Exec(ctx, query, prizeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("奖品库存不足")
	}
	return nil
}

func (r *Repository) getLotteryPrize(ctx context.Context, prizeID int64) (*lotterydomain.Prize, error) {
	query := `SELECT id, activity_id, name, type, value, COALESCE(image_url,''), quantity, used, weight, position, is_default, extra, created_at, updated_at
		FROM lottery_prizes WHERE id = $1`
	return scanLotteryPrize(r.pool.QueryRow(ctx, query, prizeID))
}

// ── 参与记录 ──

func (r *Repository) JoinLotteryActivity(ctx context.Context, activityID int64, userID int64, joinType string) error {
	query := `INSERT INTO lottery_participants (activity_id, user_id, join_type) VALUES ($1,$2,$3) ON CONFLICT (activity_id, user_id) DO NOTHING`
	_, err := r.pool.Exec(ctx, query, activityID, userID, joinType)
	return err
}

func (r *Repository) IsLotteryParticipant(ctx context.Context, activityID int64, userID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lottery_participants WHERE activity_id = $1 AND user_id = $2)`, activityID, userID).Scan(&exists)
	return exists, err
}

func (r *Repository) CountLotteryParticipants(ctx context.Context, activityID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_participants WHERE activity_id = $1`, activityID).Scan(&count)
	return count, err
}

// ── 抽奖记录 ──

func (r *Repository) CreateLotteryDraw(ctx context.Context, d lotterydomain.Draw) (*lotterydomain.Draw, error) {
	snapshotJSON, _ := json.Marshal(d.PrizeSnapshot)
	if d.PrizeSnapshot == nil {
		snapshotJSON = []byte("{}")
	}
	query := `INSERT INTO lottery_draws
		(activity_id, user_id, prize_id, prize_snapshot, draw_seed, draw_proof, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, activity_id, user_id, prize_id, prize_snapshot, COALESCE(draw_seed,''), COALESCE(draw_proof,''), status, claimed_at, created_at`
	return scanLotteryDraw(r.pool.QueryRow(ctx, query,
		d.ActivityID, d.UserID, d.PrizeID, snapshotJSON, nullableString(d.DrawSeed), nullableString(d.DrawProof), d.Status))
}

func (r *Repository) ListLotteryDraws(ctx context.Context, q lotterydomain.DrawListQuery) ([]lotterydomain.Draw, int64, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if q.ActivityID > 0 {
		where = append(where, fmt.Sprintf("activity_id = $%d", idx))
		args = append(args, q.ActivityID)
		idx++
	}
	if q.UserID > 0 {
		where = append(where, fmt.Sprintf("user_id = $%d", idx))
		args = append(args, q.UserID)
		idx++
	}
	if q.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM lottery_draws WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	listSQL := fmt.Sprintf(`SELECT id, activity_id, user_id, prize_id, prize_snapshot, COALESCE(draw_seed,''), COALESCE(draw_proof,''), status, claimed_at, created_at
		FROM lottery_draws WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]lotterydomain.Draw, 0, limit)
	for rows.Next() {
		item, err := scanLotteryDrawRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, nil
}

func (r *Repository) CountUserLotteryDrawsToday(ctx context.Context, activityID int64, userID int64) (int64, error) {
	// 使用 UTC 日期边界
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lottery_draws WHERE activity_id = $1 AND user_id = $2 AND created_at >= $3`,
		activityID, userID, todayStart).Scan(&count)
	return count, err
}

func (r *Repository) CountUserLotteryDrawsTotal(ctx context.Context, activityID int64, userID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM lottery_draws WHERE activity_id = $1 AND user_id = $2`,
		activityID, userID).Scan(&count)
	return count, err
}

// ── 种子承诺 ──

func (r *Repository) CreateLotterySeedCommitment(ctx context.Context, activityID int64, round int, seedHash string) (*lotterydomain.SeedCommitment, error) {
	query := `INSERT INTO lottery_seed_commitments (activity_id, round, seed_hash)
		VALUES ($1,$2,$3)
		RETURNING id, activity_id, round, seed_hash, COALESCE(seed_value,''), committed_at, revealed_at, COALESCE(chain_tx_hash,'')`
	return scanLotterySeedCommitment(r.pool.QueryRow(ctx, query, activityID, round, seedHash))
}

func (r *Repository) RevealLotterySeed(ctx context.Context, activityID int64, round int, seedValue string) error {
	query := `UPDATE lottery_seed_commitments SET seed_value = $3, revealed_at = NOW() WHERE activity_id = $1 AND round = $2`
	_, err := r.pool.Exec(ctx, query, activityID, round, seedValue)
	return err
}

func (r *Repository) GetLotterySeedCommitment(ctx context.Context, activityID int64, round int) (*lotterydomain.SeedCommitment, error) {
	query := `SELECT id, activity_id, round, seed_hash, COALESCE(seed_value,''), committed_at, revealed_at, COALESCE(chain_tx_hash,'')
		FROM lottery_seed_commitments WHERE activity_id = $1 AND round = $2`
	return scanLotterySeedCommitment(r.pool.QueryRow(ctx, query, activityID, round))
}

func (r *Repository) UpdateLotterySeedChainTx(ctx context.Context, activityID int64, round int, txHash string) error {
	query := `UPDATE lottery_seed_commitments SET chain_tx_hash = $3 WHERE activity_id = $1 AND round = $2`
	_, err := r.pool.Exec(ctx, query, activityID, round, txHash)
	return err
}

func (r *Repository) UpdateLotteryActivitySeed(ctx context.Context, activityID int64, seedHash, chainTxHash, chainNetwork string) error {
	query := `UPDATE lottery_activities SET seed_hash = $2, chain_tx_hash = $3, chain_network = $4, updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, activityID, seedHash, nullableString(chainTxHash), nullableString(chainNetwork))
	return err
}

func (r *Repository) RevealLotteryActivitySeed(ctx context.Context, activityID int64, seedValue string) error {
	query := `UPDATE lottery_activities SET seed_value = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, activityID, seedValue)
	return err
}

// ── 统计 ──

func (r *Repository) GetLotteryActivityStats(ctx context.Context, activityID int64) (*lotterydomain.ActivityStats, error) {
	stats := &lotterydomain.ActivityStats{ActivityID: activityID}

	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws WHERE activity_id = $1`, activityID).Scan(&stats.TotalDraws)
	if err != nil {
		return nil, err
	}
	err = r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM lottery_draws WHERE activity_id = $1`, activityID).Scan(&stats.TotalUsers)
	if err != nil {
		return nil, err
	}
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_participants WHERE activity_id = $1`, activityID).Scan(&stats.Participants)
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT id, name, type, quantity, used, weight FROM lottery_prizes WHERE activity_id = $1 ORDER BY position`, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prizeStats := make([]lotterydomain.PrizeStatItem, 0, 8)
	for rows.Next() {
		var ps lotterydomain.PrizeStatItem
		if err := rows.Scan(&ps.PrizeID, &ps.Name, &ps.Type, &ps.Quantity, &ps.Used, &ps.Weight); err != nil {
			return nil, err
		}
		prizeStats = append(prizeStats, ps)
	}
	stats.PrizeStats = prizeStats
	return stats, nil
}

// ── scan 辅助函数 ──

func scanLotteryActivity(row interface{ Scan(dest ...any) error }) (*lotterydomain.Activity, error) {
	var item lotterydomain.Activity
	var rulesJSON []byte
	if err := row.Scan(
		&item.ID, &item.AppID, &item.Name, &item.Description, &item.UIMode, &item.Status,
		&item.JoinMode, &rulesJSON,
		&item.CostType, &item.CostAmount, &item.DailyLimit, &item.TotalLimit,
		&item.StartTime, &item.EndTime,
		&item.SeedHash, &item.SeedValue, &item.ChainTxHash, &item.ChainNetwork,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(rulesJSON, &item.AutoJoinRules)
	return &item, nil
}

func scanLotteryActivityRow(rows pgx.Rows) (*lotterydomain.Activity, error) {
	var item lotterydomain.Activity
	var rulesJSON []byte
	if err := rows.Scan(
		&item.ID, &item.AppID, &item.Name, &item.Description, &item.UIMode, &item.Status,
		&item.JoinMode, &rulesJSON,
		&item.CostType, &item.CostAmount, &item.DailyLimit, &item.TotalLimit,
		&item.StartTime, &item.EndTime,
		&item.SeedHash, &item.SeedValue, &item.ChainTxHash, &item.ChainNetwork,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(rulesJSON, &item.AutoJoinRules)
	return &item, nil
}

func scanLotteryPrize(row interface{ Scan(dest ...any) error }) (*lotterydomain.Prize, error) {
	var item lotterydomain.Prize
	var extraJSON []byte
	if err := row.Scan(
		&item.ID, &item.ActivityID, &item.Name, &item.Type, &item.Value, &item.ImageURL,
		&item.Quantity, &item.Used, &item.Weight, &item.Position, &item.IsDefault,
		&extraJSON, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(extraJSON, &item.Extra)
	return &item, nil
}

func scanLotteryPrizeRow(rows pgx.Rows) (*lotterydomain.Prize, error) {
	var item lotterydomain.Prize
	var extraJSON []byte
	if err := rows.Scan(
		&item.ID, &item.ActivityID, &item.Name, &item.Type, &item.Value, &item.ImageURL,
		&item.Quantity, &item.Used, &item.Weight, &item.Position, &item.IsDefault,
		&extraJSON, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(extraJSON, &item.Extra)
	return &item, nil
}

func scanLotteryDraw(row interface{ Scan(dest ...any) error }) (*lotterydomain.Draw, error) {
	var item lotterydomain.Draw
	var snapshotJSON []byte
	if err := row.Scan(
		&item.ID, &item.ActivityID, &item.UserID, &item.PrizeID,
		&snapshotJSON, &item.DrawSeed, &item.DrawProof, &item.Status,
		&item.ClaimedAt, &item.CreatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(snapshotJSON, &item.PrizeSnapshot)
	return &item, nil
}

func scanLotteryDrawRow(rows pgx.Rows) (*lotterydomain.Draw, error) {
	var item lotterydomain.Draw
	var snapshotJSON []byte
	if err := rows.Scan(
		&item.ID, &item.ActivityID, &item.UserID, &item.PrizeID,
		&snapshotJSON, &item.DrawSeed, &item.DrawProof, &item.Status,
		&item.ClaimedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(snapshotJSON, &item.PrizeSnapshot)
	return &item, nil
}

func scanLotterySeedCommitment(row interface{ Scan(dest ...any) error }) (*lotterydomain.SeedCommitment, error) {
	var item lotterydomain.SeedCommitment
	if err := row.Scan(
		&item.ID, &item.ActivityID, &item.Round, &item.SeedHash,
		&item.SeedValue, &item.CommittedAt, &item.RevealedAt, &item.ChainTxHash,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}
