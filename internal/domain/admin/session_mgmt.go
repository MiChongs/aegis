package admin

import "time"

// AdminSessionRecord 管理员会话持久化记录
type AdminSessionRecord struct {
	ID           string     `json:"id"`
	AdminID      int64      `json:"adminId"`
	IP           string     `json:"ip"`
	UserAgent    string     `json:"userAgent"`
	Device       string     `json:"device"`
	IssuedAt     time.Time  `json:"issuedAt"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	LastActiveAt time.Time  `json:"lastActiveAt"`
	IsRevoked    bool       `json:"isRevoked"`
	RevokedBy    *int64     `json:"revokedBy,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	// 关联字段（JOIN 填充）
	AdminAccount string `json:"adminAccount,omitempty"`
	AdminName    string `json:"adminName,omitempty"`
}

// TempPermission 临时权限授予
type TempPermission struct {
	ID         int64     `json:"id"`
	AdminID    int64     `json:"adminId"`
	Permission string    `json:"permission"`
	AppID      *int64    `json:"appId,omitempty"`
	GrantedBy  int64     `json:"grantedBy"`
	Reason     string    `json:"reason"`
	ExpiresAt  time.Time `json:"expiresAt"`
	IsRevoked  bool      `json:"isRevoked"`
	CreatedAt  time.Time `json:"createdAt"`
	// 关联
	AdminAccount  string `json:"adminAccount,omitempty"`
	GranterName   string `json:"granterName,omitempty"`
}

// AdminDelegation 全局代理授权
type AdminDelegation struct {
	ID          int64     `json:"id"`
	DelegatorID int64     `json:"delegatorId"`
	DelegateID  int64     `json:"delegateId"`
	Scope       string    `json:"scope"` // all / app / org
	ScopeID     *int64    `json:"scopeId,omitempty"`
	GrantedBy   int64     `json:"grantedBy"`
	Reason      string    `json:"reason"`
	ExpiresAt   time.Time `json:"expiresAt"`
	IsRevoked   bool      `json:"isRevoked"`
	CreatedAt   time.Time `json:"createdAt"`
	// 关联
	DelegatorName string `json:"delegatorName,omitempty"`
	DelegateName  string `json:"delegateName,omitempty"`
}

// OnlineAdmin 在线管理员摘要
type OnlineAdmin struct {
	AdminID      int64     `json:"adminId"`
	Account      string    `json:"account"`
	DisplayName  string    `json:"displayName"`
	SessionCount int64     `json:"sessionCount"`
	LastActiveAt time.Time `json:"lastActiveAt"`
}
