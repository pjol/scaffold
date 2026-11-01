#!/usr/bin/env bash
#
# dev-up: the Scaffold App development stack in one command, with zero
# third-party keys. Boots Postgres (a private cluster in .data/postgres), the Go
# API, the Next.js web app and Expo, then stays in the foreground with a menu.
# Ctrl-C (or q) stops exactly what it started; logs/ and .data/ are kept.
#
# Usage:
#   scripts/dev-up/dev-up.sh [flags]   boot everything, then the menu
#   scripts/dev-up/dev-up.sh menu      the menu against a running stack (boots nothing)
#   scripts/dev-up/dev-up.sh status    URLs, ports, PIDs and health; exit 0 when up
#   scripts/dev-up/dev-up.sh stop      stop the stack this project's dev-up started
#
# Flags:
#   --no-backend --no-web --no-mobile  skip a surface (missing directories are skipped anyway)
#   --reset-db           drop and recreate the dev database (asks), then migrate + seed
#   --no-seed            skip cmd/seed (the server still migrates at boot)
#   --clone-from <url>   restore a remote Postgres instead of seeding, then run scrub.sql
#   --web-prod           serve a production build (built in the background) instead of next dev
#   --lan | --tunnel     Expo for physical devices (LAN IP / Expo tunnel); default localhost
#   --ios | --android    open the simulator / emulator once Metro is up
#   --s3 | --redis       MinIO / Redis in docker on the project's ports, wired into the backend
#   --public             expose the API through a cloudflared quick tunnel (webhooks)
#   --yes                answer yes to confirmations (required for --reset-db without a terminal)
#   -h | --help          this text
#
# Ports: one block per project, DEV_PORT_BASE in .env.dev or derived from the
# slug: API +0, web +1, Postgres +2, Metro +3, MinIO +4, Redis +5. A port held
# by anything this project did not start is never touched; the block moves.
#
# Writes backend/.env, web/.env.local and mobile/.env (scaffold.json + .env.dev
# via env.mjs), .data/ (postgres, bin, run/*.pid, uploads, mail) and logs/.
# Without a terminal (agents, CI) it skips the menu and stays up until
# `dev-up.sh stop` or a signal. Reference: scripts/dev-up/dev-up.md

set -uo pipefail

# shellcheck source=../lib.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib.sh"

usage() { awk 'NR > 1 && !/^#/ { exit } NR > 1 { sub(/^# ?/, ""); print }' "$0"; }

# ---- flags (parsed before .env.dev is read; they win over it) ---------------

CMD="boot"
RUN_BACKEND=1 RUN_WEB=1 RUN_MOBILE=1
RESET_DB=0 SEED=1 CLONE_FROM="" WEB_PROD=0 EXPO_HOST="localhost"
OPEN_IOS=0 OPEN_ANDROID=0 WANT_S3=0 WANT_REDIS=0 PUBLIC=0 ASSUME_YES=0
if [[ $# -gt 0 ]]; then
  case "$1" in menu|status|stop) CMD="$1"; shift ;; esac
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-backend) RUN_BACKEND=0 ;;
    --no-web) RUN_WEB=0 ;;
    --no-mobile) RUN_MOBILE=0 ;;
    --reset-db) RESET_DB=1 ;;
    --no-seed) SEED=0 ;;
    --clone-from)
      [[ $# -ge 2 && -n "$2" ]] || die "--clone-from needs a postgres:// URL"
      CLONE_FROM="$2"; shift ;;
    --clone-from=*) CLONE_FROM="${1#--clone-from=}" ;;
    --web-prod) WEB_PROD=1 ;;
    --lan) [[ "$EXPO_HOST" == "tunnel" ]] && die "--lan and --tunnel are exclusive"; EXPO_HOST="lan" ;;
    --tunnel) [[ "$EXPO_HOST" == "lan" ]] && die "--lan and --tunnel are exclusive"; EXPO_HOST="tunnel" ;;
    --ios) OPEN_IOS=1 ;;
    --android) OPEN_ANDROID=1 ;;
    --s3) WANT_S3=1 ;;
    --redis) WANT_REDIS=1 ;;
    --public) PUBLIC=1 ;;
    -y|--yes) ASSUME_YES=1 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
  shift
done
if [[ -n "$CLONE_FROM" ]]; then
  case "$CLONE_FROM" in postgres://*|postgresql://*) ;; *) die "--clone-from expects a postgres:// URL" ;; esac
fi

INTERACTIVE=0
[[ -t 0 ]] && INTERACTIVE=1
DEVUP_MODE="boot"
ARMED=0 CLEANED=0 PG_STOP_ON_EXIT=0
PORT_BASE="" API_PORT="" WEB_PORT="" PG_PORT="" METRO_PORT="" MINIO_PORT="" REDIS_PORT=""
API_URL="" WEB_URL="" PUBLIC_URL="" LAN_IP="" PG_MODE="" PG_BIN="" DATABASE_URL="" MAIL_SINK_DIR=""
WEB_MODE="dev" WEB_BUILD_PID=""
ON_START_BG=arm_cleanup

step() { echo; c_blue "[$1/12] $2"; }

redact_url() { printf '%s' "$1" | sed -E 's#(://[^:@/]+):[^@/]*@#\1:***@#'; }

with_timeout() {
  local s="$1"
  shift
  if command -v perl >/dev/null 2>&1; then
    perl -e 'alarm shift; exec { $ARGV[0] } @ARGV or exit 127' "$s" "$@"
  else
    "$@"
  fi
}

ver_ge() { # ver_ge <a> <b> — dotted versions, a >= b
  local IFS=.
  local -a A=($1) B=($2)
  local i x y
  for i in 0 1 2; do
    x="${A[$i]:-0}"; y="${B[$i]:-0}"
    x="${x%%[!0-9]*}"; y="${y%%[!0-9]*}"
    [[ "$((10#${x:-0}))" -gt "$((10#${y:-0}))" ]] && return 0
    [[ "$((10#${x:-0}))" -lt "$((10#${y:-0}))" ]] && return 1
  done
  return 0
}

node_major() { node --version 2>/dev/null | sed -nE 's/^v([0-9]+).*/\1/p'; }

nvm_use() {
  local nvm_sh="${NVM_DIR:-$HOME/.nvm}/nvm.sh" want="$1" rc
  [[ -s "$nvm_sh" ]] || return 1
  set +u
  # shellcheck disable=SC1090
  . "$nvm_sh" --no-use >/dev/null 2>&1
  nvm use "$want" >/dev/null 2>&1 || nvm use "${want%%.*}" >/dev/null 2>&1
  rc=$?
  set -u
  return $rc
}

open_url() {
  if command -v open >/dev/null 2>&1; then open "$1"
  elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$1" >/dev/null 2>&1
  else echo "  $1"; fi
}

docker_ready() {
  command -v docker >/dev/null 2>&1 || return 1
  docker compose version >/dev/null 2>&1 || return 1
  with_timeout 10 docker info >/dev/null 2>&1
}

# ---- teardown ---------------------------------------------------------------

arm_cleanup() { ARMED=1; }

docker_stop_ours() {
  local f="$RUN_DIR/docker.services" svcs
  [[ -f "$f" ]] || return 0
  svcs="$(sort -u "$f" | tr '\n' ' ')"
  if [[ -n "${svcs// /}" ]] && docker_ready; then
    # shellcheck disable=SC2086
    compose stop $svcs >>"$LOG_DIR/docker.log" 2>&1 && c_dim "  docker: stopped $svcs"
  fi
  rm -f "$f"
}

pg_running() { [[ -n "${PG_BIN:-}" && -f "$PGDATA/PG_VERSION" ]] && "$PG_BIN/pg_ctl" -D "$PGDATA" status >/dev/null 2>&1; }

pg_stop_cluster() {
  pg_running || return 0
  "$PG_BIN/pg_ctl" -D "$PGDATA" stop -m fast -w -t 30 >/dev/null 2>&1 && c_dim "  postgres stopped (data kept in .data/postgres)"
}

cleanup() {
  local status="${1:-0}"
  [[ "$CLEANED" -eq 1 ]] && return
  CLEANED=1
  trap - INT TERM HUP EXIT
  if [[ "$ARMED" -eq 1 ]]; then
    echo
    c_yellow "Shutting down…"
    stop_all_services
    docker_stop_ours
    [[ "$PG_STOP_ON_EXIT" -eq 1 && "$PG_MODE" == "private" ]] && pg_stop_cluster
    c_green "Stopped. logs/ and .data/ are kept."
  fi
  rm -f "$RUN_DIR/dev-up.pid"
  exit "$status"
}

# ---- step 1: tools ----------------------------------------------------------

detect_surfaces() {
  if [[ "$RUN_BACKEND" -eq 1 && ! -f "$ROOT/backend/go.mod" ]]; then RUN_BACKEND=0; c_dim "  backend/ not found: skipping the API"; fi
  if [[ "$RUN_WEB" -eq 1 && ! -f "$ROOT/web/package.json" ]]; then RUN_WEB=0; c_dim "  web/ not found: skipping web"; fi
  if [[ "$RUN_MOBILE" -eq 1 && ! -f "$ROOT/mobile/package.json" ]]; then RUN_MOBILE=0; c_dim "  mobile/ not found: skipping mobile"; fi
  [[ "$RUN_WEB" -eq 0 && "$WEB_PROD" -eq 1 ]] && WEB_PROD=0
  if [[ "$RUN_MOBILE" -eq 0 ]]; then OPEN_IOS=0; OPEN_ANDROID=0; fi
  return 0
}

