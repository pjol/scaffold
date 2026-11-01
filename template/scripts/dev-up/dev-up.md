# dev-up

`scripts/dev-up/dev-up.sh` boots the whole Scaffold App stack with one command, from a fresh clone, with zero third-party keys: Postgres, the Go API, the Next.js web app and Expo. It then stays in the foreground with a menu. Ctrl-C or `q` stops exactly what it started; `logs/` and `.data/` are kept. Every change is tested on this stack.

```sh
scripts/dev-up/dev-up.sh [flags]   # boot everything, then the menu
scripts/dev-up/dev-up.sh menu      # the menu against a running stack (boots nothing; q leaves it running)
scripts/dev-up/dev-up.sh status    # URLs, ports, PIDs, health; exit 0 when dev-up runs and the API is ready
scripts/dev-up/dev-up.sh stop      # stop the stack this project's dev-up started (from its PID files)
```

| Flag | Effect |
|---|---|
| `--no-backend` `--no-web` `--no-mobile` | skip a surface. A missing `backend/`, `web/` or `mobile/` is skipped anyway. |
| `--reset-db` | drop and recreate `scaffoldapp_dev` (asks first), then migrate + seed |
| `--no-seed` | skip `cmd/seed`; the server still migrates at boot |
| `--clone-from <postgres-url>` | `pg_dump` a remote database (read-only on the source; highest local `pg_dump` major), restore it instead of seeding, then run `scrub.sql`. Asks first. Leave the password out of the URL and use `PGPASSWORD`/`~/.pgpass` to keep it out of shell history. |
| `--web-prod` | `next build` in the background during step 5, then `next start`; falls back to `next dev` if the build fails |
| `--lan` | Expo `--host lan`; writes the LAN IP into `EXPO_PUBLIC_API_URL`, `PUBLIC_API_URL` and the CORS list (physical devices on your network) |
| `--tunnel` | Expo `--host tunnel` (needs `@expo/ngrok`). The API stays on the LAN IP unless you add `--public`. |
| `--ios` / `--android` | open the simulator / emulator once Metro is up (see Mobile) |
| `--s3` | MinIO in docker on base+4, bucket `scaffoldapp-dev` (objects public-read), backend pointed at it |
| `--redis` | Redis in docker on base+5, `REDIS_URL` set |
| `--public` | `cloudflared` quick tunnel to the API; prints `https://….trycloudflare.com` for webhooks (Clerk, Stripe). With `--tunnel` it also becomes the device API URL. |
| `--yes` | answer yes to confirmations; required for `--reset-db`/`--clone-from` without a terminal |
| `-h`, `--help` | the script's header comment |

Flags are parsed before `.env.dev` is read and win over it. Unknown flags are errors.

## Without a terminal

When stdin is not a terminal (an agent, CI, `… </dev/null &`), dev-up boots, prints the summary, skips the menu and stays resident until `dev-up.sh stop` or a signal, logging a line if the backend dies. Use `status` to poll and `stop` to tear down. A second boot while one runs refuses (exit 1); interactively it offers attach, restart or abort.

## Ports

One block of ten per project. `DEV_PORT_BASE` in `.env.dev`, else derived from the slug: `20000 + crc32(slug) % 999 * 10` (Scaffold App: 21970).

| Offset | Service |
|---|---|
| +0 | API |
| +1 | web |
| +2 | Postgres |
| +3 | Metro |
| +4 | MinIO (`--s3`) |
| +5 | Redis (`--redis`) |
| +6…+9 | free for feature hooks (`hook_port 6`) |

Only the ports this boot needs are checked. Processes this project's dev-up left running (a crashed or killed run) are found through `.data/run/*.pid` and stopped first. A port held by anything else is never touched: the block moves up by ten and the summary says which base to pin. 3000–3010 and 8080 are never used.

## What it writes

| Path | What |
|---|---|
| `.env.dev` | created from `.env.dev.example` on first run; yours to edit (gitignored) |
| `backend/.env`, `web/.env.local`, `mobile/.env` | generated on every boot by `env.mjs` (below); one per surface directory that exists |
| `.data/env-backup/*.dev-up-backup` | a copy of a runtime env file that was edited by hand, taken before overwriting it |
| `.data/postgres` | the private cluster |
| `.data/bin/server`, `.data/bin/seed` | backend binaries |
| `.data/run/*.pid` | one per service: pid, start time, match string, command, settled command |
| `.data/run/stack.env` | ports, URLs, Postgres mode for `status`, the menu and `scripts/*` |
| `.data/uploads`, `.data/mail` | local uploads; the mail sink (one Markdown file per email) |
| `.data/dumps` | `--clone-from` dumps |
| `logs/*.log` | one per service and step (`backend`, `seed`, `backend-build`, `postgres`, `install`, `web`, `web-build`, `metro`, `cloudflared`, `docker`, `clone`, `ios-build`, `android-build`); appended across restarts, rotated past 5 MB |

## Env generation (`env.mjs`)

