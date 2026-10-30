package room_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"aegis/services/listentogether/internal/bus"
	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/store"
)

type fixture struct {
	t     *testing.T
	svc   *room.Service
	repo  *store.Memory
	bus   *bus.Memory
	now   time.Time
	mu    sync.Mutex
	saw   []room.Event
	unsub []func()
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, repo: store.NewMemory(), bus: bus.NewMemory(), now: time.UnixMilli(1_800_000_000_000)}
	f.svc = room.NewService(f.repo, f.bus, func() time.Time {
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.now
	}, room.Options{
		Capacity: 3, CapacityMax: 10, QueueLimit: 5, ChatHistory: 3,
		MemberGrace: time.Minute, IdleTTL: 5 * time.Minute, Instance: "i1",
	}, nil)
	t.Cleanup(func() {
		for _, fn := range f.unsub {
			fn()
		}
	})
	return f
}

func (f *fixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *fixture) watch(roomID string) {
	unsub, _ := f.bus.SubscribeRoom("app", roomID, func(e room.Event) {
		f.mu.Lock()
		f.saw = append(f.saw, e)
		f.mu.Unlock()
	})
	f.unsub = append(f.unsub, unsub)
}

func (f *fixture) kinds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.saw))
	for _, e := range f.saw {
		out = append(out, e.Kind)
	}
	return out
}

func actor(id int64, conn string) room.Actor {
	return room.Actor{Identity: room.Identity{AppKey: "app", UserID: id, Nickname: "u" + conn}, ConnID: conn}
}

// must 测试里取值；出错直接 panic（go test 会把它记为该用例失败）。
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func ptr[T any](v T) *T { return &v }

func queue(ids ...string) []room.Track {
	out := make([]room.Track, 0, len(ids))
	for _, id := range ids {
		out = append(out, room.Track{ItemID: id, Source: "tx", SongID: "s" + id, Title: "song " + id, DurationMs: 200_000})
	}
	return out
}

func TestCreateJoinAndHostAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	host, guest := actor(1, "c1"), actor(2, "c2")

	snap := must(f.svc.Create(ctx, host, room.SettingsInput{Name: ptr("周末歌单")}))
	if snap.Room.Code == "" || snap.Room.HostID != 1 || snap.Room.Settings.Name != "周末歌单" {
		t.Fatalf("bad snapshot %+v", snap.Room)
	}
	f.watch(snap.Room.ID)
	joined := must(f.svc.Join(ctx, guest, snap.Room.Code))
	if len(joined.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(joined.Members))
	}

	if err := f.svc.SetQueue(ctx, guest, queue("a")); room.AsError(err) != room.ErrNotHost {
		t.Fatalf("guest must not set queue, got %v", err)
	}
	if err := f.svc.SetQueue(ctx, host, queue("a", "b")); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdatePlayback(ctx, host, room.PlaybackInput{ItemID: "zzz", Playing: true}); room.AsError(err) != room.ErrUnknownItem {
		t.Fatalf("unknown item must be rejected, got %v", err)
	}
	if err := f.svc.UpdatePlayback(ctx, host, room.PlaybackInput{ItemID: "b", PositionMs: 1000, Playing: true}); err != nil {
		t.Fatal(err)
	}
	f.advance(3 * time.Second)
	state := must(f.svc.Sync(ctx, guest))
	if got := state.Playback.PositionAt(f.now.UnixMilli()); got != 4000 {
		t.Fatalf("extrapolated position = %d, want 4000", got)
	}
	if err := f.svc.SetQueue(ctx, host, queue("a", "b", "c", "d", "e", "f")); room.AsError(err) != room.ErrQueueTooLong {
		t.Fatalf("queue limit not enforced: %v", err)
	}
	want := []string{room.EventMemberJoined, room.EventQueue, room.EventPlayback}
	if got := f.kinds(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestControlGoesToHostOnly(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	host, guest := actor(1, "c1"), actor(2, "c2")
	snap := must(f.svc.Create(ctx, host, room.SettingsInput{}))
	must(f.svc.Join(ctx, guest, snap.Room.Code))
	f.watch(snap.Room.ID)

	if err := f.svc.SendControl(ctx, guest, room.ControlInput{Action: room.ActionPause}); room.AsError(err) != room.ErrNoControl {
		t.Fatalf("guest without control: %v", err)
	}
	if err := f.svc.Grant(ctx, host, 2, true); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SendControl(ctx, guest, room.ControlInput{Action: room.ActionPause}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	last := f.saw[len(f.saw)-1]
	f.mu.Unlock()
	if last.Kind != room.EventControl || last.Target != 1 || last.Control.By != 2 {
		t.Fatalf("control event = %+v", last)
	}
	// 房主掉线后指令无处可去
	if err := f.svc.Detach(ctx, host, snap.Room.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SendControl(ctx, guest, room.ControlInput{Action: room.ActionPlay}); room.AsError(err) != room.ErrHostOffline {
		t.Fatalf("host offline: %v", err)
	}
}

func TestCapacityLockAndBan(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snap := must(f.svc.Create(ctx, actor(1, "c1"), room.SettingsInput{}))
	must(f.svc.Join(ctx, actor(2, "c2"), snap.Room.Code))
	must(f.svc.Join(ctx, actor(3, "c3"), snap.Room.Code))
	if _, err := f.svc.Join(ctx, actor(4, "c4"), snap.Room.Code); room.AsError(err) != room.ErrRoomFull {
		t.Fatalf("capacity: %v", err)
	}
	if err := f.svc.Kick(ctx, actor(1, "c1"), 3); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Join(ctx, actor(3, "c3"), snap.Room.Code); room.AsError(err) != room.ErrBanned {
		t.Fatalf("banned rejoin: %v", err)
	}
	if err := f.svc.UpdateSettings(ctx, actor(1, "c1"), room.SettingsInput{Locked: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Join(ctx, actor(5, "c5"), snap.Room.Code); room.AsError(err) != room.ErrRoomLocked {
		t.Fatalf("locked: %v", err)
	}
	// 已在房间的人重连不受锁定影响
	if _, err := f.svc.Join(ctx, actor(2, "c2b"), snap.Room.Code); err != nil {
		t.Fatalf("member rejoin while locked: %v", err)
	}
}

func TestHostLeavingTransfersAndLastOneClosesRoom(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snap := must(f.svc.Create(ctx, actor(1, "c1"), room.SettingsInput{}))
	must(f.svc.Join(ctx, actor(2, "c2"), snap.Room.Code))
	f.watch(snap.Room.ID)
	if err := f.svc.Leave(ctx, actor(1, "c1")); err != nil {
		t.Fatal(err)
	}
	r := must(f.svc.Sync(ctx, actor(2, "c2")))
	if r.Room.HostID != 2 {
		t.Fatalf("host not transferred: %d", r.Room.HostID)
	}
	if err := f.svc.Leave(ctx, actor(2, "c2")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.LoadRoom(ctx, "app", snap.Room.ID); err != room.ErrNotFound {
		t.Fatalf("room should be deleted, got %v", err)
	}
	if _, err := f.repo.FindByCode(ctx, "app", snap.Room.Code); err != room.ErrNotFound {
		t.Fatal("room code must be released")
	}
	kinds := f.kinds()
	if kinds[len(kinds)-1] != room.EventRoomClosed {
		t.Fatalf("last event = %v", kinds)
	}
}

func TestAttachResumesAndDetachOnlyAffectsCurrentConnection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snap := must(f.svc.Create(ctx, actor(1, "c1"), room.SettingsInput{}))
	// 新连接接回房间，旧连接随后断开不应把人判成离线
	resumed := must(f.svc.Attach(ctx, actor(1, "c1-new")))
	if resumed == nil || resumed.Room.ID != snap.Room.ID {
		t.Fatal("attach should resume the room")
	}
	if err := f.svc.Detach(ctx, actor(1, "c1"), snap.Room.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := f.repo.LoadRoom(ctx, "app", snap.Room.ID)
	if !r.Members[0].Online() {
		t.Fatal("stale connection detach must not mark the member offline")
	}
	if none := must(f.svc.Attach(ctx, actor(9, "c9"))); none != nil {
		t.Fatal("user without a room must not attach")
	}
}

func TestSweepTimesOutMembersAndClosesIdleRooms(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snap := must(f.svc.Create(ctx, actor(1, "c1"), room.SettingsInput{}))
	must(f.svc.Join(ctx, actor(2, "c2"), snap.Room.Code))
	// 实例 i1 死了：成员被判离线
	if err := f.svc.Sweep(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	r, _ := f.repo.LoadRoom(ctx, "app", snap.Room.ID)
	if r.OnlineCount() != 0 || r.EmptySince == 0 {
		t.Fatalf("members should be offline: %+v", r)
	}
	f.advance(2 * time.Minute)
	if err := f.svc.Sweep(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	r, _ = f.repo.LoadRoom(ctx, "app", snap.Room.ID)
	if len(r.Members) != 1 {
		t.Fatalf("one offline member should time out, members=%d", len(r.Members))
	}
	f.advance(5 * time.Minute)
	if err := f.svc.Sweep(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.LoadRoom(ctx, "app", snap.Room.ID); err != room.ErrNotFound {
		t.Fatal("idle room should be closed")
	}
	if id, _ := f.repo.UserRoom(ctx, "app", 1); id != "" {
		t.Fatal("user room mapping must be cleared with the room")
	}
}

func TestRequestsAndChat(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	host, guest := actor(1, "c1"), actor(2, "c2")
	snap := must(f.svc.Create(ctx, host, room.SettingsInput{}))
	must(f.svc.Join(ctx, guest, snap.Room.Code))
	song := room.Track{Source: "wy", SongID: "123", Title: "晴天"}
	for i := 0; i < 3; i++ {
		if err := f.svc.Request(ctx, guest, song); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.svc.Request(ctx, guest, song); room.AsError(err) != room.ErrTooManyRequests {
		t.Fatalf("per-member request cap: %v", err)
	}
	state := must(f.svc.Sync(ctx, host))
	resolved := must(f.svc.ResolveRequest(ctx, host, state.Requests.Items[0].ID))
	if resolved.Track.RequestedBy != 2 {
		t.Fatalf("resolved = %+v", resolved)
	}
	if err := f.svc.UpdateSettings(ctx, host, room.SettingsInput{AllowRequests: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Request(ctx, guest, song); room.AsError(err) != room.ErrRequestsOff {
		t.Fatalf("requests off: %v", err)
	}
	for _, text := range []string{"一", "二", "三", "四"} {
		must(f.svc.Chat(ctx, guest, text))
	}
	state = must(f.svc.Sync(ctx, guest))
	if len(state.Chat) != 3 || state.Chat[0].Text != "二" {
		t.Fatalf("chat history = %+v", state.Chat)
	}
	if _, err := f.svc.Chat(ctx, guest, "   "); room.AsError(err) == nil {
		t.Fatal("blank chat must be rejected")
	}
}

func TestJoiningAnotherRoomLeavesThePreviousOne(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := must(f.svc.Create(ctx, actor(1, "c1"), room.SettingsInput{}))
	must(f.svc.Join(ctx, actor(3, "c3"), a.Room.Code))
	b := must(f.svc.Create(ctx, actor(2, "c2"), room.SettingsInput{}))
	must(f.svc.Join(ctx, actor(3, "c3"), b.Room.Code))
	ra, _ := f.repo.LoadRoom(ctx, "app", a.Room.ID)
	if ra.Member(3) != nil {
		t.Fatal("user should have left room A")
	}
	if id, _ := f.repo.UserRoom(ctx, "app", 3); id != b.Room.ID {
		t.Fatalf("user room = %s, want %s", id, b.Room.ID)
	}
}

func TestValidation(t *testing.T) {
	if _, err := room.NormalizeTrack(room.Track{Source: "TX", SongID: "abc", Title: " x "}, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []room.Track{
		{Source: "tx", SongID: "a b", Title: "x"},
		{Source: "../", SongID: "abc", Title: "x"},
		{Source: "tx", SongID: "abc", Title: "   "},
	} {
		if _, err := room.NormalizeTrack(bad, false); err == nil {
			t.Fatalf("track %+v should be rejected", bad)
		}
	}
	clean, _ := room.NormalizeTrack(room.Track{Source: "tx", SongID: "a", Title: "x", Cover: "javascript:alert(1)"}, false)
	if clean.Cover != "" {
		t.Fatal("non-http cover must be dropped")
	}
	if got := room.CleanText("a\x00b​c", 10); got != "ab​c" {
		t.Fatalf("clean text = %q", got)
	}
}
