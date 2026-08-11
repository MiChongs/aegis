package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/timeutil"

	redisrate "github.com/go-redis/redis_rate/v10"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// RiskService 风控中心业务服务
type RiskService struct {
	mu             sync.RWMutex
	log            *zap.Logger
	pg             *pgrepo.Repository
	redis          *redislib.Client
	location       *LocationService
	keyPrefix      string
	cfg            config.RiskConfig
	rateLimiter    *redisrate.Limiter
	ipProvider     IPReputationProvider
	ipLookupFlight singleflight.Group
}

// velocityWindow 账号扩散基数统计的窗口。
// 短于登录爆破的典型节奏就统计不到东西，长于一天则昨天的正常登录会污染今天的判定。
const velocityWindow = 6 * time.Hour

// NewRiskService 创建风控服务
func NewRiskService(cfg config.RiskConfig, log *zap.Logger, pg *pgrepo.Repository, redis *redislib.Client, keyPrefix string) *RiskService {
	service := &RiskService{
		log:       log,
		pg:        pg,
		redis:     redis,
		keyPrefix: keyPrefix,
		cfg:       cfg,
	}
	if redis != nil {
		service.rateLimiter = redisrate.NewLimiter(redis)
	}
	service.applyConfig(cfg)
	return service
}

// SetLocationService 注入地理定位服务。
// 归属地此前只能从外部 IP 情报源拿，未采购情报源时 geo_* 系列变量恒为空 ——
// 那意味着「归属地异常」这类规则配了也永不命中。本地 mmdb 是免费且离线的兜底。
func (s *RiskService) SetLocationService(location *LocationService) {
	s.mu.Lock()
	s.location = location
	s.mu.Unlock()
}

func (s *RiskService) Reload(cfg config.RiskConfig) {
	s.applyConfig(cfg)
}

func (s *RiskService) applyConfig(cfg config.RiskConfig) {
	provider := buildIPReputationProvider(cfg.IPReputation, s.log)
	s.mu.Lock()
	s.cfg = cfg
	s.ipProvider = provider
	s.mu.Unlock()
}

type riskRuntime struct {
	cfg      config.RiskConfig
	provider IPReputationProvider
	location *LocationService
}

func (s *RiskService) runtime() riskRuntime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return riskRuntime{cfg: s.cfg, provider: s.ipProvider, location: s.location}
}

func buildIPReputationProvider(cfg config.RiskIPReputationConfig, log *zap.Logger) IPReputationProvider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "none":
	case "ipqualityscore":
		provider := NewIPQualityScoreProvider(cfg)
		if provider == nil && log != nil {
			log.Warn("IP reputation provider enabled but api key is empty", zap.String("provider", "ipqualityscore"))
		}
		return wrapIPReputationProvider(provider)
	default:
		if log != nil {
			log.Warn("unknown IP reputation provider, fallback to local records only", zap.String("provider", cfg.Provider))
		}
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  核心评估
// ════════════════════════════════════════════════════════════

// EvaluateRisk 对请求执行风险评估。
//
// 这条路径挂在登录 / 注册的主链路上，因此**判定同步、留痕异步**：
// 调用方只等「命中了什么、该怎么处置」，评估记录、设备档案、IP 计数、
// 规则命中计数这四件事在后台落库。留痕失败绝不反噬业务请求 ——
// 让一次写库抖动把用户的登录挡在门外是本末倒置。
func (s *RiskService) EvaluateRisk(ctx context.Context, req securitydomain.RiskEvalRequest) (*securitydomain.RiskEvalResult, error) {
	startedAt := timeutil.Now()

	rules, err := s.pg.GetActiveRulesByScene(ctx, req.Scene)
	if err != nil {
		return nil, fmt.Errorf("获取活跃规则失败: %w", err)
	}

	env := s.buildEvalEnv(ctx, req)
	evaluations := s.evaluateRules(rules, env)

	totalScore, matched := summarizeEvaluations(evaluations)
	riskLevel := securitydomain.ScoreToLevel(totalScore)
	action, actionDetail := s.resolveAction(ctx, req.Scene, totalScore)
	latency := int(timeutil.Since(startedAt).Milliseconds())

	result := &securitydomain.RiskEvalResult{
		TotalScore:   totalScore,
		RiskLevel:    riskLevel,
		MatchedRules: matched,
		Action:       action,
		ActionDetail: actionDetail,
		LatencyMS:    latency,
	}

	assessment := securitydomain.RiskAssessment{
		Scene:        req.Scene,
		AppID:        req.AppID,
		UserID:       req.UserID,
		IdentityID:   req.IdentityID,
		Account:      env.Account,
		IP:           req.IP,
		DeviceID:     req.DeviceID,
		UserAgent:    req.UserAgent,
		Country:      env.GeoCountry,
		TotalScore:   totalScore,
		RiskLevel:    riskLevel,
		MatchedRules: matched,
		EvalContext:  env.Snapshot(),
		LatencyMS:    latency,
		Action:       action,
		ActionDetail: actionDetail,
	}
	s.persistAsync(ctx, assessment, env, matched)

	return result, nil
}

// persistAsync 把一次评估产生的全部留痕挪到请求生命周期之外。
// 用 WithoutCancel 是因为请求返回后 ctx 立刻被取消 —— 直接沿用会让
// 「评估记录写了一半」成为常态，而这些记录正是事后复盘的唯一依据。
func (s *RiskService) persistAsync(ctx context.Context, assessment securitydomain.RiskAssessment, env *RiskEvalEnv, matched []securitydomain.MatchedRule) {
	detached := context.WithoutCancel(ctx)
	go func() {
		writeCtx, cancel := context.WithTimeout(detached, 5*time.Second)
		defer cancel()
		s.persistEvaluation(writeCtx, assessment, env, matched)
	}()
}

func (s *RiskService) persistEvaluation(ctx context.Context, assessment securitydomain.RiskAssessment, env *RiskEvalEnv, matched []securitydomain.MatchedRule) {
	if _, err := s.pg.CreateRiskAssessment(ctx, assessment); err != nil {
		s.logWarn("写入风险评估记录失败", zap.Error(err))
	}

	// 设备档案：重构前这张表**从来没有被写过**，只被 device_new 规则读。
	// 于是 device_age_hours 恒为 0，「新设备」规则对每一个请求都命中，
	// 而控制台的「设备指纹」页永远是空的。写入点就在这里。
	if assessment.DeviceID != "" {
		fingerprint := map[string]any{
			"uaBrowser":     env.UABrowser,
			"uaBrowserVer":  env.UABrowserVersion,
			"uaOs":          env.UAOS,
			"uaOsVersion":   env.UAOSVersion,
			"uaDevice":      env.UADevice,
			"uaDeviceClass": env.UADeviceClass,
			"lastScene":     assessment.Scene,
			"lastCountry":   env.GeoCountry,
		}
		if _, err := s.pg.TouchDeviceFingerprint(ctx, securitydomain.DeviceFingerprint{
			DeviceID:    assessment.DeviceID,
			UserID:      assessment.UserID,
			AppID:       assessment.AppID,
			Fingerprint: fingerprint,
			LastIP:      assessment.IP,
			UserAgent:   assessment.UserAgent,
		}); err != nil {
			s.logWarn("更新设备指纹失败", zap.String("deviceId", assessment.DeviceID), zap.Error(err))
		}
	}

	// IP 计数：total_requests / total_blocks 此前也从未被累加过，
	// 「高风险 IP」列表上那两列恒为 0，管理员无从判断一个 IP 到底闹得凶不凶。
	blocked := assessment.Action == securitydomain.ActionBlock || assessment.Action == securitydomain.ActionBan
	if assessment.IP != "" {
		if err := s.pg.TouchIPRiskCounters(ctx, assessment.IP, blocked, env.GeoCountry, env.GeoRegion, env.IPISP, env.IPASN); err != nil {
			s.logWarn("更新 IP 计数失败", zap.String("ip", assessment.IP), zap.Error(err))
		}
	}

	// 规则命中计数：这是「这条规则到底有没有在生效」的直接答案。
	if len(matched) > 0 {
		ids := make([]int64, 0, len(matched))
		for _, rule := range matched {
			ids = append(ids, rule.RuleID)
		}
		if err := s.pg.BumpRuleHits(ctx, ids); err != nil {
			s.logWarn("更新规则命中计数失败", zap.Error(err))
		}
	}
}