check_node() {
  local want have nvmrc=""
  [[ -f "$ROOT/.nvmrc" ]] && nvmrc="$(tr -d '[:space:]' <"$ROOT/.nvmrc")"
  want="$(printf '%s' "$nvmrc" | sed -nE 's/^v?([0-9]+).*/\1/p')"
  want="${want:-22}"
  have="$(node_major)"
  if [[ -z "$have" || "$have" -lt "$want" ]] && nvm_use "${nvmrc:-$want}"; then
    have="$(node_major)"
    c_dim "  using nvm's node $(node --version) (.nvmrc)"
  fi
  [[ -n "$have" ]] || die "  node not found: install Node $want (nvm install $want, or https://nodejs.org)"
  if [[ "$have" -lt "$want" ]]; then
    if [[ "$RUN_WEB" -eq 1 || "$RUN_MOBILE" -eq 1 ]]; then
      die "  node $(node --version) is older than .nvmrc ($nvmrc): run nvm install && nvm use"
    fi
    [[ "$have" -ge 18 ]] || die "  node $(node --version) is too old even for env.mjs: run nvm install && nvm use"
    c_yellow "  node $(node --version) is older than .nvmrc ($nvmrc): fine backend-only, run nvm use before web/mobile"
  fi
}

load_settings() {
  local out line k v
  if [[ ! -f "$ROOT/.env.dev" && -f "$ROOT/.env.dev.example" ]]; then
    cp "$ROOT/.env.dev.example" "$ROOT/.env.dev"
    chmod 600 "$ROOT/.env.dev" 2>/dev/null || true
    c_yellow "  created .env.dev from .env.dev.example (every key is optional in development)"
  fi
  out="$(node "$ROOT/scripts/dev-up/env.mjs" shell)" || die "  scripts/dev-up/env.mjs failed (is scaffold.json valid JSON?)"
  DEV_PORT_BASE="" DEV_LAN_IP="" DEV_PG_BIN="" DEFAULT_PORT_BASE=""
  while IFS= read -r line; do
    k="${line%%=*}"; v="${line#*=}"
    case "$k" in
      DEV_PORT_BASE|DEV_LAN_IP|DEV_PG_BIN|DEFAULT_PORT_BASE) printf -v "$k" '%s' "$v" ;;
    esac
  done <<<"$out"
}

resolve_postgres() {
  local want=""
  [[ -s "$PGDATA/PG_VERSION" ]] && want="$(tr -d '[:space:]' <"$PGDATA/PG_VERSION")"
  if [[ -n "$DEV_PG_BIN" ]]; then
    pg_find_bins "$DEV_PG_BIN" "$want" \
      || die "  DEV_PG_BIN=$DEV_PG_BIN has no initdb/pg_ctl/psql ${want:-17+} (fix it in .env.dev)"
    PG_MODE="private"
    return 0
  fi
  if pg_find_bins "" "$want"; then PG_MODE="private"; return 0; fi
  if [[ -n "$want" ]] && pg_find_bins "" ""; then
    die "  .data/postgres is a Postgres $want cluster but only Postgres $(pg_major "$PG_BIN/pg_ctl") is installed: set DEV_PG_BIN to $want binaries, or delete .data/postgres (its data is lost)"
  fi
  if docker_ready; then PG_MODE="docker"; return 0; fi
  c_red "  Postgres 17 not found: brew install postgresql@17 (or the EDB installer), or start Docker for the compose fallback"
  return 1
}

pg_find_dump() { # PG_DUMP/PG_RESTORE: the highest-major pair available
  local d m best=0 cand=()
  command -v pg_dump >/dev/null 2>&1 && cand+=("$(dirname "$(command -v pg_dump)")")
  [[ -n "${PG_BIN:-}" ]] && cand+=("$PG_BIN")
  for d in /Library/PostgreSQL/*/bin /opt/homebrew/opt/libpq/bin /opt/homebrew/opt/postgresql@*/bin \
    /usr/local/opt/libpq/bin /usr/local/opt/postgresql@*/bin /Applications/Postgres.app/Contents/Versions/*/bin \
    /usr/lib/postgresql/*/bin; do
    [[ -d "$d" ]] && cand+=("$d")
  done
  PG_DUMP="" PG_RESTORE=""
  for d in ${cand[@]+"${cand[@]}"}; do
    [[ -x "$d/pg_dump" && -x "$d/pg_restore" ]] || continue
    m="$(pg_major "$d/pg_dump")"
    if [[ -n "$m" && "$m" -gt "$best" ]]; then best="$m"; PG_DUMP="$d/pg_dump"; PG_RESTORE="$d/pg_restore"; fi
  done
  [[ -n "$PG_DUMP" ]]
}

android_sdk() { printf '%s' "${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"; }

check_tools() {
  local missing=0 v want versions=""
  command -v curl >/dev/null 2>&1 || { c_red "  curl not found: install curl"; missing=1; }
  command -v lsof >/dev/null 2>&1 || { c_red "  lsof not found: install lsof"; missing=1; }
  if [[ "$RUN_BACKEND" -eq 1 ]]; then
    want="$(sed -nE 's/^go ([0-9.]+).*/\1/p' "$ROOT/backend/go.mod" | head -n 1)"
    if ! command -v go >/dev/null 2>&1; then
      c_red "  go not found: install Go $want+ (https://go.dev/dl or brew install go)"; missing=1
    else
      v="$(go env GOVERSION 2>/dev/null | sed 's/^go//')"
      ver_ge "$v" "$want" || { c_red "  go $v is older than backend/go.mod ($want): upgrade Go"; missing=1; }
      versions="go $v"
    fi
  fi
  if [[ "$RUN_WEB" -eq 1 || "$RUN_MOBILE" -eq 1 ]]; then
    if ! command -v pnpm >/dev/null 2>&1; then
      c_red "  pnpm not found: corepack enable pnpm (pnpm 10)"; missing=1
    else
      v="$(pnpm --version 2>/dev/null)"
      [[ "${v%%.*}" -ge 10 ]] 2>/dev/null || { c_red "  pnpm $v is older than 10: corepack enable pnpm && corepack prepare pnpm@10 --activate"; missing=1; }
      versions="$versions · pnpm $v"
    fi
  fi
  versions="$versions · node $(node --version)"
  resolve_postgres || missing=1
  if [[ "$PG_MODE" == "private" ]]; then versions="$versions · postgres $(pg_major "$PG_BIN/pg_ctl") ($PG_BIN)"
  elif [[ "$PG_MODE" == "docker" ]]; then versions="$versions · postgres in docker"; fi
  if [[ "$WANT_S3" -eq 1 || "$WANT_REDIS" -eq 1 ]] && ! docker_ready; then
    c_red "  --s3/--redis need Docker running (install Docker Desktop, or start it)"; missing=1
  fi
  if [[ "$PUBLIC" -eq 1 ]] && ! command -v cloudflared >/dev/null 2>&1; then
    c_red "  --public needs cloudflared: brew install cloudflared"; missing=1
  fi
  if [[ "$OPEN_IOS" -eq 1 ]] && ! xcrun simctl help >/dev/null 2>&1; then
    c_red "  --ios needs Xcode and its simulators (xcode-select --install, then Xcode → Settings → Components)"; missing=1
  fi
  if [[ "$OPEN_ANDROID" -eq 1 && ! -x "$(android_sdk)/emulator/emulator" ]]; then
    c_red "  --android needs the Android SDK emulator (Android Studio → SDK Manager; or set ANDROID_HOME)"; missing=1
  fi
  if [[ -n "$CLONE_FROM" ]] && ! pg_find_dump; then
    c_red "  --clone-from needs pg_dump/pg_restore: brew install libpq (or postgresql@17)"; missing=1
  fi
  [[ "${#DETACH[@]}" -gt 0 ]] || c_yellow "  neither setsid nor perl found: a Ctrl-C in a log tail may also reach services"
  [[ "$missing" -eq 0 ]] || exit 1
  c_dim "  ${versions# · }"
}

# ---- step 2: already running? -----------------------------------------------

check_running() {
  pid_alive dev-up || { c_dim "  no dev-up running for this project"; return 0; }
  local other="$PID_PID" answer=""
  c_yellow "  dev-up is already running for this project (pid $other):"
  ( cmd_status ) || true
  if [[ "$INTERACTIVE" -eq 0 ]]; then
    die "  not starting a second copy: use dev-up.sh status | menu | stop"
  fi
  printf '  [a]ttach (menu)  [r]estart  [x] abort: '
  read -r answer || answer="x"
  case "$answer" in
    a|A) cmd_menu; exit $? ;;
    r|R) ( cmd_stop ) || die "  could not stop the running stack" ;;
    *) exit 1 ;;
  esac
}

# ---- step 3: ports ----------------------------------------------------------

block_forbidden() { # the block [base, base+5] touches 3000-3010 or 8080
  local b="$1"
  [[ $((b + 5)) -ge 3000 && "$b" -le 3010 ]] && return 0
  [[ "$b" -le 8080 && $((b + 5)) -ge 8080 ]] && return 0
  return 1
}

our_postmaster_pid() {
  pg_running || return 0
  head -n 1 "$PGDATA/postmaster.pid" 2>/dev/null
}

