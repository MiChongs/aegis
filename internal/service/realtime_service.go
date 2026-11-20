package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	realtimedomain "aegis/internal/domain/realtime"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type UserEventPublisher interface {
	PublishUserEvent(ctx context.Context, appID int64, userID int64, eventType string, data map[string]any) error
}

// AppEventPublisher 向某个应用下的全部在线连接广播事件（不区分用户）。
type AppEventPublisher interface {
	PublishAppEvent(ctx context.Context, appID int64, eventType string, data map[string]any) error
}

// RealtimeSubprotocol 是服务端唯一会协商并回显的 WebSocket 子协议。
//
// 客户端握手时须提供两个子协议：
//
//	["aegis", "aegis.jwt.<accessToken>"]
//
// 前者用于协商回显（浏览器强制要求响应带 Sec-WebSocket-Protocol），
// 后者用于携带令牌（浏览器 WebSocket API 无法自定义请求头）。
// 服务端只回显前者，令牌永不进入响应头。
const RealtimeSubprotocol = "aegis"

type RealtimeService struct {
	log         *zap.Logger
	auth        *AuthService
	admin       *AdminService
	repository  *redisrepo.RealtimeRepository
	identities  *pgrepo.Repository
	location    *LocationService
	natsConn    *nats.Conn
	serverID    string
	upgrader    websocket.Upgrader
	presenceTTL time.Duration
	pingPeriod  time.Duration
	pongWait    time.Duration
	writeWait   time.Duration
	sendBuffer  int

	mu      sync.RWMutex
	clients map[int64]map[int64]map[string]*realtimeClient
	subs    []*nats.Subscription
	stopCh  chan struct{}

	listenerMu        sync.RWMutex
	presenceListeners []func(realtimedomain.PresenceEvent)
}

type realtimeClient struct {
	service      *RealtimeService
	connectionID string
	session      *authdomain.Session
	ip           string
	userAgent    string
	connectedAt  time.Time
	conn         *websocket.Conn
	send         chan []byte
	// presence 建连时组装好的在线记录（含设备与地区），上下线事件也从它取字段
	presence  realtimedomain.PresenceConnection
	closeOnce sync.Once
	// internalID / publicID 下发前把帧里的主键换成对外 ID（docs/user-public-id.md）；publicID 为空时原样发
	internalID string
	publicID   string
	// accruedAt 在线时长已经记到哪一刻（只在读泵 goroutine 里读写）
	accruedAt time.Time
}

// onlineAccrualInterval 在线时长多久落一次库：心跳时满这个间隔就把这段记进 user_online_daily，
// 断开时再记最后一段。实例崩溃时最多丢一个间隔。
const onlineAccrualInterval = 5 * time.Minute

// 实时连接被服务端主动断开时的关闭码（4000-4999 为应用自定义区间）。
const (
	realtimeCloseSessionRevoked = 4001
)

func NewRealtimeService(log *zap.Logger, auth *AuthService, repository *redisrepo.RealtimeRepository, natsConn *nats.Conn, allowedOrigins []string) (*RealtimeService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	service := &RealtimeService{
		log:        log,
		auth:       auth,
		repository: repository,
		natsConn:   natsConn,
		serverID:   uuid.NewString(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  2048,
			WriteBufferSize: 2048,
			CheckOrigin:     WebSocketOriginChecker(allowedOrigins),
			// 必须声明并回显子协议，否则浏览器会直接判握手失败：
			// Chromium 的要求是「客户端请求了子协议，响应就必须带 Sec-WebSocket-Protocol」，
			// 缺这个头会以 close code 1006 断开，且不产生任何服务端错误日志 —— 极难排查。
			// （curl 不做这项校验，所以命令行测试一直是 101 成功，掩盖了问题。）
			//
			// 这里只声明 "aegis"：客户端第二个子协议是 `aegis.jwt.<token>`（浏览器无法自定义
			// 请求头，只能借子协议携带令牌），**绝不能把它回显**，否则 JWT 会进到响应头，
			// 进而落入反代 / 网关的访问日志。
			Subprotocols: []string{RealtimeSubprotocol},
		},
		presenceTTL: 90 * time.Second,
		pingPeriod:  25 * time.Second,
		pongWait:    70 * time.Second,
		writeWait:   10 * time.Second,
		sendBuffer:  128,
		clients:     make(map[int64]map[int64]map[string]*realtimeClient),
		stopCh:      make(chan struct{}),
	}
	if err := service.subscribe(); err != nil {
		return nil, err
	}
	go service.presenceJanitor()
	return service, nil
}

