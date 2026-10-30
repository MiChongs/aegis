package room

import (
	"context"
	"time"
)

// Repository 房间状态的存储端口（store 包实现 Redis 与内存两种）。
//
// 一个房间整体存成一份文档，UpdateRoom 以乐观并发改它：读、交给 fn 改、仅当期间没人改过才写回，
// 否则重新读再来，fn 因此可能被调用多次，必须是纯的（不发事件、不碰外部）。
type Repository interface {
	// CreateRoom 新建房间并占用房间号；房间号已被占用返回 ErrCodeTaken。
	CreateRoom(ctx context.Context, r *Room) error
	LoadRoom(ctx context.Context, app, id string) (*Room, error)
	UpdateRoom(ctx context.Context, app, id string, fn func(*Room) error) (*Room, error)
	// DeleteRoom 删除房间、房间号、聊天记录，以及仍指向它的「用户所在房间」。
	DeleteRoom(ctx context.Context, r *Room) error
	FindByCode(ctx context.Context, app, code string) (string, error)

	// UserRoom 用户当前所在房间；不在任何房间返回 ""。
	UserRoom(ctx context.Context, app string, userID int64) (string, error)
	SetUserRoom(ctx context.Context, app string, userID int64, roomID string) error
	// ClearUserRoom 仅当用户当前所在房间是 roomID 时清除（防止清掉刚加入的新房间）。
	ClearUserRoom(ctx context.Context, app string, userID int64, roomID string) error

	// ListRooms 某应用的房间，按最近活动倒序。
	ListRooms(ctx context.Context, app string, offset, limit int) ([]string, int, error)
	// Apps 有房间的应用。
	Apps(ctx context.Context) ([]string, error)

	AppendChat(ctx context.Context, app, roomID string, msg ChatMessage, keep int) error
	RecentChat(ctx context.Context, app, roomID string, limit int) ([]ChatMessage, error)

	// Heartbeat 实例心跳；Instances 当前存活的实例。
	Heartbeat(ctx context.Context, info InstanceInfo, ttl time.Duration) error
	Instances(ctx context.Context) ([]InstanceInfo, error)
	RemoveInstance(ctx context.Context, id string) error

	// TryLock 抢一把带过期的锁（巡检只需一个实例做）；拿到返回释放函数。
	TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error)
}

// InstanceInfo 一个服务实例的心跳内容。
type InstanceInfo struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	StartedAt   int64  `json:"startedAt"`
	Connections int    `json:"connections"`
	Rooms       int    `json:"rooms"`
	HeartbeatAt int64  `json:"heartbeatAt"`
}

// Bus 跨实例事件总线端口（bus 包实现 NATS 与进程内两种）。
type Bus interface {
	// PublishRoom 把房间事件发给所有订阅了这个房间的实例。
	PublishRoom(ctx context.Context, app, roomID string, event Event) error
	// SubscribeRoom 实例上有这个房间的连接时订阅，最后一个连接离开时退订。
	SubscribeRoom(app, roomID string, handler func(Event)) (func(), error)
	// PublishUser 发给某用户在任意实例上的连接（顶替、撤销）。
	PublishUser(ctx context.Context, signal UserSignal) error
	SubscribeUsers(handler func(UserSignal)) (func(), error)
}

// 用户信号。
const (
	// SignalReplace 同一用户在别处建了新连接，旧连接（ConnID 不同的）应断开。
	SignalReplace = "replace"
	// SignalRevoke 主服务要求断开（封禁、强制下线、失去权益）。
	SignalRevoke = "revoke"
)

// UserSignal 发给某个用户全部连接的控制信号。
type UserSignal struct {
	Kind   string `json:"kind"`
	AppKey string `json:"appKey"`
	UserID int64  `json:"userId"`
	ConnID string `json:"connId,omitempty"`
	Reason string `json:"reason,omitempty"`
}
