package service

import (
	"context"

	admindomain "aegis/internal/domain/admin"
)

// AdminDetail 控制台管理员详情的一次完整取数。
type AdminDetail struct {
	admindomain.Profile
	// Resolved 按数据权限展开之后、实际参与判定的角色分配（停用的角色已剔除）。
	Resolved []admindomain.Assignment `json:"resolved"`
	// Permissions 生效权限快照（与 /auth/me 同一段展开代码）。
	Permissions admindomain.PermissionSnapshot `json:"permissions"`
	// Grants 个人授权（直接授予 / 禁止）。
	Grants []AdminGrantView `json:"grants"`
	// TempPermissions 未过期的临时权限。
	TempPermissions []admindomain.TempPermission `json:"tempPermissions"`
	Capabilities    admindomain.AdminCapabilities `json:"capabilities"`
}

// GetAdminDetail 管理员详情。
func (s *AdminService) GetAdminDetail(ctx context.Context, actor *admindomain.AccessContext, adminID int64) (*AdminDetail, error) {
	profile, err := s.GetProfile(ctx, adminID)
	if err != nil {
		return nil, err
	}
	access := s.accessFromProfile(ctx, profile)
	detail := &AdminDetail{
		Profile:         *profile,
		Resolved:        access.Assignments,
		Permissions:     s.EffectivePermissions(ctx, access),
		Grants:          []AdminGrantView{},
		TempPermissions: []admindomain.TempPermission{},
		Capabilities:    s.AdminCapabilities(ctx, actor, profile.Account),
	}
	if detail.Grants, err = s.ListAdminGrants(ctx, adminID); err != nil {
		return nil, err
	}
	if temp, err := s.pg.GetActiveTempPermissions(ctx, adminID); err == nil && temp != nil {
		detail.TempPermissions = temp
	}
	return detail, nil
}
