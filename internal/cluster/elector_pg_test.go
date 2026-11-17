package cluster

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGElectorSingleHolderAndFailover(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AEGIS_TEST_PG_DSN 未设置")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS cluster_leases`)

	ttl := 3 * time.Second
	a := NewElector(pool, "node-a", ttl, nil)
	b := NewElector(pool, "node-b", ttl, nil)
	ga, gb := a.Gate("job"), b.Gate("job")
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ga() || gb() {
		t.Fatalf("want a only: a=%v b=%v", ga(), gb())
	}
	// a 正常下线会释放租约，b 在下一轮续约（TTL/3）内接手
	a.Stop(context.Background())
	deadline := time.Now().Add(ttl)
	for !gb() && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !gb() {
		t.Fatal("b did not take over after a released")
	}
	b.Stop(context.Background())
}
