package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

// 有效反馈奖励积分（docs/feedback-rewards.md）。
//
// 权限：发放与撤回都要两样都有——
//   - 能处理这张反馈：该应用的 ticket:close（结单与重开），或与我有关（受理人 / 处理组 / 关注人……），与改状态同一口径；
//   - 能改这个应用用户的积分：app:user:asset（资产冲正）或 points:write（积分调整）。
//
// 只会处理反馈、没有积分权限的处理组成员看得到奖励记录，但看不到「发放」按钮（ActionSet.Reward 为假）。
// 策略的读取跟着工单可见范围走，修改要 ticket:manage（工单配置）。
//
// 积分流水、奖励记录、时间线在仓储层一个事务里写；站内信在提交后经 NotificationService.Deliver 发，
// 发送失败只记日志（积分已经到账，不能因为通知失败让整笔回滚）。

const (
	// permAppUserAsset 应用用户「资产冲正」（含调整积分），见 authz/tree.go 的 app.user 菜单
	permAppUserAsset = "app:user:asset"

	errCodeFeedbackRewardNotFound   = 40407 // 404 没有可撤回的奖励
	errCodeFeedbackRewardExists     = 40999 // 409 已发放过一笔数额不同的奖励
	errCodeFeedbackRewardDisabled   = 42257 // 422 应用未开启反馈奖励
	errCodeFeedbackRewardLimit      = 42258 // 422 超过单条 / 每日 / 每月上限
	errCodeFeedbackRewardIneligible = 42259 // 422 这张单子不能发奖励（不是反馈、提单人不是用户、已撤销、不采纳）
	feedbackRewardRequestIDMaxLen   = 64
)

// ticketUserNotifier 给应用用户发站内信（NotificationService.Deliver，全站唯一写入口）。
type ticketUserNotifier interface {
	Deliver(ctx context.Context, in notificationdomain.DeliverInput) (*notificationdomain.Item, error)
}

// FeedbackRewardResult 发放 / 撤回结果。
type FeedbackRewardResult struct {
	Reward        *ticketdomain.Reward `json:"reward"`
	BalanceBefore int64                `json:"balanceBefore"`
	BalanceAfter  int64                `json:"balanceAfter"`
	// Replayed 重复提交：这张反馈已有同样的一笔（或同一请求编号已处理过），原样返回、没有再动积分
	Replayed bool `json:"replayed"`
	// Notified 是否已给用户发站内信
	Notified bool `json:"notified"`
	// Info 操作之后的奖励区块（与详情里的 reward 同形）
	Info *ticketdomain.RewardInfo `json:"info,omitempty"`
}

// FeedbackRewardView 用户侧反馈详情里的奖励（只有当前有效的那一笔）。
type FeedbackRewardView struct {
	Points    int64     `json:"points"`
	Reason    string    `json:"reason,omitempty"`
	GrantedAt time.Time `json:"grantedAt"`
}

// canAdjustAppPoints 能否改这个应用用户的积分。
func (s *TicketService) canAdjustAppPoints(ctx context.Context, access *admindomain.AccessContext, appID int64) bool {
	scoped := &appID
	return s.can(ctx, access, permAppUserAsset, scoped) || s.can(ctx, access, authz.PermPointsWrite, scoped)
}

// resolveRewardActions 回填 ActionSet 的 Reward / RevokeReward（ResolveActions 调）。
func (s *TicketService) resolveRewardActions(ctx context.Context, access *admindomain.AccessContext,
	item *ticketdomain.Ticket, handle bool, actions *ticketdomain.ActionSet) {
	if item.Kind != ticketdomain.KindFeedback || !handle {
		return
	}
	allowed := s.canAdjustAppPoints(ctx, access, item.AppID)
	actions.Reward = allowed
	actions.RevokeReward = allowed
}

// ─────────────── 策略 ───────────────

