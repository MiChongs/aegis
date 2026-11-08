package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/catalog"
	"aegis/services/risk/internal/engine"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/models"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/stream"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/route"
)

func (s *Server) routes(g *route.RouterGroup) {
	g.GET("/meta", s.wrap(s.meta))
	g.POST("/expr/validate", s.wrap(s.validateExpr))

	g.GET("/scenes", s.wrap(s.listScenes))
	g.GET("/scenes/:key", s.wrap(s.getScene))
	g.PUT("/scenes/:key", s.wrap(s.saveScene))
	g.DELETE("/scenes/:key", s.wrap(s.deleteScene))
	g.GET("/scenes/:key/bundle", s.wrap(s.draftBundle))
	g.POST("/scenes/:key/validate", s.wrap(s.validateScene))
	g.POST("/scenes/:key/simulate", s.wrap(s.simulate))
	g.POST("/scenes/:key/publish", s.wrap(s.publish))
	g.POST("/scenes/:key/promote", s.wrap(s.promote))
	g.POST("/scenes/:key/rollback", s.wrap(s.rollback))
	g.POST("/scenes/:key/withdraw", s.wrap(s.withdraw))
	g.POST("/scenes/:key/traffic", s.wrap(s.setTraffic))
	g.GET("/scenes/:key/releases", s.wrap(s.listReleases))
	g.GET("/releases/:id", s.wrap(s.getRelease))

	g.GET("/features", s.wrap(s.listFeatures))
	g.PUT("/features/:key", s.wrap(s.saveFeature))
	g.DELETE("/features/:key", s.wrap(s.deleteFeature))
	g.GET("/features/:key/sql", s.wrap(s.featureSQL))
	g.POST("/features/sync", s.wrap(s.syncStreams))

	g.GET("/rules", s.wrap(s.listRules))
	g.POST("/rules", s.wrap(s.saveRule))
	g.PUT("/rules/:id", s.wrap(s.saveRule))
	g.DELETE("/rules/:id", s.wrap(s.deleteRule))
	g.GET("/graph/backfill", s.wrap(s.getBackfill))
	g.POST("/graph/backfill", s.wrap(s.startBackfill))
	g.GET("/templates", s.wrap(s.listTemplates))
	g.POST("/templates/apply", s.wrap(s.applyTemplate))

	g.GET("/models", s.wrap(s.listModels))
	g.POST("/models", s.wrap(s.uploadModel))
	g.PUT("/models/:key", s.wrap(s.updateModel))
	g.DELETE("/models/:key", s.wrap(s.deleteModel))
	g.POST("/models/:key/predict", s.wrap(s.predictModel))

	g.GET("/lists", s.wrap(s.listEntries))
	g.POST("/lists", s.wrap(s.addEntries))
	g.POST("/lists/remove", s.wrap(s.removeEntries))
	g.POST("/lists/check", s.wrap(s.checkList))
	g.POST("/lists/rebuild", s.wrap(s.rebuildLists))

	g.GET("/cep/patterns", s.wrap(s.listPatterns))
	g.PUT("/cep/patterns/:key", s.wrap(s.savePattern))
	g.DELETE("/cep/patterns/:key", s.wrap(s.deletePattern))

	g.GET("/dashboard", s.wrap(s.dashboard))
	g.GET("/decisions", s.wrap(s.listDecisions))
	g.GET("/decisions/:id", s.wrap(s.getDecision))
	g.GET("/events", s.wrap(s.listEvents))
	g.GET("/alerts", s.wrap(s.listAlerts))
	g.GET("/cases", s.wrap(s.listCases))
	g.POST("/cases/:id/review", s.wrap(s.reviewCase))
	g.GET("/graph", s.wrap(s.graph))
	g.GET("/profile", s.wrap(s.profile))
	g.GET("/status", s.wrap(s.status))
	g.GET("/audit", s.wrap(s.audit))
	g.GET("/report", s.wrap(s.report))
	g.GET("/users/score", s.wrap(s.userScore))
	g.POST("/dispose", s.wrap(s.dispose))
}

// ── 元数据 ──

