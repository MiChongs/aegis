package httptransport

import systemdomain "aegis/internal/domain/system"

// ── 审批链 DTO ──

type CreateApprovalChainRequest struct {
	Name        string                     `json:"name" binding:"required"`
	TriggerType string                     `json:"triggerType" binding:"required"`
	Steps       []systemdomain.ApprovalStep `json:"steps" binding:"required"`
}

type UpdateApprovalChainRequest struct {
	Name     *string                      `json:"name,omitempty"`
	Steps    *[]systemdomain.ApprovalStep `json:"steps,omitempty"`
	IsActive *bool                        `json:"isActive,omitempty"`
}

// ── 审批操作 DTO ──

type ApproveRejectRequest struct {
	Comment string `json:"comment"`
}

// ── 权限模板 DTO ──

type CreatePermTemplateRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions" binding:"required"`
	IsDefault   bool     `json:"isDefault"`
}

type ApplyTemplateRequest struct {
	AdminID int64 `json:"adminId" binding:"required"`
}

// ── 资源绑定 DTO ──

type BindOrgAppRequest struct {
	AppID int64 `json:"appId" binding:"required"`
}

// ── 协作组 DTO ──

type CreateCollabGroupRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	DeptIDs     []int64  `json:"deptIds"`
	Permissions []string `json:"permissions"`
}

type UpdateCollabGroupRequest struct {
	Name        *string   `json:"name,omitempty"`
	Description *string   `json:"description,omitempty"`
	DeptIDs     *[]int64  `json:"deptIds,omitempty"`
	Permissions *[]string `json:"permissions,omitempty"`
}

// ── 审批实例查询参数 ──

type ApprovalInstanceListQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}
