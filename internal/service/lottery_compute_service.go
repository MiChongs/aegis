package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"aegis/internal/cluster"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

// 抽奖计算引擎的服务层：精确分析、奖盘求解、大规模推演。规则见 docs/lottery.md「计算引擎」。
//
// 三件事都要先回答「对谁算」。同一个活动在不同的人眼里不是同一个奖盘：永久会员的已拥有奖品被剔除或折算、
// 特权人群有中奖加成与提前的保底、拿到至尊加成的人运气系数更高。这里把「一类人」叫一种人群（persona），
// 用与线上同一套函数（PersonalPrizes / ResolvePerks / BoostPrizes / SupremeGrades）算出他们眼里的活动与奖盘，
// 再交给领域层的三个引擎。只读不写：不发奖、不扣次数，推演任务只写它自己的那张表。

const (
	errCodeLotteryComputeNotFound = 40417
	errCodeLotteryComputeActive   = 40939
	errCodeLotteryComputeBusy     = 42922
)

// 推演任务的执行节奏。
const (
	// lotteryComputePoll 没人叫醒时多久看一次有没有排队的任务
	lotteryComputePoll = 2 * time.Second
	// lotteryComputeHeartbeat 执行期间续心跳、写进度的周期（取消也是靠它发现的）
	lotteryComputeHeartbeat = 2 * time.Second
	// lotteryComputeStaleAfter 心跳超过这么久没更新，视为执行它的实例已经没了
	lotteryComputeStaleAfter = 30 * time.Second
	// lotteryComputeUserBudget 同时在内存里的虚拟用户总数上限（每人连同中奖计数约 300–400 字节，
	// 上限处约 200 MB）：人数多的场景少开几个工作协程，不让一次推演挤占线上请求的内存
	lotteryComputeUserBudget = 600_000
)

// LotteryPersonaSpec 一种人群的描述：身份由几个开关给出，与策略模拟里虚拟用户的特权身份同一套口径。
type LotteryPersonaSpec struct {
	Key   string
	Name  string
	Share float64
	// DrawsPerDay 这类人每天抽几次（只对推演有用）；0 为跟随场景
	DrawsPerDay        int
	Member             bool
	Lifetime           bool
	AdFree             bool
	SignedToday        bool
	OnlineMinutesToday float64
	// Tiers 所在的会员档位（至尊加成按档位分级时用）
	Tiers []int64
}

// 运气取哪一端（分析与求解用）。
const (
	LotteryFortuneAverage = "average"
	LotteryFortuneHigh    = "high"
	LotteryFortuneLow     = "low"
)

// lotteryBoard 一次计算里共用的事实：活动、基础奖盘，以及个人化要用的目录、折算策略、商城单价与至尊加成。
type lotteryBoard struct {
	appID      int64
	activity   lotterydomain.Activity
	prizes     []lotterydomain.Prize
	saved      *lotterydomain.AdminActivity
	plans      map[int64]pointstore.PlanRef
	policy     vipdomain.ConversionPolicy
	storePrice int64
	supreme    *lotterydomain.Supreme
	now        time.Time
}

// loadBoard 取活动与基础奖盘：已保存的活动，或控制台里还没保存的编辑内容（input）。
func (s *LotteryService) loadBoard(ctx context.Context, appID, activityID int64, input *lotterydomain.ActivityInput) (*lotteryBoard, error) {
	now := time.Now().In(timeutil.DefaultLocation())
	board := &lotteryBoard{appID: appID, now: now}
	if activityID > 0 {
		item, err := s.pg.GetLotteryAdminActivity(ctx, appID, activityID, lotteryDayStart(now))
		if err != nil {
			return nil, s.translate(err)
		}
		board.saved = item
	}
	switch {
	case input != nil:
		prepared, err := s.prepare(ctx, *input)
		if err != nil {
			return nil, err
		}
		board.activity, board.prizes = draftActivity(prepared, board.saved, now)
	case board.saved != nil:
		board.activity, board.prizes = board.saved.Activity, board.saved.Prizes
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "需要指定活动或提供活动内容")
	}
	var err error
	if board.policy, err = s.pg.GetVipConversionPolicy(ctx, appID); err != nil {
		return nil, err
	}
	if board.storePrice, err = s.pg.LotteryStoreDrawPrice(ctx, appID); err != nil {
		return nil, err
	}
	if board.plans, _, err = s.pg.PointsStoreCatalogRefs(ctx, appID); err != nil {
		return nil, err
	}
	app, err := s.pg.LotteryAppSupreme(ctx, appID)
	if err != nil {
		return nil, err
	}
	board.supreme, _ = lotterydomain.EffectiveSupreme(board.activity.Strategy.Fortune, app)
	return board, nil
}

