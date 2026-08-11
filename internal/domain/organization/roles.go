package organization

// 组织内置角色。层级自高到低，高位角色天然覆盖低位角色的权限集。
const (
	RoleOwner   = "owner"   // 所有者：组织的唯一主人，可转让、可删组织
	RoleAdmin   = "admin"   // 管理员：除删组织 / 转让外的全部权限
	RoleManager = "manager" // 部门主管：管人管部门，不碰组织设置与角色
	RoleMember  = "member"  // 普通成员：只读 + 参与协作
	RoleViewer  = "viewer"  // 访客：只读
)

// 组织级权限点。与平台级权限点（app:* / system:*）分属不同命名空间，
// 平台超管绕过全部判定，平台管理员通过 admin_assignments 获得跨组织能力。
const (
	PermOrgRead     = "org:read"
	PermOrgWrite    = "org:write"    // 改组织资料 / 配额 / 设置
	PermOrgDelete   = "org:delete"   // 删除或归档组织
	PermOrgTransfer = "org:transfer" // 转让所有者

	PermDeptRead  = "org:dept:read"
	PermDeptWrite = "org:dept:write" // 建 / 改 / 移 / 删部门

	PermMemberRead   = "org:member:read"
	PermMemberWrite  = "org:member:write"  // 增删改成员、调整部门归属
	PermMemberInvite = "org:member:invite" // 发起邀请

	PermPositionRead  = "org:position:read"
	PermPositionWrite = "org:position:write"

	PermRoleRead  = "org:role:read"
	PermRoleWrite = "org:role:write" // 组织自定义角色与授权

	PermAppRead  = "org:app:read"
	PermAppBind  = "org:app:bind"

	PermApprovalRead   = "org:approval:read"
	PermApprovalManage = "org:approval:manage"

	PermImport = "org:import"
	PermExport = "org:export"

	PermActivityRead = "org:activity:read"
)

// AllPermissions 全部组织权限点，供控制台渲染权限勾选树
func AllPermissions() []string {
	return []string{
		PermOrgRead, PermOrgWrite, PermOrgDelete, PermOrgTransfer,
		PermDeptRead, PermDeptWrite,
		PermMemberRead, PermMemberWrite, PermMemberInvite,
		PermPositionRead, PermPositionWrite,
		PermRoleRead, PermRoleWrite,
		PermAppRead, PermAppBind,
		PermApprovalRead, PermApprovalManage,
		PermImport, PermExport,
		PermActivityRead,
	}
}

// PermissionGroup 权限分组（控制台展示用）
type PermissionGroup struct {
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Permissions []PermissionItem `json:"permissions"`
}

// PermissionItem 单个权限点
type PermissionItem struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// PermissionCatalog 权限目录，前端据此渲染勾选树 —— 新增权限点只改这里
func PermissionCatalog() []PermissionGroup {
	return []PermissionGroup{
		{Key: "org", Name: "组织", Permissions: []PermissionItem{
			{Code: PermOrgRead, Name: "查看组织资料"},
			{Code: PermOrgWrite, Name: "修改组织资料"},
			{Code: PermOrgDelete, Name: "归档 / 删除组织"},
			{Code: PermOrgTransfer, Name: "转让所有者"},
		}},
		{Key: "dept", Name: "部门", Permissions: []PermissionItem{
			{Code: PermDeptRead, Name: "查看部门"},
			{Code: PermDeptWrite, Name: "管理部门"},
		}},
		{Key: "member", Name: "成员", Permissions: []PermissionItem{
			{Code: PermMemberRead, Name: "查看成员"},
			{Code: PermMemberWrite, Name: "管理成员"},
			{Code: PermMemberInvite, Name: "邀请成员"},
		}},
		{Key: "position", Name: "岗位", Permissions: []PermissionItem{
			{Code: PermPositionRead, Name: "查看岗位"},
			{Code: PermPositionWrite, Name: "管理岗位"},
		}},
		{Key: "role", Name: "角色权限", Permissions: []PermissionItem{
			{Code: PermRoleRead, Name: "查看角色"},
			{Code: PermRoleWrite, Name: "管理角色与授权"},
		}},
		{Key: "app", Name: "应用资源", Permissions: []PermissionItem{
			{Code: PermAppRead, Name: "查看绑定应用"},
			{Code: PermAppBind, Name: "绑定 / 解绑应用"},
		}},
		{Key: "approval", Name: "审批", Permissions: []PermissionItem{
			{Code: PermApprovalRead, Name: "查看审批"},
			{Code: PermApprovalManage, Name: "配置审批链"},
		}},
		{Key: "data", Name: "数据", Permissions: []PermissionItem{
			{Code: PermImport, Name: "导入组织架构"},
			{Code: PermExport, Name: "导出组织架构"},
			{Code: PermActivityRead, Name: "查看操作日志"},
		}},
	}
}

