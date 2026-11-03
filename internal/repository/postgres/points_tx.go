package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/growth"
	"aegis/internal/domain/levels"
	pointdomain "aegis/internal/domain/points"
)

// applyExperienceChangeTx 事务内经验值变更（锁行 → 更新 → 同步等级 → 记账）。
//
// 抽出来是因为经验值此前**只有**一个自开事务的管理端入口，任何想
// 「在同一事务里既发经验又发别的」的场景都接不进去 —— 卡密核销要求
// 七档权益要么全发、要么全不发，经验值单独跑一个事务就破了这条。
//
// 与 applyIntegralChangeTx 的形状刻意保持一致（同样的参数顺序、同样的锁行位置），
// 两者会在同一个事务里被连续调用，形状不同只会让调用处更容易写错。
//
// 这里只管「获得」（amount 必须为正）。扣减走 applyExperienceDeductTx：只有管理员调整与冲正会扣经验，
// 由管理员显式选择等级怎么处理（保留 / 按经验重新判定），自动发放的路径永远不会扣。
func (r *Repository) applyExperienceChangeTx(ctx context.Context, tx pgx.Tx, userID int64, appID int64,
	amount int64, category string, title string, description string, sourceType string, sourceID *int64,
	extra map[string]any) (*pointdomain.ExperienceAdjustResult, error) {
	return r.applyExperienceChangeTxNo(ctx, tx, userID, appID, amount, category, title, description, sourceType, sourceID, extra, "")
}

// applyExperienceChangeTxNo 同 applyExperienceChangeTx，可指定流水号（管理端幂等调整用）。
func (r *Repository) applyExperienceChangeTxNo(ctx context.Context, tx pgx.Tx, userID int64, appID int64,
	amount int64, category string, title string, description string, sourceType string, sourceID *int64,
	extra map[string]any, transactionNo string) (*pointdomain.ExperienceAdjustResult, error) {
	if amount <= 0 {
		return nil, ErrExperienceDeduction
	}
	var userIDDB int64
	var account string
	var experienceBefore int64
	if err := tx.QueryRow(ctx,
		`SELECT id, account, experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, appID).Scan(&userIDDB, &account, &experienceBefore); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 成长调控：受管来源的经验按规则换算，可能被换算成 0（冻结或到了每日上限），这笔流水照记以便追溯
	multiplier := 1.0
	source := growth.SourceFor(growth.KindExperience, sourceType, false)
	outcome, touched, err := applyGrowthTx(ctx, tx, appID, userID, growth.KindExperience, source, amount)
	if err != nil {
		return nil, err
	}
	if touched {
		amount, extra, multiplier = outcome.Granted, growthExtra(extra, outcome), outcome.Multiplier()
	}

	experienceAfter, err := pointdomain.ApplyDelta(experienceBefore, amount)
	if err != nil {
		return nil, &BalanceError{Err: pointdomain.ErrBalanceOverflow, Kind: pointdomain.KindExperience, Balance: experienceBefore}
	}

	levelBefore, _, err := r.userLevelQ(ctx, tx, appID, userID, experienceBefore)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	if _, err := tx.Exec(ctx,
		`UPDATE users SET experience = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		experienceAfter, userID, appID); err != nil {
		return nil, err
	}
	levelInfo, err := r.syncUserLevelRecord(ctx, tx, userID, appID, experienceAfter, now)
	if err != nil {
		return nil, err
	}
	levelAfter := levelInfo.CurrentLevel

	if transactionNo == "" {
		transactionNo = generateTransactionNo("EXP")
	}
	isLevelUp := levelAfter > levelBefore

	payload := map[string]any{
		"oldLevel": levelBefore,
		"newLevel": levelAfter,
	}
	for key, value := range extra {
		payload[key] = value
	}
	extraData, _ := json.Marshal(payload)

	if _, err := tx.Exec(ctx, `INSERT INTO experience_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, level_before, level_after, status, title, description, source_id, source_type, multiplier, is_level_up, client_ip, user_agent, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, 'earn', $4, $5, $6, $7, $8, $9, 'completed', $10, $11, $12, $13, $19, $14, $15, $16, $17, $18, $18)`,
		transactionNo,
		userID,
		appID,
		category,
		amount,
		experienceBefore,
		experienceAfter,
		levelBefore,
		levelAfter,
		title,
		description,
		sourceID,
		nullableString(sourceType),
		isLevelUp,
		nullableString(stringFrom(extra, "clientIp")),
		nullableString(stringFrom(extra, "userAgent")),
		extraData,
		now,
		multiplier,
	); err != nil {
		return nil, err
	}

	return &pointdomain.ExperienceAdjustResult{
		UserID:        userIDDB,
		AppID:         appID,
		Account:       account,
		Amount:        amount,
		BeforeAmount:  experienceBefore,
		AfterAmount:   experienceAfter,
		Reason:        description,
		OperationType: "add",
		TransactionNo: transactionNo,
		LevelChanged:  isLevelUp,
		OldLevel:      levelBefore,
		NewLevel:      levelAfter,
		CreatedAt:     now,
	}, nil
}

