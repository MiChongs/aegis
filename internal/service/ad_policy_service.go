package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/adpolicy"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

const (
	adPolicyStatsDefaultDays = 14
	adPolicyStatsMaxDays     = 90
	adPolicyProfileLogs      = 20
	adPolicyProfileSplash    = 10
)

// AdPolicyService 广告服务策略：用户是否同意广告服务、同意与否决定的服务范围、开屏广告记录。
//
// 同意是账号上的事实，判定只有一处（adpolicy.Evaluate）：网关下发给客户端的 mode、
// 管理端用户详情里看到的 mode 都出自它，两边不会各说各话。
type AdPolicyService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	rewarded *RewardedAdService
	// 选择变更后通知这个用户的其他在线设备、策略变更后通知应用下的全部在线客户端。为空时只是不推送。
	userEvents UserEventPublisher
	appEvents  AppEventPublisher
}

func NewAdPolicyService(log *zap.Logger, pg *pgrepo.Repository, rewarded *RewardedAdService) *AdPolicyService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AdPolicyService{log: log, pg: pg, rewarded: rewarded}
}

// SetEventPublishers 接上实时推送。
func (s *AdPolicyService) SetEventPublishers(user UserEventPublisher, app AppEventPublisher) {
	s.userEvents = user
	s.appEvents = app
}

// ── 管理端：策略 ──

// AdminPolicy 读取策略。没配过时返回默认值，configured 为 false。
func (s *AdPolicyService) AdminPolicy(ctx context.Context, appID int64) (*adpolicy.AdminPolicy, error) {
	policy, err := s.pg.GetAdPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return &adpolicy.AdminPolicy{Policy: adpolicy.DefaultPolicy(appID)}, nil
	}
	return &adpolicy.AdminPolicy{Policy: *policy, Configured: true}, nil
}

// SavePolicy 保存策略，并通知在线客户端重新拉取。
func (s *AdPolicyService) SavePolicy(ctx context.Context, input adpolicy.SavePolicyInput) (*adpolicy.AdminPolicy, error) {
	next := adpolicy.NormalizePolicy(input)
	if err := adpolicy.ValidatePolicy(next); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	// 条款版本只能往上调：调低等于让已经按新条款重新选择过的人，
	// 记录凭空变成「按将来的版本选的」，再调回来时他们不会被重新询问。
	current, err := s.pg.GetAdPolicy(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	if current != nil && next.ConsentVersion < current.ConsentVersion {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"条款版本不能调低（当前为 "+strconv.Itoa(current.ConsentVersion)+"）")
	}
	saved, err := s.pg.SaveAdPolicy(ctx, next)
	if err != nil {
		return nil, err
	}
	s.publishApp(input.AppID, "ad.policy.changed", map[string]any{
		"enabled":        saved.Enabled,
		"consentVersion": saved.ConsentVersion,
	})
	return &adpolicy.AdminPolicy{Policy: *saved, Configured: true}, nil
}

// ── 客户端 ──

