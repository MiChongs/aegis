package service

import (
	"context"
	"fmt"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	legacyrepo "aegis/internal/repository/legacymysql"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	"go.uber.org/zap"
)

type MigrationService struct {
	cfg       config.Config
	log       *zap.Logger
	legacy    *legacyrepo.Repository
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
}

type SyncResult struct {
	Requested  int   `json:"requested"`
	Synced     int   `json:"synced"`
	Skipped    int   `json:"skipped"`
	Failed     int   `json:"failed"`
	LastUserID int64 `json:"lastUserId"`
}

func NewMigrationService(cfg config.Config, log *zap.Logger, legacy *legacyrepo.Repository, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher) *MigrationService {
	return &MigrationService{cfg: cfg, log: log, legacy: legacy, pg: pg, sessions: sessions, publisher: publisher}
}

func (s *MigrationService) SyncLegacyUserByID(ctx context.Context, userID int64) error {
	legacyUser, err := s.legacy.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if legacyUser == nil {
		return fmt.Errorf("legacy user %d not found", userID)
	}
	return s.syncLegacyUser(ctx, *legacyUser)
}

func (s *MigrationService) SyncLegacyUsersBatch(ctx context.Context, lastID int64, limit int) (*SyncResult, error) {
	if limit <= 0 {
		limit = s.cfg.LegacyMySQL.BatchSize
	}
	users, err := s.legacy.ListUsersAfterID(ctx, lastID, limit)
	if err != nil {
		return nil, err
	}
	result := &SyncResult{Requested: len(users)}
	for _, legacyUser := range users {
		result.LastUserID = legacyUser.ID
		if err := s.syncLegacyUser(ctx, legacyUser); err != nil {
			result.Failed++
			s.log.Error("sync legacy user failed", zap.Int64("user_id", legacyUser.ID), zap.Error(err))
			continue
		}
		result.Synced++
	}
	if len(users) == 0 {
		result.Skipped = 0
	}
	if err := s.pg.ResetUserIDSequence(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (s *MigrationService) syncLegacyUser(ctx context.Context, legacyUser legacyrepo.LegacyUser) error {
	account := legacyUser.Account
	if account == "" {
		account = fmt.Sprintf("legacy_%d", legacyUser.ID)
	}
	user := userdomain.User{
		ID:              legacyUser.ID,
		AppID:           legacyUser.AppID,
		Account:         account,
		PasswordHash:    legacyUser.Password,
		Integral:        legacyUser.Integral,
		Experience:      legacyUser.Experience,
		Enabled:         legacyUser.Enabled,
		DisabledEndTime: legacyUser.DisabledEndTime,
		VIPExpireAt:     normalizeLegacyVIPTime(legacyUser.VIPTime),
		CreatedAt:       zeroOrNow(legacyUser.CreatedAt),
		UpdatedAt:       zeroOrNow(legacyUser.UpdatedAt),
	}
	if err := s.pg.UpsertImportedUser(ctx, user); err != nil {
		return err
	}

	profile := userdomain.Profile{
		UserID:   legacyUser.ID,
		Nickname: legacyUser.Name,
		Avatar:   legacyUser.Avatar,
		Email:    legacyUser.Email,
		Extra: map[string]any{
			"phone":                    legacyUser.Phone,
			"role":                     legacyUser.Role,
			"markcode":                 legacyUser.MarkCode,
			"integral":                 legacyUser.Integral,
			"experience":               legacyUser.Experience,
			"register_ip":              legacyUser.RegisterIP,
			"register_time":            formatTime(legacyUser.RegisterTime),
			"register_province":        legacyUser.RegisterProvince,
			"register_city":            legacyUser.RegisterCity,
			"register_isp":             legacyUser.RegisterISP,
			"disabled_reason":          legacyUser.Reason,
			"parent_invite_account":    legacyUser.ParentInviteAccount,
			"invite_code":              legacyUser.InviteCode,
			"custom_id":                legacyUser.CustomID,
			"custom_id_count":          legacyUser.CustomIDCount,
			"two_factor_enabled":       legacyUser.TwoFactorEnabled,
			"two_factor_method":        legacyUser.TwoFactorMethod,
			"two_factor_enabled_at":    formatTime(legacyUser.TwoFactorEnabledAt),
			"two_factor_disabled_at":   formatTime(legacyUser.TwoFactorDisabledAt),
			"passkey_enabled":          legacyUser.PasskeyEnabled,
			"passkey_enabled_at":       formatTime(legacyUser.PasskeyEnabledAt),
			"password_changed_at":      formatTime(legacyUser.PasswordChangedAt),
			"password_expires_at":      formatTime(legacyUser.PasswordExpiresAt),
			"password_change_required": legacyUser.PasswordChangeRequired,
			"password_strength_score":  legacyUser.PasswordStrengthScore,
			"legacy_vip_time":          legacyUser.VIPTime,
		},
	}
	if err := s.pg.UpsertUserProfile(ctx, profile); err != nil {
		return err
	}

	settings, err := s.legacy.GetUserSettings(ctx, legacyUser.ID, legacyUser.AppID)
	if err != nil {
		return err
	}
	for _, item := range settings {
		if err := s.pg.UpsertUserSettings(ctx, userdomain.Settings{
			UserID:    legacyUser.ID,
			Category:  item.Category,
			Settings:  item.Settings,
			Version:   item.Version,
			IsActive:  item.IsActive,
			UpdatedAt: derefTime(item.LastModified),
		}); err != nil {
			return err
		}
	}

	if legacyUser.OpenQQ != "" {
		if err := s.pg.UpsertOAuthBinding(ctx, legacyUser.AppID, legacyUser.ID, authdomain.ProviderProfile{
			Provider:       "qq",
			ProviderUserID: legacyUser.OpenQQ,
			RawProfile:     map[string]any{"source": "legacy_mysql"},
		}); err != nil {
			return err
		}
	}
	if legacyUser.OpenWechat != "" {
		if err := s.pg.UpsertOAuthBinding(ctx, legacyUser.AppID, legacyUser.ID, authdomain.ProviderProfile{
			Provider:       "wechat",
			ProviderUserID: legacyUser.OpenWechat,
			RawProfile:     map[string]any{"source": "legacy_mysql"},
		}); err != nil {
			return err
		}
	}

	_ = s.sessions.DeleteMyView(ctx, legacyUser.AppID, legacyUser.ID)
	_ = s.sessions.DeleteUserProfile(ctx, legacyUser.AppID, legacyUser.ID)
	_ = s.sessions.DeleteSecurityStatus(ctx, legacyUser.AppID, legacyUser.ID)
	for _, category := range []string{"general", "autoSign", "notifications", "privacy", "ui", "security"} {
		_ = s.sessions.DeleteUserSettings(ctx, legacyUser.AppID, legacyUser.ID, category)
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectUserProfileRefresh, map[string]any{"user_id": legacyUser.ID, "appid": legacyUser.AppID, "source": "legacy_mysql_sync"})
	return nil
}

func normalizeLegacyVIPTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	if value == 999999999 {
		permanent := time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC)
		return &permanent
	}
	parsed := time.Unix(value, 0).UTC()
	if parsed.Year() < 2000 || parsed.Year() > 2100 {
		return nil
	}
	return &parsed
}

func zeroOrNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
