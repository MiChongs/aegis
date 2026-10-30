// Package room 一起听的领域层：房间、成员、共享队列、播放状态与它们的规则。
//
// 这一层不知道 WebSocket、Redis 与 NATS 的存在：它定义端口（Repository、Bus），
// 由 store / bus 包实现；网关把客户端的帧翻译成这里的命令。规则写成纯函数（rules.go），
// 单测不需要任何基础设施。
//
// ── 同步模型：房主权威 ──
//
// 房主的本地播放器是唯一事实：房主客户端观察自己的播放器，把「放哪首、放到哪、在不在放」
// 和队列推给服务端；其他人照着渲染（各自用自己的音源取流，服务端从不碰音频）。
// 有控制权的成员发出的播放指令经服务端转给房主执行，执行结果再从房主那里广播出来。
// 这样房主照常用原来的播放界面，不需要另一套「房间播放器」。
//
// ── 版本号 ──
//
// 房间每次变化 Rev 加一，并把新 Rev 记在变化的那一块上（播放、队列、设置、点歌、某个成员）。
// 多实例下事件可能乱序到达，客户端按块比较：某块收到的 rev 不比手里的新就丢弃。
// 不同块之间互不影响，因此不需要全局有序的事件流。
package room

import (
	"time"
)

// 角色。
const (
	RoleHost   = "host"
	RoleMember = "member"
)

// 控制模式：只有房主（与被授权的人）能控制，或者所有人都能控制。
const (
	ControlHost = "host"
	ControlAll  = "all"
)

// 成员的播放状态（客户端上报，只用于展示）。
const (
	StateIdle      = "idle"
	StateBuffering = "buffering"
	StatePlaying   = "playing"
	StatePaused    = "paused"
	StateError     = "error"
)

// Identity 已验过票据的用户。
type Identity struct {
	AppKey   string
	UserID   int64
	Nickname string
	Avatar   string
	Device   string
	Platform string
}

