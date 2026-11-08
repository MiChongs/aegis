package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/events"
	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/store"
	"aegis/services/risk/internal/stream"
	"aegis/services/risk/internal/vector"
	riskv1 "aegis/services/risk/kitex_gen/aegis/risk/v1"

	"github.com/cloudwego/hertz/pkg/app"
)

// DecisionRow 决策流水一行（列表不带轨迹）
type DecisionRow struct {
	ID        string    `ch:"id" json:"id"`
	TS        time.Time `ch:"ts" json:"ts"`
	AppID     int64     `ch:"app_id" json:"appId"`
	Scene     string    `ch:"scene" json:"scene"`
	UserID    int64     `ch:"user_id" json:"userId"`
	Account   string    `ch:"account" json:"account"`
	IP        string    `ch:"ip" json:"ip"`
	DeviceID  string    `ch:"device_id" json:"deviceId"`
	Country   string    `ch:"country" json:"country"`
	City      string    `ch:"city" json:"city"`
	Action    string    `ch:"action" json:"action"`
	Level     string    `ch:"level" json:"level"`
	Score     int32     `ch:"score" json:"score"`
	Release   string    `ch:"release" json:"release"`
	Hits      []string  `ch:"hits" json:"hits"`
	Reasons   []string  `ch:"reasons" json:"reasons"`
	LatencyUS int64     `ch:"latency_us" json:"latencyUs"`
	Shadow    uint8     `ch:"shadow" json:"shadow"`
}

const decisionCols = "id, ts, app_id, scene, user_id, account, ip, device_id, country, city, action, level, score, release, hits, reasons, latency_us, shadow"

type chWhere struct {
	parts []string
	args  []any
}

func (w *chWhere) add(cond string, args ...any) {
	w.parts = append(w.parts, cond)
	w.args = append(w.args, args...)
}

func (w *chWhere) sql() string {
	if len(w.parts) == 0 {
		return "1"
	}
	return strings.Join(w.parts, " AND ")
}

func decisionFilter(c *app.RequestContext) *chWhere {
	w := &chWhere{}
	from := qtime(c, "from", time.Now().Add(-24*time.Hour))
	to := qtime(c, "to", time.Now().Add(time.Minute))
	w.add("ts >= ? AND ts < ?", from.UTC(), to.UTC())
	if a := qapp(c); a != nil {
		w.add("app_id = ?", *a)
	}
	for _, k := range []string{"scene", "action", "ip", "account", "release"} {
		if v := string(c.Query(k)); v != "" {
			w.add(k+" = ?", v)
		}
	}
	if v := string(c.Query("device")); v != "" {
		w.add("device_id = ?", v)
	}
	if v := qint64(c, "user"); v > 0 {
		w.add("user_id = ?", v)
	}
	if v := string(c.Query("rule")); v != "" {
		w.add("has(hits, ?)", v)
	}
	if string(c.Query("shadow")) == "true" {
		w.add("shadow = 1")
	} else {
		w.add("shadow = 0")
	}
	return w
}

func (s *Server) listDecisions(ctx context.Context, c *app.RequestContext) (any, error) {
	w := decisionFilter(c)
	page, size := qint(c, "page", 1), min(max(qint(c, "limit", 50), 1), 200)
	var total uint64
	if err := s.CH.Conn().QueryRow(ctx, "SELECT count() FROM decisions WHERE "+w.sql(), w.args...).Scan(&total); err != nil {
		return nil, err
	}
	var rows []DecisionRow
	// LIMIT 1 BY id：ReplacingMergeTree 合并前同一条重放的决策可能有两行
	q := fmt.Sprintf("SELECT %s FROM decisions WHERE %s ORDER BY ts DESC LIMIT 1 BY id LIMIT %d OFFSET %d", decisionCols, w.sql(), size, (page-1)*size)
	if err := s.CH.Conn().Select(ctx, &rows, q, w.args...); err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []DecisionRow{}
	}
	return paged(rows, int64(total), page, size), nil
}

func (s *Server) getDecision(ctx context.Context, c *app.RequestContext) (any, error) {
	var rows []struct {
		DecisionRow
		UA       string             `ch:"ua"`
		Province string             `ch:"province"`
		ISP      string             `ch:"isp"`
		ASN      uint32             `ch:"asn"`
		Features map[string]float64 `ch:"features"`
		Models   map[string]float64 `ch:"models"`
		Props    string             `ch:"props"`
		Trace    string             `ch:"trace"`
		Device   string             `ch:"device"`
	}
	if err := s.CH.Conn().Select(ctx, &rows, "SELECT "+decisionCols+", ua, province, isp, asn, features, models, props, trace, device FROM decisions WHERE id = ? LIMIT 1", c.Param("id")); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, missing("决策不存在（流水写入有几秒延迟）")
	}
	r := rows[0]
	var trace, props, device any
	_ = json.Unmarshal([]byte(r.Trace), &trace)
	_ = json.Unmarshal([]byte(r.Props), &props)
	_ = json.Unmarshal([]byte(r.Device), &device)
	return map[string]any{"decision": r.DecisionRow, "ua": r.UA, "province": r.Province, "isp": r.ISP, "asn": r.ASN,
		"features": r.Features, "models": r.Models, "extra": props, "trace": trace, "device": device}, nil
}

type kv struct {
	Key   string `ch:"k" json:"key"`
	Count uint64 `ch:"n" json:"count"`
}

type series struct {
	T      time.Time `ch:"t" json:"t"`
	Action string    `ch:"action" json:"action"`
	Count  uint64    `ch:"n" json:"count"`
}

