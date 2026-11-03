package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"aegis/internal/domain/growth"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// GrowthService 成长调控（docs/growth-control.md）：积分与经验获取速度的奖惩规则、等级封顶，以及用户详情页的调控总览。
type GrowthService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	userEvents UserEventPublisher
}

// NewGrowthService 新建服务。
func NewGrowthService(log *zap.Logger, pg *pgrepo.Repository) *GrowthService {
	if log == nil {
		log = zap.NewNop()
	}
	return &GrowthService{log: log, pg: pg}
}

// SetEventPublisher 规则与封顶变化后推送给在线的用户端（刷新等级页）。
func (s *GrowthService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

// GrowthCatalog 控制台表单用的目录。
type GrowthCatalog struct {
	Sources   []growth.SourceSpec   `json:"sources"`
	Roundings []growth.RoundingSpec `json:"roundings"`
	Presets   []growth.Preset       `json:"presets"`
	MaxLevel  int                   `json:"maxLevel"`
	Limits    map[string]int64      `json:"limits"`
}

// Catalog 来源、取整方式、常用倍率、等级上限与各项数值边界。
func (s *GrowthService) Catalog(ctx context.Context, appID int64) (*GrowthCatalog, error) {
	scheme, _, err := s.pg.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &GrowthCatalog{
		Sources: growth.Sources(), Roundings: growth.Roundings(), Presets: growth.Presets(), MaxLevel: scheme.MaxLevel(),
		Limits: map[string]int64{
			"maxFactorTerm": growth.MaxFactorTerm, "maxDailyCap": growth.MaxDailyCap,
			"maxActiveRules": growth.MaxActiveRulesPerTarget, "maxReason": growth.MaxReasonRunes, "maxLabel": growth.MaxLabelRunes,
		},
	}, nil
}

// GrowthKindView 某一类型此刻对这个人的综合效果。
type GrowthKindView struct {
	Kind growth.Kind `json:"kind"`
	// Factor 默认来源（如签到）上的综合倍率；BySource 每个来源各自的综合结果
	Factor     growth.Factor             `json:"factor"`
	Label      string                    `json:"label"`
	Rounding   growth.Rounding           `json:"rounding"`
	DailyCap   int64                     `json:"dailyCap"`
	UsedToday  int64                     `json:"usedToday"`
	BySource   []GrowthSourceEffect      `json:"bySource"`
	Stats      []pgrepo.GrowthSourceStat `json:"stats"`
	LiveRules  int                       `json:"liveRules"`
	Restricted bool                      `json:"restricted"`
	Boosted    bool                      `json:"boosted"`
}

// GrowthSourceEffect 一个来源上叠乘后的结果。
type GrowthSourceEffect struct {
	Source   string          `json:"source"`
	Label    string          `json:"label"`
	Factor   growth.Factor   `json:"factor"`
	Display  string          `json:"display"`
	Rounding growth.Rounding `json:"rounding"`
	DailyCap int64           `json:"dailyCap"`
	RuleIDs  []int64         `json:"ruleIds"`
}

// GrowthProfile 用户详情页「奖惩」页签的全部数据。
type GrowthProfile struct {
	UserID     int64                    `json:"userId"`
	Integral   GrowthKindView           `json:"integral"`
	Experience GrowthKindView           `json:"experience"`
	Rules      []growth.Rule            `json:"rules"`
	AppRules   []growth.Rule            `json:"appRules"`
	Level      *pgrepo.GrowthLevelState `json:"level"`
	Usage      []growth.Usage           `json:"usage"`
	Events     []growth.Event           `json:"events"`
	StatsDays  int                      `json:"statsDays"`
	ComputedAt time.Time                `json:"computedAt"`
}

// Profile 一个人的调控总览：规则、各来源的综合倍率、今天的上限进度、近 30 天各来源收入、等级封顶、调控记录。
// 八组查询互不依赖，并行执行（连接池足够，单次总耗时 ≈ 最慢的一条）。
func (s *GrowthService) Profile(ctx context.Context, appID, userID int64) (*GrowthProfile, error) {
	const statsDays, usageDays = 30, 14
	out := &GrowthProfile{UserID: userID, StatsDays: statsDays, ComputedAt: time.Now()}
	var (
		userRules, appRules     *growth.Page[growth.Rule]
		liveIntegral, liveExp   []growth.Rule
		usedIntegral, usedExp   int64
		statsIntegral, statsExp []pgrepo.GrowthSourceStat
		events                  *growth.Page[growth.Event]
	)
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() (err error) {
		out.Level, err = s.pg.GrowthLevel(gctx, appID, userID)
		return
	})
	group.Go(func() (err error) {
		userRules, err = s.pg.ListGrowthRules(gctx, growth.RuleQuery{AppID: appID, UserID: userID, Scope: "user", Limit: 200})
		return
	})
	group.Go(func() (err error) {
		appRules, err = s.pg.ListGrowthRules(gctx, growth.RuleQuery{AppID: appID, Scope: "app", Status: "live", Limit: 200})
		return
	})
	group.Go(func() (err error) {
		if liveIntegral, err = s.pg.LiveGrowthRules(gctx, appID, userID, growth.KindIntegral); err != nil {
			return
		}
		liveExp, err = s.pg.LiveGrowthRules(gctx, appID, userID, growth.KindExperience)
		return
	})
	group.Go(func() (err error) {
		if usedIntegral, err = s.pg.GrowthTodayGranted(gctx, appID, userID, growth.KindIntegral); err != nil {
			return
		}
		usedExp, err = s.pg.GrowthTodayGranted(gctx, appID, userID, growth.KindExperience)
		return
	})
	group.Go(func() (err error) {
		statsIntegral, err = s.pg.GrowthSourceStats(gctx, appID, userID, growth.KindIntegral, statsDays)
		return
	})
	group.Go(func() (err error) {
		statsExp, err = s.pg.GrowthSourceStats(gctx, appID, userID, growth.KindExperience, statsDays)
		return
	})
	group.Go(func() (err error) {
		out.Usage, err = s.pg.GrowthUsage(gctx, appID, userID, usageDays)
		return
	})
	group.Go(func() (err error) {
		events, err = s.pg.ListGrowthEvents(gctx, appID, userID, 1, 30)
		return
	})
	if err := group.Wait(); err != nil {
		return nil, s.translate(err)
	}
	out.Rules, out.AppRules, out.Events = userRules.Items, appRules.Items, events.Items
	out.Integral = kindView(growth.KindIntegral, liveIntegral, usedIntegral, statsIntegral, out.ComputedAt)
	out.Experience = kindView(growth.KindExperience, liveExp, usedExp, statsExp, out.ComputedAt)
	return out, nil
}

