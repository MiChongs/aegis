package service

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/usernumber"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
)

// 号码的业务错误码（docs/user-number.md）。
const (
	errCodeUserNumberDisabled     = 42980 // 403 应用没开放换号
	errCodeUserNumberInvalid      = 42981 // 400 号码格式或位数不对
	errCodeUserNumberTaken        = 42982 // 409 已被别人使用
	errCodeUserNumberHeld         = 42983 // 409 保留号 / 冻结中 / 摇号锁定中
	errCodeUserNumberCooldown     = 42984 // 429 换号冷却中
	errCodeUserNumberNoRight      = 42985 // 403 没有能换这个等级的权益
	errCodeUserNumberSelfPickOff  = 42986 // 403 应用只允许摇号
	errCodeUserNumberNoDraw       = 42987 // 403 摇号次数不足
	errCodeUserNumberDrawNotFound = 42988 // 404 摇号单不存在、已关闭或已过期
	errCodeUserNumberNotCandidate = 42989 // 400 号码不在这张摇号单里
	errCodeUserNumberSame         = 42950 // 400 与当前号码相同
	errCodeUserNumberNoCandidates = 42951 // 409 一个空闲的候选号都没摇到（次数不扣）
	errCodeUserNumberHistory      = 42952 // 404 换号记录不存在
	errCodeUserNumberBalance      = 42953 // 400 扣减会让余额变负
	errCodeUserNumberUser         = 42954 // 404 用户不存在
)

// numberLuckActivity 号运在每日运气哈希里的「活动号」：与任何抽奖活动错开，号运与抽奖运气各是一份。
const numberLuckActivity int64 = -1

// UserNumberService 用户号码：查看、查号、联想、摇号、换号（用户端）与设置、指定、回滚、保留号、权益调整（管理端）。
//
// 权益的发放不在这里：各渠道在自己的事务里调用仓储的 grantUserNumberRightTx。
type UserNumberService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	search   *AdminUserSearchService
	now      func() time.Time
}

func NewUserNumberService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *UserNumberService {
	if log == nil {
		log = zap.NewNop()
	}
	return &UserNumberService{log: log, pg: pg, sessions: sessions, now: time.Now}
}

// SetAdminUserSearchService 换号后刷新管理端搜索索引。
func (s *UserNumberService) SetAdminUserSearchService(search *AdminUserSearchService) {
	s.search = search
}

func newNumberRand() *rand.Rand {
	var seed [32]byte
	_, _ = crand.Read(seed[:])
	return rand.New(rand.NewChaCha8(seed))
}

// ── 视图 ──

// UserNumberRules 用户端看到的规则。
type UserNumberRules struct {
	MinLength       int  `json:"minLength"`
	MaxLength       int  `json:"maxLength"`
	DefaultLength   int  `json:"defaultLength"`
	SelfPick        bool `json:"selfPick"`
	CooldownDays    int  `json:"cooldownDays"`
	FreezeDays      int  `json:"freezeDays"`
	DrawCandidates  int  `json:"drawCandidates"`
	DrawHoldMinutes int  `json:"drawHoldMinutes"`
}

// UserNumberPatternView 一条生效的规律。
type UserNumberPatternView struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Example string `json:"example"`
	Tier    int    `json:"tier"`
}

// UserNumberLuck 今日号运：系数、档位文案，以及乘过号运之后各级出现的概率。
type UserNumberLuck struct {
	*lotterydomain.FortuneReading
	// Odds 各级在一次摇号里出现的概率（百分比，下标即等级）
	Odds [usernumber.TierCount]float64 `json:"odds"`
	// BaseOdds 不乘号运时的概率
	BaseOdds [usernumber.TierCount]float64 `json:"baseOdds"`
}

// UserNumberView 我的号码页。
type UserNumberView struct {
	Enabled      bool                    `json:"enabled"`
	Number       int64                   `json:"number"`
	Tier         int                     `json:"tier"`
	TierName     string                  `json:"tierName"`
	Patterns     []string                `json:"patterns"`
	PatternNames []string                `json:"patternNames"`
	ChangedAt    *time.Time              `json:"changedAt,omitempty"`
	NextChangeAt *time.Time              `json:"nextChangeAt,omitempty"`
	Rights       usernumber.Rights       `json:"rights"`
	Draw         *usernumber.Draw        `json:"draw,omitempty"`
	Luck         UserNumberLuck          `json:"luck"`
	Frozen       []usernumber.Hold       `json:"frozen"`
	Rules        UserNumberRules         `json:"rules"`
	Tiers        []usernumber.TierSpec   `json:"tiers"`
	Catalog      []UserNumberPatternView `json:"catalog"`
}