// state 分析与求解用的基准状态：此刻的时间与当天进度，这个人还没抽过、今天全站还没出过限量奖品。
func (b *lotteryBoard) state() lotterydomain.DrawState {
	dayStart := lotteryDayStart(b.now)
	return lotterydomain.DrawState{
		Now: b.now, Source: lotterydomain.SourceFree, DeviceAccounts: 1, IPAccounts: 1, SubnetAccounts: 1, DeviceHistory: 1,
		DayProgress: min(1, max(0, b.now.Sub(dayStart).Hours()/24)),
	}
}

// coverage 这类人已经拥有什么。永久会员按「已拥有奖盘里全部会员与功能奖品」算（与积分账同一个最坏情况）。
func (b *lotteryBoard) coverage(spec LotteryPersonaSpec) lotterydomain.MemberCoverage {
	coverage := lotterydomain.MemberCoverage{Policy: b.policy}
	if spec.Lifetime {
		owned := []string{}
		for _, prize := range b.prizes {
			switch prize.Kind {
			case lotterydomain.KindVipPlan:
				owned = append(owned, b.plans[prize.Reward.RefID].Features...)
			case lotterydomain.KindFeature, lotterydomain.KindFeatureTime:
				owned = append(owned, prize.Reward.Tag)
			}
		}
		coverage.Holdings = vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true,
			LifetimeFeatures: vipdomain.NormalizeFeatureTags(owned)}
	}
	for _, id := range spec.Tiers {
		coverage.Tiers = append(coverage.Tiers, vipdomain.HeldTier{ID: id, Lifetime: spec.Lifetime})
	}
	return coverage
}

// lotteryPersonaView 一种人群眼里的活动：个人化之后的活动与奖盘（乘运气之前），以及生效的特权与至尊加成。
type lotteryPersonaView struct {
	spec     LotteryPersonaSpec
	coverage lotterydomain.MemberCoverage
	rule     lotterydomain.SwapRule
	activity lotterydomain.Activity
	effects  lotterydomain.PerkEffects
	grades   []lotterydomain.SupremeGrade
}

// view 这类人的个人化规则（不依赖具体的权重，求解时每一轮的候选奖盘都按它再个人化一遍）。
func (b *lotteryBoard) view(spec LotteryPersonaSpec) lotteryPersonaView {
	view := lotteryPersonaView{spec: spec, coverage: b.coverage(spec), activity: b.activity}
	view.rule = lotterydomain.RuleOf(b.activity, b.policy)
	if lotterydomain.PerksActive(b.activity.Perks) {
		view.effects = lotterydomain.ResolvePerks(b.activity.Perks, lotterydomain.PerkFacts{
			Member: spec.Member || spec.Lifetime, Lifetime: spec.Lifetime, AdFree: spec.AdFree,
			SignedToday: spec.SignedToday, OnlineMinutesToday: spec.OnlineMinutesToday, Now: b.now,
		})
		if !view.effects.Empty() {
			view.activity = view.effects.Personalize(b.activity)
		}
	}
	if b.supreme != nil {
		view.grades = lotterydomain.SupremeGrades(b.supreme, view.coverage.Holdings)
	}
	return view
}

// personal 把一张基础奖盘变成这类人的个人奖盘（已拥有剔除 → 中奖加成；运气另乘）。
func (b *lotteryBoard) personal(view lotteryPersonaView, prizes []lotterydomain.Prize) []lotterydomain.Prize {
	board := lotterydomain.PersonalPrizes(view.rule, view.coverage, prizes, b.plans)
	if !view.effects.Empty() {
		board = view.effects.BoostPrizes(board)
	}
	return board
}

