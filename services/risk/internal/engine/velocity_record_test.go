package engine

import (
	"context"
	"testing"

	"aegis/services/risk/internal/bundle"
)

// recordingSources 记下决策发给滑窗计数的查询
type recordingSources struct {
	NopSources
	got []VelocityQuery
}

func (r *recordingSources) Velocity(_ context.Context, q []VelocityQuery) (map[string]float64, error) {
	r.got = append(r.got, q...)
	return nil, nil
}

func TestVelocityRecordsOnlyConfirmedAccounts(t *testing.T) {
	vel := func(key, entity, agg, of string) bundle.Feature {
		return bundle.Feature{Key: key, Kind: bundle.FeatureVelocity, Entity: entity,
			Velocity: &bundle.VelocityDef{WindowSeconds: 3600, Agg: agg, DistinctOf: of, AllScenes: true}}
	}
	b := bundle.Bundle{
		Scene: bundle.Scene{Key: "login"},
		Features: []bundle.Feature{
			vel("ip_req", bundle.EntityIP, bundle.AggCount, ""),
			vel("ip_accounts", bundle.EntityIP, bundle.AggDistinct, bundle.EntityAccount),
			vel("account_ips", bundle.EntityAccount, bundle.AggDistinct, bundle.EntityIP),
			vel("account_req", bundle.EntityAccount, bundle.AggCount, ""),
			vel("device_ips", bundle.EntityDevice, bundle.AggDistinct, bundle.EntityIP),
		},
	}
	p := compileTest(t, b)
	queries := func(req Request) map[string]VelocityQuery {
		src := &recordingSources{}
		if _, err := p.Evaluate(context.Background(), req, src, Options{DecisionID: "d"}); err != nil {
			t.Fatal(err)
		}
		out := map[string]VelocityQuery{}
		for _, q := range src.got {
			out[q.Feature] = q
		}
		return out
	}
	readOnly := func(req Request) map[string]bool {
		out := map[string]bool{}
		for k, q := range queries(req) {
			out[k] = q.ReadOnly
		}
		return out
	}

	// 登录前：登录名只是输入，凡是拿它当主体或去重成员的都只读
	pre := readOnly(Request{Scene: "login", AppID: 1, Account: "Typo", IP: "203.0.113.7", Device: Device{ID: "dev-1"}})
	want := map[string]bool{"ip_req": false, "ip_accounts": true, "account_ips": true, "account_req": true, "device_ips": false}
	for k, v := range want {
		if got, ok := pre[k]; !ok || got != v {
			t.Errorf("登录前 %s ReadOnly = %v（有查询 %v），想要 %v", k, got, ok, v)
		}
	}

	// 登录前但登录名对应已有账号、或已登录：账号确认存在，全部记入，账号按 ID 去重
	for name, req := range map[string]Request{
		"登录前已有账号": {Scene: "login", AppID: 1, AccountUserID: 42, Account: "Alice@Example.com", IP: "203.0.113.7", Device: Device{ID: "dev-1"}},
		"已登录":     {Scene: "login", AppID: 1, UserID: 42, Account: "alice", IP: "203.0.113.7", Device: Device{ID: "dev-1"}},
	} {
		qs := queries(req)
		for k := range want {
			if qs[k].ReadOnly {
				t.Errorf("%s %s 不该只读", name, k)
			}
		}
		if q := qs["ip_accounts"]; q.Distinct != "42" || q.Member != "u:42" {
			t.Errorf("%s 账号去重成员 = %q / %q，想要按账号 ID", name, q.Distinct, q.Member)
		}
	}

	// 已有账号 ID 只用于计数，不当成登录用户
	env := BuildEnv(Request{Scene: "login", AppID: 1, AccountUserID: 42, Account: "a"})
	if env.UserID != 0 || env.LoggedIn || EntityValue(&env, bundle.EntityUser) != "" {
		t.Fatal("登录前的已有账号不该算已登录")
	}
}
