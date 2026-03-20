package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	admindomain "aegis/internal/domain/admin"
	redislib "github.com/redis/go-redis/v9"
)

func (r *SessionRepository) SetAdminSession(ctx context.Context, token string, session admindomain.Session, ttl time.Duration) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.adminSessionKey(token), data, ttl).Err()
}

func (r *SessionRepository) GetAdminSession(ctx context.Context, token string) (*admindomain.Session, error) {
	value, err := r.client.Get(ctx, r.adminSessionKey(token)).Result()
	if err != nil {
		if err == redislib.Nil {
			return nil, nil
		}
		return nil, err
	}
	var session admindomain.Session
	if err := json.Unmarshal([]byte(value), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) DeleteAdminSession(ctx context.Context, token string) error {
	return r.client.Del(ctx, r.adminSessionKey(token)).Err()
}

func (r *SessionRepository) adminSessionKey(token string) string {
	return fmt.Sprintf("%s:admin:session:%s", r.keyPrefix, hashToken(token))
}
