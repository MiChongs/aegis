package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	realtimedomain "aegis/internal/domain/realtime"
	apperrors "aegis/pkg/errors"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// 在线统计长连接的关闭码（4000-4999 为应用自定义区间），控制台据此决定是否重连。
const (
	liveCloseHandshake    = 4400 // 握手失败或收到无法解密的帧：不重连，提示错误
	liveCloseUnauthorized = 4401 // 令牌失效 / 会话被撤销：回登录页
	liveCloseForbidden    = 4403 // 权限被收回
	liveCloseRotate       = 4408 // 连接达到最长寿命，换一组密钥：立即重连
)

const (
	liveWriteWait  = 10 * time.Second
	livePongWait   = 70 * time.Second
	livePingPeriod = 25 * time.Second
	liveSearchCap  = 5000
	liveAppsCap    = 500
)

// liveSubscriber 一条已完成密钥交换的管理端连接。
type liveSubscriber struct {
	hub       *OnlineLiveHub
	conn      *websocket.Conn
	channel   *liveChannel
	token     string
	send      chan []byte
	done      chan struct{}
	started   time.Time
	closeOnce sync.Once
	inflight  chan struct{}

	mu          sync.RWMutex
	adminID     int64
	account     string
	displayName string
	super       bool
	all         bool
	apps        map[int64]struct{}
	focus       int64
	feed        bool
	lastVersion int64
}

// liveRequest 客户端请求（解密后的 JSON）。id 原样回填到响应的 re 字段。
type liveRequest struct {
	Type  string `json:"t"`
	ID    string `json:"id,omitempty"`
	App   int64  `json:"app,omitempty"`
	Feed  *bool  `json:"feed,omitempty"`
	Range string `json:"range,omitempty"`
	Page  int    `json:"page,omitempty"`
	Limit int    `json:"limit,omitempty"`
	Query string `json:"q,omitempty"`
	TS    int64  `json:"ts,omitempty"`
}

type liveKeyFrame struct {
	Type string `json:"t"`
	Key  string `json:"key"`
}

type liveFocus struct {
	AppID       int64                     `json:"appid"`
	Name        string                    `json:"name,omitempty"`
	AppKey      string                    `json:"appKey,omitempty"`
	Users       int64                     `json:"users"`
	Connections int64                     `json:"connections"`
	Dims        realtimedomain.Dimensions `json:"dims"`
}

type liveSnapshotFrame struct {
	Type      string                            `json:"t"`
	Version   int64                             `json:"v"`
	At        time.Time                         `json:"at"`
	Totals    realtimedomain.OnlineTotals       `json:"totals"`
	Apps      []realtimedomain.AppOnlineSummary `json:"apps"`
	Dims      realtimedomain.Dimensions         `json:"dims"`
	Focus     *liveFocus                        `json:"focus,omitempty"`
	Nodes     []realtimedomain.NodeLoad         `json:"nodes,omitempty"`
	Admins    *realtimedomain.AdminOnlineTotals `json:"admins,omitempty"`
	Truncated bool                              `json:"truncated,omitempty"`
}

func (s *liveSubscriber) applyAccess(access *admindomain.AccessContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminID = access.AdminID
	s.account = access.Account
	s.displayName = access.DisplayName
	s.super = access.IsSuperAdmin
	s.all = access.IsSuperAdmin
	s.apps = make(map[int64]struct{})
	for _, a := range access.Assignments {
		if a.AppID == nil {
			s.all = true
			continue
		}
		s.apps[*a.AppID] = struct{}{}
	}
	if s.focus > 0 && !s.allowedLocked(s.focus) {
		s.focus = 0
	}
}

func (s *liveSubscriber) allowedLocked(appID int64) bool {
	if s.all {
		return true
	}
	_, ok := s.apps[appID]
	return ok
}

func (s *liveSubscriber) allowed(appID int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.allowedLocked(appID)
}

