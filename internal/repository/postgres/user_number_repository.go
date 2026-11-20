package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"aegis/internal/domain/usernumber"
)

// 用户号码的账目（见 internal/domain/usernumber 与 docs/user-number.md）。
//
// 锁的顺序恒为：用户行（users FOR UPDATE）→ 摇号单 → 权益行 → 占用行。
// 换号、摇号、指定、回滚都先锁用户行，同一个人的这些操作串行；不同的人抢同一个号时，
// 最后一道是 users(appid, number) 的唯一索引（23505 → ErrUserNumberTaken），占用表的主键管摇号候选。

var (
	ErrUserNumberUserNotFound   = errors.New("user number: user not found")
	ErrUserNumberSame           = errors.New("user number: same as current")
	ErrUserNumberTaken          = errors.New("user number: taken")
	ErrUserNumberReserved       = errors.New("user number: reserved")
	ErrUserNumberFrozen         = errors.New("user number: frozen")
	ErrUserNumberLocked         = errors.New("user number: locked by another draw")
	ErrUserNumberNoRight        = errors.New("user number: no usable right")
	ErrUserNumberNoDrawRight    = errors.New("user number: no draw left")
	ErrUserNumberDrawNotFound   = errors.New("user number: draw not found or closed")
	ErrUserNumberNotCandidate   = errors.New("user number: not a candidate of this draw")
	ErrUserNumberNoCandidates   = errors.New("user number: no free candidate")
	ErrUserNumberHistoryMissing = errors.New("user number: history not found")
	ErrUserNumberBalance        = errors.New("user number: balance would go negative")
	ErrUserNumberSelfPickOff    = errors.New("user number: self pick disabled")
)

// UserNumberCooldownError 冷却中，Until 之后才能再换。
type UserNumberCooldownError struct{ Until time.Time }

func (e *UserNumberCooldownError) Error() string {
	return "user number: cooldown until " + e.Until.Format(time.RFC3339)
}

type numberQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ── 设置 ──

// GetUserNumberSettings 应用的号码设置；没有配置过时为默认设置。
func (r *Repository) GetUserNumberSettings(ctx context.Context, appID int64) (usernumber.Settings, error) {
	return getUserNumberSettings(ctx, r.pool, appID)
}

func getUserNumberSettings(ctx context.Context, q numberQuerier, appID int64) (usernumber.Settings, error) {
	settings := usernumber.DefaultSettings()
	var raw []byte
	var updatedBy string
	var updatedAt time.Time
	err := q.QueryRow(ctx, `SELECT config, updated_by, updated_at FROM user_number_settings WHERE appid = $1`, appID).
		Scan(&raw, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		settings.Normalize()
		return settings, nil
	}
	if err != nil {
		return settings, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return usernumber.DefaultSettings(), nil
		}
	}
	settings.Normalize()
	settings.UpdatedBy = updatedBy
	settings.UpdatedAt = &updatedAt
	return settings, nil
}

// SaveUserNumberSettings 保存应用的号码设置（调用方先 Normalize + Validate）。
func (r *Repository) SaveUserNumberSettings(ctx context.Context, appID int64, settings usernumber.Settings, operator string) error {
	settings.UpdatedBy, settings.UpdatedAt = "", nil
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO user_number_settings (appid, config, updated_by, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (appid) DO UPDATE SET config = EXCLUDED.config, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		appID, raw, operator)
	return err
}

// ── 号码与权益 ──

// UserNumberRecord 一个用户当前的号码。
type UserNumberRecord struct {
	UserID    int64
	Number    int64
	Tier      int
	ChangedAt *time.Time
}

// GetUserNumber 用户当前的号码；用户不存在时 ErrUserNumberUserNotFound。
func (r *Repository) GetUserNumber(ctx context.Context, appID, userID int64) (*UserNumberRecord, error) {
	rec := &UserNumberRecord{UserID: userID}
	err := r.pool.QueryRow(ctx, `SELECT number, number_tier, number_changed_at FROM users WHERE appid = $1 AND id = $2`,
		appID, userID).Scan(&rec.Number, &rec.Tier, &rec.ChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNumberUserNotFound
	}
	return rec, err
}

// GetUserNumbers 一批用户的号码（主键 → 号码），不存在的不出现在结果里。
func (r *Repository) GetUserNumbers(ctx context.Context, appID int64, userIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, number FROM users WHERE appid = $1 AND id = ANY($2)`, appID, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, number int64
		if err := rows.Scan(&id, &number); err != nil {
			return nil, err
		}
		out[id] = number
	}
	return out, rows.Err()
}

// FindUserIDByNumber 号码 → 用户主键；没人用时返回 0。
func (r *Repository) FindUserIDByNumber(ctx context.Context, appID, number int64) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM users WHERE appid = $1 AND number = $2`, appID, number).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ListUserNumberRights 一个人的全部权益：改号卡、四级靓号券（由低到高，没有的余额为 0）、摇号次数。
func (r *Repository) ListUserNumberRights(ctx context.Context, appID, userID int64) (usernumber.Rights, error) {
	return listUserNumberRights(ctx, r.pool, appID, userID)
}