func (s *Server) meta(_ context.Context, _ *app.RequestContext) (any, error) {
	return map[string]any{
		"variables":      engine.Variables(),
		"entities":       bundle.Entities(),
		"dimensions":     bundle.Dimensions(),
		"listTypes":      bundle.ListTypes(),
		"eventTypes":     events.Types(),
		"graphTemplates": bundle.GraphTemplates(),
		"streamColumns":  stream.Columns(),
		"actions":        []string{bundle.ActionPass, bundle.ActionReview, bundle.ActionCaptcha, bundle.ActionBlock, bundle.ActionBan},
		"levels":         []string{bundle.LevelNone, bundle.LevelLow, bundle.LevelMedium, bundle.LevelHigh, bundle.LevelCritical},
		"guardPolicy":    engine.GuardPolicy(),
		"defaultBands":   bundle.DefaultBands(),
		"graphEdges":     graph.Edges(),
	}, nil
}

func (s *Server) validateExpr(_ context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		Expr string `json:"expr"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if err := engine.CompileExpr(in.Expr); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true}, nil
}

// ── 场景 ──

type sceneView struct {
	store.Scene
	State     *store.State `json:"state,omitempty"`
	RuleCount int          `json:"ruleCount"`
}

func (s *Server) listScenes(ctx context.Context, _ *app.RequestContext) (any, error) {
	scenes, err := s.Store.ListScenes(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.Store.ListStates(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := s.Store.ListRules(ctx, "")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	global := 0
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.Scene == "*" {
			global++
		} else {
			counts[r.Scene]++
		}
	}
	out := make([]sceneView, 0, len(scenes))
	for _, sc := range scenes {
		v := sceneView{Scene: sc, RuleCount: counts[sc.Key] + global}
		if st, ok := states[sc.Key]; ok {
			st := st
			v.State = &st
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) getScene(ctx context.Context, c *app.RequestContext) (any, error) {
	sc, err := s.Store.GetScene(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	v := sceneView{Scene: *sc}
	if st, err := s.Store.GetState(ctx, sc.Key); err == nil {
		v.State = st
	}
	return v, nil
}

func (s *Server) saveScene(ctx context.Context, c *app.RequestContext) (any, error) {
	var sc store.Scene
	if err := bind(c, &sc); err != nil {
		return nil, err
	}
	sc.Key = c.Param("key")
	if old, err := s.Store.GetScene(ctx, sc.Key); err == nil {
		sc.Builtin = old.Builtin
	}
	if err := s.Store.UpsertScene(ctx, &sc, operator(c)); err != nil {
		return nil, bad(err.Error())
	}
	s.Store.Audit(ctx, operator(c), "scene.save", sc.Key, sc)
	return s.Store.GetScene(ctx, sc.Key)
}

func (s *Server) deleteScene(ctx context.Context, c *app.RequestContext) (any, error) {
	key := c.Param("key")
	if st, err := s.Store.GetState(ctx, key); err == nil && st.PrimaryID != nil {
		return nil, bad("场景还有生效的发布，先下线再删除")
	}
	if err := s.Store.DeleteScene(ctx, key); err != nil {
		return nil, bad(err.Error())
	}
	s.Store.Audit(ctx, operator(c), "scene.delete", key, nil)
	return nil, nil
}

// ── 特征 ──

func (s *Server) listFeatures(ctx context.Context, _ *app.RequestContext) (any, error) {
	return s.Store.ListFeatures(ctx)
}

func (s *Server) saveFeature(ctx context.Context, c *app.RequestContext) (any, error) {
	var f store.Feature
	if err := bind(c, &f); err != nil {
		return nil, err
	}
	f.Key = c.Param("key")
	// 用一个只含这个特征的发布包做编译校验，与线上是同一套检查
	b := bundle.Bundle{Scene: bundle.Scene{Key: "validate"}, Features: []bundle.Feature{f.Feature}}
	if f.Kind == bundle.FeatureDerived {
		all, _ := s.Store.ListFeatures(ctx)
		for _, o := range all {
			if o.Key != f.Key {
				b.Features = append([]bundle.Feature{o.Feature}, b.Features...)
			}
		}
	}
	if p, err := engine.Compile(ctx, b, nil); err != nil {
		var ce *engine.CompileError
		if errors.As(err, &ce) {
			var mine []engine.Issue
			for _, i := range ce.Issues {
				if i.Key == f.Key {
					mine = append(mine, i)
				}
			}
			if len(mine) > 0 {
				return nil, badData(mine[0].Message, mine)
			}
		} else {
			return nil, err
		}
	} else {
		p.Dispose()
	}
	if f.Kind == bundle.FeatureStream {
		if _, err := stream.SQL(f.Feature, s.Stream); err != nil {
			return nil, bad(err.Error())
		}
	}
	if err := s.Store.UpsertFeature(ctx, &f, operator(c)); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "feature.save", f.Key, f)
	return s.Store.GetFeature(ctx, f.Key)
}

func (s *Server) deleteFeature(ctx context.Context, c *app.RequestContext) (any, error) {
	if err := s.Store.DeleteFeature(ctx, c.Param("key")); err != nil {
		return nil, bad(err.Error())
	}
	s.Store.Audit(ctx, operator(c), "feature.delete", c.Param("key"), nil)
	return nil, nil
}

func (s *Server) featureSQL(ctx context.Context, c *app.RequestContext) (any, error) {
	f, err := s.Store.GetFeature(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	sql, err := stream.Preview(f.Feature, s.Stream)
	if err != nil {
		return nil, bad(err.Error())
	}
	return map[string]any{"sql": sql, "object": stream.ObjectFor(f.Feature)}, nil
}

// liveStreamFeatures 当前生效（正式或候选）的发布包里用到的流式特征
func (s *Server) liveStreamFeatures(ctx context.Context, extra *bundle.Bundle) ([]bundle.Feature, error) {
	states, err := s.Store.ListStates(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bundle.Feature{}
	add := func(b bundle.Bundle) {
		for _, f := range b.Features {
			if f.Kind == bundle.FeatureStream {
				seen[stream.ObjectFor(f).Base] = f
			}
		}
	}
	for _, st := range states {
		for _, id := range []*int64{st.PrimaryID, st.CandidateID} {
			if id == nil {
				continue
			}
			r, err := s.Store.GetRelease(ctx, *id)
			if err != nil {
				return nil, err
			}
			add(r.Bundle)
		}
	}
	if extra != nil {
		add(*extra)
	}
	out := make([]bundle.Feature, 0, len(seen))
	for _, f := range seen {
		out = append(out, f)
	}
	return out, nil
}

func (s *Server) syncStreams(ctx context.Context, c *app.RequestContext) (any, error) {
	fs, err := s.liveStreamFeatures(ctx, nil)
	if err != nil {
		return nil, err
	}
	m, err := s.streamManager(ctx)
	if err != nil {
		return nil, err
	}
	defer m.Close(ctx)
	res, err := m.Sync(ctx, fs)
	if err == nil {
		s.Store.Audit(ctx, operator(c), "stream.sync", "", res)
	}
	return res, err
}

// ── 规则 ──

func (s *Server) listRules(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.Store.ListRules(ctx, string(c.Query("scene")))
}

func (s *Server) saveRule(ctx context.Context, c *app.RequestContext) (any, error) {
	var r store.Rule
	if err := bind(c, &r); err != nil {
		return nil, err
	}
	if id := c.Param("id"); id != "" {
		r.ID, _ = strconv.ParseInt(id, 10, 64)
	}
	if r.Scene == "" {
		return nil, bad("规则要指定场景（* 表示全部场景）")
	}
	if !engine.ValidKey(r.Key) {
		return nil, bad("规则键只能用小写字母、数字、下划线，且以字母开头")
	}
	if err := engine.CompileExpr(r.Expr); err != nil {
		return nil, badData("条件编译失败："+err.Error(), map[string]string{"field": "expr"})
	}
	if r.Decision != "" && !bundle.ValidAction(r.Decision) {
		return nil, bad("强制处置不合法")
	}
	if r.Mode != "" && r.Mode != bundle.ModeEnforce && r.Mode != bundle.ModeObserve {
		return nil, bad("模式只能是 enforce 或 observe")
	}
	if err := s.Store.SaveRule(ctx, &r, operator(c)); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return nil, bad("这个场景下已经有同键规则")
		}
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "rule.save", fmt.Sprintf("%s/%s", r.Scene, r.Key), r)
	return s.Store.GetRule(ctx, r.ID)
}

func (s *Server) deleteRule(ctx context.Context, c *app.RequestContext) (any, error) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	r, err := s.Store.GetRule(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteRule(ctx, id); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "rule.delete", r.Scene+"/"+r.Key, r)
	return nil, nil
}

func (s *Server) listTemplates(_ context.Context, _ *app.RequestContext) (any, error) {
	return catalog.Templates(), nil
}

func (s *Server) applyTemplate(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		Template string `json:"template"`
		Scene    string `json:"scene"`
		Observe  bool   `json:"observe"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	n, skipped, err := catalog.Apply(ctx, s.Store, in.Template, in.Scene, in.Observe, operator(c))
	if err != nil {
		return nil, bad(err.Error())
	}
	s.Store.Audit(ctx, operator(c), "template.apply", in.Scene+"/"+in.Template, in)
	return map[string]int{"installed": n, "skipped": skipped}, nil
}

