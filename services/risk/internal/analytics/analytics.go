// Package analytics 风控流水与分析（ClickHouse）。
//
// 表都是 ReplacingMergeTree，排序键里带事件 ID：JetStream 至少一次投递，重放的同一条事件会被合并掉
// （查询时用 FINAL 或按 id 去重的聚合，看板用的分钟汇总表接受合并前的短暂重复）。
package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/config"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Schema 建表语句（IF NOT EXISTS，可重复执行）
func Schema() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS decisions (
			id String,
			ts DateTime64(3, 'UTC'),
			app_id Int64,
			scene LowCardinality(String),
			user_id Int64,
			account String,
			ip String,
			ip_subnet String,
			device_id String,
			ua String,
			country LowCardinality(String),
			province LowCardinality(String),
			city LowCardinality(String),
			isp LowCardinality(String),
			asn UInt32,
			action LowCardinality(String),
			level LowCardinality(String),
			score Int32,
			release LowCardinality(String),
			hits Array(LowCardinality(String)),
			reasons Array(String),
			latency_us Int64,
			features Map(LowCardinality(String), Float64),
			models Map(LowCardinality(String), Float64),
			shadow UInt8,
			props String CODEC(ZSTD(3)),
			trace String CODEC(ZSTD(6)),
			ingested_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_user user_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_device device_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_ip ip TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_account account TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_id id TYPE bloom_filter(0.001) GRANULARITY 4
		) ENGINE = ReplacingMergeTree(ingested_at)
		PARTITION BY toYYYYMM(ts)
		ORDER BY (app_id, scene, ts, id)
		TTL toDateTime(ts) + INTERVAL 180 DAY`,

		`CREATE TABLE IF NOT EXISTS events (
			id String,
			ts DateTime64(3, 'UTC'),
			type LowCardinality(String),
			scene LowCardinality(String),
			app_id Int64,
			user_id Int64,
			account String,
			ip String,
			ip_subnet String,
			device_id String,
			country LowCardinality(String),
			province LowCardinality(String),
			city LowCardinality(String),
			amount Float64,
			target_user_id Int64,
			props String CODEC(ZSTD(3)),
			ingested_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_user user_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_device device_id TYPE bloom_filter(0.01) GRANULARITY 4,
			INDEX idx_ip ip TYPE bloom_filter(0.01) GRANULARITY 4
		) ENGINE = ReplacingMergeTree(ingested_at)
		PARTITION BY toYYYYMM(ts)
		ORDER BY (app_id, type, ts, id)
		TTL toDateTime(ts) + INTERVAL 365 DAY`,

		`CREATE TABLE IF NOT EXISTS alerts (
			id String,
			ts DateTime64(3, 'UTC'),
			app_id Int64,
			pattern LowCardinality(String),
			pattern_name String,
			entity LowCardinality(String),
			value String,
			user_id Int64,
			device_id String,
			ip String,
			severity LowCardinality(String),
			event_ids Array(String),
			detail String CODEC(ZSTD(3)),
			ingested_at DateTime64(3, 'UTC') DEFAULT now64(3),
			INDEX idx_user user_id TYPE bloom_filter(0.01) GRANULARITY 4
		) ENGINE = ReplacingMergeTree(ingested_at)
		PARTITION BY toYYYYMM(ts)
		ORDER BY (app_id, pattern, ts, id)
		TTL toDateTime(ts) + INTERVAL 365 DAY`,

		`CREATE TABLE IF NOT EXISTS commands (
			id String,
			ts DateTime64(3, 'UTC'),
			kind LowCardinality(String),
			target LowCardinality(String),
			value String,
			app_id Int64,
			duration_seconds Int64,
			source LowCardinality(String),
			decision_id String,
			reason String,
			operator String,
			status LowCardinality(String),
			detail String,
			ingested_at DateTime64(3, 'UTC') DEFAULT now64(3)
		) ENGINE = ReplacingMergeTree(ingested_at)
		PARTITION BY toYYYYMM(ts)
		ORDER BY (app_id, ts, id)
		TTL toDateTime(ts) + INTERVAL 730 DAY`,

		// 设备指纹（JSON），用户画像里的相似设备检索要用
		`ALTER TABLE decisions ADD COLUMN IF NOT EXISTS device String CODEC(ZSTD(3)) AFTER trace`,

		// 看板：按分钟的决策汇总
		`CREATE TABLE IF NOT EXISTS decisions_minute (
			minute DateTime('UTC'),
			app_id Int64,
			scene LowCardinality(String),
			action LowCardinality(String),
			shadow UInt8,
			n UInt64,
			score_sum Int64,
			latency_sum Int64
		) ENGINE = SummingMergeTree
		PARTITION BY toYYYYMM(minute)
		ORDER BY (app_id, scene, action, shadow, minute)
		TTL minute + INTERVAL 400 DAY`,

		`CREATE MATERIALIZED VIEW IF NOT EXISTS decisions_minute_mv TO decisions_minute AS
		SELECT toStartOfMinute(ts) AS minute, app_id, scene, action, shadow,
		       count() AS n, sum(toInt64(score)) AS score_sum, sum(latency_us) AS latency_sum
		FROM decisions GROUP BY minute, app_id, scene, action, shadow`,

		// 看板：规则命中按小时
		`CREATE TABLE IF NOT EXISTS rule_hits_hour (
			hour DateTime('UTC'),
			app_id Int64,
			scene LowCardinality(String),
			rule LowCardinality(String),
			n UInt64
		) ENGINE = SummingMergeTree
		PARTITION BY toYYYYMM(hour)
		ORDER BY (app_id, scene, rule, hour)
		TTL hour + INTERVAL 400 DAY`,

		`CREATE MATERIALIZED VIEW IF NOT EXISTS rule_hits_hour_mv TO rule_hits_hour AS
		SELECT toStartOfHour(ts) AS hour, app_id, scene, arrayJoin(hits) AS rule, count() AS n
		FROM decisions WHERE shadow = 0 GROUP BY hour, app_id, scene, rule`,
	}
}

// Client ClickHouse
type Client struct {
	conn driver.Conn
}

// Open 连接
func Open(cfg config.Config) (*Client, error) {
	opts := &clickhouse.Options{
		Addr:            []string{cfg.ClickHouseAddr},
		Auth:            clickhouse.Auth{Database: cfg.ClickHouseDB, Username: cfg.ClickHouseUser, Password: cfg.ClickHousePassword},
		DialTimeout:     5 * time.Second,
		ReadTimeout:     30 * time.Second,
		MaxOpenConns:    16,
		MaxIdleConns:    4,
		ConnMaxLifetime: time.Hour,
		Compression:     &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		Settings:        clickhouse.Settings{"max_execution_time": 30},
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		return nil, err
	}
	return &Client{conn: conn}, nil
}

// Conn 底层连接（查询用）
func (c *Client) Conn() driver.Conn { return c.conn }

// Close 关闭
func (c *Client) Close() error { return c.conn.Close() }

// Migrate 建表
func (c *Client) Migrate(ctx context.Context) error {
	for _, stmt := range Schema() {
		if err := c.conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("clickhouse 建表失败：%w\n%s", err, stmt)
		}
	}
	return nil
}

func ms(ts int64) time.Time { return time.UnixMilli(ts).UTC() }

func propsJSON(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	raw, _ := json.Marshal(p)
	return string(raw)
}

func deviceJSON(d map[string]string) string {
	if len(d) == 0 {
		return ""
	}
	raw, _ := json.Marshal(d)
	return string(raw)
}

func strs(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func floats(v map[string]float64) map[string]float64 {
	if v == nil {
		return map[string]float64{}
	}
	return v
}

// WriteDecisions 批量写决策流水
func (c *Client) WriteDecisions(ctx context.Context, evs []events.Event) error {
	if len(evs) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO decisions (id, ts, app_id, scene, user_id, account, ip, ip_subnet, device_id, ua, country, province, city, isp, asn, action, level, score, release, hits, reasons, latency_us, features, models, shadow, props, trace, device)`)
	if err != nil {
		return err
	}
	for _, e := range evs {
		shadow := uint8(0)
		if e.Shadow {
			shadow = 1
		}
		if err := b.Append(e.ID, ms(e.TS), e.AppID, e.Scene, e.UserID, e.Account, e.IP, e.IPSubnet, e.DeviceID, e.UA,
			e.Country, e.Province, e.City, e.ISP, uint32(e.ASN), e.Action, e.Level, int32(e.Score), e.Release,
			strs(e.Hits), strs(e.Reasons), e.Latency, floats(e.Features), floats(e.Models), shadow, propsJSON(e.Props), e.Trace, deviceJSON(e.Device)); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// WriteEvents 批量写业务事件
func (c *Client) WriteEvents(ctx context.Context, evs []events.Event) error {
	if len(evs) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO events (id, ts, type, scene, app_id, user_id, account, ip, ip_subnet, device_id, country, province, city, amount, target_user_id, props)`)
	if err != nil {
		return err
	}
	for _, e := range evs {
		if err := b.Append(e.ID, ms(e.TS), e.Type, e.Scene, e.AppID, e.UserID, e.Account, e.IP, e.IPSubnet, e.DeviceID,
			e.Country, e.Province, e.City, e.Amount, e.TargetUserID, propsJSON(e.Props)); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// Alert CEP 告警
type Alert struct {
	ID          string         `json:"id"`
	TS          int64          `json:"ts"`
	AppID       int64          `json:"app_id"`
	Pattern     string         `json:"pattern"`
	PatternName string         `json:"pattern_name"`
	Entity      string         `json:"entity"`
	Value       string         `json:"value"`
	UserID      int64          `json:"user_id"`
	DeviceID    string         `json:"device_id"`
	IP          string         `json:"ip"`
	Severity    string         `json:"severity"`
	EventIDs    []string       `json:"event_ids"`
	Detail      map[string]any `json:"detail"`
}

// WriteAlerts 批量写告警
func (c *Client) WriteAlerts(ctx context.Context, as []Alert) error {
	if len(as) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO alerts (id, ts, app_id, pattern, pattern_name, entity, value, user_id, device_id, ip, severity, event_ids, detail)`)
	if err != nil {
		return err
	}
	for _, a := range as {
		if err := b.Append(a.ID, ms(a.TS), a.AppID, a.Pattern, a.PatternName, a.Entity, a.Value, a.UserID, a.DeviceID, a.IP,
			a.Severity, strs(a.EventIDs), propsJSON(a.Detail)); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// CommandRecord 处置记录
type CommandRecord struct {
	events.Command
	Status string
	Detail string
}

// WriteCommands 批量写处置记录
func (c *Client) WriteCommands(ctx context.Context, cs []CommandRecord) error {
	if len(cs) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO commands (id, ts, kind, target, value, app_id, duration_seconds, source, decision_id, reason, operator, status, detail)`)
	if err != nil {
		return err
	}
	for _, r := range cs {
		if err := b.Append(r.ID, ms(r.TS), r.Kind, r.Target, r.Value, r.AppID, r.DurationSeconds, r.Source, r.DecisionID, r.Reason, r.Operator, r.Status, r.Detail); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}