// factor 这类人的运气系数：平均（自然运气 1）、最走运或最倒霉的那天，计入至尊加成。没开每日运气时为 1。
func (b *lotteryBoard) factor(view lotteryPersonaView, mode string) float64 {
	fortune := b.activity.Strategy.Fortune
	if fortune == nil || fortune.Spread <= 0 {
		return 1
	}
	natural := 1.0
	low, high := lotterydomain.FortuneRange(fortune)
	switch mode {
	case LotteryFortuneHigh:
		natural = high
	case LotteryFortuneLow:
		natural = low
	}
	if len(view.grades) > 0 {
		_, natural = lotterydomain.BestSupreme(view.grades, natural)
	}
	return natural
}

// payout 这类人抽中每一格时实际到账的积分与抽奖次数：已拥有的格子按活动的处理方式改发
// （无套利缩减按 factor 那天的个人奖盘算，与抽奖事务同一个公式）。
func (b *lotteryBoard) payout(view lotteryPersonaView, board []lotterydomain.Prize, factor float64) (
	points, redraws []float64, economy lotterydomain.Economy) {
	price := lotterydomain.DrawPrice(view.activity, b.storePrice)
	economy = lotterydomain.EconomyOf(view.rule, view.coverage, lotterydomain.FortunePrizes(board, factor), b.plans, price)
	rule := view.rule.WithEconomy(economy)
	points, redraws = make([]float64, len(board)), make([]float64, len(board))
	for index, prize := range board {
		reward := lotterydomain.NormalizeReward(prize.Reward)
		if _, owned := view.coverage.Owned(reward, b.plans); owned {
			if swapped, ok := lotterydomain.Swap(rule, prize, b.plans); ok {
				reward = swapped
			}
		}
		switch reward.Type {
		case lotterydomain.KindPoints:
			points[index] = float64(reward.Amount)
		case lotterydomain.KindLotteryDraws:
			redraws[index] = float64(reward.Amount)
		}
	}
	return points, redraws, economy
}

// normalizePersonas 清洗人群：没给时只有普通用户；补键与名字，去掉占比不为正的。
func normalizePersonas(specs []LotteryPersonaSpec) ([]LotteryPersonaSpec, error) {
	if len(specs) == 0 {
		return []LotteryPersonaSpec{{Key: "user", Name: "普通用户", Share: 1}}, nil
	}
	if len(specs) > lotterydomain.MaxComputePersonas {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			fmt.Sprintf("人群最多 %d 种", lotterydomain.MaxComputePersonas))
	}
	out := make([]LotteryPersonaSpec, 0, len(specs))
	seen := map[string]bool{}
	for index, spec := range specs {
		spec.Name = strings.TrimSpace(spec.Name)
		spec.Key = strings.TrimSpace(spec.Key)
		if spec.Name == "" {
			switch {
			case spec.Lifetime:
				spec.Name = "永久会员"
			case spec.Member:
				spec.Name = "会员"
			case spec.AdFree:
				spec.Name = "免广告用户"
			default:
				spec.Name = "普通用户"
			}
		}
		if utf8.RuneCountInString(spec.Name) > 16 {
			return nil, apperrors.New(40000, http.StatusBadRequest, "人群名称不能超过 16 字")
		}
		if spec.Key == "" || seen[spec.Key] {
			spec.Key = fmt.Sprintf("persona%d", index+1)
		}
		seen[spec.Key] = true
		if spec.Share < 0 {
			return nil, apperrors.New(40000, http.StatusBadRequest, "人群「"+spec.Name+"」的占比不能为负")
		}
		out = append(out, spec)
	}
	return out, nil
}

// ── 精确分析 ──

// LotteryAnalysisRequest 精确分析的请求。
type LotteryAnalysisRequest struct {
	AppID      int64
	ActivityID int64
	// Input 控制台里还没保存的编辑内容；为空时用已保存的活动
	Input    *lotterydomain.ActivityInput
	Personas []LotteryPersonaSpec
}

