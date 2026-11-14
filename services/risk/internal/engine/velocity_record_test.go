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
	readOnly := func(req Request) map[string]bool {
		src := &recordingSources{}
		if _, err := p.Evaluate(context.Background(), req, src, Options{DecisionID: "d"}); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, q := range src.got {
			out[q.Feature] = q.ReadOnly
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

	// 已登录：账号确认存在，全部记入
	post := readOnly(Request{Scene: "login", AppID: 1, UserID: 42, Account: "alice", IP: "203.0.113.7", Device: Device{ID: "dev-1"}})
	for k := range want {
		if post[k] {
			t.Errorf("已登录 %s 不该只读", k)
		}
	}
}