// FeedbackRewardPolicy 读应用的反馈奖励策略（没有配置时返回推荐默认值，未开启）。
func (s *TicketService) FeedbackRewardPolicy(ctx context.Context, access *admindomain.AccessContext, appID int64) (*ticketdomain.RewardPolicy, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请指定应用")
	}
	scoped := &appID
	if !s.can(ctx, access, PermTicketRead, scoped) && !s.can(ctx, access, PermTicketManage, scoped) &&
		!s.canAdjustAppPoints(ctx, access, appID) {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权查看该应用的反馈奖励策略")
	}
	return s.pg.GetFeedbackRewardPolicy(ctx, appID)
}

// SaveFeedbackRewardPolicy 修改应用的反馈奖励策略（ticket:manage）。
func (s *TicketService) SaveFeedbackRewardPolicy(ctx context.Context, access *admindomain.AccessContext, policy ticketdomain.RewardPolicy) (*ticketdomain.RewardPolicy, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if policy.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请指定应用")
	}
	if !s.can(ctx, access, PermTicketManage, &policy.AppID) {
		return nil, apperrors.New(40311, http.StatusForbidden, "修改反馈奖励策略需要该应用的「工单配置」权限")
	}
	normalized, err := ticketdomain.NormalizeRewardPolicy(policy)
	if err != nil {
		return nil, mapFeedbackRewardError(err)
	}
	adminID := access.AdminID
	saved, err := s.pg.SaveFeedbackRewardPolicy(ctx, normalized, nullableAdmin(adminID), adminDisplayName(access))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return nil, apperrors.New(40400, http.StatusNotFound, "应用不存在")
		}
		return nil, err
	}
	return saved, nil
}

// ─────────────── 详情区块 ───────────────

