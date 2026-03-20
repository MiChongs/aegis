package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	storagedomain "aegis/internal/domain/storage"
)

func (r *Repository) ListStorageConfigs(ctx context.Context, query storagedomain.ListQuery) ([]storagedomain.Config, error) {
	args := make([]any, 0, 3)
	sql := `SELECT id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at FROM storage_configs WHERE 1=1`
	if scope := strings.TrimSpace(query.Scope); scope != "" {
		sql += fmt.Sprintf(" AND scope = $%d", len(args)+1)
		args = append(args, scope)
	}
	if query.AppID != nil {
		sql += fmt.Sprintf(" AND appid = $%d", len(args)+1)
		args = append(args, *query.AppID)
	} else if strings.TrimSpace(query.Scope) == storagedomain.ScopeGlobal {
		sql += " AND appid IS NULL"
	}
	if provider := strings.TrimSpace(query.Provider); provider != "" {
		sql += fmt.Sprintf(" AND provider = $%d", len(args)+1)
		args = append(args, provider)
	}
	sql += " ORDER BY scope ASC, COALESCE(appid, 0) ASC, is_default DESC, id ASC"

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]storagedomain.Config, 0, 8)
	for rows.Next() {
		item, err := scanStorageConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetStorageConfigByID(ctx context.Context, id int64) (*storagedomain.Config, error) {
	return scanStorageConfig(r.pool.QueryRow(ctx, `SELECT id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at FROM storage_configs WHERE id = $1 LIMIT 1`, id))
}

func (r *Repository) GetStorageConfigByScopeID(ctx context.Context, scope string, appID *int64, id int64) (*storagedomain.Config, error) {
	if strings.TrimSpace(scope) == storagedomain.ScopeGlobal {
		return scanStorageConfig(r.pool.QueryRow(ctx, `SELECT id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at FROM storage_configs WHERE id = $1 AND scope = 'global' AND appid IS NULL LIMIT 1`, id))
	}
	if appID == nil || *appID <= 0 {
		return nil, nil
	}
	return scanStorageConfig(r.pool.QueryRow(ctx, `SELECT id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at FROM storage_configs WHERE id = $1 AND scope = 'app' AND appid = $2 LIMIT 1`, id, *appID))
}

func (r *Repository) ResolveStorageConfig(ctx context.Context, appID int64, configName string, provider string) (*storagedomain.Config, error) {
	configName = strings.TrimSpace(configName)
	provider = strings.TrimSpace(provider)
	if appID > 0 {
		if item, err := r.resolveStorageConfigByScope(ctx, storagedomain.ScopeApp, &appID, configName, provider); err != nil || item != nil {
			return item, err
		}
	}
	return r.resolveStorageConfigByScope(ctx, storagedomain.ScopeGlobal, nil, configName, provider)
}

func (r *Repository) resolveStorageConfigByScope(ctx context.Context, scope string, appID *int64, configName string, provider string) (*storagedomain.Config, error) {
	args := make([]any, 0, 4)
	sql := `SELECT id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at FROM storage_configs WHERE enabled = TRUE`
	if scope == storagedomain.ScopeGlobal {
		sql += " AND scope = 'global' AND appid IS NULL"
	} else {
		sql += " AND scope = 'app' AND appid = $1"
		args = append(args, *appID)
	}
	if configName != "" {
		sql += fmt.Sprintf(" AND config_name = $%d", len(args)+1)
		args = append(args, configName)
	}
	if provider != "" {
		sql += fmt.Sprintf(" AND provider = $%d", len(args)+1)
		args = append(args, provider)
	}
	sql += " ORDER BY is_default DESC, id ASC LIMIT 1"
	return scanStorageConfig(r.pool.QueryRow(ctx, sql, args...))
}

func (r *Repository) UpsertStorageConfig(ctx context.Context, item storagedomain.Config) (*storagedomain.Config, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if item.IsDefault {
		if item.Scope == storagedomain.ScopeGlobal {
			if _, err := tx.Exec(ctx, `UPDATE storage_configs SET is_default = FALSE, updated_at = NOW() WHERE scope = 'global' AND appid IS NULL AND id <> $1`, item.ID); err != nil {
				return nil, err
			}
		} else if item.AppID != nil {
			if _, err := tx.Exec(ctx, `UPDATE storage_configs SET is_default = FALSE, updated_at = NOW() WHERE scope = 'app' AND appid = $1 AND id <> $2`, *item.AppID, item.ID); err != nil {
				return nil, err
			}
		}
	}

	raw, _ := json.Marshal(item.ConfigData)
	saved, err := scanStorageConfig(tx.QueryRow(ctx, `INSERT INTO storage_configs (id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, base_url, root_path, description, config_data, created_at, updated_at)
VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	scope = EXCLUDED.scope,
	appid = EXCLUDED.appid,
	provider = EXCLUDED.provider,
	config_name = EXCLUDED.config_name,
	access_mode = EXCLUDED.access_mode,
	enabled = EXCLUDED.enabled,
	is_default = EXCLUDED.is_default,
	proxy_download = EXCLUDED.proxy_download,
	base_url = EXCLUDED.base_url,
	root_path = EXCLUDED.root_path,
	description = EXCLUDED.description,
	config_data = EXCLUDED.config_data,
	updated_at = NOW()
RETURNING id, scope, appid, provider, config_name, access_mode, enabled, is_default, proxy_download, COALESCE(base_url, ''), COALESCE(root_path, ''), COALESCE(description, ''), COALESCE(config_data, '{}'::jsonb), created_at, updated_at`,
		item.ID,
		item.Scope,
		item.AppID,
		item.Provider,
		item.ConfigName,
		item.AccessMode,
		item.Enabled,
		item.IsDefault,
		item.ProxyDownload,
		nullableString(item.BaseURL),
		nullableString(item.RootPath),
		nullableString(item.Description),
		raw,
	))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return saved, nil
}

func (r *Repository) DeleteStorageConfig(ctx context.Context, scope string, appID *int64, id int64) (bool, error) {
	var (
		result any
		err    error
	)
	if strings.TrimSpace(scope) == storagedomain.ScopeGlobal {
		result, err = r.pool.Exec(ctx, `DELETE FROM storage_configs WHERE id = $1 AND scope = 'global' AND appid IS NULL`, id)
	} else if appID != nil && *appID > 0 {
		result, err = r.pool.Exec(ctx, `DELETE FROM storage_configs WHERE id = $1 AND scope = 'app' AND appid = $2`, id, *appID)
	} else {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return result.(interface{ RowsAffected() int64 }).RowsAffected() > 0, nil
}

func scanStorageConfig(row interface{ Scan(dest ...any) error }) (*storagedomain.Config, error) {
	var item storagedomain.Config
	var (
		appID *int64
		raw   []byte
	)
	if err := row.Scan(
		&item.ID,
		&item.Scope,
		&appID,
		&item.Provider,
		&item.ConfigName,
		&item.AccessMode,
		&item.Enabled,
		&item.IsDefault,
		&item.ProxyDownload,
		&item.BaseURL,
		&item.RootPath,
		&item.Description,
		&raw,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.AppID = appID
	_ = json.Unmarshal(raw, &item.ConfigData)
	if item.ConfigData == nil {
		item.ConfigData = map[string]any{}
	}
	return &item, nil
}
