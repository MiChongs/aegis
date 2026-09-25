package vip

import (
	"sort"
	"strings"
	"time"
)

// 会员段：一条开通记录在「此刻给了什么」上的投影。
//
// ── 为什么权益要跟随套餐，而不是停在开通那一刻 ──
//
// 功能标识最早是**按值快照**进账本的（vip_transactions.features），判定只读快照。
// 结果是套餐配置与已开通用户彻底脱钩：运营给「高级版」加一项 ai.chat，
// 老用户拿不到；把 export 挪到更贵的档位（降级），老用户照用不误。
// 套餐是运营唯一能动的那根杠杆，它拉不动存量用户，等于这根杠杆是断的。
//
// 所以现在的规则是：**套餐还在，权益就按套餐当前的配置算**；
// 快照退回到它真正该做的事 —— 套餐被删之后的兜底（删除时会定格成套餐最后的配置，
// 见 DeleteVipPlan）。不是按套餐开的（自定义发放、卡密赠送天数）本来就只有快照。
//
// ── 为什么窗口要单独一对字段 ──
//
// expire_before / expire_after 是账本，记的是「开通那一刻发生了什么」，不该被改写。
// 但开通之后还会发生两件事会让一段会员期的实际位置变化：
//
//	退款冲正  这一段作废（revoked_at），排在它后面的各段整体前移同样天数
//	扣减天数  从链尾往回截，完全落在截断点之后的段整段失效
//
// 这两件事都只调整 active_from / active_until，账本原值不动。

// Segment 一段仍可能贡献权益的会员期。
type Segment struct {
	ID            int64  `json:"id"`
	TransactionNo string `json:"transactionNo"`
	Channel       string `json:"channel"`
	PlanID        *int64 `json:"planId,omitempty"`
	// PlanName / Features 开通那一刻的快照（套餐已删除时的兜底）
	PlanName string   `json:"planName"`
	Features []string `json:"features"`
	// Plan 所引用套餐的**当前**配置。自定义发放、卡密赠送天数、套餐已删除时为 nil。
	Plan *SegmentPlan `json:"plan,omitempty"`
	// ActiveFrom / ActiveUntil 这一段在会员链上的实际位置（已计入退款前移与扣减截断）
	ActiveFrom  time.Time `json:"activeFrom"`
	ActiveUntil time.Time `json:"activeUntil"`
}

// SegmentPlan 套餐的当前配置（判定只需要这两项）。
type SegmentPlan struct {
	Name     string   `json:"name"`
	Features []string `json:"features"`
}

// EffectiveFeatures 这一段此刻解锁的功能：套餐还在就按套餐现在的配置，否则按快照。
func (s Segment) EffectiveFeatures() []string {
	if s.Plan != nil {
		return s.Plan.Features
	}
	return s.Features
}

// EffectivePlanName 这一段此刻的展示名：套餐改过名就跟着改，套餐没了就用开通时的名字。
func (s Segment) EffectivePlanName() string {
	if s.Plan != nil && strings.TrimSpace(s.Plan.Name) != "" {
		return s.Plan.Name
	}
	return s.PlanName
}

// LiveAt 这一段在 now 时刻是否仍贡献权益：窗口非空且尚未结束。
//
// 刻意**不要求** ActiveFrom <= now：会员期是顺延的，先买基础版再买高级版时
// 高级版那段排在后面、还没轮到，但用户付了高级版的钱，理所当然认为现在就能用。
// 窗口为空（ActiveUntil <= ActiveFrom）的是被扣减整段截掉的，不再算数。
func (s Segment) LiveAt(now time.Time) bool {
	return s.ActiveUntil.After(now) && s.ActiveUntil.After(s.ActiveFrom)
}

// liveSegments 此刻仍贡献权益的段，按开通先后排列。
func liveSegments(segments []Segment, now time.Time) []Segment {
	out := make([]Segment, 0, len(segments))
	for _, segment := range segments {
		if segment.LiveAt(now) {
			out = append(out, segment)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// latestSegment 最近开通的那一段（「当前套餐」「会员来源」取它）。
func latestSegment(live []Segment) *Segment {
	if len(live) == 0 {
		return nil
	}
	latest := live[len(live)-1]
	return &latest
}

// resolveFeatures 各段功能的并集，再与启用中的功能目录取交集。
//
// 与目录取交集是为了让「结论」只有一个口径：停用或删除一个功能标识之后，
// 服务端校验（`VerifyMembership`）已经判它不通过，而 `/vip/status` 的 features、
// 远程函数的 `hasFeature` 如果还列着它，客户端就会亮起一个服务端不认的入口。
func resolveFeatures(live []Segment, catalog []string) []string {
	if len(live) == 0 || len(catalog) == 0 {
		return []string{}
	}
	enabled := make(map[string]struct{}, len(catalog))
	for _, tag := range NormalizeFeatureTags(catalog) {
		enabled[tag] = struct{}{}
	}
	union := make([]string, 0, 8)
	for _, segment := range live {
		union = append(union, segment.EffectiveFeatures()...)
	}
	union = NormalizeFeatureTags(union)
	out := make([]string, 0, len(union))
	for _, tag := range union {
		if _, ok := enabled[tag]; ok {
			out = append(out, tag)
		}
	}
	return out
}
