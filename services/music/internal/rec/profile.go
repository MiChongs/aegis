package rec

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/signal"
	"aegis/services/music/internal/store"
)

// Profile 听歌画像（客户端与控制台同形，docs「画像」）。
type Profile struct {
	Days             int                 `json:"days"`
	ListeningMinutes int64               `json:"listeningMinutes"`
	Plays            int64               `json:"plays"`
	Tracks           int64               `json:"tracks"`
	Artists          int64               `json:"artists"`
	CompletionRate   float64             `json:"completionRate"`
	SkipRate         float64             `json:"skipRate"`
	Likes            int64               `json:"likes"`
	ActiveDays       int                 `json:"activeDays"`
	LongestStreak    int                 `json:"longestStreak"`
	NewTrackShare    float64             `json:"newTrackShare"`
	TopArtists       []ProfileArtist     `json:"topArtists"`
	TopTracks        []ProfileTrack      `json:"topTracks"`
	Hours            []int64             `json:"hours"`
	Weekdays         []int64             `json:"weekdays"`
	Sources          []store.SourceCount `json:"sources"`
	Persona          Persona             `json:"persona"`
	Traits           []Trait             `json:"traits"`
	UpdatedAt        int64               `json:"updatedAt"`
}

// ProfileArtist 常听歌手。
type ProfileArtist struct {
	Name    string  `json:"name"`
	Plays   int64   `json:"plays"`
	Minutes int64   `json:"minutes"`
	Share   float64 `json:"share"`
}

// ProfileTrack 常听歌曲。
type ProfileTrack struct {
	Track   contract.Track `json:"track"`
	Plays   int64          `json:"plays"`
	Minutes int64          `json:"minutes"`
}

// Persona 一句话画像。
type Persona struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Trait 口味特征（0–1）。
type Trait struct {
	Key   string  `json:"key"`
	Title string  `json:"title"`
	Value float64 `json:"value"`
}

// ValidDays 画像可选的天数。
func ValidDays(d int) int {
	switch d {
	case 7, 30, 90, 365:
		return d
	}
	return 30
}

const profileCacheTTL = 10 * time.Minute

// Profile 画像（Redis 缓存 10 分钟；清除数据时一并删除）。
func (s *Service) Profile(ctx context.Context, appID, userID int64, days int, useCache bool) (*Profile, error) {
	days = ValidDays(days)
	key := s.RT.UserKey(appID, userID, "profile:"+strconv.Itoa(days))
	if useCache {
		if raw, err := s.RT.Client().Get(ctx, key).Bytes(); err == nil {
			var p Profile
			if json.Unmarshal(raw, &p) == nil {
				return &p, nil
			}
		}
	}
	now := time.Now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	plays, err := s.CH.UserPlays(ctx, appID, userID, from)
	if err != nil {
		return nil, fmt.Errorf("读取播放记录失败：%w", err)
	}
	likes, oldTracks, err := s.CH.UserCounts(ctx, appID, userID, from)
	if err != nil {
		return nil, fmt.Errorf("读取收藏失败：%w", err)
	}
	p := BuildProfile(plays, likes, oldTracks, days, s.Loc, now)
	if raw, err := json.Marshal(p); err == nil {
		if err := s.RT.Client().Set(ctx, key, raw, profileCacheTTL).Err(); err != nil {
			s.Log.Warn("cache profile failed", zap.Error(err))
		}
	}
	return p, nil
}

