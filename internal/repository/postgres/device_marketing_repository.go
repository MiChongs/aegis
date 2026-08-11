package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	devicedomain "aegis/internal/domain/device"

	"github.com/jackc/pgx/v5"
)

// 设备营销名称字典仓储：
// - 读取侧支持平台过滤 + 模糊搜索 + 分页
// - 写入侧支持 CRUD + 批量 upsert（用于 seed）

const deviceNameColumns = `id, platform, identifier, marketing_name, manufacturer, manufacturer_icon_url, device_image_url, source, notes, created_at, updated_at`

func (r *Repository) CountDeviceMarketingNames(ctx context.Context) (int64, error) {
	var total int64
	err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM device_marketing_names").Scan(&total)
	return total, err
}

func (r *Repository) ListDeviceMarketingNames(ctx context.Context, filter devicedomain.Filter) (*devicedomain.Page, error) {
	var conditions []string
	var args []any
	idx := 1

	if p := strings.ToLower(strings.TrimSpace(filter.Platform)); p != "" {
		// 强制小写比对，防 Service 层遗漏归一化时仍可正确过滤
		conditions = append(conditions, fmt.Sprintf("LOWER(platform) = $%d", idx))
		args = append(args, p)
		idx++
	}
	if filter.Source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", idx))
		args = append(args, filter.Source)
		idx++
	}
	if filter.Manufacturer != "" {
		conditions = append(conditions, fmt.Sprintf("LOWER(manufacturer) = LOWER($%d)", idx))
		args = append(args, filter.Manufacturer)
		idx++
	}
	if filter.Keyword != "" {
		// 搜索增强：
		// 1) 常规 ILIKE —— 原样匹配（带空格、符号的输入）
		// 2) 归一化 ILIKE —— 将两侧都去掉非字母数字再比对；输入 "17promax" 可命中 "iPhone 17 Pro Max"
		raw := filter.Keyword
		normalized := normalizeDeviceKeyword(raw)
		conditions = append(conditions, fmt.Sprintf(
			"(identifier ILIKE $%d OR marketing_name ILIKE $%d OR manufacturer ILIKE $%d OR notes ILIKE $%d OR "+
				"regexp_replace(LOWER(identifier),     '[^a-z0-9]', '', 'g') LIKE $%d OR "+
				"regexp_replace(LOWER(marketing_name), '[^a-z0-9]', '', 'g') LIKE $%d OR "+
				"regexp_replace(LOWER(manufacturer),   '[^a-z0-9]', '', 'g') LIKE $%d)",
			idx, idx, idx, idx, idx+1, idx+1, idx+1))
		args = append(args, "%"+raw+"%", "%"+normalized+"%")
		idx += 2
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM device_marketing_names "+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit

	query := fmt.Sprintf(`SELECT %s FROM device_marketing_names %s ORDER BY platform, marketing_name, identifier LIMIT %d OFFSET %d`,
		deviceNameColumns, where, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]devicedomain.MarketingName, 0, limit)
	for rows.Next() {
		item, err := scanDeviceMarketingName(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return &devicedomain.Page{Items: items, Total: total, Page: page, Limit: limit}, rows.Err()
}

func (r *Repository) GetDeviceMarketingName(ctx context.Context, id int64) (*devicedomain.MarketingName, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+deviceNameColumns+" FROM device_marketing_names WHERE id = $1", id)
	return scanDeviceMarketingName(row)
}

func (r *Repository) FindDeviceMarketingName(ctx context.Context, platform, identifier string) (*devicedomain.MarketingName, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+deviceNameColumns+" FROM device_marketing_names WHERE platform = $1 AND identifier = $2 LIMIT 1",
		platform, identifier)
	return scanDeviceMarketingName(row)
}

