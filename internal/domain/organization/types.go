// Package organization 组织架构领域类型。
//
// 对外标识一律用 UUID：所有结构体的自增主键 `ID` 带 `json:"-"`，
// 而 `UUID` 序列化成 `id`。接入方与前端只认 UUID，自增 ID 不出网 ——
// 既避免枚举探测，也让「组织 ID」成为可跨系统引用的稳定标识。
package organization

import "time"

// ── 组织 ──

// Contact 组织联系人
type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

// Quota 组织配额，0 表示不限
type Quota struct {
	MemberLimit int `json:"memberLimit"`
	DeptLimit   int `json:"deptLimit"`
	AppLimit    int `json:"appLimit"`
}

// Stats 组织规模统计
type Stats struct {
	MemberCount int `json:"memberCount"`
	DeptCount   int `json:"deptCount"`
	AppCount    int `json:"appCount"`
	ChildCount  int `json:"childCount"`
}

// Organization 组织
type Organization struct {
	ID           int64          `json:"-"`
	UUID         string         `json:"id"`
	ParentID     *int64         `json:"-"`
	ParentUUID   string         `json:"parentId,omitempty"`
	ParentName   string         `json:"parentName,omitempty"`
	Path         string         `json:"-"`
	Depth        int            `json:"depth"`
	Name         string         `json:"name"`
	Code         string         `json:"code"`
	Kind         string         `json:"kind"`
	Description  string         `json:"description"`
	LogoURL      string         `json:"logoURL"`
	Status       string         `json:"status"`
	OwnerID      *int64         `json:"ownerId,omitempty"`
	OwnerName    string         `json:"ownerName,omitempty"`
	OwnerAccount string         `json:"ownerAccount,omitempty"`
	Contact      Contact        `json:"contact"`
	Industry     string         `json:"industry"`
	Region       string         `json:"region"`
	Quota        Quota          `json:"quota"`
	ExpiresAt    *time.Time     `json:"expiresAt,omitempty"`
	Settings     map[string]any `json:"settings,omitempty"`
	Stats        Stats          `json:"stats"`
	CreatedBy    *int64         `json:"createdBy,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`

	// ViewerRole 当前查看者在该组织中的角色，空串表示非成员（超管跨组织查看时为空）
	ViewerRole string `json:"viewerRole,omitempty"`
}

// OrganizationNode 组织层级树节点
type OrganizationNode struct {
	Organization
	Children []OrganizationNode `json:"children"`
}

// ── 部门 ──

