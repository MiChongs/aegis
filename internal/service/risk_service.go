package service

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/timeutil"

	"github.com/expr-lang/expr"
	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/mssola/useragent"
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
	keyPrefix      string
	cfg            config.RiskConfig
	rateLimiter    *redisrate.Limiter
	ipProvider     IPReputationProvider
	ipLookupFlight singleflight.Group
}

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

func (s *RiskService) runtime() (config.RiskConfig, IPReputationProvider) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg, s.ipProvider
}

func buildIPReputationProvider(cfg config.RiskIPReputationConfig, log *zap.Logger) IPReputationProvider {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "none":
	case "ipqualityscore":
		provider := NewIPQualityScoreProvider(cfg)
		if provider == nil && log != nil {
			log.Warn("IP reputation provider enabled but api key is empty", zap.String("provider", "ipqualityscore"))
		}
		return provider
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

// EvaluateRisk 对请求执行风险评估
func (s *RiskService) EvaluateRisk(ctx context.Context, req securitydomain.RiskEvalRequest) (*securitydomain.RiskEvalResult, error) {
	rules, err := s.pg.GetActiveRulesByScene(ctx, req.Scene)
	if err != nil {
		return nil, fmt.Errorf("获取活跃规则失败: %w", err)
	}

	// 构建评估环境变量
	env := s.buildEvalEnv(ctx, req)

	// 逐条规则评估
	var totalScore int
	var matched []securitydomain.MatchedRule
	for _, rule := range rules {
		hit := s.evaluateCondition(rule, env)
		if hit {
			totalScore += rule.Score
			matched = append(matched, securitydomain.MatchedRule{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Score:    rule.Score,
			})
		}
	}

	// 分数→等级映射
	riskLevel := scoreToLevel(totalScore)

	// 查询处置策略
	action, actionDetail := s.resolveAction(ctx, req.Scene, totalScore)

	// 写入评估记录
	assessment := securitydomain.RiskAssessment{
		Scene:        req.Scene,
		AppID:        req.AppID,
		UserID:       req.UserID,
		IdentityID:   req.IdentityID,
		IP:           req.IP,
		DeviceID:     req.DeviceID,
		TotalScore:   totalScore,
		RiskLevel:    riskLevel,
		MatchedRules: matched,
		Action:       action,
		ActionDetail: actionDetail,
	}
	if _, err := s.pg.CreateRiskAssessment(ctx, assessment); err != nil {
		s.log.Error("写入风险评估记录失败", zap.Error(err))
	}

	return &securitydomain.RiskEvalResult{
		TotalScore:   totalScore,
		RiskLevel:    riskLevel,
		MatchedRules: matched,
		Action:       action,
		ActionDetail: actionDetail,
	}, nil
}

// SimulateRule 模拟规则评估（不写入记录）
func (s *RiskService) SimulateRule(ctx context.Context, ruleID int64, input securitydomain.SimulateInput) (*securitydomain.RiskEvalResult, error) {
	rules, err := s.pg.GetActiveRulesByScene(ctx, input.Scene)
	if err != nil {
		return nil, fmt.Errorf("获取活跃规则失败: %w", err)
	}

	req := securitydomain.RiskEvalRequest{
		Scene:     input.Scene,
		IP:        input.IP,
		DeviceID:  input.DeviceID,
		UserAgent: input.UserAgent,
	}
	env := s.buildEvalEnv(ctx, req)

	var totalScore int
	var matched []securitydomain.MatchedRule
	for _, rule := range rules {
		hit := s.evaluateCondition(rule, env)
		if hit {
			totalScore += rule.Score
			matched = append(matched, securitydomain.MatchedRule{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Score:    rule.Score,
			})
		}
	}

	riskLevel := scoreToLevel(totalScore)
	action, actionDetail := s.resolveAction(ctx, input.Scene, totalScore)

	return &securitydomain.RiskEvalResult{
		TotalScore:   totalScore,
		RiskLevel:    riskLevel,
		MatchedRules: matched,
		Action:       action,
		ActionDetail: actionDetail,
	}, nil
}

// ════════════════════════════════════════════════════════════
//  内部辅助
// ════════════════════════════════════════════════════════════

// buildEvalEnv 构建评估环境变量
func (s *RiskService) buildEvalEnv(ctx context.Context, req securitydomain.RiskEvalRequest) map[string]any {
	env := make(map[string]any, 24)

	// UserAgent 解析
	ua := useragent.New(req.UserAgent)
	browserName, _ := ua.Browser()
	env["ua_is_bot"] = ua.Bot()
	env["ua_os"] = ua.OS()
	env["ua_browser"] = browserName

	// 请求频率计数 / GCRA 限流信号
	s.populateRateLimitEnv(ctx, req, env)

	// 设备年龄
	env["device_age_hours"] = float64(0)
	if req.DeviceID != "" {
		if fp, err := s.pg.GetDeviceFingerprint(ctx, req.DeviceID); fp != nil && err == nil {
			hours := timeutil.Since(fp.FirstSeenAt).Hours()
			env["device_age_hours"] = hours
		}
	}

	// IP 风险信息
	env["ip_is_proxy"] = false
	env["ip_is_vpn"] = false
	env["ip_is_tor"] = false
	env["ip_is_datacenter"] = false
	env["ip_risk_score"] = 0
	env["ip_risk_tag"] = "normal"
	env["geo_country"] = ""
	if req.IP != "" {
		if ipRisk := s.resolveIPRisk(ctx, req.IP); ipRisk != nil {
			env["ip_is_proxy"] = ipRisk.IsProxy
			env["ip_is_vpn"] = ipRisk.IsVPN
			env["ip_is_tor"] = ipRisk.IsTor
			env["ip_is_datacenter"] = ipRisk.IsDatacenter
			env["ip_risk_score"] = ipRisk.RiskScore
			env["ip_risk_tag"] = ipRisk.RiskTag
			env["geo_country"] = ipRisk.Country
		}
	}

	// 合并 Extra 数据
	for k, v := range req.Extra {
		env[k] = v
	}

	return env
}

// evaluateCondition 评估单条规则
func (s *RiskService) evaluateCondition(rule securitydomain.RiskRule, env map[string]any) bool {
	switch rule.ConditionType {
	case "ip_frequency":
		threshold := toFloat64(rule.ConditionData["threshold"], 100)
		count := toFloat64(env["ip_request_count"], 0)
		return count > threshold

	case "device_new":
		maxHours := toFloat64(rule.ConditionData["max_hours"], 1)
		ageHours := toFloat64(env["device_age_hours"], 0)
		return ageHours < maxHours

	case "ua_bot":
		isBot, _ := env["ua_is_bot"].(bool)
		return isBot

	case "ip_proxy":
		isProxy, _ := env["ip_is_proxy"].(bool)
		isVPN, _ := env["ip_is_vpn"].(bool)
		return isProxy || isVPN

	case "rate_limited":
		dimension, _ := rule.ConditionData["dimension"].(string)
		dimension = strings.TrimSpace(strings.ToLower(dimension))
		if dimension == "" {
			dimension = "ip"
		}
		limited, _ := env[dimension+"_rate_limited"].(bool)
		return limited

	case "geo_anomaly":
		expected, _ := rule.ConditionData["expected_country"].(string)
		actual, _ := env["geo_country"].(string)
		return expected != "" && actual != "" && actual != expected

	case "custom_expr":
		expression, _ := rule.ConditionData["expression"].(string)
		if expression == "" {
			return false
		}
		program, err := expr.Compile(expression, expr.AsBool())
		if err != nil {
			s.log.Warn("expr 编译失败", zap.String("rule", rule.Name), zap.Error(err))
			return false
		}
		result, err := expr.Run(program, env)
		if err != nil {
			s.log.Warn("expr 执行失败", zap.String("rule", rule.Name), zap.Error(err))
			return false
		}
		if b, ok := result.(bool); ok {
			return b
		}
		return false

	default:
		s.log.Warn("未知规则条件类型", zap.String("type", rule.ConditionType))
		return false
	}
}

// resolveAction 根据分数查找匹配的处置策略
func (s *RiskService) resolveAction(ctx context.Context, scene string, totalScore int) (string, string) {
	actions, err := s.pg.ListRiskActions(ctx, scene)
	if err != nil {
		s.log.Error("查询处置策略失败", zap.Error(err))
		return "pass", ""
	}
	for _, a := range actions {
		if totalScore >= a.MinScore && (a.MaxScore == nil || totalScore <= *a.MaxScore) {
			detail := a.Description
			if a.BanDuration > 0 {
				detail = fmt.Sprintf("%s（封禁 %d 秒）", detail, a.BanDuration)
			}
			return a.Action, detail
		}
	}
	return "pass", ""
}

// scoreToLevel 分数→风险等级映射
func scoreToLevel(score int) string {
	switch {
	case score <= 20:
		return "normal"
	case score <= 40:
		return "low"
	case score <= 60:
		return "medium"
	case score <= 80:
		return "high"
	default:
		return "critical"
	}
}

// toFloat64 安全类型转换辅助
func toFloat64(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	case json.Number:
		if f, err := n.Float64(); err == nil {
			return f
		}
	}
	// 尝试 json 反序列化后的 number → float64
	if v == nil {
		return def
	}
	return def
}

