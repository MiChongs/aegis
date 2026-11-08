package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"aegis/services/risk/internal/bundle"
)

// 决策表：分数 ≥ 80 或模型标记高危 → block；≥ 50 → captcha；其余 pass
const testFlow = `{
  "nodes": [
    {"id": "in", "type": "inputNode", "name": "Request", "position": {"x": 0, "y": 0}},
    {"id": "table", "type": "decisionTableNode", "name": "Decide", "position": {"x": 200, "y": 0},
     "content": {"hitPolicy": "first",
       "inputs": [{"id": "s", "name": "Score", "field": "score", "type": "expression"},
                  {"id": "h", "name": "Velocity", "field": "features.ip_login_10m", "type": "expression"}],
       "outputs": [{"id": "a", "name": "Action", "field": "action", "type": "expression"},
                   {"id": "r", "name": "Reason", "field": "reason", "type": "expression"}],
       "rules": [
         {"_id": "1", "s": ">= 80", "h": "", "a": "\"block\"", "r": "\"高分拦截\""},
         {"_id": "2", "s": ">= 50", "h": "", "a": "\"captcha\"", "r": "\"中分加验\""},
         {"_id": "3", "s": "", "h": "> 100", "a": "\"review\"", "r": "\"频次异常\""},
         {"_id": "4", "s": "", "h": "", "a": "\"pass\"", "r": "\"\""}
       ]}},
    {"id": "out", "type": "outputNode", "name": "Response", "position": {"x": 400, "y": 0}}
  ],
  "edges": [
    {"id": "e1", "sourceId": "in", "targetId": "table", "type": "edge"},
    {"id": "e2", "sourceId": "table", "targetId": "out", "type": "edge"}
  ]
}`

// 场景策略：老用户（user_id < 100）分数再高也只加验，不拦
const testPolicy = `package aegis.risk.scene

import rego.v1

decision := {"action": "captcha", "reasons": array.concat(input.proposed.reasons, ["老用户保护"])} if {
	input.proposed.action == "block"
	input.request.user_id > 0
	input.request.user_id < 100
}
`

func testBundle() bundle.Bundle {
	return bundle.Bundle{
		Version: "t1",
		Scene: bundle.Scene{Key: "login", Name: "登录", TimeoutMS: 200, FailMode: "open", CaptchaSupported: true,
			Enforcement: bundle.Enforcement{Targets: []string{"device", "account"}, DurationSeconds: 3600, AutoBanAllowed: true}},
		Features: []bundle.Feature{
			{Key: "ip_login_10m", Name: "IP 10 分钟登录次数", Kind: bundle.FeatureVelocity, Entity: bundle.EntityIP, Velocity: &bundle.VelocityDef{WindowSeconds: 600, Agg: bundle.AggCount}},
			{Key: "device_accounts_1h", Name: "设备 1 小时账号数", Kind: bundle.FeatureVelocity, Entity: bundle.EntityDevice, Velocity: &bundle.VelocityDef{WindowSeconds: 3600, Agg: bundle.AggDistinct, DistinctOf: bundle.EntityAccount}},
			{Key: "fail_24h", Name: "账号 24 小时失败次数", Kind: bundle.FeatureStream, Entity: bundle.EntityAccount, Stream: &bundle.StreamDef{Events: []string{"login.fail"}, Agg: bundle.AggCount, WindowSeconds: 86400}},
			{Key: "risky_net", Name: "高危网络", Kind: bundle.FeatureDerived, Expr: "net.proxy || net.tor || net.datacenter"},
		},
		Rules: []bundle.Rule{
			{Key: "ip_burst", Name: "IP 登录突增", Dimension: bundle.DimBehavior, Expr: "F.ip_login_10m > 20", Score: 40, Mode: bundle.ModeEnforce, Reason: "IP 10 分钟登录 {{F.ip_login_10m}} 次"},
			{Key: "device_farm", Name: "一机多号", Dimension: bundle.DimDevice, Expr: "F.device_accounts_1h >= 5", Score: 50, Mode: bundle.ModeEnforce},
			{Key: "risky_network", Name: "代理网络", Dimension: bundle.DimNetwork, Expr: "F.risky_net == 1", Score: 20, Mode: bundle.ModeEnforce},
			{Key: "emulator_ban", Name: "模拟器批量", Dimension: bundle.DimDevice, Expr: "device.emulator && F.device_accounts_1h >= 10", Score: 0, Mode: bundle.ModeEnforce, Decision: bundle.ActionBan},
			{Key: "observe_fail", Name: "失败多（观察）", Dimension: bundle.DimAccount, Expr: "F.fail_24h > 3", Score: 30, Mode: bundle.ModeObserve},
		},
		Flow:   json.RawMessage(testFlow),
		Policy: testPolicy,
	}
}

