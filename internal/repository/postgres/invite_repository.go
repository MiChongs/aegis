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
	"github.com/shopspring/decimal"

	"aegis/internal/domain/invite"
	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
)

// 邀请：配置、邀请码、填写（单事务：锁两个人 → 锁码 → 判定 → 记关系 → 发被邀请人 → 结算邀请人）、
// 邀请人奖励的延后结算与阶梯奖励。规则见 docs/invite.md。

var (
	ErrInviteCodeNotFound     = errors.New("invite code not found")
	ErrInviteRelationNotFound = errors.New("invite relation not found")
	// ErrInviteRelationState 只有等待中的邀请人奖励能被发放或取消
	ErrInviteRelationState = errors.New("invite relation is not pending")
	// ErrInvitePersonalCodeExists 个人码已经有了（并发首次生成时撞上一人一码的唯一索引）
	ErrInvitePersonalCodeExists = errors.New("personal invite code already exists")
)

// InviteUnavailableError 这个人此刻不能填写这个码（判定结论随错误带出）。
type InviteUnavailableError struct {
	Reason string
}

func (e *InviteUnavailableError) Error() string { return "invite unavailable: " + e.Reason }

// ── 配置 ──

// GetInviteSettings 读配置；没保存过时返回默认配置、关闭、configured=false。
func (r *Repository) GetInviteSettings(ctx context.Context, appID int64) (*invite.Settings, error) {
	return getInviteSettings(ctx, r.pool, appID)
}

func getInviteSettings(ctx context.Context, q queryExecutor, appID int64) (*invite.Settings, error) {
	settings := invite.Settings{AppID: appID, Config: invite.DefaultConfig()}
	var raw []byte
	var updatedAt time.Time
	err := q.QueryRow(ctx, `SELECT enabled, config, COALESCE(updated_by, ''), updated_at
FROM invite_settings WHERE appid = $1`, appID).Scan(&settings.Enabled, &raw, &settings.UpdatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		// 缺的字段保留默认值：新加的配置项对老配置也有一个合理的取值
		if err := json.Unmarshal(raw, &settings.Config); err != nil {
			return nil, fmt.Errorf("decode invite config: %w", err)
		}
	}
	settings.Config = invite.NormalizeConfig(settings.Config)
	settings.UpdatedAt = &updatedAt
	settings.Configured = true
	return &settings, nil
}

