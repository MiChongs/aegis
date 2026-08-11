package httptransport

// 组织域 DTO。
//
// 所有对外的实体标识都是 UUID 字符串（orgId / deptId / positionId / roleId …），
// 只有 adminId 与 appId 仍是数字 —— 它们分属管理员与应用两个既有主键空间，
// 硬改成 UUID 会波及整个平台，收益也不在这次范围内。

// ── 组织 ──

// OrgListQueryParams 组织列表查询
type OrgListQueryParams struct {
	Keyword  string `form:"keyword"`
	Status   string `form:"status"`
	Kind     string `form:"kind"`
	ParentID string `form:"parentId"`
	OnlyMine bool   `form:"onlyMine"`
	Page     int    `form:"page"`
	Limit    int    `form:"limit"`
}

// CreateOrgRequest 创建组织
type CreateOrgRequest struct {
	Name         string         `json:"name" binding:"required"`
	Code         string         `json:"code" binding:"required"`
	Kind         string         `json:"kind"`
	ParentID     string         `json:"parentId"`
	Description  string         `json:"description"`
	LogoURL      string         `json:"logoURL"`
	ContactName  string         `json:"contactName"`
	ContactEmail string         `json:"contactEmail"`
	ContactPhone string         `json:"contactPhone"`
	Industry     string         `json:"industry"`
	Region       string         `json:"region"`
	MemberLimit  int            `json:"memberLimit"`
	DeptLimit    int            `json:"deptLimit"`
	AppLimit     int            `json:"appLimit"`
	ExpiresAt    string         `json:"expiresAt"`
	Settings     map[string]any `json:"settings"`
	OwnerAdminID int64          `json:"ownerAdminId"`
}

// UpdateOrgRequest 更新组织，nil 字段表示不修改
type UpdateOrgRequest struct {
	Name         *string        `json:"name"`
	Code         *string        `json:"code"`
	Kind         *string        `json:"kind"`
	Description  *string        `json:"description"`
	LogoURL      *string        `json:"logoURL"`
	Status       *string        `json:"status"`
	ContactName  *string        `json:"contactName"`
	ContactEmail *string        `json:"contactEmail"`
	ContactPhone *string        `json:"contactPhone"`
	Industry     *string        `json:"industry"`
	Region       *string        `json:"region"`
	MemberLimit  *int           `json:"memberLimit"`
	DeptLimit    *int           `json:"deptLimit"`
	AppLimit     *int           `json:"appLimit"`
	ExpiresAt    *string        `json:"expiresAt"`
	ClearExpiry  bool           `json:"clearExpiry"`
	Settings     map[string]any `json:"settings"`
}

// TransferOrgRequest 转让所有权
type TransferOrgRequest struct {
	NewOwnerAdminID int64 `json:"newOwnerAdminId" binding:"required"`
}

// ── 部门 ──

// CreateDeptRequest 创建部门
type CreateDeptRequest struct {
	ParentID      string         `json:"parentId"`
	Name          string         `json:"name" binding:"required"`
	Code          string         `json:"code" binding:"required"`
	Kind          string         `json:"kind"`
	Description   string         `json:"description"`
	SortOrder     int            `json:"sortOrder"`
	LeaderAdminID int64          `json:"leaderId"`
	MemberLimit   int            `json:"memberLimit"`
	Settings      map[string]any `json:"settings"`
}

// UpdateDeptRequest 更新部门
type UpdateDeptRequest struct {
	Name          *string        `json:"name"`
	Code          *string        `json:"code"`
	Kind          *string        `json:"kind"`
	Description   *string        `json:"description"`
	SortOrder     *int           `json:"sortOrder"`
	LeaderAdminID *int64         `json:"leaderId"`
	ClearLeader   bool           `json:"clearLeader"`
	Status        *string        `json:"status"`
	MemberLimit   *int           `json:"memberLimit"`
	Settings      map[string]any `json:"settings"`
}

// MoveDeptRequest 移动部门。parentId 留空表示移到组织根。
type MoveDeptRequest struct {
	ParentID  string `json:"parentId"`
	SortOrder *int   `json:"sortOrder"`
}

// ReorderDeptRequest 批量排序：部门 UUID → 序号
type ReorderDeptRequest struct {
	Order map[string]int `json:"order" binding:"required"`
}

// ── 成员 ──

// MemberListQueryParams 成员列表查询
type MemberListQueryParams struct {
	Keyword         string `form:"keyword"`
	OrgRole         string `form:"orgRole"`
	Status          string `form:"status"`
	DeptID          string `form:"deptId"`
	IncludeSubDepts bool   `form:"includeSubDepts"`
	Unassigned      bool   `form:"unassigned"`
	Page            int    `form:"page"`
	Limit           int    `form:"limit"`
}

