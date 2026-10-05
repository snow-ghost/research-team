CREATE TABLE research_cycles (
    id text PRIMARY KEY, position integer NOT NULL, data jsonb NOT NULL CHECK(data->>'id'=id),
    study_id text GENERATED ALWAYS AS (data->>'study') STORED NOT NULL REFERENCES studies(id) DEFERRABLE INITIALLY DEFERRED,
    goal_id text GENERATED ALWAYS AS (data->>'goal') STORED NOT NULL REFERENCES entities(id) DEFERRABLE INITIALLY DEFERRED,
    status text GENERATED ALWAYS AS (data->>'status') STORED NOT NULL,
    used_attempts integer GENERATED ALWAYS AS ((data->>'used_attempts')::integer) STORED,
    max_attempts integer GENERATED ALWAYS AS ((data->>'max_attempts')::integer) STORED,
    CHECK (max_attempts BETWEEN 1 AND 20 AND used_attempts BETWEEN 0 AND max_attempts)
);
CREATE UNIQUE INDEX one_active_cycle_per_study ON research_cycles(study_id)
    WHERE status IN ('running','awaiting_review','paused','blocked');
ALTER TABLE tasks ADD COLUMN cycle_id text GENERATED ALWAYS AS (NULLIF(data->>'cycle_id','')) STORED
    REFERENCES research_cycles(id) DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE attempts ADD COLUMN cycle_id text GENERATED ALWAYS AS (NULLIF(data->>'cycle_id','')) STORED
    REFERENCES research_cycles(id) DEFERRABLE INITIALLY DEFERRED;
