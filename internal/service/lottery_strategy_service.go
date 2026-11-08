package service

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// 出奖策略：抽奖前取行为画像，管理端的策略模拟与单人预览。规则见 docs/lottery.md「出奖策略」。

// strategyNeedsProfile 策略里有没有用到行为画像（行为分或自定义规则）。没用到就不查那一串表。
func strategyNeedsProfile(strategy lotterydomain.Strategy) bool {
	return strategy.Behavior != nil || len(strategy.Rules) > 0
}

// drawProfile 抽奖前取这个人的行为画像。取不到时记日志、按空画像继续：画像只调概率，不该拦住抽奖。
// 人群特权写了条件表达式时也要取（表达式里能用画像的全部字段）。
func (s *LotteryService) drawProfile(ctx context.Context, activity lotterydomain.Activity, appID, userID int64,
	now time.Time) lotterydomain.BehaviorProfile {
	if !strategyNeedsProfile(activity.Strategy) && !lotterydomain.PerksNeedProfile(activity.Perks) {
		return lotterydomain.BehaviorProfile{}
	}
	profile, err := s.pg.LotteryBehaviorProfile(ctx, appID, userID, now, lotteryDayStart(now))
	if err != nil {
		s.log.Warn("lottery behavior profile failed", zap.Error(err), zap.Int64("appId", appID), zap.Int64("userId", userID))
		return lotterydomain.BehaviorProfile{}
	}
	return profile
}

// cachedDrawProfile 抽奖用的行为画像：同一个人 30 秒内连抽复用上一次的（活动页与预览照常取最新的）。
func (s *LotteryService) cachedDrawProfile(ctx context.Context, activity lotterydomain.Activity, appID, userID int64,
	now time.Time) lotterydomain.BehaviorProfile {
	if !strategyNeedsProfile(activity.Strategy) && !lotterydomain.PerksNeedProfile(activity.Perks) {
		return lotterydomain.BehaviorProfile{}
	}
	key := lotteryProfileKey{appID: appID, userID: userID}
	if profile, ok := s.profiles.Get(key); ok {
		return profile
	}
	profile, err := s.pg.LotteryBehaviorProfile(ctx, appID, userID, now, lotteryDayStart(now))
	if err != nil {
		s.log.Warn("lottery behavior profile failed", zap.Error(err), zap.Int64("appId", appID), zap.Int64("userId", userID))
		return lotterydomain.BehaviorProfile{}
	}
	s.profiles.Add(key, profile)
	return profile
}

// LotterySimulation 管理端的模拟请求。
type LotterySimulation struct {
	AppID      int64
	ActivityID int64
	// Input 控制台里还没保存的编辑内容；为空时用已保存的活动
	Input *lotterydomain.ActivityInput
	// UserID 用这个真实用户的画像、已拥有的奖品与本活动里的进度；为 0 时用 Profile
	UserID  int64
	Profile lotterydomain.BehaviorProfile
	// PerkFacts 不指定用户时虚拟用户的特权身份（会员、永久会员、免广告……）；为空时没有特权身份
	PerkFacts *lotterydomain.PerkFacts
	// Tiers 不指定用户时虚拟用户所在的会员档位（至尊加成按档位分级时用；PerkFacts.Lifetime 时按永久开通算）
	Tiers   []int64
	Options lotterydomain.SimulationInput
}

// LotterySimulationResult 模拟结果，以及（指定了用户时）这个人的画像与下一次抽奖的出奖方案。
type LotterySimulationResult struct {
	Result   lotterydomain.SimulationResult `json:"result"`
	Profile  lotterydomain.BehaviorProfile  `json:"profile"`
	Behavior *lotterydomain.BehaviorScore   `json:"behavior,omitempty"`
	// Perks 模拟用的这个人（或虚拟用户）生效的人群特权：保底提前、中奖加成已计入模拟
	Perks *lotterydomain.PerkEffects `json:"perks,omitempty"`
	// Current 指定用户此刻再抽一次的方案（只对已保存的活动给出）
	Current *lotterydomain.DrawPlan `json:"current,omitempty"`
	// Analytic 距首次中奖次数的精确分布（吸收马尔可夫链），与模拟结果互相印证
	Analytic lotterydomain.StreakAnalysis `json:"analytic"`
	// Supreme 活动实际生效的至尊加成与它的效果（没有生效的配置时没有）
	Supreme *LotterySupremeSimulation `json:"supreme,omitempty"`
}

