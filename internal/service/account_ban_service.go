package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"aegis/internal/config"
	plugindomain "aegis/internal/domain/plugin"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

// accountBanReasonMaxRunes 封禁原因的长度上限，与控制台输入框一致。
const accountBanReasonMaxRunes = 500

type AccountBanService struct {
	mu               sync.Mutex
	log              *zap.Logger
	pg               *pgrepo.Repository
	sessions         *redisrepo.SessionRepository
	plugin           *PluginService
	oauth2           *OAuth2ServerService
	listenTogether   *ListenTogetherService
	cron             *cron.Cron
	cleanupBatchSize int
	started          bool
	suspension       *SuspensionService
}

// SetSuspensionService 接全站封禁与封禁通知（SuspensionService.SetDependencies 会调）
func (s *AccountBanService) SetSuspensionService(suspension *SuspensionService) {
	s.mu.Lock()
	s.suspension = suspension
	s.mu.Unlock()
}

func (s *AccountBanService) suspensionService() *SuspensionService {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.suspension
}

func NewAccountBanService(cfg config.AccountBanConfig, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) (*AccountBanService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	service := &AccountBanService{
		log:      log,
		pg:       pg,
		sessions: sessions,
	}
	if err := service.Reload(cfg); err != nil {
		return nil, err
	}
	return service, nil
}

func (s *AccountBanService) SetPluginService(plugin *PluginService) {
	s.plugin = plugin
}

func (s *AccountBanService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = true
	if s.cron != nil {
		s.cron.Start()
	}
}

func (s *AccountBanService) Close(ctx context.Context) error {
	s.mu.Lock()
	cronScheduler := s.cron
	s.started = false
	s.mu.Unlock()
	if cronScheduler == nil {
		return nil
	}
	stopCtx := cronScheduler.Stop()
	select {
	case <-stopCtx.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *AccountBanService) Reload(cfg config.AccountBanConfig) error {
	if cfg.CleanupBatchSize <= 0 {
		cfg.CleanupBatchSize = 200
	}
	var scheduler *cron.Cron
	if cfg.CleanupEnabled {
		scheduler = cron.New(cron.WithLocation(timeutil.DefaultLocation()))
		if _, err := scheduler.AddFunc(cfg.CleanupSpec, s.runCleanupJob); err != nil {
			return err
		}
	}

	s.mu.Lock()
	oldScheduler := s.cron
	started := s.started
	s.cleanupBatchSize = cfg.CleanupBatchSize
	s.cron = scheduler
	s.mu.Unlock()

	if oldScheduler != nil {
		<-oldScheduler.Stop().Done()
	}
	if started && scheduler != nil {
		scheduler.Start()
	}
	return nil
}

// BanUser 封禁一个账号。level=platform 时转给全站封禁（封这个人在所有应用的账号）；默认发封禁通知。
func (s *AccountBanService) BanUser(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	normalized, err := normalizeAccountBanCreateInput(input)
	if err != nil {
		return nil, err
	}
	sus := s.suspensionService()
	if normalized.Level == userdomain.AccountBanLevelPlatform && normalized.PlatformBanID == nil {
		if sus == nil {
			return nil, apperrors.New(50330, http.StatusServiceUnavailable, "全站封禁未接入")
		}
		return sus.BanPlatform(ctx, appID, userID, normalized)
	}
	item, err := s.createOne(ctx, appID, userID, normalized)
	if err != nil {
		return nil, err
	}
	if sus != nil && (normalized.Notify == nil || *normalized.Notify) {
		go sus.notifySuspended(context.WithoutCancel(ctx), item, map[string]bool{})
	}
	return item, nil
}

// createOne 落一条账号封禁并执行封禁后的动作（撤会话、钩子），不发通知、不转全站
func (s *AccountBanService) createOne(ctx context.Context, appID int64, userID int64, normalized userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	item, err := s.pg.CreateUserAccountBan(ctx, appID, userID, normalized)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	s.afterBanCreated(ctx, appID, userID, item)
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
		item, err := s.BanUser(ctx, appID, userID, normalized)
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
	}
	result.Failed = len(result.FailedUserIDs)
	return result, nil
}

