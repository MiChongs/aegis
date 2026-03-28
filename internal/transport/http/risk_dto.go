package httptransport

// ════════════════════════════════════════════════════════════
//  风控中心 DTO
// ════════════════════════════════════════════════════════════

// RiskRuleCreateRequest 创建风险规则
type RiskRuleCreateRequest struct {
	Name          string         `json:"name" binding:"required"`
	Description   string         `json:"description"`
	Scene         string         `json:"scene" binding:"required"`
	ConditionType string         `json:"conditionType" binding:"required"`
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
	Priority      int            `json:"priority"`
}

// RiskRuleUpdateRequest 更新风险规则
type RiskRuleUpdateRequest struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	ConditionData *map[string]any `json:"conditionData,omitempty"`
	Score         *int            `json:"score,omitempty"`
	IsActive      *bool           `json:"isActive,omitempty"`
	Priority      *int            `json:"priority,omitempty"`
}

// RiskAssessmentListRequest 评估记录列表查询
type RiskAssessmentListRequest struct {
	Scene     string `form:"scene"`
	RiskLevel string `form:"riskLevel"`
	Action    string `form:"action"`
	Page      int    `form:"page"`
	PageSize  int    `form:"pageSize"`
}

// RiskReviewRequest 复核请求
type RiskReviewRequest struct {
	Result  string `json:"result" binding:"required"` // approved / rejected
	Comment string `json:"comment"`
}

// RiskActionCreateRequest 创建处置策略
type RiskActionCreateRequest struct {
	Scene       string `json:"scene" binding:"required"`
	MinScore    int    `json:"minScore"`
	MaxScore    *int   `json:"maxScore,omitempty"`
	Action      string `json:"action" binding:"required"`
	BanDuration int    `json:"banDuration"`
	Description string `json:"description"`
}

// RiskActionUpdateRequest 更新处置策略
type RiskActionUpdateRequest struct {
	IsActive bool `json:"isActive"`
}

// RiskEvalRequest 手动触发风险评估
type RiskEvalRequest struct {
	Scene     string         `json:"scene" binding:"required"`
	IP        string         `json:"ip"`
	DeviceID  string         `json:"deviceId"`
	UserAgent string         `json:"userAgent"`
	AppID     *int64         `json:"appId,omitempty"`
	UserID    *int64         `json:"userId,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// RiskSimulateRequest 模拟评估
type RiskSimulateRequest struct {
	Scene     string `json:"scene" binding:"required"`
	IP        string `json:"ip"`
	DeviceID  string `json:"deviceId"`
	UserAgent string `json:"userAgent"`
}

// DeviceRiskTagRequest 更新设备风险标签
type DeviceRiskTagRequest struct {
	Tag string `json:"tag" binding:"required"`
}

// IPRiskTagRequest 更新 IP 风险标签
type IPRiskTagRequest struct {
	Tag string `json:"tag" binding:"required"`
}

// RiskDashboardRequest 大盘查询参数
type RiskDashboardRequest struct {
	Start string `form:"start"` // RFC3339
	End   string `form:"end"`   // RFC3339
}

// PageRequest 通用分页参数
type RiskPageRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"pageSize"`
}
