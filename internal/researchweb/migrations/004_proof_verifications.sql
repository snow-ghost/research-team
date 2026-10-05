CREATE TABLE proof_verifications (
    id text PRIMARY KEY,
    position integer NOT NULL,
    data jsonb NOT NULL CHECK(data->>'id'=id),
    target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL
        REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    attempt_id text GENERATED ALWAYS AS (data->>'attempt') STORED NOT NULL
        REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED,
    status text GENERATED ALWAYS AS (data->>'status') STORED NOT NULL
);
CREATE INDEX proof_verifications_target ON proof_verifications(target_id,attempt_id);