// UserNumberCheck 一个号码对当前用户来说怎么样。
type UserNumberCheck struct {
	Number       int64    `json:"number"`
	Valid        bool     `json:"valid"`
	Available    bool     `json:"available"`
	Reason       string   `json:"reason"`
	Message      string   `json:"message"`
	Tier         int      `json:"tier"`
	TierName     string   `json:"tierName"`
	Patterns     []string `json:"patterns"`
	PatternNames []string `json:"patternNames"`
	// OwnFrozen 自己冻结中的旧号，换回免费
	OwnFrozen bool `json:"ownFrozen,omitempty"`
	// NeedKind / NeedLevel / NeedName 最低要什么权益
	NeedKind  string `json:"needKind,omitempty"`
	NeedLevel int    `json:"needLevel"`
	NeedName  string `json:"needName,omitempty"`
	// CanChange 现在就能换（空闲 + 有够用的权益或是自己的冻结号 + 不在冷却 + 应用允许自选）
	CanChange bool `json:"canChange"`
	// Block 号码空闲但现在换不了的原因：disabled 未开放、cooldown 冷却中、self_pick_off 只能摇号、
	// no_right 没有够用的权益；能换或号码本身不可用时为空
	Block string `json:"block,omitempty"`
	// UseKind / UseLevel / UseName 现在换会扣哪一份权益（服务端自动选的那份；CanChange 且不是冻结号时才有）
	UseKind  string `json:"useKind,omitempty"`
	UseLevel int    `json:"useLevel,omitempty"`
	UseName  string `json:"useName,omitempty"`
}

// 号码空闲但现在换不了的原因（UserNumberCheck.Block）。
const (
	numberBlockDisabled    = "disabled"
	numberBlockCooldown    = "cooldown"
	numberBlockSelfPickOff = "self_pick_off"
	numberBlockNoRight     = "no_right"
)

// numberGate 一个人此刻的换号条件：一次读齐，查号与联想的每一项共用。
type numberGate struct {
	enabled  bool
	selfPick bool
	cooling  bool
	rights   usernumber.Rights
}

func (s *UserNumberService) gate(ctx context.Context, appID, userID int64, st *usernumber.Settings) (numberGate, error) {
	g := numberGate{enabled: st.Enabled, selfPick: st.SelfPick}
	rec, err := s.pg.GetUserNumber(ctx, appID, userID)
	if err != nil {
		return g, s.mapErr(err)
	}
	g.cooling = nextChangeAt(*st, rec.ChangedAt, s.now()) != nil
	if g.rights, err = s.pg.ListUserNumberRights(ctx, appID, userID); err != nil {
		return g, err
	}
	return g, nil
}

// apply 给一个空闲号码填上 CanChange / Block / Use*：与 ChangeUserNumber 的判断顺序一致
// （冷却 → 自己的冻结号免费 → 只能摇号 → 自动选权益）。
func (g numberGate) apply(out *UserNumberCheck) {
	out.CanChange, out.Block, out.UseKind, out.UseLevel, out.UseName = false, "", "", 0, ""
	switch {
	case !out.Available:
		return
	case !g.enabled:
		out.Block = numberBlockDisabled
	case g.cooling:
		out.Block = numberBlockCooldown
	case out.OwnFrozen:
		out.CanChange = true
	case !g.selfPick:
		out.Block = numberBlockSelfPickOff
	default:
		kind, level, ok := usernumber.PickRight(g.rights, out.Tier)
		if !ok {
			out.Block = numberBlockNoRight
			return
		}
		out.CanChange = true
		out.UseKind, out.UseLevel, out.UseName = kind, level, usernumber.RightName(kind, level)
	}
}

// 查号结论。
const (
	numberReasonAvailable = "available"
	numberReasonFormat    = "invalid_format"
	numberReasonLength    = "invalid_length"
	numberReasonSelf      = "self"
	numberReasonTaken     = "taken"
	numberReasonReserved  = "reserved"
	numberReasonFrozen    = "frozen"
	numberReasonLocked    = "locked"
)

func (s *UserNumberService) settings(ctx context.Context, appID int64) (usernumber.Settings, error) {
	return s.pg.GetUserNumberSettings(ctx, appID)
}

func rulesOf(st usernumber.Settings) UserNumberRules {
	return UserNumberRules{MinLength: st.MinLength, MaxLength: st.MaxLength, DefaultLength: st.DefaultLength, SelfPick: st.SelfPick,
		CooldownDays: st.CooldownDays, FreezeDays: st.FreezeDays, DrawCandidates: st.DrawCandidates, DrawHoldMinutes: st.DrawHoldMinutes}
}

func patternNames(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, usernumber.PatternName(k))
	}
	return out
}

func catalogOf(st *usernumber.Settings) []UserNumberPatternView {
	out := []UserNumberPatternView{}
	for _, p := range usernumber.Patterns(st) {
		if p.Tier == usernumber.PatternDisabled {
			continue
		}
		out = append(out, UserNumberPatternView{Key: p.Key, Name: p.Name, Example: p.Example, Tier: p.Tier})
	}
	return out
}

// luck 这个人今天的号运（每日运气模型 + 抽奖的至尊加成）。号运关闭时系数为 1、没有解读。
func (s *UserNumberService) luck(ctx context.Context, appID, userID int64, st *usernumber.Settings) UserNumberLuck {
	out := UserNumberLuck{BaseOdds: st.TierOdds(1)}
	out.Odds = out.BaseOdds
	if st.LuckSpread <= 0 {
		return out
	}
	fortune := &lotterydomain.Fortune{Spread: st.LuckSpread}
	day := lotteryDayStart(s.now())
	reading := lotterydomain.ReadFortune(fortune, numberLuckActivity, userID, day)
	if reading == nil {
		return out
	}
	if st.LuckSupreme {
		if app, err := s.pg.GetLotteryAppSettings(ctx, appID); err == nil && app.Supreme.Enabled {
			if holdings, err := s.pg.GetVipHoldings(ctx, appID, userID); err == nil {
				if outcome := lotterydomain.ResolveSupreme(&app.Supreme, holdings, reading.Factor); outcome != nil {
					reading = lotterydomain.ApplySupremeReading(reading, fortune, &app.Supreme, outcome)
				}
			} else {
				s.log.Warn("load vip holdings for number luck failed", zap.Int64("userId", userID), zap.Error(err))
			}
		}
	}
	out.FortuneReading = reading
	out.Odds = st.TierOdds(reading.Factor)
	return out
}

