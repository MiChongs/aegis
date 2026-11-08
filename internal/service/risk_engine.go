package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// ════════════════════════════════════════════════════════════
//  评估引擎
//
//  一次评估的顺序固定：
//    1. 装配环境（请求 / 客户端 / 频率 / 设备 / 网络 / 地理 / 账号）
//    2. 名单：黑名单直接拦、白名单直接放、观察名单写进环境供规则使用
//    3. 规则：只跑对该应用、该场景生效的；按维度汇总分数，观察模式只留痕
//    4. 处置：分数策略与规则强制处置取更严的一个；验证码在不支持的场景转复核
//    5. 执行：ban 当场落到账号 / 设备 / IP / 防火墙；其余留痕异步写库
//
//  模拟、重放与线上评估走同一段 decide —— 两套判定是「模拟通过、上线不中」的根源。
// ════════════════════════════════════════════════════════════

// riskSnapshotTTL 规则 / 策略 / 名单内存快照的有效期。本实例的写操作会立刻作废快照，
// 其它实例最迟在这么久之后看到变化。
const riskSnapshotTTL = 15 * time.Second

// ruleForcedBanSeconds 规则强制 ban、却没有命中任何 ban 策略时的封禁时长
const ruleForcedBanSeconds = 24 * 3600

type riskSnapshot struct {
	rules    []securitydomain.RiskRule
	actions  []securitydomain.RiskAction
	lists    *riskListIndex
	loadedAt time.Time
}

func (s *RiskService) loadSnapshot(ctx context.Context) (*riskSnapshot, error) {
	if snap := s.snapshot.Load(); snap != nil && timeutil.Since(snap.loadedAt) < riskSnapshotTTL {
		return snap, nil
	}
	value, err, _ := s.snapshotFlight.Do("snapshot", func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		rules, err := s.pg.ListActiveRiskRules(loadCtx)
		if err != nil {
			return nil, err
		}
		actions, err := s.pg.ListRiskActions(loadCtx, pgRiskScope("", nil))
		if err != nil {
			return nil, err
		}
		entries, err := s.pg.ListActiveRiskListEntries(loadCtx)
		if err != nil {
			return nil, err
		}
		snap := &riskSnapshot{rules: rules, actions: actions, lists: buildRiskListIndex(entries), loadedAt: timeutil.Now()}
		s.snapshot.Store(snap)
		return snap, nil
	})
	if err != nil {
		// 刷新失败时沿用旧快照：库抖一下不该让风控整体失效
		if snap := s.snapshot.Load(); snap != nil {
			s.logWarn("刷新风控快照失败，沿用旧快照", zap.Error(err))
			return snap, nil
		}
		return nil, err
	}
	return value.(*riskSnapshot), nil
}

// invalidateSnapshot 写操作之后作废快照，下一次评估重新装载
func (s *RiskService) invalidateSnapshot() {
	if snap := s.snapshot.Load(); snap != nil {
		stale := *snap
		stale.loadedAt = time.Time{}
		s.snapshot.Store(&stale)
	}
}

// rulesFor 对某应用某场景生效的规则（已按优先级排好）
func (snap *riskSnapshot) rulesFor(appID int64, scene string) []securitydomain.RiskRule {
	out := make([]securitydomain.RiskRule, 0, len(snap.rules))
	for _, rule := range snap.rules {
		if ruleApplies(rule, appID, scene) {
			out = append(out, rule)
		}
	}
	return out
}

func ruleApplies(rule securitydomain.RiskRule, appID int64, scene string) bool {
	if !rule.IsActive {
		return false
	}
	if rule.Scene != scene && rule.Scene != securitydomain.SceneAll {
		return false
	}
	return rule.AppID == nil || *rule.AppID == appID
}

