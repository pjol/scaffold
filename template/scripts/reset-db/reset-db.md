# reset-db

Drops and recreates the local dev database, then migrates and seeds it. It asks first.

```sh
scripts/reset-db/reset-db.sh          # asks y/N
scripts/reset-db/reset-db.sh --yes    # no question (agents, no terminal)
```

1. Needs the running dev-up stack's Postgres: the private cluster or the docker fallback, at the port `backend/.env` names. Refuses a non-local `DATABASE_URL`.
2. Builds the backend binaries. If the build fails, the database is left alone.
3. Runs `DROP DATABASE scaffoldapp_dev WITH (FORCE)`, which terminates open connections, then `CREATE DATABASE`.
4. Runs `cmd/seed`: migrations, then the dev identities (`admin@scaffoldapp.test` as admin, `sam@`, `jordan@`, `priya@`) with the current legal versions accepted. Logs go to `logs/seed.log`.
5. If the backend is running, restarts it so its connection pool starts clean.

It touches only `scaffoldapp_dev` on 127.0.0.1. To reset at boot instead: `scripts/dev-up/dev-up.sh --reset-db`.
