package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"aegis/pkg/listenticket"
	"aegis/services/listentogether/internal/room"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	helloTimeout   = 10 * time.Second
	sendBuffer     = 256
	maxFrameBytes  = 256 << 10
	commandTimeout = 8 * time.Second
)

// Conn 一条已握手的客户端连接。命令在读循环里逐条同步处理，同一连接上的命令严格按到达顺序生效。
type Conn struct {
	id       string
	ws       *websocket.Conn
	server   *Server
	log      *zap.Logger
	outbox   chan outbound
	done     chan struct{}
	once     sync.Once
	closeMu  sync.Mutex
	byeFrame *outbound

	identity room.Identity
	// expires 票据到期时刻（毫秒）；续期后更新。只在读循环里改，看门狗读取时加锁。
	expiresMu sync.Mutex
	expires   int64

	// roomID 只由 Hub 在持锁时读写。
	roomID string

	limiter     *rate.Limiter
	chatLimiter *rate.Limiter
}

func (c *Conn) actor() room.Actor {
	return room.Actor{Identity: c.identity, ConnID: c.id}
}

// send 非阻塞投递；缓冲写满说明客户端读得太慢，断开它（重连后取快照补齐），不拖累整个房间。
func (c *Conn) send(frame outbound) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.outbox <- frame:
	default:
		c.server.metrics.SlowConsumers.Inc()
		c.bye(ByeSlowConsumer, "网络太慢，正在重新连接", true)
	}
}

func (c *Conn) reply(id string, data any, err error) {
	if id == "" {
		return
	}
	if err != nil {
		ok := false
		c.send(outbound{Type: FrameReply, ID: id, OK: &ok, Error: toWire(err)})
		return
	}
	ok := true
	c.send(outbound{Type: FrameReply, ID: id, OK: &ok, Data: data})
}

func toWire(err error) *wireError {
	if domain := room.AsError(err); domain != nil {
		return &wireError{Code: domain.Code, Message: domain.Message}
	}
	return &wireError{Code: "internal", Message: "服务暂时不可用，请稍后再试"}
}

// bye 告知原因后关闭连接（只生效一次）。
func (c *Conn) bye(code, message string, reconnect bool) {
	c.closeMu.Lock()
	if c.byeFrame == nil {
		c.byeFrame = &outbound{Type: FrameBye, Data: byeData{Code: code, Message: message, Reconnect: reconnect}}
	}
	c.closeMu.Unlock()
	c.close()
}

func (c *Conn) close() {
	c.once.Do(func() { close(c.done) })
}

// writeLoop 唯一的写协程：发帧、定时 ping，关闭前把 bye 帧与关闭帧写出去。
func (c *Conn) writeLoop() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case frame := <-c.outbox:
			if err := c.write(frame); err != nil {
				c.close()
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				c.close()
				return
			}
			if c.ticketLapsed() {
				c.bye(ByeTicketExpired, "登录凭证已过期，请重新进入", false)
			}
		case <-c.done:
			c.flushAndClose()
			return
		}
	}
}

func (c *Conn) write(frame outbound) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		c.log.Warn("encode frame failed", zap.Error(err))
		return nil
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(websocket.TextMessage, raw)
}

func (c *Conn) flushAndClose() {
	// 已排队的回复与事件尽量送达，再发 bye
	for {
		select {
		case frame := <-c.outbox:
			if c.write(frame) != nil {
				return
			}
			continue
		default:
		}
		break
	}
	c.closeMu.Lock()
	frame := c.byeFrame
	c.closeMu.Unlock()
	closeCode := websocket.CloseNormalClosure
	reason := ""
	if frame != nil {
		_ = c.write(*frame)
		data := frame.Data.(byeData)
		reason = data.Code
		if data.Reconnect {
			closeCode = websocket.CloseGoingAway
		}
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(time.Second))
	_ = c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, reason))
}

func (c *Conn) setExpires(at int64) {
	c.expiresMu.Lock()
	c.expires = at
	c.expiresMu.Unlock()
}

func (c *Conn) ticketLapsed() bool {
	c.expiresMu.Lock()
	expires := c.expires
	c.expiresMu.Unlock()
	return time.Now().UnixMilli() > expires+c.server.cfg.TicketGrace.Milliseconds()
}

// readLoop 读命令并逐条处理，直到连接断开。
func (c *Conn) readLoop() {
	for {
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var frame inbound
		if err := json.Unmarshal(raw, &frame); err != nil || frame.Type == "" {
			c.bye(ByeProtocol, "无法识别的消息", false)
			return
		}
		if frame.Type == OpPing {
			// 校时走快速通道，不排限流、不进业务，尽量少引入延迟
			var ping pingData
			_ = json.Unmarshal(frame.Data, &ping)
			c.send(outbound{Type: FramePong, ID: frame.ID, Data: pongData{T0: ping.T0, TS: time.Now().UnixMilli()}})
			continue
		}
		if !c.limiter.Allow() {
			c.reply(frame.ID, nil, room.ErrRateLimited)
			continue
		}
		started := time.Now()
		data, err := c.dispatch(frame)
		result := "ok"
		if err != nil {
			result = "error"
			if room.AsError(err) == nil {
				result = "internal"
				c.log.Warn("command failed", zap.String("op", frame.Type), zap.Error(err))
			}
		}
		c.server.metrics.ObserveCommand(frame.Type, result, started)
		c.reply(frame.ID, data, err)
		select {
		case <-c.done:
			return
		default:
		}
	}
}

