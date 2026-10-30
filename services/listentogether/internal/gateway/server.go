// Package gateway 一起听的对外 WebSocket 网关：握手验票、命令分发、事件投递。
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"aegis/pkg/listenticket"
	"aegis/services/listentogether/internal/config"
	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/telemetry"
	"aegis/services/listentogether/internal/ticket"
)

// Server 对外网关。
type Server struct {
	cfg      config.Config
	rooms    *room.Service
	hub      *Hub
	verifier *ticket.Verifier
	metrics  *telemetry.Metrics
	log      *zap.Logger
	upgrader websocket.Upgrader
	ready    func(context.Context) error
	draining chan struct{}
}

// NewServer 构造网关。ready 是就绪探针要额外检查的依赖（Redis、NATS）。
func NewServer(cfg config.Config, rooms *room.Service, hub *Hub, verifier *ticket.Verifier, metrics *telemetry.Metrics, log *zap.Logger, ready func(context.Context) error) *Server {
	s := &Server{cfg: cfg, rooms: rooms, hub: hub, verifier: verifier, metrics: metrics, log: log, ready: ready, draining: make(chan struct{})}
	s.upgrader = websocket.Upgrader{
		ReadBufferSize: 4096, WriteBufferSize: 4096,
		EnableCompression: true,
		CheckOrigin:       s.checkOrigin,
	}
	hub.OnUserSignal(s.revoke)
	return s
}

// Handler 对外路由：/ws、/healthz、/readyz。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", s.serveReady)
	return mux
}

// Drain 停止接受新连接（就绪探针随即失败，负载均衡把流量摘走）。
func (s *Server) Drain() {
	select {
	case <-s.draining:
	default:
		close(s.draining)
	}
}

func (s *Server) serveReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := map[string]any{"instance": s.cfg.InstanceID, "version": s.cfg.Version}
	code := http.StatusOK
	select {
	case <-s.draining:
		status["status"] = "draining"
		code = http.StatusServiceUnavailable
	default:
		if !s.verifier.Ready() {
			status["status"] = "waiting for ticket keys"
			code = http.StatusServiceUnavailable
		} else if err := s.ready(ctx); err != nil {
			status["status"] = err.Error()
			code = http.StatusServiceUnavailable
		} else {
			status["status"] = "ready"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.draining:
		http.Error(w, "draining", http.StatusServiceUnavailable)
		return
	default:
	}
	if count, _ := s.hub.Counts(); s.cfg.MaxConnsPerInstance > 0 && count >= s.cfg.MaxConnsPerInstance {
		s.metrics.ConnectsTotal.WithLabelValues(ByeOverloaded).Inc()
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
		return
	}
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.metrics.ConnectsTotal.WithLabelValues("upgrade_failed").Inc()
		return
	}
	ws.SetReadLimit(maxFrameBytes)
	go s.handshake(ws)
}

