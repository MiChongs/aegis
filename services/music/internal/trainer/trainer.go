// Package trainer 训练任务：从 MUSIC_JOBS 工作队列取任务，读 ClickHouse 交互 → 评估 → 训练 → 存模型 → 按门槛发布。
// 另有定时器按应用配置的 cron 往队列里投任务。
package trainer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/store"
)

// staleAfter 进行中的训练多久没心跳算中断（可被重新认领）。
const staleAfter = 3 * time.Minute

// Trainer 训练执行器。
type Trainer struct {
	PG      *store.PG
	CH      *store.CH
	NC      *nats.Conn
	JS      jetstream.JetStream
	Redis   redis.UniversalClient
	Prefix  string
	Workers int
	Timeout time.Duration
	Log     *zap.Logger
}

// Enqueue 新建一次训练并投进队列（管理端「立即训练」与定时器共用）。已有排队 / 进行中的返回那一次。
func Enqueue(ctx context.Context, pg *store.PG, js jetstream.JetStream, appID int64, trigger, note, operator string) (*store.Run, error) {
	run, err := pg.CreateRun(ctx, appID, trigger, note, operator)
	if errors.Is(err, store.ErrRunActive) {
		return run, nil
	}
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(contract.TrainJob{RunID: run.ID, AppID: appID})
	if _, err := js.Publish(ctx, contract.TrainSubject(appID), payload, jetstream.WithMsgID("train-"+strconv.FormatInt(run.ID, 10))); err != nil {
		// 没投进队列的排队记录会占住「同一时刻一个训练」的位置，直接标记失败让下次能重新发起
		_ = pg.FailRun(context.Background(), run.ID, nil, "训练任务入队失败："+err.Error())
		return nil, fmt.Errorf("训练任务入队失败：%w", err)
	}
	return run, nil
}

// Consume 消费训练队列（阻塞到 ctx 结束）。一个实例同一时刻只跑一个训练。
func (t *Trainer) Consume(ctx context.Context) error {
	cons, err := t.JS.CreateOrUpdateConsumer(ctx, contract.StreamJobs, jetstream.ConsumerConfig{
		Durable:       contract.ConsumerTrainer,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       time.Minute,
		MaxDeliver:    5,
		MaxAckPending: 64,
		FilterSubject: contract.SubjectTrainPrefix + ">",
	})
	if err != nil {
		return fmt.Errorf("创建训练消费者失败：%w", err)
	}
	for ctx.Err() == nil {
		batch, err := cons.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			t.Log.Warn("fetch train job failed", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}
		for msg := range batch.Messages() {
			t.handle(ctx, msg)
		}
	}
	return nil
}

func (t *Trainer) handle(ctx context.Context, msg jetstream.Msg) {
	var job contract.TrainJob
	if err := json.Unmarshal(msg.Data(), &job); err != nil || job.RunID <= 0 {
		_ = msg.Term()
		return
	}
	// 训练期间持续告诉 JetStream「还在做」，避免 AckWait 到期被别的实例重复领走
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				_ = msg.InProgress()
				_ = t.PG.Heartbeat(context.Background(), job.RunID, "")
			}
		}
	}()
	err := t.Run(ctx, job.RunID)
	close(done)
	if err != nil && ctx.Err() != nil {
		// 进程在关停：不确认，由别的实例（或重启后的自己）接着做
		_ = msg.Nak()
		return
	}
	if err != nil {
		t.Log.Warn("train failed", zap.Int64("run", job.RunID), zap.Int64("app", job.AppID), zap.Error(err))
	}
	_ = msg.Ack()
}

