package admin

import "time"

// ContactInfo 多平台联系方式
type ContactInfo struct {
	Platform string `json:"platform"`         // wechat / qq / telegram / discord / twitter / github / phone / email / other
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
	AuthSource   string        `json:"authSource"`   // password / ldap / oidc
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
	AdminID       int64     `json:"adminId"`
	Account       string    `json:"account"`
	DisplayName   string    `json:"displayName"`
	TokenID       string    `json:"tokenId"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	IsSuperAdmin  bool      `json:"isSuperAdmin"`
}

type AccessContext struct {
	Session
	Assignments []Assignment `json:"assignments"`
}

type LoginResult struct {
	AccessToken          string                `json:"accessToken,omitempty"`
	ExpiresAt            time.Time             `json:"expiresAt,omitempty"`
	TokenType            string                `json:"tokenType,omitempty"`
	Admin                Account               `json:"admin,omitempty"`
	Assignments          []Assignment          `json:"assignments,omitempty"`
	RequiresSecondFactor bool                  `json:"requiresSecondFactor,omitempty"`
	Challenge            *MFAChallenge         `json:"challenge,omitempty"`
}

type MFAChallenge struct {
	ChallengeID string   `json:"challengeId"`
	Methods     []string `json:"methods"`     // totp, recovery_code
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
