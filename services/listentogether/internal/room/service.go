package room

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Options 房间规则的可调参数（来自服务配置）。
type Options struct {
	Capacity    int
	CapacityMax int
	QueueLimit  int
	ChatHistory int
	MemberGrace time.Duration
	IdleTTL     time.Duration
	Instance    string
}

const (
	maxPendingRequests     = 30
	maxRequestsPerMember   = 3
	maxPlaybackClockSkewMs = 10_000
	createCodeAttempts     = 12
)

// Actor 发起操作的一条连接。
type Actor struct {
	Identity
	ConnID string
}

// Service 房间用例。所有改动都走 mutate：存储里乐观并发改完，再把事件发到总线。
type Service struct {
	repo  Repository
	bus   Bus
	clock Clock
	opt   Options
	log   *zap.Logger
}

// NewService 构造房间服务。
func NewService(repo Repository, bus Bus, clock Clock, opt Options, log *zap.Logger) *Service {
	if clock == nil {
		clock = time.Now
	}
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{repo: repo, bus: bus, clock: clock, opt: opt, log: log}
}

// Options 当前参数。
func (s *Service) Options() Options { return s.opt }

// ── 进出房间 ──

// SettingsInput 建房或改设置时客户端给的内容；指针为空表示不改。
type SettingsInput struct {
	Name          *string `json:"name,omitempty"`
	ControlMode   *string `json:"controlMode,omitempty"`
	AllowRequests *bool   `json:"allowRequests,omitempty"`
	Locked        *bool   `json:"locked,omitempty"`
	Capacity      *int    `json:"capacity,omitempty"`
}

func (s *Service) applySettings(current Settings, in SettingsInput) (Settings, error) {
	if in.Name != nil {
		current.Name = CleanText(*in.Name, maxNameRunes)
	}
	if in.ControlMode != nil {
		mode := strings.TrimSpace(*in.ControlMode)
		if mode != ControlHost && mode != ControlAll {
			return current, Invalid("控制模式无效")
		}
		current.ControlMode = mode
	}
	if in.AllowRequests != nil {
		current.AllowRequests = *in.AllowRequests
	}
	if in.Locked != nil {
		current.Locked = *in.Locked
	}
	if in.Capacity != nil {
		if *in.Capacity < 2 || *in.Capacity > s.opt.CapacityMax {
			return current, Invalid(fmt.Sprintf("人数上限须在 2–%d", s.opt.CapacityMax))
		}
		current.Capacity = *in.Capacity
	}
	return current, nil
}

// Create 建房并成为房主。已在别的房间时先离开那个房间。
func (s *Service) Create(ctx context.Context, a Actor, in SettingsInput) (*Snapshot, error) {
	settings, err := s.applySettings(Settings{ControlMode: ControlHost, AllowRequests: true, Capacity: s.opt.Capacity}, in)
	if err != nil {
		return nil, err
	}
	if settings.Name == "" {
		settings.Name = CleanText(a.Nickname, maxNameRunes-4) + " 的房间"
	}
	if err := s.leaveCurrent(ctx, a, ""); err != nil {
		return nil, err
	}
	now := s.clock.Millis()
	host := s.newMember(a, RoleHost, now)
	host.CanControl = true
	r := &Room{
		ID: newID(10), AppKey: a.AppKey, HostID: a.UserID, Settings: settings,
		Members: []*Member{host}, Queue: []Track{}, Requests: []Request{},
		Playback:  Playback{Rate: 1, At: now},
		CreatedAt: now, UpdatedAt: now, Rev: 1, SettingsRev: 1,
	}
	host.Rev = r.Rev
	for attempt := 0; ; attempt++ {
		if attempt >= createCodeAttempts {
			return nil, ErrCodeExhausted
		}
		r.Code = newCode()
		err := s.repo.CreateRoom(ctx, r)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrCodeTaken) {
			return nil, err
		}
	}
	if err := s.repo.SetUserRoom(ctx, a.AppKey, a.UserID, r.ID); err != nil {
		return nil, err
	}
	snapshot := r.Snapshot(nil)
	return &snapshot, nil
}

