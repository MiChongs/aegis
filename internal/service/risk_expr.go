package service

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"aegis/pkg/timeutil"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// 表达式引擎。
//
// 相比重构前的实现，这里做对了三件本该做对的事：
//
//  1. **编译一次，跑多次。** 旧代码在每次评估里调 expr.Compile —— 登录热路径上
//     每来一个请求就重新做一次词法/语法/类型检查。程序按表达式文本缓存后，
//     命中缓存的评估只剩 vm.Run 一步。
//  2. **类型化环境。** 用 RiskEvalEnv 做 expr.Env，写错的变量名在**保存规则时**
//     就是一个编译错误，而不是运行期静默判假、规则永不命中。
//  3. **领域函数。** 网段判断、关键词匹配、时段判断这些在风控里天天要写的东西，
//     若只靠表达式原语拼会写出一长串没人看得懂的条件。

// riskExprOptions 是编译与运行共用的选项集合。
// 编译与运行必须用同一套 —— 少一个 Function 选项，编译期认得的函数运行期就找不到。
func riskExprOptions() []expr.Option {
	return []expr.Option{
		expr.Env(RiskEvalEnv{}),
		expr.AsBool(),
		// 规则表达式是管理员写的，不该出现取值失败就中断整条评估链的情况；
		// 但也不能容忍未定义变量 —— 那正是我们要在保存时抓住的错误。
		expr.DisableBuiltin("fetch"),
		expr.Timezone(timeutil.DefaultTimezone()),
		exprInCIDR(),
		exprAnyCIDR(),
		exprContainsAny(),
		exprInTimeWindow(),
	}
}

func exprInCIDR() expr.Option {
	return expr.Function("in_cidr", func(params ...any) (any, error) {
		ip, _ := params[0].(string)
		cidr, _ := params[1].(string)
		return ipInCIDR(ip, cidr), nil
	}, new(func(string, string) bool))
}

// 列表参数一律声明成 []any 而不是 []string：expr 的数组字面量
// `["a", "b"]` 的静态类型就是 []interface{}，声明成 []string 会让
// 目录里给管理员抄的示例当场编译失败 —— 而那正是最难解释的一类 bug。
// 真正的类型收敛交给 toStringSlice。
func exprAnyCIDR() expr.Option {
	return expr.Function("any_cidr", func(params ...any) (any, error) {
		ip, _ := params[0].(string)
		return ipInAnyCIDR(ip, toStringSlice(params[1])), nil
	}, new(func(string, []any) bool))
}

func exprContainsAny() expr.Option {
	return expr.Function("contains_any", func(params ...any) (any, error) {
		text, _ := params[0].(string)
		return containsAnyFold(text, toStringSlice(params[1])), nil
	}, new(func(string, []any) bool))
}

func exprInTimeWindow() expr.Option {
	return expr.Function("in_time_window", func(params ...any) (any, error) {
		start, _ := params[0].(string)
		end, _ := params[1].(string)
		hit, err := inTimeWindow(timeutil.Now(), start, end)
		if err != nil {
			return false, err
		}
		return hit, nil
	}, new(func(string, string) bool))
}

// riskExprCache 表达式文本 → 已编译程序。
// 键是表达式本身而不是规则 ID：同一段表达式被多条规则复用时只编译一次，
// 规则被改写后旧程序自然失去引用，无需显式失效。
var riskExprCache sync.Map // map[string]*vm.Program

// compileRiskExpr 取出（或编译并缓存）一段表达式。
func compileRiskExpr(expression string) (*vm.Program, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return nil, fmt.Errorf("表达式不能为空")
	}
	if cached, ok := riskExprCache.Load(expression); ok {
		return cached.(*vm.Program), nil
	}
	program, err := expr.Compile(expression, riskExprOptions()...)
	if err != nil {
		return nil, err
	}
	actual, _ := riskExprCache.LoadOrStore(expression, program)
	return actual.(*vm.Program), nil
}

// ValidateRiskExpression 校验一段表达式能否编译。
// 规则的创建与更新都会先过它，写错的表达式当场被拒绝并带上具体错误位置。
func ValidateRiskExpression(expression string) error {
	_, err := compileRiskExpr(expression)
	return err
}

// runRiskExpr 在给定环境上执行表达式。
func runRiskExpr(expression string, env *RiskEvalEnv) (bool, error) {
	program, err := compileRiskExpr(expression)
	if err != nil {
		return false, err
	}
	result, err := expr.Run(program, env)
	if err != nil {
		return false, err
	}
	hit, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("表达式结果不是布尔值：%T", result)
	}
	return hit, nil
}

// ════════════════════════════════════════════════════════════
//  领域函数的实现（同时被条件类型判定复用）
// ════════════════════════════════════════════════════════════

// ipInCIDR 判断 IP 是否落在网段内。
// 用 net/netip 而不是 net.ParseCIDR：前者是值类型、零分配，
// 且天然同时处理 IPv4 与 IPv6（含 v4-mapped 形式）。
func ipInCIDR(ip, cidr string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	if err != nil {
		return false
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	return prefix.Contains(addr)
}

// isValidCIDR 校验网段字面量，用于规则保存时的参数检查。
func isValidCIDR(cidr string) bool {
	_, err := netip.ParsePrefix(strings.TrimSpace(cidr))
	return err == nil
}

func ipInAnyCIDR(ip string, cidrs []string) bool {
	for _, cidr := range cidrs {
		if ipInCIDR(ip, cidr) {
			return true
		}
	}
	return false
}

func containsAnyFold(text string, keywords []string) bool {
	if text == "" {
		return false
	}
	lowered := strings.ToLower(text)
	for _, keyword := range keywords {
		keyword = strings.ToLower(strings.TrimSpace(keyword))
		if keyword == "" {
			continue
		}
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}

// inTimeWindow 判断时刻是否落在 HH:MM 时段内，支持跨零点（23:00–06:00）。
// 判定按平台默认时区进行 —— 运维配「凌晨 2 点到 5 点」指的是本地的凌晨，
// 拿 UTC 去比会整体偏移时区差，且偏得毫无征兆。
func inTimeWindow(at time.Time, start, end string) (bool, error) {
	startOfDay, err := timeutil.ParseLocalTimeOfDay(start)
	if err != nil {
		return false, fmt.Errorf("起始时间格式应为 HH:MM：%w", err)
	}
	endOfDay, err := timeutil.ParseLocalTimeOfDay(end)
	if err != nil {
		return false, fmt.Errorf("结束时间格式应为 HH:MM：%w", err)
	}

	local := at.In(timeutil.DefaultLocation())
	minutes := local.Hour()*60 + local.Minute()
	startMinutes := startOfDay.Hour*60 + startOfDay.Minute
	endMinutes := endOfDay.Hour*60 + endOfDay.Minute

	switch {
	case startMinutes == endMinutes:
		return true, nil // 起止相同视为全天
	case startMinutes < endMinutes:
		return minutes >= startMinutes && minutes < endMinutes, nil
	default:
		return minutes >= startMinutes || minutes < endMinutes, nil // 跨零点
	}
}

// toStringSlice 把表达式/规则参数里的列表值统一成 []string。
// JSONB 反序列化出来的是 []any，表达式字面量给的是 []string，两者都要认。
func toStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(toString(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		// 允许逗号 / 换行分隔的一行写法，控制台的 list 输入框就是这么给的
		parts := strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == ';' || r == ' '
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}
