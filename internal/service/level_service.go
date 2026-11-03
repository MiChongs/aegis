package service

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 等级体系（docs/levels.md）：每个应用一套画布，草稿 → 发布（留版本）；升级经过的达成奖励由后台逐条发放，
// 升级通知由后台发送；发布后可以起一个重算任务让全体用户按新体系判定一次。

const (
	levelTickInterval  = 10 * time.Second
	levelJobBatch      = 200
	levelJobStaleAfter = 3 * time.Minute
	levelRewardBatch   = 50
	levelRewardWorkers = 4
)

// LevelService 等级体系。
type LevelService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	notifier   notificationDeliverer
	userEvents UserEventPublisher

	stop     chan struct{}
	stopOnce sync.Once
	kick     chan struct{}
	// active 本实例正在跑的任务（接管心跳超时的任务时跳过自己的）
	active sync.Map
}

// NewLevelService 新建服务，并让仓储在写入待发奖励 / 待发通知后唤醒后台循环。
func NewLevelService(log *zap.Logger, pg *pgrepo.Repository) *LevelService {
	s := &LevelService{log: log, pg: pg, stop: make(chan struct{}), kick: make(chan struct{}, 1)}
	if pg != nil {
		pg.SetLevelWorkHook(s.wake)
	}
	return s
}

