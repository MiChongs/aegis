package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	realtimedomain "aegis/internal/domain/realtime"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

type UserEventPublisher interface {
	PublishUserEvent(ctx context.Context, appID int64, userID int64, eventType string, data map[string]any) error
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
	sub     *nats.Subscription
	stopCh  chan struct{}
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
}

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
				AppID:  0, // 管理员连接标识
				UserID: access.AdminID,
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
	if s.sub != nil {
		_ = s.sub.Unsubscribe()
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
	// 订阅用户级事件
	sub, err := s.natsConn.Subscribe(event.SubjectRealtimeUserPrefix+".*.*", func(msg *nats.Msg) {
		appID, userID, ok := event.MatchRealtimeUserSubject(msg.Subject)
		if !ok {
			return
		}
		s.dispatchLocal(appID, userID, msg.Data)
	})
	if err != nil {
		return err
	}
	s.sub = sub

	// 订阅全局广播事件（系统公告等），转发给所有已连接客户端
	_, err = s.natsConn.Subscribe(event.SubjectSystemAnnouncement, func(msg *nats.Msg) {
		s.broadcastAll(msg.Data)
	})
	return err
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
		conn:         conn,
		send:         make(chan []byte, s.sendBuffer),
	}
	if err := s.touchPresence(req.Context(), client, now); err != nil {
		_ = conn.Close()
		return err
	}
	s.registerLocal(client)
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

func (s *RealtimeService) touchPresence(ctx context.Context, client *realtimeClient, lastSeen time.Time) error {
	if s.repository == nil {
		return nil
	}
	// 管理员连接（AppID=0）不记录在线状态
	if client.session.AppID == 0 {
		return nil
	}
	conn := realtimedomain.PresenceConnection{
		ConnectionID: client.connectionID,
		AppID:        client.session.AppID,
		UserID:       client.session.UserID,
		TokenID:      client.session.TokenID,
		DeviceID:     client.session.DeviceID,
		IP:           client.ip,
		UserAgent:    client.userAgent,
		ConnectedAt:  client.connectedAt,
		LastSeenAt:   lastSeen.UTC(),
		ServerID:     s.serverID,
	}
	if strings.TrimSpace(conn.UserAgent) == "" {
		conn.UserAgent = client.conn.Subprotocol()
	}
	if strings.TrimSpace(conn.UserAgent) == "" {
		conn.UserAgent = client.conn.RemoteAddr().String()
	}
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
			if err := s.repository.CleanupExpired(ctx); err != nil {
				s.log.Debug("cleanup realtime presence failed", zap.Error(err))
			}
			cancel()
		}
	}
}

func (c *realtimeClient) enqueue(event realtimedomain.Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	select {
	case c.send <- payload:
	default:
	}
}

func (c *realtimeClient) readPump() {
	defer func() {
		c.service.unregisterLocal(c)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		c.service.removePresence(ctx, c)
		cancel()
		_ = c.conn.Close()
	}()
	_ = c.conn.SetReadDeadline(time.Now().Add(c.service.pongWait))
	c.conn.SetPongHandler(func(_ string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(c.service.pongWait))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.service.refreshPresence(ctx, c)
		cancel()
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
