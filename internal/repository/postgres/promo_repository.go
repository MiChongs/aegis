package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
	"aegis/internal/domain/promo"
)

// 推广活动（docs/promo.md）：活动的增删改查、邀请档位领取、分享提交与审核、排行与统计。

var (
	ErrPromoCampaignNotFound   = errors.New("promo campaign not found")
	ErrPromoSubmissionNotFound = errors.New("promo submission not found")
	// ErrPromoCampaignInUse 上线中，或已经有人领取 / 提交过的活动不能删
	ErrPromoCampaignInUse = errors.New("promo campaign in use")
)

// PromoUnavailableError 判定不通过，Reason 是 promo 包里的 reason。
type PromoUnavailableError struct{ Reason string }

func (e *PromoUnavailableError) Error() string { return "promo unavailable: " + e.Reason }

// PromoRefs 发放时要用的套餐与功能引用。
type PromoRefs struct {
	Plans    map[int64]pointstore.PlanRef
	Features map[string]pointstore.FeatureRef
}

// effectiveInviteStatuses 计入推广活动人数的邀请人奖励状态：已发放，或条件达成但邀请人奖励到了上限。
const effectiveInviteStatuses = `('granted','limited')`

// ── 活动 ──

const promoCampaignColumns = `pc.id, pc.appid, pc.title, pc.subtitle, pc.rules_text, pc.status, pc.starts_at, pc.ends_at,
pc.config, COALESCE(pc.created_by, ''), COALESCE(pc.updated_by, ''), pc.created_at, pc.updated_at`

func scanPromoCampaign(row interface{ Scan(dest ...any) error }, extra ...any) (*promo.Campaign, error) {
	var item promo.Campaign
	var raw []byte
	dest := append([]any{&item.ID, &item.AppID, &item.Title, &item.Subtitle, &item.RulesText, &item.Status,
		&item.StartsAt, &item.EndsAt, &raw, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPromoCampaignNotFound
		}
		return nil, err
	}
	cfg := promo.DefaultConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("decode promo config: %w", err)
		}
	}
	item.Config = promo.NormalizeConfig(cfg)
	return &item, nil
}

// CreatePromoCampaign 新建一期活动（草稿）。
func (r *Repository) CreatePromoCampaign(ctx context.Context, input promo.CampaignInput) (*promo.Campaign, error) {
	raw, err := json.Marshal(input.Config)
	if err != nil {
		return nil, err
	}
	return scanPromoCampaign(r.pool.QueryRow(ctx, `INSERT INTO promo_campaigns AS pc
  (appid, title, subtitle, rules_text, status, starts_at, ends_at, config, created_by, updated_by)
VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8, $8) RETURNING `+promoCampaignColumns,
		input.AppID, input.Title, input.Subtitle, input.RulesText, input.StartsAt, input.EndsAt, raw, nullableString(input.Operator)))
}

// UpdatePromoCampaign 修改活动内容（不改状态）。
func (r *Repository) UpdatePromoCampaign(ctx context.Context, campaignID int64, input promo.CampaignInput) (*promo.Campaign, error) {
	raw, err := json.Marshal(input.Config)
	if err != nil {
		return nil, err
	}
	return scanPromoCampaign(r.pool.QueryRow(ctx, `UPDATE promo_campaigns AS pc SET title = $3, subtitle = $4, rules_text = $5,
  starts_at = $6, ends_at = $7, config = $8, updated_by = $9, updated_at = NOW()
WHERE pc.appid = $1 AND pc.id = $2 RETURNING `+promoCampaignColumns,
		input.AppID, campaignID, input.Title, input.Subtitle, input.RulesText, input.StartsAt, input.EndsAt, raw,
		nullableString(input.Operator)))
}

// GetPromoCampaign 取一期活动。
func (r *Repository) GetPromoCampaign(ctx context.Context, appID, campaignID int64) (*promo.Campaign, error) {
	return scanPromoCampaign(r.pool.QueryRow(ctx, `SELECT `+promoCampaignColumns+` FROM promo_campaigns pc
WHERE pc.appid = $1 AND pc.id = $2`, appID, campaignID))
}

// SetPromoCampaignStatus 上线 / 下线 / 退回草稿。
func (r *Repository) SetPromoCampaignStatus(ctx context.Context, appID, campaignID int64, status, operator string) (*promo.Campaign, error) {
	return scanPromoCampaign(r.pool.QueryRow(ctx, `UPDATE promo_campaigns AS pc SET status = $3, updated_by = $4, updated_at = NOW()
WHERE pc.appid = $1 AND pc.id = $2 RETURNING `+promoCampaignColumns, appID, campaignID, status, nullableString(operator)))
}

