package service

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	storagedomain "aegis/internal/domain/storage"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/gabriel-vasile/mimetype"
	"go.uber.org/zap"
)

// TicketService 工单业务逻辑。
//
// 职责边界：
//   - 状态机与字段校验在这里，Repository 只管存取
//   - 权限判定委托给 ticket_access.go（Scope / ActionSet）
//   - 所有对外通知只走 NotifyHub 一个出口（ticket_notify.go），
//     绝不在业务分支里直接调飞书 / 邮件
type TicketService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	admin    *AdminService
	notify   *NotifyHub
	storage  *StorageService
	location *time.Location

	// 撤回回复时清理它生成的通知（ticket_retract.go）；未接时只撤回消息本身
	userInbox  ticketUserInboxCleaner
	adminInbox ticketAdminInboxCleaner

	// 反馈奖励的站内信（feedback_reward_service.go）；未接时只发积分、不发通知
	userNotifier ticketUserNotifier
}

// SetUserNotifier 接上应用用户站内信（NotificationService），反馈奖励到账与撤回时通知对方。
func (s *TicketService) SetUserNotifier(notifier ticketUserNotifier) {
	s.userNotifier = notifier
}

const (
	ticketMaxAttachmentSize = 20 << 20 // 20 MB
	ticketStoragePrefix     = "storage://"
	ticketMaxTitleLen       = 200
	ticketMaxContentLen     = 20000
)

// NewTicketService 构造工单服务。
func NewTicketService(log *zap.Logger, pg *pgrepo.Repository, admin *AdminService, notify *NotifyHub, storage *StorageService) *TicketService {
	if log == nil {
		log = zap.NewNop()
	}
	return &TicketService{log: log, pg: pg, admin: admin, notify: notify, storage: storage, location: timeutil.DefaultLocation()}
}

// ─────────────── 查询 ───────────────

// List 管理端列表。可见范围由会话推导，前端无法越权拉取。
func (s *TicketService) List(ctx context.Context, access *admindomain.AccessContext, query ticketdomain.ListQuery) (*ticketdomain.ListResponse, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	normalizeTicketQuery(&query)
	items, total, err := s.pg.ListTickets(ctx, query, scope)
	if err != nil {
		return nil, err
	}
	return &ticketdomain.ListResponse{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: totalPages(total, query.Limit),
		Scope:      ScopeInfo(scope),
	}, nil
}

// Detail 工单详情：会话 + 时间线 + 附件 + 关注人 + 可执行动作。
// 无 ticket:internal 权限且与工单无关的人看不到内部备注。
func (s *TicketService) Detail(ctx context.Context, access *admindomain.AccessContext, ticketID int64, baseURL string) (*ticketdomain.Ticket, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	// 已撤回的消息照常列出（标为已撤回），原文是否露出由 presentMessagesForAdmin 决定
	messages, err := s.pg.ListTicketMessages(ctx, ticketID, actions.ViewInternal, true)
	if err != nil {
		return nil, err
	}
	events, err := s.pg.ListTicketEvents(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	attachments, err := s.pg.ListTicketAttachments(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	watchers, err := s.pg.ListTicketWatchers(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if !actions.ViewInternal {
		attachments = dropRetractedAttachments(messages, attachments)
	}
	s.resolveAttachmentURLs(ctx, baseURL, attachments)
	attachMessageFiles(messages, attachments)
	presentMessagesForAdmin(messages, actions, access.AdminID)

	item.Messages = messages
	item.Events = events
	item.Attachments = attachments
	item.Watchers = watchers
	item.Permissions = actions
	if item.Kind == ticketdomain.KindFeedback {
		// 奖励区块读不出来不影响看反馈本身
		if info, err := s.feedbackRewardInfo(ctx, item); err != nil {
			s.log.Warn("读取反馈奖励失败", zap.Int64("ticketId", ticketID), zap.Error(err))
		} else {
			item.Reward = info
		}
	}
	return item, nil
}

// permAppUserRead 应用用户查看权限点（细分后的「用户：查看」；旧的 app:user:read 经兼容关系同样满足）。
const permAppUserRead = authz.PermAppUserInfoRead

// RequesterContext 提单人资料卡的取数前提：能看到这条工单才往下走。
//
// canViewUser 表示调用者对工单所属应用有用户查看权限。处理组成员只凭「被指派」
// 能看到工单，但账号资料（邮箱、手机号、登录地址）属于用户模块，仍按那边的权限点放行。
func (s *TicketService) RequesterContext(ctx context.Context, access *admindomain.AccessContext, ticketID int64) (
	item *ticketdomain.Ticket, canViewUser bool, stats ticketdomain.RequesterTicketStats, err error) {

	item, _, _, err = s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, false, stats, err
	}
	if item.RequesterType == ticketdomain.RequesterUser && item.RequesterUserID != nil {
		appID := item.AppID
		canViewUser = s.can(ctx, access, permAppUserRead, &appID)
		stats, err = s.pg.CountRequesterTickets(ctx, item.AppID, *item.RequesterUserID)
		if err != nil {
			return nil, false, stats, err
		}
	}
	return item, canViewUser, stats, nil
}

// AdminAvatarRefs 一批管理员的头像引用，供 Handler 层解析成出网地址。
func (s *TicketService) AdminAvatarRefs(ctx context.Context, adminIDs []int64) (map[int64]ticketdomain.AvatarRef, error) {
	return s.pg.ListAdminAvatarRefs(ctx, adminIDs)
}

// Stats 工单概览统计。kind 为空表示工单与反馈合计。
func (s *TicketService) Stats(ctx context.Context, access *admindomain.AccessContext, appID *int64, kind string) (*ticketdomain.Stats, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	return s.pg.TicketStats(ctx, appID, normalizeTicketKind(kind), scope)
}

// Trend 工单趋势。
func (s *TicketService) Trend(ctx context.Context, access *admindomain.AccessContext, appID *int64, kind string, days int) ([]ticketdomain.TrendPoint, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	return s.pg.TicketTrend(ctx, appID, normalizeTicketKind(kind), scope, days)
}

// AgentStats 处理人绩效榜。
func (s *TicketService) AgentStats(ctx context.Context, access *admindomain.AccessContext, appID *int64, kind string, limit int) ([]ticketdomain.AgentStat, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	return s.pg.TicketAgentStats(ctx, appID, normalizeTicketKind(kind), scope, limit)
}

// Export 导出（受范围约束）。
func (s *TicketService) Export(ctx context.Context, access *admindomain.AccessContext, query ticketdomain.ListQuery, limit int) ([]ticketdomain.Ticket, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	normalizeTicketQuery(&query)
	return s.pg.ListTicketsForExport(ctx, query, scope, limit)
}

// AppSummaries 按应用汇总可见范围内的计数。排序：我创建的应用在前，其次未结多的、最近有动静的。
func (s *TicketService) AppSummaries(ctx context.Context, access *admindomain.AccessContext, kind string) ([]ticketdomain.AppSummary, error) {
	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, err
	}
	items, err := s.pg.TicketAppSummaries(ctx, normalizeTicketKind(kind), scope)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].AppID == 0 && items[i].AppName == "" {
			items[i].AppName = "平台内部"
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Owned != b.Owned {
			return a.Owned
		}
		if a.Active != b.Active {
			return a.Active > b.Active
		}
		switch {
		case a.LastAt != nil && b.LastAt != nil && !a.LastAt.Equal(*b.LastAt):
			return a.LastAt.After(*b.LastAt)
		case (a.LastAt == nil) != (b.LastAt == nil):
			return a.LastAt != nil
		}
		return a.AppID < b.AppID
	})
	return items, nil
}

