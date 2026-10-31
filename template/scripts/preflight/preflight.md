# preflight

Read-only health check of the running dev-up stack. Run it first when something feels off: each failure explains why a test session would give misleading results, and names the fix.

```sh
scripts/preflight/preflight.sh      # exit 1 when any check failed
```

It changes nothing. It checks:

- **Services answer**:
  - the API's `/readyz` returns 200;
  - Postgres accepts a query (private cluster or docker);
  - web answers below 500, if dev-up started web;
  - Metro's `/status`, if dev-up started mobile.
  - It also notes whether dev-up itself is running.
- **Stale backend**: the running backend process (from `.data/run/backend.pid`, else whatever holds the API port) must have started after the newest `*.go`/`*.sql`/`go.mod`/`go.sum` change under `backend/`, committed or not, and after the newest commit touching `backend/`. A commit less than 60 minutes newer is only a note: committing code that was already running is common. Fix: `scripts/restart-backend/restart-backend.sh`.
- **Env files current**: `backend/.env`, `web/.env.local` and `mobile/.env` exist (for each surface directory present) and are newer than `.env.dev` and `scaffold.json`. Fix: re-run `scripts/dev-up/dev-up.sh`; `restart-backend` does not regenerate them.

Endpoints come from the generated `backend/.env` and `.data/run/stack.env`, so it always checks the ports dev-up actually chose. It refuses a non-localhost API.
