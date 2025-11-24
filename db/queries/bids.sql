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

-- name: GetBidsAfterTime :many
SELECT * FROM tender_bids 
WHERE tender_id = $1 AND bid_time > $2 
ORDER BY bid_time DESC;


-- name: GetBidsHistoryByTenderID :many
SELECT 
    b.amount,
    b.bid_time,
    u.organization_name
FROM tender_bids b
JOIN users u ON b.user_id = u.telegram_id
WHERE b.tender_id = $1
ORDER BY b.bid_time ASC;




-- name: CheckBidExists :one
SELECT COUNT(*) as count
FROM tender_bids 
WHERE tender_id = $1 AND amount = $2;