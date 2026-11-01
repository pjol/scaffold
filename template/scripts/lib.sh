#!/usr/bin/env bash
# Shared plumbing for the scripts in scripts/*/. Source it; never run it.
#
#   output      c_blue c_green c_yellow c_red c_dim, die, confirm, run_shielded
#   discovery   stack_discover: endpoints from the generated backend/.env plus
#               .data/run/stack.env (written by dev-up), never hand-copied values
#   safety      require_local_config (URLs and database are local),
#               require_local_stack (+ /readyz answers), require_local_db
#   processes   start_bg, wait_for, kill_tree, stop_service, stop_all_services;
#               every service has .data/run/<name>.pid = pid, start time, match
#               string, command. Nothing is signalled unless all three still
#               match, so a recycled pid or someone else's process is never hit.
#   postgres    pg_find_bins, psql_dev, psql_admin, compose
#   backend     backend_build, backend_seed, backend_start, backend_stop
#
# Bash 3.2 compatible (macOS /bin/bash): no associative arrays, no mapfile.

set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="$ROOT/logs"
DATA_DIR="$ROOT/.data"
RUN_DIR="$DATA_DIR/run"
BIN_DIR="$DATA_DIR/bin"
PGDATA="$DATA_DIR/postgres"

# Project identity (scaffold new replaces these literals).
SLUG="scaffoldapp"
APP_NAME="Scaffold App"
BUNDLE_ID="com.example.scaffoldapp"
APP_SCHEME="scaffoldapp"
DB_USER="scaffoldapp"
DB_NAME="scaffoldapp_dev"
WEB_PKG="@scaffoldapp/web"
MOBILE_PKG="@scaffoldapp/mobile"
DEV_ADMIN_EMAIL="admin@scaffoldapp.test"
DEV_EMAIL_DOMAIN="scaffoldapp.test"

# ---- output -----------------------------------------------------------------

if [[ -t 1 && -z "${NO_COLOR:-}" ]]; then
  _c() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
else
  _c() { printf '%s\n' "$2"; }
fi
c_blue()   { _c '1;34' "$*"; }
c_green()  { _c '32' "$*"; }
c_yellow() { _c '33' "$*"; }
c_red()    { _c '31' "$*"; }
c_dim()    { _c '2' "$*"; }
die()      { c_red "$*" >&2; exit 1; }

# confirm <question> — yes when ASSUME_YES=1 (--yes); refuses without a terminal.
confirm() {
  if [[ "${ASSUME_YES:-0}" -eq 1 ]]; then
    echo "  $1 [y/N] y (--yes)"
    return 0
  fi
  if [[ ! -t 0 ]]; then
    c_red "  $1 — needs confirmation: re-run with --yes"
    return 1
  fi
  local answer=""
  printf '  %s [y/N] ' "$1"
  read -r answer || return 1
  [[ "$answer" =~ ^[Yy] ]]
}

# run_shielded <cmd…> — run a foreground command that Ctrl-C should stop
# without tearing the stack down. A no-op INT *handler* (not an ignore, which
# children would inherit) lets Ctrl-C kill the child while this shell carries
# on; the caller's INT trap is restored afterwards.
run_shielded() {
  local saved rc
  saved="$(trap -p INT)"
  trap ':' INT
  "$@"
  rc=$?
  if [[ -n "$saved" ]]; then eval "$saved"; else trap - INT; fi
  return $rc
}

# ---- generated env + stack discovery ----------------------------------------

