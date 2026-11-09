package admin

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/config"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/redisx"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/vector"
)

// 解除 / 删号之后关系图、向量、图特征缓存的收尾。需要本地 compose 的 risk profile：
// RISK_IT=1 go test ./services/risk/internal/admin -run SubjectRefreshIT
func TestSubjectRefreshIT(t *testing.T) {
	if os.Getenv("RISK_IT") == "" {
		t.Skip("RISK_IT 未设置")
	}
	ctx := context.Background()
	cfg := config.Load()
	st, err := store.Open(ctx, cfg.PGDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rdb := redisx.New(cfg)
	defer rdb.Close()
	if err := graph.Migrate(cfg, t.Logf); err != nil {
		t.Fatal(err)
	}
	g, err := graph.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	s := &Server{Cfg: cfg, Store: st, Lists: lists.NewIndex(rdb), Redis: rdb, Graph: g, Logf: t.Logf}
	if v, err := vector.Open(ctx, cfg); err == nil {
		if err := v.Migrate(ctx); err == nil {
			s.Vector = v
			defer v.Close(ctx)
		}
	}

	now := time.Now()
	ts := now.UnixMilli()
	app := int64(990000 + now.UnixNano()%9000)
	suffix := strconv.FormatInt(now.UnixNano(), 36)
	devA, devC := "it-dev-a-"+suffix, "it-dev-c-"+suffix
	ipShared, ipCarol := "198.51.100."+strconv.Itoa(int(now.Unix()%200)+1), "203.0.113."+strconv.Itoa(int(now.Unix()%200)+1)
	if err := g.Apply([]events.Event{
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 1, Account: "alice-" + suffix, IP: ipShared, DeviceID: devA, Phone: "1390000" + suffix[len(suffix)-4:]},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 2, Account: "bob-" + suffix, IP: ipShared, DeviceID: devA},
		{Type: events.TypeLoginSuccess, TS: ts, AppID: app, UserID: 3, Account: "carol-" + suffix, IP: ipCarol, DeviceID: devC},
	}); err != nil {
		t.Fatal(err)
	}
	if s.Vector != nil {
		vec := func(d string) []float32 { return vector.Embed(d) }
		if err := s.Vector.Upsert(ctx, []vector.Device{
			{AppID: app, DeviceID: devA, UserID: 1, UpdatedAt: now.Unix(), Vec: vec(devA)},
			{AppID: app, DeviceID: devC, UserID: 3, UpdatedAt: now.Unix(), Vec: vec(devC)},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 设备 A 有两条黑名单，用户 1 有一条
	mk := func(listType, value string) lists.Entry {
		es, _, err := s.makeEntries(addListInput{ListType: listType, Kind: bundle.KindBlock, Values: []string{value}, AppID: app, Reason: "it"}, "manual", "it", "")
		if err != nil || len(es) != 1 {
			t.Fatalf("建名单失败：%v", err)
		}
		return es[0]
	}
	e1, e2, eu := mk(bundle.ListDevice, devA), mk(bundle.ListDevice, devA), mk(bundle.ListUser, "1")
	if err := s.putEntries(ctx, []lists.Entry{e1, e2, eu}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = st.RemoveEntries(context.Background(), []string{e1.ID, e2.ID, eu.ID}, "it") })

	prop := func(vid, k string) any {
		t.Helper()
		ps, err := g.Props([]string{vid})
		if err != nil {
			t.Fatal(err)
		}
		if ps[vid] == nil {
			return nil
		}
		return ps[vid][k]
	}
	exists := func(vid string) bool {
		ps, err := g.Props([]string{vid})
		if err != nil {
			t.Fatal(err)
		}
		return ps[vid] != nil
	}

	// ── 解除：移掉设备的一条黑名单，另一条还在，设备仍算封禁 ──
	removed, err := st.RemoveEntries(ctx, []string{e1.ID}, "it")
	if err != nil {
		t.Fatal(err)
	}
	s.recomputeBanned(ctx, removed)
	if prop(graph.DeviceVID(devA), "banned") != true {
		t.Fatalf("设备还在另一条黑名单里，不应被标成未封禁")
	}
	// 用户 1 没有封号但还在用户黑名单里：按封禁算
	nb := s.collectNeighborhood(ctx, app, []int64{1})
	s.refreshAfterRelease(ctx, nb, nil)
	if prop(graph.UserVID(app, 1), "banned") != true {
		t.Fatalf("用户还在黑名单里，应保持封禁")
	}
	if _, err := st.RemoveEntries(ctx, []string{eu.ID}, "it"); err != nil {
		t.Fatal(err)
	}
	s.refreshAfterRelease(ctx, nb, nil)
	if prop(graph.UserVID(app, 1), "banned") != false || prop(graph.UserVID(app, 1), "status") != "active" {
		t.Fatalf("黑名单与封号都没了，应恢复正常：%v %v", prop(graph.UserVID(app, 1), "banned"), prop(graph.UserVID(app, 1), "status"))
	}
	s.refreshAfterRelease(ctx, nb, map[int64]bool{1: true})
	if prop(graph.UserVID(app, 1), "banned") != true {
		t.Fatalf("主服务说仍在封号中，应按封禁算")
	}

	// 图特征缓存：关联账号（bob）与设备的缓存都要清掉
	var gf *bundle.Feature
	if fs, err := st.ListFeatures(ctx); err == nil {
		for _, f := range fs {
			if f.Kind == bundle.FeatureGraph && f.Graph != nil && (f.Entity == "" || f.Entity == bundle.EntityUser) {
				ff := f.Feature
				gf = &ff
				break
			}
		}
	}
	cacheKey := ""
	if gf != nil {
		cacheKey = "rg:" + gf.Graph.Template + ":" + strconv.Itoa(gf.Graph.WithinDays) + ":" + strconv.FormatInt(app, 10) + ":user:2"
		if err := rdb.Set(ctx, cacheKey, "3", time.Hour).Err(); err != nil {
			t.Fatal(err)
		}
	}

	// ── 删号：carol 独有的设备、IP、登录名一起删；alice 的设备还有 bob 在用，留着并改挂到 bob ──
	nbDel := s.collectNeighborhood(ctx, app, []int64{1, 3})
	n, problems := s.refreshAfterDelete(ctx, nbDel)
	if len(problems) > 0 {
		t.Fatalf("删号收尾有问题：%v", problems)
	}
	if n == 0 {
		t.Fatalf("应删掉点")
	}
	for _, vid := range []string{graph.UserVID(app, 1), graph.UserVID(app, 3), graph.DeviceVID(devC), graph.IPVID(ipCarol), graph.AccountVID(app, "carol-"+suffix)} {
		if exists(vid) {
			t.Errorf("%s 应已删除", vid)
		}
	}
	if !exists(graph.DeviceVID(devA)) || !exists(graph.UserVID(app, 2)) {
		t.Fatalf("bob 与仍在用的设备不应删除")
	}
	if users := prop(graph.IPVID(ipShared), "users"); users != int64(1) {
		t.Fatalf("共用 IP 只剩 bob，用户数应为 1：%v", users)
	}
	if left, err := g.RelatedUsers(app, []int64{2}, graph.RelatedOptions{}); err != nil || len(left) != 0 {
		t.Fatalf("bob 不应再关联到已删除的账号：%+v %v", left, err)
	}
	if cacheKey != "" {
		if n, _ := rdb.Exists(ctx, cacheKey).Result(); n != 0 {
			t.Fatalf("关联账号的图特征缓存应已清掉")
		}
	}
	if s.Vector != nil {
		// Milvus 检索默认是有界一致性：写入后几秒内才可见
		var last string
		ok := false
		for i := 0; i < 20 && !ok; i++ {
			ns, err := s.Vector.Similar(ctx, app, devA+"x", 5)
			if err != nil {
				t.Fatal(err)
			}
			ok = true
			for _, nn := range ns {
				if nn.DeviceID == devC || (nn.DeviceID == devA && nn.UserID != 2) {
					ok = false
					last = nn.DeviceID + " user " + strconv.FormatInt(nn.UserID, 10)
				}
			}
			if !ok {
				time.Sleep(500 * time.Millisecond)
			}
		}
		if !ok {
			t.Fatalf("设备向量没有更新（没人用的应删除，设备 A 应改挂到 bob）：%s", last)
		}
	}
	_ = g.DeleteVertices([]string{graph.UserVID(app, 2), graph.DeviceVID(devA), graph.IPVID(ipShared), graph.AccountVID(app, "bob-"+suffix)})
	if s.Vector != nil {
		_ = s.Vector.Delete(ctx, app, []string{devA})
	}
}
