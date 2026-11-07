// Package trafficplugin 把 pkg/traffic 引擎包装成内置插件 traffic：
// 生命周期（由 nativeplugin.Host 管理）、请求链路上的 Gin 中间件、配置的持久化与
// 跨实例同步、分钟聚合与事件落库，以及管理端用到的全部操作。
package trafficplugin

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	systemdomain "aegis/internal/domain/system"
	trafficdomain "aegis/internal/domain/traffic"
	"aegis/internal/repository/postgres"
	"aegis/pkg/response"
	"aegis/pkg/traffic"
	"aegis/pkg/traffic/redisco"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ID 插件标识。
const ID = "traffic"

// Version 插件版本（随配置格式变化递增）。
const Version = "2.0.0"

const (
	metricsRetention = 30 * 24 * time.Hour
	eventsRetention  = 30 * 24 * time.Hour
)

// Store 插件用到的持久化操作（由 postgres.Repository 实现）。
type Store interface {
	TrafficLoadConfig(ctx context.Context) (trafficdomain.ConfigView, bool, error)
	TrafficRevision(ctx context.Context) (int64, error)
	TrafficPublish(ctx context.Context, expected int64, actor *int64, mutate postgres.TrafficMutation) (trafficdomain.ConfigView, error)
	TrafficRevisions(ctx context.Context, limit int) ([]trafficdomain.RevisionSummary, error)
	TrafficRevisionDetail(ctx context.Context, revision int64) (*trafficdomain.RevisionDetail, error)
	TrafficInsertMinutes(ctx context.Context, batches []trafficdomain.MinuteBatch) error
	TrafficHistory(ctx context.Context, from, to time.Time, step time.Duration, ruleID string) ([]trafficdomain.HistoryPoint, traffic.Counts, error)
	TrafficInsertEvents(ctx context.Context, instance string, events []traffic.Event) error
	TrafficEvents(ctx context.Context, limit int, kind string) ([]trafficdomain.EventRecord, error)
	TrafficPurge(ctx context.Context, metricsBefore, eventsBefore time.Time) error
	GetPlatformSetting(ctx context.Context, key string) (*systemdomain.SettingRecord, error)
}

// Plugin 流量管理插件。
type Plugin struct {
	log      *zap.Logger
	store    Store
	co       *redisco.Coordinator
	engine   *traffic.Engine
	instance string

	running atomic.Bool
	applied atomic.Int64 // 本实例已应用的修订号

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup

	minutes chan []traffic.MinuteSample
	events  chan traffic.Event
	reload  chan int64
}

// New 构造插件。rdb 为 nil 时不启用集群协同（单实例部署）。
func New(store Store, rdb *redis.Client, keyPrefix string, log *zap.Logger) *Plugin {
	if log == nil {
		log = zap.NewNop()
	}
	log = log.Named("traffic")
	instance := instanceID()
	p := &Plugin{
		log:      log,
		store:    store,
		instance: instance,
		minutes:  make(chan []traffic.MinuteSample, 64),
		events:   make(chan traffic.Event, 1024),
		reload:   make(chan int64, 8),
	}
	var co traffic.Coordinator
	if rdb != nil {
		p.co = redisco.New(rdb, keyPrefix, instance, log)
		co = p.co
	}
	p.engine = traffic.NewEngine(traffic.Options{Instance: instance, Coordinator: co, Sink: p, Logger: log})
	return p
}

func instanceID() string {
	host, _ := os.Hostname()
	if host == "" {
		host = "aegis"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return host + "-" + strconv.Itoa(os.Getpid()) + "-" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}

// Engine 底层引擎（测试与压测用）。
func (p *Plugin) Engine() *traffic.Engine { return p.engine }

// Manifest 实现 nativeplugin.Plugin。
func (p *Plugin) Manifest() plugindomain.NativeManifest {
	return plugindomain.NativeManifest{
		ID:          ID,
		Name:        "流量管理",
		Version:     Version,
		Description: "请求分类、速率限制、并发控制、排队削峰、预热爬坡与过载保护",
		Author:      "Aegis",
		Category:    "网关",
		Capabilities: []string{
			"规则匹配", "速率限制", "并发控制", "自适应并发", "排队削峰", "预热爬坡", "过载保护", "集群配额", "请求模拟", "版本回滚",
		},
		DefaultEnabled: true,
	}
}

// Start 实现 nativeplugin.Plugin：加载（必要时初始化）配置，启动后台循环。
func (p *Plugin) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	view, exists, err := p.store.TrafficLoadConfig(ctx)
	if err != nil {
		return err
	}
	if !exists || view.Revision == 0 {
		view, err = p.seed(ctx)
		if err != nil {
			return err
		}
	}
	p.engine.Apply(view.Config)
	p.applied.Store(view.Revision)

	bg, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.goLoop(func() { p.flushLoop(bg) })
	p.goLoop(func() { p.syncLoop(bg) })
	p.goLoop(func() { p.purgeLoop(bg) })
	if p.co != nil {
		p.goLoop(func() {
			p.co.Run(bg, p.engine.Summary, func(rev int64) {
				select {
				case p.reload <- rev:
				default:
				}
			})
		})
	}
	p.running.Store(true)
	return nil
}

func (p *Plugin) goLoop(fn func()) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		fn()
	}()
}