// SimulateRisk 在不产生任何副作用的前提下跑一遍评估。
//
// 与线上评估的差别只有两处：不落库，且允许覆写环境变量与试跑草稿规则。
// 判定本身走同一段 evaluateRules —— 模拟器与线上各写一套判定
// 是「模拟通过、上线不中」这类问题的根源。
func (s *RiskService) SimulateRisk(ctx context.Context, input securitydomain.SimulateInput) (*securitydomain.RiskEvalResult, error) {
	scene := strings.TrimSpace(input.Scene)
	if scene == "" {
		scene = securitydomain.SceneLogin
	}

	rules, err := s.pg.ListRiskRules(ctx, scene)
	if err != nil {
		return nil, fmt.Errorf("获取规则失败: %w", err)
	}
	// 只跑指定规则（规则详情页的「试一下这条」）
	if len(input.RuleIDs) > 0 {
		filtered := make([]securitydomain.RiskRule, 0, len(input.RuleIDs))
		for _, rule := range rules {
			if slices.Contains(input.RuleIDs, rule.ID) {
				filtered = append(filtered, rule)
			}
		}
		rules = filtered
	} else {
		// 未指定时只跑启用中的，与线上一致
		active := rules[:0:0]
		for _, rule := range rules {
			if rule.IsActive {
				active = append(active, rule)
			}
		}
		rules = active
	}
	// 草稿规则：还没保存就想知道它会不会命中
	if input.Draft != nil {
		draft := *input.Draft
		if draft.Name == "" {
			draft.Name = "（草稿规则）"
		}
		draft.IsActive = true
		rules = append(rules, draft)
	}

	env := s.buildEvalEnv(ctx, securitydomain.RiskEvalRequest{
		Scene:     scene,
		AppID:     input.AppID,
		IP:        input.IP,
		DeviceID:  input.DeviceID,
		UserAgent: input.UserAgent,
		Extra:     map[string]any{"account": input.Account},
	})
	applyEnvOverrides(env, input.Overrides)

	evaluations := s.evaluateRules(rules, env)
	totalScore, matched := summarizeEvaluations(evaluations)
	riskLevel := securitydomain.ScoreToLevel(totalScore)
	action, actionDetail := s.resolveAction(ctx, scene, totalScore)

	return &securitydomain.RiskEvalResult{
		TotalScore:     totalScore,
		RiskLevel:      riskLevel,
		MatchedRules:   matched,
		Action:         action,
		ActionDetail:   actionDetail,
		EvalContext:    env.AsMap(),
		EvaluatedRules: evaluations,
	}, nil
}

// ReplayAssessment 用一条历史评估记录的上下文快照重跑当前规则集。
// 「改完规则之后，那批漏掉的请求现在会不会被拦下来」是调规则时最想知道的事，
// 没有这条路径就只能等下一次真实攻击。
func (s *RiskService) ReplayAssessment(ctx context.Context, id int64) (*securitydomain.RiskEvalResult, error) {
	assessment, err := s.pg.GetRiskAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	if assessment == nil {
		return nil, fmt.Errorf("评估记录不存在")
	}

	rules, err := s.pg.GetActiveRulesByScene(ctx, assessment.Scene)
	if err != nil {
		return nil, fmt.Errorf("获取活跃规则失败: %w", err)
	}

	env := riskEnvFromSnapshot(assessment.EvalContext, timeutil.Now())
	evaluations := s.evaluateRules(rules, env)
	totalScore, matched := summarizeEvaluations(evaluations)
	action, actionDetail := s.resolveAction(ctx, assessment.Scene, totalScore)

	return &securitydomain.RiskEvalResult{
		TotalScore:     totalScore,
		RiskLevel:      securitydomain.ScoreToLevel(totalScore),
		MatchedRules:   matched,
		Action:         action,
		ActionDetail:   actionDetail,
		EvalContext:    env.AsMap(),
		EvaluatedRules: evaluations,
	}, nil
}