// Department 部门
type Department struct {
	ID          int64          `json:"-"`
	UUID        string         `json:"id"`
	OrgID       int64          `json:"-"`
	OrgUUID     string         `json:"orgId"`
	ParentID    *int64         `json:"-"`
	ParentUUID  string         `json:"parentId,omitempty"`
	Path        string         `json:"-"`
	Depth       int            `json:"depth"`
	Name        string         `json:"name"`
	Code        string         `json:"code"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	SortOrder   int            `json:"sortOrder"`
	LeaderID    *int64         `json:"leaderId,omitempty"`
	LeaderName  string         `json:"leaderName,omitempty"`
	Status      string         `json:"status"`
	MemberLimit int            `json:"memberLimit"`
	Settings    map[string]any `json:"settings,omitempty"`

	// MemberCount 本部门直属成员数；TotalMemberCount 含所有子部门（去重）
	MemberCount      int `json:"memberCount"`
	TotalMemberCount int `json:"totalMemberCount"`
	ChildCount       int `json:"childCount"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// DepartmentNode 部门树节点
type DepartmentNode struct {
	Department
	Children []DepartmentNode `json:"children"`
}

// DeptRef 部门轻引用，用在成员归属等处
type DeptRef struct {
	UUID     string `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"fullName,omitempty"` // 「技术中心 / 平台组」形式的路径名
	IsLeader bool   `json:"isLeader,omitempty"`
}

// ── 组织成员 ──

// Member 组织成员
type Member struct {
	ID          int64      `json:"-"`
	UUID        string     `json:"id"`
	OrgUUID     string     `json:"orgId"`
	AdminID     int64      `json:"adminId"`
	Account     string     `json:"account"`
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email"`
	Avatar      string     `json:"avatar"`
	OrgRole     string     `json:"orgRole"`
	PrimaryDept *DeptRef   `json:"primaryDept,omitempty"`
	Departments []DeptRef  `json:"departments"`
	EmployeeNo  string     `json:"employeeNo"`
	Title       string     `json:"title"`
	Status      string     `json:"status"`
	JoinedAt    time.Time  `json:"joinedAt"`
	LeftAt      *time.Time `json:"leftAt,omitempty"`
	InvitedBy   *int64     `json:"invitedBy,omitempty"`
	IsSuperAdmin bool      `json:"isSuperAdmin,omitempty"`
}

// DepartmentMember 部门内成员（含岗位 / 汇报线 / 代理）
type DepartmentMember struct {
	AdminID           int64      `json:"adminId"`
	Account           string     `json:"account"`
	DisplayName       string     `json:"displayName"`
	Avatar            string     `json:"avatar"`
	OrgRole           string     `json:"orgRole"`
	IsLeader          bool       `json:"isLeader"`
	JoinedAt          time.Time  `json:"joinedAt"`
	PositionUUID      string     `json:"positionId,omitempty"`
	PositionName      string     `json:"positionName,omitempty"`
	JobTitle          string     `json:"jobTitle,omitempty"`
	ReportingTo       *int64     `json:"reportingTo,omitempty"`
	ReportingName     string     `json:"reportingName,omitempty"`
	DelegateTo        *int64     `json:"delegateTo,omitempty"`
	DelegateName      string     `json:"delegateName,omitempty"`
	DelegateExpiresAt *time.Time `json:"delegateExpiresAt,omitempty"`
}

// ── 岗位 ──

// Position 岗位
type Position struct {
	ID          int64     `json:"-"`
	UUID        string    `json:"id"`
	OrgUUID     string    `json:"orgId"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Level       int       `json:"level"`
	Description string    `json:"description"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ── 组织角色 ──

// Role 组织自定义 / 内置角色
type Role struct {
	ID          int64     `json:"-"`
	UUID        string    `json:"id"`
	OrgUUID     string    `json:"orgId"`
	RoleKey     string    `json:"roleKey"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	IsBuiltin   bool      `json:"isBuiltin"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RoleGrant 组织角色授予记录
type RoleGrant struct {
	RoleUUID    string    `json:"roleId"`
	RoleName    string    `json:"roleName"`
	AdminID     int64     `json:"adminId"`
	Account     string    `json:"account"`
	DisplayName string    `json:"displayName"`
	ScopeDept   *DeptRef  `json:"scopeDept,omitempty"`
	GrantedBy   *int64    `json:"grantedBy,omitempty"`
	GrantedAt   time.Time `json:"grantedAt"`
}

// ── 汇报链 ──

// ReportingNode 汇报链节点
type ReportingNode struct {
	AdminID     int64  `json:"adminId"`
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	Avatar      string `json:"avatar"`
	JobTitle    string `json:"jobTitle"`
	Depth       int    `json:"depth"`
}

// ── 邀请 ──

// Invitation 组织 / 部门邀请
type Invitation struct {
	ID           int64      `json:"-"`
	UUID         string     `json:"id"`
	OrgUUID      string     `json:"orgId"`
	OrgName      string     `json:"orgName"`
	DeptUUID     string     `json:"deptId,omitempty"`
	DeptName     string     `json:"deptName,omitempty"`
	InviterID    int64      `json:"inviterId"`
	InviterName  string     `json:"inviterName"`
	InviteeID    int64      `json:"inviteeId"`
	InviteeName  string     `json:"inviteeName"`
	OrgRole      string     `json:"orgRole"`
	IsLeader     bool       `json:"isLeader"`
	Status       string     `json:"status"` // pending / accepted / rejected / expired / cancelled
	Message      string     `json:"message"`
	RespondedAt  *time.Time `json:"respondedAt,omitempty"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// ── 活动留痕 ──

// ActivityLog 组织操作日志
type ActivityLog struct {
	ID         int64          `json:"-"`
	OrgUUID    string         `json:"orgId"`
	ActorID    *int64         `json:"actorId,omitempty"`
	ActorName  string         `json:"actorName,omitempty"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType,omitempty"`
	TargetID   string         `json:"targetId,omitempty"`
	Summary    string         `json:"summary"`
	Detail     map[string]any `json:"detail,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ── 应用绑定 ──

// AppBinding 组织↔应用绑定。Owned 为 true 表示应用归该组织所有，
// 否则只是被授权访问（跨组织共享）。
type AppBinding struct {
	OrgUUID   string    `json:"orgId"`
	AppID     int64     `json:"appId"`
	AppName   string    `json:"appName"`
	AppKey    string    `json:"appKey,omitempty"`
	Owned     bool      `json:"owned"`
	CreatedAt time.Time `json:"createdAt"`
}

// ── 分页 ──

// Page 通用分页信封
type Page[T any] struct {
	Items      []T   `json:"items"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}
