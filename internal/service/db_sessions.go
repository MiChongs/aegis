package service

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 服务端视角的数据库状态采集。
//
// 进程内的观测装置只能看到「我们自己借出的连接」；下面这些视图补上另一半：
// 别的进程、上一次部署残留的会话、以及那些不占连接却同样是泄漏的东西
// （未消费的复制槽、悬挂的两阶段事务、迟迟不推进的快照）。

// ServerMetrics 服务端聚合指标
type ServerMetrics struct {
	// ── 连接维度 ──
	MaxConnections  int     `json:"maxConnections"`
	TotalBackends   int     `json:"totalBackends"`
	ActiveBackends  int     `json:"activeBackends"`
	IdleBackends    int     `json:"idleBackends"`
	IdleInTxCount   int     `json:"idleInTxCount"`
	IdleInTxAborted int     `json:"idleInTxAborted"`
	WaitingBackends int     `json:"waitingBackends"`
	ConnUsagePct    float64 `json:"connUsagePct"`

	// ── 时长维度（秒），用于识别「卡住的东西」 ──
	OldestXactSeconds     float64 `json:"oldestXactSeconds"`
	OldestQuerySeconds    float64 `json:"oldestQuerySeconds"`
	OldestIdleInTxSeconds float64 `json:"oldestIdleInTxSeconds"`
	// OldestSnapshotSeconds 最老快照年龄：它决定 VACUUM 能回收到哪一行，
	// 长期不推进就是表膨胀的根因（即使那个会话看起来很闲）。
	OldestSnapshotSeconds float64 `json:"oldestSnapshotSeconds"`

	// ── 吞吐与健康 ──
	XactCommit     int64   `json:"xactCommit"`
	XactRollback   int64   `json:"xactRollback"`
	Deadlocks      int64   `json:"deadlocks"`
	TempFiles      int64   `json:"tempFiles"`
	TempBytes      int64   `json:"tempBytes"`
	BlksHit        int64   `json:"blksHit"`
	BlksRead       int64   `json:"blksRead"`
	CacheHitRatio  float64 `json:"cacheHitRatio"`
	DatabaseBytes  int64   `json:"databaseBytes"`
	ConflictCount  int64   `json:"conflictCount"`
	RolledBackPct  float64 `json:"rolledBackPct"`
	BlockedByLocks int     `json:"blockedByLocks"`

	// ── 非连接型泄漏 ──
	InactiveReplicationSlots int     `json:"inactiveReplicationSlots"`
	ReplicationRetainedBytes int64   `json:"replicationRetainedBytes"`
	PreparedXacts            int     `json:"preparedXacts"`
	OldestPreparedXactSecs   float64 `json:"oldestPreparedXactSeconds"`
	DeadTuples               int64   `json:"deadTuples"`
	TablesNeedingVacuum      int     `json:"tablesNeedingVacuum"`

	CollectErrors []string `json:"collectErrors,omitempty"`
}

// DBSession 单个服务端会话
type DBSession struct {
	PID             int32      `json:"pid"`
	User            string     `json:"user,omitempty"`
	ApplicationName string     `json:"applicationName,omitempty"`
	ClientAddr      string     `json:"clientAddr,omitempty"`
	State           string     `json:"state,omitempty"`
	WaitEventType   string     `json:"waitEventType,omitempty"`
	WaitEvent       string     `json:"waitEvent,omitempty"`
	BackendType     string     `json:"backendType,omitempty"`
	Query           string     `json:"query,omitempty"`
	BackendStart    *time.Time `json:"backendStart,omitempty"`
	XactStart       *time.Time `json:"xactStart,omitempty"`
	QueryStart      *time.Time `json:"queryStart,omitempty"`
	StateChange     *time.Time `json:"stateChange,omitempty"`
	XactSeconds     float64    `json:"xactSeconds"`
	QuerySeconds    float64    `json:"querySeconds"`
	StateSeconds    float64    `json:"stateSeconds"`
	BlockedBy       []int32    `json:"blockedBy,omitempty"`
	IsSelf          bool       `json:"isSelf"`
}

