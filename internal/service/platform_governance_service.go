package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	admindomain "aegis/internal/domain/admin"
	notificationdomain "aegis/internal/domain/notification"
	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"go.uber.org/zap"
)

// PlatformGovernanceService 平台治理：超级管理员 / 平台管理员对全站应用的强制管控。
//
// ── 为什么不复用 apps.status ──
//
// `apps.status` 是应用自治的营业开关，应用管理员自己就能改。如果平台的封禁结论也写在那里，
// 被封的应用把开关拨回去就复活了。因此治理状态单独存表、单独鉴权，`apps.status` 保持原义。
// 两者是**与**的关系：任一为关都拒绝服务。
//
// ── 限制项的执行点索引 ──
//
// 每一项限制都必须有真实执行点，只落库不生效的开关比没有这个开关更危险。
//
//	blockLogin        AppService.EnsureLoginAllowed（覆盖密码/短信/OAuth/Passkey 全部登录入口）
//	blockRegister     AppService.EnsureRegisterAllowed（含 OAuth / 短信自动建号）
//	blockApi          AuthService.ValidateAccessToken + Refresh（现存会话立刻失效，刷新也换不出新令牌）
//	blockPayment      PaymentService.CreateOrder（**只挡新订单，不挡退款**：
//	                  冻结一个应用不该把用户已经付进去的钱一起锁死。停运 / 封禁档
//	                  会通过 blockAdminWrite 把退款收归平台管理员操作）
//	blockStorage      StorageService 上传与写入类操作
//	blockNotification NotificationService 站内信 + EmailService 出信
//	blockAdminWrite   middleware.AdminAccess（应用作用域的一切非 GET 请求）
//
// ── 判定为什么走内存 ──
//
// 登录与每次带令牌的请求都要判一次，打库不现实。因此状态表在内存里留一份快照：
// 本实例的写操作立即刷新，跨实例靠后台 tick 收敛（默认 15s）。
// 需要强一致的场景（管理端读详情）走 `Get`，直接读库。
type PlatformGovernanceService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	inbox    *AdminInboxService
	plugin   *PluginService

	mu     sync.RWMutex
	states map[int64]platformdomain.Governance

	version atomic.Int64
	loaded  atomic.Bool

	stopOnce sync.Once
	stopCh   chan struct{}
}

// 治理相关错误码（403 段 40330-40349 / 400 段 40031-40044 未被占用）
const (
	errCodeGovLoginBlocked        = 40330
	errCodeGovRegisterBlocked     = 40331
	errCodeGovAPIBlocked          = 40332
	errCodeGovPaymentBlocked      = 40333
	errCodeGovStorageBlocked      = 40334
	errCodeGovNotificationBlocked = 40335
	errCodeGovAdminWriteBlocked   = 40336
	errCodeGovNeedGovernPerm      = 40337
	errCodeGovNeedDangerPerm      = 40338
	errCodeGovIllegalTransition   = 40339

	errCodeGovInvalidAction       = 40031
	errCodeGovReasonRequired      = 40032
	errCodeGovInvalidDeadline     = 40033
	errCodeGovNoRestriction       = 40034
	errCodeGovInvalidState        = 40035
	errCodeGovAppealContent       = 40036
	errCodeGovInvalidDecision     = 40037
	errCodeGovAppealNotApplicable = 40038

	errCodeGovAppNotFound    = 40493
	errCodeGovAppealNotFound = 40494
	errCodeGovAppealPending  = 40904
)

// governanceRefreshInterval 跨实例收敛间隔。
// 本实例的写立即生效，别的实例最迟 15s 后看到；治理是分钟级人工操作，这个窗口可接受。
const governanceRefreshInterval = 15 * time.Second

// governanceMaxDuration 单次治理的最长期限，防止"临时冻结"写成一百年后到期。
const governanceMaxDuration = 5 * 365 * 24 * time.Hour

func NewPlatformGovernanceService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *PlatformGovernanceService {
	if log == nil {
		log = zap.NewNop()
	}
	return &PlatformGovernanceService{
		log:      log,
		pg:       pg,
		sessions: sessions,
		states:   map[int64]platformdomain.Governance{},
		stopCh:   make(chan struct{}),
	}
}

// SetAdminInbox 注入管理员收件箱（治理动作通知应用管理员）。
func (s *PlatformGovernanceService) SetAdminInbox(inbox *AdminInboxService) { s.inbox = inbox }

// SetPluginService 注入插件系统（治理动作触发钩子）。
func (s *PlatformGovernanceService) SetPluginService(plugin *PluginService) { s.plugin = plugin }

// Initialize 启动时加载治理快照。
func (s *PlatformGovernanceService) Initialize(ctx context.Context) error {
	return s.ReloadFromDB(ctx)
}