// evaluateRules 逐条评估并返回**全部**规则的轨迹（含未命中与出错的）。
// 只返回命中项等于让排查的人猜「另外几条为什么没中」。
func (s *RiskService) evaluateRules(rules []securitydomain.RiskRule, env *RiskEvalEnv) []securitydomain.RuleEvaluation {
	out := make([]securitydomain.RuleEvaluation, 0, len(rules))
	for _, rule := range rules {
		hit, reason, err := s.evaluateCondition(rule, env)
		item := securitydomain.RuleEvaluation{
			RuleID:        rule.ID,
			RuleName:      rule.Name,
			ConditionType: rule.ConditionType,
			Score:         rule.Score,
			Priority:      rule.Priority,
			Hit:           hit,
			Reason:        reason,
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

func summarizeEvaluations(evaluations []securitydomain.RuleEvaluation) (int, []securitydomain.MatchedRule) {
	total := 0
	matched := make([]securitydomain.MatchedRule, 0, len(evaluations))
	for _, item := range evaluations {
		if !item.Hit {
			continue
		}
		total += item.Score
		matched = append(matched, securitydomain.MatchedRule{
			RuleID:        item.RuleID,
			RuleName:      item.RuleName,
			ConditionType: item.ConditionType,
			Score:         item.Score,
			Reason:        item.Reason,
		})
	}
	return total, matched
}

// ════════════════════════════════════════════════════════════
//  条件判定
//
//  每一种条件类型在这里都必须有分支，且返回**人类可读的判据**。
//  只回 true/false 的引擎在事后复盘时提供不了任何信息 ——
//  「命中了 IP 高频」和「命中了 IP 高频：当前 312 次 > 阈值 100」
//  是两种完全不同的可运维性。
// ════════════════════════════════════════════════════════════

func (s *RiskService) evaluateCondition(rule securitydomain.RiskRule, env *RiskEvalEnv) (bool, string, error) {
	data := rule.ConditionData
	switch rule.ConditionType {

	case securitydomain.CondIPFrequency:
		threshold := int64(numberOf(data["threshold"], 100))
		count := requestCountFor(env, stringOf(data["dimension"], "ip"))
		if count > threshold {
			return true, fmt.Sprintf("窗口内请求 %d 次 > 阈值 %d", count, threshold), nil
		}
		return false, fmt.Sprintf("窗口内请求 %d 次 ≤ 阈值 %d", count, threshold), nil

	case securitydomain.CondRateLimited:
		dimension := stringOf(data["dimension"], "ip")
		if rateLimitedFor(env, dimension) {
			return true, fmt.Sprintf("%s 维度已触发平台限流阈值", dimension), nil
		}
		return false, fmt.Sprintf("%s 维度未触发限流", dimension), nil

	case securitydomain.CondAccountVelocity:
		threshold := int64(numberOf(data["threshold"], 5))
		dimension := stringOf(data["dimension"], "ip")
		seen := env.IPAccountsSeen
		if dimension == "device" {
			seen = env.DeviceAccountsSeen
		}
		if seen >= threshold {
			return true, fmt.Sprintf("%s 维度窗口内触达 %d 个不同账号 ≥ 阈值 %d", dimension, seen, threshold), nil
		}
		return false, fmt.Sprintf("%s 维度窗口内触达 %d 个不同账号 < 阈值 %d", dimension, seen, threshold), nil

	case securitydomain.CondDeviceNew:
		maxHours := numberOf(data["max_hours"], 1)
		if !env.DeviceKnown {
			// 设备完全没在档：这是最"新"的情况，但只有真的带了设备标识才算数。
			// 客户端没上报 device_id 时不该被判成新设备 —— 那是缺数据，不是风险信号。
			if env.DeviceID == "" {
				return false, "请求未携带设备标识，不参与设备判定", nil
			}
			return true, "设备首次出现", nil
		}
		if env.DeviceAgeHours < maxHours {
			return true, fmt.Sprintf("设备存续 %.1f 小时 < 阈值 %.0f 小时", env.DeviceAgeHours, maxHours), nil
		}
		return false, fmt.Sprintf("设备存续 %.1f 小时 ≥ 阈值 %.0f 小时", env.DeviceAgeHours, maxHours), nil

	case securitydomain.CondDeviceShared:
		threshold := int64(numberOf(data["threshold"], 3))
		if env.DeviceID == "" {
			return false, "请求未携带设备标识，不参与设备判定", nil
		}
		if env.DeviceAccountsSeen >= threshold {
			return true, fmt.Sprintf("该设备窗口内出现 %d 个不同账号 ≥ 阈值 %d", env.DeviceAccountsSeen, threshold), nil
		}
		return false, fmt.Sprintf("该设备窗口内出现 %d 个不同账号 < 阈值 %d", env.DeviceAccountsSeen, threshold), nil

	case securitydomain.CondUABot:
		if env.UAIsBot {
			return true, fmt.Sprintf("User-Agent 被识别为 %s", firstNonEmptyString(env.UABrowser, "机器人")), nil
		}
		return false, "User-Agent 未被识别为机器人", nil

	case securitydomain.CondUAMissing:
		minLength := int(numberOf(data["min_length"], 16))
		if env.UALength == 0 {
			return true, "请求未携带 User-Agent", nil
		}
		if env.UALength < minLength {
			return true, fmt.Sprintf("User-Agent 长度 %d < 阈值 %d", env.UALength, minLength), nil
		}
		return false, fmt.Sprintf("User-Agent 长度 %d ≥ 阈值 %d", env.UALength, minLength), nil

	case securitydomain.CondUADeviceClass:
		classes := toStringSlice(data["classes"])
		if len(classes) == 0 {
			return false, "", fmt.Errorf("未配置客户端类型")
		}
		for _, class := range classes {
			if strings.EqualFold(strings.TrimSpace(class), env.UADeviceClass) {
				return true, fmt.Sprintf("客户端类型为 %s，在名单内", env.UADeviceClass), nil
			}
		}
		return false, fmt.Sprintf("客户端类型为 %s，不在名单 [%s] 内", env.UADeviceClass, strings.Join(classes, ", ")), nil

	case securitydomain.CondIPProxy:
		includeTor := boolOf(data["include_tor"], true)
		includeDatacenter := boolOf(data["include_datacenter"], false)
		// 人工加白的 IP 直接放过：管理员的结论优先于情报源的判断。
		if env.IPTrusted {
			return false, "该 IP 已人工加白", nil
		}
		hits := make([]string, 0, 4)
		if env.IPIsProxy {
			hits = append(hits, "代理")
		}
		if env.IPIsVPN {
			hits = append(hits, "VPN")
		}
		if includeTor && env.IPIsTor {
			hits = append(hits, "Tor")
		}
		if includeDatacenter && env.IPIsDatacenter {
			hits = append(hits, "机房")
		}
		if len(hits) > 0 {
			return true, "IP 判定为 " + strings.Join(hits, " / "), nil
		}
		if !env.IPKnown {
			return false, "IP 情报缺失，未做判定", nil
		}
		return false, "IP 非代理出口", nil

	case securitydomain.CondIPReputation:
		minScore := int(numberOf(data["min_score"], 75))
		if env.IPTrusted {
			return false, "该 IP 已人工加白", nil
		}
		if !env.IPKnown {
			return false, "IP 情报缺失，未做判定", nil
		}
		if env.IPRiskScore >= minScore {
			return true, fmt.Sprintf("IP 信誉分 %d ≥ 阈值 %d", env.IPRiskScore, minScore), nil
		}
		return false, fmt.Sprintf("IP 信誉分 %d < 阈值 %d", env.IPRiskScore, minScore), nil

	case securitydomain.CondIPCIDR:
		cidrs := toStringSlice(data["cidrs"])
		if len(cidrs) == 0 {
			return false, "", fmt.Errorf("未配置网段列表")
		}
		inside := ipInAnyCIDR(env.IP, cidrs)
		if boolOf(data["negate"], false) {
			if !inside {
				return true, fmt.Sprintf("IP %s 不在任何配置网段内", env.IP), nil
			}
			return false, fmt.Sprintf("IP %s 在配置网段内", env.IP), nil
		}
		if inside {
			return true, fmt.Sprintf("IP %s 落在配置网段内", env.IP), nil
		}
		return false, fmt.Sprintf("IP %s 不在配置网段内", env.IP), nil

	case securitydomain.CondGeoAnomaly:
		expected := strings.ToUpper(strings.TrimSpace(stringOf(data["expected_country"], "")))
		if expected == "" {
			return false, "", fmt.Errorf("未配置预期国家代码")
		}
		if !env.GeoKnown || env.GeoCountry == "" {
			// 情报缺失时判「不命中」。反过来会把每一个查不到归属地的请求
			// 都算成异常 —— 那不是风控，那是拒绝服务。
			return false, "归属地未知，未做判定", nil
		}
		if !strings.EqualFold(env.GeoCountry, expected) {
			return true, fmt.Sprintf("归属地 %s ≠ 预期 %s", env.GeoCountry, expected), nil
		}
		return false, fmt.Sprintf("归属地 %s 与预期一致", env.GeoCountry), nil

	case securitydomain.CondGeoCountryIn, securitydomain.CondGeoCountryNotIn:
		countries := toStringSlice(data["countries"])
		if len(countries) == 0 {
			return false, "", fmt.Errorf("未配置国家代码列表")
		}
		if !env.GeoKnown || env.GeoCountry == "" {
			return false, "归属地未知，未做判定", nil
		}
		inList := false
		for _, country := range countries {
			if strings.EqualFold(strings.TrimSpace(country), env.GeoCountry) {
				inList = true
				break
			}
		}
		if rule.ConditionType == securitydomain.CondGeoCountryIn {
			if inList {
				return true, fmt.Sprintf("归属地 %s 在名单内", env.GeoCountry), nil
			}
			return false, fmt.Sprintf("归属地 %s 不在名单内", env.GeoCountry), nil
		}
		if !inList {
			return true, fmt.Sprintf("归属地 %s 不在允许名单内", env.GeoCountry), nil
		}
		return false, fmt.Sprintf("归属地 %s 在允许名单内", env.GeoCountry), nil

	case securitydomain.CondASNIn:
		keywords := toStringSlice(data["keywords"])
		if len(keywords) == 0 {
			return false, "", fmt.Errorf("未配置 ASN / ISP 关键词")
		}
		target := strings.TrimSpace(env.IPASN + " " + env.IPISP)
		if target == "" {
			return false, "ASN / ISP 情报缺失，未做判定", nil
		}
		if containsAnyFold(target, keywords) {
			return true, fmt.Sprintf("归属 %s 命中关键词", target), nil
		}
		return false, fmt.Sprintf("归属 %s 未命中关键词", target), nil

	case securitydomain.CondTimeWindow:
		start := stringOf(data["start"], "")
		end := stringOf(data["end"], "")
		hit, err := inTimeWindow(env.evaluatedAt, start, end)
		if err != nil {
			return false, "", err
		}
		if hit {
			return true, fmt.Sprintf("请求发生在 %s–%s 时段内", start, end), nil
		}
		return false, fmt.Sprintf("请求不在 %s–%s 时段内", start, end), nil

	case securitydomain.CondCustomExpr:
		expression := strings.TrimSpace(stringOf(data["expression"], ""))
		if expression == "" {
			return false, "", fmt.Errorf("未配置表达式")
		}
		hit, err := runRiskExpr(expression, env)
		if err != nil {
			return false, "", err
		}
		if hit {
			return true, "表达式判定为真：" + truncateText(expression, 120), nil
		}
		return false, "表达式判定为假", nil

	default:
		return false, "", fmt.Errorf("未知规则条件类型：%s", rule.ConditionType)
	}
}

func requestCountFor(env *RiskEvalEnv, dimension string) int64 {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "account":
		return env.AccountRequestCount
	case "device":
		return env.DeviceRequestCount
	case "account_device":
		return env.AccountDeviceRequestCount
	default:
		return env.IPRequestCount
	}
}

func rateLimitedFor(env *RiskEvalEnv, dimension string) bool {
	switch strings.ToLower(strings.TrimSpace(dimension)) {
	case "account":
		return env.AccountRateLimited
	case "device":
		return env.DeviceRateLimited
	case "account_device":
		return env.AccountDeviceRateLimited
	default:
		return env.IPRateLimited
	}
}

// ════════════════════════════════════════════════════════════
//  环境装配
// ════════════════════════════════════════════════════════════

func (s *RiskService) buildEvalEnv(ctx context.Context, req securitydomain.RiskEvalRequest) *RiskEvalEnv {
	now := timeutil.Now()
	env := newRiskEvalEnv(now)

	env.Scene = req.Scene
	env.IP = strings.TrimSpace(req.IP)
	env.DeviceID = strings.TrimSpace(req.DeviceID)
	env.Account = strings.TrimSpace(toString(req.Extra["account"]))
	if req.AppID != nil {
		env.AppID = *req.AppID
	}
	if req.UserID != nil {
		env.UserID = *req.UserID
	}
	env.applyUserAgent(req.UserAgent)

	s.populateRateLimitEnv(ctx, req, env)
	s.populateVelocityEnv(ctx, env)
	s.populateDeviceEnv(ctx, env)
	s.populateNetworkEnv(ctx, env)

	for k, v := range req.Extra {
		if k == "account" {
			continue
		}
		env.Extra[k] = v
	}
	return env
}

// applyEnvOverrides 用模拟器传入的覆写值改写环境。
// 走 JSON 往返而不是逐字段 switch：字段名与 json 标签一致，
// 加一个新变量时这里零改动，不会出现「加了变量但模拟器覆写不到」。
func applyEnvOverrides(env *RiskEvalEnv, overrides map[string]any) {
	if len(overrides) == 0 {
		return
	}
	raw, err := json.Marshal(overrides)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, env)
	if env.Extra == nil {
		env.Extra = map[string]any{}
	}
}

func (s *RiskService) populateDeviceEnv(ctx context.Context, env *RiskEvalEnv) {
	if env.DeviceID == "" {
		return
	}
	fp, err := s.pg.GetDeviceFingerprint(ctx, env.DeviceID)
	if err != nil {
		s.logWarn("查询设备指纹失败", zap.String("deviceId", env.DeviceID), zap.Error(err))
		return
	}
	if fp == nil {
		return
	}
	env.DeviceKnown = true
	env.DeviceAgeHours = timeutil.Since(fp.FirstSeenAt).Hours()
	env.DeviceSeenCount = int64(fp.SeenCount)
	env.DeviceRiskTag = fp.RiskTag
	env.DeviceBlocked = fp.RiskTag == securitydomain.TagBlocked
}

func (s *RiskService) populateNetworkEnv(ctx context.Context, env *RiskEvalEnv) {
	if env.IP == "" {
		return
	}
	rt := s.runtime()

	// 归属地优先走本地 mmdb：离线、免费、零配额。
	// 外部情报源只补充「是不是代理 / 信誉分多少」这类 mmdb 给不出的判断。
	if rt.location != nil {
		loc := rt.location.Resolve(ctx, env.IP)
		env.GeoCountry = firstNonEmptyString(loc.CountryCode, loc.Country)
		env.GeoRegion = loc.Region
		env.GeoCity = loc.City
		env.GeoKnown = env.GeoCountry != ""
		env.IPASN = loc.Network.ASN
		env.IPISP = firstNonEmptyString(loc.ISP, loc.Network.Organization)
	}

	record := s.resolveIPRisk(ctx, env.IP)
	if record == nil {
		return
	}
	env.IPKnown = true
	env.IPIsProxy = record.IsProxy
	env.IPIsVPN = record.IsVPN
	env.IPIsTor = record.IsTor
	env.IPIsDatacenter = record.IsDatacenter
	env.IPRiskScore = record.RiskScore
	env.IPRiskTag = record.RiskTag
	env.IPTrusted = record.RiskTag == securitydomain.TagTrusted
	env.IPTotalBlocks = record.TotalBlocks
	if record.Country != "" {
		env.GeoCountry = record.Country
		env.GeoKnown = true
	}
	if record.Region != "" {
		env.GeoRegion = record.Region
	}
	env.IPASN = firstNonEmptyString(record.ASN, env.IPASN)
	env.IPISP = firstNonEmptyString(record.ISP, env.IPISP)
}

// populateVelocityEnv 统计「一个 IP / 设备在窗口内碰过多少个不同账号」。
//
// 用 Redis HyperLogLog（PFADD/PFCOUNT）而不是 Set：撞库场景下一个 IP 一小时
// 能试上万个账号，用 Set 存会把内存吃光；HLL 恒定 12KB、误差 0.81%，
// 而这里要回答的是「是不是远超阈值」而非精确计数，正是它的适用场景。
func (s *RiskService) populateVelocityEnv(ctx context.Context, env *RiskEvalEnv) {
	if s.redis == nil {
		return
	}
	window := int64(velocityWindow / time.Second)

	if env.IP != "" && env.Account != "" {
		env.IPAccountsSeen = s.trackCardinality(ctx, "ipacct", env.IP, env.Account, window)
	}
	if env.DeviceID != "" && env.Account != "" {
		env.DeviceAccountsSeen = s.trackCardinality(ctx, "devacct", env.DeviceID, env.Account, window)
	}
	if env.Account != "" && env.IP != "" {
		env.AccountIPsSeen = s.trackCardinality(ctx, "acctip", env.Account, env.IP, window)
	}
}

func (s *RiskService) trackCardinality(ctx context.Context, scope, subject, member string, ttlSeconds int64) int64 {
	key := s.riskKey("hll", scope, subject)
	pipe := s.redis.Pipeline()
	pipe.PFAdd(ctx, key, member)
	countCmd := pipe.PFCount(ctx, key)
	pipe.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		s.logWarn("基数统计失败", zap.String("scope", scope), zap.Error(err))
		return 0
	}
	return countCmd.Val()
}

