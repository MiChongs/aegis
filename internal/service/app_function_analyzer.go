package service

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	functiondomain "aegis/internal/domain/appfunction"
)

// 脚本静态检查。
//
// 它回答的是一个具体问题：**这份脚本发上去之后会不会因为一眼可见的原因跑不起来**。
// 沙箱是 deny-by-default 的，因此「勾了 kv.read 却调了 aegis.points.add」
// 在运行时的表现是 `TypeError: Cannot read property 'add' of undefined` ——
// 一句既不说缺什么、也不说在哪一行的报错，而且要等到真实调用才出现。
// 这套检查把它提前到保存那一刻，并直接说出「第 12 行，需要 points.write」。
//
// 为什么不走 AST：goja 这个版本的 ast 包只有节点定义，没有 Walk / Visitor，
// 手写一个覆盖全部节点类型的遍历器是几百行且每次 goja 升级都要跟。
// 而这里要找的东西（`aegis.x.y` 与裸标识符）是**词法层面**的模式，
// 一个只需分清「代码 / 字符串 / 注释」的扫描器就够，且不会随语法演进失效。
// 语法正确性本来就由 goja.Compile 把关，两者分工明确。

// 诊断规则标识。控制台按它决定要不要给「一键修复」。
const (
	diagRuleSyntax          = "syntax"
	diagRuleEntry           = "entry"
	diagRuleAsync           = "async"
	diagRuleCapability      = "capability"
	diagRuleUnknownMember   = "unknown-member"
	diagRuleForbiddenGlobal = "forbidden-global"
	diagRuleUnusedIntent    = "unused-capability"
	diagRuleBusyLoop        = "busy-loop"
	diagRuleDangerous       = "dangerous"
)

// forbiddenGlobals 是脚本里写了就一定会 ReferenceError 的名字。
//
// 值是「那你该用什么」。只说「不存在」会让人以为是平台缺功能，
// 而这些名字十有八九是从浏览器或 Node 的写法直接搬过来的。
var forbiddenGlobals = map[string]string{
	"require":        "沙箱没有模块系统；需要的工具在 aegis.crypto / aegis.encoding / aegis.text 下",
	"process":        "沙箱不暴露进程与环境变量；配置请放函数配置（aegis.config）",
	"setTimeout":     "沙箱没有事件循环，handle 必须同步返回",
	"setInterval":    "沙箱没有事件循环，周期任务请用工作流或定时调度",
	"setImmediate":   "沙箱没有事件循环，handle 必须同步返回",
	"XMLHttpRequest": "出站请求请用 aegis.fetch（需声明 http.fetch）",
	"document":       "沙箱在服务端运行，没有 DOM",
	"window":         "沙箱在服务端运行，没有浏览器全局",
	"localStorage":   "服务端独占状态请用 aegis.kv（需声明 kv.read / kv.write）",
	"sessionStorage": "服务端独占状态请用 aegis.kv（需声明 kv.read / kv.write）",
	"__dirname":      "沙箱没有文件系统",
	"__filename":     "沙箱没有文件系统",
}

// dangerousGlobals 是**确实存在**但几乎总是坏主意的那批。只提示，不拦发布。
//
// Promise 尤其要分清：goja 有它，`typeof Promise` 是 "function" ——
// 把它列进「沙箱里没有」既不准确，也会挡住合法（只是没用）的代码。
// 它的真实问题是没有事件循环去推进它，所以 then 里的东西永远不会跑。
var dangerousGlobals = map[string]string{
	"Promise": "沙箱没有事件循环，Promise 永远不会被推进（then 里的代码不会执行），请改成同步写法",
	"eval":    "eval 会让这份脚本的实际逻辑无法被静态检查，也无法被审计",
	"Function": "用 Function 构造器动态生成代码，等于绕过了能力声明这套检查" +
		"（它构造出的代码同样跑在沙箱里，但没人看得出它会做什么）",
}