// SaveInviteSettings 保存开关与配置（调用方已清洗、校验）。
func (r *Repository) SaveInviteSettings(ctx context.Context, appID int64, enabled bool, cfg invite.Config, operator string) (*invite.Settings, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO invite_settings (appid, enabled, config, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, config = EXCLUDED.config,
    updated_by = EXCLUDED.updated_by, updated_at = NOW()`, appID, enabled, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetInviteSettings(ctx, appID)
}

// ── 邀请码 ──

const inviteCodeColumns = `c.id, c.appid, c.code, c.kind, c.owner_user_id, c.title, c.note, c.rewards, c.max_uses, c.used_count,
c.starts_at, c.ends_at, c.status, c.custom, COALESCE(c.created_by, ''), c.created_at, c.updated_at`

func scanInviteCode(row interface{ Scan(dest ...any) error }, extra ...any) (*invite.Code, error) {
	var code invite.Code
	var rewards []byte
	dest := []any{&code.ID, &code.AppID, &code.Code, &code.Kind, &code.OwnerUserID, &code.Title, &code.Note, &rewards,
		&code.MaxUses, &code.UsedCount, &code.StartsAt, &code.EndsAt, &code.Status, &code.Custom, &code.CreatedBy,
		&code.CreatedAt, &code.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInviteCodeNotFound
		}
		return nil, err
	}
	if len(rewards) > 0 && string(rewards) != "null" {
		list := []invite.Reward{}
		if err := json.Unmarshal(rewards, &list); err != nil {
			return nil, fmt.Errorf("decode invite code rewards: %w", err)
		}
		code.Rewards = &list
	}
	return &code, nil
}

func inviteRewardsJSON(rewards *[]invite.Reward) any {
	if rewards == nil {
		return nil
	}
	raw, _ := json.Marshal(*rewards)
	return raw
}

// GetPersonalInviteCode 某人的个人码；还没有时返回 nil。
func (r *Repository) GetPersonalInviteCode(ctx context.Context, appID, userID int64) (*invite.Code, error) {
	code, err := scanInviteCode(r.pool.QueryRow(ctx, `SELECT `+inviteCodeColumns+`
FROM invite_codes c WHERE c.appid = $1 AND c.owner_user_id = $2 AND c.kind = 'personal'`, appID, userID))
	if errors.Is(err, ErrInviteCodeNotFound) {
		return nil, nil
	}
	return code, err
}

// CreatePersonalInviteCode 生成个人码。撞码返回唯一约束错误（调用方换码重试）；
// 此人已经有个人码（并发首次生成）返回 ErrInvitePersonalCodeExists。
func (r *Repository) CreatePersonalInviteCode(ctx context.Context, appID, userID int64, code string, custom bool) (*invite.Code, error) {
	created, err := scanInviteCode(r.pool.QueryRow(ctx, `INSERT INTO invite_codes AS c (appid, code, kind, owner_user_id, custom)
VALUES ($1, $2, 'personal', $3, $4)
ON CONFLICT (appid, owner_user_id) WHERE kind = 'personal' DO NOTHING
RETURNING `+inviteCodeColumns, appID, code, userID, custom))
	if errors.Is(err, ErrInviteCodeNotFound) {
		return nil, ErrInvitePersonalCodeExists
	}
	return created, err
}

// UpdatePersonalInviteCode 换个人码（自定义或重新生成）。撞码返回唯一约束错误。
// 已经填写过旧码的关系不受影响：关系记的是码的快照与邀请人。
func (r *Repository) UpdatePersonalInviteCode(ctx context.Context, appID, userID int64, code string, custom bool) (*invite.Code, error) {
	return scanInviteCode(r.pool.QueryRow(ctx, `UPDATE invite_codes AS c SET code = $3, custom = $4, updated_at = NOW()
WHERE c.appid = $1 AND c.owner_user_id = $2 AND c.kind = 'personal'
RETURNING `+inviteCodeColumns, appID, userID, code, custom))
}

// InviteCodeTaken 码是否已被占用（新码或老系统的个人码）。
func (r *Repository) InviteCodeTaken(ctx context.Context, appID int64, code string) (bool, error) {
	found, err := findInviteCode(ctx, r.pool, appID, code, false)
	if err != nil {
		return false, err
	}
	return found != nil, nil
}

// FindInviteCode 按填写的码查（已规范化：大写、无空白）。查不到时回落到老系统的个人码，仍查不到返回 nil。
func (r *Repository) FindInviteCode(ctx context.Context, appID int64, code string) (*invite.Code, error) {
	return findInviteCode(ctx, r.pool, appID, code, false)
}

func findInviteCode(ctx context.Context, q queryExecutor, appID int64, code string, lock bool) (*invite.Code, error) {
	query := `SELECT ` + inviteCodeColumns + ` FROM invite_codes c WHERE c.appid = $1 AND UPPER(c.code) = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	found, err := scanInviteCode(q.QueryRow(ctx, query, appID, code))
	if err == nil {
		return found, nil
	}
	if !errors.Is(err, ErrInviteCodeNotFound) {
		return nil, err
	}
	// 老系统的个人码：24 位、全局唯一、存的就是大写，按原值比对能走唯一索引
	var ownerID int64
	var createdAt time.Time
	err = q.QueryRow(ctx, `SELECT u.id, u.created_at FROM user_profiles p JOIN users u ON u.id = p.user_id
WHERE p.invite_code = $2 AND u.appid = $1`, appID, code).Scan(&ownerID, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &invite.Code{AppID: appID, Code: code, Kind: invite.KindPersonal, OwnerUserID: &ownerID,
		Status: invite.CodeActive, CreatedAt: createdAt, UpdatedAt: createdAt, Legacy: true}, nil
}

// ListInviteCodes 管理端邀请码列表（kind / status / ownerId / keyword），新的在前。
func (r *Repository) ListInviteCodes(ctx context.Context, query invite.CodeQuery) (*invite.Page[invite.AdminCode], error) {
	where := []string{"c.appid = $1"}
	args := []any{query.AppID}
	if kind := strings.TrimSpace(query.Kind); kind != "" {
		args = append(args, kind)
		where = append(where, fmt.Sprintf("c.kind = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("c.status = $%d", len(args)))
	}
	if query.OwnerID > 0 {
		args = append(args, query.OwnerID)
		where = append(where, fmt.Sprintf("c.owner_user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(c.code ILIKE $%d OR c.title ILIKE $%d OR c.note ILIKE $%d OR u.account ILIKE $%d OR p.nickname ILIKE $%d)",
			len(args), len(args), len(args), len(args), len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	from := ` FROM invite_codes c LEFT JOIN users u ON u.id = c.owner_user_id LEFT JOIN user_profiles p ON p.user_id = c.owner_user_id`
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+inviteCodeColumns+`, COALESCE(u.account, ''), COALESCE(p.nickname, '')`+
		from+clause+fmt.Sprintf(" ORDER BY c.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]invite.AdminCode, 0, limit)
	for rows.Next() {
		var account, nickname string
		code, err := scanInviteCode(rows, &account, &nickname)
		if err != nil {
			return nil, err
		}
		items = append(items, invite.AdminCode{Code: *code, OwnerAccount: account, OwnerNickname: nickname})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return invitePage(items, total, page, limit), nil
}

// GetInviteCode 管理端按 ID 取一个码。
func (r *Repository) GetInviteCode(ctx context.Context, appID, codeID int64) (*invite.Code, error) {
	return scanInviteCode(r.pool.QueryRow(ctx, `SELECT `+inviteCodeColumns+`
FROM invite_codes c WHERE c.appid = $1 AND c.id = $2`, appID, codeID))
}

// CreateInviteCampaignCodes 创建活动码：codes 是已经定好的码（指定的一个，或按格式生成的一批），
// 一个事务里全部插入；任何一个撞码整批回滚，返回唯一约束错误。
func (r *Repository) CreateInviteCampaignCodes(ctx context.Context, input invite.CodeInput, codes []string) ([]invite.Code, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	out := make([]invite.Code, 0, len(codes))
	for _, code := range codes {
		created, err := scanInviteCode(tx.QueryRow(ctx, `INSERT INTO invite_codes AS c
(appid, code, kind, owner_user_id, title, note, rewards, max_uses, starts_at, ends_at, status, created_by)
VALUES ($1, $2, 'campaign', $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING `+inviteCodeColumns,
			input.AppID, code, input.OwnerUserID, input.Title, input.Note, inviteRewardsJSON(input.Rewards), input.MaxUses,
			input.StartsAt, input.EndsAt, input.Status, nullableString(input.Operator)))
		if err != nil {
			return nil, err
		}
		out = append(out, *created)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateInviteCampaignCode 更新活动码（码本身不改：已经发出去的码改了就作废了）。
func (r *Repository) UpdateInviteCampaignCode(ctx context.Context, codeID int64, input invite.CodeInput) (*invite.Code, error) {
	return scanInviteCode(r.pool.QueryRow(ctx, `UPDATE invite_codes AS c SET owner_user_id = $3, title = $4, note = $5,
    rewards = $6, max_uses = $7, starts_at = $8, ends_at = $9, status = $10, updated_at = NOW()
WHERE c.appid = $1 AND c.id = $2 AND c.kind = 'campaign'
RETURNING `+inviteCodeColumns,
		input.AppID, codeID, input.OwnerUserID, input.Title, input.Note, inviteRewardsJSON(input.Rewards), input.MaxUses,
		input.StartsAt, input.EndsAt, input.Status))
}

// SetInviteCodeStatus 启用 / 停用一个码（个人码也可以停用：违规推广时用）。
func (r *Repository) SetInviteCodeStatus(ctx context.Context, appID, codeID int64, status string) (*invite.Code, error) {
	return scanInviteCode(r.pool.QueryRow(ctx, `UPDATE invite_codes AS c SET status = $3, updated_at = NOW()
WHERE c.appid = $1 AND c.id = $2 RETURNING `+inviteCodeColumns, appID, codeID, status))
}

// DeleteInviteCode 删除活动码（关系保留码的快照）。个人码不能删：删了主人下次打开邀请页又会生成一个新的。
func (r *Repository) DeleteInviteCode(ctx context.Context, appID, codeID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM invite_codes WHERE appid = $1 AND id = $2 AND kind = 'campaign'`, appID, codeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInviteCodeNotFound
	}
	return nil
}

// ── 用户事实 ──

// InviteUserFacts 邀请页需要的这个人的事实：注册时间、是否会员、等级。
type InviteUserFacts struct {
	RegisteredAt time.Time
	IsVip        bool
	Level        int
}

// GetInviteUserFacts 取邀请页的事实。
func (r *Repository) GetInviteUserFacts(ctx context.Context, appID, userID int64) (*InviteUserFacts, error) {
	var facts InviteUserFacts
	var experience int64
	if err := r.pool.QueryRow(ctx, `SELECT created_at, experience FROM users WHERE id = $1 AND appid = $2`,
		userID, appID).Scan(&facts.RegisteredAt, &experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	entitlement, err := r.GetVipEntitlementFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	facts.IsVip = vipdomain.Evaluate(*entitlement, time.Now()).IsVIP
	facts.Level, _, err = r.userLevelQ(ctx, r.pool, appID, userID, experience)
	if err != nil {
		return nil, err
	}
	return &facts, nil
}

// UserRegisteredAt 注册时间。
func (r *Repository) UserRegisteredAt(ctx context.Context, appID, userID int64) (time.Time, error) {
	var createdAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrUserNotFound
	}
	return createdAt, err
}

// GetInviteClientUser 一个人对外展示的昵称、账号与头像原值（头像由服务层翻成地址）。
func (r *Repository) GetInviteClientUser(ctx context.Context, appID, userID int64) (*invite.ClientUser, error) {
	user := invite.ClientUser{UserID: userID}
	var nickname string
	if err := r.pool.QueryRow(ctx, `SELECT u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, '')
FROM users u LEFT JOIN user_profiles p ON p.user_id = u.id WHERE u.id = $1 AND u.appid = $2`, userID, appID).
		Scan(&user.Account, &nickname, &user.RawAvatar); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	user.Nickname = invite.DisplayName(nickname, user.Account)
	return &user, nil
}

// ── 填写 ──

// BindInvite 填写邀请码（单事务，要么全成要么全不成）。
//
// 顺序：查码 → 按 ID 升序锁住被邀请人与邀请人 → 锁码行并重读 → 取齐事实并判定 → 记关系 →
// 码的使用次数加一 → 发被邀请人奖励 → 写回老字段 parent_invite_account → 条件已达成时结算邀请人。
//
// 两个人按 ID 升序一起锁：A 填 B 的码、B 同时填 A 的码时，两边按同一顺序等锁，
// 后到的一方会看到「对方是我邀请来的」，而不是两个事务互相等死。
// 邀请人的行锁同时让他的奖励计数（每日 / 累计上限、阶梯）在并发填写下仍然准确。
func (r *Repository) BindInvite(ctx context.Context, input invite.BindInput) (*invite.BindResult, error) {
	cfg := input.Settings.Config
	found, err := findInviteCode(ctx, r.pool, input.AppID, input.Code, false)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var inviterID int64
	if found != nil && found.OwnerUserID != nil {
		inviterID = *found.OwnerUserID
	}
	registeredAt, inviterAccount, err := lockInviteUsersTx(ctx, tx, input.AppID, input.UserID, inviterID)
	if err != nil {
		return nil, err
	}
	if inviterID != 0 && inviterAccount == "" {
		// 推广人 / 个人码的主人已被删除
		found = nil
		inviterID = 0
	}
	if found != nil && found.ID > 0 {
		if found, err = findInviteCode(ctx, tx, input.AppID, input.Code, true); err != nil {
			return nil, err
		}
	}

	facts := invite.BindFacts{
		Now: input.Now, Source: input.Source, RegisteredAt: registeredAt, InviteeID: input.UserID,
		Code: found, InviterEligible: true,
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM invite_relations WHERE appid = $1 AND invitee_id = $2)`,
		input.AppID, input.UserID).Scan(&facts.AlreadyBound); err != nil {
		return nil, err
	}
	if inviterID != 0 {
		var parentID *int64
		err := tx.QueryRow(ctx, `SELECT inviter_id FROM invite_relations WHERE appid = $1 AND invitee_id = $2`,
			input.AppID, inviterID).Scan(&parentID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if parentID != nil {
			facts.InviterParentID = *parentID
		}
		if found.Kind == invite.KindPersonal && (cfg.InviterAudience == invite.AudienceVip || cfg.InviterMinLevel > 0) {
			if facts.InviterEligible, err = r.inviterEligibleTx(ctx, tx, cfg, input.AppID, inviterID); err != nil {
				return nil, err
			}
		}
	}
	if cfg.DeviceLimit > 0 && input.DeviceID != "" {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM invite_relations WHERE appid = $1 AND device_id = $2`,
			input.AppID, input.DeviceID).Scan(&facts.DeviceBindings); err != nil {
			return nil, err
		}
	}
	if cfg.IPDailyLimit > 0 && input.ClientIP != "" {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM invite_relations WHERE appid = $1 AND client_ip = $2 AND created_at >= $3`,
			input.AppID, input.ClientIP, input.Windows.Day).Scan(&facts.IPBindingsToday); err != nil {
			return nil, err
		}
	}
	plans, features, err := pointsStoreCatalogRefs(ctx, tx, input.AppID)
	if err != nil {
		return nil, err
	}
	inviteeRewards := invite.InviteeRewardsFor(cfg, found)
	facts.RewardsResolvable = invite.RewardsResolvable(inviteeRewards, plans, features)
	if inviterID != 0 {
		facts.RewardsResolvable = facts.RewardsResolvable && invite.RewardsResolvable(cfg.InviterRewards, plans, features)
	}
	if reason := invite.EvaluateBind(input.Settings, facts); reason != invite.ReasonOK {
		return nil, &InviteUnavailableError{Reason: reason}
	}

	status := invite.InviterNone
	if inviterID != 0 {
		status = invite.InviterPending
	}
	var codeID *int64
	if found.ID > 0 {
		codeID = &found.ID
	}
	var inviterRef *int64
	if inviterID != 0 {
		inviterRef = &inviterID
	}
	relation, err := scanInviteRelation(tx.QueryRow(ctx, `INSERT INTO invite_relations AS ir
(appid, invitee_id, inviter_id, code_id, code, code_kind, source, inviter_status, device_id, client_ip)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING `+inviteRelationColumns,
		input.AppID, input.UserID, inviterRef, codeID, found.Code, found.Kind, input.Source, status,
		nullableString(truncateColumn(input.DeviceID, 128)), nullableString(truncateColumn(input.ClientIP, 64))))
	if err != nil {
		return nil, err
	}
	if found.ID > 0 {
		if _, err := tx.Exec(ctx, `UPDATE invite_codes SET used_count = used_count + 1, updated_at = NOW() WHERE id = $1`, found.ID); err != nil {
			return nil, err
		}
	}

	results, err := r.grantInviteRewardsTx(ctx, tx, inviteGrant{
		AppID: input.AppID, UserID: input.UserID, RelationID: relation.ID,
		Key: "invitee", Title: "填写邀请码奖励", Plans: plans, Features: features,
	}, inviteeRewards)
	if err != nil {
		return nil, err
	}
	resultsJSON, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE invite_relations SET invitee_results = $2 WHERE id = $1`, relation.ID, resultsJSON); err != nil {
		return nil, err
	}
	relation.InviteeResults = results

	out := &invite.BindResult{Rewards: results}
	if inviterID != 0 {
		// 老系统只认这一列：写回去，读老字段的地方（导出、用户详情）也知道是谁邀请的
		if _, err := tx.Exec(ctx, `UPDATE user_profiles SET parent_invite_account = $2, updated_at = NOW() WHERE user_id = $1`,
			input.UserID, inviterAccount); err != nil {
			return nil, err
		}
		signDays, err := inviteeSignDaysTx(ctx, tx, input.AppID, input.UserID)
		if err != nil {
			return nil, err
		}
		if invite.InviterDue(cfg, signDays) {
			settled, err := r.settleInviterTx(ctx, tx, cfg, relation, input.Windows, plans, features, "", false)
			if err != nil {
				return nil, err
			}
			out.InviterSettled = true
			out.Milestones = settled.Milestones
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	out.Relation = *relation
	out.Code = relation.Code
	return out, nil
}

// lockInviteUsersTx 按 ID 升序锁住被邀请人与邀请人（可为 0），返回被邀请人的注册时间与邀请人的账号。
// 邀请人不存在时账号为空串。
func lockInviteUsersTx(ctx context.Context, tx pgx.Tx, appID, inviteeID, inviterID int64) (time.Time, string, error) {
	ids := []int64{inviteeID}
	if inviterID != 0 && inviterID != inviteeID {
		ids = append(ids, inviterID)
	}
	rows, err := tx.Query(ctx, `SELECT id, account, created_at FROM users
WHERE appid = $1 AND id = ANY($2) ORDER BY id FOR UPDATE`, appID, ids)
	if err != nil {
		return time.Time{}, "", err
	}
	defer rows.Close()
	var registeredAt time.Time
	var inviterAccount string
	foundInvitee := false
	for rows.Next() {
		var id int64
		var account string
		var createdAt time.Time
		if err := rows.Scan(&id, &account, &createdAt); err != nil {
			return time.Time{}, "", err
		}
		if id == inviteeID {
			registeredAt, foundInvitee = createdAt, true
		}
		if id == inviterID {
			inviterAccount = account
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, "", err
	}
	if !foundInvitee {
		return time.Time{}, "", ErrUserNotFound
	}
	return registeredAt, inviterAccount, nil
}

func (r *Repository) inviterEligibleTx(ctx context.Context, tx pgx.Tx, cfg invite.Config, appID, inviterID int64) (bool, error) {
	isVip := false
	if cfg.InviterAudience == invite.AudienceVip {
		facts, err := entitlementFactsTx(ctx, tx, appID, inviterID, nil)
		if err != nil {
			return false, err
		}
		isVip = vipdomain.Evaluate(*facts, time.Now()).IsVIP
	}
	level := 0
	if cfg.InviterMinLevel > 0 {
		var experience int64
		if err := tx.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1`, inviterID).Scan(&experience); err != nil {
			return false, err
		}
		var err error
		level, _, err = r.userLevelQ(ctx, tx, appID, inviterID, experience)
		if err != nil {
			return false, err
		}
	}
	ok, _ := invite.EvaluateInviter(cfg, isVip, level)
	return ok, nil
}

func inviteeSignDaysTx(ctx context.Context, tx pgx.Tx, appID, userID int64) (int64, error) {
	var days int64
	err := tx.QueryRow(ctx, `SELECT total_sign_days FROM sign_stats WHERE user_id = $1 AND appid = $2`, userID, appID).Scan(&days)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return days, err
}

// ── 邀请人奖励的结算 ──

// settleInviterTx 结算一条关系的邀请人奖励（两个人的行锁已由调用方持有）。
//
// 未达上限：发每次的奖励，记 granted（算一次成功邀请），再看累计人数让哪些阶梯到了；
// 已达上限：记 limited，不发、不计入阶梯。force 为管理员手动发放：不看上限。
func (r *Repository) settleInviterTx(ctx context.Context, tx pgx.Tx, cfg invite.Config, relation *invite.Relation,
	windows pointstore.Windows, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef,
	operator string, force bool) (*invite.SettleResult, error) {
	if relation.InviterID == nil {
		return nil, ErrInviteRelationState
	}
	appID, inviterID := relation.AppID, *relation.InviterID
	var grantedToday, grantedTotal int64
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE inviter_granted_at >= $3), COUNT(*)
FROM invite_relations WHERE appid = $1 AND inviter_id = $2 AND inviter_status = 'granted'`,
		appID, inviterID, windows.Day).Scan(&grantedToday, &grantedTotal); err != nil {
		return nil, err
	}
	result := &invite.SettleResult{Results: []invite.RewardResult{}, Milestones: []invite.MilestoneGrant{}}
	if !force && !invite.InviterWithinLimits(cfg, grantedToday, grantedTotal) {
		result.Status = invite.InviterLimited
	} else {
		results, err := r.grantInviteRewardsTx(ctx, tx, inviteGrant{
			AppID: appID, UserID: inviterID, RelationID: relation.ID,
			Key: "inviter", Title: "邀请奖励", Plans: plans, Features: features,
		}, cfg.InviterRewards)
		if err != nil {
			return nil, err
		}
		result.Status, result.Results = invite.InviterGranted, results
	}
	resultsJSON, _ := json.Marshal(result.Results)
	updated, err := scanInviteRelation(tx.QueryRow(ctx, `UPDATE invite_relations AS ir SET inviter_status = $2, inviter_results = $3,
    inviter_granted_at = NOW(), handled_by = $4, updated_at = NOW()
WHERE ir.id = $1 RETURNING `+inviteRelationColumns, relation.ID, result.Status, resultsJSON, nullableString(operator)))
	if err != nil {
		return nil, err
	}
	*relation = *updated
	result.Relation = *updated

	if result.Status == invite.InviterGranted && len(cfg.Milestones) > 0 {
		already := []int{}
		rows, err := tx.Query(ctx, `SELECT milestone FROM invite_milestone_grants WHERE appid = $1 AND user_id = $2`, appID, inviterID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var milestone int
			if err := rows.Scan(&milestone); err != nil {
				rows.Close()
				return nil, err
			}
			already = append(already, milestone)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, milestone := range invite.DueMilestones(cfg, grantedTotal+1, already) {
			grantID := int64(0)
			err := tx.QueryRow(ctx, `INSERT INTO invite_milestone_grants (appid, user_id, milestone)
VALUES ($1, $2, $3) ON CONFLICT (appid, user_id, milestone) DO NOTHING RETURNING id`, appID, inviterID, milestone.Count).Scan(&grantID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			title := "邀请满 " + strconv.Itoa(milestone.Count) + " 人奖励"
			results, err := r.grantInviteRewardsTx(ctx, tx, inviteGrant{
				AppID: appID, UserID: inviterID, RelationID: relation.ID,
				Key: "milestone:" + strconv.Itoa(milestone.Count), Title: title, Plans: plans, Features: features,
			}, milestone.Rewards)
			if err != nil {
				return nil, err
			}
			raw, _ := json.Marshal(results)
			if _, err := tx.Exec(ctx, `UPDATE invite_milestone_grants SET results = $2 WHERE id = $1`, grantID, raw); err != nil {
				return nil, err
			}
			result.Milestones = append(result.Milestones, invite.MilestoneGrant{Milestone: milestone.Count, Results: results, CreatedAt: time.Now()})
		}
	}
	return result, nil
}

// SettleInvitePending 被邀请人达成条件后结算邀请人奖励（签到后调用）。没有等待中的关系、
// 或条件还没达成时返回 nil, nil。
func (r *Repository) SettleInvitePending(ctx context.Context, settings invite.Settings, inviteeID int64, windows pointstore.Windows) (*invite.SettleResult, error) {
	relation, err := r.inviteRelationOfInvitee(ctx, r.pool, settings.AppID, inviteeID)
	if err != nil || relation == nil || relation.InviterStatus != invite.InviterPending {
		return nil, err
	}
	return r.settleRelation(ctx, settings, relation.ID, windows, "", false, false)
}

// SettleInviteRelation 管理员立即发放一条等待中的邀请人奖励（不看发放条件与上限）。
func (r *Repository) SettleInviteRelation(ctx context.Context, settings invite.Settings, relationID int64, windows pointstore.Windows, operator string) (*invite.SettleResult, error) {
	return r.settleRelation(ctx, settings, relationID, windows, operator, true, true)
}

func (r *Repository) settleRelation(ctx context.Context, settings invite.Settings, relationID int64, windows pointstore.Windows,
	operator string, force, strict bool) (*invite.SettleResult, error) {
	current, err := scanInviteRelation(r.pool.QueryRow(ctx, `SELECT `+inviteRelationColumns+`
FROM invite_relations ir WHERE ir.appid = $1 AND ir.id = $2`, settings.AppID, relationID))
	if err != nil {
		return nil, err
	}
	if current.InviterID == nil {
		return nil, ErrInviteRelationState
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, _, err := lockInviteUsersTx(ctx, tx, settings.AppID, current.InviteeID, *current.InviterID); err != nil {
		return nil, err
	}
	relation, err := scanInviteRelation(tx.QueryRow(ctx, `SELECT `+inviteRelationColumns+`
FROM invite_relations ir WHERE ir.id = $1 FOR UPDATE`, relationID))
	if err != nil {
		return nil, err
	}
	if relation.InviterStatus != invite.InviterPending {
		if strict {
			return nil, ErrInviteRelationState
		}
		return nil, nil
	}
	if !force {
		signDays, err := inviteeSignDaysTx(ctx, tx, relation.AppID, relation.InviteeID)
		if err != nil {
			return nil, err
		}
		if !invite.InviterDue(settings.Config, signDays) {
			return nil, nil
		}
	}
	plans, features, err := pointsStoreCatalogRefs(ctx, tx, relation.AppID)
	if err != nil {
		return nil, err
	}
	result, err := r.settleInviterTx(ctx, tx, settings.Config, relation, windows, plans, features, operator, force)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// CancelInviteRelation 取消一条等待中的邀请人奖励（判定为刷量时用）。被邀请人已到账的奖励不收回。
func (r *Repository) CancelInviteRelation(ctx context.Context, appID, relationID int64, operator string) (*invite.Relation, error) {
	relation, err := scanInviteRelation(r.pool.QueryRow(ctx, `UPDATE invite_relations AS ir SET inviter_status = 'cancelled',
    handled_by = $3, updated_at = NOW()
WHERE ir.appid = $1 AND ir.id = $2 AND ir.inviter_status = 'pending' RETURNING `+inviteRelationColumns,
		appID, relationID, nullableString(operator)))
	if errors.Is(err, ErrInviteRelationNotFound) {
		if _, getErr := r.GetInviteRelation(ctx, appID, relationID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrInviteRelationState
	}
	return relation, err
}

// ── 发放 ──

type inviteGrant struct {
	AppID      int64
	UserID     int64
	RelationID int64
	// Key 区分同一条关系上的几次发放（被邀请人 / 邀请人 / 阶梯），拼进幂等键
	Key      string
	Title    string
	Plans    map[int64]pointstore.PlanRef
	Features map[string]pointstore.FeatureRef
	// Ref / Meta 不挂在邀请关系上的发放（推广活动）自带的账本引用与流水元数据；
	// Ref 为空时按关系 ID 生成，此时 RelationID 为 0 的流水不记 source_id
	Ref  string
	Meta map[string]any
}

// grantInviteRewardsTx 逐项发放。这里的 switch 与 invite 的权益目录双向钉死
// （TestInviteCatalogHasGrantBranch）。任何一项发不出去整笔回滚。
func (r *Repository) grantInviteRewardsTx(ctx context.Context, tx pgx.Tx, grant inviteGrant, rewards []invite.Reward) ([]invite.RewardResult, error) {
	appID, userID := grant.AppID, grant.UserID
	relationID := grant.RelationID
	meta := map[string]any{"inviteGrant": grant.Key}
	ref := grant.Ref
	sourceID := &relationID
	if relationID != 0 {
		meta["inviteRelationId"] = relationID
	} else {
		sourceID = nil
	}
	if ref == "" {
		ref = "INV" + strconv.FormatInt(relationID, 10)
	}
	for key, value := range grant.Meta {
		meta[key] = value
	}
	out := make([]invite.RewardResult, 0, len(rewards))
	for _, reward := range invite.NormalizeRewards(rewards) {
		described := invite.DescribeReward(reward, grant.Plans, grant.Features)
		result := invite.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail}

		switch reward.Type {
		case invite.RewardPoints:
			_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, reward.Amount,
				"earn", invite.IntegralCategory, grant.Title, "", invite.SourceType, sourceID, meta)
			if err != nil {
				return nil, err
			}
			result.Detail += "（余额 " + strconv.FormatInt(after, 10) + "）"
			result.TransactionNo = txnNo

		case pointstore.RewardVipPlan:
			plan, err := scanVipPlan(tx.QueryRow(ctx,
				`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
			if err != nil {
				return nil, err
			}
			if plan == nil || plan.IsTrial() {
				return nil, &InviteUnavailableError{Reason: invite.ReasonRewardUnavailable}
			}
			planID := plan.ID
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID: userID, AppID: appID, PlanID: &planID, PlanName: plan.Name, Features: plan.Features,
				DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
				PayChannel: vipdomain.ChannelInvite, PayAmount: decimal.Zero, Metadata: meta, ConvertOwned: true,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
			result.TransactionNo = txn.TransactionNo
			applyVipConversionDetail(&result.Detail, &result.Conversion, txn)

		case pointstore.RewardVipDays:
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID: userID, AppID: appID, PlanName: grant.Title, DurationDays: int(reward.Amount),
				PayChannel: vipdomain.ChannelInvite, PayAmount: decimal.Zero, Metadata: meta, ConvertOwned: true,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = txn.TransactionNo
			applyVipConversionDetail(&result.Detail, &result.Conversion, txn)

		case pointstore.RewardFeature:
			if feature, ok := grant.Features[reward.Tag]; !ok || !feature.IsActive {
				return nil, &InviteUnavailableError{Reason: invite.ReasonRewardUnavailable}
			}
			outcome, err := grantFeatureUnlockOrConvertTx(ctx, tx, FeatureUnlockGrant{
				AppID: appID, UserID: userID, Tag: reward.Tag, Days: int(reward.Amount),
				Source: vipdomain.UnlockSourceInvite, SourceRef: ref,
			}, vipdomain.ChannelInvite)
			if errors.Is(err, ErrFeatureUnlockOwned) {
				// 已永久拥有：这一项没有可发的，记一笔说明而不是让整次邀请失败
				result.Detail += "（已永久拥有）"
				out = append(out, result)
				continue
			}
			if err != nil {
				return nil, err
			}
			if outcome.Conversion != nil {
				result.Detail = outcome.Conversion.Message
				result.Conversion = outcome.Conversion.Summary()
				result.TransactionNo = outcome.Conversion.ConversionNo
				out = append(out, result)
				continue
			}
			unlock := outcome.Unlock
			result.ExpireAt = unlock.EndsAt
			result.TransactionNo = strconv.FormatInt(unlock.ID, 10)

		case pointstore.RewardExperience:
			expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
				invite.IntegralCategory, grant.Title, "", invite.SourceType, sourceID, meta)
			if err != nil {
				return nil, err
			}
			if expResult.LevelChanged {
				result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
			}
			result.TransactionNo = expResult.TransactionNo

		case pointstore.RewardBalance:
			change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
				UserID: userID, AppID: appID, Type: walletdomain.TxnTypeInvite, Amount: reward.Money,
				Title: grant.Title, Metadata: meta,
				IdempotencyKey: "invite:" + strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(relationID, 10) + ":" + grant.Key,
			})
			if err != nil {
				return nil, err
			}
			result.TransactionNo = change.Transaction.TransactionNo

		case pointstore.RewardLotteryDraws:
			balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
				AppID: appID, UserID: userID, Amount: reward.Amount, Origin: lotterydomain.TicketOriginInvite,
				Ref: ref, Title: grant.Title,
			})
			if err != nil {
				return nil, err
			}
			result.Detail += "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

		default:
			return nil, fmt.Errorf("invite: unsupported reward type %s", reward.Type)
		}
		out = append(out, result)
	}
	return out, nil
}

