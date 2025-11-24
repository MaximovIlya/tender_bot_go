-- name: CreateUser :one
INSERT INTO users (telegram_id, organization_name, inn, ogrn, phone_number, classification, role, banned, name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;


-- name: GetUserByTelegramID :one
SELECT telegram_id, organization_name, inn, ogrn, phone_number, classification, role, banned, name
FROM users
WHERE telegram_id = $1;

-- name: UpdateUser :exec
UPDATE users
SET
    organization_name = $2,
    inn = $3,
    ogrn = $4,
    phone_number = $5,
    name = $6,
    classification = $7
WHERE telegram_id = $1;

-- name: GetUsersByClassification :many
SELECT telegram_id FROM users 
WHERE $1 = ANY(string_to_array(classification, ','));


-- name: GetAllUsers :many
SELECT * FROM users WHERE role = 'supplier';

-- name: BlockUser :exec
UPDATE users SET banned = true WHERE telegram_id = $1;

-- name: UnblockUser :exec
UPDATE users SET banned = false WHERE telegram_id = $1;