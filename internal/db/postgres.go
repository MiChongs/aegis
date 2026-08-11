package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aegis/internal/config"
	"aegis/pkg/timeutil"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Postgres 带生命周期治理的连接池句柄。
//
// 相比裸 *pgxpool.Pool 多出三件事：
//  1. 会话级超时（statement / idle_in_transaction / lock）在每条连接建立时就写死，
//     不依赖调用方自觉——这是「一条慢 SQL 拖垮整个池」的结构性防线；
//  2. 借出/归还/建连/执行全链路挂了观测装置，泄漏可定位到代码行；
//  3. Close 走排空流程，而不是把在途查询直接掐断。
type Postgres struct {
	Pool       *pgxpool.Pool
	Instrument *PoolInstrument
	cfg        config.PostgresConfig
	dbCfg      config.DatabaseConfig
	log        *zap.Logger
}

// NewPostgres 兼容旧签名：只建池，不带观测。
// 迁移期保留给仅需连接的场景（CLI 命令、一次性任务）。
func NewPostgres(ctx context.Context, cfg config.PostgresConfig) (*pgxpool.Pool, error) {
	handle, err := NewPostgresWithLifecycle(ctx, cfg, config.DatabaseConfig{}, nil)
	if err != nil {
		return nil, err
	}
	return handle.Pool, nil
}

// NewPostgresWithLifecycle 建池并挂载完整生命周期治理。
func NewPostgresWithLifecycle(ctx context.Context, cfg config.PostgresConfig, dbCfg config.DatabaseConfig, log *zap.Logger) (*Postgres, error) {
	if log == nil {
		log = zap.NewNop()
	}
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, err
	}

	// ── 池容量与连接寿命 ──
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	if cfg.MaxConnLifetimeJitter > 0 {
		// 抖动让同批建立的连接错峰到期，避免整池同时重连造成周期性抖动
		poolCfg.MaxConnLifetimeJitter = cfg.MaxConnLifetimeJitter
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	} else {
		poolCfg.MaxConnIdleTime = 5 * time.Minute
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}
	if cfg.ConnectTimeout > 0 {
		poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	}

	// ── 会话级参数：在连接建立时随启动包下发，对该连接上的所有语句生效 ──
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	sessionTimezone := strings.TrimSpace(cfg.SessionTimezone)
	if sessionTimezone == "" {
		sessionTimezone = "UTC"
	}
	if loc, err := timeutil.LoadLocation(sessionTimezone); err == nil {
		sessionTimezone = loc.String()
	}
	poolCfg.ConnConfig.RuntimeParams["timezone"] = sessionTimezone
	if name := strings.TrimSpace(cfg.ApplicationName); name != "" {
		// application_name 会出现在 pg_stat_activity，库侧排障时能一眼分清来源
		poolCfg.ConnConfig.RuntimeParams["application_name"] = name
	}
	if cfg.StatementTimeout > 0 {
		poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = millis(cfg.StatementTimeout)
	}
	if cfg.IdleInTxTimeout > 0 {
		poolCfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = millis(cfg.IdleInTxTimeout)
	}
	if cfg.LockTimeout > 0 {
		poolCfg.ConnConfig.RuntimeParams["lock_timeout"] = millis(cfg.LockTimeout)
	}

	instrument := NewPoolInstrument(log, dbCfg)
	attachInstrument(poolCfg, instrument)

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	handle := &Postgres{Pool: pool, Instrument: instrument, cfg: cfg, dbCfg: dbCfg, log: log}

	if dbCfg.WarmupOnStart {
		if err := handle.Warmup(ctx); err != nil {
			// 预热失败不阻断启动：连接会在首次请求时按需建立
			log.Warn("数据库连接池预热未完成", zap.Error(err))
		}
	}
	log.Info("数据库连接池已就绪",
		zap.Int32("maxConns", poolCfg.MaxConns),
		zap.Int32("minConns", poolCfg.MinConns),
		zap.Duration("maxConnLifetime", poolCfg.MaxConnLifetime),
		zap.Duration("maxConnIdleTime", poolCfg.MaxConnIdleTime),
		zap.Duration("statementTimeout", cfg.StatementTimeout),
		zap.Duration("idleInTxTimeout", cfg.IdleInTxTimeout),
	)
	return handle, nil
}

// Warmup 预热到 MinConns：把建连成本从「首个用户请求」挪到启动阶段。
func (p *Postgres) Warmup(ctx context.Context) error {
	target := int(p.cfg.MinConns)
	if target <= 0 {
		return nil
	}
	warmCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	conns := make([]*pgxpool.Conn, 0, target)
	defer func() {
		for _, conn := range conns {
			conn.Release()
		}
	}()
	for i := 0; i < target; i++ {
		conn, err := p.Pool.Acquire(warmCtx)
		if err != nil {
			return fmt.Errorf("预热第 %d 条连接失败: %w", i+1, err)
		}
		if err := conn.Ping(warmCtx); err != nil {
			return fmt.Errorf("预热第 %d 条连接 Ping 失败: %w", i+1, err)
		}
		conns = append(conns, conn)
	}
	p.log.Info("数据库连接池预热完成", zap.Int("conns", len(conns)))
	return nil
}

// Ping 健康探测，返回往返耗时。
func (p *Postgres) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	err := p.Pool.Ping(ctx)
	return time.Since(start), err
}

// Drain 优雅排空：等待在途语句自然结束，超时后再强制关闭。
//
// 直接 Close 会切断在途查询，导致收尾阶段出现本可避免的错误日志与半完成写入；
// 先排空能让绝大多数请求跑完，剩下的才承担被中断的代价。
func (p *Postgres) Drain(ctx context.Context) {
	timeout := p.dbCfg.DrainTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		stats := p.Instrument.Stats()
		if stats.InFlightQueries == 0 && p.Pool.Stat().AcquiredConns() == 0 {
			p.log.Info("数据库连接池已排空")
			return
		}
		if time.Now().After(deadline) {
			p.log.Warn("数据库连接池排空超时，强制关闭",
				zap.Int64("inFlightQueries", stats.InFlightQueries),
				zap.Int32("acquiredConns", p.Pool.Stat().AcquiredConns()),
				zap.Duration("timeout", timeout),
			)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Close 排空后关闭连接池。
func (p *Postgres) Close(ctx context.Context) {
	p.Drain(ctx)
	p.Pool.Close()
}

// AcquireWithTimeout 带超时的连接获取。
//
// 供确实需要长时间独占连接的场景（如 advisory lock、COPY）显式使用，
// 强制调用方给出上界，避免无限等待把 goroutine 堆起来。
func (p *Postgres) AcquireWithTimeout(ctx context.Context, timeout time.Duration) (*pgxpool.Conn, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	acquireCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return p.Pool.Acquire(acquireCtx)
}

// SessionSettings 返回实际下发到每条连接的会话参数，供管理端展示「当前生效值」。
func (p *Postgres) SessionSettings() map[string]string {
	settings := map[string]string{}
	for key, value := range p.Pool.Config().ConnConfig.RuntimeParams {
		settings[key] = value
	}
	return settings
}

// ExplainConn 便于诊断：返回连接在服务端的 backend PID。
func ExplainConn(conn *pgx.Conn) uint32 {
	if conn == nil || conn.PgConn() == nil {
		return 0
	}
	return conn.PgConn().PID()
}

func millis(d time.Duration) string {
	return fmt.Sprintf("%d", d.Milliseconds())
}