// ClientPolicy 当前策略与当前用户的处境。userID 为 0 表示未登录：此时按设备算开屏频控，
// 选择一律按「还没选」回答（客户端以本机的选择为准）。
func (s *AdPolicyService) ClientPolicy(ctx context.Context, appID, userID int64, deviceID string) (*adpolicy.ClientPolicy, error) {
	now := time.Now()
	policy, configured, err := s.policyOrDefault(ctx, appID)
	if err != nil {
		return nil, err
	}
	result := &adpolicy.ClientPolicy{
		Configured:     configured,
		Enabled:        policy.Enabled,
		ConsentVersion: policy.ConsentVersion,
		PolicyURL:      policy.PolicyURL,
		VipExempt:      policy.VipExempt,
		BasicTools:     policy.BasicTools,
		SignedIn:       userID > 0,
		ServerTime:     now.UTC(),
	}
	if result.BasicTools == nil {
		result.BasicTools = []string{}
	}

	var consent *adpolicy.Consent
	if userID > 0 {
		consent, err = s.pg.GetUserAdConsent(ctx, appID, userID)
		if err != nil {
			return nil, err
		}
		result.Consent = consent
		result.Vip = s.isVip(ctx, appID, userID)
		if s.rewarded != nil {
			result.AdUserID = s.rewarded.AdUserToken(appID, userID)
		}
	}
	verdict := adpolicy.Evaluate(policy, consent, result.Vip)
	result.Mode = verdict.Mode
	result.DecisionRequired = verdict.DecisionRequired
	result.Exempt = verdict.Exempt

	usage, err := s.pg.SplashUsage(ctx, appID, userID, deviceID, adPolicyDayStart(now))
	if err != nil {
		return nil, err
	}
	// 未登录时服务端不知道本机的选择，同意这一条交给客户端判断。
	accepted := userID == 0 || (consent != nil && consent.Accepted && !verdict.DecisionRequired)
	available, next := adpolicy.SplashVerdict(policy.Splash, *usage, accepted, verdict.Exempt, now)
	result.Splash = adpolicy.ClientSplash{
		SplashConfig:    policy.Splash,
		TodayCount:      usage.TodayCount,
		LastShownAt:     usage.LastShownAt,
		NextAvailableAt: next,
		Available:       available,
	}
	return result, nil
}

// SaveConsent 记录用户的选择，返回记录后的策略视图。
func (s *AdPolicyService) SaveConsent(ctx context.Context, input adpolicy.SaveConsentInput) (*adpolicy.ClientPolicy, error) {
	policy, _, err := s.policyOrDefault(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	input.Source = strings.TrimSpace(input.Source)
	if input.Source == "" {
		input.Source = adpolicy.SourceApp
	}
	if !adpolicy.ValidSource(input.Source) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的选择来源")
	}
	if input.Version == 0 {
		input.Version = policy.ConsentVersion
	}
	if input.Version < 1 || input.Version > policy.ConsentVersion {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"条款版本无效（当前为 "+strconv.Itoa(policy.ConsentVersion)+"）")
	}
	input.DeviceID = adpolicy.TruncateRunes(input.DeviceID, 128)
	input.ClientIP = adpolicy.TruncateRunes(input.ClientIP, 64)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}

	consent, changed, err := s.pg.SaveUserAdConsent(ctx, input)
	if err != nil {
		return nil, err
	}
	result, err := s.ClientPolicy(ctx, input.AppID, input.UserID, input.DeviceID)
	if err != nil {
		return nil, err
	}
	if changed {
		s.publishUser(input.AppID, input.UserID, "ad.consent.changed", map[string]any{
			"accepted": consent.Accepted,
			"version":  consent.Version,
			"source":   consent.Source,
			"mode":     result.Mode,
		})
	}
	return result, nil
}

// ReportSplash 落一批开屏记录。不合法的单条直接丢弃，不让一条坏数据拖累整批 ——
// 客户端会把整批从队列里删掉，报错只会让它反复重传同一批。
func (s *AdPolicyService) ReportSplash(ctx context.Context, input adpolicy.SplashReportInput) (*adpolicy.SplashReportResult, error) {
	if len(input.Events) > adpolicy.MaxSplashBatch {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"每次最多上报 "+strconv.Itoa(adpolicy.MaxSplashBatch)+" 条")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	events := make([]adpolicy.SplashEvent, 0, len(input.Events))
	seen := map[string]bool{}
	for _, item := range input.Events {
		eventID := strings.TrimSpace(item.EventID)
		status := strings.ToLower(strings.TrimSpace(item.Status))
		if !adpolicy.ValidEventID(eventID) || !adpolicy.ValidSplashStatus(status) || seen[eventID] {
			continue
		}
		seen[eventID] = true
		// 账号只能来自令牌，客户端只能声明「这条不属于当前账号」，不能把记录挂到别人头上。
		userID := input.UserID
		if item.Anonymous {
			userID = 0
		}
		events = append(events, adpolicy.SplashEvent{
			UserID:       userID,
			EventID:      eventID,
			PlacementID:  adpolicy.TruncateRunes(item.PlacementID, 64),
			Status:       status,
			ErrorCode:    adpolicy.TruncateRunes(item.ErrorCode, 64),
			ErrorMessage: adpolicy.TruncateRunes(item.ErrorMessage, 256),
			LoadMs:       clampAdMillis(item.LoadMs),
			ShownMs:      clampAdMillis(item.ShownMs),
			OccurredAt:   adpolicy.ClampOccurredAt(item.OccurredAt, now),
		})
	}
	recorded, err := s.pg.RecordSplashEvents(ctx, input.AppID,
		adpolicy.TruncateRunes(input.DeviceID, 128), adpolicy.TruncateRunes(input.ClientIP, 64), events)
	if err != nil {
		return nil, err
	}
	return &adpolicy.SplashReportResult{Received: len(input.Events), Recorded: recorded}, nil
}

