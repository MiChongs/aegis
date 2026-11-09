package admin

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/graph"
	"aegis/services/risk/internal/lists"
	"aegis/services/risk/internal/store"

	"github.com/cloudwego/hertz/pkg/app"
)

// 账号处置：一键解除风控、关联删除风险账号（主服务 internal/service/risk_ops.go 编排，这里只管风控侧）。
//
//   - inspect：一组账号身上的风控处置（名单、待复核单、近期拦截）与它们的关联账号；
//   - release：撤掉选中的名单条目、复核单按「判定为正常」结案、可选加一段时间白名单、关系图封禁标记清掉；
//   - forget：账号已在主服务删除之后的收尾：可选拉黑它们用过的设备、待复核单按「确认有风险」结案、关系图标记为已删除。
//
// 封号、防火墙、会话在主服务里，不经这里。

const maxSubjects = 100

type subjectInput struct {
	AppID   int64   `json:"appId"`
	UserIDs []int64 `json:"userIds"`
}

func (in *subjectInput) check() error {
	if in.AppID <= 0 {
		return bad("要应用")
	}
	ids := make([]int64, 0, len(in.UserIDs))
	for _, id := range in.UserIDs {
		if id > 0 && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return bad("要至少一个用户")
	}
	if len(ids) > maxSubjects {
		return bad(fmt.Sprintf("一次最多 %d 个账号", maxSubjects))
	}
	in.UserIDs = ids
	return nil
}

// SubjectEntity 账号用过的设备 / IP 等对象，带它们身上的名单
type SubjectEntity struct {
	Kind     string        `json:"kind"` // device / ip / account / phone / email / identity / payer / passkey
	Value    string        `json:"value,omitempty"`
	Label    string        `json:"label"`
	LastSeen int64         `json:"lastSeen,omitempty"`
	Shared   bool          `json:"shared,omitempty"` // IP：共享出口或机房出口
	Users    int64         `json:"users,omitempty"`  // IP：用过它的用户数
	Banned   bool          `json:"banned,omitempty"`
	Lists    []lists.Entry `json:"lists"`
}

// SubjectBlock 近期被拦截的情况
type SubjectBlock struct {
	Blocked  uint64    `ch:"blocked" json:"blocked"`
	Reviewed uint64    `ch:"reviewed" json:"reviewed"`
	Total    uint64    `ch:"total" json:"total"`
	MaxScore int32     `ch:"max_score" json:"maxScore"`
	LastAt   time.Time `ch:"last_at" json:"lastAt"`
	LastHit  string    `ch:"last_hit" json:"lastHit"`
	LastWhy  string    `ch:"last_why" json:"lastReason"`
	LastScn  string    `ch:"last_scene" json:"lastScene"`
}

// Subject 一个账号在风控侧的全部状况
type Subject struct {
	UserID   int64           `json:"userId"`
	Name     string          `json:"name"`
	Banned   bool            `json:"banned"`
	Status   string          `json:"status,omitempty"`
	Lists    []lists.Entry   `json:"lists"` // 用户本身（user 名单）
	Entities []SubjectEntity `json:"entities"`
	Cases    []store.Case    `json:"cases"` // 待复核
	Recent   *SubjectBlock   `json:"recent,omitempty"`
}