// handshake 明文发 hello、收 key，返回加密通道。整个过程限时 10 秒。
func (s *liveSubscriber) handshake() (*liveChannel, error) {
	hs, err := newLiveHandshake()
	if err != nil {
		return nil, err
	}
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := s.conn.WriteJSON(hs.hello()); err != nil {
		return nil, err
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	kind, payload, err := s.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if kind != websocket.TextMessage {
		return nil, stderrors.New("握手帧必须是文本帧")
	}
	var frame liveKeyFrame
	if err := json.Unmarshal(payload, &frame); err != nil || frame.Type != "key" {
		return nil, stderrors.New("握手帧格式错误")
	}
	channel, err := hs.complete(frame.Key, s.token)
	if err != nil {
		return nil, err
	}
	_ = s.conn.SetReadDeadline(time.Now().Add(livePongWait))
	return channel, nil
}

func (s *liveSubscriber) readyFrame() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	apps := make([]int64, 0, len(s.apps))
	for id := range s.apps {
		apps = append(apps, id)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i] < apps[j] })
	return map[string]any{
		"t":          "ready",
		"serverTime": time.Now().UTC(),
		"node":       s.hub.realtime.ServerID(),
		"admin": map[string]any{
			"id":           s.adminID,
			"account":      s.account,
			"displayName":  s.displayName,
			"isSuperAdmin": s.super,
		},
		"scope": map[string]any{"all": s.all, "apps": apps},
		"limits": map[string]any{
			"tickMs":        s.hub.tick.Milliseconds(),
			"retentionDays": int(s.hub.retention.Hours() / 24),
			"lifetimeSec":   int(s.hub.maxLifetime.Seconds()),
		},
	}
}

// ── 收发 ──

func (s *liveSubscriber) writePump() {
	ticker := time.NewTicker(livePingPeriod)
	defer func() {
		ticker.Stop()
		s.closeWith(websocket.CloseNormalClosure, "")
	}()
	for {
		select {
		case <-s.done:
			return
		case payload := <-s.send:
			_ = s.conn.SetWriteDeadline(time.Now().Add(liveWriteWait))
			if err := s.conn.WriteMessage(websocket.BinaryMessage, s.channel.seal(payload)); err != nil {
				return
			}
		case <-ticker.C:
			_ = s.conn.SetWriteDeadline(time.Now().Add(liveWriteWait))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// sendJSON 排队一帧；队列满时丢弃。快照与动态都会被下一帧覆盖，丢一帧无妨。
func (s *liveSubscriber) sendJSON(v any) bool {
	payload, err := json.Marshal(v)
	if err != nil {
		return false
	}
	select {
	case <-s.done:
		return false
	case s.send <- payload:
		return true
	default:
		return false
	}
}

// reply 请求的响应不能丢：队列满时最多等两秒，仍然发不出去说明客户端卡死，断开。
func (s *liveSubscriber) reply(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-s.done:
	case s.send <- payload:
	case <-timer.C:
		s.closeWith(websocket.CloseTryAgainLater, "client too slow")
	}
}

func (s *liveSubscriber) readPump() {
	defer func() {
		s.hub.unregister(s)
		s.closeWith(websocket.CloseNormalClosure, "")
	}()
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(livePongWait))
	})
	s.inflight = make(chan struct{}, 4)
	for {
		kind, payload, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.BinaryMessage {
			// 握手之后不接受任何明文
			s.closeWith(liveCloseHandshake, "plaintext rejected")
			return
		}
		plain, err := s.channel.open(payload)
		if err != nil {
			s.closeWith(liveCloseHandshake, "bad frame")
			return
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(livePongWait))
		var req liveRequest
		if err := json.Unmarshal(plain, &req); err != nil {
			continue
		}
		select {
		case s.inflight <- struct{}{}:
		case <-s.done:
			return
		}
		go func() {
			defer func() { <-s.inflight }()
			s.handle(req)
		}()
	}
}

// watch 定期复核令牌：会话被撤销、账号被停用或权限被收回时立即断开；
// 连接满最长寿命时要求客户端重连，换一组临时密钥。
func (s *liveSubscriber) watch() {
	ticker := time.NewTicker(s.hub.revalidate)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if time.Since(s.started) >= s.hub.maxLifetime {
				s.closeWith(liveCloseRotate, "rotate keys")
				return
			}
			if s.hub.admin == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			access, err := s.hub.admin.ValidateAccessToken(ctx, s.token)
			if err != nil {
				cancel()
				var appErr *apperrors.AppError
				if stderrors.As(err, &appErr) {
					s.closeWith(liveCloseUnauthorized, "session invalid")
					return
				}
				// 基础设施抖动（Redis / PG 超时）不踢人，下一轮再查
				continue
			}
			if err := s.hub.admin.Authorize(ctx, access, authz.PermSystemOnlineRead, nil); err != nil {
				cancel()
				s.closeWith(liveCloseForbidden, "permission revoked")
				return
			}
			cancel()
			s.applyAccess(access)
		}
	}
}