func listUserNumberRights(ctx context.Context, q numberQuerier, appID, userID int64) (usernumber.Rights, error) {
	type key struct {
		kind  string
		level int
	}
	found := map[key]usernumber.Right{}
	rows, err := q.Query(ctx, `SELECT kind, level, balance, earned, used FROM user_number_rights WHERE appid = $1 AND user_id = $2`,
		appID, userID)
	if err != nil {
		return usernumber.Rights{}, err
	}
	for rows.Next() {
		var right usernumber.Right
		if err := rows.Scan(&right.Kind, &right.Level, &right.Balance, &right.Earned, &right.Used); err != nil {
			rows.Close()
			return usernumber.Rights{}, err
		}
		found[key{right.Kind, right.Level}] = right
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return usernumber.Rights{}, err
	}
	get := func(kind string, level int) usernumber.Right {
		right := found[key{kind, level}]
		right.Kind, right.Level, right.Name = kind, level, usernumber.RightName(kind, level)
		return right
	}
	out := usernumber.Rights{Tickets: []usernumber.Right{}, Items: []usernumber.Right{}}
	change := get(usernumber.RightChange, 0)
	out.Change = change.Balance
	out.Items = append(out.Items, change)
	for level := usernumber.TierFine; level <= usernumber.TierSupreme; level++ {
		ticket := get(usernumber.RightTicket, level)
		out.Tickets = append(out.Tickets, ticket)
		out.Items = append(out.Items, ticket)
	}
	draw := get(usernumber.RightDraw, 0)
	out.Draw = draw.Balance
	out.Items = append(out.Items, draw)
	return out, nil
}

// grantUserNumberRightTx 在调用方的事务里发一份号码权益，返回发放后的余额。
// 各发放渠道（物品发放、卡密、积分商城、抽奖、邀请、排行榜、推广、激励广告、等级）都走这里。
func grantUserNumberRightTx(ctx context.Context, tx pgx.Tx, grant usernumber.Grant) (int64, error) {
	if !usernumber.ValidRight(grant.Kind, grant.Level) {
		return 0, fmt.Errorf("号码权益种类或等级不合法：%s/%d", grant.Kind, grant.Level)
	}
	if grant.Amount <= 0 {
		return 0, errors.New("号码权益数量必须大于 0")
	}
	var balance int64
	if err := tx.QueryRow(ctx, `INSERT INTO user_number_rights (appid, user_id, kind, level, balance, earned)
VALUES ($1, $2, $3, $4, $5::bigint, $5::bigint)
ON CONFLICT (appid, user_id, kind, level) DO UPDATE
SET balance = user_number_rights.balance + EXCLUDED.balance,
    earned = user_number_rights.earned + EXCLUDED.earned,
    updated_at = NOW()
RETURNING balance`, grant.AppID, grant.UserID, grant.Kind, grant.Level, grant.Amount).Scan(&balance); err != nil {
		return 0, err
	}
	if err := insertUserNumberLedgerTx(ctx, tx, grant.AppID, grant.UserID, grant.Kind, grant.Level, grant.Amount, balance,
		grant.Origin, grant.Ref, grant.Title, grant.Operator); err != nil {
		return 0, err
	}
	return balance, nil
}

// GrantUserNumberRight 单独发一份号码权益（自带事务；渠道内发放用 grantUserNumberRightTx）。
func (r *Repository) GrantUserNumberRight(ctx context.Context, grant usernumber.Grant) (int64, error) {
	var balance int64
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		balance, err = grantUserNumberRightTx(ctx, tx, grant)
		return err
	})
	return balance, err
}

// AdjustUserNumberRight 管理员调整余额（delta 可正可负，扣到负数报 ErrUserNumberBalance）。
func (r *Repository) AdjustUserNumberRight(ctx context.Context, appID, userID int64, kind string, level int, delta int64,
	operator, note string) (int64, error) {
	if !usernumber.ValidRight(kind, level) {
		return 0, fmt.Errorf("号码权益种类或等级不合法：%s/%d", kind, level)
	}
	if delta == 0 {
		return 0, errors.New("调整数量不能为 0")
	}
	var balance int64
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if delta > 0 {
			var err error
			balance, err = grantUserNumberRightTx(ctx, tx, usernumber.Grant{AppID: appID, UserID: userID, Kind: kind, Level: level,
				Amount: delta, Origin: usernumber.OriginAdmin, Title: note, Operator: operator})
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE user_number_rights SET balance = balance + $5, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND kind = $3 AND level = $4 AND balance + $5 >= 0
RETURNING balance`, appID, userID, kind, level, delta).Scan(&balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNumberBalance
		}
		if err != nil {
			return err
		}
		return insertUserNumberLedgerTx(ctx, tx, appID, userID, kind, level, delta, balance, usernumber.OriginAdmin, "", note, operator)
	})
	return balance, err
}

func insertUserNumberLedgerTx(ctx context.Context, q numberQuerier, appID, userID int64, kind string, level int,
	delta, balanceAfter int64, origin, ref, title, operator string) error {
	_, err := q.Exec(ctx, `INSERT INTO user_number_ledger (appid, user_id, kind, level, delta, balance_after, origin, ref, title, operator)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		appID, userID, kind, level, delta, balanceAfter, origin, truncateColumn(ref, 128), truncateColumn(title, 256), truncateColumn(operator, 128))
	return err
}

