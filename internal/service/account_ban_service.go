package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/config"
	plugindomain "aegis/internal/domain/plugin"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type AccountBanService struct {
	log              *zap.Logger
	pg               *pgrepo.Repository
	sessions         *redisrepo.SessionRepository
	plugin           *PluginService
	cron             *cron.Cron
	cleanupBatchSize int
}

func NewAccountBanService(cfg config.AccountBanConfig, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) (*AccountBanService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	service := &AccountBanService{
		log:              log,
		pg:               pg,
		sessions:         sessions,
		cleanupBatchSize: cfg.CleanupBatchSize,
	}
	if !cfg.CleanupEnabled {
		return service, nil
	}

	scheduler := cron.New(cron.WithLocation(time.UTC))
	if _, err := scheduler.AddFunc(cfg.CleanupSpec, service.runCleanupJob); err != nil {
		return nil, err
	}
	service.cron = scheduler
	return service, nil
}

func (s *AccountBanService) SetPluginService(plugin *PluginService) {
	s.plugin = plugin
}

func (s *AccountBanService) Start() {
	if s.cron != nil {
		s.cron.Start()
	}
}

func (s *AccountBanService) Close(ctx context.Context) error {
	if s.cron == nil {
		return nil
	}
	stopCtx := s.cron.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *AccountBanService) BanUser(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	normalized, err := normalizeAccountBanCreateInput(input)
	if err != nil {
		return nil, err
	}
	item, err := s.pg.CreateUserAccountBan(ctx, appID, userID, normalized)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	s.revokeUserSessions(ctx, appID, userID)
	s.emitUserBannedHook(appID, userID, item)
	return item, nil
}

func (s *AccountBanService) BatchBanUsers(ctx context.Context, appID int64, input userdomain.AccountBanBatchCreateInput) (*userdomain.AccountBanBatchCreateResult, error) {
	if len(input.UserIDs) == 0 {
		return nil, apperrors.New(40024, http.StatusBadRequest, "用户标识不能为空")
	}
	normalized, err := normalizeAccountBanCreateInput(input.AccountBanCreateInput)
	if err != nil {
		return nil, err
	}

	result := &userdomain.AccountBanBatchCreateResult{
		AppID:            appID,
		Requested:        len(input.UserIDs),
		ProcessedUserIDs: make([]int64, 0, len(input.UserIDs)),
		FailedUserIDs:    make([]int64, 0),
	}
	seen := make(map[int64]struct{}, len(input.UserIDs))
	for _, userID := range input.UserIDs {
		if userID <= 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		result.ProcessedUserIDs = append(result.ProcessedUserIDs, userID)
		item, err := s.pg.CreateUserAccountBan(ctx, appID, userID, normalized)
		if err != nil {
			s.log.Warn("batch create account ban failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			result.FailedUserIDs = append(result.FailedUserIDs, userID)
			continue
		}
		if item == nil {
			result.FailedUserIDs = append(result.FailedUserIDs, userID)
			continue
		}
		result.Created++
		s.revokeUserSessions(ctx, appID, userID)
		s.emitUserBannedHook(appID, userID, item)
	}
	result.Failed = len(result.FailedUserIDs)
	return result, nil
}

func (s *AccountBanService) RevokeBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error) {
	item, err := s.pg.RevokeUserAccountBan(ctx, appID, userID, banID, input)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "封禁记录不存在")
	}
	return item, nil
}

func (s *AccountBanService) UpdateActiveBanReason(ctx context.Context, appID int64, userID int64, reason string) (*userdomain.AccountBan, error) {
	return s.pg.UpdateActiveUserAccountBanReason(ctx, appID, userID, reason)
}

func (s *AccountBanService) GetActiveBan(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error) {
	return s.pg.RefreshUserAccountBanState(ctx, appID, userID)
}

func (s *AccountBanService) ListBans(ctx context.Context, appID int64, userID int64, query userdomain.AccountBanQuery) (*userdomain.AccountBanListResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	query.Page = page
	query.Limit = limit

	items, total, err := s.pg.ListUserAccountBans(ctx, appID, userID, query)
	if err != nil {
		return nil, err
	}
	return &userdomain.AccountBanListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPages(total, limit),
	}, nil
}

