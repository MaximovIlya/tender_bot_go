ALTER TABLE tender_participants 
ADD joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW();