-- name: CreateSmallBlob :one
INSERT INTO blobs (key, meta, data, large)
VALUES (?, ?, ?, 0)
RETURNING key, meta, data, large;

-- name: GetSmallBlob :one
SELECT key, meta, data, large
FROM blobs
WHERE key = ? AND large = 0;

-- name: DeleteSmallBlob :exec
DELETE FROM blobs
WHERE key = ? AND large = 0;

-- name: CreateLargeBlob :one
INSERT INTO blobs (key, meta, data, large)
VALUES (?, ?, X'', 1)
RETURNING key, meta, data, large;

-- name: GetLargeBlob :one
SELECT key, meta, data, large
FROM blobs
WHERE key = ? AND large = 1;

-- name: DeleteLargeBlob :one
DELETE FROM blobs
WHERE key = ? AND large = 1
RETURNING key;
