package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"aegis/internal/cluster"
	firewalldomain "aegis/internal/domain/firewall"
	fw "aegis/internal/firewall"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// FirewallPolicyService 防火墙的自定义规则、WAF 例外、规则目录与测试台（docs/firewall.md）。
//
// 规则与例外存库；每次改动先在本机试编译（例外拼进 Coraza 指令，拼错会让整个 WAF 编译失败），
// 通过才落库，然后本机热更新并广播给其他副本（各副本收到后自己从库里重载，消息里不带内容）。
type FirewallPolicyService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	firewall firewallPolicyRuntime
	cluster  clusterLink
}

type firewallPolicyRuntime interface {
	SetPolicy([]fw.CustomRule, []fw.Exclusion) error
	ValidatePolicy([]fw.CustomRule, []fw.Exclusion) error
	TestWAF([]fw.Exclusion) (*fw.WAF, error)
	CurrentWAF() *fw.WAF
	TakeRuleHits() map[int64]int64
	Snapshot() fw.Snapshot
}

// NewFirewallPolicyService 创建服务；firewall 为 nil 时（防火墙未启用的进程，如 Worker）只做数据读写。
func NewFirewallPolicyService(log *zap.Logger, pg *pgrepo.Repository, firewall firewallPolicyRuntime) *FirewallPolicyService {
	if log == nil {
		log = zap.NewNop()
	}
	return &FirewallPolicyService{log: log, pg: pg, firewall: firewall}
}

type firewallPolicyChange struct {
	Kind string `json:"kind"`
}

// SetCluster 订阅其他副本的策略变更广播。
func (s *FirewallPolicyService) SetCluster(c *cluster.Cluster) {
	if c == nil {
		return
	}
	s.cluster.set(c)
	c.Subscribe(cluster.TopicFirewallPolicy, func([]byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.Reload(ctx); err != nil {
			s.log.Warn("按集群广播重载防火墙策略失败", zap.Error(err))
			return
		}
		s.log.Info("已按集群广播重载防火墙策略")
	})
}

// Initialize 启动时把库里的规则与例外装进运行时。
func (s *FirewallPolicyService) Initialize(ctx context.Context) error {
	return s.Reload(ctx)
}

// Reload 从库里读启用的规则与例外并换上。
func (s *FirewallPolicyService) Reload(ctx context.Context) error {
	if s.firewall == nil {
		return nil
	}
	rules, exclusions, err := s.loadEnabled(ctx)
	if err != nil {
		return err
	}
	return s.firewall.SetPolicy(rules, exclusions)
}

func (s *FirewallPolicyService) loadEnabled(ctx context.Context) ([]fw.CustomRule, []fw.Exclusion, error) {
	ruleRows, err := s.pg.ListFirewallRules(ctx)
	if err != nil {
		return nil, nil, err
	}
	exclusionRows, err := s.pg.ListWAFExclusions(ctx)
	if err != nil {
		return nil, nil, err
	}
	rules := make([]fw.CustomRule, 0, len(ruleRows))
	for _, r := range ruleRows {
		if r.Enabled {
			rules = append(rules, toEngineRule(r))
		}
	}
	exclusions := make([]fw.Exclusion, 0, len(exclusionRows))
	for _, e := range exclusionRows {
		if e.Enabled {
			exclusions = append(exclusions, toEngineExclusion(e))
		}
	}
	return rules, exclusions, nil
}

func toEngineRule(r firewalldomain.Rule) fw.CustomRule {
	return fw.CustomRule{ID: r.ID, Name: r.Name, Priority: r.Priority, Expression: r.Expression, Action: r.Action, BlockMode: r.BlockMode, BanSeconds: r.BanSeconds}
}

func toEngineExclusion(e firewalldomain.WAFExclusion) fw.Exclusion {
	return fw.Exclusion{ID: e.ID, Name: e.Name, PathMatch: e.PathMatch, Path: e.Path, Methods: e.Methods, RuleIDs: e.RuleIDs, Tags: e.Tags, Targets: e.Targets}
}

