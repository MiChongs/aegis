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

func TestIsHosting(t *testing.T) {
	cases := []struct {
		isp  string
		asn  int64
		want bool
	}{
		{"Oracle", 0, true}, {"Hytron Network Services Limited", 151407, true}, {"Cloudinnovation", 0, true},
		{"TermRat Network Limited", 154648, true}, {"Alibaba (US) Technology Co., Ltd.", 45102, true},
		{"移动", 9808, false}, {"中国电信", 4134, false}, {"Far EastTone Telecommunication Co., Ltd.", 9674, false},
		{"KDDI", 2516, false}, {"教育网", 4538, false}, {"", 0, false}, {"OVH SAS", 16276, true}, {"阿里云", 0, true},
	}
	for _, c := range cases {
		if got := IsHosting(c.isp, c.asn); got != c.want {
			t.Errorf("%q/%d: got %v want %v", c.isp, c.asn, got, c.want)
		}
	}
}
