package rec

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/store"
)

// 第二期管理端：运营干预、实验、搜索分析、歌手、用户的风格歌单与听歌报告（docs/music-personalization-2.md「管理端增量」）。

// SubjectOpsChanged 运营规则或实验改了（各 music-rec 实例清缓存）。
const SubjectOpsChanged = "music.ops.changed"

// SubscribeOps 订阅规则 / 实验变更。
func (s *Service) SubscribeOps(nc *nats.Conn) (*nats.Subscription, error) {
	return nc.Subscribe(SubjectOpsChanged, func(m *nats.Msg) {
		var p struct {
			AppID int64 `json:"app_id"`
		}
		if json.Unmarshal(m.Data, &p) == nil {
			s.InvalidateOps(p.AppID)
		}
	})
}

func (a *Admin) opsChanged(appID int64) {
	a.Svc.InvalidateOps(appID)
	if a.NC != nil {
		payload, _ := json.Marshal(map[string]int64{"app_id": appID})
		_ = a.NC.Publish(SubjectOpsChanged, payload)
	}
}

func (a *Admin) mountOps(route func(string, func(http.ResponseWriter, *http.Request, int64))) {
	route("GET /v1/apps/{app}/rules", a.rules)
	route("POST /v1/apps/{app}/rules", a.saveRule)
	route("PUT /v1/apps/{app}/rules/{id}", a.saveRule)
	route("DELETE /v1/apps/{app}/rules/{id}", a.deleteRule)
	route("GET /v1/apps/{app}/experiments", a.experiments)
	route("POST /v1/apps/{app}/experiments", a.saveExperiment)
	route("PUT /v1/apps/{app}/experiments/{id}", a.saveExperiment)
	route("DELETE /v1/apps/{app}/experiments/{id}", a.deleteExperiment)
	route("POST /v1/apps/{app}/experiments/{id}/start", a.startExperiment)
	route("POST /v1/apps/{app}/experiments/{id}/stop", a.stopExperiment)
	route("GET /v1/apps/{app}/experiments/{id}/results", a.experimentResults)
	route("GET /v1/apps/{app}/search", a.searchStats)
	route("GET /v1/apps/{app}/artists", a.artists)
	route("GET /v1/apps/{app}/artists/{name}", a.artist)
	route("GET /v1/apps/{app}/users/{user}/mixes", a.userMixes)
	route("GET /v1/apps/{app}/users/{user}/report", a.userReport)
}

func (a *Admin) failOps(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, 40900, strings.TrimPrefix(err.Error(), store.ErrConflict.Error()+"："))
		return
	}
	a.fail(w, err)
}

// ── 运营干预规则 ──

// RuleView 规则 + 近 7 天影响。
type RuleView struct {
	store.Rule
	Target RuleTargetView `json:"target"`
	Stats  store.RuleStat `json:"stats"`
}

// RuleTargetView 规则目标（带作品信息）。
type RuleTargetView struct {
	store.RuleTarget
	Track *contract.Track `json:"track"`
}

func (a *Admin) ruleViews(ctx context.Context, appID int64, rules []store.Rule) []RuleView {
	stats, err := a.Svc.CH.RuleStats(ctx, appID, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		a.Log.Warn("rule stats failed", zap.Error(err))
	}
	var keys []string
	for _, r := range rules {
		if r.Target.WorkKey != "" {
			keys = append(keys, r.Target.WorkKey)
		}
	}
	tracks, _ := a.Svc.PG.Tracks(ctx, appID, keys)
	out := make([]RuleView, 0, len(rules))
	for _, r := range rules {
		v := RuleView{Rule: r, Target: RuleTargetView{RuleTarget: r.Target}, Stats: stats[r.ID]}
		if t, ok := tracks[r.Target.WorkKey]; ok {
			t := t
			v.Target.Track = &t
		}
		out = append(out, v)
	}
	return out
}

func (a *Admin) rules(w http.ResponseWriter, r *http.Request, appID int64) {
	list, err := a.Svc.PG.Rules(r.Context(), appID)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取干预规则成功", map[string]any{"items": a.ruleViews(r.Context(), appID, list)})
}