// ── 模型 ──

func (s *Server) listModels(ctx context.Context, _ *app.RequestContext) (any, error) {
	return s.Store.ListModels(ctx)
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) uploadModel(ctx context.Context, c *app.RequestContext) (any, error) {
	key := strings.TrimSpace(string(c.FormValue("key")))
	if !engine.ValidKey(key) {
		return nil, bad("模型键只能用小写字母、数字、下划线，且以字母开头")
	}
	format := string(c.FormValue("format"))
	fh, err := c.FormFile("file")
	if err != nil {
		return nil, bad("缺模型文件")
	}
	if fh.Size > models.MaxSize {
		return nil, bad("模型文件超过 64 MB")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	ens, err := models.Parse(format, data)
	if err != nil {
		return nil, bad("模型解析失败：" + err.Error())
	}
	inputs := splitList(string(c.FormValue("inputs")))
	if len(inputs) != ens.NFeatures() {
		return nil, badData(fmt.Sprintf("模型需要 %d 个输入，映射了 %d 个", ens.NFeatures(), len(inputs)), map[string]int{"nFeatures": ens.NFeatures()})
	}
	objectKey, checksum, err := s.Models.Put(ctx, key, data)
	if err != nil {
		return nil, err
	}
	m := &store.Model{Model: bundle.Model{Key: key, Name: string(c.FormValue("name")), Format: format, ObjectKey: objectKey, Checksum: checksum,
		Inputs: inputs, Transform: string(c.FormValue("transform")) != "false"},
		Description: string(c.FormValue("description")), NFeatures: ens.NFeatures(), SizeBytes: int64(len(data))}
	if m.Name == "" {
		m.Name = key
	}
	if err := s.Store.UpsertModel(ctx, m, operator(c)); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "model.upload", key, map[string]any{"checksum": checksum, "size": len(data)})
	return s.Store.GetModel(ctx, key)
}