# env_get <file> <KEY> — a value from a generated env file ('' when absent).
env_get() {
  local file="$1" key="$2" line v
  [[ -f "$file" ]] || return 0
  line="$(grep -E "^${key}=" "$file" 2>/dev/null | tail -n 1)"
  [[ -n "$line" ]] || return 0
  v="${line#*=}"
  case "$v" in
    \'*\') v="${v#\'}"; v="${v%\'}" ;;
    \"*\") v="${v#\"}"; v="${v%\"}"; v="$(printf '%s' "$v" | sed -e 's/\\"/"/g' -e 's/\\\$/$/g' -e 's/\\\\/\\/g')" ;;
  esac
  printf '%s' "$v"
}

# stack_load — read .data/run/stack.env (plain KEY=value, written by dev-up).
stack_load() {
  local f="$RUN_DIR/stack.env" k v
  [[ -f "$f" ]] || return 1
  while IFS='=' read -r k v; do
    case "$k" in
      PORT_BASE|API_PORT|WEB_PORT|PG_PORT|METRO_PORT|MINIO_PORT|REDIS_PORT|API_URL|WEB_URL|PUBLIC_URL|LAN_IP|\
      PG_MODE|PG_BIN|DATABASE_URL|RUN_BACKEND|RUN_WEB|RUN_MOBILE|WEB_MODE|EXPO_HOST|STARTED_AT)
        printf -v "$k" '%s' "$v" ;;
    esac
  done <"$f"
  return 0
}

# stack_discover — endpoints for scripts. backend/.env (generated) wins for the
# API port and database; stack.env fills the rest.
stack_discover() {
  API_PORT="" WEB_PORT="" PG_PORT="" METRO_PORT="" MINIO_PORT="" REDIS_PORT="" PORT_BASE=""
  API_URL="" WEB_URL="" PUBLIC_URL="" LAN_IP="" PG_MODE="" PG_BIN="" DATABASE_URL=""
  RUN_BACKEND="" RUN_WEB="" RUN_MOBILE="" WEB_MODE="" EXPO_HOST="" STARTED_AT=""
  stack_load || true
  local be="$ROOT/backend/.env" v
  if [[ -f "$be" ]]; then
    v="$(env_get "$be" PORT)"; [[ -n "$v" ]] && API_PORT="$v"
    v="$(env_get "$be" DATABASE_URL)"; [[ -n "$v" ]] && DATABASE_URL="$v"
    v="$(env_get "$be" PUBLIC_WEB_URL)"; [[ -n "$v" && -z "$WEB_URL" ]] && WEB_URL="$v"
  fi
  MAIL_SINK_DIR="$(env_get "$be" MAIL_SINK_DIR)"
  [[ -n "$API_PORT" ]] && API_URL="http://localhost:$API_PORT"
  if [[ -z "$PG_PORT" && -n "$DATABASE_URL" ]]; then
    PG_PORT="$(printf '%s' "$DATABASE_URL" | sed -nE 's#^postgres(ql)?://[^@/]*@[^:/]+:([0-9]+)/.*#\2#p')"
  fi
  [[ -n "$PG_MODE" ]] || PG_MODE="private"
  if [[ "$PG_MODE" == "private" && -z "$PG_BIN" ]]; then pg_find_bins "" "" >/dev/null 2>&1 || true; fi
  return 0
}

is_local_url() {
  case "$1" in
    http://localhost:*|http://127.0.0.1:*|http://localhost|http://127.0.0.1) return 0 ;;
  esac
  return 1
}

is_local_db_url() {
  case "$1" in
    postgres://*@127.0.0.1:*|postgres://*@localhost:*|postgresql://*@127.0.0.1:*|postgresql://*@localhost:*) return 0 ;;
  esac
  return 1
}

# require_local_config — the discovered API and database are this machine's.
require_local_config() {
  stack_discover
  [[ -n "$API_PORT" ]] || die "backend/.env not found — start the stack once with scripts/dev-up/dev-up.sh"
  is_local_url "$API_URL" || die "API is $API_URL — these scripts only ever run against localhost"
  [[ -n "$DATABASE_URL" ]] || die "DATABASE_URL missing from backend/.env — re-run scripts/dev-up/dev-up.sh"
  is_local_db_url "$DATABASE_URL" || die "DATABASE_URL in backend/.env is not local — refusing"
}

# require_local_stack — require_local_config + the backend answers /readyz.
require_local_stack() {
  require_local_config
  local code
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$API_URL/readyz" 2>/dev/null)"
  [[ "$code" == "200" ]] || die "backend not ready at $API_URL/readyz (HTTP ${code:-000}) — start it with scripts/dev-up/dev-up.sh"
}

