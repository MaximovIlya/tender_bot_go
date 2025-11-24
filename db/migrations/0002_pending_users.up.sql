CREATE TABLE pending_users (
    id SERIAL PRIMARY KEY,
    telegram_id BIGINT NOT NULL,
    organization_name VARCHAR(255),
    inn VARCHAR(12),
    phone_number VARCHAR(20),
    name VARCHAR(255),
    classification VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);