// ── 管理端：记录与统计 ──

// ListConsents 按当前选择列用户。
func (s *AdPolicyService) ListConsents(ctx context.Context, query adpolicy.ConsentQuery) (*adpolicy.Page[adpolicy.Consent], error) {
	policy, _, err := s.policyOrDefault(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	query.CurrentVersion = policy.ConsentVersion
	page, err := s.pg.ListAdConsents(ctx, query)
	if err != nil {
		return nil, err
	}
	for index := range page.Items {
		page.Items[index].Outdated = page.Items[index].Version < policy.ConsentVersion
	}
	return page, nil
}

// ListConsentLogs 选择的变更历史。
func (s *AdPolicyService) ListConsentLogs(ctx context.Context, query adpolicy.ConsentLogQuery) (*adpolicy.Page[adpolicy.ConsentLog], error) {
	return s.pg.ListAdConsentLogs(ctx, query)
}

// ListSplashEvents 开屏记录。
func (s *AdPolicyService) ListSplashEvents(ctx context.Context, query adpolicy.SplashQuery) (*adpolicy.Page[adpolicy.SplashEvent], error) {
	return s.pg.ListSplashEvents(ctx, query)
}

// Stats 概览与按天趋势。缺失的日期补零，前端不必自己对齐横轴。
func (s *AdPolicyService) Stats(ctx context.Context, appID int64, days int) (*adpolicy.Stats, error) {
	if days <= 0 {
		days = adPolicyStatsDefaultDays
	}
	if days > adPolicyStatsMaxDays {
		days = adPolicyStatsMaxDays
	}
	policy, _, err := s.policyOrDefault(ctx, appID)
	if err != nil {
		return nil, err
	}
	loc := timeutil.DefaultLocation()
	today := adPolicyDayStart(time.Now())
	since := today.AddDate(0, 0, -(days - 1))
	stats, err := s.pg.AdPolicyStats(ctx, appID, policy.ConsentVersion, since, loc.String())
	if err != nil {
		return nil, err
	}
	stats.Days = days
	stats.Trend = fillAdPolicyTrend(stats.Trend, since, days)
	if last := stats.Trend[len(stats.Trend)-1]; last.Date == today.Format("2006-01-02") {
		stats.Summary.TodayShown = last.Shown
		stats.Summary.TodayClicked = last.Clicked
		stats.Summary.TodayFailed = last.Failed
		stats.Summary.TodayUsers = last.SplashUsers
	}
	return stats, nil
}

// UserProfile 一个用户的广告服务情况：当前选择、服务模式、变更历史与开屏记录。
func (s *AdPolicyService) UserProfile(ctx context.Context, appID, userID int64) (*adpolicy.UserProfile, error) {
	policy, configured, err := s.policyOrDefault(ctx, appID)
	if err != nil {
		return nil, err
	}
	consent, err := s.pg.GetUserAdConsent(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	vip := s.isVip(ctx, appID, userID)
	verdict := adpolicy.Evaluate(policy, consent, vip)
	logs, err := s.pg.ListAdConsentLogs(ctx, adpolicy.ConsentLogQuery{
		AppID: appID, UserID: userID, Page: 1, Limit: adPolicyProfileLogs})
	if err != nil {
		return nil, err
	}
	summary, err := s.pg.UserSplashSummary(ctx, appID, userID, adPolicyDayStart(time.Now()))
	if err != nil {
		return nil, err
	}
	recent, err := s.pg.ListSplashEvents(ctx, adpolicy.SplashQuery{
		AppID: appID, UserID: userID, Page: 1, Limit: adPolicyProfileSplash})
	if err != nil {
		return nil, err
	}
	return &adpolicy.UserProfile{
		Configured:       configured,
		Enabled:          policy.Enabled,
		ConsentVersion:   policy.ConsentVersion,
		Consent:          consent,
		Vip:              vip,
		Exempt:           verdict.Exempt,
		DecisionRequired: verdict.DecisionRequired,
		Mode:             verdict.Mode,
		Logs:             logs.Items,
		Splash:           *summary,
		RecentSplash:     recent.Items,
	}, nil
}

// ── 内部 ──

func (s *AdPolicyService) policyOrDefault(ctx context.Context, appID int64) (adpolicy.Policy, bool, error) {
	policy, err := s.pg.GetAdPolicy(ctx, appID)
	if err != nil {
		return adpolicy.Policy{}, false, err
	}
	if policy == nil {
		return adpolicy.DefaultPolicy(appID), false, nil
	}
	return *policy, true, nil
}

// isVip 当前是否为有效会员（含试用与永久）。判定失败按非会员处理：
// 这里的结论只决定「免不免除」，一次查询抖动不该让会员突然被要求看广告以外的任何事。
func (s *AdPolicyService) isVip(ctx context.Context, appID, userID int64) bool {
	facts, err := s.pg.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		if !errors.Is(err, pgrepo.ErrUserNotFound) {
			s.log.Warn("ad policy: resolve vip failed", zap.Int64("appid", appID),
				zap.Int64("userId", userID), zap.Error(err))
		}
		return false
	}
	return vipdomain.Evaluate(*facts, time.Now()).IsVIP
}

func (s *AdPolicyService) publishUser(appID, userID int64, event string, data map[string]any) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.userEvents.PublishUserEvent(ctx, appID, userID, event, data); err != nil {
			s.log.Debug("publish ad policy user event failed", zap.String("event", event), zap.Error(err))
		}
	}()
}