// MyWorkbenchCount 我的待办数量（侧边栏角标）。
func (s *TicketService) MyWorkbenchCount(ctx context.Context, adminID int64) (int64, error) {
	return s.pg.CountAdminOpenTickets(ctx, adminID)
}

func normalizeTicketQuery(query *ticketdomain.ListQuery) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 200 {
		query.Limit = 200
	}
	query.Statuses = filterValues(query.Statuses, ticketdomain.ValidStatuses)
	query.Priorities = filterValues(query.Priorities, ticketdomain.ValidPriorities)
	query.Kind = normalizeTicketKind(query.Kind)
}

// normalizeTicketKind 不认识的类型按「不区分」处理，与状态筛选对非法值的口径一致。
func normalizeTicketKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if !ticketdomain.ValidKind(kind) {
		return ""
	}
	return kind
}

// normalizeUserContentType 用户端只能发纯文本或 Markdown。html 会被控制台按富文本渲染，
// 放开等于让任何用户往客服的浏览器里塞标记。
func normalizeUserContentType(contentType string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(contentType)) {
	case "", "text":
		return "text", nil
	case "markdown":
		return "markdown", nil
	}
	return "", apperrors.New(40000, http.StatusBadRequest, "不支持的内容格式")
}

func filterValues(values []string, allowed map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.ToLower(value))
		if _, ok := allowed[trimmed]; ok {
			out = append(out, trimmed)
		}
	}
	return out
}

// ─────────────── 建单 ───────────────

// CreateByAdmin 管理员建单（代客提单或平台内部工单）。
func (s *TicketService) CreateByAdmin(ctx context.Context, access *admindomain.AccessContext, cmd ticketdomain.CreateCommand) (*ticketdomain.Ticket, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	scoped := &cmd.AppID
	if cmd.AppID <= 0 {
		scoped = nil
	}
	if !s.can(ctx, access, PermTicketWrite, scoped) {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权创建工单")
	}
	if strings.TrimSpace(cmd.RequesterType) == "" {
		cmd.RequesterType = ticketdomain.RequesterAdmin
	}
	if cmd.RequesterType == ticketdomain.RequesterAdmin && cmd.RequesterAdminID == nil {
		adminID := access.AdminID
		cmd.RequesterAdminID = &adminID
		if strings.TrimSpace(cmd.RequesterName) == "" {
			cmd.RequesterName = adminDisplayName(access)
		}
	}
	adminID := access.AdminID
	cmd.CreatedByAdminID = &adminID
	if strings.TrimSpace(cmd.Source) == "" {
		cmd.Source = ticketdomain.SourceConsole
	}
	return s.create(ctx, cmd)
}

