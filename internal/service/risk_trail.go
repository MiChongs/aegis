package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"

	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
)

// riskTrail 风控评估的留痕：经 JetStream 可靠地异步写库，一条不丢（docs/database.md「风控留痕」）。
//
// 请求路径：评估完把留痕同步发布到 JetStream 流 AEGIS_RISK_TRAIL（文件存储，服务端落盘后才回确认；带事件 ID，
// 两分钟内重复发布的同一条只存一份）。发布要不到确认（NATS 不可用、流满）时退回直接写库，不丢弃。
//
// 写库：每个 API 实例跑 riskTrailWorkers 个持久拉取消费者（同一个 durable，工作队列：一条消息只给一个实例），
// 一次取一批，在一个 Postgres 事务里写完（pgrepo.WriteRiskTrail：去重 → 记录 → 合并计数），提交之后才逐条确认。
// 写库失败的整批延迟重投；进程在提交前崩溃，没确认的消息在 AckWait 之后重投；提交后、确认前崩溃，重投的那批在
// 去重表里全部撞上，什么都不做。不限重投次数（MaxDeliver -1），只有解不开的消息（代码错误）会被终止并记错误日志。
type riskTrail struct {
	pg       *pgrepo.Repository
	log      *zap.Logger
	remember func(ctx context.Context, fact riskAccountFact)

	js    jetstream.JetStream
	ready atomic.Bool

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	lastWarnAt atomic.Int64
}

// riskTrailEvent 流里的一条消息：留痕本身，加上账号风险事实（Redis 里的近期高风险次数、上次归属地）。
type riskTrailEvent struct {
	pgrepo.RiskTrailRecord
	Fact riskAccountFact `json:"fact"`
}

// riskAccountFact 写进 Redis 的账号风险事实（与 rememberAccountFacts 同一口径）。
type riskAccountFact struct {
	Subject    string `json:"subject,omitempty"`
	Level      string `json:"level,omitempty"`
	GeoKnown   bool   `json:"geoKnown,omitempty"`
	GeoCountry string `json:"geoCountry,omitempty"`
}

const (
	riskTrailStream   = "AEGIS_RISK_TRAIL"
	riskTrailSubject  = "risk.trail"
	riskTrailConsumer = "risk-trail-writer"
	// riskTrailMaxBytes 流的容量上限：写库长时间停摆时积压到这里，之后的发布被拒、退回直接写库
	riskTrailMaxBytes = 2 << 30
	riskTrailWorkers  = 2
	riskTrailBatch    = 500
	// riskTrailFetchWait 一次取批最多等多久（攒不满一批也按时写）
	riskTrailFetchWait = 500 * time.Millisecond
	// riskTrailAckWait 取走之后多久没确认就重投（要比一批写库的最长耗时长得多）
	riskTrailAckWait        = time.Minute
	riskTrailPublishTimeout = 2 * time.Second
	riskTrailWriteTimeout   = 30 * time.Second
	riskTrailRetryDelay     = 5 * time.Second
	// riskTrailDedupeRetention 去重记录保留多久（远长于任何重投间隔）
	riskTrailDedupeRetention = 8 * 24 * time.Hour
)

func newRiskTrail(pg *pgrepo.Repository, log *zap.Logger, remember func(ctx context.Context, fact riskAccountFact)) *riskTrail {
	if log == nil {
		log = zap.NewNop()
	}
	return &riskTrail{pg: pg, log: log, remember: remember}
}

// attach 接上 NATS（bootstrap 调用）。没接上时留痕直接写库。
func (t *riskTrail) attach(conn *nats.Conn) error {
	if conn == nil {
		return nil
	}
	js, err := jetstream.New(conn)
	if err != nil {
		return err
	}
	t.js = js
	return nil
}

