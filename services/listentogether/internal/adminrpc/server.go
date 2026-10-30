// Package adminrpc 内部管理接口（Connect：同一个端口同时说 Connect / gRPC / gRPC-Web）。
// 只给 Aegis 主服务调，凭内部令牌；终端用户永远到不了这里。
package adminrpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	ltv1 "aegis/api/gen/listentogether/v1"
	"aegis/api/gen/listentogether/v1/listentogetherv1connect"
	"aegis/services/listentogether/internal/room"
)

// Server 实现 ListenTogetherAdminService。
type Server struct {
	rooms     *room.Service
	repo      room.Repository
	bus       room.Bus
	localConn func(app string, userID int64) int
	log       *zap.Logger
}

var _ listentogetherv1connect.ListenTogetherAdminServiceHandler = (*Server)(nil)

// New 构造管理接口。localConn 返回本实例上某用户的连接数（DisconnectUser 回报用）。
func New(rooms *room.Service, repo room.Repository, bus room.Bus, localConn func(string, int64) int, log *zap.Logger) *Server {
	return &Server{rooms: rooms, repo: repo, bus: bus, localConn: localConn, log: log}
}

// AuthInterceptor 校验内部令牌（常量时间比较）。令牌为空时拒绝一切调用：宁可不可用也不裸奔。
func AuthInterceptor(token string) connect.Interceptor {
	expected := []byte("Bearer " + token)
	return connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			got := []byte(strings.TrimSpace(req.Header().Get("Authorization")))
			if token == "" || subtle.ConstantTimeCompare(got, expected) != 1 {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid internal token"))
			}
			return next(ctx, req)
		}
	})
}

func (s *Server) GetStats(ctx context.Context, req *connect.Request[ltv1.GetStatsRequest]) (*connect.Response[ltv1.GetStatsResponse], error) {
	apps := []string{req.Msg.GetAppKey()}
	if req.Msg.GetAppKey() == "" {
		var err error
		if apps, err = s.repo.Apps(ctx); err != nil {
			return nil, internal(err)
		}
	}
	out := &ltv1.GetStatsResponse{}
	for _, app := range apps {
		offset := 0
		for {
			ids, total, err := s.repo.ListRooms(ctx, app, offset, 200)
			if err != nil {
				return nil, internal(err)
			}
			for _, id := range ids {
				r, err := s.repo.LoadRoom(ctx, app, id)
				if err != nil {
					continue
				}
				out.Rooms++
				if r.Playback.Playing {
					out.PlayingRooms++
				}
				out.Members += int32(len(r.Members))
				out.OnlineMembers += int32(r.OnlineCount())
			}
			offset += len(ids)
			if len(ids) == 0 || offset >= total {
				break
			}
		}
	}
	instances, err := s.repo.Instances(ctx)
	if err != nil {
		return nil, internal(err)
	}
	for _, item := range instances {
		out.Instances = append(out.Instances, &ltv1.Instance{
			Id: item.ID, Version: item.Version, StartedAtMs: item.StartedAt,
			Connections: int32(item.Connections), Rooms: int32(item.Rooms), HeartbeatAtMs: item.HeartbeatAt,
		})
	}
	return connect.NewResponse(out), nil
}

func (s *Server) ListRooms(ctx context.Context, req *connect.Request[ltv1.ListRoomsRequest]) (*connect.Response[ltv1.ListRoomsResponse], error) {
	app := strings.TrimSpace(req.Msg.GetAppKey())
	if app == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("app_key is required"))
	}
	page, size := max(1, int(req.Msg.GetPage())), int(req.Msg.GetPageSize())
	if size <= 0 || size > 100 {
		size = 20
	}
	ids, total, err := s.repo.ListRooms(ctx, app, (page-1)*size, size)
	if err != nil {
		return nil, internal(err)
	}
	out := &ltv1.ListRoomsResponse{Total: int32(total)}
	for _, id := range ids {
		r, err := s.repo.LoadRoom(ctx, app, id)
		if err != nil {
			continue
		}
		out.Rooms = append(out.Rooms, summary(r))
	}
	return connect.NewResponse(out), nil
}

func (s *Server) GetRoom(ctx context.Context, req *connect.Request[ltv1.GetRoomRequest]) (*connect.Response[ltv1.GetRoomResponse], error) {
	r, chat, err := s.rooms.Get(ctx, req.Msg.GetAppKey(), req.Msg.GetRoomId())
	if err != nil {
		return nil, domainError(err)
	}
	detail := &ltv1.RoomDetail{Summary: summary(r), BannedUserIds: r.Banned}
	for _, member := range r.Members {
		detail.Members = append(detail.Members, &ltv1.Member{
			UserId: member.UserID, Nickname: member.Nickname, Avatar: member.Avatar, Role: member.Role,
			CanControl: member.CanControl, Online: member.Online(), JoinedAtMs: member.JoinedAt,
			Platform: member.Platform, State: member.Status.State,
		})
	}
	for _, item := range r.Queue {
		detail.Queue = append(detail.Queue, track(item))
	}
	for _, msg := range chat {
		detail.RecentChat = append(detail.RecentChat, &ltv1.ChatMessage{
			Id: msg.ID, UserId: msg.UserID, Nickname: msg.Nickname, Text: msg.Text, SentAtMs: msg.At,
		})
	}
	return connect.NewResponse(&ltv1.GetRoomResponse{Room: detail}), nil
}

