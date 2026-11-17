package cluster

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// memLeases 用内存模拟 cluster_leases 表，时钟可拨。
type memLeases struct {
	mu   sync.Mutex
	rows map[string]memLease
	now  func() time.Time
	fail bool
}

type memLease struct {
	holder  string
	expires time.Time
}

func (m *memLeases) ensure(context.Context) error { return nil }

func (m *memLeases) acquire(_ context.Context, name, holder string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return false, errors.New("db down")
	}
	now := m.now()
	row, ok := m.rows[name]
	if !ok || row.holder == holder || row.expires.Before(now) {
		m.rows[name] = memLease{holder: holder, expires: now.Add(ttl)}
		return true, nil
	}
	return false, nil
}

func (m *memLeases) release(_ context.Context, names []string, holder string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, name := range names {
		if m.rows[name].holder == holder {
			delete(m.rows, name)
		}
	}
	return nil
}

func TestElectorOnlyOneHolder(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	now := func() time.Time { return clock }
	store := &memLeases{rows: map[string]memLease{}, now: now}

	a := newElector(store, "a", 30*time.Second, nil)
	b := newElector(store, "b", 30*time.Second, nil)
	a.now, b.now = now, now
	gateA := a.Gate("payment.poll")
	gateB := b.Gate("payment.poll")

	a.tick(context.Background())
	b.tick(context.Background())
	if !gateA() || gateB() {
		t.Fatalf("want only a holding, got a=%v b=%v", gateA(), gateB())
	}

	// a 停止续约（宕机）：租约过期后 b 接手。
	clock = clock.Add(31 * time.Second)
	if gateA() {
		t.Fatalf("a should consider its lease expired locally")
	}
	b.tick(context.Background())
	if !gateB() {
		t.Fatalf("b should take over after expiry")
	}
}

func TestElectorKeepsLeaseThroughBriefDBOutage(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	now := func() time.Time { return clock }
	store := &memLeases{rows: map[string]memLease{}, now: now}
	a := newElector(store, "a", 30*time.Second, nil)
	a.now = now
	gate := a.Gate("monitor")
	a.tick(context.Background())

	store.fail = true
	clock = clock.Add(10 * time.Second)
	a.tick(context.Background())
	if !gate() {
		t.Fatalf("lease should survive a short outage")
	}
	clock = clock.Add(20 * time.Second) // 超过本地期限（TTL - TTL/6 = 25s）
	if gate() {
		t.Fatalf("lease must be dropped locally once the local deadline passes")
	}
}

func TestNilClusterAlwaysAllows(t *testing.T) {
	var c *Cluster
	if !c.Gate("x")() {
		t.Fatal("nil cluster must behave as single instance")
	}
	var e *Elector
	if !e.Gate("x")() || !e.Holding("x") {
		t.Fatal("nil elector must allow")
	}
	c.Publish("t", nil) // 不得 panic
	c.Subscribe("t", func([]byte) {})
}

func TestStopReleases(t *testing.T) {
	store := &memLeases{rows: map[string]memLease{}, now: time.Now}
	a := newElector(store, "a", 30*time.Second, nil)
	a.Gate("x")
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.Holding("x") {
		t.Fatal("first tick in Start should acquire")
	}
	a.Stop(context.Background())
	if _, ok := store.rows["x"]; ok {
		t.Fatal("stop should release lease")
	}
}
