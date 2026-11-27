package itemgrant

import (
	"encoding/json"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
	"time"

	lotterydomain "aegis/internal/domain/lottery"
)

type fakeFacts struct {
	profile lotterydomain.BehaviorProfile
	user    map[string]int64
	global  map[string]int64
	loads   int
}

func (f *fakeFacts) Profile() (lotterydomain.BehaviorProfile, error) {
	f.loads++
	return f.profile, nil
}

func (f *fakeFacts) Usage(nodeID string, _ time.Time, _, _ bool) (int64, int64, error) {
	return f.user[nodeID], f.global[nodeID], nil
}

func mustNode(t *testing.T, id, nodeType string, data any) Node {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return Node{ID: id, Type: nodeType, Data: raw}
}

func edge(source, handle, target string) Edge {
	return Edge{ID: source + "-" + handle + "-" + target, Source: source, SourceHandle: handle, Target: target}
}

func subject(userID int64) Subject {
	return Subject{
		RuleID: 7, Trigger: TriggerManual, Now: time.Date(2026, 10, 28, 10, 30, 0, 0, time.UTC),
		Basics: Basics{UserID: userID, Eligible: true, RegisteredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Level: 3},
	}
}

func compileOK(t *testing.T, graph Graph) *Compiled {
	t.Helper()
	compiled, issues := Compile(NormalizeGraph(graph), nil)
	if compiled == nil {
		t.Fatalf("画布应能编译，问题：%+v", issues)
	}
	return compiled
}

func TestCompileRejectsBrokenGraphs(t *testing.T) {
	grant := GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 10}}}
	cases := map[string]struct {
		graph Graph
		want  string
	}{
		"没有触发器":  {Graph{Nodes: []Node{mustNode(t, "g", NodeGrant, grant)}}, "需要一个触发器"},
		"没有发放节点": {Graph{Nodes: []Node{mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual})}}, "没有任何"},
		"有环": {Graph{
			Nodes: []Node{
				mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}),
				mustNode(t, "a", NodeGrant, grant),
				mustNode(t, "b", NodeNotify, NotifyConfig{Title: "x"}),
			},
			Edges: []Edge{edge("t", HandleNext, "a"), edge("a", HandleNext, "b"), edge("b", HandleNext, "a")},
		}, "有环"},
		"端口不存在": {Graph{
			Nodes: []Node{mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}), mustNode(t, "g", NodeGrant, grant)},
			Edges: []Edge{edge("t", HandleYes, "g")},
		}, "没有端口"},
		"表达式错误": {Graph{
			Nodes: []Node{
				mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}),
				mustNode(t, "c", NodeCondition, ConditionConfig{Mode: MatchExpr, Expr: "user.nope > 1"}),
				mustNode(t, "g", NodeGrant, grant),
			},
			Edges: []Edge{edge("t", HandleNext, "c"), edge("c", HandleYes, "g")},
		}, "无法编译"},
		"物品越界": {Graph{
			Nodes: []Node{
				mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}),
				mustNode(t, "g", NodeGrant, GrantConfig{Items: []Item{{Type: ItemVipDays, Amount: 99999}}}),
			},
			Edges: []Edge{edge("t", HandleNext, "g")},
		}, "会员天数"},
	}
	for name, tc := range cases {
		compiled, issues := Compile(NormalizeGraph(tc.graph), nil)
		if compiled != nil {
			t.Errorf("%s：应当编译失败", name)
			continue
		}
		joined := ""
		for _, issue := range issues {
			joined += issue.Message + "；"
		}
		if !strings.Contains(joined, tc.want) {
			t.Errorf("%s：问题里应提到「%s」，实际：%s", name, tc.want, joined)
		}
	}
}

