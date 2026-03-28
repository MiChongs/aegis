package system

import "time"

// ── 审批链 ──

// ApprovalChain 组织审批链配置
type ApprovalChain struct {
	ID          int64          `json:"id"`
	OrgID       int64          `json:"orgId"`
	Name        string         `json:"name"`
	TriggerType string         `json:"triggerType"` // member_join, member_leave, dept_create, role_change
	Steps       []ApprovalStep `json:"steps"`
	IsActive    bool           `json:"isActive"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// ApprovalStep 审批步骤
type ApprovalStep struct {
	ApproverType string `json:"approverType"` // leader, position, admin
	ApproverID   int64  `json:"approverId"`
	Order        int    `json:"order"`
}

// ApprovalInstance 审批实例
type ApprovalInstance struct {
	ID          int64          `json:"id"`
	ChainID     int64          `json:"chainId"`
	OrgID       int64          `json:"orgId"`
	TriggerType string         `json:"triggerType"`
	RequesterID int64          `json:"requesterId"`
	SubjectData map[string]any `json:"subjectData"`
	CurrentStep int            `json:"currentStep"`
	Status      string         `json:"status"` // pending, approved, rejected, cancelled
	StepsResult []StepResult   `json:"stepsResult"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// StepResult 审批步骤结果
type StepResult struct {
	Step       int       `json:"step"`
	ApproverID int64     `json:"approverId"`
	Action     string    `json:"action"` // approved, rejected
	Comment    string    `json:"comment"`
	At         time.Time `json:"at"`
}

// ── 权限模板 ──

// OrgPermissionTemplate 组织级权限模板
type OrgPermissionTemplate struct {
	ID          int64     `json:"id"`
	OrgID       int64     `json:"orgId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	IsDefault   bool      `json:"isDefault"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ── 资源绑定 ──

// OrgAppBinding 组织→应用绑定
type OrgAppBinding struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"orgId"`
	AppID     int64     `json:"appId"`
	CreatedAt time.Time `json:"createdAt"`
}

// ── 协作组 ──

// CollaborationGroup 跨部门协作组
type CollaborationGroup struct {
	ID          int64     `json:"id"`
	OrgID       int64     `json:"orgId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DeptIDs     []int64   `json:"deptIds"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ── 输入类型 ──

type CreateApprovalChainInput struct {
	Name        string         `json:"name"`
	TriggerType string         `json:"triggerType"`
	Steps       []ApprovalStep `json:"steps"`
}

type CreatePermTemplateInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	IsDefault   bool     `json:"isDefault"`
}

type CreateCollabGroupInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DeptIDs     []int64  `json:"deptIds"`
	Permissions []string `json:"permissions"`
}