func (s *liveSubscriber) closeWith(code int, reason string) {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = s.conn.Close()
	})
}

// ── 推送 ──

func (s *liveSubscriber) pushSnapshot(snap *realtimedomain.OnlineSnapshot) {
	s.mu.Lock()
	if snap.Version <= s.lastVersion {
		s.mu.Unlock()
		return
	}
	s.lastVersion = snap.Version
	frame := s.scopeSnapshotLocked(snap)
	s.mu.Unlock()
	s.sendJSON(frame)
}

// forceSnapshot 订阅范围变了：即使版本没变也立刻按新范围下发一份。
func (s *liveSubscriber) forceSnapshot() {
	snap := s.hub.snapshot.Load()
	if snap == nil {
		return
	}
	s.mu.Lock()
	s.lastVersion = snap.Version
	frame := s.scopeSnapshotLocked(snap)
	s.mu.Unlock()
	s.sendJSON(frame)
}

func (s *liveSubscriber) scopeSnapshotLocked(snap *realtimedomain.OnlineSnapshot) liveSnapshotFrame {
	frame := liveSnapshotFrame{
		Type:      "snapshot",
		Version:   snap.Version,
		At:        snap.At,
		Truncated: snap.Truncated,
	}
	if s.all {
		frame.Totals = snap.Totals
		frame.Apps = capApps(snap.Apps)
		frame.Dims = snap.Dims
	} else {
		parts := make([]realtimedomain.Dimensions, 0, len(s.apps))
		for _, app := range snap.Apps {
			if _, ok := s.apps[app.AppID]; !ok {
				continue
			}
			frame.Apps = append(frame.Apps, app)
			frame.Totals.Users += app.Users
			frame.Totals.Connections += app.Connections
			frame.Totals.Apps++
			if d, ok := snap.AppDims[app.AppID]; ok {
				parts = append(parts, d)
			}
		}
		frame.Apps = capApps(frame.Apps)
		frame.Dims = mergeDimensions(parts, 12)
	}
	if frame.Apps == nil {
		frame.Apps = []realtimedomain.AppOnlineSummary{}
	}
	if s.focus > 0 && s.allowedLocked(s.focus) {
		focus := &liveFocus{AppID: s.focus, Dims: snap.AppDims[s.focus]}
		for _, app := range snap.Apps {
			if app.AppID == s.focus {
				focus.Users, focus.Connections = app.Users, app.Connections
				focus.Name, focus.AppKey = app.Name, app.AppKey
				break
			}
		}
		frame.Focus = focus
	}
	if s.super {
		frame.Nodes = snap.Nodes
		frame.Admins = snap.Admins
	}
	return frame
}

func capApps(apps []realtimedomain.AppOnlineSummary) []realtimedomain.AppOnlineSummary {
	if len(apps) > liveAppsCap {
		return apps[:liveAppsCap]
	}
	return apps
}

func (s *liveSubscriber) pushFeed(batch []realtimedomain.PresenceEvent, dropped int64) {
	s.mu.RLock()
	if !s.feed {
		s.mu.RUnlock()
		return
	}
	events := make([]realtimedomain.PresenceEvent, 0, len(batch))
	for _, evt := range batch {
		if evt.Admin {
			if s.super && s.focus == 0 {
				events = append(events, evt)
			}
			continue
		}
		if !s.allowedLocked(evt.AppID) || (s.focus > 0 && evt.AppID != s.focus) {
			continue
		}
		events = append(events, evt)
	}
	all := s.all
	s.mu.RUnlock()
	if !all {
		dropped = 0
	}
	if len(events) == 0 && dropped == 0 {
		return
	}
	s.sendJSON(map[string]any{"t": "feed", "events": events, "dropped": dropped})
}

// ── 请求处理 ──

func (s *liveSubscriber) handle(req liveRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	switch req.Type {
	case "ping":
		s.reply(map[string]any{"t": "pong", "re": req.ID, "ts": req.TS, "server": time.Now().UnixMilli()})
	case "sub":
		s.handleSubscribe(req)
	case "series":
		s.handleSeries(ctx, req)
	case "users":
		s.handleUsers(ctx, req)
	case "admins":
		s.handleAdmins(ctx, req)
	default:
		s.replyError(req, 40000, "不支持的请求类型")
	}
}