func (s *Server) dashboard(ctx context.Context, c *app.RequestContext) (any, error) {
	from := qtime(c, "from", time.Now().Add(-24*time.Hour)).UTC()
	to := qtime(c, "to", time.Now().Add(time.Minute)).UTC()
	app := qapp(c)
	appCond, appArgs := "", []any{}
	if app != nil {
		appCond, appArgs = " AND app_id = ?", []any{*app}
	}
	// scene：两张表都有这一列；user：只有明细表有，按用户看时分布与趋势改走明细表
	if sc := strings.TrimSpace(string(c.Query("scene"))); sc != "" {
		appCond += " AND scene = ?"
		appArgs = append(appArgs, sc)
	}
	userID := qint64(c, "user")
	userCond, userArgs := "", []any{}
	if userID > 0 {
		userCond, userArgs = " AND user_id = ?", []any{userID}
	}
	args := func(extra ...any) []any { return append(append([]any{from, to}, appArgs...), extra...) }
	dargs := func(extra ...any) []any {
		return append(append(append([]any{from, to}, appArgs...), userArgs...), extra...)
	}
	bucket := "toStartOfHour(minute)"
	if to.Sub(from) > 7*24*time.Hour {
		bucket = "toStartOfDay(minute)"
	} else if to.Sub(from) <= 6*time.Hour {
		bucket = "toStartOfFiveMinutes(minute)"
	}
	out := map[string]any{"from": from, "to": to}
	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	run := func(name string, fn func() (any, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s：%w", name, err)
				}
				return
			}
			out[name] = v
		}()
	}
	conn := s.CH.Conn()
	run("byAction", func() (any, error) {
		var r []kv
		if userID > 0 {
			err := conn.Select(ctx, &r, "SELECT action AS k, count() AS n FROM decisions WHERE ts >= ? AND ts < ? AND shadow = 0"+appCond+userCond+" GROUP BY k ORDER BY n DESC", dargs()...)
			return r, err
		}
		err := conn.Select(ctx, &r, "SELECT action AS k, sum(n) AS n FROM decisions_minute WHERE minute >= ? AND minute < ? AND shadow = 0"+appCond+" GROUP BY k ORDER BY n DESC", args()...)
		return r, err
	})
	run("byScene", func() (any, error) {
		var r []struct {
			Scene  string `ch:"scene" json:"scene"`
			Action string `ch:"action" json:"action"`
			Count  uint64 `ch:"n" json:"count"`
		}
		err := conn.Select(ctx, &r, "SELECT scene, action, sum(n) AS n FROM decisions_minute WHERE minute >= ? AND minute < ? AND shadow = 0"+appCond+" GROUP BY scene, action ORDER BY n DESC", args()...)
		return r, err
	})
	run("series", func() (any, error) {
		var r []series
		if userID > 0 {
			err := conn.Select(ctx, &r, "SELECT "+strings.ReplaceAll(bucket, "minute", "ts")+" AS t, action, count() AS n FROM decisions WHERE ts >= ? AND ts < ? AND shadow = 0"+appCond+userCond+" GROUP BY t, action ORDER BY t", dargs()...)
			return r, err
		}
		err := conn.Select(ctx, &r, "SELECT "+bucket+" AS t, action, sum(n) AS n FROM decisions_minute WHERE minute >= ? AND minute < ? AND shadow = 0"+appCond+" GROUP BY t, action ORDER BY t", args()...)
		return r, err
	})
	run("latency", func() (any, error) {
		var r []struct {
			Avg float64 `ch:"avg" json:"avg"`
			P50 float64 `ch:"p50" json:"p50"`
			P99 float64 `ch:"p99" json:"p99"`
		}
		err := conn.Select(ctx, &r, "SELECT ifNotFinite(avg(latency_us), 0) AS avg, ifNotFinite(quantile(0.5)(latency_us), 0) AS p50, ifNotFinite(quantile(0.99)(latency_us), 0) AS p99 FROM decisions WHERE ts >= ? AND ts < ? AND shadow = 0"+appCond, args()...)
		if len(r) == 0 {
			return map[string]float64{}, err
		}
		return r[0], err
	})
	run("topRules", func() (any, error) {
		var r []kv
		err := conn.Select(ctx, &r, "SELECT rule AS k, sum(n) AS n FROM rule_hits_hour WHERE hour >= toStartOfHour(?) AND hour < ?"+appCond+" GROUP BY k ORDER BY n DESC LIMIT 12", args()...)
		return r, err
	})
	run("topIPs", func() (any, error) {
		var r []kv
		err := conn.Select(ctx, &r, "SELECT ip AS k, count() AS n FROM decisions WHERE ts >= ? AND ts < ? AND shadow = 0 AND action IN ('block','ban') AND ip != ''"+appCond+" GROUP BY k ORDER BY n DESC LIMIT 10", args()...)
		return r, err
	})
	run("topCountries", func() (any, error) {
		var r []kv
		err := conn.Select(ctx, &r, "SELECT country AS k, count() AS n FROM decisions WHERE ts >= ? AND ts < ? AND shadow = 0 AND action IN ('block','ban','review','captcha')"+appCond+" GROUP BY k ORDER BY n DESC LIMIT 10", args()...)
		return r, err
	})
	run("alerts", func() (any, error) {
		var r []kv
		err := conn.Select(ctx, &r, "SELECT pattern AS k, count() AS n FROM alerts WHERE ts >= ? AND ts < ?"+appCond+" GROUP BY k ORDER BY n DESC LIMIT 10", args()...)
		return r, err
	})
	run("pendingCases", func() (any, error) {
		_, total, err := s.Store.ListCases(ctx, store.CaseQuery{Status: "pending", AppID: app, Limit: 1})
		return total, err
	})
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

// EventRow 业务事件一行
type EventRow struct {
	ID           string    `ch:"id" json:"id"`
	TS           time.Time `ch:"ts" json:"ts"`
	Type         string    `ch:"type" json:"type"`
	Scene        string    `ch:"scene" json:"scene"`
	AppID        int64     `ch:"app_id" json:"appId"`
	UserID       int64     `ch:"user_id" json:"userId"`
	Account      string    `ch:"account" json:"account"`
	IP           string    `ch:"ip" json:"ip"`
	DeviceID     string    `ch:"device_id" json:"deviceId"`
	Country      string    `ch:"country" json:"country"`
	City         string    `ch:"city" json:"city"`
	Amount       float64   `ch:"amount" json:"amount"`
	TargetUserID int64     `ch:"target_user_id" json:"targetUserId"`
	Props        string    `ch:"props" json:"props"`
}

