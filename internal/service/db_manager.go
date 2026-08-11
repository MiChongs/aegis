package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"aegis/internal/config"
	"aegis/internal/db"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5/pgxpool"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// PoolMetrics 连接池指标（pgxpool.Stat 的可序列化投影）
type PoolMetrics struct {
	MaxConns             int32   `json:"maxConns"`
	TotalConns           int32   `json:"totalConns"`
	AcquiredConns        int32   `json:"acquiredConns"`
	IdleConns            int32   `json:"idleConns"`
	ConstructingConns    int32   `json:"constructingConns"`
	AcquireCount         int64   `json:"acquireCount"`
	AcquireDurationMs    int64   `json:"acquireDurationMs"`
	AvgAcquireWaitMs     float64 `json:"avgAcquireWaitMs"`
	EmptyAcquireCount    int64   `json:"emptyAcquireCount"`
	CanceledAcquireCount int64   `json:"canceledAcquireCount"`
	NewConnsCount        int64   `json:"newConnsCount"`
	MaxLifetimeDestroy   int64   `json:"maxLifetimeDestroyCount"`
	MaxIdleDestroy       int64   `json:"maxIdleDestroyCount"`
	UsagePct             float64 `json:"usagePct"`
}

// RedisPoolMetrics Redis 连接池指标（同属数据层，一并纳入生命周期视图）
type RedisPoolMetrics struct {
	Hits       uint32 `json:"hits"`
	Misses     uint32 `json:"misses"`
	Timeouts   uint32 `json:"timeouts"`
	TotalConns uint32 `json:"totalConns"`
	IdleConns  uint32 `json:"idleConns"`
	StaleConns uint32 `json:"staleConns"`
}

// DatabaseMetrics 一次采样的完整指标
type DatabaseMetrics struct {
	Timestamp  time.Time          `json:"timestamp"`
	Healthy    bool               `json:"healthy"`
	PingMs     int64              `json:"pingMs"`
	Pool       PoolMetrics        `json:"pool"`
	Instrument db.InstrumentStats `json:"instrument"`
	Server     ServerMetrics      `json:"server"`
	Redis      *RedisPoolMetrics  `json:"redis,omitempty"`
}

// DatabaseAlert 告警条目
type DatabaseAlert struct {
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	At        time.Time `json:"at"`
}

// DatabaseLifecycleView 生命周期配置的生效值（供管理端确认「实际在跑什么」）
type DatabaseLifecycleView struct {
	MaxConns              int32             `json:"maxConns"`
	MinConns              int32             `json:"minConns"`
	MaxConnLifetime       string            `json:"maxConnLifetime"`
	MaxConnLifetimeJitter string            `json:"maxConnLifetimeJitter"`
	MaxConnIdleTime       string            `json:"maxConnIdleTime"`
	HealthCheckPeriod     string            `json:"healthCheckPeriod"`
	ConnectTimeout        string            `json:"connectTimeout"`
	SessionSettings       map[string]string `json:"sessionSettings"`
	MonitorInterval       string            `json:"monitorInterval"`
	SlowQueryThreshold    string            `json:"slowQueryThreshold"`
	ConnHoldThreshold     string            `json:"connHoldThreshold"`
	IdleInTxThreshold     string            `json:"idleInTxThreshold"`
	LongQueryThreshold    string            `json:"longQueryThreshold"`
	TrackAcquireStack     bool              `json:"trackAcquireStack"`
	AutoTerminateIdleInTx bool              `json:"autoTerminateIdleInTx"`
	AutoTerminateAfter    string            `json:"autoTerminateAfter"`
	DrainTimeout          string            `json:"drainTimeout"`
	WarmupOnStart         bool              `json:"warmupOnStart"`
}

// DatabaseSnapshot 完整快照
type DatabaseSnapshot struct {
	Timestamp    time.Time             `json:"timestamp"`
	Metrics      *DatabaseMetrics      `json:"metrics,omitempty"`
	Leak         *DBLeakReport         `json:"leak,omitempty"`
	Alerts       []DatabaseAlert       `json:"alerts"`
	SlowQueries  []db.SlowQuerySample  `json:"slowQueries"`
	InFlight     []db.InFlightQuery    `json:"inFlight"`
	LeakSuspects []db.ConnLeakSuspect  `json:"leakSuspects"`
	Lifecycle    DatabaseLifecycleView `json:"lifecycle"`
}

