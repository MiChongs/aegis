package proxyip

import "testing"

func TestContains(t *testing.T) {
	s := Parse("10.0.0.0/8,95.82.8.0/23,104.168.4.167/32, 8.221.123.21 ,2606:4700::/32,garbage")
	cases := map[string]bool{
		"10.0.1.9": true, "95.82.9.4": true, "95.82.8.0": true, "95.82.10.1": false, "104.168.4.167": true,
		"8.221.123.21": true, "8.221.123.22": false, "2606:4700::6810:62d7": true, "::ffff:95.82.9.1": true,
		"120.231.114.206": false, "not-an-ip": true,
	}
	for ip, want := range cases {
		if got := s.Contains(ip); got != want {
			t.Errorf("%s: got %v want %v", ip, got, want)
		}
	}
	if s.Len() != 5 {
		t.Fatalf("len %d", s.Len())
	}
}
