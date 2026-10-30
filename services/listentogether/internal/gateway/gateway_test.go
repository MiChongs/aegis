package gateway_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"aegis/pkg/listenticket"
	"aegis/services/listentogether/internal/bus"
	"aegis/services/listentogether/internal/config"
	"aegis/services/listentogether/internal/gateway"
	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/store"
	"aegis/services/listentogether/internal/telemetry"
	"aegis/services/listentogether/internal/ticket"
)

// 两个实例共用一份 Redis 与一个 NATS：用户连在不同实例上也要能一起听。
type cluster struct {
	t       *testing.T
	signer  *listenticket.Signer
	urls    []string
	natsURL string
}

func startCluster(t *testing.T, instances int) *cluster {
	t.Helper()
	mr := miniredis.RunT(t)
	ns, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats not ready")
	}
	t.Cleanup(ns.Shutdown)

	seed, _ := listenticket.DeriveSeed("gateway-test")
	key, _ := listenticket.KeyFromSeed(seed)
	signer, _ := listenticket.NewSigner([]listenticket.Key{key}, time.Minute)
	keys, _ := listenticket.ParseJWKS(mustJSON(signer.JWKS()))

	c := &cluster{t: t, signer: signer, natsURL: ns.ClientURL()}
	for i := 0; i < instances; i++ {
		cfg := config.Config{
			InstanceID: "inst-" + strconv.Itoa(i), Version: "test", AllowedOrigins: []string{"*"},
			RoomCapacity: 5, RoomCapacityMax: 10, QueueLimit: 50, ChatHistory: 20,
			MemberGrace: time.Minute, RoomIdleTTL: time.Minute, TicketGrace: time.Minute, MaxConnsPerInstance: 100,
		}
		rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		nc, err := nats.Connect(ns.ClientURL())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { nc.Close(); _ = rc.Close() })
		repo := store.NewRedis(rc, "lt:")
		eventBus := bus.NewNATS(nc, zap.NewNop())
		metrics := telemetry.NewMetrics()
		svc := room.NewService(repo, eventBus, time.Now, room.Options{
			Capacity: cfg.RoomCapacity, CapacityMax: cfg.RoomCapacityMax, QueueLimit: cfg.QueueLimit,
			ChatHistory: cfg.ChatHistory, MemberGrace: cfg.MemberGrace, IdleTTL: cfg.RoomIdleTTL, Instance: cfg.InstanceID,
		}, zap.NewNop())
		hub, err := gateway.NewHub(eventBus, metrics, zap.NewNop())
		if err != nil {
			t.Fatal(err)
		}
		server := gateway.NewServer(cfg, svc, hub, ticket.NewStatic(keys, time.Second), metrics, zap.NewNop(),
			func(context.Context) error { return nil })
		httpServer := httptest.NewServer(server.Handler())
		t.Cleanup(httpServer.Close)
		c.urls = append(c.urls, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws")
	}
	return c
}

func (c *cluster) ticket(userID int64, name string, features ...string) string {
	if features == nil {
		features = []string{listenticket.Feature}
	}
	raw, _, err := c.signer.Issue(listenticket.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: strconv.FormatInt(userID, 10)},
		AppKey:           "voyage", Nickname: name, Features: features,
	}, time.Now(), "jti-"+name)
	if err != nil {
		c.t.Fatal(err)
	}
	return raw
}

