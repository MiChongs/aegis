package httptransport

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	rewardedad "aegis/internal/domain/rewardedad"
	"aegis/internal/service"
	"aegis/pkg/response"
	"aegis/pkg/timeutil"
)

// 激励广告：管理端（配置 / 记录 / 统计 / 补发）、用户端（状态 / 上报 / 我的记录）
// 与广告平台的服务端回调。

// ── DTO ──

// RewardedAdSceneRequest 一个奖励场景。权益沿用卡密的权益形态。
type RewardedAdSceneRequest struct {
	Key             string                 `json:"key"`
	Name            string                 `json:"name"`
	PlacementID     string                 `json:"placementId"`
	Enabled         bool                   `json:"enabled"`
	Rewards         []CardKeyRewardRequest `json:"rewards"`
	DailyLimit      int                    `json:"dailyLimit"`
	CooldownSeconds int                    `json:"cooldownSeconds"`
}

// SaveRewardedAdConfigRequest 保存激励广告配置。securityKey 留空表示不修改。
type SaveRewardedAdConfigRequest struct {
	Enabled          bool                     `json:"enabled"`
	Provider         string                   `json:"provider"`
	ProviderAppID    string                   `json:"providerAppId"`
	SecurityKey      string                   `json:"securityKey"`
	ClearSecurityKey bool                     `json:"clearSecurityKey"`
	VerifyMode       string                   `json:"verifyMode"`
	DailyLimit       int                      `json:"dailyLimit"`
	Scenes           []RewardedAdSceneRequest `json:"scenes"`
}

// GrantRewardedAdViewRequest 管理端补发。scene 留空时沿用记录上的场景。
type GrantRewardedAdViewRequest struct {
	Scene string `json:"scene"`
}

// RewardedAdClaimRequest 客户端上报一次看完的激励广告。
//
// transId 是 SDK 在 onVideoRewarded 里给的那一个；同一个 transId 重复上报是安全的，
// 用来轮询「还在等平台回调」的结果。verified / errorCode 是 SDK 自己的奖励校验结论，
// 只在 client 校验模式下作数，其余模式只留档。
type RewardedAdClaimRequest struct {
	Scene       string `json:"scene" binding:"required"`
	TransID     string `json:"transId" binding:"required"`
	PlacementID string `json:"placementId"`
	Verified    *bool  `json:"verified"`
	ErrorCode   string `json:"errorCode"`
	DeviceID    string `json:"deviceId"`
}

func (req SaveRewardedAdConfigRequest) toInput(appID int64, operator string) rewardedad.SaveConfigInput {
	scenes := make([]rewardedad.Scene, 0, len(req.Scenes))
	for _, item := range req.Scenes {
		rewards := make([]rewardedad.Reward, 0, len(item.Rewards))
		for _, reward := range item.Rewards {
			rewards = append(rewards, reward.toDomain())
		}
		scenes = append(scenes, rewardedad.Scene{
			Key:             item.Key,
			Name:            item.Name,
			PlacementID:     item.PlacementID,
			Enabled:         item.Enabled,
			Rewards:         rewards,
			DailyLimit:      item.DailyLimit,
			CooldownSeconds: item.CooldownSeconds,
		})
	}
	return rewardedad.SaveConfigInput{
		AppID:            appID,
		Enabled:          req.Enabled,
		Provider:         req.Provider,
		ProviderAppID:    req.ProviderAppID,
		SecurityKey:      req.SecurityKey,
		ClearSecurityKey: req.ClearSecurityKey,
		VerifyMode:       req.VerifyMode,
		DailyLimit:       req.DailyLimit,
		Scenes:           scenes,
		Operator:         operator,
	}
}

// ── 管理端 ──