func (s *liveSubscriber) replyError(req liveRequest, code int, message string) {
	s.reply(map[string]any{"t": "error", "re": req.ID, "code": code, "message": message, "req": req.Type})
}

func (s *liveSubscriber) handleSubscribe(req liveRequest) {
	if req.App > 0 && !s.allowed(req.App) {
		s.replyError(req, 40311, "无权查看该应用的在线统计")
		return
	}
	s.mu.Lock()
	s.focus = req.App
	if req.Feed != nil {
		s.feed = *req.Feed
	}
	s.mu.Unlock()
	s.reply(map[string]any{"t": "sub", "re": req.ID, "app": req.App})
	s.forceSnapshot()
}

// liveRanges 曲线范围 → （时长，步长）。步长内取峰值，曲线不会把短时高峰抹平。
var liveRanges = map[string][2]time.Duration{
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, time.Minute},
	"24h": {24 * time.Hour, 5 * time.Minute},
	"7d":  {7 * 24 * time.Hour, 30 * time.Minute},
}

func (s *liveSubscriber) handleSeries(ctx context.Context, req liveRequest) {
	window, ok := liveRanges[req.Range]
	if !ok {
		req.Range = "24h"
		window = liveRanges["24h"]
	}
	if req.App > 0 && !s.allowed(req.App) {
		s.replyError(req, 40311, "无权查看该应用的在线统计")
		return
	}
	to := time.Now().UTC()
	from := to.Add(-window[0])
	global, err := s.hub.repo.ReadSeries(ctx, 0, from, to)
	if err != nil {
		s.replyError(req, 50000, "读取在线曲线失败")
		return
	}
	s.mu.RLock()
	all, super := s.all, s.super
	apps := make([]int64, 0, len(s.apps))
	for id := range s.apps {
		apps = append(apps, id)
	}
	s.mu.RUnlock()

	// 全站曲线的时间点就是「当时有采样」的时间点；应用曲线按这些点对齐，
	// 某分钟没有该应用的记录就是那一分钟它没人在线，记 0 而不是断线。
	var sources []int64
	switch {
	case req.App > 0:
		sources = []int64{req.App}
	case !all:
		sources = apps
		if len(sources) > 50 {
			sources = sources[:50]
		}
	}
	points := global
	if sources != nil {
		sum := make(map[int64][2]int64)
		for _, appID := range sources {
			series, err := s.hub.repo.ReadSeries(ctx, appID, from, to)
			if err != nil {
				s.replyError(req, 50000, "读取在线曲线失败")
				return
			}
			for _, p := range series {
				v := sum[p.At]
				sum[p.At] = [2]int64{v[0] + p.Users, v[1] + p.Connections}
			}
		}
		points = make([]realtimedomain.SeriesPoint, 0, len(global))
		for _, g := range global {
			v := sum[g.At]
			points = append(points, realtimedomain.SeriesPoint{At: g.At, Users: v[0], Connections: v[1]})
		}
	}
	showAdmins := super && req.App == 0
	step := int64(window[1].Seconds())
	out := make([][4]int64, 0, len(points))
	for _, p := range points {
		bucket := p.At - p.At%step
		admins := int64(0)
		if showAdmins {
			admins = p.Admins
		}
		if n := len(out); n > 0 && out[n-1][0] == bucket {
			out[n-1][1] = max(out[n-1][1], p.Users)
			out[n-1][2] = max(out[n-1][2], p.Connections)
			out[n-1][3] = max(out[n-1][3], admins)
			continue
		}
		out = append(out, [4]int64{bucket, p.Users, p.Connections, admins})
	}
	s.reply(map[string]any{
		"t": "series", "re": req.ID, "app": req.App, "range": req.Range,
		"step": step, "from": from.Unix(), "to": to.Unix(), "points": out,
	})
}

