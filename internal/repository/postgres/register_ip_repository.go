package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"aegis/internal/domain/regip"

	"github.com/jackc/pgx/v5"
)

// 注册 IP 控制的数据访问：注册台账、名单与拦截记录。规则见 docs/register-ip.md。
//
// 地址一律以 inet 存。按网段数注册数时用网段的首末地址做范围条件（ip >= first AND ip <= last），
// 走 (appid, ip) 的 btree 索引；`<<=` 在没有 GiST 索引时只能逐行比。

/* ───────────────────── 注册台账 ───────────────────── */

// RegisterIPLedgerRow 待写入的一行台账。
type RegisterIPLedgerRow struct {
	AppID     int64
	UserID    int64
	Account   string
	IP        netip.Addr
	Method    string
	Country   string
	Province  string
	City      string
	ISP       string
	DeviceID  string
	CreatedAt time.Time
}

// InsertRegisterIPLedger 批量写台账：一条 INSERT … SELECT unnest(…)，一次往返写完一整批。
// 同一用户重复写入（重试、回填过的老账号）按唯一索引忽略。
func (r *Repository) InsertRegisterIPLedger(ctx context.Context, rows []RegisterIPLedgerRow) error {
	if len(rows) == 0 {
		return nil
	}
	n := len(rows)
	appIDs, userIDs := make([]int64, n), make([]int64, n)
	accounts, ips, methods := make([]string, n), make([]string, n), make([]string, n)
	countries, provinces, cities, isps, devices := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	createdAt := make([]time.Time, n)
	for i, row := range rows {
		appIDs[i], userIDs[i] = row.AppID, row.UserID
		accounts[i] = clipString(row.Account, 128)
		ips[i] = row.IP.String()
		methods[i] = clipString(row.Method, 16)
		countries[i], provinces[i], cities[i] = clipString(row.Country, 64), clipString(row.Province, 64), clipString(row.City, 64)
		isps[i], devices[i] = clipString(row.ISP, 128), clipString(row.DeviceID, 128)
		createdAt[i] = row.CreatedAt
		if createdAt[i].IsZero() {
			createdAt[i] = time.Now()
		}
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO register_ip_ledger (appid, user_id, account, ip, method, country, province, city, isp, device_id, created_at)
SELECT a, NULLIF(u, 0), acc, ip::inet, m, co, pr, ci, isp, d, t
FROM unnest($1::bigint[], $2::bigint[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::timestamptz[])
    AS x(a, u, acc, ip, m, co, pr, ci, isp, d, t)
WHERE EXISTS (SELECT 1 FROM apps WHERE apps.id = x.a)
ON CONFLICT (user_id) WHERE user_id IS NOT NULL DO NOTHING`,
		appIDs, userIDs, accounts, ips, methods, countries, provinces, cities, isps, devices, createdAt)
	return err
}

// RegisterIPCountQuery 累计型规则要数的范围。Prefix、Device、City 三选一，都为空表示整个应用。
type RegisterIPCountQuery struct {
	AppID  int64
	Prefix netip.Prefix
	// Device 规整后的设备标识（小写）
	Device  string
	City    string // 「国家/省/市」
	Methods []string
	Since   *time.Time
}

func registerIPScopeWhere(q RegisterIPCountQuery, alias string, args []any) ([]string, []any) {
	where := []string{alias + "appid = $1", alias + "released_at IS NULL"}
	add := func(clause string, values ...any) {
		for _, value := range values {
			args = append(args, value)
			clause = strings.Replace(clause, "?", fmt.Sprintf("$%d", len(args)), 1)
		}
		where = append(where, clause)
	}
	if q.Prefix.IsValid() {
		from, to := regip.PrefixRange(q.Prefix)
		add(alias+"ip >= ?::inet AND "+alias+"ip <= ?::inet", from.String(), to.String())
	} else if q.Device != "" {
		add(alias+"device_id <> '' AND lower("+alias+"device_id) = ?", strings.ToLower(q.Device))
	} else if q.City != "" {
		parts := strings.SplitN(q.City, "/", 3)
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		add(alias+"country = ?", parts[0])
		if parts[1] != "" {
			add(alias+"province = ?", parts[1])
		}
		if parts[2] != "" {
			add(alias+"city = ?", parts[2])
		}
	}
	if len(q.Methods) > 0 {
		add(alias+"method = ANY(?::text[])", q.Methods)
	}
	if q.Since != nil {
		add(alias+"created_at >= ?", *q.Since)
	}
	return where, args
}

// CountRegisterIPLedger 范围内未解除的注册数。
func (r *Repository) CountRegisterIPLedger(ctx context.Context, q RegisterIPCountQuery) (int, error) {
	where, args := registerIPScopeWhere(q, "", []any{q.AppID})
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_ip_ledger WHERE `+strings.Join(where, " AND "), args...).Scan(&count)
	return count, err
}

// ReleaseRegisterIPLedgerByDevice 解除一台设备的台账：不再计入设备累计上限。
func (r *Repository) ReleaseRegisterIPLedgerByDevice(ctx context.Context, appID int64, device string) (int64, error) {
	where, args := registerIPScopeWhere(RegisterIPCountQuery{AppID: appID, Device: device}, "", []any{appID})
	tag, err := r.pool.Exec(ctx, `UPDATE register_ip_ledger SET released_at = NOW() WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ReleaseRegisterIPLedger 解除网段内的台账：不再计入累计上限，账号不受影响。返回解除了多少行。
func (r *Repository) ReleaseRegisterIPLedger(ctx context.Context, appID int64, prefix netip.Prefix) (int64, error) {
	where, args := registerIPScopeWhere(RegisterIPCountQuery{AppID: appID, Prefix: prefix}, "", []any{appID})
	tag, err := r.pool.Exec(ctx, `UPDATE register_ip_ledger SET released_at = NOW() WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const registerIPLedgerColumns = `l.id, l.appid, l.user_id, COALESCE(NULLIF(l.account, ''), u.account, ''), host(l.ip), l.method,
l.country, l.province, l.city, l.isp, l.device_id, l.released_at, l.created_at`

func scanRegisterIPLedger(row pgx.Row) (*regip.Registration, error) {
	var item regip.Registration
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Account, &item.IP, &item.Method,
		&item.Country, &item.Province, &item.City, &item.ISP, &item.DeviceID, &item.ReleasedAt, &item.CreatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListRegisterIPLedger 注册台账，最新的在前。IP 可以是地址或网段。
func (r *Repository) ListRegisterIPLedger(ctx context.Context, q regip.RegistrationQuery) ([]regip.Registration, int64, error) {
	where := []string{"l.appid = $1"}
	args := []any{q.AppID}
	add := func(clause string, values ...any) {
		for _, value := range values {
			args = append(args, value)
			clause = strings.Replace(clause, "?", fmt.Sprintf("$%d", len(args)), 1)
		}
		where = append(where, clause)
	}
	if raw := strings.TrimSpace(q.IP); raw != "" {
		prefix, err := regip.ParsePrefix(raw)
		if err != nil {
			return nil, 0, err
		}
		from, to := regip.PrefixRange(prefix)
		add("l.ip >= ?::inet AND l.ip <= ?::inet", from.String(), to.String())
	}
	if q.Method != "" {
		add("l.method = ?", q.Method)
	}
	if device := strings.TrimSpace(q.DeviceID); device != "" {
		add("l.device_id <> '' AND lower(l.device_id) = ?", strings.ToLower(device))
	}
	if q.Released != nil {
		if *q.Released {
			where = append(where, "l.released_at IS NOT NULL")
		} else {
			where = append(where, "l.released_at IS NULL")
		}
	}
	if q.From != nil {
		add("l.created_at >= ?", *q.From)
	}
	if q.To != nil {
		add("l.created_at < ?", *q.To)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		add("(l.account ILIKE ? OR u.account ILIKE ? OR l.city ILIKE ? OR l.province ILIKE ? OR l.isp ILIKE ? OR l.device_id ILIKE ?)",
			like, like, like, like, like, like)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_ip_ledger l LEFT JOIN users u ON u.id = l.user_id WHERE `+clause,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+registerIPLedgerColumns+`
FROM register_ip_ledger l LEFT JOIN users u ON u.id = l.user_id
WHERE %s ORDER BY l.created_at DESC, l.id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []regip.Registration{}
	for rows.Next() {
		item, err := scanRegisterIPLedger(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

/* ───────────────────── 名单 ───────────────────── */

const registerIPListColumns = `id, appid, cidr::text, kind, factor, note, expires_at, hits, last_hit_at, created_by, created_at, updated_at`

func scanRegisterIPList(row pgx.Row) (*regip.ListEntry, error) {
	var item regip.ListEntry
	if err := row.Scan(&item.ID, &item.AppID, &item.CIDR, &item.Kind, &item.Factor, &item.Note, &item.ExpiresAt,
		&item.Hits, &item.LastHitAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.CIDR = compactCIDR(item.CIDR)
	return &item, nil
}

// compactCIDR 单个地址的 cidr 文本是 1.2.3.4/32，展示与比对都去掉后缀。
func compactCIDR(value string) string {
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.IsSingleIP() {
		return prefix.Addr().String()
	}
	return value
}

// ListActiveRegisterIPLists 应用当前生效的全部名单项，编译名单快照用。
func (r *Repository) ListActiveRegisterIPLists(ctx context.Context, appID int64) ([]regip.ListEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+registerIPListColumns+` FROM register_ip_lists
WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW()) ORDER BY id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []regip.ListEntry{}
	for rows.Next() {
		item, err := scanRegisterIPList(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListRegisterIPLists 名单列表。Keyword 是地址时列出包含它的网段，否则按备注与网段文本模糊查。
func (r *Repository) ListRegisterIPLists(ctx context.Context, q regip.ListQuery) ([]regip.ListEntry, int64, error) {
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
		if addr, ok := regip.ParseIP(kw); ok {
			add("cidr >>= ?::inet", addr.String())
		} else {
			add("(cidr::text ILIKE ? OR note ILIKE ?)", "%"+kw+"%")
		}
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_ip_lists WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+registerIPListColumns+` FROM register_ip_lists
WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []regip.ListEntry{}
	for rows.Next() {
		item, err := scanRegisterIPList(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// UpsertRegisterIPLists 批量写入名单。同一网段已存在时覆盖类型、倍数、备注与到期时间，命中次数保留。
func (r *Repository) UpsertRegisterIPLists(ctx context.Context, appID int64, prefixes []netip.Prefix, in regip.ListInput, operator string) ([]regip.ListEntry, error) {
	if len(prefixes) == 0 {
		return []regip.ListEntry{}, nil
	}
	values := make([]string, len(prefixes))
	for i, prefix := range prefixes {
		values[i] = prefix.String()
	}
	rows, err := r.pool.Query(ctx, `
INSERT INTO register_ip_lists (appid, cidr, kind, factor, note, expires_at, created_by)
SELECT $1, v::cidr, $3, $4, $5, $6, $7 FROM unnest($2::text[]) AS v
ON CONFLICT (appid, cidr) DO UPDATE SET kind = EXCLUDED.kind, factor = EXCLUDED.factor, note = EXCLUDED.note,
    expires_at = EXCLUDED.expires_at, updated_at = NOW()
RETURNING `+registerIPListColumns,
		appID, values, in.Kind, in.Factor, clipString(in.Note, 200), in.ExpiresAt, clipString(operator, 128))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []regip.ListEntry{}
	for rows.Next() {
		item, err := scanRegisterIPList(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// UpdateRegisterIPList 修改一项名单。不存在时返回 nil。
func (r *Repository) UpdateRegisterIPList(ctx context.Context, appID, id int64, in regip.ListUpdate) (*regip.ListEntry, error) {
	item, err := scanRegisterIPList(r.pool.QueryRow(ctx, `
UPDATE register_ip_lists SET kind = $3, factor = $4, note = $5, expires_at = $6, updated_at = NOW()
WHERE appid = $1 AND id = $2 RETURNING `+registerIPListColumns, appID, id, in.Kind, in.Factor, clipString(in.Note, 200), in.ExpiresAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// DeleteRegisterIPLists 删除名单项，返回删了几项。
func (r *Repository) DeleteRegisterIPLists(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM register_ip_lists WHERE appid = $1 AND id = ANY($2::bigint[])`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PurgeExpiredRegisterIPLists 删除过期超过 grace 的名单项。
func (r *Repository) PurgeExpiredRegisterIPLists(ctx context.Context, grace time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM register_ip_lists WHERE expires_at IS NOT NULL AND expires_at < $1`, time.Now().Add(-grace))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// AddRegisterIPListHits 累加命中次数（批量，一次往返）。
func (r *Repository) AddRegisterIPListHits(ctx context.Context, hits map[int64]int64, at time.Time) error {
	if len(hits) == 0 {
		return nil
	}
	ids, counts := make([]int64, 0, len(hits)), make([]int64, 0, len(hits))
	for id, count := range hits {
		ids, counts = append(ids, id), append(counts, count)
	}
	_, err := r.pool.Exec(ctx, `
UPDATE register_ip_lists l SET hits = l.hits + x.c, last_hit_at = $3
FROM unnest($1::bigint[], $2::bigint[]) AS x(id, c) WHERE l.id = x.id`, ids, counts, at)
	return err
}

/* ───────────────────── 拦截记录 ───────────────────── */

// InsertRegisterIPEvents 批量写拦截记录：COPY 协议，一批几百条也是一次往返。
func (r *Repository) InsertRegisterIPEvents(ctx context.Context, events []regip.Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		extra := event.Extra
		if extra == nil {
			extra = map[string]any{}
		}
		data, err := json.Marshal(extra)
		if err != nil {
			return err
		}
		at := event.At
		if at.IsZero() {
			at = time.Now()
		}
		rows = append(rows, []any{
			event.AppID, clipString(event.Kind, 24), clipString(event.Reason, 16), clipString(event.RuleID, 32), clipString(event.RuleName, 64),
			clipString(event.Method, 16), clipString(event.IP, 64), clipString(event.Subnet, 64), clipString(event.Account, 128),
			clipString(event.DeviceID, 128), clipString(event.Country, 64), clipString(event.Province, 64), clipString(event.City, 64),
			clipString(event.ISP, 128), int32(event.Count), int32(event.Limit), clipString(event.Detail, 300), data,
			clipString(event.Operator, 128), at,
		})
	}
	_, err := r.pool.CopyFrom(ctx, pgx.Identifier{"register_ip_events"}, []string{
		"appid", "kind", "reason", "rule_id", "rule_name", "method", "ip", "subnet", "account", "device_id",
		"country", "province", "city", "isp", "count_value", "limit_value", "detail", "extra", "operator", "created_at",
	}, pgx.CopyFromRows(rows))
	return err
}

const registerIPEventColumns = `id, appid, kind, reason, rule_id, rule_name, method, ip, subnet, account, device_id,
country, province, city, isp, count_value, limit_value, detail, extra, operator, created_at`

func scanRegisterIPEvent(row pgx.Row) (*regip.Event, error) {
	var item regip.Event
	var extra []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Kind, &item.Reason, &item.RuleID, &item.RuleName, &item.Method, &item.IP,
		&item.Subnet, &item.Account, &item.DeviceID, &item.Country, &item.Province, &item.City, &item.ISP, &item.Count, &item.Limit,
		&item.Detail, &extra, &item.Operator, &item.At); err != nil {
		return nil, err
	}
	if len(extra) > 2 {
		_ = json.Unmarshal(extra, &item.Extra)
	}
	return &item, nil
}

// ListRegisterIPEvents 拦截记录，最新的在前。IP 可以是地址或网段。
func (r *Repository) ListRegisterIPEvents(ctx context.Context, q regip.EventQuery) ([]regip.Event, int64, error) {
	where := []string{"appid = $1"}
	args := []any{q.AppID}
	add := func(clause string, values ...any) {
		for _, value := range values {
			args = append(args, value)
			clause = strings.Replace(clause, "?", fmt.Sprintf("$%d", len(args)), 1)
		}
		where = append(where, clause)
	}
	if q.Kind != "" {
		add("kind = ?", q.Kind)
	}
	if q.Reason != "" {
		add("reason = ?", q.Reason)
	}
	if q.RuleID != "" {
		add("rule_id = ?", q.RuleID)
	}
	if device := strings.TrimSpace(q.DeviceID); device != "" {
		add("device_id <> '' AND lower(device_id) = ?", strings.ToLower(device))
	}
	if q.Method != "" {
		add("method = ?", q.Method)
	}
	if raw := strings.TrimSpace(q.IP); raw != "" {
		prefix, err := regip.ParsePrefix(raw)
		if err != nil {
			return nil, 0, err
		}
		if prefix.IsSingleIP() {
			add("(ip = ? OR subnet = ?)", prefix.Addr().String(), prefix.String())
		} else {
			// ip 列是文本（拦截时可能连地址都解析不了），网段筛选在库里转一次
			add("register_ip_try_inet(ip) <<= ?::cidr", prefix.String())
		}
	}
	if q.From != nil {
		add("created_at >= ?", *q.From)
	}
	if q.To != nil {
		add("created_at < ?", *q.To)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		like := "%" + kw + "%"
		add("(account ILIKE ? OR device_id ILIKE ? OR city ILIKE ? OR province ILIKE ? OR isp ILIKE ? OR detail ILIKE ? OR rule_name ILIKE ?)",
			like, like, like, like, like, like, like)
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_ip_events WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+registerIPEventColumns+` FROM register_ip_events
WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []regip.Event{}
	for rows.Next() {
		item, err := scanRegisterIPEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// PurgeRegisterIPEvents 删除 before 之前的拦截记录。
func (r *Repository) PurgeRegisterIPEvents(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM register_ip_events WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

/* ───────────────────── 概览 ───────────────────── */

// RegisterIPStats 概览需要的聚合。
type RegisterIPStats struct {
	Registrations  int64
	DistinctIPs    int64
	DistinctSubnet int64
	// Trend 日期 → 类别 → 数量；类别是 registered 加上各记录类型
	Trend   map[string]map[string]int64
	Kinds   map[string]int64
	Reasons map[string]int64
	Methods map[string]int64
	Rules   []RegisterIPRuleStat
	// Distribution 每个地址累计注册的账号数分布：1 / 2 / 3–5 / 6–10 / 11+
	Distribution map[string]int64
	TopIPs       []RegisterIPTopItem
	TopSubnets   []RegisterIPTopItem
	TopBlocked   []RegisterIPTopItem
	TopCities    []RegisterIPTopItem
	Lists        map[string]int64
}

// RegisterIPRuleStat 规则在窗口内的命中次数。
type RegisterIPRuleStat struct {
	RuleID   string `json:"ruleId"`
	RuleName string `json:"ruleName"`
	Kind     string `json:"kind"`
	Count    int64  `json:"count"`
}

// RegisterIPTopItem 排行的一项。
type RegisterIPTopItem struct {
	Key      string    `json:"key"`
	Count    int64     `json:"count"`
	Accounts int64     `json:"accounts,omitempty"`
	Blocked  int64     `json:"blocked,omitempty"`
	Location string    `json:"location,omitempty"`
	LastAt   time.Time `json:"lastAt"`
}

// RegisterIPOverviewStats 概览聚合：since 之后的台账与记录。v4 / v6 是网段排行的前缀长度。
// 七条查询互不依赖，用一个 pgx.Batch 一次往返发完。
func (r *Repository) RegisterIPOverviewStats(ctx context.Context, appID int64, since time.Time, tz string, v4, v6, top int) (*RegisterIPStats, error) {
	stats := &RegisterIPStats{
		Trend: map[string]map[string]int64{}, Kinds: map[string]int64{}, Reasons: map[string]int64{}, Methods: map[string]int64{},
		Rules: []RegisterIPRuleStat{}, Distribution: map[string]int64{}, TopIPs: []RegisterIPTopItem{}, TopSubnets: []RegisterIPTopItem{},
		TopBlocked: []RegisterIPTopItem{}, TopCities: []RegisterIPTopItem{}, Lists: map[string]int64{},
	}
	bump := func(day, key string, count int64) {
		if stats.Trend[day] == nil {
			stats.Trend[day] = map[string]int64{}
		}
		stats.Trend[day][key] += count
	}
	// 网段表达式：v4 / v6 前缀长度占第 a、b 个参数
	subnetExpr := func(a, b int) string {
		return fmt.Sprintf(`network(set_masklen(ip, CASE WHEN family(ip) = 4 THEN $%d::int ELSE $%d::int END))::text`, a, b)
	}
	batch := &pgx.Batch{}
	batch.Queue(`SELECT COUNT(*), COUNT(DISTINCT ip), COUNT(DISTINCT `+subnetExpr(3, 4)+`)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2`, appID, since, v4, v6).
		QueryRow(func(row pgx.Row) error {
			return row.Scan(&stats.Registrations, &stats.DistinctIPs, &stats.DistinctSubnet)
		})
	batch.Queue(`SELECT to_char(date_trunc('day', created_at AT TIME ZONE $3), 'YYYY-MM-DD'), method, COUNT(*)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 GROUP BY 1, 2`, appID, since, tz).
		Query(func(rows pgx.Rows) error {
			for rows.Next() {
				var day, method string
				var count int64
				if err := rows.Scan(&day, &method, &count); err != nil {
					return err
				}
				bump(day, "registered", count)
				if method == "" {
					method = "unknown"
				}
				stats.Methods[method] += count
			}
			return rows.Err()
		})
	batch.Queue(`SELECT to_char(date_trunc('day', created_at AT TIME ZONE $3), 'YYYY-MM-DD'), kind, reason, rule_id, MAX(rule_name), COUNT(*)
FROM register_ip_events WHERE appid = $1 AND created_at >= $2 GROUP BY 1, 2, 3, 4`, appID, since, tz).
		Query(func(rows pgx.Rows) error {
			rules := map[string]int{}
			for rows.Next() {
				var day, kind, reason, ruleID, ruleName string
				var count int64
				if err := rows.Scan(&day, &kind, &reason, &ruleID, &ruleName, &count); err != nil {
					return err
				}
				bump(day, kind, count)
				stats.Kinds[kind] += count
				if reason != "" {
					stats.Reasons[reason] += count
				}
				if ruleID != "" {
					key := ruleID + "\x00" + kind
					index, ok := rules[key]
					if !ok {
						index = len(stats.Rules)
						rules[key] = index
						stats.Rules = append(stats.Rules, RegisterIPRuleStat{RuleID: ruleID, RuleName: ruleName, Kind: kind})
					}
					stats.Rules[index].Count += count
				}
			}
			return rows.Err()
		})
	batch.Queue(`SELECT bucket, COUNT(*) FROM (
    SELECT CASE WHEN c = 1 THEN '1' WHEN c = 2 THEN '2' WHEN c <= 5 THEN '3-5' WHEN c <= 10 THEN '6-10' ELSE '11+' END AS bucket
    FROM (SELECT COUNT(*) AS c FROM register_ip_ledger WHERE appid = $1 AND released_at IS NULL AND created_at >= $2 GROUP BY ip) per
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
	scanTop := func(dst *[]RegisterIPTopItem) func(pgx.Rows) error {
		return func(rows pgx.Rows) error {
			for rows.Next() {
				var item RegisterIPTopItem
				if err := rows.Scan(&item.Key, &item.Count, &item.Location, &item.LastAt); err != nil {
					return err
				}
				*dst = append(*dst, item)
			}
			return rows.Err()
		}
	}
	batch.Queue(`SELECT host(ip), COUNT(*), COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 GROUP BY ip ORDER BY 2 DESC, 4 DESC LIMIT $3`, appID, since, top).
		Query(scanTop(&stats.TopIPs))
	batch.Queue(`SELECT `+subnetExpr(4, 5)+`, COUNT(*), COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 GROUP BY 1 ORDER BY 2 DESC, 4 DESC LIMIT $3`, appID, since, top, v4, v6).
		Query(scanTop(&stats.TopSubnets))
	batch.Queue(`SELECT ip, COUNT(*), COALESCE(MAX(NULLIF(concat_ws('/', NULLIF(province, ''), NULLIF(city, '')), '')), ''), MAX(created_at)
FROM register_ip_events WHERE appid = $1 AND created_at >= $2 AND ip <> '' AND kind IN ('rejected', 'challenged', 'penalized')
GROUP BY ip ORDER BY 2 DESC, 4 DESC LIMIT $3`, appID, since, top).
		Query(scanTop(&stats.TopBlocked))
	batch.Queue(`SELECT concat_ws('/', country, NULLIF(province, ''), NULLIF(city, '')), COUNT(*), '', MAX(created_at)
FROM register_ip_ledger WHERE appid = $1 AND created_at >= $2 AND country <> '' GROUP BY 1 ORDER BY 2 DESC LIMIT $3`, appID, since, top).
		Query(scanTop(&stats.TopCities))
	batch.Queue(`SELECT kind, COUNT(*) FROM register_ip_lists WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW()) GROUP BY kind`, appID).
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

// RegisterIPLedgerByPrefix IP 查询用：网段内最近的注册（含已解除的），以及未解除的总数。
func (r *Repository) RegisterIPLedgerByPrefix(ctx context.Context, appID int64, prefix netip.Prefix, limit int) ([]regip.Registration, int64, error) {
	items, _, err := r.ListRegisterIPLedger(ctx, regip.RegistrationQuery{AppID: appID, IP: prefix.String(), Page: 1, PageSize: limit})
	if err != nil {
		return nil, 0, err
	}
	active, err := r.CountRegisterIPLedger(ctx, RegisterIPCountQuery{AppID: appID, Prefix: prefix})
	if err != nil {
		return nil, 0, err
	}
	return items, int64(active), nil
}
