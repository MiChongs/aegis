package httptransport

import (
	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 管理端「某一个用户」的审计视图。
//
// 应用级的 /apps/:appkey/audits/login 只支持 keyword 模糊搜，
// 而 keyword 同时匹配账号 / 昵称 / IP / deviceId / UA / provider ——
// 拿账号当 keyword 拼出来的「这个人的登录历史」既会混进他人记录，
// 分页总数也不是这个人的。用户详情页需要的是按 user_id 精确过滤的那一份。

type AdminUserLoginAuditQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

type AdminUserSessionAuditQuery struct {
	EventType string `form:"eventType"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}

// AdminAppUserLoginAudits GET /api/admin/apps/:appkey/users/:userId/audits/login
func (h *Handler) AdminAppUserLoginAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var query AdminUserLoginAuditQuery
	_ = c.ShouldBindQuery(&query)
	item, err := h.user.AdminListUserLoginAudits(c.Request.Context(), appID, userID, userdomain.LoginAuditQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 位置回填：活动地图按这批记录画点与轨迹，没有经纬度就只能靠城市名猜坐标
	if item != nil && h.location != nil {
		ips := make([]string, 0, len(item.Items))
		for i := range item.Items {
			ips = append(ips, item.Items[i].LoginIP)
		}
		located := h.resolveIPLocations(c.Request.Context(), ips)
		for i := range item.Items {
			loc, ok := located[item.Items[i].LoginIP]
			if !ok {
				continue
			}
			item.Items[i].Country = loc.Country
			item.Items[i].CountryCode = loc.CountryCode
			item.Items[i].Region = loc.Region
			item.Items[i].City = loc.City
			item.Items[i].ISP = loc.ISP
			item.Items[i].Location = loc.Location
			item.Items[i].Latitude, item.Items[i].Longitude = geoCoords(loc)
			item.Items[i].IsPrivate = loc.IsPrivate
		}
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminAppUserSessionAudits GET /api/admin/apps/:appkey/users/:userId/audits/sessions
func (h *Handler) AdminAppUserSessionAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var query AdminUserSessionAuditQuery
	_ = c.ShouldBindQuery(&query)
	item, err := h.user.AdminListUserSessionAudits(c.Request.Context(), appID, userID, userdomain.SessionAuditQuery{
		EventType: query.EventType,
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}
