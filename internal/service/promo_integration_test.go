package service

import (
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/promo"
	pgrepo "aegis/internal/repository/postgres"
)

// 推广活动的端到端集成测试：真实 Postgres，走一遍
// 「配置校验 → 上线 → 邀请人数按活动时间段计 → 领取档位（重复 / 未达到 / 并发只成一次）→ 未开始的活动 →
// 提交文章（抖音禁止 / 平台不符 / 未启用平台 / 重复 / 平台篇数 / 每人篇数）→ 审核（驳回要原因、驳回后可重交、
// 通过发奖、重复审核）→ 排行 → 统计 → 删除限制」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55434/postgres go test ./internal/service -run TestPromoIntegration
func TestPromoIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	inviteService := NewInviteService(zap.NewNop(), pg)
	svc := NewPromoService(zap.NewNop(), pg, inviteService)
	const appID = int64(31100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'promo-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	user := func(account string) int64 {
		t.Helper()
		id := insertTestUser(t, ctx, pool, appID, account, "0")
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname) VALUES ($1, $2)`, id, account); err != nil {
			t.Fatal(err)
		}
		return id
	}
	integral := func(userID int64) int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	points := func(amount int64) []promo.Reward { return []promo.Reward{{Type: invite.RewardPoints, Amount: amount}} }

	// 邀请：填写即算成功邀请
	cfg := invite.DefaultConfig()
	cfg.InviterRewards = []invite.Reward{{Type: invite.RewardPoints, Amount: 1}}
	if _, err := inviteService.SaveSettings(ctx, appID, true, cfg, "admin"); err != nil {
		t.Fatal(err)
	}
	alice, bob, carol, dave, erin := user("alice"), user("bob"), user("carol"), user("dave"), user("erin")
	center, err := inviteService.Center(ctx, appID, alice)
	if err != nil || center.Code == "" {
		t.Fatalf("取 alice 的邀请码失败：%v %+v", err, center)
	}

	// ── 配置校验 ──
	now := time.Now()
	input := promo.CampaignInput{
		AppID: appID, Title: "推广季", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(7 * 24 * time.Hour), Operator: "admin",
		Config: promo.DefaultConfig(),
	}
	if _, err := svc.CreateCampaign(ctx, input); errCode(err) != 40000 {
		t.Fatalf("没有档位与分享奖励应当保存失败，得到 %v", err)
	}
	input.Config.Invite.Tiers = []promo.Tier{{Count: 2, Rewards: points(200)}, {Count: 1, Title: "首邀", Rewards: points(100)}}
	input.Config.Share.Rewards = points(50)
	input.Config.Share.MaxApprovedPerUser = 2
	bad := input
	bad.Config.Invite.Tiers = []promo.Tier{{Count: 1, Rewards: []promo.Reward{{Type: "vip_plan", RefID: 99999}}}}
	if _, err := svc.CreateCampaign(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("引用不存在的套餐应当保存失败，得到 %v", err)
	}
	campaign, err := svc.CreateCampaign(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if campaign.Status != promo.StatusDraft || campaign.Config.Invite.Tiers[0].Count != 1 {
		t.Fatalf("新建应为草稿、档位升序，得到 %+v", campaign)
	}
	// 草稿用户端看不到
	if _, err := svc.Campaign(ctx, appID, alice, campaign.ID); errCode(err) != errCodePromoNotFound {
		t.Fatalf("草稿应当不可见，得到 %v", err)
	}
	if campaign, err = svc.SetStatus(ctx, appID, campaign.ID, promo.StatusOnline, "admin"); err != nil {
		t.Fatal(err)
	}

	// ── 邀请人数：活动开始之前的邀请不算 ──
	if _, err := inviteService.Bind(ctx, InviteBindRequest{AppID: appID, UserID: bob, Code: center.Code, DeviceID: "d-bob"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE invite_relations SET created_at = $2 WHERE invitee_id = $1`, bob, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for index, invitee := range []int64{carol, dave} {
		if _, err := inviteService.Bind(ctx, InviteBindRequest{AppID: appID, UserID: invitee, Code: center.Code,
			DeviceID: "d-" + string(rune('a'+index))}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := svc.Campaign(ctx, appID, alice, campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Invite == nil || detail.Invite.Count != 2 || detail.Invite.Code != center.Code {
		t.Fatalf("活动时间内应有 2 人（bob 在开始前不算），得到 %+v", detail.Invite)
	}
	if detail.Invite.Tiers[0].State != promo.TierClaimable || detail.Invite.Tiers[1].State != promo.TierClaimable {
		t.Fatalf("两档都应可领取，得到 %+v", detail.Invite.Tiers)
	}
	if detail.Share == nil || !detail.Share.CanSubmit || len(detail.Share.Forbidden) == 0 || detail.Share.Forbidden[0] != "抖音" {
		t.Fatalf("分享任务应可提交且明示抖音禁止，得到 %+v", detail.Share)
	}
	summaries, err := svc.Campaigns(ctx, appID, alice)
	if err != nil || len(summaries) != 1 || !summaries[0].Claimable {
		t.Fatalf("列表应有一期可领取的活动，得到 %+v %v", summaries, err)
	}

	// ── 领取档位 ──
	before := integral(alice)
	grant, err := svc.ClaimTier(ctx, appID, alice, campaign.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(grant.Results) != 1 || integral(alice) != before+100 {
		t.Fatalf("领取首邀应得 100 积分，得到 %+v，积分 %d→%d", grant, before, integral(alice))
	}
	_, err = svc.ClaimTier(ctx, appID, alice, campaign.ID, 1)
	expectCode("重复领取", err, errCodePromoTierClaimed)
	_, err = svc.ClaimTier(ctx, appID, alice, campaign.ID, 5)
	expectCode("不存在的档位", err, errCodePromoTierLocked)
	_, err = svc.ClaimTier(ctx, appID, bob, campaign.ID, 1)
	expectCode("人数不够", err, errCodePromoTierLocked)
	// 并发领同一档只成一次
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := svc.ClaimTier(ctx, appID, alice, campaign.ID, 2); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if succeeded != 1 || integral(alice) != before+300 {
		t.Fatalf("并发领取应只成功一次，成功 %d 次，积分 %d", succeeded, integral(alice)-before)
	}
	var category string
	if err := pool.QueryRow(ctx, `SELECT category FROM integral_transactions WHERE user_id = $1 ORDER BY id DESC LIMIT 1`,
		alice).Scan(&category); err != nil || category != invite.IntegralCategory {
		t.Fatalf("流水分类应为 invite，得到 %q %v", category, err)
	}

	// 未开始的活动
	future := input
	future.Title, future.StartsAt, future.EndsAt = "下一季", now.Add(24*time.Hour), now.Add(48*time.Hour)
	upcoming, err := svc.CreateCampaign(ctx, future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetStatus(ctx, appID, upcoming.ID, promo.StatusOnline, "admin"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ClaimTier(ctx, appID, alice, upcoming.ID, 1)
	expectCode("未开始的活动领取", err, errCodePromoNotActive)

	// ── 提交文章 ──
	submit := func(userID int64, platform, url string) (*promo.ClientSubmission, error) {
		return svc.Submit(ctx, promo.SubmitInput{AppID: appID, CampaignID: campaign.ID, UserID: userID, Platform: platform, URL: url})
	}
	_, err = submit(alice, "xiaohongshu", "https://www.douyin.com/video/7300000000")
	expectCode("抖音链接", err, errCodePromoLinkForbidden)
	_, err = submit(alice, "xiaohongshu", "https://www.coolapk.com/feed/1")
	expectCode("平台不符", err, errCodePromoLinkInvalid)
	_, err = submit(alice, "zhihu", "https://zhuanlan.zhihu.com/p/1")
	expectCode("未启用的平台", err, errCodePromoPlatformUnavail)
	xhs, err := submit(alice, "xiaohongshu", "https://www.xiaohongshu.com/explore/abc?xsec_token=1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = submit(erin, "xiaohongshu", "https://xiaohongshu.com/explore/abc?xsec_token=2")
	expectCode("别人提交过的同一篇", err, errCodePromoLinkDuplicate)
	_, err = submit(alice, "xiaohongshu", "https://www.xiaohongshu.com/explore/def")
	expectCode("同一平台第二篇", err, errCodePromoLimited)
	cool, err := submit(alice, "coolapk", "https://www.coolapk.com/feed/123")
	if err != nil {
		t.Fatal(err)
	}
	_, err = submit(alice, "wechat_mp", "https://mp.weixin.qq.com/s/AbC")
	expectCode("每人篇数（待审核也占）", err, errCodePromoLimited)

	// ── 审核 ──
	if _, err := svc.Review(ctx, promo.ReviewInput{AppID: appID, IDs: []int64{cool.ID}, Action: promo.ReviewReject}); errCode(err) != 40000 {
		t.Fatalf("驳回不写原因应失败，得到 %v", err)
	}
	outcomes, err := svc.Review(ctx, promo.ReviewInput{AppID: appID, IDs: []int64{cool.ID}, Action: promo.ReviewReject, Reason: "内容与应用无关", Operator: "admin"})
	if err != nil || len(outcomes) != 1 || !outcomes[0].OK || outcomes[0].Status != promo.SubmissionRejected {
		t.Fatalf("驳回失败：%+v %v", outcomes, err)
	}
	if _, err := submit(alice, "coolapk", "https://coolapk.com/feed/123/"); err != nil {
		t.Fatalf("驳回之后同一篇应能重交：%v", err)
	}
	before = integral(alice)
	outcomes, err = svc.Review(ctx, promo.ReviewInput{AppID: appID, IDs: []int64{xhs.ID, xhs.ID, 999999}, Action: promo.ReviewApprove, Operator: "admin"})
	if err != nil || len(outcomes) != 2 {
		t.Fatalf("审核应去重并逐条返回：%+v %v", outcomes, err)
	}
	if !outcomes[0].OK || outcomes[0].Status != promo.SubmissionApproved || integral(alice) != before+50 {
		t.Fatalf("通过应发 50 积分：%+v，积分 %d", outcomes[0], integral(alice)-before)
	}
	if outcomes[1].OK || outcomes[1].Reason != promo.ReasonNotFound {
		t.Fatalf("不存在的提交应失败：%+v", outcomes[1])
	}
	outcomes, err = svc.Review(ctx, promo.ReviewInput{AppID: appID, IDs: []int64{xhs.ID}, Action: promo.ReviewApprove, Operator: "admin"})
	if err != nil || outcomes[0].OK || outcomes[0].Reason != promo.ReasonAlreadyReviewed {
		t.Fatalf("重复审核应失败：%+v %v", outcomes, err)
	}
	mine, err := svc.MySubmissions(ctx, appID, alice, campaign.ID, 1, 20)
	if err != nil || mine.Total != 3 || mine.Items[0].Status != promo.SubmissionPending {
		t.Fatalf("我的提交应有 3 条、最新的待审核：%+v %v", mine, err)
	}

	// ── 排行与统计 ──
	board, err := svc.Leaderboard(ctx, appID, alice, campaign.ID)
	if err != nil || len(board.Items) != 1 || board.Items[0].Count != 2 || board.Me == nil || board.Me.Rank != 1 {
		t.Fatalf("排行应只有 alice 2 人：%+v %v", board, err)
	}
	if board.Items[0].Account != "" {
		t.Fatalf("用户端排行不应带账号：%+v", board.Items[0])
	}
	board, err = svc.Leaderboard(ctx, appID, erin, campaign.ID)
	if err != nil || board.Me == nil || board.Me.Rank != 0 {
		t.Fatalf("没有邀请的人名次为 0：%+v %v", board, err)
	}
	stats, err := svc.Stats(ctx, appID, campaign.ID, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if stats.Invited != 2 || stats.Participants != 1 || stats.Submissions.Approved != 1 || stats.Submissions.Rejected != 1 ||
		stats.Submissions.Pending != 1 || len(stats.TierClaims) != 2 || stats.TierClaims[1].Claimed != 1 || len(stats.Daily) == 0 {
		t.Fatalf("统计不对：%+v", stats)
	}
	list, err := svc.ListCampaigns(ctx, promo.CampaignQuery{AppID: appID})
	if err != nil || list.Total != 2 {
		t.Fatalf("管理端列表应有 2 期：%+v %v", list, err)
	}

	// ── 删除限制 ──
	if err := svc.DeleteCampaign(ctx, appID, campaign.ID); errCode(err) != 40000 {
		t.Fatalf("上线中的活动不能删，得到 %v", err)
	}
	if _, err := svc.SetStatus(ctx, appID, campaign.ID, promo.StatusOffline, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCampaign(ctx, appID, campaign.ID); errCode(err) != 40000 {
		t.Fatalf("有人参与过的活动不能删，得到 %v", err)
	}
	if _, err := svc.Campaign(ctx, appID, alice, campaign.ID); errCode(err) != errCodePromoNotFound {
		t.Fatalf("下线之后用户端不可见，得到 %v", err)
	}
	if _, err := svc.SetStatus(ctx, appID, upcoming.ID, promo.StatusOffline, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCampaign(ctx, appID, upcoming.ID); err != nil {
		t.Fatalf("没人参与的下线活动应能删：%v", err)
	}
}
