package legacymysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

type LegacyUser struct {
	ID                     int64
	AppID                  int64
	Account                string
	Password               string
	Name                   string
	Avatar                 string
	Email                  string
	Phone                  string
	Enabled                bool
	DisabledEndTime        *time.Time
	VIPTime                int64
	Role                   string
	MarkCode               string
	OpenQQ                 string
	OpenWechat             string
	Integral               int64
	Experience             int64
	RegisterIP             string
	RegisterTime           *time.Time
	RegisterProvince       string
	RegisterCity           string
	RegisterISP            string
	Reason                 string
	ParentInviteAccount    string
	InviteCode             string
	CustomID               string
	CustomIDCount          int64
	TwoFactorEnabled       bool
	TwoFactorMethod        string
	TwoFactorEnabledAt     *time.Time
	TwoFactorDisabledAt    *time.Time
	PasskeyEnabled         bool
	PasskeyEnabledAt       *time.Time
	PasswordChangedAt      *time.Time
	PasswordExpiresAt      *time.Time
	PasswordChangeRequired bool
	PasswordStrengthScore  int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type LegacyUserSettings struct {
	UserID       int64
	AppID        int64
	Category     string
	Settings     map[string]any
	Version      int
	LastModified *time.Time
	IsActive     bool
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetUserByID(ctx context.Context, userID int64) (*LegacyUser, error) {
	query := `
SELECT id, appid, COALESCE(account, ''), COALESCE(password, ''), COALESCE(name, ''), COALESCE(avatar, ''),
       COALESCE(email, ''), COALESCE(phone, ''), COALESCE(enabled, TRUE), disabledEndTime, COALESCE(vip_time, 0),
       COALESCE(role, 'user'), COALESCE(markcode, ''), COALESCE(open_qq, ''), COALESCE(open_wechat, ''),
       COALESCE(integral, 0), COALESCE(experience, 0), COALESCE(register_ip, ''), register_time,
       COALESCE(register_province, ''), COALESCE(register_city, ''), COALESCE(register_isp, ''), COALESCE(reason, ''),
       COALESCE(parent_invite_account, ''), COALESCE(invite_code, ''), COALESCE(customId, ''), COALESCE(customIdCount, 0),
       COALESCE(two_factor_enabled, FALSE), COALESCE(two_factor_method, ''), two_factor_enabled_at, two_factor_disabled_at,
       COALESCE(passkey_enabled, FALSE), passkey_enabled_at, password_changed_at, password_expires_at,
       COALESCE(password_change_required, FALSE), COALESCE(password_strength_score, 0), created_at, updated_at
FROM ` + "`user`" + ` WHERE id = ? LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, userID)
	return scanLegacyUser(row)
}

func (r *Repository) ListUsersAfterID(ctx context.Context, lastID int64, limit int) ([]LegacyUser, error) {
	query := `
SELECT id, appid, COALESCE(account, ''), COALESCE(password, ''), COALESCE(name, ''), COALESCE(avatar, ''),
       COALESCE(email, ''), COALESCE(phone, ''), COALESCE(enabled, TRUE), disabledEndTime, COALESCE(vip_time, 0),
       COALESCE(role, 'user'), COALESCE(markcode, ''), COALESCE(open_qq, ''), COALESCE(open_wechat, ''),
       COALESCE(integral, 0), COALESCE(experience, 0), COALESCE(register_ip, ''), register_time,
       COALESCE(register_province, ''), COALESCE(register_city, ''), COALESCE(register_isp, ''), COALESCE(reason, ''),
       COALESCE(parent_invite_account, ''), COALESCE(invite_code, ''), COALESCE(customId, ''), COALESCE(customIdCount, 0),
       COALESCE(two_factor_enabled, FALSE), COALESCE(two_factor_method, ''), two_factor_enabled_at, two_factor_disabled_at,
       COALESCE(passkey_enabled, FALSE), passkey_enabled_at, password_changed_at, password_expires_at,
       COALESCE(password_change_required, FALSE), COALESCE(password_strength_score, 0), created_at, updated_at
FROM ` + "`user`" + ` WHERE id > ? ORDER BY id ASC LIMIT ?`
	rows, err := r.db.QueryContext(ctx, query, lastID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]LegacyUser, 0, limit)
	for rows.Next() {
		user, err := scanLegacyUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, rows.Err()
}

func (r *Repository) GetUserSettings(ctx context.Context, userID int64, appID int64) ([]LegacyUserSettings, error) {
	query := `SELECT userId, appid, COALESCE(category, 'general'), settings, COALESCE(version, 1), lastModified, COALESCE(isActive, TRUE) FROM UserSettings WHERE userId = ? AND appid = ? ORDER BY id ASC`
	rows, err := r.db.QueryContext(ctx, query, userID, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make([]LegacyUserSettings, 0, 8)
	for rows.Next() {
		var item LegacyUserSettings
		var rawSettings any
		var lastModified sql.NullTime
		if err := rows.Scan(&item.UserID, &item.AppID, &item.Category, &rawSettings, &item.Version, &lastModified, &item.IsActive); err != nil {
			return nil, err
		}
		parsed, err := parseJSONValue(rawSettings)
		if err != nil {
			return nil, fmt.Errorf("parse legacy settings failed for user %d category %s: %w", userID, item.Category, err)
		}
		item.Settings = parsed
		if lastModified.Valid {
			value := lastModified.Time.UTC()
			item.LastModified = &value
		}
		settings = append(settings, item)
	}
	return settings, rows.Err()
}

func scanLegacyUser(scanner interface{ Scan(dest ...any) error }) (*LegacyUser, error) {
	var item LegacyUser
	var disabledEndTime sql.NullTime
	var registerTime sql.NullTime
	var twoFactorEnabledAt sql.NullTime
	var twoFactorDisabledAt sql.NullTime
	var passkeyEnabledAt sql.NullTime
	var passwordChangedAt sql.NullTime
	var passwordExpiresAt sql.NullTime
	if err := scanner.Scan(
		&item.ID,
		&item.AppID,
		&item.Account,
		&item.Password,
		&item.Name,
		&item.Avatar,
		&item.Email,
		&item.Phone,
		&item.Enabled,
		&disabledEndTime,
		&item.VIPTime,
		&item.Role,
		&item.MarkCode,
		&item.OpenQQ,
		&item.OpenWechat,
		&item.Integral,
		&item.Experience,
		&item.RegisterIP,
		&registerTime,
		&item.RegisterProvince,
		&item.RegisterCity,
		&item.RegisterISP,
		&item.Reason,
		&item.ParentInviteAccount,
		&item.InviteCode,
		&item.CustomID,
		&item.CustomIDCount,
		&item.TwoFactorEnabled,
		&item.TwoFactorMethod,
		&twoFactorEnabledAt,
		&twoFactorDisabledAt,
		&item.PasskeyEnabled,
		&passkeyEnabledAt,
		&passwordChangedAt,
		&passwordExpiresAt,
		&item.PasswordChangeRequired,
		&item.PasswordStrengthScore,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	if disabledEndTime.Valid {
		value := disabledEndTime.Time.UTC()
		item.DisabledEndTime = &value
	}
	if registerTime.Valid {
		value := registerTime.Time.UTC()
		item.RegisterTime = &value
	}
	if twoFactorEnabledAt.Valid {
		value := twoFactorEnabledAt.Time.UTC()
		item.TwoFactorEnabledAt = &value
	}
	if twoFactorDisabledAt.Valid {
		value := twoFactorDisabledAt.Time.UTC()
		item.TwoFactorDisabledAt = &value
	}
	if passkeyEnabledAt.Valid {
		value := passkeyEnabledAt.Time.UTC()
		item.PasskeyEnabledAt = &value
	}
	if passwordChangedAt.Valid {
		value := passwordChangedAt.Time.UTC()
		item.PasswordChangedAt = &value
	}
	if passwordExpiresAt.Valid {
		value := passwordExpiresAt.Time.UTC()
		item.PasswordExpiresAt = &value
	}
	return &item, nil
}

func parseJSONValue(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	switch value := raw.(type) {
	case []byte:
		return decodeJSONBytes(value)
	case string:
		return decodeJSONBytes([]byte(value))
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return decodeJSONBytes(data)
	}
}

func decodeJSONBytes(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	switch value := result.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return value, nil
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return map[string]any{}, nil
		}
		return decodeJSONBytes([]byte(value))
	default:
		return nil, fmt.Errorf("expected JSON object, got %T", value)
	}
}
