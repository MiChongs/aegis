package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/nats.go"
	gobreaker "github.com/sony/gobreaker/v2"
	"go.uber.org/zap"

	ltv1 "aegis/api/gen/listentogether/v1"
	"aegis/api/gen/listentogether/v1/listentogetherv1connect"
	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/listenticket"
)

// ListenTogetherService 一起听在主服务这一侧的全部职责（docs/listen-together.md）：
//
//   - 签票据：判「已登录 + 令牌属于这个应用 + 拥有 listen_together 权益」，把结论连同昵称头像
//     签成短期 EdDSA 票据，并告诉客户端去哪里连（一起听服务的 wss 地址）。
//   - 发布 JWKS：一起听服务据此离线验票，运行时不回头调主服务。
//   - 管理端转发：控制台的房间列表、解散、移出经内部 RPC（Connect）交给一起听服务，带熔断，
//     一起听服务不可用时只影响这几个管理页面。
//   - 撤销：封禁、强制下线时经 NATS 通知一起听服务立刻断开，不等票据过期。
//
// 长连接与房间状态都不在主服务进程里，一起听服务挂了、重启、扩容都不影响主服务。
type ListenTogetherService struct {
	cfg       config.ListenTogetherConfig
	signer    *listenticket.Signer
	client    listentogetherv1connect.ListenTogetherAdminServiceClient
	breaker   *gobreaker.CircuitBreaker[any]
	vip       *VipService
	user      *UserService
	apps      *AppService
	publicIDs *UserPublicIDs
	nats      *nats.Conn
	log       *zap.Logger
}

// 错误码（docs/listen-together.md「错误码」）。没有权益沿用权益凭证的 40230。
const (
	errCodeListenRoomNotFound   = 40467
	errCodeListenNotEnabled     = 50360
	errCodeListenUnavailable    = 50361
	errCodeListenRPCUnavailable = 50362
)

// NewListenTogetherService 构造一起听对接。签名钥匙配置无效时返回错误（启动即失败）。
func NewListenTogetherService(log *zap.Logger, cfg config.ListenTogetherConfig, vip *VipService, user *UserService, apps *AppService, natsConn *nats.Conn) (*ListenTogetherService, error) {
	keys, err := listenticket.ParseSeeds(cfg.TicketKeys)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		seed, err := listenticket.DeriveSeed(cfg.DeriveSecret)
		if err != nil {
			return nil, err
		}
		key, err := listenticket.KeyFromSeed(seed)
		if err != nil {
			return nil, err
		}
		keys = []listenticket.Key{key}
	}
	signer, err := listenticket.NewSigner(keys, cfg.TicketTTL)
	if err != nil {
		return nil, err
	}
	s := &ListenTogetherService{cfg: cfg, signer: signer, vip: vip, user: user, apps: apps, nats: natsConn, log: log.Named("listen_together")}
	if user != nil {
		s.publicIDs = user.PublicIDs()
	}
	if cfg.RPCURL != "" {
		otelInterceptor, err := otelconnect.NewInterceptor()
		if err != nil {
			return nil, err
		}
		token := cfg.InternalToken
		auth := connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
			return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
				req.Header().Set("Authorization", "Bearer "+token)
				return next(ctx, req)
			}
		})
		s.client = listentogetherv1connect.NewListenTogetherAdminServiceClient(
			&http.Client{Timeout: 5 * time.Second}, cfg.RPCURL,
			connect.WithInterceptors(otelInterceptor, auth),
		)
		s.breaker = gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
			Name:        "listen-together-rpc",
			MaxRequests: 1,
			Timeout:     20 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures >= 5 },
			// 业务上的「没有这个房间」不是服务故障，不计入熔断
			IsSuccessful: func(err error) bool {
				if err == nil {
					return true
				}
				switch connect.CodeOf(err) {
				case connect.CodeNotFound, connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
					return true
				}
				return false
			},
		})
	}
	return s, nil
}

// ListenTogetherTicket POST /listen-together/ticket 的响应。
type ListenTogetherTicket struct {
	Ticket string `json:"ticket"`
	// ExpiresAt / RefreshAfter 毫秒时间戳：过了 RefreshAfter 就该回来换新票据并在连接上续期。
	ExpiresAt    int64 `json:"expiresAt"`
	RefreshAfter int64 `json:"refreshAfter"`
	// Endpoints 一起听服务的 wss 地址，按顺序尝试。
	Endpoints  []string                 `json:"endpoints"`
	ServerTime int64                    `json:"serverTime"`
	User       ListenTogetherTicketUser `json:"user"`
}

