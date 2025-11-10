-- name: GetParticipantsForTender :many
SELECT user_id FROM tender_participants 
WHERE tender_id = $1;