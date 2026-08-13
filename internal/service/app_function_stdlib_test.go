package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 标准库的用例集中回答一个问题：**作者不需要自己手写这些东西了吗**。
// 因此断言的是结果正确性（往返一致、与既有实现同口径），不是「函数存在」。

func runScript(t *testing.T, source string, target any, capabilities ...string) {
	t.Helper()
	executor := NewAppFunctionScriptExecutor()
	output, err := executor.Execute(context.Background(), source, scriptContext(`{}`),
		newTestSDK(capabilities...), 1<<20)
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if err := json.Unmarshal(output, target); err != nil {
		t.Fatalf("返回值不是预期 JSON（%s）: %v", output, err)
	}
}

// 金额运算必须走定点小数。脚本里 0.1 + 0.2 得到 0.30000000000000004，
// 落到账上就是对不平的那一分 —— 而这类错误不报错，只是悄悄给出错误结果。
func TestStdlibDecimalAvoidsFloatDrift(t *testing.T) {
	t.Parallel()

	var got struct {
		Sum     string `json:"sum"`
		Native  string `json:"native"`
		Divided string `json:"divided"`
		Cmp     int    `json:"cmp"`
		Pretty  string `json:"pretty"`
	}
	runScript(t, `function handle(ctx) {
		return {
			sum: aegis.decimal.add("0.1", "0.2"),
			native: String(0.1 + 0.2),
			divided: aegis.decimal.div("1", "3", 4),
			cmp: aegis.decimal.cmp("1.10", "1.2"),
			pretty: aegis.decimal.format("1234567.891", 2)
		};
	}`, &got)

	if got.Sum != "0.3" {
		t.Errorf("定点加法应得 0.3，实际 %q", got.Sum)
	}
	if got.Native == "0.3" {
		t.Error("对照组失效：原生浮点这里本该有误差，说明用例没测到点子上")
	}
	if got.Divided != "0.3333" {
		t.Errorf("除法应按给定精度截断，实际 %q", got.Divided)
	}
	if got.Cmp != -1 {
		t.Errorf("1.10 应小于 1.2，实际 %d", got.Cmp)
	}
	if got.Pretty != "1,234,567.89" {
		t.Errorf("千分位格式化不正确：%q", got.Pretty)
	}
}

// 签名串的顺序必须稳定：JS 对象的遍历顺序对数字键与字符串键规则不同，
// 靠它排序的话同一份参数在不同调用里能拼出两种字符串，而签名会时对时不对。
func TestStdlibQueryStringifyIsDeterministic(t *testing.T) {
	t.Parallel()

	var got struct {
		Signed string         `json:"signed"`
		Parsed map[string]any `json:"parsed"`
	}
	runScript(t, `function handle(ctx) {
		var payload = { zeta: "z", alpha: 1, mid: true };
		return {
			signed: aegis.encoding.queryStringify(payload),
			parsed: aegis.encoding.queryParse("a=1&b=x&b=y")
		};
	}`, &got)

	if got.Signed != "alpha=1&mid=true&zeta=z" {
		t.Errorf("应按键名字典序拼接，实际 %q", got.Signed)
	}
	if got.Parsed["a"] != "1" {
		t.Errorf("单值键应解析成字符串，实际 %v", got.Parsed["a"])
	}
	if list, ok := got.Parsed["b"].([]any); !ok || len(list) != 2 {
		t.Errorf("重复键应解析成数组，实际 %v", got.Parsed["b"])
	}
}

func TestStdlibEncodingRoundTrips(t *testing.T) {
	t.Parallel()

	var got struct {
		Yaml    map[string]any `json:"yaml"`
		CSV     []any          `json:"csv"`
		XML     string         `json:"xml"`
		Gzipped string         `json:"gzipped"`
	}
	runScript(t, `function handle(ctx) {
		return {
			yaml: aegis.encoding.yamlParse("name: aegis\nport: 8088\n"),
			csv: aegis.encoding.csvParse("name,age\nzhang,30\nli,28\n"),
			xml: aegis.json.get(aegis.encoding.xmlToJson("<r><a>1</a></r>"), "r.a"),
			gzipped: aegis.encoding.gunzip(aegis.encoding.gzip("往返一次"))
		};
	}`, &got)

	if got.Yaml["name"] != "aegis" {
		t.Errorf("YAML 解析失败：%v", got.Yaml)
	}
	if len(got.CSV) != 2 {
		t.Fatalf("CSV 应解析出 2 行，实际 %d 行", len(got.CSV))
	}
	if row, ok := got.CSV[0].(map[string]any); !ok || row["name"] != "zhang" {
		t.Errorf("CSV 第一行应按表头成对象，实际 %v", got.CSV[0])
	}
	if got.XML != "1" {
		t.Errorf("XML 转对象后按路径取值应得 1，实际 %q", got.XML)
	}
	if got.Gzipped != "往返一次" {
		t.Errorf("gzip 往返应还原原文，实际 %q", got.Gzipped)
	}
}

