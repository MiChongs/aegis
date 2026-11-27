package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"aegis/internal/domain/itemgrant"
	notificationdomain "aegis/internal/domain/notification"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 物品发放（docs/item-grant.md）。
//
// 运营在控制台的画布上编排规则：触发器 →（目标人群 / 条件 / 时间窗口 / 随机分流 / 频次限额）→ 发放物品 / 随机礼包
// → 站内信。规则有三种触发方式：
//
//	手动执行   控制台点「执行」，对目标人群按用户 ID 升序逐批判定、发放（后台执行，可取消，进程重启后由任一实例接着跑）
//	定时执行   Cron（应用时区）到点时同上；多实例靠 next_run_at 的 CAS 只跑一次
//	事件触发   用户注册 / 登录 / 签到 / 支付时实时为这个人判定、发放
//
// 判定是 itemgrant.Compiled.Evaluate 这一个纯函数；单人发放是仓储层的一个事务（锁用户 → 判定 → 写记录占幂等键
// → 逐项走各账本的事务内入口）。预演与正式执行调同一个判定，控制台画布上看到的路径就是执行时会走的路径。

const (
	itemGrantBatchSize      = 200
	itemGrantBatchWorkers   = 4
	itemGrantEventWorkers   = 8
	itemGrantMaxRuns        = 2
	itemGrantTickInterval   = 30 * time.Second
	itemGrantStaleAfter     = 3 * time.Minute
	itemGrantEventCacheTTL  = 30 * time.Second
	itemGrantMinCronGap     = 10 * time.Minute
	itemGrantDefaultSamples = 200
	itemGrantMaxSamples     = 1000
)

var itemGrantCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// ItemGrantService 物品发放服务。
type ItemGrantService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	notifier   notificationDeliverer
	userEvents UserEventPublisher

	cacheMu    sync.Mutex
	eventCache map[string]itemGrantEventEntry

	eventSlots chan struct{}
	runSlots   chan struct{}
	kick       chan struct{}
	stop       chan struct{}
	stopOnce   sync.Once
	// active 本实例正在跑的执行（避免同一实例重复接管自己的执行）
	active sync.Map
}

type itemGrantEventEntry struct {
	rules  []itemGrantCompiledRule
	loaded time.Time
}

type itemGrantCompiledRule struct {
	rule     itemgrant.Rule
	compiled *itemgrant.Compiled
}

// NewItemGrantService 创建物品发放服务。
func NewItemGrantService(log *zap.Logger, pg *pgrepo.Repository) *ItemGrantService {
	return &ItemGrantService{
		log: log, pg: pg,
		eventCache: map[string]itemGrantEventEntry{},
		eventSlots: make(chan struct{}, itemGrantEventWorkers),
		runSlots:   make(chan struct{}, itemGrantMaxRuns),
		kick:       make(chan struct{}, 1),
		stop:       make(chan struct{}),
	}
}

