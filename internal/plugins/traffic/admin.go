package trafficplugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	trafficdomain "aegis/internal/domain/traffic"
	"aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/traffic"

	"go.uber.org/zap"
)

// 管理端操作。所有写操作都走 store.TrafficPublish（行锁串行化 + 修订快照），
// 成功后本实例立即应用，并广播修订号让其他实例跟进。

var (
	errConflict       = apperrors.New(40990, http.StatusConflict, "配置已被其他管理员修改，请刷新后重试")
	errRuleNotFound   = apperrors.New(40490, http.StatusNotFound, "规则不存在")
	errRevisionAbsent = apperrors.New(40491, http.StatusNotFound, "修订不存在")
	errTooManyRules   = apperrors.New(40091, http.StatusBadRequest, "规则数量已达上限（200 条）")
)

const maxRules = 200

func validationError(errs []traffic.ValidationError) error {
	return apperrors.New(40091, http.StatusBadRequest, errs[0].Message)
}

// Overview 管理端总览：本实例运行态 + 集群各实例摘要。
type Overview struct {
	Plugin   *plugindomain.NativeView `json:"plugin"`
	Revision int64                    `json:"revision"`
	Local    traffic.Snapshot         `json:"local"`
	Nodes    []traffic.NodeSummary    `json:"nodes"`
	Cluster  ClusterView              `json:"cluster"`
}

// ClusterView 集群合计。
type ClusterView struct {
	Instances   int            `json:"instances"`
	LastSecond  traffic.Counts `json:"lastSecond"`
	Inflight    int64          `json:"inflight"`
	Coordinator string         `json:"coordinator"`
}

// Overview 组装总览。view 为宿主给出的插件视图。
func (p *Plugin) Overview(ctx context.Context, seconds int, view *plugindomain.NativeView) Overview {
	snap := p.engine.Snapshot(seconds)
	out := Overview{Plugin: view, Revision: snap.Revision, Local: snap, Nodes: []traffic.NodeSummary{}}
	out.Cluster.Coordinator = "none"
	if p.co != nil {
		out.Cluster.Coordinator = "redis"
		nctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		nodes, err := p.co.Nodes(nctx)
		cancel()
		if err == nil {
			out.Nodes = nodes
		}
	}
	// 本实例的摘要总是最新的，覆盖掉心跳里可能滞后 2 秒的那份。
	self := p.engine.Summary()
	found := false
	for i := range out.Nodes {
		if out.Nodes[i].Instance == self.Instance {
			out.Nodes[i], found = self, true
		}
	}
	if !found {
		out.Nodes = append([]traffic.NodeSummary{self}, out.Nodes...)
	}
	out.Cluster.Instances = len(out.Nodes)
	for _, n := range out.Nodes {
		out.Cluster.LastSecond = out.Cluster.LastSecond.Add(n.LastSec)
		out.Cluster.Inflight += n.Inflight
	}
	return out
}

// Config 当前配置（以数据库为准）。
func (p *Plugin) Config(ctx context.Context) (trafficdomain.ConfigView, error) {
	view, exists, err := p.store.TrafficLoadConfig(ctx)
	if err != nil {
		return view, err
	}
	if !exists {
		view.Config = p.engine.Config()
	}
	return view, nil
}

// SaveSettings 替换全局设置。
func (p *Plugin) SaveSettings(ctx context.Context, actor *int64, settings traffic.Settings, expected int64, note string) (trafficdomain.ConfigView, error) {
	settings = traffic.NormalizeSettings(settings)
	for i, path := range settings.ExemptPaths {
		if !strings.HasPrefix(path, "/") || strings.Count(path, "*") > 0 && !validWildcard(path) {
			return trafficdomain.ConfigView{}, apperrors.New(40091, http.StatusBadRequest, "免检路径第 "+itoa(i+1)+" 项写法无效")
		}
	}
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		cur.Settings = settings
		return trafficdomain.ActionSettings, defaultNote(note, "修改全局设置"), nil
	})
}

