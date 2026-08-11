package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	oauthdomain "aegis/internal/domain/oauth"
)

const appOAuthProviderColumns = `id, appid, provider, kind, display_name, icon, color, enabled,
client_id, client_secret_cipher, redirect_url, auth_url, token_url, user_info_url, scopes,
token_auth_style, user_info_auth_style, profile_mapping, extra_auth_params,
allow_login, allow_register, allow_bind, sort_order, remark, created_at, updated_at`

// ListAppOAuthProviders 返回该 App 的全部渠道（含未启用），按展示顺序排列。
func (r *Repository) ListAppOAuthProviders(ctx context.Context, appID int64) ([]oauthdomain.Provider, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+appOAuthProviderColumns+`
FROM app_oauth_providers WHERE appid=$1 ORDER BY sort_order ASC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]oauthdomain.Provider, 0, 8)
	for rows.Next() {
		item, err := scanAppOAuthProvider(rows)
		if err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, rows.Err()
}

// GetAppOAuthProvider 按 slug 读取单个渠道，不存在返回 (nil, nil)。
func (r *Repository) GetAppOAuthProvider(ctx context.Context, appID int64, provider string) (*oauthdomain.Provider, error) {
	return scanAppOAuthProvider(r.pool.QueryRow(ctx, `SELECT `+appOAuthProviderColumns+`
FROM app_oauth_providers WHERE appid=$1 AND provider=$2`, appID, strings.TrimSpace(provider)))
}

// UpsertAppOAuthProvider 按 (appid, provider) 做插入或整体更新。
func (r *Repository) UpsertAppOAuthProvider(ctx context.Context, item oauthdomain.Provider) (*oauthdomain.Provider, error) {
	mapping, err := json.Marshal(defaultStringMap(item.ProfileMapping))
	if err != nil {
		return nil, err
	}
	extra, err := json.Marshal(defaultStringMap(item.ExtraAuthParams))
	if err != nil {
		return nil, err
	}
	scopes := item.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return scanAppOAuthProvider(r.pool.QueryRow(ctx, `INSERT INTO app_oauth_providers
(appid, provider, kind, display_name, icon, color, enabled, client_id, client_secret_cipher,
redirect_url, auth_url, token_url, user_info_url, scopes, token_auth_style, user_info_auth_style,
profile_mapping, extra_auth_params, allow_login, allow_register, allow_bind, sort_order, remark)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
ON CONFLICT (appid, provider) DO UPDATE SET
kind=EXCLUDED.kind, display_name=EXCLUDED.display_name, icon=EXCLUDED.icon, color=EXCLUDED.color,
enabled=EXCLUDED.enabled, client_id=EXCLUDED.client_id,
client_secret_cipher=EXCLUDED.client_secret_cipher, redirect_url=EXCLUDED.redirect_url,
auth_url=EXCLUDED.auth_url, token_url=EXCLUDED.token_url, user_info_url=EXCLUDED.user_info_url,
scopes=EXCLUDED.scopes, token_auth_style=EXCLUDED.token_auth_style,
user_info_auth_style=EXCLUDED.user_info_auth_style, profile_mapping=EXCLUDED.profile_mapping,
extra_auth_params=EXCLUDED.extra_auth_params, allow_login=EXCLUDED.allow_login,
allow_register=EXCLUDED.allow_register, allow_bind=EXCLUDED.allow_bind,
sort_order=EXCLUDED.sort_order, remark=EXCLUDED.remark, updated_at=NOW()
RETURNING `+appOAuthProviderColumns,
		item.AppID, item.Provider, item.Kind, item.DisplayName, item.Icon, item.Color, item.Enabled,
		item.ClientID, item.ClientSecret, item.RedirectURL, item.AuthURL, item.TokenURL,
		item.UserInfoURL, scopes, item.TokenAuthStyle, item.UserInfoAuthStyle, mapping, extra,
		item.AllowLogin, item.AllowRegister, item.AllowBind, item.SortOrder, item.Remark))
}

// SetAppOAuthProviderEnabled 仅切换启用状态，返回受影响行数。
func (r *Repository) SetAppOAuthProviderEnabled(ctx context.Context, appID int64, provider string, enabled bool) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_oauth_providers SET enabled=$3, updated_at=NOW()
WHERE appid=$1 AND provider=$2`, appID, strings.TrimSpace(provider), enabled)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteAppOAuthProvider 删除渠道配置（已产生的用户绑定不受影响）。
func (r *Repository) DeleteAppOAuthProvider(ctx context.Context, appID int64, provider string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_oauth_providers WHERE appid=$1 AND provider=$2`,
		appID, strings.TrimSpace(provider))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ReorderAppOAuthProviders 在单个事务内重排展示顺序。
func (r *Repository) ReorderAppOAuthProviders(ctx context.Context, appID int64, providers []string) error {
	if len(providers) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for index, provider := range providers {
		if _, err := tx.Exec(ctx, `UPDATE app_oauth_providers SET sort_order=$3, updated_at=NOW()
WHERE appid=$1 AND provider=$2`, appID, strings.TrimSpace(provider), index); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// CountAppOAuthBindingsByProvider 统计每个渠道已绑定的用户数。
func (r *Repository) CountAppOAuthBindingsByProvider(ctx context.Context, appID int64) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT provider, COUNT(DISTINCT user_id)
FROM oauth_bindings WHERE appid=$1 GROUP BY provider`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64, 8)
	for rows.Next() {
		var provider string
		var count int64
		if err := rows.Scan(&provider, &count); err != nil {
			return nil, err
		}
		result[provider] = count
	}
	return result, rows.Err()
}