func (s *UserNumberService) luckFactor(l UserNumberLuck) float64 {
	if l.FortuneReading == nil {
		return 1
	}
	return l.Factor
}

func nextChangeAt(st usernumber.Settings, changedAt *time.Time, now time.Time) *time.Time {
	if st.CooldownDays <= 0 || changedAt == nil {
		return nil
	}
	until := changedAt.Add(time.Duration(st.CooldownDays) * 24 * time.Hour)
	if !now.Before(until) {
		return nil
	}
	return &until
}

// Me 我的号码页：号码与等级、冷却、权益、打开的摇号单、今日号运、冻结中的旧号与规则。
func (s *UserNumberService) Me(ctx context.Context, appID, userID int64) (*UserNumberView, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	rec, err := s.pg.GetUserNumber(ctx, appID, userID)
	if err != nil {
		return nil, s.mapErr(err)
	}
	rights, err := s.pg.ListUserNumberRights(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	draw, err := s.pg.GetOpenUserNumberDraw(ctx, appID, userID, now)
	if err != nil {
		return nil, err
	}
	cls := usernumber.Classify(rec.Number, &st)
	view := &UserNumberView{
		Enabled: st.Enabled, Number: rec.Number, Tier: cls.Tier, TierName: cls.TierName,
		Patterns: cls.Patterns, PatternNames: patternNames(cls.Patterns),
		ChangedAt: rec.ChangedAt, NextChangeAt: nextChangeAt(st, rec.ChangedAt, now),
		Rights: rights, Draw: draw, Luck: s.luck(ctx, appID, userID, &st),
		Frozen: []usernumber.Hold{}, Rules: rulesOf(st), Tiers: usernumber.Tiers(), Catalog: catalogOf(&st),
	}
	if frozen, err := s.ownFrozen(ctx, appID, userID); err == nil {
		view.Frozen = frozen
	}
	return view, nil
}

// ownFrozen 自己冻结中的旧号（最近换下来的最多 10 个）。
func (s *UserNumberService) ownFrozen(ctx context.Context, appID, userID int64) ([]usernumber.Hold, error) {
	holds, _, err := s.pg.ListUserNumberHolds(ctx, appID, usernumber.HoldFrozen, 500, 0)
	if err != nil {
		return nil, err
	}
	out := []usernumber.Hold{}
	for _, h := range holds {
		if h.UserID != nil && *h.UserID == userID {
			out = append(out, h)
			if len(out) == 10 {
				break
			}
		}
	}
	return out, nil
}

// Check 查一个号码：合法不合法、等级、有没有人用、要什么权益、现在能不能换。
func (s *UserNumberService) Check(ctx context.Context, appID, userID int64, raw string) (*UserNumberCheck, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &UserNumberCheck{Patterns: []string{}, PatternNames: []string{}}
	number, err := usernumber.ParseNumber(strings.TrimSpace(raw))
	if err != nil {
		out.Reason, out.Message = numberReasonFormat, "号码须为纯数字且不以 0 开头"
		return out, nil
	}
	out.Number = number
	if err := st.ValidNumber(number); err != nil {
		out.Reason = numberReasonLength
		out.Message = "号码须为 " + strconv.Itoa(st.MinLength) + "–" + strconv.Itoa(st.MaxLength) + " 位"
		return out, nil
	}
	out.Valid = true
	cls := usernumber.Classify(number, &st)
	out.Tier, out.TierName, out.Patterns, out.PatternNames = cls.Tier, cls.TierName, cls.Patterns, patternNames(cls.Patterns)
	kind, level := usernumber.Requirement(cls.Tier)
	out.NeedKind, out.NeedLevel, out.NeedName = kind, level, usernumber.RightName(kind, level)

	avail, err := s.pg.CheckUserNumber(ctx, appID, number)
	if err != nil {
		return nil, err
	}
	switch availErr := avail.Err(userID, false); {
	case availErr == nil:
		out.Available, out.Reason, out.Message = true, numberReasonAvailable, "这个号码可以使用"
		out.OwnFrozen = avail.OwnFrozen(userID)
		if out.OwnFrozen {
			out.Message = "这是你之前用过的号码，冻结期内可以免费换回"
		}
	case errors.Is(availErr, pgrepo.ErrUserNumberSame):
		out.Reason, out.Message = numberReasonSelf, "这就是你现在的号码"
	case errors.Is(availErr, pgrepo.ErrUserNumberTaken):
		out.Reason, out.Message = numberReasonTaken, "这个号码已被使用"
	case errors.Is(availErr, pgrepo.ErrUserNumberReserved):
		out.Reason, out.Message = numberReasonReserved, "这个号码暂不开放"
	case errors.Is(availErr, pgrepo.ErrUserNumberFrozen):
		out.Reason, out.Message = numberReasonFrozen, "这个号码冻结中，暂不可用"
	case errors.Is(availErr, pgrepo.ErrUserNumberLocked):
		out.Reason, out.Message = numberReasonLocked, "这个号码正被别人摇中锁定"
	}
	if !out.Available {
		return out, nil
	}
	g, err := s.gate(ctx, appID, userID, &st)
	if err != nil {
		return nil, err
	}
	g.apply(out)
	return out, nil
}

// Suggest 自选联想：含 part 的空闲号码（part 为空时按 tier 造号，tier < 0 时给普通号）。
func (s *UserNumberService) Suggest(ctx context.Context, appID, userID int64, part string, tier, count int) ([]UserNumberCheck, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	count = min(max(count, 1), 30)
	part = strings.TrimSpace(part)
	for i := 0; i < len(part); i++ {
		if part[i] < '0' || part[i] > '9' {
			return nil, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "只能输入数字")
		}
	}
	if len(part) > st.MaxLength {
		return nil, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码最长 "+strconv.Itoa(st.MaxLength)+" 位")
	}
	r := newNumberRand()
	var raw []int64
	switch {
	case part != "":
		raw = usernumber.ContainingNumbers(part, count*3, &st, r)
	default:
		if !usernumber.ValidTier(tier) {
			tier = usernumber.TierCommon
		}
		seen := map[int64]bool{}
		for i := 0; i < count*4 && len(raw) < count*3; i++ {
			if n := usernumber.NumberOfTier(tier, &st, r); n > 0 && !seen[n] {
				seen[n] = true
				raw = append(raw, n)
			}
		}
	}
	free, err := s.pg.FreeUserNumbers(ctx, appID, raw)
	if err != nil {
		return nil, err
	}
	// 联想的每一项与查号同一套判断：之前这里不填 CanChange，客户端点了联想就一律显示「权益不足」
	g, err := s.gate(ctx, appID, userID, &st)
	if err != nil {
		return nil, err
	}
	out := []UserNumberCheck{}
	for _, n := range free {
		if len(out) == count {
			break
		}
		cls := usernumber.Classify(n, &st)
		if part == "" && usernumber.ValidTier(tier) && cls.Tier != tier {
			continue
		}
		kind, level := usernumber.Requirement(cls.Tier)
		item := UserNumberCheck{Number: n, Valid: true, Available: true, Reason: numberReasonAvailable,
			Message: "这个号码可以使用", Tier: cls.Tier, TierName: cls.TierName, Patterns: cls.Patterns,
			PatternNames: patternNames(cls.Patterns), NeedKind: kind, NeedLevel: level, NeedName: usernumber.RightName(kind, level)}
		g.apply(&item)
		out = append(out, item)
	}
	return out, nil
}