func kindView(kind growth.Kind, live []growth.Rule, used int64, stats []pgrepo.GrowthSourceStat, now time.Time) GrowthKindView {
	view := GrowthKindView{Kind: kind, UsedToday: used, Stats: stats, LiveRules: len(live), BySource: []GrowthSourceEffect{}}
	headline := growth.Combine(live, growth.SourceSignin, now)
	if kind == growth.KindExperience || len(headline.RuleIDs) == 0 {
		// 签到也发经验；没有作用于签到的规则时，用第一个受影响的来源当代表
		for _, spec := range growth.Sources() {
			if c := growth.Combine(live, spec.Key, now); len(c.RuleIDs) > 0 && len(headline.RuleIDs) == 0 {
				headline = c
			}
		}
	}
	view.Factor, view.Rounding, view.DailyCap = headline.Factor, headline.Rounding, headline.DailyCap
	view.Label = headline.Factor.String()
	for _, spec := range growth.Sources() {
		if !containsKind(spec.Kinds, kind) {
			continue
		}
		c := growth.Combine(live, spec.Key, now)
		view.BySource = append(view.BySource, GrowthSourceEffect{
			Source: spec.Key, Label: spec.Label, Factor: c.Factor, Display: c.Factor.String(), Rounding: c.Rounding,
			DailyCap: c.DailyCap, RuleIDs: c.RuleIDs,
		})
		if c.Factor.Num < c.Factor.Den || c.DailyCap > 0 {
			view.Restricted = true
		}
		if c.Factor.Num > c.Factor.Den {
			view.Boosted = true
		}
	}
	return view
}