// LotterySupremeSimulation 模拟里的至尊加成。
type LotterySupremeSimulation struct {
	Title string `json:"title"`
	// Source activity：活动单独设置；app：跟随应用设置
	Source string `json:"source"`
	// Applied 模拟的这个人（或虚拟用户）拿到了加成；Grades 他能得到的参数组
	Applied bool                         `json:"applied"`
	Grades  []lotterydomain.SupremeGrade `json:"grades"`
	// BaseFactor / Factor 指定真实用户时，他今天加成之前与之后的运气系数
	BaseFactor float64 `json:"baseFactor,omitempty"`
	Factor     float64 `json:"factor,omitempty"`
	// Baseline 同一批随机数、不加成时的模拟结果（与 result 一比就是加成的效果）
	Baseline *lotterydomain.SimulationResult `json:"baseline,omitempty"`
}

// Simulate 按策略模拟一批虚拟用户连续抽奖。不写库、不发奖。
func (s *LotteryService) Simulate(ctx context.Context, req LotterySimulation) (*LotterySimulationResult, error) {
	if err := lotterydomain.ValidateSimulation(req.Options); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	now := time.Now().In(timeutil.DefaultLocation())

	var saved *lotterydomain.AdminActivity
	if req.ActivityID > 0 {
		item, err := s.pg.GetLotteryAdminActivity(ctx, req.AppID, req.ActivityID, lotteryDayStart(now))
		if err != nil {
			return nil, s.translate(err)
		}
		saved = item
	}
	var activity lotterydomain.Activity
	var prizes []lotterydomain.Prize
	switch {
	case req.Input != nil:
		input, err := s.prepare(ctx, *req.Input)
		if err != nil {
			return nil, err
		}
		activity, prizes = draftActivity(input, saved, now)
	case saved != nil:
		activity, prizes = saved.Activity, saved.Prizes
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "需要指定活动或提供活动内容")
	}

	out := &LotterySimulationResult{Profile: req.Profile}
	base := lotterydomain.DrawState{Now: now, Source: req.Options.Source}
	appSupreme, err := s.pg.LotteryAppSupreme(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	supremeConfig, supremeSource := lotterydomain.EffectiveSupreme(activity.Strategy.Fortune, appSupreme)
	if supremeConfig != nil {
		out.Supreme = &LotterySupremeSimulation{Title: lotterydomain.SupremeTitle(supremeConfig), Source: supremeSource,
			Grades: []lotterydomain.SupremeGrade{}}
	}
	// baselinePrizes 指定真实用户且拿到至尊加成时，只乘自然运气的奖盘（对照组）
	var baselinePrizes []lotterydomain.Prize
	if req.UserID > 0 {
		profile, err := s.pg.LotteryBehaviorProfile(ctx, req.AppID, req.UserID, now, lotteryDayStart(now))
		if err != nil {
			return nil, s.translate(err)
		}
		out.Profile = profile
		// 这个人的个人奖盘：已拥有的奖品按活动的处理方式剔除
		coverage, err := s.pg.LotteryMemberCoverage(ctx, req.AppID, req.UserID)
		if err != nil {
			return nil, err
		}
		var plans map[int64]pointstore.PlanRef
		if plans, _, err = s.pg.PointsStoreCatalogRefs(ctx, req.AppID); err != nil {
			return nil, err
		}
		prizes = lotterydomain.PersonalPrizes(lotterydomain.RuleOf(activity, coverage.Policy), coverage, prizes, plans)
		// 这个人今天的运气（与抽奖事务、活动页同一个系数）
		controls, err := s.pg.LotteryFortuneControls(ctx, activity.ID, req.UserID, now)
		if err != nil {
			return nil, err
		}
		natural := lotterydomain.FortuneFactor(activity.Strategy.Fortune, activity.ID, req.UserID, lotteryDayStart(now), now, controls)
		factor := natural
		if out.Supreme != nil {
			out.Supreme.Grades = nonNilGrades(lotterydomain.SupremeGrades(supremeConfig, coverage.Holdings))
			if outcome := lotterydomain.ResolveSupreme(supremeConfig, coverage.Holdings, natural); outcome != nil {
				factor = outcome.Factor
				out.Supreme.Applied, out.Supreme.BaseFactor, out.Supreme.Factor = true, natural, factor
				baselinePrizes = lotterydomain.FortunePrizes(prizes, natural)
			}
		}
		prizes = lotterydomain.FortunePrizes(prizes, factor)
		req.Options.FortuneApplied = true
		if saved != nil {
			state, err := s.pg.LotteryPreview(ctx, activity, prizes, req.AppID, req.UserID, profile, now,
				lotteryDayStart(now), timeutil.DefaultLocation())
			if err != nil {
				return nil, err
			}
			plan := lotterydomain.PlanDraw(activity, activity.Strategy, prizes, state)
			out.Current = &plan
			base = state
		}
	} else if out.Supreme != nil && req.PerkFacts != nil {
		// 虚拟用户：按请求给的身份（永久会员、所在档位）取参数组，每人按自己抽到的自然运气取最好的一组
		holdings := vipdomain.Holdings{Lifetime: req.PerkFacts.Lifetime}
		for _, id := range req.Tiers {
			holdings.Tiers = append(holdings.Tiers, vipdomain.HeldTier{ID: id, Lifetime: req.PerkFacts.Lifetime})
		}
		out.Supreme.Grades = nonNilGrades(lotterydomain.SupremeGrades(supremeConfig, holdings))
		out.Supreme.Applied = len(out.Supreme.Grades) > 0
		req.Options.Supreme = out.Supreme.Grades
	}
	base.Profile = out.Profile
	// 人群特权：真实用户按他此刻的身份与行为；虚拟用户按请求给的身份。只有保底提前与中奖加成影响出奖
	if lotterydomain.PerksActive(activity.Perks) && (req.UserID > 0 || req.PerkFacts != nil) {
		var effects lotterydomain.PerkEffects
		if req.UserID > 0 {
			effects = s.perkEffects(ctx, activity, req.AppID, req.UserID, lotterydomain.UserFacts{
				TotalDraws: int64(base.TotalDraws)}, lotterydomain.VideoChances{}, now)
		} else {
			facts := *req.PerkFacts
			facts.Now, facts.Profile = now, out.Profile
			effects = lotterydomain.ResolvePerks(activity.Perks, facts)
		}
		if !effects.Empty() {
			activity = effects.Personalize(activity)
			prizes = effects.BoostPrizes(prizes)
			if baselinePrizes != nil {
				baselinePrizes = effects.BoostPrizes(baselinePrizes)
			}
			out.Perks = &effects
			if out.Current != nil {
				plan := lotterydomain.PlanDraw(activity, activity.Strategy, prizes, base)
				plan.Perks = &effects
				out.Current = &plan
			}
		}
	}
	out.Analytic = lotterydomain.AnalyzeStreak(activity, activity.Strategy, prizes, base)
	out.Behavior = lotterydomain.ScoreBehavior(activity.Strategy.Behavior, out.Profile)

	if out.Current != nil && out.Supreme != nil && out.Supreme.Applied && out.Supreme.Factor > 0 {
		grade, _ := lotterydomain.BestSupreme(out.Supreme.Grades, out.Supreme.BaseFactor)
		out.Current.Fortune, out.Current.FortuneBase, out.Current.Supreme = out.Supreme.Factor, out.Supreme.BaseFactor, grade
	}

	options := req.Options
	options.Profile = out.Profile
	options.Now = now
	if options.Seed == 0 {
		// 对照组要用同一批随机数
		options.Seed = rand.Uint64()
	}
	out.Result = lotterydomain.Simulate(activity, activity.Strategy, prizes, options)
	if out.Supreme != nil && out.Supreme.Applied {
		plain := options
		plain.Supreme = nil
		board := prizes
		if baselinePrizes != nil {
			board = baselinePrizes
		}
		baseline := lotterydomain.Simulate(activity, activity.Strategy, board, plain)
		out.Supreme.Baseline = &baseline
	}
	return out, nil
}

