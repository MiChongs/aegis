package httptransport

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"
	systemdomain "aegis/internal/domain/system"
	fw "aegis/internal/firewall"
	"aegis/internal/middleware"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 防火墙规则集 v2 的端到端集成测试：真实 Postgres（迁移 000174）+ miniredis。
// 覆盖自定义规则与 WAF 例外的增删改（含试编译拒绝）、测试台（含草稿例外）、事件落库与筛选、
// 统计只数拦截、规则组从命中明细聚合、命中数回写、设置分区保存与热更新。
//
// 用独立库，别指向别人在用的库（openTestDatabase 会清空 public schema）：
//
//	AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/aegis_fw_test go test ./internal/transport/http -run TestFirewallIntegration
func TestFirewallIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetRelayTestDatabase(t, ctx, pool)
	pg := pgrepo.New(pool)

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rc := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })

	base := config.FirewallConfig{Enabled: true, CorazaEnabled: true}
	firewall, err := middleware.NewFirewall(base, zap.NewNop(), rc, "fwit", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := service.NewFirewallPolicyService(zap.NewNop(), pg, firewall)
	if err := policy.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	// ── 自定义规则 ──
	if _, err := policy.CreateRule(ctx, service.FirewallRuleInput{Name: "坏表达式", Enabled: true, Expression: "ip ==", Action: fw.ActionBlock}, 1); err == nil {
		t.Fatal("非法表达式必须拒绝")
	}
	if _, err := policy.CreateRule(ctx, service.FirewallRuleInput{Name: "只有拦截能封", Enabled: true, Expression: `ip == "1.1.1.1"`, Action: fw.ActionLog, BanSeconds: 60}, 1); err == nil {
		t.Fatal("log 动作不能带封禁")
	}
	ruleA, err := policy.CreateRule(ctx, service.FirewallRuleInput{Name: "拦 curl 写请求", Enabled: true, Priority: 20, Expression: `ua startsWith "curl/" && method == "POST"`, Action: fw.ActionBlock, BanSeconds: 600}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ruleB, err := policy.CreateRule(ctx, service.FirewallRuleInput{Name: "探针放行", Enabled: true, Priority: 10, Expression: `headers["x-probe"] == "ok"`, Action: fw.ActionAllow}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rt := policy.Runtime(); rt.CustomRules != 2 || !rt.WAFActive {
		t.Fatalf("运行态不对：%+v", rt)
	}
	if err := policy.ReorderRules(ctx, []int64{ruleA.ID, ruleB.ID}, 1); err != nil {
		t.Fatal(err)
	}
	rules, _ := policy.ListRules(ctx)
	if len(rules) != 2 || rules[0].ID != ruleA.ID || rules[0].Priority != 10 {
		t.Fatalf("重排后顺序不对：%+v", rules)
	}
	if err := policy.ToggleRule(ctx, ruleB.ID, false, 1); err != nil {
		t.Fatal(err)
	}
	if rt := policy.Runtime(); rt.CustomRules != 1 {
		t.Fatalf("停用后应剩 1 条：%+v", rt)
	}

	// ── 测试台 ──
	attack := service.SandboxInput{Method: "POST", URL: "/api/v1/apps/voyage_main/user/profile", Headers: map[string]string{"Content-Type": "application/json"},
		Body: `{"bio":"<script>alert(document.cookie)</script>"}`, Transport: "sealed"}
	res, err := policy.Sandbox(ctx, attack)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "block" || res.InboundScore < 5 || len(res.Matches) == 0 {
		t.Fatalf("测试台应判拦截：%+v", res)
	}
	draft := service.FirewallExclusionInput{Name: "bio 放行 XSS", PathMatch: "prefix", Path: "/api/v1/apps/voyage_main/user/", Tags: []string{"attack-xss"}, Targets: []string{"ARGS:json.bio"}}
	attack.DraftExclusion = &draft
	res, err = policy.Sandbox(ctx, attack)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != "pass" || !res.UsedDraft {
		t.Fatalf("草稿例外应消掉误报：%+v", res)
	}
	curl := service.SandboxInput{Method: "POST", URL: "/api/v1/apps/a/x", Headers: map[string]string{"User-Agent": "curl/8.0"}}
	if res, err = policy.Sandbox(ctx, curl); err != nil || res.Verdict != "block" || res.CustomRule == nil || res.CustomRule.ID != ruleA.ID {
		t.Fatalf("测试台应命中自定义规则：%+v %v", res, err)
	}

	// ── WAF 例外 ──
	if _, err := policy.CreateExclusion(ctx, service.FirewallExclusionInput{Name: "坏正则", Enabled: true, PathMatch: "regex", Path: "^/a(", RuleIDs: []int{941100}}, 1); err == nil {
		t.Fatal("非法正则必须拒绝")
	}
	draft.Enabled = true
	ex, err := policy.CreateExclusion(ctx, draft, 1)
	if err != nil {
		t.Fatal(err)
	}
	attack.DraftExclusion = nil
	if res, _ = policy.Sandbox(ctx, attack); res.Verdict != "pass" {
		t.Fatalf("保存后的例外应在线上生效：%+v", res)
	}
	if !strings.Contains(policy.Directives(), "ARGS:json.bio") {
		t.Fatal("指令里应能看到例外")
	}
	if err := policy.ToggleExclusion(ctx, ex.ID, false, 1); err != nil {
		t.Fatal(err)
	}
	if res, _ = policy.Sandbox(ctx, attack); res.Verdict != "block" {
		t.Fatalf("停用例外后应恢复拦截：%+v", res)
	}
	if err := policy.DeleteExclusion(ctx, ex.ID); err != nil {
		t.Fatal(err)
	}

	// ── 事件落库：拦截、观察、自定义规则 ──
	logs := service.NewFirewallLogService(zap.NewNop(), pg, nil, nil)
	now := time.Now().UTC()
	ruleID := 941110
	events := []firewalldomain.BlockEvent{
		{IP: "203.0.113.5", Method: "POST", Path: "/api/v1/apps/voyage_main/user/profile", Reason: "waf_blocked", HTTPStatus: 403, ResponseCode: 40396, WAFRuleID: &ruleID,
			Severity: "critical", BlockedAt: now, EventDetail: firewalldomain.EventDetail{Action: "block", Stage: "payload", Transport: "sealed", AppKey: "voyage_main", AnomalyScore: 10,
				Matches: []firewalldomain.MatchedRule{{ID: 941110, Message: "XSS Filter - Category 1", Group: "941", Severity: "critical"}, {ID: 942100, Message: "SQLi", Group: "942", Severity: "critical"}}}},
		{IP: "203.0.113.6", Method: "GET", Path: "/api/v1/apps/voyage_main/search", Reason: "waf_observed", Severity: "low", BlockedAt: now,
			EventDetail: firewalldomain.EventDetail{Action: "observe", Stage: "payload", Transport: "plain", AnomalyScore: 5, Matches: []firewalldomain.MatchedRule{{ID: 942100, Group: "942"}}}},
		{IP: "203.0.113.7", Method: "POST", Path: "/api/v1/apps/a/x", Reason: "custom_rule", HTTPStatus: 403, ResponseCode: 40395, Severity: "high", BlockedAt: now,
			EventDetail: firewalldomain.EventDetail{Action: "block", Stage: "edge", RuleRef: ruleA.ID, RuleName: ruleA.Name}},
	}
	for _, evt := range events {
		raw, _ := json.Marshal(evt)
		payload := map[string]any{}
		_ = json.Unmarshal(raw, &payload)
		if err := logs.HandleFirewallBlocked(ctx, payload); err != nil {
			t.Fatal(err)
		}
	}
	page, err := logs.ListLogs(ctx, firewalldomain.FirewallLogFilter{Action: "block", Page: 1, PageSize: 20})
	if err != nil || page.Total != 2 {
		t.Fatalf("拦截事件应为 2 条：%+v %v", page, err)
	}
	match := 942100
	page, err = logs.ListLogs(ctx, firewalldomain.FirewallLogFilter{WAFRuleID: &match, Page: 1, PageSize: 20})
	if err != nil || page.Total != 2 {
		t.Fatalf("按命中明细里的规则筛应找到 2 条：%+v %v", page, err)
	}
	page, err = logs.ListLogs(ctx, firewalldomain.FirewallLogFilter{Keyword: "拦 curl", Transport: "", Page: 1, PageSize: 20})
	if err != nil || page.Total != 1 || page.Items[0].RuleRef != ruleA.ID {
		t.Fatalf("按规则名关键字筛：%+v %v", page, err)
	}
	page, _ = logs.ListLogs(ctx, firewalldomain.FirewallLogFilter{Transport: "sealed", Page: 1, PageSize: 20})
	if page.Total != 1 || len(page.Items[0].Matches) != 2 || page.Items[0].AppKey != "voyage_main" {
		t.Fatalf("sealed 事件与命中明细：%+v", page)
	}
	detail, err := logs.GetLog(ctx, page.Items[0].ID)
	if err != nil || detail.AnomalyScore != 10 || detail.Stage != "payload" {
		t.Fatalf("详情：%+v %v", detail, err)
	}
	stats, err := logs.GetStats(ctx, now.Add(-time.Hour), now.Add(time.Hour), "minute")
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalBlocked != 2 || stats.TotalObserved != 1 || stats.UniqueIPs != 2 {
		t.Fatalf("统计应只数拦截：%+v", stats)
	}
	groups := map[string]int64{}
	for _, g := range stats.TopGroups {
		groups[g.Key] = g.Count
	}
	if groups["941"] != 1 || groups["942"] != 1 {
		t.Fatalf("规则组聚合（只数拦截）：%+v", stats.TopGroups)
	}
	if len(stats.TopCustomRules) != 1 || stats.TopCustomRules[0].Key != ruleA.Name {
		t.Fatalf("自定义规则排行：%+v", stats.TopCustomRules)
	}
	if n, _ := pg.CountRecentBlocks(ctx, "203.0.113.6", now.Add(-time.Hour)); n != 0 {
		t.Fatal("观察事件不能计入自动封禁")
	}

	// ── 命中数回写 ──
	if err := pg.AddFirewallRuleHits(ctx, map[int64]int64{ruleA.ID: 3}, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.GetFirewallRule(ctx, ruleA.ID); got.Hits != 3 || got.LastHitAt == nil {
		t.Fatalf("命中数：%+v", got)
	}

	// ── 设置分区 ──
	settings := service.NewPlatformSettingsService(config.Config{Firewall: base}, zap.NewNop(), pg, firewall, nil, nil, nil, nil)
	threshold, detect := 10, true
	empty := []string{}
	view, err := settings.UpdateFirewallSettings(ctx, nil, systemdomain.FirewallSettingsPatch{CorazaAnomalyThreshold: &threshold, CorazaDetectionOnly: &detect, CorazaRemovedRules: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if view.CorazaAnomalyThreshold != 10 || !view.CorazaDetectionOnly || len(view.CorazaRemovedRules) != 0 || view.Source != "database" {
		t.Fatalf("设置保存结果：%+v", view)
	}
	if rt := policy.Runtime(); !rt.DetectionOnly || rt.Threshold != 10 {
		t.Fatalf("设置应热更新：%+v", rt)
	}
	attack.DraftExclusion = nil
	if res, _ = policy.Sandbox(ctx, attack); res.Verdict != "observe" {
		t.Fatalf("观察模式下测试台应判 observe：%+v", res)
	}
	bad := "abc"
	if _, err := settings.UpdateFirewallSettings(ctx, nil, systemdomain.FirewallSettingsPatch{GlobalRate: &bad}); err == nil {
		t.Fatal("非法限流格式必须拒绝")
	}
}
