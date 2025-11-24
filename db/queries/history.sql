-- name: AddToHistory :exec
INSERT INTO history (tender_id, title, winner, phone_number, inn, fio, bid, start_price)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetTendersHistory :many
SELECT * FROM history ORDER BY created_at ASC;