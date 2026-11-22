package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"aegis/internal/domain/cosmetic"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"
	"aegis/pkg/response"
)

// 个性装扮（docs/cosmetics.md）：管理端（目录 / 设置 / 素材 / 装扮与设计器 / 版本 / 用户 / 持有记录 / 统计）
// 与用户端（装扮中心 / 我的佩戴 / 佩戴 / 持有记录 / 批量查别人身上的装扮）。

// ── DTO ──

// CosmeticUnlockRequest 获取方式。
type CosmeticUnlockRequest struct {
	Mode     string  `json:"mode"`
	TierIDs  []int64 `json:"tierIds"`
	MinLevel int     `json:"minLevel"`
	Feature  string  `json:"feature"`
	Hint     string  `json:"hint"`
}

// CosmeticItemRequest 新建 / 更新装扮的基本信息。slot 只在新建时生效；template 新建时套用的模板键。
type CosmeticItemRequest struct {
	Slot        string                `json:"slot"`
	Code        string                `json:"code" binding:"required"`
	Name        string                `json:"name" binding:"required"`
	Description string                `json:"description"`
	Rarity      string                `json:"rarity"`
	Series      string                `json:"series"`
	Unlock      CosmeticUnlockRequest `json:"unlock"`
	SortOrder   int                   `json:"sortOrder"`
	Hidden      bool                  `json:"hidden"`
	Template    string                `json:"template"`
}

func (req CosmeticItemRequest) toInput(appID int64, operator string) cosmetic.ItemInput {
	return cosmetic.ItemInput{
		AppID: appID, Slot: req.Slot, Code: req.Code, Name: req.Name, Description: req.Description, Rarity: req.Rarity,
		Series: req.Series, SortOrder: req.SortOrder, Hidden: req.Hidden, Operator: operator,
		Unlock: cosmetic.Unlock{Mode: req.Unlock.Mode, TierIDs: req.Unlock.TierIDs, MinLevel: req.Unlock.MinLevel,
			Feature: req.Unlock.Feature, Hint: req.Unlock.Hint},
	}
}

// CosmeticDraftRequest 保存设计器草稿：design 是整份设计稿（docs/cosmetics.md「设计稿」）。
type CosmeticDraftRequest struct {
	Design cosmetic.Design `json:"design" binding:"required"`
}

// CosmeticPublishRequest 发布；note 是这次发布的说明（版本历史里显示）。
type CosmeticPublishRequest struct {
	Note string `json:"note"`
}

// CosmeticStatusRequest 上架 / 下架 / 回到草稿：active / archived / draft。
type CosmeticStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// CosmeticOrderRequest 按数组顺序重写排序值。
type CosmeticOrderRequest struct {
	ItemIDs []int64 `json:"itemIds" binding:"required"`
}

// CosmeticSettingsRequest 应用设置。
type CosmeticSettingsRequest struct {
	Enabled    bool `json:"enabled"`
	BadgeSlots int  `json:"badgeSlots"`
	// Workshop 自定义工坊设置；不传时保持原样（老版控制台只改开关与勋章位）
	Workshop *cosmetic.WorkshopSettings `json:"workshop"`
}

// CosmeticGrantRequest 给用户发一件装扮；days 为 0 即永久。
type CosmeticGrantRequest struct {
	CosmeticID int64 `json:"cosmeticId" binding:"required"`
	Days       int   `json:"days"`
}

// CosmeticRevokeRequest 收回一条持有。
type CosmeticRevokeRequest struct {
	Reason string `json:"reason"`
}

// CosmeticUnequipRequest 管理员摘下用户的某个槽位；position 为 -1 摘下整个槽位。
type CosmeticUnequipRequest struct {
	Slot     string `json:"slot" binding:"required"`
	Position int    `json:"position"`
}

// CosmeticEquipRequest 用户佩戴；cosmeticId 为 0 即摘下。勋章用 position 指定第几位（0 起；-1 且 cosmeticId 为 0 摘下全部）。
type CosmeticEquipRequest struct {
	Slot       string `json:"slot" binding:"required"`
	Position   int    `json:"position"`
	CosmeticID int64  `json:"cosmeticId"`
}

