package httptransport

import (
	"net/http"
	"strings"

	ticketdomain "aegis/internal/domain/ticket"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 意见反馈（用户端，/api/v1/apps/:appkey/feedback*）。
// 反馈就是 kind=feedback 的工单；校验、频率限制与对外形状都在 TicketService 的反馈部分。

// FeedbackListQuery 我的反馈。status 逗号分隔，可空。
type FeedbackListQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// FeedbackClientInfo 提交反馈的客户端信息，各字段至多 128 字。
type FeedbackClientInfo struct {
	Platform string `json:"platform"`
	Version  string `json:"version"`
	Device   string `json:"device"`
}

// FeedbackCreateRequest 提交反馈。
type FeedbackCreateRequest struct {
	CategoryID    int64               `json:"categoryId" binding:"required"`
	Title         string              `json:"title" binding:"required"`
	Content       string              `json:"content" binding:"required"`
	Contact       string              `json:"contact"`
	ImageIDs      []int64             `json:"imageIds"`
	AttachmentIDs []int64             `json:"attachmentIds"`
	Client        *FeedbackClientInfo `json:"client"`
}

// FeedbackReplyRequest 追加反馈回复。
type FeedbackReplyRequest struct {
	Content       string  `json:"content" binding:"required"`
	ImageIDs      []int64 `json:"imageIds"`
	AttachmentIDs []int64 `json:"attachmentIds"`
}

// AppFeedbackCategories 反馈分类
// GET /api/v1/apps/:appkey/feedback/categories
func (h *Handler) AppFeedbackCategories(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	items, err := h.ticket.FeedbackCategories(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", items)
}

// AppUploadFeedbackAttachment 上传反馈图片或附件
// POST /api/v1/apps/:appkey/feedback/attachments  (multipart: file, kind=image|file)
func (h *Handler) AppUploadFeedbackAttachment(c *gin.Context) {
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

	saved, err := h.ticket.UploadFeedbackAttachment(c.Request.Context(), session, service.TicketAttachmentInput{
		FileName:      file.Filename,
		ContentType:   strings.TrimSpace(file.Header.Get("Content-Type")),
		ContentLength: file.Size,
		Content:       opened,
		Kind:          strings.TrimSpace(c.PostForm("kind")),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "上传成功", saved)
}

// AppCreateFeedback 提交反馈
// POST /api/v1/apps/:appkey/feedback
func (h *Handler) AppCreateFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	var req FeedbackCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := service.FeedbackCreateInput{
		CategoryID:    req.CategoryID,
		Title:         req.Title,
		Content:       req.Content,
		Contact:       req.Contact,
		ImageIDs:      req.ImageIDs,
		AttachmentIDs: req.AttachmentIDs,
	}
	if req.Client != nil {
		input.Client = &service.FeedbackClient{Platform: req.Client.Platform, Version: req.Client.Version, Device: req.Client.Device}
	}
	detail, err := h.ticket.CreateFeedback(c.Request.Context(), session, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "提交成功", detail)
}

// AppListFeedback 我的反馈
// GET /api/v1/apps/:appkey/feedback
func (h *Handler) AppListFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	var q FeedbackListQuery
	_ = c.ShouldBindQuery(&q)
	result, err := h.ticket.ListFeedback(c.Request.Context(), session, splitCSV(q.Status), q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AppGetFeedback 反馈详情
// GET /api/v1/apps/:appkey/feedback/:feedbackId
func (h *Handler) AppGetFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	feedbackID, ok := feedbackPathID(c)
	if !ok {
		return
	}
	detail, err := h.ticket.FeedbackDetail(c.Request.Context(), session, feedbackID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", detail)
}

// AppReplyFeedback 追加反馈回复
// POST /api/v1/apps/:appkey/feedback/:feedbackId/replies
func (h *Handler) AppReplyFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	feedbackID, ok := feedbackPathID(c)
	if !ok {
		return
	}
	var req FeedbackReplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	detail, err := h.ticket.ReplyFeedback(c.Request.Context(), session, feedbackID, service.FeedbackReplyInput{
		Content: req.Content, ImageIDs: req.ImageIDs, AttachmentIDs: req.AttachmentIDs,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "回复成功", detail)
}

// AppRateFeedback 评价反馈处理结果
// POST /api/v1/apps/:appkey/feedback/:feedbackId/rating
func (h *Handler) AppRateFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	feedbackID, ok := feedbackPathID(c)
	if !ok {
		return
	}
	var req TicketRatingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	detail, err := h.ticket.RateFeedback(c.Request.Context(), session, feedbackID, ticketdomain.RatingCommand{
		Rating: req.Rating, Comment: req.Comment,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "评价成功", detail)
}

// AppCancelFeedback 撤回反馈
// POST /api/v1/apps/:appkey/feedback/:feedbackId/cancel
func (h *Handler) AppCancelFeedback(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "用户未认证")
		return
	}
	feedbackID, ok := feedbackPathID(c)
	if !ok {
		return
	}
	var req TicketCancelRequest
	_ = bind(c, &req)
	detail, err := h.ticket.CancelFeedback(c.Request.Context(), session, feedbackID, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已撤回", detail)
}

func feedbackPathID(c *gin.Context) (int64, bool) {
	id, err := pathInt64(c, "feedbackId")
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的反馈标识")
		return 0, false
	}
	return id, true
}