// applyChange 改动生效：本机重载 + 广播。改动已落库，重载失败只记日志（下一次广播或重启会再试）。
func (s *FirewallPolicyService) applyChange(ctx context.Context, kind string) {
	if err := s.Reload(ctx); err != nil {
		s.log.Error("防火墙策略热更新失败", zap.String("kind", kind), zap.Error(err))
	}
	s.cluster.publish(s.log, cluster.TopicFirewallPolicy, firewallPolicyChange{Kind: kind})
}

// validateCandidate 用「改动之后」的整套启用规则与例外试编译。
func (s *FirewallPolicyService) validateCandidate(ctx context.Context, rule *firewalldomain.Rule, exclusion *firewalldomain.WAFExclusion) error {
	if s.firewall == nil {
		return nil
	}
	rules, exclusions, err := s.loadEnabled(ctx)
	if err != nil {
		return err
	}
	if rule != nil {
		next := rules[:0:0]
		for _, r := range rules {
			if r.ID != rule.ID {
				next = append(next, r)
			}
		}
		if rule.Enabled {
			next = append(next, toEngineRule(*rule))
		}
		rules = next
	}
	if exclusion != nil {
		next := exclusions[:0:0]
		replaced := false
		for _, e := range exclusions {
			if e.ID == exclusion.ID && exclusion.ID != 0 {
				replaced = true
				if exclusion.Enabled {
					next = append(next, toEngineExclusion(*exclusion))
				}
				continue
			}
			next = append(next, e)
		}
		if !replaced && exclusion.Enabled {
			next = append(next, toEngineExclusion(*exclusion))
		}
		exclusions = next
	}
	return s.firewall.ValidatePolicy(rules, exclusions)
}

// ──────────────────────────────────────
// 自定义规则
// ──────────────────────────────────────

// RuleInput 新建 / 修改规则的输入。
type FirewallRuleInput struct {
	Name        string
	Description string
	Enabled     bool
	Priority    int
	Expression  string
	Action      string
	BlockMode   string
	BanSeconds  int64
}

func (in FirewallRuleInput) validate() error {
	if strings.TrimSpace(in.Name) == "" || len([]rune(in.Name)) > 80 {
		return apperrors.New(40001, http.StatusBadRequest, "规则名称不能为空，最多 80 个字符")
	}
	if len([]rune(in.Description)) > 500 {
		return apperrors.New(40001, http.StatusBadRequest, "规则说明最多 500 个字符")
	}
	if in.BlockMode != "" && firewalldomain.NormalizeBanMode(in.BlockMode, "") != in.BlockMode {
		return apperrors.New(40001, http.StatusBadRequest, "拦截响应方式无效")
	}
	if in.BanSeconds < -1 || in.BanSeconds > 365*24*3600 {
		return apperrors.New(40001, http.StatusBadRequest, "封禁时长需在 0 到 365 天之间，-1 表示永久")
	}
	if in.Action != fw.ActionBlock && in.BanSeconds != 0 {
		return apperrors.New(40001, http.StatusBadRequest, "只有拦截动作可以同时封禁来源 IP")
	}
	if _, err := fw.CompileRule(fw.CustomRule{Name: in.Name, Expression: in.Expression, Action: in.Action}); err != nil {
		return apperrors.New(40092, http.StatusBadRequest, err.Error())
	}
	return nil
}

// ListRules 全部规则（含停用的）。
func (s *FirewallPolicyService) ListRules(ctx context.Context) ([]firewalldomain.Rule, error) {
	return s.pg.ListFirewallRules(ctx)
}

