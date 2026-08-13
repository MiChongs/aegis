package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	functiondomain "aegis/internal/domain/appfunction"

	"go.uber.org/zap"
)

// 目录 ↔ 绑定分支，双向钉死。
//
// 目录多一条 → 控制台上勾得上，脚本里却没有那个对象（`aegis.wallet is undefined`）。
// 绑定多一条 → 没声明也能调，「声明即授权」当场失效。
// 两种漂移都不会有编译错误，也不会有运行时报错，只会在某个脚本里表现为
// 一句莫名其妙的 TypeError —— 而那时距离改动已经过去很久了。
func TestCapabilityCatalogMatchesBinders(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	// 逐项能力单独绑定，确认「勾了就有、没勾就没有」
	for _, capability := range functiondomain.CapabilityCatalog() {
		if capability.Deprecated {
			continue
		}
		namespace := capability.Namespace
		if namespace == "" {
			// 挂在 aegis 根上的能力：从 API 字面量里取成员名（aegis.fetch → fetch）
			namespace = rootMemberOf(capability)
		}
		if namespace == "" {
			t.Errorf("%s 既没有命名空间也推导不出根成员名", capability.Key)
			continue
		}

		source := `function handle(ctx) { return { present: typeof aegis.` + namespace + ` }; }`
		output, err := executor.Execute(context.Background(), source, scriptContext(`{}`),
			newTestSDK(capability.Key), 65536)
		if err != nil {
			t.Errorf("%s：绑定失败 %v", capability.Key, err)
			continue
		}
		var decoded struct {
			Present string `json:"present"`
		}
		if err := json.Unmarshal(output, &decoded); err != nil {
			t.Fatalf("返回值不是 JSON: %v", err)
		}
		if decoded.Present == "undefined" {
			t.Errorf("目录里有 %s，但 SDK 没有绑定 aegis.%s —— 勾上它脚本里也没有这个对象",
				capability.Key, namespace)
		}
	}
}

// 反方向：一个能力都不声明时，aegis 上只应剩下免声明的那几项。
func TestNoCapabilityBindsNothingExtra(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		var keys = [];
		for (var k in aegis) { keys.push(k); }
		keys.sort();
		return keys;
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), newTestSDK(), 65536)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var keys []string
	if err := json.Unmarshal(output, &keys); err != nil {
		t.Fatalf("返回值不是 JSON: %v", err)
	}
	// 与 domain 的 BaseSDKMembers 是同一份清单：一边加了成员另一边没加，
	// 静态分析器会把一个真实存在的成员报成「SDK 上没有」。
	want := append([]string{}, functiondomain.BaseSDKMembers()...)
	sort.Strings(want)
	sort.Strings(keys)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("零声明时 aegis 上应只有免声明成员 %v，实际 %v", want, keys)
	}
}