// DeletePromoCampaign 删除草稿或下线的活动；有人领取或提交过的不能删。
func (r *Repository) DeletePromoCampaign(ctx context.Context, appID, campaignID int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM promo_campaigns WHERE appid = $1 AND id = $2 FOR UPDATE`,
		appID, campaignID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPromoCampaignNotFound
		}
		return err
	}
	if status == promo.StatusOnline {
		return ErrPromoCampaignInUse
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM promo_tier_grants WHERE campaign_id = $1)
  OR EXISTS (SELECT 1 FROM promo_submissions WHERE campaign_id = $1)`, campaignID).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrPromoCampaignInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM promo_campaigns WHERE appid = $1 AND id = $2`, appID, campaignID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListPromoCampaigns 管理端活动列表，新的在前，带简要统计。
func (r *Repository) ListPromoCampaigns(ctx context.Context, query promo.CampaignQuery, now time.Time) (*promo.Page[promo.AdminCampaign], error) {
	where := []string{"pc.appid = $1"}
	args := []any{query.AppID}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("pc.status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(pc.title ILIKE $%d OR pc.subtitle ILIKE $%d)", len(args), len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM promo_campaigns pc`+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+promoCampaignColumns+`,
  (SELECT COUNT(*) FROM (SELECT user_id FROM promo_tier_grants WHERE campaign_id = pc.id
     UNION SELECT user_id FROM promo_submissions WHERE campaign_id = pc.id) AS people),
  (SELECT COUNT(*) FROM promo_tier_grants WHERE campaign_id = pc.id),
  (SELECT COUNT(*) FROM promo_submissions WHERE campaign_id = pc.id AND status = 'pending'),
  (SELECT COUNT(*) FROM promo_submissions WHERE campaign_id = pc.id AND status = 'approved')
FROM promo_campaigns pc`+clause+fmt.Sprintf(" ORDER BY pc.starts_at DESC, pc.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]promo.AdminCampaign, 0)
	for rows.Next() {
		var item promo.AdminCampaign
		campaign, err := scanPromoCampaign(rows, &item.Participants, &item.TierClaims, &item.PendingSubmissions, &item.ApprovedSubmission)
		if err != nil {
			return nil, err
		}
		item.Campaign = *campaign
		item.Phase = promo.Phase(*campaign, now)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invitePage(items, total, page, limit), nil
}

// ListOnlinePromoCampaigns 已上线、领取期还没过的活动（用户端列表），开始时间早的在前。
func (r *Repository) ListOnlinePromoCampaigns(ctx context.Context, appID int64, now time.Time) ([]promo.Campaign, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+promoCampaignColumns+` FROM promo_campaigns pc
WHERE pc.appid = $1 AND pc.status = 'online' AND pc.ends_at > $2
ORDER BY pc.starts_at, pc.id`, appID, now.Add(-promo.MaxClaimGraceDays*24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]promo.Campaign, 0)
	for rows.Next() {
		campaign, err := scanPromoCampaign(rows)
		if err != nil {
			return nil, err
		}
		if promo.VisibleToUsers(*campaign, now) {
			out = append(out, *campaign)
		}
	}
	return out, rows.Err()
}

// ── 邀请人数与档位 ──

func promoInviteCountsQ(ctx context.Context, q queryExecutor, campaign promo.Campaign, userID int64) (int64, int64, error) {
	var count, pending int64
	err := q.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE inviter_status IN `+effectiveInviteStatuses+`),
  COUNT(*) FILTER (WHERE inviter_status = 'pending')
FROM invite_relations WHERE appid = $1 AND inviter_id = $2 AND created_at >= $3 AND created_at < $4`,
		campaign.AppID, userID, campaign.StartsAt, campaign.EndsAt).Scan(&count, &pending)
	return count, pending, err
}

// PromoInviteCounts 活动时间内我的有效邀请人数与待生效人数。
func (r *Repository) PromoInviteCounts(ctx context.Context, campaign promo.Campaign, userID int64) (int64, int64, error) {
	return promoInviteCountsQ(ctx, r.pool, campaign, userID)
}

// PromoTierClaims 我领过的档位（人数 → 领取时间）。
func (r *Repository) PromoTierClaims(ctx context.Context, campaignID, userID int64) (map[int]time.Time, error) {
	rows, err := r.pool.Query(ctx, `SELECT tier_count, created_at FROM promo_tier_grants
