package httptransport

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	authdomain "aegis/internal/domain/auth"
	cardkeydomain "aegis/internal/domain/cardkey"
	"aegis/pkg/response"
)

// 卡密：管理端（生成 / 作废 / 导出 / 核销记录）与用户端（兑换 / 我的授权）。

// ── DTO ──

// CardKeyRewardRequest 一项权益。三个取值字段互斥，由权益目录决定用哪一个。
type CardKeyRewardRequest struct {
	Type   string `json:"type" binding:"required"`
	Amount int64  `json:"amount"`
	Money  string `json:"money"`
	RefID  int64  `json:"refId"`
}

// GenerateCardKeysRequest 生成一批卡密。
type GenerateCardKeysRequest struct {
	Name          string                 `json:"name" binding:"required"`
	Kind          string                 `json:"kind" binding:"required"`
	Remark        string                 `json:"remark"`
	Count         int                    `json:"count" binding:"required"`
	CodePrefix    string                 `json:"codePrefix"`
	Segments      int                    `json:"segments"`
	SegmentLength int                    `json:"segmentLength"`
	Rewards       []CardKeyRewardRequest `json:"rewards"`
	MaxDevices    int                    `json:"maxDevices"`
	ValidityMode  string                 `json:"validityMode"`
	ValidityDays  int                    `json:"validityDays"`
	ValidUntil    *time.Time             `json:"validUntil"`
}

// CardKeyBatchStatusRequest 批次启停。
type CardKeyBatchStatusRequest struct {
	Enabled bool `json:"enabled"`
}

// CardKeyIDsRequest 批量作废 / 恢复。
//
// 按**选中的 id 列表**下发而不是「按当前筛选条件批量」：管理员看到的列表与
// 实际执行之间存在时间差（翻页期间有人兑换），按条件批量会误伤没被看过的卡。
type CardKeyIDsRequest struct {
	IDs    []int64 `json:"ids" binding:"required"`
	Reason string  `json:"reason"`
}

// RedeemCardKeyRequest 用户兑换。
type RedeemCardKeyRequest struct {
	Code     string `json:"code" binding:"required"`
	DeviceID string `json:"deviceId"`
}

// CardKeyLoginResponse 卡密登录的响应：登录结果 + 这张卡的授权快照。
type CardKeyLoginResponse struct {
	*authdomain.LoginResult
	Authorization *cardkeydomain.LoginAuthorization `json:"authorization"`
}

func (req GenerateCardKeysRequest) toInput(appID int64, operator string) cardkeydomain.GenerateInput {
	rewards := make([]cardkeydomain.Reward, 0, len(req.Rewards))
	for _, item := range req.Rewards {
		rewards = append(rewards, item.toDomain())
	}
	return cardkeydomain.GenerateInput{
		AppID:         appID,
		Name:          req.Name,
		Kind:          req.Kind,
		Remark:        req.Remark,
		Count:         req.Count,
		CodePrefix:    req.CodePrefix,
		Segments:      req.Segments,
		SegmentLength: req.SegmentLength,
		Rewards:       rewards,
		MaxDevices:    req.MaxDevices,
		ValidityMode:  req.ValidityMode,
		ValidityDays:  req.ValidityDays,
		ValidUntil:    req.ValidUntil,
		Operator:      operator,
	}
}

func (req CardKeyRewardRequest) toDomain() cardkeydomain.Reward {
	reward := cardkeydomain.Reward{Type: req.Type, Amount: req.Amount, RefID: req.RefID}
	// 金额按字符串传输：JSON number 在前端是 IEEE754 双精度，
	// 一张面额 0.1 元的卡走一遍就可能变成 0.09999999999999999。
	if money := strings.TrimSpace(req.Money); money != "" {
		if parsed, err := decimal.NewFromString(money); err == nil {
			reward.Money = parsed
		}
	}
	return reward
}

// cardKeyPathID 取路径上的数字标识，无效时已经写好响应。
func cardKeyPathID(c *gin.Context, name string) (int64, bool) {
	value, err := pathInt64(c, name)
	if err != nil || value <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, name+" 无效")
		return 0, false
	}
	return value, true
}

// ── 管理端 ──

// AdminCardKeyCatalog 权益目录。控制台的权益编辑表单由它驱动。
func (h *Handler) AdminCardKeyCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok {
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{
		"rewards":       cardkeydomain.RewardCatalog(),
		"kinds":         []string{cardkeydomain.KindLogin, cardkeydomain.KindRedeem},
		"validityModes": []string{cardkeydomain.ValidityPermanent, cardkeydomain.ValidityFixedUntil, cardkeydomain.ValidityFromFirstUse},
		"maxRewards":    cardkeydomain.MaxRewardsPerCard,
	})
}

// AdminListCardKeyBatches 批次列表（随行带核销进度）。
func (h *Handler) AdminListCardKeyBatches(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.cardKey.ListBatches(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminGenerateCardKeys 生成一批卡密。
func (h *Handler) AdminGenerateCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req GenerateCardKeysRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	batch, err := h.cardKey.GenerateBatch(c.Request.Context(), req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "生成成功", batch)
}

// AdminSetCardKeyBatchStatus 启停批次。
func (h *Handler) AdminSetCardKeyBatchStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	var req CardKeyBatchStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.cardKey.SetBatchStatus(c.Request.Context(), appID, batchID, req.Enabled); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已更新", gin.H{"enabled": req.Enabled})
}

