package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	musicv1 "aegis/api/gen/music/v1"
	"aegis/api/gen/music/v1/musicv1connect"
	"aegis/services/music/contract"
)

// TestMusicPhase2IT 第二期（docs/music-personalization-2.md）：精排样本与训练、实时折入、续播、雷达、风格歌单、
// 歌手推荐、新用户偏好、偏好设置、不喜欢与撤销、报告、热搜、重排、导出、运营规则、实验。
func TestMusicPhase2IT(t *testing.T) {
	if os.Getenv("MUSIC_IT") != "1" {
		t.Skip("MUSIC_IT=1 才跑（需要先起好三个音乐服务）")
	}
	app := 980000000 + time.Now().Unix()%1000000
	e := &env{t: t, base: os.Getenv("MUSIC_IT_RPC"), token: os.Getenv("MUSIC_INTERNAL_TOKEN"), app: app}
	httpc := &http.Client{Timeout: 60 * time.Second}
	e.client = musicv1connect.NewMusicServiceClient(httpc, e.base, connect.WithInterceptors(connect.UnaryInterceptorFunc(
		func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				req.Header().Set("Authorization", "Bearer "+e.token)
				return next(ctx, req)
			}
		})))
	ctx := context.Background()
	nc, err := nats.Connect(os.Getenv("MUSIC_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)

	run := time.Now().UnixNano()
	now := time.Now()
	seq := 0
	track := func(item int) *contract.Track {
		return &contract.Track{Source: "wy", SongID: fmt.Sprintf("20%02d", item), Title: fmt.Sprintf("二期歌曲%02d", item),
			Artist: fmt.Sprintf("二期歌手%d", item/5), DurationMs: 240000, Cover: fmt.Sprintf("https://example.com/%d.jpg", item)}
	}
	mk := func(user int64, typ string, item int, at time.Time, mut func(*contract.ClientEvent)) contract.Event {
		seq++
		ev := contract.ClientEvent{ID: fmt.Sprintf("p2%d-%d", run, seq), Type: typ, TS: at.UnixMilli(), SessionID: fmt.Sprintf("s%d", user)}
		if item >= 0 {
			ev.Track = track(item)
		}
		if typ == contract.TypePlayEnd {
			ev.PlayedMs, ev.Reason = 238000, contract.ReasonCompleted
		}
		if mut != nil {
			mut(&ev)
		}
		if r := contract.Normalize(&ev, now); r != "" {
			t.Fatalf("event rejected: %s", r)
		}
		return contract.Enrich(ev, contract.ClientInfo{Platform: "android", AppVersion: "it"}, app, user, "dev", "203.0.113.9", now)
	}
	publish := func(list []contract.Event) {
		for _, ev := range list {
			raw, _ := json.Marshal(ev)
			msg := &nats.Msg{Subject: contract.EventSubject(app), Data: raw, Header: nats.Header{}}
			msg.Header.Set(jetstream.MsgIDHeader, contract.MsgID(ev.AppID, ev.UserID, ev.ID))
			if _, err := js.PublishMsg(ctx, msg); err != nil {
				t.Fatal(err)
			}
		}
	}
	op := func(user int64, name string, params any, out any) error {
		raw, _ := json.Marshal(params)
		resp, err := e.client.UserOp(ctx, connect.NewRequest(&musicv1.UserOpRequest{AppId: app, UserId: user, Op: name, ParamsJson: raw, Personalized: true}))
		if err != nil {
			return err
		}
		if out != nil {
			return json.Unmarshal(resp.Msg.GetResultJson(), out)
		}
		return nil
	}
	group := func(item int) int { return item / 30 }

	// ── 数据：1–40 号听 A 组（0–29），41–80 号听 B 组（30–59），每人 18 首；搜索「二期热词」的有 10 人 ──
	var events []contract.Event
	for u := int64(1); u <= 80; u++ {
		g := 0
		if u > 40 {
			g = 30
		}
		at := now.Add(-30 * time.Hour)
		for k := 0; k < 18; k++ {
			item := g + int((u*7+int64(k)*11)%30)
			at = at.Add(4 * time.Minute)
			events = append(events, mk(u, contract.TypePlayStart, item, at.Add(-4*time.Minute), nil), mk(u, contract.TypePlayEnd, item, at, nil))
			if k < 2 {
				events = append(events, mk(u, contract.TypeLike, item, at.Add(time.Second), nil))
			}
		}
		if u <= 10 {
			events = append(events, mk(u, contract.TypeSearch, -1, now.Add(-time.Hour), func(ev *contract.ClientEvent) { ev.Context.Query = "二期热词" }))
		}
	}
	// 2 号：听腻了 A 组的 3 号歌；音质差 4 号歌的 wy 版本
	events = append(events, mk(2, contract.TypeDislike, 3, now.Add(-time.Hour), func(ev *contract.ClientEvent) {
		ev.Props = map[string]string{"reason": contract.DislikeHeardTooMuch}
	}), mk(2, contract.TypeDislike, 4, now.Add(-time.Hour), func(ev *contract.ClientEvent) {
		ev.Props = map[string]string{"reason": contract.DislikePoorQuality}
	}))
	publish(events)
	var ov struct {
		Totals struct{ Plays int64 }
	}
	waitFor(t, "ingest", 60*time.Second, func() bool {
		e.admin("GET", "overview?days=7", nil, &ov)
		return ov.Totals.Plays >= 80*18
	})

	// 精排门槛调低（测试数据量小），训练第一版
	var cfg map[string]any
	e.admin("GET", "config", nil, &cfg)
	cfg["ranker"] = map[string]any{"enabled": true, "minSamples": 200, "l2": 0.001, "epochs": 8}
	if code := e.admin("PUT", "config", map[string]any{"ranker": cfg["ranker"]}, nil); code != 200 {
		t.Fatalf("partial config put = %d", code)
	}
	e.admin("GET", "config", nil, &cfg)
	if cfg["artistCap"] == nil || cfg["foldIn"] != true {
		t.Fatalf("partial PUT should keep other fields: %v", cfg)
	}
	train := func(note string) int64 {
		var run struct{ ID int64 }
		if code := e.admin("POST", "models/train", map[string]string{"note": note}, &run); code != 200 {
			t.Fatalf("train %s = %d", note, code)
		}
		waitFor(t, "train "+note, 120*time.Second, func() bool {
			var d struct {
				Status      string
				PublishedAt *int64 `json:"publishedAt"`
			}
			e.admin("GET", fmt.Sprintf("models/%d", run.ID), nil, &d)
			if d.Status == "failed" {
				t.Fatalf("train %s failed", note)
			}
			return d.Status == "succeeded" && d.PublishedAt != nil
		})
		time.Sleep(2 * time.Second) // 等 music-rec 热加载
		return run.ID
	}
	train("p2-1")

	rec := func(user int64, scene string, mut func(*musicv1.RecommendRequest)) *musicv1.RecommendResponse {
		t.Helper()
		req := &musicv1.RecommendRequest{AppId: app, UserId: user, Scene: scene, Limit: 20, Personalized: true}
		if mut != nil {
			mut(req)
		}
		resp, err := e.client.Recommend(ctx, connect.NewRequest(req))
		if err != nil {
			t.Fatalf("recommend %s for %d: %v", scene, user, err)
		}
		return resp.Msg
	}
	itemOf := func(t *musicv1.Track) int {
		var n int
		fmt.Sscanf(t.GetSongId(), "20%02d", &n)
		return n
	}

	// ── 精排样本：每人拿两次首页，展示后点自己那组的歌 ──
	var clicks []contract.Event
	for round := 0; round < 2; round++ {
		for u := int64(1); u <= 80; u++ {
			r := rec(u, "home", func(q *musicv1.RecommendRequest) { q.Cursor = fmt.Sprint(round) })
			if len(r.Items) == 0 {
				continue
			}
			mine := 0
			if u > 40 {
				mine = 1
			}
			clicks = append(clicks, mk(u, contract.TypeImpression, -1, time.Now(), func(ev *contract.ClientEvent) {
				ev.Context.RecID, ev.Context.Scene = r.RecId, "home"
			}))
			for pos, it := range r.Items {
				item := itemOf(it.Track)
				if group(item) == mine && pos%2 == 0 {
					clicks = append(clicks, mk(u, contract.TypePlayStart, item, time.Now(), func(ev *contract.ClientEvent) {
						ev.Context.RecID, ev.Context.Scene, ev.Context.Position = r.RecId, "home", int32(pos)
					}))
				}
			}
		}
	}
	publish(clicks)
	time.Sleep(5 * time.Second)
	run2 := train("p2-ranker")
	var detail struct {
		Ranker *struct {
			Enabled          bool
			Samples          int
			AUC, BaselineAUC float64
			Reason           string
			Features         []struct{ Name string }
		}
	}
	e.admin("GET", fmt.Sprintf("models/%d", run2), nil, &detail)
	if detail.Ranker == nil || detail.Ranker.Samples < 200 || len(detail.Ranker.Features) != 16 {
		t.Fatalf("ranker info = %+v", detail.Ranker)
	}
	t.Logf("ranker: enabled=%v samples=%d auc=%.3f baseline=%.3f %s", detail.Ranker.Enabled, detail.Ranker.Samples,
		detail.Ranker.AUC, detail.Ranker.BaselineAUC, detail.Ranker.Reason)

	// ── 实时折入：模型里没有的 900 号，听了 A 组 6 首，首页应偏 A 组 ──
	var fresh []contract.Event
	for k := 0; k < 6; k++ {
		fresh = append(fresh, mk(900, contract.TypePlayEnd, k*4, time.Now().Add(-time.Duration(10-k)*time.Minute), nil))
	}
	publish(fresh)
	time.Sleep(3 * time.Second)
	// A 组 6 位歌手、前 20 首同一歌手最多 2 首：最多 12 首 A 组，前 10 首应全是 A 组
	r900 := rec(900, "home", nil)
	if r900.ColdStart || len(r900.Items) < 10 {
		t.Fatalf("fold-in: cold=%v items=%d", r900.ColdStart, len(r900.Items))
	}
	for k, it := range r900.Items[:10] {
		if group(itemOf(it.Track)) != 0 {
			t.Fatalf("fold-in: item %d is from group B (%d)", k, itemOf(it.Track))
		}
	}

	// ── 智能续播：种子是 B 组歌，排除队列 ──
	auto := rec(5, "autoplay", func(q *musicv1.RecommendRequest) {
		q.Limit = 10
		for _, i := range []int{31, 36, 41} {
			tr := track(i)
			q.Seeds = append(q.Seeds, &musicv1.Track{Source: tr.Source, SongId: tr.SongID, Title: tr.Title, Artist: tr.Artist})
		}
		q.Exclude = []string{"wy:2031", "wy:2036", "wy:2041", "wy:2046"}
	})
	inB := 0
	for _, it := range auto.Items {
		n := itemOf(it.Track)
		if n == 31 || n == 36 || n == 41 || n == 46 {
			t.Fatalf("autoplay returned an excluded track %d", n)
		}
		if group(n) == 1 {
			inB++
		}
	}
	if len(auto.Items) == 0 || inB < len(auto.Items)/2 {
		t.Fatalf("autoplay should follow the seeds: B=%d/%d", inB, len(auto.Items))
	}

	// ── 每周雷达：只出没听过的 ──
	disc := rec(7, "discover", nil)
	heard := map[int]bool{}
	for k := 0; k < 18; k++ {
		heard[int((7*7+int64(k)*11)%30)] = true
	}
	for _, it := range disc.Items {
		if heard[itemOf(it.Track)] {
			t.Fatalf("discover returned a heard track %d", itemOf(it.Track))
		}
	}
	disc2 := rec(7, "discover", nil)
	if disc2.RecId != disc.RecId {
		t.Fatal("discover should be fixed for the week")
	}

	// ── 歌手推荐场景 ──
	art := rec(3, "artist", func(q *musicv1.RecommendRequest) { q.Artist = "二期歌手2" })
	if len(art.Items) == 0 {
		t.Fatal("artist scene empty")
	}
	for _, it := range art.Items {
		if it.Track.GetArtist() != "二期歌手2" {
			t.Fatalf("artist scene returned %s", it.Track.GetArtist())
		}
	}

	// ── 风格歌单 ──
	var mixes struct {
		Items []struct {
			ID    string
			Title string
			Size  int
		}
	}
	if err := op(1, "mixes", map[string]any{}, &mixes); err != nil {
		t.Fatal(err)
	}
	t.Logf("mixes for user 1: %d", len(mixes.Items))
	if len(mixes.Items) > 0 {
		m := rec(1, "mix", func(q *musicv1.RecommendRequest) { q.MixId = mixes.Items[0].ID })
		if len(m.Items) != mixes.Items[0].Size {
			t.Fatalf("mix tracks = %d, want %d", len(m.Items), mixes.Items[0].Size)
		}
	}

	// ── 推荐歌手、新用户偏好、偏好设置 ──
	var artists struct {
		Items []struct {
			Name   string
			Tracks []json.RawMessage
		}
	}
	if err := op(3, "artists", map[string]any{"limit": 5}, &artists); err != nil || len(artists.Items) == 0 {
		t.Fatalf("recommended artists: %v %d", err, len(artists.Items))
	}
	var onb struct {
		Needed  bool
		Artists []struct{ Name string }
	}
	if err := op(5000, "onboarding", map[string]any{}, &onb); err != nil || !onb.Needed || len(onb.Artists) == 0 {
		t.Fatalf("onboarding for a new user: %v %+v", err, onb)
	}
	if err := op(5000, "prefs_put", map[string]any{"languages": []string{"zh"}, "artists": []string{"二期歌手9"}, "onboarded": true}, nil); err != nil {
		t.Fatal(err)
	}
	_ = op(5000, "onboarding", map[string]any{}, &onb)
	if onb.Needed {
		t.Fatal("onboarded user should not be asked again")
	}
	if err := op(5000, "prefs_put", map[string]any{"languages": []string{"xx"}}, nil); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("bad language accepted: %v", err)
	}
	r5000 := rec(5000, "home", nil)
	pick := 0
	for _, it := range r5000.Items {
		if it.Track.GetArtist() == "二期歌手9" {
			pick++
		}
	}
	if pick == 0 {
		t.Fatalf("preferred artist missing from a new user's home: %+v", r5000.Items)
	}
	// 关掉服务端个性化：不读个人数据
	_ = op(900, "prefs_put", map[string]any{"personalized": false}, nil)
	if rec(900, "home", nil).Personalized {
		t.Fatal("server-side personalized=false ignored")
	}

	// ── 不喜欢与撤销 ──
	var fb struct {
		Items []struct {
			Kind, WorkKey, Reason string
			Until                 int64
		}
		Total int
	}
	if err := op(2, "feedback_list", map[string]any{"kind": "suppressed"}, &fb); err != nil || fb.Total != 1 || fb.Items[0].Until == 0 {
		t.Fatalf("suppressed list: %v %+v", err, fb)
	}
	for _, it := range rec(2, "home", func(q *musicv1.RecommendRequest) { q.Limit = 50 }).Items {
		if n := itemOf(it.Track); n == 3 || n == 4 {
			t.Fatalf("suppressed / bad version %d recommended", n)
		}
	}
	if err := op(2, "feedback_undo", map[string]any{"kind": "suppressed", "workKey": fb.Items[0].WorkKey}, nil); err != nil {
		t.Fatal(err)
	}
	_ = op(2, "feedback_list", map[string]any{"kind": "suppressed"}, &fb)
	if fb.Total != 0 {
		t.Fatal("undo did not remove the suppression")
	}

	// ── 报告、热搜、重排、导出 ──
	var rep struct {
		Plays      int64
		Empty      bool
		TopTracks  []json.RawMessage
		Highlights []string
	}
	if err := op(1, "report", map[string]any{"period": "week"}, &rep); err != nil {
		t.Fatal(err)
	}
	var repMonth struct{ Plays int64 }
	_ = op(1, "report", map[string]any{"period": "month"}, &repMonth)
	if rep.Plays+repMonth.Plays == 0 || len(rep.Highlights) == 0 && rep.Plays > 0 {
		t.Fatalf("report = %+v month=%d", rep, repMonth.Plays)
	}
	var trend struct {
		Items []struct {
			Query string
			Users int64
		}
	}
	if err := op(1, "trending", map[string]any{}, &trend); err != nil || len(trend.Items) == 0 || trend.Items[0].Query != "二期热词" {
		t.Fatalf("trending: %v %+v", err, trend)
	}
	items := []contract.Track{*track(35), *track(36), *track(7)} // 7 号是 1 号收藏过的歌
	var rr struct {
		Order        []int
		Personalized bool
	}
	if err := op(1, "rerank", map[string]any{"query": "x", "items": items}, &rr); err != nil || !rr.Personalized || rr.Order[0] != 2 {
		t.Fatalf("rerank: %v %+v", err, rr)
	}
	var exp struct {
		Items      []struct{ ID, Type string }
		NextBefore *int64
	}
	if err := op(1, "export", map[string]any{"limit": 10}, &exp); err != nil || len(exp.Items) != 10 || exp.NextBefore == nil {
		t.Fatalf("export: %v %d", err, len(exp.Items))
	}

	// ── 运营规则：屏蔽 + 置顶 ──
	home := rec(10, "home", nil)
	if len(home.Items) < 3 {
		t.Fatal("home too short")
	}
	blocked := home.Items[0].Track
	var rule struct{ ID int64 }
	if code := e.admin("POST", "rules", map[string]any{"kind": "block", "target": map[string]any{"type": "work", "source": "wy", "songId": blocked.GetSongId()}}, &rule); code != 200 {
		t.Fatalf("create block rule = %d", code)
	}
	if code := e.admin("POST", "rules", map[string]any{"kind": "pin", "position": 2, "target": map[string]any{"type": "work", "source": "wy", "songId": "2059"}}, nil); code != 200 {
		t.Fatal("create pin rule")
	}
	if code := e.admin("POST", "rules", map[string]any{"kind": "boost", "weight": 50, "target": map[string]any{"type": "artist", "artist": "x"}}, nil); code != 400 {
		t.Fatal("boost weight out of range accepted")
	}
	time.Sleep(time.Second)
	home2 := rec(11, "home", nil)
	for _, it := range home2.Items {
		if it.Track.GetSongId() == blocked.GetSongId() {
			t.Fatal("blocked work recommended")
		}
	}
	if len(home2.Items) < 2 || home2.Items[1].Track.GetSongId() != "2059" || home2.Items[1].Reason.GetKind() != "featured" {
		t.Fatalf("pin not at position 2: %+v", home2.Items[:2])
	}
	if rec(11, "home", nil).Items[1].Track.GetSongId() == "2059" {
		t.Fatal("pin should show once per user per day")
	}
	var rules struct {
		Items []struct {
			ID    int64
			Stats struct{ Served int64 }
		}
	}
	e.admin("GET", "rules", nil, &rules)
	if len(rules.Items) != 2 {
		t.Fatalf("rules = %d", len(rules.Items))
	}

	// ── 实验 ──
	var ex struct {
		ID     int64
		Status string
	}
	body := map[string]any{"name": "探索对比", "scenes": []string{"radio"}, "variants": []map[string]any{
		{"key": "A", "name": "对照", "traffic": 50}, {"key": "B", "name": "多探索", "traffic": 50, "overrides": map[string]any{"exploreRatio": 0.4}},
	}}
	if code := e.admin("POST", "experiments", body, &ex); code != 200 {
		t.Fatalf("create experiment = %d", code)
	}
	if code := e.admin("POST", fmt.Sprintf("experiments/%d/start", ex.ID), nil, &ex); code != 200 || ex.Status != "running" {
		t.Fatalf("start experiment = %d %s", code, ex.Status)
	}
	var clash struct{ ID int64 }
	e.admin("POST", "experiments", body, &clash)
	if code := e.admin("POST", fmt.Sprintf("experiments/%d/start", clash.ID), nil, nil); code != 409 {
		t.Fatalf("second experiment on the same scene should conflict, got %d", code)
	}
	time.Sleep(time.Second)
	variants := map[string]int{}
	for u := int64(1); u <= 40; u++ {
		r := rec(u, "radio", nil)
		if r.ExperimentId != ex.ID {
			t.Fatalf("radio for %d not in experiment", u)
		}
		variants[r.Variant]++
	}
	if variants["A"] == 0 || variants["B"] == 0 {
		t.Fatalf("variants = %v", variants)
	}
	time.Sleep(4 * time.Second)
	var res struct {
		Variants []struct {
			Key      string
			Requests int64
			Users    int64
		}
	}
	e.admin("GET", fmt.Sprintf("experiments/%d/results?days=1", ex.ID), nil, &res)
	if len(res.Variants) != 2 || res.Variants[0].Requests == 0 {
		t.Fatalf("experiment results = %+v", res)
	}
	if code := e.admin("DELETE", fmt.Sprintf("experiments/%d", ex.ID), nil, nil); code != 409 {
		t.Fatal("running experiment deleted")
	}
	e.admin("POST", fmt.Sprintf("experiments/%d/stop", ex.ID), nil, nil)

	// ── 管理端统计 ──
	var search struct {
		Searches int64
		Top      []struct{ Query string }
	}
	e.admin("GET", "search?days=7", nil, &search)
	if search.Searches < 10 || len(search.Top) == 0 {
		t.Fatalf("search stats = %+v", search)
	}
	var arts struct {
		Items []struct{ Name string }
		Total int64
	}
	e.admin("GET", "artists?days=7", nil, &arts)
	if arts.Total < 10 {
		t.Fatalf("artists = %+v", arts)
	}
	var ad struct {
		Name      string
		Plays     int64
		TopTracks []json.RawMessage
		Similar   []struct{ Name string }
	}
	if code := e.admin("GET", "artists/"+strings.ReplaceAll(arts.Items[0].Name, " ", "%20")+"?days=7", nil, &ad); code != 200 || ad.Plays == 0 {
		t.Fatalf("artist detail %d %+v", code, ad)
	}
	var ov2 struct {
		Models          []struct{ Version int64 }
		FeedbackReasons []struct{ Reason string }
	}
	e.admin("GET", "overview?days=7", nil, &ov2)
	if len(ov2.Models) == 0 || len(ov2.FeedbackReasons) == 0 {
		t.Fatalf("overview extras = %+v", ov2)
	}
	var ud struct {
		Preferences *struct{ Personalized bool }
		Feedback    []json.RawMessage
		Realtime    struct{ Fatigue []json.RawMessage }
	}
	e.admin("GET", "users/2?days=7", nil, &ud)
	if len(ud.Feedback) == 0 || len(ud.Realtime.Fatigue) == 0 {
		t.Fatalf("user detail extras = %+v", ud)
	}
	var prev struct {
		Items []struct{ Channels map[string]float64 }
	}
	e.admin("GET", "users/1/recommendations?scene=autoplay&seeds=wy:2001,wy:2005", nil, &prev)
	if len(prev.Items) == 0 || prev.Items[0].Channels == nil {
		t.Fatal("autoplay preview empty")
	}

	// 清除：偏好一起删
	if _, err := e.client.ForgetUser(ctx, connect.NewRequest(&musicv1.ForgetUserRequest{AppId: app, UserId: 5000, Reason: "user"})); err != nil {
		t.Fatal(err)
	}
	_ = op(5000, "onboarding", map[string]any{}, &onb)
	if !onb.Needed {
		t.Fatal("forget should clear preferences")
	}
}