func (s *Server) listEvents(ctx context.Context, c *app.RequestContext) (any, error) {
	w := &chWhere{}
	w.add("ts >= ? AND ts < ?", qtime(c, "from", time.Now().Add(-7*24*time.Hour)).UTC(), qtime(c, "to", time.Now().Add(time.Minute)).UTC())
	if a := qapp(c); a != nil {
		w.add("app_id = ?", *a)
	}
	if v := qint64(c, "user"); v > 0 {
		w.add("user_id = ?", v)
	}
	if v := string(c.Query("type")); v != "" {
		w.add("type = ?", v)
	}
	if v := string(c.Query("device")); v != "" {
		w.add("device_id = ?", v)
	}
	if v := string(c.Query("ip")); v != "" {
		w.add("ip = ?", v)
	}
	size := min(max(qint(c, "limit", 50), 1), 500)
	var rows []EventRow
	err := s.CH.Conn().Select(ctx, &rows, fmt.Sprintf("SELECT id, ts, type, scene, app_id, user_id, account, ip, device_id, country, city, amount, target_user_id, props FROM events WHERE %s ORDER BY ts DESC LIMIT 1 BY id LIMIT %d", w.sql(), size), w.args...)
	if rows == nil {
		rows = []EventRow{}
	}
	return rows, err
}

// AlertRow 告警一行
type AlertRow struct {
	ID          string    `ch:"id" json:"id"`
	TS          time.Time `ch:"ts" json:"ts"`
	AppID       int64     `ch:"app_id" json:"appId"`
	Pattern     string    `ch:"pattern" json:"pattern"`
	PatternName string    `ch:"pattern_name" json:"patternName"`
	Entity      string    `ch:"entity" json:"entity"`
	Value       string    `ch:"value" json:"value"`
	UserID      int64     `ch:"user_id" json:"userId"`
	DeviceID    string    `ch:"device_id" json:"deviceId"`
	IP          string    `ch:"ip" json:"ip"`
	Severity    string    `ch:"severity" json:"severity"`
	EventIDs    []string  `ch:"event_ids" json:"eventIds"`
	Detail      string    `ch:"detail" json:"detail"`
}

func (s *Server) queryAlerts(ctx context.Context, w *chWhere, limit int) ([]AlertRow, error) {
	var rows []AlertRow
	err := s.CH.Conn().Select(ctx, &rows, fmt.Sprintf("SELECT id, ts, app_id, pattern, pattern_name, entity, value, user_id, device_id, ip, severity, event_ids, detail FROM alerts WHERE %s ORDER BY ts DESC LIMIT 1 BY id LIMIT %d", w.sql(), limit), w.args...)
	if rows == nil {
		rows = []AlertRow{}
	}
	return rows, err
}

func (s *Server) listAlerts(ctx context.Context, c *app.RequestContext) (any, error) {
	w := &chWhere{}
	w.add("ts >= ? AND ts < ?", qtime(c, "from", time.Now().Add(-7*24*time.Hour)).UTC(), qtime(c, "to", time.Now().Add(time.Minute)).UTC())
	if a := qapp(c); a != nil {
		w.add("app_id = ?", *a)
	}
	if v := string(c.Query("pattern")); v != "" {
		w.add("pattern = ?", v)
	}
	if v := qint64(c, "user"); v > 0 {
		w.add("user_id = ?", v)
	}
	return s.queryAlerts(ctx, w, min(max(qint(c, "limit", 100), 1), 500))
}

// ── 复核 ──

// caseStats 复核单全局统计：待复核按风险分段、最早一张待复核的创建时间、近 30 天结案结论
func (s *Server) caseStats(ctx context.Context, c *app.RequestContext) (any, error) {
	return s.Store.CaseStats(ctx, qapp(c))
}

func (s *Server) listCases(ctx context.Context, c *app.RequestContext) (any, error) {
	q := store.CaseQuery{Status: string(c.Query("status")), AppID: qapp(c), UserID: qint64(c, "user"), Scene: string(c.Query("scene")),
		Page: qint(c, "page", 1), Limit: qint(c, "limit", 50)}
	list, total, err := s.Store.ListCases(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []store.Case{}
	}
	return paged(list, total, q.Page, q.Limit), nil
}

type disposeInput struct {
	AppID           int64    `json:"appId"`
	UserID          int64    `json:"userId"`
	Account         string   `json:"account"`
	DeviceID        string   `json:"deviceId"`
	IP              string   `json:"ip"`
	Kind            string   `json:"kind"`    // block / allow / watch
	Targets         []string `json:"targets"` // device / ip / account / user / ban_account / firewall
	DurationSeconds int64    `json:"durationSeconds"`
	Reason          string   `json:"reason"`
}