func validWildcard(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.Contains(seg, "*") && seg != "*" {
			return false
		}
	}
	return true
}

// Validate 归一化并校验一条规则（编辑器实时校验用）。
func (p *Plugin) Validate(rule traffic.Rule) (traffic.Rule, []traffic.ValidationError) {
	n := traffic.NormalizeRule(rule)
	errs := traffic.ValidateRule(n)
	if errs == nil {
		errs = []traffic.ValidationError{}
	}
	return n, errs
}

// CreateRule 新建规则。position = top 放到最前，其余追加到末尾。
func (p *Plugin) CreateRule(ctx context.Context, actor *int64, rule traffic.Rule, position string, expected int64, note string) (trafficdomain.ConfigView, error) {
	rule.ID = newRuleID()
	n, errs := p.Validate(rule)
	if len(errs) > 0 {
		return trafficdomain.ConfigView{}, validationError(errs)
	}
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		if len(cur.Rules) >= maxRules {
			return "", "", errTooManyRules
		}
		if position == "top" {
			cur.Rules = append([]traffic.Rule{n}, cur.Rules...)
		} else {
			cur.Rules = append(cur.Rules, n)
		}
		return trafficdomain.ActionRuleCreate, defaultNote(note, "新建规则「"+n.Name+"」"), nil
	})
}

// UpdateRule 修改规则（ID 不变，位置不变）。
func (p *Plugin) UpdateRule(ctx context.Context, actor *int64, id string, rule traffic.Rule, expected int64, note string) (trafficdomain.ConfigView, error) {
	rule.ID = id
	n, errs := p.Validate(rule)
	if len(errs) > 0 {
		return trafficdomain.ConfigView{}, validationError(errs)
	}
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		for i := range cur.Rules {
			if cur.Rules[i].ID == id {
				cur.Rules[i] = n
				return trafficdomain.ActionRuleUpdate, defaultNote(note, "修改规则「"+n.Name+"」"), nil
			}
		}
		return "", "", errRuleNotFound
	})
}

// DeleteRule 删除规则。
func (p *Plugin) DeleteRule(ctx context.Context, actor *int64, id string, expected int64) (trafficdomain.ConfigView, error) {
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		for i := range cur.Rules {
			if cur.Rules[i].ID == id {
				name := cur.Rules[i].Name
				cur.Rules = append(cur.Rules[:i], cur.Rules[i+1:]...)
				return trafficdomain.ActionRuleDelete, "删除规则「" + name + "」", nil
			}
		}
		return "", "", errRuleNotFound
	})
}

// ToggleRule 启用 / 停用规则。
func (p *Plugin) ToggleRule(ctx context.Context, actor *int64, id string, enabled bool, expected int64) (trafficdomain.ConfigView, error) {
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		for i := range cur.Rules {
			if cur.Rules[i].ID == id {
				cur.Rules[i].Enabled = enabled
				verb := "停用"
				if enabled {
					verb = "启用"
				}
				return trafficdomain.ActionRuleToggle, verb + "规则「" + cur.Rules[i].Name + "」", nil
			}
		}
		return "", "", errRuleNotFound
	})
}

// ReorderRules 调整匹配顺序。ids 必须恰好是现有规则的一个排列。
func (p *Plugin) ReorderRules(ctx context.Context, actor *int64, ids []string, expected int64) (trafficdomain.ConfigView, error) {
	return p.publish(ctx, expected, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		if len(ids) != len(cur.Rules) {
			return "", "", apperrors.New(40091, http.StatusBadRequest, "排序列表与现有规则不一致")
		}
		byID := make(map[string]traffic.Rule, len(cur.Rules))
		for _, r := range cur.Rules {
			byID[r.ID] = r
		}
		next := make([]traffic.Rule, 0, len(ids))
		for _, id := range ids {
			r, ok := byID[id]
			if !ok {
				return "", "", apperrors.New(40091, http.StatusBadRequest, "排序列表与现有规则不一致")
			}
			delete(byID, id)
			next = append(next, r)
		}
		cur.Rules = next
		return trafficdomain.ActionRuleReorder, "调整规则顺序", nil
	})
}

