package levels

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"

	"aegis/internal/domain/itemgrant"
	lotterydomain "aegis/internal/domain/lottery"
)

// 等级体系的画布：形状与物品发放相同（nodes / edges / viewport，React Flow 口径），原样保存原样取回。
//
// 解释方式：从「体系设置」的 next 端口出发沿主干走，依次经过的「等级」就是 Lv.1、Lv.2……（等级号不存，按顺序编排，
// 插入一级不需要改别的节点）；主干上两级之间可以插一个「晋级条件」。等级的 attach 端口挂「达成奖励」与「等级权益」。

// Graph / Node / Edge / Issue 与物品发放同形。
type (
	Graph    = itemgrant.Graph
	Node     = itemgrant.Node
	Edge     = itemgrant.Edge
	Issue    = itemgrant.Issue
	NodeMeta = itemgrant.NodeMeta
)

const (
	IssueError   = itemgrant.IssueError
	IssueWarning = itemgrant.IssueWarning
)

// HasErrors 是否有阻止发布的问题。
func HasErrors(issues []Issue) bool { return itemgrant.HasErrors(issues) }

// SchemeConfig 体系设置。Label 是体系名称。
type SchemeConfig struct {
	NodeMeta
	// Notify 升级时发站内信
	Notify        bool   `json:"notify"`
	NotifyTitle   string `json:"notifyTitle,omitempty"`
	NotifyContent string `json:"notifyContent,omitempty"`
	NotifyRoute   string `json:"notifyRoute,omitempty"`
	// HideLocked App 不展示还没达到的等级（只露出下一级）
	HideLocked bool `json:"hideLocked,omitempty"`
}

// LevelConfig 一个等级。Label 是等级名称。
type LevelConfig struct {
	NodeMeta
	// Threshold 累计经验门槛（第一级必须为 0）
	Threshold   int64  `json:"threshold"`
	Icon        string `json:"icon,omitempty"`
	Color       string `json:"color,omitempty"`
	Description string `json:"description,omitempty"`
}

// GateConfig 晋级条件：可视化规则（全部 / 任一满足）或一条表达式。
type GateConfig struct {
	NodeMeta
	Mode  string                    `json:"mode"`
	Rules []itemgrant.ConditionRule `json:"rules,omitempty"`
	Expr  string                    `json:"expr,omitempty"`
	// Hint 给用户看的说明（「需要开通会员」）；空时 App 只说「还需满足晋级条件」
	Hint string `json:"hint,omitempty"`
}

// RewardConfig 达成奖励。Label 是礼包名称。
type RewardConfig struct {
	NodeMeta
	Items []itemgrant.Item `json:"items"`
	// Claim auto 自动到账 / manual 用户领取
	Claim string `json:"claim"`
}

// PrivilegeConfig 等级权益。Label 是权益名称。
type PrivilegeConfig struct {
	NodeMeta
	Kind  string  `json:"kind"`
	Value float64 `json:"value,omitempty"`
	Key   string  `json:"key,omitempty"`
	Text  string  `json:"text,omitempty"`
	Icon  string  `json:"icon,omitempty"`
	Scope string  `json:"scope,omitempty"`
}

var (
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	keyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
)

func decodeData(node Node) (any, error) {
	var target any
	switch node.Type {
	case NodeScheme:
		target = &SchemeConfig{}
	case NodeLevel:
		target = &LevelConfig{}
	case NodeGate:
		target = &GateConfig{}
	case NodeReward:
		target = &RewardConfig{}
	case NodePrivilege:
		target = &PrivilegeConfig{}
	default:
		return nil, fmt.Errorf("未知的节点类型 %s", node.Type)
	}
	if len(node.Data) > 0 {
		if err := json.Unmarshal(node.Data, target); err != nil {
			return nil, fmt.Errorf("节点配置无法解析：%v", err)
		}
	}
	return target, nil
}

func trimMeta(meta *NodeMeta) {
	meta.Label = strings.TrimSpace(meta.Label)
	meta.Note = strings.TrimSpace(meta.Note)
}

