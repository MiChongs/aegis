package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	notifydomain "aegis/internal/domain/notify"
	ticketdomain "aegis/internal/domain/ticket"

	"go.uber.org/zap"
)

// 工单 → 统一通知出口的唯一桥梁。
//
// 业务代码只调 emitTicketEvent(ctx, ticket, 事件类型, 附加变量)，
// 这里负责把工单翻译成 notifydomain.Event（标题 / 摘要 / 结构化字段 / 跳转链接 / 收件人），
// 剩下的路由与投递全交给 NotifyHub。
//
// 想加一个"工单被合并"的通知？在这里加一个事件类型即可，
// 不需要碰任何 IM 相关代码，也不需要改渠道配置的数据结构。

// 工单侧事件类型（映射到 notifydomain 的事件 key）
const (
	ticketEventCreated       = notifydomain.EventTicketCreated
	ticketEventUserReplied   = notifydomain.EventTicketReplied
	ticketEventAgentReplied  = notifydomain.EventTicketAgentReplied
	ticketEventAssigned      = notifydomain.EventTicketAssigned
	ticketEventStatusChanged = notifydomain.EventTicketStatusChanged
	ticketEventResolved      = notifydomain.EventTicketResolved
	ticketEventClosed        = notifydomain.EventTicketClosed
	ticketEventReopened      = notifydomain.EventTicketReopened
	ticketEventEscalated     = notifydomain.EventTicketEscalated
	ticketEventSLAWarning    = notifydomain.EventTicketSLAWarning
	ticketEventSLABreached   = notifydomain.EventTicketSLABreached
	ticketEventRated         = notifydomain.EventTicketRated
)

// ticketActor 触发本次事件的操作者。用于把「自己操作产生的通知」从收件人里剔除。
type ticketActor struct {
	AdminID *int64
	UserID  *int64
}

// emitTicketEvent 把一次工单变更广播到统一通知出口（系统触发，无操作者）。
func (s *TicketService) emitTicketEvent(ctx context.Context, item *ticketdomain.Ticket, eventKey string, extraVars map[string]any) {
	s.emitTicketEventAs(ctx, item, eventKey, ticketActor{}, extraVars)
}

// emitTicketEventAs 带操作者身份广播。异步执行，不阻塞业务。
//
// 事件里同时携带两套受众视角：
//   - Link / Title / Summary        → 处理侧（控制台深链、含受理人等内部信息）
//   - UserLink / UserTitle / UserSummary → 提单人侧（应用内路径、不含内部归属）
//
// 由 provider 按收件人类型各取所需，避免把控制台路径推给 App 用户。
func (s *TicketService) emitTicketEventAs(ctx context.Context, item *ticketdomain.Ticket,
	eventKey string, actor ticketActor, extraVars map[string]any) {

	if s.notify == nil || item == nil {
		return
	}
	consoleLink := s.ticketConsoleLink(item.ID)
	event := notifydomain.Event{
		Key:         eventKey,
		AppID:       item.AppID,
		AppName:     item.AppName,
		Type:        "ticket",
		Level:       ticketEventLevel(eventKey, item.Priority),
		Title:       ticketEventTitle(eventKey, item),
		Summary:     ticketEventSummaryText(eventKey, item, extraVars),
		Link:        consoleLink,
		UserLink:    fmt.Sprintf("/tickets/%d", item.ID),
		UserTitle:   ticketUserTitle(eventKey, item),
		UserSummary: ticketUserSummary(eventKey, item, extraVars),
		Priority:    item.Priority,
		CategoryID:  item.CategoryID,
		Resource:    "ticket",
		ResourceID:  strconv.FormatInt(item.ID, 10),
		Fields:      ticketEventFields(item),
		Vars:        ticketEventVars(item, extraVars),
	}
	event.Actions = []notifydomain.Action{{Text: "处理工单", URL: consoleLink, Style: "primary"}}
	event.Recipients = s.ticketRecipients(ctx, item, eventKey, actor)
	// 同一工单同一事件在极短时间内重复触发时，靠 dedupe 键收敛为一次
	event.DedupeKey = fmt.Sprintf("ticket:%d:%s:%d", item.ID, eventKey, item.UpdatedAt.Unix())

	s.notify.DispatchAsync(event)
}

