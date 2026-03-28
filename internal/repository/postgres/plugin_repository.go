package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

const pluginColumns = `id, name, display_name, description, type, status, version, author, hooks, config, expr_script, wasm_module_url, wasm_hash, priority, error_message, created_by, created_at, updated_at`

func scanPlugin(row pgx.Row) (*plugindomain.Plugin, error) {
	var p plugindomain.Plugin
	var hooksRaw, configRaw []byte
	if err := row.Scan(
		&p.ID, &p.Name, &p.DisplayName, &p.Description, &p.Type, &p.Status,
		&p.Version, &p.Author, &hooksRaw, &configRaw, &p.ExprScript,
		&p.WASMModuleURL, &p.WASMHash, &p.Priority, &p.ErrorMessage,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(hooksRaw, &p.Hooks)
	_ = json.Unmarshal(configRaw, &p.Config)
	if p.Hooks == nil {
		p.Hooks = []plugindomain.HookBinding{}
	}
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	return &p, nil
}

func (r *Repository) CreatePlugin(ctx context.Context, input plugindomain.CreatePluginInput, createdBy *int64) (*plugindomain.Plugin, error) {
	hooksJSON, _ := json.Marshal(input.Hooks)
	configJSON, _ := json.Marshal(input.Config)
	if configJSON == nil {
		configJSON = []byte("{}")
	}
	query := `INSERT INTO plugins (name, display_name, description, type, hooks, config, expr_script, priority, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
RETURNING ` + pluginColumns
	p, err := scanPlugin(r.pool.QueryRow(ctx, query,
		strings.TrimSpace(input.Name), strings.TrimSpace(input.DisplayName),
		input.Description, input.Type, hooksJSON, configJSON,
		input.ExprScript, input.Priority, createdBy,
	))
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40950, http.StatusConflict, "插件名称已存在")
		}
		return nil, err
	}
	return p, nil
}

func (r *Repository) GetPlugin(ctx context.Context, id int64) (*plugindomain.Plugin, error) {
	return scanPlugin(r.pool.QueryRow(ctx, `SELECT `+pluginColumns+` FROM plugins WHERE id = $1`, id))
}

func (r *Repository) GetPluginByName(ctx context.Context, name string) (*plugindomain.Plugin, error) {
	return scanPlugin(r.pool.QueryRow(ctx, `SELECT `+pluginColumns+` FROM plugins WHERE name = $1`, name))
}

func (r *Repository) UpdatePlugin(ctx context.Context, id int64, input plugindomain.UpdatePluginInput) (*plugindomain.Plugin, error) {
	sets := []string{"updated_at = NOW()"}
	args := []any{id}
	idx := 2

	if input.DisplayName != nil {
		sets = append(sets, fmt.Sprintf("display_name = $%d", idx))
		args = append(args, strings.TrimSpace(*input.DisplayName))
		idx++
	}
	if input.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", idx))
		args = append(args, *input.Description)
		idx++
	}
	if input.Hooks != nil {
		hooksJSON, _ := json.Marshal(input.Hooks)
		sets = append(sets, fmt.Sprintf("hooks = $%d", idx))
		args = append(args, hooksJSON)
		idx++
	}
	if input.Config != nil {
		configJSON, _ := json.Marshal(input.Config)
		sets = append(sets, fmt.Sprintf("config = $%d", idx))
		args = append(args, configJSON)
		idx++
	}
	if input.ExprScript != nil {
		sets = append(sets, fmt.Sprintf("expr_script = $%d", idx))
		args = append(args, *input.ExprScript)
		idx++
	}
	if input.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", idx))
		args = append(args, *input.Priority)
		idx++
	}

	query := fmt.Sprintf("UPDATE plugins SET %s WHERE id = $1 RETURNING %s", strings.Join(sets, ", "), pluginColumns)
	return scanPlugin(r.pool.QueryRow(ctx, query, args...))
}

