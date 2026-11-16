package stream

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"aegis/services/risk/internal/bundle"
)

// 登录名主体的流式特征按账号 ID 归并，并且只有它们换对象名（要重建）；其余特征的对象名不变
func TestAccountEntityMergesByUserID(t *testing.T) {
	def := &bundle.StreamDef{Events: []string{"login.fail"}, Agg: bundle.AggCount, WindowSeconds: 3600}
	acct := bundle.Feature{Key: "account_login_fail_1h", Kind: bundle.FeatureStream, Entity: bundle.EntityAccount, Stream: def}
	ip := bundle.Feature{Key: "ip_login_fail_1h", Kind: bundle.FeatureStream, Entity: bundle.EntityIP, Stream: def}

	sql, err := SQL(acct, Target{})
	if err != nil {
		t.Fatal(err)
	}
	if all := strings.Join(sql, "\n"); !strings.Contains(all, "CASE WHEN user_id > 0 THEN '#' || user_id::varchar") {
		t.Fatalf("登录名主体没有按账号归并：\n%s", all)
	}

	old := func(f bundle.Feature) string {
		raw, _ := json.Marshal(struct {
			E string
			G bool
			S *bundle.StreamDef
			V int
		}{f.Entity, f.Global, f.Stream, 2})
		sum := sha256.Sum256(raw)
		return "rf_" + f.Key + "_" + hex.EncodeToString(sum[:])[:10]
	}
	if ObjectFor(ip).Base != old(ip) {
		t.Fatal("非登录名主体的对象名不该变")
	}
	if ObjectFor(acct).Base == old(acct) {
		t.Fatal("登录名主体的对象名要变，部署后才会按新写法重建")
	}
}