// SetAdminService 延迟注入 AdminService（避免循环依赖）
func (s *RealtimeService) SetAdminService(admin *AdminService) {
	s.admin = admin
}

func (s *RealtimeService) AuthenticateRequest(ctx context.Context, req *http.Request) (*authdomain.Session, string, error) {
	if s == nil || s.auth == nil {
		return nil, "", apperrors.New(50300, http.StatusServiceUnavailable, "实时服务暂不可用")
	}
	token := extractRealtimeToken(req)
	if token == "" {
		return nil, "", apperrors.New(40100, http.StatusUnauthorized, "访问请求未获授权")
	}
	// 优先尝试用户 token
	session, err := s.auth.ValidateAccessToken(ctx, token)
	if err == nil && session != nil {
		return session, token, nil
	}
	// 用户 token 失败 → 尝试管理员 token（appID=0, userID=adminID）
	if s.admin != nil {
		access, adminErr := s.admin.ValidateAccessToken(ctx, token)
		if adminErr == nil && access != nil {
			return &authdomain.Session{
				AppID:   0, // 管理员连接标识
				UserID:  access.AdminID,
				Account: access.Account,
				TokenID: access.TokenID,
			}, token, nil
		}
	}
	return nil, "", apperrors.New(40100, http.StatusUnauthorized, "访问请求未获授权")
}

func (s *RealtimeService) PublishUserEvent(ctx context.Context, appID int64, userID int64, eventType string, data map[string]any) error {
	if s == nil {
		return nil
	}
	payload, err := json.Marshal(realtimedomain.Event{
		ID:        uuid.NewString(),
		Type:      strings.TrimSpace(eventType),
		AppID:     appID,
		UserID:    userID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
	if err != nil {
		return err
	}
	if s.natsConn != nil && s.natsConn.IsConnected() {
		return s.natsConn.Publish(event.SubjectRealtimeUser(appID, userID), payload)
	}
	s.dispatchLocal(appID, userID, payload)
	return nil
}

// PublishAppEvent 应用级广播：信封与用户事件相同，userId 为 0。
func (s *RealtimeService) PublishAppEvent(ctx context.Context, appID int64, eventType string, data map[string]any) error {
	if s == nil {
		return nil
	}
	payload, err := json.Marshal(realtimedomain.Event{
		ID:        uuid.NewString(),
		Type:      strings.TrimSpace(eventType),
		AppID:     appID,
		Timestamp: time.Now().UTC(),
		Data:      data,
	})
	if err != nil {
		return err
	}
	if s.natsConn != nil && s.natsConn.IsConnected() {
		return s.natsConn.Publish(event.SubjectRealtimeApp(appID), payload)
	}
	s.dispatchApp(appID, payload)
	return nil
}

// FilterOnlineUsers 给定的用户里当前在线的那些（onlineUserFilter）。实时服务不可用时原样返回，宁可多发。
func (s *RealtimeService) FilterOnlineUsers(ctx context.Context, appID int64, userIDs []int64) ([]int64, error) {
	if s == nil || s.repository == nil {
		return userIDs, nil
	}
	return s.repository.FilterOnlineUserIDs(ctx, appID, userIDs)
}

func (s *RealtimeService) OnlineStats(ctx context.Context) (*realtimedomain.OnlineStats, error) {
	if s == nil || s.repository == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "实时服务暂不可用")
	}
	return s.repository.OnlineStats(ctx)
}

func (s *RealtimeService) AppOnlineStats(ctx context.Context, appID int64) (*realtimedomain.AppOnlineStats, error) {
	if s == nil || s.repository == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "实时服务暂不可用")
	}
	return s.repository.AppOnlineStats(ctx, appID)
}