// riskDecision 一次判定的完整结论
type riskDecision struct {
	evaluations []securitydomain.RuleEvaluation
	matched     []securitydomain.MatchedRule
	dimensions  map[string]int
	listHits    []securitydomain.ListHit
	listEntries []securitydomain.RiskListEntry
	total       int
	level       string
	action      string
	detail      string
	decisionBy  string
	policy      *securitydomain.RiskAction
	banSeconds  int
	banTargets  []string
}

// decide 名单 → 规则 → 处置。纯判定，不写库、不执行处置。
func (s *RiskService) decide(ctx context.Context, snap *riskSnapshot, rules []securitydomain.RiskRule, env *RiskEvalEnv) riskDecision {
	d := riskDecision{dimensions: map[string]int{}, listHits: []securitydomain.ListHit{}}

	// ── 名单 ──
	var allowHits, blockHits []securitydomain.RiskListEntry
	if snap != nil {
		d.listEntries = snap.lists.match(listSubjectFromEnv(env), env.evaluatedAt)
	}
	for _, entry := range d.listEntries {
		d.listHits = append(d.listHits, securitydomain.ListHit{
			EntryID: entry.ID, ListType: entry.ListType, Kind: entry.Kind, Value: entry.Value, Reason: entry.Reason,
		})
		switch entry.Kind {
		case securitydomain.ListKindAllow:
			allowHits = append(allowHits, entry)
		case securitydomain.ListKindBlock:
			blockHits = append(blockHits, entry)
		case securitydomain.ListKindWatch:
			env.ListWatch = true
			if !slices.Contains(env.ListWatchTypes, entry.ListType) {
				env.ListWatchTypes = append(env.ListWatchTypes, entry.ListType)
			}
		}
	}

	// ── 规则（名单命中也照跑：留痕里要看得到「如果没有名单会怎样」）──
	d.evaluations = s.evaluateRules(rules, env)
	forced := securitydomain.ActionPass
	var forcedRule string
	for _, item := range d.evaluations {
		if !item.Hit {
			continue
		}
		observe := item.Mode == securitydomain.RuleModeObserve
		d.matched = append(d.matched, securitydomain.MatchedRule{
			RuleID: item.RuleID, RuleName: item.RuleName, ConditionType: item.ConditionType,
			Dimension: item.Dimension, Score: item.Score, Reason: item.Reason, Observe: observe, Decision: item.Decision,
		})
		if observe {
			continue
		}
		d.total += item.Score
		d.dimensions[item.Dimension] += item.Score
		if item.Decision != "" && securitydomain.ActionSeverity(item.Decision) > securitydomain.ActionSeverity(forced) {
			forced = item.Decision
			forcedRule = item.RuleName
		}
	}
	if d.matched == nil {
		d.matched = []securitydomain.MatchedRule{}
	}
	d.level = securitydomain.ScoreToLevel(d.total)

	// ── 处置 ──
	switch {
	case len(blockHits) > 0:
		// 黑白同时命中时黑名单优先：IP 加白不该放过一台已经拉黑的设备
		d.action = securitydomain.ActionBlock
		d.decisionBy = securitydomain.DecisionByBlocklist
		d.detail = fmt.Sprintf("命中黑名单：%s %s", listTypeLabel(blockHits[0].ListType), blockHits[0].Value)
	case len(allowHits) > 0:
		d.action = securitydomain.ActionPass
		d.decisionBy = securitydomain.DecisionByAllowlist
		d.detail = fmt.Sprintf("命中白名单：%s %s", listTypeLabel(allowHits[0].ListType), allowHits[0].Value)
	default:
		policy := resolvePolicy(snap, env.AppID, env.Scene, d.total)
		policyAction := securitydomain.ActionPass
		if policy != nil {
			policyAction = policy.Action
			d.policy = policy
		}
		switch {
		case securitydomain.ActionSeverity(forced) > securitydomain.ActionSeverity(policyAction):
			d.action = forced
			d.decisionBy = securitydomain.DecisionByRule
			d.detail = fmt.Sprintf("规则「%s」强制%s", forcedRule, actionLabel(forced))
		case policyAction != securitydomain.ActionPass:
			d.action = policyAction
			d.decisionBy = securitydomain.DecisionByPolicy
			d.detail = policy.Description
			if d.detail == "" {
				d.detail = fmt.Sprintf("总分 %d 命中策略区间 %d–%s", d.total, policy.MinScore, formatMaxScore(policy.MaxScore))
			}
		default:
			d.action = securitydomain.ActionPass
			d.decisionBy = securitydomain.DecisionByNone
		}
		if d.action == securitydomain.ActionBan {
			d.banSeconds = ruleForcedBanSeconds
			d.banTargets = []string{securitydomain.BanTargetAccount, securitydomain.BanTargetDevice}
			if policy != nil && policy.Action == securitydomain.ActionBan {
				d.banSeconds = policy.BanDuration
				if len(policy.BanTargets) > 0 {
					d.banTargets = policy.BanTargets
				}
			}
			d.detail = fmt.Sprintf("%s（封禁 %s）", d.detail, humanDuration(d.banSeconds))
		}
	}

	// 验证码：已经过了就放行；场景接不了验证码就转人工复核 ——
	// 返回一个客户端处理不了的验证码要求，等于拒绝服务。
	if d.action == securitydomain.ActionCaptcha {
		scene, _ := securitydomain.FindScene(env.Scene)
		switch {
		case env.CaptchaPassed:
			d.action = securitydomain.ActionPass
			d.detail = "已通过人机验证，放行"
		case !scene.SupportsCaptcha || !s.captchaAvailable(ctx, env.AppID):
			d.action = securitydomain.ActionReview
			d.detail = "需要人机验证，但该场景或应用无验证码，转人工复核"
		}
	}
	return d
}