// feedbackRewardInfo 管理端详情里的奖励区块。kind 不是反馈时返回 nil。
func (s *TicketService) feedbackRewardInfo(ctx context.Context, item *ticketdomain.Ticket) (*ticketdomain.RewardInfo, error) {
	if item == nil || item.Kind != ticketdomain.KindFeedback {
		return nil, nil
	}
	policy, err := s.pg.GetFeedbackRewardPolicy(ctx, item.AppID)
	if err != nil {
		return nil, err
	}
	history, err := s.pg.ListFeedbackRewards(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	info := &ticketdomain.RewardInfo{
		Enabled:         policy.Enabled,
		SuggestedPoints: policy.SuggestPoints(item.CategoryKey),
		MaxPoints:       policy.MaxPoints,
		Ineligible:      ticketdomain.RewardIneligibleReason(item),
		History:         history,
	}
	for i := range history {
		if history[i].Status == ticketdomain.RewardGranted {
			active := history[i]
			info.Active = &active
			break
		}
	}
	if item.RequesterUserID != nil {
		day, month := ticketdomain.RewardPeriodStarts(timeutil.Now(), s.location)
		usage, err := s.pg.FeedbackRewardUsage(ctx, item.AppID, *item.RequesterUserID, day, month)
		if err != nil {
			return nil, err
		}
		info.Usage = usage
	}
	info.MaxGrantable = policy.MaxGrantable(info.Usage)
	return info, nil
}

// FeedbackRewardInfo 一张反馈的奖励区块（能看到这张单子即可读）。
func (s *TicketService) FeedbackRewardInfo(ctx context.Context, access *admindomain.AccessContext, ticketID int64) (*ticketdomain.RewardInfo, error) {
	item, _, _, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	if item.Kind != ticketdomain.KindFeedback {
		return nil, apperrors.New(errCodeFeedbackRewardIneligible, http.StatusUnprocessableEntity, "只有意见反馈可以发放奖励")
	}
	return s.feedbackRewardInfo(ctx, item)
}

// ─────────────── 发放 ───────────────

// GrantFeedbackReward 给一条反馈发放奖励。Points 为 0 时按策略的建议值发。
func (s *TicketService) GrantFeedbackReward(ctx context.Context, access *admindomain.AccessContext, ticketID int64, cmd ticketdomain.RewardGrantCommand) (*FeedbackRewardResult, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	result, err := s.grantFeedbackReward(ctx, access, item, actions, cmd)
	if err != nil {
		return nil, err
	}
	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err == nil && refreshed != nil {
		result.Info, _ = s.feedbackRewardInfo(ctx, refreshed)
	}
	return result, nil
}

func (s *TicketService) grantFeedbackReward(ctx context.Context, access *admindomain.AccessContext,
	item *ticketdomain.Ticket, actions *ticketdomain.ActionSet, cmd ticketdomain.RewardGrantCommand) (*FeedbackRewardResult, error) {
	if reason := ticketdomain.RewardIneligibleReason(item); reason != "" {
		return nil, apperrors.New(errCodeFeedbackRewardIneligible, http.StatusUnprocessableEntity, reason)
	}
	if actions == nil || !actions.Reward {
		return nil, apperrors.New(40311, http.StatusForbidden, "发放反馈奖励需要能处理这条反馈，并持有该应用的积分调整权限")
	}
	reason, err := ticketdomain.NormalizeRewardReason(cmd.Reason)
	if err != nil {
		return nil, mapFeedbackRewardError(err)
	}
	requestID, err := normalizeRewardRequestID(cmd.RequestID)
	if err != nil {
		return nil, err
	}
	policy, err := s.pg.GetFeedbackRewardPolicy(ctx, item.AppID)
	if err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return nil, mapFeedbackRewardError(ticketdomain.ErrRewardDisabled)
	}
	points := cmd.Points
	if points == 0 {
		points = policy.SuggestPoints(item.CategoryKey)
	}
	if points <= 0 || points > ticketdomain.RewardMaxPoints {
		return nil, mapFeedbackRewardError(ticketdomain.ErrRewardPoints)
	}

	adminID := access.AdminID
	adminName := adminDisplayName(access)
	day, month := ticketdomain.RewardPeriodStarts(timeutil.Now(), s.location)
	granted, err := s.pg.GrantFeedbackReward(ctx, pgrepo.GrantFeedbackRewardInput{
		TicketID: item.ID, Points: points, Reason: reason, RequestID: requestID,
		AdminID: nullableAdmin(adminID), AdminName: adminName, Policy: *policy,
		DayStart: day, MonthStart: month, ClientIP: cmd.ClientIP, UserAgent: cmd.UserAgent,
	})
	if err != nil {
		return nil, mapFeedbackRewardError(err)
	}
	result := &FeedbackRewardResult{
		Reward: granted.Reward, BalanceBefore: granted.BalanceBefore, BalanceAfter: granted.BalanceAfter, Replayed: granted.Replayed,
	}
	if granted.Replayed {
		result.Notified = granted.Reward.NotificationID != nil
		return result, nil
	}
	result.Notified = s.notifyRewardGranted(ctx, granted.Reward)
	return result, nil
}

// notifyRewardGranted 「你的反馈 #xxx 被采纳，获得 N 积分」。失败只记日志。
func (s *TicketService) notifyRewardGranted(ctx context.Context, reward *ticketdomain.Reward) bool {
	if s.userNotifier == nil || reward == nil || reward.UserID == nil {
		return false
	}
	content := fmt.Sprintf("你的反馈 #%s 被采纳，获得 %d 积分。", reward.TicketNo, reward.Points)
	if reward.Reason != "" {
		content += reward.Reason
	}
	item, err := s.userNotifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: reward.AppID, UserID: *reward.UserID, Type: notificationdomain.TypeFeedbackReward,
		Title: "反馈奖励到账", Content: content, Level: notificationdomain.LevelSuccess,
		Action:   notificationdomain.RouteAction(notificationdomain.RoutePoints, "查看积分"),
		Metadata: rewardNotificationMetadata(reward, "granted"),
	})
	if err != nil {
		s.log.Warn("反馈奖励站内信发送失败", zap.Int64("ticketId", reward.TicketID), zap.Int64("rewardId", reward.ID), zap.Error(err))
		return false
	}
	if item != nil {
		if err := s.pg.SetFeedbackRewardNotification(ctx, reward.ID, item.ID); err != nil {
			s.log.Warn("记录反馈奖励站内信失败", zap.Int64("rewardId", reward.ID), zap.Error(err))
		}
	}
	return true
}

