package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	apperrors "aegis/pkg/errors"
	"aegis/services/music/contract"
)

// TestMusicServiceIT 主服务这一侧：校验 → JetStream → music-ingest 落地 → music-rec 推荐 / 画像 / 清除。
// 需要先起好三个音乐服务（docs/music-personalization.md「测试」）：
//
//	MUSIC_IT=1 MUSIC_IT_RPC=http://127.0.0.1:18096 MUSIC_INTERNAL_TOKEN=… MUSIC_NATS_URL=… MUSIC_REDIS_URL=… go test ./internal/service -run TestMusicServiceIT
func TestMusicServiceIT(t *testing.T) {
	if os.Getenv("MUSIC_IT") != "1" {
		t.Skip("MUSIC_IT=1 才跑")
	}
	nc, err := nats.Connect(os.Getenv("MUSIC_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	opts, err := redis.ParseURL(os.Getenv("MUSIC_REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	svc, err := NewMusicService(zap.NewNop(), config.MusicConfig{RPCURL: os.Getenv("MUSIC_IT_RPC"), InternalToken: os.Getenv("MUSIC_INTERNAL_TOKEN"), EventsEnabled: true},
		nc, rdb, "aegis-it:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	app := &appdomain.App{ID: 980000000 + time.Now().Unix()%1000000, AppKey: "it"}
	session := &authdomain.Session{UserID: 7, AppID: app.ID, DeviceID: "dev-it"}
	now := time.Now()
	run := now.UnixNano()

	batch := MusicEventBatch{Client: contract.ClientInfo{Platform: "android", AppVersion: "it", Network: "wifi"}}
	for i := 0; i < 5; i++ {
		batch.Events = append(batch.Events, contract.ClientEvent{
			ID: fmt.Sprintf("svc%d-%d", run, i), Type: contract.TypePlayEnd, TS: now.Add(-time.Duration(i) * time.Minute).UnixMilli(),
			Reason: contract.ReasonCompleted, PlayedMs: 200000,
			Track: &contract.Track{Source: "wy", SongID: fmt.Sprintf("77%d", i), Title: fmt.Sprintf("主服务测试%d", i), Artist: "测试", DurationMs: 210000},
		})
	}
	// 非法：类型不对、ID 重复、本机歌
	batch.Events = append(batch.Events,
		contract.ClientEvent{ID: fmt.Sprintf("svc%d-bad", run), Type: "hack", TS: now.UnixMilli()},
		batch.Events[0],
		contract.ClientEvent{ID: fmt.Sprintf("svc%d-local", run), Type: contract.TypeLike, TS: now.UnixMilli(), Track: &contract.Track{Source: "local", SongID: "l1", Title: "x"}},
	)
	res, err := svc.Report(ctx, app, session, "dev-it", "203.0.113.7", batch)
	if err != nil {
		t.Fatal(err)
	}
	if res.Accepted != 5 || len(res.Rejected) != 3 {
		t.Fatalf("report = %+v", res)
	}
	reasons := []string{}
	for _, r := range res.Rejected {
		reasons = append(reasons, r.Reason)
	}
	if strings.Join(reasons, ",") != "invalid_type,duplicate,invalid_track" {
		t.Fatalf("rejected = %v", reasons)
	}
	// 整批重传：去重窗口丢掉，不会多算
	if _, err := svc.Report(ctx, app, session, "dev-it", "203.0.113.7", batch); err != nil {
		t.Fatal(err)
	}

	// 等落地：画像里能看到 5 次播放（画像有 10 分钟缓存，第一次取到之前不会写缓存）
	var profile struct {
		Plays int64 `json:"plays"`
	}
	// 每个天数的画像各缓存一次，轮询时换着天数取
	for _, days := range []int{30, 90, 365} {
		time.Sleep(2 * time.Second)
		if cnt, _ := svcCountEvents(ctx, svc, app, session, days); cnt >= 5 {
			break
		}
	}
	raw, err := svc.Profile(ctx, app, session, 7)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &profile)
	if profile.Plays != 5 {
		t.Fatalf("profile plays = %d (%s)", profile.Plays, raw)
	}
	// 还没有模型：home 给热度兜底（只有这个人自己听过的，近期播放过的会被过滤）
	recs, err := svc.Recommend(ctx, app, session, MusicRecommendQuery{Scene: "home", Limit: 10, Personalized: true})
	if err != nil {
		t.Fatal(err)
	}
	if recs.RecID == "" || recs.Items == nil {
		t.Fatalf("recommend = %+v", recs)
	}
	if _, err := svc.Recommend(ctx, app, session, MusicRecommendQuery{Scene: "nope"}); err == nil {
		t.Fatal("bad scene accepted")
	}
	// 限额
	for i := 0; i < 7; i++ {
		big := MusicEventBatch{}
		for j := 0; j < 100; j++ {
			big.Events = append(big.Events, contract.ClientEvent{ID: fmt.Sprintf("rl%d-%d-%d", run, i, j), Type: contract.TypeSearch, TS: now.UnixMilli(),
				Context: contract.Context{Query: "x"}})
		}
		_, err = svc.Report(ctx, app, &authdomain.Session{UserID: 8, AppID: app.ID}, "", "", big)
	}
	if ae, ok := err.(*apperrors.AppError); !ok || ae.Code != errCodeMusicRateLimited {
		t.Fatalf("rate limit not hit: %v", err)
	}
	if err := svc.ClearProfile(ctx, app, session); err != nil {
		t.Fatal(err)
	}
	raw, _ = svc.Profile(ctx, app, session, 7)
	_ = json.Unmarshal(raw, &profile)
	if profile.Plays != 0 {
		t.Fatalf("profile after clear = %s", raw)
	}
}

// svcCountEvents 经画像接口看落地了几条。
func svcCountEvents(ctx context.Context, svc *MusicService, app *appdomain.App, session *authdomain.Session, days int) (int64, error) {
	raw, err := svc.Profile(ctx, app, session, days)
	if err != nil {
		return 0, err
	}
	var p struct {
		Plays int64 `json:"plays"`
	}
	_ = json.Unmarshal(raw, &p)
	return p.Plays, nil
}