func (s *liveSubscriber) handleUsers(ctx context.Context, req liveRequest) {
	if req.App <= 0 {
		s.replyError(req, 40000, "缺少应用")
		return
	}
	if !s.allowed(req.App) {
		s.replyError(req, 40311, "无权查看该应用的在线用户")
		return
	}
	page, limit := max(req.Page, 1), req.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	repo := s.hub.repo
	keyword := strings.TrimSpace(req.Query)
	var list *realtimedomain.AppOnlineUserList
	if keyword == "" {
		result, err := repo.ListAppOnlineUsers(ctx, req.App, page, limit)
		if err != nil {
			s.replyError(req, 50000, "读取在线用户失败")
			return
		}
		list = result
	} else {
		matched, err := s.searchOnlineUsers(ctx, req.App, keyword)
		if err != nil {
			s.replyError(req, 50000, "搜索在线用户失败")
			return
		}
		total := int64(len(matched))
		start := min((page-1)*limit, len(matched))
		end := min(start+limit, len(matched))
		items, err := repo.BuildAppOnlineUsers(ctx, req.App, matched[start:end])
		if err != nil {
			s.replyError(req, 50000, "读取在线用户失败")
			return
		}
		list = &realtimedomain.AppOnlineUserList{
			AppID: req.App, Page: page, Limit: limit, Total: total,
			TotalPages: max(1, int((total+int64(limit)-1)/int64(limit))),
			Items:      items, RefreshedAt: time.Now().UTC(),
		}
	}
	if err := s.hub.realtime.FillOnlineUserIdentities(ctx, req.App, list.Items); err != nil {
		s.hub.log.Debug("fill online identities failed", zap.Error(err))
	}
	if list.Items == nil {
		list.Items = []realtimedomain.AppOnlineUser{}
	}
	s.reply(map[string]any{"t": "users", "re": req.ID, "q": keyword, "list": list})
}

// searchOnlineUsers 在全部在线用户（最多 liveSearchCap 个）里按账号、昵称、
// 用户 ID、IP 前缀或设备 ID 找人，保持最近心跳优先的顺序。
func (s *liveSubscriber) searchOnlineUsers(ctx context.Context, appID int64, keyword string) ([]int64, error) {
	repo := s.hub.repo
	ids, err := repo.ListAppOnlineUserIDs(ctx, appID, liveSearchCap)
	if err != nil {
		return nil, err
	}
	hit := make(map[int64]struct{})
	if s.hub.pg != nil {
		found, err := s.hub.pg.MatchOnlineUsers(ctx, appID, ids, keyword)
		if err != nil {
			return nil, err
		}
		hit = found
	}
	// IP 与设备 ID 只在连接元数据里：关键字像地址或标识时才去扫
	if looksLikeAddressOrID(keyword) {
		lower := strings.ToLower(keyword)
		_, err := repo.ScanAppConnections(ctx, appID, liveSearchCap, func(conn realtimedomain.PresenceConnection) {
			if strings.HasPrefix(conn.IP, keyword) || (conn.DeviceID != "" && strings.Contains(strings.ToLower(conn.DeviceID), lower)) {
				hit[conn.UserID] = struct{}{}
			}
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]int64, 0, len(hit))
	for _, id := range ids {
		if _, ok := hit[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func looksLikeAddressOrID(keyword string) bool {
	if net.ParseIP(keyword) != nil || strings.Count(keyword, ".") >= 1 || strings.Contains(keyword, ":") {
		return true
	}
	return len(keyword) >= 6 && strings.Trim(strings.ToLower(keyword), "0123456789abcdef-") == ""
}

func (s *liveSubscriber) handleAdmins(ctx context.Context, req liveRequest) {
	s.mu.RLock()
	super := s.super
	s.mu.RUnlock()
	if !super {
		s.replyError(req, 40311, "需要超级管理员权限")
		return
	}
	items, err := s.hub.repo.ListAdminPresence(ctx)
	if err != nil {
		s.replyError(req, 50000, "读取在线管理员失败")
		return
	}
	out := make([]realtimedomain.AdminPresence, 0, len(items))
	for _, item := range items {
		row := realtimedomain.AdminPresence{AdminID: item.AdminID, Connections: item.Connections}
		for _, conn := range item.Connections {
			if row.LastSeenAt.Before(conn.LastSeenAt) {
				row.LastSeenAt = conn.LastSeenAt
			}
			if row.ConnectedAt.IsZero() || conn.ConnectedAt.Before(row.ConnectedAt) {
				row.ConnectedAt = conn.ConnectedAt
			}
		}
		if s.hub.pg != nil {
			if profile, err := s.hub.pg.GetAdminAccessByID(ctx, item.AdminID); err == nil && profile != nil {
				row.Account = profile.Account.Account
				row.DisplayName = profile.Account.DisplayName
				row.Avatar = profile.Account.Avatar
				row.SuperAdmin = profile.Account.IsSuperAdmin
			}
		}
		out = append(out, row)
	}
	s.reply(map[string]any{"t": "admins", "re": req.ID, "items": out})
}