# require_local_db — require_local_config + the dev database server answers.
require_local_db() {
  require_local_config
  psql_admin -tAc "SELECT 1" >/dev/null 2>&1 \
    || die "Postgres is not answering on 127.0.0.1:${PG_PORT:-?} — start the stack with scripts/dev-up/dev-up.sh"
}

# ---- processes --------------------------------------------------------------

proc_lstart() { ps -o lstart= -p "$1" 2>/dev/null | sed -e 's/^ *//' -e 's/ *$//'; }
proc_cmd()    { ps -ww -o command= -p "$1" 2>/dev/null; }
proc_name()   { local c; c="$(ps -o comm= -p "$1" 2>/dev/null)"; printf '%s' "${c##*/}"; }

# Services run in their own session so a Ctrl-C at the menu (or in a log tail)
# reaches only the foreground, never the stack. perl ships with macOS.
DETACH=()
if command -v setsid >/dev/null 2>&1; then
  DETACH=(setsid)
elif command -v perl >/dev/null 2>&1; then
  DETACH=(perl -MPOSIX -e 'POSIX::setsid(); exec { $ARGV[0] } @ARGV or die "exec $ARGV[0]: $!\n"' --)
fi

# _argv0 <cmd…> — what a start_bg command line really runs, for the pid file's
# match string (skips env, its -u options and VAR=value words).
_argv0() {
  local a skip=0
  for a in "$@"; do
    if [[ "$skip" -eq 1 ]]; then skip=0; continue; fi
    case "$a" in
      env|*/env|nohup) continue ;;
      -u) skip=1; continue ;;
      -i) continue ;;
      [A-Za-z_]*=*) continue ;;
    esac
    case "$a" in
      /*) printf '%s' "$a" ;;
      *) printf '%s' "${a##*/}" ;;
    esac
    return
  done
}

# pid_record <name> <pid> <match> <cmd…> — a second later, line 5 gets the
# command line ps shows once the process has settled, for programs that
# re-exec under another name (python3 → Python). The live command must contain
# the match string or equal that settled command line.
pid_record() {
  local name="$1" pid="$2" match="$3" f="$RUN_DIR/$1.pid"
  shift 3
  mkdir -p "$RUN_DIR"
  { echo "$pid"; proc_lstart "$pid"; echo "$match"; echo "$*"; } >"$f"
  (
    sleep 1
    local first seen
    first="$(head -n 1 "$f" 2>/dev/null)"
    seen="$(proc_cmd "$pid")"
    if [[ "$first" == "$pid" && -n "$seen" ]]; then
      { head -n 4 "$f"; echo "$seen"; } >"$f.tmp" 2>/dev/null && mv -f "$f.tmp" "$f"
    fi
  ) >/dev/null 2>&1 </dev/null &
}

# pid_alive <name> — the recorded process still runs: same pid, same start
# time, and its command contains the match (or equals the settled command).
# Sets PID_PID.
pid_alive() {
  local f="$RUN_DIR/$1.pid" pid="" lstart="" match="" cmdline="" seen="" now
  PID_PID=""
  [[ -f "$f" ]] || return 1
  { read -r pid; read -r lstart; read -r match; read -r cmdline; read -r seen; } <"$f"
  [[ "$pid" =~ ^[0-9]+$ && -n "$lstart" && -n "$match" ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  now="$(proc_lstart "$pid")"
  [[ "$now" == "$lstart" ]] || return 1
  now="$(proc_cmd "$pid")"
  case "$now" in
    *"$match"*) ;;
    *) [[ -n "$seen" && "$now" == "$seen" ]] || return 1 ;;
  esac
  PID_PID="$pid"
  return 0
}

# descendants <pid> — every process below pid, deepest first.
descendants() {
  local child
  for child in $(pgrep -P "$1" 2>/dev/null); do
    descendants "$child"
    echo "$child"
  done
}

# tree_of <pid> — pid, its descendants and the rest of its process group (a
# service is its own group leader, so the group catches orphaned grandchildren).
tree_of() {
  descendants "$1"
  ps -A -o pid= -o pgid= 2>/dev/null | awk -v g="$1" '$2 == g && $1 != g { print $1 }'
  echo "$1"
}

