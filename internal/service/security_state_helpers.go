package service

import (
	"time"

	userdomain "aegis/internal/domain/user"
)

func securityBoolValue(state *userdomain.ProfileSecurityState, key string) bool {
	if state == nil {
		return false
	}
	switch key {
	case "password_change_required":
		if state.PasswordChangeRequired != nil {
			return *state.PasswordChangeRequired
		}
	}
	return false
}

func securityIntValue(state *userdomain.ProfileSecurityState, key string) int {
	if state == nil {
		return 0
	}
	switch key {
	case "password_strength_score":
		if state.PasswordStrengthScore != nil {
			return *state.PasswordStrengthScore
		}
	}
	return 0
}

func securityTimeValue(state *userdomain.ProfileSecurityState, key string) *time.Time {
	if state == nil {
		return nil
	}
	switch key {
	case "password_changed_at":
		if state.PasswordChangedAt != nil {
			value := state.PasswordChangedAt.UTC()
			return &value
		}
	case "password_expires_at":
		if state.PasswordExpiresAt != nil {
			value := state.PasswordExpiresAt.UTC()
			return &value
		}
	}
	return nil
}

func intPtr(value int) *int {
	return &value
}

func timePtr(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}
