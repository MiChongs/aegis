package httptransport

import (
	"encoding/json"
	"net/http"
	"strings"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// Agent 会话与流式对话入口（应用级）。
//
// 流的载荷格式是 Vercel AI SDK 的 UI Message Stream（SSE，`data: {chunk}`，
// 终止符 [DONE]）—— 控制台前端用 @ai-sdk/react 的 useChat 直接消费，
// 自定义协议意味着前端要手写一个流解析器与状态机，那是它最容易写错的部分。

type adminAIAgentStreamRequest struct {
	// ConversationID 续聊的会话；0 = 新建。
	ConversationID int64 `json:"conversationId"`
	// Scene 会话场景，缺省 function。
	Scene string `json:"scene"`
	// Ref 场景锚点：function 场景下是函数名（新建函数的会话可为空）。
	Ref string `json:"ref"`
	// Message 用户这轮说的话。
	Message string `json:"message" binding:"required"`
	// DraftSource 编辑器当前草稿，作为工具的缺省脚本正文。
	DraftSource string `json:"draftSource"`
	// ConfigID / Model 钉死通道与型号；缺省沿用会话记住的，再缺省走链路。
	ConfigID int64  `json:"configId"`
	Model    string `json:"model"`
	// SkillKeys 本轮注入的技能键；不传 = 全部已启用技能。
	SkillKeys []string `json:"skillKeys"`
	// DisableWrites 关掉所有落库的写工具（建函数/改设置/发版）。
	DisableWrites bool `json:"disableWrites"`
}

// AdminAppAIAgentStream 跑一轮 Agent 对话，以 SSE 流式返回。
func (h *Handler) AdminAppAIAgentStream(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	session, ok := adminAccessSession(c)
	if !ok || session.AdminID <= 0 {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员会话无效")
		return
	}
	var req adminAIAgentStreamRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// 反向代理的响应缓冲会把「流式」变成「憋满一批一起到」，必须显式关掉。
	header.Set("X-Accel-Buffering", "no")
	// AI SDK 靠这个头识别 UI Message Stream 协议版本。
	header.Set("x-vercel-ai-ui-message-stream", "v1")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	wroteAny := false
	emit := func(chunk any) error {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := c.Writer.WriteString("data: " + string(encoded) + "\n\n"); err != nil {
			return err
		}
		c.Writer.Flush()
		wroteAny = true
		return nil
	}

	err := h.aiAgent.Run(c.Request.Context(), service.AIAgentRunInput{
		AppID:          appID,
		AdminID:        session.AdminID,
		ConversationID: req.ConversationID,
		Scene:          req.Scene,
		Ref:            strings.TrimSpace(req.Ref),
		UserText:       req.Message,
		DraftSource:    req.DraftSource,
		ConfigID:       req.ConfigID,
		Model:          strings.TrimSpace(req.Model),
		SkillKeys:      req.SkillKeys,
		DisableWrites:  req.DisableWrites,
	}, emit)
	if err != nil {
		// Run 内部对流中失败已经发过 error chunk；这里只兜「开跑前就失败」
		// （会话不存在、消息为空）那一档 —— 此时还什么都没写。
		if !wroteAny {
			_ = emit(map[string]any{"type": "error", "errorText": err.Error()})
		}
		_ = c.Error(err)
	}
	_, _ = c.Writer.WriteString("data: [DONE]\n\n")
	c.Writer.Flush()
}

// AdminAppAIConversationList 列出当前管理员在该场景锚点下的会话。
func (h *Handler) AdminAppAIConversationList(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员会话无效")
		return
	}
	scene := strings.TrimSpace(c.Query("scene"))
	if scene == "" {
		scene = aidomain.SceneFunction
	}
	items, err := h.aiAgent.ListConversations(c.Request.Context(), aidomain.ConversationQuery{
		AppID:   appID,
		AdminID: session.AdminID,
		Scene:   scene,
		Ref:     strings.TrimSpace(c.Query("ref")),
		Limit:   int(parseInt64Query(c, "limit")),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminAppAIConversationDetail 会话详情 + 全量消息（含被压缩的旧消息，界面回放用）。
func (h *Handler) AdminAppAIConversationDetail(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员会话无效")
		return
	}
	id, ok := parseAIPathID(c, "conversationId")
	if !ok {
		return
	}
	conversation, messages, err := h.aiAgent.ConversationMessages(c.Request.Context(), appID, session.AdminID, id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"conversation": conversation,
		"messages":     messages,
	})
}

func (h *Handler) AdminAppAIConversationDelete(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员会话无效")
		return
	}
	id, ok := parseAIPathID(c, "conversationId")
	if !ok {
		return
	}
	if err := h.aiAgent.DeleteConversation(c.Request.Context(), appID, session.AdminID, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}