// consumeUserNumberRightTx 扣一份权益，扣不到报 noRight。
func consumeUserNumberRightTx(ctx context.Context, tx pgx.Tx, appID, userID int64, kind string, level int,
	origin, ref string, noRight error) error {
	var balance int64
	err := tx.QueryRow(ctx, `UPDATE user_number_rights SET balance = balance - 1, used = used + 1, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND kind = $3 AND level = $4 AND balance > 0
RETURNING balance`, appID, userID, kind, level).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return noRight
	}
	if err != nil {
		return err
	}
	return insertUserNumberLedgerTx(ctx, tx, appID, userID, kind, level, -1, balance, origin, ref, "", "")
}

// ── 占用与可用性 ──

// UserNumberAvailability 一个号码对某个人来说能不能拿。
type UserNumberAvailability struct {
	// OwnerID 正在用这个号的人（0 没人用）
	OwnerID int64
	// Hold 仍生效的占用（nil 没有）
	Hold *usernumber.Hold
}

// Err 对 userID 来说拿不拿得到（force：管理员，可以拿保留号与别人的冻结 / 候选号）。
func (a UserNumberAvailability) Err(userID int64, force bool) error {
	if a.OwnerID != 0 {
		if a.OwnerID == userID {
			return ErrUserNumberSame
		}
		return ErrUserNumberTaken
	}
	if a.Hold == nil || force {
		return nil
	}
	own := a.Hold.UserID != nil && *a.Hold.UserID == userID
	switch a.Hold.Kind {
	case usernumber.HoldReserved:
		return ErrUserNumberReserved
	case usernumber.HoldFrozen:
		if !own {
			return ErrUserNumberFrozen
		}
	case usernumber.HoldCandidate:
		if !own {
			return ErrUserNumberLocked
		}
	}
	return nil
}

// OwnFrozen 是自己冻结中的旧号（换回免费）。
func (a UserNumberAvailability) OwnFrozen(userID int64) bool {
	return a.OwnerID == 0 && a.Hold != nil && a.Hold.Kind == usernumber.HoldFrozen && a.Hold.UserID != nil && *a.Hold.UserID == userID
}

// CheckUserNumber 号码当前的占用情况。
func (r *Repository) CheckUserNumber(ctx context.Context, appID, number int64) (UserNumberAvailability, error) {
	return checkUserNumber(ctx, r.pool, appID, number)
}

func checkUserNumber(ctx context.Context, q numberQuerier, appID, number int64) (UserNumberAvailability, error) {
	var out UserNumberAvailability
	err := q.QueryRow(ctx, `SELECT id FROM users WHERE appid = $1 AND number = $2`, appID, number).Scan(&out.OwnerID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	hold := &usernumber.Hold{}
	err = q.QueryRow(ctx, `SELECT appid, number, kind, tier, user_id, draw_id, note, expires_at, created_by, created_at
FROM user_number_holds WHERE appid = $1 AND number = $2 AND (expires_at IS NULL OR expires_at > NOW())`, appID, number).
		Scan(&hold.AppID, &hold.Number, &hold.Kind, &hold.Tier, &hold.UserID, &hold.DrawID, &hold.Note, &hold.ExpiresAt,
			&hold.CreatedBy, &hold.CreatedAt)
	if err == nil {
		out.Hold = hold
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	return out, nil
}

// FreeUserNumbers 一批号码里还没人用、也没有生效占用的（保持输入顺序）。
func (r *Repository) FreeUserNumbers(ctx context.Context, appID int64, numbers []int64) ([]int64, error) {
	if len(numbers) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT n FROM unnest($2::bigint[]) WITH ORDINALITY AS t(n, ord)
WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.appid = $1 AND u.number = t.n)
  AND NOT EXISTS (SELECT 1 FROM user_number_holds h WHERE h.appid = $1 AND h.number = t.n
                  AND (h.expires_at IS NULL OR h.expires_at > NOW()))
ORDER BY ord`, appID, numbers)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ── 换号 ──

type lockedUserNumber struct {
	number    int64
	tier      int
	changedAt *time.Time
}

func lockUserNumberTx(ctx context.Context, tx pgx.Tx, appID, userID int64) (lockedUserNumber, error) {
	var cur lockedUserNumber
	err := tx.QueryRow(ctx, `SELECT number, number_tier, number_changed_at FROM users WHERE appid = $1 AND id = $2 FOR UPDATE`,
		appID, userID).Scan(&cur.number, &cur.tier, &cur.changedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, ErrUserNumberUserNotFound
	}
	return cur, err
}

func cooldownErr(settings usernumber.Settings, changedAt *time.Time, now time.Time) error {
	if settings.CooldownDays <= 0 || changedAt == nil {
		return nil
	}
	until := changedAt.Add(time.Duration(settings.CooldownDays) * 24 * time.Hour)
	if now.Before(until) {
		return &UserNumberCooldownError{Until: until}
	}
	return nil
}

type numberSwitch struct {
	appID, userID  int64
	oldNumber      int64
	newNumber      int64
	tier           int
	source         string
	rightKind      string
	rightLevel     int
	drawID         int64
	operator, note string
	freezeDays     int
	now            time.Time
}

// applyUserNumberSwitchTx 换上新号：写 users（唯一索引兜底并发抢号）、清掉新号的占用、冻结旧号、记历史。
func applyUserNumberSwitchTx(ctx context.Context, tx pgx.Tx, sw numberSwitch) (*usernumber.History, error) {
	if _, err := tx.Exec(ctx, `UPDATE users SET number = $3, number_tier = $4, number_changed_at = $5, updated_at = NOW()
WHERE appid = $1 AND id = $2`, sw.appID, sw.userID, sw.newNumber, sw.tier, sw.now); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrUserNumberTaken
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_number_holds WHERE appid = $1 AND number = $2`, sw.appID, sw.newNumber); err != nil {
		return nil, err
	}
	if sw.drawID > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM user_number_holds WHERE draw_id = $1 AND kind = 'candidate'`, sw.drawID); err != nil {
			return nil, err
		}
	}
	if sw.freezeDays > 0 && sw.oldNumber > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO user_number_holds (appid, number, kind, tier, user_id, note, expires_at, created_by)
VALUES ($1, $2, 'frozen', (SELECT number_tier FROM users WHERE id = $3), $3, '换号后冻结', $4, 'system')
ON CONFLICT (appid, number) DO UPDATE
SET kind = 'frozen', tier = EXCLUDED.tier, user_id = EXCLUDED.user_id, draw_id = NULL, note = EXCLUDED.note,
    expires_at = EXCLUDED.expires_at, created_by = EXCLUDED.created_by, created_at = NOW()
WHERE user_number_holds.kind <> 'reserved'`,
			sw.appID, sw.oldNumber, sw.userID, sw.now.Add(time.Duration(sw.freezeDays)*24*time.Hour)); err != nil {
			return nil, err
		}
	}
	h := &usernumber.History{AppID: sw.appID, UserID: sw.userID, NewNumber: sw.newNumber, Tier: sw.tier, Source: sw.source,
		RightKind: sw.rightKind, RightLvl: sw.rightLevel, Operator: sw.operator, Note: sw.note}
	if sw.oldNumber > 0 {
		old := sw.oldNumber
		h.OldNumber = &old
	}
	var drawID *int64
	if sw.drawID > 0 {
		id := sw.drawID
		drawID = &id
		h.DrawID = drawID
	}
	err := tx.QueryRow(ctx, `INSERT INTO user_number_history
(appid, user_id, old_number, new_number, tier, source, right_kind, right_level, draw_id, operator, note, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id, created_at`,
		sw.appID, sw.userID, h.OldNumber, sw.newNumber, sw.tier, sw.source, sw.rightKind, sw.rightLevel, drawID,
		truncateColumn(sw.operator, 128), truncateColumn(sw.note, 256), sw.now).Scan(&h.ID, &h.CreatedAt)
	return h, err
}