// Join 凭房间号加入。已经是成员（例如换了设备）时只接上连接。
func (s *Service) Join(ctx context.Context, a Actor, code string) (*Snapshot, error) {
	code = strings.TrimSpace(code)
	if !ValidCode(code) {
		return nil, ErrRoomNotFound
	}
	roomID, err := s.repo.FindByCode(ctx, a.AppKey, code)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrRoomNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.leaveCurrent(ctx, a, roomID); err != nil {
		return nil, err
	}
	var events []Event
	r, err := s.mutate(ctx, a.AppKey, roomID, func(r *Room) ([]Event, string, error) {
		if member := r.Member(a.UserID); member != nil {
			return s.connect(r, member, a), "", nil
		}
		if r.IsBanned(a.UserID) {
			return nil, "", ErrBanned
		}
		if r.Settings.Locked {
			return nil, "", ErrRoomLocked
		}
		if len(r.Members) >= r.Settings.Capacity {
			return nil, "", ErrRoomFull
		}
		now := s.clock.Millis()
		member := s.newMember(a, RoleMember, now)
		r.Members = append(r.Members, member)
		r.EmptySince = 0
		bump(r, now)
		member.Rev = r.Rev
		return []Event{memberEvent(EventMemberJoined, member, r.Rev)}, "", nil
	}, &events)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetUserRoom(ctx, a.AppKey, a.UserID, r.ID); err != nil {
		return nil, err
	}
	return s.snapshot(ctx, r)
}

// Attach 新连接接回用户所在的房间（重连、换设备）；不在任何房间返回 nil。
func (s *Service) Attach(ctx context.Context, a Actor) (*Snapshot, error) {
	roomID, err := s.repo.UserRoom(ctx, a.AppKey, a.UserID)
	if err != nil || roomID == "" {
		return nil, err
	}
	r, err := s.mutate(ctx, a.AppKey, roomID, func(r *Room) ([]Event, string, error) {
		member := r.Member(a.UserID)
		if member == nil {
			return nil, "", ErrNotInRoom
		}
		return s.connect(r, member, a), "", nil
	}, nil)
	if err != nil {
		if AsError(err) != nil {
			_ = s.repo.ClearUserRoom(ctx, a.AppKey, a.UserID, roomID)
			return nil, nil
		}
		return nil, err
	}
	return s.snapshot(ctx, r)
}

// Sync 重新取一份完整状态（客户端觉得自己不同步时）。
func (s *Service) Sync(ctx context.Context, a Actor) (*Snapshot, error) {
	r, err := s.current(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.snapshot(ctx, r)
}

// Leave 主动离开。房主离开时转交给下一位；最后一个人离开时房间解散。
func (s *Service) Leave(ctx context.Context, a Actor) error {
	roomID, err := s.repo.UserRoom(ctx, a.AppKey, a.UserID)
	if err != nil {
		return err
	}
	if roomID == "" {
		return nil
	}
	return s.removeMember(ctx, a.AppKey, roomID, a.UserID, LeftLeave, false)
}

// Detach 连接断开：成员转为离线，席位保留 MemberGrace，期间重连无感恢复。
// 只有断开的正是成员当前那条连接时才生效（被新连接顶替的旧连接断开不影响）。
func (s *Service) Detach(ctx context.Context, a Actor, roomID string) error {
	if roomID == "" {
		return nil
	}
	_, err := s.mutate(ctx, a.AppKey, roomID, func(r *Room) ([]Event, string, error) {
		member := r.Member(a.UserID)
		if member == nil || member.Conn == nil || member.Conn.ID != a.ConnID {
			return nil, "", nil
		}
		return s.disconnect(r, member), "", nil
	}, nil)
	if AsError(err) != nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// Close 房主解散房间。
func (s *Service) Close(ctx context.Context, a Actor) error {
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	_, err = s.mutate(ctx, a.AppKey, r.ID, func(r *Room) ([]Event, string, error) {
		if r.HostID != a.UserID {
			return nil, "", ErrNotHost
		}
		return nil, ClosedByHost, nil
	}, nil)
	return err
}

// ── 房主管理 ──

// UpdateSettings 改房间设置。
func (s *Service) UpdateSettings(ctx context.Context, a Actor, in SettingsInput) error {
	return s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		settings, err := s.applySettings(r.Settings, in)
		if err != nil {
			return nil, "", err
		}
		if settings.Name == "" {
			settings.Name = r.Settings.Name
		}
		if settings.Capacity < len(r.Members) && in.Capacity != nil {
			return nil, "", Invalid("人数上限不能小于当前人数")
		}
		r.Settings = settings
		bump(r, s.clock.Millis())
		r.SettingsRev = r.Rev
		return []Event{roomUpdated(r)}, "", nil
	})
}

