package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/leaderboard"
	notificationdomain "aegis/internal/domain/notification"
	"aegis/internal/domain/pointstore"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 排行榜的错误码。客户端按码分支，message 是可以直接展示的原话。
const (
	errCodeLeaderboardDisabled           = 40358
	errCodeLeaderboardAnonymousForbidden = 40359
	errCodeLeaderboardNotFound           = 40452
	errCodeLeaderboardSnapshotNotFound   = 40453
	errCodeLeaderboardExclusionNotFound  = 40454
	errCodeLeaderboardPeriodNotEnded     = 40922

	// leaderboardCacheTTL 每个 (应用, 榜, 周期) 的前 topLimit 名整体缓存多久
	leaderboardCacheTTL = 60 * time.Second
	// leaderboardTickInterval 结算循环的间隔
	leaderboardTickInterval = time.Minute
	// leaderboardGrantBatch 每轮最多发几份奖励（每份一个事务）
	leaderboardGrantBatch = 50
	// leaderboardCacheNamespace Redis 键的命名空间（与旧排行榜的 points / sign 分开）
	leaderboardCacheNamespace = "leaderboard"
)

// leaderboardCached 缓存里的一份榜单：前 topLimit 名与参与者总数。
type leaderboardCached struct {
	Entries     []leaderboard.Entry `json:"entries"`
	Total       int64               `json:"total"`
	GeneratedAt time.Time           `json:"generatedAt"`
}

// LeaderboardService 积分排行榜：实时榜（缓存 + singleflight）、我的名次、偏好、往期与奖励记录、
// 管理端，以及周期结算的后台循环。规则见 docs/leaderboard.md。
type LeaderboardService struct {
	// publicIDs 榜上别人的 userId 换成对外 ID（docs/user-public-id.md）
	publicIDs *UserPublicIDs
	log       *zap.Logger
	pg        *pgrepo.Repository
	cache     *redisrepo.SessionRepository
	notifier  notificationDeliverer
	flight    singleflight.Group
	now       func() time.Time
	loc       func() *time.Location

	stop     chan struct{}
	stopOnce sync.Once
	kick     chan struct{}
}

func NewLeaderboardService(log *zap.Logger, pg *pgrepo.Repository, cache *redisrepo.SessionRepository) *LeaderboardService {
	if log == nil {
		log = zap.NewNop()
	}
	return &LeaderboardService{
		log: log, pg: pg, cache: cache, now: timeutil.Now, loc: timeutil.DefaultLocation,
		stop: make(chan struct{}), kick: make(chan struct{}, 1), publicIDs: newUserPublicIDsFor(pg),
	}
}

// SetNotifier 接上站内信：奖励到账时通知用户。
func (s *LeaderboardService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetClockForTest 测试用：固定「现在」。
func (s *LeaderboardService) SetClockForTest(now func() time.Time) { s.now = now }

func (s *LeaderboardService) location() *time.Location {
	if s.loc == nil {
		return time.Local
	}
	return s.loc()
}

// ── 错误 ──

func leaderboardDisabledError() error {
	return apperrors.New(errCodeLeaderboardDisabled, http.StatusForbidden, "排行榜暂未开放")
}

func leaderboardNotFoundError() error {
	return apperrors.New(errCodeLeaderboardNotFound, http.StatusNotFound, "排行榜不存在")
}

func leaderboardSnapshotNotFoundError() error {
	return apperrors.New(errCodeLeaderboardSnapshotNotFound, http.StatusNotFound, "该期榜单不存在或尚未结算")
}

func (s *LeaderboardService) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrLeaderboardSnapshotNotFound):
		return leaderboardSnapshotNotFoundError()
	case errors.Is(err, pgrepo.ErrLeaderboardExclusionNotFound):
		return apperrors.New(errCodeLeaderboardExclusionNotFound, http.StatusNotFound, "该用户不在移出名单中")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}

// ── 共用 ──

func (s *LeaderboardService) query(settings *leaderboard.Settings, spec leaderboard.BoardSpec, period leaderboard.Period, includeExcluded bool) pgrepo.LeaderboardQuery {
	return pgrepo.LeaderboardQuery{
		AppID: settings.AppID, Board: spec, Period: period, MinValue: settings.MinValue,
		CountAdminAdjust: settings.CountAdminAdjust, IncludeExcluded: includeExcluded,
	}
}

// openBoard 用户端取一个开启的榜：整体未开启 → 40358，榜不存在或未开启 → 40452。
func (s *LeaderboardService) openBoard(ctx context.Context, appID int64, key string) (*leaderboard.Settings, leaderboard.BoardSpec, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, leaderboard.BoardSpec{}, err
	}
	if !settings.Enabled {
		return nil, leaderboard.BoardSpec{}, leaderboardDisabledError()
	}
	spec, ok := leaderboard.FindBoard(key)
	if !ok || !settings.BoardEnabled(key) {
		return nil, leaderboard.BoardSpec{}, leaderboardNotFoundError()
	}
	return settings, spec, nil
}

// refs 奖励描述要用的套餐与功能（没有任何档位时不查）。
func (s *LeaderboardService) refs(ctx context.Context, appID int64, needed bool) (map[int64]pointstore.PlanRef, map[string]pointstore.FeatureRef, error) {
	if !needed {
		return map[int64]pointstore.PlanRef{}, map[string]pointstore.FeatureRef{}, nil
	}
	return s.pg.PointsStoreCatalogRefs(ctx, appID)
}

