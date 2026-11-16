package rec

import (
	"strings"
	"testing"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/store"
)

func TestDiversifyCapsArtist(t *testing.T) {
	artists := []string{"a", "a", "a", "a", "b", "b", "c", "a", "d"}
	got := diversify(len(artists), func(k int) string { return artists[k] }, 6, 2)
	count := map[string]int{}
	for i, k := range got {
		count[artists[k]]++
		if i > 0 && artists[k] == artists[got[i-1]] && len(got) > 1 {
			// 有别的可选时不应连着同一歌手
			t.Errorf("consecutive same artist at %d: %v", i, got)
		}
	}
	if count["a"] > 2 {
		t.Fatalf("artist cap exceeded: %v", got)
	}
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
	// 只剩同一歌手时放宽，照样补满
	all := []string{"a", "a", "a"}
	if out := diversify(3, func(k int) string { return all[k] }, 3, 1); len(out) != 3 {
		t.Fatalf("relaxed fill = %v", out)
	}
}

func TestRecIDIsULID(t *testing.T) {
	a := newRecID(time.UnixMilli(1763280000000))
	b := newRecID(time.UnixMilli(1763280000001))
	if len(a) != 26 || len(b) != 26 || a >= b {
		t.Fatalf("ids %s %s", a, b)
	}
	if strings.ContainsAny(a, "ILOU") {
		t.Fatalf("not crockford: %s", a)
	}
}

func TestBuildProfile(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day := time.Date(2026, 11, 10, 23, 0, 0, 0, loc)
	var plays []store.PlayRow
	add := func(at time.Time, key, title, artist string, played int64, reason string) {
		plays = append(plays, store.PlayRow{TS: at.UnixMilli(), WorkKey: key, PlayedMs: played, Reason: reason,
			Track: contract.Track{Source: "wy", SongID: key, Title: title, Artist: artist, DurationMs: 240000}})
	}
	for d := 0; d < 4; d++ {
		at := day.AddDate(0, 0, d)
		add(at, "w1", "晴天", "周杰伦", 240000, "completed")
		add(at.Add(5*time.Minute), "w2", "七里香", "周杰伦", 240000, "completed")
		add(at.Add(10*time.Minute), "w3", "江南", "林俊杰", 8000, "skipped")
	}
	add(day.AddDate(0, 0, 6).Add(-12*time.Hour), "w4", "稻香", "周杰伦/费玉清", 240000, "completed")
	p := BuildProfile(plays, 3, 1, 30, loc, day.AddDate(0, 0, 7))
	if p.Plays != 13 || p.Tracks != 4 || p.Likes != 3 {
		t.Fatalf("plays=%d tracks=%d likes=%d", p.Plays, p.Tracks, p.Likes)
	}
	if p.LongestStreak != 4 || p.ActiveDays != 5 {
		t.Fatalf("streak=%d active=%d", p.LongestStreak, p.ActiveDays)
	}
	if p.TopArtists[0].Name != "周杰伦" || p.TopTracks[0].Plays != 4 {
		t.Fatalf("top = %+v %+v", p.TopArtists[0], p.TopTracks[0])
	}
	if p.Persona.Key != "night_owl" {
		t.Fatalf("persona = %+v", p.Persona)
	}
	if p.SkipRate <= 0.2 || p.CompletionRate < 0.6 {
		t.Fatalf("rates %f %f", p.SkipRate, p.CompletionRate)
	}
	if p.NewTrackShare != 0.75 {
		t.Fatalf("new share = %f", p.NewTrackShare)
	}
	if len(p.Hours) != 24 || len(p.Weekdays) != 7 || p.Hours[23] == 0 {
		t.Fatal("hour buckets")
	}
	empty := BuildProfile(nil, 0, 0, 7, loc, day)
	if empty.Persona.Key != "casual" || empty.TopArtists == nil || empty.Sources == nil {
		t.Fatalf("empty profile = %+v", empty)
	}
}
