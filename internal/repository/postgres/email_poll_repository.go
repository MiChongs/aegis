package postgres

import (
	"context"
	"time"
)

// PendingProviderDelivery 一封还没有终态、要去服务商那边对账的信。
//
// 给不推送回执、只能主动去查的服务商用（目前是 Cloud Mail）。
// ConfigAppID 是**通道**的作用域（平台级为 0），不是发信应用：借道平台共享通道的信，
// 回填时要按通道归属匹配，见 ApplyEmailDeliveryEventOutcome。
type PendingProviderDelivery struct {
	ID                int64
	ConfigID          int64
	ConfigAppID       int64
	ProviderMessageID string
	LastEvent         string
	CreatedAt         time.Time
}

// ListPendingProviderDeliveries 某个服务商在 since 之后发出、仍停在 pending / sent 的信，新的在前。
//
// 走 000171 的部分索引：只有未结的信在索引里，表再大这条查询也只扫一小段。
func (r *Repository) ListPendingProviderDeliveries(ctx context.Context, provider string, since time.Time, limit int) ([]PendingProviderDelivery, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.config_id, COALESCE(c.appid, 0), d.provider_message_id,
	COALESCE(d.last_event, ''), d.created_at
FROM email_deliveries d
JOIN app_email_configs c ON c.id = d.config_id
WHERE d.provider = $1
	AND d.status IN ('pending', 'sent')
	AND d.provider_message_id IS NOT NULL
	AND d.created_at >= $2
ORDER BY d.created_at DESC
LIMIT $3`, provider, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PendingProviderDelivery, 0, 16)
	for rows.Next() {
		var item PendingProviderDelivery
		if err := rows.Scan(&item.ID, &item.ConfigID, &item.ConfigAppID, &item.ProviderMessageID,
			&item.LastEvent, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
