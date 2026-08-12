package admin

import "time"

// ContactInfo 多平台联系方式
type ContactInfo struct {
	Platform string `json:"platform"` // wechat / qq / telegram / discord / twitter / github / phone / email / other
	Value    string `json:"value"`
	Label    string `json:"label,omitempty"`
}

type Account struct {
	ID           int64         `json:"id"`
	Account      string        `json:"account"`
	DisplayName  string        `json:"displayName"`
	Email        string        `json:"email"`
	Avatar       string        `json:"avatar"`
	Phone        string        `json:"phone"`
	Birthday     *time.Time    `json:"birthday,omitempty"`
	Bio          string        `json:"bio"`
	Contacts     []ContactInfo `json:"contacts,omitempty"`
	Status       string        `json:"status"`
	AuthSource   string        `json:"authSource"` // password / ldap / oidc / saml
	IsSuperAdmin bool          `json:"isSuperAdmin"`
	LastLoginAt  *time.Time    `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time     `json:"createdAt"`
	UpdatedAt    time.Time     `json:"updatedAt"`
}

type Assignment struct {
	RoleKey string `json:"roleKey"`
	AppID   *int64 `json:"appid,omitempty"`
	AppName string `json:"appName,omitempty"`
}

type Profile struct {
	Account     Account      `json:"account"`
	Assignments []Assignment `json:"assignments"`
}

type AuthRecord struct {
	Profile
	PasswordHash string `json:"-"`
}

type Session struct {
	AdminID      int64     `json:"adminId"`
	Account      string    `json:"account"`
	DisplayName  string    `json:"displayName"`
	TokenID      string    `json:"tokenId"`
	IssuedAt     time.Time `json:"issuedAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
	IsSuperAdmin bool      `json:"isSuperAdmin"`
}

type AccessContext struct {
	Session
	Assignments []Assignment `json:"assignments"`
}

// PermissionSnapshot 一次会话展开后的**生效**权限集，供控制台决定哪些入口该显示。
//
// 只下发角色 key 是不够的：前端得自己维护一份"角色 → 权限"的副本，
// 而那份副本一旦与后端的 builtInAdminRoles 漂移，用户看到的就是
// "按钮在、点了 403"。展开在服务端做，前端不需要知道角色体系长什么样。
//
// Apps 里的每一项是**该应用下的完整生效集**（已并入全局权限），
// 不是"额外追加的部分" —— 让调用方自己做并集是又一处会漂移的逻辑。
type PermissionSnapshot struct {
	// Global 全局作用域（appid 为 nil）下生效的权限点。
	Global []string `json:"global"`
	// Apps 按应用 ID（十进制字符串，JSON 对象键只能是字符串）索引的完整生效集。
	Apps map[string][]string `json:"apps"`
	// All 为超级管理员置真：它不走权限点判定，枚举权限集没有意义。
	All bool `json:"all"`
}

// SelfServiceState 自助能力状态。
//
// 自助注册出来的管理员没有任何角色分配，「创建自己的第一个应用」是唯一
// 能把自己从零权限带出来的动作。这个结构告诉控制台：这个出口现在开不开、
// 还剩几个名额、不开的话是为什么 —— 否则前端只能先让用户填完表单、
// 提交、再把一句"当前管理员无权执行此操作"糊在对话框底部。
type SelfServiceState struct {
	// CanCreateApp 当前能否创建应用。
	CanCreateApp bool `json:"canCreateApp"`
	// Privileged 为真表示走的是常规授权（app:write）而非自助配额，此时配额字段无意义。
	Privileged bool `json:"privileged"`
	// AppQuota 自助创建的应用数上限，0 表示不限。
	AppQuota int `json:"appQuota"`
	// AppsCreated 该管理员已自助创建的应用数。
	AppsCreated int `json:"appsCreated"`
	// Reason 不能创建时的人类可读原因，能创建时为空。
	Reason string `json:"reason,omitempty"`
}

// AccessSnapshot 是 /api/admin/auth/me 的回包：会话 + 展开后的权限 + 自助能力。
// 内嵌 AccessContext 保证旧字段（adminId / isSuperAdmin / assignments）原位不变。
type AccessSnapshot struct {
	AccessContext
	Permissions PermissionSnapshot `json:"permissions"`
	SelfService SelfServiceState   `json:"selfService"`
}

