package httptransport

type CreateCustomRoleRequest struct {
	RoleKey     string   `json:"roleKey" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Level       int      `json:"level" binding:"required,min=1,max=19"`
	Scope       string   `json:"scope" binding:"required,oneof=global app"`
	BaseRole    string   `json:"baseRole"`
	Permissions []string `json:"permissions" binding:"required,min=1"`
}

type UpdateCustomRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Level       int      `json:"level" binding:"min=1,max=19"`
	Permissions []string `json:"permissions" binding:"required,min=1"`
}
