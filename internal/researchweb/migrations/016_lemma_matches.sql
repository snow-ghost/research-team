CREATE TABLE lemma_matches (
    id text PRIMARY KEY,
    position integer NOT NULL,
    data jsonb NOT NULL CHECK (jsonb_typeof(data) = 'object' AND data->>'id' = id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL
        REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    library_id text GENERATED ALWAYS AS (data->>'library') STORED NOT NULL
        REFERENCES lemma_library(id) DEFERRABLE INITIALLY DEFERRED,
    target_revision integer GENERATED ALWAYS AS ((data->>'target_revision')::integer) STORED
        NOT NULL CHECK (target_revision > 0)
);
