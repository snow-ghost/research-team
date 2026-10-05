CREATE TABLE study_connectors (
    id text PRIMARY KEY, position integer NOT NULL,
    data jsonb NOT NULL CHECK(data->>'id'=id),
    study_id text GENERATED ALWAYS AS (data->>'study') STORED NOT NULL
        REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED,
    kind text GENERATED ALWAYS AS (data->>'kind') STORED NOT NULL,
    connector text GENERATED ALWAYS AS (data->>'connector') STORED NOT NULL,
    UNIQUE(study_id,kind,connector)
);