func (s *AdPolicyService) publishApp(appID int64, event string, data map[string]any) {
	if s.appEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.appEvents.PublishAppEvent(ctx, appID, event, data); err != nil {
			s.log.Debug("publish ad policy app event failed", zap.String("event", event), zap.Error(err))
		}
	}()
}

func adPolicyDayStart(now time.Time) time.Time {
	return rewardedAdDayStart(now, timeutil.DefaultLocation())
}

// fillAdPolicyTrend 按日期顺序补齐窗口内缺失的日子。
func fillAdPolicyTrend(trend []adpolicy.StatsDay, since time.Time, days int) []adpolicy.StatsDay {
	byDate := make(map[string]adpolicy.StatsDay, len(trend))
	for _, day := range trend {
		byDate[day.Date] = day
	}
	out := make([]adpolicy.StatsDay, 0, days)
	for offset := 0; offset < days; offset++ {
		date := since.AddDate(0, 0, offset).Format("2006-01-02")
		day, ok := byDate[date]
		if !ok {
			day = adpolicy.StatsDay{Date: date}
		}
		out = append(out, day)
	}
	return out
}

// clampAdMillis 客户端报来的耗时：负数归零，超过 10 分钟的按 10 分钟记（多半是时钟跳变）。
func clampAdMillis(value int) int {
	const limit = 10 * 60 * 1000
	switch {
	case value < 0:
		return 0
	case value > limit:
		return limit
	}
	return value
}