func rewardNotificationMetadata(reward *ticketdomain.Reward, action string) map[string]any {
	id := strconv.FormatInt(reward.TicketID, 10)
	return map[string]any{
		"event": "feedback_reward_" + action, "resource": "feedback", "resourceId": id, "link": "/feedback/" + id,
		"ticketNo": reward.TicketNo, "rewardId": reward.ID, "points": reward.Points,
	}
}

// ─────────────── 撤回 ───────────────

// RevokeFeedbackReward 撤回一条反馈当前有效的奖励并扣回积分。
func (s *TicketService) RevokeFeedbackReward(ctx context.Context, access *admindomain.AccessContext, ticketID int64, cmd ticketdomain.RewardRevokeCommand) (*FeedbackRewardResult, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	if item.Kind != ticketdomain.KindFeedback {
		return nil, apperrors.New(errCodeFeedbackRewardIneligible, http.StatusUnprocessableEntity, "只有意见反馈有奖励可撤回")
	}
	if !actions.RevokeReward {
		return nil, apperrors.New(40311, http.StatusForbidden, "撤回反馈奖励需要能处理这条反馈，并持有该应用的积分调整权限")
	}
	reason, err := ticketdomain.NormalizeRewardReason(cmd.Reason)
	if err != nil {
		return nil, mapFeedbackRewardError(err)
	}
	requestID, err := normalizeRewardRequestID(cmd.RequestID)
	if err != nil {
		return nil, err
	}
	adminID := access.AdminID
	revoked, err := s.pg.RevokeFeedbackReward(ctx, pgrepo.RevokeFeedbackRewardInput{
		TicketID: ticketID, Reason: reason, AllowPartial: cmd.AllowPartial, RequestID: requestID,
		AdminID: nullableAdmin(adminID), AdminName: adminDisplayName(access), ClientIP: cmd.ClientIP, UserAgent: cmd.UserAgent,
	})
	if err != nil {
		var balance *pgrepo.BalanceError
		if errors.As(err, &balance) && errors.Is(err, pgrepo.ErrInsufficientIntegral) {
			return nil, apperrors.New(40009, http.StatusBadRequest, fmt.Sprintf(
				"用户积分不足（当前 %s），不能整笔扣回；可选择「余额不足时扣到 0」", pointdomain.GroupDigits(balance.Balance)))
		}
		return nil, mapFeedbackRewardError(err)
	}
	result := &FeedbackRewardResult{
		Reward: revoked.Reward, BalanceBefore: revoked.BalanceBefore, BalanceAfter: revoked.BalanceAfter, Replayed: revoked.Replayed,
	}
	if !revoked.Replayed {
		result.Notified = s.notifyRewardRevoked(ctx, revoked.Reward)
	}
	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err == nil && refreshed != nil {
		result.Info, _ = s.feedbackRewardInfo(ctx, refreshed)
	}
	return result, nil
}

