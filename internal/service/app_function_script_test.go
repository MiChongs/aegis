package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	functiondomain "aegis/internal/domain/appfunction"

	"github.com/redis/go-redis/v9"
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
		Realtime:      &RealtimeService{},
		Location:      &LocationService{},
		// 客户端建出来但不连：go-redis 是惰性建连的，
		// 只要不发命令就不会有任何网络行为。
		Redis:     redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"}),
		KeyPrefix: "test:",
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
//
// require 尤其要盯住：Buffer / URL 是经 goja_nodejs 的模块系统装上去的，
// 装完必须把 require 全局删掉 —— 留着它等于给脚本开了一个按路径
// 加载宿主文件的入口（那个库默认的加载器就是直接读磁盘）。
func TestScriptSandboxHasNoAmbientCapabilities(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	for _, global := range []string{
		"require", "process", "fetch", "XMLHttpRequest", "setTimeout", "setInterval",
		"globalThis.require", "globalThis.process",
	} {
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

// 反过来：标准全局必须真的在。它们是纯内存类型，与能力声明无关 ——
// 「沙箱里没有 Node」不等于「连一个字节缓冲类型都没有」，后者纯粹是缺东西，
// 而缺了它接第三方二进制接口就只能用字符串硬拼字节。
func TestScriptSandboxProvidesStandardGlobals(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		var url = new URL("https://example.com/a/b?x=1&y=2");
		url.searchParams.set("z", "3");
		return {
			buffer: Buffer.from("往返", "utf8").toString("base64"),
			encoded: new TextEncoder().encode("ab").length,
			decoded: new TextDecoder().decode(new TextEncoder().encode("往返")),
			host: url.hostname,
			query: url.searchParams.get("z"),
			roundTrip: atob(btoa("hello"))
		};
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), nil, 65536)
	if err != nil {
		t.Fatalf("标准全局应可用: %v", err)
	}
	var decoded struct {
		Buffer    string `json:"buffer"`
		Encoded   int    `json:"encoded"`
		Decoded   string `json:"decoded"`
		Host      string `json:"host"`
		Query     string `json:"query"`
		RoundTrip string `json:"roundTrip"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("返回值不是 JSON: %v", err)
	}
	if decoded.Buffer != base64.StdEncoding.EncodeToString([]byte("往返")) {
		t.Errorf("Buffer base64 编码不正确：%s", decoded.Buffer)
	}
	if decoded.Encoded != 2 || decoded.Decoded != "往返" {
		t.Errorf("TextEncoder/TextDecoder 往返失败：%d / %q", decoded.Encoded, decoded.Decoded)
	}
	if decoded.Host != "example.com" || decoded.Query != "3" {
		t.Errorf("URL 解析不正确：%s / %s", decoded.Host, decoded.Query)
	}
	if decoded.RoundTrip != "hello" {
		t.Errorf("atob(btoa()) 应还原原文，实际 %q", decoded.RoundTrip)
	}
}

// 同一份正文只编译一次。
//
// 每次调用新建运行时是隔离要求，每次重新编译只是重复劳动 ——
// 一份 200 行脚本的编译在热路径上比它自己的执行还贵。
func TestScriptExecutorCachesCompiledPrograms(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) { return 1; }`
	for i := 0; i < 5; i++ {
		if _, err := executor.Execute(context.Background(), source, scriptContext(`{}`), nil, 65536); err != nil {
			t.Fatalf("执行失败: %v", err)
		}
	}
	if cached := executor.CachedPrograms(); cached != 1 {
		t.Errorf("同一份正文应只留一份编译产物，实际 %d 份", cached)
	}
	if _, err := executor.Execute(context.Background(),
		`function handle(ctx) { return 2; }`, scriptContext(`{}`), nil, 65536); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if cached := executor.CachedPrograms(); cached != 2 {
		t.Errorf("不同正文应各留一份，实际 %d 份", cached)
	}
}

// 抛错要能定位到行。只给一句 TypeError 而不说哪一行，
// 作者只能把两百行脚本从头读一遍。
func TestScriptErrorCarriesPosition(t *testing.T) {
	executor := NewAppFunctionScriptExecutor()
	source := "function handle(ctx) {\n  var missing = null;\n  return missing.value;\n}"
	_, err := executor.Execute(context.Background(), source, scriptContext(`{}`), nil, 65536)
	if err == nil {
		t.Fatal("读取 null 的属性应抛错")
	}
	line, column, stack := scriptErrorPosition(err)
	if line != 3 {
		t.Errorf("抛错位置应是第 3 行，实际第 %d 行（列 %d）", line, column)
	}
	if len(stack) == 0 {
		t.Error("应带调用栈")
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