# kill_tree <sig> <pid> — signal a process and all its descendants, leaves first.
kill_tree() {
  local sig="$1" pid="$2" p
  for p in $(tree_of "$pid" | awk '!seen[$0]++'); do
    kill -"$sig" "$p" 2>/dev/null || true
  done
}

# stop_pids <pid…> — TERM the trees, wait up to 3 s, KILL survivors. Callers
# pass only verified pids (pid_alive).
stop_pids() {
  local all="" p i alive
  for p in "$@"; do all="$all $(tree_of "$p" | tr '\n' ' ')"; done
  all="$(printf '%s\n' $all | awk 'NF && !seen[$0]++' | tr '\n' ' ')"
  [[ -n "${all// /}" ]] || return 0
  for p in $all; do kill -TERM "$p" 2>/dev/null || true; done
  for i in 1 2 3 4 5 6; do
    alive=0
    for p in $all; do kill -0 "$p" 2>/dev/null && { alive=1; break; }; done
    [[ "$alive" -eq 0 ]] && return 0
    sleep 0.5
  done
  for p in $all; do kill -KILL "$p" 2>/dev/null || true; done
  return 0
}

# stop_service <name> — stop a recorded service if it is still ours; always
# removes the pid file.
stop_service() {
  if pid_alive "$1"; then
    stop_pids "$PID_PID"
  fi
  rm -f "$RUN_DIR/$1.pid"
}

