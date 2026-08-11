package service

import (
	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	legacyrepo "aegis/internal/repository/legacymysql"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/taskpool"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type MigrationService struct {
	cfg    config.Config
	log    *zap.Logger
	legacy *legacyrepo.Repository
	pg     *pgrepo.Repository
}

type SyncResult struct {
	Requested  int   `json:"requested"`
	Synced     int   `json:"synced"`
	Skipped    int   `json:"skipped"`
	Failed     int   `json:"failed"`
	LastUserID int64 `json:"lastUserId"`
}

var errLegacyUserSkipped = errors.New("legacy user skipped")

func NewMigrationService(cfg config.Config, log *zap.Logger, legacy *legacyrepo.Repository, pg *pgrepo.Repository) *MigrationService {
	return &MigrationService{cfg: cfg, log: log, legacy: legacy, pg: pg}
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
	if len(users) > 0 {
		result.LastUserID = users[len(users)-1].ID
	}
	var mu sync.Mutex
	if err := taskpool.Dispatch(ctx, s.cfg.LegacyMySQL.Concurrency, users, func(taskCtx context.Context, legacyUser legacyrepo.LegacyUser) {
		if err := s.syncLegacyUser(taskCtx, legacyUser); err != nil {
			mu.Lock()
			defer mu.Unlock()
			if errors.Is(err, errLegacyUserSkipped) {
				result.Skipped++
				s.log.Info("sync legacy user skipped",
					zap.Int64("user_id", legacyUser.ID),
					zap.Int64("appid", legacyUser.AppID),
					zap.String("account", legacyUser.Account),
					zap.Error(err),
				)
				return
			}
			result.Failed++
			s.log.Error("sync legacy user failed", zap.Int64("user_id", legacyUser.ID), zap.Error(err))
			return
		}
		mu.Lock()
		result.Synced++
		mu.Unlock()
	}); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		result.Skipped = 0
	}
	return result, nil
}

func (s *MigrationService) FinalizeLegacySync(ctx context.Context) error {
	return s.pg.ResetUserIDSequence(ctx)
}

func (s *MigrationService) CountLegacyUsersAfterID(ctx context.Context, lastID int64) (int64, error) {
	return s.legacy.CountUsersAfterID(ctx, lastID)
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
		if errors.Is(err, pgrepo.ErrAccountAlreadyExists) {
			return fmt.Errorf("%w: appid=%d account=%s", errLegacyUserSkipped, user.AppID, user.Account)
		}
		return err
	}

	profile := userdomain.Profile{
		UserID:              legacyUser.ID,
		Nickname:            legacyUser.Name,
		Avatar:              legacyUser.Avatar,
		Email:               legacyUser.Email,
		Phone:               legacyUser.Phone,
		Role:                legacyUser.Role,
		MarkCode:            legacyUser.MarkCode,
		CustomID:            legacyUser.CustomID,
		CustomIDCount:       int64ToIntPtr(legacyUser.CustomIDCount),
		ParentInviteAccount: legacyUser.ParentInviteAccount,
		RegisterIP:          legacyUser.RegisterIP,
		RegisterISP:         legacyUser.RegisterISP,
		RegisterProvince:    legacyUser.RegisterProvince,
		RegisterCity:        legacyUser.RegisterCity,
		RegisterTime:        legacyUser.RegisterTime,
		DisabledReason:      legacyUser.Reason,
		Extra:               map[string]any{},
	}
	inviteCode, err := s.resolveLegacyInviteCode(ctx, legacyUser.ID, legacyUser.InviteCode)
	if err != nil {
		return err
	}
	profile.InviteCode = inviteCode
	if err := s.upsertLegacyProfileWithInviteCode(ctx, profile); err != nil {
		return err
	}
	if err := s.pg.UpsertUserSecurityState(ctx, userdomain.ProfileSecurityState{
		UserID:                 legacyUser.ID,
		PasswordChangedAt:      legacyUser.PasswordChangedAt,
		PasswordExpiresAt:      legacyUser.PasswordExpiresAt,
		PasswordStrengthScore:  int64ToIntPtr(legacyUser.PasswordStrengthScore),
		PasswordChangeRequired: new(legacyUser.PasswordChangeRequired),
	}); err != nil {
		return err
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

	return nil
}

func normalizeLegacyVIPTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	if value == 999999999 {
		return new(time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC))
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

func boolPtr(value bool) *bool {
	return &value
}

func int64ToIntPtr(value int64) *int {
	return new(int(value))
}

func (s *MigrationService) resolveLegacyInviteCode(ctx context.Context, userID int64, preferred string) (string, error) {
	preferred = strings.TrimSpace(preferred)
	if preferred != "" {
		existingProfile, err := s.pg.GetUserProfileByUserID(ctx, userID)
		if err != nil {
			return "", err
		}
		if existingProfile != nil && strings.EqualFold(strings.TrimSpace(existingProfile.InviteCode), preferred) {
			return preferred, nil
		}
		exists, err := s.pg.HasInviteCode(ctx, preferred)
		if err != nil {
			return "", err
		}
		if !exists {
			return preferred, nil
		}
	}
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		inviteCode, err := randomInviteCode(inviteCodeLength)
		if err != nil {
			return "", err
		}
		exists, err := s.pg.HasInviteCode(ctx, inviteCode)
		if err != nil {
			return "", err
		}
		if !exists {
			return inviteCode, nil
		}
	}
	return "", fmt.Errorf("generate invite code exceeded max retries")
}

func (s *MigrationService) upsertLegacyProfileWithInviteCode(ctx context.Context, profile userdomain.Profile) error {
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		if err := s.pg.UpsertUserProfile(ctx, profile); err != nil {
			if pgrepo.IsUniqueViolation(err) {
				inviteCode, retryErr := s.resolveLegacyInviteCode(ctx, profile.UserID, "")
				if retryErr != nil {
					return retryErr
				}
				profile.InviteCode = inviteCode
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("upsert legacy profile invite code exceeded max retries")
}
