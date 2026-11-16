// Package e2e 音乐个性化三服务端到端：像主服务一样往 JetStream 发事件 → music-ingest 落地 →
// 经管理端触发 music-trainer 训练并自动发布 → music-rec 推荐、画像、预览、清除。
//
// 先起好三个服务（见 docs/music-personalization.md「测试」），再：
//
//	MUSIC_IT=1 MUSIC_IT_RPC=http://127.0.0.1:18096 MUSIC_INTERNAL_TOKEN=… MUSIC_NATS_URL=… go test ./services/music/internal/e2e -run IT -v
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// itApp 每次运行一个新应用，互不干扰（只在集成测试库里）。
var itApp = 990000000 + time.Now().Unix()%1000000

type env struct {
	t      *testing.T
	base   string
	token  string
	client musicv1connect.MusicServiceClient
}

func (e *env) admin(method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = strings.NewReader(string(raw))
	}
	req, _ := http.NewRequest(method, fmt.Sprintf("%s/v1/apps/%d/%s", e.base, itApp, path), rd)
	req.Header.Set("X-Music-Token", e.token)
	req.Header.Set("X-Music-Operator", "e2e")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		e.t.Fatalf("%s %s: bad envelope %s", method, path, raw)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			e.t.Fatalf("%s %s: decode %v: %s", method, path, err, env.Data)
		}
	}
	if resp.StatusCode != 200 {
		e.t.Logf("%s %s → %d %s", method, path, resp.StatusCode, env.Message)
	}
	return resp.StatusCode
}

