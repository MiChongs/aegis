package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 需要真实 Postgres：AEGIS_TEST_PG_DSN=postgres://... go test ./internal/bootstrap/ -run PG
func pgTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AEGIS_TEST_PG_DSN 未设置")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `DROP TABLE IF EXISTS schema_migrations, mig_counter, mig_t1, mig_t2`)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func writeMigrations(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	target := filepath.Join(dir, "migrations", "postgres")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(target, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestPGMigrationsConcurrentReplicasApplyOnce(t *testing.T) {
	pool := pgTestPool(t)
	dir := t.TempDir()
	chdir(t, dir)
	// 第二个文件故意不幂等：并发重复执行会让计数大于 1
	writeMigrations(t, dir, map[string]string{
		"000001_counter.up.sql": `CREATE TABLE IF NOT EXISTS mig_counter (n INT NOT NULL)`,
		"000002_bump.up.sql":    `INSERT INTO mig_counter VALUES (1)`,
	})
	// 先模拟「已有库首次接入」：表是空的，全部执行一遍并记账
	if _, err := applyMigrations(context.Background(), pool, nil, true, nil); err != nil {
		t.Fatal(err)
	}
	writeMigrations(t, dir, map[string]string{
		"000003_t1.up.sql":   `CREATE TABLE mig_t1 (id INT)`,
		"000004_bump.up.sql": `INSERT INTO mig_counter VALUES (1)`,
	})
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := applyMigrations(context.Background(), pool, nil, true, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migrate failed: %v", err)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM mig_counter`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("non-idempotent migrations ran %d times in total, want 2", n)
	}
}

func TestPGMigrationsFirstAdoptionAndStrict(t *testing.T) {
	pool := pgTestPool(t)
	dir := t.TempDir()
	chdir(t, dir)
	// 模拟线上已有表：首次接入时重跑会报「已存在」，应按已应用记账而不是失败
	if _, err := pool.Exec(context.Background(), `CREATE TABLE mig_t2 (id INT)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS mig_t2`) })
	writeMigrations(t, dir, map[string]string{"000001_t2.up.sql": `CREATE TABLE mig_t2 (id INT)`})
	report, err := applyMigrations(context.Background(), pool, nil, true, nil)
	if err != nil {
		t.Fatalf("first adoption must tolerate already-applied files: %v", err)
	}
	if len(report.Assumed) != 1 {
		t.Fatalf("assumed = %v", report.Assumed)
	}
	// 之后的新迁移失败，strict 必须报错
	writeMigrations(t, dir, map[string]string{"000002_bad.up.sql": `SELECT * FROM no_such_table`})
	if _, err := applyMigrations(context.Background(), pool, nil, true, nil); err == nil {
		t.Fatal("strict mode should fail on a broken new migration")
	}
	// 非 strict（启动时自动迁移）只记日志
	report, err = applyMigrations(context.Background(), pool, nil, false, nil)
	if err != nil || len(report.Failed) != 1 {
		t.Fatalf("lenient mode: err=%v failed=%v", err, report.Failed)
	}
}