// AdminRewardedAdConfig 读取配置（含权益目录与回调地址）。
func (h *Handler) AdminRewardedAdConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cfg, err := h.rewardedAd.AdminConfig(c.Request.Context(), appID, strings.TrimSpace(c.Param("appkey")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", cfg)
}

// AdminSaveRewardedAdConfig 保存配置。
func (h *Handler) AdminSaveRewardedAdConfig(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req SaveRewardedAdConfigRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	cfg, err := h.rewardedAd.SaveConfig(c.Request.Context(), req.toInput(appID, operator),
		strings.TrimSpace(c.Param("appkey")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "激励广告配置已保存", cfg)
}

// AdminListRewardedAdViews 观看记录。
func (h *Handler) AdminListRewardedAdViews(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, _ := strconv.ParseInt(c.Query("userId"), 10, 64)
	query := rewardedad.ViewQuery{
		AppID:   appID,
		UserID:  userID,
		Status:  strings.TrimSpace(c.Query("status")),
		Scene:   strings.TrimSpace(c.Query("scene")),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Page:    parsePositiveInt(c.Query("page"), 1),
		Limit:   parsePositiveInt(c.Query("limit"), 20),
	}
	query.Start, query.End = parseRewardedAdRange(c.Query("start"), c.Query("end"))
	page, err := h.rewardedAd.ListViews(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// AdminRewardedAdStats 概览与趋势。
func (h *Handler) AdminRewardedAdStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.rewardedAd.Stats(c.Request.Context(), appID, parsePositiveInt(c.Query("days"), 14))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminGrantRewardedAdView 补发一条未发放的观看记录。
func (h *Handler) AdminGrantRewardedAdView(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	viewID, err := pathInt64(c, "viewId")
	if err != nil || viewID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "viewId 无效")
		return
	}
	var req GrantRewardedAdViewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	view, err := h.rewardedAd.GrantView(c.Request.Context(), appID, viewID, req.Scene, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已补发", view)
}

// ── 用户端 ──

// AppRewardedAdStatus 激励广告状态：开没开、每个场景还能看几次、传给 SDK 的用户标识。
func (h *Handler) AppRewardedAdStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	status, err := h.rewardedAd.ClientStatus(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", status)
}

// AppClaimRewardedAd 上报一次看完的激励广告（重复上报同一个 transId 即轮询结果）。
func (h *Handler) AppClaimRewardedAd(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req RewardedAdClaimRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deviceID, _ := h.resolveGatewayDevice(c, req.DeviceID, "")
	result, err := h.rewardedAd.Claim(c.Request.Context(), service.RewardedAdClaimInput{
		AppID:       session.AppID,
		UserID:      session.UserID,
		Scene:       req.Scene,
		TransID:     req.TransID,
		PlacementID: req.PlacementID,
		Verified:    req.Verified,
		ErrorCode:   req.ErrorCode,
		DeviceID:    deviceID,
		ClientIP:    c.ClientIP(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, result.Message, result)
}

// AppRewardedAdRecords 我的观看记录。
func (h *Handler) AppRewardedAdRecords(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var query PaginationQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	page, err := h.rewardedAd.MyViews(c.Request.Context(), session.AppID, session.UserID,
		normalizePage(query.Page), parsePositiveInt(strconv.Itoa(query.Limit), 20))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", page)
}

// ── 广告平台回调 ──

// RewardedAdHuijingCallback 灰鲸激励视频的服务端回调（GET）。
//
// 由灰鲸服务器发起，没有用户令牌也没有应用密钥可带，凭 sha256(securityKey:transId) 验签。
// 响应体是平台规定的 {"isValid": bool}，不套本项目的信封 —— 与支付回调回 "success" 同理，
// 对方只认它自己的格式。参数名大小写两种写法都认（后台模板是 camelCase，宏名是 SNAKE）。
func (h *Handler) RewardedAdHuijingCallback(c *gin.Context) {
	app, err := h.app.GetAppByKey(c.Request.Context(), strings.TrimSpace(c.Param("appkey")))
	if err != nil || app == nil {
		c.JSON(http.StatusOK, gin.H{"isValid": false})
		return
	}
	amount, _ := strconv.Atoi(firstQuery(c, "rewardAmount", "reward_amount", "REWARD_AMOUNT"))
	valid, _ := h.rewardedAd.HuijingCallback(c.Request.Context(), service.HuijingCallbackInput{
		AppID:        app.ID,
		UserID:       firstQuery(c, "userId", "user_id", "USER_ID"),
		TransID:      firstQuery(c, "transId", "trans_id", "TRANS_ID"),
		Sign:         firstQuery(c, "sign", "SIGN"),
		PlacementID:  firstQuery(c, "placementId", "placement_id", "PLACEMENT_ID"),
		RewardName:   firstQuery(c, "rewardName", "reward_name", "REWARD_NAME"),
		RewardAmount: amount,
		NetworkID:    firstQuery(c, "networkId", "network_id", "NETWORK_ID"),
		Extra:        firstQuery(c, "extrainfo", "extraInfo", "extra", "EXTRAINFO"),
	})
	c.JSON(http.StatusOK, gin.H{"isValid": valid})
}

// parseRewardedAdRange 解析记录筛选的时间范围。
//
// 控制台的范围选择器给的是 RFC3339（已经是本地自然日的起止）；手写调用常给 YYYY-MM-DD，
// 按默认时区的自然日解释：「10 月 5 日」指那一天的零点到次日零点。两种都认。
func parseRewardedAdRange(rawStart, rawEnd string) (*time.Time, *time.Time) {
	loc := timeutil.DefaultLocation()
	parse := func(raw string, endOfDay bool) *time.Time {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		if value, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return &value
		}
		if value, err := time.ParseInLocation("2006-01-02", raw, loc); err == nil {
			if endOfDay {
				value = value.AddDate(0, 0, 1)
			}
			return &value
		}
		return nil
	}
	return parse(rawStart, false), parse(rawEnd, true)
}

// firstQuery 依次取几个候选参数名，返回第一个非空的值。
func firstQuery(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			return value
		}
	}
	return ""
}