`.env.dev` is parsed by `scripts/dev-up/env.mjs` (Node, no dependencies), never `source`d, so a secret reaches only the runtime file of the tier that declares it. For each variable in `scaffold.json` → `env`, lowest to highest:

1. the entry's `dev` value, else its `default`;
2. values dev-up computes and you may override: `PUBLIC_API_URL`, `PUBLIC_WEB_URL`, `CORS_ALLOWED_ORIGINS`, `NEXT_PUBLIC_API_URL`, `EXPO_PUBLIC_API_URL`, `AUTH_DEV_BYPASS=true`, `NEXT_PUBLIC_AUTH_DEV=true`, `EXPO_PUBLIC_AUTH_DEV=true`;
3. `X` in `.env.dev` fills `NEXT_PUBLIC_X` and `EXPO_PUBLIC_X` where those exist. Paste `CLERK_PUBLISHABLE_KEY` once and web and mobile both get it. `AUTH_DEV=false` turns both pickers off;
4. `X` in `.env.dev` overrides every tier that declares `X`. A blank value means not set;
5. `ADMIN_EMAILS` always keeps `admin@scaffoldapp.test` (union with yours);
6. values dev-up owns, where `.env.dev` is ignored with a note: `PORT`, `DATABASE_URL`, `APP_ENV`/`NEXT_PUBLIC_APP_ENV`/`EXPO_PUBLIC_APP_ENV=development`, and whatever a flag sets (`--lan`, `--public` with `--tunnel`, `--s3`, `--redis`);
7. `hook_env <tier> KEY VALUE` from a feature hook's prepare phase, for that tier only. An undeclared key is still written, but warns.

Values starting with `.data/` become absolute paths. A `.env.dev` key that no tier declares gets a warning. `dev`-tier entries (`DEV_PORT_BASE`, `DEV_LAN_IP`, `DEV_PG_BIN`, `CLERK_PUBLISHABLE_KEY`) are read by dev-up only.

Validation stops the boot before anything starts, with nothing written, when:

- Clerk keys are half set;
- the web and mobile publishable keys differ;
- a key is `pk_live_` or `sk_live_`;
- the keys come from different instances. This is checked online, once per key pair, by comparing the instance JWKS; offline it is only a warning;
- `AUTH_DEV_BYPASS=false` has no secret key;
- a `required: always` variable is empty.

The summary prints each provider as live or dev fallback: auth, storage, mail, realtime, and each feature group with secrets.

A process started with the generated file also gets `env -u` for every key in it, so a stray `export DATABASE_URL=…` in your shell can't override the file. `node scripts/dev-up/env.mjs shell` prints the computed map as `KEY=value` lines; secrets appear only as `has_KEY=yes|no`.

## Boot order

1. **Tools**: Node at least the `.nvmrc` major (nvm's matching version is used when installed; backend-only boots accept Node 18+ with a warning). pnpm 10 (web/mobile), Go at least `backend/go.mod`, `curl`, `lsof`. Postgres binaries come from `DEV_PG_BIN`, `/Library/PostgreSQL/17/bin`, Homebrew, Postgres.app or `PATH` (major 17+, or exactly an existing cluster's version), else Docker. Flag tools only when asked: Xcode for `--ios`, the Android SDK for `--android`, `cloudflared` for `--public`, `pg_dump` for `--clone-from`, Docker for `--s3`/`--redis`. Anything missing prints how to install it and exits before anything starts.
2. **Already running?**: refuse (no terminal), or offer attach, restart or abort. Confirm `--reset-db`/`--clone-from` here, up front.
3. **Ports**: reclaim this project's leftovers, then pick the block (above).
4. **Env**: LAN IP (`--lan`/`--tunnel`), cloudflared (`--public`), then the hooks are sourced and each `hook_<name>_prepare` runs (it may start services and call `hook_env`). A failing prepare stops the boot. Then `env.mjs write`.
5. **Dependencies**: `pnpm install --prefer-offline` when `node_modules` is missing or older than any `package.json`, `pnpm-lock.yaml` or `pnpm-workspace.yaml`, then `touch node_modules`. `go mod download` when `backend/go.sum` changed. Output goes to `logs/install.log`. `--web-prod` starts its build here.
6. **Generated assets**: `pnpm tokens` when a `packages/tokens/src/**/*.ts` file is newer than `src/generated/tokens.css` (needs `node_modules`). `pnpm icons` when the root `package.json` defines it and `design/logo.svg` (or `mobile/assets/**/logo.svg`) is newer than the app icon. No `icons` script ships yet.
7. **Postgres**: on first run, `initdb --auth=trust -U scaffoldapp --locale=C` into `.data/postgres`. Then `pg_ctl start` on 127.0.0.1:base+2, TCP only (`unix_socket_directories=''`: macOS caps socket paths at 103 bytes), with `synchronous_commit=off`, `wal_level=minimal` and a small WAL. A running cluster is reused, or restarted if it is on another port. Without binaries: `docker compose up -d db`, same port and URL. Creates `scaffoldapp_dev`. `--reset-db` drops and recreates it (`WITH (FORCE)`). `--clone-from` dumps, restores, then runs `scrub.sql` in one transaction; if the scrub fails the clone is dropped. Then MinIO/Redis for `--s3`/`--redis`.
8. **Backend**: `go build -o .data/bin/ ./cmd/server ./cmd/seed`, run `seed` (it migrates first, then upserts the dev identities; skipped by `--no-seed`/`--clone-from`), start the server, wait up to 180 s for `/readyz` (200). It stops early if the process dies, showing the log tail. If the backend fails, the boot fails and everything started so far is stopped.
9. **Feature hooks**: each `hook_<name>_started` runs (services that need the API or the database, menu items). See `hooks/README.md`.
10. **Web**: `next dev -p base+1`, or `next start` after the `--web-prod` build. Waits up to 180 s for any HTTP answer below 500. A web failure warns and leaves the rest running (`R` retries).
11. **Mobile**: `expo start --port base+3 --host localhost|lan|tunnel` with stdin `/dev/null`. Waits for `/status`. Simulators use `localhost`: the macOS firewall times out LAN URLs.
12. **Summary**: URLs, how to sign in, a curl line, the mail sink, log paths, providers, then the menu.