// ── 关系 ──

const inviteRelationColumns = `ir.id, ir.appid, ir.invitee_id, ir.inviter_id, ir.code_id, ir.code, ir.code_kind, ir.source,
ir.invitee_results, ir.inviter_status, ir.inviter_results, ir.inviter_granted_at, COALESCE(ir.handled_by, ''),
COALESCE(ir.device_id, ''), COALESCE(ir.client_ip, ''), ir.created_at, ir.updated_at`

func scanInviteRelation(row interface{ Scan(dest ...any) error }, extra ...any) (*invite.Relation, error) {
	var item invite.Relation
	var inviteeResults, inviterResults []byte
	dest := []any{&item.ID, &item.AppID, &item.InviteeID, &item.InviterID, &item.CodeID, &item.Code, &item.CodeKind, &item.Source,
		&inviteeResults, &item.InviterStatus, &inviterResults, &item.InviterGrantedAt, &item.HandledBy,
		&item.DeviceID, &item.ClientIP, &item.CreatedAt, &item.UpdatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInviteRelationNotFound
		}
		return nil, err
	}
	item.InviteeResults = []invite.RewardResult{}
	item.InviterResults = []invite.RewardResult{}
	if len(inviteeResults) > 0 {
		if err := json.Unmarshal(inviteeResults, &item.InviteeResults); err != nil {
			return nil, fmt.Errorf("decode invitee results: %w", err)
		}
	}
	if len(inviterResults) > 0 {
		if err := json.Unmarshal(inviterResults, &item.InviterResults); err != nil {
			return nil, fmt.Errorf("decode inviter results: %w", err)
		}
	}
	return &item, nil
}

