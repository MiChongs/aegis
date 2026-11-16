package rec

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/store"
	"aegis/services/music/internal/trainer"
)

// Admin 管理端 HTTP 接口：/v1/apps/{app}/*，由主服务 /api/admin/apps/:appkey/music/* 反向代理过来
// （主服务负责管理员登录、权限与审计，这里只认内部令牌）。响应信封与主服务一致：{code, message, data}。
type Admin struct {
	Svc   *Service
	NC    *nats.Conn
	JS    jetstream.JetStream
	Token string
	Log   *zap.Logger
}

// Mount 挂路由。
func (a *Admin) Mount(mux *http.ServeMux) {
	route := func(pattern string, fn func(w http.ResponseWriter, r *http.Request, appID int64)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !a.authorized(r) {
				writeErr(w, http.StatusUnauthorized, 40100, "内部令牌无效")
				return
			}
			appID, err := strconv.ParseInt(r.PathValue("app"), 10, 64)
			if err != nil || appID <= 0 {
				writeErr(w, http.StatusBadRequest, 40000, "应用无效")
				return
			}
			fn(w, r, appID)
		})
	}
	route("GET /v1/apps/{app}/overview", a.overview)
	route("GET /v1/apps/{app}/users", a.users)
	route("GET /v1/apps/{app}/users/{user}", a.user)
	route("GET /v1/apps/{app}/users/{user}/events", a.timeline)
	route("GET /v1/apps/{app}/users/{user}/recommendations", a.preview)
	route("POST /v1/apps/{app}/users/{user}/forget", a.forget)
	route("GET /v1/apps/{app}/tracks", a.tracks)
	route("GET /v1/apps/{app}/tracks/{work}", a.track)
	route("GET /v1/apps/{app}/events", a.events)
	route("GET /v1/apps/{app}/models", a.models)
	route("GET /v1/apps/{app}/models/{run}", a.model)
	route("POST /v1/apps/{app}/models/train", a.train)
	route("POST /v1/apps/{app}/models/{run}/publish", a.publish)
	route("GET /v1/apps/{app}/config", a.getConfig)
	route("PUT /v1/apps/{app}/config", a.putConfig)
}

func (a *Admin) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Music-Token")
	return a.Token != "" && subtle.ConstantTimeCompare([]byte(got), []byte(a.Token)) == 1
}

func operator(r *http.Request) string {
	v, err := url.PathUnescape(r.Header.Get("X-Music-Operator"))
	if err != nil || strings.TrimSpace(v) == "" {
		return "admin"
	}
	return strings.TrimSpace(v)
}

type envelope struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

func writeOK(w http.ResponseWriter, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(envelope{Code: 200, Message: msg, Data: data, Timestamp: time.Now().UnixMilli()})
}

func writeErr(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Code: code, Message: msg, Timestamp: time.Now().UnixMilli()})
}

func (a *Admin) fail(w http.ResponseWriter, err error) {
	var bad ErrBadRequest
	switch {
	case errors.As(err, &bad):
		writeErr(w, http.StatusBadRequest, 40000, bad.Msg)
	case errors.Is(err, ErrNotEnabled):
		writeErr(w, http.StatusServiceUnavailable, 50396, "该应用已关闭音乐个性化")
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, 40400, "记录不存在")
	default:
		a.Log.Warn("music admin request failed", zap.Error(err))
		writeErr(w, http.StatusInternalServerError, 50000, "音乐服务处理失败："+err.Error())
	}
}

func qInt(r *http.Request, key string, def, lo, hi int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	return min(max(v, lo), hi)
}

func qDays(r *http.Request, def int) int {
	switch d := qInt(r, "days", def, 1, 365); d {
	case 1, 7, 14, 30, 90, 180, 365:
		return d
	}
	return def
}

func pathInt(r *http.Request, key string) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	return v, err == nil && v > 0
}

// OverviewView 总览。
type OverviewView struct {
	*store.Overview
	Model    *store.Run  `json:"model"`
	Pipeline PipelineLag `json:"pipeline"`
}

