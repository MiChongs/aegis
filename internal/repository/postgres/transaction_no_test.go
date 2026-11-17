package postgres

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 批量发放会并发写各账本流水，流水号必须在高并发下不重复（曾撞 uq_integral_transactions_no）。
func TestGenerateTransactionNoConcurrentUnique(t *testing.T) {
	const (
		workers   = 64
		perWorker = 4000
	)
	results := make([][]string, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := make([]string, perWorker)
			for i := range out {
				out[i] = generateTransactionNo("INT")
			}
			results[w] = out
		}()
	}
	wg.Wait()

	seen := make(map[string]struct{}, workers*perWorker)
	for _, batch := range results {
		for _, no := range batch {
			if _, dup := seen[no]; dup {
				t.Fatalf("duplicate transaction no %s", no)
			}
			seen[no] = struct{}{}
		}
	}
}

func TestGenerateTransactionNoFormat(t *testing.T) {
	pattern := regexp.MustCompile(`^VIP(\d{14})(\d{6})$`)
	no := generateTransactionNo("VIP")
	m := pattern.FindStringSubmatch(no)
	if m == nil {
		t.Fatalf("unexpected format %q", no)
	}
	stamp, err := time.Parse("20060102150405", m[1])
	if err != nil {
		t.Fatalf("time part %q: %v", m[1], err)
	}
	if d := time.Since(stamp); d < -time.Second || d > time.Minute {
		t.Fatalf("time part %q is not current UTC", m[1])
	}
}

func TestRandomDigits(t *testing.T) {
	digits := regexp.MustCompile(`^\d{16}$`)
	seen := make(map[string]struct{})
	for range 1000 {
		v := randomDigits(16)
		if !digits.MatchString(v) {
			t.Fatalf("unexpected value %q", v)
		}
		seen[v] = struct{}{}
	}
	if len(seen) < 1000 {
		t.Fatalf("randomDigits repeated within 1000 draws: %d distinct", len(seen))
	}
}

// 多副本：分到节点槽位后，6 位序号 = 2 位槽位 + 4 位秒内序号。同一秒发满 1 万个号时向后借秒，
// 同一进程内绝不重复；不同槽位的号段互不相交，长度与「全是数字」的格式与单实例时一致。
func TestGenerateTransactionNoNodeScoped(t *testing.T) {
	prevNode, prevNow := transactionNoNode.Load(), transactionNoNow
	t.Cleanup(func() {
		transactionNoNode.Store(prevNode)
		transactionNoNow = prevNow
		transactionNoClock.Lock()
		transactionNoClock.sec, transactionNoClock.n = 0, 0
		transactionNoClock.Unlock()
	})
	frozen := time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)
	transactionNoNow = func() time.Time { return frozen }
	transactionNoClock.Lock()
	transactionNoClock.sec, transactionNoClock.n = 0, 0
	transactionNoClock.Unlock()

	pattern := regexp.MustCompile(`^INT(\d{14})(\d{2})(\d{4})$`)
	generate := func(node int32, count int) []string {
		transactionNoNode.Store(node)
		out := make([]string, count)
		for i := range out {
			out[i] = generateTransactionNo("INT")
		}
		return out
	}
	const burst = transactionNoPerSecond + 5 // 同一秒内发超一万个，必须借秒
	nodeA := generate(7, burst)
	seen := map[string]struct{}{}
	for _, no := range nodeA {
		m := pattern.FindStringSubmatch(no)
		if m == nil {
			t.Fatalf("格式不对：%q", no)
		}
		if m[2] != "07" {
			t.Fatalf("槽位应为 07：%q", no)
		}
		if _, dup := seen[no]; dup {
			t.Fatalf("同一进程内重复：%q", no)
		}
		seen[no] = struct{}{}
	}
	if !strings.HasPrefix(nodeA[transactionNoPerSecond], "INT20261117080001") {
		t.Fatalf("第一万零一个号应借到下一秒，实得 %q", nodeA[transactionNoPerSecond])
	}
	if len(nodeA[0]) != len("INT")+14+6 {
		t.Fatalf("长度应与单实例一致：%q", nodeA[0])
	}

	// 另一个副本（另一个槽位）在同一秒发号：与上面全无交集
	transactionNoClock.Lock()
	transactionNoClock.sec, transactionNoClock.n = 0, 0
	transactionNoClock.Unlock()
	for _, no := range generate(8, 100) {
		if _, dup := seen[no]; dup {
			t.Fatalf("不同槽位撞号：%q", no)
		}
		seen[no] = struct{}{}
	}

	// 时钟往回调：逻辑秒不回退，不会重复发号
	frozen = frozen.Add(-time.Hour)
	for _, no := range generate(8, 10) {
		if _, dup := seen[no]; dup {
			t.Fatalf("时钟回调后撞号：%q", no)
		}
		seen[no] = struct{}{}
	}

	// 没分到槽位：回到单实例的 6 位序号
	transactionNoNode.Store(-1)
	transactionNoNow = prevNow
	if !regexp.MustCompile(`^INT\d{20}$`).MatchString(generateTransactionNo("INT")) {
		t.Fatal("单实例格式变了")
	}
}

// ReserveTransactionNoNode：两个进程（两次调用）拿到不同槽位，之后发号即进入节点模式。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/repository/postgres -run TestReserveTransactionNoNode
func TestReserveTransactionNoNode(t *testing.T) {
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
	if _, err := pool.Exec(ctx, `CREATE SEQUENCE IF NOT EXISTS transaction_no_node_seq`); err != nil {
		t.Fatal(err)
	}
	prev := transactionNoNode.Load()
	t.Cleanup(func() { transactionNoNode.Store(prev) })

	repo := New(pool)
	first, err := repo.ReserveTransactionNoNode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.ReserveTransactionNoNode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first < 0 || first >= transactionNoNodeSlots || second < 0 || second >= transactionNoNodeSlots {
		t.Fatalf("两次应拿到不同的合法槽位：%d %d", first, second)
	}
	if got := generateTransactionNo("VIP"); got[3+14:3+16] != fmt.Sprintf("%02d", second) {
		t.Fatalf("发号应带上当前槽位 %02d：%q", second, got)
	}
	if _, err := New(nil).ReserveTransactionNoNode(ctx); err == nil {
		t.Fatal("没有连接池时应报错")
	}
}