func TestEvaluateFollowsConditionAndLimit(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}),
			mustNode(t, "aud", NodeAudience, AudienceConfig{Scope: ScopeAll, MinLevel: 2}),
			mustNode(t, "cond", NodeCondition, ConditionConfig{Mode: MatchAll,
				Rules: []ConditionRule{{Field: "user.signStreak", Op: OpGte, Value: []float64{7}}}}),
			mustNode(t, "lim", NodeLimit, LimitConfig{Period: PeriodDay, PerUser: 1}),
			mustNode(t, "big", NodeGrant, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 10, AmountExpr: "user.signStreak * 1000000000"}}}),
			mustNode(t, "small", NodeGrant, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 5}}}),
			mustNode(t, "msg", NodeNotify, NotifyConfig{Title: "到账"}),
		},
		Edges: []Edge{
			edge("t", HandleNext, "aud"), edge("aud", HandleNext, "cond"),
			edge("cond", HandleYes, "lim"), edge("cond", HandleNo, "small"),
			edge("lim", HandlePass, "big"), edge("big", HandleNext, "msg"), edge("small", HandleNext, "msg"),
		},
	}
	compiled := compileOK(t, graph)
	if !compiled.NeedsProfile {
		t.Fatal("引用了 user.* 的画布应当需要行为画像")
	}
	if compiled.EntryAudience == nil || compiled.EntryAudience.MinLevel != 2 {
		t.Fatal("触发器后面直接连的目标人群应作为批量执行的候选人范围")
	}

	streaky := &fakeFacts{profile: lotterydomain.BehaviorProfile{SignStreak: 9}}
	outcome, err := compiled.Evaluate(subject(1), streaky, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcome.Items) != 1 || outcome.Items[0].NodeID != "big" {
		t.Fatalf("连签 9 天应走到 big，实际 %+v", outcome.Items)
	}
	if outcome.Items[0].Item.Amount != 100_000_000 {
		t.Errorf("数量表达式应截在上界 1 亿，实际 %d", outcome.Items[0].Item.Amount)
	}
	if len(outcome.Limits) != 1 || outcome.Limits[0] != "lim" || len(outcome.Notifies) != 1 {
		t.Errorf("应记下通过的限额节点并发一条站内信：%+v", outcome)
	}
	if streaky.loads != 1 {
		t.Errorf("行为画像一次判定只取一次，实际 %d 次", streaky.loads)
	}

	over := &fakeFacts{profile: lotterydomain.BehaviorProfile{SignStreak: 9}, user: map[string]int64{"lim": 1}}
	outcome, _ = compiled.Evaluate(subject(1), over, nil)
	if outcome.Granting() {
		t.Errorf("今天已发过一次，超出限额又没有连「超出」分支时什么都不发，实际 %+v", outcome.Items)
	}

	lazy := &fakeFacts{}
	outcome, _ = compiled.Evaluate(subject(1), lazy, nil)
	if len(outcome.Items) != 1 || outcome.Items[0].NodeID != "small" || outcome.Items[0].Item.Amount != 5 {
		t.Errorf("连签不足应走 small，实际 %+v", outcome.Items)
	}

	lowLevel := subject(1)
	lowLevel.Basics.Level = 1
	outcome, _ = compiled.Evaluate(lowLevel, &fakeFacts{}, nil)
	if outcome.Granting() || len(outcome.Steps) != 2 {
		t.Errorf("等级不够应停在目标人群，实际 %+v", outcome.Steps)
	}

	banned := subject(1)
	banned.Basics.Eligible = false
	outcome, _ = compiled.Evaluate(banned, &fakeFacts{}, nil)
	if outcome.Blocked == "" || outcome.Granting() {
		t.Error("账号不可用的人不进入画布")
	}
}

func TestSplitIsStickyAndRandomPicksWithoutReplacement(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerManual}),
			mustNode(t, "s", NodeSplit, SplitConfig{Sticky: true, Branches: []SplitBranch{{ID: "a", Weight: 1}, {ID: "b", Weight: 1}}}),
			mustNode(t, "ga", NodeGrant, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 1}}}),
			mustNode(t, "r", NodeRandom, RandomConfig{Picks: 2, Options: []RandomOption{
				{ID: "x", Weight: 1, Items: []Item{{Type: ItemExperience, Amount: 1}}},
				{ID: "y", Weight: 1, Items: []Item{{Type: ItemLotteryDraws, Amount: 1}}},
				{ID: "z", Weight: 1, Items: []Item{{Type: ItemAdFree, Amount: 1}}},
			}}),
		},
		Edges: []Edge{edge("t", HandleNext, "s"), edge("s", "a", "ga"), edge("s", "b", "r")},
	}
	compiled := compileOK(t, graph)
	counts := map[string]int{}
	rng := rand.New(rand.NewPCG(1, 2))
	for user := int64(1); user <= 2000; user++ {
		first, _ := compiled.Evaluate(subject(user), &fakeFacts{}, rng)
		second, _ := compiled.Evaluate(subject(user), &fakeFacts{}, rng)
		if first.Steps[1].Handle != second.Steps[1].Handle {
			t.Fatalf("固定分配时同一个人两次应进同一分支（用户 %d）", user)
		}
		counts[first.Steps[1].Handle]++
		if first.Steps[1].Handle == "b" {
			seen := map[string]bool{}
			for _, item := range first.Items {
				if seen[item.OptionID] {
					t.Fatalf("随机礼包应不放回抽取，重复抽到 %s", item.OptionID)
				}
				seen[item.OptionID] = true
			}
			if len(seen) != 2 {
				t.Fatalf("应抽出 2 个选项，实际 %d", len(seen))
			}
		}
	}
	if counts["a"] < 850 || counts["b"] < 850 {
		t.Errorf("等权分流应大致对半，实际 %v", counts)
	}
}

