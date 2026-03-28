package system

import "time"

// Organization 组织
type Organization struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	LogoURL     string    `json:"logoURL"`
	Status      string    `json:"status"`
	CreatedBy   *int64    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Department 部门
type Department struct {
	ID          int64     `json:"id"`
	OrgID       int64     `json:"orgId"`
	ParentID    *int64    `json:"parentId,omitempty"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Description string    `json:"description"`
	SortOrder   int       `json:"sortOrder"`
	LeaderID    *int64    `json:"leaderId,omitempty"`
	LeaderName  string    `json:"leaderName,omitempty"`
	Status      string    `json:"status"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// DepartmentTree 部门树节点
type DepartmentTree struct {
	Department
	Children []DepartmentTree `json:"children"`
}

// DepartmentMember 部门成员
type DepartmentMember struct {
	AdminID           int64      `json:"adminId"`
	Account           string     `json:"account"`
	DisplayName       string     `json:"displayName"`
	Avatar            string     `json:"avatar"`
	IsLeader          bool       `json:"isLeader"`
	JoinedAt          string     `json:"joinedAt"`
	PositionID        *int64     `json:"positionId,omitempty"`
	PositionName      string     `json:"positionName,omitempty"`
	JobTitle          string     `json:"jobTitle,omitempty"`
	ReportingTo       *int64     `json:"reportingTo,omitempty"`
	ReportingName     string     `json:"reportingName,omitempty"`
	DelegateTo        *int64     `json:"delegateTo,omitempty"`
	DelegateName      string     `json:"delegateName,omitempty"`
	DelegateExpiresAt *time.Time `json:"delegateExpiresAt,omitempty"`
}

// Position 岗位
type Position struct {
	ID          int64     `json:"id"`
	OrgID       int64     `json:"orgId"`
	Name        string    `json:"name"`
	Code        string    `json:"code"`
	Level       int       `json:"level"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CreatePositionInput 创建岗位请求
type CreatePositionInput struct {
	OrgID       int64  `json:"orgId"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Level       int    `json:"level"`
	Description string `json:"description"`
}

// ReportingChainNode 汇报链节点
type ReportingChainNode struct {
	AdminID     int64  `json:"adminId"`
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	JobTitle    string `json:"jobTitle"`
	Depth       int    `json:"depth"`
}

// CreateOrgInput 创建组织请求
type CreateOrgInput struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	LogoURL     string `json:"logoURL"`
}

// UpdateOrgInput 更新组织请求
type UpdateOrgInput struct {
	Name        *string `json:"name,omitempty"`
	Code        *string `json:"code,omitempty"`
	Description *string `json:"description,omitempty"`
	LogoURL     *string `json:"logoURL,omitempty"`
	Status      *string `json:"status,omitempty"`
}

// CreateDeptInput 创建部门请求
type CreateDeptInput struct {
	ParentID    *int64 `json:"parentId,omitempty"`
	Name        string `json:"name"`
	Code        string `json:"code"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
	LeaderID    *int64 `json:"leaderId,omitempty"`
}

// UpdateDeptInput 更新部门请求
type UpdateDeptInput struct {
	Name        *string `json:"name,omitempty"`
	Code        *string `json:"code,omitempty"`
	Description *string `json:"description,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	LeaderID    *int64  `json:"leaderId,omitempty"`
	Status      *string `json:"status,omitempty"`
}

// MoveDeptInput 移动部门请求
type MoveDeptInput struct {
	ParentID *int64 `json:"parentId"`
}

// BuildDepartmentTree 将平铺部门列表构建为树
func BuildDepartmentTree(departments []Department) []DepartmentTree {
	childMap := make(map[int64][]Department)
	var roots []Department
	for _, d := range departments {
		if d.ParentID == nil {
			roots = append(roots, d)
		} else {
			childMap[*d.ParentID] = append(childMap[*d.ParentID], d)
		}
	}
	return buildChildren(roots, childMap)
}

func buildChildren(items []Department, childMap map[int64][]Department) []DepartmentTree {
	result := make([]DepartmentTree, 0, len(items))
	for _, item := range items {
		node := DepartmentTree{Department: item}
		if children, ok := childMap[item.ID]; ok {
			node.Children = buildChildren(children, childMap)
		}
		if node.Children == nil {
			node.Children = []DepartmentTree{}
		}
		result = append(result, node)
	}
	return result
}

// ── 部门邀请 ──

// DeptInvitation 部门邀请
type DeptInvitation struct {
	ID           int64      `json:"id"`
	DepartmentID int64      `json:"departmentId"`
	InviterID    int64      `json:"inviterId"`
	InviteeID    int64      `json:"inviteeId"`
	IsLeader     bool       `json:"isLeader"`
	Status       string     `json:"status"` // pending / accepted / rejected / expired / cancelled
	Message      string     `json:"message"`
	DeptName     string     `json:"deptName"`
	OrgName      string     `json:"orgName"`
	InviterName  string     `json:"inviterName"`
	InviteeName  string     `json:"inviteeName"`
	RespondedAt  *time.Time `json:"respondedAt,omitempty"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// CreateInvitationInput 创建邀请输入
type CreateInvitationInput struct {
	DepartmentID int64  `json:"departmentId"`
	InviteeID    int64  `json:"inviteeId"`
	IsLeader     bool   `json:"isLeader"`
	Message      string `json:"message"`
}

// InvitationListQuery 邀请列表查询
type InvitationListQuery struct {
	AdminID int64
	Role    string // sent / received
	Status  string
	Page    int
	Limit   int
}

// InvitationListResult 邀请列表结果
type InvitationListResult struct {
	Items      []DeptInvitation `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
}
