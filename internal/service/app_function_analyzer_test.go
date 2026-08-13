package service

import (
	"strings"
	"testing"

	functiondomain "aegis/internal/domain/appfunction"
)

// 静态检查存在的全部理由：把「发布通过、调用时 TypeError」提前到保存那一刻，
// 并说出是哪一行、缺哪一项。因此每条用例都同时断言「报没报」与「报在哪」。

func TestAnalyzerReportsMissingCapability(t *testing.T) {
	t.Parallel()

	source := "function handle(ctx) {\n  return aegis.points.add(10);\n}"
	result := AnalyzeFunctionScript(source, []string{CapUserRead})
	if result.OK {
		t.Fatal("调用未声明的能力应挡住发布")
	}

	diagnostic := findDiagnostic(result.Diagnostics, diagRuleCapability)
	if diagnostic == nil {
		t.Fatalf("应报出缺少能力，实际诊断：%+v", result.Diagnostics)
	}
	if diagnostic.Line != 2 {
		t.Errorf("应定位到第 2 行，实际第 %d 行", diagnostic.Line)
	}
	if len(diagnostic.Capabilities) != 1 || diagnostic.Capabilities[0] != CapPointsWrite {
		t.Errorf("应指名 %s，实际 %v", CapPointsWrite, diagnostic.Capabilities)
	}
	// 声明上之后同一份脚本必须通过，否则这条检查只是个噪音源
	if allowed := AnalyzeFunctionScript(source, []string{CapPointsWrite}); !allowed.OK {
		t.Errorf("声明能力后应通过，实际 %+v", allowed.Diagnostics)
	}
}

// 旧能力名声明的存量函数不能被误报：运行时按 NormalizeCapabilities
// 之后的集合绑定，检查也必须按同一份集合判。
func TestAnalyzerHonorsLegacyCapabilityNames(t *testing.T) {
	t.Parallel()

	source := "function handle(ctx) { return aegis.user.get(); }"
	if result := AnalyzeFunctionScript(source, []string{"user.profile.read"}); !result.OK {
		t.Errorf("旧能力名等价于 user.read，不应报缺声明：%+v", result.Diagnostics)
	}
}

// 字符串与注释里的 aegis.xxx 只是文本。报出来就是误报，
// 而一个会误报的检查很快会被所有人绕开。
func TestAnalyzerIgnoresStringsAndComments(t *testing.T) {
	t.Parallel()

	source := `// 用法：aegis.points.add(10)
/* 也可以 aegis.wallet.adjust("1.00") */
function handle(ctx) {
  var hint = "调用 aegis.vip.grant(30) 即可";
  var tpl = ` + "`aegis.email.send(a, b)`" + `;
  return { hint: hint, tpl: tpl };
}`
	result := AnalyzeFunctionScript(source, nil)
	if diagnostic := findDiagnostic(result.Diagnostics, diagRuleCapability); diagnostic != nil {
		t.Errorf("注释与字符串里的写法不该被当成调用：%+v", diagnostic)
	}
	if !result.OK {
		t.Errorf("这份脚本应通过检查，实际 %+v", result.Diagnostics)
	}
}

func TestAnalyzerRejectsAsyncHandle(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript("async function handle(ctx) { return 1; }", nil)
	if result.OK {
		t.Fatal("async handle 应挡住发布")
	}
	if findDiagnostic(result.Diagnostics, diagRuleAsync) == nil {
		t.Errorf("应报 async，实际 %+v", result.Diagnostics)
	}
}

func TestAnalyzerRequiresHandleEntry(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript("var x = 1;", nil)
	if findDiagnostic(result.Diagnostics, diagRuleEntry) == nil {
		t.Errorf("缺 handle 入口应报错，实际 %+v", result.Diagnostics)
	}
	// 函数表达式赋值同样算数
	assigned := AnalyzeFunctionScript("const handle = function (ctx) { return 1; };", nil)
	if findDiagnostic(assigned.Diagnostics, diagRuleEntry) != nil {
		t.Errorf("函数表达式形式的 handle 不该被判缺入口：%+v", assigned.Diagnostics)
	}
}

// 这些名字写了就一定 ReferenceError。在发布前说出来，
// 顺带告诉作者该用什么替代 —— 只说「不存在」会被当成平台缺功能。
func TestAnalyzerRejectsForbiddenGlobals(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript(
		"function handle(ctx) {\n  setTimeout(function () {}, 10);\n  return 1;\n}", nil)
	diagnostic := findDiagnostic(result.Diagnostics, diagRuleForbiddenGlobal)
	if diagnostic == nil {
		t.Fatalf("setTimeout 应被拦下，实际 %+v", result.Diagnostics)
	}
	if !strings.Contains(diagnostic.Message, "同步") {
		t.Errorf("提示应说清该怎么办，实际 %q", diagnostic.Message)
	}
}

// 自己声明的同名标识符不能被当成禁用全局 —— 一个把正确代码判错的检查
// 比没有检查更糟，何况这条诊断是 error 档、会直接挡住发布。
//
// 变量、函数名、形参、箭头函数形参、catch 绑定各覆盖一种写法：
// 形参那几种最容易漏，而 `function render(document)` 是完全合法的代码。
func TestAnalyzerAllowsLocallyDeclaredNames(t *testing.T) {
	t.Parallel()

	source := `function process(input) { return input; }
function render(document) { return String(document); }
function handle(ctx) {
  const items = (ctx.input.list || []).map((window) => String(window));
  try {
    return { a: process(ctx.input), b: render(1), items: items };
  } catch (localStorage) {
    return { error: String(localStorage) };
  }
}`
	result := AnalyzeFunctionScript(source, nil)
	if diagnostic := findDiagnostic(result.Diagnostics, diagRuleForbiddenGlobal); diagnostic != nil {
		t.Errorf("脚本自己声明的名字不该被报成禁用全局：%+v", diagnostic)
	}
	if !result.OK {
		t.Errorf("这份脚本应通过检查，实际 %+v", result.Diagnostics)
	}
}