// Kick 移出成员，房间存续期间不能再加入。
func (s *Service) Kick(ctx context.Context, a Actor, userID int64) error {
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	if r.HostID != a.UserID {
		return ErrNotHost
	}
	if userID == a.UserID {
		return Invalid("不能移出自己")
	}
	return s.removeMember(ctx, a.AppKey, r.ID, userID, LeftKick, true)
}

// Transfer 把房主转给另一位成员。
func (s *Service) Transfer(ctx context.Context, a Actor, userID int64) error {
	return s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		target := r.Member(userID)
		if target == nil || userID == a.UserID {
			return nil, "", ErrMemberNotFound
		}
		return s.setHost(r, target), "", nil
	})
}

// Grant 给成员（或收回）控制权。
func (s *Service) Grant(ctx context.Context, a Actor, userID int64, canControl bool) error {
	return s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		target := r.Member(userID)
		if target == nil || target.Role == RoleHost {
			return nil, "", ErrMemberNotFound
		}
		target.CanControl = canControl
		bump(r, s.clock.Millis())
		target.Rev = r.Rev
		return []Event{memberEvent(EventMemberUpdated, target, r.Rev)}, "", nil
	})
}

// ── 播放与队列（房主权威）──

// SetQueue 房主同步自己的队列（整体替换）。
func (s *Service) SetQueue(ctx context.Context, a Actor, items []Track) error {
	if len(items) > s.opt.QueueLimit {
		return ErrQueueTooLong
	}
	clean := make([]Track, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		track, err := NormalizeTrack(item, true)
		if err != nil {
			return err
		}
		if _, dup := seen[track.ItemID]; dup {
			return Invalid("队列项标识重复")
		}
		seen[track.ItemID] = struct{}{}
		clean = append(clean, track)
	}
	return s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		if slices.Equal(r.Queue, clean) {
			return nil, "", nil
		}
		r.Queue = clean
		bump(r, s.clock.Millis())
		r.QueueRev = r.Rev
		view := r.QueueView()
		return []Event{{Kind: EventQueue, Rev: r.Rev, Queue: &view}}, "", nil
	})
}

// PlaybackInput 房主上报的播放状态。At 是房主估算的服务端时刻（0 表示「就是现在」）。
type PlaybackInput struct {
	ItemID     string  `json:"itemId"`
	PositionMs int64   `json:"positionMs"`
	Playing    bool    `json:"playing"`
	Rate       float64 `json:"rate"`
	At         int64   `json:"at"`
}

