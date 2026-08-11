package security

import "time"

// ════════════════════════════════════════════════════════════
//  枚举与目录常量
//
//  这些常量是「风控中心自描述目录」的真实来源：后端判定读它，
//  /risk/metadata 把同一份下发给控制台驱动表单与图例。
//  在前端另抄一份枚举，后端新增一种条件类型时前端就会静默漏掉。
// ════════════════════════════════════════════════════════════

// 评估场景
const (
	SceneRegister = "register"
	SceneLogin    = "login"
	ScenePayment  = "payment"
	SceneInvite   = "invite"
	SceneLottery  = "lottery"
	SceneAPI      = "api"
)

// 风险等级（由总分映射，见 RiskLevelBands）
const (
	LevelNormal   = "normal"
	LevelLow      = "low"
	LevelMedium   = "medium"
	LevelHigh     = "high"
	LevelCritical = "critical"
)

// 处置动作
const (
	ActionPass    = "pass"
	ActionCaptcha = "captcha"
	ActionReview  = "review"
	ActionBlock   = "block"
	ActionBan     = "ban"
)

// 规则条件类型
const (
	CondIPFrequency     = "ip_frequency"
	CondRateLimited     = "rate_limited"
	CondDeviceNew       = "device_new"
	CondDeviceShared    = "device_shared"
	CondAccountVelocity = "account_velocity"
	CondUABot           = "ua_bot"
	CondUAMissing       = "ua_missing"
	CondUADeviceClass   = "ua_device_class"
	CondIPProxy         = "ip_proxy"
	CondIPReputation    = "ip_reputation"
	CondIPCIDR          = "ip_cidr"
	CondGeoAnomaly      = "geo_anomaly"
	CondGeoCountryIn    = "geo_country_in"
	CondGeoCountryNotIn = "geo_country_not_in"
	CondASNIn           = "asn_in"
	CondTimeWindow      = "time_window"
	CondCustomExpr      = "custom_expr"
)

// 设备 / IP 风险标签
const (
	TagNormal     = "normal"
	TagSuspicious = "suspicious"
	TagBlocked    = "blocked"
	TagTrusted    = "trusted"

	TagProxy      = "proxy"
	TagVPN        = "vpn"
	TagTor        = "tor"
	TagDatacenter = "datacenter"
	TagBot        = "bot"
)

// RiskLevelBand 一档风险等级的分数区间。
// MaxScore 为 nil 表示该档无上界（critical）。
type RiskLevelBand struct {
	Level    string `json:"level"`
	MinScore int    `json:"minScore"`
	MaxScore *int   `json:"maxScore,omitempty"`
}

// RiskLevelBands 分数 → 等级的唯一映射表。
// ScoreToLevel 与 /risk/metadata 读同一份，控制台的分段图例因此不会与判定漂移。
var RiskLevelBands = []RiskLevelBand{
	{Level: LevelNormal, MinScore: 0, MaxScore: new(20)},
	{Level: LevelLow, MinScore: 21, MaxScore: new(40)},
	{Level: LevelMedium, MinScore: 41, MaxScore: new(60)},
	{Level: LevelHigh, MinScore: 61, MaxScore: new(80)},
	{Level: LevelCritical, MinScore: 81},
}

// ScoreToLevel 把总分映射成风险等级。
func ScoreToLevel(score int) string {
	for _, band := range RiskLevelBands {
		if score < band.MinScore {
			continue
		}
		if band.MaxScore == nil || score <= *band.MaxScore {
			return band.Level
		}
	}
	return LevelCritical
}

// ════════════════════════════════════════════════════════════
//  核心实体
// ════════════════════════════════════════════════════════════

