CREATE TABLE study_messages (
 id text PRIMARY KEY,
 position integer NOT NULL UNIQUE,
 data jsonb NOT NULL,
 study_id text GENERATED ALWAYS AS (data->>'study') STORED REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE telegram_offsets (
 id text PRIMARY KEY,
 position integer NOT NULL UNIQUE,
 data jsonb NOT NULL
);