// create 建单公共路径：校验 → 应用分类默认值 → 计算 SLA → 落库 → 发通知。
func (s *TicketService) create(ctx context.Context, cmd ticketdomain.CreateCommand) (*ticketdomain.Ticket, error) {
	cmd.Title = strings.TrimSpace(cmd.Title)
	cmd.Content = strings.TrimSpace(cmd.Content)
	if cmd.Title == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "工单标题不能为空")
	}
	if len([]rune(cmd.Title)) > ticketMaxTitleLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "工单标题过长")
	}
	if cmd.Content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "工单内容不能为空")
	}
	if len([]rune(cmd.Content)) > ticketMaxContentLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "工单内容过长")
	}
	cmd.RequesterName = strings.TrimSpace(cmd.RequesterName)
	if cmd.RequesterName == "" {
		cmd.RequesterName = "匿名用户"
	}
	cmd.Tags = normalizeTags(cmd.Tags)

	// 分类默认值：优先级 / 处理组 / SLA
	var category *ticketdomain.Category
	if cmd.CategoryID != nil && *cmd.CategoryID > 0 {
		found, err := s.pg.GetTicketCategory(ctx, *cmd.CategoryID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, apperrors.New(40461, http.StatusNotFound, "工单分类不存在")
		}
		category = found
	}
	// 类型跟着分类走：反馈分类下的单就是反馈，避免出现「反馈分类里的普通工单」
	if category != nil && ticketdomain.ValidKind(category.Kind) {
		cmd.Kind = category.Kind
	}
	if !ticketdomain.ValidKind(cmd.Kind) {
		cmd.Kind = ticketdomain.KindTicket
	}
	// 工单是管理员之间的内部系统，提单人只能是管理员；应用用户的诉求一律走意见反馈，
	// 管理员代用户登记也建反馈，用户才能在反馈入口看到并跟进。
	if cmd.Kind == ticketdomain.KindTicket && cmd.RequesterType != ticketdomain.RequesterAdmin {
		return nil, apperrors.New(40000, http.StatusBadRequest, "工单只对管理员开放，代用户登记请改建意见反馈")
	}
	if strings.TrimSpace(cmd.Priority) == "" {
		if category != nil {
			cmd.Priority = category.DefaultPriority
		} else {
			cmd.Priority = ticketdomain.PriorityNormal
		}
	}
	if _, ok := ticketdomain.ValidPriorities[cmd.Priority]; !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的工单优先级")
	}
	if cmd.GroupID == nil && category != nil && category.DefaultGroupID != nil {
		cmd.GroupID = category.DefaultGroupID
	}

	// 处理组配置了自动分派策略时直接落到人，减少"无人认领"
	if cmd.AssigneeAdminID == nil && cmd.GroupID != nil {
		if picked, err := s.pg.PickTicketGroupAgent(ctx, *cmd.GroupID); err == nil && picked != nil {
			cmd.AssigneeAdminID = picked
		}
	}

	policy, firstDue, resolveDue := s.computeSLA(ctx, category, cmd.Priority, timeutil.Now())
	var policyID *int64
	if policy != nil {
		policyID = &policy.ID
	}

	ticketNo, err := s.nextTicketNo(ctx)
	if err != nil {
		return nil, err
	}
	item, err := s.pg.CreateTicket(ctx, cmd, ticketNo, firstDue, resolveDue, policyID)
	if err != nil {
		return nil, mapTicketAttachmentError(err)
	}
	// 建单人本身不必收到"新工单"提醒——管理员代客提单时尤其明显
	s.emitTicketEventAs(ctx, item, ticketEventCreated,
		ticketActor{AdminID: cmd.CreatedByAdminID, UserID: cmd.RequesterUserID}, nil)
	return item, nil
}

// nextTicketNo 生成工单号：TK + yyyyMMdd + 6 位流水（序列超过 6 位时自然增长）。
func (s *TicketService) nextTicketNo(ctx context.Context) (string, error) {
	seq, err := s.pg.NextTicketSequence(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("TK%s%06d", time.Now().In(s.location).Format("20060102"), seq), nil
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" || len([]rune(trimmed)) > 32 {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func adminDisplayName(access *admindomain.AccessContext) string {
	if access == nil {
		return ""
	}
	if strings.TrimSpace(access.DisplayName) != "" {
		return access.DisplayName
	}
	return access.Account
}

// ─────────────── 回复 ───────────────

// ReplyByAdmin 管理员回复（对外回复或内部备注）。
func (s *TicketService) ReplyByAdmin(ctx context.Context, access *admindomain.AccessContext, cmd ticketdomain.ReplyCommand) (*ticketdomain.Message, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, cmd.TicketID)
	if err != nil {
		return nil, err
	}
	if cmd.Internal {
		if !actions.InternalNote {
			return nil, apperrors.New(40311, http.StatusForbidden, "无权发表内部备注")
		}
	} else if !actions.Reply {
		if item.Locked || ticketdomain.IsTerminal(item.Status) {
			return nil, apperrors.New(40316, http.StatusForbidden, "工单已关闭，无法继续回复")
		}
		return nil, apperrors.New(40311, http.StatusForbidden, "无权回复该工单")
	}

	content := strings.TrimSpace(cmd.Content)
	if content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容不能为空")
	}
	if len([]rune(content)) > ticketMaxContentLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容过长")
	}
	next := strings.ToLower(strings.TrimSpace(cmd.NextStatus))
	if cmd.Internal {
		next = "" // 内部备注不改变对外状态
	}
	if next != "" {
		if _, ok := ticketdomain.ValidStatuses[next]; !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "无效的工单状态")
		}
		if !actions.ChangeStatus {
			return nil, apperrors.New(40311, http.StatusForbidden, "无权变更工单状态")
		}
	}
	// 进行中的状态随消息一起落库；有结论的状态（已解决 / 不采纳 / 已关闭）要记时间、算 SLA、发通知，
	// 回复落库后走 ChangeStatus。早先一律随消息落库，「回复并标为已解决」不记解决时间也不通知。
	inlineStatus, finishStatus := next, ""
	if ticketdomain.IsFinished(next) {
		inlineStatus, finishStatus = "", next
	}

	adminID := access.AdminID
	message, err := s.pg.AddTicketMessage(ctx, pgrepo.AddTicketMessageInput{
		TicketID:      cmd.TicketID,
		AuthorType:    ticketdomain.AuthorAgent,
		AuthorAdminID: &adminID,
		AuthorName:    adminDisplayName(access),
		Internal:      cmd.Internal,
		Content:       content,
		ContentType:   cmd.ContentType,
		Metadata:      cmd.Metadata,
		AttachmentIDs: cmd.AttachmentIDs,
		NextStatus:    inlineStatus,
	})
	if err != nil {
		return nil, mapTicketAttachmentError(err)
	}

	eventKey := ticketdomain.EventReplied
	if cmd.Internal {
		eventKey = ticketdomain.EventInternalNote
	}
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: cmd.TicketID, Event: eventKey, ActorType: "admin", ActorID: &adminID,
		ActorName: adminDisplayName(access), Summary: ticketEventSummary(eventKey, adminDisplayName(access)),
	})

	// 内部备注不外发，避免把内部讨论推给提单人
	if !cmd.Internal {
		refreshed, _ := s.pg.GetTicketByID(ctx, cmd.TicketID)
		if refreshed != nil {
			s.emitTicketEventAs(ctx, refreshed, ticketEventAgentReplied, ticketActor{AdminID: &adminID}, map[string]any{
				"replyBy":      adminDisplayName(access),
				"replyExcerpt": excerpt(content, 200),
				// 写进站内信与收件箱的元数据，撤回这条回复时按它找到对应通知
				"messageId": message.ID,
			})
		}
	}
	if finishStatus != "" {
		if _, err := s.ChangeStatus(ctx, access, cmd.TicketID, ticketdomain.StatusCommand{Status: finishStatus}); err != nil {
			return nil, err
		}
	}
	return message, nil
}