// ReloadFromDB 重新加载全部非 active 的治理状态。
func (s *PlatformGovernanceService) ReloadFromDB(ctx context.Context) error {
	if s == nil || s.pg == nil {
		return nil
	}
	items, err := s.pg.ListAppGovernance(ctx)
	if err != nil {
		return err
	}
	next := make(map[int64]platformdomain.Governance, len(items))
	for _, item := range items {
		next[item.AppID] = item
	}
	s.mu.Lock()
	s.states = next
	s.mu.Unlock()
	s.version.Add(1)
	s.loaded.Store(true)
	return nil
}

// Start 拉起后台循环：定期收敛快照 + 结算到期治理。
func (s *PlatformGovernanceService) Start(ctx context.Context) {
	s.start(ctx, true)
}

// StartReadOnly 只收敛快照、不结算到期。
//
// 供 Worker 使用：判定必须与 API 一致，但到期恢复只该有一个执行者 ——
// 让两个进程各自去恢复同一批应用，流水里会出现两条"到期自动恢复"。
func (s *PlatformGovernanceService) StartReadOnly(ctx context.Context) {
	s.start(ctx, false)
}

func (s *PlatformGovernanceService) start(ctx context.Context, withExpiry bool) {
	if s == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(governanceRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				runCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				if withExpiry {
					if _, err := s.RunExpiry(runCtx); err != nil {
						s.log.Warn("平台治理到期结算失败", zap.Error(err))
					}
				}
				if err := s.ReloadFromDB(runCtx); err != nil {
					s.log.Warn("平台治理快照刷新失败", zap.Error(err))
				}
				cancel()
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *PlatformGovernanceService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Snapshot 读内存快照；无记录表示无治理。
func (s *PlatformGovernanceService) Snapshot(appID int64) *platformdomain.Governance {
	if s == nil || appID <= 0 {
		return nil
	}
	s.mu.RLock()
	item, ok := s.states[appID]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	// 快照里可能残留刚过期但后台还没结算的记录，此处按时间直接放行，
	// 否则用户要多等最多一个 tick 才能恢复访问。
	if item.EndAt != nil && !item.EndAt.After(timeutil.NowUTC()) {
		return nil
	}
	return &item
}

// Decide 判定某项能力当前是否被平台限制。
func (s *PlatformGovernanceService) Decide(appID int64, capability string) platformdomain.Decision {
	state := s.Snapshot(appID)
	if state == nil || state.IsActive() {
		return platformdomain.Decision{Allowed: true, State: platformdomain.StateActive}
	}
	if !state.Restrictions.Blocks(capability) {
		return platformdomain.Decision{Allowed: true, State: state.State}
	}
	return platformdomain.Decision{
		Allowed:    false,
		State:      state.State,
		Capability: capability,
		Reason:     state.Reason,
		Message:    governanceBlockMessage(state, capability),
		EndAt:      state.EndAt,
	}
}

// EnsureCapability 判定并在被限制时返回可直接下发的业务错误。
func (s *PlatformGovernanceService) EnsureCapability(appID int64, capability string) error {
	decision := s.Decide(appID, capability)
	if decision.Allowed {
		return nil
	}
	return apperrors.New(governanceErrorCode(capability), http.StatusForbidden, decision.Message)
}

// Get 从数据库读取治理详情（管理端强一致读）。无记录时返回 active 占位。
func (s *PlatformGovernanceService) Get(ctx context.Context, appID int64) (*platformdomain.Governance, error) {
	item, err := s.pg.GetAppGovernance(ctx, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return &platformdomain.Governance{
			AppID:        appID,
			State:        platformdomain.StateActive,
			AppealStatus: platformdomain.AppealStatusNone,
		}, nil
	}
	return item, nil
}

// Apply 执行一次治理动作。
//
// 状态与流水在同一事务落库，落库成功后才做副作用（踢会话、发通知、缓存刷新）——
// 副作用失败不回滚：治理结论已经生效，回滚反而会造成"库里没封、判定已封"的错位。
func (s *PlatformGovernanceService) Apply(
	ctx context.Context,
	appID int64,
	input platformdomain.ActionInput,
	actor platformdomain.Actor,
) (*platformdomain.Governance, *platformdomain.ActionRecord, error) {
	action := strings.TrimSpace(strings.ToLower(input.Action))
	targetState := platformdomain.ActionTargetState(action)
	if action == platformdomain.ActionUpdate {
		targetState = platformdomain.ActionUpdate // 占位，下面按当前状态求解
	}
	if targetState == "" {
		return nil, nil, apperrors.New(errCodeGovInvalidAction, http.StatusBadRequest, "不支持的治理动作")
	}

	current, err := s.Get(ctx, appID)
	if err != nil {
		return nil, nil, err
	}
	fromState := current.State
	if fromState == "" {
		fromState = platformdomain.StateActive
	}

	if action == platformdomain.ActionUpdate {
		if current.IsActive() {
			return nil, nil, apperrors.New(errCodeGovIllegalTransition, http.StatusForbidden, "该应用当前没有生效中的治理，无法调整")
		}
		targetState = fromState
	}

	// 危险档位的进入与解除都要危险操作权限：
	// 封禁 / 归档是不可逆感极强的动作，普通平台管理员只能冻结与限制。
	if platformdomain.StateRequiresDanger(targetState) && !actor.CanDanger {
		return nil, nil, apperrors.New(errCodeGovNeedDangerPerm, http.StatusForbidden, "封禁与归档属于危险操作，需要超级管理员或平台危险操作权限")
	}
	if platformdomain.StateRequiresDanger(fromState) && !actor.CanDanger {
		return nil, nil, apperrors.New(errCodeGovNeedDangerPerm, http.StatusForbidden, "解除封禁 / 归档需要超级管理员或平台危险操作权限")
	}

	reason := strings.TrimSpace(input.Reason)
	if targetState != platformdomain.StateActive && len([]rune(reason)) < 4 {
		return nil, nil, apperrors.New(errCodeGovReasonRequired, http.StatusBadRequest, "治理理由不能少于 4 个字，它会写进流水并展示给被治理方")
	}

	restrictions := platformdomain.PresetRestrictions(targetState)
	if targetState == platformdomain.StateRestricted || (action == platformdomain.ActionUpdate && fromState == platformdomain.StateRestricted) {
		if input.Restrictions == nil {
			restrictions = current.Restrictions
		} else {
			restrictions = *input.Restrictions
		}
		if !restrictions.Any() {
			return nil, nil, apperrors.New(errCodeGovNoRestriction, http.StatusBadRequest, "部分受限至少要勾选一项限制")
		}
	}
	if targetState == platformdomain.StateActive {
		restrictions = platformdomain.Restrictions{}
	}

	endAt, err := resolveGovernanceDeadline(targetState, input)
	if err != nil {
		return nil, nil, err
	}

	now := timeutil.NowUTC()
	startAt := current.StartAt
	if targetState == platformdomain.StateActive {
		startAt, endAt = nil, nil
	} else if startAt == nil || fromState == platformdomain.StateActive {
		startAt = &now
	}

	appealStatus := current.AppealStatus
	if targetState == platformdomain.StateActive {
		appealStatus = platformdomain.AppealStatusNone
	}
	if appealStatus == "" {
		appealStatus = platformdomain.AppealStatusNone
	}

	var operatorID *int64
	if actor.AdminID > 0 {
		id := actor.AdminID
		operatorID = &id
	}

	next := platformdomain.Governance{
		AppID:           appID,
		State:           targetState,
		Reason:          reason,
		Restrictions:    restrictions,
		Evidence:        input.Evidence,
		StartAt:         startAt,
		EndAt:           endAt,
		OperatorAdminID: operatorID,
		OperatorName:    actor.AdminName,
		LastAction:      action,
		AppealStatus:    appealStatus,
	}
	record := platformdomain.ActionRecord{
		AppID:           appID,
		Action:          action,
		FromState:       fromState,
		ToState:         targetState,
		Reason:          reason,
		Restrictions:    restrictions,
		Evidence:        input.Evidence,
		EndAt:           endAt,
		OperatorAdminID: operatorID,
		OperatorName:    actor.AdminName,
		OperatorIP:      actor.IP,
	}

	saved, savedAction, err := s.pg.ApplyAppGovernance(ctx, next, record)
	if err != nil {
		return nil, nil, err
	}
	if saved == nil {
		return nil, nil, apperrors.New(errCodeGovAppNotFound, http.StatusNotFound, "应用不存在")
	}

	s.applyLocal(*saved)

	// ── 副作用 ──
	revoked := 0
	if input.RevokeSessions || restrictions.BlockAPI {
		if result, err := s.purgeAppSessions(ctx, appID); err != nil {
			s.log.Warn("治理动作清扫应用会话失败", zap.Int64("appid", appID), zap.Error(err))
		} else {
			revoked = result.Sessions
		}
	}
	if revoked > 0 {
		savedAction.RevokedSessions = revoked
		if err := s.pg.UpdateGovernanceActionRevokedSessions(ctx, savedAction.ID, revoked); err != nil {
			s.log.Warn("回写治理流水会话数失败", zap.Int64("actionId", savedAction.ID), zap.Error(err))
		}
	}
	if input.NotifyAdmins {
		s.notifyAppAdmins(ctx, *saved, *savedAction)
	}
	s.emitHook(appID, *saved, *savedAction)

	s.log.Warn("平台治理动作已执行",
		zap.Int64("appid", appID),
		zap.String("action", action),
		zap.String("fromState", fromState),
		zap.String("toState", targetState),
		zap.String("operator", actor.AdminName),
		zap.Int("revokedSessions", revoked),
	)
	return saved, savedAction, nil
}

// BatchApply 批量治理。逐个执行，单个失败不影响其余（治理往往针对一批同类应用，
// 中途中断会留下"改了一半"的现场，比逐条报错更难收拾）。
func (s *PlatformGovernanceService) BatchApply(
	ctx context.Context,
	input platformdomain.BatchActionInput,
	actor platformdomain.Actor,
) (*platformdomain.BatchActionResult, error) {
	if len(input.AppIDs) == 0 {
		return nil, apperrors.New(errCodeGovInvalidAction, http.StatusBadRequest, "请至少选择一个应用")
	}
	result := &platformdomain.BatchActionResult{
		Requested: len(input.AppIDs),
		Items:     make([]platformdomain.BatchActionOutcome, 0, len(input.AppIDs)),
	}
	seen := make(map[int64]struct{}, len(input.AppIDs))
	for _, appID := range input.AppIDs {
		if appID <= 0 {
			continue
		}
		if _, ok := seen[appID]; ok {
			continue
		}
		seen[appID] = struct{}{}

		saved, _, err := s.Apply(ctx, appID, input.ActionInput, actor)
		outcome := platformdomain.BatchActionOutcome{AppID: appID}
		if err != nil {
			outcome.OK = false
			outcome.Error = governanceErrorMessage(err)
			result.Failed++
		} else {
			outcome.OK = true
			outcome.State = saved.State
			outcome.AppName = saved.AppName
			result.Succeeded++
		}
		result.Items = append(result.Items, outcome)
	}
	return result, nil
}

// RevokeAppSessions 强制下线该应用全部在线用户（不改变治理状态）。危险操作。
func (s *PlatformGovernanceService) RevokeAppSessions(ctx context.Context, appID int64, reason string, actor platformdomain.Actor) (*platformdomain.ActionRecord, error) {
	if !actor.CanDanger {
		return nil, apperrors.New(errCodeGovNeedDangerPerm, http.StatusForbidden, "强制下线属于危险操作，需要超级管理员或平台危险操作权限")
	}
	current, err := s.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	result, err := s.purgeAppSessions(ctx, appID)
	if err != nil {
		return nil, err
	}
	var operatorID *int64
	if actor.AdminID > 0 {
		id := actor.AdminID
		operatorID = &id
	}
	record, err := s.pg.RecordAppGovernanceAction(ctx, platformdomain.ActionRecord{
		AppID:           appID,
		Action:          platformdomain.ActionRevokeSessions,
		FromState:       current.State,
		ToState:         current.State,
		Reason:          strings.TrimSpace(reason),
		OperatorAdminID: operatorID,
		OperatorName:    actor.AdminName,
		OperatorIP:      actor.IP,
		RevokedSessions: result.Sessions,
	})
	if err != nil {
		return nil, err
	}
	s.log.Warn("平台强制下线应用会话",
		zap.Int64("appid", appID),
		zap.Int("users", result.Users),
		zap.Int("sessions", result.Sessions),
		zap.String("operator", actor.AdminName),
	)
	return record, nil
}

// RunExpiry 结算到期治理，返回恢复的应用数。
func (s *PlatformGovernanceService) RunExpiry(ctx context.Context) (int, error) {
	if s == nil || s.pg == nil {
		return 0, nil
	}
	restored, err := s.pg.ExpireDueAppGovernance(ctx, 200)
	if err != nil {
		return 0, err
	}
	for _, item := range restored {
		s.applyLocal(item)
		s.log.Info("平台治理到期自动恢复", zap.Int64("appid", item.AppID))
	}
	return len(restored), nil
}

// ── 总览与流水 ──

// Overview 全站应用总览。allowedAppIDs 为空表示不限范围（超管 / 全局角色）。
func (s *PlatformGovernanceService) Overview(ctx context.Context, query platformdomain.OverviewQuery, allowedAppIDs []int64) (*platformdomain.OverviewResult, error) {
	query.Page, query.Limit = normalizePaging(query.Page, query.Limit, 20, 100)
	items, total, err := s.pg.ListAppGovernanceOverview(ctx, query, allowedAppIDs)
	if err != nil {
		return nil, err
	}
	summary, err := s.pg.GetAppGovernanceSummary(ctx, allowedAppIDs)
	if err != nil {
		return nil, err
	}
	return &platformdomain.OverviewResult{
		Items:      items,
		Summary:    *summary,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: calcPages(total, query.Limit),
	}, nil
}

// ListActions 治理流水分页。
func (s *PlatformGovernanceService) ListActions(ctx context.Context, query platformdomain.ActionQuery) (*platformdomain.ActionListResult, error) {
	query.Page, query.Limit = normalizePaging(query.Page, query.Limit, 20, 100)
	items, total, err := s.pg.ListAppGovernanceActions(ctx, query)
	if err != nil {
		return nil, err
	}
	return &platformdomain.ActionListResult{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: calcPages(total, query.Limit),
	}, nil
}

// ── 申诉 ──

// SubmitAppeal 被治理应用的管理员提交申诉。
func (s *PlatformGovernanceService) SubmitAppeal(ctx context.Context, appID int64, input platformdomain.AppealCreateInput, adminID int64, adminName string) (*platformdomain.Appeal, error) {
	content := strings.TrimSpace(input.Content)
	if len([]rune(content)) < 10 {
		return nil, apperrors.New(errCodeGovAppealContent, http.StatusBadRequest, "申诉说明不能少于 10 个字")
	}
	current, err := s.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if current.IsActive() {
		return nil, apperrors.New(errCodeGovAppealNotApplicable, http.StatusBadRequest, "该应用当前未被治理，无需申诉")
	}
	existing, err := s.pg.GetLatestPendingAppeal(ctx, appID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperrors.New(errCodeGovAppealPending, http.StatusConflict, "该应用已有待审申诉，请等待处理结果")
	}
	input.Content = content
	appeal, err := s.pg.CreateGovernanceAppeal(ctx, appID, current.State, nil, input, adminID, adminName)
	if err != nil {
		return nil, err
	}
	s.applyLocalAppealStatus(appID, platformdomain.AppealStatusPending)
	return appeal, nil
}

// ListAppeals 申诉列表。allowedAppIDs 为空表示不限范围。
func (s *PlatformGovernanceService) ListAppeals(ctx context.Context, query platformdomain.AppealQuery, allowedAppIDs []int64) (*platformdomain.AppealListResult, error) {
	query.Page, query.Limit = normalizePaging(query.Page, query.Limit, 20, 100)
	items, total, err := s.pg.ListGovernanceAppeals(ctx, query, allowedAppIDs)
	if err != nil {
		return nil, err
	}
	return &platformdomain.AppealListResult{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: calcPages(total, query.Limit),
	}, nil
}

// ReviewAppeal 裁决申诉；通过且 Restore 为真时一并解除治理。
func (s *PlatformGovernanceService) ReviewAppeal(ctx context.Context, appealID int64, input platformdomain.AppealReviewInput, actor platformdomain.Actor) (*platformdomain.Appeal, error) {
	decision := strings.TrimSpace(strings.ToLower(input.Decision))
	if decision != platformdomain.AppealStatusApproved && decision != platformdomain.AppealStatusRejected {
		return nil, apperrors.New(errCodeGovInvalidDecision, http.StatusBadRequest, "裁决结果只能是 approved 或 rejected")
	}
	appeal, err := s.pg.GetGovernanceAppeal(ctx, appealID)
	if err != nil {
		return nil, err
	}
	if appeal == nil {
		return nil, apperrors.New(errCodeGovAppealNotFound, http.StatusNotFound, "申诉不存在")
	}
	if appeal.Status != platformdomain.AppealStatusPending {
		return nil, apperrors.New(errCodeGovInvalidDecision, http.StatusBadRequest, "该申诉已被处理")
	}

	restore := decision == platformdomain.AppealStatusApproved
	if input.Restore != nil {
		restore = restore && *input.Restore
	}
	// 先解除再裁决：反过来一旦解除失败，申诉已标记通过但应用还封着，
	// 被治理方看到的是"通过了却还是用不了"。
	if restore {
		if _, _, err := s.Apply(ctx, appeal.AppID, platformdomain.ActionInput{
			Action: platformdomain.ActionAppealApproved,
			Reason: strings.TrimSpace(input.Note),
		}, actor); err != nil {
			return nil, err
		}
	}

	reviewed, err := s.pg.ReviewGovernanceAppeal(ctx, appealID, decision, input.Note, actor.AdminID, actor.AdminName)
	if err != nil {
		return nil, err
	}
	if reviewed == nil {
		return nil, apperrors.New(errCodeGovAppealNotFound, http.StatusNotFound, "申诉不存在或已被处理")
	}
	s.applyLocalAppealStatus(reviewed.AppID, decision)
	s.notifyAppealResult(ctx, *reviewed, restore)
	return reviewed, nil
}

// WithdrawAppeal 撤回自己提交的待审申诉。
func (s *PlatformGovernanceService) WithdrawAppeal(ctx context.Context, appealID int64, adminID int64) (*platformdomain.Appeal, error) {
	appeal, err := s.pg.WithdrawGovernanceAppeal(ctx, appealID, adminID)
	if err != nil {
		return nil, err
	}
	if appeal == nil {
		return nil, apperrors.New(errCodeGovAppealNotFound, http.StatusNotFound, "待审申诉不存在或不属于当前管理员")
	}
	s.applyLocalAppealStatus(appeal.AppID, platformdomain.AppealStatusNone)
	return appeal, nil
}

// ── 目录 ──

// Catalog 返回状态 / 能力 / 常用时长目录，驱动控制台的治理面板。
//
// 目录里带上每项能力的执行点：这个开关到底管不管用，接手的人不必翻代码就能核对。
func (s *PlatformGovernanceService) Catalog() platformdomain.Catalog {
	states := []platformdomain.StateMeta{
		{
			Key: platformdomain.StateActive, Name: "正常", Action: platformdomain.ActionRestore, Severity: 0,
			Description: "解除全部平台限制，应用恢复自治",
		},
		{
			Key: platformdomain.StateRestricted, Name: "部分受限", Action: platformdomain.ActionRestrict, Severity: 1,
			Description:  "按需勾选要停用的能力，其余照常运行",
			Restrictions: platformdomain.PresetRestrictions(platformdomain.StateRestricted),
			Customizable: true,
		},
		{
			Key: platformdomain.StateFrozen, Name: "冻结", Action: platformdomain.ActionFreeze, Severity: 2,
			Description:  "用户侧全部停用（登录/注册/接口/支付/存储），应用管理员仍可进控制台排查",
			Restrictions: platformdomain.PresetRestrictions(platformdomain.StateFrozen),
		},
		{
			Key: platformdomain.StateSuspended, Name: "停运", Action: platformdomain.ActionSuspend, Severity: 3,
			Description:  "在冻结之上再把应用管理员的写操作一并只读化",
			Restrictions: platformdomain.PresetRestrictions(platformdomain.StateSuspended),
		},
		{
			Key: platformdomain.StateBanned, Name: "封禁", Action: platformdomain.ActionBan, Severity: 4,
			Description:    "永久停运，仅超级管理员或持危险操作权限者可解除",
			Permanent:      true,
			RequiresDanger: true,
			Restrictions:   platformdomain.PresetRestrictions(platformdomain.StateBanned),
		},
		{
			Key: platformdomain.StateArchived, Name: "归档", Action: platformdomain.ActionArchive, Severity: 4,
			Description:    "永久只读留档，用于已下线但要保留数据的应用",
			Permanent:      true,
			RequiresDanger: true,
			Restrictions:   platformdomain.PresetRestrictions(platformdomain.StateArchived),
		},
	}
	capabilities := []platformdomain.CapabilityMeta{
		{Key: platformdomain.CapabilityLogin, Field: "blockLogin", Name: "登录", Description: "密码 / 短信 / 第三方 / Passkey 全部登录入口", Enforcement: "AppService.EnsureLoginAllowed"},
		{Key: platformdomain.CapabilityRegister, Field: "blockRegister", Name: "注册", Description: "含第三方与短信自动建号", Enforcement: "AppService.EnsureRegisterAllowed"},
		{Key: platformdomain.CapabilityAPI, Field: "blockApi", Name: "业务接口", Description: "现存会话立即失效，刷新令牌也换不出新会话", Enforcement: "AuthService.ValidateAccessToken / Refresh"},
		{Key: platformdomain.CapabilityPayment, Field: "blockPayment", Name: "支付下单", Description: "只挡新订单；退款仍可发起，避免把用户已付的钱锁死", Enforcement: "PaymentService.CreateOrder"},
		{Key: platformdomain.CapabilityStorage, Field: "blockStorage", Name: "存储写入", Description: "上传与写入类操作，读取不受影响", Enforcement: "StorageService.Upload"},
		{Key: platformdomain.CapabilityNotification, Field: "blockNotification", Name: "对外通知", Description: "站内信与邮件出信", Enforcement: "NotificationService.Create / EmailService.sendMail"},
		{Key: platformdomain.CapabilityAdminWrite, Field: "blockAdminWrite", Name: "管理端写操作", Description: "应用管理员对该应用的一切写操作只读化；平台级管理员不受限", Enforcement: "middleware.AdminAccess"},
	}
	durations := []platformdomain.DurationOption{
		{Label: "1 小时", Seconds: 3600},
		{Label: "24 小时", Seconds: 86400},
		{Label: "7 天", Seconds: 7 * 86400},
		{Label: "30 天", Seconds: 30 * 86400},
		{Label: "90 天", Seconds: 90 * 86400},
		{Label: "不设期限", Seconds: 0},
	}
	return platformdomain.Catalog{States: states, Capabilities: capabilities, Durations: durations}
}

// ── 内部 ──

func (s *PlatformGovernanceService) applyLocal(item platformdomain.Governance) {
	s.mu.Lock()
	if item.IsActive() {
		delete(s.states, item.AppID)
	} else {
		s.states[item.AppID] = item
	}
	s.mu.Unlock()
	s.version.Add(1)
}

func (s *PlatformGovernanceService) applyLocalAppealStatus(appID int64, status string) {
	s.mu.Lock()
	if item, ok := s.states[appID]; ok {
		item.AppealStatus = status
		s.states[appID] = item
	}
	s.mu.Unlock()
}

func (s *PlatformGovernanceService) purgeAppSessions(ctx context.Context, appID int64) (redisrepo.AppSessionSweepResult, error) {
	if s.sessions == nil {
		return redisrepo.AppSessionSweepResult{}, nil
	}
	return s.sessions.PurgeAppSessions(ctx, appID)
}

func (s *PlatformGovernanceService) notifyAppAdmins(ctx context.Context, state platformdomain.Governance, record platformdomain.ActionRecord) {
	if s.inbox == nil {
		return
	}
	adminIDs, err := s.pg.ListAppAdminIDs(ctx, state.AppID)
	if err != nil {
		s.log.Warn("查询应用管理员失败，治理通知未发出", zap.Int64("appid", state.AppID), zap.Error(err))
		return
	}
	if len(adminIDs) == 0 {
		return
	}
	level := notificationdomain.AdminLevelWarning
	title := fmt.Sprintf("应用「%s」已被平台%s", state.AppName, governanceStateName(state.State))
	if state.IsActive() {
		level = notificationdomain.AdminLevelSuccess
		title = fmt.Sprintf("应用「%s」的平台限制已解除", state.AppName)
	} else if platformdomain.StateRequiresDanger(state.State) {
		level = notificationdomain.AdminLevelCritical
	}

	content := strings.TrimSpace(record.Reason)
	if content == "" {
		content = "平台未填写具体理由，如有疑问可在治理详情页提交申诉。"
	}
	if state.EndAt != nil {
		content += fmt.Sprintf("\n生效至：%s", state.EndAt.In(timeutil.DefaultLocation()).Format("2006-01-02 15:04"))
	}

	pushes := make([]notificationdomain.AdminInboxPush, 0, len(adminIDs))
	for _, adminID := range adminIDs {
		pushes = append(pushes, notificationdomain.AdminInboxPush{
			AdminID:    adminID,
			Type:       "platform_governance",
			Title:      title,
			Content:    content,
			Level:      level,
			Resource:   "app",
			ResourceID: fmt.Sprintf("%d", state.AppID),
			Link:       fmt.Sprintf("/apps?appid=%d&tab=governance", state.AppID),
			DedupeKey:  fmt.Sprintf("governance:%d:%d", state.AppID, record.ID),
			Metadata: map[string]any{
				"state":  state.State,
				"action": record.Action,
			},
		})
	}
	if _, err := s.inbox.Push(ctx, pushes); err != nil {
		s.log.Warn("治理通知投递失败", zap.Int64("appid", state.AppID), zap.Error(err))
	}
}

func (s *PlatformGovernanceService) notifyAppealResult(ctx context.Context, appeal platformdomain.Appeal, restored bool) {
	if s.inbox == nil || appeal.SubmittedByAdminID == nil {
		return
	}
	level := notificationdomain.AdminLevelSuccess
	title := fmt.Sprintf("应用「%s」的治理申诉已通过", appeal.AppName)
	if appeal.Status != platformdomain.AppealStatusApproved {
		level = notificationdomain.AdminLevelWarning
		title = fmt.Sprintf("应用「%s」的治理申诉未通过", appeal.AppName)
	} else if !restored {
		title = fmt.Sprintf("应用「%s」的治理申诉已通过（限制暂未解除）", appeal.AppName)
	}
	content := strings.TrimSpace(appeal.ReviewNote)
	if content == "" {
		content = "审核方未填写说明。"
	}
	if _, err := s.inbox.Push(ctx, []notificationdomain.AdminInboxPush{{
		AdminID:    *appeal.SubmittedByAdminID,
		Type:       "platform_governance",
		Title:      title,
		Content:    content,
		Level:      level,
		Resource:   "app",
		ResourceID: fmt.Sprintf("%d", appeal.AppID),
		Link:       fmt.Sprintf("/apps?appid=%d&tab=governance", appeal.AppID),
		DedupeKey:  fmt.Sprintf("governance:appeal:%d", appeal.ID),
	}}); err != nil {
		s.log.Warn("申诉结果通知投递失败", zap.Int64("appealId", appeal.ID), zap.Error(err))
	}
}

func (s *PlatformGovernanceService) emitHook(appID int64, state platformdomain.Governance, record platformdomain.ActionRecord) {
	if s.plugin == nil {
		return
	}
	go s.plugin.ExecuteHook(context.Background(), HookAppGovernanceChanged, map[string]any{
		"appId":     appID,
		"action":    record.Action,
		"fromState": record.FromState,
		"toState":   record.ToState,
		"reason":    record.Reason,
		"endAt":     state.EndAt,
	}, plugindomain.HookMetadata{AppID: &appID})
}

// AllowedGovernanceAppIDs 从管理员的角色分配推导可见应用范围。
// 返回 nil 表示不限范围（超管或持有全局角色）。
//
// 与 filterAppsByAssignments 的语义保持一致：任一全局角色（AppID 为空）即视为全站可见。
func AllowedGovernanceAppIDs(isSuperAdmin bool, assignments []admindomain.Assignment) []int64 {
	if isSuperAdmin {
		return nil
	}
	ids := make([]int64, 0, len(assignments))
	for _, item := range assignments {
		if item.AppID == nil {
			return nil
		}
		ids = append(ids, *item.AppID)
	}
	if len(ids) == 0 {
		// 没有任何分配：给一个不可能命中的范围，避免"空切片=不限范围"被误读成全站可见
		return []int64{-1}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func resolveGovernanceDeadline(targetState string, input platformdomain.ActionInput) (*time.Time, error) {
	if targetState == platformdomain.StateActive {
		return nil, nil
	}
	if platformdomain.StatePermanent(targetState) {
		if input.EndAt != nil || input.DurationSeconds > 0 {
			return nil, apperrors.New(errCodeGovInvalidDeadline, http.StatusBadRequest, "封禁与归档是永久状态，不接受到期时间")
		}
		return nil, nil
	}
	now := timeutil.NowUTC()
	var endAt *time.Time
	switch {
	case input.EndAt != nil:
		value := input.EndAt.UTC()
		endAt = &value
	case input.DurationSeconds > 0:
		value := now.Add(time.Duration(input.DurationSeconds) * time.Second)
		endAt = &value
	default:
		return nil, nil // 无限期，需人工解除
	}
	if !endAt.After(now.Add(time.Minute)) {
		return nil, apperrors.New(errCodeGovInvalidDeadline, http.StatusBadRequest, "到期时间必须晚于当前时间至少 1 分钟")
	}
	if endAt.After(now.Add(governanceMaxDuration)) {
		return nil, apperrors.New(errCodeGovInvalidDeadline, http.StatusBadRequest, "单次治理期限不能超过 5 年，需要长期停用请改用封禁或归档")
	}
	return endAt, nil
}

func governanceStateName(state string) string {
	switch state {
	case platformdomain.StateRestricted:
		return "限制"
	case platformdomain.StateFrozen:
		return "冻结"
	case platformdomain.StateSuspended:
		return "停运"
	case platformdomain.StateBanned:
		return "封禁"
	case platformdomain.StateArchived:
		return "归档"
	default:
		return "恢复"
	}
}

func governanceErrorCode(capability string) int {
	switch capability {
	case platformdomain.CapabilityLogin:
		return errCodeGovLoginBlocked
	case platformdomain.CapabilityRegister:
		return errCodeGovRegisterBlocked
	case platformdomain.CapabilityAPI:
		return errCodeGovAPIBlocked
	case platformdomain.CapabilityPayment:
		return errCodeGovPaymentBlocked
	case platformdomain.CapabilityStorage:
		return errCodeGovStorageBlocked
	case platformdomain.CapabilityNotification:
		return errCodeGovNotificationBlocked
	case platformdomain.CapabilityAdminWrite:
		return errCodeGovAdminWriteBlocked
	default:
		return errCodeGovAPIBlocked
	}
}

// governanceBlockMessage 面向被拒方的文案。
//
// 只说"被平台限制了哪一项、到什么时候"，不透出操作者与证据 ——
// 那些是内部信息，被治理方在控制台的治理详情页才看得到。
func governanceBlockMessage(state *platformdomain.Governance, capability string) string {
	action := governanceStateName(state.State)
	var subject string
	switch capability {
	case platformdomain.CapabilityLogin:
		subject = "登录"
	case platformdomain.CapabilityRegister:
		subject = "注册"
	case platformdomain.CapabilityPayment:
		subject = "支付"
	case platformdomain.CapabilityStorage:
		subject = "文件上传"
	case platformdomain.CapabilityNotification:
		subject = "消息发送"
	case platformdomain.CapabilityAdminWrite:
		return fmt.Sprintf("该应用已被平台%s，当前为只读状态，无法修改配置", action)
	default:
		subject = "服务"
	}
	message := fmt.Sprintf("该应用已被平台%s，%s暂不可用", action, subject)
	if reason := strings.TrimSpace(state.Reason); reason != "" {
		message += "：" + reason
	}
	if state.EndAt != nil {
		message += fmt.Sprintf("（预计 %s 恢复）", state.EndAt.In(timeutil.DefaultLocation()).Format("2006-01-02 15:04"))
	}
	return message
}

func governanceErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if appErr, ok := err.(*apperrors.AppError); ok {
		return appErr.Message
	}
	return err.Error()
}

func normalizePaging(page, limit, defaultLimit, maxLimit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return page, limit
}
