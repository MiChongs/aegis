package postgres

import (
	"context"
	"encoding/json"

	authprotocol "aegis/internal/domain/authprotocol"
)

const authProtocolPolicyColumns = `appid, protocol_version, identifiers, login_methods,
register_methods, registration_schema, require_captcha, auto_login_after_register,
security_level, allow_legacy, signing_secret_cipher, signing_secret_hint,
signing_secret_rotated_at, created_at, updated_at`

func (r *Repository) GetAppAuthProtocolPolicy(ctx context.Context, appID int64) (*authprotocol.Policy, error) {
	return scanAuthProtocolPolicy(r.pool.QueryRow(ctx,
		`SELECT `+authProtocolPolicyColumns+` FROM app_auth_protocol_policies WHERE appid=$1`, appID))
}

// UpsertAppAuthProtocolPolicy 只写策略字段；签名密钥有独立的轮换入口，
// 避免保存一次表单就把密钥冲掉。
func (r *Repository) UpsertAppAuthProtocolPolicy(ctx context.Context, policy authprotocol.Policy) (*authprotocol.Policy, error) {
	schema, _ := json.Marshal(policy.RegistrationSchema)
	return scanAuthProtocolPolicy(r.pool.QueryRow(ctx, `INSERT INTO app_auth_protocol_policies
(appid, protocol_version, identifiers, login_methods, register_methods, registration_schema,
require_captcha, auto_login_after_register, security_level, allow_legacy)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (appid) DO UPDATE SET
identifiers=EXCLUDED.identifiers, login_methods=EXCLUDED.login_methods,
register_methods=EXCLUDED.register_methods, registration_schema=EXCLUDED.registration_schema,
require_captcha=EXCLUDED.require_captcha, auto_login_after_register=EXCLUDED.auto_login_after_register,
security_level=EXCLUDED.security_level, allow_legacy=EXCLUDED.allow_legacy, updated_at=NOW()
RETURNING `+authProtocolPolicyColumns,
		policy.AppID, authprotocol.ProtocolVersion, policy.Identifiers, policy.LoginMethods,
		policy.RegisterMethods, schema, policy.RequireCaptcha, policy.AutoLogin,
		policy.SecurityLevel, policy.AllowLegacy))
}

// SetAppAuthProtocolSigningSecret 轮换签名密钥。策略行可能尚不存在（应用从未保存过策略），
// 因此用 upsert 并让其余列走建表默认值。
func (r *Repository) SetAppAuthProtocolSigningSecret(ctx context.Context, appID int64, cipher, hint string) (*authprotocol.Policy, error) {
	return scanAuthProtocolPolicy(r.pool.QueryRow(ctx, `INSERT INTO app_auth_protocol_policies
(appid, signing_secret_cipher, signing_secret_hint, signing_secret_rotated_at)
VALUES ($1,$2,$3,NOW())
ON CONFLICT (appid) DO UPDATE SET
signing_secret_cipher=EXCLUDED.signing_secret_cipher,
signing_secret_hint=EXCLUDED.signing_secret_hint,
signing_secret_rotated_at=NOW(), updated_at=NOW()
RETURNING `+authProtocolPolicyColumns, appID, cipher, hint))
}

func (r *Repository) GetActiveAppTransportKey(ctx context.Context, appID int64) (*authprotocol.TransportKey, error) {
	return scanTransportKey(r.pool.QueryRow(ctx, transportKeySelect+`
WHERE appid=$1 AND status='active' AND not_before<=NOW() AND not_after>NOW()
ORDER BY created_at DESC LIMIT 1`, appID))
}

func (r *Repository) GetUsableAppTransportKey(ctx context.Context, appID int64, keyID string) (*authprotocol.TransportKey, error) {
	return scanTransportKey(r.pool.QueryRow(ctx, transportKeySelect+`
WHERE appid=$1 AND key_id=$2 AND status IN ('active','retiring')
AND not_before<=NOW() AND not_after>NOW() LIMIT 1`, appID, keyID))
}