// JWT 自己手写的话，base64url 的 padding 与算法白名单十有八九会写错，
// 而写错的表现是「本地能签、对方验不过」或者更糟：alg=none 被接受。
func TestStdlibJWTSignAndVerify(t *testing.T) {
	t.Parallel()

	var got struct {
		Valid   bool   `json:"valid"`
		Subject string `json:"subject"`
		Tampered bool  `json:"tampered"`
	}
	runScript(t, `function handle(ctx) {
		var token = aegis.crypto.jwtSign("s3cret", { sub: "42" }, { expiresIn: 60 });
		var ok = aegis.crypto.jwtVerify("s3cret", token);
		var bad = aegis.crypto.jwtVerify("wrong", token);
		return { valid: ok.valid, subject: ok.claims.sub, tampered: bad.valid };
	}`, &got)

	if !got.Valid || got.Subject != "42" {
		t.Errorf("签发后应能验出原 claims，实际 valid=%v sub=%q", got.Valid, got.Subject)
	}
	if got.Tampered {
		t.Error("换一把密钥必须验不过")
	}
}

// AES-GCM 的价值就在于「改过一个比特就解不开」，
// 因此解密失败必须抛错，绝不能返回一段尽力而为的半截明文。
func TestStdlibAESRoundTripAndTamperDetection(t *testing.T) {
	t.Parallel()

	var got struct {
		Plain   string `json:"plain"`
		Rejected bool  `json:"rejected"`
	}
	runScript(t, `function handle(ctx) {
		var sealed = aegis.crypto.aesEncrypt("key", "机密内容");
		var plain = aegis.crypto.aesDecrypt("key", sealed);
		var rejected = false;
		try { aegis.crypto.aesDecrypt("another", sealed); } catch (e) { rejected = true; }
		return { plain: plain, rejected: rejected };
	}`, &got)

	if got.Plain != "机密内容" {
		t.Errorf("加解密往返应还原原文，实际 %q", got.Plain)
	}
	if !got.Rejected {
		t.Error("换密钥解密必须抛错而不是给出垃圾数据")
	}
}

// 按时区日切：默认的 dayKey 走 UTC，而国内接入方要的几乎都是东八区零点。
// 让作者自己 +8 小时再取日期，会在跨年与月末那两天错一整天。
func TestStdlibTimeZoneAwareDayKey(t *testing.T) {
	t.Parallel()

	var got struct {
		Shanghai string `json:"shanghai"`
		UTC      string `json:"utc"`
		Week     string `json:"week"`
		Cron     bool   `json:"cron"`
		Parsed   int64  `json:"parsed"`
	}
	runScript(t, `function handle(ctx) {
		var next = aegis.time.cronNext("0 3 * * *", 0);
		return {
			shanghai: aegis.time.dayKeyIn("Asia/Shanghai"),
			utc: aegis.time.dayKey(),
			week: aegis.time.weekKey(),
			cron: next > 0,
			parsed: aegis.time.parse("2026-03-21T00:00:00Z")
		};
	}`, &got)

	if len(got.Shanghai) != 10 || len(got.UTC) != 10 {
		t.Errorf("日键格式应是 YYYY-MM-DD，实际 %q / %q", got.Shanghai, got.UTC)
	}
	if !strings.Contains(got.Week, "-W") {
		t.Errorf("周键格式应是 YYYY-Www，实际 %q", got.Week)
	}
	if !got.Cron {
		t.Error("cron 表达式应能算出下一次触发时刻")
	}
	if got.Parsed != 1774051200000 {
		t.Errorf("RFC3339 应解析成 Unix 毫秒，实际 %d", got.Parsed)
	}
}

// 校验要一次给全部错误。只回第一条会让作者陷入
// 「改一个、再跑一次、又冒出一个」的循环，而这些错误本来一次就都知道。
func TestStdlibSchemaValidationReportsEveryError(t *testing.T) {
	t.Parallel()

	var got struct {
		Valid  bool     `json:"valid"`
		Errors []string `json:"errors"`
		Good   bool     `json:"good"`
	}
	runScript(t, `function handle(ctx) {
		var schema = {
			type: "object",
			required: ["name", "age"],
			properties: { name: { type: "string" }, age: { type: "number" } }
		};
		var bad = aegis.validate.schema(schema, { age: "x" });
		var good = aegis.validate.schema(schema, { name: "a", age: 1 });
		return { valid: bad.valid, errors: bad.errors, good: good.valid };
	}`, &got)

	if got.Valid {
		t.Error("缺字段且类型不对，不该判通过")
	}
	if len(got.Errors) < 2 {
		t.Errorf("应同时报出「缺 name」与「age 类型不对」，实际 %v", got.Errors)
	}
	if !got.Good {
		t.Error("合法数据应通过")
	}
}

