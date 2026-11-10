package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// AI 中转网关：用户端（接入网关 /api/v1/apps/{appKey}/ai/*）与管理端（/api/admin/apps/{appkey}/ai-gateway/*）。
// 调用面本身在 ai_relay_handlers.go。

// AIGatewayKeyRequest 新建 / 修改 API 密钥。spendLimit 单位为点，0 不限。修改时没传的字段不改。
type AIGatewayKeyRequest struct {
	Name        *string          `json:"name"`
	Status      string           `json:"status"`
	Models      *[]string        `json:"models"`
	SpendLimit  *decimal.Decimal `json:"spendLimit"`
	ExpiresAt   *time.Time       `json:"expiresAt"`
	ClearExpiry bool             `json:"clearExpiry"`
}

func (req AIGatewayKeyRequest) toInput() aigateway.KeyInput {
	return aigateway.KeyInput{Name: req.Name, Status: req.Status, Models: req.Models, SpendLimit: req.SpendLimit,
		ExpiresAt: req.ExpiresAt, ClearExpiry: req.ClearExpiry}
}

func (h *Handler) aiGatewayReady(c *gin.Context) bool {
	if h.aiGateway == nil {
		response.Error(c, http.StatusServiceUnavailable, 40202, "AI 服务未开启")
		return false
	}
	return true
}

// aiGatewaySession 用户端入口的前置：已登录、网关服务已装配。
func (h *Handler) aiGatewaySession(c *gin.Context) (*authdomain.Session, bool) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return nil, false
	}
	if !h.aiGatewayReady(c) {
		return nil, false
	}
	return session, true
}

func aiGatewayRequestQuery(c *gin.Context) aigateway.RequestQuery {
	query := aigateway.RequestQuery{
		Model:    strings.TrimSpace(c.Query("model")),
		Status:   strings.TrimSpace(c.Query("status")),
		Endpoint: strings.TrimSpace(c.Query("endpoint")),
		Page:     parsePositiveInt(c.Query("page"), 1),
		Limit:    parsePositiveInt(c.Query("limit"), 20),
		From:     cardKeyQueryTime(c.Query("start")),
		To:       cardKeyQueryTime(c.Query("end")),
	}
	query.KeyID, _ = strconv.ParseInt(c.Query("keyId"), 10, 64)
	query.Keyword = strings.TrimSpace(c.Query("keyword"))
	query.ConfigID, _ = strconv.ParseInt(c.Query("configId"), 10, 64)
	return query
}

// ── 用户端 ──