// UpdatePlayback 房主上报播放状态。
func (s *Service) UpdatePlayback(ctx context.Context, a Actor, in PlaybackInput) error {
	in.ItemID = strings.TrimSpace(in.ItemID)
	if in.Rate == 0 {
		in.Rate = 1
	}
	if in.Rate < 0.25 || in.Rate > 4 || in.PositionMs < 0 {
		return Invalid("播放状态无效")
	}
	return s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		now := s.clock.Millis()
		at := in.At
		if at <= 0 || at > now+2000 || at < now-maxPlaybackClockSkewMs {
			at = now
		}
		position := in.PositionMs
		if in.ItemID != "" {
			item, ok := r.Item(in.ItemID)
			if !ok {
				return nil, "", ErrUnknownItem
			}
			if item.DurationMs > 0 && position > item.DurationMs+5000 {
				position = item.DurationMs
			}
		} else {
			in.Playing = false
			position = 0
		}
		r.Playback = Playback{ItemID: in.ItemID, PositionMs: position, Playing: in.Playing, Rate: in.Rate, At: at, By: a.UserID}
		bump(r, now)
		r.Playback.Rev = r.Rev
		playback := r.Playback
		return []Event{{Kind: EventPlayback, Rev: r.Rev, Playback: &playback}}, "", nil
	})
}

// Request 成员点歌，等房主处理。
func (s *Service) Request(ctx context.Context, a Actor, track Track) error {
	track, err := NormalizeTrack(track, false)
	if err != nil {
		return err
	}
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	_, err = s.mutate(ctx, a.AppKey, r.ID, func(r *Room) ([]Event, string, error) {
		if r.Member(a.UserID) == nil {
			return nil, "", ErrNotInRoom
		}
		if !r.Settings.AllowRequests && !r.CanControl(a.UserID) {
			return nil, "", ErrRequestsOff
		}
		mine := 0
		for _, request := range r.Requests {
			if request.By == a.UserID {
				mine++
			}
		}
		if mine >= maxRequestsPerMember && a.UserID != r.HostID {
			return nil, "", ErrTooManyRequests
		}
		now := s.clock.Millis()
		track.RequestedBy = a.UserID
		r.Requests = append(r.Requests, Request{ID: newID(8), Track: track, By: a.UserID, Nickname: a.Nickname, At: now})
		if len(r.Requests) > maxPendingRequests {
			r.Requests = r.Requests[len(r.Requests)-maxPendingRequests:]
		}
		bump(r, now)
		r.RequestsRev = r.Rev
		view := r.RequestView()
		return []Event{{Kind: EventRequests, Rev: r.Rev, Requests: &view}}, "", nil
	}, nil)
	return err
}

// ResolveRequest 房主处理点歌。接受时由房主客户端把歌插进自己的队列，随后的队列同步自然带上它。
func (s *Service) ResolveRequest(ctx context.Context, a Actor, requestID string) (*Request, error) {
	var resolved *Request
	err := s.hostMutate(ctx, a, func(r *Room) ([]Event, string, error) {
		resolved = nil
		for i, request := range r.Requests {
			if request.ID == requestID {
				found := request
				resolved = &found
				r.Requests = append(r.Requests[:i:i], r.Requests[i+1:]...)
				break
			}
		}
		if resolved == nil {
			return nil, "", ErrRequestNotFound
		}
		bump(r, s.clock.Millis())
		r.RequestsRev = r.Rev
		view := r.RequestView()
		return []Event{{Kind: EventRequests, Rev: r.Rev, Requests: &view}}, "", nil
	})
	return resolved, err
}

// ControlInput 有控制权的成员发的播放指令。
type ControlInput struct {
	Action     string `json:"action"`
	ItemID     string `json:"itemId"`
	PositionMs int64  `json:"positionMs"`
}

// SendControl 把指令转给房主执行。房主自己不需要这条路（直接操作本地播放器）。
func (s *Service) SendControl(ctx context.Context, a Actor, in ControlInput) error {
	if !validAction(in.Action) || in.PositionMs < 0 {
		return Invalid("播放指令无效")
	}
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	if !r.CanControl(a.UserID) {
		return ErrNoControl
	}
	if in.Action == ActionJump {
		if _, ok := r.Item(in.ItemID); !ok {
			return ErrUnknownItem
		}
	}
	host := r.Host()
	if host == nil || !host.Online() {
		return ErrHostOffline
	}
	control := &Control{
		ID: newID(8), Action: in.Action, ItemID: in.ItemID, PositionMs: in.PositionMs,
		By: a.UserID, Nickname: a.Nickname, At: s.clock.Millis(),
	}
	return s.publish(ctx, r, Event{Kind: EventControl, Target: r.HostID, Control: control})
}