func containsKind(kinds []growth.Kind, kind growth.Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ListRules 规则列表。
func (s *GrowthService) ListRules(ctx context.Context, query growth.RuleQuery) (*growth.Page[growth.Rule], error) {
	page, err := s.pg.ListGrowthRules(ctx, query)
	return page, s.translate(err)
}

// GetRule 一条规则。
func (s *GrowthService) GetRule(ctx context.Context, appID, ruleID int64) (*growth.Rule, error) {
	rule, err := s.pg.GetGrowthRule(ctx, appID, ruleID)
	return rule, s.translate(err)
}

// CreateRule 新建规则（UserID 为 0 时作用于全应用）。
func (s *GrowthService) CreateRule(ctx context.Context, in growth.RuleInput) (*growth.Rule, error) {
	if err := in.Normalize(time.Now()); err != nil {
		return nil, s.translate(err)
	}
	rule, err := s.pg.CreateGrowthRule(ctx, in)
	if err != nil {
		return nil, s.translate(err)
	}
	s.notify(in.AppID, in.UserID)
	return rule, nil
}

// UpdateRule 修改规则。
func (s *GrowthService) UpdateRule(ctx context.Context, ruleID int64, in growth.RuleInput) (*growth.Rule, error) {
	current, err := s.pg.GetGrowthRule(ctx, in.AppID, ruleID)
	if err != nil {
		return nil, s.translate(err)
	}
	in.UserID, in.Kind = current.UserID, current.Kind
	if err := in.Normalize(time.Now()); err != nil {
		return nil, s.translate(err)
	}
	rule, err := s.pg.UpdateGrowthRule(ctx, ruleID, in)
	if err != nil {
		return nil, s.translate(err)
	}
	s.notify(in.AppID, rule.UserID)
	return rule, nil
}

// RevokeRule 撤销规则。
func (s *GrowthService) RevokeRule(ctx context.Context, appID, ruleID int64, operator, note string) (*growth.Rule, error) {
	rule, err := s.pg.RevokeGrowthRule(ctx, appID, ruleID, operator, note)
	if err != nil {
		return nil, s.translate(err)
	}
	s.notify(appID, rule.UserID)
	return rule, nil
}

// DeleteRule 删除规则。
func (s *GrowthService) DeleteRule(ctx context.Context, appID, ruleID int64, operator string) error {
	return s.translate(s.pg.DeleteGrowthRule(ctx, appID, ruleID, operator))
}

// ListEvents 调控记录。
func (s *GrowthService) ListEvents(ctx context.Context, appID, userID int64, page, limit int) (*growth.Page[growth.Event], error) {
	result, err := s.pg.ListGrowthEvents(ctx, appID, userID, page, limit)
	return result, s.translate(err)
}

// GrowthPreviewInput 推演：一笔 base 的收入在当前规则（可再叠上一条草稿规则）下会入账多少。
type GrowthPreviewInput struct {
	AppID  int64
	UserID int64
	Kind   growth.Kind
	Source string
	Base   int64
	Draft  *growth.RuleInput
	// ExcludeRuleID 编辑已有规则时，用草稿替换掉它
	ExcludeRuleID int64
}

// GrowthPreview 推演结果。随机取整给出期望值；Before 不含草稿，After 含草稿。
type GrowthPreview struct {
	Source        string         `json:"source"`
	Base          int64          `json:"base"`
	Before        growth.Outcome `json:"before"`
	After         growth.Outcome `json:"after"`
	ExpectedAfter float64        `json:"expectedAfter"`
	UsedToday     int64          `json:"usedToday"`
}

// Preview 推演。
func (s *GrowthService) Preview(ctx context.Context, in GrowthPreviewInput) (*GrowthPreview, error) {
	if !in.Kind.Valid() {
		return nil, apperrors.New(40000, http.StatusBadRequest, "类型只能是积分或经验")
	}
	if _, ok := growth.FindSource(in.Source); !ok {
		in.Source = growth.SourceSignin
	}
	if in.Base <= 0 || in.Base > growth.MaxDailyCap {
		return nil, apperrors.New(40000, http.StatusBadRequest, "推演数量需要在 1–"+strconv.FormatInt(growth.MaxDailyCap, 10)+" 之间")
	}
	var live []growth.Rule
	var used int64
	group, gctx := errgroup.WithContext(ctx)
	group.Go(func() (err error) {
		if in.UserID > 0 {
			live, err = s.pg.LiveGrowthRules(gctx, in.AppID, in.UserID, in.Kind)
		} else {
			page, listErr := s.pg.ListGrowthRules(gctx, growth.RuleQuery{AppID: in.AppID, Scope: "app", Kind: in.Kind, Status: growth.StatusActive, Limit: 200})
			if listErr == nil {
				live = page.Items
			}
			err = listErr
		}
		return
	})
	group.Go(func() (err error) {
		if in.UserID > 0 {
			used, err = s.pg.GrowthTodayGranted(gctx, in.AppID, in.UserID, in.Kind)
		}
		return
	})
	if err := group.Wait(); err != nil {
		return nil, s.translate(err)
	}
	now := time.Now()
	before := growth.Combine(live, in.Source, now)
	withDraft := make([]growth.Rule, 0, len(live)+1)
	for _, rule := range live {
		if rule.ID != in.ExcludeRuleID || in.ExcludeRuleID == 0 {
			withDraft = append(withDraft, rule)
		}
	}
	if in.Draft != nil {
		draft := *in.Draft
		draft.AppID, draft.UserID, draft.Kind = in.AppID, in.UserID, in.Kind
		if err := draft.Normalize(now); err != nil {
			return nil, s.translate(err)
		}
		withDraft = append(withDraft, growth.Rule{
			ID: -1, AppID: in.AppID, UserID: max(in.UserID, 0), Kind: in.Kind, Numerator: draft.Numerator, Denominator: draft.Denominator,
			Rounding: draft.Rounding, DailyCap: draft.DailyCap, Sources: draft.Sources, Enabled: true, CreatedAt: now.Add(time.Second),
		})
	}
	after := growth.Combine(withDraft, in.Source, now)
	result := &GrowthPreview{
		Source: in.Source, Base: in.Base, UsedToday: used,
		Before: growth.Apply(in.Base, before, used, in.Source, nil),
		After:  growth.Apply(in.Base, after, used, in.Source, nil),
	}
	result.ExpectedAfter = float64(result.After.Granted)
	if after.Rounding == growth.RoundRandom && after.Factor.Den > 0 {
		expected := float64(in.Base) * float64(after.Factor.Num) / float64(after.Factor.Den)
		if after.DailyCap > 0 {
			expected = min(expected, float64(max(after.DailyCap-used, 0)))
		}
		result.ExpectedAfter = expected
	}
	return result, nil
}

// SetLevelCap 等级封顶（可同时降级）。
func (s *GrowthService) SetLevelCap(ctx context.Context, appID, userID int64, level int, demote bool, operator, note string) (*pgrepo.GrowthLevelState, error) {
	state, err := s.pg.SetGrowthLevelCap(ctx, appID, userID, level, demote, operator, note)
	if err != nil {
		return nil, s.translate(err)
	}
	s.publish(appID, userID, "level.changed", map[string]any{"level": state.Level, "cappedLevel": state.Capped})
	return state, nil
}

// ClearLevelCap 解除等级封顶。
func (s *GrowthService) ClearLevelCap(ctx context.Context, appID, userID int64, operator, note string) (*pgrepo.GrowthLevelState, error) {
	state, err := s.pg.ClearGrowthLevelCap(ctx, appID, userID, operator, note)
	if err != nil {
		return nil, s.translate(err)
	}
	s.publish(appID, userID, "level.changed", map[string]any{"level": state.Level, "cappedLevel": 0})
	return state, nil
}

func (s *GrowthService) notify(appID, userID int64) {
	if userID > 0 {
		s.publish(appID, userID, "growth.changed", map[string]any{})
	}
}

func (s *GrowthService) publish(appID, userID int64, event string, data map[string]any) {
	if s.userEvents == nil || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.userEvents.PublishUserEvent(ctx, appID, userID, event, data)
	}()
}

func (s *GrowthService) translate(err error) error {
	var invalid *growth.ValidationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &invalid):
		return apperrors.New(40000, http.StatusBadRequest, invalid.Message)
	case errors.Is(err, pgrepo.ErrGrowthRuleMissing):
		return apperrors.New(40400, http.StatusNotFound, "规则不存在")
	case errors.Is(err, pgrepo.ErrGrowthRuleRevoked):
		return apperrors.New(40900, http.StatusConflict, "规则已撤销，不能再修改")
	case errors.Is(err, pgrepo.ErrGrowthRuleLimit):
		return apperrors.New(40900, http.StatusConflict, "同一对象同一类型最多 "+strconv.Itoa(growth.MaxActiveRulesPerTarget)+" 条未结束的规则，先撤销不用的")
	case errors.Is(err, pgrepo.ErrGrowthLevelAboveCap):
		return apperrors.New(40900, http.StatusConflict, "当前等级高于封顶等级，勾选「同时降级」后再提交")
	case errors.Is(err, pgrepo.ErrGrowthLevelCapRange):
		return apperrors.New(40000, http.StatusBadRequest, "封顶等级超出当前等级体系的范围")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40400, http.StatusNotFound, "用户不存在")
	}
	return err
}
