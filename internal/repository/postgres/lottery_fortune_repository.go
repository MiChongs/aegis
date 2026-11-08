package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// ErrFortuneControlNotFound 运气干预不存在（或不属于这个应用 / 活动）。
var ErrFortuneControlNotFound = errors.New("lottery fortune control not found")

// ErrFortuneUsersNotFound 选中的用户里有不属于这个应用的。
var ErrFortuneUsersNotFound = errors.New("lottery fortune users not found")

const fortuneControlColumns = `c.id, c.appid, c.activity_id, c.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''),
c.kind, c.z, c.level, c.salt, c.starts_at, c.ends_at, c.note, c.operator, c.revoked_at, c.revoked_by, c.created_at`

const fortuneControlJoins = `FROM lottery_fortune_controls c
LEFT JOIN users u ON u.id = c.user_id
LEFT JOIN user_profiles up ON up.user_id = c.user_id`

func scanFortuneControl(row interface{ Scan(dest ...any) error }, now time.Time) (lotterydomain.FortuneControl, error) {
	var item lotterydomain.FortuneControl
	err := row.Scan(&item.ID, &item.AppID, &item.ActivityID, &item.UserID, &item.Account, &item.Nickname,
		&item.Kind, &item.Z, &item.Level, &item.Salt, &item.StartsAt, &item.EndsAt, &item.Note, &item.Operator,
		&item.Revoked, &item.RevokedBy, &item.CreatedAt)
	item.Status = item.StatusAt(now)
	return item, err
}

// lotteryFortuneControlsQ 这个活动里与 [userID] 相关（全站或本人）、未撤销、在 now 还没到期的干预。
// 抽奖事务里用事务连接取，与出奖同一个快照。
func lotteryFortuneControlsQ(ctx context.Context, q queryExecutor, activityID, userID int64, now time.Time) (
	[]lotterydomain.FortuneControl, error) {
	rows, err := q.Query(ctx, lotteryFortuneControlsSQL, activityID, userID, now)
	if err != nil {
		return nil, err
	}
	return scanFortuneControlRows(rows, now)
}

// lotteryFortuneControlsSQL 见 lotteryFortuneControlsQ（$1 活动、$2 用户、$3 此刻）。
const lotteryFortuneControlsSQL = `SELECT ` + fortuneControlColumns + ` ` + fortuneControlJoins + `
WHERE c.activity_id = $1 AND (c.user_id IS NULL OR c.user_id = $2) AND c.revoked_at IS NULL AND c.ends_at > $3
ORDER BY c.id`