// RevokeBan 解除一条账号封禁。全站封禁连带的（level=platform）会解除整条全站封禁与它连带的所有账号，
// 否则下次登录又会按全站封禁被连带封上。默认发解封通知。
func (s *AccountBanService) RevokeBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error) {
	sus := s.suspensionService()
	notify := input.Notify == nil || *input.Notify
	if sus != nil {
		// 全站封禁连带的：整条全站封禁一起解除（包括这一条），通知统一发、同一邮箱只发一封
		if current, err := s.pg.GetUserAccountBan(ctx, appID, banID); err == nil && current != nil && current.UserID == userID &&
			current.Status == userdomain.AccountBanStatusActive && current.Level == userdomain.AccountBanLevelPlatform && current.PlatformBanID != nil {
			if _, err := sus.RevokePlatform(ctx, *current.PlatformBanID, input.Operator, input.Reason, notify); err == nil {
				if item, err := s.pg.GetUserAccountBan(ctx, appID, banID); err == nil && item != nil && item.Status != userdomain.AccountBanStatusActive {
					return item, nil
				}
			} else {
				var appErr *apperrors.AppError
				if !errors.As(err, &appErr) || appErr.Code != 40934 {
					s.log.Warn("解除全站封禁失败", zap.Int64("platformBanId", *current.PlatformBanID), zap.Error(err))
				}
			}
		}
	}
	item, err := s.revokeOne(ctx, appID, userID, banID, input)
	if err != nil {
		return nil, err
	}
	if sus != nil && notify {
		go sus.notifyReinstated(context.WithoutCancel(ctx), item, map[string]bool{})
	}
	return item, nil
}

// revokeOne 只解除这一条，不连带、不通知
func (s *AccountBanService) revokeOne(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error) {
	item, err := s.pg.RevokeUserAccountBan(ctx, appID, userID, banID, input)
	if errors.Is(err, pgrepo.ErrAccountBanNotActive) {
		return nil, apperrors.New(40934, http.StatusConflict, "该封禁已到期或已撤销，无需解除")
	}
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
	switch query.Status = strings.TrimSpace(query.Status); query.Status {
	case "", userdomain.AccountBanStatusActive, userdomain.AccountBanStatusExpired, userdomain.AccountBanStatusRevoked:
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的封禁状态")
	}

	// 到期的临时封禁要等清理任务才改状态；先把这个用户的结掉，列表上就不会出现「已到期却仍生效」
	if _, err := s.pg.RefreshUserAccountBanState(ctx, appID, userID); err != nil {
		return nil, err
	}
	items, total, err := s.pg.ListUserAccountBans(ctx, appID, userID, query)
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.CountUserAccountBans(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return &userdomain.AccountBanListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPages(total, limit),
		Counts:     counts,
	}, nil
}

