package service

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	devicedomain "aegis/internal/domain/device"
	reportdomain "aegis/internal/domain/report"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service/reportengine"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
	"go.uber.org/zap"
)

// 业务侧报表：登录、设备、地区、渠道、支付、通知、风控、抽奖、互动。

// Login 登录
func (s *ReportService) Login(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Login, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act                                    *activityFacts
		daily                                  []pgrepo.ReportDailyRow
		perUser                                []pgrepo.ReportGroupRow
		types, providers, failures, failureIPs []reportdomain.Slice
		heat                                   reportdomain.Heatmap
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) {
		daily, err = s.pg.ReportDaily(gctx, "login.daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		perUser, err = s.pg.ReportGroups(gctx, "login.per_user", appID, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		types, err = s.groups(gctx, "login.types", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		providers, err = s.groups(gctx, "login.providers", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		failures, err = s.groups(gctx, "login.failures", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		failureIPs, err = s.groups(gctx, "login.failure_ips", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) { heat, err = s.heatmap(gctx, "login.heatmap", appID, w); return })
	if err := g.Wait(); err != nil {
		return nil, err
	}

	success, failure, users := mergeDaily(w, daily)
	attempts := success.total() + failure.total()
	prevAttempts := success.prevTotal() + failure.prevTotal()
	loginUsers := reportengine.Cardinality(act.signals[pgrepo.ReportSignalLogin].Union(0, w.Days-1))
	prevLoginUsers := reportengine.Cardinality(act.signals[pgrepo.ReportSignalLogin].Union(-w.Days, -1))
	hist := map[int]int64{}
	for _, row := range perUser {
		n, _ := strconv.Atoi(row.Key)
		hist[n] += row.Count
	}
	failureRate := w.Zeros()
	okB, failB := sumBuckets(w, success.cur), sumBuckets(w, failure.cur)
	for i := range failureRate {
		failureRate[i] = reportengine.Round(reportengine.Ratio(failB[i], okB[i]+failB[i]), 4)
	}
	return &reportdomain.Login{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"success":     kpi(success.total(), success.prevTotal(), okB),
			"failure":     kpi(failure.total(), failure.prevTotal(), failB),
			"failureRate": ratioKPI(failure.total(), attempts, failure.prevTotal(), prevAttempts),
			"users":       kpi(loginUsers, prevLoginUsers, nil),
			"perUser":     ratioKPI(success.total(), loginUsers, success.prevTotal(), prevLoginUsers),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"success":     okB,
				"failure":     failB,
				"failureRate": failureRate,
				"users":       sumBuckets(w, users.cur),
			},
		},
		Types:      types,
		Providers:  providers,
		Failures:   failures,
		FailureIPs: failureIPs,
		Frequency:  reportengine.BandHistogram(hist, []int{1, 2, 4, 8, 16, 32}),
		Heatmap:    heat,
	}, nil
}