WHERE campaign_id = $1 AND user_id = $2`, campaignID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]time.Time{}
	for rows.Next() {
		var count int
		var at time.Time
		if err := rows.Scan(&count, &at); err != nil {
			return nil, err
		}
		out[count] = at
	}
	return out, rows.Err()
}

// lockPromoUserTx 锁住用户行：同一个人的领取、提交串行，计数才准。
func lockPromoUserTx(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE appid = $1 AND id = $2 FOR UPDATE`, appID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	return err
}

// ClaimPromoTier 领取一档：锁用户 → 重算人数 → 判定 → 记领取 → 发奖励，一个事务。
func (r *Repository) ClaimPromoTier(ctx context.Context, campaign promo.Campaign, userID int64, tierCount int,
	refs PromoRefs, now time.Time) (*promo.TierGrant, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockPromoUserTx(ctx, tx, campaign.AppID, userID); err != nil {
		return nil, err
	}
	// 活动行用共享锁重读：运营同时改档位时，按改之前或之后的一份算，不会一半一半
	current, err := scanPromoCampaign(tx.QueryRow(ctx, `SELECT `+promoCampaignColumns+` FROM promo_campaigns pc
WHERE pc.appid = $1 AND pc.id = $2 FOR SHARE`, campaign.AppID, campaign.ID))
	if err != nil {
		return nil, err
	}
	facts := promo.TierFacts{}
	if facts.Count, _, err = promoInviteCountsQ(ctx, tx, *current, userID); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM promo_tier_grants
WHERE campaign_id = $1 AND user_id = $2 AND tier_count = $3)`, current.ID, userID, tierCount).Scan(&facts.Claimed); err != nil {
		return nil, err
	}
	if reason := promo.EvaluateTier(*current, tierCount, facts, now); reason != "" {
		return nil, &PromoUnavailableError{Reason: reason}
	}
	tier, _ := promo.FindTier(current.Config, tierCount)
	if !invite.RewardsResolvable(tier.Rewards, refs.Plans, refs.Features) {
		return nil, &PromoUnavailableError{Reason: promo.ReasonRewardUnavailable}
	}
	var grantID int64
	if err := tx.QueryRow(ctx, `INSERT INTO promo_tier_grants (appid, campaign_id, user_id, tier_count)
