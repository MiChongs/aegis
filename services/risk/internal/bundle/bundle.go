// Package bundle 是风控发布包：一个场景生效时用到的全部配置，编成不可变的一份 JSON。
//
// risk-admin 从草稿编出发布包、写进 releases 表并推到 etcd；risk-decision watch 到后编译成
// engine.Program 再切换。发布包是两边唯一的契约，字段只加不改。
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// 处置，从轻到重
const (
	ActionPass    = "pass"
	ActionReview  = "review"
	ActionCaptcha = "captcha"
	ActionBlock   = "block"
	ActionBan     = "ban"
)

// ActionRank 处置的轻重，取更严的一个时比较它
func ActionRank(action string) int {
	switch action {
	case ActionPass:
		return 0
	case ActionReview:
		return 1
	case ActionCaptcha:
		return 2
	case ActionBlock:
		return 3
	case ActionBan:
		return 4
	}
	return -1
}

// ValidAction 处置名是否合法
func ValidAction(action string) bool { return ActionRank(action) >= 0 }

// Stricter 取两个处置里更严的一个（空串视为没有意见）
func Stricter(a, b string) string {
	if ActionRank(b) > ActionRank(a) {
		return b
	}
	return a
}

// 风险等级
const (
	LevelNone     = "none"
	LevelLow      = "low"
	LevelMedium   = "medium"
	LevelHigh     = "high"
	LevelCritical = "critical"
)

// Bundle 一个场景的发布包
type Bundle struct {
	Scene       Scene           `json:"scene"`
	Version     string          `json:"version"`
	Checksum    string          `json:"checksum,omitempty"`
	Features    []Feature       `json:"features"`
	Rules       []Rule          `json:"rules"`
	Models      []Model         `json:"models"`
	Bands       []ScoreBand     `json:"bands"`
	Flow        json.RawMessage `json:"flow,omitempty"`   // zen JDM 决策图；空则按 Bands
	Policy      string          `json:"policy,omitempty"` // Rego；空则用内置默认策略
	ListTypes   []string        `json:"listTypes"`        // 本场景查哪些名单类型；空 = 全部
	PublishedAt time.Time       `json:"publishedAt"`
	PublishedBy string          `json:"publishedBy,omitempty"`
	Note        string          `json:"note,omitempty"`
}

// Scene 场景定义
type Scene struct {
	Key              string      `json:"key"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	AppIDs           []int64     `json:"appIds,omitempty"` // 空 = 全部应用
	Fields           []Field     `json:"fields,omitempty"`
	TimeoutMS        int         `json:"timeoutMs"`
	FailMode         string      `json:"failMode"` // open（出错放行）/ closed（出错拒绝）
	CaptchaSupported bool        `json:"captchaSupported"`
	DefaultAction    string      `json:"defaultAction"`
	Enforcement      Enforcement `json:"enforcement"`
}

// Enforcement 最终处置为 ban 时落到哪些对象上
type Enforcement struct {
	Targets         []string `json:"targets"` // device / ip / account / firewall
	DurationSeconds int64    `json:"durationSeconds"`
	AutoBanAllowed  bool     `json:"autoBanAllowed"` // false 时 ban 降为 block（只拦不封）
}

// 场景字段类型
const (
	FieldString = "string"
	FieldNumber = "number"
	FieldBool   = "bool"
	FieldList   = "list"
	FieldObject = "object"
)

// Field 场景入参字段（extra_json 里的键）
type Field struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required,omitempty"`
}

// 特征种类
const (
	FeatureVelocity = "velocity" // 决策服务自己在 Redis Cluster 里维护的滑窗计数
	FeatureStream   = "stream"   // RisingWave 物化视图写回 Redis 的流式特征
	FeatureGraph    = "graph"    // NebulaGraph 关系特征
	FeatureVector   = "vector"   // Milvus 相似检索特征
	FeatureDerived  = "derived"  // expr 派生
)

// 特征主体
const (
	EntityUser        = "user"
	EntityAccount     = "account"
	EntityDevice      = "device"
	EntityIP          = "ip"
	EntityIPSubnet    = "ip_subnet"
	EntityPhone       = "phone"
	EntityEmailDomain = "email_domain"
	EntityApp         = "app"
)

