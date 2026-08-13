package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	functiondomain "aegis/internal/domain/appfunction"

	"github.com/dop251/goja"
)

const (
	maxScriptSourceBytes = 256 << 10
	// goja 无内存硬上限，只能靠调用栈深度 + 超时中断兜底。
	// 真正的隔离边界是「脚本拿不到任何未注入的能力」，而不是资源配额。
	maxScriptCallStack = 2048
	scriptEntryPoint   = "handle"
	// maxCachedPrograms 编译产物缓存的条目上限。
	//
	// 缓存的是**不可变版本的正文**，因此命中率天然很高；但试跑每改一个字符
	// 就是一份新正文，不封顶的话一个下午的迭代能攒下几千份编译产物，
	// 而它们再也不会被命中。超出时整体清空 —— LRU 要额外一把锁和一条链表，
	// 而这里的访问模式（少量热版本 + 大量一次性草稿）用不着那个精度。
	maxCachedPrograms = 512
)

var errScriptInterrupted = errors.New("脚本执行超时")

// ScriptError 是脚本抛错的结构化形态。
//
// 只给一句「TypeError: Cannot read property 'add' of undefined」，作者要在
// 两百行里自己找那个 undefined。行列与调用栈是把这次排查从「读一遍全文」
// 变成「跳到那一行」的唯一区别。
type ScriptError struct {
	Message string
	Line    int
	Column  int
	Stack   []string
}

func (e *ScriptError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s（第 %d 行第 %d 列）", e.Message, e.Line, e.Column)
	}
	return e.Message
}

// AppFunctionScriptExecutor 在 Aegis 进程内执行服务端 JS 脚本。
//
// 沙箱模型是 deny-by-default：goja 运行时本身不提供任何 I/O、定时器、模块加载
// 或网络能力，脚本能做的每一件有副作用的事都必须由 ScriptSDK 显式注入。
// 每次调用都新建运行时，杜绝跨请求、跨应用的状态残留。
//
// 「每次新建运行时」与「每次重新编译」是两件事：前者是隔离要求，后者纯粹是
// 重复劳动 —— 同一个激活版本的正文永远不变，一份 200 行脚本的编译在热路径上
// 比它自己的执行还贵。因此产物按正文摘要缓存，运行时仍然一次一个。
type AppFunctionScriptExecutor struct {
	programs sync.Map // sha256(source) → *goja.Program
	cached   atomic.Int64
}

func NewAppFunctionScriptExecutor() *AppFunctionScriptExecutor {
	return &AppFunctionScriptExecutor{}
}

// compile 取编译产物，未命中时编译并缓存。
func (e *AppFunctionScriptExecutor) compile(source string) (*goja.Program, error) {
	digest := sha256.Sum256([]byte(source))
	key := hex.EncodeToString(digest[:])
	if cached, ok := e.programs.Load(key); ok {
		return cached.(*goja.Program), nil
	}
	program, err := goja.Compile("function.js", source, true)
	if err != nil {
		return nil, err
	}
	if e.cached.Load() >= maxCachedPrograms {
		e.programs.Range(func(key, _ any) bool {
			e.programs.Delete(key)
			return true
		})
		e.cached.Store(0)
	}
	if _, loaded := e.programs.LoadOrStore(key, program); !loaded {
		e.cached.Add(1)
	}
	return program, nil
}

// CachedPrograms 当前缓存的编译产物数量，供监控与测试观察。
func (e *AppFunctionScriptExecutor) CachedPrograms() int64 { return e.cached.Load() }

// syntaxPositionPattern 从 goja 的语法错误文案里抠出行列。
//
// goja 把位置拼进了消息字符串（"SyntaxError: function.js: Line 3:12 …"），
// 没有结构化字段可取。抠不出来时退回第 1 行 —— 标在开头总好过不标，
// 至少编辑器上会有一处红色告诉作者"这份编译不过"。
var syntaxPositionPattern = regexp.MustCompile(`Line (\d+):(\d+)`)