// applyDispose 人工处置：名单类直接落库 + 索引；封号与防火墙发命令给主服务
func (s *Server) applyDispose(ctx context.Context, in disposeInput, source, by, decisionID string) (map[string]any, error) {
	if in.Kind == "" {
		in.Kind = bundle.KindBlock
	}
	result := map[string]any{}
	var entries []lists.Entry
	for _, t := range in.Targets {
		var listType, value string
		appID := int64(0)
		switch t {
		case "device":
			listType, value = bundle.ListDevice, in.DeviceID
		case "ip":
			listType, value = bundle.ListIP, in.IP
		case "account":
			listType, value, appID = bundle.ListAccount, in.Account, in.AppID
		case "user":
			if in.UserID > 0 {
				listType, value, appID = bundle.ListUser, strconv.FormatInt(in.UserID, 10), in.AppID
			}
		case "ban_account", "firewall":
			kind, target, val := "account_ban", "user", strconv.FormatInt(in.UserID, 10)
			if t == "firewall" {
				kind, target, val = "firewall_ban", "ip", in.IP
			}
			if in.Kind != bundle.KindBlock {
				result[t] = "只有拉黑才会封号 / 封 IP"
				continue
			}
			if (t == "ban_account" && in.UserID <= 0) || val == "" {
				result[t] = "缺对象"
				continue
			}
			cmd := events.Command{ID: events.NewID(), Kind: kind, Target: target, Value: val, AppID: in.AppID, DurationSeconds: in.DurationSeconds,
				Reason: in.Reason, Source: source, DecisionID: decisionID, Operator: by}
			if err := s.Bus.PublishCommand(ctx, cmd); err != nil {
				result[t] = err.Error()
			} else {
				result[t] = "已下发"
			}
			continue
		default:
			result[t] = "未知对象"
			continue
		}
		if value == "" {
			result[t] = "缺对象"
			continue
		}
		es, invalid, err := s.makeEntries(addListInput{ListType: listType, Kind: in.Kind, Values: []string{value}, AppID: appID, Reason: in.Reason, DurationSeconds: in.DurationSeconds}, source, by, decisionID)
		if err != nil || len(invalid) > 0 {
			result[t] = "取值不合法"
			continue
		}
		entries = append(entries, es...)
		result[t] = "已加入名单"
	}
	if err := s.putEntries(ctx, entries); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) dispose(ctx context.Context, c *app.RequestContext) (any, error) {
	var in disposeInput
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	res, err := s.applyDispose(ctx, in, "manual", operator(c), "")
	if err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "dispose", fmt.Sprintf("app %d user %d", in.AppID, in.UserID), map[string]any{"input": in, "result": res})
	return res, nil
}

func (s *Server) reviewCase(ctx context.Context, c *app.RequestContext) (any, error) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var in struct {
		Verdict string        `json:"verdict"` // approve（放行，没问题）/ reject（确认有风险）
		Note    string        `json:"note"`
		Dispose *disposeInput `json:"dispose"`
		Allow   bool          `json:"allow"` // approve 时把对象加白
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if in.Verdict != "approve" && in.Verdict != "reject" {
		return nil, bad("结论只能是 approve 或 reject")
	}
	cs, err := s.Store.GetCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Store.ReviewCase(ctx, id, in.Verdict, in.Note, operator(c)); err != nil {
		return nil, bad(err.Error())
	}
	var res map[string]any
	base := disposeInput{AppID: cs.AppID, UserID: cs.UserID, Account: cs.Account, DeviceID: cs.DeviceID, IP: cs.IP, Reason: "复核：" + in.Note}
	switch {
	case in.Verdict == "reject" && in.Dispose != nil:
		d := *in.Dispose
		d.AppID, d.UserID, d.Account, d.DeviceID, d.IP = base.AppID, base.UserID, base.Account, base.DeviceID, base.IP
		if d.Reason == "" {
			d.Reason = base.Reason
		}
		res, err = s.applyDispose(ctx, d, "review", operator(c), cs.DecisionID)
	case in.Verdict == "approve" && in.Allow:
		base.Kind, base.Targets, base.DurationSeconds = bundle.KindAllow, []string{"device", "user"}, 30*86400
		res, err = s.applyDispose(ctx, base, "review", operator(c), cs.DecisionID)
	}
	if err != nil {
		return nil, err
	}
	s.Store.Audit(ctx, operator(c), "case.review", strconv.FormatInt(id, 10), map[string]any{"verdict": in.Verdict, "note": in.Note, "result": res})
	return map[string]any{"case": id, "dispose": res}, nil
}

// ── 关系图与用户画像 ──

func graphCenter(c *app.RequestContext) (string, error) {
	app := qint64(c, "app")
	value := strings.TrimSpace(string(c.Query("value")))
	switch string(c.Query("type")) {
	case "", "user":
		uid := qint64(c, "user")
		if uid == 0 {
			uid, _ = strconv.ParseInt(value, 10, 64)
		}
		if app <= 0 || uid <= 0 {
			return "", bad("要应用与用户")
		}
		return graph.UserVID(app, uid), nil
	case "device":
		return graph.DeviceVID(value), nil
	case "ip":
		return graph.IPVID(value), nil
	case "account":
		return graph.AccountVID(app, value), nil
	case "vid":
		return value, nil
	}
	return "", bad("类型只能是 user / device / ip / account")
}

func (s *Server) graph(ctx context.Context, c *app.RequestContext) (any, error) {
	if s.Graph == nil {
		return nil, errors.New("关系图未就绪")
	}
	center, err := graphCenter(c)
	if err != nil {
		return nil, err
	}
	o := graph.ExpandOptions{Depth: min(max(qint(c, "depth", 2), 1), 3), Fanout: min(max(qint(c, "fanout", 40), 5), 200),
		Max: min(max(qint(c, "max", 300), 20), 800), Subnets: string(c.Query("subnets")) == "1"}
	// days：只看最近 N 天出现过的关系（0 或不传不限）
	if d := qint(c, "days", 0); d > 0 {
		o.Since = time.Now().Add(-time.Duration(min(d, 3650)) * 24 * time.Hour).Unix()
	}
	return s.Graph.ExpandWith(center, o)
}

type dayAction struct {
	Day    time.Time `ch:"d" json:"day"`
	Action string    `ch:"action" json:"action"`
	Count  uint64    `ch:"n" json:"count"`
	MaxSc  int32     `ch:"max_score" json:"maxScore"`
	AvgSc  float64   `ch:"avg_score" json:"avgScore"`
}

type entityUse struct {
	Value    string    `ch:"v" json:"value"`
	LastSeen time.Time `ch:"last" json:"lastSeen"`
	First    time.Time `ch:"first" json:"firstSeen"`
	Count    uint64    `ch:"n" json:"count"`
	Country  string    `ch:"country" json:"country"`
	Province string    `ch:"province" json:"province,omitempty"`
	City     string    `ch:"city" json:"city"`
	ISP      string    `ch:"isp" json:"isp,omitempty"`
	ASN      int64     `ch:"asn" json:"asn,omitempty"`
	Device   string    `ch:"device" json:"-"`
}