func (h *Handler) cosmeticReady(c *gin.Context) bool {
	if h.cosmetic == nil {
		response.Error(c, http.StatusServiceUnavailable, 50391, "个性装扮服务未启用")
		return false
	}
	return true
}

func queryInt64(c *gin.Context, name string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(c.Query(name)), 10, 64)
	return v
}

// ── 管理端：目录与设置 ──

// AdminCosmeticCatalog 槽位、稀有度、获取方式、来源、设计稿规格、模板、会员档位与功能。
func (h *Handler) AdminCosmeticCatalog(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	catalog, err := h.cosmetic.Catalog(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", catalog)
}

// AdminCosmeticSettings 应用设置。
func (h *Handler) AdminCosmeticSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	settings, err := h.cosmetic.Settings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", settings)
}

// AdminSaveCosmeticSettings 保存应用设置。
func (h *Handler) AdminSaveCosmeticSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	var req CosmeticSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	next := cosmetic.Settings{AppID: appID, Enabled: req.Enabled, BadgeSlots: req.BadgeSlots, UpdatedBy: operator}
	if req.Workshop != nil {
		next.Workshop = *req.Workshop
	} else {
		current, err := h.cosmetic.Settings(c.Request.Context(), appID)
		if err != nil {
			h.writeError(c, err)
			return
		}
		next.Workshop = current.Workshop
	}
	settings, err := h.cosmetic.SaveSettings(c.Request.Context(), next)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "个性装扮已关闭"
	if settings.Enabled {
		message = "个性装扮已开启"
	}
	response.Success(c, http.StatusOK, message, settings)
}

// ── 管理端：素材 ──

