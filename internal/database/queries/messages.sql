-- name: CreateMessage :one
INSERT INTO messages (id, tenant_id, payload, status, retry_count)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tenant_id, payload, status, retry_count, created_at, updated_at;

-- name: GetMessage :one
SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
FROM messages
WHERE id = $1 AND tenant_id = $2;

-- name: ListMessagesByTenant :many
SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
FROM messages
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: ListMessagesByTenantCursor :many
SELECT id, tenant_id, payload, status, retry_count, created_at, updated_at
FROM messages
WHERE tenant_id = $1
  AND (created_at, id) < ($2, $3)
ORDER BY created_at DESC, id DESC
LIMIT $4;

-- name: UpdateMessageStatus :exec
UPDATE messages
SET status = $3, retry_count = $4, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2;

-- name: CountMessagesByTenant :one
SELECT COUNT(*) FROM messages WHERE tenant_id = $1;

-- name: CountMessagesByTenantAndStatus :one
SELECT COUNT(*) FROM messages WHERE tenant_id = $1 AND status = $2;

-- name: DeleteMessage :exec
DELETE FROM messages WHERE id = $1 AND tenant_id = $2;

-- name: CreateDeadLetterMessage :one
INSERT INTO dead_letter_messages (
    original_message_id, tenant_id, payload, error_reason, retry_count
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListDeadLetterMessagesByTenant :many
SELECT *
FROM dead_letter_messages
WHERE tenant_id = $1
ORDER BY failed_at DESC
LIMIT $2 OFFSET $3;

-- name: GetDeadLetterMessage :one
SELECT *
FROM dead_letter_messages
WHERE id = $1 AND tenant_id = $2;

-- name: CountDeadLetterMessagesByTenant :one
SELECT COUNT(*) FROM dead_letter_messages WHERE tenant_id = $1;
