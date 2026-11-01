# Scripts folder maintenance

## Why these scripts exist

Testing here is human-in-the-loop product use. A person clicks through the running dev-up stack on web and mobile and reports what is wrong. An agent fixes it, and the person looks again. These scripts only automate the setup around that loop: boot the stack, restart a piece, reset data, check that what is running is current.

Do **not** add test harnesses, e2e suites or "run all the checks" runners here or anywhere else. If a repetitive manual action slows testing down, add a script for that one action.

## Layout

| Path | What |
|---|---|
| `dev-up/` | the stack: `dev-up.sh` + `dev-up.md`. Its supporting files are `env.mjs` (env generator), `scrub.sql` (clone scrub) and `hooks/` (feature hooks + `README.md`, the hook API). |
| `preflight/` | read-only health check, stale-binary check, env freshness |
| `restart-backend/` | rebuild and restart the backend from the generated `backend/.env` |
| `reset-db/` | drop, recreate, migrate, seed (asks first) |
| `lib.sh` | shared plumbing, sourced and never run |

`lib.sh` holds:

- output helpers;
- `confirm` and `run_shielded`;
- stack discovery from the generated `backend/.env` and `.data/run/stack.env`;
- `require_local_config`, `require_local_stack` and `require_local_db`, which refuse anything that isn't localhost;
- `start_bg`, `wait_for`, the pid-file records and `stop_service`;
- `psql_dev`;
- the backend build, seed, start and stop.

Rules:

- Every folder holds exactly one script and one Markdown file named after it. `dev-up/` is the one exception, for the supporting files above.
- Name folders so `ls scripts/` finds them.
- A script that touches the stack sources `lib.sh` and calls one of the `require_local_*` guards first.

## Duties when touching a script

- If a script's `.md` no longer matches what it does, fix the `.md`.
- If the `.md` is right and the script broke for structural reasons (a moved path, a renamed route, a changed schema), fix the script. A description never says "this script is broken".
- Never stop a process the project didn't start. Every service goes through `start_bg`, so it has a pid file; a port held by anything else is reported and left alone.
- Never `source .env.dev`. Read generated values with `env_get` and pass a secret only to the one command that needs it.
- Bash 3.2 (macOS `/bin/bash`): no associative arrays, `mapfile`, `${var,,}`, `&>>` or `wait -n`, and guard empty arrays with `${a[@]+"${a[@]}"}`.
- New scripts follow the same shape: one folder, one script, one honest Markdown file.
