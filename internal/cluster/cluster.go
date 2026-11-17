// Package cluster 让同一套 Aegis 可以在多台机器上同时跑多个 API / Worker 副本。
//
// 多副本之间需要协调的事只有三类，这个包各给一种手段：
//
//   - 只该有一个实例做的后台任务（按时轮询支付上游、写共享监控曲线、续跑存储迁移……）
//     → Elector：Postgres 租约选主，每个任务名一把租约，持有者宕机后一个 TTL 内被别的实例接手；
//   - 某个实例改了内存里的配置（防火墙、地理封禁、插件钩子……），其余实例要跟着重载
//     → Bus：NATS 核心主题广播，自己发的消息自己不再处理；
//   - 同一时刻只能有一个实例执行的一次性动作（迁移、内置策略重建）
//     → WithAdvisoryLock：Postgres 会话级 advisory lock。
//
// 三者都对 nil 安全：没有 Postgres / NATS（单测、单机老部署）时，Gate 一律放行、
// 广播什么也不做，行为与改造前的单实例完全一致。
package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

// Cluster 是注入到各服务里的协调句柄。
type Cluster struct {
	// NodeID 本进程的租约持有者标识，每次启动都不同（带随机串）。
	NodeID string
	// Instance 副本的稳定标识：配置了 AEGIS_NODE_ID 时就是它（Swarm 里是「主机名-角色-槽位」），否则为主机名。
	// 重启后不变，用于按副本分键的监控历史这类「重启后还应接上」的数据；不要拿它当租约持有者。
	Instance string
	Elector *Elector
	Bus     *Bus
}

// Gate 返回「本实例当前是否负责 name 这项任务」的判定函数。
// c 或 Elector 为 nil 时永远返回 true（单实例语义）。
func (c *Cluster) Gate(name string) func() bool {
	if c == nil || c.Elector == nil {
		return alwaysTrue
	}
	return c.Elector.Gate(name)
}

// Publish 广播一条集群消息；c 为 nil 时什么也不做。
func (c *Cluster) Publish(topic string, payload []byte) {
	if c == nil || c.Bus == nil {
		return
	}
	c.Bus.Publish(topic, payload)
}

// Subscribe 订阅其他实例发来的集群消息；c 为 nil 时什么也不做。
func (c *Cluster) Subscribe(topic string, fn func(payload []byte)) {
	if c == nil || c.Bus == nil {
		return
	}
	c.Bus.Subscribe(topic, fn)
}

// ID 返回节点标识；c 为 nil 时返回进程级默认标识。
func (c *Cluster) ID() string {
	if c == nil || c.NodeID == "" {
		return defaultNodeID
	}
	return c.NodeID
}

// InstanceKey 返回副本的稳定标识；c 为 nil 或未设置时退回主机名。
func (c *Cluster) InstanceKey() string {
	if c != nil && strings.TrimSpace(c.Instance) != "" {
		return c.Instance
	}
	return InstanceID("")
}

// InstanceID 按配置生成副本稳定标识：configured 非空原样返回，否则用主机名。
func InstanceID(configured string) string {
	if id := strings.TrimSpace(configured); id != "" {
		return id
	}
	host, _ := os.Hostname()
	if host = strings.TrimSpace(host); host == "" {
		host = "aegis"
	}
	return host
}

func alwaysTrue() bool { return true }

var defaultNodeID = NodeID("")

// NodeID 生成本进程的集群标识。
//
// configured 非空时原样使用（Swarm 里用 {{.Node.Hostname}}-{{.Task.Slot}}，日志里一眼能对上是哪台机器的第几个副本）；
// 为空时用「主机名-随机串」。随机串保证同一台机器上重启后的新进程不会冒用旧进程尚未过期的租约。
func NodeID(configured string) string {
	if id := strings.TrimSpace(configured); id != "" {
		return id + "-" + randomSuffix(3)
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "aegis"
	}
	return host + "-" + randomSuffix(4)
}

func randomSuffix(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "0000"
	}
	return hex.EncodeToString(buf)
}