func (s *RiskService) populateRateLimitEnv(ctx context.Context, req securitydomain.RiskEvalRequest, env *RiskEvalEnv) {
	rt := s.runtime()
	if s.redis == nil {
		return
	}

	if s.rateLimiter == nil || !rt.cfg.RateLimit.Enabled {
		// 未启用 GCRA 限流时退回朴素计数器，至少让 ip_frequency 规则有数可用。
		ipReqKey := s.riskKey("count", req.Scene, env.IP)
		count, err := s.redis.Incr(ctx, ipReqKey).Result()
		if err != nil {
			return
		}
		if count == 1 {
			s.redis.Expire(ctx, ipReqKey, time.Minute)
		}
		env.IPRequestCount = count
		return
	}

	appID := ""
	if req.AppID != nil {
		appID = strconv.FormatInt(*req.AppID, 10)
	}
	account := normalizeRiskDimension(env.Account)
	deviceID := normalizeRiskDimension(env.DeviceID)
	scene := normalizeRiskDimension(req.Scene)
	ip := normalizeRiskDimension(env.IP)

	if count, limited, ok := s.takeRateSample(ctx, "ip", rt.cfg.RateLimit.IPPerMinute, scene, appID, ip); ok {
		env.IPRequestCount = count
		env.IPRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account", rt.cfg.RateLimit.AccountPerMinute, scene, appID, account); ok {
		env.AccountRequestCount = count
		env.AccountRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "device", rt.cfg.RateLimit.DevicePerMinute, scene, appID, deviceID); ok {
		env.DeviceRequestCount = count
		env.DeviceRateLimited = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account_device", rt.cfg.RateLimit.AccountDevicePerMinute, scene, appID, account, deviceID); ok {
		env.AccountDeviceRequestCount = count
		env.AccountDeviceRateLimited = limited
	}
}

func (s *RiskService) takeRateSample(ctx context.Context, scope string, perMinute int, parts ...string) (int64, bool, bool) {
	if s.rateLimiter == nil || perMinute <= 0 {
		return 0, false, false
	}
	// 维度值为空时不该占用配额：所有「没带账号的请求」会被归成同一个 key，
	// 于是它们互相把对方顶到限流线上。
	if !hasNonEmptyDimension(parts) {
		return 0, false, false
	}
	key := s.riskRateLimitKey(scope, parts...)
	result, err := s.rateLimiter.Allow(ctx, key, redisrate.PerMinute(perMinute))
	if err != nil {
		s.logWarn("risk rate limit check failed", zap.String("scope", scope), zap.Error(err))
		return 0, false, false
	}
	used := int64(perMinute - result.Remaining)
	used = max(used, 0)
	used = min(used, int64(perMinute))
	return used, result.Allowed == 0, true
}