// RiskRule 风险规则定义
type RiskRule struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Scene         string         `json:"scene"`
	ConditionType string         `json:"conditionType"`
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
	IsActive      bool           `json:"isActive"`
	Priority      int            `json:"priority"`
	// HitCount / LastHitAt 是「这条规则到底有没有在生效」的直接答案。
	// 只落库不生效的规则比没有这条规则更危险 —— 管理员会以为已经防住了。
	HitCount  int64      `json:"hitCount"`
	LastHitAt *time.Time `json:"lastHitAt,omitempty"`
	CreatedBy *int64     `json:"createdBy,omitempty"`
	UpdatedBy *int64     `json:"updatedBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// RiskAssessment 风险评估记录
type RiskAssessment struct {
	ID           int64         `json:"id"`
	Scene        string        `json:"scene"`
	AppID        *int64        `json:"appId,omitempty"`
	UserID       *int64        `json:"userId,omitempty"`
	IdentityID   *int64        `json:"identityId,omitempty"`
	Account      string        `json:"account"`
	IP           string        `json:"ip"`
	DeviceID     string        `json:"deviceId"`
	UserAgent    string        `json:"userAgent"`
	Country      string        `json:"country"`
	TotalScore   int           `json:"totalScore"`
	RiskLevel    string        `json:"riskLevel"`
	MatchedRules []MatchedRule `json:"matchedRules"`
	// EvalContext 是评估当时引擎看到的全部环境变量快照。
	// 没有它，一条「命中 3 条规则、判 block」的记录无法解释成因，
	// 复核人只能凭猜；有了它，详情页可以逐条展示「判据是什么、当时的值是多少」。
	EvalContext   map[string]any `json:"evalContext,omitempty"`
	LatencyMS     int            `json:"latencyMs"`
	Action        string         `json:"action"`
	ActionDetail  string         `json:"actionDetail"`
	Reviewed      bool           `json:"reviewed"`
	ReviewerID    *int64         `json:"reviewerId,omitempty"`
	ReviewerName  string         `json:"reviewerName,omitempty"`
	ReviewResult  string         `json:"reviewResult,omitempty"`
	ReviewComment string         `json:"reviewComment"`
	ReviewedAt    *time.Time     `json:"reviewedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

// MatchedRule 命中的规则。
// 除了「命中了什么」，还记下命中当时的条件快照与人类可读的判据说明，
// 规则事后被改过也不影响这条历史记录的可解释性。
type MatchedRule struct {
	RuleID        int64  `json:"ruleId"`
	RuleName      string `json:"ruleName"`
	ConditionType string `json:"conditionType"`
	Score         int    `json:"score"`
	Reason        string `json:"reason,omitempty"`
}

// DeviceFingerprint 设备指纹
type DeviceFingerprint struct {
	ID          int64          `json:"id"`
	DeviceID    string         `json:"deviceId"`
	UserID      *int64         `json:"userId,omitempty"`
	AppID       *int64         `json:"appId,omitempty"`
	Fingerprint map[string]any `json:"fingerprint"`
	RiskTag     string         `json:"riskTag"`
	LastIP      string         `json:"lastIp"`
	UserAgent   string         `json:"userAgent"`
	Note        string         `json:"note"`
	FirstSeenAt time.Time      `json:"firstSeenAt"`
	LastSeenAt  time.Time      `json:"lastSeenAt"`
	SeenCount   int            `json:"seenCount"`
}

// IPRiskRecord IP 风险记录
type IPRiskRecord struct {
	ID            int64     `json:"id"`
	IP            string    `json:"ip"`
	RiskTag       string    `json:"riskTag"`
	RiskScore     int       `json:"riskScore"`
	Country       string    `json:"country"`
	Region        string    `json:"region"`
	ISP           string    `json:"isp"`
	ASN           string    `json:"asn"`
	Source        string    `json:"source"`
	Note          string    `json:"note"`
	IsProxy       bool      `json:"isProxy"`
	IsVPN         bool      `json:"isVpn"`
	IsTor         bool      `json:"isTor"`
	IsDatacenter  bool      `json:"isDatacenter"`
	TotalRequests int64     `json:"totalRequests"`
	TotalBlocks   int64     `json:"totalBlocks"`
	FirstSeenAt   time.Time `json:"firstSeenAt"`
	LastSeenAt    time.Time `json:"lastSeenAt"`
}

