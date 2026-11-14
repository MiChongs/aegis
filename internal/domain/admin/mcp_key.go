package admin

import (
	"strings"
	"time"
)

// MCP 密钥：管理员给 MCP 服务端（AI 助手）签发的长期凭证，见 docs/mcp.md。
//
// 一把密钥恒代表签发它的那个管理员，判定是两层交集：管理员本人此刻的权限，
// 再叠加密钥自己的范围（权限点、应用、只读、来源 IP）。管理员被撤角色、停用，
// 密钥随之收窄或失效，不存在「密钥比人权限大」的情形。

// MCPKeyPrefix 明文密钥的固定前缀。中间件靠它把请求分流到密钥校验，
// 不带这个前缀的令牌一律按会话 JWT 处理。
const MCPKeyPrefix = "aegis_mcp_"

// MCPKeyAllPermissions 权限范围里的全部通配：含超级管理员专属接口（若签发人本身是超管）。
const MCPKeyAllPermissions = "*"

const (
	MCPKeyStatusActive   = "active"
	MCPKeyStatusDisabled = "disabled"
	MCPKeyStatusRevoked  = "revoked"
)

// MCPKey 一把密钥的元数据（不含明文与摘要）。
type MCPKey struct {
	ID                 int64      `json:"id"`
	AdminID            int64      `json:"adminId"`
	AdminAccount       string     `json:"adminAccount,omitempty"`
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	KeyPrefix          string     `json:"keyPrefix"`
	Permissions        []string   `json:"permissions"`
	AppIDs             []int64    `json:"appIds"`
	ReadOnly           bool       `json:"readOnly"`
	AllowIPs           []string   `json:"allowIps"`
	RateLimitPerMinute int        `json:"rateLimitPerMinute"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt         *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedIP         string     `json:"lastUsedIp"`
	UseCount           int64      `json:"useCount"`
	RotatedAt          *time.Time `json:"rotatedAt,omitempty"`
	RevokedAt          *time.Time `json:"revokedAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	// KeyHash 只在服务端比对时使用，永不出境。
	KeyHash []byte `json:"-"`
}

// Expired 是否已过期。
func (k *MCPKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)
}

// EffectiveStatus 列表展示用的状态：过期是时间推出来的，库里不落这一档。
func (k *MCPKey) EffectiveStatus(now time.Time) string {
	if k.Status == MCPKeyStatusActive && k.Expired(now) {
		return "expired"
	}
	return k.Status
}

// CreatedMCPKey 签发 / 轮换的结果，Secret 只出现这一次。
type CreatedMCPKey struct {
	MCPKey
	Secret string `json:"secret"`
}

// MCPKeyInput 签发与修改共用的参数。
type MCPKeyInput struct {
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	Permissions        []string   `json:"permissions"`
	AppIDs             []int64    `json:"appIds"`
	ReadOnly           bool       `json:"readOnly"`
	AllowIPs           []string   `json:"allowIps"`
	RateLimitPerMinute int        `json:"rateLimitPerMinute"`
	ExpiresAt          *time.Time `json:"expiresAt"`
}

// MCPKeyScope 挂在 AccessContext 上的密钥范围：中间件与 Authorize 都只读它。
type MCPKeyScope struct {
	KeyID       int64    `json:"keyId"`
	Name        string   `json:"name"`
	KeyPrefix   string   `json:"keyPrefix"`
	Permissions []string `json:"permissions"`
	AppIDs      []int64  `json:"appIds"`
	ReadOnly    bool     `json:"readOnly"`
}

// AllowsAll 范围是否为全部（`*`）。
func (s *MCPKeyScope) AllowsAll() bool {
	if s == nil {
		return false
	}
	for _, item := range s.Permissions {
		if item == MCPKeyAllPermissions {
			return true
		}
	}
	return false
}

// AllowsPermission 权限点是否落在密钥范围里。模式支持结尾通配：`ticket:*` 覆盖 `ticket:read`。
func (s *MCPKeyScope) AllowsPermission(permission string) bool {
	if s == nil {
		return false
	}
	for _, pattern := range s.Permissions {
		if MCPPermissionMatches(pattern, permission) {
			return true
		}
	}
	return false
}

// AllowsApp 应用是否落在密钥范围里；范围为空表示不限应用。
func (s *MCPKeyScope) AllowsApp(appID int64) bool {
	if s == nil || len(s.AppIDs) == 0 {
		return true
	}
	for _, id := range s.AppIDs {
		if id == appID {
			return true
		}
	}
	return false
}

// RestrictsApps 是否限定了应用。
func (s *MCPKeyScope) RestrictsApps() bool { return s != nil && len(s.AppIDs) > 0 }

// PermissionImplies 由授权包（internal/authz）在初始化时注入：判定一条授予模式是否覆盖
// 被请求的权限点，含「细分权限点 → 旧权限点」的兼容关系。domain 层不能反向 import authz。
var PermissionImplies func(granted, requested string) bool

// MCPPermissionMatches 单条模式与权限点的匹配。
//
// 签发于权限拆分之前的密钥范围里写的是旧代码（app:read / system:admin:manage …），
// 它们必须继续覆盖拆分出来的细分权限点，否则升级之后这些密钥会突然什么都调不动。
func MCPPermissionMatches(pattern, permission string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if PermissionImplies != nil {
		return PermissionImplies(pattern, permission)
	}
	if pattern == MCPKeyAllPermissions || pattern == permission {
		return true
	}
	if strings.HasSuffix(pattern, ":*") {
		return strings.HasPrefix(permission, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// MCPEndpoint MCP 端点目录里的一条管理端路由。
type MCPEndpoint struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Handler        string `json:"handler"`
	Group          string `json:"group"`
	Permission     string `json:"permission"`
	PermissionName string `json:"permissionName,omitempty"`
	AppScoped      bool   `json:"appScoped"`
	SuperAdminOnly bool   `json:"superAdminOnly,omitempty"`
	Allowed        bool   `json:"allowed"`
	Reason         string `json:"reason,omitempty"`
}
