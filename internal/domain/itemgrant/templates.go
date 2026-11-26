package itemgrant

import (
	"encoding/json"
	"fmt"
	"sort"
)

// 规则模板与校验、预演汇总的辅助。

// Template 一个开箱即用的规则模板（控制台「从模板新建」）。
type Template struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Graph       Graph  `json:"graph"`
}

type graphBuilder struct {
	graph Graph
	edges int
}

func (b *graphBuilder) node(id, nodeType string, x, y float64, data any) string {
	raw, _ := json.Marshal(data)
	b.graph.Nodes = append(b.graph.Nodes, Node{ID: id, Type: nodeType, Position: Position{X: x, Y: y}, Data: raw})
	return id
}

func (b *graphBuilder) link(source, handle, target string) {
	b.edges++
	b.graph.Edges = append(b.graph.Edges, Edge{ID: fmt.Sprintf("e%d", b.edges), Source: source, SourceHandle: handle, Target: target})
}

func newBuilder() *graphBuilder {
	return &graphBuilder{graph: Graph{Nodes: []Node{}, Edges: []Edge{}}}
}

// StarterGraph 新建规则时的初始画布：手动执行 → 全部用户 → 发 10 积分。
func StarterGraph() Graph {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 120, TriggerConfig{Kind: TriggerManual})
	audience := b.node("audience", NodeAudience, 320, 120, AudienceConfig{Scope: ScopeAll})
	grant := b.node("grant", NodeGrant, 640, 120, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 10}}})
	b.link(trigger, HandleNext, audience)
	b.link(audience, HandleNext, grant)
	return b.graph
}

// Templates 内置模板。
func Templates() []Template {
	return []Template{
		templateWelcome(), templateDailyLogin(), templateSignStreak(), templateMemberWeekly(), templateWinBack(),
	}
}

func templateWelcome() Template {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 160, TriggerConfig{Kind: TriggerEvent, Event: EventRegister})
	limit := b.node("limit", NodeLimit, 320, 160, LimitConfig{NodeMeta: NodeMeta{Label: "每人一次"}, Period: PeriodTotal, PerUser: 1})
	grant := b.node("grant", NodeGrant, 640, 100, GrantConfig{NodeMeta: NodeMeta{Label: "新人礼包"}, Items: []Item{
		{Type: ItemPoints, Amount: 100}, {Type: ItemVipDays, Amount: 3}, {Type: ItemLotteryDraws, Amount: 3},
	}})
	notify := b.node("notify", NodeNotify, 960, 100, NotifyConfig{Title: "欢迎加入", Content: "新人礼包已到账：{items}", Route: "points"})
	b.link(trigger, HandleNext, limit)
	b.link(limit, HandlePass, grant)
	b.link(grant, HandleNext, notify)
	return Template{Key: "welcome", Name: "新人礼包", Description: "新用户注册后立即发放积分、会员体验与抽奖次数，并发站内信", Graph: b.graph}
}

func templateDailyLogin() Template {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 160, TriggerConfig{Kind: TriggerEvent, Event: EventLogin})
	limit := b.node("limit", NodeLimit, 320, 160, LimitConfig{NodeMeta: NodeMeta{Label: "每天一次"}, Period: PeriodDay, PerUser: 1})
	condition := b.node("member", NodeCondition, 640, 160, ConditionConfig{NodeMeta: NodeMeta{Label: "是会员"}, Mode: MatchAll,
		Rules: []ConditionRule{{Field: "user.member", Op: OpTrue}}})
	member := b.node("grant-member", NodeGrant, 960, 60, GrantConfig{NodeMeta: NodeMeta{Label: "会员加倍"},
		Items: []Item{{Type: ItemPoints, Amount: 20}}})
	normal := b.node("grant-normal", NodeGrant, 960, 260, GrantConfig{NodeMeta: NodeMeta{Label: "普通用户"},
		Items: []Item{{Type: ItemPoints, Amount: 10}}})
	b.link(trigger, HandleNext, limit)
	b.link(limit, HandlePass, condition)
	b.link(condition, HandleYes, member)
	b.link(condition, HandleNo, normal)
	return Template{Key: "daily-login", Name: "每日登录礼", Description: "每天第一次登录发积分，会员加倍", Graph: b.graph}
}

