package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	functiondomain "aegis/internal/domain/appfunction"
)

// appFunctionColumns 与 scanAppFunction 的读取顺序严格一一对应。
// 提成常量是因为它出现在五处 SQL 里，逐处手抄迟早会漏掉新加的列，
// 而 pgx 的表现是「Scan 位置错位」而不是编译错误。
const appFunctionColumns = `id, appid, name, description, runtime, status, active_version, capabilities,
timeout_ms, max_request_bytes, max_response_bytes, max_concurrency, rate_limit_per_min, config,
created_by, created_at, updated_at`

func (r *Repository) CreateAppFunction(ctx context.Context, input functiondomain.CreateFunctionInput) (*functiondomain.Function, error) {
	capabilities, _ := json.Marshal(input.Capabilities)
	config := input.Config
	if len(config) == 0 {
		config = json.RawMessage(`{}`)
	}
	return scanAppFunction(r.pool.QueryRow(ctx, `INSERT INTO app_functions
(appid, name, description, runtime, capabilities, timeout_ms, max_request_bytes, max_response_bytes,
max_concurrency, rate_limit_per_min, config, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING `+appFunctionColumns,
		input.AppID, input.Name, input.Description, input.Runtime, capabilities, input.TimeoutMs,
		input.MaxRequestBytes, input.MaxResponseBytes, input.MaxConcurrency, input.RateLimitPerMin,
		[]byte(config), input.CreatedBy))
}

