package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	notificationdomain "aegis/internal/domain/notification"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 单条回复撤回。
//
// 撤回是软删除：消息行留在库里（撤回人、时间、原因一并记下），
//   - 提单人侧（反馈详情、列表摘要、附件计数）一律不再下发；
//   - 管理端详情照常列出并标为「已撤回」，能看内部备注的人看得到原文，其余人只看到占位；
//   - 这条回复生成的站内信（提单人）与管理员收件箱条目一并删除，并经实时通道刷新未读数；
//   - 时间线记一条 message_retracted 事件；工单汇总字段（消息数、最后发言）按剩下的消息重算。
//
// 谁能撤回：
//   - 提单人的消息：谁都不能（撤回的是我们说错的话，不是替用户删话）；
//   - 处理人自己的回复 / 备注：能看到这张工单（备注还要能看内部备注）就能撤回；
//   - 别人的回复 / 备注、系统消息：要有该应用的 ticket:manage（超管天然满足）。
//
// 工单状态、锁定与否都不影响撤回：撤回是纠错，结了单的会话里说错的话同样要能收回。

const (
	ticketRetractReasonMaxLen = 200
	// ticketRetractProbeLen 旧通知（元数据里没有 messageId）按正文开头这么多字匹配
	ticketRetractProbeLen = 40

	errCodeTicketMessageNotFound  = 40467
	errCodeTicketMessageRetracted = 40904
)

// ticketUserInboxCleaner 删除应用用户站内信（失效缓存并推送 notification.deleted 与最新未读数）。
type ticketUserInboxCleaner interface {
	AdminDelete(ctx context.Context, appID int64, ids []int64) (*notificationdomain.AdminDeleteResult, error)
}

// ticketAdminInboxCleaner 删除管理员收件箱条目并刷新角标。
type ticketAdminInboxCleaner interface {
	RemoveByIDs(ctx context.Context, ids []int64) (int64, error)
}

// SetInboxes 接上两个收件箱，撤回回复时用来清理它生成的通知。未接时只撤回消息本身。
func (s *TicketService) SetInboxes(users ticketUserInboxCleaner, admins ticketAdminInboxCleaner) {
	s.userInbox = users
	s.adminInbox = admins
}

// RetractMessageResult 撤回结果。
type RetractMessageResult struct {
	Message *ticketdomain.Message `json:"message"`
	// NotificationsRemoved 删掉的提单人站内信条数
	NotificationsRemoved int64 `json:"notificationsRemoved"`
	// AdminInboxRemoved 删掉的管理员收件箱条数
	AdminInboxRemoved int64 `json:"adminInboxRemoved"`
}

// canRetractMessage 撤回判定（纯函数，详情页的 canRetract 与撤回接口共用）。
// 返回不能撤回时给调用方的错误；能撤回返回 nil。
func canRetractMessage(message *ticketdomain.Message, actions *ticketdomain.ActionSet, adminID int64) error {
	if message == nil {
		return apperrors.New(errCodeTicketMessageNotFound, http.StatusNotFound, "消息不存在")
	}
	if actions == nil || !actions.View {
		return apperrors.New(40313, http.StatusForbidden, "无权访问该工单")
	}
	if message.Internal && !actions.ViewInternal {
		// 看不到的内部备注按不存在回答，不让人探测备注 ID
		return apperrors.New(errCodeTicketMessageNotFound, http.StatusNotFound, "消息不存在")
	}
	if message.Retracted {
		return apperrors.New(errCodeTicketMessageRetracted, http.StatusConflict, "这条消息已经撤回")
	}
	if message.AuthorType == ticketdomain.AuthorRequester {
		return apperrors.New(40311, http.StatusForbidden, "提单人的消息不能撤回")
	}
	if actions.RetractAny {
		return nil
	}
	own := message.AuthorType == ticketdomain.AuthorAgent &&
		message.AuthorAdminID != nil && *message.AuthorAdminID == adminID
	if own && actions.RetractOwn {
		return nil
	}
	return apperrors.New(40311, http.StatusForbidden, "只能撤回自己的回复；撤回他人的回复需要「工单配置」权限")
}

// RetractMessage 撤回一条回复或内部备注。reason 可空。
func (s *TicketService) RetractMessage(ctx context.Context, access *admindomain.AccessContext,
	ticketID int64, messageID int64, reason string) (*RetractMessageResult, error) {

	item, _, actions, err := s.requireTicketAccess(ctx, access, ticketID)
	if err != nil {
		return nil, err
	}
	message, err := s.pg.GetTicketMessage(ctx, ticketID, messageID)
	if err != nil {
		return nil, err
	}
	if err := canRetractMessage(message, actions, access.AdminID); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > ticketRetractReasonMaxLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "撤回原因不能超过 200 字")
	}

	adminID := access.AdminID
	actorName := adminDisplayName(access)
	retracted, err := s.pg.RetractTicketMessage(ctx, pgrepo.RetractTicketMessageInput{
		TicketID: ticketID, MessageID: messageID, AdminID: &adminID, AdminName: actorName, Reason: reason,
	})
	if err != nil {
		if errors.Is(err, pgrepo.ErrTicketMessageAlreadyRetracted) {
			return nil, apperrors.New(errCodeTicketMessageRetracted, http.StatusConflict, "这条消息已经撤回")
		}
		return nil, err
	}

	_ = s.pg.AddTicketEvent(ctx, ticketdomain.Event{
		TicketID: ticketID, Event: ticketdomain.EventMessageRetracted, ActorType: "admin", ActorID: &adminID,
		ActorName: actorName, FromValue: strconv.FormatInt(messageID, 10),
		Summary: retractEventSummary(actorName, message, reason),
		Metadata: map[string]any{
			"messageId":  messageID,
			"internal":   message.Internal,
			"authorName": message.AuthorName,
			"reason":     reason,
		},
	})

	result := &RetractMessageResult{Message: retracted}
	// 内部备注从不外发，也不进收件箱（ReplyByAdmin 只对对外回复发事件）
	if !message.Internal {
		result.NotificationsRemoved, result.AdminInboxRemoved = s.removeReplyNotifications(ctx, item, message)
	}
	return result, nil
}

