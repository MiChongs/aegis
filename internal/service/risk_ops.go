package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	riskevents "aegis/services/risk/events"

	"go.uber.org/zap"
)

// 账号处置：一键解除风控、关联删除风险账号。
//
// 风控对一个账号的处置散在两边：主服务里的封号（含踢下线）、防火墙封 IP；风控平台里的名单（账号、设备、IP、登录名）、
// 待复核单、关系图上的封禁标记。这里把两边合成一份清单给控制台逐项勾选，执行时两边一起改：
//
//   - Inspect：一组账号的基本情况、当前封禁、各自用过的设备与 IP 及其名单、防火墙封禁、待复核单、近 30 天拦截，
//     以及经关系图找到的关联账号（共用设备、手机号、邮箱、第三方身份、付款账号、通行密钥、登录名、邀请；IP 线路算弱关联）。
//   - Release：撤销选中的封号与防火墙封禁、移除选中的名单条目、复核单按「判定为正常」结案、可选加一段白名单保护。
//   - Purge：硬删除账号（与管理员删除、账号注销同一条 PurgeUser 路径），可选同时拉黑它们用过的设备，
//     风控侧的复核单按「确认有风险」结案，关系图删掉这些账号与只属于它们的点、设备向量改挂或删除、图特征缓存清掉。
//     拉黑的设备点保留，换号再来仍会命中。

const riskOpsMaxUsers = 100

type riskOpsStore interface {
	RiskOpsUsers(ctx context.Context, appID int64, ids []int64) ([]userdomain.RiskOpsUser, error)
	RiskBannedUsers(ctx context.Context, q userdomain.RiskBannedQuery) ([]userdomain.RiskOpsUser, int64, error)
	ActiveIPBansFor(ctx context.Context, ips []string) ([]firewalldomain.IPBan, error)
	RiskDeviceClusters(ctx context.Context, appID int64, minUsers, limit int) ([]userdomain.RiskDeviceCluster, error)
}

type riskUserPurger interface {
	PurgeUser(ctx context.Context, appID int64, userID int64) error
}

// SetOps 接账号处置需要的库查询与删号
func (s *RiskService) SetOps(store riskOpsStore, purger riskUserPurger) {
	s.mu.Lock()
	s.opsStore, s.purger = store, purger
	s.mu.Unlock()
}

// SetAccountCounters 接主服务里随账号处置更新的计数（注册准入、登录防爆破、排行榜、在线人数）
func (s *RiskService) SetAccountCounters(c *AccountCounters) {
	s.mu.Lock()
	s.counters = c
	s.mu.Unlock()
}

// RiskOpsOperator 执行处置的管理员
type RiskOpsOperator struct {
	AdminID int64
	Name    string
}

func (o RiskOpsOperator) label() string {
	if strings.TrimSpace(o.Name) == "" {
		return "管理员"
	}
	return o.Name
}

// ── 风控侧的数据（risk-admin /v1/subjects/*，JSON 原样带给控制台） ──

// RiskOpsEntity 账号用过的设备、IP、登录名等，带名单与（IP 的）防火墙封禁
type RiskOpsEntity struct {
	Kind     string                 `json:"kind"`
	Value    string                 `json:"value,omitempty"`
	Label    string                 `json:"label"`
	LastSeen int64                  `json:"lastSeen,omitempty"`
	Shared   bool                   `json:"shared,omitempty"`
	Users    int64                  `json:"users,omitempty"`
	Banned   bool                   `json:"banned,omitempty"`
	Lists    json.RawMessage        `json:"lists"`
	Firewall []firewalldomain.IPBan `json:"firewall,omitempty"`
}

type riskSubject struct {
	UserID   int64           `json:"userId"`
	Name     string          `json:"name"`
	Banned   bool            `json:"banned"`
	Status   string          `json:"status"`
	Lists    json.RawMessage `json:"lists"`
	Entities []RiskOpsEntity `json:"entities"`
	Cases    json.RawMessage `json:"cases"`
	Recent   json.RawMessage `json:"recent,omitempty"`
}

