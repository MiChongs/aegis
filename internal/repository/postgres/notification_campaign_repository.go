package postgres

// 站内信群发任务（docs/notification-center.md「群发任务」）。
//
// 写入路径只有 DeliverNotificationCampaignBatch：锁住任务行 → 一条语句按游标取下一批目标、写消息、写回执
// → 推进游标与计数（→ 发完就结束任务），全部在同一事务。于是：
//   - 进度与写入要么一起提交要么一起回滚，接手中断的任务从游标继续，不重不漏；
//   - (campaign_id, user_id) 唯一索引兜底，重放同一批也只会跳过（skipped），不会重复投递；
//   - 取消 / 撤回要改同一行，拿不到行锁就得等这一批写完——撤回开始之后不会再有新消息写进来。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	notificationdomain "aegis/internal/domain/notification"

	"github.com/jackc/pgx/v5"
)

// ErrNotificationCampaignNotFound 任务不存在（或不属于这个应用）。
var ErrNotificationCampaignNotFound = errors.New("notification campaign not found")

const notificationCampaignColumns = `c.id, c.appid, c.request_id, c.type, c.category, c.title, c.content, c.level,
	COALESCE(c.metadata, '{}'::jsonb), c.action, c.image, c.expires_at, c.audience, c.audience_label, c.status, c.scheduled_at,
	c.total, c.max_user_id, c.cursor, c.processed, c.delivered, c.skipped, c.recalled, c.attempts, c.message, c.operator,
	c.started_at, c.finished_at, c.recalled_at, c.heartbeat_at, c.created_at, c.updated_at`