// hasNonEmptyDimension 判断限流维度里是否至少有一个实际取值。
// parts 的前两项是场景与应用 ID（恒有值），真正的维度从第三项起。
func hasNonEmptyDimension(parts []string) bool {
	if len(parts) <= 2 {
		return false
	}
	for _, part := range parts[2:] {
		if strings.TrimSpace(part) != "" {
			return true
		}
	}
	return false
}

func (s *RiskService) riskRateLimitKey(scope string, parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = normalizeRiskDimension(part); part != "" {
			filtered = append(filtered, part)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(filtered, "|")))
	return s.riskKey("rate", scope, hex.EncodeToString(sum[:]))
}

// riskKey 统一的 Redis 键构造。维度值可能很长（UA 派生的设备号）或含冒号，
// 一律哈希后再拼，避免键长失控与分隔符歧义。
func (s *RiskService) riskKey(kind, scope, subject string) string {
	if len(subject) > 48 || strings.ContainsAny(subject, ": \t\n") {
		sum := sha1.Sum([]byte(subject))
		subject = hex.EncodeToString(sum[:])
	}
	return fmt.Sprintf("%s:risk:%s:%s:%s", s.keyPrefix, kind, scope, subject)
}

// ════════════════════════════════════════════════════════════
//  IP 情报解析
// ════════════════════════════════════════════════════════════

func (s *RiskService) resolveIPRisk(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	rt := s.runtime()
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	value, err, _ := s.ipLookupFlight.Do(ip, func() (any, error) {
		if cached := s.readIPRiskCache(ctx, ip); cached != nil {
			return cached, nil
		}

		local, err := s.pg.GetIPRisk(ctx, ip)
		if err != nil {
			s.logWarn("load local ip risk failed", zap.String("ip", ip), zap.Error(err))
		}
		// 人工结论不被情报源覆盖：管理员加白 / 拉黑之后，
		// 下一次情报刷新不该把这个决定悄悄改回去。
		if local != nil && (local.Source == riskSourceManual || timeutil.Since(local.LastSeenAt) <= rt.cfg.IPReputation.CacheTTL) {
			s.writeIPRiskCache(ctx, local)
			return local, nil
		}
		if rt.provider == nil {
			return local, nil
		}

		record, err := rt.provider.Lookup(ctx, ip)
		if err != nil {
			if local != nil && rt.cfg.IPReputation.AllowStale {
				return local, nil
			}
			return nil, err
		}
		record = normalizeIPRiskRecord(ip, record)
		if record == nil {
			return local, nil
		}
		record.Source = rt.provider.Name()
		stored, upsertErr := s.pg.UpsertIPRisk(ctx, *record)
		if upsertErr != nil {
			s.logWarn("persist ip reputation failed", zap.String("ip", ip), zap.String("provider", rt.provider.Name()), zap.Error(upsertErr))
			s.writeIPRiskCache(ctx, record)
			return record, nil
		}
		s.writeIPRiskCache(ctx, stored)
		return stored, nil
	})
	if err != nil {
		s.logWarn("resolve ip reputation failed", zap.String("ip", ip), zap.Error(err))
		return nil
	}
	record, _ := value.(*securitydomain.IPRiskRecord)
	return record
}

const riskSourceManual = "manual"

func (s *RiskService) readIPRiskCache(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	rt := s.runtime()
	if s.redis == nil || rt.cfg.IPReputation.CacheTTL <= 0 {
		return nil
	}
	raw, err := s.redis.Get(ctx, s.ipRiskCacheKey(ip)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	var record securitydomain.IPRiskRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return normalizeIPRiskRecord(ip, &record)
}

func (s *RiskService) writeIPRiskCache(ctx context.Context, record *securitydomain.IPRiskRecord) {
	rt := s.runtime()
	if s.redis == nil || record == nil || rt.cfg.IPReputation.CacheTTL <= 0 {
		return
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := s.redis.Set(ctx, s.ipRiskCacheKey(record.IP), payload, rt.cfg.IPReputation.CacheTTL).Err(); err != nil {
		s.logWarn("write ip risk cache failed", zap.String("ip", record.IP), zap.Error(err))
	}
}

func (s *RiskService) invalidateIPRiskCache(ctx context.Context, ip string) {
	if s.redis == nil || strings.TrimSpace(ip) == "" {
		return
	}
	s.redis.Del(ctx, s.ipRiskCacheKey(ip))
}

func (s *RiskService) ipRiskCacheKey(ip string) string {
	return fmt.Sprintf("%s:risk:iprep:%s", s.keyPrefix, strings.TrimSpace(ip))
}

// ════════════════════════════════════════════════════════════
//  处置策略解析
// ════════════════════════════════════════════════════════════

func (s *RiskService) resolveAction(ctx context.Context, scene string, totalScore int) (string, string) {
	actions, err := s.pg.ListRiskActions(ctx, scene)
	if err != nil {
		s.logWarn("查询处置策略失败", zap.Error(err))
		return securitydomain.ActionPass, ""
	}
	for _, a := range actions {
		if !a.IsActive {
			continue
		}
		if totalScore >= a.MinScore && (a.MaxScore == nil || totalScore <= *a.MaxScore) {
			detail := a.Description
			if detail == "" {
				detail = fmt.Sprintf("总分 %d 命中策略区间", totalScore)
			}
			if a.BanDuration > 0 {
				detail = fmt.Sprintf("%s（封禁 %d 秒）", detail, a.BanDuration)
			}
			return a.Action, detail
		}
	}
	return securitydomain.ActionPass, ""
}

// ════════════════════════════════════════════════════════════
//  规则管理（带校验）
// ════════════════════════════════════════════════════════════

// ValidateRuleInput 在落库之前把规则配置检查一遍。
//
// 这是整套改造里最值钱的一段：以前一条写错的规则能顺利存进数据库，
// 之后它对每一个请求都判假、永不命中，而列表上显示「已启用」。
// 现在场景 / 条件类型 / 必填参数 / 表达式语法任一项不合法都当场拒绝。
func ValidateRuleInput(scene, conditionType string, data map[string]any) error {
	if !slices.Contains(securitydomain.SceneValues(), scene) {
		return fmt.Errorf("未知场景：%s", scene)
	}
	catalog := securitydomain.ConditionCatalogEntries()
	idx := slices.IndexFunc(catalog, func(c securitydomain.ConditionCatalog) bool { return c.Value == conditionType })
	if idx < 0 {
		return fmt.Errorf("未知条件类型：%s", conditionType)
	}
	for _, field := range catalog[idx].Fields {
		if !field.Required {
			continue
		}
		value, ok := data[field.Key]
		if !ok || isBlankValue(value) {
			return fmt.Errorf("条件参数「%s」为必填项", field.Label)
		}
		switch field.Type {
		case "number":
			number := numberOf(value, 0)
			if field.Min != nil && number < *field.Min {
				return fmt.Errorf("条件参数「%s」不得小于 %g", field.Label, *field.Min)
			}
			if field.Max != nil && number > *field.Max {
				return fmt.Errorf("条件参数「%s」不得大于 %g", field.Label, *field.Max)
			}
		case "list":
			if len(toStringSlice(value)) == 0 {
				return fmt.Errorf("条件参数「%s」至少需要一项", field.Label)
			}
		case "time":
			if _, err := timeutil.ParseLocalTimeOfDay(stringOf(value, "")); err != nil {
				return fmt.Errorf("条件参数「%s」格式应为 HH:MM", field.Label)
			}
		}
	}

	switch conditionType {
	case securitydomain.CondCustomExpr:
		if err := ValidateRiskExpression(stringOf(data["expression"], "")); err != nil {
			return fmt.Errorf("表达式无法编译：%w", err)
		}
	case securitydomain.CondIPCIDR:
		for _, cidr := range toStringSlice(data["cidrs"]) {
			if !isValidCIDR(cidr) {
				return fmt.Errorf("网段格式非法：%s", cidr)
			}
		}
	case securitydomain.CondTimeWindow:
		if _, err := inTimeWindow(timeutil.Now(), stringOf(data["start"], ""), stringOf(data["end"], "")); err != nil {
			return err
		}
	}
	return nil
}

func (s *RiskService) CreateRiskRule(ctx context.Context, input securitydomain.CreateRiskRuleInput, createdBy int64) (*securitydomain.RiskRule, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("规则名称不能为空")
	}
	if input.ConditionData == nil {
		input.ConditionData = map[string]any{}
	}
	if err := ValidateRuleInput(input.Scene, input.ConditionType, input.ConditionData); err != nil {
		return nil, err
	}
	if input.Score <= 0 {
		input.Score = 20
	}
	if input.Priority <= 0 {
		input.Priority = 100
	}
	return s.pg.CreateRiskRule(ctx, input, createdBy)
}

func (s *RiskService) UpdateRiskRule(ctx context.Context, id int64, input securitydomain.UpdateRiskRuleInput) error {
	// 部分更新也必须整体校验：只改 conditionData 而不看 conditionType，
	// 就会出现「参数换成另一种条件的形状」这种存得进去但永不命中的组合。
	current, err := s.pg.GetRiskRule(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("规则不存在")
	}
	scene := current.Scene
	if input.Scene != nil {
		scene = *input.Scene
	}
	conditionType := current.ConditionType
	if input.ConditionType != nil {
		conditionType = *input.ConditionType
	}
	data := current.ConditionData
	if input.ConditionData != nil {
		data = *input.ConditionData
	}
	if data == nil {
		data = map[string]any{}
	}
	if err := ValidateRuleInput(scene, conditionType, data); err != nil {
		return err
	}
	return s.pg.UpdateRiskRule(ctx, id, input)
}

func (s *RiskService) ListRiskRules(ctx context.Context, scene string) ([]securitydomain.RiskRule, error) {
	return s.pg.ListRiskRules(ctx, scene)
}

func (s *RiskService) GetRiskRule(ctx context.Context, id int64) (*securitydomain.RiskRule, error) {
	return s.pg.GetRiskRule(ctx, id)
}

// GetRiskRuleDetail 规则详情：定义 + 效果 + 最近命中的请求。
// 「这条规则拦了什么」必须能被直接看到，否则调分数只能靠猜。
func (s *RiskService) GetRiskRuleDetail(ctx context.Context, id int64, start, end time.Time) (*securitydomain.RiskRuleDetail, error) {
	rule, err := s.pg.GetRiskRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if rule == nil {
		return nil, nil
	}
	stats, err := s.pg.GetRuleHitStat(ctx, id, start, end)
	if err != nil {
		return nil, err
	}
	recent, _, err := s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{RuleID: id, Page: 1, PageSize: 20})
	if err != nil {
		return nil, err
	}
	series, err := s.pg.GetRuleHitSeries(ctx, id, start, end, bucketFor(start, end))
	if err != nil {
		return nil, err
	}
	return &securitydomain.RiskRuleDetail{
		Rule:        *rule,
		Stats:       stats,
		RecentHits:  recent,
		Series:      series,
		Explanation: explainRule(*rule),
	}, nil
}

