package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"aegis/internal/domain/invite"
	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/promo"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 推广活动的错误码（docs/promo.md）。message 是可以直接展示的原话。
const (
	errCodePromoNotFound           = 40415
	errCodePromoSubmissionNotFound = 40416
	errCodePromoAlreadyReviewed    = 40954
	errCodePromoTierClaimed        = 40955
	errCodePromoLinkDuplicate      = 40956
	errCodePromoNotActive          = 42240
	errCodePromoInviteDisabled     = 42241
	errCodePromoTierLocked         = 42242
	errCodePromoShareDisabled      = 42243
	errCodePromoPlatformUnavail    = 42244
	errCodePromoLinkInvalid        = 42245
	errCodePromoLinkForbidden      = 42246
	errCodePromoLimited            = 42247
	errCodePromoRewardUnavailable  = 42248

	promoStatsTimeout = 10 * time.Second
)

// PromoService 推广活动：管理端的活动配置、审核与统计，用户端的活动页、档位领取与文章提交。
//
// 人数与邀请码都来自邀请（InviteService）；判定只有 promo 包里的一处，用户端的「为什么不能」与错误码同源。
type PromoService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	invite     *InviteService
	notifier   notificationDeliverer
	userEvents UserEventPublisher
}

func NewPromoService(log *zap.Logger, pg *pgrepo.Repository, inviteService *InviteService) *PromoService {
	if log == nil {
		log = zap.NewNop()
	}
	return &PromoService{log: log, pg: pg, invite: inviteService}
}

