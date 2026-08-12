package authz

import (
	"strings"
	"sync"

	"github.com/casbin/casbin/v2/persist"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// SubjectPolicyUpdated 策略变更广播主题。
const SubjectPolicyUpdated = "authz.policy.updated"

// natsWatcher 用 NATS 做跨实例策略同步。
//
// 复用已有的那条连接而不是引一个 casbin watcher 包：这里要做的事只有
// 「发一条消息 / 收到就重载」，而多一个依赖就多一份连接配置、一套重连语义
// 和一处会在断线时静默失效的地方。
//
// 自己发的消息会被自己收到（NATS 没有 no-echo 语义可依赖），因此消息里带上
// 实例 ID，收到自己发的直接丢掉 —— 否则每次改策略本实例都要多做一次无谓的全量重载。
type natsWatcher struct {
	conn       *nats.Conn
	instanceID string
	log        *zap.Logger

	mu       sync.RWMutex
	callback func(string)
	sub      *nats.Subscription
}

var _ persist.Watcher = (*natsWatcher)(nil)

// NewNATSWatcher 构造广播器；conn 为 nil（单实例 / 未配置 NATS）时返回 nil，
// 调用方按"没有广播"处理即可 —— 单实例本来就不需要它。
func NewNATSWatcher(conn *nats.Conn, instanceID string, log *zap.Logger) persist.Watcher {
	if conn == nil {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &natsWatcher{conn: conn, instanceID: instanceID, log: log}
}

func (w *natsWatcher) SetUpdateCallback(callback func(string)) error {
	w.mu.Lock()
	w.callback = callback
	existing := w.sub
	w.mu.Unlock()
	if existing != nil {
		return nil
	}
	sub, err := w.conn.Subscribe(SubjectPolicyUpdated, func(msg *nats.Msg) {
		sender := strings.TrimSpace(string(msg.Data))
		if sender != "" && sender == w.instanceID {
			return // 自己发的，本实例已经重载过了
		}
		w.mu.RLock()
		fn := w.callback
		w.mu.RUnlock()
		if fn != nil {
			fn(sender)
		}
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.sub = sub
	w.mu.Unlock()
	return nil
}

func (w *natsWatcher) Update() error {
	return w.conn.Publish(SubjectPolicyUpdated, []byte(w.instanceID))
}

func (w *natsWatcher) Close() {
	w.mu.Lock()
	sub := w.sub
	w.sub = nil
	w.mu.Unlock()
	if sub != nil {
		_ = sub.Unsubscribe()
	}
}
