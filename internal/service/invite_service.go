package service

import (
	riskevents "aegis/services/risk/events"
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
	"aegis/internal/domain/pointstore"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 邀请的错误码。客户端按码分支，message 是可以直接展示的原话。
const (
	errCodeInviteRewardUnavailable = 40355
	errCodeInviteCustomForbidden   = 40356
	errCodeInviteNotEligible       = 40357
	errCodeInviteDisabled          = 40363
	errCodeInviteWindowClosed      = 40364
	errCodeInviteAlreadyBound      = 40365
	errCodeInviteCodeInvalid       = 40366
	errCodeInviteSelf              = 40367
	errCodeInviteLimited           = 40368
	errCodeInviteRequired          = 40369
	errCodeInviteCodeNotFound      = 40446
	errCodeInviteRelationNotFound  = 40447
	errCodeInviteRelationHandled   = 40914
	errCodeInviteCodeTaken         = 40948

	inviteStatsDefaultDays = 14
	inviteStatsMaxDays     = 90
	// inviteCodeAttempts 生成个人码 / 活动码时撞码换码的次数
	inviteCodeAttempts = 8
	// inviteRegisterGrace 注册接口返回后多久以内的账号仍算「注册时填写」。
	// 短信注册对已有手机号是直接登录：那不是新账号，填的码要按补填的规则走。
	inviteRegisterGrace = 10 * time.Minute
)

// inviteAppNames 取应用名（分享文案里的 {app}）。
type inviteAppNames interface {
	AppName(ctx context.Context, appID int64) string
}

// InviteService 邀请：配置、个人码与活动码、填写邀请码、邀请人奖励的结算。
//
// 能不能填写只有一处判定（invite.EvaluateBind，在仓储的事务里取齐事实后调用）：
// 邀请页上「为什么不能填」与填写失败的错误码出自同一套判据。
type InviteService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	notifier   notificationDeliverer
	userEvents UserEventPublisher
	risk       *RiskService
	apps       inviteAppNames
}

func NewInviteService(log *zap.Logger, pg *pgrepo.Repository) *InviteService {
	if log == nil {
		log = zap.NewNop()
	}
	return &InviteService{log: log, pg: pg}
}

