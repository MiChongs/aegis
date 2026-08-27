package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	aidomain "aegis/internal/domain/ai"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AI 供应商与 Agent 配置管理面。
//
// 两套作用域共用同一批服务层方法，只在「appid 从哪来」上不同：
//   - 应用级：`/api/admin/apps/:appkey/ai/*`，appid 由路径上的 appkey 解析；
//   - 平台级：`/api/admin/system/ai/*`，appid 恒为 aidomain.PlatformAppID。
//
// 与邮件通道同一条戒律：平台级不接受请求体里的 appid ——
// 接受它就得防「传了别的 appid 会怎样」，那正是最容易漏掉的一处越权。

// ── 请求体 ──

type adminAIConfigSaveRequest struct {
	Name     *string `json:"name"`
	Provider *string `json:"provider"`
	Enabled  *bool   `json:"enabled"`
	// IsDefault 该作用域的首选通道。
	IsDefault *bool `json:"isDefault"`
	// Shared 仅平台级生效：允许没有自有配置的应用回落到这条通道。
	Shared      *bool             `json:"shared"`
	Priority    *int              `json:"priority"`
	Description *string           `json:"description"`
	Settings    map[string]string `json:"settings"`
	// Secrets 密钥明文；留空的键不修改，要清除请用 clearSecrets。
	Secrets         map[string]string `json:"secrets"`
	ClearSecrets    []string          `json:"clearSecrets"`
	ReplaceSettings bool              `json:"replaceSettings"`
}

type adminAIConfigTestRequest struct {
	// Model 用哪个型号测试；缺省用配置的默认型号。
	Model string `json:"model"`
}