func (a *Admin) overview(w http.ResponseWriter, r *http.Request, appID int64) {
	ov, err := a.Svc.CH.Overview(r.Context(), appID, qDays(r, 7))
	if err != nil {
		a.fail(w, err)
		return
	}
	run, err := a.Svc.PG.CurrentRun(r.Context(), appID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取音乐总览成功", OverviewView{Overview: ov, Model: run, Pipeline: a.Svc.Lag(r.Context(), ov.LastEvent)})
}

func (a *Admin) users(w http.ResponseWriter, r *http.Request, appID int64) {
	var uid int64
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		v, err := strconv.ParseInt(q, 10, 64)
		if err != nil || v <= 0 {
			writeOK(w, "获取听众成功", map[string]any{"items": []store.UserRow{}, "total": 0})
			return
		}
		uid = v
	}
	items, total, err := a.Svc.CH.Users(r.Context(), appID, qDays(r, 30), uid, qInt(r, "page", 1, 1, 10000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取听众成功", map[string]any{"items": items, "total": total})
}

func (a *Admin) user(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	detail, err := a.Svc.UserDetail(r.Context(), appID, uid, ValidDays(qInt(r, "days", 30, 1, 365)))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取用户画像成功", detail)
}

func (a *Admin) timeline(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if before <= 0 {
		before = time.Now().Add(time.Minute).UnixMilli()
	}
	limit := qInt(r, "limit", 50, 1, 200)
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	if typ != "" && !contract.ValidType(typ) {
		writeErr(w, http.StatusBadRequest, 40000, "未知的事件类型")
		return
	}
	items, err := a.Svc.CH.Timeline(r.Context(), appID, uid, typ, before, limit)
	if err != nil {
		a.fail(w, err)
		return
	}
	var next *int64
	if len(items) == limit {
		v := items[len(items)-1].TS
		next = &v
	}
	writeOK(w, "获取事件成功", map[string]any{"items": items, "nextBefore": next})
}

func (a *Admin) preview(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	q := r.URL.Query()
	scene := q.Get("scene")
	if scene == "" {
		scene = contract.SceneHome
	}
	res, err := a.Svc.Recommend(r.Context(), Request{AppID: appID, UserID: uid, Scene: scene, Limit: qInt(r, "limit", 20, 1, 50),
		SeedSource: q.Get("source"), SeedSongID: q.Get("songId"), Personalized: true, Explain: true})
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取推荐预览成功", res)
}

func (a *Admin) forget(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	if err := a.Svc.Forget(r.Context(), a.NC, appID, uid, "admin", operator(r)); err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "已清除该用户的听歌数据", map[string]any{"deleted": true})
}

func (a *Admin) tracks(w http.ResponseWriter, r *http.Request, appID int64) {
	q := r.URL.Query()
	items, total, err := a.Svc.CH.Tracks(r.Context(), appID, qDays(r, 7), q.Get("q"), q.Get("sort"),
		qInt(r, "page", 1, 1, 10000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取作品成功", map[string]any{"items": items, "total": total})
}

// TrackDetailView 作品详情。
type TrackDetailView struct {
	WorkKey  string            `json:"workKey"`
	Track    contract.Track    `json:"track"`
	Variants []VariantView     `json:"variants"`
	Stats    *store.TrackStats `json:"stats"`
	Daily    []store.TrackDay  `json:"daily"`
	Similar  []Similar         `json:"similar"`
}

// VariantView 音源版本。
type VariantView struct {
	Source     string `json:"source"`
	SongID     string `json:"songId"`
	Plays      int64  `json:"plays"`
	LastSeenAt int64  `json:"lastSeenAt"`
}

func (a *Admin) track(w http.ResponseWriter, r *http.Request, appID int64) {
	work := strings.TrimSpace(r.PathValue("work"))
	if work == "" {
		writeErr(w, http.StatusBadRequest, 40000, "作品无效")
		return
	}
	ctx := r.Context()
	stats, daily, plays, chTrack, err := a.Svc.CH.TrackDetail(ctx, appID, work, qDays(r, 30))
	if err != nil {
		a.fail(w, err)
		return
	}
	tracks, err := a.Svc.PG.Tracks(ctx, appID, []string{work})
	if err != nil {
		a.fail(w, err)
		return
	}
	t, ok := tracks[work]
	if !ok {
		if chTrack == nil {
			writeErr(w, http.StatusNotFound, 40400, "作品不存在")
			return
		}
		t = *chTrack
	}
	variants, err := a.Svc.PG.Variants(ctx, appID, work)
	if err != nil {
		a.fail(w, err)
		return
	}
	count := map[string]int64{}
	for _, v := range plays {
		count[v.Source+":"+v.SongID] = v.Plays
	}
	view := TrackDetailView{WorkKey: work, Track: t, Variants: []VariantView{}, Stats: stats, Daily: daily, Similar: a.Svc.SimilarOf(appID, work)}
	for _, v := range variants {
		view.Variants = append(view.Variants, VariantView{Source: v.Source, SongID: v.SongID, Plays: count[v.Source+":"+v.SongID], LastSeenAt: v.LastSeenAt})
	}
	writeOK(w, "获取作品详情成功", view)
}

func parseMillis(s string, def time.Time) time.Time {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || v <= 0 {
		return def
	}
	return time.UnixMilli(v)
}

func (a *Admin) events(w http.ResponseWriter, r *http.Request, appID int64) {
	q := r.URL.Query()
	now := time.Now()
	f := store.EventFilter{
		Type: strings.TrimSpace(q.Get("type")), Q: q.Get("q"),
		From: parseMillis(q.Get("from"), now.Add(-7*24*time.Hour)), To: parseMillis(q.Get("to"), now.Add(time.Minute)),
	}
	if f.Type != "" && !contract.ValidType(f.Type) {
		writeErr(w, http.StatusBadRequest, 40000, "未知的事件类型")
		return
	}
	if v := strings.TrimSpace(q.Get("userId")); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil || id <= 0 {
			writeErr(w, http.StatusBadRequest, 40000, "用户 ID 无效")
			return
		}
		f.UserID = id
	}
	if f.To.Sub(f.From) > 400*24*time.Hour {
		f.From = f.To.Add(-400 * 24 * time.Hour)
	}
	items, total, err := a.Svc.CH.Events(r.Context(), appID, f, qInt(r, "page", 1, 1, 10000), qInt(r, "pageSize", 50, 1, 200))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取事件成功", map[string]any{"items": items, "total": total})
}

func (a *Admin) models(w http.ResponseWriter, r *http.Request, appID int64) {
	items, total, err := a.Svc.PG.Runs(r.Context(), appID, qInt(r, "page", 1, 1, 10000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取训练记录成功", map[string]any{"items": items, "total": total})
}

func (a *Admin) model(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "run")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "训练记录无效")
		return
	}
	run, err := a.Svc.PG.Run(r.Context(), appID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取训练详情成功", run)
}

func (a *Admin) train(w http.ResponseWriter, r *http.Request, appID int64) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body)
	note := strings.TrimSpace(body.Note)
	if len([]rune(note)) > 200 {
		note = string([]rune(note)[:200])
	}
	run, err := trainer.Enqueue(r.Context(), a.Svc.PG, a.JS, appID, "manual", note, operator(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "训练任务已排队", run)
}

func (a *Admin) publish(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "run")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "训练记录无效")
		return
	}
	if err := trainer.Publish(r.Context(), a.Svc.PG, a.NC, appID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusConflict, 40900, "只能发布训练成功且模型文件仍保留的版本")
			return
		}
		a.fail(w, err)
		return
	}
	// 本实例立即加载，其它实例靠通知 / 轮询
	go a.Svc.Reg.Load(r.Context(), appID, id)
	run, err := a.Svc.PG.Run(r.Context(), appID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "已发布", run)
}

func (a *Admin) getConfig(w http.ResponseWriter, r *http.Request, appID int64) {
	cfg, err := a.Svc.PG.Config(r.Context(), appID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取音乐配置成功", cfg)
}

func (a *Admin) putConfig(w http.ResponseWriter, r *http.Request, appID int64) {
	cfg := appcfg.Default()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, 40000, "配置格式不正确")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, 40000, err.Error())
		return
	}
	saved, err := a.Svc.PG.SaveConfig(r.Context(), appID, cfg, operator(r))
	if err != nil {
		a.fail(w, err)
		return
	}
	a.Svc.Cfgs.Invalidate(appID)
	if a.NC != nil {
		payload, _ := json.Marshal(map[string]int64{"app_id": appID})
		_ = a.NC.Publish(contract.SubjectConfigChanged, payload)
	}
	writeOK(w, "音乐配置已保存", saved)
}