func waitFor(t *testing.T, what string, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func TestMusicIT(t *testing.T) {
	if os.Getenv("MUSIC_IT") != "1" {
		t.Skip("MUSIC_IT=1 才跑（需要先起好三个音乐服务）")
	}
	e := &env{t: t, base: os.Getenv("MUSIC_IT_RPC"), token: os.Getenv("MUSIC_INTERNAL_TOKEN")}
	httpc := &http.Client{Timeout: 30 * time.Second}
	e.client = musicv1connect.NewMusicServiceClient(httpc, e.base, connect.WithInterceptors(connect.UnaryInterceptorFunc(
		func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				req.Header().Set("Authorization", "Bearer "+e.token)
				return next(ctx, req)
			}
		})))
	ctx := context.Background()

	// 内部令牌必须校验
	bad := musicv1connect.NewMusicServiceClient(httpc, e.base)
	if _, err := bad.GetSwitches(ctx, connect.NewRequest(&musicv1.GetSwitchesRequest{AppId: itApp})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("missing token should be rejected, got %v", err)
	}

	nc, err := nats.Connect(os.Getenv("MUSIC_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, _ := jetstream.New(nc)

	// 两类口味：1–30 号听 A 组（0–29），31–60 号听 B 组（30–59）。每人听 15 首、收藏 2 首、对对面组早跳一首。
	run := time.Now().UnixNano()
	now := time.Now()
	var events []contract.Event
	add := func(user int64, typ string, item int, at time.Time, played int64, reason string, mut func(*contract.ClientEvent)) {
		ev := contract.ClientEvent{ID: fmt.Sprintf("it%d-%d-%d", run, len(events), user), Type: typ, TS: at.UnixMilli(), Reason: reason, PlayedMs: played,
			SessionID: fmt.Sprintf("s%d", user)}
		if item >= 0 {
			ev.Track = &contract.Track{Source: "wy", SongID: fmt.Sprintf("10%02d", item), Title: fmt.Sprintf("测试歌曲%02d", item),
				Artist: fmt.Sprintf("测试歌手%d", item/5), DurationMs: 240000, Cover: "https://example.com/c.jpg"}
		}
		if mut != nil {
			mut(&ev)
		}
		if r := contract.Normalize(&ev, now); r != "" {
			t.Fatalf("event rejected: %s", r)
		}
		events = append(events, contract.Enrich(ev, contract.ClientInfo{Platform: "android", AppVersion: "it"}, itApp, user, "dev-it", "203.0.113.9", now))
	}
	for u := int64(1); u <= 60; u++ {
		group := 0
		if u > 30 {
			group = 30
		}
		at := now.Add(-36 * time.Hour)
		for k := 0; k < 15; k++ {
			item := group + int((u*7+int64(k)*11)%30)
			at = at.Add(4 * time.Minute)
			add(u, contract.TypePlayStart, item, at.Add(-4*time.Minute), 0, "", nil)
			add(u, contract.TypePlayEnd, item, at, 238000, contract.ReasonCompleted, nil)
			if k < 2 {
				add(u, contract.TypeLike, item, at.Add(time.Second), 0, "", nil)
			}
		}
		add(u, contract.TypePlayEnd, (group+30+int(u))%60, at.Add(time.Minute), 6000, contract.ReasonSkipped, nil)
		add(u, contract.TypeSearch, -1, at.Add(2*time.Minute), 0, "", func(ev *contract.ClientEvent) { ev.Context.Query = "测试" })
	}
	// 1 号不喜欢「测试歌手1」
	add(1, contract.TypeDislike, 5, now.Add(-time.Hour), 0, "", func(ev *contract.ClientEvent) { ev.Props = map[string]string{"scope": "artist"} })

	publish := func(list []contract.Event) {
		for _, ev := range list {
			raw, _ := json.Marshal(ev)
			msg := &nats.Msg{Subject: contract.EventSubject(itApp), Data: raw, Header: nats.Header{}}
			msg.Header.Set(jetstream.MsgIDHeader, contract.MsgID(ev.AppID, ev.UserID, ev.ID))
			if _, err := js.PublishMsg(ctx, msg); err != nil {
				t.Fatal(err)
			}
		}
	}
	publish(events)
	publish(events[:50]) // 重传：去重窗口内应被丢弃
	t.Logf("published %d events", len(events))

	var ov struct {
		Totals struct {
			Events, Listeners, Plays int64
		} `json:"totals"`
		Scenes []json.RawMessage `json:"scenes"`
	}
	waitFor(t, "ingest", 60*time.Second, func() bool {
		e.admin("GET", "overview?days=7", nil, &ov)
		return ov.Totals.Events >= int64(len(events))
	})
	if ov.Totals.Events != int64(len(events)) || ov.Totals.Listeners != 60 {
		t.Fatalf("overview totals = %+v (want %d events, 60 listeners)", ov.Totals, len(events))
	}

	// 训练 → 自动发布
	var run1 struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	if code := e.admin("POST", "models/train", map[string]string{"note": "e2e"}, &run1); code != 200 || run1.ID == 0 {
		t.Fatalf("train enqueue failed: %d %+v", code, run1)
	}
	var detail struct {
		Status      string   `json:"status"`
		Error       string   `json:"error"`
		PublishedAt *int64   `json:"publishedAt"`
		Users       int      `json:"users"`
		Items       int      `json:"items"`
		Log         []string `json:"log"`
		Metrics     *struct {
			NDCG20   float64 `json:"ndcg20"`
			Baseline struct {
				NDCG20 float64 `json:"ndcg20"`
			} `json:"baseline"`
		} `json:"metrics"`
	}
	waitFor(t, "training", 120*time.Second, func() bool {
		e.admin("GET", fmt.Sprintf("models/%d", run1.ID), nil, &detail)
		return detail.Status == "succeeded" || detail.Status == "failed"
	})
	t.Logf("training log:\n  %s", strings.Join(detail.Log, "\n  "))
	if detail.Status != "succeeded" || detail.PublishedAt == nil || detail.Users != 60 {
		t.Fatalf("training = %+v", detail)
	}

	// music-rec 热加载
	var rec *musicv1.RecommendResponse
	waitFor(t, "model load", 30*time.Second, func() bool {
		resp, err := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 1, Scene: "home", Limit: 10, Personalized: true}))
		if err != nil {
			t.Fatalf("recommend: %v", err)
		}
		rec = resp.Msg
		return rec.GetModelVersion() == run1.ID
	})
	own, dislikedArtist := 0, 0
	for _, it := range rec.GetItems() {
		var n int
		fmt.Sscanf(it.GetTrack().GetSongId(), "10%d", &n)
		if n < 30 {
			own++
		}
		if it.GetTrack().GetArtist() == "测试歌手1" {
			dislikedArtist++
		}
	}
	t.Logf("home for user 1: %d items, %d in own group, first reason %q", len(rec.GetItems()), own, rec.GetItems()[0].GetReason().GetText())
	if len(rec.GetItems()) < 5 || own < len(rec.GetItems())*7/10 || dislikedArtist > 0 {
		t.Fatalf("home recommendations look wrong: own=%d disliked=%d of %d", own, dislikedArtist, len(rec.GetItems()))
	}

	// 每日推荐当天固定
	d1, err := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 40, Scene: "daily", Personalized: true}))
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 40, Scene: "daily", Personalized: true}))
	if d1.Msg.GetRecId() != d2.Msg.GetRecId() || len(d1.Msg.GetItems()) == 0 {
		t.Fatalf("daily not stable: %s vs %s (%d items)", d1.Msg.GetRecId(), d2.Msg.GetRecId(), len(d1.Msg.GetItems()))
	}
	// 电台翻页不重复
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 2; page++ {
		r, err := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 41, Scene: "radio", Limit: 8, Cursor: cursor, Personalized: true}))
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range r.Msg.GetItems() {
			if seen[it.GetWorkKey()] {
				t.Fatalf("radio repeated %s", it.GetTrack().GetTitle())
			}
			seen[it.GetWorkKey()] = true
		}
		cursor = r.Msg.GetNextCursor()
	}
	// 相似歌曲
	sim, err := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 2, Scene: "similar", SeedSource: "wy", SeedSongId: "1003", Limit: 10, Personalized: true}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sim.Msg.GetItems()) == 0 || !strings.Contains(sim.Msg.GetItems()[0].GetReason().GetText(), "测试歌曲03") {
		t.Fatalf("similar = %+v", sim.Msg.GetItems())
	}
	// 关闭个性化：只给热度
	np, _ := e.client.Recommend(ctx, connect.NewRequest(&musicv1.RecommendRequest{AppId: itApp, UserId: 1, Scene: "home", Limit: 5, Personalized: false}))
	for _, it := range np.Msg.GetItems() {
		if k := it.GetReason().GetKind(); k != "trending" && k != "explore" {
			t.Fatalf("non-personalized reason = %s", k)
		}
	}

	// 画像
	prof, err := e.client.GetProfile(ctx, connect.NewRequest(&musicv1.GetProfileRequest{AppId: itApp, UserId: 1, Days: 7}))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Plays      int64 `json:"plays"`
		Likes      int64 `json:"likes"`
		TopArtists []struct {
			Name string `json:"name"`
		} `json:"topArtists"`
		Persona struct {
			Title string `json:"title"`
		} `json:"persona"`
	}
	_ = json.Unmarshal(prof.Msg.GetProfileJson(), &p)
	if p.Plays < 15 || p.Likes != 2 || len(p.TopArtists) == 0 {
		t.Fatalf("profile = %s", prof.Msg.GetProfileJson())
	}

	// 管理端各页
	var users struct {
		Items []struct{ UserID int64 } `json:"items"`
		Total int64                    `json:"total"`
	}
	e.admin("GET", "users?days=7&pageSize=5", nil, &users)
	if users.Total != 60 || len(users.Items) != 5 {
		t.Fatalf("users = %+v", users)
	}
	var ud struct {
		Realtime struct {
			Recent          []json.RawMessage       `json:"recent"`
			Positive        []json.RawMessage       `json:"positive"`
			Negative        []json.RawMessage       `json:"negative"`
			DislikedArtists []struct{ Name string } `json:"dislikedArtists"`
		} `json:"realtime"`
		Model struct {
			InModel bool `json:"inModel"`
		} `json:"model"`
	}
	e.admin("GET", "users/1?days=7", nil, &ud)
	if len(ud.Realtime.Recent) == 0 || len(ud.Realtime.Positive) == 0 || len(ud.Realtime.Negative) == 0 || len(ud.Realtime.DislikedArtists) != 1 || !ud.Model.InModel {
		t.Fatalf("user detail = %+v", ud)
	}
	var preview struct {
		Items []struct {
			Channels map[string]float64 `json:"channels"`
		} `json:"items"`
	}
	e.admin("GET", "users/1/recommendations?scene=home&limit=5", nil, &preview)
	if len(preview.Items) == 0 || len(preview.Items[0].Channels) == 0 {
		t.Fatalf("preview has no channel breakdown: %+v", preview)
	}
	var tl struct {
		Items []json.RawMessage `json:"items"`
	}
	e.admin("GET", "users/1/events?limit=10", nil, &tl)
	if len(tl.Items) != 10 {
		t.Fatalf("timeline = %d", len(tl.Items))
	}
	var tracks struct {
		Items []struct{ WorkKey string } `json:"items"`
	}
	e.admin("GET", "tracks?days=7&sort=listeners", nil, &tracks)
	if len(tracks.Items) == 0 {
		t.Fatal("tracks empty")
	}
	var td struct {
		Similar  []json.RawMessage `json:"similar"`
		Variants []json.RawMessage `json:"variants"`
	}
	e.admin("GET", "tracks/"+tracks.Items[0].WorkKey, nil, &td)
	if len(td.Similar) == 0 || len(td.Variants) == 0 {
		t.Fatalf("track detail = %+v", td)
	}
	var evp struct {
		Total int64 `json:"total"`
	}
	e.admin("GET", "events?type=search&q=测试", nil, &evp)
	if evp.Total != 60 {
		t.Fatalf("search events = %d", evp.Total)
	}
	var cfg map[string]any
	e.admin("GET", "config", nil, &cfg)
	cfg["artistCap"] = 99.0
	if code := e.admin("PUT", "config", cfg, nil); code != 400 {
		t.Fatalf("invalid config accepted: %d", code)
	}
	cfg["artistCap"] = 3.0
	if code := e.admin("PUT", "config", cfg, nil); code != 200 {
		t.Fatalf("valid config rejected: %d", code)
	}

	// 清除 1 号的数据
	if _, err := e.client.ForgetUser(ctx, connect.NewRequest(&musicv1.ForgetUserRequest{AppId: itApp, UserId: 1, Reason: "user"})); err != nil {
		t.Fatal(err)
	}
	e.admin("GET", "users/1/events?limit=10", nil, &tl)
	e.admin("GET", "users/1?days=7", nil, &ud)
	if len(tl.Items) != 0 || len(ud.Realtime.Recent) != 0 || ud.Model.InModel {
		t.Fatalf("forget incomplete: timeline=%d recent=%d inModel=%v", len(tl.Items), len(ud.Realtime.Recent), ud.Model.InModel)
	}
}