func (s *Server) CloseRoom(ctx context.Context, req *connect.Request[ltv1.CloseRoomRequest]) (*connect.Response[ltv1.CloseRoomResponse], error) {
	if err := s.rooms.AdminClose(ctx, req.Msg.GetAppKey(), req.Msg.GetRoomId()); err != nil {
		return nil, domainError(err)
	}
	s.log.Info("room closed by admin", zap.String("app", req.Msg.GetAppKey()), zap.String("room", req.Msg.GetRoomId()), zap.String("reason", req.Msg.GetReason()))
	return connect.NewResponse(&ltv1.CloseRoomResponse{}), nil
}

func (s *Server) KickMember(ctx context.Context, req *connect.Request[ltv1.KickMemberRequest]) (*connect.Response[ltv1.KickMemberResponse], error) {
	if _, _, err := s.rooms.Get(ctx, req.Msg.GetAppKey(), req.Msg.GetRoomId()); err != nil {
		return nil, domainError(err)
	}
	if err := s.rooms.AdminKick(ctx, req.Msg.GetAppKey(), req.Msg.GetRoomId(), req.Msg.GetUserId()); err != nil {
		return nil, domainError(err)
	}
	return connect.NewResponse(&ltv1.KickMemberResponse{}), nil
}

func (s *Server) DisconnectUser(ctx context.Context, req *connect.Request[ltv1.DisconnectUserRequest]) (*connect.Response[ltv1.DisconnectUserResponse], error) {
	app, userID := req.Msg.GetAppKey(), req.Msg.GetUserId()
	if app == "" || userID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("app_key and user_id are required"))
	}
	local := s.localConn(app, userID)
	if err := s.bus.PublishUser(ctx, room.UserSignal{Kind: room.SignalRevoke, AppKey: app, UserID: userID, Reason: req.Msg.GetReason()}); err != nil {
		return nil, internal(err)
	}
	if err := s.rooms.RemoveUser(ctx, app, userID); err != nil {
		return nil, internal(err)
	}
	return connect.NewResponse(&ltv1.DisconnectUserResponse{Connections: int32(local)}), nil
}

func summary(r *room.Room) *ltv1.RoomSummary {
	out := &ltv1.RoomSummary{
		RoomId: r.ID, Code: r.Code, Name: r.Settings.Name, HostUserId: r.HostID,
		MemberCount: int32(len(r.Members)), OnlineCount: int32(r.OnlineCount()), Capacity: int32(r.Settings.Capacity),
		ControlMode: r.Settings.ControlMode, AllowRequests: r.Settings.AllowRequests, Locked: r.Settings.Locked,
		QueueLength: int32(len(r.Queue)), CreatedAtMs: r.CreatedAt, UpdatedAtMs: r.UpdatedAt,
	}
	if host := r.Host(); host != nil {
		out.HostNickname = host.Nickname
	}
	playback := &ltv1.Playback{
		ItemId: r.Playback.ItemID, PositionMs: r.Playback.PositionMs, Playing: r.Playback.Playing,
		Rate: r.Playback.Rate, UpdatedAtMs: r.Playback.At,
	}
	if item, ok := r.Item(r.Playback.ItemID); ok {
		playback.Track = track(item)
	}
	out.Playback = playback
	return out
}

func track(t room.Track) *ltv1.Track {
	return &ltv1.Track{
		ItemId: t.ItemID, Source: t.Source, SongId: t.SongID, Title: t.Title, Artist: t.Artist,
		Album: t.Album, CoverUrl: t.Cover, DurationMs: t.DurationMs, RequestedBy: t.RequestedBy,
	}
}

func domainError(err error) error {
	if domain := room.AsError(err); domain != nil {
		code := connect.CodeFailedPrecondition
		if domain == room.ErrRoomNotFound || domain == room.ErrMemberNotFound {
			code = connect.CodeNotFound
		}
		return connect.NewError(code, errors.New(domain.Message))
	}
	return internal(err)
}

func internal(err error) error {
	return connect.NewError(connect.CodeInternal, err)
}

// Mount 把管理接口挂到 mux 上（拦截器链：链路追踪 → 令牌）。
func Mount(mux *http.ServeMux, server *Server, interceptors ...connect.Interceptor) {
	path, handler := listentogetherv1connect.NewListenTogetherAdminServiceHandler(server, connect.WithInterceptors(interceptors...))
	mux.Handle(path, handler)
}
