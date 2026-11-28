package cloudstorage

import "testing"

func testPlan(tier int64, quota int64) Plan {
	return Plan{TierID: tier, QuotaBytes: quota, MaxItemBytes: 1 << 10, MaxItems: 10, MaxRevisions: 3,
		TrashRetentionDays: 7, SyncEnabled: true, MaxDevices: 2, MaxSyncRecords: 100, PollSeconds: 60}
}

// 档位按列表顺序定优先级：同时持有两个档位时排在前面的那个生效，都没有时落到免费档。
func TestResolvePlanFollowsListOrder(t *testing.T) {
	cfg := DefaultConfig(1)
	cfg.Plans = []Plan{testPlan(2, 300), testPlan(5, 200), testPlan(1, 100), testPlan(0, 10)}
	cases := []struct {
		held []int64
		want int64
	}{
		{nil, 10},
		{[]int64{1}, 100},
		{[]int64{1, 5}, 200},
		{[]int64{5, 2}, 300},
		{[]int64{99}, 10},
	}
	for _, c := range cases {
		if got := cfg.ResolvePlan(c.held).QuotaBytes; got != c.want {
			t.Errorf("持有 %v 应得配额 %d，得到 %d", c.held, c.want, got)
		}
	}
}

// 没配免费档时落到基础档（应用配置本身），升级前的应用行为不变。
func TestResolvePlanFallsBackToBase(t *testing.T) {
	cfg := DefaultConfig(1)
	cfg.Plans = []Plan{testPlan(5, 200)}
	plan := cfg.ResolvePlan(nil)
	if plan.QuotaBytes != cfg.QuotaBytes || !plan.SyncEnabled || !plan.Realtime || len(plan.Collections) != 0 {
		t.Fatalf("基础档应沿用应用配额并开放全部同步：%+v", plan)
	}
	if got := cfg.WithPlan(cfg.ResolvePlan([]int64{5})).QuotaBytes; got != 200 {
		t.Fatalf("WithPlan 应套上档位配额，得到 %d", got)
	}
}

func TestNormalizePlansRejectsBadInput(t *testing.T) {
	cases := map[string][]Plan{
		"免费档重复": {testPlan(0, 10), testPlan(0, 20)},
		"档位重复":  {testPlan(3, 10), testPlan(3, 20)},
		"设备为零":  {func() Plan { p := testPlan(1, 10); p.MaxDevices = 0; return p }()},
		"集合非法":  {func() Plan { p := testPlan(1, 10); p.Collections = []string{"Music"}; return p }()},
		"单条超配额": {func() Plan { p := testPlan(1, 10); p.MaxItemBytes = 20; return p }()},
	}
	for name, plans := range cases {
		cfg := DefaultConfig(1)
		cfg.Plans = plans
		if err := cfg.Normalize(); err == nil {
			t.Errorf("%s：应校验失败", name)
		}
	}
	cfg := DefaultConfig(1)
	plan := testPlan(1, 1<<20)
	plan.PollSeconds = 0
	plan.Collections = []string{"music.history", "music.history"}
	cfg.Plans = []Plan{plan}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.Plans[0].PollSeconds != DefaultPollSeconds || len(cfg.Plans[0].Collections) != 1 {
		t.Fatalf("轮询间隔应补默认值、集合去重：%+v", cfg.Plans[0])
	}
}

func TestMinRetention(t *testing.T) {
	cfg := DefaultConfig(1)
	a, b := testPlan(0, 10), testPlan(1, 100)
	a.HistoryRetentionDays, b.HistoryRetentionDays = 30, 0
	a.TrashRetentionDays, b.TrashRetentionDays = 7, 90
	cfg.Plans = []Plan{b, a}
	if got := cfg.MinHistoryRetentionDays(); got != 30 {
		t.Fatalf("永久的档位不参与，最短历史保留应为 30，得到 %d", got)
	}
	if got := cfg.MinTrashRetentionDays(); got != 7 {
		t.Fatalf("最短回收站保留应为 7，得到 %d", got)
	}
}

func TestCollectionNames(t *testing.T) {
	for _, ok := range []string{"music.history", "novel.bookshelf", "app.favorites", "x1.y_z-2"} {
		if !ValidCollection(ok) {
			t.Errorf("%q 应合法", ok)
		}
	}
	for _, bad := range []string{"music", "Music.history", "music.", ".history", "music.history.x", "1a.b"} {
		if ValidCollection(bad) {
			t.Errorf("%q 应不合法", bad)
		}
	}
	if info := LookupCollection("game.saves"); info.Domain != "game" || info.Kind != CollectionKindState {
		t.Fatalf("目录外的集合应按名字推领域：%+v", info)
	}
}
