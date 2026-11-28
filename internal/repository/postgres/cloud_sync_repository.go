package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	cloudstorage "aegis/internal/domain/cloudstorage"
)

// 云服务的记录级同步（docs/cloud-service.md）。
//
// 与条目写入同一套纪律：先锁用户行（app_cloud_storage_users）把同一个用户的写入串起来，
// 序号在锁内分配，所以提交顺序 = 序号顺序；读者先读已提交的 sync_seq 再扫 seq <= 它的记录，
// 就不会漏掉一条「序号更小但还没提交」的写入。用量从记录表重算写回，不做加减。

var (
	// ErrCloudSyncDeviceLimit 登记的设备数已达档位上限。
	ErrCloudSyncDeviceLimit = errors.New("cloud sync device limit reached")
	// ErrCloudSyncDeviceUnknown 设备没有登记（或已被移除）。
	ErrCloudSyncDeviceUnknown = errors.New("cloud sync device not registered")
	// ErrCloudSyncRecordLimit 同步记录条数已达档位上限。
	ErrCloudSyncRecordLimit = errors.New("cloud sync record limit reached")
	// ErrCloudSyncHandoffNotFound 接力请求不存在或已过期。
	ErrCloudSyncHandoffNotFound = errors.New("cloud sync handoff not found")
)

// CloudSyncPushPlan 一批改动（服务层已校验格式、按档位过滤过集合）。
type CloudSyncPushPlan struct {
	AppID    int64
	UserID   int64
	DeviceID string
	Changes  []cloudstorage.SyncChange
	Now      time.Time

	QuotaBytes     int64
	MaxSyncRecords int
}

// CloudSyncState 用户的同步账目。
type CloudSyncState struct {
	Seq           int64
	FloorSeq      int64
	SyncBytes     int64
	SyncRecords   int64
	ItemBytes     int64
	QuotaOverride *int64
	Frozen        bool
	LastSyncAt    *time.Time
}

// GetCloudSyncState 读取同步账目；从没用过的用户返回零值。
func (r *Repository) GetCloudSyncState(ctx context.Context, appID, userID int64) (CloudSyncState, error) {
	var state CloudSyncState
	err := r.pool.QueryRow(ctx, `SELECT sync_seq, sync_floor_seq, sync_bytes, sync_records, used_bytes, quota_bytes, frozen, last_sync_at
FROM app_cloud_storage_users WHERE appid = $1 AND user_id = $2`, appID, userID).Scan(
		&state.Seq, &state.FloorSeq, &state.SyncBytes, &state.SyncRecords, &state.ItemBytes, &state.QuotaOverride,
		&state.Frozen, &state.LastSyncAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return CloudSyncState{}, nil
	}
	return state, err
}

// refreshCloudSyncUsage 从记录表重算同步用量（墓碑不占空间也不计条数）。
func refreshCloudSyncUsage(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	_, err := tx.Exec(ctx, `UPDATE app_cloud_storage_users s SET
  sync_bytes = COALESCE((SELECT SUM(r.size) FROM app_cloud_sync_records r WHERE r.user_id = s.user_id AND r.appid = s.appid AND NOT r.deleted), 0),
  sync_records = (SELECT COUNT(*) FROM app_cloud_sync_records r WHERE r.user_id = s.user_id AND r.appid = s.appid AND NOT r.deleted),
  updated_at = NOW()
WHERE s.appid = $1 AND s.user_id = $2`, appID, userID)
	return err
}

// syncWins 最后写入者胜：时间新的胜，同一时刻按设备号字典序（各端算出来的胜者相同）。
func syncWins(incomingAt time.Time, incomingDevice string, storedAt time.Time, storedDevice string) bool {
	if !incomingAt.Equal(storedAt) {
		return incomingAt.After(storedAt)
	}
	return incomingDevice > storedDevice
}

type storedSyncRow struct {
	record cloudstorage.SyncRecord
}

