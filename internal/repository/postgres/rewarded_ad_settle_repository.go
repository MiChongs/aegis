package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/aigateway"
	cardkeydomain "aegis/internal/domain/cardkey"
	"aegis/internal/domain/featuretime"
	lotterydomain "aegis/internal/domain/lottery"
	rewardedad "aegis/internal/domain/rewardedad"
	"aegis/internal/domain/usernumber"
	vipdomain "aegis/internal/domain/vip"
)

// 激励广告的结算：一方带来事实 → 补齐观看记录 → 条件满足时同事务发奖。
//
// 一次观看由两方各确认一次（服务端回调证明看过、客户端上报说明为谁为什么看），
// 两方到达的先后不确定，平台还会重试回调、客户端还会轮询上报。于是这里只有一个入口，
// 无论谁先到、到几次，都落在 (appid, trans_id) 这一行上：
//
//   - 先到的一方建行（INSERT … ON CONFLICT DO NOTHING），然后**锁住这一行**；
//   - 每一方只补自己那一半的事实，从不覆盖另一方的；
//   - 状态只有一次跃迁 pending → granted / rejected，跃迁后再来的请求只读不写。
//
// 日限额与冷却在锁住用户行之后判定：同一用户的两次结算由此串行，
// 「两个请求同时看到还剩 1 次」这种事不会发生（抽奖的日限额曾经就是这么被并发穿透的）。

