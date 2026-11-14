package httptransport

type CreateCustomRoleRequest struct {
	RoleKey     string   `json:"roleKey" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Level       int      `json:"level" binding:"required,min=1,max=19"`
	Scope       string   `json:"scope" binding:"required,oneof=global app"`
	BaseRole    string   `json:"baseRole"`
	Permissions []string `json:"permissions"`
	Deny        []string `json:"deny"`
	Status      string   `json:"status"`
	SortOrder   int      `json:"sortOrder"`
	DataScope   string   `json:"dataScope"`
	DataAppIDs  []int64  `json:"dataAppIds"`
}

type UpdateCustomRoleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Level       int      `json:"level" binding:"min=1,max=19"`
	Permissions []string `json:"permissions"`
	Deny        []string `json:"deny"`
	BaseRole    *string  `json:"baseRole"`
	Status      string   `json:"status"`
	SortOrder   int      `json:"sortOrder"`
	DataScope   string   `json:"dataScope"`
	DataAppIDs  []int64  `json:"dataAppIds"`
}

// RolePermissionsRequest 角色编辑器「功能权限」页签的整组保存。
type RolePermissionsRequest struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

// RoleStatusRequest 启用 / 停用自定义角色。
type RoleStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=active disabled"`
}

// RoleCopyRequest 以某个角色为模板复制出一个自定义角色。
type RoleCopyRequest struct {
	RoleKey string `json:"roleKey" binding:"required"`
	Name    string `json:"name" binding:"required"`
}

// RoleMembersRequest 把一批管理员加进角色；按授权绑定的应用级角色要给 appid。
type RoleMembersRequest struct {
	AdminIDs []int64 `json:"adminIds" binding:"required,min=1"`
	AppID    *int64  `json:"appid"`
}