// Rollback 把某个历史修订的内容发布为新修订。
func (p *Plugin) Rollback(ctx context.Context, actor *int64, revision int64, note string) (trafficdomain.ConfigView, error) {
	detail, err := p.store.TrafficRevisionDetail(ctx, revision)
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	if detail == nil {
		return trafficdomain.ConfigView{}, errRevisionAbsent
	}
	return p.publish(ctx, 0, actor, func(cur *traffic.Config, _ bool) (string, string, error) {
		cur.Settings = detail.Config.Settings
		cur.Rules = append([]traffic.Rule(nil), detail.Config.Rules...)
		return trafficdomain.ActionRollback, defaultNote(note, "回滚到修订 "+itoa64(revision)), nil
	})
}

// Revisions 修订列表。
func (p *Plugin) Revisions(ctx context.Context, limit int) ([]trafficdomain.RevisionSummary, error) {
	return p.store.TrafficRevisions(ctx, limit)
}

// Revision 修订详情。
func (p *Plugin) Revision(ctx context.Context, revision int64) (*trafficdomain.RevisionDetail, error) {
	d, err := p.store.TrafficRevisionDetail(ctx, revision)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, errRevisionAbsent
	}
	return d, nil
}

// RuleStats 一条规则的运行态。
func (p *Plugin) RuleStats(id string, seconds int) (traffic.RuleStats, error) {
	rs, ok := p.engine.RuleStats(id, seconds)
	if !ok {
		return rs, errRuleNotFound
	}
	if rs.Series == nil {
		rs.Series = []traffic.SeriesPoint{}
	}
	return rs, nil
}

// Simulate 模拟请求。
func (p *Plugin) Simulate(in traffic.SimulateInput, draft *traffic.Rule) traffic.SimulateResult {
	return p.engine.Simulate(in, draft)
}

// History 历史曲线。range：1h | 6h | 24h | 7d。
func (p *Plugin) History(ctx context.Context, rng, ruleID string) (trafficdomain.History, error) {
	var span, step time.Duration
	switch rng {
	case "6h":
		span, step = 6*time.Hour, 5*time.Minute
	case "24h":
		span, step = 24*time.Hour, 15*time.Minute
	case "7d":
		span, step = 7*24*time.Hour, time.Hour
	default:
		rng, span, step = "1h", time.Hour, time.Minute
	}
	to := time.Now().UTC().Truncate(time.Minute).Add(time.Minute)
	from := to.Add(-span)
	points, totals, err := p.store.TrafficHistory(ctx, from, to, step, strings.TrimSpace(ruleID))
	if err != nil {
		return trafficdomain.History{}, err
	}
	return trafficdomain.History{Range: rng, StepSeconds: int(step.Seconds()), RuleID: ruleID, Points: points, Totals: totals}, nil
}

// Events 最近事件。
func (p *Plugin) Events(ctx context.Context, limit int, kind string) ([]trafficdomain.EventRecord, error) {
	return p.store.TrafficEvents(ctx, limit, strings.TrimSpace(kind))
}

// ResetStats 清零本实例统计。
func (p *Plugin) ResetStats() { p.engine.ResetStats() }

func (p *Plugin) publish(ctx context.Context, expected int64, actor *int64, mutate postgres.TrafficMutation) (trafficdomain.ConfigView, error) {
	view, err := p.store.TrafficPublish(ctx, expected, actor, mutate)
	if err != nil {
		if errors.Is(err, postgres.ErrTrafficRevisionConflict) {
			return view, errConflict
		}
		return view, err
	}
	p.applyLocal(view.Config)
	if p.co != nil {
		bctx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := p.co.PublishReload(bctx, view.Revision); err != nil {
			p.log.Warn("broadcast traffic reload failed", zap.Error(err))
		}
		cancel()
	}
	return view, nil
}

