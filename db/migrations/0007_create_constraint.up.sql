ALTER TABLE tender_participants 
ADD CONSTRAINT unique_tender_participant 
UNIQUE (tender_id, user_id);