const inviteRelationJoins = ` FROM invite_relations ir
LEFT JOIN users ue ON ue.id = ir.invitee_id LEFT JOIN user_profiles pe ON pe.user_id = ir.invitee_id
LEFT JOIN users ur ON ur.id = ir.inviter_id LEFT JOIN user_profiles pr ON pr.user_id = ir.inviter_id`

const inviteRelationPeople = `, COALESCE(ue.account, ''), COALESCE(pe.nickname, ''), COALESCE(pe.avatar, ''),
COALESCE(ur.account, ''), COALESCE(pr.nickname, ''), COALESCE(pr.avatar, '')`

func scanInviteRelationWithPeople(row interface{ Scan(dest ...any) error }) (*invite.Relation, error) {
	var people [6]string
	item, err := scanInviteRelation(row, &people[0], &people[1], &people[2], &people[3], &people[4], &people[5])
	if err != nil {
		return nil, err
	}
	item.InviteeAccount, item.InviteeNickname, item.InviteeAvatar = people[0], people[1], people[2]
	item.InviterAccount, item.InviterNickname, item.InviterAvatar = people[3], people[4], people[5]
	return item, nil
}

// GetInviteRelation 管理端按 ID 取一条关系（带双方账号）。
func (r *Repository) GetInviteRelation(ctx context.Context, appID, relationID int64) (*invite.Relation, error) {
	return scanInviteRelationWithPeople(r.pool.QueryRow(ctx, `SELECT `+inviteRelationColumns+inviteRelationPeople+
		inviteRelationJoins+` WHERE ir.appid = $1 AND ir.id = $2`, appID, relationID))
}

