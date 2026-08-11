package service

import (
	"testing"
	"time"

	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
)

// SLA 计时是工单系统里最容易"看起来对、实际差几小时"的部分：
// 跨周末、跨零点、跨多天累加各来一组用例钉住行为。

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("时区数据不可用（%s），跳过：%v", name, err)
	}
	return loc
}

func TestAddBusinessMinutesWithoutBusinessHours(t *testing.T) {
	start := time.Date(2026, 8, 8, 22, 0, 0, 0, time.UTC) // 周六深夜
	got := addBusinessMinutes(start, 120, nil)
	want := start.Add(2 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("未配置工作时间时应按自然时间累加：got=%s want=%s", got, want)
	}
}

func TestAddBusinessMinutesSkipsWeekend(t *testing.T) {
	loc := mustLocation(t, "Asia/Shanghai")
	hours := &ticketdomain.BusinessHours{
		Timezone: "Asia/Shanghai",
		Days:     []int{1, 2, 3, 4, 5}, // 周一到周五
		Start:    "09:00",
		End:      "18:00",
	}
	// 2026-08-07 是周五。17:00 提单，剩 60 分钟工作时间，要 2 小时首响
	start := time.Date(2026, 8, 7, 17, 0, 0, 0, loc)
	got := addBusinessMinutes(start, 120, hours).In(loc)

	// 周五消耗 60 分钟 → 剩 60 分钟顺延到周一 09:00 起算 → 周一 10:00
	want := time.Date(2026, 8, 10, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("周末不应计入 SLA：got=%s want=%s", got, want)
	}
}

func TestAddBusinessMinutesStartsBeforeWorkday(t *testing.T) {
	loc := mustLocation(t, "Asia/Shanghai")
	hours := &ticketdomain.BusinessHours{
		Timezone: "Asia/Shanghai",
		Days:     []int{1, 2, 3, 4, 5},
		Start:    "09:00",
		End:      "18:00",
	}
	// 周一凌晨 03:00 提单，应从当天 09:00 开始计时
	start := time.Date(2026, 8, 10, 3, 0, 0, 0, loc)
	got := addBusinessMinutes(start, 30, hours).In(loc)
	want := time.Date(2026, 8, 10, 9, 30, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("上班前提单应从开工时刻起算：got=%s want=%s", got, want)
	}
}

func TestAddBusinessMinutesSpansMultipleDays(t *testing.T) {
	loc := mustLocation(t, "Asia/Shanghai")
	hours := &ticketdomain.BusinessHours{
		Timezone: "Asia/Shanghai",
		Days:     []int{1, 2, 3, 4, 5},
		Start:    "09:00",
		End:      "18:00", // 每天 540 分钟
	}
	// 周一 09:00 起算 1200 分钟：周一 540 + 周二 540 = 1080，剩 120 分 → 周三 11:00
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, loc)
	got := addBusinessMinutes(start, 1200, hours).In(loc)
	want := time.Date(2026, 8, 12, 11, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("跨多个工作日累加错误：got=%s want=%s", got, want)
	}
}

func TestEvaluateSLARowBreachAndWarning(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	created := now.Add(-90 * time.Minute)

	// 首响已过期
	overdue := now.Add(-10 * time.Minute)
	row := pgrepo.SLAScanRow{CreatedAt: created, FirstResponseDueAt: &overdue, WarnRatio: 0.8}
	state, reason, due := evaluateSLARow(row, now)
	if state != ticketdomain.SLABreached {
		t.Fatalf("首响超时应判定为 breached，got=%s (%s)", state, reason)
	}
	if due == nil || !due.Equal(overdue) {
		t.Fatalf("应回传首响时限")
	}

	// 已消耗 90/100 = 90% > 80%，应预警
	warnDue := created.Add(100 * time.Minute)
	row = pgrepo.SLAScanRow{CreatedAt: created, FirstResponseDueAt: &warnDue, WarnRatio: 0.8}
	if state, _, _ = evaluateSLARow(row, now); state != ticketdomain.SLAWarning {
		t.Fatalf("消耗超过 warnRatio 应预警，got=%s", state)
	}

	// 才消耗 90/1000 = 9%，不应有任何状态变化
	safeDue := created.Add(1000 * time.Minute)
	row = pgrepo.SLAScanRow{CreatedAt: created, FirstResponseDueAt: &safeDue, WarnRatio: 0.8}
	if state, _, _ = evaluateSLARow(row, now); state != "" {
		t.Fatalf("时限充裕时不应变更状态，got=%s", state)
	}
}

func TestEvaluateSLARowUsesResolveDueAfterFirstResponse(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	created := now.Add(-5 * time.Hour)
	responded := now.Add(-4 * time.Hour)
	// 首响时限早已过，但实际已经响应过了 → 不该再按首响判超时
	firstDue := now.Add(-4*time.Hour - 30*time.Minute)
	resolveDue := now.Add(2 * time.Hour)

	row := pgrepo.SLAScanRow{
		CreatedAt:          created,
		FirstResponseDueAt: &firstDue,
		FirstRespondedAt:   &responded,
		ResolveDueAt:       &resolveDue,
		WarnRatio:          0.8,
	}
	state, _, _ := evaluateSLARow(row, now)
	// 已消耗 5h / 7h ≈ 71% < 80%，尚未预警
	if state != "" {
		t.Fatalf("已首响后应改看解决时限，got=%s", state)
	}
}
