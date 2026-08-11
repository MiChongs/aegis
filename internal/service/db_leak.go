package service

import (
	"fmt"
	"sync"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
)

// 数据库泄漏检测。
//
// 「泄漏」在数据库语境下不止一种，本文件覆盖六类，每类的表现与定位手段都不同：
//
//	connection      连接借出未归还           ← 进程内栈回溯，能定位到代码行
//	transaction     事务开着不提交            ← 服务端 idle in transaction
//	snapshot        快照不推进，VACUUM 收不动 ← backend_xmin 年龄
//	wal             复制槽未消费，WAL 堆积    ← 磁盘会被慢慢吃光
//	prepared_xact   两阶段事务悬挂            ← 持锁且阻止回收，必须人工处理
//	storage         死元组 / 临时文件堆积      ← 空间与性能的慢性劣化
//
// 前五类是「确定性规则」：命中即为问题，直接给结论和处置建议。
// 最后再叠一层趋势检测，用于捕捉尚未越过阈值、但在单调上涨的指标。

// 泄漏类别
const (
	LeakKindConnection   = "connection"
	LeakKindTransaction  = "transaction"
	LeakKindSnapshot     = "snapshot"
	LeakKindWAL          = "wal"
	LeakKindPreparedXact = "prepared_xact"
	LeakKindStorage      = "storage"
	LeakKindPool         = "pool"
	LeakKindTrend        = "trend"
)

// 严重级别
const (
	LeakSeverityInfo     = "info"
	LeakSeverityWarning  = "warning"
	LeakSeverityCritical = "critical"
)

// DBLeakFinding 一条泄漏结论
type DBLeakFinding struct {
	Kind       string    `json:"kind"`
	Severity   string    `json:"severity"`
	Title      string    `json:"title"`
	Detail     string    `json:"detail"`
	Count      int64     `json:"count,omitempty"`
	Evidence   []string  `json:"evidence,omitempty"`
	Advice     string    `json:"advice,omitempty"`
	DetectedAt time.Time `json:"detectedAt"`
}

// DBLeakReport 泄漏检测完整报告
type DBLeakReport struct {
	CheckedAt  time.Time       `json:"checkedAt"`
	Suspicious bool            `json:"suspicious"`
	Critical   int             `json:"critical"`
	Warning    int             `json:"warning"`
	Summary    string          `json:"summary"`
	Findings   []DBLeakFinding `json:"findings"`
	Trends     []LeakIndicator `json:"trends"`
}

// dbTrendDetector 数据库指标的滑动窗口趋势检测。
//
// 与内存侧同构（连续上升 N 次即可疑），但盯的是「只增不减就一定有问题」的量：
// 借出连接数、在途语句数、idle in transaction 数、死元组、临时文件字节、WAL 滞留字节。
type dbTrendDetector struct {
	mu         sync.RWMutex
	windowSize int
	threshold  int

	checkedOut  []float64
	inFlight    []float64
	idleInTx    []float64
	deadTuples  []float64
	tempBytes   []float64
	walRetained []float64
	writeIdx    int
	sampleCount int
}

func newDBTrendDetector(windowSize int) *dbTrendDetector {
	if windowSize <= 0 {
		windowSize = 20
	}
	threshold := windowSize * 60 / 100
	if threshold < 5 {
		threshold = 5
	}
	return &dbTrendDetector{
		windowSize:  windowSize,
		threshold:   threshold,
		checkedOut:  make([]float64, windowSize),
		inFlight:    make([]float64, windowSize),
		idleInTx:    make([]float64, windowSize),
		deadTuples:  make([]float64, windowSize),
		tempBytes:   make([]float64, windowSize),
		walRetained: make([]float64, windowSize),
	}
}

