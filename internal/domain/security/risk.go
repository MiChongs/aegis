package security

import "time"

// RiskRule 风险规则定义
type RiskRule struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Scene         string         `json:"scene"`         // register/login/payment/invite/lottery/api
	ConditionType string         `json:"conditionType"` // ip_frequency/geo_anomaly/device_new/ua_bot/ip_proxy/custom_expr
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
	IsActive      bool           `json:"isActive"`
	Priority      int            `json:"priority"`
	CreatedBy     *int64         `json:"createdBy,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// RiskAssessment 风险评估记录
type RiskAssessment struct {
	ID            int64          `json:"id"`
	Scene         string         `json:"scene"`
	AppID         *int64         `json:"appId,omitempty"`
	UserID        *int64         `json:"userId,omitempty"`
	IdentityID    *int64         `json:"identityId,omitempty"`
	IP            string         `json:"ip"`
	DeviceID      string         `json:"deviceId"`
	TotalScore    int            `json:"totalScore"`
	RiskLevel     string         `json:"riskLevel"` // normal/low/medium/high/critical
	MatchedRules  []MatchedRule  `json:"matchedRules"`
	Action        string         `json:"action"` // pass/captcha/review/block/ban
	ActionDetail  string         `json:"actionDetail"`
	Reviewed      bool           `json:"reviewed"`
	ReviewerID    *int64         `json:"reviewerId,omitempty"`
	ReviewResult  string         `json:"reviewResult,omitempty"` // approved/rejected
	ReviewComment string         `json:"reviewComment"`
	ReviewedAt    *time.Time     `json:"reviewedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

// MatchedRule 命中的规则
type MatchedRule struct {
	RuleID   int64  `json:"ruleId"`
	RuleName string `json:"ruleName"`
	Score    int    `json:"score"`
}

// DeviceFingerprint 设备指纹
type DeviceFingerprint struct {
	ID          int64          `json:"id"`
	DeviceID    string         `json:"deviceId"`
	UserID      *int64         `json:"userId,omitempty"`
	AppID       *int64         `json:"appId,omitempty"`
	Fingerprint map[string]any `json:"fingerprint"`
	RiskTag     string         `json:"riskTag"` // normal/suspicious/blocked
	FirstSeenAt time.Time      `json:"firstSeenAt"`
	LastSeenAt  time.Time      `json:"lastSeenAt"`
	SeenCount   int            `json:"seenCount"`
}

// IPRiskRecord IP 风险记录
type IPRiskRecord struct {
	ID            int64     `json:"id"`
	IP            string    `json:"ip"`
	RiskTag       string    `json:"riskTag"` // normal/proxy/vpn/tor/datacenter/bot
	RiskScore     int       `json:"riskScore"`
	Country       string    `json:"country"`
	Region        string    `json:"region"`
	ISP           string    `json:"isp"`
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
	Action      string    `json:"action"` // pass/captcha/review/block/ban
	BanDuration int       `json:"banDuration"`
	Description string    `json:"description"`
	IsActive    bool      `json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
}

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
	TotalScore   int           `json:"totalScore"`
	RiskLevel    string        `json:"riskLevel"`
	MatchedRules []MatchedRule `json:"matchedRules"`
	Action       string        `json:"action"`
	ActionDetail string        `json:"actionDetail"`
}

// RiskDashboard 风控大盘统计
type RiskDashboard struct {
	TotalAssessments int64            `json:"totalAssessments"`
	TotalBlocked     int64            `json:"totalBlocked"`
	TotalReviews     int64            `json:"totalReviews"`
	PendingReviews   int64            `json:"pendingReviews"`
	SceneDistribution []SceneStat     `json:"sceneDistribution"`
	LevelDistribution []LevelStat     `json:"levelDistribution"`
	ActionDistribution []ActionStat   `json:"actionDistribution"`
}

type SceneStat struct {
	Scene string `json:"scene"`
	Count int64  `json:"count"`
}

type LevelStat struct {
	Level string `json:"level"`
	Count int64  `json:"count"`
}

type ActionStat struct {
	Action string `json:"action"`
	Count  int64  `json:"count"`
}

// ── 输入类型 ──

type CreateRiskRuleInput struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Scene         string         `json:"scene"`
	ConditionType string         `json:"conditionType"`
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
	Priority      int            `json:"priority"`
}

type UpdateRiskRuleInput struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	ConditionData *map[string]any `json:"conditionData,omitempty"`
	Score         *int            `json:"score,omitempty"`
	IsActive      *bool           `json:"isActive,omitempty"`
	Priority      *int            `json:"priority,omitempty"`
}

type CreateRiskActionInput struct {
	Scene       string `json:"scene"`
	MinScore    int    `json:"minScore"`
	MaxScore    *int   `json:"maxScore,omitempty"`
	Action      string `json:"action"`
	BanDuration int    `json:"banDuration"`
	Description string `json:"description"`
}

type ReviewInput struct {
	Result  string `json:"result"` // approved/rejected
	Comment string `json:"comment"`
}

type SimulateInput struct {
	Scene     string `json:"scene"`
	IP        string `json:"ip"`
	DeviceID  string `json:"deviceId"`
	UserAgent string `json:"userAgent"`
}