// ── 社交 ──

// Chat 发一条聊天。
func (s *Service) Chat(ctx context.Context, a Actor, text string) (*ChatMessage, error) {
	text = CleanText(text, maxTextRunes)
	if text == "" {
		return nil, Invalid("消息不能为空")
	}
	r, err := s.current(ctx, a)
	if err != nil {
		return nil, err
	}
	msg := ChatMessage{ID: newID(8), UserID: a.UserID, Nickname: a.Nickname, Avatar: a.Avatar, Text: text, At: s.clock.Millis()}
	if s.opt.ChatHistory > 0 {
		if err := s.repo.AppendChat(ctx, a.AppKey, r.ID, msg, s.opt.ChatHistory); err != nil {
			return nil, err
		}
	}
	return &msg, s.publish(ctx, r, Event{Kind: EventChat, Chat: &msg})
}

// React 发一个表情。
func (s *Service) React(ctx context.Context, a Actor, emoji string) error {
	emoji, err := NormalizeEmoji(emoji)
	if err != nil {
		return err
	}
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	return s.publish(ctx, r, Event{Kind: EventReaction, Reaction: &Reaction{UserID: a.UserID, Emoji: emoji, At: s.clock.Millis()}})
}

// ReportStatus 成员上报自己的播放状态（缓冲中 / 播放中…），供房间里显示。
func (s *Service) ReportStatus(ctx context.Context, a Actor, status MemberStatus) error {
	if !validState(status.State) {
		return Invalid("状态无效")
	}
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	if member := r.Member(a.UserID); member != nil && member.Status.State == status.State && member.Status.ItemID == status.ItemID {
		return nil
	}
	_, err = s.mutate(ctx, a.AppKey, r.ID, func(r *Room) ([]Event, string, error) {
		member := r.Member(a.UserID)
		if member == nil {
			return nil, "", ErrNotInRoom
		}
		now := s.clock.Millis()
		status.At = now
		member.Status = status
		bump(r, now)
		member.Rev = r.Rev
		return []Event{memberEvent(EventMemberUpdated, member, r.Rev)}, "", nil
	}, nil)
	return err
}

// ── 管理与巡检 ──

// Get 读房间（管理端）。
func (s *Service) Get(ctx context.Context, app, roomID string) (*Room, []ChatMessage, error) {
	r, err := s.repo.LoadRoom(ctx, app, roomID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, ErrRoomNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	chat, err := s.repo.RecentChat(ctx, app, roomID, s.opt.ChatHistory)
	return r, chat, err
}

// AdminClose 管理员解散房间。
func (s *Service) AdminClose(ctx context.Context, app, roomID string) error {
	_, err := s.mutate(ctx, app, roomID, func(r *Room) ([]Event, string, error) {
		return nil, ClosedByAdmin, nil
	}, nil)
	return err
}

// AdminKick 管理员移出成员。
func (s *Service) AdminKick(ctx context.Context, app, roomID string, userID int64) error {
	return s.removeMember(ctx, app, roomID, userID, LeftKick, true)
}

// RemoveUser 撤销某人：退出所在房间（封禁、强制下线、失去权益）。
func (s *Service) RemoveUser(ctx context.Context, app string, userID int64) error {
	roomID, err := s.repo.UserRoom(ctx, app, userID)
	if err != nil || roomID == "" {
		return err
	}
	return s.removeMember(ctx, app, roomID, userID, LeftRevoked, false)
}

// PublishReplace 通知其它实例：这个用户有了新连接，旧的断开。
func (s *Service) PublishReplace(ctx context.Context, a Actor) error {
	return s.bus.PublishUser(ctx, UserSignal{Kind: SignalReplace, AppKey: a.AppKey, UserID: a.UserID, ConnID: a.ConnID})
}

// RevokeOnce 撤销通知会到达每个实例，只让抢到锁的那个去改房间（锁不释放，靠过期去重）。
func (s *Service) RevokeOnce(ctx context.Context, app string, userID int64) error {
	_, ok, err := s.repo.TryLock(ctx, fmt.Sprintf("revoke:%s:%d", app, userID), 10*time.Second)
	if err != nil || !ok {
		return err
	}
	return s.RemoveUser(ctx, app, userID)
}

// Sweep 巡检一个应用的全部房间（只由抢到锁的实例执行）：
// 把挂在已死实例上的连接判为离线、移出超过宽限期的离线成员、解散长时间没人在线的房间。
func (s *Service) Sweep(ctx context.Context, alive map[string]bool) error {
	apps, err := s.repo.Apps(ctx)
	if err != nil {
		return err
	}
	for _, app := range apps {
		offset := 0
		for {
			ids, total, err := s.repo.ListRooms(ctx, app, offset, 200)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if err := s.sweepRoom(ctx, app, id, alive); err != nil && ctx.Err() == nil {
					s.log.Warn("sweep room failed", zap.String("app", app), zap.String("room", id), zap.Error(err))
				}
			}
			offset += len(ids)
			if len(ids) == 0 || offset >= total {
				break
			}
		}
	}
	return nil
}