VALUES ($1, $2, $3, $4) RETURNING id`, current.AppID, current.ID, userID, tierCount).Scan(&grantID); err != nil {
		if IsUniqueViolation(err) {
			return nil, &PromoUnavailableError{Reason: promo.ReasonTierClaimed}
		}
		return nil, err
	}
	results, err := r.grantPromoRewardsTx(ctx, tx, *current, userID, "PRT"+strconv.FormatInt(grantID, 10),
		"promo-tier:"+strconv.FormatInt(grantID, 10), promo.TierTitle(tier), refs, tier.Rewards,
		map[string]any{"promoTierGrantId": grantID, "tierCount": tierCount})
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(results)
	grant := &promo.TierGrant{CampaignID: current.ID, Count: tierCount, Results: results}
	if err := tx.QueryRow(ctx, `UPDATE promo_tier_grants SET results = $2 WHERE id = $1 RETURNING created_at`,
		grantID, raw).Scan(&grant.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return grant, nil
}

// grantPromoRewardsTx 推广活动的发放：与邀请同一个发放函数、同一套账本，流水标题带活动名。
func (r *Repository) grantPromoRewardsTx(ctx context.Context, tx pgx.Tx, campaign promo.Campaign, userID int64,
	ref, key, what string, refs PromoRefs, rewards []promo.Reward, meta map[string]any) ([]promo.RewardResult, error) {
	meta["promoCampaignId"] = campaign.ID
	results, err := r.grantInviteRewardsTx(ctx, tx, inviteGrant{
		AppID: campaign.AppID, UserID: userID, Key: key, Ref: ref, Meta: meta,
		Title: promo.IntegralTitlePrefix + campaign.Title + "（" + what + "）",
		Plans: refs.Plans, Features: refs.Features,
	}, rewards)
	var unavailable *InviteUnavailableError
	if errors.As(err, &unavailable) {
		return nil, &PromoUnavailableError{Reason: promo.ReasonRewardUnavailable}
	}
	return results, err
}

// ── 分享提交 ──

const promoSubmissionColumns = `ps.id, ps.appid, ps.campaign_id, ps.user_id, ps.platform, ps.url, ps.url_key, ps.note, ps.status,
ps.reject_reason, ps.results, COALESCE(ps.reviewed_by, ''), ps.reviewed_at, COALESCE(ps.device_id, ''), COALESCE(ps.client_ip, ''),
ps.created_at, ps.updated_at`

func scanPromoSubmission(row interface{ Scan(dest ...any) error }, extra ...any) (*promo.Submission, error) {
	var item promo.Submission
	var raw []byte
	dest := append([]any{&item.ID, &item.AppID, &item.CampaignID, &item.UserID, &item.Platform, &item.URL, &item.URLKey,
		&item.Note, &item.Status, &item.RejectReason, &raw, &item.ReviewedBy, &item.ReviewedAt, &item.DeviceID, &item.ClientIP,
		&item.CreatedAt, &item.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPromoSubmissionNotFound
		}
		return nil, err
	}
	item.Results = []promo.RewardResult{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &item.Results); err != nil {
			return nil, fmt.Errorf("decode promo results: %w", err)
		}
	}
	item.PlatformName = promo.PlatformName(item.Platform)
	return &item, nil
}

func promoSubmitFactsQ(ctx context.Context, q queryExecutor, campaignID, userID int64, platform, urlKey string,
	dayStart time.Time) (promo.SubmitFacts, error) {
	var facts promo.SubmitFacts
	err := q.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE created_at >= $3),
  COUNT(*) FILTER (WHERE status = 'approved'),
  COUNT(*) FILTER (WHERE status = 'pending'),
  COUNT(*) FILTER (WHERE status <> 'rejected' AND platform = $4)
FROM promo_submissions WHERE campaign_id = $1 AND user_id = $2`, campaignID, userID, dayStart, platform).
		Scan(&facts.TodaySubmitted, &facts.Approved, &facts.Pending, &facts.PlatformTaken)
	if err != nil || urlKey == "" {
		return facts, err
	}
	err = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM promo_submissions
WHERE campaign_id = $1 AND url_key = $2 AND status <> 'rejected')`, campaignID, urlKey).Scan(&facts.Duplicate)
	return facts, err
}

// PromoSubmitFacts 用户端展示用的次数（不加锁）。
func (r *Repository) PromoSubmitFacts(ctx context.Context, campaignID, userID int64, dayStart time.Time) (promo.SubmitFacts, error) {
	return promoSubmitFactsQ(ctx, r.pool, campaignID, userID, "", "", dayStart)
}

// CreatePromoSubmission 提交一篇文章：锁用户 → 取齐次数 → 判定 → 写入，一个事务。
// linkReason 是 promo.ParseLink 的结论（空为可用）。
func (r *Repository) CreatePromoSubmission(ctx context.Context, campaign promo.Campaign, input promo.SubmitInput,
	link promo.Link, linkReason string, dayStart, now time.Time) (*promo.Submission, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockPromoUserTx(ctx, tx, input.AppID, input.UserID); err != nil {
		return nil, err
	}
	facts, err := promoSubmitFactsQ(ctx, tx, campaign.ID, input.UserID, input.Platform, link.Key, dayStart)
	if err != nil {
		return nil, err
	}
	if reason := promo.EvaluateSubmit(campaign, input.Platform, linkReason, facts, now); reason != "" {
		return nil, &PromoUnavailableError{Reason: reason}
	}
	item, err := scanPromoSubmission(tx.QueryRow(ctx, `INSERT INTO promo_submissions AS ps
  (appid, campaign_id, user_id, platform, url, url_key, note, device_id, client_ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+promoSubmissionColumns,
		input.AppID, campaign.ID, input.UserID, input.Platform, link.URL, link.Key, input.Note,
		nullableString(input.DeviceID), nullableString(input.ClientIP)))
	if err != nil {
		if IsUniqueViolation(err) {
			// 别人同时提交了同一篇
			return nil, &PromoUnavailableError{Reason: promo.ReasonLinkDuplicate}
		}
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return item, nil
}