func nonNilGrades(grades []lotterydomain.SupremeGrade) []lotterydomain.SupremeGrade {
	if grades == nil {
		return []lotterydomain.SupremeGrade{}
	}
	return grades
}

// draftActivity 把还没保存的编辑内容变成可以模拟的活动与奖盘。
// 已有的奖品沿用已抽中数量（匀速释放、库存要按真实进度算）；新奖品用位置换一个负数 ID。
func draftActivity(input lotterydomain.ActivityInput, saved *lotterydomain.AdminActivity, now time.Time,
) (lotterydomain.Activity, []lotterydomain.Prize) {
	activity := lotterydomain.Activity{
		AppID: input.AppID, Name: input.Name, UIMode: input.UIMode, Status: input.Status,
		StartTime: input.StartTime, EndTime: input.EndTime, FreeDaily: input.FreeDaily, PointsCost: input.PointsCost,
		PointsDaily: input.PointsDaily, PityThreshold: input.PityThreshold, LifetimePolicy: input.LifetimePolicy,
		LifetimePointsPerDay: input.LifetimePointsPerDay, LifetimePointsLifetime: input.LifetimePointsLifetime,
		LifetimeDraws: input.LifetimeDraws, Strategy: input.Strategy, Perks: input.Perks, CreatedAt: now,
	}
	used := map[int64]int64{}
	if saved != nil {
		activity.ID, activity.CreatedAt = saved.ID, saved.CreatedAt
		for _, prize := range saved.Prizes {
			used[prize.ID] = prize.Used
		}
	}
	prizes := make([]lotterydomain.Prize, 0, len(input.Prizes))
	for _, item := range input.Prizes {
		id := item.ID
		if _, ok := used[id]; !ok || id <= 0 {
			id = -int64(item.Position + 1)
		}
		prize := lotterydomain.Prize{
			ID: id, ActivityID: activity.ID, Name: item.Name, Kind: item.Reward.Type, Reward: item.Reward,
			Quantity: item.Quantity, Used: used[id], Weight: item.Weight, Position: item.Position,
			Fallback: item.Fallback, DailyLimit: item.DailyLimit, UserLimit: item.UserLimit,
			UserDailyLimit: item.UserDailyLimit, LifetimeReward: item.LifetimeReward, Value: item.Value,
		}
		if prize.Quantity >= 0 && prize.Used > prize.Quantity {
			prize.Used = prize.Quantity
		}
		prizes = append(prizes, prize)
		if input.PityThreshold > 0 && item.Position == input.PityPosition {
			pityID := id
			activity.PityPrizeID = &pityID
		}
	}
	return activity, prizes
}

