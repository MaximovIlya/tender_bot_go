CREATE TABLE tender_bids (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    amount FLOAT NOT NULL
);