// AnalyzeFunctionScript 对一份脚本做静态检查。
//
// declared 是函数**已声明**的能力。检查的宽严刻意与运行时一致：
// 运行时按 NormalizeCapabilities 之后的集合绑定，这里也一样，
// 否则存量函数（声明的是 user.profile.read 这类旧名）会被误报成缺声明。
func AnalyzeFunctionScript(source string, declared []string) functiondomain.AnalysisResult {
	result := functiondomain.AnalysisResult{
		Diagnostics: []functiondomain.Diagnostic{},
		SourceBytes: len(source),
	}
	if strings.TrimSpace(source) == "" {
		result.Diagnostics = append(result.Diagnostics, functiondomain.Diagnostic{
			Severity: functiondomain.DiagnosticError, Rule: diagRuleEntry,
			Message: "脚本正文为空", Line: 1, Column: 1,
		})
		return result
	}

	granted := make(map[string]struct{})
	for _, capability := range functiondomain.NormalizeCapabilities(declared) {
		granted[capability] = struct{}{}
	}

	tokens := scanScriptTokens(source)
	used := map[string]struct{}{}

	result.Diagnostics = append(result.Diagnostics, checkEntryPoint(tokens)...)
	result.Diagnostics = append(result.Diagnostics, checkSDKUsage(tokens, granted, used)...)
	result.Diagnostics = append(result.Diagnostics, checkGlobals(tokens)...)
	result.Diagnostics = append(result.Diagnostics, checkBusyLoop(tokens)...)
	result.Diagnostics = append(result.Diagnostics, checkUnusedCapabilities(granted, used)...)

	for capability := range used {
		result.UsedCapabilities = append(result.UsedCapabilities, capability)
	}
	sort.Strings(result.UsedCapabilities)
	if result.UsedCapabilities == nil {
		result.UsedCapabilities = []string{}
	}

	sort.SliceStable(result.Diagnostics, func(i, j int) bool {
		if result.Diagnostics[i].Line != result.Diagnostics[j].Line {
			return result.Diagnostics[i].Line < result.Diagnostics[j].Line
		}
		return result.Diagnostics[i].Column < result.Diagnostics[j].Column
	})
	result.OK = !hasBlockingDiagnostic(result.Diagnostics)
	return result
}

// hasBlockingDiagnostic 只有 error 档挡发布。
//
// warning 与 info 不挡是刻意的：这套检查是词法层面的近似，
// 把「我拿不准」也变成硬闸门，代价是某个合法写法从此发不出去，
// 而作者除了绕开检查别无办法 —— 那会让整套检查被当成障碍而不是帮助。
func hasBlockingDiagnostic(diagnostics []functiondomain.Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == functiondomain.DiagnosticError {
			return true
		}
	}
	return false
}

// BlockingDiagnostics 摘出会挡住发布的那几条，供服务层拼错误文案。
func BlockingDiagnostics(diagnostics []functiondomain.Diagnostic) []functiondomain.Diagnostic {
	out := make([]functiondomain.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == functiondomain.DiagnosticError {
			out = append(out, diagnostic)
		}
	}
	return out
}

// ── 各条规则 ────────────────────────────────────────────────────────

func checkEntryPoint(tokens []scriptToken) []functiondomain.Diagnostic {
	for index, token := range tokens {
		if token.Text != scriptEntryPoint || index == 0 {
			continue
		}
		previous := tokens[index-1]
		if previous.Text == "function" {
			if index >= 2 && tokens[index-2].Text == "async" {
				return []functiondomain.Diagnostic{{
					Severity: functiondomain.DiagnosticError, Rule: diagRuleAsync,
					Message: "handle 不能是 async 函数：沙箱没有事件循环，返回的 Promise 永远不会被推进",
					Line:    tokens[index-2].Line, Column: tokens[index-2].Column,
					EndColumn: token.Column + len(token.Text),
				}}
			}
			return nil
		}
		// `const handle = function (ctx) {}` / `handle = (ctx) => {}` 同样算数
		if previous.Text == "var" || previous.Text == "let" || previous.Text == "const" {
			return nil
		}
	}
	return []functiondomain.Diagnostic{{
		Severity: functiondomain.DiagnosticError, Rule: diagRuleEntry,
		Message: "脚本必须定义 function handle(ctx) —— 那是宿主唯一会调用的入口",
		Line:    1, Column: 1,
	}}
}

