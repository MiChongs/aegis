package service

import (
	"context"
	"testing"
	"time"

	"aegis/internal/config"
	redisrepo "aegis/internal/repository/redis"
	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newLoginGuardForTest(t *testing.T) (*LoginGuardService, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	repo := redisrepo.NewLoginGuardRepository(client, "test")
	svc := NewLoginGuardService(config.LoginGuardConfig{
		Enabled:          true,
		Window:           time.Minute,
		AccountThreshold: 3,
		IPThreshold:      5,
		BaseLockDuration: 4 * time.Minute,
		MaxLockDuration:  time.Hour,
	}, zap.NewNop(), repo, nil)
	return svc, mr
}

func TestLoginGuardLocksAccountAfterThreshold(t *testing.T) {
	svc, _ := newLoginGuardForTest(t)
	ctx := context.Background()
	const appID = int64(10000)

	// 阈值内不锁
	svc.RegisterFailure(ctx, appID, "alice", "1.2.3.4")
	svc.RegisterFailure(ctx, appID, "alice", "1.2.3.4")
	if err := svc.Check(ctx, appID, "alice", "1.2.3.4"); err != nil {
		t.Fatalf("阈值内不应锁定: %v", err)
	}

	// 第 3 次失败触发账号锁
	svc.RegisterFailure(ctx, appID, "alice", "1.2.3.4")
	if err := svc.Check(ctx, appID, "alice", "9.9.9.9"); err == nil {
		t.Fatal("账号应已锁定")
	}
	// 其他账号不受影响（IP 阈值 5 尚未到）
	if err := svc.Check(ctx, appID, "bob", "1.2.3.4"); err != nil {
		t.Fatalf("其他账号不应被锁: %v", err)
	}
}

func TestLoginGuardAccountCaseInsensitive(t *testing.T) {
	svc, _ := newLoginGuardForTest(t)
	ctx := context.Background()
	const appID = int64(10000)

	svc.RegisterFailure(ctx, appID, "Alice@Example.com", "1.1.1.1")
	svc.RegisterFailure(ctx, appID, "alice@example.com", "1.1.1.1")
	svc.RegisterFailure(ctx, appID, "ALICE@EXAMPLE.COM", "1.1.1.1")
	if err := svc.Check(ctx, appID, "alice@example.com", "8.8.8.8"); err == nil {
		t.Fatal("大小写变体应共享同一计数器并触发锁定")
	}
}

func TestLoginGuardLocksIPAfterThreshold(t *testing.T) {
	svc, _ := newLoginGuardForTest(t)
	ctx := context.Background()
	const appID = int64(10000)

	// 同一 IP 撞 5 个不同账号
	for _, acct := range []string{"u1", "u2", "u3", "u4", "u5"} {
		svc.RegisterFailure(ctx, appID, acct, "6.6.6.6")
	}
	if err := svc.Check(ctx, appID, "fresh-account", "6.6.6.6"); err == nil {
		t.Fatal("IP 应已锁定（撞库防护）")
	}
	if err := svc.Check(ctx, appID, "fresh-account", "7.7.7.7"); err != nil {
		t.Fatalf("其他 IP 不应被锁: %v", err)
	}
}

func TestLoginGuardExponentialBackoff(t *testing.T) {
	svc, mr := newLoginGuardForTest(t)
	ctx := context.Background()
	const appID = int64(10000)

	trigger := func() {
		for i := 0; i < 3; i++ {
			svc.RegisterFailure(ctx, appID, "carol", "2.2.2.2")
		}
	}

	// 第一次锁定：base = 4m
	trigger()
	first, err := svc.redis.LockRemaining(ctx, loginGuardKindAccount, appID, "carol")
	if err != nil || first <= 0 {
		t.Fatalf("应存在账号锁: ttl=%v err=%v", first, err)
	}

	// 锁过期后再次触发 → 时长翻倍
	mr.FastForward(first + time.Second)
	trigger()
	second, err := svc.redis.LockRemaining(ctx, loginGuardKindAccount, appID, "carol")
	if err != nil || second <= 0 {
		t.Fatalf("应存在第二次账号锁: ttl=%v err=%v", second, err)
	}
	if second <= first {
		t.Fatalf("第二次锁定应更长（指数退避）: first=%v second=%v", first, second)
	}
}

func TestLoginGuardSuccessClearsCounters(t *testing.T) {
	svc, _ := newLoginGuardForTest(t)
	ctx := context.Background()
	const appID = int64(10000)

	svc.RegisterFailure(ctx, appID, "dave", "3.3.3.3")
	svc.RegisterFailure(ctx, appID, "dave", "3.3.3.3")
	svc.RegisterSuccess(ctx, appID, "dave", "3.3.3.3")
	// 计数清零后再失败 2 次不应触发（阈值 3）
	svc.RegisterFailure(ctx, appID, "dave", "3.3.3.3")
	svc.RegisterFailure(ctx, appID, "dave", "3.3.3.3")
	if err := svc.Check(ctx, appID, "dave", "3.3.3.3"); err != nil {
		t.Fatalf("成功登录应重置计数: %v", err)
	}
}

func TestLoginGuardFailOpenWhenRedisDown(t *testing.T) {
	svc, mr := newLoginGuardForTest(t)
	ctx := context.Background()
	mr.Close()

	if err := svc.Check(ctx, 10000, "eve", "4.4.4.4"); err != nil {
		t.Fatalf("Redis 故障时应 fail-open: %v", err)
	}
	// 失败注册也不应 panic
	svc.RegisterFailure(ctx, 10000, "eve", "4.4.4.4")
}

func TestLockDurationForLevel(t *testing.T) {
	base, max := 5*time.Minute, 24*time.Hour
	cases := []struct {
		level int64
		want  time.Duration
	}{
		{1, 5 * time.Minute},
		{2, 10 * time.Minute},
		{4, 40 * time.Minute},
		{9, 24 * time.Hour}, // 5m × 2^8 = 21.3h < 24h? → 1280m ≈ 21.3h
		{10, 24 * time.Hour},
		{64, 24 * time.Hour}, // 级别截断不溢出
	}
	for _, c := range cases {
		got := lockDurationForLevel(base, max, c.level)
		if c.level == 9 {
			// 5m << 8 = 1280m = 21h20m，未达上限
			if got != 1280*time.Minute {
				t.Fatalf("level 9 = %v, want 21h20m", got)
			}
			continue
		}
		if got != c.want {
			t.Fatalf("level %d = %v, want %v", c.level, got, c.want)
		}
	}
}
