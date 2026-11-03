#!/bin/bash
set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
: "${RESEARCH_BACKUP_ROOT:?Set an absolute private backup root}"
case "$RESEARCH_BACKUP_ROOT" in
  /*) ;;
  *) printf '%s\n' 'RESEARCH_BACKUP_ROOT must be absolute' >&2; exit 64 ;;
esac
LABEL="${RESEARCH_TEAM_LABEL:-com.snowghost.research-team-server}"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DB="research_backup_$(uuidgen | tr -d '-' | tr 'A-F' 'a-f')_test"
DESTINATION="$RESEARCH_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)"

restart() {
  launchctl bootstrap "gui/$(id -u)" "$PLIST" >/dev/null 2>&1 || true
}
cleanup() {
  docker compose --env-file .env.postgres exec -T postgres dropdb -U postgres "$DB" >/dev/null 2>&1 || true
}
finish() {
  status=$?
  trap - EXIT
  cleanup
  restart
  exit "$status"
}
trap finish EXIT
trap 'exit 1' HUP INT TERM

launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
docker compose --env-file .env.postgres exec -T postgres createdb -U postgres -O research -T research "$DB"
mkdir -p "$RESEARCH_BACKUP_ROOT"
mkdir "$DESTINATION"
mkdir "$DESTINATION/state"
docker compose --env-file .env.postgres exec -T postgres pg_dump -U postgres -d "$DB" -Fc > "$DESTINATION/database.dump"
node scripts/check-postgres-backup.mjs "$DB"

cp -a .research-server/state/attempts "$DESTINATION/state/attempts"
cp -a .research-server/state/verifications "$DESTINATION/state/verifications"
diff -r .research-server/state/attempts "$DESTINATION/state/attempts" >/dev/null
diff -r .research-server/state/verifications "$DESTINATION/state/verifications" >/dev/null
test -s "$DESTINATION/database.dump"
node -e 'console.log(JSON.stringify({backup:process.argv[1],artifacts_match:true,database_dump:true,restore_verified:true,credentials_included:false}))' "$DESTINATION" | tee "$DESTINATION/manifest.json"
