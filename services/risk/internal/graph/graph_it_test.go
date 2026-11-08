package graph

import (
	"os"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
)

// 需要本地 compose 的 risk profile：RISK_IT=1 go test ./services/risk/internal/graph -run IT
func TestGraphIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	cfg := config.Load()
	if err := Migrate(cfg, t.Logf); err != nil {
		t.Fatal(err)
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	now := time.Now()
	ts := now.UnixMilli()
	app := int64(990000 + now.Unix()%1000)
	evs := []events.Event{
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 1, Account: "alice", IP: "10.1.1.1", IPSubnet: "10.1.1.0/24", DeviceID: "dev-A", Device: map[string]string{"brand": "Xiaomi", "model": "14"}, Phone: "13800138000"},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 2, Account: "bob", IP: "10.1.1.2", IPSubnet: "10.1.1.0/24", DeviceID: "dev-A", Phone: "13800138000"},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 3, Account: "carol", IP: "10.1.1.1", DeviceID: "dev-B"},
		{Type: events.TypeAccountBan, TS: ts, AppID: app, UserID: 3},
		{Type: events.TypeInviteBind, TS: ts, AppID: app, UserID: 2, TargetUserID: 1},
		{Type: events.TypeInviteBind, TS: ts, AppID: app, UserID: 3, TargetUserID: 1},
	}
	if err := c.Apply(evs); err != nil {
		t.Fatal(err)
	}
	u1 := UserVID(app, 1)
	want := map[string]float64{
		bundle.GraphDeviceAccounts:  1, // bob 与 alice 同设备
		bundle.GraphIPAccounts:      1, // carol 与 alice 同 IP
		bundle.GraphPhoneAccounts:   1,
		bundle.GraphBannedNeighbors: 1, // carol 被封
		bundle.GraphInviteChildren:  2,
		bundle.GraphInviteTree:      2,
	}
	for tpl, n := range want {
		stmt, err := FeatureQuery(tpl, u1, app, 30, now)
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.Count(stmt)
		if err != nil {
			t.Fatalf("%s: %v\n%s", tpl, err, stmt)
		}
		if got != n {
			t.Errorf("%s = %v, want %v", tpl, got, n)
		}
	}
	stmt, _ := FeatureQuery(bundle.GraphDeviceUsers, DeviceVID("dev-A"), app, 30, now)
	if got, err := c.Count(stmt); err != nil || got != 2 {
		t.Errorf("device_users = %v %v", got, err)
	}
	g, err := c.Expand(u1, 2, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, n := range g.Nodes {
		kinds[n.Kind]++
	}
	if kinds[TagUser] < 3 || kinds[TagDevice] < 2 || kinds[TagIP] < 2 || len(g.Links) < 8 {
		t.Errorf("subgraph too small: %v links=%d", kinds, len(g.Links))
	}
	t.Logf("subgraph: %d nodes %v, %d links", len(g.Nodes), kinds, len(g.Links))
}