// RiskAction 自动处置策略
type RiskAction struct {
	ID          int64     `json:"id"`
	Scene       string    `json:"scene"`
	MinScore    int       `json:"minScore"`
	MaxScore    *int      `json:"maxScore,omitempty"`
	Action      string    `json:"action"`
	BanDuration int       `json:"banDuration"`
	Description string    `json:"description"`
	IsActive    bool      `json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ════════════════════════════════════════════════════════════
//  评估请求与结果
// ════════════════════════════════════════════════════════════

// RiskEvalRequest 风险评估请求
type RiskEvalRequest struct {
	Scene      string         `json:"scene"`
	AppID      *int64         `json:"appId,omitempty"`
	UserID     *int64         `json:"userId,omitempty"`
	IdentityID *int64         `json:"identityId,omitempty"`
	IP         string         `json:"ip"`
	DeviceID   string         `json:"deviceId"`
	UserAgent  string         `json:"userAgent"`
	Extra      map[string]any `json:"extra,omitempty"`
}

// RiskEvalResult 风险评估结果
type RiskEvalResult struct {
	AssessmentID int64          `json:"assessmentId,omitempty"`
	TotalScore   int            `json:"totalScore"`
	RiskLevel    string         `json:"riskLevel"`
	MatchedRules []MatchedRule  `json:"matchedRules"`
	Action       string         `json:"action"`
	ActionDetail string         `json:"actionDetail"`
	LatencyMS    int            `json:"latencyMs"`
	EvalContext  map[string]any `json:"evalContext,omitempty"`
	// EvaluatedRules 是**全部**参评规则的逐条结论（含未命中的）。
	// 模拟器上只显示命中项等于让人猜「另外几条为什么没中」，
	// 排查规则写错的成本会高一个量级。
	EvaluatedRules []RuleEvaluation `json:"evaluatedRules,omitempty"`
}

// RuleEvaluation 单条规则的评估轨迹
type RuleEvaluation struct {
	RuleID        int64  `json:"ruleId"`
	RuleName      string `json:"ruleName"`
	ConditionType string `json:"conditionType"`
	Score         int    `json:"score"`
	Priority      int    `json:"priority"`
	Hit           bool   `json:"hit"`
	Reason        string `json:"reason"`
	Error         string `json:"error,omitempty"`
}

// ════════════════════════════════════════════════════════════
//  详情视图（控制台「看得懂完整详情」的载体）
// ════════════════════════════════════════════════════════════

// RiskAssessmentDetail 评估记录详情：记录本身 + 判据 + 上下文关联
type RiskAssessmentDetail struct {
	Assessment  RiskAssessment     `json:"assessment"`
	Device      *DeviceFingerprint `json:"device,omitempty"`
	IPRecord    *IPRiskRecord      `json:"ipRecord,omitempty"`
	Rules       []RuleEvaluation   `json:"rules"`
	SameIP      []RiskAssessment   `json:"sameIp"`
	SameDevice  []RiskAssessment   `json:"sameDevice"`
	SameAccount []RiskAssessment   `json:"sameAccount"`
	IPSummary   RiskEntitySummary  `json:"ipSummary"`
	DevSummary  RiskEntitySummary  `json:"deviceSummary"`
}

// RiskEntitySummary 某个实体（IP / 设备 / 账号）在窗口内的聚合画像
type RiskEntitySummary struct {
	TotalAssessments int64      `json:"totalAssessments"`
	Blocked          int64      `json:"blocked"`
	AvgScore         float64    `json:"avgScore"`
	MaxScore         int        `json:"maxScore"`
	DistinctAccounts int64      `json:"distinctAccounts"`
	DistinctDevices  int64      `json:"distinctDevices"`
	DistinctIPs      int64      `json:"distinctIps"`
	FirstSeenAt      *time.Time `json:"firstSeenAt,omitempty"`
	LastSeenAt       *time.Time `json:"lastSeenAt,omitempty"`
}

// RiskRuleDetail 规则详情：定义 + 效果 + 最近命中
type RiskRuleDetail struct {
	Rule        RiskRule         `json:"rule"`
	Stats       RuleHitStat      `json:"stats"`
	RecentHits  []RiskAssessment `json:"recentHits"`
	Series      []RuleHitPoint   `json:"series"`
	Explanation string           `json:"explanation"`
}

// RuleHitPoint 规则命中的时间序列点
type RuleHitPoint struct {
	Time  time.Time `json:"time"`
	Count int64     `json:"count"`
}

// IPRiskDetail IP 详情
type IPRiskDetail struct {
	Record   IPRiskRecord        `json:"record"`
	Summary  RiskEntitySummary   `json:"summary"`
	Recent   []RiskAssessment    `json:"recent"`
	Devices  []DeviceFingerprint `json:"devices"`
	Accounts []string            `json:"accounts"`
}

// DeviceRiskDetail 设备详情
type DeviceRiskDetail struct {
	Device   DeviceFingerprint `json:"device"`
	Summary  RiskEntitySummary `json:"summary"`
	Recent   []RiskAssessment  `json:"recent"`
	IPs      []string          `json:"ips"`
	Accounts []string          `json:"accounts"`
}

// ════════════════════════════════════════════════════════════
//  统计大盘
// ════════════════════════════════════════════════════════════

// RiskDashboard 风控大盘统计
type RiskDashboard struct {
	Range              DashboardRange    `json:"range"`
	Summary            RiskSummary       `json:"summary"`
	Series             []RiskSeriesPoint `json:"series"`
	SceneDistribution  []SceneStat       `json:"sceneDistribution"`
	LevelDistribution  []LevelStat       `json:"levelDistribution"`
	ActionDistribution []ActionStat      `json:"actionDistribution"`
	ScoreHistogram     []ScoreBucket     `json:"scoreHistogram"`
	TopRules           []RuleHitStat     `json:"topRules"`
	TopIPs             []IPHitStat       `json:"topIps"`
	TopDevices         []DeviceHitStat   `json:"topDevices"`
	TopCountries       []CountryStat     `json:"topCountries"`
	Engine             EngineStatus      `json:"engine"`
}

// DashboardRange 大盘的查询区间与聚合粒度
type DashboardRange struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Bucket string    `json:"bucket"` // hour / day
}

// RiskSummary 大盘概要。每个计数都带上一周期同区间的对照值，
// 只给绝对数看不出「今天是不是不对劲」。
type RiskSummary struct {
	TotalAssessments int64   `json:"totalAssessments"`
	TotalBlocked     int64   `json:"totalBlocked"`
	TotalChallenged  int64   `json:"totalChallenged"`
	TotalReviews     int64   `json:"totalReviews"`
	PendingReviews   int64   `json:"pendingReviews"`
	TotalPassed      int64   `json:"totalPassed"`
	HighRiskCount    int64   `json:"highRiskCount"`
	BlockRate        float64 `json:"blockRate"`
	AvgScore         float64 `json:"avgScore"`
	MaxScore         int     `json:"maxScore"`
	AvgLatencyMS     float64 `json:"avgLatencyMs"`
	DistinctIPs      int64   `json:"distinctIps"`
	DistinctDevices  int64   `json:"distinctDevices"`
	DistinctAccounts int64   `json:"distinctAccounts"`

	PrevTotalAssessments int64   `json:"prevTotalAssessments"`
	PrevTotalBlocked     int64   `json:"prevTotalBlocked"`
	PrevBlockRate        float64 `json:"prevBlockRate"`
	PrevAvgScore         float64 `json:"prevAvgScore"`
}

// RiskSeriesPoint 时间序列上的一个桶。同时给出等级与动作两组切分：
// 等级回答「风险在不在上升」，动作回答「拦了多少」，两者不能互相推导。
type RiskSeriesPoint struct {
	Time      time.Time `json:"time"`
	Total     int64     `json:"total"`
	Normal    int64     `json:"normal"`
	Low       int64     `json:"low"`
	Medium    int64     `json:"medium"`
	High      int64     `json:"high"`
	Critical  int64     `json:"critical"`
	Pass      int64     `json:"pass"`
	Captcha   int64     `json:"captcha"`
	Review    int64     `json:"review"`
	Block     int64     `json:"block"`
	Ban       int64     `json:"ban"`
	AvgScore  float64   `json:"avgScore"`
	BlockRate float64   `json:"blockRate"`
}

// ScoreBucket 分数直方图的一格
type ScoreBucket struct {
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Level string `json:"level"`
	Count int64  `json:"count"`
}

type SceneStat struct {
	Scene    string  `json:"scene"`
	Count    int64   `json:"count"`
	Blocked  int64   `json:"blocked"`
	AvgScore float64 `json:"avgScore"`
}

type LevelStat struct {
	Level string `json:"level"`
	Count int64  `json:"count"`
}

type ActionStat struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

type CountryStat struct {
	Country string `json:"country"`
	Count   int64  `json:"count"`
	Blocked int64  `json:"blocked"`
}

// RuleHitStat 规则命中统计
type RuleHitStat struct {
	RuleID        int64      `json:"ruleId"`
	RuleName      string     `json:"ruleName"`
	Scene         string     `json:"scene"`
	ConditionType string     `json:"conditionType"`
	Score         int        `json:"score"`
	IsActive      bool       `json:"isActive"`
	Hits          int64      `json:"hits"`
	Blocked       int64      `json:"blocked"`
	ScoreSum      int64      `json:"scoreSum"`
	LastHitAt     *time.Time `json:"lastHitAt,omitempty"`
}

// IPHitStat 高频 IP 统计
type IPHitStat struct {
	IP       string  `json:"ip"`
	Country  string  `json:"country"`
	Count    int64   `json:"count"`
	Blocked  int64   `json:"blocked"`
	MaxScore int     `json:"maxScore"`
	AvgScore float64 `json:"avgScore"`
	RiskTag  string  `json:"riskTag"`
}

// DeviceHitStat 高频设备统计
type DeviceHitStat struct {
	DeviceID string  `json:"deviceId"`
	Count    int64   `json:"count"`
	Blocked  int64   `json:"blocked"`
	MaxScore int     `json:"maxScore"`
	AvgScore float64 `json:"avgScore"`
	RiskTag  string  `json:"riskTag"`
	Accounts int64   `json:"accounts"`
}

// EngineStatus 引擎运行态：规则装载情况与外部情报源健康度。
// 「大盘全是 0」有两种截然不同的原因 —— 真没风险，或者根本没规则在跑。
// 不把这件事显式说出来，管理员会把后者当成前者。
type EngineStatus struct {
	TotalRules      int64    `json:"totalRules"`
	ActiveRules     int64    `json:"activeRules"`
	TotalActions    int64    `json:"totalActions"`
	ActiveActions   int64    `json:"activeActions"`
	ScenesCovered   []string `json:"scenesCovered"`
	ScenesUncovered []string `json:"scenesUncovered"`
	IPProvider      string   `json:"ipProvider"`
	IPProviderReady bool     `json:"ipProviderReady"`
	RateLimitOn     bool     `json:"rateLimitOn"`
	CacheTTLSeconds int      `json:"cacheTtlSeconds"`
}

// ════════════════════════════════════════════════════════════
//  自描述目录（/risk/metadata）
// ════════════════════════════════════════════════════════════

// RiskMetadata 风控中心的机器可读目录。
// 控制台的场景下拉、条件类型表单、表达式变量提示全部由它驱动 ——
// 后端新增一种条件类型时前端零改动即自动出现。
type RiskMetadata struct {
	Scenes         []CatalogEntry     `json:"scenes"`
	Levels         []LevelCatalog     `json:"levels"`
	Actions        []CatalogEntry     `json:"actions"`
	ConditionTypes []ConditionCatalog `json:"conditionTypes"`
	Variables      []VariableCatalog  `json:"variables"`
	Functions      []FunctionCatalog  `json:"functions"`
	DeviceTags     []CatalogEntry     `json:"deviceTags"`
	IPTags         []CatalogEntry     `json:"ipTags"`
	Samples        []ExprSample       `json:"samples"`
}

// CatalogEntry 通用的「值 + 标签 + 说明」目录项
type CatalogEntry struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Tone        string `json:"tone,omitempty"` // 前端着色提示：neutral/info/warning/danger/success
}

// LevelCatalog 等级目录，附带分数区间供图例使用
type LevelCatalog struct {
	CatalogEntry
	MinScore int  `json:"minScore"`
	MaxScore *int `json:"maxScore,omitempty"`
}

// ConditionCatalog 一种条件类型的完整自述：说明 + 参数 schema。
// 前端据此渲染参数表单，不再把每种条件的字段硬编码回组件里。
type ConditionCatalog struct {
	Value       string        `json:"value"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Group       string        `json:"group"`
	Fields      []FieldSchema `json:"fields"`
	// RequiresProvider 标记该条件依赖外部 IP 情报源；未配置时前端要给出提示，
	// 否则会出现「规则配好了但永远不命中」而无人知道为什么。
	RequiresProvider bool `json:"requiresProvider,omitempty"`
	RequiresRedis    bool `json:"requiresRedis,omitempty"`
}