func normalizeConfig(data any) {
	switch cfg := data.(type) {
	case *SchemeConfig:
		trimMeta(&cfg.NodeMeta)
		cfg.NotifyTitle = strings.TrimSpace(cfg.NotifyTitle)
		cfg.NotifyContent = strings.TrimSpace(cfg.NotifyContent)
		cfg.NotifyRoute = strings.TrimSpace(cfg.NotifyRoute)
	case *LevelConfig:
		trimMeta(&cfg.NodeMeta)
		cfg.Icon = strings.TrimSpace(cfg.Icon)
		cfg.Color = strings.ToLower(strings.TrimSpace(cfg.Color))
		cfg.Description = strings.TrimSpace(cfg.Description)
	case *GateConfig:
		trimMeta(&cfg.NodeMeta)
		if cfg.Mode == "" {
			cfg.Mode = itemgrant.MatchAll
		}
		cfg.Expr = strings.TrimSpace(cfg.Expr)
		cfg.Hint = strings.TrimSpace(cfg.Hint)
		if cfg.Mode != itemgrant.MatchExpr {
			cfg.Expr = ""
		} else {
			cfg.Rules = nil
		}
	case *RewardConfig:
		trimMeta(&cfg.NodeMeta)
		if cfg.Claim != ClaimManual {
			cfg.Claim = ClaimAuto
		}
		items := make([]itemgrant.Item, 0, len(cfg.Items))
		for _, item := range cfg.Items {
			item = itemgrant.NormalizeItem(item)
			// 达成奖励的数量是固定的：一级只发一次，按人计算没有意义
			item.AmountExpr = ""
			items = append(items, item)
		}
		cfg.Items = items
	case *PrivilegeConfig:
		trimMeta(&cfg.NodeMeta)
		cfg.Key = strings.TrimSpace(cfg.Key)
		cfg.Text = strings.TrimSpace(cfg.Text)
		cfg.Icon = strings.TrimSpace(cfg.Icon)
		if cfg.Scope != ScopeOnly {
			cfg.Scope = ScopeInherit
		}
		if spec, ok := FindPrivilegeSpec(cfg.Kind); ok {
			if !spec.NeedsKey {
				cfg.Key = ""
			}
			if !spec.Numeric && cfg.Kind != PrivilegeCustom {
				cfg.Value = 0
			}
		}
	}
}

