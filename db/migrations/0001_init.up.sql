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

