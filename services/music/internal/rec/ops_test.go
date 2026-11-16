package rec

import (
	"testing"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

func TestBucketStableAndSpread(t *testing.T) {
	if Bucket(3, 42) != Bucket(3, 42) {
		t.Fatal("bucket must be stable")
	}
	counts := make([]int, 2)
	for u := int64(1); u <= 4000; u++ {
		if Bucket(7, u) < 50 {
			counts[0]++
		} else {
			counts[1]++
		}
	}
	if counts[0] < 1800 || counts[1] < 1800 {
		t.Fatalf("bucket spread = %v", counts)
	}
}

func TestValidateExperiment(t *testing.T) {
	ok := store.Experiment{Name: "精排对比", Scenes: []string{"home"}, Variants: []store.ExpVariant{{Key: "A", Traffic: 50}, {Key: "B", Traffic: 50}}}
	if err := ValidateExperiment(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.Variants[0].Name == "" {
		t.Fatal("variant name should be defaulted")
	}
	bad := []store.Experiment{
		{Name: "", Scenes: []string{"home"}, Variants: ok.Variants},
		{Name: "x", Scenes: []string{"nope"}, Variants: ok.Variants},
		{Name: "x", Scenes: []string{"home"}, Variants: []store.ExpVariant{{Key: "A", Traffic: 50}}},
		{Name: "x", Scenes: []string{"home"}, Variants: []store.ExpVariant{{Key: "A", Traffic: 60}, {Key: "B", Traffic: 60}}},
		{Name: "x", Scenes: []string{"home"}, Variants: []store.ExpVariant{{Key: "A", Traffic: 10}, {Key: "A", Traffic: 10}}},
	}
	big := 9.0
	bad = append(bad, store.Experiment{Name: "x", Scenes: []string{"home"}, Variants: []store.ExpVariant{{Key: "A", Traffic: 10},
		{Key: "B", Traffic: 10, Overrides: store.Overrides{ExploreRatio: &big}}}})
	for i, e := range bad {
		e := e
		if ValidateExperiment(&e) == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestRuleIndex(t *testing.T) {
	rules := []store.Rule{
		{ID: 1, Kind: "block", Target: store.RuleTarget{Type: "work", WorkKey: "w1"}},
		{ID: 2, Kind: "block", Target: store.RuleTarget{Type: "artist", Artist: "周杰倫/费玉清"}},
		{ID: 3, Kind: "boost", Weight: 2, Scenes: []string{"daily"}, Target: store.RuleTarget{Type: "work", WorkKey: "w2"}},
		{ID: 4, Kind: "boost", Weight: 3, Target: store.RuleTarget{Type: "artist", Artist: "林俊杰"}},
	}
	bw, ba, boosts := ruleIndex(rules, "home")
	if !bw["w1"] || !ba[contract.NormalizeArtist("周杰伦")] {
		t.Fatalf("block index wrong: %v %v", bw, ba)
	}
	if _, ok := boosts.work["w2"]; ok {
		t.Fatal("daily-only boost must not apply to home")
	}
	if b := boosts.artist[contract.NormalizeArtist("林俊杰")]; b.w != 3 || b.id != 4 {
		t.Fatalf("artist boost = %+v", b)
	}
	_, _, daily := ruleIndex(rules, "daily")
	if daily.work["w2"].w != 2 {
		t.Fatal("daily boost missing")
	}
}

func TestFeedbackList(t *testing.T) {
	st := realtime.Empty()
	st.Disliked["w1"] = 100
	st.DislikedArtists["周杰伦"] = 300
	st.Suppressed["w2"] = 200 + 30*24*3600*1000
	st.BadVersions["wy:9"] = 50
	all := FeedbackList(st, "", 30)
	if len(all) != 4 || all[0].Kind != "artist" || all[1].Kind != "suppressed" || all[1].At != 200 {
		t.Fatalf("feedback = %+v", all)
	}
	if only := FeedbackList(st, "track", 30); len(only) != 1 || only[0].WorkKey != "w1" {
		t.Fatalf("track feedback = %+v", only)
	}
}

func TestPeriodBoundsAndReport(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, loc) // 周三
	from, to, prev, label, err := periodBounds("week", 0, now, loc)
	if err != nil || from.Weekday() != time.Monday || from.Day() != 16 || to.Sub(from) != 7*24*time.Hour || prev.Day() != 9 {
		t.Fatalf("week bounds %v %v %v %v", from, to, prev, err)
	}
	if label != "11 月 16 日至 11 月 22 日" {
		t.Fatalf("label = %q", label)
	}
	if _, _, _, _, err := periodBounds("week", 53, now, loc); err == nil {
		t.Fatal("offset limit")
	}
	mFrom, _, _, mLabel, _ := periodBounds("month", 1, now, loc)
	if mFrom.Month() != time.October || mLabel != "2026 年 10 月" {
		t.Fatalf("month = %v %s", mFrom, mLabel)
	}
	tr := func(title string) contract.Track {
		return contract.Track{Source: "wy", SongID: title, Title: title, Artist: "歌手" + title[:1], DurationMs: 200000}
	}
	at := func(day, hour, min int) int64 { return time.Date(2026, 11, day, hour, min, 0, 0, loc).UnixMilli() }
	var plays []store.PlayRow
	for i := 0; i < 4; i++ {
		plays = append(plays, store.PlayRow{TS: at(17, 20, i), WorkKey: "a1", Track: tr("a1"), PlayedMs: 200000, Reason: "completed"})
	}
	plays = append(plays, store.PlayRow{TS: at(18, 2, 30), WorkKey: "b1", Track: tr("b1"), PlayedMs: 120000, Reason: "stopped"})
	works := map[string]int64{"a1": at(1, 0, 0), "b1": at(18, 2, 30)}
	artists := map[string]int64{contract.NormalizeArtist("歌手a"): at(1, 0, 0), contract.NormalizeArtist("歌手b"): at(18, 2, 30)}
	rep := BuildReport(plays, ReportPrevious{ListeningMinutes: 10}, works, artists, "week", from, to, label, loc, now)
	if rep.Empty || rep.Plays != 5 || rep.NewTracks != 1 || rep.NewArtists != 1 || len(rep.Discoveries) != 1 || rep.Discoveries[0].Name != "歌手b" {
		t.Fatalf("report = %+v", rep)
	}
	if rep.MostRepeated == nil || rep.MostRepeated.Plays != 4 || rep.LatestNight == nil || rep.LatestNight.Track.Title != "b1" {
		t.Fatalf("highlights data = %+v %+v", rep.MostRepeated, rep.LatestNight)
	}
	if rep.FirstTrack == nil || rep.FirstTrack.Track.Title != "a1" || len(rep.Highlights) < 3 {
		t.Fatalf("first / highlights = %+v %v", rep.FirstTrack, rep.Highlights)
	}
	empty := BuildReport(nil, ReportPrevious{}, works, artists, "week", from, to, label, loc, now)
	if !empty.Empty || empty.FirstTrack != nil || len(empty.Highlights) != 0 {
		t.Fatalf("empty report = %+v", empty)
	}
}
