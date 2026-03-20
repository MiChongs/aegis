package postgres

import (
	"context"
	"encoding/json"

	emaildomain "aegis/internal/domain/email"
)

func (r *Repository) ListEmailConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at FROM app_email_configs WHERE appid = $1 ORDER BY is_default DESC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]emaildomain.Config, 0, 4)
	for rows.Next() {
		item, err := scanEmailConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetEmailConfigByID(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at FROM app_email_configs WHERE appid = $1 AND id = $2 LIMIT 1`, appID, id))
}

func (r *Repository) GetEmailConfigByName(ctx context.Context, appID int64, name string) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at FROM app_email_configs WHERE appid = $1 AND name = $2 LIMIT 1`, appID, name))
}

func (r *Repository) GetDefaultEmailConfig(ctx context.Context, appID int64) (*emaildomain.Config, error) {
	return scanEmailConfig(r.pool.QueryRow(ctx, `SELECT id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at FROM app_email_configs WHERE appid = $1 AND enabled = TRUE ORDER BY is_default DESC, id ASC LIMIT 1`, appID))
}

func (r *Repository) UpsertEmailConfig(ctx context.Context, item emaildomain.Config) (*emaildomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if item.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE app_email_configs SET is_default = FALSE, updated_at = NOW() WHERE appid = $1 AND id <> $2`, item.AppID, item.ID); err != nil {
			return nil, err
		}
	}

	configJSON, _ := json.Marshal(item.SMTP)
	query := `INSERT INTO app_email_configs (id, appid, name, provider, enabled, is_default, description, config, created_at, updated_at)
VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	provider = EXCLUDED.provider,
	enabled = EXCLUDED.enabled,
	is_default = EXCLUDED.is_default,
	description = EXCLUDED.description,
	config = EXCLUDED.config,
	updated_at = NOW()
RETURNING id, appid, name, provider, enabled, is_default, COALESCE(description, ''), COALESCE(config, '{}'::jsonb), created_at, updated_at`
	saved, err := scanEmailConfig(tx.QueryRow(ctx, query, item.ID, item.AppID, item.Name, item.Provider, item.Enabled, item.IsDefault, nullableString(item.Description), configJSON))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Repository) DeleteEmailConfig(ctx context.Context, appID int64, id int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM app_email_configs WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func scanEmailConfig(row interface{ Scan(dest ...any) error }) (*emaildomain.Config, error) {
	var item emaildomain.Config
	var configBytes []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Provider, &item.Enabled, &item.IsDefault, &item.Description, &configBytes, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(configBytes, &item.SMTP)
	return &item, nil
}
