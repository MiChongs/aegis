package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	functiondomain "aegis/internal/domain/appfunction"

	"go.uber.org/zap"
)

func scriptContext(input string) functiondomain.ScriptContext {
	userID := int64(42)
	return functiondomain.ScriptContext{
		EventID:  "11111111-1111-4111-8111-111111111111",
		AppID:    7,
		AppKey:   "demo_app",
		Function: "test-fn",
		Version:  "1.0.0",
		Caller:   functiondomain.Caller{Type: "user", UserID: &userID},
		Input:    json.RawMessage(input),
	}
}

// newTestSDK 构造一个只声明了指定能力的 SDK。
// 这些用例只验证「能力是否被绑定」，不触发真正的数据库写入。
func newTestSDK(capabilities ...string) *ScriptSDK {
	return newTestSDKWithConfig(nil, capabilities...)
}

func newTestSDKWithConfig(config json.RawMessage, capabilities ...string) *ScriptSDK {
	userID := int64(42)
	return newScriptSDK(
		context.Background(),
		testScriptDeps(),
		7, "demo_app", "test-fn", "evt",
		functiondomain.Caller{Type: "user", UserID: &userID},
		capabilities,
		scriptSDKOptions{Config: config},
	)
}

// newDryRunTestSDK 与 newTestSDK 相同，但打开试跑开关。
func newDryRunTestSDK(capabilities ...string) *ScriptSDK {
	userID := int64(42)
	return newScriptSDK(
		context.Background(),
		testScriptDeps(),
		7, "demo_app", "test-fn", "evt",
		functiondomain.Caller{Type: "user", UserID: &userID},
		capabilities,
		scriptSDKOptions{DryRun: true},
	)
}

// testScriptDeps 给出**非 nil 但从不被调用**的宿主依赖。
//
// 绑定阶段只检查依赖在不在（缺了就点名报错，而不是绑上去等运行时空指针），
// 因此这里用零值指针即可让全部能力完成绑定。这些用例验证的是
// 「能力是否被绑定」，一旦真的调用到业务方法就会 panic —— 那正是我们要的：
// 谁不小心在这里写出一次真实调用，测试会立刻炸掉而不是静默打库。
func testScriptDeps() ScriptSDKDeps {
	return ScriptSDKDeps{
		Log:           zap.NewNop(),
		Points:        &PointsService{},
		Vip:           &VipService{},
		Notifications: &NotificationService{},
		Audit:         &AuditService{},
		Wallet:        &WalletService{},
		Email:         &EmailService{},
		Bans:          &AccountBanService{},
	}
}

func TestScriptExecutorReturnsOutput(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		return { doubled: ctx.input.value * 2, caller: ctx.caller.userId };
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{"value":21}`), nil, 65536)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var decoded struct {
		Doubled int64 `json:"doubled"`
		Caller  int64 `json:"caller"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("返回值不是 JSON: %v", err)
	}
	if decoded.Doubled != 42 {
		t.Errorf("期望 doubled=42，实际 %d", decoded.Doubled)
	}
	if decoded.Caller != 42 {
		t.Errorf("脚本应能读到调用者 userId，实际 %d", decoded.Caller)
	}
}

func TestScriptExecutorRequiresHandleEntry(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	if err := executor.Validate(`var x = 1;`); err == nil {
		t.Fatal("缺少 handle 入口时应校验失败")
	}
	if err := executor.Validate(`function handle(ctx) { return 1; }`); err != nil {
		t.Fatalf("合法脚本不应报错: %v", err)
	}
}

func TestScriptExecutorRejectsSyntaxError(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	if err := executor.Validate(`function handle( {`); err == nil {
		t.Fatal("语法错误应在发布前被拦截")
	}
}

// 死循环必须能被打断，否则一个脚本就能占满一个函数并发槽位。
func TestScriptExecutorInterruptsInfiniteLoop(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := executor.Execute(ctx, `function handle(ctx) { while (true) {} }`, scriptContext(`{}`), nil, 65536)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("死循环脚本必须被中断")
	}
	if elapsed > 3*time.Second {
		t.Errorf("中断耗时过长: %v", elapsed)
	}
}

