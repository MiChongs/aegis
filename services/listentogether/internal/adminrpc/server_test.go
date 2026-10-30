package adminrpc_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	ltv1 "aegis/api/gen/listentogether/v1"
	"aegis/api/gen/listentogether/v1/listentogetherv1connect"
	"aegis/services/listentogether/internal/adminrpc"
	"aegis/services/listentogether/internal/bus"
	"aegis/services/listentogether/internal/room"
	"aegis/services/listentogether/internal/store"
)

func TestAdminServiceRequiresTokenAndManagesRooms(t *testing.T) {
	repo, eventBus := store.NewMemory(), bus.NewMemory()
	svc := room.NewService(repo, eventBus, time.Now, room.Options{
		Capacity: 5, CapacityMax: 10, QueueLimit: 10, ChatHistory: 5, MemberGrace: time.Minute, IdleTTL: time.Minute, Instance: "i1",
	}, zap.NewNop())
	mux := http.NewServeMux()
	adminrpc.Mount(mux, adminrpc.New(svc, repo, eventBus, func(string, int64) int { return 0 }, zap.NewNop()), adminrpc.AuthInterceptor("secret"))
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx := context.Background()
	host := room.Actor{Identity: room.Identity{AppKey: "voyage", UserID: 1, Nickname: "房主"}, ConnID: "c1"}
	snap, err := svc.Create(ctx, host, room.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Join(ctx, room.Actor{Identity: room.Identity{AppKey: "voyage", UserID: 2, Nickname: "访客"}, ConnID: "c2"}, snap.Room.Code); err != nil {
		t.Fatal(err)
	}

	anonymous := listentogetherv1connect.NewListenTogetherAdminServiceClient(server.Client(), server.URL)
	_, err = anonymous.GetStats(ctx, connect.NewRequest(&ltv1.GetStatsRequest{}))
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("missing token: %v", err)
	}

	client := listentogetherv1connect.NewListenTogetherAdminServiceClient(server.Client(), server.URL,
		connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				req.Header().Set("Authorization", "Bearer secret")
				return next(ctx, req)
			}
		})), connect.WithGRPC())

	stats, err := client.GetStats(ctx, connect.NewRequest(&ltv1.GetStatsRequest{AppKey: "voyage"}))
	if err != nil {
		t.Fatal(err)
	}
	if stats.Msg.GetRooms() != 1 || stats.Msg.GetMembers() != 2 {
		t.Fatalf("stats = %+v", stats.Msg)
	}
	list, err := client.ListRooms(ctx, connect.NewRequest(&ltv1.ListRoomsRequest{AppKey: "voyage"}))
	if err != nil || list.Msg.GetTotal() != 1 || list.Msg.GetRooms()[0].GetHostNickname() != "房主" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := client.KickMember(ctx, connect.NewRequest(&ltv1.KickMemberRequest{AppKey: "voyage", RoomId: snap.Room.ID, UserId: 2})); err != nil {
		t.Fatal(err)
	}
	detail, err := client.GetRoom(ctx, connect.NewRequest(&ltv1.GetRoomRequest{AppKey: "voyage", RoomId: snap.Room.ID}))
	if err != nil || len(detail.Msg.GetRoom().GetMembers()) != 1 || detail.Msg.GetRoom().GetBannedUserIds()[0] != 2 {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if _, err := client.CloseRoom(ctx, connect.NewRequest(&ltv1.CloseRoomRequest{AppKey: "voyage", RoomId: snap.Room.ID})); err != nil {
		t.Fatal(err)
	}
	_, err = client.GetRoom(ctx, connect.NewRequest(&ltv1.GetRoomRequest{AppKey: "voyage", RoomId: snap.Room.ID}))
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeNotFound {
		t.Fatalf("closed room: %v", err)
	}
}
