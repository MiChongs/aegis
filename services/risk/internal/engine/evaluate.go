package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/bundle"

	"github.com/expr-lang/expr"
	"github.com/gorules/zen-go"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
)

// RuleHit 一条规则的结论
type RuleHit struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Score     int    `json:"score"`
	Mode      string `json:"mode"`
	Decision  string `json:"decision,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Hit       bool   `json:"hit"`
	Error     string `json:"error,omitempty"`
}

// Command 处置命令
type Command struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Target          string `json:"target"`
	Value           string `json:"value"`
	DurationSeconds int64  `json:"durationSeconds,omitempty"`
	Reason          string `json:"reason,omitempty"`
	AppID           int64  `json:"appId,omitempty"`
}

// 命令种类
const (
	CmdListAdd        = "list_add"
	CmdAccountBan     = "account_ban"
	CmdFirewallBan    = "firewall_ban"
	CmdRevokeSessions = "revoke_sessions"
)

// StageTrace 一步的耗时与说明
type StageTrace struct {
	Stage    string  `json:"stage"`
	Micros   int64   `json:"micros"`
	Note     string  `json:"note,omitempty"`
	Degraded bool    `json:"degraded,omitempty"`
	Error    string  `json:"error,omitempty"`
	Result   any     `json:"result,omitempty"`
	Sources  []Stamp `json:"sources,omitempty"`
}

// Stamp 一个数据源的耗时
type Stamp struct {
	Source string `json:"source"`
	Micros int64  `json:"micros"`
	Error  string `json:"error,omitempty"`
}

// Trace 完整轨迹（模拟器、决策详情页）
type Trace struct {
	Stages   []StageTrace       `json:"stages"`
	Env      map[string]any     `json:"env"`
	Features map[string]float64 `json:"features"`
	Rules    []RuleHit          `json:"rules"`
	Flow     json.RawMessage    `json:"flow,omitempty"`
	Policy   map[string]any     `json:"policy,omitempty"`
}

// Result 一次评估的结论
type Result struct {
	Action     string             `json:"action"`
	Level      string             `json:"level"`
	Score      int                `json:"score"`
	Reasons    []string           `json:"reasons"`
	Hits       []RuleHit          `json:"hits"`
	Dimensions map[string]int     `json:"dimensions"`
	Models     map[string]float64 `json:"models"`
	Features   map[string]float64 `json:"features"`
	ListHits   []ListHit          `json:"listHits"`
	DecidedBy  string             `json:"decidedBy"`
	Commands   []Command          `json:"commands,omitempty"`
	Degraded   []string           `json:"degraded,omitempty"`
	Release    string             `json:"release"`
	Env        *Env               `json:"-"`
	Trace      *Trace             `json:"trace,omitempty"`
}

// Options 求值选项
type Options struct {
	DecisionID string
	Explain    bool
	// Overrides 模拟器手动给的特征值：取完数据源之后直接覆盖（主体缺失、本该取不到的特征也照样生效）
	Overrides map[string]float64
}

// stage 计时器
type stageClock struct {
	trace *Trace
	start time.Time
}

func (c *stageClock) done(stage, note string, result any) *StageTrace {
	if c.trace == nil {
		return nil
	}
	c.trace.Stages = append(c.trace.Stages, StageTrace{Stage: stage, Micros: time.Since(c.start).Microseconds(), Note: note, Result: result})
	c.start = time.Now()
	return &c.trace.Stages[len(c.trace.Stages)-1]
}

// Evaluate 跑完整条管线
func (p *Program) Evaluate(ctx context.Context, req Request, src Sources, opts Options) (*Result, error) {
	if src == nil {
		src = NopSources{}
	}
	req.Normalize()
	env := BuildEnv(req)
	res := &Result{
		Dimensions: map[string]int{}, Models: env.M, Features: env.F, Release: p.Bundle.Version,
		Reasons: []string{}, Hits: []RuleHit{}, ListHits: []ListHit{},
	}
	var trace *Trace
	if opts.Explain {
		trace = &Trace{}
	}
	clock := &stageClock{trace: trace, start: time.Now()}

	budget := time.Duration(p.Bundle.Scene.TimeoutMS) * time.Millisecond
	if budget <= 0 {
		budget = 200 * time.Millisecond
	}
	// 外部数据源最多用掉总预算的 70%，剩下留给本地计算与发布事件
	fetchCtx, cancel := context.WithTimeout(ctx, budget*7/10)
	defer cancel()

	// 1. 名单 + 2. 特征：并行
	p.fetch(fetchCtx, &env, req, src, res, clock)
	for k, v := range opts.Overrides {
		if _, ok := p.features[k]; ok {
			env.F[k] = v
		}
	}

	// 3. 派生特征
	for _, d := range p.derived {
		out, err := expr.Run(d.prog, env)
		if err != nil {
			env.F[d.key] = d.def
			res.Degraded = append(res.Degraded, "derived:"+d.key)
			continue
		}
		env.F[d.key] = toFloat(out)
	}
	clock.done("derived", fmt.Sprintf("%d 个派生特征", len(p.derived)), nil)

	// 4. 模型
	for _, m := range p.models {
		score, err := m.predict(&env)
		if err != nil {
			res.Degraded = append(res.Degraded, "model:"+m.Key)
			continue
		}
		env.M[m.Key] = score
	}
	clock.done("models", fmt.Sprintf("%d 个模型", len(p.models)), env.M)

	// 5. 规则
	forced := ""
	evaluated := make([]RuleHit, 0, len(p.rules))
	for _, r := range p.rules {
		hit := RuleHit{Key: r.Key, Name: r.Name, Dimension: r.Dimension, Score: r.Score, Mode: r.Mode, Decision: r.Decision}
		out, err := expr.Run(r.prog, env)
		if err != nil {
			hit.Error = err.Error()
			evaluated = append(evaluated, hit)
			continue
		}
		hit.Hit, _ = out.(bool)
		if hit.Hit {
			hit.Reason = renderReason(r.Reason, r.Name, &env)
			res.Hits = append(res.Hits, hit)
			if r.Mode == bundle.ModeEnforce {
				res.Dimensions[r.Dimension] += r.Score
				res.Score += r.Score
				if r.Decision != "" {
					forced = bundle.Stricter(forced, r.Decision)
				}
				res.Reasons = append(res.Reasons, hit.Reason)
			}
		}
		evaluated = append(evaluated, hit)
	}
	if res.Score < 0 {
		res.Score = 0
	}
	clock.done("rules", fmt.Sprintf("%d 条参评，%d 条命中", len(p.rules), len(res.Hits)), nil)

	// 6. 决策流 / 分数段
	proposed := map[string]any{"action": bundle.ActionPass, "level": bundle.LevelNone, "reasons": res.Reasons, "decided_by": "default"}
	envMap := env.asMap()
	dims := make(map[string]any, len(res.Dimensions))
	for k, v := range res.Dimensions {
		dims[k] = v
	}
	flowInput := map[string]any{
		"request": envMap, "features": envMap["F"], "models": envMap["M"], "score": res.Score, "dimensions": dims,
		"hits": hitKeys(res.Hits), "forced": forced, "lists": listSummary(&env, res.ListHits), "scene": p.Bundle.Scene.Key,
	}
	if p.flow != nil {
		action, level, reason, raw, err := p.runFlow(flowInput, opts.Explain)
		if err != nil {
			res.Degraded = append(res.Degraded, "flow")
			if st := clock.done("flow", "决策流出错，按分数段", nil); st != nil {
				st.Error = err.Error()
			}
			action, level = p.band(res.Score)
		} else {
			proposed["decided_by"] = "flow"
			if reason != "" {
				res.Reasons = append(res.Reasons, reason)
			}
			if trace != nil {
				trace.Flow = raw
			}
			clock.done("flow", "决策流 "+action, map[string]any{"action": action, "level": level})
		}
		proposed["action"], proposed["level"] = action, level
	} else {
		action, level := p.band(res.Score)
		proposed["action"], proposed["level"] = action, level
		if res.Score > 0 {
			proposed["decided_by"] = "rule"
		}
		clock.done("flow", "分数段 "+action, nil)
	}
	if forced != "" && bundle.ActionRank(forced) > bundle.ActionRank(proposed["action"].(string)) {
		proposed["action"] = forced
		proposed["decided_by"] = "rule"
	}
	if def := p.Bundle.Scene.DefaultAction; def != "" && proposed["decided_by"] == "default" {
		proposed["action"] = def
	}

	// 7. 策略
	reasons := make([]any, len(res.Reasons))
	for i, r := range res.Reasons {
		reasons[i] = r
	}
	proposed["reasons"] = reasons
	policyInput := map[string]any{
		"request": envMap, "scene": p.sceneMap, "features": envMap["F"], "models": envMap["M"], "score": res.Score,
		"dimensions": dims, "hits": flowInput["hits"], "lists": flowInput["lists"], "proposed": proposed,
	}
	final, policyCommands, err := p.runPolicy(ctx, policyInput)
	if err != nil {
		res.Degraded = append(res.Degraded, "policy")
		final = proposed
		if st := clock.done("policy", "策略出错，沿用提议", nil); st != nil {
			st.Error = err.Error()
		}
	} else {
		clock.done("policy", "裁决 "+fmt.Sprint(final["action"]), final)
	}
	res.Action, _ = final["action"].(string)
	if !bundle.ValidAction(res.Action) {
		res.Action = bundle.ActionPass
	}
	res.Level, _ = final["level"].(string)
	// 护栏或策略把处置改重了，等级至少跟到这个处置对应的那一档
	if levelRank(res.Level) < levelRank(levelFor(res.Action)) {
		res.Level = levelFor(res.Action)
	}
	res.DecidedBy, _ = final["decided_by"].(string)
	if rs, ok := final["reasons"].([]any); ok {
		res.Reasons = res.Reasons[:0]
		for _, r := range rs {
			if s, ok := r.(string); ok && s != "" {
				res.Reasons = append(res.Reasons, s)
			}
		}
	}

	// 8. 处置命令
	res.Commands = p.commands(opts.DecisionID, &env, res.Action, policyCommands)

	res.Env = &env
	if trace != nil {
		trace.Env = envMap
		trace.Features = env.F
		trace.Rules = evaluated
		trace.Policy = final
		res.Trace = trace
	}
	return res, nil
}

// fetch 名单与特征并行取
func (p *Program) fetch(ctx context.Context, env *Env, req Request, src Sources, res *Result, clock *stageClock) {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		stamps []Stamp
	)
	run := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start := time.Now()
			err := fn()
			st := Stamp{Source: name, Micros: time.Since(start).Microseconds()}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				st.Error = err.Error()
				res.Degraded = append(res.Degraded, name)
			}
			stamps = append(stamps, st)
		}()
	}
	merge := func(values map[string]float64) {
		mu.Lock()
		for k, v := range values {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				env.F[k] = v
			}
		}
		mu.Unlock()
	}

	// 名单
	lq := ListQuery{AppID: env.AppID, Scene: env.Scene, Values: map[string]string{}}
	putList := func(t, v string) {
		if v != "" && (len(p.listSet) == 0 || p.listSet[t]) {
			lq.Values[t] = v
		}
	}
	putList(bundle.ListIP, env.IP)
	putList(bundle.ListCIDR, env.IP)
	putList(bundle.ListDevice, env.DeviceID)
	putList(bundle.ListAccount, strings.ToLower(env.Account))
	if env.UserID > 0 {
		putList(bundle.ListUser, itoa(env.UserID))
	}
	putList(bundle.ListEmailDomain, env.EmailDomain)
	putList(bundle.ListPhonePrefix, env.PhonePrefix)
	if env.Net.ASN > 0 {
		putList(bundle.ListASN, itoa(env.Net.ASN))
	}
	putList(bundle.ListCountry, env.Net.Country)
	putList(bundle.ListUAKeyword, env.UA)
	if len(lq.Values) > 0 {
		run("lists", func() error {
			hits, err := src.CheckLists(ctx, lq)
			if err != nil {
				return err
			}
			mu.Lock()
			res.ListHits = append(res.ListHits, hits...)
			mu.Unlock()
			return nil
		})
	}

	// 特征按来源分组
	var (
		vel    []VelocityQuery
		stream []LookupQuery
		graph  []GraphQuery
		vector []VectorQuery
	)
	for _, f := range p.Bundle.Features {
		if f.Kind != bundle.FeatureDerived {
			env.F[f.Key] = f.Default
		}
		switch f.Kind {
		case bundle.FeatureVelocity:
			value := EntityValue(env, f.Entity)
			if value == "" {
				continue
			}
			q := VelocityQuery{Feature: f.Key, WindowSeconds: f.Velocity.WindowSeconds, Key: p.counterKey(f, env, value)}
			if f.Velocity.Agg == bundle.AggDistinct {
				q.Distinct = EntityValue(env, f.Velocity.DistinctOf)
				if q.Distinct == "" {
					continue
				}
			}
			vel = append(vel, q)
		case bundle.FeatureStream:
			if value := EntityValue(env, f.Entity); value != "" {
				stream = append(stream, LookupQuery{Feature: f.Key, Key: StreamKey(f, env.AppID, value)})
			}
		case bundle.FeatureGraph:
			entity := f.Entity
			if entity == "" {
				entity = bundle.EntityUser
			}
			if value := EntityValue(env, entity); value != "" {
				graph = append(graph, GraphQuery{Feature: f.Key, Template: f.Graph.Template, Entity: entity, AppID: env.AppID, Value: value, WithinDays: f.Graph.WithinDays, CacheTTL: f.Graph.CacheTTL})
			}
		case bundle.FeatureVector:
			if env.DeviceID != "" {
				vector = append(vector, VectorQuery{Feature: f.Key, Collection: f.Vector.Collection, AppID: env.AppID, TopK: f.Vector.TopK, MinScore: f.Vector.MinScore, Output: f.Vector.Output, Self: env.DeviceID})
			}
		}
	}
	if len(vel) > 0 {
		run("velocity", func() error { v, err := src.Velocity(ctx, vel); merge(v); return err })
	}
	if len(stream) > 0 {
		run("stream", func() error { v, err := src.Stream(ctx, stream); merge(v); return err })
	}
	if len(graph) > 0 {
		run("graph", func() error { v, err := src.Graph(ctx, graph); merge(v); return err })
	}
	if len(vector) > 0 {
		run("vector", func() error { v, err := src.Vector(ctx, vector, req.Device); merge(v); return err })
	}
	wg.Wait()

	// 名单结论进环境
	for _, h := range res.ListHits {
		switch h.Kind {
		case bundle.KindBlock:
			env.List.Block = true
		case bundle.KindAllow:
			env.List.Allow = true
		case bundle.KindWatch:
			env.List.Watch = true
		}
		if !contains(env.List.Types, h.ListType) {
			env.List.Types = append(env.List.Types, h.ListType)
		}
	}
	sort.Strings(res.Degraded)
	if st := clock.done("fetch", fmt.Sprintf("名单 %d 项、特征 %d 个", len(lq.Values), len(vel)+len(stream)+len(graph)+len(vector)), nil); st != nil {
		st.Sources = stamps
		st.Degraded = len(res.Degraded) > 0
	}
}

// counterKey 滑窗计数键：特征 + 作用域 + 主体
func (p *Program) counterKey(f bundle.Feature, env *Env, value string) string {
	var b strings.Builder
	b.WriteString(f.Key)
	b.WriteByte(':')
	if f.Global {
		b.WriteByte('g')
	} else {
		b.WriteString(itoa(env.AppID))
	}
	b.WriteByte(':')
	if f.Velocity.AllScenes {
		b.WriteByte('*')
	} else {
		b.WriteString(env.Scene)
	}
	b.WriteByte(':')
	b.WriteString(value)
	return b.String()
}

// StreamKey 流式特征在 Redis 里的键（不含前缀）：特征 + 应用（全局为 g）+ 主体
func StreamKey(f bundle.Feature, appID int64, value string) string {
	scope := itoa(appID)
	if f.Global {
		scope = "g"
	}
	return f.Key + ":" + scope + ":" + value
}

func (p *Program) band(score int) (string, string) {
	bands := p.Bundle.Bands
	if len(bands) == 0 {
		bands = bundle.DefaultBands()
	}
	for _, b := range bands {
		if score >= b.Min && (b.Max <= 0 || score <= b.Max) {
			level := b.Level
			if level == "" {
				level = levelFor(b.Action)
			}
			return b.Action, level
		}
	}
	return bundle.ActionPass, bundle.LevelNone
}

func (p *Program) runFlow(input map[string]any, explain bool) (action, level, reason string, raw json.RawMessage, err error) {
	raw, err = json.Marshal(input)
	if err != nil {
		return "", "", "", nil, err
	}
	resp, err := p.flow.EvaluateWithOpts(raw, zen.EvaluationOptions{Trace: explain, MaxDepth: 8})
	if err != nil {
		return "", "", "", nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Result, &out); err != nil {
		return "", "", "", nil, fmt.Errorf("决策流输出不是对象：%w", err)
	}
	if inner, ok := out["decision"].(map[string]any); ok {
		out = inner
	}
	action, _ = out["action"].(string)
	if !bundle.ValidAction(action) {
		return "", "", "", nil, fmt.Errorf("决策流输出的 action %q 不合法", action)
	}
	level, _ = out["level"].(string)
	if level == "" {
		level = levelFor(action)
	}
	reason, _ = out["reason"].(string)
	raw = nil
	if explain && resp.Trace != nil {
		raw = *resp.Trace
	}
	return action, level, reason, raw, nil
}

func (p *Program) runPolicy(ctx context.Context, input map[string]any) (map[string]any, []any, error) {
	var commands []any
	if p.scene != nil {
		value, err := ast.InterfaceToValue(input)
		if err != nil {
			return nil, nil, err
		}
		rs, err := p.scene.Eval(ctx, rego.EvalParsedInput(value))
		if err != nil {
			return nil, nil, fmt.Errorf("场景策略：%w", err)
		}
		if d := firstObject(rs); d != nil {
			proposed := input["proposed"].(map[string]any)
			for k, v := range d {
				if k == "commands" {
					commands, _ = v.([]any)
					continue
				}
				proposed[k] = v
			}
			if _, ok := d["action"]; ok {
				proposed["decided_by"] = "policy"
			}
			if _, ok := proposed["reasons"].([]any); !ok {
				proposed["reasons"] = []any{}
			}
		}
	}
	value, err := ast.InterfaceToValue(input)
	if err != nil {
		return nil, nil, err
	}
	rs, err := p.guard.Eval(ctx, rego.EvalParsedInput(value))
	if err != nil {
		return nil, nil, fmt.Errorf("护栏策略：%w", err)
	}
	d := firstObject(rs)
	if d == nil {
		return nil, nil, fmt.Errorf("护栏策略没有输出")
	}
	return d, commands, nil
}

// commands 最终为 ban 时按场景的处置对象出命令；策略给的命令一并带上
func (p *Program) commands(decisionID string, env *Env, action string, extra []any) []Command {
	var out []Command
	enf := p.Bundle.Scene.Enforcement
	reason := "风控自动处置（场景 " + p.Bundle.Scene.Key + "）"
	add := func(kind, target, value string, duration int64) {
		if value == "" {
			return
		}
		out = append(out, Command{ID: decisionID + ":" + kind + ":" + target, Kind: kind, Target: target, Value: value, DurationSeconds: duration, Reason: reason, AppID: env.AppID})
	}
	if action == bundle.ActionBan {
		for _, t := range enf.Targets {
			switch t {
			case "device":
				add(CmdListAdd, "device", env.DeviceID, enf.DurationSeconds)
			case "ip":
				add(CmdListAdd, "ip", env.IP, enf.DurationSeconds)
			case "account":
				if env.UserID > 0 {
					add(CmdAccountBan, "user", itoa(env.UserID), enf.DurationSeconds)
					add(CmdRevokeSessions, "user", itoa(env.UserID), 0)
				} else {
					add(CmdListAdd, "account", strings.ToLower(env.Account), enf.DurationSeconds)
				}
			case "firewall":
				add(CmdFirewallBan, "ip", env.IP, enf.DurationSeconds)
			}
		}
	}
	for i, raw := range extra {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		target, _ := m["target"].(string)
		value, _ := m["value"].(string)
		if value == "" {
			value = targetValue(env, target)
		}
		dur, _ := m["duration_seconds"].(json.Number)
		d, _ := dur.Int64()
		switch kind {
		case CmdListAdd, CmdAccountBan, CmdFirewallBan, CmdRevokeSessions:
			if value != "" {
				c := Command{ID: fmt.Sprintf("%s:policy:%d", decisionID, i), Kind: kind, Target: target, Value: value, DurationSeconds: d, AppID: env.AppID}
				c.Reason, _ = m["reason"].(string)
				if c.Reason == "" {
					c.Reason = reason
				}
				out = append(out, c)
			}
		}
	}
	return out
}

func targetValue(env *Env, target string) string {
	switch target {
	case "device":
		return env.DeviceID
	case "ip":
		return env.IP
	case "user":
		if env.UserID > 0 {
			return itoa(env.UserID)
		}
	case "account":
		return strings.ToLower(env.Account)
	}
	return ""
}

func (m compiledModel) predict(env *Env) (float64, error) {
	if m.ensemble == nil {
		return 0, errNoModel
	}
	values := make([]float64, len(m.Inputs))
	for i, in := range m.Inputs {
		if v, ok := env.F[in]; ok {
			values[i] = v
			continue
		}
		values[i] = requestValue(env, in)
	}
	return m.ensemble.PredictSingle(values, 0), nil
}

func requestValue(env *Env, name string) float64 {
	b := func(v bool) float64 {
		if v {
			return 1
		}
		return 0
	}
	switch name {
	case "amount":
		return env.Amount
	case "hour":
		return float64(env.Hour)
	case "weekday":
		return float64(env.Weekday)
	case "logged_in":
		return b(env.LoggedIn)
	case "captcha_passed":
		return b(env.CaptchaPassed)
	case "device.emulator":
		return b(env.Device.Emulator)
	case "device.rooted":
		return b(env.Device.Rooted)
	case "device.present":
		return b(env.Device.Present)
	case "client.bot":
		return b(env.Client.Bot)
	case "client.headless":
		return b(env.Client.Headless)
	case "client.mobile":
		return b(env.Client.Mobile)
	case "client.empty":
		return b(env.Client.Empty)
	case "client.length":
		return float64(env.Client.Length)
	case "net.proxy":
		return b(env.Net.Proxy)
	case "net.vpn":
		return b(env.Net.VPN)
	case "net.tor":
		return b(env.Net.Tor)
	case "net.datacenter":
		return b(env.Net.Datacenter)
	case "net.score":
		return float64(env.Net.Score)
	case "net.ipv6":
		return b(env.Net.IPv6)
	case "net.asn":
		return float64(env.Net.ASN)
	case "list.block":
		return b(env.List.Block)
	case "list.allow":
		return b(env.List.Allow)
	case "list.watch":
		return b(env.List.Watch)
	}
	if strings.HasPrefix(name, "extra.") {
		return toFloat(env.Extra[strings.TrimPrefix(name, "extra.")])
	}
	return math.NaN()
}

func toFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	case int32:
		return float64(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return 0
}

func levelFor(action string) string {
	switch action {
	case bundle.ActionReview:
		return bundle.LevelHigh
	case bundle.ActionCaptcha:
		return bundle.LevelMedium
	case bundle.ActionBlock, bundle.ActionBan:
		return bundle.LevelCritical
	}
	return bundle.LevelNone
}

func levelRank(level string) int {
	switch level {
	case bundle.LevelLow:
		return 1
	case bundle.LevelMedium:
		return 2
	case bundle.LevelHigh:
		return 3
	case bundle.LevelCritical:
		return 4
	}
	return 0
}

func hitKeys(hits []RuleHit) []any {
	out := make([]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Key)
	}
	return out
}

func listSummary(env *Env, hits []ListHit) map[string]any {
	types := make([]any, len(env.List.Types))
	for i, t := range env.List.Types {
		types[i] = t
	}
	hs := make([]any, len(hits))
	for i, h := range hits {
		hs[i] = map[string]any{"list_type": h.ListType, "kind": h.Kind, "value": h.Value, "list_key": h.ListKey, "reason": h.Reason}
	}
	return map[string]any{"block": env.List.Block, "allow": env.List.Allow, "watch": env.List.Watch, "types": types, "hits": hs}
}

// renderReason 规则说明里可以写 {{F.x}}、{{ip}} 之类的占位
func renderReason(tpl, fallback string, env *Env) string {
	if tpl == "" {
		return fallback
	}
	if !strings.Contains(tpl, "{{") {
		return tpl
	}
	var b strings.Builder
	rest := tpl
	for {
		i := strings.Index(rest, "{{")
		if i < 0 {
			b.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], "}}")
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:i])
		src := strings.TrimSpace(rest[i+2 : i+j])
		if out, err := expr.Eval(src, *env); err == nil {
			if f, ok := out.(float64); ok && f == math.Trunc(f) {
				b.WriteString(fmt.Sprintf("%.0f", f))
			} else {
				b.WriteString(fmt.Sprint(out))
			}
		}
		rest = rest[i+j+2:]
	}
	return b.String()
}

func firstObject(rs rego.ResultSet) map[string]any {
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return nil
	}
	m, _ := rs[0].Expressions[0].Value.(map[string]any)
	return m
}