// CreateRule 新建。
func (s *FirewallPolicyService) CreateRule(ctx context.Context, in FirewallRuleInput, adminID int64) (*firewalldomain.Rule, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	rule := firewalldomain.Rule{
		Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Enabled: in.Enabled,
		Priority: in.Priority, Expression: strings.TrimSpace(in.Expression), Action: in.Action,
		BlockMode: in.BlockMode, BanSeconds: in.BanSeconds, CreatedBy: &adminID,
	}
	if rule.Priority == 0 {
		rule.Priority = 100
	}
	if err := s.validateCandidate(ctx, &rule, nil); err != nil {
		return nil, apperrors.New(40092, http.StatusBadRequest, err.Error())
	}
	created, err := s.pg.InsertFirewallRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	s.applyChange(ctx, "rule")
	return created, nil
}

// UpdateRule 修改。
func (s *FirewallPolicyService) UpdateRule(ctx context.Context, id int64, in FirewallRuleInput, adminID int64) (*firewalldomain.Rule, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	rule := firewalldomain.Rule{
		ID: id, Name: strings.TrimSpace(in.Name), Description: strings.TrimSpace(in.Description), Enabled: in.Enabled,
		Priority: in.Priority, Expression: strings.TrimSpace(in.Expression), Action: in.Action,
		BlockMode: in.BlockMode, BanSeconds: in.BanSeconds, UpdatedBy: &adminID,
	}
	if err := s.validateCandidate(ctx, &rule, nil); err != nil {
		return nil, apperrors.New(40092, http.StatusBadRequest, err.Error())
	}
	updated, err := s.pg.UpdateFirewallRule(ctx, rule)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, apperrors.New(40490, http.StatusNotFound, "规则不存在")
	}
	s.applyChange(ctx, "rule")
	return updated, nil
}

// ToggleRule 启停。
func (s *FirewallPolicyService) ToggleRule(ctx context.Context, id int64, enabled bool, adminID int64) error {
	ok, err := s.pg.SetFirewallRuleEnabled(ctx, id, enabled, adminID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40490, http.StatusNotFound, "规则不存在")
	}
	s.applyChange(ctx, "rule")
	return nil
}

// DeleteRule 删除。
func (s *FirewallPolicyService) DeleteRule(ctx context.Context, id int64) error {
	ok, err := s.pg.DeleteFirewallRule(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40490, http.StatusNotFound, "规则不存在")
	}
	s.applyChange(ctx, "rule")
	return nil
}

// ReorderRules 按给定顺序重排。
func (s *FirewallPolicyService) ReorderRules(ctx context.Context, ids []int64, adminID int64) error {
	if len(ids) == 0 || len(ids) > 500 {
		return apperrors.New(40001, http.StatusBadRequest, "排序列表为空或过长")
	}
	if err := s.pg.ReorderFirewallRules(ctx, ids, adminID); err != nil {
		return err
	}
	s.applyChange(ctx, "rule")
	return nil
}

// FlushRuleHits 把本副本攒下的命中数累加进库（每 30 秒一次，见 RunHitFlusher）。
func (s *FirewallPolicyService) FlushRuleHits(ctx context.Context) error {
	if s.firewall == nil {
		return nil
	}
	hits := s.firewall.TakeRuleHits()
	if len(hits) == 0 {
		return nil
	}
	return s.pg.AddFirewallRuleHits(ctx, hits, time.Now().UTC())
}

// RunHitFlusher 定时回写命中数，ctx 结束时最后再写一次。
func (s *FirewallPolicyService) RunHitFlusher(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.FlushRuleHits(flushCtx)
			cancel()
			return
		case <-ticker.C:
			if err := s.FlushRuleHits(ctx); err != nil {
				s.log.Warn("回写防火墙规则命中数失败", zap.Error(err))
			}
		}
	}
}

// ──────────────────────────────────────
// WAF 例外
// ──────────────────────────────────────

// FirewallExclusionInput 新建 / 修改例外的输入。
type FirewallExclusionInput struct {
	Name      string
	Note      string
	Enabled   bool
	PathMatch string
	Path      string
	Methods   []string
	RuleIDs   []int
	Tags      []string
	Targets   []string
}

