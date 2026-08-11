package organization

import "time"

// ── 组织 ──

// CreateOrgInput 创建组织
type CreateOrgInput struct {
	Name        string         `json:"name"`
	Code        string         `json:"code"`
	Kind        string         `json:"kind"`
	ParentUUID  string         `json:"parentId,omitempty"`
	Description string         `json:"description"`
	LogoURL     string         `json:"logoURL"`
	Contact     Contact        `json:"contact"`
	Industry    string         `json:"industry"`
	Region      string         `json:"region"`
	Quota       Quota          `json:"quota"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
	Settings    map[string]any `json:"settings,omitempty"`
	// OwnerAdminID 组织所有者；留空则取创建者本人
	OwnerAdminID int64 `json:"ownerAdminId,omitempty"`
}

// UpdateOrgInput 更新组织，nil 字段表示不修改
type UpdateOrgInput struct {
	Name        *string        `json:"name,omitempty"`
	Code        *string        `json:"code,omitempty"`
	Kind        *string        `json:"kind,omitempty"`
	Description *string        `json:"description,omitempty"`
	LogoURL     *string        `json:"logoURL,omitempty"`
	Status      *string        `json:"status,omitempty"`
	Contact     *Contact       `json:"contact,omitempty"`
	Industry    *string        `json:"industry,omitempty"`
	Region      *string        `json:"region,omitempty"`
	Quota       *Quota         `json:"quota,omitempty"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
	ClearExpiry bool           `json:"clearExpiry,omitempty"`
	Settings    map[string]any `json:"settings,omitempty"`
}

// OrgListQuery 组织列表查询
type OrgListQuery struct {
	Keyword    string
	Status     string
	Kind       string
	ParentUUID string
	// OnlyMine 为 true 时只返回调用者所属的组织（超管默认看全部）
	OnlyMine bool
	Page     int
	Limit    int
}

// ── 部门 ──

