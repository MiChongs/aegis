package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	workflowdomain "aegis/internal/domain/workflow"
)

func (r *Repository) ListWorkflows(ctx context.Context, appID int64, query workflowdomain.ListQuery) ([]workflowdomain.Workflow, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	base, args := buildWorkflowFilter(appID, query.Status, query.Category, query.Keyword)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := `SELECT id, appid, name, COALESCE(description, ''), COALESCE(category, ''), status, version, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(permissions, '{}'::jsonb), created_by, updated_by, created_at, updated_at` + base + fmt.Sprintf(` ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]workflowdomain.Workflow, 0, limit)
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) GetWorkflowByID(ctx context.Context, appID int64, workflowID int64) (*workflowdomain.Workflow, error) {
	return scanWorkflow(r.pool.QueryRow(ctx, `SELECT id, appid, name, COALESCE(description, ''), COALESCE(category, ''), status, version, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(permissions, '{}'::jsonb), created_by, updated_by, created_at, updated_at FROM workflows WHERE appid = $1 AND id = $2 LIMIT 1`, appID, workflowID))
}

func (r *Repository) ListSchedulableWorkflows(ctx context.Context) ([]workflowdomain.Workflow, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, appid, name, COALESCE(description, ''), COALESCE(category, ''), status, version, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(permissions, '{}'::jsonb), created_by, updated_by, created_at, updated_at FROM workflows WHERE status = 'active' ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workflowdomain.Workflow, 0, 16)
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertWorkflow(ctx context.Context, item workflowdomain.Workflow) (*workflowdomain.Workflow, error) {
	definition, _ := json.Marshal(item.Definition)
	trigger, _ := json.Marshal(item.TriggerConfig)
	ui, _ := json.Marshal(item.UIConfig)
	permissions, _ := json.Marshal(item.Permissions)
	query := `INSERT INTO workflows (id, appid, name, description, category, status, version, definition, trigger_config, ui_config, permissions, created_by, updated_by, created_at, updated_at)
VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, CASE WHEN $1 = 0 THEN 1 ELSE COALESCE((SELECT version + 1 FROM workflows WHERE id = $1), 1) END, $7, $8, $9, $10, $11, $12, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	description = EXCLUDED.description,
	category = EXCLUDED.category,
	status = EXCLUDED.status,
	version = workflows.version + 1,
	definition = EXCLUDED.definition,
	trigger_config = EXCLUDED.trigger_config,
	ui_config = EXCLUDED.ui_config,
	permissions = EXCLUDED.permissions,
	updated_by = EXCLUDED.updated_by,
	updated_at = NOW()
RETURNING id, appid, name, COALESCE(description, ''), COALESCE(category, ''), status, version, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(permissions, '{}'::jsonb), created_by, updated_by, created_at, updated_at`
	return scanWorkflow(r.pool.QueryRow(ctx, query, item.ID, item.AppID, item.Name, nullableString(item.Description), nullableString(item.Category), item.Status, definition, trigger, ui, permissions, item.CreatedBy, item.UpdatedBy))
}

func (r *Repository) DeleteWorkflow(ctx context.Context, appID int64, workflowID int64) (bool, error) {
	result, err := r.pool.Exec(ctx, `DELETE FROM workflows WHERE appid = $1 AND id = $2`, appID, workflowID)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

func (r *Repository) CreateWorkflowInstance(ctx context.Context, item workflowdomain.Instance) (*workflowdomain.Instance, error) {
	input, _ := json.Marshal(item.InputData)
	output, _ := json.Marshal(item.OutputData)
	query := `INSERT INTO workflow_instances (workflow_id, appid, instance_name, status, priority, started_by, current_node_id, input_data, output_data, error_message, started_at, ended_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
RETURNING id, workflow_id, appid, COALESCE(instance_name, ''), status, priority, started_by, COALESCE(current_node_id, ''), COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(error_message, ''), started_at, ended_at, created_at, updated_at`
	return scanWorkflowInstance(r.pool.QueryRow(ctx, query, item.WorkflowID, item.AppID, nullableString(item.InstanceName), item.Status, item.Priority, item.StartedBy, nullableString(item.CurrentNodeID), input, output, nullableString(item.ErrorMessage), item.StartedAt, item.EndedAt))
}

func (r *Repository) UpdateWorkflowInstance(ctx context.Context, item workflowdomain.Instance) error {
	input, _ := json.Marshal(item.InputData)
	output, _ := json.Marshal(item.OutputData)
	_, err := r.pool.Exec(ctx, `UPDATE workflow_instances SET status = $2, priority = $3, current_node_id = $4, input_data = $5, output_data = $6, error_message = $7, started_at = $8, ended_at = $9, updated_at = NOW() WHERE id = $1`, item.ID, item.Status, item.Priority, nullableString(item.CurrentNodeID), input, output, nullableString(item.ErrorMessage), item.StartedAt, item.EndedAt)
	return err
}

func (r *Repository) GetWorkflowInstance(ctx context.Context, appID int64, instanceID int64) (*workflowdomain.Instance, error) {
	return scanWorkflowInstance(r.pool.QueryRow(ctx, `SELECT id, workflow_id, appid, COALESCE(instance_name, ''), status, priority, started_by, COALESCE(current_node_id, ''), COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(error_message, ''), started_at, ended_at, created_at, updated_at FROM workflow_instances WHERE appid = $1 AND id = $2 LIMIT 1`, appID, instanceID))
}

func (r *Repository) ListWorkflowInstances(ctx context.Context, appID int64, query workflowdomain.InstanceQuery) ([]workflowdomain.Instance, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	args := []any{appID}
	base := ` FROM workflow_instances WHERE appid = $1`
	if query.WorkflowID > 0 {
		base += fmt.Sprintf(" AND workflow_id = $%d", len(args)+1)
		args = append(args, query.WorkflowID)
	}
	if strings.TrimSpace(query.Status) != "" {
		base += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(query.Status))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := `SELECT id, workflow_id, appid, COALESCE(instance_name, ''), status, priority, started_by, COALESCE(current_node_id, ''), COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(error_message, ''), started_at, ended_at, created_at, updated_at` + base + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]workflowdomain.Instance, 0, limit)
	for rows.Next() {
		item, err := scanWorkflowInstance(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) CreateWorkflowTask(ctx context.Context, item workflowdomain.Task) (*workflowdomain.Task, error) {
	input, _ := json.Marshal(item.InputData)
	output, _ := json.Marshal(item.OutputData)
	formSchema, _ := json.Marshal(item.FormSchema)
	query := `INSERT INTO workflow_tasks (workflow_id, instance_id, appid, node_id, name, type, status, priority, assigned_to, input_data, output_data, form_schema, comment, due_at, completed_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW())
RETURNING id, workflow_id, instance_id, appid, node_id, name, type, status, priority, assigned_to, COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(form_schema, '{}'::jsonb), COALESCE(comment, ''), due_at, completed_at, created_at, updated_at`
	return scanWorkflowTask(r.pool.QueryRow(ctx, query, item.WorkflowID, item.InstanceID, item.AppID, item.NodeID, item.Name, item.Type, item.Status, item.Priority, item.AssignedTo, input, output, formSchema, nullableString(item.Comment), item.DueAt, item.CompletedAt))
}

func (r *Repository) UpdateWorkflowTask(ctx context.Context, item workflowdomain.Task) error {
	output, _ := json.Marshal(item.OutputData)
	_, err := r.pool.Exec(ctx, `UPDATE workflow_tasks SET status = $2, assigned_to = $3, output_data = $4, comment = $5, completed_at = $6, updated_at = NOW() WHERE id = $1`, item.ID, item.Status, item.AssignedTo, output, nullableString(item.Comment), item.CompletedAt)
	return err
}

func (r *Repository) GetWorkflowTask(ctx context.Context, appID int64, taskID int64) (*workflowdomain.Task, error) {
	return scanWorkflowTask(r.pool.QueryRow(ctx, `SELECT id, workflow_id, instance_id, appid, node_id, name, type, status, priority, assigned_to, COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(form_schema, '{}'::jsonb), COALESCE(comment, ''), due_at, completed_at, created_at, updated_at FROM workflow_tasks WHERE appid = $1 AND id = $2 LIMIT 1`, appID, taskID))
}

func (r *Repository) ListWorkflowTasks(ctx context.Context, appID int64, query workflowdomain.TaskQuery) ([]workflowdomain.Task, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	args := []any{appID}
	base := ` FROM workflow_tasks WHERE appid = $1`
	if query.UserID > 0 {
		base += fmt.Sprintf(" AND assigned_to = $%d", len(args)+1)
		args = append(args, query.UserID)
	}
	if strings.TrimSpace(query.Status) != "" {
		base += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, strings.TrimSpace(query.Status))
	}
	if query.Priority > 0 {
		base += fmt.Sprintf(" AND priority = $%d", len(args)+1)
		args = append(args, query.Priority)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := `SELECT id, workflow_id, instance_id, appid, node_id, name, type, status, priority, assigned_to, COALESCE(input_data, '{}'::jsonb), COALESCE(output_data, '{}'::jsonb), COALESCE(form_schema, '{}'::jsonb), COALESCE(comment, ''), due_at, completed_at, created_at, updated_at` + base + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]workflowdomain.Task, 0, limit)
	for rows.Next() {
		item, err := scanWorkflowTask(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) CreateWorkflowLog(ctx context.Context, item workflowdomain.LogEntry) error {
	meta, _ := json.Marshal(item.Metadata)
	_, err := r.pool.Exec(ctx, `INSERT INTO workflow_logs (appid, workflow_id, instance_id, task_id, level, event, message, metadata, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())`, item.AppID, item.WorkflowID, item.InstanceID, item.TaskID, item.Level, item.Event, item.Message, meta)
	return err
}

func (r *Repository) ListWorkflowLogs(ctx context.Context, appID int64, workflowID int64, instanceID int64, taskID int64, limit int) ([]workflowdomain.LogEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	args := []any{appID}
	base := ` FROM workflow_logs WHERE appid = $1`
	if workflowID > 0 {
		base += fmt.Sprintf(" AND workflow_id = $%d", len(args)+1)
		args = append(args, workflowID)
	}
	if instanceID > 0 {
		base += fmt.Sprintf(" AND instance_id = $%d", len(args)+1)
		args = append(args, instanceID)
	}
	if taskID > 0 {
		base += fmt.Sprintf(" AND task_id = $%d", len(args)+1)
		args = append(args, taskID)
	}
	sql := `SELECT id, appid, workflow_id, instance_id, task_id, level, event, message, COALESCE(metadata, '{}'::jsonb), created_at` + base + fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]workflowdomain.LogEntry, 0, limit)
	for rows.Next() {
		item, err := scanWorkflowLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) ListWorkflowTemplates(ctx context.Context, appID int64, category string, page int, limit int) ([]workflowdomain.Template, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit
	args := []any{appID}
	base := ` FROM workflow_templates WHERE appid = $1`
	if category = strings.TrimSpace(category); category != "" {
		base += fmt.Sprintf(" AND category = $%d", len(args)+1)
		args = append(args, category)
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	sql := `SELECT id, appid, name, COALESCE(description, ''), COALESCE(category, ''), is_public, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(metadata, '{}'::jsonb), created_at, updated_at` + base + fmt.Sprintf(` ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)+1, len(args)+2)
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]workflowdomain.Template, 0, limit)
	for rows.Next() {
		item, err := scanWorkflowTemplate(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

func (r *Repository) GetWorkflowTemplate(ctx context.Context, appID int64, templateID int64) (*workflowdomain.Template, error) {
	return scanWorkflowTemplate(r.pool.QueryRow(ctx, `SELECT id, appid, name, COALESCE(description, ''), COALESCE(category, ''), is_public, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(metadata, '{}'::jsonb), created_at, updated_at FROM workflow_templates WHERE appid = $1 AND id = $2 LIMIT 1`, appID, templateID))
}

func (r *Repository) UpsertWorkflowTemplate(ctx context.Context, item workflowdomain.Template) (*workflowdomain.Template, error) {
	definition, _ := json.Marshal(item.Definition)
	trigger, _ := json.Marshal(item.TriggerConfig)
	ui, _ := json.Marshal(item.UIConfig)
	meta, _ := json.Marshal(item.Metadata)
	query := `INSERT INTO workflow_templates (id, appid, name, description, category, is_public, definition, trigger_config, ui_config, metadata, created_at, updated_at)
VALUES (NULLIF($1, 0), $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	description = EXCLUDED.description,
	category = EXCLUDED.category,
	is_public = EXCLUDED.is_public,
	definition = EXCLUDED.definition,
	trigger_config = EXCLUDED.trigger_config,
	ui_config = EXCLUDED.ui_config,
	metadata = EXCLUDED.metadata,
	updated_at = NOW()
RETURNING id, appid, name, COALESCE(description, ''), COALESCE(category, ''), is_public, COALESCE(definition, '{}'::jsonb), COALESCE(trigger_config, '{}'::jsonb), COALESCE(ui_config, '{}'::jsonb), COALESCE(metadata, '{}'::jsonb), created_at, updated_at`
	return scanWorkflowTemplate(r.pool.QueryRow(ctx, query, item.ID, item.AppID, item.Name, nullableString(item.Description), nullableString(item.Category), item.IsPublic, definition, trigger, ui, meta))
}

func (r *Repository) GetWorkflowStatistics(ctx context.Context, appID int64) (*workflowdomain.Statistics, error) {
	stats := &workflowdomain.Statistics{AppID: appID, StatusCounts: map[string]int64{}}
	query := `SELECT
	(SELECT COUNT(*) FROM workflows WHERE appid = $1) AS total_workflows,
	(SELECT COUNT(*) FROM workflows WHERE appid = $1 AND status = 'active') AS active_workflows,
	(SELECT COUNT(*) FROM workflow_instances WHERE appid = $1) AS total_instances,
	(SELECT COUNT(*) FROM workflow_instances WHERE appid = $1 AND status = 'running') AS running_instances,
	(SELECT COUNT(*) FROM workflow_instances WHERE appid = $1 AND status = 'completed') AS completed_instances,
	(SELECT COUNT(*) FROM workflow_instances WHERE appid = $1 AND status = 'failed') AS failed_instances,
	(SELECT COUNT(*) FROM workflow_tasks WHERE appid = $1 AND status IN ('pending', 'running')) AS pending_tasks`
	if err := r.pool.QueryRow(ctx, query, appID).Scan(&stats.TotalWorkflows, &stats.ActiveWorkflows, &stats.TotalInstances, &stats.RunningInstances, &stats.CompletedInstances, &stats.FailedInstances, &stats.PendingTasks); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT status, COUNT(*) FROM workflow_instances WHERE appid = $1 GROUP BY status`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		stats.StatusCounts[status] = count
	}
	return stats, rows.Err()
}

func buildWorkflowFilter(appID int64, status string, category string, keyword string) (string, []any) {
	args := []any{appID}
	base := ` FROM workflows WHERE appid = $1`
	if status = strings.TrimSpace(status); status != "" {
		base += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, status)
	}
	if category = strings.TrimSpace(category); category != "" {
		base += fmt.Sprintf(" AND category = $%d", len(args)+1)
		args = append(args, category)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		like := "%" + keyword + "%"
		base += fmt.Sprintf(" AND (name ILIKE $%d OR COALESCE(description, '') ILIKE $%d)", len(args)+1, len(args)+1)
		args = append(args, like)
	}
	return base, args
}