// NormalizeGraph 整理画布：去空白、补默认值、抹掉与种类无关的字段。
func NormalizeGraph(graph Graph) Graph {
	out := Graph{Nodes: make([]Node, 0, len(graph.Nodes)), Edges: make([]Edge, 0, len(graph.Edges)), Viewport: graph.Viewport}
	for _, node := range graph.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		if data, err := decodeData(node); err == nil {
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

// ── 编译结果 ──

// Scheme 编译好的等级体系。Levels[i] 是 Lv.(i+1)。
type Scheme struct {
	Name          string
	Notify        bool
	NotifyTitle   string
	NotifyContent string
	NotifyRoute   string
	HideLocked    bool
	Levels        []Level
}

// Level 一个等级。
type Level struct {
	Level       int
	NodeID      string
	Name        string
	Threshold   int64
	Icon        string
	Color       string
	Description string
	// Gate 升到这一级要额外满足的条件（nil 为只看经验）
	Gate       *Gate
	Rewards    []Reward
	Privileges []Privilege
}

// Gate 晋级条件。
type Gate struct {
	NodeID  string
	Label   string
	Hint    string
	Source  string
	program *vm.Program
}

// Reward 达成奖励。
type Reward struct {
	NodeID string           `json:"nodeId"`
	Label  string           `json:"label"`
	Items  []itemgrant.Item `json:"items"`
	Claim  string           `json:"claim"`
}

// Key 奖励在一个人身上的唯一键（同一级同一个节点只发一次）。
func (r Reward) Key(level int) string { return fmt.Sprintf("%d:%s", level, r.NodeID) }

// Privilege 等级权益。
type Privilege struct {
	NodeID string  `json:"nodeId"`
	Kind   string  `json:"kind"`
	Label  string  `json:"label"`
	Key    string  `json:"key,omitempty"`
	Value  float64 `json:"value,omitempty"`
	Text   string  `json:"text,omitempty"`
	Icon   string  `json:"icon,omitempty"`
	Scope  string  `json:"scope"`
	// Level 这条权益来自哪一级
	Level int `json:"level"`
}

// GateEnv 晋级条件的表达式环境：user.* 行为画像（与抽奖、物品发放同一份）。
type GateEnv struct {
	User lotterydomain.BehaviorProfile `expr:"user"`
}

// Pass 判定晋级条件。
func (g *Gate) Pass(profile lotterydomain.BehaviorProfile) (bool, error) {
	if g == nil || g.program == nil {
		return true, nil
	}
	out, err := expr.Run(g.program, GateEnv{User: profile})
	if err != nil {
		return false, err
	}
	ok, _ := out.(bool)
	return ok, nil
}

// ── 编译 ──

type compileState struct {
	graph    Graph
	nodes    map[string]Node
	configs  map[string]any
	outgoing map[string][]Edge
	incoming map[string][]Edge
	issues   []Issue
}

func (c *compileState) issue(level, nodeID, format string, args ...any) {
	c.issues = append(c.issues, Issue{Level: level, NodeID: nodeID, Message: fmt.Sprintf(format, args...)})
}

func (c *compileState) edgeIssue(level, edgeID, format string, args ...any) {
	c.issues = append(c.issues, Issue{Level: level, EdgeID: edgeID, Message: fmt.Sprintf(format, args...)})
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// Compile 校验并编译画布。有错误时返回 nil 与全部问题；只有提醒时照常返回。
func Compile(graph Graph) (*Scheme, []Issue) {
	graph = NormalizeGraph(graph)
	c := &compileState{
		graph: graph, nodes: map[string]Node{}, configs: map[string]any{},
		outgoing: map[string][]Edge{}, incoming: map[string][]Edge{}, issues: []Issue{},
	}
	if len(graph.Nodes) > MaxNodes {
		c.issue(IssueError, "", "节点太多（最多 %d 个）", MaxNodes)
		return nil, c.issues
	}
	if len(graph.Edges) > MaxEdges {
		c.issue(IssueError, "", "连线太多（最多 %d 条）", MaxEdges)
		return nil, c.issues
	}

	schemeID := ""
	for _, node := range graph.Nodes {
		if node.ID == "" {
			c.issue(IssueError, "", "有节点缺少标识")
			continue
		}
		if _, dup := c.nodes[node.ID]; dup {
			c.issue(IssueError, node.ID, "节点标识重复：%s", node.ID)
			continue
		}
		c.nodes[node.ID] = node
		data, err := decodeData(node)
		if err != nil {
			c.issue(IssueError, node.ID, "%s", err.Error())
			continue
		}
		c.configs[node.ID] = data
		if node.Type == NodeScheme {
			if schemeID != "" {
				c.issue(IssueError, node.ID, "画布上只能有一个「体系设置」")
				continue
			}
			schemeID = node.ID
		}
		for _, message := range validateConfig(data) {
			c.issue(IssueError, node.ID, "%s", message)
		}
	}
	if schemeID == "" {
		c.issue(IssueError, "", "缺少「体系设置」节点")
		return nil, c.issues
	}

	seenEdge := map[string]bool{}
	for _, edge := range graph.Edges {
		source, okS := c.nodes[edge.Source]
		_, okT := c.nodes[edge.Target]
		if !okS || !okT {
			c.edgeIssue(IssueError, edge.ID, "连线指向不存在的节点")
			continue
		}
		if edge.Source == edge.Target {
			c.edgeIssue(IssueError, edge.ID, "节点不能连到自己")
			continue
		}
		spec, _ := FindNodeSpec(source.Type)
		handle := edge.SourceHandle
		if handle == "" && len(spec.Outputs) == 1 {
			handle = spec.Outputs[0].ID
		}
		valid := false
		for _, out := range spec.Outputs {
			if out.ID == handle {
				valid = true
			}
		}
		if !valid {
			c.edgeIssue(IssueError, edge.ID, "「%s」没有这个输出端口", spec.Label)
			continue
		}
		key := edge.Source + ":" + handle + "->" + edge.Target
		if seenEdge[key] {
			continue
		}
		seenEdge[key] = true
		edge.SourceHandle = handle
		c.outgoing[edge.Source] = append(c.outgoing[edge.Source], edge)
		c.incoming[edge.Target] = append(c.incoming[edge.Target], edge)
	}

	scheme := &Scheme{}
	if cfg, ok := c.configs[schemeID].(*SchemeConfig); ok {
		scheme.Name = cfg.Label
		scheme.Notify = cfg.Notify
		scheme.NotifyTitle = cfg.NotifyTitle
		scheme.NotifyContent = cfg.NotifyContent
		scheme.NotifyRoute = cfg.NotifyRoute
		scheme.HideLocked = cfg.HideLocked
	}

	// 主干：体系设置 → (晋级条件) → 等级 → (晋级条件) → 等级 …
	onSpine := map[string]bool{schemeID: true}
	current := schemeID
	var pendingGate *Gate
	for {
		next := c.spineNext(current)
		if next == "" {
			break
		}
		if onSpine[next] {
			c.issue(IssueError, next, "等级主干绕成了环")
			break
		}
		onSpine[next] = true
		if len(c.incoming[next]) > 1 {
			c.issue(IssueError, next, "主干上的节点只能有一个上游")
		}
		node := c.nodes[next]
		switch cfg := c.configs[next].(type) {
		case *GateConfig:
			if pendingGate != nil {
				c.issue(IssueError, next, "两个晋级条件不能直接相连，合并成一个")
			}
			if len(scheme.Levels) == 0 {
				c.issue(IssueError, next, "第一级之前不能放晋级条件：人人都从第一级开始")
			}
			pendingGate = c.compileGate(next, cfg)
		case *LevelConfig:
			level := Level{
				Level: len(scheme.Levels) + 1, NodeID: next, Name: cfg.Label, Threshold: cfg.Threshold,
				Icon: cfg.Icon, Color: cfg.Color, Description: cfg.Description, Gate: pendingGate,
			}
			pendingGate = nil
			if level.Level == 1 && cfg.Threshold != 0 {
				c.issue(IssueError, next, "第一级的经验门槛必须是 0")
			}
			if n := len(scheme.Levels); n > 0 && cfg.Threshold <= scheme.Levels[n-1].Threshold {
				c.issue(IssueError, next, "Lv.%d 的经验门槛要高于 Lv.%d（%d）", level.Level, n, scheme.Levels[n-1].Threshold)
			}
			c.attach(&level)
			scheme.Levels = append(scheme.Levels, level)
		default:
			c.issue(IssueError, next, "「%s」不能放在等级主干上", nodeLabel(node.Type))
		}
		if len(scheme.Levels) > MaxLevels {
			c.issue(IssueError, next, "等级太多（最多 %d 级）", MaxLevels)
			break
		}
		current = next
	}
	if pendingGate != nil {
		c.issue(IssueError, pendingGate.NodeID, "晋级条件后面没有连等级")
	}
	if len(scheme.Levels) == 0 {
		c.issue(IssueError, schemeID, "「体系设置」后面至少要连一个等级")
	}

	// 不在主干上、也没挂到等级上的节点
	for _, node := range graph.Nodes {
		if onSpine[node.ID] {
			continue
		}
		switch node.Type {
		case NodeLevel, NodeGate:
			c.issue(IssueWarning, node.ID, "这个「%s」没有接在等级主干上，不会生效", nodeLabel(node.Type))
		case NodeReward, NodePrivilege:
			if len(c.incoming[node.ID]) == 0 {
				c.issue(IssueWarning, node.ID, "这个「%s」没有挂到任何等级上，不会生效", nodeLabel(node.Type))
			}
		}
	}

	names := map[string]int{}
	for _, level := range scheme.Levels {
		if level.Name == "" {
			continue
		}
		if prev, dup := names[level.Name]; dup {
			c.issue(IssueWarning, level.NodeID, "等级名称「%s」与 Lv.%d 重复", level.Name, prev)
		}
		names[level.Name] = level.Level
	}

	if HasErrors(c.issues) {
		return nil, c.issues
	}
	return scheme, c.issues
}

// spineNext 主干上的下一个节点（next 端口只能连一条）。
func (c *compileState) spineNext(nodeID string) string {
	var targets []Edge
	for _, edge := range c.outgoing[nodeID] {
		if edge.SourceHandle == HandleNext {
			targets = append(targets, edge)
		}
	}
	if len(targets) > 1 {
		for _, edge := range targets[1:] {
			c.edgeIssue(IssueError, edge.ID, "「下一级」只能连一条线：等级是一条链")
		}
	}
	if len(targets) == 0 {
		return ""
	}
	return targets[0].Target
}

func (c *compileState) attach(level *Level) {
	count := 0
	for _, edge := range c.outgoing[level.NodeID] {
		if edge.SourceHandle != HandleAttach {
			continue
		}
		target := c.nodes[edge.Target]
		if len(c.incoming[edge.Target]) > 1 {
			c.issue(IssueError, edge.Target, "奖励与权益只能挂在一个等级上；要在多级都生效，权益选「本级及以上」，奖励各复制一份")
			continue
		}
		count++
		switch cfg := c.configs[edge.Target].(type) {
		case *RewardConfig:
			label := cfg.Label
			if label == "" {
				label = fmt.Sprintf("Lv.%d 达成奖励", level.Level)
			}
			level.Rewards = append(level.Rewards, Reward{NodeID: target.ID, Label: label, Items: cfg.Items, Claim: cfg.Claim})
		case *PrivilegeConfig:
			label := cfg.Label
			if label == "" {
				if spec, ok := FindPrivilegeSpec(cfg.Kind); ok {
					label = spec.Label
				}
			}
			level.Privileges = append(level.Privileges, Privilege{
				NodeID: target.ID, Kind: cfg.Kind, Label: label, Key: cfg.Key, Value: cfg.Value,
				Text: cfg.Text, Icon: cfg.Icon, Scope: cfg.Scope, Level: level.Level,
			})
		default:
			c.edgeIssue(IssueError, edge.ID, "「奖励与权益」端口只能连「达成奖励」或「等级权益」")
		}
	}
	if count > MaxAttachPerLevel {
		c.issue(IssueError, level.NodeID, "一个等级最多挂 %d 个奖励与权益", MaxAttachPerLevel)
	}
}

func (c *compileState) compileGate(nodeID string, cfg *GateConfig) *Gate {
	gate := &Gate{NodeID: nodeID, Label: cfg.Label, Hint: cfg.Hint}
	source := cfg.Expr
	if cfg.Mode != itemgrant.MatchExpr {
		source = itemgrant.RulesExpr(cfg.Mode, cfg.Rules)
	}
	if source == "" {
		c.issue(IssueWarning, nodeID, "晋级条件是空的，等于只看经验")
		return gate
	}
	gate.Source = source
	program, err := expr.Compile(source, expr.Env(GateEnv{}), expr.AsBool())
	if err != nil {
		message := err.Error()
		if i := strings.IndexByte(message, '\n'); i >= 0 {
			message = message[:i]
		}
		c.issue(IssueError, nodeID, "条件表达式无法编译：%s", message)
		return gate
	}
	gate.program = program
	return gate
}

func nodeLabel(nodeType string) string {
	if spec, ok := FindNodeSpec(nodeType); ok {
		return spec.Label
	}
	return nodeType
}

// validateConfig 单个节点的配置是否合法。
func validateConfig(data any) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }
	switch cfg := data.(type) {
	case *SchemeConfig:
		if runes(cfg.Label) > MaxLabelRunes {
			add("体系名称不能超过 %d 字", MaxLabelRunes)
		}
		if cfg.Notify {
			if cfg.NotifyTitle == "" {
				add("开启升级通知时需要填写标题")
			}
			if runes(cfg.NotifyTitle) > MaxNotifyTitleRunes {
				add("通知标题不能超过 %d 字", MaxNotifyTitleRunes)
			}
			if runes(cfg.NotifyContent) > MaxNotifyContentRunes {
				add("通知内容不能超过 %d 字", MaxNotifyContentRunes)
			}
		}
	case *LevelConfig:
		if cfg.Label == "" {
			add("等级需要一个名称")
		}
		if runes(cfg.Label) > MaxLabelRunes {
			add("等级名称不能超过 %d 字", MaxLabelRunes)
		}
		if cfg.Threshold < 0 || cfg.Threshold > MaxThreshold {
			add("经验门槛需要在 0–%d 之间", int64(MaxThreshold))
		}
		if !ValidIcon(cfg.Icon) {
			add("未知的图标 %s", cfg.Icon)
		}
		if cfg.Color != "" && !colorPattern.MatchString(cfg.Color) {
			add("颜色需要是 #RRGGBB 形式")
		}
		if runes(cfg.Description) > MaxDescriptionRunes {
			add("等级说明不能超过 %d 字", MaxDescriptionRunes)
		}
	case *GateConfig:
		if runes(cfg.Hint) > MaxHintRunes {
			add("给用户的说明不能超过 %d 字", MaxHintRunes)
		}
		switch cfg.Mode {
		case itemgrant.MatchAll, itemgrant.MatchAny:
			if len(cfg.Rules) > MaxConditionRules {
				add("条件最多 %d 行", MaxConditionRules)
			}
			for _, rule := range cfg.Rules {
				if !validGateField(rule.Field) {
					add("晋级条件不能使用字段 %s", rule.Field)
					continue
				}
				if err := validateRule(rule); err != nil {
					add("%s", err.Error())
				}
			}
		case itemgrant.MatchExpr:
			if cfg.Expr == "" {
				add("表达式不能为空")
			}
			if len(cfg.Expr) > MaxExprLength {
				add("表达式不能超过 %d 个字符", MaxExprLength)
			}
		default:
			add("未知的条件组合方式 %s", cfg.Mode)
		}
	case *RewardConfig:
		if runes(cfg.Label) > MaxLabelRunes {
			add("礼包名称不能超过 %d 字", MaxLabelRunes)
		}
		if err := itemgrant.ValidateItems(cfg.Items); err != nil {
			add("%s", err.Error())
		}
		for _, item := range cfg.Items {
			// 同一个号码只能给一个人，人人都会达成的等级奖励里发它没有意义
			if item.Type == itemgrant.ItemNumberAssign {
				add("等级奖励不能发「指定号码」")
			}
		}
	case *PrivilegeConfig:
		spec, ok := FindPrivilegeSpec(cfg.Kind)
		if !ok {
			add("未知的权益种类 %s", cfg.Kind)
			break
		}
		if runes(cfg.Label) > MaxLabelRunes {
			add("权益名称不能超过 %d 字", MaxLabelRunes)
		}
		if cfg.Kind == PrivilegeDisplay && cfg.Label == "" {
			add("展示权益需要一个名称")
		}
		if runes(cfg.Text) > MaxPrivilegeTextRunes {
			add("权益说明不能超过 %d 字", MaxPrivilegeTextRunes)
		}
		if !ValidIcon(cfg.Icon) {
			add("未知的图标 %s", cfg.Icon)
		}
		if spec.Numeric && (cfg.Value < spec.Min || cfg.Value > spec.Max) {
			add("「%s」的数值需要在 %g–%g 之间", spec.Label, spec.Min, spec.Max)
		}
		if spec.NeedsKey {
			if !keyPattern.MatchString(cfg.Key) || len(cfg.Key) > MaxKeyLength {
				add("权益键需要以小写字母开头，只含小写字母、数字、下划线、点与短横线，最长 %d 位", MaxKeyLength)
			}
			if math.Abs(cfg.Value) > spec.Max {
				add("自定义数值超出范围")
			}
		}
	}
	return out
}

// validateRule 一行可视化条件（与物品发放同一套运算符）。
func validateRule(rule itemgrant.ConditionRule) error {
	field, ok := itemgrant.FindField(rule.Field)
	if !ok {
		return fmt.Errorf("未知的字段 %s", rule.Field)
	}
	op, ok := itemgrant.FindOperator(rule.Op)
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
	case op.Op == itemgrant.OpBetween && rule.Value[0] > rule.Value[1]:
		return fmt.Errorf("区间的下限大于上限")
	}
	return nil
}

// CheckRefs 校验奖励引用的会员套餐与功能（与物品发放同一套规则）。
func CheckRefs(scheme *Scheme, refs itemgrant.Refs) []Issue {
	if scheme == nil {
		return nil
	}
	shadow := Graph{}
	for _, level := range scheme.Levels {
		for _, reward := range level.Rewards {
			raw, _ := json.Marshal(itemgrant.GrantConfig{Items: reward.Items})
			shadow.Nodes = append(shadow.Nodes, Node{ID: reward.NodeID, Type: itemgrant.NodeGrant, Data: raw})
		}
	}
	return itemgrant.CheckRefs(shadow, refs)
}
