// Package stream 流式特征（RisingWave）。
//
// 控制台上配置的每个流式特征编译成三个对象，名字带定义摘要（定义一改就是新对象，旧的删掉）：
//
//	rf_<键>_<摘要>_src  NATS JetStream 源，自己的持久消费者，从流的最早一条读起（新特征自动回填 7 天）
//	rf_<键>_<摘要>      物化视图：按主体分组聚合，时间过滤 ts + 窗口 > now() 让过窗的事件自动退出
//	rf_<键>_<摘要>_sink Redis Sink（UPSERT）：k → v 写进 Redis Cluster；分组被清空时键随之删除
//
// 决策服务读 rf:<键>:<应用或 g>:<主体取值>，与 engine.StreamKey 一致。
// SQL 全部由结构化定义生成，取值逐个转义，不拼接控制台上的原文。
package stream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"

	"github.com/jackc/pgx/v5"
)

// columns 事件里可用于过滤与聚合的列
var columns = map[string]string{
	"id": "varchar", "type": "varchar", "scene": "varchar", "ts": "bigint", "app_id": "bigint", "user_id": "bigint",
	"account": "varchar", "ip": "varchar", "ip_subnet": "varchar", "device_id": "varchar", "country": "varchar",
	"province": "varchar", "city": "varchar", "isp": "varchar", "asn": "bigint", "phone": "varchar", "phone_prefix": "varchar",
	"email_domain": "varchar", "amount": "double precision", "target_user_id": "bigint", "action": "varchar",
	"level": "varchar", "score": "int", "shadow": "boolean",
}