// Device 设备：每台设备取窗口内最近一次登录时上报的信息，机型按设备字典显示营销名
func (s *ReportService) Device(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Device, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	platforms, manufacturers, models := reportengine.NewCounter(), reportengine.NewCounter(), reportengine.NewCounter()
	osVersions, appVersions, browsers := reportengine.NewCounter(), reportengine.NewCounter(), reportengine.NewCounter()
	users := roaring64.New()
	var devices, matched float64
	described := map[string]devicedomain.Info{}

	err = s.pg.ReportDevices(ctx, appID, w.Start, w.End(), func(d pgrepo.ReportDeviceRow) error {
		devices++
		uid := uint64(max(d.UserID, 0))
		if uid != 0 {
			users.Add(uid)
		}
		platform := devicedomain.NormalizePlatform(d.Platform)
		if platform == "" {
			platform = devicedomain.PlatformFromUserAgent(d.UserAgent)
		}
		key := platform + "\x00" + d.Model + "\x00" + d.Codename + "\x00" + d.Name + "\x00" + d.Manufacturer
		info, ok := described[key]
		if !ok {
			in := devicedomain.DescribeInput{
				Model: d.Model, Platform: platform, Name: d.Name, UserAgent: d.UserAgent,
				Extra: devicedomain.Extra{Manufacturer: d.Manufacturer, Brand: d.Brand, Codename: d.Codename, OS: d.OS, OSVersion: d.OSVersion, AppVersion: d.AppVersion},
			}
			// 字典查询有缓存，但不同机型很多时仍要限量：只给前 2000 种组合查字典
			if len(described) < 2000 {
				info = s.devices.Describe(ctx, in)
			} else {
				info = (*DeviceMarketingService)(nil).Describe(ctx, in)
			}
			described[key] = info
		}
		if info.Matched {
			matched++
		}
		platforms.Add(platform, uid, 0)
		manufacturer := strings.TrimSpace(info.Manufacturer)
		if manufacturer == "" {
			manufacturer = strings.TrimSpace(d.Brand)
		}
		manufacturers.Add(manufacturer, uid, 0)
		manufacturers.SetMeta(manufacturer, "icon", info.ManufacturerIconURL)
		if name := strings.TrimSpace(info.Name); name != "" {
			models.Add(name, uid, 0)
			models.SetMeta(name, "image", info.DeviceImageURL)
			models.SetMeta(name, "platform", info.Platform)
			models.SetMeta(name, "identifier", info.Identifier)
		} else {
			models.Add("", uid, 0)
		}
		osName := strings.TrimSpace(d.OS)
		if osName == "" {
			osName = platformOSName(platform)
		}
		if osName != "" && strings.TrimSpace(d.OSVersion) != "" {
			osName += " " + strings.TrimSpace(d.OSVersion)
		}
		osVersions.Add(osName, uid, 0)
		appVersions.Add(d.AppVersion, uid, 0)
		if browser := classifyBrowser(d.UserAgent); browser != "" {
			browsers.Add(browser, uid, 0)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	userN := reportengine.Cardinality(users)
	return &reportdomain.Device{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"devices":        reportengine.NewKPI(devices, nil, nil),
			"users":          reportengine.NewKPI(userN, nil, nil),
			"devicesPerUser": reportengine.NewKPI(reportengine.Round(reportengine.Ratio(devices, userN), 2), nil, nil),
			"matchedRate":    reportengine.NewKPI(reportengine.Round(reportengine.Ratio(matched, devices), 4), nil, nil),
		},
		Platforms:     platforms.Slices(reportengine.SliceOptions{}),
		Manufacturers: manufacturers.Slices(reportengine.SliceOptions{Top: 15}),
		Models:        models.Slices(reportengine.SliceOptions{Top: 30}),
		OSVersions:    osVersions.Slices(reportengine.SliceOptions{Top: 20}),
		AppVersions:   appVersions.Slices(reportengine.SliceOptions{Top: 20}),
		Browsers:      browsers.Slices(reportengine.SliceOptions{Top: 10}),
	}, nil
}

func platformOSName(platform string) string {
	switch platform {
	case devicedomain.PlatformAndroid:
		return "Android"
	case devicedomain.PlatformIOS:
		return "iOS"
	}
	return ""
}

// classifyBrowser 浏览器（只认浏览器 UA；App 客户端、SDK 的请求返回空）
func classifyBrowser(ua string) string {
	lower := strings.ToLower(ua)
	if !strings.Contains(lower, "mozilla/") {
		return ""
	}
	switch {
	case strings.Contains(lower, "micromessenger"):
		return "WeChat"
	case strings.Contains(lower, " qq/") || strings.Contains(lower, "mqqbrowser"):
		return "QQ"
	case strings.Contains(lower, "edg/") || strings.Contains(lower, "edga/") || strings.Contains(lower, "edgios/"):
		return "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		return "Opera"
	case strings.Contains(lower, "samsungbrowser"):
		return "Samsung Internet"
	case strings.Contains(lower, "ucbrowser"):
		return "UC"
	case strings.Contains(lower, "quark"):
		return "Quark"
	case strings.Contains(lower, "firefox/") || strings.Contains(lower, "fxios/"):
		return "Firefox"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		return "Chrome"
	case strings.Contains(lower, "safari/"):
		return "Safari"
	}
	return "other"
}

// Region 地区（登录归属地，来自 GeoIP 三层解析的登录地理事件）
func (s *ReportService) Region(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Region, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var countries, provinces, cities, isps, register []reportdomain.Slice
	byUsers := reportengine.SliceOptions{By: "users"}
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { countries, err = s.groups(gctx, "region.countries", appID, w, byUsers); return })
	g.Go(func() (err error) { provinces, err = s.groups(gctx, "region.provinces", appID, w, byUsers); return })
	g.Go(func() (err error) { cities, err = s.groups(gctx, "region.cities", appID, w, byUsers); return })
	g.Go(func() (err error) { isps, err = s.groups(gctx, "region.isps", appID, w, byUsers); return })
	g.Go(func() (err error) {
		register, err = s.groups(gctx, "users.provinces", appID, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var domestic, total float64
	known := func(items []reportdomain.Slice) float64 {
		var n float64
		for _, it := range items {
			if it.Key != "unknown" && it.Key != reportengine.OtherKey {
				n++
			}
		}
		return n
	}
	for _, c := range countries {
		total += float64(c.Users)
		if c.Key == "CN" {
			domestic += float64(c.Users)
		}
	}
	return &reportdomain.Region{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"countries":     reportengine.NewKPI(known(countries), nil, nil),
			"provinces":     reportengine.NewKPI(known(provinces), nil, nil),
			"cities":        reportengine.NewKPI(known(cities), nil, nil),
			"domesticShare": reportengine.NewKPI(reportengine.Round(reportengine.Ratio(domestic, total), 4), nil, nil),
		},
		Countries:         countries,
		Provinces:         provinces,
		Cities:            cities,
		ISPs:              isps,
		RegisterProvinces: register,
	}, nil
}

// Channel 渠道
func (s *ReportService) Channel(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Channel, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		reg                         *registrationFacts
		sources                     []pgrepo.ReportGroupRow
		daily                       []pgrepo.ReportDailyRow
		providers, invites, cardkey []reportdomain.Slice
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { reg, err = s.registrations(gctx, appID, w); return })
	g.Go(func() (err error) {
		sources, err = s.pg.ReportGroups(gctx, "users.sources", appID, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		daily, err = s.pg.ReportDaily(gctx, "channel.register_daily", appID, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		providers, err = s.groups(gctx, "login.providers", appID, w, reportengine.SliceOptions{By: "users"})
		return
	})
	g.Go(func() (err error) {
		invites, err = s.groups(gctx, "channel.invites", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		cardkey, err = s.groups(gctx, "channel.cardkeys", appID, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	newUsers := reportengine.Cardinality(reg.current)
	groups := toGroups(sources)
	var audited, thirdParty int64
	for _, gr := range groups {
		audited += gr.Count
		switch gr.Key {
		case "password", "sms", "email", "cardkey", "admin":
		default:
			thirdParty += gr.Count
		}
	}
	if rest := int64(newUsers) - audited; rest > 0 {
		groups = append(groups, reportengine.Group{Key: "admin", Count: rest, Users: rest})
	}
	var invited, cardkeyCount float64
	for _, it := range invites {
		invited += float64(it.Count)
	}
	for _, it := range cardkey {
		cardkeyCount += float64(it.Count)
	}
	// 按注册来源堆叠：前 5 个来源各一条，其余并成「其他」
	perSource := keyedDaily(w, daily, func(r pgrepo.ReportDailyRow) float64 { return r.A })
	registerSlices := reportengine.ToSlices(groups, reportengine.SliceOptions{})
	series := map[string][]float64{}
	keep := map[string]bool{}
	for i, sl := range registerSlices {
		if i < 5 && sl.Key != "admin" {
			keep[sl.Key] = true
		}
	}
	for key, daily := range perSource {
		target := key
		if !keep[key] {
			target = reportengine.OtherKey
		}
		buckets := sumBuckets(w, daily)
		if series[target] == nil {
			series[target] = w.Zeros()
		}
		for i, v := range buckets {
			series[target][i] += v
		}
	}
	return &reportdomain.Channel{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"newUsers":        reportengine.NewKPI(newUsers, nil, nil),
			"invited":         reportengine.NewKPI(invited, nil, nil),
			"inviteRate":      reportengine.NewKPI(reportengine.Round(reportengine.Ratio(invited, newUsers), 4), nil, nil),
			"thirdPartyShare": reportengine.NewKPI(reportengine.Round(reportengine.Ratio(float64(thirdParty), newUsers), 4), nil, nil),
			"cardKeys":        reportengine.NewKPI(cardkeyCount, nil, nil),
		},
		Trend:          reportdomain.Trend{Buckets: w.Buckets(), Series: series},
		RegisterSource: registerSlices,
		LoginProviders: providers,
		InviteSources:  invites,
		CardKeySources: cardkey,
	}, nil
}

// Payment 支付
func (s *ReportService) Payment(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Payment, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act              *activityFacts
		pay              *paymentFacts
		refunds, created []pgrepo.ReportDailyRow
		statuses, vip    []reportdomain.Slice
		newPayers        = roaring64.New()
		prevNewPayers    = roaring64.New()
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) { pay, err = s.payments(gctx, appID, w); return })
	g.Go(func() (err error) {
		refunds, err = s.pg.ReportDaily(gctx, "payment.refund_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		created, err = s.pg.ReportDaily(gctx, "payment.created_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() error {
		return s.pg.ReportFirstPayments(gctx, appID, w.PrevStart(), w.End(), func(userID int64, at time.Time) error {
			if w.DayIndexOf(at) < 0 {
				prevNewPayers.Add(uint64(userID))
			} else {
				newPayers.Add(uint64(userID))
			}
			return nil
		})
	})
	g.Go(func() (err error) {
		statuses, err = s.groups(gctx, "payment.statuses", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		vip, err = s.groups(gctx, "payment.vip", appID, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	now := s.now()
	elapsed := w.ElapsedDays(now)
	revenue, payers, prevPayers := revenueDaily(w, pay)
	refund, refundCount, _ := mergeDaily(w, refunds)
	createdOrders, paidOfCreated, _ := mergeDaily(w, created)
	orders := newDailySeries(w)
	payerDays := reportengine.NewDaySets(0, w.Days, 0)
	methods, providers, products := reportengine.NewCounter(), reportengine.NewCounter(), reportengine.NewCounter()
	amounts := make([]float64, 0, len(pay.current))
	perUser := map[uint64]int{}
	var heat reportdomain.Heatmap
	for _, o := range pay.current {
		day := w.DayIndexOf(o.PaidAt)
		orders.add(w, day, 1)
		uid := uint64(max(o.UserID, 0))
		if uid != 0 {
			payerDays.Add(day, uid)
			perUser[uid]++
		}
		yuan := cents(o.Cents)
		amounts = append(amounts, yuan)
		methods.Add(o.Method, uid, yuan)
		providers.Add(o.Provider, uid, yuan)
		products.Add(o.Subject, uid, yuan)
		wd, hour := w.Weekday(o.PaidAt)
		reportengine.HeatmapAdd(&heat, wd, hour, 1)
	}
	for _, o := range pay.previous {
		orders.add(w, w.DayIndexOf(o.PaidAt), 1)
	}
	var repeat float64
	for _, n := range perUser {
		if n >= 2 {
			repeat++
		}
	}

	revenueTotal, revenuePrev := revenue.total(), revenue.prevTotal()
	netDaily := make([]float64, w.Days)
	for i := range netDaily {
		netDaily[i] = revenue.cur[i] - refund.cur[i]
	}
	payersN, prevPayersN := reportengine.Cardinality(payers), reportengine.Cardinality(prevPayers)
	activeN := reportengine.Cardinality(act.all.Union(0, w.Days-1))
	activePrevN := reportengine.Cardinality(act.all.Union(-w.Days, -1))
	netTotal, netPrev := revenueTotal-refund.total(), revenuePrev-refund.prevTotal()
	summary := reportengine.Summarize(amounts)

	return &reportdomain.Payment{
		Window: w.Public(now),
		KPIs: map[string]reportdomain.KPI{
			"revenue":     kpi(revenueTotal, revenuePrev, sumBuckets(w, revenue.cur)),
			"refunded":    kpi(refund.total(), refund.prevTotal(), sumBuckets(w, refund.cur)),
			"net":         kpi(netTotal, netPrev, sumBuckets(w, netDaily)),
			"orders":      kpi(orders.total(), orders.prevTotal(), sumBuckets(w, orders.cur)),
			"payers":      kpi(payersN, prevPayersN, uniqueBuckets(w, payerDays)),
			"newPayers":   kpi(reportengine.Cardinality(newPayers), reportengine.Cardinality(prevNewPayers), nil),
			"arppu":       ratioKPI(revenueTotal, payersN, revenuePrev, prevPayersN),
			"arpu":        ratioKPI(netTotal, activeN, netPrev, activePrevN),
			"avgOrder":    ratioKPI(revenueTotal, orders.total(), revenuePrev, orders.prevTotal()),
			"payRate":     ratioKPI(payersN, activeN, prevPayersN, activePrevN),
			"successRate": ratioKPI(paidOfCreated.total(), createdOrders.total(), paidOfCreated.prevTotal(), createdOrders.prevTotal()),
			"repeatRate":  reportengine.NewKPI(reportengine.Round(reportengine.Ratio(repeat, payersN), 4), nil, nil),
			"refundCount": kpi(refundCount.total(), refundCount.prevTotal(), nil),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"revenue":  sumBuckets(w, revenue.cur),
				"refunded": sumBuckets(w, refund.cur),
				"net":      sumBuckets(w, netDaily),
				"orders":   sumBuckets(w, orders.cur),
				"created":  sumBuckets(w, createdOrders.cur),
				"payers":   uniqueBuckets(w, payerDays),
			},
		},
		Methods:   methods.Slices(reportengine.SliceOptions{By: "amount"}),
		Providers: providers.Slices(reportengine.SliceOptions{By: "amount"}),
		Products:  products.Slices(reportengine.SliceOptions{By: "amount", Top: 20}),
		Statuses:  statuses,
		VipPlans:  vip,
		Amounts:   summary,
		Histogram: reportengine.Histogram(amounts, reportengine.MoneyEdges(summary.Max), moneyBinLabel),
		Heatmap:   heat,
		Insights:  topInsights(dailyInsights(w, "revenue", revenue.cur[:elapsed]), 6),
	}, nil
}

func moneyBinLabel(lo, hi float64) string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	if hi <= 0 || hi > 1e15 {
		return "≥" + f(lo)
	}
	return f(lo) + "-" + f(hi)
}

// Notification 通知
func (s *ReportService) Notification(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Notification, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		sent, read    []pgrepo.ReportDailyRow
		types, levels []reportdomain.Slice
		latencies     []float64
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) {
		sent, err = s.pg.ReportDaily(gctx, "notification.sent_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		read, err = s.pg.ReportDaily(gctx, "notification.read_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		types, err = s.groups(gctx, "notification.types", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		levels, err = s.groups(gctx, "notification.levels", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() error {
		return s.pg.ReportReadLatencies(gctx, appID, w.Start, w.End(), func(minutes float64) error {
			latencies = append(latencies, minutes)
			return nil
		})
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	notices, deliveries, _ := mergeDaily(w, sent)
	reads, _, _ := mergeDaily(w, read)
	var delivered, readOfSent float64
	for _, t := range types {
		delivered += float64(t.Users)
		readOfSent += t.Value
	}
	latency := reportengine.Summarize(latencies)
	return &reportdomain.Notification{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"sent":       kpi(notices.total(), notices.prevTotal(), sumBuckets(w, notices.cur)),
			"deliveries": kpi(deliveries.total(), deliveries.prevTotal(), sumBuckets(w, deliveries.cur)),
			"reads":      kpi(reads.total(), reads.prevTotal(), sumBuckets(w, reads.cur)),
			"readRate":   reportengine.NewKPI(reportengine.Round(reportengine.Ratio(readOfSent, delivered), 4), nil, nil),
			"medianRead": reportengine.NewKPI(latency.P50, nil, nil),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"sent":       sumBuckets(w, notices.cur),
				"deliveries": sumBuckets(w, deliveries.cur),
				"reads":      sumBuckets(w, reads.cur),
			},
		},
		Types:       types,
		Levels:      levels,
		ReadLatency: latency,
	}, nil
}

// Risk 风控：应用的风控评估，外加全局的防火墙拦截
func (s *ReportService) Risk(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Risk, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		daily, fwDaily                            []pgrepo.ReportDailyRow
		levels, actions, scenes, rules, ips       []reportdomain.Slice
		fwSeverity, fwRules, fwCountries, fwPaths []reportdomain.Slice
		scores, latencies                         []float64
	)
	g, gctx := reportGroup(ctx)
	// 风控流水在风控平台（ClickHouse），一次取齐；风控没接入或不可用时这一块为空，不影响防火墙那一半
	g.Go(func() error {
		rr, err := s.riskReport(gctx, appID, w)
		if err != nil {
			s.log.Warn("报表：风控平台取数失败", zap.Int64("app", appID), zap.Error(err))
			return nil
		}
		daily, scores, latencies = rr.daily, rr.scores, rr.latencies
		levels, actions, scenes, rules, ips = rr.groups["levels"], rr.groups["actions"], rr.groups["scenes"], rr.groups["rules"], rr.groups["ips"]
		return nil
	})
	g.Go(func() (err error) {
		fwDaily, err = s.pg.ReportDaily(gctx, "firewall.daily", 0, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		fwSeverity, err = s.groups(gctx, "firewall.severity", 0, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		fwRules, err = s.groups(gctx, "firewall.rules", 0, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		fwCountries, err = s.groups(gctx, "firewall.countries", 0, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		fwPaths, err = s.groups(gctx, "firewall.paths", 0, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	total, blocked, _ := mergeDaily(w, daily)
	byLevel := keyedDaily(w, daily, func(r pgrepo.ReportDailyRow) float64 { return r.A })
	series := map[string][]float64{}
	for level, d := range byLevel {
		series[level] = sumBuckets(w, d)
	}
	fwByLevel := keyedDaily(w, fwDaily, func(r pgrepo.ReportDailyRow) float64 { return r.A })
	fwSeries := map[string][]float64{}
	var fwTotal float64
	for sev, d := range fwByLevel {
		fwSeries[sev] = sumBuckets(w, d)
		fwTotal += reportengine.Sum(d)
	}
	scoreSummary, latencySummary := reportengine.Summarize(scores), reportengine.Summarize(latencies)
	var flagged float64
	for _, a := range actions {
		if a.Key != "pass" {
			flagged += float64(a.Count)
		}
	}
	return &reportdomain.Risk{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"assessments": kpi(total.total(), total.prevTotal(), sumBuckets(w, total.cur)),
			"blocked":     kpi(blocked.total(), blocked.prevTotal(), sumBuckets(w, blocked.cur)),
			"blockRate":   ratioKPI(blocked.total(), total.total(), blocked.prevTotal(), total.prevTotal()),
			"flagged":     reportengine.NewKPI(flagged, nil, nil),
			"avgScore":    reportengine.NewKPI(scoreSummary.Mean, nil, nil),
			"p90Latency":  reportengine.NewKPI(latencySummary.P90, nil, nil),
			"firewall":    reportengine.NewKPI(fwTotal, nil, nil),
		},
		Trend:   reportdomain.Trend{Buckets: w.Buckets(), Series: series},
		Levels:  levels,
		Actions: actions,
		Scenes:  scenes,
		Rules:   rules,
		IPs:     ips,
		Scores:  scoreSummary,
		Latency: latencySummary,
		Firewall: reportdomain.Firewall{
			Total:     int64(fwTotal),
			Trend:     reportdomain.Trend{Buckets: w.Buckets(), Series: fwSeries},
			Severity:  fwSeverity,
			Rules:     fwRules,
			Countries: fwCountries,
			Paths:     fwPaths,
		},
	}, nil
}

// Activity 抽奖活动
func (s *ReportService) Activity(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Activity, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		daily                  []pgrepo.ReportDailyRow
		activities             []pgrepo.ReportLotteryActivity
		prizes, kinds, sources []reportdomain.Slice
		draws, participants    int64
		wins, prevDraws        int64
		prevParticipants       int64
		prevWins               int64
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) {
		daily, err = s.pg.ReportDaily(gctx, "activity.daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		activities, err = s.pg.ReportLotteryActivities(gctx, appID, w.Start, w.End())
		return
	})
	g.Go(func() (err error) {
		draws, participants, wins, err = s.pg.ReportLotteryTotals(gctx, appID, w.Start, w.End())
		return
	})
	g.Go(func() (err error) {
		prevDraws, prevParticipants, prevWins, err = s.pg.ReportLotteryTotals(gctx, appID, w.PrevStart(), w.Start)
		return
	})
	g.Go(func() (err error) {
		prizes, err = s.groups(gctx, "activity.prizes", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		kinds, err = s.groups(gctx, "activity.kinds", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		sources, err = s.groups(gctx, "activity.sources", appID, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	drawDaily, winDaily, costDaily := mergeDaily(w, daily)
	rows := make([]reportdomain.ActivityRow, len(activities))
	for i, a := range activities {
		rows[i] = reportdomain.ActivityRow{
			ID: a.ID, Name: a.Name, Status: a.Status, Draws: a.Draws, Participants: a.Participants,
			Wins: a.Wins, WinRate: reportengine.Round(reportengine.Ratio(float64(a.Wins), float64(a.Draws)), 4),
			PointsCost: a.PointsCost, Pity: a.Pity,
		}
	}
	return &reportdomain.Activity{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"draws":        kpi(float64(draws), float64(prevDraws), sumBuckets(w, drawDaily.cur)),
			"participants": kpi(float64(participants), float64(prevParticipants), nil),
			"wins":         kpi(float64(wins), float64(prevWins), sumBuckets(w, winDaily.cur)),
			"winRate":      ratioKPI(float64(wins), float64(draws), float64(prevWins), float64(prevDraws)),
			"perUser":      ratioKPI(float64(draws), float64(participants), float64(prevDraws), float64(prevParticipants)),
			"pointsCost":   kpi(costDaily.total(), costDaily.prevTotal(), sumBuckets(w, costDaily.cur)),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"draws":      sumBuckets(w, drawDaily.cur),
				"wins":       sumBuckets(w, winDaily.cur),
				"pointsCost": sumBuckets(w, costDaily.cur),
			},
		},
		Activities: rows,
		Prizes:     prizes,
		Kinds:      kinds,
		Sources:    sources,
	}, nil
}

// Engagement 互动：签到、积分流转、激励广告与开屏
func (s *ReportService) Engagement(ctx context.Context, appID int64, p ReportParams) (*reportdomain.Engagement, error) {
	w, err := s.window(p)
	if err != nil {
		return nil, err
	}
	var (
		act                                          *activityFacts
		signs, points, ads                           []pgrepo.ReportDailyRow
		streakRows                                   []pgrepo.ReportGroupRow
		signSources, earned, spent, rewarded, scenes []reportdomain.Slice
		splash                                       []reportdomain.Slice
	)
	g, gctx := reportGroup(ctx)
	g.Go(func() (err error) { act, err = s.activity(gctx, appID, w); return })
	g.Go(func() (err error) {
		signs, err = s.pg.ReportDaily(gctx, "engagement.sign_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		points, err = s.pg.ReportDaily(gctx, "engagement.points_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		ads, err = s.pg.ReportDaily(gctx, "engagement.ads_daily", appID, w.PrevStart(), w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		streakRows, err = s.pg.ReportGroups(gctx, "engagement.streaks", appID, w.Start, w.End(), w.TZ)
		return
	})
	g.Go(func() (err error) {
		signSources, err = s.groups(gctx, "engagement.sign_sources", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		earned, err = s.groups(gctx, "engagement.points_earned", appID, w, reportengine.SliceOptions{By: "value", Top: 12})
		return
	})
	g.Go(func() (err error) {
		spent, err = s.groups(gctx, "engagement.points_spent", appID, w, reportengine.SliceOptions{By: "value", Top: 12})
		return
	})
	g.Go(func() (err error) {
		rewarded, err = s.groups(gctx, "engagement.rewarded", appID, w, reportengine.SliceOptions{})
		return
	})
	g.Go(func() (err error) {
		scenes, err = s.groups(gctx, "engagement.rewarded_scenes", appID, w, reportengine.SliceOptions{Top: 12})
		return
	})
	g.Go(func() (err error) {
		splash, err = s.groups(gctx, "engagement.splash", appID, w, reportengine.SliceOptions{})
		return
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	signDaily, signReward, _ := mergeDaily(w, signs)
	earnDaily, spendDaily, _ := mergeDaily(w, points)
	adDaily, adGranted, _ := mergeDaily(w, ads)
	signers := act.signals[pgrepo.ReportSignalSignIn]
	signUsers := reportengine.Cardinality(signers.Union(0, w.Days-1))
	prevSignUsers := reportengine.Cardinality(signers.Union(-w.Days, -1))
	hist := map[int]int64{}
	for _, row := range streakRows {
		n, _ := strconv.Atoi(row.Key)
		hist[n] += row.Count
	}
	var splashShown float64
	for _, sl := range splash {
		if sl.Key == "shown" || sl.Key == "clicked" {
			splashShown += float64(sl.Count)
		}
	}
	return &reportdomain.Engagement{
		Window: w.Public(s.now()),
		KPIs: map[string]reportdomain.KPI{
			"signins":      kpi(signDaily.total(), signDaily.prevTotal(), sumBuckets(w, signDaily.cur)),
			"signUsers":    kpi(signUsers, prevSignUsers, uniqueBuckets(w, signers)),
			"signReward":   kpi(signReward.total(), signReward.prevTotal(), nil),
			"pointsEarned": kpi(earnDaily.total(), earnDaily.prevTotal(), sumBuckets(w, earnDaily.cur)),
			"pointsSpent":  kpi(spendDaily.total(), spendDaily.prevTotal(), sumBuckets(w, spendDaily.cur)),
			"adViews":      kpi(adDaily.total(), adDaily.prevTotal(), sumBuckets(w, adDaily.cur)),
			"adGrantRate":  ratioKPI(adGranted.total(), adDaily.total(), adGranted.prevTotal(), adDaily.prevTotal()),
			"splashShown":  reportengine.NewKPI(splashShown, nil, nil),
		},
		Trend: reportdomain.Trend{
			Buckets: w.Buckets(),
			Series: map[string][]float64{
				"signins":      sumBuckets(w, signDaily.cur),
				"signUsers":    uniqueBuckets(w, signers),
				"pointsEarned": sumBuckets(w, earnDaily.cur),
				"pointsSpent":  sumBuckets(w, spendDaily.cur),
				"adViews":      sumBuckets(w, adDaily.cur),
				"adGranted":    sumBuckets(w, adGranted.cur),
			},
		},
		SignSources:    signSources,
		Streaks:        reportengine.BandHistogram(hist, []int{1, 3, 7, 14, 30}),
		PointsEarned:   earned,
		PointsSpent:    spent,
		RewardedAds:    rewarded,
		RewardedScenes: scenes,
		SplashAds:      splash,
	}, nil
}

type riskReportData struct {
	daily             []pgrepo.ReportDailyRow
	groups            map[string][]reportdomain.Slice
	scores, latencies []float64
}

// riskReport 从风控平台取应用的风控报表数据（risk-admin /v1/report），转成报表引擎的行
func (s *ReportService) riskReport(ctx context.Context, appID int64, w *reportengine.Window) (*riskReportData, error) {
	if s.risk == nil || !s.risk.Enabled() {
		return &riskReportData{groups: map[string][]reportdomain.Slice{}}, nil
	}
	var raw struct {
		Daily []struct {
			Day string  `json:"day"`
			Key string  `json:"key"`
			A   float64 `json:"a"`
			B   float64 `json:"b"`
		} `json:"daily"`
		Groups map[string][]struct {
			Key    string  `json:"key"`
			Label  string  `json:"label"`
			Count  int64   `json:"count"`
			Users  int64   `json:"users"`
			Amount float64 `json:"amount"`
			Value  float64 `json:"value"`
		} `json:"groups"`
		Scores    []float64 `json:"scores"`
		Latencies []float64 `json:"latencies"`
	}
	q := url.Values{"app": {strconv.FormatInt(appID, 10)}, "from": {w.PrevStart().UTC().Format(time.RFC3339)}, "to": {w.End().UTC().Format(time.RFC3339)}, "tz": {w.TZ}}
	if err := s.risk.AdminGet(ctx, "report", q, &raw); err != nil {
		return nil, err
	}
	out := &riskReportData{groups: map[string][]reportdomain.Slice{}, scores: raw.Scores, latencies: raw.Latencies}
	for _, d := range raw.Daily {
		day, err := time.Parse(time.DateOnly, d.Day)
		if err != nil {
			continue
		}
		out.daily = append(out.daily, pgrepo.ReportDailyRow{Day: day, Key: d.Key, A: d.A, B: d.B})
	}
	for name, rows := range raw.Groups {
		conv := make([]pgrepo.ReportGroupRow, 0, len(rows))
		for _, r := range rows {
			conv = append(conv, pgrepo.ReportGroupRow{Key: r.Key, Label: r.Label, Count: r.Count, Users: r.Users, Amount: r.Amount, Value: r.Value})
		}
		out.groups[name] = reportengine.ToSlices(toGroups(conv), reportengine.SliceOptions{})
	}
	return out, nil
}
