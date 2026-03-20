package httptransport

import admindomain "aegis/internal/domain/admin"

type AdminLoginRequest struct {
	Account  string `json:"account" form:"account" binding:"required"`
	Password string `json:"password" form:"password" binding:"required"`
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
