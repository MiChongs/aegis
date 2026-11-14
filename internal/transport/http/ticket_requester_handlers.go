package httptransport

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ticketdomain "aegis/internal/domain/ticket"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 工单 / 反馈里「人」的那部分：头像与提单人资料卡。
//
// 头像在库里是引用（或空），翻译成出网地址要经 AvatarService，而它只在 Handler 层可得；
// 解析只做令牌编码、不碰存储（见 AvatarService.view），放在列表链路上是安全的。

// resolveTicketAvatars 回填一批工单的提单人与受理人头像。
func (h *Handler) resolveTicketAvatars(c *gin.Context, items []ticketdomain.Ticket) {
	if h.avatar == nil {
		return
	}
	ctx, baseURL := c.Request.Context(), requestBaseURL(c.Request)
	for i := range items {
		h.resolveTicketAvatar(ctx, baseURL, &items[i])
	}
}

func (h *Handler) resolveTicketAvatar(ctx context.Context, baseURL string, item *ticketdomain.Ticket) {
	if h.avatar == nil || item == nil {
		return
	}
	ref := item.RequesterAvatarRef
	switch {
	case item.RequesterType == ticketdomain.RequesterAdmin && item.RequesterAdminID != nil:
		item.RequesterAvatar = h.avatar.ResolveAdminAvatar(ctx, baseURL, *item.RequesterAdminID, ref.Raw, ref.Email, ref.Account)
	case item.RequesterUserID != nil:
		item.RequesterAvatar = h.avatar.ResolveUserAvatar(ctx, baseURL, item.AppID, *item.RequesterUserID, ref.Raw, ref.Email, ref.Account)
	}
	if item.AssigneeAdminID != nil {
		ref = item.AssigneeAvatarRef
		item.AssigneeAvatar = h.avatar.ResolveAdminAvatar(ctx, baseURL, *item.AssigneeAdminID, ref.Raw, ref.Email, ref.Account)
	}
}

// resolveTicketDetailAvatars 详情场景：再补上会话里每个发言人与关注人的头像。
// 客服头像要多查一次 admin_accounts，失败只是没有头像，不影响详情本身。
func (h *Handler) resolveTicketDetailAvatars(c *gin.Context, item *ticketdomain.Ticket) {
	if h.avatar == nil || item == nil {
		return
	}
	ctx, baseURL := c.Request.Context(), requestBaseURL(c.Request)
	h.resolveTicketAvatar(ctx, baseURL, item)

	seen := make(map[int64]struct{}, 8)
	adminIDs := make([]int64, 0, 8)
	collect := func(id int64) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			adminIDs = append(adminIDs, id)
		}
	}
	for i := range item.Messages {
		if id := item.Messages[i].AuthorAdminID; id != nil {
			collect(*id)
		}
	}
	for i := range item.Watchers {
		collect(item.Watchers[i].AdminID)
	}
	refs, err := h.ticket.AdminAvatarRefs(ctx, adminIDs)
	if err != nil {
		refs = nil
	}
	adminAvatar := func(id int64) string {
		ref := refs[id]
		return h.avatar.ResolveAdminAvatar(ctx, baseURL, id, ref.Raw, ref.Email, ref.Account)
	}

	for i := range item.Messages {
		message := &item.Messages[i]
		switch {
		case message.AuthorAdminID != nil:
			message.AuthorAvatar = adminAvatar(*message.AuthorAdminID)
		case message.AuthorUserID != nil && item.RequesterUserID != nil && *message.AuthorUserID == *item.RequesterUserID:
			message.AuthorAvatar = item.RequesterAvatar
		}
	}
	for i := range item.Watchers {
		item.Watchers[i].Avatar = adminAvatar(item.Watchers[i].AdminID)
	}
}

