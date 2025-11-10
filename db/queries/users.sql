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