// SetIdentityRepository 注入用于回查账号名的 Postgres 仓储。
//
// 走 setter 而不是构造参数：presence 本身完全不依赖数据库，只有管理端那张表
// 需要把 userId 翻成人名。没注入时列表照常返回，只是没有账号名。
func (s *RealtimeService) SetIdentityRepository(pg *pgrepo.Repository) {
	if s == nil {
		return
	}
	s.identities = pg
}

// SetLocationService 注入 IP 归属地解析：在线记录里的国家 / 省份 / 城市
// 在建连时取一次，只读本地 IP 库与缓存，不为建连发远程请求。
func (s *RealtimeService) SetLocationService(location *LocationService) {
	if s == nil {
		return
	}
	s.location = location
}

// ServerID 本实例的接入节点标识。
func (s *RealtimeService) ServerID() string {
	if s == nil {
		return ""
	}
	return s.serverID
}

// Repository 在线记录仓储，供在线统计推送读取。
func (s *RealtimeService) Repository() *redisrepo.RealtimeRepository {
	if s == nil {
		return nil
	}
	return s.repository
}

// OnPresence 订阅全集群的上线 / 下线事件（本实例与其他实例产生的都会收到）。
func (s *RealtimeService) OnPresence(listener func(realtimedomain.PresenceEvent)) {
	if s == nil || listener == nil {
		return
	}
	s.listenerMu.Lock()
	s.presenceListeners = append(s.presenceListeners, listener)
	s.listenerMu.Unlock()
}

// DisconnectUser 断开某用户在某应用下的全部实时连接（所有实例）。
//
// 撤销会话只让令牌失效，已经建立的 WebSocket 不会因此断开 —— 旧实现里被
// 强制下线的用户会一直挂在在线列表上，直到心跳超时。appID 为 0 时断开的是管理员。
func (s *RealtimeService) DisconnectUser(ctx context.Context, appID int64, userID int64) {
	if s == nil {
		return
	}
	payload, err := json.Marshal(realtimeDisconnect{AppID: appID, UserID: userID})
	if err != nil {
		return
	}
	if s.natsConn != nil && s.natsConn.IsConnected() {
		if err := s.natsConn.Publish(event.SubjectRealtimeDisconnect, payload); err == nil {
			return
		}
	}
	s.disconnectLocal(appID, userID)
	_ = ctx
}

type realtimeDisconnect struct {
	AppID  int64 `json:"appid"`
	UserID int64 `json:"userId"`
}

func (s *RealtimeService) disconnectLocal(appID int64, userID int64) {
	s.mu.RLock()
	var targets []*realtimeClient
	for _, client := range s.clients[appID][userID] {
		targets = append(targets, client)
	}
	s.mu.RUnlock()
	for _, client := range targets {
		client.closeWith(realtimeCloseSessionRevoked, "session revoked")
	}
}

func (s *RealtimeService) publishPresence(evt realtimedomain.PresenceEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if s.natsConn != nil && s.natsConn.IsConnected() {
		if err := s.natsConn.Publish(event.SubjectRealtimePresence, payload); err == nil {
			return
		}
	}
	s.emitPresence(evt)
}

func (s *RealtimeService) emitPresence(evt realtimedomain.PresenceEvent) {
	s.listenerMu.RLock()
	listeners := s.presenceListeners
	s.listenerMu.RUnlock()
	for _, listener := range listeners {
		listener(evt)
	}
}

func presenceEvent(kind string, conn realtimedomain.PresenceConnection, at time.Time) realtimedomain.PresenceEvent {
	evt := realtimedomain.PresenceEvent{
		Kind:         kind,
		AppID:        conn.AppID,
		UserID:       conn.UserID,
		Account:      conn.Account,
		ConnectionID: conn.ConnectionID,
		IP:           conn.IP,
		Location:     conn.Location(),
		Platform:     conn.Platform,
		DeviceName:   conn.DeviceName,
		ServerID:     conn.ServerID,
		At:           at.UTC(),
		Admin:        conn.AppID == 0,
	}
	if kind == "offline" && !conn.ConnectedAt.IsZero() {
		evt.Duration = int64(at.Sub(conn.ConnectedAt).Seconds())
	}
	return evt
}