// SetNotifier 接上站内信：邀请人拿到奖励时通知他。
func (s *InviteService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 接上实时推送：奖励到账后通知双方的在线设备刷新积分与权益。
func (s *InviteService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

// SetRiskService 接上风控：填写邀请码走 invite 场景。
func (s *InviteService) SetRiskService(risk *RiskService) { s.risk = risk }

// SetAppNames 接上应用名（分享文案）。
func (s *InviteService) SetAppNames(apps inviteAppNames) { s.apps = apps }

// ── 管理端：目录与配置 ──

// Catalog 控制台的配置表单与活动码表单由它驱动。
func (s *InviteService) Catalog(ctx context.Context, appID int64) (map[string]any, error) {
	return map[string]any{
		"rewards":        invite.RewardCatalog(),
		"registerFields": invite.RegisterFields(),
		"charsets":       invite.Charsets(),
		"audiences":      invite.Audiences(),
		"triggers":       invite.Triggers(),
		"defaults":       invite.DefaultConfig(),
		"limits": map[string]any{
			"maxRewards":          invite.MaxRewardsPerList,
			"maxMilestones":       invite.MaxMilestones,
			"maxBindWindowHours":  invite.MaxBindWindowHours,
			"minCodeLength":       invite.MinCodeLength,
			"minDigitsCodeLength": invite.MinDigitsCodeLength,
			"maxCodeLength":       invite.MaxCodeLength,
			"maxPrefixLength":     invite.MaxPrefixLength,
			"minCustomCodeLength": invite.MinCustomCodeLength,
			"maxCustomCodeLength": invite.MaxCustomCodeLength,
			"maxBatchCount":       invite.MaxBatchCount,
			"maxRulesLength":      invite.MaxRulesRunes,
			"maxShareLength":      invite.MaxShareRunes,
		},
	}, nil
}

// Settings 配置（没保存过时为默认配置、关闭）。
func (s *InviteService) Settings(ctx context.Context, appID int64) (*invite.Settings, error) {
	return s.pg.GetInviteSettings(ctx, appID)
}

// SaveSettings 保存开关与配置。引用的套餐与功能在保存时就核对：
// 等到有人填写时才发现发不出去，那时被拒的是一个刚注册、满心期待奖励的新用户。
func (s *InviteService) SaveSettings(ctx context.Context, appID int64, enabled bool, cfg invite.Config, operator string) (*invite.Settings, error) {
	cfg = invite.NormalizeConfig(cfg)
	if err := invite.ValidateConfig(cfg); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.checkRewardRefs(ctx, appID, cfg.AllRewards()); err != nil {
		return nil, err
	}
	return s.pg.SaveInviteSettings(ctx, appID, enabled, cfg, operator)
}

func (s *InviteService) checkRewardRefs(ctx context.Context, appID int64, rewards []invite.Reward) error {
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return err
	}
	for _, reward := range rewards {
		switch reward.Type {
		case pointstore.RewardVipPlan:
			plan, ok := plans[reward.RefID]
			if !ok {
				return apperrors.New(40000, http.StatusBadRequest, "所选会员套餐不存在")
			}
			if plan.Trial {
				return apperrors.New(40000, http.StatusBadRequest, "试用套餐不能作为邀请奖励")
			}
		case pointstore.RewardFeature:
			if _, ok := features[reward.Tag]; !ok {
				return apperrors.New(40000, http.StatusBadRequest,
					"功能标识 "+reward.Tag+" 未登记，请先在会员功能目录里创建它")
			}
		}
	}
	return nil
}

// ── 管理端：邀请码 ──

// ListCodes 邀请码列表。
func (s *InviteService) ListCodes(ctx context.Context, query invite.CodeQuery) (*invite.Page[invite.AdminCode], error) {
	return s.pg.ListInviteCodes(ctx, query)
}

// CreateCampaignCodes 创建活动码：指定一个码，或按应用的格式（可另给前缀）批量生成。
func (s *InviteService) CreateCampaignCodes(ctx context.Context, input invite.CodeInput) ([]invite.Code, error) {
	input, err := s.prepareCampaign(ctx, input)
	if err != nil {
		return nil, err
	}
	if input.Code != "" {
		code := invite.NormalizeCode(input.Code)
		if err := invite.ValidateCustomCode(code); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		taken, err := s.pg.InviteCodeTaken(ctx, input.AppID, code)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, apperrors.New(errCodeInviteCodeTaken, http.StatusConflict, "邀请码已被占用")
		}
		codes, err := s.pg.CreateInviteCampaignCodes(ctx, input, []string{code})
		if pgrepo.IsUniqueViolation(err) {
			return nil, apperrors.New(errCodeInviteCodeTaken, http.StatusConflict, "邀请码已被占用")
		}
		return codes, err
	}

	count := input.Count
	if count <= 0 {
		count = 1
	}
	if count > invite.MaxBatchCount {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多生成 "+strconv.Itoa(invite.MaxBatchCount)+" 个")
	}
	prefix := invite.NormalizeCode(input.Prefix)
	if len(prefix) > invite.MaxPrefixLength || (prefix != "" && !invite.ValidCodeShape(prefix)) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "前缀只能使用字母与数字，最多 "+strconv.Itoa(invite.MaxPrefixLength)+" 位")
	}
	settings, err := s.pg.GetInviteSettings(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < inviteCodeAttempts; attempt++ {
		codes := make([]string, 0, count)
		seen := map[string]bool{}
		for len(codes) < count {
			code, err := invite.GenerateCodeWithPrefix(settings.Config, prefix)
			if err != nil {
				return nil, err
			}
			if !seen[code] {
				seen[code] = true
				codes = append(codes, code)
			}
		}
		created, err := s.pg.CreateInviteCampaignCodes(ctx, input, codes)
		if err == nil {
			return created, nil
		}
		if !pgrepo.IsUniqueViolation(err) {
			return nil, err
		}
	}
	return nil, apperrors.New(errCodeInviteCodeTaken, http.StatusConflict, "生成的邀请码与已有的重复，请加长邀请码或更换前缀后重试")
}

// UpdateCampaignCode 更新活动码（码本身不改）。
func (s *InviteService) UpdateCampaignCode(ctx context.Context, codeID int64, input invite.CodeInput) (*invite.Code, error) {
	input, err := s.prepareCampaign(ctx, input)
	if err != nil {
		return nil, err
	}
	code, err := s.pg.UpdateInviteCampaignCode(ctx, codeID, input)
	return code, s.translate(err)
}

// SetCodeStatus 启用 / 停用一个码。
func (s *InviteService) SetCodeStatus(ctx context.Context, appID, codeID int64, active bool) (*invite.Code, error) {
	status := invite.CodeDisabled
	if active {
		status = invite.CodeActive
	}
	code, err := s.pg.SetInviteCodeStatus(ctx, appID, codeID, status)
	return code, s.translate(err)
}