// ListMyPromoSubmissions 我在一期活动里的提交，新的在前。
func (r *Repository) ListMyPromoSubmissions(ctx context.Context, campaignID, userID int64, page, limit int) (*promo.Page[promo.Submission], error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM promo_submissions WHERE campaign_id = $1 AND user_id = $2`,
		campaignID, userID).Scan(&total); err != nil {
		return nil, err
	}
	page, limit = adPolicyPaging(page, limit)
	rows, err := r.pool.Query(ctx, `SELECT `+promoSubmissionColumns+` FROM promo_submissions ps
WHERE ps.campaign_id = $1 AND ps.user_id = $2 ORDER BY ps.id DESC LIMIT $3 OFFSET $4`, campaignID, userID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]promo.Submission, 0)
	for rows.Next() {
		item, err := scanPromoSubmission(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invitePage(items, total, page, limit), nil
}

// ListPromoSubmissions 管理端提交列表（带用户与活动），新的在前。
func (r *Repository) ListPromoSubmissions(ctx context.Context, query promo.SubmissionQuery) (*promo.Page[promo.Submission], error) {
	where := []string{"ps.appid = $1"}
	args := []any{query.AppID}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if query.CampaignID > 0 {
		add("ps.campaign_id = $%d", query.CampaignID)
	}
	if query.UserID > 0 {
		add("ps.user_id = $%d", query.UserID)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		add("ps.status = $%d", status)
	}
	if platform := strings.TrimSpace(query.Platform); platform != "" {
		add("ps.platform = $%d", platform)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(ps.url ILIKE $%d OR ps.note ILIKE $%d OR u.account ILIKE $%d OR p.nickname ILIKE $%d)", n, n, n, n))
	}
	joins := ` FROM promo_submissions ps
LEFT JOIN promo_campaigns pc ON pc.id = ps.campaign_id
LEFT JOIN users u ON u.id = ps.user_id LEFT JOIN user_profiles p ON p.user_id = ps.user_id`
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+joins+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+promoSubmissionColumns+`, COALESCE(pc.title, ''), COALESCE(u.account, ''),
  COALESCE(p.nickname, ''), COALESCE(p.avatar, '')`+joins+clause+
		fmt.Sprintf(" ORDER BY ps.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]promo.Submission, 0)
	for rows.Next() {
		var title, account, nickname, avatar string
		item, err := scanPromoSubmission(rows, &title, &account, &nickname, &avatar)
		if err != nil {
			return nil, err
		}
		item.CampaignTitle, item.UserAccount, item.UserNickname, item.UserAvatar = title, account, nickname, avatar
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invitePage(items, total, page, limit), nil
}

// ReviewPromoSubmission 审核一条提交。通过时再核一次篇数上限并发奖励；只处理待审核的。
// campaigns 按 ID 缓存活动（批量审核里同一期活动只读一次），由调用方传入。
// 判定不通过时也返回 outcome（带 CampaignID），调用方据此写说明。
func (r *Repository) ReviewPromoSubmission(ctx context.Context, appID, submissionID int64, action, reason, operator string,
	campaigns map[int64]*promo.Campaign, refs PromoRefs) (*promo.ReviewOutcome, error) {
	outcome := &promo.ReviewOutcome{ID: submissionID}
	// 先读出用户，按「用户 → 提交」的顺序加锁，与提交时的锁顺序一致
	var userID, campaignID int64
	if err := r.pool.QueryRow(ctx, `SELECT user_id, campaign_id FROM promo_submissions WHERE appid = $1 AND id = $2`,
		appID, submissionID).Scan(&userID, &campaignID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPromoSubmissionNotFound
		}
		return nil, err
	}
	campaign, ok := campaigns[campaignID]
	if !ok {
		loaded, err := r.GetPromoCampaign(ctx, appID, campaignID)
		if err != nil {
			return nil, err
		}
		campaigns[campaignID], campaign = loaded, loaded
	}
	outcome.UserID, outcome.CampaignID, outcome.Title = userID, campaignID, campaign.Title

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockPromoUserTx(ctx, tx, appID, userID); err != nil {
		return nil, err
	}
	item, err := scanPromoSubmission(tx.QueryRow(ctx, `SELECT `+promoSubmissionColumns+` FROM promo_submissions ps
WHERE ps.appid = $1 AND ps.id = $2 FOR UPDATE`, appID, submissionID))
	if err != nil {
		return nil, err
	}
	if item.Status != promo.SubmissionPending {
		return outcome, &PromoUnavailableError{Reason: promo.ReasonAlreadyReviewed}
	}

	results := []promo.RewardResult{}
	status := promo.SubmissionRejected
	if action == promo.ReviewApprove {
		var approved, onPlatform int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE platform = $3)
FROM promo_submissions WHERE campaign_id = $1 AND user_id = $2 AND status = 'approved'`,
			campaignID, userID, item.Platform).Scan(&approved, &onPlatform); err != nil {
			return nil, err
		}
		if limited := promo.EvaluateApprove(campaign.Config.Share, approved, onPlatform); limited != "" {
			return outcome, &PromoUnavailableError{Reason: limited}
		}
		if !invite.RewardsResolvable(campaign.Config.Share.Rewards, refs.Plans, refs.Features) {
			return outcome, &PromoUnavailableError{Reason: promo.ReasonRewardUnavailable}
		}
		results, err = r.grantPromoRewardsTx(ctx, tx, *campaign, userID, "PRS"+strconv.FormatInt(item.ID, 10),
			"promo-share:"+strconv.FormatInt(item.ID, 10), promo.PlatformName(item.Platform)+"推广", refs,
			campaign.Config.Share.Rewards, map[string]any{"promoSubmissionId": item.ID})
		if err != nil {
			return outcome, err
		}
		status, reason = promo.SubmissionApproved, ""
	}
	raw, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE promo_submissions SET status = $2, reject_reason = $3, results = $4,
  reviewed_by = $5, reviewed_at = NOW(), updated_at = NOW() WHERE id = $1`,
		item.ID, status, reason, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	outcome.OK, outcome.Status, outcome.Results = true, status, results
	if status == promo.SubmissionRejected {
		outcome.Message = reason
	}
	return outcome, nil
}

// ── 排行与统计 ──

// PromoLeaderboard 活动时间内有效邀请人数的排行：人数降序，同人数先达到的在前。
// userID 非 0 时另算这个人的名次（不在前 N 名里也算）。
func (r *Repository) PromoLeaderboard(ctx context.Context, campaign promo.Campaign, limit int, userID int64) (*promo.Leaderboard, error) {
	board := &promo.Leaderboard{Items: []promo.RankEntry{}}
	// 达到人数的时间 = 最后一条有效邀请的时间
	const ranked = `WITH counts AS (
  SELECT inviter_id, COUNT(*) AS n, MAX(created_at) AS reached_at FROM invite_relations
  WHERE appid = $1 AND inviter_id IS NOT NULL AND created_at >= $2 AND created_at < $3
    AND inviter_status IN ` + effectiveInviteStatuses + `
  GROUP BY inviter_id
), ranked AS (
  SELECT inviter_id, n, reached_at, ROW_NUMBER() OVER (ORDER BY n DESC, reached_at, inviter_id) AS rank FROM counts
)`
	rows, err := r.pool.Query(ctx, ranked+`
