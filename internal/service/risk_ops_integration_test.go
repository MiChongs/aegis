package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"
	realtimedomain "aegis/internal/domain/realtime"
	"aegis/internal/domain/regip"
	userdomain "aegis/internal/domain/user"
	redisrepo "aegis/internal/repository/redis"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"

	"go.uber.org/zap"
)

// 账号处置的端到端集成测试：真实 Postgres（全部迁移）+ 假的 risk-admin（httptest）+ 记录调用的假防火墙。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestRiskOpsIntegration
func TestRiskOpsIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newDeletionTestEnv(t, ctx, pool)
	appID := env.appID
	log := zap.NewNop()

	alice := env.recovery.user(t, "alice", "Pw-123456", "alice@example.com", "13800000001")
	bob := env.recovery.user(t, "bob", "Pw-123456", "bob@example.com", "13800000002")
	carol := env.recovery.user(t, "carol", "Pw-123456", "carol@example.com", "13800000003")
	dave := env.recovery.user(t, "dave", "Pw-123456", "dave@example.com", "13800000004")

	bans, err := NewAccountBanService(config.AccountBanConfig{}, log, env.pg, env.sessions)
	if err != nil {
		t.Fatal(err)
	}
	riskBan, err := bans.BanUser(ctx, appID, alice, userdomain.AccountBanCreateInput{
		BanType: userdomain.AccountBanTypePermanent, BanScope: userdomain.AccountBanScopeAll, Reason: "风控自动处置（场景 login）",
		Operator: userdomain.BanOperator{AdminName: "风控平台"}, Evidence: map[string]any{"source": "risk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bans.BanUser(ctx, appID, bob, userdomain.AccountBanCreateInput{
		BanType: userdomain.AccountBanTypePermanent, BanScope: userdomain.AccountBanScopeAll, Reason: "人工封禁",
		Operator: userdomain.BanOperator{AdminName: "root"},
	}); err != nil {
		t.Fatal(err)
	}
	var ipBanID int64
	if err := pool.QueryRow(ctx, `INSERT INTO ip_bans (ip, reason, source, status) VALUES ('203.0.113.7', '风控', 'risk', 'active') RETURNING id`).Scan(&ipBanID); err != nil {
		t.Fatal(err)
	}

	// 假 risk-admin：按请求里的用户回实体，记下 release / forget 的请求体
	var mu sync.Mutex
	got := map[string]map[string]any{}
	operators := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Risk-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		path := strings.TrimPrefix(r.URL.Path, "/v1/")
		mu.Lock()
		got[path] = body
		operators[path], _ = url.PathUnescape(r.Header.Get("X-Risk-Operator"))
		mu.Unlock()
		var data any
		switch path {
		case "subjects/inspect":
			var subs []map[string]any
			for _, v := range body["userIds"].([]any) {
				uid := int64(v.(float64))
				ents := []map[string]any{{"kind": "device", "value": "dev-" + itoa64(uid), "label": "小米 14", "lists": []any{}}}
				if uid == alice {
					ents = append(ents, map[string]any{"kind": "ip", "value": "203.0.113.7", "label": "203.0.113.7",
						"lists": []any{map[string]any{"id": "e1", "type": "ip", "value": "203.0.113.7", "kind": "block"}}})
				}
				subs = append(subs, map[string]any{"userId": uid, "banned": true, "lists": []any{}, "entities": ents, "cases": []any{}})
			}
			related := []any{}
			if body["related"] == true {
				related = []any{
					map[string]any{"userId": carol, "strength": "strong", "via": []any{alice}, "reasons": []any{map[string]any{"kind": "device", "label": "小米 14"}}},
					map[string]any{"userId": 987654321, "strength": "strong", "via": []any{alice}, "reasons": []any{}},
				}
			}
			data = map[string]any{"days": 180, "subjects": subs, "related": related, "errors": map[string]any{}}
		case "subjects/release":
			data = map[string]any{"listsRemoved": len(body["entryIds"].([]any)), "casesClosed": 2, "allowed": 1, "countersCleared": 4,
				"ips": []any{"198.51.100.99"}, "devices": []any{}, "problems": []any{}}
		case "subjects/candidates":
			if r.URL.Query().Get("source") != "blocked" || r.URL.Query().Get("min") != "5" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			data = map[string]any{"items": []any{
				map[string]any{"appId": appID, "userId": bob, "count": 9, "maxScore": 95, "reason": "设备刷量"},
				map[string]any{"appId": appID, "userId": 987654321, "count": 7},
			}}
		case "subjects/forget":
			data = map[string]any{"devicesBlocked": len(body["blockDevices"].([]any)), "casesClosed": 1, "graphRemoved": 5, "problems": []any{}}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": data})
	}))
	defer srv.Close()

	risk := NewRiskService(config.RiskConfig{AdminURL: srv.URL, InternalToken: "tok"}, log, "ops-test", "", nil, "t")
	risk.client = fakeDecisionClient{}
	fw := &fakeRiskFirewall{}
	users := NewUserService(log, env.pg, env.sessions, nil, nil)
	users.SetAccountBanService(bans)
	risk.SetOps(env.pg, users)
	risk.SetAccountBanService(bans)
	risk.SetFirewall(fw)
	op := RiskOpsOperator{AdminID: 1, Name: "运营 小王"}

	// 主服务侧计数：注册准入、登录防爆破、在线连接、排行榜缓存（miniredis）
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	regIP := NewRegisterIPService(log, env.pg, redisrepo.NewRegisterIPRepository(rdb, "t"), NewAppService(log, env.pg, env.sessions), nil)
	guardRepo := redisrepo.NewLoginGuardRepository(rdb, "t")
	guard := NewLoginGuardService(config.LoginGuardConfig{}, log, guardRepo, env.pg)
	rt := redisrepo.NewRealtimeRepository(rdb, "t")
	counters := NewAccountCounters(log, regIP, guard, rt, redisrepo.NewSessionRepository(rdb, "t"), risk)
	users.AddPurgeHook(counters.PurgeHook)
	risk.SetAccountCounters(counters)
	policy, err := regIP.policyOf(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	var daveLedger int64
	if err := pool.QueryRow(ctx, `INSERT INTO register_ip_ledger (appid, user_id, account, ip, device_id) VALUES ($1, $2, 'dave', '198.51.100.7', 'farm-device-0001') RETURNING id`,
		appID, dave).Scan(&daveLedger); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO register_ip_ledger (appid, user_id, account, ip, device_id) VALUES ($1, $2, 'alice', '203.0.113.9', '')`, appID, alice); err != nil {
		t.Fatal(err)
	}
	regKeys := func(ip, device string) []string {
		addr, _ := regip.ParseAddress(ip, policy.Network)
		dev := regip.ParseDevice(device, policy.Device.MinLength)
		var keys []string
		for _, rule := range policy.Rules {
			if rule.Scope == regip.ScopeApp && !rule.Lifetime() {
				continue
			}
			if subject := regip.SubjectKey(rule.Scope, addr, dev, regip.Location{}); subject != "" {
				keys = append(keys, fmt.Sprintf("t{regip:%d}:w:%s:%s", appID, rule.ID, subject), fmt.Sprintf("t{regip:%d}:n:%s:%s", appID, rule.ID, subject))
			}
		}
		return keys
	}
	daveRegKeys := regKeys("198.51.100.7", "farm-device-0001")
	if len(daveRegKeys) == 0 {
		t.Fatalf("默认策略里没有按地址或设备计数的规则")
	}
	for _, k := range daveRegKeys {
		mr.Set(k, "5")
	}
	penaltyKey := fmt.Sprintf("t{regip:%d}:pen:203.0.113.9/32", appID)
	mr.Set(penaltyKey, "1")
	_ = guardRepo.SetLock(ctx, loginGuardKindAccount, appID, "alice", time.Hour)
	_ = guardRepo.SetLock(ctx, loginGuardKindAccount, appID, "dave", time.Hour)
	_ = guardRepo.SetLock(ctx, loginGuardKindIP, appID, "198.51.100.99", time.Hour)
	if err := rt.UpsertConnection(ctx, realtimedomain.PresenceConnection{ConnectionID: "conn-dave", AppID: appID, UserID: dave, ConnectedAt: time.Now(), LastSeenAt: time.Now()}, time.Minute); err != nil {
		t.Fatal(err)
	}
	rankKey := fmt.Sprintf("t:rank:points:%d:integral:all:1:20", appID)
	mr.Set(rankKey, "[]")

	// ── 清单 ──
	ins, err := risk.RiskOpsInspect(ctx, appID, []int64{alice, bob, alice, 0}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins.Subjects) != 2 || len(ins.Errors) != 0 {
		t.Fatalf("去重后应是两个账号且没有错误：%+v", ins)
	}
	a, b := ins.Subjects[0], ins.Subjects[1]
	if a.User == nil || a.User.Ban == nil || a.User.Ban.ID != riskBan.ID || !a.User.BanFromRisk {
		t.Fatalf("alice 的风控封禁没认出来：%+v", a.User)
	}
	if b.User == nil || b.User.Ban == nil || b.User.BanFromRisk {
		t.Fatalf("bob 是人工封禁，不应算风控封禁：%+v", b.User)
	}
	var fwSeen bool
	for _, e := range a.Entities {
		if e.Kind == "ip" && len(e.Firewall) == 1 && e.Firewall[0].ID == ipBanID {
			fwSeen = true
		}
	}
	if !fwSeen {
		t.Fatalf("alice 用过的 IP 上的防火墙封禁应挂到实体上：%+v", a.Entities)
	}
	if len(ins.Related) != 1 || ins.Related[0].UserID != carol || ins.Related[0].User == nil || ins.Related[0].User.Account != "carol" {
		t.Fatalf("关联账号应只剩主库里存在的 carol：%+v", ins.Related)
	}

	// ── 风控封禁中的账号 ──
	list, total, err := risk.RiskOpsBanned(ctx, userdomain.RiskBannedQuery{AppID: appID})
	if err != nil || total != 1 || len(list) != 1 || list[0].ID != alice {
		t.Fatalf("风控封禁中的应只有 alice：%d %+v %v", total, list, err)
	}
	if _, total, _ := risk.RiskOpsBanned(ctx, userdomain.RiskBannedQuery{AppID: appID, Keyword: "ali"}); total != 1 {
		t.Fatalf("按账号搜不到 alice")
	}
	if _, total, _ := risk.RiskOpsBanned(ctx, userdomain.RiskBannedQuery{AppID: appID, Keyword: itoa64(alice)}); total != 1 {
		t.Fatalf("按用户 ID 搜不到 alice")
	}
	if _, total, _ := risk.RiskOpsBanned(ctx, userdomain.RiskBannedQuery{AppID: appID, Keyword: "bob"}); total != 0 {
		t.Fatalf("人工封禁的 bob 不应出现")
	}

	// ── 风险账号发现：设备聚集（占位设备码不算）、风控侧多次被拦截 ──
	for _, row := range []struct {
		uid int64
		key string
	}{{carol, "farm-device-1"}, {dave, "farm-device-1"}, {alice, "farm-device-1"}, {carol, "0000-0000"}, {dave, "0000-0000"}, {bob, "solo-device"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO user_login_devices (appid, user_id, device_key, model) VALUES ($1, $2, $3, 'Xiaomi Mi 10')`, appID, row.uid, row.key); err != nil {
			t.Fatal(err)
		}
	}
	found, err := risk.RiskOpsFind(ctx, RiskOpsCandidateQuery{AppID: appID, Source: "device", Min: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(found.Clusters) != 1 || found.Clusters[0].DeviceKey != "farm-device-1" || len(found.Clusters[0].Members) != 3 || found.Clusters[0].Model != "Xiaomi Mi 10" {
		t.Fatalf("应只聚出 farm-device-1 上的 3 个账号：%+v", found.Clusters)
	}
	if f, _ := risk.RiskOpsFind(ctx, RiskOpsCandidateQuery{AppID: appID, Source: "device", Min: 4}); len(f.Clusters) != 0 {
		t.Fatalf("门槛 4 时不应有聚集：%+v", f.Clusters)
	}
	blocked, err := risk.RiskOpsFind(ctx, RiskOpsCandidateQuery{AppID: appID, Source: "blocked", Min: 5, Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked.Items) != 1 || blocked.Items[0].User.ID != bob || blocked.Items[0].Count != 9 || blocked.Items[0].Reason != "设备刷量" {
		t.Fatalf("多次被拦截应只剩主库里存在的 bob：%+v", blocked.Items)
	}
	if b, _ := risk.RiskOpsFind(ctx, RiskOpsCandidateQuery{AppID: appID, Source: "banned"}); len(b.Items) != 1 || b.Items[0].User.ID != alice || b.Items[0].LastAt == nil {
		t.Fatalf("风控封禁中应是 alice：%+v", b.Items)
	}

	// ── 一键解除 ──
	rel, err := risk.RiskOpsRelease(ctx, op, RiskOpsReleaseInput{AppID: appID, UserIDs: []int64{alice, bob}, BanIDs: []int64{riskBan.ID},
		IPBanIDs: []int64{ipBanID, ipBanID}, EntryIDs: []string{"e1"}, CloseCases: true, AllowDays: 7, Note: "误判"})
	if err != nil {
		t.Fatal(err)
	}
	if rel.Unbanned != 1 || rel.IPUnbanned != 1 || rel.ListsRemoved != 1 || rel.CasesClosed != 2 || rel.Allowed != 1 || len(rel.Problems) != 0 {
		t.Fatalf("解除结果不对：%+v", rel)
	}
	if active, _ := bans.GetActiveBan(ctx, appID, alice); active != nil {
		t.Fatalf("alice 应已解封：%+v", active)
	}
	if active, _ := bans.GetActiveBan(ctx, appID, bob); active == nil {
		t.Fatalf("没勾选的 bob 人工封禁不应被动")
	}
	if len(fw.unbanned) != 1 || fw.unbanned[0] != ipBanID {
		t.Fatalf("防火墙应撤销一次：%v", fw.unbanned)
	}
	if body := got["subjects/release"]; body["allowDays"] != float64(7) || body["closeCases"] != true || body["note"] != "误判" {
		t.Fatalf("发给风控侧的解除请求不对：%+v", body)
	}
	if rel.RiskCounters != 4 || rel.Counters.RegisterCounters == 0 || rel.Counters.LoginGuards < 3 {
		t.Fatalf("解除时两边的计数都要清：%+v", rel)
	}
	if mr.Exists(penaltyKey) {
		t.Fatalf("alice 注册地址的惩罚期应已解除")
	}
	if rem, _ := guardRepo.LockRemaining(ctx, loginGuardKindAccount, appID, "alice"); rem > 0 {
		t.Fatalf("alice 的登录锁定应已清掉")
	}
	if rem, _ := guardRepo.LockRemaining(ctx, loginGuardKindIP, appID, "198.51.100.99"); rem > 0 {
		t.Fatalf("风控记录到的地址的登录锁定应已清掉")
	}
	// bob 的人工封禁没勾选：关系图上要继续按封禁算
	if still, _ := got["subjects/release"]["stillBanned"].([]any); len(still) != 1 || int64(still[0].(float64)) != bob {
		t.Fatalf("仍在封号中的应只有 bob：%v", got["subjects/release"]["stillBanned"])
	}
	if operators["subjects/release"] != "运营 小王" {
		t.Fatalf("操作者应带到风控侧：%q", operators["subjects/release"])
	}

	// ── 关联删除 ──
	if _, err := risk.RiskOpsPurge(ctx, op, RiskOpsPurgeInput{AppID: appID, UserIDs: []int64{dave, carol}, Confirm: "删除 1 个账号"}); errCode(err) != 40000 {
		t.Fatalf("确认文字不对应被拒，得到 %v", err)
	}
	purge, err := risk.RiskOpsPurge(ctx, op, RiskOpsPurgeInput{AppID: appID, UserIDs: []int64{dave, carol, 987654321}, BlockDevices: true, BlockDays: 30,
		Confirm: RiskOpsConfirmText(3), Note: "批量注册"})
	if err != nil {
		t.Fatal(err)
	}
	if purge.Deleted != 2 || purge.DevicesBlocked != 2 || purge.CasesClosed != 1 || purge.GraphRemoved != 5 || len(purge.Users) != 3 || purge.Users[2].OK {
		t.Fatalf("删除结果不对：%+v", purge)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = ANY($1)`, []int64{dave, carol}).Scan(&left); err != nil || left != 0 {
		t.Fatalf("dave、carol 应已删除，还剩 %d（%v）", left, err)
	}
	// 删号收尾：dave 的注册台账标为已解除、相关计数清掉、登录锁定与在线连接清掉、排行榜缓存清掉
	var releasedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT released_at FROM register_ip_ledger WHERE id = $1`, daveLedger).Scan(&releasedAt); err != nil || releasedAt == nil {
		t.Fatalf("dave 的注册台账应标为已解除：%v %v", releasedAt, err)
	}
	for _, k := range daveRegKeys {
		if mr.Exists(k) {
			t.Fatalf("注册计数 %s 应已清掉", k)
		}
	}
	if rem, _ := guardRepo.LockRemaining(ctx, loginGuardKindAccount, appID, "dave"); rem > 0 {
		t.Fatalf("dave 的登录锁定应已清掉")
	}
	if n, _ := rdb.ZCard(ctx, fmt.Sprintf("t:realtime:app:%d:user:%d:connections", appID, dave)).Result(); n != 0 {
		t.Fatalf("dave 的在线连接应已清掉：%d", n)
	}
	if mr.Exists(rankKey) {
		t.Fatalf("排行榜缓存应已清掉")
	}
	body := got["subjects/forget"]
	devs, _ := body["blockDevices"].([]any)
	if len(devs) != 2 || body["blockDays"] != float64(30) || len(body["userIds"].([]any)) != 2 {
		t.Fatalf("风控侧收尾请求不对：%+v", body)
	}
}

type fakeDecisionClient struct{ decisionservice.Client }

type fakeRiskFirewall struct {
	mu       sync.Mutex
	unbanned []int64
}

func (f *fakeRiskFirewall) BanIPWithOptions(context.Context, string, string, int64, int64, BanIPOptions) (*firewalldomain.IPBan, error) {
	return &firewalldomain.IPBan{}, nil
}

func (f *fakeRiskFirewall) UnbanIP(_ context.Context, banID int64, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unbanned = append(f.unbanned, banID)
	return nil
}

func (f *fakeRiskFirewall) ListBans(context.Context, firewalldomain.IPBanFilter) (*firewalldomain.IPBanPage, error) {
	return &firewalldomain.IPBanPage{}, nil
}