// console 是 aegis.log 的别名。没有它的话，几乎每个作者的第一行 console.log
// 都会撞上 ReferenceError —— 而「沙箱里没有 DOM」与「没人绑 console」是两回事。
func TestConsoleIsBoundAsLogAlias(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	sdk := newTestSDK()
	source := `function handle(ctx) {
		console.log("hello", { a: 1 });
		console.warn("careful");
		return typeof console.error;
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), sdk, 65536)
	if err != nil {
		t.Fatalf("console 应可用: %v", err)
	}
	if string(output) != `"function"` {
		t.Errorf("console.error 应是函数，实际 %s", output)
	}
	logs := sdk.Logs()
	if len(logs) != 2 {
		t.Fatalf("应收集到 2 行日志，实际 %d 行：%v", len(logs), logs)
	}
	// 对象要序列化成 JSON 而不是 [object Object]，否则日志里什么也读不出来
	if !strings.Contains(logs[0].Message, `{"a":1}`) {
		t.Errorf("对象参数应被序列化，实际 %q", logs[0].Message)
	}
	// 级别是结构化字段而不是消息前缀：靠 "warn " 前缀切级别，
	// 在消息本身以 warn 开头时就会切错。
	if logs[1].Level != "warn" {
		t.Errorf("console.warn 应记为 warn 级别，实际 %q", logs[1].Level)
	}
	if strings.HasPrefix(logs[1].Message, "warn ") {
		t.Errorf("级别不该再被拼进消息里，实际 %q", logs[1].Message)
	}
}

// 免声明的 crypto / time 是脚本自给自足的基础：不给的话作者只能自己手写
// MD5 与日切逻辑，那两件事都很容易写错且不会报错。
func TestBaseCryptoAndTimeAreUsable(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		return {
			md5: aegis.crypto.md5("abc"),
			hmac: aegis.crypto.hmacSha256("k", "v"),
			b64: aegis.crypto.base64Decode(aegis.crypto.base64Encode("往返")),
			equal: aegis.crypto.timingSafeEqual("a", "a"),
			notEqual: aegis.crypto.timingSafeEqual("a", "ab"),
			uuidLen: aegis.crypto.uuid().length,
			dayKeyLen: aegis.time.dayKey().length,
			monthKeyLen: aegis.time.monthKey().length
		};
	}`
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), newTestSDK(), 65536)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	var decoded struct {
		MD5         string `json:"md5"`
		HMAC        string `json:"hmac"`
		B64         string `json:"b64"`
		Equal       bool   `json:"equal"`
		NotEqual    bool   `json:"notEqual"`
		UUIDLen     int    `json:"uuidLen"`
		DayKeyLen   int    `json:"dayKeyLen"`
		MonthKeyLen int    `json:"monthKeyLen"`
	}
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("返回值不是 JSON: %v", err)
	}
	if decoded.MD5 != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("md5(\"abc\") 不正确：%s", decoded.MD5)
	}
	if len(decoded.HMAC) != 64 {
		t.Errorf("hmacSha256 应是 64 位十六进制，实际 %q", decoded.HMAC)
	}
	if decoded.B64 != "往返" {
		t.Errorf("base64 往返应还原原文，实际 %q", decoded.B64)
	}
	if !decoded.Equal || decoded.NotEqual {
		t.Errorf("timingSafeEqual 判定错误：equal=%v notEqual=%v", decoded.Equal, decoded.NotEqual)
	}
	if decoded.UUIDLen != 36 {
		t.Errorf("uuid 长度应为 36，实际 %d", decoded.UUIDLen)
	}
	if decoded.DayKeyLen != 10 || decoded.MonthKeyLen != 7 {
		t.Errorf("日键/月键格式不对：%d / %d", decoded.DayKeyLen, decoded.MonthKeyLen)
	}
}

// 函数配置对脚本可见，且顶层永远是对象 —— 未配置时也不该是 undefined，
// 否则模板里那句 `aegis.config.dailyQuota || 100` 会直接抛 TypeError。
func TestFunctionConfigIsExposedToScript(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) {
		return { quota: aegis.config.dailyQuota || 0, type: typeof aegis.config };
	}`

	withConfig := newTestSDKWithConfig(json.RawMessage(`{"dailyQuota":42}`))
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`), withConfig, 65536)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if !strings.Contains(string(output), `"quota":42`) {
		t.Errorf("脚本应读到配置值，实际 %s", output)
	}

	empty := newTestSDK()
	output, err = executor.Execute(context.Background(), source, scriptContext(`{}`), empty, 65536)
	if err != nil {
		t.Fatalf("未配置时也应能执行: %v", err)
	}
	if !strings.Contains(string(output), `"type":"object"`) {
		t.Errorf("未配置时 aegis.config 仍应是对象，实际 %s", output)
	}
}

