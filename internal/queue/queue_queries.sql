-- name: Enqueue :one
INSERT INTO queue (data, reference)
VALUES (?, ?)
RETURNING id, data, reference, enqueued_at, unavailable_until, processed_at;

-- name: ClaimBatch :many
UPDATE queue
SET unavailable_until = strftime('%Y-%m-%d %H:%M:%f', 'now', '+' || CAST(? AS TEXT) || ' seconds')
WHERE id IN (
    SELECT pending.id
    FROM queue AS pending
    WHERE pending.reference = ?
      AND pending.processed_at IS NULL
      AND pending.unavailable_until <= strftime('%Y-%m-%d %H:%M:%f', 'now')
    ORDER BY pending.enqueued_at, pending.id
    LIMIT 50
)
RETURNING id, data, reference, enqueued_at, unavailable_until, processed_at;

-- name: MarkQueueDone :exec
UPDATE queue
SET processed_at = CURRENT_TIMESTAMP
WHERE id = ? AND processed_at IS NULL;

-- name: RetryQueue :exec
UPDATE queue
SET unavailable_until = strftime('%Y-%m-%d %H:%M:%f', 'now', '+' || ? || ' seconds')
WHERE id = ? AND processed_at IS NULL;

-- name: MarkQueueDead :exec
UPDATE queue
SET unavailable_until = '9999-01-01 00:00:00'
WHERE id = ? AND processed_at IS NULL;
