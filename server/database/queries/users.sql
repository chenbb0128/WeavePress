-- name: CreateUser :execresult
INSERT INTO users (
    username,
    password_hash,
    nickname,
    avatar,
    role,
    status
) VALUES (?, ?, ?, ?, ?, ?);

-- name: GetUserByID :one
SELECT
    id,
    username,
    password_hash,
    nickname,
    avatar,
    role,
    status,
    created_at,
    updated_at
FROM users
WHERE id = ?
LIMIT 1;

-- name: GetUserByUsername :one
SELECT
    id,
    username,
    password_hash,
    nickname,
    avatar,
    role,
    status,
    created_at,
    updated_at
FROM users
WHERE username = ?
LIMIT 1;

-- name: UpdateUserProfile :execresult
UPDATE users
SET
    nickname = ?,
    avatar = ?
WHERE id = ?;

-- name: SetUserStatus :execresult
UPDATE users
SET status = ?
WHERE id = ?;
