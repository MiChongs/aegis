package service

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 抽奖计算引擎的端到端集成测试（真实 Postgres）：
// 精确分析（人群、特权、已拥有折算、积分账）→ 奖盘求解（解出的权重保存后复算达标）→ 推演任务
// （排队 → 认领执行 → 结果；同一个种子重跑逐字节相同；取消；实例没了之后被接手；反复中断判失败；
// 实例关闭时放回排队；排队上限；删除）→ 逐笔复算审计（真实抽奖的种子随方案落库，改一条记录能指出来）。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryComputeIntegration
func TestLotteryComputeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40700)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-compute'), ($1 + 1, 'other') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}

	// 九宫格：3 格谢谢参与（合计 50%）、10 积分兜底 30%、50 积分 8%、会员 7 天 6%、抽奖次数 4%、会员 30 天 2%
	none := lotterydomain.Reward{Type: lotterydomain.KindNone}
	prizes := []lotterydomain.PrizeInput{
		{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 10}, Weight: 3000, Quantity: -1, Fallback: true},
		{Position: 1, Reward: none, Weight: 2000, Quantity: -1},
		{Position: 2, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 50}, Weight: 800, Quantity: -1},
		{Position: 3, Reward: none, Weight: 2000, Quantity: -1},
		{Position: 4, Reward: lotterydomain.Reward{Type: lotterydomain.KindVipDays, Amount: 7}, Weight: 600, Quantity: -1},
		{Position: 5, Reward: none, Weight: 1000, Quantity: -1},
		{Position: 6, Reward: lotterydomain.Reward{Type: lotterydomain.KindLotteryDraws, Amount: 1}, Weight: 400, Quantity: -1},
		{Position: 7, Reward: lotterydomain.Reward{Type: lotterydomain.KindVipDays, Amount: 30}, Weight: 200, Quantity: 500},
	}
	input := lotterydomain.ActivityInput{
		AppID: appID, Name: "计算引擎", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		FreeDaily: 100, PointsCost: 20, PityThreshold: 8, PityPosition: 2, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{Fortune: &lotterydomain.Fortune{Spread: 0.3}, SoftPity: &lotterydomain.SoftPity{Start: 4, Step: 5}},
		Perks:    []lotterydomain.Perk{{Name: "永久会员特权", Audience: lotterydomain.AudienceLifetime, WinBoost: 1.5, PityCut: 2}},
	}
	activity, err := lottery.CreateActivity(ctx, input)
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	personas := []LotteryPersonaSpec{
		{Key: "user", Share: 8},
		{Key: "lifetime", Member: true, Lifetime: true, Share: 2, DrawsPerDay: 6},
	}

	// ── 精确分析 ──
	analysis, err := lottery.Analyze(ctx, LotteryAnalysisRequest{AppID: appID, ActivityID: activity.ID, Personas: personas})
	if err != nil {
		t.Fatalf("精确分析失败：%v", err)
	}
	if len(analysis.Personas) != 2 || analysis.Economy == nil {
		t.Fatalf("应有两种人群与积分账：%+v", analysis)
	}
	// 结果要能编成 JSON 发给控制台：里面不能有 NaN / Inf
	if _, err := json.Marshal(analysis); err != nil {
		t.Fatalf("分析结果编不成 JSON：%v", err)
	}
	user, lifetime := analysis.Personas[0], analysis.Personas[1]
	if user.Name != "普通用户" || lifetime.Name != "永久会员" || lifetime.Perks == nil || lifetime.Perks.WinBoost != 1.5 {
		t.Fatalf("人群与特权：%+v / %+v", user.Name, lifetime.Perks)
	}
	for _, persona := range analysis.Personas {
		var sum float64
		for _, prize := range persona.Analysis.Prizes {
			sum += prize.LongRun
		}
		if math.Abs(sum-1) > 1e-9 || persona.Analysis.Fortune == nil {
			t.Fatalf("%s 的综合概率之和 %v，应有每日运气一项", persona.Name, sum)
		}
	}
	// 普通用户：公示中奖率 50%，保底与软保底把综合中奖率抬高；保底奖（50 积分）的综合概率高于公示
	if a := user.Analysis; a.BaseWinRate != 0.5 || a.WinRate <= 0.5 || a.PityShare <= 0 || a.Prizes[2].LongRun <= a.Prizes[2].Nominal {
		t.Fatalf("普通用户的综合中奖率应高于公示：%+v", a)
	}
	// 永久会员：中奖加成 ×1.5、保底提前 2 次 → 综合中奖率更高；会员奖品折成积分 → 每抽到账的积分更多
	if lifetime.Analysis.WinRate <= user.Analysis.WinRate || lifetime.Return <= user.Return {
		t.Fatalf("永久会员的中奖率与积分回报应更高：%v/%v，%v/%v",
			lifetime.Analysis.WinRate, user.Analysis.WinRate, lifetime.Return, user.Return)
	}
	if user.Payout[4] != 0 || lifetime.Payout[4] <= 0 || user.Price != 20 || lifetime.Redraws[6] != 1 {
		t.Fatalf("会员 7 天只对永久会员折成积分：%v / %v", user.Payout, lifetime.Payout)
	}
	// 未保存的编辑内容也能分析：去掉保底之后综合中奖率回到公示值
	draft := input
	draft.PityThreshold, draft.PityPosition, draft.Strategy = 0, -1, lotterydomain.Strategy{}
	plain, err := lottery.Analyze(ctx, LotteryAnalysisRequest{AppID: appID, ActivityID: activity.ID, Input: &draft})
	if err != nil {
		t.Fatal(err)
	}
	if a := plain.Personas[0].Analysis; len(plain.Personas) != 1 || math.Abs(a.WinRate-0.5) > 1e-9 || a.Fortune != nil {
		t.Fatalf("草稿没有保底与运气：%+v", a)
	}
	if _, err := lottery.Analyze(ctx, LotteryAnalysisRequest{AppID: appID + 1, ActivityID: activity.ID}); errCode(err) != errCodeLotteryNotFound {
		t.Fatalf("别的应用的活动应报 40402，得到 %v", err)
	}

	// ── 奖盘求解 ──
	solved, err := lottery.Solve(ctx, LotterySolveRequest{AppID: appID, ActivityID: activity.ID, Goal: lotterydomain.SolveGoal{
		WinRate: 0.45,
		Prizes:  []lotterydomain.SolvePrizeGoal{{Position: 7, Mode: lotterydomain.SolveTarget, Probability: 0.01}},
	}})
	if err != nil {
		t.Fatalf("求解失败：%v", err)
	}
	if !solved.Feasible || !solved.Converged || solved.Persona != "普通用户" || solved.Factor != 1 {
		t.Fatalf("应可解：%+v", solved.SolveResult)
	}
	if solved.EconomyBefore == nil || solved.EconomyAfter == nil {
		t.Fatal("应带求解前后的积分账")
	}
	if raw, err := json.Marshal(solved); err != nil || !strings.Contains(string(raw), `"newWeight"`) || !strings.Contains(string(raw), `"persona"`) {
		t.Fatalf("求解结果应编成一层 JSON（求解结果的字段与人群并列）：%v", err)
	}
	// 把解出来的权重保存，再独立分析一遍：综合中奖率与那一格的综合概率达标
	applied := input
	applied.Prizes = append([]lotterydomain.PrizeInput(nil), prizes...)
	saved, err := lottery.GetActivity(ctx, appID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range applied.Prizes {
		applied.Prizes[index].ID = saved.Prizes[index].ID
		applied.Prizes[index].Weight = solved.Prizes[index].NewWeight
	}
	if _, err := lottery.UpdateActivity(ctx, activity.ID, applied); err != nil {
		t.Fatalf("保存求解结果失败：%v", err)
	}
	again, err := lottery.Analyze(ctx, LotteryAnalysisRequest{AppID: appID, ActivityID: activity.ID})
	if err != nil {
		t.Fatal(err)
	}
	if a := again.Personas[0].Analysis; math.Abs(a.WinRate-0.45) > 5e-4 || math.Abs(a.Prizes[7].LongRun-0.01) > 2e-4 {
		t.Fatalf("保存后复算：中奖率 %v，会员 30 天 %v", a.WinRate, a.Prizes[7].LongRun)
	}
	// 目标提在永久会员最走运那天：系数大于 1，求解对象是永久会员
	lucky, err := lottery.Solve(ctx, LotterySolveRequest{AppID: appID, ActivityID: activity.ID,
		Persona: &personas[1], Fortune: LotteryFortuneHigh, Goal: lotterydomain.SolveGoal{PointsReturn: 18}})
	if err != nil {
		t.Fatal(err)
	}
	if lucky.Persona != "永久会员" || lucky.Factor <= 1.5 || lucky.After.PointsReturn > 18*1.006 {
		t.Fatalf("永久会员最走运那天的积分回报应压到 18 以内：%+v，系数 %v", lucky.After, lucky.Factor)
	}
	if _, err := lottery.Solve(ctx, LotterySolveRequest{AppID: appID, ActivityID: activity.ID,
		Goal: lotterydomain.SolveGoal{WinRate: 1.5}}); errCode(err) != 40000 {
		t.Fatalf("不合法的目标应报 40000，得到 %v", err)
	}
	if _, err := lottery.Solve(ctx, LotterySolveRequest{AppID: appID, ActivityID: activity.ID, Fortune: "best"}); errCode(err) != 40000 {
		t.Fatalf("未知的运气取法应报 40000，得到 %v", err)
	}

	// ── 推演任务 ──
	request := LotteryComputeRequest{
		AppID: appID, ActivityID: activity.ID, Operator: "admin", Name: "上线前推演",
		Users: 300, Days: 3, DrawsPerDay: 4, Replications: 16, Seed: 20261120, Personas: personas,
	}
	waitFor := func(jobID int64, want func(*lotterydomain.ComputeJob) bool) *lotterydomain.ComputeJob {
		t.Helper()
		deadline := time.Now().Add(60 * time.Second)
		for {
			job, err := lottery.GetComputeJob(ctx, appID, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if want(job) {
				return job
			}
			if time.Now().After(deadline) {
				t.Fatalf("等任务 %d 超时：%+v", jobID, job)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	finished := func(job *lotterydomain.ComputeJob) bool { return job.Finished() }

	// 没有执行循环时任务只是排队；排队与执行中的到上限后不再收
	queued := []int64{}
	for range lotterydomain.MaxComputePending {
		job, err := lottery.CreateComputeJob(ctx, request)
		if err != nil {
			t.Fatalf("发起推演失败：%v", err)
		}
		queued = append(queued, job.ID)
	}
	first, err := lottery.GetComputeJob(ctx, appID, queued[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != lotterydomain.ComputeQueued || first.Result != nil || first.Total != 16 || first.ActivityID == nil ||
		first.Summary.Seed != "20261120" || first.Summary.Draws != (240*4+60*6)*3*16 || len(first.Summary.Personas) != 2 {
		t.Fatalf("排队中的任务：%+v", first)
	}
	if _, err := lottery.CreateComputeJob(ctx, request); errCode(err) != errCodeLotteryComputeBusy {
		t.Fatalf("排队到上限应报 42922，得到 %v", err)
	}
	if err := lottery.DeleteComputeJob(ctx, appID, queued[0]); errCode(err) != errCodeLotteryComputeActive {
		t.Fatalf("排队中的任务不能删，应报 40939，得到 %v", err)
	}
	// 取消排队中的：直接结束，不会被认领
	for _, id := range queued[2:] {
		cancelled, err := lottery.CancelComputeJob(ctx, appID, id, "admin")
		if err != nil || cancelled.Status != lotterydomain.ComputeCancelled {
			t.Fatalf("取消排队中的任务：%+v，%v", cancelled, err)
		}
	}

	// 起执行循环：两个同样的任务先后跑完，结果逐字节相同（同一个种子）
	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan struct{})
	go func() { defer close(loopDone); lottery.runComputeLoop(loopCtx) }()
	lottery.compute.wake()
	one := waitFor(queued[0], finished)
	two := waitFor(queued[1], finished)
	if one.Status != lotterydomain.ComputeSucceeded || one.Result == nil || one.Progress != 16 || one.Attempts != 1 || one.StartedAt == nil {
		t.Fatalf("任务应完成：%+v", one)
	}
	rawOne, _ := json.Marshal(one.Result)
	rawTwo, _ := json.Marshal(two.Result)
	if string(rawOne) != string(rawTwo) {
		t.Fatal("同一个种子的两次推演结果应逐字节相同")
	}
	result := one.Result
	if result.Draws != first.Summary.Draws || result.Replications != 16 || len(result.Personas) != 2 || len(result.Daily) != 3 {
		t.Fatalf("推演规模：%+v", result)
	}
	// 推演与精确分析互相印证（保存求解结果之后的奖盘：综合中奖率 45%）。每人只抽十几次，保底发生在周期末尾，
	// 推演的中奖率会略低于长期值，所以只要求在几个百分点之内
	if math.Abs(result.Personas[0].WinRate.Mean-0.45) > 0.03 {
		t.Fatalf("普通用户的中奖率：推演 %v，精确 0.45", result.Personas[0].WinRate.Mean)
	}
	if result.Personas[1].RealRate.Mean <= result.Personas[0].RealRate.Mean || result.Personas[1].Draws != 60*6*3*16 {
		t.Fatalf("永久会员应更容易中、每天抽 6 次：%+v", result.Personas[1])
	}
	if result.Arbitrage == nil || result.Arbitrage.Price != 20 || result.Fortune == nil || len(result.Stock) != 1 {
		t.Fatalf("套利检查、运气分布与库存曲线：%+v", result)
	}

	// 列表：新的在前、不带结果；按活动筛
	list, err := lottery.ListComputeJobs(ctx, appID, activity.ID, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != int64(lotterydomain.MaxComputePending) || list.Items[0].ID != queued[len(queued)-1] || list.Items[0].Result != nil {
		t.Fatalf("列表：%+v", list)
	}
	if other, _ := lottery.ListComputeJobs(ctx, appID, activity.ID+999, 1, 10); other.Total != 0 {
		t.Fatalf("别的活动没有任务：%+v", other)
	}

	// 取消执行中的：本实例立刻停下，状态是已取消、没有结果
	big := request
	big.Users, big.Days, big.DrawsPerDay, big.Replications, big.Seed = 20_000, 10, 10, 12, 0
	running, err := lottery.CreateComputeJob(ctx, big)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(running.ID, func(job *lotterydomain.ComputeJob) bool { return job.Status == lotterydomain.ComputeRunning })
	startedCancel := time.Now()
	cancelled, err := lottery.CancelComputeJob(ctx, appID, running.ID, "admin")
	if err != nil || cancelled.Status != lotterydomain.ComputeCancelled || cancelled.Result != nil {
		t.Fatalf("取消执行中的任务：%+v，%v", cancelled, err)
	}
	// 执行循环腾出手来接下一个任务，说明那一次推演确实停了
	next, err := lottery.CreateComputeJob(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if job := waitFor(next.ID, finished); job.Status != lotterydomain.ComputeSucceeded || time.Since(startedCancel) > 30*time.Second {
		t.Fatalf("取消之后循环应继续接任务：%+v", job)
	}
	if job, _ := lottery.GetComputeJob(ctx, appID, running.ID); job.Status != lotterydomain.ComputeCancelled || job.Result != nil {
		t.Fatalf("取消的任务不该被迟到的结果覆盖：%+v", job)
	}

	// 实例关闭时手上的任务放回排队，认领次数退回
	held, err := lottery.CreateComputeJob(ctx, big)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(held.ID, func(job *lotterydomain.ComputeJob) bool { return job.Status == lotterydomain.ComputeRunning })
	stopLoop()
	<-loopDone
	if job, _ := lottery.GetComputeJob(ctx, appID, held.ID); job.Status != lotterydomain.ComputeQueued || job.Attempts != 0 {
		t.Fatalf("关闭时应放回排队：%+v", job)
	}

	// 执行它的实例没了（心跳过期）：别的实例接手，从头重跑
	stale := time.Now().Add(-lotteryComputeStaleAfter)
	claimed, err := pg.ClaimLotteryComputeJobs(ctx, "dead-node", stale, 1)
	if err != nil || len(claimed) != 1 || claimed[0].ID != held.ID || claimed[0].Scenario == nil {
		t.Fatalf("认领：%+v，%v", claimed, err)
	}
	if again, _ := pg.ClaimLotteryComputeJobs(ctx, "other-node", stale, 1); len(again) != 0 {
		t.Fatal("心跳新鲜的任务不该被别人认领")
	}
	if ok, _ := pg.HeartbeatLotteryComputeJob(ctx, held.ID, "other-node", 3); ok {
		t.Fatal("不是持有者不能续心跳")
	}
	if ok, _ := pg.HeartbeatLotteryComputeJob(ctx, held.ID, "dead-node", 3); !ok {
		t.Fatal("持有者应能续心跳")
	}
	if _, err := pool.Exec(ctx, `UPDATE lottery_compute_jobs SET heartbeat_at = NOW() - INTERVAL '5 minutes' WHERE id = $1`, held.ID); err != nil {
		t.Fatal(err)
	}
	taken, err := pg.ClaimLotteryComputeJobs(ctx, "other-node", time.Now().Add(-lotteryComputeStaleAfter), 1)
	if err != nil || len(taken) != 1 || taken[0].Attempts != 2 || taken[0].Progress != 0 {
		t.Fatalf("心跳过期后应被接手、从头重跑：%+v，%v", taken, err)
	}
	// 原持有者迟到的心跳与结果都写不上
	if ok, _ := pg.HeartbeatLotteryComputeJob(ctx, held.ID, "dead-node", 9); ok {
		t.Fatal("被接手之后原持有者的心跳应失败")
	}
	if wrote, _ := pg.FinishLotteryComputeJob(ctx, held.ID, "dead-node", lotterydomain.ComputeSucceeded, one.Result, "", time.Second); wrote {
		t.Fatal("被接手之后原持有者的结果不该写上")
	}
	// 反复中断：认领次数用完后判失败，不再接手
	if _, err := pool.Exec(ctx, `UPDATE lottery_compute_jobs SET attempts = $2, heartbeat_at = NOW() - INTERVAL '5 minutes' WHERE id = $1`,
		held.ID, lotterydomain.MaxComputeAttempts); err != nil {
		t.Fatal(err)
	}
	if none, _ := pg.ClaimLotteryComputeJobs(ctx, "third-node", time.Now().Add(-lotteryComputeStaleAfter), 1); len(none) != 0 {
		t.Fatal("认领次数用完的任务不再认领")
	}
	if failed, err := pg.FailExhaustedLotteryComputeJobs(ctx, time.Now().Add(-lotteryComputeStaleAfter)); err != nil || failed != 1 {
		t.Fatalf("应判失败 1 个：%d，%v", failed, err)
	}
	if job, _ := lottery.GetComputeJob(ctx, appID, held.ID); job.Status != lotterydomain.ComputeFailed || job.Message == "" {
		t.Fatalf("反复中断的任务应判失败：%+v", job)
	}

	// 删除：结束的可以删；删过的、别的应用的都是不存在
	if err := lottery.DeleteComputeJob(ctx, appID, held.ID); err != nil {
		t.Fatal(err)
	}
	if err := lottery.DeleteComputeJob(ctx, appID, held.ID); errCode(err) != errCodeLotteryComputeNotFound {
		t.Fatalf("删过的任务应报 40417，得到 %v", err)
	}
	if _, err := lottery.GetComputeJob(ctx, appID+1, one.ID); errCode(err) != errCodeLotteryComputeNotFound {
		t.Fatalf("别的应用的任务应报 40417，得到 %v", err)
	}
	// 规模超限、未保存也没有活动
	huge := request
	huge.Users, huge.Days, huge.DrawsPerDay, huge.Replications = 100_000, 60, 200, 256
	if _, err := lottery.CreateComputeJob(ctx, huge); errCode(err) != 40000 {
		t.Fatalf("规模超限应报 40000，得到 %v", err)
	}
	if _, err := lottery.CreateComputeJob(ctx, LotteryComputeRequest{AppID: appID, Users: 10, Days: 1, DrawsPerDay: 1, Replications: 1}); errCode(err) != 40000 {
		t.Fatalf("没有活动应报 40000，得到 %v", err)
	}

	// ── 逐笔复算审计 ──
	alice := insertTestUser(t, ctx, pool, appID, "compute-alice", "0")
	const draws = 60
	for i := range draws {
		if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: alice, ActivityID: activity.ID,
			RequestID: "replay-" + strconv.Itoa(i)}); err != nil {
			t.Fatalf("第 %d 次抽奖失败：%v", i+1, err)
		}
	}
	audit, err := lottery.Audit(ctx, appID, activity.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	replay := audit.Replay
	if replay.Checked+replay.Skipped != draws || replay.Unseeded != 0 || replay.Matched != replay.Checked || replay.Checked < draws/2 {
		t.Fatalf("真实抽奖应全部复算一致（保底的不复算）：%+v", replay)
	}
	// 改一条记录的结果（方案里的 picked 换成另一个奖品）：统计检验看不出来，复算能指到它
	var victim, picked int64
	if err := pool.QueryRow(ctx, `SELECT id, (trace->>'picked')::bigint FROM lottery_draws
WHERE activity_id = $1 AND trace ? 'seed' ORDER BY id LIMIT 1`, activity.ID).Scan(&victim, &picked); err != nil {
		t.Fatal(err)
	}
	other := saved.Prizes[0].ID
	if other == picked {
		other = saved.Prizes[1].ID
	}
	if _, err := pool.Exec(ctx, `UPDATE lottery_draws SET trace = jsonb_set(trace, '{picked}', to_jsonb($2::bigint)) WHERE id = $1`,
		victim, other); err != nil {
		t.Fatal(err)
	}
	tampered, err := lottery.Audit(ctx, appID, activity.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if tampered.Passed || len(tampered.Replay.Mismatches) != 1 || tampered.Replay.Mismatches[0].DrawID != victim ||
		tampered.Replay.Mismatches[0].Recorded != other || tampered.Replay.Mismatches[0].Replayed != picked {
		t.Fatalf("应指出被改的那一条：%+v", tampered.Replay)
	}
}
