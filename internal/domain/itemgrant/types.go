package itemgrant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// 规则状态。
const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusPaused   = "paused"
	StatusArchived = "archived"
)

// ValidStatus 状态是否合法。
func ValidStatus(status string) bool {
	switch status {
	case StatusDraft, StatusActive, StatusPaused, StatusArchived:
		return true
	}
	return false
}

// 执行状态。
const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

// 发放记录状态。
const (
	RecordGranted   = "granted"
	RecordPending   = "pending"
	RecordFulfilled = "fulfilled"
	RecordFailed    = "failed"
)

// 上界。
const (
	MaxNameRunes        = 40
	MaxDescriptionRunes = 500
)

// Rule 一条发放规则。
type Rule struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appid"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	// Trigger / Event / Cron 从画布的触发器派生（便于按触发方式查询）
	Trigger   string     `json:"trigger"`
	Event     string     `json:"event,omitempty"`
	Cron      string     `json:"cron,omitempty"`
	StartsAt  *time.Time `json:"startsAt,omitempty"`
	EndsAt    *time.Time `json:"endsAt,omitempty"`
	Graph     Graph      `json:"graph"`
	Version   int        `json:"version"`
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
	LastRunAt *time.Time `json:"lastRunAt,omitempty"`
	CreatedBy string     `json:"createdBy,omitempty"`
	UpdatedBy string     `json:"updatedBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	// Stats 列表里附带的发放统计
	Stats *RuleStats `json:"stats,omitempty"`
	// Running 正在执行的那次（没有为空）
	Running *Run `json:"running,omitempty"`
}

// RuleStats 一条规则的发放统计。
type RuleStats struct {
	Records   int64      `json:"records"`
	Users     int64      `json:"users"`
	Today     int64      `json:"today"`
	Pending   int64      `json:"pending"`
	Failed    int64      `json:"failed"`
	LastGrant *time.Time `json:"lastGrantAt,omitempty"`
}

// RuleInput 新建 / 更新规则。
type RuleInput struct {
	AppID       int64
	RuleID      int64
	Name        string
	Description string
	StartsAt    *time.Time
	EndsAt      *time.Time
	Graph       Graph
	Operator    string
}

// NormalizeRuleInput 清洗输入。
func NormalizeRuleInput(input RuleInput) RuleInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Graph = NormalizeGraph(input.Graph)
	return input
}

// ValidateRuleInput 校验规则本身的字段（画布另由 Compile 校验；草稿允许画布有问题）。
func ValidateRuleInput(input RuleInput) error {
	switch {
	case input.Name == "":
		return fmt.Errorf("规则名称不能为空")
	case utf8.RuneCountInString(input.Name) > MaxNameRunes:
		return fmt.Errorf("规则名称不能超过 %d 字", MaxNameRunes)
	case utf8.RuneCountInString(input.Description) > MaxDescriptionRunes:
		return fmt.Errorf("规则说明不能超过 %d 字", MaxDescriptionRunes)
	case input.StartsAt != nil && input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt):
		return fmt.Errorf("结束时间需要晚于开始时间")
	case len(input.Graph.Nodes) > MaxNodes || len(input.Graph.Edges) > MaxEdges:
		return fmt.Errorf("画布过大：最多 %d 个节点、%d 条连线", MaxNodes, MaxEdges)
	}
	if raw, err := json.Marshal(input.Graph); err != nil || len(raw) > 512*1024 {
		return fmt.Errorf("画布过大")
	}
	return nil
}

// InWindow 规则此刻是否在生效期内。
func (r Rule) InWindow(now time.Time) bool {
	if r.StartsAt != nil && now.Before(*r.StartsAt) {
		return false
	}
	if r.EndsAt != nil && !now.Before(*r.EndsAt) {
		return false
	}
	return true
}

// Run 一次批量执行（手动或定时）。
type Run struct {
	ID          int64      `json:"id"`
	AppID       int64      `json:"appid"`
	RuleID      int64      `json:"ruleId"`
	RuleName    string     `json:"ruleName,omitempty"`
	RuleVersion int        `json:"ruleVersion"`
	Trigger     string     `json:"trigger"`
	Status      string     `json:"status"`
	Total       int64      `json:"total"`
	Processed   int64      `json:"processed"`
	Granted     int64      `json:"granted"`
	Skipped     int64      `json:"skipped"`
	Failed      int64      `json:"failed"`
	Cursor      int64      `json:"cursor"`
	Error       string     `json:"error,omitempty"`
	Operator    string     `json:"operator,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	HeartbeatAt time.Time  `json:"heartbeatAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	// Graph 执行时的画布快照（只在详情里返回）
	Graph *Graph `json:"graph,omitempty"`
}

// ItemResult 一项物品的实际发放结果。
type ItemResult struct {
	NodeID        string     `json:"nodeId"`
	OptionID      string     `json:"optionId,omitempty"`
	Type          string     `json:"type"`
	Label         string     `json:"label"`
	Detail        string     `json:"detail"`
	TransactionNo string     `json:"transactionNo,omitempty"`
	ExpireAt      *time.Time `json:"expireAt,omitempty"`
	// Converted 已拥有，按会员折算策略改发（Detail 写明折成了什么）
	Converted bool `json:"converted,omitempty"`
	// Pending 人工发放，等管理员处理
	Pending bool `json:"pending,omitempty"`
	// Equipped 个性装扮按 equip 戴上了
	Equipped bool `json:"equipped,omitempty"`
}

