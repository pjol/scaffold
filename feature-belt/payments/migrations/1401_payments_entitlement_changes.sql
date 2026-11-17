-- payments 1.1.0: entitlement changes are announced to OnEntitlementChange
-- hooks. end_reported_at marks a row whose end has been announced (revoked
-- by a sync, or lapsed and found by the sweep), so each end is announced
-- once; a row that becomes active again clears it.

ALTER TABLE payment_entitlements ADD COLUMN end_reported_at timestamptz;

-- Ends that happened before this release are not news.
UPDATE payment_entitlements SET end_reported_at = now()
WHERE active_until IS NOT NULL AND active_until <= now();

CREATE INDEX payment_entitlements_lapse_idx ON payment_entitlements (active_until)
    WHERE end_reported_at IS NULL AND active_until IS NOT NULL;
