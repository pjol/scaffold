-- scrub.sql: run by `dev-up.sh --clone-from <url>` right after the restore, in
-- one transaction (psql -1, ON_ERROR_STOP). If any statement fails, dev-up
-- drops the cloned database rather than leave unscrubbed data behind.
--
-- Purpose: nothing in a production copy may reach a real person or a real
-- provider from a laptop. Point webhooks, emails, push tokens and provider ids
-- away from production. The template ships every statement commented out:
-- uncomment what fits this app's schema, and add a block for each feature table
-- that stores contact details, tokens or outbound URLs.
--
-- Variables (psql -v; use them as :'name'):
--   :'api_url'      this stack's API origin, e.g. http://localhost:21970
--   :'web_url'      this stack's web origin
--   :'mail_domain'  the dev mail domain (scaffoldapp.test; .test never delivers)

-- Emails: every user becomes <id>@scaffoldapp.test, so a stray RESEND_API_KEY in
-- .env.dev can only ever "send" into the void. Dev identities keep theirs.
-- UPDATE users
--    SET email = id::text || '@' || :'mail_domain'
--  WHERE email NOT ILIKE '%@' || :'mail_domain';

-- Admin roles: nobody from production keeps admin locally (grant yourself one
-- with the dev-up menu: g).
-- UPDATE users SET admin_role = NULL WHERE email NOT ILIKE '%@' || :'mail_domain';

-- Provider ids: Clerk subjects from the production instance mean nothing to the
-- development instance; rename them so a dev sign-in can't collide.
-- UPDATE users SET auth_subject = 'prod_' || auth_subject WHERE auth_subject LIKE 'user\_%';

-- Webhook dedup ledger: production event ids are irrelevant here.
-- TRUNCATE webhook_events;

-- Feature examples (adapt the table and column names):
-- DELETE FROM push_tokens;                                     -- device push tokens
-- UPDATE outbound_webhooks SET url = :'api_url' || '/dev/null'; -- customer webhooks
-- UPDATE payment_customers SET provider_customer_id = NULL;     -- Stripe ids