type riskRelated struct {
	UserID   int64           `json:"userId"`
	Name     string          `json:"name"`
	Banned   bool            `json:"banned"`
	Status   string          `json:"status,omitempty"`
	Strength string          `json:"strength"`
	Via      []int64         `json:"via"`
	Reasons  json.RawMessage `json:"reasons"`
	LastSeen int64           `json:"lastSeen"`
}

// RiskOpsSubject 一个账号的完整处置清单
type RiskOpsSubject struct {
	UserID int64                   `json:"userId"`
	User   *userdomain.RiskOpsUser `json:"user"` // 主库里已经没有这个账号时为 nil
	// 风控侧
	GraphBanned bool            `json:"graphBanned"`
	GraphStatus string          `json:"graphStatus,omitempty"`
	Lists       json.RawMessage `json:"lists"`
	Entities    []RiskOpsEntity `json:"entities"`
	Cases       json.RawMessage `json:"cases"`
	Recent      json.RawMessage `json:"recent,omitempty"`
}

// RiskOpsRelated 一个关联账号
type RiskOpsRelated struct {
	riskRelated
	User *userdomain.RiskOpsUser `json:"user"`
}

// RiskOpsInspection Inspect 的结果
type RiskOpsInspection struct {
	AppID    int64             `json:"appId"`
	Days     int               `json:"days"`
	Subjects []RiskOpsSubject  `json:"subjects"`
	Related  []RiskOpsRelated  `json:"related"`
	Errors   map[string]string `json:"errors"`
}

func (s *RiskService) ops() (riskOpsStore, riskUserPurger, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.opsStore == nil {
		return nil, nil, errors.New("账号处置未接入")
	}
	return s.opsStore, s.purger, nil
}

func normalizeOpsUsers(ids []int64) ([]int64, error) {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请至少选择一个账号")
	}
	if len(out) > riskOpsMaxUsers {
		return nil, apperrors.New(40000, http.StatusBadRequest, fmt.Sprintf("一次最多处置 %d 个账号", riskOpsMaxUsers))
	}
	return out, nil
}

