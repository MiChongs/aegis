package redis

import (
	"context"
	"fmt"
	"time"

	securitydomain "aegis/internal/domain/security"
)

func (r *SessionRepository) SetTwoFactorChallenge(ctx context.Context, challenge securitydomain.LoginChallenge, ttl time.Duration) error {
	return r.setJSON(ctx, r.twoFactorChallengeKey(challenge.ChallengeID), challenge, ttl)
}

func (r *SessionRepository) GetTwoFactorChallenge(ctx context.Context, challengeID string) (*securitydomain.LoginChallenge, error) {
	var challenge securitydomain.LoginChallenge
	found, err := r.getJSON(ctx, r.twoFactorChallengeKey(challengeID), &challenge)
	if err != nil || !found {
		return nil, err
	}
	return &challenge, nil
}

func (r *SessionRepository) DeleteTwoFactorChallenge(ctx context.Context, challengeID string) error {
	return r.client.Del(ctx, r.twoFactorChallengeKey(challengeID)).Err()
}

func (r *SessionRepository) SetTOTPEnrollmentState(ctx context.Context, state securitydomain.TOTPEnrollmentState, ttl time.Duration) error {
	return r.setJSON(ctx, r.totpEnrollmentKey(state.EnrollmentID), state, ttl)
}

func (r *SessionRepository) GetTOTPEnrollmentState(ctx context.Context, enrollmentID string) (*securitydomain.TOTPEnrollmentState, error) {
	var state securitydomain.TOTPEnrollmentState
	found, err := r.getJSON(ctx, r.totpEnrollmentKey(enrollmentID), &state)
	if err != nil || !found {
		return nil, err
	}
	return &state, nil
}

func (r *SessionRepository) DeleteTOTPEnrollmentState(ctx context.Context, enrollmentID string) error {
	return r.client.Del(ctx, r.totpEnrollmentKey(enrollmentID)).Err()
}

func (r *SessionRepository) SetPasskeyRegistrationState(ctx context.Context, state securitydomain.PasskeyRegistrationState, ttl time.Duration) error {
	return r.setJSON(ctx, r.passkeyRegistrationKey(state.ChallengeID), state, ttl)
}

func (r *SessionRepository) GetPasskeyRegistrationState(ctx context.Context, challengeID string) (*securitydomain.PasskeyRegistrationState, error) {
	var state securitydomain.PasskeyRegistrationState
	found, err := r.getJSON(ctx, r.passkeyRegistrationKey(challengeID), &state)
	if err != nil || !found {
		return nil, err
	}
	return &state, nil
}

func (r *SessionRepository) DeletePasskeyRegistrationState(ctx context.Context, challengeID string) error {
	return r.client.Del(ctx, r.passkeyRegistrationKey(challengeID)).Err()
}

func (r *SessionRepository) SetPasskeyLoginState(ctx context.Context, state securitydomain.PasskeyLoginState, ttl time.Duration) error {
	return r.setJSON(ctx, r.passkeyLoginKey(state.ChallengeID), state, ttl)
}

func (r *SessionRepository) GetPasskeyLoginState(ctx context.Context, challengeID string) (*securitydomain.PasskeyLoginState, error) {
	var state securitydomain.PasskeyLoginState
	found, err := r.getJSON(ctx, r.passkeyLoginKey(challengeID), &state)
	if err != nil || !found {
		return nil, err
	}
	return &state, nil
}

func (r *SessionRepository) DeletePasskeyLoginState(ctx context.Context, challengeID string) error {
	return r.client.Del(ctx, r.passkeyLoginKey(challengeID)).Err()
}

func (r *SessionRepository) twoFactorChallengeKey(challengeID string) string {
	return fmt.Sprintf("%s:security:2fa:challenge:%s", r.keyPrefix, challengeID)
}

func (r *SessionRepository) totpEnrollmentKey(enrollmentID string) string {
	return fmt.Sprintf("%s:security:totp:enrollment:%s", r.keyPrefix, enrollmentID)
}

func (r *SessionRepository) passkeyRegistrationKey(challengeID string) string {
	return fmt.Sprintf("%s:security:passkey:registration:%s", r.keyPrefix, challengeID)
}

func (r *SessionRepository) passkeyLoginKey(challengeID string) string {
	return fmt.Sprintf("%s:security:passkey:login:%s", r.keyPrefix, challengeID)
}

// ── OIDC state / ticket ──

func (r *SessionRepository) SetOIDCState(ctx context.Context, state string, ttl time.Duration) error {
	return r.client.Set(ctx, r.oidcStateKey(state), "1", ttl).Err()
}

func (r *SessionRepository) GetAndDeleteOIDCState(ctx context.Context, state string) (bool, error) {
	key := r.oidcStateKey(state)
	result, err := r.client.GetDel(ctx, key).Result()
	if err != nil {
		return false, nil
	}
	return result == "1", nil
}

func (r *SessionRepository) SetOIDCTicket(ctx context.Context, ticket string, payload []byte, ttl time.Duration) error {
	return r.client.Set(ctx, r.oidcTicketKey(ticket), payload, ttl).Err()
}

func (r *SessionRepository) GetAndDeleteOIDCTicket(ctx context.Context, ticket string) ([]byte, error) {
	key := r.oidcTicketKey(ticket)
	result, err := r.client.GetDel(ctx, key).Bytes()
	if err != nil {
		return nil, nil
	}
	return result, nil
}

func (r *SessionRepository) oidcStateKey(state string) string {
	return fmt.Sprintf("%s:oidc:state:%s", r.keyPrefix, state)
}

func (r *SessionRepository) oidcTicketKey(ticket string) string {
	return fmt.Sprintf("%s:oidc:ticket:%s", r.keyPrefix, ticket)
}