func describeTiers(tiers []leaderboard.Tier, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) []leaderboard.ClientTier {
	out := make([]leaderboard.ClientTier, 0, len(tiers))
	for _, tier := range tiers {
		out = append(out, leaderboard.ClientTier{
			RankFrom: tier.RankFrom, RankTo: tier.RankTo, Label: leaderboard.TierLabel(tier),
			Rewards: invite.DescribeRewards(tier.Rewards, plans, features),
		})
	}
	return out
}

func (s *LeaderboardService) clientBoard(spec leaderboard.BoardSpec, period leaderboard.Period, tiers []leaderboard.ClientTier, now time.Time) leaderboard.ClientBoard {
	board := leaderboard.ClientBoard{
		Key: spec.Key, Label: spec.Label, Metric: spec.Metric, Period: spec.Period,
		PeriodKey: period.Key, PeriodLabel: period.Label(now, s.location()), ValueUnit: spec.ValueUnit, Rewards: tiers,
	}
	if board.Rewards == nil {
		board.Rewards = []leaderboard.ClientTier{}
	}
	if spec.Periodic() {
		start, end := period.Start, period.End
		board.PeriodStart, board.PeriodEnd = &start, &end
	}
	return board
}

func levelNameFor(spec leaderboard.BoardSpec, level int, name string) string {
	if spec.Metric == leaderboard.MetricExperience || spec.Metric == leaderboard.MetricLevel {
		return leaderboard.LevelLabel(level, name)
	}
	return ""
}

