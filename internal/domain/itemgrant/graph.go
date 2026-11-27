package itemgrant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 规则画布：节点 + 连线，形状与控制台的 React Flow 一致（position / data / sourceHandle），原样保存原样取回。

// 上界。
const (
	MaxNodes          = 120
	MaxEdges          = 240
	MaxRandomOptions  = 20
	MaxSplitBranches  = 8
	MaxConditionRules = 20
	MaxExprLength     = 2000
	MaxAudienceUsers  = 5000
	MaxLabelRunes     = 40
	MaxNoteRunes      = 200
	MaxNotifyTitle    = 60
	MaxNotifyContent  = 500
)

// Graph 一条规则的画布。
type Graph struct {
	Nodes    []Node    `json:"nodes"`
	Edges    []Edge    `json:"edges"`
	Viewport *Viewport `json:"viewport,omitempty"`
}

// Node 一个节点。Data 按 Type 解析成对应的配置（TriggerConfig、GrantConfig……）。
type Node struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Position Position        `json:"position"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// Edge 一条连线：从 Source 的 SourceHandle 端口连到 Target。
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	Target       string `json:"target"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}

// NodeMeta 所有节点共有的展示字段。
type NodeMeta struct {
	Label string `json:"label,omitempty"`
	Note  string `json:"note,omitempty"`
}

// TriggerConfig 触发器。
type TriggerConfig struct {
	NodeMeta
	Kind string `json:"kind"`
	// Cron 定时执行的 Cron 表达式（五段，应用时区），如 `0 10 * * *`
	Cron string `json:"cron,omitempty"`
	// Event 事件触发的事件键
	Event string `json:"event,omitempty"`
}

// AudienceConfig 目标人群。
type AudienceConfig struct {
	NodeMeta
	Scope string `json:"scope"`
	// RegisteredWithinDays 注册不超过 N 天（0 不限）
	RegisteredWithinDays int `json:"registeredWithinDays,omitempty"`
	// RegisteredBeforeDays 注册满 N 天（0 不限）
	RegisteredBeforeDays int `json:"registeredBeforeDays,omitempty"`
	// ActiveWithinDays 最近 N 天内登录或在线过（0 不限）
	ActiveWithinDays int `json:"activeWithinDays,omitempty"`
	MinLevel         int `json:"minLevel,omitempty"`
	MaxLevel         int `json:"maxLevel,omitempty"`
	// UserIDs 只发给这些人（管理端用户 ID）；空为不限
	UserIDs []int64 `json:"userIds,omitempty"`
	// ExcludeUserIDs 排除这些人
	ExcludeUserIDs []int64 `json:"excludeUserIds,omitempty"`
	// TierIDs 此刻持有其中任一会员档位的人（空为不限）。同为「永久会员」的几档（如领航者与领航者青春版）靠它区分
	TierIDs []int64 `json:"tierIds,omitempty"`
	// TierLifetimeOnly 只认永久持有的档位（限时持有同一档位的不算）
	TierLifetimeOnly bool `json:"tierLifetimeOnly,omitempty"`
}

// MaxAudienceTiers 人群里最多选几个档位。
const MaxAudienceTiers = 20

// ConditionRule 可视化条件的一行：字段 运算符 取值。
type ConditionRule struct {
	Field string    `json:"field"`
	Op    string    `json:"op"`
	Value []float64 `json:"value,omitempty"`
}

// 条件的组合方式。
const (
	MatchAll  = "all"
	MatchAny  = "any"
	MatchExpr = "expr"
)

// ConditionConfig 条件判断：可视化规则（全部满足 / 任一满足）或一条表达式。
type ConditionConfig struct {
	NodeMeta
	Mode  string          `json:"mode"`
	Rules []ConditionRule `json:"rules,omitempty"`
	Expr  string          `json:"expr,omitempty"`
}

// WindowConfig 时间窗口（应用时区）。
type WindowConfig struct {
	NodeMeta
	// Weekdays 1 为周一 … 7 为周日；空为每天
	Weekdays []int `json:"weekdays,omitempty"`
	// StartMinute / EndMinute 每日时段（0–1440）；相等为全天，开始大于结束为跨零点
	StartMinute int `json:"startMinute"`
	EndMinute   int `json:"endMinute"`
	// DateFrom / DateTo 日期范围（YYYY-MM-DD，含两端），空为不限
	DateFrom string `json:"dateFrom,omitempty"`
	DateTo   string `json:"dateTo,omitempty"`
}

