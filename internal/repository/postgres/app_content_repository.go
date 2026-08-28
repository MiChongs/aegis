package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"

	"github.com/jackc/pgx/v5"
)

// 应用级内容（Banner + 公告）的数据访问。
//
// 两张表的查询原本散在 repository.go 里，随公告补上生命周期之后列数翻了一倍，
// 单独成文件是为了让「展示端看到什么」与「管理端看到什么」这两条链路挨在一起 ——
// 它们的过滤条件必须互为镜像，隔着三千行代码对不上。
//
// **单行查询一律用「查不到 = (nil, nil)」表达**，与本包其余仓储一致（见
// repository.go 的 GetAppCreator / normalizeNotFound）。把 pgx.ErrNoRows 原样
// 抛给上层会绕过 service 的 `item == nil` 分支，最后由 writeError 当成 500
// 把驱动的英文错误串直接画到管理员的提示条上（"no rows in result set"）——
// 那句话既不说明是哪条记录，也不告诉管理员该怎么办。

const bannerColumns = `id, COALESCE(header, ''), title, COALESCE(content, ''), COALESCE(url, ''), type, position, status, start_time, end_time, view_count, click_count, created_at, updated_at`

const noticeColumns = `id, COALESCE(title, ''), content, COALESCE(summary, ''), type, level, status, pinned, start_time, end_time, published_at, view_count, created_by, created_at, updated_at`

