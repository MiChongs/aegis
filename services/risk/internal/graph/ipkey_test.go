package graph

import "testing"

func TestIPKey(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":                              "1.2.3.4",
		" 117.152.185.89 ":                     "117.152.185.89",
		"2409:8962:5a6:57:6071:d0ff:feb7:4aef": "2409:8962:5a6:57::/64",
		"2409:8962:5a6:57:1111:2222:3333:4444": "2409:8962:5a6:57::/64",
		"::ffff:95.82.9.1":                     "95.82.9.1",
		"2409:8962:5a6:57::/64":                "2409:8962:5a6:57::/64",
		"not-ip":                               "not-ip",
	}
	for in, want := range cases {
		if got := IPKey(in); got != want {
			t.Errorf("IPKey(%q) = %q, want %q", in, got, want)
		}
	}
	if IPVID("2409:8962:5a6:57:6071:d0ff:feb7:4aef") != "ip:2409:8962:5a6:57::/64" || !IsV6Net("2409:8962:5a6:57::/64") || IsV6Net("1.2.3.4") {
		t.Fatal("IPVID / IsV6Net")
	}
}
