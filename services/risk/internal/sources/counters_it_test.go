package sources

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/redisx"

	"github.com/redis/go-redis/v9"
)

func itRedis(t *testing.T) (*Redis, redis.UniversalClient) {
	t.Helper()
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	cfg := config.Load()
	if len(cfg.RedisAddrMap) == 0 {
		cfg.RedisAddrMap = map[string]string{}
		for i := 1; i <= 6; i++ {
			cfg.RedisAddrMap[fmt.Sprintf("risk-redis-%d:%d", i, 7000+i)] = fmt.Sprintf("127.0.0.1:%d", 7000+i)
		}
	}
	if cfg.RedisPassword == "" {
		cfg.RedisPassword = "aegis-risk"
	}
	rdb := redisx.New(cfg)
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedis(rdb), rdb
}

// 一个人：账号 ID（注册时为 0）、登录名、设备
type who struct {
	uid     int64
	account string
	device  string
}

const itApp = 990001

func (w who) actors() []string {
	var out []string
	if w.uid > 0 {
		out = append(out, engine.CounterTag(bundle.EntityUser, itApp, fmt.Sprint(w.uid)))
	}
	if w.account != "" {
		out = append(out, engine.CounterTag(bundle.EntityAccount, itApp, w.account))
	}
	if w.device != "" {
		out = append(out, engine.CounterTag(bundle.EntityDevice, itApp, w.device))
	}
	return out
}