// SetNotifier 升级通知走站内信。
func (s *LevelService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 奖励到账、升级后通知在线设备刷新。
func (s *LevelService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

func levelLocation() *time.Location { return timeutil.DefaultLocation() }

func levelDayStart(now time.Time) time.Time {
	local := now.In(levelLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

// ── 目录 ──

// LevelCatalog 控制台画布需要的全部目录。
type LevelCatalog struct {
	Nodes        []levels.NodeSpec              `json:"nodes"`
	Categories   []levels.CategorySpec          `json:"categories"`
	Privileges   []levels.PrivilegeSpec         `json:"privileges"`
	Icons        []levels.IconSpec              `json:"icons"`
	Items        []itemgrant.ItemSpec           `json:"items"`
	ItemGroups   []itemgrant.GroupSpec          `json:"itemGroups"`
	Fields       []itemgrant.FieldSpec          `json:"fields"`
	Operators    []itemgrant.OperatorSpec       `json:"operators"`
	Routes       []notificationdomain.RouteSpec `json:"routes"`
	Plans        []itemgrant.PlanRef            `json:"plans"`
	Features     []itemgrant.FeatureRef         `json:"features"`
	Templates    []levels.Template              `json:"templates"`
	Placeholders []LevelPlaceholder             `json:"placeholders"`
	Limits       map[string]int64               `json:"limits"`
	Timezone     string                         `json:"timezone"`
}

// LevelPlaceholder 升级通知里可用的占位符。
type LevelPlaceholder struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var levelPlaceholders = []LevelPlaceholder{
	{Key: "{level}", Label: "新等级号"}, {Key: "{name}", Label: "新等级名称"}, {Key: "{from}", Label: "原等级号"},
	{Key: "{nickname}", Label: "用户昵称"}, {Key: "{rewards}", Label: "获得的奖励（自动到账与待领取）"},
}

// Catalog 节点、权益、图标、物品、条件字段、套餐与功能目录。
func (s *LevelService) Catalog(ctx context.Context, appID int64) (*LevelCatalog, error) {
	refs, err := s.pg.ItemGrantRefs(ctx, appID)
	if err != nil {
		return nil, err
	}
	plans := make([]itemgrant.PlanRef, 0, len(refs.Plans))
	for _, plan := range refs.Plans {
		if !plan.Trial {
			plans = append(plans, plan)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].ID < plans[j].ID })
	features := make([]itemgrant.FeatureRef, 0, len(refs.Features))
	for _, feature := range refs.Features {
		features = append(features, feature)
	}
	sort.Slice(features, func(i, j int) bool { return features[i].Tag < features[j].Tag })
	items := make([]itemgrant.ItemSpec, 0, 16)
	for _, item := range itemgrant.ItemCatalog() {
		// 达成奖励的数量固定，不支持按人计算
		item.Scalable = false
		items = append(items, item)
	}
	return &LevelCatalog{
		Nodes: levels.NodeCatalog(), Categories: levels.NodeCategories(), Privileges: levels.PrivilegeCatalog(),
		Icons: levels.IconCatalog(), Items: items, ItemGroups: itemgrant.ItemGroups(),
		Fields: levels.GateFields(), Operators: itemgrant.OperatorCatalog(), Routes: levels.NotifyRoutes(),
		Plans: plans, Features: features, Templates: levels.Templates(), Placeholders: levelPlaceholders,
		Timezone: levelLocation().String(),
		Limits: map[string]int64{
			"maxLevels": levels.MaxLevels, "maxNodes": levels.MaxNodes, "maxEdges": levels.MaxEdges,
			"maxAttachPerLevel": levels.MaxAttachPerLevel, "maxThreshold": levels.MaxThreshold,
			"maxItemsPerNode": itemgrant.MaxItemsPerNode, "maxConditionRules": levels.MaxConditionRules,
		},
	}, nil
}

// ── 体系 ──

// LevelSchemeView 控制台的体系页：草稿、草稿的校验结果与逐级展开。
type LevelSchemeView struct {
	*levels.SchemeRecord
	Issues  []levels.Issue    `json:"issues"`
	Levels  []levels.LevelRow `json:"levels"`
	Summary levels.Summary    `json:"summary"`
}

// LevelValidation 校验结果。
type LevelValidation struct {
	Issues  []levels.Issue    `json:"issues"`
	Levels  []levels.LevelRow `json:"levels"`
	Summary levels.Summary    `json:"summary"`
	Valid   bool              `json:"valid"`
}

// GetScheme 体系（草稿 + 发布信息 + 校验）。
func (s *LevelService) GetScheme(ctx context.Context, appID int64) (*LevelSchemeView, error) {
	record, err := s.pg.GetLevelSchemeRecord(ctx, appID)
	if err != nil {
		return nil, err
	}
	validation := s.validate(ctx, appID, record.Draft)
	return &LevelSchemeView{SchemeRecord: record, Issues: validation.Issues, Levels: validation.Levels, Summary: validation.Summary}, nil
}

// Validate 校验画布（不保存）。
func (s *LevelService) Validate(ctx context.Context, appID int64, graph levels.Graph) *LevelValidation {
	return s.validate(ctx, appID, graph)
}

func (s *LevelService) validate(ctx context.Context, appID int64, graph levels.Graph) *LevelValidation {
	scheme, issues := levels.Compile(graph)
	if scheme != nil {
		if refs, err := s.pg.ItemGrantRefs(ctx, appID); err == nil {
			issues = append(issues, levels.CheckRefs(scheme, refs)...)
		}
	}
	if issues == nil {
		issues = []levels.Issue{}
	}
	return &LevelValidation{
		Issues: issues, Levels: levels.Rows(scheme), Summary: levels.Summarize(scheme),
		Valid: scheme != nil && !levels.HasErrors(issues),
	}
}

// SaveDraft 保存草稿（草稿可以有错误，发布时才拦）。
func (s *LevelService) SaveDraft(ctx context.Context, appID int64, graph levels.Graph, revision int, operator string) (*LevelSchemeView, error) {
	if len(graph.Nodes) > levels.MaxNodes || len(graph.Edges) > levels.MaxEdges {
		return nil, apperrors.New(40000, http.StatusBadRequest, "画布太大")
	}
	graph = levels.NormalizeGraph(graph)
	if _, err := s.pg.SaveLevelSchemeDraft(ctx, appID, graph, revision, operator); err != nil {
		return nil, s.translate(err)
	}
	return s.GetScheme(ctx, appID)
}

// LevelPublishInput 发布。
type LevelPublishInput struct {
	AppID    int64
	Graph    levels.Graph
	Note     string
	Operator string
	// Recalc 发布后起一个重算任务；Downgrade 重算时按经验重新判定（允许降级）
	Recalc    bool
	Downgrade bool
}

// LevelPublishResult 发布结果。
type LevelPublishResult struct {
	Version int            `json:"version"`
	Issues  []levels.Issue `json:"issues"`
	Job     *levels.Job    `json:"job,omitempty"`
}

// Publish 发布：校验无错误 → 写成已发布并留版本 → 本实例立即换用 →（可选）起重算任务。
func (s *LevelService) Publish(ctx context.Context, input LevelPublishInput) (*LevelPublishResult, error) {
	graph := levels.NormalizeGraph(input.Graph)
	validation := s.validate(ctx, input.AppID, graph)
	if !validation.Valid {
		return nil, levelIssuesError("不能发布", validation.Issues)
	}
	scheme, _ := levels.Compile(graph)
	version, err := s.pg.PublishLevelScheme(ctx, input.AppID, graph, levels.Summarize(scheme), input.Note, input.Operator)
	if err != nil {
		return nil, s.translate(err)
	}
	result := &LevelPublishResult{Version: version, Issues: validation.Issues}
	if input.Recalc {
		job, err := s.StartRecalc(ctx, input.AppID, input.Downgrade, input.Operator)
		if err != nil {
			s.log.Warn("level: recalc after publish not started", zap.Int64("appid", input.AppID), zap.Error(err))
		} else {
			result.Job = job
		}
	}
	return result, nil
}

func levelIssuesError(prefix string, issues []levels.Issue) error {
	for _, issue := range issues {
		if issue.Level == levels.IssueError {
			return apperrors.New(40000, http.StatusBadRequest, prefix+"："+issue.Message)
		}
	}
	return apperrors.New(40000, http.StatusBadRequest, prefix)
}

// ListVersions 发布历史。
func (s *LevelService) ListVersions(ctx context.Context, appID int64) ([]levels.Version, error) {
	return s.pg.ListLevelSchemeVersions(ctx, appID)
}

// GetVersion 一次发布（带画布）。
func (s *LevelService) GetVersion(ctx context.Context, appID int64, version int) (*levels.Version, error) {
	v, err := s.pg.GetLevelSchemeVersion(ctx, appID, version)
	return v, s.translate(err)
}

// RestoreVersion 把某次发布的画布取回草稿（覆盖当前草稿，不自动发布）。
func (s *LevelService) RestoreVersion(ctx context.Context, appID int64, version int, operator string) (*LevelSchemeView, error) {
	v, err := s.pg.GetLevelSchemeVersion(ctx, appID, version)
	if err != nil {
		return nil, s.translate(err)
	}
	if _, err := s.pg.SaveLevelSchemeDraft(ctx, appID, *v.Graph, -1, operator); err != nil {
		return nil, s.translate(err)
	}
	return s.GetScheme(ctx, appID)
}

// ── 推演 ──

// LevelSimulateInput 推演：给一个经验值，或给一个用户（取他的经验并判定晋级条件）；Graph 为空时用已发布的体系。
type LevelSimulateInput struct {
	AppID      int64
	Graph      *levels.Graph
	Experience int64
	User       string
}

// Simulate 按经验或按用户推演等级。
func (s *LevelService) Simulate(ctx context.Context, input LevelSimulateInput) (*levels.Simulation, error) {
	var scheme *levels.Scheme
	if input.Graph != nil && len(input.Graph.Nodes) > 0 {
		compiled, issues := levels.Compile(*input.Graph)
		if compiled == nil {
			return nil, levelIssuesError("画布有错误", issues)
		}
		scheme = compiled
	} else {
		published, _, err := s.pg.LevelScheme(ctx, input.AppID)
		if err != nil {
			return nil, err
		}
		scheme = published
	}
	experience := max(input.Experience, 0)
	var check levels.GateCheck
	traces := []levels.GateTrace{}
	if keyword := strings.TrimSpace(input.User); keyword != "" {
		basics, err := s.pg.FindItemGrantUser(ctx, input.AppID, keyword)
		if err != nil {
			return nil, s.translate(err)
		}
		exp, err := s.pg.UserExperience(ctx, input.AppID, basics.UserID)
		if err != nil {
			return nil, s.translate(err)
		}
		experience = exp
		now := time.Now()
		profile, err := s.pg.LotteryBehaviorProfile(ctx, input.AppID, basics.UserID, now, levelDayStart(now))
		if err != nil {
			return nil, err
		}
		check = func(level levels.Level) (bool, error) {
			passed, err := level.Gate.Pass(profile)
			trace := levels.GateTrace{Level: level.Level, NodeID: level.Gate.NodeID, Label: level.Gate.Label, Passed: passed && err == nil}
			if err != nil {
				trace.Error = err.Error()
			}
			traces = append(traces, trace)
			return passed && err == nil, nil
		}
	}
	level, blocked, err := scheme.Resolve(experience, 0, check)
	if err != nil {
		return nil, err
	}
	state := scheme.StateOf(level, experience)
	out := &levels.Simulation{
		Experience: experience, Level: state.Level, Name: state.Config.Name, Progress: state.Progress,
		ToNext: state.ExpToNextLevel, Effective: scheme.Effective(state.Level), Rewards: []levels.Reward{}, Trace: traces,
	}
	for _, reward := range scheme.RewardsBetween(0, state.Level) {
		out.Rewards = append(out.Rewards, reward.Reward)
	}
	if blocked != nil && blocked.Gate != nil {
		out.Blocked = &levels.GateRow{NodeID: blocked.Gate.NodeID, Label: blocked.Gate.Label, Hint: blocked.Gate.Hint, Expr: blocked.Gate.Source}
	}
	return out, nil
}

// ── 总览与用户 ──

// Overview 等级分布、升级趋势、奖励状态。
func (s *LevelService) Overview(ctx context.Context, appID int64, days int) (*levels.Overview, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	return s.pg.LevelOverview(ctx, appID, levelDayStart(time.Now()), days)
}

// ListUsers 用户等级列表。
func (s *LevelService) ListUsers(ctx context.Context, query levels.UserQuery) (*levels.Page[levels.UserLevel], error) {
	return s.pg.ListUserLevels(ctx, query)
}

// LevelUserDetail 一个人的等级详情。
type LevelUserDetail struct {
	User    levels.UserLevel       `json:"user"`
	Info    *pointdomain.LevelInfo `json:"info"`
	Events  []levels.Event         `json:"events"`
	Rewards []levels.RewardRecord  `json:"rewards"`
}

// GetUser 一个人的等级详情（顺带按当前体系补判一次）。
func (s *LevelService) GetUser(ctx context.Context, appID, userID int64) (*LevelUserDetail, error) {
	profile, err := s.pg.GetUserLevelProfile(ctx, userID, appID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, s.translate(pgrepo.ErrUserNotFound)
	}
	page, err := s.pg.ListUserLevels(ctx, levels.UserQuery{AppID: appID, Keyword: strconv.FormatInt(userID, 10), Limit: 5})
	if err != nil {
		return nil, err
	}
	detail := &LevelUserDetail{Info: &profile.LevelInfo}
	for _, item := range page.Items {
		if item.UserID == userID {
			detail.User = item
		}
	}
	events, err := s.pg.ListLevelEvents(ctx, levels.EventQuery{AppID: appID, UserID: userID, Limit: 50})
	if err != nil {
		return nil, err
	}
	rewards, err := s.pg.ListLevelRewards(ctx, levels.RewardQuery{AppID: appID, UserID: userID, Limit: 200})
	if err != nil {
		return nil, err
	}
	detail.Events, detail.Rewards = events.Items, rewards.Items
	return detail, nil
}

// LevelAdjustInput 管理员强制升级（或只补判）一个人。
type LevelAdjustInput struct {
	AppID  int64
	UserID int64
	// Level 强制升到至少这一级（越过经验门槛与晋级条件）；0 表示只按当前体系补判
	Level int
	// TopUp 同时把经验补到这一级的门槛（经验只增不减）
	TopUp bool
	// Lock 把调整后的等级锁定为保底（「重算并允许降级」也不降到它以下）；Unlock 解除锁定
	Lock   bool
	Unlock bool
	// SkipRewards 不发这次经过的各级达成奖励；Silent 不发升级通知
	SkipRewards bool
	Silent      bool
	Note        string
	Operator    string
	AdminID     int64
}

// LevelAdjustResult 调整结果。
type LevelAdjustResult struct {
	User          *levels.UserLevel `json:"user"`
	From          int               `json:"from"`
	To            int               `json:"to"`
	AddExperience int64             `json:"addExperience"`
}

// MaxLevelBatchAdjust 一次批量强制升级最多的人数。
const MaxLevelBatchAdjust = 500

// LevelBatchAdjustInput 批量强制升级：选项与单人相同，逐人在自己的事务里执行。
type LevelBatchAdjustInput struct {
	LevelAdjustInput
	UserIDs []int64
}

// LevelBatchAdjustItem 批量里的一个人。
type LevelBatchAdjustItem struct {
	UserID        int64  `json:"userId"`
	Account       string `json:"account,omitempty"`
	From          int    `json:"from"`
	To            int    `json:"to"`
	AddExperience int64  `json:"addExperience"`
	Error         string `json:"error,omitempty"`
}

// LevelBatchAdjustResult 批量结果：changed 为等级确实变了的人数。
type LevelBatchAdjustResult struct {
	Total     int                    `json:"total"`
	Succeeded int                    `json:"succeeded"`
	Changed   int                    `json:"changed"`
	Failed    int                    `json:"failed"`
	Items     []LevelBatchAdjustItem `json:"items"`
}

func (s *LevelService) checkAdjust(ctx context.Context, input LevelAdjustInput) (*levels.Scheme, error) {
	scheme, _, err := s.pg.LevelScheme(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	if input.Level < 0 || input.Level > scheme.MaxLevel() {
		return nil, apperrors.New(40000, http.StatusBadRequest, "目标等级需要在 1–"+strconv.Itoa(scheme.MaxLevel())+" 之间")
	}
	if input.Lock && input.Unlock {
		return nil, apperrors.New(40000, http.StatusBadRequest, "锁定与解除锁定只能选一个")
	}
	return scheme, nil
}

// AdjustUser 管理员强制升级：可先补经验，再把等级直接调到至少目标级，不看经验门槛与晋级条件。
// 等级不能调低（经验不扣减，调低了下次就会升回去）；要让它在「重算并允许降级」时也保住，同时锁定。
func (s *LevelService) AdjustUser(ctx context.Context, input LevelAdjustInput) (*LevelAdjustResult, error) {
	scheme, err := s.checkAdjust(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.adjustUser(ctx, scheme, input)
}

func (s *LevelService) adjustUser(ctx context.Context, scheme *levels.Scheme, input LevelAdjustInput) (*LevelAdjustResult, error) {
	if input.Level > 0 {
		// 成长调控封顶的人不能被强制升到封顶之上（封顶会把结果截回去，不如直接说清楚）
		state, err := s.pg.GrowthLevel(ctx, input.AppID, input.UserID)
		if err != nil {
			return nil, s.translate(err)
		}
		if state.Capped > 0 && input.Level > state.Capped {
			return nil, apperrors.New(40900, http.StatusConflict, "该用户等级封顶在 Lv."+strconv.Itoa(state.Capped)+"，先在用户详情的「奖惩」里解除封顶")
		}
	}
	note := strings.TrimSpace(input.Note)
	result := &LevelAdjustResult{}
	if input.TopUp && input.Level > 0 {
		experience, err := s.pg.UserExperience(ctx, input.AppID, input.UserID)
		if err != nil {
			return nil, s.translate(err)
		}
		if need := scheme.At(input.Level).Threshold - experience; need > 0 {
			reason := "管理员强制升级至 Lv." + strconv.Itoa(input.Level)
			if note != "" {
				reason += "：" + note
			}
			if _, err := s.pg.AdminAdjustExperience(ctx, pointdomain.AdjustInput{
				Kind: pointdomain.KindExperience, AppID: input.AppID, UserID: input.UserID, Amount: need, Reason: reason,
			}, pointdomain.AdminAdjustOptions{AdminID: input.AdminID, AdminAccount: input.Operator}); err != nil {
				return nil, s.translate(err)
			}
			result.AddExperience = need
		}
	}
	user, from, to, err := s.pg.SyncUserLevelAdmin(ctx, input.AppID, input.UserID, pgrepo.LevelAdminSync{
		Force: input.Level, Lock: input.Lock, Unlock: input.Unlock, SkipRewards: input.SkipRewards, Silent: input.Silent,
		Operator: input.Operator, Note: note,
	})
	if err != nil {
		return nil, s.translate(err)
	}
	result.User, result.From, result.To = user, from, to
	s.publishUser(input.AppID, input.UserID, "level.changed", map[string]any{"level": to})
	return result, nil
}

// AdjustUsers 批量强制升级：逐人执行，一个人失败（不存在、补经验失败）不影响其他人。
func (s *LevelService) AdjustUsers(ctx context.Context, input LevelBatchAdjustInput) (*LevelBatchAdjustResult, error) {
	ids := make([]int64, 0, len(input.UserIDs))
	seen := map[int64]bool{}
	for _, id := range input.UserIDs {
		if id > 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择要调整的用户")
	}
	if len(ids) > MaxLevelBatchAdjust {
		return nil, apperrors.New(40000, http.StatusBadRequest, "一次最多调整 "+strconv.Itoa(MaxLevelBatchAdjust)+" 人")
	}
	if input.Level <= 0 && !input.Lock && !input.Unlock {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请选择目标等级")
	}
	scheme, err := s.checkAdjust(ctx, input.LevelAdjustInput)
	if err != nil {
		return nil, err
	}
	out := &LevelBatchAdjustResult{Total: len(ids), Items: make([]LevelBatchAdjustItem, 0, len(ids))}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		one := input.LevelAdjustInput
		one.UserID = id
		item := LevelBatchAdjustItem{UserID: id}
		result, err := s.adjustUser(ctx, scheme, one)
		if err != nil {
			var appErr *apperrors.AppError
			if errors.As(err, &appErr) {
				item.Error = appErr.Message
			} else {
				s.log.Warn("level: batch adjust failed", zap.Int64("appid", input.AppID), zap.Int64("userId", id), zap.Error(err))
				item.Error = "调整失败"
			}
			out.Failed++
		} else {
			item.From, item.To, item.AddExperience = result.From, result.To, result.AddExperience
			if result.User != nil {
				item.Account = result.User.Account
			}
			out.Succeeded++
			if result.To != result.From {
				out.Changed++
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// ListEvents 升级记录。
func (s *LevelService) ListEvents(ctx context.Context, query levels.EventQuery) (*levels.Page[levels.Event], error) {
	return s.pg.ListLevelEvents(ctx, query)
}

// ── 达成奖励 ──

// ListRewards 达成奖励记录。
func (s *LevelService) ListRewards(ctx context.Context, query levels.RewardQuery) (*levels.Page[levels.RewardRecord], error) {
	return s.pg.ListLevelRewards(ctx, query)
}

// GetReward 一条达成奖励记录。
func (s *LevelService) GetReward(ctx context.Context, appID, rewardID int64) (*levels.RewardRecord, error) {
	rec, err := s.pg.GetLevelReward(ctx, appID, rewardID)
	return rec, s.translate(err)
}

// RetryReward 重试失败的奖励（立即发放）。
func (s *LevelService) RetryReward(ctx context.Context, appID, rewardID int64) (*levels.RewardRecord, error) {
	if _, err := s.pg.GetLevelReward(ctx, appID, rewardID); err != nil {
		return nil, s.translate(err)
	}
	rec, err := s.pg.GrantLevelReward(ctx, rewardID, pgrepo.LevelRewardRetry, 0)
	if err != nil {
		return nil, s.translate(err)
	}
	s.afterReward(rec)
	return rec, nil
}

// FulfillReward 人工物品已处理。
func (s *LevelService) FulfillReward(ctx context.Context, appID, rewardID int64, operator, note string) (*levels.RewardRecord, error) {
	rec, err := s.pg.FulfillLevelReward(ctx, appID, rewardID, operator, note)
	if err != nil {
		return nil, s.translate(err)
	}
	s.deliver(rec.AppID, rec.UserID, "等级奖励已发放", "「"+rec.Label+"」已由管理员处理："+strings.Join(rec.ItemsText, "、"), notificationdomain.RouteLevel)
	return rec, nil
}

// Backfill 补发：给已经达到这份奖励所在等级、还没有记录的人各写一条。
func (s *LevelService) Backfill(ctx context.Context, appID int64, nodeID string) (int64, error) {
	count, err := s.pg.BackfillLevelReward(ctx, appID, strings.TrimSpace(nodeID))
	if errors.Is(err, pgrepo.ErrLevelRewardMissing) {
		return 0, apperrors.New(40400, http.StatusNotFound, "已发布的体系里没有这份达成奖励（先发布再补发）")
	}
	return count, err
}

// ── 用户端 ──

// LevelRewardView 用户看到的一条达成奖励。
type LevelRewardView struct {
	ID        int64      `json:"id"`
	Level     int        `json:"level"`
	LevelName string     `json:"levelName,omitempty"`
	Label     string     `json:"label"`
	Items     []string   `json:"items"`
	Claim     string     `json:"claim"`
	Status    string     `json:"status"`
	Error     string     `json:"error,omitempty"`
	GrantedAt *time.Time `json:"grantedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// LevelEventView 用户看到的一条升级记录。
type LevelEventView struct {
	FromLevel int       `json:"fromLevel"`
	ToLevel   int       `json:"toLevel"`
	FromName  string    `json:"fromName,omitempty"`
	ToName    string    `json:"toName,omitempty"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *LevelService) rewardView(ctx context.Context, appID int64, rec levels.RewardRecord) LevelRewardView {
	view := LevelRewardView{
		ID: rec.ID, Level: rec.Level, Label: rec.Label, Items: rec.ItemsText, Claim: rec.Claim, Status: rec.Status,
		GrantedAt: rec.GrantedAt, CreatedAt: rec.CreatedAt,
	}
	if rec.Status == levels.RewardFailed {
		view.Error = "发放遇到问题，已通知管理员处理"
	}
	if scheme, _, err := s.pg.LevelScheme(ctx, appID); err == nil && rec.Level <= scheme.MaxLevel() {
		view.LevelName = scheme.At(rec.Level).Name
	}
	if view.Items == nil {
		view.Items = []string{}
	}
	return view
}

// MyRewards 我的达成奖励（待领取在前）。
func (s *LevelService) MyRewards(ctx context.Context, session *authdomain.Session, status string) ([]LevelRewardView, error) {
	page, err := s.pg.ListLevelRewards(ctx, levels.RewardQuery{AppID: session.AppID, UserID: session.UserID, Status: status, Limit: 200})
	if err != nil {
		return nil, err
	}
	out := make([]LevelRewardView, 0, len(page.Items))
	for _, rec := range page.Items {
		out = append(out, s.rewardView(ctx, session.AppID, rec))
	}
	return out, nil
}

// ClaimReward 领取一份待领取的达成奖励。
func (s *LevelService) ClaimReward(ctx context.Context, session *authdomain.Session, rewardID int64) (*LevelRewardView, error) {
	rec, err := s.pg.GrantLevelReward(ctx, rewardID, pgrepo.LevelRewardClaim, session.UserID)
	if err != nil {
		switch {
		case errors.Is(err, pgrepo.ErrLevelRewardMissing):
			return nil, apperrors.New(40400, http.StatusNotFound, "这份奖励不存在")
		case errors.Is(err, pgrepo.ErrLevelRewardState):
			return nil, apperrors.New(40900, http.StatusConflict, "这份奖励已经领取过了")
		}
		return nil, s.translate(err)
	}
	if rec.AppID != session.AppID {
		return nil, apperrors.New(40400, http.StatusNotFound, "这份奖励不存在")
	}
	if rec.Status == levels.RewardFailed {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "奖励暂时发不出去，已记录，请稍后再试或联系客服")
	}
	s.afterReward(rec)
	view := s.rewardView(ctx, session.AppID, *rec)
	return &view, nil
}

// MyHistory 我的升级记录。
func (s *LevelService) MyHistory(ctx context.Context, session *authdomain.Session, page, limit int) (*levels.Page[LevelEventView], error) {
	events, err := s.pg.ListLevelEvents(ctx, levels.EventQuery{AppID: session.AppID, UserID: session.UserID, Page: page, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := &levels.Page[LevelEventView]{Items: make([]LevelEventView, 0, len(events.Items)), Total: events.Total, Page: events.Page, Limit: events.Limit}
	for _, e := range events.Items {
		out.Items = append(out.Items, LevelEventView{FromLevel: e.FromLevel, ToLevel: e.ToLevel, FromName: e.FromName, ToName: e.ToName, Reason: e.Reason, CreatedAt: e.CreatedAt})
	}
	return out, nil
}

// ── 重算任务 ──

// StartRecalc 起一个重算任务：全体用户按当前体系判定一次（downgrade 时从经验重新判定，允许降级）。
func (s *LevelService) StartRecalc(ctx context.Context, appID int64, downgrade bool, operator string) (*levels.Job, error) {
	_, version, err := s.pg.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	job, err := s.pg.CreateLevelJob(ctx, appID, levels.JobRecalc, map[string]any{"downgrade": downgrade}, version, operator)
	if err != nil {
		return nil, s.translate(err)
	}
	s.launchJob(*job)
	return job, nil
}

// ListJobs 最近的任务。
func (s *LevelService) ListJobs(ctx context.Context, appID int64) ([]levels.Job, error) {
	return s.pg.ListLevelJobs(ctx, appID, 20)
}

// GetJob 一个任务。
func (s *LevelService) GetJob(ctx context.Context, appID, jobID int64) (*levels.Job, error) {
	job, err := s.pg.GetLevelJob(ctx, appID, jobID)
	return job, s.translate(err)
}

// CancelJob 取消任务。
func (s *LevelService) CancelJob(ctx context.Context, appID, jobID int64, operator string) (*levels.Job, error) {
	job, err := s.pg.CancelLevelJob(ctx, appID, jobID, operator)
	return job, s.translate(err)
}

func (s *LevelService) launchJob(job levels.Job) {
	if _, loaded := s.active.LoadOrStore(job.ID, struct{}{}); loaded {
		return
	}
	go func() {
		defer s.active.Delete(job.ID)
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("level job panic", zap.Int64("job", job.ID), zap.Any("panic", r))
				_ = s.pg.FinishLevelJob(context.Background(), job.ID, levels.JobFailed, "任务异常中止")
			}
		}()
		s.runJob(job)
	}()
}

func (s *LevelService) runJob(job levels.Job) {
	ctx := context.Background()
	downgrade, _ := job.Params["downgrade"].(bool)
	operator := job.CreatedBy
	cursor := job.Cursor
	for {
		ids, err := s.pg.LevelJobBatch(ctx, job.AppID, cursor, levelJobBatch)
		if err != nil {
			_ = s.pg.FinishLevelJob(ctx, job.ID, levels.JobFailed, err.Error())
			return
		}
		if len(ids) == 0 {
			_ = s.pg.FinishLevelJob(ctx, job.ID, levels.JobSucceeded, "")
			return
		}
		var changed int64
		for _, userID := range ids {
			moved, err := s.pg.RecalcUserLevel(ctx, job.AppID, userID, downgrade, operator)
			if err != nil {
				s.log.Warn("level job: recalc user failed", zap.Int64("job", job.ID), zap.Int64("userId", userID), zap.Error(err))
				continue
			}
			if moved {
				changed++
			}
		}
		cursor = ids[len(ids)-1]
		status, err := s.pg.AdvanceLevelJob(ctx, job.ID, int64(len(ids)), changed, cursor)
		if err != nil || status != levels.JobRunning {
			return
		}
	}
}

// ── 后台循环 ──

// Start 后台循环：发放待发的达成奖励、发送升级通知、接管心跳超时的重算任务。
func (s *LevelService) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("level loop panic", zap.Any("panic", r))
			}
		}()
		_ = s.pg.ResetStaleLevelNotices(ctx, time.Now().Add(-10*time.Minute))
		timer := time.NewTimer(15 * time.Second)
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
				timer.Reset(levelTickInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *LevelService) Stop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *LevelService) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *LevelService) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("level tick panic", zap.Any("panic", r))
		}
	}()
	s.processRewards(ctx)
	s.processNotices(ctx)
	stale, err := s.pg.ClaimStaleLevelJobs(ctx, time.Now().Add(-levelJobStaleAfter), 3)
	if err != nil {
		s.log.Warn("level: claim stale jobs failed", zap.Error(err))
		return
	}
	for _, job := range stale {
		if _, mine := s.active.Load(job.ID); mine {
			continue
		}
		s.log.Info("level: resuming job", zap.Int64("job", job.ID), zap.Int64("cursor", job.Cursor))
		s.launchJob(job)
	}
}