// Run 执行一次训练。失败写进训练记录（返回的错误只用于日志）。
func (t *Trainer) Run(ctx context.Context, runID int64) error {
	run, err := t.PG.RunByID(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != store.RunQueued && run.Status != store.RunRunning {
		return nil
	}
	cfg, err := t.PG.Config(ctx, run.AppID)
	if err != nil {
		return err
	}
	ok, err := t.PG.StartRun(ctx, runID, cfg, staleAfter)
	if err != nil || !ok {
		return err
	}
	if t.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.Timeout)
		defer cancel()
	}
	var stages []store.Stage
	stage := func(name string, start time.Time) {
		stages = append(stages, store.Stage{Name: name, Ms: time.Since(start).Milliseconds()})
	}
	logf := func(format string, args ...any) {
		_ = t.PG.Heartbeat(context.Background(), runID, time.Now().Format("15:04:05")+" "+fmt.Sprintf(format, args...))
	}
	fail := func(err error) error {
		logf("失败：%v", err)
		_ = t.PG.FailRun(context.Background(), runID, stages, err.Error())
		return err
	}

	now := time.Now()
	start := time.Now()
	logf("读取近 %d 天的行为数据", cfg.WindowDays)
	builder := model.NewBuilder(cfg, now)
	from := now.Add(-time.Duration(cfg.WindowDays) * 24 * time.Hour)
	if err := t.CH.StreamInteractions(ctx, run.AppID, from, func(r *store.InteractionRow) error {
		builder.Add(r)
		return nil
	}); err != nil {
		return fail(fmt.Errorf("读取行为数据失败：%w", err))
	}
	m := builder.Finish()
	stage("load", start)
	logf("事件 %d 条，听众 %d 人，作品 %d 首，交互 %d 对", builder.Events(), len(m.Users), len(m.Items), m.Interact)
	if len(m.Users) < 2 || len(m.Items) < 2 {
		return fail(fmt.Errorf("数据不足：至少需要 2 位听众与 2 首被多人听过的作品（当前 %d 人、%d 首）", len(m.Users), len(m.Items)))
	}
	seed := uint64(runID)*2654435761 + 1

	start = time.Now()
	metrics, err := model.Evaluate(ctx, m, cfg, t.Workers, seed)
	if err != nil {
		return fail(fmt.Errorf("离线评估失败：%w", err))
	}
	stage("eval", start)
	if metrics != nil {
		logf("离线评估（%d 人）：NDCG@20 %.4f（热度 %.4f），Recall@20 %.4f（热度 %.4f）",
			metrics.TestUsers, metrics.NDCG20, metrics.Baseline.NDCG20, metrics.Recall20, metrics.Baseline.Recall20)
	} else {
		logf("可评估的听众不足 10 人，跳过离线评估")
	}

	snap, err := model.Build(ctx, m, cfg, t.Workers, seed, stage)
	if err != nil {
		return fail(fmt.Errorf("训练失败：%w", err))
	}
	snap.AppID, snap.RunID, snap.CreatedAt = run.AppID, runID, now.UnixMilli()

	// 精排：用近 30 天被展示过的推荐（出推荐时记下的特征）训练逻辑回归，胜过融合分才启用
	var rankInfo *store.RankerInfo
	if cfg.Ranker.Enabled {
		start = time.Now()
		rankInfo = t.trainRanker(ctx, run.AppID, cfg, snap, logf)
		stage("ranker", start)
		if rankInfo != nil && rankInfo.Enabled && metrics != nil {
			metrics.AUC = rankInfo.AUC
		}
	}
	start = time.Now()
	blob, err := snap.Encode()
	if err != nil {
		return fail(fmt.Errorf("模型序列化失败：%w", err))
	}
	if err := t.PG.FinishRun(ctx, runID, metrics, rankInfo, append(stages, store.Stage{Name: "save", Ms: time.Since(start).Milliseconds()}),
		len(m.Users), len(m.Items), m.Interact, blob); err != nil {
		return fail(fmt.Errorf("保存模型失败：%w", err))
	}
	logf("模型已保存（%.1f MB）", float64(len(blob))/(1<<20))

	if !cfg.AutoPublish {
		logf("未开启自动发布，等待手动发布")
	} else if metrics != nil && metrics.NDCG20 < metrics.Baseline.NDCG20*cfg.MinLift {
		logf("NDCG@20 未达到发布门槛（热度 × %.2f），保留为待发布", cfg.MinLift)
	} else {
		if err := Publish(context.Background(), t.PG, t.NC, run.AppID, runID); err != nil {
			logf("自动发布失败：%v", err)
		} else {
			logf("已自动发布")
		}
	}
	if err := t.PG.PruneBlobs(context.Background(), run.AppID, 5); err != nil {
		t.Log.Warn("prune model blobs failed", zap.Error(err))
	}
	return nil
}

// Publish 发布一个版本并通知 music-rec 热加载。
func Publish(ctx context.Context, pg *store.PG, nc *nats.Conn, appID, runID int64) error {
	if err := pg.PublishRun(ctx, appID, runID); err != nil {
		return err
	}
	payload, _ := json.Marshal(contract.ModelPublished{AppID: appID, RunID: runID})
	if nc != nil {
		// 通知丢了也没关系：music-rec 每 MUSIC_MODEL_POLL 会比对一次已发布版本
		_ = nc.Publish(contract.SubjectModelPublished, payload)
	}
	return nil
}