// FillOnlineUserIdentities 给在线用户补上账号与昵称，原地修改。
func (s *RealtimeService) FillOnlineUserIdentities(ctx context.Context, appID int64, items []realtimedomain.AppOnlineUser) error {
	if s == nil || s.identities == nil || len(items) == 0 {
		return nil
	}
	return s.identities.FillOnlineUserIdentities(ctx, appID, items)
}

func (s *RealtimeService) ListAppOnlineUsers(ctx context.Context, appID int64, page int, limit int) (*realtimedomain.AppOnlineUserList, error) {
	if s == nil || s.repository == nil {
		return nil, apperrors.New(50300, http.StatusServiceUnavailable, "实时服务暂不可用")
	}
	return s.repository.ListAppOnlineUsers(ctx, appID, page, limit)
}

func (s *RealtimeService) Close(ctx context.Context) {
	if s == nil {
		return
	}
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, appClients := range s.clients {
		for _, userClients := range appClients {
			for _, client := range userClients {
				_ = client.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				_ = client.conn.Close()
			}
		}
	}
	s.clients = make(map[int64]map[int64]map[string]*realtimeClient)
	_ = ctx
}

func (s *RealtimeService) subscribe() error {
	if s.natsConn == nil {
		return nil
	}
	handlers := []struct {
		subject string
		handle  func(*nats.Msg)
	}{
		// 用户级事件
		{event.SubjectRealtimeUserPrefix + ".*.*", func(msg *nats.Msg) {
			appID, userID, ok := event.MatchRealtimeUserSubject(msg.Subject)
			if !ok {
				return
			}
			s.dispatchLocal(appID, userID, msg.Data)
		}},
		// 应用级广播事件（应用公告变更等），转发给该应用下的所有连接
		{event.SubjectRealtimeAppPrefix + ".*", func(msg *nats.Msg) {
			appID, ok := event.MatchRealtimeAppSubject(msg.Subject)
			if !ok {
				return
			}
			s.dispatchApp(appID, msg.Data)
		}},
		// 全局广播事件（系统公告等），转发给所有已连接客户端
		{event.SubjectSystemAnnouncement, func(msg *nats.Msg) {
			s.broadcastAll(msg.Data)
		}},
		// 上线 / 下线事件：交给在线统计推送
		{event.SubjectRealtimePresence, func(msg *nats.Msg) {
			var evt realtimedomain.PresenceEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				return
			}
			s.emitPresence(evt)
		}},
		// 强制下线：断开本实例上该用户的连接
		{event.SubjectRealtimeDisconnect, func(msg *nats.Msg) {
			var cmd realtimeDisconnect
			if err := json.Unmarshal(msg.Data, &cmd); err != nil {
				return
			}
			s.disconnectLocal(cmd.AppID, cmd.UserID)
		}},
	}
	for _, h := range handlers {
		sub, err := s.natsConn.Subscribe(h.subject, h.handle)
		if err != nil {
			return err
		}
		s.subs = append(s.subs, sub)
	}
	return nil
}

// broadcastAll 向所有已连接的 WebSocket 客户端广播消息
func (s *RealtimeService) broadcastAll(payload []byte) {
	s.mu.RLock()
	var targets []*realtimeClient
	for _, appClients := range s.clients {
		for _, userClients := range appClients {
			for _, c := range userClients {
				targets = append(targets, c)
			}
		}
	}
	s.mu.RUnlock()
	for _, c := range targets {
		select {
		case c.send <- append([]byte(nil), payload...):
		default:
		}
	}
}