// Promise 在 goja 里**确实存在**（typeof 是 "function"），它的问题是
// 没有事件循环去推进。把它列进「沙箱里没有」既不准确，也会挡住合法代码；
// 因此它是 warning 而不是 error。
func TestAnalyzerTreatsPromiseAsWarningNotMissing(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript(
		"function handle(ctx) { const p = Promise.resolve(1); return typeof p; }", nil)
	if findDiagnostic(result.Diagnostics, diagRuleForbiddenGlobal) != nil {
		t.Error("Promise 不该被报成「沙箱里没有」")
	}
	diagnostic := findDiagnostic(result.Diagnostics, diagRuleDangerous)
	if diagnostic == nil {
		t.Fatalf("应提示 Promise 不会被推进，实际 %+v", result.Diagnostics)
	}
	if !result.OK {
		t.Error("这条是提示，不该挡发布")
	}
}

// 带 break 的 while(true) 是常见写法，不该被打扰；不带出口的才要提示。
func TestAnalyzerFlagsOnlyExitlessLoops(t *testing.T) {
	t.Parallel()

	withExit := AnalyzeFunctionScript(
		"function handle(ctx) { while (true) { if (ctx.input.stop) break; } return 1; }", nil)
	if findDiagnostic(withExit.Diagnostics, diagRuleBusyLoop) != nil {
		t.Errorf("带 break 的循环不该被提示：%+v", withExit.Diagnostics)
	}

	exitless := AnalyzeFunctionScript(
		"function handle(ctx) { var n = 0; while (true) { n++; } }", nil)
	diagnostic := findDiagnostic(exitless.Diagnostics, diagRuleBusyLoop)
	if diagnostic == nil {
		t.Fatalf("没有出口的循环应被提示：%+v", exitless.Diagnostics)
	}
	// 只是提示，不该挡住发布
	if !exitless.OK {
		t.Error("死循环提示是 warning，不该挡发布")
	}
}

// 勾了没用到只是 info：多勾一项脚本照样能跑，但「声明即授权」
// 的价值取决于声明是不是精确的，而没人会主动回来清理。
func TestAnalyzerReportsUnusedCapabilities(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript(
		"function handle(ctx) { return aegis.user.get(); }",
		[]string{CapUserRead, CapWalletWrite})
	diagnostic := findDiagnostic(result.Diagnostics, diagRuleUnusedIntent)
	if diagnostic == nil {
		t.Fatalf("应提示勾了没用到，实际 %+v", result.Diagnostics)
	}
	if !strings.Contains(diagnostic.Message, CapWalletWrite) {
		t.Errorf("应点名 %s，实际 %q", CapWalletWrite, diagnostic.Message)
	}
	if !result.OK {
		t.Error("这只是 info，不该挡发布")
	}
	if len(result.UsedCapabilities) != 1 || result.UsedCapabilities[0] != CapUserRead {
		t.Errorf("实际用到的能力应是 [%s]，实际 %v", CapUserRead, result.UsedCapabilities)
	}
}

// 标准库（crypto / text / decimal…）不需要任何声明，也不该被报成缺能力。
func TestAnalyzerAcceptsStdlibWithoutDeclaration(t *testing.T) {
	t.Parallel()

	source := `function handle(ctx) {
  var sign = aegis.crypto.hmacSha256("k", aegis.encoding.queryStringify({ a: 1 }));
  var total = aegis.decimal.add("0.1", "0.2");
  return { sign: sign, total: total, day: aegis.time.dayKeyIn("Asia/Shanghai") };
}`
	result := AnalyzeFunctionScript(source, nil)
	if !result.OK {
		t.Errorf("标准库免声明，不该报错：%+v", result.Diagnostics)
	}
}

// 拼错成员名要说「没有这个成员」，而不是含糊地说缺能力 ——
// 后者会把作者引到设置页去勾一个根本不缺的东西。
func TestAnalyzerFlagsUnknownMember(t *testing.T) {
	t.Parallel()

	result := AnalyzeFunctionScript(
		"function handle(ctx) { return aegis.points.increase(1); }",
		[]string{CapPointsWrite})
	diagnostic := findDiagnostic(result.Diagnostics, diagRuleUnknownMember)
	if diagnostic == nil {
		t.Fatalf("拼错的成员名应被指出：%+v", result.Diagnostics)
	}
	if diagnostic.Severity != functiondomain.DiagnosticWarning {
		t.Errorf("这条是 warning（词法判定有近似成分，不该硬挡），实际 %q", diagnostic.Severity)
	}
}

// 内置模板必须全部通过自己的静态检查。
//
// 模板是作者的起点：一个开箱就报错的模板会让人第一时间失去对整套检查的信任，
// 而这件事只要有一条模板漏了能力声明就会发生。
func TestBuiltinTemplatesPassAnalysis(t *testing.T) {
	t.Parallel()

	for _, template := range functiondomain.ScriptTemplates() {
		result := AnalyzeFunctionScript(template.Source, template.Capabilities)
		if !result.OK {
			t.Errorf("模板 %s 通不过静态检查：%+v", template.Key, BlockingDiagnostics(result.Diagnostics))
		}
	}
}

func findDiagnostic(diagnostics []functiondomain.Diagnostic, rule string) *functiondomain.Diagnostic {
	for index := range diagnostics {
		if diagnostics[index].Rule == rule {
			return &diagnostics[index]
		}
	}
	return nil
}