// ChangeUserNumber 用户自己换号：从摇号单里选、换回自己冻结中的旧号、或用改号卡 / 靓号券自选。
// tier 是服务层按应用规律判好的等级。
func (r *Repository) ChangeUserNumber(ctx context.Context, req usernumber.ChangeRequest, settings usernumber.Settings,
	tier int, now time.Time) (*usernumber.ChangeResult, error) {
	var result *usernumber.ChangeResult
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		cur, err := lockUserNumberTx(ctx, tx, req.AppID, req.UserID)
		if err != nil {
			return err
		}
		if cur.number == req.Number {
			return ErrUserNumberSame
		}
		if err := cooldownErr(settings, cur.changedAt, now); err != nil {
			return err
		}
		avail, err := checkUserNumber(ctx, tx, req.AppID, req.Number)
		if err != nil {
			return err
		}
		if err := avail.Err(req.UserID, false); err != nil {
			return err
		}

		sw := numberSwitch{appID: req.AppID, userID: req.UserID, oldNumber: cur.number, newNumber: req.Number, tier: tier,
			freezeDays: settings.FreezeDays, now: now}
		var used *usernumber.Right
		switch {
		case req.DrawID > 0:
			if err := pickFromDrawTx(ctx, tx, req.AppID, req.UserID, req.DrawID, req.Number, now); err != nil {
				return err
			}
			sw.source, sw.drawID = usernumber.SourceDraw, req.DrawID
		case avail.OwnFrozen(req.UserID):
			sw.source = usernumber.SourceFrozen
		default:
			if !settings.SelfPick {
				return ErrUserNumberSelfPickOff
			}
			kind, level := req.UseKind, req.UseLevel
			if kind == "" {
				rights, err := listUserNumberRights(ctx, tx, req.AppID, req.UserID)
				if err != nil {
					return err
				}
				var ok bool
				if kind, level, ok = usernumber.PickRight(rights, tier); !ok {
					return ErrUserNumberNoRight
				}
			} else if !usernumber.ValidRight(kind, level) || !usernumber.Accepts(kind, level, tier) {
				return ErrUserNumberNoRight
			}
			if err := consumeUserNumberRightTx(ctx, tx, req.AppID, req.UserID, kind, level, usernumber.OriginChange,
				strconv.FormatInt(req.Number, 10), ErrUserNumberNoRight); err != nil {
				return err
			}
			sw.rightKind, sw.rightLevel = kind, level
			sw.source = usernumber.SourceChange
			if kind == usernumber.RightTicket {
				sw.source = usernumber.SourceTicket
			}
			used = &usernumber.Right{Kind: kind, Level: level, Name: usernumber.RightName(kind, level)}
		}
		if _, err := applyUserNumberSwitchTx(ctx, tx, sw); err != nil {
			return err
		}
		rights, err := listUserNumberRights(ctx, tx, req.AppID, req.UserID)
		if err != nil {
			return err
		}
		old := cur.number
		result = &usernumber.ChangeResult{Number: req.Number, Tier: tier, OldNumber: &old, Source: sw.source, Used: used, Rights: rights}
		if settings.CooldownDays > 0 {
			next := now.Add(time.Duration(settings.CooldownDays) * 24 * time.Hour)
			result.NextChangeAt = &next
		}
		return nil
	})
	return result, err
}

