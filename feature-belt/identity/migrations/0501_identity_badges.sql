-- identity 1.1.0: verification badges beside ID verification. Kinds, highest
-- first: official > org > notable > paid; a person's badge is the highest
-- active one they hold. official comes from admins, org and notable from
-- reviewed applications, paid from an entitlement (SetPaidBadge).

-- Applications for reviewed badges. evidence is [{kind, ref, verified}]:
-- url | domain | account | text, verified computed by the app at submission.
CREATE TABLE identity_badge_applications (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid NOT NULL REFERENCES users (id),
    category      text NOT NULL,                            -- IDENTITY_BADGE_CATEGORIES key
    display_label text NOT NULL,                            -- what the badge would say
    status        text NOT NULL DEFAULT 'submitted'
                  CHECK (status IN ('submitted', 'needs_info', 'approved', 'rejected', 'withdrawn')),
    evidence      jsonb NOT NULL DEFAULT '[]',
    note          text NOT NULL DEFAULT '',                 -- the applicant's note for reviewers
    reviewer_note text NOT NULL DEFAULT '',                 -- staff's note; the applicant sees it
    reviewed_by   uuid REFERENCES users (id),
    submitted_at  timestamptz NOT NULL DEFAULT now(),       -- last (re)submission
    decided_at    timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
-- One open application per person and category.
CREATE UNIQUE INDEX identity_badge_applications_open_idx ON identity_badge_applications (user_id, category)
    WHERE status IN ('submitted', 'needs_info');
CREATE INDEX identity_badge_applications_user_idx ON identity_badge_applications (user_id, created_at DESC, id DESC);
CREATE INDEX identity_badge_applications_queue_idx ON identity_badge_applications (status, submitted_at, id);

-- One row per person and kind; an ended badge keeps its row (revoked_at) so
-- a staff removal of the paid badge can hold it (source staff_revoked).
CREATE TABLE identity_badges (
    user_id        uuid NOT NULL REFERENCES users (id),
    kind           text NOT NULL CHECK (kind IN ('official', 'org', 'notable', 'paid')),
    label          text NOT NULL DEFAULT '',                -- '' = the kind's label (IDENTITY_BADGE_LABELS)
    category       text,                                    -- reviewed badges: the application's category
    source         text NOT NULL CHECK (source IN ('review', 'admin', 'billing', 'staff_revoked')),
    application_id uuid REFERENCES identity_badge_applications (id) ON DELETE SET NULL,
    granted_by     uuid REFERENCES users (id),              -- staff; NULL for billing
    granted_at     timestamptz NOT NULL DEFAULT now(),      -- start of the current unbroken hold
    expires_at     timestamptz,                             -- NULL = no expiry; paid: the entitlement's end
    revoked_at     timestamptz,                             -- ended: removed, expired, plan ended, not eligible
    revoked_by     uuid REFERENCES users (id),
    revoked_reason text NOT NULL DEFAULT '',
    paid_active    boolean NOT NULL DEFAULT false,          -- paid: the entitlement holds (kept while staff hold the badge)
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, kind)
);
CREATE INDEX identity_badges_expiry_idx ON identity_badges (expires_at) WHERE revoked_at IS NULL AND expires_at IS NOT NULL;
CREATE INDEX identity_badges_admin_idx ON identity_badges (updated_at DESC, user_id DESC, kind DESC);
CREATE INDEX identity_badges_withheld_idx ON identity_badges (user_id)
    WHERE kind = 'paid' AND revoked_reason = 'not_eligible' AND paid_active;

-- Domain proofs: a TXT record <slug>-verify=<token> at _<slug>-verify.<domain>.
CREATE TABLE identity_domains (
    user_id     uuid NOT NULL REFERENCES users (id),
    domain      text NOT NULL,
    token       text NOT NULL,
    verified_at timestamptz,
    checked_at  timestamptz,
    last_error  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, domain)
);