// explainRule 把一条规则翻译成一句中文。
// 控制台上并排列着十几条规则时，`conditionData` 的 JSON 是看不动的。
func explainRule(rule securitydomain.RiskRule) string {
	data := rule.ConditionData
	switch rule.ConditionType {
	case securitydomain.CondIPFrequency:
		return fmt.Sprintf("当 %s 维度窗口内请求数超过 %g 次时 +%d 分", stringOf(data["dimension"], "ip"), numberOf(data["threshold"], 100), rule.Score)
	case securitydomain.CondRateLimited:
		return fmt.Sprintf("当 %s 维度触发平台限流时 +%d 分", stringOf(data["dimension"], "ip"), rule.Score)
	case securitydomain.CondAccountVelocity:
		return fmt.Sprintf("当同一 %s 触达的不同账号数达到 %g 个时 +%d 分", stringOf(data["dimension"], "ip"), numberOf(data["threshold"], 5), rule.Score)
	case securitydomain.CondDeviceNew:
		return fmt.Sprintf("当设备首次出现不足 %g 小时时 +%d 分", numberOf(data["max_hours"], 1), rule.Score)
	case securitydomain.CondDeviceShared:
		return fmt.Sprintf("当同一设备出现 %g 个以上不同账号时 +%d 分", numberOf(data["threshold"], 3), rule.Score)
	case securitydomain.CondUABot:
		return fmt.Sprintf("当客户端被识别为机器人时 +%d 分", rule.Score)
	case securitydomain.CondUAMissing:
		return fmt.Sprintf("当 User-Agent 缺失或短于 %g 字符时 +%d 分", numberOf(data["min_length"], 16), rule.Score)
	case securitydomain.CondUADeviceClass:
		return fmt.Sprintf("当客户端类型属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["classes"]), ", "), rule.Score)
	case securitydomain.CondIPProxy:
		return fmt.Sprintf("当 IP 为代理 / VPN 出口时 +%d 分", rule.Score)
	case securitydomain.CondIPReputation:
		return fmt.Sprintf("当 IP 信誉分达到 %g 时 +%d 分", numberOf(data["min_score"], 75), rule.Score)
	case securitydomain.CondIPCIDR:
		verb := "落在"
		if boolOf(data["negate"], false) {
			verb = "不落在"
		}
		return fmt.Sprintf("当 IP %s [%s] 时 +%d 分", verb, strings.Join(toStringSlice(data["cidrs"]), ", "), rule.Score)
	case securitydomain.CondGeoAnomaly:
		return fmt.Sprintf("当归属地不是 %s 时 +%d 分", stringOf(data["expected_country"], "?"), rule.Score)
	case securitydomain.CondGeoCountryIn:
		return fmt.Sprintf("当归属地属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["countries"]), ", "), rule.Score)
	case securitydomain.CondGeoCountryNotIn:
		return fmt.Sprintf("当归属地不属于 [%s] 时 +%d 分", strings.Join(toStringSlice(data["countries"]), ", "), rule.Score)
	case securitydomain.CondASNIn:
		return fmt.Sprintf("当 ASN / ISP 命中 [%s] 时 +%d 分", strings.Join(toStringSlice(data["keywords"]), ", "), rule.Score)
	case securitydomain.CondTimeWindow:
		return fmt.Sprintf("当请求发生在 %s–%s 时段内时 +%d 分", stringOf(data["start"], "?"), stringOf(data["end"], "?"), rule.Score)
	case securitydomain.CondCustomExpr:
		return fmt.Sprintf("当表达式 `%s` 为真时 +%d 分", truncateText(stringOf(data["expression"], ""), 80), rule.Score)
	default:
		return ""
	}
}

func (s *RiskService) DeleteRiskRule(ctx context.Context, id int64) error {
	return s.pg.DeleteRiskRule(ctx, id)
}

// ════════════════════════════════════════════════════════════
//  评估记录与复核
// ════════════════════════════════════════════════════════════

func (s *RiskService) ListRiskAssessments(ctx context.Context, query securitydomain.AssessmentQuery) ([]securitydomain.RiskAssessment, int64, error) {
	return s.pg.ListRiskAssessments(ctx, query)
}

func (s *RiskService) GetRiskAssessment(ctx context.Context, id int64) (*securitydomain.RiskAssessment, error) {
	return s.pg.GetRiskAssessment(ctx, id)
}

// GetRiskAssessmentDetail 组装一条评估记录的完整上下文。
// 复核人要回答的是「这次拦得对不对」，光看分数和 IP 回答不了；
// 同 IP / 同设备 / 同账号最近发生过什么，才是判断依据。
func (s *RiskService) GetRiskAssessmentDetail(ctx context.Context, id int64) (*securitydomain.RiskAssessmentDetail, error) {
	assessment, err := s.pg.GetRiskAssessment(ctx, id)
	if err != nil {
		return nil, err
	}
	if assessment == nil {
		return nil, nil
	}

	detail := &securitydomain.RiskAssessmentDetail{Assessment: *assessment}
	detail.Rules = rebuildRuleEvaluations(*assessment)

	if assessment.DeviceID != "" {
		if fp, err := s.pg.GetDeviceFingerprint(ctx, assessment.DeviceID); err == nil {
			detail.Device = fp
		}
		detail.SameDevice, _, _ = s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{DeviceID: assessment.DeviceID, Page: 1, PageSize: 10})
		detail.DevSummary, _ = s.pg.GetEntitySummary(ctx, "device", assessment.DeviceID)
	}
	if assessment.IP != "" {
		if record, err := s.pg.GetIPRisk(ctx, assessment.IP); err == nil {
			detail.IPRecord = record
		}
		detail.SameIP, _, _ = s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{IP: assessment.IP, Page: 1, PageSize: 10})
		detail.IPSummary, _ = s.pg.GetEntitySummary(ctx, "ip", assessment.IP)
	}
	if assessment.Account != "" {
		detail.SameAccount, _, _ = s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{Account: assessment.Account, Page: 1, PageSize: 10})
	}
	return detail, nil
}