// sceneUse 用户在某个场景的评估次数与被拦次数
type sceneUse struct {
	Key     string `ch:"k" json:"key"`
	Count   uint64 `ch:"n" json:"count"`
	Blocked uint64 `ch:"blocked" json:"blocked"`
	Flagged uint64 `ch:"flagged" json:"flagged"` // 没有直接放行的（复核、验证码、拦截、封禁）
}

// similarView 相似设备：补上型号与那位用户的名字，控制台能写成「张三的小米 14」
type similarView struct {
	vector.Neighbor
	Model    string `json:"model,omitempty"`
	UserName string `json:"userName,omitempty"`
}

// ipFlags 关系图里这个 IP 的判断（机房出口、多人共用、IPv6 线路）
type ipFlags struct {
	Users   int64  `json:"users,omitempty"`
	Hosting bool   `json:"hosting,omitempty"`
	Shared  bool   `json:"shared,omitempty"`
	Proxy   bool   `json:"proxy,omitempty"`
	VPN     bool   `json:"vpn,omitempty"`
	V6      bool   `json:"v6,omitempty"`
	Line    string `json:"line,omitempty"` // IPv6 所在的 /64 线路
}

// profile 用户风险画像：关系图 + 时间线 + 设备与 IP + 名单 + 复核 + 告警 + 相似设备
func (s *Server) profile(ctx context.Context, c *app.RequestContext) (any, error) {
	appID, uid := qint64(c, "app"), qint64(c, "user")
	if appID <= 0 || uid <= 0 {
		return nil, bad("要应用与用户")
	}
	days := min(max(qint(c, "days", 30), 1), 180)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	out := map[string]any{"appId": appID, "userId": uid, "days": days}
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := map[string]string{}
	run := func(name string, fn func() (any, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := fn()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[name] = err.Error()
				return
			}
			out[name] = v
		}()
	}
	conn := s.CH.Conn()
	base := "app_id = ? AND user_id = ? AND ts >= ? AND shadow = 0"
	args := []any{appID, uid, since}
	run("graph", func() (any, error) {
		if s.Graph == nil {
			return nil, errors.New("关系图未就绪")
		}
		// 与画像同一时间窗；网段不画（信息量少、点多）；共享 / 机房出口不往外展开
		return s.Graph.ExpandWith(graph.UserVID(appID, uid), graph.ExpandOptions{Depth: 2, Fanout: 40, Max: 260, Since: since.Unix()})
	})
	run("timeline", func() (any, error) {
		var r []dayAction
		err := conn.Select(ctx, &r, "SELECT toStartOfDay(ts) AS d, action, count() AS n, max(score) AS max_score, ifNotFinite(avg(score), 0) AS avg_score FROM decisions WHERE "+base+" GROUP BY d, action ORDER BY d", args...)
		return r, err
	})
	run("summary", func() (any, error) {
		var r []struct {
			Total    uint64    `ch:"total" json:"total"`
			Blocked  uint64    `ch:"blocked" json:"blocked"`
			Reviewed uint64    `ch:"reviewed" json:"reviewed"`
			Captcha  uint64    `ch:"captcha" json:"captcha"`
			MaxScore int32     `ch:"max_score" json:"maxScore"`
			AvgScore float64   `ch:"avg_score" json:"avgScore"`
			Scenes   uint64    `ch:"scenes" json:"scenes"`
			Devices  uint64    `ch:"devices" json:"devices"`
			IPs      uint64    `ch:"ips" json:"ips"`
			First    time.Time `ch:"first" json:"firstSeen"`
			Last     time.Time `ch:"last" json:"lastSeen"`
		}
		err := conn.Select(ctx, &r, `SELECT count() AS total, countIf(action IN ('block','ban')) AS blocked, countIf(action = 'review') AS reviewed,
			countIf(action = 'captcha') AS captcha, max(score) AS max_score, ifNotFinite(avg(score), 0) AS avg_score, uniqExact(scene) AS scenes,
			uniqExactIf(device_id, device_id != '') AS devices, uniqExactIf(ip, ip != '') AS ips, min(ts) AS first, max(ts) AS last
			FROM decisions WHERE `+base, args...)
		// 没有评估时聚合出来的首末时间是 1970-01-01，控制台会显示成「56 年前」：直接给空摘要
		if len(r) == 0 || r[0].Total == 0 {
			return map[string]any{}, err
		}
		return r[0], err
	})
	run("scenes", func() (any, error) {
		var r []sceneUse
		err := conn.Select(ctx, &r, "SELECT scene AS k, count() AS n, countIf(action IN ('block','ban')) AS blocked, countIf(action != 'pass') AS flagged FROM decisions WHERE "+base+" GROUP BY k ORDER BY n DESC", args...)
		return r, err
	})
	run("names", func() (any, error) { return s.configNames(ctx) })
	run("ruleMeta", func() (any, error) { return s.ruleMeta(ctx) })
	run("rules", func() (any, error) {
		var r []kv
		err := conn.Select(ctx, &r, "SELECT arrayJoin(hits) AS k, count() AS n FROM decisions WHERE "+base+" GROUP BY k ORDER BY n DESC LIMIT 15", args...)
		return r, err
	})
	run("decisions", func() (any, error) {
		var r []DecisionRow
		err := conn.Select(ctx, &r, "SELECT "+decisionCols+" FROM decisions WHERE "+base+" ORDER BY ts DESC LIMIT 1 BY id LIMIT 30", args...)
		if r == nil {
			r = []DecisionRow{}
		}
		return r, err
	})
	run("events", func() (any, error) {
		var r []EventRow
		err := conn.Select(ctx, &r, "SELECT id, ts, type, scene, app_id, user_id, account, ip, device_id, country, city, amount, target_user_id, props FROM events WHERE app_id = ? AND user_id = ? AND ts >= ? ORDER BY ts DESC LIMIT 1 BY id LIMIT 30", args...)
		if r == nil {
			r = []EventRow{}
		}
		return r, err
	})
	run("alerts", func() (any, error) {
		w := &chWhere{}
		w.add("app_id = ? AND user_id = ? AND ts >= ?", appID, uid, since)
		return s.queryAlerts(ctx, w, 30)
	})
	run("features", func() (any, error) {
		var r []struct {
			Scene    string             `ch:"scene" json:"scene"`
			TS       time.Time          `ch:"ts" json:"ts"`
			Features map[string]float64 `ch:"features" json:"features"`
			Models   map[string]float64 `ch:"models" json:"models"`
		}
		err := conn.Select(ctx, &r, "SELECT scene, ts, features, models FROM decisions WHERE "+base+" ORDER BY ts DESC LIMIT 1 BY scene LIMIT 12", args...)
		return r, err
	})
	run("cases", func() (any, error) {
		list, _, err := s.Store.ListCases(ctx, store.CaseQuery{AppID: &appID, UserID: uid, Limit: 20})
		if list == nil {
			list = []store.Case{}
		}
		return list, err
	})
	wg.Wait()

	// 设备、IP 与它们身上的名单、相似设备（依赖上面的流水，串行做）
	var devices, ips []entityUse
	_ = conn.Select(ctx, &devices, "SELECT device_id AS v, max(ts) AS last, min(ts) AS first, count() AS n, any(country) AS country, '' AS province, any(city) AS city, '' AS isp, toInt64(0) AS asn, argMax(device, ts) AS device FROM decisions WHERE "+base+" AND device_id != '' GROUP BY v ORDER BY last DESC LIMIT 20", args...)
	_ = conn.Select(ctx, &ips, "SELECT ip AS v, max(ts) AS last, min(ts) AS first, count() AS n, any(country) AS country, any(province) AS province, any(city) AS city, any(isp) AS isp, toInt64(any(asn)) AS asn, '' AS device FROM decisions WHERE "+base+" AND ip != '' GROUP BY v ORDER BY last DESC LIMIT 20", args...)
	type entityView struct {
		entityUse
		Lists       []lists.Entry     `json:"lists"`
		Fingerprint map[string]string `json:"fingerprint,omitempty"`
		Similar     []similarView     `json:"similar,omitempty"`
		Net         *ipFlags          `json:"net,omitempty"`
	}
	devViews := make([]entityView, len(devices))
	var swg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, d := range devices {
		v := entityView{entityUse: d}
		v.Lists, _ = s.Store.EntriesFor(ctx, bundle.ListDevice, d.Value)
		_ = json.Unmarshal([]byte(d.Device), &v.Fingerprint)
		devViews[i] = v
		if s.Vector == nil || len(v.Fingerprint) == 0 {
			continue
		}
		// 每台设备都查相似设备（并发 4 个、各 2 秒上限）
		swg.Add(1)
		go func(i int, fp map[string]string, id string) {
			defer swg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			ns, _ := s.Vector.Similar(sctx, appID, id, vector.Embed(fp), 10)
			views := make([]similarView, len(ns))
			for k, n := range ns {
				views[k] = similarView{Neighbor: n}
			}
			devViews[i].Similar = views
		}(i, v.Fingerprint, d.Value)
	}
	swg.Wait()
	// 相似设备补型号与用户名：所有相似设备与其用户一次从关系图取
	if s.Graph != nil {
		var ids []string
		seen := map[string]bool{}
		for _, v := range devViews {
			for _, n := range v.Similar {
				for _, id := range []string{graph.DeviceVID(n.DeviceID), graph.UserVID(appID, n.UserID)} {
					if n.UserID <= 0 && strings.HasPrefix(id, "u:") || seen[id] {
						continue
					}
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		if len(ids) > 0 {
			if props, err := s.Graph.Props(ids); err == nil {
				str := func(p map[string]any, k string) string { v, _ := p[k].(string); return strings.TrimSpace(v) }
				for i := range devViews {
					for k := range devViews[i].Similar {
						n := &devViews[i].Similar[k]
						if p := props[graph.DeviceVID(n.DeviceID)]; p != nil {
							n.Model = strings.TrimSpace(str(p, "brand") + " " + str(p, "model"))
						}
						if p := props[graph.UserVID(appID, n.UserID)]; p != nil {
							if n.UserName = str(p, "nickname"); n.UserName == "" {
								n.UserName = str(p, "name")
							}
						}
					}
				}
			}
		}
	}
	ipViews := make([]entityView, 0, len(ips))
	for _, p := range ips {
		v := entityView{entityUse: p}
		v.Lists, _ = s.Store.EntriesFor(ctx, bundle.ListIP, p.Value)
		ipViews = append(ipViews, v)
	}
	if s.Graph != nil && len(ipViews) > 0 {
		ids := make([]string, len(ipViews))
		for i, v := range ipViews {
			ids[i] = graph.IPVID(v.Value)
		}
		if props, err := s.Graph.Props(ids); err == nil {
			for i := range ipViews {
				p := props[ids[i]]
				if p == nil {
					continue
				}
				f := &ipFlags{Users: num64(p["users"]), Hosting: p["hosting"] == true, Shared: p["shared"] == true,
					Proxy: p["proxy"] == true, VPN: p["vpn"] == true, V6: p["v6"] == true}
				if f.V6 {
					f.Line = graph.IPKey(ipViews[i].Value)
				}
				if ipViews[i].ISP == "" {
					ipViews[i].ISP, _ = p["isp"].(string)
				}
				if ipViews[i].Province == "" {
					ipViews[i].Province, _ = p["province"].(string)
				}
				ipViews[i].Net = f
			}
		}
	}
	out["devices"], out["ips"] = nonNilSlice(devViews), nonNilSlice(ipViews)
	userLists, _ := s.Store.EntriesFor(ctx, bundle.ListUser, strconv.FormatInt(uid, 10))
	var mine []lists.Entry
	for _, e := range userLists {
		if e.AppID == 0 || e.AppID == appID {
			mine = append(mine, e)
		}
	}
	out["lists"] = nonNilSlice(mine)
	out["errors"] = errs
	return out, nil
}

func nonNilSlice[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// ── 状态 ──

func (s *Server) status(ctx context.Context, _ *app.RequestContext) (any, error) {
	comps := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	check := func(name string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			v := "ok"
			if err := fn(cctx); err != nil {
				v = err.Error()
			}
			mu.Lock()
			comps[name] = v
			mu.Unlock()
		}()
	}
	check("postgres", func(c context.Context) error { return s.Store.Pool().Ping(c) })
	check("redis", func(c context.Context) error { return s.Redis.Ping(c).Err() })
	check("clickhouse", func(c context.Context) error { return s.CH.Conn().Ping(c) })
	check("etcd", func(c context.Context) error { _, err := s.Etcd.Get(c, "/aegis/risk/ping"); return err })
	check("nats", func(context.Context) error {
		if s.Bus.Conn().IsConnected() {
			return nil
		}
		return errors.New("未连接")
	})
	check("nebula", func(context.Context) error {
		if s.Graph == nil {
			return errors.New("未连接")
		}
		_, err := s.Graph.Exec("SHOW HOSTS")
		return err
	})
	check("milvus", func(c context.Context) error {
		if s.Vector == nil {
			return errors.New("未连接")
		}
		_, err := s.Vector.Similar(c, 0, "", vector.Embed(map[string]string{"brand": "probe"}), 1)
		return err
	})
	check("risingwave", func(c context.Context) error {
		m, err := stream.Connect(c, s.Cfg.RisingWaveDSN, s.Stream)
		if err != nil {
			return err
		}
		defer m.Close(c)
		_, err = m.Existing(c)
		return err
	})
	var decisionStatus *riskv1.StatusResponse
	wg.Add(1)
	go func() {
		defer wg.Done()
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		st, err := s.Decision.Status(cctx, &riskv1.StatusRequest{})
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			comps["risk-decision"] = err.Error()
			return
		}
		comps["risk-decision"] = "ok"
		decisionStatus = st
	}()
	wg.Wait()
	return map[string]any{"components": comps, "decision": decisionStatus, "spoolPending": s.Bus.Pending()}, nil
}

// ── 应用报表（主服务的报表中心「风控」页取数）──

// ReportGroupRow 与主服务报表引擎的分组行同形
type ReportGroupRow struct {
	Key    string  `ch:"k" json:"key"`
	Label  string  `ch:"l" json:"label"`
	Count  uint64  `ch:"n" json:"count"`
	Users  uint64  `ch:"u" json:"users"`
	Amount float64 `ch:"a" json:"amount"`
	Value  float64 `ch:"v" json:"value"`
}

// ReportDailyRow 与主服务报表引擎的按日行同形
type ReportDailyRow struct {
	Day string  `ch:"d" json:"day"`
	Key string  `ch:"k" json:"key"`
	A   float64 `ch:"a" json:"a"`
	B   float64 `ch:"b" json:"b"`
}

func (s *Server) report(ctx context.Context, c *app.RequestContext) (any, error) {
	appID := qint64(c, "app")
	if appID <= 0 {
		return nil, bad("要应用")
	}
	from := qtime(c, "from", time.Now().Add(-7*24*time.Hour)).UTC()
	to := qtime(c, "to", time.Now()).UTC()
	tz := string(c.Query("tz"))
	if _, err := time.LoadLocation(tz); tz == "" || err != nil {
		tz = "Asia/Shanghai"
	}
	conn := s.CH.Conn()
	base := "app_id = ? AND ts >= ? AND ts < ? AND shadow = 0"
	args := []any{appID, from, to}
	users := "uniqExactIf(user_id, user_id > 0)"
	queries := map[string]string{
		"levels":  "SELECT level AS k, '' AS l, count() AS n, " + users + " AS u, 0. AS a, ifNotFinite(avg(score), 0) AS v FROM decisions WHERE " + base + " GROUP BY k",
		"actions": "SELECT action AS k, '' AS l, count() AS n, " + users + " AS u, 0. AS a, 0. AS v FROM decisions WHERE " + base + " GROUP BY k",
		"scenes":  "SELECT scene AS k, '' AS l, count() AS n, " + users + " AS u, 0. AS a, toFloat64(countIf(action IN ('block','ban'))) AS v FROM decisions WHERE " + base + " GROUP BY k",
		"rules":   "SELECT arrayJoin(hits) AS k, '' AS l, count() AS n, " + users + " AS u, 0. AS a, ifNotFinite(avg(score), 0) AS v FROM decisions WHERE " + base + " GROUP BY k ORDER BY n DESC LIMIT 20",
		"ips":     "SELECT ip AS k, any(country) AS l, count() AS n, uniqExact(if(user_id > 0, toString(user_id), account)) AS u, 0. AS a, toFloat64(max(score)) AS v FROM decisions WHERE " + base + " AND action != 'pass' AND ip != '' GROUP BY k ORDER BY n DESC LIMIT 20",
	}
	out := map[string]any{}
	groups := map[string][]ReportGroupRow{}
	for name, q := range queries {
		var rows []ReportGroupRow
		if err := conn.Select(ctx, &rows, q, args...); err != nil {
			return nil, fmt.Errorf("%s：%w", name, err)
		}
		groups[name] = nonNilSlice(rows)
	}
	out["groups"] = groups
	var daily []ReportDailyRow
	if err := conn.Select(ctx, &daily, "SELECT toString(toDate(ts, ?)) AS d, level AS k, toFloat64(count()) AS a, toFloat64(countIf(action IN ('block','ban'))) AS b FROM decisions WHERE "+base+" GROUP BY d, k ORDER BY d",
		append([]any{tz}, args...)...); err != nil {
		return nil, err
	}
	out["daily"] = nonNilSlice(daily)
	var samples []struct {
		Score   int32 `ch:"score"`
		Latency int64 `ch:"latency_us"`
	}
	if err := conn.Select(ctx, &samples, "SELECT score, latency_us FROM decisions WHERE "+base+" LIMIT 20000", args...); err != nil {
		return nil, err
	}
	scores, latencies := make([]float64, len(samples)), make([]float64, len(samples))
	for i, sm := range samples {
		scores[i], latencies[i] = float64(sm.Score), float64(sm.Latency)/1000
	}
	out["scores"], out["latencies"] = scores, latencies
	return out, nil
}

// userScore 用户近 N 天的最高风险分（抽奖行为画像用）
func (s *Server) userScore(ctx context.Context, c *app.RequestContext) (any, error) {
	appID, uid := qint64(c, "app"), qint64(c, "user")
	days := min(max(qint(c, "days", 30), 1), 365)
	var max32 int32
	err := s.CH.Conn().QueryRow(ctx, "SELECT max(score) FROM decisions WHERE app_id = ? AND user_id = ? AND ts >= ? AND shadow = 0",
		appID, uid, time.Now().Add(-time.Duration(days)*24*time.Hour).UTC()).Scan(&max32)
	if err != nil {
		return nil, err
	}
	return map[string]any{"maxScore": max32}, nil
}

func num64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

// configNames 规则、特征、模型的中文名（画像里只有键，控制台据此显示名称，不必再单独请求配置接口）
func (s *Server) configNames(ctx context.Context) (map[string]map[string]string, error) {
	out := map[string]map[string]string{"rules": {}, "features": {}, "models": {}}
	rules, err := s.Store.ListRules(ctx, "")
	if err != nil {
		return out, err
	}
	for _, r := range rules {
		if r.Name != "" {
			out["rules"][r.Key] = r.Name
		}
	}
	if fs, err := s.Store.ListFeatures(ctx); err == nil {
		for _, f := range fs {
			if f.Name != "" {
				out["features"][f.Key] = f.Name
			}
		}
	}
	if ms, err := s.Store.ListModels(ctx); err == nil {
		for _, m := range ms {
			if m.Name != "" {
				out["models"][m.Key] = m.Name
			}
		}
	}
	return out, nil
}

// ruleInfo 规则在控制台上要显示的几项（命中规则旁的维度、分值、是否观察中）
type ruleInfo struct {
	Name      string `json:"name"`
	Dimension string `json:"dimension"`
	Score     int    `json:"score"`
	Mode      string `json:"mode"`
	Decision  string `json:"decision,omitempty"`
}

// ruleMeta 规则键 → 显示信息（同一个键在多个场景时取最近更新的那条）
func (s *Server) ruleMeta(ctx context.Context) (map[string]ruleInfo, error) {
	rules, err := s.Store.ListRules(ctx, "")
	if err != nil {
		return map[string]ruleInfo{}, err
	}
	out := make(map[string]ruleInfo, len(rules))
	for _, r := range rules {
		out[r.Key] = ruleInfo{Name: r.Name, Dimension: r.Dimension, Score: r.Score, Mode: string(r.Mode), Decision: r.Decision}
	}
	return out, nil
}

// RuleHit 一条规则在近 N 天的命中情况
type RuleHit struct {
	Rule     string    `json:"rule"`
	Total    uint64    `json:"total"`
	LastHour time.Time `json:"lastHour"`
	Daily    []uint64  `json:"daily"` // 与 Days 一一对应，最早的一天在前
}

// RuleHitsView 规则命中统计：给规则列表标「近 7 天命中 N 次」和走势，并给出同期评估总量算命中率
type RuleHitsView struct {
	Days      []string  `json:"days"` // UTC 日期 yyyy-mm-dd
	Decisions uint64    `json:"decisions"`
	Rules     []RuleHit `json:"rules"`
}

// ruleHits GET /rules/hits?scene=&app=&days=7，只统计正式评估（影子评估不进 rule_hits_hour）
func (s *Server) ruleHits(ctx context.Context, c *app.RequestContext) (any, error) {
	days := qint(c, "days", 7)
	if days < 1 {
		days = 1
	} else if days > 90 {
		days = 90
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
	cond, args := "", []any{start}
	if a := qapp(c); a != nil {
		cond += " AND app_id = ?"
		args = append(args, *a)
	}
	if sc := strings.TrimSpace(string(c.Query("scene"))); sc != "" {
		cond += " AND scene = ?"
		args = append(args, sc)
	}
	out := RuleHitsView{Days: make([]string, days), Rules: []RuleHit{}}
	index := map[string]int{}
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		out.Days[i] = d
		index[d] = i
	}
	conn := s.CH.Conn()
	var rows []struct {
		Rule string    `ch:"rule"`
		Day  time.Time `ch:"d"`
		N    uint64    `ch:"n"`
		Last time.Time `ch:"last"`
	}
	if err := conn.Select(ctx, &rows, "SELECT rule, toDate(hour) AS d, sum(n) AS n, max(hour) AS last FROM rule_hits_hour WHERE hour >= ?"+cond+" GROUP BY rule, d", args...); err != nil {
		return nil, err
	}
	byRule := map[string]*RuleHit{}
	for _, r := range rows {
		h := byRule[r.Rule]
		if h == nil {
			h = &RuleHit{Rule: r.Rule, Daily: make([]uint64, days)}
			byRule[r.Rule] = h
		}
		if i, ok := index[r.Day.UTC().Format("2006-01-02")]; ok {
			h.Daily[i] += r.N
		}
		h.Total += r.N
		if r.Last.After(h.LastHour) {
			h.LastHour = r.Last
		}
	}
	for _, h := range byRule {
		out.Rules = append(out.Rules, *h)
	}
	sort.Slice(out.Rules, func(i, j int) bool { return out.Rules[i].Total > out.Rules[j].Total })
	var total []struct {
		N uint64 `ch:"n"`
	}
	if err := conn.Select(ctx, &total, "SELECT sum(n) AS n FROM decisions_minute WHERE minute >= ? AND shadow = 0"+cond, args...); err != nil {
		return nil, err
	}
	if len(total) > 0 {
		out.Decisions = total[0].N
	}
	return out, nil
}