func (s *RiskService) populateRateLimitEnv(ctx context.Context, req securitydomain.RiskEvalRequest, env map[string]any) {
	cfg, _ := s.runtime()
	env["ip_request_count"] = int64(0)
	env["account_request_count"] = int64(0)
	env["device_request_count"] = int64(0)
	env["account_device_request_count"] = int64(0)
	env["ip_rate_limited"] = false
	env["account_rate_limited"] = false
	env["device_rate_limited"] = false
	env["account_device_rate_limited"] = false

	if s.redis == nil {
		return
	}

	if s.rateLimiter == nil || !cfg.RateLimit.Enabled {
		ipReqKey := fmt.Sprintf("%s:risk:ip:%s:%s", s.keyPrefix, req.Scene, req.IP)
		count, err := s.redis.Incr(ctx, ipReqKey).Result()
		if err == nil && count == 1 {
			s.redis.Expire(ctx, ipReqKey, 60*time.Second)
		}
		env["ip_request_count"] = count
		return
	}

	appID := ""
	if req.AppID != nil {
		appID = fmt.Sprintf("%d", *req.AppID)
	}
	account := normalizeRiskDimension(toString(req.Extra["account"]))
	deviceID := normalizeRiskDimension(req.DeviceID)
	scene := normalizeRiskDimension(req.Scene)
	ip := normalizeRiskDimension(req.IP)

	if count, limited, ok := s.takeRateSample(ctx, "ip", cfg.RateLimit.IPPerMinute, scene, appID, ip); ok {
		env["ip_request_count"] = count
		env["ip_rate_limited"] = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account", cfg.RateLimit.AccountPerMinute, scene, appID, account); ok {
		env["account_request_count"] = count
		env["account_rate_limited"] = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "device", cfg.RateLimit.DevicePerMinute, scene, appID, deviceID); ok {
		env["device_request_count"] = count
		env["device_rate_limited"] = limited
	}
	if count, limited, ok := s.takeRateSample(ctx, "account_device", cfg.RateLimit.AccountDevicePerMinute, scene, appID, account, deviceID); ok {
		env["account_device_request_count"] = count
		env["account_device_rate_limited"] = limited
	}
}

