#!/bin/sh
set -eu
: "${RESEARCH_TEAM_CONFIG:?Set the private server config path}"
: "${RESEARCH_BACKUP_ROOT:?Set a persistent private backup directory}"
case "$RESEARCH_BACKUP_ROOT" in /*) ;; *) printf '%s\n' 'Backup root must be absolute.' >&2; exit 2;; esac
umask 077
mkdir -p "$RESEARCH_BACKUP_ROOT"
chmod 700 "$RESEARCH_BACKUP_ROOT"
bin/research-quiesce -config "$RESEARCH_TEAM_CONFIG"
systemctl --user stop research-team.service
trap 'systemctl --user start research-team.service' EXIT HUP INT TERM
destination="$RESEARCH_BACKUP_ROOT/$(date -u +%Y%m%dT%H%M%SZ)"
bin/research-backup -config "$RESEARCH_TEAM_CONFIG" -out "$destination"
