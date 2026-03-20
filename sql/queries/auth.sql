-- name: GetUserByAppAndAccount :one
SELECT id, appid, account, password_hash, enabled, disabled_end_time, vip_expire_at, created_at, updated_at
FROM users
WHERE appid = $1 AND account = $2
LIMIT 1;

-- name: GetUserByID :one
SELECT id, appid, account, password_hash, enabled, disabled_end_time, vip_expire_at, created_at, updated_at
FROM users
WHERE id = $1
LIMIT 1;

-- name: CreateUser :one
INSERT INTO users (appid, account, password_hash, enabled)
VALUES ($1, $2, $3, TRUE)
RETURNING id, appid, account, password_hash, enabled, disabled_end_time, vip_expire_at, created_at, updated_at;