func (s *RiskService) takeRateSample(ctx context.Context, scope string, perMinute int, parts ...string) (int64, bool, bool) {
	if s.rateLimiter == nil || perMinute <= 0 {
		return 0, false, false
	}
	key := s.riskRateLimitKey(scope, parts...)
	result, err := s.rateLimiter.Allow(ctx, key, redisrate.PerMinute(perMinute))
	if err != nil {
		s.log.Warn("risk rate limit check failed", zap.String("scope", scope), zap.Error(err))
		return 0, false, false
	}
	used := int64(perMinute - result.Remaining)
	if used < 0 {
		used = 0
	}
	if used > int64(perMinute) {
		used = int64(perMinute)
	}
	return used, result.Allowed == 0, true
}

func (s *RiskService) riskRateLimitKey(scope string, parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = normalizeRiskDimension(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(filtered, "|")))
	return fmt.Sprintf("%s:risk:rate:%s:%s", s.keyPrefix, scope, hex.EncodeToString(sum[:]))
}

func (s *RiskService) resolveIPRisk(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	cfg, provider := s.runtime()
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	value, err, _ := s.ipLookupFlight.Do(ip, func() (any, error) {
		if cached := s.readIPRiskCache(ctx, ip); cached != nil {
			return cached, nil
		}

		local, err := s.pg.GetIPRisk(ctx, ip)
		if err != nil && s.log != nil {
			s.log.Warn("load local ip risk failed", zap.String("ip", ip), zap.Error(err))
		}
		if local != nil && timeutil.Since(local.LastSeenAt) <= cfg.IPReputation.CacheTTL {
			s.writeIPRiskCache(ctx, local)
			return local, nil
		}
		if provider == nil {
			return local, nil
		}

		record, err := provider.Lookup(ctx, ip)
		if err != nil {
			if local != nil && cfg.IPReputation.AllowStale {
				return local, nil
			}
			return nil, err
		}
		record = normalizeIPRiskRecord(ip, record)
		if record == nil {
			return local, nil
		}
		stored, upsertErr := s.pg.UpsertIPRisk(ctx, *record)
		if upsertErr != nil {
			if s.log != nil {
				s.log.Warn("persist ip reputation failed", zap.String("ip", ip), zap.String("provider", provider.Name()), zap.Error(upsertErr))
			}
			s.writeIPRiskCache(ctx, record)
			return record, nil
		}
		s.writeIPRiskCache(ctx, stored)
		return stored, nil
	})
	if err != nil {
		if s.log != nil {
			s.log.Warn("resolve ip reputation failed", zap.String("ip", ip), zap.Error(err))
		}
		return nil
	}
	record, _ := value.(*securitydomain.IPRiskRecord)
	return record
}

