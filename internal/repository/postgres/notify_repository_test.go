package postgres

import (
	"slices"
	"testing"
)

// eventKeyMatchers 决定一个事件能命中哪些订阅写法。
// 早期实现只算最后一段，导致内置的 `ticket.*` 订阅匹配不到 `ticket.sla.warning`，
// SLA 预警/超时整类事件静默失踪 —— 这组用例专门守住多级 key。
func TestEventKeyMatchersCoversAllAncestors(t *testing.T) {
	cases := []struct {
		event string
		want  []string
	}{
		{
			event: "ticket.created",
			want:  []string{"ticket.created", "*", "ticket.*"},
		},
		{
			// 三段 key：必须同时命中 ticket.sla.* 与 ticket.*
			event: "ticket.sla.warning",
			want:  []string{"ticket.sla.warning", "*", "ticket.sla.*", "ticket.*"},
		},
		{
			event: "ticket.overdue.digest",
			want:  []string{"ticket.overdue.digest", "*", "ticket.overdue.*", "ticket.*"},
		},
		{
			// 无点号的 key 只匹配自身与全局通配
			event: "heartbeat",
			want:  []string{"heartbeat", "*"},
		},
	}

	for _, tc := range cases {
		got := eventKeyMatchers(tc.event)
		if len(got) != len(tc.want) {
			t.Fatalf("%s：期望 %v，实际 %v", tc.event, tc.want, got)
		}
		for _, want := range tc.want {
			if !slices.Contains(got, want) {
				t.Fatalf("%s：缺少匹配项 %q（实际 %v）", tc.event, want, got)
			}
		}
	}
}

// 内置订阅写的是 `ticket.*`，必须能兜住所有工单事件（含多级 key）。
func TestEventKeyMatchersHitsSeededWildcard(t *testing.T) {
	events := []string{
		"ticket.created", "ticket.replied", "ticket.agent_replied", "ticket.assigned",
		"ticket.status_changed", "ticket.resolved", "ticket.closed", "ticket.reopened",
		"ticket.escalated", "ticket.rated",
		"ticket.sla.warning", "ticket.sla.breached", "ticket.overdue.digest",
	}
	for _, event := range events {
		if !slices.Contains(eventKeyMatchers(event), "ticket.*") {
			t.Fatalf("%s 无法命中内置的 ticket.* 订阅", event)
		}
	}
}