## Mobile

- `--ios` (or `i`): boots the first available iPhone simulator if none is booted. If `com.example.scaffoldapp` is installed, it opens `scaffoldapp://expo-development-client/?url=http://localhost:<metro>`. Otherwise it offers to build it once with `expo run:ios --no-bundler` (`logs/ios-build.log`); without a terminal it only prints that command.
- `--android` (or `d`): uses a connected device, or starts the first AVD (stopped with the stack). It runs `adb reverse` for Metro and the API, so `localhost` works on the emulator. Then it opens the dev build the same way, or offers `expo run:android --no-bundler`.
- Physical devices: `--lan` (same network), or `--tunnel --public` (anywhere). Scan the QR code in `logs/metro.log` with the installed dev build.

## The menu

| Key | Action |
|---|---|
| `w` / `a` | open web / `/admin` |
| `i` / `d` | iOS simulator / Android emulator (above) |
| `l` | tail one log, or every running service's; Ctrl-C returns to the menu |
| `r` | rebuild + restart the backend |
| `R` | restart web (prod mode rebuilds first) |
| `s` | rebuild + re-run `cmd/seed` |
| `g` | grant admin: search users, pick one, show every account with that email, confirm, then `UPDATE users SET admin_role='admin' WHERE lower(email)=lower(:'e')` (psql `-v`, injection-safe) |
| `t` | print `dev:<subject>:<email>` and a curl line for a dev user (subject = `auth_subject` without `dev_`). Clerk users (`user_…`) can't get one. Any new `dev:<name>:<name>@scaffoldapp.test` creates that user on first use. |
| `m` | open the mail sink folder |
| `p` | psql on `scaffoldapp_dev` |
| `c` | `scaffold doctor` (if on `PATH`) + `scripts/preflight/preflight.sh` (stale binary, env freshness) |
| `q` | quit: tears the stack down (in `dev-up.sh menu`: leaves it running) |

Feature hooks add their own keys with `menu_add` in `hook_<name>_started`.

## Dev identities

`cmd/seed` creates `admin@scaffoldapp.test` (admin), `sam@`, `jordan@` and `priya@scaffoldapp.test`, all with the current legal versions accepted. Web and mobile `/sign-in` show the dev picker while no Clerk keys are set. curl: `-H 'Authorization: Bearer dev:sam:sam@scaffoldapp.test'`.

## Process rules

- Every service goes through `start_bg` (`scripts/lib.sh`). It gets stdin `/dev/null` and its own session (via `setsid` or perl), so a Ctrl-C at the menu reaches only dev-up. Output goes to `logs/<name>.log` and the record to `.data/run/<name>.pid`.
- Teardown (Ctrl-C, `q`, `stop`, closing the terminal) TERMs each recorded tree: descendants plus the service's process group, leaves first. It waits up to 3 s, KILLs survivors, stops the cluster with `pg_ctl stop -m fast` and stops the docker services it started.
- A pid is signalled only while its start time and command still match the record, so a recycled pid is never hit. The helper scripts write the same records, so a backend restarted by `scripts/restart-backend` is still stopped by dev-up.
- A process this project didn't start is never signalled. A port held by one moves the block.

## Known limits

- The web and mobile process handling (start, the `--web-prod` build wait and fallback, waits, early-death detection, restart, teardown) was exercised with a stub `pnpm`. Real `next` and `expo` runs haven't happened yet: those apps were being built at the same time.
- The simulator/emulator flows, `--s3`/`--redis`/`--public`/`--clone-from` and the docker fallback are written but haven't run on this machine.
- `scrub.sql` ships commented out: a clone scrubs nothing until you uncomment the statements that fit.
