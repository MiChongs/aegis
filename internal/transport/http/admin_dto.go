package httptransport

import (
	"time"

	admindomain "aegis/internal/domain/admin"
)

// AdminLoginAvailability 第三方认证源健康探测结果
// 仅超级管理员查询 /api/admin/system/admins 时附加到响应项里。
type AdminLoginAvailability struct {
	Source    string    `json:"source"`           // ldap / oidc / saml
	Available bool      `json:"available"`        // true 表示可用（enabled 且探测成功）
	Reason    string    `json:"reason,omitempty"` // 不可用时的中文原因
	CheckedAt time.Time `json:"checkedAt"`        // 本次探测/缓存时间
}

// AdminListItem 管理员列表响应项（超管可见 loginAvailability 字段）
// 非超管会话直接返回 admindomain.Profile，不包含 loginAvailability。
type AdminListItem struct {
	admindomain.Profile
	LoginAvailability *AdminLoginAvailability `json:"loginAvailability,omitempty"`
}

type AdminLoginRequest struct {
	Account       string `json:"account" form:"account" binding:"required"`
	Password      string `json:"password" form:"password" binding:"required"`
	CaptchaID     string `json:"captchaId" form:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer" form:"captchaAnswer"`
}

type AdminVerifyMFARequest struct {
	ChallengeID  string `json:"challengeId" form:"challengeId" binding:"required"`
	Code         string `json:"code" form:"code"`
	RecoveryCode string `json:"recoveryCode" form:"recoveryCode"`
}

type AdminCreateRequest struct {
	Account      string                           `json:"account" binding:"required"`
	Password     string                           `json:"password" binding:"required"`
	DisplayName  string                           `json:"displayName"`
	Email        string                           `json:"email"`
	IsSuperAdmin bool                             `json:"isSuperAdmin"`
	Assignments  []admindomain.AssignmentMutation `json:"assignments"`
}

type AdminStatusUpdateRequest struct {
	Status string `json:"status" form:"status" binding:"required"`
}

type AdminAccessUpdateRequest struct {
	IsSuperAdmin bool                             `json:"isSuperAdmin"`
	Assignments  []admindomain.AssignmentMutation `json:"assignments"`
}

type AdminRegisterRequest struct {
	Account       string `json:"account" binding:"required"`
	Password      string `json:"password" binding:"required"`
	DisplayName   string `json:"displayName"`
	Email         string `json:"email"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type AdminProfileUpdateRequest struct {
	DisplayName string                     `json:"displayName" form:"displayName"`
	Email       string                     `json:"email" form:"email"`
	Avatar      string                     `json:"avatar" form:"avatar"`
	Phone       string                     `json:"phone" form:"phone"`
	Birthday    string                     `json:"birthday" form:"birthday"`
	Bio         string                     `json:"bio" form:"bio"`
	Contacts    []admindomain.ContactInfo  `json:"contacts"`
}