// ListenTogetherTicketUser 票据里给房间其他人看的资料。
type ListenTogetherTicketUser struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
}

// IssueTicket 签一张一起听票据。
func (s *ListenTogetherService) IssueTicket(ctx context.Context, app *appdomain.App, session *authdomain.Session, deviceID string) (*ListenTogetherTicket, error) {
	if !s.cfg.Enabled() {
		return nil, apperrors.New(errCodeListenNotEnabled, http.StatusServiceUnavailable, "一起听服务尚未开通")
	}
	verification, err := s.vip.VerifyMembershipByToken(ctx, app.ID, session, listenticket.Feature)
	if err != nil {
		return nil, err
	}
	if verification == nil || !verification.Granted {
		return nil, apperrors.New(errCodeFeatureNotGranted, http.StatusPaymentRequired, "一起听是会员权益，开通后即可使用")
	}
	me, err := s.user.GetMy(ctx, session)
	if err != nil {
		return nil, err
	}
	if me.ID <= 0 {
		return nil, apperrors.New(errCodeListenUnavailable, http.StatusServiceUnavailable, "账号资料暂不可用，请稍后再试")
	}
	nickname := strings.TrimSpace(me.Nickname)
	if nickname == "" {
		nickname = maskListenAccount(me.Account)
	}
	now := time.Now()
	token, expires, err := s.signer.Issue(listenticket.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: strconv.FormatInt(me.ID, 10)},
		AppKey:           app.AppKey,
		Nickname:         nickname,
		Avatar:           me.Avatar,
		Device:           strings.TrimSpace(deviceID),
		Features:         []string{listenticket.Feature},
		Lifetime:         verification.Membership.IsLifetime,
	}, now, newListenJTI())
	if err != nil {
		return nil, err
	}
	ttl := expires.Sub(now)
	return &ListenTogetherTicket{
		Ticket:       token,
		ExpiresAt:    expires.UnixMilli(),
		RefreshAfter: now.Add(ttl * 4 / 5).UnixMilli(),
		Endpoints:    append([]string(nil), s.cfg.Endpoints...),
		ServerTime:   now.UnixMilli(),
		User:         ListenTogetherTicketUser{ID: me.ID, Nickname: nickname, Avatar: me.Avatar},
	}, nil
}

// JWKS 票据公钥。
func (s *ListenTogetherService) JWKS() listenticket.JWKS { return s.signer.JWKS() }