// SetNotifier 站内信出口（规则里的「站内信」节点）。
func (s *ItemGrantService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 发放后通知用户的在线设备刷新积分与权益。
func (s *ItemGrantService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

func itemGrantLocation() *time.Location { return timeutil.DefaultLocation() }

// checkItemGrantCron 校验 Cron：能解析，且相邻两次至少隔 10 分钟（每分钟对全站执行一次是事故）。
func checkItemGrantCron(spec string) error {
	schedule, err := itemGrantCronParser.Parse(spec)
	if err != nil {
		return err
	}
	first := schedule.Next(time.Now().In(itemGrantLocation()))
	if first.IsZero() {
		return errors.New("这个表达式不会在任何时刻触发")
	}
	if second := schedule.Next(first); !second.IsZero() && second.Sub(first) < itemGrantMinCronGap {
		return fmt.Errorf("相邻两次执行至少间隔 %d 分钟", int(itemGrantMinCronGap.Minutes()))
	}
	return nil
}

func nextItemGrantRun(spec string, after time.Time) *time.Time {
	schedule, err := itemGrantCronParser.Parse(spec)
	if err != nil {
		return nil
	}
	next := schedule.Next(after.In(itemGrantLocation()))
	if next.IsZero() {
		return nil
	}
	return &next
}

// ── 目录 ──

// ItemGrantCatalog 控制台画布需要的全部目录。
type ItemGrantCatalog struct {
	Items      []itemgrant.ItemSpec           `json:"items"`
	ItemGroups []itemgrant.GroupSpec          `json:"itemGroups"`
	Nodes      []itemgrant.NodeSpec           `json:"nodes"`
	Categories []itemgrant.CategorySpec       `json:"categories"`
	Triggers   []itemgrant.TriggerSpec        `json:"triggers"`
	Events     []itemgrant.EventSpec          `json:"events"`
	Fields     []itemgrant.FieldSpec          `json:"fields"`
	Operators  []itemgrant.OperatorSpec       `json:"operators"`
	Scopes     []itemgrant.OptionSpec         `json:"scopes"`
	Periods    []itemgrant.OptionSpec         `json:"periods"`
	Routes     []notificationdomain.RouteSpec `json:"routes"`
	Plans      []itemgrant.PlanRef            `json:"plans"`
	Features   []itemgrant.FeatureRef         `json:"features"`
	Tiers      []itemgrant.TierRef            `json:"tiers"`
	Limits     map[string]int                 `json:"limits"`
	Timezone   string                         `json:"timezone"`
	Statuses   []string                       `json:"statuses"`
	Templates  []itemgrant.Template           `json:"templates"`
}

// Catalog 物品、节点、触发方式、事件、条件字段、套餐与功能目录。
func (s *ItemGrantService) Catalog(ctx context.Context, appID int64) (*ItemGrantCatalog, error) {
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
	tiers := make([]itemgrant.TierRef, 0, len(refs.Tiers))
	for _, tier := range refs.Tiers {
		tiers = append(tiers, tier)
	}
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].ID < tiers[j].ID })
	return &ItemGrantCatalog{
		Items: itemgrant.ItemCatalog(), ItemGroups: itemgrant.ItemGroups(),
		Nodes: itemgrant.NodeCatalog(), Categories: itemgrant.NodeCategories(),
		Triggers: itemgrant.TriggerCatalog(), Events: itemgrant.EventCatalog(),
		Fields: itemgrant.FieldCatalog(), Operators: itemgrant.OperatorCatalog(),
		Scopes: itemgrant.Scopes(), Periods: itemgrant.Periods(), Routes: itemgrant.NotifyRoutes(),
		Plans: plans, Features: features, Tiers: tiers, Timezone: itemGrantLocation().String(),
		Statuses:  []string{itemgrant.StatusDraft, itemgrant.StatusActive, itemgrant.StatusPaused, itemgrant.StatusArchived},
		Templates: itemgrant.Templates(),
		Limits: map[string]int{
			"maxNodes": itemgrant.MaxNodes, "maxEdges": itemgrant.MaxEdges, "maxItemsPerNode": itemgrant.MaxItemsPerNode,
			"maxRandomOptions": itemgrant.MaxRandomOptions, "maxSplitBranches": itemgrant.MaxSplitBranches,
			"maxConditionRules": itemgrant.MaxConditionRules, "maxAudienceUsers": itemgrant.MaxAudienceUsers,
			"maxAudienceTiers": itemgrant.MaxAudienceTiers,
			"maxSamples":       itemGrantMaxSamples, "minCronGapMinutes": int(itemGrantMinCronGap.Minutes()),
		},
	}, nil
}

// ── 规则 ──

// ListRules 应用的全部规则（附统计与正在进行的执行）。
func (s *ItemGrantService) ListRules(ctx context.Context, appID int64) ([]itemgrant.Rule, error) {
	return s.pg.ListItemGrantRules(ctx, appID, itemGrantDayStart(time.Now()))
}

// GetRule 取一条规则。
func (s *ItemGrantService) GetRule(ctx context.Context, appID, ruleID int64) (*itemgrant.Rule, error) {
	rule, err := s.pg.GetItemGrantRule(ctx, appID, ruleID)
	return rule, s.translate(err)
}

// CreateRule 新建规则（草稿：画布有问题也能存，上线时再拦）。
func (s *ItemGrantService) CreateRule(ctx context.Context, input itemgrant.RuleInput) (*itemgrant.Rule, error) {
	input = itemgrant.NormalizeRuleInput(input)
	if err := itemgrant.ValidateRuleInput(input); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if len(input.Graph.Nodes) == 0 {
		input.Graph = itemgrant.StarterGraph()
	}
	rule := itemgrant.Rule{
		AppID: input.AppID, Name: input.Name, Description: input.Description, Status: itemgrant.StatusDraft,
		StartsAt: input.StartsAt, EndsAt: input.EndsAt, Graph: input.Graph, CreatedBy: input.Operator,
	}
	deriveItemGrantTrigger(&rule)
	created, err := s.pg.CreateItemGrantRule(ctx, rule)
	return created, s.translate(err)
}