func templateSignStreak() Template {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 180, TriggerConfig{Kind: TriggerEvent, Event: EventSignIn})
	condition := b.node("streak", NodeCondition, 320, 180, ConditionConfig{NodeMeta: NodeMeta{Label: "连签 7 / 14 / 30 天"},
		Mode: MatchAll, Rules: []ConditionRule{{Field: "event.consecutiveDays", Op: OpIn, Value: []float64{7, 14, 30}}}})
	random := b.node("random", NodeRandom, 640, 120, RandomConfig{NodeMeta: NodeMeta{Label: "连签盲盒"}, Picks: 1, Options: []RandomOption{
		{ID: "a", Label: "大额积分", Weight: 60, Items: []Item{{Type: ItemPoints, Amount: 100, AmountExpr: "50 + event.consecutiveDays * 5"}}},
		{ID: "b", Label: "抽奖次数", Weight: 30, Items: []Item{{Type: ItemLotteryDraws, Amount: 3}}},
		{ID: "c", Label: "会员体验", Weight: 10, Items: []Item{{Type: ItemVipDays, Amount: 3}}},
	}})
	notify := b.node("notify", NodeNotify, 960, 120, NotifyConfig{Title: "连签奖励", Content: "坚持签到，收获{items}", Route: "signin"})
	end := b.node("end", NodeEnd, 640, 320, EndConfig{})
	b.link(trigger, HandleNext, condition)
	b.link(condition, HandleYes, random)
	b.link(condition, HandleNo, end)
	b.link(random, HandleNext, notify)
	return Template{Key: "sign-streak", Name: "连签盲盒", Description: "连续签到满 7、14、30 天时从盲盒里抽一份奖励，积分随连签天数增长", Graph: b.graph}
}

func templateMemberWeekly() Template {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 180, TriggerConfig{Kind: TriggerSchedule, Cron: "0 10 * * 1"})
	audience := b.node("audience", NodeAudience, 320, 180, AudienceConfig{NodeMeta: NodeMeta{Label: "当前会员"}, Scope: ScopeMember, ActiveWithinDays: 30})
	split := b.node("split", NodeSplit, 640, 180, SplitConfig{NodeMeta: NodeMeta{Label: "A/B 测试"}, Sticky: true, Branches: []SplitBranch{
		{ID: "a", Label: "积分组", Weight: 50}, {ID: "b", Label: "抽奖组", Weight: 50},
	}})
	grantA := b.node("grant-a", NodeGrant, 960, 80, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 50}}})
	grantB := b.node("grant-b", NodeGrant, 960, 280, GrantConfig{Items: []Item{{Type: ItemLotteryDraws, Amount: 3}}})
	notify := b.node("notify", NodeNotify, 1280, 180, NotifyConfig{Title: "会员周礼", Content: "本周会员福利已到账：{items}", Route: "membership"})
	b.link(trigger, HandleNext, audience)
	b.link(audience, HandleNext, split)
	b.link(split, "a", grantA)
	b.link(split, "b", grantB)
	b.link(grantA, HandleNext, notify)
	b.link(grantB, HandleNext, notify)
	return Template{Key: "member-weekly", Name: "会员周礼（A/B）", Description: "每周一 10:00 给近 30 天活跃的会员发福利，按用户固定分成两组对比效果", Graph: b.graph}
}

func templateWinBack() Template {
	b := newBuilder()
	trigger := b.node("trigger", NodeTrigger, 0, 180, TriggerConfig{Kind: TriggerManual})
	audience := b.node("audience", NodeAudience, 320, 180, AudienceConfig{NodeMeta: NodeMeta{Label: "注册满 30 天"}, Scope: ScopeAll, RegisteredBeforeDays: 30})
	condition := b.node("dormant", NodeCondition, 640, 180, ConditionConfig{NodeMeta: NodeMeta{Label: "沉睡 14 天以上"}, Mode: MatchAll,
		Rules: []ConditionRule{{Field: "user.recencyDays", Op: OpGte, Value: []float64{14}}}})
	limit := b.node("limit", NodeLimit, 960, 120, LimitConfig{NodeMeta: NodeMeta{Label: "每月一次"}, Period: PeriodMonth, PerUser: 1})
	grant := b.node("grant", NodeGrant, 1280, 120, GrantConfig{Items: []Item{{Type: ItemPoints, Amount: 30}, {Type: ItemAdFree, Amount: 3}}})
	notify := b.node("notify", NodeNotify, 1600, 120, NotifyConfig{Title: "好久不见", Content: "{nickname}，送你{items}，回来看看吧"})
	b.link(trigger, HandleNext, audience)
	b.link(audience, HandleNext, condition)
	b.link(condition, HandleYes, limit)
	b.link(limit, HandlePass, grant)
	b.link(grant, HandleNext, notify)
	return Template{Key: "win-back", Name: "沉睡唤醒", Description: "手动执行：给 14 天没来的老用户发积分与免广告，每人每月最多一次", Graph: b.graph}
}

