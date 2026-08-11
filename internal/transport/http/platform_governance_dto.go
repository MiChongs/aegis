package httptransport

import (
	platformdomain "aegis/internal/domain/platform"
)

// GovernanceActionRequest 单应用治理动作请求。
//
// `restrictions` 只在 restrict / update 生效；其余状态用服务端预设，
// 避免"前端传了一组限制、后端按另一组执行"这类两处配同一件事的分叉。
type GovernanceActionRequest struct {
	Action          string                       `json:"action" binding:"required"`
	Reason          string                       `json:"reason"`
	Restrictions    *platformdomain.Restrictions `json:"restrictions,omitempty"`
	Evidence        map[string]any               `json:"evidence,omitempty"`
	EndAt           string                       `json:"endAt,omitempty"`
	DurationSeconds int64                        `json:"durationSeconds,omitempty"`
	RevokeSessions  bool                         `json:"revokeSessions"`
	NotifyAdmins    bool                         `json:"notifyAdmins"`
}

// ToInput 转换为领域入参。endAt 支持 RFC3339 与 "2006-01-02 15:04:05"。
func (r GovernanceActionRequest) ToInput() (platformdomain.ActionInput, error) {
	input := platformdomain.ActionInput{
		Action:          r.Action,
		Reason:          r.Reason,
		Restrictions:    r.Restrictions,
		Evidence:        r.Evidence,
		DurationSeconds: r.DurationSeconds,
		RevokeSessions:  r.RevokeSessions,
		NotifyAdmins:    r.NotifyAdmins,
	}
	endAt, err := parseOptionalDateTime(r.EndAt)
	if err != nil {
		return input, err
	}
	input.EndAt = endAt
	return input, nil
}

// GovernanceBatchActionRequest 批量治理请求。
type GovernanceBatchActionRequest struct {
	AppIDs []int64 `json:"appids" binding:"required"`
	GovernanceActionRequest
}

// GovernanceRevokeSessionsRequest 强制下线全站会话请求。
type GovernanceRevokeSessionsRequest struct {
	Reason string `json:"reason"`
}

// GovernanceAppealRequest 提交治理申诉。
type GovernanceAppealRequest struct {
	Content     string   `json:"content" binding:"required"`
	Attachments []string `json:"attachments,omitempty"`
}

// GovernanceAppealReviewRequest 申诉裁决。
type GovernanceAppealReviewRequest struct {
	Decision string `json:"decision" binding:"required"`
	Note     string `json:"note"`
	Restore  *bool  `json:"restore,omitempty"`
}

// GovernanceDetailResponse 单应用治理详情（含最近流水与待审申诉）。
type GovernanceDetailResponse struct {
	Governance    platformdomain.Governance     `json:"governance"`
	RecentActions []platformdomain.ActionRecord `json:"recentActions"`
	PendingAppeal *platformdomain.Appeal        `json:"pendingAppeal,omitempty"`
	// CanGovern 当前管理员能否改动这条治理结论，用于控制台决定按钮显隐，
	// 而不是让人点了才吃 403
	CanGovern bool `json:"canGovern"`
	// CanDanger 当前管理员能否执行封禁 / 归档 / 强制下线
	CanDanger bool `json:"canDanger"`
}
