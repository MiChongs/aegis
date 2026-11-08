package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/config"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/timeutil"

	redisrate "github.com/go-redis/redis_rate/v10"
	"github.com/nats-io/nats.go"
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

	// 规则 / 策略 / 名单的内存快照：评估热路径不查库
	snapshot       atomic.Pointer[riskSnapshot]
	snapshotFlight singleflight.Group

	// 处置执行器
	accountBans  riskAccountBanner
	firewall     riskFirewall
	captchaProbe func(ctx context.Context, appID int64) bool

	// trail 评估留痕（JetStream 可靠队列 + 批量写库，risk_trail.go）
	trail *riskTrail
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
	service.trail = newRiskTrail(pg, log, service.rememberAccountFact)
	service.applyConfig(cfg)
	return service
}

// AttachTrailStream 接上 NATS：评估留痕改走 JetStream（AEGIS_RISK_TRAIL）。不调用时留痕直接写库。
func (s *RiskService) AttachTrailStream(conn *nats.Conn) error {
	return s.trail.attach(conn)
}

// Start 建留痕流与消费者、启动写库协程（接了 NATS 时）。
func (s *RiskService) Start(ctx context.Context) {
	if s == nil || s.trail == nil {
		return
	}
	s.trail.start(ctx)
}

// Close 停止取新的留痕；正在写的那一批写完、确认完（退出时调用；ctx 到点就不再等）。没取走的留在流里，下次启动接着写。
func (s *RiskService) Close(ctx context.Context) {
	if s == nil || s.trail == nil {
		return
	}
	s.trail.stop(ctx)
}

// SetLocationService 注入地理定位服务。
// 归属地此前只能从外部 IP 情报源拿，未采购情报源时 geo_* 系列变量恒为空 ——
// 那意味着「归属地异常」这类规则配了也永不命中。LocationService 是免费的兜底。
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
//  规则管理（带校验）
// ════════════════════════════════════════════════════════════