// ticketRecipients 解析"人"维度的收件目标。
//
//	提单人方向：处理进展（回复 / 解决 / 关闭）回到提单用户的站内信与实时推送
//	处理人方向：新单 / 用户追问 / SLA 告警落到受理人、关注人、处理组成员
//
// 操作者本人会被剔除：谁都不该因为自己点了「已解决」而收到一条"工单已解决"。
func (s *TicketService) ticketRecipients(ctx context.Context, item *ticketdomain.Ticket,
	eventKey string, actor ticketActor) notifydomain.Recipients {

	recipients := notifydomain.Recipients{}
	adminIDs := make([]int64, 0, 8)

	// ── 提单人方向 ──
	// 提单人可能是应用用户，也可能是管理员（平台内部工单就全是后者）。
	// 早期实现只看 RequesterUserID，于是管理员提的工单被回复后**没有任何人**收到通知。
	if ticketEventFacesRequester(eventKey) {
		switch {
		case item.RequesterUserID != nil:
			if actor.UserID == nil || *actor.UserID != *item.RequesterUserID {
				recipients.UserIDs = append(recipients.UserIDs, *item.RequesterUserID)
				if contact := strings.TrimSpace(item.RequesterContact); strings.Contains(contact, "@") {
					recipients.Emails = append(recipients.Emails, contact)
				}
			}
		case item.RequesterAdminID != nil:
			adminIDs = append(adminIDs, *item.RequesterAdminID)
		}
	}

	// ── 处理侧 ──
	if ticketEventFacesAgents(eventKey) {
		targets, err := s.pg.ListTicketNotifyTargets(ctx, item.ID)
		if err != nil {
			s.log.Debug("解析工单通知目标失败", zap.Int64("ticketId", item.ID), zap.Error(err))
		} else {
			adminIDs = append(adminIDs, targets...)
		}
		// 管理员提单人始终跟进自己的单
		if item.RequesterAdminID != nil {
			adminIDs = append(adminIDs, *item.RequesterAdminID)
		}
	}

	recipients.AdminIDs = excludeActorAdmin(adminIDs, actor.AdminID)

	// ── 兜底 ──
	// 未指派、无处理组、无关注人的工单，处理侧收件人会是空的 —— 新单尤其常见。
	// 这时通知超级管理员：没人认领的工单本来就该让超管看见，
	// 否则「工单系统装好了却一条通知都收不到」。
	if len(recipients.AdminIDs) == 0 && ticketEventNeedsFallback(eventKey) {
		supers, err := s.pg.ListSuperAdminIDs(ctx)
		if err != nil {
			s.log.Debug("回退超级管理员失败", zap.Int64("ticketId", item.ID), zap.Error(err))
		} else {
			recipients.AdminIDs = excludeActorAdmin(supers, actor.AdminID)
		}
	}
	return recipients
}

// ticketEventNeedsFallback 该事件在无人认领时是否需要兜底给超管。
// 只覆盖「必须有人看见」的事件，避免把每一次状态微调都推给超管。
func ticketEventNeedsFallback(eventKey string) bool {
	switch eventKey {
	case ticketEventCreated, ticketEventUserReplied, ticketEventEscalated,
		ticketEventSLAWarning, ticketEventSLABreached:
		return true
	}
	return false
}