// DatabaseManager 数据库生命周期与泄漏监控的统一入口。
//
// 与 MemoryManager 同构：构造即可用，Start 拉起后台采集，Stop 收敛 goroutine，
// Snapshot 是纯读操作（读缓存 + 进程内计数），不打库。
// 只有显式的 Refresh / 会话列表 / 维护视图才会真正查询数据库。
type DatabaseManager struct {
	log   *zap.Logger
	cfg   config.DatabaseConfig
	pgCfg config.PostgresConfig
	// role 区分同进程内的多个池（Unified 模式下 API 与 Worker 各有一个）。
	// 只有 primary 角色写历史时序并运行清道夫，避免两个实例互相覆盖 / 重复终止会话。
	role     string
	handle   *db.Postgres
	redis    *redislib.Client
	prefix   string
	detector *dbTrendDetector

	mu     sync.RWMutex
	latest *DatabaseMetrics
	leak   *DBLeakReport
	alerts []DatabaseAlert

	cancel   context.CancelFunc
	stopOnce sync.Once
}

const databaseAlertRingSize = 50

// DatabaseRolePrimary 主角色：负责写历史时序与运行 idle-in-transaction 清道夫
const DatabaseRolePrimary = "api"

func NewDatabaseManager(log *zap.Logger, cfg config.DatabaseConfig, pgCfg config.PostgresConfig, handle *db.Postgres, redis *redislib.Client, keyPrefix, role string) *DatabaseManager {
	if log == nil {
		log = zap.NewNop()
	}
	if role == "" {
		role = DatabaseRolePrimary
	}
	manager := &DatabaseManager{
		log: log, cfg: cfg, pgCfg: pgCfg, role: role, handle: handle,
		redis: redis, prefix: keyPrefix, alerts: make([]DatabaseAlert, 0, databaseAlertRingSize),
	}
	if cfg.LeakDetection {
		manager.detector = newDBTrendDetector(cfg.LeakWindow)
	}
	return manager
}

// Start 拉起后台采集循环与 idle-in-transaction 清道夫。
func (m *DatabaseManager) Start(ctx context.Context) {
	if m == nil || m.handle == nil {
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)

	if m.cfg.MonitorEnabled {
		go m.monitorLoop(ctx)
		m.log.Info("数据库监控已启动",
			zap.String("role", m.role),
			zap.Duration("interval", m.cfg.MonitorInterval),
			zap.Bool("leakDetection", m.cfg.LeakDetection),
			zap.Bool("trackAcquireStack", m.cfg.TrackAcquireStack),
		)
	}
	// 清道夫会终止别人的会话，同进程内只需一个实例执行
	if m.cfg.AutoTerminateIdleInTx && m.isPrimary() {
		go m.reaperLoop(ctx)
		m.log.Warn("数据库清道夫已启用：超时的 idle in transaction 会话将被自动终止",
			zap.Duration("threshold", m.cfg.AutoTerminateThreshold))
	}
}

// Stop 停止后台循环（幂等）。
func (m *DatabaseManager) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
		}
		m.log.Info("数据库监控已停止")
	})
}

// Snapshot 纯读快照：读最近一次采样结果 + 进程内实时计数，不产生数据库往返。
func (m *DatabaseManager) Snapshot() DatabaseSnapshot {
	snapshot := DatabaseSnapshot{
		Timestamp:    time.Now().UTC(),
		Alerts:       []DatabaseAlert{},
		SlowQueries:  []db.SlowQuerySample{},
		InFlight:     []db.InFlightQuery{},
		LeakSuspects: []db.ConnLeakSuspect{},
		Lifecycle:    m.lifecycleView(),
	}
	if m == nil || m.handle == nil {
		return snapshot
	}
	m.mu.RLock()
	if m.latest != nil {
		copied := *m.latest
		snapshot.Metrics = &copied
	}
	if m.leak != nil {
		copiedLeak := *m.leak
		snapshot.Leak = &copiedLeak
	}
	snapshot.Alerts = append(snapshot.Alerts, m.alerts...)
	m.mu.RUnlock()

	instrument := m.handle.Instrument
	snapshot.SlowQueries = instrument.SlowQuerySamples()
	snapshot.InFlight = instrument.InFlightQueries(20)
	snapshot.LeakSuspects = instrument.LeakSuspects(20)
	return snapshot
}

// Refresh 立即采集一次并返回结果（管理端「刷新」按钮）。
func (m *DatabaseManager) Refresh(ctx context.Context) (*DatabaseMetrics, error) {
	if m == nil || m.handle == nil {
		return nil, apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	metric := m.collect(ctx)
	m.store(ctx, metric)
	return &metric, nil
}

// History 读取 Redis 中的历史指标。
func (m *DatabaseManager) History(ctx context.Context, rangeStr string) ([]DatabaseMetrics, error) {
	if m == nil || m.redis == nil {
		return []DatabaseMetrics{}, nil
	}
	duration := parseRange(rangeStr)
	now := time.Now()
	results, err := m.redis.ZRangeByScore(ctx, m.historyKey(), &redislib.ZRangeBy{
		Min: fmt.Sprintf("%d", now.Add(-duration).UnixMilli()),
		Max: fmt.Sprintf("%d", now.UnixMilli()),
	}).Result()
	if err != nil {
		return nil, err
	}
	metrics := make([]DatabaseMetrics, 0, len(results))
	for _, raw := range results {
		var item DatabaseMetrics
		if json.Unmarshal([]byte(raw), &item) == nil {
			metrics = append(metrics, item)
		}
	}
	return metrics, nil
}

// Sessions 列出服务端会话。
func (m *DatabaseManager) Sessions(ctx context.Context, onlyProblematic bool, limit int) ([]DBSession, error) {
	if m == nil || m.handle == nil {
		return nil, apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	return listSessions(ctx, m.handle.Pool, onlyProblematic, limit)
}

// Maintenance 返回存储侧健康视图（死元组表 + 未使用索引）。
func (m *DatabaseManager) Maintenance(ctx context.Context, limit int) (map[string]any, error) {
	if m == nil || m.handle == nil {
		return nil, apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	bloated, err := listBloatedTables(ctx, m.handle.Pool, limit)
	if err != nil {
		return nil, err
	}
	unused, err := listUnusedIndexes(ctx, m.handle.Pool, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"bloatedTables": bloated, "unusedIndexes": unused}, nil
}

// TerminateSession 结束指定会话（回滚其事务）。
func (m *DatabaseManager) TerminateSession(ctx context.Context, pid int32) error {
	if m == nil || m.handle == nil {
		return apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	ok, err := terminateBackend(ctx, m.handle.Pool, pid)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40431, http.StatusNotFound, "会话不存在或不属于当前数据库")
	}
	m.log.Warn("管理员终止数据库会话", zap.Int32("pid", pid))
	return nil
}

// CancelSession 取消指定会话正在执行的语句（保留连接）。
func (m *DatabaseManager) CancelSession(ctx context.Context, pid int32) error {
	if m == nil || m.handle == nil {
		return apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	ok, err := cancelBackend(ctx, m.handle.Pool, pid)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40431, http.StatusNotFound, "会话不存在或不属于当前数据库")
	}
	m.log.Warn("管理员取消数据库语句", zap.Int32("pid", pid))
	return nil
}

// Warmup 手动预热连接池。
func (m *DatabaseManager) Warmup(ctx context.Context) error {
	if m == nil || m.handle == nil {
		return apperrors.New(50330, http.StatusServiceUnavailable, "数据库监控服务不可用")
	}
	return m.handle.Warmup(ctx)
}

// ── 内部 ──

func (m *DatabaseManager) monitorLoop(ctx context.Context) {
	interval := m.cfg.MonitorInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.store(ctx, m.collect(ctx))
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 每轮采集独立设限，避免库侧卡顿把采集 goroutine 一起拖住
			collectCtx, cancel := context.WithTimeout(context.Background(), interval)
			m.store(collectCtx, m.collect(collectCtx))
			cancel()
		}
	}
}

// reaperLoop 定期清理超时的 idle in transaction 会话
func (m *DatabaseManager) reaperLoop(ctx context.Context) {
	interval := m.cfg.MonitorInterval
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reapCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			killed, err := reapIdleInTransaction(reapCtx, m.handle.Pool, m.cfg.AutoTerminateThreshold)
			cancel()
			if err != nil {
				m.log.Warn("清理 idle in transaction 会话失败", zap.Error(err))
				continue
			}
			if len(killed) > 0 {
				m.log.Warn("已自动终止超时的 idle in transaction 会话",
					zap.Int32s("pids", killed),
					zap.Duration("threshold", m.cfg.AutoTerminateThreshold))
			}
		}
	}
}

