package db

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// PoolInstrument 连接池的进程内观测装置。
//
// 它同时挂在三个位置，覆盖「一次数据库交互」的完整生命周期：
//
//	BeforeAcquire / AfterRelease  → 连接被借出到归还的这段持有期
//	TraceQueryStart / End         → 单条语句的执行期
//	TraceConnectStart / End       → 物理建连
//
// 之所以要在进程内做这件事而不是只查 pg_stat_activity：
// 服务端只知道「哪个连接在跑什么」，不知道**是哪段 Go 代码借走了连接却没还**。
// 连接泄漏的根因几乎总在后者（忘记 Release、rows 未 Close、err 分支提前 return），
// 因此这里在 acquire 时抓调用栈——那是唯一能把问题定位到代码行的信息。
type PoolInstrument struct {
	log *zap.Logger
	cfg config.DatabaseConfig

	// 借出中的连接：key 为 *pgx.Conn 指针（池内连接对象在其生命周期内地址稳定）
	checkedOut sync.Map // map[*pgx.Conn]*checkoutRecord
	// 在途语句：key 为自增 token
	inFlight sync.Map // map[uint64]*queryRecord

	seq atomic.Uint64

	// ── 计数器（原子，读侧零锁） ──
	acquireTotal   atomic.Int64
	releaseTotal   atomic.Int64
	queryTotal     atomic.Int64
	queryErrors    atomic.Int64
	slowQueries    atomic.Int64
	verySlowQuery  atomic.Int64
	connectTotal   atomic.Int64
	connectErrors  atomic.Int64
	totalQueryNano atomic.Int64
	maxQueryNano   atomic.Int64

	// 慢查询样本环形缓冲
	sampleMu   sync.Mutex
	samples    []SlowQuerySample
	sampleIdx  int
	sampleFull bool
}

// checkoutRecord 一次连接借出的现场
type checkoutRecord struct {
	AcquiredAt time.Time
	PID        uint32
	Stack      string
	// reported 已上报过的泄漏不重复刷屏
	reported atomic.Bool
}

// queryRecord 一条在途语句的现场
type queryRecord struct {
	SQL       string
	StartedAt time.Time
	PID       uint32
}