func TestWindowAndPeriods(t *testing.T) {
	// 2026-10-28 是周三
	now := time.Date(2026, 10, 28, 23, 30, 0, 0, time.UTC)
	cases := []struct {
		cfg  WindowConfig
		want bool
	}{
		{WindowConfig{}, true},
		{WindowConfig{Weekdays: []int{3}}, true},
		{WindowConfig{Weekdays: []int{6, 7}}, false},
		{WindowConfig{StartMinute: 22 * 60, EndMinute: 60}, true},
		{WindowConfig{StartMinute: 9 * 60, EndMinute: 18 * 60}, false},
		{WindowConfig{DateFrom: "2026-10-29"}, false},
		{WindowConfig{DateTo: "2026-10-28"}, true},
	}
	for i, tc := range cases {
		if got := InWindow(tc.cfg, now); got != tc.want {
			t.Errorf("第 %d 个窗口：期望 %v，得到 %v", i, tc.want, got)
		}
	}
	if start := PeriodStart(PeriodWeek, now); start.Weekday() != time.Monday || start.Day() != 26 {
		t.Errorf("本周应从周一 26 号开始，得到 %v", start)
	}
	if start := PeriodStart(PeriodMonth, now); start.Day() != 1 {
		t.Errorf("本月应从 1 号开始，得到 %v", start)
	}
	if !PeriodStart(PeriodTotal, now).IsZero() {
		t.Error("累计没有起点")
	}
}

func TestRulesExprAndTemplates(t *testing.T) {
	got := RulesExpr(MatchAny, []ConditionRule{
		{Field: "user.level", Op: OpBetween, Value: []float64{3, 5}},
		{Field: "event.amount", Op: OpGt, Value: []float64{9.9}},
		{Field: "user.member", Op: OpTrue},
	})
	want := "(user.level >= 3 && user.level <= 5) || (event.amount ?? 0) > 9.9 || user.member == true"
	if got != want {
		t.Errorf("表达式拼接：\n得到 %s\n期望 %s", got, want)
	}
	for _, template := range append(Templates(), Template{Key: "starter", Graph: StarterGraph()}) {
		if _, issues := Compile(NormalizeGraph(template.Graph), nil); HasErrors(issues) {
			t.Errorf("模板 %s 编译失败：%+v", template.Key, issues)
		}
	}
}

func TestEventFieldsDefaultToZero(t *testing.T) {
	graph := Graph{
		Nodes: []Node{
			mustNode(t, "t", NodeTrigger, TriggerConfig{Kind: TriggerEvent, Event: EventPayment}),
			mustNode(t, "c", NodeCondition, ConditionConfig{Mode: MatchAll,
				Rules: []ConditionRule{{Field: "event.amount", Op: OpGte, Value: []float64{30}}}}),
			mustNode(t, "g", NodeGrant, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 1, AmountExpr: "(event.amount ?? 0) * 10"}}}),
		},
		Edges: []Edge{edge("t", HandleNext, "c"), edge("c", HandleYes, "g")},
	}
	compiled := compileOK(t, graph)
	paid := subject(1)
	paid.Event = map[string]any{"amount": 68.5}
	outcome, _ := compiled.Evaluate(paid, &fakeFacts{}, nil)
	if len(outcome.Items) != 1 || outcome.Items[0].Item.Amount != 685 {
		t.Fatalf("支付 68.5 元应发 685 积分，实际 %+v", outcome.Items)
	}
	outcome, _ = compiled.Evaluate(subject(1), &fakeFacts{}, nil)
	if outcome.Granting() {
		t.Error("没有事件数据时 event.amount 按 0 处理，不满足条件")
	}
}

// 控制台把目录里的数组字段直接当数组读（.length / .map）：任何一处 nil 切片序列化成 null 都会让画布编辑器崩溃。
func TestCatalogHasNoNullArrays(t *testing.T) {
	payload := map[string]any{
		"items": ItemCatalog(), "itemGroups": ItemGroups(), "nodes": NodeCatalog(), "categories": NodeCategories(),
		"triggers": TriggerCatalog(), "events": EventCatalog(), "fields": FieldCatalog(), "operators": OperatorCatalog(),
		"scopes": Scopes(), "periods": Periods(), "routes": NotifyRoutes(), "templates": Templates(), "starter": StarterGraph(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range regexp.MustCompile(`.{0,80}:null`).FindAll(raw, -1) {
		t.Errorf("目录里有 null 字段：…%s", match)
	}
}

func TestMatchAudienceTiers(t *testing.T) {
	now := time.Now()
	b := Basics{UserID: 1, RegisteredAt: now.Add(-time.Hour), Lifetime: true, TierIDs: []int64{2, 7}, LifetimeTierIDs: []int64{7}}
	if ok, _ := MatchAudience(AudienceConfig{Scope: ScopeLifetime, TierIDs: []int64{2}}, b, now); !ok {
		t.Fatal("holds tier 2 (timed) should match without lifetimeOnly")
	}
	if ok, _ := MatchAudience(AudienceConfig{Scope: ScopeLifetime, TierIDs: []int64{2}, TierLifetimeOnly: true}, b, now); ok {
		t.Fatal("tier 2 is not held for life")
	}
	if ok, _ := MatchAudience(AudienceConfig{Scope: ScopeAll, TierIDs: []int64{3, 7}, TierLifetimeOnly: true}, b, now); !ok {
		t.Fatal("tier 7 is held for life")
	}
}
