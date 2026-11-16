package rec

import (
	"testing"

	"aegis/services/music/contract"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

func TestInsertResolve(t *testing.T) {
	var out []Item
	for i := 0; i < 10; i++ {
		out = append(out, Item{WorkKey: string(rune('a' + i))})
	}
	extra := []Item{
		{WorkKey: "x1", Track: contract.Track{Title: "x1", Artist: "甲"}, Resolve: true},
		{WorkKey: "x2", Track: contract.Track{Title: "x2", Artist: "坏歌手"}, Resolve: true},
		{WorkKey: "x3", Track: contract.Track{Title: "x3", Artist: "乙"}, Resolve: true},
		{WorkKey: "x4", Track: contract.Track{Title: "x4", Artist: "丙"}, Resolve: true},
	}
	got := insertResolve(append([]Item(nil), out...), extra, 10, 0.3, map[string]bool{contract.NormalizeArtist("坏歌手"): true}, nil, false)
	if len(got) != 10 {
		t.Fatalf("len = %d", len(got))
	}
	pos := []int{}
	for i, it := range got {
		if it.Resolve {
			pos = append(pos, i)
			if it.WorkKey == "x2" {
				t.Fatal("disliked artist inserted")
			}
		}
	}
	if len(pos) != 3 || pos[0] != 2 || pos[1] != 5 || pos[2] != 8 {
		t.Fatalf("resolve positions = %v", pos)
	}
	if n := insertResolve(out, extra, 10, 0, nil, nil, false); len(n) != 10 || n[2].Resolve {
		t.Fatal("ratio 0 must insert nothing")
	}
}

func TestContentProfileMatch(t *testing.T) {
	ct := &appContent{items: map[string]*store.ItemContent{
		"a": {Keys: []string{"pop", "sad"}, Embedding: []float32{1, 0}},
		"b": {Keys: []string{"pop", "happy"}, Embedding: []float32{0.9, 0.1}},
		"c": {Keys: []string{"metal", "angry"}, Embedding: []float32{-1, 0}},
	}}
	st := realtime.Empty()
	st.Positive = []realtime.Entry{{WorkKey: "a", At: 1, Weight: 1}}
	p := buildContentProfile(ct, st)
	tb, sb := p.match(ct.items["b"])
	tc, sc := p.match(ct.items["c"])
	if tb <= tc || sb <= sc || sb < 0.9 || sc > 0.1 {
		t.Fatalf("match b=(%v,%v) c=(%v,%v)", tb, sb, tc, sc)
	}
	if x, y := p.match(nil); x != 0 || y != 0 {
		t.Fatal("nil content")
	}
}

func TestTagsClean(t *testing.T) {
	tg := contract.Tags{Genres: []string{"pop", "xx", "rock", "folk", "jazz"}, Moods: []string{"sad"}, Era: "1990s", Vocal: "robot",
		Energy: 3, Confidence: -1, Flags: []string{"live", "fake"}}
	tg.Clean()
	if len(tg.Genres) != 3 || tg.Genres[1] != "rock" || tg.Vocal != "" || tg.Energy != 1 || tg.Confidence != 0 || len(tg.Flags) != 1 {
		t.Fatalf("clean = %+v", tg)
	}
	if contract.TagTitle("healing") != "治愈" || contract.TagTitle("zzz") != "zzz" {
		t.Fatal("tag title")
	}
	keys := tg.Keys()
	if len(keys) != 6 { // 3 曲风 + 1 情绪 + era + flag
		t.Fatalf("keys = %v", keys)
	}
}