func scanBanner(row interface{ Scan(dest ...any) error }) (*appdomain.Banner, error) {
	var item appdomain.Banner
	if err := row.Scan(&item.ID, &item.Header, &item.Title, &item.Content, &item.URL, &item.Type, &item.Position, &item.Status, &item.StartTime, &item.EndTime, &item.ViewCount, &item.ClickCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func scanNotice(row interface{ Scan(dest ...any) error }) (*appdomain.Notice, error) {
	var item appdomain.Notice
	if err := row.Scan(&item.ID, &item.Title, &item.Content, &item.Summary, &item.Type, &item.Level, &item.Status, &item.Pinned, &item.StartTime, &item.EndTime, &item.PublishedAt, &item.ViewCount, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// scanBannerRow / scanNoticeRow 是单行查询专用：查不到返回 (nil, nil)，
// 与本包其余仓储共用 normalizeNotFound 这一个口径。
//
// 单独包一层而不是把 normalizeNotFound 塞进 scanBanner / scanNotice：
// 那两个还要喂给 rows.Next() 循环，在那里把错误抹成 nil 会让紧接着的
// `*item` 变成空指针解引用。
//
// 用在 `UPDATE … RETURNING` 上同样成立 —— 匹配不到行意味着「要改的那条已经
// 不在了」，这是一个 404，不是一次数据库故障。
func scanBannerRow(row pgx.Row) (*appdomain.Banner, error) {
	item, err := scanBanner(row)
	return item, normalizeNotFound(err)
}

func scanNoticeRow(row pgx.Row) (*appdomain.Notice, error) {
	item, err := scanNotice(row)
	return item, normalizeNotFound(err)
}

/* ───────────────────────── Banner ───────────────────────── */

// ListActiveBanners 展示端：启用中且落在投放窗口内的 Banner。
// 顺带累加曝光数 —— 展示端每取一次就是一次曝光，让客户端另外上报会漏掉大半。
func (r *Repository) ListActiveBanners(ctx context.Context, appID int64, now time.Time) ([]appdomain.Banner, error) {
	query := `SELECT ` + bannerColumns + `
FROM banners
WHERE appid = $1
  AND status = true
  AND (start_time IS NULL OR start_time <= $2)
  AND (end_time IS NULL OR end_time >= $2)
ORDER BY position ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, appID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Banner, 0, 8)
	ids := make([]int64, 0, 8)
	for rows.Next() {
		item, err := scanBanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		// 曝光计数不参与业务判定，写失败不该让展示端拿不到内容。
		_, _ = r.pool.Exec(ctx, `UPDATE banners SET view_count = view_count + 1 WHERE id = ANY($1)`, ids)
	}
	return items, nil
}

// ListBanners 管理端：按过滤条件返回全部 Banner（不分页，见 BannerFilter 的说明）。
func (r *Repository) ListBanners(ctx context.Context, appID int64, filter appdomain.BannerFilter) ([]appdomain.Banner, error) {
	conditions := []string{"appid = $1"}
	args := []any{appID}
	if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.Type); trimmed != "" {
		args = append(args, trimmed)
		conditions = append(conditions, fmt.Sprintf("type = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		conditions = append(conditions, fmt.Sprintf("(title ILIKE $%d OR COALESCE(content, '') ILIKE $%d OR COALESCE(url, '') ILIKE $%d)", len(args), len(args), len(args)))
	}

	query := `SELECT ` + bannerColumns + ` FROM banners WHERE ` + strings.Join(conditions, " AND ") + ` ORDER BY position ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Banner, 0, 8)
	for rows.Next() {
		item, err := scanBanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetBannerByID 查不到返回 (nil, nil)。
func (r *Repository) GetBannerByID(ctx context.Context, appID int64, bannerID int64) (*appdomain.Banner, error) {
	query := `SELECT ` + bannerColumns + ` FROM banners WHERE appid = $1 AND id = $2 LIMIT 1`
	return scanBannerRow(r.pool.QueryRow(ctx, query, appID, bannerID))
}

// UpsertBanner 新建或更新。
//
// header / content / url 一律原样写入空串，**不要**再套 nullableString：
// 000084 之后这三列是 NOT NULL DEFAULT ''，把 '' 转成 NULL 会直接撞 23502。
// 「没填」在这个模块里只有空串一种表示法，读取端的 COALESCE 只是为了兼容
// 迁移尚未落地的库。
func (r *Repository) UpsertBanner(ctx context.Context, appID int64, item appdomain.Banner) (*appdomain.Banner, error) {
	if item.ID > 0 {
		query := `UPDATE banners
SET header = $3,
	title = $4,
	content = $5,
	url = $6,
	type = $7,
	position = $8,
	status = $9,
	start_time = $10,
	end_time = $11,
	updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING ` + bannerColumns
		// 更新匹配不到行 → (nil, nil)：那条 Banner 已被别人删掉，由 service 转成 404。
		return scanBannerRow(r.pool.QueryRow(ctx, query, appID, item.ID, item.Header, item.Title, item.Content, item.URL, item.Type, item.Position, item.Status, item.StartTime, item.EndTime))
	}

	// 新建时若未指定顺序，落到当前末位之后，而不是与既有 Banner 抢 position=0。
	query := `INSERT INTO banners (appid, header, title, content, url, type, position, status, start_time, end_time, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
RETURNING ` + bannerColumns
	return scanBanner(r.pool.QueryRow(ctx, query, appID, item.Header, item.Title, item.Content, item.URL, item.Type, item.Position, item.Status, item.StartTime, item.EndTime))
}

// NextBannerPosition 返回该应用当前最大 position + 1。
func (r *Repository) NextBannerPosition(ctx context.Context, appID int64) (int, error) {
	var next int
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(MAX(position), -1) + 1 FROM banners WHERE appid = $1`, appID).Scan(&next)
	return next, err
}

func (r *Repository) DeleteBanner(ctx context.Context, appID int64, bannerID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM banners WHERE appid = $1 AND id = $2`, appID, bannerID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteBanners(ctx context.Context, appID int64, bannerIDs []int64) (int64, error) {
	if len(bannerIDs) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM banners WHERE appid = $1 AND id = ANY($2)`, appID, bannerIDs)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// ReorderBanners 按传入顺序重写 position，整体在一个事务里。
//
// 逐条 UPDATE 且不加事务时，中途失败会留下一半新顺序一半旧顺序 ——
// 那种状态下拖拽界面显示的顺序和客户端拿到的顺序对不上，而且没有任何报错。
// 返回真正被改动的行数：传进来的 id 可能已被别人删掉。
func (r *Repository) ReorderBanners(ctx context.Context, appID int64, bannerIDs []int64) (int64, error) {
	if len(bannerIDs) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for index, id := range bannerIDs {
		batch.Queue(`UPDATE banners SET position = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`, appID, id, index)
	}
	results := tx.SendBatch(ctx, batch)
	var affected int64
	for range bannerIDs {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return 0, err
		}
		affected += tag.RowsAffected()
	}
	if err := results.Close(); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return affected, nil
}

// IncrementBannerClick 点击上报。返回 false 表示该 Banner 不存在或不属于这个应用。
func (r *Repository) IncrementBannerClick(ctx context.Context, appID int64, bannerID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `UPDATE banners SET click_count = click_count + 1 WHERE appid = $1 AND id = $2`, appID, bannerID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

/* ───────────────────────── 公告 ───────────────────────── */

// ListActiveNotices 展示端：已发布且落在投放窗口内，置顶优先。
//
// 与管理端列表刻意用不同的排序：管理端按更新时间看「我刚改了什么」，
// 展示端按发布时间看「用户该先看到什么」。
func (r *Repository) ListActiveNotices(ctx context.Context, appID int64, now time.Time) ([]appdomain.Notice, error) {
	query := `SELECT ` + noticeColumns + `
FROM notices
WHERE appid = $1
  AND status = 'published'
  AND (start_time IS NULL OR start_time <= $2)
  AND (end_time IS NULL OR end_time >= $2)
ORDER BY pinned DESC, COALESCE(published_at, created_at) DESC, id DESC`
	rows, err := r.pool.Query(ctx, query, appID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]appdomain.Notice, 0, 8)
	ids := make([]int64, 0, 8)
	for rows.Next() {
		item, err := scanNotice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		_, _ = r.pool.Exec(ctx, `UPDATE notices SET view_count = view_count + 1 WHERE id = ANY($1)`, ids)
	}
	return items, nil
}

// ListNotices 管理端：分页 + 过滤，同时返回符合条件的总数。
func (r *Repository) ListNotices(ctx context.Context, appID int64, filter appdomain.NoticeFilter) ([]appdomain.Notice, int64, error) {
	conditions := []string{"appid = $1"}
	args := []any{appID}
	if trimmed := strings.TrimSpace(filter.Status); trimmed != "" {
		args = append(args, trimmed)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.Type); trimmed != "" {
		args = append(args, trimmed)
		conditions = append(conditions, fmt.Sprintf("type = $%d", len(args)))
	}
	if trimmed := strings.TrimSpace(filter.Level); trimmed != "" {
		args = append(args, trimmed)
		conditions = append(conditions, fmt.Sprintf("level = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		// 关键词匹配标题与**摘要**而不是正文：正文是 HTML，搜「重要」会命中 <strong> 之类的标签名。
		conditions = append(conditions, fmt.Sprintf("(COALESCE(title, '') ILIKE $%d OR COALESCE(summary, '') ILIKE $%d)", len(args), len(args)))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notices WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []appdomain.Notice{}, 0, nil
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	query := fmt.Sprintf(
		`SELECT %s FROM notices WHERE %s ORDER BY pinned DESC, updated_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		noticeColumns, where, len(args)-1, len(args),
	)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]appdomain.Notice, 0, limit)
	for rows.Next() {
		item, err := scanNotice(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// GetNoticeByID 查不到返回 (nil, nil)。
func (r *Repository) GetNoticeByID(ctx context.Context, appID int64, noticeID int64) (*appdomain.Notice, error) {
	query := `SELECT ` + noticeColumns + ` FROM notices WHERE appid = $1 AND id = $2 LIMIT 1`
	return scanNoticeRow(r.pool.QueryRow(ctx, query, appID, noticeID))
}

func (r *Repository) UpsertNotice(ctx context.Context, appID int64, item appdomain.Notice) (*appdomain.Notice, error) {
	if item.ID > 0 {
		query := `UPDATE notices
SET title = $3,
	content = $4,
	summary = $5,
	type = $6,
	level = $7,
	status = $8,
	pinned = $9,
	start_time = $10,
	end_time = $11,
	published_at = $12,
	updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING ` + noticeColumns
		return scanNoticeRow(r.pool.QueryRow(ctx, query, appID, item.ID, item.Title, item.Content, item.Summary,
			item.Type, item.Level, item.Status, item.Pinned, item.StartTime, item.EndTime, item.PublishedAt))
	}

	query := `INSERT INTO notices (appid, title, content, summary, type, level, status, pinned, start_time, end_time, published_at, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
RETURNING ` + noticeColumns
	return scanNotice(r.pool.QueryRow(ctx, query, appID, item.Title, item.Content, item.Summary,
		item.Type, item.Level, item.Status, item.Pinned, item.StartTime, item.EndTime, item.PublishedAt, item.CreatedBy))
}

func (r *Repository) DeleteNotice(ctx context.Context, appID int64, noticeID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM notices WHERE appid = $1 AND id = $2`, appID, noticeID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) DeleteNotices(ctx context.Context, appID int64, noticeIDs []int64) (int64, error) {
	if len(noticeIDs) == 0 {
		return 0, nil
	}
	result, err := r.pool.Exec(ctx, `DELETE FROM notices WHERE appid = $1 AND id = ANY($2)`, appID, noticeIDs)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

/* ───────────────────────── 总览 ───────────────────────── */

// ContentOverview 一次查询取齐两张表的计数。
//
// 分成两条 SQL 会在页面上出现「Banner 那栏已经刷新、公告那栏还是上一次」的画面，
// 而管理员会照着它做投放决定。投放态（生效 / 待开始 / 已结束）在 SQL 里判，
// 与展示端 ListActiveBanners 用的是同一组条件。
func (r *Repository) ContentOverview(ctx context.Context, appID int64, now time.Time) (*appdomain.ContentOverview, error) {
	query := `
WITH banner_stat AS (
    SELECT
        COUNT(*)                                                          AS total,
        COUNT(*) FILTER (WHERE status AND (start_time IS NULL OR start_time <= $2)
                                      AND (end_time IS NULL OR end_time >= $2)) AS live,
        COUNT(*) FILTER (WHERE status AND start_time IS NOT NULL AND start_time > $2) AS scheduled,
        COUNT(*) FILTER (WHERE status AND end_time IS NOT NULL AND end_time < $2)     AS expired,
        COUNT(*) FILTER (WHERE NOT status)                                AS disabled,
        COALESCE(SUM(view_count), 0)                                      AS views,
        COALESCE(SUM(click_count), 0)                                     AS clicks
    FROM banners WHERE appid = $1
), notice_stat AS (
    SELECT
        COUNT(*)                                                AS total,
        COUNT(*) FILTER (WHERE status = 'published')            AS published,
        COUNT(*) FILTER (WHERE status = 'draft')                AS draft,
        COUNT(*) FILTER (WHERE status = 'archived')             AS archived,
        COUNT(*) FILTER (WHERE pinned AND status = 'published') AS pinned,
        COALESCE(SUM(view_count), 0)                            AS views,
        MAX(published_at) FILTER (WHERE status = 'published')    AS last_published_at
    FROM notices WHERE appid = $1
)
SELECT b.total, b.live, b.scheduled, b.expired, b.disabled, b.views, b.clicks,
       n.total, n.published, n.draft, n.archived, n.pinned, n.views, n.last_published_at
FROM banner_stat b, notice_stat n`

	var overview appdomain.ContentOverview
	err := r.pool.QueryRow(ctx, query, appID, now).Scan(
		&overview.BannerTotal, &overview.BannerLive, &overview.BannerScheduled, &overview.BannerExpired,
		&overview.BannerDisabled, &overview.BannerViews, &overview.BannerClicks,
		&overview.NoticeTotal, &overview.NoticePublished, &overview.NoticeDraft, &overview.NoticeArchived,
		&overview.NoticePinned, &overview.NoticeViews, &overview.LastPublishedAt,
	)
	if err != nil {
		return nil, err
	}
	return &overview, nil
}
