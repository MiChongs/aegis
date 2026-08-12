package postgres

import (
	"context"
	"errors"
	"time"

	legaldomain "aegis/internal/domain/legal"
	"github.com/jackc/pgx/v5"
)

// 法律文本存取。
//
// 表按「文档类型 × 语言」唯一，因此这里没有分页也没有排序参数：
// 语言数是个位数，公开接口的查询形状永远是「取这个文档的全部语言，
// 然后在内存里协商」——按语言逐个试会多出「协商结果没有对应行」的往返。

const legalDocumentColumns = `id, doc_type, locale, title, summary, body, version,
	effective_at, published, updated_by, created_at, updated_at`

func scanLegalDocument(row pgx.Row) (*legaldomain.Document, error) {
	var (
		item        legaldomain.Document
		docType     string
		effectiveAt *time.Time
		updatedBy   *int64
		createdAt   time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(&item.ID, &docType, &item.Locale, &item.Title, &item.Summary, &item.Body,
		&item.Version, &effectiveAt, &item.Published, &updatedBy, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	item.DocType = legaldomain.DocType(docType)
	item.EffectiveAt = effectiveAt
	item.UpdatedBy = updatedBy
	item.CreatedAt = &createdAt
	item.UpdatedAt = &updatedAt
	item.Source = legaldomain.SourceCustom
	return &item, nil
}

// ListLegalDocuments 取某个文档类型的全部语言。onlyPublished 为真时只取已发布的。
func (r *Repository) ListLegalDocuments(ctx context.Context, docType legaldomain.DocType, onlyPublished bool) ([]legaldomain.Document, error) {
	query := `SELECT ` + legalDocumentColumns + ` FROM legal_documents WHERE doc_type = $1`
	if onlyPublished {
		query += ` AND published = TRUE`
	}
	query += ` ORDER BY locale`

	rows, err := r.pool.Query(ctx, query, string(docType))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]legaldomain.Document, 0, 4)
	for rows.Next() {
		item, err := scanLegalDocument(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListAllLegalDocuments 取全部文档的全部语言，供管理端一次拉齐。
func (r *Repository) ListAllLegalDocuments(ctx context.Context) ([]legaldomain.Document, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+legalDocumentColumns+`
		FROM legal_documents ORDER BY doc_type, locale`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]legaldomain.Document, 0, 8)
	for rows.Next() {
		item, err := scanLegalDocument(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// GetLegalDocument 取一份文本；不存在返回 (nil, nil)。
func (r *Repository) GetLegalDocument(ctx context.Context, docType legaldomain.DocType, locale string) (*legaldomain.Document, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+legalDocumentColumns+`
		FROM legal_documents WHERE doc_type = $1 AND locale = $2`, string(docType), locale)
	item, err := scanLegalDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// UpsertLegalDocument 写入一份文本。同一「文档 × 语言」只有一份现行文本，
// 因此是 upsert 而不是 insert —— 历史版本由审计日志承担，不堆在这张表里。
func (r *Repository) UpsertLegalDocument(ctx context.Context, item legaldomain.Document) (*legaldomain.Document, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO legal_documents (doc_type, locale, title, summary, body, version, effective_at, published, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (doc_type, locale) DO UPDATE SET
			title = EXCLUDED.title,
			summary = EXCLUDED.summary,
			body = EXCLUDED.body,
			version = EXCLUDED.version,
			effective_at = EXCLUDED.effective_at,
			published = EXCLUDED.published,
			updated_by = EXCLUDED.updated_by,
			updated_at = NOW()
		RETURNING `+legalDocumentColumns,
		string(item.DocType), item.Locale, item.Title, item.Summary, item.Body,
		item.Version, item.EffectiveAt, item.Published, item.UpdatedBy)
	return scanLegalDocument(row)
}

// DeleteLegalDocument 删除一份自定义文本，删除后该语言回落到内置默认版本。
// 返回是否真的删掉了一行 —— 删一份本来就不存在的文本应当是 404 而不是静默成功。
func (r *Repository) DeleteLegalDocument(ctx context.Context, docType legaldomain.DocType, locale string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM legal_documents WHERE doc_type = $1 AND locale = $2`,
		string(docType), locale)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
