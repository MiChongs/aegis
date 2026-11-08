package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	reportdomain "aegis/internal/domain/report"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service/reportengine"
	apperrors "aegis/pkg/errors"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// ReportService 应用级报表分析中心。
//
// 计算分两层：仓储层（report_repository.go）按日去重后把明细流出来，
// 这里用 reportengine 的 roaring 位图与 gonum 统计在内存里做多维计算。
// 多个报表共用的「活跃事实」「注册事实」按（应用, 窗口, 时区）缓存一分钟，
// 同一时刻的并发请求经 singleflight 合并成一次加载 —— 控制台切标签页不会把同一批明细反复扫一遍。
type ReportService struct {
	risk    *RiskService
	log     *zap.Logger
	pg      *pgrepo.Repository
	devices *DeviceMarketingService
	now     func() time.Time

	flight singleflight.Group
	mu     sync.Mutex
	cache  map[string]reportCacheEntry
}

type reportCacheEntry struct {
	value   any
	expires time.Time
}

const (
	reportCacheTTL  = time.Minute
	reportCacheSize = 256
	// reportParallel 单个报表同时在跑的查询数。连接池不大，报表又是低频的后台请求，不能把池子占满
	reportParallel = 4
	// reportObserveDays 窗口之后继续观察回访的天数（留存第 30 天、第 8 周与漏斗的回访）
	reportObserveDays = 60
)

// NewReportService 创建报表服务
func NewReportService(log *zap.Logger, pg *pgrepo.Repository) *ReportService {
	return &ReportService{log: log, pg: pg, now: time.Now, cache: map[string]reportCacheEntry{}}
}

// SetDeviceDirectory 接上设备字典：设备报表的机型按字典显示营销名与设备图
func (s *ReportService) SetDeviceDirectory(devices *DeviceMarketingService) { s.devices = devices }

// ReportParams 一次报表请求
type ReportParams struct {
	From        time.Time
	To          time.Time
	TimeZone    string
	Granularity string
	// 留存：unit = day / week，mode = exact / rolling
	Unit string
	Mode string
}

func (s *ReportService) window(p ReportParams) (*reportengine.Window, error) {
	w, err := reportengine.NewWindow(p.From, p.To, p.TimeZone, p.Granularity)
	if err != nil {
		return nil, apperrors.New(40000, 400, err.Error())
	}
	return w, nil
}

