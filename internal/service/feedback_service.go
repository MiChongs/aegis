package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 意见反馈：kind = 'feedback' 的工单。
//
// 存取、状态机、SLA、通知全部沿用工单；这里只做反馈入口特有的三件事：
//   - 更严的入参（分类必须是反馈分类、图片与附件分开限量、来源与元数据由服务端填）
//   - 频率限制（同一用户每小时 5 条、每天 20 条，直接数库里的反馈，不引入新设施）
//   - 对外形状（FeedbackSummary / FeedbackDetail），不暴露受理人、处理组、SLA 等内部字段

const (
	errCodeFeedbackRateLimited          = 42960 // 429 提交过于频繁
	errCodeTicketAttachmentLimit        = 42270 // 422 图片或附件数量超过上限
	errCodeTicketAttachmentUnavailable  = 42271 // 422 附件不存在、已被使用或不属于当前账号
	errCodeFeedbackAttachmentKind       = 42272 // 422 imageIds 里放了非图片，或 attachmentIds 里放了图片
	errCodeFeedbackImageUnsupported     = 41560 // 415 按魔数判定不是允许的图片格式
	errCodeFeedbackAttachmentTooLarge   = 41370 // 413 图片超过 10MB / 附件超过 20MB
	errCodeFeedbackNotFound             = 40467 // 404 反馈不存在（含：不是本人的、不是反馈）
	errCodeFeedbackCategoryUnavailable  = 40468 // 404 反馈分类不存在、已停用或不可自助提交
	feedbackMaxImageSize                = 10 << 20
	feedbackMaxImages                   = 4
	feedbackMaxFiles                    = 2
	feedbackMaxTitleLen                 = 100
	feedbackMaxContentLen               = 5000
	feedbackMaxContactLen               = 128
	feedbackMaxClientFieldLen           = 128
	feedbackHourlyLimit                 = 5
	feedbackDailyLimit                  = 20
	feedbackListMaxLimit                = 50
	feedbackListDefaultLimit            = 20
	feedbackRateLimitedMessage          = "提交过于频繁，请稍后再试"
	feedbackAttachmentUnavailableNotice = "附件不存在、已被使用或不属于当前账号"
)

// 反馈图片只收这几种：官网与 App 都能直接内联展示，且不会被浏览器当脚本执行。
var feedbackImageTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/webp": {}, "image/gif": {},
}

func isFeedbackImageType(contentType string) bool {
	_, ok := feedbackImageTypes[contentTypeMediaType(contentType)]
	return ok
}

// mapTicketAttachmentError 把仓储层的绑定失败翻译成对客户端有意义的错误码。
func mapTicketAttachmentError(err error) error {
	if errors.Is(err, pgrepo.ErrTicketAttachmentUnavailable) {
		return apperrors.New(errCodeTicketAttachmentUnavailable, http.StatusUnprocessableEntity, feedbackAttachmentUnavailableNotice)
	}
	return err
}

// ─────────────── 对外形状 ───────────────

