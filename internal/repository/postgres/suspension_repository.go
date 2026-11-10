package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"

	"github.com/jackc/pgx/v5"
)

// 账号封禁的全站一面与申诉（docs/account-suspension.md）。

// ── 用户联系信息与语言 ──

// SuspensionContact 发封禁通知需要的账号信息
type SuspensionContact struct {
	AppID    int64
	AppName  string
	UserID   int64
	Account  string
	Nickname string
	Email    string
	Phone    string
	Locale   string
}

// SuspensionContactOf 账号的邮箱、昵称、语言（user_profiles.extra.locale）
func (r *Repository) SuspensionContactOf(ctx context.Context, appID, userID int64) (*SuspensionContact, error) {
	var c SuspensionContact
	err := r.pool.QueryRow(ctx, `SELECT u.appid, COALESCE(a.name, ''), u.id, u.account, COALESCE(p.nickname, ''), COALESCE(p.email, ''), COALESCE(p.phone, ''),
		COALESCE(p.extra->>'locale', '')
		FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id LEFT JOIN apps a ON a.id = u.appid
		WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&c.AppID, &c.AppName, &c.UserID, &c.Account, &c.Nickname, &c.Email, &c.Phone, &c.Locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// SetUserLocale 记下用户最近使用的语言（通知按它发）；没变就不写
func (r *Repository) SetUserLocale(ctx context.Context, userID int64, locale string) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_profiles SET extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object('locale', $1::text)
		WHERE user_id = $2 AND COALESCE(extra->>'locale', '') <> $1`, locale, userID)
	return err
}

// ── 账号的身份主体（全站封禁用） ──

// SubjectValues 一个账号的邮箱、手机号、第三方身份、设备原值
type SubjectValues struct {
	Emails     []string
	Phones     []string
	Identities []string // provider:provider_user_id
	Devices    []string
}

// SubjectValuesOf 一个账号绑定与用过的身份主体
func (r *Repository) SubjectValuesOf(ctx context.Context, appID, userID int64) (*SubjectValues, error) {
	var v SubjectValues
	var email, phone string
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(lower(trim(p.email)), ''), COALESCE(trim(p.phone), '') FROM users u
		LEFT JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&email, &phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if email != "" {
		v.Emails = append(v.Emails, email)
	}
	if phone != "" {
		v.Phones = append(v.Phones, phone)
	}
	rows, err := r.pool.Query(ctx, `SELECT lower(provider) || ':' || provider_user_id FROM oauth_bindings WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			v.Identities = append(v.Identities, s)
		}
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT device_key FROM user_login_devices WHERE appid = $1 AND user_id = $2
		AND device_key !~ '^[0\-:_ .]*$' AND device_key NOT ILIKE 'ua:%'
		AND lower(device_key) NOT IN ('9774d56d682e549c', 'unknown', 'null', 'nil', 'none', 'undefined')`, appID, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			v.Devices = append(v.Devices, s)
		}
	}
	rows.Close()
	return &v, nil
}

// AccountRef 一个账号
type AccountRef struct {
	AppID  int64
	UserID int64
}