// SplitBranch 随机分流的一个分支；ID 即输出端口。
type SplitBranch struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
}

// SplitConfig 随机分流。
type SplitConfig struct {
	NodeMeta
	Branches []SplitBranch `json:"branches"`
	// Sticky 固定分配：同一个人在这条规则里永远进同一个分支（按用户哈希）；否则每次重新抽
	Sticky bool `json:"sticky"`
}

// LimitConfig 频次限额：经过这个节点并且最终发放成功的次数。
type LimitConfig struct {
	NodeMeta
	Period string `json:"period"`
	// PerUser 每人在周期内最多几次（0 不限）
	PerUser int `json:"perUser,omitempty"`
	// Global 全站在周期内最多几人次（0 不限）
	Global int `json:"global,omitempty"`
}

// GrantConfig 发放物品。
type GrantConfig struct {
	NodeMeta
	Items []Item `json:"items"`
}

// RandomOption 随机礼包的一个选项。
type RandomOption struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
	Items  []Item  `json:"items"`
}

// RandomConfig 随机礼包：按权重不放回地抽 Picks 个选项。
type RandomConfig struct {
	NodeMeta
	Picks   int            `json:"picks"`
	Options []RandomOption `json:"options"`
}

// NotifyConfig 站内信。
type NotifyConfig struct {
	NodeMeta
	Title   string `json:"title"`
	Content string `json:"content"`
	// Route 点开后跳转的 App 页面（消息中心的路由键），空为不跳转
	Route string `json:"route,omitempty"`
}

// EndConfig 结束。
type EndConfig struct {
	NodeMeta
}

// Issue 画布上的一个问题；NodeID / EdgeID 指出在哪里（控制台据此高亮）。
type Issue struct {
	Level   string `json:"level"`
	NodeID  string `json:"nodeId,omitempty"`
	EdgeID  string `json:"edgeId,omitempty"`
	Message string `json:"message"`
}

const (
	IssueError   = "error"
	IssueWarning = "warning"
)

// HasErrors 是否有阻止上线的问题。
func HasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Level == IssueError {
			return true
		}
	}
	return false
}

// decodeData 按节点类型解析配置。
func decodeData(node Node) (any, error) {
	var target any
	switch node.Type {
	case NodeTrigger:
		target = &TriggerConfig{}
	case NodeAudience:
		target = &AudienceConfig{}
	case NodeCondition:
		target = &ConditionConfig{}
	case NodeWindow:
		target = &WindowConfig{}
	case NodeSplit:
		target = &SplitConfig{}
	case NodeLimit:
		target = &LimitConfig{}
	case NodeGrant:
		target = &GrantConfig{}
	case NodeRandom:
		target = &RandomConfig{}
	case NodeNotify:
		target = &NotifyConfig{}
	case NodeEnd:
		target = &EndConfig{}
	default:
		return nil, fmt.Errorf("未登记的节点类型：%s", node.Type)
	}
	if len(node.Data) > 0 && string(node.Data) != "null" {
		if err := json.Unmarshal(node.Data, target); err != nil {
			return nil, fmt.Errorf("节点配置格式不对：%v", err)
		}
	}
	return target, nil
}

// TriggerOf 取画布上的触发器配置（规则行上的触发方式、Cron、事件由它派生）。
func TriggerOf(graph Graph) (TriggerConfig, bool) {
	for _, node := range graph.Nodes {
		if node.Type != NodeTrigger {
			continue
		}
		data, err := decodeData(node)
		if err != nil {
			return TriggerConfig{}, false
		}
		return *data.(*TriggerConfig), true
	}
	return TriggerConfig{}, false
}