// top 一个榜一个周期的前 topLimit 名：Redis 缓存 60 秒，singleflight 防击穿。
// 缓存里只有展示名（已按展示身份算好）与头像原值，没有原始账号。
func (s *LeaderboardService) top(ctx context.Context, settings *leaderboard.Settings, spec leaderboard.BoardSpec, period leaderboard.Period) (*leaderboardCached, error) {
	scope := strings.Join([]string{period.Key, strconv.FormatInt(settings.MinValue, 10), settings.Identity,
		strconv.FormatBool(settings.CountAdminAdjust)}, ":")
	if s.cache != nil {
		var cached leaderboardCached
		found, err := s.cache.GetRankingCache(ctx, leaderboardCacheNamespace, settings.AppID, spec.Key, scope, 0, settings.TopLimit, &cached)
		if err != nil {
			s.log.Warn("read leaderboard cache failed", zap.Error(err), zap.String("board", spec.Key), zap.Int64("appid", settings.AppID))
		} else if found {
			return &cached, nil
		}
	}
	flightKey := fmt.Sprintf("%d:%s:%s:%d", settings.AppID, spec.Key, scope, settings.TopLimit)
	result, err, _ := s.flight.Do(flightKey, func() (any, error) {
		slice, err := s.pg.LeaderboardRows(ctx, s.query(settings, spec, period, false), settings.TopLimit, 0)
		if err != nil {
			return nil, err
		}
		cached := &leaderboardCached{Entries: make([]leaderboard.Entry, 0, len(slice.Rows)), Total: slice.Participants,
			GeneratedAt: s.now().UTC()}
		for _, row := range slice.Rows {
			cached.Entries = append(cached.Entries, leaderboard.Entry{
				Rank: row.Rank, UserID: row.UserID, Name: leaderboard.DisplayName(settings.Identity, row.Nickname, row.Account),
				RawAvatar: row.RawAvatar, Value: row.Value, Anonymous: row.Anonymous, Experience: row.Experience,
				LevelName: levelNameFor(spec, row.Level, row.LevelName), LastAt: row.LastAt,
			})
		}
		if s.cache != nil {
			if err := s.cache.SetRankingCache(context.WithoutCancel(ctx), leaderboardCacheNamespace, settings.AppID, spec.Key, scope, 0,
				settings.TopLimit, cached, leaderboardCacheTTL); err != nil {
				s.log.Warn("write leaderboard cache failed", zap.Error(err), zap.String("board", spec.Key), zap.Int64("appid", settings.AppID))
			}
		}
		return cached, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*leaderboardCached), nil
}

// ── 用户端 ──

// Boards 排行榜首页：开启的榜单（当前周期、奖励档位）与我的偏好。未开启时 enabled=false、boards=[]，不报错。
func (s *LeaderboardService) Boards(ctx context.Context, appID, userID int64) (*leaderboard.ClientHome, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	home := &leaderboard.ClientHome{Boards: []leaderboard.ClientBoard{}, Me: leaderboard.ClientMeState{AllowAnonymous: settings.AllowAnonymous}}
	anonymous, err := s.pg.GetLeaderboardPreference(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	home.Me.Anonymous = anonymous
	if home.Me.Excluded, err = s.pg.LeaderboardExcluded(ctx, appID, userID); err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return home, nil
	}
	home.Enabled = true
	boards := settings.EnabledBoards()
	needRefs := false
	for _, spec := range boards {
		needRefs = needRefs || len(settings.ActiveTiers(spec.Key)) > 0
	}
	plans, features, err := s.refs(ctx, appID, needRefs)
	if err != nil {
		return nil, err
	}
	now := s.now()
	for _, spec := range boards {
		period := leaderboard.PeriodAt(spec.Period, now, s.location())
		home.Boards = append(home.Boards, s.clientBoard(spec, period, describeTiers(settings.ActiveTiers(spec.Key), plans, features), now))
	}
	return home, nil
}

// Board 一个榜单的实时名次：前 topLimit 名内分页，me 实时算（可以在榜外）。
func (s *LeaderboardService) Board(ctx context.Context, appID, userID int64, key string, page, limit int) (*leaderboard.ClientBoardPage, error) {
	settings, spec, err := s.openBoard(ctx, appID, key)
	if err != nil {
		return nil, err
	}
	now := s.now()
	period := leaderboard.PeriodAt(spec.Period, now, s.location())
	tiers := settings.ActiveTiers(spec.Key)
	plans, features, err := s.refs(ctx, appID, len(tiers) > 0)
	if err != nil {
		return nil, err
	}
	top, err := s.top(ctx, settings, spec, period)
	if err != nil {
		return nil, err
	}
	page, limit = leaderboard.NormalizePage(page, limit, 20, leaderboard.MaxPageLimit)
	shown := min(top.Total, int64(settings.TopLimit))
	out := &leaderboard.ClientBoardPage{
		Board: s.clientBoard(spec, period, describeTiers(tiers, plans, features), now),
		Total: top.Total, TopLimit: settings.TopLimit, Page: page, Limit: limit,
		TotalPages: leaderboard.TotalPages(shown, limit), Items: []leaderboard.ClientRow{}, UpdatedAt: top.GeneratedAt,
	}
	start := (page - 1) * limit
	for i := start; i < len(top.Entries) && i < start+limit; i++ {
		entry := top.Entries[i]
		out.Items = append(out.Items, leaderboard.PresentRow(entry.Rank, entry.UserID, entry.Name, entry.RawAvatar, entry.Value,
			entry.LevelName, settings.AllowAnonymous && entry.Anonymous, userID))
	}

	s.publicizeRows(ctx, out.Items)

	standing, err := s.pg.LeaderboardStanding(ctx, s.query(settings, spec, period, false), userID)
	if err != nil {
		return nil, s.translate(err)
	}
	out.Me = leaderboard.ClientMe{
		Rank: standing.Rank, Value: standing.Value, GapToPrev: standing.GapToPrev,
		InTop:     standing.Rank != nil && *standing.Rank <= settings.TopLimit,
		Anonymous: settings.AllowAnonymous && standing.Anonymous, Excluded: standing.Excluded,
		Name:        leaderboard.DisplayName(settings.Identity, standing.Nickname, standing.Account),
		LevelName:   levelNameFor(spec, standing.Level, standing.LevelName),
		AvatarOwner: userID, RawAvatar: standing.RawAvatar,
	}
	return out, nil
}

// Periods 周期榜的往期列表（已结算的快照），新的在前；my 只在我上过这一期快照时出现。
func (s *LeaderboardService) Periods(ctx context.Context, appID, userID int64, key string, limit int) (*leaderboard.ClientPeriods, error) {
	_, spec, err := s.openBoard(ctx, appID, key)
	if err != nil {
		return nil, err
	}
	out := &leaderboard.ClientPeriods{Items: []leaderboard.ClientPeriod{}}
	if !spec.Periodic() {
		return out, nil
	}
	if limit <= 0 {
		limit = 12
	}
	limit = min(limit, leaderboard.MaxPageLimit)
	snapshots, err := s.pg.ListLeaderboardPeriodSnapshots(ctx, appID, spec.Key, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.ID)
	}
	mine, err := s.pg.LeaderboardEntriesOf(ctx, appID, userID, ids)
	if err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		item := s.clientPeriod(spec, snapshot)
		if entry, ok := mine[snapshot.ID]; ok {
			item.My = &leaderboard.ClientMyPeriod{Rank: entry.Rank, Value: entry.Value, RewardStatus: entry.RewardStatus}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (s *LeaderboardService) clientPeriod(spec leaderboard.BoardSpec, snapshot leaderboard.Snapshot) leaderboard.ClientPeriod {
	period := leaderboard.Period{Kind: spec.Period, Key: snapshot.PeriodKey, Start: snapshot.PeriodStart.In(s.location()),
		End: snapshot.PeriodEnd.In(s.location())}
	return leaderboard.ClientPeriod{
		PeriodKey: snapshot.PeriodKey, PeriodLabel: period.FullLabel(), PeriodStart: snapshot.PeriodStart,
		PeriodEnd: snapshot.PeriodEnd, Participants: snapshot.Participants, SettledAt: snapshot.SettledAt,
	}
}

// Period 往期榜单：结算那一刻冻结的名次、我在这一期的名次与奖励、该期的奖励档位。
func (s *LeaderboardService) Period(ctx context.Context, appID, userID int64, key, periodKey string, page, limit int) (*leaderboard.ClientPeriodPage, error) {
	_, spec, err := s.openBoard(ctx, appID, key)
	if err != nil {
		return nil, err
	}
	if !spec.Periodic() {
		return nil, leaderboardSnapshotNotFoundError()
	}
	if _, err := leaderboard.ParsePeriodKey(spec.Period, periodKey, s.location()); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "期号格式不正确")
	}
	snapshot, err := s.pg.LeaderboardSnapshotByPeriod(ctx, appID, spec.Key, periodKey)
	if err != nil {
		return nil, s.translate(err)
	}
	if snapshot.Status != leaderboard.SnapshotSettled {
		return nil, leaderboardSnapshotNotFoundError()
	}
	entries, err := s.pg.ListLeaderboardSnapshotEntries(ctx, snapshot.ID, "", page, limit, leaderboard.MaxPageLimit, false)
	if err != nil {
		return nil, err
	}
	plans, features, err := s.refs(ctx, appID, len(snapshot.Tiers) > 0)
	if err != nil {
		return nil, err
	}
	period := leaderboard.Period{Kind: spec.Period, Key: snapshot.PeriodKey, Start: snapshot.PeriodStart.In(s.location()),
		End: snapshot.PeriodEnd.In(s.location())}
	tiers := describeTiers(snapshot.Tiers, plans, features)
	out := &leaderboard.ClientPeriodPage{
		Board: s.clientBoard(spec, period, tiers, s.now()), Period: s.clientPeriod(spec, *snapshot),
		Total: entries.Total, Page: entries.Page, Limit: entries.Limit, TotalPages: entries.TotalPages,
		Items: make([]leaderboard.ClientRow, 0, len(entries.Items)), Rewards: tiers,
	}
	for _, entry := range entries.Items {
		out.Items = append(out.Items, leaderboard.PresentRow(entry.Rank, entry.UserID, entry.DisplayName, entry.RawAvatar,
			entry.Value, entry.LevelName, entry.Anonymous, userID))
	}
	s.publicizeRows(ctx, out.Items)
	mine, err := s.pg.LeaderboardEntryOf(ctx, snapshot.ID, userID)
	if err != nil {
		return nil, err
	}
	if mine != nil {
		out.Me = &leaderboard.ClientMyPeriod{Rank: mine.Rank, Value: mine.Value, RewardStatus: mine.RewardStatus, Rewards: mine.RewardResults}
		out.Period.My = out.Me
	}
	return out, nil
}

