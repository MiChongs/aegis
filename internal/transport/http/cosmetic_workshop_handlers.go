package httptransport

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/cosmetic"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/response"
)

// 自定义工坊（docs/cosmetics.md「自定义工坊」）：用户端作品增删改，管理端零件库与作品巡查。

// WorkshopItemRequest 新建 / 修改作品；slot 只在新建时生效。
type WorkshopItemRequest struct {
	Slot   string          `json:"slot"`
	Name   string          `json:"name" binding:"required"`
	Design cosmetic.Design `json:"design"`
}

// WorkshopTakedownRequest 下架作品；reason 作者会看到。
type WorkshopTakedownRequest struct {
	Reason string `json:"reason"`
}

// ── 用户端 ──

// AppCosmeticWorkshop 工坊首页：额度、编辑器上界、零件、起点模板、我的作品。
func (h *Handler) AppCosmeticWorkshop(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	result, err := h.cosmetic.Workshop(c.Request.Context(), session.AppID, session.UserID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AppCreateWorkshopItem 新建作品。
func (h *Handler) AppCreateWorkshopItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	var req WorkshopItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cosmetic.CreateWorkshopItem(c.Request.Context(), session.AppID, session.UserID,
		cosmetic.WorkshopInput{Slot: req.Slot, Name: req.Name, Design: req.Design}, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "作品已保存", item)
}

// AppUpdateWorkshopItem 修改作品。
func (h *Handler) AppUpdateWorkshopItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req WorkshopItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cosmetic.UpdateWorkshopItem(c.Request.Context(), session.AppID, session.UserID, id, req.Name, req.Design,
		requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "作品已保存", item)
}

// AppDeleteWorkshopItem 删除作品。
func (h *Handler) AppDeleteWorkshopItem(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	if err := h.cosmetic.DeleteWorkshopItem(c.Request.Context(), session.AppID, session.UserID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// ── 管理端 ──

// AdminWorkshopParts 零件库（含停用的）。
func (h *Handler) AdminWorkshopParts(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	parts, err := h.cosmetic.AdminWorkshopParts(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"items": parts})
}

// AdminCreateWorkshopPart 登记零件。
func (h *Handler) AdminCreateWorkshopPart(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	var req cosmetic.PartInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	part, err := h.cosmetic.CreateWorkshopPart(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "零件已登记", part)
}

// AdminUpdateWorkshopPart 修改零件。
func (h *Handler) AdminUpdateWorkshopPart(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "partId")
	if !ok {
		return
	}
	var req cosmetic.PartInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	part, err := h.cosmetic.UpdateWorkshopPart(c.Request.Context(), appID, id, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "零件已保存", part)
}

// AdminDeleteWorkshopPart 删零件。
func (h *Handler) AdminDeleteWorkshopPart(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "partId")
	if !ok {
		return
	}
	if err := h.cosmetic.DeleteWorkshopPart(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// AdminWorkshopItems 作品列表（userId / slot / status / keyword / page / limit）。
func (h *Handler) AdminWorkshopItems(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	limit := queryInt(c, "limit", 20)
	page := max(queryInt(c, "page", 1), 1)
	result, err := h.cosmetic.AdminWorkshopItems(c.Request.Context(), pgrepo.WorkshopItemFilter{
		AppID: appID, UserID: queryInt64(c, "userId"), Slot: strings.TrimSpace(c.Query("slot")),
		Status: strings.TrimSpace(c.Query("status")), Keyword: strings.TrimSpace(c.Query("keyword")),
		Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminTakedownWorkshopItem 下架作品（原因作者可见）。
func (h *Handler) AdminTakedownWorkshopItem(c *gin.Context) {
	h.setWorkshopItemStatus(c, true)
}

// AdminRestoreWorkshopItem 恢复被下架的作品。
func (h *Handler) AdminRestoreWorkshopItem(c *gin.Context) {
	h.setWorkshopItemStatus(c, false)
}

func (h *Handler) setWorkshopItemStatus(c *gin.Context, takedown bool) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req WorkshopTakedownRequest
	if takedown {
		if err := bind(c, &req); err != nil {
			response.Error(c, http.StatusBadRequest, 40000, err.Error())
			return
		}
	}
	item, err := h.cosmetic.SetWorkshopItemStatus(c.Request.Context(), appID, id, takedown, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "作品已恢复"
	if takedown {
		message = "作品已下架"
	}
	response.Success(c, http.StatusOK, message, item)
}
