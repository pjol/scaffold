# dev-up

`scripts/dev-up/dev-up.sh` brings the whole stack up with one command, from a fresh clone, with zero third-party keys, and tears down exactly what it started on Ctrl-C. It is how every change is tested (human-in-the-loop on the running app). The template ships it; this file is its spec. Distilled from SFLuv's dev-up (the most complete), ChatMTG's private Postgres and DOGPATCH's dev.sh, with their gaps closed.

## Contract

```
scripts/dev-up/dev-up.sh [flags]          boot everything, then a resident menu
scripts/dev-up/dev-up.sh menu             the utilities menu against a running stack (boots nothing)
scripts/dev-up/dev-up.sh status           print URLs, ports, PIDs and health; exit 0 if up
scripts/dev-up/dev-up.sh stop             stop a stack this project's dev-up started (from its PID files)

--no-web --no-mobile --no-backend         skip a surface (absent directories are skipped automatically)
--reset-db                                drop and recreate the dev database (confirm), then migrate + seed
--no-seed                                 skip cmd/seed
--clone-from <postgres-url>               restore a remote database instead of seeding, then scrub it
--web-prod                                serve a production build (built in the background at boot) instead of next dev
--lan | --tunnel                          Expo for physical devices (LAN IP / Expo tunnel); default is localhost for simulators
--ios | --android                         open the simulator/emulator once Metro is up
--s3 | --redis                            start MinIO (the `minio` binary, else docker) / Redis on the project's ports and point the backend at them
--public                                  expose the API with cloudflared (webhooks from Clerk/Stripe, remote devices)
-h | --help                               the header comment
```

Flags are parsed before `.env.dev` is read and win over it. Unknown flags are errors.

## Boot order

1. **Tools and versions** (per subcommand): go ≥ go.mod, node ≥ `.nvmrc` major (tell the user `nvm use` if not), pnpm 10, `initdb`/`pg_ctl`/`psql` 17 (or docker for the fallback), `curl`, `lsof`. Xcode/`xcrun simctl` only for `--ios`, Android SDK only for `--android`, `minio` or docker only for `--s3`, docker only for `--redis`. Missing tool → one line saying how to install it, exit before anything starts.
2. **Already running?** If `.data/run/dev-up.pid` is alive, print its URLs and offer: attach (menu), restart, or abort. Never start a second copy.
3. **Ports**: block `DEV_PORT_BASE` (default derived from the slug: `20000 + crc32(slug) % 999 * 10`): API +0, web +1, Postgres +2, Metro +3, MinIO +4, Redis +5, hooks +6…+9. A port held by **this project's** stale process (PID file + command match) is reclaimed; a port held by anything else is never touched — move to the next free block and say so. Never use 3000–3010 or 8080.
4. **Env**: create `.env.dev` from `.env.dev.example` if missing and continue (every key is optional in development). Generate `backend/.env`, `web/.env.local`, `mobile/.env` with `scripts/dev-up/env.mjs` from `scaffold.json` (`dev` values, else defaults) + `.env.dev` overrides + computed values (ports, URLs, LAN IP, absolute `.data/` paths, `AUTH_DEV_BYPASS=true`, `*_AUTH_DEV=true`, `ADMIN_EMAILS` with the dev admin). Back up a hand-edited runtime file once (`.dev-up-backup`) before the first overwrite. Never `source` `.env.dev` into the shell (secrets would leak to every child). Validate: Clerk publishable and secret keys are both set or both empty and from the same instance; `pk_live`/`sk_live` keys refuse to run in development; required-always vars present. Print which providers are live vs dev fallback.
5. **Dependencies**: `pnpm install --prefer-offline` when `node_modules` is missing or older than any `package.json`/lockfile (then touch it); `go mod download` when `go.sum` changed. Install output goes to `logs/install.log`; on failure show its tail.
6. **Generated assets**: `pnpm tokens` when palette sources are newer than the generated CSS; app icons when the logo is newer than them.
7. **Postgres**: a private cluster in `.data/postgres` (`initdb --auth=trust -U <slug>`), TCP only on base+2 (`-c unix_socket_directories=''` — macOS caps socket paths at 103 bytes), dev settings (`synchronous_commit=off`, small WAL). Fall back to `docker compose up -d db` (same port, same URL) when no binaries. Create `<slug>_dev` if missing. `--reset-db` drops it (after confirmation). `--clone-from`: `pg_dump -Fc --no-owner --no-acl` with the highest available client major, `pg_restore`, then run `scripts/dev-up/scrub.sql` (point webhooks, emails, push tokens and provider ids away from production). `--s3`: MinIO on base+4 (a tracked `minio server` with data in `.data/minio`; `docker compose --profile s3` when the binary is missing), bucket `<slug>-dev` created over SigV4 curl.
8. **Backend**: `go build -o .data/bin/server ./cmd/server` and `./cmd/seed` (build log on failure). With `--s3`, apply the production bucket policy (`server -bucket-policy`: public purposes public-read, the rest private) so dev fails where production would. Run `seed` (it migrates first; idempotent) unless `--no-seed`/`--clone-from`, then start the server; wait for `/readyz` (180 s, stop early if the process died, show the log tail on failure).
9. **Feature hooks** (`scripts/dev-up/hooks/<name>.sh`, sourced once after ports are picked; features ship them through feature.json):
   - `hook_<name>_prepare` runs before env files are written and before Postgres and the backend start: it may `start_bg`/`wait_for` its own services, take a port with `hook_port <6-9>` (offsets reserved for hooks), and set values with `hook_env backend|web|mobile KEY VALUE` (highest precedence, passed through a 0600 file, never argv). A failing prepare stops the boot and tears down what started.
   - `hook_<name>_started` runs once the backend is ready: it may add menu items with `menu_add <key> <label> <fn>` and query with `psql_dev`.
   - Hook processes get PID files and are torn down like every other service. Details: `scripts/dev-up/hooks/README.md`.
