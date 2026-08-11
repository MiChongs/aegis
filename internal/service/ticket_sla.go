package service

import (
	"context"
	"strings"
	"time"

	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// SLA 计时与巡检。
//
// 时限来自分类绑定的 SLA 策略：按优先级取「首次响应分钟数」「解决分钟数」，
// 从工单创建时刻起算。配置了 business_hours 时只在工作时段内累计，
// 也就是周五 18:00 提的工单不会在周末被判超时。
//
// 巡检器由 worker 定时调用 RunSLAScan：接近时限 → sla.warning，越过时限 → sla.breached，
// 两者都通过 NotifyHub 出口告警，不在这里直接对接任何 IM。

// computeSLA 计算工单的首响 / 解决时限。分类未绑定策略时回落到平台默认策略。
func (s *TicketService) computeSLA(ctx context.Context, category *ticketdomain.Category, priority string, from time.Time) (*ticketdomain.SLAPolicy, *time.Time, *time.Time) {
	var policy *ticketdomain.SLAPolicy
	if category != nil && category.SLAPolicyID != nil {
		found, err := s.pg.GetTicketSLAPolicy(ctx, *category.SLAPolicyID)
		if err == nil && found != nil && found.Enabled {
			policy = found
		}
	}
	if policy == nil {
		appID := int64(0)
		if category != nil {
			appID = category.AppID
		}
		policies, err := s.pg.ListTicketSLAPolicies(ctx, appID)
		if err == nil {
			for i := range policies {
				if policies[i].Enabled {
					policy = &policies[i]
					break
				}
			}
		}
	}
	if policy == nil {
		return nil, nil, nil
	}

	firstMinutes := policy.FirstResponseMinutes[priority]
	resolveMinutes := policy.ResolveMinutes[priority]
	var firstDue, resolveDue *time.Time
	if firstMinutes > 0 {
		due := addBusinessMinutes(from, firstMinutes, policy.BusinessHours)
		firstDue = &due
	}
	if resolveMinutes > 0 {
		due := addBusinessMinutes(from, resolveMinutes, policy.BusinessHours)
		resolveDue = &due
	}
	return policy, firstDue, resolveDue
}

// recomputeSLA 优先级变更 / 工单重开后重新计时。
func (s *TicketService) recomputeSLA(ctx context.Context, ticketID int64, categoryID *int64, priority string, from time.Time) {
	var category *ticketdomain.Category
	if categoryID != nil && *categoryID > 0 {
		found, err := s.pg.GetTicketCategory(ctx, *categoryID)
		if err == nil {
			category = found
		}
	}
	policy, firstDue, resolveDue := s.computeSLA(ctx, category, priority, from)
	var policyID *int64
	if policy != nil {
		policyID = &policy.ID
	}
	if err := s.pg.UpdateTicketSLAWindow(ctx, ticketID, policyID, firstDue, resolveDue); err != nil {
		s.log.Warn("重算工单 SLA 失败", zap.Int64("ticketId", ticketID), zap.Error(err))
	}
}

// addBusinessMinutes 在工作时间窗口内累加分钟数。hours 为 nil 时按自然时间累加。
func addBusinessMinutes(from time.Time, minutes int, hours *ticketdomain.BusinessHours) time.Time {
	if hours == nil || strings.TrimSpace(hours.Start) == "" || strings.TrimSpace(hours.End) == "" {
		return from.Add(time.Duration(minutes) * time.Minute)
	}
	loc := timeutil.DefaultLocation()
	if tz := strings.TrimSpace(hours.Timezone); tz != "" {
		if parsed, err := time.LoadLocation(tz); err == nil {
			loc = parsed
		}
	}
	startMinute, okStart := parseClock(hours.Start)
	endMinute, okEnd := parseClock(hours.End)
	if !okStart || !okEnd || endMinute <= startMinute {
		return from.Add(time.Duration(minutes) * time.Minute)
	}
	workdays := make(map[time.Weekday]bool, 7)
	if len(hours.Days) == 0 {
		for day := time.Sunday; day <= time.Saturday; day++ {
			workdays[day] = true
		}
	} else {
		for _, day := range hours.Days {
			// 配置用 1=周一 … 7=周日，转成 Go 的 Weekday
			switch {
			case day >= 1 && day <= 6:
				workdays[time.Weekday(day)] = true
			case day == 7:
				workdays[time.Sunday] = true
			}
		}
	}

	current := from.In(loc)
	remaining := minutes
	// 最多推进 400 天，避免配置错误（例如工作日全为空）导致死循环
	for guard := 0; guard < 400*24*60 && remaining > 0; guard++ {
		if !workdays[current.Weekday()] {
			current = startOfDay(current, loc).AddDate(0, 0, 1)
			continue
		}
		dayStart := startOfDay(current, loc).Add(time.Duration(startMinute) * time.Minute)
		dayEnd := startOfDay(current, loc).Add(time.Duration(endMinute) * time.Minute)
		if current.Before(dayStart) {
			current = dayStart
		}
		if !current.Before(dayEnd) {
			current = startOfDay(current, loc).AddDate(0, 0, 1)
			continue
		}
		available := int(dayEnd.Sub(current).Minutes())
		if available >= remaining {
			return current.Add(time.Duration(remaining) * time.Minute)
		}
		remaining -= available
		current = startOfDay(current, loc).AddDate(0, 0, 1)
	}
	return current
}

func startOfDay(value time.Time, loc *time.Location) time.Time {
	local := value.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// RunSLAScan 巡检一批工单，写回 SLA 状态并发出预警 / 超时通知。
// 返回 (预警数, 超时数)。由 worker 定时（建议 1-5 分钟）调用。
func (s *TicketService) RunSLAScan(ctx context.Context, limit int) (int, int, error) {
	rows, err := s.pg.ScanTicketsForSLA(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	now := timeutil.Now()
	warned, breached := 0, 0

	for _, row := range rows {
		state, reason, due := evaluateSLARow(row, now)
		if state == "" || state == row.SLAState {
			continue
		}
		if err := s.pg.SetTicketSLAState(ctx, row.ID, state); err != nil {
			s.log.Warn("写回工单 SLA 状态失败", zap.Int64("ticketId", row.ID), zap.Error(err))
			continue
		}

		item, err := s.pg.GetTicketByID(ctx, row.ID)
		if err != nil || item == nil {
			continue
		}
		eventName := ticketdomain.EventSLAWarning
		notifyEvent := ticketEventSLAWarning
		if state == ticketdomain.SLABreached {
			eventName = ticketdomain.EventSLABreached
			notifyEvent = ticketEventSLABreached
			breached++
		} else {
			warned++
		}
		_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
			TicketID: row.ID, Event: eventName, ActorType: "system", ActorName: "SLA 巡检",
			ToValue: state, Summary: reason,
		})
		vars := map[string]any{"slaReason": reason}
		if due != nil {
			vars["slaDueAt"] = due.In(s.location).Format("2006-01-02 15:04:05")
		}
		s.emitTicketEvent(ctx, item, notifyEvent, vars)
	}
	return warned, breached, nil
}

// evaluateSLARow 判定单条工单当前应处于的 SLA 状态。
// 返回 (状态, 人类可读原因, 相关时限)。状态为空表示无需变更。
func evaluateSLARow(row pgrepo.SLAScanRow, now time.Time) (string, string, *time.Time) {
	warnRatio := row.WarnRatio
	if warnRatio <= 0 || warnRatio >= 1 {
		warnRatio = 0.8
	}

	// 首响未完成时优先看首响时限
	if row.FirstRespondedAt == nil && row.FirstResponseDueAt != nil {
		due := *row.FirstResponseDueAt
		if now.After(due) {
			return ticketdomain.SLABreached, "首次响应已超时", &due
		}
		if reachedWarnPoint(row.CreatedAt, due, now, warnRatio) {
			return ticketdomain.SLAWarning, "即将超出首次响应时限", &due
		}
	}
	if row.ResolveDueAt != nil {
		due := *row.ResolveDueAt
		if now.After(due) {
			return ticketdomain.SLABreached, "解决时限已超时", &due
		}
		if reachedWarnPoint(row.CreatedAt, due, now, warnRatio) {
			return ticketdomain.SLAWarning, "即将超出解决时限", &due
		}
	}
	return "", "", nil
}

// reachedWarnPoint 是否已消耗掉 warnRatio 比例的时限。
func reachedWarnPoint(start, due, now time.Time, ratio float64) bool {
	total := due.Sub(start)
	if total <= 0 {
		return false
	}
	elapsed := now.Sub(start)
	return float64(elapsed) >= float64(total)*ratio
}
