-- name: GetUserBidsForTender :many
SELECT * FROM tender_bids 
WHERE tender_id = $1 AND user_id = $2 
ORDER BY bid_time DESC;

-- name: CreateBid :exec
INSERT INTO tender_bids (tender_id, user_id, amount, bid_time) 
VALUES ($1, $2, $3, $4);

-- name: GetUserBidCount :one
SELECT COUNT(*) FROM tender_bids
WHERE tender_id = $1 AND user_id = $2;


-- name: UpdateTenderCurrentPrice :exec
UPDATE tenders 
SET current_price = $2
WHERE id = $1;