// replyByRequester 反馈人追加回复。应用用户只有意见反馈一个入口，工单不对用户开放。
func (s *TicketService) replyByRequester(ctx context.Context, session *authdomain.Session, cmd ticketdomain.ReplyCommand) (*ticketdomain.Message, error) {
	item, err := s.requireUserFeedback(ctx, session, cmd.TicketID)
	if err != nil {
		return nil, err
	}
	if item.Locked || ticketdomain.IsTerminal(item.Status) {
		return nil, apperrors.New(40316, http.StatusForbidden, "反馈已结束，无法继续回复")
	}
	content := strings.TrimSpace(cmd.Content)
	if content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容不能为空")
	}
	if len([]rune(content)) > ticketMaxContentLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容过长")
	}
	contentType, err := normalizeUserContentType(cmd.ContentType)
	if err != nil {
		return nil, err
	}

	userID := session.UserID
	message, err := s.pg.AddTicketMessage(ctx, pgrepo.AddTicketMessageInput{
		TicketID:        cmd.TicketID,
		AuthorType:      ticketdomain.AuthorRequester,
		AuthorUserID:    &userID,
		AuthorName:      item.RequesterName,
		Internal:        false, // 用户永远发不出内部备注
		Content:         content,
		ContentType:     contentType,
		AttachmentIDs:   cmd.AttachmentIDs,
		AttachmentOwner: &ticketdomain.AttachmentOwner{Type: "user", ID: userID},
	})
	if err != nil {
		return nil, mapTicketAttachmentError(err)
	}
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: cmd.TicketID, Event: ticketdomain.EventReplied, ActorType: "user", ActorID: &userID,
		ActorName: item.RequesterName, Summary: "提单人追加了回复",
	})
	refreshed, _ := s.pg.GetTicketByID(ctx, cmd.TicketID)
	if refreshed != nil {
		s.emitTicketEventAs(ctx, refreshed, ticketEventUserReplied, ticketActor{UserID: &userID}, map[string]any{
			"replyExcerpt": excerpt(content, 200),
		})
	}
	return message, nil
}

func excerpt(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	return string(runes[:max]) + "…"
}

// ─────────────── 变更 ───────────────

// Update 修改工单基础字段。
func (s *TicketService) Update(ctx context.Context, access *admindomain.AccessContext, ticketID int64, cmd ticketdomain.UpdateCommand) (*ticketdomain.Ticket, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	if !actions.Edit {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权修改该工单")
	}
	if cmd.Priority != nil {
		normalized := strings.TrimSpace(strings.ToLower(*cmd.Priority))
		if _, ok := ticketdomain.ValidPriorities[normalized]; !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "无效的工单优先级")
		}
		cmd.Priority = &normalized
	}
	if cmd.Tags != nil {
		normalized := normalizeTags(*cmd.Tags)
		cmd.Tags = &normalized
	}
	if err := s.pg.UpdateTicketFields(ctx, ticketID, cmd); err != nil {
		return nil, err
	}

	adminID := access.AdminID
	actorName := adminDisplayName(access)
	if cmd.Priority != nil && *cmd.Priority != item.Priority {
		_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
			TicketID: ticketID, Event: ticketdomain.EventPriorityChanged, ActorType: "admin", ActorID: &adminID,
			ActorName: actorName, FromValue: item.Priority, ToValue: *cmd.Priority,
			Summary: fmt.Sprintf("优先级 %s → %s", priorityLabel(item.Priority), priorityLabel(*cmd.Priority)),
		})
		// 优先级变了，SLA 时限要跟着重算，否则「升级为紧急」形同虚设
		s.recomputeSLA(ctx, ticketID, item.CategoryID, *cmd.Priority, item.CreatedAt)
	}
	if cmd.CategoryID != nil && !equalInt64Ptr(cmd.CategoryID, item.CategoryID) {
		_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
			TicketID: ticketID, Event: ticketdomain.EventCategoryChanged, ActorType: "admin", ActorID: &adminID,
			ActorName: actorName, Summary: "调整了工单分类",
		})
	}
	if cmd.Tags != nil {
		_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
			TicketID: ticketID, Event: ticketdomain.EventTagsChanged, ActorType: "admin", ActorID: &adminID,
			ActorName: actorName, ToValue: strings.Join(*cmd.Tags, ","), Summary: "更新了标签",
		})
	}

	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if cmd.Priority != nil && *cmd.Priority == ticketdomain.PriorityUrgent && item.Priority != ticketdomain.PriorityUrgent {
		s.emitTicketEventAs(ctx, refreshed, ticketEventEscalated, ticketActor{AdminID: &adminID}, nil)
	}
	return refreshed, nil
}

