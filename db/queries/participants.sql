-- name: GetParticipantsForTender :many
SELECT user_id FROM tender_participants 
WHERE tender_id = $1;

-- name: GetTenderFromParticipants :one
select (tender_id) from tender_participants 
where user_id = $1;

-- name: RemoveParticipants :exec
DELETE FROM tender_participants WHERE tender_id = $1;