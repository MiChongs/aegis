package firewall

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

// 自定义规则的动作。
const (
	// ActionAllow 放行：跳过其余入口检查与 WAF（封禁仍先于规则生效）。
	ActionAllow = "allow"
	// ActionBlock 拦截（可选同时封禁来源 IP 一段时间）。
	ActionBlock = "block"
	// ActionLog 只记录一条事件，继续往下检查。
	ActionLog = "log"
	// ActionSkipWAF 继续入口检查，但这次请求不进 WAF。
	ActionSkipWAF = "skip_waf"
)

// ValidActions 合法动作。
var ValidActions = []string{ActionAllow, ActionBlock, ActionLog, ActionSkipWAF}

// CustomRule 一条自定义规则（表 firewall_rules）。
type CustomRule struct {
	ID         int64
	Name       string
	Priority   int
	Expression string
	Action     string
	// BlockMode 拦截时的响应方式（封禁模式之一，空 = 403 JSON）。
	BlockMode string
	// BanSeconds 命中后封禁来源 IP 的时长（0 = 不封禁，-1 = 永久）。
	BanSeconds int64
}

// RuleEnv 表达式里可用的变量。字段名即表达式里的名字（见 docs/firewall.md「自定义规则」）。
type RuleEnv struct {
	IP          string            `expr:"ip"`
	Method      string            `expr:"method"`
	Path        string            `expr:"path"`
	Query       string            `expr:"query"`
	Host        string            `expr:"host"`
	UA          string            `expr:"ua"`
	Referer     string            `expr:"referer"`
	ContentType string            `expr:"contentType"`
	Headers     map[string]string `expr:"headers"`
	Args        map[string]string `expr:"args"`
	App         string            `expr:"app"`
	Country     string            `expr:"country"`
	Region      string            `expr:"region"`
	City        string            `expr:"city"`
	ISP         string            `expr:"isp"`
	ASN         string            `expr:"asn"`
	Hour        int               `expr:"hour"`
	Weekday     int               `expr:"weekday"`
	// Encrypted 客户端声明了加密载荷（入口阶段还没解开，只能看声明）。
	Encrypted bool `expr:"encrypted"`
}

// geoFields 引用了这些变量的规则才需要查归属地（查询可能走远程接口，不能每个请求都查）。
var geoFields = map[string]bool{"country": true, "region": true, "city": true, "isp": true, "asn": true}

// CompiledRule 编译好的自定义规则。
type CompiledRule struct {
	CustomRule
	program *vm.Program
	needGeo bool
	hits    atomic.Int64
}

// TakeHits 自上次取走以来的命中次数（取走后清零）。
func (r *CompiledRule) TakeHits() int64 { return r.hits.Swap(0) }

// cidrFunction 表达式函数 cidr(ip, "10.0.0.0/8", "192.168.0.0/16")。
var cidrFunction = expr.Function("cidr", func(params ...any) (any, error) {
	if len(params) < 2 {
		return false, fmt.Errorf("cidr(ip, 网段...) 至少需要两个参数")
	}
	ipText, _ := params[0].(string)
	addr, err := netip.ParseAddr(strings.TrimSpace(ipText))
	if err != nil {
		return false, nil
	}
	addr = addr.Unmap()
	for _, p := range params[1:] {
		text, _ := p.(string)
		if !strings.Contains(text, "/") {
			if single, err := netip.ParseAddr(strings.TrimSpace(text)); err == nil && single.Unmap() == addr {
				return true, nil
			}
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(text))
		if err != nil {
			return false, fmt.Errorf("网段 %q 无效", text)
		}
		if prefix.Contains(addr) {
			return true, nil
		}
	}
	return false, nil
}, new(func(string, ...string) bool))

func compileOptions() []expr.Option {
	return []expr.Option{expr.Env(RuleEnv{}), expr.AsBool(), cidrFunction}
}

// CompileRule 编译一条规则；错误信息可直接给控制台显示（带行列位置）。
func CompileRule(rule CustomRule) (*CompiledRule, error) {
	if strings.TrimSpace(rule.Expression) == "" {
		return nil, fmt.Errorf("表达式不能为空")
	}
	if len(rule.Expression) > 4000 {
		return nil, fmt.Errorf("表达式不能超过 4000 个字符")
	}
	valid := false
	for _, a := range ValidActions {
		if rule.Action == a {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("动作只能是 %s", strings.Join(ValidActions, " / "))
	}
	program, err := expr.Compile(rule.Expression, compileOptions()...)
	if err != nil {
		return nil, fmt.Errorf("表达式错误：%v", err)
	}
	visitor := &identVisitor{}
	if node := program.Node(); node != nil {
		ast.Walk(&node, visitor)
	}
	return &CompiledRule{CustomRule: rule, program: program, needGeo: visitor.geo}, nil
}

type identVisitor struct{ geo bool }

func (v *identVisitor) Visit(node *ast.Node) {
	if ident, ok := (*node).(*ast.IdentifierNode); ok && geoFields[ident.Value] {
		v.geo = true
	}
}

// RuleSet 按优先级排好的规则集。不可变。
type RuleSet struct {
	rules   []*CompiledRule
	needGeo bool
}

// NewRuleSet 编译并排序；任何一条编译失败都整体失败（保存前已逐条校验，这里失败说明库里的数据被绕过校验改过）。
func NewRuleSet(rules []CustomRule) (*RuleSet, []error) {
	set := &RuleSet{}
	var errs []error
	for _, rule := range rules {
		compiled, err := CompileRule(rule)
		if err != nil {
			errs = append(errs, fmt.Errorf("规则 #%d %s: %w", rule.ID, rule.Name, err))
			continue
		}
		set.rules = append(set.rules, compiled)
		if compiled.needGeo {
			set.needGeo = true
		}
	}
	sort.SliceStable(set.rules, func(i, j int) bool {
		if set.rules[i].Priority != set.rules[j].Priority {
			return set.rules[i].Priority < set.rules[j].Priority
		}
		return set.rules[i].ID < set.rules[j].ID
	})
	return set, errs
}

// Empty 没有任何规则。
func (s *RuleSet) Empty() bool { return s == nil || len(s.rules) == 0 }

// NeedGeo 是否有规则引用了归属地变量。
func (s *RuleSet) NeedGeo() bool { return s != nil && s.needGeo }

// Rules 规则列表（只读）。
func (s *RuleSet) Rules() []*CompiledRule {
	if s == nil {
		return nil
	}
	return s.rules
}

// Evaluate 按优先级依次求值：log 记下继续，allow / block / skip_waf 立即返回。
// 求值出错（如类型不符）的规则视为未命中，不影响后面的规则。
func (s *RuleSet) Evaluate(env *RuleEnv) (terminal *CompiledRule, logged []*CompiledRule) {
	if s.Empty() {
		return nil, nil
	}
	for _, rule := range s.rules {
		out, err := expr.Run(rule.program, env)
		if err != nil {
			continue
		}
		matched, _ := out.(bool)
		if !matched {
			continue
		}
		rule.hits.Add(1)
		if rule.Action == ActionLog {
			logged = append(logged, rule)
			continue
		}
		return rule, logged
	}
	return nil, logged
}

// FillTime 写入小时与星期（北京时间：规则里写「凌晨 2 点到 6 点」的人想的是本地时间）。
func (env *RuleEnv) FillTime(now time.Time) {
	local := now.In(shanghai)
	env.Hour = local.Hour()
	env.Weekday = int(local.Weekday())
}

var shanghai = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}()