// AccountsBySubjects 所有应用里用了这些邮箱、手机号、第三方身份、设备的账号
func (r *Repository) AccountsBySubjects(ctx context.Context, v SubjectValues) ([]AccountRef, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT u.appid, u.id FROM users u JOIN user_profiles p ON p.user_id = u.id
		WHERE cardinality($1::text[]) > 0 AND lower(trim(p.email)) = ANY($1::text[])
		UNION
		SELECT DISTINCT u.appid, u.id FROM users u JOIN user_profiles p ON p.user_id = u.id
		WHERE cardinality($2::text[]) > 0 AND trim(p.phone) = ANY($2::text[])
		UNION
		SELECT DISTINCT b.appid, b.user_id FROM oauth_bindings b
		WHERE cardinality($3::text[]) > 0 AND lower(b.provider) || ':' || b.provider_user_id = ANY($3::text[])
		UNION
		SELECT DISTINCT d.appid, d.user_id FROM user_login_devices d
		WHERE cardinality($4::text[]) > 0 AND d.device_key = ANY($4::text[])`,
		nonNilStrings(v.Emails), nonNilStrings(v.Phones), nonNilStrings(v.Identities), nonNilStrings(v.Devices))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountRef
	for rows.Next() {
		var a AccountRef
		if err := rows.Scan(&a.AppID, &a.UserID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── 全站封禁 ──

// SubjectDigest 一个主体的摘要
type SubjectDigest struct {
	Kind   string
	Digest string
	Masked string
}

const platformBanColumns = `pb.id, pb.ban_type, pb.status, pb.reason, pb.public_reason, pb.reason_code, pb.source, pb.appealable, pb.origin_app_id, pb.origin_user_id,
	pb.evidence, pb.created_by_admin_id, pb.created_by_name, pb.revoked_by_name, pb.revoke_reason, pb.start_at, pb.end_at, pb.revoked_at, pb.created_at`

func scanPlatformBan(row pgx.Row) (*suspension.PlatformBan, error) {
	var b suspension.PlatformBan
	var evidence []byte
	err := row.Scan(&b.ID, &b.BanType, &b.Status, &b.Reason, &b.PublicReason, &b.ReasonCode, &b.Source, &b.Appealable, &b.OriginAppID, &b.OriginUserID,
		&evidence, &b.CreatedByAdminID, &b.CreatedByName, &b.RevokedByName, &b.RevokeReason, &b.StartAt, &b.EndAt, &b.RevokedAt, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Evidence = map[string]any{}
	_ = json.Unmarshal(evidence, &b.Evidence)
	return &b, nil
}

// CreatePlatformBan 建一条全站封禁与它的主体
func (r *Repository) CreatePlatformBan(ctx context.Context, b suspension.PlatformBan, subjects []SubjectDigest) (*suspension.PlatformBan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	evidence, _ := json.Marshal(b.Evidence)
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO platform_bans (ban_type, reason, public_reason, reason_code, source, appealable, origin_app_id, origin_user_id,
		evidence, created_by_admin_id, created_by_name, start_at, end_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`,
		b.BanType, b.Reason, b.PublicReason, b.ReasonCode, b.Source, b.Appealable, b.OriginAppID, b.OriginUserID, evidence,
		b.CreatedByAdminID, b.CreatedByName, b.StartAt, b.EndAt).Scan(&id); err != nil {
		return nil, err
	}
	for _, s := range subjects {
		if _, err := tx.Exec(ctx, `INSERT INTO platform_ban_subjects (platform_ban_id, kind, digest, masked) VALUES ($1,$2,$3,$4)
			ON CONFLICT DO NOTHING`, id, s.Kind, s.Digest, s.Masked); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPlatformBan(ctx, id)
}

// GetPlatformBan 一条全站封禁（到期的顺手结掉）
func (r *Repository) GetPlatformBan(ctx context.Context, id int64) (*suspension.PlatformBan, error) {
	_, _ = r.pool.Exec(ctx, `UPDATE platform_bans SET status = 'expired', updated_at = NOW() WHERE id = $1 AND status = 'active' AND end_at IS NOT NULL AND end_at <= NOW()`, id)
	return scanPlatformBan(r.pool.QueryRow(ctx, `SELECT `+platformBanColumns+` FROM platform_bans pb WHERE pb.id = $1`, id))
}

// AddPlatformBanSubjects 给全站封禁补主体（之后用新的邮箱、设备撞上来的账号）
func (r *Repository) AddPlatformBanSubjects(ctx context.Context, id int64, subjects []SubjectDigest) error {
	for _, s := range subjects {
		if _, err := r.pool.Exec(ctx, `INSERT INTO platform_ban_subjects (platform_ban_id, kind, digest, masked) VALUES ($1,$2,$3,$4)
			ON CONFLICT DO NOTHING`, id, s.Kind, s.Digest, s.Masked); err != nil {
			return err
		}
	}
	return nil
}

// MatchPlatformBan 这些主体有没有撞上生效中的全站封禁（最早的一条）
func (r *Repository) MatchPlatformBan(ctx context.Context, subjects []SubjectDigest) (*suspension.PlatformBan, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	kinds := make([]string, len(subjects))
	digests := make([]string, len(subjects))
	for i, s := range subjects {
		kinds[i], digests[i] = s.Kind, s.Digest
	}
	return scanPlatformBan(r.pool.QueryRow(ctx, `SELECT `+platformBanColumns+` FROM platform_bans pb
		WHERE pb.status = 'active' AND (pb.end_at IS NULL OR pb.end_at > NOW())
		AND EXISTS (SELECT 1 FROM platform_ban_subjects s, unnest($1::text[], $2::text[]) AS q(kind, digest)
			WHERE s.platform_ban_id = pb.id AND s.kind = q.kind AND s.digest = q.digest)
		ORDER BY pb.id LIMIT 1`, kinds, digests))
}

