package rec

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/store"
)

// 听歌报告（docs/music-personalization-2.md「听歌报告」）。

// ReportTrackAt 某一刻的一首歌。
type ReportTrackAt struct {
	Track contract.Track `json:"track"`
	At    int64          `json:"at"`
}

// ReportRepeat 单曲循环最多的一天。
type ReportRepeat struct {
	Track contract.Track `json:"track"`
	Day   string         `json:"day"`
	Plays int64          `json:"plays"`
}

// ReportDay 某一天。
type ReportDay struct {
	Day     string `json:"day"`
	Minutes int64  `json:"minutes"`
}

// ReportArtist 报告里的歌手。
type ReportArtist struct {
	Name    string `json:"name"`
	Plays   int64  `json:"plays"`
	Minutes int64  `json:"minutes"`
	Cover   string `json:"cover"`
}

// ReportDiscovery 这一期新认识的歌手。
type ReportDiscovery struct {
	Name    string `json:"name"`
	FirstAt int64  `json:"firstAt"`
	Plays   int64  `json:"plays"`
	Cover   string `json:"cover"`
}

// Report 听歌报告。
type Report struct {
	Period           string            `json:"period"`
	From             int64             `json:"from"`
	To               int64             `json:"to"`
	Label            string            `json:"label"`
	ListeningMinutes int64             `json:"listeningMinutes"`
	Plays            int64             `json:"plays"`
	Tracks           int64             `json:"tracks"`
	Artists          int64             `json:"artists"`
	NewTracks        int64             `json:"newTracks"`
	NewArtists       int64             `json:"newArtists"`
	ActiveDays       int               `json:"activeDays"`
	Previous         ReportPrevious    `json:"previous"`
	TopTracks        []ProfileTrack    `json:"topTracks"`
	TopArtists       []ReportArtist    `json:"topArtists"`
	Discoveries      []ReportDiscovery `json:"discoveries"`
	FirstTrack       *ReportTrackAt    `json:"firstTrack"`
	LatestNight      *ReportTrackAt    `json:"latestNight"`
	MostRepeated     *ReportRepeat     `json:"mostRepeated"`
	PeakDay          *ReportDay        `json:"peakDay"`
	Hours            []int64           `json:"hours"`
	Weekdays         []int64           `json:"weekdays"`
	Months           []int64           `json:"months"`
	Persona          Persona           `json:"persona"`
	Highlights       []string          `json:"highlights"`
	Empty            bool              `json:"empty"`
}

// ReportPrevious 上一期的对比数。
type ReportPrevious struct {
	ListeningMinutes int64 `json:"listeningMinutes"`
	Plays            int64 `json:"plays"`
}

