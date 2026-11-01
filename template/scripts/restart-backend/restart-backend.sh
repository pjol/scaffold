#!/usr/bin/env bash
# Rebuild the Go backend and restart it on the dev-up stack, from the generated
# backend/.env (the server reads it itself; nothing is copied by hand).
#
# Usage: scripts/restart-backend/restart-backend.sh
#
# Stops only the backend dev-up (or this script) recorded in
# .data/run/backend.pid; a process it did not start is never touched. The new
# server runs detached, so it outlives this script; dev-up's Ctrl-C/stop still
# stops it (it reads the same pid file).
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

require_local_config
[[ -f "$ROOT/backend/go.mod" ]] || die "no backend/ in this project"
command -v go >/dev/null 2>&1 || die "go not found"

c_blue "Restart backend ($API_URL)"
c_dim "  go build ./cmd/server ./cmd/seed → .data/bin (logs/backend-build.log)"
backend_build || die "build failed: the running backend was left alone"
if pid_alive backend; then
  c_dim "  stopping the running backend (pid $PID_PID)"
fi
backend_stop
holder="$(port_owner "$API_PORT")"
[[ -z "$holder" ]] || die "port $API_PORT is held by $holder, which this project did not start: not touching it"
DEVUP_ALLOW_START=1 backend_start || die "the backend did not become ready (logs/backend.log)"
c_green "  backend restarted (pid $LAST_PID)"