// RevokeUser 封禁、强制下线后通知一起听服务断开这个人（异步、尽力而为：票据过期是兜底）。
func (s *ListenTogetherService) RevokeUser(appID, userID int64, reason string) {
	if s == nil || s.nats == nil || !s.cfg.Enabled() || appID <= 0 || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		app, err := s.apps.GetApp(ctx, appID)
		if err != nil || app == nil {
			return
		}
		publicID := s.publicIDs.Of(ctx, userID)
		if publicID <= 0 {
			return
		}
		payload, _ := json.Marshal(listenticket.Revoke{AppKey: app.AppKey, UserID: publicID, Reason: reason})
		if err := s.nats.Publish(listenticket.SubjectRevoke, payload); err != nil {
			s.log.Warn("publish listen revoke failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}()
}

// ── 管理端 ──

// ListenTogetherTrack 管理端看到的歌。
type ListenTogetherTrack struct {
	ItemID      string `json:"itemId"`
	Source      string `json:"source"`
	SongID      string `json:"songId"`
	Title       string `json:"title"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Cover       string `json:"cover,omitempty"`
	DurationMs  int64  `json:"durationMs,omitempty"`
	RequestedBy int64  `json:"requestedBy,omitempty"`
}

// ListenTogetherPlayback 管理端看到的播放状态。
type ListenTogetherPlayback struct {
	Track      *ListenTogetherTrack `json:"track,omitempty"`
	PositionMs int64                `json:"positionMs"`
	Playing    bool                 `json:"playing"`
	UpdatedAt  int64                `json:"updatedAt"`
}

// ListenTogetherRoom 管理端房间摘要。用户 ID 是应用内主键（控制台跳用户详情用），publicId 是对外 ID。
type ListenTogetherRoom struct {
	RoomID        string                 `json:"roomId"`
	Code          string                 `json:"code"`
	Name          string                 `json:"name"`
	HostUserID    int64                  `json:"hostUserId"`
	HostPublicID  int64                  `json:"hostPublicId"`
	HostNickname  string                 `json:"hostNickname"`
	MemberCount   int32                  `json:"memberCount"`
	OnlineCount   int32                  `json:"onlineCount"`
	Capacity      int32                  `json:"capacity"`
	ControlMode   string                 `json:"controlMode"`
	AllowRequests bool                   `json:"allowRequests"`
	Locked        bool                   `json:"locked"`
	QueueLength   int32                  `json:"queueLength"`
	Playback      ListenTogetherPlayback `json:"playback"`
	CreatedAt     int64                  `json:"createdAt"`
	UpdatedAt     int64                  `json:"updatedAt"`
}

// ListenTogetherMember 管理端房间成员。
type ListenTogetherMember struct {
	UserID     int64  `json:"userId"`
	PublicID   int64  `json:"publicId"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar,omitempty"`
	Role       string `json:"role"`
	CanControl bool   `json:"canControl"`
	Online     bool   `json:"online"`
	JoinedAt   int64  `json:"joinedAt"`
	Platform   string `json:"platform,omitempty"`
	State      string `json:"state,omitempty"`
}

// ListenTogetherChat 管理端看到的聊天。
type ListenTogetherChat struct {
	ID       string `json:"id"`
	UserID   int64  `json:"userId"`
	PublicID int64  `json:"publicId"`
	Nickname string `json:"nickname"`
	Text     string `json:"text"`
	SentAt   int64  `json:"sentAt"`
}

// ListenTogetherRoomDetail 管理端房间详情。
type ListenTogetherRoomDetail struct {
	ListenTogetherRoom
	Members []ListenTogetherMember `json:"members"`
	Queue   []ListenTogetherTrack  `json:"queue"`
	Chat    []ListenTogetherChat   `json:"chat"`
	Banned  []int64                `json:"bannedPublicIds"`
}

// ListenTogetherInstance 一起听服务的一个实例。
type ListenTogetherInstance struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	StartedAt   int64  `json:"startedAt"`
	Connections int32  `json:"connections"`
	Rooms       int32  `json:"rooms"`
	HeartbeatAt int64  `json:"heartbeatAt"`
}

// ListenTogetherStats 管理端统计。
type ListenTogetherStats struct {
	Enabled       bool                     `json:"enabled"`
	Endpoints     []string                 `json:"endpoints"`
	Rooms         int32                    `json:"rooms"`
	PlayingRooms  int32                    `json:"playingRooms"`
	Members       int32                    `json:"members"`
	OnlineMembers int32                    `json:"onlineMembers"`
	Instances     []ListenTogetherInstance `json:"instances"`
	Feature       string                   `json:"feature"`
}

// ListenTogetherRoomPage 管理端房间列表。
type ListenTogetherRoomPage struct {
	Items []ListenTogetherRoom `json:"items"`
	Total int32                `json:"total"`
}

// Stats 统计（一起听服务不可用时返回 50361，控制台据此提示）。
func (s *ListenTogetherService) Stats(ctx context.Context, appKey string) (*ListenTogetherStats, error) {
	resp, err := listenCall(s, ctx, func(ctx context.Context) (*connect.Response[ltv1.GetStatsResponse], error) {
		return s.client.GetStats(ctx, connect.NewRequest(&ltv1.GetStatsRequest{AppKey: appKey}))
	})
	if err != nil {
		return nil, err
	}
	out := &ListenTogetherStats{
		Enabled: s.cfg.Enabled(), Endpoints: append([]string{}, s.cfg.Endpoints...), Feature: listenticket.Feature,
		Rooms: resp.Msg.GetRooms(), PlayingRooms: resp.Msg.GetPlayingRooms(),
		Members: resp.Msg.GetMembers(), OnlineMembers: resp.Msg.GetOnlineMembers(),
		Instances: []ListenTogetherInstance{},
	}
	for _, item := range resp.Msg.GetInstances() {
		out.Instances = append(out.Instances, ListenTogetherInstance{
			ID: item.GetId(), Version: item.GetVersion(), StartedAt: item.GetStartedAtMs(),
			Connections: item.GetConnections(), Rooms: item.GetRooms(), HeartbeatAt: item.GetHeartbeatAtMs(),
		})
	}
	return out, nil
}

// ListRooms 房间列表。
func (s *ListenTogetherService) ListRooms(ctx context.Context, app *appdomain.App, page, pageSize int) (*ListenTogetherRoomPage, error) {
	resp, err := listenCall(s, ctx, func(ctx context.Context) (*connect.Response[ltv1.ListRoomsResponse], error) {
		return s.client.ListRooms(ctx, connect.NewRequest(&ltv1.ListRoomsRequest{AppKey: app.AppKey, Page: int32(page), PageSize: int32(pageSize)}))
	})
	if err != nil {
		return nil, err
	}
	out := &ListenTogetherRoomPage{Items: []ListenTogetherRoom{}, Total: resp.Msg.GetTotal()}
	for _, item := range resp.Msg.GetRooms() {
		out.Items = append(out.Items, s.roomView(ctx, app.ID, item))
	}
	return out, nil
}

// GetRoom 房间详情。
func (s *ListenTogetherService) GetRoom(ctx context.Context, app *appdomain.App, roomID string) (*ListenTogetherRoomDetail, error) {
	resp, err := listenCall(s, ctx, func(ctx context.Context) (*connect.Response[ltv1.GetRoomResponse], error) {
		return s.client.GetRoom(ctx, connect.NewRequest(&ltv1.GetRoomRequest{AppKey: app.AppKey, RoomId: roomID}))
	})
	if err != nil {
		return nil, err
	}
	room := resp.Msg.GetRoom()
	out := &ListenTogetherRoomDetail{
		ListenTogetherRoom: s.roomView(ctx, app.ID, room.GetSummary()),
		Members:            []ListenTogetherMember{}, Queue: []ListenTogetherTrack{}, Chat: []ListenTogetherChat{},
		Banned: append([]int64{}, room.GetBannedUserIds()...),
	}
	for _, member := range room.GetMembers() {
		out.Members = append(out.Members, ListenTogetherMember{
			UserID: s.resolve(ctx, app.ID, member.GetUserId()), PublicID: member.GetUserId(),
			Nickname: member.GetNickname(), Avatar: member.GetAvatar(), Role: member.GetRole(),
			CanControl: member.GetCanControl(), Online: member.GetOnline(), JoinedAt: member.GetJoinedAtMs(),
			Platform: member.GetPlatform(), State: member.GetState(),
		})
	}
	for _, item := range room.GetQueue() {
		out.Queue = append(out.Queue, trackView(item))
	}
	for _, msg := range room.GetRecentChat() {
		out.Chat = append(out.Chat, ListenTogetherChat{
			ID: msg.GetId(), UserID: s.resolve(ctx, app.ID, msg.GetUserId()), PublicID: msg.GetUserId(),
			Nickname: msg.GetNickname(), Text: msg.GetText(), SentAt: msg.GetSentAtMs(),
		})
	}
	return out, nil
}

// CloseRoom 解散房间。
func (s *ListenTogetherService) CloseRoom(ctx context.Context, app *appdomain.App, roomID, reason string) error {
	_, err := listenCall(s, ctx, func(ctx context.Context) (*connect.Response[ltv1.CloseRoomResponse], error) {
		return s.client.CloseRoom(ctx, connect.NewRequest(&ltv1.CloseRoomRequest{AppKey: app.AppKey, RoomId: roomID, Reason: reason}))
	})
	return err
}

// KickMember 把用户（应用内主键）移出房间。
func (s *ListenTogetherService) KickMember(ctx context.Context, app *appdomain.App, roomID string, userID int64) error {
	publicID := s.publicIDs.Of(ctx, userID)
	if publicID <= 0 {
		return apperrors.New(40400, http.StatusNotFound, "用户不存在")
	}
	_, err := listenCall(s, ctx, func(ctx context.Context) (*connect.Response[ltv1.KickMemberResponse], error) {
		return s.client.KickMember(ctx, connect.NewRequest(&ltv1.KickMemberRequest{AppKey: app.AppKey, RoomId: roomID, UserId: publicID}))
	})
	return err
}

func (s *ListenTogetherService) roomView(ctx context.Context, appID int64, item *ltv1.RoomSummary) ListenTogetherRoom {
	view := ListenTogetherRoom{
		RoomID: item.GetRoomId(), Code: item.GetCode(), Name: item.GetName(),
		HostUserID: s.resolve(ctx, appID, item.GetHostUserId()), HostPublicID: item.GetHostUserId(), HostNickname: item.GetHostNickname(),
		MemberCount: item.GetMemberCount(), OnlineCount: item.GetOnlineCount(), Capacity: item.GetCapacity(),
		ControlMode: item.GetControlMode(), AllowRequests: item.GetAllowRequests(), Locked: item.GetLocked(),
		QueueLength: item.GetQueueLength(), CreatedAt: item.GetCreatedAtMs(), UpdatedAt: item.GetUpdatedAtMs(),
	}
	if playback := item.GetPlayback(); playback != nil {
		view.Playback = ListenTogetherPlayback{PositionMs: playback.GetPositionMs(), Playing: playback.GetPlaying(), UpdatedAt: playback.GetUpdatedAtMs()}
		if playback.GetTrack() != nil {
			track := trackView(playback.GetTrack())
			view.Playback.Track = &track
		}
	}
	return view
}

func (s *ListenTogetherService) resolve(ctx context.Context, appID, publicID int64) int64 {
	id, err := s.publicIDs.Resolve(ctx, appID, publicID)
	if err != nil {
		return 0
	}
	return id
}

func trackView(item *ltv1.Track) ListenTogetherTrack {
	return ListenTogetherTrack{
		ItemID: item.GetItemId(), Source: item.GetSource(), SongID: item.GetSongId(), Title: item.GetTitle(),
		Artist: item.GetArtist(), Album: item.GetAlbum(), Cover: item.GetCoverUrl(), DurationMs: item.GetDurationMs(),
		RequestedBy: item.GetRequestedBy(),
	}
}

// listenCall 经熔断器调内部 RPC，把 Connect 错误翻译成 Aegis 错误码。
func listenCall[T any](s *ListenTogetherService, ctx context.Context, fn func(context.Context) (*connect.Response[T], error)) (*connect.Response[T], error) {
	if s.client == nil {
		return nil, apperrors.New(errCodeListenRPCUnavailable, http.StatusServiceUnavailable, "未配置一起听服务的内部地址（LISTEN_TOGETHER_RPC_URL）")
	}
	result, err := s.breaker.Execute(func() (any, error) { return fn(ctx) })
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, apperrors.New(errCodeListenUnavailable, http.StatusServiceUnavailable, "一起听服务暂时不可用")
		}
		switch connect.CodeOf(err) {
		case connect.CodeNotFound:
			return nil, apperrors.New(errCodeListenRoomNotFound, http.StatusNotFound, "房间不存在或已解散")
		case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
			return nil, apperrors.New(40000, http.StatusBadRequest, connectMessage(err))
		case connect.CodeUnauthenticated:
			s.log.Error("listen-together rejected the internal token; check LISTEN_TOGETHER_INTERNAL_TOKEN")
		}
		s.log.Warn("listen-together rpc failed", zap.Error(err))
		return nil, apperrors.New(errCodeListenUnavailable, http.StatusServiceUnavailable, "一起听服务暂时不可用")
	}
	return result.(*connect.Response[T]), nil
}

func connectMessage(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) && connectErr.Message() != "" {
		return connectErr.Message()
	}
	return "请求无效"
}

func newListenJTI() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// maskListenAccount 没设昵称时给别人看的名字：账号打码，不把完整账号（可能是手机号、邮箱）亮给陌生人。
func maskListenAccount(account string) string {
	runes := []rune(strings.TrimSpace(account))
	switch {
	case len(runes) == 0:
		return "Voyager"
	case len(runes) <= 2:
		return string(runes[:1]) + "*"
	case len(runes) <= 6:
		return string(runes[:1]) + "***" + string(runes[len(runes)-1:])
	default:
		return string(runes[:2]) + "***" + string(runes[len(runes)-2:])
	}
}
