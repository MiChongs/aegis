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
	// IsSystemAdmin 系统管理员：超级管理员之上的一档（系统管理员一定也是超级管理员）。
	IsSystemAdmin bool `json:"isSystemAdmin"`
	// Tier 身份档位，由上面两列推导：system / super / admin。
	Tier        string     `json:"tier"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	// PreviousAccount 改名前的账号，未改过名时为空。旧名永久保留，不能被他人占用。
	PreviousAccount string `json:"previousAccount,omitempty"`
	// AccountChangedAt 改名时间。非空即唯一一次改名机会已用掉。
	AccountChangedAt *time.Time `json:"accountChangedAt,omitempty"`
	// PasswordChangedAt 最近一次自助修改密码的时间。
	PasswordChangedAt *time.Time `json:"passwordChangedAt,omitempty"`
	// CanChangeAccount 当前是否还能改名：本地密码账号且从未改过名。
	// 由服务端算好下发，控制台不必自己复刻这条规则。
	CanChangeAccount bool `json:"canChangeAccount"`
	// CanChangePassword 是否能在本平台修改密码。LDAP / OIDC / SAML 账号的密码归外部身份源管理。
	CanChangePassword bool `json:"canChangePassword"`
}

// 管理员身份档位。从高到低：系统管理员 > 超级管理员 > 管理员。
//
// 档位决定「谁能管谁」：只能管理档位严格低于自己的管理员（系统管理员之间例外，见 service/admin_tier.go）。
// 权限点决定「能做什么」，两者正交 —— 持有「管理员停用」权限点的普通管理员，
// 照样停用不了任何一位超级管理员。
const (
	TierSystem = "system"
	TierSuper  = "super"
	TierAdmin  = "admin"
)

// TierRank 档位的高低次序。
func TierRank(tier string) int {
	switch tier {
	case TierSystem:
		return 3
	case TierSuper:
		return 2
	default:
		return 1
	}
}

// TierOf 由两列布尔推导档位。
func TierOf(isSuper, isSystem bool) string {
	switch {
	case isSystem && isSuper:
		return TierSystem
	case isSuper:
		return TierSuper
	default:
		return TierAdmin
	}
}

// TierName 档位中文名。
func TierName(tier string) string {
	switch tier {
	case TierSystem:
		return "系统管理员"
	case TierSuper:
		return "超级管理员"
	default:
		return "管理员"
	}
}

// FillSelfServiceFlags 依据持久化字段推导 CanChangeAccount / CanChangePassword（顺带推导档位）。
func (a *Account) FillSelfServiceFlags() {
	a.Tier = TierOf(a.IsSuperAdmin, a.IsSystemAdmin)
	local := a.AuthSource == "" || a.AuthSource == "password"
	a.CanChangePassword = local
	a.CanChangeAccount = local && a.AccountChangedAt == nil
}

// AccountAvailability 新用户名的可用性检查结果。
type AccountAvailability struct {
	Account   string `json:"account"`
	Available bool   `json:"available"`
	// Reason 不可用时的机器可读原因：invalid / reserved / same / taken
	Reason string `json:"reason,omitempty"`
	// Message 不可用时的人类可读说明
	Message string `json:"message,omitempty"`
}

// PasswordChangeResult 修改密码的结果。
type PasswordChangeResult struct {
	PasswordChangedAt time.Time `json:"passwordChangedAt"`
	// RevokedSessions 随之下线的其他会话数
	RevokedSessions int64 `json:"revokedSessions"`
}

type Assignment struct {
	RoleKey string `json:"roleKey"`
	AppID   *int64 `json:"appid,omitempty"`
	AppName string `json:"appName,omitempty"`
	// AllApps 由数据权限「全部应用」展开而来：在任何应用里生效，但**不满足平台级请求**。
	// AppID 为空且 AllApps 为假才是真正的平台级（全局）授权。
	AllApps bool `json:"allApps,omitempty"`
	// DataScope 会话里展开后的分配记下它来自哪种数据权限（assigned / all / created / custom）。
	DataScope string `json:"dataScope,omitempty"`
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
	// IsSystemAdmin 与 IsSuperAdmin 一样每次请求随账号现查。
	IsSystemAdmin bool   `json:"isSystemAdmin"`
	Tier          string `json:"tier"`
}

type AccessContext struct {
	Session
	Assignments []Assignment `json:"assignments"`
	// MCPKey 本次请求经 MCP 密钥认证时非空：判定在管理员权限之上再叠加密钥范围（见 mcp_key.go）。
	MCPKey *MCPKeyScope `json:"mcpKey,omitempty"`
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
	// AnyApp 在**未列入 Apps 的任意应用**里生效的权限点（全局角色与「全部应用」数据权限带来的）。
	// 控制台判定某个应用的权限时：Apps[id] 有就用它，没有就用 AnyApp。
	AnyApp []string `json:"anyApp"`
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
	// Tier 身份档位；留空时按 IsSuperAdmin 推导（兼容旧调用方）。
	Tier          string `json:"tier,omitempty"`
	IsSystemAdmin bool   `json:"-"`
}

type UpdateAccessInput struct {
	IsSuperAdmin  bool                 `json:"isSuperAdmin"`
	Assignments   []AssignmentMutation `json:"assignments"`
	Tier          string               `json:"tier,omitempty"`
	IsSystemAdmin bool                 `json:"-"`
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

// 应用级角色的数据权限（决定角色在哪些应用里生效）。
const (
	DataScopeAssigned = "assigned" // 按授权时绑定的应用
	DataScopeAll      = "all"      // 全部应用（不含平台级）
	DataScopeCreated  = "created"  // 本人创建的应用
	DataScopeCustom   = "custom"   // 角色上指定的应用
)

// ValidDataScope 是否为合法的数据权限取值。
func ValidDataScope(scope string) bool {
	switch scope {
	case DataScopeAssigned, DataScopeAll, DataScopeCreated, DataScopeCustom:
		return true
	}
	return false
}

// 角色状态。停用的角色仍保留授权关系，但不再贡献任何权限。
const (
	RoleStatusActive   = "active"
	RoleStatusDisabled = "disabled"
)

type RoleDefinition struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Level       int    `json:"level"`
	Scope       string `json:"scope"`
	// Permissions 展开后的生效权限点（含通配展开、继承、拒绝）。
	Permissions []string `json:"permissions"`
	IsCustom    bool     `json:"isCustom"`
	BaseRole    string   `json:"baseRole,omitempty"`
	CreatedBy   *int64   `json:"createdBy,omitempty"`
	Status      string   `json:"status"`
	SortOrder   int      `json:"sortOrder"`
	DataScope   string   `json:"dataScope"`
	DataAppIDs  []int64  `json:"dataAppIds"`
	// Allow / Deny 角色定义里直接写的授予与禁止（自定义角色）。内置角色的 Allow 是代码给的定义。
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
	// MemberCount 持有该角色的管理员数（按人去重）。
	MemberCount int        `json:"memberCount"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
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
	Deny        []string  `json:"deny"`
	Status      string    `json:"status"`
	SortOrder   int       `json:"sortOrder"`
	DataScope   string    `json:"dataScope"`
	DataAppIDs  []int64   `json:"dataAppIds"`
	CreatedBy   *int64    `json:"createdBy,omitempty"`
	UpdatedBy   *int64    `json:"updatedBy,omitempty"`
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
	Deny        []string `json:"deny"`
	Status      string   `json:"status"`
	SortOrder   int      `json:"sortOrder"`
	DataScope   string   `json:"dataScope"`
	DataAppIDs  []int64  `json:"dataAppIds"`
}

type UpdateCustomRoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level"`
	Permissions []string `json:"permissions"`
	Deny        []string `json:"deny"`
	BaseRole    *string  `json:"baseRole,omitempty"`
	Status      string   `json:"status"`
	SortOrder   int      `json:"sortOrder"`
	DataScope   string   `json:"dataScope"`
	DataAppIDs  []int64  `json:"dataAppIds"`
}

// RoleMember 持有某角色的一条授权（同一个人在多个应用持有同一角色是多条）。
type RoleMember struct {
	AdminID     int64      `json:"adminId"`
	Account     string     `json:"account"`
	DisplayName string     `json:"displayName"`
	Avatar      string     `json:"avatar,omitempty"`
	Status      string     `json:"status"`
	Tier        string     `json:"tier"`
	AppID       *int64     `json:"appid,omitempty"`
	AppName     string     `json:"appName,omitempty"`
	GrantedAt   time.Time  `json:"grantedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// RoleDetail 角色编辑器的一次完整取数。
type RoleDetail struct {
	Role RoleDefinition `json:"role"`
	// Builtin 内置角色由代码定义：Allow 不可编辑，增减走 OverrideAllow / OverrideDeny。
	Builtin       bool     `json:"builtin"`
	OverrideAllow []string `json:"overrideAllow"`
	OverrideDeny  []string `json:"overrideDeny"`
	// Effective 按角色作用域展开的生效权限点（与真实判定同一段代码）。
	Effective []string `json:"effective"`
	// Legacy 角色策略里仍在使用的旧版合并权限点（编辑保存后会换成细分权限点）。
	Legacy  []string     `json:"legacy"`
	Members []RoleMember `json:"members"`
	// Editable 当前操作者能否编辑这个角色；不能时 Reason 说明原因。
	Editable bool   `json:"editable"`
	Reason   string `json:"reason,omitempty"`
}

// AdminCapabilities 当前操作者对某个管理员能做什么（列表与详情据此显隐按钮、给出锁定原因）。
type AdminCapabilities struct {
	ManageReason string `json:"manageReason,omitempty"`
	CanStatus    bool   `json:"canStatus"`
	CanAccess    bool   `json:"canAccess"`
	CanGrant     bool   `json:"canGrant"`
	CanSession   bool   `json:"canSession"`
	CanTier      bool   `json:"canTier"`
	IsSelf       bool   `json:"isSelf"`
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
