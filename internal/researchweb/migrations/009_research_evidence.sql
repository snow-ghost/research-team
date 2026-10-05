CREATE TABLE research_results (
 id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK(data->>'id'=id),
 target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
 verification_id text GENERATED ALWAYS AS (data->'binding'->>'verification') STORED NOT NULL REFERENCES proof_verifications(id) DEFERRABLE INITIALLY DEFERRED,
 review_id text GENERATED ALWAYS AS (data->>'review_attempt') STORED NOT NULL REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED,
 counter_id text GENERATED ALWAYS AS (data->>'counter_attempt') STORED NOT NULL REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE entities ADD COLUMN research_result_id text GENERATED ALWAYS AS (data->>'researchResult') STORED REFERENCES research_results(id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE decomposition_proposals (
 id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK(data->>'id'=id),
 target_id text GENERATED ALWAYS AS (data->>'target') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
 attempt_id text GENERATED ALWAYS AS (data->>'attempt') STORED NOT NULL REFERENCES attempts(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE lemma_library (
 id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK(data->>'id'=id),
 lemma_id text GENERATED ALWAYS AS (data->>'lemma') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
 result_id text GENERATED ALWAYS AS (data->>'result') STORED NOT NULL REFERENCES research_results(id) DEFERRABLE INITIALLY DEFERRED
);