// SetNotifier 接上站内信：审核有结论时通知用户。
func (s *PromoService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 接上实时推送：奖励到账、审核有结论后推 promo.changed。
func (s *PromoService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

// ── 管理端 ──

// Catalog 控制台的活动表单由它驱动。
func (s *PromoService) Catalog() map[string]any {
	return map[string]any{
		"platforms":          promo.PlatformCatalog(),
		"forbidden":          promo.ForbiddenPlatforms(),
		"rewards":            invite.RewardCatalog(),
		"statuses":           promo.Statuses(),
		"submissionStatuses": promo.SubmissionStatuses(),
		"defaults":           promo.DefaultConfig(),
		"limits": map[string]any{
			"maxTitleLength":        promo.MaxTitleRunes,
			"maxSubtitleLength":     promo.MaxSubtitleRunes,
			"maxRulesLength":        promo.MaxRulesRunes,
			"maxTierTitleLength":    promo.MaxTierTitleRunes,
			"maxShareLength":        promo.MaxShareTemplateRunes,
			"maxRejectReasonLength": promo.MaxRejectReasonRunes,
			"maxTiers":              promo.MaxTiers,
			"maxTierCount":          promo.MaxTierCount,
			"maxRewards":            invite.MaxRewardsPerList,
			"maxDurationDays":       promo.MaxDurationDays,
			"maxClaimGraceDays":     promo.MaxClaimGraceDays,
			"minLeaderboardSize":    promo.MinLeaderboardSize,
			"maxLeaderboardSize":    promo.MaxLeaderboardSize,
			"maxPerUserLimit":       promo.MaxPerUserLimit,
			"maxBlockedDomains":     promo.MaxBlockedDomains,
			"maxReviewBatch":        promo.MaxReviewBatch,
		},
	}
}

func (s *PromoService) prepare(ctx context.Context, input promo.CampaignInput) (promo.CampaignInput, error) {
	input = promo.NormalizeInput(input)
	if err := promo.ValidateInput(input); err != nil {
		return input, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.invite.checkRewardRefs(ctx, input.AppID, promo.AllRewards(input.Config)); err != nil {
		return input, err
	}
	return input, nil
}

// ListCampaigns 管理端列表。
func (s *PromoService) ListCampaigns(ctx context.Context, query promo.CampaignQuery) (*promo.Page[promo.AdminCampaign], error) {
	return s.pg.ListPromoCampaigns(ctx, query, time.Now())
}

// GetCampaign 管理端详情。
func (s *PromoService) GetCampaign(ctx context.Context, appID, campaignID int64) (*promo.Campaign, error) {
	campaign, err := s.pg.GetPromoCampaign(ctx, appID, campaignID)
	return campaign, s.translate(err, nil)
}

// CreateCampaign 新建（草稿）。
func (s *PromoService) CreateCampaign(ctx context.Context, input promo.CampaignInput) (*promo.Campaign, error) {
	input, err := s.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.pg.CreatePromoCampaign(ctx, input)
}

// UpdateCampaign 修改内容（上线中的也能改）。
func (s *PromoService) UpdateCampaign(ctx context.Context, campaignID int64, input promo.CampaignInput) (*promo.Campaign, error) {
	input, err := s.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	campaign, err := s.pg.UpdatePromoCampaign(ctx, campaignID, input)
	return campaign, s.translate(err, nil)
}

// SetStatus 上线 / 下线 / 退回草稿。上线前按当前配置再校验一遍（套餐、功能可能在草稿之后被删）。
func (s *PromoService) SetStatus(ctx context.Context, appID, campaignID int64, status, operator string) (*promo.Campaign, error) {
	status = strings.TrimSpace(status)
	switch status {
	case promo.StatusDraft, promo.StatusOnline, promo.StatusOffline:
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的活动状态")
	}
	if status == promo.StatusOnline {
		campaign, err := s.pg.GetPromoCampaign(ctx, appID, campaignID)
		if err != nil {
			return nil, s.translate(err, nil)
		}
		if _, err := s.prepare(ctx, promo.CampaignInput{
			AppID: appID, Title: campaign.Title, Subtitle: campaign.Subtitle, RulesText: campaign.RulesText,
			StartsAt: campaign.StartsAt, EndsAt: campaign.EndsAt, Config: campaign.Config,
		}); err != nil {
			return nil, err
		}
		if !time.Now().Before(campaign.EndsAt) {
			return nil, apperrors.New(40000, http.StatusBadRequest, "活动已过结束时间，请先修改时间再上线")
		}
	}
	campaign, err := s.pg.SetPromoCampaignStatus(ctx, appID, campaignID, status, operator)
	return campaign, s.translate(err, nil)
}

// DeleteCampaign 删除草稿或下线、且没人参与过的活动。
func (s *PromoService) DeleteCampaign(ctx context.Context, appID, campaignID int64) error {
	err := s.pg.DeletePromoCampaign(ctx, appID, campaignID)
	if errors.Is(err, pgrepo.ErrPromoCampaignInUse) {
		return apperrors.New(40000, http.StatusBadRequest, "上线中或已有人参与的活动不能删除，可以下线")
	}
	return s.translate(err, nil)
}

// Stats 一期活动的统计。
func (s *PromoService) Stats(ctx context.Context, appID, campaignID int64, timezone string) (*promo.Stats, error) {
	campaign, err := s.pg.GetPromoCampaign(ctx, appID, campaignID)
	if err != nil {
		return nil, s.translate(err, nil)
	}
	if strings.TrimSpace(timezone) == "" {
		timezone = timeutil.DefaultTimezone()
	} else if _, err := time.LoadLocation(timezone); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的时区")
	}
	ctx, cancel := context.WithTimeout(ctx, promoStatsTimeout)
	defer cancel()
	return s.pg.PromoStats(ctx, *campaign, timezone)
}

// AdminLeaderboard 管理端排行（带账号，不打码）。
func (s *PromoService) AdminLeaderboard(ctx context.Context, appID, campaignID int64, limit int) (*promo.Leaderboard, error) {
	campaign, err := s.pg.GetPromoCampaign(ctx, appID, campaignID)
	if err != nil {
		return nil, s.translate(err, nil)
	}
	if limit <= 0 || limit > promo.MaxLeaderboardSize {
		limit = promo.MaxLeaderboardSize
	}
	return s.pg.PromoLeaderboard(ctx, *campaign, limit, 0)
}

// ListSubmissions 管理端提交列表。
func (s *PromoService) ListSubmissions(ctx context.Context, query promo.SubmissionQuery) (*promo.Page[promo.Submission], error) {
	return s.pg.ListPromoSubmissions(ctx, query)
}

// Review 批量审核：逐条处理，一条失败不影响别的，返回每条的结论。
func (s *PromoService) Review(ctx context.Context, input promo.ReviewInput) ([]promo.ReviewOutcome, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	switch input.Action {
	case promo.ReviewApprove:
		input.Reason = ""
	case promo.ReviewReject:
		if input.Reason == "" {
			return nil, apperrors.New(40000, http.StatusBadRequest, "驳回需要填写原因")
		}
		if utf8.RuneCountInString(input.Reason) > promo.MaxRejectReasonRunes {
			return nil, apperrors.New(40000, http.StatusBadRequest, "驳回原因最多 "+strconv.Itoa(promo.MaxRejectReasonRunes)+" 字")
		}
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的审核动作")
	}
	ids := make([]int64, 0, len(input.IDs))
	seen := map[int64]bool{}
	for _, id := range input.IDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要审核的提交")
	}
	if len(ids) > promo.MaxReviewBatch {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多审核 "+strconv.Itoa(promo.MaxReviewBatch)+" 条")
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	refs := pgrepo.PromoRefs{Plans: plans, Features: features}
	campaigns := map[int64]*promo.Campaign{}
	out := make([]promo.ReviewOutcome, 0, len(ids))
	for _, id := range ids {
		outcome, err := s.pg.ReviewPromoSubmission(ctx, input.AppID, id, input.Action, input.Reason, input.Operator, campaigns, refs)
		if err != nil {
			failed := promo.ReviewOutcome{ID: id}
			var unavailable *pgrepo.PromoUnavailableError
			switch {
			case errors.As(err, &unavailable):
				failed.Reason = unavailable.Reason
				var campaign promo.Campaign
				if outcome != nil && campaigns[outcome.CampaignID] != nil {
					campaign = *campaigns[outcome.CampaignID]
				}
				failed.Message = promo.ReasonMessage(unavailable.Reason, campaign, "")
			case errors.Is(err, pgrepo.ErrPromoSubmissionNotFound):
				failed.Reason, failed.Message = promo.ReasonNotFound, "提交不存在"
			default:
				s.log.Warn("promo: review failed", zap.Int64("submissionId", id), zap.Error(err))
				failed.Reason, failed.Message = "error", "审核失败，请稍后重试"
			}
			out = append(out, failed)
			continue
		}
		out = append(out, *outcome)
		s.afterReview(input.AppID, *outcome)
	}
	return out, nil
}

func (s *PromoService) afterReview(appID int64, outcome promo.ReviewOutcome) {
	s.publishUser(appID, outcome.UserID, outcome.CampaignID)
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meta := map[string]any{"scene": "promo", "campaignId": outcome.CampaignID, "submissionId": outcome.ID}
		if outcome.Status == promo.SubmissionApproved {
			content := "你在「" + outcome.Title + "」提交的推广文章已通过审核"
			if len(outcome.Results) > 0 {
				content += "，奖励已到账：" + describeResults(outcome.Results)
			}
			s.notifyUser(ctx, appID, outcome.UserID, "推广文章审核通过", content+"。", "success", meta)
			return
		}
		s.notifyUser(ctx, appID, outcome.UserID, "推广文章未通过审核",
			"你在「"+outcome.Title+"」提交的推广文章未通过审核，原因："+outcome.Message+"。修改后可以重新提交。", "warning", meta)
	}()
}

func (s *PromoService) notifyUser(ctx context.Context, appID, userID int64, title, content, level string, meta map[string]any) {
	if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: appID, UserID: userID, Type: notificationdomain.TypeInvite, Title: title, Content: content, Level: level, Metadata: meta,
		Action: notificationdomain.RouteAction(notificationdomain.RoutePromo, "查看活动"),
	}); err != nil {
		s.log.Warn("promo: notify user failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func (s *PromoService) publishUser(appID, userID, campaignID int64) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.userEvents.PublishUserEvent(ctx, appID, userID, "promo.changed",
			map[string]any{"campaignId": campaignID}); err != nil {
			s.log.Debug("publish promo event failed", zap.Error(err))
		}
	}()
}