sweep_stale() {
  local n any=0
  for n in $(service_names); do
    pid_alive "$n" && any=1
  done
  if [[ "$any" -eq 1 ]]; then
    c_yellow "  reclaiming processes this project's dev-up left running:"
    stop_all_services
  else
    for n in $(service_names); do rm -f "$RUN_DIR/$n.pid"; done
  fi
  if [[ -f "$RUN_DIR/docker.services" ]]; then docker_stop_ours; fi
  return 0
}

allocate_ports() {
  local base orig tries=0 offs="2" off port pids pgpid ok why
  if [[ -n "$DEV_PORT_BASE" ]]; then
    [[ "$DEV_PORT_BASE" =~ ^[0-9]+$ ]] || die "  DEV_PORT_BASE must be a number (got $DEV_PORT_BASE)"
    base="$DEV_PORT_BASE"
  else
    base="$DEFAULT_PORT_BASE"
  fi
  [[ "$base" -ge 1024 && "$base" -le 65520 ]] || die "  DEV_PORT_BASE must be between 1024 and 65520 (got $base)"
  orig="$base"
  [[ "$RUN_BACKEND" -eq 1 ]] && offs="0 $offs"
  [[ "$RUN_WEB" -eq 1 ]] && offs="$offs 1"
  [[ "$RUN_MOBILE" -eq 1 ]] && offs="$offs 3"
  [[ "$WANT_S3" -eq 1 ]] && offs="$offs 4"
  [[ "$WANT_REDIS" -eq 1 ]] && offs="$offs 5"
  pgpid="$(our_postmaster_pid)"
  while [[ "$tries" -lt 50 ]]; do
    ok=1 why=""
    if block_forbidden "$base"; then
      ok=0; why="overlaps 3000-3010/8080"
    else
      for off in $offs; do
        port=$((base + off))
        pids="$(port_pids "$port")"
        [[ -z "$pids" ]] && continue
        if [[ -n "$pgpid" ]] && printf '%s\n' "$pids" | grep -qx "$pgpid"; then
          [[ "$off" -eq 2 ]] && continue
          c_yellow "  this project's Postgres holds :$port; stopping it to move it"
          pg_stop_cluster; pgpid=""
          wait_port_free "$port" 10 && continue
        fi
        ok=0; why="port $port is held by $(port_owner "$port")"
        break
      done
    fi
    [[ "$ok" -eq 1 ]] && break
    c_yellow "  block $base: $why, not ours; trying $((base + 10))"
    base=$((base + 10)); tries=$((tries + 1))
  done
  [[ "$ok" -eq 1 ]] || die "  no free port block after 50 tries"
  PORT_BASE="$base"
  API_PORT="$base" WEB_PORT=$((base + 1)) PG_PORT=$((base + 2)) METRO_PORT=$((base + 3))
  MINIO_PORT=$((base + 4)) REDIS_PORT=$((base + 5))
  API_URL="http://localhost:$API_PORT" WEB_URL="http://localhost:$WEB_PORT"
  DATABASE_URL="postgres://$DB_USER@127.0.0.1:$PG_PORT/$DB_NAME?sslmode=disable"
  if [[ "$base" != "$orig" ]]; then
    c_yellow "  using block $base instead of $orig (pin it with DEV_PORT_BASE=$base in .env.dev)"
  fi
  c_dim "  block $base: api $API_PORT · web $WEB_PORT · postgres $PG_PORT · metro $METRO_PORT · minio $MINIO_PORT · redis $REDIS_PORT"
}

# ---- step 4: env ------------------------------------------------------------

detect_lan_ip() {
  local ifc="" ip=""
  if [[ -n "$DEV_LAN_IP" ]]; then printf '%s' "$DEV_LAN_IP"; return; fi
  if command -v ipconfig >/dev/null 2>&1; then
    ifc="$(route -n get default 2>/dev/null | awk '/interface:/ { print $2 }')"
    [[ -n "$ifc" ]] && ip="$(ipconfig getifaddr "$ifc" 2>/dev/null)"
    [[ -z "$ip" ]] && ip="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null)"
  else
    ip="$(hostname -I 2>/dev/null | awk '{ print $1 }')"
  fi
  printf '%s' "$ip"
}

start_tunnel() {
  start_bg cloudflared "$ROOT" cloudflared tunnel --no-autoupdate --url "http://localhost:$API_PORT" || die "  could not start cloudflared"
  local pid="$LAST_PID" deadline=$((SECONDS + 45))
  while [[ $SECONDS -lt $deadline ]]; do
    PUBLIC_URL="$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG_DIR/cloudflared.log" 2>/dev/null | grep -v '//api\.' | head -n 1)"
    [[ -n "$PUBLIC_URL" ]] && break
    kill -0 "$pid" 2>/dev/null || break
    sleep 1
  done
  if [[ -z "$PUBLIC_URL" ]]; then
    tail -n 20 "$LOG_DIR/cloudflared.log" 2>/dev/null
    die "  cloudflared did not report a tunnel URL"
  fi
  c_green "  public URL $PUBLIC_URL → API"
}

generate_env() {
  local api_local="http://localhost:$API_PORT" web_local="http://localhost:$WEB_PORT" devices cors s3host
  if [[ "$EXPO_HOST" != "localhost" ]]; then
    LAN_IP="$(detect_lan_ip)"
    [[ -n "$LAN_IP" ]] || die "  could not detect a LAN IP: set DEV_LAN_IP in .env.dev"
    c_dim "  LAN IP $LAN_IP"
  fi
  [[ "$PUBLIC" -eq 1 ]] && start_tunnel
  load_hooks
  rm -f "$HOOK_ENV_FILE"
  run_hook_phase prepare
  cors="$web_local,http://127.0.0.1:$WEB_PORT"
  [[ -n "$LAN_IP" ]] && cors="$cors,http://$LAN_IP:$WEB_PORT"
  local args=(
    --force "PORT=$API_PORT" --force "DATABASE_URL=$DATABASE_URL"
    --force APP_ENV=development --force NEXT_PUBLIC_APP_ENV=development --force EXPO_PUBLIC_APP_ENV=development
    --set "PUBLIC_API_URL=$api_local" --set "PUBLIC_WEB_URL=$web_local" --set "CORS_ALLOWED_ORIGINS=$cors"
    --set AUTH_DEV_BYPASS=true --set NEXT_PUBLIC_AUTH_DEV=true --set EXPO_PUBLIC_AUTH_DEV=true
    --set "NEXT_PUBLIC_API_URL=$api_local" --set "EXPO_PUBLIC_API_URL=$api_local" --set "EXPO_PUBLIC_WEB_URL=$web_local"
    --add "ADMIN_EMAILS=$DEV_ADMIN_EMAIL"
  )
  # Devices need an API they can reach: the LAN IP, or the public tunnel when
  # Expo itself tunnels (--tunnel --public). Simulators use localhost.
  devices=""
  [[ -n "$LAN_IP" ]] && devices="http://$LAN_IP:$API_PORT"
  [[ "$EXPO_HOST" == "tunnel" && -n "$PUBLIC_URL" ]] && devices="$PUBLIC_URL"
  [[ -n "$devices" ]] && args+=(--force "PUBLIC_API_URL=$devices" --force "EXPO_PUBLIC_API_URL=$devices")
  [[ -n "$LAN_IP" ]] && args+=(--force "EXPO_PUBLIC_WEB_URL=http://$LAN_IP:$WEB_PORT")
  if [[ "$EXPO_HOST" == "tunnel" && -z "$PUBLIC_URL" ]]; then
    c_yellow "  --tunnel without --public: the device reaches the API over the LAN ($devices)"
  fi
  if [[ "$WANT_S3" -eq 1 ]]; then
    s3host="localhost"; [[ -n "$LAN_IP" ]] && s3host="$LAN_IP"
    args+=(--force "S3_BUCKET=$SLUG-dev" --force S3_REGION=us-east-1 --force "S3_ENDPOINT=http://$s3host:$MINIO_PORT"
      --force "S3_ACCESS_KEY_ID=$SLUG-dev" --force "S3_SECRET_ACCESS_KEY=$SLUG-dev-secret" --force S3_FORCE_PATH_STYLE=true
      --force "S3_PUBLIC_URL=http://$s3host:$MINIO_PORT/$SLUG-dev")
  fi
  [[ "$WANT_REDIS" -eq 1 ]] && args+=(--force "REDIS_URL=redis://127.0.0.1:$REDIS_PORT/0")
  [[ -s "$HOOK_ENV_FILE" ]] && args+=(--hook-env "$HOOK_ENV_FILE")
  node "$ROOT/scripts/dev-up/env.mjs" write "${args[@]}"
  local rc=$?
  rm -f "$HOOK_ENV_FILE"
  [[ "$rc" -eq 0 ]] || die "  fix .env.dev, then re-run"
  MAIL_SINK_DIR="$(env_get "$ROOT/backend/.env" MAIL_SINK_DIR)"
  [[ -n "$MAIL_SINK_DIR" ]] && mkdir -p "$MAIL_SINK_DIR"
  write_stack_env
}

write_stack_env() {
  local tmp="$RUN_DIR/stack.env.tmp"
  cat >"$tmp" <<EOF
PORT_BASE=$PORT_BASE
API_PORT=$API_PORT
WEB_PORT=$WEB_PORT
PG_PORT=$PG_PORT
METRO_PORT=$METRO_PORT
MINIO_PORT=$MINIO_PORT
REDIS_PORT=$REDIS_PORT
API_URL=$API_URL
WEB_URL=$WEB_URL
PUBLIC_URL=$PUBLIC_URL
LAN_IP=$LAN_IP
PG_MODE=$PG_MODE
PG_BIN=$PG_BIN
DATABASE_URL=$DATABASE_URL
RUN_BACKEND=$RUN_BACKEND
RUN_WEB=$RUN_WEB
RUN_MOBILE=$RUN_MOBILE
WEB_MODE=$WEB_MODE
EXPO_HOST=$EXPO_HOST
STARTED_AT=$(date +%s)
EOF
  mv "$tmp" "$RUN_DIR/stack.env"
}