func (d *dbTrendDetector) record(metric DatabaseMetrics) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.checkedOut[d.writeIdx] = float64(metric.Instrument.CheckedOut)
	d.inFlight[d.writeIdx] = float64(metric.Instrument.InFlightQueries)
	d.idleInTx[d.writeIdx] = float64(metric.Server.IdleInTxCount)
	d.deadTuples[d.writeIdx] = float64(metric.Server.DeadTuples)
	d.tempBytes[d.writeIdx] = float64(metric.Server.TempBytes)
	d.walRetained[d.writeIdx] = float64(metric.Server.ReplicationRetainedBytes)
	d.writeIdx = (d.writeIdx + 1) % d.windowSize
	if d.sampleCount < d.windowSize {
		d.sampleCount++
	}
}

func (d *dbTrendDetector) indicators(now time.Time) []LeakIndicator {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return []LeakIndicator{
		d.analyze("checked_out_conns", d.checkedOut, "借出中的连接数", now),
		d.analyze("in_flight_queries", d.inFlight, "在途语句数", now),
		d.analyze("idle_in_transaction", d.idleInTx, "idle in transaction 会话数", now),
		d.analyze("dead_tuples", d.deadTuples, "死元组总数", now),
		d.analyze("temp_bytes", d.tempBytes, "临时文件字节", now),
		d.analyze("wal_retained_bytes", d.walRetained, "复制槽滞留 WAL 字节", now),
	}
}

// analyze 与内存侧 LeakDetector 判定逻辑一致：连续上升次数 + 首尾变化率双判据。
func (d *dbTrendDetector) analyze(name string, ring []float64, displayName string, now time.Time) LeakIndicator {
	indicator := LeakIndicator{Name: name, Samples: d.sampleCount, Trending: "stable", LastCheckedAt: now}
	if d.sampleCount < 3 {
		indicator.AlertMessage = "采样点不足，暂无法判定趋势。"
		return indicator
	}
	ordered := make([]float64, 0, d.sampleCount)
	if d.sampleCount < d.windowSize {
		ordered = append(ordered, ring[:d.sampleCount]...)
	} else {
		ordered = append(ordered, ring[d.writeIdx:]...)
		ordered = append(ordered, ring[:d.writeIdx]...)
	}
	indicator.LatestValue = ordered[len(ordered)-1]

	consecutive, maxConsecutive := 0, 0
	for i := 1; i < len(ordered); i++ {
		if ordered[i] > ordered[i-1] {
			consecutive++
			if consecutive > maxConsecutive {
				maxConsecutive = consecutive
			}
		} else {
			consecutive = 0
		}
	}
	indicator.ConsecutiveUp = maxConsecutive
	if first := ordered[0]; first > 0 {
		indicator.DeltaPercent = (ordered[len(ordered)-1] - first) / first * 100
	}

	switch {
	case maxConsecutive >= d.threshold:
		indicator.Trending = "rising"
		indicator.SuspectedLeak = true
		indicator.AlertMessage = fmt.Sprintf("%s 连续上升 %d 次（阈值 %d），变化率 %.1f%%，存在只增不减的迹象。",
			displayName, maxConsecutive, d.threshold, indicator.DeltaPercent)
	case indicator.DeltaPercent > 50 && maxConsecutive >= d.threshold/2:
		indicator.Trending = "rising"
		indicator.SuspectedLeak = true
		indicator.AlertMessage = fmt.Sprintf("%s 变化率 %.1f%%（连续上升 %d 次），增长较快，需关注。",
			displayName, indicator.DeltaPercent, maxConsecutive)
	case indicator.DeltaPercent < -10:
		indicator.Trending = "declining"
	}
	return indicator
}