// rebuildRuleEvaluations 把留痕里的命中规则还原成轨迹结构，
// 让详情页与模拟器共用同一套渲染。
func rebuildRuleEvaluations(assessment securitydomain.RiskAssessment) []securitydomain.RuleEvaluation {
	out := make([]securitydomain.RuleEvaluation, 0, len(assessment.MatchedRules))
	for _, rule := range assessment.MatchedRules {
		out = append(out, securitydomain.RuleEvaluation{
			RuleID:        rule.RuleID,
			RuleName:      rule.RuleName,
			ConditionType: rule.ConditionType,
			Score:         rule.Score,
			Hit:           true,
			Reason:        rule.Reason,
		})
	}
	return out
}

func (s *RiskService) ListPendingReviews(ctx context.Context, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	reviewed := false
	return s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{
		Action:   securitydomain.ActionReview,
		Reviewed: &reviewed,
		Page:     page,
		PageSize: limit,
	})
}

// ReviewRiskAssessment 复核一条评估记录。
//
// 拒绝时把该 IP 标成人工封禁 —— 但**只改标签，不动情报字段**。
// 重构前这里把一个只填了 IP 与分数的空记录整体 upsert 进去，
// 于是国家 / 运营商 / 请求计数全被清零：复核这个动作本身在破坏证据。
func (s *RiskService) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, result, comment string) error {
	result = strings.TrimSpace(result)
	if result != "approved" && result != "rejected" {
		return fmt.Errorf("复核结果只能是 approved 或 rejected")
	}
	if err := s.pg.ReviewRiskAssessment(ctx, id, reviewerID, result, comment); err != nil {
		return err
	}
	if result != "rejected" {
		return nil
	}

	assessment, err := s.pg.GetRiskAssessment(ctx, id)
	if err != nil || assessment == nil {
		return nil
	}
	if assessment.IP != "" {
		if err := s.pg.SetIPRiskTag(ctx, assessment.IP, securitydomain.TagBlocked, riskSourceManual,
			fmt.Sprintf("复核拒绝 #%d：%s", id, comment)); err != nil {
			s.logWarn("复核拒绝后标记 IP 失败", zap.String("ip", assessment.IP), zap.Error(err))
		} else {
			s.invalidateIPRiskCache(ctx, assessment.IP)
			s.logInfo("复核拒绝，IP 已标记封禁",
				zap.Int64("assessmentId", id), zap.String("ip", assessment.IP), zap.String("scene", assessment.Scene))
		}
	}
	if assessment.DeviceID != "" {
		if err := s.pg.SetDeviceRiskTagByDeviceID(ctx, assessment.DeviceID, securitydomain.TagBlocked); err != nil {
			s.logWarn("复核拒绝后标记设备失败", zap.String("deviceId", assessment.DeviceID), zap.Error(err))
		}
	}
	return nil
}

// PurgeAssessments 清理指定时间之前的评估记录。
// 这张表按登录量线性增长，没有清理入口时它迟早会变成库里最大的一张表。
func (s *RiskService) PurgeAssessments(ctx context.Context, before time.Time) (int64, error) {
	return s.pg.PurgeRiskAssessments(ctx, before)
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

func (s *RiskService) UpsertDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	return s.pg.TouchDeviceFingerprint(ctx, fp)
}

func (s *RiskService) GetDeviceFingerprint(ctx context.Context, deviceID string) (*securitydomain.DeviceFingerprint, error) {
	return s.pg.GetDeviceFingerprint(ctx, deviceID)
}

func (s *RiskService) ListDevices(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.DeviceFingerprint, int64, error) {
	return s.pg.ListDeviceFingerprints(ctx, query)
}

func (s *RiskService) GetDeviceDetail(ctx context.Context, deviceID string) (*securitydomain.DeviceRiskDetail, error) {
	fp, err := s.pg.GetDeviceFingerprint(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if fp == nil {
		return nil, nil
	}
	summary, _ := s.pg.GetEntitySummary(ctx, "device", deviceID)
	recent, _, _ := s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{DeviceID: deviceID, Page: 1, PageSize: 30})
	ips, accounts, _ := s.pg.ListDevicePeers(ctx, deviceID, 20)
	return &securitydomain.DeviceRiskDetail{
		Device:   *fp,
		Summary:  summary,
		Recent:   recent,
		IPs:      ips,
		Accounts: accounts,
	}, nil
}

func (s *RiskService) UpdateDeviceRiskTag(ctx context.Context, id int64, tag, note string) error {
	if !slices.ContainsFunc(securitydomain.DeviceTagCatalog(), func(e securitydomain.CatalogEntry) bool { return e.Value == tag }) {
		return fmt.Errorf("未知设备风险标签：%s", tag)
	}
	return s.pg.UpdateDeviceRiskTag(ctx, id, tag, note)
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

func (s *RiskService) UpsertIPRisk(ctx context.Context, rec securitydomain.IPRiskRecord) (*securitydomain.IPRiskRecord, error) {
	record, err := s.pg.UpsertIPRisk(ctx, rec)
	if err == nil {
		s.invalidateIPRiskCache(ctx, rec.IP)
	}
	return record, err
}

func (s *RiskService) GetIPRisk(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	return s.pg.GetIPRisk(ctx, ip)
}

func (s *RiskService) ListIPRecords(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.IPRiskRecord, int64, error) {
	return s.pg.ListIPRiskRecords(ctx, query)
}

func (s *RiskService) GetIPDetail(ctx context.Context, ip string) (*securitydomain.IPRiskDetail, error) {
	record, err := s.pg.GetIPRisk(ctx, ip)
	if err != nil {
		return nil, err
	}
	if record == nil {
		// 没有档案不等于没有数据：这个 IP 可能刚刚触发过评估但还没被情报源收录。
		// 直接 404 会让「点开列表里的一行什么都看不到」。
		record = &securitydomain.IPRiskRecord{IP: ip, RiskTag: securitydomain.TagNormal}
	}
	summary, _ := s.pg.GetEntitySummary(ctx, "ip", ip)
	recent, _, _ := s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{IP: ip, Page: 1, PageSize: 30})
	devices, accounts, _ := s.pg.ListIPPeers(ctx, ip, 20)
	return &securitydomain.IPRiskDetail{
		Record:   *record,
		Summary:  summary,
		Recent:   recent,
		Devices:  devices,
		Accounts: accounts,
	}, nil
}

// UpdateIPRiskTag 人工改写 IP 标签。source 固定为 manual，
// 之后的情报刷新会绕开这条记录，管理员的结论不会被静默改回去。
func (s *RiskService) UpdateIPRiskTag(ctx context.Context, id int64, tag, note string) error {
	if !slices.ContainsFunc(securitydomain.IPTagCatalog(), func(e securitydomain.CatalogEntry) bool { return e.Value == tag }) {
		return fmt.Errorf("未知 IP 风险标签：%s", tag)
	}
	ip, err := s.pg.UpdateIPRiskTag(ctx, id, tag, riskSourceManual, note)
	if err != nil {
		return err
	}
	s.invalidateIPRiskCache(ctx, ip)
	return nil
}