// SetPublicIDs 接上全进程共用的对外 ID 换算。
func (s *LeaderboardService) SetPublicIDs(publicIDs *UserPublicIDs) {
	s.publicIDs = publicIDs
}

// publicizeRows 榜上每一行的 userId 换成对外 ID；换不到的（刚注销）干脆不给，按匿名那样省略。
func (s *LeaderboardService) publicizeRows(ctx context.Context, rows []leaderboard.ClientRow) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		if row.UserID != nil {
			ids = append(ids, *row.UserID)
		}
	}
	if len(ids) == 0 {
		return
	}
	public := s.publicIDs.Map(ctx, ids)
	for i := range rows {
		if rows[i].UserID == nil {
			continue
		}
		if id, ok := public[*rows[i].UserID]; ok {
			rows[i].UserID = &id
		} else {
			rows[i].UserID = nil
		}
	}
}

// MyRewards 我的榜单奖励记录（落在档位里的每一期），新的在前。不看排行榜是否仍开启：这是历史。
func (s *LeaderboardService) MyRewards(ctx context.Context, appID, userID int64, page, limit int) (*leaderboard.Page[leaderboard.ClientRewardRecord], error) {
	page, limit = leaderboard.NormalizePage(page, limit, 20, leaderboard.MaxPageLimit)
	records, total, err := s.pg.ListLeaderboardRewardRecords(ctx, appID, userID, page, limit)
	if err != nil {
		return nil, err
	}
	items := make([]leaderboard.ClientRewardRecord, 0, len(records))
	for _, record := range records {
		spec, _ := leaderboard.FindBoard(record.Board)
		period := leaderboard.Period{Kind: spec.Period, Key: record.PeriodKey, Start: record.PeriodStart.In(s.location())}
		items = append(items, leaderboard.ClientRewardRecord{
			Board: record.Board, BoardLabel: spec.Label, PeriodKey: record.PeriodKey, PeriodLabel: period.FullLabel(),
			Rank: record.Rank, Value: record.Value, Status: record.Status, Rewards: record.RewardResults, GrantedAt: record.GrantedAt,
		})
	}
	return &leaderboard.Page[leaderboard.ClientRewardRecord]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: leaderboard.TotalPages(total, limit)}, nil
}