func compileTest(t *testing.T, b bundle.Bundle) *Program {
	t.Helper()
	p, err := Compile(context.Background(), b, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	t.Cleanup(p.Dispose)
	return p
}

func TestEvaluatePipeline(t *testing.T) {
	p := compileTest(t, testBundle())
	ctx := context.Background()

	cases := []struct {
		name     string
		req      Request
		features map[string]float64
		lists    []ListHit
		action   string
		hits     []string
		by       string
	}{
		{name: "clean", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "alice"}, features: map[string]float64{"ip_login_10m": 1}, action: "pass"},
		{name: "captcha band", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d1"}},
			features: map[string]float64{"ip_login_10m": 30, "device_accounts_1h": 1}, action: "pass", hits: []string{"ip_burst"}},
		{name: "captcha", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d1"}, Geo: Geo{Proxy: true}},
			features: map[string]float64{"ip_login_10m": 30}, action: "captcha", hits: []string{"ip_burst", "risky_network"}, by: "flow"},
		{name: "captcha passed", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "a", CaptchaPassed: true, Geo: Geo{Proxy: true}},
			features: map[string]float64{"ip_login_10m": 30}, action: "pass", hits: []string{"ip_burst", "risky_network"}},
		{name: "block", req: Request{Scene: "login", AppID: 1, UserID: 5000, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d1"}},
			features: map[string]float64{"ip_login_10m": 30, "device_accounts_1h": 6}, action: "block", hits: []string{"ip_burst", "device_farm"}},
		{name: "old user protected", req: Request{Scene: "login", AppID: 1, UserID: 7, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d1"}},
			features: map[string]float64{"ip_login_10m": 30, "device_accounts_1h": 6}, action: "captcha", by: "policy", hits: []string{"ip_burst", "device_farm"}},
		{name: "forced ban", req: Request{Scene: "login", AppID: 1, UserID: 5000, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d9", Emulator: true}},
			features: map[string]float64{"device_accounts_1h": 12}, action: "ban", hits: []string{"device_farm", "emulator_ban"}},
		{name: "allow list wins", req: Request{Scene: "login", AppID: 1, UserID: 5000, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d9", Emulator: true}},
			features: map[string]float64{"device_accounts_1h": 12}, lists: []ListHit{{ListType: "ip", Kind: "allow", Value: "1.2.3.4"}}, action: "pass", by: "list", hits: []string{"device_farm", "emulator_ban"}},
		{name: "block list", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4"}, lists: []ListHit{{ListType: "ip", Kind: "block", Value: "1.2.3.4"}}, action: "block", by: "list"},
		{name: "observe does not score", req: Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "a"}, features: map[string]float64{"fail_24h": 9}, action: "pass", hits: []string{"observe_fail"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := p.Evaluate(ctx, c.req, FixedSources{Features: c.features, Lists: c.lists}, Options{DecisionID: "d", Explain: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.Action != c.action {
				t.Fatalf("action = %s, want %s (score %d, reasons %v, degraded %v)", res.Action, c.action, res.Score, res.Reasons, res.Degraded)
			}
			var got []string
			for _, h := range res.Hits {
				got = append(got, h.Key)
			}
			sort.Strings(got)
			want := append([]string{}, c.hits...)
			sort.Strings(want)
			if len(got) != 0 || len(want) != 0 {
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("hits = %v, want %v", got, want)
				}
			}
			if c.by != "" && res.DecidedBy != c.by {
				t.Fatalf("decidedBy = %s, want %s", res.DecidedBy, c.by)
			}
			if len(res.Degraded) > 0 {
				t.Fatalf("unexpected degraded: %v", res.Degraded)
			}
			if res.Trace == nil || len(res.Trace.Stages) < 5 {
				t.Fatalf("trace missing stages")
			}
		})
	}
}

func TestBanCommands(t *testing.T) {
	p := compileTest(t, testBundle())
	res, err := p.Evaluate(context.Background(), Request{Scene: "login", AppID: 3, UserID: 5000, Account: "bob", IP: "9.9.9.9", Device: Device{ID: "dev", Emulator: true}},
		FixedSources{Features: map[string]float64{"device_accounts_1h": 12}}, Options{DecisionID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]string{}
	for _, c := range res.Commands {
		kinds[c.Kind+"/"+c.Target] = c.Value
		if c.AppID != 3 || !strings.HasPrefix(c.ID, "abc:") {
			t.Fatalf("bad command %+v", c)
		}
	}
	want := map[string]string{"list_add/device": "dev", "account_ban/user": "5000", "revoke_sessions/user": "5000"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("commands = %v, want %v", kinds, want)
	}

	// 场景不允许自动封禁：ban 降成 block，也不出命令
	b := testBundle()
	b.Scene.Enforcement.AutoBanAllowed = false
	p2 := compileTest(t, b)
	res, _ = p2.Evaluate(context.Background(), Request{Scene: "login", AppID: 3, UserID: 5000, Account: "bob", IP: "9.9.9.9", Device: Device{ID: "dev", Emulator: true}},
		FixedSources{Features: map[string]float64{"device_accounts_1h": 12}}, Options{DecisionID: "abc"})
	if res.Action != bundle.ActionBlock || len(res.Commands) != 0 {
		t.Fatalf("want block without commands, got %s %v", res.Action, res.Commands)
	}
}

func TestReasonTemplate(t *testing.T) {
	p := compileTest(t, testBundle())
	res, _ := p.Evaluate(context.Background(), Request{Scene: "login", AppID: 1, IP: "1.2.3.4"}, FixedSources{Features: map[string]float64{"ip_login_10m": 42}}, Options{})
	if len(res.Hits) == 0 || res.Hits[0].Reason != "IP 10 分钟登录 42 次" {
		t.Fatalf("reason = %+v", res.Hits)
	}
}

func TestCompileErrors(t *testing.T) {
	b := testBundle()
	b.Rules = append(b.Rules,
		bundle.Rule{Key: "typo", Name: "写错变量", Dimension: "custom", Expr: "ip_requst_count > 1", Mode: bundle.ModeEnforce},
		bundle.Rule{Key: "missing_feature", Name: "缺特征", Dimension: "custom", Expr: "F.nope > 1", Mode: bundle.ModeEnforce},
	)
	b.Policy = "package wrong"
	b.Flow = json.RawMessage(`{"nodes": 3}`)
	_, err := Compile(context.Background(), b, nil)
	ce, ok := err.(*CompileError)
	if !ok {
		t.Fatalf("want CompileError, got %v", err)
	}
	stages := map[string]bool{}
	for _, i := range ce.Issues {
		stages[i.Stage+"/"+i.Key] = true
	}
	for _, want := range []string{"rules/typo", "rules/missing_feature", "policy/login", "flow/login"} {
		if !stages[want] {
			t.Fatalf("missing issue %s in %v", want, ce.Issues)
		}
	}
}

func TestBandsWithoutFlow(t *testing.T) {
	b := testBundle()
	b.Flow = nil
	b.Policy = ""
	p := compileTest(t, b)
	res, _ := p.Evaluate(context.Background(), Request{Scene: "login", AppID: 1, IP: "1.2.3.4", Account: "c", Device: Device{ID: "x"}},
		FixedSources{Features: map[string]float64{"ip_login_10m": 30, "device_accounts_1h": 6}}, Options{})
	// 40 + 50 = 90 → 默认分数段 review
	if res.Action != bundle.ActionReview || res.Score != 90 {
		t.Fatalf("got %s score %d", res.Action, res.Score)
	}
}

func TestVariablesMatchEnv(t *testing.T) {
	names := map[string]bool{}
	for _, v := range Variables() {
		names[v.Name] = true
	}
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := prefix + f.Tag.Get("expr")
			if f.Type.Kind() == reflect.Struct {
				walk(name+".", f.Type)
				continue
			}
			if !names[name] {
				t.Errorf("变量目录缺 %s", name)
			}
			delete(names, name)
		}
	}
	walk("", reflect.TypeOf(Env{}))
	for n := range names {
		t.Errorf("变量目录多出 %s", n)
	}
}

func TestHelpers(t *testing.T) {
	if Subnet("1.2.3.4") != "1.2.3.0/24" || Subnet("2001:db8::1") != "2001:db8::/64" || Subnet("bad") != "" {
		t.Fatal("subnet")
	}
	if PhonePrefix("+86 138-0013-8000") != "1380013" || EmailDomain("A@QQ.com") != "qq.com" {
		t.Fatal("phone/email")
	}
	if !parseClient("Mozilla/5.0 HeadlessChrome/120").Headless || !parseClient("curl/8.0").Bot {
		t.Fatal("ua")
	}
}

func BenchmarkEvaluate(b *testing.B) {
	p, err := Compile(context.Background(), testBundle(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer p.Dispose()
	src := FixedSources{Features: map[string]float64{"ip_login_10m": 30, "device_accounts_1h": 6}}
	req := Request{Scene: "login", AppID: 1, UserID: 5000, IP: "1.2.3.4", Account: "a", Device: Device{ID: "d1"}, UserAgent: "Mozilla/5.0 (Linux; Android 14) Chrome/130.0 Mobile"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Evaluate(context.Background(), req, src, Options{DecisionID: "x"}); err != nil {
			b.Fatal(err)
		}
	}
}