// AdminTicketRequester 提单人资料卡
// GET /api/admin/tickets/:ticketId/requester
func (h *Handler) AdminTicketRequester(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	ctx := c.Request.Context()
	item, canViewUser, stats, err := h.ticket.RequesterContext(ctx, session, ticketID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.resolveTicketAvatar(ctx, requestBaseURL(c.Request), item)

	profile := ticketdomain.RequesterProfile{
		Type:    item.RequesterType,
		AppID:   item.AppID,
		AppName: item.AppName,
		Name:    item.RequesterName,
		Contact: item.RequesterContact,
		Avatar:  item.RequesterAvatar,
		Tickets: stats,
	}
	if item.AppID > 0 && h.app != nil {
		if app, appErr := h.app.GetApp(ctx, item.AppID); appErr == nil && app != nil {
			profile.AppKey = app.AppKey
		}
	}

	if item.RequesterType == ticketdomain.RequesterAdmin {
		profile.Removed = item.RequesterAdminID == nil
		response.Success(c, http.StatusOK, "获取成功", profile)
		return
	}
	// 用户被删除或注销后，数据库把 requester_user_id 置空，工单本身留档
	if item.RequesterUserID == nil {
		profile.Removed = true
		response.Success(c, http.StatusOK, "获取成功", profile)
		return
	}
	profile.CanViewUser = canViewUser
	if !canViewUser || h.user == nil {
		response.Success(c, http.StatusOK, "获取成功", profile)
		return
	}

	userID := *item.RequesterUserID
	user, err := h.user.GetAdminUser(ctx, item.AppID, userID)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusNotFound {
			profile.Removed = true
			response.Success(c, http.StatusOK, "获取成功", profile)
			return
		}
		h.writeError(c, err)
		return
	}
	profile.User = &ticketdomain.RequesterAccount{
		ID:               user.ID,
		PublicID:         user.PublicID,
		Account:          user.Account,
		Nickname:         user.Nickname,
		Email:            user.Email,
		Phone:            user.Phone,
		Enabled:          user.Enabled,
		DisabledReason:   user.DisabledReason,
		DisabledEndTime:  user.DisabledEndTime,
		Integral:         user.Integral,
		Experience:       user.Experience,
		VIPExpireAt:      user.VIPExpireAt,
		VIPLifetimeAt:    user.VIPLifetimeAt,
		RegisterTime:     user.RegisterTime,
		RegisterIP:       user.RegisterIP,
		RegisterProvince: user.RegisterProvince,
		RegisterCity:     user.RegisterCity,
		RegisterISP:      user.RegisterISP,
		CreatedAt:        user.CreatedAt,
	}
	profile.LastLogin = h.ticketRequesterLastLogin(ctx, item.AppID, userID)
	response.Success(c, http.StatusOK, "获取成功", profile)
}

// ticketRequesterLastLogin 最近一次成功登录。取不到就不显示，资料卡不因此失败。
func (h *Handler) ticketRequesterLastLogin(ctx context.Context, appID int64, userID int64) *ticketdomain.RequesterLogin {
	result, err := h.user.AdminListUserLoginAudits(ctx, appID, userID, userdomain.LoginAuditQuery{
		Status: "success",
		Page:   1,
		Limit:  1,
	})
	if err != nil || result == nil || len(result.Items) == 0 {
		return nil
	}
	record := result.Items[0]
	login := &ticketdomain.RequesterLogin{
		At:        record.CreatedAt,
		IP:        record.LoginIP,
		LoginType: record.LoginType,
		Provider:  record.Provider,
	}
	if info := record.DeviceInfo; info != nil {
		login.DeviceName = info.Name
		login.Platform = info.Platform
		login.OSVersion = strings.TrimSpace(strings.TrimSpace(info.OS) + " " + strings.TrimSpace(info.OSVersion))
		login.AppVersion = info.AppVersion
	}
	if ip := strings.TrimSpace(record.LoginIP); ip != "" {
		if loc, ok := h.resolveIPLocations(ctx, []string{ip})[ip]; ok && !loc.IsPrivate {
			login.Location = loc.Location
			login.ISP = loc.ISP
		}
	}
	return login
}