// RefreshIPReputation 强制向情报源重新拉取一次。
func (s *RiskService) RefreshIPReputation(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	rt := s.runtime()
	if rt.provider == nil {
		return nil, fmt.Errorf("未配置外部 IP 情报源")
	}
	record, err := rt.provider.Lookup(ctx, ip)
	if err != nil {
		return nil, err
	}
	record = normalizeIPRiskRecord(ip, record)
	if record == nil {
		return nil, fmt.Errorf("情报源未返回该 IP 的数据")
	}
	record.Source = rt.provider.Name()
	stored, err := s.pg.UpsertIPRisk(ctx, *record)
	if err != nil {
		return nil, err
	}
	s.invalidateIPRiskCache(ctx, ip)
	return stored, nil
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

func (s *RiskService) CreateRiskAction(ctx context.Context, input securitydomain.CreateRiskActionInput) (*securitydomain.RiskAction, error) {
	if err := validateActionInput(input.Scene, input.Action, input.MinScore, input.MaxScore, input.BanDuration); err != nil {
		return nil, err
	}
	// 区间重叠会让「到底走哪条策略」取决于 SQL 的排序，是最难排查的一类配置事故。
	existing, err := s.pg.ListRiskActions(ctx, input.Scene)
	if err != nil {
		return nil, err
	}
	for _, action := range existing {
		if scoreRangesOverlap(input.MinScore, input.MaxScore, action.MinScore, action.MaxScore) {
			return nil, fmt.Errorf("分数区间与已有策略 #%d（%d–%s）重叠", action.ID, action.MinScore, formatMaxScore(action.MaxScore))
		}
	}
	return s.pg.CreateRiskAction(ctx, input)
}

func (s *RiskService) ListRiskActions(ctx context.Context, scene string) ([]securitydomain.RiskAction, error) {
	return s.pg.ListRiskActions(ctx, scene)
}

func (s *RiskService) UpdateRiskAction(ctx context.Context, id int64, input securitydomain.UpdateRiskActionInput) error {
	current, err := s.pg.GetRiskAction(ctx, id)
	if err != nil {
		return err
	}
	if current == nil {
		return fmt.Errorf("处置策略不存在")
	}
	minScore := current.MinScore
	if input.MinScore != nil {
		minScore = *input.MinScore
	}
	maxScore := current.MaxScore
	if input.MaxScore != nil {
		maxScore = *input.MaxScore
	}
	action := current.Action
	if input.Action != nil {
		action = *input.Action
	}
	banDuration := current.BanDuration
	if input.BanDuration != nil {
		banDuration = *input.BanDuration
	}
	if err := validateActionInput(current.Scene, action, minScore, maxScore, banDuration); err != nil {
		return err
	}
	existing, err := s.pg.ListRiskActions(ctx, current.Scene)
	if err != nil {
		return err
	}
	for _, other := range existing {
		if other.ID == id {
			continue
		}
		if scoreRangesOverlap(minScore, maxScore, other.MinScore, other.MaxScore) {
			return fmt.Errorf("分数区间与已有策略 #%d（%d–%s）重叠", other.ID, other.MinScore, formatMaxScore(other.MaxScore))
		}
	}
	return s.pg.UpdateRiskAction(ctx, id, input)
}

func (s *RiskService) DeleteRiskAction(ctx context.Context, id int64) error {
	return s.pg.DeleteRiskAction(ctx, id)
}

func validateActionInput(scene, action string, minScore int, maxScore *int, banDuration int) error {
	if !slices.Contains(securitydomain.SceneValues(), scene) {
		return fmt.Errorf("未知场景：%s", scene)
	}
	if !slices.Contains(securitydomain.ActionValues(), action) {
		return fmt.Errorf("未知处置动作：%s", action)
	}
	if minScore < 0 {
		return fmt.Errorf("最低分数不能为负")
	}
	if maxScore != nil && *maxScore < minScore {
		return fmt.Errorf("最高分数不能小于最低分数")
	}
	if action == securitydomain.ActionBan && banDuration <= 0 {
		// 封禁 0 秒等于没封。让它存进去只会制造一条看起来生效、实际不生效的策略。
		return fmt.Errorf("封禁动作必须指定大于 0 的封禁时长")
	}
	if banDuration < 0 {
		return fmt.Errorf("封禁时长不能为负")
	}
	return nil
}

func scoreRangesOverlap(aMin int, aMax *int, bMin int, bMax *int) bool {
	aHigh := 1 << 30
	if aMax != nil {
		aHigh = *aMax
	}
	bHigh := 1 << 30
	if bMax != nil {
		bHigh = *bMax
	}
	return aMin <= bHigh && bMin <= aHigh
}

func formatMaxScore(maxScore *int) string {
	if maxScore == nil {
		return "∞"
	}
	return strconv.Itoa(*maxScore)
}

// ════════════════════════════════════════════════════════════
//  大盘与目录
// ════════════════════════════════════════════════════════════

// GetRiskDashboard 组装风控大盘。
func (s *RiskService) GetRiskDashboard(ctx context.Context, start, end time.Time) (*securitydomain.RiskDashboard, error) {
	bucket := bucketFor(start, end)
	dash, err := s.pg.GetRiskDashboard(ctx, start, end, bucket)
	if err != nil {
		return nil, err
	}
	dash.Engine = s.engineStatus(ctx)
	return dash, nil
}

// bucketFor 按跨度选聚合粒度。
// 跨度超过 3 天还按小时聚合会给出几百个点，折线图上只剩噪声。
func bucketFor(start, end time.Time) string {
	if end.Sub(start) > 3*24*time.Hour {
		return "day"
	}
	return "hour"
}

// engineStatus 回答「引擎现在到底在跑什么」。
// 大盘全是 0 有两种截然不同的原因 —— 真没风险，或者根本没有规则在跑。
// 不显式说出来，管理员会把后者当成前者。
func (s *RiskService) engineStatus(ctx context.Context) securitydomain.EngineStatus {
	rt := s.runtime()
	status := securitydomain.EngineStatus{
		IPProvider:      strings.ToLower(strings.TrimSpace(rt.cfg.IPReputation.Provider)),
		IPProviderReady: rt.provider != nil,
		RateLimitOn:     rt.cfg.RateLimit.Enabled && s.rateLimiter != nil,
		CacheTTLSeconds: int(rt.cfg.IPReputation.CacheTTL / time.Second),
	}
	if status.IPProvider == "" {
		status.IPProvider = "none"
	}

	rules, err := s.pg.ListRiskRules(ctx, "")
	if err == nil {
		covered := map[string]bool{}
		for _, rule := range rules {
			status.TotalRules++
			if rule.IsActive {
				status.ActiveRules++
				covered[rule.Scene] = true
			}
		}
		for _, scene := range securitydomain.SceneValues() {
			if covered[scene] {
				status.ScenesCovered = append(status.ScenesCovered, scene)
			} else {
				status.ScenesUncovered = append(status.ScenesUncovered, scene)
			}
		}
	}
	actions, err := s.pg.ListRiskActions(ctx, "")
	if err == nil {
		for _, action := range actions {
			status.TotalActions++
			if action.IsActive {
				status.ActiveActions++
			}
		}
	}
	return status
}

// Metadata 返回风控中心的自描述目录。
func (s *RiskService) Metadata() securitydomain.RiskMetadata {
	return securitydomain.BuildRiskMetadata()
}

// ValidateExpression 校验表达式，供控制台边写边提示。
func (s *RiskService) ValidateExpression(expression string) securitydomain.ExprValidation {
	if err := ValidateRiskExpression(expression); err != nil {
		return securitydomain.ExprValidation{Valid: false, Error: err.Error(), Message: "表达式无法编译"}
	}
	return securitydomain.ExprValidation{Valid: true, Message: "表达式可用"}
}

// ════════════════════════════════════════════════════════════
//  小工具
// ════════════════════════════════════════════════════════════

func (s *RiskService) logWarn(msg string, fields ...zap.Field) {
	if s.log != nil {
		s.log.Warn(msg, fields...)
	}
}

func (s *RiskService) logInfo(msg string, fields ...zap.Field) {
	if s.log != nil {
		s.log.Info(msg, fields...)
	}
}

func normalizeRiskDimension(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func toString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}

// numberOf 从 JSONB 反序列化出来的任意数值形态里取 float64。
// conditionData 存的是 JSONB，回来的数字一律是 float64；
// 但表达式覆写、测试构造的 map 可能给 int —— 两种都要认。
func numberOf(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func stringOf(value any, fallback string) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

func boolOf(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(typed)); err == nil {
			return parsed
		}
	}
	return fallback
}

func isBlankValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

func truncateText(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
