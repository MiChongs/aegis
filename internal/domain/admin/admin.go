package admin

import "time"

type Account struct {
	ID           int64      `json:"id"`
	Account      string     `json:"account"`
	DisplayName  string     `json:"displayName"`
	Email        string     `json:"email"`
	Status       string     `json:"status"`
	IsSuperAdmin bool       `json:"isSuperAdmin"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
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
	FallbackToken bool      `json:"fallbackToken"`
}

type AccessContext struct {
	Session
	Assignments []Assignment `json:"assignments"`
}

type LoginResult struct {
	AccessToken string       `json:"accessToken"`
	ExpiresAt   time.Time    `json:"expiresAt"`
	TokenType   string       `json:"tokenType"`
	Admin       Account      `json:"admin"`
	Assignments []Assignment `json:"assignments"`
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

type RoleDefinition struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level"`
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions"`
}