// UpdateRule 保存规则。生效中的规则保存时画布不能有错误（否则下一次触发就会出事），先暂停再改结构。
func (s *ItemGrantService) UpdateRule(ctx context.Context, input itemgrant.RuleInput) (*itemgrant.Rule, error) {
	input = itemgrant.NormalizeRuleInput(input)
	if err := itemgrant.ValidateRuleInput(input); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	current, err := s.pg.GetItemGrantRule(ctx, input.AppID, input.RuleID)
	if err != nil {
		return nil, s.translate(err)
	}
	rule := *current
	rule.Name, rule.Description, rule.StartsAt, rule.EndsAt = input.Name, input.Description, input.StartsAt, input.EndsAt
	rule.Graph, rule.UpdatedBy = input.Graph, input.Operator
	deriveItemGrantTrigger(&rule)
	rule.NextRunAt = nil
	if rule.Status == itemgrant.StatusActive {
		if issues := s.validateGraph(ctx, rule.AppID, rule.Graph); itemgrant.HasErrors(issues) {
			return nil, itemGrantIssuesError("生效中的规则保存时画布不能有错误（可以先暂停）", issues)
		}
		if rule.Trigger == itemgrant.TriggerSchedule {
			rule.NextRunAt = nextItemGrantRun(rule.Cron, time.Now())
		}
	}
	updated, err := s.pg.UpdateItemGrantRule(ctx, rule)
	if err != nil {
		return nil, s.translate(err)
	}
	s.invalidateEvents(rule.AppID)
	return updated, nil
}

// Validate 校验画布：结构、配置、表达式、引用的套餐与功能。
func (s *ItemGrantService) Validate(ctx context.Context, appID int64, graph itemgrant.Graph) []itemgrant.Issue {
	return s.validateGraph(ctx, appID, itemgrant.NormalizeGraph(graph))
}

func (s *ItemGrantService) validateGraph(ctx context.Context, appID int64, graph itemgrant.Graph) []itemgrant.Issue {
	_, issues := itemgrant.Compile(graph, checkItemGrantCron)
	refs, err := s.pg.ItemGrantRefs(ctx, appID)
	if err != nil {
		return append(issues, itemgrant.Issue{Level: itemgrant.IssueError, Message: "读取会员套餐与功能目录失败"})
	}
	return append(issues, itemgrant.CheckRefs(graph, refs)...)
}

// SetStatus 上线 / 暂停 / 归档 / 回到草稿。上线前画布必须没有错误。
func (s *ItemGrantService) SetStatus(ctx context.Context, appID, ruleID int64, status, operator string) (*itemgrant.Rule, error) {
	if !itemgrant.ValidStatus(status) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的规则状态："+status)
	}
	rule, err := s.pg.GetItemGrantRule(ctx, appID, ruleID)
	if err != nil {
		return nil, s.translate(err)
	}
	var next *time.Time
	if status == itemgrant.StatusActive {
		if issues := s.validateGraph(ctx, appID, rule.Graph); itemgrant.HasErrors(issues) {
			return nil, itemGrantIssuesError("画布还有错误，不能上线", issues)
		}
		if rule.Trigger == itemgrant.TriggerSchedule {
			next = nextItemGrantRun(rule.Cron, time.Now())
		}
	}
	updated, err := s.pg.SetItemGrantRuleStatus(ctx, appID, ruleID, status, next, operator)
	if err != nil {
		return nil, s.translate(err)
	}
	s.invalidateEvents(appID)
	s.wake()
	return updated, nil
}

// DeleteRule 删除规则（有发放记录的只能归档）。
func (s *ItemGrantService) DeleteRule(ctx context.Context, appID, ruleID int64) error {
	if err := s.pg.DeleteItemGrantRule(ctx, appID, ruleID); err != nil {
		return s.translate(err)
	}
	s.invalidateEvents(appID)
	return nil
}

func deriveItemGrantTrigger(rule *itemgrant.Rule) {
	trigger, ok := itemgrant.TriggerOf(rule.Graph)
	if !ok {
		rule.Trigger, rule.Event, rule.Cron = itemgrant.TriggerManual, "", ""
		return
	}
	rule.Trigger, rule.Event, rule.Cron = trigger.Kind, trigger.Event, trigger.Cron
	if !itemgrant.ValidTrigger(rule.Trigger) {
		rule.Trigger = itemgrant.TriggerManual
	}
}

// itemGrantIssuesError 把画布问题合成一个 400：第一条错误写进文案，全部问题放在 data 里不方便，控制台另调校验接口。
func itemGrantIssuesError(prefix string, issues []itemgrant.Issue) error {
	for _, issue := range issues {
		if issue.Level == itemgrant.IssueError {
			return apperrors.New(40000, http.StatusBadRequest, prefix+"："+issue.Message)
		}
	}
	return apperrors.New(40000, http.StatusBadRequest, prefix)
}

// ── 预演 ──

// ItemGrantPreviewInput 预演：指定了用户就追踪这一个人，否则抽样估算。
type ItemGrantPreviewInput struct {
	AppID  int64
	RuleID int64
	Graph  itemgrant.Graph
	// User 用户 ID 或账号
	User      string
	EventData map[string]any
	Samples   int
}

