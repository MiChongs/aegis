package lottery

import (
	crand "crypto/rand"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gonum.org/v1/gonum/stat/sampleuv"
)

// 出奖策略。
//
// 旧版只有「固定权重随机 + 硬保底」：同一个奖盘对谁、在什么时候、抽了多少次都是同一组概率，
// 限量奖品开场几分钟就被抽空，积分奖发多少全看运气，换号刷也拿得一样多。
//
// 策略在基础权重之上逐个维度乘一个系数，算出这一次、这个人的最终概率：
//
//	基础权重（含已拥有剔除）
//	  → 硬性资格：库存、全站每日限量、个人总上限、个人每日上限
//	  → 风控规则（risk.go）：信号比阈值，命中后降权、停发稀有、不触发保底、只发兜底或拒绝抽奖
//	  → 库存匀速释放：限量奖品按活动进度放量，超前暂停、落后加速
//	  → 积分预算：当日已发积分过半后积分奖逐步收紧，单次超出剩余预算的不出
//	  → 行为画像：注册时长、在线时长、签到、会员率、付费、等级、邀请、广告、兑换、风险等全部行为
//	    加权成 0–1 的行为分，折成中奖奖品的系数（见 behavior.go）
//	  → 中奖冷却：最近 N 次里中过的奖品打折
//	  → 自定义规则：expr 表达式按人、按次、按奖品、按活动进度乘系数
//	  → 控制层（strategy_control.go）：运气平衡 → 软保底或伪随机分布 → 全站中奖率反馈 → 护栏投影
//	  → 硬保底（PityThreshold，旧逻辑）：到点直接给保底奖，资格不满足时退回常规抽取
//
// 每个维度都把系数记进 Factor，最终概率与系数一起写进抽奖记录的 trace，管理端能看到「为什么是这个结果」。
// 抽样用 gonum 的 sampleuv.Weighted，随机源是每次用 crypto/rand 播种的 ChaCha8。
//
// 零值策略、奖品不设限量时，结果与旧版完全相同。

// 策略的规模与取值上界。
const (
	MaxStrategyRules    = 12
	MaxRuleNameRunes    = 32
	MaxRuleExprLength   = 500
	MaxRuleFactor       = 100
	MaxSoftPityStep     = 50
	MaxCooldownDraws    = 100
	MaxPacingBoost      = 5
	MaxBudgetPoints     = 1_000_000_000
	MaxRiskAccounts     = 1000
	MaxPrizeLimit       = 1_000_000
	DefaultPacingSlack  = 0.05
	budgetTaperFrom     = 0.5
	probabilityDecimals = 1e6
)

// 系数的标识（trace 里的 k）。
const (
	FactorStock          = "stock"
	FactorDailyLimit     = "dailyLimit"
	FactorUserLimit      = "userLimit"
	FactorUserDailyLimit = "userDailyLimit"
	FactorRisk           = "risk"
	FactorPacing         = "pacing"
	FactorBudget         = "budget"
	FactorCooldown       = "cooldown"
	FactorBehavior       = "behavior"
	FactorRule           = "rule"
	FactorSoftPity       = "softPity"
)

// Strategy 活动的出奖策略（lottery_activities.strategy）。每一项都可以不设。
type Strategy struct {
	SoftPity *SoftPity      `json:"softPity,omitempty"`
	Pacing   *Pacing        `json:"pacing,omitempty"`
	Cooldown *Cooldown      `json:"cooldown,omitempty"`
	Budget   *Budget        `json:"budget,omitempty"`
	Risk     *RiskGuard     `json:"risk,omitempty"`
	Behavior *BehaviorModel `json:"behavior,omitempty"`
	Rules    []StrategyRule `json:"rules,omitempty"`
	// 控制层，见 strategy_control.go
	Luck     *LuckBalance    `json:"luck,omitempty"`
	Variance string          `json:"variance,omitempty"`
	WinRate  *WinRateControl `json:"winRate,omitempty"`
	Guard    *Guard          `json:"guard,omitempty"`
	// Fortune 每日运气，见 fortune.go（在个人奖盘上生效，不在出奖方案里）
	Fortune *Fortune `json:"fortune,omitempty"`
}