// Track 一首歌。服务端只认标识与展示字段，取流由各端自己用自己的音源完成。
type Track struct {
	// ItemID 队列项标识，房主客户端给（同一首歌在队列里出现两次时两项不同）。
	ItemID     string `json:"itemId"`
	Source     string `json:"source"`
	SongID     string `json:"songId"`
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Cover      string `json:"cover,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// RequestedBy 点歌人（对外用户 ID），房主自己加的为 0。
	RequestedBy int64 `json:"requestedBy,omitempty"`
}

// Playback 房间的播放状态：在服务端时刻 At 时，ItemID 这一项放到 PositionMs。
// 正在播放时，任意时刻 t 的位置 = PositionMs + (t - At) × Rate。
type Playback struct {
	ItemID     string  `json:"itemId,omitempty"`
	PositionMs int64   `json:"positionMs"`
	Playing    bool    `json:"playing"`
	Rate       float64 `json:"rate"`
	At         int64   `json:"at"`
	By         int64   `json:"by,omitempty"`
	Rev        int64   `json:"rev"`
}

// Settings 房主可调的房间设置。
type Settings struct {
	Name          string `json:"name"`
	ControlMode   string `json:"controlMode"`
	AllowRequests bool   `json:"allowRequests"`
	// Locked 锁定后不再接受新成员（已在房间的人掉线重连不受影响）。
	Locked   bool `json:"locked"`
	Capacity int  `json:"capacity"`
}

// MemberConn 成员当前的连接；为空表示离线。
type MemberConn struct {
	ID       string `json:"id"`
	Instance string `json:"instance"`
	Since    int64  `json:"since"`
}

// MemberStatus 成员自报的播放状态。
type MemberStatus struct {
	State      string `json:"state"`
	ItemID     string `json:"itemId,omitempty"`
	PositionMs int64  `json:"positionMs,omitempty"`
	At         int64  `json:"at,omitempty"`
}

// Member 房间成员。
type Member struct {
	UserID       int64        `json:"userId"`
	Nickname     string       `json:"nickname"`
	Avatar       string       `json:"avatar,omitempty"`
	Platform     string       `json:"platform,omitempty"`
	Role         string       `json:"role"`
	CanControl   bool         `json:"canControl"`
	JoinedAt     int64        `json:"joinedAt"`
	Conn         *MemberConn  `json:"conn,omitempty"`
	OfflineSince int64        `json:"offlineSince,omitempty"`
	Status       MemberStatus `json:"status"`
	Rev          int64        `json:"rev"`
}

// Online 是否在线。
func (m *Member) Online() bool { return m.Conn != nil }

// Request 待房主处理的点歌。
type Request struct {
	ID       string `json:"id"`
	Track    Track  `json:"track"`
	By       int64  `json:"by"`
	Nickname string `json:"nickname"`
	At       int64  `json:"at"`
}

// Room 房间的完整状态（存储形态）。
type Room struct {
	ID          string    `json:"id"`
	AppKey      string    `json:"appKey"`
	Code        string    `json:"code"`
	HostID      int64     `json:"hostId"`
	Settings    Settings  `json:"settings"`
	SettingsRev int64     `json:"settingsRev"`
	Members     []*Member `json:"members"`
	Banned      []int64   `json:"banned,omitempty"`
	Queue       []Track   `json:"queue"`
	QueueRev    int64     `json:"queueRev"`
	Playback    Playback  `json:"playback"`
	Requests    []Request `json:"requests"`
	RequestsRev int64     `json:"requestsRev"`
	CreatedAt   int64     `json:"createdAt"`
	UpdatedAt   int64     `json:"updatedAt"`
	// EmptySince 最后一个在线成员离线的时刻；有人在线时为 0。
	EmptySince int64 `json:"emptySince,omitempty"`
	Rev        int64 `json:"rev"`
}

// Member 按用户找成员。
func (r *Room) Member(userID int64) *Member {
	for _, member := range r.Members {
		if member.UserID == userID {
			return member
		}
	}
	return nil
}

// Host 房主（可能为空：房主刚被移除、转交尚未发生的瞬间）。
func (r *Room) Host() *Member { return r.Member(r.HostID) }

// OnlineCount 在线成员数。
func (r *Room) OnlineCount() int {
	count := 0
	for _, member := range r.Members {
		if member.Online() {
			count++
		}
	}
	return count
}

// Item 按队列项标识找歌。
func (r *Room) Item(itemID string) (Track, bool) {
	for _, item := range r.Queue {
		if item.ItemID == itemID {
			return item, true
		}
	}
	return Track{}, false
}

// IsBanned 是否被移出过（房间存续期间不能再加入）。
func (r *Room) IsBanned(userID int64) bool {
	for _, id := range r.Banned {
		if id == userID {
			return true
		}
	}
	return false
}

// CanControl 某人此刻能否发播放指令。
func (r *Room) CanControl(userID int64) bool {
	if userID == r.HostID {
		return true
	}
	member := r.Member(userID)
	if member == nil {
		return false
	}
	return member.CanControl || r.Settings.ControlMode == ControlAll
}

// PositionAt 推算服务端时刻 at 的播放位置。
func (p Playback) PositionAt(at int64) int64 {
	if !p.Playing || at <= p.At {
		return p.PositionMs
	}
	rate := p.Rate
	if rate <= 0 {
		rate = 1
	}
	return p.PositionMs + int64(float64(at-p.At)*rate)
}

// ChatMessage 一条聊天。
type ChatMessage struct {
	ID       string `json:"id"`
	UserID   int64  `json:"userId"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Text     string `json:"text"`
	At       int64  `json:"at"`
}

// Reaction 一次表情互动（不留存）。
type Reaction struct {
	UserID int64  `json:"userId"`
	Emoji  string `json:"emoji"`
	At     int64  `json:"at"`
}

// Control 有控制权的成员发给房主执行的播放指令。
type Control struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	ItemID     string `json:"itemId,omitempty"`
	PositionMs int64  `json:"positionMs,omitempty"`
	By         int64  `json:"by"`
	Nickname   string `json:"nickname"`
	At         int64  `json:"at"`
}

// 播放指令。
const (
	ActionPlay     = "play"
	ActionPause    = "pause"
	ActionSeek     = "seek"
	ActionNext     = "next"
	ActionPrevious = "previous"
	ActionJump     = "jump"
)

// Clock 可替换的时钟（测试用）。
type Clock func() time.Time

// Millis 毫秒时间戳。
func (c Clock) Millis() int64 { return c().UnixMilli() }