// streamConfig 留痕流：工作队列（确认即删）、文件存储、满了拒绝新消息（不丢旧的）、两分钟去重窗口、S2 压缩。
func riskTrailStreamConfig() jetstream.StreamConfig {
	return jetstream.StreamConfig{
		Name:        riskTrailStream,
		Description: "风控评估留痕（写库前的可靠队列）",
		Subjects:    []string{riskTrailSubject},
		Retention:   jetstream.WorkQueuePolicy,
		Storage:     jetstream.FileStorage,
		Discard:     jetstream.DiscardNew,
		MaxBytes:    riskTrailMaxBytes,
		MaxMsgSize:  1 << 20,
		Duplicates:  2 * time.Minute,
		Compression: jetstream.S2Compression,
		Replicas:    1,
	}
}

func riskTrailConsumerConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:         riskTrailConsumer,
		Description:     "风控留痕写库",
		AckPolicy:       jetstream.AckExplicitPolicy,
		AckWait:         riskTrailAckWait,
		MaxDeliver:      -1,
		MaxAckPending:   riskTrailBatch * riskTrailWorkers * 8,
		MaxRequestBatch: riskTrailBatch,
		FilterSubject:   riskTrailSubject,
	}
}

// start 建流与消费者并启动写库协程。建不起来时每 30 秒重试；在此之前留痕直接写库。
func (t *riskTrail) start(parent context.Context) {
	if t.js == nil {
		return
	}
	t.mu.Lock()
	if t.cancel != nil {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	t.cancel = cancel
	t.mu.Unlock()

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		var consumer jetstream.Consumer
		for {
			var err error
			if consumer, err = t.setup(ctx); err == nil {
				break
			}
			t.log.Error("风控留痕流不可用，留痕暂时直接写库", zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
		t.ready.Store(true)
		for range riskTrailWorkers {
			t.wg.Add(1)
			go func() {
				defer t.wg.Done()
				t.consume(ctx, consumer)
			}()
		}
		t.purgeLoop(ctx)
	}()
}

func (t *riskTrail) setup(ctx context.Context) (jetstream.Consumer, error) {
	setupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stream, err := t.js.CreateOrUpdateStream(setupCtx, riskTrailStreamConfig())
	if err != nil {
		return nil, err
	}
	return stream.CreateOrUpdateConsumer(setupCtx, riskTrailConsumerConfig())
}

// stop 停止取新批次；正在写的那一批写完、确认完才返回（ctx 到点就不再等）。没取走的消息留在流里，下次启动接着写。
func (t *riskTrail) stop(ctx context.Context) {
	t.mu.Lock()
	cancel := t.cancel
	t.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// record 记一条留痕：发布到 JetStream 拿到确认就返回；拿不到时直接写库。
func (t *riskTrail) record(ctx context.Context, event riskTrailEvent) {
	if event.EventID == "" {
		event.EventID = newRiskTrailID()
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	if t.js != nil && t.ready.Load() {
		data, err := json.Marshal(event)
		if err == nil {
			pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), riskTrailPublishTimeout)
			_, err = t.js.PublishMsg(pubCtx, &nats.Msg{Subject: riskTrailSubject, Data: data},
				jetstream.WithMsgID(event.EventID), jetstream.WithExpectStream(riskTrailStream))
			cancel()
			if err == nil {
				return
			}
		}
		t.warnLimited("风控留痕发布失败，改为直接写库", err)
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), riskTrailWriteTimeout)
	defer cancel()
	if err := t.write(writeCtx, []riskTrailEvent{event}); err != nil {
		// 最后的退路：整条留痕进错误日志，能从日志里补回来
		raw, _ := json.Marshal(event)
		t.log.Error("风控留痕写库失败", zap.Error(err), zap.ByteString("trail", raw))
	}
}

// write 写一批（与消费者同一个事务逻辑），之后把第一次见到的那些账号风险事实写进 Redis。
func (t *riskTrail) write(ctx context.Context, events []riskTrailEvent) error {
	records := make([]pgrepo.RiskTrailRecord, len(events))
	for index := range events {
		records[index] = events[index].RiskTrailRecord
	}
	fresh, err := t.pg.WriteRiskTrail(ctx, records)
	if err != nil {
		return err
	}
	if t.remember != nil {
		for _, event := range events {
			if fresh[event.EventID] {
				t.remember(ctx, event.Fact)
			}
		}
	}
	return nil
}

func (t *riskTrail) consume(ctx context.Context, consumer jetstream.Consumer) {
	for ctx.Err() == nil {
		batch, err := consumer.Fetch(riskTrailBatch, jetstream.FetchMaxWait(riskTrailFetchWait))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.warnLimited("取风控留痕失败", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		msgs := make([]jetstream.Msg, 0, riskTrailBatch)
		for msg := range batch.Messages() {
			msgs = append(msgs, msg)
		}
		if err := batch.Error(); err != nil && !errors.Is(err, nats.ErrTimeout) && ctx.Err() == nil {
			t.warnLimited("取风控留痕的批次中断", err)
		}
		if len(msgs) > 0 {
			t.handle(msgs)
		}
	}
}

// handle 写一批并确认。用独立的上下文：退出时也要把已经取到的这一批写完、确认完。
func (t *riskTrail) handle(msgs []jetstream.Msg) {
	events := make([]riskTrailEvent, 0, len(msgs))
	good := make([]jetstream.Msg, 0, len(msgs))
	for _, msg := range msgs {
		var event riskTrailEvent
		if err := json.Unmarshal(msg.Data(), &event); err != nil || event.EventID == "" {
			t.log.Error("风控留痕消息无法解析，已终止投递", zap.Error(err), zap.ByteString("data", msg.Data()))
			_ = msg.TermWithReason("undecodable")
			continue
		}
		events = append(events, event)
		good = append(good, msg)
	}
	if len(events) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), riskTrailWriteTimeout)
	defer cancel()
	if err := t.write(ctx, events); err != nil {
		t.log.Warn("风控留痕写库失败，稍后重投", zap.Int("events", len(events)), zap.Error(err))
		for _, msg := range good {
			_ = msg.NakWithDelay(riskTrailRetryDelay)
		}
		return
	}
	for _, msg := range good {
		// 确认失败时消息会在 AckWait 之后重投，去重表兜住
		_ = msg.Ack()
	}
}