// Assign 指派工单到人或处理组。
func (s *TicketService) Assign(ctx context.Context, access *admindomain.AccessContext, ticketID int64, cmd ticketdomain.AssignCommand) (*ticketdomain.Ticket, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	if !actions.Assign {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权指派该工单")
	}

	assignee := cmd.AssigneeAdminID
	groupID := cmd.GroupID
	if cmd.AutoPick && groupID != nil {
		picked, err := s.pg.PickTicketGroupAgent(ctx, *groupID)
		if err != nil {
			return nil, err
		}
		if picked == nil {
			return nil, apperrors.New(40462, http.StatusNotFound, "处理组内没有可用成员")
		}
		assignee = picked
	}
	if assignee != nil && *assignee <= 0 {
		assignee = nil
	}
	if groupID != nil && *groupID <= 0 {
		groupID = nil
	}
	if err := s.pg.AssignTicket(ctx, ticketID, assignee, groupID); err != nil {
		return nil, err
	}

	adminID := access.AdminID
	summary := "取消了指派"
	if assignee != nil {
		summary = "指派了处理人"
	} else if groupID != nil {
		summary = "指派到处理组"
	}
	if reason := strings.TrimSpace(cmd.Reason); reason != "" {
		summary += "：" + reason
	}
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: ticketID, Event: ticketdomain.EventAssigned, ActorType: "admin", ActorID: &adminID,
		ActorName: adminDisplayName(access),
		FromValue: int64PtrString(item.AssigneeAdminID), ToValue: int64PtrString(assignee),
		Summary: summary,
	})

	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	s.emitTicketEventAs(ctx, refreshed, ticketEventAssigned, ticketActor{AdminID: &adminID}, map[string]any{
		"assignedBy": adminDisplayName(access),
		"reason":     strings.TrimSpace(cmd.Reason),
	})
	return refreshed, nil
}

// ChangeStatus 状态流转。resolved 时可带解决方案，作为一条对外消息落库。
func (s *TicketService) ChangeStatus(ctx context.Context, access *admindomain.AccessContext, ticketID int64, cmd ticketdomain.StatusCommand) (*ticketdomain.Ticket, error) {
	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	next := strings.TrimSpace(strings.ToLower(cmd.Status))
	if _, ok := ticketdomain.ValidStatuses[next]; !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的工单状态")
	}
	if next == item.Status {
		if cmd.Reward != nil {
			// 状态没变，只发奖励
			if _, err := s.grantFeedbackReward(ctx, access, item, actions, *cmd.Reward); err != nil {
				return nil, err
			}
			return s.ticketWithReward(ctx, ticketID)
		}
		return item, nil
	}
	// 已有结论（已解决 / 不采纳 / 已关闭 / 已撤销）改回进行中的状态才算重开
	reopen := ticketdomain.IsFinished(item.Status) && !ticketdomain.IsFinished(next)
	if reopen {
		if !actions.Reopen {
			return nil, apperrors.New(40311, http.StatusForbidden, "无权重开该工单")
		}
	} else if !actions.ChangeStatus {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权变更工单状态")
	}

	// 同时发放反馈奖励：先发奖励（判定多、涉及积分），失败则状态不变；发成功之后改状态再失败，奖励照样有效
	if cmd.Reward != nil {
		if !ticketdomain.RewardableStatusTarget(next) {
			return nil, apperrors.New(errCodeFeedbackRewardIneligible, http.StatusUnprocessableEntity,
				"改为「"+ticketdomain.StatusLabel(next)+"」时不能同时发放奖励")
		}
		if _, err := s.grantFeedbackReward(ctx, access, item, actions, *cmd.Reward); err != nil {
			return nil, err
		}
	}

	adminID := access.AdminID
	actorName := adminDisplayName(access)

	// 解决方案先落成对外消息，提单人在会话里看得到
	if solution := strings.TrimSpace(cmd.Solution); solution != "" {
		if _, err := s.pg.AddTicketMessage(ctx, pgrepo.AddTicketMessageInput{
			TicketID: ticketID, AuthorType: ticketdomain.AuthorAgent, AuthorAdminID: &adminID,
			AuthorName: actorName, Content: solution, ContentType: "text",
		}); err != nil {
			return nil, err
		}
	}
	if err := s.pg.UpdateTicketStatus(ctx, ticketID, next, reopen); err != nil {
		return nil, err
	}

	eventKey := ticketdomain.EventStatusChanged
	if reopen {
		eventKey = ticketdomain.EventReopened
	}
	summary := fmt.Sprintf("状态 %s → %s", ticketdomain.StatusLabel(item.Status), ticketdomain.StatusLabel(next))
	if reason := strings.TrimSpace(cmd.Reason); reason != "" {
		summary += "：" + reason
	}
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: ticketID, Event: eventKey, ActorType: "admin", ActorID: &adminID,
		ActorName: actorName, FromValue: item.Status, ToValue: next, Summary: summary,
	})

	// 重开后重新计时，否则老的 due 会让工单立刻又"超时"
	if reopen {
		s.recomputeSLA(ctx, ticketID, item.CategoryID, item.Priority, timeutil.Now())
	}

	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	switch {
	case reopen:
		s.emitTicketEventAs(ctx, refreshed, ticketEventReopened, ticketActor{AdminID: &adminID}, map[string]any{"reason": cmd.Reason})
	case next == ticketdomain.StatusResolved:
		s.emitTicketEventAs(ctx, refreshed, ticketEventResolved, ticketActor{AdminID: &adminID}, map[string]any{"solution": excerpt(cmd.Solution, 200)})
	case ticketdomain.IsTerminal(next):
		s.emitTicketEventAs(ctx, refreshed, ticketEventClosed, ticketActor{AdminID: &adminID}, map[string]any{"reason": cmd.Reason})
	default:
		s.emitTicketEventAs(ctx, refreshed, ticketEventStatusChanged, ticketActor{AdminID: &adminID}, map[string]any{
			"from": ticketdomain.StatusLabel(item.Status), "to": ticketdomain.StatusLabel(next),
		})
	}
	if cmd.Reward != nil && refreshed != nil {
		refreshed.Reward, _ = s.feedbackRewardInfo(ctx, refreshed)
	}
	return refreshed, nil
}

