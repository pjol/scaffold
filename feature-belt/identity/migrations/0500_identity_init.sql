-- identity: verification attempts, one row per attempt. A user's status is
-- their verified, unexpired attempt if any, else their newest attempt (none
-- when there are no rows). Only status, the provider inquiry id, timestamps
-- and the reviewer are stored: names, ID numbers and images stay with the
-- provider, and manual-review photos are deleted from storage right after
-- the decision (the keys are cleared once the objects are gone).

CREATE TABLE identity_verifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id),
    provider     text NOT NULL CHECK (provider IN ('persona', 'manual')),
    inquiry_id   text,                                   -- provider inquiry id (persona); NULL for manual review
    status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'rejected', 'expired')),
    reason       text NOT NULL DEFAULT '',               -- user-facing rejection reason
    document_key text,                                   -- manual: storage key (identity_document, private) until deleted
    selfie_key   text,
    submitted_at timestamptz,                            -- the user finished the flow; awaiting a decision
    decided_at   timestamptz,
    reviewed_by  uuid REFERENCES users (id),             -- staff decision; NULL = provider decision
    expires_at   timestamptz,                            -- verified status lapses (IDENTITY_VERIFICATION_TTL)
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX identity_verifications_inquiry_idx ON identity_verifications (provider, inquiry_id) WHERE inquiry_id IS NOT NULL;
CREATE INDEX identity_verifications_user_idx ON identity_verifications (user_id, created_at DESC, id DESC);
CREATE INDEX identity_verifications_queue_idx ON identity_verifications (status, created_at, id);
CREATE INDEX identity_verifications_verified_idx ON identity_verifications (user_id) WHERE status = 'verified';
-- Housekeeping: documents awaiting deletion, verifications about to lapse.
CREATE INDEX identity_verifications_docs_idx ON identity_verifications (decided_at)
    WHERE document_key IS NOT NULL OR selfie_key IS NOT NULL;
CREATE INDEX identity_verifications_expiry_idx ON identity_verifications (expires_at)
    WHERE status = 'verified' AND expires_at IS NOT NULL;

-- Provider-side erasure owed after an account deletion. Holds only the
-- provider's opaque inquiry id, never a user id.
CREATE TABLE identity_redactions (
    provider     text NOT NULL,
    inquiry_id   text NOT NULL,
    attempts     int NOT NULL DEFAULT 0,
    last_error   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    PRIMARY KEY (provider, inquiry_id)
);
CREATE INDEX identity_redactions_pending_idx ON identity_redactions (created_at) WHERE completed_at IS NULL;