type adminAISkillSaveRequest struct {
	Key         *string `json:"key"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Enabled     *bool   `json:"enabled"`
}

type adminAIMCPServerSaveRequest struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	Enabled     *bool   `json:"enabled"`
	Description *string `json:"description"`
	// Headers 鉴权请求头（整体加密存放）；nil 不修改，要清除请用 clearHeaders。
	Headers      map[string]string `json:"headers"`
	ClearHeaders bool              `json:"clearHeaders"`
}

// ── 供应商目录（静态，两个作用域共用） ──

// AIProviderCatalog 下发全部 AI 供应商的自述与技能/能力目录。
// 静态目录，不含任何租户数据与凭据，控制台据此渲染供应商卡片与配置表单。
func (h *Handler) AIProviderCatalog(c *gin.Context) {
	response.Success(c, 200, "获取成功", gin.H{
		"providers":  h.aiProvider.ProviderCatalog(),
		"categories": aidomain.CategoryNames,
	})
}

// ── 配置 CRUD（作用域通用实现） ──

func (h *Handler) adminAIConfigList(c *gin.Context, appID int64) {
	items, err := h.aiProvider.ListConfigs(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) adminAIConfigSave(c *gin.Context, appID int64, id int64) {
	var req adminAIConfigSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.aiProvider.Save(c.Request.Context(), aidomain.ConfigMutation{
		ID:              id,
		AppID:           appID,
		Name:            req.Name,
		Provider:        req.Provider,
		Enabled:         req.Enabled,
		IsDefault:       req.IsDefault,
		Shared:          req.Shared,
		Priority:        req.Priority,
		Description:     req.Description,
		Settings:        copyStringMap(req.Settings),
		Secrets:         copyStringMap(req.Secrets),
		ClearSecrets:    req.ClearSecrets,
		ReplaceSettings: req.ReplaceSettings,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) adminAIConfigDelete(c *gin.Context, appID int64) {
	id, ok := parseAIPathID(c, "configId")
	if !ok {
		return
	}
	if err := h.aiProvider.Delete(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) adminAIConfigTest(c *gin.Context, appID int64) {
	id, ok := parseAIPathID(c, "configId")
	if !ok {
		return
	}
	var req adminAIConfigTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.aiProvider.TestConfig(c.Request.Context(), appID, id, req.Model)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", result)
}

func (h *Handler) adminAIChannel(c *gin.Context, appID int64) {
	overview, err := h.aiProvider.ChannelOverview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", overview)
}

// ── 技能 CRUD（作用域通用实现） ──

func (h *Handler) adminAISkillList(c *gin.Context, appID int64) {
	items, err := h.aiProvider.ListSkills(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) adminAISkillSave(c *gin.Context, appID int64, id int64) {
	var req adminAISkillSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.aiProvider.SaveSkill(c.Request.Context(), aidomain.SkillMutation{
		ID:          id,
		AppID:       appID,
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Enabled:     req.Enabled,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) adminAISkillDelete(c *gin.Context, appID int64) {
	id, ok := parseAIPathID(c, "skillId")
	if !ok {
		return
	}
	if err := h.aiProvider.DeleteSkill(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// ── MCP 服务器 CRUD（作用域通用实现） ──

func (h *Handler) adminAIMCPList(c *gin.Context, appID int64) {
	items, err := h.aiProvider.ListMCPServers(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) adminAIMCPSave(c *gin.Context, appID int64, id int64) {
	var req adminAIMCPServerSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.aiProvider.SaveMCPServer(c.Request.Context(), aidomain.MCPServerMutation{
		ID:           id,
		AppID:        appID,
		Name:         req.Name,
		URL:          req.URL,
		Enabled:      req.Enabled,
		Description:  req.Description,
		Headers:      copyStringMap(req.Headers),
		ClearHeaders: req.ClearHeaders,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if id > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) adminAIMCPDelete(c *gin.Context, appID int64) {
	id, ok := parseAIPathID(c, "serverId")
	if !ok {
		return
	}
	if err := h.aiProvider.DeleteMCPServer(c.Request.Context(), appID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) adminAIMCPTest(c *gin.Context, appID int64) {
	id, ok := parseAIPathID(c, "serverId")
	if !ok {
		return
	}
	result, err := h.aiProvider.TestMCPServer(c.Request.Context(), appID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试完成", result)
}

// ── 平台级入口 ──

func (h *Handler) AdminPlatformAIConfigList(c *gin.Context) {
	h.adminAIConfigList(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIConfigCreate(c *gin.Context) {
	h.adminAIConfigSave(c, aidomain.PlatformAppID, 0)
}

func (h *Handler) AdminPlatformAIConfigUpdate(c *gin.Context) {
	id, ok := parseAIPathID(c, "configId")
	if !ok {
		return
	}
	h.adminAIConfigSave(c, aidomain.PlatformAppID, id)
}

func (h *Handler) AdminPlatformAIConfigDelete(c *gin.Context) {
	h.adminAIConfigDelete(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIConfigTest(c *gin.Context) {
	h.adminAIConfigTest(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIChannel(c *gin.Context) {
	h.adminAIChannel(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAISkillList(c *gin.Context) {
	h.adminAISkillList(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAISkillCreate(c *gin.Context) {
	h.adminAISkillSave(c, aidomain.PlatformAppID, 0)
}

func (h *Handler) AdminPlatformAISkillUpdate(c *gin.Context) {
	id, ok := parseAIPathID(c, "skillId")
	if !ok {
		return
	}
	h.adminAISkillSave(c, aidomain.PlatformAppID, id)
}

func (h *Handler) AdminPlatformAISkillDelete(c *gin.Context) {
	h.adminAISkillDelete(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIMCPList(c *gin.Context) {
	h.adminAIMCPList(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIMCPCreate(c *gin.Context) {
	h.adminAIMCPSave(c, aidomain.PlatformAppID, 0)
}

func (h *Handler) AdminPlatformAIMCPUpdate(c *gin.Context) {
	id, ok := parseAIPathID(c, "serverId")
	if !ok {
		return
	}
	h.adminAIMCPSave(c, aidomain.PlatformAppID, id)
}

func (h *Handler) AdminPlatformAIMCPDelete(c *gin.Context) {
	h.adminAIMCPDelete(c, aidomain.PlatformAppID)
}

func (h *Handler) AdminPlatformAIMCPTest(c *gin.Context) {
	h.adminAIMCPTest(c, aidomain.PlatformAppID)
}

// ── 应用级入口 ──

func (h *Handler) AdminAppAIConfigList(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIConfigList(c, appID)
}

func (h *Handler) AdminAppAIConfigCreate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIConfigSave(c, appID, 0)
}

func (h *Handler) AdminAppAIConfigUpdate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := parseAIPathID(c, "configId")
	if !ok {
		return
	}
	h.adminAIConfigSave(c, appID, id)
}

func (h *Handler) AdminAppAIConfigDelete(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIConfigDelete(c, appID)
}

func (h *Handler) AdminAppAIConfigTest(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIConfigTest(c, appID)
}

func (h *Handler) AdminAppAIChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIChannel(c, appID)
}

func (h *Handler) AdminAppAISkillList(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAISkillList(c, appID)
}

func (h *Handler) AdminAppAISkillCreate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAISkillSave(c, appID, 0)
}

func (h *Handler) AdminAppAISkillUpdate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := parseAIPathID(c, "skillId")
	if !ok {
		return
	}
	h.adminAISkillSave(c, appID, id)
}

func (h *Handler) AdminAppAISkillDelete(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAISkillDelete(c, appID)
}

func (h *Handler) AdminAppAIMCPList(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIMCPList(c, appID)
}

func (h *Handler) AdminAppAIMCPCreate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIMCPSave(c, appID, 0)
}

func (h *Handler) AdminAppAIMCPUpdate(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	id, ok := parseAIPathID(c, "serverId")
	if !ok {
		return
	}
	h.adminAIMCPSave(c, appID, id)
}

func (h *Handler) AdminAppAIMCPDelete(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIMCPDelete(c, appID)
}

func (h *Handler) AdminAppAIMCPTest(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	h.adminAIMCPTest(c, appID)
}

// parseAIPathID 解析路径上的数字标识。
func parseAIPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param(name)), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "标识非法："+name)
		return 0, false
	}
	return id, true
}