// PushCloudSync 在一个事务里落下一批改动：逐条最后写入者胜，接受的分配新序号，重算用量，
// 记下设备的同步时间。设备必须已登记（被移除的设备推不进来）。
func (r *Repository) PushCloudSync(ctx context.Context, plan CloudSyncPushPlan) (result *cloudstorage.SyncPushResult, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	quotaOverride, frozen, err := lockCloudUser(ctx, tx, plan.AppID, plan.UserID)
	if err != nil {
		return nil, err
	}
	if frozen {
		return nil, ErrCloudFrozen
	}
	var registered bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_cloud_sync_devices
WHERE user_id = $1 AND appid = $2 AND device_id = $3)`, plan.UserID, plan.AppID, plan.DeviceID).Scan(&registered); err != nil {
		return nil, err
	}
	if !registered {
		return nil, ErrCloudSyncDeviceUnknown
	}
	quota := plan.QuotaBytes
	if quotaOverride != nil && *quotaOverride > 0 {
		quota = *quotaOverride
	}

	// 同一批里同一条记录出现多次时只留最新的那次。
	type recordKey struct{ collection, id string }
	latest := make(map[recordKey]cloudstorage.SyncChange, len(plan.Changes))
	order := make([]recordKey, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		key := recordKey{change.Collection, change.ID}
		prev, seen := latest[key]
		if !seen {
			order = append(order, key)
		}
		if !seen || !change.UpdatedAt.Before(prev.UpdatedAt) {
			latest[key] = change
		}
	}

	collections := make([]string, 0, len(order))
	ids := make([]string, 0, len(order))
	for _, key := range order {
		collections = append(collections, key.collection)
		ids = append(ids, key.id)
	}
	stored := map[recordKey]cloudstorage.SyncRecord{}
	rows, err := tx.Query(ctx, `SELECT r.collection, r.record_id, r.payload, r.size, r.deleted, r.client_updated_at, r.device_id, r.seq, r.created_at
FROM app_cloud_sync_records r
JOIN UNNEST($3::text[], $4::text[]) AS k(collection, record_id) ON k.collection = r.collection AND k.record_id = r.record_id
WHERE r.user_id = $1 AND r.appid = $2
FOR UPDATE OF r`, plan.UserID, plan.AppID, collections, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var record cloudstorage.SyncRecord
		var payload []byte
		if err = rows.Scan(&record.Collection, &record.ID, &payload, &record.Size, &record.Deleted, &record.UpdatedAt,
			&record.DeviceID, &record.Seq, &record.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if len(payload) > 0 {
			record.Payload = json.RawMessage(payload)
		}
		stored[recordKey{record.Collection, record.ID}] = record
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}

	var seq int64
	if err = tx.QueryRow(ctx, `SELECT sync_seq FROM app_cloud_storage_users WHERE appid = $1 AND user_id = $2`,
		plan.AppID, plan.UserID).Scan(&seq); err != nil {
		return nil, err
	}

	result = &cloudstorage.SyncPushResult{Rejected: []cloudstorage.SyncRecord{}, Applied: []cloudstorage.SyncAck{}}
	var deltaBytes, deltaRecords int64
	type pendingWrite struct {
		change cloudstorage.SyncChange
		seq    int64
		size   int64
	}
	writes := make([]pendingWrite, 0, len(order))
	for _, key := range order {
		change := latest[key]
		existing, exists := stored[key]
		if exists && !syncWins(change.UpdatedAt, plan.DeviceID, existing.UpdatedAt, existing.DeviceID) {
			result.Rejected = append(result.Rejected, existing)
			continue
		}
		if exists && change.Deleted && existing.Deleted {
			// 墓碑叠墓碑：不占序号，也不必让别的设备再处理一次。
			result.Applied = append(result.Applied, cloudstorage.SyncAck{Collection: key.collection, ID: key.id, Seq: existing.Seq})
			continue
		}
		size := int64(0)
		if !change.Deleted {
			size = int64(len(change.Payload))
		}
		if exists {
			deltaBytes += size - existing.Size
			switch {
			case existing.Deleted && !change.Deleted:
				deltaRecords++
			case !existing.Deleted && change.Deleted:
				deltaRecords--
			}
		} else {
			deltaBytes += size
			if !change.Deleted {
				deltaRecords++
			}
		}
		seq++
		writes = append(writes, pendingWrite{change: change, seq: seq, size: size})
	}

	if len(writes) > 0 {
		var syncBytes, syncRecords, itemBytes int64
		if err = tx.QueryRow(ctx, `SELECT sync_bytes, sync_records, used_bytes FROM app_cloud_storage_users
