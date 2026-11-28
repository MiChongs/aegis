package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	gobreaker "github.com/sony/gobreaker/v2"
	"go.uber.org/zap"

	musicv1 "aegis/api/gen/music/v1"
	"aegis/api/gen/music/v1/musicv1connect"
	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/services/music/contract"
)

// MusicService 音乐个性化在主服务这一侧的全部职责（docs/music-personalization.md）：
//
//   - 上报：校验客户端行为事件、补齐身份，逐条发布到 JetStream（MUSIC 流）并等确认，全部确认才回 200。
//     音乐服务挂了也照常收：事件在流里排队，恢复后补落地。
//   - 推荐、画像、清除数据：经内部 RPC（Connect）交给 music-rec，带熔断。
//   - 管理端：控制台接口反向代理到 music-rec（AdminBaseURL / InternalToken）。
//   - 删号：账号删除时清除这个人的听歌数据。
type MusicService struct {
	cfg     config.MusicConfig
	js      jetstream.JetStream
	client  musicv1connect.MusicServiceClient
	breaker *gobreaker.CircuitBreaker[any]
	rdb     *redis.Client
	prefix  string
	log     *zap.Logger

	streamMu sync.Mutex
	streamOK bool

	switchMu sync.Mutex
	switches map[int64]musicSwitch
}

type musicSwitch struct {
	enabled, collect bool
	at               time.Time
}

// 错误码（docs「客户端接口」）。
const (
	errCodeMusicNotEnabled  = 50396
	errCodeMusicUnavailable = 50397
	errCodeMusicRateLimited = 42970
)

// 每人上报限额。
const (
	musicEventsPerMinute = 600
	musicEventsPerDay    = 20000
)

// NewMusicService 构造。natsConn 为空时上报不可用（回 50397）；RPCURL 为空时推荐与管理端不可用（50396）。
func NewMusicService(log *zap.Logger, cfg config.MusicConfig, natsConn *nats.Conn, rdb *redis.Client, redisPrefix string) (*MusicService, error) {
	s := &MusicService{cfg: cfg, rdb: rdb, prefix: redisPrefix + "music:", log: log.Named("music"), switches: map[int64]musicSwitch{}}
	if natsConn != nil {
		js, err := jetstream.New(natsConn)
		if err != nil {
			return nil, err
		}
		s.js = js
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
		s.client = musicv1connect.NewMusicServiceClient(&http.Client{Timeout: 10 * time.Second}, cfg.RPCURL,
			connect.WithInterceptors(otelInterceptor, auth))
		s.breaker = gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
			Name:        "music-rpc",
			MaxRequests: 1,
			Timeout:     20 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool { return counts.ConsecutiveFailures >= 5 },
			IsSuccessful: func(err error) bool {
				if err == nil {
					return true
				}
				switch connect.CodeOf(err) {
				case connect.CodeInvalidArgument, connect.CodeFailedPrecondition, connect.CodeNotFound:
					return true
				}
				return false
			},
		})
	}
	return s, nil
}

// Enabled 是否接入了 music-rec（推荐与管理端）。
func (s *MusicService) Enabled() bool { return s != nil && s.cfg.Enabled() }

// AdminBaseURL music-rec 地址（管理端反代）。
func (s *MusicService) AdminBaseURL() string { return s.cfg.RPCURL }

// InternalToken 内部令牌（管理端反代）。
func (s *MusicService) InternalToken() string { return s.cfg.InternalToken }

// ── 上报 ──

// MusicEventBatch POST /music/events 的请求体。
type MusicEventBatch struct {
	Events []contract.ClientEvent `json:"events"`
	Client contract.ClientInfo    `json:"client"`
}

// MusicRejected 被拒的一条（不要重传）。
type MusicRejected struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// MusicReportResult 上报结果。
type MusicReportResult struct {
	Accepted   int             `json:"accepted"`
	Rejected   []MusicRejected `json:"rejected"`
	ServerTime int64           `json:"serverTime"`
}

