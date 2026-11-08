package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	bolt "go.etcd.io/bbolt"
)

// StreamMaxBytes RISK 流的容量上限：RISK_STREAM_MAX_BYTES（字节数，或带 K / M / G 后缀），默认 20G。
// 不能超过 NATS 服务端 jetstream.max_file，否则建流失败。
func StreamMaxBytes() int64 {
	raw := strings.ToUpper(strings.TrimSpace(os.Getenv("RISK_STREAM_MAX_BYTES")))
	if raw == "" {
		return 20 << 30
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(raw, "G"), strings.HasSuffix(raw, "GB"):
		mult, raw = 1<<30, strings.TrimSuffix(strings.TrimSuffix(raw, "B"), "G")
	case strings.HasSuffix(raw, "M"), strings.HasSuffix(raw, "MB"):
		mult, raw = 1<<20, strings.TrimSuffix(strings.TrimSuffix(raw, "B"), "M")
	case strings.HasSuffix(raw, "K"), strings.HasSuffix(raw, "KB"):
		mult, raw = 1<<10, strings.TrimSuffix(strings.TrimSuffix(raw, "B"), "K")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 20 << 30
	}
	return n * mult
}

// EnsureStreams 建流（可重复执行；配置变了就更新）
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	cfgs := []jetstream.StreamConfig{
		{
			Name:        StreamEvents,
			Description: "风控事件：业务结果、决策、CEP 告警",
			Subjects:    []string{SubjectEventPrefix + ">", SubjectDecisionPrefix + ">", SubjectAlertPrefix + ">"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			MaxAge:      7 * 24 * time.Hour,
			MaxBytes:    StreamMaxBytes(),
			Discard:     jetstream.DiscardOld,
			// 去重窗口覆盖待发队列的积压时长：NATS 断开期间落盘的事件恢复后补发，不会重复入流
			Duplicates: 2 * time.Hour,
			Replicas:   1,
		},
		{
			Name:        StreamCommands,
			Description: "风控处置命令（工作队列）",
			Subjects:    []string{SubjectCommandPrefix + ">"},
			Retention:   jetstream.WorkQueuePolicy,
			Storage:     jetstream.FileStorage,
			MaxAge:      7 * 24 * time.Hour,
			MaxBytes:    1 << 30,
			Duplicates:  2 * time.Hour,
			Replicas:    1,
		},
	}
	for _, c := range cfgs {
		if _, err := js.CreateOrUpdateStream(ctx, c); err != nil {
			return fmt.Errorf("建流 %s：%w", c.Name, err)
		}
	}
	return nil
}

// Bus 可靠发布：同步等 JetStream 确认；失败落本地 bbolt，后台按原消息 ID 补发
type Bus struct {
	nc    *nats.Conn
	js    jetstream.JetStream
	db    *bolt.DB
	mu    sync.Mutex
	logf  func(string, ...any)
	close chan struct{}
	wg    sync.WaitGroup
}

var spoolBucket = []byte("spool")

type spooled struct {
	Subject string `json:"s"`
	MsgID   string `json:"i"`
	Data    []byte `json:"d"`
}

// Connect 连 NATS 并打开待发队列
func Connect(url, spoolDir, name string, logf func(string, ...any)) (*Bus, error) {
	nc, err := nats.Connect(url, nats.Name(name), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second), nats.RetryOnFailedConnect(true))
	if err != nil {
		return nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, err
	}
	b := &Bus{nc: nc, js: js, logf: logf, close: make(chan struct{})}
	if spoolDir != "" {
		if err := os.MkdirAll(spoolDir, 0o750); err != nil {
			nc.Close()
			return nil, err
		}
		db, err := bolt.Open(filepath.Join(spoolDir, name+"-spool.db"), 0o600, &bolt.Options{Timeout: 5 * time.Second})
		if err != nil {
			nc.Close()
			return nil, err
		}
		if err := db.Update(func(tx *bolt.Tx) error { _, err := tx.CreateBucketIfNotExists(spoolBucket); return err }); err != nil {
			nc.Close()
			_ = db.Close()
			return nil, err
		}
		b.db = db
		b.wg.Add(1)
		go b.drainLoop()
	}
	return b, nil
}

// JetStream 底层句柄（消费者用）
func (b *Bus) JetStream() jetstream.JetStream { return b.js }

// Conn NATS 连接
func (b *Bus) Conn() *nats.Conn { return b.nc }

// Publish 发一条；返回 nil 表示已入流或已落盘待发
func (b *Bus) Publish(ctx context.Context, subject, msgID string, data []byte) error {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := b.js.Publish(pctx, subject, data, jetstream.WithMsgID(msgID))
	if err == nil {
		return nil
	}
	if b.db == nil {
		return err
	}
	if serr := b.spool(spooled{Subject: subject, MsgID: msgID, Data: data}); serr != nil {
		return errors.Join(err, serr)
	}
	return nil
}

// PublishEvent 发事件
func (b *Bus) PublishEvent(ctx context.Context, e Event) error {
	if e.ID == "" {
		e.ID = NewID()
	}
	if e.TS == 0 {
		e.TS = time.Now().UnixMilli()
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return b.Publish(ctx, e.Subject(), e.ID, raw)
}

// PublishCommand 发命令
func (b *Bus) PublishCommand(ctx context.Context, c Command) error {
	if c.TS == 0 {
		c.TS = time.Now().UnixMilli()
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return b.Publish(ctx, CommandSubject(c.Kind), c.ID, raw)
}

func (b *Bus) spool(m spooled) error {
	raw, _ := json.Marshal(m)
	return b.db.Update(func(tx *bolt.Tx) error {
		bk := tx.Bucket(spoolBucket)
		seq, _ := bk.NextSequence()
		key := fmt.Appendf(nil, "%020d", seq)
		return bk.Put(key, raw)
	})
}

// Pending 待发条数
func (b *Bus) Pending() int {
	if b.db == nil {
		return 0
	}
	n := 0
	_ = b.db.View(func(tx *bolt.Tx) error { n = tx.Bucket(spoolBucket).Stats().KeyN; return nil })
	return n
}

func (b *Bus) drainLoop() {
	defer b.wg.Done()
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.close:
			return
		case <-t.C:
			if b.nc.IsConnected() {
				b.drain()
			}
		}
	}
}

// drain 按顺序补发，成功一条删一条；遇到失败就停，下一轮再来
func (b *Bus) drain() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		var key []byte
		var m spooled
		_ = b.db.View(func(tx *bolt.Tx) error {
			k, v := tx.Bucket(spoolBucket).Cursor().First()
			if k != nil {
				key = append([]byte{}, k...)
				_ = json.Unmarshal(v, &m)
			}
			return nil
		})
		if key == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := b.js.Publish(ctx, m.Subject, m.Data, jetstream.WithMsgID(m.MsgID))
		cancel()
		if err != nil {
			if b.logf != nil {
				b.logf("待发事件补发失败（%d 条积压）：%v", b.Pending(), err)
			}
			return
		}
		_ = b.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(spoolBucket).Delete(key) })
	}
}

// Close 先尽量补发，再关
func (b *Bus) Close() {
	close(b.close)
	b.wg.Wait()
	if b.db != nil {
		if b.nc.IsConnected() {
			b.drain()
		}
		_ = b.db.Close()
	}
	_ = b.nc.Drain()
}
