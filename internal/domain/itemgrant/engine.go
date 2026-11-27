package itemgrant

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	lotterydomain "aegis/internal/domain/lottery"
)

// 规则引擎：画布编译一次，逐人判定。
//
// 判定是纯函数：输入这个人（基础信息 + 按需取的行为画像）、此刻、事件数据与限额用量，输出走过的路径
// 与要发的物品。发不发、发多少只由它决定；落库、锁、幂等在仓储层。手动预演、单人追踪、正式发放
// 三处调同一个 Evaluate，所以控制台上看到的路径就是真正执行时会走的路径。
//
// 画布按「从触发器出发、沿端口走」解释：同一个端口连出多条线就都走（并行发放），节点只判定一次
// （多条路汇到同一个节点时，后到的不再重复），画布必须无环。

// Env 条件与数量表达式的环境：user.* 行为画像（与抽奖同一份）、now.* 应用时区的此刻、event.* 事件数据。
type Env struct {
	User  lotterydomain.BehaviorProfile `expr:"user"`
	Now   NowEnv                        `expr:"now"`
	Event map[string]any                `expr:"event"`
}

// NowEnv 应用时区的此刻。
type NowEnv struct {
	Hour    int `expr:"hour"`
	Minute  int `expr:"minute"`
	Weekday int `expr:"weekday"`
	Day     int `expr:"day"`
	Month   int `expr:"month"`
	Year    int `expr:"year"`
}

func nowEnv(now time.Time) NowEnv {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return NowEnv{Hour: now.Hour(), Minute: now.Minute(), Weekday: weekday, Day: now.Day(), Month: int(now.Month()), Year: now.Year()}
}

// Basics 一个人的基础信息（取候选人时一并取到，不需要单独查）。
type Basics struct {
	UserID       int64      `json:"userId"`
	Account      string     `json:"account"`
	Nickname     string     `json:"nickname"`
	RegisteredAt time.Time  `json:"registeredAt"`
	Member       bool       `json:"member"`
	Lifetime     bool       `json:"lifetime"`
	Level        int        `json:"level"`
	LastActiveAt *time.Time `json:"lastActiveAt,omitempty"`
	// Eligible 账号可用（启用、未冻结、没有生效中的封禁）；不可用的人不发
	Eligible bool `json:"eligible"`
	// TierIDs 此刻持有的会员档位；LifetimeTierIDs 其中永久持有的
	TierIDs         []int64 `json:"tierIds,omitempty"`
	LifetimeTierIDs []int64 `json:"lifetimeTierIds,omitempty"`
}

// Subject 一次判定的对象。
type Subject struct {
	RuleID  int64
	Basics  Basics
	Trigger string
	Event   map[string]any
	// Now 已换到应用时区的此刻
	Now time.Time
}

// Facts 判定时需要现查的事实。仓储层在发放事务里实现（限额读的是锁住之后的用量）。
type Facts interface {
	// Profile 行为画像；只有表达式引用了 user.* 才会被调用，且一次判定最多一次
	Profile() (lotterydomain.BehaviorProfile, error)
	// Usage 限额节点自 since 起的用量：这个人的、全站的（不需要的那一项可以不查，返回 0）
	Usage(nodeID string, since time.Time, needUser, needGlobal bool) (user int64, global int64, err error)
}

// Step 判定路径上的一步。
type Step struct {
	NodeID string `json:"nodeId"`
	Type   string `json:"type"`
	// Handle 走出去的端口（空为到此为止）
	Handle string `json:"handle,omitempty"`
	Detail string `json:"detail"`
}

// PlannedItem 判定出的一项物品（数量已按人算好）。
type PlannedItem struct {
	NodeID   string `json:"nodeId"`
	OptionID string `json:"optionId,omitempty"`
	Item     Item   `json:"item"`
}