func (s *Service) sweepRoom(ctx context.Context, app, roomID string, alive map[string]bool) error {
	var dropped []int64
	_, err := s.mutate(ctx, app, roomID, func(r *Room) ([]Event, string, error) {
		dropped = dropped[:0]
		now := s.clock.Millis()
		var events []Event
		for _, member := range r.Members {
			if member.Conn != nil && !alive[member.Conn.Instance] {
				events = append(events, s.disconnect(r, member)...)
			}
		}
		grace := s.opt.MemberGrace.Milliseconds()
		for _, member := range slices.Clone(r.Members) {
			if member.Online() || member.OfflineSince == 0 || now-member.OfflineSince < grace {
				continue
			}
			// 只剩这一个人时不移出：房间是否解散交给空闲判定
			if len(r.Members) == 1 {
				break
			}
			evs, closeReason := s.dropMember(r, member.UserID, LeftTimeout, false)
			events = append(events, evs...)
			dropped = append(dropped, member.UserID)
			if closeReason != "" {
				return events, closeReason, nil
			}
		}
		if r.OnlineCount() == 0 {
			if r.EmptySince == 0 {
				r.EmptySince = now
			}
			if now-r.EmptySince >= s.opt.IdleTTL.Milliseconds() {
				return events, ClosedIdle, nil
			}
		}
		return events, "", nil
	}, nil)
	if errors.Is(err, ErrNotFound) || AsError(err) == ErrRoomNotFound {
		return nil
	}
	for _, userID := range dropped {
		if clearErr := s.repo.ClearUserRoom(ctx, app, userID, roomID); clearErr != nil && err == nil {
			err = clearErr
		}
	}
	return err
}

// ── 内部 ──

func (s *Service) newMember(a Actor, role string, now int64) *Member {
	return &Member{
		UserID: a.UserID, Nickname: CleanText(a.Nickname, maxFieldRunes), Avatar: cleanCover(a.Avatar),
		Platform: CleanText(a.Platform, 16), Role: role, JoinedAt: now,
		Conn:   &MemberConn{ID: a.ConnID, Instance: s.opt.Instance, Since: now},
		Status: MemberStatus{State: StateIdle},
	}
}

// connect 把成员挂到这条连接上（重连 / 换设备），资料以最新票据为准。
func (s *Service) connect(r *Room, member *Member, a Actor) []Event {
	now := s.clock.Millis()
	member.Conn = &MemberConn{ID: a.ConnID, Instance: s.opt.Instance, Since: now}
	member.OfflineSince = 0
	member.Nickname = CleanText(a.Nickname, maxFieldRunes)
	member.Avatar = cleanCover(a.Avatar)
	member.Platform = CleanText(a.Platform, 16)
	r.EmptySince = 0
	bump(r, now)
	member.Rev = r.Rev
	return []Event{memberEvent(EventMemberUpdated, member, r.Rev)}
}