// CompileCheck 只做编译，把语法错误翻译成一条带行列的诊断。
//
// 静态检查那套词法扫描给不出语法错误的位置（它压根不解析语法），
// 而语法错误恰恰是最需要精确定位的一类 —— 少一个括号的报错信息
// 如果不说在哪一行，作者只能从头读一遍。
func (e *AppFunctionScriptExecutor) CompileCheck(source string) *functiondomain.Diagnostic {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if _, err := e.compile(source); err != nil {
		diagnostic := &functiondomain.Diagnostic{
			Severity: functiondomain.DiagnosticError,
			Rule:     "syntax",
			Message:  "语法错误：" + err.Error(),
			Line:     1, Column: 1,
		}
		if matched := syntaxPositionPattern.FindStringSubmatch(err.Error()); len(matched) == 3 {
			diagnostic.Line, _ = strconv.Atoi(matched[1])
			diagnostic.Column, _ = strconv.Atoi(matched[2])
		}
		return diagnostic
	}
	return nil
}

// Validate 在创建版本时做语法与入口检查，避免把跑不起来的脚本发布上去。
func (e *AppFunctionScriptExecutor) Validate(source string) error {
	if strings.TrimSpace(source) == "" {
		return errors.New("脚本正文不能为空")
	}
	if len(source) > maxScriptSourceBytes {
		return fmt.Errorf("脚本超过 %d KB 上限", maxScriptSourceBytes>>10)
	}
	program, err := e.compile(source)
	if err != nil {
		return fmt.Errorf("脚本语法错误: %w", err)
	}

	// 在只有标准全局、没有 SDK 的运行时里执行顶层代码，确认 handle 已定义。
	// 顶层代码只允许做声明；任何试图调用 aegis.* 的写法都会在这里失败，
	// 这正是我们要的 —— 副作用必须发生在 handle 内部。
	vm := goja.New()
	vm.SetMaxCallStackSize(maxScriptCallStack)
	if err := installSandboxGlobals(vm); err != nil {
		return err
	}
	stop := interruptOnContext(context.Background(), vm, 2*time.Second)
	defer stop()

	if _, err := vm.RunProgram(program); err != nil {
		return fmt.Errorf("脚本顶层执行失败: %w", err)
	}
	if _, ok := goja.AssertFunction(vm.Get(scriptEntryPoint)); !ok {
		return fmt.Errorf("脚本必须定义 function %s(ctx)", scriptEntryPoint)
	}
	return nil
}

// Execute 运行脚本并返回 handle 的返回值（JSON 编码）。
//
// sdk 为 nil 时脚本仍可运行，但拿不到任何平台能力 —— 用于纯计算场景。
func (e *AppFunctionScriptExecutor) Execute(
	ctx context.Context,
	source string,
	scriptCtx functiondomain.ScriptContext,
	sdk *ScriptSDK,
	maxOutput int,
) (json.RawMessage, error) {
	program, err := e.compile(source)
	if err != nil {
		return nil, fmt.Errorf("脚本语法错误: %w", err)
	}

	vm := goja.New()
	// 用 json tag 做字段名映射，脚本里看到的字段名与 API 文档一致
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	vm.SetMaxCallStackSize(maxScriptCallStack)

	stop := interruptOnContext(ctx, vm, 0)
	defer stop()

	// 标准全局先于 SDK 装：Buffer / URL / TextEncoder 是纯内存类型，
	// 与能力声明无关，脚本里恒定存在。
	if err := installSandboxGlobals(vm); err != nil {
		return nil, err
	}
	if sdk != nil {
		if err := sdk.bind(vm); err != nil {
			return nil, fmt.Errorf("注入 SDK 失败: %w", err)
		}
	}

	if _, err := vm.RunProgram(program); err != nil {
		return nil, wrapScriptError("脚本顶层执行失败", err)
	}

	entry, ok := goja.AssertFunction(vm.Get(scriptEntryPoint))
	if !ok {
		return nil, fmt.Errorf("脚本必须定义 function %s(ctx)", scriptEntryPoint)
	}

	argument, err := buildScriptArgument(vm, scriptCtx)
	if err != nil {
		return nil, err
	}

	returned, err := entry(goja.Undefined(), argument)
	if err != nil {
		return nil, wrapScriptError("脚本执行失败", err)
	}

	output, err := encodeScriptResult(returned, maxOutput)
	if err != nil {
		return nil, err
	}
	return output, nil
}

// interruptOnContext 在 ctx 结束（或超过 fallback 时长）时中断 goja 运行时。
// goja 的 Interrupt 能打断死循环，这是脚本超时唯一可靠的手段。
func interruptOnContext(ctx context.Context, vm *goja.Runtime, fallback time.Duration) func() {
	done := make(chan struct{})
	var timeout <-chan time.Time
	if fallback > 0 {
		timer := time.NewTimer(fallback)
		timeout = timer.C
		go func() {
			<-done
			timer.Stop()
		}()
	}
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(errScriptInterrupted)
		case <-timeout:
			vm.Interrupt(errScriptInterrupted)
		case <-done:
		}
	}()
	return func() { close(done) }
}