// AppAIOverview GET /api/v1/apps/:appkey/ai/overview
func (h *Handler) AppAIOverview(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	overview, err := h.aiGateway.Overview(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", overview)
}

// AppAIModels GET /api/v1/apps/:appkey/ai/models
func (h *Handler) AppAIModels(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	models, err := h.aiGateway.Models(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": models})
}

// AppAIKeys GET /api/v1/apps/:appkey/ai/keys
func (h *Handler) AppAIKeys(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	keys, err := h.aiGateway.ListMyKeys(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": keys})
}

// AppCreateAIKey POST /api/v1/apps/:appkey/ai/keys —— 返回的 secret 只出现这一次。
func (h *Handler) AppCreateAIKey(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	var req AIGatewayKeyRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	created, err := h.aiGateway.CreateKey(c.Request.Context(), session, req.toInput())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", created)
}

// AppUpdateAIKey PUT /api/v1/apps/:appkey/ai/keys/:keyId
func (h *Handler) AppUpdateAIKey(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	keyID, ok := cardKeyPathID(c, "keyId")
	if !ok {
		return
	}
	var req AIGatewayKeyRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	key, err := h.aiGateway.UpdateKey(c.Request.Context(), session, keyID, req.toInput())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", key)
}

// AppDeleteAIKey DELETE /api/v1/apps/:appkey/ai/keys/:keyId
func (h *Handler) AppDeleteAIKey(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	keyID, ok := cardKeyPathID(c, "keyId")
	if !ok {
		return
	}
	if err := h.aiGateway.DeleteKey(c.Request.Context(), session, keyID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"deleted": true})
}

// AppAIUsage GET /api/v1/apps/:appkey/ai/usage?range=today|24h|7d|30d|90d&keyId=&model=
// keyId 不传统计全部，传 0 只统计登录令牌发起的调用。
func (h *Handler) AppAIUsage(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	var keyID *int64
	if raw := strings.TrimSpace(c.Query("keyId")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			response.Error(c, http.StatusBadRequest, 40000, "keyId 无效")
			return
		}
		keyID = &parsed
	}
	usage, err := h.aiGateway.MyUsage(c.Request.Context(), session, c.Query("range"), keyID, c.Query("model"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", usage)
}

// AppAIRequests GET /api/v1/apps/:appkey/ai/requests
func (h *Handler) AppAIRequests(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	page, err := h.aiGateway.ListMyRequests(c.Request.Context(), session, aiGatewayRequestQuery(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AppAIRequest GET /api/v1/apps/:appkey/ai/requests/:requestNo
func (h *Handler) AppAIRequest(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	record, err := h.aiGateway.GetMyRequest(c.Request.Context(), session, c.Param("requestNo"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", record)
}

// AppAICredits GET /api/v1/apps/:appkey/ai/credits
func (h *Handler) AppAICredits(c *gin.Context) {
	session, ok := h.aiGatewaySession(c)
	if !ok {
		return
	}
	page, err := h.aiGateway.ListMyCredits(c.Request.Context(), session,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// ── 管理端 ──

// AdminAIGatewayCatalog GET /api/admin/apps/:appkey/ai-gateway/catalog
func (h *Handler) AdminAIGatewayCatalog(c *gin.Context) {
	if _, ok := resolveAppID(c, h.app); !ok || !h.aiGatewayReady(c) {
		return
	}
	response.Success(c, http.StatusOK, "ok", h.aiGateway.Catalog())
}

// AdminAIGatewaySettings GET /api/admin/apps/:appkey/ai-gateway/settings
func (h *Handler) AdminAIGatewaySettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	settings, err := h.aiGateway.GetSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminSaveAIGatewaySettings PUT /api/admin/apps/:appkey/ai-gateway/settings
func (h *Handler) AdminSaveAIGatewaySettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req aigateway.Settings
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	_, operator := adminAccount(c)
	settings, err := h.aiGateway.SaveSettings(c.Request.Context(), appID, req, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminAIGatewayModels GET /api/admin/apps/:appkey/ai-gateway/models
func (h *Handler) AdminAIGatewayModels(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	models, err := h.aiGateway.ListModels(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": models})
}

// AdminCreateAIGatewayModel POST /api/admin/apps/:appkey/ai-gateway/models
func (h *Handler) AdminCreateAIGatewayModel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req aigateway.ModelInput
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	model, err := h.aiGateway.CreateModel(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", model)
}

// AdminUpdateAIGatewayModel PUT /api/admin/apps/:appkey/ai-gateway/models/:modelId
func (h *Handler) AdminUpdateAIGatewayModel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	modelID, ok := cardKeyPathID(c, "modelId")
	if !ok {
		return
	}
	var req aigateway.ModelInput
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	model, err := h.aiGateway.UpdateModel(c.Request.Context(), appID, modelID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", model)
}

// AdminDeleteAIGatewayModel DELETE /api/admin/apps/:appkey/ai-gateway/models/:modelId
func (h *Handler) AdminDeleteAIGatewayModel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	modelID, ok := cardKeyPathID(c, "modelId")
	if !ok {
		return
	}
	if err := h.aiGateway.DeleteModel(c.Request.Context(), appID, modelID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"deleted": true})
}

// AdminAIGatewayRequests GET /api/admin/apps/:appkey/ai-gateway/requests
func (h *Handler) AdminAIGatewayRequests(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	query := aiGatewayRequestQuery(c)
	query.AppID = appID
	query.UserID, _ = strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.aiGateway.ListRequests(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminAIGatewayRequest GET /api/admin/apps/:appkey/ai-gateway/requests/:requestNo
func (h *Handler) AdminAIGatewayRequest(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	record, err := h.aiGateway.GetRequest(c.Request.Context(), appID, c.Param("requestNo"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", record)
}

// AdminAIGatewayRequestSummary GET /api/admin/apps/:appkey/ai-gateway/requests/summary?minutes=60
func (h *Handler) AdminAIGatewayRequestSummary(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	summary, err := h.aiGateway.LiveSummary(c.Request.Context(), appID, parsePositiveInt(c.Query("minutes"), 60))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", summary)
}

const (
	aiGatewayLiveHeartbeat = 15 * time.Second
	// aiGatewayLiveMaxAge 一条实时连接最长保持多久；到点由控制台重连（顺带重新校验登录与权限）。
	aiGatewayLiveMaxAge = 30 * time.Minute
)

// AdminAIGatewayRequestsLive GET /api/admin/apps/:appkey/ai-gateway/requests/live
//
// SSE：调用记录的实时事件（created / attempt / progress / finished / resync），见 docs/ai-gateway.md「实时」。
// 先发一条 ready，之后每 15 秒一条注释行保活。
func (h *Handler) AdminAIGatewayRequestsLive(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	h.streamAIGatewayLive(c, appID)
}

func (h *Handler) streamAIGatewayLive(c *gin.Context, appID int64) {
	events, cancel := h.aiGateway.SubscribeLive(appID)
	defer cancel()

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	// 长连接不受服务器的写超时约束。
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Time{})
	c.Status(http.StatusOK)

	write := func(event string, payload any) bool {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}
	if !write("ready", gin.H{"appid": appID, "at": time.Now()}) {
		return
	}
	heartbeat := time.NewTicker(aiGatewayLiveHeartbeat)
	defer heartbeat.Stop()
	deadline := time.NewTimer(aiGatewayLiveMaxAge)
	defer deadline.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			write("bye", gin.H{"reason": "max-age"})
			return
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		case event := <-events:
			if !write(event.Type, event) {
				return
			}
		}
	}
}

// AIGatewayRefundRequest 管理员退款。
type AIGatewayRefundRequest struct {
	Reason string `json:"reason"`
}

// AdminRefundAIGatewayRequest POST /api/admin/apps/:appkey/ai-gateway/requests/:requestNo/refund
func (h *Handler) AdminRefundAIGatewayRequest(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	var req AIGatewayRefundRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	_, operator := adminAccount(c)
	record, err := h.aiGateway.RefundRequest(c.Request.Context(), aigateway.RefundInput{
		AppID: appID, RequestNo: c.Param("requestNo"), Reason: req.Reason, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", record)
}

// AdminAIGatewayKeys GET /api/admin/apps/:appkey/ai-gateway/keys
func (h *Handler) AdminAIGatewayKeys(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	page, err := h.aiGateway.ListKeys(c.Request.Context(), appID, userID, strings.TrimSpace(c.Query("status")),
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AIGatewayKeyStatusRequest 管理端改密钥状态。
type AIGatewayKeyStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// AdminSetAIGatewayKeyStatus PUT /api/admin/apps/:appkey/ai-gateway/keys/:keyId/status
func (h *Handler) AdminSetAIGatewayKeyStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	keyID, ok := cardKeyPathID(c, "keyId")
	if !ok {
		return
	}
	var req AIGatewayKeyStatusRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	key, err := h.aiGateway.SetKeyStatus(c.Request.Context(), appID, keyID, strings.TrimSpace(req.Status))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", key)
}

// AdminAIGatewayStats GET /api/admin/apps/:appkey/ai-gateway/stats?days=30&tz=Asia/Shanghai
func (h *Handler) AdminAIGatewayStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	stats, err := h.aiGateway.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 30), c.Query("tz"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminAIGatewayUserAccount GET /api/admin/apps/:appkey/ai-gateway/users/:userId/account
func (h *Handler) AdminAIGatewayUserAccount(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	overview, err := h.aiGateway.UserOverview(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", overview)
}

// AdminAIGatewayUserCredits GET /api/admin/apps/:appkey/ai-gateway/users/:userId/credits
func (h *Handler) AdminAIGatewayUserCredits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	page, err := h.aiGateway.ListCredits(c.Request.Context(), appID, userID,
		parsePositiveInt(c.Query("page"), 1), parsePositiveInt(c.Query("limit"), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AIGatewayCreditAdjustRequest 管理员增减赠送额度（点，正负皆可）。
type AIGatewayCreditAdjustRequest struct {
	Amount    decimal.Decimal `json:"amount"`
	Reason    string          `json:"reason"`
	RequestID string          `json:"requestId"`
}

// AdminAdjustAIGatewayCredit POST /api/admin/apps/:appkey/ai-gateway/users/:userId/credits
func (h *Handler) AdminAdjustAIGatewayCredit(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.aiGatewayReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req AIGatewayCreditAdjustRequest
	if err := bind(c, &req); err != nil {
		h.writeError(c, err)
		return
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	_, operator := adminAccount(c)
	result, err := h.aiGateway.AdjustCredit(c.Request.Context(), aigateway.CreditAdjustInput{
		AppID: appID, UserID: userID, Amount: req.Amount, Reason: req.Reason, RequestID: requestID, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}
