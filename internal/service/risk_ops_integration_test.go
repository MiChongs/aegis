package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"
	userdomain "aegis/internal/domain/user"
	"aegis/services/risk/kitex_gen/aegis/risk/v1/decisionservice"

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
			data = map[string]any{"listsRemoved": len(body["entryIds"].([]any)), "casesClosed": 2, "allowed": 1, "problems": []any{}}
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