// notifyRewardRevoked 删掉当初那条「奖励到账」，再告诉用户奖励撤回了、扣回多少。失败只记日志。
func (s *TicketService) notifyRewardRevoked(ctx context.Context, reward *ticketdomain.Reward) bool {
	if reward == nil || reward.UserID == nil {
		return false
	}
	if reward.NotificationID != nil && s.userInbox != nil {
		if _, err := s.userInbox.AdminDelete(ctx, reward.AppID, []int64{*reward.NotificationID}); err != nil {
			s.log.Warn("删除反馈奖励站内信失败", zap.Int64("rewardId", reward.ID), zap.Error(err))
		}
	}
	if s.userNotifier == nil {
		return false
	}
	content := fmt.Sprintf("你的反馈 #%s 的 %d 积分奖励已撤回", reward.TicketNo, reward.Points)
	if reward.ClawedBack > 0 {
		content += fmt.Sprintf("，已扣回 %d 积分", reward.ClawedBack)
	}
	content += "。"
	if reward.RevokeReason != "" {
		content += reward.RevokeReason
	}
	if _, err := s.userNotifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: reward.AppID, UserID: *reward.UserID, Type: notificationdomain.TypeFeedbackReward,
		Title: "反馈奖励已撤回", Content: content, Level: notificationdomain.LevelWarning,
		Action:   notificationdomain.RouteAction(notificationdomain.RoutePoints, "查看积分"),
		Metadata: rewardNotificationMetadata(reward, "revoked"),
	}); err != nil {
		s.log.Warn("反馈奖励撤回站内信发送失败", zap.Int64("rewardId", reward.ID), zap.Error(err))
		return false
	}
	return true
}

// ─────────────── 用户侧 ───────────────

// feedbackRewardViews 用户侧：这些反馈当前有效的奖励（列表与详情共用，一次查询）。
func (s *TicketService) feedbackRewardViews(ctx context.Context, ticketIDs []int64) map[int64]*FeedbackRewardView {
	if len(ticketIDs) == 0 {
		return nil
	}
	rewards, err := s.pg.ActiveFeedbackRewards(ctx, ticketIDs)
	if err != nil {
		s.log.Warn("读取反馈奖励失败", zap.Error(err))
		return nil
	}
	out := make(map[int64]*FeedbackRewardView, len(rewards))
	for i := range rewards {
		reward := rewards[i]
		out[reward.TicketID] = &FeedbackRewardView{Points: reward.Points, Reason: reward.Reason, GrantedAt: reward.GrantedAt}
	}
	return out
}

// ─────────────── 工具 ───────────────

func normalizeRewardRequestID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if len(id) > feedbackRewardRequestIDMaxLen {
		return "", apperrors.New(40000, http.StatusBadRequest, "请求编号不能超过 64 个字符")
	}
	return id, nil
}

func nullableAdmin(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// mapFeedbackRewardError 把领域与仓储层的错误翻译成对调用方有意义的错误码。
func mapFeedbackRewardError(err error) error {
	var policyErr *ticketdomain.RewardPolicyError
	var limitErr *ticketdomain.RewardLimitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &policyErr):
		return apperrors.New(40000, http.StatusBadRequest, policyErr.Message)
	case errors.As(err, &limitErr):
		return apperrors.New(errCodeFeedbackRewardLimit, http.StatusUnprocessableEntity, limitErr.Error())
	case errors.Is(err, ticketdomain.ErrRewardDisabled):
		return apperrors.New(errCodeFeedbackRewardDisabled, http.StatusUnprocessableEntity, "该应用未开启反馈奖励，请先在「工单配置 → 反馈奖励」里开启")
	case errors.Is(err, ticketdomain.ErrRewardPoints):
		return apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("奖励积分须在 1 到 %d 之间", ticketdomain.RewardMaxPoints))
	case errors.Is(err, pgrepo.ErrFeedbackRewardExists):
		return apperrors.New(errCodeFeedbackRewardExists, http.StatusConflict, "这条反馈已经发放过奖励；要改数额请先撤回再发")
	case errors.Is(err, pgrepo.ErrFeedbackRewardNotFound):
		return apperrors.New(errCodeFeedbackRewardNotFound, http.StatusNotFound, "这条反馈没有可撤回的奖励")
	case errors.Is(err, pgrepo.ErrFeedbackTicketGone):
		return apperrors.New(errCodeFeedbackRewardIneligible, http.StatusUnprocessableEntity, "这条反馈已不能发放奖励（已撤销、不采纳或已删除）")
	default:
		return mapPointsAdjustError(pointdomain.KindIntegral, err)
	}
}
