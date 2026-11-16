package contract

import (
	"strings"
	"testing"
	"time"
)

func TestWorkKeyMergesVariants(t *testing.T) {
	same := [][2]string{
		{"晴天", "周杰伦"},
		{"晴天 (Live)", "周杰伦"},
		{"晴天（现场版）", "周杰伦/五月天"},
		{"  晴天 ", "周杰倫"},
		{"晴天 - 伴奏版", "周杰伦"},
	}
	base := WorkKey(same[0][0], same[0][1])
	if len(base) != 16 {
		t.Fatalf("work key length = %d", len(base))
	}
	for _, s := range same[1:] {
		if got := WorkKey(s[0], s[1]); got != base {
			t.Errorf("WorkKey(%q, %q) = %s, want %s", s[0], s[1], got, base)
		}
	}
	diff := [][2]string{{"晴天", "林俊杰"}, {"七里香", "周杰伦"}, {"晴天 (我的版本故事)", "周杰伦"}}
	for _, s := range diff[:2] {
		if WorkKey(s[0], s[1]) == base {
			t.Errorf("WorkKey(%q, %q) should differ", s[0], s[1])
		}
	}
	if WorkKey("", "周杰伦") != "" {
		t.Error("empty title must give empty key")
	}
	// 全角英文与大小写
	if WorkKey("ＨＥＬＬＯ", "Adele") != WorkKey("hello", "ADELE") {
		t.Error("full width / case should be normalized")
	}
}

func TestSplitArtists(t *testing.T) {
	got := SplitArtists("周杰伦/费玉清、 林俊杰 & 周杰伦")
	if strings.Join(got, "|") != "周杰伦|费玉清|林俊杰" {
		t.Fatalf("SplitArtists = %v", got)
	}
	if FirstArtist("Taylor Swift feat. Ed Sheeran") != "Taylor Swift" {
		t.Fatal("FirstArtist feat")
	}
}

func TestNormalize(t *testing.T) {
	now := time.UnixMilli(1763280000000)
	ok := ClientEvent{ID: "01JD3XABCDEF", Type: TypePlayEnd, TS: now.UnixMilli() - 1000, Reason: ReasonCompleted, PlayedMs: 200000,
		Track:   &Track{Source: "WY", SongID: "186016", Title: " 晴天\x00 ", Artist: "周杰伦", Cover: "javascript:alert(1)", DurationMs: 269000},
		Context: Context{Position: -3, Page: "home page!"}, Props: map[string]string{"quality": "lossless", "": "x"}}
	if r := Normalize(&ok, now); r != "" {
		t.Fatalf("valid event rejected: %s", r)
	}
	if ok.Track.Source != "wy" || ok.Track.Title != "晴天" || ok.Track.Cover != "" || ok.Context.Position != 0 || ok.Context.Page != "homepage" {
		t.Fatalf("not normalized: %+v %+v", ok.Track, ok.Context)
	}
	if len(ok.Props) != 1 {
		t.Fatalf("props = %v", ok.Props)
	}
	cases := []struct {
		name string
		ev   ClientEvent
		want string
	}{
		{"bad id", ClientEvent{ID: "x", Type: TypeLike, TS: now.UnixMilli()}, RejectInvalidID},
		{"bad type", ClientEvent{ID: "abcdefgh", Type: "hack", TS: now.UnixMilli()}, RejectInvalidType},
		{"too old", ClientEvent{ID: "abcdefgh", Type: TypeSearch, TS: now.Add(-31 * 24 * time.Hour).UnixMilli(), Context: Context{Query: "a"}}, RejectInvalidTime},
		{"future", ClientEvent{ID: "abcdefgh", Type: TypeSearch, TS: now.Add(time.Hour).UnixMilli(), Context: Context{Query: "a"}}, RejectInvalidTime},
		{"no track", ClientEvent{ID: "abcdefgh", Type: TypeLike, TS: now.UnixMilli()}, RejectMissingTrack},
		{"local track", ClientEvent{ID: "abcdefgh", Type: TypeLike, TS: now.UnixMilli(), Track: &Track{Source: "local", SongID: "l1", Title: "a"}}, RejectInvalidTrack},
		{"play_end no reason", ClientEvent{ID: "abcdefgh", Type: TypePlayEnd, TS: now.UnixMilli(), Track: &Track{Source: "wy", SongID: "1", Title: "a"}}, RejectMissingField},
		{"search no query", ClientEvent{ID: "abcdefgh", Type: TypeSearch, TS: now.UnixMilli()}, RejectMissingField},
		{"impression no rec", ClientEvent{ID: "abcdefgh", Type: TypeImpression, TS: now.UnixMilli()}, RejectMissingField},
	}
	for _, c := range cases {
		ev := c.ev
		if got := Normalize(&ev, now); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestEnrich(t *testing.T) {
	ev := ClientEvent{ID: "abcdefgh", Type: TypeLike, TS: 5, Track: &Track{Source: "wy", SongID: "1", Title: "晴天", Artist: "周杰伦"}}
	out := Enrich(ev, ClientInfo{Platform: "Android", AppVersion: "3.8.0"}, 10000, 42, "dev", "1.2.3.4", time.UnixMilli(9))
	if out.WorkKey != WorkKey("晴天", "周杰伦") || out.TrackKey != "wy:1" || out.Platform != "android" || out.ReceivedAt != 9 || out.AppID != 10000 {
		t.Fatalf("enrich = %+v", out)
	}
	if MsgID(1, 2, "x") != "1:2:x" || EventSubject(7) != "music.event.7" {
		t.Fatal("subjects")
	}
}