func (r *Repository) ListAppFunctions(ctx context.Context, appID int64) ([]functiondomain.Function, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+appFunctionColumns+`
FROM app_functions WHERE appid=$1 ORDER BY name`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.Function, 0, 8)
	for rows.Next() {
		item, err := scanAppFunction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAppFunction(ctx context.Context, appID int64, name string) (*functiondomain.Function, error) {
	return scanAppFunction(r.pool.QueryRow(ctx, `SELECT `+appFunctionColumns+`
FROM app_functions WHERE appid=$1 AND name=$2`, appID, name))
}

func (r *Repository) UpdateAppFunction(ctx context.Context, appID int64, name string, input functiondomain.UpdateFunctionInput) (*functiondomain.Function, error) {
	var capabilities any
	if input.Capabilities != nil {
		capabilities, _ = json.Marshal(input.Capabilities)
	}
	var config any
	if len(input.Config) > 0 {
		config = []byte(input.Config)
	}
	return scanAppFunction(r.pool.QueryRow(ctx, `UPDATE app_functions SET
description=COALESCE($3,description), status=COALESCE($4,status),
capabilities=CASE WHEN $5::jsonb IS NULL THEN capabilities ELSE $5::jsonb END,
timeout_ms=COALESCE($6,timeout_ms), max_request_bytes=COALESCE($7,max_request_bytes),
max_response_bytes=COALESCE($8,max_response_bytes),
max_concurrency=COALESCE($9,max_concurrency), rate_limit_per_min=COALESCE($10,rate_limit_per_min),
config=CASE WHEN $11::jsonb IS NULL THEN config ELSE $11::jsonb END,
updated_at=NOW()
WHERE appid=$1 AND name=$2
RETURNING `+appFunctionColumns,
		appID, name, input.Description, input.Status, capabilities, input.TimeoutMs,
		input.MaxRequestBytes, input.MaxResponseBytes, input.MaxConcurrency, input.RateLimitPerMin, config))
}

func (r *Repository) DeleteAppFunction(ctx context.Context, appID int64, name string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_functions WHERE appid=$1 AND name=$2`, appID, name)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// appFunctionVersionColumns 与 scanAppFunctionVersion 一一对应，同 appFunctionColumns。
//
// 注意 wasm_module 与 source 在这里被读出来，但它们在 domain 上都是 `json:"-"`：
// 「脚本正文永远不通过 API 下发」这条保证由类型保证，不靠每处查询记得不选它。
const appFunctionVersionColumns = `id, function_id, appid, version, endpoint_url, response_public_key,
wasm_module, source, notes, artifact_sha256, status, created_by, created_at, activated_at`

func (r *Repository) CreateAppFunctionVersion(ctx context.Context, input functiondomain.CreateVersionInput) (*functiondomain.Version, error) {
	return scanAppFunctionVersion(r.pool.QueryRow(ctx, `INSERT INTO app_function_versions
(function_id, appid, version, endpoint_url, response_public_key, wasm_module, source, notes, artifact_sha256, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING `+appFunctionVersionColumns,
		input.FunctionID, input.AppID, input.Version, input.EndpointURL, input.ResponsePublicKey,
		nullableBytes(input.WASMModule), input.Source, input.Notes, input.ArtifactSHA256, input.CreatedBy))
}

func (r *Repository) ListAppFunctionVersions(ctx context.Context, appID, functionID int64) ([]functiondomain.Version, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+appFunctionVersionColumns+`
FROM app_function_versions WHERE appid=$1 AND function_id=$2 ORDER BY created_at DESC`, appID, functionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.Version, 0, 8)
	for rows.Next() {
		item, err := scanAppFunctionVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetAppFunctionVersion(ctx context.Context, appID, functionID int64, version string) (*functiondomain.Version, error) {
	return scanAppFunctionVersion(r.pool.QueryRow(ctx, `SELECT `+appFunctionVersionColumns+`
FROM app_function_versions WHERE appid=$1 AND function_id=$2 AND version=$3`,
		appID, functionID, version))
}

// DeleteAppFunctionVersion 删除一个非激活版本。
//
// 激活中的版本删不掉（SQL 里就挡住），否则会出现「函数 active_version 指向一条
// 不存在的记录」——调用时表现为 40992，而列表上看不出任何异常。
func (r *Repository) DeleteAppFunctionVersion(ctx context.Context, appID, functionID int64, version string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_function_versions
WHERE appid=$1 AND function_id=$2 AND version=$3 AND status<>'active'`, appID, functionID, version)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) ActivateAppFunctionVersion(ctx context.Context, appID, functionID int64, version string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE app_function_versions SET status='active', activated_at=NOW()
WHERE appid=$1 AND function_id=$2 AND version=$3`, appID, functionID, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return normalizeNotFound(nil)
	}
	if _, err = tx.Exec(ctx, `UPDATE app_function_versions SET status='retired'
WHERE appid=$1 AND function_id=$2 AND version<>$3 AND status='active'`, appID, functionID, version); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE app_functions SET active_version=$3, status='active', updated_at=NOW()
WHERE appid=$1 AND id=$2`, appID, functionID, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) GetAppFunctionInvocationByEvent(ctx context.Context, appID int64, eventID string) (*functiondomain.Invocation, error) {
	return scanAppFunctionInvocation(r.pool.QueryRow(ctx, invocationSelect+` WHERE appid=$1 AND event_id=$2`, appID, eventID))
}

func (r *Repository) InsertAppFunctionInvocation(ctx context.Context, item functiondomain.Invocation) (*functiondomain.Invocation, error) {
	var raw any
	if item.Result != nil {
		raw, _ = json.Marshal(item.Result)
	}
	return scanAppFunctionInvocation(r.pool.QueryRow(ctx, `INSERT INTO app_function_invocations
(event_id, appid, function_id, version_id, caller_type, caller_id, status, duration_ms,
request_sha256, response_sha256, error_message, result)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING id, event_id::text, appid, function_id, version_id, caller_type, caller_id, status,
duration_ms, request_sha256, response_sha256, error_message, result, created_at`,
		item.EventID, item.AppID, item.FunctionID, item.VersionID, item.CallerType, item.CallerID,
		item.Status, item.DurationMs, item.RequestSHA256, item.ResponseSHA256, item.ErrorMessage, raw))
}

func (r *Repository) ReserveAppFunctionInvocation(ctx context.Context, item functiondomain.Invocation) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO app_function_invocations
(event_id, appid, function_id, version_id, caller_type, caller_id, status, request_sha256)
VALUES ($1,$2,$3,$4,$5,$6,'running',$7)
ON CONFLICT (appid, event_id) DO UPDATE SET
function_id=EXCLUDED.function_id, version_id=EXCLUDED.version_id,
caller_type=EXCLUDED.caller_type, caller_id=EXCLUDED.caller_id,
status='running', duration_ms=0, request_sha256=EXCLUDED.request_sha256,
response_sha256='', error_message='', result=NULL, created_at=NOW()
WHERE app_function_invocations.status='running'
AND app_function_invocations.created_at < NOW() - INTERVAL '2 minutes'`,
		item.EventID, item.AppID, item.FunctionID, item.VersionID, item.CallerType, item.CallerID, item.RequestSHA256)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) CompleteAppFunctionInvocation(ctx context.Context, item functiondomain.Invocation) error {
	var raw any
	if item.Result != nil {
		raw, _ = json.Marshal(item.Result)
	}
	_, err := r.pool.Exec(ctx, `UPDATE app_function_invocations SET
status=$3, duration_ms=$4, response_sha256=$5, error_message=$6, result=$7
WHERE appid=$1 AND event_id=$2`,
		item.AppID, item.EventID, item.Status, item.DurationMs, item.ResponseSHA256,
		item.ErrorMessage, raw)
	return err
}

func (r *Repository) ListAppFunctionInvocations(
	ctx context.Context,
	appID, functionID int64,
	query functiondomain.InvocationQuery,
) (*functiondomain.InvocationPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := query.Page
	if page <= 0 {
		page = 1
	}

	// 条件与参数一起累加，避免出现「SQL 里写了 $4 但只传了三个参数」这种
	// 只在特定筛选组合下才触发的运行时错误。
	where := []string{"appid=$1", "function_id=$2"}
	args := []any{appID, functionID}
	if query.Status != "" {
		args = append(args, query.Status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	if query.CallerType != "" {
		args = append(args, query.CallerType)
		where = append(where, fmt.Sprintf("caller_type=$%d", len(args)))
	}
	if query.EventID != "" {
		args = append(args, query.EventID)
		where = append(where, fmt.Sprintf("event_id=$%d::uuid", len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM app_function_invocations`+clause, args...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, invocationSelect+clause+
		fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.Invocation, 0, limit)
	for rows.Next() {
		item, err := scanAppFunctionInvocation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &functiondomain.InvocationPage{List: items, Total: total, Page: page, Limit: limit}, nil
}