10. **Web**: `next dev -p <base+1>` (or with `--web-prod`, a production build started in the background at step 5 and served with `next start`, falling back to dev if the build fails). Wait for HTTP 200.
11. **Mobile**: Expo on base+3, stdin `</dev/null` so it can't steal menu keys; `--host localhost` by default (simulators time out on LAN URLs behind the macOS firewall), `--lan` writes the LAN IP into `EXPO_PUBLIC_API_URL`, `--tunnel` uses Expo's tunnel. `--ios`: boot a simulator, open the installed dev build via its scheme (`<scheme>://expo-development-client/?url=…`), or offer to build it once with `expo run:ios` (logs to `logs/ios-build.log`); `--android` likewise with the first AVD.
12. **Summary**: URLs (API, web, admin, Metro), dev identities and how to sign in, log paths, which providers are live, then the menu.

## Process management

- `start_bg <name> <dir> <cmd…>`: background with `</dev/null`, output to `logs/<name>.log`, PID to `.data/run/<name>.pid`.
- `kill_tree <sig> <pid>`: leaves-first through `pgrep -P` (reaches what `go run`, `next`, `npx`, `expo` spawn).
- `cleanup` (trap INT TERM EXIT, idempotent, armed only after the first service starts): TERM every tree it started, wait up to 3 s, KILL survivors, remove PID files. It only ever signals PIDs it recorded. Logs and `.data/` survive.
- Log tailing from the menu survives Ctrl-C (a no-op INT handler while `tail` runs, then the cleanup trap is restored).
- No tmux: one foreground script with a resident menu.

## Resident menu

`w` open web · `a` open admin · `i` iOS simulator · `d` Android emulator · `l` tail a log (pick a service) · `r` rebuild + restart backend · `R` restart web · `s` re-run seed · `g` grant admin by email (search, confirm) · `t` print a dev token for any user (for curl) · `m` open the mail sink folder · `p` psql shell · `c` `scaffold doctor` + stale-binary check · `q` quit (tears down). Feature hooks add their items.

## Helper scripts

`scripts/` holds one folder per convenience script, each with exactly one script and one honest `.md` (usage + what it does). `scripts/lib.sh` is shared plumbing, sourced only, and includes `require_local_stack` (refuses to run unless the API URL is localhost and `/readyz` answers). Ship at least:
- `scripts/preflight/`: read-only health — services answer; the running backend started after the newest commit touching `backend/` and after uncommitted backend changes (stale-binary check); generated env files are newer than `.env.dev` and `scaffold.json`; each failure names the fix.
- `scripts/restart-backend/`: rebuild and restart from the generated `backend/.env` (never a hand-copied env list, which drifts).
- `scripts/reset-db/`: drop, recreate, migrate, seed (asks first).

If a script's `.md` drifts from what it does, fix the `.md`; if the script broke, fix the script. No test harnesses or "run all checks" runners.

## Rules

- Zero keys must work: dev auth, local disk uploads, mail sink, memory realtime, provider fallbacks.
- Never stop or kill processes this run didn't start, and never touch another project's ports or data.
- Every wait has a timeout, an early exit when the process dies, and a log tail on failure.
- Idempotent: re-running reuses the cluster, database, deps and builds when current.
- Keep `dev-up.md` (beside the script) accurate: flags, ports, menu, what it writes.