// DeleteCode 删除活动码。
func (s *InviteService) DeleteCode(ctx context.Context, appID, codeID int64) error {
	return s.translate(s.pg.DeleteInviteCode(ctx, appID, codeID))
}

func (s *InviteService) prepareCampaign(ctx context.Context, input invite.CodeInput) (invite.CodeInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Note = strings.TrimSpace(input.Note)
	if input.Status == "" {
		input.Status = invite.CodeActive
	}
	switch {
	case input.Title == "":
		return input, apperrors.New(40000, http.StatusBadRequest, "请填写活动名称")
	case utf8.RuneCountInString(input.Title) > invite.MaxCampaignTitle:
		return input, apperrors.New(40000, http.StatusBadRequest, "活动名称不能超过 "+strconv.Itoa(invite.MaxCampaignTitle)+" 字")
	case utf8.RuneCountInString(input.Note) > invite.MaxCampaignNote:
		return input, apperrors.New(40000, http.StatusBadRequest, "备注不能超过 "+strconv.Itoa(invite.MaxCampaignNote)+" 字")
	case input.Status != invite.CodeActive && input.Status != invite.CodeDisabled:
		return input, apperrors.New(40000, http.StatusBadRequest, "未知的状态："+input.Status)
	case input.MaxUses != nil && (*input.MaxUses < 0 || *input.MaxUses > invite.MaxCodeUses):
		return input, apperrors.New(40000, http.StatusBadRequest, "可使用次数超出范围")
	case input.StartsAt != nil && input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt):
		return input, apperrors.New(40000, http.StatusBadRequest, "结束时间需要晚于开始时间")
	}
	if input.MaxUses != nil && *input.MaxUses == 0 {
		input.MaxUses = nil
	}
	if input.Rewards != nil {
		rewards := invite.NormalizeRewards(*input.Rewards)
		if err := invite.ValidateRewards("活动码奖励", rewards); err != nil {
			return input, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		if err := s.checkRewardRefs(ctx, input.AppID, rewards); err != nil {
			return input, err
		}
		input.Rewards = &rewards
	}
	if input.OwnerUserID != nil {
		if *input.OwnerUserID <= 0 {
			input.OwnerUserID = nil
		} else if _, err := s.pg.GetInviteClientUser(ctx, input.AppID, *input.OwnerUserID); err != nil {
			if errors.Is(err, pgrepo.ErrUserNotFound) {
				return input, apperrors.New(40000, http.StatusBadRequest, "推广人不存在")
			}
			return input, err
		}
	}
	return input, nil
}

// ── 管理端：关系与统计 ──

// ListRelations 邀请关系。
func (s *InviteService) ListRelations(ctx context.Context, query invite.RelationQuery) (*invite.Page[invite.Relation], error) {
	return s.pg.ListInviteRelations(ctx, query)
}

// SettleRelation 立即发放一条等待中的邀请人奖励（不看发放条件与上限）。
func (s *InviteService) SettleRelation(ctx context.Context, appID, relationID int64, operator string) (*invite.Relation, error) {
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	result, err := s.pg.SettleInviteRelation(ctx, *settings, relationID, s.windows(), operator)
	if err != nil {
		return nil, s.translate(err)
	}
	s.afterSettle(*settings, result)
	return s.pg.GetInviteRelation(ctx, appID, relationID)
}

// CancelRelation 取消一条等待中的邀请人奖励。
func (s *InviteService) CancelRelation(ctx context.Context, appID, relationID int64, operator string) (*invite.Relation, error) {
	relation, err := s.pg.CancelInviteRelation(ctx, appID, relationID, operator)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.pg.GetInviteRelation(ctx, appID, relation.ID)
}

// Stats 概览与按天趋势（缺的日期补零）。
func (s *InviteService) Stats(ctx context.Context, appID int64, days int) (*invite.Stats, error) {
	if days <= 0 {
		days = inviteStatsDefaultDays
	}
	if days > inviteStatsMaxDays {
		days = inviteStatsMaxDays
	}
	loc := timeutil.DefaultLocation()
	windows := pointstore.WindowsAt(time.Now(), loc)
	since := windows.Day.AddDate(0, 0, -(days - 1))
	stats, err := s.pg.InviteStats(ctx, appID, since, windows.Day, loc.String())
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]invite.StatsDay, len(stats.Trend))
	for _, day := range stats.Trend {
		byDate[day.Date] = day
	}
	trend := make([]invite.StatsDay, 0, days)
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		day, ok := byDate[date]
		if !ok {
			day = invite.StatsDay{Date: date}
		}
		trend = append(trend, day)
	}
	stats.Trend = trend
	return stats, nil
}