# ---- step 5/6: dependencies and generated assets ----------------------------

deps_stale() {
  [[ -d "$ROOT/node_modules" ]] || return 0
  local f
  for f in "$ROOT/package.json" "$ROOT/pnpm-lock.yaml" "$ROOT/pnpm-workspace.yaml" \
    "$ROOT/web/package.json" "$ROOT/mobile/package.json" "$ROOT"/packages/*/package.json; do
    [[ -f "$f" && "$f" -nt "$ROOT/node_modules" ]] && return 0
  done
  return 1
}

install_deps() {
  if [[ "$RUN_WEB" -eq 1 || "$RUN_MOBILE" -eq 1 ]]; then
    if deps_stale; then
      c_yellow "  pnpm install (logs/install.log)…"
      if ! ( cd "$ROOT" && pnpm install --prefer-offline ) >"$LOG_DIR/install.log" 2>&1; then
        tail -n 30 "$LOG_DIR/install.log"
        die "  pnpm install failed (logs/install.log)"
      fi
      touch "$ROOT/node_modules"
      c_green "  node_modules installed"
    else
      c_dim "  node_modules current"
    fi
  fi
  if [[ "$RUN_BACKEND" -eq 1 && -f "$ROOT/backend/go.sum" ]]; then
    local sum stamp="$DATA_DIR/go.sum.cksum"
    sum="$(cksum <"$ROOT/backend/go.sum")"
    if [[ ! -f "$stamp" || "$(cat "$stamp")" != "$sum" ]]; then
      c_yellow "  go mod download (logs/install.log)…"
      if ! ( cd "$ROOT/backend" && go mod download ) >>"$LOG_DIR/install.log" 2>&1; then
        tail -n 30 "$LOG_DIR/install.log"
        die "  go mod download failed (logs/install.log)"
      fi
      printf '%s\n' "$sum" >"$stamp"
    else
      c_dim "  go modules current"
    fi
  fi
  if [[ "$WEB_PROD" -eq 1 ]]; then
    c_dim "  building web for production in the background (logs/web-build.log)"
    env_unset_args "$ROOT/web/.env.local"
    START_BG_WAITABLE=1 start_bg web-build "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} pnpm --filter "$WEB_PKG" exec next build \
      && WEB_BUILD_PID="$LAST_PID"
  fi
}

generated_assets() {
  local tok="$ROOT/packages/tokens" gen newer logo="" icon="" f did=0
  if [[ -d "$tok/src" && -d "$ROOT/node_modules" ]] && grep -q '"tokens"' "$ROOT/package.json" 2>/dev/null; then
    gen="$tok/src/generated/tokens.css"
    newer=""
    if [[ -f "$gen" ]]; then newer="$(find "$tok/src" -name '*.ts' -newer "$gen" 2>/dev/null | head -n 1)"; fi
    if [[ ! -f "$gen" || -n "$newer" ]]; then
      c_yellow "  pnpm tokens (palette sources changed)"
      if ( cd "$ROOT" && pnpm tokens ) >"$LOG_DIR/tokens.log" 2>&1; then
        touch "$gen" 2>/dev/null || true
      else
        c_yellow "  pnpm tokens failed (logs/tokens.log); continuing with the committed CSS"
      fi
    else
      c_dim "  tokens current"
    fi
    did=1
  fi
  if grep -qE '"icons"[[:space:]]*:' "$ROOT/package.json" 2>/dev/null; then
    for f in "$ROOT/design/logo.svg" "$ROOT/mobile/assets/logo.svg" "$ROOT/mobile/assets/images/logo.svg"; do
      [[ -f "$f" ]] && { logo="$f"; break; }
    done
    for f in "$ROOT/mobile/assets/images/icon.png" "$ROOT/mobile/assets/icon.png"; do
      [[ -f "$f" ]] && { icon="$f"; break; }
    done
    if [[ -n "$logo" && ( -z "$icon" || "$logo" -nt "$icon" ) ]]; then
      c_yellow "  pnpm icons (logo changed)"
      ( cd "$ROOT" && pnpm icons ) >"$LOG_DIR/icons.log" 2>&1 || c_yellow "  pnpm icons failed (logs/icons.log)"
      did=1
    fi
  fi
  [[ "$did" -eq 1 ]] || c_dim "  nothing to regenerate (tokens need node_modules; no icons script)"
  return 0
}

# ---- step 7: postgres -------------------------------------------------------

PG_OPTS() {
  printf '%s' "-p $PG_PORT -c listen_addresses=127.0.0.1 -c unix_socket_directories='' \
-c synchronous_commit=off -c wal_level=minimal -c max_wal_senders=0 -c max_wal_size=256MB \
-c min_wal_size=64MB -c log_min_messages=warning"
}

pg_private_up() {
  mkdir -p "$LOG_DIR"
  if [[ ! -s "$PGDATA/PG_VERSION" ]]; then
    if [[ -d "$PGDATA" && -n "$(ls -A "$PGDATA" 2>/dev/null)" ]]; then
      die "  .data/postgres exists but is not a Postgres cluster: move it away and re-run"
    fi
    c_yellow "  initialising .data/postgres (Postgres $(pg_major "$PG_BIN/initdb"), trust auth, role $DB_USER)"
    if ! "$PG_BIN/initdb" -D "$PGDATA" -U "$DB_USER" --auth=trust -E UTF8 --locale=C --no-instructions \
      >>"$LOG_DIR/postgres.log" 2>&1; then
      tail -n 20 "$LOG_DIR/postgres.log"
      die "  initdb failed (logs/postgres.log)"
    fi
  else
    c_dim "  reusing the cluster in .data/postgres"
  fi
  PG_STOP_ON_EXIT=1
  arm_cleanup
  if pg_running; then
    local cur
    cur="$(sed -n 4p "$PGDATA/postmaster.pid" 2>/dev/null)"
    if [[ "$cur" == "$PG_PORT" ]]; then
      c_dim "  already running on :$PG_PORT"
      return 0
    fi
    c_yellow "  running on :$cur; restarting it on :$PG_PORT"
    pg_stop_cluster
  fi
  if ! "$PG_BIN/pg_ctl" -D "$PGDATA" -l "$LOG_DIR/postgres.log" -w -t 60 -o "$(PG_OPTS)" start >/dev/null 2>&1; then
    tail -n 20 "$LOG_DIR/postgres.log"
    die "  postgres failed to start (logs/postgres.log)"
  fi
  c_green "  postgres on 127.0.0.1:$PG_PORT (TCP only)"
}

note_docker() { echo "$1" >>"$RUN_DIR/docker.services"; arm_cleanup; }

pg_docker_up() {
  c_yellow "  no Postgres 17 binaries: using docker compose (db) on :$PG_PORT"
  note_docker db
  if ! compose up -d db >>"$LOG_DIR/docker.log" 2>&1; then
    tail -n 20 "$LOG_DIR/docker.log"
    die "  docker compose up db failed (logs/docker.log)"
  fi
  local deadline=$((SECONDS + 90))
  until compose exec -T db pg_isready -q -U "$DB_USER" >/dev/null 2>&1; do
    [[ $SECONDS -lt $deadline ]] || { compose logs --tail 20 db; die "  the db container did not become ready in 90s"; }
    sleep 1
  done
  c_green "  postgres (docker) on 127.0.0.1:$PG_PORT"
}

ensure_db() {
  local exists
  exists="$(psql_admin -tA -v db="$DB_NAME" <<'SQL'
SELECT 1 FROM pg_database WHERE datname = :'db';
SQL
)"
  if [[ "$exists" == "1" ]]; then
    c_dim "  database $DB_NAME exists"
  else
    psql_admin -c "CREATE DATABASE \"$DB_NAME\" OWNER \"$DB_USER\"" >/dev/null || die "  could not create $DB_NAME"
    c_green "  created database $DB_NAME"
  fi
}

clone_db() {
  local safe dump
  safe="$(redact_url "$CLONE_FROM")"
  case "$CLONE_FROM" in
    *@127.0.0.1:"$PG_PORT"/*|*@localhost:"$PG_PORT"/*) die "  --clone-from points at this project's own dev database" ;;
  esac
  mkdir -p "$DATA_DIR/dumps"
  dump="$DATA_DIR/dumps/clone-$(date +%Y%m%d-%H%M%S).dump"
  c_yellow "  pg_dump $(pg_major "$PG_DUMP") from $safe (read-only on the source; logs/clone.log)…"
  if ! "$PG_DUMP" -Fc --no-owner --no-acl -f "$dump" -d "$CLONE_FROM" >>"$LOG_DIR/clone.log" 2>&1; then
    rm -f "$dump"
    tail -n 20 "$LOG_DIR/clone.log" | sed -E 's#(://[^:@/]+):[^@/]*@#\1:***@#g'
    die "  pg_dump failed (logs/clone.log)"
  fi
  db_recreate >/dev/null || die "  could not recreate $DB_NAME"
  c_yellow "  restoring into $DB_NAME…"
  if [[ "$PG_MODE" == "docker" ]]; then
    compose exec -T db pg_restore --no-owner --no-acl -U "$DB_USER" -d "$DB_NAME" <"$dump" >>"$LOG_DIR/clone.log" 2>&1
  else
    "$PG_RESTORE" --no-owner --no-acl -d "$DATABASE_URL" "$dump" >>"$LOG_DIR/clone.log" 2>&1
  fi || c_yellow "  pg_restore reported problems (often harmless: extensions, comments); see logs/clone.log"
  c_yellow "  scrubbing with scripts/dev-up/scrub.sql…"
  if ! psql_dev -1 -v api_url="http://localhost:$API_PORT" -v web_url="http://localhost:$WEB_PORT" \
    -v mail_domain="$DEV_EMAIL_DOMAIN" -f - <"$ROOT/scripts/dev-up/scrub.sql" >>"$LOG_DIR/clone.log" 2>&1; then
    psql_admin -c "DROP DATABASE IF EXISTS \"$DB_NAME\" WITH (FORCE)" >/dev/null 2>&1
    tail -n 15 "$LOG_DIR/clone.log"
    die "  scrub.sql failed, so the unscrubbed clone was dropped: fix scrub.sql and re-run"
  fi
  c_green "  cloned and scrubbed (dump kept at ${dump#"$ROOT"/})"
}

postgres_up() {
  if [[ "$PG_MODE" == "private" ]]; then pg_private_up; else pg_docker_up; fi
  if [[ -n "$CLONE_FROM" ]]; then
    clone_db
  elif [[ "$RESET_DB" -eq 1 ]]; then
    db_recreate >/dev/null || die "  could not recreate $DB_NAME"
    c_yellow "  dropped and recreated $DB_NAME"
  else
    ensure_db
  fi
}

aux_services_up() {
  if [[ "$WANT_REDIS" -eq 1 ]]; then
    note_docker redis
    compose --profile redis up -d redis >>"$LOG_DIR/docker.log" 2>&1 || die "  docker compose up redis failed (logs/docker.log)"
    c_green "  redis on 127.0.0.1:$REDIS_PORT"
  fi
  if [[ "$WANT_S3" -eq 1 ]]; then
    note_docker minio
    compose --profile s3 up -d minio >>"$LOG_DIR/docker.log" 2>&1 || die "  docker compose up minio failed (logs/docker.log)"
    wait_for "http://127.0.0.1:$MINIO_PORT/minio/health/ready" "minio" 60 "" "$LOG_DIR/docker.log" ready || die "  minio did not start"
    minio_bucket
  fi
}

# Bucket + public-read policy over the S3 API itself (curl's SigV4), so no mc image is needed.
minio_bucket() {
  local bucket="$SLUG-dev" user="$SLUG-dev:$SLUG-dev-secret" url="http://127.0.0.1:$MINIO_PORT" code
  code="$(curl -s -o /dev/null -w '%{http_code}' --aws-sigv4 "aws:amz:us-east-1:s3" --user "$user" -X PUT "$url/$bucket")"
  case "$code" in 200|409) ;; *) c_yellow "  could not create bucket $bucket (HTTP $code)"; return 0 ;; esac
  curl -s -o /dev/null --aws-sigv4 "aws:amz:us-east-1:s3" --user "$user" -X PUT "$url/$bucket?policy" \
    -H 'Content-Type: application/json' \
    --data "{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"AWS\":[\"*\"]},\"Action\":[\"s3:GetObject\"],\"Resource\":[\"arn:aws:s3:::$bucket/*\"]}]}"
  c_green "  minio bucket $bucket (objects public-read in development)"
}

# ---- step 8: backend --------------------------------------------------------

backend_up() {
  c_dim "  go build ./cmd/server ./cmd/seed → .data/bin (logs/backend-build.log)"
  backend_build || die "  backend build failed"
  if [[ -n "$CLONE_FROM" ]]; then
    c_dim "  no seed (cloned data); the server migrates at boot"
  elif [[ "$SEED" -eq 0 ]]; then
    c_dim "  no seed (--no-seed); the server migrates at boot"
  else
    backend_seed || die "  seed failed"
    c_green "  migrated + seeded the dev identities (logs/seed.log)"
  fi
  backend_start || die "  the backend did not become ready"
}

# ---- feature hooks ------------------------------------------------------------
#
# scripts/dev-up/hooks/<name>.sh (name: lowercase letters and digits) is sourced
# once, after the port block is picked, and may define (both optional):
#   hook_<name>_prepare  before env files are written and the backend starts;
#                        may start_bg/wait_for its own services and call
#                        hook_env <backend|web|mobile> KEY VALUE (highest precedence)
#   hook_<name>_started  after the backend is ready; may menu_add items
# hooks/README.md is the reference.

HOOK_NAMES=() HOOK_PHASE=""
MENU_KEYS=() MENU_LABELS=() MENU_FUNCS=()
HOOK_ENV_FILE="$RUN_DIR/hook-env.tsv"

# menu_add <key> <label> <function> — a hook's menu item.
menu_add() {
  local key="$1" label="$2" fn="$3" k
  case "$key" in
    w|a|i|d|l|r|R|s|g|t|m|p|c|q|"") c_yellow "  menu_add: key '$key' is built in"; return 1 ;;
  esac
  [[ "${#key}" -eq 1 ]] || { c_yellow "  menu_add: key must be one character"; return 1; }
  for k in ${MENU_KEYS[@]+"${MENU_KEYS[@]}"}; do
    [[ "$k" == "$key" ]] && { c_yellow "  menu_add: key '$key' already added"; return 1; }
  done
  MENU_KEYS+=("$key"); MENU_LABELS+=("$label"); MENU_FUNCS+=("$fn")
}

# hook_env <backend|web|mobile> <KEY> <value> — written into that tier's
# generated env file over everything else. Only in hook_<name>_prepare. Values
# travel through a 0600 file, never a command line.
hook_env() {
  local tier="$1" key="$2" value="${3-}"
  case "$tier" in backend|web|mobile) ;; *) c_red "  hook_env: tier must be backend, web or mobile (got $tier)"; return 1 ;; esac
  [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || { c_red "  hook_env: invalid key '$key'"; return 1; }
  case "$value" in *$'\n'*|*$'\t'*) c_red "  hook_env $key: the value contains a newline or tab"; return 1 ;; esac
  if [[ "$HOOK_PHASE" != "prepare" ]]; then
    c_yellow "  hook_env $key: ignored outside hook_<name>_prepare (env files are already written)"
    return 1
  fi
  ( umask 077; printf '%s\t%s\t%s\n' "$tier" "$key" "$value" >>"$HOOK_ENV_FILE" )
}

# hook_port <6-9> — prints base+offset, the block's spare ports; fails (with a
# note on stderr) when a process this project didn't start holds it.
hook_port() {
  local off="$1" port
  [[ "$off" =~ ^[6-9]$ ]] || { c_red "  hook_port: offset must be 6-9" >&2; return 1; }
  port=$((PORT_BASE + off))
  if [[ -n "$(port_pids "$port")" ]]; then
    c_red "  port $port is held by $(port_owner "$port"), not ours: pin another DEV_PORT_BASE in .env.dev" >&2
    return 1
  fi
  echo "$port"
}

load_hooks() {
  local f name
  HOOK_NAMES=()
  for f in "$ROOT"/scripts/dev-up/hooks/*.sh; do
    [[ -e "$f" ]] || continue
    name="$(basename "$f" .sh)"
    if [[ ! "$name" =~ ^[a-z0-9]+$ ]]; then
      c_yellow "  skipping ${f#"$ROOT"/}: hook names are lowercase letters and digits"
      continue
    fi
    # shellcheck disable=SC1090
    if ! source "$f"; then c_yellow "  ${f#"$ROOT"/} failed to load"; continue; fi
    HOOK_NAMES+=("$name")
  done
  if [[ "${#HOOK_NAMES[@]}" -gt 0 ]]; then c_dim "  hooks: ${HOOK_NAMES[*]}"; fi
  return 0
}

# run_hook_phase prepare|started — a failing prepare stops the boot.
run_hook_phase() {
  local phase="$1" name fn n=0
  HOOK_PHASE="$phase"
  for name in ${HOOK_NAMES[@]+"${HOOK_NAMES[@]}"}; do
    fn="hook_${name}_${phase}"
    declare -F "$fn" >/dev/null || continue
    n=$((n + 1))
    c_dim "  $fn"
    if ! "$fn"; then
      HOOK_PHASE=""
      [[ "$phase" == "prepare" && "$DEVUP_MODE" == "boot" ]] && die "  $fn failed: fix scripts/dev-up/hooks/$name.sh"
      c_yellow "  $fn returned non-zero"
    fi
  done
  HOOK_PHASE=""
  [[ "$n" -gt 0 || "$phase" != "started" || "$DEVUP_MODE" != "boot" ]] || c_dim "  no hook_<name>_started functions"
  return 0
}

# ---- step 10: web -----------------------------------------------------------

start_web() { # start_web dev|start
  env_unset_args "$ROOT/web/.env.local"
  start_bg web "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} pnpm --filter "$WEB_PKG" exec next "$1" -p "$WEB_PORT"
}

web_up() {
  local holder rc
  holder="$(port_owner "$WEB_PORT")"
  [[ -z "$holder" ]] || { c_red "  port $WEB_PORT is held by $holder, not ours: web not started"; return 1; }
  if [[ "$WEB_PROD" -eq 1 && -n "$WEB_BUILD_PID" ]]; then
    c_dim "  waiting for the production build (logs/web-build.log)…"
    local deadline=$((SECONDS + 900))
    while kill -0 "$WEB_BUILD_PID" 2>/dev/null && [[ $SECONDS -lt $deadline ]]; do sleep 1; done
    if kill -0 "$WEB_BUILD_PID" 2>/dev/null; then
      stop_service web-build
      rc=124
    else
      wait "$WEB_BUILD_PID" 2>/dev/null
      rc=$?
    fi
    rm -f "$RUN_DIR/web-build.pid"
    if [[ "$rc" -eq 0 ]]; then
      WEB_MODE="prod"
      start_web start
    else
      tail -n 20 "$LOG_DIR/web-build.log"
      c_yellow "  production build failed (exit $rc): falling back to next dev"
      WEB_MODE="dev"
      start_web dev
    fi
  else
    WEB_MODE="dev"
    start_web dev
  fi
  write_stack_env
  wait_for "$WEB_URL/" "web ($WEB_MODE)" 180 "$LAST_PID" "$LOG_DIR/web.log" up \
    || c_yellow "  web is not answering yet: R restarts it, l tails logs/web.log"
}

# ---- step 11: mobile --------------------------------------------------------

metro_up() {
  local holder
  holder="$(port_owner "$METRO_PORT")"
  [[ -z "$holder" ]] || { c_red "  port $METRO_PORT is held by $holder, not ours: Metro not started"; return 1; }
  env_unset_args "$ROOT/mobile/.env"
  start_bg metro "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} \
    pnpm --filter "$MOBILE_PKG" exec expo start --port "$METRO_PORT" --host "$EXPO_HOST" || return 1
  wait_for "http://localhost:$METRO_PORT/status" "metro (expo --host $EXPO_HOST)" 180 "$LAST_PID" "$LOG_DIR/metro.log" up \
    || { c_yellow "  Metro is not answering: see logs/metro.log"; return 1; }
  [[ "$EXPO_HOST" != "localhost" ]] && c_dim "  devices: scan the QR in logs/metro.log with the dev build"
  [[ "$OPEN_IOS" -eq 1 ]] && open_ios
  [[ "$OPEN_ANDROID" -eq 1 ]] && open_android
  return 0
}

dev_client_url() { printf '%s://expo-development-client/?url=http%%3A%%2F%%2Flocalhost%%3A%s' "$APP_SCHEME" "$METRO_PORT"; }

metro_running() {
  pid_alive metro && return 0
  c_yellow "  Metro is not running (started with --no-mobile, or mobile/ is missing)"
  return 1
}

open_ios() {
  xcrun simctl help >/dev/null 2>&1 || { c_red "  xcrun simctl not found: install Xcode"; return 1; }
  metro_running || return 1
  local udid
  udid="$(xcrun simctl list devices booted 2>/dev/null | grep -oE '[0-9A-F]{8}-([0-9A-F]{4}-){3}[0-9A-F]{12}' | head -n 1)"
  if [[ -z "$udid" ]]; then
    udid="$(xcrun simctl list devices available 2>/dev/null | grep 'iPhone' | grep -oE '[0-9A-F]{8}-([0-9A-F]{4}-){3}[0-9A-F]{12}' | head -n 1)"
    [[ -n "$udid" ]] || { c_red "  no iPhone simulator: create one in Xcode → Window → Devices and Simulators"; return 1; }
    c_dim "  booting simulator $udid"
    xcrun simctl boot "$udid" >/dev/null 2>&1 || true
  fi
  open -a Simulator >/dev/null 2>&1 || true
  with_timeout 120 xcrun simctl bootstatus "$udid" -b >/dev/null 2>&1 || true
  if ! xcrun simctl get_app_container "$udid" "$BUNDLE_ID" >/dev/null 2>&1; then
    c_yellow "  the $APP_NAME dev build ($BUNDLE_ID) is not installed on this simulator"
    if [[ "$INTERACTIVE" -eq 1 ]] && ASSUME_YES=0 confirm "build and install it now with expo run:ios (minutes; logs/ios-build.log)?"; then
      env_unset_args "$ROOT/mobile/.env"
      c_dim "  building… (tail -f logs/ios-build.log to watch; Ctrl-C cancels the build)"
      if ! run_shielded bash -c 'cd "$1" && shift && exec "$@"' _ "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} \
        pnpm --filter "$MOBILE_PKG" exec expo run:ios --no-bundler --port "$METRO_PORT" --device "$udid" \
        >"$LOG_DIR/ios-build.log" 2>&1; then
        tail -n 30 "$LOG_DIR/ios-build.log"
        c_red "  the iOS build failed (logs/ios-build.log)"
        return 1
      fi
    else
      c_dim "  build it once: pnpm --filter $MOBILE_PKG exec expo run:ios --no-bundler --port $METRO_PORT"
      return 1
    fi
  fi
  if xcrun simctl openurl "$udid" "$(dev_client_url)" 2>/dev/null; then
    c_green "  opened the dev build on the simulator (Metro :$METRO_PORT)"
  else
    c_red "  could not open $(dev_client_url): retry in a few seconds (i)"
  fi
}

open_android() {
  local sdk adb emu serial avd deadline
  sdk="$(android_sdk)"; adb="$sdk/platform-tools/adb"; emu="$sdk/emulator/emulator"
  [[ -x "$adb" && -x "$emu" ]] || { c_red "  Android SDK not found at $sdk (set ANDROID_HOME)"; return 1; }
  metro_running || return 1
  serial="$("$adb" devices 2>/dev/null | awk 'NR > 1 && $2 == "device" { print $1; exit }')"
  if [[ -z "$serial" ]]; then
    avd="$("$emu" -list-avds 2>/dev/null | head -n 1)"
    [[ -n "$avd" ]] || { c_red "  no Android Virtual Device: create one in Android Studio → Device Manager"; return 1; }
    DEVUP_ALLOW_START=1 start_bg android-emulator "$ROOT" "$emu" -avd "$avd" -no-boot-anim || return 1
    c_dim "  booting emulator $avd…"
    deadline=$((SECONDS + 180))
    while [[ $SECONDS -lt $deadline ]]; do
      serial="$("$adb" devices 2>/dev/null | awk 'NR > 1 && $2 == "device" { print $1; exit }')"
      [[ -n "$serial" && "$("$adb" -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == "1" ]] && break
      sleep 2
    done
    [[ -n "$serial" ]] || { c_red "  the emulator did not boot in 180s (logs/android-emulator.log)"; return 1; }
  fi
  # adb reverse makes localhost on the device reach this machine (Metro + API).
  "$adb" -s "$serial" reverse "tcp:$METRO_PORT" "tcp:$METRO_PORT" >/dev/null 2>&1
  "$adb" -s "$serial" reverse "tcp:$API_PORT" "tcp:$API_PORT" >/dev/null 2>&1
  if ! "$adb" -s "$serial" shell pm path "$BUNDLE_ID" 2>/dev/null | grep -q package:; then
    c_yellow "  the $APP_NAME dev build ($BUNDLE_ID) is not installed on $serial"
    if [[ "$INTERACTIVE" -eq 1 ]] && ASSUME_YES=0 confirm "build and install it now with expo run:android (minutes; logs/android-build.log)?"; then
      env_unset_args "$ROOT/mobile/.env"
      c_dim "  building… (tail -f logs/android-build.log to watch; Ctrl-C cancels the build)"
      if ! run_shielded bash -c 'cd "$1" && shift && exec "$@"' _ "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} \
        pnpm --filter "$MOBILE_PKG" exec expo run:android --no-bundler --port "$METRO_PORT" \
        >"$LOG_DIR/android-build.log" 2>&1; then
        tail -n 30 "$LOG_DIR/android-build.log"
        c_red "  the Android build failed (logs/android-build.log)"
        return 1
      fi
    else
      c_dim "  build it once: pnpm --filter $MOBILE_PKG exec expo run:android --no-bundler --port $METRO_PORT"
      return 1
    fi
  fi
  "$adb" -s "$serial" shell am start -a android.intent.action.VIEW -d "'$(dev_client_url)'" >/dev/null 2>&1 \
    && c_green "  opened the dev build on $serial (Metro :$METRO_PORT)" \
    || c_red "  could not open the dev build on $serial"
}

# ---- step 12: summary -------------------------------------------------------

print_summary() {
  echo
  c_blue "━━ $APP_NAME is up ━━"
  [[ "$RUN_BACKEND" -eq 1 ]] && echo "  API       $API_URL   (logs/backend.log)"
  [[ -n "$PUBLIC_URL" ]] && echo "  Public    $PUBLIC_URL → API (webhooks: $PUBLIC_URL/webhooks/<provider>)"
  if [[ "$RUN_WEB" -eq 1 ]]; then
    if pid_alive web; then
      echo "  Web       $WEB_URL   ($WEB_MODE; logs/web.log)"
      echo "  Admin     $WEB_URL/admin"
    else
      c_red "  Web       not running: see logs/web.log (R restarts it)"
    fi
  fi
  if [[ "$RUN_MOBILE" -eq 1 ]]; then
    if pid_alive metro; then
      echo "  Metro     http://localhost:$METRO_PORT   (expo --host $EXPO_HOST; logs/metro.log)"
    else
      c_red "  Metro     not running: see logs/metro.log"
    fi
    [[ -n "$LAN_IP" ]] && echo "  Devices   API at http://$LAN_IP:$API_PORT"
  fi
  if [[ "$PG_MODE" == "private" ]]; then
    echo "  Postgres  $DATABASE_URL   (private cluster .data/postgres)"
  else
    echo "  Postgres  $DATABASE_URL   (docker compose db)"
  fi
  echo "  Sign in   /sign-in → pick a dev identity: $DEV_ADMIN_EMAIL (admin), sam@, jordan@, priya@$DEV_EMAIL_DOMAIN"
  [[ "$RUN_BACKEND" -eq 1 ]] && echo "  curl      curl -H 'Authorization: Bearer dev:sam:sam@$DEV_EMAIL_DOMAIN' $API_URL/v1/me"
  [[ -n "$MAIL_SINK_DIR" ]] && echo "  Mail      ${MAIL_SINK_DIR#"$ROOT"/} (mail sink)"
  echo "  Logs      logs/        Data .data/"
  if [[ -f "$RUN_DIR/providers.txt" ]]; then
    echo "  Providers"
    sed 's/^/  /' "$RUN_DIR/providers.txt"
  fi
}

# ---- the menu ---------------------------------------------------------------

PICKED_ID="" PICKED_EMAIL="" PICKED_SUBJECT="" PICKED_NAME="" PICKED_ROLE=""

# pick_user <label> — searchable user chooser (psql -v keeps the term inert).
pick_user() {
  local term rows id email sub name role i choice
  local ids emails subs names roles
  while :; do
    printf '  %s — search email or name (blank cancels): ' "$1"
    read -r term || return 1
    [[ -n "$term" ]] || return 1
    rows="$(psql_dev -tA -F $'\t' -v q="$term" <<'SQL' 2>/dev/null
SELECT id, email, auth_subject, COALESCE(NULLIF(display_name, ''), '-'), COALESCE(admin_role, '-')
  FROM users
 WHERE status <> 'deleted'
   AND (email ILIKE '%' || :'q' || '%' OR display_name ILIKE '%' || :'q' || '%' OR auth_subject = :'q')
 ORDER BY lower(email), created_at
 LIMIT 30;
SQL
)"
    ids=() emails=() subs=() names=() roles=()
    while IFS=$'\t' read -r id email sub name role; do
      [[ -n "$id" ]] || continue
      ids+=("$id"); emails+=("$email"); subs+=("$sub"); names+=("$name"); roles+=("$role")
    done <<<"$rows"
    if [[ "${#ids[@]}" -eq 0 ]]; then c_yellow "    no users match \"$term\""; continue; fi
    for i in "${!ids[@]}"; do
      printf '    [%2d] %-34s %-22s %-8s %s\n' "$((i + 1))" "${emails[$i]}" "${names[$i]}" "${roles[$i]}" "${subs[$i]}"
    done
    printf '  pick 1-%d, s to search again (blank cancels): ' "${#ids[@]}"
    read -r choice || return 1
    [[ -n "$choice" ]] || return 1
    [[ "$choice" == "s" ]] && continue
    if [[ "$choice" =~ ^[0-9]+$ && "$choice" -ge 1 && "$choice" -le "${#ids[@]}" ]]; then
      i=$((choice - 1))
      PICKED_ID="${ids[$i]}" PICKED_EMAIL="${emails[$i]}" PICKED_SUBJECT="${subs[$i]}"
      PICKED_NAME="${names[$i]}" PICKED_ROLE="${roles[$i]}"
      return 0
    fi
    c_yellow "    not a choice"
  done
}

menu_grant_admin() {
  pick_user "grant admin" || { c_dim "  cancelled"; return 0; }
  local matches n
  matches="$(psql_dev -tA -F $'\t' -v e="$PICKED_EMAIL" <<'SQL'
SELECT email, auth_subject, COALESCE(admin_role, '-') FROM users WHERE lower(email) = lower(:'e') ORDER BY created_at;
SQL
)"
  echo "  accounts with $PICKED_EMAIL:"
  printf '%s\n' "$matches" | awk -F'\t' 'NF { printf "    %-34s %-28s role=%s\n", $1, $2, $3 }'
  ASSUME_YES=0 confirm "make every account above admin?" || { c_dim "  cancelled"; return 0; }
  n="$(psql_dev -tA -v e="$PICKED_EMAIL" <<'SQL'
WITH granted AS (
  UPDATE users SET admin_role = 'admin', updated_at = now() WHERE lower(email) = lower(:'e') RETURNING 1
)
SELECT count(*) FROM granted;
SQL
)"
  c_green "  ${n:-0} account(s) are admin now (from their next request)"
}

menu_dev_token() {
  pick_user "dev token for" || { c_dim "  cancelled"; return 0; }
  case "$PICKED_SUBJECT" in
    dev_*) ;;
    *)
      c_yellow "  $PICKED_EMAIL is a Clerk user ($PICKED_SUBJECT): dev tokens only exist for dev_ subjects."
      c_dim "  Sign in through Clerk, or pick a dev identity. A new one appears on first use of dev:<name>:<name>@$DEV_EMAIL_DOMAIN."
      return 0 ;;
  esac
  local token="dev:${PICKED_SUBJECT#dev_}:$PICKED_EMAIL"
  echo "  $token"
  echo "  curl -H 'Authorization: Bearer $token' ${API_URL}/v1/me"
}

menu_logs() {
  local files=() f i choice picks=() n
  for f in "$LOG_DIR"/*.log; do [[ -e "$f" ]] && files+=("$f"); done
  [[ "${#files[@]}" -gt 0 ]] || { c_yellow "  no logs yet"; return 0; }
  for i in "${!files[@]}"; do printf '    [%2d] %s\n' "$((i + 1))" "$(basename "${files[$i]}" .log)"; done
  echo "    [ a] every running service"
  printf '  log (blank cancels): '
  read -r choice || return 0
  [[ -n "$choice" ]] || return 0
  if [[ "$choice" == "a" ]]; then
    for n in $(service_names); do
      pid_alive "$n" && [[ -f "$LOG_DIR/$n.log" ]] && picks+=("$LOG_DIR/$n.log")
    done
    [[ "$PG_MODE" == "private" && -f "$LOG_DIR/postgres.log" ]] && picks+=("$LOG_DIR/postgres.log")
  elif [[ "$choice" =~ ^[0-9]+$ && "$choice" -ge 1 && "$choice" -le "${#files[@]}" ]]; then
    picks=("${files[$((choice - 1))]}")
  fi
  [[ "${#picks[@]}" -gt 0 ]] || { c_yellow "  nothing to tail"; return 0; }
  c_dim "  tailing; Ctrl-C returns to the menu"
  run_shielded tail -n 40 -f "${picks[@]}"
  echo
}

menu_restart_backend() {
  [[ -f "$ROOT/backend/go.mod" ]] || { c_yellow "  no backend/"; return 0; }
  c_dim "  rebuilding…"
  backend_build || return 0
  backend_stop
  DEVUP_ALLOW_START=1 backend_start || c_red "  the backend did not come back: l → backend"
}

menu_restart_web() {
  [[ -f "$ROOT/web/package.json" ]] || { c_yellow "  no web/"; return 0; }
  stop_service web
  wait_port_free "$WEB_PORT" 10 || { c_red "  port $WEB_PORT is still held by $(port_owner "$WEB_PORT")"; return 0; }
  if [[ "${WEB_MODE:-dev}" == "prod" ]]; then
    c_dim "  rebuilding for production (logs/web-build.log; Ctrl-C cancels)…"
    env_unset_args "$ROOT/web/.env.local"
    if run_shielded bash -c 'cd "$1" && shift && exec "$@"' _ "$ROOT" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} \
      pnpm --filter "$WEB_PKG" exec next build >"$LOG_DIR/web-build.log" 2>&1; then
      DEVUP_ALLOW_START=1 start_web start
    else
      tail -n 20 "$LOG_DIR/web-build.log"
      c_yellow "  build failed: starting next dev instead"
      WEB_MODE="dev"
      DEVUP_ALLOW_START=1 start_web dev
    fi
  else
    DEVUP_ALLOW_START=1 start_web dev
  fi
  wait_for "$WEB_URL/" "web" 180 "$LAST_PID" "$LOG_DIR/web.log" up || true
}

menu_seed() {
  [[ -f "$ROOT/backend/go.mod" ]] || { c_yellow "  no backend/"; return 0; }
  backend_build && backend_seed && c_green "  seeded (logs/seed.log)"
}

menu_mail() {
  [[ -n "$MAIL_SINK_DIR" ]] || { c_yellow "  MAIL_SINK_DIR is unset (backend/.env)"; return 0; }
  mkdir -p "$MAIL_SINK_DIR"
  echo "  $MAIL_SINK_DIR ($(find "$MAIL_SINK_DIR" -type f | wc -l | tr -d ' ') messages)"
  open_url "$MAIL_SINK_DIR"
}

menu_doctor() {
  local cli=""
  if command -v scaffold >/dev/null 2>&1; then cli="scaffold"
  elif [ -n "${SCAFFOLD_HOME:-}" ] && [ -x "$SCAFFOLD_HOME/bin/scaffold" ]; then cli="$SCAFFOLD_HOME/bin/scaffold"
  elif [ -x "$HOME/scaffold/bin/scaffold" ]; then cli="$HOME/scaffold/bin/scaffold"
  fi
  if [ -n "$cli" ]; then
    ( cd "$ROOT" && run_shielded "$cli" doctor ) || true
  else
    c_yellow "  scaffold CLI not found (PATH, \$SCAFFOLD_HOME/bin, ~/scaffold/bin): skipping scaffold doctor"
  fi
  run_shielded "$ROOT/scripts/preflight/preflight.sh" || true
}

menu_loop() {
  local sel i hit
  while :; do
    echo
    if [[ "$DEVUP_MODE" == "boot" ]]; then
      c_blue "$APP_NAME dev: running (q or Ctrl-C stops everything)"
    else
      c_blue "$APP_NAME dev menu (q leaves the stack running)"
    fi
    [[ "${RUN_WEB:-0}" -eq 1 ]] && echo "  web $WEB_URL   admin $WEB_URL/admin"
    [[ "${RUN_BACKEND:-0}" -eq 1 ]] && echo "  api $API_URL"
    [[ "${RUN_MOBILE:-0}" -eq 1 ]] && echo "  metro http://localhost:$METRO_PORT"
    echo "  w web  a admin  i iOS sim  d Android emu  l logs  r rebuild+restart backend  R restart web"
    echo "  s seed  g grant admin  t dev token  m mail sink  p psql  c doctor+preflight  q quit"
    for i in ${MENU_KEYS[@]+"${!MENU_KEYS[@]}"}; do
      echo "  ${MENU_KEYS[$i]} ${MENU_LABELS[$i]}"
    done
    printf '> '
    sel=""
    read -r sel || sel="q"
    case "$sel" in
      w) open_url "$WEB_URL" ;;
      a) open_url "$WEB_URL/admin" ;;
      i) open_ios ;;
      d) open_android ;;
      l) menu_logs ;;
      r) menu_restart_backend ;;
      R) menu_restart_web ;;
      s) menu_seed ;;
      g) menu_grant_admin ;;
      t) menu_dev_token ;;
      m) menu_mail ;;
      p) run_shielded psql_shell ;;
      c) menu_doctor ;;
      q) return 0 ;;
      "") ;;
      *)
        hit=0
        for i in ${MENU_KEYS[@]+"${!MENU_KEYS[@]}"}; do
          if [[ "${MENU_KEYS[$i]}" == "$sel" ]]; then hit=1; "${MENU_FUNCS[$i]}" || true; fi
        done
        [[ "$hit" -eq 1 ]] || c_yellow "  unknown choice: $sel"
        ;;
    esac
  done
}

resident_wait() {
  c_dim "  no terminal: no menu. Stop with scripts/dev-up/dev-up.sh stop (or a signal)."
  local warned=0
  while :; do
    sleep 2 &
    wait $! 2>/dev/null
    if [[ "$RUN_BACKEND" -eq 1 ]]; then
      if pid_alive backend; then
        [[ "$warned" -eq 1 ]] && c_green "  the backend is running again (pid $PID_PID)"
        warned=0
      elif [[ "$warned" -eq 0 ]]; then
        c_red "  the backend is no longer running (logs/backend.log; scripts/restart-backend/restart-backend.sh)"
        warned=1
      fi
    fi
  done
}

# ---- subcommands ------------------------------------------------------------

http_code() { curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$1" 2>/dev/null; }

status_row() { # status_row <service> <address> <health> <pid-file-name>
  local pid="-"
  if [[ -n "$4" ]] && pid_alive "$4"; then pid="$PID_PID"; elif [[ -n "$4" ]]; then pid="not running"; fi
  printf '  %-10s %-36s %-12s %s\n' "$1" "$2" "$3" "$pid"
}

cmd_status() {
  local rc=0 code pgpid health
  stack_discover
  if pid_alive dev-up; then
    c_green "$APP_NAME dev stack: running (dev-up pid $PID_PID)"
  else
    c_yellow "$APP_NAME dev stack: dev-up is not running"
    rc=1
  fi
  if [[ ! -f "$RUN_DIR/stack.env" ]]; then
    echo "  nothing recorded yet: start it with scripts/dev-up/dev-up.sh"
    return 1
  fi
  printf '  %-10s %-36s %-12s %s\n' "service" "address" "health" "pid"
  if [[ "${RUN_BACKEND:-0}" -eq 1 ]]; then
    code="$(http_code "http://127.0.0.1:$API_PORT/readyz")"
    [[ "$code" == "200" ]] && health="ready" || { health="HTTP ${code:-000}"; rc=1; }
    status_row api "$API_URL" "$health" backend
  fi
  if [[ "${RUN_WEB:-0}" -eq 1 ]]; then
    code="$(http_code "http://localhost:$WEB_PORT/")"
    status_row web "$WEB_URL (${WEB_MODE:-dev})" "HTTP ${code:-000}" web
  fi
  if [[ "${RUN_MOBILE:-0}" -eq 1 ]]; then
    code="$(http_code "http://localhost:$METRO_PORT/status")"
    status_row metro "http://localhost:$METRO_PORT" "HTTP ${code:-000}" metro
  fi
  if [[ "$PG_MODE" == "private" ]]; then
    pgpid="not running"
    pg_running && pgpid="$(head -n 1 "$PGDATA/postmaster.pid")"
    if psql_admin -tAc "SELECT 1" >/dev/null 2>&1; then health="accepting"; else health="down"; rc=1; fi
    printf '  %-10s %-36s %-12s %s\n' postgres "127.0.0.1:$PG_PORT (private)" "$health" "$pgpid"
  else
    if psql_admin -tAc "SELECT 1" >/dev/null 2>&1; then health="accepting"; else health="down"; rc=1; fi
    printf '  %-10s %-36s %-12s %s\n' postgres "127.0.0.1:$PG_PORT (docker)" "$health" "container"
  fi
  local n
  for n in $(service_names); do
    case "$n" in backend|web|metro) continue ;; esac
    status_row "$n" "-" "-" "$n"
  done
  [[ -n "${PUBLIC_URL:-}" ]] && echo "  public     $PUBLIC_URL"
  echo "  logs       ${LOG_DIR#"$ROOT"/}/   data .data/"
  return $rc
}

cmd_stop() {
  local p deadline
  stack_discover
  if pid_alive dev-up; then
    p="$PID_PID"
    c_yellow "  stopping dev-up (pid $p)…"
    # Its TERM trap tears the stack down. If a foreground child (a log tail,
    # psql, a build) keeps it from running the trap, TERM the children too.
    kill -TERM "$p" 2>/dev/null || true
    deadline=$((SECONDS + 5))
    while kill -0 "$p" 2>/dev/null && [[ $SECONDS -lt $deadline ]]; do sleep 0.25; done
    if kill -0 "$p" 2>/dev/null; then
      local c
      for c in $(descendants "$p"); do kill -TERM "$c" 2>/dev/null || true; done
    fi
    deadline=$((SECONDS + 20))
    while kill -0 "$p" 2>/dev/null && [[ $SECONDS -lt $deadline ]]; do sleep 0.5; done
    if kill -0 "$p" 2>/dev/null; then
      c_yellow "  dev-up did not exit in 20s; killing it"
      kill -KILL "$p" 2>/dev/null || true
    fi
  fi
  rm -f "$RUN_DIR/dev-up.pid"
  stop_all_services
  docker_stop_ours
  [[ "$PG_MODE" == "private" ]] && pg_stop_cluster
  c_green "  stopped (logs/ and .data/ are kept)"
  return 0
}

cmd_menu() {
  DEVUP_MODE="menu"
  require_local_config
  [[ -t 0 ]] || die "the menu needs a terminal"
  pid_alive dev-up || c_yellow "  dev-up is not running: items that need a service will say so"
  load_hooks >/dev/null
  run_hook_phase started >/dev/null
  menu_loop
}

# ---- boot -------------------------------------------------------------------

boot() {
  mkdir -p "$LOG_DIR" "$RUN_DIR"

  step 1 "Tools"
  detect_surfaces
  check_node
  load_settings
  check_tools

  step 2 "Already running?"
  check_running
  if [[ -n "$CLONE_FROM" ]]; then
    confirm "replace the local $DB_NAME with a scrubbed copy of $(redact_url "$CLONE_FROM")?" || die "  cancelled"
  elif [[ "$RESET_DB" -eq 1 ]]; then
    confirm "drop and recreate the local $DB_NAME (all its data is lost)?" || die "  cancelled"
  fi
  pid_record dev-up $$ "dev-up.sh" "$0"
  trap 'cleanup 130' INT
  trap 'cleanup 143' TERM
  trap 'cleanup 129' HUP
  trap 'cleanup $?' EXIT

  step 3 "Ports"
  sweep_stale
  allocate_ports

  step 4 "Env"
  generate_env

  step 5 "Dependencies"
  install_deps

  step 6 "Generated assets"
  generated_assets

  step 7 "Postgres"
  postgres_up
  aux_services_up

  step 8 "Backend"
  if [[ "$RUN_BACKEND" -eq 1 ]]; then backend_up; else c_dim "  skipped"; fi

  step 9 "Feature hooks"
  run_hook_phase started

  step 10 "Web"
  if [[ "$RUN_WEB" -eq 1 ]]; then web_up || true; else c_dim "  skipped"; fi

  step 11 "Mobile"
  if [[ "$RUN_MOBILE" -eq 1 ]]; then metro_up || true; else c_dim "  skipped"; fi

  step 12 "Summary"
  write_stack_env
  print_summary
  if [[ "$INTERACTIVE" -eq 1 ]]; then
    menu_loop
    exit 0
  fi
  resident_wait
}

case "$CMD" in
  status) cmd_status; exit $? ;;
  stop) cmd_stop; exit $? ;;
  menu) cmd_menu; exit $? ;;
  boot) boot ;;
esac
