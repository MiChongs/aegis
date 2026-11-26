package service

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	ticketdomain "aegis/internal/domain/ticket"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 有效反馈奖励积分（000173）：策略默认关闭、按分类预填、单条 / 每日 / 每月上限、
// 一张反馈只发一次（重复点击与并发都只落一笔）、积分流水 + 时间线 + 站内信、撤回扣回（整笔 / 扣到 0）、
// 改状态时同时发放、用户侧详情与列表带奖励。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/aegis_fbreward_test go test ./internal/service -run TestFeedbackRewardIntegration
func TestFeedbackRewardIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	const appID = int64(32300)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'feedback-reward-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	notifications := NewNotificationService(zap.NewNop(), pg, nil, nil)
	svc := NewTicketService(zap.NewNop(), pg, nil, nil, nil)
	svc.SetInboxes(notifications, NewAdminInboxService(zap.NewNop(), pg, nil))
	svc.SetUserNotifier(notifications)

	userID := insertTestUser(t, ctx, pool, appID, "fr-user", "0")
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	session := &authdomain.Session{UserID: userID, AppID: appID, Account: "fr-user"}
	newAdmin := func(account string, super bool) *admindomain.AccessContext {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ($1, 'x', $1, $2) RETURNING id`, account, super).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return &admindomain.AccessContext{Session: admindomain.Session{AdminID: id, Account: account, DisplayName: account, IsSuperAdmin: super}}
	}
	boss := newAdmin("fr-boss", true)
	agent := newAdmin("fr-agent", false)

	categoryID := func(key string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM ticket_categories WHERE appid = 0 AND key = $1`, key).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	bugCategory, featureCategory := categoryID("feedback_bug"), categoryID("feedback_feature")
	seq := 0
	newFeedback := func(category int64, status string, requesterUser bool) int64 {
		seq++
		var id int64
		requesterType, requester, requesterAdmin := "user", any(userID), any(nil)
		if !requesterUser {
			// 管理员代登记的反馈：提单人不是应用用户
			requesterType, requester, requesterAdmin = "admin", nil, boss.AdminID
		}
		if err := pool.QueryRow(ctx, `INSERT INTO tickets (kind, ticket_no, appid, requester_type, requester_user_id, requester_admin_id,
requester_name, title, source, status, category_id, assignee_admin_id)
VALUES ('feedback', $1, $2, $3, $4, $8, '用户', '反馈', 'app', $5, $6, $7) RETURNING id`,
			fmt.Sprintf("FR-%d", seq), appID, requesterType, requester, status, category, agent.AdminID, requesterAdmin).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	balance := func() int64 {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	count := func(sql string, args ...any) int {
		var value int
		if err := pool.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	first := newFeedback(bugCategory, ticketdomain.StatusProcessing, true)

	// ── 默认关闭：老行为不变 ──
	policy, err := svc.FeedbackRewardPolicy(ctx, boss, appID)
	if err != nil || policy.Enabled || policy.Configured {
		t.Fatalf("没有配置时应返回未开启的默认值：%+v %v", policy, err)
	}
	if _, err := svc.GrantFeedbackReward(ctx, boss, first, ticketdomain.RewardGrantCommand{Points: 10}); errCode(err) != errCodeFeedbackRewardDisabled {
		t.Fatalf("未开启时不能发，得到 %v", err)
	}
	detail, err := svc.Detail(ctx, boss, first, "")
	if err != nil || detail.Reward == nil || detail.Reward.Enabled || !detail.Permissions.Reward {
		t.Fatalf("详情应带奖励区块（未开启）且超管可发：%+v %v", detail.Reward, err)
	}

	// ── 策略 ──
	if _, err := svc.SaveFeedbackRewardPolicy(ctx, boss, ticketdomain.RewardPolicy{AppID: appID, Enabled: true, DefaultPoints: 200, MaxPoints: 100}); errCode(err) != 40000 {
		t.Fatalf("默认值超过单条上限应拒绝，得到 %v", err)
	}
	if _, err := svc.SaveFeedbackRewardPolicy(ctx, agent, ticketdomain.RewardPolicy{AppID: appID, Enabled: true}); errCode(err) != 40311 {
		t.Fatalf("没有工单配置权限不能改策略，得到 %v", err)
	}
	saved, err := svc.SaveFeedbackRewardPolicy(ctx, boss, ticketdomain.RewardPolicy{
		AppID: appID, Enabled: true, DefaultPoints: 20, MaxPoints: 100, UserDailyLimit: 120, UserMonthlyLimit: 200,
		CategoryPoints: map[string]int64{"feedback_bug": 50, "feedback_feature": 30},
	})
	if err != nil || !saved.Configured || saved.UpdatedByName != "fr-boss" {
		t.Fatalf("保存策略：%+v %v", saved, err)
	}
	detail, _ = svc.Detail(ctx, boss, first, "")
	if detail.Reward.SuggestedPoints != 50 || detail.Reward.MaxGrantable == nil || *detail.Reward.MaxGrantable != 100 {
		t.Fatalf("按分类预填 50、本次最多 100：%+v", detail.Reward)
	}

	// ── 权限：受理人能处理反馈，但没有积分调整权限 ──
	agentDetail, err := svc.Detail(ctx, agent, first, "")
	if err != nil || agentDetail.Permissions.Reward || !agentDetail.Permissions.ChangeStatus {
		t.Fatalf("受理人能改状态、不能发奖励：%+v %v", agentDetail.Permissions, err)
	}
	if _, err := svc.GrantFeedbackReward(ctx, agent, first, ticketdomain.RewardGrantCommand{Points: 10}); errCode(err) != 40311 {
		t.Fatalf("没有积分调整权限不能发，得到 %v", err)
	}

	// ── 发放：points 省略按建议值 ──
	result, err := svc.GrantFeedbackReward(ctx, boss, first, ticketdomain.RewardGrantCommand{Reason: "定位到闪退原因", ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Reward.Points != 50 || result.BalanceBefore != 100 || result.BalanceAfter != 150 || !result.Notified {
		t.Fatalf("发放结果不对：%+v", result)
	}
	if balance() != 150 {
		t.Fatalf("余额应为 150，得到 %d", balance())
	}
	if n := count(`SELECT COUNT(*) FROM integral_transactions WHERE user_id = $1 AND category = 'feedback_reward'
AND source_type = 'feedback_reward' AND source_id = $2 AND amount = 50 AND description LIKE '%FR-1%'`, userID, first); n != 1 {
		t.Fatalf("应有一笔标明反馈单号的积分流水，得到 %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM ticket_events WHERE ticket_id = $1 AND event = 'reward_granted'`, first); n != 1 {
		t.Fatalf("时间线应记一条发放，得到 %d", n)
	}
	var notificationType, notificationCategory, notificationContent string
	if err := pool.QueryRow(ctx, `SELECT type, category, content FROM notifications WHERE user_id = $1 AND type = 'feedback_reward' ORDER BY id DESC LIMIT 1`,
		userID).Scan(&notificationType, &notificationCategory, &notificationContent); err != nil {
		t.Fatal(err)
	}
	if notificationCategory != "reward" || !strings.Contains(notificationContent, "#FR-1") || !strings.Contains(notificationContent, "50 积分") {
		t.Fatalf("站内信不对：%s / %s / %s", notificationType, notificationCategory, notificationContent)
	}
	if n := count(`SELECT COUNT(*) FROM feedback_rewards WHERE ticket_id = $1 AND notification_id IS NOT NULL`, first); n != 1 {
		t.Fatalf("奖励记录应记下站内信 ID，得到 %d", n)
	}

	// ── 幂等：重复点同样的数额原样返回；不同数额报已发过 ──
	again, err := svc.GrantFeedbackReward(ctx, boss, first, ticketdomain.RewardGrantCommand{Points: 50})
	if err != nil || !again.Replayed || again.Reward.ID != result.Reward.ID {
		t.Fatalf("重复发放应原样返回：%+v %v", again, err)
	}
	if _, err := svc.GrantFeedbackReward(ctx, boss, first, ticketdomain.RewardGrantCommand{Points: 60}); errCode(err) != errCodeFeedbackRewardExists {
		t.Fatalf("已发过不同数额应回 %d，得到 %v", errCodeFeedbackRewardExists, err)
	}
	if balance() != 150 {
		t.Fatalf("重复提交不能多发，余额 %d", balance())
	}

	// ── 并发：同一张反馈同时点十次只落一笔 ──
	second := newFeedback(featureCategory, ticketdomain.StatusResolved, true)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fresh, failures := 0, 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.GrantFeedbackReward(ctx, boss, second, ticketdomain.RewardGrantCommand{Points: 30, RequestID: fmt.Sprintf("req-%d", i)})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				return
			}
			if !res.Replayed {
				fresh++
			}
		}(i)
	}
	wg.Wait()
	if fresh != 1 || failures != 0 || count(`SELECT COUNT(*) FROM feedback_rewards WHERE ticket_id = $1`, second) != 1 || balance() != 180 {
		t.Fatalf("并发发放应只落一笔：新发 %d、失败 %d、余额 %d", fresh, failures, balance())
	}

	// ── 每日上限：已领 80，再发 50 超过 120 ──
	third := newFeedback(bugCategory, ticketdomain.StatusProcessing, true)
	if _, err := svc.GrantFeedbackReward(ctx, boss, third, ticketdomain.RewardGrantCommand{Points: 50}); errCode(err) != errCodeFeedbackRewardLimit {
		t.Fatalf("超过每日上限应回 %d，得到 %v", errCodeFeedbackRewardLimit, err)
	}
	if _, err := svc.GrantFeedbackReward(ctx, boss, third, ticketdomain.RewardGrantCommand{Points: 101}); errCode(err) != errCodeFeedbackRewardLimit {
		t.Fatalf("超过单条上限应回 %d，得到 %v", errCodeFeedbackRewardLimit, err)
	}

	// ── 不能发的单子 ──
	if _, err := svc.GrantFeedbackReward(ctx, boss, newFeedback(bugCategory, ticketdomain.StatusRejected, true), ticketdomain.RewardGrantCommand{Points: 5}); errCode(err) != errCodeFeedbackRewardIneligible {
		t.Fatalf("不采纳的反馈不能发，得到 %v", err)
	}
	if _, err := svc.GrantFeedbackReward(ctx, boss, newFeedback(bugCategory, ticketdomain.StatusProcessing, false), ticketdomain.RewardGrantCommand{Points: 5}); errCode(err) != errCodeFeedbackRewardIneligible {
		t.Fatalf("提单人不是用户的不能发，得到 %v", err)
	}

	// ── 用户侧：详情与列表带奖励 ──
	mine, err := svc.FeedbackDetail(ctx, session, first)
	if err != nil || mine.Reward == nil || mine.Reward.Points != 50 || mine.Reward.Reason != "定位到闪退原因" {
		t.Fatalf("用户详情应带奖励：%+v %v", mine.Reward, err)
	}
	list, err := svc.ListFeedback(ctx, session, nil, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	rewarded := 0
	for _, item := range list.Items {
		if item.Reward != nil {
			rewarded++
		}
	}
	if rewarded != 2 {
		t.Fatalf("列表里应有两条带奖励，得到 %d", rewarded)
	}

	// ── 撤回：余额不足默认整笔拒绝 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 10 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeFeedbackReward(ctx, agent, first, ticketdomain.RewardRevokeCommand{}); errCode(err) != 40311 {
		t.Fatalf("没有积分调整权限不能撤回，得到 %v", err)
	}
	if _, err := svc.RevokeFeedbackReward(ctx, boss, first, ticketdomain.RewardRevokeCommand{Reason: "重复反馈"}); errCode(err) != 40009 {
		t.Fatalf("余额不足应整笔拒绝（40009），得到 %v", err)
	}
	if balance() != 10 || count(`SELECT COUNT(*) FROM feedback_rewards WHERE ticket_id = $1 AND status = 'granted'`, first) != 1 {
		t.Fatal("整笔拒绝时什么都不能变")
	}

	// ── 撤回：扣到 0，差额记下，原通知删掉、另发撤回通知 ──
	revoked, err := svc.RevokeFeedbackReward(ctx, boss, first, ticketdomain.RewardRevokeCommand{Reason: "重复反馈", AllowPartial: true, RequestID: "rv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.Reward.Status != ticketdomain.RewardRevoked || revoked.Reward.ClawedBack != 10 || revoked.Reward.Shortfall != 40 || balance() != 0 {
		t.Fatalf("扣到 0：%+v 余额 %d", revoked.Reward, balance())
	}
	if n := count(`SELECT COUNT(*) FROM integral_transactions WHERE user_id = $1 AND category = 'feedback_reward_revoke' AND amount = -10`, userID); n != 1 {
		t.Fatalf("应有一笔撤回扣减流水，得到 %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM ticket_events WHERE ticket_id = $1 AND event = 'reward_revoked'`, first); n != 1 {
		t.Fatalf("时间线应记一条撤回，得到 %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = 'feedback_reward' AND title = '反馈奖励到账' AND content LIKE '%FR-1%'`, userID); n != 0 {
		t.Fatalf("原来的到账通知应删掉，还剩 %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND title = '反馈奖励已撤回'`, userID); n != 1 {
		t.Fatalf("应发一条撤回通知，得到 %d", n)
	}
	replay, err := svc.RevokeFeedbackReward(ctx, boss, first, ticketdomain.RewardRevokeCommand{AllowPartial: true, RequestID: "rv-1"})
	if err != nil || !replay.Replayed {
		t.Fatalf("同一编号重复撤回原样返回：%+v %v", replay, err)
	}
	if _, err := svc.RevokeFeedbackReward(ctx, boss, first, ticketdomain.RewardRevokeCommand{}); errCode(err) != errCodeFeedbackRewardNotFound {
		t.Fatalf("没有有效奖励时应回 %d，得到 %v", errCodeFeedbackRewardNotFound, err)
	}
	if mine, _ := svc.FeedbackDetail(ctx, session, first); mine.Reward != nil {
		t.Fatal("撤回后用户侧不再显示奖励")
	}

	// ── 撤回之后可以按正确数额重新发 ──
	if res, err := svc.GrantFeedbackReward(ctx, boss, first, ticketdomain.RewardGrantCommand{Points: 20}); err != nil || res.Replayed {
		t.Fatalf("撤回后可重新发放：%+v %v", res, err)
	}
	detail, _ = svc.Detail(ctx, boss, first, "")
	if detail.Reward.Active == nil || detail.Reward.Active.Points != 20 || len(detail.Reward.History) != 2 {
		t.Fatalf("历史应有两条（一撤回一有效）：%+v", detail.Reward)
	}

	// ── 改状态时同时发放 ──
	fourth := newFeedback(bugCategory, ticketdomain.StatusProcessing, true)
	if _, err := svc.ChangeStatus(ctx, boss, fourth, ticketdomain.StatusCommand{Status: ticketdomain.StatusRejected,
		Reward: &ticketdomain.RewardGrantCommand{Points: 5}}); errCode(err) != errCodeFeedbackRewardIneligible {
		t.Fatalf("改为不采纳时不能同时发放，得到 %v", err)
	}
	if item, _ := pg.GetTicketByID(ctx, fourth); item.Status != ticketdomain.StatusProcessing {
		t.Fatalf("奖励失败时状态不变，得到 %s", item.Status)
	}
	// 今天已领 20 + 30，单条 100，日上限 120：再发 60 可以
	changed, err := svc.ChangeStatus(ctx, boss, fourth, ticketdomain.StatusCommand{Status: ticketdomain.StatusResolved,
		Solution: "已修复", Reward: &ticketdomain.RewardGrantCommand{Points: 60, Reason: "感谢反馈"}})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Status != ticketdomain.StatusResolved || changed.Reward == nil || changed.Reward.Active == nil || changed.Reward.Active.Points != 60 {
		t.Fatalf("改为已解决并发放 60：%s %+v", changed.Status, changed.Reward)
	}
	// 已领 110，再改状态时发 20 会超过日上限：整体失败、状态不变
	fifth := newFeedback(bugCategory, ticketdomain.StatusProcessing, true)
	if _, err := svc.ChangeStatus(ctx, boss, fifth, ticketdomain.StatusCommand{Status: ticketdomain.StatusResolved,
		Reward: &ticketdomain.RewardGrantCommand{Points: 20}}); errCode(err) != errCodeFeedbackRewardLimit {
		t.Fatalf("超过上限应回 %d，得到 %v", errCodeFeedbackRewardLimit, err)
	}
	if item, _ := pg.GetTicketByID(ctx, fifth); item.Status != ticketdomain.StatusProcessing {
		t.Fatalf("奖励失败时状态不变，得到 %s", item.Status)
	}
}