func (r *Repository) ListPublicAppTransportKeys(ctx context.Context, appID int64) ([]authprotocol.TransportKey, error) {
	rows, err := r.pool.Query(ctx, transportKeySelect+`
WHERE appid=$1 AND status IN ('active','retiring') AND not_after>NOW()
ORDER BY status='active' DESC, created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]authprotocol.TransportKey, 0, 2)
	for rows.Next() {
		item, err := scanTransportKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListAppTransportKeys(ctx context.Context, appID int64) ([]authprotocol.TransportKey, error) {
	rows, err := r.pool.Query(ctx, transportKeySelect+`
WHERE appid=$1 ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]authprotocol.TransportKey, 0, 4)
	for rows.Next() {
		item, err := scanTransportKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) RotateAppTransportKey(ctx context.Context, item authprotocol.TransportKey) (*authprotocol.TransportKey, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// 跨实例串行化同一 App 的密钥轮换，避免并发 Bootstrap/管理员操作撞上
	// “每个 App 仅一个 active key”的部分唯一索引。
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, item.AppID); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `UPDATE app_transport_keys SET status='retiring',
not_after=LEAST(not_after, NOW()+INTERVAL '24 hours')
WHERE appid=$1 AND status='active'`, item.AppID); err != nil {
		return nil, err
	}
	created, err := scanTransportKey(tx.QueryRow(ctx, `INSERT INTO app_transport_keys
(appid,key_id,algorithm,public_key,private_key_cipher,status,not_before,not_after)
VALUES ($1,$2,$3,$4,$5,'active',$6,$7)
RETURNING id, appid, key_id, algorithm, public_key, private_key_cipher, status,
not_before, not_after, created_at, revoked_at`,
		item.AppID, item.KeyID, item.Algorithm, item.PublicKey, item.PrivateKeyCipher,
		item.NotBefore, item.NotAfter))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *Repository) RevokeAppTransportKey(ctx context.Context, appID int64, keyID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE app_transport_keys SET status='revoked', revoked_at=NOW()
WHERE appid=$1 AND key_id=$2`, appID, keyID)
	return err
}

func (r *Repository) HasUserRegisteredFromIPForApp(ctx context.Context, appID int64, ip string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM user_profiles p JOIN users u ON u.id=p.user_id
WHERE u.appid=$1 AND COALESCE(p.register_ip, COALESCE(p.extra->>'register_ip',''))=$2
LIMIT 1)`, appID, ip).Scan(&exists)
	return exists, err
}

const transportKeySelect = `SELECT id, appid, key_id, algorithm, public_key, private_key_cipher,
status, not_before, not_after, created_at, revoked_at FROM app_transport_keys `

func scanAuthProtocolPolicy(row interface{ Scan(...any) error }) (*authprotocol.Policy, error) {
	var item authprotocol.Policy
	var schema []byte
	var cipher, hint *string
	if err := row.Scan(&item.AppID, &item.ProtocolVersion, &item.Identifiers, &item.LoginMethods,
		&item.RegisterMethods, &schema, &item.RequireCaptcha, &item.AutoLogin,
		&item.SecurityLevel, &item.AllowLegacy, &cipher, &hint,
		&item.SigningSecretRotatedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if cipher != nil {
		item.SigningSecretCipher = *cipher
		item.SigningSecretSet = true
	}
	if hint != nil {
		item.SigningSecretHint = *hint
	}
	_ = json.Unmarshal(schema, &item.RegistrationSchema)
	return &item, nil
}

func scanTransportKey(row interface{ Scan(...any) error }) (*authprotocol.TransportKey, error) {
	var item authprotocol.TransportKey
	if err := row.Scan(&item.ID, &item.AppID, &item.KeyID, &item.Algorithm, &item.PublicKey,
		&item.PrivateKeyCipher, &item.Status, &item.NotBefore, &item.NotAfter,
		&item.CreatedAt, &item.RevokedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}
