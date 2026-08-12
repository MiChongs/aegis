package httptransport

import (
	"net/http"
	"strings"

	vipdomain "aegis/internal/domain/vip"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// 服务端会员校验 + 会员功能标识目录。
//
// 校验口刻意**不在** `/api/v1/apps/{appKey}` 网关命名空间下：那条命名空间是给
// 客户端的，整条链路围绕「用户令牌 + 三档包装」设计。服务端校验的调用方是
// 接入方自己的后端，它持有的是应用级服务端密钥，没有用户令牌、也不该为了
// 问一句话去实现签名与加密。因此它与远程函数调用同处 `/api/apps/{appKey}/*`：
// 同一个命名空间、同一种凭据、同一种风格。

// VipVerifyRequest 服务端校验请求。
//
// **只接受用户访问令牌，不接受 userId / account。** 这是这套接口唯一守得住的边界：
// 接入方的后端几乎一定会把「当前请求是谁」交给它自己的客户端来说，一旦这里收
// userId，攻击者只要知道任意一个会员的 userId 就能白嫖 —— 而服务端密钥拦不住，
// 因为犯错的正是持有密钥的那一方。
//
// 需要按 userId 批量查（对账、到期提醒、客服）走管理端
// `/api/admin/apps/{appKey}/vip/entitlement`，那条路有管理员鉴权与审计。
type VipVerifyRequest struct {
	// AccessToken 用户的访问令牌：它同时证明「是谁」与「这个人现在在场」
	AccessToken string `json:"accessToken" binding:"required"`
	// Feature 要校验的功能标识，留空即只校验"是不是会员"（通用档）
	Feature string `json:"feature"`
}

// AdminVipFeatureRequest 功能标识的新建 / 更新
type AdminVipFeatureRequest struct {
	Tag         string  `json:"tag" binding:"required"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	IsActive    *bool   `json:"isActive"`
	SortOrder   *int    `json:"sortOrder"`
}

// VerifyVipMembership POST /api/apps/:appkey/vip/verify
//
// 鉴权走 `X-Aegis-Function-Key`（应用服务端密钥，控制台可签发与撤销），
// 与远程函数调用同一把钥匙 —— 再造一套"会员校验专用密钥"只会让接入方
// 在服务器上配两份凭据，而它们的信任级别完全一样。
func (h *Handler) VerifyVipMembership(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	secret := strings.TrimSpace(c.GetHeader("X-Aegis-Function-Key"))
	if secret == "" {
		response.Error(c, http.StatusUnauthorized, 40100,
			"缺少应用服务端密钥，请在 X-Aegis-Function-Key 头里带上")
		return
	}
	if _, err := h.appFunction.AuthenticateKey(c.Request.Context(), appID, secret); err != nil {
		h.writeError(c, err)
		return
	}

	var req VipVerifyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	// 身份来自令牌本身，不来自请求体里的任何字段
	session, err := h.auth.ValidateAccessToken(c.Request.Context(), strings.TrimSpace(req.AccessToken))
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 40100, "用户令牌无效或已过期")
		return
	}
	result, err := h.vip.VerifyMembershipByToken(c.Request.Context(), appID, session, req.Feature)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "校验完成", result)
}

// ── 管理端：会员功能标识目录 ──

// AdminAppVipFeatures GET /api/admin/apps/:appkey/vip/features
func (h *Handler) AdminAppVipFeatures(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.vip.AdminListFeatures(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取功能目录成功", items)
}

// AdminSaveAppVipFeature POST /api/admin/apps/:appkey/vip/features
func (h *Handler) AdminSaveAppVipFeature(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminVipFeatureRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	feature, err := h.vip.AdminSaveFeature(c.Request.Context(), vipdomain.FeatureMutation{
		AppID:       appID,
		Tag:         req.Tag,
		Name:        req.Name,
		Description: req.Description,
		IsActive:    req.IsActive,
		SortOrder:   req.SortOrder,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存功能标识成功", feature)
}

// AdminDeleteAppVipFeature DELETE /api/admin/apps/:appkey/vip/features/:tag
func (h *Handler) AdminDeleteAppVipFeature(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	affectedPlans, err := h.vip.AdminDeleteFeature(c.Request.Context(), appID, c.Param("tag"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 一并告诉调用方有几个套餐刚刚失去了这一项：删除本身成功了，
	// 但那几个套餐从此不再发放这个权益，这件事必须说出来。
	response.Success(c, 200, "删除功能标识成功", gin.H{
		"deleted": true, "affectedPlans": affectedPlans,
	})
}