func (in FirewallExclusionInput) toDomain() firewalldomain.WAFExclusion {
	upper := make([]string, 0, len(in.Methods))
	for _, m := range in.Methods {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			upper = append(upper, m)
		}
	}
	return firewalldomain.WAFExclusion{
		Name: strings.TrimSpace(in.Name), Note: strings.TrimSpace(in.Note), Enabled: in.Enabled,
		PathMatch: in.PathMatch, Path: strings.TrimSpace(in.Path), Methods: upper,
		RuleIDs: in.RuleIDs, Tags: compactStrings(in.Tags), Targets: compactStrings(in.Targets),
	}
}

func validateExclusion(e firewalldomain.WAFExclusion) error {
	if e.Name == "" || len([]rune(e.Name)) > 80 {
		return apperrors.New(40001, http.StatusBadRequest, "例外名称不能为空，最多 80 个字符")
	}
	if err := fw.ValidateExclusion(toEngineExclusion(e)); err != nil {
		return apperrors.New(40093, http.StatusBadRequest, err.Error())
	}
	return nil
}

// ListExclusions 全部例外。
func (s *FirewallPolicyService) ListExclusions(ctx context.Context) ([]firewalldomain.WAFExclusion, error) {
	return s.pg.ListWAFExclusions(ctx)
}

// CreateExclusion 新建。
func (s *FirewallPolicyService) CreateExclusion(ctx context.Context, in FirewallExclusionInput, adminID int64) (*firewalldomain.WAFExclusion, error) {
	e := in.toDomain()
	e.CreatedBy = &adminID
	if err := validateExclusion(e); err != nil {
		return nil, err
	}
	if err := s.validateCandidate(ctx, nil, &e); err != nil {
		return nil, apperrors.New(40093, http.StatusBadRequest, err.Error())
	}
	created, err := s.pg.InsertWAFExclusion(ctx, e)
	if err != nil {
		return nil, err
	}
	s.applyChange(ctx, "exclusion")
	return created, nil
}

// UpdateExclusion 修改。
func (s *FirewallPolicyService) UpdateExclusion(ctx context.Context, id int64, in FirewallExclusionInput, adminID int64) (*firewalldomain.WAFExclusion, error) {
	e := in.toDomain()
	e.ID = id
	e.UpdatedBy = &adminID
	if err := validateExclusion(e); err != nil {
		return nil, err
	}
	if err := s.validateCandidate(ctx, nil, &e); err != nil {
		return nil, apperrors.New(40093, http.StatusBadRequest, err.Error())
	}
	updated, err := s.pg.UpdateWAFExclusion(ctx, e)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, apperrors.New(40491, http.StatusNotFound, "例外不存在")
	}
	s.applyChange(ctx, "exclusion")
	return updated, nil
}

// ToggleExclusion 启停。
func (s *FirewallPolicyService) ToggleExclusion(ctx context.Context, id int64, enabled bool, adminID int64) error {
	if enabled {
		current, err := s.pg.GetWAFExclusion(ctx, id)
		if err != nil {
			return err
		}
		if current == nil {
			return apperrors.New(40491, http.StatusNotFound, "例外不存在")
		}
		current.Enabled = true
		if err := s.validateCandidate(ctx, nil, current); err != nil {
			return apperrors.New(40093, http.StatusBadRequest, err.Error())
		}
	}
	ok, err := s.pg.SetWAFExclusionEnabled(ctx, id, enabled, adminID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40491, http.StatusNotFound, "例外不存在")
	}
	s.applyChange(ctx, "exclusion")
	return nil
}

// DeleteExclusion 删除。
func (s *FirewallPolicyService) DeleteExclusion(ctx context.Context, id int64) error {
	ok, err := s.pg.DeleteWAFExclusion(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40491, http.StatusNotFound, "例外不存在")
	}
	s.applyChange(ctx, "exclusion")
	return nil
}

// ──────────────────────────────────────
// 运行态、规则目录、测试台
// ──────────────────────────────────────