// experienceDeduction 一次经验扣减（管理员调整、冲正）。Amount 为负数。
type experienceDeduction struct {
	UserID        int64
	AppID         int64
	Amount        int64
	Category      string
	Title         string
	Description   string
	SourceType    string
	SourceID      *int64
	Extra         map[string]any
	TransactionNo string
	LevelMode     pointdomain.LevelMode
	Operator      string
}

// applyExperienceDeductTx 事务内扣经验：锁行 → 余额判定（不足即拒）→ 更新 → 按 LevelMode 处理等级 → 记一条负数流水。
//
// 等级：
//   - keep：已达到的等级是保底，扣完经验等级不变，进度按新经验显示（低于本级门槛时本级进度为 0）；
//   - recalc：按新经验从锁定的保底起重新判定（「重算并允许降级」同一套逻辑），封顶照旧生效，降级写升级记录、不通知。
//
// 两种方式都不收回已发的等级达成奖励；以后再升回去，奖励按 用户 × 奖励键 去重，不会重复发。
func (r *Repository) applyExperienceDeductTx(ctx context.Context, tx pgx.Tx, in experienceDeduction) (*pointdomain.ExperienceAdjustResult, error) {
	if in.Amount >= 0 {
		return nil, errors.New("postgres: experience deduction amount must be negative")
	}
	var account string
	var before int64
	if err := tx.QueryRow(ctx, `SELECT account, experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		in.UserID, in.AppID).Scan(&account, &before); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	after, err := pointdomain.ApplyDelta(before, in.Amount)
	if err != nil {
		return nil, &BalanceError{Err: ErrInsufficientExperience, Kind: pointdomain.KindExperience, Balance: before}
	}
	levelBefore, _, err := r.userLevelQ(ctx, tx, in.AppID, in.UserID, before)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE users SET experience = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		after, in.UserID, in.AppID); err != nil {
		return nil, err
	}
	mode := in.LevelMode
	if mode != pointdomain.LevelRecalc {
		mode = pointdomain.LevelKeep
	}
	opts := levelSyncOptions{Reason: levels.ReasonExperience}
	if mode == pointdomain.LevelRecalc {
		opts = levelSyncOptions{Reason: levels.ReasonAdmin, Recompute: true, Silent: true, Operator: in.Operator,
			Note: truncateRunes("扣减经验 "+pointdomain.GroupDigits(-in.Amount)+"："+in.Description, 200)}
	}
	synced, err := r.syncUserLevel(ctx, tx, in.UserID, in.AppID, after, now, opts)
	if err != nil {
		return nil, err
	}
	levelAfter := synced.To

	if in.TransactionNo == "" {
		in.TransactionNo = generateTransactionNo("EXP")
	}
	payload := map[string]any{"oldLevel": levelBefore, "newLevel": levelAfter, "levelMode": string(mode)}
	for key, value := range in.Extra {
		payload[key] = value
	}
	extraData, _ := json.Marshal(payload)
	if _, err := tx.Exec(ctx, `INSERT INTO experience_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after, level_before, level_after, status, title, description, source_id, source_type, multiplier, is_level_up, client_ip, user_agent, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, 'consume', $4, $5, $6, $7, $8, $9, 'completed', $10, $11, $12, $13, 1, FALSE, $14, $15, $16, $17, $17)`,
		in.TransactionNo, in.UserID, in.AppID, in.Category, in.Amount, before, after, levelBefore, levelAfter,
		in.Title, in.Description, in.SourceID, nullableString(in.SourceType),
		nullableString(stringFrom(in.Extra, "clientIp")), nullableString(stringFrom(in.Extra, "userAgent")), extraData, now); err != nil {
		return nil, err
	}
	return &pointdomain.ExperienceAdjustResult{
		UserID: in.UserID, AppID: in.AppID, Account: account, Amount: in.Amount, BeforeAmount: before, AfterAmount: after,
		Reason: in.Description, OperationType: "consume", TransactionNo: in.TransactionNo,
		LevelChanged: levelAfter != levelBefore, OldLevel: levelBefore, NewLevel: levelAfter, LevelMode: mode, CreatedAt: now,
	}, nil
}

// BalanceError 余额校验失败，带当前余额，便于给出「当前 120，不足以扣减 200」这样的提示。
// Unwrap 返回 ErrInsufficientIntegral 或 pointdomain.ErrBalanceOverflow，原有的 errors.Is 判断照常生效。
type BalanceError struct {
	Err     error
	Kind    pointdomain.Kind
	Balance int64
}

func (e *BalanceError) Error() string { return e.Err.Error() }
func (e *BalanceError) Unwrap() error { return e.Err }

var (
	// ErrExperienceDeduction 自动发放路径不能扣经验（扣减只走 applyExperienceDeductTx）。
	ErrExperienceDeduction = errors.New("postgres: experience cannot be deducted")
	// ErrInsufficientExperience 扣减后经验为负。
	ErrInsufficientExperience = errors.New("postgres: insufficient experience")
	// ErrRequestIDConflict 请求编号已被另一笔不同内容的调整使用。
	ErrRequestIDConflict = errors.New("postgres: request id already used by a different adjustment")
)

// stringFrom 从 extra 里取一个字符串字段；缺失或类型不符时返回空串。
func stringFrom(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	if value, ok := extra[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
