CREATE TABLE users (
    telegram_id        BIGINT PRIMARY KEY,
    organization_name  VARCHAR(255),
    inn                VARCHAR(12)  UNIQUE,
    ogrn               VARCHAR(13)  UNIQUE,
    phone_number       VARCHAR(20),
    classification     VARCHAR(255),
    role               VARCHAR(15) NOT NULL,
    banned             BOOLEAN DEFAULT FALSE, 
    name               VARCHAR(255) 
);

CREATE TABLE tenders (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    start_price FLOAT NOT NULL,
    start_at TIMESTAMPTZ,                  
    status VARCHAR(16) NOT NULL DEFAULT 'pending_approval',
    conditions_path VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    classification VARCHAR(255),
    participants_count INTEGER NOT NULL DEFAULT 0,

    
    last_bid_at TIMESTAMPTZ,              
    current_price FLOAT NOT NULL,
    min_bid_decrease FLOAT NOT NULL DEFAULT 10000.0
);

CREATE TABLE tender_participants (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    CONSTRAINT unique_event_participant UNIQUE(event_id, user_id)
);

CREATE TABLE tender_bids (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    amount FLOAT NOT NULL,
    bid_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);