// 沙箱是 deny-by-default：宿主环境不提供任何 I/O 或模块加载。
func TestScriptSandboxHasNoAmbientCapabilities(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	for _, global := range []string{"require", "process", "fetch", "XMLHttpRequest", "setTimeout", "globalThis.Buffer"} {
		source := `function handle(ctx) { return typeof ` + global + `; }`
		output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), nil, 65536)
		if err != nil {
			// 访问不存在的全局对象属性会抛错，同样说明能力不存在
			continue
		}
		if string(output) != `"undefined"` {
			t.Errorf("全局对象 %s 不应存在，实际为 %s", global, output)
		}
	}
}

// 未声明的能力不是「调用时报错」，而是压根不存在于 aegis 对象上。
func TestScriptSDKBindsOnlyDeclaredCapabilities(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		return {
			user: typeof aegis.user,
			points: typeof aegis.points,
			kv: typeof aegis.kv,
			fetch: typeof aegis.fetch,
			log: typeof aegis.log
		};
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`),
		newTestSDK(CapUserRead), 65536)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("返回值不是 JSON: %v", err)
	}
	if decoded["user"] != "object" {
		t.Errorf("已声明 user.read，aegis.user 应存在，实际 %s", decoded["user"])
	}
	if decoded["log"] != "function" {
		t.Errorf("aegis.log 属于基础能力，应始终可用，实际 %s", decoded["log"])
	}
	for _, name := range []string{"points", "kv", "fetch"} {
		if decoded[name] != "undefined" {
			t.Errorf("未声明的能力 aegis.%s 不应被绑定，实际 %s", name, decoded[name])
		}
	}
}

// aegis.fail() 是业务判定（授权过期、次数用尽），要能与「函数崩了」区分开。
func TestScriptFailProducesBusinessError(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	sdk := newTestSDK()
	_, err := executor.Execute(context.Background(),
		`function handle(ctx) { aegis.fail("授权已过期", 40310); return 1; }`,
		scriptContext(`{}`), sdk, 65536)
	if err == nil {
		t.Fatal("aegis.fail 应终止脚本")
	}
	business := sdk.BusinessError()
	if business == nil {
		t.Fatal("应记录业务错误")
	}
	if business.Code != 40310 || business.Message != "授权已过期" {
		t.Errorf("业务错误内容不符: code=%d message=%s", business.Code, business.Message)
	}
}

func TestScriptOutputSizeLimit(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		var s = "";
		for (var i = 0; i < 1000; i++) { s += "0123456789"; }
		return { blob: s };
	}`
	_, err := executor.Execute(context.Background(), source, scriptContext(`{}`), nil, 256)
	if err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超大返回值应被拒绝，实际 err=%v", err)
	}
}

// 顶层代码只允许做声明；SDK 未注入时调用 aegis.* 必须失败，
// 这保证副作用只可能发生在 handle 内部。
func TestScriptValidateRejectsTopLevelSideEffects(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	err := executor.Validate(`aegis.points.add(100); function handle(ctx) { return 1; }`)
	if err == nil {
		t.Fatal("顶层调用 aegis.* 应在发布前被拦截")
	}
}

func TestScriptSourceSizeLimit(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	huge := "function handle(ctx){return 1;}\n// " + strings.Repeat("x", maxScriptSourceBytes)
	if err := executor.Validate(huge); err == nil {
		t.Fatal("超长脚本应被拒绝")
	}
}

// async handle 会返回一个永远不会 resolve 的 Promise（沙箱无事件循环），
// 必须显式报错而不是把空对象当作结果返回。
func TestScriptRejectsAsyncHandle(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	_, err := executor.Execute(context.Background(),
		`async function handle(ctx) { return { ok: true }; }`,
		scriptContext(`{}`), nil, 65536)
	if err == nil {
		t.Fatal("async handle 应被拒绝")
	}
	if !strings.Contains(err.Error(), "async") {
		t.Errorf("错误信息应指出 async 不受支持，实际: %v", err)
	}
}
