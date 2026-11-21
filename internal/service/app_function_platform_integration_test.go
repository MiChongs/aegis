package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	functiondomain "aegis/internal/domain/appfunction"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
)

// 远程函数平台化的端到端集成测试（真实 Postgres）：
// 权益发放（物品发放同一事务、幂等键回放）→ 调用记录留存来源 / 入参 / 日志 / 额度 → 事件触发器（钩子入队、
// 同一事件只入一次、后台循环抢占执行、ctx.trigger）→ 定时触发器（比较交换推进、入队执行）→ 立即触发 →
// 函数互调（真实与试跑）→ 全景 → 重放。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55466/postgres go test ./internal/service -run TestAppFunctionPlatformIntegration
func TestAppFunctionPlatformIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	log := zap.NewNop()
	svc := NewAppFunctionService(log, pg, "integration-root-secret")
	svc.SetScriptDeps(ScriptSDKDeps{
		Points:        NewPointsService(log, pg, nil),
		Vip:           NewVipService(log, pg),
		Notifications: &NotificationService{},
		Audit:         NewAuditService(log, pg),
	})
	const appID = int64(40720)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'functions') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "fn-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "fn-bob", "0")

	deploy := func(name string, capabilities []string, source string) {
		t.Helper()
		fn, err := svc.CreateFunction(ctx, functiondomain.CreateFunctionInput{
			AppID: appID, Name: name, Runtime: functiondomain.RuntimeScript, Capabilities: capabilities, TimeoutMs: 5000,
		})
		if err != nil {
			t.Fatalf("建函数 %s: %v", name, err)
		}
		if _, err := svc.CreateVersion(ctx, fn, functiondomain.CreateVersionInput{AppID: appID, FunctionID: fn.ID, Version: "1.0.0", Source: source}); err != nil {
			t.Fatalf("发布 %s: %v", name, err)
		}
		if err := svc.ActivateVersion(ctx, appID, name, "1.0.0"); err != nil {
			t.Fatalf("激活 %s: %v", name, err)
		}
	}
	userCaller := func(id int64) functiondomain.Caller {
		return functiondomain.Caller{Type: functiondomain.CallerUser, UserID: &id}
	}
	decode := func(raw json.RawMessage) map[string]any {
		t.Helper()
		out := map[string]any{}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("输出不是对象：%s", raw)
		}
		return out
	}

	// ── 权益发放：物品发放同一个事务，幂等键回放 ──
	deploy("welcome", []string{functiondomain.CapRightsRead, functiondomain.CapRightsGrant, functiondomain.CapPointsRead},
		`function handle(ctx) {
  console.log("发欢迎礼", ctx.source);
  var grant = aegis.rights.grant([{ type: "points", amount: 50 }, { type: "lottery_draws", amount: 3 }], { key: "welcome", title: "欢迎礼" });
  var rights = aegis.rights.summary();
  return { grantNo: grant.grantNo, replayed: grant.replayed, results: grant.results.length,
           balance: aegis.points.balance(), tickets: rights.lotteryTickets, source: ctx.source,
           trigger: ctx.trigger ? ctx.trigger.event || ctx.trigger.kind : null };
}`)
	first, err := svc.Invoke(ctx, appID, "welcome", uuid.NewString(), json.RawMessage(`{"from":"test"}`), userCaller(alice))
	if err != nil {
		t.Fatalf("首次调用: %v", err)
	}
	out := decode(first.Output)
	if out["replayed"] != false || out["balance"].(float64) != 50 || out["tickets"].(float64) != 3 || out["results"].(float64) != 2 {
		t.Fatalf("首次发放结果不对：%v", out)
	}
	if !strings.HasPrefix(out["grantNo"].(string), "AFG") || out["source"] != functiondomain.SourceAPI {
		t.Fatalf("发放单号或来源不对：%v", out)
	}
	second, err := svc.Invoke(ctx, appID, "welcome", uuid.NewString(), json.RawMessage(`{}`), userCaller(alice))
	if err != nil {
		t.Fatalf("第二次调用: %v", err)
	}
	if again := decode(second.Output); again["replayed"] != true || again["balance"].(float64) != 50 || again["grantNo"] != out["grantNo"] {
		t.Fatalf("同一个幂等键应回放第一次的结果、不再多发：%v", again)
	}

	// 调用记录：来源、入参、日志、额度、版本号
	page, err := svc.ListInvocations(ctx, appID, "welcome", functiondomain.InvocationQuery{Source: functiondomain.SourceAPI})
	if err != nil || page.Total != 2 {
		t.Fatalf("按来源筛选应有 2 条：%v %+v", err, page)
	}
	detail, err := svc.GetInvocation(ctx, appID, "welcome", page.List[len(page.List)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Version != "1.0.0" || string(detail.Input) != `{"from": "test"}` && string(detail.Input) != `{"from":"test"}` {
		t.Fatalf("详情应带版本号与入参：%+v", detail)
	}
	if len(detail.Logs) != 1 || !strings.Contains(detail.Logs[0].Message, "发欢迎礼") || detail.Usage == nil || detail.Usage.Mutations != 1 {
		t.Fatalf("详情应留下日志与额度：logs=%v usage=%+v", detail.Logs, detail.Usage)
	}
	if detail.Result == nil || len(detail.Result.Effects) != 1 || detail.Result.Effects[0].Type != functiondomain.CapRightsGrant {
		t.Fatalf("详情应带 rights.grant 副作用：%+v", detail.Result)
	}

	// 重放：用这次的入参与身份在激活版本上试跑，写只记不做
	replay, err := svc.ReplayInvocation(ctx, appID, "welcome", detail.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.OK || len(replay.Effects) != 1 || !replay.Effects[0].Simulated {
		t.Fatalf("重放应成功且副作用标记为模拟：%+v", replay)
	}
	var integral int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id=$1`, alice).Scan(&integral); err != nil || integral != 50 {
		t.Fatalf("重放不应真的发放：integral=%d err=%v", integral, err)
	}

	// ── 事件触发器：钩子入队 → 后台循环执行 ──
	trigger, err := svc.CreateTrigger(ctx, appID, "welcome", functiondomain.TriggerInput{
		Kind: functiondomain.TriggerKindEvent, Event: functiondomain.EventUserRegistered, Description: "注册即发欢迎礼",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateTrigger(ctx, appID, "welcome", functiondomain.TriggerInput{Kind: functiondomain.TriggerKindEvent, Event: "user.nope"}, nil); err == nil {
		t.Fatal("未登记的事件应被拒")
	}
	hook := func() {
		svc.OnHook(HookUserRegistered, map[string]any{"account": "fn-bob", "secret": "不该外泄"},
			plugindomain.HookMetadata{AppID: ptrInt64(appID), UserID: ptrInt64(bob)})
	}
	hook()
	hook() // 同一个人注册只入队一次
	waitFor(t, func() bool {
		var count int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_function_trigger_jobs WHERE trigger_id=$1`, trigger.ID).Scan(&count)
		return count == 1
	})
	time.Sleep(300 * time.Millisecond)
	var jobs int
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_function_trigger_jobs WHERE trigger_id=$1`, trigger.ID).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("同一事件应只入队一次，实际 %d", jobs)
	}
	svc.triggerTick(ctx)
	jobPage, err := svc.ListTriggerJobs(ctx, appID, "welcome", functiondomain.TriggerJobQuery{TriggerID: trigger.ID})
	if err != nil || len(jobPage.Items) != 1 || jobPage.Items[0].Status != functiondomain.JobSucceeded {
		t.Fatalf("事件任务应执行成功：%v %+v", err, jobPage)
	}
	if data := string(jobPage.Items[0].Data); strings.Contains(data, "secret") || !strings.Contains(data, "fn-bob") {
		t.Fatalf("事件数据只应带目录登记过的字段：%s", data)
	}
	eventCalls, err := svc.ListInvocations(ctx, appID, "welcome", functiondomain.InvocationQuery{TriggerID: trigger.ID})
	if err != nil || eventCalls.Total != 1 || eventCalls.List[0].Source != functiondomain.SourceEvent || eventCalls.List[0].CallerType != functiondomain.CallerEvent {
		t.Fatalf("应有一条事件来源的调用：%v %+v", err, eventCalls)
	}
	eventDetail, _ := svc.GetInvocation(ctx, appID, "welcome", eventCalls.List[0].ID)
	if got := decode(eventDetail.Result.Output); got["trigger"] != functiondomain.EventUserRegistered || got["balance"].(float64) != 50 {
		t.Fatalf("事件调用应锁定到事件里的用户并带 ctx.trigger：%v", got)
	}
	reloaded, _ := pg.GetAppFunctionTrigger(ctx, appID, 0, trigger.ID)
	if reloaded.FireCount != 1 || reloaded.LastStatus != functiondomain.JobSucceeded {
		t.Fatalf("触发器应记下一次成功：%+v", reloaded)
	}

	// ── 定时触发器：比较交换推进 + 入队执行 ──
	deploy("nightly", []string{functiondomain.CapKVWrite}, `function handle(ctx) {
  var n = aegis.kv.incr("runs", 1, 0);
  var t = ctx.trigger || {};
  return { kind: t.kind || ctx.source, cron: t.cron, caller: ctx.caller.type, runs: n, input: ctx.input.mode };
}`)
	schedule, err := svc.CreateTrigger(ctx, appID, "nightly", functiondomain.TriggerInput{
		Kind: functiondomain.TriggerKindSchedule, Cron: "0 3 * * *", Timezone: "Asia/Shanghai", Input: json.RawMessage(`{"mode":"full"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if schedule.NextRunAt == nil || schedule.NextRunAt.Before(time.Now()) {
		t.Fatalf("定时触发器应算出未来的下一次：%+v", schedule.NextRunAt)
	}
	if _, err := svc.CreateTrigger(ctx, appID, "nightly", functiondomain.TriggerInput{Kind: functiondomain.TriggerKindSchedule, Cron: "every day"}, nil); err == nil {
		t.Fatal("无效 cron 应被拒")
	}
	if _, err := pool.Exec(ctx, `UPDATE app_function_triggers SET next_run_at = NOW() - INTERVAL '1 minute' WHERE id=$1`, schedule.ID); err != nil {
		t.Fatal(err)
	}
	svc.triggerTick(ctx)
	scheduled, _ := pg.GetAppFunctionTrigger(ctx, appID, 0, schedule.ID)
	if scheduled.NextRunAt == nil || !scheduled.NextRunAt.After(time.Now()) || scheduled.FireCount != 1 {
		t.Fatalf("到点后应推进到下一次并执行一次：%+v", scheduled)
	}
	nightly, _ := svc.ListInvocations(ctx, appID, "nightly", functiondomain.InvocationQuery{Source: functiondomain.SourceSchedule})
	if nightly.Total != 1 {
		t.Fatalf("应有一条定时来源的调用：%+v", nightly)
	}
	nightlyDetail, _ := svc.GetInvocation(ctx, appID, "nightly", nightly.List[0].ID)
	if got := decode(nightlyDetail.Result.Output); got["kind"] != "schedule" || got["cron"] != "0 3 * * *" || got["caller"] != "schedule" || got["input"] != "full" {
		t.Fatalf("定时调用的 ctx 不对：%v", got)
	}

	// 立即触发（同步，真实执行）
	fired, err := svc.FireTrigger(ctx, appID, "nightly", schedule.ID, functiondomain.TriggerFireInput{})
	if err != nil || !fired.OK || decode(fired.Output)["runs"].(float64) != 2 {
		t.Fatalf("立即触发应真实执行：%v %+v", err, fired)
	}
	if _, err := svc.FireTrigger(ctx, appID, "welcome", trigger.ID, functiondomain.TriggerFireInput{}); err == nil {
		t.Fatal("事件触发器立即触发时必须指定用户")
	}

	// ── 函数互调 ──
	deploy("outer", []string{functiondomain.CapFunctionCall}, `function handle(ctx) {
  var inner = aegis.functions.call("welcome", {});
  var failed = null;
  try { aegis.functions.call("missing", {}); } catch (e) { failed = String(e); }
  return { inner: inner.replayed, depth: ctx.depth, failed: failed };
}`)
	outer, err := svc.Invoke(ctx, appID, "outer", uuid.NewString(), nil, userCaller(alice))
	if err != nil {
		t.Fatalf("互调: %v", err)
	}
	if got := decode(outer.Output); got["inner"] != true || got["depth"].(float64) != 0 || !strings.Contains(got["failed"].(string), "不存在") {
		t.Fatalf("互调结果不对：%v", got)
	}
	nested, _ := svc.ListInvocations(ctx, appID, "welcome", functiondomain.InvocationQuery{Source: functiondomain.SourceFunction})
	if nested.Total != 1 {
		t.Fatalf("被调函数应留下一条 function 来源的调用：%+v", nested)
	}
	dry, err := svc.TestScript(ctx, appID, "outer", functiondomain.TestRequest{
		Source: `function handle(ctx) { return aegis.functions.call("nightly", {}); }`,
	}, nil)
	if err != nil || !dry.OK {
		t.Fatalf("试跑互调: %v %+v", err, dry)
	}
	var runs int64
	_ = pool.QueryRow(ctx, `SELECT (value::text)::bigint FROM app_function_kv WHERE appid=$1 AND key='runs'`, appID).Scan(&runs)
	if runs != 2 {
		t.Fatalf("试跑里的互调不应真的写 KV：runs=%d", runs)
	}

	// ── 全景 ──
	overview, err := svc.Overview(ctx, appID, 24)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Functions != 3 || overview.Active != 3 || overview.Triggers != 2 || overview.Total < 6 || overview.Failed != 0 {
		t.Fatalf("全景计数不对：%+v", overview)
	}
	sources := map[string]int64{}
	for _, row := range overview.Sources {
		sources[row.Source] = row.Count
	}
	if sources[functiondomain.SourceEvent] != 1 || sources[functiondomain.SourceSchedule] != 2 || sources[functiondomain.SourceFunction] != 1 {
		t.Fatalf("按来源的计数不对：%v", sources)
	}
	for _, item := range overview.Items {
		if item.Name == "welcome" && (item.Triggers != 1 || item.Total < 4 || len(item.Spark) != 25) {
			t.Fatalf("welcome 的全景行不对：%+v", item)
		}
	}
}

func ptrInt64(v int64) *int64 { return &v }
