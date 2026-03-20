-- name: GetUserProfileByUserID :one
SELECT user_id, nickname, avatar, email, extra, updated_at
FROM user_profiles
WHERE user_id = $1
LIMIT 1;

-- name: UpsertUserProfile :exec
INSERT INTO user_profiles (user_id, nickname, avatar, email, extra, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (user_id)
DO UPDATE SET nickname = EXCLUDED.nickname,
              avatar = EXCLUDED.avatar,
              email = EXCLUDED.email,
              extra = EXCLUDED.extra,
              updated_at = NOW();
