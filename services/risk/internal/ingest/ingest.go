// Package ingest risk-ingest：把 JetStream 上的事件落到各个存储，并执行风控侧的处置命令。
//
// 四个持久消费者，各自批量取、批量写、全部写成功才确认；任何一步失败整批 Nak，稍后重投。
// 每个落地都是幂等的（ClickHouse 按事件 ID 合并、Nebula / Milvus UPSERT、PG 按 ID 冲突忽略），
// 所以重投不会产生重复数据；只有图上的边计数会多加，判定不用它。
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/analytics"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/vector"

	"github.com/nats-io/nats.go/jetstream"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// Worker 依赖
type Worker struct {
	Bus    *events.Bus
	CH     *analytics.Client
	Graph  *graph.Client
	Vector *vector.Client
	Store  *store.Store
	Lists  *lists.Index
	Etcd   *clientv3.Client
	Logf   func(string, ...any)

	BatchSize int
	MaxWait   time.Duration

	patMu    sync.RWMutex
	patterns map[string]bundle.CEPPattern
	patAt    time.Time
}

type consumerSpec struct {
	name    string
	stream  string
	filters []string
	handle  func(context.Context, []jetstream.Msg) error
}

// Run 跑全部消费者与后台任务，直到 ctx 结束
func (w *Worker) Run(ctx context.Context) error {
	if w.BatchSize <= 0 {
		w.BatchSize = 500
	}
	if w.MaxWait <= 0 {
		w.MaxWait = time.Second
	}
	specs := []consumerSpec{
		{name: "risk-ingest-decisions", stream: events.StreamEvents, filters: []string{events.SubjectDecisionPrefix + ">"}, handle: w.handleDecisions},
		{name: "risk-ingest-events", stream: events.StreamEvents, filters: []string{events.SubjectEventPrefix + ">"}, handle: w.handleEvents},
		{name: "risk-ingest-alerts", stream: events.StreamEvents, filters: []string{events.SubjectAlertPrefix + ">"}, handle: w.handleAlerts},
		{name: "risk-ingest-commands", stream: events.StreamCommands, filters: []string{events.CommandSubject("list_add"), events.CommandSubject("list_remove")}, handle: w.handleCommands},
	}
	var wg sync.WaitGroup
	errCh := make(chan error, len(specs))
	for _, sp := range specs {
		cons, err := w.Bus.JetStream().CreateOrUpdateConsumer(ctx, sp.stream, jetstream.ConsumerConfig{
			Durable: sp.name, FilterSubjects: sp.filters, AckPolicy: jetstream.AckExplicitPolicy,
			AckWait: 2 * time.Minute, MaxAckPending: w.BatchSize * 4, MaxDeliver: -1, DeliverPolicy: jetstream.DeliverAllPolicy,
		})
		if err != nil {
			return fmt.Errorf("消费者 %s：%w", sp.name, err)
		}
		wg.Add(1)
		go func(sp consumerSpec, cons jetstream.Consumer) {
			defer wg.Done()
			errCh <- w.loop(ctx, sp, cons)
		}(sp, cons)
	}
	wg.Add(1)
	go func() { defer wg.Done(); w.sweepLoop(ctx) }()
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return nil
}