func (s *AccountBanService) runCleanupJob() {
	if s.pg == nil {
		return
	}
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		expired, err := s.pg.ExpireOverdueUserAccountBans(ctx, s.cleanupBatchSize)
		cancel()
		if err != nil {
			s.log.Warn("expire overdue account bans failed", zap.Error(err))
			return
		}
		if expired > 0 {
			s.log.Info("expired overdue account bans", zap.Int64("expired", expired))
		}
		if expired == 0 || expired < int64(s.cleanupBatchSize) {
			return
		}
	}
}

func (s *AccountBanService) revokeUserSessions(ctx context.Context, appID int64, userID int64) {
	if s.sessions == nil {
		return
	}
	sessions, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		s.log.Warn("list user sessions failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		return
	}
	for _, session := range sessions {
		if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, session.TokenHash); err != nil {
			s.log.Warn("delete user session failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.String("tokenHash", session.TokenHash), zap.Error(err))
		}
	}
}

func (s *AccountBanService) emitUserBannedHook(appID int64, userID int64, ban *userdomain.AccountBan) {
	if s.plugin == nil || ban == nil {
		return
	}
	go s.plugin.ExecuteHook(context.Background(), HookUserBanned, map[string]any{
		"userId":   userID,
		"banId":    ban.ID,
		"banType":  ban.BanType,
		"banScope": ban.BanScope,
		"status":   ban.Status,
		"reason":   ban.Reason,
	}, plugindomain.HookMetadata{UserID: &userID, AppID: &appID})
}

func normalizeAccountBanCreateInput(input userdomain.AccountBanCreateInput) (userdomain.AccountBanCreateInput, error) {
	output := input
	output.BanType = strings.TrimSpace(strings.ToLower(output.BanType))
	output.BanScope = strings.TrimSpace(strings.ToLower(output.BanScope))
	output.Reason = strings.TrimSpace(output.Reason)
	if output.BanType == "" {
		if output.EndAt != nil {
			output.BanType = userdomain.AccountBanTypeTemporary
		} else {
			output.BanType = userdomain.AccountBanTypePermanent
		}
	}
	if output.BanScope == "" {
		output.BanScope = userdomain.AccountBanScopeLogin
	}
	switch output.BanType {
	case userdomain.AccountBanTypeTemporary, userdomain.AccountBanTypePermanent:
	default:
		return output, apperrors.New(40000, http.StatusBadRequest, "不支持的封禁类型")
	}
	switch output.BanScope {
	case userdomain.AccountBanScopeLogin, userdomain.AccountBanScopeAll:
	default:
		return output, apperrors.New(40000, http.StatusBadRequest, "不支持的封禁范围")
	}

	now := time.Now().UTC()
	if output.StartAt == nil {
		output.StartAt = &now
	} else {
		value := output.StartAt.UTC()
		output.StartAt = &value
		if value.After(now.Add(5 * time.Second)) {
			return output, apperrors.New(40000, http.StatusBadRequest, "暂不支持未来生效的封禁时间")
		}
	}
	if output.BanType == userdomain.AccountBanTypeTemporary {
		if output.EndAt == nil {
			return output, apperrors.New(40000, http.StatusBadRequest, "临时封禁必须提供结束时间")
		}
		value := output.EndAt.UTC()
		output.EndAt = &value
		if !value.After(*output.StartAt) {
			return output, apperrors.New(40000, http.StatusBadRequest, "封禁结束时间必须晚于开始时间")
		}
	} else {
		output.EndAt = nil
	}
	if output.Evidence == nil {
		output.Evidence = map[string]any{}
	}
	output.Operator.AdminName = strings.TrimSpace(output.Operator.AdminName)
	return output, nil
}

func BanMessageFromRecord(ban *userdomain.AccountBan) string {
	if ban == nil {
		return "用户账户已被限制"
	}
	reason := strings.TrimSpace(ban.Reason)
	switch ban.BanType {
	case userdomain.AccountBanTypePermanent:
		if reason != "" {
			return fmt.Sprintf("用户账户已被封禁：%s", reason)
		}
		return "用户账户已被封禁"
	default:
		if reason != "" {
			return fmt.Sprintf("用户账户暂时被冻结：%s", reason)
		}
		return "用户账户暂时被冻结"
	}
}