// NormalizeGraph 清洗画布：去掉空白、规整物品字段、补默认值。不改变结构。
func NormalizeGraph(graph Graph) Graph {
	out := Graph{Nodes: make([]Node, 0, len(graph.Nodes)), Edges: make([]Edge, 0, len(graph.Edges)), Viewport: graph.Viewport}
	for _, node := range graph.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		data, err := decodeData(node)
		if err == nil {
			normalizeConfig(data)
			if raw, err := json.Marshal(data); err == nil {
				node.Data = raw
			}
		}
		out.Nodes = append(out.Nodes, node)
	}
	for _, edge := range graph.Edges {
		edge.ID = strings.TrimSpace(edge.ID)
		edge.Source = strings.TrimSpace(edge.Source)
		edge.Target = strings.TrimSpace(edge.Target)
		edge.SourceHandle = strings.TrimSpace(edge.SourceHandle)
		out.Edges = append(out.Edges, edge)
	}
	return out
}

func normalizeMeta(meta *NodeMeta) {
	meta.Label = strings.TrimSpace(meta.Label)
	meta.Note = strings.TrimSpace(meta.Note)
}

func normalizeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		out = append(out, NormalizeItem(item))
	}
	return out
}

func normalizeConfig(data any) {
	switch cfg := data.(type) {
	case *TriggerConfig:
		normalizeMeta(&cfg.NodeMeta)
		cfg.Cron = strings.Join(strings.Fields(cfg.Cron), " ")
		cfg.Event = strings.TrimSpace(cfg.Event)
		if cfg.Kind == "" {
			cfg.Kind = TriggerManual
		}
		if cfg.Kind != TriggerSchedule {
			cfg.Cron = ""
		}
		if cfg.Kind != TriggerEvent {
			cfg.Event = ""
		}
	case *AudienceConfig:
		normalizeMeta(&cfg.NodeMeta)
		if cfg.Scope == "" {
			cfg.Scope = ScopeAll
		}
		cfg.UserIDs = uniquePositive(cfg.UserIDs)
		cfg.ExcludeUserIDs = uniquePositive(cfg.ExcludeUserIDs)
		cfg.TierIDs = uniquePositive(cfg.TierIDs)
		if len(cfg.TierIDs) == 0 {
			cfg.TierLifetimeOnly = false
		}
	case *ConditionConfig:
		normalizeMeta(&cfg.NodeMeta)
		cfg.Expr = strings.TrimSpace(cfg.Expr)
		if cfg.Mode == "" {
			cfg.Mode = MatchAll
		}
		if cfg.Mode == MatchExpr {
			cfg.Rules = nil
		} else {
			cfg.Expr = ""
		}
	case *WindowConfig:
		normalizeMeta(&cfg.NodeMeta)
		cfg.DateFrom = strings.TrimSpace(cfg.DateFrom)
		cfg.DateTo = strings.TrimSpace(cfg.DateTo)
	case *SplitConfig:
		normalizeMeta(&cfg.NodeMeta)
		for i := range cfg.Branches {
			cfg.Branches[i].ID = strings.TrimSpace(cfg.Branches[i].ID)
			cfg.Branches[i].Label = strings.TrimSpace(cfg.Branches[i].Label)
		}
	case *LimitConfig:
		normalizeMeta(&cfg.NodeMeta)
		if cfg.Period == "" {
			cfg.Period = PeriodTotal
		}
	case *GrantConfig:
		normalizeMeta(&cfg.NodeMeta)
		cfg.Items = normalizeItems(cfg.Items)
	case *RandomConfig:
		normalizeMeta(&cfg.NodeMeta)
		if cfg.Picks <= 0 {
			cfg.Picks = 1
		}
		for i := range cfg.Options {
			cfg.Options[i].ID = strings.TrimSpace(cfg.Options[i].ID)
			cfg.Options[i].Label = strings.TrimSpace(cfg.Options[i].Label)
			cfg.Options[i].Items = normalizeItems(cfg.Options[i].Items)
		}
	case *NotifyConfig:
		normalizeMeta(&cfg.NodeMeta)
		cfg.Title = strings.TrimSpace(cfg.Title)
		cfg.Content = strings.TrimSpace(cfg.Content)
		cfg.Route = strings.TrimSpace(cfg.Route)
	case *EndConfig:
		normalizeMeta(&cfg.NodeMeta)
	}
}