// buildScriptArgument 把调用上下文转成脚本可读的对象。
// input 用 JSON 反序列化后注入，保证脚本拿到的是普通 JS 值而不是 Go 结构体代理。
func buildScriptArgument(vm *goja.Runtime, scriptCtx functiondomain.ScriptContext) (goja.Value, error) {
	var input any
	if len(scriptCtx.Input) > 0 {
		if err := json.Unmarshal(scriptCtx.Input, &input); err != nil {
			return nil, fmt.Errorf("函数输入不是有效 JSON: %w", err)
		}
	} else {
		input = map[string]any{}
	}

	caller := map[string]any{"type": scriptCtx.Caller.Type}
	if scriptCtx.Caller.UserID != nil {
		caller["userId"] = *scriptCtx.Caller.UserID
	}
	if scriptCtx.Caller.AdminID != nil {
		caller["adminId"] = *scriptCtx.Caller.AdminID
	}
	if scriptCtx.Caller.KeyID != nil {
		caller["keyId"] = *scriptCtx.Caller.KeyID
	}

	return vm.ToValue(map[string]any{
		"eventId":  scriptCtx.EventID,
		"appId":    scriptCtx.AppID,
		"appKey":   scriptCtx.AppKey,
		"function": scriptCtx.Function,
		"version":  scriptCtx.Version,
		"caller":   caller,
		"input":    input,
		// 试跑标记要透给脚本：有些动作（给第三方发通知、写外部账本）
		// 即便平台这边只是模拟，作者自己也该有机会跳过。
		"dryRun": scriptCtx.DryRun,
	}), nil
}

// encodeScriptResult 把 handle 的返回值编码为 JSON，并强制大小上限。
func encodeScriptResult(value goja.Value, maxOutput int) (json.RawMessage, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return json.RawMessage(`null`), nil
	}
	exported := value.Export()

	// 沙箱没有事件循环，Promise 永远不会被推进。若放行会把一个空对象当作结果返回，
	// 静默产出错误数据 —— 必须显式拒绝，让作者改成同步写法。
	if _, isPromise := exported.(*goja.Promise); isPromise {
		return nil, errors.New("handle 不能是 async 函数：沙箱没有事件循环，请改为同步返回")
	}

	encoded, err := json.Marshal(exported)
	if err != nil {
		return nil, fmt.Errorf("脚本返回值无法序列化为 JSON: %w", err)
	}
	if maxOutput > 0 && len(encoded) > maxOutput {
		return nil, fmt.Errorf("脚本返回值超过 %d 字节上限", maxOutput)
	}
	return encoded, nil
}

// wrapScriptError 把 goja 的中断与异常翻译成可读错误，
// 并剥离宿主细节，避免把内部实现暴露给调用方。
//
// 异常会带上抛错位置与调用栈：这两样都在 goja 的 Exception 里，
// 只是它的 String() 把它们和消息拼成了一坨文本 —— 拼好的文本没法在
// 编辑器上标红，而作者第一时间要的就是「跳到那一行」。
func wrapScriptError(prefix string, err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return errScriptInterrupted
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		failure := &ScriptError{Message: prefix + ": " + exception.Value().String()}
		for _, frame := range exception.Stack() {
			position := frame.Position()
			// 只保留脚本自己的帧：native / 宿主帧对作者没有意义，
			// 而且会把内部实现暴露出去。
			if position.Filename != "function.js" {
				continue
			}
			name := frame.FuncName()
			if name == "" {
				name = "<top>"
			}
			if failure.Line == 0 {
				failure.Line, failure.Column = position.Line, position.Column
			}
			failure.Stack = append(failure.Stack,
				fmt.Sprintf("%s (第 %d 行第 %d 列)", name, position.Line, position.Column))
		}
		return failure
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// scriptErrorPosition 从任意错误里取脚本抛错位置；不是脚本异常时返回零值。
func scriptErrorPosition(err error) (line, column int, stack []string) {
	var failure *ScriptError
	if errors.As(err, &failure) {
		return failure.Line, failure.Column, failure.Stack
	}
	return 0, 0, nil
}