// LotteryAnalysisPersona 一种人群的精确分析。
type LotteryAnalysisPersona struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Factor 这份分析用的运气系数：自然运气 1，拿到至尊加成时是加成之后的；没开每日运气时为 1
	Factor float64 `json:"factor"`
	// Perks 生效的人群特权；Supreme 能得到的至尊加成参数组
	Perks   *lotterydomain.PerkEffects   `json:"perks,omitempty"`
	Supreme []lotterydomain.SupremeGrade `json:"supreme"`
	// Price 这类人用积分换一次抽奖的最低价（0 为换不到）；Return 每抽期望到账的积分回报 P / (1 − Q)，
	// 含已拥有奖品的折算（按公示口径、无套利缩减之后）；Scale 已拥有奖品按原价的几成折算
	Price  int64   `json:"price"`
	Return float64 `json:"return"`
	Scale  float64 `json:"scale"`
	// Payout / Redraws 这类人抽中每一格实际到账的积分与抽奖次数（与 analysis.prizes 同序）
	Payout   []float64                   `json:"payout"`
	Redraws  []float64                   `json:"redraws"`
	Analysis lotterydomain.ExactAnalysis `json:"analysis"`
}

// LotteryAnalysisResult 精确分析的结果。
type LotteryAnalysisResult struct {
	Personas []LotteryAnalysisPersona `json:"personas"`
	// Economy 这张奖盘的积分账（与活动列表上的同一份）
	Economy *lotterydomain.ActivityEconomy `json:"economy,omitempty"`
}

// Analyze 精确分析：每种人群的综合概率、每抽价值、各奖品多少抽能出。不抽样、不写库。
func (s *LotteryService) Analyze(ctx context.Context, req LotteryAnalysisRequest) (*LotteryAnalysisResult, error) {
	specs, err := normalizePersonas(req.Personas)
	if err != nil {
		return nil, err
	}
	board, err := s.loadBoard(ctx, req.AppID, req.ActivityID, req.Input)
	if err != nil {
		return nil, err
	}
	out := &LotteryAnalysisResult{Personas: make([]LotteryAnalysisPersona, 0, len(specs))}
	state := board.state()
	for _, spec := range specs {
		view := board.view(spec)
		personal := board.personal(view, board.prizes)
		factor := board.factor(view, LotteryFortuneAverage)
		analysis := lotterydomain.AnalyzeExact(view.activity, view.activity.Strategy,
			lotterydomain.FortunePrizes(personal, factor), state)
		analysis.Fortune = lotterydomain.AnalyzeExactFortune(view.activity, view.activity.Strategy, personal, state, view.grades)
		points, redraws, economy := board.payout(view, personal, factor)
		item := LotteryAnalysisPersona{
			Key: spec.Key, Name: spec.Name, Factor: factor, Supreme: nonNilGrades(view.grades),
			Price: economy.Price, Scale: economy.Scale, Payout: points, Redraws: redraws, Analysis: analysis,
		}
		// 回报按综合概率算（含保底），到账按这类人的折算表
		var p, q float64
		for index, prize := range analysis.Prizes {
			p += prize.LongRun * points[index]
			q += prize.LongRun * redraws[index]
		}
		item.Return = -1
		if q < 1 {
			item.Return = p / (1 - q)
		}
		if !view.effects.Empty() {
			effects := view.effects
			item.Perks = &effects
		}
		out.Personas = append(out.Personas, item)
	}
	out.Economy = s.boardEconomy(ctx, board, board.prizes)
	return out, nil
}

// boardEconomy 一张奖盘（可以是还没保存的）的积分账。
func (s *LotteryService) boardEconomy(ctx context.Context, board *lotteryBoard, prizes []lotterydomain.Prize) *lotterydomain.ActivityEconomy {
	item := lotterydomain.AdminActivity{Activity: board.activity, Prizes: prizes}
	s.annotateEconomy(ctx, board.appID, &item)
	return item.Economy
}

// ── 奖盘求解 ──