// ItemGrantPreview 预演结果。
type ItemGrantPreview struct {
	Issues   []itemgrant.Issue   `json:"issues"`
	Trace    *itemgrant.Trace    `json:"trace,omitempty"`
	Estimate *itemgrant.Estimate `json:"estimate,omitempty"`
}

// Preview 只判定、不发放。画布有错误时只返回问题。
func (s *ItemGrantService) Preview(ctx context.Context, input ItemGrantPreviewInput) (*ItemGrantPreview, error) {
	graph := itemgrant.NormalizeGraph(input.Graph)
	issues := s.validateGraph(ctx, input.AppID, graph)
	out := &ItemGrantPreview{Issues: issues}
	compiled, compileIssues := itemgrant.Compile(graph, checkItemGrantCron)
	if compiled == nil || itemgrant.HasErrors(compileIssues) {
		return out, nil
	}
	rule := itemgrant.Rule{ID: input.RuleID, AppID: input.AppID, Graph: graph}
	if input.RuleID > 0 {
		if _, err := s.pg.GetItemGrantRule(ctx, input.AppID, input.RuleID); err != nil {
			return nil, s.translate(err)
		}
	}
	refs, err := s.pg.ItemGrantRefs(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	now := time.Now().In(itemGrantLocation())

	if strings.TrimSpace(input.User) != "" {
		basics, err := s.pg.FindItemGrantUser(ctx, input.AppID, input.User)
		if err != nil {
			return nil, s.translate(err)
		}
		outcome, err := s.pg.PreviewItemGrant(ctx, compiled, rule, basics, input.EventData, now)
		if err != nil {
			return nil, err
		}
		described := make([]string, 0, len(outcome.Items))
		for _, planned := range outcome.Items {
			described = append(described, itemgrant.DescribePlannedItem(planned.Item, refs))
		}
		out.Trace = &itemgrant.Trace{User: basics, Outcome: outcome, Items: described}
		return out, nil
	}

	samples := input.Samples
	if samples <= 0 {
		samples = itemGrantDefaultSamples
	}
	samples = min(samples, itemGrantMaxSamples)
	total, err := s.pg.CountItemGrantCandidates(ctx, input.AppID, compiled.EntryAudience)
	if err != nil {
		return nil, err
	}
	candidates, err := s.pg.SampleItemGrantCandidates(ctx, input.AppID, compiled.EntryAudience, samples)
	if err != nil {
		return nil, err
	}
	outcomes := make([]itemgrant.Outcome, len(candidates))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	var firstErr error
	var errMu sync.Mutex
	for i, basics := range candidates {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			outcome, err := s.pg.PreviewItemGrant(ctx, compiled, rule, basics, input.EventData, now)
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			outcomes[i] = outcome
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	out.Estimate = itemgrant.Summarize(outcomes, total, refs)
	return out, nil
}

// ── 批量执行 ──

// RunRule 立即执行一次（手动规则，或让定时规则提前跑一次）。规则必须已上线。
func (s *ItemGrantService) RunRule(ctx context.Context, appID, ruleID int64, operator string) (*itemgrant.Run, error) {
	rule, err := s.pg.GetItemGrantRule(ctx, appID, ruleID)
	if err != nil {
		return nil, s.translate(err)
	}
	switch {
	case rule.Status != itemgrant.StatusActive:
		return nil, apperrors.New(40000, http.StatusBadRequest, "规则上线后才能执行")
	case rule.Trigger == itemgrant.TriggerEvent:
		return nil, apperrors.New(40000, http.StatusBadRequest, "事件触发的规则在用户行为发生时自动执行，不能批量执行")
	case !rule.InWindow(time.Now()):
		return nil, apperrors.New(40000, http.StatusBadRequest, "规则不在生效期内")
	}
	trigger := itemgrant.TriggerManual
	if operator == "" {
		trigger = itemgrant.TriggerSchedule
	}
	return s.startRun(ctx, *rule, trigger, operator)
}

func (s *ItemGrantService) startRun(ctx context.Context, rule itemgrant.Rule, trigger, operator string) (*itemgrant.Run, error) {
	compiled, issues := itemgrant.Compile(rule.Graph, checkItemGrantCron)
	if compiled == nil {
		return nil, itemGrantIssuesError("画布有错误，不能执行", issues)
	}
	total, err := s.pg.CountItemGrantCandidates(ctx, rule.AppID, compiled.EntryAudience)
	if err != nil {
		return nil, err
	}
	run, err := s.pg.CreateItemGrantRun(ctx, itemgrant.Run{
		AppID: rule.AppID, RuleID: rule.ID, RuleVersion: rule.Version, Trigger: trigger, Total: total, Operator: operator,
	}, rule.Graph)
	if err != nil {
		return nil, s.translate(err)
	}
	s.launchRun(*run, rule, compiled)
	return run, nil
}

// launchRun 在后台跑一次执行。同一实例同时最多跑 itemGrantMaxRuns 次，多出来的排队。
func (s *ItemGrantService) launchRun(run itemgrant.Run, rule itemgrant.Rule, compiled *itemgrant.Compiled) {
	if _, loaded := s.active.LoadOrStore(run.ID, true); loaded {
		return
	}
	go func() {
		defer s.active.Delete(run.ID)
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("item grant run panic", zap.Int64("run", run.ID), zap.Any("panic", r))
				_ = s.pg.FinishItemGrantRun(context.Background(), run.ID, itemgrant.RunFailed, fmt.Sprint("执行中断：", r))
			}
		}()
		s.runSlots <- struct{}{}
		defer func() { <-s.runSlots }()
		s.processRun(run, rule, compiled)
	}()
}