// FirewallRuntimeView 本副本的运行态。
type FirewallRuntimeView struct {
	Enabled        bool        `json:"enabled"`
	WAFActive      bool        `json:"wafActive"`
	DetectionOnly  bool        `json:"detectionOnly"`
	Threshold      int         `json:"threshold"`
	Paranoia       int         `json:"paranoia"`
	CustomRules    int         `json:"customRules"`
	Exclusions     int         `json:"exclusions"`
	RuleErrors     []string    `json:"ruleErrors"`
	ReloadVersion  uint64      `json:"reloadVersion"`
	ReloadedAt     time.Time   `json:"reloadedAt"`
	CRSVersion     string      `json:"crsVersion"`
	ActiveRules    int         `json:"activeRules"`
	TotalRules     int         `json:"totalRules"`
	RulesetVersion int         `json:"rulesetVersion"`
	Counters       fw.Counters `json:"counters"`
}

// Runtime 运行态。
func (s *FirewallPolicyService) Runtime() FirewallRuntimeView {
	if s.firewall == nil {
		return FirewallRuntimeView{}
	}
	snap := s.firewall.Snapshot()
	view := FirewallRuntimeView{
		Enabled: snap.RuntimeEnabled, WAFActive: snap.WAFActive, DetectionOnly: snap.Config.CorazaDetectionOnly,
		Threshold: snap.Config.CorazaAnomalyThreshold, Paranoia: snap.Config.CorazaParanoia,
		CustomRules: snap.CustomRules, Exclusions: snap.Exclusions, RuleErrors: snap.RuleErrors,
		ReloadVersion: snap.ReloadVersion, ReloadedAt: snap.ReloadedAt, RulesetVersion: snap.Config.CorazaRulesetVersion,
		Counters: snap.Counters,
	}
	if view.RuleErrors == nil {
		view.RuleErrors = []string{}
	}
	if snap.Catalog != nil {
		view.CRSVersion = snap.Catalog.Version
		view.ActiveRules = snap.Catalog.ActiveRules
		view.TotalRules = snap.Catalog.TotalRules
	}
	return view
}

// Catalog 当前配置下的规则目录；WAF 未启用时按当前配置现编一份（只为列目录）。
func (s *FirewallPolicyService) Catalog() (*fw.Catalog, error) {
	if s.firewall == nil {
		return nil, apperrors.New(50390, http.StatusServiceUnavailable, "防火墙未启用")
	}
	if waf := s.firewall.CurrentWAF(); waf != nil {
		return waf.Catalog(), nil
	}
	waf, err := s.firewall.TestWAF(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = waf.Close() }()
	return waf.Catalog(), nil
}

// Directives 线上 WAF 的完整指令（排查例外有没有拼对）。
func (s *FirewallPolicyService) Directives() string {
	if s.firewall == nil {
		return ""
	}
	if waf := s.firewall.CurrentWAF(); waf != nil {
		return waf.Directives()
	}
	return ""
}

// SandboxInput 测试台的请求。
type SandboxInput struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Body      string            `json:"body"`
	IP        string            `json:"ip"`
	Transport string            `json:"transport"`
	// DraftExclusion 带上一条还没保存的例外，看它能不能消掉误报。
	DraftExclusion *FirewallExclusionInput `json:"-"`
}

// SandboxResult 测试台结论。
type SandboxResult struct {
	Verdict       string                       `json:"verdict"` // pass / block / observe
	Stage         string                       `json:"stage"`
	Reason        string                       `json:"reason"`
	CustomRule    *firewalldomain.Rule         `json:"customRule,omitempty"`
	LoggedRules   []firewalldomain.Rule        `json:"loggedRules"`
	InboundScore  int                          `json:"inboundScore"`
	Threshold     int                          `json:"threshold"`
	DetectionOnly bool                         `json:"detectionOnly"`
	Status        int                          `json:"status"`
	RuleID        int                          `json:"ruleId"`
	Matches       []firewalldomain.MatchedRule `json:"matches"`
	EffectiveType string                       `json:"effectiveContentType"`
	ElapsedMicros int64                        `json:"elapsedMicros"`
	UsedDraft     bool                         `json:"usedDraft"`
}