// ── /config ──

// Capability /config 里的邀请能力。读不到配置时按关闭处理：注册页少一个框，好过整个 /config 报错。
func (s *InviteService) Capability(ctx context.Context, appID int64) invite.Capability {
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		s.log.Warn("invite: load settings for config failed", zap.Int64("appid", appID), zap.Error(err))
		return invite.Settings{Config: invite.DefaultConfig()}.Capability()
	}
	return settings.Capability()
}

// ── 用户端 ──

// Center 邀请页：我的邀请码、奖励规则、统计，以及我自己能不能填写邀请码。
//
// 没开启时不报错，回 enabled=false：客户端据此隐藏入口。
func (s *InviteService) Center(ctx context.Context, appID, userID int64) (*invite.ClientCenter, error) {
	now := time.Now()
	center := &invite.ClientCenter{
		InviteeRewards: []invite.ClientReward{}, InviterRewards: []invite.ClientReward{},
		Milestones: []invite.ClientMilestone{}, ServerTime: now.UTC(),
	}
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		center.Bind = invite.ClientBind{Reason: invite.ReasonDisabled, Message: invite.ReasonMessage(invite.ReasonDisabled)}
		return center, nil
	}
	cfg := settings.Config
	center.Enabled = true
	facts, err := s.pg.GetInviteUserFacts(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	center.InviteeRewards = invite.DescribeRewards(cfg.InviteeRewards, plans, features)
	center.InviterRewards = invite.DescribeRewards(cfg.InviterRewards, plans, features)
	center.InviterTrigger = cfg.InviterTrigger
	center.TriggerSignInDays = cfg.TriggerSignInDays
	center.DailyLimit, center.TotalLimit = cfg.InviterDailyLimit, cfg.InviterTotalLimit
	center.AllowCustomCode = cfg.AllowCustomCode
	center.RulesText = cfg.RulesText

	center.CanInvite, center.InviteMessage = invite.EvaluateInviter(cfg, facts.IsVip, facts.Level)
	if center.CanInvite {
		code, err := s.ensurePersonalCode(ctx, *settings, userID)
		if err != nil {
			return nil, err
		}
		center.Code, center.CodeCustom = code.Code, code.Custom
		if code.Status != invite.CodeActive {
			center.CanInvite, center.InviteMessage = false, "你的邀请码已被停用"
		} else {
			center.ShareLink = invite.ShareLink(cfg, code.Code)
			center.ShareText = invite.ShareText(cfg, code.Code, s.appName(ctx, appID))
		}
	}

	if center.Stats, err = s.pg.InviterStats(ctx, appID, userID, s.windows().Day); err != nil {
		return nil, err
	}
	grants, err := s.pg.InviteMilestoneGrants(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	granted := map[int]bool{}
	for _, grant := range grants {
		granted[grant.Milestone] = true
	}
	for _, milestone := range cfg.Milestones {
		center.Milestones = append(center.Milestones, invite.ClientMilestone{
			Count:   milestone.Count,
			Title:   milestone.Title,
			Rewards: invite.DescribeRewards(milestone.Rewards, plans, features),
			Reached: center.Stats.Rewarded >= int64(milestone.Count),
			Granted: granted[milestone.Count],
		})
	}

	relation, err := s.pg.InviteRelationOfInvitee(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	reason := invite.EvaluateEntry(*settings, invite.SourceBind, facts.RegisteredAt, relation != nil, now)
	center.Bind = invite.ClientBind{Available: reason == invite.ReasonOK}
	if reason != invite.ReasonOK {
		center.Bind.Reason, center.Bind.Message = reason, invite.ReasonMessage(reason)
	} else {
		center.Bind.Deadline = invite.BindDeadline(cfg, facts.RegisteredAt)
	}
	if relation != nil {
		bound := &invite.ClientBound{Code: relation.Code, Source: relation.Source, Rewards: relation.InviteeResults, BoundAt: relation.CreatedAt}
		if relation.InviterID != nil {
			bound.Inviter = &invite.ClientUser{
				UserID: *relation.InviterID, Account: relation.InviterAccount, RawAvatar: relation.InviterAvatar,
				Nickname: invite.DisplayName(relation.InviterNickname, relation.InviterAccount),
			}
		}
		center.Bind.Bound = bound
	}
	return center, nil
}

// MyInvitees 我邀请的人。
func (s *InviteService) MyInvitees(ctx context.Context, appID, userID int64, page, limit int) (*invite.Page[invite.ClientInvitee], error) {
	result, err := s.pg.ListInviteRelations(ctx, invite.RelationQuery{AppID: appID, InviterID: userID, Page: page, Limit: limit})
	if err != nil {
		return nil, err
	}
	items := make([]invite.ClientInvitee, 0, len(result.Items))
	for _, relation := range result.Items {
		items = append(items, invite.ClientInvitee{
			Nickname:  invite.DisplayName(relation.InviteeNickname, relation.InviteeAccount),
			Source:    relation.Source,
			Status:    relation.InviterStatus,
			Results:   relation.InviterResults,
			BoundAt:   relation.CreatedAt,
			SettledAt: relation.InviterGrantedAt,
			UserID:    relation.InviteeID,
			Account:   relation.InviteeAccount,
			RawAvatar: relation.InviteeAvatar,
		})
	}
	return &invite.Page[invite.ClientInvitee]{Items: items, Total: result.Total, Page: result.Page,
		Limit: result.Limit, TotalPages: result.TotalPages}, nil
}

// UpdateMyCode 换我的个人码：code 非空为自定义（需应用允许），为空则按格式重新生成。
// 换码之后旧码立即失效；已经填写过旧码的人不受影响。
func (s *InviteService) UpdateMyCode(ctx context.Context, appID, userID int64, custom string) (*invite.Code, error) {
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, apperrors.New(errCodeInviteDisabled, http.StatusForbidden, invite.ReasonMessage(invite.ReasonDisabled))
	}
	facts, err := s.pg.GetInviteUserFacts(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	if ok, message := invite.EvaluateInviter(settings.Config, facts.IsVip, facts.Level); !ok {
		return nil, apperrors.New(errCodeInviteNotEligible, http.StatusForbidden, message)
	}
	current, err := s.ensurePersonalCode(ctx, *settings, userID)
	if err != nil {
		return nil, err
	}
	if current.Status != invite.CodeActive {
		return nil, apperrors.New(errCodeInviteNotEligible, http.StatusForbidden, "你的邀请码已被停用")
	}

	custom = invite.NormalizeCode(custom)
	if custom != "" {
		if !settings.AllowCustomCode {
			return nil, apperrors.New(errCodeInviteCustomForbidden, http.StatusForbidden, "当前应用不支持自定义邀请码")
		}
		if err := invite.ValidateCustomCode(custom); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
		}
		if strings.EqualFold(custom, current.Code) {
			return current, nil
		}
		taken, err := s.pg.InviteCodeTaken(ctx, appID, custom)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, apperrors.New(errCodeInviteCodeTaken, http.StatusConflict, "邀请码已被占用")
		}
		updated, err := s.pg.UpdatePersonalInviteCode(ctx, appID, userID, custom, true)
		if pgrepo.IsUniqueViolation(err) {
			return nil, apperrors.New(errCodeInviteCodeTaken, http.StatusConflict, "邀请码已被占用")
		}
		return updated, s.translate(err)
	}

	for attempt := 0; attempt < inviteCodeAttempts; attempt++ {
		code, err := invite.GenerateCode(settings.Config)
		if err != nil {
			return nil, err
		}
		updated, err := s.pg.UpdatePersonalInviteCode(ctx, appID, userID, code, false)
		if err == nil {
			return updated, nil
		}
		if !pgrepo.IsUniqueViolation(err) {
			return nil, s.translate(err)
		}
	}
	return nil, apperrors.New(50000, http.StatusInternalServerError, "生成邀请码失败，请重试")
}