// checkSDKUsage 逐处 `aegis.x` / `aegis.x.y` 反查它需要哪项能力。
func checkSDKUsage(
	tokens []scriptToken,
	granted map[string]struct{},
	used map[string]struct{},
) []functiondomain.Diagnostic {
	var diagnostics []functiondomain.Diagnostic
	reported := map[string]struct{}{}

	for index, token := range tokens {
		if token.Text != "aegis" || !isIdentifierStart(token) {
			continue
		}
		if index+2 >= len(tokens) || tokens[index+1].Text != "." {
			continue
		}
		root := tokens[index+2]
		if !root.Identifier {
			continue
		}
		member := ""
		if index+4 < len(tokens) && tokens[index+3].Text == "." && tokens[index+4].Identifier {
			member = tokens[index+4].Text
		}

		needed, known := functiondomain.CapabilitiesForMember(root.Text, member)
		if !known {
			if functiondomain.NamespaceExists(root.Text) {
				diagnostics = appendOnce(diagnostics, reported, functiondomain.Diagnostic{
					Severity: functiondomain.DiagnosticWarning, Rule: diagRuleUnknownMember,
					Message: fmt.Sprintf("aegis.%s 上没有 %s 这个成员，运行时会是 undefined", root.Text, member),
					Line:    root.Line, Column: root.Column, EndColumn: root.Column + len(root.Text),
				})
				continue
			}
			diagnostics = appendOnce(diagnostics, reported, functiondomain.Diagnostic{
				Severity: functiondomain.DiagnosticWarning, Rule: diagRuleUnknownMember,
				Message: fmt.Sprintf("SDK 上没有 aegis.%s，运行时会是 undefined", root.Text),
				Line:    root.Line, Column: root.Column, EndColumn: root.Column + len(root.Text),
			})
			continue
		}
		if len(needed) == 0 {
			// 免声明成员（crypto / text / decimal…）
			continue
		}
		satisfied := false
		for _, capability := range needed {
			if _, ok := granted[capability]; ok {
				used[capability] = struct{}{}
				satisfied = true
			}
		}
		if satisfied {
			continue
		}
		expression := "aegis." + root.Text
		if member != "" {
			expression += "." + member
		}
		diagnostics = appendOnce(diagnostics, reported, functiondomain.Diagnostic{
			Severity: functiondomain.DiagnosticError, Rule: diagRuleCapability,
			Message: fmt.Sprintf("%s 需要声明能力 %s —— 没声明的话它在运行时就是 undefined",
				expression, strings.Join(needed, " 或 ")),
			Line: root.Line, Column: root.Column, EndColumn: root.Column + len(root.Text),
			Capabilities: needed,
		})
	}
	return diagnostics
}

func checkGlobals(tokens []scriptToken) []functiondomain.Diagnostic {
	var diagnostics []functiondomain.Diagnostic
	reported := map[string]struct{}{}
	declaredLocally := collectDeclaredNames(tokens)

	for index, token := range tokens {
		if !token.Identifier {
			continue
		}
		// 属性名不算：`obj.window` 与全局 `window` 是两回事
		if index > 0 && tokens[index-1].Text == "." {
			continue
		}
		// 对象字面量的键同样不算：`{ process: 1 }`
		if index+1 < len(tokens) && tokens[index+1].Text == ":" {
			continue
		}
		if _, local := declaredLocally[token.Text]; local {
			continue
		}
		if hint, forbidden := forbiddenGlobals[token.Text]; forbidden {
			diagnostics = appendOnce(diagnostics, reported, functiondomain.Diagnostic{
				Severity: functiondomain.DiagnosticError, Rule: diagRuleForbiddenGlobal,
				Message: fmt.Sprintf("沙箱里没有 %s：%s", token.Text, hint),
				Line:    token.Line, Column: token.Column, EndColumn: token.Column + len(token.Text),
			})
			continue
		}
		if hint, risky := dangerousGlobals[token.Text]; risky {
			diagnostics = appendOnce(diagnostics, reported, functiondomain.Diagnostic{
				Severity: functiondomain.DiagnosticWarning, Rule: diagRuleDangerous,
				Message: fmt.Sprintf("不建议使用 %s：%s", token.Text, hint),
				Line:    token.Line, Column: token.Column, EndColumn: token.Column + len(token.Text),
			})
		}
	}
	return diagnostics
}

