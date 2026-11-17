package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// 支付凭证导出的存放（migrations/postgres/000156_cluster_replicas.up.sql）。
//
// 原先凭证 PDF 落在本地目录，多节点部署时「在 A 节点导出、负载均衡把下载打到 B 节点」就下不到。
// 放进数据库后任一节点都能取回同一份文件。

// StoredPaymentReceipt 一份导出的凭证。
type StoredPaymentReceipt struct {
	BillID        string
	AppID         int64
	UserID        int64
	OrderNo       string
	TransactionNo string
	FileName      string
	Locale        string
	Content       []byte
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// SavePaymentReceiptExport 保存一份导出。bill_id 是 128 位随机数，撞号按失败处理而不是覆盖。
func (r *Repository) SavePaymentReceiptExport(ctx context.Context, rec StoredPaymentReceipt) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO payment_receipt_exports
(bill_id, appid, user_id, order_no, transaction_no, file_name, locale, content, created_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		rec.BillID, rec.AppID, rec.UserID, rec.OrderNo, rec.TransactionNo, rec.FileName, rec.Locale, rec.Content, rec.CreatedAt, rec.ExpiresAt)
	return err
}

// GetPaymentReceiptExport 按 (应用, 凭证标识) 取回一份导出；不存在返回 nil。
// 应用也进查询条件：凭证标识来自 URL，不能让别的应用凭一个标识读到这边的凭证。
func (r *Repository) GetPaymentReceiptExport(ctx context.Context, appID int64, billID string) (*StoredPaymentReceipt, error) {
	var rec StoredPaymentReceipt
	err := r.pool.QueryRow(ctx, `SELECT bill_id, appid, user_id, order_no, transaction_no, file_name, locale, content, created_at, expires_at
FROM payment_receipt_exports WHERE bill_id = $1 AND appid = $2`, billID, appID).
		Scan(&rec.BillID, &rec.AppID, &rec.UserID, &rec.OrderNo, &rec.TransactionNo, &rec.FileName, &rec.Locale, &rec.Content, &rec.CreatedAt, &rec.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// DeletePaymentReceiptExport 删除一份导出（下载时发现已过期）。
func (r *Repository) DeletePaymentReceiptExport(ctx context.Context, appID int64, billID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM payment_receipt_exports WHERE bill_id = $1 AND appid = $2`, billID, appID)
	return err
}

// PurgeExpiredPaymentReceiptExports 删一批已过期的导出，返回删除条数。
// 分批删：积压很多时一条 DELETE 扫全表、长时间持锁不划算，调用方循环到删不满一批为止。
func (r *Repository) PurgeExpiredPaymentReceiptExports(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 500
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM payment_receipt_exports
WHERE bill_id IN (SELECT bill_id FROM payment_receipt_exports WHERE expires_at < $1 ORDER BY expires_at LIMIT $2)`, now, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
