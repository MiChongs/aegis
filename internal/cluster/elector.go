package cluster

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// DefaultLeaseTTL 租约有效期。持有者宕机后，最长这么久任务会被别的实例接手。
const DefaultLeaseTTL = 30 * time.Second

// leaseStore 抽象出租约的读写，便于单测替换。
type leaseStore interface {
	ensure(ctx context.Context) error
	// acquire 尝试获取或续期 name 的租约，返回本实例是否持有。
	acquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error)
	release(ctx context.Context, names []string, holder string) error
}

// Elector 用 Postgres 表 cluster_leases 为每个任务名选出一个负责实例。
//
// 为什么不用 advisory lock 做选主：advisory lock 绑在一条会话上，连接池回收、
// PgBouncer 事务模式、网络抖动都会让锁悄悄换手而持有方毫无察觉。租约是一行带
// 过期时间的数据，持有方每 TTL/3 续一次；续不上（数据库抖了）就在本地期限到时
// 主动认为自己已失去租约，宁可让任务空转一个周期，也不让两个实例同时干。
type Elector struct {
	store  leaseStore
	holder string
	ttl    time.Duration
	log    *zap.Logger

	mu     sync.RWMutex
	names  map[string]struct{}
	held   map[string]time.Time // name → 本地认定的到期时刻
	cancel context.CancelFunc
	done   chan struct{}
	now    func() time.Time
}

// NewElector 创建选主器。pool 为 nil 时返回 nil（调用方的 Gate 会一律放行）。
func NewElector(pool *pgxpool.Pool, holder string, ttl time.Duration, log *zap.Logger) *Elector {
	if pool == nil {
		return nil
	}
	return newElector(&pgLeaseStore{pool: pool}, holder, ttl, log)
}

func newElector(store leaseStore, holder string, ttl time.Duration, log *zap.Logger) *Elector {
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Elector{
		store:  store,
		holder: holder,
		ttl:    ttl,
		log:    log.Named("cluster"),
		names:  map[string]struct{}{},
		held:   map[string]time.Time{},
		now:    time.Now,
	}
}

// Gate 登记任务名并返回判定函数。可以在 Start 之前或之后调用。
//
// 判定是纯内存读，可以放在每秒都跑的循环里。Start 之后新登记的名字在下一轮续约时才会去抢，
// 期间返回 false。
func (e *Elector) Gate(name string) func() bool {
	if e == nil {
		return alwaysTrue
	}
	e.mu.Lock()
	e.names[name] = struct{}{}
	e.mu.Unlock()
	return func() bool { return e.Holding(name) }
}

// Holding 报告本实例此刻是否持有 name 的租约。
func (e *Elector) Holding(name string) bool {
	if e == nil {
		return true
	}
	e.mu.RLock()
	until, ok := e.held[name]
	e.mu.RUnlock()
	return ok && e.now().Before(until)
}

// Held 返回本实例当前持有的全部任务名（排好序），供横幅与状态接口展示。
func (e *Elector) Held() []string {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	out := make([]string, 0, len(e.held))
	now := e.now()
	for name, until := range e.held {
		if now.Before(until) {
			out = append(out, name)
		}
	}
	e.mu.RUnlock()
	sort.Strings(out)
	return out
}

// Start 建表并同步抢一轮租约，然后在后台每 TTL/3 续约一次。
//
// 同步抢第一轮是为了让启动后立刻要跑一次的任务（例如续跑存储迁移）在 Start 返回时就能知道归不归自己。
func (e *Elector) Start(ctx context.Context) error {
	if e == nil {
		return nil
	}
	if err := e.store.ensure(ctx); err != nil {
		return err
	}
	e.tick(ctx)
	loopCtx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.done = make(chan struct{})
	go e.loop(loopCtx)
	return nil
}

// Stop 停止续约并主动释放持有的租约，让别的实例立刻接手，而不是干等 TTL。
func (e *Elector) Stop(ctx context.Context) {
	if e == nil || e.cancel == nil {
		return
	}
	e.cancel()
	<-e.done
	e.mu.Lock()
	names := make([]string, 0, len(e.held))
	for name := range e.held {
		names = append(names, name)
	}
	e.held = map[string]time.Time{}
	e.mu.Unlock()
	if len(names) == 0 {
		return
	}
	if err := e.store.release(ctx, names, e.holder); err != nil {
		e.log.Warn("释放集群租约失败，其他实例将在租约过期后接手", zap.Strings("names", names), zap.Error(err))
	}
}

func (e *Elector) loop(ctx context.Context) {
	defer close(e.done)
	ticker := time.NewTicker(e.ttl / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Elector) tick(ctx context.Context) {
	e.mu.RLock()
	names := make([]string, 0, len(e.names))
	for name := range e.names {
		names = append(names, name)
	}
	e.mu.RUnlock()
	sort.Strings(names)

	for _, name := range names {
		started := e.now()
		callCtx, cancel := context.WithTimeout(ctx, e.ttl/3)
		ok, err := e.store.acquire(callCtx, name, e.holder, e.ttl)
		cancel()
		if err != nil {
			// 续不上不立刻放手：本地期限内仍认为持有（数据库里那行也还没过期，别人抢不走）。
			// 期限一到 Holding 自然返回 false。
			if ctx.Err() == nil {
				e.log.Warn("集群租约续期失败", zap.String("name", name), zap.Error(err))
			}
			continue
		}
		e.mu.Lock()
		_, had := e.held[name]
		if ok {
			// 本地期限比库里的短一截（扣掉这次调用的耗时与 1/6 TTL 余量），
			// 保证本实例认定的持有期一定落在库里那行的有效期之内。
			e.held[name] = started.Add(e.ttl - e.ttl/6)
		} else {
			delete(e.held, name)
		}
		e.mu.Unlock()
		switch {
		case ok && !had:
			e.log.Info("本实例接管集群任务", zap.String("name", name), zap.String("node", e.holder))
		case !ok && had:
			e.log.Info("集群任务已由其他实例负责", zap.String("name", name))
		}
	}
}

type pgLeaseStore struct {
	pool *pgxpool.Pool
}

const leaseTableDDL = `
CREATE TABLE IF NOT EXISTS cluster_leases (
    name        TEXT PRIMARY KEY,
    holder      TEXT NOT NULL,
    acquired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    renewed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
)`

func (s *pgLeaseStore) ensure(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, leaseTableDDL)
	return err
}

// acquire：行不存在则插入；存在时只有「本来就是我」或「已经过期」才改写。
// 用数据库时钟判断过期，各节点的本地时钟偏差不影响正确性。
func (s *pgLeaseStore) acquire(ctx context.Context, name, holder string, ttl time.Duration) (bool, error) {
	var got string
	err := s.pool.QueryRow(ctx, `
INSERT INTO cluster_leases (name, holder, acquired_at, renewed_at, expires_at)
VALUES ($1, $2, now(), now(), now() + make_interval(secs => $3))
ON CONFLICT (name) DO UPDATE
   SET holder      = EXCLUDED.holder,
       acquired_at = CASE WHEN cluster_leases.holder = EXCLUDED.holder THEN cluster_leases.acquired_at ELSE now() END,
       renewed_at  = now(),
       expires_at  = EXCLUDED.expires_at
 WHERE cluster_leases.holder = EXCLUDED.holder
    OR cluster_leases.expires_at < now()
RETURNING holder`, name, holder, ttl.Seconds()).Scan(&got)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, err
	}
	return got == holder, nil
}

func (s *pgLeaseStore) release(ctx context.Context, names []string, holder string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM cluster_leases WHERE holder = $1 AND name = ANY($2)`, holder, names)
	return err
}