func (a *Admin) saveRule(w http.ResponseWriter, r *http.Request, appID int64) {
	var body struct {
		Kind   string `json:"kind"`
		Target struct {
			Type    string `json:"type"`
			WorkKey string `json:"workKey"`
			Artist  string `json:"artist"`
			Source  string `json:"source"`
			SongID  string `json:"songId"`
		} `json:"target"`
		Scenes   []string `json:"scenes"`
		Weight   float64  `json:"weight"`
		Position int      `json:"position"`
		StartsAt *int64   `json:"startsAt"`
		EndsAt   *int64   `json:"endsAt"`
		Enabled  *bool    `json:"enabled"`
		Note     string   `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, 40000, "规则格式不正确")
		return
	}
	rule := store.Rule{AppID: appID, Kind: body.Kind, Scenes: body.Scenes, Weight: body.Weight, Position: body.Position,
		StartsAt: body.StartsAt, EndsAt: body.EndsAt, Enabled: true, Note: strings.TrimSpace(body.Note), Operator: operator(r)}
	if body.Enabled != nil {
		rule.Enabled = *body.Enabled
	}
	if id, ok := pathInt(r, "id"); ok {
		rule.ID = id
	}
	switch rule.Kind {
	case "block":
		rule.Scenes, rule.Weight, rule.Position = []string{}, 1, 1
	case "boost":
		if rule.Weight < 0.1 || rule.Weight > 10 {
			writeErr(w, http.StatusBadRequest, 40000, "加权倍数应在 0.1 到 10 之间")
			return
		}
		rule.Position = 1
	case "pin":
		if rule.Position < 1 || rule.Position > 20 {
			writeErr(w, http.StatusBadRequest, 40000, "置顶位置应在 1 到 20 之间")
			return
		}
		rule.Weight = 1
	default:
		writeErr(w, http.StatusBadRequest, 40000, "规则类型只能是 block、boost、pin")
		return
	}
	for _, sc := range rule.Scenes {
		if !contract.ValidScene(sc) {
			writeErr(w, http.StatusBadRequest, 40000, "未知的场景："+sc)
			return
		}
	}
	if rule.StartsAt != nil && rule.EndsAt != nil && *rule.EndsAt <= *rule.StartsAt {
		writeErr(w, http.StatusBadRequest, 40000, "结束时间要晚于开始时间")
		return
	}
	if len([]rune(rule.Note)) > 200 {
		writeErr(w, http.StatusBadRequest, 40000, "备注不超过 200 字")
		return
	}
	t := body.Target
	switch t.Type {
	case "work":
		key := strings.TrimSpace(t.WorkKey)
		if key == "" && t.Source != "" && t.SongID != "" {
			key, _ = a.Svc.PG.WorkKeyOf(r.Context(), appID, strings.ToLower(strings.TrimSpace(t.Source)), strings.TrimSpace(t.SongID))
		}
		if key == "" {
			writeErr(w, http.StatusBadRequest, 40000, "曲库里没有这首歌（作品键或音源与歌曲 ID 不对）")
			return
		}
		tracks, err := a.Svc.PG.Tracks(r.Context(), appID, []string{key})
		if err != nil {
			a.fail(w, err)
			return
		}
		tr, ok := tracks[key]
		if !ok {
			writeErr(w, http.StatusBadRequest, 40000, "曲库里没有这首歌（作品键或音源与歌曲 ID 不对）")
			return
		}
		rule.Target = store.RuleTarget{Type: "work", WorkKey: key, Title: tr.Title, Artist: tr.Artist}
	case "artist":
		name := strings.TrimSpace(contract.FirstArtist(t.Artist))
		if name == "" || len([]rune(name)) > 64 {
			writeErr(w, http.StatusBadRequest, 40000, "歌手名不能为空，且不超过 64 字")
			return
		}
		rule.Target = store.RuleTarget{Type: "artist", Artist: name}
	default:
		writeErr(w, http.StatusBadRequest, 40000, "目标类型只能是 work 或 artist")
		return
	}
	saved, err := a.Svc.PG.SaveRule(r.Context(), rule)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "干预规则已保存", a.ruleViews(r.Context(), appID, []store.Rule{*saved})[0])
}

func (a *Admin) deleteRule(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "规则无效")
		return
	}
	if err := a.Svc.PG.DeleteRule(r.Context(), appID, id); err != nil {
		a.fail(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "干预规则已删除", map[string]bool{"deleted": true})
}

// ── 实验 ──

func (a *Admin) experiments(w http.ResponseWriter, r *http.Request, appID int64) {
	list, err := a.Svc.PG.Experiments(r.Context(), appID, "")
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取实验成功", map[string]any{"items": list})
}

func (a *Admin) saveExperiment(w http.ResponseWriter, r *http.Request, appID int64) {
	var e store.Experiment
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&e); err != nil {
		writeErr(w, http.StatusBadRequest, 40000, "实验格式不正确")
		return
	}
	e.ID, e.AppID, e.Operator = 0, appID, operator(r)
	if id, ok := pathInt(r, "id"); ok {
		e.ID = id
	}
	e.Name, e.Description = strings.TrimSpace(e.Name), strings.TrimSpace(e.Description)
	if err := ValidateExperiment(&e); err != nil {
		a.fail(w, err)
		return
	}
	for _, v := range e.Variants {
		if v.Overrides.ModelRunID != nil {
			run, err := a.Svc.PG.Run(r.Context(), appID, *v.Overrides.ModelRunID)
			if err != nil || run.Status != store.RunSucceeded {
				writeErr(w, http.StatusBadRequest, 40000, "变体 "+v.Key+" 固定的模型版本不存在或没有训练成功")
				return
			}
		}
	}
	saved, err := a.Svc.PG.SaveExperiment(r.Context(), e)
	if err != nil {
		a.failOps(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "实验已保存", saved)
}

func (a *Admin) deleteExperiment(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "实验无效")
		return
	}
	if err := a.Svc.PG.DeleteExperiment(r.Context(), appID, id); err != nil {
		a.failOps(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "实验已删除", map[string]bool{"deleted": true})
}

func (a *Admin) startExperiment(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "实验无效")
		return
	}
	e, err := a.Svc.PG.StartExperiment(r.Context(), appID, id, operator(r))
	if err != nil {
		a.failOps(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "实验已开始", e)
}

func (a *Admin) stopExperiment(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "实验无效")
		return
	}
	e, err := a.Svc.PG.StopExperiment(r.Context(), appID, id, operator(r))
	if err != nil {
		a.failOps(w, err)
		return
	}
	a.opsChanged(appID)
	writeOK(w, "实验已停止", e)
}

func (a *Admin) experimentResults(w http.ResponseWriter, r *http.Request, appID int64) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "实验无效")
		return
	}
	e, err := a.Svc.PG.Experiment(r.Context(), appID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	days := qDays(r, 14)
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if e.StartedAt != nil && time.UnixMilli(*e.StartedAt).After(from) {
		from = time.UnixMilli(*e.StartedAt)
	}
	var variants [][2]string
	for _, v := range e.Variants {
		variants = append(variants, [2]string{v.Key, v.Name})
	}
	res, err := a.Svc.CH.Experiment(r.Context(), appID, id, from, days, variants)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取实验结果成功", res)
}

// ── 搜索与歌手 ──

func (a *Admin) searchStats(w http.ResponseWriter, r *http.Request, appID int64) {
	res, err := a.Svc.CH.Search(r.Context(), appID, qDays(r, 7), qInt(r, "limit", 50, 1, 200))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取搜索分析成功", res)
}

func (a *Admin) artists(w http.ResponseWriter, r *http.Request, appID int64) {
	items, total, err := a.Svc.CH.Artists(r.Context(), appID, qDays(r, 30), r.URL.Query().Get("q"),
		qInt(r, "page", 1, 1, 100000), qInt(r, "pageSize", 20, 1, 100))
	if err != nil {
		a.fail(w, err)
		return
	}
	if items == nil {
		items = []store.ArtistRow{}
	}
	writeOK(w, "获取歌手排行成功", map[string]any{"items": items, "total": total})
}

// ArtistDetailView 歌手详情。
type ArtistDetailView struct {
	*store.ArtistDetailStats
	Similar []SimilarArtist `json:"similar"`
}

// SimilarArtist 相似歌手。
type SimilarArtist struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
	Cover string  `json:"cover"`
}

func (a *Admin) artist(w http.ResponseWriter, r *http.Request, appID int64) {
	name := strings.TrimSpace(r.PathValue("name"))
	stats, err := a.Svc.CH.ArtistDetail(r.Context(), appID, name, qDays(r, 30))
	if err != nil {
		a.fail(w, err)
		return
	}
	if stats == nil {
		writeErr(w, http.StatusNotFound, 40400, "这段时间里没有这位歌手的播放")
		return
	}
	view := ArtistDetailView{ArtistDetailStats: stats, Similar: []SimilarArtist{}}
	if l := a.Svc.Reg.Get(appID); l != nil {
		snap := l.Snap
		for _, n := range snap.ArtistSim[contract.NormalizeArtist(contract.FirstArtist(name))] {
			sa := SimilarArtist{Name: snap.ArtistName[n.Artist], Score: round4(float64(n.Score))}
			for _, i := range snap.ArtistTop[n.Artist] {
				if c := l.Tracks[i].Cover; c != "" {
					sa.Cover = c
					break
				}
			}
			if sa.Name != "" {
				view.Similar = append(view.Similar, sa)
			}
		}
	}
	writeOK(w, "获取歌手详情成功", view)
}

// ── 用户的风格歌单与报告 ──

func (a *Admin) userMixes(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	c, err := a.Svc.mixes(r.Context(), appID, uid, true, true)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取风格歌单成功", map[string]any{"items": c.Mixes, "generatedAt": c.GeneratedAt, "expiresAt": c.ExpiresAt})
}

func (a *Admin) userReport(w http.ResponseWriter, r *http.Request, appID int64) {
	uid, ok := pathInt(r, "user")
	if !ok {
		writeErr(w, http.StatusBadRequest, 40000, "用户无效")
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "week"
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	rep, err := a.Svc.GetReport(r.Context(), appID, uid, period, offset, false)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeOK(w, "获取听歌报告成功", rep)
}