// BloatedTable 死元组堆积的表
type BloatedTable struct {
	Schema          string     `json:"schema"`
	Table           string     `json:"table"`
	LiveTuples      int64      `json:"liveTuples"`
	DeadTuples      int64      `json:"deadTuples"`
	DeadRatio       float64    `json:"deadRatio"`
	TotalBytes      int64      `json:"totalBytes"`
	LastAutoVacuum  *time.Time `json:"lastAutoVacuum,omitempty"`
	LastAutoAnalyze *time.Time `json:"lastAutoAnalyze,omitempty"`
}

// UnusedIndex 从未被扫描过的索引（占空间、拖慢写入）
type UnusedIndex struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Index  string `json:"index"`
	Scans  int64  `json:"scans"`
	Bytes  int64  `json:"bytes"`
}

// collectServerMetrics 汇总服务端指标。
//
// 每个子查询独立容错：权限不足或视图不可用（如无复制槽权限）只记一条采集错误，
// 不让整份快照失败——监控本身不应成为新的故障点。
func collectServerMetrics(ctx context.Context, pool *pgxpool.Pool) ServerMetrics {
	metrics := ServerMetrics{}
	fail := func(scope string, err error) {
		if err != nil {
			metrics.CollectErrors = append(metrics.CollectErrors, scope+": "+err.Error())
		}
	}

	// 连接状态分布 + 各类「最老」时长。一次扫描 pg_stat_activity 拿全，避免多次全表扫。
	const activitySQL = `
SELECT
  COALESCE(current_setting('max_connections')::int, 0)                                       AS max_connections,
  COUNT(*)                                                                                   AS total_backends,
  COUNT(*) FILTER (WHERE state = 'active')                                                   AS active_backends,
  COUNT(*) FILTER (WHERE state = 'idle')                                                     AS idle_backends,
  COUNT(*) FILTER (WHERE state = 'idle in transaction')                                      AS idle_in_tx,
  COUNT(*) FILTER (WHERE state = 'idle in transaction (aborted)')                            AS idle_in_tx_aborted,
  COUNT(*) FILTER (WHERE wait_event_type IS NOT NULL AND state = 'active')                   AS waiting_backends,
  COALESCE(EXTRACT(EPOCH FROM MAX(NOW() - xact_start)), 0)                                   AS oldest_xact,
  COALESCE(EXTRACT(EPOCH FROM MAX(NOW() - query_start) FILTER (WHERE state = 'active')), 0)  AS oldest_query,
  COALESCE(EXTRACT(EPOCH FROM MAX(NOW() - state_change)
           FILTER (WHERE state LIKE 'idle in transaction%')), 0)                             AS oldest_idle_in_tx,
  COALESCE(EXTRACT(EPOCH FROM MAX(NOW() - xact_start) FILTER (WHERE backend_xmin IS NOT NULL)), 0) AS oldest_snapshot
FROM pg_stat_activity
WHERE datname = current_database() AND backend_type = 'client backend'`
	fail("activity", pool.QueryRow(ctx, activitySQL).Scan(
		&metrics.MaxConnections, &metrics.TotalBackends, &metrics.ActiveBackends,
		&metrics.IdleBackends, &metrics.IdleInTxCount, &metrics.IdleInTxAborted,
		&metrics.WaitingBackends, &metrics.OldestXactSeconds, &metrics.OldestQuerySeconds,
		&metrics.OldestIdleInTxSeconds, &metrics.OldestSnapshotSeconds,
	))
	if metrics.MaxConnections > 0 {
		metrics.ConnUsagePct = float64(metrics.TotalBackends) / float64(metrics.MaxConnections) * 100
	}

	// 库级累计计数器
	const dbStatSQL = `
SELECT COALESCE(xact_commit,0), COALESCE(xact_rollback,0), COALESCE(deadlocks,0),
       COALESCE(temp_files,0), COALESCE(temp_bytes,0), COALESCE(blks_hit,0),
       COALESCE(blks_read,0), COALESCE(conflicts,0), pg_database_size(current_database())
FROM pg_stat_database WHERE datname = current_database()`
	fail("pg_stat_database", pool.QueryRow(ctx, dbStatSQL).Scan(
		&metrics.XactCommit, &metrics.XactRollback, &metrics.Deadlocks,
		&metrics.TempFiles, &metrics.TempBytes, &metrics.BlksHit,
		&metrics.BlksRead, &metrics.ConflictCount, &metrics.DatabaseBytes,
	))
	if total := metrics.BlksHit + metrics.BlksRead; total > 0 {
		metrics.CacheHitRatio = float64(metrics.BlksHit) / float64(total) * 100
	}
	if total := metrics.XactCommit + metrics.XactRollback; total > 0 {
		metrics.RolledBackPct = float64(metrics.XactRollback) / float64(total) * 100
	}

	// 正在被锁阻塞的会话数
	const blockedSQL = `
SELECT COUNT(*) FROM pg_stat_activity
WHERE datname = current_database() AND cardinality(pg_blocking_pids(pid)) > 0`
	fail("blocked", pool.QueryRow(ctx, blockedSQL).Scan(&metrics.BlockedByLocks))

	// 未消费的复制槽：会无限扣住 WAL，是最容易被忽视的「磁盘泄漏」
	const slotSQL = `
SELECT COUNT(*) FILTER (WHERE NOT active),
       COALESCE(SUM(COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0))
                FILTER (WHERE NOT active), 0)::bigint
FROM pg_replication_slots`
	fail("replication_slots", pool.QueryRow(ctx, slotSQL).Scan(
		&metrics.InactiveReplicationSlots, &metrics.ReplicationRetainedBytes))

	// 悬挂的两阶段事务：一直持锁并阻止 VACUUM 回收，必须人工 COMMIT/ROLLBACK PREPARED
	const preparedSQL = `
SELECT COUNT(*), COALESCE(EXTRACT(EPOCH FROM MAX(NOW() - prepared)), 0)
FROM pg_prepared_xacts WHERE database = current_database()`
	fail("prepared_xacts", pool.QueryRow(ctx, preparedSQL).Scan(
		&metrics.PreparedXacts, &metrics.OldestPreparedXactSecs))

	// 死元组总量与「该被 autovacuum 收拾却还没收拾」的表数
	const deadTupleSQL = `
SELECT COALESCE(SUM(n_dead_tup),0)::bigint,
       COUNT(*) FILTER (WHERE n_dead_tup > 10000 AND n_dead_tup > n_live_tup * 0.2)
FROM pg_stat_user_tables`
	fail("dead_tuples", pool.QueryRow(ctx, deadTupleSQL).Scan(
		&metrics.DeadTuples, &metrics.TablesNeedingVacuum))

	return metrics
}

