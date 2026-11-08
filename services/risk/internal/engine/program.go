package engine

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/bundle"

	"github.com/dmitryikh/leaves"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/gorules/zen-go"
	"github.com/open-policy-agent/opa/v1/rego"
)

//go:embed guard.rego
var guardPolicy string

// GuardPolicy 内置护栏策略（控制台只读展示）
func GuardPolicy() string { return guardPolicy }

// ModelLoader 按引用取模型文件（对象存储，按 checksum 缓存）
type ModelLoader interface {
	Load(ctx context.Context, ref bundle.Model) (*leaves.Ensemble, error)
}

// Issue 编译问题
type Issue struct {
	Stage   string `json:"stage"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

// CompileError 编译失败：带全部问题，控制台逐条标在对应配置上
type CompileError struct{ Issues []Issue }

func (e *CompileError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, fmt.Sprintf("%s/%s: %s", i.Stage, i.Key, i.Message))
	}
	return "发布包编译失败：" + strings.Join(parts, "；")
}

type compiledRule struct {
	bundle.Rule
	prog *vm.Program
}

type compiledDerived struct {
	key  string
	prog *vm.Program
	def  float64
}

type compiledModel struct {
	bundle.Model
	ensemble *leaves.Ensemble
}

// Program 编译好的发布包，可并发求值
type Program struct {
	Bundle   bundle.Bundle
	rules    []compiledRule
	derived  []compiledDerived
	models   []compiledModel
	flow     zen.Decision
	scene    *rego.PreparedEvalQuery
	guard    rego.PreparedEvalQuery
	features map[string]bundle.Feature
	listSet  map[string]bool
	sceneMap map[string]any // 场景定义的普通对象形态（策略输入）

	disposeOnce sync.Once
}

var (
	zenOnce   sync.Once
	zenEngine zen.Engine
)

func sharedZen() zen.Engine {
	zenOnce.Do(func() { zenEngine = zen.NewEngine(zen.EngineConfig{}) })
	return zenEngine
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidKey 特征 / 规则 / 模型的键：小写字母开头，字母数字下划线
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

// exprOptions 规则与派生特征共用的编译选项
func exprOptions(extra ...expr.Option) []expr.Option {
	// WarnOnAny 要排在 AsBool / AsFloat64 之后才生效（它在应用时检查期望类型）
	opts := append([]expr.Option{expr.Env(Env{})}, extra...)
	return append(opts, expr.WarnOnAny())
}

// CompileExpr 单独校验一条布尔表达式（控制台即时校验用）
func CompileExpr(src string) error {
	_, err := expr.Compile(src, exprOptions(expr.AsBool())...)
	return err
}

// Compile 编译发布包。models 为 nil 时跳过模型加载（只做语法校验）。
func Compile(ctx context.Context, b bundle.Bundle, models ModelLoader) (*Program, error) {
	var issues []Issue
	add := func(stage, key, format string, args ...any) {
		issues = append(issues, Issue{Stage: stage, Key: key, Message: fmt.Sprintf(format, args...)})
	}
	p := &Program{Bundle: b, features: map[string]bundle.Feature{}, listSet: map[string]bool{}}

	if strings.TrimSpace(b.Scene.Key) == "" {
		add("scene", "", "场景键为空")
	}
	if b.Scene.DefaultAction != "" && !bundle.ValidAction(b.Scene.DefaultAction) {
		add("scene", b.Scene.Key, "默认处置 %q 不合法", b.Scene.DefaultAction)
	}
	if raw, err := json.Marshal(b.Scene); err == nil {
		_ = json.Unmarshal(raw, &p.sceneMap)
	}
	for _, t := range b.ListTypes {
		p.listSet[t] = true
	}

	// 特征
	for _, f := range b.Features {
		if !ValidKey(f.Key) {
			add("features", f.Key, "特征键只能用小写字母、数字、下划线，且以字母开头")
			continue
		}
		if _, dup := p.features[f.Key]; dup {
			add("features", f.Key, "特征键重复")
			continue
		}
		p.features[f.Key] = f
		switch f.Kind {
		case bundle.FeatureVelocity:
			if f.Velocity == nil || f.Velocity.WindowSeconds <= 0 {
				add("features", f.Key, "滑窗计数要设窗口")
			} else if f.Velocity.WindowSeconds > 30*86400 {
				add("features", f.Key, "滑窗最长 30 天，更长的窗口请用流式特征")
			} else if f.Velocity.Agg == bundle.AggDistinct && !validEntity(f.Velocity.DistinctOf) {
				add("features", f.Key, "去重计数要指定数哪个主体")
			}
			if !validEntity(f.Entity) {
				add("features", f.Key, "主体 %q 不合法", f.Entity)
			}
		case bundle.FeatureStream:
			if f.Stream == nil || len(f.Stream.Events) == 0 || f.Stream.WindowSeconds <= 0 {
				add("features", f.Key, "流式特征要设事件与窗口")
			}
			if !validEntity(f.Entity) {
				add("features", f.Key, "主体 %q 不合法", f.Entity)
			}
		case bundle.FeatureGraph:
			if f.Graph == nil || !contains(bundle.GraphTemplates(), f.Graph.Template) {
				add("features", f.Key, "图特征模板不存在")
			}
		case bundle.FeatureVector:
			if f.Vector == nil || f.Vector.TopK <= 0 || f.Vector.TopK > 100 {
				add("features", f.Key, "向量特征的 TopK 要在 1-100 之间")
			}
		case bundle.FeatureDerived:
			prog, err := expr.Compile(f.Expr, exprOptions(expr.AsFloat64())...)
			if err != nil {
				// 布尔表达式也允许：真记 1、假记 0
				prog, err = expr.Compile(f.Expr, exprOptions(expr.AsBool())...)
			}
			if err != nil {
				add("features", f.Key, "派生表达式编译失败：%v", err)
				continue
			}
			p.derived = append(p.derived, compiledDerived{key: f.Key, prog: prog, def: f.Default})
		default:
			add("features", f.Key, "未知特征种类 %q", f.Kind)
		}
	}

	// 模型
	seenModel := map[string]bool{}
	for _, m := range b.Models {
		if !ValidKey(m.Key) || seenModel[m.Key] {
			add("models", m.Key, "模型键不合法或重复")
			continue
		}
		seenModel[m.Key] = true
		if m.Format != bundle.ModelLightGBM && m.Format != bundle.ModelXGBoost {
			add("models", m.Key, "不支持的模型格式 %q", m.Format)
			continue
		}
		for _, in := range m.Inputs {
			if _, ok := p.features[in]; !ok && !requestInput(in) {
				add("models", m.Key, "模型输入 %q 既不是特征也不是请求字段", in)
			}
		}
		cm := compiledModel{Model: m}
		if models != nil {
			ens, err := models.Load(ctx, m)
			if err != nil {
				add("models", m.Key, "加载模型失败：%v", err)
				continue
			}
			if ens.NFeatures() != len(m.Inputs) {
				add("models", m.Key, "模型需要 %d 个输入，映射了 %d 个", ens.NFeatures(), len(m.Inputs))
				continue
			}
			cm.ensemble = ens
		}
		p.models = append(p.models, cm)
	}

	// 规则
	seenRule := map[string]bool{}
	for _, r := range b.Rules {
		if !ValidKey(r.Key) || seenRule[r.Key] {
			add("rules", r.Key, "规则键不合法或重复")
			continue
		}
		seenRule[r.Key] = true
		if r.Decision != "" && !bundle.ValidAction(r.Decision) {
			add("rules", r.Key, "强制处置 %q 不合法", r.Decision)
		}
		if r.Mode != bundle.ModeEnforce && r.Mode != bundle.ModeObserve {
			add("rules", r.Key, "模式只能是 enforce 或 observe")
		}
		prog, err := expr.Compile(r.Expr, exprOptions(expr.AsBool())...)
		if err != nil {
			add("rules", r.Key, "条件编译失败：%v", err)
			continue
		}
		for _, ref := range featureRefs(r.Expr) {
			if _, ok := p.features[ref]; !ok {
				add("rules", r.Key, "引用了不存在的特征 F.%s", ref)
			}
		}
		p.rules = append(p.rules, compiledRule{Rule: r, prog: prog})
	}
	sort.SliceStable(p.rules, func(i, j int) bool { return p.rules[i].Priority > p.rules[j].Priority })

	// 分数段
	for i, band := range b.Bands {
		if !bundle.ValidAction(band.Action) {
			add("bands", fmt.Sprint(i), "处置 %q 不合法", band.Action)
		}
	}

	// 决策流
	if len(strings.TrimSpace(string(b.Flow))) > 0 && string(b.Flow) != "null" {
		d, err := sharedZen().CreateDecision(b.Flow)
		if err != nil {
			add("flow", b.Scene.Key, "决策流无法加载：%v", err)
		} else {
			p.flow = d
		}
	}

	// 策略
	guard, err := rego.New(rego.Query("data.aegis.risk.guard.decision"), rego.Module("guard.rego", guardPolicy)).PrepareForEval(ctx)
	if err != nil {
		add("policy", "guard", "内置护栏策略编译失败：%v", err)
	}
	p.guard = guard
	if strings.TrimSpace(b.Policy) != "" {
		if !strings.Contains(b.Policy, "package aegis.risk.scene") {
			add("policy", b.Scene.Key, "场景策略的包名必须是 aegis.risk.scene")
		} else {
			q, err := rego.New(rego.Query("data.aegis.risk.scene.decision"), rego.Module("scene.rego", b.Policy)).PrepareForEval(ctx)
			if err != nil {
				add("policy", b.Scene.Key, "场景策略编译失败：%v", err)
			} else {
				p.scene = &q
			}
		}
	}

	if len(issues) > 0 {
		p.Dispose()
		return nil, &CompileError{Issues: issues}
	}
	return p, nil
}

// Dispose 释放决策流占用的原生内存。只能在没有请求还在用这个程序时调用。
func (p *Program) Dispose() {
	if p == nil {
		return
	}
	p.disposeOnce.Do(func() {
		if p.flow != nil {
			p.flow.Dispose()
		}
	})
}

// DisposeLater 版本切换后延迟释放：在途请求有超时上限，过了宽限期一定已经用完
func (p *Program) DisposeLater(grace time.Duration) {
	if p == nil {
		return
	}
	time.AfterFunc(grace, p.Dispose)
}

func validEntity(e string) bool { return contains(bundle.Entities(), e) }

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var featureRefPattern = regexp.MustCompile(`\bF\.([a-z][a-z0-9_]*)|\bF\["([a-z][a-z0-9_]*)"\]`)

// featureRefs 表达式引用的特征键（F.x 与 F["x"] 两种写法）
func featureRefs(src string) []string {
	var out []string
	for _, m := range featureRefPattern.FindAllStringSubmatch(src, -1) {
		if m[1] != "" {
			out = append(out, m[1])
		} else {
			out = append(out, m[2])
		}
	}
	return out
}

// requestInput 模型可以直接用的请求字段
func requestInput(name string) bool {
	switch name {
	case "amount", "hour", "weekday", "logged_in", "captcha_passed", "device.emulator", "device.rooted", "device.present",
		"client.bot", "client.headless", "client.mobile", "client.empty", "client.length",
		"net.proxy", "net.vpn", "net.tor", "net.datacenter", "net.score", "net.ipv6", "net.asn",
		"list.block", "list.allow", "list.watch":
		return true
	}
	return strings.HasPrefix(name, "extra.")
}

var errNoModel = errors.New("模型未加载")