// ticketWithReward 重新读一条并带上奖励区块（改状态时只发了奖励的返回值）。
func (s *TicketService) ticketWithReward(ctx context.Context, ticketID int64) (*ticketdomain.Ticket, error) {
	item, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil || item == nil {
		return item, err
	}
	item.Reward, _ = s.feedbackRewardInfo(ctx, item)
	return item, nil
}

// Delete 删除工单。
func (s *TicketService) Delete(ctx context.Context, access *admindomain.AccessContext, ticketID int64) error {
	_, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return err
	}
	if !actions.Delete {
		return apperrors.New(40311, http.StatusForbidden, "无权删除该工单")
	}
	ok, err := s.pg.DeleteTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40460, http.StatusNotFound, "工单不存在")
	}
	return nil
}

// Bulk 批量操作。逐条走单条路径，保证权限判定一致；失败项单独返回。
func (s *TicketService) Bulk(ctx context.Context, access *admindomain.AccessContext, cmd ticketdomain.BulkCommand) (*ticketdomain.BulkResult, error) {
	if len(cmd.IDs) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要操作的工单")
	}
	if len(cmd.IDs) > 200 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "单次最多操作 200 条工单")
	}
	result := &ticketdomain.BulkResult{Requested: len(cmd.IDs), Action: cmd.Action}
	for _, id := range cmd.IDs {
		var err error
		switch strings.TrimSpace(cmd.Action) {
		case "assign":
			_, err = s.Assign(ctx, access, id, ticketdomain.AssignCommand{
				AssigneeAdminID: cmd.AssigneeAdminID, GroupID: cmd.GroupID, Reason: cmd.Reason,
			})
		case "status", "close":
			status := cmd.Status
			if strings.TrimSpace(cmd.Action) == "close" {
				status = ticketdomain.StatusClosed
			}
			_, err = s.ChangeStatus(ctx, access, id, ticketdomain.StatusCommand{Status: status, Reason: cmd.Reason})
		case "priority":
			priority := cmd.Priority
			_, err = s.Update(ctx, access, id, ticketdomain.UpdateCommand{Priority: &priority})
		case "tag":
			tags := cmd.Tags
			_, err = s.Update(ctx, access, id, ticketdomain.UpdateCommand{Tags: &tags})
		case "delete":
			err = s.Delete(ctx, access, id)
		default:
			return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的批量操作")
		}
		if err != nil {
			result.Failed = append(result.Failed, ticketdomain.BulkFailure{ID: id, Reason: errorMessage(err)})
			continue
		}
		result.Succeeded++
	}
	return result, nil
}

func errorMessage(err error) string {
	if appErr, ok := err.(*apperrors.AppError); ok {
		return appErr.Message
	}
	return err.Error()
}

// ─────────────── 关注 / 评价 ───────────────

// Watch 关注或取消关注工单。
func (s *TicketService) Watch(ctx context.Context, access *admindomain.AccessContext, ticketID int64, watch bool) error {
	_, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return err
	}
	if !actions.Watch {
		return apperrors.New(40311, http.StatusForbidden, "无权关注该工单")
	}
	if watch {
		return s.pg.AddTicketWatcher(ctx, ticketID, access.AdminID)
	}
	return s.pg.RemoveTicketWatcher(ctx, ticketID, access.AdminID)
}