// scanFortuneControlRows 读完并关闭 rows（单发与批量管道共用）。
func scanFortuneControlRows(rows pgx.Rows, now time.Time) ([]lotterydomain.FortuneControl, error) {
	defer rows.Close()
	out := []lotterydomain.FortuneControl{}
	for rows.Next() {
		item, err := scanFortuneControl(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// LotteryFortuneControls 活动页、模拟用：同 lotteryFortuneControlsQ。
func (r *Repository) LotteryFortuneControls(ctx context.Context, activityID, userID int64, now time.Time) (
	[]lotterydomain.FortuneControl, error) {
	return lotteryFortuneControlsQ(ctx, r.pool, activityID, userID, now)
}

// LotteryGlobalFortuneControls 这个活动的全站干预（未撤销、还没到期），统计今日分布时与每个人的合起来算。
func (r *Repository) LotteryGlobalFortuneControls(ctx context.Context, activityID int64, now time.Time) (
	[]lotterydomain.FortuneControl, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+fortuneControlColumns+` `+fortuneControlJoins+`
WHERE c.activity_id = $1 AND c.user_id IS NULL AND c.revoked_at IS NULL AND c.ends_at > $2
ORDER BY c.id`, activityID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lotterydomain.FortuneControl{}
	for rows.Next() {
		item, err := scanFortuneControl(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// LotteryUserFortuneControls 一批用户各自的个人干预（未撤销、还没到期），按用户分组。
func (r *Repository) LotteryUserFortuneControls(ctx context.Context, activityID int64, userIDs []int64, now time.Time) (
	map[int64][]lotterydomain.FortuneControl, error) {
	out := map[int64][]lotterydomain.FortuneControl{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+fortuneControlColumns+` `+fortuneControlJoins+`
WHERE c.activity_id = $1 AND c.user_id = ANY($2) AND c.revoked_at IS NULL AND c.ends_at > $3
ORDER BY c.id`, activityID, userIDs, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanFortuneControl(rows, now)
		if err != nil {
			return nil, err
		}
		out[*item.UserID] = append(out[*item.UserID], item)
	}
	return out, rows.Err()
}

// CreateLotteryFortuneControls 下一次干预：全站一条，指定用户每人一条（同一批共用开始结束时间与备注）。
// 用户必须属于这个应用。reroll 每条各自一段随机盐（同一批里的人也各换各的手气）。
func (r *Repository) CreateLotteryFortuneControls(ctx context.Context, input lotterydomain.ControlInput, z float64,
	now time.Time) ([]lotterydomain.FortuneControl, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	targets := []*int64{nil}
	if !input.AllUsers {
		var found int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1 AND id = ANY($2)`,
			input.AppID, input.UserIDs).Scan(&found); err != nil {
			return nil, err
		}
		if found != len(input.UserIDs) {
			return nil, ErrFortuneUsersNotFound
		}
		targets = targets[:0]
		for index := range input.UserIDs {
			targets = append(targets, &input.UserIDs[index])
		}
	}
	level := ""
	if input.Kind == lotterydomain.ControlPin {
		level = input.Level
	}
	ids := make([]int64, 0, len(targets))
	for _, userID := range targets {
		salt := ""
		if input.Kind == lotterydomain.ControlReroll {
			if salt, err = randomSalt(); err != nil {
				return nil, err
			}
		}
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO lottery_fortune_controls
(appid, activity_id, user_id, kind, z, level, salt, starts_at, ends_at, note, operator)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			input.AppID, input.ActivityID, userID, input.Kind, z, level, salt, input.StartsAt, input.EndsAt,
			input.Note, input.Operator).Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.fortuneControlsByID(ctx, ids, now)
}

func (r *Repository) fortuneControlsByID(ctx context.Context, ids []int64, now time.Time) ([]lotterydomain.FortuneControl, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+fortuneControlColumns+` `+fortuneControlJoins+`
WHERE c.id = ANY($1) ORDER BY c.id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lotterydomain.FortuneControl{}
	for rows.Next() {
		item, err := scanFortuneControl(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// RevokeLotteryFortuneControls 撤销干预（已撤销的不重复记）。返回撤销了几条。
// [ids] 为空且 [all] 为真时撤销这个活动所有生效中与待生效的干预（「全部恢复自然运气」）。
func (r *Repository) RevokeLotteryFortuneControls(ctx context.Context, appID, activityID int64, ids []int64, all bool,
	operator string, now time.Time) (int64, error) {
	if !all && len(ids) == 0 {
		return 0, nil
	}
	sql := `UPDATE lottery_fortune_controls SET revoked_at = $3, revoked_by = $4
WHERE appid = $1 AND activity_id = $2 AND revoked_at IS NULL`
	args := []any{appID, activityID, now, operator}
	if all {
		sql += ` AND ends_at > $3`
	} else {
		sql += ` AND id = ANY($5)`
		args = append(args, ids)
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	if !all && tag.RowsAffected() == 0 {
		return 0, ErrFortuneControlNotFound
	}
	return tag.RowsAffected(), nil
}

// FortuneControlQuery 干预记录的筛选。
type FortuneControlQuery struct {
	AppID      int64
	ActivityID int64
	UserID     int64
	// Status active / scheduled / expired / revoked，空为全部
	Status string
	Kind   string
	Page   int
	Limit  int
}

// ListLotteryFortuneControls 干预记录，新的在前。
func (r *Repository) ListLotteryFortuneControls(ctx context.Context, query FortuneControlQuery, now time.Time) (
	*lotterydomain.Page[lotterydomain.FortuneControl], error) {
	where := []string{"c.appid = $1", "c.activity_id = $2"}
	args := []any{query.AppID, query.ActivityID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.UserID > 0 {
		add("c.user_id = ?", query.UserID)
	}
	if query.Kind != "" {
		add("c.kind = ?", query.Kind)
	}
	switch query.Status {
	case lotterydomain.ControlRevoked:
		where = append(where, "c.revoked_at IS NOT NULL")
	case lotterydomain.ControlActive:
		add("c.revoked_at IS NULL AND c.starts_at <= ? AND c.ends_at > ?", now)
	case lotterydomain.ControlScheduled:
		add("c.revoked_at IS NULL AND c.starts_at > ?", now)
	case lotterydomain.ControlExpired:
		add("c.revoked_at IS NULL AND c.ends_at <= ?", now)
	}
	whereSQL := strings.Join(where, " AND ")
	page, limit := query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	result := &lotterydomain.Page[lotterydomain.FortuneControl]{Items: []lotterydomain.FortuneControl{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+fortuneControlJoins+` WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+fortuneControlColumns+` `+fortuneControlJoins+` WHERE `+whereSQL+`
ORDER BY c.created_at DESC, c.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanFortuneControl(rows, now)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// FortuneControlCounts 这个活动此刻生效中与待生效的干预数（分全站 / 个人）。
type FortuneControlCounts struct {
	ActiveGlobal int `json:"activeGlobal"`
	ActiveUsers  int `json:"activeUsers"`
	Scheduled    int `json:"scheduled"`
}

// LotteryFortuneControlCounts 生效中与待生效的干预数。
func (r *Repository) LotteryFortuneControlCounts(ctx context.Context, activityID int64, now time.Time) (FortuneControlCounts, error) {
	var out FortuneControlCounts
	err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE user_id IS NULL AND starts_at <= $2),
  COUNT(DISTINCT user_id) FILTER (WHERE user_id IS NOT NULL AND starts_at <= $2),
  COUNT(*) FILTER (WHERE starts_at > $2)
FROM lottery_fortune_controls
WHERE activity_id = $1 AND revoked_at IS NULL AND ends_at > $2`, activityID, now).
		Scan(&out.ActiveGlobal, &out.ActiveUsers, &out.Scheduled)
	return out, err
}

// LotteryDrawersSince 这个活动 since 之后抽过奖的用户（今日运气分布按他们算），最多 limit 个，最近抽的在前。
func (r *Repository) LotteryDrawersSince(ctx context.Context, activityID int64, since time.Time, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id FROM lottery_draws
WHERE activity_id = $1 AND created_at >= $2
GROUP BY user_id ORDER BY MAX(created_at) DESC LIMIT $3`, activityID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func randomSalt() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
