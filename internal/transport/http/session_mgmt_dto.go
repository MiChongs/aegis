package httptransport

// GrantTempPermRequest 授予临时权限请求
type GrantTempPermRequest struct {
	AdminID    int64  `json:"adminId" binding:"required"`
	Permission string `json:"permission" binding:"required"`
	AppID      *int64 `json:"appId"`
	Reason     string `json:"reason"`
	ExpiresAt  string `json:"expiresAt" binding:"required"` // RFC3339
}

// CreateDelegationRequest 创建代理授权请求
type CreateDelegationRequest struct {
	DelegatorID int64  `json:"delegatorId" binding:"required"`
	DelegateID  int64  `json:"delegateId" binding:"required"`
	Scope       string `json:"scope" binding:"required"` // all / app / org
	ScopeID     *int64 `json:"scopeId"`
	Reason      string `json:"reason"`
	ExpiresAt   string `json:"expiresAt" binding:"required"` // RFC3339
}

// ForceLogoutRequest 强制踢出请求
type ForceLogoutRequest struct {
	Reason string `json:"reason"`
}
