// Package store 风控配置与名单的权威存储（PostgreSQL 独立库 aegis_risk，risk-admin 与 risk-ingest 共用）。
package store

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrations 只追加，不改已有条目
var migrations = []string{
	// 1：全部基础表
	`
CREATE TABLE IF NOT EXISTS scenes (
	key text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	app_ids bigint[] NOT NULL DEFAULT '{}',
	fields jsonb NOT NULL DEFAULT '[]',
	timeout_ms int NOT NULL DEFAULT 200,
	fail_mode text NOT NULL DEFAULT 'open',
	captcha_supported boolean NOT NULL DEFAULT false,
	default_action text NOT NULL DEFAULT '',
	enforcement jsonb NOT NULL DEFAULT '{}',
	list_types text[] NOT NULL DEFAULT '{}',
	features text[] NOT NULL DEFAULT '{}',
	models text[] NOT NULL DEFAULT '{}',
	bands jsonb NOT NULL DEFAULT '[]',
	flow jsonb,
	policy text NOT NULL DEFAULT '',
	builtin boolean NOT NULL DEFAULT false,
	enabled boolean NOT NULL DEFAULT true,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS features (
	key text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	kind text NOT NULL,
	entity text NOT NULL DEFAULT '',
	global boolean NOT NULL DEFAULT false,
	default_value double precision NOT NULL DEFAULT 0,
	def jsonb NOT NULL DEFAULT '{}',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS rules (
	id bigserial PRIMARY KEY,
	scene text NOT NULL,
	key text NOT NULL,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	dimension text NOT NULL DEFAULT 'custom',
	expr text NOT NULL,
	score int NOT NULL DEFAULT 0,
	mode text NOT NULL DEFAULT 'enforce',
	decision text NOT NULL DEFAULT '',
	priority int NOT NULL DEFAULT 0,
	reason text NOT NULL DEFAULT '',
	tags text[] NOT NULL DEFAULT '{}',
	enabled boolean NOT NULL DEFAULT true,
	template_key text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT '',
	UNIQUE (scene, key)
);

CREATE TABLE IF NOT EXISTS models (
	key text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	format text NOT NULL,
	object_key text NOT NULL,
	checksum text NOT NULL,
	inputs text[] NOT NULL DEFAULT '{}',
	transform boolean NOT NULL DEFAULT true,
	n_features int NOT NULL DEFAULT 0,
	size_bytes bigint NOT NULL DEFAULT 0,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS list_entries (
	id text PRIMARY KEY,
	list_type text NOT NULL,
	value text NOT NULL,
	kind text NOT NULL,
	app_id bigint NOT NULL DEFAULT 0,
	scenes text[] NOT NULL DEFAULT '{}',
	reason text NOT NULL DEFAULT '',
	source text NOT NULL DEFAULT 'manual',
	created_by text NOT NULL DEFAULT '',
	decision_id text NOT NULL DEFAULT '',
	expires_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT now(),
	removed_at timestamptz,
	removed_by text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS list_entries_value ON list_entries (list_type, value) WHERE removed_at IS NULL;
CREATE INDEX IF NOT EXISTS list_entries_created ON list_entries (created_at DESC) WHERE removed_at IS NULL;
CREATE INDEX IF NOT EXISTS list_entries_expiry ON list_entries (expires_at) WHERE removed_at IS NULL AND expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS cep_patterns (
	key text PRIMARY KEY,
	name text NOT NULL,
	description text NOT NULL DEFAULT '',
	enabled boolean NOT NULL DEFAULT true,
	key_by text NOT NULL,
	app_ids bigint[] NOT NULL DEFAULT '{}',
	steps jsonb NOT NULL,
	within_seconds int NOT NULL,
	severity text NOT NULL DEFAULT 'medium',
	actions jsonb NOT NULL DEFAULT '[]',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS releases (
	id bigserial PRIMARY KEY,
	scene text NOT NULL,
	version text NOT NULL,
	bundle jsonb NOT NULL,
	checksum text NOT NULL,
	note text NOT NULL DEFAULT '',
	published_by text NOT NULL DEFAULT '',
	published_at timestamptz NOT NULL DEFAULT now(),
	UNIQUE (scene, version)
);

CREATE TABLE IF NOT EXISTS release_state (
	scene text PRIMARY KEY,
	primary_id bigint REFERENCES releases(id),
	candidate_id bigint REFERENCES releases(id),
	canary_percent int NOT NULL DEFAULT 0,
	shadow boolean NOT NULL DEFAULT false,
	updated_at timestamptz NOT NULL DEFAULT now(),
	updated_by text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS cases (
	id bigserial PRIMARY KEY,
	decision_id text NOT NULL UNIQUE,
	app_id bigint NOT NULL,
	scene text NOT NULL,
	user_id bigint NOT NULL DEFAULT 0,
	account text NOT NULL DEFAULT '',
	ip text NOT NULL DEFAULT '',
	device_id text NOT NULL DEFAULT '',
	action text NOT NULL,
	score int NOT NULL DEFAULT 0,
	reasons text[] NOT NULL DEFAULT '{}',
	source text NOT NULL DEFAULT 'decision',
	status text NOT NULL DEFAULT 'pending',
	verdict text NOT NULL DEFAULT '',
	reviewer text NOT NULL DEFAULT '',
	note text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	reviewed_at timestamptz
);
CREATE INDEX IF NOT EXISTS cases_pending ON cases (created_at DESC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS cases_user ON cases (app_id, user_id);

CREATE TABLE IF NOT EXISTS processed_commands (
	id text PRIMARY KEY,
	at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS audit_log (
	id bigserial PRIMARY KEY,
	operator text NOT NULL DEFAULT '',
	action text NOT NULL,
	target text NOT NULL DEFAULT '',
	detail jsonb NOT NULL DEFAULT '{}',
	at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_log_at ON audit_log (at DESC);
`,
	// 2：关系图回灌记录
	`
CREATE TABLE IF NOT EXISTS graph_backfills (
	id bigserial PRIMARY KEY,
	operator text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'running',
	progress jsonb NOT NULL DEFAULT '{}',
	started_at timestamptz NOT NULL DEFAULT now(),
	finished_at timestamptz
);
`,
}

// EnsureDatabase 连主库建 aegis_risk 库（已存在则跳过）
func EnsureDatabase(ctx context.Context, adminDSN, dsn string) error {
	if adminDSN == "" {
		return nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	name := strings.TrimPrefix(u.Path, "/")
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	return err
}

// Migrate 按版本执行迁移（咨询锁保证多实例同时启动只跑一次）
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(7428401)"); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(7428401)")
	if _, err := conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	var current int
	if err := conn.QueryRow(ctx, "SELECT coalesce(max(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	for i := current; i < len(migrations); i++ {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, migrations[i]); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("迁移 %d：%w", i+1, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", i+1); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