func (s *Server) updateModel(ctx context.Context, c *app.RequestContext) (any, error) {
	m, err := s.Store.GetModel(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Inputs      []string `json:"inputs"`
		Transform   *bool    `json:"transform"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if in.Inputs != nil {
		if len(in.Inputs) != m.NFeatures {
			return nil, bad(fmt.Sprintf("模型需要 %d 个输入", m.NFeatures))
		}
		m.Inputs = in.Inputs
	}
	if in.Name != "" {
		m.Name = in.Name
	}
	m.Description = in.Description
	if in.Transform != nil {
		m.Transform = *in.Transform
	}
	if err := s.Store.UpsertModel(ctx, m, operator(c)); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Server) deleteModel(ctx context.Context, c *app.RequestContext) (any, error) {
	scenes, _ := s.Store.ListScenes(ctx)
	for _, sc := range scenes {
		for _, k := range sc.Models {
			if k == c.Param("key") {
				return nil, bad("模型仍被场景 " + sc.Key + " 使用")
			}
		}
	}
	if err := s.Store.DeleteModel(ctx, c.Param("key")); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "model.delete", c.Param("key"), nil)
	return nil, nil
}

func (s *Server) predictModel(ctx context.Context, c *app.RequestContext) (any, error) {
	m, err := s.Store.GetModel(ctx, c.Param("key"))
	if err != nil {
		return nil, err
	}
	var in struct {
		Values map[string]float64 `json:"values"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	ens, err := s.Models.Load(ctx, m.Model)
	if err != nil {
		return nil, err
	}
	vals := make([]float64, len(m.Inputs))
	for i, k := range m.Inputs {
		vals[i] = in.Values[k]
	}
	return map[string]any{"score": ens.PredictSingle(vals, 0), "inputs": m.Inputs, "values": vals}, nil
}

// ── 名单 ──

func (s *Server) listEntries(ctx context.Context, c *app.RequestContext) (any, error) {
	q := store.ListQuery{ListType: string(c.Query("type")), Kind: string(c.Query("kind")), Value: string(c.Query("value")),
		AppID: qapp(c), Source: string(c.Query("source")), Active: string(c.Query("active")) != "false",
		Page: qint(c, "page", 1), Limit: qint(c, "limit", 50)}
	list, total, err := s.Store.ListEntries(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []lists.Entry{}
	}
	return paged(list, total, q.Page, q.Limit), nil
}

type addListInput struct {
	ListType        string   `json:"listType"`
	Kind            string   `json:"kind"`
	Values          []string `json:"values"`
	AppID           int64    `json:"appId"`
	Scenes          []string `json:"scenes"`
	Reason          string   `json:"reason"`
	DurationSeconds int64    `json:"durationSeconds"`
}

func (s *Server) addEntries(ctx context.Context, c *app.RequestContext) (any, error) {
	var in addListInput
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	entries, invalid, err := s.makeEntries(in, "manual", operator(c), "")
	if err != nil {
		return nil, bad(err.Error())
	}
	if err := s.putEntries(ctx, entries); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "list.add", in.ListType, in)
	return map[string]any{"added": len(entries), "invalid": invalid}, nil
}