func (s *Service) disconnect(r *Room, member *Member) []Event {
	now := s.clock.Millis()
	member.Conn = nil
	member.OfflineSince = now
	member.Status = MemberStatus{State: StateIdle}
	if r.OnlineCount() == 0 && r.EmptySince == 0 {
		r.EmptySince = now
	}
	bump(r, now)
	member.Rev = r.Rev
	events := []Event{memberEvent(EventMemberUpdated, member, r.Rev)}
	// 房主掉线、有人在线时先不转交：给房主 MemberGrace 的时间回来，过了再由巡检转交
	return events
}

func (s *Service) setHost(r *Room, target *Member) []Event {
	now := s.clock.Millis()
	var events []Event
	if old := r.Host(); old != nil && old != target {
		old.Role = RoleMember
		old.CanControl = false
		bump(r, now)
		old.Rev = r.Rev
		events = append(events, memberEvent(EventMemberUpdated, old, r.Rev))
	}
	target.Role = RoleHost
	target.CanControl = true
	r.HostID = target.UserID
	r.Requests = r.Requests[:0]
	bump(r, now)
	target.Rev = r.Rev
	r.SettingsRev = r.Rev
	r.RequestsRev = r.Rev
	view := r.RequestView()
	return append(events,
		memberEvent(EventMemberUpdated, target, r.Rev),
		roomUpdated(r),
		Event{Kind: EventRequests, Rev: r.Rev, Requests: &view},
	)
}

// dropMember 从房间里去掉一个人；房主走了就转交，没人了就解散（返回解散原因）。
func (s *Service) dropMember(r *Room, userID int64, reason string, ban bool) ([]Event, string) {
	index := slices.IndexFunc(r.Members, func(m *Member) bool { return m.UserID == userID })
	if index < 0 {
		return nil, ""
	}
	now := s.clock.Millis()
	r.Members = slices.Delete(r.Members, index, index+1)
	if ban && !r.IsBanned(userID) {
		r.Banned = append(r.Banned, userID)
	}
	kept := r.Requests[:0]
	for _, request := range r.Requests {
		if request.By != userID {
			kept = append(kept, request)
		}
	}
	r.Requests = kept
	bump(r, now)
	events := []Event{{Kind: EventMemberLeft, Rev: r.Rev, UserID: userID, Reason: reason}}
	if len(r.Members) == 0 {
		return events, ClosedEmpty
	}
	if r.HostID == userID {
		events = append(events, s.setHost(r, nextHost(r.Members))...)
	}
	if r.OnlineCount() == 0 && r.EmptySince == 0 {
		r.EmptySince = now
	}
	return events, ""
}

func nextHost(members []*Member) *Member {
	ordered := slices.Clone(members)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Online() != ordered[j].Online() {
			return ordered[i].Online()
		}
		return ordered[i].JoinedAt < ordered[j].JoinedAt
	})
	return ordered[0]
}

func (s *Service) removeMember(ctx context.Context, app, roomID string, userID int64, reason string, ban bool) error {
	_, err := s.mutate(ctx, app, roomID, func(r *Room) ([]Event, string, error) {
		if r.Member(userID) == nil {
			return nil, "", ErrMemberNotFound
		}
		events, closeReason := s.dropMember(r, userID, reason, ban)
		return events, closeReason, nil
	}, nil)
	if errors.Is(err, ErrMemberNotFound) || errors.Is(err, ErrRoomNotFound) {
		err = nil
	}
	if clearErr := s.repo.ClearUserRoom(ctx, app, userID, roomID); err == nil {
		err = clearErr
	}
	return err
}