// Preference 我的排行榜偏好。
func (s *LeaderboardService) Preference(ctx context.Context, appID, userID int64) (*leaderboard.ClientPreference, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	anonymous, err := s.pg.GetLeaderboardPreference(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return &leaderboard.ClientPreference{Anonymous: anonymous, AllowAnonymous: settings.AllowAnonymous}, nil
}

// UpdatePreference 开关匿名上榜。应用不允许匿名时只能关、不能开（40359）。
func (s *LeaderboardService) UpdatePreference(ctx context.Context, appID, userID int64, anonymous bool) (*leaderboard.ClientPreference, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if anonymous && !settings.AllowAnonymous {
		return nil, apperrors.New(errCodeLeaderboardAnonymousForbidden, http.StatusForbidden, "当前应用不允许匿名上榜")
	}
	if err := s.pg.SetLeaderboardPreference(ctx, appID, userID, anonymous); err != nil {
		return nil, s.translate(err)
	}
	return &leaderboard.ClientPreference{Anonymous: anonymous, AllowAnonymous: settings.AllowAnonymous}, nil
}

// ── 管理端：设置 ──

// AdminSettings 设置，连同榜单目录与权益目录（控制台表单由它驱动）。
func (s *LeaderboardService) AdminSettings(ctx context.Context, appID int64) (map[string]any, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"settings":           settings,
		"catalog":            leaderboard.Catalog(),
		"rewardCatalog":      invite.RewardCatalog(),
		"identities":         leaderboard.Identities(),
		"rewardStatuses":     leaderboard.RewardStatuses(),
		"integralCategories": leaderboard.IntegralCategories(),
		"defaults":           leaderboard.DefaultConfig(),
		"limits": map[string]any{
			"minTopLimit":      leaderboard.MinTopLimit,
			"maxTopLimit":      leaderboard.MaxTopLimit,
			"maxMinValue":      leaderboard.MaxMinValue,
			"maxTiers":         leaderboard.MaxTiers,
			"maxRewardsInTier": invite.MaxRewardsPerList,
			"maxReasonLength":  leaderboard.MaxReasonRunes,
			"maxExclusions":    leaderboard.MaxExclusionBatch,
		},
	}, nil
}

// SaveSettings 保存开关与配置。档位、奖励引用（试用套餐、未登记的功能）在保存时就核对。
func (s *LeaderboardService) SaveSettings(ctx context.Context, appID int64, enabled bool, cfg leaderboard.Config, operator string) (*leaderboard.Settings, error) {
	cfg = leaderboard.NormalizeConfig(cfg)
	if err := leaderboard.ValidateConfig(cfg); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if rewards := cfg.AllRewards(); len(rewards) > 0 {
		plans, features, err := s.pg.PointsStoreCatalogRefs(ctx, appID)
		if err != nil {
			return nil, err
		}
		for _, reward := range rewards {
			switch reward.Type {
			case pointstore.RewardVipPlan:
				plan, ok := plans[reward.RefID]
				if !ok {
					return nil, apperrors.New(40000, http.StatusBadRequest, "所选会员套餐不存在")
				}
				if plan.Trial {
					return nil, apperrors.New(40000, http.StatusBadRequest, "试用套餐不能作为排行榜奖励")
				}
			case pointstore.RewardFeature:
				if _, ok := features[reward.Tag]; !ok {
					return nil, apperrors.New(40000, http.StatusBadRequest,
						"功能标识 "+reward.Tag+" 未登记，请先在会员功能目录里创建它")
				}
			case pointstore.RewardCosmetic:
				if err := checkCosmeticRef(ctx, s.pg, appID, reward.RefID); err != nil {
					return nil, err
				}
			}
		}
	}
	return s.pg.SaveLeaderboardSettings(ctx, appID, enabled, cfg, operator)
}

// ── 管理端：榜单 ──

