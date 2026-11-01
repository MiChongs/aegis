package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"aegis/internal/domain/regip"

	"github.com/jackc/pgx/v5"
)

// 注册准入的设备一侧：设备名单、按设备查台账、地址与设备的关联、设备概览。规则见 docs/register-ip.md。
// 设备标识一律按小写比对（与 regip.ParseDevice 一致），索引建在 lower(device_id) 上。

/* ───────────────────── 设备名单 ───────────────────── */

const registerDeviceListColumns = `id, appid, device_id, kind, factor, note, expires_at, hits, last_hit_at, created_by, created_at, updated_at`

func scanRegisterDeviceList(row pgx.Row) (*regip.DeviceListEntry, error) {
	var item regip.DeviceListEntry
	if err := row.Scan(&item.ID, &item.AppID, &item.DeviceID, &item.Kind, &item.Factor, &item.Note, &item.ExpiresAt,
		&item.Hits, &item.LastHitAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

func collectRegisterDeviceLists(rows pgx.Rows) ([]regip.DeviceListEntry, error) {
	defer rows.Close()
	items := []regip.DeviceListEntry{}
	for rows.Next() {
		item, err := scanRegisterDeviceList(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListActiveRegisterDeviceLists 应用当前生效的设备名单，编译快照用。
func (r *Repository) ListActiveRegisterDeviceLists(ctx context.Context, appID int64) ([]regip.DeviceListEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+registerDeviceListColumns+` FROM register_device_lists
WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW()) ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	return collectRegisterDeviceLists(rows)
}

// ListRegisterDeviceLists 设备名单列表，Keyword 模糊匹配设备标识与备注。
func (r *Repository) ListRegisterDeviceLists(ctx context.Context, q regip.ListQuery) ([]regip.DeviceListEntry, int64, error) {
	where := []string{"appid = $1"}
	args := []any{q.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if q.Kind != "" {
		add("kind = ?", q.Kind)
	}
	if !q.IncludeExpired {
		where = append(where, "(expires_at IS NULL OR expires_at > NOW())")
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		add("(device_id ILIKE ? OR note ILIKE ?)", "%"+kw+"%")
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_device_lists WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+registerDeviceListColumns+` FROM register_device_lists
WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	items, err := collectRegisterDeviceLists(rows)
	return items, total, err
}

// UpsertRegisterDeviceLists 批量写入设备名单，已存在的覆盖类型、倍数、备注与到期，命中次数保留。
func (r *Repository) UpsertRegisterDeviceLists(ctx context.Context, appID int64, devices []string, in regip.ListInput, operator string) ([]regip.DeviceListEntry, error) {
	if len(devices) == 0 {
		return []regip.DeviceListEntry{}, nil
	}
	rows, err := r.pool.Query(ctx, `
INSERT INTO register_device_lists (appid, device_id, kind, factor, note, expires_at, created_by)
SELECT $1, v, $3, $4, $5, $6, $7 FROM unnest($2::text[]) AS v
ON CONFLICT (appid, device_id) DO UPDATE SET kind = EXCLUDED.kind, factor = EXCLUDED.factor, note = EXCLUDED.note,
    expires_at = EXCLUDED.expires_at, updated_at = NOW()
RETURNING `+registerDeviceListColumns,
		appID, devices, in.Kind, in.Factor, clipString(in.Note, 200), in.ExpiresAt, clipString(operator, 128))
	if err != nil {
		return nil, err
	}
	return collectRegisterDeviceLists(rows)
}

// UpdateRegisterDeviceList 修改一项设备名单，不存在返回 nil。
func (r *Repository) UpdateRegisterDeviceList(ctx context.Context, appID, id int64, in regip.ListUpdate) (*regip.DeviceListEntry, error) {
	item, err := scanRegisterDeviceList(r.pool.QueryRow(ctx, `
UPDATE register_device_lists SET kind = $3, factor = $4, note = $5, expires_at = $6, updated_at = NOW()
WHERE appid = $1 AND id = $2 RETURNING `+registerDeviceListColumns, appID, id, in.Kind, in.Factor, clipString(in.Note, 200), in.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// DeleteRegisterDeviceLists 删除设备名单项。
func (r *Repository) DeleteRegisterDeviceLists(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM register_device_lists WHERE appid = $1 AND id = ANY($2::bigint[])`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// FindRegisterDeviceList 一台设备在名单里的那一项（含已过期），没有返回 nil。
func (r *Repository) FindRegisterDeviceList(ctx context.Context, appID int64, device string) (*regip.DeviceListEntry, error) {
	item, err := scanRegisterDeviceList(r.pool.QueryRow(ctx, `SELECT `+registerDeviceListColumns+` FROM register_device_lists
WHERE appid = $1 AND device_id = $2`, appID, strings.ToLower(device)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// AddRegisterDeviceListHits 累加设备名单命中次数。
func (r *Repository) AddRegisterDeviceListHits(ctx context.Context, hits map[int64]int64, at time.Time) error {
	if len(hits) == 0 {
		return nil
	}
	ids, counts := make([]int64, 0, len(hits)), make([]int64, 0, len(hits))
	for id, count := range hits {
		ids, counts = append(ids, id), append(counts, count)
	}
	_, err := r.pool.Exec(ctx, `
UPDATE register_device_lists l SET hits = l.hits + x.c, last_hit_at = $3
FROM unnest($1::bigint[], $2::bigint[]) AS x(id, c) WHERE l.id = x.id`, ids, counts, at)
	return err
}

// PurgeExpiredRegisterDeviceLists 删除过期超过 grace 的设备名单项。
func (r *Repository) PurgeExpiredRegisterDeviceLists(ctx context.Context, grace time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM register_device_lists WHERE expires_at IS NOT NULL AND expires_at < $1`, time.Now().Add(-grace))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

/* ───────────────────── 按设备的台账与关联 ───────────────────── */

// RegisterIPLedgerByDevice 设备最近的注册（含已解除的）与未解除的总数。
func (r *Repository) RegisterIPLedgerByDevice(ctx context.Context, appID int64, device string, limit int) ([]regip.Registration, int64, error) {
	items, _, err := r.ListRegisterIPLedger(ctx, regip.RegistrationQuery{AppID: appID, DeviceID: device, Page: 1, PageSize: limit})
	if err != nil {
		return nil, 0, err
	}
	active, err := r.CountRegisterIPLedger(ctx, RegisterIPCountQuery{AppID: appID, Device: device})
	if err != nil {
		return nil, 0, err
	}
	return items, int64(active), nil
}

func collectLinked(rows pgx.Rows) ([]regip.LinkedItem, error) {
	defer rows.Close()
	items := []regip.LinkedItem{}
	for rows.Next() {
		var item regip.LinkedItem
		if err := rows.Scan(&item.Key, &item.Accounts, &item.FirstAt, &item.LastAt, &item.Location); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// RegisterIPDevicesOfPrefix 网段内注册过的不同设备，账号多的在前。
func (r *Repository) RegisterIPDevicesOfPrefix(ctx context.Context, appID int64, prefix netip.Prefix, limit int) ([]regip.LinkedItem, int64, error) {
	from, to := regip.PrefixRange(prefix)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT lower(device_id)) FROM register_ip_ledger
WHERE appid = $1 AND ip >= $2::inet AND ip <= $3::inet AND device_id <> ''`, appID, from.String(), to.String()).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT lower(device_id), COUNT(*), MIN(created_at), MAX(created_at), ''
FROM register_ip_ledger WHERE appid = $1 AND ip >= $2::inet AND ip <= $3::inet AND device_id <> ''
GROUP BY 1 ORDER BY 2 DESC, 4 DESC LIMIT $4`, appID, from.String(), to.String(), limit)
	if err != nil {
		return nil, 0, err
	}
	items, err := collectLinked(rows)
	return items, total, err
}

// RegisterIPAddressesOfDevice 一台设备注册时用过的不同地址，账号多的在前。
func (r *Repository) RegisterIPAddressesOfDevice(ctx context.Context, appID int64, device string, limit int) ([]regip.LinkedItem, int64, error) {
	device = strings.ToLower(device)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT ip) FROM register_ip_ledger
WHERE appid = $1 AND device_id <> '' AND lower(device_id) = $2`, appID, device).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT host(ip), COUNT(*), MIN(created_at), MAX(created_at),
    COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), '')
FROM register_ip_ledger WHERE appid = $1 AND device_id <> '' AND lower(device_id) = $2
GROUP BY ip ORDER BY 2 DESC, 4 DESC LIMIT $3`, appID, device, limit)
	if err != nil {
		return nil, 0, err
	}
	items, err := collectLinked(rows)
	return items, total, err
}

/* ───────────────────── 设备概览 ───────────────────── */

// RegisterDeviceStats 概览里设备一侧的聚合。
type RegisterDeviceStats struct {
	// WithDevice / WithoutDevice 区间内带 / 不带设备标识的注册数
	WithDevice      int64
	WithoutDevice   int64
	DistinctDevices int64
	// Distribution 每台设备注册的账号数分布：1 / 2 / 3–5 / 6–10 / 11+
	Distribution map[string]int64
	TopDevices   []RegisterIPTopItem
	// TopIPDevices 出现设备最多的地址（Count 是不同设备数，Accounts 是注册数）
	TopIPDevices []RegisterIPTopItem
	// TopDeviceIPs 换地址最多的设备（Count 是不同地址数）
	TopDeviceIPs []RegisterIPTopItem
	Lists        map[string]int64
}

// RegisterDeviceOverviewStats 设备一侧的概览，六条查询一个 Batch 一次往返。
func (r *Repository) RegisterDeviceOverviewStats(ctx context.Context, appID int64, since time.Time, top int) (*RegisterDeviceStats, error) {
	stats := &RegisterDeviceStats{Distribution: map[string]int64{}, TopDevices: []RegisterIPTopItem{}, TopIPDevices: []RegisterIPTopItem{},
		TopDeviceIPs: []RegisterIPTopItem{}, Lists: map[string]int64{}}
	batch := &pgx.Batch{}
	batch.Queue(`SELECT COUNT(*) FILTER (WHERE device_id <> ''), COUNT(*) FILTER (WHERE device_id = ''),
    COUNT(DISTINCT lower(device_id)) FILTER (WHERE device_id <> '')
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2`, appID, since).
		QueryRow(func(row pgx.Row) error {
			return row.Scan(&stats.WithDevice, &stats.WithoutDevice, &stats.DistinctDevices)
		})
	batch.Queue(`SELECT bucket, COUNT(*) FROM (
    SELECT CASE WHEN c = 1 THEN '1' WHEN c = 2 THEN '2' WHEN c <= 5 THEN '3-5' WHEN c <= 10 THEN '6-10' ELSE '11+' END AS bucket
    FROM (SELECT COUNT(*) AS c FROM register_ip_ledger WHERE appid = $1 AND released_at IS NULL AND created_at >= $2 AND device_id <> ''
          GROUP BY lower(device_id)) per
) b GROUP BY bucket`, appID, since).
		Query(func(rows pgx.Rows) error {
			for rows.Next() {
				var bucket string
				var count int64
				if err := rows.Scan(&bucket, &count); err != nil {
					return err
				}
				stats.Distribution[bucket] = count
			}
			return rows.Err()
		})
	scan := func(dst *[]RegisterIPTopItem) func(pgx.Rows) error {
		return func(rows pgx.Rows) error {
			for rows.Next() {
				var item RegisterIPTopItem
				if err := rows.Scan(&item.Key, &item.Count, &item.Accounts, &item.Location, &item.LastAt); err != nil {
					return err
				}
				*dst = append(*dst, item)
			}
			return rows.Err()
		}
	}
	batch.Queue(`SELECT lower(device_id), COUNT(*), COUNT(DISTINCT ip),
    COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 AND device_id <> ''
GROUP BY 1 ORDER BY 2 DESC, 5 DESC LIMIT $3`, appID, since, top).Query(scan(&stats.TopDevices))
	batch.Queue(`SELECT host(ip), COUNT(DISTINCT lower(device_id)), COUNT(*),
    COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 AND device_id <> ''
GROUP BY ip HAVING COUNT(DISTINCT lower(device_id)) > 1 ORDER BY 2 DESC, 3 DESC LIMIT $3`, appID, since, top).Query(scan(&stats.TopIPDevices))
	batch.Queue(`SELECT lower(device_id), COUNT(DISTINCT ip), COUNT(*),
    COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 AND device_id <> ''
GROUP BY 1 HAVING COUNT(DISTINCT ip) > 1 ORDER BY 2 DESC, 3 DESC LIMIT $3`, appID, since, top).Query(scan(&stats.TopDeviceIPs))
	batch.Queue(`SELECT kind, COUNT(*) FROM register_device_lists WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW()) GROUP BY kind`, appID).
		Query(func(rows pgx.Rows) error {
			for rows.Next() {
				var kind string
				var count int64
				if err := rows.Scan(&kind, &count); err != nil {
					return err
				}
				stats.Lists[kind] = count
			}
			return rows.Err()
		})
	if err := r.pool.SendBatch(ctx, batch).Close(); err != nil {
		return nil, err
	}
	return stats, nil
}