type LoginResult struct {
	AccessToken          string        `json:"accessToken,omitempty"`
	ExpiresAt            time.Time     `json:"expiresAt,omitempty"`
	TokenType            string        `json:"tokenType,omitempty"`
	Admin                Account       `json:"admin,omitempty"`
	Assignments          []Assignment  `json:"assignments,omitempty"`
	RequiresSecondFactor bool          `json:"requiresSecondFactor,omitempty"`
	Challenge            *MFAChallenge `json:"challenge,omitempty"`
}

type MFAChallenge struct {
	ChallengeID string    `json:"challengeId"`
	Methods     []string  `json:"methods"` // totp, recovery_code
	ExpiresAt   time.Time `json:"expiresAt"`
}

type AssignmentMutation struct {
	RoleKey string `json:"roleKey"`
	AppID   *int64 `json:"appid,omitempty"`
}

type CreateInput struct {
	Account      string               `json:"account"`
	Password     string               `json:"password"`
	DisplayName  string               `json:"displayName"`
	Email        string               `json:"email"`
	IsSuperAdmin bool                 `json:"isSuperAdmin"`
	Assignments  []AssignmentMutation `json:"assignments"`
}

type UpdateAccessInput struct {
	IsSuperAdmin bool                 `json:"isSuperAdmin"`
	Assignments  []AssignmentMutation `json:"assignments"`
}

type ProfileUpdate struct {
	DisplayName string        `json:"displayName"`
	Email       string        `json:"email"`
	Avatar      string        `json:"avatar"`
	Phone       string        `json:"phone"`
	Birthday    string        `json:"birthday"` // "2000-01-15" 或 ""
	Bio         string        `json:"bio"`
	Contacts    []ContactInfo `json:"contacts"`
}

type RoleDefinition struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level"`
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions"`
	IsCustom    bool     `json:"isCustom"`
	BaseRole    string   `json:"baseRole,omitempty"`
	CreatedBy   *int64   `json:"createdBy,omitempty"`
}

// CustomRole 自定义角色（持久化到数据库）
type CustomRole struct {
	ID          int64     `json:"id"`
	RoleKey     string    `json:"roleKey"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Level       int       `json:"level"`
	Scope       string    `json:"scope"`
	BaseRole    string    `json:"baseRole,omitempty"`
	Permissions []string  `json:"permissions"`
	CreatedBy   *int64    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type CreateCustomRoleInput struct {
	RoleKey     string   `json:"roleKey"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level"`
	Scope       string   `json:"scope"`
	BaseRole    string   `json:"baseRole,omitempty"`
	Permissions []string `json:"permissions"`
}

type UpdateCustomRoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level"`
	Permissions []string `json:"permissions"`
}

// RoleGraphNode 角色关系图节点
type RoleGraphNode struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	Scope     string `json:"scope"`
	IsCustom  bool   `json:"isCustom"`
	PermCount int    `json:"permCount"`
}

// RoleGraphEdge 角色关系图边
type RoleGraphEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Relation string `json:"relation"` // includes | inherits
}

// RoleGraph 角色关系图
type RoleGraph struct {
	Nodes []RoleGraphNode `json:"nodes"`
	Edges []RoleGraphEdge `json:"edges"`
}

// RoleMatrixRow 权限矩阵行
type RoleMatrixRow struct {
	PermissionCode string          `json:"permissionCode"`
	PermissionName string          `json:"permissionName"`
	GroupKey       string          `json:"groupKey"`
	GroupName      string          `json:"groupName"`
	Grants         map[string]bool `json:"grants"`
}

// RoleMatrix 权限矩阵
type RoleMatrix struct {
	Roles  []RoleDefinition  `json:"roles"`
	Groups []PermissionGroup `json:"groups"`
	Rows   []RoleMatrixRow   `json:"rows"`
}

// ImpactAdmin 受影响的管理员
type ImpactAdmin struct {
	AdminID     int64  `json:"adminId"`
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
}

// ImpactPreview 修改影响预览
type ImpactPreview struct {
	AffectedAdmins []ImpactAdmin `json:"affectedAdmins"`
	TotalAffected  int           `json:"totalAffected"`
}

// Permission 单个权限项
type Permission struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PermissionGroup 权限分组
type PermissionGroup struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions"`
}

// RoleWithPermissions 角色 + 权限树
type RoleWithPermissions struct {
	RoleDefinition
	PermissionGroups []PermissionGroup `json:"permissionGroups"`
}
