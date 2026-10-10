package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// 意见反馈的端到端集成测试：真实 Postgres，迁移跑两遍（含 000094），本地存储真实落盘。
//
// 走一遍「种子分类 → 上传图片与附件（冒充图片被拒）→ 提交反馈 → 数量 / 归属 / 类型校验 →
// 只看得到自己的 → 工单列表与分类不混入反馈 → 客服回复后的通知指向 /feedback/{id} →
// 用户补充、评价、撤回 → 频率限制 → 工单附件绑定的安全修复 → 管理端按类型筛选与统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestFeedbackIntegration
func TestFeedbackIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(32000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'feedback-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO storage_configs (scope, provider, config_name, access_mode, enabled, is_default, proxy_download, config_data)
VALUES ('global', 'local', 'default', 'public', TRUE, TRUE, FALSE, $1)`, map[string]any{"root_dir": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	svc := NewTicketService(zap.NewNop(), pg, nil, nil, storage)

	userA := insertTestUser(t, ctx, pool, appID, "fb-a", "0")
	userB := insertTestUser(t, ctx, pool, appID, "fb-b", "0")
	if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname) VALUES ($1, '小 A') ON CONFLICT (user_id) DO UPDATE SET nickname = EXCLUDED.nickname`, userA); err != nil {
		t.Fatal(err)
	}
	sessA := &authdomain.Session{UserID: userA, AppID: appID, Account: "fb-a"}
	sessB := &authdomain.Session{UserID: userB, AppID: appID, Account: "fb-b"}
	var adminID int64
	if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ('fb-admin', 'x', '客服小王', TRUE) RETURNING id`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	admin := &admindomain.AccessContext{Session: admindomain.Session{AdminID: adminID, Account: "fb-admin", DisplayName: "客服小王", IsSuperAdmin: true}}

	// ── 迁移：种子分类按 sort 排好，kind=feedback ──
	categories, err := svc.FeedbackCategories(ctx, sessA)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(categories))
	for _, item := range categories {
		keys = append(keys, item.Key)
	}
	if strings.Join(keys, ",") != "feedback_bug,feedback_feature,feedback_experience,feedback_other" {
		t.Fatalf("反馈分类应为四个种子分类且按 sort 排序，得到 %v", keys)
	}
	bugCategory := categories[0]
	var ticketCategoryID int64
	if err := pool.QueryRow(ctx, `INSERT INTO ticket_categories (appid, key, name) VALUES ($1, 'account', '账号问题') RETURNING id`, appID).Scan(&ticketCategoryID); err != nil {
		t.Fatal(err)
	}

	// ── 上传：图片按魔数判定 ──
	png := tinyPNG()
	upload := func(sess *authdomain.Session, kind, name, declared string, data []byte) (*FeedbackAttachment, error) {
		return svc.UploadFeedbackAttachment(ctx, sess, TicketAttachmentInput{
			FileName: name, ContentType: declared, ContentLength: int64(len(data)), Content: bytes.NewReader(data), Kind: kind,
		})
	}
	img1, err := upload(sessA, "image", "a.png", "application/octet-stream", png)
	if err != nil {
		t.Fatal(err)
	}
	if img1.Kind != "image" || img1.ContentType != "image/png" {
		t.Fatalf("PNG 应按魔数判为 image/png，得到 %+v", img1)
	}
	if _, err := upload(sessA, "image", "fake.png", "image/png", []byte("this is plain text, not a picture at all")); errCode(err) != errCodeFeedbackImageUnsupported {
		t.Fatalf("文本冒充图片应被拒（41560），得到 %v", err)
	}
	if _, err := upload(sessA, "video", "a.png", "image/png", png); errCode(err) != 40000 {
		t.Fatalf("未知 kind 应被拒，得到 %v", err)
	}
	file1, err := upload(sessA, "", "log.txt", "image/png", []byte("2026-10-10 crash log line\nsecond line\n"))
	if err != nil {
		t.Fatal(err)
	}
	if file1.Kind != "file" || !strings.HasPrefix(file1.ContentType, "text/plain") {
		t.Fatalf("文本附件应存嗅探出的 text/plain 而不是声明的 image/png，得到 %+v", file1)
	}
	var storedType string
	if err := pool.QueryRow(ctx, `SELECT content_type FROM ticket_attachments WHERE id = $1`, file1.ID).Scan(&storedType); err != nil || !strings.HasPrefix(storedType, "text/plain") {
		t.Fatalf("附件行应存嗅探出的类型，得到 %q / %v", storedType, err)
	}

	moreImages := make([]int64, 0, 5)
	moreImages = append(moreImages, img1.ID)
	for i := 0; i < 4; i++ {
		item, err := upload(sessA, "image", fmt.Sprintf("p%d.png", i), "image/png", png)
		if err != nil {
			t.Fatal(err)
		}
		moreImages = append(moreImages, item.ID)
	}
	moreFiles := []int64{file1.ID}
	for i := 0; i < 2; i++ {
		item, err := upload(sessA, "file", fmt.Sprintf("f%d.txt", i), "", []byte(fmt.Sprintf("file %d content", i)))
		if err != nil {
			t.Fatal(err)
		}
		moreFiles = append(moreFiles, item.ID)
	}
	foreign, err := upload(sessB, "image", "b.png", "image/png", png)
	if err != nil {
		t.Fatal(err)
	}

	base := FeedbackCreateInput{CategoryID: bugCategory.ID, Title: "闪退", Content: "打开设置页就闪退",
		Contact: "a@example.com", Client: &FeedbackClient{Platform: "web", Version: "1.4.0", Device: "Chrome 140 / Windows"}}
	withAttachments := func(images, files []int64) FeedbackCreateInput {
		in := base
		in.ImageIDs = images
		in.AttachmentIDs = files
		return in
	}

	// ── 数量 / 归属 / 类型 ──
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments(moreImages, nil)); errCode(err) != errCodeTicketAttachmentLimit {
		t.Fatalf("5 张图片应被拒（42270），得到 %v", err)
	}
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments(nil, moreFiles)); errCode(err) != errCodeTicketAttachmentLimit {
		t.Fatalf("3 个附件应被拒（42270），得到 %v", err)
	}
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments([]int64{foreign.ID}, nil)); errCode(err) != errCodeTicketAttachmentUnavailable {
		t.Fatalf("别人的附件应被拒（42271），得到 %v", err)
	}
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments([]int64{file1.ID}, nil)); errCode(err) != errCodeFeedbackAttachmentKind {
		t.Fatalf("把文件放进 imageIds 应被拒（42272），得到 %v", err)
	}
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments(nil, []int64{img1.ID})); errCode(err) != errCodeFeedbackAttachmentKind {
		t.Fatalf("把图片放进 attachmentIds 应被拒（42272），得到 %v", err)
	}
	wrongCategory := base
	wrongCategory.CategoryID = ticketCategoryID
	if _, err := svc.CreateFeedback(ctx, sessA, wrongCategory); errCode(err) != errCodeFeedbackCategoryUnavailable {
		t.Fatalf("工单分类不能用来提反馈（40468），得到 %v", err)
	}
	longTitle := base
	longTitle.Title = strings.Repeat("长", 101)
	if _, err := svc.CreateFeedback(ctx, sessA, longTitle); errCode(err) != 40000 {
		t.Fatalf("101 字标题应被拒，得到 %v", err)
	}
	var stillUnbound int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ticket_attachments WHERE ticket_id IS NULL`).Scan(&stillUnbound); err != nil || stillUnbound != 9 {
		t.Fatalf("被拒的提交不应绑定任何附件，未绑定数应为 9，得到 %d / %v", stillUnbound, err)
	}

	// ── 提交 ──
	created, err := svc.CreateFeedback(ctx, sessA, withAttachments(moreImages[:4], moreFiles[:2]))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "open" || created.Content != base.Content || created.Contact != base.Contact ||
		created.Category.Key != "feedback_bug" || created.Category.ID != bugCategory.ID ||
		len(created.Attachments) != 6 || len(created.Messages) != 1 || len(created.Messages[0].Attachments) != 6 ||
		created.Client == nil || created.Client.Platform != "web" || created.MessageCount != 1 || created.LastReplyFromAgent {
		t.Fatalf("反馈详情不对：%+v", created)
	}
	raw, _ := json.Marshal(created)
	if !strings.Contains(string(raw), fmt.Sprintf(`"id":"%d"`, created.ID)) || strings.Contains(string(raw), "assignee") {
		t.Fatalf("反馈 id 应以字符串下发且不含内部字段：%s", raw)
	}
	ticket, err := pg.GetTicketByID(ctx, created.ID)
	if err != nil || ticket == nil {
		t.Fatal(err)
	}
	if ticket.Kind != "feedback" || ticket.Source != "web" || ticket.RequesterName != "小 A" || ticket.ImageCount != 4 || ticket.AttachmentCount != 2 {
		t.Fatalf("落库的反馈不对：kind=%s source=%s name=%s images=%d files=%d", ticket.Kind, ticket.Source, ticket.RequesterName, ticket.ImageCount, ticket.AttachmentCount)
	}
	if client, _ := ticket.Metadata["client"].(map[string]any); client == nil || client["device"] != "Chrome 140 / Windows" {
		t.Fatalf("metadata.client 应原样保存，得到 %v", ticket.Metadata)
	}
	// 已用过的附件不能再用
	if _, err := svc.CreateFeedback(ctx, sessA, withAttachments([]int64{moreImages[0]}, nil)); errCode(err) != errCodeTicketAttachmentUnavailable {
		t.Fatalf("已绑定的附件不能再用（42271），得到 %v", err)
	}

	// ── 只看得到自己的；工单入口不混入反馈 ──
	if _, err := svc.FeedbackDetail(ctx, sessB, created.ID); errCode(err) != errCodeFeedbackNotFound {
		t.Fatalf("别人的反馈应 404（40467），得到 %v", err)
	}
	userTicket, err := svc.CreateByUser(ctx, sessA, ticketdomain.CreateCommand{
		CategoryID: &ticketCategoryID, Title: "改绑手机", Content: "旧号停用了",
		Source: "import", Tags: []string{"vip"}, Metadata: map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if userTicket.Kind != "ticket" || userTicket.Source != "app" || len(userTicket.Tags) != 0 || len(userTicket.Metadata) != 0 {
		t.Fatalf("用户提单不应能设置来源、标签与元数据：%+v", userTicket)
	}
	if _, err := svc.FeedbackDetail(ctx, sessA, userTicket.ID); errCode(err) != errCodeFeedbackNotFound {
		t.Fatalf("普通工单不应能从反馈入口读到，得到 %v", err)
	}
	if _, err := svc.CreateByUser(ctx, sessA, ticketdomain.CreateCommand{Title: "x", Content: "y", ContentType: "html"}); errCode(err) != 40000 {
		t.Fatalf("用户端 html 内容应被拒，得到 %v", err)
	}
	feedbackCategory := bugCategory.ID
	if _, err := svc.CreateByUser(ctx, sessA, ticketdomain.CreateCommand{CategoryID: &feedbackCategory, Title: "x", Content: "y"}); errCode(err) != 40314 {
		t.Fatalf("工单入口不能用反馈分类，得到 %v", err)
	}
	tickets, err := svc.ListForUser(ctx, sessA, ticketdomain.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if tickets.Total != 1 || tickets.Items[0].ID != userTicket.ID {
		t.Fatalf("我的工单应只有 1 条普通工单，得到 %d 条", tickets.Total)
	}
	mine, err := svc.ListFeedback(ctx, sessA, nil, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if mine.Total != 1 || mine.Items[0].ID != created.ID || mine.Limit != 50 {
		t.Fatalf("我的反馈应只有 1 条、limit 封顶 50，得到 %+v", mine)
	}
	if others, err := svc.ListFeedback(ctx, sessB, nil, 1, 10); err != nil || others.Total != 0 {
		t.Fatalf("B 不应看到 A 的反馈，得到 %+v / %v", others, err)
	}
	if filtered, err := svc.ListFeedback(ctx, sessA, []string{"resolved"}, 1, 10); err != nil || filtered.Total != 0 {
		t.Fatalf("按状态筛选不对，得到 %+v / %v", filtered, err)
	}

	// ── 客服回复：通知指向反馈详情 ──
	if _, err := svc.ReplyByAdmin(ctx, admin, ticketdomain.ReplyCommand{TicketID: created.ID, Content: "内部：复现了", Internal: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplyByAdmin(ctx, admin, ticketdomain.ReplyCommand{TicketID: created.ID, Content: "已定位，下个版本修复"}); err != nil {
		t.Fatal(err)
	}
	replied, err := pg.GetTicketByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	event := svc.buildTicketEvent(ctx, replied, ticketEventAgentReplied, ticketActor{AdminID: &adminID},
		map[string]any{"replyExcerpt": "已定位，下个版本修复"})
	if event.UserLink != fmt.Sprintf("/feedback/%d", created.ID) || event.Resource != "feedback" ||
		event.ResourceID != fmt.Sprint(created.ID) || event.UserTitle != "【反馈已回复】闪退" {
		t.Fatalf("反馈的通知应指向 /feedback/{id}，得到 link=%s resource=%s title=%s", event.UserLink, event.Resource, event.UserTitle)
	}
	if len(event.Recipients.UserIDs) != 1 || event.Recipients.UserIDs[0] != userA {
		t.Fatalf("客服回复应通知反馈人，得到 %v", event.Recipients.UserIDs)
	}
	if plain := svc.buildTicketEvent(ctx, userTicket, ticketEventAgentReplied, ticketActor{}, nil); plain.UserLink != fmt.Sprintf("/tickets/%d", userTicket.ID) || plain.Resource != "ticket" {
		t.Fatalf("普通工单的通知不应受影响，得到 %s / %s", plain.UserLink, plain.Resource)
	}
	detail, err := svc.FeedbackDetail(ctx, sessA, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Messages) != 2 || detail.Messages[1].AuthorType != "agent" || !detail.LastReplyFromAgent || detail.Status != "processing" {
		t.Fatalf("用户侧应看到客服回复、看不到内部备注：%+v", detail.Messages)
	}

	// ── 用户补充（带一张新图）、评价、撤回 ──
	img5, err := upload(sessA, "image", "more.png", "image/png", png)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReplyFeedback(ctx, sessA, created.ID, FeedbackReplyInput{Content: "补充截图", ImageIDs: []int64{foreign.ID}}); errCode(err) != errCodeTicketAttachmentUnavailable {
		t.Fatalf("补充时用别人的图应被拒，得到 %v", err)
	}
	afterReply, err := svc.ReplyFeedback(ctx, sessA, created.ID, FeedbackReplyInput{Content: "补充截图", ImageIDs: []int64{img5.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(afterReply.Messages) != 3 || len(afterReply.Messages[2].Attachments) != 1 || afterReply.LastReplyFromAgent {
		t.Fatalf("补充后的详情不对：%+v", afterReply.Messages)
	}
	if _, err := svc.RateFeedback(ctx, sessA, created.ID, ticketdomain.RatingCommand{Rating: 5}); errCode(err) != 40317 {
		t.Fatalf("未解决时不能评价，得到 %v", err)
	}
	if _, err := svc.ChangeStatus(ctx, admin, created.ID, ticketdomain.StatusCommand{Status: "resolved", Solution: "1.4.1 已修复"}); err != nil {
		t.Fatal(err)
	}
	resolvedTicket, _ := pg.GetTicketByID(ctx, created.ID)
	if ev := svc.buildTicketEvent(ctx, resolvedTicket, ticketEventResolved, ticketActor{AdminID: &adminID}, nil); ev.UserTitle != "【反馈已解决】闪退" {
		t.Fatalf("解决通知标题不对：%s", ev.UserTitle)
	}
	rated, err := svc.RateFeedback(ctx, sessA, created.ID, ticketdomain.RatingCommand{Rating: 4, Comment: "挺快"})
	if err != nil {
		t.Fatal(err)
	}
	if rated.Rating == nil || *rated.Rating != 4 || rated.RatingComment != "挺快" {
		t.Fatalf("评价没有落到详情：%+v", rated)
	}
	if _, err := svc.RateFeedback(ctx, sessA, created.ID, ticketdomain.RatingCommand{Rating: 3}); errCode(err) != 40901 {
		t.Fatalf("只能评价一次，得到 %v", err)
	}
	cancelled, err := svc.CancelFeedback(ctx, sessA, created.ID, "不用了")
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("撤回失败：%+v / %v", cancelled, err)
	}
	if _, err := svc.ReplyFeedback(ctx, sessA, created.ID, FeedbackReplyInput{Content: "还在吗"}); errCode(err) != 40316 {
		t.Fatalf("撤回后不能再回复，得到 %v", err)
	}

	// ── 频率限制：每小时 5 条 ──
	for i := 0; i < 4; i++ {
		in := base
		in.Title = fmt.Sprintf("第 %d 条", i+2)
		in.Client = nil
		if _, err := svc.CreateFeedback(ctx, sessA, in); err != nil {
			t.Fatalf("第 %d 条应能提交：%v", i+2, err)
		}
	}
	if _, err := svc.CreateFeedback(ctx, sessA, base); errCode(err) != errCodeFeedbackRateLimited {
		t.Fatalf("一小时内第 6 条应被限流（42960），得到 %v", err)
	}
	if _, err := svc.CreateFeedback(ctx, sessB, base); err != nil {
		t.Fatalf("限流按人计，B 不受 A 影响：%v", err)
	}
	// 每天 20 条：把 A 的反馈挪到一小时前，再补足 20 条
	if _, err := pool.Exec(ctx, `UPDATE tickets SET created_at = NOW() - INTERVAL '2 hours' WHERE requester_user_id = $1 AND kind = 'feedback'`, userA); err != nil {
		t.Fatal(err)
	}
	insertOldFeedback(t, ctx, pool, appID, userA, 15)
	if _, err := svc.CreateFeedback(ctx, sessA, base); errCode(err) != errCodeFeedbackRateLimited {
		t.Fatalf("一天内第 21 条应被限流，得到 %v", err)
	}
	appFeedback, err := svc.ListFeedback(ctx, sessA, nil, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if appFeedback.Total != 20 {
		t.Fatalf("A 应有 20 条反馈，得到 %d", appFeedback.Total)
	}
	if noClient, err := svc.FeedbackDetail(ctx, sessA, appFeedback.Items[0].ID); err != nil || noClient.Client != nil {
		t.Fatalf("未带客户端信息的反馈不应下发 client，得到 %+v / %v", noClient, err)
	}

	// ── 普通工单的附件绑定：别人的、已用过的都拒绝 ──
	otherFile, err := upload(sessB, "file", "b.txt", "", []byte("from b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateByUser(ctx, sessA, ticketdomain.CreateCommand{Title: "附件", Content: "看附件", AttachmentIDs: []int64{otherFile.ID}}); errCode(err) != errCodeTicketAttachmentUnavailable {
		t.Fatalf("用户建单挂别人的附件应被拒，得到 %v", err)
	}
	if _, err := svc.ReplyByUser(ctx, sessA, ticketdomain.ReplyCommand{TicketID: userTicket.ID, Content: "补充", AttachmentIDs: []int64{otherFile.ID}}); errCode(err) != errCodeTicketAttachmentUnavailable {
		t.Fatalf("用户回复挂别人的附件应被拒，得到 %v", err)
	}
	var otherTicket *int64
	if err := pool.QueryRow(ctx, `SELECT ticket_id FROM ticket_attachments WHERE id = $1`, otherFile.ID).Scan(&otherTicket); err != nil || otherTicket != nil {
		t.Fatalf("被拒后附件仍应未绑定，得到 %v / %v", otherTicket, err)
	}

	// ── 管理端：按类型筛选、统计、分类 kind ──
	feedbackOnly, err := svc.List(ctx, admin, ticketdomain.ListQuery{Kind: "feedback", IncludeClosed: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ticketOnly, err := svc.List(ctx, admin, ticketdomain.ListQuery{Kind: "ticket", IncludeClosed: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	all, err := svc.List(ctx, admin, ticketdomain.ListQuery{IncludeClosed: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if feedbackOnly.Total != 21 || ticketOnly.Total != 1 || all.Total != 22 {
		t.Fatalf("管理端按类型筛选不对：feedback=%d ticket=%d all=%d", feedbackOnly.Total, ticketOnly.Total, all.Total)
	}
	for _, item := range feedbackOnly.Items {
		if item.ID == created.ID && (item.ImageCount != 5 || item.AttachmentCount != 2 || item.Kind != "feedback") {
			t.Fatalf("管理端列表的计数不对：images=%d files=%d kind=%s", item.ImageCount, item.AttachmentCount, item.Kind)
		}
	}
	stats, err := svc.Stats(ctx, admin, nil, "feedback")
	if err != nil || stats.Total != 21 {
		t.Fatalf("反馈统计应为 21，得到 %+v / %v", stats, err)
	}
	newCategory, err := svc.SaveCategory(ctx, admin, ticketdomain.Category{AppID: appID, Key: "fb_perf", Name: "性能", Kind: "feedback", UserSubmittable: true, Enabled: true})
	if err != nil || newCategory.Kind != "feedback" {
		t.Fatalf("新建反馈分类失败：%+v / %v", newCategory, err)
	}
	newCategory.Kind = ""
	newCategory.Name = "性能问题"
	kept, err := svc.SaveCategory(ctx, admin, *newCategory)
	if err != nil || kept.Kind != "feedback" || kept.Name != "性能问题" {
		t.Fatalf("更新不传 kind 应保持原值：%+v / %v", kept, err)
	}
	kept.Kind = "ticket"
	switched, err := svc.SaveCategory(ctx, admin, *kept)
	if err != nil || switched.Kind != "ticket" {
		t.Fatalf("更新应能改 kind：%+v / %v", switched, err)
	}
	if _, err := svc.SaveCategory(ctx, admin, ticketdomain.Category{AppID: appID, Key: "bad", Name: "x", Kind: "weird"}); errCode(err) != 40000 {
		t.Fatalf("非法 kind 应被拒，得到 %v", err)
	}
}

func insertOldFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appID, userID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := pool.Exec(ctx, `INSERT INTO tickets (kind, ticket_no, appid, requester_type, requester_user_id, title, source, created_at)
VALUES ('feedback', $1, $2, 'user', $3, '早些时候', 'app', NOW() - INTERVAL '3 hours')`, fmt.Sprintf("OLD%d-%d", userID, i), appID, userID); err != nil {
			t.Fatal(err)
		}
	}
}

// tinyPNG 1×1 透明 PNG。
func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
}
