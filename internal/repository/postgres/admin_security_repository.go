package postgres

import (
	"context"
	"encoding/json"
	"time"

	admindomain "aegis/internal/domain/admin"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) GetAdminTOTPSecret(ctx context.Context, adminID int64) (*admindomain.TOTPSecretRecord, error) {
	query := `SELECT admin_id, secret_ciphertext, issuer, account_name, enabled, enabled_at, last_verified_at, created_at, updated_at
FROM admin_totp_secrets
WHERE admin_id = $1
LIMIT 1`
	return scanAdminTOTPSecret(r.pool.QueryRow(ctx, query, adminID))
}

func (r *Repository) UpsertAdminTOTPSecret(ctx context.Context, record admindomain.TOTPSecretRecord) error {
	query := `INSERT INTO admin_totp_secrets (admin_id, secret_ciphertext, issuer, account_name, enabled, enabled_at, last_verified_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, COALESCE($6, NOW()), $7, NOW(), NOW())
ON CONFLICT (admin_id) DO UPDATE SET
    secret_ciphertext = EXCLUDED.secret_ciphertext,
    issuer = EXCLUDED.issuer,
    account_name = EXCLUDED.account_name,
    enabled = EXCLUDED.enabled,
    enabled_at = EXCLUDED.enabled_at,
    last_verified_at = EXCLUDED.last_verified_at,
    updated_at = NOW()`
	_, err := r.pool.Exec(ctx, query,
		record.AdminID,
		record.SecretCipher,
		record.Issuer,
		record.AccountName,
		record.Enabled,
		nullableTime(derefTimePtr(record.EnabledAt)),
		nullableTime(derefTimePtr(record.LastVerifiedAt)),
	)
	return err
}

func (r *Repository) DeleteAdminTOTPSecret(ctx context.Context, adminID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM admin_totp_secrets WHERE admin_id = $1`, adminID)
	return err
}

func (r *Repository) ListAdminRecoveryCodes(ctx context.Context, adminID int64) ([]admindomain.RecoveryCodeRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, admin_id, code_hash, code_hint, used_at, created_at, updated_at
FROM admin_recovery_codes
WHERE admin_id = $1
ORDER BY id ASC`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]admindomain.RecoveryCodeRecord, 0, 12)
	for rows.Next() {
		item, err := scanAdminRecoveryCode(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ReplaceAdminRecoveryCodes(ctx context.Context, adminID int64, records []admindomain.RecoveryCodeRecord) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err = tx.Exec(ctx, `DELETE FROM admin_recovery_codes WHERE admin_id = $1`, adminID); err != nil {
		return err
	}

	for _, record := range records {
		if _, err = tx.Exec(ctx, `INSERT INTO admin_recovery_codes (admin_id, code_hash, code_hint, used_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())`, adminID, record.CodeHash, record.CodeHint, nullableTime(derefTimePtr(record.UsedAt))); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *Repository) MarkAdminRecoveryCodeUsed(ctx context.Context, adminID int64, codeHash string, usedAt time.Time) (bool, error) {
	result, err := r.pool.Exec(ctx, `UPDATE admin_recovery_codes
SET used_at = $3, updated_at = NOW()
WHERE admin_id = $1 AND code_hash = $2 AND used_at IS NULL`, adminID, codeHash, usedAt.UTC())
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteAdminRecoveryCodes(ctx context.Context, adminID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM admin_recovery_codes WHERE admin_id = $1`, adminID)
	return err
}

func (r *Repository) ListAdminPasskeys(ctx context.Context, adminID int64) ([]admindomain.PasskeyRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, admin_id, credential_id, COALESCE(credential_name, ''), credential_json, aaguid, sign_count, last_used_at, created_at, updated_at
FROM admin_passkeys
WHERE admin_id = $1
ORDER BY created_at ASC, id ASC`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]admindomain.PasskeyRecord, 0, 4)
	for rows.Next() {
		item, err := scanAdminPasskey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateAdminPasskey(ctx context.Context, record admindomain.PasskeyRecord) (*admindomain.PasskeyRecord, error) {
	query := `INSERT INTO admin_passkeys (admin_id, credential_id, credential_name, credential_json, aaguid, sign_count, last_used_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
RETURNING id, admin_id, credential_id, COALESCE(credential_name, ''), credential_json, aaguid, sign_count, last_used_at, created_at, updated_at`
	return scanAdminPasskey(r.pool.QueryRow(ctx, query,
		record.AdminID,
		record.CredentialID,
		nullableString(record.CredentialName),
		json.RawMessage(record.CredentialJSON),
		record.AAGUID,
		record.SignCount,
		nullableTime(derefTimePtr(record.LastUsedAt)),
	))
}

func (r *Repository) DeleteAdminPasskey(ctx context.Context, adminID int64, credentialID []byte) (int64, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM admin_passkeys WHERE admin_id = $1 AND credential_id = $2`, adminID, credentialID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func scanAdminTOTPSecret(row interface{ Scan(dest ...any) error }) (*admindomain.TOTPSecretRecord, error) {
	var item admindomain.TOTPSecretRecord
	if err := row.Scan(&item.AdminID, &item.SecretCipher, &item.Issuer, &item.AccountName, &item.Enabled, &item.EnabledAt, &item.LastVerifiedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func scanAdminRecoveryCode(row interface{ Scan(dest ...any) error }) (*admindomain.RecoveryCodeRecord, error) {
	var item admindomain.RecoveryCodeRecord
	if err := row.Scan(&item.ID, &item.AdminID, &item.CodeHash, &item.CodeHint, &item.UsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func scanAdminPasskey(row interface{ Scan(dest ...any) error }) (*admindomain.PasskeyRecord, error) {
	var (
		item      admindomain.PasskeyRecord
		rawJSON   []byte
		rawAAGUID []byte
		signCount int64
	)
	if err := row.Scan(&item.ID, &item.AdminID, &item.CredentialID, &item.CredentialName, &rawJSON, &rawAAGUID, &signCount, &item.LastUsedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.CredentialJSON = rawJSON
	item.AAGUID = rawAAGUID
	if signCount > 0 {
		item.SignCount = uint32(signCount)
	}
	return &item, nil
}

var _ = pgx.ErrNoRows
