package catalog

import (
	"context"
	"strings"
	"testing"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/store"
	rwstream "aegis/services/risk/internal/stream"
)

// 内置特征全部能编进发布包，流式特征能生成视图
func TestBuiltinFeaturesCompile(t *testing.T) {
	b := bundle.Bundle{Scene: bundle.Scene{Key: "login"}}
	for _, f := range Features() {
		b.Features = append(b.Features, f.Feature)
		if f.Kind == bundle.FeatureStream {
			if _, err := rwstream.SQL(f.Feature, rwstream.Target{}); err != nil {
				t.Errorf("%s 生成视图失败：%v", f.Key, err)
			}
		}
	}
	if _, err := engine.Compile(context.Background(), b, nil); err != nil {
		t.Fatal(err)
	}
}

// IP / 网段多账号：只数登录失败的已有账号，不再是决策时按输入的登录名去重
func TestAccountsFeaturesCountExistingFailures(t *testing.T) {
	byKey := map[string]store.Feature{}
	for _, f := range Features() {
		byKey[f.Key] = f
	}
	for _, key := range []string{"ip_accounts_1h", "subnet_accounts_1h"} {
		f := byKey[key]
		if f.Kind != bundle.FeatureStream || f.Stream.Agg != bundle.AggDistinct || f.Stream.Field != "user_id" ||
			len(f.Stream.Events) != 1 || f.Stream.Events[0] != "login.fail" {
			t.Fatalf("%s 定义不对：%+v", key, f.Stream)
		}
		sql, err := rwstream.SQL(f.Feature, rwstream.Target{})
		if err != nil {
			t.Fatal(err)
		}
		if all := strings.Join(sql, "\n"); !strings.Contains(all, "count(DISTINCT user_id)") || !strings.Contains(all, "user_id > 0") {
			t.Fatalf("%s 视图没有按已有账号去重：\n%s", key, all)
		}
	}

	// 旧内置定义会被升级，改过的不动
	old := distinct("ip_accounts_1h", "IP 1 小时出现的账号数", bundle.EntityIP, bundle.EntityAccount, 3600, true)
	if !upgrades[0].legacy(old) {
		t.Fatal("旧内置定义应当升级")
	}
	edited := old
	edited.Velocity = &bundle.VelocityDef{WindowSeconds: 600, Agg: bundle.AggDistinct, DistinctOf: bundle.EntityAccount, AllScenes: true}
	if upgrades[0].legacy(edited) {
		t.Fatal("改过窗口的不该升级")
	}
}
