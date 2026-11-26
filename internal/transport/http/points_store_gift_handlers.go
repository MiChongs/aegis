package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/pointstore"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 积分商城「赠好友」：用户端（选人 / 核对 / 赠送 / 我的赠送记录）与管理端（赠送记录 / 审核 / 撤销 / 风控策略）。
// 规则见 docs/points-store-gift.md。

// PointsStoreGiftRequest 赠送一件商品。recipient 是对方的用户号码，或候选列表里给的对外用户 ID（publicId）；
// requestId 由客户端生成（建议 UUID），网络重试带同一个值不会重复扣积分。
type PointsStoreGiftRequest struct {
	ItemID    int64  `json:"itemId" binding:"required"`
	Recipient string `json:"recipient" binding:"required"`
	// Message 赠言，最多 60 字，接收人在站内信与赠送记录里看得到
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	DeviceID  string `json:"deviceId"`
}

// PointsStoreGiftReviewRequest 审核或撤销一笔赠送。note 会写进给用户的站内信。
// refundSender 只对撤销有效：为真时把积分退给赠送人（误送、客诉），为假时不退（套利、欺诈）。
type PointsStoreGiftReviewRequest struct {
	Note         string `json:"note"`
	RefundSender bool   `json:"refundSender"`
}

// PointsStoreGiftPolicyRequest 赠送风控策略。rules 按判据键给，缺的判据按推荐默认值。
type PointsStoreGiftPolicyRequest struct {
	Enabled bool                  `json:"enabled"`
	Rules   []pointstore.GiftRule `json:"rules"`
}

// giftAvatarResolver 按本次请求的域名把头像原值换成可访问的地址。
func (h *Handler) giftAvatarResolver(c *gin.Context, appID int64) service.AvatarResolver {
	return func(userID int64, raw string) string {
		return h.avatar.ResolveUserAvatar(c.Request.Context(), requestBaseURL(c.Request), appID, userID, raw)
	}
}

// ── 用户端 ──