// ensurePersonalCode 取我的个人码，没有就按应用格式生成一个。
func (s *InviteService) ensurePersonalCode(ctx context.Context, settings invite.Settings, userID int64) (*invite.Code, error) {
	existing, err := s.pg.GetPersonalInviteCode(ctx, settings.AppID, userID)
	if err != nil || existing != nil {
		return existing, err
	}
	for attempt := 0; attempt < inviteCodeAttempts; attempt++ {
		code, err := invite.GenerateCode(settings.Config)
		if err != nil {
			return nil, err
		}
		created, err := s.pg.CreatePersonalInviteCode(ctx, settings.AppID, userID, code, false)
		switch {
		case err == nil:
			return created, nil
		case errors.Is(err, pgrepo.ErrInvitePersonalCodeExists):
			// 并发的另一次请求先生成了
			return s.pg.GetPersonalInviteCode(ctx, settings.AppID, userID)
		case pgrepo.IsUniqueViolation(err):
			continue
		default:
			return nil, err
		}
	}
	return nil, apperrors.New(50000, http.StatusInternalServerError, "生成邀请码失败，请重试")
}

// Preview 注册页填完邀请码后的预览：码能不能用、谁邀请的、填了能拿什么。不需要登录。
func (s *InviteService) Preview(ctx context.Context, appID int64, raw string) (*invite.ClientPreview, error) {
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	preview := &invite.ClientPreview{Rewards: []invite.ClientReward{}}
	if !settings.Enabled {
		preview.Reason, preview.Message = invite.ReasonDisabled, invite.ReasonMessage(invite.ReasonDisabled)
		return preview, nil
	}
	code, reason, err := s.resolveUsableCode(ctx, *settings, invite.NormalizeCode(raw))
	if err != nil {
		return nil, err
	}
	if reason != invite.ReasonOK {
		preview.Reason, preview.Message = reason, invite.ReasonMessage(reason)
		return preview, nil
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	preview.Valid = true
	preview.Code = code.Code
	preview.Rewards = invite.DescribeRewards(invite.InviteeRewardsFor(settings.Config, code), plans, features)
	if code.OwnerUserID != nil {
		if preview.Inviter, err = s.pg.GetInviteClientUser(ctx, appID, *code.OwnerUserID); err != nil {
			return nil, s.translate(err)
		}
	}
	return preview, nil
}

// CheckRegisterCode 注册前核对邀请码：必填却没填、填了却不能用，都在建号之前拒绝。
// 应用没开启邀请时忽略（字段本就不出现）。
func (s *InviteService) CheckRegisterCode(ctx context.Context, appID int64, raw string) error {
	settings, err := s.pg.GetInviteSettings(ctx, appID)
	if err != nil {
		return err
	}
	if !settings.Enabled {
		return nil
	}
	code := invite.NormalizeCode(raw)
	if code == "" {
		if settings.RegisterField == invite.FieldRequired {
			label := settings.FieldLabel
			if label == "" {
				label = "邀请码"
			}
			return apperrors.New(errCodeInviteRequired, http.StatusBadRequest, "请填写"+label)
		}
		return nil
	}
	_, reason, err := s.resolveUsableCode(ctx, *settings, code)
	if err != nil {
		return err
	}
	if reason != invite.ReasonOK {
		return reasonError(reason)
	}
	return nil
}

// resolveUsableCode 查码并判定码本身能不能用（含个人码主人的邀请资格），与填写的人无关。
func (s *InviteService) resolveUsableCode(ctx context.Context, settings invite.Settings, code string) (*invite.Code, string, error) {
	if !invite.ValidCodeShape(code) {
		return nil, invite.ReasonCodeNotFound, nil
	}
	found, err := s.pg.FindInviteCode(ctx, settings.AppID, code)
	if err != nil {
		return nil, "", err
	}
	if reason := invite.CodeUsable(found, time.Now()); reason != invite.ReasonOK {
		return nil, reason, nil
	}
	if found.Kind == invite.KindPersonal && found.OwnerUserID != nil {
		facts, err := s.pg.GetInviteUserFacts(ctx, settings.AppID, *found.OwnerUserID)
		if errors.Is(err, pgrepo.ErrUserNotFound) {
			return nil, invite.ReasonCodeNotFound, nil
		}
		if err != nil {
			return nil, "", err
		}
		if ok, _ := invite.EvaluateInviter(settings.Config, facts.IsVip, facts.Level); !ok {
			return nil, invite.ReasonInviterIneligible, nil
		}
	}
	return found, invite.ReasonOK, nil
}

// BindRequest 一次填写。
type InviteBindRequest struct {
	AppID     int64
	UserID    int64
	Code      string
	DeviceID  string
	ClientIP  string
	UserAgent string
}

// Bind 注册后补填邀请码。
func (s *InviteService) Bind(ctx context.Context, req InviteBindRequest) (*invite.BindResult, error) {
	return s.bind(ctx, req, invite.SourceBind)
}

// BindAfterRegister 注册成功后填写注册表单里的邀请码。失败不影响注册（码已在建号前核对过，
// 走到这里还失败只可能是并发用完了次数这类边角），结论随注册结果带回。
func (s *InviteService) BindAfterRegister(ctx context.Context, req InviteBindRequest) *invite.RegisterOutcome {
	code := invite.NormalizeCode(req.Code)
	if code == "" || req.UserID <= 0 {
		return nil
	}
	source := invite.SourceRegister
	if registeredAt, err := s.pg.UserRegisteredAt(ctx, req.AppID, req.UserID); err != nil || time.Since(registeredAt) > inviteRegisterGrace {
		source = invite.SourceBind
	}
	result, err := s.bind(ctx, req, source)
	if err != nil {
		outcome := &invite.RegisterOutcome{Code: code, Rewards: []invite.RewardResult{}}
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			outcome.Message = appErr.Message
		} else {
			outcome.Message = "邀请码填写失败，可在注册后到邀请页重新填写"
			s.log.Warn("invite: bind after register failed", zap.Int64("appid", req.AppID), zap.Int64("userId", req.UserID), zap.Error(err))
		}
		return outcome
	}
	return &invite.RegisterOutcome{Bound: true, Code: result.Relation.Code, Rewards: result.Rewards, Inviter: result.Inviter}
}

