package postgres

import (
	"context"
	"encoding/json"
	"time"

	functiondomain "aegis/internal/domain/appfunction"

	"github.com/jackc/pgx/v5"
)

// GetAppFunctionKV 读取脚本键值对；已过期的条目视为不存在。
func (r *Repository) GetAppFunctionKV(
	ctx context.Context,
	appID int64,
	scope string,
	scopeID int64,
	key string,
) (*functiondomain.KVEntry, error) {
	entry := functiondomain.KVEntry{Scope: scope, ScopeID: scopeID, Key: key}
	var value []byte
	err := r.pool.QueryRow(ctx, `
		SELECT value, expires_at
		FROM app_function_kv
		WHERE appid = $1 AND scope = $2 AND scope_id = $3 AND key = $4
		  AND (expires_at IS NULL OR expires_at > NOW())`,
		appID, scope, scopeID, key,
	).Scan(&value, &entry.ExpiresAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	entry.Value = json.RawMessage(value)
	return &entry, nil
}

// SetAppFunctionKV 写入或覆盖键值对。ttl <= 0 表示永不过期。
func (r *Repository) SetAppFunctionKV(
	ctx context.Context,
	appID int64,
	scope string,
	scopeID int64,
	key string,
	value json.RawMessage,
	ttl time.Duration,
) error {
	var expiresAt *time.Time
	if ttl > 0 {
		deadline := time.Now().Add(ttl)
		expiresAt = &deadline
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO app_function_kv (appid, scope, scope_id, key, value, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (appid, scope, scope_id, key)
		DO UPDATE SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at, updated_at = NOW()`,
		appID, scope, scopeID, key, []byte(value), expiresAt,
	)
	return err
}

// DeleteAppFunctionKV 删除键值对，返回是否确实删掉了一条。
func (r *Repository) DeleteAppFunctionKV(
	ctx context.Context,
	appID int64,
	scope string,
	scopeID int64,
	key string,
) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM app_function_kv
		WHERE appid = $1 AND scope = $2 AND scope_id = $3 AND key = $4`,
		appID, scope, scopeID, key,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// IncrAppFunctionKV 原子自增一个整数键，返回自增后的值。
//
// 频次限制、剩余次数这类反破解逻辑依赖它的原子性：并发调用不会读到同一个旧值。
// 非整数或已过期的条目会被当作 0 重新开始计数。
func (r *Repository) IncrAppFunctionKV(
	ctx context.Context,
	appID int64,
	scope string,
	scopeID int64,
	key string,
	delta int64,
	ttl time.Duration,
) (int64, error) {
	var expiresAt *time.Time
	if ttl > 0 {
		deadline := time.Now().Add(ttl)
		expiresAt = &deadline
	}
	var result int64
	err := r.pool.QueryRow(ctx, `
		INSERT INTO app_function_kv (appid, scope, scope_id, key, value, expires_at)
		VALUES ($1,$2,$3,$4, to_jsonb($5::bigint), $6)
		ON CONFLICT (appid, scope, scope_id, key) DO UPDATE SET
			value = to_jsonb(
				CASE
					WHEN app_function_kv.expires_at IS NOT NULL AND app_function_kv.expires_at <= NOW() THEN $5::bigint
					WHEN jsonb_typeof(app_function_kv.value) = 'number'
						THEN (app_function_kv.value)::text::bigint + $5::bigint
					ELSE $5::bigint
				END
			),
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
		RETURNING (value)::text::bigint`,
		appID, scope, scopeID, key, delta, expiresAt,
	).Scan(&result)
	return result, err
}

// PurgeExpiredAppFunctionKV 清理过期条目，返回删除行数。
func (r *Repository) PurgeExpiredAppFunctionKV(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM app_function_kv WHERE expires_at IS NOT NULL AND expires_at <= NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