// AppPointsStoreGiftRecipients 选人：不带 q 给最近互动过的人，带 q（号码）按号码找人。
func (h *Handler) AppPointsStoreGiftRecipients(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.pointsStore.GiftCandidates(c.Request.Context(), session.AppID, session.UserID, c.Query("q"),
		h.giftAvatarResolver(c, session.AppID))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// AppPreviewPointsStoreGift 提交前核对：对方是谁、会收到什么、能不能送（不能送时 allowed=false 并给出原因）。
func (h *Handler) AppPreviewPointsStoreGift(c *gin.Context) {
	h.appPointsStoreGift(c, true)
}

// AppGiftPointsStoreItem 赠送：放行的当场送达，需要审核的积分先扣、等审核，拒绝的不扣积分。
func (h *Handler) AppGiftPointsStoreItem(c *gin.Context) {
	h.appPointsStoreGift(c, false)
}

func (h *Handler) appPointsStoreGift(c *gin.Context, preview bool) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req PointsStoreGiftRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	input := service.GiftRequest{
		AppID: session.AppID, SenderID: session.UserID, ItemID: req.ItemID, Recipient: req.Recipient,
		Message: req.Message, RequestID: req.RequestID, DeviceID: deviceID, ClientIP: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	}
	avatar := h.giftAvatarResolver(c, session.AppID)
	if preview {
		result, err := h.pointsStore.PreviewGift(c.Request.Context(), input, avatar)
		if err != nil {
			h.writeError(c, err)
			return
		}
		response.Success(c, http.StatusOK, "ok", result)
		return
	}
	result, err := h.pointsStore.Gift(c.Request.Context(), input, avatar)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已送达"
	if result.Gift.Status == pointstore.GiftReviewing {
		message = "已提交，审核通过后对方即可收到"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppPointsStoreGifts 我的赠送记录：box=sent 我送出的 / received 我收到的，不传为两者。
func (h *Handler) AppPointsStoreGifts(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	page, err := h.pointsStore.MyGifts(c.Request.Context(), session.AppID, session.UserID, strings.TrimSpace(c.Query("box")),
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20), h.giftAvatarResolver(c, session.AppID))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// ── 管理端 ──

// AdminListPointsStoreGifts 赠送记录（status / userId / senderId / recipientId / itemId / keyword / from / to）与各状态笔数。
// status=reviewing 即审核队列。
func (h *Handler) AdminListPointsStoreGifts(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	parse := func(key string) int64 {
		value, _ := strconv.ParseInt(c.Query(key), 10, 64)
		return value
	}
	result, err := h.pointsStore.ListGifts(c.Request.Context(), pointstore.GiftQuery{
		AppID:       appID,
		UserID:      parse("userId"),
		SenderID:    parse("senderId"),
		RecipientID: parse("recipientId"),
		ItemID:      parse("itemId"),
		Status:      strings.TrimSpace(c.Query("status")),
		Keyword:     strings.TrimSpace(c.Query("keyword")),
		From:        cardKeyQueryTime(c.Query("from")),
		To:          cardKeyQueryTime(c.Query("to")),
		Page:        parsePositiveInt(c.Query("page"), 1),
		Limit:       parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGetPointsStoreGift 一笔赠送的详情：双方、发了什么、风控事实与命中的判据、审核与撤销记录。
func (h *Handler) AdminGetPointsStoreGift(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	giftID, ok := cardKeyPathID(c, "giftId")
	if !ok {
		return
	}
	gift, err := h.pointsStore.GetGift(c.Request.Context(), appID, giftID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gift)
}

// AdminApprovePointsStoreGift 审核通过：给接收人发放（站内信通知双方）。
func (h *Handler) AdminApprovePointsStoreGift(c *gin.Context) { h.handlePointsStoreGift(c, "approve") }

// AdminRejectPointsStoreGift 审核驳回：积分退回赠送人、库存加回。
func (h *Handler) AdminRejectPointsStoreGift(c *gin.Context) { h.handlePointsStoreGift(c, "reject") }

// AdminRevokePointsStoreGift 撤销已送达的赠送：从接收人处追回权益，refundSender 决定是否退积分。
func (h *Handler) AdminRevokePointsStoreGift(c *gin.Context) { h.handlePointsStoreGift(c, "revoke") }

func (h *Handler) handlePointsStoreGift(c *gin.Context, action string) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	giftID, ok := cardKeyPathID(c, "giftId")
	if !ok {
		return
	}
	var req PointsStoreGiftReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	input := pointstore.GiftReviewInput{AppID: appID, GiftID: giftID, Note: req.Note, RefundSender: req.RefundSender, Operator: operator}
	var (
		gift    *pointstore.Gift
		err     error
		message string
	)
	switch action {
	case "approve":
		gift, err = h.pointsStore.ApproveGift(c.Request.Context(), input)
		message = "已通过，对方已收到"
	case "reject":
		gift, err = h.pointsStore.RejectGift(c.Request.Context(), input)
		message = "已驳回，积分已退回赠送人"
	default:
		gift, err = h.pointsStore.RevokeGift(c.Request.Context(), input)
		message = "已撤销，权益已追回"
		if req.RefundSender {
			message += "，积分已退回赠送人"
		}
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, message, gift)
}

// AdminPointsStoreGiftPolicy 赠送风控策略、判据目录与推荐默认值。
func (h *Handler) AdminPointsStoreGiftPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	result, err := h.pointsStore.GiftPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminSavePointsStoreGiftPolicy 保存赠送风控策略。
func (h *Handler) AdminSavePointsStoreGiftPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req PointsStoreGiftPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	policy, err := h.pointsStore.SaveGiftPolicy(c.Request.Context(), pointstore.GiftPolicy{AppID: appID, Enabled: req.Enabled, Rules: req.Rules}, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "赠送风控已保存", policy)
}