func (s *ItemGrantService) processRun(run itemgrant.Run, rule itemgrant.Rule, compiled *itemgrant.Compiled) {
	ctx := context.Background()
	cursor := run.Cursor
	loc := itemGrantLocation()
	for {
		candidates, err := s.pg.ItemGrantCandidates(ctx, run.AppID, compiled.EntryAudience, cursor, itemGrantBatchSize)
		if err != nil {
			s.log.Warn("item grant: load candidates failed", zap.Int64("run", run.ID), zap.Error(err))
			_ = s.pg.FinishItemGrantRun(ctx, run.ID, itemgrant.RunFailed, "读取目标人群失败："+err.Error())
			return
		}
		if len(candidates) == 0 {
			_ = s.pg.FinishItemGrantRun(ctx, run.ID, itemgrant.RunSucceeded, "")
			return
		}
		var progress pgrepo.ItemGrantRunProgress
		var mu sync.Mutex
		var wg sync.WaitGroup
		slots := make(chan struct{}, itemGrantBatchWorkers)
		runID := run.ID
		for _, candidate := range candidates {
			wg.Add(1)
			slots <- struct{}{}
			go func() {
				defer func() { <-slots; wg.Done() }()
				result, err := s.pg.ExecuteItemGrant(ctx, itemgrant.ExecuteInput{
					AppID: run.AppID, Rule: rule, Compiled: compiled, RunID: &runID, UserID: candidate.UserID,
					Trigger: run.Trigger, DedupeKey: "run:" + strconv.FormatInt(run.ID, 10) + ":" + strconv.FormatInt(candidate.UserID, 10),
					Now: time.Now(), Location: loc,
				})
				mu.Lock()
				defer mu.Unlock()
				progress.Processed++
				switch {
				case err != nil:
					progress.Failed++
					s.log.Warn("item grant: execute failed", zap.Int64("run", run.ID), zap.Int64("user", candidate.UserID), zap.Error(err))
				case result.Status == itemgrant.RecordFailed:
					progress.Failed++
				case result.Status == itemgrant.RecordGranted || result.Status == itemgrant.RecordPending:
					progress.Granted++
					s.afterGrant(rule, result)
				default:
					progress.Skipped++
				}
			}()
		}
		wg.Wait()
		cursor = candidates[len(candidates)-1].UserID
		progress.Cursor = cursor
		status, err := s.pg.AdvanceItemGrantRun(ctx, run.ID, progress)
		if err != nil {
			s.log.Warn("item grant: advance run failed", zap.Int64("run", run.ID), zap.Error(err))
			return
		}
		if status != itemgrant.RunRunning {
			// 被取消（或被别的实例结束）：停在这一批之后
			return
		}
		if len(candidates) < itemGrantBatchSize {
			_ = s.pg.FinishItemGrantRun(ctx, run.ID, itemgrant.RunSucceeded, "")
			return
		}
	}
}

// CancelRun 取消执行：当前这批处理完就停，已发的不收回。
func (s *ItemGrantService) CancelRun(ctx context.Context, appID, runID int64, operator string) (*itemgrant.Run, error) {
	run, err := s.pg.CancelItemGrantRun(ctx, appID, runID, operator)
	return run, s.translate(err)
}

// ListRuns 执行记录。
func (s *ItemGrantService) ListRuns(ctx context.Context, query itemgrant.RunQuery) (*itemgrant.Page[itemgrant.Run], error) {
	return s.pg.ListItemGrantRuns(ctx, query)
}

// GetRun 一次执行（含画布快照）。
func (s *ItemGrantService) GetRun(ctx context.Context, appID, runID int64) (*itemgrant.Run, error) {
	run, err := s.pg.GetItemGrantRun(ctx, appID, runID)
	return run, s.translate(err)
}

