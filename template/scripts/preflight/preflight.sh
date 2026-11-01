#!/usr/bin/env bash
# Read-only health check of the running dev-up stack. Each failure names its fix.
#
# Usage: scripts/preflight/preflight.sh      (exit 1 when anything failed)
#
# Checks that the services dev-up started answer (API /readyz, Postgres, web,
# Metro); that the running backend started after the newest commit touching
# backend/ and after the newest backend source change on disk, committed or
# not (a stale binary); and that the generated env files are newer than
# .env.dev and scaffold.json. Changes nothing.
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

PASSED=0 FAILED=0
pass() { PASSED=$((PASSED + 1)); c_green "  ✓ $*"; }
fail() { FAILED=$((FAILED + 1)); c_red "  ✗ $*"; }
note() { c_yellow "  ~ $*"; }
finish() {
  echo
  if [[ "$FAILED" -gt 0 ]]; then c_red "Preflight: $FAILED failed, $PASSED passed"; exit 1; fi
  c_green "Preflight: all $PASSED checks passed"
  exit 0
}

# Seconds since the epoch at which <pid> started (from ps etime; portable).
proc_start_epoch() {
  local e d=0 h=0 m=0 s=0 a b c
  e="$(ps -o etime= -p "$1" 2>/dev/null | tr -d ' ')"
  [[ -n "$e" ]] || return 1
  if [[ "$e" == *-* ]]; then d="${e%%-*}"; e="${e#*-}"; fi
  IFS=: read -r a b c <<<"$e"
  if [[ -n "$c" ]]; then h="$a" m="$b" s="$c"; else m="$a" s="$b"; fi
  echo $(( $(date +%s) - (10#$d * 86400 + 10#$h * 3600 + 10#$m * 60 + 10#$s) ))
}

# "<mtime> <path>" of the newest backend source file.
newest_backend_source() {
  local fmt=(-c '%Y %n')
  case "$(uname)" in Darwin|*BSD) fmt=(-f '%m %N') ;; esac
  find "$ROOT/backend" -type f \( -name '*.go' -o -name '*.sql' -o -name go.mod -o -name go.sum \) \
    -exec stat "${fmt[@]}" {} + 2>/dev/null | sort -n | tail -n 1
}

c_blue "Preflight"
stack_discover
[[ -n "$API_PORT" ]] || { fail "backend/.env is missing: start the stack with scripts/dev-up/dev-up.sh"; finish; }
is_local_url "$API_URL" || die "API is $API_URL: preflight only checks a local stack"

# --- services ------------------------------------------------------------------
if pid_alive dev-up; then pass "dev-up is running (pid $PID_PID)"; else note "dev-up is not running (services may have been started by the helper scripts)"; fi

if [[ -f "$ROOT/backend/go.mod" && "${RUN_BACKEND:-1}" != "0" ]]; then
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$API_PORT/readyz" 2>/dev/null)"
  if [[ "$code" == "200" ]]; then pass "backend ready ($API_URL/readyz)"
  else fail "backend not ready at $API_URL/readyz (HTTP ${code:-000}): scripts/restart-backend/restart-backend.sh, or dev-up (logs/backend.log)"; fi
fi

if psql_admin -tAc "SELECT 1" >/dev/null 2>&1; then pass "Postgres answers on 127.0.0.1:$PG_PORT (${PG_MODE})"
else fail "Postgres is not answering on 127.0.0.1:${PG_PORT:-?}: start the stack with scripts/dev-up/dev-up.sh (logs/postgres.log)"; fi

if [[ "${RUN_WEB:-0}" == "1" ]]; then
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 "http://127.0.0.1:$WEB_PORT/" 2>/dev/null)"
  if [[ "$code" =~ ^[1-4][0-9][0-9]$ ]]; then pass "web answers ($WEB_URL → $code)"
  else fail "web not answering at $WEB_URL (HTTP ${code:-000}): R in the dev-up menu (logs/web.log)"; fi
fi

if [[ "${RUN_MOBILE:-0}" == "1" ]]; then
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$METRO_PORT/status" 2>/dev/null)"
  if [[ "$code" == "200" ]]; then pass "Metro answers (:$METRO_PORT)"
  else fail "Metro not answering on :$METRO_PORT: restart dev-up (logs/metro.log)"; fi
fi

# --- is the running backend built from the current source? -----------------------
# Debugging "missing" routes that exist in the source, because the process
# answering predates them, is the most expensive way to lose a morning.
backend_pid=""
if pid_alive backend; then backend_pid="$PID_PID"
else
  backend_pid="$(port_pids "$API_PORT" | head -n 1)"
  [[ -n "$backend_pid" ]] && note "port $API_PORT is held by $(proc_name "$backend_pid") (pid $backend_pid), which dev-up did not record"
fi
if [[ -n "$backend_pid" && -f "$ROOT/backend/go.mod" ]]; then
  started="$(proc_start_epoch "$backend_pid")"
  newest="$(newest_backend_source)"
  stale=0
  if [[ -n "$started" && -n "$newest" && "${newest%% *}" -gt "$started" ]]; then
    fail "backend source changed after the running backend started (${newest#*"$ROOT"/}): run scripts/restart-backend/restart-backend.sh"
    stale=1
  fi
  if [[ -n "$started" ]] && git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    commit="$(git -C "$ROOT" log -1 --format=%ct -- backend/ 2>/dev/null)"
    if [[ -n "$commit" && "$commit" -gt "$started" ]]; then
      age=$(( (commit - started) / 60 ))
      if [[ "$age" -lt 60 ]]; then
        note "the last backend commit is ${age}m newer than the running backend: fine if you committed code that was already running"
      else
        fail "the running backend predates the last backend commit by ~$((age / 60))h: run scripts/restart-backend/restart-backend.sh"
        stale=1
      fi
    fi
    dirty="$(git -C "$ROOT" status --porcelain -- backend/ 2>/dev/null | wc -l | tr -d ' ')"
    [[ "$dirty" -gt 0 ]] && note "backend/ has $dirty uncommitted change(s) (covered by the source-time check above)"
  fi
  if [[ "$stale" -eq 0 && -n "$started" ]]; then pass "the running backend is newer than its source"; fi
  [[ -z "$started" ]] && note "could not read the backend's start time"
elif [[ -f "$ROOT/backend/go.mod" ]]; then
  note "no backend process to compare with its source"
fi

# --- generated env files ------------------------------------------------------------
for pair in "backend:backend/.env" "web:web/.env.local" "mobile:mobile/.env"; do
  dir="${pair%%:*}" file="${pair#*:}"
  [[ -d "$ROOT/$dir" ]] || continue
  if [[ ! -f "$ROOT/$file" ]]; then fail "$file is missing: run scripts/dev-up/dev-up.sh"; continue; fi
  older=""
  for src in .env.dev scaffold.json; do
    [[ -f "$ROOT/$src" && "$ROOT/$src" -nt "$ROOT/$file" ]] && older="$older $src"
  done
  if [[ -n "$older" ]]; then fail "$file is older than${older}: re-run scripts/dev-up/dev-up.sh to regenerate it (restart-backend does not)"
  else pass "$file is current"; fi
done

finish