// 试跑时写操作只记录不执行。
//
// 这里的 SDK 依赖全是零值指针、PG 为 nil：只要有任何一个写操作真的落到
// 业务方法或数据库上，测试会当场 panic —— 这比断言「没写进去」更严格，
// 因为后者在数据库不可达的测试环境里恒为真。
func TestDryRunRecordsEffectsWithoutExecuting(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	sdk := newDryRunTestSDK(CapKVWrite, CapNotificationSend, CapAuditWrite, CapHTTPFetch)
	source := `function handle(ctx) {
		aegis.kv.set("k", { v: 1 }, 60);
		aegis.notify.send("标题", "内容");
		aegis.audit.log("tested", "试跑");
		var response = aegis.fetch("https://example.com/hook", { method: "POST", body: { a: 1 } });
		return { dryRun: ctx.dryRun, delivered: 1, simulated: response.simulated === true };
	}`
	scriptCtx := scriptContext(`{}`)
	scriptCtx.DryRun = true
	output, err := executor.Execute(context.Background(), source, scriptCtx, sdk, 65536)
	if err != nil {
		t.Fatalf("试跑不应失败: %v", err)
	}
	if !strings.Contains(string(output), `"dryRun":true`) {
		t.Errorf("脚本应能从 ctx.dryRun 判断出这是试跑，实际 %s", output)
	}
	// 非安全方法不实际发出：POST 可能是一次扣款、一条短信、一封信
	if !strings.Contains(string(output), `"simulated":true`) {
		t.Errorf("试跑时的 POST 应被跳过并标记 simulated，实际 %s", output)
	}

	effects := sdk.Effects()
	if len(effects) != 4 {
		t.Fatalf("应记录 4 条副作用，实际 %d 条", len(effects))
	}
	for _, effect := range effects {
		if !effect.Simulated {
			t.Errorf("试跑产生的副作用必须标记 simulated，否则会被当成真的发生过：%s", effect.Type)
		}
	}
}

// 平台自用的 KV 前缀对脚本不可见：能读能写就意味着脚本可以把
// 限制自己的那个频次计数清零，那个限制就等于不存在。
func TestScriptCannotTouchReservedKVKeys(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	source := `function handle(ctx) { return aegis.kv.get("` + kvReservedPrefix + `rate:1:2"); }`
	_, err := executor.Execute(context.Background(), source, scriptContext(`{}`),
		newTestSDK(CapKVRead), 65536)
	if err == nil {
		t.Fatal("读取平台保留键应被拒绝")
	}
	if !strings.Contains(err.Error(), kvReservedPrefix) {
		t.Errorf("错误信息应说明是保留前缀，实际：%v", err)
	}
}

// script 是控制台上的默认运行时。这里曾经只放行 wasm / http，
// 于是「创建远程函数」表单按默认值提交必然失败，功能从第一步就走不通。
func TestCreateFunctionAcceptsScriptRuntime(t *testing.T) {
	t.Parallel()

	for _, runtime := range []string{
		functiondomain.RuntimeScript, functiondomain.RuntimeWASM, functiondomain.RuntimeHTTP,
	} {
		if err := validateFunctionRuntime(runtime); err != nil {
			t.Errorf("运行时 %s 应被接受，实际 %v", runtime, err)
		}
	}
	if err := validateFunctionRuntime("python"); err == nil {
		t.Error("未知运行时应被拒绝")
	}
}

// 配置顶层必须是对象：数组或标量会让脚本里的 aegis.config.xxx 恒为 undefined，
// 而那种失败不会报错，只会让阈值静默变回默认值。
func TestNormalizeFunctionConfigRequiresObject(t *testing.T) {
	t.Parallel()

	if got, err := normalizeFunctionConfig(nil); err != nil || string(got) != "{}" {
		t.Errorf("空配置应归一化为 {}，实际 %s / %v", got, err)
	}
	if _, err := normalizeFunctionConfig(json.RawMessage(`[1,2]`)); err == nil {
		t.Error("数组配置应被拒绝")
	}
	if _, err := normalizeFunctionConfig(json.RawMessage(`"text"`)); err == nil {
		t.Error("标量配置应被拒绝")
	}
	got, err := normalizeFunctionConfig(json.RawMessage(`{"a": 1}`))
	if err != nil || !strings.Contains(string(got), `"a":1`) {
		t.Errorf("合法对象应通过，实际 %s / %v", got, err)
	}
}

// rootMemberOf 从能力的 API 字面量里推导它挂在 aegis 上的成员名。
// 目录里 API 写成 `aegis.fetch(url, options)` / `aegis.kv.get / list / has`。
func rootMemberOf(capability functiondomain.Capability) string {
	const prefix = "aegis."
	index := strings.Index(capability.API, prefix)
	if index < 0 {
		return ""
	}
	rest := capability.API[index+len(prefix):]
	return strings.FieldsFunc(rest, func(r rune) bool {
		return r == '.' || r == '(' || r == ' ' || r == '/'
	})[0]
}

// 确保测试里那个 zap logger 不会因为 SDK 依赖缺失而 panic。
var _ = zap.NewNop