// BuiltinRolePermissions 内置角色的权限集。
//
// owner 用通配 "*"：它是组织的主人，任何后续新增的权限点都应自动归它，
// 逐条枚举必然在下次加权限点时漏掉。
func BuiltinRolePermissions() map[string][]string {
	return map[string][]string{
		RoleOwner: {"*"},
		RoleAdmin: {
			PermOrgRead, PermOrgWrite,
			PermDeptRead, PermDeptWrite,
			PermMemberRead, PermMemberWrite, PermMemberInvite,
			PermPositionRead, PermPositionWrite,
			PermRoleRead, PermRoleWrite,
			PermAppRead, PermAppBind,
			PermApprovalRead, PermApprovalManage,
			PermImport, PermExport, PermActivityRead,
		},
		RoleManager: {
			PermOrgRead,
			PermDeptRead, PermDeptWrite,
			PermMemberRead, PermMemberWrite, PermMemberInvite,
			PermPositionRead,
			PermRoleRead,
			PermAppRead,
			PermApprovalRead,
			PermExport, PermActivityRead,
		},
		RoleMember: {
			PermOrgRead, PermDeptRead, PermMemberRead, PermPositionRead, PermAppRead, PermApprovalRead,
		},
		RoleViewer: {
			PermOrgRead, PermDeptRead, PermMemberRead,
		},
	}
}

// BuiltinRoleMeta 内置角色的展示信息
type BuiltinRoleMeta struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       int    `json:"level"`
}

// BuiltinRoles 内置角色定义（按层级从高到低）
func BuiltinRoles() []BuiltinRoleMeta {
	return []BuiltinRoleMeta{
		{Key: RoleOwner, Name: "所有者", Description: "组织的唯一主人，拥有全部权限，可转让所有权与归档组织", Level: 100},
		{Key: RoleAdmin, Name: "管理员", Description: "除转让所有权与删除组织外的全部管理权限", Level: 80},
		{Key: RoleManager, Name: "部门主管", Description: "管理部门与成员，不涉及组织设置与角色授权", Level: 60},
		{Key: RoleMember, Name: "成员", Description: "查看组织架构并参与协作", Level: 40},
		{Key: RoleViewer, Name: "访客", Description: "只读访问组织架构", Level: 20},
	}
}

// roleLevels 角色权重，用于「不能操作比自己等级高的人」判定
var roleLevels = map[string]int{
	RoleOwner: 100, RoleAdmin: 80, RoleManager: 60, RoleMember: 40, RoleViewer: 20,
}

// RoleLevel 返回角色权重，未知角色为 0
func RoleLevel(role string) int { return roleLevels[role] }

// IsValidRole 是否为合法的内置组织角色
func IsValidRole(role string) bool {
	_, ok := roleLevels[role]
	return ok
}

// IsValidStatus 组织状态是否合法
func IsValidStatus(status string) bool {
	switch status {
	case "active", "suspended", "archived":
		return true
	}
	return false
}

// IsValidOrgKind 组织类型是否合法
func IsValidOrgKind(kind string) bool {
	switch kind {
	case "enterprise", "subsidiary", "branch", "team", "partner":
		return true
	}
	return false
}

// IsValidDeptKind 部门类型是否合法
func IsValidDeptKind(kind string) bool {
	switch kind {
	case "department", "team", "group", "virtual":
		return true
	}
	return false
}

// IsValidMemberStatus 成员状态是否合法
func IsValidMemberStatus(status string) bool {
	switch status {
	case "active", "suspended", "left":
		return true
	}
	return false
}