const oauthBindingColumns = `b.id, b.appid, b.user_id, COALESCE(u.account,''), b.provider,
b.provider_user_id, COALESCE(b.union_id,''), COALESCE(b.raw_profile, '{}'::jsonb),
b.created_at, b.updated_at`

// ListOAuthBindings 管理端分页查询绑定记录。
func (r *Repository) ListOAuthBindings(ctx context.Context, query oauthdomain.BindingQuery) (*oauthdomain.BindingPage, error) {
	conditions := []string{"b.appid = $1"}
	args := []any{query.AppID}
	if provider := strings.TrimSpace(query.Provider); provider != "" {
		args = append(args, provider)
		conditions = append(conditions, fmt.Sprintf("b.provider = $%d", len(args)))
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		conditions = append(conditions, fmt.Sprintf("b.user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		conditions = append(conditions,
			"(u.account ILIKE "+placeholder+" OR b.provider_user_id ILIKE "+placeholder+
				" OR COALESCE(b.raw_profile->>'nickname','') ILIKE "+placeholder+")")
	}
	where := " WHERE " + strings.Join(conditions, " AND ")

	page := query.Page
	if page < 1 {
		page = 1
	}
	pageSize := query.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}

	result := &oauthdomain.BindingPage{Items: []oauthdomain.Binding{}, Page: page, PageSize: pageSize}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM oauth_bindings b
LEFT JOIN users u ON u.id = b.user_id`+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	if result.Total == 0 {
		return result, nil
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, `SELECT `+oauthBindingColumns+` FROM oauth_bindings b
LEFT JOIN users u ON u.id = b.user_id`+where+
		fmt.Sprintf(` ORDER BY b.updated_at DESC, b.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanOAuthBinding(rows)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *item)
	}
	return result, rows.Err()
}

// ListUserOAuthBindings 返回某个用户的全部绑定明细（用户端"账号安全"页使用）。
func (r *Repository) ListUserOAuthBindings(ctx context.Context, appID, userID int64) ([]oauthdomain.Binding, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+oauthBindingColumns+` FROM oauth_bindings b
LEFT JOIN users u ON u.id = b.user_id
WHERE b.appid=$1 AND b.user_id=$2 ORDER BY b.created_at ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]oauthdomain.Binding, 0, 4)
	for rows.Next() {
		item, err := scanOAuthBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// DeleteOAuthBinding 解绑：仅删除属于该用户的记录，返回受影响行数。
func (r *Repository) DeleteOAuthBinding(ctx context.Context, appID, userID int64, provider string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM oauth_bindings
WHERE appid=$1 AND user_id=$2 AND provider=$3`, appID, userID, strings.TrimSpace(provider))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountUserOAuthBindings 统计用户已绑定的渠道数（解绑前的安全校验用）。
func (r *Repository) CountUserOAuthBindings(ctx context.Context, appID, userID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM oauth_bindings WHERE appid=$1 AND user_id=$2`,
		appID, userID).Scan(&count)
	return count, err
}

// UserHasPassword 判断账号是否设置了密码（决定解绑最后一个渠道是否会导致失去登录能力）。
func (r *Repository) UserHasPassword(ctx context.Context, userID int64) (bool, error) {
	var hasPassword bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(password_hash,'') <> '' FROM users WHERE id=$1`,
		userID).Scan(&hasPassword)
	if err != nil {
		return false, normalizeNotFound(err)
	}
	return hasPassword, nil
}

func scanAppOAuthProvider(row interface{ Scan(...any) error }) (*oauthdomain.Provider, error) {
	var item oauthdomain.Provider
	var mapping, extra []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Provider, &item.Kind, &item.DisplayName,
		&item.Icon, &item.Color, &item.Enabled, &item.ClientID, &item.ClientSecret,
		&item.RedirectURL, &item.AuthURL, &item.TokenURL, &item.UserInfoURL, &item.Scopes,
		&item.TokenAuthStyle, &item.UserInfoAuthStyle, &mapping, &extra,
		&item.AllowLogin, &item.AllowRegister, &item.AllowBind, &item.SortOrder, &item.Remark,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.ProfileMapping = map[string]string{}
	item.ExtraAuthParams = map[string]string{}
	_ = json.Unmarshal(mapping, &item.ProfileMapping)
	_ = json.Unmarshal(extra, &item.ExtraAuthParams)
	if item.Scopes == nil {
		item.Scopes = []string{}
	}
	item.Source = oauthdomain.SourceApp
	return &item, nil
}

func scanOAuthBinding(row interface{ Scan(...any) error }) (*oauthdomain.Binding, error) {
	var item oauthdomain.Binding
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Account, &item.Provider,
		&item.ProviderUserID, &item.UnionID, &raw, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	profile := map[string]any{}
	if json.Unmarshal(raw, &profile) == nil {
		item.Nickname = firstProfileString(profile, "nickname", "name", "login", "screen_name", "preferred_username")
		item.Avatar = firstProfileString(profile, "avatar_url", "picture", "headimgurl", "figureurl_qq_2", "avatar_large", "avatar")
		item.Email = firstProfileString(profile, "email")
	}
	return &item, nil
}

func firstProfileString(source map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := source[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultStringMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	return input
}