// LotterySolveRequest 奖盘求解的请求。
type LotterySolveRequest struct {
	AppID      int64
	ActivityID int64
	Input      *lotterydomain.ActivityInput
	// Persona 目标提在谁的眼里；为空时是普通用户
	Persona *LotteryPersonaSpec
	// Fortune 按哪一天的运气算：average（默认）/ high（最走运）/ low
	Fortune string
	Goal    lotterydomain.SolveGoal
}

// LotterySolveResult 求解结果，连同新旧奖盘的积分账。
type LotterySolveResult struct {
	lotterydomain.SolveResult
	// Persona / Factor 目标提在谁的眼里、按多大的运气系数算
	Persona string  `json:"persona"`
	Factor  float64 `json:"factor"`
	// EconomyBefore / EconomyAfter 求解前后整张奖盘的积分账（普通用户与永久会员的期望回报、套利警告）
	EconomyBefore *lotterydomain.ActivityEconomy `json:"economyBefore,omitempty"`
	EconomyAfter  *lotterydomain.ActivityEconomy `json:"economyAfter,omitempty"`
}

// Solve 按目标解出新的权重。不保存：控制台把解出来的权重填回表单，由管理员确认后保存。
func (s *LotteryService) Solve(ctx context.Context, req LotterySolveRequest) (*LotterySolveResult, error) {
	mode := strings.TrimSpace(req.Fortune)
	switch mode {
	case "":
		mode = LotteryFortuneAverage
	case LotteryFortuneAverage, LotteryFortuneHigh, LotteryFortuneLow:
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的运气取法："+mode)
	}
	specs := []LotteryPersonaSpec{}
	if req.Persona != nil {
		specs = append(specs, *req.Persona)
	}
	specs, err := normalizePersonas(specs)
	if err != nil {
		return nil, err
	}
	board, err := s.loadBoard(ctx, req.AppID, req.ActivityID, req.Input)
	if err != nil {
		return nil, err
	}
	if err := lotterydomain.ValidateSolveGoal(req.Goal, board.prizes); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	view := board.view(specs[0])
	factor := board.factor(view, mode)
	result := lotterydomain.SolveWheel(lotterydomain.SolveModel{
		Activity: board.activity, Strategy: board.activity.Strategy, Prizes: board.prizes, State: board.state(),
		Personalize: func(_ lotterydomain.Activity, prizes []lotterydomain.Prize) (lotterydomain.Activity, []lotterydomain.Prize) {
			return view.activity, lotterydomain.FortunePrizes(board.personal(view, prizes), factor)
		},
	}, req.Goal)
	out := &LotterySolveResult{SolveResult: result, Persona: specs[0].Name, Factor: factor}
	out.EconomyBefore = s.boardEconomy(ctx, board, board.prizes)
	solved := make([]lotterydomain.Prize, len(board.prizes))
	copy(solved, board.prizes)
	for index := range solved {
		solved[index].Weight = result.Prizes[index].NewWeight
	}
	out.EconomyAfter = s.boardEconomy(ctx, board, solved)
	return out, nil
}

// ── 大规模推演 ──

// LotteryComputeRequest 发起一次推演。
type LotteryComputeRequest struct {
	AppID      int64
	ActivityID int64
	Input      *lotterydomain.ActivityInput
	Operator   string
	Name       string
	// 规模：每个世界的人数、天数、每人每天的次数、平行世界数
	Users        int
	Days         int
	DrawsPerDay  int
	Replications int
	// Seed 为 0 时随机取一个（记在任务上，可以原样重跑）
	Seed     uint64
	Source   string
	Personas []LotteryPersonaSpec
}