WHERE appid = $1 AND user_id = $2`, plan.AppID, plan.UserID).Scan(&syncBytes, &syncRecords, &itemBytes); err != nil {
			return nil, err
		}
		if deltaRecords > 0 && syncRecords+deltaRecords > int64(plan.MaxSyncRecords) {
			return nil, ErrCloudSyncRecordLimit
		}
		if deltaBytes > 0 && itemBytes+syncBytes+deltaBytes > quota {
			return nil, &CloudQuotaError{Used: itemBytes + syncBytes, Quota: quota, Need: deltaBytes}
		}
		for _, write := range writes {
			var payload any
			if !write.change.Deleted {
				payload = []byte(write.change.Payload)
			}
			if _, err = tx.Exec(ctx, `INSERT INTO app_cloud_sync_records
(user_id, appid, collection, record_id, payload, size, deleted, client_updated_at, device_id, seq, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
ON CONFLICT (user_id, appid, collection, record_id) DO UPDATE SET
  payload = EXCLUDED.payload, size = EXCLUDED.size, deleted = EXCLUDED.deleted,
  client_updated_at = EXCLUDED.client_updated_at, device_id = EXCLUDED.device_id,
  seq = EXCLUDED.seq, updated_at = EXCLUDED.updated_at`,
				plan.UserID, plan.AppID, write.change.Collection, write.change.ID, payload, write.size,
				write.change.Deleted, write.change.UpdatedAt, plan.DeviceID, write.seq, plan.Now); err != nil {
				return nil, err
			}
			result.Applied = append(result.Applied, cloudstorage.SyncAck{Collection: write.change.Collection, ID: write.change.ID, Seq: write.seq})
		}
		result.Accepted = len(writes)
		if _, err = tx.Exec(ctx, `UPDATE app_cloud_storage_users SET sync_seq = $3, last_sync_at = $4
WHERE appid = $1 AND user_id = $2`, plan.AppID, plan.UserID, seq, plan.Now); err != nil {
			return nil, err
		}
		if err = refreshCloudSyncUsage(ctx, tx, plan.AppID, plan.UserID); err != nil {
			return nil, err
		}
	}
	result.Seq = seq
	if _, err = tx.Exec(ctx, `UPDATE app_cloud_sync_devices SET last_seen_at = $4, last_sync_at = $4,
last_seq = GREATEST(last_seq, $5), updated_at = $4
WHERE user_id = $1 AND appid = $2 AND device_id = $3`, plan.UserID, plan.AppID, plan.DeviceID, plan.Now, seq); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

const syncRecordColumns = `r.collection, r.record_id, r.payload, r.size, r.deleted, r.client_updated_at, r.device_id,
COALESCE(d.name, ''), r.seq, r.created_at`

const syncRecordFrom = `app_cloud_sync_records r
LEFT JOIN app_cloud_sync_devices d ON d.user_id = r.user_id AND d.appid = r.appid AND d.device_id = r.device_id`

func scanSyncRecord(row pgx.Row) (cloudstorage.SyncRecord, error) {
	var record cloudstorage.SyncRecord
	var payload []byte
	if err := row.Scan(&record.Collection, &record.ID, &payload, &record.Size, &record.Deleted, &record.UpdatedAt,
		&record.DeviceID, &record.DeviceName, &record.Seq, &record.CreatedAt); err != nil {
		return record, err
	}
	if len(payload) > 0 {
		record.Payload = json.RawMessage(payload)
	}
	return record, nil
}

// ListCloudSyncChanges 增量拉取：seq 大于 since 的记录（含墓碑），按 seq 升序。
func (r *Repository) ListCloudSyncChanges(ctx context.Context, query cloudstorage.SyncChangesQuery) (*cloudstorage.SyncChangesPage, error) {
	state, err := r.GetCloudSyncState(ctx, query.AppID, query.UserID)
	if err != nil {
		return nil, err
	}
	page := &cloudstorage.SyncChangesPage{Records: []cloudstorage.SyncRecord{}, Next: state.Seq, ServerTime: time.Now()}
	since := query.Since
	if since > state.Seq {
		// 游标比服务端还新：服务端被清空过（管理员清空云存储），从头来。
		page.Reset = true
		since = 0
	}
	if since > 0 && since < state.FloorSeq {
		page.Reset = true
		since = 0
	}
	args := []any{query.UserID, query.AppID, since, state.Seq}
	filter := ""
	if len(query.Collections) > 0 {
		args = append(args, query.Collections)
		filter = fmt.Sprintf(" AND r.collection = ANY($%d)", len(args))
	}
	args = append(args, query.Limit+1)
	rows, err := r.pool.Query(ctx, `SELECT `+syncRecordColumns+` FROM `+syncRecordFrom+`
WHERE r.user_id = $1 AND r.appid = $2 AND r.seq > $3 AND r.seq <= $4`+filter+`
ORDER BY r.seq LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanSyncRecord(rows)
		if err != nil {
			return nil, err
		}
		page.Records = append(page.Records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Records) > query.Limit {
		page.Records = page.Records[:query.Limit]
		page.HasMore = true
		page.Next = page.Records[len(page.Records)-1].Seq
	}
	return page, nil
}