func (s *RealtimeService) Upgrade(w http.ResponseWriter, req *http.Request, session *authdomain.Session, ip string, userAgent string) error {
	conn, err := s.upgrader.Upgrade(w, req, nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	client := &realtimeClient{
		service:      s,
		connectionID: uuid.NewString(),
		session:      session,
		ip:           ip,
		userAgent:    userAgent,
		connectedAt:  now,
		accruedAt:    now,
		conn:         conn,
		send:         make(chan []byte, s.sendBuffer),
	}
	if publicID := s.auth.PublicIDs().Of(req.Context(), session.UserID); publicID > 0 {
		client.internalID = strconv.FormatInt(session.UserID, 10)
		client.publicID = strconv.FormatInt(publicID, 10)
	}
	client.presence = s.buildPresence(client, now)
	if err := s.touchPresence(req.Context(), client, now); err != nil {
		_ = conn.Close()
		return err
	}
	s.registerLocal(client)
	s.publishPresence(presenceEvent("online", client.presence, now))
	client.enqueue(realtimedomain.Event{
		ID:        uuid.NewString(),
		Type:      "system.welcome",
		AppID:     session.AppID,
		UserID:    session.UserID,
		Timestamp: now,
		Data: map[string]any{
			"connectionId": client.connectionID,
			"serverTime":   now,
			"presenceTtl":  int(s.presenceTTL.Seconds()),
		},
	})
	go client.writePump()
	go client.readPump()
	return nil
}

// buildPresence 组装一条连接的在线记录：身份与设备取自会话，地区取自 IP 库。
func (s *RealtimeService) buildPresence(client *realtimeClient, at time.Time) realtimedomain.PresenceConnection {
	session := client.session
	conn := realtimedomain.PresenceConnection{
		ConnectionID: client.connectionID,
		AppID:        session.AppID,
		UserID:       session.UserID,
		Account:      session.Account,
		TokenID:      session.TokenID,
		DeviceID:     session.DeviceID,
		DeviceName:   session.Device,
		DeviceModel:  session.DeviceModel,
		Platform:     session.DevicePlatform,
		IP:           client.ip,
		UserAgent:    client.userAgent,
		ConnectedAt:  client.connectedAt,
		LastSeenAt:   at.UTC(),
		ServerID:     s.serverID,
	}
	if extra := session.DeviceExtra; extra != nil {
		conn.OS = extra.OS
		conn.OSVersion = extra.OSVersion
		conn.AppVersion = extra.AppVersion
	}
	if strings.TrimSpace(conn.UserAgent) == "" && client.conn != nil {
		conn.UserAgent = client.conn.RemoteAddr().String()
	}
	if conn.Platform == "" {
		conn.Platform = platformFromUserAgent(conn.UserAgent)
	}
	if conn.OS == "" {
		conn.OS = conn.Platform
	}
	if s.location != nil && conn.IP != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		if loc, ok := s.location.GetCached(ctx, conn.IP); ok {
			conn.Country, conn.Region, conn.City, conn.ISP = loc.Country, loc.Region, loc.City, loc.ISP
			if loc.IsPrivate && conn.Country == "" {
				conn.Country = "内网"
			}
		} else {
			// 缓存与本地库都没有：后台补查一次，下次建连就有了
			s.location.RefreshAsync(conn.IP)
		}
		cancel()
	}
	return conn
}

// platformFromUserAgent 会话没有上报平台时，从 UA 粗分一个：控制台的浏览器、
// 旧版 SDK 都属于这种情况。只分大类，细节交给前端解析 UA。
func platformFromUserAgent(ua string) string {
	lower := strings.ToLower(ua)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "harmonyos") || strings.Contains(lower, "openharmony"):
		return "HarmonyOS"
	case strings.Contains(lower, "android") || strings.Contains(lower, "okhttp") || strings.Contains(lower, "dalvik"):
		return "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "cfnetwork") || strings.Contains(lower, "darwin"):
		return "iOS"
	case strings.Contains(lower, "windows"):
		return "Windows"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macintosh"):
		return "macOS"
	case strings.Contains(lower, "linux") || strings.Contains(lower, "x11"):
		return "Linux"
	case strings.Contains(lower, "dart/"):
		return "Flutter"
	default:
		return "Other"
	}
}

func (s *RealtimeService) touchPresence(ctx context.Context, client *realtimeClient, lastSeen time.Time) error {
	if s.repository == nil {
		return nil
	}
	conn := client.presence
	conn.LastSeenAt = lastSeen.UTC()
	return s.repository.UpsertConnection(ctx, conn, s.presenceTTL)
}

