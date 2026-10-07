package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// openTestDatabase 连接 AEGIS_TEST_PG_DSN 指向的空库；未设置时跳过测试。
func openTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetTestDatabase(t, ctx, pool)
	return ctx, pool
}

// resetTestDatabase 清空库并跑全部迁移两遍（验证可重复执行）。
// 便携版 Postgres 不带 pgvector / PostGIS：AI 与地理模块的迁移与这里的测试无关，跳过。
func resetTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../migrations/postgres/*.up.sql")
	sort.Strings(files)
	for round := 0; round < 2; round++ {
		for _, file := range files {
			content, _ := os.ReadFile(file)
			sql := strings.TrimSpace(string(content))
			if sql == "" {
				continue
			}
			if _, err := pool.Exec(ctx, sql); err != nil {
				if msg := err.Error(); strings.Contains(msg, "vector") || strings.Contains(msg, "postgis") ||
					strings.Contains(msg, "geography") || strings.Contains(msg, "geometry") {
					continue
				}
				t.Fatalf("round %d apply %s: %v", round+1, filepath.Base(file), err)
			}
		}
	}
}