// UserNumberDrawResult 摇号结果。
type UserNumberDrawResult struct {
	Draw   *usernumber.Draw  `json:"draw"`
	Luck   UserNumberLuck    `json:"luck"`
	Rights usernumber.Rights `json:"rights"`
}

// Draw 摇一次号：按乘过号运的权重逐个抽等级、按等级造号，占住 N 个空闲的候选号，扣一次摇号次数。
func (s *UserNumberService) Draw(ctx context.Context, appID, userID int64) (*UserNumberDrawResult, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, apperrors.New(errCodeUserNumberDisabled, http.StatusForbidden, "号码更换暂未开放")
	}
	luck := s.luck(ctx, appID, userID, &st)
	factor := s.luckFactor(luck)
	r := newNumberRand()
	want := st.DrawCandidates
	seen := map[int64]bool{}
	var pool []usernumber.Candidate
	for i := 0; i < want*4 && len(pool) < want*3; i++ {
		tier := st.RollTier(factor, r)
		n := usernumber.NumberOfTier(tier, &st, r)
		for n == 0 && tier > usernumber.TierCommon {
			tier--
			n = usernumber.NumberOfTier(tier, &st, r)
		}
		if n == 0 || seen[n] {
			continue
		}
		seen[n] = true
		cls := usernumber.Classify(n, &st)
		pool = append(pool, usernumber.Candidate{Number: n, Tier: cls.Tier, Patterns: cls.Patterns})
	}
	numbers := make([]int64, len(pool))
	for i, c := range pool {
		numbers[i] = c.Number
	}
	free, err := s.pg.FreeUserNumbers(ctx, appID, numbers)
	if err != nil {
		return nil, err
	}
	freeSet := map[int64]bool{}
	for _, n := range free {
		freeSet[n] = true
	}
	candidates := pool[:0]
	for _, c := range pool {
		if freeSet[c.Number] {
			candidates = append(candidates, c)
		}
	}
	draw, err := s.pg.CreateUserNumberDraw(ctx, appID, userID, candidates, want, factor, st, s.now())
	if err != nil {
		return nil, s.mapErr(err)
	}
	rights, err := s.pg.ListUserNumberRights(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return &UserNumberDrawResult{Draw: draw, Luck: luck, Rights: rights}, nil
}