// ValidateRuleInput 在落库之前把规则配置检查一遍。
//
// 这是整套改造里最值钱的一段：以前一条写错的规则能顺利存进数据库，
// 之后它对每一个请求都判假、永不命中，而列表上显示「已启用」。
// 现在场景 / 条件类型 / 必填参数 / 表达式语法任一项不合法都当场拒绝。
func ValidateRuleInput(scene, conditionType string, data map[string]any) error {
	if !securitydomain.ValidRuleScene(scene) {
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
	if err := validateRuleMode(input.Mode, input.Decision); err != nil {
		return nil, err
	}
	rule, err := s.pg.CreateRiskRule(ctx, input, createdBy)
	if err == nil {
		s.invalidateSnapshot()
	}
	return rule, err
}

func validateRuleMode(mode, decision string) error {
	if mode != "" && mode != securitydomain.RuleModeEnforce && mode != securitydomain.RuleModeObserve {
		return fmt.Errorf("未知规则模式：%s", mode)
	}
	if decision != "" && decision != securitydomain.ActionPass && !slices.Contains(securitydomain.ActionValues(), decision) {
		return fmt.Errorf("未知处置动作：%s", decision)
	}
	return nil
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
	mode, decision := "", ""
	if input.Mode != nil {
		mode = *input.Mode
	}
	if input.Decision != nil {
		decision = *input.Decision
	}
	if err := validateRuleMode(mode, decision); err != nil {
		return err
	}
	if err := s.pg.UpdateRiskRule(ctx, id, input); err != nil {
		return err
	}
	s.invalidateSnapshot()
	return nil
}

// pgRiskScope 作用域过滤
func pgRiskScope(scene string, appID *int64) pgrepo.RiskScopeFilter {
	return pgrepo.RiskScopeFilter{Scene: scene, AppID: appID}
}

func (s *RiskService) ListRiskRules(ctx context.Context, scene string, appID *int64) ([]securitydomain.RiskRule, error) {
	return s.pg.ListRiskRules(ctx, pgRiskScope(scene, appID))
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

func (s *RiskService) DeleteRiskRule(ctx context.Context, id int64) error {
	if err := s.pg.DeleteRiskRule(ctx, id); err != nil {
		return err
	}
	s.invalidateSnapshot()
	return nil
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
	pairs := map[string][]string{
		securitydomain.ListTypeIP: {assessment.IP}, securitydomain.ListTypeDevice: {assessment.DeviceID},
		securitydomain.ListTypeAccount: {strings.ToLower(assessment.Account)},
	}
	if assessment.UserID != nil {
		pairs[securitydomain.ListTypeUser] = []string{strconv.FormatInt(*assessment.UserID, 10)}
	}
	detail.ListEntries, _ = s.pg.FindRiskListEntriesFor(ctx, assessment.AppID, pairs)
	if detail.ListEntries == nil {
		detail.ListEntries = []securitydomain.RiskListEntry{}
	}
	return detail, nil
}

// rebuildRuleEvaluations 把留痕里的命中规则还原成轨迹结构，
// 让详情页与模拟器共用同一套渲染。
func rebuildRuleEvaluations(assessment securitydomain.RiskAssessment) []securitydomain.RuleEvaluation {
	out := make([]securitydomain.RuleEvaluation, 0, len(assessment.MatchedRules))
	for _, rule := range assessment.MatchedRules {
		mode := securitydomain.RuleModeEnforce
		if rule.Observe {
			mode = securitydomain.RuleModeObserve
		}
		dimension := rule.Dimension
		if dimension == "" {
			dimension = securitydomain.ConditionDimension(rule.ConditionType, nil)
		}
		out = append(out, securitydomain.RuleEvaluation{
			RuleID: rule.RuleID, RuleName: rule.RuleName, ConditionType: rule.ConditionType, Dimension: dimension,
			Score: rule.Score, Mode: mode, Decision: rule.Decision, Hit: true, Reason: rule.Reason,
		})
	}
	return out
}

func (s *RiskService) ListPendingReviews(ctx context.Context, appID *int64, page, limit int) ([]securitydomain.RiskAssessment, int64, error) {
	reviewed := false
	return s.pg.ListRiskAssessments(ctx, securitydomain.AssessmentQuery{
		AppID:    appID,
		Action:   securitydomain.ActionReview,
		Reviewed: &reviewed,
		Page:     page,
		PageSize: limit,
	})
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
	entries, _ := s.pg.FindRiskListEntriesFor(ctx, nil, map[string][]string{securitydomain.ListTypeDevice: {deviceID}})
	if entries == nil {
		entries = []securitydomain.RiskListEntry{}
	}
	return &securitydomain.DeviceRiskDetail{
		Device:      *fp,
		Summary:     summary,
		Recent:      recent,
		IPs:         ips,
		Accounts:    accounts,
		ListEntries: entries,
	}, nil
}

// UpdateDeviceRiskTag 改设备标签。封禁 / 受信两种标签同时写进名单库，才真正参与判定；
// 改回正常或可疑时撤销对应的全平台名单。
func (s *RiskService) UpdateDeviceRiskTag(ctx context.Context, id int64, tag, note string) error {
	if !slices.ContainsFunc(securitydomain.DeviceTagCatalog(), func(e securitydomain.CatalogEntry) bool { return e.Value == tag }) {
		return fmt.Errorf("未知设备风险标签：%s", tag)
	}
	if err := s.pg.UpdateDeviceRiskTag(ctx, id, tag, note); err != nil {
		return err
	}
	deviceID, err := s.pg.GetDeviceIDByFingerprintID(ctx, id)
	if err != nil || deviceID == "" {
		return err
	}
	return s.syncTagToList(ctx, securitydomain.ListTypeDevice, deviceID, tag, note)
}

// syncTagToList 档案标签 → 全平台名单
func (s *RiskService) syncTagToList(ctx context.Context, listType, value, tag, note string) error {
	kind := ""
	switch tag {
	case securitydomain.TagBlocked:
		kind = securitydomain.ListKindBlock
	case securitydomain.TagTrusted:
		kind = securitydomain.ListKindAllow
	}
	if kind == "" {
		existing, err := s.pg.FindRiskListEntriesFor(ctx, nil, map[string][]string{listType: {value}})
		if err != nil {
			return err
		}
		ids := make([]int64, 0, len(existing))
		for _, entry := range existing {
			if entry.AppID == nil && entry.Kind != securitydomain.ListKindWatch {
				ids = append(ids, entry.ID)
			}
		}
		if len(ids) > 0 {
			if _, err := s.pg.DeleteRiskListEntries(ctx, ids); err != nil {
				return err
			}
		}
		s.invalidateSnapshot()
		return nil
	}
	if note == "" {
		note = "档案标签"
	}
	_, _, _, err := s.pg.UpsertRiskListEntries(ctx, securitydomain.ListEntryInput{
		ListType: listType, Values: []string{value}, Kind: kind, Reason: note, Source: riskSourceManual,
	})
	s.invalidateSnapshot()
	return err
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
	entries, _ := s.pg.FindRiskListEntriesFor(ctx, nil, map[string][]string{securitydomain.ListTypeIP: {normalizeListValue(securitydomain.ListTypeIP, ip)}})
	if entries == nil {
		entries = []securitydomain.RiskListEntry{}
	}
	detail := &securitydomain.IPRiskDetail{
		Record:      *record,
		Summary:     summary,
		Recent:      recent,
		Devices:     devices,
		Accounts:    accounts,
		ListEntries: entries,
	}
	if ban, err := s.pg.GetActiveIPBan(ctx, ip); err == nil && ban != nil {
		detail.FirewallBan = &securitydomain.FirewallBanRef{ID: ban.ID, Reason: ban.Reason, Source: ban.Source, ExpiresAt: ban.ExpiresAt, CreatedAt: ban.CreatedAt}
	}
	return detail, nil
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
	if tag == securitydomain.TagBlocked || tag == securitydomain.TagTrusted || tag == securitydomain.TagNormal {
		return s.syncTagToList(ctx, securitydomain.ListTypeIP, normalizeListValue(securitydomain.ListTypeIP, ip), tag, note)
	}
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
	if err := validateBanTargets(input.BanTargets); err != nil {
		return nil, err
	}
	if err := s.checkActionOverlap(ctx, 0, input.AppID, input.Scene, input.MinScore, input.MaxScore); err != nil {
		return nil, err
	}
	action, err := s.pg.CreateRiskAction(ctx, input)
	if err == nil {
		s.invalidateSnapshot()
	}
	return action, err
}

// checkActionOverlap 同一作用域、同一场景里分数区间不能重叠：
// 重叠时「到底走哪条策略」取决于 SQL 的排序，是最难排查的一类配置事故。
func (s *RiskService) checkActionOverlap(ctx context.Context, selfID int64, appID *int64, scene string, minScore int, maxScore *int) error {
	existing, err := s.pg.ListRiskActions(ctx, pgRiskScope("", nil))
	if err != nil {
		return err
	}
	for _, other := range existing {
		if other.ID == selfID || other.Scene != scene || !sameAppScope(other.AppID, appID) {
			continue
		}
		if scoreRangesOverlap(minScore, maxScore, other.MinScore, other.MaxScore) {
			return fmt.Errorf("分数区间与已有策略 #%d（%d–%s）重叠", other.ID, other.MinScore, formatMaxScore(other.MaxScore))
		}
	}
	return nil
}

func sameAppScope(a, b *int64) bool {
	av, bv := int64(0), int64(0)
	if a != nil {
		av = *a
	}
	if b != nil {
		bv = *b
	}
	return av == bv
}

func validateBanTargets(targets []string) error {
	for _, target := range targets {
		if !slices.ContainsFunc(securitydomain.BanTargetCatalog(), func(e securitydomain.CatalogEntry) bool { return e.Value == target }) {
			return fmt.Errorf("未知封禁对象：%s", target)
		}
	}
	return nil
}

func (s *RiskService) ListRiskActions(ctx context.Context, scene string, appID *int64) ([]securitydomain.RiskAction, error) {
	if scene == securitydomain.SceneAll {
		all, err := s.pg.ListRiskActions(ctx, pgRiskScope("", appID))
		if err != nil {
			return nil, err
		}
		out := all[:0]
		for _, a := range all {
			if a.Scene == securitydomain.SceneAll {
				out = append(out, a)
			}
		}
		return out, nil
	}
	return s.pg.ListRiskActions(ctx, pgRiskScope(scene, appID))
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
	if input.BanTargets != nil {
		if err := validateBanTargets(*input.BanTargets); err != nil {
			return err
		}
	}
	if err := s.checkActionOverlap(ctx, id, current.AppID, current.Scene, minScore, maxScore); err != nil {
		return err
	}
	if err := s.pg.UpdateRiskAction(ctx, id, input); err != nil {
		return err
	}
	s.invalidateSnapshot()
	return nil
}

func (s *RiskService) DeleteRiskAction(ctx context.Context, id int64) error {
	if err := s.pg.DeleteRiskAction(ctx, id); err != nil {
		return err
	}
	s.invalidateSnapshot()
	return nil
}

func validateActionInput(scene, action string, minScore int, maxScore *int, banDuration int) error {
	if !securitydomain.ValidRuleScene(scene) {
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
func (s *RiskService) GetRiskDashboard(ctx context.Context, start, end time.Time, appID *int64) (*securitydomain.RiskDashboard, error) {
	bucket := bucketFor(start, end)
	dash, err := s.pg.GetRiskDashboard(ctx, start, end, bucket, appID)
	if err != nil {
		return nil, err
	}
	dash.Engine = s.engineStatus(ctx, appID)
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
// 执行器也一样：ban 判出来了、账号封禁服务却没接上，封禁就只是一行留痕。
func (s *RiskService) engineStatus(ctx context.Context, appID *int64) securitydomain.EngineStatus {
	rt := s.runtime()
	bans, firewall, probe := s.executors()
	status := securitydomain.EngineStatus{
		IPProvider:      strings.ToLower(strings.TrimSpace(rt.cfg.IPReputation.Provider)),
		IPProviderReady: rt.provider != nil,
		GeoReady:        rt.location != nil,
		RedisReady:      s.redis != nil,
		RateLimitOn:     rt.cfg.RateLimit.Enabled && s.rateLimiter != nil,
		CacheTTLSeconds: int(rt.cfg.IPReputation.CacheTTL / time.Second),
		AccountBanReady: bans != nil,
		FirewallReady:   firewall != nil,
		CaptchaReady:    probe != nil,
	}
	if status.IPProvider == "" {
		status.IPProvider = "none"
	}
	snap, err := s.loadSnapshot(ctx)
	if err != nil {
		return status
	}
	at := snap.loadedAt
	status.SnapshotAt = &at
	scopeApp := int64(0)
	if appID != nil {
		scopeApp = *appID
	}
	inScope := func(id *int64) bool { return id == nil || scopeApp == 0 || *id == scopeApp }

	all, err := s.pg.ListRiskRules(ctx, pgRiskScope("", appID))
	if err == nil {
		for _, rule := range all {
			status.TotalRules++
			if rule.IsActive {
				status.ActiveRules++
				if rule.Mode == securitydomain.RuleModeObserve {
					status.ObserveRules++
				}
			}
		}
	}
	covered := map[string]bool{}
	for _, rule := range snap.rules {
		if !inScope(rule.AppID) || rule.Mode == securitydomain.RuleModeObserve {
			continue
		}
		covered[rule.Scene] = true
	}
	for _, scene := range securitydomain.SceneValues() {
		if covered[scene] || covered[securitydomain.SceneAll] {
			status.ScenesCovered = append(status.ScenesCovered, scene)
		} else {
			status.ScenesUncovered = append(status.ScenesUncovered, scene)
		}
	}
	for _, action := range snap.actions {
		if !inScope(action.AppID) {
			continue
		}
		status.TotalActions++
		if action.IsActive {
			status.ActiveActions++
		}
	}
	status.ListEntries = int64(snap.lists.total)
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
