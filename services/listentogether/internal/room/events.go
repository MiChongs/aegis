package room

// 事件种类。客户端按块应用：同一块 rev 不比手里新的丢弃。
const (
	EventMemberJoined  = "member.joined"
	EventMemberLeft    = "member.left"
	EventMemberUpdated = "member.updated"
	EventRoomUpdated   = "room.updated"
	EventRoomClosed    = "room.closed"
	EventPlayback      = "playback"
	EventQueue         = "queue"
	EventRequests      = "requests"
	EventChat          = "chat"
	EventReaction      = "reaction"
	// EventControl 只发给房主：有控制权的成员要房主执行的播放指令。
	EventControl = "control"
)

// 成员离开的原因。
const (
	LeftLeave   = "leave"
	LeftKick    = "kick"
	LeftTimeout = "timeout"
	LeftRevoked = "revoked"
)

// 房间解散的原因。
const (
	ClosedByHost  = "host"
	ClosedIdle    = "idle"
	ClosedByAdmin = "admin"
	ClosedEmpty   = "empty"
)

// Event 房间里发生的一件事。只带变化的那一块，客户端据 kind 合并。
type Event struct {
	Kind string `json:"kind"`
	Rev  int64  `json:"rev"`
	// Target 非零时只投递给这个用户（例如发给房主的播放指令）。
	Target int64 `json:"target,omitempty"`

	Member   *MemberView  `json:"member,omitempty"`
	UserID   int64        `json:"userId,omitempty"`
	Reason   string       `json:"reason,omitempty"`
	Room     *MetaView    `json:"room,omitempty"`
	Playback *Playback    `json:"playback,omitempty"`
	Queue    *QueueView   `json:"queue,omitempty"`
	Requests *RequestView `json:"requests,omitempty"`
	Chat     *ChatMessage `json:"chat,omitempty"`
	Reaction *Reaction    `json:"reaction,omitempty"`
	Control  *Control     `json:"control,omitempty"`
}

// MemberView 发给客户端的成员（不含连接与实例等内部字段）。
type MemberView struct {
	UserID     int64        `json:"userId"`
	Nickname   string       `json:"nickname"`
	Avatar     string       `json:"avatar,omitempty"`
	Platform   string       `json:"platform,omitempty"`
	Role       string       `json:"role"`
	CanControl bool         `json:"canControl"`
	Online     bool         `json:"online"`
	JoinedAt   int64        `json:"joinedAt"`
	Status     MemberStatus `json:"status"`
	Rev        int64        `json:"rev"`
}

// MetaView 房间的基本信息与设置。
type MetaView struct {
	ID        string   `json:"id"`
	Code      string   `json:"code"`
	HostID    int64    `json:"hostId"`
	Settings  Settings `json:"settings"`
	CreatedAt int64    `json:"createdAt"`
	Rev       int64    `json:"rev"`
}

// QueueView 共享队列。
type QueueView struct {
	Items []Track `json:"items"`
	Rev   int64   `json:"rev"`
}

// RequestView 待处理的点歌。
type RequestView struct {
	Items []Request `json:"items"`
	Rev   int64     `json:"rev"`
}

// Snapshot 加入或重连时下发的完整状态。
type Snapshot struct {
	Room     MetaView      `json:"room"`
	Members  []MemberView  `json:"members"`
	Queue    QueueView     `json:"queue"`
	Playback Playback      `json:"playback"`
	Requests RequestView   `json:"requests"`
	Chat     []ChatMessage `json:"chat"`
	Rev      int64         `json:"rev"`
}

// View 成员的对外形态。
func (m *Member) View() MemberView {
	return MemberView{
		UserID: m.UserID, Nickname: m.Nickname, Avatar: m.Avatar, Platform: m.Platform,
		Role: m.Role, CanControl: m.CanControl, Online: m.Online(), JoinedAt: m.JoinedAt,
		Status: m.Status, Rev: m.Rev,
	}
}

// Meta 房间基本信息。
func (r *Room) Meta() MetaView {
	return MetaView{ID: r.ID, Code: r.Code, HostID: r.HostID, Settings: r.Settings, CreatedAt: r.CreatedAt, Rev: r.SettingsRev}
}

// QueueView 队列。
func (r *Room) QueueView() QueueView {
	items := r.Queue
	if items == nil {
		items = []Track{}
	}
	return QueueView{Items: items, Rev: r.QueueRev}
}

// RequestView 点歌。
func (r *Room) RequestView() RequestView {
	items := r.Requests
	if items == nil {
		items = []Request{}
	}
	return RequestView{Items: items, Rev: r.RequestsRev}
}

// Snapshot 完整状态。
func (r *Room) Snapshot(chat []ChatMessage) Snapshot {
	members := make([]MemberView, 0, len(r.Members))
	for _, member := range r.Members {
		members = append(members, member.View())
	}
	if chat == nil {
		chat = []ChatMessage{}
	}
	return Snapshot{
		Room: r.Meta(), Members: members, Queue: r.QueueView(), Playback: r.Playback,
		Requests: r.RequestView(), Chat: chat, Rev: r.Rev,
	}
}

func memberEvent(kind string, member *Member, rev int64) Event {
	view := member.View()
	return Event{Kind: kind, Rev: rev, Member: &view}
}

func roomUpdated(r *Room) Event {
	meta := r.Meta()
	return Event{Kind: EventRoomUpdated, Rev: r.Rev, Room: &meta}
}