// DiscardDraw 放弃打开的摇号单（次数不退）。
func (s *UserNumberService) DiscardDraw(ctx context.Context, appID, userID, drawID int64) error {
	return s.mapErr(s.pg.DiscardUserNumberDraw(ctx, appID, userID, drawID, s.now()))
}

// Change 换号：drawID > 0 从摇号单里选；否则换回自己的冻结号，或用改号卡 / 靓号券自选。
func (s *UserNumberService) Change(ctx context.Context, appID, userID int64, raw string, drawID int64, useKind string, useLevel int) (*usernumber.ChangeResult, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, apperrors.New(errCodeUserNumberDisabled, http.StatusForbidden, "号码更换暂未开放")
	}
	number, err := s.parseValid(raw, &st)
	if err != nil {
		return nil, err
	}
	cls := usernumber.Classify(number, &st)
	result, err := s.pg.ChangeUserNumber(ctx, usernumber.ChangeRequest{AppID: appID, UserID: userID, Number: number, DrawID: drawID,
		UseKind: strings.TrimSpace(useKind), UseLevel: useLevel}, st, cls.Tier, s.now())
	if err != nil {
		return nil, s.mapErr(err)
	}
	result.Patterns = cls.Patterns
	s.afterChange(appID, userID)
	return result, nil
}

func (s *UserNumberService) parseValid(raw string, st *usernumber.Settings) (int64, error) {
	number, err := usernumber.ParseNumber(strings.TrimSpace(raw))
	if err != nil {
		return 0, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码须为纯数字且不以 0 开头")
	}
	if err := st.ValidNumber(number); err != nil {
		return 0, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest,
			"号码须为 "+strconv.Itoa(st.MinLength)+"–"+strconv.Itoa(st.MaxLength)+" 位")
	}
	return number, nil
}

// History 我的换号记录。
func (s *UserNumberService) History(ctx context.Context, appID, userID int64, page, size int) ([]usernumber.History, int64, error) {
	limit, offset := numberPage(page, size)
	items, total, err := s.pg.ListUserNumberHistory(ctx, appID, userID, limit, offset)
	for i := range items {
		items[i].Operator = "" // 用户端不给管理员账号
	}
	return items, total, err
}

// Ledger 我的权益流水。
func (s *UserNumberService) Ledger(ctx context.Context, appID, userID int64, page, size int) ([]usernumber.LedgerEntry, int64, error) {
	limit, offset := numberPage(page, size)
	items, total, err := s.pg.ListUserNumberLedger(ctx, appID, userID, "", limit, offset)
	for i := range items {
		items[i].Operator = ""
	}
	return items, total, err
}

func numberPage(page, size int) (int, int) {
	if size <= 0 || size > 100 {
		size = 20
	}
	if page <= 0 {
		page = 1
	}
	return size, (page - 1) * size
}

// afterChange 换号之后：/me 与资料缓存作废、管理端搜索索引刷新。
func (s *UserNumberService) afterChange(appID, userID int64) {
	if s.sessions != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.sessions.DeleteMyView(ctx, appID, userID)
		_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
		cancel()
	}
	if s.search != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.search.IndexUser(ctx, appID, userID); err != nil {
				s.log.Warn("sync admin user search after number change failed", zap.Int64("userId", userID), zap.Error(err))
			}
		}()
	}
}

// AfterGrantAssign 物品发放「指定号码」在别的事务里换了号之后调它刷新缓存。
func (s *UserNumberService) AfterGrantAssign(appID, userID int64) { s.afterChange(appID, userID) }

func (s *UserNumberService) mapErr(err error) error {
	if err == nil {
		return nil
	}
	var cooldown *pgrepo.UserNumberCooldownError
	switch {
	case errors.As(err, &cooldown):
		return apperrors.New(errCodeUserNumberCooldown, http.StatusTooManyRequests,
			"换号太频繁，"+cooldown.Until.In(lotteryDayStart(s.now()).Location()).Format("2006-01-02 15:04")+" 之后才能再换")
	case errors.Is(err, pgrepo.ErrUserNumberUserNotFound):
		return apperrors.New(errCodeUserNumberUser, http.StatusNotFound, "用户不存在")
	case errors.Is(err, pgrepo.ErrUserNumberSame):
		return apperrors.New(errCodeUserNumberSame, http.StatusBadRequest, "这就是你现在的号码")
	case errors.Is(err, pgrepo.ErrUserNumberTaken):
		return apperrors.New(errCodeUserNumberTaken, http.StatusConflict, "这个号码已被使用")
	case errors.Is(err, pgrepo.ErrUserNumberReserved):
		return apperrors.New(errCodeUserNumberHeld, http.StatusConflict, "这个号码暂不开放")
	case errors.Is(err, pgrepo.ErrUserNumberFrozen):
		return apperrors.New(errCodeUserNumberHeld, http.StatusConflict, "这个号码冻结中，暂不可用")
	case errors.Is(err, pgrepo.ErrUserNumberLocked):
		return apperrors.New(errCodeUserNumberHeld, http.StatusConflict, "这个号码正被别人摇中锁定")
	case errors.Is(err, pgrepo.ErrUserNumberNoRight):
		return apperrors.New(errCodeUserNumberNoRight, http.StatusForbidden, "没有能换这个等级号码的权益")
	case errors.Is(err, pgrepo.ErrUserNumberSelfPickOff):
		return apperrors.New(errCodeUserNumberSelfPickOff, http.StatusForbidden, "本应用只能通过摇号更换号码")
	case errors.Is(err, pgrepo.ErrUserNumberNoDrawRight):
		return apperrors.New(errCodeUserNumberNoDraw, http.StatusForbidden, "摇号次数不足")
	case errors.Is(err, pgrepo.ErrUserNumberDrawNotFound):
		return apperrors.New(errCodeUserNumberDrawNotFound, http.StatusNotFound, "摇号结果已过期，请重新摇号")
	case errors.Is(err, pgrepo.ErrUserNumberNotCandidate):
		return apperrors.New(errCodeUserNumberNotCandidate, http.StatusBadRequest, "这个号码不在本次摇号结果里")
	case errors.Is(err, pgrepo.ErrUserNumberNoCandidates):
		return apperrors.New(errCodeUserNumberNoCandidates, http.StatusConflict, "号码池暂时紧张，请稍后再摇（次数未扣）")
	case errors.Is(err, pgrepo.ErrUserNumberHistoryMissing):
		return apperrors.New(errCodeUserNumberHistory, http.StatusNotFound, "换号记录不存在")
	case errors.Is(err, pgrepo.ErrUserNumberBalance):
		return apperrors.New(errCodeUserNumberBalance, http.StatusBadRequest, "余额不足，不能扣到负数")
	}
	return err
}

