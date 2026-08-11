package httptransport

import securitydomain "aegis/internal/domain/security"

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
	IsActive      *bool          `json:"isActive,omitempty"`
}

// RiskRuleUpdateRequest 更新风险规则
type RiskRuleUpdateRequest struct {
	Name          *string         `json:"name,omitempty"`
	Description   *string         `json:"description,omitempty"`
	Scene         *string         `json:"scene,omitempty"`
	ConditionType *string         `json:"conditionType,omitempty"`
	ConditionData *map[string]any `json:"conditionData,omitempty"`
	Score         *int            `json:"score,omitempty"`
	IsActive      *bool           `json:"isActive,omitempty"`
	Priority      *int            `json:"priority,omitempty"`
}

// RiskAssessmentListRequest 评估记录列表查询。
// 筛选维度必须够全：这张表是排查的起点，只能按场景/等级/动作三项过滤时，
// 「查一下这个 IP 最近都干了什么」只能靠人眼翻页。
type RiskAssessmentListRequest struct {
	Scene     string `form:"scene"`
	RiskLevel string `form:"riskLevel"`
	Action    string `form:"action"`
	IP        string `form:"ip"`
	DeviceID  string `form:"deviceId"`
	Account   string `form:"account"`
	Keyword   string `form:"keyword"`
	RuleID    int64  `form:"ruleId"`
	Reviewed  string `form:"reviewed"` // ""=全部 / "true" / "false"
	MinScore  *int   `form:"minScore"`
	MaxScore  *int   `form:"maxScore"`
	Start     string `form:"start"`
	End       string `form:"end"`
	Page      int    `form:"page"`
	PageSize  int    `form:"pageSize"`
}

// RiskEntityListRequest 设备 / IP 列表查询
type RiskEntityListRequest struct {
	Keyword  string `form:"keyword"`
	Tag      string `form:"tag"`
	OnlyRisk string `form:"onlyRisk"` // "true" 时只看非正常标签
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
}

// RiskReviewRequest 复核请求
type RiskReviewRequest struct {
	Result  string `json:"result" binding:"required,oneof=approved rejected"`
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

// RiskActionUpdateRequest 更新处置策略。
// 全部字段可选：旧版本只允许改启用状态，改错分数区间只能删掉重建。
type RiskActionUpdateRequest struct {
	MinScore    *int    `json:"minScore,omitempty"`
	MaxScore    *int    `json:"maxScore,omitempty"`
	Action      *string `json:"action,omitempty"`
	BanDuration *int    `json:"banDuration,omitempty"`
	Description *string `json:"description,omitempty"`
	IsActive    *bool   `json:"isActive,omitempty"`
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

// RiskSimulateRequest 模拟评估。
// Draft 让「还没保存的规则会不会命中」可以先试再存；
// Overrides 让「情报源里没有的组合」也能在控制台上构造出来。
type RiskSimulateRequest struct {
	Scene     string         `json:"scene"`
	IP        string         `json:"ip"`
	DeviceID  string         `json:"deviceId"`
	UserAgent string         `json:"userAgent"`
	Account   string         `json:"account"`
	AppID     *int64         `json:"appId,omitempty"`
	RuleIDs   []int64        `json:"ruleIds,omitempty"`
	Draft     *RiskRuleDraft `json:"draft,omitempty"`
	Overrides map[string]any `json:"overrides,omitempty"`
}

// RiskRuleDraft 未保存的规则草稿
type RiskRuleDraft struct {
	Name          string         `json:"name"`
	Scene         string         `json:"scene"`
	ConditionType string         `json:"conditionType" binding:"required"`
	ConditionData map[string]any `json:"conditionData"`
	Score         int            `json:"score"`
}

// ToDomain 把草稿转成领域规则
func (d *RiskRuleDraft) ToDomain(scene string) *securitydomain.RiskRule {
	if d == nil {
		return nil
	}
	data := d.ConditionData
	if data == nil {
		data = map[string]any{}
	}
	ruleScene := d.Scene
	if ruleScene == "" {
		ruleScene = scene
	}
	score := d.Score
	if score <= 0 {
		score = 20
	}
	return &securitydomain.RiskRule{
		Name:          d.Name,
		Scene:         ruleScene,
		ConditionType: d.ConditionType,
		ConditionData: data,
		Score:         score,
		IsActive:      true,
	}
}

// DeviceRiskTagRequest 更新设备风险标签
type DeviceRiskTagRequest struct {
	Tag  string `json:"tag" binding:"required"`
	Note string `json:"note"`
}

// IPRiskTagRequest 更新 IP 风险标签
type IPRiskTagRequest struct {
	Tag  string `json:"tag" binding:"required"`
	Note string `json:"note"`
}

// RiskDashboardRequest 大盘查询参数
type RiskDashboardRequest struct {
	Start string `form:"start"` // RFC3339
	End   string `form:"end"`   // RFC3339
}

// RiskPageRequest 通用分页参数
type RiskPageRequest struct {
	Page     int `form:"page"`
	PageSize int `form:"pageSize"`
}

// RiskExprValidateRequest 表达式校验
type RiskExprValidateRequest struct {
	Expression string `json:"expression" binding:"required"`
}

// RiskPurgeRequest 评估记录清理
type RiskPurgeRequest struct {
	Days int `json:"days" binding:"required,min=1"`
}