// excludeActorAdmin 去重并剔除操作者本人。
func excludeActorAdmin(ids []int64, actorAdminID *int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if actorAdminID != nil && id == *actorAdminID {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// ticketEventFacesRequester 该事件是否需要告知提单人。
func ticketEventFacesRequester(eventKey string) bool {
	switch eventKey {
	case ticketEventAgentReplied, ticketEventResolved, ticketEventClosed, ticketEventStatusChanged, ticketEventReopened:
		return true
	}
	return false
}

// ticketEventFacesAgents 该事件是否需要告知处理侧。
//
// 全部事件都告知：回复人自己已由 ticketActor 排除，
// 其余处理人（关注人 / 同组成员 / 受理人）理应看到"同事已回复"这类进展。
func ticketEventFacesAgents(eventKey string) bool {
	return true
}

// ticketEventLevel 事件级别。紧急工单与 SLA 超时升级为 critical，
// 好让飞书卡片变红、并穿透静默窗口。
func ticketEventLevel(eventKey string, priority string) string {
	switch eventKey {
	case ticketEventSLABreached:
		return notifydomain.LevelCritical
	case ticketEventSLAWarning, ticketEventEscalated:
		return notifydomain.LevelWarning
	case ticketEventResolved, ticketEventClosed:
		return notifydomain.LevelSuccess
	}
	if priority == ticketdomain.PriorityUrgent {
		return notifydomain.LevelCritical
	}
	if priority == ticketdomain.PriorityHigh {
		return notifydomain.LevelWarning
	}
	return notifydomain.LevelInfo
}

// ticketEventTitle 通知标题。带上工单号，接收方一眼能对上单。
func ticketEventTitle(eventKey string, item *ticketdomain.Ticket) string {
	prefix := ""
	switch eventKey {
	case ticketEventCreated:
		prefix = "新工单"
	case ticketEventUserReplied:
		prefix = "用户追加回复"
	case ticketEventAgentReplied:
		prefix = "工单已回复"
	case ticketEventAssigned:
		prefix = "工单已指派"
	case ticketEventStatusChanged:
		prefix = "工单状态变更"
	case ticketEventResolved:
		prefix = "工单已解决"
	case ticketEventClosed:
		prefix = "工单已关闭"
	case ticketEventReopened:
		prefix = "工单重新打开"
	case ticketEventEscalated:
		prefix = "工单升级为紧急"
	case ticketEventSLAWarning:
		prefix = "SLA 预警"
	case ticketEventSLABreached:
		prefix = "SLA 超时"
	case ticketEventRated:
		prefix = "收到满意度评价"
	default:
		prefix = "工单动态"
	}
	return fmt.Sprintf("【%s】%s %s", prefix, item.TicketNo, item.Title)
}

// ticketEventSummaryText 通知正文。
func ticketEventSummaryText(eventKey string, item *ticketdomain.Ticket, vars map[string]any) string {
	switch eventKey {
	case ticketEventCreated:
		return fmt.Sprintf("%s 提交了新工单，当前优先级 %s。", item.RequesterName, priorityLabel(item.Priority))
	case ticketEventUserReplied:
		if excerptText := stringVar(vars, "replyExcerpt"); excerptText != "" {
			return fmt.Sprintf("%s 追加了回复：%s", item.RequesterName, excerptText)
		}
		return fmt.Sprintf("%s 追加了回复。", item.RequesterName)
	case ticketEventAgentReplied:
		if excerptText := stringVar(vars, "replyExcerpt"); excerptText != "" {
			return fmt.Sprintf("%s 回复了你的工单：%s", stringVar(vars, "replyBy"), excerptText)
		}
		return "客服已回复你的工单。"
	case ticketEventAssigned:
		target := item.AssigneeName
		if target == "" {
			target = item.GroupName
		}
		if target == "" {
			target = "（未指定）"
		}
		summary := fmt.Sprintf("工单已指派给 %s。", target)
		if reason := stringVar(vars, "reason"); reason != "" {
			summary += " 备注：" + reason
		}
		return summary
	case ticketEventStatusChanged:
		return fmt.Sprintf("工单状态由 %s 变更为 %s。", stringVar(vars, "from"), stringVar(vars, "to"))
	case ticketEventResolved:
		if solution := stringVar(vars, "solution"); solution != "" {
			return "工单已标记为已解决：" + solution
		}
		return "工单已标记为已解决，等待提单人确认。"
	case ticketEventClosed:
		if reason := stringVar(vars, "reason"); reason != "" {
			return "工单已关闭：" + reason
		}
		return "工单已关闭。"
	case ticketEventReopened:
		if reason := stringVar(vars, "reason"); reason != "" {
			return "工单被重新打开：" + reason
		}
		return "工单被重新打开，请及时跟进。"
	case ticketEventEscalated:
		return "工单优先级已升级为紧急，请优先处理。"
	case ticketEventSLAWarning, ticketEventSLABreached:
		summary := stringVar(vars, "slaReason")
		if summary == "" {
			summary = "SLA 状态发生变化"
		}
		if due := stringVar(vars, "slaDueAt"); due != "" {
			summary += "，时限：" + due
		}
		return summary + "。"
	case ticketEventRated:
		comment := stringVar(vars, "comment")
		rating := stringVar(vars, "rating")
		if comment != "" {
			return fmt.Sprintf("提单人给出 %s 星评价：%s", rating, comment)
		}
		return fmt.Sprintf("提单人给出 %s 星评价。", rating)
	}
	return item.Title
}

// ticketUserTitle 面向提单人的标题。不带内部术语（受理人 / 处理组 / SLA）。
func ticketUserTitle(eventKey string, item *ticketdomain.Ticket) string {
	prefix := ""
	switch eventKey {
	case ticketEventAgentReplied:
		prefix = "客服已回复"
	case ticketEventResolved:
		prefix = "问题已解决"
	case ticketEventClosed:
		prefix = "工单已关闭"
	case ticketEventReopened:
		prefix = "工单已重新打开"
	case ticketEventStatusChanged:
		prefix = "工单状态更新"
	default:
		return ""
	}
	return fmt.Sprintf("【%s】%s", prefix, item.Title)
}

// ticketUserSummary 面向提单人的正文。
func ticketUserSummary(eventKey string, item *ticketdomain.Ticket, vars map[string]any) string {
	switch eventKey {
	case ticketEventAgentReplied:
		if text := stringVar(vars, "replyExcerpt"); text != "" {
			return "客服回复：" + text
		}
		return "客服已回复你的工单，点击查看详情。"
	case ticketEventResolved:
		if solution := stringVar(vars, "solution"); solution != "" {
			return "处理结果：" + solution + "\n如问题仍未解决，可在工单内继续回复。"
		}
		return "你的问题已处理完成，如仍未解决可在工单内继续回复。"
	case ticketEventClosed:
		return "工单已关闭，感谢你的反馈。"
	case ticketEventReopened:
		return "工单已重新打开，我们会尽快跟进。"
	case ticketEventStatusChanged:
		return fmt.Sprintf("工单状态更新为「%s」。", statusLabel(item.Status))
	}
	return ""
}

// ticketEventFields 卡片上的结构化字段。
func ticketEventFields(item *ticketdomain.Ticket) []notifydomain.Field {
	fields := []notifydomain.Field{
		{Label: "工单号", Value: item.TicketNo, Short: true},
		{Label: "状态", Value: statusLabel(item.Status), Short: true},
		{Label: "优先级", Value: priorityLabel(item.Priority), Short: true},
		{Label: "提单人", Value: fallbackText(item.RequesterName, "匿名"), Short: true},
	}
	if item.CategoryName != "" {
		fields = append(fields, notifydomain.Field{Label: "分类", Value: item.CategoryName, Short: true})
	}
	if item.AssigneeName != "" {
		fields = append(fields, notifydomain.Field{Label: "受理人", Value: item.AssigneeName, Short: true})
	} else if item.GroupName != "" {
		fields = append(fields, notifydomain.Field{Label: "处理组", Value: item.GroupName, Short: true})
	}
	if item.AppName != "" {
		fields = append(fields, notifydomain.Field{Label: "所属应用", Value: item.AppName, Short: true})
	}
	if len(item.Tags) > 0 {
		fields = append(fields, notifydomain.Field{Label: "标签", Value: strings.Join(item.Tags, " / "), Short: false})
	}
	return fields
}

// ticketEventVars 模板变量，管理员可在通知模板里用 {{.TicketNo}} 之类引用。
func ticketEventVars(item *ticketdomain.Ticket, extra map[string]any) map[string]any {
	vars := map[string]any{
		"TicketID":     item.ID,
		"TicketNo":     item.TicketNo,
		"TicketTitle":  item.Title,
		"Status":       item.Status,
		"StatusLabel":  statusLabel(item.Status),
		"Priority":     item.Priority,
		"PriorityText": priorityLabel(item.Priority),
		"Requester":    item.RequesterName,
		"Assignee":     item.AssigneeName,
		"Group":        item.GroupName,
		"Category":     item.CategoryName,
		"AppName":      item.AppName,
		"CreatedAt":    item.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	for key, value := range extra {
		vars[key] = value
	}
	return vars
}

// ticketConsoleLink 拼出控制台工单详情地址。
func (s *TicketService) ticketConsoleLink(ticketID int64) string {
	base := ""
	if s.notify != nil {
		base = s.notify.consoleBaseURL
	}
	path := fmt.Sprintf("/tickets?id=%d", ticketID)
	if base == "" {
		return path
	}
	return base + path
}

func stringVar(vars map[string]any, key string) string {
	if vars == nil {
		return ""
	}
	value, ok := vars[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
