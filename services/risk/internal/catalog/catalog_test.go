package catalog

import (
	"context"
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

// IP / 网段多账号：跨场景按账号去重的滑窗计数（是否记入、成员取值由引擎按账号是否存在决定）
func TestAccountsFeatures(t *testing.T) {
	byKey := map[string]store.Feature{}
	for _, f := range Features() {
		byKey[f.Key] = f
	}
	for _, key := range []string{"ip_accounts_1h", "subnet_accounts_1h"} {
		f := byKey[key]
		if f.Kind != bundle.FeatureVelocity || f.Velocity.Agg != bundle.AggDistinct ||
			f.Velocity.DistinctOf != bundle.EntityAccount || !f.Velocity.AllScenes || f.Description == "" {
			t.Fatalf("%s 定义不对：%+v", key, f)
		}
	}
}