func resolvePolicy(snap *riskSnapshot, appID int64, scene string, score int) *securitydomain.RiskAction {
	if snap == nil {
		return nil
	}
	// 应用 + 场景 → 应用 + 全部 → 全平台 + 场景 → 全平台 + 全部
	type tier struct {
		app   bool
		scene string
	}
	tiers := []tier{{true, scene}, {true, securitydomain.SceneAll}, {false, scene}, {false, securitydomain.SceneAll}}
	for _, t := range tiers {
		found := false
		for i := range snap.actions {
			a := &snap.actions[i]
			if !a.IsActive || a.Scene != t.scene {
				continue
			}
			if t.app != (a.AppID != nil) || (t.app && *a.AppID != appID) {
				continue
			}
			found = true
			if score >= a.MinScore && (a.MaxScore == nil || score <= *a.MaxScore) {
				return a
			}
		}
		// 某一层配了策略就由这一层说了算：应用配了 0–100 分各档，不该在区间外又掉到全平台策略上
		if found {
			return nil
		}
	}
	return nil
}

// EvaluateRisk 对一次业务请求做风险评估。
//
// **判定同步、留痕异步**：调用方只等结论；评估记录、设备档案、IP 计数、规则与名单命中计数
// 在后台落库，失败不反噬业务请求。唯一的例外是 ban：封禁要当场落到对象上，
// 否则同一个人下一次请求还会进来。
func (s *RiskService) EvaluateRisk(ctx context.Context, req securitydomain.RiskEvalRequest) (*securitydomain.RiskEvalResult, error) {
	startedAt := timeutil.Now()
	if !req.CaptchaPassed && riskCaptchaPassed(ctx) {
		req.CaptchaPassed = true
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("装载风控规则失败: %w", err)
	}
	env := s.buildEvalEnv(ctx, req, false)
	d := s.decide(ctx, snap, snap.rulesFor(env.AppID, env.Scene), env)
	latency := int(timeutil.Since(startedAt).Milliseconds())

	assessment := assessmentFrom(req, env, d, latency)
	result := &securitydomain.RiskEvalResult{
		TotalScore: d.total, RiskLevel: d.level, MatchedRules: d.matched, DimensionScores: d.dimensions,
		ListHits: d.listHits, DecisionBy: d.decisionBy, Action: d.action, ActionDetail: d.detail, LatencyMS: latency,
	}

	if d.action == securitydomain.ActionBan {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		stored, err := s.pg.CreateRiskAssessment(writeCtx, assessment)
		if err != nil {
			s.logWarn("写入风险评估记录失败", zap.Error(err))
		}
		var assessmentID *int64
		if stored != nil {
			assessmentID = &stored.ID
			result.AssessmentID = stored.ID
		}
		result.Enforcement = s.enforce(writeCtx, enforceRequest{
			AssessmentID: assessmentID, AppID: env.AppID, UserID: env.UserID, Account: env.Account,
			IP: env.IP, DeviceID: env.DeviceID, Targets: d.banTargets, Kind: securitydomain.ListKindBlock,
			Duration: d.banSeconds, Source: "auto", OperatorName: "风控中心",
			Reason: fmt.Sprintf("风控自动封禁：%s 场景 %d 分", sceneLabel(env.Scene), d.total),
		})
		if assessmentID != nil {
			if err := s.pg.AppendAssessmentEnforcement(writeCtx, *assessmentID, result.Enforcement); err != nil {
				s.logWarn("写入处置留痕失败", zap.Error(err))
			}
		}
		s.invalidateSnapshot()
		s.persistSideEffects(context.WithoutCancel(ctx), assessment, env, d)
		return result, nil
	}

	// 留痕经 JetStream 可靠地异步写库（risk_trail.go）：发布拿到确认才返回，拿不到时直接写库
	s.trail.record(ctx, riskTrailEventOf(assessment, env, d))
	return result, nil
}