func (s *RealtimeService) refreshPresence(ctx context.Context, client *realtimeClient) {
	if s.repository == nil {
		return
	}
	if _, err := s.repository.RefreshConnection(ctx, client.connectionID, s.presenceTTL); err != nil && !stderrors.Is(err, context.Canceled) {
		s.log.Debug("refresh realtime presence failed", zap.Error(err), zap.String("connectionId", client.connectionID))
	}
}

func (s *RealtimeService) removePresence(ctx context.Context, client *realtimeClient) {
	if s.repository == nil {
		return
	}
	if err := s.repository.RemoveConnection(ctx, client.session.AppID, client.session.UserID, client.connectionID); err != nil && !stderrors.Is(err, context.Canceled) {
		s.log.Debug("remove realtime presence failed", zap.Error(err), zap.String("connectionId", client.connectionID))
	}
}

// accrueOnline 把 accruedAt 到 until 这段在线时间记进 user_online_daily（行为画像的在线时长）。
// 管理员的控制台连接（AppID 0）不记。写库放到后台，不卡读泵。
func (s *RealtimeService) accrueOnline(client *realtimeClient, until time.Time) {
	session := client.session
	if s.identities == nil || session == nil || session.AppID == 0 || session.UserID == 0 {
		return
	}
	from := client.accruedAt
	if !until.After(from) {
		return
	}
	client.accruedAt = until
	appID, userID := session.AppID, session.UserID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.identities.AccrueOnlineSeconds(ctx, appID, userID, from, until); err != nil {
			s.log.Debug("accrue online seconds failed", zap.Error(err), zap.Int64("userId", userID))
		}
	}()
}

func (s *RealtimeService) registerLocal(client *realtimeClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	appClients := s.clients[client.session.AppID]
	if appClients == nil {
		appClients = make(map[int64]map[string]*realtimeClient)
		s.clients[client.session.AppID] = appClients
	}
	userClients := appClients[client.session.UserID]
	if userClients == nil {
		userClients = make(map[string]*realtimeClient)
		appClients[client.session.UserID] = userClients
	}
	userClients[client.connectionID] = client
}

func (s *RealtimeService) unregisterLocal(client *realtimeClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	appClients := s.clients[client.session.AppID]
	if appClients == nil {
		return
	}
	userClients := appClients[client.session.UserID]
	if userClients == nil {
		return
	}
	delete(userClients, client.connectionID)
	if len(userClients) == 0 {
		delete(appClients, client.session.UserID)
	}
	if len(appClients) == 0 {
		delete(s.clients, client.session.AppID)
	}
}

func (s *RealtimeService) dispatchLocal(appID int64, userID int64, payload []byte) {
	s.mu.RLock()
	appClients := s.clients[appID]
	if appClients == nil {
		s.mu.RUnlock()
		return
	}
	userClients := appClients[userID]
	if userClients == nil {
		s.mu.RUnlock()
		return
	}
	targets := make([]*realtimeClient, 0, len(userClients))
	for _, client := range userClients {
		targets = append(targets, client)
	}
	s.mu.RUnlock()
	for _, client := range targets {
		select {
		case client.send <- append([]byte(nil), payload...):
		default:
			s.log.Debug("drop realtime message due to backpressure", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.String("connectionId", client.connectionID))
		}
	}
}

// dispatchApp 投递给某个应用下的全部本机连接。
func (s *RealtimeService) dispatchApp(appID int64, payload []byte) {
	s.mu.RLock()
	var targets []*realtimeClient
	for _, userClients := range s.clients[appID] {
		for _, client := range userClients {
			targets = append(targets, client)
		}
	}
	s.mu.RUnlock()
	for _, client := range targets {
		select {
		case client.send <- append([]byte(nil), payload...):
		default:
			s.log.Debug("drop realtime app message due to backpressure", zap.Int64("appid", appID), zap.String("connectionId", client.connectionID))
		}
	}
}

