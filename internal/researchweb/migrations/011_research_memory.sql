CREATE TABLE research_memory (
 id text PRIMARY KEY,
 position integer NOT NULL UNIQUE,
 data jsonb NOT NULL CHECK (jsonb_typeof(data)='object'),
 target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
 verification_id text GENERATED ALWAYS AS (NULLIF(data->>'verification','')) STORED REFERENCES proof_verifications(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX research_memory_scope ON research_memory(target_id,(data->>'kind'));
ALTER TABLE entities ADD COLUMN refutation_verification_id text GENERATED ALWAYS AS (NULLIF(data->>'refutation_verification','')) STORED REFERENCES proof_verifications(id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE entities ADD COLUMN refutation_review_id text GENERATED ALWAYS AS (NULLIF(data->>'refutation_review','')) STORED REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED;