// Report 校验并发布一批事件。
func (s *MusicService) Report(ctx context.Context, app *appdomain.App, session *authdomain.Session, deviceID, ip string, batch MusicEventBatch) (*MusicReportResult, error) {
	now := time.Now()
	out := &MusicReportResult{Rejected: []MusicRejected{}, ServerTime: now.UnixMilli()}
	if len(batch.Events) == 0 {
		return out, nil
	}
	if len(batch.Events) > contract.MaxBatch {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多上报 "+strconv.Itoa(contract.MaxBatch)+" 条事件")
	}
	if !s.cfg.EventsEnabled || !s.collectEnabled(ctx, app.ID) {
		return out, nil
	}
	if s.js == nil {
		return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
	}
	if err := s.rateLimit(ctx, app.ID, session.UserID, len(batch.Events), now); err != nil {
		return nil, err
	}
	if err := s.ensureStream(ctx); err != nil {
		s.log.Warn("ensure music stream failed", zap.Error(err))
		return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
	}
	seen := make(map[string]bool, len(batch.Events))
	futures := make([]jetstream.PubAckFuture, 0, len(batch.Events))
	subject := contract.EventSubject(app.ID)
	for i := range batch.Events {
		ev := batch.Events[i]
		if reason := contract.Normalize(&ev, now); reason != "" {
			out.Rejected = append(out.Rejected, MusicRejected{ID: ev.ID, Reason: reason})
			continue
		}
		if seen[ev.ID] {
			out.Rejected = append(out.Rejected, MusicRejected{ID: ev.ID, Reason: contract.RejectDuplicateInID})
			continue
		}
		seen[ev.ID] = true
		payload, err := json.Marshal(contract.Enrich(ev, batch.Client, app.ID, session.UserID, deviceID, ip, now))
		if err != nil {
			out.Rejected = append(out.Rejected, MusicRejected{ID: ev.ID, Reason: contract.RejectInvalidTrack})
			continue
		}
		msg := &nats.Msg{Subject: subject, Data: payload, Header: nats.Header{}}
		msg.Header.Set(jetstream.MsgIDHeader, contract.MsgID(app.ID, session.UserID, ev.ID))
		future, err := s.js.PublishMsgAsync(msg)
		if err != nil {
			s.log.Warn("publish music event failed", zap.Error(err))
			return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
		}
		futures = append(futures, future)
	}
	// 全部确认才算收下：任何一条没确认整批回 503，客户端整批重传，已入流的被去重窗口丢掉
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for _, f := range futures {
		select {
		case <-f.Ok():
		case err := <-f.Err():
			s.log.Warn("music event not acked", zap.Error(err))
			return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
		case <-timeout.C:
			return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	out.Accepted = len(futures)
	return out, nil
}

func (s *MusicService) ensureStream(ctx context.Context) error {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.streamOK {
		return nil
	}
	if _, err := s.js.Stream(ctx, contract.StreamEvents); err == nil {
		s.streamOK = true
		return nil
	}
	if err := contract.EnsureStreams(ctx, s.js); err != nil {
		return err
	}
	s.streamOK = true
	return nil
}

// rateLimit 每人每分钟 / 每天的事件数（固定窗口，按条数计）。
func (s *MusicService) rateLimit(ctx context.Context, appID, userID int64, n int, now time.Time) error {
	if s.rdb == nil {
		return nil
	}
	base := s.prefix + "rl:" + strconv.FormatInt(appID, 10) + ":" + strconv.FormatInt(userID, 10) + ":"
	minute := base + "m:" + strconv.FormatInt(now.Unix()/60, 10)
	day := base + "d:" + now.UTC().Format("20060102")
	pipe := s.rdb.Pipeline()
	m := pipe.IncrBy(ctx, minute, int64(n))
	pipe.Expire(ctx, minute, 2*time.Minute)
	d := pipe.IncrBy(ctx, day, int64(n))
	pipe.Expire(ctx, day, 26*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		// 限额只是保护，Redis 故障时不拦上报
		s.log.Warn("music rate limit unavailable", zap.Error(err))
		return nil
	}
	if m.Val() > musicEventsPerMinute || d.Val() > musicEventsPerDay {
		return apperrors.New(errCodeMusicRateLimited, http.StatusTooManyRequests, "上报过于频繁，请稍后再试")
	}
	return nil
}

// collectEnabled 应用是否在采集（music-rec 的开关，缓存 30 秒；取不到时按开启处理，事件先进流）。
func (s *MusicService) collectEnabled(ctx context.Context, appID int64) bool {
	enabled, collect, ok := s.switchesOf(ctx, appID)
	if !ok {
		return true
	}
	return enabled && collect
}

func (s *MusicService) switchesOf(ctx context.Context, appID int64) (enabled, collect, ok bool) {
	if s.client == nil {
		return false, false, false
	}
	s.switchMu.Lock()
	item, cached := s.switches[appID]
	s.switchMu.Unlock()
	if cached && time.Since(item.at) < 30*time.Second {
		return item.enabled, item.collect, true
	}
	resp, err := musicCall(s, ctx, func(ctx context.Context) (*connect.Response[musicv1.GetSwitchesResponse], error) {
		return s.client.GetSwitches(ctx, connect.NewRequest(&musicv1.GetSwitchesRequest{AppId: appID}))
	})
	if err != nil {
		if cached {
			return item.enabled, item.collect, true
		}
		return false, false, false
	}
	item = musicSwitch{enabled: resp.Msg.GetEnabled(), collect: resp.Msg.GetCollectEnabled(), at: time.Now()}
	s.switchMu.Lock()
	s.switches[appID] = item
	s.switchMu.Unlock()
	return item.enabled, item.collect, true
}

// ── 推荐与画像 ──

// MusicRecommendQuery GET /music/recommendations 的参数。
type MusicRecommendQuery struct {
	Scene        string           `json:"scene"`
	Limit        int              `json:"limit"`
	Cursor       string           `json:"cursor"`
	Source       string           `json:"source"`
	SongID       string           `json:"songId"`
	Personalized bool             `json:"personalized"`
	Seeds        []contract.Track `json:"seeds"`   // autoplay：最近放的在后（≤ 20）
	Exclude      []string         `json:"exclude"` // autoplay：source:songId（≤ 300）
	MixID        string           `json:"mixId"`
	Artist       string           `json:"artist"`
}

// MusicRecReason 推荐原因。
type MusicRecReason struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	RefTitle string `json:"refTitle,omitempty"`
}

// MusicRecItem 一首推荐。
type MusicRecItem struct {
	WorkKey string         `json:"workKey"` // 作品键（歌名|歌手，跨音源一致）
	Track   contract.Track `json:"track"`
	Score   float64        `json:"score"`
	Reason  MusicRecReason `json:"reason"`
	Resolve bool           `json:"resolve,omitempty"` // 待客户端按歌名歌手搜索解析（第三期）
}

// MusicRecommendations 推荐结果。
type MusicRecommendations struct {
	RecID        string         `json:"recId"`
	Scene        string         `json:"scene"`
	ModelVersion int64          `json:"modelVersion"`
	Personalized bool           `json:"personalized"`
	NextCursor   string         `json:"nextCursor,omitempty"`
	GeneratedAt  int64          `json:"generatedAt"`
	ExpiresAt    int64          `json:"expiresAt"`
	ExperimentID int64          `json:"experimentId"`
	Variant      string         `json:"variant"`
	Items        []MusicRecItem `json:"items"`
}

// Recommend 推荐。
func (s *MusicService) Recommend(ctx context.Context, app *appdomain.App, session *authdomain.Session, q MusicRecommendQuery) (*MusicRecommendations, error) {
	if !contract.ValidScene(q.Scene) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的推荐场景")
	}
	if len(q.Seeds) > 20 || len(q.Exclude) > 300 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "seeds 最多 20 首、exclude 最多 300 首")
	}
	req := &musicv1.RecommendRequest{
		AppId: app.ID, UserId: session.UserID, Scene: q.Scene, Limit: int32(q.Limit), Cursor: q.Cursor,
		SeedSource: q.Source, SeedSongId: q.SongID, Personalized: q.Personalized,
		Exclude: q.Exclude, MixId: q.MixID, Artist: q.Artist,
	}
	for _, t := range q.Seeds {
		req.Seeds = append(req.Seeds, &musicv1.Track{Source: t.Source, SongId: t.SongID, Title: t.Title, Artist: t.Artist,
			Album: t.Album, Cover: t.Cover, DurationMs: t.DurationMs})
	}
	resp, err := musicCall(s, ctx, func(ctx context.Context) (*connect.Response[musicv1.RecommendResponse], error) {
		return s.client.Recommend(ctx, connect.NewRequest(req))
	})
	if err != nil {
		return nil, err
	}
	m := resp.Msg
	out := &MusicRecommendations{RecID: m.GetRecId(), Scene: m.GetScene(), ModelVersion: m.GetModelVersion(),
		Personalized: m.GetPersonalized(), NextCursor: m.GetNextCursor(), GeneratedAt: m.GetGeneratedAtMs(),
		ExpiresAt: m.GetExpiresAtMs(), ExperimentID: m.GetExperimentId(), Variant: m.GetVariant(),
		Items: make([]MusicRecItem, 0, len(m.GetItems()))}
	for _, it := range m.GetItems() {
		t := it.GetTrack()
		out.Items = append(out.Items, MusicRecItem{
			WorkKey: it.GetWorkKey(),
			Track: contract.Track{Source: t.GetSource(), SongID: t.GetSongId(), Title: t.GetTitle(), Artist: t.GetArtist(),
				Album: t.GetAlbum(), Cover: t.GetCover(), DurationMs: t.GetDurationMs()},
			Score:   it.GetScore(),
			Reason:  MusicRecReason{Kind: it.GetReason().GetKind(), Text: it.GetReason().GetText(), RefTitle: it.GetReason().GetRefTitle()},
			Resolve: it.GetResolve(),
		})
	}
	return out, nil
}

