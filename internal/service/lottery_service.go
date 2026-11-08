package service

import (
	riskevents "aegis/services/risk/events"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.uber.org/zap"

	cardkeydomain "aegis/internal/domain/cardkey"
	lotterydomain "aegis/internal/domain/lottery"
	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/pointstore"
	securitydomain "aegis/internal/domain/security"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/keyedgate"
	"aegis/pkg/timeutil"
)

// 抽奖的错误码。客户端按码分支，不匹配文案。
//
// 「今天的免费次数用完了」「积分抽奖今天抽满了」「一次都没有了」对用户是三件不同的事：
// 第一件可以换别的次数，第二件可以去看视频，第三件只能明天再来。
const (
	errCodeLotteryNotFound          = 40402
	errCodeLotteryDrawNotFound      = 40403
	errCodeLotteryNotStarted        = 40923
	errCodeLotteryEnded             = 40924
	errCodeLotteryDrawing           = 40925
	errCodeLotteryDrawHandled       = 40926
	errCodeLotteryHasDraws          = 40927
	errCodeLotterySourceUnavailable = 40928
	errCodeLotteryPrizeUnavailable  = 40929
	errCodeLotteryFreeExhausted     = 42913
	errCodeLotteryPointsLimit       = 42914
	errCodeLotteryNoChance          = 42915
	errCodeLotteryRiskBlocked       = 40348
	lotteryStatsDefaultDays         = 14
	lotteryStatsMaxDays             = 90
)

// LotteryService 抽奖：活动与奖品的维护、用户端的活动页与抽奖、人工发放与统计。
//
// 抽中什么只在服务端决定（仓储层的单事务 DrawLottery），客户端只说用哪一种次数抽一次。
// 次数判定只有一处（lottery.EvaluateChances）：活动页上的剩余次数与抽奖失败的错误码是同一套判据。
type LotteryService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	rewarded   *RewardedAdService
	risk       *RiskService
	notifier   notificationDeliverer
	userEvents UserEventPublisher
	// gate 同一个人的抽奖在本实例内排队（见 lotteryGateDepth）
	gate *keyedgate.Gate
	// profiles 抽奖用的行为画像短时缓存：画像是十几张表的历史聚合，变化以天计，连抽时不必每次重算
	profiles *expirable.LRU[lotteryProfileKey, lotterydomain.BehaviorProfile]
}

// 同一个人的抽奖：本实例内同时只跑一个，后面最多排 lotteryGateDepth 个、最多等 lotteryGateWait，
// 超出的立刻回「抽奖正在处理，请稍后重试」（40925）。排队的请求不占数据库连接：
// 2026-11-08 一个人并发打几十个十连抽，每个都在数据库里等他自己的用户行锁，把连接池占满拖垮了整站。
const (
	lotteryGateDepth = 4
	lotteryGateWait  = 5 * time.Second
	// lotteryProfileTTL / lotteryProfileCap 行为画像缓存多久、最多缓存多少人
	lotteryProfileTTL = 30 * time.Second
	lotteryProfileCap = 100_000
)

type lotteryProfileKey struct{ appID, userID int64 }

// NewLotteryService 创建抽奖服务。rewarded 为空时视频抽奖不可用（活动页 video.enabled=false）。
func NewLotteryService(log *zap.Logger, pg *pgrepo.Repository, rewarded *RewardedAdService) *LotteryService {
	if log == nil {
		log = zap.NewNop()
	}
	return &LotteryService{log: log, pg: pg, rewarded: rewarded,
		gate:     keyedgate.New(lotteryGateDepth, lotteryGateWait),
		profiles: expirable.NewLRU[lotteryProfileKey, lotterydomain.BehaviorProfile](lotteryProfileCap, nil, lotteryProfileTTL),
	}
}

// lotteryMaintenanceInterval 清理过期风控聚集键的周期；lotteryCompactInterval 合并全站增量的周期。
const (
	lotteryMaintenanceInterval = time.Hour
	lotteryCompactInterval     = 500 * time.Millisecond
)

// Start 启动时补齐计数（000149 之前的历史记录，只做一次，在开始接请求之前做完），之后每 500 ms 合并全站增量、
// 每小时清理过期的风控聚集键。
func (s *LotteryService) Start(ctx context.Context) {
	now := time.Now()
	started := time.Now()
	timezone := timeutil.DefaultTimezone()
	if timezone == "" || timezone == "Local" {
		timezone = timeutil.DefaultLocation().String()
	}
	if done, err := s.pg.BackfillLotteryCounters(ctx, timezone, lotteryDayStart(now)); err != nil {
		s.log.Error("lottery: counter backfill failed", zap.Error(err))
	} else if done {
		s.log.Info("lottery: counters backfilled from history", zap.Duration("took", time.Since(started)))
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("lottery maintenance panic", zap.Any("panic", r))
			}
		}()
		ticker := time.NewTicker(lotteryMaintenanceInterval)
		defer ticker.Stop()
		compact := time.NewTicker(lotteryCompactInterval)
		defer compact.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-compact.C:
				if _, err := s.pg.CompactLotteryActivityDeltas(ctx); err != nil && ctx.Err() == nil {
					s.log.Warn("lottery: compact deltas failed", zap.Error(err))
				}
			case <-ticker.C:
				if removed, err := s.pg.PurgeLotteryRiskKeys(ctx, time.Now()); err != nil {
					s.log.Warn("lottery: purge risk keys failed", zap.Error(err))
				} else if removed > 0 {
					s.log.Info("lottery: purged risk keys", zap.Int64("rows", removed))
				}
			}
		}
	}()
}

// Close 退出前把已提交的全站增量再合并一次（不合并也不丢：增量已随抽奖事务落库，下次启动接着合并）。
func (s *LotteryService) Close(ctx context.Context) {
	if _, err := s.pg.CompactLotteryActivityDeltas(ctx); err != nil {
		s.log.Warn("lottery: compact deltas failed", zap.Error(err))
	}
}

// enterDraw 排进这个人的抽奖队伍。返回的函数必须调用（释放）。
func (s *LotteryService) enterDraw(ctx context.Context, userID int64) (func(), error) {
	release, err := s.gate.Acquire(ctx, uint64(userID))
	if err != nil {
		if errors.Is(err, keyedgate.ErrBusy) {
			return nil, apperrors.New(errCodeLotteryDrawing, http.StatusConflict, "抽奖正在处理，请稍后重试")
		}
		return nil, err
	}
	return release, nil
}

// SetRiskService 接上风控：抽奖前按 lottery 场景评估，block / ban 时拒绝。
func (s *LotteryService) SetRiskService(risk *RiskService) { s.risk = risk }