// SetWatchers 管理关注人（需要指派权限）。
func (s *TicketService) SetWatchers(ctx context.Context, access *admindomain.AccessContext, ticketID int64, adminIDs []int64) ([]ticketdomain.Watcher, error) {
	_, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	if !actions.ManageWatchers {
		return nil, apperrors.New(40311, http.StatusForbidden, "无权管理关注人")
	}
	existing, err := s.pg.ListTicketWatchers(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	target := make(map[int64]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		if id > 0 {
			target[id] = struct{}{}
		}
	}
	for _, watcher := range existing {
		if _, keep := target[watcher.AdminID]; !keep {
			if err := s.pg.RemoveTicketWatcher(ctx, ticketID, watcher.AdminID); err != nil {
				return nil, err
			}
		}
		delete(target, watcher.AdminID)
	}
	for id := range target {
		if err := s.pg.AddTicketWatcher(ctx, ticketID, id); err != nil {
			return nil, err
		}
	}
	return s.pg.ListTicketWatchers(ctx, ticketID)
}

// rateByRequester 反馈人提交满意度评价。处理有了结论（已解决 / 不采纳 / 已关闭）才开放，只能评一次。
func (s *TicketService) rateByRequester(ctx context.Context, session *authdomain.Session, ticketID int64, cmd ticketdomain.RatingCommand) (*ticketdomain.Ticket, error) {
	item, err := s.requireUserFeedback(ctx, session, ticketID)
	if err != nil {
		return nil, err
	}
	if !ticketdomain.IsFinished(item.Status) || item.Status == ticketdomain.StatusCancelled {
		return nil, apperrors.New(40317, http.StatusForbidden, "反馈处理完成后才能评价")
	}
	if cmd.Rating < 1 || cmd.Rating > 5 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "评分必须在 1-5 之间")
	}
	updated, err := s.pg.SubmitTicketRating(ctx, ticketID, cmd.Rating, cmd.Comment)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, apperrors.New(40901, http.StatusConflict, "该反馈已评价过")
	}
	userID := session.UserID
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: ticketID, Event: ticketdomain.EventRated, ActorType: "user", ActorID: &userID,
		ActorName: item.RequesterName, ToValue: strconv.Itoa(int(cmd.Rating)),
		Summary: fmt.Sprintf("提交了 %d 星评价", cmd.Rating),
	})
	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	s.emitTicketEventAs(ctx, refreshed, ticketEventRated, ticketActor{UserID: &userID}, map[string]any{
		"rating": cmd.Rating, "comment": excerpt(cmd.Comment, 200),
	})
	return refreshed, nil
}

// cancelByRequester 反馈人撤销反馈。
func (s *TicketService) cancelByRequester(ctx context.Context, session *authdomain.Session, ticketID int64, reason string) (*ticketdomain.Ticket, error) {
	item, err := s.requireUserFeedback(ctx, session, ticketID)
	if err != nil {
		return nil, err
	}
	if ticketdomain.IsTerminal(item.Status) {
		return nil, apperrors.New(40316, http.StatusForbidden, "反馈已结束，无法撤销")
	}
	if err := s.pg.UpdateTicketStatus(ctx, ticketID, ticketdomain.StatusCancelled, false); err != nil {
		return nil, err
	}
	userID := session.UserID
	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: ticketID, Event: ticketdomain.EventStatusChanged, ActorType: "user", ActorID: &userID,
		ActorName: item.RequesterName, FromValue: item.Status, ToValue: ticketdomain.StatusCancelled,
		Summary: "反馈人撤销了反馈：" + strings.TrimSpace(reason),
	})
	refreshed, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	s.emitTicketEventAs(ctx, refreshed, ticketEventClosed, ticketActor{UserID: &userID}, map[string]any{"reason": "反馈人撤销：" + reason})
	return refreshed, nil
}

// ─────────────── 附件 ───────────────

// TicketAttachmentInput 附件上传入参。
type TicketAttachmentInput struct {
	TicketID      *int64
	AppID         int64
	FileName      string
	ContentType   string
	ContentLength int64
	Content       io.Reader
	UploaderType  string
	UploaderID    *int64
	// Kind 为 image 时严格校验：魔数必须是允许的图片类型，大小按图片上限；
	// 为 file 时不限类型；为空时按嗅探结果自动归类（工单附件入口）。
	Kind string
}

// UploadAttachment 上传附件到对象存储，落库后返回可预览地址。
func (s *TicketService) UploadAttachment(ctx context.Context, baseURL string, input TicketAttachmentInput) (*ticketdomain.Attachment, error) {
	if input.ContentLength <= 0 {
		return nil, apperrors.New(40087, http.StatusBadRequest, "上传文件不能为空")
	}
	requestedKind := strings.TrimSpace(strings.ToLower(input.Kind))
	switch requestedKind {
	case "":
		if input.ContentLength > ticketMaxAttachmentSize {
			return nil, apperrors.New(40088, http.StatusBadRequest, "工单附件不能超过 20MB")
		}
	case ticketdomain.AttachmentImage:
		if input.ContentLength > feedbackMaxImageSize {
			return nil, apperrors.New(errCodeFeedbackAttachmentTooLarge, http.StatusRequestEntityTooLarge, "图片不能超过 10MB")
		}
	case ticketdomain.AttachmentFile:
		if input.ContentLength > ticketMaxAttachmentSize {
			return nil, apperrors.New(errCodeFeedbackAttachmentTooLarge, http.StatusRequestEntityTooLarge, "附件不能超过 20MB")
		}
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "附件类型只能是 image 或 file")
	}
	fileName := strings.TrimSpace(input.FileName)
	if fileName == "" {
		fileName = "attachment"
	}

	// 类型以魔数为准：落库的 content_type 与 kind 都用嗅探结果，客户端声明只在认不出时兜底。
	// 存储层还会再嗅探一次，结论相同；这里先嗅探是为了在上传之前就拒掉冒充图片的文件。
	content, contentType := sniffAttachmentContentType(input.Content)
	kind := ticketdomain.AttachmentFile
	if isFeedbackImageType(contentType) {
		kind = ticketdomain.AttachmentImage
	}
	if requestedKind == ticketdomain.AttachmentImage && kind != ticketdomain.AttachmentImage {
		return nil, apperrors.New(errCodeFeedbackImageUnsupported, http.StatusUnsupportedMediaType, "图片仅支持 PNG、JPEG、WebP、GIF 格式")
	}
	if requestedKind == ticketdomain.AttachmentFile {
		kind = ticketdomain.AttachmentFile
	}
	if s.storage == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	ext := strings.ToLower(path.Ext(fileName))
	key, err := ticketObjectKey(ext)
	if err != nil {
		return nil, err
	}

	stored, err := s.storage.UploadForApp(ctx, input.AppID, storagedomain.UploadInput{
		AppID:         input.AppID,
		ObjectKey:     key,
		FileName:      fileName,
		ContentType:   contentType,
		ContentLength: input.ContentLength,
		Metadata:      map[string]string{"module": "ticket"},
		Content:       content,
		UploadedBy:    input.UploaderID,
		UploaderType:  input.UploaderType,
	})
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.ConfigID <= 0 || strings.TrimSpace(stored.Key) == "" {
		return nil, apperrors.New(50381, http.StatusServiceUnavailable, "存储返回结果异常")
	}

	uploaderType := strings.TrimSpace(input.UploaderType)
	if uploaderType != "user" {
		uploaderType = "admin"
	}
	saved, err := s.pg.CreateTicketAttachment(ctx, ticketdomain.Attachment{
		TicketID:       input.TicketID,
		Kind:           kind,
		FileName:       fileName,
		ContentType:    contentType,
		SizeBytes:      input.ContentLength,
		StorageRef:     fmt.Sprintf("%s%d/%s", ticketStoragePrefix, stored.ConfigID, url.PathEscape(stored.Key)),
		UploadedByType: uploaderType,
		UploadedByID:   input.UploaderID,
	})
	if err != nil {
		return nil, err
	}
	saved.DownloadURL = s.resolveAttachmentURL(ctx, baseURL, saved.StorageRef)
	return saved, nil
}