// Schedule 定时器：每分钟看一遍各应用的 cron，到点就入队。多实例下同一应用同一分钟只入队一次（Redis SETNX）。
func (t *Trainer) Schedule(ctx context.Context) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	last := time.Now().Truncate(time.Minute)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			minute := now.Truncate(time.Minute)
			t.tick(ctx, parser, last, minute)
			last = minute
			if n, err := t.PG.FailStaleRuns(ctx, 10*time.Minute); err == nil && n > 0 {
				t.Log.Warn("marked stale training runs failed", zap.Int64("count", n))
			}
		}
	}
}

func (t *Trainer) tick(ctx context.Context, parser cron.Parser, prev, minute time.Time) {
	apps, err := t.PG.ConfiguredApps(ctx)
	if err != nil {
		t.Log.Warn("list music apps failed", zap.Error(err))
		return
	}
	for _, appID := range apps {
		cfg, err := t.PG.Config(ctx, appID)
		if err != nil || !cfg.Enabled || cfg.Schedule == "" {
			continue
		}
		sched, err := parser.Parse(cfg.Schedule)
		if err != nil {
			continue
		}
		// (prev, minute] 之间有触发点 → 这一分钟该触发（ticker 偶尔晚到也不漏）
		if next := sched.Next(prev); next.After(minute) {
			continue
		}
		lock := t.Prefix + "sched:" + strconv.FormatInt(appID, 10) + ":" + minute.UTC().Format("200601021504")
		ok, err := t.Redis.SetNX(ctx, lock, "1", 2*time.Hour).Result()
		if err != nil || !ok {
			continue
		}
		if run, err := Enqueue(ctx, t.PG, t.JS, appID, "schedule", "", "定时任务"); err != nil {
			t.Log.Warn("schedule train failed", zap.Int64("app", appID), zap.Error(err))
		} else {
			t.Log.Info("scheduled train", zap.Int64("app", appID), zap.Int64("run", run.ID))
		}
	}
}

// trainRanker 读样本、训练、把结果挂到快照上；读不到样本或训练失败只记日志，不影响主模型。
func (t *Trainer) trainRanker(ctx context.Context, appID int64, cfg appcfg.Config, snap *model.Snapshot, logf func(string, ...any)) *store.RankerInfo {
	raw, err := t.CH.RankSamples(ctx, appID, 30, 1_000_000)
	if err != nil {
		logf("读取精排样本失败：%v", err)
		return nil
	}
	blend := [5]float32{float32(cfg.Blend.ALS), float32(cfg.Blend.ItemCF), float32(cfg.Blend.Artist), float32(cfg.Blend.Trending), float32(cfg.Blend.Explore)}
	samples := make([]model.Sample, 0, len(raw))
	for _, r := range raw {
		if len(r.Features) != model.NumRankFeatures {
			continue // 特征版本不一致的旧样本
		}
		var base float32
		for k := 0; k < 5; k++ {
			base += blend[k] * r.Features[k]
		}
		samples = append(samples, model.Sample{X: r.Features, Y: r.Label, TS: r.TS, Base: base})
	}
	rk := model.TrainRanker(samples, model.RankerParams{MinSamples: cfg.Ranker.MinSamples, L2: cfg.Ranker.L2, Epochs: cfg.Ranker.Epochs})
	info := &store.RankerInfo{Enabled: rk.Enabled, Samples: rk.Samples, Positives: rk.Positives, AUC: rk.AUC,
		BaselineAUC: rk.BaselineAUC, Reason: rk.Reason, Features: []store.FeatureWeight{}}
	if math.IsNaN(info.AUC) {
		info.AUC = 0
	}
	if math.IsNaN(info.BaselineAUC) {
		info.BaselineAUC = 0
	}
	for i, w := range rk.Weights {
		info.Features = append(info.Features, store.FeatureWeight{Name: model.RankFeatures[i], Weight: float64(w)})
	}
	if rk.Enabled {
		snap.Ranker = rk
		logf("精排：样本 %d 条（正 %d），AUC %.4f，融合分基线 %.4f，已启用", rk.Samples, rk.Positives, rk.AUC, rk.BaselineAUC)
	} else {
		logf("精排未启用：%s", rk.Reason)
	}
	return info
}
