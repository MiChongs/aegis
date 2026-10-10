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
	// Policy 草稿策略；缺省时按应用已保存的策略试算
	Policy *appdomain.SignInRewardPolicy `json:"policy,omitempty"`
	// SimulateDays 逐日推演连续签到的天数，0 表示不推演，上限 120
	SimulateDays int `json:"simulateDays,omitempty" binding:"omitempty,min=0,max=120"`
}
