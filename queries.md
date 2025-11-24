`update tenders set status = 'active_pending' where status = 'pending_approval'`

`INSERT INTO tenders(title, description, start_price, start_at, conditions_path, current_price, classification)
VALUES ('камень', 'закупка камня', 1000000, '2025-11-24 17:54:00+03', '', 1000000 , '6')
RETURNING *;`