// AppFunctionStats 聚合一个函数近 windowHours 小时的运行状况。
//
// 三段查询而不是一条大 SQL：分位数、错误 Top、分桶各有各的聚合维度，
// 硬拼成一条会得到一个谁也看不懂、改一处就全错的语句。
func (r *Repository) AppFunctionStats(
	ctx context.Context,
	appID, functionID int64,
	windowHours int,
) (*functiondomain.Stats, error) {
	if windowHours <= 0 || windowHours > 24*30 {
		windowHours = 24
	}
	since := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	stats := functiondomain.Stats{WindowHours: windowHours, TopErrors: []functiondomain.StatsError{}, Buckets: []functiondomain.StatsBucket{}}

	err := r.pool.QueryRow(ctx, `SELECT
COUNT(*),
COUNT(*) FILTER (WHERE status='success'),
COUNT(*) FILTER (WHERE status='error'),
COUNT(*) FILTER (WHERE status='running'),
COALESCE(AVG(duration_ms) FILTER (WHERE status<>'running'), 0),
COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status<>'running'), 0),
COALESCE(MAX(duration_ms), 0),
MAX(created_at)
FROM app_function_invocations WHERE appid=$1 AND function_id=$2 AND created_at>=$3`,
		appID, functionID, since).Scan(&stats.Total, &stats.Success, &stats.Failed, &stats.Running,
		&stats.AvgMs, &stats.P95Ms, &stats.MaxMs, &stats.LastInvokedAt)
	if err != nil {
		return nil, err
	}
	// 分母只算已结束的调用：把执行中的算成失败会让刚发布的函数看起来在报错
	if finished := stats.Success + stats.Failed; finished > 0 {
		stats.SuccessRate = float64(stats.Success) / float64(finished)
	}

	errorRows, err := r.pool.Query(ctx, `SELECT error_message, COUNT(*) AS hits
FROM app_function_invocations
WHERE appid=$1 AND function_id=$2 AND created_at>=$3 AND status='error' AND error_message<>''
GROUP BY error_message ORDER BY hits DESC LIMIT 5`, appID, functionID, since)
	if err != nil {
		return nil, err
	}
	defer errorRows.Close()
	for errorRows.Next() {
		var row functiondomain.StatsError
		if err := errorRows.Scan(&row.Message, &row.Count); err != nil {
			return nil, err
		}
		stats.TopErrors = append(stats.TopErrors, row)
	}
	if err := errorRows.Err(); err != nil {
		return nil, err
	}

	// 用 generate_series 补齐空桶：缺桶的折线会把「这一小时没有调用」
	// 画成一条直接连过去的斜线，看图的人读不出中间停过。
	bucketRows, err := r.pool.Query(ctx, `
WITH slots AS (
  SELECT generate_series(date_trunc('hour', $3::timestamptz), date_trunc('hour', NOW()), INTERVAL '1 hour') AS at
)
SELECT slots.at,
  COUNT(i.id) FILTER (WHERE i.status='success'),
  COUNT(i.id) FILTER (WHERE i.status='error')
FROM slots
LEFT JOIN app_function_invocations i
  ON i.appid=$1 AND i.function_id=$2 AND date_trunc('hour', i.created_at)=slots.at
GROUP BY slots.at ORDER BY slots.at`, appID, functionID, since)
	if err != nil {
		return nil, err
	}
	defer bucketRows.Close()
	for bucketRows.Next() {
		var row functiondomain.StatsBucket
		if err := bucketRows.Scan(&row.At, &row.Success, &row.Failed); err != nil {
			return nil, err
		}
		stats.Buckets = append(stats.Buckets, row)
	}
	return &stats, bucketRows.Err()
}