SELECT rk.rank, rk.inviter_id, COALESCE(u.account, ''), COALESCE(p.nickname, ''), COALESCE(p.avatar, ''), rk.n, rk.reached_at
FROM ranked rk LEFT JOIN users u ON u.id = rk.inviter_id LEFT JOIN user_profiles p ON p.user_id = rk.inviter_id
WHERE rk.rank <= $4 ORDER BY rk.rank`, campaign.AppID, campaign.StartsAt, campaign.EndsAt, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry promo.RankEntry
		var nickname string
		if err := rows.Scan(&entry.Rank, &entry.UserID, &entry.Account, &nickname, &entry.RawAvatar, &entry.Count, &entry.ReachedAt); err != nil {
			return nil, err
		}
		entry.Nickname = invite.DisplayName(nickname, entry.Account)
		board.Items = append(board.Items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if userID != 0 {
		me := &promo.RankMe{}
		err := r.pool.QueryRow(ctx, ranked+` SELECT rank, n FROM ranked WHERE inviter_id = $4`,
			campaign.AppID, campaign.StartsAt, campaign.EndsAt, userID).Scan(&me.Rank, &me.Count)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		board.Me = me
	}
	return board, nil
}

// PromoStats 一期活动的统计。timezone 决定按天分桶。
func (r *Repository) PromoStats(ctx context.Context, campaign promo.Campaign, timezone string) (*promo.Stats, error) {
	stats := &promo.Stats{TierClaims: []promo.TierClaimStat{}, ByPlatform: []promo.PlatformStat{}, Daily: []promo.DailyCampaignRow{}}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE inviter_status IN `+effectiveInviteStatuses+`),
  COUNT(*) FILTER (WHERE inviter_status = 'pending')
FROM invite_relations WHERE appid = $1 AND inviter_id IS NOT NULL AND created_at >= $2 AND created_at < $3`,
		campaign.AppID, campaign.StartsAt, campaign.EndsAt).Scan(&stats.Invited, &stats.PendingInvites); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT user_id FROM promo_tier_grants WHERE campaign_id = $1
  UNION SELECT user_id FROM promo_submissions WHERE campaign_id = $1) AS people`, campaign.ID).Scan(&stats.Participants); err != nil {
		return nil, err
	}

	claimed := map[int]int64{}
	rows, err := r.pool.Query(ctx, `SELECT tier_count, COUNT(*) FROM promo_tier_grants WHERE campaign_id = $1 GROUP BY tier_count`, campaign.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var count int
		var n int64
		if err := rows.Scan(&count, &n); err != nil {
			rows.Close()
			return nil, err
		}
		claimed[count] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, tier := range campaign.Config.Invite.Tiers {
		stats.TierClaims = append(stats.TierClaims, promo.TierClaimStat{Count: tier.Count, Claimed: claimed[tier.Count]})
	}

	rows, err = r.pool.Query(ctx, `SELECT platform,
  COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE status = 'approved'), COUNT(*) FILTER (WHERE status = 'rejected')