func (s *Server) makeEntries(in addListInput, source, by, decisionID string) ([]lists.Entry, []string, error) {
	if !contains(bundle.ListTypes(), in.ListType) {
		return nil, nil, errors.New("名单类型不合法")
	}
	if in.Kind != bundle.KindBlock && in.Kind != bundle.KindAllow && in.Kind != bundle.KindWatch {
		return nil, nil, errors.New("名单种类只能是 block / allow / watch")
	}
	if len(in.Values) == 0 || len(in.Values) > 10000 {
		return nil, nil, errors.New("一次 1-10000 个取值")
	}
	now := time.Now().UTC()
	var exp *time.Time
	if in.DurationSeconds > 0 {
		t := now.Add(time.Duration(in.DurationSeconds) * time.Second)
		exp = &t
	}
	var out []lists.Entry
	var invalid []string
	seen := map[string]bool{}
	for _, raw := range in.Values {
		v, err := lists.Normalize(in.ListType, raw)
		if err != nil {
			invalid = append(invalid, raw)
			continue
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, lists.Entry{ID: events.NewID(), ListType: in.ListType, Value: v, Kind: in.Kind, AppID: in.AppID, Scenes: in.Scenes,
			Reason: in.Reason, Source: source, CreatedBy: by, DecisionID: decisionID, ExpiresAt: exp, CreatedAt: now})
	}
	return out, invalid, nil
}

// putEntries 先落库再写索引：落库失败什么都没发生；索引失败可以 /lists/rebuild 从库里重建
func (s *Server) putEntries(ctx context.Context, es []lists.Entry) error {
	for i := 0; i < len(es); i += 500 {
		j := min(i+500, len(es))
		if err := s.Store.PutEntries(ctx, es[i:j]); err != nil {
			return err
		}
		if err := s.Lists.Put(ctx, es[i:j]); err != nil {
			return fmt.Errorf("已保存，但名单索引写入失败（可重建索引）：%w", err)
		}
	}
	s.markGraphBanned(es, true)
	return nil
}

