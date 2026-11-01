# restart-backend

Rebuilds the Go backend and restarts it on the running dev-up stack.

```sh
scripts/restart-backend/restart-backend.sh
```

1. Builds `./cmd/server` and `./cmd/seed` into `.data/bin`. If the build fails, the running backend is left alone (`logs/backend-build.log`).
2. Stops the backend recorded in `.data/run/backend.pid`, but only if that process still matches the record (pid, start time, command). Then waits for the API port to free.
3. Refuses if the port is still held: whatever holds it, this project didn't start it.
4. Starts `.data/bin/server` from `backend/`, detached, appending to `logs/backend.log`. Waits up to 180 s for `/readyz`.

The server reads the generated `backend/.env` itself, and the restart removes those keys from the inherited environment, so the file always wins and there's no hand-copied env list to drift. This script doesn't regenerate env: after editing `.env.dev` or `scaffold.json`, re-run dev-up. The new process is recorded in the same pid file, so dev-up's Ctrl-C, `q` and `dev-up.sh stop` still stop it. The dev-up menu's `r` does the same thing.