func (s *AccountBanService) runCleanupJob() {
	if s.pg == nil {
		return
	}
	for {
		s.mu.Lock()
		batchSize := s.cleanupBatchSize
		s.mu.Unlock()
		if batchSize <= 0 {
			batchSize = 200
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		expired, err := s.pg.ExpireOverdueUserAccountBans(ctx, batchSize)
		cancel()
		if err != nil {
			s.log.Warn("expire overdue account bans failed", zap.Error(err))
			return
		}
		if expired > 0 {
			s.log.Info("expired overdue account bans", zap.Int64("expired", expired))
		}
		if expired == 0 || expired < int64(batchSize) {
			return
		}
	}
}

// SetOAuth2Server 注入授权服务器：封禁时一并撤销该账号在第三方应用上的授权与令牌。
func (s *AccountBanService) SetOAuth2Server(oauth2 *OAuth2ServerService) {
	s.oauth2 = oauth2
}

// SetListenTogether 注入一起听：封禁时让一起听服务立刻断开这个人（不等票据过期）。
func (s *AccountBanService) SetListenTogether(listen *ListenTogetherService) {
	s.listenTogether = listen
}

// afterBanCreated 封禁落库后的副作用。仅登录的封禁不动已签发的会话 —— 这正是它和全部访问的区别。
func (s *AccountBanService) afterBanCreated(ctx context.Context, appID int64, userID int64, ban *userdomain.AccountBan) {
	if ban.Blocks(userdomain.AccountBanCheckpointSession) {
		s.revokeUserSessions(ctx, appID, userID)
		s.listenTogether.RevokeUser(appID, userID, "banned")
	}
	s.emitUserBannedHook(appID, userID, ban)
}

func (s *AccountBanService) revokeUserSessions(ctx context.Context, appID int64, userID int64) {
	if s.oauth2 != nil {
		s.oauth2.RevokeUserEverywhere(appID, userID)
	}
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
	if output.Reason == "" {
		// 原因会原样出现在用户被拒时看到的提示与申诉记录里
		return output, apperrors.New(40000, http.StatusBadRequest, "封禁原因不能为空")
	}
	if utf8.RuneCountInString(output.Reason) > accountBanReasonMaxRunes {
		return output, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("封禁原因不能超过 %d 个字", accountBanReasonMaxRunes))
	}
	if output.BanType == "" {
		if output.EndAt != nil {
			output.BanType = userdomain.AccountBanTypeTemporary
		} else {
			output.BanType = userdomain.AccountBanTypePermanent
		}
	}
	if output.BanScope == "" {
		output.BanScope = userdomain.AccountBanScopeAll
	}
	output.Level = strings.TrimSpace(strings.ToLower(output.Level))
	if output.Level == "" {
		output.Level = userdomain.AccountBanLevelApp
	}
	if output.Level != userdomain.AccountBanLevelApp && output.Level != userdomain.AccountBanLevelPlatform {
		return output, apperrors.New(40000, http.StatusBadRequest, "封禁级别只能是 app 或 platform")
	}
	output.ReasonCode = strings.TrimSpace(strings.ToLower(output.ReasonCode))
	if output.ReasonCode == "" {
		output.ReasonCode = "policy"
	}
	if !userdomain.ValidBanReasonCode(output.ReasonCode) {
		return output, apperrors.New(40000, http.StatusBadRequest, "不支持的原因类别")
	}
	output.PublicReason = strings.TrimSpace(output.PublicReason)
	if utf8.RuneCountInString(output.PublicReason) > accountBanReasonMaxRunes {
		return output, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("对用户显示的说明不能超过 %d 个字", accountBanReasonMaxRunes))
	}
	output.Source = strings.TrimSpace(strings.ToLower(output.Source))
	switch output.Source {
	case "":
		output.Source = userdomain.AccountBanSourceManual
	case userdomain.AccountBanSourceManual, userdomain.AccountBanSourceRisk, userdomain.AccountBanSourcePlatform:
	default:
		return output, apperrors.New(40000, http.StatusBadRequest, "不支持的封禁来源")
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

	now := timeutil.NowUTC()
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

// BanMessageFromRecord 是用户被封禁拦下时看到的提示：永久写「封禁」，临时写「冻结」并带上解除时间。
func BanMessageFromRecord(ban *userdomain.AccountBan) string {
	if ban == nil {
		return "用户账户已被限制"
	}
	message := "用户账户已被封禁"
	if ban.BanType != userdomain.AccountBanTypePermanent {
		message = "用户账户暂时被冻结"
		if ban.EndAt != nil {
			message += "，解除时间 " + ban.EndAt.In(timeutil.DefaultLocation()).Format("2006-01-02 15:04")
		}
	}
	if reason := strings.TrimSpace(ban.Reason); reason != "" {
		message += "。原因：" + reason
	}
	return message
}

// accountBanError 把生效中的封禁换成对外的错误：40301 永久封禁，40302 临时冻结。
func accountBanError(ban *userdomain.AccountBan) error {
	if ban.BanType == userdomain.AccountBanTypePermanent {
		return apperrors.New(40301, http.StatusForbidden, BanMessageFromRecord(ban))
	}
	return apperrors.New(40302, http.StatusForbidden, BanMessageFromRecord(ban))
}

// enforceAccountBan 是账号封禁唯一的判定入口，登录、刷新、业务接口都从这里过。
//
// 以封禁记录为准：查到记录就只看它在这个执行点拦不拦，users.enabled / disabled_end_time
// 这两个投影字段不再参与 —— 仅登录的封禁同样会把投影置为受限（列表要能筛出来），
// 若再看投影，已登录的设备就会被一并挡掉，「仅登录」名不副实。
// 没有记录时才回落到投影，兜住迁移来的老数据与查询失败。
func enforceAccountBan(ctx context.Context, pg *pgrepo.Repository, log *zap.Logger, user *userdomain.User, checkpoint userdomain.AccountBanCheckpoint) error {
	ban, err := pg.RefreshUserAccountBanState(ctx, user.AppID, user.ID)
	if err != nil {
		log.Warn("refresh user account ban state failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
	} else if ban != nil {
		if ban.Blocks(checkpoint) {
			return banErrorFor(ctx, ban)
		}
		return nil
	}
	// 登录时核对全站封禁：这个账号的邮箱、手机号、第三方身份、设备撞上了，就当场连带封上
	if err == nil && checkpoint == userdomain.AccountBanCheckpointSignIn {
		if sus := suspensionHooks.Load(); sus != nil {
			if pbBan, perr := sus.checkPlatformAtSignIn(ctx, user); perr != nil {
				log.Warn("check platform ban failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(perr))
			} else if pbBan != nil {
				return banErrorFor(ctx, pbBan)
			}
		}
	}
	if !user.Enabled {
		return apperrors.New(40301, http.StatusForbidden, "用户账户已被禁用")
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(time.Now()) {
		return apperrors.New(40302, http.StatusForbidden, "用户账户暂时被冻结")
	}
	return nil
}
