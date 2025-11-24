-- name: CreatePendingUser :exec
INSERT INTO pending_users (
    telegram_id, 
    organization_name, 
    inn, 
    phone_number, 
    name, 
    classification,
    created_at
) VALUES ($1, $2, $3, $4, $5, $6, NOW());

-- name: GetPendingUser :one
SELECT * FROM pending_users WHERE telegram_id = $1;

-- name: ApprovePendingUser :exec
DELETE FROM pending_users WHERE telegram_id = $1;

-- name: GetAllPendingUsers :many
SELECT * FROM pending_users ORDER BY created_at DESC;