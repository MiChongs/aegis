package httptransport

import (
	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *Handler) CreateAdminAppUserBan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminUserBanCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.CreateAdminUserBan(c.Request.Context(), appID, userID, userdomain.AccountBanCreateInput{
		BanType:  req.BanType,
		BanScope: req.BanScope,
		Reason:   req.Reason,
		Evidence: req.Evidence,
		StartAt:  req.StartAt,
		EndAt:    req.EndAt,
		Operator: userdomain.BanOperator{AdminID: adminID, AdminName: adminName},
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "user.ban.create", "user_ban", strconv.FormatInt(item.ID, 10), fmt.Sprintf("封禁用户 #%d", userID))
	response.Success(c, 200, "封禁成功", item)
}

func (h *Handler) BatchCreateAdminAppUserBan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminUserBanBatchCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.BatchCreateAdminUserBan(c.Request.Context(), appID, userdomain.AccountBanBatchCreateInput{
		UserIDs: req.UserIDs,
		AccountBanCreateInput: userdomain.AccountBanCreateInput{
			BanType:  req.BanType,
			BanScope: req.BanScope,
			Reason:   req.Reason,
			Evidence: req.Evidence,
			StartAt:  req.StartAt,
			EndAt:    req.EndAt,
			Operator: userdomain.BanOperator{AdminID: adminID, AdminName: adminName},
		},
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "user.ban.batch_create", "user_ban", "", fmt.Sprintf("批量封禁 %d 个用户", len(req.UserIDs)))
	response.Success(c, 200, "批量封禁成功", item)
}

func (h *Handler) AdminAppUserBans(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var query AdminUserBanListQuery
	_ = c.ShouldBindQuery(&query)
	item, err := h.user.ListAdminUserBans(c.Request.Context(), appID, userID, userdomain.AccountBanQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppUserActiveBan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	item, err := h.user.GetAdminUserActiveBan(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) RevokeAdminAppUserBan(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	banID, err := pathInt64(c, "banId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的封禁标识")
		return
	}
	var req AdminUserBanRevokeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.RevokeAdminUserBan(c.Request.Context(), appID, userID, banID, userdomain.AccountBanRevokeInput{
		Reason:   req.Reason,
		Operator: userdomain.BanOperator{AdminID: adminID, AdminName: adminName},
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "user.ban.revoke", "user_ban", strconv.FormatInt(banID, 10), fmt.Sprintf("撤销用户 #%d 封禁", userID))
	response.Success(c, 200, "撤销成功", item)
}
