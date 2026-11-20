package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
)

// 站内信群发任务的端到端集成测试：真实 Postgres（全部迁移两遍）+ miniredis + 假的实时发布器。
// 「人群预览（全站 / 会员 / 平台 / 沉默 / 关键词 / 排除 / 上限）→ 一键全站跨批发完、一人一条、缓存失效、
// created 事件 → requestId 幂等 → 中断后接手（旧持有者被拒、不重不漏）→ 重放同一批只跳过 → 人数上限 →
// 定时任务不提前认领、取消与重复取消 → 撤回（删光、计数归零、deleted 事件）→ 旧群发接口：少量就地发完、
// 大量转后台任务 → 管理端按任务筛选」。
//
// AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run TestNotificationCampaignIntegration
//
// 注意：openTestDatabase 会 DROP SCHEMA public，DSN 绝不能指向开发库。
func TestNotificationCampaignIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	rt := &fakeRealtime{}
	svc := NewNotificationService(zap.NewNop(), pg, sessions, rt)

	const appID = int64(34000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'campaign-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	// 2300 个用户：超过一批（notificationCampaignBatch），也超过旧接口就地发送的上限（2000）
	rows, err := pool.Query(ctx, `INSERT INTO users (appid, account, enabled)
SELECT $1, 'user' || g, TRUE FROM generate_series(1, 2300) g RETURNING id`, appID)
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, id)
	}
	rows.Close()
	if len(userIDs) != 2300 {
		t.Fatalf("建用户失败：%d", len(userIDs))
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s：%v", sql, err)
		}
	}
	// 前 10 个禁用；11–60 会员（其中 11–20 永久）；61–90 有安卓设备、最近活跃；91–100 iOS 设备、40 天前活跃
	exec(`UPDATE users SET enabled = FALSE WHERE id = ANY($1)`, userIDs[0:10])
	exec(`UPDATE users SET vip_expire_at = NOW() + INTERVAL '30 days' WHERE id = ANY($1)`, userIDs[10:60])
	exec(`UPDATE users SET vip_lifetime_at = NOW() WHERE id = ANY($1)`, userIDs[10:20])
	exec(`INSERT INTO user_login_devices (appid, user_id, device_key, platform, last_seen_at)
SELECT $1, u, 'dev-' || u, 'android', NOW() FROM unnest($2::bigint[]) u`, appID, userIDs[60:90])
	exec(`INSERT INTO user_login_devices (appid, user_id, device_key, platform, last_seen_at)
SELECT $1, u, 'dev-' || u, 'ios', NOW() - INTERVAL '40 days' FROM unnest($2::bigint[]) u`, appID, userIDs[90:100])
	exec(`UPDATE users SET account = 'special-' || id WHERE id = ANY($1)`, userIDs[100:103])

	drainEvents := func() []fakeRealtimeEvent {
		svc.waitPendingEvents()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		events := rt.events
		rt.events = nil
		return events
	}
	countEvents := func(events []fakeRealtimeEvent, eventType string) int {
		n := 0
		for _, event := range events {
			if event.Type == eventType {
				n++
			}
		}
		return n
	}
	// runOnce 本实例认领一轮并等认领到的任务全部跑完
	runOnce := func() {
		t.Helper()
		svc.claimCampaigns(ctx)
		svc.campaigns.wg.Wait()
	}
	get := func(id int64) *notificationdomain.Campaign {
		t.Helper()
		campaign, err := svc.GetCampaign(ctx, appID, id)
		if err != nil {
			t.Fatal(err)
		}
		return campaign
	}
	campaignRows := func(id int64) (int64, int64) {
		t.Helper()
		var total, distinct int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT user_id) FROM notifications WHERE campaign_id = $1`, id).
			Scan(&total, &distinct); err != nil {
			t.Fatal(err)
		}
		var receipts int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_receipts nr JOIN notifications n ON n.id = nr.notification_id
WHERE n.campaign_id = $1`, id).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if receipts != total {
			t.Fatalf("任务 %d：消息 %d 条，回执 %d 条", id, total, receipts)
		}
		return total, distinct
	}

	// ── 人群预览 ──
	preview := func(name string, audience notificationdomain.Audience, want int64) {
		t.Helper()
		result, err := svc.PreviewAudience(ctx, appID, audience)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if result.Total != want {
			t.Fatalf("%s：人数 %d，期望 %d（%s）", name, result.Total, want, result.Label)
		}
		if want > 0 && (len(result.Samples) == 0 || len(result.Samples) > notificationAudienceSamples) {
			t.Fatalf("%s：抽样 %d 个", name, len(result.Samples))
		}
	}
	preview("全站可用账号", notificationdomain.Audience{Scope: notificationdomain.AudienceAll}, 2290)
	preview("全站含禁用", notificationdomain.Audience{Scope: notificationdomain.AudienceAll, Account: notificationdomain.AccountAll}, 2300)
	preview("仅禁用", notificationdomain.Audience{Scope: notificationdomain.AudienceAll, Account: notificationdomain.AccountDisabled}, 10)
	preview("会员", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Membership: notificationdomain.MembershipMember}, 50)
	preview("永久会员", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Membership: notificationdomain.MembershipLifetime}, 10)
	preview("非会员", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Membership: notificationdomain.MembershipNonMember}, 2240)
	preview("安卓", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Platforms: []string{"Android"}}, 30)
	preview("7 天内活跃", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, ActiveWithinDays: 7}, 30)
	preview("30 天未活跃", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, InactiveDays: 30}, 2260)
	preview("关键词", notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Keyword: "special-"}, 3)
	preview("排除与上限", notificationdomain.Audience{Scope: notificationdomain.AudienceAll, ExcludeUserIDs: userIDs[20:30], Limit: 100}, 100)
	preview("指定用户（含禁用的不算）", notificationdomain.Audience{Scope: notificationdomain.AudienceUsers, UserIDs: userIDs[5:15]}, 5)
	if _, err := svc.PreviewAudience(ctx, appID, notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, MinLevel: 5, MaxLevel: 1}); errCode(err) != 40010 {
		t.Fatalf("非法人群应报 40010：%v", err)
	}
	if catalog := svc.CampaignCatalog(); len(catalog.Presets) == 0 {
		t.Fatal("目录没有预设人群")
	}

	// 发之前先把一个人的列表缓存起来：群发写入后必须失效
	probe := &authdomain.Session{AppID: appID, UserID: userIDs[500]}
	if before, err := svc.List(ctx, probe, notificationdomain.UserListQuery{Page: 1, Limit: 20}); err != nil || before.Total != 0 {
		t.Fatalf("发之前应没有消息：%+v %v", before, err)
	}

	// ── 一键全站：跨三批发完，一人一条 ──
	expires := time.Now().Add(48 * time.Hour)
	all, created, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, RequestID: "req-all", Title: "全站公告", Content: "系统将于今晚维护",
		Action: &notificationdomain.Action{Type: "route", Target: "points"}, ExpiresAt: &expires,
		Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceAll}, Operator: "tester",
	})
	if err != nil || !created || all.Status != notificationdomain.CampaignQueued || all.Category != notificationdomain.CategoryAnnouncement ||
		all.Type != notificationdomain.TypeAnnouncement || all.AudienceLabel != "全站用户" {
		t.Fatalf("发起全站群发：%+v %v", all, err)
	}
	again, created, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, RequestID: "req-all", Title: "全站公告", Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceAll},
	})
	if err != nil || created || again.ID != all.ID {
		t.Fatalf("同一 requestId 应返回原任务：%+v created=%v %v", again, created, err)
	}
	runOnce()
	done := get(all.ID)
	if done.Status != notificationdomain.CampaignSucceeded || done.Delivered != 2290 || done.Processed != 2290 || done.Skipped != 0 ||
		done.Total == nil || *done.Total != 2290 || done.ReadCount == nil || *done.ReadCount != 0 || done.FinishedAt == nil {
		t.Fatalf("全站群发结果不对：%+v", done)
	}
	if total, distinct := campaignRows(all.ID); total != 2290 || distinct != 2290 {
		t.Fatalf("全站群发写入 %d 条（%d 人）", total, distinct)
	}
	after, err := svc.List(ctx, probe, notificationdomain.UserListQuery{Page: 1, Limit: 20})
	if err != nil || after.Total != 1 || after.Unread != 1 || after.Items[0].Title != "全站公告" || after.Items[0].Action == nil {
		t.Fatalf("群发后列表缓存没失效或内容不对：%+v %v", after, err)
	}
	events := drainEvents()
	if n := countEvents(events, notificationdomain.EventCreated); n != 2290 {
		t.Fatalf("应给每个收件人推 created：%d", n)
	}
	for _, event := range events {
		if event.Type == notificationdomain.EventCreated {
			item := event.Data["notification"].(notificationdomain.Item)
			if item.UserID == nil || *item.UserID != event.UserID || item.Title != "全站公告" || event.Data["unread"] != int64(1) {
				t.Fatalf("created 事件内容不对：%+v", event)
			}
			break
		}
	}
	// 读一条，详情的已读人数跟着变；管理端按任务筛选
	if _, err := svc.MarkRead(ctx, probe, after.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if read := get(all.ID).ReadCount; read == nil || *read != 1 {
		t.Fatalf("已读人数：%v", read)
	}
	if list, err := svc.AdminList(ctx, appID, notificationdomain.AdminListQuery{CampaignID: all.ID, Status: "read"}); err != nil ||
		list.Total != 1 || list.Items[0].CampaignID == nil || *list.Items[0].CampaignID != all.ID {
		t.Fatalf("管理端按任务与已读筛选：%+v %v", list, err)
	}
	if list, err := svc.AdminList(ctx, appID, notificationdomain.AdminListQuery{CampaignID: all.ID, Status: "unread", Limit: 1}); err != nil || list.Total != 2289 {
		t.Fatalf("管理端未读筛选：%+v %v", list, err)
	}

	// ── 中断后接手：旧持有者写完一批就死了，心跳过期后别的实例接着发，不重不漏 ──
	members, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, Title: "非会员专享", Category: notificationdomain.CategoryMembership,
		Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Membership: notificationdomain.MembershipNonMember},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := pg.ClaimNotificationCampaigns(ctx, "dead-node", time.Now().Add(-time.Hour), notificationCampaignMaxAttempts, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != members.ID {
		t.Fatalf("模拟别的实例认领：%+v %v", claimed, err)
	}
	begun, err := pg.BeginNotificationCampaign(ctx, claimed[0], "dead-node")
	if err != nil {
		t.Fatal(err)
	}
	first, err := pg.DeliverNotificationCampaignBatch(ctx, *begun, "dead-node", notificationCampaignBatch)
	if err != nil || len(first.Inserted) != notificationCampaignBatch || first.Status != notificationdomain.CampaignRunning {
		t.Fatalf("第一批：%+v %v", first, err)
	}
	// 还没过期：本实例认领不到
	runOnce()
	if status := get(members.ID).Status; status != notificationdomain.CampaignRunning {
		t.Fatalf("心跳未过期不该被接手：%s", status)
	}
	exec(`UPDATE notification_campaigns SET heartbeat_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, members.ID)
	runOnce()
	resumed := get(members.ID)
	if resumed.Status != notificationdomain.CampaignSucceeded || resumed.Delivered != 2240 || resumed.Attempts != 2 {
		t.Fatalf("接手后应发完：%+v", resumed)
	}
	if total, distinct := campaignRows(members.ID); total != 2240 || distinct != 2240 {
		t.Fatalf("接手后写入 %d 条（%d 人），有重复或遗漏", total, distinct)
	}
	if stale, err := pg.DeliverNotificationCampaignBatch(ctx, *begun, "dead-node", notificationCampaignBatch); err != nil || stale.Status != "" || len(stale.Inserted) != 0 {
		t.Fatalf("旧持有者回来后不该再写：%+v %v", stale, err)
	}
	// 重放同一批（游标退回 0）：唯一索引兜底，只计跳过
	exec(`UPDATE notification_campaigns SET status = 'running', owner = 'replay', cursor = 0, processed = 0 WHERE id = $1`, members.ID)
	replay, err := pg.DeliverNotificationCampaignBatch(ctx, *begun, "replay", notificationCampaignBatch)
	if err != nil || replay.Scanned != notificationCampaignBatch || len(replay.Inserted) != 0 {
		t.Fatalf("重放应全部跳过：%+v %v", replay, err)
	}
	exec(`UPDATE notification_campaigns SET status = 'succeeded', owner = '' WHERE id = $1`, members.ID)
	drainEvents()

	// ── 人数上限 ──
	limited, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, Title: "前五名", Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceAll, Limit: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	runOnce()
	if got := get(limited.ID); got.Status != notificationdomain.CampaignSucceeded || got.Delivered != 5 || *got.Total != 5 {
		t.Fatalf("人数上限：%+v", got)
	}

	// ── 定时：不到点不认领；取消；重复取消 40958 ──
	later := time.Now().Add(2 * time.Hour)
	scheduled, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, Title: "明天见", ScheduledAt: &later, Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceAll},
	})
	if err != nil || scheduled.Status != notificationdomain.CampaignScheduled {
		t.Fatalf("定时群发：%+v %v", scheduled, err)
	}
	runOnce()
	if status := get(scheduled.ID).Status; status != notificationdomain.CampaignScheduled {
		t.Fatalf("没到点不该开始：%s", status)
	}
	if cancelled, err := svc.CancelCampaign(ctx, appID, scheduled.ID, "tester"); err != nil || cancelled.Status != notificationdomain.CampaignCancelled {
		t.Fatalf("取消：%+v %v", cancelled, err)
	}
	if _, err := svc.CancelCampaign(ctx, appID, scheduled.ID, "tester"); errCode(err) != errCodeNotificationCampaignState {
		t.Fatalf("重复取消应报 40958：%v", err)
	}
	if _, err := svc.GetCampaign(ctx, appID, 999999); errCode(err) != errCodeNotificationCampaignNotFound {
		t.Fatalf("不存在的任务应报 40427：%v", err)
	}
	past := time.Now().Add(-time.Hour)
	if _, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{AppID: appID, Title: "x", ScheduledAt: &past}); errCode(err) != 40010 {
		t.Fatalf("定时在过去应报 40010：%v", err)
	}
	if _, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{AppID: appID, Title: "x",
		Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceFilter, Keyword: "nobody-matches"}}); errCode(err) != 40401 {
		t.Fatalf("没人可发应报 40401：%v", err)
	}

	// ── 撤回：删光、计数归零、推 deleted ──
	recalling, err := svc.RecallCampaign(ctx, appID, all.ID, "tester")
	if err != nil || recalling.Status != notificationdomain.CampaignRecalling {
		t.Fatalf("撤回：%+v %v", recalling, err)
	}
	runOnce()
	recalled := get(all.ID)
	if recalled.Status != notificationdomain.CampaignRecalled || recalled.Recalled != 2290 || recalled.RecalledAt == nil {
		t.Fatalf("撤回结果：%+v", recalled)
	}
	if total, _ := campaignRows(all.ID); total != 0 {
		t.Fatalf("撤回后还剩 %d 条", total)
	}
	if counts, err := svc.UnreadCount(ctx, probe); err != nil || counts.Categories[notificationdomain.CategoryAnnouncement] != 0 {
		t.Fatalf("撤回后公告未读应归零：%+v %v", counts, err)
	}
	if n := countEvents(drainEvents(), notificationdomain.EventDeleted); n != 2290 {
		t.Fatalf("撤回应给每个收件人推 deleted：%d", n)
	}
	if _, err := svc.RecallCampaign(ctx, appID, all.ID, "tester"); errCode(err) != errCodeNotificationCampaignState {
		t.Fatalf("重复撤回应报 40958：%v", err)
	}

	// ── 旧群发接口：少量就地发完并返回收件人；大量转后台任务 ──
	small, err := svc.AdminBulkSend(ctx, appID, notificationdomain.AdminBulkSendCommand{
		UserIDs: userIDs[200:203], Type: "system", Title: "就地发", Content: "三个人",
	})
	if err != nil || small.Async || small.Delivered != 3 || len(small.RecipientIDs) != 3 || small.Campaign == nil ||
		small.Campaign.Status != notificationdomain.CampaignSucceeded {
		t.Fatalf("旧接口少量：%+v %v", small, err)
	}
	if n := countEvents(drainEvents(), notificationdomain.EventCreated); n != 3 {
		t.Fatalf("旧接口应推 3 个 created：%d", n)
	}
	big, err := svc.AdminBulkSend(ctx, appID, notificationdomain.AdminBulkSendCommand{
		UserIDs: userIDs, Type: "system", Title: "转后台", Content: "两千三百人",
	})
	if err != nil || !big.Async || big.Campaign == nil || big.Requested != 2300 {
		t.Fatalf("旧接口大量应转后台：%+v %v", big, err)
	}
	runOnce()
	if got := get(big.Campaign.ID); got.Status != notificationdomain.CampaignSucceeded || got.Delivered != 2300 {
		t.Fatalf("旧接口后台任务（指定用户不论账号状态）：%+v", got)
	}

	// 任务列表与筛选
	page, err := svc.ListCampaigns(ctx, notificationdomain.CampaignQuery{AppID: appID, Status: notificationdomain.CampaignSucceeded})
	if err != nil || page.Total != 4 {
		t.Fatalf("任务列表按状态筛选：%+v %v", page, err)
	}
	if _, err := svc.ListCampaigns(ctx, notificationdomain.CampaignQuery{AppID: appID, Status: "nope"}); errCode(err) != 40010 {
		t.Fatalf("未知状态应报 40010：%v", err)
	}

	// 实例关闭：手上的任务放回去，不必等心跳过期
	release, _, err := svc.CreateCampaign(ctx, notificationdomain.CampaignInput{
		AppID: appID, Title: "关机前", Audience: notificationdomain.Audience{Scope: notificationdomain.AudienceAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	stopped, cancel := context.WithCancel(ctx)
	cancel()
	held, err := pg.ClaimNotificationCampaigns(ctx, svc.campaignOwner(), time.Now().Add(-time.Hour), notificationCampaignMaxAttempts, 1)
	if err != nil || len(held) != 1 || held[0].ID != release.ID {
		t.Fatalf("认领：%+v %v", held, err)
	}
	svc.runCampaign(stopped, held[0])
	if got := get(release.ID); got.Status != notificationdomain.CampaignRunning || got.Attempts != 0 || got.HeartbeatAt != nil {
		t.Fatalf("关机应放回任务：%+v", got)
	}
	runOnce()
	if got := get(release.ID); got.Status != notificationdomain.CampaignSucceeded || got.Delivered != 2290 {
		t.Fatalf("放回的任务应被重新认领发完：%+v", got)
	}
}
