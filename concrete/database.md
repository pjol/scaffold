# Database

Postgres 17 through `pgxpool` (`internal/db`). One database per app (`<slug>_dev` locally). Sessions run in UTC.

## Migrations

- Embedded SQL in `backend/migrations/`, applied on boot by `db.Migrate`: lexical order, one transaction per file under an advisory lock, recorded with a sha256 in `schema_migrations`. An edited applied file fails the boot outside development.
- Forward only. **Never edit or renumber an applied migration**; add the next number in the track's range. Keep every file valid SQL on its own.
- Name: `NNNN_<track>_<what>.sql`, lowercase, e.g. `1100_chat_init.sql`, `5003_app_add_listing_tags.sql`.

### The ledger

The number decides order, so ranges encode dependency order. `scaffold doctor` enforces it.

| Range | Track | Owner |
|---|---|---|
| 0001–0099 | `core` | concrete template (users, consent, audit, deletions, webhook events, job leases) |
| 0100–0999 | infrastructure features | blocks of 100, listed in feature-belt/SKILL.md (notifications, moderation, orgs, …) |
| 1000–4999 | product features | blocks of 100 (chat, feed, places, payments, …) |
| 5000–9999 | `app` | everything the project writes itself |

A feature may reference core and any feature with a lower block, never a higher one. App migrations may reference anything. When a project changes a feature's tables, it writes an `app` migration; it does not edit the feature's files.

## Conventions

- Primary keys `uuid DEFAULT gen_random_uuid()`; `bigserial` only for append-only logs.
- Every table has `created_at timestamptz NOT NULL DEFAULT now()`; mutable rows add `updated_at` (set in the UPDATE, no triggers).
- Foreign keys to `users(id)` without `ON DELETE CASCADE`: users are anonymised, not deleted, so history keeps its shape. Rows owned purely by one user may cascade from a parent the module deletes itself.
- Status columns are `text` with a `CHECK (status IN (…))`, not Postgres enums (cheaper to extend).
- Money: `bigint` minor units with a `currency text` column, or `numeric` for fractional assets (token amounts store base units as `numeric(78,0)`).
- Index every foreign key you filter by and every keyset order: `(parent_id, created_at DESC, id DESC)`.
- Soft-delete user content with `deleted_at` when other people's views depend on it (a removed chat message shows as removed); hard-delete otherwise.
- JSON goes in `jsonb` only for open-ended payloads (audit data, provider metadata). Anything filtered or joined gets a column.
- Text search: `pg_trgm` or `tsvector` columns in the owning feature's migration. Geo: plain `double precision` lat/lng plus a bounding-box index; add PostGIS only when a feature needs real geometry.

## Queries

- SQL lives in the owning package, beside the handler or in a `repo.go`. Use `pgx.CollectRows` with `RowToStructByPos`/`ByName`.
- `db.Querier` lets helpers run on the pool or inside a transaction.
- `db.IsNoRows` → `httpx.ErrNotFound`; `db.IsUniqueViolation(err, "constraint")` → `httpx.Conflict`.
- Queue-style work uses `FOR UPDATE SKIP LOCKED`; periodic singleton work uses `jobs.Every` (a lease row in `job_leases`, backend.md).
- To serialise writes per key (one booking per host, one new conversation per pair), make `SELECT pg_advisory_xact_lock(hashtextextended('<module>.<what>:' || $1::text, 0))` the first statement of the `db.WithTx` that does the work; the lock ends with that transaction. Never hold a lock or transaction open while doing work on other pool connections: under load every holder waits for a second connection and the pool starves.
- Never build SQL with string concatenation of user input.

## Seeds

`cmd/seed` (development only) creates the dev identities and gives each module the chance to add realistic rows through its own seed function. Seed rows live in the database, never in runtime code paths. Re-running is safe (upserts on stable keys).