// collectDeclaredNames 收集脚本自己声明过的名字：变量、函数、类、
// 以及**形参**（含箭头函数与 catch 绑定）。
//
// 形参那一段不是为了完整，是为了不误报：`function render(document) {}` 完全合法，
// 而禁用全局这条诊断是 error 档、会挡住发布。一个把正确代码判成错误、
// 且作者除了绕开别无办法的检查，比没有这个检查更糟。
func collectDeclaredNames(tokens []scriptToken) map[string]struct{} {
	declared := map[string]struct{}{}
	collectParams := func(open int) {
		for index := open + 1; index < len(tokens); index++ {
			if tokens[index].Text == ")" {
				return
			}
			if tokens[index].Identifier {
				declared[tokens[index].Text] = struct{}{}
			}
		}
	}

	for index, token := range tokens {
		if token.Identifier && index > 0 {
			switch tokens[index-1].Text {
			case "var", "let", "const", "function", "class":
				declared[token.Text] = struct{}{}
			}
		}
		switch token.Text {
		case "function", "catch":
			// function f(a, b) / function (a, b) / catch (err)
			for cursor := index + 1; cursor < len(tokens) && cursor <= index+2; cursor++ {
				if tokens[cursor].Text == "(" {
					collectParams(cursor)
					break
				}
			}
		case "=>":
			// 箭头函数的形参在 => 左边：要么是 (a, b)，要么是单个标识符
			if index == 0 {
				continue
			}
			if tokens[index-1].Text != ")" {
				if tokens[index-1].Identifier {
					declared[tokens[index-1].Text] = struct{}{}
				}
				continue
			}
			depth := 0
			for cursor := index - 1; cursor >= 0; cursor-- {
				if tokens[cursor].Text == ")" {
					depth++
				}
				if tokens[cursor].Text == "(" {
					depth--
					if depth == 0 {
						collectParams(cursor)
						break
					}
				}
			}
		}
	}
	return declared
}

// checkBusyLoop 找没有出口的循环。
//
// 它不是错误 —— `while (true) { ...; break; }` 是常见写法，而带 break
// 的那种我们放过。真正会出事的是一个连 break 都没有的循环：它会一直跑到
// timeoutMs 被中断，占满一个并发槽位，而调用方看到的只是一句「执行失败」。
func checkBusyLoop(tokens []scriptToken) []functiondomain.Diagnostic {
	var diagnostics []functiondomain.Diagnostic
	for index, token := range tokens {
		infinite := false
		switch {
		case token.Text == "while" && index+2 < len(tokens) &&
			tokens[index+1].Text == "(" && tokens[index+2].Text == "true":
			infinite = true
		case token.Text == "for" && index+3 < len(tokens) &&
			tokens[index+1].Text == "(" && tokens[index+2].Text == ";" && tokens[index+3].Text == ";":
			infinite = true
		}
		if !infinite || loopHasExit(tokens, index) {
			continue
		}
		diagnostics = append(diagnostics, functiondomain.Diagnostic{
			Severity: functiondomain.DiagnosticWarning, Rule: diagRuleBusyLoop,
			Message: "这个循环没有 break / return / throw 出口，会一直跑到 timeoutMs 被中断，" +
				"期间占着一个并发名额",
			Line: token.Line, Column: token.Column, EndColumn: token.Column + len(token.Text),
		})
	}
	return diagnostics
}

// loopHasExit 在循环体的花括号配对范围内找出口关键字。
func loopHasExit(tokens []scriptToken, start int) bool {
	depth := 0
	entered := false
	for index := start; index < len(tokens); index++ {
		switch tokens[index].Text {
		case "{":
			depth++
			entered = true
		case "}":
			depth--
			if entered && depth <= 0 {
				return false
			}
		case "break", "return", "throw":
			if entered {
				return true
			}
		}
	}
	return false
}