// retractEventSummary 时间线文案。
func retractEventSummary(actor string, message *ticketdomain.Message, reason string) string {
	what := "一条回复"
	if message.Internal {
		what = "一条内部备注"
	}
	summary := actor + " 撤回了" + what
	if author := strings.TrimSpace(message.AuthorName); author != "" && author != actor {
		summary = actor + " 撤回了 " + author + " 的" + what
	}
	if reason != "" {
		summary += "：" + reason
	}
	return summary
}

// removeReplyNotifications 删掉这条回复生成的站内信与管理员收件箱条目。失败只记日志：
// 消息本身已经撤回，通知清理是尽力而为，不能让它把撤回整个报错。
func (s *TicketService) removeReplyNotifications(ctx context.Context, item *ticketdomain.Ticket, message *ticketdomain.Message) (int64, int64) {
	resource := "ticket"
	if item.Kind == ticketdomain.KindFeedback {
		resource = "feedback"
	}
	query := pgrepo.TicketReplyNotificationQuery{
		AppID:      item.AppID,
		Resource:   resource,
		ResourceID: strconv.FormatInt(item.ID, 10),
		EventKey:   ticketEventAgentReplied,
		MessageID:  message.ID,
		SentAfter:  message.CreatedAt,
		Probe:      retractProbe(message.Content),
	}

	var userRemoved, adminRemoved int64
	if s.userInbox != nil && item.Kind == ticketdomain.KindFeedback && item.RequesterUserID != nil {
		query.UserID = *item.RequesterUserID
		ids, err := s.pg.FindTicketReplyNotifications(ctx, query)
		if err != nil {
			s.log.Warn("查找回复生成的站内信失败", zap.Int64("ticketId", item.ID), zap.Int64("messageId", message.ID), zap.Error(err))
		} else if len(ids) > 0 {
			deleted, derr := s.userInbox.AdminDelete(ctx, item.AppID, ids)
			if derr != nil {
				s.log.Warn("删除回复生成的站内信失败", zap.Int64("ticketId", item.ID), zap.Int64("messageId", message.ID), zap.Error(derr))
			} else if deleted != nil {
				userRemoved = deleted.Deleted
			}
		}
	}
	if s.adminInbox != nil {
		ids, err := s.pg.FindTicketReplyAdminInbox(ctx, query)
		if err != nil {
			s.log.Warn("查找回复生成的收件箱条目失败", zap.Int64("ticketId", item.ID), zap.Int64("messageId", message.ID), zap.Error(err))
		} else if len(ids) > 0 {
			deleted, derr := s.adminInbox.RemoveByIDs(ctx, ids)
			if derr != nil {
				s.log.Warn("删除回复生成的收件箱条目失败", zap.Int64("ticketId", item.ID), zap.Int64("messageId", message.ID), zap.Error(derr))
			}
			adminRemoved = deleted
		}
	}
	return userRemoved, adminRemoved
}

// retractProbe 旧通知兜底匹配用的正文开头。通知正文是「客服回复：」+ 回复开头 200 字，
// 取前 40 字足以在同一张工单、同一时间窗里唯一命中。
func retractProbe(content string) string {
	runes := []rune(strings.TrimSpace(content))
	if len(runes) > ticketRetractProbeLen {
		runes = runes[:ticketRetractProbeLen]
	}
	return string(runes)
}

// presentMessagesForAdmin 管理端详情里的消息：标出能否撤回；已撤回的消息对看不到内部备注的人隐去原文。
func presentMessagesForAdmin(messages []ticketdomain.Message, actions *ticketdomain.ActionSet, adminID int64) {
	for i := range messages {
		message := &messages[i]
		if message.Retracted {
			if actions == nil || !actions.ViewInternal {
				message.Content = ""
				message.Attachments = nil
				message.DeleteReason = ""
			}
			continue
		}
		message.CanRetract = canRetractMessage(message, actions, adminID) == nil
	}
}

// dropRetractedAttachments 去掉挂在已撤回消息上的附件（看不到内部备注的人用）。
func dropRetractedAttachments(messages []ticketdomain.Message, attachments []ticketdomain.Attachment) []ticketdomain.Attachment {
	retracted := make(map[int64]struct{})
	for _, message := range messages {
		if message.Retracted {
			retracted[message.ID] = struct{}{}
		}
	}
	if len(retracted) == 0 {
		return attachments
	}
	out := make([]ticketdomain.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.MessageID != nil {
			if _, ok := retracted[*attachment.MessageID]; ok {
				continue
			}
		}
		out = append(out, attachment)
	}
	return out
}