func (s *InviteService) bind(ctx context.Context, req InviteBindRequest, source string) (*invite.BindResult, error) {
	code := invite.NormalizeCode(req.Code)
	if code == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请填写邀请码")
	}
	if !invite.ValidCodeShape(code) {
		return nil, reasonError(invite.ReasonCodeNotFound)
	}
	settings, err := s.pg.GetInviteSettings(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, reasonError(invite.ReasonDisabled)
	}
	if s.risk != nil {
		appID, userID := req.AppID, req.UserID
		if _, err := s.risk.Guard(ctx, securitydomain.RiskEvalRequest{
			Scene: securitydomain.SceneInvite, AppID: &appID, UserID: &userID,
			IP: req.ClientIP, DeviceID: req.DeviceID, UserAgent: req.UserAgent,
			Extra: map[string]any{"inviteCode": code, "source": source},
		}, RiskGuardOption{Code: errCodeInviteLimited, Message: "当前环境存在风险，暂时无法填写邀请码"}); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	result, err := s.pg.BindInvite(ctx, invite.BindInput{
		AppID: req.AppID, UserID: req.UserID, Code: code, Source: source,
		DeviceID: strings.TrimSpace(req.DeviceID), ClientIP: strings.TrimSpace(req.ClientIP),
		Settings: *settings, Windows: s.windowsAt(now), Now: now,
	})
	if err != nil {
		return nil, s.translate(err)
	}
	if result.Relation.InviterID != nil {
		if inviter, err := s.pg.GetInviteClientUser(ctx, req.AppID, *result.Relation.InviterID); err == nil {
			result.Inviter = inviter
		}
	}
	s.publishUser(req.AppID, req.UserID, map[string]any{"relationId": result.Relation.ID})
	if s.risk != nil && result.Relation.InviterID != nil {
		// 邀请关系进关系图：邀请树规模、同设备邀请团都从这条边上算
		s.risk.Report(ctx, riskevents.Event{Type: riskevents.TypeInviteBind, Scene: securitydomain.SceneInvite, AppID: req.AppID, UserID: req.UserID,
			TargetUserID: *result.Relation.InviterID, IP: req.ClientIP, DeviceID: strings.TrimSpace(req.DeviceID), UA: req.UserAgent,
			Device: DeviceFingerprint(ctx), Props: map[string]any{"source": source}})
	}
	if result.InviterSettled {
		relation := result.Relation
		s.afterSettle(*settings, &invite.SettleResult{
			Relation: relation, Status: relation.InviterStatus, Results: relation.InviterResults, Milestones: result.Milestones,
		})
	}
	return result, nil
}