// scenario 把请求解析成领域层的完整场景。
func (s *LotteryService) computeScenario(ctx context.Context, req LotteryComputeRequest) (*lotteryBoard, lotterydomain.ComputeScenario, error) {
	var scenario lotterydomain.ComputeScenario
	specs, err := normalizePersonas(req.Personas)
	if err != nil {
		return nil, scenario, err
	}
	board, err := s.loadBoard(ctx, req.AppID, req.ActivityID, req.Input)
	if err != nil {
		return nil, scenario, err
	}
	scenario = lotterydomain.ComputeScenario{
		Users: req.Users, Days: req.Days, DrawsPerDay: req.DrawsPerDay, Replications: req.Replications,
		Seed: req.Seed, Source: strings.TrimSpace(req.Source), Start: lotteryDayStart(board.now),
		Personas: make([]lotterydomain.ComputePersona, 0, len(specs)),
	}
	if scenario.Seed == 0 {
		scenario.Seed = rand.Uint64()
	}
	// 一个占比都没给（只列了人群）时按均分算，而不是报「占比合计为 0」
	var shareSum float64
	for _, spec := range specs {
		shareSum += spec.Share
	}
	for index := range specs {
		if shareSum <= 0 {
			specs[index].Share = 1
		}
	}
	for _, spec := range specs {
		view := board.view(spec)
		personal := board.personal(view, board.prizes)
		points, redraws, economy := board.payout(view, personal, board.factor(view, LotteryFortuneAverage))
		scenario.Personas = append(scenario.Personas, lotterydomain.ComputePersona{
			Key: spec.Key, Name: spec.Name, Share: spec.Share, Activity: view.activity, Prizes: personal,
			Supreme: view.grades, Payout: points, Redraws: redraws, DrawsPerDay: spec.DrawsPerDay,
		})
		// 价格取各人群里最低的：只要有一类人能用更便宜的价格买到次数，稳赚与否就该按它判
		if economy.Price > 0 && (scenario.Price == 0 || economy.Price < scenario.Price) {
			scenario.Price = economy.Price
		}
	}
	if err := lotterydomain.ValidateCompute(scenario); err != nil {
		return nil, scenario, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return board, scenario, nil
}

// CreateComputeJob 发起一次推演：解析场景、落成一个排队中的任务，立刻返回。任务由任意一个实例认领执行。
func (s *LotteryService) CreateComputeJob(ctx context.Context, req LotteryComputeRequest) (*lotterydomain.ComputeJob, error) {
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > lotterydomain.MaxComputeJobName {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			fmt.Sprintf("任务名称不能超过 %d 字", lotterydomain.MaxComputeJobName))
	}
	board, scenario, err := s.computeScenario(ctx, req)
	if err != nil {
		return nil, err
	}
	job := lotterydomain.ComputeJob{
		AppID: req.AppID, ActivityName: board.activity.Name, Name: name, CreatedBy: req.Operator,
		Summary: lotterydomain.SummaryOfScenario(scenario),
	}
	if board.saved != nil {
		id := board.saved.ID
		job.ActivityID = &id
	}
	created, err := s.pg.CreateLotteryComputeJob(ctx, job, scenario)
	if err != nil {
		if errors.Is(err, pgrepo.ErrLotteryComputeBusy) {
			return nil, apperrors.New(errCodeLotteryComputeBusy, http.StatusTooManyRequests,
				fmt.Sprintf("排队与执行中的推演已有 %d 个，请等它们结束或先取消", lotterydomain.MaxComputePending))
		}
		return nil, err
	}
	s.compute.wake()
	return created, nil
}

// ListComputeJobs 推演任务列表（新的在前，不带结果）。
func (s *LotteryService) ListComputeJobs(ctx context.Context, appID, activityID int64, page, limit int) (
	*lotterydomain.Page[lotterydomain.ComputeJob], error) {
	page, limit = max(page, 1), min(max(limit, 1), 100)
	items, total, err := s.pg.ListLotteryComputeJobs(ctx, appID, activityID, page, limit)
	if err != nil {
		return nil, err
	}
	return &lotterydomain.Page[lotterydomain.ComputeJob]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: int((total + int64(limit) - 1) / int64(limit))}, nil
}

// GetComputeJob 一个推演任务，完成的带结果。
func (s *LotteryService) GetComputeJob(ctx context.Context, appID, jobID int64) (*lotterydomain.ComputeJob, error) {
	job, err := s.pg.GetLotteryComputeJob(ctx, appID, jobID)
	if err != nil {
		return nil, s.computeError(err)
	}
	return job, nil
}

