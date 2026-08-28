-- name: CountAdmins :one
SELECT COUNT(*) FROM admins;

-- name: CreateAdmin :one
INSERT INTO admins (email, password_hash)
VALUES (?, ?)
RETURNING id, email, created_at;

-- name: GetAdminCredentialsByEmail :one
SELECT id, email, password_hash, created_at
FROM admins
WHERE email = ? COLLATE NOCASE;

-- name: CreateAdminSession :exec
INSERT INTO admin_sessions (token_hash, admin_id, expires_at)
VALUES (?, ?, ?);

-- name: GetAdminBySession :one
SELECT admins.id, admins.email, admins.created_at
FROM admin_sessions
JOIN admins ON admins.id = admin_sessions.admin_id
WHERE admin_sessions.token_hash = ?
  AND admin_sessions.expires_at > ?;