// CheckRefs 校验画布引用的会员套餐与功能：不存在或是试用套餐的是错误，已下架 / 停用的是提醒或错误。
func CheckRefs(graph Graph, refs Refs) []Issue {
	issues := []Issue{}
	check := func(nodeID string, items []Item) {
		for _, item := range items {
			switch item.Type {
			case ItemVipPlan:
				plan, ok := refs.Plans[item.RefID]
				switch {
				case !ok:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: fmt.Sprintf("会员套餐 #%d 不存在", item.RefID)})
				case plan.Trial:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: "试用套餐不能用来发放：「" + plan.Name + "」"})
				case !plan.Active:
					issues = append(issues, Issue{Level: IssueWarning, NodeID: nodeID, Message: "会员套餐「" + plan.Name + "」已下架，仍会照常发放"})
				}
			case ItemFeature:
				feature, ok := refs.Features[item.Tag]
				switch {
				case !ok:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: "功能 " + item.Tag + " 不存在"})
				case !feature.IsActive:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: "功能「" + feature.Name + "」已停用，解锁发不出去"})
				}
			case ItemFeatureTime:
				if _, ok := refs.Features[item.Tag]; !ok {
					issues = append(issues, Issue{Level: IssueWarning, NodeID: nodeID, Message: "功能 " + item.Tag + " 不在功能目录里，时长照发但客户端可能用不上"})
				}
			case ItemCosmetic:
				cosmetic, ok := refs.Cosmetics[item.RefID]
				switch {
				case !ok:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: fmt.Sprintf("装扮 #%d 不存在", item.RefID)})
				case !cosmetic.Live:
					issues = append(issues, Issue{Level: IssueError, NodeID: nodeID, Message: "装扮「" + cosmetic.Name + "」未上架或未发布，发不出去"})
				}
			}
		}
	}
	for _, node := range graph.Nodes {
		data, err := decodeData(node)
		if err != nil {
			continue
		}
		switch cfg := data.(type) {
		case *GrantConfig:
			check(node.ID, cfg.Items)
		case *RandomConfig:
			for _, option := range cfg.Options {
				check(node.ID, option.Items)
			}
		}
	}
	return issues
}

// Summarize 把抽样判定汇总成预演结果：节点 / 端口的到达人数、按物品的发放量与全量推算。
func Summarize(outcomes []Outcome, candidates int64, refs Refs) *Estimate {
	estimate := &Estimate{
		Candidates: candidates, Sampled: len(outcomes),
		NodeHits: map[string]int{}, HandleHits: map[string]int{}, Items: []EstimateItem{},
	}
	type key struct{ itemType, detail string }
	totals := map[key]*EstimateItem{}
	order := []key{}
	for _, outcome := range outcomes {
		if outcome.Blocked != "" {
			estimate.Blocked++
			continue
		}
		if outcome.Granting() {
			estimate.Granting++
		}
		for _, step := range outcome.Steps {
			estimate.NodeHits[step.NodeID]++
			if step.Handle != "" {
				estimate.HandleHits[step.NodeID+"/"+step.Handle]++
			}
		}
		for _, planned := range outcome.Items {
			spec, _ := FindItemSpec(planned.Item.Type)
			detail := ""
			if spec.Value != ValueAmount {
				detail = DescribePlannedItem(planned.Item, refs)
			}
			k := key{planned.Item.Type, detail}
			entry, ok := totals[k]
			if !ok {
				entry = &EstimateItem{Type: planned.Item.Type, Label: spec.Label, Detail: detail}
				totals[k] = entry
				order = append(order, k)
			}
			entry.Count++
			if spec.Value == ValueAmount || spec.Value == ValueFeatureMinutes {
				entry.Amount += planned.Item.Amount
			}
		}
	}
	scale := 0.0
	if len(outcomes) > 0 {
		scale = float64(candidates) / float64(len(outcomes))
	}
	for _, k := range order {
		entry := totals[k]
		base := float64(entry.Count)
		if entry.Amount > 0 {
			base = float64(entry.Amount)
		}
		entry.Projected = base * scale
		estimate.Items = append(estimate.Items, *entry)
	}
	sort.SliceStable(estimate.Items, func(i, j int) bool { return estimate.Items[i].Count > estimate.Items[j].Count })
	return estimate
}