func (s *Server) markGraphBanned(es []lists.Entry, banned bool) {
	if s.Graph == nil {
		return
	}
	for _, e := range es {
		if e.Kind != bundle.KindBlock {
			continue
		}
		var vid string
		switch e.ListType {
		case bundle.ListDevice:
			vid = graph.DeviceVID(e.Value)
		case bundle.ListIP:
			vid = graph.IPVID(e.Value)
		case bundle.ListUser:
			if e.AppID > 0 {
				uid, _ := strconv.ParseInt(e.Value, 10, 64)
				vid = graph.UserVID(e.AppID, uid)
			}
		}
		if vid != "" {
			_ = s.Graph.SetBanned(vid, banned)
		}
	}
}

func (s *Server) removeEntries(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	removed, err := s.Store.RemoveEntries(ctx, in.IDs, operator(c))
	if err != nil {
		return nil, err
	}
	if err := s.Lists.Remove(ctx, removed); err != nil {
		return nil, err
	}
	s.markGraphBanned(removed, false)
	s.Store.Audit(ctx, operator(c), "list.remove", "", in.IDs)
	return map[string]int{"removed": len(removed)}, nil
}

func (s *Server) checkList(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		AppID  int64             `json:"appId"`
		Scene  string            `json:"scene"`
		Values map[string]string `json:"values"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if err := s.Lists.Refresh(ctx); err != nil {
		return nil, err
	}
	return s.Lists.Check(ctx, engine.ListQuery{AppID: in.AppID, Scene: in.Scene, Values: in.Values})
}

// rebuildLists 从库里全量重写 Redis 索引（索引丢失或怀疑不一致时）
func (s *Server) rebuildLists(ctx context.Context, c *app.RequestContext) (any, error) {
	n := 0
	err := s.Store.EachActiveEntry(ctx, func(es []lists.Entry) error {
		n += len(es)
		return s.Lists.Put(ctx, es)
	})
	if err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "list.rebuild", "", map[string]int{"entries": n})
	return map[string]int{"entries": n}, nil
}

// ── CEP ──

func (s *Server) listPatterns(ctx context.Context, _ *app.RequestContext) (any, error) {
	return s.Store.ListPatterns(ctx, false)
}

func (s *Server) savePattern(ctx context.Context, c *app.RequestContext) (any, error) {
	var p bundle.CEPPattern
	if err := bind(c, &p); err != nil {
		return nil, err
	}
	p.Key = c.Param("key")
	if !engine.ValidKey(p.Key) {
		return nil, bad("模式键只能用小写字母、数字、下划线，且以字母开头")
	}
	for _, st := range p.Steps {
		for _, cond := range st.Conditions {
			if !contains(stream.Columns(), cond.Field) {
				return nil, bad("没有字段 " + cond.Field)
			}
		}
	}
	if err := s.Store.UpsertPattern(ctx, &p, operator(c)); err != nil {
		return nil, bad(err.Error())
	}
	s.Store.Audit(ctx, operator(c), "cep.save", p.Key, p)
	return p, nil
}

func (s *Server) deletePattern(ctx context.Context, c *app.RequestContext) (any, error) {
	if err := s.Store.DeletePattern(ctx, c.Param("key")); err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "cep.delete", c.Param("key"), nil)
	return nil, nil
}

// internalPatterns Flink 作业拉取启用的模式与版本号
func (s *Server) internalPatterns(ctx context.Context, _ *app.RequestContext) (any, error) {
	ps, err := s.Store.ListPatterns(ctx, true)
	if err != nil {
		return nil, err
	}
	v, err := s.Store.PatternsVersion(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]bundle.CEPPattern, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.CEPPattern)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return map[string]any{"version": v, "patterns": out}, nil
}

func (s *Server) audit(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.Store.ListAudit(ctx, min(qint(c, "limit", 100), 500))
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var _ = json.Marshal
