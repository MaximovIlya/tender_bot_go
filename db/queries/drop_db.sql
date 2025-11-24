-- name: DropDb :exec
TRUNCATE TABLE 
    history, 
    tender_bids, 
    tender_participants, 
    pending_users,
	tenders,
	users
CASCADE;