type frame struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	OK    *bool           `json:"ok"`
	Room  string          `json:"room"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type client struct {
	t    *testing.T
	ws   *websocket.Conn
	seq  int
	name string
}

func (c *cluster) dial(instance int, userID int64, name string, features ...string) (*client, frame) {
	c.t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(c.urls[instance], nil)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(func() { _ = ws.Close() })
	cl := &client{t: c.t, ws: ws, name: name}
	cl.write("hello", "", map[string]any{"ticket": c.ticket(userID, name, features...), "client": map[string]string{"platform": "android"}})
	return cl, cl.next()
}

func (c *client) write(kind, id string, data any) {
	c.t.Helper()
	if err := c.ws.WriteJSON(map[string]any{"type": kind, "id": id, "data": data}); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) next() frame {
	c.t.Helper()
	_ = c.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var f frame
	if err := c.ws.ReadJSON(&f); err != nil {
		c.t.Fatalf("%s: read: %v", c.name, err)
	}
	return f
}

// call 发命令并等它的回复（期间到达的其它帧丢弃）。
func (c *client) call(kind string, data any) frame {
	c.t.Helper()
	c.seq++
	id := "r" + strconv.Itoa(c.seq)
	c.write(kind, id, data)
	for {
		f := c.next()
		if f.Type == "reply" && f.ID == id {
			return f
		}
	}
}

// event 等某种房间事件。
func (c *client) event(kind string) room.Event {
	c.t.Helper()
	for {
		f := c.next()
		if f.Type != "event" {
			continue
		}
		var e room.Event
		_ = json.Unmarshal(f.Data, &e)
		if e.Kind == kind {
			return e
		}
	}
}

func (c *client) waitType(kind string) frame {
	c.t.Helper()
	for {
		if f := c.next(); f.Type == kind {
			return f
		}
	}
}

func okData[T any](t *testing.T, f frame) T {
	t.Helper()
	if f.OK == nil || !*f.OK {
		code := ""
		if f.Error != nil {
			code = f.Error.Code
		}
		t.Fatalf("reply failed: %s", code)
	}
	var out T
	if err := json.Unmarshal(f.Data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func errCode(f frame) string {
	if f.Error == nil {
		return ""
	}
	return f.Error.Code
}

func TestListenTogetherAcrossInstances(t *testing.T) {
	c := startCluster(t, 2)
	host, welcome := c.dial(0, 1001, "host")
	if welcome.Type != "welcome" {
		t.Fatalf("first frame = %s", welcome.Type)
	}
	created := okData[room.Snapshot](t, host.call("room.create", map[string]any{"name": "晚安电台"}))
	if created.Room.Code == "" || created.Room.Settings.Name != "晚安电台" {
		t.Fatalf("created = %+v", created.Room)
	}

	guest, _ := c.dial(1, 2002, "guest")
	joined := okData[room.Snapshot](t, guest.call("room.join", map[string]string{"code": created.Room.Code}))
	if len(joined.Members) != 2 {
		t.Fatalf("members = %d", len(joined.Members))
	}
	if e := host.event(room.EventMemberJoined); e.Member.UserID != 2002 {
		t.Fatalf("joined event for %d", e.Member.UserID)
	}

	// 房主同步队列与播放状态，另一实例上的成员收到
	queue := []map[string]any{
		{"itemId": "i1", "source": "tx", "songId": "001", "title": "晴天", "artist": "周杰伦", "durationMs": 269000},
		{"itemId": "i2", "source": "wy", "songId": "002", "title": "稻香", "durationMs": 223000},
	}
	if f := host.call("queue.set", map[string]any{"items": queue}); errCode(f) != "" {
		t.Fatalf("queue.set: %s", errCode(f))
	}
	if q := guest.event(room.EventQueue); len(q.Queue.Items) != 2 {
		t.Fatalf("queue event = %+v", q.Queue)
	}
	if f := host.call("playback.update", map[string]any{"itemId": "i2", "positionMs": 12000, "playing": true}); errCode(f) != "" {
		t.Fatalf("playback: %s", errCode(f))
	}
	if p := guest.event(room.EventPlayback); p.Playback.ItemID != "i2" || !p.Playback.Playing {
		t.Fatalf("playback event = %+v", p.Playback)
	}
	if f := guest.call("queue.set", map[string]any{"items": queue}); errCode(f) != "not_host" {
		t.Fatalf("guest queue.set = %s", errCode(f))
	}

	// 控制权：先没有，授权后指令只送到房主
	if f := guest.call("control", map[string]any{"action": "pause"}); errCode(f) != "no_control" {
		t.Fatalf("control without grant = %s", errCode(f))
	}
	if f := host.call("member.grant", map[string]any{"userId": 2002, "canControl": true}); errCode(f) != "" {
		t.Fatal(errCode(f))
	}
	if f := guest.call("control", map[string]any{"action": "seek", "positionMs": 60000}); errCode(f) != "" {
		t.Fatalf("control = %s", errCode(f))
	}
	if ctl := host.event(room.EventControl); ctl.Control.Action != "seek" || ctl.Control.PositionMs != 60000 || ctl.Control.By != 2002 {
		t.Fatalf("control event = %+v", ctl.Control)
	}

	// 聊天
	if f := guest.call("chat.send", map[string]string{"text": "这首好听"}); errCode(f) != "" {
		t.Fatal(errCode(f))
	}
	if msg := host.event(room.EventChat); msg.Chat.Text != "这首好听" || msg.Chat.UserID != 2002 {
		t.Fatalf("chat = %+v", msg.Chat)
	}

	// 校时
	guest.write("ping", "p1", map[string]int64{"t0": 42})
	pong := guest.waitType("pong")
	var pd struct{ T0, TS int64 }
	_ = json.Unmarshal(pong.Data, &pd)
	if pd.T0 != 42 || pd.TS == 0 {
		t.Fatalf("pong = %+v", pd)
	}

	// 成员换到另一个实例重连：旧连接被顶替，新连接自动接回房间
	moved, welcome2 := c.dial(0, 2002, "guest-moved")
	var w struct{ Room string }
	_ = json.Unmarshal(welcome2.Data, &w)
	if w.Room != created.Room.ID {
		t.Fatalf("resumed room = %q", w.Room)
	}
	if snap := moved.waitType("snapshot"); snap.Room != created.Room.ID {
		t.Fatal("snapshot expected after resume")
	}
	if bye := guest.waitType("bye"); !strings.Contains(string(bye.Data), "replaced") {
		t.Fatalf("old connection bye = %s", bye.Data)
	}

	// 主服务经 NATS 撤销：连接断开、从房间移出
	nc, err := nats.Connect(c.natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	raw, _ := json.Marshal(listenticket.Revoke{AppKey: "voyage", UserID: 2002, Reason: "banned"})
	if err := nc.Publish(listenticket.SubjectRevoke, raw); err != nil {
		t.Fatal(err)
	}
	if bye := moved.waitType("bye"); !strings.Contains(string(bye.Data), "revoked") {
		t.Fatalf("revoke bye = %s", bye.Data)
	}
	if left := host.event(room.EventMemberLeft); left.UserID != 2002 || left.Reason != room.LeftRevoked {
		t.Fatalf("left = %+v", left)
	}

	// 房主解散
	if f := host.call("room.close", nil); errCode(f) != "" {
		t.Fatal(errCode(f))
	}
	if closed := host.event(room.EventRoomClosed); closed.Reason != room.ClosedByHost {
		t.Fatalf("closed = %+v", closed)
	}
}

func TestHandshakeRejectsBadTickets(t *testing.T) {
	c := startCluster(t, 1)
	_, bye := c.dial(0, 1, "nofeature", "vip")
	if bye.Type != "bye" || !strings.Contains(string(bye.Data), "not_entitled") {
		t.Fatalf("missing feature: %s %s", bye.Type, bye.Data)
	}

	ws, _, err := websocket.DefaultDialer.Dial(c.urls[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	_ = ws.WriteJSON(map[string]any{"type": "hello", "data": map[string]string{"ticket": "not-a-jwt"}})
	var f frame
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := ws.ReadJSON(&f); err != nil || f.Type != "bye" || !strings.Contains(string(f.Data), "ticket_invalid") {
		t.Fatalf("bad ticket: %v %+v", err, f)
	}

	cl, _ := c.dial(0, 7, "seven")
	if f := cl.call("room.join", map[string]string{"code": "000000"}); errCode(f) != "room_not_found" {
		t.Fatalf("join unknown = %s", errCode(f))
	}
	if f := cl.call("nope", nil); errCode(f) != "invalid" {
		t.Fatalf("unknown op = %s", errCode(f))
	}
	other := c.ticket(8, "eight")
	if f := cl.call("auth", map[string]string{"ticket": other}); errCode(f) != "ticket_invalid" {
		t.Fatalf("foreign renewal = %s", errCode(f))
	}
	if f := cl.call("auth", map[string]string{"ticket": c.ticket(7, "seven-renamed")}); errCode(f) != "" {
		t.Fatalf("renewal = %s", errCode(f))
	}
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