// ── 记录 ──

// ListRecords 发放记录。
func (s *ItemGrantService) ListRecords(ctx context.Context, query itemgrant.RecordQuery) (*itemgrant.Page[itemgrant.Record], error) {
	return s.pg.ListItemGrantRecords(ctx, query)
}

// GetRecord 一条发放记录。
func (s *ItemGrantService) GetRecord(ctx context.Context, appID, recordID int64) (*itemgrant.Record, error) {
	record, err := s.pg.GetItemGrantRecord(ctx, appID, recordID)
	return record, s.translate(err)
}

// FulfillRecord 人工发放的物品已经处理：标记已发放并通知用户。
func (s *ItemGrantService) FulfillRecord(ctx context.Context, input itemgrant.HandleInput) (*itemgrant.Record, error) {
	record, err := s.pg.FulfillItemGrantRecord(ctx, input)
	if err != nil {
		return nil, s.translate(err)
	}
	pending := []string{}
	for _, result := range record.Results {
		if result.Pending {
			pending = append(pending, result.Detail)
		}
	}
	content := "「" + strings.Join(pending, "、") + "」已发放"
	if strings.TrimSpace(input.Note) != "" {
		content += "：" + strings.TrimSpace(input.Note)
	}
	s.deliver(record.AppID, record.UserID, record.RecordNo, "物品已发放", content, "")
	return record, nil
}

// RetryRecord 重试一条失败的记录：让出幂等键，按规则当前的画布重新为这个人判定、发放。
func (s *ItemGrantService) RetryRecord(ctx context.Context, appID, recordID int64) (*itemgrant.ExecuteResult, error) {
	dedupe, err := s.pg.ItemGrantRecordDedupeKey(ctx, recordID)
	if err != nil {
		return nil, s.translate(err)
	}
	record, err := s.pg.GetItemGrantRecord(ctx, appID, recordID)
	if err != nil {
		return nil, s.translate(err)
	}
	if record.Status != itemgrant.RecordFailed {
		return nil, apperrors.New(40900, http.StatusConflict, "只有发放失败的记录可以重试")
	}
	rule, err := s.pg.GetItemGrantRule(ctx, appID, record.RuleID)
	if err != nil {
		return nil, s.translate(err)
	}
	compiled, issues := itemgrant.Compile(rule.Graph, checkItemGrantCron)
	if compiled == nil {
		return nil, itemGrantIssuesError("规则的画布有错误，不能重试", issues)
	}
	if _, err := s.pg.ReleaseFailedItemGrantRecord(ctx, appID, recordID); err != nil {
		return nil, s.translate(err)
	}
	result, err := s.pg.ExecuteItemGrant(ctx, itemgrant.ExecuteInput{
		AppID: appID, Rule: *rule, Compiled: compiled, RunID: record.RunID, UserID: record.UserID,
		Trigger: record.Trigger, Event: record.Event, EventData: record.EventData, DedupeKey: dedupe,
		Now: time.Now(), Location: itemGrantLocation(),
	})
	if err != nil {
		return nil, s.translate(err)
	}
	if result.Status == itemgrant.RecordGranted || result.Status == itemgrant.RecordPending {
		s.afterGrant(*rule, result)
	}
	return result, nil
}

// Overview 发放总览。
func (s *ItemGrantService) Overview(ctx context.Context, appID int64, days int) (*itemgrant.Overview, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	return s.pg.ItemGrantOverview(ctx, appID, itemGrantDayStart(time.Now()), days)
}

// ── 发放之后 ──

// afterGrant 发放成功之后：按画布上走到的「站内信」节点发消息，并通知在线设备刷新。
func (s *ItemGrantService) afterGrant(rule itemgrant.Rule, result *itemgrant.ExecuteResult) {
	if result == nil || result.Record == nil {
		return
	}
	record := result.Record
	described := make([]string, 0, len(record.Results))
	for _, item := range record.Results {
		described = append(described, item.Detail)
	}
	nickname := record.Nickname
	if nickname == "" {
		nickname = record.Account
	}
	replacer := strings.NewReplacer("{items}", strings.Join(described, "、"), "{nickname}", nickname, "{rule}", rule.Name)
	for _, notify := range result.Outcome.Notifies {
		s.deliver(record.AppID, record.UserID, record.RecordNo, replacer.Replace(notify.Title), replacer.Replace(notify.Content), notify.Route)
	}
	if s.userEvents != nil {
		equipped := false
		for _, item := range record.Results {
			equipped = equipped || item.Equipped
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.userEvents.PublishUserEvent(ctx, record.AppID, record.UserID, "item_grant.granted",
				map[string]any{"recordNo": record.RecordNo, "status": record.Status})
			if equipped {
				// 奖励里 equip = true 的装扮戴上了：与用户自己佩戴同一个事件，客户端重拉 /cosmetics/me
				_ = s.userEvents.PublishUserEvent(ctx, record.AppID, record.UserID, "cosmetic.changed", map[string]any{"action": "equipped"})
			}
		}()
	}
}