// CreateDeptInput 创建部门
type CreateDeptInput struct {
	ParentUUID  string         `json:"parentId,omitempty"`
	Name        string         `json:"name"`
	Code        string         `json:"code"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	SortOrder   int            `json:"sortOrder"`
	LeaderAdmin int64          `json:"leaderId,omitempty"`
	MemberLimit int            `json:"memberLimit"`
	Settings    map[string]any `json:"settings,omitempty"`
}

// UpdateDeptInput 更新部门
type UpdateDeptInput struct {
	Name        *string        `json:"name,omitempty"`
	Code        *string        `json:"code,omitempty"`
	Kind        *string        `json:"kind,omitempty"`
	Description *string        `json:"description,omitempty"`
	SortOrder   *int           `json:"sortOrder,omitempty"`
	LeaderAdmin *int64         `json:"leaderId,omitempty"`
	ClearLeader bool           `json:"clearLeader,omitempty"`
	Status      *string        `json:"status,omitempty"`
	MemberLimit *int           `json:"memberLimit,omitempty"`
	Settings    map[string]any `json:"settings,omitempty"`
}

// MoveDeptInput 移动部门。ParentUUID 为空表示移动到组织根。
type MoveDeptInput struct {
	ParentUUID string `json:"parentId"`
	SortOrder  *int   `json:"sortOrder,omitempty"`
}

// DeleteDeptStrategy 删除非空部门时对子部门与成员的处置策略
type DeleteDeptStrategy string

const (
	// DeleteRestrict 有子部门或成员时拒绝删除（默认，最安全）
	DeleteRestrict DeleteDeptStrategy = "restrict"
	// DeleteCascade 连同整棵子树一并删除，成员退回组织但保留组织籍
	DeleteCascade DeleteDeptStrategy = "cascade"
	// DeleteReparent 子部门上移一层挂到被删部门的父节点
	DeleteReparent DeleteDeptStrategy = "reparent"
)

// ── 成员 ──

// AddMemberInput 直接把管理员加入组织（超管 / 组织管理员用，跳过邀请）
type AddMemberInput struct {
	AdminID     int64    `json:"adminId"`
	OrgRole     string   `json:"orgRole"`
	EmployeeNo  string   `json:"employeeNo"`
	Title       string   `json:"title"`
	DeptUUIDs   []string `json:"deptIds,omitempty"`
	PrimaryDept string   `json:"primaryDeptId,omitempty"`
}

// UpdateMemberInput 更新组织成员档案
type UpdateMemberInput struct {
	OrgRole     *string `json:"orgRole,omitempty"`
	EmployeeNo  *string `json:"employeeNo,omitempty"`
	Title       *string `json:"title,omitempty"`
	Status      *string `json:"status,omitempty"`
	PrimaryDept *string `json:"primaryDeptId,omitempty"`
}

// MemberListQuery 组织成员查询
type MemberListQuery struct {
	Keyword string
	OrgRole string
	Status  string
	// DeptUUID 限定部门；IncludeSubDepts 为 true 时含子部门成员
	DeptUUID       string
	IncludeSubDept bool
	// Unassigned 为 true 时只看尚未加入任何部门的成员
	Unassigned bool
	Page       int
	Limit      int
}

// AssignDeptInput 调整成员的部门归属
type AssignDeptInput struct {
	DeptUUIDs   []string `json:"deptIds"`
	PrimaryDept string   `json:"primaryDeptId,omitempty"`
	// Replace 为 true 时以 DeptUUIDs 全量覆盖，否则增量追加
	Replace bool `json:"replace"`
}

// SetDeptMemberInput 设置部门内成员属性
type SetDeptMemberInput struct {
	IsLeader     *bool      `json:"isLeader,omitempty"`
	PositionUUID *string    `json:"positionId,omitempty"`
	JobTitle     *string    `json:"jobTitle,omitempty"`
	ReportingTo  *int64     `json:"reportingTo,omitempty"`
	ClearReport  bool       `json:"clearReporting,omitempty"`
	DelegateTo   *int64     `json:"delegateTo,omitempty"`
	DelegateTill *time.Time `json:"delegateExpiresAt,omitempty"`
	ClearDeleg   bool       `json:"clearDelegate,omitempty"`
}

// ── 岗位 ──

// PositionInput 创建 / 更新岗位
type PositionInput struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Level       int    `json:"level"`
	Description string `json:"description"`
}

// ── 角色 ──

// RoleInput 创建 / 更新组织角色
type RoleInput struct {
	RoleKey     string   `json:"roleKey"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// GrantRoleInput 授予组织角色
type GrantRoleInput struct {
	AdminIDs      []int64 `json:"adminIds"`
	ScopeDeptUUID string  `json:"scopeDeptId,omitempty"`
}

// ── 邀请 ──

// InviteInput 邀请加入组织 / 部门
type InviteInput struct {
	AdminIDs []int64 `json:"adminIds"`
	DeptUUID string  `json:"deptId,omitempty"`
	OrgRole  string  `json:"orgRole"`
	IsLeader bool    `json:"isLeader"`
	Message  string  `json:"message"`
}

// InvitationQuery 邀请列表查询
type InvitationQuery struct {
	AdminID int64
	Role    string // sent / received
	Status  string
	OrgUUID string
	Page    int
	Limit   int
}

// ── 导入导出 ──

// ImportRow Excel 导入的一行
type ImportRow struct {
	RowNo      int    `json:"rowNo"`
	DeptPath   string `json:"deptPath"`   // 「技术中心/平台组」
	DeptCode   string `json:"deptCode"`
	Account    string `json:"account"`    // 管理员登录账号
	EmployeeNo string `json:"employeeNo"`
	Title      string `json:"title"`
	OrgRole    string `json:"orgRole"`
	Position   string `json:"position"` // 岗位代码
	IsLeader   bool   `json:"isLeader"`
	ReportTo   string `json:"reportTo"` // 上级账号
}

// ImportIssue 导入校验问题
type ImportIssue struct {
	RowNo   int    `json:"rowNo"`
	Field   string `json:"field"`
	Value   string `json:"value"`
	Message string `json:"message"`
	Fatal   bool   `json:"fatal"`
}

// ImportResult 导入结果
type ImportResult struct {
	DryRun       bool          `json:"dryRun"`
	TotalRows    int           `json:"totalRows"`
	DeptCreated  int           `json:"deptCreated"`
	MemberAdded  int           `json:"memberAdded"`
	MemberUpdate int           `json:"memberUpdated"`
	Skipped      int           `json:"skipped"`
	Issues       []ImportIssue `json:"issues"`
}

// ── 组织总览 ──

// Overview 组织概览：给控制台首屏一次取齐
type Overview struct {
	Organization Organization  `json:"organization"`
	Stats        OverviewStats `json:"stats"`
	RoleBreakdown map[string]int `json:"roleBreakdown"`
	TopDepts     []DeptRef     `json:"topDepartments"`
	RecentLogs   []ActivityLog `json:"recentActivity"`
}

// OverviewStats 概览统计
type OverviewStats struct {
	MemberTotal     int `json:"memberTotal"`
	MemberActive    int `json:"memberActive"`
	MemberSuspended int `json:"memberSuspended"`
	DeptTotal       int `json:"deptTotal"`
	DeptMaxDepth    int `json:"deptMaxDepth"`
	PositionTotal   int `json:"positionTotal"`
	AppTotal        int `json:"appTotal"`
	PendingInvites  int `json:"pendingInvites"`
	Unassigned      int `json:"unassignedMembers"`
	ChildOrgs       int `json:"childOrgs"`
}