// leaveCurrent 进新房间前离开旧房间（except 是将要进的那个）。
func (s *Service) leaveCurrent(ctx context.Context, a Actor, except string) error {
	current, err := s.repo.UserRoom(ctx, a.AppKey, a.UserID)
	if err != nil || current == "" || current == except {
		return err
	}
	return s.removeMember(ctx, a.AppKey, current, a.UserID, LeftLeave, false)
}

func (s *Service) current(ctx context.Context, a Actor) (*Room, error) {
	roomID, err := s.repo.UserRoom(ctx, a.AppKey, a.UserID)
	if err != nil {
		return nil, err
	}
	if roomID == "" {
		return nil, ErrNotInRoom
	}
	r, err := s.repo.LoadRoom(ctx, a.AppKey, roomID)
	if errors.Is(err, ErrNotFound) {
		_ = s.repo.ClearUserRoom(ctx, a.AppKey, a.UserID, roomID)
		return nil, ErrNotInRoom
	}
	if err != nil {
		return nil, err
	}
	if r.Member(a.UserID) == nil {
		_ = s.repo.ClearUserRoom(ctx, a.AppKey, a.UserID, roomID)
		return nil, ErrNotInRoom
	}
	return r, nil
}

func (s *Service) hostMutate(ctx context.Context, a Actor, fn func(*Room) ([]Event, string, error)) error {
	r, err := s.current(ctx, a)
	if err != nil {
		return err
	}
	_, err = s.mutate(ctx, a.AppKey, r.ID, func(r *Room) ([]Event, string, error) {
		if r.HostID != a.UserID {
			return nil, "", ErrNotHost
		}
		return fn(r)
	}, nil)
	return err
}

// mutate 在存储里改房间、成功后发事件；fn 返回非空的解散原因时删除房间并通知所有人。
// out 不为空时收集本次发出的事件（测试与调用方需要时用）。
func (s *Service) mutate(ctx context.Context, app, roomID string, fn func(*Room) ([]Event, string, error), out *[]Event) (*Room, error) {
	var events []Event
	var closeReason string
	r, err := s.repo.UpdateRoom(ctx, app, roomID, func(r *Room) error {
		var err error
		events, closeReason, err = fn(r)
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return nil, ErrRoomNotFound
	}
	if err != nil {
		return nil, err
	}
	if closeReason != "" {
		return r, s.destroy(ctx, r, closeReason, events)
	}
	for _, event := range events {
		if err := s.publish(ctx, r, event); err != nil {
			s.log.Warn("publish room event failed", zap.String("room", r.ID), zap.String("kind", event.Kind), zap.Error(err))
		}
	}
	if out != nil {
		*out = events
	}
	return r, nil
}

func (s *Service) destroy(ctx context.Context, r *Room, reason string, before []Event) error {
	if err := s.repo.DeleteRoom(ctx, r); err != nil {
		return err
	}
	for _, event := range before {
		_ = s.publish(ctx, r, event)
	}
	return s.publish(ctx, r, Event{Kind: EventRoomClosed, Rev: r.Rev + 1, Reason: reason})
}

func (s *Service) publish(ctx context.Context, r *Room, event Event) error {
	return s.bus.PublishRoom(ctx, r.AppKey, r.ID, event)
}

func (s *Service) snapshot(ctx context.Context, r *Room) (*Snapshot, error) {
	var chat []ChatMessage
	if s.opt.ChatHistory > 0 {
		var err error
		chat, err = s.repo.RecentChat(ctx, r.AppKey, r.ID, s.opt.ChatHistory)
		if err != nil {
			s.log.Warn("load chat failed", zap.String("room", r.ID), zap.Error(err))
		}
	}
	snapshot := r.Snapshot(chat)
	return &snapshot, nil
}

func bump(r *Room, now int64) {
	r.Rev++
	r.UpdatedAt = now
}

var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

func newID(size int) string {
	buf := make([]byte, size)
	_, _ = rand.Read(buf)
	return idEncoding.EncodeToString(buf)
}

func newCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	return fmt.Sprintf("%06d", n.Int64()+100000)
}
