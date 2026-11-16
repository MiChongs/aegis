package rec

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/nats-io/nats.go"

	musicv1 "aegis/api/gen/music/v1"
	"aegis/api/gen/music/v1/musicv1connect"
	"aegis/services/music/contract"
)

// RPC Connect 处理器（主服务网关调用：推荐、画像、清除、开关）。
type RPC struct {
	Svc *Service
	NC  *nats.Conn
}

var _ musicv1connect.MusicServiceHandler = (*RPC)(nil)

// Mount 挂到 mux，带令牌校验。
func (h *RPC) Mount(mux *http.ServeMux, token string, interceptors ...connect.Interceptor) {
	opts := connect.WithInterceptors(append([]connect.Interceptor{AuthInterceptor(token)}, interceptors...)...)
	path, handler := musicv1connect.NewMusicServiceHandler(h, opts)
	mux.Handle(path, handler)
}

// AuthInterceptor 内部令牌（Authorization: Bearer）；令牌为空时拒绝一切调用。
func AuthInterceptor(token string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			got := strings.TrimPrefix(req.Header().Get("Authorization"), "Bearer ")
			if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid internal token"))
			}
			return next(ctx, req)
		}
	}
}

func rpcErr(err error) error {
	var bad ErrBadRequest
	switch {
	case errors.As(err, &bad):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, ErrNotEnabled):
		return connect.NewError(connect.CodeFailedPrecondition, errors.New("music personalization disabled"))
	}
	return connect.NewError(connect.CodeInternal, err)
}

// Recommend 推荐。
func (h *RPC) Recommend(ctx context.Context, req *connect.Request[musicv1.RecommendRequest]) (*connect.Response[musicv1.RecommendResponse], error) {
	m := req.Msg
	if m.GetAppId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("app_id required"))
	}
	r := Request{
		AppID: m.GetAppId(), UserID: m.GetUserId(), Scene: m.GetScene(), Limit: int(m.GetLimit()), Cursor: m.GetCursor(),
		SeedSource: m.GetSeedSource(), SeedSongID: m.GetSeedSongId(), Personalized: m.GetPersonalized(), Explain: m.GetExplain(),
		Exclude: m.GetExclude(), MixID: m.GetMixId(), Artist: m.GetArtist(),
	}
	for _, t := range m.GetSeeds() {
		r.Seeds = append(r.Seeds, contract.Track{Source: t.GetSource(), SongID: t.GetSongId(), Title: t.GetTitle(), Artist: t.GetArtist(),
			Album: t.GetAlbum(), Cover: t.GetCover(), DurationMs: t.GetDurationMs()})
	}
	res, err := h.Svc.Recommend(ctx, r)
	if err != nil {
		return nil, rpcErr(err)
	}
	out := &musicv1.RecommendResponse{
		RecId: res.RecID, Scene: res.Scene, ModelVersion: res.ModelVersion, Personalized: res.Personalized,
		NextCursor: res.NextCursor, GeneratedAtMs: res.GeneratedAt, ExpiresAtMs: res.ExpiresAt, ColdStart: res.ColdStart, TookMs: res.TookMs,
		ExperimentId: res.ExperimentID, Variant: res.Variant,
	}
	for _, it := range res.Items {
		out.Items = append(out.Items, &musicv1.RecItem{
			WorkKey: it.WorkKey, Track: trackPB(it.Track), Score: it.Score,
			Reason:   &musicv1.Reason{Kind: it.Reason.Kind, Text: it.Reason.Text, RefTitle: it.Reason.RefTitle},
			Channels: it.Channels,
			Resolve:  it.Resolve,
		})
	}
	return connect.NewResponse(out), nil
}

func trackPB(t contract.Track) *musicv1.Track {
	return &musicv1.Track{Source: t.Source, SongId: t.SongID, Title: t.Title, Artist: t.Artist, Album: t.Album, Cover: t.Cover, DurationMs: t.DurationMs}
}

// GetProfile 画像（JSON 透传）。
func (h *RPC) GetProfile(ctx context.Context, req *connect.Request[musicv1.GetProfileRequest]) (*connect.Response[musicv1.GetProfileResponse], error) {
	m := req.Msg
	if m.GetAppId() <= 0 || m.GetUserId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("app_id and user_id required"))
	}
	cfg, err := h.Svc.Cfgs.Get(ctx, m.GetAppId())
	if err != nil {
		return nil, rpcErr(err)
	}
	if !cfg.Enabled {
		return nil, rpcErr(ErrNotEnabled)
	}
	p, err := h.Svc.Profile(ctx, m.GetAppId(), m.GetUserId(), int(m.GetDays()), true)
	if err != nil {
		return nil, rpcErr(err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, rpcErr(err)
	}
	return connect.NewResponse(&musicv1.GetProfileResponse{ProfileJson: raw}), nil
}

// ForgetUser 清除数据。
func (h *RPC) ForgetUser(ctx context.Context, req *connect.Request[musicv1.ForgetUserRequest]) (*connect.Response[musicv1.ForgetUserResponse], error) {
	m := req.Msg
	reason := m.GetReason()
	if reason == "" {
		reason = "user"
	}
	if err := h.Svc.Forget(ctx, h.NC, m.GetAppId(), m.GetUserId(), reason, m.GetOperator()); err != nil {
		return nil, rpcErr(err)
	}
	return connect.NewResponse(&musicv1.ForgetUserResponse{Deleted: true}), nil
}

// GetSwitches 开关。
func (h *RPC) GetSwitches(ctx context.Context, req *connect.Request[musicv1.GetSwitchesRequest]) (*connect.Response[musicv1.GetSwitchesResponse], error) {
	cfg, err := h.Svc.Cfgs.Get(ctx, req.Msg.GetAppId())
	if err != nil {
		return nil, rpcErr(err)
	}
	return connect.NewResponse(&musicv1.GetSwitchesResponse{Enabled: cfg.Enabled, CollectEnabled: cfg.Enabled && cfg.CollectEnabled}), nil
}

// UserOp 用户侧的其他操作（JSON 进、JSON 出）。
func (h *RPC) UserOp(ctx context.Context, req *connect.Request[musicv1.UserOpRequest]) (*connect.Response[musicv1.UserOpResponse], error) {
	m := req.Msg
	if m.GetAppId() <= 0 || m.GetUserId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("app_id and user_id required"))
	}
	result, err := h.Svc.UserOp(ctx, m.GetAppId(), m.GetUserId(), m.GetOp(), m.GetParamsJson(), m.GetPersonalized())
	if err != nil {
		return nil, rpcErr(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, rpcErr(err)
	}
	return connect.NewResponse(&musicv1.UserOpResponse{ResultJson: raw}), nil
}