// InviteRelationOfInvitee 这个人填写过的邀请（带邀请人）；没有时返回 nil。
func (r *Repository) InviteRelationOfInvitee(ctx context.Context, appID, inviteeID int64) (*invite.Relation, error) {
	return r.inviteRelationOfInvitee(ctx, r.pool, appID, inviteeID)
}

func (r *Repository) inviteRelationOfInvitee(ctx context.Context, q queryExecutor, appID, inviteeID int64) (*invite.Relation, error) {
	relation, err := scanInviteRelationWithPeople(q.QueryRow(ctx, `SELECT `+inviteRelationColumns+inviteRelationPeople+
		inviteRelationJoins+` WHERE ir.appid = $1 AND ir.invitee_id = $2`, appID, inviteeID))
	if errors.Is(err, ErrInviteRelationNotFound) {
		return nil, nil
	}
	return relation, err
}

// ListInviteRelations 邀请关系列表，新的在前。
func (r *Repository) ListInviteRelations(ctx context.Context, query invite.RelationQuery) (*invite.Page[invite.Relation], error) {
	where := []string{"ir.appid = $1"}
	args := []any{query.AppID}
	if query.InviterID > 0 {
		args = append(args, query.InviterID)
		where = append(where, fmt.Sprintf("ir.inviter_id = $%d", len(args)))
	}
	if query.InviteeID > 0 {
		args = append(args, query.InviteeID)
		where = append(where, fmt.Sprintf("ir.invitee_id = $%d", len(args)))
	}
	if query.CodeID > 0 {
		args = append(args, query.CodeID)
		where = append(where, fmt.Sprintf("ir.code_id = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.InviterStatus); status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("ir.inviter_status = $%d", len(args)))
	}
	if source := strings.TrimSpace(query.Source); source != "" {
		args = append(args, source)
		where = append(where, fmt.Sprintf("ir.source = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf(`(ir.code ILIKE $%d OR ue.account ILIKE $%d OR pe.nickname ILIKE $%d
 OR ur.account ILIKE $%d OR pr.nickname ILIKE $%d)`, len(args), len(args), len(args), len(args), len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+inviteRelationJoins+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+inviteRelationColumns+inviteRelationPeople+inviteRelationJoins+clause+
		fmt.Sprintf(" ORDER BY ir.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]invite.Relation, 0, limit)
	for rows.Next() {
		item, err := scanInviteRelationWithPeople(rows)
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

// InviterStats 某人作为邀请人的统计。
func (r *Repository) InviterStats(ctx context.Context, appID, userID int64, dayStart time.Time) (invite.ClientStats, error) {
	var stats invite.ClientStats
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
  COUNT(*) FILTER (WHERE inviter_status = 'granted'),
  COUNT(*) FILTER (WHERE inviter_status = 'pending'),
  COUNT(*) FILTER (WHERE inviter_status = 'granted' AND inviter_granted_at >= $3)
FROM invite_relations WHERE appid = $1 AND inviter_id = $2`, appID, userID, dayStart).
		Scan(&stats.Invited, &stats.Rewarded, &stats.Pending, &stats.TodayRewarded)
	return stats, err
}

// InviteMilestoneGrants 某人已发放的阶梯。
func (r *Repository) InviteMilestoneGrants(ctx context.Context, appID, userID int64) ([]invite.MilestoneGrant, error) {
	rows, err := r.pool.Query(ctx, `SELECT milestone, results, created_at FROM invite_milestone_grants
WHERE appid = $1 AND user_id = $2 ORDER BY milestone`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []invite.MilestoneGrant{}
	for rows.Next() {
		var grant invite.MilestoneGrant
		var raw []byte
		if err := rows.Scan(&grant.Milestone, &raw, &grant.CreatedAt); err != nil {
			return nil, err
		}
		grant.Results = []invite.RewardResult{}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &grant.Results); err != nil {
				return nil, fmt.Errorf("decode milestone results: %w", err)
			}
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

// InviteStats 管理端概览：全部关系的汇总、窗口内按天趋势、邀请最多的人与用得最多的活动码。
func (r *Repository) InviteStats(ctx context.Context, appID int64, since, dayStart time.Time, timezone string) (*invite.Stats, error) {
	stats := &invite.Stats{Trend: []invite.StatsDay{}, TopInviters: []invite.TopInviter{}, TopCodes: []invite.TopCode{}}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
  COUNT(*) FILTER (WHERE created_at >= $2),
  COUNT(*) FILTER (WHERE inviter_status = 'granted'),
  COUNT(*) FILTER (WHERE inviter_status = 'pending'),
  COUNT(*) FILTER (WHERE inviter_status = 'limited'),
  COUNT(DISTINCT inviter_id),
  COUNT(*) FILTER (WHERE source = 'register'),
  COUNT(*) FILTER (WHERE source = 'bind')
FROM invite_relations WHERE appid = $1`, appID, dayStart).Scan(
		&stats.Summary.Relations, &stats.Summary.Today, &stats.Summary.Granted, &stats.Summary.Pending,
		&stats.Summary.Limited, &stats.Summary.Inviters, &stats.Summary.FromRegister, &stats.Summary.FromBind); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE kind = 'personal'), COUNT(*) FILTER (WHERE kind = 'campaign')
FROM invite_codes WHERE appid = $1`, appID).Scan(&stats.Summary.PersonalCodes, &stats.Summary.CampaignCodes); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*), COUNT(*) FILTER (WHERE inviter_status = 'granted')
FROM invite_relations WHERE appid = $1 AND created_at >= $2
GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day invite.StatsDay
		if err := rows.Scan(&day.Date, &day.Relations, &day.Granted); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	inviterRows, err := r.pool.Query(ctx, `SELECT ir.inviter_id, COALESCE(MAX(u.account), ''), COALESCE(MAX(p.nickname), ''),
       COUNT(*), COUNT(*) FILTER (WHERE ir.inviter_status = 'granted')
FROM invite_relations ir LEFT JOIN users u ON u.id = ir.inviter_id LEFT JOIN user_profiles p ON p.user_id = ir.inviter_id
WHERE ir.appid = $1 AND ir.inviter_id IS NOT NULL AND ir.created_at >= $2
GROUP BY ir.inviter_id ORDER BY COUNT(*) DESC, ir.inviter_id LIMIT 10`, appID, since)
	if err != nil {
		return nil, err
	}
	for inviterRows.Next() {
		var item invite.TopInviter
		if err := inviterRows.Scan(&item.UserID, &item.Account, &item.Nickname, &item.Invited, &item.Granted); err != nil {
			inviterRows.Close()
			return nil, err
		}
		stats.TopInviters = append(stats.TopInviters, item)
	}
	inviterRows.Close()
	if err := inviterRows.Err(); err != nil {
		return nil, err
	}

	codeRows, err := r.pool.Query(ctx, `SELECT ir.code_id, MAX(ir.code), COALESCE(MAX(c.title), ''), COUNT(*)
FROM invite_relations ir LEFT JOIN invite_codes c ON c.id = ir.code_id
WHERE ir.appid = $1 AND ir.code_kind = 'campaign' AND ir.code_id IS NOT NULL AND ir.created_at >= $2
GROUP BY ir.code_id ORDER BY COUNT(*) DESC, ir.code_id LIMIT 10`, appID, since)
	if err != nil {
		return nil, err
	}
	defer codeRows.Close()
	for codeRows.Next() {
		var item invite.TopCode
		if err := codeRows.Scan(&item.CodeID, &item.Code, &item.Title, &item.Used); err != nil {
			return nil, err
		}
		stats.TopCodes = append(stats.TopCodes, item)
	}
	return stats, codeRows.Err()
}

func invitePage[T any](items []T, total int64, page, limit int) *invite.Page[T] {
	return &invite.Page[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: int((total + int64(limit) - 1) / int64(limit)),
	}
}