// CancelComputeJob 取消一个排队或执行中的任务（已经结束的原样返回）。本实例正在跑它时立刻停；
// 在别的实例上跑时，那个实例下一次心跳（最多几秒）发现状态变了就停。
func (s *LotteryService) CancelComputeJob(ctx context.Context, appID, jobID int64, operator string) (*lotterydomain.ComputeJob, error) {
	job, err := s.pg.CancelLotteryComputeJob(ctx, appID, jobID, operator)
	if err != nil {
		return nil, s.computeError(err)
	}
	s.compute.stop(jobID)
	return job, nil
}

// DeleteComputeJob 删除一个已经结束的任务。
func (s *LotteryService) DeleteComputeJob(ctx context.Context, appID, jobID int64) error {
	return s.computeError(s.pg.DeleteLotteryComputeJob(ctx, appID, jobID))
}

func (s *LotteryService) computeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return apperrors.New(errCodeLotteryComputeNotFound, http.StatusNotFound, "推演任务不存在")
	case errors.Is(err, pgrepo.ErrLotteryComputeActive):
		return apperrors.New(errCodeLotteryComputeActive, http.StatusConflict, "任务还在排队或执行，请先取消")
	}
	return err
}

// ── 任务的执行 ──

// lotteryComputeRunner 本实例上的推演执行器：一次只跑一个任务（推演会吃满分给它的核，同时跑几个只会互相拖慢）。
type lotteryComputeRunner struct {
	link clusterLink
	// kick 有新任务时叫醒循环（容量 1：叫醒是幂等的）
	kick chan struct{}
	// running 本实例正在跑的任务 → 让它停下的函数
	running sync.Map
	// done 执行循环退出后关闭（Close 等它，好把手上的任务放回排队）
	done chan struct{}
	// started 循环是否启动过
	started atomic.Bool
}

func newLotteryComputeRunner() *lotteryComputeRunner {
	return &lotteryComputeRunner{kick: make(chan struct{}, 1), done: make(chan struct{})}
}

func (r *lotteryComputeRunner) wake() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// stop 本实例正在跑这个任务时让它停下，返回是否在跑。
func (r *lotteryComputeRunner) stop(jobID int64) bool {
	if cancel, ok := r.running.Load(jobID); ok {
		cancel.(context.CancelFunc)()
		return true
	}
	return false
}

// SetCluster 接上集群协调：推演任务的持有者标识用集群的节点标识（bootstrap 在 Start 之前调用）。
func (s *LotteryService) SetCluster(c *cluster.Cluster) {
	s.compute.link.set(c)
}

// computeOwner 本实例认领任务时写的标识。
func (s *LotteryService) computeOwner() string {
	return s.compute.link.get().ID()
}

// computeWorkers 一次推演同时演几个世界：留一半的核给线上请求，人数多的场景再按内存收紧。
func computeWorkers(users int) int {
	workers := max(1, runtime.GOMAXPROCS(0)/2)
	return max(1, min(workers, lotteryComputeUserBudget/max(users, 1)))
}

// runComputeLoop 认领并执行推演任务，直到 ctx 结束。
func (s *LotteryService) runComputeLoop(ctx context.Context) {
	defer close(s.compute.done)
	ticker := time.NewTicker(lotteryComputePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.compute.kick:
		}
		for ctx.Err() == nil {
			stale := time.Now().Add(-lotteryComputeStaleAfter)
			if failed, err := s.pg.FailExhaustedLotteryComputeJobs(ctx, stale); err != nil {
				if ctx.Err() == nil {
					s.log.Warn("lottery compute: fail exhausted jobs", zap.Error(err))
				}
			} else if failed > 0 {
				s.log.Warn("lottery compute: jobs failed after repeated interruptions", zap.Int64("jobs", failed))
			}
			jobs, err := s.pg.ClaimLotteryComputeJobs(ctx, s.computeOwner(), stale, 1)
			if err != nil {
				if ctx.Err() == nil {
					s.log.Warn("lottery compute: claim failed", zap.Error(err))
				}
				break
			}
			if len(jobs) == 0 {
				break
			}
			s.runComputeJob(ctx, jobs[0])
		}
	}
}