// BuildProfile 由播放记录算画像（纯函数，单测覆盖）。
func BuildProfile(plays []store.PlayRow, likes, oldTracks int64, days int, loc *time.Location, now time.Time) *Profile {
	p := &Profile{Days: days, Hours: make([]int64, 24), Weekdays: make([]int64, 7), TopArtists: []ProfileArtist{},
		TopTracks: []ProfileTrack{}, Sources: []store.SourceCount{}, Traits: []Trait{}, UpdatedAt: now.UnixMilli(), Likes: max(likes, 0)}
	type trackAcc struct {
		track contract.Track
		plays int64
		ms    int64
		last  int64
	}
	type artistAcc struct {
		plays int64
		ms    int64
	}
	tracks := map[string]*trackAcc{}
	artists := map[string]*artistAcc{}
	sources := map[string]int64{}
	activeDays := map[string]bool{}
	var ends, completes, skips, totalMs, nightMs, morningMs, commuteMs int64
	for i := range plays {
		r := &plays[i]
		ev := contract.Event{Type: contract.TypePlayEnd, PlayedMs: r.PlayedMs, Reason: r.Reason, Track: &contract.Track{DurationMs: r.Track.DurationMs}}
		k := signal.Classify(&ev)
		ends++
		switch k {
		case signal.KindComplete:
			completes++
		case signal.KindSkip:
			skips++
		}
		if r.PlayedMs < 3000 && r.Reason != contract.ReasonCompleted {
			continue
		}
		p.Plays++
		totalMs += r.PlayedMs
		at := time.UnixMilli(r.TS).In(loc)
		h := at.Hour()
		p.Hours[h] += r.PlayedMs
		p.Weekdays[(int(at.Weekday())+6)%7] += r.PlayedMs
		switch {
		case h >= 22 || h < 4:
			nightMs += r.PlayedMs
		case h >= 5 && h < 8:
			morningMs += r.PlayedMs
		}
		if (h >= 7 && h < 9) || (h >= 17 && h < 19) {
			commuteMs += r.PlayedMs
		}
		activeDays[at.Format("2006-01-02")] = true
		t := tracks[r.WorkKey]
		if t == nil {
			t = &trackAcc{}
			tracks[r.WorkKey] = t
		}
		t.plays++
		t.ms += r.PlayedMs
		if r.TS >= t.last {
			t.last, t.track = r.TS, r.Track
		}
		for _, a := range contract.SplitArtists(r.Track.Artist) {
			acc := artists[a]
			if acc == nil {
				acc = &artistAcc{}
				artists[a] = acc
			}
			acc.plays++
			acc.ms += r.PlayedMs
		}
		if r.Track.Source != "" {
			sources[r.Track.Source]++
		}
	}
	toMin := func(ms int64) int64 { return int64(math.Round(float64(ms) / 60000)) }
	for h := range p.Hours {
		p.Hours[h] = toMin(p.Hours[h])
	}
	for d := range p.Weekdays {
		p.Weekdays[d] = toMin(p.Weekdays[d])
	}
	p.ListeningMinutes = toMin(totalMs)
	p.Tracks, p.Artists = int64(len(tracks)), int64(len(artists))
	if ends > 0 {
		p.CompletionRate = round4(float64(completes) / float64(ends))
		p.SkipRate = round4(float64(skips) / float64(ends))
	}
	p.ActiveDays = len(activeDays)
	p.LongestStreak = longestStreak(activeDays, loc)
	if len(tracks) > 0 {
		p.NewTrackShare = round4(1 - float64(min(oldTracks, int64(len(tracks))))/float64(len(tracks)))
	}

	type kv struct {
		key string
		ms  int64
		n   int64
	}
	var al []kv
	for name, a := range artists {
		al = append(al, kv{name, a.ms, a.plays})
	}
	sort.Slice(al, func(a, b int) bool {
		if al[a].ms != al[b].ms {
			return al[a].ms > al[b].ms
		}
		return al[a].key < al[b].key
	})
	for k, a := range al {
		if k >= 10 {
			break
		}
		share := 0.0
		if totalMs > 0 {
			share = round4(float64(a.ms) / float64(totalMs))
		}
		p.TopArtists = append(p.TopArtists, ProfileArtist{Name: a.key, Plays: a.n, Minutes: toMin(a.ms), Share: share})
	}
	var tl []*trackAcc
	for _, t := range tracks {
		tl = append(tl, t)
	}
	sort.Slice(tl, func(a, b int) bool {
		if tl[a].plays != tl[b].plays {
			return tl[a].plays > tl[b].plays
		}
		if tl[a].ms != tl[b].ms {
			return tl[a].ms > tl[b].ms
		}
		return tl[a].track.Key() < tl[b].track.Key()
	})
	for k, t := range tl {
		if k >= 10 {
			break
		}
		p.TopTracks = append(p.TopTracks, ProfileTrack{Track: t.track, Plays: t.plays, Minutes: toMin(t.ms)})
	}
	for src, n := range sources {
		p.Sources = append(p.Sources, store.SourceCount{Source: src, Plays: n})
	}
	sort.Slice(p.Sources, func(a, b int) bool {
		if p.Sources[a].Plays != p.Sources[b].Plays {
			return p.Sources[a].Plays > p.Sources[b].Plays
		}
		return p.Sources[a].Source < p.Sources[b].Source
	})

	share := func(v int64) float64 {
		if totalMs <= 0 {
			return 0
		}
		return float64(v) / float64(totalMs)
	}
	topShare := 0.0
	if len(p.TopArtists) > 0 {
		topShare = p.TopArtists[0].Share
	}
	p.Traits = []Trait{
		{Key: "loyal", Title: "专一", Value: round4(math.Min(1, topShare/0.4))},
		{Key: "explorer", Title: "探索", Value: round4(p.NewTrackShare)},
		{Key: "completionist", Title: "完播", Value: p.CompletionRate},
		{Key: "night_owl", Title: "夜听", Value: round4(share(nightMs))},
		{Key: "variety", Title: "多元", Value: round4(math.Min(1, float64(p.Artists)/math.Max(1, float64(p.Tracks))*1.5))},
	}
	p.Persona = persona(p, share(nightMs), share(morningMs), share(commuteMs), topShare)
	return p
}

