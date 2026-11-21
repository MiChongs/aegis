package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/itemgrant"
)

// AppFunctionGrantInput 远程函数 aegis.rights.grant 的一笔发放。
type AppFunctionGrantInput struct {
	AppID      int64
	FunctionID int64
	Function   string
	UserID     int64
	EventID    string
	// Key 幂等键：同一个函数里、同一个人、同一个键只发一次，第二次直接回第一次的结果
	Key   string
	Title string
	Items []itemgrant.Item
}

// AppFunctionGrantResult 发放结果。
type AppFunctionGrantResult struct {
	GrantNo  string                 `json:"grantNo"`
	Replayed bool                   `json:"replayed"`
	Results  []itemgrant.ItemResult `json:"results"`
}

// GrantAppFunctionItems 在一个事务里把一组物品发给一个人：锁用户 → 占幂等键 → 逐项发放 → 提交。
//
// 逐项复用物品发放引擎的 grantItemTx，因此从远程函数发出去的会员、功能解锁、抽奖次数、装扮……
// 与积分商城、卡密、发放规则发的完全同口径（会员折算、次数批次、功能时长账户、排行榜分类）。
// 任何一项发不出去整笔回滚，幂等键也随之释放 —— 脚本修好之后用同一个键重试能正常发出。
func (r *Repository) GrantAppFunctionItems(ctx context.Context, input AppFunctionGrantInput) (*AppFunctionGrantResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := lockUserTx(ctx, tx, input.AppID, input.UserID); err != nil {
		return nil, err
	}

	// 同一个键已经发过：回第一次的结果
	var existingID int64
	var existingResults []byte
	err = tx.QueryRow(ctx, `SELECT id, results FROM app_function_grants WHERE appid=$1 AND function_id=$2 AND user_id=$3 AND grant_key=$4`,
		input.AppID, input.FunctionID, input.UserID, input.Key).Scan(&existingID, &existingResults)
	if err == nil {
		out := &AppFunctionGrantResult{GrantNo: appFunctionGrantNo(existingID), Replayed: true, Results: []itemgrant.ItemResult{}}
		_ = json.Unmarshal(existingResults, &out.Results)
		return out, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	rawItems, _ := json.Marshal(input.Items)
	var eventID any
	if input.EventID != "" {
		eventID = input.EventID
	}
	var grantID int64
	if err := tx.QueryRow(ctx, `INSERT INTO app_function_grants (appid, function_id, user_id, event_id, grant_key, title, items)
VALUES ($1,$2,$3,$4::uuid,$5,$6,$7) RETURNING id`,
		input.AppID, input.FunctionID, input.UserID, eventID, truncateRunes(input.Key, 160),
		truncateRunes(input.Title, 128), rawItems).Scan(&grantID); err != nil {
		return nil, err
	}

	refs, err := itemGrantRefsQ(ctx, tx, input.AppID)
	if err != nil {
		return nil, err
	}
	grantNo := appFunctionGrantNo(grantID)
	grant := itemGrantContext{
		AppID: input.AppID, UserID: input.UserID, RecordID: grantID, RecordNo: grantNo,
		Title: input.Title, Source: "远程函数 " + input.Function, Refs: refs, Now: time.Now(),
		Meta: map[string]any{"source": "app_function", "function": input.Function, "grant": grantID, "eventId": input.EventID},
	}
	results := make([]itemgrant.ItemResult, 0, len(input.Items))
	for i, item := range input.Items {
		result, err := r.grantItemTx(ctx, tx, grant, i, itemgrant.PlannedItem{NodeID: "function", Item: item})
		if err != nil {
			return nil, errors.New(itemGrantFailureMessage(err))
		}
		results = append(results, result)
	}
	rawResults, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE app_function_grants SET results=$2 WHERE id=$1`, grantID, rawResults); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &AppFunctionGrantResult{GrantNo: grantNo, Results: results}, nil
}

// AppFunctionItemGrantRefs 应用的套餐 / 功能 / 装扮引用表（发放前核对引用是否存在，试跑时描述物品）。
func (r *Repository) AppFunctionItemGrantRefs(ctx context.Context, appID int64) (itemgrant.Refs, error) {
	return itemGrantRefsQ(ctx, r.pool, appID)
}

func appFunctionGrantNo(id int64) string { return "AFG" + strconv.FormatInt(id, 10) }
