package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖的应用级设置（表 lottery_app_settings，迁移 000142）。目前只有每日运气的至尊加成，见 domain/lottery/supreme.go。

// lotteryAppSettingsQ 应用的抽奖设置；没保存过时是零值（至尊加成关闭）。抽奖事务里用事务连接取。
func lotteryAppSettingsQ(ctx context.Context, q queryExecutor, appID int64) (lotterydomain.AppSettings, error) {
	return scanLotteryAppSettings(q.QueryRow(ctx, lotteryAppSettingsSQL, appID), appID)
}

// lotteryAppSettingsSQL 见 lotteryAppSettingsQ（$1 appid）。
const lotteryAppSettingsSQL = `SELECT supreme, updated_by, updated_at FROM lottery_app_settings WHERE appid = $1`

// scanLotteryAppSettings 解析一行；没保存过时是零值（单发与批量管道共用）。
func scanLotteryAppSettings(row pgx.Row, appID int64) (lotterydomain.AppSettings, error) {
	settings := lotterydomain.AppSettings{AppID: appID, Supreme: lotterydomain.Supreme{Scope: lotterydomain.SupremeScopeLifetime}}
	var raw []byte
	err := row.Scan(&raw, &settings.UpdatedBy, &settings.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &settings.Supreme); err != nil {
			return settings, err
		}
	}
	if normalized := lotterydomain.NormalizeSupreme(&settings.Supreme); normalized != nil {
		settings.Supreme = *normalized
	}
	return settings, nil
}

// lotteryAppSupremeQ 应用的至尊加成（关闭时为 nil）。
func lotteryAppSupremeQ(ctx context.Context, q queryExecutor, appID int64) (*lotterydomain.Supreme, error) {
	settings, err := lotteryAppSettingsQ(ctx, q, appID)
	if err != nil || !settings.Supreme.Enabled {
		return nil, err
	}
	return &settings.Supreme, nil
}

// GetLotteryAppSettings 见 lotteryAppSettingsQ。
func (r *Repository) GetLotteryAppSettings(ctx context.Context, appID int64) (lotterydomain.AppSettings, error) {
	return lotteryAppSettingsQ(ctx, r.pool, appID)
}

// LotteryAppSupreme 见 lotteryAppSupremeQ（活动页、模拟、积分账用；抽奖事务里另取一份）。
func (r *Repository) LotteryAppSupreme(ctx context.Context, appID int64) (*lotterydomain.Supreme, error) {
	return lotteryAppSupremeQ(ctx, r.pool, appID)
}

// SaveLotteryAppSettings 保存应用的抽奖设置（已校验）。
func (r *Repository) SaveLotteryAppSettings(ctx context.Context, appID int64, supreme lotterydomain.Supreme, operator string,
	now time.Time) (lotterydomain.AppSettings, error) {
	raw, err := json.Marshal(supreme)
	if err != nil {
		return lotterydomain.AppSettings{}, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO lottery_app_settings (appid, supreme, updated_by, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (appid) DO UPDATE SET supreme = EXCLUDED.supreme, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		appID, raw, truncateColumn(operator, 128), now); err != nil {
		return lotterydomain.AppSettings{}, err
	}
	return lotteryAppSettingsQ(ctx, r.pool, appID)
}
