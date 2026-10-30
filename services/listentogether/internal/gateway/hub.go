package gateway

import (
	"context"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/telemetry"
)

// Hub 本实例上的连接表：按连接、按用户、按房间索引。
// 房间事件从总线进来后只投递给本实例上在那个房间里的连接；本实例上一个房间的第一条连接
// 到来时订阅总线上这个房间的主题，最后一条离开时退订。
type Hub struct {
	mu      sync.Mutex
	conns   map[string]*Conn
	byUser  map[string]map[string]*Conn
	rooms   map[string]*roomSubscription
	bus     room.Bus
	metrics *telemetry.Metrics
	log     *zap.Logger
	unsub   func()
	onUser  func(room.UserSignal)
}

type roomSubscription struct {
	conns map[string]*Conn
	unsub func()
}

func hubUserKey(app string, userID int64) string { return app + "/" + strconv.FormatInt(userID, 10) }
func hubRoomKey(app, roomID string) string       { return app + "/" + roomID }

// NewHub 新建连接表并订阅用户信号。
func NewHub(bus room.Bus, metrics *telemetry.Metrics, log *zap.Logger) (*Hub, error) {
	h := &Hub{
		conns: map[string]*Conn{}, byUser: map[string]map[string]*Conn{}, rooms: map[string]*roomSubscription{},
		bus: bus, metrics: metrics, log: log,
	}
	unsub, err := bus.SubscribeUsers(h.handleSignal)
	if err != nil {
		return nil, err
	}
	h.unsub = unsub
	return h, nil
}

// OnUserSignal 撤销信号的额外处理（让房间服务把人移出房间）。
func (h *Hub) OnUserSignal(fn func(room.UserSignal)) { h.onUser = fn }

// Counts 当前连接数与房间数（实例心跳上报）。
func (h *Hub) Counts() (int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns), len(h.rooms)
}

// UserConnections 本实例上某用户的连接数。
func (h *Hub) UserConnections(app string, userID int64) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byUser[hubUserKey(app, userID)])
}

// Add 登记一条已通过握手的连接，返回本实例上同一用户的其它连接（由调用方顶替断开）。
func (h *Hub) Add(c *Conn) []*Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c.id] = c
	key := hubUserKey(c.identity.AppKey, c.identity.UserID)
	var others []*Conn
	for _, other := range h.byUser[key] {
		others = append(others, other)
	}
	if h.byUser[key] == nil {
		h.byUser[key] = map[string]*Conn{}
	}
	h.byUser[key][c.id] = c
	h.metrics.Connections.Set(float64(len(h.conns)))
	return others
}

// Remove 注销连接（同时离开它订阅的房间）。
func (h *Hub) Remove(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.conns[c.id]; !ok {
		return
	}
	delete(h.conns, c.id)
	key := hubUserKey(c.identity.AppKey, c.identity.UserID)
	delete(h.byUser[key], c.id)
	if len(h.byUser[key]) == 0 {
		delete(h.byUser, key)
	}
	h.unbindLocked(c)
	h.metrics.Connections.Set(float64(len(h.conns)))
}

// Bind 让连接收某个房间的事件（先离开之前的房间）。
func (h *Hub) Bind(c *Conn, roomID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.roomID == roomID {
		return nil
	}
	h.unbindLocked(c)
	key := hubRoomKey(c.identity.AppKey, roomID)
	sub := h.rooms[key]
	if sub == nil {
		app := c.identity.AppKey
		unsub, err := h.bus.SubscribeRoom(app, roomID, func(event room.Event) { h.deliver(app, roomID, event) })
		if err != nil {
			return err
		}
		sub = &roomSubscription{conns: map[string]*Conn{}, unsub: unsub}
		h.rooms[key] = sub
		h.metrics.Rooms.Set(float64(len(h.rooms)))
	}
	sub.conns[c.id] = c
	c.roomID = roomID
	return nil
}

// Unbind 连接不再收房间事件。
func (h *Hub) Unbind(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unbindLocked(c)
}

func (h *Hub) unbindLocked(c *Conn) {
	if c.roomID == "" {
		return
	}
	key := hubRoomKey(c.identity.AppKey, c.roomID)
	if sub := h.rooms[key]; sub != nil {
		delete(sub.conns, c.id)
		if len(sub.conns) == 0 {
			// 退订放到锁外：NATS 退订可能要等回调结束，而回调会来拿这把锁
			go sub.unsub()
			delete(h.rooms, key)
			h.metrics.Rooms.Set(float64(len(h.rooms)))
		}
	}
	c.roomID = ""
}

// RoomOf 连接当前所在房间。
func (h *Hub) RoomOf(c *Conn) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return c.roomID
}

func (h *Hub) deliver(app, roomID string, event room.Event) {
	h.mu.Lock()
	sub := h.rooms[hubRoomKey(app, roomID)]
	if sub == nil {
		h.mu.Unlock()
		return
	}
	targets := make([]*Conn, 0, len(sub.conns))
	for _, c := range sub.conns {
		if event.Target != 0 && event.Target != c.identity.UserID {
			continue
		}
		targets = append(targets, c)
	}
	// 房间解散或自己被移出时，这些连接不再属于这个房间
	for _, c := range targets {
		if event.Kind == room.EventRoomClosed || (event.Kind == room.EventMemberLeft && event.UserID == c.identity.UserID) {
			h.unbindLocked(c)
		}
	}
	h.mu.Unlock()
	for _, c := range targets {
		c.send(outbound{Type: FrameEvent, Room: roomID, Data: event})
		h.metrics.EventsDelivered.Inc()
	}
}

// handleSignal 用户信号：顶替旧连接、撤销全部连接。
func (h *Hub) handleSignal(signal room.UserSignal) {
	h.mu.Lock()
	var targets []*Conn
	for _, c := range h.byUser[hubUserKey(signal.AppKey, signal.UserID)] {
		if signal.Kind == room.SignalReplace && c.id == signal.ConnID {
			continue
		}
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		switch signal.Kind {
		case room.SignalReplace:
			c.bye(ByeReplaced, "你在另一台设备上打开了一起听", false)
		case room.SignalRevoke:
			c.bye(ByeRevoked, "一起听资格已失效", false)
		}
	}
	if signal.Kind == room.SignalRevoke && h.onUser != nil {
		h.onUser(signal)
	}
}

// CloseAll 关停时通知所有连接稍后重连（会连到别的实例）。
func (h *Hub) CloseAll(ctx context.Context) {
	h.mu.Lock()
	conns := make([]*Conn, 0, len(h.conns))
	for _, c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.bye(ByeShutdown, "服务正在重启，请稍后重连", true)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if count, _ := h.Counts(); count == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
	if h.unsub != nil {
		h.unsub()
	}
}
