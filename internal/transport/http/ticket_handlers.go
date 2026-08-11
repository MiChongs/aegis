package httptransport

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"

	ticketdomain "aegis/internal/domain/ticket"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 管理端工单接口。
// 权限判定全部下沉到 TicketService（Scope + ActionSet），handler 只做参数解析与响应包装。

// ─────────────── 列表 / 详情 ───────────────

// AdminListTickets 工单列表
// GET /api/admin/tickets
func (h *Handler) AdminListTickets(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var q TicketListQuery
	_ = c.ShouldBindQuery(&q)

	query := ticketdomain.ListQuery{
		AppID:         q.AppID,
		Statuses:      splitCSV(q.Status),
		Priorities:    splitCSV(q.Priority),
		CategoryID:    q.CategoryID,
		GroupID:       q.GroupID,
		AssigneeID:    q.AssigneeID,
		Unassigned:    q.Unassigned,
		RequesterID:   q.RequesterID,
		Keyword:       q.Keyword,
		Tags:          splitCSV(q.Tags),
		SLAState:      q.SLAState,
		OverdueOnly:   q.OverdueOnly,
		Rated:         q.Rated,
		SortBy:        q.SortBy,
		SortDir:       q.SortDir,
		IncludeClosed: q.IncludeClosed,
		Page:          q.Page,
		Limit:         q.Limit,
	}
	if from, err := parseOptionalDateTime(q.CreatedFrom); err == nil {
		query.CreatedFrom = from
	}
	if to, err := parseOptionalDateTime(q.CreatedTo); err == nil {
		query.CreatedTo = to
	}
	if q.Mine {
		adminID := session.AdminID
		query.AssigneeID = &adminID
		query.Unassigned = false
	}

	result, err := h.ticket.List(c.Request.Context(), session, query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminGetTicket 工单详情
// GET /api/admin/tickets/:ticketId
func (h *Handler) AdminGetTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	item, err := h.ticket.Detail(c.Request.Context(), session, ticketID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminTicketStats 工单概览统计
// GET /api/admin/tickets/stats
func (h *Handler) AdminTicketStats(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	stats, err := h.ticket.Stats(c.Request.Context(), session, optionalQueryInt64(c, "appid"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", stats)
}

// AdminTicketTrend 工单趋势
// GET /api/admin/tickets/trend?days=30
func (h *Handler) AdminTicketTrend(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	items, err := h.ticket.Trend(c.Request.Context(), session, optionalQueryInt64(c, "appid"), days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminTicketAgentStats 处理人绩效
// GET /api/admin/tickets/agents
func (h *Handler) AdminTicketAgentStats(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	items, err := h.ticket.AgentStats(c.Request.Context(), session, optionalQueryInt64(c, "appid"), limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminTicketWorkbench 我的待办计数
// GET /api/admin/tickets/workbench
func (h *Handler) AdminTicketWorkbench(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	count, err := h.ticket.MyWorkbenchCount(c.Request.Context(), session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", gin.H{"pending": count})
}

// ExportAdminTickets 导出 CSV
// GET /api/admin/tickets/export
func (h *Handler) ExportAdminTickets(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var q TicketListQuery
	_ = c.ShouldBindQuery(&q)
	query := ticketdomain.ListQuery{
		AppID:         q.AppID,
		Statuses:      splitCSV(q.Status),
		Priorities:    splitCSV(q.Priority),
		CategoryID:    q.CategoryID,
		GroupID:       q.GroupID,
		AssigneeID:    q.AssigneeID,
		Keyword:       q.Keyword,
		IncludeClosed: true,
		SortBy:        q.SortBy,
		SortDir:       q.SortDir,
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "5000"))
	items, err := h.ticket.Export(c.Request.Context(), session, query, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=tickets.csv")
	// BOM：Excel 打开中文 CSV 不乱码
	_, _ = c.Writer.WriteString("\xEF\xBB\xBF")
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()
	_ = writer.Write([]string{"工单号", "标题", "状态", "优先级", "分类", "提单人", "受理人", "处理组", "SLA", "创建时间", "更新时间"})
	for _, item := range items {
		_ = writer.Write([]string{
			item.TicketNo, item.Title, item.Status, item.Priority, item.CategoryName,
			item.RequesterName, item.AssigneeName, item.GroupName, item.SLAState,
			item.CreatedAt.Format("2006-01-02 15:04:05"), item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
}

// ─────────────── 写操作 ───────────────

// AdminCreateTicket 管理员建单
// POST /api/admin/tickets
func (h *Handler) AdminCreateTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.CreateByAdmin(c.Request.Context(), session, req.ToCommand())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "创建成功", item)
}

// AdminReplyTicket 回复工单 / 内部备注
// POST /api/admin/tickets/:ticketId/replies
func (h *Handler) AdminReplyTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketReplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	message, err := h.ticket.ReplyByAdmin(c.Request.Context(), session, ticketdomain.ReplyCommand{
		TicketID:      ticketID,
		Content:       req.Content,
		ContentType:   req.ContentType,
		Internal:      req.Internal,
		AttachmentIDs: req.AttachmentIDs,
		Metadata:      req.Metadata,
		NextStatus:    req.NextStatus,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	if req.QuickReplyID > 0 {
		_ = h.ticket.UseQuickReply(c.Request.Context(), req.QuickReplyID)
	}
	response.Success(c, http.StatusOK, "回复成功", message)
}

// AdminUpdateTicket 更新工单字段
// PATCH /api/admin/tickets/:ticketId
func (h *Handler) AdminUpdateTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.Update(c.Request.Context(), session, ticketID, ticketdomain.UpdateCommand{
		Title: req.Title, CategoryID: req.CategoryID, Priority: req.Priority,
		Tags: req.Tags, Locked: req.Locked,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "更新成功", item)
}

// AdminAssignTicket 指派工单
// POST /api/admin/tickets/:ticketId/assign
func (h *Handler) AdminAssignTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketAssignRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.Assign(c.Request.Context(), session, ticketID, ticketdomain.AssignCommand{
		AssigneeAdminID: req.AssigneeAdminID, GroupID: req.GroupID,
		AutoPick: req.AutoPick, Reason: req.Reason,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "指派成功", item)
}

// AdminChangeTicketStatus 状态流转
// POST /api/admin/tickets/:ticketId/status
func (h *Handler) AdminChangeTicketStatus(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.ChangeStatus(c.Request.Context(), session, ticketID, ticketdomain.StatusCommand{
		Status: req.Status, Reason: req.Reason, Solution: req.Solution,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "操作成功", item)
}

// AdminDeleteTicket 删除工单
// DELETE /api/admin/tickets/:ticketId
func (h *Handler) AdminDeleteTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	if err := h.ticket.Delete(c.Request.Context(), session, ticketID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": ticketID})
}

// AdminBulkTickets 批量操作
// POST /api/admin/tickets/bulk
func (h *Handler) AdminBulkTickets(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketBulkRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.ticket.Bulk(c.Request.Context(), session, ticketdomain.BulkCommand{
		IDs: req.IDs, Action: req.Action, AssigneeAdminID: req.AssigneeAdminID,
		GroupID: req.GroupID, Status: req.Status, Priority: req.Priority,
		Tags: req.Tags, Reason: req.Reason,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "操作完成", result)
}

// AdminWatchTicket 关注 / 取消关注
// POST /api/admin/tickets/:ticketId/watch
func (h *Handler) AdminWatchTicket(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	watch := c.Query("watch") != "false"
	if err := h.ticket.Watch(c.Request.Context(), session, ticketID, watch); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "操作成功", gin.H{"watching": watch})
}

// AdminSetTicketWatchers 设置关注人
// PUT /api/admin/tickets/:ticketId/watchers
func (h *Handler) AdminSetTicketWatchers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketWatchersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	watchers, err := h.ticket.SetWatchers(c.Request.Context(), session, ticketID, req.AdminIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", watchers)
}

// AdminUploadTicketAttachment 上传附件
// POST /api/admin/tickets/attachments
func (h *Handler) AdminUploadTicketAttachment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
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

	appID, _ := strconv.ParseInt(strings.TrimSpace(c.PostForm("appid")), 10, 64)
	var ticketID *int64
	if raw := strings.TrimSpace(c.PostForm("ticketId")); raw != "" {
		if parsed, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && parsed > 0 {
			ticketID = &parsed
		}
	}
	uploaderID := session.AdminID
	saved, err := h.ticket.UploadAttachment(c.Request.Context(), requestBaseURL(c.Request), service.TicketAttachmentInput{
		TicketID:      ticketID,
		AppID:         appID,
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploaderType:  "admin",
		UploaderID:    &uploaderID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", saved)
}

// ─────────────── 配置 ───────────────

// AdminListTicketCategories 分类列表
// GET /api/admin/tickets/categories
func (h *Handler) AdminListTicketCategories(c *gin.Context) {
	appID := queryInt64Default(c, "appid", 0)
	items, err := h.ticket.ListCategories(c.Request.Context(), appID, c.Query("enabled") == "true")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveTicketCategory 新建 / 更新分类
// POST /api/admin/tickets/categories  |  PUT /api/admin/tickets/categories/:id
func (h *Handler) AdminSaveTicketCategory(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketCategoryRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item, err := h.ticket.SaveCategory(c.Request.Context(), session, req.ToDomain(id))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", item)
}

// AdminDeleteTicketCategory 删除分类
// DELETE /api/admin/tickets/categories/:id
func (h *Handler) AdminDeleteTicketCategory(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的分类标识")
		return
	}
	if err := h.ticket.DeleteCategory(c.Request.Context(), session, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminListTicketGroups 处理组列表
// GET /api/admin/tickets/groups
func (h *Handler) AdminListTicketGroups(c *gin.Context) {
	appID := queryInt64Default(c, "appid", 0)
	items, err := h.ticket.ListGroups(c.Request.Context(), appID, c.Query("members") != "false")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveTicketGroup 新建 / 更新处理组
// POST /api/admin/tickets/groups  |  PUT /api/admin/tickets/groups/:id
func (h *Handler) AdminSaveTicketGroup(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketGroupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item := ticketdomain.Group{
		ID: id, AppID: req.AppID, Key: req.Key, Name: req.Name,
		Description: req.Description, AssignStrategy: req.AssignStrategy, Enabled: true,
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	saved, err := h.ticket.SaveGroup(c.Request.Context(), session, item)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", saved)
}

// AdminDeleteTicketGroup 删除处理组
// DELETE /api/admin/tickets/groups/:id
func (h *Handler) AdminDeleteTicketGroup(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的处理组标识")
		return
	}
	if err := h.ticket.DeleteGroup(c.Request.Context(), session, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminSetTicketGroupMembers 设置组成员（授权特定人员处理工单）
// PUT /api/admin/tickets/groups/:id/members
func (h *Handler) AdminSetTicketGroupMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的处理组标识")
		return
	}
	var req TicketGroupMembersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	members := make([]ticketdomain.GroupMember, 0, len(req.Members))
	for _, member := range req.Members {
		members = append(members, ticketdomain.GroupMember{AdminID: member.AdminID, Role: member.Role})
	}
	saved, err := h.ticket.SetGroupMembers(c.Request.Context(), session, id, members)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", saved)
}

// AdminListTicketSLAPolicies SLA 策略列表
// GET /api/admin/tickets/sla-policies
func (h *Handler) AdminListTicketSLAPolicies(c *gin.Context) {
	appID := queryInt64Default(c, "appid", 0)
	items, err := h.ticket.ListSLAPolicies(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveTicketSLAPolicy 新建 / 更新 SLA 策略
// POST /api/admin/tickets/sla-policies  |  PUT /api/admin/tickets/sla-policies/:id
func (h *Handler) AdminSaveTicketSLAPolicy(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketSLARequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item := ticketdomain.SLAPolicy{
		ID: id, AppID: req.AppID, Name: req.Name, Description: req.Description,
		FirstResponseMinutes: req.FirstResponseMinutes, ResolveMinutes: req.ResolveMinutes,
		BusinessHours: req.BusinessHours, WarnRatio: req.WarnRatio, Enabled: true,
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	saved, err := h.ticket.SaveSLAPolicy(c.Request.Context(), session, item)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", saved)
}

// AdminDeleteTicketSLAPolicy 删除 SLA 策略
// DELETE /api/admin/tickets/sla-policies/:id
func (h *Handler) AdminDeleteTicketSLAPolicy(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的策略标识")
		return
	}
	if err := h.ticket.DeleteSLAPolicy(c.Request.Context(), session, id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminListTicketQuickReplies 快捷回复列表
// GET /api/admin/tickets/quick-replies
func (h *Handler) AdminListTicketQuickReplies(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	items, err := h.ticket.ListQuickReplies(c.Request.Context(), session, queryInt64Default(c, "appid", 0))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AdminSaveTicketQuickReply 新建 / 更新快捷回复
// POST /api/admin/tickets/quick-replies  |  PUT /api/admin/tickets/quick-replies/:id
func (h *Handler) AdminSaveTicketQuickReply(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req TicketQuickReplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id, _ := pathInt64(c, "id")
	item := ticketdomain.QuickReply{
		ID: id, AppID: req.AppID, Title: req.Title, Content: req.Content,
		CategoryID: req.CategoryID, Sort: req.Sort, Enabled: true,
	}
	if req.Private {
		owner := session.AdminID
		item.OwnerAdminID = &owner
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	saved, err := h.ticket.SaveQuickReply(c.Request.Context(), session, item)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "保存成功", saved)
}

// AdminDeleteTicketQuickReply 删除快捷回复
// DELETE /api/admin/tickets/quick-replies/:id
func (h *Handler) AdminDeleteTicketQuickReply(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的快捷回复标识")
		return
	}
	if err := h.ticket.DeleteQuickReply(c.Request.Context(), session, id, queryInt64Default(c, "appid", 0)); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", gin.H{"id": id})
}

// AdminTicketMetadata 工单模块元数据：状态 / 优先级 / 来源枚举 + 中文标签。
// 前端据此渲染筛选器，避免枚举在两端各写一份。
// GET /api/admin/tickets/metadata
func (h *Handler) AdminTicketMetadata(c *gin.Context) {
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"statuses": []gin.H{
			{"value": ticketdomain.StatusOpen, "label": "待受理"},
			{"value": ticketdomain.StatusProcessing, "label": "处理中"},
			{"value": ticketdomain.StatusPendingUser, "label": "待用户补充"},
			{"value": ticketdomain.StatusPendingThirdParty, "label": "等待第三方"},
			{"value": ticketdomain.StatusResolved, "label": "已解决"},
			{"value": ticketdomain.StatusClosed, "label": "已关闭"},
			{"value": ticketdomain.StatusCancelled, "label": "已撤销"},
		},
		"priorities": []gin.H{
			{"value": ticketdomain.PriorityUrgent, "label": "紧急", "weight": 4},
			{"value": ticketdomain.PriorityHigh, "label": "高", "weight": 3},
			{"value": ticketdomain.PriorityNormal, "label": "中", "weight": 2},
			{"value": ticketdomain.PriorityLow, "label": "低", "weight": 1},
		},
		"sources": []gin.H{
			{"value": ticketdomain.SourceConsole, "label": "控制台"},
			{"value": ticketdomain.SourceApp, "label": "应用内"},
			{"value": ticketdomain.SourceAPI, "label": "接口"},
			{"value": ticketdomain.SourceEmail, "label": "邮件"},
			{"value": ticketdomain.SourceBot, "label": "机器人"},
			{"value": ticketdomain.SourceImport, "label": "导入"},
		},
		"slaStates": []gin.H{
			{"value": ticketdomain.SLAOnTime, "label": "正常"},
			{"value": ticketdomain.SLAWarning, "label": "预警"},
			{"value": ticketdomain.SLABreached, "label": "超时"},
			{"value": ticketdomain.SLAMet, "label": "达标"},
			{"value": ticketdomain.SLAPaused, "label": "已暂停"},
		},
	})
}

// ─────────────── 用户端 ───────────────

// UserListTickets 用户查看自己的工单
// GET /api/user/tickets
func (h *Handler) UserListTickets(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	var q TicketListQuery
	_ = c.ShouldBindQuery(&q)
	result, err := h.ticket.ListForUser(c.Request.Context(), session, ticketdomain.ListQuery{
		Statuses: splitCSV(q.Status),
		Keyword:  q.Keyword,
		Page:     q.Page,
		Limit:    q.Limit,
		SortBy:   q.SortBy,
		SortDir:  q.SortDir,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// UserGetTicket 用户查看工单详情
// GET /api/user/tickets/:ticketId
func (h *Handler) UserGetTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	item, err := h.ticket.DetailForUser(c.Request.Context(), session, ticketID, requestBaseURL(c.Request))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", item)
}

// UserCreateTicket 用户提交工单
// POST /api/user/tickets
func (h *Handler) UserCreateTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	var req TicketCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.CreateByUser(c.Request.Context(), session, req.ToCommand())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "提交成功", item)
}

// UserReplyTicket 用户追加回复
// POST /api/user/tickets/:ticketId/replies
func (h *Handler) UserReplyTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketReplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	message, err := h.ticket.ReplyByUser(c.Request.Context(), session, ticketdomain.ReplyCommand{
		TicketID:      ticketID,
		Content:       req.Content,
		ContentType:   req.ContentType,
		AttachmentIDs: req.AttachmentIDs,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "回复成功", message)
}

// UserRateTicket 用户提交满意度评价
// POST /api/user/tickets/:ticketId/rating
func (h *Handler) UserRateTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketRatingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.ticket.RateByUser(c.Request.Context(), session, ticketID, ticketdomain.RatingCommand{
		Rating: req.Rating, Comment: req.Comment,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "评价成功", item)
}

// UserCancelTicket 用户撤销工单
// POST /api/user/tickets/:ticketId/cancel
func (h *Handler) UserCancelTicket(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	ticketID, err := pathInt64(c, "ticketId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的工单标识")
		return
	}
	var req TicketCancelRequest
	_ = bind(c, &req)
	item, err := h.ticket.CancelByUser(c.Request.Context(), session, ticketID, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已撤销", item)
}

// UserListTicketCategories 用户可选的工单分类
// GET /api/user/tickets/categories
func (h *Handler) UserListTicketCategories(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	items, err := h.ticket.ListCategories(c.Request.Context(), session.AppID, true)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 只暴露允许自助提交的分类，且不下发内部归属字段
	visible := make([]ticketdomain.Category, 0, len(items))
	for _, item := range items {
		if !item.UserSubmittable {
			continue
		}
		item.DefaultGroupID = nil
		item.SLAPolicyID = nil
		visible = append(visible, item)
	}
	response.Success(c, http.StatusOK, "获取成功", visible)
}

// UserUploadTicketAttachment 用户上传工单附件
// POST /api/user/tickets/attachments
func (h *Handler) UserUploadTicketAttachment(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
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

	uploaderID := session.UserID
	saved, err := h.ticket.UploadAttachment(c.Request.Context(), requestBaseURL(c.Request), service.TicketAttachmentInput{
		AppID:         session.AppID,
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		UploaderType:  "user",
		UploaderID:    &uploaderID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", saved)
}

// ─────────────── 小工具 ───────────────

// splitCSV 拆分逗号分隔的多值查询参数。
func splitCSV(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// optionalQueryInt64 读取可选的整型查询参数，缺失或非法返回 nil。
func optionalQueryInt64(c *gin.Context, name string) *int64 {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &value
}

// queryInt64Default 读取整型查询参数，缺失或非法返回默认值。
func queryInt64Default(c *gin.Context, name string, fallback int64) int64 {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return value
}