func (w *Worker) loop(ctx context.Context, sp consumerSpec, cons jetstream.Consumer) error {
	backoff := time.Second
	for ctx.Err() == nil {
		batch, err := cons.Fetch(w.BatchSize, jetstream.FetchMaxWait(w.MaxWait))
		if err != nil {
			w.logf("%s 取消息失败：%v", sp.name, err)
			sleep(ctx, backoff)
			continue
		}
		var msgs []jetstream.Msg
		for m := range batch.Messages() {
			msgs = append(msgs, m)
		}
		if len(msgs) == 0 {
			continue
		}
		hctx, cancel := context.WithTimeout(ctx, time.Minute)
		err = sp.handle(hctx, msgs)
		cancel()
		if err != nil {
			w.logf("%s 处理 %d 条失败，稍后重投：%v", sp.name, len(msgs), err)
			for _, m := range msgs {
				_ = m.NakWithDelay(backoff)
			}
			backoff = min(backoff*2, 30*time.Second)
			continue
		}
		backoff = time.Second
		for _, m := range msgs {
			_ = m.Ack()
		}
	}
	return ctx.Err()
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (w *Worker) logf(format string, args ...any) {
	if w.Logf != nil {
		w.Logf(format, args...)
	}
}

func decode(msgs []jetstream.Msg) []events.Event {
	out := make([]events.Event, 0, len(msgs))
	for _, m := range msgs {
		var e events.Event
		if err := json.Unmarshal(m.Data(), &e); err != nil || e.ID == "" {
			continue // 坏消息确认掉，不阻塞后面的
		}
		// 时间戳缺失或离谱（早于 2020 年）时用入流时间，否则 ClickHouse 的 TTL 会当场把它删掉
		if e.TS < 1577836800000 {
			if md, err := m.Metadata(); err == nil {
				e.TS = md.Timestamp.UnixMilli()
			} else {
				e.TS = time.Now().UnixMilli()
			}
		}
		out = append(out, e)
	}
	return out
}

func (w *Worker) handleDecisions(ctx context.Context, msgs []jetstream.Msg) error {
	evs := decode(msgs)
	if err := w.CH.WriteDecisions(ctx, evs); err != nil {
		return fmt.Errorf("clickhouse：%w", err)
	}
	live := make([]events.Event, 0, len(evs))
	for _, e := range evs {
		if !e.Shadow {
			live = append(live, e)
		}
	}
	if err := w.Graph.Apply(live); err != nil {
		return fmt.Errorf("nebula：%w", err)
	}
	// 设备向量：同一批里同一台设备只留最新一条
	latest := map[string]vector.Device{}
	for _, e := range live {
		if e.DeviceID == "" || len(e.Device) == 0 {
			continue
		}
		vec := vector.Embed(e.Device)
		if vector.Empty(vec) {
			continue
		}
		k := vector.Key(e.AppID, e.DeviceID)
		if cur, ok := latest[k]; !ok || cur.UpdatedAt <= e.TS {
			latest[k] = vector.Device{AppID: e.AppID, DeviceID: e.DeviceID, UserID: e.UserID, UpdatedAt: e.TS, Vec: vec,
				Banned: e.Action == bundle.ActionBan}
		}
	}
	if len(latest) > 0 {
		rows := make([]vector.Device, 0, len(latest))
		for _, d := range latest {
			// 封禁标记以名单为准：设备已在黑名单里的，后续评估写向量时不能把标记冲掉
			if !d.Banned {
				if es, err := w.Lists.Exact(ctx, bundle.ListDevice, d.DeviceID); err == nil {
					now := time.Now()
					for _, e := range es {
						if e.Kind == bundle.KindBlock && e.Active(now) {
							d.Banned = true
							break
						}
					}
				}
			}
			rows = append(rows, d)
		}
		if err := w.Vector.Upsert(ctx, rows); err != nil {
			return fmt.Errorf("milvus：%w", err)
		}
	}
	var cases []store.Case
	for _, e := range live {
		if e.Action == bundle.ActionReview {
			cases = append(cases, store.Case{DecisionID: e.ID, AppID: e.AppID, Scene: e.Scene, UserID: e.UserID, Account: e.Account, IP: e.IP,
				DeviceID: e.DeviceID, Action: e.Action, Score: e.Score, Reasons: e.Reasons, Source: "decision", CreatedAt: time.UnixMilli(e.TS).UTC()})
		}
	}
	return w.Store.InsertCases(ctx, cases)
}

func (w *Worker) handleEvents(ctx context.Context, msgs []jetstream.Msg) error {
	evs := decode(msgs)
	if err := w.CH.WriteEvents(ctx, evs); err != nil {
		return fmt.Errorf("clickhouse：%w", err)
	}
	if err := w.Graph.Apply(evs); err != nil {
		return fmt.Errorf("nebula：%w", err)
	}
	return nil
}

// pattern 模式定义缓存（30 秒）
func (w *Worker) pattern(ctx context.Context, key string) (bundle.CEPPattern, bool) {
	w.patMu.RLock()
	fresh := time.Since(w.patAt) < 30*time.Second
	p, ok := w.patterns[key]
	w.patMu.RUnlock()
	if fresh && ok {
		return p, true
	}
	ps, err := w.Store.ListPatterns(ctx, false)
	if err != nil {
		return p, ok
	}
	m := map[string]bundle.CEPPattern{}
	for _, x := range ps {
		m[x.Key] = x.CEPPattern
	}
	w.patMu.Lock()
	w.patterns, w.patAt = m, time.Now()
	w.patMu.Unlock()
	p, ok = m[key]
	return p, ok
}

func (w *Worker) handleAlerts(ctx context.Context, msgs []jetstream.Msg) error {
	var alerts []analytics.Alert
	for _, m := range msgs {
		var a analytics.Alert
		if err := json.Unmarshal(m.Data(), &a); err == nil && a.ID != "" {
			alerts = append(alerts, a)
		}
	}
	if err := w.CH.WriteAlerts(ctx, alerts); err != nil {
		return fmt.Errorf("clickhouse：%w", err)
	}
	var entries []lists.Entry
	var cases []store.Case
	now := time.Now().UTC()
	for _, a := range alerts {
		p, ok := w.pattern(ctx, a.Pattern)
		if !ok {
			continue
		}
		for _, act := range p.Actions {
			switch act.Kind {
			case "case":
				cases = append(cases, store.Case{DecisionID: "cep:" + a.ID, AppID: a.AppID, Scene: "cep:" + a.Pattern, UserID: a.UserID,
					IP: a.IP, DeviceID: a.DeviceID, Action: bundle.ActionReview, Reasons: []string{"行为模式：" + p.Name}, Source: "cep", CreatedAt: time.UnixMilli(a.TS).UTC()})
			case "list_add":
				var listType, value string
				appID := int64(0)
				switch act.Target {
				case "device":
					listType, value = bundle.ListDevice, a.DeviceID
				case "ip":
					listType, value = bundle.ListIP, a.IP
				case "user":
					if a.UserID > 0 {
						listType, value, appID = bundle.ListUser, strconv.FormatInt(a.UserID, 10), a.AppID
					}
				case "account":
					if a.Entity == bundle.EntityAccount {
						listType, value, appID = bundle.ListAccount, a.Value, a.AppID
					}
				}
				if value == "" {
					continue
				}
				v, err := lists.Normalize(listType, value)
				if err != nil {
					continue
				}
				e := lists.Entry{ID: "cep:" + a.ID + ":" + act.Target, ListType: listType, Value: v, Kind: act.ListKind, AppID: appID,
					Reason: "行为模式：" + p.Name, Source: "cep", CreatedAt: now, DecisionID: a.ID}
				if act.DurationSeconds > 0 {
					exp := now.Add(time.Duration(act.DurationSeconds) * time.Second)
					e.ExpiresAt = &exp
				}
				entries = append(entries, e)
			}
		}
	}
	if err := w.Store.InsertCases(ctx, cases); err != nil {
		return err
	}
	return w.persistEntries(ctx, entries)
}

// persistEntries 先落库再写索引与图、向量上的封禁标记
func (w *Worker) persistEntries(ctx context.Context, es []lists.Entry) error {
	if len(es) == 0 {
		return nil
	}
	if err := w.Store.PutEntries(ctx, es); err != nil {
		return fmt.Errorf("postgres：%w", err)
	}
	if err := w.Lists.Put(ctx, es); err != nil {
		return fmt.Errorf("名单索引：%w", err)
	}
	return w.markBanned(ctx, es, true)
}

func (w *Worker) markBanned(ctx context.Context, es []lists.Entry, banned bool) error {
	for _, e := range es {
		if e.Kind != bundle.KindBlock {
			continue
		}
		switch e.ListType {
		case bundle.ListDevice:
			if err := w.Graph.SetBanned(graph.DeviceVID(e.Value), banned); err != nil {
				return err
			}
			if e.AppID > 0 {
				if err := w.Vector.SetBanned(ctx, e.AppID, e.Value, banned); err != nil {
					return err
				}
			}
		case bundle.ListIP:
			if err := w.Graph.SetBanned(graph.IPVID(e.Value), banned); err != nil {
				return err
			}
		case bundle.ListUser:
			if uid, err := strconv.ParseInt(e.Value, 10, 64); err == nil && e.AppID > 0 {
				if err := w.Graph.SetBanned(graph.UserVID(e.AppID, uid), banned); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

var targetList = map[string]string{"device": bundle.ListDevice, "ip": bundle.ListIP, "account": bundle.ListAccount, "user": bundle.ListUser}

func (w *Worker) handleCommands(ctx context.Context, msgs []jetstream.Msg) error {
	var adds []lists.Entry
	var records []analytics.CommandRecord
	now := time.Now().UTC()
	for _, m := range msgs {
		var c events.Command
		if err := json.Unmarshal(m.Data(), &c); err != nil || c.ID == "" {
			continue
		}
		listType := targetList[c.Target]
		if listType == "" {
			records = append(records, analytics.CommandRecord{Command: c, Status: "skipped", Detail: "对象类型不支持"})
			continue
		}
		value, err := lists.Normalize(listType, c.Value)
		if err != nil {
			records = append(records, analytics.CommandRecord{Command: c, Status: "skipped", Detail: err.Error()})
			continue
		}
		switch c.Kind {
		case "list_add":
			e := lists.Entry{ID: c.ID, ListType: listType, Value: value, Kind: bundle.KindBlock, Reason: c.Reason, Source: c.Source,
				CreatedBy: c.Operator, DecisionID: c.DecisionID, CreatedAt: time.UnixMilli(c.TS).UTC()}
			if listType == bundle.ListAccount || listType == bundle.ListUser || listType == bundle.ListDevice {
				e.AppID = c.AppID
			}
			if listType == bundle.ListDevice {
				// 设备名单全平台生效；AppID 只用于标记向量库里那一行
				e.AppID = 0
			}
			if c.DurationSeconds > 0 {
				exp := time.UnixMilli(c.TS).UTC().Add(time.Duration(c.DurationSeconds) * time.Second)
				e.ExpiresAt = &exp
			}
			adds = append(adds, e)
			if listType == bundle.ListDevice && c.AppID > 0 {
				if err := w.Vector.SetBanned(ctx, c.AppID, value, true); err != nil {
					return fmt.Errorf("milvus：%w", err)
				}
			}
			records = append(records, analytics.CommandRecord{Command: c, Status: "ok"})
		case "list_remove":
			existing, err := w.Store.EntriesFor(ctx, listType, value)
			if err != nil {
				return err
			}
			ids := make([]string, 0, len(existing))
			for _, e := range existing {
				if e.AppID == 0 || e.AppID == c.AppID {
					ids = append(ids, e.ID)
				}
			}
			removed, err := w.Store.RemoveEntries(ctx, ids, c.Operator)
			if err != nil {
				return err
			}
			if err := w.Lists.Remove(ctx, removed); err != nil {
				return err
			}
			if err := w.markBanned(ctx, removed, false); err != nil {
				return err
			}
			records = append(records, analytics.CommandRecord{Command: c, Status: "ok", Detail: fmt.Sprintf("移除 %d 条", len(removed))})
		}
		_ = now
	}
	if err := w.persistEntries(ctx, adds); err != nil {
		return err
	}
	return w.CH.WriteCommands(ctx, records)
}

// sweepLoop 每 5 分钟清一次过期名单；多实例经 etcd 选一个来做
func (w *Worker) sweepLoop(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.sweep(ctx); err != nil {
				w.logf("过期名单清理失败：%v", err)
			}
		}
	}
}

func (w *Worker) sweep(ctx context.Context) error {
	sess, err := concurrency.NewSession(w.Etcd, concurrency.WithTTL(60))
	if err != nil {
		return err
	}
	defer sess.Close()
	mu := concurrency.NewMutex(sess, "/aegis/risk/locks/list-sweep")
	if err := mu.TryLock(ctx); err != nil {
		if errors.Is(err, concurrency.ErrLocked) {
			return nil
		}
		return err
	}
	defer mu.Unlock(context.WithoutCancel(ctx))
	for {
		es, err := w.Store.ExpiredEntries(ctx, 1000)
		if err != nil || len(es) == 0 {
			return err
		}
		ids := make([]string, len(es))
		for i, e := range es {
			ids[i] = e.ID
		}
		removed, err := w.Store.RemoveEntries(ctx, ids, "expiry")
		if err != nil {
			return err
		}
		if err := w.Lists.Remove(ctx, removed); err != nil {
			return err
		}
		if err := w.markBanned(ctx, removed, false); err != nil {
			return err
		}
		w.logf("清理过期名单 %d 条", len(removed))
	}
}