// Entities 全部特征主体
func Entities() []string {
	return []string{EntityUser, EntityAccount, EntityDevice, EntityIP, EntityIPSubnet, EntityPhone, EntityEmailDomain, EntityApp}
}

// Feature 特征定义
type Feature struct {
	Key     string  `json:"key"` // 表达式里写 F.<key>
	Name    string  `json:"name"`
	Kind    string  `json:"kind"`
	Entity  string  `json:"entity,omitempty"`
	Global  bool    `json:"global,omitempty"` // 跨应用统计（默认按应用隔离）
	Default float64 `json:"default"`          // 取不到时的值

	Velocity *VelocityDef `json:"velocity,omitempty"`
	Stream   *StreamDef   `json:"stream,omitempty"`
	Graph    *GraphDef    `json:"graph,omitempty"`
	Vector   *VectorDef   `json:"vector,omitempty"`
	Expr     string       `json:"expr,omitempty"` // derived
}

// 聚合
const (
	AggCount    = "count"
	AggDistinct = "distinct"
	AggSum      = "sum"
	AggMax      = "max"
	AggAvg      = "avg"
)

// VelocityDef 滑窗计数：本次请求算在内
type VelocityDef struct {
	WindowSeconds int    `json:"windowSeconds"`
	Agg           string `json:"agg"`                  // count / distinct
	DistinctOf    string `json:"distinctOf,omitempty"` // distinct 时数哪个主体（如同一 IP 上的不同账号）
	AllScenes     bool   `json:"allScenes,omitempty"`  // 默认只数本场景
}

// StreamDef 流式特征（RisingWave）。决策只读 Redis 里的结果；定义由 risk-admin 生成 SQL。
type StreamDef struct {
	Events        []string    `json:"events"`           // 事件类型：login.fail / decision / payment.paid …
	Scenes        []string    `json:"scenes,omitempty"` // 限定场景
	Agg           string      `json:"agg"`
	Field         string      `json:"field,omitempty"` // distinct / sum / max / avg 的字段
	WindowSeconds int         `json:"windowSeconds"`
	Conditions    []Condition `json:"conditions,omitempty"`
}

// Condition 结构化过滤条件（生成 SQL 时逐项转义，不拼接原文）
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq / ne / gt / gte / lt / lte / in / not_in
	Value any    `json:"value"`
}

// GraphDef 图特征：内置查询模板
type GraphDef struct {
	Template   string `json:"template"`
	WithinDays int    `json:"withinDays,omitempty"`
	CacheTTL   int    `json:"cacheTtl,omitempty"` // 秒，默认 60
}

// 图特征模板
const (
	GraphDeviceAccounts  = "device_accounts"  // 与主体共用设备的其他账号数
	GraphIPAccounts      = "ip_accounts"      // 与主体共用 IP 的其他账号数
	GraphPhoneAccounts   = "phone_accounts"   // 与主体共用手机号的其他账号数
	GraphBannedNeighbors = "banned_neighbors" // 两跳内（经设备 / IP / 手机）被封禁的账号数
	GraphInviteChildren  = "invite_children"  // 直接邀请人数
	GraphInviteTree      = "invite_tree"      // 三层内邀请树规模
	GraphDeviceUsers     = "device_users"     // 这台设备上出现过的账号数（主体为设备）
	GraphIPUsers         = "ip_users"         // 这个 IP 上出现过的账号数（主体为 IP）
)

// GraphTemplates 全部图特征模板
func GraphTemplates() []string {
	return []string{GraphDeviceAccounts, GraphIPAccounts, GraphPhoneAccounts, GraphBannedNeighbors, GraphInviteChildren, GraphInviteTree, GraphDeviceUsers, GraphIPUsers}
}

// VectorDef 向量特征
type VectorDef struct {
	Collection string  `json:"collection"` // device / behavior
	TopK       int     `json:"topK"`
	MinScore   float64 `json:"minScore"`
	Output     string  `json:"output"` // similar_count / similar_banned / max_score
}

// 规则模式
const (
	ModeEnforce = "enforce"
	ModeObserve = "observe"
)