func (s *ItemGrantService) deliver(appID, userID int64, recordNo, title, content, route string) {
	if s.notifier == nil || strings.TrimSpace(title) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		input := notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeItemGrant,
			Title: title, Content: content, Level: "success",
			Metadata: map[string]any{"recordNo": recordNo},
		}
		if route != "" {
			input.Action = notificationdomain.RouteAction(route, "查看")
		}
		if _, err := s.notifier.Deliver(ctx, input); err != nil {
			s.log.Warn("item grant: notify user failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

// ── 事件触发 ──

// Emit 用户行为事件：找出这个应用里由它触发的生效规则，逐条为这个人判定、发放（后台执行，不阻塞调用方）。
// key 是事件在规则内的幂等键（同一个键只发一次）。
func (s *ItemGrantService) Emit(appID, userID int64, event, key string, data map[string]any) {
	if s == nil || appID <= 0 || userID <= 0 {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("item grant event panic", zap.String("event", event), zap.Any("panic", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rules, err := s.eventRules(ctx, appID, event)
		if err != nil {
			s.log.Warn("item grant: load event rules failed", zap.Int64("appid", appID), zap.String("event", event), zap.Error(err))
			return
		}
		now := time.Now()
		for _, entry := range rules {
			if !entry.rule.InWindow(now) {
				continue
			}
			s.eventSlots <- struct{}{}
			result, err := s.pg.ExecuteItemGrant(ctx, itemgrant.ExecuteInput{
				AppID: appID, Rule: entry.rule, Compiled: entry.compiled, UserID: userID,
				Trigger: itemgrant.TriggerEvent, Event: event, EventData: data,
				DedupeKey: "event:" + event + ":" + key, Now: now, Location: itemGrantLocation(),
			})
			<-s.eventSlots
			if err != nil {
				s.log.Warn("item grant: event execute failed", zap.Int64("rule", entry.rule.ID), zap.Int64("user", userID), zap.Error(err))
				continue
			}
			if result.Status == itemgrant.RecordGranted || result.Status == itemgrant.RecordPending {
				s.afterGrant(entry.rule, result)
			}
		}
	}()
}

func (s *ItemGrantService) eventRules(ctx context.Context, appID int64, event string) ([]itemGrantCompiledRule, error) {
	key := strconv.FormatInt(appID, 10) + "|" + event
	s.cacheMu.Lock()
	entry, ok := s.eventCache[key]
	s.cacheMu.Unlock()
	if ok && time.Since(entry.loaded) < itemGrantEventCacheTTL {
		return entry.rules, nil
	}
	rules, err := s.pg.ActiveItemGrantEventRules(ctx, appID, event)
	if err != nil {
		return nil, err
	}
	compiled := make([]itemGrantCompiledRule, 0, len(rules))
	for _, rule := range rules {
		program, _ := itemgrant.Compile(rule.Graph, checkItemGrantCron)
		if program == nil {
			s.log.Warn("item grant: active rule does not compile", zap.Int64("rule", rule.ID))
			continue
		}
		compiled = append(compiled, itemGrantCompiledRule{rule: rule, compiled: program})
	}
	s.cacheMu.Lock()
	s.eventCache[key] = itemGrantEventEntry{rules: compiled, loaded: time.Now()}
	s.cacheMu.Unlock()
	return compiled, nil
}

func (s *ItemGrantService) invalidateEvents(appID int64) {
	prefix := strconv.FormatInt(appID, 10) + "|"
	s.cacheMu.Lock()
	for key := range s.eventCache {
		if strings.HasPrefix(key, prefix) {
			delete(s.eventCache, key)
		}
	}
	s.cacheMu.Unlock()
}

// OnSignIn 签到回调（SignInService.afterSignIn）。
func (s *ItemGrantService) OnSignIn(appID, userID int64, consecutiveDays int) {
	date := time.Now().In(itemGrantLocation()).Format(time.DateOnly)
	s.Emit(appID, userID, itemgrant.EventSignIn, strconv.FormatInt(userID, 10)+":"+date,
		map[string]any{"consecutiveDays": consecutiveDays})
}

// OnLogin 登录观察者（AuthService.finalizeLogin，全部登录方式的收口）：每次登录一个幂等键，频次交给限额节点。
func (s *ItemGrantService) OnLogin(appID, userID int64, _ string) {
	s.Emit(appID, userID, itemgrant.EventLogin, strconv.FormatInt(userID, 10)+":"+strconv.FormatInt(time.Now().UnixMilli(), 10),
		map[string]any{})
}

// ObserveHook 插件钩子观察者：注册、支付成功两个钩子转成发放事件。
func (s *ItemGrantService) ObserveHook(hookName string, data map[string]any, meta plugindomain.HookMetadata) {
	if meta.AppID == nil || meta.UserID == nil {
		return
	}
	appID, userID := *meta.AppID, *meta.UserID
	uid := strconv.FormatInt(userID, 10)
	switch hookName {
	case HookUserRegistered:
		s.Emit(appID, userID, itemgrant.EventRegister, uid, map[string]any{})
	case HookPaymentCompleted:
		orderID := fmt.Sprint(data["orderId"])
		payload := map[string]any{}
		if amount, ok := data["amount"]; ok {
			if value, err := strconv.ParseFloat(fmt.Sprint(amount), 64); err == nil {
				payload["amount"] = value
			}
		}
		s.Emit(appID, userID, itemgrant.EventPayment, orderID, payload)
	}
}

// ── 后台循环 ──

// Start 后台循环：到点的定时规则、心跳超时的执行（接着跑）。
func (s *ItemGrantService) Start(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("item grant loop panic", zap.Any("panic", r))
			}
		}()
		timer := time.NewTimer(20 * time.Second)
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
				timer.Reset(itemGrantTickInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *ItemGrantService) Stop() { s.stopOnce.Do(func() { close(s.stop) }) }

func (s *ItemGrantService) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *ItemGrantService) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("item grant tick panic", zap.Any("panic", r))
		}
	}()
	now := time.Now()
	due, err := s.pg.DueItemGrantScheduleRules(ctx, now, 20)
	if err != nil {
		s.log.Warn("item grant: load due rules failed", zap.Error(err))
	}
	for _, rule := range due {
		expected := *rule.NextRunAt
		next := nextItemGrantRun(rule.Cron, now)
		claimed, err := s.pg.AdvanceItemGrantSchedule(ctx, rule.ID, expected, next)
		if err != nil || !claimed {
			continue
		}
		if !rule.InWindow(now) {
			continue
		}
		if _, err := s.startRun(ctx, rule, itemgrant.TriggerSchedule, ""); err != nil {
			s.log.Warn("item grant: scheduled run not started", zap.Int64("rule", rule.ID), zap.Error(err))
		}
	}

	stale, err := s.pg.ClaimStaleItemGrantRuns(ctx, now.Add(-itemGrantStaleAfter), 5)
	if err != nil {
		s.log.Warn("item grant: claim stale runs failed", zap.Error(err))
		return
	}
	for _, run := range stale {
		if _, mine := s.active.Load(run.ID); mine {
			continue
		}
		rule, err := s.pg.GetItemGrantRuleByID(ctx, run.RuleID)
		if err != nil || run.Graph == nil {
			_ = s.pg.FinishItemGrantRun(ctx, run.ID, itemgrant.RunFailed, "规则已不存在")
			continue
		}
		compiled, _ := itemgrant.Compile(*run.Graph, checkItemGrantCron)
		if compiled == nil {
			_ = s.pg.FinishItemGrantRun(ctx, run.ID, itemgrant.RunFailed, "执行时的画布无法编译")
			continue
		}
		s.log.Info("item grant: resuming run", zap.Int64("run", run.ID), zap.Int64("cursor", run.Cursor))
		s.launchRun(run, *rule, compiled)
	}
}

func itemGrantDayStart(now time.Time) time.Time {
	local := now.In(itemGrantLocation())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

func (s *ItemGrantService) translate(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrItemGrantRuleNotFound):
		return apperrors.New(40400, http.StatusNotFound, "发放规则不存在")
	case errors.Is(err, pgrepo.ErrItemGrantRunNotFound):
		return apperrors.New(40400, http.StatusNotFound, "执行记录不存在")
	case errors.Is(err, pgrepo.ErrItemGrantRecordNotFound):
		return apperrors.New(40400, http.StatusNotFound, "发放记录不存在")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40400, http.StatusNotFound, "用户不存在")
	case errors.Is(err, pgrepo.ErrItemGrantRunBusy):
		return apperrors.New(40900, http.StatusConflict, "这条规则正在执行，等它结束或先取消")
	case errors.Is(err, pgrepo.ErrItemGrantRuleHasRecords):
		return apperrors.New(40900, http.StatusConflict, "已有发放记录的规则不能删除，可以归档")
	case errors.Is(err, pgrepo.ErrItemGrantRecordState):
		return apperrors.New(40900, http.StatusConflict, "当前状态不能这样处理")
	}
	return err
}