func pickFromDrawTx(ctx context.Context, tx pgx.Tx, appID, userID, drawID, number int64, now time.Time) error {
	var status string
	var expiresAt time.Time
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT status, expires_at, candidates FROM user_number_draws
WHERE id = $1 AND appid = $2 AND user_id = $3 FOR UPDATE`, drawID, appID, userID).Scan(&status, &expiresAt, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNumberDrawNotFound
	}
	if err != nil {
		return err
	}
	if status != usernumber.DrawOpen || !now.Before(expiresAt) {
		return ErrUserNumberDrawNotFound
	}
	var candidates []usernumber.Candidate
	_ = json.Unmarshal(raw, &candidates)
	found := false
	for _, c := range candidates {
		if c.Number == number {
			found = true
			break
		}
	}
	if !found {
		return ErrUserNumberNotCandidate
	}
	_, err = tx.Exec(ctx, `UPDATE user_number_draws SET status = 'picked', picked = $2, closed_at = $3 WHERE id = $1`,
		drawID, number, now)
	return err
}

// AssignUserNumber 直接指定号码（管理员或物品发放）：不扣权益、不看冷却。
// Force（管理员）可以拿保留号、别人的冻结号与候选号；仍不能抢别人正在用的号。
func (r *Repository) AssignUserNumber(ctx context.Context, req usernumber.AssignRequest, settings usernumber.Settings,
	tier int, now time.Time) (*usernumber.History, error) {
	var h *usernumber.History
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var err error
		h, err = assignUserNumberTx(ctx, tx, req, settings, tier, now)
		return err
	})
	return h, err
}

func assignUserNumberTx(ctx context.Context, tx pgx.Tx, req usernumber.AssignRequest, settings usernumber.Settings,
	tier int, now time.Time) (*usernumber.History, error) {
	cur, err := lockUserNumberTx(ctx, tx, req.AppID, req.UserID)
	if err != nil {
		return nil, err
	}
	if cur.number == req.Number {
		return nil, ErrUserNumberSame
	}
	avail, err := checkUserNumber(ctx, tx, req.AppID, req.Number)
	if err != nil {
		return nil, err
	}
	if err := avail.Err(req.UserID, req.Force); err != nil {
		return nil, err
	}
	source := req.Source
	if source == "" {
		source = usernumber.SourceAdmin
	}
	// 候选号被管理员拿走时，那张摇号单里少了一个号（单子照旧，选它会报已被占用）
	return applyUserNumberSwitchTx(ctx, tx, numberSwitch{appID: req.AppID, userID: req.UserID, oldNumber: cur.number,
		newNumber: req.Number, tier: tier, source: source, operator: req.Operator, note: req.Note,
		freezeDays: settings.FreezeDays, now: now})
}

// UserNumberHistoryEntry 取一条换号记录（属于这个人）。
func (r *Repository) UserNumberHistoryEntry(ctx context.Context, appID, userID, historyID int64) (*usernumber.History, error) {
	h := &usernumber.History{}
	err := r.pool.QueryRow(ctx, `SELECT `+userNumberHistoryColumns+` FROM user_number_history h
LEFT JOIN users u ON u.id = h.user_id LEFT JOIN user_profiles p ON p.user_id = h.user_id
WHERE h.appid = $1 AND h.user_id = $2 AND h.id = $3`, appID, userID, historyID).Scan(historyScanTargets(h)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNumberHistoryMissing
	}
	return h, err
}

// ── 摇号 ──

// CreateUserNumberDraw 摇一次：锁用户 → 判冷却 → 扣一次摇号次数 → 作废上一张打开的单 → 建单 → 逐个占住候选号。
// candidates 由服务层造好（一般多造一些），这里按顺序占住前 want 个仍空闲的；一个都占不到时整笔回滚。
func (r *Repository) CreateUserNumberDraw(ctx context.Context, appID, userID int64, candidates []usernumber.Candidate,
	want int, luck float64, settings usernumber.Settings, now time.Time) (*usernumber.Draw, error) {
	var draw *usernumber.Draw
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		cur, err := lockUserNumberTx(ctx, tx, appID, userID)
		if err != nil {
			return err
		}
		if err := cooldownErr(settings, cur.changedAt, now); err != nil {
			return err
		}
		if err := closeOpenDrawsTx(ctx, tx, appID, userID, usernumber.DrawDiscarded, now); err != nil {
			return err
		}
		expires := now.Add(time.Duration(settings.DrawHoldMinutes) * time.Minute)
		draw = &usernumber.Draw{AppID: appID, UserID: userID, Luck: luck, Status: usernumber.DrawOpen, CreatedAt: now, ExpiresAt: expires,
			Candidates: []usernumber.Candidate{}}
		if err := tx.QueryRow(ctx, `INSERT INTO user_number_draws (appid, user_id, luck, status, created_at, expires_at)
VALUES ($1, $2, $3, 'open', $4, $5) RETURNING id`, appID, userID, luck, now, expires).Scan(&draw.ID); err != nil {
			return err
		}
		for _, c := range candidates {
			if len(draw.Candidates) >= want {
				break
			}
			tag, err := tx.Exec(ctx, `INSERT INTO user_number_holds (appid, number, kind, tier, user_id, draw_id, expires_at, created_by)
SELECT $1, $2, 'candidate', $3, $4, $5, $6, 'draw'
WHERE NOT EXISTS (SELECT 1 FROM users WHERE appid = $1 AND number = $2)
ON CONFLICT (appid, number) DO UPDATE
SET kind = 'candidate', tier = EXCLUDED.tier, user_id = EXCLUDED.user_id, draw_id = EXCLUDED.draw_id, note = '',
    expires_at = EXCLUDED.expires_at, created_by = EXCLUDED.created_by, created_at = NOW()
WHERE user_number_holds.expires_at IS NOT NULL AND user_number_holds.expires_at <= NOW()`,
				appID, c.Number, c.Tier, userID, draw.ID, expires)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				draw.Candidates = append(draw.Candidates, c)
			}
		}
		if len(draw.Candidates) == 0 {
			return ErrUserNumberNoCandidates
		}
		if err := consumeUserNumberRightTx(ctx, tx, appID, userID, usernumber.RightDraw, 0, usernumber.OriginDraw,
			strconv.FormatInt(draw.ID, 10), ErrUserNumberNoDrawRight); err != nil {
			return err
		}
		raw, _ := json.Marshal(draw.Candidates)
		_, err = tx.Exec(ctx, `UPDATE user_number_draws SET candidates = $2 WHERE id = $1`, draw.ID, raw)
		return err
	})
	if err != nil {
		return nil, err
	}
	return draw, nil
}

func closeOpenDrawsTx(ctx context.Context, tx pgx.Tx, appID, userID int64, status string, now time.Time) error {
	rows, err := tx.Query(ctx, `UPDATE user_number_draws SET status = $3, closed_at = $4
WHERE appid = $1 AND user_id = $2 AND status = 'open' RETURNING id`, appID, userID, status, now)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `DELETE FROM user_number_holds WHERE draw_id = ANY($1) AND kind = 'candidate'`, ids)
	return err
}

// GetOpenUserNumberDraw 当前打开的摇号单；过期的顺手标成 expired，返回 nil。
func (r *Repository) GetOpenUserNumberDraw(ctx context.Context, appID, userID int64, now time.Time) (*usernumber.Draw, error) {
	if _, err := r.pool.Exec(ctx, `UPDATE user_number_draws SET status = 'expired', closed_at = expires_at
WHERE appid = $1 AND user_id = $2 AND status = 'open' AND expires_at <= $3`, appID, userID, now); err != nil {
		return nil, err
	}
	draws, _, err := r.listUserNumberDraws(ctx, `d.appid = $1 AND d.user_id = $2 AND d.status = 'open'`, []any{appID, userID}, 1, 0)
	if err != nil || len(draws) == 0 {
		return nil, err
	}
	return &draws[0], nil
}

// DiscardUserNumberDraw 放弃一张打开的摇号单（次数不退），释放候选号。
func (r *Repository) DiscardUserNumberDraw(ctx context.Context, appID, userID, drawID int64, now time.Time) error {
	return pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE user_number_draws SET status = 'discarded', closed_at = $4
WHERE id = $1 AND appid = $2 AND user_id = $3 AND status = 'open'`, drawID, appID, userID, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrUserNumberDrawNotFound
		}
		_, err = tx.Exec(ctx, `DELETE FROM user_number_holds WHERE draw_id = $1 AND kind = 'candidate'`, drawID)
		return err
	})
}