func (r *Repository) UpdatePluginStatus(ctx context.Context, id int64, status, errorMessage string) error {
	_, err := r.pool.Exec(ctx, `UPDATE plugins SET status = $2, error_message = $3, updated_at = NOW() WHERE id = $1`, id, status, errorMessage)
	return err
}

func (r *Repository) DeletePlugin(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM plugins WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40480, http.StatusNotFound, "插件不存在")
	}
	return nil
}

func (r *Repository) ListPlugins(ctx context.Context, q plugindomain.PluginListQuery) (*plugindomain.PluginListResult, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if q.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}
	if q.Type != "" {
		where = append(where, fmt.Sprintf("type = $%d", idx))
		args = append(args, q.Type)
		idx++
	}
	if q.Keyword != "" {
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR display_name ILIKE $%d)", idx, idx))
		args = append(args, "%"+q.Keyword+"%")
		idx++
	}
	whereStr := strings.Join(where, " AND ")

	var total int64
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM plugins WHERE "+whereStr, countArgs...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := q.Page, q.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	args = append(args, limit, offset)

	query := fmt.Sprintf("SELECT %s FROM plugins WHERE %s ORDER BY priority ASC, id ASC LIMIT $%d OFFSET $%d", pluginColumns, whereStr, idx, idx+1)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []plugindomain.Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *p)
	}
	if items == nil {
		items = []plugindomain.Plugin{}
	}

	return &plugindomain.PluginListResult{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (r *Repository) ListEnabledPlugins(ctx context.Context) ([]plugindomain.Plugin, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+pluginColumns+` FROM plugins WHERE status = 'enabled' ORDER BY priority ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []plugindomain.Plugin
	for rows.Next() {
		p, err := scanPlugin(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *p)
	}
	return items, rows.Err()
}

func (r *Repository) InsertHookExecution(ctx context.Context, exec plugindomain.HookExecution) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO plugin_hook_executions (plugin_id, plugin_name, hook_name, phase, duration_ns, status, error, input, output, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		exec.PluginID, exec.PluginName, exec.HookName, exec.Phase,
		int64(exec.DurationMs*1e6), exec.Status, exec.Error,
		jsonOrNull(exec.Input), jsonOrNull(exec.Output), exec.CreatedAt,
	)
	return err
}

func (r *Repository) ListHookExecutions(ctx context.Context, q plugindomain.HookExecutionListQuery) (*plugindomain.HookExecutionListResult, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if q.PluginID > 0 {
		where = append(where, fmt.Sprintf("plugin_id = $%d", idx))
		args = append(args, q.PluginID)
		idx++
	}
	if q.HookName != "" {
		where = append(where, fmt.Sprintf("hook_name = $%d", idx))
		args = append(args, q.HookName)
		idx++
	}
	if q.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}
	whereStr := strings.Join(where, " AND ")

	var total int64
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM plugin_hook_executions WHERE "+whereStr, countArgs...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := q.Page, q.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	args = append(args, limit, offset)

	query := fmt.Sprintf(`SELECT id, plugin_id, plugin_name, hook_name, phase, duration_ns, status, error, input, output, created_at
FROM plugin_hook_executions WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, whereStr, idx, idx+1)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []plugindomain.HookExecution
	for rows.Next() {
		var e plugindomain.HookExecution
		var durationNs int64
		var inputRaw, outputRaw []byte
		if err := rows.Scan(&e.ID, &e.PluginID, &e.PluginName, &e.HookName, &e.Phase, &durationNs, &e.Status, &e.Error, &inputRaw, &outputRaw, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.DurationMs = float64(durationNs) / 1e6
		_ = json.Unmarshal(inputRaw, &e.Input)
		_ = json.Unmarshal(outputRaw, &e.Output)
		items = append(items, e)
	}
	if items == nil {
		items = []plugindomain.HookExecution{}
	}
	return &plugindomain.HookExecutionListResult{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

func (r *Repository) CleanOldHookExecutions(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM plugin_hook_executions WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func jsonOrNull(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