func assessmentFrom(req securitydomain.RiskEvalRequest, env *RiskEvalEnv, d riskDecision, latency int) securitydomain.RiskAssessment {
	a := securitydomain.RiskAssessment{
		Scene: req.Scene, AppID: req.AppID, UserID: req.UserID, IdentityID: req.IdentityID,
		Account: env.Account, IP: env.IP, DeviceID: env.DeviceID, UserAgent: req.UserAgent, Country: env.GeoCountry,
		TotalScore: d.total, RiskLevel: d.level, MatchedRules: d.matched, EvalContext: env.Snapshot(),
		DimensionScores: d.dimensions, ListHits: d.listHits, DecisionBy: d.decisionBy,
		Enforcement: []securitydomain.Enforcement{}, LatencyMS: latency, Action: d.action, ActionDetail: d.detail,
	}
	// 登录时只带了登录名，引擎认出了是谁 —— 留痕挂到这个用户上，账号画像才连得起来
	if a.UserID == nil && env.UserID > 0 {
		uid := env.UserID
		a.UserID = &uid
	}
	return a
}

// persistSideEffects 设备档案、IP 计数、规则与名单命中计数、账号跨请求事实
func (s *RiskService) persistSideEffects(ctx context.Context, assessment securitydomain.RiskAssessment, env *RiskEvalEnv, d riskDecision) {
	if assessment.DeviceID != "" {
		fingerprint := map[string]any{
			"uaBrowser": env.UABrowser, "uaBrowserVer": env.UABrowserVersion, "uaOs": env.UAOS,
			"uaOsVersion": env.UAOSVersion, "uaDevice": env.UADevice, "uaDeviceClass": env.UADeviceClass,
			"lastScene": assessment.Scene, "lastCountry": env.GeoCountry,
		}
		if _, err := s.pg.TouchDeviceFingerprint(ctx, securitydomain.DeviceFingerprint{
			DeviceID: assessment.DeviceID, UserID: assessment.UserID, AppID: assessment.AppID,
			Fingerprint: fingerprint, LastIP: assessment.IP, UserAgent: assessment.UserAgent,
		}); err != nil {
			s.logWarn("更新设备指纹失败", zap.String("deviceId", assessment.DeviceID), zap.Error(err))
		}
	}
	if assessment.IP != "" {
		if err := s.pg.TouchIPRiskCounters(ctx, assessment.IP, securitydomain.IsRejection(assessment.Action),
			env.GeoCountry, env.GeoRegion, env.IPISP, env.IPASN); err != nil {
			s.logWarn("更新 IP 计数失败", zap.String("ip", assessment.IP), zap.Error(err))
		}
	}
	if len(d.matched) > 0 {
		ids := make([]int64, 0, len(d.matched))
		for _, rule := range d.matched {
			if rule.RuleID > 0 {
				ids = append(ids, rule.RuleID)
			}
		}
		if err := s.pg.BumpRuleHits(ctx, ids); err != nil {
			s.logWarn("更新规则命中计数失败", zap.Error(err))
		}
	}
	if len(d.listEntries) > 0 {
		ids := make([]int64, 0, len(d.listEntries))
		for _, entry := range d.listEntries {
			ids = append(ids, entry.ID)
		}
		if err := s.pg.BumpRiskListHits(ctx, ids); err != nil {
			s.logWarn("更新名单命中计数失败", zap.Error(err))
		}
	}
	s.rememberAccountFacts(ctx, env, d.level)
}