// ListUserNumberDraws 摇号单列表（userID 为 0 时全应用）。
func (r *Repository) ListUserNumberDraws(ctx context.Context, appID, userID int64, limit, offset int) ([]usernumber.Draw, int64, error) {
	if userID > 0 {
		return r.listUserNumberDraws(ctx, `d.appid = $1 AND d.user_id = $2`, []any{appID, userID}, limit, offset)
	}
	return r.listUserNumberDraws(ctx, `d.appid = $1`, []any{appID}, limit, offset)
}

func (r *Repository) listUserNumberDraws(ctx context.Context, where string, args []any, limit, offset int) ([]usernumber.Draw, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_number_draws d WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.appid, d.user_id, d.candidates, d.luck, d.status, d.picked, d.created_at, d.expires_at, d.closed_at
FROM user_number_draws d WHERE `+where+fmt.Sprintf(` ORDER BY d.created_at DESC, d.id DESC LIMIT %d OFFSET %d`, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []usernumber.Draw{}
	for rows.Next() {
		var d usernumber.Draw
		var raw []byte
		if err := rows.Scan(&d.ID, &d.AppID, &d.UserID, &raw, &d.Luck, &d.Status, &d.Picked, &d.CreatedAt, &d.ExpiresAt, &d.ClosedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(raw, &d.Candidates)
		if d.Candidates == nil {
			d.Candidates = []usernumber.Candidate{}
		}
		out = append(out, d)
	}
	return out, total, rows.Err()
}

// ── 列表 ──

const userNumberHistoryColumns = `h.id, h.appid, h.user_id, h.old_number, h.new_number, h.tier, h.source, h.right_kind, h.right_level,
h.draw_id, h.operator, h.note, h.created_at, COALESCE(u.account, ''), COALESCE(p.nickname, '')`

func historyScanTargets(h *usernumber.History) []any {
	return []any{&h.ID, &h.AppID, &h.UserID, &h.OldNumber, &h.NewNumber, &h.Tier, &h.Source, &h.RightKind, &h.RightLvl,
		&h.DrawID, &h.Operator, &h.Note, &h.CreatedAt, &h.Account, &h.Nickname}
}

// ListUserNumberHistory 换号记录（userID 为 0 时全应用，新的在前）。
func (r *Repository) ListUserNumberHistory(ctx context.Context, appID, userID int64, limit, offset int) ([]usernumber.History, int64, error) {
	where, args := `h.appid = $1`, []any{appID}
	if userID > 0 {
		where, args = `h.appid = $1 AND h.user_id = $2`, []any{appID, userID}
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_number_history h WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+userNumberHistoryColumns+` FROM user_number_history h
LEFT JOIN users u ON u.id = h.user_id LEFT JOIN user_profiles p ON p.user_id = h.user_id
WHERE `+where+fmt.Sprintf(` ORDER BY h.created_at DESC, h.id DESC LIMIT %d OFFSET %d`, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []usernumber.History{}
	for rows.Next() {
		var h usernumber.History
		if err := rows.Scan(historyScanTargets(&h)...); err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ListUserNumberLedger 权益流水（userID 为 0 时全应用；kind 为空时全部种类）。
func (r *Repository) ListUserNumberLedger(ctx context.Context, appID, userID int64, kind string, limit, offset int) ([]usernumber.LedgerEntry, int64, error) {
	where, args := `l.appid = $1`, []any{appID}
	if userID > 0 {
		args = append(args, userID)
		where += fmt.Sprintf(` AND l.user_id = $%d`, len(args))
	}
	if kind != "" {
		args = append(args, kind)
		where += fmt.Sprintf(` AND l.kind = $%d`, len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_number_ledger l WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT l.id, l.appid, l.user_id, l.kind, l.level, l.delta, l.balance_after, l.origin, l.ref, l.title,
l.operator, l.created_at, COALESCE(u.account, ''), COALESCE(p.nickname, '')
FROM user_number_ledger l LEFT JOIN users u ON u.id = l.user_id LEFT JOIN user_profiles p ON p.user_id = l.user_id
WHERE `+where+fmt.Sprintf(` ORDER BY l.created_at DESC, l.id DESC LIMIT %d OFFSET %d`, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []usernumber.LedgerEntry{}
	for rows.Next() {
		var e usernumber.LedgerEntry
		if err := rows.Scan(&e.ID, &e.AppID, &e.UserID, &e.Kind, &e.Level, &e.Delta, &e.BalanceAfter, &e.Origin, &e.Ref, &e.Title,
			&e.Operator, &e.CreatedAt, &e.Account, &e.Nickname); err != nil {
			return nil, 0, err
		}
		e.Name = usernumber.RightName(e.Kind, e.Level)
		e.OriginLabel = usernumber.OriginLabel(e.Origin)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// ListUserNumberHolds 占用列表（kind 为空时全部；只列仍生效的）。
func (r *Repository) ListUserNumberHolds(ctx context.Context, appID int64, kind string, limit, offset int) ([]usernumber.Hold, int64, error) {
	where, args := `appid = $1 AND (expires_at IS NULL OR expires_at > NOW())`, []any{appID}
	if kind != "" {
		args = append(args, kind)
		where += ` AND kind = $2`
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_number_holds WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT appid, number, kind, tier, user_id, draw_id, note, expires_at, created_by, created_at
FROM user_number_holds WHERE `+where+fmt.Sprintf(` ORDER BY created_at DESC, number LIMIT %d OFFSET %d`, limit, offset), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []usernumber.Hold{}
	for rows.Next() {
		var h usernumber.Hold
		if err := rows.Scan(&h.AppID, &h.Number, &h.Kind, &h.Tier, &h.UserID, &h.DrawID, &h.Note, &h.ExpiresAt, &h.CreatedBy, &h.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, h)
	}
	return out, total, rows.Err()
}

// ReserveUserNumbers 把一批号码设为保留号（已有人在用的跳过；覆盖冻结与候选）。返回实际保留的号码。
func (r *Repository) ReserveUserNumbers(ctx context.Context, appID int64, holds []usernumber.Hold, operator string) ([]int64, error) {
	var added []int64
	err := pgx.BeginTxFunc(ctx, r.pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
		for _, h := range holds {
			tag, err := tx.Exec(ctx, `INSERT INTO user_number_holds (appid, number, kind, tier, note, expires_at, created_by)
SELECT $1, $2, 'reserved', $3, $4, $5, $6
WHERE NOT EXISTS (SELECT 1 FROM users WHERE appid = $1 AND number = $2)
ON CONFLICT (appid, number) DO UPDATE
SET kind = 'reserved', tier = EXCLUDED.tier, user_id = NULL, draw_id = NULL, note = EXCLUDED.note,
    expires_at = EXCLUDED.expires_at, created_by = EXCLUDED.created_by, created_at = NOW()`,
				appID, h.Number, h.Tier, truncateColumn(h.Note, 256), h.ExpiresAt, truncateColumn(operator, 128))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				added = append(added, h.Number)
			}
		}
		return nil
	})
	return added, err
}

// ReleaseUserNumberHold 删掉一条占用（保留、冻结或候选）。返回是否删到。
func (r *Repository) ReleaseUserNumberHold(ctx context.Context, appID, number int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM user_number_holds WHERE appid = $1 AND number = $2`, appID, number)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UserNumberOverview 管理端总览。
func (r *Repository) UserNumberOverview(ctx context.Context, appID int64, dayStart time.Time) (*usernumber.Overview, error) {
	out := &usernumber.Overview{Holds: map[string]int64{}, ByTier: []usernumber.TierTally{}, Outstanding: []usernumber.Right{}}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(number) FROM users WHERE appid = $1`, appID).
		Scan(&out.Users, &out.Numbered); err != nil {
		return nil, err
	}
	tiers := map[int]int64{}
	rows, err := r.pool.Query(ctx, `SELECT number_tier, COUNT(*) FROM users WHERE appid = $1 GROUP BY number_tier`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tier int
		var n int64
		if err := rows.Scan(&tier, &n); err != nil {
			rows.Close()
			return nil, err
		}
		tiers[tier] = n
	}
	rows.Close()
	for tier := usernumber.TierCommon; tier < usernumber.TierCount; tier++ {
		out.ByTier = append(out.ByTier, usernumber.TierTally{Tier: tier, Count: tiers[tier]})
	}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE created_at >= $2),
  COUNT(*) FILTER (WHERE created_at >= $2 - INTERVAL '6 days')
FROM user_number_history WHERE appid = $1 AND created_at >= $2 - INTERVAL '6 days'`, appID, dayStart).
		Scan(&out.ChangesToday, &out.Changes7d); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE created_at >= $2),
  COUNT(*) FILTER (WHERE status = 'open' AND expires_at > NOW())
FROM user_number_draws WHERE appid = $1 AND (created_at >= $2 OR status = 'open')`, appID, dayStart).
		Scan(&out.DrawsToday, &out.OpenDraws); err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, `SELECT kind, COUNT(*) FROM user_number_holds
WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW()) GROUP BY kind`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var n int64
		if err := rows.Scan(&kind, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out.Holds[kind] = n
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT kind, level, COALESCE(SUM(balance), 0), COALESCE(SUM(earned), 0), COALESCE(SUM(used), 0)
FROM user_number_rights WHERE appid = $1 GROUP BY kind, level ORDER BY kind, level`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var right usernumber.Right
		if err := rows.Scan(&right.Kind, &right.Level, &right.Balance, &right.Earned, &right.Used); err != nil {
			return nil, err
		}
		right.Name = usernumber.RightName(right.Kind, right.Level)
		out.Outstanding = append(out.Outstanding, right)
	}
	return out, rows.Err()
}