// RISK_IT=1 go test ./services/risk/internal/sources -run CountersIT
func TestCountersIT(t *testing.T) {
	r, rdb := itRedis(t)
	ctx := context.Background()
	tag := fmt.Sprint(time.Now().UnixNano())
	ip := "198.51.100." + fmt.Sprint(time.Now().UnixNano()%250)
	devA, devShared := "devA-"+tag, "devS-"+tag
	u1 := who{uid: time.Now().UnixNano() % 1e12, account: "alice-" + tag, device: devA}
	u2 := who{uid: u1.uid + 1, account: "bob-" + tag, device: devShared}
	reg1 := who{account: u1.account, device: devA} // 注册那一刻还没有账号 ID

	count := func(feature, entity, value string, w who) engine.VelocityQuery {
		return engine.VelocityQuery{Feature: feature, Key: fmt.Sprintf("%s-%s:%d:login:%s", feature, tag, itApp, value), WindowSeconds: 600,
			Subject: engine.CounterTag(entity, itApp, value), Actors: w.actors()}
	}
	distinct := func(feature, entity, value, memberEntity, member string, w who) engine.VelocityQuery {
		q := count(feature, entity, value, w)
		q.Distinct = member
		q.Member = engine.CounterTag(memberEntity, itApp, member)
		return q
	}
	run := func(qs ...engine.VelocityQuery) map[string]float64 {
		t.Helper()
		v, err := r.Velocity(ctx, qs)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// peek 不想让读数改变计数：用一个匿名请求读，结果减去自己这一次
	peek := func(q engine.VelocityQuery) float64 {
		t.Helper()
		q.Actors, q.Subject = nil, ""
		return run(q)[q.Feature] - 1
	}
	uidStr := func(w who) string { return fmt.Sprint(w.uid) }

	// ── 次数：同一个地址上三个人各自的贡献 ──
	run(count("ip_reg", bundle.EntityIP, ip, reg1))                                         // u1 注册
	run(count("ip_reg", bundle.EntityIP, ip, u1), count("ip_reg", bundle.EntityIP, ip, u1)) // u1 登录后两次
	run(count("ip_reg", bundle.EntityIP, ip, u2))
	if v := run(count("ip_reg", bundle.EntityIP, ip, who{}))["ip_reg"]; v != 5 {
		t.Fatalf("地址上的次数 = %v，应为 5", v)
	}
	// u1 自己为主体的计数、共用设备上的计数
	run(count("user_login", bundle.EntityUser, uidStr(u1), u1), count("user_login", bundle.EntityUser, uidStr(u1), u1))
	run(count("dev_login", bundle.EntityDevice, devShared, who{uid: u1.uid, account: u1.account, device: devShared}))
	run(count("dev_login", bundle.EntityDevice, devShared, u2), count("dev_login", bundle.EntityDevice, devShared, u2))

	// ── 去重：共用设备上的账号数；设备用过的地址数（同一个地址两个人都带来过）──
	onShared := who{uid: u1.uid, account: u1.account, device: devShared}
	run(distinct("dev_users", bundle.EntityDevice, devShared, bundle.EntityUser, uidStr(u1), onShared))
	run(distinct("dev_users", bundle.EntityDevice, devShared, bundle.EntityUser, uidStr(u2), u2))
	run(distinct("dev_ips", bundle.EntityDevice, devShared, bundle.EntityIP, ip, onShared))
	run(distinct("dev_ips", bundle.EntityDevice, devShared, bundle.EntityIP, ip, u2))
	if v := run(distinct("dev_ips", bundle.EntityDevice, devShared, bundle.EntityIP, "203.0.113.7", onShared))["dev_ips"]; v != 2 {
		t.Fatalf("设备用过的地址数 = %v，应为 2", v)
	}
	// 地址上的设备数：devA 只属于 u1
	run(distinct("ip_devs", bundle.EntityIP, ip, bundle.EntityDevice, devA, u1))
	if v := run(distinct("ip_devs", bundle.EntityIP, ip, bundle.EntityDevice, devShared, u2))["ip_devs"]; v != 2 {
		t.Fatalf("地址上的设备数 = %v，应为 2", v)
	}

	// 索引有寿命
	if ttl := rdb.TTL(ctx, indexPrefix+engine.CounterTag(bundle.EntityUser, itApp, uidStr(u1))+"}").Val(); ttl <= 0 || ttl > 20*time.Minute {
		t.Fatalf("索引寿命 = %v", ttl)
	}

	// ── 删除 u1：账号、登录名、只属于它的设备是主体；共用设备与地址只撤回它的贡献 ──
	res, err := r.Forget(ctx, []string{
		engine.CounterTag(bundle.EntityUser, itApp, uidStr(u1)),
		engine.CounterTag(bundle.EntityAccount, itApp, u1.account),
		engine.CounterTag(bundle.EntityDevice, itApp, devA),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Counters == 0 || res.Dropped == 0 || res.Removed == 0 {
		t.Fatalf("回退结果 = %+v", res)
	}
	// 地址上：5 次里 u1 占 3 次（含注册那次，靠登录名认出），剩 u2 的 1 次和匿名的 1 次
	if v := peek(count("ip_reg", bundle.EntityIP, ip, who{})); v != 2 {
		t.Errorf("删除后地址上的次数 = %v，应为 2", v)
	}
	if v := peek(count("user_login", bundle.EntityUser, uidStr(u1), who{})); v != 0 {
		t.Errorf("被删账号自己的计数 = %v，应为 0", v)
	}
	if v := peek(count("dev_login", bundle.EntityDevice, devShared, who{})); v != 2 {
		t.Errorf("共用设备上的次数 = %v，应为 2（只剩另一个人的）", v)
	}
	// 去重读数：再记一次已有成员，不会多出来
	if v := run(distinct("dev_users", bundle.EntityDevice, devShared, bundle.EntityUser, uidStr(u2), u2))["dev_users"]; v != 1 {
		t.Errorf("共用设备上的账号数 = %v，应为 1", v)
	}
	// 地址 ip 另一个人也带来过，留下；203.0.113.7 只有 u1 带来过，去掉
	if v := run(distinct("dev_ips", bundle.EntityDevice, devShared, bundle.EntityIP, ip, u2))["dev_ips"]; v != 1 {
		t.Errorf("共用设备用过的地址数 = %v，应为 1", v)
	}
	if v := run(distinct("ip_devs", bundle.EntityIP, ip, bundle.EntityDevice, devShared, u2))["ip_devs"]; v != 1 {
		t.Errorf("地址上的设备数 = %v，应为 1（被删的设备不再算）", v)
	}
	if n := rdb.Exists(ctx, indexPrefix+engine.CounterTag(bundle.EntityUser, itApp, uidStr(u1))+"}").Val(); n != 0 {
		t.Error("被删账号的索引还在")
	}

	// ── 只撤回贡献（不是主体）：u2 的贡献拿掉，它自己为主体的计数留着 ──
	run(count("user_login", bundle.EntityUser, uidStr(u2), u2))
	if _, err := r.Forget(ctx, nil, []string{engine.CounterTag(bundle.EntityUser, itApp, uidStr(u2))}); err != nil {
		t.Fatal(err)
	}
	if v := peek(count("ip_reg", bundle.EntityIP, ip, who{})); v != 2 {
		// 之前 2 次（u2 一次 + 匿名一次）+ 上面 peek 的匿名一次 − u2 的一次
		t.Errorf("撤回贡献后地址上的次数 = %v，应为 2", v)
	}

	// ── 从名单解除一个地址：以它为主体的计数整个清掉 ──
	if _, err := r.Forget(ctx, []string{engine.CounterTag(bundle.EntityIP, itApp, ip)}, nil); err != nil {
		t.Fatal(err)
	}
	if v := peek(count("ip_reg", bundle.EntityIP, ip, who{})); v != 0 {
		t.Errorf("解除地址后的次数 = %v，应为 0", v)
	}
}

// 旧格式：次数并进新键，去重在切换后的一个窗口内读旧桶兜底
func TestCountersLegacyIT(t *testing.T) {
	r, rdb := itRedis(t)
	ctx := context.Background()
	tag := fmt.Sprint(time.Now().UnixNano())
	now := time.Now()
	ip := "198.51.100.77"
	base := "lg-" + tag + ":" + fmt.Sprint(itApp) + ":login:" + ip
	width := bucketWidth(600)
	old := legacyCountPrefix + base + "}:" + fmt.Sprint(now.Unix()/width)
	if err := rdb.Set(ctx, old, 7, 10*time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	n, err := r.MigrateLegacy(ctx, func(feature string) (string, int, bool) {
		if feature == "lg-"+tag {
			return bundle.EntityIP, 600, true
		}
		return "", 0, false
	})
	if err != nil || n < 1 {
		t.Fatalf("并入 = %d %v", n, err)
	}
	if rdb.Exists(ctx, old).Val() != 0 {
		t.Fatal("旧键还在")
	}
	q := engine.VelocityQuery{Feature: "lg", Key: base, WindowSeconds: 600}
	v, err := r.Velocity(ctx, []engine.VelocityQuery{q})
	if err != nil || v["lg"] != 8 {
		t.Fatalf("并入后的次数 = %v %v，应为 8", v, err)
	}
	// 并入的计数补了主体索引：解除这个地址能清掉
	if _, err := r.Forget(ctx, []string{engine.CounterTag(bundle.EntityIP, itApp, ip)}, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.Velocity(ctx, []engine.VelocityQuery{q}); v["lg"] != 1 {
		t.Fatalf("解除后的次数 = %v，应为 1", v["lg"])
	}

	// 去重：旧桶里有 5 个成员，新集合刚开始
	dbase := "lgd-" + tag + ":" + fmt.Sprint(itApp) + ":login:" + ip
	hll := legacyHLLPrefix + dbase + "}:" + fmt.Sprint(now.Unix()/width)
	if err := rdb.PFAdd(ctx, hll, "a", "b", "c", "d", "e").Err(); err != nil {
		t.Fatal(err)
	}
	rdb.Expire(ctx, hll, 10*time.Minute)
	dq := engine.VelocityQuery{Feature: "lgd", Key: dbase, WindowSeconds: 600, Distinct: "x"}
	fresh := NewRedis(rdb)
	fresh.cutoverOnce.Do(func() { fresh.cutover = now.Unix() })
	if v, _ := fresh.Velocity(ctx, []engine.VelocityQuery{dq}); v["lgd"] != 5 {
		t.Fatalf("窗口内读旧桶兜底 = %v，应为 5", v["lgd"])
	}
	// 过了一个窗口就不再读旧桶
	late := NewRedis(rdb)
	late.cutoverOnce.Do(func() { late.cutover = now.Unix() - 3600 })
	if v, _ := late.Velocity(ctx, []engine.VelocityQuery{dq}); v["lgd"] != 1 {
		t.Fatalf("过窗后只看新集合 = %v，应为 1", v["lgd"])
	}
}