FROM promo_submissions WHERE campaign_id = $1 GROUP BY platform ORDER BY platform`, campaign.ID)
	if err != nil {
		return nil, err
	}
	byPlatform := map[string]promo.PlatformStat{}
	for rows.Next() {
		var item promo.PlatformStat
		if err := rows.Scan(&item.Platform, &item.Pending, &item.Approved, &item.Rejected); err != nil {
			rows.Close()
			return nil, err
		}
		item.Name = promo.PlatformName(item.Platform)
		byPlatform[item.Platform] = item
		stats.Submissions.Pending += item.Pending
		stats.Submissions.Approved += item.Approved
		stats.Submissions.Rejected += item.Rejected
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 目录顺序在前，已停用却有提交的平台排在后面
	for _, platform := range promo.PlatformCatalog() {
		if item, ok := byPlatform[platform.Key]; ok {
			stats.ByPlatform = append(stats.ByPlatform, item)
			delete(byPlatform, platform.Key)
		} else if containsString(campaign.Config.Share.Platforms, platform.Key) {
			stats.ByPlatform = append(stats.ByPlatform, promo.PlatformStat{Platform: platform.Key, Name: platform.Name})
		}
	}
	for _, item := range byPlatform {
		stats.ByPlatform = append(stats.ByPlatform, item)
	}

	rows, err = r.pool.Query(ctx, `WITH days AS (
  SELECT to_char(d, 'YYYY-MM-DD') AS day FROM generate_series(
    date_trunc('day', $2::timestamptz AT TIME ZONE $4), date_trunc('day', LEAST($3::timestamptz, NOW()) AT TIME ZONE $4), interval '1 day') AS d
), inv AS (
  SELECT to_char(created_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day, COUNT(*) AS n FROM invite_relations
  WHERE appid = $1 AND inviter_id IS NOT NULL AND created_at >= $2 AND created_at < $3 AND inviter_status IN `+effectiveInviteStatuses+`
  GROUP BY 1
), sub AS (
  SELECT to_char(created_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day, COUNT(*) AS n FROM promo_submissions
  WHERE campaign_id = $5 GROUP BY 1
), apr AS (
  SELECT to_char(reviewed_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day, COUNT(*) AS n FROM promo_submissions
  WHERE campaign_id = $5 AND status = 'approved' AND reviewed_at IS NOT NULL GROUP BY 1
)
SELECT days.day, COALESCE(inv.n, 0), COALESCE(sub.n, 0), COALESCE(apr.n, 0)
FROM days LEFT JOIN inv ON inv.day = days.day LEFT JOIN sub ON sub.day = days.day LEFT JOIN apr ON apr.day = days.day
ORDER BY days.day`, campaign.AppID, campaign.StartsAt, campaign.EndsAt, timezone, campaign.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row promo.DailyCampaignRow
		if err := rows.Scan(&row.Date, &row.Invited, &row.Submitted, &row.Approved); err != nil {
			return nil, err
		}
		stats.Daily = append(stats.Daily, row)
	}
	return stats, rows.Err()
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