// PlatformBanQuery 全站封禁列表
type PlatformBanQuery struct {
	Status   string
	Keyword  string
	Page     int
	PageSize int
}

// ListPlatformBans 全站封禁列表（带主体数与连带账号数）
func (r *Repository) ListPlatformBans(ctx context.Context, q PlatformBanQuery) ([]suspension.PlatformBan, int64, error) {
	_, _ = r.pool.Exec(ctx, `UPDATE platform_bans SET status = 'expired', updated_at = NOW() WHERE status = 'active' AND end_at IS NOT NULL AND end_at <= NOW()`)
	where := []string{"TRUE"}
	var args []any
	if q.Status != "" {
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("pb.status = $%d", len(args)))
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		where = append(where, fmt.Sprintf("(pb.reason ILIKE $%d OR pb.public_reason ILIKE $%d OR pb.id::text = trim(both '%%' from $%d)"+
			" OR EXISTS (SELECT 1 FROM user_account_bans b JOIN users u ON u.id = b.user_id WHERE b.platform_ban_id = pb.id AND (u.account ILIKE $%d OR u.id::text = trim(both '%%' from $%d))))",
			len(args), len(args), len(args), len(args), len(args)))
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM platform_bans pb WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	size := q.PageSize
	if size <= 0 || size > 100 {
		size = 20
	}
	page := max(q.Page, 1)
	args = append(args, size, (page-1)*size)
	rows, err := r.pool.Query(ctx, `SELECT `+platformBanColumns+`,
		(SELECT count(*) FROM platform_ban_subjects s WHERE s.platform_ban_id = pb.id),
		(SELECT count(*) FROM user_account_bans b WHERE b.platform_ban_id = pb.id AND b.status = 'active')
		FROM platform_bans pb WHERE `+cond+fmt.Sprintf(` ORDER BY pb.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []suspension.PlatformBan
	for rows.Next() {
		var b suspension.PlatformBan
		var evidence []byte
		if err := rows.Scan(&b.ID, &b.BanType, &b.Status, &b.Reason, &b.PublicReason, &b.ReasonCode, &b.Source, &b.Appealable, &b.OriginAppID, &b.OriginUserID,
			&evidence, &b.CreatedByAdminID, &b.CreatedByName, &b.RevokedByName, &b.RevokeReason, &b.StartAt, &b.EndAt, &b.RevokedAt, &b.CreatedAt,
			&b.SubjectCount, &b.AccountCount); err != nil {
			return nil, 0, err
		}
		b.Evidence = map[string]any{}
		_ = json.Unmarshal(evidence, &b.Evidence)
		out = append(out, b)
	}
	return out, total, rows.Err()
}

// PlatformBanSubjects 全站封禁的主体（打码）
func (r *Repository) PlatformBanSubjects(ctx context.Context, id int64) ([]suspension.PlatformBanSubject, error) {
	rows, err := r.pool.Query(ctx, `SELECT kind, masked FROM platform_ban_subjects WHERE platform_ban_id = $1 ORDER BY kind, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []suspension.PlatformBanSubject{}
	for rows.Next() {
		var s suspension.PlatformBanSubject
		if err := rows.Scan(&s.Kind, &s.Masked); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PlatformBanAccounts 全站封禁连带的账号封禁（最近的在前）
func (r *Repository) PlatformBanAccounts(ctx context.Context, id int64) ([]suspension.PlatformBanAccount, error) {
	rows, err := r.pool.Query(ctx, `SELECT b.appid, COALESCE(a.name, ''), b.user_id, u.account, COALESCE(p.nickname, ''), b.id, b.status
		FROM user_account_bans b JOIN users u ON u.id = b.user_id LEFT JOIN user_profiles p ON p.user_id = u.id LEFT JOIN apps a ON a.id = b.appid
		WHERE b.platform_ban_id = $1 ORDER BY b.created_at DESC LIMIT 500`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []suspension.PlatformBanAccount{}
	for rows.Next() {
		var a suspension.PlatformBanAccount
		if err := rows.Scan(&a.AppID, &a.AppName, &a.UserID, &a.Account, &a.Nickname, &a.BanID, &a.Status); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RevokePlatformBan 解除全站封禁；返回它连带的、仍生效的账号封禁（调用方逐个解除）
func (r *Repository) RevokePlatformBan(ctx context.Context, id int64, by userdomain.BanOperator, reason string) ([]userdomain.AccountBan, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE platform_bans SET status = 'revoked', revoked_by_admin_id = $2, revoked_by_name = $3, revoke_reason = $4,
		revoked_at = NOW(), updated_at = NOW() WHERE id = $1 AND status = 'active'`, id, nullableInt64(by.AdminID), by.AdminName, reason)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAccountBanNotActive
	}
	return r.ActiveBansOfPlatformBan(ctx, id)
}

// ActiveBansOfPlatformBan 全站封禁连带的、仍生效的账号封禁
func (r *Repository) ActiveBansOfPlatformBan(ctx context.Context, id int64) ([]userdomain.AccountBan, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+userAccountBanColumns+` FROM user_account_bans WHERE platform_ban_id = $1 AND status = 'active'`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []userdomain.AccountBan
	for rows.Next() {
		b, err := scanAccountBan(rows)
		if err != nil {
			return nil, err
		}
		if b != nil {
			out = append(out, *b)
		}
	}
	return out, rows.Err()
}

// GetUserAccountBan 一条账号封禁
func (r *Repository) GetUserAccountBan(ctx context.Context, appID, banID int64) (*userdomain.AccountBan, error) {
	return scanAccountBan(r.pool.QueryRow(ctx, `SELECT `+userAccountBanColumns+` FROM user_account_bans WHERE id = $1 AND appid = $2`, banID, appID))
}

// ── 申诉 ──

const appealColumns = `ap.id, ap.appid, COALESCE(a.name, ''), ap.user_id, ap.ban_id, ap.platform_ban_id, ap.status, ap.content, ap.contact_email, ap.locale,
	ap.source, ap.client_ip, ap.user_agent, ap.model_verdict, ap.model_confidence, ap.model_summary, ap.model_reply, ap.model_name, ap.model_error,
	ap.risk_level, ap.risk_evidence, ap.model_reviewed_at, ap.review_attempts, ap.decided_by, ap.reviewer_admin_id, ap.reviewer_name,
	ap.review_note, ap.reply, ap.created_at, ap.updated_at, ap.decided_at, COALESCE(u.account, ''), COALESCE(p.nickname, '')`

const appealFrom = ` FROM ban_appeals ap LEFT JOIN apps a ON a.id = ap.appid LEFT JOIN users u ON u.id = ap.user_id LEFT JOIN user_profiles p ON p.user_id = ap.user_id`

func scanAppeal(row pgx.Row) (*suspension.Appeal, error) {
	var a suspension.Appeal
	var evidence []byte
	var conf float32
	err := row.Scan(&a.ID, &a.AppID, &a.AppName, &a.UserID, &a.BanID, &a.PlatformBanID, &a.Status, &a.Content, &a.ContactEmail, &a.Locale,
		&a.Source, &a.ClientIP, &a.UserAgent, &a.ModelVerdict, &conf, &a.ModelSummary, &a.ModelReply, &a.ModelName, &a.ModelError,
		&a.RiskLevel, &evidence, &a.ModelReviewedAt, &a.ReviewAttempts, &a.DecidedBy, &a.ReviewerAdminID, &a.ReviewerName,
		&a.ReviewNote, &a.Reply, &a.CreatedAt, &a.UpdatedAt, &a.DecidedAt, &a.Account, &a.Nickname)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ModelConfidence = float64(conf)
	a.RiskEvidence = map[string]any{}
	_ = json.Unmarshal(evidence, &a.RiskEvidence)
	return &a, nil
}

// ErrAppealOpenExists 这条封禁已有进行中的申诉
var ErrAppealOpenExists = errors.New("open appeal exists")

// InsertAppeal 新申诉
func (r *Repository) InsertAppeal(ctx context.Context, a suspension.Appeal) (*suspension.Appeal, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO ban_appeals (appid, user_id, ban_id, platform_ban_id, content, contact_email, locale, source, client_ip, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		a.AppID, a.UserID, a.BanID, a.PlatformBanID, a.Content, a.ContactEmail, a.Locale, a.Source, a.ClientIP, a.UserAgent).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "uq_ban_appeals_open") {
			return nil, ErrAppealOpenExists
		}
		return nil, err
	}
	return r.GetAppeal(ctx, 0, id)
}

// GetAppeal 一条申诉；appID 为 0 时不限应用
func (r *Repository) GetAppeal(ctx context.Context, appID, id int64) (*suspension.Appeal, error) {
	if appID > 0 {
		return scanAppeal(r.pool.QueryRow(ctx, `SELECT `+appealColumns+appealFrom+` WHERE ap.id = $1 AND ap.appid = $2`, id, appID))
	}
	return scanAppeal(r.pool.QueryRow(ctx, `SELECT `+appealColumns+appealFrom+` WHERE ap.id = $1`, id))
}

// AppealsOfBan 一条封禁的全部申诉（最近的在前）
func (r *Repository) AppealsOfBan(ctx context.Context, banID int64) ([]suspension.Appeal, error) {
	return r.queryAppeals(ctx, ` WHERE ap.ban_id = $1 ORDER BY ap.created_at DESC LIMIT 50`, banID)
}

// AppealsOfUser 一个账号的申诉（最近的在前）
func (r *Repository) AppealsOfUser(ctx context.Context, appID, userID int64, limit int) ([]suspension.Appeal, error) {
	return r.queryAppeals(ctx, ` WHERE ap.appid = $1 AND ap.user_id = $2 ORDER BY ap.created_at DESC LIMIT $3`, appID, userID, limit)
}

func (r *Repository) queryAppeals(ctx context.Context, tail string, args ...any) ([]suspension.Appeal, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+appealColumns+appealFrom+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []suspension.Appeal{}
	for rows.Next() {
		a, err := scanAppeal(rows)
		if err != nil {
			return nil, err
		}
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, rows.Err()
}

func appealWhere(q suspension.AppealQuery) (string, []any) {
	where := []string{"TRUE"}
	var args []any
	if q.AppID > 0 {
		args = append(args, q.AppID)
		where = append(where, fmt.Sprintf("ap.appid = $%d", len(args)))
	}
	switch q.Status {
	case "":
	case "open":
		// 进行中且模型还没给结论
		where = append(where, "ap.status IN ('pending','reviewing') AND ap.model_verdict = ''")
	case "suggested":
		// 进行中且模型已给出结论，等人工拍板
		where = append(where, "ap.status IN ('pending','reviewing') AND ap.model_verdict <> ''")
	case "active":
		where = append(where, "ap.status IN ('pending','reviewing')")
	default:
		args = append(args, q.Status)
		where = append(where, fmt.Sprintf("ap.status = $%d", len(args)))
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%", kw)
		where = append(where, fmt.Sprintf("(u.account ILIKE $%d OR p.nickname ILIKE $%d OR ap.content ILIKE $%d OR ap.user_id::text = $%d OR ap.id::text = $%d)",
			len(args)-1, len(args)-1, len(args)-1, len(args), len(args)))
	}
	return strings.Join(where, " AND "), args
}

// ListAppeals 管理端列表
func (r *Repository) ListAppeals(ctx context.Context, q suspension.AppealQuery) ([]suspension.Appeal, int64, error) {
	cond, args := appealWhere(q)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*)`+appealFrom+` WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	size := q.PageSize
	if size <= 0 || size > 100 {
		size = 20
	}
	page := max(q.Page, 1)
	args = append(args, size, (page-1)*size)
	// 进行中的按等得最久排在前，已结案的按结案时间倒序
	order := ` ORDER BY CASE WHEN ap.status IN ('pending','reviewing') THEN 0 ELSE 1 END,
		CASE WHEN ap.status IN ('pending','reviewing') THEN ap.created_at END ASC, COALESCE(ap.decided_at, ap.created_at) DESC`
	list, err := r.queryAppeals(ctx, ` WHERE `+cond+order+fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	return list, total, err
}

// AppealStatsOf 各状态数量；appID 0 = 全部应用
func (r *Repository) AppealStatsOf(ctx context.Context, appID int64) (*suspension.AppealStats, error) {
	var s suspension.AppealStats
	err := r.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('pending','reviewing') AND model_verdict = ''),
		count(*) FILTER (WHERE status IN ('pending','reviewing') AND model_verdict <> ''),
		count(*) FILTER (WHERE status = 'approved'), count(*) FILTER (WHERE status = 'rejected'), count(*) FILTER (WHERE status = 'withdrawn'),
		count(*), count(*) FILTER (WHERE status = 'approved' AND decided_by = 'model'), count(*) FILTER (WHERE status = 'rejected' AND decided_by = 'model')
		FROM ban_appeals WHERE ($1 = 0 OR appid = $1)`, appID).Scan(&s.Open, &s.Suggested, &s.Approved, &s.Rejected, &s.Withdrawn, &s.Total, &s.AutoApproved, &s.AutoRejected)
	return &s, err
}

// AppealModelResult 决策模型复审的结果
type AppealModelResult struct {
	Verdict    string
	Confidence float64
	Summary    string
	Reply      string
	Model      string
	Error      string
	RiskLevel  string
	Evidence   map[string]any
}

// SaveAppealModel 记下决策模型复审的结果（只改进行中的申诉）
func (r *Repository) SaveAppealModel(ctx context.Context, id int64, m AppealModelResult) error {
	evidence, _ := json.Marshal(m.Evidence)
	_, err := r.pool.Exec(ctx, `UPDATE ban_appeals SET model_verdict = $2, model_confidence = $3, model_summary = $4, model_reply = $5, model_name = $6,
		model_error = $7, risk_level = $8, risk_evidence = $9, model_reviewed_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND status IN ('pending','reviewing')`, id, m.Verdict, m.Confidence, m.Summary, m.Reply, m.Model, m.Error, m.RiskLevel, evidence)
	return err
}

// ErrAppealClosed 申诉已经结案
var ErrAppealClosed = errors.New("appeal closed")

// DecideAppeal 结案：approved / rejected / withdrawn；只有进行中的能结
func (r *Repository) DecideAppeal(ctx context.Context, id int64, status, decidedBy string, reviewer userdomain.BanOperator, note, reply string) (*suspension.Appeal, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE ban_appeals SET status = $2, decided_by = $3, reviewer_admin_id = $4, reviewer_name = $5, review_note = $6, reply = $7,
		decided_at = NOW(), updated_at = NOW() WHERE id = $1 AND status IN ('pending','reviewing')`,
		id, status, decidedBy, nullableInt64(reviewer.AdminID), reviewer.AdminName, note, reply)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAppealClosed
	}
	return r.GetAppeal(ctx, 0, id)
}

// ClaimUnreviewedAppeals 认领还没跑过决策模型的申诉（提交 30 秒后仍没审的、尝试不到 3 次的）
func (r *Repository) ClaimUnreviewedAppeals(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `UPDATE ban_appeals SET review_attempts = review_attempts + 1, updated_at = NOW()
		WHERE id IN (SELECT id FROM ban_appeals WHERE status = 'pending' AND model_reviewed_at IS NULL AND review_attempts < 3
			AND created_at < NOW() - INTERVAL '30 seconds' ORDER BY created_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		RETURNING id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// MarkAppealReviewing 申诉进入复审中（决策模型开始审）
func (r *Repository) MarkAppealReviewing(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE ban_appeals SET status = 'reviewing', updated_at = NOW() WHERE id = $1 AND status = 'pending'`, id)
	return err
}

// ── 申诉设置 ──

// GetAppealSettings 应用的申诉设置；没有时取平台默认（appid 0），再没有返回 nil
func (r *Repository) GetAppealSettings(ctx context.Context, appID int64) (*suspension.Settings, bool, error) {
	var raw []byte
	var owner int64
	err := r.pool.QueryRow(ctx, `SELECT appid, config FROM ban_appeal_settings WHERE appid = $1 OR appid = 0 ORDER BY appid DESC LIMIT 1`, appID).Scan(&owner, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var s suspension.Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, err
	}
	return &s, owner == appID, nil
}

// SaveAppealSettings 保存应用（或平台默认 appid 0）的申诉设置
func (r *Repository) SaveAppealSettings(ctx context.Context, appID int64, s suspension.Settings, by string) error {
	raw, _ := json.Marshal(s)
	_, err := r.pool.Exec(ctx, `INSERT INTO ban_appeal_settings (appid, config, updated_by, updated_at) VALUES ($1, $2, $3, NOW())
		ON CONFLICT (appid) DO UPDATE SET config = EXCLUDED.config, updated_by = EXCLUDED.updated_by, updated_at = NOW()`, appID, raw, by)
	return err
}

// LatestBanForUser 账号最近一条封禁（生效中的优先）
func (r *Repository) LatestBanForUser(ctx context.Context, appID, userID int64) (*userdomain.AccountBan, error) {
	return scanAccountBan(r.pool.QueryRow(ctx, `SELECT `+userAccountBanColumns+` FROM user_account_bans WHERE appid = $1 AND user_id = $2
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, created_at DESC LIMIT 1`, appID, userID))
}