// MusicUserOps 第二期用户侧操作（docs/music-personalization-2.md「客户端接口增量」）。
var MusicUserOps = map[string]bool{
	"mixes": true, "artists": true, "onboarding": true, "prefs_get": true, "prefs_put": true, "feedback_list": true,
	"feedback_undo": true, "report": true, "trending": true, "rerank": true, "export": true,
}

// UserOp 用户侧的其他操作：参数与结果都是 JSON，原样透传 music-rec。
func (s *MusicService) UserOp(ctx context.Context, app *appdomain.App, session *authdomain.Session, op string, params any, personalized bool) (json.RawMessage, error) {
	if !MusicUserOps[op] {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的音乐操作")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "参数格式不正确")
	}
	resp, err := musicCall(s, ctx, func(ctx context.Context) (*connect.Response[musicv1.UserOpResponse], error) {
		return s.client.UserOp(ctx, connect.NewRequest(&musicv1.UserOpRequest{AppId: app.ID, UserId: session.UserID, Op: op,
			ParamsJson: raw, Personalized: personalized}))
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Msg.GetResultJson()), nil
}

// Profile 听歌画像（music-rec 的 JSON 原样透传）。
func (s *MusicService) Profile(ctx context.Context, app *appdomain.App, session *authdomain.Session, days int) (json.RawMessage, error) {
	resp, err := musicCall(s, ctx, func(ctx context.Context) (*connect.Response[musicv1.GetProfileResponse], error) {
		return s.client.GetProfile(ctx, connect.NewRequest(&musicv1.GetProfileRequest{AppId: app.ID, UserId: session.UserID, Days: int32(days)}))
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(resp.Msg.GetProfileJson()), nil
}

// ClearProfile 清除自己的听歌数据。
func (s *MusicService) ClearProfile(ctx context.Context, app *appdomain.App, session *authdomain.Session) error {
	_, err := musicCall(s, ctx, func(ctx context.Context) (*connect.Response[musicv1.ForgetUserResponse], error) {
		return s.client.ForgetUser(ctx, connect.NewRequest(&musicv1.ForgetUserRequest{AppId: app.ID, UserId: session.UserID, Reason: "user"}))
	})
	return err
}

// PurgeHook 挂到 UserService.AddPurgeHook：账号删除后清除这个人的听歌数据。
func (s *MusicService) PurgeHook(_ context.Context, appID int64, _ []pgrepo.PurgeTarget) func(context.Context, []pgrepo.PurgeTarget) {
	return func(ctx context.Context, deleted []pgrepo.PurgeTarget) {
		if !s.Enabled() || len(deleted) == 0 {
			return
		}
		bg := context.WithoutCancel(ctx)
		for _, t := range deleted {
			userID := t.UserID
			cctx, cancel := context.WithTimeout(bg, 30*time.Second)
			_, err := musicCall(s, cctx, func(ctx context.Context) (*connect.Response[musicv1.ForgetUserResponse], error) {
				return s.client.ForgetUser(ctx, connect.NewRequest(&musicv1.ForgetUserRequest{AppId: appID, UserId: userID, Reason: "account_deleted", Operator: "system"}))
			})
			cancel()
			if err != nil {
				s.log.Warn("forget music data of deleted user failed", zap.Int64("app", appID), zap.Int64("user", userID), zap.Error(err))
			}
		}
	}
}

// musicCall 经熔断器调内部 RPC，把 Connect 错误翻译成 Aegis 错误码。
func musicCall[T any](s *MusicService, ctx context.Context, fn func(context.Context) (*connect.Response[T], error)) (*connect.Response[T], error) {
	if s == nil || s.client == nil {
		return nil, apperrors.New(errCodeMusicNotEnabled, http.StatusServiceUnavailable, "音乐个性化服务尚未开通")
	}
	result, err := s.breaker.Execute(func() (any, error) { return fn(ctx) })
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
		}
		switch connect.CodeOf(err) {
		case connect.CodeFailedPrecondition:
			return nil, apperrors.New(errCodeMusicNotEnabled, http.StatusServiceUnavailable, "该应用未开启音乐个性化")
		case connect.CodeInvalidArgument:
			return nil, apperrors.New(40000, http.StatusBadRequest, musicConnectMessage(err))
		case connect.CodeUnauthenticated:
			s.log.Error("music-rec rejected the internal token; check MUSIC_INTERNAL_TOKEN")
		}
		s.log.Warn("music rpc failed", zap.Error(err))
		return nil, apperrors.New(errCodeMusicUnavailable, http.StatusServiceUnavailable, "音乐服务暂时不可用")
	}
	return result.(*connect.Response[T]), nil
}

func musicConnectMessage(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) && strings.TrimSpace(connectErr.Message()) != "" {
		return connectErr.Message()
	}
	return "请求无效"
}