// ListDeviceManufacturers 返回当前字典中已登记的厂商列表（去重、非空），供前端筛选器使用
func (r *Repository) ListDeviceManufacturers(ctx context.Context, platform string) ([]string, error) {
	var rows pgx.Rows
	var err error
	if platform != "" {
		rows, err = r.pool.Query(ctx,
			`SELECT DISTINCT manufacturer FROM device_marketing_names
			 WHERE manufacturer <> '' AND platform = $1
			 ORDER BY manufacturer`, platform)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT DISTINCT manufacturer FROM device_marketing_names
			 WHERE manufacturer <> ''
			 ORDER BY manufacturer`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 64)
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) CreateDeviceMarketingName(ctx context.Context, input devicedomain.CreateInput, source string) (*devicedomain.MarketingName, error) {
	if source == "" {
		source = devicedomain.SourceManual
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO device_marketing_names (platform, identifier, marketing_name, manufacturer, manufacturer_icon_url, device_image_url, source, notes)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING `+deviceNameColumns,
		input.Platform, input.Identifier, input.MarketingName,
		input.Manufacturer, input.ManufacturerIconURL, input.DeviceImageURL,
		source, input.Notes)
	return scanDeviceMarketingName(row)
}

func (r *Repository) UpdateDeviceMarketingName(ctx context.Context, id int64, input devicedomain.UpdateInput) (*devicedomain.MarketingName, error) {
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	idx := 1
	if input.Platform != nil {
		sets = append(sets, fmt.Sprintf("platform = $%d", idx))
		args = append(args, *input.Platform)
		idx++
	}
	if input.Identifier != nil {
		sets = append(sets, fmt.Sprintf("identifier = $%d", idx))
		args = append(args, *input.Identifier)
		idx++
	}
	if input.MarketingName != nil {
		sets = append(sets, fmt.Sprintf("marketing_name = $%d", idx))
		args = append(args, *input.MarketingName)
		idx++
	}
	if input.Manufacturer != nil {
		sets = append(sets, fmt.Sprintf("manufacturer = $%d", idx))
		args = append(args, *input.Manufacturer)
		idx++
	}
	if input.ManufacturerIconURL != nil {
		sets = append(sets, fmt.Sprintf("manufacturer_icon_url = $%d", idx))
		args = append(args, *input.ManufacturerIconURL)
		idx++
	}
	if input.DeviceImageURL != nil {
		sets = append(sets, fmt.Sprintf("device_image_url = $%d", idx))
		args = append(args, *input.DeviceImageURL)
		idx++
	}
	if input.Notes != nil {
		sets = append(sets, fmt.Sprintf("notes = $%d", idx))
		args = append(args, *input.Notes)
		idx++
	}
	if len(sets) == 1 {
		// 只有 updated_at，无实际字段更新
		return r.GetDeviceMarketingName(ctx, id)
	}
	args = append(args, id)
	query := fmt.Sprintf("UPDATE device_marketing_names SET %s WHERE id = $%d RETURNING %s",
		strings.Join(sets, ", "), idx, deviceNameColumns)
	row := r.pool.QueryRow(ctx, query, args...)
	return scanDeviceMarketingName(row)
}

func (r *Repository) DeleteDeviceMarketingName(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM device_marketing_names WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("device marketing name not found")
	}
	return nil
}

