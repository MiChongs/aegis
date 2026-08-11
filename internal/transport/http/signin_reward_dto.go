package httptransport

import (
	appdomain "aegis/internal/domain/app"
	"time"
)

type AdminSignInRewardPolicyUpdateRequest struct {
	Policy appdomain.SignInRewardPolicy `json:"policy" binding:"required"`
}

type AdminSignInRewardTestRequest struct {
	OccurredAt      *time.Time `json:"occurredAt,omitempty"`
	ConsecutiveDays int        `json:"consecutiveDays"`
	TotalSignIns    int64      `json:"totalSignIns"`
	UserExperience  int64      `json:"userExperience"`
}
