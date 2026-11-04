# dev-up feature hooks

A feature that needs something extra on the dev stack ships a hook. Examples: a local chain, `stripe listen`, a worker emulator. The hook is `scripts/dev-up/hooks/<name>.sh`, installed through the feature's `feature.json`. `<name>` is lowercase letters and digits only (`wallets.sh`, `payments.sh`); other file names are skipped with a warning.

## Lifecycle

dev-up sources every hook once, in name order, after the port block is picked. The file's top level should only define functions. It may define two, both optional:

| Function | Called | Typical use |
|---|---|---|
| `hook_<name>_prepare` | step 4, after the LAN IP and `--public` tunnel are known, before the env files are written. That is before dependencies, Postgres and the backend. | start a service the backend needs, then hand its URL or secret to a tier with `hook_env` |
| `hook_<name>_started` | step 9, after the backend is ready (or skipped with `--no-backend`), before web and Metro | services that need the API or the database, menu items |

If `hook_<name>_prepare` returns non-zero, the boot stops and everything started so far is torn down. A `_started` failure only warns.

`dev-up.sh menu` also sources the hooks and calls `hook_<name>_started` with `DEVUP_MODE=menu`, so menu items appear there too. `start_bg` refuses in menu mode, so check `$DEVUP_MODE` before starting anything.

## Functions

| Function | What it does |
|---|---|
| `start_bg <name> <dir> <cmd…>` | Starts a tracked service: stdin `/dev/null`, its own session, output appended to `logs/<name>.log`, a record in `.data/run/<name>.pid`. Sets `LAST_PID`. Stopped by Ctrl-C, `q` and `dev-up.sh stop` like every other service, and listed by `status`. |
| `wait_for <url> <label> <timeout_s> [pid] [log] [ready\|up]` | Polls until the URL answers: `up` (default) means any HTTP status below 500, `ready` means 200. Returns 0 when it answers, 2 if the pid died first, 1 on timeout. Prints the log tail on failure. |
| `hook_env <backend\|web\|mobile> <KEY> <value>` | **prepare only.** Writes KEY into that tier's generated file (`backend/.env`, `web/.env.local`, `mobile/.env`), above everything else, `.env.dev` and flags included. Values travel through a 0600 file, never a command line; no newlines or tabs. Declare KEY for the tier in `scaffold.json`. An undeclared key is still written, but warns. |
| `hook_port <6-9>` | Prints `PORT_BASE + offset`: the block's spare ports. Fails, with a note on stderr, if a process this project didn't start holds it. dev-up never kills it, so a busy port means picking another `DEV_PORT_BASE`. |
| `menu_add <key> <label> <function>` | Adds a menu item, listed under the hook's name (a leading `<name>: ` in the label is dropped there). Keys are one character. The built-ins `w a i d l r R s g t m p c q` are refused. |
| `env_get <file> <KEY>` | Reads a value from a generated env file. Use it when a command needs a secret, then pass it to that one command; never `export` it. |
| `psql_dev [args…]` | psql on the dev database (`_started` only: Postgres isn't up during prepare). Feed SQL on stdin and use `-v name=value` / `:'name'` for anything user-supplied. |
| `confirm <question>` | Asks y/N. `--yes` answers yes; without a terminal it refuses. |
| `run_shielded <cmd…>` | Runs a foreground command that Ctrl-C stops without tearing the stack down (for menu items). |
| `c_blue c_green c_yellow c_red c_dim`, `die` | Output. `die` during boot tears the stack down. |
| `port_owner <port>`, `pid_alive <name>` | Who holds a port; whether a recorded service still runs (sets `PID_PID`). |

## Variables

| Variable | Meaning |
|---|---|
| `ROOT`, `LOG_DIR`, `RUN_DIR`, `DATA_DIR` | project root, `logs/`, `.data/run/`, `.data/` |
| `SLUG`, `APP_NAME`, `BUNDLE_ID`, `APP_SCHEME` | project identity |
| `PORT_BASE`, `API_PORT`, `WEB_PORT`, `PG_PORT`, `METRO_PORT`, `MINIO_PORT`, `REDIS_PORT` | the block: base, +0 … +5 |
| `API_URL`, `WEB_URL` | `http://localhost:<port>` |
| `PUBLIC_URL` | the cloudflared URL with `--public`, else empty |
| `LAN_IP` | set with `--lan`/`--tunnel`, else empty |
| `DATABASE_URL`, `DB_NAME`, `DB_USER` | the dev database (reachable from `_started` on) |
| `RUN_BACKEND`, `RUN_WEB`, `RUN_MOBILE` | 1 when that surface runs |
| `DEVUP_MODE` | `boot` or `menu` |
| `INTERACTIVE`, `ASSUME_YES` | 1 when there is a terminal / `--yes` was given |

Hooks run inside dev-up's shell (`set -uo pipefail`, bash 3.2): quote everything, use `${VAR:-}` for anything that may be unset, prefix your function and variable names with the hook name, and never `source .env.dev`.

## Example

`scripts/dev-up/hooks/ledger.sh`: a local service on the block's first spare port. Its URL goes to the backend before the backend starts, and a menu item checks it.

```bash
# Local ledger emulator for the ledger feature (LEDGER_URL is declared for the
# backend tier in scaffold.json).

hook_ledger_prepare() {
  local port
  port="$(hook_port 6)" || return 1
  start_bg ledger "$ROOT" npx --yes ledger-emulator --port "$port" || return 1
  wait_for "http://127.0.0.1:$port/health" "ledger" 60 "$LAST_PID" "$LOG_DIR/ledger.log" ready || return 1
  hook_env backend LEDGER_URL "http://127.0.0.1:$port"
}

hook_ledger_started() {
  menu_add L "ledger: health" ledger_health
}

ledger_health() {
  curl -s "http://127.0.0.1:$((PORT_BASE + 6))/health"; echo
}
```

A secret produced at boot follows the same path, for example the signing secret `stripe listen --print-secret` prints: `hook_env backend STRIPE_WEBHOOK_SECRET "$secret"` in prepare, then `start_bg stripe "$ROOT" stripe listen --forward-to "$API_URL/webhooks/stripe"`.

Values from `hook_env` are written into the generated files, so `scripts/restart-backend` keeps them. They last until the next dev-up boot rewrites the files.