// Columns 可用列（控制台的字段下拉）
func Columns() []string {
	out := make([]string, 0, len(columns))
	for c := range columns {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func entityExpr(entity string) (expr, notEmpty string, err error) {
	switch entity {
	case bundle.EntityUser:
		return "user_id::varchar", "user_id > 0", nil
	case bundle.EntityAccount:
		return "lower(account)", "account <> ''", nil
	case bundle.EntityDevice:
		return "device_id", "device_id <> ''", nil
	case bundle.EntityIP:
		return "ip", "ip <> ''", nil
	case bundle.EntityIPSubnet:
		return "ip_subnet", "ip_subnet <> ''", nil
	case bundle.EntityPhone:
		return "phone", "phone <> ''", nil
	case bundle.EntityEmailDomain:
		return "email_domain", "email_domain <> ''", nil
	case bundle.EntityApp:
		return "app_id::varchar", "app_id > 0", nil
	}
	return "", "", fmt.Errorf("主体 %q 不能用于流式特征", entity)
}

// Literal SQL 字符串字面量
func Literal(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func literalFor(col string, v any) (string, error) {
	typ := columns[col]
	switch typ {
	case "varchar":
		return Literal(fmt.Sprint(v)), nil
	case "boolean":
		switch fmt.Sprint(v) {
		case "true", "1":
			return "true", nil
		case "false", "0":
			return "false", nil
		}
		return "", fmt.Errorf("%s 要布尔值", col)
	default:
		f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
		if err != nil {
			return "", fmt.Errorf("%s 要数值", col)
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
}

func condition(c bundle.Condition) (string, error) {
	if _, ok := columns[c.Field]; !ok {
		return "", fmt.Errorf("没有字段 %q", c.Field)
	}
	ops := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
	switch c.Op {
	case "in", "not_in":
		var items []any
		switch v := c.Value.(type) {
		case []any:
			items = v
		case []string:
			for _, s := range v {
				items = append(items, s)
			}
		default:
			for _, s := range strings.Split(fmt.Sprint(v), ",") {
				items = append(items, strings.TrimSpace(s))
			}
		}
		if len(items) == 0 {
			return "", fmt.Errorf("%s 的取值列表为空", c.Field)
		}
		lits := make([]string, len(items))
		for i, it := range items {
			l, err := literalFor(c.Field, it)
			if err != nil {
				return "", err
			}
			lits[i] = l
		}
		op := "IN"
		if c.Op == "not_in" {
			op = "NOT IN"
		}
		return fmt.Sprintf("%s %s (%s)", c.Field, op, strings.Join(lits, ", ")), nil
	}
	op, ok := ops[c.Op]
	if !ok {
		return "", fmt.Errorf("不支持的比较 %q", c.Op)
	}
	l, err := literalFor(c.Field, c.Value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", c.Field, op, l), nil
}

// Object 一个特征在 RisingWave 里的对象名
type Object struct {
	Feature string `json:"feature"`
	Base    string `json:"base"` // rf_<键>_<摘要>
}

// Source / View / Sink 三个对象名
func (o Object) Source() string { return o.Base + "_src" }
func (o Object) View() string   { return o.Base }
func (o Object) Sink() string   { return o.Base + "_sink" }

// ObjectFor 特征对应的对象名（定义摘要取前 10 位）
func ObjectFor(f bundle.Feature) Object {
	raw, _ := json.Marshal(struct {
		E string
		G bool
		S *bundle.StreamDef
	}{f.Entity, f.Global, f.Stream})
	sum := sha256.Sum256(raw)
	return Object{Feature: f.Key, Base: "rf_" + f.Key + "_" + hex.EncodeToString(sum[:])[:10]}
}

// Target 连接参数
type Target struct {
	NATSServer    string // risk 视角的 NATS 地址（host:port）
	NATSUser      string // 有值时用 user_and_password 连接（生产 NATS 带账户鉴权）
	NATSPassword  string
	RedisURLs     []string // redis://:密码@主机:端口
	StartupMode   string   // earliest（默认，回填）/ latest
	DurablePrefix string
}

// SQL 一个特征的建对象语句
func SQL(f bundle.Feature, t Target) ([]string, error) {
	if f.Kind != bundle.FeatureStream || f.Stream == nil {
		return nil, fmt.Errorf("%s 不是流式特征", f.Key)
	}
	def := f.Stream
	if def.WindowSeconds <= 0 || def.WindowSeconds > 90*86400 {
		return nil, fmt.Errorf("窗口要在 1 秒到 90 天之间")
	}
	ent, notEmpty, err := entityExpr(f.Entity)
	if err != nil {
		return nil, err
	}
	var agg string
	switch def.Agg {
	case bundle.AggCount, "":
		agg = "count(*)"
	case bundle.AggDistinct, bundle.AggSum, bundle.AggMax, bundle.AggAvg:
		if _, ok := columns[def.Field]; !ok {
			return nil, fmt.Errorf("聚合字段 %q 不存在", def.Field)
		}
		switch def.Agg {
		case bundle.AggDistinct:
			agg = "count(DISTINCT " + def.Field + ")"
		case bundle.AggSum:
			agg = "sum(" + def.Field + ")"
		case bundle.AggMax:
			agg = "max(" + def.Field + ")"
		case bundle.AggAvg:
			agg = "avg(" + def.Field + ")"
		}
	default:
		return nil, fmt.Errorf("不支持的聚合 %q", def.Agg)
	}
	if len(def.Events) == 0 {
		return nil, fmt.Errorf("至少选一种事件")
	}
	evLits := make([]string, len(def.Events))
	for i, e := range def.Events {
		evLits[i] = Literal(e)
	}
	where := []string{"type IN (" + strings.Join(evLits, ", ") + ")", notEmpty, "NOT coalesce(shadow, false)",
		fmt.Sprintf("to_timestamp(ts / 1000.0) + INTERVAL '%d seconds' > now()", def.WindowSeconds)}
	if len(def.Scenes) > 0 {
		sc := make([]string, len(def.Scenes))
		for i, s := range def.Scenes {
			sc[i] = Literal(s)
		}
		where = append(where, "scene IN ("+strings.Join(sc, ", ")+")")
	}
	for _, c := range def.Conditions {
		s, err := condition(c)
		if err != nil {
			return nil, err
		}
		where = append(where, s)
	}
	scope := "app_id::varchar"
	group := "app_id, " + ent
	if f.Global {
		scope = "'g'"
		group = ent
	}
	obj := ObjectFor(f)

	cols := make([]string, 0, len(columns))
	for _, name := range Columns() {
		cols = append(cols, name+" "+columns[name])
	}
	startup := t.StartupMode
	if startup == "" {
		startup = "earliest"
	}
	subjects := events.SubjectEventPrefix + ">," + events.SubjectDecisionPrefix + ">"
	auth := "connect_mode = 'plain'"
	if t.NATSUser != "" {
		auth = fmt.Sprintf("connect_mode = 'user_and_password', username = %s, password = %s", Literal(t.NATSUser), Literal(t.NATSPassword))
	}
	src := fmt.Sprintf(`CREATE SOURCE IF NOT EXISTS %s (%s) WITH (
  connector = 'nats', server_url = %s, subject = %s, stream = %s, %s,
  consumer.durable_name = %s, scan.startup.mode = %s
) FORMAT PLAIN ENCODE JSON`, obj.Source(), strings.Join(cols, ", "), Literal(t.NATSServer), Literal(subjects), Literal(events.StreamEvents), auth,
		Literal(t.DurablePrefix+obj.Base), Literal(startup))
	view := fmt.Sprintf(`CREATE MATERIALIZED VIEW IF NOT EXISTS %s AS
SELECT %s || ':' || %s || ':' || %s AS k, (%s)::double precision AS v
FROM %s WHERE %s GROUP BY %s`, obj.View(), Literal(f.Key), scope, ent, agg, obj.Source(), strings.Join(where, " AND "), group)
	urls, _ := json.Marshal(t.RedisURLs)
	sink := fmt.Sprintf(`CREATE SINK IF NOT EXISTS %s FROM %s WITH (
  connector = 'redis', primary_key = 'k', redis.url = %s
) FORMAT UPSERT ENCODE TEMPLATE (key_format = 'rf:{k}', value_format = '{v}')`, obj.Sink(), obj.View(), Literal(string(urls)))
	return []string{src, view, sink}, nil
}

// Manager 把一组流式特征同步到 RisingWave
type Manager struct {
	conn   *pgx.Conn
	target Target
	// OnDrop 特征对象删掉后回调持久消费者名：RisingWave 删源不会删 NATS 上的消费者，不删会一直积压
	OnDrop func(ctx context.Context, durable string)
}

// Connect 连 RisingWave（pgwire）
func Connect(ctx context.Context, dsn string, t Target) (*Manager, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if t.DurablePrefix == "" {
		t.DurablePrefix = "aegis_"
	}
	return &Manager{conn: conn, target: t}, nil
}

// Close 关闭
func (m *Manager) Close(ctx context.Context) { _ = m.conn.Close(ctx) }

// Existing RisingWave 里已有的特征对象（基名）
func (m *Manager) Existing(ctx context.Context) (map[string]bool, error) {
	rows, err := m.conn.Query(ctx, `SELECT name FROM rw_catalog.rw_materialized_views WHERE name LIKE 'rf\_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// SyncResult 同步结果
type SyncResult struct {
	Created []string          `json:"created"`
	Dropped []string          `json:"dropped"`
	Failed  map[string]string `json:"failed"`
}

// Sync 建出 features 里缺的、删掉不再需要的
func (m *Manager) Sync(ctx context.Context, features []bundle.Feature) (*SyncResult, error) {
	existing, err := m.Existing(ctx)
	if err != nil {
		return nil, err
	}
	res := &SyncResult{Failed: map[string]string{}}
	want := map[string]bool{}
	for _, f := range features {
		if f.Kind != bundle.FeatureStream {
			continue
		}
		obj := ObjectFor(f)
		want[obj.Base] = true
		if existing[obj.Base] {
			continue
		}
		stmts, err := SQL(f, m.target)
		if err != nil {
			res.Failed[f.Key] = err.Error()
			continue
		}
		var failed error
		for _, s := range stmts {
			if _, err := m.conn.Exec(ctx, s); err != nil {
				failed = err
				break
			}
		}
		if failed != nil {
			res.Failed[f.Key] = failed.Error()
			m.drop(ctx, obj.Base)
			continue
		}
		res.Created = append(res.Created, obj.Base)
	}
	for base := range existing {
		if !want[base] {
			m.drop(ctx, base)
			res.Dropped = append(res.Dropped, base)
		}
	}
	return res, nil
}

func (m *Manager) drop(ctx context.Context, base string) {
	o := Object{Base: base}
	_, _ = m.conn.Exec(ctx, "DROP SINK IF EXISTS "+o.Sink())
	_, _ = m.conn.Exec(ctx, "DROP MATERIALIZED VIEW IF EXISTS "+o.View())
	_, _ = m.conn.Exec(ctx, "DROP SOURCE IF EXISTS "+o.Source())
	if m.OnDrop != nil {
		m.OnDrop(ctx, m.target.DurablePrefix+base)
	}
}

// Preview 控制台上展示生成的 SQL
func Preview(f bundle.Feature, t Target) (string, error) {
	stmts, err := SQL(f, t)
	if err != nil {
		return "", err
	}
	// 预览里不露出 Redis 与 NATS 密码
	out := strings.Join(stmts, ";\n\n") + ";"
	if t.NATSPassword != "" {
		out = strings.ReplaceAll(out, Literal(t.NATSPassword), "'******'")
	}
	for _, u := range t.RedisURLs {
		if i := strings.Index(u, "@"); i > 0 {
			out = strings.ReplaceAll(out, u[:i], "redis://:******")
		}
	}
	return out, nil
}