// analyzeDBLeaks 基于一份快照产出确定性泄漏结论。
func analyzeDBLeaks(metric DatabaseMetrics, suspects []db.ConnLeakSuspect, cfg config.DatabaseConfig, now time.Time) []DBLeakFinding {
	findings := make([]DBLeakFinding, 0, 8)

	// ① 连接泄漏：进程内证据最硬，直接把调用栈端上来
	if len(suspects) > 0 {
		evidence := make([]string, 0, 3)
		for i, suspect := range suspects {
			if i >= 3 {
				break
			}
			entry := fmt.Sprintf("PID %d 已持有 %.1fs", suspect.PID, float64(suspect.HeldMs)/1000)
			if suspect.Stack != "" {
				entry += "\n" + suspect.Stack
			}
			evidence = append(evidence, entry)
		}
		severity := LeakSeverityWarning
		if int32(len(suspects)) >= metric.Pool.MaxConns/2 && metric.Pool.MaxConns > 0 {
			severity = LeakSeverityCritical
		}
		advice := "检查 evidence 中的调用栈：常见成因是 rows 未 Close、tx 未 Commit/Rollback、或错误分支提前 return 漏掉 Release。"
		if !cfg.TrackAcquireStack {
			advice = "开启 DB_TRACK_ACQUIRE_STACK=true 后可获取借出连接的调用栈，从而定位到具体代码行。"
		}
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindConnection, Severity: severity,
			Title: "连接借出后长时间未归还",
			Detail: fmt.Sprintf("%d 条连接持有时长超过阈值 %s，池上限 %d。持续下去会耗尽连接池并让新请求全部排队。",
				len(suspects), cfg.ConnHoldThreshold, metric.Pool.MaxConns),
			Count: int64(len(suspects)), Evidence: evidence, Advice: advice, DetectedAt: now,
		})
	}

	// ② 事务泄漏：事务开着不动，既占连接又扣住快照
	if cfg.IdleInTxThreshold > 0 && metric.Server.OldestIdleInTxSeconds >= cfg.IdleInTxThreshold.Seconds() {
		severity := LeakSeverityWarning
		if metric.Server.OldestIdleInTxSeconds >= cfg.IdleInTxThreshold.Seconds()*5 {
			severity = LeakSeverityCritical
		}
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindTransaction, Severity: severity,
			Title: "存在长时间 idle in transaction 会话",
			Detail: fmt.Sprintf("最久的一个已保持 %.0fs（阈值 %s），当前共 %d 个（其中 %d 个已 aborted）。这类会话会一直持锁并阻止 VACUUM 回收。",
				metric.Server.OldestIdleInTxSeconds, cfg.IdleInTxThreshold,
				metric.Server.IdleInTxCount, metric.Server.IdleInTxAborted),
			Count: int64(metric.Server.IdleInTxCount),
			Advice: "在「会话」页按 idle in transaction 过滤定位来源；本进程可用 POSTGRES_IDLE_IN_TX_TIMEOUT 自动了结，" +
				"外部客户端需开启 DB_AUTO_TERMINATE_IDLE_IN_TX 或人工终止。",
			DetectedAt: now,
		})
	}

	// ③ 快照泄漏：xmin 不推进 → 死元组回收不了 → 表持续膨胀
	if metric.Server.OldestSnapshotSeconds > 3600 {
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindSnapshot, Severity: LeakSeverityWarning,
			Title: "最老快照长时间未推进",
			Detail: fmt.Sprintf("存在已持续 %.0f 分钟的事务快照。只要它还在，VACUUM 就无法回收该时间点之后产生的死元组，表会持续膨胀。",
				metric.Server.OldestSnapshotSeconds/60),
			Advice:     "定位并结束最老的那个长事务（会话列表按事务时长倒序第一条），或为长任务改用短事务分批提交。",
			DetectedAt: now,
		})
	}

	// ④ WAL 泄漏：未消费的复制槽会无上限占用磁盘，直到把数据盘写满
	if metric.Server.InactiveReplicationSlots > 0 {
		severity := LeakSeverityWarning
		if metric.Server.ReplicationRetainedBytes > 10<<30 {
			severity = LeakSeverityCritical
		}
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindWAL, Severity: severity,
			Title: "存在未消费的复制槽",
			Detail: fmt.Sprintf("%d 个复制槽处于 inactive，已滞留约 %s WAL。复制槽不会自动过期，磁盘会被持续占用直至写满。",
				metric.Server.InactiveReplicationSlots, humanBytes(metric.Server.ReplicationRetainedBytes)),
			Count:      int64(metric.Server.InactiveReplicationSlots),
			Advice:     "确认对应的订阅端/备库是否已下线；确定不再需要时执行 SELECT pg_drop_replication_slot('槽名')。",
			DetectedAt: now,
		})
	}

	// ⑤ 两阶段事务悬挂：只能人工了结，放着必然出事
	if metric.Server.PreparedXacts > 0 {
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindPreparedXact, Severity: LeakSeverityCritical,
			Title: "存在悬挂的两阶段事务",
			Detail: fmt.Sprintf("%d 个 prepared transaction 未了结，最久 %.0f 分钟。它们会一直持锁并阻止 VACUUM，且不受任何超时参数约束。",
				metric.Server.PreparedXacts, metric.Server.OldestPreparedXactSecs/60),
			Count:      int64(metric.Server.PreparedXacts),
			Advice:     "查询 pg_prepared_xacts 后逐个 COMMIT PREPARED / ROLLBACK PREPARED；若本系统未使用两阶段提交，应排查是谁在用。",
			DetectedAt: now,
		})
	}

	// ⑥ 存储劣化：死元组堆积说明 autovacuum 没跟上
	if metric.Server.TablesNeedingVacuum > 0 {
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindStorage, Severity: LeakSeverityWarning,
			Title: "死元组堆积，autovacuum 未跟上",
			Detail: fmt.Sprintf("%d 张表死元组占比超过 20%% 且绝对量超过 1 万，全库死元组共 %d 行。",
				metric.Server.TablesNeedingVacuum, metric.Server.DeadTuples),
			Count:      int64(metric.Server.TablesNeedingVacuum),
			Advice:     "先排除长事务/长快照占用（它们会让 VACUUM 无功而返），再考虑调低这些表的 autovacuum_vacuum_scale_factor。",
			DetectedAt: now,
		})
	}

	// ⑦ 池饱和：等待与取消是连接不够用的直接证据
	if metric.Pool.EmptyAcquireCount > 0 || metric.Pool.CanceledAcquireCount > 0 {
		severity := LeakSeverityInfo
		if metric.Pool.CanceledAcquireCount > 0 {
			severity = LeakSeverityWarning
		}
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindPool, Severity: severity,
			Title: "连接池出现等待",
			Detail: fmt.Sprintf("累计 %d 次获取连接时池内无空闲，%d 次因上下文取消而放弃；平均等待 %.1fms。",
				metric.Pool.EmptyAcquireCount, metric.Pool.CanceledAcquireCount, metric.Pool.AvgAcquireWaitMs),
			Count: metric.Pool.EmptyAcquireCount,
			Advice: "若同时存在连接泄漏结论，先修泄漏；否则说明并发确实超过池容量，考虑上调 POSTGRES_MAX_CONNS " +
				"（注意不要超过服务端 max_connections 的分配额度）。",
			DetectedAt: now,
		})
	}

	// ⑧ 服务端连接水位：逼近 max_connections 时新连接会直接被拒
	if metric.Server.ConnUsagePct >= 80 {
		severity := LeakSeverityWarning
		if metric.Server.ConnUsagePct >= 90 {
			severity = LeakSeverityCritical
		}
		findings = append(findings, DBLeakFinding{
			Kind: LeakKindPool, Severity: severity,
			Title: "服务端连接数逼近上限",
			Detail: fmt.Sprintf("当前 %d / %d（%.0f%%）。达到上限后任何新连接都会被直接拒绝，包括运维用的 psql。",
				metric.Server.TotalBackends, metric.Server.MaxConnections, metric.Server.ConnUsagePct),
			Advice:     "确认是否有其它服务共用这套库；本进程的占用可由 POSTGRES_MAX_CONNS 收敛。",
			DetectedAt: now,
		})
	}

	return findings
}

func humanBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(value)/float64(div), "KMGTP"[exp])
}