// RiskOpsInspect 一组账号的处置清单与关联账号
func (s *RiskService) RiskOpsInspect(ctx context.Context, appID int64, userIDs []int64, related bool) (*RiskOpsInspection, error) {
	store, _, err := s.ops()
	if err != nil {
		return nil, err
	}
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择应用")
	}
	ids, err := normalizeOpsUsers(userIDs)
	if err != nil {
		return nil, err
	}
	out := &RiskOpsInspection{AppID: appID, Subjects: []RiskOpsSubject{}, Related: []RiskOpsRelated{}, Errors: map[string]string{}}

	var risk struct {
		Days     int               `json:"days"`
		Subjects []riskSubject     `json:"subjects"`
		Related  []riskRelated     `json:"related"`
		Errors   map[string]string `json:"errors"`
	}
	if s.Enabled() {
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := s.adminDo(cctx, http.MethodPost, "subjects/inspect", map[string]any{"appId": appID, "userIds": ids, "related": related}, "aegis-server", &risk)
		cancel()
		if err != nil {
			out.Errors["risk"] = err.Error()
		}
		for k, v := range risk.Errors {
			out.Errors[k] = v
		}
	} else {
		out.Errors["risk"] = "风控平台未接入，只能处理封号与防火墙"
	}
	out.Days = risk.Days

	// 主库：账号基本情况与当前封禁（起点与关联账号一次查完）
	lookup := slices.Clone(ids)
	for _, r := range risk.Related {
		lookup = append(lookup, r.UserID)
	}
	users := map[int64]*userdomain.RiskOpsUser{}
	for i := 0; i < len(lookup); i += 500 {
		rows, err := store.RiskOpsUsers(ctx, appID, lookup[i:min(i+500, len(lookup))])
		if err != nil {
			return nil, err
		}
		for k := range rows {
			u := rows[k]
			users[u.ID] = &u
		}
	}

	bySubject := map[int64]riskSubject{}
	for _, sub := range risk.Subjects {
		bySubject[sub.UserID] = sub
	}
	// 防火墙：所有起点账号用过的 IP 一次查
	var ips []string
	for _, sub := range risk.Subjects {
		for _, e := range sub.Entities {
			if e.Kind == "ip" {
				if a, err := netip.ParseAddr(e.Value); err == nil {
					ips = append(ips, a.Unmap().String())
				}
			}
		}
	}
	fw := map[string][]firewalldomain.IPBan{}
	if len(ips) > 0 {
		bans, err := store.ActiveIPBansFor(ctx, ips)
		if err != nil {
			out.Errors["firewall"] = err.Error()
		}
		for _, b := range bans {
			fw[b.IP] = append(fw[b.IP], b)
		}
	}
	for _, id := range ids {
		sub := bySubject[id]
		item := RiskOpsSubject{UserID: id, User: users[id], GraphBanned: sub.Banned, GraphStatus: sub.Status,
			Lists: rawOr(sub.Lists, "[]"), Cases: rawOr(sub.Cases, "[]"), Recent: sub.Recent, Entities: sub.Entities}
		if item.Entities == nil {
			item.Entities = []RiskOpsEntity{}
		}
		for k := range item.Entities {
			e := &item.Entities[k]
			e.Lists = rawOr(e.Lists, "[]")
			if e.Kind == "ip" {
				if a, err := netip.ParseAddr(e.Value); err == nil {
					e.Firewall = fw[a.Unmap().String()]
				}
			}
		}
		out.Subjects = append(out.Subjects, item)
	}
	for _, r := range risk.Related {
		u := users[r.UserID]
		if u == nil {
			continue // 主库里已经删掉的账号不再列出
		}
		r.Reasons = rawOr(r.Reasons, "[]")
		out.Related = append(out.Related, RiskOpsRelated{riskRelated: r, User: u})
	}
	return out, nil
}

func rawOr(v json.RawMessage, def string) json.RawMessage {
	if len(v) == 0 || string(v) == "null" {
		return json.RawMessage(def)
	}
	return v
}

// RiskOpsReleaseInput 一键解除
type RiskOpsReleaseInput struct {
	AppID      int64    `json:"appId"`
	UserIDs    []int64  `json:"userIds"`
	BanIDs     []int64  `json:"banIds"`     // 要撤销的账号封禁
	IPBanIDs   []int64  `json:"ipBanIds"`   // 要撤销的防火墙封禁
	EntryIDs   []string `json:"entryIds"`   // 要移除的风控名单条目
	CloseCases bool     `json:"closeCases"` // 待复核单按「判定为正常」结案
	AllowDays  int      `json:"allowDays"`  // 解除后加多少天白名单保护（0 不加）
	Note       string   `json:"note"`
}

