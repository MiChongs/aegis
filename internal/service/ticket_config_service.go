package service

import (
	"context"
	"net/http"
	"strings"

	admindomain "aegis/internal/domain/admin"
	ticketdomain "aegis/internal/domain/ticket"
	apperrors "aegis/pkg/errors"
)

// 工单配置：分类 / 处理组 / SLA 策略 / 快捷回复。
//
// 统一鉴权口径：
//   - 读：任意能进工单模块的管理员（提单表单要用分类，处理人要用快捷回复）
//   - 写：ticket:manage（平台级配置 appid=0 额外要求超管，避免应用管理员改动全局默认值）

// requireTicketManage 写配置的统一闸门。
func (s *TicketService) requireTicketManage(ctx context.Context, access *admindomain.AccessContext, appID int64) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.IsSuperAdmin {
		return nil
	}
	// 平台级配置影响所有应用，只有超管能改
	if appID <= 0 {
		return apperrors.New(40301, http.StatusForbidden, "平台级工单配置仅超级管理员可修改")
	}
	if !s.can(ctx, access, PermTicketManage, &appID) {
		return apperrors.New(40311, http.StatusForbidden, "无权修改工单配置")
	}
	return nil
}

// ─────────────── 分类 ───────────────

// ListCategories 分类列表（含平台级）。
func (s *TicketService) ListCategories(ctx context.Context, appID int64, onlyEnabled bool) ([]ticketdomain.Category, error) {
	return s.pg.ListTicketCategories(ctx, appID, onlyEnabled)
}

// SaveCategory 新建或更新分类。
func (s *TicketService) SaveCategory(ctx context.Context, access *admindomain.AccessContext, item ticketdomain.Category) (*ticketdomain.Category, error) {
	if item.ID > 0 {
		existing, err := s.pg.GetTicketCategory(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(40461, http.StatusNotFound, "工单分类不存在")
		}
		// appid 不可迁移，防止把应用级分类"提权"成平台级
		item.AppID = existing.AppID
	}
	if err := s.requireTicketManage(ctx, access, item.AppID); err != nil {
		return nil, err
	}
	item.Key = strings.TrimSpace(item.Key)
	item.Name = strings.TrimSpace(item.Name)
	if item.Key == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "分类标识不能为空")
	}
	if item.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "分类名称不能为空")
	}
	if item.DefaultPriority == "" {
		item.DefaultPriority = ticketdomain.PriorityNormal
	}
	if _, ok := ticketdomain.ValidPriorities[item.DefaultPriority]; !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的默认优先级")
	}
	if item.ParentID != nil && *item.ParentID == item.ID && item.ID > 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "分类不能以自身作为父级")
	}
	return s.pg.UpsertTicketCategory(ctx, item)
}

// DeleteCategory 删除分类。
func (s *TicketService) DeleteCategory(ctx context.Context, access *admindomain.AccessContext, id int64) error {
	existing, err := s.pg.GetTicketCategory(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return apperrors.New(40461, http.StatusNotFound, "工单分类不存在")
	}
	if err := s.requireTicketManage(ctx, access, existing.AppID); err != nil {
		return err
	}
	ok, err := s.pg.DeleteTicketCategory(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40461, http.StatusNotFound, "工单分类不存在")
	}
	return nil
}

// ─────────────── 处理组 ───────────────

// ListGroups 处理组列表（含成员）。
func (s *TicketService) ListGroups(ctx context.Context, appID int64, withMembers bool) ([]ticketdomain.Group, error) {
	groups, err := s.pg.ListTicketGroups(ctx, appID, false)
	if err != nil {
		return nil, err
	}
	if !withMembers {
		return groups, nil
	}
	for i := range groups {
		members, err := s.pg.ListTicketGroupMembers(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}
		groups[i].Members = members
	}
	return groups, nil
}

// SaveGroup 新建或更新处理组。
func (s *TicketService) SaveGroup(ctx context.Context, access *admindomain.AccessContext, item ticketdomain.Group) (*ticketdomain.Group, error) {
	if item.ID > 0 {
		existing, err := s.pg.GetTicketGroup(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(40462, http.StatusNotFound, "处理组不存在")
		}
		item.AppID = existing.AppID
	}
	if err := s.requireTicketManage(ctx, access, item.AppID); err != nil {
		return nil, err
	}
	item.Key = strings.TrimSpace(item.Key)
	item.Name = strings.TrimSpace(item.Name)
	if item.Key == "" || item.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "处理组标识与名称不能为空")
	}
	switch item.AssignStrategy {
	case "", "manual":
		item.AssignStrategy = "manual"
	case "round_robin", "least_open":
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的分派策略")
	}
	return s.pg.UpsertTicketGroup(ctx, item)
}

// DeleteGroup 删除处理组。
func (s *TicketService) DeleteGroup(ctx context.Context, access *admindomain.AccessContext, id int64) error {
	existing, err := s.pg.GetTicketGroup(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return apperrors.New(40462, http.StatusNotFound, "处理组不存在")
	}
	if err := s.requireTicketManage(ctx, access, existing.AppID); err != nil {
		return err
	}
	ok, err := s.pg.DeleteTicketGroup(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40462, http.StatusNotFound, "处理组不存在")
	}
	return nil
}

