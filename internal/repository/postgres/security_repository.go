package postgres

import (
	"context"
	"encoding/json"
	"time"

	securitydomain "aegis/internal/domain/security"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetUserTOTPSecret(ctx context.Context, appID int64, userID int64) (*securitydomain.TOTPSecretRecord, error) {
	query := `SELECT user_id, appid, secret_ciphertext, issuer, account_name, enabled, enabled_at, last_verified_at, created_at, updated_at
FROM user_totp_secrets
WHERE appid = $1 AND user_id = $2
LIMIT 1`
	return scanTOTPSecret(r.pool.QueryRow(ctx, query, appID, userID))
}

func (r *Repository) UpsertUserTOTPSecret(ctx context.Context, record securitydomain.TOTPSecretRecord) error {
	query := `INSERT INTO user_totp_secrets (appid, user_id, secret_ciphertext, issuer, account_name, enabled, enabled_at, last_verified_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, NOW()), $8, NOW(), NOW())
ON CONFLICT (user_id) DO UPDATE SET
    appid = EXCLUDED.appid,
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    issuer = EXCLUDED.issuer,
    account_name = EXCLUDED.account_name,
    enabled = EXCLUDED.enabled,
    enabled_at = EXCLUDED.enabled_at,
    last_verified_at = EXCLUDED.last_verified_at,
    updated_at = NOW()`
	_, err := r.pool.Exec(ctx, query,
		record.AppID,
		record.UserID,
		record.SecretCipher,
		record.Issuer,
		record.AccountName,
		record.Enabled,
		nullableTime(derefTimePtr(record.EnabledAt)),
		nullableTime(derefTimePtr(record.LastVerifiedAt)),
	)
	return err
}

func (r *Repository) DeleteUserTOTPSecret(ctx context.Context, appID int64, userID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_totp_secrets WHERE appid = $1 AND user_id = $2`, appID, userID)
	return err
}

func (r *Repository) ListUserRecoveryCodes(ctx context.Context, appID int64, userID int64) ([]securitydomain.RecoveryCodeRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, appid, code_hash, code_hint, used_at, created_at, updated_at
FROM user_recovery_codes
WHERE appid = $1 AND user_id = $2
ORDER BY id ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.RecoveryCodeRecord, 0, 12)
	for rows.Next() {
		item, err := scanRecoveryCode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ReplaceUserRecoveryCodes(ctx context.Context, appID int64, userID int64, records []securitydomain.RecoveryCodeRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err = tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE appid = $1 AND user_id = $2`, appID, userID); err != nil {
		return err
	}

	for _, record := range records {
		if _, err = tx.Exec(ctx, `INSERT INTO user_recovery_codes (appid, user_id, code_hash, code_hint, used_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`, appID, userID, record.CodeHash, record.CodeHint, nullableTime(derefTimePtr(record.UsedAt))); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) MarkRecoveryCodeUsed(ctx context.Context, appID int64, userID int64, codeHash string, usedAt time.Time) (bool, error) {
	result, err := r.pool.Exec(ctx, `UPDATE user_recovery_codes
SET used_at = $4, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND code_hash = $3 AND used_at IS NULL`, appID, userID, codeHash, usedAt.UTC())
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteUserRecoveryCodes(ctx context.Context, appID int64, userID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_recovery_codes WHERE appid = $1 AND user_id = $2`, appID, userID)
	return err
}

func (r *Repository) ListUserPasskeys(ctx context.Context, appID int64, userID int64) ([]securitydomain.PasskeyRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, user_id, appid, credential_id, COALESCE(credential_name, ''), credential_json, aaguid, sign_count, last_used_at, created_at, updated_at
FROM user_passkeys
WHERE appid = $1 AND user_id = $2
ORDER BY created_at ASC, id ASC`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.PasskeyRecord, 0, 4)
	for rows.Next() {
		item, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetUserPasskeyByCredentialID(ctx context.Context, appID int64, credentialID []byte) (*securitydomain.PasskeyRecord, error) {
	query := `SELECT id, user_id, appid, credential_id, COALESCE(credential_name, ''), credential_json, aaguid, sign_count, last_used_at, created_at, updated_at
FROM user_passkeys
WHERE appid = $1 AND credential_id = $2
LIMIT 1`
	return scanPasskey(r.pool.QueryRow(ctx, query, appID, credentialID))
}

func (r *Repository) CreateUserPasskey(ctx context.Context, record securitydomain.PasskeyRecord) (*securitydomain.PasskeyRecord, error) {
	query := `INSERT INTO user_passkeys (appid, user_id, credential_id, credential_name, credential_json, aaguid, sign_count, last_used_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
RETURNING id, user_id, appid, credential_id, COALESCE(credential_name, ''), credential_json, aaguid, sign_count, last_used_at, created_at, updated_at`
	return scanPasskey(r.pool.QueryRow(ctx, query,
		record.AppID,
		record.UserID,
		record.CredentialID,
		nullableString(record.CredentialName),
		json.RawMessage(record.CredentialJSON),
		record.AAGUID,
		record.SignCount,
		nullableTime(derefTimePtr(record.LastUsedAt)),
	))
}

func (r *Repository) UpdateUserPasskeyCredential(ctx context.Context, appID int64, userID int64, credentialID []byte, credentialJSON []byte, signCount uint32, lastUsedAt *time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_passkeys
SET credential_json = $4,
    sign_count = $5,
    last_used_at = COALESCE($6, last_used_at),
    updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND credential_id = $3`, appID, userID, credentialID, json.RawMessage(credentialJSON), signCount, nullableTime(derefTimePtr(lastUsedAt)))
	return err
}

func (r *Repository) DeleteUserPasskey(ctx context.Context, appID int64, userID int64, credentialID []byte) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM user_passkeys WHERE appid = $1 AND user_id = $2 AND credential_id = $3`, appID, userID, credentialID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func scanTOTPSecret(row interface{ Scan(dest ...any) error }) (*securitydomain.TOTPSecretRecord, error) {
	var item securitydomain.TOTPSecretRecord
	if err := row.Scan(&item.UserID, &item.AppID, &item.SecretCipher, &item.Issuer, &item.AccountName, &item.Enabled, &item.EnabledAt, &item.LastVerifiedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func scanRecoveryCode(row interface{ Scan(dest ...any) error }) (*securitydomain.RecoveryCodeRecord, error) {
	var item securitydomain.RecoveryCodeRecord
	if err := row.Scan(&item.ID, &item.UserID, &item.AppID, &item.CodeHash, &item.CodeHint, &item.UsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func scanPasskey(row interface{ Scan(dest ...any) error }) (*securitydomain.PasskeyRecord, error) {
	var (
		item      securitydomain.PasskeyRecord
		rawJSON   []byte
		rawAAGUID []byte
		signCount int64
	)
	if err := row.Scan(&item.ID, &item.UserID, &item.AppID, &item.CredentialID, &item.CredentialName, &rawJSON, &rawAAGUID, &signCount, &item.LastUsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.CredentialJSON = rawJSON
	item.AAGUID = rawAAGUID
	if signCount > 0 {
		item.SignCount = uint32(signCount)
	}
	return &item, nil
}

func derefTimePtr(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

var _ = pgx.ErrNoRows