// listSessions 列出服务端会话（含阻塞关系）。
func listSessions(ctx context.Context, pool *pgxpool.Pool, onlyProblematic bool, limit int) ([]DBSession, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	filter := ""
	if onlyProblematic {
		// 只看「值得看的」：活跃中、事务挂着、或被锁住
		filter = ` AND (state = 'active' OR state LIKE 'idle in transaction%'
                        OR cardinality(pg_blocking_pids(pid)) > 0)`
	}
	sql := `
SELECT pid, COALESCE(usename,''), COALESCE(application_name,''),
       COALESCE(host(client_addr),''), COALESCE(state,''),
       COALESCE(wait_event_type,''), COALESCE(wait_event,''), COALESCE(backend_type,''),
       LEFT(COALESCE(query,''), 800),
       backend_start, xact_start, query_start, state_change,
       COALESCE(EXTRACT(EPOCH FROM NOW() - xact_start), 0),
       COALESCE(EXTRACT(EPOCH FROM NOW() - query_start), 0),
       COALESCE(EXTRACT(EPOCH FROM NOW() - state_change), 0),
       pg_blocking_pids(pid),
       pid = pg_backend_pid()
FROM pg_stat_activity
WHERE datname = current_database() AND backend_type = 'client backend'` + filter + `
ORDER BY COALESCE(xact_start, query_start, backend_start) ASC NULLS LAST
LIMIT $1`

	rows, err := pool.Query(ctx, sql, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]DBSession, 0, 32)
	for rows.Next() {
		var item DBSession
		if err := rows.Scan(&item.PID, &item.User, &item.ApplicationName, &item.ClientAddr,
			&item.State, &item.WaitEventType, &item.WaitEvent, &item.BackendType, &item.Query,
			&item.BackendStart, &item.XactStart, &item.QueryStart, &item.StateChange,
			&item.XactSeconds, &item.QuerySeconds, &item.StateSeconds,
			&item.BlockedBy, &item.IsSelf); err != nil {
			return nil, err
		}
		item.Query = strings.Join(strings.Fields(item.Query), " ")
		items = append(items, item)
	}
	return items, rows.Err()
}

