package rec

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/store"
	"aegis/services/music/internal/trainer"
)

// 第三期管理端：深度模型训练记录、内容理解（docs/music-personalization-3.md「管理端增量」）。

func (a *Admin) mountDeep(route func(string, func(http.ResponseWriter, *http.Request, int64))) {
	route("GET /v1/apps/{app}/deep/runs", a.deepRuns)
	route("GET /v1/apps/{app}/deep/runs/{run}", a.deepRun)
	route("POST /v1/apps/{app}/deep/train", a.deepTrain)
	route("POST /v1/apps/{app}/deep/runs/{run}/publish", a.deepPublish)
	route("GET /v1/apps/{app}/content", a.contentOverview)
	route("GET /v1/apps/{app}/content/tracks", a.contentTracks)
	route("PUT /v1/apps/{app}/content/tracks/{work}", a.contentEdit)
	route("POST /v1/apps/{app}/content/retag", a.contentRetag)
}

func (a *Admin) deepRuns(w http.ResponseWriter, r *http.Request, appID int64) {
	items, total, err := a.Svc.PG.DeepRuns(r.Context(), appID, qInt(r, "page", 1, 1, 100000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取深度模型训练记录成功", map[string]any{"items": items, "total": total})
}

func (a *Admin) deepRun(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "run")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "训练记录无效")
		return
	}
	run, err := a.Svc.PG.DeepRunOf(r.Context(), appID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取深度模型训练详情成功", run)
}

func (a *Admin) deepTrain(w http.ResponseWriter, r *http.Request, appID int64) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body)
	cfg, err := a.Svc.Cfgs.Get(r.Context(), appID)
	if err != nil {
		a.fail(w, err)
		return
	}
	run, err := trainer.EnqueueDeep(r.Context(), a.Svc.PG, a.JS, appID, "manual", strings.TrimSpace(body.Note), operator(r), cfg.Deep)
	if errors.Is(err, store.ErrDeepActive) {
		writeErr(w, http.StatusConflict, 40900, err.Error())
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "已排上深度模型训练", run)
}

func (a *Admin) deepPublish(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "run")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "训练记录无效")
		return
	}
	if err := a.Svc.PG.PublishDeepRun(r.Context(), appID, id); err != nil {
		a.failOps(w, err)
		return
	}
	if a.NC != nil {
		payload, _ := json.Marshal(map[string]int64{"app_id": appID, "run_id": id})
		_ = a.NC.Publish(contract.SubjectDeepPublished, payload)
	}
	run, err := a.Svc.PG.DeepRunOf(r.Context(), appID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "深度模型已发布", run)
}

// ContentView 内容理解总览。
type ContentView struct {
	*store.ContentStats
	LLMConfigured bool `json:"llmConfigured"`
}

func (a *Admin) contentOverview(w http.ResponseWriter, r *http.Request, appID int64) {
	cs, err := a.Svc.PG.Content(r.Context(), appID, qDays(r, 14))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取内容理解总览成功", ContentView{ContentStats: cs, LLMConfigured: a.Svc.LLM.Enabled()})
}

func (a *Admin) contentTracks(w http.ResponseWriter, r *http.Request, appID int64) {
	q := r.URL.Query()
	status := q.Get("status")
	switch status {
	case "", "tagged", "pending", "failed":
	default:
		writeErr(w, http.StatusBadRequest, 40000, "status 只能是 tagged、pending、failed")
		return
	}
	items, total, err := a.Svc.PG.ContentTracks(r.Context(), appID, status, strings.TrimSpace(q.Get("q")),
		qInt(r, "page", 1, 1, 100000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取作品标签成功", map[string]any{"items": items, "total": total})
}

func (a *Admin) contentEdit(w http.ResponseWriter, r *http.Request, appID int64) {
	work := strings.TrimSpace(r.PathValue("work"))
	var t contract.Tags
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&t); err != nil || work == "" {
		writeErr(w, http.StatusBadRequest, 40000, "标签格式不正确")
		return
	}
	t.Clean()
	tracks, err := a.Svc.PG.Tracks(r.Context(), appID, []string{work})
	if err != nil {
		a.fail(w, err)
		return
	}
	tr, ok := tracks[work]
	if !ok {
		writeErr(w, http.StatusNotFound, 40400, "作品不存在")
		return
	}
	var emb []float32
	if a.Svc.LLM.Enabled() {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		vecs, usage, err := a.Svc.LLM.Embed(ctx, []string{trainer.EmbedText(tr, t)})
		cancel()
		_ = a.Svc.PG.AddUsage(r.Context(), appID, "embed", 1, usage.TotalTokens, 1)
		if err == nil {
			emb = vecs[0]
		}
	}
	if err := a.Svc.PG.SetManualTags(r.Context(), appID, work, t, emb); err != nil {
		a.fail(w, err)
		return
	}
	tags, _ := a.Svc.PG.TrackTags(r.Context(), appID, []string{work})
	ti := tags[work]
	writeOK(w, "标签已保存", ti)
}

func (a *Admin) contentRetag(w http.ResponseWriter, r *http.Request, appID int64) {
	var body struct {
		WorkKeys []string `json:"workKeys"`
		All      bool     `json:"all"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, 40000, "请求体格式不正确")
		return
	}
	if !body.All && len(body.WorkKeys) == 0 {
		writeErr(w, http.StatusBadRequest, 40000, "请选择要重新打标签的作品")
		return
	}
	if len(body.WorkKeys) > 5000 {
		writeErr(w, http.StatusBadRequest, 40000, "一次最多 5000 首")
		return
	}
	n, err := a.Svc.PG.Retag(r.Context(), appID, body.WorkKeys, body.All)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "已加入重新打标签的队列", map[string]int64{"queued": n})
}