// OnSignIn 被邀请人签到之后：发放条件是「累计签到满 N 天」的，看这次是否达成。
// 签到链路不等它：在后台跑，失败只记日志（下一次签到会再试）。
func (s *InviteService) OnSignIn(appID, userID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		settings, err := s.pg.GetInviteSettings(ctx, appID)
		if err != nil || !settings.Configured {
			return
		}
		result, err := s.pg.SettleInvitePending(ctx, *settings, userID, s.windows())
		if err != nil {
			s.log.Warn("invite: settle after sign-in failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			return
		}
		s.afterSettle(*settings, result)
	}()
}

// afterSettle 邀请人奖励结算之后：通知邀请人、推送刷新。
func (s *InviteService) afterSettle(settings invite.Settings, result *invite.SettleResult) {
	if result == nil || result.Relation.InviterID == nil {
		return
	}
	appID, inviterID := result.Relation.AppID, *result.Relation.InviterID
	s.publishUser(appID, inviterID, map[string]any{"relationId": result.Relation.ID, "status": result.Status})
	if s.notifier == nil || !settings.NotifyInviter {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		name := "好友"
		if invitee, err := s.pg.GetInviteClientUser(ctx, appID, result.Relation.InviteeID); err == nil {
			name = invitee.Nickname
		}
		meta := map[string]any{"scene": "invite", "relationId": result.Relation.ID}
		if result.Status == invite.InviterGranted && len(result.Results) > 0 {
			content := name + " 通过你的邀请加入，奖励已到账：" + describeResults(result.Results) + "。"
			s.notifyUser(ctx, appID, inviterID, "邀请奖励已到账", content, "success", meta)
		}
		for _, milestone := range result.Milestones {
			content := "累计成功邀请 " + strconv.Itoa(milestone.Milestone) + " 人，奖励已到账：" + describeResults(milestone.Results) + "。"
			s.notifyUser(ctx, appID, inviterID, "阶梯奖励已到账", content, "success", meta)
		}
	}()
}