func scanNotificationCampaign(row interface{ Scan(dest ...any) error }) (*notificationdomain.Campaign, error) {
	var c notificationdomain.Campaign
	var metadata, action, audience []byte
	if err := row.Scan(&c.ID, &c.AppID, &c.RequestID, &c.Type, &c.Category, &c.Title, &c.Content, &c.Level,
		&metadata, &action, &c.Image, &c.ExpiresAt, &audience, &c.AudienceLabel, &c.Status, &c.ScheduledAt,
		&c.Total, &c.MaxUserID, &c.Cursor, &c.Processed, &c.Delivered, &c.Skipped, &c.Recalled, &c.Attempts, &c.Message, &c.Operator,
		&c.StartedAt, &c.FinishedAt, &c.RecalledAt, &c.HeartbeatAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(metadata, &c.Metadata)
	_ = json.Unmarshal(audience, &c.Audience)
	c.Action = decodeNotificationAction(action)
	c.CategoryLabel = notificationdomain.CategoryLabel(c.Category)
	c.StatusLabel = notificationdomain.CampaignStatusLabel(c.Status)
	return &c, nil
}

// CreateNotificationCampaign 建一个任务。带 requestId 且同一应用已经有这个键的任务时不新建，
// 返回已有的那个与 created=false。owner 非空即建成已被它认领、正在执行的任务（旧群发接口在请求里就地发完）。
func (r *Repository) CreateNotificationCampaign(ctx context.Context, in notificationdomain.CampaignInput, status, owner string) (
	*notificationdomain.Campaign, bool, error) {
	metadata := in.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, false, err
	}
	var actionJSON []byte
	if in.Action != nil {
		if actionJSON, err = json.Marshal(in.Action); err != nil {
			return nil, false, err
		}
	}
	audienceJSON, err := json.Marshal(in.Audience)
	if err != nil {
		return nil, false, err
	}
	var requestID *string
	if id := strings.TrimSpace(in.RequestID); id != "" {
		requestID = &id
	}
	campaign, err := scanNotificationCampaign(r.pool.QueryRow(ctx, `INSERT INTO notification_campaigns AS c
    (appid, request_id, type, category, title, content, level, metadata, action, image, expires_at, audience, audience_label,
     status, scheduled_at, operator, owner, heartbeat_at, attempts, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12, $13, $14, $15, $16, $17::varchar,
    CASE WHEN $17::varchar <> '' THEN NOW() END, CASE WHEN $17::varchar <> '' THEN 1 ELSE 0 END,
    CASE WHEN $17::varchar <> '' THEN NOW() END)
ON CONFLICT (appid, request_id) WHERE request_id IS NOT NULL DO NOTHING
RETURNING `+notificationCampaignColumns,
		in.AppID, requestID, in.Type, in.Category, in.Title, in.Content, in.Level, metadataJSON, actionJSON, in.Image, in.ExpiresAt,
		audienceJSON, truncateColumn(notificationdomain.AudienceLabel(in.Audience), 255), status, in.ScheduledAt,
		truncateColumn(in.Operator, 128), owner))
	if errors.Is(err, pgx.ErrNoRows) && requestID != nil {
		existing, err := scanNotificationCampaign(r.pool.QueryRow(ctx, `SELECT `+notificationCampaignColumns+`
FROM notification_campaigns c WHERE c.appid = $1 AND c.request_id = $2`, in.AppID, *requestID))
		if err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return campaign, true, nil
}

// GetNotificationCampaign 一个任务；不存在返回 ErrNotificationCampaignNotFound。
func (r *Repository) GetNotificationCampaign(ctx context.Context, appID, campaignID int64) (*notificationdomain.Campaign, error) {
	campaign, err := scanNotificationCampaign(r.pool.QueryRow(ctx, `SELECT `+notificationCampaignColumns+`
FROM notification_campaigns c WHERE c.id = $1 AND c.appid = $2`, campaignID, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationCampaignNotFound
	}
	return campaign, err
}

// ListNotificationCampaigns 任务列表（新的在前）。
func (r *Repository) ListNotificationCampaigns(ctx context.Context, query notificationdomain.CampaignQuery) ([]notificationdomain.Campaign, int64, error) {
	where := "c.appid = $1"
	args := []any{query.AppID}
	if query.Status != "" {
		args = append(args, query.Status)
		where += " AND c.status = $" + strconv.Itoa(len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_campaigns c WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+notificationCampaignColumns+` FROM notification_campaigns c WHERE `+where+
		fmt.Sprintf(" ORDER BY c.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]notificationdomain.Campaign, 0, query.Limit)
	for rows.Next() {
		campaign, err := scanNotificationCampaign(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *campaign)
	}
	return items, total, rows.Err()
}

// NotificationCampaignReadCount 任务里已读的人数（详情现算，走 (campaign_id, user_id) 索引）。
func (r *Repository) NotificationCampaignReadCount(ctx context.Context, campaignID int64) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = n.user_id
WHERE n.campaign_id = $1 AND nr.status = 'read'`, campaignID).Scan(&n)
	return n, err
}

// ClaimNotificationCampaigns 认领要干活的任务：排队的、到点的定时任务、心跳过期的发送中 / 撤回中任务。
// 发送的认领次数用完的不再认领（由 FailExhaustedNotificationCampaigns 判失败）；撤回只是删除，不限次数。
func (r *Repository) ClaimNotificationCampaigns(ctx context.Context, owner string, staleBefore time.Time, maxAttempts, limit int) (
	[]notificationdomain.Campaign, error) {
	rows, err := r.pool.Query(ctx, `UPDATE notification_campaigns AS c SET
    status = CASE WHEN c.status IN ('scheduled', 'queued') THEN 'running' ELSE c.status END,
    owner = $1, heartbeat_at = NOW(), attempts = c.attempts + 1,
    started_at = COALESCE(c.started_at, NOW()), updated_at = NOW()
WHERE c.id IN (
    SELECT id FROM notification_campaigns
    WHERE (status = 'queued'
        OR (status = 'scheduled' AND scheduled_at <= NOW())
        OR (status IN ('running', 'recalling') AND (heartbeat_at IS NULL OR heartbeat_at < $2)))
      AND (status = 'recalling' OR attempts < $3)
    ORDER BY id LIMIT $4 FOR UPDATE SKIP LOCKED)
RETURNING `+notificationCampaignColumns, owner, staleBefore, maxAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notificationdomain.Campaign{}
	for rows.Next() {
		campaign, err := scanNotificationCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *campaign)
	}
	return out, rows.Err()
}

// FailExhaustedNotificationCampaigns 发送被中断太多次、又没人在跑的任务判失败。已经写进去的消息保留（可撤回）。
func (r *Repository) FailExhaustedNotificationCampaigns(ctx context.Context, staleBefore time.Time, maxAttempts int) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notification_campaigns SET status = 'failed', owner = '', finished_at = NOW(), updated_at = NOW(),
    message = '发送多次被中断，已停止重试；已发出的消息保留'
WHERE attempts >= $2 AND (status = 'queued' OR (status = 'running' AND (heartbeat_at IS NULL OR heartbeat_at < $1)))`,
		staleBefore, maxAttempts)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// BeginNotificationCampaign 第一次开始发送时定格人群：人数（估计值）与这一刻最大的用户 ID。
// 已经定格过的（被接手的任务）原样返回。
func (r *Repository) BeginNotificationCampaign(ctx context.Context, campaign notificationdomain.Campaign, owner string) (
	*notificationdomain.Campaign, error) {
	if campaign.MaxUserID != nil {
		return &campaign, nil
	}
	args := []any{campaign.AppID}
	joins, where := notificationAudienceSQL(campaign.Audience, &args)
	var total, maxUserID int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(u.id), 0) FROM users u`+joins+`
WHERE u.appid = $1 AND `+where, args...).Scan(&total, &maxUserID); err != nil {
		return nil, err
	}
	if limit := campaign.Audience.Limit; limit > 0 && total > limit {
		total = limit
	}
	updated, err := scanNotificationCampaign(r.pool.QueryRow(ctx, `UPDATE notification_campaigns AS c SET
    total = COALESCE(c.total, $3), max_user_id = COALESCE(c.max_user_id, $4), heartbeat_at = NOW(), updated_at = NOW()
WHERE c.id = $1 AND c.owner = $2
RETURNING `+notificationCampaignColumns, campaign.ID, owner, total, maxUserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotificationCampaignNotFound
	}
	return updated, err
}

// NotificationCampaignBatch 一批投递的结果。
type NotificationCampaignBatch struct {
	// Status 这一批之后任务的状态；不是 running 就该停下（发完、被取消、被撤回、被别的实例接手时为空串）
	Status string
	// Scanned 这一批扫过的目标人数；Inserted 真正写入的消息
	Scanned  int64
	Inserted []NotificationCampaignDelivery
	// CreatedAt 这一批消息的创建时间（同一事务内相同）
	CreatedAt time.Time
}

// NotificationCampaignDelivery 写进去的一条。
type NotificationCampaignDelivery struct {
	ID     int64
	UserID int64
}

// DeliverNotificationCampaignBatch 投递下一批（最多 batchSize 人）。只在任务仍是 running 且归 owner 时写入；
// 扫到的人数不足一批、或到了人数上限，就在同一事务里把任务结束为 succeeded。
func (r *Repository) DeliverNotificationCampaignBatch(ctx context.Context, campaign notificationdomain.Campaign, owner string,
	batchSize int) (NotificationCampaignBatch, error) {
	var out NotificationCampaignBatch
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status, current string
	var cursor, processed int64
	var maxUserID *int64
	if err := tx.QueryRow(ctx, `SELECT status, owner, cursor, processed, max_user_id FROM notification_campaigns WHERE id = $1 FOR UPDATE`,
		campaign.ID).Scan(&status, &current, &cursor, &processed, &maxUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if status != notificationdomain.CampaignRunning || current != owner {
		if current != owner {
			status = ""
		}
		out.Status = status
		return out, nil
	}
	if maxUserID == nil {
		return out, fmt.Errorf("notification campaign %d has not been started", campaign.ID)
	}
	take := int64(batchSize)
	if limit := campaign.Audience.Limit; limit > 0 {
		take = min(take, limit-processed)
	}
	if take <= 0 {
		out.Status, err = finishNotificationCampaignTx(ctx, tx, campaign.ID)
		if err != nil {
			return out, err
		}
		return out, tx.Commit(ctx)
	}

	metadataJSON, err := json.Marshal(nonNilMetadata(campaign.Metadata))
	if err != nil {
		return out, err
	}
	var actionJSON []byte
	if campaign.Action != nil {
		if actionJSON, err = json.Marshal(campaign.Action); err != nil {
			return out, err
		}
	}
	image := ""
	if campaign.Image != nil {
		image = *campaign.Image
	}
	// $1 appid  $2 campaign  $3 cursor  $4 max_user_id  $5 take  $6.. 内容  之后是人群条件
	args := []any{campaign.AppID, campaign.ID, cursor, *maxUserID, take,
		campaign.Type, campaign.Category, campaign.Title, campaign.Content, campaign.Level, metadataJSON, actionJSON, image, campaign.ExpiresAt}
	joins, where := notificationAudienceSQL(campaign.Audience, &args)
	var lastID *int64
	var ids, userIDs []int64
	var createdAt *time.Time
	if err := tx.QueryRow(ctx, `WITH targets AS (
    SELECT u.id FROM users u`+joins+`
    WHERE u.appid = $1 AND u.id > $3 AND u.id <= $4 AND `+where+`
    ORDER BY u.id LIMIT $5
), inserted AS (
    INSERT INTO notifications (appid, user_id, campaign_id, type, category, title, content, level, status,
        metadata, action, image, expires_at, created_at, updated_at)
    SELECT $1::bigint, t.id, $2::bigint, $6::varchar, $7::varchar, $8::varchar, $9::text, $10::varchar, 'unread',
        $11::jsonb, $12::jsonb, NULLIF($13::text, ''), $14::timestamptz, NOW(), NOW() FROM targets t
    ON CONFLICT (campaign_id, user_id) WHERE campaign_id IS NOT NULL DO NOTHING
    RETURNING id, user_id, created_at
), receipts AS (
    INSERT INTO notification_receipts (notification_id, user_id, status, created_at, updated_at)
    SELECT i.id, i.user_id, 'delivered', NOW(), NOW() FROM inserted i
)
SELECT (SELECT COUNT(*) FROM targets), (SELECT MAX(id) FROM targets),
    COALESCE((SELECT array_agg(i.id ORDER BY i.user_id) FROM inserted i), '{}'),
    COALESCE((SELECT array_agg(i.user_id ORDER BY i.user_id) FROM inserted i), '{}'),
    (SELECT MIN(created_at) FROM inserted)`, args...).Scan(&out.Scanned, &lastID, &ids, &userIDs, &createdAt); err != nil {
		return out, err
	}
	out.Inserted = make([]NotificationCampaignDelivery, len(ids))
	for i := range ids {
		out.Inserted[i] = NotificationCampaignDelivery{ID: ids[i], UserID: userIDs[i]}
	}
	if createdAt != nil {
		out.CreatedAt = *createdAt
	}
	nextCursor := cursor
	if lastID != nil {
		nextCursor = *lastID
	}
	inserted := int64(len(ids))
	if _, err := tx.Exec(ctx, `UPDATE notification_campaigns SET cursor = $2, processed = processed + $3, delivered = delivered + $4,
    skipped = skipped + $5, heartbeat_at = NOW(), updated_at = NOW()
WHERE id = $1`, campaign.ID, nextCursor, out.Scanned, inserted, out.Scanned-inserted); err != nil {
		return out, err
	}
	out.Status = notificationdomain.CampaignRunning
	reachedLimit := campaign.Audience.Limit > 0 && processed+out.Scanned >= campaign.Audience.Limit
	if out.Scanned < take || reachedLimit {
		if out.Status, err = finishNotificationCampaignTx(ctx, tx, campaign.ID); err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}

func finishNotificationCampaignTx(ctx context.Context, tx pgx.Tx, campaignID int64) (string, error) {
	_, err := tx.Exec(ctx, `UPDATE notification_campaigns SET status = 'succeeded', owner = '', finished_at = NOW(), updated_at = NOW()
WHERE id = $1`, campaignID)
	return notificationdomain.CampaignSucceeded, err
}

func nonNilMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

// RecallNotificationCampaignBatch 撤回：删掉下一批（最多 batchSize 条）这个任务写进去的消息，按用户返回删掉的 ID。
// 删完就在同一事务里把任务结束为 recalled。只在任务仍是 recalling 且归 owner 时动手。
func (r *Repository) RecallNotificationCampaignBatch(ctx context.Context, campaignID int64, owner string, batchSize int) (
	string, []NotificationCampaignDelivery, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, current string
	if err := tx.QueryRow(ctx, `SELECT status, owner FROM notification_campaigns WHERE id = $1 FOR UPDATE`, campaignID).
		Scan(&status, &current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, nil
		}
		return "", nil, err
	}
	if status != notificationdomain.CampaignRecalling || current != owner {
		if current != owner {
			status = ""
		}
		return status, nil, nil
	}
	var ids, userIDs []int64
	if err := tx.QueryRow(ctx, `WITH victims AS (
    SELECT id FROM notifications WHERE campaign_id = $1 ORDER BY user_id LIMIT $2
), removed AS (
    DELETE FROM notifications n USING victims v WHERE n.id = v.id RETURNING n.id, n.user_id
)
SELECT COALESCE(array_agg(id ORDER BY user_id), '{}'), COALESCE(array_agg(user_id ORDER BY user_id), '{}') FROM removed`,
		campaignID, batchSize).Scan(&ids, &userIDs); err != nil {
		return "", nil, err
	}
	status = notificationdomain.CampaignRecalling
	query := `UPDATE notification_campaigns SET recalled = recalled + $2, heartbeat_at = NOW(), updated_at = NOW() WHERE id = $1`
	if len(ids) < batchSize {
		status = notificationdomain.CampaignRecalled
		query = `UPDATE notification_campaigns SET recalled = recalled + $2, status = 'recalled', owner = '', recalled_at = NOW(),
    finished_at = COALESCE(finished_at, NOW()), updated_at = NOW() WHERE id = $1`
	}
	if _, err := tx.Exec(ctx, query, campaignID, len(ids)); err != nil {
		return "", nil, err
	}
	removed := make([]NotificationCampaignDelivery, len(ids))
	for i := range ids {
		removed[i] = NotificationCampaignDelivery{ID: ids[i], UserID: userIDs[i]}
	}
	return status, removed, tx.Commit(ctx)
}

// FailNotificationCampaign 执行方判失败（内容过期、应用被治理等）。只在任务仍归 owner 时生效。
func (r *Repository) FailNotificationCampaign(ctx context.Context, campaignID int64, owner, message string) error {
	_, err := r.pool.Exec(ctx, `UPDATE notification_campaigns SET status = 'failed', owner = '', finished_at = NOW(), updated_at = NOW(),
    message = $3 WHERE id = $1 AND owner = $2 AND status = 'running'`, campaignID, owner, truncateColumn(message, 2000))
	return err
}

// ReleaseNotificationCampaign 实例关闭时把手上的任务放回去（不必等心跳过期），这次认领不算次数。
func (r *Repository) ReleaseNotificationCampaign(ctx context.Context, campaignID int64, owner string) error {
	_, err := r.pool.Exec(ctx, `UPDATE notification_campaigns SET owner = '', heartbeat_at = NULL, attempts = GREATEST(attempts - 1, 0),
    updated_at = NOW() WHERE id = $1 AND owner = $2 AND status IN ('running', 'recalling')`, campaignID, owner)
	return err
}

// CancelNotificationCampaign 取消定时、排队或发送中的任务；已经写进去的消息保留。返回是否取消了。
func (r *Repository) CancelNotificationCampaign(ctx context.Context, appID, campaignID int64, operator string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notification_campaigns SET status = 'cancelled', owner = '', finished_at = NOW(), updated_at = NOW(),
    message = $3 WHERE id = $1 AND appid = $2 AND status IN ('scheduled', 'queued', 'running')`,
		campaignID, appID, truncateColumn("已由 "+operator+" 取消", 2000))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RecallNotificationCampaign 开始撤回：发送中的任务会在这一批写完后停下（行锁保证撤回开始之后不会再写入）。
// 返回是否进入了撤回。
func (r *Repository) RecallNotificationCampaign(ctx context.Context, appID, campaignID int64, operator string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notification_campaigns SET status = 'recalling', owner = '', heartbeat_at = NULL, attempts = 0,
    finished_at = COALESCE(finished_at, NOW()), updated_at = NOW(), message = $3
WHERE id = $1 AND appid = $2 AND status IN ('running', 'succeeded', 'failed', 'cancelled')`,
		campaignID, appID, truncateColumn("已由 "+operator+" 撤回", 2000))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ── 人群 ──

// notificationAudienceSQL 目标人群的 JOIN 与 WHERE（别名 u = users，$1 已是 appid）。只在用到时才连等级与资料表。
func notificationAudienceSQL(a notificationdomain.Audience, args *[]any) (string, string) {
	add := func(clause string, value any) string {
		*args = append(*args, value)
		return strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(*args)))
	}
	joins := ""
	where := []string{}
	switch a.Account {
	case notificationdomain.AccountAll:
	case notificationdomain.AccountDisabled:
		where = append(where, "NOT "+leaderboardEligibleSQL)
	default:
		where = append(where, leaderboardEligibleSQL)
	}
	if len(a.ExcludeUserIDs) > 0 {
		where = append(where, add("NOT (u.id = ANY(?))", a.ExcludeUserIDs))
	}
	switch a.Scope {
	case notificationdomain.AudienceUsers:
		where = append(where, add("u.id = ANY(?)", a.UserIDs))
	case notificationdomain.AudienceFilter:
		switch a.Membership {
		case notificationdomain.MembershipMember:
			where = append(where, "(COALESCE(u.vip_expire_at > NOW(), FALSE) OR u.vip_lifetime_at IS NOT NULL)")
		case notificationdomain.MembershipNonMember:
			where = append(where, "NOT (COALESCE(u.vip_expire_at > NOW(), FALSE) OR u.vip_lifetime_at IS NOT NULL)")
		case notificationdomain.MembershipLifetime:
			where = append(where, "u.vip_lifetime_at IS NOT NULL")
		case notificationdomain.MembershipNonLifetime:
			where = append(where, "u.vip_lifetime_at IS NULL")
		}
		if a.RegisteredWithinDays > 0 {
			where = append(where, add("u.created_at >= NOW() - make_interval(days => ?)", a.RegisteredWithinDays))
		}
		if a.RegisteredBeforeDays > 0 {
			where = append(where, add("u.created_at <= NOW() - make_interval(days => ?)", a.RegisteredBeforeDays))
		}
		const activeSince = `(EXISTS (SELECT 1 FROM user_login_devices d WHERE d.appid = u.appid AND d.user_id = u.id
      AND d.last_seen_at >= NOW() - make_interval(days => ?))
   OR EXISTS (SELECT 1 FROM user_online_daily o WHERE o.user_id = u.id AND o.updated_at >= NOW() - make_interval(days => ?)))`
		if a.ActiveWithinDays > 0 {
			where = append(where, add(activeSince, a.ActiveWithinDays))
		}
		if a.InactiveDays > 0 {
			where = append(where, "NOT "+add(activeSince, a.InactiveDays))
		}
		if a.MinLevel > 0 || a.MaxLevel > 0 {
			joins += "\nLEFT JOIN user_level_records lv ON lv.user_id = u.id AND lv.appid = u.appid"
			if a.MinLevel > 0 {
				where = append(where, add("COALESCE(lv.current_level, 1) >= ?", a.MinLevel))
			}
			if a.MaxLevel > 0 {
				where = append(where, add("COALESCE(lv.current_level, 1) <= ?", a.MaxLevel))
			}
		}
		if len(a.Platforms) > 0 {
			where = append(where, add(`EXISTS (SELECT 1 FROM user_login_devices pd WHERE pd.appid = u.appid AND pd.user_id = u.id
      AND pd.platform = ANY(?))`, a.Platforms))
		}
		if a.Keyword != "" {
			joins += "\nLEFT JOIN user_profiles p ON p.user_id = u.id"
			like := add("?", "%"+escapeLike(a.Keyword)+"%")
			exact := add("?", a.Keyword)
			where = append(where, fmt.Sprintf(`(u.account ILIKE %[1]s OR COALESCE(p.nickname, '') ILIKE %[1]s
      OR COALESCE(p.email, '') ILIKE %[1]s OR COALESCE(p.phone, '') ILIKE %[1]s OR CAST(u.id AS TEXT) = %[2]s)`, like, exact))
		}
	}
	if len(where) == 0 {
		return joins, "TRUE"
	}
	return joins, strings.Join(where, " AND ")
}

// CountNotificationAudience 人群的人数（受人数上限约束）。
func (r *Repository) CountNotificationAudience(ctx context.Context, appID int64, audience notificationdomain.Audience) (int64, error) {
	args := []any{appID}
	joins, where := notificationAudienceSQL(audience, &args)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users u`+joins+` WHERE u.appid = $1 AND `+where, args...).Scan(&total); err != nil {
		return 0, err
	}
	if audience.Limit > 0 && total > audience.Limit {
		total = audience.Limit
	}
	return total, nil
}

// SampleNotificationAudience 人群里最近注册的几个人（预览用；按主键倒序，不做全表随机）。
func (r *Repository) SampleNotificationAudience(ctx context.Context, appID int64, audience notificationdomain.Audience, limit int) (
	[]notificationdomain.AudienceSample, error) {
	args := []any{appID}
	joins, where := notificationAudienceSQL(audience, &args)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, `SELECT s.id, s.account, COALESCE(sp.nickname, ''), s.created_at,
    (COALESCE(s.vip_expire_at > NOW(), FALSE) OR s.vip_lifetime_at IS NOT NULL),
    COALESCE(slv.current_level, 1),
    GREATEST((SELECT MAX(d.last_seen_at) FROM user_login_devices d WHERE d.appid = s.appid AND d.user_id = s.id),
             (SELECT MAX(o.updated_at) FROM user_online_daily o WHERE o.user_id = s.id))
FROM (SELECT u.* FROM users u`+joins+` WHERE u.appid = $1 AND `+where+` ORDER BY u.id DESC LIMIT $`+strconv.Itoa(len(args))+`) s
LEFT JOIN user_profiles sp ON sp.user_id = s.id
LEFT JOIN user_level_records slv ON slv.user_id = s.id AND slv.appid = s.appid
ORDER BY s.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []notificationdomain.AudienceSample{}
	for rows.Next() {
		var sample notificationdomain.AudienceSample
		if err := rows.Scan(&sample.UserID, &sample.Account, &sample.Nickname, &sample.RegisteredAt, &sample.Member, &sample.Level,
			&sample.LastActiveAt); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

// ── 批量实时事件用 ──

// NotificationUnreadCountsBatch 一批用户各自的总未读与按分类未读（一条语句，走 (appid, user_id, ...) 索引）。
// 没有未读的人不在结果里（计数为 0）。
func (r *Repository) NotificationUnreadCountsBatch(ctx context.Context, appID int64, userIDs []int64) (map[int64]notificationdomain.Counts, error) {
	out := make(map[int64]notificationdomain.Counts, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT n.user_id, n.category, COUNT(*) FROM notifications n
JOIN notification_receipts nr ON nr.notification_id = n.id AND nr.user_id = n.user_id
WHERE n.appid = $1 AND n.user_id = ANY($2) AND nr.status <> 'read' AND `+notificationLive+`
GROUP BY n.user_id, n.category`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID, n int64
		var category string
		if err := rows.Scan(&userID, &category, &n); err != nil {
			return nil, err
		}
		counts, ok := out[userID]
		if !ok {
			counts = notificationdomain.Counts{CategoryUnread: map[string]int64{}}
		}
		if n > 0 {
			counts.CategoryUnread[category] = n
			counts.Unread += n
		}
		out[userID] = counts
	}
	return out, rows.Err()
}

// NotificationPushMutedUsers 一批用户里关掉了这个分类提醒的人。
func (r *Repository) NotificationPushMutedUsers(ctx context.Context, appID int64, category string, userIDs []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT user_id FROM notification_preferences
WHERE appid = $1 AND category = $2 AND push = FALSE AND user_id = ANY($3)`, appID, category, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		out[userID] = true
	}
	return out, rows.Err()
}
