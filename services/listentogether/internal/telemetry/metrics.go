// Package telemetry 一起听服务的可观测性：Prometheus 指标与 OpenTelemetry 链路。
package telemetry

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics 服务指标。全部挂在独立的注册表上，经内部端口 /metrics 暴露。
type Metrics struct {
	Registry *prometheus.Registry

	Connections     prometheus.Gauge
	Rooms           prometheus.Gauge
	ConnectsTotal   *prometheus.CounterVec
	Commands        *prometheus.CounterVec
	CommandDuration *prometheus.HistogramVec
	EventsDelivered prometheus.Counter
	SlowConsumers   prometheus.Counter
	SweepDuration   prometheus.Histogram
}

// NewMetrics 注册全部指标（含 Go 运行时与进程指标）。
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		Registry: reg,
		Connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "listen_together", Name: "connections", Help: "当前实例上的 WebSocket 连接数",
		}),
		Rooms: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "listen_together", Name: "local_rooms", Help: "当前实例上有连接的房间数",
		}),
		ConnectsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "listen_together", Name: "connects_total", Help: "握手结果计数",
		}, []string{"result"}),
		Commands: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "listen_together", Name: "commands_total", Help: "客户端命令计数",
		}, []string{"op", "result"}),
		CommandDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "listen_together", Name: "command_duration_seconds", Help: "命令处理耗时",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1},
		}, []string{"op"}),
		EventsDelivered: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "listen_together", Name: "events_delivered_total", Help: "投递给客户端的房间事件数",
		}),
		SlowConsumers: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "listen_together", Name: "slow_consumers_total", Help: "因发送缓冲写满被断开的连接数",
		}),
		SweepDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "listen_together", Name: "sweep_duration_seconds", Help: "房间巡检耗时",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.Connections, m.Rooms, m.ConnectsTotal, m.Commands, m.CommandDuration,
		m.EventsDelivered, m.SlowConsumers, m.SweepDuration,
	)
	return m
}

// ObserveCommand 记一条命令的结果与耗时。
func (m *Metrics) ObserveCommand(op, result string, started time.Time) {
	m.Commands.WithLabelValues(op, result).Inc()
	m.CommandDuration.WithLabelValues(op).Observe(time.Since(started).Seconds())
}