// SoftPity 软保底：连续未中达到 Start 次后，每多一次未中，中奖总概率抬高 Step 个百分点（上限 100%）。
// 抬高的部分按各中奖奖品当前的比例分配，未中奖的权重相应缩小。
type SoftPity struct {
	Start int     `json:"start"`
	Step  float64 `json:"step"`
}

// Pacing 库存匀速释放：限量奖品在活动时间窗里按进度放量。
// 已放量 = 库存 × 进度 + 库存 × Slack；已抽中追上已放量时权重降到 0（不是售罄，过一会儿又会放出来）。
// MaxBoost > 1 时，落后于进度的奖品权重最多放大到这个倍数，追回进度。没有结束时间的活动不生效。
type Pacing struct {
	Slack    float64 `json:"slack"`
	MaxBoost float64 `json:"maxBoost"`
}

// Cooldown 中奖冷却：这个人最近 Draws 次里抽中过的奖品，这一次权重 × Factor（0–1）。只作用于中奖奖品。
type Cooldown struct {
	Draws  int     `json:"draws"`
	Factor float64 `json:"factor"`
}

// Budget 积分预算：本活动当天发出的积分（含折算）达到预算一半后，积分奖的权重随剩余比例线性收紧；
// 单个奖品的积分超过剩余预算时不出。
//
// Paced 为真时改为按当天时间进度放量（见 pacedBudgetFactor）：超前收紧、落后放宽。
type Budget struct {
	DailyPoints int64 `json:"dailyPoints"`
	Paced       bool  `json:"paced,omitempty"`
}

// StrategyRule 一条自定义规则：When 为真时，给命中的奖品（Kinds / Positions 都空 = 全部奖品）乘 Factor。
// When 是 expr 表达式，变量见 RuleEnv；空表示总是生效。Factor 为 0 等于这一次剔除这些奖品。
type StrategyRule struct {
	Name      string   `json:"name"`
	When      string   `json:"when"`
	Kinds     []string `json:"kinds,omitempty"`
	Positions []int    `json:"positions,omitempty"`
	Factor    float64  `json:"factor"`
	Disabled  bool     `json:"disabled,omitempty"`
}

// Empty 没有任何策略。
func (s Strategy) Empty() bool {
	return s.SoftPity == nil && s.Pacing == nil && s.Cooldown == nil && s.Budget == nil && s.Risk == nil &&
		s.Behavior == nil && len(s.Rules) == 0 && s.Luck == nil && s.Variance == "" && s.WinRate == nil && s.Guard == nil && s.Fortune == nil
}