// listBloatedTables 死元组占比高的表（VACUUM 没跟上的信号）
func listBloatedTables(ctx context.Context, pool *pgxpool.Pool, limit int) ([]BloatedTable, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	const sql = `
SELECT schemaname, relname, COALESCE(n_live_tup,0), COALESCE(n_dead_tup,0),
       CASE WHEN COALESCE(n_live_tup,0) > 0
            THEN n_dead_tup::float8 / n_live_tup * 100 ELSE 0 END,
       pg_total_relation_size(relid)::bigint,
       last_autovacuum, last_autoanalyze
FROM pg_stat_user_tables
WHERE n_dead_tup > 0
ORDER BY n_dead_tup DESC
LIMIT $1`
	rows, err := pool.Query(ctx, sql, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BloatedTable, 0, limit)
	for rows.Next() {
		var item BloatedTable
		if err := rows.Scan(&item.Schema, &item.Table, &item.LiveTuples, &item.DeadTuples,
			&item.DeadRatio, &item.TotalBytes, &item.LastAutoVacuum, &item.LastAutoAnalyze); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// listUnusedIndexes 从未被扫描的索引（排除主键与唯一约束，那些是正确性所需）
func listUnusedIndexes(ctx context.Context, pool *pgxpool.Pool, limit int) ([]UnusedIndex, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	const sql = `
SELECT s.schemaname, s.relname, s.indexrelname, COALESCE(s.idx_scan,0),
       pg_relation_size(s.indexrelid)::bigint
FROM pg_stat_user_indexes s
JOIN pg_index i ON i.indexrelid = s.indexrelid
WHERE s.idx_scan = 0 AND NOT i.indisprimary AND NOT i.indisunique
ORDER BY pg_relation_size(s.indexrelid) DESC
LIMIT $1`
	rows, err := pool.Query(ctx, sql, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]UnusedIndex, 0, limit)
	for rows.Next() {
		var item UnusedIndex
		if err := rows.Scan(&item.Schema, &item.Table, &item.Index, &item.Scans, &item.Bytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// terminateBackend 结束会话（回滚其事务并断开）。
func terminateBackend(ctx context.Context, pool *pgxpool.Pool, pid int32) (bool, error) {
	var ok bool
	// 限定本库且非自身，避免误杀监控连接自己
	err := pool.QueryRow(ctx, `
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE pid = $1 AND datname = current_database() AND pid <> pg_backend_pid()`, pid).Scan(&ok)
	return ok, err
}

// cancelBackend 取消会话正在执行的语句（保留连接与事务）。
func cancelBackend(ctx context.Context, pool *pgxpool.Pool, pid int32) (bool, error) {
	var ok bool
	err := pool.QueryRow(ctx, `
SELECT pg_cancel_backend(pid) FROM pg_stat_activity
WHERE pid = $1 AND datname = current_database() AND pid <> pg_backend_pid()`, pid).Scan(&ok)
	return ok, err
}

// reapIdleInTransaction 终止超过阈值的 idle in transaction 会话，返回被终止的 PID。
//
// 这是最后一道闸：会话级 idle_in_transaction_session_timeout 只能管住我们自己建的连接，
// 管不住其它客户端（psql、上一次部署的残留进程、外部 ETL）留下的悬挂事务。
func reapIdleInTransaction(ctx context.Context, pool *pgxpool.Pool, threshold time.Duration) ([]int32, error) {
	if threshold <= 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
SELECT pid FROM pg_stat_activity
WHERE datname = current_database()
  AND state LIKE 'idle in transaction%'
  AND pid <> pg_backend_pid()
  AND state_change < NOW() - make_interval(secs => $1)`, threshold.Seconds())
	if err != nil {
		return nil, err
	}
	pids := make([]int32, 0, 4)
	for rows.Next() {
		var pid int32
		if err := rows.Scan(&pid); err != nil {
			rows.Close()
			return nil, err
		}
		pids = append(pids, pid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	killed := make([]int32, 0, len(pids))
	for _, pid := range pids {
		if ok, err := terminateBackend(ctx, pool, pid); err == nil && ok {
			killed = append(killed, pid)
		}
	}
	return killed, nil
}
