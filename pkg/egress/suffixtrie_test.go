package egress

import "testing"

func TestSuffixTrieMatchesOnLabelBoundary(t *testing.T) {
	trie := buildSuffixTrie([]string{"google.com", "a.example.com", "cn"})

	cases := []struct {
		host string
		want string
		ok   bool
	}{
		{"google.com", "google.com", true},
		{"www.google.com", "google.com", true},
		{"WWW.Google.COM.", "google.com", true},
		// 后缀匹配必须落在标签边界上，否则 notgoogle.com 会被误当成 google.com。
		{"notgoogle.com", "", false},
		{"google.com.evil.net", "", false},
		{"example.com", "", false},
		// 同时可命中时取更长的那条。
		{"x.a.example.com", "a.example.com", true},
		{"anything.cn", "cn", true},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := trie.match(tc.host)
		if ok != tc.ok || got != tc.want {
			t.Errorf("match(%q) = (%q, %v)，期望 (%q, %v)", tc.host, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSuffixTrieNilSafe(t *testing.T) {
	var trie *suffixTrie
	if _, ok := trie.match("google.com"); ok {
		t.Fatal("空 trie 不应命中任何域名")
	}
	if buildSuffixTrie(nil) != nil {
		t.Fatal("空后缀列表应返回 nil，让调用方跳过匹配")
	}
}

func TestNormalizeDomainSuffix(t *testing.T) {
	cases := map[string]string{
		"*.Google.com.": "google.com",
		".google.com":   "google.com",
		"google.com":    "google.com",
		"  GOOGLE.COM ": "google.com",
		"*":             "",
	}
	for input, want := range cases {
		if got := NormalizeDomainSuffix(input); got != want {
			t.Errorf("NormalizeDomainSuffix(%q) = %q，期望 %q", input, got, want)
		}
	}
}