// NormalizeStrategy 清洗：去掉不生效的项，补默认值。
func NormalizeStrategy(s Strategy) Strategy {
	if s.SoftPity != nil && (s.SoftPity.Start <= 0 || s.SoftPity.Step <= 0) {
		s.SoftPity = nil
	}
	if s.Pacing != nil {
		pacing := *s.Pacing
		if pacing.Slack <= 0 {
			pacing.Slack = DefaultPacingSlack
		}
		if pacing.MaxBoost < 1 {
			pacing.MaxBoost = 1
		}
		s.Pacing = &pacing
	}
	if s.Cooldown != nil && (s.Cooldown.Draws <= 0 || s.Cooldown.Factor >= 1) {
		s.Cooldown = nil
	}
	if s.Budget != nil && s.Budget.DailyPoints <= 0 {
		s.Budget = nil
	}
	s.Risk = normalizeRisk(s.Risk)
	s.Behavior = normalizeBehavior(s.Behavior)
	normalizeControl(&s)
	rules := make([]StrategyRule, 0, len(s.Rules))
	for _, rule := range s.Rules {
		rule.Name = strings.TrimSpace(rule.Name)
		rule.When = strings.TrimSpace(rule.When)
		kinds := make([]string, 0, len(rule.Kinds))
		for _, kind := range rule.Kinds {
			if kind = strings.TrimSpace(kind); kind != "" && !slices.Contains(kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
		rule.Kinds = kinds
		slices.Sort(rule.Positions)
		rule.Positions = slices.Compact(rule.Positions)
		rules = append(rules, rule)
	}
	s.Rules = rules
	if len(s.Rules) == 0 {
		s.Rules = nil
	}
	return s
}

// ValidateStrategy 校验清洗后的策略；prizeCount 用来核对规则里的奖品位置。
func ValidateStrategy(s Strategy, prizeCount int) error {
	if p := s.SoftPity; p != nil {
		if p.Start > MaxPityThreshold {
			return fmt.Errorf("软保底起点需要在 1–%d 之间", MaxPityThreshold)
		}
		if p.Step > MaxSoftPityStep {
			return fmt.Errorf("软保底每次提升需要在 0–%d 个百分点之间", MaxSoftPityStep)
		}
	}
	if p := s.Pacing; p != nil {
		if p.Slack > 1 {
			return fmt.Errorf("匀速释放的提前量需要在 0–100%% 之间")
		}
		if p.MaxBoost > MaxPacingBoost {
			return fmt.Errorf("匀速释放的追赶倍数需要在 1–%d 之间", MaxPacingBoost)
		}
	}
	if c := s.Cooldown; c != nil {
		if c.Draws > MaxCooldownDraws {
			return fmt.Errorf("中奖冷却的次数需要在 1–%d 之间", MaxCooldownDraws)
		}
		if c.Factor < 0 {
			return fmt.Errorf("中奖冷却的系数需要在 0–1 之间")
		}
	}
	if b := s.Budget; b != nil && b.DailyPoints > MaxBudgetPoints {
		return fmt.Errorf("每日积分预算不能超过 %d", MaxBudgetPoints)
	}
	if err := validateRisk(s.Risk); err != nil {
		return err
	}
	if err := validateBehavior(s.Behavior); err != nil {
		return err
	}
	if err := validateControl(s); err != nil {
		return err
	}
	if len(s.Rules) > MaxStrategyRules {
		return fmt.Errorf("自定义规则最多 %d 条", MaxStrategyRules)
	}
	for index, rule := range s.Rules {
		label := fmt.Sprintf("第 %d 条规则", index+1)
		if rule.Name != "" {
			label = fmt.Sprintf("规则「%s」", rule.Name)
		}
		if utf8.RuneCountInString(rule.Name) > MaxRuleNameRunes {
			return fmt.Errorf("%s的名称不能超过 %d 字", label, MaxRuleNameRunes)
		}
		if len(rule.When) > MaxRuleExprLength {
			return fmt.Errorf("%s的条件不能超过 %d 个字符", label, MaxRuleExprLength)
		}
		if rule.Factor < 0 || rule.Factor > MaxRuleFactor {
			return fmt.Errorf("%s的系数需要在 0–%d 之间", label, MaxRuleFactor)
		}
		for _, kind := range rule.Kinds {
			if _, ok := FindSpec(kind); !ok {
				return fmt.Errorf("%s里的奖品类型 %s 不存在", label, kind)
			}
		}
		for _, position := range rule.Positions {
			if position < 0 || position >= prizeCount {
				return fmt.Errorf("%s里的奖品位置 %d 超出奖盘", label, position)
			}
		}
		if _, err := compileRule(rule.When); err != nil {
			return fmt.Errorf("%s的条件有误：%w", label, err)
		}
	}
	return nil
}

// ── 一次抽奖的输入 ──

// DrawState 选奖时这个人、这个活动此刻的事实。由仓储在抽奖事务里取齐（模拟器里由模拟过程维护）。
type DrawState struct {
	Now    time.Time
	Source string
	// MissStreak 连续未中次数；TotalDraws / TodayDraws 本活动里抽过几次（不含这一次）
	MissStreak int
	TotalDraws int
	TodayDraws int
	WinsTotal  int
	WinsToday  int
	// PrizeWins / PrizeWinsToday 这个人每个奖品抽中过几次（按奖品 ID）
	PrizeWins      map[int64]int
	PrizeWinsToday map[int64]int
	// Recent 这个人最近几次抽中的奖品 ID，新的在前（未中记 0）；长度取 Cooldown.Draws
	Recent []int64
	// PrizeDailyUsed 全站今天每个奖品出了几个（只含设了每日限量的奖品）
	PrizeDailyUsed map[int64]int64
	// PointsIssuedToday 本活动今天发出的积分（含已拥有折算）
	PointsIssuedToday int64
	// DeviceAccounts / IPAccounts / SubnetAccounts 本活动今天同设备 / 同 IP / 同网段抽过奖的账号数（含这个人）
	DeviceAccounts int
	IPAccounts     int
	SubnetAccounts int
	// DeviceHistory 这台设备在应用里登录过的账号数
	DeviceHistory int
	// DeviceMissing / IPMissing 请求没有设备号 / IP（相应的聚集信号取不到）
	DeviceMissing bool
	IPMissing     bool
	// RecentDraws 这个人最近 BurstWindowMinutes 分钟在本活动抽了几次（不含这一次）
	RecentDraws int
	// Profile 这个人在应用里的全部行为
	Profile BehaviorProfile
	// DayProgress 应用时区的今天过去了多少（0–1）
	DayProgress float64
	// ActivityDrawsToday / ActivityWinsToday 本活动今天全站抽了几次、中了几次（全站反馈用）
	ActivityDrawsToday int
	ActivityWinsToday  int
	// LuckExpected / LuckVariance 这个人在本活动里历次抽奖的中奖概率之和与 p(1−p) 之和（运气平衡用）
	LuckExpected float64
	LuckVariance float64
	// LuckSkew 历次 p(1−p)(1−2p) 之和（泊松二项分布的三阶中心矩，偏度修正用）
	LuckSkew float64
}

// Factor 一个维度给一个奖品的系数。
type Factor struct {
	Key   string  `json:"k"`
	Value float64 `json:"v"`
	Note  string  `json:"n,omitempty"`
}

// PrizeOdds 一个奖品在这一次里的最终权重与概率。
type PrizeOdds struct {
	PrizeID  int64   `json:"id"`
	Position int     `json:"pos"`
	Base     float64 `json:"base"`
	Weight   float64 `json:"w"`
	// Probability 这一次抽中它的概率（0–1，六位小数）
	Probability float64  `json:"p"`
	Factors     []Factor `json:"f,omitempty"`
	// Blocked 被硬性资格挡掉的原因（库存、限量、上限、风控）；为空时只是权重被调整
	Blocked string `json:"blocked,omitempty"`
}

// DrawPlan 一次抽奖的完整出奖方案。会原样写进记录的 trace。
type DrawPlan struct {
	Odds []PrizeOdds `json:"odds"`
	// SoftPity 软保底给中奖总概率加了多少个百分点
	SoftPity float64 `json:"softPity,omitempty"`
	// Risk 风控只发兜底或拒绝（老控制台只认这一项）；RiskReport 逐条规则的评估
	Risk       bool        `json:"risk,omitempty"`
	RiskReport *RiskReport `json:"riskReport,omitempty"`
	// Behavior 行为分与各信号明细（启用行为画像时）
	Behavior *BehaviorScore `json:"behavior,omitempty"`
	// WinRate 这一次的中奖总概率（0–1）；Value 这一次的期望价值（积分当量）
	WinRate float64 `json:"winRate"`
	Value   float64 `json:"value"`
	// Luck 这个人的运气 z 分数；PRD 伪随机分布给这一次定的中奖率
	Luck float64 `json:"luck,omitempty"`
	// Fair 控制层之前的中奖率：运气平衡按它累计「应得」的中奖次数（用校正后的概率会自我追涨、来回过冲）
	Fair float64 `json:"fair"`
	// GuardInfeasible 护栏约束在可发的奖品上无法同时满足（已尽力逼近）
	GuardInfeasible bool    `json:"guardInfeasible,omitempty"`
	PRD             float64 `json:"prd,omitempty"`
	// Pity 硬保底触发；Picked 最终选中的奖品 ID；Fallback 改发了兜底奖，FallbackReason 说明为什么
	Pity           bool   `json:"pity,omitempty"`
	Picked         int64  `json:"picked,omitempty"`
	Fallback       bool   `json:"fallback,omitempty"`
	FallbackReason string `json:"fallbackReason,omitempty"`
	// FloorID 这一次的兜底奖（风控只发兜底、权重全为 0、限量被抢光时发它）
	FloorID int64 `json:"floor,omitempty"`
	// Perks 这一次生效的人群特权（权重与保底已按它改写）
	Perks *PerkEffects `json:"perks,omitempty"`
	// Fortune 这个人今天的运气系数（真实奖品的权重已乘过）；没开每日运气时不写
	Fortune float64 `json:"fortune,omitempty"`
	// FortuneBase / Supreme 拿到至尊加成时：加成之前的系数与生效的参数（Fortune 是加成之后的）
	FortuneBase float64       `json:"fortuneBase,omitempty"`
	Supreme     *SupremeGrade `json:"supreme,omitempty"`
}

// PlanDraw 按策略算出这一次每个奖品的最终概率。纯函数：同样的输入永远得到同样的方案。
//
// prizes 是这个人的奖盘（已按已拥有处理方式剔除，见 PersonalPrizes）。
func PlanDraw(activity Activity, strategy Strategy, prizes []Prize, state DrawState) DrawPlan {
	plan := DrawPlan{Odds: make([]PrizeOdds, len(prizes))}
	risk := EvaluateRisk(strategy.Risk, state)
	plan.RiskReport = risk
	plan.Risk = risk.Floor()
	if floor, ok := FallbackPrize(prizes); ok {
		plan.FloorID = floor.ID
	}
	// Probabilities 是百分比，风控的稀有线是 0–1
	baseProbabilities := Probabilities(prizes)
	missStreak := state.MissStreak
	if risk != nil && risk.NoPity {
		missStreak = 0
	}
	recent := map[int64]bool{}
	if c := strategy.Cooldown; c != nil {
		for index, id := range state.Recent {
			if index >= c.Draws {
				break
			}
			if id > 0 {
				recent[id] = true
			}
		}
	}
	progress, daysLeft := activityProgress(activity, state.Now)
	rules := compiledRules(strategy.Rules)
	behavior := ScoreBehavior(strategy.Behavior, state.Profile)
	plan.Behavior = behavior

	for index, prize := range prizes {
		odds := PrizeOdds{PrizeID: prize.ID, Position: prize.Position, Base: float64(prize.Weight)}
		weight := float64(prize.Weight)
		block := func(key, note string) {
			odds.Blocked = key
			odds.Factors = append(odds.Factors, Factor{Key: key, Value: 0, Note: note})
			weight = 0
		}
		multiply := func(key string, value float64, note string) {
			if value == 1 {
				return
			}
			odds.Factors = append(odds.Factors, Factor{Key: key, Value: round6(value), Note: note})
			weight *= value
		}

		// 资格不看权重：权重为 0 的保底奖也要知道自己这一次能不能发
		switch {
		case !prize.InStock():
			block(FactorStock, "")
		case prize.DailyLimit > 0 && state.PrizeDailyUsed[prize.ID] >= int64(prize.DailyLimit):
			block(FactorDailyLimit, fmt.Sprintf("%d/%d", state.PrizeDailyUsed[prize.ID], prize.DailyLimit))
		case prize.UserLimit > 0 && state.PrizeWins[prize.ID] >= prize.UserLimit:
			block(FactorUserLimit, fmt.Sprintf("%d/%d", state.PrizeWins[prize.ID], prize.UserLimit))
		case prize.UserDailyLimit > 0 && state.PrizeWinsToday[prize.ID] >= prize.UserDailyLimit:
			block(FactorUserDailyLimit, fmt.Sprintf("%d/%d", state.PrizeWinsToday[prize.ID], prize.UserDailyLimit))
		}
		if weight > 0 || odds.Blocked == "" {
			blocked, note, damp := riskPrizeEffect(risk, prize, baseProbabilities[index]/100, plan.FloorID)
			switch {
			case blocked && odds.Blocked == "":
				block(FactorRisk, note)
			case damp < 1 && weight > 0:
				multiply(FactorRisk, damp, note)
			}
		}
		// 只发兜底时兜底奖必须可出：它的权重为 0（纯保底格）时给 1，方案里看得到它是 100%
		if risk.Floor() && prize.ID == plan.FloorID && odds.Blocked == "" && weight == 0 {
			odds.Factors = append(odds.Factors, Factor{Key: FactorRisk, Value: 1, Note: "只发兜底，兜底奖照常可出"})
			weight = 1
		}

		if weight > 0 && strategy.Pacing != nil && prize.Quantity >= 0 && progress >= 0 {
			value, note := pacingFactor(*strategy.Pacing, prize, progress)
			multiply(FactorPacing, value, note)
		}
		if weight > 0 && strategy.Budget != nil && prize.Kind == KindPoints {
			value, note := budgetFactor(*strategy.Budget, prize.Reward.Amount, state.PointsIssuedToday)
			if strategy.Budget.Paced {
				value, note = pacedBudgetFactor(*strategy.Budget, prize.Reward.Amount, state.PointsIssuedToday, state.DayProgress)
			}
			multiply(FactorBudget, value, note)
		}
		if weight > 0 && behavior != nil && behaviorApplies(strategy.Behavior, prize) {
			multiply(FactorBehavior, behavior.Factor, fmt.Sprintf("行为分 %.2f", behavior.Score))
		}
		if weight > 0 && prize.Winning() && recent[prize.ID] {
			multiply(FactorCooldown, strategy.Cooldown.Factor, "")
		}
		if weight > 0 && len(rules) > 0 {
			score := 0.0
			if behavior != nil {
				score = behavior.Score
			}
			env := ruleEnv(state, prize, baseProbabilities[index], progress, daysLeft, score)
			for _, rule := range rules {
				if !rule.applies(prize) {
					continue
				}
				hit, err := rule.eval(env)
				if err != nil {
					odds.Factors = append(odds.Factors, Factor{Key: FactorRule, Value: 1, Note: rule.label() + "：执行出错，已跳过"})
					continue
				}
				if hit {
					multiply(FactorRule, rule.Factor, rule.label())
				}
			}
		}
		odds.Weight = weight
		plan.Odds[index] = odds
	}

	plan.Fair = round6(winShare(plan.Odds, prizes))
	applyLuck(&plan, prizes, strategy.Luck, state)
	if strategy.Variance == VariancePRD {
		applyPRD(&plan, prizes, missStreak)
	} else if p := strategy.SoftPity; p != nil && missStreak >= p.Start {
		plan.SoftPity = applySoftPity(plan.Odds, prizes, p.Step*float64(missStreak-p.Start+1))
	}
	applyWinRateControl(&plan, prizes, strategy.WinRate, state)
	values := make([]float64, len(prizes))
	for index, prize := range prizes {
		values[index] = PrizeValue(activity, strategy.Guard, prize)
	}
	applyGuard(&plan, prizes, strategy.Guard, values)

	var total, winning, value float64
	for index, odds := range plan.Odds {
		total += odds.Weight
		if prizes[index].Winning() {
			winning += odds.Weight
		}
	}
	if total > 0 {
		for index := range plan.Odds {
			plan.Odds[index].Probability = round6(plan.Odds[index].Weight / total)
			value += plan.Odds[index].Weight / total * values[index]
		}
		plan.WinRate = round6(winning / total)
		plan.Value = round6(value)
	}
	return plan
}

// applySoftPity 把中奖总概率抬高 boost 个百分点：中奖奖品之间的比例不变，只缩小未中奖的权重。
// 返回实际抬高了多少个百分点。
func applySoftPity(odds []PrizeOdds, prizes []Prize, boost float64) float64 {
	var winning, losing float64
	for index, item := range odds {
		if prizes[index].Winning() {
			winning += item.Weight
		} else {
			losing += item.Weight
		}
	}
	if winning <= 0 || losing <= 0 || boost <= 0 {
		return 0
	}
	current := winning / (winning + losing)
	target := math.Min(1, current+boost/100)
	var scale float64
	if target < 1 {
		// winning / (winning + scale·losing) = target
		scale = winning * (1 - target) / (target * losing)
	}
	for index := range odds {
		if prizes[index].Winning() || odds[index].Weight == 0 {
			continue
		}
		odds[index].Weight *= scale
		odds[index].Factors = append(odds[index].Factors, Factor{Key: FactorSoftPity, Value: round6(scale)})
	}
	return round6((target - current) * 100)
}

// activityProgress 活动进度（0–1）与剩余天数；没有结束时间时进度为 -1。
func activityProgress(activity Activity, now time.Time) (float64, float64) {
	if activity.EndTime == nil {
		return -1, -1
	}
	start := activity.CreatedAt
	if activity.StartTime != nil {
		start = *activity.StartTime
	}
	span := activity.EndTime.Sub(start)
	if span <= 0 {
		return 1, 0
	}
	progress := float64(now.Sub(start)) / float64(span)
	daysLeft := math.Max(0, activity.EndTime.Sub(now).Hours()/24)
	return math.Min(1, math.Max(0, progress)), daysLeft
}

// pacingFactor 已放量 = 库存 ×（进度 + 提前量）。超前时在提前量内线性降到 0；落后超过一个提前量时放大到 MaxBoost。
func pacingFactor(pacing Pacing, prize Prize, progress float64) (float64, string) {
	quantity := float64(prize.Quantity)
	if quantity <= 0 {
		return 1, ""
	}
	slack := math.Max(1, pacing.Slack*quantity)
	scheduled := quantity * progress
	released := math.Min(quantity, scheduled+slack)
	used := float64(prize.Used)
	note := fmt.Sprintf("%d/%.0f", prize.Used, released)
	if used >= released {
		return 0, note
	}
	if ahead := used - scheduled; ahead > 0 {
		return round6(1 - ahead/slack), note
	}
	if pacing.MaxBoost > 1 {
		behind := math.Min(1, (scheduled-used)/slack)
		return round6(1 + behind*(pacing.MaxBoost-1)), note
	}
	return 1, note
}

// budgetFactor 当日积分预算：超过剩余预算的不出；用掉一半之后按剩余比例线性收紧。
func budgetFactor(budget Budget, amount, issued int64) (float64, string) {
	remaining := budget.DailyPoints - issued
	note := fmt.Sprintf("%d/%d", issued, budget.DailyPoints)
	if remaining <= 0 || amount > remaining {
		return 0, note
	}
	used := float64(issued) / float64(budget.DailyPoints)
	if used <= budgetTaperFrom {
		return 1, note
	}
	return round6((1 - used) / (1 - budgetTaperFrom)), note
}

// ── 抽样 ──

// NewDrawSource 一次抽奖的随机源：用 crypto/rand 取 32 字节播种的 ChaCha8。
// 每次抽奖一个新源，不跨请求复用，也就没有可以被推算的共享状态。
func NewDrawSource() (rand.Source, error) {
	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		return nil, err
	}
	return rand.NewChaCha8(seed), nil
}

// Sample 按方案的最终权重抽一个，返回奖品在 prizes 里的下标。所有权重都为 0 时返回 false。
func Sample(plan DrawPlan, src rand.Source) (int, bool) {
	weights := make([]float64, len(plan.Odds))
	positive := false
	for index, odds := range plan.Odds {
		weights[index] = odds.Weight
		if odds.Weight > 0 {
			positive = true
		}
	}
	if !positive {
		return 0, false
	}
	return sampleuv.NewWeighted(weights, src).Take()
}

// PityChoice 硬保底：到点、风控没有关掉保底、且保底奖这一次没有被硬性资格挡掉时，返回它的下标。
func PityChoice(activity Activity, plan DrawPlan, prizes []Prize, missStreak int) (int, bool) {
	if plan.RiskReport != nil && plan.RiskReport.NoPity {
		return 0, false
	}
	if activity.PityPrizeID == nil || !PityDue(activity.PityThreshold, missStreak) {
		return 0, false
	}
	for index, prize := range prizes {
		if prize.ID == *activity.PityPrizeID {
			return index, plan.Odds[index].Blocked == ""
		}
	}
	return 0, false
}

func round6(v float64) float64 {
	return math.Round(v*probabilityDecimals) / probabilityDecimals
}
