#!/usr/bin/env bash
# Drop and recreate the local dev database, then migrate + seed it (cmd/seed),
# on the running dev-up stack's Postgres. Asks first.
#
# Usage: scripts/reset-db/reset-db.sh [--yes]
#
# Only ever touches scaffoldapp_dev on 127.0.0.1 at the port backend/.env names.
# If the backend is running it is restarted afterwards so its pool starts clean.
source "$(dirname "${BASH_SOURCE[0]}")/../lib.sh"

ASSUME_YES=0
for arg in "$@"; do
  case "$arg" in
    -y|--yes) ASSUME_YES=1 ;;
    -h|--help) awk 'NR > 1 && !/^#/ { exit } NR > 1 { sub(/^# ?/, ""); print }' "$0"; exit 0 ;;
    *) die "unknown argument: $arg (try --help)" ;;
  esac
done

require_local_db
[[ -f "$ROOT/backend/go.mod" ]] || die "no backend/ in this project"

c_blue "Reset $DB_NAME on 127.0.0.1:$PG_PORT"
confirm "drop and recreate $DB_NAME (all its data is lost)?" || die "  cancelled"
c_dim "  go build → .data/bin (logs/backend-build.log)"
backend_build || die "build failed: the database was left alone"
db_recreate >/dev/null || die "could not recreate $DB_NAME"
c_yellow "  dropped and recreated $DB_NAME"
backend_seed || die "seed failed (logs/seed.log)"
c_green "  migrated + seeded the dev identities"
if pid_alive backend; then
  c_dim "  restarting the backend (pid $PID_PID)"
  backend_stop
  DEVUP_ALLOW_START=1 backend_start || die "the backend did not come back (logs/backend.log)"
fi
c_green "  done"