// sniffAttachmentContentType 只按魔数判定类型，认不出来就是 application/octet-stream。
//
// 与存储层的 resolveUploadContentType 不同，这里**不**回落到客户端声明或扩展名：
// 工单附件的 content_type 决定控制台与官网是否按图片内联展示，一段随机字节声明成
// image/png 就不该被当成图片。返回的 reader 保留了嗅探读掉的头部。
func sniffAttachmentContentType(content io.Reader) (io.Reader, string) {
	const generic = "application/octet-stream"
	if content == nil {
		return content, generic
	}
	buffered := bufio.NewReaderSize(content, mimeSniffLimit+512)
	head, err := buffered.Peek(mimeSniffLimit)
	if err != nil && !errors.Is(err, io.EOF) {
		return buffered, generic
	}
	if len(head) == 0 {
		return buffered, generic
	}
	detected := normalizeContentType(mimetype.Detect(head).String())
	if detected == "" || isGenericContentType(detected) {
		return buffered, generic
	}
	return buffered, detected
}

func ticketObjectKey(ext string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("tickets/%s/%s%s", time.Now().UTC().Format("200601"), hex.EncodeToString(buf), ext), nil
}

// resolveAttachmentURLs 批量换取永久代理地址。
func (s *TicketService) resolveAttachmentURLs(ctx context.Context, baseURL string, items []ticketdomain.Attachment) {
	for i := range items {
		items[i].DownloadURL = s.resolveAttachmentURL(ctx, baseURL, items[i].StorageRef)
	}
}

func (s *TicketService) resolveAttachmentURL(ctx context.Context, baseURL string, ref string) string {
	if s.storage == nil {
		return ""
	}
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, ticketStoragePrefix) {
		return trimmed
	}
	rest := strings.TrimPrefix(trimmed, ticketStoragePrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	configID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || configID <= 0 {
		return ""
	}
	objectKey, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(objectKey) == "" {
		return ""
	}
	_ = baseURL // 同源反代下返回相对路径即可，避免被前端图片管线判定为跨域 upstream
	link, err := s.storage.PermanentObjectLink(ctx, 0, configID, objectKey, false, "")
	if err != nil {
		s.log.Warn("工单附件地址解析失败", zap.Int64("configId", configID), zap.Error(err))
		return ""
	}
	return link
}

// attachMessageFiles 把附件挂回各自的消息，详情页可以就近展示。
func attachMessageFiles(messages []ticketdomain.Message, attachments []ticketdomain.Attachment) {
	if len(messages) == 0 || len(attachments) == 0 {
		return
	}
	byMessage := make(map[int64][]ticketdomain.Attachment, len(attachments))
	for _, attachment := range attachments {
		if attachment.MessageID == nil {
			continue
		}
		byMessage[*attachment.MessageID] = append(byMessage[*attachment.MessageID], attachment)
	}
	for i := range messages {
		if files, ok := byMessage[messages[i].ID]; ok {
			messages[i].Attachments = files
		}
	}
}

// ─────────────── 展示辅助 ───────────────

func priorityLabel(priority string) string {
	switch priority {
	case ticketdomain.PriorityUrgent:
		return "紧急"
	case ticketdomain.PriorityHigh:
		return "高"
	case ticketdomain.PriorityNormal:
		return "中"
	case ticketdomain.PriorityLow:
		return "低"
	}
	return priority
}

func ticketEventSummary(event string, actor string) string {
	switch event {
	case ticketdomain.EventReplied:
		return actor + " 回复了工单"
	case ticketdomain.EventInternalNote:
		return actor + " 添加了内部备注"
	}
	return actor + " 执行了操作"
}

func equalInt64Ptr(left, right *int64) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return *left == *right
}

func int64PtrString(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