// 维度
const (
	DimNetwork  = "network"
	DimDevice   = "device"
	DimAccount  = "account"
	DimBehavior = "behavior"
	DimGeo      = "geo"
	DimClient   = "client"
	DimList     = "list"
	DimGraph    = "graph"
	DimModel    = "model"
	DimCustom   = "custom"
)

// Dimensions 全部维度
func Dimensions() []string {
	return []string{DimNetwork, DimDevice, DimAccount, DimBehavior, DimGeo, DimClient, DimList, DimGraph, DimModel, DimCustom}
}

// Rule 规则
type Rule struct {
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Dimension string   `json:"dimension"`
	Expr      string   `json:"expr"`
	Score     int      `json:"score"`
	Mode      string   `json:"mode"`
	Decision  string   `json:"decision,omitempty"`
	Priority  int      `json:"priority"`
	Reason    string   `json:"reason,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}

// 模型格式
const (
	ModelLightGBM = "lightgbm"
	ModelXGBoost  = "xgboost"
)

// Model 树模型引用：文件在对象存储里，按 checksum 缓存
type Model struct {
	Key       string   `json:"key"` // 表达式里写 M.<key>
	Name      string   `json:"name"`
	Format    string   `json:"format"`
	ObjectKey string   `json:"objectKey"`
	Checksum  string   `json:"checksum"`
	Inputs    []string `json:"inputs"`    // 输入顺序：特征键或请求字段（amount、device.emulator …）
	Transform bool     `json:"transform"` // 套模型自带的输出变换（如 sigmoid）
}

// ScoreBand 分数段（没配决策流时的默认决策）
type ScoreBand struct {
	Min    int    `json:"min"`
	Max    int    `json:"max"` // 含；<=0 表示无上限
	Action string `json:"action"`
	Level  string `json:"level"`
}

// DefaultBands 内置分数段
func DefaultBands() []ScoreBand {
	return []ScoreBand{
		{Min: 0, Max: 29, Action: ActionPass, Level: LevelNone},
		{Min: 30, Max: 59, Action: ActionPass, Level: LevelLow},
		{Min: 60, Max: 79, Action: ActionCaptcha, Level: LevelMedium},
		{Min: 80, Max: 99, Action: ActionReview, Level: LevelHigh},
		{Min: 100, Max: 0, Action: ActionBlock, Level: LevelCritical},
	}
}

// ComputeChecksum 对内容（不含 checksum 自身与发布时间）算摘要
func (b Bundle) ComputeChecksum() string {
	c := b
	c.Checksum = ""
	c.PublishedAt = time.Time{}
	c.PublishedBy = ""
	c.Note = ""
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// Release etcd 里的发布指针：正式版本 + 可选候选（灰度 / 影子）
type Release struct {
	Scene         string  `json:"scene"`
	Primary       Bundle  `json:"primary"`
	Candidate     *Bundle `json:"candidate,omitempty"`
	CanaryPercent int     `json:"canaryPercent,omitempty"` // 候选版本接管的流量百分比
	Shadow        bool    `json:"shadow,omitempty"`        // 候选版本只评估不生效
}

// 名单类型（与旧风控中心相同的十种，旧名单可直接导入）
const (
	ListIP          = "ip"
	ListCIDR        = "cidr"
	ListDevice      = "device"
	ListAccount     = "account"
	ListUser        = "user"
	ListEmailDomain = "email_domain"
	ListPhonePrefix = "phone_prefix"
	ListASN         = "asn"
	ListCountry     = "country"
	ListUAKeyword   = "ua_keyword"
)

// ListTypes 全部名单类型
func ListTypes() []string {
	return []string{ListIP, ListCIDR, ListDevice, ListAccount, ListUser, ListEmailDomain, ListPhonePrefix, ListASN, ListCountry, ListUAKeyword}
}

// ExactListType 精确值名单走 Redis Cluster；其余（网段、ASN、国家、关键词、域名、号段）走内存快照
func ExactListType(t string) bool {
	switch t {
	case ListIP, ListDevice, ListAccount, ListUser:
		return true
	}
	return false
}

// 名单种类
const (
	KindBlock = "block"
	KindAllow = "allow"
	KindWatch = "watch"
)
