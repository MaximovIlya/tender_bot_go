-- name: GetParticipantsForTender :many
SELECT user_id FROM tender_participants 
WHERE tender_id = $1;

-- name: GetTenderFromParticipants :one
select (tender_id) from tender_participants 
where user_id = $1;

-- name: RemoveParticipants :exec
DELETE FROM tender_participants WHERE tender_id = $1;

-- name: GetParticipantNumber :one
SELECT COUNT(*) + 1 as participant_number
FROM tender_participants tp1
WHERE tp1.tender_id = $1 
AND tp1.joined_at < (
    SELECT joined_at 
    FROM tender_participants tp2 
    WHERE tp2.tender_id = $1 AND tp2.user_id = $2
);