// SetGroupMembers 全量设置组成员，这是"授权特定人员处理工单"的入口。
func (s *TicketService) SetGroupMembers(ctx context.Context, access *admindomain.AccessContext, groupID int64, members []ticketdomain.GroupMember) ([]ticketdomain.GroupMember, error) {
	group, err := s.pg.GetTicketGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, apperrors.New(40462, http.StatusNotFound, "处理组不存在")
	}
	if err := s.requireTicketManage(ctx, access, group.AppID); err != nil {
		return nil, err
	}
	if len(members) > 200 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "单个处理组成员不能超过 200 人")
	}
	valid := make([]ticketdomain.GroupMember, 0, len(members))
	for _, member := range members {
		if member.AdminID <= 0 {
			continue
		}
		valid = append(valid, member)
	}
	if err := s.pg.SetTicketGroupMembers(ctx, groupID, valid); err != nil {
		return nil, err
	}
	return s.pg.ListTicketGroupMembers(ctx, groupID)
}

// ─────────────── SLA 策略 ───────────────

// ListSLAPolicies SLA 策略列表。
func (s *TicketService) ListSLAPolicies(ctx context.Context, appID int64) ([]ticketdomain.SLAPolicy, error) {
	return s.pg.ListTicketSLAPolicies(ctx, appID)
}

// SaveSLAPolicy 新建或更新 SLA 策略。
func (s *TicketService) SaveSLAPolicy(ctx context.Context, access *admindomain.AccessContext, item ticketdomain.SLAPolicy) (*ticketdomain.SLAPolicy, error) {
	if item.ID > 0 {
		existing, err := s.pg.GetTicketSLAPolicy(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(40463, http.StatusNotFound, "SLA 策略不存在")
		}
		item.AppID = existing.AppID
	}
	if err := s.requireTicketManage(ctx, access, item.AppID); err != nil {
		return nil, err
	}
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "策略名称不能为空")
	}
	if item.WarnRatio <= 0 || item.WarnRatio >= 1 {
		item.WarnRatio = 0.8
	}
	item.FirstResponseMinutes = sanitizeSLAMinutes(item.FirstResponseMinutes)
	item.ResolveMinutes = sanitizeSLAMinutes(item.ResolveMinutes)
	if item.BusinessHours != nil {
		if _, ok := parseClock(item.BusinessHours.Start); !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "工作时间起点格式应为 HH:mm")
		}
		if _, ok := parseClock(item.BusinessHours.End); !ok {
			return nil, apperrors.New(40000, http.StatusBadRequest, "工作时间终点格式应为 HH:mm")
		}
	}
	return s.pg.UpsertTicketSLAPolicy(ctx, item)
}

// sanitizeSLAMinutes 只保留合法优先级键，且分钟数落在 1 分钟 ~ 90 天之间。
func sanitizeSLAMinutes(in map[string]int) map[string]int {
	out := make(map[string]int, len(ticketdomain.ValidPriorities))
	for priority := range ticketdomain.ValidPriorities {
		minutes, ok := in[priority]
		if !ok || minutes <= 0 {
			continue
		}
		if minutes > 90*24*60 {
			minutes = 90 * 24 * 60
		}
		out[priority] = minutes
	}
	return out
}

// DeleteSLAPolicy 删除 SLA 策略。
func (s *TicketService) DeleteSLAPolicy(ctx context.Context, access *admindomain.AccessContext, id int64) error {
	existing, err := s.pg.GetTicketSLAPolicy(ctx, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return apperrors.New(40463, http.StatusNotFound, "SLA 策略不存在")
	}
	if err := s.requireTicketManage(ctx, access, existing.AppID); err != nil {
		return err
	}
	ok, err := s.pg.DeleteTicketSLAPolicy(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40463, http.StatusNotFound, "SLA 策略不存在")
	}
	return nil
}

// ─────────────── 快捷回复 ───────────────

// ListQuickReplies 当前管理员可用的快捷回复（共享 + 自己的）。
func (s *TicketService) ListQuickReplies(ctx context.Context, access *admindomain.AccessContext, appID int64) ([]ticketdomain.QuickReply, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	return s.pg.ListTicketQuickReplies(ctx, appID, access.AdminID)
}

// SaveQuickReply 新建或更新快捷回复。
// 私人话术（owner = 自己）任何处理人都能维护；共享话术需要 ticket:manage。
func (s *TicketService) SaveQuickReply(ctx context.Context, access *admindomain.AccessContext, item ticketdomain.QuickReply) (*ticketdomain.QuickReply, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	isPrivate := item.OwnerAdminID != nil && *item.OwnerAdminID == access.AdminID
	if !isPrivate {
		if err := s.requireTicketManage(ctx, access, item.AppID); err != nil {
			return nil, err
		}
	}
	item.Title = strings.TrimSpace(item.Title)
	item.Content = strings.TrimSpace(item.Content)
	if item.Title == "" || item.Content == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "标题与内容不能为空")
	}
	return s.pg.UpsertTicketQuickReply(ctx, item)
}

// DeleteQuickReply 删除快捷回复。
func (s *TicketService) DeleteQuickReply(ctx context.Context, access *admindomain.AccessContext, id int64, appID int64) error {
	if err := s.requireTicketManage(ctx, access, appID); err != nil {
		return err
	}
	ok, err := s.pg.DeleteTicketQuickReply(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40464, http.StatusNotFound, "快捷回复不存在")
	}
	return nil
}

// UseQuickReply 记录一次使用（热度排序）。
func (s *TicketService) UseQuickReply(ctx context.Context, id int64) error {
	return s.pg.IncrTicketQuickReplyUsage(ctx, id)
}