// RecordRewardedAdView 记录一方带来的事实，并在条件满足时发奖。返回结算后的记录。
func (r *Repository) RecordRewardedAdView(ctx context.Context, input rewardedad.RecordInput,
	policy rewardedad.SettlePolicy) (*rewardedad.View, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if input.Side != rewardedad.SideAdmin {
		if _, err := tx.Exec(ctx, `INSERT INTO app_rewarded_ad_views (appid, user_id, trans_id)
VALUES ($1, $2, $3) ON CONFLICT (appid, trans_id) DO NOTHING`,
			input.AppID, nullableInt64(input.UserID), input.TransID); err != nil {
			return nil, err
		}
	}

	view, err := scanRewardedAdView(tx.QueryRow(ctx, `SELECT `+rewardedAdViewColumns+`
FROM app_rewarded_ad_views v WHERE v.appid = $1 AND v.trans_id = $2 FOR UPDATE`, input.AppID, input.TransID))
	if err != nil {
		return nil, err
	}

	// 身份一致性：两方说的必须是同一个人。
	if input.UserID > 0 && view.UserID > 0 && view.UserID != input.UserID {
		switch input.Side {
		case rewardedad.SideClient:
			// 拿别人的 transId 来领奖。不动这一行，原主人的结算不受影响。
			return nil, ErrRewardedAdUserMismatch
		case rewardedad.SideServer:
			// 平台说是 A 看的，先来上报的却是 B：以平台为准，这次观看谁都不发。
			if view.Status == rewardedad.StatusPending {
				view.Status = rewardedad.StatusRejected
				view.Reason = rewardedad.ReasonUserMismatch
			}
		}
	}
	// 平台回调里的 userId 是我们签发的那一个才算数；认不出时这次观看谁都不发，
	// 否则任何拿到这个 transId 的人都能来认领它。
	if input.Side == rewardedad.SideServer && input.UserID == 0 && view.Status == rewardedad.StatusPending {
		view.Status = rewardedad.StatusRejected
		view.Reason = rewardedad.ReasonUserNotFound
	}
	if view.UserID == 0 {
		view.UserID = input.UserID
	}

	mergeRewardedAdFacts(view, input, now)

	if view.Status == rewardedad.StatusGranted && input.Side == rewardedad.SideAdmin {
		return nil, ErrRewardedAdAlreadyGranted
	}
	settleable := view.Status == rewardedad.StatusPending ||
		(policy.ForceGrant && view.Status == rewardedad.StatusRejected)
	if settleable && policy.Ready(view) {
		if err := r.settleRewardedAdViewTx(ctx, tx, view, policy, now); err != nil {
			return nil, err
		}
	}

	results, err := json.Marshal(view.Results)
	if err != nil {
		return nil, err
	}
	var conversion []byte
	if view.Conversion != nil {
		if conversion, err = json.Marshal(view.Conversion); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE app_rewarded_ad_views SET
  user_id = $2, scene = $3, placement_id = $4, status = $5, reason = $6,
  server_verified_at = $7, reward_name = $8, reward_amount = $9, network_id = $10, extra = $11,
  client_reported_at = $12, client_verified = $13, client_error = $14, device_id = $15, client_ip = $16,
  rewards = $17, granted_at = $18, operator = $19, conversion = $20, updated_at = NOW()
WHERE id = $1`,
		view.ID, nullableInt64(view.UserID), view.Scene, view.PlacementID, view.Status, view.Reason,
		view.ServerVerifiedAt, view.RewardName, view.RewardAmount, view.NetworkID, view.Extra,
		view.ClientReportedAt, view.ClientVerified, view.ClientError, view.DeviceID, view.ClientIP,
		results, view.GrantedAt, view.Operator, conversion); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return view, nil
}

// mergeRewardedAdFacts 只补这一方的那一半事实，从不覆盖另一方的。
func mergeRewardedAdFacts(view *rewardedad.View, input rewardedad.RecordInput, now time.Time) {
	switch input.Side {
	case rewardedad.SideServer:
		if view.ServerVerifiedAt == nil {
			view.ServerVerifiedAt = &now
		}
		if input.PlacementID != "" {
			view.PlacementID = truncateColumn(input.PlacementID, 64)
		}
		view.RewardName = truncateColumn(input.RewardName, 128)
		view.RewardAmount = input.RewardAmount
		view.NetworkID = truncateColumn(input.NetworkID, 32)
		view.Extra = truncateColumn(input.Extra, 2000)
		// 场景以客户端上报为准；回调里推出来的只在客户端还没说话时补位。
		if view.Scene == "" && input.Scene != "" {
			view.Scene = truncateColumn(input.Scene, 64)
		}
	case rewardedad.SideClient:
		if view.ClientReportedAt == nil {
			view.ClientReportedAt = &now
		}
		if view.Status == rewardedad.StatusPending && input.Scene != "" {
			view.Scene = truncateColumn(input.Scene, 64)
		}
		if view.PlacementID == "" && input.PlacementID != "" {
			view.PlacementID = truncateColumn(input.PlacementID, 64)
		}
		if input.ClientVerified != nil {
			view.ClientVerified = input.ClientVerified
		}
		if input.ClientError != "" {
			view.ClientError = truncateColumn(input.ClientError, 128)
		}
		if input.DeviceID != "" {
			view.DeviceID = truncateColumn(input.DeviceID, 128)
		}
		if input.ClientIP != "" {
			view.ClientIP = truncateColumn(input.ClientIP, 64)
		}
	case rewardedad.SideAdmin:
		if input.Scene != "" {
			view.Scene = truncateColumn(input.Scene, 64)
		}
		view.Operator = truncateColumn(input.Operator, 128)
	}
}

// settleRewardedAdViewTx 判定并发放。调用方已持有观看记录的行锁。
func (r *Repository) settleRewardedAdViewTx(ctx context.Context, tx pgx.Tx, view *rewardedad.View,
	policy rewardedad.SettlePolicy, now time.Time) error {
	reject := func(reason string) error {
		view.Status = rewardedad.StatusRejected
		view.Reason = reason
		return nil
	}

	if !policy.Enabled && !policy.ForceGrant {
		return reject(rewardedad.ReasonDisabled)
	}
	if view.UserID == 0 {
		return reject(rewardedad.ReasonUserNotFound)
	}
	if view.Scene == "" && policy.VerifyMode == rewardedad.VerifyServer {
		cfg := rewardedad.Config{Scenes: policy.Scenes}
		if scene, ok := cfg.SceneForPlacement(view.PlacementID); ok {
			view.Scene = scene.Key
		}
	}
	scene, ok := policy.FindScene(view.Scene)
	if !ok || (!scene.Enabled && !policy.ForceGrant) {
		return reject(rewardedad.ReasonSceneUnavailable)
	}
	if policy.VerifyMode == rewardedad.VerifyClient && !policy.ForceGrant &&
		view.ClientVerified != nil && !*view.ClientVerified {
		return reject(rewardedad.ReasonClientUnverified)
	}

	// 锁用户行：同一用户的结算从这里开始串行，限额判定因此不会被并发穿透。
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		view.UserID, view.AppID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reject(rewardedad.ReasonUserNotFound)
		}
		return err
	}

	if !policy.ForceGrant {
		usage, err := rewardedAdUsage(ctx, tx, view.AppID, view.UserID, policy.DayStart)
		if err != nil {
			return err
		}
		if policy.DailyLimit > 0 && usage.TodayTotal >= policy.DailyLimit {
			return reject(rewardedad.ReasonDailyLimit)
		}
		if scene.DailyLimit > 0 && usage.TodayByScene[scene.Key] >= scene.DailyLimit {
			return reject(rewardedad.ReasonSceneDailyLimit)
		}
		if scene.CooldownSeconds > 0 {
			if last, ok := usage.LastByScene[scene.Key]; ok &&
				now.Before(last.Add(time.Duration(scene.CooldownSeconds)*time.Second)) {
				return reject(rewardedad.ReasonCooldown)
			}
		}
	}

	// 已拥有判定在锁用户行之后取：同一把锁下，看广告与购买 / 兑换会员是串行的
	outcome, plans, err := resolveRewardedAdSceneTx(ctx, tx, view.AppID, view.UserID, scene)
	if err != nil {
		return err
	}
	results, err := r.grantAdRewardsTx(ctx, tx, view, scene, outcome, plans)
	if err != nil {
		return err
	}
	view.Status = rewardedad.StatusGranted
	view.Reason = ""
	view.Results = results
	view.Conversion = outcome.Conversion
	view.GrantedAt = &now
	return nil
}

// resolveRewardedAdSceneTx 这个场景对这个人发什么：已拥有的部分按场景的 lifetime 折算（见 rewardedad.Resolve）。
// 顺带返回场景引用的套餐，发放时不必再查一遍。
func resolveRewardedAdSceneTx(ctx context.Context, tx pgx.Tx, appID, userID int64,
	scene rewardedad.Scene) (rewardedad.Outcome, map[int64]vipdomain.Plan, error) {
	plans, err := rewardedAdScenePlans(ctx, tx, appID, []rewardedad.Scene{scene})
	if err != nil {
		return rewardedad.Outcome{}, nil, err
	}
	coverage, err := rewardedAdCoverage(ctx, tx, appID, userID)
	if err != nil {
		return rewardedad.Outcome{}, nil, err
	}
	labels := rewardedad.Labels{Plans: map[int64]string{}, Features: map[string]string{}}
	for id, plan := range plans {
		labels.Plans[id] = plan.Name
	}
	for _, tag := range scene.FeatureMinutesTags() {
		name, err := featureNameTx(ctx, tx, appID, tag)
		if err != nil {
			return rewardedad.Outcome{}, nil, err
		}
		labels.Features[tag] = name
	}
	return rewardedad.Resolve(scene, coverage, plans, labels), plans, nil
}

// rewardedAdCoverage 这个人已经永久拥有什么：永久会员与其功能、已永久解锁的功能（与抽奖同一个判据）。
func rewardedAdCoverage(ctx context.Context, q queryExecutor, appID, userID int64) (rewardedad.Coverage, error) {
	coverage, err := lotteryMemberCoverage(ctx, q, appID, userID)
	if err != nil {
		return rewardedad.Coverage{}, err
	}
	return rewardedad.Coverage{Holdings: coverage.Holdings}, nil
}

// RewardedAdCoverage 见 rewardedAdCoverage（客户端列表据此说明「对你来说这个场景会怎样」）。
func (r *Repository) RewardedAdCoverage(ctx context.Context, appID, userID int64) (rewardedad.Coverage, error) {
	return rewardedAdCoverage(ctx, r.pool, appID, userID)
}

// rewardedAdScenePlans 场景里「会员套餐」引用的套餐（含停用的：已配进场景的照发）。
func rewardedAdScenePlans(ctx context.Context, q queryExecutor, appID int64,
	scenes []rewardedad.Scene) (map[int64]vipdomain.Plan, error) {
	ids := make([]int64, 0, 2)
	for _, scene := range scenes {
		for _, reward := range scene.Rewards {
			if reward.Type == cardkeydomain.RewardVipPlan && reward.RefID > 0 {
				ids = append(ids, reward.RefID)
			}
		}
	}
	plans := make(map[int64]vipdomain.Plan, len(ids))
	if len(ids) == 0 {
		return plans, nil
	}
	rows, err := q.Query(ctx, `SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = ANY($2)`, appID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		plan, err := scanVipPlan(rows)
		if err != nil {
			return nil, err
		}
		plans[plan.ID] = *plan
	}
	return plans, rows.Err()
}

// RewardedAdScenePlans 见 rewardedAdScenePlans。
func (r *Repository) RewardedAdScenePlans(ctx context.Context, appID int64,
	scenes []rewardedad.Scene) (map[int64]vipdomain.Plan, error) {
	return rewardedAdScenePlans(ctx, r.pool, appID, scenes)
}

// grantAdRewardsTx 在同一事务里发放一个场景对这个人的全部权益（已拥有的部分已由 outcome 折算好）。
//
// 这里的 switch 与 rewardedad 的权益目录**双向钉死**（TestRewardedAdCatalogHasGrantBranch）：
// 目录多一档 → 配得上却发不出来；这里多一档 → 控制台配不出来。
func (r *Repository) grantAdRewardsTx(ctx context.Context, tx pgx.Tx, view *rewardedad.View,
	scene rewardedad.Scene, outcome rewardedad.Outcome, plans map[int64]vipdomain.Plan) ([]rewardedad.RewardResult, error) {
	out := make([]rewardedad.RewardResult, 0, len(outcome.Grants))
	source := "激励广告 " + scene.Name
	viewID := view.ID
	baseMeta := map[string]any{"transId": view.TransID, "scene": scene.Key, "rewardedAdViewId": view.ID}
	// 场景的会员权益在判定时按场景的当前配置实时取（见 vip.Segment.AdTerms），
	// 这里只留开通那一刻的快照，供场景被删除后兜底。
	if scene.GrantsMembership() {
		baseMeta["adFree"] = scene.Membership.AdFreeOrDefault()
	}
	convertedMeta := maps.Clone(baseMeta)
	convertedMeta["converted"] = true
	if outcome.Conversion != nil {
		convertedMeta["conversionReason"] = outcome.Conversion.Reason
	}

	// 折算说明放进结果的第一行：已发布的客户端只把 results 逐行展示（领取弹窗、观看记录），
	// 不读 conversion 与 message，不升级也能看到「为什么发的是积分」。
	if outcome.Conversion != nil {
		out = append(out, rewardedad.RewardResult{
			Type: rewardedad.ResultConversion, Label: rewardedad.ConversionResultLabel(outcome.Conversion),
			Detail: outcome.Conversion.Message,
		})
	}

	for _, grant := range outcome.Grants {
		reward := grant.Reward
		spec, _ := rewardedad.FindRewardSpec(reward.Type)
		result := rewardedad.RewardResult{Type: reward.Type, Label: spec.Label}
		meta, integralRemark, experienceRemark := baseMeta, "看广告赠送积分", "看广告赠送经验"
		if grant.Converted {
			meta, integralRemark, experienceRemark = convertedMeta, "看广告奖励折算积分", "看广告奖励折算经验"
			result.Label += "（折算）"
		}

		switch reward.Type {
		case cardkeydomain.RewardVipPlan:
			plan, ok := plans[reward.RefID]
			if !ok {
				return nil, errors.New("rewarded ad: vip plan " + strconv.FormatInt(reward.RefID, 10) + " not found")
			}
			planID := plan.ID
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:        view.UserID,
				AppID:         view.AppID,
				PlanID:        &planID,
				PlanName:      plan.Name,
				Features:      plan.Features,
				DurationDays:  plan.DurationDays,
				Lifetime:      plan.Lifetime,
				PayChannel:    vipdomain.ChannelAdReward,
				PayAmount:     decimal.Zero,
				BonusIntegral: plan.BonusIntegral,
				Operator:      view.Operator,
				Metadata:      meta,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
			result.TransactionNo = txn.TransactionNo

		case cardkeydomain.RewardVipDays:
			txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:       view.UserID,
				AppID:        view.AppID,
				PlanName:     "看广告赠送",
				Features:     scene.Membership.Features,
				DurationDays: int(reward.Amount),
				PayChannel:   vipdomain.ChannelAdReward,
				PayAmount:    decimal.Zero,
				Operator:     view.Operator,
				Metadata:     meta,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward)
			result.TransactionNo = txn.TransactionNo

		case cardkeydomain.RewardIntegral:
			_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, view.UserID, view.AppID, reward.Amount,
				"earn", "rewarded_ad", integralRemark, source, "rewarded_ad", &viewID, meta)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + "（余额 " + strconv.FormatInt(after, 10) + "）"
			result.TransactionNo = txnNo

		case cardkeydomain.RewardExperience:
			expResult, err := r.applyExperienceChangeTx(ctx, tx, view.UserID, view.AppID, reward.Amount,
				"rewarded_ad", experienceRemark, source, "rewarded_ad", &viewID, meta)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward)
			if expResult.LevelChanged {
				result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
			}
			result.TransactionNo = expResult.TransactionNo

		case cardkeydomain.RewardLotteryDraws:
			balance, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
				AppID: view.AppID, UserID: view.UserID, Amount: reward.Amount, Origin: lotterydomain.TicketOriginRewardedAd,
				Ref: view.TransID, Title: source,
			})
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

		case rewardedad.RewardFeatureMinutes:
			// 功能时长按秒记账；功能名取目录里的展示名，目录里删掉了也照发（标识本身就是账户的键）
			name, err := featureNameTx(ctx, tx, view.AppID, reward.Feature)
			if err != nil {
				return nil, err
			}
			balance, err := grantFeatureTimeTx(ctx, tx, view.AppID, view.UserID, reward.Feature, reward.Amount*60,
				featuretime.RefRewardedAd, strconv.FormatInt(view.ID, 10), source)
			if err != nil {
				return nil, err
			}
			result.Detail = rewardedad.DescribeFeatureMinutes(reward, name) +
				"（余额 " + strconv.FormatInt(featuretime.FormatMinutes(balance), 10) + " 分钟）"

		case rewardedad.RewardAICredit:
			// AI 中转的赠送额度：用户行已在上面锁住，这里只锁网关账户行（锁序 users → ai_gateway_accounts）。
			balance, err := grantAIGatewayCreditTx(ctx, tx, view.AppID, view.UserID, reward.Amount*aigateway.MicroPerPoint,
				"rewarded_ad", strconv.FormatInt(view.ID, 10), source)
			if err != nil {
				return nil, err
			}
			result.Detail = rewardedad.DescribeAICredit(reward) + "（余额 " + aigateway.MicroToPoints(balance).Round(2).String() + " 点）"

		case cardkeydomain.RewardNumberChange:
			_, note, err := grantNumberRewardTx(ctx, tx, view.AppID, view.UserID, reward.Type, reward.Amount, 0, usernumber.OriginRewardedAd, view.TransID, source)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + note

		case cardkeydomain.RewardNumberTicket:
			_, note, err := grantNumberRewardTx(ctx, tx, view.AppID, view.UserID, reward.Type, reward.Amount, reward.RefID, usernumber.OriginRewardedAd, view.TransID, source)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + note

		case cardkeydomain.RewardNumberDraw:
			_, note, err := grantNumberRewardTx(ctx, tx, view.AppID, view.UserID, reward.Type, reward.Amount, 0, usernumber.OriginRewardedAd, view.TransID, source)
			if err != nil {
				return nil, err
			}
			result.Detail = cardkeydomain.DescribeReward(reward) + note

		default:
			// 目录里有、这里没有 —— 由测试挡在合并之前；运行期走到这里说明配置绕过了校验。
			return nil, errors.New("rewarded ad: unsupported reward type " + reward.Type)
		}
		out = append(out, result)
	}
	return out, nil
}