// ── 用户端 ──

// onlineCampaign 用户端能看到的一期活动；看不到的一律当不存在。
func (s *PromoService) onlineCampaign(ctx context.Context, appID, campaignID int64, now time.Time) (*promo.Campaign, error) {
	campaign, err := s.pg.GetPromoCampaign(ctx, appID, campaignID)
	if err != nil {
		return nil, s.translate(err, nil)
	}
	if !promo.VisibleToUsers(*campaign, now) {
		return nil, s.reasonError(promo.ReasonNotFound, *campaign, "")
	}
	return campaign, nil
}

// Campaigns 用户端列表。
func (s *PromoService) Campaigns(ctx context.Context, appID, userID int64) ([]promo.ClientCampaignSummary, error) {
	now := time.Now()
	campaigns, err := s.pg.ListOnlinePromoCampaigns(ctx, appID, now)
	if err != nil {
		return nil, err
	}
	out := make([]promo.ClientCampaignSummary, 0, len(campaigns))
	for _, campaign := range campaigns {
		item := promo.ClientCampaignSummary{
			ID: campaign.ID, Title: campaign.Title, Subtitle: campaign.Subtitle, Phase: promo.Phase(campaign, now),
			StartsAt: campaign.StartsAt, EndsAt: campaign.EndsAt,
			HasInvite: campaign.Config.Invite.Enabled, HasShare: campaign.Config.Share.Enabled,
		}
		if item.HasInvite {
			count, _, err := s.pg.PromoInviteCounts(ctx, campaign, userID)
			if err != nil {
				return nil, err
			}
			claims, err := s.pg.PromoTierClaims(ctx, campaign.ID, userID)
			if err != nil {
				return nil, err
			}
			for _, tier := range campaign.Config.Invite.Tiers {
				_, claimed := claims[tier.Count]
				if promo.TierState(campaign, tier, count, claimed, now) == promo.TierClaimable {
					item.Claimable = true
					break
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// Campaign 用户端详情。
func (s *PromoService) Campaign(ctx context.Context, appID, userID, campaignID int64) (*promo.ClientCampaign, error) {
	now := time.Now()
	campaign, err := s.onlineCampaign(ctx, appID, campaignID, now)
	if err != nil {
		return nil, err
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &promo.ClientCampaign{
		ID: campaign.ID, Title: campaign.Title, Subtitle: campaign.Subtitle, RulesText: campaign.RulesText,
		Phase: promo.Phase(*campaign, now), StartsAt: campaign.StartsAt, EndsAt: campaign.EndsAt, ClaimUntil: campaign.ClaimUntil(),
	}
	// 邀请码与分享链接来自邀请页：两项任务共用
	center, err := s.invite.Center(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	code, link := "", ""
	if center.Enabled && center.CanInvite {
		code, link = center.Code, center.ShareLink
	}

	if task := campaign.Config.Invite; task.Enabled {
		count, pending, err := s.pg.PromoInviteCounts(ctx, *campaign, userID)
		if err != nil {
			return nil, err
		}
		claims, err := s.pg.PromoTierClaims(ctx, campaign.ID, userID)
		if err != nil {
			return nil, err
		}
		section := &promo.ClientInvite{
			Count: count, Pending: pending, Code: code, ShareLink: link, Tiers: []promo.ClientTier{},
			Leaderboard: task.Leaderboard,
		}
		switch {
		case !center.Enabled:
			section.InviteMessage = "邀请功能暂未开放"
		case !center.CanInvite:
			section.InviteMessage = center.InviteMessage
		default:
			section.CanInvite, section.ShareText = true, center.ShareText
		}
		for _, tier := range task.Tiers {
			claimedAt, claimed := claims[tier.Count]
			item := promo.ClientTier{
				Count: tier.Count, Title: tier.Title, Rewards: invite.DescribeRewards(tier.Rewards, plans, features),
				State: promo.TierState(*campaign, tier, count, claimed, now),
			}
			if claimed {
				at := claimedAt
				item.ClaimedAt = &at
			}
			section.Tiers = append(section.Tiers, item)
		}
		out.Invite = section
	}

	if task := campaign.Config.Share; task.Enabled {
		facts, err := s.pg.PromoSubmitFacts(ctx, campaign.ID, userID, s.dayStart(now))
		if err != nil {
			return nil, err
		}
		section := &promo.ClientShare{
			Platforms: []promo.ClientPlatform{}, Forbidden: promo.ForbiddenNames(),
			Rewards:            invite.DescribeRewards(task.Rewards, plans, features),
			MaxApprovedPerUser: task.MaxApprovedPerUser, MaxApprovedPerPlatform: task.MaxApprovedPerPlatform,
			DailySubmitLimit: task.DailySubmitLimit,
			Approved:         facts.Approved, Pending: facts.Pending, TodaySubmitted: facts.TodaySubmitted,
		}
		names := make([]string, 0, len(task.Platforms))
		for _, key := range task.Platforms {
			platform, ok := promo.FindPlatform(key)
			if !ok {
				continue
			}
			names = append(names, platform.Name)
			section.Platforms = append(section.Platforms, promo.ClientPlatform{
				Key: platform.Key, Name: platform.Name, Hint: platform.Hint, PackageName: platform.PackageName, Domains: platform.Domains,
			})
		}
		section.ShareText = promo.RenderShareText(task.ShareTemplate, code, link, s.invite.appName(ctx, appID), strings.Join(names, "、"))
		reason := promo.EvaluateSubmitWindow(*campaign, facts, now)
		section.CanSubmit = reason == ""
		section.Reason, section.Message = reason, promo.ReasonMessage(reason, *campaign, "")
		out.Share = section
	}
	return out, nil
}

// Leaderboard 用户端排行：昵称（没有昵称时账号打码），不返回账号与用户 ID。
func (s *PromoService) Leaderboard(ctx context.Context, appID, userID, campaignID int64) (*promo.Leaderboard, error) {
	campaign, err := s.onlineCampaign(ctx, appID, campaignID, time.Now())
	if err != nil {
		return nil, err
	}
	if !campaign.Config.Invite.Enabled || !campaign.Config.Invite.Leaderboard {
		return &promo.Leaderboard{Items: []promo.RankEntry{}, Me: &promo.RankMe{}}, nil
	}
	board, err := s.pg.PromoLeaderboard(ctx, *campaign, campaign.Config.Invite.LeaderboardSize, userID)
	if err != nil {
		return nil, err
	}
	for index := range board.Items {
		board.Items[index].Account = ""
	}
	return board, nil
}

// ClaimTier 领取一档邀请奖励。
func (s *PromoService) ClaimTier(ctx context.Context, appID, userID, campaignID int64, count int) (*promo.TierGrant, error) {
	now := time.Now()
	campaign, err := s.onlineCampaign(ctx, appID, campaignID, now)
	if err != nil {
		return nil, err
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	grant, err := s.pg.ClaimPromoTier(ctx, *campaign, userID, count, pgrepo.PromoRefs{Plans: plans, Features: features}, now)
	if err != nil {
		return nil, s.translate(err, campaign)
	}
	s.publishUser(appID, userID, campaignID)
	return grant, nil
}

// MySubmissions 我在一期活动里的提交。
func (s *PromoService) MySubmissions(ctx context.Context, appID, userID, campaignID int64, page, limit int) (*promo.Page[promo.ClientSubmission], error) {
	if _, err := s.onlineCampaign(ctx, appID, campaignID, time.Now()); err != nil {
		return nil, err
	}
	raw, err := s.pg.ListMyPromoSubmissions(ctx, campaignID, userID, page, limit)
	if err != nil {
		return nil, err
	}
	items := make([]promo.ClientSubmission, 0, len(raw.Items))
	for _, item := range raw.Items {
		items = append(items, clientSubmission(item))
	}
	return &promo.Page[promo.ClientSubmission]{Items: items, Total: raw.Total, Page: raw.Page, Limit: raw.Limit, TotalPages: raw.TotalPages}, nil
}

func clientSubmission(item promo.Submission) promo.ClientSubmission {
	return promo.ClientSubmission{
		ID: item.ID, Platform: item.Platform, PlatformName: promo.PlatformName(item.Platform), URL: item.URL, Note: item.Note,
		Status: item.Status, RejectReason: item.RejectReason, Results: item.Results, CreatedAt: item.CreatedAt, ReviewedAt: item.ReviewedAt,
	}
}

// Submit 提交一篇推广文章。
func (s *PromoService) Submit(ctx context.Context, input promo.SubmitInput) (*promo.ClientSubmission, error) {
	now := time.Now()
	campaign, err := s.onlineCampaign(ctx, input.AppID, input.CampaignID, now)
	if err != nil {
		return nil, err
	}
	input.Platform = strings.TrimSpace(input.Platform)
	input.Note = strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(input.Note) > promo.MaxNoteRunes {
		return nil, apperrors.New(40000, http.StatusBadRequest, "备注最多 "+strconv.Itoa(promo.MaxNoteRunes)+" 字")
	}
	if len(input.DeviceID) > 128 {
		input.DeviceID = input.DeviceID[:128]
	}
	var link promo.Link
	linkReason := ""
	if platform, ok := promo.FindPlatform(input.Platform); ok {
		link, linkReason = promo.ParseLink(input.URL, platform, campaign.Config.Share.BlockedDomains)
	}
	item, err := s.pg.CreatePromoSubmission(ctx, *campaign, input, link, linkReason, s.dayStart(now), now)
	if err != nil {
		return nil, s.translate(err, campaign, input.Platform)
	}
	view := clientSubmission(*item)
	return &view, nil
}

func (s *PromoService) dayStart(now time.Time) time.Time {
	local := now.In(timeutil.DefaultLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

// reasonError 判据 → 带错误码的业务错误。
func (s *PromoService) reasonError(reason string, campaign promo.Campaign, platform string) error {
	message := promo.ReasonMessage(reason, campaign, platform)
	switch reason {
	case promo.ReasonNotFound:
		return apperrors.New(errCodePromoNotFound, http.StatusNotFound, message)
	case promo.ReasonNotStarted, promo.ReasonEnded:
		return apperrors.New(errCodePromoNotActive, http.StatusUnprocessableEntity, message)
	case promo.ReasonInviteDisabled:
		return apperrors.New(errCodePromoInviteDisabled, http.StatusUnprocessableEntity, message)
	case promo.ReasonTierNotFound, promo.ReasonTierLocked:
		return apperrors.New(errCodePromoTierLocked, http.StatusUnprocessableEntity, message)
	case promo.ReasonTierClaimed:
		return apperrors.New(errCodePromoTierClaimed, http.StatusConflict, message)
	case promo.ReasonShareDisabled:
		return apperrors.New(errCodePromoShareDisabled, http.StatusUnprocessableEntity, message)
	case promo.ReasonPlatformUnavailable:
		return apperrors.New(errCodePromoPlatformUnavail, http.StatusUnprocessableEntity, message)
	case promo.ReasonLinkInvalid, promo.ReasonLinkPlatformMismatch:
		return apperrors.New(errCodePromoLinkInvalid, http.StatusUnprocessableEntity, message)
	case promo.ReasonLinkForbidden:
		return apperrors.New(errCodePromoLinkForbidden, http.StatusUnprocessableEntity, message)
	case promo.ReasonLinkDuplicate:
		return apperrors.New(errCodePromoLinkDuplicate, http.StatusConflict, message)
	case promo.ReasonDailyLimited, promo.ReasonUserLimited, promo.ReasonPlatformLimited:
		return apperrors.New(errCodePromoLimited, http.StatusUnprocessableEntity, message)
	case promo.ReasonRewardUnavailable:
		return apperrors.New(errCodePromoRewardUnavailable, http.StatusUnprocessableEntity, message)
	case promo.ReasonAlreadyReviewed:
		return apperrors.New(errCodePromoAlreadyReviewed, http.StatusConflict, message)
	default:
		return apperrors.New(errCodePromoNotActive, http.StatusUnprocessableEntity, message)
	}
}

// translate 把仓储层的错误翻成业务错误。campaign 为空时上限类的说明不带数字。
func (s *PromoService) translate(err error, campaign *promo.Campaign, platform ...string) error {
	if err == nil {
		return nil
	}
	current := promo.Campaign{}
	if campaign != nil {
		current = *campaign
	}
	platformKey := ""
	if len(platform) > 0 {
		platformKey = platform[0]
	}
	var unavailable *pgrepo.PromoUnavailableError
	if errors.As(err, &unavailable) {
		return s.reasonError(unavailable.Reason, current, platformKey)
	}
	switch {
	case errors.Is(err, pgrepo.ErrPromoCampaignNotFound):
		return apperrors.New(errCodePromoNotFound, http.StatusNotFound, "活动不存在或已下线")
	case errors.Is(err, pgrepo.ErrPromoSubmissionNotFound):
		return apperrors.New(errCodePromoSubmissionNotFound, http.StatusNotFound, "提交不存在")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}