// RiskOpsUserResult 一个账号的执行结果
type RiskOpsUserResult struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Done   string `json:"done,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RiskOpsReleaseResult 一键解除的结果
type RiskOpsReleaseResult struct {
	Users        []RiskOpsUserResult `json:"users"`
	Unbanned     int                 `json:"unbanned"`
	IPUnbanned   int                 `json:"ipUnbanned"`
	ListsRemoved int                 `json:"listsRemoved"`
	CasesClosed  int                 `json:"casesClosed"`
	Allowed      int                 `json:"allowed"`
	// RiskCounters 风控平台侧删掉的滑窗计数、流式特征与图特征缓存键数
	RiskCounters int `json:"riskCounters"`
	// Counters 主服务侧清掉的注册准入计数与登录防爆破锁定
	Counters RiskOpsCountersResult `json:"counters"`
	Problems []string              `json:"problems"`
}

// RiskOpsRelease 一键解除风控
func (s *RiskService) RiskOpsRelease(ctx context.Context, op RiskOpsOperator, in RiskOpsReleaseInput) (*RiskOpsReleaseResult, error) {
	store, _, err := s.ops()
	if err != nil {
		return nil, err
	}
	ids, err := normalizeOpsUsers(in.UserIDs)
	if err != nil {
		return nil, err
	}
	if in.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择应用")
	}
	if in.AllowDays < 0 || in.AllowDays > 365 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "白名单保护期 0-365 天")
	}
	s.mu.RLock()
	bans, firewall := s.accountBans, s.firewall
	s.mu.RUnlock()
	note := strings.TrimSpace(in.Note)
	reason := "一键解除风控"
	if note != "" {
		reason += "：" + note
	}
	res := &RiskOpsReleaseResult{Users: []RiskOpsUserResult{}, Problems: []string{}}
	users, err := store.RiskOpsUsers(ctx, in.AppID, ids)
	if err != nil {
		return nil, err
	}
	known := map[int64]userdomain.RiskOpsUser{}
	for _, u := range users {
		known[u.ID] = u
	}
	operator := userdomain.BanOperator{AdminID: op.AdminID, AdminName: op.label()}
	var stillBanned []int64 // 执行完仍在封号中的（没勾选解封的人工封禁、解封失败的）
	for _, id := range ids {
		u, ok := known[id]
		r := RiskOpsUserResult{UserID: id, Name: opsName(u, id), OK: true}
		switch {
		case !ok:
			r.OK, r.Error = false, "账号不存在"
		case u.Ban != nil && slices.Contains(in.BanIDs, u.Ban.ID):
			if bans == nil {
				r.OK, r.Error = false, "封号服务未接入"
				break
			}
			if _, err := bans.RevokeBan(ctx, in.AppID, id, u.Ban.ID, userdomain.AccountBanRevokeInput{Reason: reason, Operator: operator}); err != nil {
				r.OK, r.Error = false, "解封失败："+err.Error()
				break
			}
			res.Unbanned++
			r.Done = "已解封"
			s.Report(ctx, riskevents.Event{Type: riskevents.TypeAccountUnban, AppID: in.AppID, UserID: id})
		}
		if ok && u.Ban != nil && r.Done != "已解封" {
			stillBanned = append(stillBanned, id)
		}
		res.Users = append(res.Users, r)
	}
	s.forgetScoreCache(ctx, in.AppID, ids)
	for _, banID := range uniqInt64(in.IPBanIDs) {
		if firewall == nil {
			res.Problems = append(res.Problems, "防火墙未接入，IP 封禁没有撤销")
			break
		}
		if err := firewall.UnbanIP(ctx, banID, op.AdminID); err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("防火墙封禁 #%d 没有撤销：%s", banID, err.Error()))
			continue
		}
		res.IPUnbanned++
	}
	var releaseIPs, releaseDevices []string
	if s.Enabled() {
		var out struct {
			ListsRemoved    int      `json:"listsRemoved"`
			CasesClosed     int      `json:"casesClosed"`
			Allowed         int      `json:"allowed"`
			CountersCleared int      `json:"countersCleared"`
			IPs             []string `json:"ips"`
			Devices         []string `json:"devices"`
			Problems        []string `json:"problems"`
		}
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err := s.adminDo(cctx, http.MethodPost, "subjects/release", map[string]any{
			"appId": in.AppID, "userIds": ids, "entryIds": nonNilStrings(in.EntryIDs), "stillBanned": uniqInt64(stillBanned),
			"closeCases": in.CloseCases, "allowDays": in.AllowDays, "note": note,
		}, op.label(), &out)
		cancel()
		if err != nil {
			res.Problems = append(res.Problems, "风控名单与复核单没有处理："+err.Error())
		} else {
			res.ListsRemoved, res.CasesClosed, res.Allowed, res.RiskCounters = out.ListsRemoved, out.CasesClosed, out.Allowed, out.CountersCleared
			res.Problems = append(res.Problems, out.Problems...)
			releaseIPs, releaseDevices = out.IPs, out.Devices
		}
	} else if len(in.EntryIDs) > 0 || in.CloseCases || in.AllowDays > 0 {
		res.Problems = append(res.Problems, "风控平台未接入，名单与复核单没有处理")
	}
	// 主服务侧计数：注册准入的窗口计数与惩罚期、登录防爆破锁定（登录名 + 风控记录到的地址与设备）
	s.mu.RLock()
	counters := s.counters
	s.mu.RUnlock()
	if counters != nil {
		accounts := make([]string, 0, len(known))
		for _, u := range known {
			accounts = append(accounts, u.Account)
		}
		res.Counters = counters.Release(ctx, in.AppID, ids, accounts, releaseIPs, releaseDevices, op.label())
	}
	s.log.Info("一键解除风控", zap.Int64("appid", in.AppID), zap.Int64s("users", ids), zap.String("operator", op.label()),
		zap.Int("unbanned", res.Unbanned), zap.Int("ipUnbanned", res.IPUnbanned), zap.Int("listsRemoved", res.ListsRemoved))
	return res, nil
}

// RiskOpsPurgeInput 关联删除
type RiskOpsPurgeInput struct {
	AppID        int64   `json:"appId"`
	UserIDs      []int64 `json:"userIds"`
	BlockDevices bool    `json:"blockDevices"` // 同时拉黑这些账号用过的设备
	BlockDays    int     `json:"blockDays"`    // 0 = 永久
	Note         string  `json:"note"`
	Confirm      string  `json:"confirm"` // 必须是「删除 N 个账号」
}

// RiskOpsPurgeResult 关联删除的结果
type RiskOpsPurgeResult struct {
	Users          []RiskOpsUserResult `json:"users"`
	Deleted        int                 `json:"deleted"`
	DevicesBlocked int                 `json:"devicesBlocked"`
	CasesClosed    int                 `json:"casesClosed"`
	GraphRemoved   int                 `json:"graphRemoved"` // 关系图里删掉的点（账号与只属于它们的登录名、手机号、设备等）
	RiskCounters   int                 `json:"riskCounters"` // 风控平台侧删掉的滑窗计数、流式特征与图特征缓存键数
	Problems       []string            `json:"problems"`
}

// RiskOpsConfirmText 删除前要求输入的确认文字
func RiskOpsConfirmText(n int) string { return fmt.Sprintf("删除 %d 个账号", n) }

// RiskOpsPurge 删除风险账号（可连同关联账号一起）
func (s *RiskService) RiskOpsPurge(ctx context.Context, op RiskOpsOperator, in RiskOpsPurgeInput) (*RiskOpsPurgeResult, error) {
	store, purger, err := s.ops()
	if err != nil {
		return nil, err
	}
	if purger == nil {
		return nil, errors.New("删号服务未接入")
	}
	ids, err := normalizeOpsUsers(in.UserIDs)
	if err != nil {
		return nil, err
	}
	if in.AppID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择应用")
	}
	if strings.TrimSpace(in.Confirm) != RiskOpsConfirmText(len(ids)) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入「"+RiskOpsConfirmText(len(ids))+"」确认删除")
	}
	if in.BlockDays < 0 || in.BlockDays > 3650 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "设备拉黑时长 0-3650 天")
	}
	note := strings.TrimSpace(in.Note)
	res := &RiskOpsPurgeResult{Users: []RiskOpsUserResult{}, Problems: []string{}}

	users, err := store.RiskOpsUsers(ctx, in.AppID, ids)
	if err != nil {
		return nil, err
	}
	known := map[int64]userdomain.RiskOpsUser{}
	for _, u := range users {
		known[u.ID] = u
	}
	// 删号之前先记下每个账号用过的设备（删掉之后风控侧仍有，但按账号查更准）
	devices := map[int64][]string{}
	if in.BlockDevices && s.Enabled() {
		var risk struct {
			Subjects []riskSubject `json:"subjects"`
		}
		cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		err := s.adminDo(cctx, http.MethodPost, "subjects/inspect", map[string]any{"appId": in.AppID, "userIds": ids}, "aegis-server", &risk)
		cancel()
		if err != nil {
			return nil, apperrors.New(50300, http.StatusServiceUnavailable, "读不到这些账号用过的设备，没有执行删除："+err.Error())
		}
		for _, sub := range risk.Subjects {
			for _, e := range sub.Entities {
				if e.Kind == "device" && e.Value != "" && riskevents.NormalizeDeviceID(e.Value) != "" {
					devices[sub.UserID] = append(devices[sub.UserID], e.Value)
				}
			}
		}
	}
	var deleted []int64
	var block []string
	for _, id := range ids {
		u, ok := known[id]
		r := RiskOpsUserResult{UserID: id, Name: opsName(u, id), OK: true}
		if !ok {
			r.OK, r.Error = false, "账号不存在或已删除"
			res.Users = append(res.Users, r)
			continue
		}
		if err := purger.PurgeUser(ctx, in.AppID, id); err != nil {
			r.OK, r.Error = false, err.Error()
			res.Users = append(res.Users, r)
			continue
		}
		r.Done = "已删除"
		res.Deleted++
		deleted = append(deleted, id)
		block = append(block, devices[id]...)
		res.Users = append(res.Users, r)
	}
	if len(deleted) > 0 && s.Enabled() {
		var out struct {
			DevicesBlocked int      `json:"devicesBlocked"`
			CasesClosed    int      `json:"casesClosed"`
			GraphRemoved   int      `json:"graphRemoved"`
			Counters       int      `json:"countersCleared"`
			Problems       []string `json:"problems"`
		}
		s.forgetScoreCache(ctx, in.AppID, deleted)
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err := s.adminDo(cctx, http.MethodPost, "subjects/forget", map[string]any{
			"appId": in.AppID, "userIds": deleted, "blockDevices": nonNilStrings(block), "blockDays": in.BlockDays, "note": note,
		}, op.label(), &out)
		cancel()
		if err != nil {
			res.Problems = append(res.Problems, "账号已删除，但风控侧没有收尾（设备拉黑、复核单结案）："+err.Error())
		} else {
			res.DevicesBlocked, res.CasesClosed, res.GraphRemoved, res.RiskCounters = out.DevicesBlocked, out.CasesClosed, out.GraphRemoved, out.Counters
			res.Problems = append(res.Problems, out.Problems...)
		}
	}
	// 主服务侧计数在每个账号的 PurgeUser 收尾里已经更新；排行榜缓存批量过程中节流，这里兜底再清一次
	s.mu.RLock()
	counters := s.counters
	s.mu.RUnlock()
	if counters != nil && len(deleted) > 0 {
		counters.AfterBatch(ctx, in.AppID)
	}
	s.log.Warn("关联删除风险账号", zap.Int64("appid", in.AppID), zap.Int64s("users", deleted), zap.String("operator", op.label()),
		zap.Int("devicesBlocked", res.DevicesBlocked), zap.String("note", note))
	return res, nil
}

// ── 风险账号发现：不需要先打开某个用户 ──

// RiskOpsCandidateQuery 按风控信号找账号
type RiskOpsCandidateQuery struct {
	AppID  int64
	Source string // banned / blocked / score / listed / confirmed / device
	Days   int
	Min    int
	Limit  int
	Query  string
	Page   int
}

// RiskOpsCandidate 一个风险账号与它被列出来的原因
type RiskOpsCandidate struct {
	User      userdomain.RiskOpsUser `json:"user"`
	Count     uint64                 `json:"count"`
	MaxScore  int32                  `json:"maxScore"`
	LastAt    *time.Time             `json:"lastAt,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
	Scene     string                 `json:"scene,omitempty"`
	ExpiresAt *time.Time             `json:"expiresAt,omitempty"`
}

