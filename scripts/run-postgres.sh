#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
. ./.env.postgres
export RESEARCH_DATABASE_URL
exec go run ./cmd/research-server -config examples/server/postgres.json "$@"
