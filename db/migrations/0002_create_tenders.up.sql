CREATE TABLE tenders (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT,
    start_price FLOAT NOT NULL,
    start_at TIMESTAMPTZ,                  
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    conditions_path VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    
    last_bid_at TIMESTAMPTZ,              
    current_price FLOAT NOT NULL,
    min_bid_decrease FLOAT NOT NULL DEFAULT 10000.0
);