// FeedbackCategory 反馈分类（用户端）。
type FeedbackCategory struct {
	ID          int64  `json:"id"`
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// FeedbackCategoryRef 反馈所属分类的摘要。
type FeedbackCategoryRef struct {
	ID   int64  `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// FeedbackAttachment 反馈附件。downloadUrl 为相对 Aegis 根的代理地址，30 分钟有效。
type FeedbackAttachment struct {
	ID          int64     `json:"id"`
	Kind        string    `json:"kind"`
	FileName    string    `json:"fileName"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	DownloadURL string    `json:"downloadUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FeedbackSummary 反馈列表项。id 按契约以字符串下发（值即工单 ID）。
type FeedbackSummary struct {
	ID                 int64               `json:"id,string"`
	TicketNo           string              `json:"ticketNo"`
	Category           FeedbackCategoryRef `json:"category"`
	Title              string              `json:"title"`
	Status             string              `json:"status"`
	MessageCount       int                 `json:"messageCount"`
	LastMessageAt      *time.Time          `json:"lastMessageAt,omitempty"`
	LastReplyFromAgent bool                `json:"lastReplyFromAgent"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
}

// FeedbackClient 提交反馈时的客户端信息。
type FeedbackClient struct {
	Platform string `json:"platform,omitempty"`
	Version  string `json:"version,omitempty"`
	Device   string `json:"device,omitempty"`
}

// FeedbackMessage 反馈会话消息（不含内部备注）。
type FeedbackMessage struct {
	ID          int64                `json:"id"`
	AuthorType  string               `json:"authorType"`
	AuthorName  string               `json:"authorName"`
	Content     string               `json:"content"`
	CreatedAt   time.Time            `json:"createdAt"`
	Attachments []FeedbackAttachment `json:"attachments"`
}

// FeedbackDetail 反馈详情。
type FeedbackDetail struct {
	FeedbackSummary
	Content       string               `json:"content"`
	Contact       string               `json:"contact"`
	Client        *FeedbackClient      `json:"client,omitempty"`
	Rating        *int                 `json:"rating,omitempty"`
	RatingComment string               `json:"ratingComment,omitempty"`
	Messages      []FeedbackMessage    `json:"messages"`
	Attachments   []FeedbackAttachment `json:"attachments"`
}

// FeedbackList 反馈分页。
type FeedbackList struct {
	Items      []FeedbackSummary `json:"items"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"totalPages"`
}

// FeedbackCreateInput 提交反馈。
type FeedbackCreateInput struct {
	CategoryID    int64
	Title         string
	Content       string
	Contact       string
	ImageIDs      []int64
	AttachmentIDs []int64
	Client        *FeedbackClient
}

// FeedbackReplyInput 追加反馈回复。
type FeedbackReplyInput struct {
	Content       string
	ImageIDs      []int64
	AttachmentIDs []int64
}

// ─────────────── 查询 ───────────────

// FeedbackCategories 当前应用可选的反馈分类（本应用 + 平台级，启用且可自助提交）。
func (s *TicketService) FeedbackCategories(ctx context.Context, session *authdomain.Session) ([]FeedbackCategory, error) {
	if session == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "用户未认证")
	}
	items, err := s.pg.ListTicketCategories(ctx, session.AppID, true)
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackCategory, 0, len(items))
	for _, item := range items {
		if item.Kind != ticketdomain.KindFeedback || !item.UserSubmittable {
			continue
		}
		out = append(out, FeedbackCategory{ID: item.ID, Key: item.Key, Name: item.Name, Description: item.Description})
	}
	sortFeedbackCategories(out, items)
	return out, nil
}

// sortFeedbackCategories 按 sort 排序（平台级与应用级混排），sort 相同按 ID。
// 仓储层按 appid 先排，平台级会被压在前面；反馈入口要的是运营排好的顺序。
func sortFeedbackCategories(out []FeedbackCategory, source []ticketdomain.Category) {
	sortKey := make(map[int64]int, len(source))
	for _, item := range source {
		sortKey[item.ID] = item.Sort
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if sortKey[a.ID] < sortKey[b.ID] || (sortKey[a.ID] == sortKey[b.ID] && a.ID <= b.ID) {
				break
			}
			out[j-1], out[j] = b, a
		}
	}
}

// ListFeedback 本人的反馈。
func (s *TicketService) ListFeedback(ctx context.Context, session *authdomain.Session, statuses []string, page, limit int) (*FeedbackList, error) {
	if session == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "用户未认证")
	}
	if limit <= 0 {
		limit = feedbackListDefaultLimit
	}
	if limit > feedbackListMaxLimit {
		limit = feedbackListMaxLimit
	}
	query := ticketdomain.ListQuery{Statuses: statuses, Page: page, Limit: limit, SortBy: "created"}
	normalizeTicketQuery(&query)
	userID := session.UserID
	appID := session.AppID
	query.AppID = &appID
	query.RequesterID = &userID
	query.IncludeClosed = true
	query.Kind = ticketdomain.KindFeedback
	items, total, err := s.pg.ListTickets(ctx, query, ticketdomain.Scope{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackSummary, 0, len(items))
	for i := range items {
		out = append(out, feedbackSummary(&items[i]))
	}
	return &FeedbackList{Items: out, Page: query.Page, Limit: query.Limit, Total: total, TotalPages: totalPages(total, query.Limit)}, nil
}

// FeedbackDetail 本人的一条反馈。
func (s *TicketService) FeedbackDetail(ctx context.Context, session *authdomain.Session, feedbackID int64) (*FeedbackDetail, error) {
	item, err := s.requireUserFeedback(ctx, session, feedbackID)
	if err != nil {
		return nil, err
	}
	return s.buildFeedbackDetail(ctx, item)
}

// requireUserFeedback 取反馈并校验属于当前用户。不是本人的、不是反馈的，一律按不存在回答，
// 不让人用 ID 探测别人的工单是否存在。
func (s *TicketService) requireUserFeedback(ctx context.Context, session *authdomain.Session, feedbackID int64) (*ticketdomain.Ticket, error) {
	if session == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "用户未认证")
	}
	item, err := s.pg.GetTicketByID(ctx, feedbackID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.Kind != ticketdomain.KindFeedback || item.AppID != session.AppID ||
		item.RequesterUserID == nil || *item.RequesterUserID != session.UserID {
		return nil, apperrors.New(errCodeFeedbackNotFound, http.StatusNotFound, "反馈不存在")
	}
	return item, nil
}

func (s *TicketService) buildFeedbackDetail(ctx context.Context, item *ticketdomain.Ticket) (*FeedbackDetail, error) {
	messages, err := s.pg.ListTicketMessages(ctx, item.ID, false)
	if err != nil {
		return nil, err
	}
	attachments, err := s.pg.ListTicketAttachments(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	// 只下发挂在对外消息上的附件：内部备注的附件同样是内部信息
	public := make(map[int64]struct{}, len(messages))
	for _, message := range messages {
		public[message.ID] = struct{}{}
	}
	visible := make([]ticketdomain.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.MessageID != nil {
			if _, ok := public[*attachment.MessageID]; !ok {
				continue
			}
		}
		visible = append(visible, attachment)
	}
	s.resolveAttachmentURLs(ctx, "", visible)

	byMessage := make(map[int64][]FeedbackAttachment, len(messages))
	all := make([]FeedbackAttachment, 0, len(visible))
	for _, attachment := range visible {
		view := feedbackAttachmentView(&attachment)
		all = append(all, view)
		if attachment.MessageID != nil {
			byMessage[*attachment.MessageID] = append(byMessage[*attachment.MessageID], view)
		}
	}

	detail := &FeedbackDetail{
		FeedbackSummary: feedbackSummary(item),
		Contact:         item.RequesterContact,
		Client:          feedbackClientFromMetadata(item.Metadata),
		RatingComment:   item.RatingComment,
		Messages:        make([]FeedbackMessage, 0, len(messages)),
		Attachments:     all,
	}
	if item.Rating != nil {
		rating := int(*item.Rating)
		detail.Rating = &rating
	}
	for i, message := range messages {
		if i == 0 {
			detail.Content = message.Content
		}
		files := byMessage[message.ID]
		if files == nil {
			files = []FeedbackAttachment{}
		}
		detail.Messages = append(detail.Messages, FeedbackMessage{
			ID: message.ID, AuthorType: message.AuthorType, AuthorName: message.AuthorName,
			Content: message.Content, CreatedAt: message.CreatedAt, Attachments: files,
		})
	}
	return detail, nil
}

func feedbackSummary(item *ticketdomain.Ticket) FeedbackSummary {
	summary := FeedbackSummary{
		ID:                 item.ID,
		TicketNo:           item.TicketNo,
		Category:           FeedbackCategoryRef{Key: item.CategoryKey, Name: item.CategoryName},
		Title:              item.Title,
		Status:             item.Status,
		MessageCount:       item.MessageCount,
		LastMessageAt:      item.LastMessageAt,
		LastReplyFromAgent: item.LastMessageRole == ticketdomain.AuthorAgent,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
	if item.CategoryID != nil {
		summary.Category.ID = *item.CategoryID
	}
	return summary
}

func feedbackAttachmentView(item *ticketdomain.Attachment) FeedbackAttachment {
	kind := item.Kind
	if kind != ticketdomain.AttachmentImage {
		kind = ticketdomain.AttachmentFile
	}
	return FeedbackAttachment{
		ID: item.ID, Kind: kind, FileName: item.FileName, ContentType: item.ContentType,
		SizeBytes: item.SizeBytes, DownloadURL: item.DownloadURL, CreatedAt: item.CreatedAt,
	}
}

func feedbackClientFromMetadata(metadata map[string]any) *FeedbackClient {
	raw, ok := metadata["client"].(map[string]any)
	if !ok {
		return nil
	}
	read := func(key string) string {
		value, _ := raw[key].(string)
		return value
	}
	client := &FeedbackClient{Platform: read("platform"), Version: read("version"), Device: read("device")}
	if client.Platform == "" && client.Version == "" && client.Device == "" {
		return nil
	}
	return client
}

// ─────────────── 提交 / 回复 ───────────────

// CreateFeedback 提交反馈。
func (s *TicketService) CreateFeedback(ctx context.Context, session *authdomain.Session, input FeedbackCreateInput) (*FeedbackDetail, error) {
	if session == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "用户未认证")
	}
	if input.CategoryID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择反馈分类")
	}
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.Content)
	contact := strings.TrimSpace(input.Contact)
	if title == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "反馈标题不能为空")
	}
	if len([]rune(title)) > feedbackMaxTitleLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "反馈标题不能超过 100 字")
	}
	if content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "反馈内容不能为空")
	}
	if len([]rune(content)) > feedbackMaxContentLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "反馈内容不能超过 5000 字")
	}
	if len([]rune(contact)) > feedbackMaxContactLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "联系方式不能超过 128 字")
	}
	client, err := normalizeFeedbackClient(input.Client)
	if err != nil {
		return nil, err
	}
	imageIDs, fileIDs, err := checkFeedbackAttachmentCounts(input.ImageIDs, input.AttachmentIDs)
	if err != nil {
		return nil, err
	}

	category, err := s.pg.GetTicketCategory(ctx, input.CategoryID)
	if err != nil {
		return nil, err
	}
	if category == nil || category.Kind != ticketdomain.KindFeedback || !category.Enabled || !category.UserSubmittable ||
		(category.AppID != 0 && category.AppID != session.AppID) {
		return nil, apperrors.New(errCodeFeedbackCategoryUnavailable, http.StatusNotFound, "反馈分类不存在或已停用")
	}

	if err := s.checkFeedbackRate(ctx, session); err != nil {
		return nil, err
	}
	if err := s.checkFeedbackAttachments(ctx, session.UserID, imageIDs, fileIDs); err != nil {
		return nil, err
	}

	source := ticketdomain.SourceApp
	metadata := map[string]any{}
	if client != nil {
		if strings.EqualFold(client.Platform, "web") {
			source = ticketdomain.SourceWeb
		}
		clientMap := map[string]any{}
		if client.Platform != "" {
			clientMap["platform"] = client.Platform
		}
		if client.Version != "" {
			clientMap["version"] = client.Version
		}
		if client.Device != "" {
			clientMap["device"] = client.Device
		}
		metadata["client"] = clientMap
	}

	userID := session.UserID
	categoryID := category.ID
	item, err := s.create(ctx, ticketdomain.CreateCommand{
		AppID:            session.AppID,
		Kind:             ticketdomain.KindFeedback,
		RequesterType:    ticketdomain.RequesterUser,
		RequesterUserID:  &userID,
		RequesterName:    s.feedbackRequesterName(ctx, session),
		RequesterContact: contact,
		CategoryID:       &categoryID,
		Title:            title,
		Content:          content,
		ContentType:      "text",
		Priority:         category.DefaultPriority,
		Source:           source,
		Metadata:         metadata,
		AttachmentIDs:    append(append([]int64{}, imageIDs...), fileIDs...),
		AttachmentOwner:  &ticketdomain.AttachmentOwner{Type: "user", ID: userID},
	})
	if err != nil {
		return nil, err
	}
	return s.buildFeedbackDetail(ctx, item)
}

// ReplyFeedback 反馈人追加回复。
func (s *TicketService) ReplyFeedback(ctx context.Context, session *authdomain.Session, feedbackID int64, input FeedbackReplyInput) (*FeedbackDetail, error) {
	if _, err := s.requireUserFeedback(ctx, session, feedbackID); err != nil {
		return nil, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容不能为空")
	}
	if len([]rune(content)) > feedbackMaxContentLen {
		return nil, apperrors.New(40000, http.StatusBadRequest, "回复内容不能超过 5000 字")
	}
	imageIDs, fileIDs, err := checkFeedbackAttachmentCounts(input.ImageIDs, input.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	if err := s.checkFeedbackAttachments(ctx, session.UserID, imageIDs, fileIDs); err != nil {
		return nil, err
	}
	if _, err := s.ReplyByUser(ctx, session, ticketdomain.ReplyCommand{
		TicketID:      feedbackID,
		Content:       content,
		ContentType:   "text",
		AttachmentIDs: append(append([]int64{}, imageIDs...), fileIDs...),
	}); err != nil {
		return nil, err
	}
	return s.FeedbackDetail(ctx, session, feedbackID)
}

// RateFeedback 评价反馈处理结果。
func (s *TicketService) RateFeedback(ctx context.Context, session *authdomain.Session, feedbackID int64, cmd ticketdomain.RatingCommand) (*FeedbackDetail, error) {
	if _, err := s.requireUserFeedback(ctx, session, feedbackID); err != nil {
		return nil, err
	}
	if _, err := s.RateByUser(ctx, session, feedbackID, cmd); err != nil {
		return nil, err
	}
	return s.FeedbackDetail(ctx, session, feedbackID)
}

// CancelFeedback 撤回反馈。
func (s *TicketService) CancelFeedback(ctx context.Context, session *authdomain.Session, feedbackID int64, reason string) (*FeedbackDetail, error) {
	if _, err := s.requireUserFeedback(ctx, session, feedbackID); err != nil {
		return nil, err
	}
	if _, err := s.CancelByUser(ctx, session, feedbackID, reason); err != nil {
		return nil, err
	}
	return s.FeedbackDetail(ctx, session, feedbackID)
}

// UploadFeedbackAttachment 上传反馈图片或附件。kind 缺省为 file。
func (s *TicketService) UploadFeedbackAttachment(ctx context.Context, session *authdomain.Session, input TicketAttachmentInput) (*FeedbackAttachment, error) {
	if session == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "用户未认证")
	}
	kind := strings.TrimSpace(strings.ToLower(input.Kind))
	if kind == "" {
		kind = ticketdomain.AttachmentFile
	}
	if kind != ticketdomain.AttachmentImage && kind != ticketdomain.AttachmentFile {
		return nil, apperrors.New(40000, http.StatusBadRequest, "附件类型只能是 image 或 file")
	}
	userID := session.UserID
	input.Kind = kind
	input.TicketID = nil
	input.AppID = session.AppID
	input.UploaderType = "user"
	input.UploaderID = &userID
	saved, err := s.UploadAttachment(ctx, "", input)
	if err != nil {
		return nil, err
	}
	view := feedbackAttachmentView(saved)
	return &view, nil
}