// RiskOpsCluster 一台设备与登录过它的账号
type RiskOpsCluster struct {
	userdomain.RiskDeviceCluster
	Members []userdomain.RiskOpsUser `json:"members"`
}

// RiskOpsCandidates 发现结果
type RiskOpsCandidates struct {
	Source   string             `json:"source"`
	Total    int64              `json:"total"`
	Items    []RiskOpsCandidate `json:"items"`
	Clusters []RiskOpsCluster   `json:"clusters"`
}

// RiskOpsFind 按来源列出风险账号：风控封禁中、多次被拦截、高风险分、在黑名单里、复核确认有风险、设备聚集
func (s *RiskService) RiskOpsFind(ctx context.Context, q RiskOpsCandidateQuery) (*RiskOpsCandidates, error) {
	store, _, err := s.ops()
	if err != nil {
		return nil, err
	}
	out := &RiskOpsCandidates{Source: q.Source, Items: []RiskOpsCandidate{}, Clusters: []RiskOpsCluster{}}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	switch q.Source {
	case "", "banned":
		out.Source = "banned"
		users, total, err := store.RiskBannedUsers(ctx, userdomain.RiskBannedQuery{AppID: q.AppID, Keyword: q.Query, Page: q.Page, Limit: limit})
		if err != nil {
			return nil, err
		}
		out.Total = total
		for _, u := range users {
			c := RiskOpsCandidate{User: u, Count: 1}
			if u.Ban != nil {
				at := u.Ban.StartAt
				c.LastAt, c.Reason, c.ExpiresAt = &at, u.Ban.Reason, u.Ban.EndAt
			}
			out.Items = append(out.Items, c)
		}
		return out, nil
	case "device":
		minUsers := q.Min
		if minUsers <= 0 {
			minUsers = 3
		}
		clusters, err := store.RiskDeviceClusters(ctx, q.AppID, minUsers, min(limit, 100))
		if err != nil {
			return nil, err
		}
		byApp := map[int64][]int64{}
		for _, c := range clusters {
			byApp[c.AppID] = append(byApp[c.AppID], c.UserIDs...)
		}
		users, err := s.opsUsersByApp(ctx, store, byApp)
		if err != nil {
			return nil, err
		}
		for _, c := range clusters {
			item := RiskOpsCluster{RiskDeviceCluster: c, Members: []userdomain.RiskOpsUser{}}
			for _, id := range c.UserIDs {
				if u, ok := users[c.AppID][id]; ok {
					item.Members = append(item.Members, u)
				}
			}
			if len(item.Members) < 2 {
				continue
			}
			out.Clusters = append(out.Clusters, item)
		}
		out.Total = int64(len(out.Clusters))
		return out, nil
	case "blocked", "score", "listed", "confirmed":
		if !s.Enabled() {
			return nil, apperrors.New(50301, http.StatusServiceUnavailable, "风控平台未接入")
		}
		query := url.Values{"source": {q.Source}, "limit": {fmt.Sprint(limit)}}
		if q.AppID > 0 {
			query.Set("app", fmt.Sprint(q.AppID))
		}
		if q.Days > 0 {
			query.Set("days", fmt.Sprint(q.Days))
		}
		if q.Min > 0 {
			query.Set("min", fmt.Sprint(q.Min))
		}
		var risk struct {
			Items []struct {
				AppID     int64      `json:"appId"`
				UserID    int64      `json:"userId"`
				Count     uint64     `json:"count"`
				MaxScore  int32      `json:"maxScore"`
				LastAt    *time.Time `json:"lastAt"`
				Reason    string     `json:"reason"`
				Scene     string     `json:"scene"`
				ExpiresAt *time.Time `json:"expiresAt"`
			} `json:"items"`
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.adminDo(cctx, http.MethodGet, "subjects/candidates?"+query.Encode(), nil, "aegis-server", &risk)
		cancel()
		if err != nil {
			return nil, apperrors.New(50302, http.StatusBadGateway, "风控平台没有返回："+err.Error())
		}
		byApp := map[int64][]int64{}
		for _, it := range risk.Items {
			byApp[it.AppID] = append(byApp[it.AppID], it.UserID)
		}
		users, err := s.opsUsersByApp(ctx, store, byApp)
		if err != nil {
			return nil, err
		}
		kw := strings.ToLower(strings.TrimSpace(q.Query))
		for _, it := range risk.Items {
			u, ok := users[it.AppID][it.UserID]
			if !ok {
				continue // 账号已删除
			}
			if kw != "" && !strings.Contains(strings.ToLower(u.Account+" "+u.Nickname+" "+fmt.Sprint(u.ID)+" "+fmt.Sprint(u.PublicID)), kw) {
				continue
			}
			out.Items = append(out.Items, RiskOpsCandidate{User: u, Count: it.Count, MaxScore: it.MaxScore, LastAt: it.LastAt,
				Reason: it.Reason, Scene: it.Scene, ExpiresAt: it.ExpiresAt})
		}
		out.Total = int64(len(out.Items))
		return out, nil
	}
	return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的来源")
}

// opsUsersByApp 分应用批量取账号（每批 500）
func (s *RiskService) opsUsersByApp(ctx context.Context, store riskOpsStore, byApp map[int64][]int64) (map[int64]map[int64]userdomain.RiskOpsUser, error) {
	out := map[int64]map[int64]userdomain.RiskOpsUser{}
	for app, ids := range byApp {
		out[app] = map[int64]userdomain.RiskOpsUser{}
		for i := 0; i < len(ids); i += 500 {
			rows, err := store.RiskOpsUsers(ctx, app, ids[i:min(i+500, len(ids))])
			if err != nil {
				return nil, err
			}
			for _, u := range rows {
				out[app][u.ID] = u
			}
		}
	}
	return out, nil
}

// RiskOpsBanned 正被风控平台封禁中的账号
func (s *RiskService) RiskOpsBanned(ctx context.Context, q userdomain.RiskBannedQuery) ([]userdomain.RiskOpsUser, int64, error) {
	store, _, err := s.ops()
	if err != nil {
		return nil, 0, err
	}
	items, total, err := store.RiskBannedUsers(ctx, q)
	if items == nil {
		items = []userdomain.RiskOpsUser{}
	}
	return items, total, err
}

// forgetScoreCache 清掉这些账号在主服务里缓存的风险分（UserMaxScore 的 5 分钟缓存），下次按风控平台现状重读
func (s *RiskService) forgetScoreCache(ctx context.Context, appID int64, ids []int64) {
	if s.redis == nil || len(ids) == 0 {
		return
	}
	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, fmt.Sprintf("%srisk:maxscore:%d:%d:30", s.prefix, appID, id))
	}
	_ = s.redis.Del(ctx, keys...).Err()
}