func (s *RiskService) readIPRiskCache(ctx context.Context, ip string) *securitydomain.IPRiskRecord {
	cfg, _ := s.runtime()
	if s.redis == nil || cfg.IPReputation.CacheTTL <= 0 {
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
	cfg, _ := s.runtime()
	if s.redis == nil || record == nil || cfg.IPReputation.CacheTTL <= 0 {
		return
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := s.redis.Set(ctx, s.ipRiskCacheKey(record.IP), payload, cfg.IPReputation.CacheTTL).Err(); err != nil && s.log != nil {
		s.log.Warn("write ip risk cache failed", zap.String("ip", record.IP), zap.Error(err))
	}
}

func (s *RiskService) ipRiskCacheKey(ip string) string {
	return fmt.Sprintf("%s:risk:iprep:%s", s.keyPrefix, strings.TrimSpace(ip))
}

func normalizeRiskDimension(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return value
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}

// ════════════════════════════════════════════════════════════
//  代理到 Repository 的 CRUD 方法
// ════════════════════════════════════════════════════════════

// ── 规则 ──

func (s *RiskService) CreateRiskRule(ctx context.Context, input securitydomain.CreateRiskRuleInput, createdBy int64) (*securitydomain.RiskRule, error) {
	return s.pg.CreateRiskRule(ctx, input, createdBy)
}

func (s *RiskService) ListRiskRules(ctx context.Context, scene string) ([]securitydomain.RiskRule, error) {
	return s.pg.ListRiskRules(ctx, scene)
}

func (s *RiskService) UpdateRiskRule(ctx context.Context, id int64, input securitydomain.UpdateRiskRuleInput) error {
	return s.pg.UpdateRiskRule(ctx, id, input)
}

func (s *RiskService) DeleteRiskRule(ctx context.Context, id int64) error {
	return s.pg.DeleteRiskRule(ctx, id)
}

// ── 评估记录 ──

func (s *RiskService) ListRiskAssessments(ctx context.Context, scene, riskLevel, action string, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	return s.pg.ListRiskAssessments(ctx, scene, riskLevel, action, page, limit)
}

func (s *RiskService) GetRiskAssessment(ctx context.Context, id int64) (*securitydomain.RiskAssessment, error) {
	return s.pg.GetRiskAssessment(ctx, id)
}

func (s *RiskService) ListPendingReviews(ctx context.Context, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	return s.pg.ListPendingReviews(ctx, page, limit)
}

func (s *RiskService) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, result, comment string) error {
	// 更新复核状态
	if err := s.pg.ReviewRiskAssessment(ctx, id, reviewerID, result, comment); err != nil {
		return err
	}

	// 业务联动：拒绝时自动封禁 IP
	if result == "rejected" {
		assessment, err := s.pg.GetRiskAssessment(ctx, id)
		if err == nil && assessment != nil && assessment.IP != "" {
			s.log.Info("复核拒绝，自动封禁 IP",
				zap.Int64("assessmentId", id),
				zap.String("ip", assessment.IP),
				zap.String("scene", assessment.Scene),
			)
			// 标记 IP 为高风险
			_, _ = s.pg.UpsertIPRisk(ctx, securitydomain.IPRiskRecord{
				IP:        assessment.IP,
				RiskTag:   "bot",
				RiskScore: assessment.TotalScore,
			})
		}
	}
	return nil
}

// ── 设备指纹 ──

func (s *RiskService) UpsertDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	return s.pg.UpsertDeviceFingerprint(ctx, fp)
}