func (s *Server) inspectSubjects(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		subjectInput
		Related bool `json:"related"`
		Days    int  `json:"days"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	days := in.Days
	if days <= 0 {
		days = 180
	}
	days = min(days, 365)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour)

	out := map[string]any{"appId": in.AppID, "days": days}
	subjects := make([]Subject, len(in.UserIDs))
	errs := map[string]string{}
	var mu sync.Mutex
	note := func(k string, err error) {
		if err != nil {
			mu.Lock()
			errs[k] = err.Error()
			mu.Unlock()
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	for i, uid := range in.UserIDs {
		wg.Add(1)
		go func(i int, uid int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sub, err := s.inspectOne(ctx, in.AppID, uid, since)
			note(fmt.Sprintf("user:%d", uid), err)
			subjects[i] = sub
		}(i, uid)
	}
	var related []graph.RelatedUser
	if in.Related && s.Graph != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Graph.RelatedUsers(in.AppID, in.UserIDs, graph.RelatedOptions{Since: since.Unix(), PerEntity: 30, Max: 200})
			note("related", err)
			related = r
		}()
	}
	wg.Wait()
	out["subjects"] = subjects
	out["related"] = nonNilSlice(related)
	out["errors"] = errs
	return out, nil
}

// inspectOne 一个账号：名单、实体、待复核单、近期拦截
func (s *Server) inspectOne(ctx context.Context, appID, uid int64, since time.Time) (Subject, error) {
	sub := Subject{UserID: uid, Lists: []lists.Entry{}, Entities: []SubjectEntity{}, Cases: []store.Case{}}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 用户名单（应用内或全平台）
	if es, err := s.Store.EntriesFor(ctx, bundle.ListUser, strconv.FormatInt(uid, 10)); err == nil {
		for _, e := range es {
			if e.AppID == 0 || e.AppID == appID {
				sub.Lists = append(sub.Lists, e)
			}
		}
	} else {
		keep(err)
	}
	// 关系图上直接连着的对象 + 流水里出现过的设备与 IP（关系图回灌之前的、IPv6 具体地址）
	type key struct{ kind, value string }
	ents := map[key]*SubjectEntity{}
	order := []key{}
	put := func(e SubjectEntity) *SubjectEntity {
		k := key{e.Kind, e.Value}
		if e.Value == "" {
			k.value = e.Label
		}
		if old, ok := ents[k]; ok {
			old.LastSeen = max(old.LastSeen, e.LastSeen)
			if old.Label == "" || old.Label == old.Value {
				old.Label = e.Label
			}
			return old
		}
		cp := e
		cp.Lists = []lists.Entry{}
		ents[k] = &cp
		order = append(order, k)
		return &cp
	}
	if s.Graph != nil {
		nodes, err := s.Graph.Entities(appID, uid, since.Unix())
		keep(err)
		for _, n := range nodes {
			e := SubjectEntity{Kind: n.Kind, Label: n.Label, LastSeen: num64(n.Props["last_seen"]), Banned: n.Props["banned"] == true}
			switch n.Kind {
			case graph.TagDevice:
				e.Value, _ = n.Props["device_id"].(string)
			case graph.TagIP:
				e.Value = strings.TrimPrefix(n.ID, "ip:")
				e.Users = num64(n.Props["users"])
				e.Shared = n.Props["shared"] == true || n.Props["hosting"] == true
			case graph.TagAccount:
				e.Value, _ = n.Props["name"].(string)
			case graph.TagSubnet, graph.TagClient:
				continue
			}
			put(e)
		}
		if props, err := s.Graph.Props([]string{graph.UserVID(appID, uid)}); err == nil {
			if p := props[graph.UserVID(appID, uid)]; p != nil {
				sub.Name, _ = p["nickname"].(string)
				if sub.Name == "" {
					sub.Name, _ = p["name"].(string)
				}
				sub.Banned = p["banned"] == true
				sub.Status, _ = p["status"].(string)
			}
		}
	}
	if s.CH != nil {
		conn := s.CH.Conn()
		type usedRow struct {
			Kind  string    `ch:"k"`
			Value string    `ch:"v"`
			Last  time.Time `ch:"last"`
		}
		var used []usedRow
		for _, col := range []struct{ kind, col string }{{"device", "device_id"}, {"ip", "ip"}} {
			var rows []usedRow
			err := conn.Select(ctx, &rows, "SELECT '"+col.kind+"' AS k, "+col.col+" AS v, max(ts) AS last FROM decisions "+
				"WHERE app_id = ? AND user_id = ? AND ts >= ? AND "+col.col+" != '' GROUP BY v ORDER BY last DESC LIMIT 30", appID, uid, since)
			keep(err)
			used = append(used, rows...)
		}
		for _, u := range used {
			e := SubjectEntity{Kind: u.Kind, Value: u.Value, Label: u.Value, LastSeen: u.Last.Unix()}
			if u.Kind == "ip" {
				// 关系图里 IPv6 按 /64 线路建点，手机的具体地址隔一阵就换：只有被拉进名单的具体地址才单列，其余并到线路上
				if line := graph.IPKey(u.Value); line != u.Value {
					if ln, ok := ents[key{"ip", line}]; ok {
						ln.LastSeen = max(ln.LastSeen, e.LastSeen)
					}
					if es, err := s.Store.EntriesFor(ctx, bundle.ListIP, u.Value); err != nil || len(es) == 0 {
						continue
					}
				}
			}
			if u.Kind == "device" {
				// 关系图的设备点有型号；流水里只有设备码
				found := false
				for _, k := range order {
					if k.kind == "device" && k.value == u.Value {
						found = true
						break
					}
				}
				if found {
					continue
				}
			}
			put(e)
		}
		var recent []SubjectBlock
		err := conn.Select(ctx, &recent, `
			SELECT countIf(action IN ('block','ban')) AS blocked, countIf(action = 'review') AS reviewed, count() AS total,
				max(score) AS max_score,
				maxIf(ts, action IN ('block','ban','review')) AS last_at,
				argMaxIf(arrayStringConcat(hits, ','), ts, action IN ('block','ban','review')) AS last_hit,
				argMaxIf(arrayStringConcat(reasons, '；'), ts, action IN ('block','ban','review')) AS last_why,
				argMaxIf(scene, ts, action IN ('block','ban','review')) AS last_scene
			FROM decisions WHERE app_id = ? AND user_id = ? AND ts >= ? AND shadow = 0`, appID, uid, time.Now().Add(-30*24*time.Hour))
		keep(err)
		if len(recent) > 0 && recent[0].Total > 0 {
			sub.Recent = &recent[0]
		}
	}
	// 每个对象身上的名单
	for _, k := range order {
		e := ents[k]
		var lt string
		switch e.Kind {
		case "device":
			lt = bundle.ListDevice
		case "ip":
			if graph.IsV6Net(e.Value) && strings.Contains(e.Value, "/") {
				continue // IPv6 线路本身不是名单取值
			}
			lt = bundle.ListIP
		case "account":
			lt = bundle.ListAccount
		default:
			continue
		}
		v, err := lists.Normalize(lt, e.Value)
		if err != nil {
			continue
		}
		es, err := s.Store.EntriesFor(ctx, lt, v)
		if err != nil {
			keep(err)
			continue
		}
		for _, x := range es {
			if x.AppID == 0 || x.AppID == appID {
				e.Lists = append(e.Lists, x)
			}
		}
	}
	for _, k := range order {
		sub.Entities = append(sub.Entities, *ents[k])
	}
	sort.SliceStable(sub.Entities, func(a, b int) bool {
		la, lb := len(sub.Entities[a].Lists), len(sub.Entities[b].Lists)
		if (la > 0) != (lb > 0) {
			return la > 0
		}
		return sub.Entities[a].LastSeen > sub.Entities[b].LastSeen
	})
	appID2 := appID
	cases, _, err := s.Store.ListCases(ctx, store.CaseQuery{Status: "pending", AppID: &appID2, UserID: uid, Limit: 50})
	keep(err)
	sub.Cases = nonNilSlice(cases)
	return sub, firstErr
}

func (s *Server) releaseSubjects(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		subjectInput
		EntryIDs   []string `json:"entryIds"`
		CloseCases bool     `json:"closeCases"`
		AllowDays  int      `json:"allowDays"`
		Note       string   `json:"note"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	if in.AllowDays < 0 || in.AllowDays > 365 {
		return nil, bad("白名单保护期 0-365 天")
	}
	by := operator(c)
	note := strings.TrimSpace(in.Note)
	reason := "一键解除风控"
	if note != "" {
		reason += "：" + note
	}
	res := map[string]any{}
	var problems []string

	removed, err := s.Store.RemoveEntries(ctx, in.EntryIDs, by)
	if err != nil {
		return nil, err
	}
	if err := s.Lists.Remove(ctx, removed); err != nil {
		problems = append(problems, "名单已移除，索引更新失败（可在系统页重建索引）："+err.Error())
	}
	s.markGraphBanned(removed, false)
	res["listsRemoved"] = len(removed)

	closed := 0
	if in.CloseCases {
		for _, uid := range in.UserIDs {
			appID := in.AppID
			cases, _, err := s.Store.ListCases(ctx, store.CaseQuery{Status: "pending", AppID: &appID, UserID: uid, Limit: 200})
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			for _, cs := range cases {
				if err := s.Store.ReviewCase(ctx, cs.ID, "approve", reason, by); err == nil {
					closed++
				}
			}
		}
	}
	res["casesClosed"] = closed

	allowed := 0
	if in.AllowDays > 0 {
		values := make([]string, len(in.UserIDs))
		for i, uid := range in.UserIDs {
			values[i] = strconv.FormatInt(uid, 10)
		}
		es, _, err := s.makeEntries(addListInput{ListType: bundle.ListUser, Kind: bundle.KindAllow, Values: values, AppID: in.AppID,
			Reason: reason, DurationSeconds: int64(in.AllowDays) * 86400}, "manual", by, "")
		if err == nil {
			err = s.putEntries(ctx, es)
		}
		if err != nil {
			problems = append(problems, "白名单保护没有加上："+err.Error())
		} else {
			allowed = len(es)
		}
	}
	res["allowed"] = allowed

	if s.Graph != nil {
		for _, uid := range in.UserIDs {
			if err := s.Graph.MarkUserReleased(in.AppID, uid); err != nil {
				problems = append(problems, "关系图标记没有更新："+err.Error())
				break
			}
		}
	}
	res["problems"] = nonNilSlice(problems)
	s.Store.Audit(ctx, by, "subjects.release", fmt.Sprintf("app %d users %v", in.AppID, in.UserIDs), map[string]any{
		"entries": in.EntryIDs, "closeCases": in.CloseCases, "allowDays": in.AllowDays, "note": note, "result": res})
	return res, nil
}

