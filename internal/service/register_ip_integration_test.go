package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	"aegis/internal/domain/regip"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 注册 IP 控制的端到端集成测试：真实 Postgres（全部迁移跑两遍）+ miniredis。
//
// 旧开关推导 → 加验（40015，过验证码放行）→ 拒绝（40902）与名额回收（建号失败撤销占位）→
// 并发注册不超额 → 惩罚期（42917）与解除 → 放行 / 拒绝 / 放宽名单 → 累计规则按台账回填 →
// 观察模式 → 概览、IP 查询、模拟、记录与台账查询 → 历史档案回填。
//
// AEGIS_TEST_PG_DSN=postgres://…/空库 go test ./internal/service -run TestRegisterIPIntegration
func TestRegisterIPIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(35000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'register-ip-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "register-ip-test-secret-register-ip-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	apps := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), apps, nil)
	svc := NewRegisterIPService(log, pg, redisrepo.NewRegisterIPRepository(rdb, "t"), apps, nil)
	auth.SetRegisterIPService(svc)
	// 不 Start：批量写入退化成同步写，断言不用等异步队列

	var seq atomic.Int64
	register := func(rctx context.Context, ip string) error {
		n := seq.Add(1)
		_, err := auth.RegisterWithPassword(rctx, PasswordRegisterInput{
			AppID: appID, Account: fmt.Sprintf("user%04d", n), Password: "Str0ng!Passw0rd#2026", IP: ip,
			DeviceID: fmt.Sprintf("dev-%d", n), UserAgent: "test", SuppressSession: true,
		})
		return err
	}
	codeOf := func(err error) int {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return appErr.Code
		}
		if err != nil {
			return -1
		}
		return 0
	}
	// Commit / Release 是异步的：等计数落定再断言
	settle := func() { time.Sleep(80 * time.Millisecond) }
	savePolicy := func(p regip.Policy) {
		t.Helper()
		p.Enabled = true
		if _, err := svc.SavePolicy(ctx, appID, p); err != nil {
			t.Fatalf("保存策略失败：%v", err)
		}
	}
	reset := func() {
		mr.FlushAll()
		if _, err := pool.Exec(ctx, `DELETE FROM register_ip_ledger; DELETE FROM register_ip_lists; DELETE FROM register_ip_events`); err != nil {
			t.Fatal(err)
		}
		svc.lists.Delete(appID)
		svc.surge.Delete(appID)
	}

	t.Run("旧开关推导：同一地址累计 1 个，缺 IP 拒绝", func(t *testing.T) {
		if _, err := apps.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: map[string]any{"registerCheckIp": true}}); err != nil {
			t.Fatal(err)
		}
		if err := register(ctx, "1.1.1.1"); err != nil {
			t.Fatalf("第一个应放行：%v", err)
		}
		settle()
		if code := codeOf(register(ctx, "1.1.1.1")); code != errCodeRegisterIPLimited {
			t.Fatalf("第二个应 40902，得到 %d", code)
		}
		if code := codeOf(register(ctx, "")); code != errCodeRegisterIPMissing {
			t.Fatalf("缺 IP 应 40025，得到 %d", code)
		}
		if err := register(ctx, "1.1.1.2"); err != nil {
			t.Fatalf("另一个地址应放行：%v", err)
		}
	})

	t.Run("加验与拒绝、建号失败归还名额", func(t *testing.T) {
		reset()
		p := regip.DefaultPolicy()
		p.Penalty.Enabled = false
		p.Rules = []regip.Rule{
			{ID: "ip-hour", Enabled: true, Scope: regip.ScopeIP, WindowSeconds: 3600, Limit: 1, Action: regip.ActionChallenge},
			{ID: "ip-day", Enabled: true, Scope: regip.ScopeIP, WindowSeconds: 86400, Limit: 2, Action: regip.ActionReject},
		}
		savePolicy(p)
		if err := register(ctx, "2.2.2.2"); err != nil {
			t.Fatal(err)
		}
		settle()
		if code := codeOf(register(ctx, "2.2.2.2")); code != errCodeRiskCaptchaRequired {
			t.Fatalf("超过加验线应 40015，得到 %d", code)
		}
		passed := WithRiskCaptchaPassed(ctx)
		if err := register(passed, "2.2.2.2"); err != nil {
			t.Fatalf("过了验证码应放行：%v", err)
		}
		settle()
		if code := codeOf(register(passed, "2.2.2.2")); code != errCodeRegisterIPLimited {
			t.Fatalf("每天 2 个已满应 40902，得到 %d", code)
		}
		// 名额回收：账号已存在 → 建号失败 → 占位撤销
		_, err := auth.RegisterWithPassword(passed, PasswordRegisterInput{AppID: appID, Account: "user0001", Password: "Str0ng!Passw0rd#2026", IP: "2.2.3.3", SuppressSession: true})
		if code := codeOf(err); code != 40901 {
			t.Fatalf("重复账号应 40901，得到 %d", code)
		}
		settle()
		inspect, err := svc.Inspect(ctx, appID, "2.2.3.3")
		if err != nil {
			t.Fatal(err)
		}
		for _, usage := range inspect.Usage {
			if usage.Count != 0 {
				t.Fatalf("建号失败后 %s 计数应归零，得到 %d", usage.RuleID, usage.Count)
			}
		}
	})

	t.Run("并发注册不超额", func(t *testing.T) {
		reset()
		p := regip.DefaultPolicy()
		p.Penalty.Enabled = false
		p.Rules = []regip.Rule{
			{ID: "subnet-hour", Enabled: true, Scope: regip.ScopeSubnet, WindowSeconds: 3600, Limit: 5, Action: regip.ActionReject},
			{ID: "ip-life", Enabled: true, Scope: regip.ScopeIP, WindowSeconds: 0, Limit: 4, Action: regip.ActionReject},
		}
		savePolicy(p)
		var ok, limited atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < 24; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ip := "3.3.3.3"
				if i%2 == 1 {
					ip = "3.3.3.9"
				}
				switch code := codeOf(register(ctx, ip)); code {
				case 0:
					ok.Add(1)
				case errCodeRegisterIPLimited:
					limited.Add(1)
				default:
					t.Errorf("意外的错误码 %d", code)
				}
			}(i)
		}
		wg.Wait()
		if ok.Load() != 5 || limited.Load() != 19 {
			t.Fatalf("网段每小时 5 个：放行 %d、拒绝 %d", ok.Load(), limited.Load())
		}
		settle()
		var rows int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM register_ip_ledger WHERE ip <<= '3.3.3.0/24'`).Scan(&rows)
		if rows != 5 {
			t.Fatalf("台账应 5 行，得到 %d", rows)
		}
	})

	t.Run("惩罚期与解除", func(t *testing.T) {
		reset()
		p := regip.DefaultPolicy()
		p.Rules = []regip.Rule{{ID: "ip-hour", Enabled: true, Scope: regip.ScopeIP, WindowSeconds: 3600, Limit: 1, Action: regip.ActionReject}}
		p.Penalty = regip.Penalty{Enabled: true, Strikes: 2, StrikeWindowSeconds: 600, BaseSeconds: 600, Multiplier: 2, MaxSeconds: 3600, Scope: regip.ScopeIP}
		savePolicy(p)
		_ = register(ctx, "4.4.4.4")
		settle()
		_ = register(ctx, "4.4.4.4")
		_ = register(ctx, "4.4.4.4") // 第二次拒绝 → 进惩罚期
		if code := codeOf(register(ctx, "4.4.4.4")); code != errCodeRegisterIPPenalty {
			t.Fatalf("惩罚期内应 42917，得到 %d", code)
		}
		penalties, total, err := svc.Penalties(ctx, appID, 10)
		if err != nil || total != 1 || len(penalties) != 1 || penalties[0].Level != 1 {
			t.Fatalf("惩罚期列表：%+v total=%d err=%v", penalties, total, err)
		}
		if _, err := svc.Release(ctx, appID, RegisterIPReleaseInput{IP: "4.4.4.4", ResetLevel: true}, "tester"); err != nil {
			t.Fatal(err)
		}
		if err := register(ctx, "4.4.4.4"); err != nil {
			t.Fatalf("解除后应放行：%v", err)
		}
	})

	t.Run("名单", func(t *testing.T) {
		reset()
		p := regip.DefaultPolicy()
		p.Penalty.Enabled = false
		p.Rules = []regip.Rule{{ID: "ip-day", Enabled: true, Scope: regip.ScopeIP, WindowSeconds: 86400, Limit: 1, Action: regip.ActionReject}}
		savePolicy(p)
		if _, err := svc.AddLists(ctx, appID, regip.ListInput{Values: []string{"5.5.5.0/24"}, Kind: regip.ListAllow}, "tester"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddLists(ctx, appID, regip.ListInput{Values: []string{"6.6.6.6, 6.6.7.0/24"}, Kind: regip.ListDeny}, "tester"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddLists(ctx, appID, regip.ListInput{Values: []string{"7.7.7.7"}, Kind: regip.ListElevated, Factor: 3}, "tester"); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			if err := register(ctx, "5.5.5.5"); err != nil {
				t.Fatalf("放行名单不受限：%v", err)
			}
		}
		if code := codeOf(register(ctx, "6.6.7.8")); code != errCodeRegisterIPForbidden {
			t.Fatalf("拒绝名单应 40919，得到 %d", code)
		}
		for i := 0; i < 3; i++ {
			if err := register(ctx, "7.7.7.7"); err != nil {
				t.Fatalf("放宽 3 倍，第 %d 个应放行：%v", i+1, err)
			}
			settle()
		}
		if code := codeOf(register(ctx, "7.7.7.7")); code != errCodeRegisterIPLimited {
			t.Fatalf("放宽后第 4 个应 40902，得到 %d", code)
		}
		list, total, err := svc.ListLists(ctx, regip.ListQuery{AppID: appID, Keyword: "6.6.7.200"})
		if err != nil || total != 1 || list[0].CIDR != "6.6.7.0/24" {
			t.Fatalf("按地址查名单：%+v total=%d err=%v", list, total, err)
		}
	})

	t.Run("累计规则按台账回填、观察模式", func(t *testing.T) {
		reset()
		// 台账里已有 2 个（例如升级前注册的），Redis 里没有计数
		if _, err := pool.Exec(ctx, `INSERT INTO register_ip_ledger (appid, ip, method) VALUES ($1, '8.8.8.8', 'password'), ($1, '8.8.8.8', 'sms')`, appID); err != nil {
			t.Fatal(err)
		}
		p := regip.DefaultPolicy()
		p.Penalty.Enabled = false
		p.Rules = []regip.Rule{{ID: "ip-life", Enabled: true, Scope: regip.ScopeIP, Limit: 2, Action: regip.ActionReject}}
		savePolicy(p)
		if code := codeOf(register(ctx, "8.8.8.8")); code != errCodeRegisterIPLimited {
			t.Fatalf("按台账回填后应 40902，得到 %d", code)
		}
		p.Mode = regip.ModeObserve
		savePolicy(p)
		if err := register(ctx, "8.8.8.8"); err != nil {
			t.Fatalf("观察模式应放行：%v", err)
		}
		events, _, err := svc.Events(ctx, regip.EventQuery{AppID: appID, Kind: regip.EventObserved})
		if err != nil || len(events) != 1 || events[0].RuleID != "ip-life" {
			t.Fatalf("观察记录：%+v err=%v", events, err)
		}
	})

	t.Run("概览、查询、模拟、台账", func(t *testing.T) {
		overview, err := svc.Overview(ctx, appID, 7, "Asia/Shanghai")
		if err != nil {
			t.Fatal(err)
		}
		if overview.Totals["registrations"] == 0 || len(overview.Trend) != 7 || len(overview.TopIPs) == 0 {
			t.Fatalf("概览：%+v", overview.Totals)
		}
		inspect, err := svc.Inspect(ctx, appID, "8.8.8.8")
		if err != nil {
			t.Fatal(err)
		}
		if inspect.HostRegistrations != 3 || len(inspect.Usage) != 1 {
			t.Fatalf("查询：host=%d usage=%+v", inspect.HostRegistrations, inspect.Usage)
		}
		draft := regip.DefaultPolicy()
		draft.Enabled = true
		simulated, err := svc.Simulate(ctx, appID, regip.SimulateInput{Policy: &draft, IP: "9.9.9.9", Counts: map[string]int{"ip-day": 10}})
		if err != nil {
			t.Fatal(err)
		}
		if simulated.Decision.Outcome != regip.OutcomeReject || simulated.Decision.RuleID != "ip-day" || !simulated.Draft {
			t.Fatalf("模拟：%+v", simulated.Decision)
		}
		rows, total, err := svc.Registrations(ctx, regip.RegistrationQuery{AppID: appID, IP: "8.8.8.0/24"})
		if err != nil || total != 3 || len(rows) != 3 {
			t.Fatalf("台账：total=%d err=%v", total, err)
		}
		if _, _, err := svc.Registrations(ctx, regip.RegistrationQuery{AppID: appID, IP: "nonsense"}); codeOf(err) != 40000 {
			t.Fatalf("无效筛选应 400：%v", err)
		}
		released, err := svc.Release(ctx, appID, RegisterIPReleaseInput{IP: "8.8.8.8", Ledger: true}, "tester")
		if err != nil || released.LedgerReleased != 3 {
			t.Fatalf("解除台账：%+v err=%v", released, err)
		}
	})

	t.Run("历史档案回填", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `DROP TABLE register_ip_events; DROP TABLE register_ip_lists; DROP TABLE register_ip_ledger`); err != nil {
			t.Fatal(err)
		}
		user, err := pg.CreateUser(ctx, appID, "legacy-user", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, register_ip) VALUES ($1, '[2408:8207::1]:443') ON CONFLICT (user_id) DO UPDATE SET register_ip = EXCLUDED.register_ip`, user.ID); err != nil {
			t.Fatal(err)
		}
		bad, _ := pg.CreateUser(ctx, appID, "bad-ip-user", "")
		_, _ = pool.Exec(ctx, `INSERT INTO user_profiles (user_id, register_ip) VALUES ($1, 'unknown') ON CONFLICT (user_id) DO UPDATE SET register_ip = EXCLUDED.register_ip`, bad.ID)
		content, err := os.ReadFile("../../migrations/postgres/000143_register_ip.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		for round := 0; round < 2; round++ {
			if _, err := pool.Exec(ctx, strings.TrimSpace(string(content))); err != nil {
				t.Fatalf("迁移第 %d 遍：%v", round+1, err)
			}
		}
		var ip string
		if err := pool.QueryRow(ctx, `SELECT host(ip) FROM register_ip_ledger WHERE user_id = $1`, user.ID).Scan(&ip); err != nil || ip != "2408:8207::1" {
			t.Fatalf("回填：ip=%q err=%v", ip, err)
		}
		var mapped string
		_ = pool.QueryRow(ctx, `SELECT host(register_ip_try_inet('::ffff:1.2.3.4'))`).Scan(&mapped)
		if mapped != "1.2.3.4" {
			t.Fatalf("映射地址：%q", mapped)
		}
	})
}