func (s *InviteService) notifyUser(ctx context.Context, appID, userID int64, title, content, level string, meta map[string]any) {
	if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: appID, UserID: userID, Type: "invite", Title: title, Content: content, Level: level, Metadata: meta,
		Action: notificationdomain.RouteAction(notificationdomain.RouteInvite, "查看邀请"),
	}); err != nil {
		s.log.Warn("invite: notify user failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func describeResults(results []invite.RewardResult) string {
	parts := make([]string, 0, len(results))
	for _, result := range results {
		parts = append(parts, result.Detail)
	}
	return strings.Join(parts, "、")
}

func (s *InviteService) publishUser(appID, userID int64, data map[string]any) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.userEvents.PublishUserEvent(ctx, appID, userID, "invite.changed", data); err != nil {
			s.log.Debug("publish invite event failed", zap.Error(err))
		}
	}()
}

func (s *InviteService) appName(ctx context.Context, appID int64) string {
	if s.apps == nil {
		return ""
	}
	return s.apps.AppName(ctx, appID)
}

func (s *InviteService) windows() pointstore.Windows { return s.windowsAt(time.Now()) }

func (s *InviteService) windowsAt(now time.Time) pointstore.Windows {
	return pointstore.WindowsAt(now, timeutil.DefaultLocation())
}

// reasonError 判据 → 带错误码的业务错误。
func reasonError(reason string) error {
	message := invite.ReasonMessage(reason)
	switch reason {
	case invite.ReasonDisabled:
		return apperrors.New(errCodeInviteDisabled, http.StatusForbidden, message)
	case invite.ReasonRequired:
		return apperrors.New(errCodeInviteRequired, http.StatusBadRequest, message)
	case invite.ReasonWindowClosed:
		return apperrors.New(errCodeInviteWindowClosed, http.StatusForbidden, message)
	case invite.ReasonAlreadyBound:
		return apperrors.New(errCodeInviteAlreadyBound, http.StatusConflict, message)
	case invite.ReasonSelfInvite, invite.ReasonCycle:
		return apperrors.New(errCodeInviteSelf, http.StatusForbidden, message)
	case invite.ReasonDeviceLimited, invite.ReasonIPLimited:
		return apperrors.New(errCodeInviteLimited, http.StatusForbidden, message)
	case invite.ReasonRewardUnavailable:
		return apperrors.New(errCodeInviteRewardUnavailable, http.StatusForbidden, message)
	default:
		// 码不存在、停用、未开始、已过期、次数用完、主人不满足邀请条件：对填写的人都是「这个码用不了」
		return apperrors.New(errCodeInviteCodeInvalid, http.StatusBadRequest, message)
	}
}

// translate 把仓储层的判定翻成带错误码的业务错误。
func (s *InviteService) translate(err error) error {
	if err == nil {
		return nil
	}
	var unavailable *pgrepo.InviteUnavailableError
	if errors.As(err, &unavailable) {
		return reasonError(unavailable.Reason)
	}
	switch {
	case errors.Is(err, pgrepo.ErrInviteCodeNotFound):
		return apperrors.New(errCodeInviteCodeNotFound, http.StatusNotFound, "邀请码不存在")
	case errors.Is(err, pgrepo.ErrInviteRelationNotFound):
		return apperrors.New(errCodeInviteRelationNotFound, http.StatusNotFound, "邀请记录不存在")
	case errors.Is(err, pgrepo.ErrInviteRelationState):
		return apperrors.New(errCodeInviteRelationHandled, http.StatusConflict, "这条邀请奖励不在等待中")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	case pgrepo.IsUniqueViolation(err):
		// 同一个人并发填写两次：唯一索引挡住了第二次
		return reasonError(invite.ReasonAlreadyBound)
	}
	return err
}