// AdminBoardPage 管理员看到的一页榜单。
type AdminBoardPage struct {
	Board leaderboard.ClientBoard `json:"board"`
	// Source live 实时计算 / snapshot 取自结算快照
	Source     string                   `json:"source"`
	Snapshot   *leaderboard.Snapshot    `json:"snapshot,omitempty"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	Limit      int                      `json:"limit"`
	TotalPages int                      `json:"totalPages"`
	Items      []leaderboard.AdminEntry `json:"items"`
}

// AdminBoard 管理员看实时榜或往期榜：行里带真实账号与用户 ID，并标出匿名、被移出。
// periodKey 为空看当前周期；给了期号且已结算时取快照，否则按那个周期实时算（结算前预览）。
// 实时榜不限 topLimit，被移出的人也列出来（不参与排名，rank 为空）。
func (s *LeaderboardService) AdminBoard(ctx context.Context, appID int64, key, periodKey string, page, limit int) (*AdminBoardPage, error) {
	spec, ok := leaderboard.FindBoard(key)
	if !ok {
		return nil, leaderboardNotFoundError()
	}
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	period := leaderboard.PeriodAt(spec.Period, now, s.location())
	if periodKey = strings.TrimSpace(periodKey); periodKey != "" {
		if period, err = leaderboard.ParsePeriodKey(spec.Period, periodKey, s.location()); err != nil {
			return nil, apperrors.New(40000, http.StatusBadRequest, "期号格式不正确")
		}
	}
	page, limit = leaderboard.NormalizePage(page, limit, 20, leaderboard.MaxAdminPageLimit)
	out := &AdminBoardPage{Source: "live", Page: page, Limit: limit, Items: []leaderboard.AdminEntry{}}
	if spec.Periodic() && periodKey != "" {
		snapshot, err := s.pg.LeaderboardSnapshotByPeriod(ctx, appID, spec.Key, period.Key)
		if err != nil && !errors.Is(err, pgrepo.ErrLeaderboardSnapshotNotFound) {
			return nil, err
		}
		if snapshot != nil {
			plans, features, err := s.refs(ctx, appID, len(snapshot.Tiers) > 0)
			if err != nil {
				return nil, err
			}
			entries, err := s.pg.ListLeaderboardSnapshotEntries(ctx, snapshot.ID, "", page, limit, leaderboard.MaxAdminPageLimit, true)
			if err != nil {
				return nil, err
			}
			out.Source, out.Snapshot = "snapshot", snapshot
			out.Board = s.clientBoard(spec, period, describeTiers(snapshot.Tiers, plans, features), now)
			out.Total, out.TotalPages = entries.Total, entries.TotalPages
			for _, entry := range entries.Items {
				rank := entry.Rank
				out.Items = append(out.Items, leaderboard.AdminEntry{
					Rank: &rank, UserID: entry.UserID, Account: entry.Account, Name: entry.DisplayName,
					RawAvatar: entry.RawAvatar, Value: entry.Value, Anonymous: entry.Anonymous, LevelName: entry.LevelName,
				})
			}
			return out, nil
		}
	}
	tiers := settings.ActiveTiers(spec.Key)
	plans, features, err := s.refs(ctx, appID, len(tiers) > 0)
	if err != nil {
		return nil, err
	}
	out.Board = s.clientBoard(spec, period, describeTiers(tiers, plans, features), now)
	slice, err := s.pg.LeaderboardRows(ctx, s.query(settings, spec, period, true), limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	out.Total, out.TotalPages = slice.Participants, leaderboard.TotalPages(slice.TotalRows, limit)
	for _, row := range slice.Rows {
		entry := leaderboard.AdminEntry{
			UserID: row.UserID, Account: row.Account, Nickname: row.Nickname,
			Name: leaderboard.DisplayName(settings.Identity, row.Nickname, row.Account), RawAvatar: row.RawAvatar,
			Value: row.Value, Anonymous: row.Anonymous, Excluded: row.Excluded,
			LevelName: levelNameFor(spec, row.Level, row.LevelName), LastAt: row.LastAt, Experience: row.Experience,
		}
		if !row.Excluded {
			rank := row.Rank
			entry.Rank = &rank
		}
		out.Items = append(out.Items, entry)
	}
	return out, nil
}

// ── 管理端：移出名单 ──

// ListExclusions 移出名单。
func (s *LeaderboardService) ListExclusions(ctx context.Context, appID int64, keyword string, page, limit int) (*leaderboard.Page[leaderboard.Exclusion], error) {
	return s.pg.ListLeaderboardExclusions(ctx, appID, keyword, page, limit)
}

// AddExclusions 移出一批人。对所有榜生效（含未结算的周期，缓存过期后可见）；已结算的快照不改写，
// 但其中尚未发放的奖励在发放时记为 skipped。
func (s *LeaderboardService) AddExclusions(ctx context.Context, appID int64, userIDs []int64, reason, operator string) (map[string]any, error) {
	ids := uniquePositiveIDs(userIDs)
	reason = strings.TrimSpace(reason)
	switch {
	case len(ids) == 0:
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要移出的用户")
	case len(ids) > leaderboard.MaxExclusionBatch:
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多移出 "+strconv.Itoa(leaderboard.MaxExclusionBatch)+" 人")
	case utf8.RuneCountInString(reason) > leaderboard.MaxReasonRunes:
		return nil, apperrors.New(40000, http.StatusBadRequest, "原因不能超过 "+strconv.Itoa(leaderboard.MaxReasonRunes)+" 字")
	}
	missing, err := s.pg.AddLeaderboardExclusions(ctx, appID, ids, reason, operator)
	if err != nil {
		return nil, err
	}
	if len(missing) == len(ids) {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return map[string]any{"added": len(ids) - len(missing), "missing": missing}, nil
}

// RemoveExclusion 把一个人移回排行榜。
func (s *LeaderboardService) RemoveExclusion(ctx context.Context, appID, userID int64) error {
	return s.translate(s.pg.RemoveLeaderboardExclusion(ctx, appID, userID))
}

// ── 管理端：结算 ──

// ListSnapshots 结算记录（期号、参与人数、发放统计）。
func (s *LeaderboardService) ListSnapshots(ctx context.Context, appID int64, board string, page, limit int) (*leaderboard.Page[leaderboard.Snapshot], error) {
	if board != "" {
		if _, ok := leaderboard.FindBoard(board); !ok {
			return nil, leaderboardNotFoundError()
		}
	}
	return s.pg.ListLeaderboardSnapshots(ctx, appID, board, page, limit)
}

// SnapshotEntries 结算明细（带真实账号、奖励状态、失败原因）。
func (s *LeaderboardService) SnapshotEntries(ctx context.Context, appID, snapshotID int64, status string, page, limit int) (map[string]any, error) {
	snapshot, err := s.pg.GetLeaderboardSnapshot(ctx, appID, snapshotID)
	if err != nil {
		return nil, s.translate(err)
	}
	if status != "" && !slices.Contains(leaderboard.RewardStatuses(), status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的奖励状态："+status)
	}
	entries, err := s.pg.ListLeaderboardSnapshotEntries(ctx, snapshot.ID, status, page, limit, leaderboard.MaxAdminPageLimit, true)
	if err != nil {
		return nil, err
	}
	stats, err := s.pg.LeaderboardSnapshotStats(ctx, snapshot.ID)
	if err != nil {
		return nil, err
	}
	snapshot.Stats = stats
	return map[string]any{"snapshot": snapshot, "entries": entries}, nil
}

// RetrySnapshot 把该期全部发放失败的条目置回待发放，由结算循环发放。
func (s *LeaderboardService) RetrySnapshot(ctx context.Context, appID, snapshotID int64) (int64, error) {
	count, err := s.pg.RetryLeaderboardSnapshot(ctx, appID, snapshotID)
	if err != nil {
		return 0, s.translate(err)
	}
	if count > 0 {
		s.wake()
	}
	return count, nil
}

// Settle 手动结算一个已结束的周期（与自动结算同一入口，幂等）：未结束 → 40922；已有快照直接返回它。
func (s *LeaderboardService) Settle(ctx context.Context, appID int64, board, periodKey, operator string) (*leaderboard.Snapshot, bool, error) {
	spec, ok := leaderboard.FindBoard(board)
	if !ok {
		return nil, false, leaderboardNotFoundError()
	}
	if !spec.Periodic() {
		return nil, false, apperrors.New(40000, http.StatusBadRequest, spec.Label+"不结算")
	}
	period, err := leaderboard.ParsePeriodKey(spec.Period, periodKey, s.location())
	if err != nil {
		return nil, false, apperrors.New(40000, http.StatusBadRequest, "期号格式不正确")
	}
	if !period.Ended(s.now()) {
		return nil, false, apperrors.New(errCodeLeaderboardPeriodNotEnded, http.StatusConflict, "这一期还没有结束，不能结算")
	}
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, false, err
	}
	snapshot, created, err := s.settle(ctx, settings, spec, period, operator)
	if err != nil {
		return nil, false, err
	}
	if created {
		s.wake()
	}
	return snapshot, created, nil
}

func (s *LeaderboardService) settle(ctx context.Context, settings *leaderboard.Settings, spec leaderboard.BoardSpec, period leaderboard.Period, operator string) (*leaderboard.Snapshot, bool, error) {
	tiers := settings.ActiveTiers(spec.Key)
	plans, features, err := s.refs(ctx, settings.AppID, len(tiers) > 0)
	if err != nil {
		return nil, false, err
	}
	return s.pg.SettleLeaderboardPeriod(ctx, pgrepo.LeaderboardSettleInput{
		Query: s.query(settings, spec, period, false), TopLimit: settings.TopLimit, Identity: settings.Identity,
		AllowAnonymous: settings.AllowAnonymous, Tiers: tiers, Plans: plans, Features: features, Operator: operator,
	})
}

// LeaderboardBoardStats 控制台概览里的一个榜。
type LeaderboardBoardStats struct {
	Key           string                   `json:"key"`
	Label         string                   `json:"label"`
	Enabled       bool                     `json:"enabled"`
	PeriodKey     string                   `json:"periodKey"`
	PeriodLabel   string                   `json:"periodLabel"`
	Participants  int64                    `json:"participants"`
	Top           []leaderboard.AdminEntry `json:"top"`
	LastSettledAt *time.Time               `json:"lastSettledAt"`
}

// Stats 控制台概览：各榜参与人数、本期前 3、最近一次结算时间；移出人数与待发 / 失败的奖励数。
func (s *LeaderboardService) Stats(ctx context.Context, appID int64) (map[string]any, error) {
	settings, err := s.pg.GetLeaderboardSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	latest, err := s.pg.LatestLeaderboardSettlements(ctx, appID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	boards := []LeaderboardBoardStats{}
	for _, board := range settings.Boards {
		spec, ok := leaderboard.FindBoard(board.Key)
		if !ok {
			continue
		}
		period := leaderboard.PeriodAt(spec.Period, now, s.location())
		item := LeaderboardBoardStats{Key: spec.Key, Label: spec.Label, Enabled: board.Enabled, PeriodKey: period.Key,
			PeriodLabel: period.Label(now, s.location()), Top: []leaderboard.AdminEntry{}}
		if at, ok := latest[spec.Key]; ok {
			item.LastSettledAt = &at
		}
		if board.Enabled {
			slice, err := s.pg.LeaderboardRows(ctx, s.query(settings, spec, period, false), 3, 0)
			if err != nil {
				return nil, err
			}
			item.Participants = slice.Participants
			for _, row := range slice.Rows {
				rank := row.Rank
				item.Top = append(item.Top, leaderboard.AdminEntry{
					Rank: &rank, UserID: row.UserID, Account: row.Account, Nickname: row.Nickname,
					Name: leaderboard.DisplayName(settings.Identity, row.Nickname, row.Account), RawAvatar: row.RawAvatar,
					Value: row.Value, Anonymous: row.Anonymous, LevelName: levelNameFor(spec, row.Level, row.LevelName),
				})
			}
		}
		boards = append(boards, item)
	}
	exclusions, err := s.pg.ListLeaderboardExclusions(ctx, appID, "", 1, 1)
	if err != nil {
		return nil, err
	}
	pending, failed, err := s.pg.LeaderboardRewardBacklog(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"enabled": settings.Enabled, "boards": boards, "exclusions": exclusions.Total,
		"pendingRewards": pending, "failedRewards": failed,
	}, nil
}

// ════════════════════════════════════════════════════════════
//  后台循环：周期结算与奖励发放
// ════════════════════════════════════════════════════════════

// Start 起后台循环：每分钟结算到期的上一个周期（只看紧挨着的上一个，不补历史）、发放待发的奖励。
// 多实例同时跑靠快照的唯一约束与 SKIP LOCKED，不会重复结算、重复发奖。
func (s *LeaderboardService) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("leaderboard loop panic", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-s.kick:
				s.tick(ctx)
			case <-timer.C:
				s.tick(ctx)
				timer.Reset(leaderboardTickInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *LeaderboardService) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// wake 让循环尽快跑一轮（手动结算、重试之后）。循环没启动时什么都不做。
func (s *LeaderboardService) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// tick 跑一轮；一轮里的 panic 只丢掉这一轮，循环照旧。
func (s *LeaderboardService) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("leaderboard tick panic", zap.Any("panic", r))
		}
	}()
	settled, granted, err := s.RunOnce(ctx)
	if err != nil {
		s.log.Warn("排行榜结算失败", zap.Error(err))
	}
	if settled > 0 || granted > 0 {
		s.log.Info("排行榜结算", zap.Int("settled", settled), zap.Int("granted", granted))
	}
}

// RunOnce 结算到期的周期、发放一批待发的奖励。返回新生成的快照数与本轮处理的奖励份数。
func (s *LeaderboardService) RunOnce(ctx context.Context) (int, int, error) {
	settled, err := s.SettleDue(ctx)
	granted, grantErr := s.GrantPending(ctx, leaderboardGrantBatch)
	if err == nil {
		err = grantErr
	}
	return settled, granted, err
}

// SettleDue 对每个开启的应用、每个开启的周期榜，结算已过「结束 + 5 分钟宽限」且还没有快照的上一个周期。
func (s *LeaderboardService) SettleDue(ctx context.Context) (int, error) {
	all, err := s.pg.ListEnabledLeaderboardSettings(ctx)
	if err != nil {
		return 0, err
	}
	now := s.now()
	created := 0
	var firstErr error
	for i := range all {
		settings := &all[i]
		for _, spec := range settings.EnabledBoards() {
			if !spec.Periodic() {
				continue
			}
			previous := leaderboard.PeriodAt(spec.Period, now, s.location()).Previous()
			if now.Before(previous.End.Add(leaderboard.SettleGrace)) {
				continue
			}
			exists, err := s.pg.LeaderboardSnapshotExists(ctx, settings.AppID, spec.Key, previous.Key)
			if err != nil {
				firstErr = cmpErr(firstErr, err)
				continue
			}
			if exists {
				continue
			}
			if _, ok, err := s.settle(ctx, settings, spec, previous, ""); err != nil {
				s.log.Warn("结算排行榜失败", zap.Int64("appid", settings.AppID), zap.String("board", spec.Key),
					zap.String("period", previous.Key), zap.Error(err))
				firstErr = cmpErr(firstErr, err)
			} else if ok {
				created++
			}
		}
	}
	return created, firstErr
}

func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

// GrantPending 发放至多 batch 份待发的奖励（每人一个事务），成功的发站内信。返回处理的份数。
func (s *LeaderboardService) GrantPending(ctx context.Context, batch int) (int, error) {
	processed := 0
	for processed < batch {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		outcome, err := s.pg.GrantNextLeaderboardReward(ctx)
		if err != nil {
			return processed, err
		}
		if outcome == nil {
			return processed, nil
		}
		processed++
		switch outcome.Entry.RewardStatus {
		case leaderboard.RewardGranted:
			s.notifyGranted(ctx, outcome)
		case leaderboard.RewardFailed:
			s.log.Warn("排行榜奖励发放失败", zap.Int64("appid", outcome.Snapshot.AppID), zap.Int64("snapshotId", outcome.Snapshot.ID),
				zap.Int64("userId", outcome.Entry.UserID), zap.String("reason", outcome.Entry.RewardError))
		}
	}
	return processed, nil
}

// notifyGranted 奖励到账的站内信。站内信失败不回滚奖励（奖励已经提交）。
func (s *LeaderboardService) notifyGranted(ctx context.Context, outcome *pgrepo.LeaderboardGrantOutcome) {
	if s.notifier == nil {
		return
	}
	spec, _ := leaderboard.FindBoard(outcome.Snapshot.Board)
	period := leaderboard.Period{Kind: spec.Period, Key: outcome.Snapshot.PeriodKey, Start: outcome.Snapshot.PeriodStart.In(s.location())}
	content := "你在" + period.FullLabel() + spec.Label + "获得第 " + strconv.Itoa(outcome.Entry.Rank) + " 名，奖励已到账"
	if detail := describeResults(outcome.Entry.RewardResults); detail != "" {
		content += "：" + detail
	}
	content += "。"
	meta := map[string]any{"scene": "leaderboard", "snapshotId": outcome.Snapshot.ID, "board": outcome.Snapshot.Board,
		"periodKey": outcome.Snapshot.PeriodKey, "rank": outcome.Entry.Rank}
	notifyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := s.notifier.Deliver(notifyCtx, notificationdomain.DeliverInput{
		AppID: outcome.Snapshot.AppID, UserID: outcome.Entry.UserID, Type: "leaderboard",
		Title: "排行榜奖励已到账", Content: content, Level: "success", Metadata: meta,
		Action: notificationdomain.RouteAction(notificationdomain.RouteLeaderboard, "查看排行榜"),
	}); err != nil {
		s.log.Warn("leaderboard: notify user failed", zap.Int64("appid", outcome.Snapshot.AppID),
			zap.Int64("userId", outcome.Entry.UserID), zap.Error(err))
	}
}