// periodBounds 第 offset 期（0 = 本期）的起止（服务器时区）。
func periodBounds(period string, offset int, now time.Time, loc *time.Location) (from, to, prevFrom time.Time, label string, err error) {
	l := now.In(loc)
	switch period {
	case "week":
		if offset < 0 || offset > 52 {
			return from, to, prevFrom, "", ErrBadRequest{"周报最多回看 52 周"}
		}
		wd := (int(l.Weekday()) + 6) % 7
		from = time.Date(l.Year(), l.Month(), l.Day()-wd-7*offset, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 0, 7)
		prevFrom = from.AddDate(0, 0, -7)
		last := to.AddDate(0, 0, -1)
		label = fmt.Sprintf("%d 月 %d 日至 %d 月 %d 日", int(from.Month()), from.Day(), int(last.Month()), last.Day())
	case "month":
		if offset < 0 || offset > 24 {
			return from, to, prevFrom, "", ErrBadRequest{"月报最多回看 24 个月"}
		}
		from = time.Date(l.Year(), l.Month()-time.Month(offset), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
		prevFrom = from.AddDate(0, -1, 0)
		label = fmt.Sprintf("%d 年 %d 月", from.Year(), int(from.Month()))
	case "year":
		if offset < 0 || offset > 5 {
			return from, to, prevFrom, "", ErrBadRequest{"年报最多回看 5 年"}
		}
		from = time.Date(l.Year()-offset, 1, 1, 0, 0, 0, 0, loc)
		to = from.AddDate(1, 0, 0)
		prevFrom = from.AddDate(-1, 0, 0)
		label = fmt.Sprintf("%d 年", from.Year())
	default:
		return from, to, prevFrom, "", ErrBadRequest{"period 只能是 week、month、year"}
	}
	return from, to, prevFrom, label, nil
}

var periodWord = map[string][2]string{"week": {"这周", "上周"}, "month": {"这个月", "上个月"}, "year": {"今年", "去年"}}

// GetReport 听歌报告（本期缓存 30 分钟，往期缓存 1 天）。
func (s *Service) GetReport(ctx context.Context, appID, userID int64, period string, offset int, useCache bool) (*Report, error) {
	if appID <= 0 || userID <= 0 {
		return nil, ErrBadRequest{"缺少应用或用户"}
	}
	now := time.Now()
	from, to, prevFrom, label, err := periodBounds(period, offset, now, s.Loc)
	if err != nil {
		return nil, err
	}
	cacheKey := s.RT.UserKey(appID, userID, "report:"+period+":"+strconv.FormatInt(from.Unix(), 10))
	if useCache {
		if raw, err := s.RT.Client().Get(ctx, cacheKey).Bytes(); err == nil {
			var r Report
			if json.Unmarshal(raw, &r) == nil {
				return &r, nil
			}
		}
	}
	all, err := s.CH.UserPlays(ctx, appID, userID, prevFrom)
	if err != nil {
		return nil, err
	}
	var cur []store.PlayRow
	prev := ReportPrevious{}
	for _, r := range all {
		switch {
		case r.TS >= from.UnixMilli() && r.TS < to.UnixMilli():
			cur = append(cur, r)
		case r.TS < from.UnixMilli():
			if isPlay(r) {
				prev.Plays++
				prev.ListeningMinutes += r.PlayedMs
			}
		}
	}
	prev.ListeningMinutes /= 60000
	works, artists, err := s.CH.UserFirstSeen(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	rep := BuildReport(cur, prev, works, artists, period, from, to, label, s.Loc, now)
	ttl := 30 * time.Minute
	if offset > 0 {
		ttl = 24 * time.Hour
	}
	if raw, err := json.Marshal(rep); err == nil {
		_ = s.RT.Client().Set(ctx, cacheKey, raw, ttl).Err()
	}
	return rep, nil
}

func isPlay(r store.PlayRow) bool { return r.PlayedMs >= 3000 || r.Reason == contract.ReasonCompleted }

// BuildReport 纯函数：一期的播放 → 报告（便于测试）。
func BuildReport(plays []store.PlayRow, prev ReportPrevious, firstWork, firstArtist map[string]int64, period string,
	from, to time.Time, label string, loc *time.Location, now time.Time) *Report {
	days := int(math.Ceil(to.Sub(from).Hours() / 24))
	p := BuildProfile(plays, 0, 0, days, loc, now)
	rep := &Report{Period: period, From: from.UnixMilli(), To: to.UnixMilli(), Label: label, Previous: prev,
		ListeningMinutes: p.ListeningMinutes, Plays: p.Plays, Tracks: p.Tracks, Artists: p.Artists, ActiveDays: p.ActiveDays,
		TopTracks: p.TopTracks, TopArtists: []ReportArtist{}, Discoveries: []ReportDiscovery{}, Hours: p.Hours, Weekdays: p.Weekdays,
		Months: []int64{}, Persona: p.Persona, Highlights: []string{}}
	if len(rep.TopTracks) > 10 {
		rep.TopTracks = rep.TopTracks[:10]
	}
	if period == "year" {
		rep.Months = make([]int64, 12)
	}
	covers := map[string]string{} // 规范化歌手 → 封面
	type dayWork struct{ day, work string }
	repeat := map[dayWork]int64{}
	repeatTrack := map[string]contract.Track{}
	dayMs := map[string]int64{}
	artistFirstIn := map[string]int64{}
	artistPlays := map[string]int64{}
	artistName := map[string]string{}
	newWorks := map[string]bool{}
	bestNight := -1
	for _, r := range plays {
		if !isPlay(r) {
			continue
		}
		t := time.UnixMilli(r.TS).In(loc)
		day := t.Format("2006-01-02")
		if rep.FirstTrack == nil {
			rep.FirstTrack = &ReportTrackAt{Track: r.Track, At: r.TS}
		}
		if h := t.Hour(); h < 5 {
			if m := h*60 + t.Minute(); m > bestNight {
				bestNight = m
				rep.LatestNight = &ReportTrackAt{Track: r.Track, At: r.TS}
			}
		}
		repeat[dayWork{day, r.WorkKey}]++
		repeatTrack[r.WorkKey] = r.Track
		dayMs[day] += r.PlayedMs
		if period == "year" {
			rep.Months[int(t.Month())-1] += r.PlayedMs / 60000
		}
		a := contract.NormalizeArtist(contract.FirstArtist(r.Track.Artist))
		if a != "" {
			if covers[a] == "" && r.Track.Cover != "" {
				covers[a] = r.Track.Cover
			}
			artistPlays[a]++
			artistName[a] = contract.FirstArtist(r.Track.Artist)
			if first, ok := firstArtist[a]; ok && first >= from.UnixMilli() && first < to.UnixMilli() {
				artistFirstIn[a] = first
			}
		}
		if first, ok := firstWork[r.WorkKey]; ok && first >= from.UnixMilli() && first < to.UnixMilli() {
			newWorks[r.WorkKey] = true
		}
	}
	rep.NewTracks = int64(len(newWorks))
	rep.NewArtists = int64(len(artistFirstIn))
	for _, a := range p.TopArtists {
		if len(rep.TopArtists) >= 10 {
			break
		}
		rep.TopArtists = append(rep.TopArtists, ReportArtist{Name: a.Name, Plays: a.Plays, Minutes: a.Minutes,
			Cover: covers[contract.NormalizeArtist(a.Name)]})
	}
	disc := make([]string, 0, len(artistFirstIn))
	for a := range artistFirstIn {
		disc = append(disc, a)
	}
	sort.Slice(disc, func(x, y int) bool {
		if artistPlays[disc[x]] != artistPlays[disc[y]] {
			return artistPlays[disc[x]] > artistPlays[disc[y]]
		}
		return disc[x] < disc[y]
	})
	for _, a := range disc[:min(5, len(disc))] {
		rep.Discoveries = append(rep.Discoveries, ReportDiscovery{Name: artistName[a], FirstAt: artistFirstIn[a], Plays: artistPlays[a], Cover: covers[a]})
	}
	var best dayWork
	var bestN int64
	for k, n := range repeat {
		if n > bestN || n == bestN && (k.day < best.day || k.day == best.day && k.work < best.work) {
			best, bestN = k, n
		}
	}
	if bestN >= 3 {
		rep.MostRepeated = &ReportRepeat{Track: repeatTrack[best.work], Day: best.day, Plays: bestN}
	}
	var peak string
	for d, ms := range dayMs {
		if peak == "" || ms > dayMs[peak] || ms == dayMs[peak] && d < peak {
			peak = d
		}
	}
	if peak != "" && dayMs[peak] >= 60000 {
		rep.PeakDay = &ReportDay{Day: peak, Minutes: dayMs[peak] / 60000}
	}
	rep.Empty = rep.Plays == 0
	// 亮点句子
	words := periodWord[period]
	if !rep.Empty {
		line := fmt.Sprintf("%s你听了 %d 分钟音乐", words[0], rep.ListeningMinutes)
		if prev.ListeningMinutes > 0 {
			diff := float64(rep.ListeningMinutes-prev.ListeningMinutes) / float64(prev.ListeningMinutes) * 100
			switch {
			case diff >= 1:
				line += fmt.Sprintf("，比%s多 %.0f%%", words[1], diff)
			case diff <= -1:
				line += fmt.Sprintf("，比%s少 %.0f%%", words[1], -diff)
			default:
				line += "，和" + words[1] + "差不多"
			}
		}
		rep.Highlights = append(rep.Highlights, line)
		if len(rep.TopArtists) > 0 {
			rep.Highlights = append(rep.Highlights, fmt.Sprintf("听得最多的歌手是 %s，共 %d 次", rep.TopArtists[0].Name, rep.TopArtists[0].Plays))
		}
		if rep.NewArtists > 0 {
			rep.Highlights = append(rep.Highlights, fmt.Sprintf("新认识了 %d 位歌手", rep.NewArtists))
		}
		if rep.MostRepeated != nil {
			rep.Highlights = append(rep.Highlights, fmt.Sprintf("%s 那天，《%s》你听了 %d 遍", rep.MostRepeated.Day, rep.MostRepeated.Track.Title, rep.MostRepeated.Plays))
		}
		if rep.LatestNight != nil {
			t := time.UnixMilli(rep.LatestNight.At).In(loc)
			rep.Highlights = append(rep.Highlights, fmt.Sprintf("最晚的一次是 %s，在听《%s》", t.Format("1 月 2 日 15:04"), rep.LatestNight.Track.Title))
		}
	}
	return rep
}