// 文本遮罩必须与平台既有口径一致：同一个邮箱在资料页与脚本产出里
// 遮成两个样子，看的人会以为是两条记录。
func TestStdlibTextHelpersMatchPlatformMasking(t *testing.T) {
	t.Parallel()

	var got struct {
		Email     string `json:"email"`
		Phone     string `json:"phone"`
		Slug      string `json:"slug"`
		Pinyin    string `json:"pinyin"`
		Initials  string `json:"initials"`
		Truncated string `json:"truncated"`
		Safe      string `json:"safe"`
	}
	runScript(t, `function handle(ctx) {
		return {
			email: aegis.text.maskEmail("zhangsan@example.com"),
			phone: aegis.text.maskPhone("13800001111"),
			slug: aegis.text.slugify("  Hello  World! "),
			pinyin: aegis.text.pinyin("北京"),
			initials: aegis.text.pinyin("北京", "initials"),
			truncated: aegis.text.truncate("一二三四五", 3),
			safe: aegis.text.sanitizeHtml("<p class=\"x\">ok</p><script>alert(1)</script>")
		};
	}`, &got)

	if got.Email != maskEmail("zhangsan@example.com") {
		t.Errorf("邮箱遮罩应与平台一致，实际 %q", got.Email)
	}
	if got.Phone != maskPhoneValue("13800001111") {
		t.Errorf("手机遮罩应与平台一致，实际 %q", got.Phone)
	}
	if got.Slug != "hello-world" {
		t.Errorf("slug 不正确：%q", got.Slug)
	}
	if got.Pinyin != "bei jing" || got.Initials != "bj" {
		t.Errorf("拼音不正确：%q / %q", got.Pinyin, got.Initials)
	}
	if got.Truncated != "一二三…" {
		t.Errorf("应按字符数截断（不劈开汉字），实际 %q", got.Truncated)
	}
	if strings.Contains(got.Safe, "<script") || !strings.Contains(got.Safe, `class="x"`) {
		t.Errorf("净化应拒绝脚本、放行排版 class，实际 %q", got.Safe)
	}
}

// aegis.lock.run 在宿主回调里跑一段脚本，那段脚本抛出的错误必须原样送回 JS 栈。
//
// 直接把 Go 侧的 error 拿去 panic 是不行的：goja 只认 Value / *Exception /
// uncatchableException 三类，别的类型会继续向上 panic 把请求 goroutine 带崩。
// 这条用例同时守两件事：进程不炸，且 aegis.fail 的业务码不被盖掉。
func TestLockRunPropagatesBusinessError(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	// 试跑档：不真的占 Redis 的锁，但 heldLocks 的记账逻辑完全一致
	sdk := newDryRunTestSDK(CapLockAcquire)
	scriptCtx := scriptContext(`{}`)
	scriptCtx.DryRun = true
	_, err := executor.Execute(context.Background(),
		`function handle(ctx) {
			return aegis.lock.run("k", function () { aegis.fail("库存不足", 40904); });
		}`, scriptCtx, sdk, 65536)
	if err == nil {
		t.Fatal("锁内抛错应终止脚本")
	}
	business := sdk.BusinessError()
	if business == nil || business.Code != 40904 {
		t.Errorf("锁内的 aegis.fail 应原样透出业务码，实际 %+v", business)
	}
	// 锁必须已经归还：脚本抛错的那条路径上没有任何代码会走到 release
	sdk.Release()
	if len(sdk.heldLocks) != 0 {
		t.Errorf("收口后不该还持有锁，实际 %v", sdk.heldLocks)
	}
}

// aegis.assert 是前置校验的收口写法。它必须与 aegis.fail 走同一条业务错误通道，
// 否则「参数不对」会被记成「函数崩了」。
func TestStdlibAssertProducesBusinessError(t *testing.T) {
	t.Parallel()

	executor := NewAppFunctionScriptExecutor()
	sdk := newTestSDK()
	_, err := executor.Execute(context.Background(),
		`function handle(ctx) { aegis.assert(false, "缺少 orderNo", 40002); return 1; }`,
		scriptContext(`{}`), sdk, 65536)
	if err == nil {
		t.Fatal("断言不成立应终止脚本")
	}
	business := sdk.BusinessError()
	if business == nil || business.Code != 40002 || business.Message != "缺少 orderNo" {
		t.Errorf("应记成业务错误，实际 %+v", business)
	}

	// 断言成立时不该有任何影响
	passing := newTestSDK()
	if _, err := executor.Execute(context.Background(),
		`function handle(ctx) { aegis.assert(1 === 1, "不该触发"); return "ok"; }`,
		scriptContext(`{}`), passing, 65536); err != nil {
		t.Fatalf("断言成立时不应报错: %v", err)
	}
	if passing.BusinessError() != nil {
		t.Error("断言成立时不该记业务错误")
	}
}