// processRewards 逐条发放待发的达成奖励（每条一个事务；一次最多处理几批，剩下的下一轮接着发）。
func (s *LevelService) processRewards(ctx context.Context) {
	for round := 0; round < 20; round++ {
		ids, err := s.pg.QueuedLevelRewards(ctx, levelRewardBatch)
		if err != nil {
			s.log.Warn("level: load queued rewards failed", zap.Error(err))
			return
		}
		if len(ids) == 0 {
			return
		}
		jobs := make(chan int64)
		var wg sync.WaitGroup
		for w := 0; w < levelRewardWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for id := range jobs {
					rec, err := s.pg.GrantLevelReward(ctx, id, pgrepo.LevelRewardAuto, 0)
					if err != nil {
						s.log.Warn("level: grant reward failed", zap.Int64("reward", id), zap.Error(err))
						continue
					}
					s.afterReward(rec)
				}
			}()
		}
		for _, id := range ids {
			jobs <- id
		}
		close(jobs)
		wg.Wait()
		if len(ids) < levelRewardBatch {
			return
		}
	}
}

// processNotices 发送待发的升级通知。
func (s *LevelService) processNotices(ctx context.Context) {
	notices, err := s.pg.ClaimLevelNotices(ctx, 100)
	if err != nil {
		s.log.Warn("level: claim notices failed", zap.Error(err))
		return
	}
	for _, notice := range notices {
		sent := s.sendNotice(ctx, notice)
		if err := s.pg.FinishLevelNotice(ctx, notice.EventID, sent); err != nil {
			s.log.Warn("level: finish notice failed", zap.Int64("event", notice.EventID), zap.Error(err))
		}
	}
}

