-- name: CreateTenant :one
INSERT INTO tenants (name, concurrency)
VALUES ($1, $2)
RETURNING id, name, concurrency, created_at, updated_at;

-- name: GetTenant :one
SELECT id, name, concurrency, created_at, updated_at
FROM tenants
WHERE id = $1;

-- name: GetTenantByName :one
SELECT id, name, concurrency, created_at, updated_at
FROM tenants
WHERE name = $1;

-- name: ListTenants :many
SELECT id, name, concurrency, created_at, updated_at
FROM tenants
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: UpdateTenantConcurrency :one
UPDATE tenants
SET concurrency = $2, updated_at = NOW()
WHERE id = $1
RETURNING id, name, concurrency, created_at, updated_at;

-- name: DeleteTenant :exec
DELETE FROM tenants
WHERE id = $1;

-- name: CountTenants :one
SELECT COUNT(*) FROM tenants;