func (m *DatabaseManager) collect(ctx context.Context) DatabaseMetrics {
	metric := DatabaseMetrics{Timestamp: time.Now().UTC()}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	latency, err := m.handle.Ping(pingCtx)
	cancel()
	metric.Healthy = err == nil
	metric.PingMs = latency.Milliseconds()

	stat := m.handle.Pool.Stat()
	metric.Pool = PoolMetrics{
		MaxConns: stat.MaxConns(), TotalConns: stat.TotalConns(),
		AcquiredConns: stat.AcquiredConns(), IdleConns: stat.IdleConns(),
		ConstructingConns: stat.ConstructingConns(), AcquireCount: stat.AcquireCount(),
		AcquireDurationMs:    stat.AcquireDuration().Milliseconds(),
		EmptyAcquireCount:    stat.EmptyAcquireCount(),
		CanceledAcquireCount: stat.CanceledAcquireCount(),
		NewConnsCount:        stat.NewConnsCount(),
		MaxLifetimeDestroy:   stat.MaxLifetimeDestroyCount(),
		MaxIdleDestroy:       stat.MaxIdleDestroyCount(),
	}
	if stat.AcquireCount() > 0 {
		metric.Pool.AvgAcquireWaitMs = float64(stat.AcquireDuration().Microseconds()) / float64(stat.AcquireCount()) / 1000
	}
	if stat.MaxConns() > 0 {
		metric.Pool.UsagePct = float64(stat.AcquiredConns()) / float64(stat.MaxConns()) * 100
	}

	metric.Instrument = m.handle.Instrument.Stats()
	if metric.Healthy {
		metric.Server = collectServerMetrics(ctx, m.handle.Pool)
	}
	if m.redis != nil {
		stats := m.redis.PoolStats()
		metric.Redis = &RedisPoolMetrics{
			Hits: stats.Hits, Misses: stats.Misses, Timeouts: stats.Timeouts,
			TotalConns: stats.TotalConns, IdleConns: stats.IdleConns, StaleConns: stats.StaleConns,
		}
	}
	return metric
}

func (m *DatabaseManager) store(ctx context.Context, metric DatabaseMetrics) {
	// 先做一次泄漏扫描：把新出现的连接泄漏现场打进日志（每条只报一次）
	m.handle.Instrument.SweepLeaks()

	report := m.buildLeakReport(metric)

	m.mu.Lock()
	m.latest = &metric
	m.leak = report
	m.mu.Unlock()

	m.checkAlerts(metric, report)

	// 非主角色只做本地采集与泄漏日志，不写历史，避免两个池的样本互相覆盖
	if m.redis == nil || !m.isPrimary() {
		return
	}
	data, err := json.Marshal(metric)
	if err != nil {
		return
	}
	retain := m.cfg.HistoryRetain
	if retain <= 0 {
		retain = time.Hour
	}
	pipe := m.redis.Pipeline()
	pipe.ZAdd(ctx, m.historyKey(), redislib.Z{Score: float64(metric.Timestamp.UnixMilli()), Member: string(data)})
	pipe.ZRemRangeByScore(ctx, m.historyKey(), "-inf",
		fmt.Sprintf("%d", metric.Timestamp.Add(-retain).UnixMilli()))
	if _, err := pipe.Exec(ctx); err != nil {
		m.log.Debug("数据库监控：写入历史指标失败", zap.Error(err))
	}
}

func (m *DatabaseManager) buildLeakReport(metric DatabaseMetrics) *DBLeakReport {
	if !m.cfg.LeakDetection {
		return nil
	}
	now := time.Now().UTC()
	suspects := m.handle.Instrument.LeakSuspects(20)
	findings := analyzeDBLeaks(metric, suspects, m.cfg, now)

	var trends []LeakIndicator
	if m.detector != nil {
		m.detector.record(metric)
		trends = m.detector.indicators(now)
		for _, indicator := range trends {
			if !indicator.SuspectedLeak {
				continue
			}
			findings = append(findings, DBLeakFinding{
				Kind: LeakKindTrend, Severity: LeakSeverityInfo,
				Title: "指标持续上升：" + indicator.Name, Detail: indicator.AlertMessage,
				Advice:     "该指标尚未越过硬阈值，但趋势单调；结合上方确定性结论一起看，可在故障前介入。",
				DetectedAt: now,
			})
		}
	}

	report := &DBLeakReport{CheckedAt: now, Findings: findings, Trends: trends}
	for _, finding := range findings {
		switch finding.Severity {
		case LeakSeverityCritical:
			report.Critical++
		case LeakSeverityWarning:
			report.Warning++
		}
	}
	report.Suspicious = report.Critical > 0 || report.Warning > 0
	switch {
	case report.Critical > 0:
		report.Summary = fmt.Sprintf("发现 %d 项严重泄漏问题、%d 项警告，需立即处理。", report.Critical, report.Warning)
	case report.Warning > 0:
		report.Summary = fmt.Sprintf("发现 %d 项警告级泄漏迹象，建议尽快确认。", report.Warning)
	case len(findings) > 0:
		report.Summary = "存在提示级观察项，暂不影响服务。"
	default:
		report.Summary = "未检测到连接、事务、快照、WAL 或存储层面的泄漏迹象。"
	}
	return report
}