func (s *LevelService) sendNotice(ctx context.Context, notice pgrepo.PendingLevelNotice) bool {
	scheme, _, err := s.pg.LevelScheme(ctx, notice.AppID)
	if err != nil || scheme == nil || !scheme.Notify {
		return err == nil
	}
	nickname := notice.Nickname
	if nickname == "" {
		nickname = notice.Account
	}
	var granted, claimable []string
	for _, rec := range notice.Rewards {
		switch rec.Status {
		case levels.RewardClaimable:
			claimable = append(claimable, "「"+rec.Label+"」")
		case levels.RewardFailed:
		default:
			granted = append(granted, rec.ItemsText...)
		}
	}
	var parts []string
	if len(granted) > 0 {
		parts = append(parts, "已到账："+strings.Join(granted, "、")+"。")
	}
	if len(claimable) > 0 {
		parts = append(parts, "可领取"+strings.Join(claimable, "、")+"，前往我的等级领取。")
	}
	level := scheme.At(notice.ToLevel)
	replacer := strings.NewReplacer("{level}", strconv.Itoa(notice.ToLevel), "{name}", level.Name,
		"{from}", strconv.Itoa(notice.FromLevel), "{nickname}", nickname, "{rewards}", strings.Join(parts, ""))
	title := strings.TrimSpace(replacer.Replace(scheme.NotifyTitle))
	content := strings.TrimSpace(replacer.Replace(scheme.NotifyContent))
	if title == "" {
		return true
	}
	if s.notifier == nil {
		return false
	}
	route := scheme.NotifyRoute
	if route == "" {
		route = notificationdomain.RouteLevel
	}
	input := notificationdomain.DeliverInput{
		AppID: notice.AppID, UserID: notice.UserID, Type: notificationdomain.TypeLevel,
		Title: title, Content: content, Level: "success",
		Metadata: map[string]any{"level": notice.ToLevel, "fromLevel": notice.FromLevel},
		Action:   notificationdomain.RouteAction(route, "查看"),
	}
	deliverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := s.notifier.Deliver(deliverCtx, input); err != nil {
		s.log.Warn("level: notify user failed", zap.Int64("appid", notice.AppID), zap.Int64("userId", notice.UserID), zap.Error(err))
		return false
	}
	s.publishUser(notice.AppID, notice.UserID, "level.up", map[string]any{"level": notice.ToLevel})
	return true
}

