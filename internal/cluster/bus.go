package cluster

import (
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// SubjectPrefix 集群广播主题前缀。用核心 NATS（不进 JetStream）：
// 失效通知只对此刻在线的实例有意义，后启动的实例本来就会从数据库全量加载。
const SubjectPrefix = "aegis.cluster."

const originHeader = "Aegis-Origin"

// 已知主题。新加缓存失效广播时在这里登记，免得各处手写字符串对不上。
const (
	TopicPlatformSettings = "platform-settings" // 防火墙 / 安全 / LDAP / OIDC / SAML 设置
	TopicGeoBan           = "geo-ban"
	TopicGeoFence         = "geo-fence"
	TopicPluginHooks      = "plugin-hooks"
	TopicAdminUserSearch  = "admin-user-search" // payload: {"op":"index"|"delete"|"sync-app", ...}
	TopicStorageMigration = "storage-migration" // payload: {"op":"cancel","id":...}
)

// Bus 是集群内的失效广播。
type Bus struct {
	nc   *nats.Conn
	node string
	log  *zap.Logger

	mu   sync.Mutex
	subs []*nats.Subscription
}

// NewBus 创建广播总线；nc 为 nil 时返回 nil（Publish / Subscribe 什么也不做）。
func NewBus(nc *nats.Conn, node string, log *zap.Logger) *Bus {
	if nc == nil {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Bus{nc: nc, node: node, log: log.Named("cluster")}
}

// Publish 发出一条广播。失败只记日志：广播丢了，其他实例最多晚到下一次重启或定时刷新才看到新配置，
// 不能因此让发起方的管理操作报错。
func (b *Bus) Publish(topic string, payload []byte) {
	if b == nil {
		return
	}
	msg := &nats.Msg{Subject: SubjectPrefix + topic, Data: payload, Header: nats.Header{}}
	msg.Header.Set(originHeader, b.node)
	if err := b.nc.PublishMsg(msg); err != nil {
		b.log.Warn("集群广播发送失败", zap.String("topic", topic), zap.Error(err))
	}
}

// Subscribe 订阅 topic。本实例自己发出的消息会被跳过 —— 发起方在本地已经改好了。
// fn 在 NATS 的回调协程里执行，里面的 panic 会被拦下记日志。
func (b *Bus) Subscribe(topic string, fn func(payload []byte)) {
	if b == nil || fn == nil {
		return
	}
	sub, err := b.nc.Subscribe(SubjectPrefix+topic, func(msg *nats.Msg) {
		if msg.Header != nil && msg.Header.Get(originHeader) == b.node {
			return
		}
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("集群广播处理异常", zap.String("topic", topic), zap.Any("panic", r))
			}
		}()
		fn(msg.Data)
	})
	if err != nil {
		b.log.Warn("集群广播订阅失败", zap.String("topic", topic), zap.Error(err))
		return
	}
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
}

// Close 退订全部主题。连接本身由调用方负责关闭。
func (b *Bus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	for _, sub := range subs {
		_ = sub.Unsubscribe()
	}
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