// handshake 第一帧必须是 hello（带票据），10 秒内不到就断开。
// 票据放在首帧而不是 URL 里：URL 会进 CDN 与反代的访问日志。
func (s *Server) handshake(ws *websocket.Conn) {
	_ = ws.SetReadDeadline(time.Now().Add(helloTimeout))
	_, raw, err := ws.ReadMessage()
	if err != nil {
		s.reject(ws, ByeHelloTimeout, "握手超时")
		return
	}
	var frame inbound
	if json.Unmarshal(raw, &frame) != nil || frame.Type != OpHello {
		s.reject(ws, ByeProtocol, "第一条消息必须是 hello")
		return
	}
	var hello helloData
	if json.Unmarshal(frame.Data, &hello) != nil {
		s.reject(ws, ByeProtocol, "hello 格式错误")
		return
	}
	claims, err := s.verifier.Verify(strings.TrimSpace(hello.Ticket))
	if err != nil {
		code, message := ByeTicketInvalid, "凭证无效，请重新进入"
		if errors.Is(err, listenticket.ErrMissingFeature) {
			code, message = "not_entitled", "当前账号没有一起听权益"
		}
		s.metrics.ConnectsTotal.WithLabelValues(code).Inc()
		s.reject(ws, code, message)
		return
	}
	c := &Conn{
		id: newConnID(), ws: ws, server: s,
		outbox: make(chan outbound, sendBuffer), done: make(chan struct{}),
		identity: room.Identity{
			AppKey: claims.AppKey, UserID: claims.UserID(), Nickname: claims.Nickname, Avatar: claims.Avatar,
			Device: claims.Device, Platform: room.CleanText(hello.Client.Platform, 16),
		},
		expires:     claims.ExpiresAt.Time.UnixMilli(),
		limiter:     rate.NewLimiter(rate.Limit(20), 40),
		chatLimiter: rate.NewLimiter(rate.Every(time.Second), 5),
	}
	c.log = s.log.With(zap.String("conn", c.id), zap.String("app", c.identity.AppKey), zap.Int64("user", c.identity.UserID))
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { return ws.SetReadDeadline(time.Now().Add(pongWait)) })
	ws.SetPingHandler(func(data string) error {
		_ = ws.SetReadDeadline(time.Now().Add(pongWait))
		return ws.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(writeWait))
	})

	// 同一用户只保留最新的一条连接：本实例上的直接顶替，其它实例经总线通知
	for _, old := range s.hub.Add(c) {
		old.bye(ByeReplaced, "你在另一台设备上打开了一起听", false)
	}
	_ = s.rooms.PublishReplace(context.Background(), c.actor())
	s.metrics.ConnectsTotal.WithLabelValues("ok").Inc()
	go c.writeLoop()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	snapshot, attachErr := s.rooms.Attach(ctx, c.actor())
	cancel()
	welcome := welcomeData{
		Protocol: ProtocolVersion, ConnID: c.id, Instance: s.cfg.InstanceID, ServerTime: time.Now().UnixMilli(),
		TicketExpiresAt: c.expires, HeartbeatSec: int(pingPeriod / time.Second),
		User:   userView{UserID: c.identity.UserID, Nickname: c.identity.Nickname, Avatar: c.identity.Avatar},
		Limits: limitsView{Capacity: s.cfg.RoomCapacity, CapacityMax: s.cfg.RoomCapacityMax, QueueLimit: s.cfg.QueueLimit},
	}
	if attachErr != nil {
		c.log.Warn("attach room failed", zap.Error(attachErr))
	}
	if snapshot != nil && s.hub.Bind(c, snapshot.Room.ID) == nil {
		welcome.Room = snapshot.Room.ID
		c.send(outbound{Type: FrameWelcome, Data: welcome})
		c.send(outbound{Type: FrameSnapshot, Room: snapshot.Room.ID, Data: snapshot})
	} else {
		c.send(outbound{Type: FrameWelcome, Data: welcome})
	}

	c.readLoop()
	c.close()
	roomID := s.hub.RoomOf(c)
	s.hub.Remove(c)
	if roomID != "" {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		if err := s.rooms.Detach(ctx, c.actor(), roomID); err != nil {
			c.log.Warn("detach failed", zap.Error(err))
		}
		cancel()
	}
}

func (s *Server) reject(ws *websocket.Conn, code, message string) {
	raw, _ := json.Marshal(outbound{Type: FrameBye, Data: byeData{Code: code, Message: message}})
	_ = ws.SetWriteDeadline(time.Now().Add(writeWait))
	_ = ws.WriteMessage(websocket.TextMessage, raw)
	_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, code))
	_ = ws.Close()
}

// revoke 撤销信号：抢到锁的实例负责把人移出房间（连接已由 Hub 断开）。
func (s *Server) revoke(signal room.UserSignal) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		if err := s.rooms.RevokeOnce(ctx, signal.AppKey, signal.UserID); err != nil {
			s.log.Warn("revoke user failed", zap.String("app", signal.AppKey), zap.Int64("user", signal.UserID), zap.Error(err))
		}
	}()
}

var connIDEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func newConnID() string {
	buf := make([]byte, 10)
	_, _ = rand.Read(buf)
	return connIDEncoding.EncodeToString(buf)
}