func opsName(u userdomain.RiskOpsUser, id int64) string {
	if u.Nickname != "" {
		return u.Nickname
	}
	if u.Account != "" {
		return u.Account
	}
	return fmt.Sprintf("用户 #%d", id)
}

func uniqInt64(v []int64) []int64 {
	out := make([]int64, 0, len(v))
	for _, x := range v {
		if x > 0 && !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

var riskAdminOpsHTTP = &http.Client{Timeout: 120 * time.Second}

// adminDo 调 risk-admin /v1/<path>（带内部令牌与操作者），解开 {code, data} 信封
func (s *RiskService) adminDo(ctx context.Context, method, path string, body any, operator string, out any) error {
	if !s.Enabled() {
		return errors.New("风控平台未接入")
	}
	base, err := s.AdminBaseURL(ctx)
	if err != nil {
		return err
	}
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/v1/"+strings.TrimPrefix(path, "/"), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Risk-Token", s.cfg.InternalToken)
	req.Header.Set("X-Risk-Operator", url.PathEscape(operator))
	resp, err := riskAdminOpsHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("risk-admin %s 返回无法解析：%w", path, err)
	}
	if resp.StatusCode != http.StatusOK || env.Code != 200 {
		if env.Message == "" {
			env.Message = resp.Status
		}
		return fmt.Errorf("%s", env.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}
