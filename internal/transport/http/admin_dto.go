package httptransport

import admindomain "aegis/internal/domain/admin"

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