// cached 按键缓存一分钟；并发的同键请求只算一次
func reportCached[T any](s *ReportService, key string, load func() (T, error)) (T, error) {
	now := s.now()
	s.mu.Lock()
	if entry, ok := s.cache[key]; ok && now.Before(entry.expires) {
		s.mu.Unlock()
		return entry.value.(T), nil
	}
	s.mu.Unlock()
	v, err, _ := s.flight.Do(key, func() (any, error) {
		value, err := load()
		if err != nil {
			return value, err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.cache) >= reportCacheSize {
			for k, entry := range s.cache {
				if now.After(entry.expires) || len(s.cache) >= reportCacheSize {
					delete(s.cache, k)
				}
			}
		}
		s.cache[key] = reportCacheEntry{value: value, expires: s.now().Add(reportCacheTTL)}
		return value, nil
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

func windowKey(appID int64, w *reportengine.Window) string {
	return fmt.Sprintf("%d|%s|%d|%s", appID, w.DayLabel(0), w.Days, w.TZ)
}

// ── 活跃事实 ──

// activityFacts 每天的活跃位图（按信号分开，另有合并），覆盖 [-lookback, days+observe)
type activityFacts struct {
	all     *reportengine.DaySets
	signals map[int]*reportengine.DaySets
	// today 今天在窗口坐标里的天索引；回访观察不越过今天
	today int
}

func (s *ReportService) activity(ctx context.Context, appID int64, w *reportengine.Window) (*activityFacts, error) {
	return reportCached(s, "activity|"+windowKey(appID, w), func() (*activityFacts, error) {
		lookback := w.Days
		if lookback < 29 {
			lookback = 29
		}
		today := w.DayIndexOf(s.now())
		extra := reportObserveDays
		// 窗口之后只观察到今天为止
		if limit := today - w.Days + 1; limit < extra {
			extra = limit
		}
		if extra < 0 {
			extra = 0
		}
		facts := &activityFacts{
			all:     reportengine.NewDaySets(lookback, w.Days, extra),
			signals: map[int]*reportengine.DaySets{},
			today:   today,
		}
		for _, signal := range []int{pgrepo.ReportSignalLogin, pgrepo.ReportSignalSession, pgrepo.ReportSignalSignIn} {
			facts.signals[signal] = reportengine.NewDaySets(lookback, w.Days, extra)
		}
		from, to := w.DayStart(-lookback), w.DayStart(w.Days+extra)
		err := s.pg.ReportActivityDays(ctx, appID, from, to, w.TZ, func(signal int, userID int64, day time.Time) error {
			idx := w.DayIndex(day)
			uid := uint64(userID)
			facts.all.Add(idx, uid)
			if set := facts.signals[signal]; set != nil {
				set.Add(idx, uid)
			}
			return nil
		})
		return facts, err
	})
}

// lastObserved 可用于回访观察的最后一天（不越过今天，也不越过已加载的范围）
func (f *activityFacts) lastObserved() int {
	last := f.all.Last()
	if f.today < last {
		return f.today
	}
	return last
}

// ── 注册事实 ──

type registrationFacts struct {
	// dayOf 本期与上期注册用户各自的注册日（窗口坐标）
	dayOf     map[uint64]int
	createdAt map[uint64]time.Time
	current   *roaring64.Bitmap
	previous  *roaring64.Bitmap
	byDay     []*roaring64.Bitmap // 本期每天注册的用户
	prevByDay []*roaring64.Bitmap // 上期每天注册的用户（索引 = 天 + 窗口天数）
	heatmap   reportdomain.Heatmap
	weekdays  [7]int64
	before    int64 // 本期之前的累计用户
}

func (s *ReportService) registrations(ctx context.Context, appID int64, w *reportengine.Window) (*registrationFacts, error) {
	return reportCached(s, "registrations|"+windowKey(appID, w), func() (*registrationFacts, error) {
		facts := &registrationFacts{
			dayOf:     map[uint64]int{},
			createdAt: map[uint64]time.Time{},
			current:   roaring64.New(),
			previous:  roaring64.New(),
			byDay:     make([]*roaring64.Bitmap, w.Days),
			prevByDay: make([]*roaring64.Bitmap, w.Days),
		}
		for i := range facts.byDay {
			facts.byDay[i] = roaring64.New()
			facts.prevByDay[i] = roaring64.New()
		}
		g, gctx := errgroup.WithContext(ctx)
		g.Go(func() error {
			n, err := s.pg.ReportUsersBefore(gctx, appID, w.Start)
			facts.before = n
			return err
		})
		g.Go(func() error {
			return s.pg.ReportRegistrations(gctx, appID, w.PrevStart(), w.End(), func(userID int64, createdAt time.Time) error {
				uid := uint64(userID)
				day := w.DayIndexOf(createdAt)
				facts.dayOf[uid] = day
				if day < 0 {
					facts.previous.Add(uid)
					if day >= -w.Days {
						facts.prevByDay[day+w.Days].Add(uid)
					}
					return nil
				}
				if day >= w.Days {
					return nil
				}
				facts.createdAt[uid] = createdAt
				facts.current.Add(uid)
				facts.byDay[day].Add(uid)
				wd, hour := w.Weekday(createdAt)
				reportengine.HeatmapAdd(&facts.heatmap, wd, hour, 1)
				facts.weekdays[wd]++
				return nil
			})
		})
		return facts, g.Wait()
	})
}

// ── 已支付订单 ──

type paymentFacts struct {
	current  []pgrepo.ReportPaidOrder
	previous []pgrepo.ReportPaidOrder
}

func (s *ReportService) payments(ctx context.Context, appID int64, w *reportengine.Window) (*paymentFacts, error) {
	return reportCached(s, "payments|"+windowKey(appID, w), func() (*paymentFacts, error) {
		facts := &paymentFacts{}
		err := s.pg.ReportPaidOrders(ctx, appID, w.PrevStart(), w.End(), func(o pgrepo.ReportPaidOrder) error {
			if w.DayIndexOf(o.PaidAt) < 0 {
				facts.previous = append(facts.previous, o)
			} else {
				facts.current = append(facts.current, o)
			}
			return nil
		})
		return facts, err
	})
}

// ── 小工具 ──

// group 并发跑若干查询，最多 reportParallel 个同时进行
func reportGroup(ctx context.Context) (*errgroup.Group, context.Context) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(reportParallel)
	return g, gctx
}

// dailySeries 本期每天一个值、上期每天一个值
type dailySeries struct {
	cur  []float64
	prev []float64
}

func newDailySeries(w *reportengine.Window) *dailySeries {
	return &dailySeries{cur: make([]float64, w.Days), prev: make([]float64, w.Days)}
}

func (d *dailySeries) add(w *reportengine.Window, day int, v float64) {
	switch {
	case day >= 0 && day < w.Days:
		d.cur[day] += v
	case day < 0 && day >= -w.Days:
		d.prev[day+w.Days] += v
	}
}

func (d *dailySeries) total() float64     { return reportengine.Sum(d.cur) }
func (d *dailySeries) prevTotal() float64 { return reportengine.Sum(d.prev) }

// sumBuckets 每天的值按桶求和
func sumBuckets(w *reportengine.Window, daily []float64) []float64 {
	out := w.Zeros()
	for i, v := range daily {
		if b := w.Bucket(i); b >= 0 {
			out[b] += v
		}
	}
	return out
}

// avgBuckets 每天的值按桶取日均（只算到今天为止的天）
func avgBuckets(w *reportengine.Window, daily []float64, elapsed int) []float64 {
	out := w.Zeros()
	days := make([]float64, len(out))
	for i, v := range daily {
		if i >= elapsed {
			break
		}
		if b := w.Bucket(i); b >= 0 {
			out[b] += v
			days[b]++
		}
	}
	for i := range out {
		out[i] = reportengine.Round(reportengine.Ratio(out[i], days[i]), 2)
	}
	return out
}

// lastBuckets 每个桶取桶内最后一个已经过去的日子的值（如 WAU、MAU、累计用户）
func lastBuckets(w *reportengine.Window, daily []float64, elapsed int) []float64 {
	out := w.Zeros()
	for i, v := range daily {
		if i >= elapsed {
			break
		}
		if b := w.Bucket(i); b >= 0 {
			out[b] = v
		}
	}
	return out
}

// uniqueBuckets 每个桶内的去重人数
func uniqueBuckets(w *reportengine.Window, sets *reportengine.DaySets) []float64 {
	out := w.Zeros()
	start := 0
	for i := 1; i <= w.Days; i++ {
		if i == w.Days || w.Bucket(i) != w.Bucket(start) {
			out[w.Bucket(start)] = reportengine.Cardinality(sets.Union(start, i-1))
			start = i
		}
	}
	return out
}

// mergeDaily 仓储按日行 → 本期 / 上期序列（列 a / b / c 各一条）
func mergeDaily(w *reportengine.Window, rows []pgrepo.ReportDailyRow) (a, b, c *dailySeries) {
	a, b, c = newDailySeries(w), newDailySeries(w), newDailySeries(w)
	for _, r := range rows {
		day := w.DayIndex(r.Day)
		a.add(w, day, r.A)
		b.add(w, day, r.B)
		c.add(w, day, r.C)
	}
	return
}

// keyedDaily 按键拆开的按日序列（只取本期）
func keyedDaily(w *reportengine.Window, rows []pgrepo.ReportDailyRow, value func(pgrepo.ReportDailyRow) float64) map[string][]float64 {
	out := map[string][]float64{}
	for _, r := range rows {
		day := w.DayIndex(r.Day)
		if day < 0 || day >= w.Days {
			continue
		}
		series := out[r.Key]
		if series == nil {
			series = make([]float64, w.Days)
			out[r.Key] = series
		}
		series[day] += value(r)
	}
	return out
}

func toGroups(rows []pgrepo.ReportGroupRow) []reportengine.Group {
	out := make([]reportengine.Group, len(rows))
	for i, r := range rows {
		out[i] = reportengine.Group{Key: r.Key, Label: r.Label, Count: r.Count, Users: r.Users, Amount: r.Amount, Value: r.Value}
	}
	return out
}

// SetRiskService 接风控平台（「风控」报表的数据在它的 ClickHouse 里）
func (s *ReportService) SetRiskService(risk *RiskService) { s.risk = risk }

func (s *ReportService) groups(ctx context.Context, name string, appID int64, w *reportengine.Window, opts reportengine.SliceOptions) ([]reportdomain.Slice, error) {
	rows, err := s.pg.ReportGroups(ctx, name, appID, w.Start, w.End(), w.TZ)
	if err != nil {
		return nil, err
	}
	return reportengine.ToSlices(toGroups(rows), opts), nil
}

func (s *ReportService) heatmap(ctx context.Context, name string, appID int64, w *reportengine.Window) (reportdomain.Heatmap, error) {
	var h reportdomain.Heatmap
	cells, err := s.pg.ReportHeatmap(ctx, name, appID, w.Start, w.End(), w.TZ)
	if err != nil {
		return h, err
	}
	for _, c := range cells {
		reportengine.HeatmapAdd(&h, c.Weekday-1, c.Hour, c.Count)
	}
	return h, nil
}

// kpi 本期合计、上期合计与按桶走势
func kpi(cur, prev float64, spark []float64) reportdomain.KPI {
	return reportengine.NewKPI(cur, reportengine.Ptr(prev), spark)
}

func ratioKPI(num, den, prevNum, prevDen float64) reportdomain.KPI {
	cur := reportengine.Ratio(num, den)
	if prevDen == 0 {
		return reportengine.NewKPI(cur, nil, nil)
	}
	return reportengine.NewKPI(cur, reportengine.Ptr(reportengine.Ratio(prevNum, prevDen)), nil)
}

func cents(v int64) float64 { return float64(v) / 100 }

func trimLower(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
