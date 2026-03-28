package postgres

import (
	"context"
	"encoding/json"
	"errors"

	systemdomain "aegis/internal/domain/system"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetPlatformSetting(ctx context.Context, key string) (*systemdomain.SettingRecord, error) {
	query := `SELECT setting_key, setting_value, updated_by, created_at, updated_at FROM platform_settings WHERE setting_key = $1 LIMIT 1`

	var item systemdomain.SettingRecord
	var value []byte
	if err := r.pool.QueryRow(ctx, query, key).Scan(&item.Key, &value, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.Value = json.RawMessage(value)
	return &item, nil
}

func (r *Repository) UpsertPlatformSetting(ctx context.Context, item systemdomain.SettingRecord) (*systemdomain.SettingRecord, error) {
	query := `INSERT INTO platform_settings (setting_key, setting_value, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (setting_key) DO UPDATE SET
	setting_value = EXCLUDED.setting_value,
	updated_by = EXCLUDED.updated_by,
	updated_at = NOW()
RETURNING setting_key, setting_value, updated_by, created_at, updated_at`

	var saved systemdomain.SettingRecord
	var value []byte
	if err := r.pool.QueryRow(ctx, query, item.Key, []byte(item.Value), item.UpdatedBy).Scan(&saved.Key, &value, &saved.UpdatedBy, &saved.CreatedAt, &saved.UpdatedAt); err != nil {
		return nil, err
	}
	saved.Value = json.RawMessage(value)
	return &saved, nil
}