func (s *RealtimeService) presenceJanitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.repository == nil {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			removed, err := s.repository.CleanupExpired(ctx)
			if err != nil {
				s.log.Debug("cleanup realtime presence failed", zap.Error(err))
			}
			cancel()
			// 被清掉的是没有正常关闭的连接（实例崩溃、断网），补发下线事件；
			// 下线时刻按最后一次心跳算，而不是清理的这一刻。
			for _, conn := range removed {
				s.publishPresence(presenceEvent("offline", conn, conn.LastSeenAt))
			}
		}
	}
}

func (c *realtimeClient) enqueue(event realtimedomain.Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	if c.publicID != "" {
		// 信封与 data 里所有「用户 ID 字段且值是自己主键」的地方，与 HTTP 响应同一套规则
		payload = response.RewriteUserID(payload, c.internalID, c.publicID)
	}
	select {
	case c.send <- payload:
	default:
	}
}

// closeWith 发送关闭帧后断开；读泵随之退出并走正常的下线流程。
func (c *realtimeClient) closeWith(code int, reason string) {
	c.closeOnce.Do(func() {
		_ = c.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
		_ = c.conn.Close()
	})
}

func (c *realtimeClient) readPump() {
	defer func() {
		c.service.unregisterLocal(c)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c.service.removePresence(ctx, c)
		cancel()
		_ = c.conn.Close()
		c.service.accrueOnline(c, time.Now().UTC())
		c.service.publishPresence(presenceEvent("offline", c.presence, time.Now().UTC()))
	}()
	_ = c.conn.SetReadDeadline(time.Now().Add(c.service.pongWait))
	c.conn.SetPongHandler(func(_ string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.service.pongWait))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.service.refreshPresence(ctx, c)
		cancel()
		if now := time.Now().UTC(); now.Sub(c.accruedAt) >= onlineAccrualInterval {
			c.service.accrueOnline(c, now)
		}
		return nil
	})
	for {
		messageType, payload, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				c.service.log.Debug("websocket closed unexpectedly", zap.Error(err), zap.String("connectionId", c.connectionID))
			}
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		var inbound struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &inbound); err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(inbound.Type), "ping") {
			c.enqueue(realtimedomain.Event{
				ID:        uuid.NewString(),
				Type:      "system.pong",
				AppID:     c.session.AppID,
				UserID:    c.session.UserID,
				Timestamp: time.Now().UTC(),
			})
		}
	}
}

func (c *realtimeClient) writePump() {
	ticker := time.NewTicker(c.service.pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case payload, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.service.writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(c.service.writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			c.service.refreshPresence(ctx, c)
			cancel()
		case <-c.service.stopCh:
			return
		}
	}
}

// ExtractRealtimeToken 取 WebSocket 握手里的访问令牌（Authorization 头或 aegis.jwt. 子协议）。
func ExtractRealtimeToken(req *http.Request) string {
	return extractRealtimeToken(req)
}

func extractRealtimeToken(req *http.Request) string {
	if req == nil {
		return ""
	}
	if token := bearerToken(req.Header.Get("Authorization")); token != "" {
		return token
	}
	for _, protocol := range websocket.Subprotocols(req) {
		const prefix = "aegis.jwt."
		if strings.HasPrefix(protocol, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(protocol, prefix))
		}
	}
	return ""
}

// WebSocketOriginChecker 生成 WebSocket 升级的 Origin 闸门：允许无 Origin
// （非浏览器客户端）、同 Host 请求，以及 CORS 白名单中的来源。
// 除 /api/ws 外，管理端的 AI Agent 对话通道也复用同一套判定。
func WebSocketOriginChecker(allowedOrigins []string) func(*http.Request) bool {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, raw := range allowedOrigins {
		if normalized := normalizeWebSocketOrigin(raw); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return func(req *http.Request) bool {
		if req == nil {
			return false
		}
		rawOrigin := strings.TrimSpace(req.Header.Get("Origin"))
		if rawOrigin == "" {
			return true
		}
		origin, err := url.Parse(rawOrigin)
		if err != nil || origin.Host == "" {
			return false
		}
		if strings.EqualFold(origin.Host, req.Host) {
			return true
		}
		_, ok := allowed[normalizeWebSocketOrigin(rawOrigin)]
		return ok
	}
}

func normalizeWebSocketOrigin(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme + "://" + parsed.Host)
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