// ── 各发放渠道共用 ──

// 各渠道目录里号码权益的类型名（物品发放、卡密、积分商城、邀请 / 排行榜 / 推广、抽奖、激励广告同名）。
const (
	RewardNumberChange = "number_change"
	RewardNumberTicket = "number_ticket"
	RewardNumberDraw   = "number_draw"
)

// numberRewardRight 奖励类型 → 权益种类与等级（靓号券的等级在 refId 里，1–4）。
func numberRewardRight(rewardType string, refID int64) (string, int, error) {
	switch rewardType {
	case RewardNumberChange:
		return usernumber.RightChange, 0, nil
	case RewardNumberDraw:
		return usernumber.RightDraw, 0, nil
	case RewardNumberTicket:
		if refID < 1 || refID > 4 {
			return "", 0, fmt.Errorf("靓号券等级须为 1–4，实际 %d", refID)
		}
		return usernumber.RightTicket, int(refID), nil
	}
	return "", 0, fmt.Errorf("不是号码权益：%s", rewardType)
}

// grantNumberRewardTx 各渠道发号码权益的统一入口：返回发放后的余额与一句「（现有 N 张）」的说明。
func grantNumberRewardTx(ctx context.Context, tx pgx.Tx, appID, userID int64, rewardType string, amount, refID int64,
	origin, ref, title string) (int64, string, error) {
	kind, level, err := numberRewardRight(rewardType, refID)
	if err != nil {
		return 0, "", err
	}
	balance, err := grantUserNumberRightTx(ctx, tx, usernumber.Grant{AppID: appID, UserID: userID, Kind: kind, Level: level,
		Amount: amount, Origin: origin, Ref: ref, Title: title})
	if err != nil {
		return 0, "", err
	}
	unit := "张"
	if kind == usernumber.RightDraw {
		unit = "次"
	}
	return balance, "（现有 " + strconv.FormatInt(balance, 10) + " " + unit + "）", nil
}

// assignNumberRewardTx 物品发放「指定号码」：按应用当前设置判等级，在调用方的事务里换号（不扣权益、不看冷却）。
// 已经是这个号时算成功。
func assignNumberRewardTx(ctx context.Context, tx pgx.Tx, appID, userID int64, text, title string) (string, error) {
	number, err := usernumber.ParseNumber(text)
	if err != nil {
		return "", err
	}
	settings, err := getUserNumberSettings(ctx, tx, appID)
	if err != nil {
		return "", err
	}
	cls := usernumber.Classify(number, &settings)
	_, err = assignUserNumberTx(ctx, tx, usernumber.AssignRequest{AppID: appID, UserID: userID, Number: number,
		Source: usernumber.SourceGrant, Operator: "item_grant", Note: title}, settings, cls.Tier, time.Now())
	if errors.Is(err, ErrUserNumberSame) {
		return "（已是这个号码）", nil
	}
	if err != nil {
		return "", err
	}
	return "（" + cls.TierName + "）", nil
}