func (s *RiskService) GetDeviceFingerprint(ctx context.Context, deviceID string) (*securitydomain.DeviceFingerprint, error) {
	return s.pg.GetDeviceFingerprint(ctx, deviceID)
}

func (s *RiskService) ListSuspiciousDevices(ctx context.Context, page, limit int) ([]securitydomain.DeviceFingerprint, int64, error) {
	return s.pg.ListSuspiciousDevices(ctx, page, limit)
}

func (s *RiskService) UpdateDeviceRiskTag(ctx context.Context, id int64, tag string) error {
	return s.pg.UpdateDeviceRiskTag(ctx, id, tag)
}

// ── IP 风险库 ──

func (s *RiskService) UpsertIPRisk(ctx context.Context, rec securitydomain.IPRiskRecord) (*securitydomain.IPRiskRecord, error) {
	return s.pg.UpsertIPRisk(ctx, rec)
}

func (s *RiskService) GetIPRisk(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	return s.pg.GetIPRisk(ctx, ip)
}

func (s *RiskService) ListHighRiskIPs(ctx context.Context, page, limit int) ([]securitydomain.IPRiskRecord, int64, error) {
	return s.pg.ListHighRiskIPs(ctx, page, limit)
}

func (s *RiskService) UpdateIPRiskTag(ctx context.Context, id int64, tag string) error {
	return s.pg.UpdateIPRiskTag(ctx, id, tag)
}

// ── 处置策略 ──

func (s *RiskService) CreateRiskAction(ctx context.Context, input securitydomain.CreateRiskActionInput) (*securitydomain.RiskAction, error) {
	return s.pg.CreateRiskAction(ctx, input)
}

func (s *RiskService) ListRiskActions(ctx context.Context, scene string) ([]securitydomain.RiskAction, error) {
	return s.pg.ListRiskActions(ctx, scene)
}

func (s *RiskService) UpdateRiskAction(ctx context.Context, id int64, isActive bool) error {
	return s.pg.UpdateRiskAction(ctx, id, isActive)
}

func (s *RiskService) DeleteRiskAction(ctx context.Context, id int64) error {
	return s.pg.DeleteRiskAction(ctx, id)
}

// ── 统计 ──

func (s *RiskService) GetRiskDashboard(ctx context.Context, start, end time.Time) (*securitydomain.RiskDashboard, error) {
	return s.pg.GetRiskDashboard(ctx, start, end)
}