// checkUnusedCapabilities 勾了却没用到的能力。
//
// 这是 info 而不是 warning：多勾一项的直接后果只是权限面变大，
// 脚本照样能跑。但「声明即授权」这条约束的价值取决于声明是不是精确的，
// 而没人会主动回来清理自己勾多的那几项。
func checkUnusedCapabilities(granted, used map[string]struct{}) []functiondomain.Diagnostic {
	var idle []string
	for capability := range granted {
		if _, ok := used[capability]; ok {
			continue
		}
		// 旧能力名不参与提示：它们本来就不绑定任何对象，
		// 提示「没用到」只会诱导作者去掉那个为兼容而保留的声明。
		if item, ok := functiondomain.CapabilityByKey(capability); ok && item.Deprecated {
			continue
		}
		idle = append(idle, capability)
	}
	if len(idle) == 0 {
		return nil
	}
	sort.Strings(idle)
	return []functiondomain.Diagnostic{{
		Severity: functiondomain.DiagnosticInfo, Rule: diagRuleUnusedIntent,
		Message: fmt.Sprintf("声明了但脚本里没有用到：%s。用不到就取消勾选，"+
			"「声明即授权」的意义在于声明是精确的", strings.Join(idle, "、")),
		Line: 1, Column: 1,
	}}
}

func appendOnce(
	diagnostics []functiondomain.Diagnostic,
	reported map[string]struct{},
	diagnostic functiondomain.Diagnostic,
) []functiondomain.Diagnostic {
	// 同一处问题在一份脚本里往往出现很多次（一个循环体里调五次 aegis.kv）。
	// 逐处都报会把诊断列表淹掉，因此按 (规则, 消息) 只留第一处。
	key := diagnostic.Rule + "|" + diagnostic.Message
	if _, duplicate := reported[key]; duplicate {
		return diagnostics
	}
	reported[key] = struct{}{}
	return append(diagnostics, diagnostic)
}

// ── 词法扫描 ────────────────────────────────────────────────────────

// scriptToken 是扫描器认识的最小单位：标识符、数字、或单个符号。
// 字符串、模板串、注释在扫描阶段整体跳过 —— 它们里面的 `aegis.points`
// 只是一段文本，报出来就是误报。
type scriptToken struct {
	Text       string
	Line       int
	Column     int
	Identifier bool
}

func isIdentifierStart(token scriptToken) bool { return token.Identifier }

func scanScriptTokens(source string) []scriptToken {
	runes := []rune(source)
	tokens := make([]scriptToken, 0, len(runes)/4)
	line, column := 1, 1

	for index := 0; index < len(runes); {
		current := runes[index]

		switch {
		case current == '\n':
			line++
			column = 1
			index++
			continue
		case unicode.IsSpace(current):
			index++
			column++
			continue
		case current == '/' && index+1 < len(runes) && runes[index+1] == '/':
			for index < len(runes) && runes[index] != '\n' {
				index++
			}
			continue
		case current == '/' && index+1 < len(runes) && runes[index+1] == '*':
			index += 2
			column += 2
			for index < len(runes) && !(runes[index] == '*' && index+1 < len(runes) && runes[index+1] == '/') {
				if runes[index] == '\n' {
					line++
					column = 1
				} else {
					column++
				}
				index++
			}
			index += 2
			column += 2
			continue
		case current == '"' || current == '\'' || current == '`':
			quote := current
			index++
			column++
			for index < len(runes) {
				if runes[index] == '\\' {
					index += 2
					column += 2
					continue
				}
				if runes[index] == quote {
					index++
					column++
					break
				}
				if runes[index] == '\n' {
					line++
					column = 1
				} else {
					column++
				}
				index++
			}
			continue
		}

		if isIdentifierRune(current, true) {
			start := index
			startColumn := column
			for index < len(runes) && isIdentifierRune(runes[index], false) {
				index++
				column++
			}
			tokens = append(tokens, scriptToken{
				Text: string(runes[start:index]), Line: line, Column: startColumn, Identifier: true,
			})
			continue
		}

		// 箭头是唯一需要成对识别的符号：形参收集靠它定位，
		// 拆成 `=` 与 `>` 两个 token 的话箭头函数的参数一个都收不到。
		if current == '=' && index+1 < len(runes) && runes[index+1] == '>' {
			tokens = append(tokens, scriptToken{Text: "=>", Line: line, Column: column})
			index += 2
			column += 2
			continue
		}

		tokens = append(tokens, scriptToken{Text: string(current), Line: line, Column: column})
		index++
		column++
	}
	return tokens
}

func isIdentifierRune(value rune, first bool) bool {
	if value == '_' || value == '$' {
		return true
	}
	if unicode.IsLetter(value) {
		return true
	}
	return !first && unicode.IsDigit(value)
}