// ── 初始化与旧配置导入 ──

// legacyRampKey 旧版「突发流量爬坡」的平台设置键。
const legacyRampKey = "security.traffic_ramp"

// legacyRamp 旧配置的 JSON 形状（当年用无 tag 的结构体直接序列化，字段名即 Go 名）。
type legacyRamp struct {
	Enabled            bool
	BaselineRPS        int
	MaxRPS             int
	RampStepPct        int
	RampIntervalMs     int
	CooldownSeconds    int
	QueueSize          int
	QueueTimeoutMs     int
	MaxConcurrent      int
	ExemptPathPrefixes []string
	ExemptAdmin        bool
	RetryAfterSeconds  int
}

// seed 首次启动：写入默认设置；旧版爬坡配置存在时转换成一条等价规则。
func (p *Plugin) seed(ctx context.Context) (trafficdomain.ConfigView, error) {
	settings := traffic.DefaultSettings()
	var rules []traffic.Rule
	action, note := trafficdomain.ActionSeed, "初始化流量管理"
	if rec, err := p.store.GetPlatformSetting(ctx, legacyRampKey); err == nil && rec != nil && len(rec.Value) > 0 {
		var old legacyRamp
		old.ExemptAdmin = true
		if json.Unmarshal(rec.Value, &old) == nil {
			settings.ExemptAdmin = old.ExemptAdmin
			for _, prefix := range old.ExemptPathPrefixes {
				prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
				if prefix != "" {
					settings.ExemptPaths = append(settings.ExemptPaths, prefix+"/*")
				}
			}
			if old.RetryAfterSeconds > 0 {
				settings.Response.RetryAfterSeconds = old.RetryAfterSeconds
			}
			settings = traffic.NormalizeSettings(settings)
			rule := traffic.Rule{
				ID:      newRuleID(),
				Name:    "全站突发爬坡",
				Enabled: old.Enabled,
				Mode:    traffic.ModeEnforce,
				Class:   traffic.ClassNormal,
				Match:   traffic.MatchSpec{Paths: []string{"/*"}},
				Warmup: &traffic.WarmupSpec{
					BaselineRPS: old.BaselineRPS, MaxRPS: old.MaxRPS, StepPct: old.RampStepPct,
					IntervalMs: old.RampIntervalMs, CooldownSeconds: old.CooldownSeconds,
				},
			}
			if old.QueueSize > 0 {
				rule.Queue = &traffic.QueueSpec{Size: old.QueueSize, TimeoutMs: old.QueueTimeoutMs}
			}
			if old.MaxConcurrent > 0 {
				rule.Concurrency = &traffic.ConcurrencySpec{Max: old.MaxConcurrent}
			}
			rules = append(rules, traffic.NormalizeRule(rule))
			action, note = trafficdomain.ActionImport, "从突发流量爬坡导入"
		}
	}
	view, err := p.store.TrafficPublish(ctx, 0, nil, func(cur *traffic.Config, exists bool) (string, string, error) {
		if exists {
			// 另一个实例抢先完成了初始化：原样保留，只记一条无害的修订。
			return trafficdomain.ActionSeed, "初始化（已由其他实例完成）", nil
		}
		cur.Settings = settings
		cur.Rules = rules
		return action, note, nil
	})
	if err != nil {
		return view, err
	}
	p.log.Info("traffic config seeded", zap.String("action", action), zap.Int64("revision", view.Revision))
	return view, nil
}

func defaultNote(note, fallback string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		return fallback
	}
	return note
}

func itoa(i int) string     { return strconv.Itoa(i) }
func itoa64(i int64) string { return strconv.FormatInt(i, 10) }
