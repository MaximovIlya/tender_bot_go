-- name: CreateTender :one 
INSERT INTO tenders(title, description, start_price, start_at, conditions_path, current_price, classification)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTenders :many
SELECT * FROM tenders ORDER BY created_at DESC;

-- name: GetHistory :many 
SELECT * FROM tenders WHERE status = 'completed' ORDER BY created_at DESC;


-- name: GetTendersForDeletion :many
SELECT * FROM tenders 
WHERE status != 'completed' 
ORDER BY created_at DESC;

-- name: DeleteTender :exec
DELETE FROM tenders WHERE id = $1;

-- name: ApproveTender :exec
UPDATE tenders 
SET status = 'active_pending'
WHERE id = $1;

-- name: ActivatePendingTenders :exec
UPDATE tenders 
SET status = 'active' 
WHERE status = 'active_pending' 
AND start_at <= NOW();

-- name: GetTendersForSuppliers :many
SELECT * FROM tenders 
WHERE (status = 'active' OR status = 'active_pending')
AND (classification = $1 OR classification = $2);


-- name: JoinTender :exec
WITH inserted AS (
    INSERT INTO tender_participants (tender_id, user_id)
    VALUES ($1, $2)
    ON CONFLICT (tender_id, user_id) DO NOTHING
    RETURNING 1
)
UPDATE tenders
SET participants_count = participants_count + 1
WHERE tenders.id = $1 AND EXISTS (SELECT 1 FROM inserted);


-- name: LeaveTender :exec
WITH deleted AS (
    DELETE FROM tender_participants 
    WHERE tender_id = $1 AND user_id = $2
    RETURNING 1
)
UPDATE tenders
SET participants_count = participants_count - 1
WHERE tenders.id = $1 AND EXISTS (SELECT 1 FROM deleted);

-- name: CheckTenderParticipation :one
SELECT EXISTS(
    SELECT 1 FROM tender_participants 
    WHERE tender_id = $1 AND user_id = $2
) as is_participating;

-- name: GetTender :one
SELECT * FROM tenders WHERE id = $1;