-- calendar 1.1.2: outbox rows are claimed in a short transaction and sent
-- outside it. claim marks a row in flight (next_attempt_at is its lease: a
-- crashed sender's rows come due again after it); a result is recorded only
-- by the sender still holding the claim.

ALTER TABLE calendar_outbox ADD COLUMN claim uuid;