# service_names — every recorded service (not dev-up itself).
service_names() {
  local f n
  for f in "$RUN_DIR"/*.pid; do
    [[ -e "$f" ]] || continue
    n="$(basename "$f" .pid)"
    [[ "$n" == "dev-up" ]] && continue
    echo "$n"
  done
}

# stop_all_services — every recorded service still ours, in one TERM/KILL round.
stop_all_services() {
  local n pids=()
  for n in $(service_names); do
    if pid_alive "$n"; then
      c_dim "  stopping $n (pid $PID_PID)"
      pids+=("$PID_PID")
    fi
  done
  [[ ${#pids[@]} -gt 0 ]] && stop_pids "${pids[@]}"
  for n in $(service_names); do rm -f "$RUN_DIR/$n.pid"; done
  return 0
}

# start_bg <name> <dir> <cmd…> — a tracked background service: stdin
# /dev/null (it can't steal menu keys), output to logs/<name>.log, own
# session, pid file in .data/run. Sets LAST_PID.
start_bg() {
  local name="$1" dir="$2" log match
  shift 2
  if [[ "${DEVUP_MODE:-boot}" == "menu" && "${DEVUP_ALLOW_START:-0}" -ne 1 ]]; then
    c_yellow "  $name: not started (menu mode boots nothing)"
    return 1
  fi
  [[ -d "$dir" ]] || { c_red "  cannot start $name: $dir does not exist"; return 1; }
  mkdir -p "$LOG_DIR" "$RUN_DIR"
  log="$LOG_DIR/$name.log"
  match="$(_argv0 "$@")"
  # Append across restarts (a crash stays readable); rotate past ~5 MB.
  if [[ -f "$log" && "$(wc -c <"$log" | tr -d ' ')" -gt 5000000 ]]; then mv -f "$log" "$log.1"; fi
  printf '\n===== %s: %s =====\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$name" >>"$log"
  ( cd "$dir" && exec ${DETACH[@]+"${DETACH[@]}"} "$@" ) >>"$log" 2>&1 </dev/null &
  LAST_PID=$!
  # Out of the job table (no "Terminated" notices at teardown) unless the
  # caller needs `wait` for its exit status (START_BG_WAITABLE=1).
  [[ "${START_BG_WAITABLE:-0}" -eq 1 ]] || disown "$LAST_PID" 2>/dev/null || true
  pid_record "$name" "$LAST_PID" "$match" "$@"
  c_dim "  $name: pid $LAST_PID, log ${log#"$ROOT"/}"
  [[ -n "${ON_START_BG:-}" ]] && "$ON_START_BG"
  return 0
}

# wait_for <url> <name> <timeout_s> [pid] [log] [ready|up]
#   ready = HTTP 200; up (default) = any HTTP answer below 500.
# Returns 0 when it answers, 2 when the process died first, 1 on timeout; on
# failure prints the log tail.
wait_for() {
  local url="$1" name="$2" timeout="${3:-60}" pid="${4:-}" log="${5:-}" mode="${6:-up}"
  local deadline=$((SECONDS + timeout)) code
  while [[ $SECONDS -lt $deadline ]]; do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$url" 2>/dev/null)"
    if [[ "$mode" == "ready" && "$code" == "200" ]] || \
       [[ "$mode" == "up" && "$code" =~ ^[1-4][0-9][0-9]$ ]]; then
      c_green "  $name is up ($url)"
      return 0
    fi
    if [[ -n "$pid" ]] && ! kill -0 "$pid" 2>/dev/null; then
      c_red "  $name exited before it was ready"
      [[ -n "$log" && -f "$log" ]] && tail -n 25 "$log"
      return 2
    fi
    sleep 1
  done
  c_red "  $name did not answer $url within ${timeout}s (last HTTP ${code:-000})"
  [[ -n "$log" && -f "$log" ]] && tail -n 25 "$log"
  return 1
}

port_pids()  { lsof -nP -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null; }
port_owner() {
  local p
  p="$(port_pids "$1" | head -n 1)"
  [[ -n "$p" ]] && printf '%s (pid %s)' "$(proc_name "$p")" "$p"
}

# wait_port_free <port> [seconds]
wait_port_free() {
  local deadline=$((SECONDS + ${2:-10}))
  while [[ $SECONDS -lt $deadline ]]; do
    [[ -z "$(port_pids "$1")" ]] && return 0
    sleep 0.25
  done
  [[ -z "$(port_pids "$1")" ]]
}

# env_unset_args <file> — UNSET_ARGS=(-u KEY …) for every key the generated
# file sets or leaves unset (# KEY=), so a stray export in the caller's shell
# can't beat the file (godotenv, Next and Expo all let the real environment win).
env_unset_args() {
  UNSET_ARGS=()
  local k
  [[ -f "$1" ]] || return 0
  for k in $(sed -nE 's/^(# )?([A-Za-z_][A-Za-z0-9_]*)=.*/\2/p' "$1"); do
    UNSET_ARGS+=(-u "$k")
  done
}

# ---- postgres ---------------------------------------------------------------

pg_major() { "$1" --version 2>/dev/null | sed -nE 's/[^0-9]*([0-9]+).*/\1/p' | head -n 1; }

# pg_find_bins [preferred_dir] [exact_major] — PG_BIN = a directory with
# initdb, pg_ctl and psql of major >= 17 (or exactly exact_major).
pg_find_bins() {
  local want="${2:-}" d m cand=()
  if [[ -n "${1:-}" ]]; then
    cand=("$1")
  else
    cand=(/Library/PostgreSQL/17/bin /opt/homebrew/opt/postgresql@17/bin /usr/local/opt/postgresql@17/bin
      /Applications/Postgres.app/Contents/Versions/17/bin /usr/lib/postgresql/17/bin)
    command -v pg_ctl >/dev/null 2>&1 && cand+=("$(dirname "$(command -v pg_ctl)")")
    for d in /Library/PostgreSQL/*/bin /opt/homebrew/opt/postgresql@*/bin /usr/local/opt/postgresql@*/bin \
      /Applications/Postgres.app/Contents/Versions/*/bin /usr/lib/postgresql/*/bin; do
      [[ -d "$d" ]] && cand+=("$d")
    done
  fi
  for d in "${cand[@]}"; do
    [[ -x "$d/initdb" && -x "$d/pg_ctl" && -x "$d/psql" ]] || continue
    m="$(pg_major "$d/pg_ctl")"
    [[ -n "$m" ]] || continue
    if [[ -n "$want" ]]; then [[ "$m" -eq "$want" ]] || continue
    else [[ "$m" -ge 17 ]] || continue; fi
    PG_BIN="${d%/}"
    return 0
  done
  return 1
}