// SimulateRisk 无副作用地跑一遍评估：不写库、不动计数，允许覆写环境变量与试跑草稿规则。
func (s *RiskService) SimulateRisk(ctx context.Context, input securitydomain.SimulateInput) (*securitydomain.RiskEvalResult, error) {
	scene := strings.TrimSpace(input.Scene)
	if scene == "" {
		scene = securitydomain.SceneLogin
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	appID := int64(0)
	if input.AppID != nil {
		appID = *input.AppID
	}

	var rules []securitydomain.RiskRule
	if len(input.RuleIDs) > 0 {
		// 只跑指定规则（规则详情页的「试一下这条」），停用的也跑
		all, err := s.pg.ListRiskRules(ctx, pgRiskScope("", nil))
		if err != nil {
			return nil, err
		}
		for _, rule := range all {
			if slices.Contains(input.RuleIDs, rule.ID) {
				rule.IsActive = true
				rules = append(rules, rule)
			}
		}
	} else {
		rules = snap.rulesFor(appID, scene)
	}
	if input.Draft != nil {
		draft := *input.Draft
		if draft.Name == "" {
			draft.Name = "（草稿规则）"
		}
		draft.IsActive = true
		if draft.Mode == "" {
			draft.Mode = securitydomain.RuleModeEnforce
		}
		draft.Dimension = securitydomain.ConditionDimension(draft.ConditionType, draft.ConditionData)
		rules = append(rules, draft)
	}

	env := s.buildEvalEnv(ctx, securitydomain.RiskEvalRequest{
		Scene: scene, AppID: input.AppID, UserID: input.UserID, IP: input.IP, DeviceID: input.DeviceID,
		UserAgent: input.UserAgent, Account: input.Account, CaptchaPassed: input.CaptchaPassed,
	}, true)
	applyEnvOverrides(env, input.Overrides)
	d := s.decide(ctx, snap, rules, env)
	return decisionResult(d, env), nil
}

// ReplayAssessment 用一条历史记录的上下文快照重跑当前规则集。
func (s *RiskService) ReplayAssessment(ctx context.Context, id int64) (*securitydomain.RiskEvalResult, error) {
	assessment, err := s.pg.GetRiskAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	if assessment == nil {
		return nil, fmt.Errorf("评估记录不存在")
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	env := riskEnvFromSnapshot(assessment.EvalContext, timeutil.Now())
	if env.Scene == "" {
		env.Scene = assessment.Scene
	}
	// 名单命中由决策重新计算，快照里的观察名单标记不能带进来
	env.ListWatch = false
	env.ListWatchTypes = []string{}
	d := s.decide(ctx, snap, snap.rulesFor(env.AppID, env.Scene), env)
	return decisionResult(d, env), nil
}

func decisionResult(d riskDecision, env *RiskEvalEnv) *securitydomain.RiskEvalResult {
	return &securitydomain.RiskEvalResult{
		TotalScore: d.total, RiskLevel: d.level, MatchedRules: d.matched, DimensionScores: d.dimensions,
		ListHits: d.listHits, DecisionBy: d.decisionBy, Action: d.action, ActionDetail: d.detail,
		EvalContext: env.AsMap(), EvaluatedRules: d.evaluations,
	}
}

// evaluateRules 逐条评估并返回**全部**规则的轨迹（含未命中与出错的）。
func (s *RiskService) evaluateRules(rules []securitydomain.RiskRule, env *RiskEvalEnv) []securitydomain.RuleEvaluation {
	out := make([]securitydomain.RuleEvaluation, 0, len(rules))
	for _, rule := range rules {
		hit, reason, err := s.evaluateCondition(rule, env)
		mode := rule.Mode
		if mode == "" {
			mode = securitydomain.RuleModeEnforce
		}
		dimension := rule.Dimension
		if dimension == "" {
			dimension = securitydomain.ConditionDimension(rule.ConditionType, rule.ConditionData)
		}
		item := securitydomain.RuleEvaluation{
			RuleID: rule.ID, RuleName: rule.Name, ConditionType: rule.ConditionType, Dimension: dimension,
			Score: rule.Score, Priority: rule.Priority, Mode: mode, Decision: rule.Decision, Hit: hit, Reason: reason,
		}
		if err != nil {
			item.Error = err.Error()
			item.Hit = false
			s.logWarn("规则评估失败", zap.String("rule", rule.Name), zap.String("type", rule.ConditionType), zap.Error(err))
		}
		out = append(out, item)
	}
	return out
}

// summarizeEvaluations 计分命中（观察模式不计分）的总分与留痕
func summarizeEvaluations(evaluations []securitydomain.RuleEvaluation) (int, []securitydomain.MatchedRule) {
	total := 0
	matched := make([]securitydomain.MatchedRule, 0, len(evaluations))
	for _, item := range evaluations {
		if !item.Hit {
			continue
		}
		observe := item.Mode == securitydomain.RuleModeObserve
		if !observe {
			total += item.Score
		}
		matched = append(matched, securitydomain.MatchedRule{
			RuleID: item.RuleID, RuleName: item.RuleName, ConditionType: item.ConditionType, Dimension: item.Dimension,
			Score: item.Score, Reason: item.Reason, Observe: observe, Decision: item.Decision,
		})
	}
	return total, matched
}

func catalogLabel(entries []securitydomain.CatalogEntry, value string) string {
	for _, e := range entries {
		if e.Value == value {
			return e.Label
		}
	}
	return value
}

func actionLabel(action string) string { return catalogLabel(securitydomain.ActionCatalog(), action) }

func sceneLabel(scene string) string {
	if entry, ok := securitydomain.FindScene(scene); ok {
		return entry.Label
	}
	if scene == securitydomain.SceneAll {
		return "全部"
	}
	return scene
}

func listTypeLabel(listType string) string {
	for _, e := range securitydomain.ListTypeCatalogEntries() {
		if e.Value == listType {
			return e.Label
		}
	}
	return listType
}

func humanDuration(seconds int) string {
	switch {
	case seconds <= 0:
		return "永久"
	case seconds%86400 == 0:
		return fmt.Sprintf("%d 天", seconds/86400)
	case seconds%3600 == 0:
		return fmt.Sprintf("%d 小时", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("%d 分钟", seconds/60)
	default:
		return fmt.Sprintf("%d 秒", seconds)
	}
}