// Record 一条发放记录：一个人在一条规则里的一次发放。
type Record struct {
	ID          int64          `json:"id"`
	RecordNo    string         `json:"recordNo"`
	AppID       int64          `json:"appid"`
	RuleID      int64          `json:"ruleId"`
	RuleName    string         `json:"ruleName,omitempty"`
	RuleVersion int            `json:"ruleVersion"`
	RunID       *int64         `json:"runId,omitempty"`
	UserID      int64          `json:"userId"`
	Account     string         `json:"account,omitempty"`
	Nickname    string         `json:"nickname,omitempty"`
	Trigger     string         `json:"trigger"`
	Event       string         `json:"event,omitempty"`
	Status      string         `json:"status"`
	Steps       []Step         `json:"steps"`
	Items       []PlannedItem  `json:"items"`
	Results     []ItemResult   `json:"results"`
	Error       string         `json:"error,omitempty"`
	EventData   map[string]any `json:"eventData,omitempty"`
	Note        string         `json:"note,omitempty"`
	HandledBy   string         `json:"handledBy,omitempty"`
	HandledAt   *time.Time     `json:"handledAt,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// RecordQuery 发放记录筛选。
type RecordQuery struct {
	AppID   int64
	RuleID  int64
	RunID   int64
	UserID  int64
	Status  string
	Keyword string
	Start   *time.Time
	End     *time.Time
	Page    int
	Limit   int
}

// RunQuery 执行记录筛选。
type RunQuery struct {
	AppID  int64
	RuleID int64
	Page   int
	Limit  int
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// ExecuteInput 给一个人执行一条规则（仓储层的单人事务）。
type ExecuteInput struct {
	AppID    int64
	Rule     Rule
	Compiled *Compiled
	RunID    *int64
	UserID   int64
	Trigger  string
	Event    string
	// EventData 事件自带的数据（条件里的 event.*）
	EventData map[string]any
	// DedupeKey 同一条规则内的幂等键：同一个键只会成功发放一次
	DedupeKey string
	Now       time.Time
	// Location 应用时区（判定的「此刻」「今天」都按它）
	Location *time.Location
}

// ExecuteResult 单人执行的结果。
type ExecuteResult struct {
	// Status granted / pending：已发放；skipped：没有命中任何物品；duplicate：同一幂等键已经发过；failed：发放失败（已回滚）
	Status  string  `json:"status"`
	Outcome Outcome `json:"outcome"`
	Record  *Record `json:"record,omitempty"`
	Error   string  `json:"error,omitempty"`
}

// 单人执行的结果状态（granted / pending / failed 与记录状态同名）。
const (
	ExecSkipped   = "skipped"
	ExecDuplicate = "duplicate"
)

// Candidate 批量执行的一个候选人。
type Candidate = Basics

// Trace 单人预演：这个人此刻会走哪条路、拿到什么（不发放）。
type Trace struct {
	User    Basics   `json:"user"`
	Outcome Outcome  `json:"outcome"`
	Items   []string `json:"items"`
}

// Estimate 预演：目标人群规模，与抽样判定的结果分布。
type Estimate struct {
	Candidates int64 `json:"candidates"`
	Sampled    int   `json:"sampled"`
	Granting   int   `json:"granting"`
	// NodeHits 抽样里每个节点被走到的人数；HandleHits 每个端口走出去的人数（键 "节点/端口"）
	NodeHits   map[string]int `json:"nodeHits"`
	HandleHits map[string]int `json:"handleHits"`
	// Items 抽样里按物品汇总的发放量（数量型为总数，其它为份数），并按候选人规模推算全量
	Items []EstimateItem `json:"items"`
	// Blocked 抽样里因账号不可用被跳过的人数
	Blocked int `json:"blocked"`
}

// EstimateItem 预演的一档物品汇总。
type EstimateItem struct {
	Type      string  `json:"type"`
	Label     string  `json:"label"`
	Detail    string  `json:"detail"`
	Count     int     `json:"count"`
	Amount    int64   `json:"amount"`
	Projected float64 `json:"projected"`
}

// Overview 物品发放总览。
type Overview struct {
	Rules       int64           `json:"rules"`
	ActiveRules int64           `json:"activeRules"`
	Records     int64           `json:"records"`
	Users       int64           `json:"users"`
	Today       int64           `json:"today"`
	Pending     int64           `json:"pending"`
	Failed      int64           `json:"failed"`
	Running     int64           `json:"running"`
	Trend       []TrendPoint    `json:"trend"`
	ByItem      []ItemAggregate `json:"byItem"`
}

// TrendPoint 每天的发放量。
type TrendPoint struct {
	Date    string `json:"date"`
	Records int64  `json:"records"`
	Users   int64  `json:"users"`
}

// ItemAggregate 按物品类型汇总（近 30 天）。
type ItemAggregate struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Count  int64  `json:"count"`
	Amount int64  `json:"amount"`
}

// HandleInput 处理一条待发放的记录。
type HandleInput struct {
	AppID    int64
	RecordID int64
	Note     string
	Operator string
}
