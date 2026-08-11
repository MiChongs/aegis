package organization

import "time"

// ── 审批链 ──

// 审批触发场景
const (
	TriggerMemberJoin  = "member_join"
	TriggerMemberLeave = "member_leave"
	TriggerDeptCreate  = "dept_create"
	TriggerDeptDelete  = "dept_delete"
	TriggerRoleChange  = "role_change"
	TriggerAppBind     = "app_bind"
)

// 审批人类型
const (
	ApproverLeader   = "leader"   // 申请人所在部门负责人
	ApproverPosition = "position" // 指定岗位的任意持有者
	ApproverAdmin    = "admin"    // 指定管理员本人
	ApproverOrgRole  = "org_role" // 组织角色（如所有 admin）
)

// ApprovalChain 组织审批链配置
type ApprovalChain struct {
	ID          int64          `json:"-"`
	UUID        string         `json:"id"`
	OrgUUID     string         `json:"orgId"`
	Name        string         `json:"name"`
	TriggerType string         `json:"triggerType"`
	Steps       []ApprovalStep `json:"steps"`
	IsActive    bool           `json:"isActive"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// ApprovalStep 审批步骤
type ApprovalStep struct {
	ApproverType string `json:"approverType"`
	// ApproverID 语义随 ApproverType 变化：admin → 管理员 ID；position → 岗位内部 ID；
	// leader → 忽略；org_role → 忽略（改用 ApproverRole）
	ApproverID   int64  `json:"approverId"`
	ApproverRole string `json:"approverRole,omitempty"`
	Name         string `json:"name,omitempty"`
	Order        int    `json:"order"`
}

// ApprovalInstance 审批实例
type ApprovalInstance struct {
	ID            int64          `json:"-"`
	UUID          string         `json:"id"`
	ChainUUID     string         `json:"chainId"`
	ChainName     string         `json:"chainName,omitempty"`
	OrgUUID       string         `json:"orgId"`
	TriggerType   string         `json:"triggerType"`
	RequesterID   int64          `json:"requesterId"`
	RequesterName string         `json:"requesterName,omitempty"`
	SubjectData   map[string]any `json:"subjectData"`
	CurrentStep   int            `json:"currentStep"`
	TotalSteps    int            `json:"totalSteps"`
	Status        string         `json:"status"`
	StepsResult   []StepResult   `json:"stepsResult"`
	// PendingApprovers 当前步骤解析出的实际审批人，供前端展示「卡在谁那里」
	PendingApprovers []ApproverRef `json:"pendingApprovers,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	UpdatedAt        time.Time     `json:"updatedAt"`
}

// ApproverRef 审批人引用
type ApproverRef struct {
	AdminID     int64  `json:"adminId"`
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
}

// StepResult 审批步骤结果
type StepResult struct {
	Step         int       `json:"step"`
	ApproverID   int64     `json:"approverId"`
	ApproverName string    `json:"approverName,omitempty"`
	Action       string    `json:"action"` // approved / rejected
	Comment      string    `json:"comment"`
	At           time.Time `json:"at"`
}

// CreateApprovalChainInput 创建审批链
type CreateApprovalChainInput struct {
	Name        string         `json:"name"`
	TriggerType string         `json:"triggerType"`
	Steps       []ApprovalStep `json:"steps"`
	IsActive    *bool          `json:"isActive,omitempty"`
}

// UpdateApprovalChainInput 更新审批链
type UpdateApprovalChainInput struct {
	Name     *string         `json:"name,omitempty"`
	Steps    *[]ApprovalStep `json:"steps,omitempty"`
	IsActive *bool           `json:"isActive,omitempty"`
}

// ── 权限模板 ──

// PermissionTemplate 组织级权限模板
type PermissionTemplate struct {
	ID          int64     `json:"-"`
	UUID        string    `json:"id"`
	OrgUUID     string    `json:"orgId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	IsDefault   bool      `json:"isDefault"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CreatePermTemplateInput 创建权限模板
type CreatePermTemplateInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	IsDefault   bool     `json:"isDefault"`
}

// ApplyTemplateInput 套用权限模板
type ApplyTemplateInput struct {
	AdminIDs      []int64 `json:"adminIds"`
	ScopeDeptUUID string  `json:"scopeDeptId,omitempty"`
	// RoleName 套用时创建的组织角色名；留空则用模板名
	RoleName string `json:"roleName,omitempty"`
}

// ── 协作组 ──

// CollaborationGroup 跨部门协作组
type CollaborationGroup struct {
	ID          int64     `json:"-"`
	UUID        string    `json:"id"`
	OrgUUID     string    `json:"orgId"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Depts       []DeptRef `json:"departments"`
	Permissions []string  `json:"permissions"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CollabGroupInput 创建 / 更新协作组
type CollabGroupInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DeptUUIDs   []string `json:"deptIds"`
	Permissions []string `json:"permissions"`
}