// ── 管理端 ──

// UserNumberCatalog 管理端目录：等级、规律（带本应用的等级）、权益种类、流水来源。
type UserNumberCatalog struct {
	Tiers    []usernumber.TierSpec  `json:"tiers"`
	Patterns []usernumber.Pattern   `json:"patterns"`
	Rights   []usernumber.Right     `json:"rights"`
	Origins  []UserNumberOriginView `json:"origins"`
	Limits   UserNumberLimits       `json:"limits"`
}

// UserNumberOriginView 一个流水来源。
type UserNumberOriginView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// UserNumberLimits 设置项的边界。
type UserNumberLimits struct {
	HardMinLength    int `json:"hardMinLength"`
	HardMaxLength    int `json:"hardMaxLength"`
	MinDefaultLength int `json:"minDefaultLength"`
}

// Catalog 管理端目录。
func (s *UserNumberService) Catalog(ctx context.Context, appID int64) (*UserNumberCatalog, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &UserNumberCatalog{Tiers: usernumber.Tiers(), Patterns: usernumber.Patterns(&st),
		Limits: UserNumberLimits{HardMinLength: usernumber.HardMinLength, HardMaxLength: usernumber.HardMaxLength,
			MinDefaultLength: usernumber.MinDefaultLength}}
	out.Rights = append(out.Rights, usernumber.Right{Kind: usernumber.RightChange, Name: usernumber.RightName(usernumber.RightChange, 0)})
	for level := usernumber.TierFine; level <= usernumber.TierSupreme; level++ {
		out.Rights = append(out.Rights, usernumber.Right{Kind: usernumber.RightTicket, Level: level, Name: usernumber.RightName(usernumber.RightTicket, level)})
	}
	out.Rights = append(out.Rights, usernumber.Right{Kind: usernumber.RightDraw, Name: usernumber.RightName(usernumber.RightDraw, 0)})
	for _, key := range []string{usernumber.OriginItemGrant, usernumber.OriginLevel, usernumber.OriginCardKey, usernumber.OriginPointsStore,
		usernumber.OriginLottery, usernumber.OriginInvite, usernumber.OriginLeaderboard, usernumber.OriginPromo, usernumber.OriginRewardedAd,
		usernumber.OriginAdmin, usernumber.OriginChange, usernumber.OriginDraw} {
		out.Origins = append(out.Origins, UserNumberOriginView{Key: key, Label: usernumber.OriginLabel(key)})
	}
	return out, nil
}

// Settings 管理端读设置。
func (s *UserNumberService) Settings(ctx context.Context, appID int64) (*usernumber.Settings, error) {
	st, err := s.settings(ctx, appID)
	return &st, err
}

// SaveSettings 保存设置。
func (s *UserNumberService) SaveSettings(ctx context.Context, appID int64, st usernumber.Settings, operator string) (*usernumber.Settings, error) {
	st.Normalize()
	if err := st.Validate(); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.pg.SaveUserNumberSettings(ctx, appID, st, operator); err != nil {
		return nil, err
	}
	return s.Settings(ctx, appID)
}

// UserNumberInspect 管理端查一个号码。
type UserNumberInspect struct {
	usernumber.Classification
	Valid        bool             `json:"valid"`
	PatternNames []string         `json:"patternNames"`
	OwnerID      int64            `json:"ownerId,omitempty"`
	OwnerAccount string           `json:"ownerAccount,omitempty"`
	OwnerName    string           `json:"ownerName,omitempty"`
	Hold         *usernumber.Hold `json:"hold,omitempty"`
}

