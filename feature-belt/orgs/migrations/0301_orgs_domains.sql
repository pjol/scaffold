-- orgs 1.1: join by verified email domain. An org claims an email domain;
-- once verified (an admin's own account email is on it, or a DNS TXT
-- record), people whose verified email is on that domain join on their own
-- (auto_join) or ask and wait for an admin (request).

CREATE TABLE org_domains (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    domain           text NOT NULL,                          -- lowercase ASCII, no trailing dot
    policy           text NOT NULL DEFAULT 'request' CHECK (policy IN ('auto_join', 'request')),
    role             text NOT NULL DEFAULT 'member' CHECK (role IN ('member')),
    verify_token     text NOT NULL,                          -- TXT value: scaffoldapp-verify=<token>
    verified_at      timestamptz,
    verify_method    text CHECK (verify_method IN ('email', 'dns')),
    verified_by      uuid REFERENCES users (id),
    last_checked_at  timestamptz,                            -- last DNS check (rate limit + display)
    last_check_error text,
    added_by         uuid REFERENCES users (id),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, domain)
);
-- One org owns a verified domain. Unverified claims don't block anyone.
CREATE UNIQUE INDEX org_domains_verified_idx ON org_domains (domain) WHERE verified_at IS NOT NULL;
CREATE INDEX org_domains_org_idx ON org_domains (org_id, created_at, id);
CREATE INDEX org_domains_added_by_idx ON org_domains (added_by);
CREATE INDEX org_domains_verified_by_idx ON org_domains (verified_by);

-- Requests to join through a request-policy domain. One pending per person
-- and org; a denial blocks new requests to that org for 7 days.
CREATE TABLE org_join_requests (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    domain     text NOT NULL,                                -- the email domain they qualified with
    status     text NOT NULL DEFAULT 'pending'
               CHECK (status IN ('pending', 'approved', 'denied', 'cancelled')),
    decided_by uuid REFERENCES users (id),                   -- NULL for auto-join and cancellations
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX org_join_requests_one_pending_idx ON org_join_requests (org_id, user_id) WHERE status = 'pending';
CREATE INDEX org_join_requests_pending_idx ON org_join_requests (org_id, created_at, id) WHERE status = 'pending';
CREATE INDEX org_join_requests_user_idx ON org_join_requests (user_id, created_at DESC);
CREATE INDEX org_join_requests_decided_by_idx ON org_join_requests (decided_by);

-- People who left an org or were removed from it, so domain auto-join never
-- brings them back. Joining again by any route clears the row.
CREATE TABLE org_departures (
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users (id),
    reason     text NOT NULL CHECK (reason IN ('left', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);
CREATE INDEX org_departures_user_idx ON org_departures (user_id);