func persona(p *Profile, night, morning, commute, topShare float64) Persona {
	pct := func(v float64) string { return strconv.Itoa(int(math.Round(v*100))) + "%" }
	switch {
	case p.Plays == 0:
		return Persona{Key: "casual", Title: "初来乍到", Text: "多听几首，画像会越来越准"}
	case night >= 0.35:
		return Persona{Key: "night_owl", Title: "夜猫子", Text: "你 " + pct(night) + " 的音乐时间在 22 点之后"}
	case morning >= 0.3:
		return Persona{Key: "early_bird", Title: "早起派", Text: "你 " + pct(morning) + " 的音乐时间在清晨"}
	case commute >= 0.4:
		return Persona{Key: "commuter", Title: "通勤党", Text: "你 " + pct(commute) + " 的音乐时间在上下班路上"}
	case topShare >= 0.25 && len(p.TopArtists) > 0:
		return Persona{Key: "loyal", Title: "专一型", Text: p.TopArtists[0].Name + " 占了你 " + pct(topShare) + " 的音乐时间"}
	case p.NewTrackShare >= 0.5:
		return Persona{Key: "explorer", Title: "探索型", Text: "你听的歌里 " + pct(p.NewTrackShare) + " 是新发现"}
	case p.CompletionRate >= 0.85:
		return Persona{Key: "completionist", Title: "完播型", Text: "你 " + pct(p.CompletionRate) + " 的歌都会听完"}
	}
	return Persona{Key: "casual", Title: "随性型", Text: "什么好听就听什么"}
}

func longestStreak(days map[string]bool, loc *time.Location) int {
	if len(days) == 0 {
		return 0
	}
	list := make([]time.Time, 0, len(days))
	for d := range days {
		t, err := time.ParseInLocation("2006-01-02", d, loc)
		if err == nil {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Before(list[b]) })
	best, cur := 1, 1
	for i := 1; i < len(list); i++ {
		if list[i].Sub(list[i-1]) <= 25*time.Hour {
			cur++
		} else {
			cur = 1
		}
		best = max(best, cur)
	}
	return best
}
