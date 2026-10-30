package gateway

import (
	"encoding/json"

	"aegis/services/listentogether/internal/room"
)

// ProtocolVersion WebSocket 协议版本（docs/listen-together.md「协议」一节）。
const ProtocolVersion = 1

// 客户端发来的命令。
const (
	OpHello          = "hello"
	OpAuth           = "auth"
	OpPing           = "ping"
	OpRoomCreate     = "room.create"
	OpRoomJoin       = "room.join"
	OpRoomLeave      = "room.leave"
	OpRoomClose      = "room.close"
	OpRoomSync       = "room.sync"
	OpRoomSettings   = "room.settings"
	OpMemberKick     = "member.kick"
	OpMemberTransfer = "member.transfer"
	OpMemberGrant    = "member.grant"
	OpQueueSet       = "queue.set"
	OpPlayback       = "playback.update"
	OpRequestAdd     = "request.add"
	OpRequestResolve = "request.resolve"
	OpControl        = "control"
	OpChat           = "chat.send"
	OpReaction       = "reaction"
	OpStatus         = "status"
)

// 服务端下发的帧。
const (
	FrameWelcome  = "welcome"
	FrameReply    = "reply"
	FrameSnapshot = "snapshot"
	FrameEvent    = "event"
	FramePong     = "pong"
	FrameBye      = "bye"
)

// 连接级错误码（bye 帧与握手失败）。
const (
	ByeTicketInvalid = "ticket_invalid"
	ByeTicketExpired = "ticket_expired"
	ByeHelloTimeout  = "hello_timeout"
	ByeReplaced      = "replaced"
	ByeRevoked       = "revoked"
	ByeShutdown      = "shutdown"
	ByeOverloaded    = "overloaded"
	ByeSlowConsumer  = "slow_consumer"
	ByeProtocol      = "protocol_error"
)

// inbound 客户端帧：{ id, type, data }。id 由客户端给，回复帧原样带回。
type inbound struct {
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// outbound 服务端帧。
type outbound struct {
	Type  string     `json:"type"`
	ID    string     `json:"id,omitempty"`
	OK    *bool      `json:"ok,omitempty"`
	Room  string     `json:"room,omitempty"`
	Data  any        `json:"data,omitempty"`
	Error *wireError `json:"error,omitempty"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type helloData struct {
	Ticket string `json:"ticket"`
	Client struct {
		Platform string `json:"platform"`
		Version  string `json:"version"`
	} `json:"client"`
}

type authData struct {
	Ticket string `json:"ticket"`
}

type pingData struct {
	T0 int64 `json:"t0"`
}

type pongData struct {
	T0 int64 `json:"t0"`
	TS int64 `json:"ts"`
}

type welcomeData struct {
	Protocol        int        `json:"protocol"`
	ConnID          string     `json:"connId"`
	Instance        string     `json:"instance"`
	ServerTime      int64      `json:"serverTime"`
	TicketExpiresAt int64      `json:"ticketExpiresAt"`
	HeartbeatSec    int        `json:"heartbeatSec"`
	User            userView   `json:"user"`
	Limits          limitsView `json:"limits"`
	// Room 用户已在某个房间里时为该房间 ID，紧接着会收到 snapshot 帧。
	Room string `json:"room,omitempty"`
}

type userView struct {
	UserID   int64  `json:"userId"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
}

type limitsView struct {
	Capacity    int `json:"capacity"`
	CapacityMax int `json:"capacityMax"`
	QueueLimit  int `json:"queueLimit"`
}

type byeData struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Reconnect 客户端是否应自动重连（服务重启时为真，被顶替、被撤销时为假）。
	Reconnect bool `json:"reconnect"`
}

type joinData struct {
	Code string `json:"code"`
}

type userTarget struct {
	UserID int64 `json:"userId"`
}

type grantData struct {
	UserID     int64 `json:"userId"`
	CanControl bool  `json:"canControl"`
}

type queueData struct {
	Items []room.Track `json:"items"`
}

type requestData struct {
	Track room.Track `json:"track"`
}

type resolveData struct {
	RequestID string `json:"requestId"`
	Accept    bool   `json:"accept"`
}

type chatData struct {
	Text string `json:"text"`
}

type reactionData struct {
	Emoji string `json:"emoji"`
}
