#!/bin/sh
set -eu
psql -X -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<'SQL'
\getenv app_password RESEARCH_DB_PASSWORD
CREATE ROLE research LOGIN PASSWORD :'app_password' NOSUPERUSER NOCREATEDB NOCREATEROLE;
ALTER DATABASE research OWNER TO research;
GRANT ALL ON SCHEMA public TO research;
SQL
