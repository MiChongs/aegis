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

// TestMusicPhase3IT 第三期（docs/music-personalization-3.md）：大模型打标签与语义向量、歌手知识、曲库扩展（待解析项）、
// 风格歌单按标签起名、内容理解管理端；MUSIC_IT_DEEP=1 时再等 music-deep 训练发布深度模型并核对 seq 通道。
// 三个音乐服务要用 MUSIC_LLM_BASE_URL 指向 TestFakeLLMServe 起的假大模型。
func TestMusicPhase3IT(t *testing.T) {
	if os.Getenv("MUSIC_IT") != "1" {
		t.Skip("MUSIC_IT=1 才跑")
	}
	app := 970000000 + time.Now().Unix()%1000000
	e := &env{t: t, base: os.Getenv("MUSIC_IT_RPC"), token: os.Getenv("MUSIC_INTERNAL_TOKEN"), app: app}
	e.client = musicv1connect.NewMusicServiceClient(&http.Client{Timeout: 60 * time.Second}, e.base, connect.WithInterceptors(connect.UnaryInterceptorFunc(
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
		return &contract.Track{Source: "wy", SongID: fmt.Sprintf("30%02d", item), Title: fmt.Sprintf("三期歌曲%02d", item),
			Artist: fmt.Sprintf("三期歌手%d", item/5), DurationMs: 240000, Cover: "https://example.com/c.jpg"}
	}
	mk := func(user int64, typ string, item int, at time.Time) contract.Event {
		seq++
		ev := contract.ClientEvent{ID: fmt.Sprintf("p3%d-%d", run, seq), Type: typ, TS: at.UnixMilli(), SessionID: fmt.Sprintf("s%d", user), Track: track(item)}
		if typ == contract.TypePlayEnd {
			ev.PlayedMs, ev.Reason = 238000, contract.ReasonCompleted
		}
		if r := contract.Normalize(&ev, now); r != "" {
			t.Fatalf("rejected %s", r)
		}
		return contract.Enrich(ev, contract.ClientInfo{Platform: "android"}, app, user, "dev", "203.0.113.9", now)
	}
	var events []contract.Event
	for u := int64(1); u <= 60; u++ {
		g := 0
		if u > 30 {
			g = 30
		}
		at := now.Add(-20 * time.Hour)
		for k := 0; k < 16; k++ {
			item := g + int((u*7+int64(k)*11)%30)
			at = at.Add(3 * time.Minute)
			events = append(events, mk(u, contract.TypePlayEnd, item, at))
			if k < 2 {
				events = append(events, mk(u, contract.TypeLike, item, at.Add(time.Second)))
			}
		}
	}
	// 950 号：只听过 3 首 A 组（行为少，触发曲库扩展）
	for k := 0; k < 3; k++ {
		events = append(events, mk(950, contract.TypePlayEnd, k*3, now.Add(-time.Duration(30-k)*time.Minute)))
	}
	for _, ev := range events {
		raw, _ := json.Marshal(ev)
		msg := &nats.Msg{Subject: contract.EventSubject(app), Data: raw, Header: nats.Header{}}
		msg.Header.Set(jetstream.MsgIDHeader, contract.MsgID(ev.AppID, ev.UserID, ev.ID))
		if _, err := js.PublishMsg(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}

	// ── 打标签：enricher 每 30 秒一轮 ──
	var content struct {
		Tracks, Tagged, Embedded int64
		LLMConfigured            bool
		Genres                   []struct {
			Key   string
			Count int64
		}
		Today struct{ Calls, Tokens int64 }
	}
	waitFor(t, "tagging", 150*time.Second, func() bool {
		e.admin("GET", "content?days=7", nil, &content)
		return content.Tracks >= 60 && content.Tagged == content.Tracks && content.Embedded == content.Tracks
	})
	if !content.LLMConfigured || len(content.Genres) != 2 || content.Today.Calls == 0 {
		t.Fatalf("content overview = %+v", content)
	}
	var list struct {
		Items []struct {
			WorkKey string
			Status  string
			Tags    *struct{ Genres []string }
		}
		Total int64
	}
	e.admin("GET", "content/tracks?status=tagged&q=三期歌曲05", nil, &list)
	if list.Total != 1 || list.Items[0].Tags == nil || list.Items[0].Tags.Genres[0] != "pop" {
		t.Fatalf("content tracks = %+v", list)
	}
	key := list.Items[0].WorkKey
	if code := e.admin("PUT", "content/tracks/"+key, map[string]any{"genres": []string{"jazz", "nope"}, "moods": []string{"calm"}}, nil); code != 200 {
		t.Fatal("manual tag edit")
	}
	var td struct {
		Tags *struct {
			Genres []string
			Manual bool
		}
	}
	e.admin("GET", "tracks/"+key, nil, &td)
	if td.Tags == nil || !td.Tags.Manual || len(td.Tags.Genres) != 1 || td.Tags.Genres[0] != "jazz" {
		t.Fatalf("track tags after edit = %+v", td.Tags)
	}
	var rt struct{ Queued int64 }
	e.admin("POST", "content/retag", map[string]any{"workKeys": []string{key}}, &rt)
	if rt.Queued != 1 {
		t.Fatalf("retag = %+v", rt)
	}

	// ── 训练 ALS（自动跟一次深度模型训练） ──
	var r1 struct{ ID int64 }
	e.admin("POST", "models/train", map[string]string{"note": "p3"}, &r1)
	waitFor(t, "als", 120*time.Second, func() bool {
		var d struct {
			Status      string
			PublishedAt *int64 `json:"publishedAt"`
		}
		e.admin("GET", fmt.Sprintf("models/%d", r1.ID), nil, &d)
		return d.Status == "succeeded" && d.PublishedAt != nil
	})
	time.Sleep(2 * time.Second)
	var deepRuns struct {
		Items []struct {
			ID     int64
			Status string
		}
	}
	e.admin("GET", "deep/runs", nil, &deepRuns)
	if len(deepRuns.Items) == 0 {
		t.Fatal("deep run should be enqueued after ALS")
	}

	// ── 曲库扩展：950 号第一次请求触发后台大模型，第二次带上待解析项 ──
	rec := func(user int64, scene string) *musicv1.RecommendResponse {
		resp, err := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: app, UserId: user, Scene: scene, Limit: 20, Personalized: true}))
		if err != nil {
			t.Fatal(err)
		}
		return resp.Msg
	}
	rec(950, "home")
	var resolved int
	waitFor(t, "llm expansion", 20*time.Second, func() bool {
		resolved = 0
		for _, it := range rec(950, "home").Items {
			if it.Resolve {
				resolved++
				if it.Track.GetSource() != "" || !strings.HasPrefix(it.Track.GetTitle(), "曲库外的歌") {
					t.Fatalf("bad resolve item %+v", it.Track)
				}
			}
		}
		return resolved > 0
	})
	if resolved > 6 { // expandRatio 0.3 × 20
		t.Fatalf("too many resolve items: %d", resolved)
	}
	var prev struct {
		Items []struct {
			Resolve  bool
			Track    struct{ Title string }
			Channels map[string]float64
		}
	}
	e.admin("GET", "users/950/recommendations?scene=home&limit=20", nil, &prev)
	llmIn := 0
	for _, it := range prev.Items {
		if it.Channels["llm"] > 0 && !it.Resolve {
			llmIn++
		}
		if it.Channels["tag_match"] > 0 {
			llmIn += 0
		}
	}
	if llmIn == 0 {
		t.Fatal("in-catalog llm suggestions should be scored with the llm channel")
	}

	// ── 风格歌单按标签起名 ──
	var mixes struct {
		Items []struct{ Title string }
	}
	raw, _ := json.Marshal(map[string]any{})
	resp, err := e.client.UserOp(ctx, connect.NewRequest(&musicv1.UserOpRequest{AppId: app, UserId: 1, Op: "mixes", ParamsJson: raw, Personalized: true}))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(resp.Msg.GetResultJson(), &mixes)
	if len(mixes.Items) == 0 || !strings.Contains(mixes.Items[0].Title, "流行") {
		t.Fatalf("mix titles = %+v", mixes.Items)
	}

	// ── 深度模型（需要 music-deep 在跑） ──
	if os.Getenv("MUSIC_IT_DEEP") != "1" {
		t.Log("MUSIC_IT_DEEP 未开，跳过深度模型")
		return
	}
	var dr struct {
		Status      string
		PublishedAt *int64 `json:"publishedAt"`
		Metrics     *struct{ Ndcg20 float64 }
		Parity      *struct{ Ok bool }
		Error       string
	}
	waitFor(t, "deep training", 15*time.Minute, func() bool {
		e.admin("GET", fmt.Sprintf("deep/runs/%d", deepRuns.Items[0].ID), nil, &dr)
		if dr.Status == "failed" {
			t.Fatalf("deep run failed: %s", dr.Error)
		}
		return dr.Status == "succeeded"
	})
	t.Logf("deep: published=%v metrics=%+v parity=%+v", dr.PublishedAt != nil, dr.Metrics, dr.Parity)
	if dr.PublishedAt == nil {
		e.admin("POST", fmt.Sprintf("deep/runs/%d/publish", deepRuns.Items[0].ID), nil, nil)
	}
	waitFor(t, "deep model loaded", 30*time.Second, func() bool {
		e.admin("GET", "users/3/recommendations?scene=home&limit=20", nil, &prev)
		for _, it := range prev.Items {
			if it.Channels["seq"] > 0 {
				return true
			}
		}
		return false
	})
}
