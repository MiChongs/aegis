package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// ListAppFunctionKVKeys 按前缀列出键名（脚本用，只回键不回值）。
//
// 只回键是刻意的：脚本要遍历一批状态时，值往往用不上却会把响应撑大，
// 而 SDK 的额度是按调用次数算的 —— 一次 list 拉回 200 个对象与拉回
// 200 个字符串，对脚本作者是同一笔开销，对数据库不是。
func (r *Repository) ListAppFunctionKVKeys(
	ctx context.Context,
	appID int64,
	scope string,
	scopeID int64,
	prefix string,
	limit int,
) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT key FROM app_function_kv
		WHERE appid = $1 AND scope = $2 AND scope_id = $3
		  AND (expires_at IS NULL OR expires_at > NOW())
		  AND ($4 = '' OR key LIKE $4 || '%')
		ORDER BY key LIMIT $5`,
		appID, scope, scopeID, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]string, 0, limit)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// BrowseAppFunctionKV 是管理端的 KV 浏览器。
//
// 存在的理由：脚本的全部「服务端独占状态」都落在这张表上，而排障时最常问的
// 一句是「这个用户的配额计数现在是多少」。没有这个视图，唯一的回答方式是
// 写一个临时脚本去读它 —— 而那本身就是一次真实的副作用。
//
// 过期条目会被列出并标注：判定上它们等于不存在，但看不见它们就解释不了
// 「为什么 KV 表里有几十万行」。
func (r *Repository) BrowseAppFunctionKV(
	ctx context.Context,
	appID int64,
	query functiondomain.KVQuery,
) (*functiondomain.KVPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}
	where := []string{"appid=$1"}
	args := []any{appID}
	if query.Scope != "" {
		args = append(args, query.Scope)
		where = append(where, fmt.Sprintf("scope=$%d", len(args)))
	}
	if query.ScopeID > 0 {
		args = append(args, query.ScopeID)
		where = append(where, fmt.Sprintf("scope_id=$%d", len(args)))
	}
	if query.Prefix != "" {
		args = append(args, query.Prefix)
		where = append(where, fmt.Sprintf("key LIKE $%d || '%%'", len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_function_kv`+clause, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, limit, (page-1)*limit)
	// 值截断在 SQL 里做：一行可以到 32KB，一页 20 行就是 640KB，
	// 而浏览器上真正要看的只是开头那几十个字符。
	rows, err := r.pool.Query(ctx, `SELECT scope, scope_id, key,
LEFT(value::text, 512), LENGTH(value::text) > 512, expires_at, updated_at
FROM app_function_kv`+clause+
		fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.KVView, 0, limit)
	for rows.Next() {
		var (
			item  functiondomain.KVView
			value string
		)
		if err := rows.Scan(&item.Scope, &item.ScopeID, &item.Key, &value, &item.Truncated,
			&item.ExpiresAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		// 截断后的字符串不再是合法 JSON，按字符串回传，展示端照原样显示
		if item.Truncated {
			encoded, _ := json.Marshal(value)
			item.Value = encoded
		} else {
			item.Value = json.RawMessage(value)
		}
		items = append(items, item)
	}
	return &functiondomain.KVPage{List: items, Total: total, Page: page, Limit: limit}, rows.Err()
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