func uniquePositive(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// validateConfig 单个节点配置的形状校验（表达式能否编译在 Compile 里查）。
func validateConfig(data any, cronCheck func(string) error) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	meta := func(m NodeMeta) {
		if utf8.RuneCountInString(m.Label) > MaxLabelRunes {
			add("名称不能超过 %d 字", MaxLabelRunes)
		}
		if utf8.RuneCountInString(m.Note) > MaxNoteRunes {
			add("备注不能超过 %d 字", MaxNoteRunes)
		}
	}
	switch cfg := data.(type) {
	case *TriggerConfig:
		meta(cfg.NodeMeta)
		switch cfg.Kind {
		case TriggerManual:
		case TriggerSchedule:
			if cfg.Cron == "" {
				add("定时执行需要填写 Cron 表达式")
			} else if cronCheck != nil {
				if err := cronCheck(cfg.Cron); err != nil {
					add("Cron 表达式无效：%v", err)
				}
			}
		case TriggerEvent:
			if _, ok := FindEventSpec(cfg.Event); !ok {
				add("没有选择触发的事件")
			}
		default:
			add("未知的触发方式：%s", cfg.Kind)
		}
	case *AudienceConfig:
		meta(cfg.NodeMeta)
		if !validOption(scopeSpecs, cfg.Scope) {
			add("未知的人群范围：%s", cfg.Scope)
		}
		for _, days := range []int{cfg.RegisteredWithinDays, cfg.RegisteredBeforeDays, cfg.ActiveWithinDays} {
			if days < 0 || days > 36500 {
				add("天数需要在 0–36500 之间")
				break
			}
		}
		if cfg.RegisteredWithinDays > 0 && cfg.RegisteredBeforeDays > 0 && cfg.RegisteredBeforeDays > cfg.RegisteredWithinDays {
			add("「注册满」的天数不能多于「注册不超过」的天数")
		}
		if cfg.MinLevel < 0 || cfg.MaxLevel < 0 || (cfg.MaxLevel > 0 && cfg.MinLevel > cfg.MaxLevel) {
			add("等级范围不对")
		}
		if len(cfg.UserIDs) > MaxAudienceUsers || len(cfg.ExcludeUserIDs) > MaxAudienceUsers {
			add("用户名单最多 %d 人", MaxAudienceUsers)
		}
		if len(cfg.TierIDs) > MaxAudienceTiers {
			add("会员档位最多选 %d 个", MaxAudienceTiers)
		}
	case *ConditionConfig:
		meta(cfg.NodeMeta)
		switch cfg.Mode {
		case MatchAll, MatchAny:
			if len(cfg.Rules) == 0 {
				add("至少要有一条条件")
			}
			if len(cfg.Rules) > MaxConditionRules {
				add("最多 %d 条条件", MaxConditionRules)
			}
			for i, rule := range cfg.Rules {
				if err := validateRule(rule); err != nil {
					add("第 %d 条条件：%v", i+1, err)
				}
			}
		case MatchExpr:
			if cfg.Expr == "" {
				add("表达式不能为空")
			}
			if len(cfg.Expr) > MaxExprLength {
				add("表达式不能超过 %d 个字符", MaxExprLength)
			}
		default:
			add("未知的条件组合方式：%s", cfg.Mode)
		}
	case *WindowConfig:
		meta(cfg.NodeMeta)
		for _, day := range cfg.Weekdays {
			if day < 1 || day > 7 {
				add("星期需要在 1–7 之间")
				break
			}
		}
		if cfg.StartMinute < 0 || cfg.StartMinute > 1440 || cfg.EndMinute < 0 || cfg.EndMinute > 1440 {
			add("时段需要在 00:00–24:00 之间")
		}
		var from, to time.Time
		var err error
		if cfg.DateFrom != "" {
			if from, err = time.Parse(time.DateOnly, cfg.DateFrom); err != nil {
				add("开始日期格式应为 YYYY-MM-DD")
			}
		}
		if cfg.DateTo != "" {
			if to, err = time.Parse(time.DateOnly, cfg.DateTo); err != nil {
				add("结束日期格式应为 YYYY-MM-DD")
			}
		}
		if !from.IsZero() && !to.IsZero() && to.Before(from) {
			add("结束日期早于开始日期")
		}
	case *SplitConfig:
		meta(cfg.NodeMeta)
		if len(cfg.Branches) < 2 {
			add("随机分流至少要有两个分支")
		}
		if len(cfg.Branches) > MaxSplitBranches {
			add("随机分流最多 %d 个分支", MaxSplitBranches)
		}
		seen := map[string]bool{}
		for _, branch := range cfg.Branches {
			if branch.ID == "" || seen[branch.ID] {
				add("分支标识为空或重复")
				break
			}
			seen[branch.ID] = true
			if branch.Weight <= 0 || branch.Weight > 1_000_000 {
				add("分支「%s」的权重需要大于 0", branch.Label)
			}
		}
	case *LimitConfig:
		meta(cfg.NodeMeta)
		if !validOption(periodSpecs, cfg.Period) {
			add("未知的限额周期：%s", cfg.Period)
		}
		if cfg.PerUser < 0 || cfg.Global < 0 || cfg.PerUser > 1_000_000 || cfg.Global > 100_000_000 {
			add("限额次数不对")
		}
		if cfg.PerUser == 0 && cfg.Global == 0 {
			add("每人与全站至少设置一项限额")
		}
	case *GrantConfig:
		meta(cfg.NodeMeta)
		if err := ValidateItems(cfg.Items); err != nil {
			add("%v", err)
		}
	case *RandomConfig:
		meta(cfg.NodeMeta)
		if len(cfg.Options) < 1 {
			add("随机礼包至少要有一个选项")
		}
		if len(cfg.Options) > MaxRandomOptions {
			add("随机礼包最多 %d 个选项", MaxRandomOptions)
		}
		if cfg.Picks < 1 || cfg.Picks > len(cfg.Options) {
			add("抽取个数需要在 1–%d 之间", max(1, len(cfg.Options)))
		}
		seen := map[string]bool{}
		for i, option := range cfg.Options {
			if option.ID == "" || seen[option.ID] {
				add("选项标识为空或重复")
				break
			}
			seen[option.ID] = true
			if option.Weight <= 0 || option.Weight > 1_000_000 {
				add("第 %d 个选项的权重需要大于 0", i+1)
			}
			if err := ValidateItems(option.Items); err != nil {
				add("第 %d 个选项：%v", i+1, err)
			}
		}
	case *NotifyConfig:
		meta(cfg.NodeMeta)
		if cfg.Title == "" {
			add("站内信标题不能为空")
		}
		if utf8.RuneCountInString(cfg.Title) > MaxNotifyTitle {
			add("站内信标题不能超过 %d 字", MaxNotifyTitle)
		}
		if utf8.RuneCountInString(cfg.Content) > MaxNotifyContent {
			add("站内信内容不能超过 %d 字", MaxNotifyContent)
		}
		if cfg.Route != "" {
			known := false
			for _, route := range NotifyRoutes() {
				if route.Key == cfg.Route {
					known = true
					break
				}
			}
			if !known {
				add("未知的跳转页面：%s", cfg.Route)
			}
		}
	case *EndConfig:
		meta(cfg.NodeMeta)
	}
	return problems
}

func validateRule(rule ConditionRule) error {
	field, ok := FindField(rule.Field)
	if !ok {
		return fmt.Errorf("未知的字段 %s", rule.Field)
	}
	op, ok := FindOperator(rule.Op)
	if !ok {
		return fmt.Errorf("未知的运算符 %s", rule.Op)
	}
	allowed := false
	for _, kind := range op.Kinds {
		if kind == field.Kind {
			allowed = true
		}
	}
	if !allowed {
		return fmt.Errorf("「%s」不能用「%s」比较", field.Label, op.Label)
	}
	switch {
	case op.Arity >= 0 && len(rule.Value) != op.Arity:
		return fmt.Errorf("「%s」需要 %d 个取值", op.Label, op.Arity)
	case op.Arity < 0 && (len(rule.Value) == 0 || len(rule.Value) > 50):
		return fmt.Errorf("「%s」需要 1–50 个取值", op.Label)
	case op.Op == OpBetween && rule.Value[0] > rule.Value[1]:
		return fmt.Errorf("区间的下限大于上限")
	}
	return nil
}