// AddOrgMemberRequest 直接加入组织
type AddOrgMemberRequest struct {
	AdminID       int64    `json:"adminId" binding:"required"`
	OrgRole       string   `json:"orgRole"`
	EmployeeNo    string   `json:"employeeNo"`
	Title         string   `json:"title"`
	DeptIDs       []string `json:"deptIds"`
	PrimaryDeptID string   `json:"primaryDeptId"`
}

// UpdateOrgMemberRequest 更新成员档案
type UpdateOrgMemberRequest struct {
	OrgRole       *string `json:"orgRole"`
	EmployeeNo    *string `json:"employeeNo"`
	Title         *string `json:"title"`
	Status        *string `json:"status"`
	PrimaryDeptID *string `json:"primaryDeptId"`
}

// AssignDeptRequest 调整部门归属
type AssignDeptRequest struct {
	DeptIDs       []string `json:"deptIds"`
	PrimaryDeptID string   `json:"primaryDeptId"`
	Replace       bool     `json:"replace"`
}

// SetDeptMemberRequest 设置部门内成员属性
type SetDeptMemberRequest struct {
	IsLeader          *bool   `json:"isLeader"`
	PositionID        *string `json:"positionId"`
	JobTitle          *string `json:"jobTitle"`
	ReportingTo       *int64  `json:"reportingTo"`
	ClearReporting    bool    `json:"clearReporting"`
	DelegateTo        *int64  `json:"delegateTo"`
	DelegateExpiresAt *string `json:"delegateExpiresAt"`
	ClearDelegate     bool    `json:"clearDelegate"`
}

// ── 邀请 ──

// InviteMembersRequest 发起邀请
type InviteMembersRequest struct {
	AdminIDs []int64 `json:"adminIds" binding:"required"`
	DeptID   string  `json:"deptId"`
	OrgRole  string  `json:"orgRole"`
	IsLeader bool    `json:"isLeader"`
	Message  string  `json:"message"`
}

// InvitationListQueryParams 邀请列表查询
type InvitationListQueryParams struct {
	Role   string `form:"role"`
	Status string `form:"status"`
	OrgID  string `form:"orgId"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// ── 岗位 ──

// PositionRequest 创建 / 更新岗位
type PositionRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Level       int    `json:"level"`
	Description string `json:"description"`
}

// ── 组织角色 ──

// OrgRoleRequest 创建 / 更新组织角色
type OrgRoleRequest struct {
	RoleKey     string   `json:"roleKey" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions" binding:"required"`
}

// GrantOrgRoleRequest 授予组织角色
type GrantOrgRoleRequest struct {
	AdminIDs    []int64 `json:"adminIds" binding:"required"`
	ScopeDeptID string  `json:"scopeDeptId"`
}

// ── 应用绑定 ──

// BindOrgAppRequest 绑定应用。owned 为 true 表示转移归属（仅平台管理员）。
type BindOrgAppRequest struct {
	AppID int64 `json:"appId" binding:"required"`
	Owned bool  `json:"owned"`
}

// ── 审批 ──

// ApprovalStepPayload 审批步骤
type ApprovalStepPayload struct {
	ApproverType string `json:"approverType" binding:"required"`
	ApproverID   int64  `json:"approverId"`
	ApproverRole string `json:"approverRole"`
	Name         string `json:"name"`
	Order        int    `json:"order"`
}

// ApprovalChainRequest 创建审批链
type ApprovalChainRequest struct {
	Name        string                `json:"name" binding:"required"`
	TriggerType string                `json:"triggerType" binding:"required"`
	Steps       []ApprovalStepPayload `json:"steps" binding:"required"`
	IsActive    *bool                 `json:"isActive"`
}

// UpdateApprovalChainRequest 更新审批链
type UpdateApprovalChainRequest struct {
	Name     *string                `json:"name"`
	Steps    *[]ApprovalStepPayload `json:"steps"`
	IsActive *bool                  `json:"isActive"`
}

// ApprovalDecisionRequest 审批决定
type ApprovalDecisionRequest struct {
	Action  string `json:"action" binding:"required"` // approved / rejected
	Comment string `json:"comment"`
}

// ── 权限模板 ──

// PermTemplateRequest 创建权限模板
type PermTemplateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions" binding:"required"`
	IsDefault   bool     `json:"isDefault"`
}

// ApplyTemplateRequest 套用权限模板
type ApplyTemplateRequest struct {
	AdminIDs    []int64 `json:"adminIds" binding:"required"`
	ScopeDeptID string  `json:"scopeDeptId"`
	RoleName    string  `json:"roleName"`
}

// ── 协作组 ──

// CollabGroupRequest 创建 / 更新协作组
type CollabGroupRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	DeptIDs     []string `json:"deptIds"`
	Permissions []string `json:"permissions"`
}
