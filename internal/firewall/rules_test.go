package firewall

import (
	"net/netip"
	"testing"
)

func TestCustomRulesEvaluateInPriorityOrder(t *testing.T) {
	set, errs := NewRuleSet([]CustomRule{
		{ID: 1, Name: "log admin", Priority: 10, Action: ActionLog, Expression: `path startsWith "/api/admin/"`},
		{ID: 2, Name: "office", Priority: 20, Action: ActionAllow, Expression: `cidr(ip, "10.0.0.0/8", "192.168.1.7")`},
		{ID: 3, Name: "curl", Priority: 30, Action: ActionBlock, Expression: `ua matches "(?i)^curl/" && method == "POST"`},
		{ID: 4, Name: "geo", Priority: 40, Action: ActionBlock, Expression: `country in ["KP", "IR"] && hour < 6`},
	})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if !set.NeedGeo() {
		t.Fatal("rule 4 references country")
	}
	env := &RuleEnv{IP: "10.2.3.4", Path: "/api/admin/users", Method: "POST", UA: "curl/8.0"}
	terminal, logged := set.Evaluate(env)
	if terminal == nil || terminal.ID != 2 || len(logged) != 1 || logged[0].ID != 1 {
		t.Fatalf("unexpected: terminal=%v logged=%v", terminal, logged)
	}
	env.IP = "203.0.113.1"
	if terminal, _ := set.Evaluate(env); terminal == nil || terminal.ID != 3 {
		t.Fatalf("curl rule should block: %v", terminal)
	}
	if set.Rules()[0].TakeHits() != 2 {
		t.Fatal("hit counter")
	}
}

func TestCompileRuleRejectsBadInput(t *testing.T) {
	for _, rule := range []CustomRule{
		{Action: ActionBlock, Expression: ""},
		{Action: "drop", Expression: `ip == "1.1.1.1"`},
		{Action: ActionBlock, Expression: `ip ==`},
		{Action: ActionBlock, Expression: `path + 1`},
		{Action: ActionBlock, Expression: `unknownVar == 1`},
	} {
		if _, err := CompileRule(rule); err == nil {
			t.Errorf("expected error for %+v", rule)
		}
	}
}

func TestIPSet(t *testing.T) {
	set, err := ParseIPSet([]string{"10.0.0.0/8", "2001:db8::/32", "1.2.3.4", " "})
	if err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"10.9.9.9": true, "::ffff:10.1.1.1": true, "2001:db8::1": true, "1.2.3.4": true, "1.2.3.5": false} {
		if got := set.Contains(netip.MustParseAddr(ip)); got != want {
			t.Errorf("%s: got %v", ip, got)
		}
	}
	if _, err := ParseIPSet([]string{"10.0.0.0/33"}); err == nil {
		t.Fatal("bad prefix must fail")
	}
}