func (t *riskTrail) purgeLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := t.pg.PurgeRiskTrailEvents(ctx, time.Now().Add(-riskTrailDedupeRetention)); err != nil && ctx.Err() == nil {
				t.log.Warn("清理风控留痕去重记录失败", zap.Error(err))
			}
		}
	}
}

func (t *riskTrail) warnLimited(message string, err error) {
	now := time.Now().UnixNano()
	last := t.lastWarnAt.Load()
	if now-last > int64(10*time.Second) && t.lastWarnAt.CompareAndSwap(last, now) {
		t.log.Warn(message, zap.Error(err))
	}
}

func newRiskTrailID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	return uuid.NewString()
}

// riskTrailEventOf 把一次评估整理成留痕消息。
func riskTrailEventOf(assessment securitydomain.RiskAssessment, env *RiskEvalEnv, d riskDecision) riskTrailEvent {
	event := riskTrailEvent{RiskTrailRecord: pgrepo.RiskTrailRecord{
		EventID: newRiskTrailID(), At: time.Now(), Assessment: assessment,
	}}
	for _, rule := range d.matched {
		if rule.RuleID > 0 {
			event.RuleIDs = append(event.RuleIDs, rule.RuleID)
		}
	}
	for _, entry := range d.listEntries {
		event.ListIDs = append(event.ListIDs, entry.ID)
	}
	if env != nil {
		event.Country, event.Region, event.ISP, event.ASN = env.GeoCountry, env.GeoRegion, env.IPISP, env.IPASN
		if assessment.DeviceID != "" {
			event.Fingerprint = map[string]any{
				"uaBrowser": env.UABrowser, "uaBrowserVer": env.UABrowserVersion, "uaOs": env.UAOS,
				"uaOsVersion": env.UAOSVersion, "uaDevice": env.UADevice, "uaDeviceClass": env.UADeviceClass,
				"lastScene": assessment.Scene, "lastCountry": env.GeoCountry,
			}
		}
		event.Fact = riskAccountFact{Subject: riskSubjectKey(env), Level: d.level, GeoKnown: env.GeoKnown,
			GeoCountry: env.GeoCountry}
	}
	return event
}