// PlannedNotify 判定出的一条站内信。
type PlannedNotify struct {
	NodeID  string `json:"nodeId"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Route   string `json:"route,omitempty"`
}

// Outcome 一个人的判定结果。
type Outcome struct {
	Steps    []Step          `json:"steps"`
	Items    []PlannedItem   `json:"items"`
	Notifies []PlannedNotify `json:"notifies"`
	// Limits 通过了的限额节点（发放成功后计入它们的用量）
	Limits []string `json:"limits"`
	// Blocked 没有进入画布的原因（账号不可用等），为空表示正常判定
	Blocked string `json:"blocked,omitempty"`
}

// Granting 是否有东西要发。
func (o Outcome) Granting() bool { return len(o.Items) > 0 }

// compiledNode 编译后的节点。
type compiledNode struct {
	id      string
	typ     string
	cfg     any
	out     map[string][]string
	program *vm.Program
	// amounts 数量表达式：键为 "<选项 ID>/<物品下标>"，发放节点的选项 ID 为空
	amounts map[string]*vm.Program
}

// Compiled 编译好的规则。
type Compiled struct {
	Trigger   TriggerConfig
	TriggerID string
	// EntryAudience 紧接在触发器后面的目标人群：手动与定时执行据此取候选人（为空则面向全部可用账号）
	EntryAudience *AudienceConfig
	// NeedsProfile 有表达式引用了行为画像
	NeedsProfile bool
	nodes        map[string]*compiledNode
}

// CronChecker 校验 Cron 表达式（由服务层注入，领域层不依赖调度库）。
type CronChecker func(spec string) error

// Compile 校验并编译画布。返回的问题里有 error 时 Compiled 为 nil。
func Compile(graph Graph, cronCheck CronChecker) (*Compiled, []Issue) {
	issues := []Issue{}
	nodeIssue := func(level, id, format string, args ...any) {
		issues = append(issues, Issue{Level: level, NodeID: id, Message: fmt.Sprintf(format, args...)})
	}
	edgeIssue := func(id, format string, args ...any) {
		issues = append(issues, Issue{Level: IssueError, EdgeID: id, Message: fmt.Sprintf(format, args...)})
	}
	if len(graph.Nodes) > MaxNodes {
		issues = append(issues, Issue{Level: IssueError, Message: fmt.Sprintf("节点不能超过 %d 个", MaxNodes)})
	}
	if len(graph.Edges) > MaxEdges {
		issues = append(issues, Issue{Level: IssueError, Message: fmt.Sprintf("连线不能超过 %d 条", MaxEdges)})
	}
	if HasErrors(issues) {
		return nil, issues
	}

	compiled := &Compiled{nodes: make(map[string]*compiledNode, len(graph.Nodes))}
	triggers := 0
	for _, node := range graph.Nodes {
		if node.ID == "" {
			issues = append(issues, Issue{Level: IssueError, Message: "有节点缺少标识"})
			continue
		}
		if _, dup := compiled.nodes[node.ID]; dup {
			nodeIssue(IssueError, node.ID, "节点标识重复：%s", node.ID)
			continue
		}
		data, err := decodeData(node)
		if err != nil {
			nodeIssue(IssueError, node.ID, "%v", err)
			continue
		}
		for _, problem := range validateConfig(data, cronCheck) {
			nodeIssue(IssueError, node.ID, "%s", problem)
		}
		compiled.nodes[node.ID] = &compiledNode{id: node.ID, typ: node.Type, cfg: data, out: map[string][]string{}}
		if node.Type == NodeTrigger {
			triggers++
			compiled.TriggerID = node.ID
			compiled.Trigger = *data.(*TriggerConfig)
		}
	}
	switch {
	case triggers == 0:
		issues = append(issues, Issue{Level: IssueError, Message: "画布上需要一个触发器"})
	case triggers > 1:
		issues = append(issues, Issue{Level: IssueError, Message: "一条规则只能有一个触发器"})
	}

	seenEdges := map[string]bool{}
	for _, edge := range graph.Edges {
		source, ok := compiled.nodes[edge.Source]
		target, ok2 := compiled.nodes[edge.Target]
		switch {
		case !ok || !ok2:
			edgeIssue(edge.ID, "连线的端点不存在")
			continue
		case edge.Source == edge.Target:
			edgeIssue(edge.ID, "节点不能连到自己")
			continue
		case target.typ == NodeTrigger:
			edgeIssue(edge.ID, "触发器不能作为连线的终点")
			continue
		}
		handle := edge.SourceHandle
		if handle == "" {
			handle = defaultHandle(source)
		}
		if !validHandle(source, handle) {
			edgeIssue(edge.ID, "节点「%s」没有端口 %s", nodeTitle(source), handle)
			continue
		}
		key := edge.Source + "|" + handle + "|" + edge.Target
		if seenEdges[key] {
			continue
		}
		seenEdges[key] = true
		source.out[handle] = append(source.out[handle], edge.Target)
	}

	if compiled.TriggerID != "" && triggers == 1 {
		if cycle := findCycle(compiled); cycle != "" {
			nodeIssue(IssueError, cycle, "画布里有环：沿连线会回到这个节点")
		}
		reachable := compiled.reachable()
		rewards := 0
		for _, node := range graph.Nodes {
			cn, ok := compiled.nodes[node.ID]
			if !ok {
				continue
			}
			if !reachable[node.ID] {
				if node.Type != NodeTrigger {
					nodeIssue(IssueWarning, node.ID, "这个节点从触发器走不到，不会生效")
				}
				continue
			}
			if node.Type == NodeGrant || node.Type == NodeRandom {
				rewards++
			}
			if spec, ok := FindNodeSpec(node.Type); ok && len(spec.Outputs) > 0 && node.Type != NodeGrant &&
				node.Type != NodeRandom && node.Type != NodeNotify && len(cn.out) == 0 {
				nodeIssue(IssueWarning, node.ID, "这个节点没有连出去，走到这里就结束了")
			}
		}
		if rewards == 0 {
			issues = append(issues, Issue{Level: IssueError, Message: "从触发器出发，没有任何「发放物品」或「随机礼包」节点"})
		}
		compiled.EntryAudience = compiled.entryAudience()
		if compiled.Trigger.Kind != TriggerEvent && compiled.EntryAudience == nil {
			issues = append(issues, Issue{Level: IssueWarning, NodeID: compiled.TriggerID,
				Message: "触发器后面没有直接连「目标人群」：执行时会面向全部可用账号"})
		}
	}

	// 表达式：条件与数量
	for _, cn := range compiled.nodes {
		switch cfg := cn.cfg.(type) {
		case *ConditionConfig:
			source := cfg.Expr
			if cfg.Mode != MatchExpr {
				source = RulesExpr(cfg.Mode, cfg.Rules)
			}
			if source == "" {
				continue
			}
			program, err := expr.Compile(source, expr.Env(Env{}), expr.AsBool())
			if err != nil {
				nodeIssue(IssueError, cn.id, "条件表达式无法编译：%s", firstLine(err.Error()))
				continue
			}
			cn.program = program
			compiled.noteExpr(source, cn.id, &issues)
		case *GrantConfig:
			compiled.compileAmounts(cn, "", cfg.Items, &issues)
		case *RandomConfig:
			for _, option := range cfg.Options {
				compiled.compileAmounts(cn, option.ID, option.Items, &issues)
			}
		}
	}
	if HasErrors(issues) {
		return nil, issues
	}
	return compiled, issues
}

func (c *Compiled) compileAmounts(cn *compiledNode, optionID string, items []Item, issues *[]Issue) {
	for i, item := range items {
		if item.AmountExpr == "" {
			continue
		}
		if len(item.AmountExpr) > MaxExprLength {
			*issues = append(*issues, Issue{Level: IssueError, NodeID: cn.id, Message: "数量表达式过长"})
			continue
		}
		program, err := expr.Compile(item.AmountExpr, expr.Env(Env{}), expr.AsFloat64())
		if err != nil {
			*issues = append(*issues, Issue{Level: IssueError, NodeID: cn.id,
				Message: fmt.Sprintf("数量表达式无法编译：%s", firstLine(err.Error()))})
			continue
		}
		if cn.amounts == nil {
			cn.amounts = map[string]*vm.Program{}
		}
		cn.amounts[optionID+"/"+strconv.Itoa(i)] = program
		c.noteExpr(item.AmountExpr, cn.id, issues)
	}
}

// noteExpr 记下表达式的依赖：引用 user.* 要取行为画像；手动 / 定时执行引用 event.* 给个提醒。
func (c *Compiled) noteExpr(source, nodeID string, issues *[]Issue) {
	if strings.Contains(source, "user.") {
		c.NeedsProfile = true
	}
	if strings.Contains(source, "event.") && c.Trigger.Kind != TriggerEvent {
		*issues = append(*issues, Issue{Level: IssueWarning, NodeID: nodeID,
			Message: "手动与定时执行没有事件数据，event.* 一律按 0 计算"})
	}
}

// RulesExpr 把可视化条件拼成表达式（控制台的「查看表达式」与这里同一份）。
func RulesExpr(mode string, rules []ConditionRule) string {
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		field := rule.Field
		if strings.HasPrefix(field, "event.") {
			field = "(" + field + " ?? 0)"
		}
		num := func(i int) string {
			if i >= len(rule.Value) {
				return "0"
			}
			return strconv.FormatFloat(rule.Value[i], 'f', -1, 64)
		}
		var part string
		switch rule.Op {
		case OpEq:
			part = field + " == " + num(0)
		case OpNe:
			part = field + " != " + num(0)
		case OpGt:
			part = field + " > " + num(0)
		case OpGte:
			part = field + " >= " + num(0)
		case OpLt:
			part = field + " < " + num(0)
		case OpLte:
			part = field + " <= " + num(0)
		case OpBetween:
			part = "(" + field + " >= " + num(0) + " && " + field + " <= " + num(1) + ")"
		case OpIn:
			values := make([]string, 0, len(rule.Value))
			for i := range rule.Value {
				values = append(values, num(i))
			}
			part = field + " in [" + strings.Join(values, ", ") + "]"
		case OpTrue:
			part = field + " == true"
		case OpFalse:
			part = field + " == false"
		default:
			continue
		}
		parts = append(parts, part)
	}
	joiner := " && "
	if mode == MatchAny {
		joiner = " || "
	}
	return strings.Join(parts, joiner)
}

func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}

func nodeTitle(cn *compiledNode) string {
	if spec, ok := FindNodeSpec(cn.typ); ok {
		return spec.Label
	}
	return cn.typ
}

func defaultHandle(cn *compiledNode) string {
	spec, ok := FindNodeSpec(cn.typ)
	if ok && len(spec.Outputs) == 1 {
		return spec.Outputs[0].ID
	}
	return ""
}

func validHandle(cn *compiledNode, handle string) bool {
	if cfg, ok := cn.cfg.(*SplitConfig); ok {
		return slices.ContainsFunc(cfg.Branches, func(branch SplitBranch) bool { return branch.ID == handle })
	}
	spec, ok := FindNodeSpec(cn.typ)
	if !ok {
		return false
	}
	return slices.ContainsFunc(spec.Outputs, func(out HandleSpec) bool { return out.ID == handle })
}

func (c *Compiled) successors(cn *compiledNode) []string {
	out := []string{}
	for _, targets := range cn.out {
		out = append(out, targets...)
	}
	return out
}

func (c *Compiled) reachable() map[string]bool {
	seen := map[string]bool{}
	stack := []string{c.TriggerID}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		if cn, ok := c.nodes[id]; ok {
			stack = append(stack, c.successors(cn)...)
		}
	}
	return seen
}

// findCycle 找环（三色 DFS），返回环上的一个节点。
func findCycle(c *Compiled) string {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var found string
	var visit func(id string) bool
	visit = func(id string) bool {
		color[id] = grey
		if cn, ok := c.nodes[id]; ok {
			for _, next := range c.successors(cn) {
				switch color[next] {
				case grey:
					found = next
					return true
				case white:
					if visit(next) {
						return true
					}
				}
			}
		}
		color[id] = black
		return false
	}
	ids := make([]string, 0, len(c.nodes))
	for id := range c.nodes {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		if color[id] == white && visit(id) {
			return found
		}
	}
	return ""
}

func (c *Compiled) entryAudience() *AudienceConfig {
	trigger := c.nodes[c.TriggerID]
	if trigger == nil {
		return nil
	}
	targets := trigger.out[HandleNext]
	if len(targets) != 1 {
		return nil
	}
	if cfg, ok := c.nodes[targets[0]].cfg.(*AudienceConfig); ok {
		copied := *cfg
		return &copied
	}
	return nil
}

// Evaluate 判定一个人。rng 只用于非固定分配的分流与随机礼包。
func (c *Compiled) Evaluate(subject Subject, facts Facts, rng *rand.Rand) (Outcome, error) {
	outcome := Outcome{Steps: []Step{}, Items: []PlannedItem{}, Notifies: []PlannedNotify{}, Limits: []string{}}
	if !subject.Basics.Eligible {
		outcome.Blocked = "账号不可用（已停用、冻结或封禁）"
		return outcome, nil
	}
	env := Env{Now: nowEnv(subject.Now), Event: subject.Event}
	if env.Event == nil {
		env.Event = map[string]any{}
	}
	profileLoaded := false
	loadEnv := func() (Env, error) {
		if c.NeedsProfile && !profileLoaded {
			profile, err := facts.Profile()
			if err != nil {
				return env, err
			}
			env.User = profile
			profileLoaded = true
		}
		return env, nil
	}

	visited := map[string]bool{}
	queue := []string{c.TriggerID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		cn := c.nodes[id]
		if cn == nil {
			continue
		}
		step := Step{NodeID: id, Type: cn.typ}
		var follow []string

		switch cfg := cn.cfg.(type) {
		case *TriggerConfig:
			step.Handle, step.Detail = HandleNext, triggerDetail(*cfg)
		case *AudienceConfig:
			if ok, reason := MatchAudience(*cfg, subject.Basics, subject.Now); ok {
				step.Handle, step.Detail = HandleNext, "在目标人群内"
			} else {
				step.Detail = reason
			}
		case *ConditionConfig:
			current, err := loadEnv()
			if err != nil {
				return outcome, err
			}
			result, err := expr.Run(cn.program, current)
			if err != nil {
				// 算不出来按不满足处理（比如事件数据缺字段），不让整次发放失败
				step.Handle, step.Detail = HandleNo, "表达式出错，按不满足处理："+firstLine(err.Error())
				break
			}
			if matched, _ := result.(bool); matched {
				step.Handle, step.Detail = HandleYes, "满足条件"
			} else {
				step.Handle, step.Detail = HandleNo, "不满足条件"
			}
		case *WindowConfig:
			if InWindow(*cfg, subject.Now) {
				step.Handle, step.Detail = HandleIn, "在时间窗口内"
			} else {
				step.Handle, step.Detail = HandleOut, "在时间窗口外"
			}
		case *SplitConfig:
			branch := pickBranch(*cfg, subject.RuleID, id, subject.Basics.UserID, rng)
			step.Handle, step.Detail = branch.ID, "分到「"+branchLabel(branch)+"」"
		case *LimitConfig:
			since := PeriodStart(cfg.Period, subject.Now)
			user, global, err := facts.Usage(id, since, cfg.PerUser > 0, cfg.Global > 0)
			if err != nil {
				return outcome, err
			}
			switch {
			case cfg.PerUser > 0 && user >= int64(cfg.PerUser):
				step.Handle, step.Detail = HandleOver, fmt.Sprintf("这个人%s已发 %d 次，达到上限 %d", periodWord(cfg.Period), user, cfg.PerUser)
			case cfg.Global > 0 && global >= int64(cfg.Global):
				step.Handle, step.Detail = HandleOver, fmt.Sprintf("全站%s已发 %d 人次，达到上限 %d", periodWord(cfg.Period), global, cfg.Global)
			default:
				step.Handle, step.Detail = HandlePass, limitDetail(*cfg, user, global)
				outcome.Limits = append(outcome.Limits, id)
			}
		case *GrantConfig:
			current, err := loadEnv()
			if err != nil {
				return outcome, err
			}
			for i, item := range cfg.Items {
				outcome.Items = append(outcome.Items, PlannedItem{NodeID: id, Item: resolveAmount(cn, "", i, item, current)})
			}
			step.Handle, step.Detail = HandleNext, fmt.Sprintf("发放 %d 项物品", len(cfg.Items))
		case *RandomConfig:
			current, err := loadEnv()
			if err != nil {
				return outcome, err
			}
			picked := pickOptions(*cfg, rng)
			labels := make([]string, 0, len(picked))
			for _, option := range picked {
				labels = append(labels, optionLabel(option))
				for i, item := range option.Items {
					outcome.Items = append(outcome.Items, PlannedItem{NodeID: id, OptionID: option.ID,
						Item: resolveAmount(cn, option.ID, i, item, current)})
				}
			}
			step.Handle, step.Detail = HandleNext, "抽中「"+strings.Join(labels, "」「")+"」"
		case *NotifyConfig:
			outcome.Notifies = append(outcome.Notifies, PlannedNotify{NodeID: id, Title: cfg.Title, Content: cfg.Content, Route: cfg.Route})
			step.Handle, step.Detail = HandleNext, "发站内信"
		case *EndConfig:
			step.Detail = "结束"
		}
		if step.Handle != "" {
			follow = cn.out[step.Handle]
		}
		outcome.Steps = append(outcome.Steps, step)
		queue = append(queue, follow...)
	}
	return outcome, nil
}

// resolveAmount 数量表达式算出这个人的数量：向下取整、截在上下界内；算不出来退回固定数量。
func resolveAmount(cn *compiledNode, optionID string, index int, item Item, env Env) Item {
	program := cn.amounts[optionID+"/"+strconv.Itoa(index)]
	resolved := item
	resolved.AmountExpr = ""
	if program == nil {
		return resolved
	}
	result, err := expr.Run(program, env)
	if err != nil {
		return resolved
	}
	value, ok := result.(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return resolved
	}
	spec, _ := FindItemSpec(item.Type)
	amount := int64(math.Floor(value))
	amount = max(spec.Min, min(spec.Max, amount))
	resolved.Amount = amount
	return resolved
}

// MatchAudience 这个人是否在目标人群内；不在时给出原因。
func MatchAudience(cfg AudienceConfig, basics Basics, now time.Time) (bool, string) {
	switch cfg.Scope {
	case ScopeMember:
		if !basics.Member {
			return false, "不是会员"
		}
	case ScopeNonMember:
		if basics.Member {
			return false, "已是会员"
		}
	case ScopeLifetime:
		if !basics.Lifetime {
			return false, "不是永久会员"
		}
	case ScopeNonLifetime:
		if basics.Lifetime {
			return false, "已是永久会员"
		}
	}
	registeredDays := now.Sub(basics.RegisteredAt).Hours() / 24
	if cfg.RegisteredWithinDays > 0 && registeredDays > float64(cfg.RegisteredWithinDays) {
		return false, fmt.Sprintf("注册已超过 %d 天", cfg.RegisteredWithinDays)
	}
	if cfg.RegisteredBeforeDays > 0 && registeredDays < float64(cfg.RegisteredBeforeDays) {
		return false, fmt.Sprintf("注册不满 %d 天", cfg.RegisteredBeforeDays)
	}
	if cfg.ActiveWithinDays > 0 {
		if basics.LastActiveAt == nil || now.Sub(*basics.LastActiveAt) > time.Duration(cfg.ActiveWithinDays)*24*time.Hour {
			return false, fmt.Sprintf("最近 %d 天没有活跃", cfg.ActiveWithinDays)
		}
	}
	if cfg.MinLevel > 0 && basics.Level < cfg.MinLevel {
		return false, fmt.Sprintf("等级低于 Lv.%d", cfg.MinLevel)
	}
	if cfg.MaxLevel > 0 && basics.Level > cfg.MaxLevel {
		return false, fmt.Sprintf("等级高于 Lv.%d", cfg.MaxLevel)
	}
	if len(cfg.UserIDs) > 0 && !slices.Contains(cfg.UserIDs, basics.UserID) {
		return false, "不在用户名单里"
	}
	if slices.Contains(cfg.ExcludeUserIDs, basics.UserID) {
		return false, "在排除名单里"
	}
	if len(cfg.TierIDs) > 0 {
		held := basics.TierIDs
		if cfg.TierLifetimeOnly {
			held = basics.LifetimeTierIDs
		}
		if !slices.ContainsFunc(held, func(id int64) bool { return slices.Contains(cfg.TierIDs, id) }) {
			if cfg.TierLifetimeOnly {
				return false, "没有永久持有所选的会员档位"
			}
			return false, "不持有所选的会员档位"
		}
	}
	return true, ""
}

// InWindow 此刻（应用时区）是否在时间窗口内。
func InWindow(cfg WindowConfig, now time.Time) bool {
	date := now.Format(time.DateOnly)
	if cfg.DateFrom != "" && date < cfg.DateFrom {
		return false
	}
	if cfg.DateTo != "" && date > cfg.DateTo {
		return false
	}
	if len(cfg.Weekdays) > 0 {
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		if !slices.Contains(cfg.Weekdays, weekday) {
			return false
		}
	}
	start, end := cfg.StartMinute%1440, cfg.EndMinute
	if end == 1440 {
		end = 0
	}
	if start == end {
		return true
	}
	minute := now.Hour()*60 + now.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

// PeriodStart 限额周期在此刻（应用时区）的起点；累计为零值。
func PeriodStart(period string, now time.Time) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch period {
	case PeriodDay:
		return day
	case PeriodWeek:
		offset := (int(now.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset)
	case PeriodMonth:
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	}
	return time.Time{}
}

func periodWord(period string) string {
	switch period {
	case PeriodDay:
		return "今天"
	case PeriodWeek:
		return "本周"
	case PeriodMonth:
		return "本月"
	}
	return "累计"
}

func limitDetail(cfg LimitConfig, user, global int64) string {
	parts := []string{}
	if cfg.PerUser > 0 {
		parts = append(parts, fmt.Sprintf("个人 %d/%d", user, cfg.PerUser))
	}
	if cfg.Global > 0 {
		parts = append(parts, fmt.Sprintf("全站 %d/%d", global, cfg.Global))
	}
	return periodWord(cfg.Period) + " " + strings.Join(parts, "，")
}

func triggerDetail(cfg TriggerConfig) string {
	switch cfg.Kind {
	case TriggerSchedule:
		return "定时执行 " + cfg.Cron
	case TriggerEvent:
		if spec, ok := FindEventSpec(cfg.Event); ok {
			return "事件：" + spec.Label
		}
	}
	return "手动执行"
}

// stickyUnit 固定分配用的 [0,1) 均匀数：同一条规则、同一个节点、同一个人永远相同。
func stickyUnit(ruleID int64, nodeID string, userID int64) float64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte("itemgrant:" + strconv.FormatInt(ruleID, 10) + ":" + nodeID + ":" + strconv.FormatInt(userID, 10)))
	return float64(hasher.Sum64()>>11) / float64(uint64(1)<<53)
}

func pickBranch(cfg SplitConfig, ruleID int64, nodeID string, userID int64, rng *rand.Rand) SplitBranch {
	total := 0.0
	for _, branch := range cfg.Branches {
		total += branch.Weight
	}
	var unit float64
	if cfg.Sticky || rng == nil {
		unit = stickyUnit(ruleID, nodeID, userID)
	} else {
		unit = rng.Float64()
	}
	target := unit * total
	for _, branch := range cfg.Branches {
		if target < branch.Weight {
			return branch
		}
		target -= branch.Weight
	}
	return cfg.Branches[len(cfg.Branches)-1]
}

// pickOptions 按权重不放回地抽 Picks 个选项（保持配置顺序输出）。
func pickOptions(cfg RandomConfig, rng *rand.Rand) []RandomOption {
	if rng == nil {
		rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x9e3779b97f4a7c15))
	}
	remaining := slices.Clone(cfg.Options)
	picks := min(max(cfg.Picks, 1), len(remaining))
	chosen := map[string]bool{}
	for range picks {
		total := 0.0
		for _, option := range remaining {
			total += option.Weight
		}
		target := rng.Float64() * total
		index := len(remaining) - 1
		for i, option := range remaining {
			if target < option.Weight {
				index = i
				break
			}
			target -= option.Weight
		}
		chosen[remaining[index].ID] = true
		remaining = slices.Delete(remaining, index, index+1)
	}
	out := make([]RandomOption, 0, picks)
	for _, option := range cfg.Options {
		if chosen[option.ID] {
			out = append(out, option)
		}
	}
	return out
}

func branchLabel(branch SplitBranch) string {
	if branch.Label != "" {
		return branch.Label
	}
	return branch.ID
}

func optionLabel(option RandomOption) string {
	if option.Label != "" {
		return option.Label
	}
	return option.ID
}

// Probabilities 随机分流 / 随机礼包每个分支的概率（控制台显示用）。
func Probabilities(weights []float64) []float64 {
	total := 0.0
	for _, weight := range weights {
		total += math.Max(0, weight)
	}
	out := make([]float64, len(weights))
	if total <= 0 {
		return out
	}
	for i, weight := range weights {
		out[i] = math.Max(0, weight) / total
	}
	return out
}