// Inspect 一个号码的等级、谁在用、有没有占用。
func (s *UserNumberService) Inspect(ctx context.Context, appID int64, raw string) (*UserNumberInspect, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	number, err := usernumber.ParseNumber(strings.TrimSpace(raw))
	if err != nil {
		return nil, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码须为纯数字且不以 0 开头")
	}
	cls := usernumber.Classify(number, &st)
	out := &UserNumberInspect{Classification: cls, Valid: st.ValidNumber(number) == nil, PatternNames: patternNames(cls.Patterns)}
	avail, err := s.pg.CheckUserNumber(ctx, appID, number)
	if err != nil {
		return nil, err
	}
	out.Hold = avail.Hold
	if avail.OwnerID > 0 {
		out.OwnerID = avail.OwnerID
		if user, err := s.pg.GetAdminUserByApp(ctx, appID, avail.OwnerID); err == nil && user != nil {
			out.OwnerAccount, out.OwnerName = user.Account, user.Nickname
		}
	}
	return out, nil
}

// UserNumberAdminUser 管理端一个用户的号码详情。
type UserNumberAdminUser struct {
	UserID       int64                `json:"userId"`
	Number       int64                `json:"number"`
	Tier         int                  `json:"tier"`
	TierName     string               `json:"tierName"`
	Patterns     []string             `json:"patterns"`
	PatternNames []string             `json:"patternNames"`
	ChangedAt    *time.Time           `json:"changedAt,omitempty"`
	NextChangeAt *time.Time           `json:"nextChangeAt,omitempty"`
	Rights       usernumber.Rights    `json:"rights"`
	Draw         *usernumber.Draw     `json:"draw,omitempty"`
	Frozen       []usernumber.Hold    `json:"frozen"`
	History      []usernumber.History `json:"history"`
	Luck         UserNumberLuck       `json:"luck"`
}

// AdminUser 一个用户的号码、权益、打开的摇号单、冻结的旧号与最近的换号记录。
func (s *UserNumberService) AdminUser(ctx context.Context, appID, userID int64) (*UserNumberAdminUser, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	rec, err := s.pg.GetUserNumber(ctx, appID, userID)
	if err != nil {
		return nil, s.mapErr(err)
	}
	rights, err := s.pg.ListUserNumberRights(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	draw, err := s.pg.GetOpenUserNumberDraw(ctx, appID, userID, now)
	if err != nil {
		return nil, err
	}
	history, _, err := s.pg.ListUserNumberHistory(ctx, appID, userID, 20, 0)
	if err != nil {
		return nil, err
	}
	cls := usernumber.Classify(rec.Number, &st)
	out := &UserNumberAdminUser{UserID: userID, Number: rec.Number, Tier: cls.Tier, TierName: cls.TierName,
		Patterns: cls.Patterns, PatternNames: patternNames(cls.Patterns), ChangedAt: rec.ChangedAt,
		NextChangeAt: nextChangeAt(st, rec.ChangedAt, now), Rights: rights, Draw: draw, History: history,
		Frozen: []usernumber.Hold{}, Luck: s.luck(ctx, appID, userID, &st)}
	if frozen, err := s.ownFrozen(ctx, appID, userID); err == nil {
		out.Frozen = frozen
	}
	return out, nil
}

// Assign 管理员给用户指定号码（可以是保留号；不扣权益、不看冷却、不受应用开关影响）。
func (s *UserNumberService) Assign(ctx context.Context, appID, userID int64, raw string, force bool, note, operator string) (*usernumber.History, error) {
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	number, err := usernumber.ParseNumber(strings.TrimSpace(raw))
	if err != nil {
		return nil, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码须为纯数字且不以 0 开头")
	}
	if number < 10000 || number > 999999999999 {
		return nil, apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码须为 5–12 位")
	}
	cls := usernumber.Classify(number, &st)
	h, err := s.pg.AssignUserNumber(ctx, usernumber.AssignRequest{AppID: appID, UserID: userID, Number: number, Force: force,
		Source: usernumber.SourceAdmin, Operator: operator, Note: note}, st, cls.Tier, s.now())
	if err != nil {
		return nil, s.mapErr(err)
	}
	s.afterChange(appID, userID)
	return h, nil
}

// Rollback 回滚到某次换号之前的号码（那个号仍空闲或是这个人冻结中的才行；管理员可以覆盖保留与冻结）。
func (s *UserNumberService) Rollback(ctx context.Context, appID, userID, historyID int64, operator string) (*usernumber.History, error) {
	entry, err := s.pg.UserNumberHistoryEntry(ctx, appID, userID, historyID)
	if err != nil {
		return nil, s.mapErr(err)
	}
	if entry.OldNumber == nil {
		return nil, apperrors.New(errCodeUserNumberHistory, http.StatusBadRequest, "这次换号之前没有号码，无法回滚")
	}
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	cls := usernumber.Classify(*entry.OldNumber, &st)
	h, err := s.pg.AssignUserNumber(ctx, usernumber.AssignRequest{AppID: appID, UserID: userID, Number: *entry.OldNumber, Force: true,
		Source: usernumber.SourceRollback, Operator: operator, Note: "回滚记录 #" + strconv.FormatInt(historyID, 10)}, st, cls.Tier, s.now())
	if err != nil {
		return nil, s.mapErr(err)
	}
	s.afterChange(appID, userID)
	return h, nil
}

// AdjustRight 管理员调整权益余额。
func (s *UserNumberService) AdjustRight(ctx context.Context, appID, userID int64, kind string, level int, delta int64, note, operator string) (*usernumber.Rights, error) {
	if !usernumber.ValidRight(kind, level) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "权益种类或等级不合法")
	}
	if delta == 0 || delta > 100000 || delta < -100000 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "调整数量须在 ±100000 之间且不为 0")
	}
	if _, err := s.pg.GetUserNumber(ctx, appID, userID); err != nil {
		return nil, s.mapErr(err)
	}
	if _, err := s.pg.AdjustUserNumberRight(ctx, appID, userID, kind, level, delta, operator, note); err != nil {
		return nil, s.mapErr(err)
	}
	rights, err := s.pg.ListUserNumberRights(ctx, appID, userID)
	return &rights, err
}

