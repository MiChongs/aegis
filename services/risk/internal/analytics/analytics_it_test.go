package analytics

import (
	"context"
	"os"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/config"
)

func TestAnalyticsIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	cfg := config.Load()
	if cfg.ClickHousePassword == "" {
		cfg.ClickHousePassword = "aegis-risk"
	}
	c, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if err := c.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id := events.NewID()
	e := events.Event{ID: id, Type: events.TypeDecision, Scene: "login", TS: time.Now().UnixMilli(), AppID: 42, UserID: 7, IP: "1.2.3.4",
		Action: "block", Score: 90, Hits: []string{"ip_burst"}, Features: map[string]float64{"x": 1}, Trace: `{"a":1}`}
	// 同一条写两次（模拟重放），FINAL 后只剩一条
	if err := c.WriteDecisions(ctx, []events.Event{e, e}); err != nil {
		t.Fatal(err)
	}
	var n uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM decisions FINAL WHERE id = ?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("dedupe: %d rows", n)
	}
	if err := c.WriteEvents(ctx, []events.Event{{ID: events.NewID(), Type: events.TypeLoginFail, TS: e.TS, AppID: 42, UserID: 7}}); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteAlerts(ctx, []Alert{{ID: events.NewID(), TS: e.TS, AppID: 42, Pattern: "p", Entity: "user", Value: "7", EventIDs: []string{id}}}); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteCommands(ctx, []CommandRecord{{Command: events.Command{ID: id + ":x", Kind: "list_add", TS: e.TS, AppID: 42}, Status: "ok"}}); err != nil {
		t.Fatal(err)
	}
}