// ─────────────── 校验 ───────────────

func normalizeFeedbackClient(client *FeedbackClient) (*FeedbackClient, error) {
	if client == nil {
		return nil, nil
	}
	out := &FeedbackClient{
		Platform: strings.TrimSpace(client.Platform),
		Version:  strings.TrimSpace(client.Version),
		Device:   strings.TrimSpace(client.Device),
	}
	for _, value := range []string{out.Platform, out.Version, out.Device} {
		if len([]rune(value)) > feedbackMaxClientFieldLen {
			return nil, apperrors.New(40000, http.StatusBadRequest, "客户端信息过长")
		}
	}
	if out.Platform == "" && out.Version == "" && out.Device == "" {
		return nil, nil
	}
	return out, nil
}

// checkFeedbackAttachmentCounts 去重后检查数量；同一个 ID 不能同时出现在图片与附件里。
func checkFeedbackAttachmentCounts(imageIDs, fileIDs []int64) ([]int64, []int64, error) {
	images := distinctIDs(imageIDs)
	files := distinctIDs(fileIDs)
	if len(images) > feedbackMaxImages {
		return nil, nil, apperrors.New(errCodeTicketAttachmentLimit, http.StatusUnprocessableEntity, "最多上传 4 张图片")
	}
	if len(files) > feedbackMaxFiles {
		return nil, nil, apperrors.New(errCodeTicketAttachmentLimit, http.StatusUnprocessableEntity, "最多上传 2 个附件")
	}
	seen := make(map[int64]struct{}, len(images))
	for _, id := range images {
		seen[id] = struct{}{}
	}
	for _, id := range files {
		if _, dup := seen[id]; dup {
			return nil, nil, apperrors.New(errCodeFeedbackAttachmentKind, http.StatusUnprocessableEntity, "同一个文件不能既作图片又作附件")
		}
	}
	return images, files, nil
}

func distinctIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// checkFeedbackAttachments 逐个核对附件：本人上传、尚未使用、类型与所放的位置一致。
// 绑定事务里还会再按归属校验一次（防并发），这里先查是为了给出准确的错误。
func (s *TicketService) checkFeedbackAttachments(ctx context.Context, userID int64, imageIDs, fileIDs []int64) error {
	if len(imageIDs) == 0 && len(fileIDs) == 0 {
		return nil
	}
	items, err := s.pg.ListTicketAttachmentsByIDs(ctx, append(append([]int64{}, imageIDs...), fileIDs...))
	if err != nil {
		return err
	}
	byID := make(map[int64]ticketdomain.Attachment, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	check := func(ids []int64, wantKind string) error {
		for _, id := range ids {
			item, ok := byID[id]
			if !ok || item.MessageID != nil || item.TicketID != nil || item.UploadedByType != "user" ||
				item.UploadedByID == nil || *item.UploadedByID != userID {
				return apperrors.New(errCodeTicketAttachmentUnavailable, http.StatusUnprocessableEntity, feedbackAttachmentUnavailableNotice)
			}
			if item.Kind != wantKind {
				if wantKind == ticketdomain.AttachmentImage {
					return apperrors.New(errCodeFeedbackAttachmentKind, http.StatusUnprocessableEntity, "图片位置只能放图片，请按图片重新上传")
				}
				return apperrors.New(errCodeFeedbackAttachmentKind, http.StatusUnprocessableEntity, "图片请放到 imageIds 里")
			}
		}
		return nil
	}
	if err := check(imageIDs, ticketdomain.AttachmentImage); err != nil {
		return err
	}
	return check(fileIDs, ticketdomain.AttachmentFile)
}

// checkFeedbackRate 同一用户每小时最多 5 条、每天最多 20 条。
// 直接数库：反馈量级很小，走 (appid, kind, requester_user_id, created_at) 索引，
// 不值得为此引入 Redis 计数器，也省掉了计数器与真实数据不一致的问题。
func (s *TicketService) checkFeedbackRate(ctx context.Context, session *authdomain.Session) error {
	now := timeutil.Now()
	hourly, err := s.pg.CountUserTicketsSince(ctx, session.AppID, session.UserID, ticketdomain.KindFeedback, now.Add(-time.Hour))
	if err != nil {
		return err
	}
	if hourly >= feedbackHourlyLimit {
		return apperrors.New(errCodeFeedbackRateLimited, http.StatusTooManyRequests, feedbackRateLimitedMessage)
	}
	daily, err := s.pg.CountUserTicketsSince(ctx, session.AppID, session.UserID, ticketdomain.KindFeedback, now.Add(-24*time.Hour))
	if err != nil {
		return err
	}
	if daily >= feedbackDailyLimit {
		return apperrors.New(errCodeFeedbackRateLimited, http.StatusTooManyRequests, feedbackRateLimitedMessage)
	}
	return nil
}

// feedbackRequesterName 反馈人显示名：昵称优先，其次账号。
func (s *TicketService) feedbackRequesterName(ctx context.Context, session *authdomain.Session) string {
	if profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID); err == nil && profile != nil {
		if name := strings.TrimSpace(profile.Nickname); name != "" {
			return name
		}
	}
	if account := strings.TrimSpace(session.Account); account != "" {
		return account
	}
	if user, err := s.pg.GetUserByID(ctx, session.UserID); err == nil && user != nil && strings.TrimSpace(user.Account) != "" {
		return user.Account
	}
	return "用户" + strconv.FormatInt(session.UserID, 10)
}