func (r *Repository) CreateAppFunctionKey(ctx context.Context, item functiondomain.Key) (*functiondomain.Key, error) {
	return scanAppFunctionKey(r.pool.QueryRow(ctx, `INSERT INTO app_function_keys
(appid, name, key_prefix, key_hash, created_by) VALUES ($1,$2,$3,$4,$5)
RETURNING id, appid, name, key_prefix, key_hash, status, created_by, last_used_at, created_at, revoked_at`,
		item.AppID, item.Name, item.KeyPrefix, item.KeyHash, item.CreatedBy))
}

func (r *Repository) ListAppFunctionKeys(ctx context.Context, appID int64) ([]functiondomain.Key, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, name, key_prefix, key_hash, status, created_by,
last_used_at, created_at, revoked_at FROM app_function_keys WHERE appid=$1 ORDER BY created_at DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.Key, 0, 4)
	for rows.Next() {
		item, err := scanAppFunctionKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetActiveAppFunctionKeyByHash(ctx context.Context, appID int64, keyHash []byte) (*functiondomain.Key, error) {
	return scanAppFunctionKey(r.pool.QueryRow(ctx, `SELECT id, appid, name, key_prefix, key_hash, status,
created_by, last_used_at, created_at, revoked_at FROM app_function_keys
WHERE appid=$1 AND key_hash=$2 AND status='active'`, appID, keyHash))
}

func (r *Repository) TouchAppFunctionKey(ctx context.Context, appID, keyID int64) {
	_, _ = r.pool.Exec(ctx, `UPDATE app_function_keys SET last_used_at=NOW() WHERE appid=$1 AND id=$2`, appID, keyID)
}

func (r *Repository) RevokeAppFunctionKey(ctx context.Context, appID, keyID int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_function_keys SET status='revoked', revoked_at=NOW()
WHERE appid=$1 AND id=$2 AND status='active'`, appID, keyID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const invocationSelect = `SELECT id, event_id::text, appid, function_id, version_id, caller_type,
caller_id, status, duration_ms, request_sha256, response_sha256, error_message, result, created_at
FROM app_function_invocations`

func scanAppFunction(row interface{ Scan(...any) error }) (*functiondomain.Function, error) {
	var item functiondomain.Function
	var capabilities, config []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Runtime, &item.Status,
		&item.ActiveVersion, &capabilities, &item.TimeoutMs, &item.MaxRequestBytes, &item.MaxResponseBytes,
		&item.MaxConcurrency, &item.RateLimitPerMin, &config,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(capabilities, &item.Capabilities)
	if len(config) > 0 {
		item.Config = json.RawMessage(config)
	} else {
		item.Config = json.RawMessage(`{}`)
	}
	return &item, nil
}

func scanAppFunctionVersion(row interface{ Scan(...any) error }) (*functiondomain.Version, error) {
	var item functiondomain.Version
	if err := row.Scan(&item.ID, &item.FunctionID, &item.AppID, &item.Version, &item.EndpointURL,
		&item.ResponsePublicKey, &item.WASMModule, &item.Source, &item.Notes, &item.ArtifactSHA256,
		&item.Status, &item.CreatedBy, &item.CreatedAt, &item.ActivatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.SourceBytes = len(item.Source)
	return &item, nil
}

func scanAppFunctionInvocation(row interface{ Scan(...any) error }) (*functiondomain.Invocation, error) {
	var item functiondomain.Invocation
	var raw []byte
	if err := row.Scan(&item.ID, &item.EventID, &item.AppID, &item.FunctionID, &item.VersionID,
		&item.CallerType, &item.CallerID, &item.Status, &item.DurationMs, &item.RequestSHA256,
		&item.ResponseSHA256, &item.ErrorMessage, &raw, &item.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(raw) > 0 {
		item.Result = &functiondomain.InvocationResult{}
		_ = json.Unmarshal(raw, item.Result)
	}
	return &item, nil
}

func scanAppFunctionKey(row interface{ Scan(...any) error }) (*functiondomain.Key, error) {
	var item functiondomain.Key
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.KeyPrefix, &item.KeyHash, &item.Status,
		&item.CreatedBy, &item.LastUsedAt, &item.CreatedAt, &item.RevokedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