func decode[T any](raw json.RawMessage) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, room.Invalid("参数格式错误")
	}
	return out, nil
}

// dispatch 把一条命令交给房间服务。返回值作为回复的 data。
func (c *Conn) dispatch(frame inbound) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	svc := c.server.rooms
	a := c.actor()
	switch frame.Type {
	case OpHello:
		return nil, room.Invalid("已经完成握手")
	case OpAuth:
		in, err := decode[authData](frame.Data)
		if err != nil {
			return nil, err
		}
		return c.renew(in.Ticket)
	case OpRoomCreate:
		in, err := decode[room.SettingsInput](frame.Data)
		if err != nil {
			return nil, err
		}
		snapshot, err := svc.Create(ctx, a, in)
		return c.entered(snapshot, err)
	case OpRoomJoin:
		in, err := decode[joinData](frame.Data)
		if err != nil {
			return nil, err
		}
		snapshot, err := svc.Join(ctx, a, in.Code)
		return c.entered(snapshot, err)
	case OpRoomSync:
		snapshot, err := svc.Sync(ctx, a)
		return c.entered(snapshot, err)
	case OpRoomLeave:
		err := svc.Leave(ctx, a)
		if err == nil {
			c.server.hub.Unbind(c)
		}
		return nil, err
	case OpRoomClose:
		return nil, svc.Close(ctx, a)
	case OpRoomSettings:
		in, err := decode[room.SettingsInput](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.UpdateSettings(ctx, a, in)
	case OpMemberKick:
		in, err := decode[userTarget](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.Kick(ctx, a, in.UserID)
	case OpMemberTransfer:
		in, err := decode[userTarget](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.Transfer(ctx, a, in.UserID)
	case OpMemberGrant:
		in, err := decode[grantData](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.Grant(ctx, a, in.UserID, in.CanControl)
	case OpQueueSet:
		in, err := decode[queueData](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.SetQueue(ctx, a, in.Items)
	case OpPlayback:
		in, err := decode[room.PlaybackInput](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.UpdatePlayback(ctx, a, in)
	case OpRequestAdd:
		in, err := decode[requestData](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.Request(ctx, a, in.Track)
	case OpRequestResolve:
		in, err := decode[resolveData](frame.Data)
		if err != nil {
			return nil, err
		}
		request, err := svc.ResolveRequest(ctx, a, in.RequestID)
		return request, err
	case OpControl:
		in, err := decode[room.ControlInput](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.SendControl(ctx, a, in)
	case OpChat:
		if !c.chatLimiter.Allow() {
			return nil, room.ErrRateLimited
		}
		in, err := decode[chatData](frame.Data)
		if err != nil {
			return nil, err
		}
		return svc.Chat(ctx, a, in.Text)
	case OpReaction:
		if !c.chatLimiter.Allow() {
			return nil, room.ErrRateLimited
		}
		in, err := decode[reactionData](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.React(ctx, a, in.Emoji)
	case OpStatus:
		in, err := decode[room.MemberStatus](frame.Data)
		if err != nil {
			return nil, err
		}
		return nil, svc.ReportStatus(ctx, a, in)
	}
	return nil, room.Invalid("不支持的命令：" + frame.Type)
}

// entered 进入房间成功后开始收这个房间的事件；快照作为回复数据。
func (c *Conn) entered(snapshot *room.Snapshot, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, room.ErrNotInRoom
	}
	if err := c.server.hub.Bind(c, snapshot.Room.ID); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// renew 连接上续期票据：只接受同一应用、同一用户的新票据。
func (c *Conn) renew(raw string) (any, error) {
	claims, err := c.server.verifier.Verify(raw)
	if err != nil {
		if errors.Is(err, listenticket.ErrMissingFeature) {
			return nil, &room.Error{Code: "not_entitled", Message: "当前账号没有一起听权益"}
		}
		return nil, &room.Error{Code: ByeTicketInvalid, Message: "凭证无效"}
	}
	if claims.AppKey != c.identity.AppKey || claims.UserID() != c.identity.UserID {
		return nil, &room.Error{Code: ByeTicketInvalid, Message: "凭证与当前连接不符"}
	}
	c.identity.Nickname = claims.Nickname
	c.identity.Avatar = claims.Avatar
	expires := claims.ExpiresAt.Time.UnixMilli()
	c.setExpires(expires)
	return map[string]int64{"ticketExpiresAt": expires}, nil
}