// AdminDeleteCardKeyBatch 删除批次（级联带走其下全部卡与核销记录）。
func (h *Handler) AdminDeleteCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	if err := h.cardKey.DeleteBatch(c.Request.Context(), appID, batchID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// AdminExportCardKeyBatch 导出一批卡面（CSV）。
//
// 按 batchId 导出而不是按当前筛选条件：生成完立刻导出时列表可能还没刷新到那一批，
// 按筛选导出会得到一份不完整的卡 —— 而那份文件是要发给渠道的。
func (h *Handler) AdminExportCardKeyBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, ok := cardKeyPathID(c, "batchId")
	if !ok {
		return
	}
	batch, cards, err := h.cardKey.ExportBatch(c.Request.Context(), appID, batchID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "card_keys_" + strconv.FormatInt(batch.ID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	// BOM：Excel 打开中文 CSV 不乱码
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"卡密", "批次", "类型", "状态", "绑定账号", "可绑设备数", "激活时间", "到期时间", "备注"})
	for _, card := range cards {
		_ = writer.Write([]string{
			card.Code,
			batch.Name,
			cardKindLabel(card.Kind),
			cardStatusLabel(&card),
			card.BoundAccount,
			strconv.Itoa(card.MaxDevices),
			formatOptionalTime(card.ActivatedAt),
			formatOptionalTime(card.ExpiresAt),
			card.Remark,
		})
	}
}

// AdminListCardKeys 单卡列表（服务端筛选 + 分页）。
func (h *Handler) AdminListCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, _ := strconv.ParseInt(c.Query("batchId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.cardKey.ListCards(c.Request.Context(), cardkeydomain.CardQuery{
		AppID:   appID,
		BatchID: batchID,
		UserID:  userID,
		Status:  strings.TrimSpace(c.Query("status")),
		Kind:    strings.TrimSpace(c.Query("kind")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminDisableCardKeys 批量作废。
func (h *Handler) AdminDisableCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.DisableCards(c.Request.Context(), appID, req.IDs, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已作废", gin.H{"affected": affected})
}

// AdminRestoreCardKeys 批量撤销作废。
func (h *Handler) AdminRestoreCardKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req CardKeyIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	affected, err := h.cardKey.RestoreCards(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已恢复", gin.H{"affected": affected})
}

// AdminListCardKeyDevices 一张卡绑定的设备。
func (h *Handler) AdminListCardKeyDevices(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	items, err := h.cardKey.ListDevices(c.Request.Context(), appID, cardID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminUnbindCardKeyDevice 解绑一台设备，把名额还回去。
func (h *Handler) AdminUnbindCardKeyDevice(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cardID, ok := cardKeyPathID(c, "cardId")
	if !ok {
		return
	}
	if err := h.cardKey.UnbindDevice(c.Request.Context(), appID, cardID, c.Param("deviceId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已解绑", gin.H{"unbound": true})
}

// AdminListCardKeyRedemptions 核销记录。
func (h *Handler) AdminListCardKeyRedemptions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	batchID, _ := strconv.ParseInt(c.Query("batchId"), 10, 64)
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.cardKey.ListRedemptions(c.Request.Context(), cardkeydomain.RedemptionQuery{
		AppID:   appID,
		BatchID: batchID,
		UserID:  userID,
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// ── 用户端 ──

// AppRedeemCardKey 兑换一张卡密。
func (h *Handler) AppRedeemCardKey(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req RedeemCardKeyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	result, err := h.cardKey.Redeem(c.Request.Context(), cardkeydomain.RedeemInput{
		AppID:     session.AppID,
		Code:      req.Code,
		UserID:    session.UserID,
		Source:    cardkeydomain.SourceRedeem,
		DeviceID:  deviceID,
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "兑换成功", result)
}

// AppMyCardKeys 我名下的授权卡（还剩多久、绑了几台设备）。
func (h *Handler) AppMyCardKeys(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.cardKey.MyCards(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": items})
}

// ── 展示辅助 ──

func cardKindLabel(kind string) string {
	if kind == cardkeydomain.KindLogin {
		return "授权卡"
	}
	return "兑换卡"
}

// cardStatusLabel 「已过期」是算出来的结论，不是一档状态 —— 导出的 CSV 里
// 也要如实说出来，否则运营拿着一张写着「未使用」的过期卡去排查客诉。
func cardStatusLabel(card *cardkeydomain.Card) string {
	if card.Status != cardkeydomain.StatusDisabled && card.Expired(time.Now().UTC()) {
		return "已过期"
	}
	switch card.Status {
	case cardkeydomain.StatusUnused:
		return "未使用"
	case cardkeydomain.StatusActive:
		return "使用中"
	case cardkeydomain.StatusUsed:
		return "已核销"
	case cardkeydomain.StatusDisabled:
		return "已作废"
	}
	return card.Status
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}
