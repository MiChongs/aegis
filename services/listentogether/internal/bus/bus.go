// Package bus 跨实例事件总线：NATS（生产）与进程内（单实例与测试）。
//
// 主题：
//
//	lt.room.{app}.{room}  房间事件；实例上有该房间的连接时才订阅
//	lt.user               用户信号（顶替、撤销），所有实例都订阅、本地过滤
//	listen.control.revoke 主服务发来的撤销通知（listenticket.Revoke），转成 SignalRevoke
package bus

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"aegis/pkg/listenticket"
	"aegis/services/listentogether/internal/room"
)

const (
	roomSubjectPrefix = "lt.room."
	userSubject       = "lt.user"
)

// roomSubject NATS 主题里不能有点号与空白；appKey 与房间 ID 都是受限字符集，这里再兜一层。
func roomSubject(app, roomID string) string {
	return roomSubjectPrefix + token(app) + "." + token(roomID)
}

func token(value string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\r', '\n':
			return '_'
		}
		return r
	}, value)
}

// NATS 基于 NATS core（at-most-once）：房间事件是瞬时状态，客户端重连时取快照补齐，不需要持久化。
type NATS struct {
	conn *nats.Conn
	log  *zap.Logger
}

// NewNATS 用已连接的 NATS 构造总线。
func NewNATS(conn *nats.Conn, log *zap.Logger) *NATS {
	return &NATS{conn: conn, log: log}
}

func (b *NATS) PublishRoom(_ context.Context, app, roomID string, event room.Event) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return b.conn.Publish(roomSubject(app, roomID), raw)
}

func (b *NATS) SubscribeRoom(app, roomID string, handler func(room.Event)) (func(), error) {
	sub, err := b.conn.Subscribe(roomSubject(app, roomID), func(msg *nats.Msg) {
		var event room.Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			b.log.Warn("bad room event", zap.Error(err))
			return
		}
		handler(event)
	})
	if err != nil {
		return nil, err
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

func (b *NATS) PublishUser(_ context.Context, signal room.UserSignal) error {
	raw, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	return b.conn.Publish(userSubject, raw)
}

func (b *NATS) SubscribeUsers(handler func(room.UserSignal)) (func(), error) {
	own, err := b.conn.Subscribe(userSubject, func(msg *nats.Msg) {
		var signal room.UserSignal
		if json.Unmarshal(msg.Data, &signal) == nil {
			handler(signal)
		}
	})
	if err != nil {
		return nil, err
	}
	revoke, err := b.conn.Subscribe(listenticket.SubjectRevoke, func(msg *nats.Msg) {
		var payload listenticket.Revoke
		if json.Unmarshal(msg.Data, &payload) != nil || payload.AppKey == "" || payload.UserID <= 0 {
			return
		}
		handler(room.UserSignal{Kind: room.SignalRevoke, AppKey: payload.AppKey, UserID: payload.UserID, Reason: payload.Reason})
	})
	if err != nil {
		_ = own.Unsubscribe()
		return nil, err
	}
	return func() {
		_ = own.Unsubscribe()
		_ = revoke.Unsubscribe()
	}, nil
}

var _ room.Bus = (*NATS)(nil)

// Memory 进程内总线：同步投递（与 NATS 一样不保证跨主题的先后）。
type Memory struct {
	mu    sync.RWMutex
	next  int
	rooms map[string]map[int]func(room.Event)
	users map[int]func(room.UserSignal)
}

// NewMemory 新建进程内总线。
func NewMemory() *Memory {
	return &Memory{rooms: map[string]map[int]func(room.Event){}, users: map[int]func(room.UserSignal){}}
}

func (b *Memory) PublishRoom(_ context.Context, app, roomID string, event room.Event) error {
	b.mu.RLock()
	handlers := make([]func(room.Event), 0, len(b.rooms[roomSubject(app, roomID)]))
	for _, handler := range b.rooms[roomSubject(app, roomID)] {
		handlers = append(handlers, handler)
	}
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(event)
	}
	return nil
}

func (b *Memory) SubscribeRoom(app, roomID string, handler func(room.Event)) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subject := roomSubject(app, roomID)
	if b.rooms[subject] == nil {
		b.rooms[subject] = map[int]func(room.Event){}
	}
	b.next++
	id := b.next
	b.rooms[subject][id] = handler
	return func() {
		b.mu.Lock()
		delete(b.rooms[subject], id)
		if len(b.rooms[subject]) == 0 {
			delete(b.rooms, subject)
		}
		b.mu.Unlock()
	}, nil
}

func (b *Memory) PublishUser(_ context.Context, signal room.UserSignal) error {
	b.mu.RLock()
	handlers := make([]func(room.UserSignal), 0, len(b.users))
	for _, handler := range b.users {
		handlers = append(handlers, handler)
	}
	b.mu.RUnlock()
	for _, handler := range handlers {
		handler(signal)
	}
	return nil
}

func (b *Memory) SubscribeUsers(handler func(room.UserSignal)) (func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	b.users[id] = handler
	return func() {
		b.mu.Lock()
		delete(b.users, id)
		b.mu.Unlock()
	}, nil
}

var _ room.Bus = (*Memory)(nil)