func (m *DatabaseManager) checkAlerts(metric DatabaseMetrics, report *DBLeakReport) {
	now := time.Now().UTC()
	appendAlert := func(level, message, name string, value, threshold float64) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.alerts = append(m.alerts, DatabaseAlert{
			Level: level, Message: message, Metric: name,
			Value: value, Threshold: threshold, At: now,
		})
		if len(m.alerts) > databaseAlertRingSize {
			m.alerts = m.alerts[len(m.alerts)-databaseAlertRingSize:]
		}
	}

	if !metric.Healthy {
		appendAlert("critical", "数据库健康探测失败", "healthy", 0, 1)
	}
	if metric.Pool.UsagePct >= 90 {
		appendAlert("critical", fmt.Sprintf("连接池使用率 %.0f%%，接近耗尽", metric.Pool.UsagePct),
			"pool_usage_pct", metric.Pool.UsagePct, 90)
	} else if metric.Pool.UsagePct >= 75 {
		appendAlert("warning", fmt.Sprintf("连接池使用率 %.0f%%", metric.Pool.UsagePct),
			"pool_usage_pct", metric.Pool.UsagePct, 75)
	}
	if metric.Healthy && metric.Server.CacheHitRatio > 0 && metric.Server.CacheHitRatio < 90 {
		appendAlert("warning", fmt.Sprintf("共享缓冲命中率 %.1f%%，磁盘读偏多", metric.Server.CacheHitRatio),
			"cache_hit_ratio", metric.Server.CacheHitRatio, 90)
	}
	if m.cfg.LongQueryThreshold > 0 && metric.Server.OldestQuerySeconds >= m.cfg.LongQueryThreshold.Seconds() {
		appendAlert("warning", fmt.Sprintf("存在已执行 %.0fs 的语句", metric.Server.OldestQuerySeconds),
			"oldest_query_seconds", metric.Server.OldestQuerySeconds, m.cfg.LongQueryThreshold.Seconds())
	}
	if report != nil && report.Critical > 0 {
		appendAlert("critical", report.Summary, "leak_findings", float64(report.Critical), 0)
	}
}

func (m *DatabaseManager) lifecycleView() DatabaseLifecycleView {
	view := DatabaseLifecycleView{
		MaxConns:              m.pgCfg.MaxConns,
		MinConns:              m.pgCfg.MinConns,
		MaxConnLifetime:       m.pgCfg.MaxConnLifetime.String(),
		MaxConnLifetimeJitter: m.pgCfg.MaxConnLifetimeJitter.String(),
		MaxConnIdleTime:       m.pgCfg.MaxConnIdleTime.String(),
		HealthCheckPeriod:     m.pgCfg.HealthCheckPeriod.String(),
		ConnectTimeout:        m.pgCfg.ConnectTimeout.String(),
		SessionSettings:       map[string]string{},
		MonitorInterval:       m.cfg.MonitorInterval.String(),
		SlowQueryThreshold:    m.cfg.SlowQueryThreshold.String(),
		ConnHoldThreshold:     m.cfg.ConnHoldThreshold.String(),
		IdleInTxThreshold:     m.cfg.IdleInTxThreshold.String(),
		LongQueryThreshold:    m.cfg.LongQueryThreshold.String(),
		TrackAcquireStack:     m.cfg.TrackAcquireStack,
		AutoTerminateIdleInTx: m.cfg.AutoTerminateIdleInTx,
		AutoTerminateAfter:    m.cfg.AutoTerminateThreshold.String(),
		DrainTimeout:          m.cfg.DrainTimeout.String(),
		WarmupOnStart:         m.cfg.WarmupOnStart,
	}
	if m.handle != nil {
		view.SessionSettings = m.handle.SessionSettings()
	}
	return view
}

func (m *DatabaseManager) isPrimary() bool { return m.role == DatabaseRolePrimary }

func (m *DatabaseManager) historyKey() string {
	prefix := m.prefix
	if prefix == "" {
		prefix = "aegis"
	}
	return prefix + ":database:history"
}

// 保证 pgxpool 依赖被显式引用（PoolMetrics 由 *pgxpool.Stat 投影而来）
var _ = (*pgxpool.Pool)(nil)
