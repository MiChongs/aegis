package events

import "testing"

func TestNormalizeDeviceID(t *testing.T) {
	for _, bad := range []string{"", "  ", "0000000000000000000000000000000000000000000000000000000000000000", "00000000-0000-0000-0000-000000000000",
		"9774d56d682e549c", "UNKNOWN", "null", "ua:19619c1804348206", "UA:abc", "0"} {
		if got := NormalizeDeviceID(bad); got != "" {
			t.Errorf("%q → %q，应为空", bad, got)
		}
	}
	for _, ok := range []string{"108db9c2f1a64e0b9d3c7a5e2f8b6d41", "web-00bd2a00-06c6-4802-9af4-b2fe8331e806", "006635f6dc6b53a1", "script-d201da59"} {
		if got := NormalizeDeviceID(" " + ok + " "); got != ok {
			t.Errorf("%q → %q", ok, got)
		}
	}
}
