package graph

import (
	"testing"

	"aegis/services/risk/events"
)

// 回灌（从原值算）与实时事件（带摘要）必须落到同一个点
func TestDigestConsistency(t *testing.T) {
	if IdentityVID("QQ", "UID_abc") != IdentityVIDFromKey("qq", events.Digest("UID_abc")) {
		t.Fatal("identity vid mismatch")
	}
	if PasskeyVID("a1b2c3") != "pk:"+events.Digest("a1b2c3") {
		t.Fatal("passkey vid mismatch")
	}
}
