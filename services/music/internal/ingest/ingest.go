// Package ingest music-ingest：消费 MUSIC 流，把事件与推荐结果落到 ClickHouse、曲库与 Redis 实时状态。
//
// 不丢不重：一批消息三处都写成功才逐条确认；任一处失败整批不确认（延迟重投）。重投的同一事件
// 在 ClickHouse 里按 (app_id, user_id, event_id) 去重、曲库是 upsert、实时状态是按时刻比较的幂等写，
// 所以「写了一半崩溃」只会导致重写，不会多算。解析不了的消息直接终止投递；投递多次仍失败的转进死信主题。
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/signal"
	"aegis/services/music/internal/store"
)

const maxDeliver = 10

// Ingest 落地消费者。
type Ingest struct {
	JS    jetstream.JetStream
	CH    *store.CH
	PG    *store.PG
	RT    *realtime.Store
	Batch int
	Wait  time.Duration
	Log   *zap.Logger

	Metrics *Metrics

	cfgMu sync.Mutex
	cfgs  map[int64]cfgEntry
}

type cfgEntry struct {
	w        appcfg.Weights
	suppress int
	at       time.Time
}

// Metrics 落地指标。
type Metrics struct {
	Events   *prometheus.CounterVec
	Batches  prometheus.Counter
	Failures *prometheus.CounterVec
	Latency  prometheus.Histogram
}

// NewMetrics 注册指标。
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Events:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "music_ingest_messages_total", Help: "落地的消息数"}, []string{"kind"}),
		Batches:  prometheus.NewCounter(prometheus.CounterOpts{Name: "music_ingest_batches_total", Help: "写成功的批次"}),
		Failures: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "music_ingest_failures_total", Help: "失败（按阶段）"}, []string{"stage"}),
		Latency:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "music_ingest_batch_seconds", Help: "一批的写入耗时", Buckets: prometheus.DefBuckets}),
	}
	reg.MustRegister(m.Events, m.Batches, m.Failures, m.Latency)
	return m
}

// Run 阻塞消费到 ctx 结束。
func (in *Ingest) Run(ctx context.Context) error {
	cons, err := in.JS.CreateOrUpdateConsumer(ctx, contract.StreamEvents, jetstream.ConsumerConfig{
		Durable:        contract.ConsumerIngest,
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        time.Minute,
		MaxDeliver:     maxDeliver + 5,
		MaxAckPending:  max(in.Batch*4, 2000),
		FilterSubjects: []string{contract.SubjectEventPrefix + ">", contract.SubjectServedPrefix + ">"},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("创建落地消费者失败：%w", err)
	}
	for ctx.Err() == nil {
		batch, err := cons.Fetch(in.Batch, jetstream.FetchMaxWait(in.Wait))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			in.Log.Warn("fetch failed", zap.Error(err))
			time.Sleep(time.Second)
			continue
		}
		var msgs []jetstream.Msg
		for msg := range batch.Messages() {
			msgs = append(msgs, msg)
		}
		if err := batch.Error(); err != nil && ctx.Err() == nil && len(msgs) == 0 {
			in.Log.Debug("fetch batch ended", zap.Error(err))
		}
		if len(msgs) > 0 {
			in.process(ctx, msgs)
		}
	}
	return nil
}

func (in *Ingest) process(ctx context.Context, msgs []jetstream.Msg) {
	begin := time.Now()
	var events []contract.Event
	var served []contract.Served
	var ok []jetstream.Msg
	for _, msg := range msgs {
		subject := msg.Subject()
		switch {
		case strings.HasPrefix(subject, contract.SubjectEventPrefix):
			var e contract.Event
			if err := json.Unmarshal(msg.Data(), &e); err != nil || e.ID == "" || e.AppID <= 0 || e.UserID <= 0 || !contract.ValidType(e.Type) {
				in.poison(msg, "event")
				continue
			}
			events = append(events, e)
		case strings.HasPrefix(subject, contract.SubjectServedPrefix):
			var s contract.Served
			if err := json.Unmarshal(msg.Data(), &s); err != nil || s.RecID == "" || s.AppID <= 0 {
				in.poison(msg, "served")
				continue
			}
			served = append(served, s)
		default:
			_ = msg.Term()
			continue
		}
		ok = append(ok, msg)
	}
	if len(ok) == 0 {
		return
	}
	if err := in.write(ctx, events, served); err != nil {
		in.Log.Warn("write batch failed, will redeliver", zap.Int("messages", len(ok)), zap.Error(err))
		for _, msg := range ok {
			in.retryOrDead(msg)
		}
		return
	}
	for _, msg := range ok {
		if err := msg.DoubleAck(ctx); err != nil {
			// 确认丢了只会导致重投，重投的写入是幂等的
			in.Log.Debug("ack failed", zap.Error(err))
		}
	}
	if in.Metrics != nil {
		in.Metrics.Batches.Inc()
		in.Metrics.Events.WithLabelValues("event").Add(float64(len(events)))
		in.Metrics.Events.WithLabelValues("served").Add(float64(len(served)))
		in.Metrics.Latency.Observe(time.Since(begin).Seconds())
	}
}

