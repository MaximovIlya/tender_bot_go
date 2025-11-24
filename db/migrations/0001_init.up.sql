CREATE TABLE users (
    telegram_id        BIGINT PRIMARY KEY,
    organization_name  VARCHAR(255),
    inn                VARCHAR(12)  UNIQUE,
    ogrn               VARCHAR(13)  UNIQUE,
    phone_number       VARCHAR(20),
    classification     VARCHAR(255),
    role               VARCHAR(15) NOT NULL,
    banned             BOOLEAN DEFAULT false, 
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
    message_sent BOOLEAN DEFAULT false,

    
    last_bid_at TIMESTAMPTZ,              
    current_price FLOAT NOT NULL,
    min_bid_decrease FLOAT NOT NULL DEFAULT 10000.0
);

CREATE TABLE tender_participants (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    CONSTRAINT unique_event_participant UNIQUE(tender_id, user_id)
);

CREATE TABLE tender_bids (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
    amount FLOAT NOT NULL,
    bid_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE history (
    id SERIAL PRIMARY KEY,
    tender_id INTEGER NOT NULL REFERENCES tenders(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    winner VARCHAR(255),
    phone_number VARCHAR(20),
    inn VARCHAR(12),
    fio VARCHAR(255),
    bid FLOAT NOT NULL,
    start_price FLOAT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);