// afterReward 奖励到账（或待人工处理）之后通知在线设备刷新。
func (s *LevelService) afterReward(rec *levels.RewardRecord) {
	if rec == nil {
		return
	}
	s.publishUser(rec.AppID, rec.UserID, "level.reward", map[string]any{"rewardId": rec.ID, "status": rec.Status})
}

func (s *LevelService) publishUser(appID, userID int64, event string, data map[string]any) {
	if s.userEvents == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.userEvents.PublishUserEvent(ctx, appID, userID, event, data)
	}()
}

func (s *LevelService) deliver(appID, userID int64, title, content, route string) {
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeLevel, Title: title, Content: content, Level: "success",
			Action: notificationdomain.RouteAction(route, "查看"),
		}); err != nil {
			s.log.Warn("level: notify user failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

func (s *LevelService) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrLevelSchemeConflict):
		return apperrors.New(40900, http.StatusConflict, "草稿刚被别人改过，请刷新后再保存")
	case errors.Is(err, pgrepo.ErrLevelVersionMissing):
		return apperrors.New(40400, http.StatusNotFound, "这个版本不存在")
	case errors.Is(err, pgrepo.ErrLevelRewardMissing):
		return apperrors.New(40400, http.StatusNotFound, "奖励记录不存在")
	case errors.Is(err, pgrepo.ErrLevelRewardState):
		return apperrors.New(40900, http.StatusConflict, "当前状态不能这样处理")
	case errors.Is(err, pgrepo.ErrLevelJobBusy):
		return apperrors.New(40900, http.StatusConflict, "已有一个重算任务在进行，等它结束或先取消")
	case errors.Is(err, pgrepo.ErrLevelJobMissing):
		return apperrors.New(40400, http.StatusNotFound, "任务不存在")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40400, http.StatusNotFound, "用户不存在")
	}
	return err
}