// SlowQuerySample 慢查询样本
type SlowQuerySample struct {
	SQL        string    `json:"sql"`
	DurationMs int64     `json:"durationMs"`
	Rows       int64     `json:"rows"`
	Err        string    `json:"err,omitempty"`
	PID        uint32    `json:"pid,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// ConnLeakSuspect 疑似连接泄漏：连接被借出后长时间未归还
type ConnLeakSuspect struct {
	PID      uint32    `json:"pid"`
	HeldMs   int64     `json:"heldMs"`
	Since    time.Time `json:"since"`
	Stack    string    `json:"stack,omitempty"`
	Reported bool      `json:"reported"`
}

// InFlightQuery 在途语句
type InFlightQuery struct {
	SQL       string    `json:"sql"`
	ElapsedMs int64     `json:"elapsedMs"`
	StartedAt time.Time `json:"startedAt"`
	PID       uint32    `json:"pid,omitempty"`
}

// InstrumentStats 进程侧统计快照
type InstrumentStats struct {
	AcquireTotal      int64 `json:"acquireTotal"`
	ReleaseTotal      int64 `json:"releaseTotal"`
	CheckedOut        int64 `json:"checkedOut"`
	QueryTotal        int64 `json:"queryTotal"`
	QueryErrors       int64 `json:"queryErrors"`
	SlowQueries       int64 `json:"slowQueries"`
	VerySlowQueries   int64 `json:"verySlowQueries"`
	InFlightQueries   int64 `json:"inFlightQueries"`
	ConnectTotal      int64 `json:"connectTotal"`
	ConnectErrors     int64 `json:"connectErrors"`
	AvgQueryMs        int64 `json:"avgQueryMs"`
	MaxQueryMs        int64 `json:"maxQueryMs"`
	LeakSuspectCount  int64 `json:"leakSuspectCount"`
	OldestCheckoutMs  int64 `json:"oldestCheckoutMs"`
	OldestInFlightMs  int64 `json:"oldestInFlightMs"`
	TrackAcquireStack bool  `json:"trackAcquireStack"`
}

func NewPoolInstrument(log *zap.Logger, cfg config.DatabaseConfig) *PoolInstrument {
	if log == nil {
		log = zap.NewNop()
	}
	size := cfg.SlowQuerySampleSize
	if size <= 0 {
		size = 50
	}
	return &PoolInstrument{log: log, cfg: cfg, samples: make([]SlowQuerySample, size)}
}

// ── pgxpool 生命周期钩子 ──

// beforeAcquire 连接借出前登记现场。返回 false 会让池丢弃该连接并换一条。
func (p *PoolInstrument) beforeAcquire(_ context.Context, conn *pgx.Conn) bool {
	record := &checkoutRecord{AcquiredAt: time.Now(), PID: conn.PgConn().PID()}
	if p.cfg.TrackAcquireStack {
		record.Stack = captureStack()
	}
	p.checkedOut.Store(conn, record)
	p.acquireTotal.Add(1)
	return true
}

// afterRelease 连接归还时销案。返回 false 表示销毁该连接而非放回池中。
func (p *PoolInstrument) afterRelease(conn *pgx.Conn) bool {
	p.checkedOut.Delete(conn)
	p.releaseTotal.Add(1)
	return true
}

// beforeClose 连接被池销毁（超龄/超空闲/健康检查失败）时清理残留登记
func (p *PoolInstrument) beforeClose(conn *pgx.Conn) {
	p.checkedOut.Delete(conn)
}

// ── pgx.QueryTracer / BatchTracer / ConnectTracer ──

type traceTokenKey struct{}

func (p *PoolInstrument) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	token := p.seq.Add(1)
	var pid uint32
	if conn != nil && conn.PgConn() != nil {
		pid = conn.PgConn().PID()
	}
	p.inFlight.Store(token, &queryRecord{SQL: data.SQL, StartedAt: time.Now(), PID: pid})
	return context.WithValue(ctx, traceTokenKey{}, token)
}

func (p *PoolInstrument) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	token, ok := ctx.Value(traceTokenKey{}).(uint64)
	if !ok {
		return
	}
	value, loaded := p.inFlight.LoadAndDelete(token)
	if !loaded {
		return
	}
	record := value.(*queryRecord)
	elapsed := time.Since(record.StartedAt)

	p.queryTotal.Add(1)
	p.totalQueryNano.Add(int64(elapsed))
	for {
		current := p.maxQueryNano.Load()
		if int64(elapsed) <= current || p.maxQueryNano.CompareAndSwap(current, int64(elapsed)) {
			break
		}
	}
	if data.Err != nil {
		p.queryErrors.Add(1)
	}
	if p.cfg.SlowQueryThreshold <= 0 || elapsed < p.cfg.SlowQueryThreshold {
		return
	}

	p.slowQueries.Add(1)
	verySlow := p.cfg.VerySlowQueryThreshold > 0 && elapsed >= p.cfg.VerySlowQueryThreshold
	if verySlow {
		p.verySlowQuery.Add(1)
	}
	sample := SlowQuerySample{
		SQL:        normalizeSQL(record.SQL),
		DurationMs: elapsed.Milliseconds(),
		Rows:       data.CommandTag.RowsAffected(),
		PID:        record.PID,
		OccurredAt: record.StartedAt,
	}
	if data.Err != nil {
		sample.Err = truncate(data.Err.Error(), 256)
	}
	p.recordSample(sample)

	fields := []zap.Field{
		zap.String("sql", sample.SQL),
		zap.Int64("durationMs", sample.DurationMs),
		zap.Uint32("pid", sample.PID),
	}
	if verySlow {
		p.log.Warn("数据库极慢查询", fields...)
	} else {
		p.log.Info("数据库慢查询", fields...)
	}
}

func (p *PoolInstrument) TraceBatchStart(ctx context.Context, conn *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return p.TraceQueryStart(ctx, conn, pgx.TraceQueryStartData{SQL: "[batch]"})
}

func (p *PoolInstrument) TraceBatchQuery(context.Context, *pgx.Conn, pgx.TraceBatchQueryData) {}

func (p *PoolInstrument) TraceBatchEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceBatchEndData) {
	p.TraceQueryEnd(ctx, conn, pgx.TraceQueryEndData{Err: data.Err})
}

func (p *PoolInstrument) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	p.connectTotal.Add(1)
	return ctx
}

func (p *PoolInstrument) TraceConnectEnd(_ context.Context, data pgx.TraceConnectEndData) {
	if data.Err != nil {
		p.connectErrors.Add(1)
		p.log.Warn("数据库建连失败", zap.Error(data.Err))
	}
}

// ── 查询接口 ──

// Stats 汇总进程侧指标（一次遍历，纯内存）
func (p *PoolInstrument) Stats() InstrumentStats {
	now := time.Now()
	var checkedOut, oldestCheckout, leakCount int64
	p.checkedOut.Range(func(_, value any) bool {
		record := value.(*checkoutRecord)
		checkedOut++
		held := now.Sub(record.AcquiredAt).Milliseconds()
		if held > oldestCheckout {
			oldestCheckout = held
		}
		if p.cfg.ConnHoldThreshold > 0 && now.Sub(record.AcquiredAt) >= p.cfg.ConnHoldThreshold {
			leakCount++
		}
		return true
	})
	var inFlight, oldestInFlight int64
	p.inFlight.Range(func(_, value any) bool {
		record := value.(*queryRecord)
		inFlight++
		elapsed := now.Sub(record.StartedAt).Milliseconds()
		if elapsed > oldestInFlight {
			oldestInFlight = elapsed
		}
		return true
	})

	total := p.queryTotal.Load()
	var avg int64
	if total > 0 {
		avg = (p.totalQueryNano.Load() / total) / int64(time.Millisecond)
	}
	return InstrumentStats{
		AcquireTotal:      p.acquireTotal.Load(),
		ReleaseTotal:      p.releaseTotal.Load(),
		CheckedOut:        checkedOut,
		QueryTotal:        total,
		QueryErrors:       p.queryErrors.Load(),
		SlowQueries:       p.slowQueries.Load(),
		VerySlowQueries:   p.verySlowQuery.Load(),
		InFlightQueries:   inFlight,
		ConnectTotal:      p.connectTotal.Load(),
		ConnectErrors:     p.connectErrors.Load(),
		AvgQueryMs:        avg,
		MaxQueryMs:        p.maxQueryNano.Load() / int64(time.Millisecond),
		LeakSuspectCount:  leakCount,
		OldestCheckoutMs:  oldestCheckout,
		OldestInFlightMs:  oldestInFlight,
		TrackAcquireStack: p.cfg.TrackAcquireStack,
	}
}

// LeakSuspects 返回持有时长超过阈值的连接现场，按持有时长倒序。
func (p *PoolInstrument) LeakSuspects(limit int) []ConnLeakSuspect {
	if p.cfg.ConnHoldThreshold <= 0 {
		return nil
	}
	now := time.Now()
	suspects := make([]ConnLeakSuspect, 0, 8)
	p.checkedOut.Range(func(_, value any) bool {
		record := value.(*checkoutRecord)
		held := now.Sub(record.AcquiredAt)
		if held < p.cfg.ConnHoldThreshold {
			return true
		}
		suspects = append(suspects, ConnLeakSuspect{
			PID: record.PID, HeldMs: held.Milliseconds(), Since: record.AcquiredAt,
			Stack: record.Stack, Reported: record.reported.Load(),
		})
		return true
	})
	sortDescByHeld(suspects)
	if limit > 0 && len(suspects) > limit {
		suspects = suspects[:limit]
	}
	return suspects
}

// InFlightQueries 返回在途语句，按已耗时倒序。
func (p *PoolInstrument) InFlightQueries(limit int) []InFlightQuery {
	now := time.Now()
	items := make([]InFlightQuery, 0, 8)
	p.inFlight.Range(func(_, value any) bool {
		record := value.(*queryRecord)
		items = append(items, InFlightQuery{
			SQL: normalizeSQL(record.SQL), ElapsedMs: now.Sub(record.StartedAt).Milliseconds(),
			StartedAt: record.StartedAt, PID: record.PID,
		})
		return true
	})
	sortDescByElapsed(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

// SlowQuerySamples 返回慢查询样本（最近的在前）
func (p *PoolInstrument) SlowQuerySamples() []SlowQuerySample {
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()
	size := len(p.samples)
	if size == 0 {
		return nil
	}
	count := p.sampleIdx
	if p.sampleFull {
		count = size
	}
	result := make([]SlowQuerySample, 0, count)
	for i := 0; i < count; i++ {
		// 从最新往回读
		idx := (p.sampleIdx - 1 - i + size*2) % size
		result = append(result, p.samples[idx])
	}
	return result
}

// SweepLeaks 扫描并对新出现的泄漏嫌疑打一次日志（每条现场只报一次，避免刷屏）。
// 返回本轮新增的嫌疑数量。
func (p *PoolInstrument) SweepLeaks() int {
	if p.cfg.ConnHoldThreshold <= 0 {
		return 0
	}
	now := time.Now()
	fresh := 0
	p.checkedOut.Range(func(_, value any) bool {
		record := value.(*checkoutRecord)
		held := now.Sub(record.AcquiredAt)
		if held < p.cfg.ConnHoldThreshold || record.reported.Load() {
			return true
		}
		if !record.reported.CompareAndSwap(false, true) {
			return true
		}
		fresh++
		fields := []zap.Field{
			zap.Uint32("pid", record.PID),
			zap.Duration("held", held),
			zap.Duration("threshold", p.cfg.ConnHoldThreshold),
		}
		if record.Stack != "" {
			fields = append(fields, zap.String("acquiredAt", record.Stack))
		}
		p.log.Warn("疑似数据库连接泄漏：连接借出后长时间未归还", fields...)
		return true
	})
	return fresh
}

func (p *PoolInstrument) recordSample(sample SlowQuerySample) {
	p.sampleMu.Lock()
	defer p.sampleMu.Unlock()
	if len(p.samples) == 0 {
		return
	}
	p.samples[p.sampleIdx] = sample
	p.sampleIdx = (p.sampleIdx + 1) % len(p.samples)
	if p.sampleIdx == 0 {
		p.sampleFull = true
	}
}

// captureStack 抓取调用栈并只保留项目自身的帧。
//
// 取舍：跳过 runtime/pgx/pgxpool 的帧后，剩下的第一条 aegis/... 就是借走连接的业务代码；
// 保留 12 帧足以还原调用链，又不至于让每条记录占用过多内存。
func captureStack() string {
	buffer := make([]byte, 8<<10)
	n := runtime.Stack(buffer, false)
	lines := strings.Split(string(buffer[:n]), "\n")
	kept := make([]string, 0, 24)
	for index := 0; index < len(lines)-1; index += 2 {
		function := strings.TrimSpace(lines[index])
		location := strings.TrimSpace(lines[index+1])
		if !strings.HasPrefix(function, "aegis/") {
			continue
		}
		// 跳过本文件与连接层自身的帧
		if strings.Contains(function, "aegis/internal/db.") {
			continue
		}
		kept = append(kept, function+" @ "+location)
		if len(kept) >= 12 {
			break
		}
	}
	if len(kept) == 0 {
		return truncate(string(buffer[:n]), 1024)
	}
	return strings.Join(kept, "\n")
}

// normalizeSQL 压平空白并截断，避免长 SQL 撑爆日志与响应体
func normalizeSQL(sql string) string {
	return truncate(strings.Join(strings.Fields(sql), " "), 512)
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func sortDescByHeld(items []ConnLeakSuspect) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].HeldMs > items[j-1].HeldMs; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func sortDescByElapsed(items []InFlightQuery) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ElapsedMs > items[j-1].ElapsedMs; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// attachInstrument 把观测装置挂到池配置上（连接生命周期钩子 + 语句 tracer）
func attachInstrument(poolCfg *pgxpool.Config, instrument *PoolInstrument) {
	if instrument == nil {
		return
	}
	poolCfg.BeforeAcquire = instrument.beforeAcquire
	poolCfg.AfterRelease = instrument.afterRelease
	poolCfg.BeforeClose = instrument.beforeClose
	poolCfg.ConnConfig.Tracer = instrument
}
