package postgres

import (
	"context"
	"encoding/json"

	functiondomain "aegis/internal/domain/appfunction"
)

func (r *Repository) CreateAppFunction(ctx context.Context, input functiondomain.CreateFunctionInput) (*functiondomain.Function, error) {
	capabilities, _ := json.Marshal(input.Capabilities)
	return scanAppFunction(r.pool.QueryRow(ctx, `INSERT INTO app_functions
(appid, name, description, runtime, capabilities, timeout_ms, max_request_bytes, max_response_bytes, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, appid, name, description, runtime, status, active_version, capabilities,
timeout_ms, max_request_bytes, max_response_bytes, created_by, created_at, updated_at`,
		input.AppID, input.Name, input.Description, input.Runtime, capabilities, input.TimeoutMs,
		input.MaxRequestBytes, input.MaxResponseBytes, input.CreatedBy))
}

func (r *Repository) ListAppFunctions(ctx context.Context, appID int64) ([]functiondomain.Function, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, name, description, runtime, status, active_version,
capabilities, timeout_ms, max_request_bytes, max_response_bytes, created_by, created_at, updated_at
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
	return scanAppFunction(r.pool.QueryRow(ctx, `SELECT id, appid, name, description, runtime, status, active_version,
capabilities, timeout_ms, max_request_bytes, max_response_bytes, created_by, created_at, updated_at
FROM app_functions WHERE appid=$1 AND name=$2`, appID, name))
}

func (r *Repository) UpdateAppFunction(ctx context.Context, appID int64, name string, input functiondomain.UpdateFunctionInput) (*functiondomain.Function, error) {
	var capabilities any
	if input.Capabilities != nil {
		capabilities, _ = json.Marshal(input.Capabilities)
	}
	return scanAppFunction(r.pool.QueryRow(ctx, `UPDATE app_functions SET
description=COALESCE($3,description), status=COALESCE($4,status),
capabilities=CASE WHEN $5::jsonb IS NULL THEN capabilities ELSE $5::jsonb END,
timeout_ms=COALESCE($6,timeout_ms), max_request_bytes=COALESCE($7,max_request_bytes),
max_response_bytes=COALESCE($8,max_response_bytes), updated_at=NOW()
WHERE appid=$1 AND name=$2
RETURNING id, appid, name, description, runtime, status, active_version, capabilities,
timeout_ms, max_request_bytes, max_response_bytes, created_by, created_at, updated_at`,
		appID, name, input.Description, input.Status, capabilities, input.TimeoutMs,
		input.MaxRequestBytes, input.MaxResponseBytes))
}

func (r *Repository) DeleteAppFunction(ctx context.Context, appID int64, name string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_functions WHERE appid=$1 AND name=$2`, appID, name)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) CreateAppFunctionVersion(ctx context.Context, input functiondomain.CreateVersionInput) (*functiondomain.Version, error) {
	return scanAppFunctionVersion(r.pool.QueryRow(ctx, `INSERT INTO app_function_versions
(function_id, appid, version, endpoint_url, response_public_key, wasm_module, source, artifact_sha256, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING id, function_id, appid, version, endpoint_url, response_public_key, wasm_module, source,
artifact_sha256, status, created_by, created_at, activated_at`,
		input.FunctionID, input.AppID, input.Version, input.EndpointURL, input.ResponsePublicKey,
		nullableBytes(input.WASMModule), input.Source, input.ArtifactSHA256, input.CreatedBy))
}

func (r *Repository) ListAppFunctionVersions(ctx context.Context, appID, functionID int64) ([]functiondomain.Version, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, function_id, appid, version, endpoint_url, response_public_key,
wasm_module, source, artifact_sha256, status, created_by, created_at, activated_at
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
	return scanAppFunctionVersion(r.pool.QueryRow(ctx, `SELECT id, function_id, appid, version, endpoint_url,
response_public_key, wasm_module, source, artifact_sha256, status, created_by, created_at, activated_at
FROM app_function_versions WHERE appid=$1 AND function_id=$2 AND version=$3`,
		appID, functionID, version))
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

func (r *Repository) ListAppFunctionInvocations(ctx context.Context, appID, functionID int64, limit int) ([]functiondomain.Invocation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, invocationSelect+` WHERE appid=$1 AND function_id=$2 ORDER BY created_at DESC LIMIT $3`,
		appID, functionID, limit)
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
	return items, rows.Err()
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
	var capabilities []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Description, &item.Runtime, &item.Status,
		&item.ActiveVersion, &capabilities, &item.TimeoutMs, &item.MaxRequestBytes, &item.MaxResponseBytes,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	_ = json.Unmarshal(capabilities, &item.Capabilities)
	return &item, nil
}

func scanAppFunctionVersion(row interface{ Scan(...any) error }) (*functiondomain.Version, error) {
	var item functiondomain.Version
	if err := row.Scan(&item.ID, &item.FunctionID, &item.AppID, &item.Version, &item.EndpointURL,
		&item.ResponsePublicKey, &item.WASMModule, &item.Source, &item.ArtifactSHA256, &item.Status, &item.CreatedBy,
		&item.CreatedAt, &item.ActivatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
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