// BulkUpsertDeviceMarketingNames 批量 upsert，用于 seed。返回新增条数
// 使用分批 INSERT ... ON CONFLICT DO UPDATE，保证 seed 可以补齐空字段
// （如 manufacturer 在老数据中为空时可通过重跑 seed 补填）
//
// forceRefresh=true 时即便字段非空也会被种子值覆盖（仅 source='seed' 的记录，
// source='manual' 的人工记录永不覆盖，保护管理员手工数据）
func (r *Repository) BulkUpsertDeviceMarketingNames(ctx context.Context, items []devicedomain.CreateInput, source string, forceRefresh bool) (inserted int, err error) {
	if len(items) == 0 {
		return 0, nil
	}
	if source == "" {
		source = devicedomain.SourceSeed
	}
	const batch = 500
	for start := 0; start < len(items); start += batch {
		end := start + batch
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk)*8)
		ph := 1
		for _, it := range chunk {
			placeholders = append(placeholders,
				fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
					ph, ph+1, ph+2, ph+3, ph+4, ph+5, ph+6, ph+7))
			args = append(args,
				it.Platform, it.Identifier, it.MarketingName,
				it.Manufacturer, it.ManufacturerIconURL, it.DeviceImageURL,
				source, it.Notes)
			ph += 8
		}
		var onConflict string
		if forceRefresh {
			// 强制刷新：仅 source='seed' 的记录覆盖，manual 的不动
			onConflict = `
ON CONFLICT (platform, identifier) DO UPDATE SET
    manufacturer          = CASE WHEN device_marketing_names.source = 'seed' AND EXCLUDED.manufacturer          <> '' THEN EXCLUDED.manufacturer          ELSE device_marketing_names.manufacturer          END,
    manufacturer_icon_url = CASE WHEN device_marketing_names.source = 'seed' AND EXCLUDED.manufacturer_icon_url <> '' THEN EXCLUDED.manufacturer_icon_url ELSE device_marketing_names.manufacturer_icon_url END,
    device_image_url      = CASE WHEN device_marketing_names.source = 'seed' AND EXCLUDED.device_image_url      <> '' THEN EXCLUDED.device_image_url      ELSE device_marketing_names.device_image_url      END,
    updated_at            = CASE WHEN device_marketing_names.source = 'seed' THEN NOW() ELSE device_marketing_names.updated_at END`
		} else {
			// 默认：已存在且原字段为空时用种子值补齐（不覆盖人工填写）
			onConflict = `
ON CONFLICT (platform, identifier) DO UPDATE SET
    manufacturer          = CASE WHEN device_marketing_names.manufacturer          = '' THEN EXCLUDED.manufacturer          ELSE device_marketing_names.manufacturer          END,
    manufacturer_icon_url = CASE WHEN device_marketing_names.manufacturer_icon_url = '' THEN EXCLUDED.manufacturer_icon_url ELSE device_marketing_names.manufacturer_icon_url END,
    device_image_url      = CASE WHEN device_marketing_names.device_image_url      = '' THEN EXCLUDED.device_image_url      ELSE device_marketing_names.device_image_url      END,
    updated_at            = CASE WHEN device_marketing_names.manufacturer = '' OR device_marketing_names.manufacturer_icon_url = '' OR device_marketing_names.device_image_url = '' THEN NOW() ELSE device_marketing_names.updated_at END`
		}
		query := `INSERT INTO device_marketing_names
(platform, identifier, marketing_name, manufacturer, manufacturer_icon_url, device_image_url, source, notes)
VALUES ` + strings.Join(placeholders, ",") + onConflict
		tag, execErr := r.pool.Exec(ctx, query, args...)
		if execErr != nil {
			return inserted, execErr
		}
		// RowsAffected 在 ON CONFLICT DO UPDATE 中包含了 update 行数，
		// 无法精准分出"纯新增"；这里按实际影响行近似返回
		inserted += int(tag.RowsAffected())
	}
	return inserted, nil
}

// normalizeDeviceKeyword 把搜索关键字归一化为纯小写字母数字序列
// 用户输入 "17 Pro Max"、"17-pro-max"、"17promax" 都会得到 "17promax"
func normalizeDeviceKeyword(s string) string {
	buf := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			buf = append(buf, c)
		case c >= 'A' && c <= 'Z':
			buf = append(buf, c+('a'-'A'))
		case c >= '0' && c <= '9':
			buf = append(buf, c)
		}
	}
	return string(buf)
}

type deviceNameScanner interface {
	Scan(dest ...any) error
}

func scanDeviceMarketingName(row deviceNameScanner) (*devicedomain.MarketingName, error) {
	var item devicedomain.MarketingName
	if err := row.Scan(
		&item.ID, &item.Platform, &item.Identifier, &item.MarketingName,
		&item.Manufacturer, &item.ManufacturerIconURL, &item.DeviceImageURL,
		&item.Source, &item.Notes, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}
