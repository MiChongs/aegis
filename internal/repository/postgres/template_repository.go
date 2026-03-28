package postgres

import (
	"context"
	"encoding/json"
	"net/http"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListMessageTemplates(ctx context.Context) ([]systemdomain.MessageTemplate, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, code, name, description, channel, subject, body_html, body_text, variables, is_builtin, enabled, created_by, created_at, updated_at FROM message_templates ORDER BY is_builtin DESC, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.MessageTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *t)
	}
	return items, rows.Err()
}

func (r *Repository) GetMessageTemplate(ctx context.Context, code string) (*systemdomain.MessageTemplate, error) {
	row := r.pool.QueryRow(ctx, `SELECT id, code, name, description, channel, subject, body_html, body_text, variables, is_builtin, enabled, created_by, created_at, updated_at FROM message_templates WHERE code = $1`, code)
	return scanTemplate(row)
}

func (r *Repository) CreateMessageTemplate(ctx context.Context, input systemdomain.CreateTemplateInput, createdBy int64) (*systemdomain.MessageTemplate, error) {
	varsJSON, _ := json.Marshal(input.Variables)
	row := r.pool.QueryRow(ctx, `INSERT INTO message_templates (code, name, description, channel, subject, body_html, body_text, variables, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, code, name, description, channel, subject, body_html, body_text, variables, is_builtin, enabled, created_by, created_at, updated_at`,
		input.Code, input.Name, input.Description, input.Channel, input.Subject, input.BodyHTML, input.BodyText, varsJSON, createdBy)
	t, err := scanTemplate(row)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40980, http.StatusConflict, "模板代码已存在")
		}
		return nil, err
	}
	return t, nil
}

func (r *Repository) UpdateMessageTemplate(ctx context.Context, code string, input systemdomain.UpdateTemplateInput) (*systemdomain.MessageTemplate, error) {
	var varsJSON []byte
	if input.Variables != nil {
		varsJSON, _ = json.Marshal(*input.Variables)
	}
	row := r.pool.QueryRow(ctx, `UPDATE message_templates SET
		name = COALESCE($2, name), description = COALESCE($3, description),
		subject = COALESCE($4, subject), body_html = COALESCE($5, body_html),
		body_text = COALESCE($6, body_text),
		variables = COALESCE($7, variables),
		enabled = COALESCE($8, enabled), updated_at = NOW()
		WHERE code = $1
		RETURNING id, code, name, description, channel, subject, body_html, body_text, variables, is_builtin, enabled, created_by, created_at, updated_at`,
		code, input.Name, input.Description, input.Subject, input.BodyHTML, input.BodyText, varsJSON, input.Enabled)
	t, err := scanTemplate(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40481, http.StatusNotFound, "模板不存在")
		}
		return nil, err
	}
	return t, nil
}

func (r *Repository) DeleteMessageTemplate(ctx context.Context, code string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM message_templates WHERE code = $1 AND is_builtin = FALSE`, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40481, http.StatusBadRequest, "模板不存在或为内置模板不可删除")
	}
	return nil
}

type templateScanner interface {
	Scan(dest ...any) error
}

func scanTemplate(row templateScanner) (*systemdomain.MessageTemplate, error) {
	var t systemdomain.MessageTemplate
	var varsRaw []byte
	if err := row.Scan(&t.ID, &t.Code, &t.Name, &t.Description, &t.Channel, &t.Subject, &t.BodyHTML, &t.BodyText, &varsRaw, &t.IsBuiltin, &t.Enabled, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(varsRaw, &t.Variables)
	return &t, nil
}