// Sandbox 不经过网络、在进程内把一个请求依次过自定义规则与 WAF（与线上同一份编译结果；带草稿例外时临时编译一份）。
// 入口段的 IP 名单、封禁、签名与限流与请求内容无关或有副作用，测试台不跑。
func (s *FirewallPolicyService) Sandbox(ctx context.Context, in SandboxInput) (*SandboxResult, error) {
	if s.firewall == nil {
		return nil, apperrors.New(50390, http.StatusServiceUnavailable, "防火墙未启用")
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	target, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || target.Path == "" || !strings.HasPrefix(target.Path, "/") {
		return nil, apperrors.New(40001, http.StatusBadRequest, "请求地址需以 / 开头，例如 /api/v1/apps/xxx/user/profile?id=1")
	}
	if len(in.Body) > 1<<20 {
		return nil, apperrors.New(40001, http.StatusBadRequest, "测试请求体最多 1 MiB")
	}
	ip := strings.TrimSpace(in.IP)
	if ip == "" {
		ip = "203.0.113.10"
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return nil, apperrors.New(40001, http.StatusBadRequest, "IP 地址无效")
	}
	headers := http.Header{}
	for k, v := range in.Headers {
		if k = strings.TrimSpace(k); k != "" {
			headers.Set(k, v)
		}
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", "Mozilla/5.0 (Aegis Firewall Sandbox)")
	}
	if headers.Get("Accept") == "" {
		headers.Set("Accept", "application/json")
	}
	transport := in.Transport
	switch transport {
	case firewalldomain.TransportSealed, firewalldomain.TransportLegacy:
	default:
		transport = firewalldomain.TransportPlain
	}
	started := time.Now()
	result := &SandboxResult{Verdict: "pass", LoggedRules: []firewalldomain.Rule{}, Matches: []firewalldomain.MatchedRule{}}

	// 自定义规则（按库里启用的规则求值，与线上一致）
	ruleRows, err := s.pg.ListFirewallRules(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]firewalldomain.Rule{}
	var enabled []fw.CustomRule
	for _, r := range ruleRows {
		if r.Enabled {
			enabled = append(enabled, toEngineRule(r))
			byID[r.ID] = r
		}
	}
	set, _ := fw.NewRuleSet(enabled)
	lowered := make(map[string]string, len(headers))
	for k, v := range headers {
		if len(v) > 0 {
			lowered[strings.ToLower(k)] = v[0]
		}
	}
	args := map[string]string{}
	for k, v := range target.Query() {
		if len(v) > 0 {
			args[k] = v[0]
		}
	}
	env := &fw.RuleEnv{
		IP: ip, Method: method, Path: target.Path, Query: target.RawQuery, Host: "sandbox.local",
		UA: headers.Get("User-Agent"), Referer: headers.Get("Referer"), ContentType: headers.Get("Content-Type"),
		Headers: lowered, Args: args, Encrypted: transport != firewalldomain.TransportPlain,
	}
	if strings.HasPrefix(target.Path, "/api/v1/apps/") {
		rest := strings.TrimPrefix(target.Path, "/api/v1/apps/")
		env.App, _, _ = strings.Cut(rest, "/")
	}
	env.FillTime(time.Now())
	terminal, logged := set.Evaluate(env)
	for _, r := range logged {
		result.LoggedRules = append(result.LoggedRules, byID[r.ID])
	}
	if terminal != nil {
		rule := byID[terminal.ID]
		result.CustomRule = &rule
		switch terminal.Action {
		case fw.ActionBlock:
			result.Verdict, result.Stage, result.Reason, result.Status = "block", firewalldomain.StageEdge, "custom_rule", http.StatusForbidden
			result.ElapsedMicros = time.Since(started).Microseconds()
			return result, nil
		case fw.ActionAllow:
			result.Reason = "custom_allow"
			result.ElapsedMicros = time.Since(started).Microseconds()
			return result, nil
		case fw.ActionSkipWAF:
			result.Reason = "custom_skip_waf"
			result.ElapsedMicros = time.Since(started).Microseconds()
			return result, nil
		}
	}

	// WAF
	waf := s.firewall.CurrentWAF()
	if in.DraftExclusion != nil {
		draft := in.DraftExclusion.toDomain()
		draft.Enabled = true
		if err := validateExclusion(draft); err != nil {
			return nil, err
		}
		exclusionRows, err := s.pg.ListWAFExclusions(ctx)
		if err != nil {
			return nil, err
		}
		var exclusions []fw.Exclusion
		for _, e := range exclusionRows {
			if e.Enabled {
				exclusions = append(exclusions, toEngineExclusion(e))
			}
		}
		exclusions = append(exclusions, toEngineExclusion(draft))
		tmp, err := s.firewall.TestWAF(exclusions)
		if err != nil {
			return nil, apperrors.New(40093, http.StatusBadRequest, err.Error())
		}
		defer func() { _ = tmp.Close() }()
		waf = tmp
		result.UsedDraft = true
	}
	if waf == nil {
		result.Reason = "waf_disabled"
		result.ElapsedMicros = time.Since(started).Microseconds()
		return result, nil
	}
	body := []byte(in.Body)
	effective := fw.EffectiveContentType(headers.Get("Content-Type"), body)
	if effective != headers.Get("Content-Type") {
		headers.Set("Content-Type", effective)
	}
	result.EffectiveType = effective
	result.Threshold = waf.InboundThreshold()
	result.DetectionOnly = waf.DetectionOnly()
	uri := target.EscapedPath()
	if target.RawQuery != "" {
		uri += "?" + target.RawQuery
	}
	txn, res := waf.Begin(fw.Request{RemoteIP: ip, Method: method, URI: uri, Host: "sandbox.local", Headers: headers, Transport: transport, ContentLength: int64(len(body))})
	defer func() { _ = txn.Close() }()
	if res == nil {
		var reader *strings.Reader
		if len(body) > 0 && txn.BodyAccessible() {
			reader = strings.NewReader(in.Body)
		}
		if reader != nil {
			res, err = txn.RequestBody(reader)
		} else {
			res, err = txn.RequestBody(nil)
		}
		if err != nil {
			return nil, fmt.Errorf("WAF 检测失败: %w", err)
		}
	}
	result.InboundScore = res.InboundScore
	result.RuleID = res.RuleID
	result.Status = res.Status
	for _, m := range res.Matches {
		result.Matches = append(result.Matches, firewalldomain.MatchedRule{ID: m.RuleID, Message: m.Message, Data: m.Data, Severity: m.Severity, Phase: m.Phase, Group: m.Group, Tags: m.Tags, Targets: m.Targets})
	}
	result.Stage = firewalldomain.StagePayload
	switch {
	case res.Interrupted:
		result.Verdict, result.Reason = "block", "waf_blocked"
	case res.WouldBlock:
		result.Verdict, result.Reason = "observe", "waf_observed"
	default:
		result.Reason = "clean"
	}
	result.ElapsedMicros = time.Since(started).Microseconds()
	return result, nil
}

// RuleDetail 规则目录里的一条规则加上最近命中（事件详情页「这条规则是什么」）。
func (s *FirewallPolicyService) RuleDetail(ctx context.Context, id int) (map[string]any, error) {
	catalog, err := s.Catalog()
	if err != nil {
		return nil, err
	}
	for _, rule := range catalog.Rules {
		if rule.ID == id {
			ruleID := id
			page, err := s.pg.ListFirewallLogs(ctx, firewalldomain.FirewallLogFilter{WAFRuleID: &ruleID, Page: 1, PageSize: 5})
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(rule)
			out := map[string]any{}
			_ = json.Unmarshal(raw, &out)
			out["recent"] = page.Items
			out["recentTotal"] = page.Total
			out["groupName"] = fw.GroupName(rule.Group)
			return out, nil
		}
	}
	return nil, apperrors.New(40492, http.StatusNotFound, "规则目录里没有这条规则")
}
