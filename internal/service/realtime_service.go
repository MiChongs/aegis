package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strings"
	"sync"
	"time"

	authdomain "aegis/internal/domain/auth"
	realtimedomain "aegis/internal/domain/realtime"
	"aegis/internal/event"
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

type RealtimeService struct {
	log         *zap.Logger
	auth        *AuthService
	repository  *redisrepo.RealtimeRepository
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

func NewRealtimeService(log *zap.Logger, auth *AuthService, repository *redisrepo.RealtimeRepository, natsConn *nats.Conn) (*RealtimeService, error) {
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
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
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

func (s *RealtimeService) AuthenticateRequest(ctx context.Context, req *http.Request) (*authdomain.Session, string, error) {
	if s == nil || s.auth == nil {
		return nil, "", apperrors.New(50300, http.StatusServiceUnavailable, "实时服务暂不可用")
	}
	token := extractRealtimeToken(req)
	if token == "" {
		return nil, "", apperrors.New(40100, http.StatusUnauthorized, "访问请求未获授权")
	}
	session, err := s.auth.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, "", err
	}
	return session, token, nil
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
	return nil
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
	for _, key := range []string{"token", "access_token"} {
		if token := strings.TrimSpace(req.URL.Query().Get(key)); token != "" {
			return token
		}
	}
	return ""
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
