package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	cloudstorage "aegis/internal/domain/cloudstorage"
	storagedomain "aegis/internal/domain/storage"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

type recordedEvent struct {
	userID    int64
	eventType string
	data      map[string]any
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (p *recordingPublisher) PublishUserEvent(_ context.Context, _ int64, userID int64, eventType string, data map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, recordedEvent{userID: userID, eventType: eventType, data: data})
	return nil
}

func (p *recordingPublisher) count(eventType string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, event := range p.events {
		if event.eventType == eventType {
			n++
		}
	}
	return n
}

// 云服务端到端集成测试：真实 Postgres。
//
// 档位：免费档 2 台设备、不实时、不接力、历史留 1 天；VIP 档位 5 台、实时、接力、历史永久。
// 走一遍：设备名额 → 推送与逐条最后写入者胜 → 增量拉取（游标、分页、按集合筛）→ 墓碑 →
// 档位不允许的集合 → 共用配额 → 开通会员后档位切换 → 设备视图与正在播放 → 接力 →
// 清空集合 → 墓碑清理推进地平线、旧游标 reset → 按档位裁剪历史 → 清空云存储。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55481/postgres?lc_messages=C go test ./internal/service -run TestCloudServiceIntegration
func TestCloudServiceIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(31200)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'cloud-service-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.UpsertStorageConfig(ctx, storagedomain.Config{
		Scope: storagedomain.ScopeGlobal, Provider: storagedomain.ProviderLocal, ConfigName: "local-cloud-service",
		AccessMode: storagedomain.AccessPrivate, Enabled: true, IsDefault: true,
		ConfigData: map[string]any{"root_dir": t.TempDir()},
	}); err != nil {
		t.Fatal(err)
	}

	vip := NewVipService(zap.NewNop(), pg)
	monthly := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("VIP 月卡"), DurationDays: ptrTo(30), Price: ptrTo(decimal.NewFromInt(6)),
		Features: &[]string{},
	})

	cfg := cloudstorage.DefaultConfig(appID)
	cfg.Enabled = true
	cfg.StorageConfigName = "local-cloud-service"
	free := cloudstorage.Plan{
		TierID: 0, Name: "免费", QuotaBytes: 4 << 10, MaxItemBytes: 2 << 10, MaxItems: 10, MaxRevisions: 2,
		TrashRetentionDays: 7, SyncEnabled: true, MaxDevices: 2, MaxSyncRecords: 50, HistoryRetentionDays: 1,
		Realtime: false, PollSeconds: 600, Handoff: false,
		Collections: []string{"music.history", "music.favorites", "video.history", "novel.bookshelf"},
	}
	paid := cloudstorage.Plan{
		TierID: monthly.TierID, QuotaBytes: 1 << 20, MaxItemBytes: 64 << 10, MaxItems: 100, MaxRevisions: 5,
		TrashRetentionDays: 30, SyncEnabled: true, MaxDevices: 5, MaxSyncRecords: 1000, HistoryRetentionDays: 0,
		Realtime: true, PollSeconds: 300, Handoff: true,
	}
	cfg.Plans = []cloudstorage.Plan{paid, free}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.SaveCloudStorageConfig(ctx, cfg); err != nil {
		t.Fatal(err)
	}

	storage := NewStorageService(zap.NewNop(), pg, nil, "test")
	storage.SetLinkSigningKey("master")
	svc := NewCloudStorageService(zap.NewNop(), pg, storage)
	events := &recordingPublisher{}
	svc.SetEventPublisher(events)
	user := insertTestUser(t, ctx, pool, appID, "cloud-service-user", "100")

	register := func(id, model string) error {
		_, _, err := svc.RegisterDevice(ctx, appID, user, CloudDeviceInput{DeviceInfo: cloudstorage.DeviceInfo{
			DeviceID: id, Model: model, Platform: "android",
		}})
		return err
	}
	at := time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)
	change := func(collection, id, title string, offset time.Duration) cloudstorage.SyncChange {
		payload, _ := json.Marshal(map[string]any{"title": title})
		return cloudstorage.SyncChange{Collection: collection, ID: id, Payload: payload, UpdatedAt: at.Add(offset)}
	}

	// ── 设备名额：免费档 2 台 ──
	if err := register("phone-a", "Xiaomi 17"); err != nil {
		t.Fatal(err)
	}
	if err := register("tablet-b", "Pad 7"); err != nil {
		t.Fatal(err)
	}
	if err := register("pc-c", "Windows"); errCode(err) != errCodeCloudSyncDeviceLimit {
		t.Fatalf("第三台设备应回 41364，得到 %v", err)
	}
	if err := register("phone-a", "Xiaomi 17"); err != nil {
		t.Fatalf("已登记设备重复登记不占名额：%v", err)
	}
	if _, err := svc.Push(ctx, appID, user, "pc-c", []cloudstorage.SyncChange{change("music.history", "x", "x", 0)}); errCode(err) != errCodeCloudSyncDeviceNotFound {
		t.Fatalf("没登记的设备推送应回 40434，得到 %v", err)
	}

	// ── 推送与最后写入者胜 ──
	result, err := svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{
		change("music.history", "kw:1", "晴天", 0),
		change("music.history", "kw:2", "七里香", time.Second),
		change("video.history", "work|2024", "繁花", 2*time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 3 || result.Seq != 3 || len(result.Rejected) != 0 {
		t.Fatalf("首批应全部接受、seq=3，得到 %+v", result)
	}
	if events.count(cloudEventSync) != 0 {
		t.Fatalf("免费档不实时，不应推 cloud.sync")
	}
	// 平板带着更旧的时间改同一条：被拒，交回云端那一版
	result, err = svc.Push(ctx, appID, user, "tablet-b", []cloudstorage.SyncChange{
		change("music.history", "kw:1", "晴天（旧）", -time.Minute),
		change("music.history", "kw:2", "七里香（新）", time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 1 || len(result.Rejected) != 1 || result.Rejected[0].ID != "kw:1" || result.Seq != 4 {
		t.Fatalf("旧改动应被拒、新改动接受，得到 %+v", result)
	}
	// 同一毫秒：设备号字典序大的胜（tablet-b > phone-a）
	result, err = svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{change("music.history", "kw:2", "同刻", time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 0 || len(result.Rejected) != 1 {
		t.Fatalf("同一时刻 phone-a 应输给 tablet-b，得到 %+v", result)
	}

	// ── 增量拉取 ──
	page, err := svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: 0, Limit: 2}, "tablet-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || !page.HasMore || page.Next != page.Records[1].Seq {
		t.Fatalf("分页拉取不对：%+v", page)
	}
	page, err = svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: page.Next, Limit: 10}, "tablet-b")
	if err != nil {
		t.Fatal(err)
	}
	if page.HasMore || page.Next != 4 || len(page.Records) != 1 {
		t.Fatalf("第二页应拉到末尾 seq=4，得到 %+v", page)
	}
	filtered, err := svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Collections: []string{"video.history"}, Limit: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Records) != 1 || filtered.Records[0].Collection != "video.history" || filtered.Next != 4 {
		t.Fatalf("按集合筛不对：%+v", filtered)
	}
	if filtered.Records[0].DeviceName != "Xiaomi 17" {
		t.Fatalf("记录应带写入设备名，得到 %q", filtered.Records[0].DeviceName)
	}

	// ── 档位不允许的集合、格式校验 ──
	if _, err := svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{change("drama.history", "d1", "x", 0)}); errCode(err) != errCodeCloudSyncCollectionPlan {
		t.Fatalf("免费档不能同步短剧，应回 42265，得到 %v", err)
	}
	if _, err := svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{{Collection: "Music", ID: "1", UpdatedAt: at}}); errCode(err) != errCodeCloudSyncInvalidRecord {
		t.Fatalf("非法集合名应回 42264，得到 %v", err)
	}
	if _, err := svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{{Collection: "music.history", ID: "1", Payload: json.RawMessage(`[1]`), UpdatedAt: at}}); errCode(err) != errCodeCloudSyncInvalidRecord {
		t.Fatalf("内容不是对象应回 42264，得到 %v", err)
	}

	// ── 墓碑 ──
	result, err = svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{{Collection: "music.history", ID: "kw:1", Deleted: true, UpdatedAt: at.Add(2 * time.Hour)}})
	if err != nil || result.Accepted != 1 {
		t.Fatalf("删除应被接受：%+v %v", result, err)
	}
	page, err = svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: 4, Limit: 10}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || !page.Records[0].Deleted || page.Records[0].Payload != nil {
		t.Fatalf("应拉到墓碑：%+v", page.Records)
	}

	// ── 共用配额：免费档 4 KiB ──
	big := map[string]any{"title": string(make([]byte, 0))}
	filler := make([]byte, 1500)
	for i := range filler {
		filler[i] = 'a'
	}
	big["blob"] = string(filler)
	bigPayload, _ := json.Marshal(big)
	var quotaErr error
	for i := 0; i < 4 && quotaErr == nil; i++ {
		_, quotaErr = svc.Push(ctx, appID, user, "phone-a", []cloudstorage.SyncChange{{
			Collection: "novel.bookshelf", ID: fmt.Sprintf("book-%d", i), Payload: bigPayload, UpdatedAt: at,
		}})
	}
	if errCode(quotaErr) != errCodeCloudQuotaExceeded {
		t.Fatalf("同步记录应计入配额并回 41360，得到 %v", quotaErr)
	}
	status, err := svc.Status(ctx, appID, user)
	if err != nil {
		t.Fatal(err)
	}
	if status.Plan == nil || status.Plan.Name != "免费" || status.SyncBytes == 0 || status.UsedBytes != status.ItemBytes+status.SyncBytes {
		t.Fatalf("状态应带免费档与同步用量：%+v", status)
	}

	// ── 开通会员：档位切换为 VIP（实时、接力、5 台设备） ──
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: user, AppID: appID, PlanID: monthly.ID,
		Quantity: 1, Reason: "测试", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := register("pc-c", "Windows"); err != nil {
		t.Fatalf("VIP 档位应能登记第三台设备：%v", err)
	}
	result, err = svc.Push(ctx, appID, user, "pc-c", []cloudstorage.SyncChange{change("drama.history", "hongguo:9", "短剧", 0)})
	if err != nil || result.Accepted != 1 {
		t.Fatalf("VIP 档位不限集合：%+v %v", result, err)
	}
	if events.count(cloudEventSync) != 1 {
		t.Fatalf("VIP 档位应实时推 cloud.sync")
	}
	plans, err := svc.Plans(ctx, appID, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].Name != "免费" || plans[0].Current || !plans[1].Current || plans[1].TierName == "" {
		t.Fatalf("档位对比不对：%+v", plans)
	}

	// ── 设备视图与正在播放 ──
	activity := &cloudstorage.Activity{Collection: "music.history", RecordID: "kw:2", State: cloudstorage.ActivityPlaying,
		Payload: json.RawMessage(`{"title":"七里香","position":61000}`)}
	if err := svc.SetActivity(ctx, appID, user, "tablet-b", activity); err != nil {
		t.Fatal(err)
	}
	overview, err := svc.Overview(ctx, appID, user, "pc-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Devices) != 3 || !overview.Devices[0].Current || overview.Devices[0].DeviceID != "pc-c" {
		t.Fatalf("当前设备应排第一：%+v", overview.Devices)
	}
	var tablet cloudstorage.SyncDevice
	for _, device := range overview.Devices {
		if device.DeviceID == "tablet-b" {
			tablet = device
		}
	}
	if tablet.Activity == nil || tablet.Activity.State != cloudstorage.ActivityPlaying || tablet.Collections["music.history"] != 1 {
		t.Fatalf("平板应显示正在播放与它写入的记录：%+v", tablet)
	}
	records, err := svc.Records(ctx, cloudstorage.SyncRecordQuery{AppID: appID, UserID: user, DeviceID: "phone-a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records.Items {
		if record.DeviceID != "phone-a" || record.Deleted {
			t.Fatalf("按设备浏览只应列出 phone-a 写入的、未删除的记录：%+v", record)
		}
	}
	if len(overview.Collections) < len(cloudstorage.SyncCollections) {
		t.Fatalf("概览应列出目录里的全部集合")
	}

	// ── 接力 ──
	handoff, err := svc.CreateHandoff(ctx, appID, user, CloudHandoffInput{FromDeviceID: "tablet-b", ToDeviceID: "pc-c",
		Collection: "music.history", RecordID: "kw:2", Payload: json.RawMessage(`{"position":61000}`)})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.PendingHandoffs(ctx, appID, user, "pc-c")
	if err != nil || len(pending) != 1 || pending[0].ID != handoff.ID || pending[0].FromName != "Pad 7" {
		t.Fatalf("目标设备应收到接力：%+v %v", pending, err)
	}
	if err := svc.AckHandoff(ctx, appID, user, handoff.ID, "phone-a"); errCode(err) != errCodeCloudSyncHandoffNotFound {
		t.Fatalf("别的设备不能领取，应回 40435，得到 %v", err)
	}
	if err := svc.AckHandoff(ctx, appID, user, handoff.ID, "pc-c"); err != nil {
		t.Fatal(err)
	}
	if pending, _ = svc.PendingHandoffs(ctx, appID, user, "pc-c"); len(pending) != 0 {
		t.Fatalf("领取后不应再列出")
	}

	// ── 清空集合 ──
	before, _ := pg.GetCloudSyncState(ctx, appID, user)
	cleared, err := svc.ClearCollection(ctx, appID, user, "novel.bookshelf", "pc-c")
	if err != nil || cleared == 0 {
		t.Fatalf("清空书架：%d %v", cleared, err)
	}
	after, _ := pg.GetCloudSyncState(ctx, appID, user)
	if after.Seq != before.Seq+cleared {
		t.Fatalf("清空的每条墓碑都应占一个 seq：%d → %d（%d 条）", before.Seq, after.Seq, cleared)
	}

	// ── 墓碑清理推进地平线：旧游标 reset ──
	if _, err := pool.Exec(ctx, `UPDATE app_cloud_sync_records SET updated_at = NOW() - INTERVAL '100 days' WHERE deleted AND user_id = $1`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.purgeSync(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	page, err = svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: 1, Limit: 100}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !page.Reset {
		t.Fatalf("游标比墓碑地平线旧，应 reset")
	}
	for _, record := range page.Records {
		if record.Deleted {
			t.Fatalf("清理后的墓碑不应再出现：%+v", record)
		}
	}
	page, err = svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: after.Seq, Limit: 100}, "")
	if err != nil || page.Reset {
		t.Fatalf("最新游标不应 reset：%+v %v", page, err)
	}

	// ── 按档位裁剪历史：免费用户的历史只留 1 天，VIP 永久 ──
	freeUser := insertTestUser(t, ctx, pool, appID, "cloud-service-free", "0")
	if _, _, err := svc.RegisterDevice(ctx, appID, freeUser, CloudDeviceInput{DeviceInfo: cloudstorage.DeviceInfo{DeviceID: "free-1"}}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	payload := json.RawMessage(`{"title":"老歌"}`)
	if _, err := svc.Push(ctx, appID, freeUser, "free-1", []cloudstorage.SyncChange{
		{Collection: "music.history", ID: "old", Payload: payload, UpdatedAt: old},
		{Collection: "music.history", ID: "new", Payload: payload, UpdatedAt: time.Now()},
		{Collection: "music.favorites", ID: "fav", Payload: payload, UpdatedAt: old},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_cloud_sync_records SET client_updated_at = NOW() - INTERVAL '72 hours' WHERE user_id = $1 AND record_id = 'kw:2'`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.purgeSync(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if record, _ := pg.GetCloudSyncRecord(ctx, appID, freeUser, "music.history", "old"); record != nil {
		t.Fatalf("免费用户 3 天前的听歌记录应被裁掉")
	}
	if record, _ := pg.GetCloudSyncRecord(ctx, appID, freeUser, "music.favorites", "fav"); record == nil {
		t.Fatalf("收藏不是历史，不应被裁")
	}
	if record, _ := pg.GetCloudSyncRecord(ctx, appID, user, "music.history", "kw:2"); record == nil {
		t.Fatalf("VIP 用户的历史永久保留")
	}
	freeState, _ := pg.GetCloudSyncState(ctx, appID, freeUser)
	if freeState.SyncRecords != 2 {
		t.Fatalf("裁剪后账目应重算为 2 条，得到 %d", freeState.SyncRecords)
	}

	// ── 移除设备、清空云存储 ──
	if err := svc.RemoveDevice(ctx, appID, user, "tablet-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Push(ctx, appID, user, "tablet-b", []cloudstorage.SyncChange{change("music.history", "z", "z", 0)}); errCode(err) != errCodeCloudSyncDeviceNotFound {
		t.Fatalf("被移除的设备推送应回 40434，得到 %v", err)
	}
	if _, err := svc.AdminPurgeUser(ctx, appID, user); err != nil {
		t.Fatal(err)
	}
	page, err = svc.Changes(ctx, cloudstorage.SyncChangesQuery{AppID: appID, UserID: user, Since: after.Seq, Limit: 10}, "")
	if err != nil || !page.Reset || len(page.Records) != 0 {
		t.Fatalf("清空后旧游标应 reset 且没有记录：%+v %v", page, err)
	}
	devices, _ := svc.ListDevices(ctx, appID, user, "")
	if len(devices) != 0 {
		t.Fatalf("清空后设备列表应为空")
	}

	// ── 回收站按档位：VIP 30 天，免费 7 天 ──
	if _, err := svc.PutItem(ctx, CloudWriteInput{AppID: appID, UserID: freeUser, Namespace: "favorites", Key: "tools",
		Data: []byte(`{"a":1}`), Encoding: cloudstorage.EncodingJSON}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteItem(ctx, appID, freeUser, "favorites", "tools", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE app_cloud_items SET deleted_at = NOW() - INTERVAL '10 days' WHERE user_id = $1`, freeUser); err != nil {
		t.Fatal(err)
	}
	if purged, err := svc.PurgeExpired(ctx, appID); err != nil || purged == 0 {
		t.Fatalf("免费档 7 天回收站，10 天前删的应被清除：%d %v", purged, err)
	}
}