// Stop 实现 nativeplugin.Plugin。中间件立刻转为直通，后台循环退出前把缓冲的样本写完。
func (p *Plugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.running.Store(false)
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Health 实现 nativeplugin.Plugin。
func (p *Plugin) Health() map[string]any {
	s := p.engine.Summary()
	cfg := p.engine.Config()
	instances := 1
	if p.co != nil {
		instances = p.co.Instances()
	}
	return map[string]any{
		"revision":   s.Revision,
		"enabled":    s.Enabled,
		"rules":      len(cfg.Rules),
		"instances":  instances,
		"rps":        s.LastSec.Arrivals,
		"inflight":   s.Inflight,
		"overloaded": s.Overloaded,
	}
}

// Running 插件是否在运行。
func (p *Plugin) Running() bool { return p.running.Load() }

// ── 请求链路 ──

// Handler 常驻在路由中间件链上（Firewall 之后、ReplayGuard 之前）。
// 插件未运行时直接放行，因此启停不需要重建路由。
func (p *Plugin) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == nil || !p.running.Load() {
			c.Next()
			return
		}
		ticket, verdict := p.engine.Admit(c.Request.Context(), c.Request, remoteIP(c.Request.RemoteAddr))
		if !verdict.Allowed {
			resp := verdict.Response
			c.Header("Retry-After", strconv.Itoa(resp.RetryAfterSeconds))
			response.Error(c, resp.Status, resp.Code, resp.Message)
			c.Abort()
			return
		}
		defer ticket.Done()
		c.Next()
	}
}

// remoteIP 客户端地址。middleware.ClientIP 已把判定后的真实地址写回 RemoteAddr。
func remoteIP(addr string) netip.Addr {
	if ap, err := netip.ParseAddrPort(addr); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(strings.Trim(addr, "[]")); err == nil {
		return a.Unmap()
	}
	return netip.IPv4Unspecified()
}

// ── traffic.Sink：引擎采样循环调用，必须非阻塞 ──

// Minutes 实现 traffic.Sink。
func (p *Plugin) Minutes(samples []traffic.MinuteSample) {
	if !p.running.Load() {
		return
	}
	select {
	case p.minutes <- samples:
	default:
		p.log.Warn("traffic minute buffer full, samples dropped", zap.Int("count", len(samples)))
	}
}

// Event 实现 traffic.Sink。
func (p *Plugin) Event(ev traffic.Event) {
	if !p.running.Load() {
		return
	}
	select {
	case p.events <- ev:
	default:
	}
}

func (p *Plugin) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var evs []traffic.Event
	var mins []trafficdomain.MinuteBatch
	flush := func(c context.Context) {
		if len(mins) > 0 {
			wctx, cancel := context.WithTimeout(c, 10*time.Second)
			if err := p.store.TrafficInsertMinutes(wctx, mins); err != nil {
				p.log.Warn("write traffic minutes failed", zap.Error(err))
			}
			cancel()
			mins = mins[:0]
		}
		if len(evs) > 0 {
			wctx, cancel := context.WithTimeout(c, 10*time.Second)
			if err := p.store.TrafficInsertEvents(wctx, p.instance, evs); err != nil {
				p.log.Warn("write traffic events failed", zap.Error(err))
			}
			cancel()
			evs = evs[:0]
		}
	}
	for {
		select {
		case <-ctx.Done():
			// 退出前排空缓冲。
			for {
				select {
				case s := <-p.minutes:
					mins = append(mins, trafficdomain.MinuteBatch{Instance: p.instance, Samples: s})
				case e := <-p.events:
					evs = append(evs, e)
				default:
					flush(context.Background())
					return
				}
			}
		case s := <-p.minutes:
			mins = append(mins, trafficdomain.MinuteBatch{Instance: p.instance, Samples: s})
		case e := <-p.events:
			evs = append(evs, e)
			if len(evs) >= 200 {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		}
	}
}

// syncLoop 让本实例跟上数据库里的最新修订：收到广播立即重载，另外每 10 秒对账一次
// （广播丢失、Redis 不可用时兜底）。
func (p *Plugin) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case rev := <-p.reload:
			if rev > p.applied.Load() {
				p.reloadFromStore(ctx)
			}
		case <-ticker.C:
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			rev, err := p.store.TrafficRevision(rctx)
			cancel()
			if err == nil && rev != p.applied.Load() {
				p.reloadFromStore(ctx)
			}
		}
	}
}

func (p *Plugin) reloadFromStore(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	view, exists, err := p.store.TrafficLoadConfig(rctx)
	if err != nil || !exists {
		return
	}
	p.applyLocal(view.Config)
}

func (p *Plugin) applyLocal(cfg traffic.Config) {
	p.engine.Apply(cfg)
	p.applied.Store(cfg.Revision)
}

func (p *Plugin) purgeLoop(ctx context.Context) {
	// 启动后错开一段时间再清理，多实例同时启动时不扎堆。
	first := time.NewTimer(2 * time.Minute)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
	}
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		now := time.Now()
		pctx, cancel := context.WithTimeout(ctx, time.Minute)
		if err := p.store.TrafficPurge(pctx, now.Add(-metricsRetention), now.Add(-eventsRetention)); err != nil && ctx.Err() == nil {
			p.log.Warn("purge traffic history failed", zap.Error(err))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ── 工具 ──

func newRuleID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "r_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
}