func (s *Server) forgetSubjects(ctx context.Context, c *app.RequestContext) (any, error) {
	var in struct {
		subjectInput
		BlockDevices []string `json:"blockDevices"`
		BlockDays    int      `json:"blockDays"` // 0 = 永久
		Note         string   `json:"note"`
	}
	if err := bind(c, &in); err != nil {
		return nil, err
	}
	if err := in.check(); err != nil {
		return nil, err
	}
	if in.BlockDays < 0 || in.BlockDays > 3650 {
		return nil, bad("设备拉黑时长 0-3650 天")
	}
	by := operator(c)
	note := strings.TrimSpace(in.Note)
	reason := "关联删除风险账号"
	if note != "" {
		reason += "：" + note
	}
	res := map[string]any{}
	var problems []string

	blocked := 0
	var devices []string
	for _, d := range in.BlockDevices {
		if d = strings.TrimSpace(d); d != "" && !slices.Contains(devices, d) {
			devices = append(devices, d)
		}
	}
	if len(devices) > 0 {
		es, invalid, err := s.makeEntries(addListInput{ListType: bundle.ListDevice, Kind: bundle.KindBlock, Values: devices, AppID: in.AppID,
			Reason: reason, DurationSeconds: int64(in.BlockDays) * 86400}, "manual", by, "")
		if err == nil {
			err = s.putEntries(ctx, es)
		}
		if err != nil {
			problems = append(problems, "设备没有拉黑："+err.Error())
		} else {
			blocked = len(es)
		}
		if len(invalid) > 0 {
			problems = append(problems, fmt.Sprintf("%d 个设备码不合法，已跳过", len(invalid)))
		}
	}
	res["devicesBlocked"] = blocked

	closed := 0
	for _, uid := range in.UserIDs {
		appID := in.AppID
		cases, _, err := s.Store.ListCases(ctx, store.CaseQuery{Status: "pending", AppID: &appID, UserID: uid, Limit: 200})
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		for _, cs := range cases {
			if err := s.Store.ReviewCase(ctx, cs.ID, "reject", reason+"（账号已删除）", by); err == nil {
				closed++
			}
		}
	}
	res["casesClosed"] = closed

	if s.Graph != nil {
		var gerr error
		for _, uid := range in.UserIDs {
			if err := s.Graph.MarkUserDeleted(in.AppID, uid); err != nil && gerr == nil {
				gerr = err
			}
		}
		if gerr != nil {
			problems = append(problems, "关系图标记没有更新："+gerr.Error())
		}
	}
	res["problems"] = nonNilSlice(problems)
	s.Store.Audit(ctx, by, "subjects.forget", fmt.Sprintf("app %d users %v", in.AppID, in.UserIDs), map[string]any{
		"blockDevices": len(devices), "blockDays": in.BlockDays, "note": note, "result": res})
	return res, nil
}

var errNoGraph = errors.New("关系图未就绪")

// relatedOf 单独查关联账号（GET /subjects/related?app=&user=）
func (s *Server) relatedSubjects(ctx context.Context, c *app.RequestContext) (any, error) {
	appID, uid := qint64(c, "app"), qint64(c, "user")
	if appID <= 0 || uid <= 0 {
		return nil, bad("要应用与用户")
	}
	if s.Graph == nil {
		return nil, errNoGraph
	}
	days := min(max(qint(c, "days", 180), 1), 365)
	since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	r, err := s.Graph.RelatedUsers(appID, []int64{uid}, graph.RelatedOptions{Since: since, PerEntity: 30, Max: 200})
	if err != nil {
		return nil, err
	}
	return map[string]any{"related": nonNilSlice(r)}, nil
}