# compose <args…> — docker compose on this project's file, with its ports.
compose() {
  DEV_PG_PORT="${PG_PORT:-0}" DEV_MINIO_PORT="${MINIO_PORT:-0}" DEV_REDIS_PORT="${REDIS_PORT:-0}" \
    docker compose -f "$ROOT/docker-compose.yml" "$@"
}

# psql_dev [args…] / psql_admin [args…] — psql on the dev database / the
# server's postgres database. Feed SQL on stdin to use :'var' (-v) quoting.
psql_dev()   { _psql "$DB_NAME" "$@"; }
psql_admin() { _psql postgres "$@"; }
_psql() {
  local db="$1"
  shift
  if [[ "${PG_MODE:-private}" == "docker" ]]; then
    compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U "$DB_USER" -d "$db" "$@"
  else
    [[ -n "${PG_BIN:-}" && -x "$PG_BIN/psql" ]] || { c_red "  psql not found (set DEV_PG_BIN in .env.dev)"; return 127; }
    "$PG_BIN/psql" -X -q -v ON_ERROR_STOP=1 -h 127.0.0.1 -p "$PG_PORT" -U "$DB_USER" -d "$db" "$@"
  fi
}

# psql_shell — an interactive psql on the dev database.
psql_shell() {
  if [[ "${PG_MODE:-private}" == "docker" ]]; then
    compose exec db psql -U "$DB_USER" -d "$DB_NAME"
  else
    "$PG_BIN/psql" -X -h 127.0.0.1 -p "$PG_PORT" -U "$DB_USER" -d "$DB_NAME"
  fi
}

# db_recreate — drop (terminating connections) and recreate the dev database.
db_recreate() {
  psql_admin <<SQL
DROP DATABASE IF EXISTS "$DB_NAME" WITH (FORCE);
CREATE DATABASE "$DB_NAME" OWNER "$DB_USER";
SQL
}

# ---- backend ----------------------------------------------------------------

# backend_build — server + seed into .data/bin (go's cache makes it quick when current).
backend_build() {
  mkdir -p "$BIN_DIR" "$LOG_DIR"
  if ! ( cd "$ROOT/backend" && go build -o "$BIN_DIR/" ./cmd/server ./cmd/seed ) >"$LOG_DIR/backend-build.log" 2>&1; then
    c_red "  backend build failed — logs/backend-build.log:"
    tail -n 30 "$LOG_DIR/backend-build.log"
    return 1
  fi
  return 0
}

# backend_seed — migrate + upsert the dev identities (cmd/seed; idempotent).
backend_seed() {
  env_unset_args "$ROOT/backend/.env"
  if ! ( cd "$ROOT/backend" && env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} "$BIN_DIR/seed" ) >"$LOG_DIR/seed.log" 2>&1; then
    c_red "  seed failed — logs/seed.log:"
    tail -n 25 "$LOG_DIR/seed.log"
    return 1
  fi
  return 0
}

# backend_start — start .data/bin/server from backend/ (it reads the generated
# backend/.env) and wait for /readyz (180 s).
backend_start() {
  local holder
  holder="$(port_owner "$API_PORT")"
  if [[ -n "$holder" ]]; then
    c_red "  port $API_PORT is held by $holder, which this project did not start — not touching it"
    return 1
  fi
  env_unset_args "$ROOT/backend/.env"
  start_bg backend "$ROOT/backend" env ${UNSET_ARGS[@]+"${UNSET_ARGS[@]}"} "$BIN_DIR/server" || return 1
  wait_for "http://127.0.0.1:$API_PORT/readyz" "backend" 180 "$LAST_PID" "$LOG_DIR/backend.log" ready
}

# backend_stop — stop the recorded backend and wait for its port.
backend_stop() {
  stop_service backend
  [[ -n "${API_PORT:-}" ]] && wait_port_free "$API_PORT" 10
  return 0
}
