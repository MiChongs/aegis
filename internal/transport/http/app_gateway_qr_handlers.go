package httptransport

import (
	"context"
	"net/http"
	"strings"

	"aegis/internal/middleware"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 网页扫码登录。流程与安全边界见 service/auth_qr.go。
//
// create / poll 不需要登录，在 signed / sealed 档下与其他网关接口一样要求包装，
// 因此网页必须经由持有 appSecret 的网站服务端代为调用；scan / confirm / cancel
// 走 Bearer 组，以移动端当前用户的身份调用。

// AppQRLoginCreate 网页申请一张扫码登录票据。
func (h *Handler) AppQRLoginCreate(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppQRLoginCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)
	ip := c.ClientIP()
	result, err := h.auth.CreateQRLogin(c.Request.Context(), service.QRLoginCreateInput{
		AppID:     app.ID,
		DeviceID:  deviceID,
		Device:    device,
		IP:        ip,
		UserAgent: c.Request.UserAgent(),
		Location:  h.qrLoginLocation(c, ip),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已生成登录二维码", result)
}

// AppQRLoginPoll 网页轮询票据状态；确认后的第一次轮询领取会话。
func (h *Handler) AppQRLoginPoll(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req AppQRLoginPollRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.PollQRLogin(c.Request.Context(), app.ID, req.TicketID, req.PollToken, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "获取成功"
	if result.Status == "confirmed" {
		message = "登录成功"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppQRLoginScan 移动端扫码，返回发起端信息供用户确认。
func (h *Handler) AppQRLoginScan(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AppQRLoginTicketRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 扫码人的昵称与头像给网页展示「某某已扫码」。取不到不影响流程。
	scanner := service.QRLoginScanner{Nickname: session.Account}
	if h.user != nil {
		if view, err := h.user.GetMy(c.Request.Context(), session); err == nil && view != nil {
			if strings.TrimSpace(view.Nickname) != "" {
				scanner.Nickname = view.Nickname
			}
			scanner.Avatar = view.Avatar
		}
	}
	result, err := h.auth.ScanQRLogin(c.Request.Context(), session, req.TicketID, scanner)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已扫码", result)
}

// AppQRLoginConfirm 移动端确认网页登录。
func (h *Handler) AppQRLoginConfirm(c *gin.Context) {
	h.decideQRLogin(c, true)
}

// AppQRLoginCancel 移动端拒绝网页登录。
func (h *Handler) AppQRLoginCancel(c *gin.Context) {
	h.decideQRLogin(c, false)
}

func (h *Handler) decideQRLogin(c *gin.Context, approve bool) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req AppQRLoginTicketRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if approve {
		if err := h.auth.ConfirmQRLogin(c.Request.Context(), session, req.TicketID); err != nil {
			h.writeError(c, err)
			return
		}
		response.Success(c, http.StatusOK, "已确认登录", gin.H{"confirmed": true})
		return
	}
	if err := h.auth.CancelQRLogin(c.Request.Context(), session, req.TicketID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已拒绝登录", gin.H{"cancelled": true})
}

// qrLoginLocation 发起端的大致位置，展示在移动端确认页上。
// 先用请求级缓存，未命中时同步解析一次；仍然没有就留空，不阻塞发码。
func (h *Handler) qrLoginLocation(c *gin.Context, ip string) string {
	if text := middleware.RequestLocationString(c); text != "" {
		return text
	}
	if h.location == nil || ip == "" {
		return ""
	}
	return formatIPLocation(h.location.Resolve(context.WithoutCancel(c.Request.Context()), ip))
}

func formatIPLocation(location service.IPLocation) string {
	if text := strings.TrimSpace(location.Location); text != "" {
		return text
	}
	parts := make([]string, 0, 4)
	for _, part := range []string{location.Country, location.Region, location.City, location.District} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}