// AdminHistory 换号记录（userID 为 0 时全应用）。
func (s *UserNumberService) AdminHistory(ctx context.Context, appID, userID int64, page, size int) ([]usernumber.History, int64, error) {
	limit, offset := numberPage(page, size)
	return s.pg.ListUserNumberHistory(ctx, appID, userID, limit, offset)
}

// AdminLedger 权益流水。
func (s *UserNumberService) AdminLedger(ctx context.Context, appID, userID int64, kind string, page, size int) ([]usernumber.LedgerEntry, int64, error) {
	limit, offset := numberPage(page, size)
	return s.pg.ListUserNumberLedger(ctx, appID, userID, kind, limit, offset)
}

// AdminDraws 摇号单。
func (s *UserNumberService) AdminDraws(ctx context.Context, appID, userID int64, page, size int) ([]usernumber.Draw, int64, error) {
	limit, offset := numberPage(page, size)
	return s.pg.ListUserNumberDraws(ctx, appID, userID, limit, offset)
}

// Holds 占用列表。
func (s *UserNumberService) Holds(ctx context.Context, appID int64, kind string, page, size int) ([]usernumber.Hold, int64, error) {
	if kind != "" && !usernumber.ValidHoldKind(kind) {
		return nil, 0, apperrors.New(40000, http.StatusBadRequest, "占用种类不合法")
	}
	limit, offset := numberPage(page, size)
	return s.pg.ListUserNumberHolds(ctx, appID, kind, limit, offset)
}

// UserNumberReserveResult 保留号码的结果。
type UserNumberReserveResult struct {
	Reserved []int64 `json:"reserved"`
	// Skipped 已有人在用或格式不对，没保留
	Skipped []string `json:"skipped"`
}

// Reserve 把一批号码设为保留号（最多 500 个；已有人在用的跳过）。
func (s *UserNumberService) Reserve(ctx context.Context, appID int64, raws []string, note string, expiresAt *time.Time, operator string) (*UserNumberReserveResult, error) {
	if len(raws) == 0 || len(raws) > 500 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次保留 1–500 个号码")
	}
	st, err := s.settings(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &UserNumberReserveResult{Reserved: []int64{}, Skipped: []string{}}
	var holds []usernumber.Hold
	seen := map[int64]bool{}
	for _, raw := range raws {
		n, err := usernumber.ParseNumber(strings.TrimSpace(raw))
		if err != nil || n < 10000 || n > 999999999999 || seen[n] {
			if strings.TrimSpace(raw) != "" && !seen[n] {
				out.Skipped = append(out.Skipped, raw)
			}
			continue
		}
		seen[n] = true
		holds = append(holds, usernumber.Hold{Number: n, Tier: usernumber.Classify(n, &st).Tier, Note: note, ExpiresAt: expiresAt})
	}
	added, err := s.pg.ReserveUserNumbers(ctx, appID, holds, operator)
	if err != nil {
		return nil, err
	}
	addedSet := map[int64]bool{}
	for _, n := range added {
		addedSet[n] = true
	}
	for _, h := range holds {
		if addedSet[h.Number] {
			out.Reserved = append(out.Reserved, h.Number)
		} else {
			out.Skipped = append(out.Skipped, strconv.FormatInt(h.Number, 10))
		}
	}
	return out, nil
}

// Release 删掉一条占用（保留、冻结或候选）。
func (s *UserNumberService) Release(ctx context.Context, appID int64, raw string) error {
	n, err := usernumber.ParseNumber(strings.TrimSpace(raw))
	if err != nil {
		return apperrors.New(errCodeUserNumberInvalid, http.StatusBadRequest, "号码须为纯数字且不以 0 开头")
	}
	ok, err := s.pg.ReleaseUserNumberHold(ctx, appID, n)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40400, http.StatusNotFound, "这个号码没有占用")
	}
	return nil
}

// Overview 管理端总览。
func (s *UserNumberService) Overview(ctx context.Context, appID int64) (*usernumber.Overview, error) {
	return s.pg.UserNumberOverview(ctx, appID, lotteryDayStart(s.now()))
}

// PreviewOdds 管理端预览：给一个号运系数，各级出现的概率（保存前试算）。
func (s *UserNumberService) PreviewOdds(st usernumber.Settings, luck float64) [usernumber.TierCount]float64 {
	st.Normalize()
	return st.TierOdds(luck)
}

// encodeSeed 仅供测试固定随机源。
func encodeSeed(a, b uint64) [32]byte {
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:8], a)
	binary.LittleEndian.PutUint64(seed[8:16], b)
	return seed
}
