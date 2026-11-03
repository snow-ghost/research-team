#!/bin/bash
set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
: "${RESEARCH_BACKUP_ROOT:?Set an absolute private backup root}"
: "${RESEARCH_TEAM_CONFIG:?Set an absolute private server config path}"
case "$RESEARCH_BACKUP_ROOT:$RESEARCH_TEAM_CONFIG" in
  /*:/*) ;;
  *) printf '%s\n' 'Backup root and server config must be absolute paths' >&2; exit 64 ;;
esac
LABEL="${RESEARCH_TEAM_LABEL:-com.snowghost.research-team-server}"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DESTINATION="$RESEARCH_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)"
IMAGE="${RESEARCH_BACKUP_IMAGE:-research-team-kimi-runtime:local}"
POSTGRES_IMAGE="${RESEARCH_BACKUP_POSTGRES_IMAGE:-postgres:18.6-bookworm@sha256:3725f4e2499eef5134592b3b4ab79a543ed7f8e533b05b5b637af926630f6650}"

run_maintenance() {
  local program="$1"
  shift
  docker run --rm --user "$(id -u):$(id -g)" --add-host host.docker.internal:host-gateway \
    -v "$ROOT:/src" -v "$ROOT:$ROOT" -w /src "$IMAGE" sh -c \
    'socat TCP-LISTEN:4187,fork,reuseaddr,bind=127.0.0.1 TCP:host.docker.internal:4187 & exec "$@"' \
    maintenance "/src/.research-server/bin/$program" -config "$RESEARCH_TEAM_CONFIG" "$@"
}

run_backup() {
  docker run --rm --user "$(id -u):$(id -g)" --add-host host.docker.internal:host-gateway \
    -v "$ROOT:/src" -v "$ROOT:$ROOT" -v "$RESEARCH_BACKUP_ROOT:$RESEARCH_BACKUP_ROOT" \
    -w /src --entrypoint /src/.research-server/bin/research-backup-linux "$POSTGRES_IMAGE" \
    -config "$RESEARCH_TEAM_CONFIG" -local-postgres -out "$DESTINATION"
}

wait_for_server() {
  local attempts=60
  while [ "$attempts" -gt 0 ]; do
    if curl -fsSI http://127.0.0.1:4187/ >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    attempts=$((attempts - 1))
  done
  return 1
}

wait_for_stop() {
  local attempts=60
  while [ "$attempts" -gt 0 ]; do
    if [ -z "$(docker ps -q --filter name=^/research-team-local$)" ]; then
      return 0
    fi
    sleep 1
    attempts=$((attempts - 1))
  done
  return 1
}

GOOS=linux GOARCH="$(go env GOARCH)" go build -o .research-server/bin/research-quiesce-linux ./cmd/research-quiesce
GOOS=linux GOARCH="$(go env GOARCH)" go build -o .research-server/bin/research-backup-linux ./cmd/research-backup
mkdir -p "$RESEARCH_BACKUP_ROOT"
engaged=0
finish() {
  status=$?
  trap - EXIT
  if [ "$engaged" -eq 1 ]; then
    if ! launchctl bootstrap "gui/$(id -u)" "$PLIST" >/dev/null 2>&1; then
      status=1
    fi
    if ! wait_for_server || ! run_maintenance research-quiesce-linux -release; then
      status=1
    fi
  fi
  exit "$status"
}
trap finish EXIT
trap 'exit 1' HUP INT TERM

run_maintenance research-quiesce-linux
engaged=1
launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
wait_for_stop
run_backup
