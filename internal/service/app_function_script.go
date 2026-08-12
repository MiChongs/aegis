package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
)

var errScriptInterrupted = errors.New("脚本执行超时")

// AppFunctionScriptExecutor 在 Aegis 进程内执行服务端 JS 脚本。
//
// 沙箱模型是 deny-by-default：goja 运行时本身不提供任何 I/O、定时器、模块加载
// 或网络能力，脚本能做的每一件有副作用的事都必须由 ScriptSDK 显式注入。
// 每次调用都新建运行时，杜绝跨请求、跨应用的状态残留。
type AppFunctionScriptExecutor struct{}

func NewAppFunctionScriptExecutor() *AppFunctionScriptExecutor {
	return &AppFunctionScriptExecutor{}
}

// Validate 在创建版本时做语法与入口检查，避免把跑不起来的脚本发布上去。
func (e *AppFunctionScriptExecutor) Validate(source string) error {
	if strings.TrimSpace(source) == "" {
		return errors.New("脚本正文不能为空")
	}
	if len(source) > maxScriptSourceBytes {
		return fmt.Errorf("脚本超过 %d KB 上限", maxScriptSourceBytes>>10)
	}
	program, err := goja.Compile("function.js", source, true)
	if err != nil {
		return fmt.Errorf("脚本语法错误: %w", err)
	}

	// 在无 SDK 的空运行时里执行顶层代码，确认 handle 已定义。
	// 顶层代码只允许做声明；任何试图调用 aegis.* 的写法都会在这里失败，
	// 这正是我们要的 —— 副作用必须发生在 handle 内部。
	vm := goja.New()
	vm.SetMaxCallStackSize(maxScriptCallStack)
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
	program, err := goja.Compile("function.js", source, true)
	if err != nil {
		return nil, fmt.Errorf("脚本语法错误: %w", err)
	}

	vm := goja.New()
	// 用 json tag 做字段名映射，脚本里看到的字段名与 API 文档一致
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	vm.SetMaxCallStackSize(maxScriptCallStack)

	stop := interruptOnContext(ctx, vm, 0)
	defer stop()

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
func wrapScriptError(prefix string, err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return errScriptInterrupted
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return fmt.Errorf("%s: %s", prefix, exception.Value().String())
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