// MaxLotteryAuditDays 审计最多回看多少天。
const MaxLotteryAuditDays = 90

// Audit 出奖审计：最近 days 天的记录是否服从每一次方案给出的概率（见 lottery.Audit）。
func (s *LotteryService) Audit(ctx context.Context, appID, activityID int64, days int) (*lotterydomain.AuditResult, error) {
	if _, err := s.pg.GetLotteryActivity(ctx, appID, activityID); err != nil {
		return nil, s.translate(err)
	}
	days = min(max(days, 1), MaxLotteryAuditDays)
	since := lotteryDayStart(time.Now()).AddDate(0, 0, -(days - 1))
	draws, err := s.pg.LotteryAuditDraws(ctx, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	result := lotterydomain.Audit(draws)
	return &result, nil
}

// MaxLotteryRiskDays 风控总览最多回看多少天。
const MaxLotteryRiskDays = 90

// RiskOverview 一个活动最近 days 天的风控总览：命中与拒绝、按处置与信号、按天、设备 / IP / 网段聚集、最近被拒绝的请求。
func (s *LotteryService) RiskOverview(ctx context.Context, appID, activityID int64, days int) (*lotterydomain.RiskOverview, error) {
	activity, err := s.pg.GetLotteryActivity(ctx, appID, activityID)
	if err != nil {
		return nil, s.translate(err)
	}
	days = min(max(days, 1), MaxLotteryRiskDays)
	since := lotteryDayStart(time.Now()).AddDate(0, 0, -(days - 1))
	overview, err := s.pg.LotteryRiskOverview(ctx, appID, activityID, since, timeutil.DefaultLocation().String())
	if err != nil {
		return nil, err
	}
	overview.Days = days
	overview.Rules = activity.Strategy.Risk.EffectiveRules()
	if overview.Rules == nil {
		overview.Rules = []lotterydomain.RiskRule{}
	}
	return overview, nil
}