// SetNotifier 接上站内信：抽中人工发放的奖品、奖品发出时通知用户。
func (s *LotteryService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 接上实时推送：抽中积分等奖品后通知这个用户的其它在线设备刷新。
func (s *LotteryService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

func lotteryDayStart(now time.Time) time.Time {
	return rewardedAdDayStart(now, timeutil.DefaultLocation())
}

// ── 管理端：目录 ──

// LotteryVideoScene 可选作视频抽奖的激励广告场景（发抽奖次数的那些）。
type LotteryVideoScene struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	DrawsPerView int64  `json:"drawsPerView"`
	DailyLimit   int    `json:"dailyLimit"`
}

// Catalog 控制台的活动表单由它驱动。
func (s *LotteryService) Catalog(ctx context.Context, appID int64) (map[string]any, error) {
	// 已拥有奖品折算积分的单价：活动不填时跟随应用的会员折算策略
	policy, err := s.pg.GetVipConversionPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	conversionRates := map[string]any{
		"perVipDay": policy.PerVipDay, "perFeatureDay": policy.PerFeatureDay,
		"perFeatureHour": policy.PerFeatureHour, "perLifetime": policy.PerLifetime,
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	planList := make([]pointstore.PlanRef, 0, len(plans))
	for _, plan := range plans {
		if !plan.Trial {
			planList = append(planList, plan)
		}
	}
	slices.SortFunc(planList, func(a, b pointstore.PlanRef) int { return int(a.ID - b.ID) })
	featureList := make([]pointstore.FeatureRef, 0, len(features))
	for _, feature := range features {
		if feature.IsActive {
			featureList = append(featureList, feature)
		}
	}
	slices.SortFunc(featureList, func(a, b pointstore.FeatureRef) int { return strings.Compare(a.Tag, b.Tag) })

	scenes := []LotteryVideoScene{}
	adsEnabled := false
	if cfg, err := s.pg.GetRewardedAdConfig(ctx, appID); err == nil && cfg != nil {
		adsEnabled = cfg.Enabled
		for _, scene := range cfg.Scenes {
			if draws := sceneLotteryDraws(scene.Rewards); draws > 0 {
				scenes = append(scenes, LotteryVideoScene{
					Key: scene.Key, Name: scene.Name, Enabled: scene.Enabled,
					DrawsPerView: draws, DailyLimit: scene.DailyLimit,
				})
			}
		}
	}
	return map[string]any{
		"kinds":          lotterydomain.Catalog(),
		"uiModes":        lotterydomain.UIModes(),
		"statuses":       lotterydomain.Statuses(),
		"sources":        lotterydomain.Sources(),
		"gridPrizeCount": lotterydomain.GridPrizeCount,
		"wheelMinPrizes": lotterydomain.WheelMinPrizes,
		"wheelMaxPrizes": lotterydomain.WheelMaxPrizes,
		"plans":          planList,
		"features":       featureList,
		"videoScenes":    scenes,
		"rewardedAds":    adsEnabled,
		// 抽中已拥有的奖品（永久会员抽中会员等）的处理方式与默认折算
		"lifetimePolicies": lotterydomain.LifetimePolicyCatalog(),
		"swapKinds":        lotterydomain.SwapKinds(),
		// 出奖策略：行为信号、规则变量与取值上界
		"signals":       lotterydomain.SignalCatalog(),
		"ruleVariables": lotterydomain.RuleVariables(),
		// 风控规则：信号与处置目录
		"riskSignals": lotterydomain.RiskSignals(),
		"riskActions": lotterydomain.RiskActions(),
		"strategyLimits": map[string]any{
			"rules": lotterydomain.MaxStrategyRules, "ruleFactor": lotterydomain.MaxRuleFactor,
			"softPityStep": lotterydomain.MaxSoftPityStep, "cooldownDraws": lotterydomain.MaxCooldownDraws,
			"pacingBoost": lotterydomain.MaxPacingBoost, "behaviorBoost": lotterydomain.MaxBehaviorBoost,
			"prizeLimit": lotterydomain.MaxPrizeLimit, "simulationUsers": lotterydomain.MaxSimulationUsers,
			"simulationEach": lotterydomain.MaxSimulationEach, "simulationDraws": lotterydomain.MaxSimulationDraws,
			"luckStrength": lotterydomain.MaxLuckStrength, "controlGain": lotterydomain.MaxControlGain,
			"fortuneSpread": lotterydomain.MaxFortuneSpread, "riskRules": lotterydomain.MaxRiskRules,
		},
		// 人群特权：人群、条件表达式的变量与取值上界
		"perkAudiences": lotterydomain.PerkAudiences(),
		"perkVariables": lotterydomain.PerkVariables(),
		"perkLimits": map[string]any{
			"perks": lotterydomain.MaxPerks, "name": lotterydomain.MaxPerkNameRunes,
			"extraFree": lotterydomain.MaxPerkExtraFree, "extraPoints": lotterydomain.MaxPerkExtraPoints,
			"pointsDiscount": lotterydomain.MaxPerkPointsDiscount, "adSkipSlots": lotterydomain.MaxPerkAdSkipSlots,
			"defaultAdSkipSlots": lotterydomain.DefaultAdSkipSlots, "onlineMinutes": lotterydomain.MaxPerkOnlineMinutes,
			"winBoost": lotterydomain.MaxPerkWinBoost, "pityCut": lotterydomain.MaxPityThreshold,
		},
		// 可能「已拥有」、能设置改发的奖品类型
		"ownableKinds": lotterydomain.OwnableKinds(),
		// 每天 / 永久积分默认 0：跟随应用的会员折算策略（conversionRates）
		"lifetimeDefaults": map[string]any{
			"policy":         lotterydomain.LifetimePoints,
			"pointsPerDay":   0,
			"pointsLifetime": 0,
			"draws":          lotterydomain.DefaultLifetimeDraws,
		},
		"conversionRates": conversionRates,
	}, nil
}

// sceneLotteryDraws 激励广告场景看一次发几次抽奖（没有这一项为 0）。
func sceneLotteryDraws(rewards []cardkeydomain.Reward) int64 {
	var total int64
	for _, reward := range rewards {
		if reward.Type == cardkeydomain.RewardLotteryDraws {
			total += reward.Amount
		}
	}
	return total
}

// ── 管理端：活动 ──

// ListActivities 管理端活动列表（含奖品与汇总）。
func (s *LotteryService) ListActivities(ctx context.Context, appID int64) ([]lotterydomain.AdminActivity, error) {
	items, err := s.pg.ListLotteryAdminActivities(ctx, appID, lotteryDayStart(time.Now()))
	if err != nil {
		return nil, err
	}
	for index := range items {
		s.annotateEconomy(ctx, appID, &items[index])
	}
	return items, nil
}

// GetActivity 管理端单个活动。
func (s *LotteryService) GetActivity(ctx context.Context, appID, activityID int64) (*lotterydomain.AdminActivity, error) {
	item, err := s.pg.GetLotteryAdminActivity(ctx, appID, activityID, lotteryDayStart(time.Now()))
	if err != nil {
		return nil, s.translate(err)
	}
	s.annotateEconomy(ctx, appID, item)
	return item, nil
}

// annotateEconomy 给管理端的活动算积分账：积分能换到次数时，奖盘本身会不会让人用积分稳赚。
// 永久会员按「已拥有奖盘里全部会员与功能奖品」算（最坏情况）。取不到事实时不标，记日志。
func (s *LotteryService) annotateEconomy(ctx context.Context, appID int64, item *lotterydomain.AdminActivity) {
	if item == nil {
		return
	}
	policy, err := s.pg.GetVipConversionPolicy(ctx, appID)
	if err != nil {
		s.log.Warn("lottery: conversion policy failed", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	storePrice, err := s.pg.LotteryStoreDrawPrice(ctx, appID)
	if err != nil {
		s.log.Warn("lottery: store draw price failed", zap.Int64("appid", appID), zap.Error(err))
		return
	}
	plans, _ := s.catalogRefs(ctx, appID, item.Prizes)
	owned := []string{}
	for _, prize := range item.Prizes {
		switch prize.Kind {
		case lotterydomain.KindVipPlan:
			owned = append(owned, plans[prize.Reward.RefID].Features...)
		case lotterydomain.KindFeature, lotterydomain.KindFeatureTime:
			owned = append(owned, prize.Reward.Tag)
		}
	}
	coverage := lotterydomain.MemberCoverage{
		Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: vipdomain.NormalizeFeatureTags(owned)},
		Policy:   policy,
	}
	app, err := s.pg.LotteryAppSupreme(ctx, appID)
	if err != nil {
		s.log.Warn("lottery: app supreme failed", zap.Int64("appid", appID), zap.Error(err))
	}
	supreme, _ := lotterydomain.EffectiveSupreme(item.Strategy.Fortune, app)
	economy := lotterydomain.AuditEconomy(item.Activity, item.Prizes, plans, coverage, storePrice, supreme)
	item.Economy = &economy
}

// CreateActivity 新建活动（连同奖品）。
func (s *LotteryService) CreateActivity(ctx context.Context, input lotterydomain.ActivityInput) (*lotterydomain.AdminActivity, error) {
	input, err := s.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	activity, err := s.pg.SaveLotteryActivity(ctx, 0, input)
	if err != nil {
		return nil, s.translate(err)
	}
	return s.GetActivity(ctx, input.AppID, activity.ID)
}

// UpdateActivity 整体更新活动与奖品。
func (s *LotteryService) UpdateActivity(ctx context.Context, activityID int64, input lotterydomain.ActivityInput) (*lotterydomain.AdminActivity, error) {
	input, err := s.prepare(ctx, input)
	if err != nil {
		return nil, err
	}
	if _, err := s.pg.SaveLotteryActivity(ctx, activityID, input); err != nil {
		return nil, s.translate(err)
	}
	return s.GetActivity(ctx, input.AppID, activityID)
}

// SetStatus 上线 / 暂停 / 结束。上线前按当前奖盘再校验一次（草稿可能是在规则收紧之前存的）。
func (s *LotteryService) SetStatus(ctx context.Context, appID, activityID int64, status string) (*lotterydomain.AdminActivity, error) {
	if !slices.Contains(lotterydomain.Statuses(), status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的活动状态："+status)
	}
	if status == lotterydomain.StatusActive {
		current, err := s.GetActivity(ctx, appID, activityID)
		if err != nil {
			return nil, err
		}
		input := activityInputOf(current)
		input.Status = status
		if _, err := s.prepare(ctx, input); err != nil {
			return nil, err
		}
	}
	if _, err := s.pg.SetLotteryActivityStatus(ctx, appID, activityID, status); err != nil {
		return nil, s.translate(err)
	}
	return s.GetActivity(ctx, appID, activityID)
}

// DeleteActivity 删除没有抽奖记录的活动。
func (s *LotteryService) DeleteActivity(ctx context.Context, appID, activityID int64) error {
	return s.translate(s.pg.DeleteLotteryActivity(ctx, appID, activityID))
}

// activityInputOf 把已存的活动翻回输入（上线前复核用）。
func activityInputOf(item *lotterydomain.AdminActivity) lotterydomain.ActivityInput {
	input := lotterydomain.ActivityInput{
		AppID: item.AppID, Name: item.Name, Description: item.Description, Rules: item.Rules, UIMode: item.UIMode,
		Status: item.Status, StartTime: item.StartTime, EndTime: item.EndTime, FreeDaily: item.FreeDaily,
		PointsCost: item.PointsCost, PointsDaily: item.PointsDaily, VideoScene: item.VideoScene,
		PityThreshold: item.PityThreshold, PityPosition: -1, SortOrder: item.SortOrder,
		LifetimePolicy: item.LifetimePolicy, LifetimePointsPerDay: item.LifetimePointsPerDay,
		LifetimePointsLifetime: item.LifetimePointsLifetime, LifetimeDraws: item.LifetimeDraws,
		Strategy: item.Strategy, Perks: item.Perks,
	}
	for _, prize := range item.Prizes {
		input.Prizes = append(input.Prizes, lotterydomain.PrizeInput{
			ID: prize.ID, Name: prize.Name, Reward: prize.Reward, ImageURL: prize.ImageURL, Quantity: prize.Quantity,
			Weight: prize.Weight, Position: prize.Position, Fallback: prize.Fallback, LifetimeReward: prize.LifetimeReward,
			DailyLimit: prize.DailyLimit, UserLimit: prize.UserLimit, UserDailyLimit: prize.UserDailyLimit,
			Value: prize.Value,
		})
		if item.PityPrizeID != nil && *item.PityPrizeID == prize.ID {
			input.PityPosition = prize.Position
		}
	}
	return input
}

// prepare 清洗、校验活动输入，补上奖品的默认名字，并核对引用的套餐、功能与激励广告场景。
//
// 引用在保存时就核对：等到抽中时才发现套餐被删了，那一次只能整笔回滚、报「奖品已失效」，
// 而用户的免费次数、积分或看过的广告在他看来已经花出去了。
func (s *LotteryService) prepare(ctx context.Context, input lotterydomain.ActivityInput) (lotterydomain.ActivityInput, error) {
	input = lotterydomain.NormalizeInput(input)
	if err := lotterydomain.ValidateInput(input); err != nil {
		return input, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, input.AppID)
	if err != nil {
		return input, err
	}
	for index, prize := range input.Prizes {
		switch prize.Reward.Type {
		case lotterydomain.KindVipPlan:
			plan, ok := plans[prize.Reward.RefID]
			if !ok {
				return input, apperrors.New(40000, http.StatusBadRequest, "所选会员套餐不存在")
			}
			if plan.Trial {
				return input, apperrors.New(40000, http.StatusBadRequest, "试用套餐不能作为奖品")
			}
		case lotterydomain.KindFeatureTime:
			if _, ok := features[prize.Reward.Tag]; !ok {
				return input, apperrors.New(40000, http.StatusBadRequest,
					"功能标识 "+prize.Reward.Tag+" 未登记，请先在会员功能目录里创建它")
			}
		case lotterydomain.KindFeature:
			feature, ok := features[prize.Reward.Tag]
			if !ok {
				return input, apperrors.New(40000, http.StatusBadRequest,
					"功能标识 "+prize.Reward.Tag+" 未登记，请先在会员功能目录里创建它")
			}
			if !feature.IsActive {
				return input, apperrors.New(40000, http.StatusBadRequest, "功能「"+feature.Name+"」已停用，不能作为奖品")
			}
		}
		if prize.Name == "" {
			input.Prizes[index].Name = lotterydomain.DefaultPrizeName(prize.Reward, plans, features)
		}
	}
	if f := input.Strategy.Fortune; f != nil {
		if err := s.checkSupremeTiers(ctx, input.AppID, f.Supreme); err != nil {
			return input, err
		}
	}
	if input.VideoScene != "" {
		cfg, err := s.pg.GetRewardedAdConfig(ctx, input.AppID)
		if err != nil {
			return input, err
		}
		scene, ok := cfg.FindScene(input.VideoScene)
		if !ok {
			return input, apperrors.New(40000, http.StatusBadRequest, "激励广告场景「"+input.VideoScene+"」不存在")
		}
		if sceneLotteryDraws(scene.Rewards) <= 0 {
			return input, apperrors.New(40000, http.StatusBadRequest,
				"激励广告场景「"+scene.Name+"」不发放抽奖次数，请先在激励广告配置里给它加上「抽奖次数」")
		}
	}
	return input, nil
}

// ── 管理端：记录与统计 ──

// ListDraws 抽奖记录。
func (s *LotteryService) ListDraws(ctx context.Context, query lotterydomain.DrawQuery) (*lotterydomain.Page[lotterydomain.Draw], error) {
	return s.pg.ListLotteryDraws(ctx, query)
}

// Fulfill 标记一条人工发放的记录为已发放，并通知用户。
func (s *LotteryService) Fulfill(ctx context.Context, input lotterydomain.HandleInput) (*lotterydomain.Draw, error) {
	if utf8.RuneCountInString(strings.TrimSpace(input.Note)) > lotterydomain.MaxNoteRunes {
		return nil, apperrors.New(40000, http.StatusBadRequest, "备注不能超过 "+strconv.Itoa(lotterydomain.MaxNoteRunes)+" 字")
	}
	draw, err := s.pg.FulfillLotteryDraw(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	content := "你在「" + draw.ActivityName + "」抽中的「" + draw.PrizeName + "」已发放。"
	if note := strings.TrimSpace(draw.Note); note != "" {
		content += "\n" + note
	}
	s.notify(draw, "奖品已发放", content, "success")
	return draw, nil
}

// Stats 一个活动的概览（缺的日期补零）。
func (s *LotteryService) Stats(ctx context.Context, appID, activityID int64, days int) (*lotterydomain.Stats, error) {
	if days <= 0 {
		days = lotteryStatsDefaultDays
	}
	if days > lotteryStatsMaxDays {
		days = lotteryStatsMaxDays
	}
	if _, err := s.pg.GetLotteryActivity(ctx, appID, activityID); err != nil {
		return nil, s.translate(err)
	}
	loc := timeutil.DefaultLocation()
	dayStart := lotteryDayStart(time.Now())
	since := dayStart.AddDate(0, 0, -(days - 1))
	stats, err := s.pg.LotteryStats(ctx, appID, activityID, since, dayStart, loc.String())
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]lotterydomain.StatsDay, len(stats.Trend))
	for _, day := range stats.Trend {
		byDate[day.Date] = day
	}
	trend := make([]lotterydomain.StatsDay, 0, days)
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		day, ok := byDate[date]
		if !ok {
			day = lotterydomain.StatsDay{Date: date}
		}
		trend = append(trend, day)
	}
	stats.Trend = trend
	stats.Days = days
	return stats, nil
}

// ── 用户端 ──

// List 进行中的活动（含尚未开始的；已结束的不列）。没有活动时返回空列表，不报错。
func (s *LotteryService) List(ctx context.Context, appID int64) (*lotterydomain.ClientList, error) {
	now := time.Now()
	out := &lotterydomain.ClientList{Activities: []lotterydomain.ClientActivitySummary{}, ServerTime: now.UTC()}
	activities, err := s.pg.ListLotteryActivities(ctx, appID, true)
	if err != nil {
		return nil, err
	}
	for _, activity := range activities {
		if reason := lotterydomain.Window(activity, now); reason == lotterydomain.ReasonEnded {
			continue
		}
		out.Activities = append(out.Activities, lotterydomain.ClientActivitySummary{
			ID: activity.ID, Name: activity.Name, Description: activity.Description, UIMode: activity.UIMode,
			StartTime: activity.StartTime, EndTime: activity.EndTime,
		})
	}
	return out, nil
}

// State 活动页：活动与奖品（带公示概率）、我的各种次数、保底进度。
func (s *LotteryService) State(ctx context.Context, appID, userID, activityID int64) (*lotterydomain.ClientState, error) {
	now := time.Now()
	activity, err := s.pg.GetLotteryActivity(ctx, appID, activityID)
	if err != nil {
		return nil, s.translate(err)
	}
	if reason := lotterydomain.Window(*activity, now); reason == lotterydomain.ReasonNotFound {
		return nil, s.translate(lotteryReasonError(reason))
	}
	prizes, err := s.pg.ListLotteryPrizes(ctx, activity.ID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.LotteryUserFacts(ctx, appID, activity.ID, userID, lotteryDayStart(now))
	if err != nil {
		return nil, s.translate(err)
	}
	coverage, err := s.pg.LotteryMemberCoverage(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	plans, features := s.catalogRefs(ctx, appID, prizes)
	rule := lotterydomain.RuleOf(*activity, coverage.Policy)
	personal := s.personalize(ctx, *activity, prizes, appID, userID, facts, now)
	fortune, natural, err := s.readFortune(ctx, *activity, userID, coverage, now)
	if err != nil {
		return nil, err
	}
	factor := 1.0
	if fortune != nil {
		factor = fortune.Factor
	}
	client, shares := s.clientActivity(ctx, *activity, prizes, coverage, personal.effects, plans, features, factor, natural)
	if fortune != nil {
		fortune.WinRate, fortune.BaseWinRate = shares.lucky, shares.plain
		if fortune.Supreme != nil {
			fortune.Supreme.BaseWinRate = shares.natural
		}
	}
	return &lotterydomain.ClientState{
		Activity:   client,
		Chances:    personal.chances,
		Pity:       personal.pity,
		Lifetime:   lotterydomain.MergePerkNotice(lotterydomain.NoticeOf(rule, coverage, prizes, plans), personal.summary, coverage.Lifetime),
		Perks:      personal.summary,
		Fortune:    fortune,
		Viewer:     lotterydomain.ClientViewer{Lifetime: coverage.Lifetime},
		ServerTime: now.UTC(),
	}, nil
}

// readFortune 这个人此刻的今日运气（计入管理员干预与至尊加成）；没开每日运气时为 nil。
// natural 是至尊加成之前的系数（没有加成时与 reading.Factor 相同）。
func (s *LotteryService) readFortune(ctx context.Context, activity lotterydomain.Activity, userID int64,
	coverage lotterydomain.MemberCoverage, now time.Time) (reading *lotterydomain.FortuneReading, natural float64, err error) {
	fortune := activity.Strategy.Fortune
	if fortune == nil || fortune.Spread <= 0 {
		return nil, 1, nil
	}
	controls, err := s.pg.LotteryFortuneControls(ctx, activity.ID, userID, now)
	if err != nil {
		return nil, 1, err
	}
	reading = lotterydomain.ReadFortuneControlled(fortune, activity.ID, userID, lotteryDayStart(now), now, controls)
	if reading == nil {
		return nil, 1, nil
	}
	natural = reading.Factor
	app, err := s.pg.LotteryAppSupreme(ctx, activity.AppID)
	if err != nil {
		return nil, 1, err
	}
	config, _ := lotterydomain.EffectiveSupreme(fortune, app)
	outcome := lotterydomain.ResolveSupreme(config, coverage.Holdings, natural)
	return lotterydomain.ApplySupremeReading(reading, fortune, config, outcome), natural, nil
}

// lotteryShares 抽中兜底奖以外奖品的概率（百分比）：乘今日运气之前、之后，与只乘自然运气（至尊加成之前）。
type lotteryShares struct{ plain, lucky, natural float64 }

// lotteryPersonal 这个人此刻的次数、保底与人群特权（活动页与抽奖结果共用）。
type lotteryPersonal struct {
	effects lotterydomain.PerkEffects
	chances lotterydomain.Chances
	pity    *lotterydomain.Pity
	summary *lotterydomain.PerkSummary
}

// personalize 按人群特权改写活动后再判次数与保底：与抽奖事务里同一套函数，「还剩 N 次」与能抽几次一致。
func (s *LotteryService) personalize(ctx context.Context, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	appID, userID int64, facts lotterydomain.UserFacts, now time.Time) lotteryPersonal {
	video := s.videoChances(ctx, activity, appID, userID)
	effects := s.perkEffects(ctx, activity, appID, userID, facts, video, now)
	personalActivity := effects.Personalize(activity)
	return lotteryPersonal{
		effects: effects,
		chances: lotterydomain.EvaluateChances(personalActivity, facts, effects.AdjustVideo(video)),
		pity:    lotterydomain.PityProgress(personalActivity, prizes, facts.MissStreak),
		summary: lotterydomain.SummaryOf(effects, activity, video),
	}
}

// perkEffects 这个人此刻生效的人群特权。事实取不到时按没有特权处理（记日志）：特权只会多给，不该拦住抽奖。
func (s *LotteryService) perkEffects(ctx context.Context, activity lotterydomain.Activity, appID, userID int64,
	facts lotterydomain.UserFacts, video lotterydomain.VideoChances, now time.Time) lotterydomain.PerkEffects {
	if !lotterydomain.PerksActive(activity.Perks) {
		return lotterydomain.PerkEffects{}
	}
	dayStart := lotteryDayStart(now)
	perkFacts, err := s.pg.LotteryPerkFacts(ctx, appID, userID, now.In(timeutil.DefaultLocation()), dayStart)
	if err != nil {
		s.log.Warn("lottery: perk facts failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		return lotterydomain.PerkEffects{}
	}
	if lotterydomain.PerksNeedProfile(activity.Perks) {
		if perkFacts.Profile, err = s.pg.LotteryBehaviorProfile(ctx, appID, userID, now, dayStart); err != nil {
			s.log.Warn("lottery: perk profile failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}
	perkFacts.TotalDraws = int(facts.TotalDraws)
	effects := lotterydomain.ResolvePerks(activity.Perks, perkFacts)
	return effects.WithAdSkip(activity, userID, dayStart, video, facts.FreeUsedToday)
}

// catalogRefs 奖盘里有会员套餐或功能奖品时取目录（判定已拥有、描述奖品用）；取不到时按空目录处理。
func (s *LotteryService) catalogRefs(ctx context.Context, appID int64, prizes []lotterydomain.Prize) (
	map[int64]pointstore.PlanRef, map[string]pointstore.FeatureRef) {
	for _, prize := range prizes {
		if prize.Kind == lotterydomain.KindVipPlan || prize.Kind == lotterydomain.KindFeature ||
			prize.Kind == lotterydomain.KindFeatureTime {
			plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
			if err != nil {
				s.log.Warn("lottery: load catalog refs failed", zap.Int64("appid", appID), zap.Error(err))
			}
			return plans, features
		}
	}
	return nil, nil
}

// clientActivity 用户端的活动：公示概率按这个人眼里的奖盘算（不参与抽取的已拥有奖品为 0、乘过今天的运气系数），
// 已拥有的奖品带上「对你来说会怎样」。另给出活动公示的原概率（baseProbability），以及今日运气前后的中奖率。
func (s *LotteryService) clientActivity(ctx context.Context, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	coverage lotterydomain.MemberCoverage, effects lotterydomain.PerkEffects, plans map[int64]pointstore.PlanRef,
	features map[string]pointstore.FeatureRef, fortune, natural float64) (lotterydomain.ClientActivity, lotteryShares) {
	rule := lotterydomain.RuleOf(activity, coverage.Policy)
	owned := lotterydomain.PersonalPrizes(rule, coverage, prizes, plans)
	boosted := effects.BoostPrizes(owned)
	personal := lotterydomain.FortunePrizes(boosted, fortune)
	probabilities := lotterydomain.Probabilities(personal)
	base := lotterydomain.Probabilities(owned)
	shares := lotteryShares{plain: lotterydomain.RealPrizeShare(boosted), lucky: lotterydomain.RealPrizeShare(personal)}
	shares.natural = shares.lucky
	if natural != fortune {
		shares.natural = lotterydomain.RealPrizeShare(lotterydomain.FortunePrizes(boosted, natural))
	}
	// 与抽奖事务同一个无套利比例：「转为 N 积分」写的是真会发的数
	storePrice, err := s.pg.LotteryStoreDrawPrice(ctx, activity.AppID)
	if err != nil {
		s.log.Warn("lottery: store draw price failed", zap.Int64("appid", activity.AppID), zap.Error(err))
	}
	rule = rule.WithEconomy(lotterydomain.EconomyOf(rule, coverage, personal, plans,
		lotterydomain.DrawPrice(effects.Personalize(activity), storePrice)))
	out := lotterydomain.ClientActivity{
		ID: activity.ID, Name: activity.Name, Description: activity.Description, Rules: activity.Rules,
		UIMode: activity.UIMode, StartTime: activity.StartTime, EndTime: activity.EndTime,
		Prizes: make([]lotterydomain.ClientPrize, 0, len(prizes)),
	}
	for index, prize := range prizes {
		out.Prizes = append(out.Prizes, lotterydomain.ClientPrize{
			ID: prize.ID, Name: prize.Name, Kind: lotterydomain.ClientKind(prize.Kind), RewardType: prize.Kind,
			Detail:   lotterydomain.DescribeReward(prize.Reward, plans, features).Detail,
			ImageURL: prize.ImageURL, Position: prize.Position, Probability: lotterydomain.PublicProbability(probabilities[index]),
			BaseProbability: lotterydomain.PublicProbability(base[index]),
			SoldOut:         !prize.InStock(),
			Swap:            lotterydomain.DescribeSwap(rule, coverage, prize, plans),
		})
	}
	return out, shares
}

// videoChances 视频抽奖：活动指定的激励广告场景今天还能看几次。广告没开、场景停用时 enabled=false。
func (s *LotteryService) videoChances(ctx context.Context, activity lotterydomain.Activity, appID, userID int64) lotterydomain.VideoChances {
	video := lotterydomain.VideoChances{Remaining: 0}
	if activity.VideoScene == "" || s.rewarded == nil {
		return video
	}
	status, err := s.rewarded.ClientStatus(ctx, appID, userID)
	if err != nil {
		s.log.Warn("lottery: rewarded ad status failed", zap.Int64("appid", appID), zap.Error(err))
		return video
	}
	if status == nil || !status.Enabled {
		return video
	}
	for _, scene := range status.Scenes {
		if scene.Key != activity.VideoScene {
			continue
		}
		draws := sceneLotteryDraws(scene.Rewards)
		if draws <= 0 {
			return video
		}
		return lotterydomain.VideoChances{
			Enabled: true, Scene: scene.Key, PlacementID: scene.PlacementID, UserID: status.UserID,
			DrawsPerView: draws, DailyLimit: scene.DailyLimit, Remaining: scene.Remaining,
			CooldownRemaining: scene.CooldownRemaining, Available: scene.Available, Watched: scene.TodayCount,
		}
	}
	return video
}

// LotteryDrawRequest 用户抽一次。
type LotteryDrawRequest struct {
	AppID      int64
	UserID     int64
	ActivityID int64
	Source     string
	RequestID  string
	DeviceID   string
	ClientIP   string
	UserAgent  string
}

// Draw 抽一次。结果（抽中什么、发了什么）全部由服务端在一个事务里决定并到账。
func (s *LotteryService) Draw(ctx context.Context, req LotteryDrawRequest) (*lotterydomain.DrawResult, error) {
	res, err := s.draw(ctx, req)
	if err == nil && s.risk != nil {
		s.risk.Report(ctx, riskevents.Event{Type: riskevents.TypeLotteryDraw, Scene: securitydomain.SceneLottery, AppID: req.AppID, UserID: req.UserID,
			IP: req.ClientIP, DeviceID: strings.TrimSpace(req.DeviceID), UA: req.UserAgent, Device: DeviceFingerprint(ctx),
			Props: map[string]any{"activityId": req.ActivityID, "source": req.Source}})
	}
	return res, err
}

func (s *LotteryService) draw(ctx context.Context, req LotteryDrawRequest) (*lotterydomain.DrawResult, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.Source = strings.TrimSpace(req.Source)
	if len(req.RequestID) > lotterydomain.MaxRequestIDLength {
		return nil, apperrors.New(40000, http.StatusBadRequest, "requestId 不能超过 64 位")
	}
	if req.Source != "" && !slices.Contains(lotterydomain.Sources(), req.Source) {
		return nil, s.translate(lotteryReasonError(lotterydomain.ReasonUnknownSource))
	}
	release, err := s.enterDraw(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := s.guardDraw(ctx, req); err != nil {
		return nil, err
	}
	input := s.drawInput(ctx, req)
	outcome, err := s.pg.DrawLottery(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}

	result := &lotterydomain.DrawResult{Draw: userLotteryDraw(outcome.Draw), Replayed: outcome.Replayed}
	result.Chances, result.Pity = s.chancesAfter(ctx, req, outcome)
	if !outcome.Replayed {
		s.afterDraw(outcome.Draw)
	}
	return result, nil
}

// LotteryBatchRequest 连抽（十连抽）：Count 次，每一次与单抽相同。
type LotteryBatchRequest struct {
	LotteryDrawRequest
	Count int
}

// DrawBatch 连抽。一个事务里逐次抽、逐次到账（每一次一个保存点，与单抽同一套逻辑），不是一次算好十个结果：
// 保底、每日运气、库存、奖品限额在十次之间照常推进。第 i 次的 requestId 是「requestId#i」，
// 网络重试带同一个 requestId 时已抽过的原样返回，没抽到的接着抽。
//
// 中途抽不了（次数用完、积分不够、活动刚好结束）时停下，已抽到的照常返回并写明停下的原因；
// 第一次就抽不了时报那个错误（与单抽同一个码）。风控只在开始时评估一次。
func (s *LotteryService) DrawBatch(ctx context.Context, req LotteryBatchRequest) (*lotterydomain.BatchResult, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	req.Source = strings.TrimSpace(req.Source)
	if req.Count < 1 || req.Count > lotterydomain.MaxBatchDraws {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多连抽 "+strconv.Itoa(lotterydomain.MaxBatchDraws)+" 次")
	}
	if len(req.RequestID) > lotterydomain.MaxBatchRequestIDLength {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			"requestId 不能超过 "+strconv.Itoa(lotterydomain.MaxBatchRequestIDLength)+" 位")
	}
	if req.Source != "" && !slices.Contains(lotterydomain.Sources(), req.Source) {
		return nil, s.translate(lotteryReasonError(lotterydomain.ReasonUnknownSource))
	}
	release, err := s.enterDraw(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	defer release()
	if err := s.guardDraw(ctx, req.LotteryDrawRequest); err != nil {
		return nil, err
	}

	input := s.drawInput(ctx, req.LotteryDrawRequest)
	requestIDs := make([]string, req.Count)
	if req.RequestID != "" {
		for index := range requestIDs {
			requestIDs[index] = req.RequestID + "#" + strconv.Itoa(index)
		}
	}
	// 一个事务连抽 Count 次（pgrepo.DrawLotteryBatch）：一次加锁、一次提交，保底与库存在这几次之间照常推进
	outcomes, stopErr, err := s.pg.DrawLotteryBatch(ctx, input, requestIDs)
	if err != nil {
		return nil, s.translate(err)
	}
	result := &lotterydomain.BatchResult{Draws: make([]lotterydomain.Draw, 0, req.Count), Requested: req.Count, Replayed: true}
	var last *pgrepo.LotteryDrawOutcome
	for index := range outcomes {
		outcome := &outcomes[index]
		last = outcome
		result.Draws = append(result.Draws, userLotteryDraw(outcome.Draw))
		result.Replayed = result.Replayed && outcome.Replayed
		if !outcome.Replayed {
			s.afterDraw(outcome.Draw)
		}
	}
	if stopErr != nil {
		translated := s.translate(stopErr)
		stop := &lotterydomain.BatchStop{Code: errCodeLotterySourceUnavailable, Message: translated.Error()}
		var appErr *apperrors.AppError
		if errors.As(translated, &appErr) {
			stop.Code = appErr.Code
		}
		result.Stopped = stop
	}
	result.Chances, result.Pity = s.chancesAfter(ctx, req.LotteryDrawRequest, last)
	return result, nil
}

// guardDraw 抽奖前的风控：block / ban 时拒绝。
func (s *LotteryService) guardDraw(ctx context.Context, req LotteryDrawRequest) error {
	if s.risk == nil {
		return nil
	}
	appID, userID := req.AppID, req.UserID
	_, err := s.risk.Guard(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.SceneLottery, AppID: &appID, UserID: &userID,
		IP: req.ClientIP, DeviceID: req.DeviceID, UserAgent: req.UserAgent,
		Extra: map[string]any{"activityId": req.ActivityID, "source": req.Source},
	}, RiskGuardOption{Code: errCodeLotteryRiskBlocked, Message: "当前环境存在风险，暂时无法抽奖"})
	return err
}

// drawInput 抽奖事务的输入。事务外先读一次活动：决定要不要取画像、要不要先问激励广告视频抽奖今天几次
// （免看广告的判定要用）。读不到时照常进事务，由事务报「活动不存在」。
func (s *LotteryService) drawInput(ctx context.Context, req LotteryDrawRequest) lotterydomain.DrawInput {
	now := time.Now()
	input := lotterydomain.DrawInput{
		AppID: req.AppID, UserID: req.UserID, ActivityID: req.ActivityID, Source: req.Source,
		RequestID: req.RequestID, DeviceID: strings.TrimSpace(req.DeviceID), ClientIP: strings.TrimSpace(req.ClientIP),
		UserAgent: req.UserAgent, DayStart: lotteryDayStart(now), Now: now, Location: timeutil.DefaultLocation(),
	}
	if activity, err := s.pg.GetLotteryActivity(ctx, req.AppID, req.ActivityID); err == nil {
		input.Profile = s.cachedDrawProfile(ctx, *activity, req.AppID, req.UserID, now)
		if lotterydomain.PerksAdSkip(activity.Perks) {
			input.Video = s.videoChances(ctx, *activity, req.AppID, req.UserID)
		}
	}
	return input
}

// chancesAfter 抽完之后的次数与保底（按人群特权改写过的活动判，与活动页一致）。取不到时为零值，记日志。
func (s *LotteryService) chancesAfter(ctx context.Context, req LotteryDrawRequest,
	outcome *pgrepo.LotteryDrawOutcome) (lotterydomain.Chances, *lotterydomain.Pity) {
	if outcome == nil {
		return lotterydomain.Chances{}, nil
	}
	facts, err := s.pg.LotteryUserFacts(ctx, req.AppID, outcome.Activity.ID, req.UserID, lotteryDayStart(time.Now()))
	if err != nil {
		s.log.Warn("lottery: reload chances failed", zap.Int64("appid", req.AppID), zap.Error(err))
		return lotterydomain.Chances{}, nil
	}
	prizes, err := s.pg.ListLotteryPrizes(ctx, outcome.Activity.ID)
	if err != nil {
		prizes = outcome.Prizes
	}
	personal := s.personalize(ctx, outcome.Activity, prizes, req.AppID, req.UserID, facts, time.Now())
	return personal.chances, personal.pity
}

// MyDraws 我的抽奖记录（activityID 为 0 时不限活动）。
func (s *LotteryService) MyDraws(ctx context.Context, appID, userID, activityID int64, page, limit int) (*lotterydomain.Page[lotterydomain.Draw], error) {
	result, err := s.pg.ListLotteryDraws(ctx, lotterydomain.DrawQuery{
		AppID: appID, UserID: userID, ActivityID: activityID, Page: page, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	for index := range result.Items {
		result.Items[index] = userLotteryDraw(result.Items[index])
	}
	return result, nil
}

// userLotteryDraw 用户端看到的记录：去掉只对排障有用的字段。
func userLotteryDraw(draw lotterydomain.Draw) lotterydomain.Draw {
	draw.Account, draw.Nickname = "", ""
	draw.DeviceID, draw.ClientIP, draw.HandledBy, draw.RequestID = "", "", "", ""
	// 出奖方案里有策略、画像与各奖品的实时系数，不给用户端
	draw.Trace = nil
	// 抽奖独有的奖品类型对老客户端按最接近的已知类型下发（图标、刷新会员缓存都照常），真实类型放 rewardType
	draw.RewardType = draw.PrizeKind
	draw.PrizeKind = lotterydomain.ClientKind(draw.PrizeKind)
	if draw.Results == nil {
		draw.Results = []lotterydomain.RewardResult{}
	}
	return draw
}

// afterDraw 抽完之后：积分有变动时推送刷新，人工发放的奖品发一条站内信。
func (s *LotteryService) afterDraw(draw lotterydomain.Draw) {
	data := map[string]any{"drawNo": draw.DrawNo, "activityId": draw.ActivityID, "prizeKind": draw.PrizeKind}
	grantedKind := draw.PrizeKind
	if draw.Conversion != nil {
		grantedKind = draw.Conversion.Reward.Type
		data["converted"] = true
	}
	if draw.PointsCost > 0 || grantedKind == lotterydomain.KindPoints || grantedKind == lotterydomain.KindExperience {
		s.publishUser(draw.AppID, draw.UserID, "points.changed", data)
	}
	if draw.PrizeKind == lotterydomain.KindManual {
		s.notify(&draw, "恭喜中奖", "你在「"+draw.ActivityName+"」抽中了「"+draw.PrizeName+"」，奖品将由工作人员尽快发放。", "success")
	}
}

// ── 内部 ──

func (s *LotteryService) notify(draw *lotterydomain.Draw, title, content, level string) {
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: draw.AppID, UserID: draw.UserID, Type: notificationdomain.TypeLottery,
			Title: title, Content: content, Level: level,
			Metadata: map[string]any{"drawNo": draw.DrawNo, "activityId": draw.ActivityID, "status": draw.Status},
			Action:   notificationdomain.RouteAction(notificationdomain.RouteLottery, "查看抽奖记录"),
		}); err != nil {
			s.log.Warn("lottery: notify user failed", zap.Int64("appid", draw.AppID),
				zap.Int64("userId", draw.UserID), zap.Error(err))
		}
	}()
}

func (s *LotteryService) publishUser(appID, userID int64, event string, data map[string]any) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.userEvents.PublishUserEvent(ctx, appID, userID, event, data); err != nil {
			s.log.Debug("publish lottery event failed", zap.Error(err))
		}
	}()
}

func lotteryReasonError(reason string) error { return &pgrepo.LotteryUnavailableError{Reason: reason} }

// translate 把仓储层的判定翻成带错误码的业务错误。
func (s *LotteryService) translate(err error) error {
	if err == nil {
		return nil
	}
	var unavailable *pgrepo.LotteryUnavailableError
	if errors.As(err, &unavailable) {
		message := lotterydomain.ReasonMessage(unavailable.Reason)
		switch unavailable.Reason {
		case lotterydomain.ReasonNotFound:
			return apperrors.New(errCodeLotteryNotFound, http.StatusNotFound, message)
		case lotterydomain.ReasonNotStarted:
			return apperrors.New(errCodeLotteryNotStarted, http.StatusConflict, message)
		case lotterydomain.ReasonEnded:
			return apperrors.New(errCodeLotteryEnded, http.StatusConflict, message)
		case lotterydomain.ReasonFreeExhausted:
			return apperrors.New(errCodeLotteryFreeExhausted, http.StatusTooManyRequests, message)
		case lotterydomain.ReasonPointsDailyLimit:
			return apperrors.New(errCodeLotteryPointsLimit, http.StatusTooManyRequests, message)
		case lotterydomain.ReasonBonusExhausted, lotterydomain.ReasonNoChance:
			return apperrors.New(errCodeLotteryNoChance, http.StatusTooManyRequests, message)
		case lotterydomain.ReasonPointsShort:
			return apperrors.New(errCodePointsInsufficient, http.StatusForbidden, message)
		default:
			return apperrors.New(errCodeLotterySourceUnavailable, http.StatusConflict, message)
		}
	}
	var denied *pgrepo.LotteryRiskDeniedError
	if errors.As(err, &denied) {
		return apperrors.New(errCodeLotteryRiskBlocked, http.StatusForbidden, "当前环境存在风险，暂时无法抽奖")
	}
	var stock *pgrepo.LotteryStockError
	if errors.As(err, &stock) {
		return apperrors.New(40000, http.StatusBadRequest, "位置 "+strconv.Itoa(stock.Position)+
			" 的奖品已抽中 "+strconv.FormatInt(stock.Used, 10)+" 份，库存不能少于这个数")
	}
	switch {
	case errors.Is(err, pgrepo.ErrLotteryActivityNotFound):
		return apperrors.New(errCodeLotteryNotFound, http.StatusNotFound, lotterydomain.ReasonMessage(lotterydomain.ReasonNotFound))
	case errors.Is(err, pgrepo.ErrLotteryDrawNotFound):
		return apperrors.New(errCodeLotteryDrawNotFound, http.StatusNotFound, "抽奖记录不存在")
	case errors.Is(err, pgrepo.ErrLotteryDrawState):
		return apperrors.New(errCodeLotteryDrawHandled, http.StatusConflict, "这条记录已经处理过了")
	case errors.Is(err, pgrepo.ErrLotteryActivityHasDraws):
		return apperrors.New(errCodeLotteryHasDraws, http.StatusConflict, "活动已有抽奖记录，不能删除，请改为结束")
	case errors.Is(err, pgrepo.ErrLotteryPoolBroken), errors.Is(err, pgrepo.ErrLotteryRewardUnavailable):
		s.log.Error("lottery: prize pool unusable", zap.Error(err))
		return apperrors.New(errCodeLotteryPrizeUnavailable, http.StatusConflict, "奖品配置已失效，请稍后再试")
	case errors.Is(err, pgrepo.ErrInsufficientIntegral):
		return apperrors.New(errCodePointsInsufficient, http.StatusForbidden, lotterydomain.ReasonMessage(lotterydomain.ReasonPointsShort))
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	case pgrepo.IsLockContention(err):
		// 别的实例或别的功能正占着这个人的行（等锁超过 3 秒）：事务已回滚，次数与积分都没动
		s.log.Warn("lottery draw lock contention", zap.Error(err))
		return apperrors.New(errCodeLotteryDrawing, http.StatusConflict, "抽奖正在处理，请稍后重试")
	case pgrepo.IsUniqueViolation(err):
		// 同一个 requestId 的并发提交撞上唯一索引：没有重复抽，让客户端用同一个 requestId 重查
		s.log.Warn("lottery draw hit unique constraint", zap.Error(err))
		return apperrors.New(errCodeLotteryDrawing, http.StatusConflict, "抽奖正在处理，请稍后重试")
	}
	return err
}