// ListCloudSyncRecords 浏览记录：按集合 / 领域 / 写入设备筛，按修改时间倒序。
func (r *Repository) ListCloudSyncRecords(ctx context.Context, query cloudstorage.SyncRecordQuery) (*cloudstorage.SyncRecordPage, error) {
	where := []string{"r.user_id = $1", "r.appid = $2"}
	args := []any{query.UserID, query.AppID}
	if !query.IncludeDeleted {
		where = append(where, "NOT r.deleted")
	}
	if query.Collection != "" {
		args = append(args, query.Collection)
		where = append(where, fmt.Sprintf("r.collection = $%d", len(args)))
	}
	if query.Domain != "" {
		args = append(args, escapeLikePattern(query.Domain)+".%")
		where = append(where, fmt.Sprintf("r.collection LIKE $%d", len(args)))
	}
	if query.DeviceID != "" {
		args = append(args, query.DeviceID)
		where = append(where, fmt.Sprintf("r.device_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		where = append(where, fmt.Sprintf("(r.record_id ILIKE $%d OR r.payload->>'title' ILIKE $%d OR r.payload->>'subtitle' ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	clause := strings.Join(where, " AND ")
	page := &cloudstorage.SyncRecordPage{Items: []cloudstorage.SyncRecord{}, Page: query.Page, Limit: query.Limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_sync_records r WHERE `+clause, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+syncRecordColumns+` FROM `+syncRecordFrom+` WHERE `+clause+`
ORDER BY r.client_updated_at DESC, r.seq DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanSyncRecord(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, record)
	}
	return page, rows.Err()
}

// GetCloudSyncRecord 一条记录（含墓碑）；没有时返回 (nil, nil)。
func (r *Repository) GetCloudSyncRecord(ctx context.Context, appID, userID int64, collection, id string) (*cloudstorage.SyncRecord, error) {
	record, err := scanSyncRecord(r.pool.QueryRow(ctx, `SELECT `+syncRecordColumns+` FROM `+syncRecordFrom+`
WHERE r.user_id = $1 AND r.appid = $2 AND r.collection = $3 AND r.record_id = $4`, userID, appID, collection, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// CloudSyncCollectionUsage 每个集合的条数、字节数、最后修改时间与按设备的分布（不含墓碑）。
func (r *Repository) CloudSyncCollectionUsage(ctx context.Context, appID, userID int64) ([]cloudstorage.CollectionUsage, error) {
	rows, err := r.pool.Query(ctx, `SELECT collection, device_id, COUNT(*), COALESCE(SUM(size), 0), MAX(client_updated_at)
FROM app_cloud_sync_records WHERE user_id = $1 AND appid = $2 AND NOT deleted
GROUP BY collection, device_id`, userID, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]*cloudstorage.CollectionUsage{}
	for rows.Next() {
		var collection, deviceID string
		var count, size int64
		var last time.Time
		if err := rows.Scan(&collection, &deviceID, &count, &size, &last); err != nil {
			return nil, err
		}
		usage := byKey[collection]
		if usage == nil {
			usage = &cloudstorage.CollectionUsage{CollectionInfo: cloudstorage.LookupCollection(collection), ByDevice: map[string]int64{}}
			byKey[collection] = usage
		}
		usage.Count += count
		usage.Bytes += size
		usage.ByDevice[deviceID] += count
		if usage.LastUpdatedAt == nil || last.After(*usage.LastUpdatedAt) {
			stamp := last
			usage.LastUpdatedAt = &stamp
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]cloudstorage.CollectionUsage, 0, len(byKey))
	for _, usage := range byKey {
		out = append(out, *usage)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// DeleteCloudSyncCollection 把一个集合里的记录全部改成墓碑（别的设备拉到后跟着删）。返回被删的条数。
func (r *Repository) DeleteCloudSyncCollection(ctx context.Context, appID, userID int64, collection, deviceID string, now time.Time) (deleted int64, seq int64, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	_, frozen, err := lockCloudUser(ctx, tx, appID, userID)
	if err != nil {
		return 0, 0, err
	}
	if frozen {
		return 0, 0, ErrCloudFrozen
	}
	if err = tx.QueryRow(ctx, `SELECT sync_seq FROM app_cloud_storage_users WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&seq); err != nil {
		return 0, 0, err
	}
	// 序号按 record_id 顺序连续分配：ROW_NUMBER 让一条语句完成，不必逐条往返。
	tag, err := tx.Exec(ctx, `WITH targets AS (
  SELECT record_id, ROW_NUMBER() OVER (ORDER BY record_id) AS n
  FROM app_cloud_sync_records WHERE user_id = $1 AND appid = $2 AND collection = $3 AND NOT deleted
)
UPDATE app_cloud_sync_records r SET deleted = TRUE, payload = NULL, size = 0,
  client_updated_at = GREATEST(r.client_updated_at, $5), device_id = $4, seq = $6 + t.n, updated_at = $5
FROM targets t
WHERE r.user_id = $1 AND r.appid = $2 AND r.collection = $3 AND r.record_id = t.record_id`,
		userID, appID, collection, deviceID, now, seq)
	if err != nil {
		return 0, 0, err
	}
	deleted = tag.RowsAffected()
	seq += deleted
	if deleted > 0 {
		if _, err = tx.Exec(ctx, `UPDATE app_cloud_storage_users SET sync_seq = $3, last_sync_at = $4
WHERE appid = $1 AND user_id = $2`, appID, userID, seq, now); err != nil {
			return 0, 0, err
		}
		if err = refreshCloudSyncUsage(ctx, tx, appID, userID); err != nil {
			return 0, 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return deleted, seq, nil
}

// ── 设备 ──

const syncDeviceColumns = `device_id, name, platform, model, os, os_version, app_version, last_seen_at, last_sync_at,
last_seq, activity, created_at`

func scanSyncDevice(row pgx.Row) (cloudstorage.SyncDevice, error) {
	var device cloudstorage.SyncDevice
	var activity []byte
	if err := row.Scan(&device.DeviceID, &device.Name, &device.Platform, &device.Model, &device.OS, &device.OSVersion,
		&device.AppVersion, &device.LastSeenAt, &device.LastSyncAt, &device.LastSeq, &activity, &device.CreatedAt); err != nil {
		return device, err
	}
	if len(activity) > 0 && !bytes.Equal(activity, []byte("null")) {
		var value cloudstorage.Activity
		if json.Unmarshal(activity, &value) == nil {
			device.Activity = &value
		}
	}
	device.Collections = map[string]int64{}
	return device, nil
}

// RegisterCloudSyncDevice 登记（或刷新）一台设备。新设备超出档位设备数时返回 ErrCloudSyncDeviceLimit；
// 已登记的设备只刷新信息（用户改过的名字不被覆盖）。
func (r *Repository) RegisterCloudSyncDevice(ctx context.Context, appID, userID int64, info cloudstorage.DeviceInfo, customName bool, maxDevices int, now time.Time) (device *cloudstorage.SyncDevice, created bool, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, _, err = lockCloudUser(ctx, tx, appID, userID); err != nil {
		return nil, false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_cloud_sync_devices WHERE user_id = $1 AND appid = $2 AND device_id = $3)`,
		userID, appID, info.DeviceID).Scan(&exists); err != nil {
		return nil, false, err
	}
	if !exists {
		var count int
		if err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM app_cloud_sync_devices WHERE user_id = $1 AND appid = $2`,
			userID, appID).Scan(&count); err != nil {
			return nil, false, err
		}
		if count >= maxDevices {
			return nil, false, ErrCloudSyncDeviceLimit
		}
		if _, err = tx.Exec(ctx, `INSERT INTO app_cloud_sync_devices
(user_id, appid, device_id, name, custom_name, platform, model, os, os_version, app_version, last_seen_at, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11,$11)`,
			userID, appID, info.DeviceID, info.Name, customName, info.Platform, info.Model, info.OS, info.OSVersion,
			info.AppVersion, now); err != nil {
			return nil, false, err
		}
	} else {
		if _, err = tx.Exec(ctx, `UPDATE app_cloud_sync_devices SET
  name = CASE WHEN $5 THEN $4 WHEN custom_name OR $4 = '' THEN name ELSE $4 END,
  custom_name = custom_name OR $5,
  platform = COALESCE(NULLIF($6, ''), platform), model = COALESCE(NULLIF($7, ''), model),
  os = COALESCE(NULLIF($8, ''), os), os_version = COALESCE(NULLIF($9, ''), os_version),
  app_version = COALESCE(NULLIF($10, ''), app_version),
  last_seen_at = $11, updated_at = $11
WHERE user_id = $1 AND appid = $2 AND device_id = $3`,
			userID, appID, info.DeviceID, info.Name, customName, info.Platform, info.Model, info.OS, info.OSVersion,
			info.AppVersion, now); err != nil {
			return nil, false, err
		}
	}
	value, err := scanSyncDevice(tx.QueryRow(ctx, `SELECT `+syncDeviceColumns+` FROM app_cloud_sync_devices
WHERE user_id = $1 AND appid = $2 AND device_id = $3`, userID, appID, info.DeviceID))
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return &value, !exists, nil
}

// RenameCloudSyncDevice 改名（之后不再随会话型号刷新）。
func (r *Repository) RenameCloudSyncDevice(ctx context.Context, appID, userID int64, deviceID, name string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE app_cloud_sync_devices SET name = $4, custom_name = TRUE, updated_at = NOW()
WHERE user_id = $1 AND appid = $2 AND device_id = $3`, userID, appID, deviceID, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudSyncDeviceUnknown
	}
	return nil
}

// ListCloudSyncDevices 用户登记的全部设备（最近活跃的在前），带每台设备写入的记录分布。
func (r *Repository) ListCloudSyncDevices(ctx context.Context, appID, userID int64) ([]cloudstorage.SyncDevice, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+syncDeviceColumns+` FROM app_cloud_sync_devices
WHERE user_id = $1 AND appid = $2 ORDER BY last_seen_at DESC`, userID, appID)
	if err != nil {
		return nil, err
	}
	devices := []cloudstorage.SyncDevice{}
	index := map[string]int{}
	for rows.Next() {
		device, err := scanSyncDevice(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		index[device.DeviceID] = len(devices)
		devices = append(devices, device)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	counts, err := r.pool.Query(ctx, `SELECT device_id, collection, COUNT(*) FROM app_cloud_sync_records
WHERE user_id = $1 AND appid = $2 AND NOT deleted GROUP BY device_id, collection`, userID, appID)
	if err != nil {
		return nil, err
	}
	defer counts.Close()
	for counts.Next() {
		var deviceID, collection string
		var count int64
		if err := counts.Scan(&deviceID, &collection, &count); err != nil {
			return nil, err
		}
		if i, ok := index[deviceID]; ok {
			devices[i].Collections[collection] = count
			devices[i].RecordCount += count
		}
	}
	return devices, counts.Err()
}

// TouchCloudSyncDevice 记一次心跳（拉取、上报都算）。没登记的设备不报错，返回 false。
func (r *Repository) TouchCloudSyncDevice(ctx context.Context, appID, userID int64, deviceID string, now time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_cloud_sync_devices SET last_seen_at = $4
WHERE user_id = $1 AND appid = $2 AND device_id = $3`, userID, appID, deviceID, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetCloudSyncActivity 设备的「正在播放 / 阅读」；activity 为 nil 时清空。
func (r *Repository) SetCloudSyncActivity(ctx context.Context, appID, userID int64, deviceID string, activity *cloudstorage.Activity, now time.Time) error {
	var payload any
	if activity != nil {
		raw, err := json.Marshal(activity)
		if err != nil {
			return err
		}
		payload = raw
	}
	tag, err := r.pool.Exec(ctx, `UPDATE app_cloud_sync_devices SET activity = $4, last_seen_at = $5, updated_at = $5
WHERE user_id = $1 AND appid = $2 AND device_id = $3`, userID, appID, deviceID, payload, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudSyncDeviceUnknown
	}
	return nil
}

// RemoveCloudSyncDevice 移除一台设备（腾出设备名额）。它写过的记录保留，待接收的接力一并作废。
func (r *Repository) RemoveCloudSyncDevice(ctx context.Context, appID, userID int64, deviceID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM app_cloud_sync_devices WHERE user_id = $1 AND appid = $2 AND device_id = $3`,
		userID, appID, deviceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudSyncDeviceUnknown
	}
	if _, err := tx.Exec(ctx, `DELETE FROM app_cloud_sync_handoffs WHERE user_id = $1 AND appid = $2 AND to_device_id = $3 AND acked_at IS NULL`,
		userID, appID, deviceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── 接力 ──

const syncHandoffColumns = `h.id, h.from_device_id, COALESCE(d.name, ''), h.to_device_id, h.collection, h.record_id, h.payload,
h.created_at, h.expires_at, h.acked_at`

const syncHandoffFrom = `app_cloud_sync_handoffs h
LEFT JOIN app_cloud_sync_devices d ON d.user_id = h.user_id AND d.appid = h.appid AND d.device_id = h.from_device_id`

func scanSyncHandoff(row pgx.Row) (cloudstorage.Handoff, error) {
	var item cloudstorage.Handoff
	var payload []byte
	if err := row.Scan(&item.ID, &item.FromDeviceID, &item.FromName, &item.ToDeviceID, &item.Collection, &item.RecordID,
		&payload, &item.CreatedAt, &item.ExpiresAt, &item.AckedAt); err != nil {
		return item, err
	}
	if len(payload) > 0 {
		item.Payload = json.RawMessage(payload)
	}
	return item, nil
}

// CreateCloudSyncHandoff 发起一次接力：目标设备必须已登记。
func (r *Repository) CreateCloudSyncHandoff(ctx context.Context, appID, userID int64, item cloudstorage.Handoff) (*cloudstorage.Handoff, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_cloud_sync_devices WHERE user_id = $1 AND appid = $2 AND device_id = $3)`,
		userID, appID, item.ToDeviceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrCloudSyncDeviceUnknown
	}
	var payload any
	if len(item.Payload) > 0 {
		payload = []byte(item.Payload)
	}
	var id int64
	if err := r.pool.QueryRow(ctx, `INSERT INTO app_cloud_sync_handoffs
(user_id, appid, from_device_id, to_device_id, collection, record_id, payload, created_at, expires_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, userID, appID, item.FromDeviceID, item.ToDeviceID, item.Collection,
		item.RecordID, payload, item.CreatedAt, item.ExpiresAt).Scan(&id); err != nil {
		return nil, err
	}
	created, err := scanSyncHandoff(r.pool.QueryRow(ctx, `SELECT `+syncHandoffColumns+` FROM `+syncHandoffFrom+` WHERE h.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// ListPendingCloudSyncHandoffs 发给这台设备、还没领取也没过期的接力（新的在前）。
func (r *Repository) ListPendingCloudSyncHandoffs(ctx context.Context, appID, userID int64, deviceID string, now time.Time) ([]cloudstorage.Handoff, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+syncHandoffColumns+` FROM `+syncHandoffFrom+`
WHERE h.user_id = $1 AND h.appid = $2 AND h.to_device_id = $3 AND h.acked_at IS NULL AND h.expires_at > $4
ORDER BY h.created_at DESC LIMIT 20`, userID, appID, deviceID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cloudstorage.Handoff{}
	for rows.Next() {
		item, err := scanSyncHandoff(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AckCloudSyncHandoff 领取（或忽略）一次接力。只有目标设备能领取。
func (r *Repository) AckCloudSyncHandoff(ctx context.Context, appID, userID, id int64, deviceID string, now time.Time) error {
	tag, err := r.pool.Exec(ctx, `UPDATE app_cloud_sync_handoffs SET acked_at = $5
WHERE id = $3 AND user_id = $1 AND appid = $2 AND to_device_id = $4 AND acked_at IS NULL`, userID, appID, id, deviceID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCloudSyncHandoffNotFound
	}
	return nil
}

// ── 后台清理 ──

// PurgeCloudSyncTombstones 清掉早于 before 的墓碑，并把各用户的墓碑地平线推到被清掉的最大序号：
// 游标比地平线旧的设备下次拉取拿到 reset。返回清掉的条数。
func (r *Repository) PurgeCloudSyncTombstones(ctx context.Context, appID int64, before time.Time, limit int) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `WITH doomed AS (
  SELECT user_id, collection, record_id FROM app_cloud_sync_records
  WHERE appid = $1 AND deleted AND updated_at < $2
  ORDER BY updated_at LIMIT $3
  FOR UPDATE SKIP LOCKED
)
DELETE FROM app_cloud_sync_records r USING doomed d
WHERE r.user_id = d.user_id AND r.appid = $1 AND r.collection = d.collection AND r.record_id = d.record_id
RETURNING r.user_id, r.seq`, appID, before, limit)
	if err != nil {
		return 0, err
	}
	floors := map[int64]int64{}
	var purged int64
	for rows.Next() {
		var userID, seq int64
		if err := rows.Scan(&userID, &seq); err != nil {
			rows.Close()
			return 0, err
		}
		purged++
		floors[userID] = max(floors[userID], seq)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for userID, floor := range floors {
		if _, err := tx.Exec(ctx, `UPDATE app_cloud_storage_users SET sync_floor_seq = GREATEST(sync_floor_seq, $3)
WHERE appid = $1 AND user_id = $2`, appID, userID, floor); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM app_cloud_sync_handoffs WHERE appid = $1 AND expires_at < $2`,
		appID, before); err != nil {
		return 0, err
	}
	return purged, tx.Commit(ctx)
}

// ListCloudSyncHistoryUsers 在历史类集合里有早于 before 的记录的用户（按档位逐人裁剪的候选）。
func (r *Repository) ListCloudSyncHistoryUsers(ctx context.Context, appID int64, collections []string, before time.Time, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT user_id FROM app_cloud_sync_records
WHERE appid = $1 AND collection = ANY($2) AND NOT deleted AND client_updated_at < $3 LIMIT $4`, appID, collections, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		users = append(users, id)
	}
	return users, rows.Err()
}

// TrimCloudSyncHistory 删掉一个用户历史类集合里早于 before 的记录。这是云端保留期到了，
// 不是用户删除：直接清除、不留墓碑，各设备本地的历史不受影响。
func (r *Repository) TrimCloudSyncHistory(ctx context.Context, appID, userID int64, collections []string, before time.Time) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, _, err := lockCloudUser(ctx, tx, appID, userID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM app_cloud_sync_records
WHERE user_id = $1 AND appid = $2 AND collection = ANY($3) AND NOT deleted AND client_updated_at < $4`,
		userID, appID, collections, before)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() > 0 {
		if err := refreshCloudSyncUsage(ctx, tx, appID, userID); err != nil {
			return 0, err
		}
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// PurgeCloudSyncUser 清空一个用户的同步数据（记录、设备、接力），序号不归零：
// 旧游标比 sync_seq 还新的情况由拉取侧判成 reset。地平线推到当前序号，旧游标同样 reset。
func purgeCloudSyncUserTx(ctx context.Context, tx pgx.Tx, appID, userID int64) error {
	for _, statement := range []string{
		`DELETE FROM app_cloud_sync_records WHERE user_id = $1 AND appid = $2`,
		`DELETE FROM app_cloud_sync_devices WHERE user_id = $1 AND appid = $2`,
		`DELETE FROM app_cloud_sync_handoffs WHERE user_id = $1 AND appid = $2`,
		`UPDATE app_cloud_storage_users SET sync_floor_seq = sync_seq + 1, sync_seq = sync_seq + 1,
  sync_bytes = 0, sync_records = 0 WHERE user_id = $1 AND appid = $2`,
	} {
		if _, err := tx.Exec(ctx, statement, userID, appID); err != nil {
			return err
		}
	}
	return nil
}