func (in *Ingest) write(ctx context.Context, events []contract.Event, served []contract.Served) error {
	wctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := in.CH.InsertEvents(wctx, events); err != nil {
		in.fail("clickhouse_events")
		return fmt.Errorf("ClickHouse 写事件：%w", err)
	}
	if err := in.CH.InsertServed(wctx, served); err != nil {
		in.fail("clickhouse_served")
		return fmt.Errorf("ClickHouse 写推荐结果：%w", err)
	}
	tracks := map[int64]map[string]store.TrackRow{}
	for i := range events {
		e := &events[i]
		// 没有音源的（解析失败的大模型推荐）不进曲库，免得把同一作品已有的音源版本覆盖成空
		if e.Track == nil || e.WorkKey == "" || e.Track.Source == "" || e.Track.SongID == "" {
			continue
		}
		byApp := tracks[e.AppID]
		if byApp == nil {
			byApp = map[string]store.TrackRow{}
			tracks[e.AppID] = byApp
		}
		at := time.UnixMilli(e.TS)
		row, exists := byApp[e.WorkKey]
		if exists && row.LastSeen.After(at) {
			continue
		}
		first := at
		if exists && row.FirstSeen.Before(first) {
			first = row.FirstSeen
		}
		byApp[e.WorkKey] = store.TrackRow{WorkKey: e.WorkKey, Track: *e.Track, FirstSeen: first, LastSeen: at}
	}
	if err := in.PG.UpsertTracks(wctx, tracks); err != nil {
		in.fail("pg_tracks")
		return fmt.Errorf("写曲库：%w", err)
	}
	if err := in.RT.Apply(wctx, events, in.params(wctx)); err != nil {
		in.fail("redis_realtime")
		return fmt.Errorf("写实时状态：%w", err)
	}
	return nil
}

// params 应用的事件权重与暂不推荐天数（配置缓存一分钟）。
func (in *Ingest) params(ctx context.Context) func(appID int64) realtime.Params {
	return func(appID int64) realtime.Params {
		in.cfgMu.Lock()
		if in.cfgs == nil {
			in.cfgs = map[int64]cfgEntry{}
		}
		entry, ok := in.cfgs[appID]
		in.cfgMu.Unlock()
		if !ok || time.Since(entry.at) > time.Minute {
			cfg, err := in.PG.Config(ctx, appID)
			if err != nil {
				cfg = appcfg.Default()
			}
			entry = cfgEntry{w: cfg.Weights, suppress: cfg.SuppressDays, at: time.Now()}
			in.cfgMu.Lock()
			in.cfgs[appID] = entry
			in.cfgMu.Unlock()
		}
		w := entry.w
		return realtime.Params{Weight: func(k signal.Kind) float64 { return signal.Weight(k, w) }, SuppressDays: entry.suppress}
	}
}

func (in *Ingest) fail(stage string) {
	if in.Metrics != nil {
		in.Metrics.Failures.WithLabelValues(stage).Inc()
	}
}

func (in *Ingest) poison(msg jetstream.Msg, kind string) {
	in.fail("decode_" + kind)
	in.Log.Warn("drop undecodable message", zap.String("subject", msg.Subject()))
	_ = msg.Term()
}

// retryOrDead 延迟重投；投递次数到上限的转进死信主题（保留在 MUSIC 流里 7 天，可人工重放）。
func (in *Ingest) retryOrDead(msg jetstream.Msg) {
	meta, err := msg.Metadata()
	if err == nil && meta.NumDelivered >= maxDeliver {
		app := strings.TrimPrefix(strings.TrimPrefix(msg.Subject(), contract.SubjectEventPrefix), contract.SubjectServedPrefix)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, perr := in.JS.Publish(ctx, contract.SubjectDeadPrefix+app, msg.Data()); perr == nil {
			in.fail("dead_letter")
			_ = msg.Term()
			return
		}
	}
	delay := time.Duration(5) * time.Second
	if err == nil {
		delay = min(time.Duration(meta.NumDelivered)*5*time.Second, time.Minute)
	}
	_ = msg.NakWithDelay(delay)
}
