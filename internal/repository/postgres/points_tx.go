package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/growth"
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
// 经验值没有「扣减」路径：等级只升不降，允许扣减就要回答「掉级之后
// 已经发出去的等级权益怎么办」，而那个问题没有好答案。
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
	// ErrExperienceDeduction 经验值只增不减（见 applyExperienceChangeTx 的说明）。
	ErrExperienceDeduction = errors.New("postgres: experience cannot be deducted")
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