// FieldSchema 参数字段 schema
type FieldSchema struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Type        string         `json:"type"` // number / text / textarea / select / list / bool / time
	Required    bool           `json:"required,omitempty"`
	Default     any            `json:"default,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Help        string         `json:"help,omitempty"`
	Min         *float64       `json:"min,omitempty"`
	Max         *float64       `json:"max,omitempty"`
	Options     []CatalogEntry `json:"options,omitempty"`
}

// VariableCatalog 表达式可用变量
type VariableCatalog struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
}

// FunctionCatalog 表达式可用函数
type FunctionCatalog struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example,omitempty"`
}

// ExprSample 表达式示例
type ExprSample struct {
	Title      string `json:"title"`
	Expression string `json:"expression"`
	Note       string `json:"note,omitempty"`
}

// ExprValidation 表达式校验结果
type ExprValidation struct {
	Valid   bool   `json:"valid"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// ════════════════════════════════════════════════════════════
//  输入类型
// ════════════════════════════════════════════════════════════

type CreateRiskRuleInput struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Scene         string         `json:"scene"`
	ConditionType string         `json:"conditionType"`
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
	Priority      int            `json:"priority"`
	IsActive      *bool          `json:"isActive,omitempty"`
}

type UpdateRiskRuleInput struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Scene         *string         `json:"scene,omitempty"`
	ConditionType *string         `json:"conditionType,omitempty"`
	ConditionData *map[string]any `json:"conditionData,omitempty"`
	Score         *int            `json:"score,omitempty"`
	IsActive      *bool           `json:"isActive,omitempty"`
	Priority      *int            `json:"priority,omitempty"`
	UpdatedBy     *int64          `json:"-"`
}

type CreateRiskActionInput struct {
	Scene       string `json:"scene"`
	MinScore    int    `json:"minScore"`
	MaxScore    *int   `json:"maxScore,omitempty"`
	Action      string `json:"action"`
	BanDuration int    `json:"banDuration"`
	Description string `json:"description"`
}

type UpdateRiskActionInput struct {
	MinScore    *int    `json:"minScore,omitempty"`
	MaxScore    **int   `json:"maxScore,omitempty"`
	Action      *string `json:"action,omitempty"`
	BanDuration *int    `json:"banDuration,omitempty"`
	Description *string `json:"description,omitempty"`
	IsActive    *bool   `json:"isActive,omitempty"`
}

type ReviewInput struct {
	Result  string `json:"result"` // approved / rejected
	Comment string `json:"comment"`
}

// SimulateInput 模拟评估输入。除请求维度外允许直接覆写环境变量（Overrides），
// 否则「IP 情报里没有的组合」根本没法在控制台上试出来。
type SimulateInput struct {
	Scene     string         `json:"scene"`
	IP        string         `json:"ip"`
	DeviceID  string         `json:"deviceId"`
	UserAgent string         `json:"userAgent"`
	Account   string         `json:"account"`
	AppID     *int64         `json:"appId,omitempty"`
	RuleIDs   []int64        `json:"ruleIds,omitempty"`
	Draft     *RiskRule      `json:"draft,omitempty"`
	Overrides map[string]any `json:"overrides,omitempty"`
}

// AssessmentQuery 评估记录查询条件
type AssessmentQuery struct {
	Scene     string
	RiskLevel string
	Action    string
	IP        string
	DeviceID  string
	Account   string
	Keyword   string
	RuleID    int64
	Reviewed  *bool
	MinScore  *int
	MaxScore  *int
	Start     *time.Time
	End       *time.Time
	Page      int
	PageSize  int
}

// EntityQuery 设备 / IP 列表查询条件
type EntityQuery struct {
	Keyword  string
	Tag      string
	OnlyRisk bool
	Page     int
	PageSize int
}