// runComputeJob 执行一个已经认领的任务：边算边续心跳、写进度；算完写回结果。
func (s *LotteryService) runComputeJob(ctx context.Context, job lotterydomain.ComputeJob) {
	owner := s.computeOwner()
	log := s.log.With(zap.Int64("job", job.ID), zap.Int64("appid", job.AppID))
	// 收尾的写入不能跟着 ctx 一起被取消（实例关闭时还要把任务放回排队）
	finish := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	}
	if job.Scenario == nil {
		fctx, cancel := finish()
		defer cancel()
		_, _ = s.pg.FinishLotteryComputeJob(fctx, job.ID, owner, lotterydomain.ComputeFailed, nil, "任务缺少场景", 0)
		return
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	s.compute.running.Store(job.ID, stop)
	defer s.compute.running.Delete(job.ID)

	var merged atomic.Int64
	var lost atomic.Bool
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		ticker := time.NewTicker(lotteryComputeHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				beatCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
				ok, err := s.pg.HeartbeatLotteryComputeJob(beatCtx, job.ID, owner, int(merged.Load()))
				cancel()
				if err != nil {
					// 写不上心跳不立刻停：数据库抖一下不该让算到一半的任务作废；真的长时间写不上，
					// 别的实例会在心跳过期后接手，那时本实例下一次心跳就会返回 false
					if runCtx.Err() == nil {
						log.Warn("lottery compute: heartbeat failed", zap.Error(err))
					}
					continue
				}
				if !ok {
					lost.Store(true)
					stop()
					return
				}
			}
		}
	}()

	scenario := *job.Scenario
	scenario.Workers = computeWorkers(scenario.Users)
	started := time.Now()
	result, err := computeSafely(runCtx, scenario, func(done, _ int) { merged.Store(int64(done)) })
	elapsed := time.Since(started)
	stop()
	<-beatDone

	fctx, cancel := finish()
	defer cancel()
	switch {
	case err == nil:
		wrote, werr := s.pg.FinishLotteryComputeJob(fctx, job.ID, owner, lotterydomain.ComputeSucceeded, &result, "", elapsed)
		if werr != nil {
			// 结果写不进去（编码不了、库里拒收）：判失败并留下原因，不能让任务一直挂在「执行中」被反复接手
			log.Error("lottery compute: save result failed", zap.Error(werr))
			if _, ferr := s.pg.FinishLotteryComputeJob(fctx, job.ID, owner, lotterydomain.ComputeFailed, nil,
				"结果保存失败："+werr.Error(), elapsed); ferr != nil {
				log.Error("lottery compute: save failure failed", zap.Error(ferr))
			}
			return
		}
		if wrote {
			log.Info("lottery compute: job finished", zap.Int64("draws", result.Draws), zap.Duration("took", elapsed),
				zap.Int("workers", scenario.Workers))
		}
	case ctx.Err() != nil:
		// 实例在关闭：放回排队，重启后（或别的实例）按同一个种子重跑
		if rerr := s.pg.ReleaseLotteryComputeJob(fctx, job.ID, owner); rerr != nil {
			log.Warn("lottery compute: release failed", zap.Error(rerr))
		}
	case lost.Load() || errors.Is(err, context.Canceled):
		// 被取消或被别的实例接手：状态已经不归自己写了
		log.Info("lottery compute: job stopped", zap.Bool("lost", lost.Load()))
	default:
		if _, werr := s.pg.FinishLotteryComputeJob(fctx, job.ID, owner, lotterydomain.ComputeFailed, nil, err.Error(), elapsed); werr != nil {
			log.Error("lottery compute: save failure failed", zap.Error(werr))
		}
		log.Warn("lottery compute: job failed", zap.Error(err))
	}
}

// computeSafely 推演；把里面的 panic 变成错误（任务判失败、留下原因，不带垮执行循环）。
func computeSafely(ctx context.Context, scenario lotterydomain.ComputeScenario, progress func(done, total int)) (
	result lotterydomain.ComputeResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("推演内部出错：%v", r)
		}
	}()
	return lotterydomain.Compute(ctx, scenario, progress)
}
