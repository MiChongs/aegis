package service

import (
	"encoding/json"
	"sync/atomic"

	"aegis/internal/cluster"

	"go.uber.org/zap"
)

// 多副本协调在服务层的两件小工具。
//
// 服务的后台协程有不少是在构造函数里就起好的（例如支付的清理循环），而 bootstrap 是在构造完之后
// 才调 SetCluster —— 判定函数与集群句柄必须能在协程已经在跑的时候安全地换上，所以都用原子指针存。
// 零值即「没接集群」：clusterTask 一律放行、clusterLink 的广播什么也不做，行为与单实例完全一致。

// clusterTask 是一项「全集群只该有一个实例在做」的后台任务的判定。
type clusterTask struct {
	gate atomic.Pointer[func() bool]
}

// bind 向集群登记任务名并记下判定函数。c 为 nil 时保持放行。
func (t *clusterTask) bind(c *cluster.Cluster, name string) {
	if t == nil || c == nil {
		return
	}
	fn := c.Gate(name)
	t.gate.Store(&fn)
}

// owned 报告本实例此刻是否负责这项任务。循环每轮先问它，不归自己就跳过这一轮
// （循环本身照常跑，负责实例宕机后本实例才能在下一轮接手）。
func (t *clusterTask) owned() bool {
	if t == nil {
		return true
	}
	fn := t.gate.Load()
	return fn == nil || (*fn)()
}

// clusterLink 持有集群句柄，用来发失效广播、取节点标识。
type clusterLink struct {
	c atomic.Pointer[cluster.Cluster]
}

func (l *clusterLink) set(c *cluster.Cluster) {
	if l != nil && c != nil {
		l.c.Store(c)
	}
}

// get 返回集群句柄；没接集群时为 nil（*cluster.Cluster 的方法对 nil 安全）。
func (l *clusterLink) get() *cluster.Cluster {
	if l == nil {
		return nil
	}
	return l.c.Load()
}

// attached 是否接上了集群。
func (l *clusterLink) attached() bool { return l.get() != nil }

// publish 把 v 编成 JSON 广播出去。广播只是「让别的实例尽快跟上」，编码失败只记日志，
// 绝不让发起方的管理操作因此报错。
func (l *clusterLink) publish(log *zap.Logger, topic string, v any) {
	c := l.get()
	if c == nil {
		return
	}
	payload, err := json.Marshal(v)
	if err != nil {
		if log != nil {
			log.Warn("集群广播编码失败", zap.String("topic", topic), zap.Error(err))
		}
		return
	}
	c.Publish(topic, payload)
}

// nodeID 本实例的集群标识；没接集群时为空串。
func (l *clusterLink) nodeID() string {
	c := l.get()
	if c == nil {
		return ""
	}
	return c.ID()
}