// AdminListCosmeticAssets 素材库（kind / keyword / page / limit）。
func (h *Handler) AdminListCosmeticAssets(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	limit := queryInt(c, "limit", 60)
	page := max(queryInt(c, "page", 1), 1)
	result, err := h.cosmetic.ListAssets(c.Request.Context(), pgrepo.CosmeticAssetFilter{
		AppID: appID, Kind: strings.TrimSpace(c.Query("kind")), Keyword: c.Query("keyword"), Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminUploadCosmeticAsset 上传素材（multipart：file，可带 name、kind=image|sprite、config_name）。
func (h *Handler) AdminUploadCosmeticAsset(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()
	_, operator := adminAccount(c)
	asset, err := h.cosmetic.UploadAsset(c.Request.Context(), service.AssetUploadInput{
		AppID: appID, ConfigName: c.PostForm("config_name"), Name: c.PostForm("name"), FileName: file.Filename,
		ContentType: file.Header.Get("Content-Type"), Kind: c.PostForm("kind"), Content: opened,
		UploadedBy: currentAdminID(c), Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", asset)
}

// AdminDeleteCosmeticAsset 删除没被引用的素材。
func (h *Handler) AdminDeleteCosmeticAsset(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	assetID, ok := cardKeyPathID(c, "assetId")
	if !ok {
		return
	}
	if err := h.cosmetic.DeleteAsset(c.Request.Context(), appID, assetID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// ── 管理端：装扮 ──

// AdminListCosmeticItems 装扮列表（slot / status / rarity / keyword），不带设计稿。
func (h *Handler) AdminListCosmeticItems(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	items, err := h.cosmetic.ListItems(c.Request.Context(), pgrepo.CosmeticItemFilter{
		AppID: appID, Slot: strings.TrimSpace(c.Query("slot")), Status: strings.TrimSpace(c.Query("status")),
		Rarity: strings.TrimSpace(c.Query("rarity")), Keyword: c.Query("keyword"),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminGetCosmeticItem 单件（含草稿与已发布的设计稿）。
func (h *Handler) AdminGetCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	item, err := h.cosmetic.GetItem(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", item)
}

// AdminCreateCosmeticItem 新建装扮（可套用模板）。
func (h *Handler) AdminCreateCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	var req CosmeticItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.cosmetic.CreateItem(c.Request.Context(), req.toInput(appID, operator), req.Template)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "装扮已创建", item)
}

// AdminUpdateCosmeticItem 更新基本信息（槽位不能改）。
func (h *Handler) AdminUpdateCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req CosmeticItemRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.cosmetic.UpdateItem(c.Request.Context(), id, req.toInput(appID, operator))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已保存", item)
}

// AdminSaveCosmeticDraft 保存设计器草稿（整份校验）。
func (h *Handler) AdminSaveCosmeticDraft(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req CosmeticDraftRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cosmetic.SaveDraft(c.Request.Context(), appID, id, req.Design)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "草稿已保存", item)
}

// AdminPublishCosmeticItem 发布草稿为新版本。
func (h *Handler) AdminPublishCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req CosmeticPublishRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	item, err := h.cosmetic.Publish(c.Request.Context(), appID, id, req.Note, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已发布第 "+strconv.Itoa(item.Revision)+" 版", item)
}

// AdminUploadCosmeticPreview 上传设计器渲染的静态预览图（multipart：file）。
func (h *Handler) AdminUploadCosmeticPreview(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "缺少上传文件")
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取上传文件失败")
		return
	}
	defer opened.Close()
	link, err := h.cosmetic.UploadPreview(c.Request.Context(), appID, id, service.AssetUploadInput{
		AppID: appID, ConfigName: c.PostForm("config_name"), FileName: file.Filename, Content: opened,
		UploadedBy: currentAdminID(c),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "预览图已更新", gin.H{"preview": link})
}

// AdminCosmeticRevisions 发布历史。
func (h *Handler) AdminCosmeticRevisions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	items, err := h.cosmetic.Revisions(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", items)
}

// AdminRestoreCosmeticRevision 把历史版本放回草稿。
func (h *Handler) AdminRestoreCosmeticRevision(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	revision, ok := cardKeyPathID(c, "revision")
	if !ok {
		return
	}
	item, err := h.cosmetic.RestoreRevision(c.Request.Context(), appID, id, int(revision))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已把第 "+strconv.FormatInt(revision, 10)+" 版放回草稿", item)
}

// AdminSetCosmeticStatus 上架 / 下架 / 回到草稿。
func (h *Handler) AdminSetCosmeticStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	var req CosmeticStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.cosmetic.SetStatus(c.Request.Context(), appID, id, strings.TrimSpace(req.Status))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "状态已更新", item)
}

// AdminDuplicateCosmeticItem 复制一件装扮。
func (h *Handler) AdminDuplicateCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	_, operator := adminAccount(c)
	item, err := h.cosmetic.Duplicate(c.Request.Context(), appID, id, operator)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已复制", item)
}

// AdminReorderCosmeticItems 重排。
func (h *Handler) AdminReorderCosmeticItems(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	var req CosmeticOrderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	updated, err := h.cosmetic.Reorder(c.Request.Context(), appID, req.ItemIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "排序已保存", gin.H{"updated": updated})
}

// AdminDeleteCosmeticItem 删除（有人持有过的不能删）。
func (h *Handler) AdminDeleteCosmeticItem(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	id, ok := cardKeyPathID(c, "itemId")
	if !ok {
		return
	}
	if err := h.cosmetic.DeleteItem(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已删除", gin.H{"deleted": true})
}

// ── 管理端：用户与记录 ──

// AdminCosmeticOwnerships 持有记录（userId / cosmeticId / source / state / page / limit）。
func (h *Handler) AdminCosmeticOwnerships(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	limit := queryInt(c, "limit", 50)
	page := max(queryInt(c, "page", 1), 1)
	result, err := h.cosmetic.Ownerships(c.Request.Context(), pgrepo.CosmeticOwnershipFilter{
		AppID: appID, UserID: queryInt64(c, "userId"), CosmeticID: queryInt64(c, "cosmeticId"),
		Source: strings.TrimSpace(c.Query("source")), State: strings.TrimSpace(c.Query("state")),
		Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminCosmeticStats 统计。
func (h *Handler) AdminCosmeticStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	stats, err := h.cosmetic.Stats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", stats)
}

// AdminUserCosmetics 用户详情的装扮页：持有、佩戴（各自是否生效）、此刻身上的装扮与判定事实。
func (h *Handler) AdminUserCosmetics(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	result, err := h.cosmetic.UserProfile(c.Request.Context(), appID, userID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AdminGrantUserCosmetic 给用户发一件装扮。
func (h *Handler) AdminGrantUserCosmetic(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req CosmeticGrantRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	result, err := h.cosmetic.Grant(c.Request.Context(), cosmetic.GrantInput{
		AppID: appID, UserID: userID, CosmeticID: req.CosmeticID, Days: req.Days, Operator: operator,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已发放"
	if result.AlreadyPermanent {
		message = "该用户已永久持有，未重复发放"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AdminRevokeUserCosmetic 收回一条持有。
func (h *Handler) AdminRevokeUserCosmetic(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	ownershipID, ok := cardKeyPathID(c, "ownershipId")
	if !ok {
		return
	}
	var req CosmeticRevokeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	_, operator := adminAccount(c)
	own, err := h.cosmetic.Revoke(c.Request.Context(), appID, userID, ownershipID, operator, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已收回", own)
}

// AdminUnequipUserCosmetic 摘下用户的某个槽位。
func (h *Handler) AdminUnequipUserCosmetic(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok || !h.cosmeticReady(c) {
		return
	}
	userID, ok := cardKeyPathID(c, "userId")
	if !ok {
		return
	}
	var req CosmeticUnequipRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.cosmetic.AdminUnequip(c.Request.Context(), appID, userID, req.Slot, req.Position); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已摘下", gin.H{"unequipped": true})
}

// ── 用户端 ──

// AppCosmeticCenter 装扮中心。
func (h *Handler) AppCosmeticCenter(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	center, err := h.cosmetic.Center(c.Request.Context(), session.AppID, session.UserID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", center)
}

// AppMyCosmetics 我此刻身上的装扮（应用未开启时为空）。
func (h *Handler) AppMyCosmetics(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	loadout, err := h.cosmetic.MyLoadout(c.Request.Context(), session.AppID, session.UserID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", loadout)
}

// AppEquipCosmetic 佩戴 / 摘下，返回新的身上装扮。
func (h *Handler) AppEquipCosmetic(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	var req CosmeticEquipRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	loadout, err := h.cosmetic.Equip(c.Request.Context(), service.EquipInput{
		AppID: session.AppID, UserID: session.UserID, Slot: strings.TrimSpace(req.Slot), Position: req.Position,
		CosmeticID: req.CosmeticID, BaseURL: requestBaseURL(c.Request),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "已佩戴"
	if req.CosmeticID == 0 {
		message = "已摘下"
	}
	response.Success(c, http.StatusOK, message, loadout)
}

// AppCosmeticHistory 我的持有记录（page / limit）。
func (h *Handler) AppCosmeticHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	limit := queryInt(c, "limit", 30)
	page := max(queryInt(c, "page", 1), 1)
	result, err := h.cosmetic.History(c.Request.Context(), session.AppID, session.UserID, limit, (page-1)*limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", result)
}

// AppLookupCosmetics 批量查别人身上的装扮：?ids=对外用户 ID，逗号分隔，最多 100 个；没戴任何东西的人不出现在结果里。
func (h *Handler) AppLookupCosmetics(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if !h.cosmeticReady(c) {
		return
	}
	ids := make([]int64, 0, 16)
	for _, part := range strings.Split(c.Query("ids"), ",") {
		if v, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && v > 0 {
			ids = append(ids, v)
		}
	}
	result, err := h.cosmetic.Lookup(c.Request.Context(), session.AppID, ids, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "ok", gin.H{"users": result})
}
